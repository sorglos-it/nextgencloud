package classes

import (
	"bytes"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/store"
)

func TestEncryptRoundTrip(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "app.ngc")
	if err := os.WriteFile(plain, bytes.Repeat([]byte("a"), 3*store.ChunkSize+7), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	keyFile := filepath.Join(dir, "content.key")
	key, err := NewContentKey(keyFile)
	if err != nil || len(key) != store.KeySize {
		t.Fatalf("key: %v", err)
	}
	if info, _ := os.Stat(keyFile); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %v", info.Mode().Perm())
	} else if runtime.GOOS == "windows" {
		// LOUD, NO RESULT: Go maps a file mode on Windows to the read-only
		// attribute alone, so 0600 cannot be read back here. Measured:
		// os.Stat reports the mode below. NewContentKey asks for 0600 and
		// O_EXCL in one step; that the bits arrive is measured on Unix only.
		t.Logf("NOT MEASURED on windows: key file mode is %v, not 0600", info.Mode().Perm())
	}
	sealed := filepath.Join(dir, "app.enc")
	if err := Encrypt(plain, sealed, keyFile); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	f, err := os.Open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	r, err := store.NewDecrypter(f, key)
	if err != nil {
		t.Fatalf("decrypter: %v", err)
	}
	back, err := io.ReadAll(r)
	if err != nil || len(back) != 3*store.ChunkSize+7 {
		t.Fatalf("read back: %v %d", err, len(back))
	}
	if !bytes.Equal(back, bytes.Repeat([]byte("a"), 3*store.ChunkSize+7)) {
		t.Fatal("the plaintext came back changed")
	}
	// Nothing of the package shows through.
	raw, _ := os.ReadFile(sealed)
	if bytes.Contains(raw, bytes.Repeat([]byte("a"), 64)) {
		t.Fatal("the plaintext shows in the encrypted file")
	}
	// Nor does the key itself, in any of its shapes.
	for _, s := range keyShapes(key) {
		if bytes.Contains(raw, []byte(s)) {
			t.Fatal("the content key stands in the encrypted file")
		}
	}
}

func TestDecryptWithAnotherKeyFails(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "app.ngc")
	if err := os.WriteFile(plain, []byte("package"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	keyFile := filepath.Join(dir, "content.key")
	key, err := NewContentKey(keyFile)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	sealed := filepath.Join(dir, "app.enc")
	if err := Encrypt(plain, sealed, keyFile); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// Intermediate check: with the right key the way is open, so the refusal
	// below is the wrong key and not a file that cannot be read at all.
	f0, err := os.Open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	r0, err := store.NewDecrypter(f0, key)
	if err != nil {
		f0.Close()
		t.Fatalf("the right key does not open the file: %v", err)
	}
	if b, err := io.ReadAll(r0); err != nil || string(b) != "package" {
		f0.Close()
		t.Fatalf("the right key gives %q, %v", b, err)
	}
	f0.Close()

	f, err := os.Open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	other := bytes.Repeat([]byte{3}, store.KeySize)
	r, err := store.NewDecrypter(f, other)
	if err != nil {
		// A wrong key may already fail here; if it does not, reading must.
		return
	}
	if _, err := io.ReadAll(r); err == nil {
		t.Fatal("another key opened the package")
	}
}

func TestEncryptIsNotReproducibleOnPurpose(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "app.ngc")
	if err := os.WriteFile(plain, []byte("package"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	keyFile := filepath.Join(dir, "content.key")
	if _, err := NewContentKey(keyFile); err != nil {
		t.Fatalf("key: %v", err)
	}
	one := filepath.Join(dir, "one.enc")
	two := filepath.Join(dir, "two.enc")
	if err := Encrypt(plain, one, keyFile); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := Encrypt(plain, two, keyFile); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	a, b := read(t, one), read(t, two)
	if len(a) != len(b) {
		t.Fatalf("the two runs differ in length: %d, %d", len(a), len(b))
	}
	// The package itself is reproducible byte for byte (TestPackIsReproducible);
	// the encryption must not be. The same file ID twice would mean the same
	// nonce for the same chunk number under the same key.
	if bytes.Equal(a, b) {
		t.Fatal("two runs gave the same bytes: the file ID is not fresh")
	}
	head := len("NGCENC1\n")
	if !bytes.Equal(a[:head], b[:head]) {
		t.Fatal("the magic differs between two runs")
	}
	if bytes.Equal(a[head:head+store.FileIDSize], b[head:head+store.FileIDSize]) {
		t.Fatal("the file ID repeated")
	}
}

func TestNewContentKeyNeverOverwritesAKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "content.key")
	first, err := NewContentKey(path)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	before := read(t, path)
	if _, err := NewContentKey(path); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("second key: %v", err)
	}
	if !bytes.Equal(read(t, path), before) {
		t.Fatal("the key file changed")
	}
	// And it is really the key that was written, so the refusal above
	// protected something.
	again, err := readContentKey(path)
	if err != nil || !bytes.Equal(again, first) {
		t.Fatalf("read back: %v", err)
	}
}

func TestEncryptRefusesAFileThatIsNoKey(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "app.ngc")
	write(t, plain, []byte("package"))
	cases := []struct {
		name string
		body []byte
		want error
	}{
		{"missing", nil, ErrKeyMissing},
		{"empty", []byte(""), ErrKeyFile},
		{"not base64url", []byte(strings.Repeat("+", 43)), ErrKeyFile},
		{"too short", []byte(enc.B64.EncodeToString(bytes.Repeat([]byte{1}, 31))), ErrKeyFile},
		{"too long", []byte(enc.B64.EncodeToString(bytes.Repeat([]byte{1}, 33))), ErrKeyFile},
		{"a whole novel", bytes.Repeat([]byte("A"), 1<<20), ErrKeyFile},
		// A real key with a megabyte of white space behind it: without the
		// limit of maxKeyFile, TrimSpace would make this a key and the file
		// would be read whole. The limit refuses it, and that is what tells
		// the two apart — a file that is merely long is refused either way.
		{"a key behind a megabyte of space",
			append([]byte(enc.B64.EncodeToString(bytes.Repeat([]byte{7}, store.KeySize))),
				bytes.Repeat([]byte(" "), 1<<20)...), ErrKeyFile},
	}
	for _, c := range cases {
		keyFile := filepath.Join(dir, c.name+".key")
		if c.body != nil {
			write(t, keyFile, c.body)
		}
		out := filepath.Join(dir, c.name+".enc")
		err := Encrypt(plain, out, keyFile)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
		if _, err := os.Stat(out); err == nil {
			t.Errorf("%s: a refused call left a file behind", c.name)
		}
	}
	// Intermediate check: a real key on the same input and the same output
	// path works, so the six refusals above are about the key file and not
	// about the package or the directory.
	good := filepath.Join(dir, "good.key")
	if _, err := NewContentKey(good); err != nil {
		t.Fatalf("key: %v", err)
	}
	if err := Encrypt(plain, filepath.Join(dir, "good.enc"), good); err != nil {
		t.Fatalf("the way is not open: %v", err)
	}
}

func TestEncryptRefusesToOverwriteItsInput(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "app.ngc")
	write(t, plain, []byte("package"))
	keyFile := filepath.Join(dir, "content.key")
	if _, err := NewContentKey(keyFile); err != nil {
		t.Fatalf("key: %v", err)
	}
	if err := Encrypt(plain, filepath.Join(dir, ".", "app.ngc"), keyFile); !errors.Is(err, ErrSamePath) {
		t.Fatalf("same path: %v", err)
	}
	if string(read(t, plain)) != "package" {
		t.Fatal("the input was changed")
	}
}

func TestEncryptLeavesNoFileBehindWhenTheInputGoes(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "content.key")
	if _, err := NewContentKey(keyFile); err != nil {
		t.Fatalf("key: %v", err)
	}
	out := filepath.Join(dir, "app.enc")
	if err := Encrypt(filepath.Join(dir, "gone.ngc"), out, keyFile); err == nil {
		t.Fatal("a missing input did not fail")
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("a failed run left a file behind")
	}
	// And a failure that happens after the output was created: a directory
	// opens, but reading it fails, so the run breaks off in the middle of
	// store.Encrypt. This is the case that measures the removal — above,
	// nothing was created yet.
	in := filepath.Join(dir, "sub")
	if err := os.Mkdir(in, 0o755); err != nil {
		t.Fatal(err)
	}
	out2 := filepath.Join(dir, "sub.enc")
	if err := Encrypt(in, out2, keyFile); err == nil {
		t.Skipf("LOUD SKIP, NO RESULT: reading a directory does not fail on %s, so the removal was not measured", runtime.GOOS)
	}
	if _, err := os.Stat(out2); err == nil {
		t.Fatal("a run that broke off left a file behind")
	}
}

// keyShapes returns the three shapes a secret can leak in: the raw bytes,
// base64url as the key file writes it, and hex in both cases. A check that
// looks for one shape only misses a message built with %x or %X.
func keyShapes(key []byte) []string {
	return []string{
		string(key),
		enc.B64.EncodeToString(key),
		hex.EncodeToString(key),
		strings.ToUpper(hex.EncodeToString(key)),
	}
}

// leaks reports whether text carries the key in any of its shapes.
func leaks(text string, key []byte) bool {
	for _, s := range keyShapes(key) {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

func TestNoSecretReachesAMessage(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "content.key")
	key, err := NewContentKey(keyFile)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	// The control that must live: a message built the forbidden way is
	// caught by every one of the four shapes. Without it a green run below
	// would only prove that the search does not work.
	for i, bad := range []string{
		"key " + string(key),
		"key " + enc.B64.EncodeToString(key),
		fmt.Sprintf("key %x", key),
		fmt.Sprintf("key %X", key),
	} {
		if !leaks(bad, key) {
			t.Fatalf("shape %d: the leak check cannot see a leak", i)
		}
	}

	plain := filepath.Join(dir, "app.ngc")
	write(t, plain, []byte("package"))
	// Every way encrypt and license can fail or succeed, through the
	// dispatcher, with the real key file in place.
	calls := [][]string{
		{"encrypt"},
		{"encrypt", plain, keyFile},
		{"encrypt", plain, filepath.Join(dir, "no.key")},
		{"encrypt", filepath.Join(dir, "gone.ngc"), keyFile},
		{"encrypt", filepath.Join(dir, "app.txt"), keyFile},
		{"license", "org.sorglos.demo", "x", keyFile, keyFile, "y", "z"},
		{"license", "org.sorglos.demo", "x", filepath.Join(dir, "no.key"), keyFile, "y", "z"},
	}
	for _, args := range calls {
		var stdout, stderr bytes.Buffer
		Run(args, &stdout, &stderr)
		if leaks(stdout.String(), key) || leaks(stderr.String(), key) {
			t.Errorf("%q: a message carries the key: %q %q", args, stdout.String(), stderr.String())
		}
	}
	// The call that makes a key of its own: whatever it prints must not be
	// the key it just drew. The key is read back from the file afterwards,
	// so the check knows the very bytes that were created.
	fresh := filepath.Join(dir, "fresh.key")
	freshPkg := filepath.Join(dir, "fresh.ngc")
	write(t, freshPkg, []byte("package"))
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"encrypt", freshPkg, fresh}, &stdout, &stderr); code != 0 {
		t.Fatalf("encrypt with a new key: exit %d, %s", code, stderr.String())
	}
	made, err := readContentKey(fresh)
	if err != nil {
		t.Fatalf("read back the new key: %v", err)
	}
	if bytes.Equal(made, key) {
		t.Fatal("the new key is the old one: nothing was measured")
	}
	if leaks(stdout.String(), made) || leaks(stderr.String(), made) {
		t.Errorf("the new key was printed: %q %q", stdout.String(), stderr.String())
	}

	// And the errors of the functions themselves, not only of the dispatcher.
	for _, err := range []error{
		func() error { _, e := NewContentKey(keyFile); return e }(),
		Encrypt(plain, filepath.Join(dir, "out.enc"), filepath.Join(dir, "no.key")),
		func() error { _, e := readContentKey(keyFile); return e }(),
		func() error {
			_, e := License("org.sorglos.demo", "x", keyFile, keyFile, "y", LicenseOpts{Store: "z", Issued: 1})
			return e
		}(),
	} {
		if err != nil && leaks(err.Error(), key) {
			t.Errorf("an error carries the key: %v", err)
		}
	}
}

// childEnv marks the process the next test starts as its own child.
const childEnv = "NGC_T27_CHILD"

// TestChildEncryptsWhenTheParentSaysSo is the child of
// TestNoSecretReachesTheProcessList, not a test of its own. It blocks until
// the parent has read its command line, so the parent never has to guess
// when to look: a sample taken against a short-lived process is a race, and
// a race that was green once and red the next time is what this avoids.
func TestChildEncryptsWhenTheParentSaysSo(t *testing.T) {
	if os.Getenv(childEnv) == "" {
		t.Skip("only runs as the child of TestNoSecretReachesTheProcessList")
	}
	args := flag.Args()
	if len(args) < 2 {
		t.Fatalf("the child got %d arguments", len(args))
	}
	if _, err := io.ReadFull(os.Stdin, make([]byte, 1)); err != nil {
		t.Fatalf("the parent never said go: %v", err)
	}
	if code := Run([]string{"encrypt", args[0], args[1]}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("encrypt in the child: exit %d", code)
	}
}

// commandLineOf returns the command line of a running process as the
// operating system shows it to another process of the same user — the list
// anyone can read with ps or the task manager.
func commandLineOf(pid int) (string, error) {
	if runtime.GOOS == "windows" {
		out, err := exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("(Get-CimInstance Win32_Process -Filter 'ProcessId=%d').CommandLine", pid)).Output()
		return string(out), err
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	return strings.ReplaceAll(string(b), "\x00", " "), err
}

func TestNoSecretReachesTheProcessList(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("LOUD SKIP, NO RESULT: the test binary does not know its own path: %v", err)
	}
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "content.key")
	key, err := NewContentKey(keyFile)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	plain := filepath.Join(dir, "app.ngc")
	write(t, plain, []byte("package"))
	marker := "ngc-t27-marker"

	// Two children: the first is the call ngc-store really makes — paths
	// only. The second adds the key as an argument, the mistake this test
	// forbids; it must be caught, or the measurement proves nothing.
	for _, c := range []struct {
		name  string
		extra []string
		leak  bool
	}{
		{"the real call", nil, false},
		{"the forbidden call", []string{enc.B64.EncodeToString(key)}, true},
	} {
		args := append([]string{"-test.run=TestChildEncryptsWhenTheParentSaysSo",
			filepath.Join(dir, c.name+".ngc"), keyFile, marker}, c.extra...)
		write(t, filepath.Join(dir, c.name+".ngc"), []byte("package"))
		cmd := exec.Command(exe, args...)
		cmd.Env = append(os.Environ(), childEnv+"=1")
		in, err := cmd.StdinPipe()
		if err != nil {
			t.Fatalf("%s: stdin: %v", c.name, err)
		}
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Start(); err != nil {
			t.Fatalf("%s: start: %v", c.name, err)
		}
		// The child waits on stdin, so it is alive for every one of these
		// tries: this loop waits for the operating system to show the
		// command line, it does not race the child's lifetime.
		var line string
		for i := 0; i < 100 && !strings.Contains(line, marker); i++ {
			if i > 0 {
				time.Sleep(20 * time.Millisecond)
			}
			line, _ = commandLineOf(cmd.Process.Pid)
		}
		if !strings.Contains(line, marker) {
			in.Close()
			cmd.Process.Kill()
			cmd.Wait()
			t.Skipf("LOUD SKIP, NO RESULT: the command line of %d could not be read, so nothing was measured (got %q)",
				cmd.Process.Pid, line)
		}
		if got := leaks(line, key); got != c.leak {
			t.Errorf("%s: the key in the process list: %v, want %v (%q)", c.name, got, c.leak, line)
		}
		in.Write([]byte("g"))
		in.Close()
		if err := cmd.Wait(); err != nil {
			t.Errorf("%s: the child failed: %v, %s", c.name, err, out.String())
		}
		if leaks(out.String(), key) {
			t.Errorf("%s: the child printed the key", c.name)
		}
	}
}
