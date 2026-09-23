package node

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// A pipe, a file and every reader of a test are no terminal. The null device
// is a character device, as a terminal is, and that is what the check looks
// at: nothing else tells the two apart on every system NGC is tested on.
func TestTerminalKnowsWhatItReadsFrom(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	file, err := os.Create(filepath.Join(t.TempDir(), "code.txt"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	for name, in := range map[string]io.Reader{
		"a pipe":   r,
		"a file":   file,
		"a reader": strings.NewReader("x"),
	} {
		if _, ok := terminal(in); ok {
			t.Errorf("%s counts as a terminal", name)
		}
	}
	dev, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dev.Close() })
	if _, ok := terminal(dev); !ok {
		t.Error("a character device is no terminal here, want it taken for one")
	}
}

// A script hands the code over on the standard input: the first line is the
// code, nothing is asked, and what stands behind it is left alone.
func TestReadCodeTakesTheFirstLineOfAPipe(t *testing.T) {
	code := pairCode()
	var prompt bytes.Buffer
	got, err := readCode(strings.NewReader(code+"\nand another line\n"), &prompt)
	if err != nil {
		t.Fatalf("readCode: %v", err)
	}
	if got != code {
		t.Error("readCode did not return the line it was given")
	}
	if prompt.Len() != 0 {
		t.Errorf("prompt = %q, want nothing asked of a script", prompt.String())
	}
}

// The last line of a file needs no newline behind it.
func TestReadCodeTakesALineWithoutAnEnd(t *testing.T) {
	code := pairCode()
	got, err := readCode(strings.NewReader(code), io.Discard)
	if err != nil || got != code {
		t.Fatalf("readCode = %v, want the line it was given", err)
	}
}

// At a terminal the admin is asked. The question is all that is written: what
// is typed goes to no writer at all.
func TestReadCodeAsksAtATerminal(t *testing.T) {
	dev, err := os.Open(os.DevNull) // a character device, as a terminal is
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	var prompt bytes.Buffer
	if _, err := readCode(dev, &prompt); err != nil {
		t.Fatalf("readCode: %v", err)
	}
	if !strings.HasPrefix(prompt.String(), askCode) {
		t.Errorf("prompt = %q, want it to start with %q", prompt.String(), askCode)
	}
}

// endless is an input without an end: it never sends a newline. It counts what
// was taken and stops well before the memory runs out, so a missing bound
// shows up as a failed test and not as a test that never ends.
type endless struct{ read int }

func (e *endless) Read(p []byte) (int, error) {
	if e.read >= 8*maxCodeLine {
		return 0, io.EOF
	}
	for i := range p {
		p[i] = 'a'
	}
	e.read += len(p)
	return len(p), nil
}

// The line is bounded before it is read: the controller runs as root, and an
// endless line must not fill its memory.
func TestReadCodeBoundsTheLine(t *testing.T) {
	var in endless
	got, err := readCode(&in, io.Discard)
	if err == nil {
		t.Error("readCode took an endless line, want an error")
	}
	if got != "" {
		t.Error("readCode returned what it read of an endless line")
	}
	if in.read > maxCodeLine {
		t.Errorf("readCode read %d bytes, want at most %d", in.read, maxCodeLine)
	}
}

// -code is gone. Whoever still gives it is turned away before anything is read
// at all, and never gets the value back: that value may be the code.
func TestParsePairRefusesTheOldFlag(t *testing.T) {
	code := pairCode()
	for name, args := range map[string][]string{
		"-code with the code":  {"-code", code},
		"-code=code":           {"-code=" + code},
		"--code with the code": {"--code", code},
		"-code without value":  {"-code"},
		"-code behind -x":      {"-x", "-code", code},
	} {
		in := strings.NewReader(code + "\n")
		var prompt bytes.Buffer
		opts, err := parsePair("/etc/ngc/controller.json", args, in, &prompt)
		if !errors.Is(err, errCodeFlag) {
			t.Errorf("%s: parsePair = %v, want the answer about the flag", name, err)
		}
		if !opts.Pair.IsZero() {
			t.Errorf("%s: parsePair kept a code", name)
		}
		if prompt.Len() != 0 || in.Len() != len(code)+1 {
			t.Errorf("%s: parsePair asked or read although the call is wrong", name)
		}
	}
}

// The admin who still calls the old way is told where the code goes now, in
// one fixed German line, and the code is nowhere in the answer.
func TestMainTellsWhereTheCodeGoesNow(t *testing.T) {
	code := pairCode()
	var out, errOut bytes.Buffer
	if got := Main("1.2.3", []string{"pair", "-code", code}, typed(code), &out, &errOut); got != 2 {
		t.Fatalf("Main = %d, want 2", got)
	}
	if got := errOut.String(); got != msgCodeFlag+"\n" {
		t.Errorf("stderr = %q, want %q", got, msgCodeFlag+"\n")
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want empty", out.String())
	}
}

// childEnv marks the process TestPairKeepsTheCodeOutOfTheCommandLine starts,
// and the two others carry the files it works in. The pairing code is not
// among them: it goes over the standard input alone.
const (
	childEnv    = "NGC_TEST_PAIR_CHILD"
	childConfig = "NGC_TEST_PAIR_CONFIG"
	childAnchor = "NGC_TEST_PAIR_ANCHOR"
)

// A real pairing in a real process, with the code on its standard input: the
// command line of that process carries no code. os.Args is what
// /proc/<pid>/cmdline holds, and every local user may read that file as long
// as the kernel is mounted without hidepid.
func TestPairKeepsTheCodeOutOfTheCommandLine(t *testing.T) {
	api := newAPI(t)
	dir := t.TempDir()
	cfgPath, anchorPath := filepath.Join(dir, "controller.json"), filepath.Join(dir, "trust-anchor.json")
	writeController(t, cfgPath)

	cmd := exec.Command(os.Args[0], "-test.run=^TestPairInAnotherProcess$", "-test.v")
	cmd.Env = append(os.Environ(), childEnv+"=1", childConfig+"="+cfgPath, childAnchor+"="+anchorPath)
	cmd.Stdin = strings.NewReader(api.code.String() + "\n")
	out, err := cmd.CombinedOutput()
	// Whatever went wrong there, none of it may be shown before this: the
	// output of that process is the one place a code could still turn up.
	if bytes.Contains(out, []byte(tunnel.PairPrefix)) {
		t.Fatal("the other process wrote a pairing code")
	}
	if err != nil {
		t.Fatalf("the other process failed: %v\n%s", err, out)
	}
	api.srv.Close() // Close waits for the request, so what it saw is here
	if n := api.seen.Load(); n != 1 {
		t.Fatalf("the API saw %d requests, want exactly one", n)
	}

	cfg, err := config.LoadController(cfgPath)
	if err != nil {
		t.Fatalf("LoadController: %v", err)
	}
	if len(cfg.APIs) != 1 || cfg.APIs[0].URL != api.code.URL || !bytes.Equal(cfg.APIs[0].Key, api.code.Key) {
		t.Errorf("apis = %+v, want the API of the code", cfg.APIs)
	}
	// The pairing made a new key for this server, and the API saw its public
	// half: the request really came out of that other process.
	if bytes.Equal(cfg.NoiseKey.Bytes(), bytes.Repeat([]byte{5}, 32)) {
		t.Error("the key of this controller stayed the same, want a new one")
	}
	if len(api.got.Noise) != 32 {
		t.Errorf("the API got a key of %d bytes, want 32", len(api.got.Noise))
	}
	a, err := config.LoadAnchor(anchorPath)
	if err != nil {
		t.Fatalf("LoadAnchor: %v", err)
	}
	if a.Seq != api.code.Anchor.Seq || !bytes.Equal(a.Hash, api.code.Anchor.Hash) {
		t.Errorf("anchor file = %+v, want the anchor of the code", a)
	}
}

// TestPairInAnotherProcess is the process the test above starts. It is no test
// of its own: without the mark in the environment it does nothing.
func TestPairInAnotherProcess(t *testing.T) {
	if os.Getenv(childEnv) == "" {
		t.Skip("started by TestPairKeepsTheCodeOutOfTheCommandLine alone")
	}
	// This is what /proc/<pid>/cmdline of this process holds.
	if line := strings.Join(os.Args, "\x00"); strings.Contains(line, tunnel.PairPrefix) ||
		strings.Contains(line, tunnel.JoinPrefix) {
		t.Fatal("the command line of this process carries a code")
	}
	for _, a := range os.Args {
		if _, err := tunnel.ParsePairCode(a); err == nil {
			t.Fatal("an argument of this process is a pairing code")
		}
	}
	if code := pair(os.Getenv(childConfig), os.Getenv(childAnchor), nil,
		os.Stdin, os.Stdout, os.Stderr); code != 0 {
		t.Fatalf("pair = %d", code)
	}
}
