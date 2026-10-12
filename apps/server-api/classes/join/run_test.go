package join

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// syncBuffer is the output of Run under a lock. Run writes to it from its own
// goroutine while the case reads what has arrived so far; with a plain
// bytes.Buffer that is a data race, and -race found it in 2 of 35 runs.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// Bytes copies what has arrived; the copy is the case's own.
func (b *syncBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.buf.Bytes())
}

func (b *syncBuffer) Len() int { return len(b.Bytes()) }

func (b *syncBuffer) String() string { return string(b.Bytes()) }

// serveAPI puts both paths of the issuing API behind one TLS server whose key
// is the one the fixture's configuration pins, so a case can run the whole way
// Run goes, pinned TLS included.
func serveAPI(t *testing.T, j *Join) (*httptest.Server, *http.Client) {
	t.Helper()
	cert, pin := trusttest.APITLS(0xE1, "api-1.test")
	mux := http.NewServeMux()
	mux.HandleFunc(tunnel.PathJoin, j.Accept)
	mux.HandleFunc(PathKeys, j.Keys)
	srv := httptest.NewUnstartedServer(mux)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	if !bytes.Equal(pin, j.S.Pin) {
		t.Fatal("the server's TLS key is not the one the API pins, so nothing here is measured")
	}
	// The pin is what the client checks; the certificate's name is not, so
	// ServerName may stay what the dialer uses.
	return srv, &http.Client{Timeout: 10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tunnel.PinnedTLS(pin)}}
}

// codeFor makes a join code whose URL is the test server's host, so Run really
// reaches these handlers.
func codeFor(t *testing.T, j *Join, srv *httptest.Server) tunnel.JoinCode {
	t.Helper()
	code, _, err := j.NewJoinCode(ctx)
	if err != nil {
		t.Fatalf("new code: %v", err)
	}
	code.URL = "wss://" + srv.Listener.Addr().String() + "/tunnel"
	return code
}

// runConfig is the configuration of a joining server inside dir.
func runConfig(t *testing.T, dir string, code tunnel.JoinCode, client *http.Client, out *syncBuffer) JoinConfig {
	t.Helper()
	_, pin := trusttest.APITLS(0xE2, "api-2.test")
	return JoinConfig{
		Code: code.String(), Node: "api-2", URL: "wss://api-2.test:7443/tunnel", TLS: pin,
		NoiseFile: filepath.Join(dir, "noise"), SignFile: filepath.Join(dir, "sign"),
		SecretFile: filepath.Join(dir, "join-secret"), Out: out, Client: client,
		Retry: 20 * time.Millisecond, Timeout: 10 * time.Second,
	}
}

// TestRunJoinsAndFetchesTheKeys walks the whole joining side against the real
// handlers: the request arrives, the fingerprint on the output is the one the
// admin sees in the shell, and after the admin's signature the two shared keys
// land in the file the ulh script reads.
func TestRunJoinsAndFetchesTheKeys(t *testing.T) {
	j, fx := newJoin(t)
	srv, client := serveAPI(t, j)
	code := codeFor(t, j, srv)
	dir := t.TempDir()
	var out syncBuffer
	cfg := runConfig(t, dir, code, client, &out)

	// The admin signs while Run is already waiting at PathKeys, which is the
	// real order; before that, Run must not finish.
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()

	// Wait for the request to arrive, so the signature really comes after it.
	var rows int
	for i := 0; i < 500; i++ {
		got, err := j.Waiting(ctx)
		if err != nil {
			t.Fatalf("waiting: %v", err)
		}
		if rows = len(got); rows == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rows != 1 {
		t.Fatalf("%d requests arrived, want 1", rows)
	}
	select {
	case err := <-done:
		t.Fatalf("Run finished before anyone signed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	// The fingerprint on the console is the one the shell shows for this row.
	pending, err := j.Waiting(ctx)
	if err != nil {
		t.Fatalf("waiting: %v", err)
	}
	want := tunnel.JoinFingerprint(pending[0].Node, pending[0].Noise, pending[0].Sign,
		pending[0].TLS, pending[0].URL)
	if !bytes.Contains(out.Bytes(), []byte(want)) {
		t.Fatalf("the output does not show the fingerprint %q: %q", want, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(tunnel.Fingerprint(code.Anchor.Hash))) {
		t.Fatalf("the output does not name the anchor: %q", out.String())
	}

	// Now the admin lists exactly the keys that arrived.
	listTheWaitingServer(t, j, fx, pending[0])
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not finish after the signature")
	}

	// The shared keys lie in the file, and they are this installation's.
	raw, err := os.ReadFile(cfg.SecretFile)
	if err != nil {
		t.Fatalf("secret file: %v", err)
	}
	var shared sharedKeys
	if err := json.Unmarshal(raw, &shared); err != nil {
		t.Fatalf("secret file: %v", err)
	}
	if !bytes.Equal(shared.SecretKey, testSecretKey) || !bytes.Equal(shared.ChallengeKey, testChallengeKey) {
		t.Fatal("the file holds other keys than the installation's")
	}
	// The private keys this server made lie in their files and are not the
	// public ones that travelled.
	noiseRaw, err := os.ReadFile(cfg.NoiseFile)
	if err != nil {
		t.Fatalf("noise file: %v", err)
	}
	priv, err := enc.B64.DecodeString(string(noiseRaw))
	if err != nil || len(priv) != 32 {
		t.Fatalf("the noise file holds no 32-byte key: %v", err)
	}
	if bytes.Equal(priv, pending[0].Noise) {
		t.Fatal("the file holds the public key, not the private one")
	}
	signRaw, err := os.ReadFile(cfg.SignFile)
	if err != nil {
		t.Fatalf("sign file: %v", err)
	}
	if seed, err := enc.B64.DecodeString(string(signRaw)); err != nil || len(seed) != 32 {
		t.Fatalf("the sign file holds no 32-byte seed: %v", err)
	}
	// All three files are 0600 where the system has such modes at all.
	if runtime.GOOS != "windows" {
		for _, p := range []string{cfg.NoiseFile, cfg.SignFile, cfg.SecretFile} {
			st, err := os.Stat(p)
			if err != nil {
				t.Fatalf("stat %s: %v", p, err)
			}
			if st.Mode().Perm() != fs.FileMode(0o600) {
				t.Fatalf("%s has mode %o, want 600", p, st.Mode().Perm())
			}
		}
	}
}

// TestRunStopsWithAGermanMessage holds the time limit: nobody signs, and Run
// stops with the message the admin reads on the console.
func TestRunStopsWithAGermanMessage(t *testing.T) {
	j, _ := newJoin(t)
	srv, client := serveAPI(t, j)
	code := codeFor(t, j, srv)
	dir := t.TempDir()
	var out syncBuffer
	cfg := runConfig(t, dir, code, client, &out)
	cfg.Timeout = 150 * time.Millisecond
	err := Run(ctx, cfg)
	if !errors.Is(err, ErrJoinTimeout) {
		t.Fatalf("Run answered %v, want ErrJoinTimeout", err)
	}
	if !bytes.Contains([]byte(err.Error()), []byte("Vertrauensliste")) {
		t.Fatalf("the message is not the German one: %q", err.Error())
	}
	// The request did arrive, so this case measured the waiting and not an
	// earlier wall.
	rows, err := j.Waiting(ctx)
	if err != nil {
		t.Fatalf("waiting: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d requests arrived, want 1", len(rows))
	}
	// And nothing was written where the ulh script looks.
	if _, err := os.Stat(cfg.SecretFile); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a secret file was left behind: %v", err)
	}
}

// TestRunStopsOnARefusedCode holds that a spent code does not reach the
// waiting at all: nothing is posted twice and no file of shared keys appears.
func TestRunStopsOnARefusedCode(t *testing.T) {
	j, _ := newJoin(t)
	srv, client := serveAPI(t, j)
	code := codeFor(t, j, srv)
	// Spend the code, as a first joining server would have.
	h := secretHash(code.Secret)
	hash, err := enc.B64.DecodeString(h)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := j.S.DB.OnceUse(ctx, dbapi.OnceJoin, hash, testNow); err != nil {
		t.Fatalf("spend: %v", err)
	}
	dir := t.TempDir()
	var out syncBuffer
	cfg := runConfig(t, dir, code, client, &out)
	cfg.Timeout = 10 * time.Second
	start := time.Now()
	err = Run(ctx, cfg)
	if !errors.Is(err, ErrJoinRefused) {
		t.Fatalf("Run answered %v, want ErrJoinRefused", err)
	}
	// It stopped at the POST, not after the hour: otherwise the case would
	// pass with the refusal unnoticed.
	if time.Since(start) > 5*time.Second {
		t.Fatal("Run went on waiting after the refusal")
	}
	if _, err := os.Stat(cfg.SecretFile); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a secret file was left behind: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("a fingerprint was shown for a refused request: %q", out.String())
	}
}

// TestRunRefusesUnusableInput holds the three values of this server itself
// that Run checks before anything travels.
func TestRunRefusesUnusableInput(t *testing.T) {
	j, _ := newJoin(t)
	srv, client := serveAPI(t, j)
	code := codeFor(t, j, srv)
	_, pin := trusttest.APITLS(0xE2, "api-2.test")
	cases := []struct {
		name  string
		spoil func(c *JoinConfig)
	}{
		{"no code", func(c *JoinConfig) { c.Code = "nonsense" }},
		{"a node ID with a space", func(c *JoinConfig) { c.Node = "api 2" }},
		{"an address that is no tunnel address", func(c *JoinConfig) { c.URL = "http://api-2.test/" }},
		{"a TLS pin of the wrong length", func(c *JoinConfig) { c.TLS = pin[:16] }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			var out syncBuffer
			cfg := runConfig(t, dir, code, client, &out)
			c.spoil(&cfg)
			if err := Run(ctx, cfg); err == nil {
				t.Fatal("Run took it")
			}
			// Nothing was made and nothing travelled.
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("read dir: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("%d files were written anyway", len(entries))
			}
			rows, err := j.Waiting(ctx)
			if err != nil {
				t.Fatalf("waiting: %v", err)
			}
			if len(rows) != 0 {
				t.Fatalf("%d requests arrived anyway", len(rows))
			}
		})
	}
}
