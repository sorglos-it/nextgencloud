package join

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// quietValues lists what an error of Run may not carry, each with the name it
// is called by in a failure: the message itself is never printed, because
// printing it is the very thing under test.
//
// Measured on 17.10.2026 against the code before this change, both of them
// really were in there:
//
//	Post "https://127.0.0.1:14780/tunnel/join?secret=d5En6nQB...": dial tcp ...
//	mkdir C:\...\blocked: Das System kann den angegebenen Pfad nicht finden.
//
// -- the address of the issuing API with the hash of the one-time secret the
// API spends, and the path of a file a private key goes into. ngc-api -join
// wrote the first one into the journal word for word.
func quietValues(cfg JoinConfig, code tunnel.JoinCode, host string) []struct{ what, text string } {
	return []struct{ what, text string }{
		{"join code", code.String()},
		{"one-time secret", enc.B64.EncodeToString(code.Secret)},
		{"hash of the one-time secret", secretHash(code.Secret)},
		{"address of the issuing API", host},
		{"path of the join request", tunnel.PathJoin},
		{"path of the Noise key", cfg.NoiseFile},
		// The error of a failed write names what it stumbled over, and that
		// is often the DIRECTORY, not the file: measured, "mkdir <dir>\blocked:
		// ..." carries the parent and not the full path. An assertion on the
		// full path alone would have let this leak through -- it did, in the
		// first red run of 17.10.2026.
		{"directory of the Noise key", filepath.Dir(cfg.NoiseFile)},
		{"path of the signing key", cfg.SignFile},
		{"directory of the signing key", filepath.Dir(cfg.SignFile)},
		{"file of the fetched keys", cfg.SecretFile},
	}
}

// Every way out of Run that is not a join answers with one of the categories,
// and the category is the whole answer: a server that cannot join has to say
// whether anything was sent at all, whether the right server answered, and
// whether the code is spent -- without naming one value.
func TestRunAnswersWithACategory(t *testing.T) {
	for name, build := range map[string]func(t *testing.T) (JoinConfig, tunnel.JoinCode, string, error){
		// Nothing goes out: this server cannot join with what it holds.
		"unusable values": func(t *testing.T) (JoinConfig, tunnel.JoinCode, string, error) {
			j, _ := newJoin(t)
			srv, client := serveAPI(t, j)
			code := codeFor(t, j, srv)
			cfg := runConfig(t, t.TempDir(), code, client, &syncBuffer{})
			cfg.Node = "api 2" // a node ID with a space
			return cfg, code, srv.Listener.Addr().String(), ErrJoinInput
		},
		// Nobody is listening any more.
		"no answer": func(t *testing.T) (JoinConfig, tunnel.JoinCode, string, error) {
			j, _ := newJoin(t)
			srv, client := serveAPI(t, j)
			code := codeFor(t, j, srv)
			cfg := runConfig(t, t.TempDir(), code, client, &syncBuffer{})
			addr := srv.Listener.Addr().String()
			srv.Close()
			return cfg, code, addr, ErrJoinNoAnswer
		},
		// Something takes the connection and never answers: a deadline of
		// the transport itself, which is a net.Error that times out and is
		// not context.DeadlineExceeded.
		"no answer in time": func(t *testing.T) (JoinConfig, tunnel.JoinCode, string, error) {
			j, _ := newJoin(t)
			srv, _ := serveAPI(t, j)
			code := codeFor(t, j, srv)
			addr := silentListener(t)
			code.URL = "wss://" + addr + "/tunnel"
			cfg := runConfig(t, t.TempDir(), code, &http.Client{Transport: &http.Transport{
				TLSClientConfig: tunnel.PinnedTLS(j.S.Pin), TLSHandshakeTimeout: 50 * time.Millisecond},
			}, &syncBuffer{})
			cfg.Code = code.String()
			return cfg, code, addr, ErrJoinTooSlow
		},
		// Somebody answers, but it is not the server the code names. The
		// client is nil here on purpose: only then does Run build the one
		// that checks the pin, and that check is what this walks.
		"another server": func(t *testing.T) (JoinConfig, tunnel.JoinCode, string, error) {
			j, _ := newJoin(t)
			srv, _ := serveAPI(t, j)
			// This case turns the handshake away on purpose; its line
			// belongs in no test output.
			srv.Config.ErrorLog = log.New(io.Discard, "", 0)
			code := codeFor(t, j, srv)
			_, foreign := trusttest.APITLS(0xE9, "api-9.test")
			code.TLS = foreign
			cfg := runConfig(t, t.TempDir(), code, nil, &syncBuffer{})
			cfg.Code = code.String()
			return cfg, code, srv.Listener.Addr().String(), ErrJoinWrongAPI
		},
		// The API answers, and it says no: the code was already spent.
		"the code refused": func(t *testing.T) (JoinConfig, tunnel.JoinCode, string, error) {
			j, _ := newJoin(t)
			srv, client := serveAPI(t, j)
			code := codeFor(t, j, srv)
			hash, err := enc.B64.DecodeString(secretHash(code.Secret))
			if err != nil {
				t.Fatalf("hash: %v", err)
			}
			if _, err := j.S.DB.OnceUse(ctx, dbapi.OnceJoin, hash, testNow); err != nil {
				t.Fatalf("spend: %v", err)
			}
			cfg := runConfig(t, t.TempDir(), code, client, &syncBuffer{})
			return cfg, code, srv.Listener.Addr().String(), ErrJoinRefused
		},
		// A private key of this server cannot be written. Its error names
		// the path of that key file, which is why it never leaves Run.
		"a key will not be written": func(t *testing.T) (JoinConfig, tunnel.JoinCode, string, error) {
			j, _ := newJoin(t)
			srv, client := serveAPI(t, j)
			code := codeFor(t, j, srv)
			dir := t.TempDir()
			cfg := runConfig(t, dir, code, client, &syncBuffer{})
			// A file where a directory would have to be: MkdirAll fails and
			// names the path it stumbled over.
			blocked := filepath.Join(dir, "blocked")
			if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg.NoiseFile = filepath.Join(blocked, "noise")
			return cfg, code, srv.Listener.Addr().String(), ErrJoinSecret
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, code, host, want := build(t)
			cfg.Timeout = 2 * time.Second
			err := Run(ctx, cfg)
			if !errors.Is(err, want) {
				t.Fatalf("Run = %v, want %v", err, want)
			}
			// The category comes back BARE. errors.Is alone would let a
			// wrapping through, and a wrapping is where a value gets back
			// in: a mutant that put "(HTTP %d)" -- the answer of that
			// server -- back onto the refusal LIVED until this line.
			if err.Error() != want.Error() {
				t.Errorf("Run wrapped the category with a word of its own")
			}
			for _, q := range quietValues(cfg, code, host) {
				if len(q.text) >= 4 && strings.Contains(err.Error(), q.text) {
					t.Errorf("Run error names the %s", q.what)
				}
			}
		})
	}
}

// silentListener accepts connections and holds them without a word, and gives
// back its address. It is not an API: it is what a deadline needs, a peer
// that neither answers nor refuses.
func silentListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	t.Cleanup(func() { ln.Close(); <-done })
	return ln.Addr().String()
}
