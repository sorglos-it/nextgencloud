package node

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

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

// Commands that are still to come answer with exit 1 and say so in German, so
// an admin sees the difference between "wrong call" and "not built yet".
func TestMainCommandsNotBuiltYet(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"pair", "-code", "ABC"},
		{"terminal"},
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

// A flag stands before the command; the command keeps its own flags.
func TestMainConfigFlagBeforeCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	args := []string{"-config", "controller.json", "pair", "-code", "ABC"}
	if code := Main("1.2.3", args, &out, &errOut); code != 1 {
		t.Fatalf("exit code = %d, want 1 (the command was reached)", code)
	}
}

// "image" only takes an architecture NGC is built for: the value becomes an
// argument of another program later.
func TestMainImageNeedsKnownArchAndFile(t *testing.T) {
	for _, args := range [][]string{
		{"image"},
		{"image", "-arch", "x86", "-out", "terminal.tar"},
		{"image", "-arch", "amd64"},
		{"image", "-arch", "amd64", "-out", "terminal.tar", "more"},
	} {
		var out, errOut bytes.Buffer
		if code := Main("1.2.3", args, &out, &errOut); code != 2 {
			t.Errorf("Main(%q) = %d, want 2", args, code)
		}
	}
}

// Secrets never reach the screen, not even when the call is wrong: a pairing
// code is one, and it is easy to mistype.
func TestMainKeepsThePairingCodeQuiet(t *testing.T) {
	const secret = "KOPPELGEHEIMNIS"
	for _, args := range [][]string{
		{"pair", "-code", secret, "extra"},
		{"pair", "-kode", secret},
		{"pair", "-code=" + secret, "-nope"},
		{"pair", "-code"},
	} {
		var out, errOut bytes.Buffer
		if code := Main("1.2.3", args, &out, &errOut); code != 2 {
			t.Errorf("Main(%q) = %d, want 2", args, code)
		}
		if strings.Contains(out.String()+errOut.String(), secret) {
			t.Errorf("Main(%q) printed the pairing code: %q %q", args, out.String(), errOut.String())
		}
	}
}

func TestParsePairTakesTheCode(t *testing.T) {
	opts, err := parsePair("/etc/ngc/controller.json", []string{"-code", "ABC"})
	if err != nil {
		t.Fatalf("parsePair: %v", err)
	}
	if opts.Config != "/etc/ngc/controller.json" || opts.Pair != "ABC" {
		t.Fatalf("options = %+v", opts)
	}
}

func TestParseImageTakesArchAndFile(t *testing.T) {
	opts, out, err := parseImage([]string{"-arch", "amd64", "-out", "terminal.tar"})
	if err != nil {
		t.Fatalf("parseImage: %v", err)
	}
	if opts.Arch != "amd64" || out != "terminal.tar" {
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
	f.Add("-code\x00ABC")
	f.Add("-code=ABC")
	f.Add("-code\x00ABC\x00extra")
	f.Add("--\x00-code\x00ABC")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		opts, err := parsePair("/etc/ngc/controller.json", args(s))
		if err != nil {
			return
		}
		if opts.Pair == "" || opts.Config != "/etc/ngc/controller.json" {
			t.Fatalf("parsePair(%q) = %+v, nil", s, opts)
		}
	})
}

func FuzzParseImage(f *testing.F) {
	f.Add("-arch\x00amd64\x00-out\x00terminal.tar")
	f.Add("-arch=arm64\x00-out=/tmp/terminal.tar")
	f.Add("-arch\x00amd64")
	f.Add("-out\x00terminal.tar")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		opts, out, err := parseImage(args(s))
		if err != nil {
			return
		}
		if !slices.Contains(arches, opts.Arch) || out == "" {
			t.Fatalf("parseImage(%q) = %+v, %q, nil", s, opts, out)
		}
	})
}
