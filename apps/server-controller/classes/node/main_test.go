package node

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// pairCode builds a pairing code the way an API hands it out. Only the
// contract says what counts as one, so the tests use a real one instead of a
// word that merely looks like a code.
func pairCode() string {
	return tunnel.PairCode{
		URL:    "wss://api-1.test:8443/tunnel",
		Key:    bytes.Repeat([]byte{1}, 32),
		TLS:    bytes.Repeat([]byte{2}, sha256.Size),
		Secret: bytes.Repeat([]byte{3}, tunnel.SecretSize),
		Anchor: trust.Anchor{Seq: 1, Hash: bytes.Repeat([]byte{4}, sha256.Size)},
	}.String()
}

func TestMainVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Main("1.2.3", []string{"-version"}, noInput(), &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := out.String(); !strings.HasPrefix(got, "ngc-controller 1.2.3 (contract ") {
		t.Fatalf("output = %q", got)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", errOut.String())
	}
}

func TestMainUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Main("1.2.3", []string{"frobnicate"}, noInput(), &out, &errOut); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "frobnicate") {
		t.Fatalf("stderr = %q, want the unknown command named", errOut.String())
	}
}

// "pair" reads the code from the standard input. A script that pipes nothing
// in gets one fixed line and pairs nothing.
func TestMainPairNeedsCode(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Main("1.2.3", []string{"pair"}, noInput(), &out, &errOut); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if got := errOut.String(); got != msgBadCode+"\n" {
		t.Fatalf("stderr = %q, want %q", got, msgBadCode+"\n")
	}
}

// noInput is a standard input that holds nothing: the calls that never get as
// far as the pairing code use it.
func noInput() io.Reader { return strings.NewReader("") }

// typed is the standard input of an admin who entered the code, and of the ulh
// script that pipes it in: one line, then the end.
func typed(code string) io.Reader { return strings.NewReader(code + "\n") }

// Only something that looks like a command name is repeated. Anything else may
// be a pasted pairing code, and that must not reach the screen or the journal.
func TestMainRepeatsOnlyCommandNames(t *testing.T) {
	for _, arg := range []string{
		pairCode(),
		"ngc-pair:abc",
		"ngc-join:abc",
		"FROBNICATE",
		"frobnicate2",
		"-frobnicate",
		strings.Repeat("a", 33),
	} {
		var out, errOut bytes.Buffer
		if code := Main("1.2.3", []string{arg}, noInput(), &out, &errOut); code != 2 {
			t.Errorf("Main(%q) = %d, want 2", arg, code)
		}
		if strings.Contains(out.String()+errOut.String(), arg) {
			t.Errorf("Main(%q) repeated its argument: %q", arg, errOut.String())
		}
		if !strings.Contains(errOut.String(), "Aufruf: ngc-controller") {
			t.Errorf("Main(%q) stderr = %q, want the German usage", arg, errOut.String())
		}
	}
}

// Commands that are still to come answer with exit 1 and say so in German, so
// an admin sees the difference between a wrong call and a part not built yet.
func TestMainCommandsNotBuiltYet(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"terminal"},
		{"hold"},
		{"image", "-arch", "arm64", "-out", "terminal-arm64.tar"},
	} {
		var out, errOut bytes.Buffer
		if code := Main("1.2.3", args, noInput(), &out, &errOut); code != 1 {
			t.Errorf("Main(%q) = %d, want 1", args, code)
		}
		if !strings.Contains(errOut.String(), "Dieser Teil ist noch nicht gebaut.") {
			t.Errorf("Main(%q) stderr = %q", args, errOut.String())
		}
		if out.Len() != 0 {
			t.Errorf("Main(%q) stdout = %q, want empty", args, out.String())
		}
	}
}

// -h is a question, not a mistake: the answer goes to stdout and the program
// ends well.
func TestMainHelpListsTheCommands(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Main("1.2.3", []string{"-h"}, noInput(), &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, want := range []string{"Aufruf: ngc-controller", "pair", "terminal", "hold", "image"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("stdout = %q, want %q in it", out.String(), want)
		}
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", errOut.String())
	}
}

// A flag stands before the command; the command keeps its own flags. The
// answer shows which command ran: only "pair" asks for controller.json, and it
// is the file this flag names, in a directory of this test alone.
func TestMainConfigFlagBeforeCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	cfgPath := filepath.Join(t.TempDir(), "controller.json")
	args := []string{"-config", cfgPath, "pair"}
	if code := Main("1.2.3", args, typed(pairCode()), &out, &errOut); code != 1 {
		t.Fatalf("exit code = %d, want 1 (the command was reached)", code)
	}
	if got := errOut.String(); got != msgNoConfig+"\n" {
		t.Errorf("stderr = %q, want %q (pair read the named file)", got, msgNoConfig+"\n")
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want empty", out.String())
	}
}

// "image" only takes an architecture NGC is built for and a file that stays
// where the admin points it: both become arguments of another program later.
func TestMainImageNeedsKnownArchAndFile(t *testing.T) {
	for _, args := range [][]string{
		{"image"},
		{"image", "-arch", "x86", "-out", "terminal.tar"},
		{"image", "-arch", "amd64"},
		{"image", "-arch", "amd64", "-out", "../terminal.tar"},
		{"image", "-arch", "amd64", "-out", strings.Repeat("a", maxOutPath+1)},
		{"image", "-arch", "amd64", "-out", "terminal.tar", "more"},
	} {
		var out, errOut bytes.Buffer
		if code := Main("1.2.3", args, noInput(), &out, &errOut); code != 2 {
			t.Errorf("Main(%q) = %d, want 2", args, code)
		}
	}
}

// No call prints the pairing code back, however wrong the call is: the code is
// a one-time permission to pair a controller with the API, and the controller
// runs as root. The cases are the ways it lands on the command line.
func TestMainKeepsThePairingCodeQuiet(t *testing.T) {
	code := pairCode()
	for _, c := range []struct {
		args []string
		want int
	}{
		{[]string{code}, 2},                          // pasted without a command
		{[]string{"-version=" + code}, 2},            // pasted onto a flag that takes no value
		{[]string{"-config", code}, 1},               // pasted onto the wrong flag: taken as a file name
		{[]string{"pair", code}, 2},                  // pasted behind the command
		{[]string{"pair", "-code", code, "more"}, 2}, // the flag that is gone, one word too many
		{[]string{"pair", "-kode", code}, 2},         // flag mistyped
		{[]string{"pair", "-code=" + code, "-x"}, 2}, // the flag that is gone, unknown flag behind it
		{[]string{"pair", "-code"}, 2},               // the flag that is gone, value missing
	} {
		var out, errOut bytes.Buffer
		if got := Main("1.2.3", c.args, typed(code), &out, &errOut); got != c.want {
			t.Errorf("Main(%q) = %d, want %d", c.args, got, c.want)
		}
		if strings.Contains(out.String()+errOut.String(), code) {
			t.Errorf("Main(%q) printed the pairing code: %q %q", c.args, out.String(), errOut.String())
		}
	}
}

// The type keeps the code quiet, not the care of whoever prints: %+v over the
// options is how a secret slips out.
func TestOptionsHideThePairingCode(t *testing.T) {
	code := pairCode()
	opts, err := parsePair("/etc/ngc/controller.json", nil, typed(code), io.Discard)
	if err != nil {
		t.Fatalf("parsePair: %v", err)
	}
	for _, s := range []string{fmt.Sprintf("%+v", opts), fmt.Sprintf("%s", opts.Pair), opts.Pair.String()} {
		if strings.Contains(s, code) {
			t.Errorf("printed = %q, want the pairing code hidden", s)
		}
	}
	if opts.Pair.Text() != code {
		t.Errorf("Text() = %q, want the pairing code", opts.Pair.Text())
	}
}

func TestParsePairTakesTheCode(t *testing.T) {
	code := pairCode()
	opts, err := parsePair("/etc/ngc/controller.json", nil, typed(code), io.Discard)
	if err != nil {
		t.Fatalf("parsePair: %v", err)
	}
	if opts.Config != "/etc/ngc/controller.json" || opts.Pair.Text() != code {
		t.Fatalf("options = %+v", opts)
	}
}

// The contract says what a pairing code is; parsePair asks it, and nothing
// else gets through.
func TestParsePairRefusesAnythingElse(t *testing.T) {
	for name, in := range map[string]string{
		"nothing":            "",
		"a word":             "ABC\n",
		"the prefix alone":   "ngc-pair:\n",
		"no base64":          "ngc-pair:not-base64!\n",
		"a join code":        "ngc-join:" + strings.TrimPrefix(pairCode(), "ngc-pair:") + "\n",
		"longer than a code": strings.Repeat("ngc-pair:a", 200) + "\n",
	} {
		if _, err := parsePair("/etc/ngc/controller.json", nil, strings.NewReader(in), io.Discard); err == nil {
			t.Errorf("parsePair took %s, want an error", name)
		}
	}
}

func TestParseImageTakesArchAndFile(t *testing.T) {
	opts, out, err := parseImage([]string{"-arch", "amd64", "-out", "build/terminal.tar"})
	if err != nil {
		t.Fatalf("parseImage: %v", err)
	}
	if opts.Arch != "amd64" || out != "build/terminal.tar" {
		t.Fatalf("options = %+v, out = %q", opts, out)
	}
}

// args splits one fuzz input into arguments; a NUL never appears in a real
// argument, so every other byte can be part of one.
func args(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x00")
}

// Neither the arguments nor the standard input is ever named in a failure:
// one of them holds the pairing code.
func FuzzParsePair(f *testing.F) {
	f.Add("", pairCode()+"\n")
	f.Add("", pairCode())
	f.Add("", "  "+pairCode()+"  \nmore\n")
	f.Add("", "ngc-pair:AAAA\n")
	f.Add("-code\x00"+pairCode(), pairCode()+"\n")
	f.Add("extra", pairCode()+"\n")
	f.Add("--", "")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, argv, in string) {
		opts, err := parsePair("/etc/ngc/controller.json", args(argv), strings.NewReader(in), io.Discard)
		if err != nil {
			return
		}
		if _, err := tunnel.ParsePairCode(opts.Pair.Text()); err != nil {
			t.Fatal("parsePair took something that is no pairing code")
		}
		if opts.Config != "/etc/ngc/controller.json" {
			t.Fatalf("parsePair kept %q as the configuration", opts.Config)
		}
	})
}

func FuzzParseImage(f *testing.F) {
	f.Add("-arch\x00amd64\x00-out\x00terminal.tar")
	f.Add("-arch=arm64\x00-out=/tmp/terminal.tar")
	f.Add("-arch\x00amd64\x00-out\x00../terminal.tar")
	f.Add("-arch\x00amd64")
	f.Add("-out\x00terminal.tar")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		opts, out, err := parseImage(args(s))
		if err != nil {
			return
		}
		if !slices.Contains(arches, opts.Arch) || out == "" || len(out) > maxOutPath {
			t.Fatalf("parseImage(%q) = %+v, %q, nil", s, opts, out)
		}
		for _, e := range strings.FieldsFunc(out, isSeparator) {
			if e == ".." {
				t.Fatalf("parseImage(%q) took the climbing path %q", s, out)
			}
		}
	})
}

// testAPI is an API that takes one pairing, and the code that leads to it.
// got holds what it saw once the server is closed; seen counts every request
// that reached it, so a test can show that nothing was sent at all.
type testAPI struct {
	srv  *httptest.Server
	code tunnel.PairCode
	got  *tunnel.PairRequest
	seen *atomic.Int64
}

// newAPI starts such an API with the keys of the test installation.
func newAPI(t *testing.T) testAPI {
	t.Helper()
	apiNoise, _ := trusttest.NodeKeys(0xA1)
	cert, pin := trusttest.APITLS(0xE1, "api-1.test")
	secret := bytes.Repeat([]byte{7}, tunnel.SecretSize)
	var got tunnel.PairRequest
	var seen atomic.Int64
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Add(1)
		b, _ := io.ReadAll(io.LimitReader(r.Body, tunnel.MaxPairRequest+1))
		req, err := tunnel.ParsePairRequest(b)
		if err != nil {
			t.Errorf("ParsePairRequest: %v", err)
		}
		if err := req.Verify(secret, apiNoise.Public); err != nil {
			t.Errorf("Verify: %v", err)
		}
		got = req
		w.WriteHeader(http.StatusNoContent)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	// A handshake this test turned away belongs in no test output.
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return testAPI{srv: srv, got: &got, seen: &seen, code: tunnel.PairCode{
		URL:    "wss://" + strings.TrimPrefix(srv.URL, "https://") + "/tunnel",
		Key:    apiNoise.Public,
		TLS:    pin,
		Secret: secret,
		Anchor: trust.Anchor{Seq: 3, Hash: bytes.Repeat([]byte{9}, sha256.Size)},
	}}
}

// writeController writes controller.json as ulh leaves it, with the API and
// the anchor of an earlier pairing, and returns what it wrote.
func writeController(t *testing.T, path string) *config.Controller {
	t.Helper()
	c := &config.Controller{
		Node:       "ctl-9",
		Name:       "ctl-1",
		NoiseKey:   config.NewSecret(bytes.Repeat([]byte{5}, 32)),
		APIs:       []config.APIRef{{URL: "wss://api-0.test:7443/tunnel", Key: bytes.Repeat([]byte{6}, 32), TLS: bytes.Repeat([]byte{7}, 32)}},
		Anchor:     trust.Anchor{Seq: 1, Hash: bytes.Repeat([]byte{8}, sha256.Size)},
		DB:         "/var/lib/ngc/controller.db",
		DBKey:      config.NewSecret(bytes.Repeat([]byte{9}, 64)),
		ReleaseKey: bytes.Repeat([]byte{10}, 32),
		Roles:      []string{tunnel.RoleServices},
		Podman:     config.DefaultPodman,
	}
	if err := config.SaveController(path, c); err != nil {
		t.Fatalf("SaveController: %v", err)
	}
	return c
}

func TestPairWritesTheConfigurationAndTheAnchor(t *testing.T) {
	api := newAPI(t)
	dir := t.TempDir()
	cfgPath, anchorPath := filepath.Join(dir, "controller.json"), filepath.Join(dir, "trust-anchor.json")
	before := writeController(t, cfgPath)

	var out, errOut bytes.Buffer
	if code := pair(cfgPath, anchorPath, nil, typed(api.code.String()), &out, &errOut); code != 0 {
		t.Fatalf("pair = %d, stderr = %q", code, errOut.String())
	}
	api.srv.Close() // Close waits for the request, so what it saw is here

	cfg, err := config.LoadController(cfgPath)
	if err != nil {
		t.Fatalf("LoadController: %v", err)
	}
	if len(cfg.APIs) != 1 || cfg.APIs[0].URL != api.code.URL ||
		!bytes.Equal(cfg.APIs[0].Key, api.code.Key) || !bytes.Equal(cfg.APIs[0].TLS, api.code.TLS) {
		t.Errorf("apis = %+v, want the API of the code", cfg.APIs)
	}
	if cfg.Anchor.Seq != api.code.Anchor.Seq || !bytes.Equal(cfg.Anchor.Hash, api.code.Anchor.Hash) {
		t.Errorf("anchor = %+v, want the anchor of the code", cfg.Anchor)
	}
	// The node ID named this server under its old key in another trust list.
	if cfg.Node != "" {
		t.Errorf("node = %q, want it cleared", cfg.Node)
	}
	// writeController leaves a server that was listed and had an API: this is
	// a second pairing. It drops both, so the admin is told what to do now.
	if got := errOut.String(); got != msgListAgain+"\n" {
		t.Errorf("stderr = %q, want %q", got, msgListAgain+"\n")
	}
	if bytes.Equal(cfg.NoiseKey.Bytes(), before.NoiseKey.Bytes()) {
		t.Error("the key of this controller stayed the same, want a new one")
	}
	if cfg.Name != before.Name || !slices.Equal(cfg.Roles, before.Roles) || cfg.DB != before.DB ||
		!bytes.Equal(cfg.DBKey.Bytes(), before.DBKey.Bytes()) || !bytes.Equal(cfg.ReleaseKey, before.ReleaseKey) {
		t.Errorf("pairing changed what belongs to this server: %+v", cfg)
	}
	a, err := config.LoadAnchor(anchorPath)
	if err != nil {
		t.Fatalf("LoadAnchor: %v", err)
	}
	if a.Seq != api.code.Anchor.Seq || !bytes.Equal(a.Hash, api.code.Anchor.Hash) {
		t.Errorf("anchor file = %+v, want the anchor of the code", a)
	}
	// Both lines the admin compares with the display of the shell, over the
	// key the API really got.
	want := "Fingerabdruck dieses Controllers: " + tunnel.Fingerprint(api.got.Noise) + "\n" +
		"Anker: Fassung 3, " + tunnel.Fingerprint(api.code.Anchor.Hash) + "\n"
	if out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	if strings.Contains(out.String()+errOut.String(), api.code.String()) {
		t.Error("the output carried the pairing code")
	}
}

// Without controller.json this server knows neither its name nor its roles,
// database and release key, and nothing could be written after a pairing. It
// ends before the one-time code is spent, and it names no path: the path may
// be the code itself (-config).
func TestPairNeedsTheConfiguration(t *testing.T) {
	api := newAPI(t)
	dir := t.TempDir()
	cfgPath, anchorPath := filepath.Join(dir, "controller.json"), filepath.Join(dir, "trust-anchor.json")

	var out, errOut bytes.Buffer
	if code := pair(cfgPath, anchorPath, nil, typed(api.code.String()), &out, &errOut); code != 1 {
		t.Fatalf("pair = %d, want 1", code)
	}
	if n := api.seen.Load(); n != 0 {
		t.Errorf("the API saw %d requests, want none", n)
	}
	if _, err := os.Stat(anchorPath); !os.IsNotExist(err) {
		t.Errorf("os.Stat(anchor) = %v, want it not written", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want empty", out.String())
	}
	if strings.Contains(errOut.String(), cfgPath) || strings.Contains(errOut.String(), api.code.String()) {
		t.Errorf("stderr = %q, want neither the path nor the code in it", errOut.String())
	}
}

// A pairing that fails changes nothing: the server keeps the API it had.
func TestPairLeavesTheConfigurationAloneWhenItFails(t *testing.T) {
	api := newAPI(t)
	api.srv.Close() // nobody answers any more
	dir := t.TempDir()
	cfgPath, anchorPath := filepath.Join(dir, "controller.json"), filepath.Join(dir, "trust-anchor.json")
	writeController(t, cfgPath)
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := pair(cfgPath, anchorPath, nil, typed(api.code.String()), &out, &errOut); code != 1 {
		t.Fatalf("pair = %d, want 1", code)
	}
	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("controller.json changed although the pairing failed")
	}
	if _, err := os.Stat(anchorPath); !os.IsNotExist(err) {
		t.Errorf("os.Stat(anchor) = %v, want it not written", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want empty", out.String())
	}
	if strings.Contains(errOut.String(), api.code.String()) {
		t.Errorf("stderr = %q, want the pairing code kept out of it", errOut.String())
	}
}

// The pairing code no longer travels on the command line, so no text of the
// program may send an admin there: an argument stands in /proc/<pid>/cmdline,
// which every local user may read without hidepid. The cases are named, never
// printed: one of them carries a code.
func TestNoTextSendsTheCodeToTheCommandLine(t *testing.T) {
	code := pairCode()
	for _, c := range []struct {
		name string
		args []string
	}{
		{"-h", []string{"-h"}},
		{"pair with the old flag", []string{"pair", "-code", code}},
		{"pair with a pasted code", []string{"pair", code}},
	} {
		var out, errOut bytes.Buffer
		Main("1.2.3", c.args, typed(code), &out, &errOut)
		if got := out.String() + errOut.String(); strings.Contains(got, "-code") {
			t.Errorf("%s: the answer names the flag -code: %q", c.name, got)
		}
	}
}

// pairOnce runs a pairing that works and returns what the admin reads on
// stderr afterwards: the one line that says what is left to do.
func pairOnce(t *testing.T) string {
	t.Helper()
	api := newAPI(t)
	dir := t.TempDir()
	cfgPath, anchorPath := filepath.Join(dir, "controller.json"), filepath.Join(dir, "trust-anchor.json")
	writeController(t, cfgPath)
	var out, errOut bytes.Buffer
	if code := pair(cfgPath, anchorPath, nil, typed(api.code.String()), &out, &errOut); code != 0 {
		t.Fatalf("pair = %d, stderr = %q", code, errOut.String())
	}
	return errOut.String()
}

// The two ways that end badly open with "Die Kopplung hat geklappt, aber"; the
// way that worked has to say so as well, or its one line reads like the report
// of a failure. The two lines on stdout are fingerprints to compare, not an
// answer to that question.
func TestPairSaysThatItWorked(t *testing.T) {
	worked, _, _ := strings.Cut(msgNotSaved, ",")
	if got := pairOnce(t); !strings.HasPrefix(got, worked) {
		t.Errorf("stderr = %q, want it to open with %q", got, worked)
	}
}

// What is left to do, in words that hold wherever the listing ends up sitting.
// The first pairing of a server never stood in a list, so "neu eintragen" is
// wrong there; and "die Verwaltung von NextGenCloud" is no place an admin can
// find, because the contract knows only ngc.users/nodes and no menu.
func TestPairSaysWhatIsLeftToDo(t *testing.T) {
	got := pairOnce(t)
	for _, bad := range []string{"neu eintragen", "Verwaltung"} {
		if strings.Contains(got, bad) {
			t.Errorf("stderr = %q, want %q out of it", got, bad)
		}
	}
	for _, want := range []string{"Administrator", "eintragen"} {
		if !strings.Contains(got, want) {
			t.Errorf("stderr = %q, want %q in it", got, want)
		}
	}
}
