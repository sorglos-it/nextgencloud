package classes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha256Of(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

func TestRunPacks(t *testing.T) {
	dir := writeApp(t)
	out := filepath.Join(t.TempDir(), "app.ngc")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"pack", dir, out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, %s", code, stderr.String())
	}
	want := "org.sorglos.demo 1.2.3\n" + hex.EncodeToString(sha256Of(read(t, out))) + "\n"
	if stdout.String() != want {
		t.Errorf("stdout %q, want %q", stdout.String(), want)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr %q", stderr.String())
	}
}

func TestRunReportsAPackThatFailed(t *testing.T) {
	out := filepath.Join(t.TempDir(), "app.ngc")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"pack", t.TempDir(), out}, &stdout, &stderr); code != 1 {
		t.Errorf("exit %d", code)
	}
	if !strings.Contains(stderr.String(), "nicht geschrieben") || stdout.Len() != 0 {
		t.Errorf("stdout %q, stderr %q", stdout.String(), stderr.String())
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("a failed pack left a file behind")
	}
}

func TestRunKnowsItsSubcommands(t *testing.T) {
	// The five that are not built yet: the name is known, so the call says
	// what is missing instead of looking like a typo (exit 1, not 2).
	for _, name := range []string{"encrypt", "catalog", "license", "release", "check"} {
		var stdout, stderr bytes.Buffer
		if code := Run([]string{name}, &stdout, &stderr); code != 1 {
			t.Errorf("%s: exit %d", name, code)
		}
		if !strings.Contains(stderr.String(), name) || !strings.Contains(stderr.String(), "noch nicht gebaut") {
			t.Errorf("%s: stderr %q", name, stderr.String())
		}
	}
}

func TestRunPrintsTheUsageForAWrongCall(t *testing.T) {
	for _, args := range [][]string{nil, {"pack"}, {"pack", "one"}, {"pack", "one", "two", "three"},
		{"unpack"}, {"--version"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr); code != 2 {
			t.Errorf("%q: exit %d", args, code)
		}
		if stderr.String() != Usage || stdout.Len() != 0 {
			t.Errorf("%q: stdout %q, stderr %q", args, stdout.String(), stderr.String())
		}
	}
	// Every subcommand the usage text names is one Run answers, so the text
	// cannot promise a call that does not exist.
	for _, name := range []string{"pack", "encrypt", "catalog", "license", "release", "check"} {
		if !strings.Contains(Usage, "ngc-store "+name+" ") {
			t.Errorf("the usage text does not name %s", name)
		}
		var stdout, stderr bytes.Buffer
		if code := Run([]string{name, "x", "y"}, &stdout, &stderr); code == 2 {
			t.Errorf("%s is named in the usage text but not understood", name)
		}
	}
}
