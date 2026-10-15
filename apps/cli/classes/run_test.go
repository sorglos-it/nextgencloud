package classes

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
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
	// Nothing is "not built yet" any more: Task 29 filled in the last two,
	// release and check. The list this case used to walk is empty, so what
	// it now holds is that no subcommand answers with that sentence and that
	// a bare name is a call of the wrong shape (exit 2), not a missing
	// feature (exit 1).
	for _, name := range []string{"pack", "encrypt", "catalog", "license", "release", "check"} {
		var stdout, stderr bytes.Buffer
		if code := Run([]string{name}, &stdout, &stderr); code != 2 {
			t.Errorf("%s: exit %d, want 2 for a call without arguments", name, code)
		}
		if strings.Contains(stderr.String(), "noch nicht gebaut") {
			t.Errorf("%s says it is not built: %q", name, stderr.String())
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
	// cannot promise a call that does not exist. Each one is called with as
	// many arguments as it takes: a call of the wrong shape is a 2 for every
	// subcommand, so one shape for all of them would prove nothing.
	// The paths are all inside a temporary directory: license creates a
	// signing key file when none is there, and a test must not leave one in
	// the work tree (it did, once — a file named "c" with a seed in it).
	tmp := t.TempDir()
	in := func(name string) string { return filepath.Join(tmp, name) }
	for _, c := range []struct {
		name string
		args []string
	}{
		{"pack", []string{"pack", in("x"), in("y")}},
		{"encrypt", []string{"encrypt", in("x"), in("y")}},
		{"catalog", []string{"catalog", in("x"), in("y"), "teststore", "1"}},
		{"license", []string{"license", "a", "b", in("c"), in("d"), "e", "f"}},
		{"release", []string{"release", "-version", "1.1.0", "-seq", "1", in("x"), in("y")}},
		{"check", []string{"check", in("x"), "ngc-web", "all"}},
	} {
		if !strings.Contains(Usage, "ngc-store "+c.name+" ") {
			t.Errorf("the usage text does not name %s", c.name)
		}
		var stdout, stderr bytes.Buffer
		if code := Run(c.args, &stdout, &stderr); code == 2 {
			t.Errorf("%s is named in the usage text but not understood", c.name)
		}
	}
	// And the two built ones give a 2 for a call of the wrong shape.
	for _, args := range [][]string{{"encrypt"}, {"encrypt", "one"}, {"encrypt", "a", "b", "c"},
		{"license"}, {"license", "a", "b", "c", "d", "e"}, {"license", "a", "b", "c", "d", "e", "f", "g"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr); code != 2 {
			t.Errorf("%q: exit %d", args, code)
		}
	}
}

func TestRunEncryptsAndCreatesItsKeyFile(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "app.ngc")
	write(t, plain, []byte("package"))
	keyFile := filepath.Join(dir, "content.key")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"encrypt", plain, keyFile}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, %s", code, stderr.String())
	}
	// The name is the package's own with ".enc" behind it, written out here
	// on purpose: Catalog of Task 28 looks for exactly this file beside the
	// plain package (its EncSuffix), and a name taken from a variable would
	// not say which one that is.
	sealed := filepath.Join(dir, "app.ngc.enc")
	if sealed != plain+".enc" {
		t.Fatalf("the test disagrees with itself: %q", sealed)
	}
	if _, err := os.Stat(sealed); err != nil {
		t.Fatalf("no encrypted package: %v", err)
	}
	if !strings.Contains(stdout.String(), "angelegt") || !strings.Contains(stdout.String(), sealed) {
		t.Errorf("stdout %q", stdout.String())
	}
	// The second call uses the key that is there; it does not make a new one
	// and the first package stays readable with it.
	before := read(t, keyFile)
	stdout.Reset()
	if code := Run([]string{"encrypt", plain, keyFile}, &stdout, &stderr); code != 0 {
		t.Fatalf("second call: exit %d, %s", code, stderr.String())
	}
	if !bytes.Equal(read(t, keyFile), before) {
		t.Error("the second call changed the key file")
	}
	if strings.Contains(stdout.String(), "angelegt") {
		t.Errorf("the second call made a key: %q", stdout.String())
	}
}

func TestRunRefusesAPackageThatIsNoNGC(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "app.tar")
	write(t, plain, []byte("package"))
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"encrypt", plain, filepath.Join(dir, "c.key")}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stderr.String(), ".ngc") {
		t.Errorf("stderr %q", stderr.String())
	}
	// Nothing was created on the way, not even the key file.
	if _, err := os.Stat(filepath.Join(dir, "c.key")); err == nil {
		t.Error("a refused call left a key file behind")
	}
}

func TestRunSignsALicence(t *testing.T) {
	f := newLicenseFixture(t)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"license", "-issued", "1790000000", "-per", "user", "-users", "3",
		f.app, f.installation, f.keyFile, f.contentKeyFile, f.installKey, f.st.ID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, %s", code, stderr.String())
	}
	var env sign.Envelope
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &env); err != nil {
		t.Fatalf("the output is no envelope: %v, %q", err, stdout.String())
	}
	l := f.verifyOK(t, env)
	if l.Per != store.PerUser || l.Users != 3 || l.Issued != 1790000000 {
		t.Fatalf("licence %+v", l)
	}
}

func TestRunCreatesASigningKeyAndNamesOnlyItsPublicHalf(t *testing.T) {
	dir := t.TempDir()
	contentKeyFile := filepath.Join(dir, "content.key")
	if _, err := NewContentKey(contentKeyFile); err != nil {
		t.Fatal(err)
	}
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(dir, "store.key")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"license", "-issued", "1790000000", "org.sorglos.terminal", testID(t),
		keyFile, contentKeyFile, enc.B64.EncodeToString(priv.PublicKey().Bytes()), "test"},
		&stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, %s", code, stderr.String())
	}
	seed := strings.TrimSpace(string(read(t, keyFile)))
	if !strings.Contains(stdout.String(), "Öffentlicher Schlüssel") {
		t.Errorf("stdout %q", stdout.String())
	}
	if strings.Contains(stdout.String(), seed) {
		t.Error("the private seed was printed")
	}
	raw, err := enc.B64.DecodeString(seed)
	if err != nil {
		t.Fatalf("the key file is no seed: %v", err)
	}
	if leaks(stdout.String(), raw) || leaks(stderr.String(), raw) {
		t.Error("the seed shows in the output")
	}
}

// TestEverySigningSubcommandCreatesAMissingKeyFile: whoever signs their first
// catalog has no key yet, and the tool makes one instead of failing with the
// bare os.Open error. catalog was the odd one out until 12.10.2026 — encrypt,
// license and release already did it — so the case walks all four rather than
// catalog alone, and a fifth subcommand that signs will fail here until it
// joins them. What is printed is the path and the public half; the key itself
// is never printed, which TestNoKeyReachesTheOutput measures.
func TestEverySigningSubcommandCreatesAMissingKeyFile(t *testing.T) {
	for _, c := range []struct {
		name string
		args func(dir, key string) []string
	}{
		{"catalog", func(dir, key string) []string { return []string{"catalog", dir, key, "teststore", "1"} }},
		{"encrypt", func(dir, key string) []string { return []string{"encrypt", filepath.Join(dir, "a.ngc"), key} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			key := filepath.Join(dir, "store.key")
			var stdout, stderr bytes.Buffer
			Run(c.args(dir, key), &stdout, &stderr)
			// The call may still fail for want of a package or a catalog —
			// what this case measures is the key file, not the subcommand.
			if _, err := os.Stat(key); err != nil {
				t.Fatalf("%s left no key file: %v (stderr %q)", c.name, err, stderr.String())
			}
			if !strings.Contains(stdout.String(), key) {
				t.Errorf("%s does not name the key file it made: %q", c.name, stdout.String())
			}
		})
	}
}
