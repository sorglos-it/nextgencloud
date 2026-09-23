package node

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/trust"
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
	if code := Main("1.2.3", []string{"-version"}, &out, &errOut); code != 0 {
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
	if code := Main("1.2.3", []string{"frobnicate"}, &out, &errOut); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "frobnicate") {
		t.Fatalf("stderr = %q, want the unknown command named", errOut.String())
	}
}

func TestMainPairNeedsCode(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Main("1.2.3", []string{"pair"}, &out, &errOut); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

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
		if code := Main("1.2.3", []string{arg}, &out, &errOut); code != 2 {
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
		{"pair", "-code", pairCode()},
		{"terminal"},
		{"hold"},
		{"image", "-arch", "arm64", "-out", "terminal-arm64.tar"},
	} {
		var out, errOut bytes.Buffer
		if code := Main("1.2.3", args, &out, &errOut); code != 1 {
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
	if code := Main("1.2.3", []string{"-h"}, &out, &errOut); code != 0 {
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

// A flag stands before the command; the command keeps its own flags.
func TestMainConfigFlagBeforeCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	args := []string{"-config", "controller.json", "pair", "-code", pairCode()}
	if code := Main("1.2.3", args, &out, &errOut); code != 1 {
		t.Fatalf("exit code = %d, want 1 (the command was reached)", code)
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
		if code := Main("1.2.3", args, &out, &errOut); code != 2 {
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
		{[]string{"pair", "-code", code, "more"}, 2}, // one word too many
		{[]string{"pair", "-kode", code}, 2},         // flag mistyped
		{[]string{"pair", "-code=" + code, "-x"}, 2}, // unknown flag behind it
		{[]string{"pair", "-code"}, 2},               // value missing
	} {
		var out, errOut bytes.Buffer
		if got := Main("1.2.3", c.args, &out, &errOut); got != c.want {
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
	opts, err := parsePair("/etc/ngc/controller.json", []string{"-code", code})
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
	opts, err := parsePair("/etc/ngc/controller.json", []string{"-code", code})
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
	for _, code := range []string{
		"",
		"ABC",
		"ngc-pair:",
		"ngc-pair:not-base64!",
		"ngc-join:" + strings.TrimPrefix(pairCode(), "ngc-pair:"),
		strings.Repeat("ngc-pair:a", 200),
	} {
		if _, err := parsePair("/etc/ngc/controller.json", []string{"-code", code}); err == nil {
			t.Errorf("parsePair(%q) = nil, want an error", code)
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

func FuzzParsePair(f *testing.F) {
	f.Add("-code\x00" + pairCode())
	f.Add("-code=" + pairCode())
	f.Add("-code\x00" + pairCode() + "\x00extra")
	f.Add("-code\x00ngc-pair:AAAA")
	f.Add("--\x00-code\x00ABC")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		opts, err := parsePair("/etc/ngc/controller.json", args(s))
		if err != nil {
			return
		}
		if _, err := tunnel.ParsePairCode(opts.Pair.Text()); err != nil {
			t.Fatalf("parsePair(%q) took %v, which is no pairing code", s, opts.Pair)
		}
		if opts.Config != "/etc/ngc/controller.json" {
			t.Fatalf("parsePair(%q) = %+v", s, opts)
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
