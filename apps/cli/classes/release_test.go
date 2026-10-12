package classes

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
)

// releaseSeed is the Ed25519 seed the release key of these cases carries. It
// is a test key, nothing else signs with it, and the case that checks that
// no secret escapes looks for exactly these bytes.
var releaseSeed = bytes.Repeat([]byte{0x3c}, ed25519.SeedSize)

// releaseFixture is one release directory: the files, the key file with the
// secret seed and the public key beside it as PEM.
type releaseFixture struct {
	dir     string
	keyFile string
	pub     ed25519.PublicKey
	bytes   map[string][]byte // file name to content, as laid down
}

// newReleaseFixture lays down the files of parts in dir, each with content
// of its own, and writes the key file.
func newReleaseFixture(t testing.TB, parts ...string) releaseFixture {
	t.Helper()
	dir := t.TempDir()
	f := releaseFixture{dir: dir, keyFile: filepath.Join(t.TempDir(), "release.key"),
		pub: ed25519.NewKeyFromSeed(releaseSeed).Public().(ed25519.PublicKey), bytes: map[string][]byte{}}
	write(t, f.keyFile, []byte(enc.B64.EncodeToString(releaseSeed)+"\n"))
	for i, p := range parts {
		name := p + ".tar"
		body := []byte(strings.Repeat(p+" ", i+1) + "\n")
		write(t, filepath.Join(dir, name), body)
		f.bytes[name] = body
	}
	return f
}

// sum is the SHA-256 of the file name in the fixture.
func (f releaseFixture) sum(name string) []byte {
	s := sha256.Sum256(f.bytes[name])
	return s[:]
}

// verifyOK reads the envelope the way an installation does: the signature
// against the release key, then the contract's parser. Every case reads its
// result through store.VerifyRelease, never through a reader of its own.
func (f releaseFixture) verifyOK(t testing.TB, env sign.Envelope, lastSeq uint64) *store.Release {
	t.Helper()
	r, err := store.VerifyRelease(env, f.pub, lastSeq)
	if err != nil {
		t.Fatalf("store.VerifyRelease: %v", err)
	}
	return r
}

func releaseOpts(seq uint64, base string) ReleaseOpts {
	return ReleaseOpts{Version: "1.1.0", Seq: seq, Issued: 1_790_000_000, Notes: "Testlauf", BaseURL: base}
}

func TestReleaseListsEveryFile(t *testing.T) {
	f := newReleaseFixture(t, "ngc-web-all", "ngc-api-amd64", "ngc-api-arm64",
		"ngc-controller-amd64", "ngc-controller-arm64")
	env, err := Release(f.dir, f.keyFile, releaseOpts(1, "https://dl.example.org/1.1.0"))
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	r := f.verifyOK(t, env, 0)
	if r.Version != "1.1.0" || r.Seq != 1 || r.Issued != 1_790_000_000 || r.Notes != "Testlauf" {
		t.Errorf("release %+v", *r)
	}
	want := []store.File{
		{Part: store.ReleaseWeb, Arch: "all"},
		{Part: store.ReleaseAPI, Arch: "amd64"},
		{Part: store.ReleaseAPI, Arch: "arm64"},
		{Part: store.ReleaseController, Arch: "amd64"},
		{Part: store.ReleaseController, Arch: "arm64"},
	}
	if len(r.Files) != len(want) {
		t.Fatalf("%d files, want %d", len(r.Files), len(want))
	}
	for i, w := range want {
		got := r.Files[i]
		name := w.Part + "-" + w.Arch + ".tar"
		if got.Part != w.Part || got.Arch != w.Arch {
			t.Errorf("file %d is %s %s, want %s %s", i, got.Part, got.Arch, w.Part, w.Arch)
			continue
		}
		if want := "https://dl.example.org/1.1.0/" + name; got.URL != want {
			t.Errorf("%s url %q, want %q", name, got.URL, want)
		}
		if want := int64(len(f.bytes[name])); got.Size != want {
			t.Errorf("%s size %d, want %d", name, got.Size, want)
		}
		if !bytes.Equal(got.SHA256, f.sum(name)) {
			t.Errorf("%s checksum %x, want %x", name, got.SHA256, f.sum(name))
		}
	}
}

// TestReleaseNamesTheFilesByTheirName is the intermediate check behind
// TestReleaseListsEveryFile: a directory that holds other files as well
// lists only the ones whose name names a part, and a directory that names
// none at all is an error and not an empty release.
func TestReleaseNamesTheFilesByTheirName(t *testing.T) {
	f := newReleaseFixture(t, "ngc-web-all")
	for _, name := range []string{"notes.txt", "ngc-web-amd64.tar", "ngc-store-all.tar", "ngc-web-all.zip"} {
		write(t, filepath.Join(f.dir, name), []byte("not a release file\n"))
	}
	env, err := Release(f.dir, f.keyFile, releaseOpts(1, ""))
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	r := f.verifyOK(t, env, 0)
	if len(r.Files) != 1 || r.Files[0].Part != store.ReleaseWeb || r.Files[0].URL != "ngc-web-all.tar" {
		t.Fatalf("files %+v", r.Files)
	}
	empty := t.TempDir()
	if _, err := Release(empty, f.keyFile, releaseOpts(1, "")); !errors.Is(err, ErrNoReleaseFile) {
		t.Errorf("empty directory: %v, want %v", err, ErrNoReleaseFile)
	}
}

func TestReleaseVerifies(t *testing.T) {
	f := newReleaseFixture(t, "ngc-web-all", "ngc-api-amd64")
	env, err := Release(f.dir, f.keyFile, releaseOpts(7, ""))
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	// The way is open for this very envelope up to the sequence number it
	// carries: without this line the refusal below would prove nothing.
	f.verifyOK(t, env, 7)
	if _, err := store.VerifyRelease(env, f.pub, 8); !errors.Is(err, store.ErrRollback) {
		t.Errorf("a smaller seq: %v, want %v", err, store.ErrRollback)
	}
	// A release of another key is refused, whatever it says.
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x11}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	if _, err := store.VerifyRelease(env, other, 0); err == nil {
		t.Error("a foreign key verified the release")
	}
}

// TestReleaseRefusesASequenceThatDoesNotRise keeps the stricter rule of Task
// 28: the contract still accepts a release whose seq equals the last one
// seen (store.VerifyRelease refuses only seq < lastSeq), and writing one
// again is still a mistake of whoever runs the server. One tool must not
// have two ideas of what a sequence number is, so ngc-store refuses the
// equal one here, in both subcommands.
func TestReleaseRefusesASequenceThatDoesNotRise(t *testing.T) {
	f := newReleaseFixture(t, "ngc-web-all")
	env, err := Release(f.dir, f.keyFile, releaseOpts(3, ""))
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(f.dir, ReleaseFile), b)
	// The intermediate check: with a higher number the way is open, so the
	// two refusals below are about the number and not about the file.
	if _, err := Release(f.dir, f.keyFile, releaseOpts(4, "")); err != nil {
		t.Fatalf("the way is not open: %v", err)
	}
	for _, seq := range []uint64{3, 2} {
		if _, err := Release(f.dir, f.keyFile, releaseOpts(seq, "")); !errors.Is(err, ErrSeq) {
			t.Errorf("seq %d: %v, want %v", seq, err, ErrSeq)
		}
	}
	// And the contract itself still takes the equal one: this is what makes
	// the rule above a decision of ours and not a repetition of the contract.
	if _, err := store.VerifyRelease(env, f.pub, 3); err != nil {
		t.Errorf("the contract refuses an equal seq: %v", err)
	}
}

// TestReleaseRefusesABaseWithAQuery is Release's one rule of its own about
// an address, and it is about the shape of the base, not its characters:
// "…?token=x/ngc-web-all.tar" stays inside store's grammar and names no
// file on any server.
func TestReleaseRefusesABaseWithAQuery(t *testing.T) {
	f := newReleaseFixture(t, "ngc-web-all")
	if _, err := Release(f.dir, f.keyFile, releaseOpts(1, "https://dl.example.org/x")); err != nil {
		t.Fatalf("the way is not open: %v", err)
	}
	if _, err := Release(f.dir, f.keyFile, releaseOpts(1, "https://dl.example.org/x?token=y")); !errors.Is(err, ErrPack) {
		t.Errorf("a base with a query: %v, want %v", err, ErrPack)
	}
}

// TestReleaseRefusesAnAddressOutsideTheGrammar leaves the grammar to the
// contract, as Punkt 25 of docs/vertraege-v1.2.md asks: ngc-store writes no
// second one, and an address the contract refuses never gets a signature.
func TestReleaseRefusesAnAddressOutsideTheGrammar(t *testing.T) {
	f := newReleaseFixture(t, "ngc-web-all")
	for _, base := range []string{"http://dl.example.org", "https://dl example.org", "https://dl.example.org/<x>",
		"https://dl.example.org/" + strings.Repeat("d", 600), "ftp://dl.example.org", "/srv/releases"} {
		if _, err := Release(f.dir, f.keyFile, releaseOpts(1, base)); !errors.Is(err, store.ErrRelease) {
			t.Errorf("base %q: %v, want %v", base, err, store.ErrRelease)
		}
	}
}

// TestReleaseRefusesABrokenKeyFile and the next case keep the secret out of
// every message: the key reaches ngc-store as a file, never as an argument,
// and a file that is no key is a sentinel without a word of its content.
func TestReleaseRefusesABrokenKeyFile(t *testing.T) {
	f := newReleaseFixture(t, "ngc-web-all")
	broken := filepath.Join(t.TempDir(), "broken.key")
	write(t, broken, []byte("Dies ist kein Schlüssel\n"))
	if _, err := Release(f.dir, f.keyFile, releaseOpts(1, "")); err != nil {
		t.Fatalf("the way is not open: %v", err)
	}
	if _, err := Release(f.dir, broken, releaseOpts(1, "")); !errors.Is(err, ErrSignKey) {
		t.Errorf("a broken key file: %v, want %v", err, ErrSignKey)
	}
	missing := filepath.Join(t.TempDir(), "none.key")
	if _, err := Release(f.dir, missing, releaseOpts(1, "")); !errors.Is(err, ErrKeyMissing) {
		t.Errorf("a missing key file: %v, want %v", err, ErrKeyMissing)
	}
}

// TestReleaseTellsNoSecret checks every message of this subcommand against
// the seed in four shapes (keyShapes), the control among them being a text
// that does leak and has to be found.
func TestReleaseTellsNoSecret(t *testing.T) {
	f := newReleaseFixture(t, "ngc-web-all")
	if !leaks(enc.B64.EncodeToString(releaseSeed), releaseSeed) {
		t.Fatal("the control does not find a leak, so this case measures nothing")
	}
	var out, errOut bytes.Buffer
	code := Run([]string{"release", "-version", "1.1.0", "-seq", "1", f.dir, f.keyFile}, &out, &errOut)
	if code != 0 {
		t.Fatalf("release: %d, %s", code, errOut.String())
	}
	broken := filepath.Join(t.TempDir(), "broken.key")
	write(t, broken, []byte(enc.B64.EncodeToString(releaseSeed)+"xx\n"))
	Run([]string{"release", "-version", "1.1.0", "-seq", "2", f.dir, broken}, &out, &errOut)
	Run([]string{"check", f.dir, store.ReleaseWeb, "all"}, &out, &errOut)
	Run([]string{"check", f.dir, store.ReleaseAPI, "amd64"}, &out, &errOut)
	for _, text := range []string{out.String(), errOut.String()} {
		if leaks(text, releaseSeed) {
			t.Errorf("a message carries the key: %q", text)
		}
	}
	// And the public half is in the directory, which is the point of it.
	pem, err := os.ReadFile(filepath.Join(f.dir, ReleaseKeyFile))
	if err != nil {
		t.Fatalf("no %s: %v", ReleaseKeyFile, err)
	}
	if leaks(string(pem), releaseSeed) {
		t.Error("the PEM file carries the seed")
	}
	key, err := publicKeyPEM(pem)
	if err != nil || !key.Equal(f.pub) {
		t.Errorf("%s is not the public key: %v", ReleaseKeyFile, err)
	}
}

// TestRunChecksARelease is the subcommand end to end: release writes the
// directory, check reads it and says "release ok", and a changed file makes
// it say what is wrong and return 1.
func TestRunChecksARelease(t *testing.T) {
	f := newReleaseFixture(t, "ngc-web-all")
	var out, errOut bytes.Buffer
	if code := Run([]string{"release", "-version", "1.1.0", "-seq", "1", f.dir, f.keyFile}, &out, &errOut); code != 0 {
		t.Fatalf("release: %d, %s", code, errOut.String())
	}
	out.Reset()
	if code := Run([]string{"check", f.dir, store.ReleaseWeb, "all"}, &out, &errOut); code != 0 || out.String() != "release ok\n" {
		t.Fatalf("check: %d, %q, %s", code, out.String(), errOut.String())
	}
	write(t, filepath.Join(f.dir, "ngc-web-all.tar"), []byte("etwas anderes\n"))
	out.Reset()
	errOut.Reset()
	if code := Run([]string{"check", f.dir, store.ReleaseWeb, "all"}, &out, &errOut); code != 1 ||
		!strings.Contains(errOut.String(), "release wrong: checksum wrong: ngc-web-all.tar") {
		t.Errorf("check of a changed file: %d, %q, %q", code, out.String(), errOut.String())
	}
}

// testdataRelease is shared/testdata/release, the vector verify.sh is
// measured against in shared/store. apps/cli may not import
// shared/internal/golden, so the directory is found by its place in the
// repository; a missing one is a Fatalf, never a skip (docs: messen.md).
func testdataRelease(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "shared", "testdata", "release")
	for _, name := range []string{"release.json", "release-key.pem", "ngc-web-all.tar", "verify.sh"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("the release vector is not there: %v", err)
		}
	}
	return dir
}

// scriptCase is one of the cases of TestReleaseScript in shared/store.
type scriptCase struct {
	name  string
	files map[string][]byte
	part  string
	arch  string
	// why is what verify.sh says of a release it refuses, copied from
	// shared/store's TestReleaseScript, where it is measured against the real
	// jq on the Linux servers; "" if the script passes it.
	why string
	// goWhy is set only where Check names a different reason than the
	// script. Every one of these is Check being stricter, and the comment of
	// the case says how.
	goWhy string
}

// TestCheckMatchesTheScript judges the 31 cases of TestReleaseScript
// (shared/store/catalog_test.go) with Check and compares case by case:
// first the verdict, then the reason.
//
// The verdicts agree on all 31. The reasons agree on 30 by the same
// substring the script's own case table uses, including the three cases the
// script answers with "no single valid entry" while the contract's parser
// refuses the whole release — Check names both in one sentence, so the
// script's words are inside its own.
//
// One case differs, and it is the last one in the table: an address whose
// last segment begins with a dot. The script gets that far and calls the
// name not allowed; store.ValidPath refuses such an address before it, so
// the release is no valid release and Check never looks at a file name.
// Stricter, and in the right direction — a release that is not a valid
// release is refused whole.
//
// What this case does NOT measure: the script itself. That needs jq, which
// windows/amd64 has not got here, and a stand-in for jq written for this
// case could make the two agree by being wrong. The script is measured in
// shared/store on a machine that has jq, and this case measures Check
// against the reasons that run recorded. The subtest "script" says so out
// loud and names the missing tool.
func TestCheckMatchesTheScript(t *testing.T) {
	vectorDir := testdataRelease(t)
	vector := map[string][]byte{}
	for _, name := range []string{"release.json", "release-key.pem", "ngc-web-all.tar"} {
		b, err := os.ReadFile(filepath.Join(vectorDir, name))
		if err != nil {
			t.Fatal(err)
		}
		vector[name] = b
	}
	releaseFile := vector["ngc-web-all.tar"]
	key := ed25519.NewKeyFromSeed(releaseSeed)
	keyPEM, err := PublicKeyPEM(key.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	otherPEM, err := PublicKeyPEM(ed25519.NewKeyFromSeed(testSeed).Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	checksum := func(b []byte) enc.Bytes { s := sha256.Sum256(b); return s[:] }
	api := []byte("NGC api test file\n")
	web := store.File{Part: store.ReleaseWeb, Arch: "all", URL: "ngc-web-all.tar",
		Size: int64(len(releaseFile)), SHA256: checksum(releaseFile)}
	apiAMD := store.File{Part: store.ReleaseAPI, Arch: "amd64", URL: "https://example.org/1.1.0/ngc-api-amd64.tar",
		Size: int64(len(api)), SHA256: checksum(api)}
	apiARM := store.File{Part: store.ReleaseAPI, Arch: "arm64", URL: "https://example.org/1.1.0/ngc-api-arm64.tar",
		Size: 1, SHA256: checksum([]byte("x"))}
	webAt := func(url string) store.File { f := web; f.URL = url; return f }
	payload := func(files ...store.File) []byte {
		b, err := json.Marshal(store.Release{Seq: 3, Issued: 1_790_000_000, Version: "1.1.0", Files: files})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	// release is release.json with payload signed by the release key, after
	// change if set — the same three fields the script reads with jq.
	release := func(payload []byte, change func(m map[string]string)) []byte {
		env, err := sign.Sign(key, sign.PurposeRelease, payload)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]string{"payload": enc.B64.EncodeToString(env.Payload), "kid": env.KeyID,
			"sig": enc.B64.EncodeToString(env.Sig)}
		if change != nil {
			change(m)
		}
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return append(b, '\n')
	}
	dir := func(releaseJSON []byte, files map[string][]byte) map[string][]byte {
		d := map[string][]byte{"release.json": releaseJSON, "release-key.pem": keyPEM}
		for name, b := range files {
			d[name] = b
		}
		return d
	}
	withKey := func(d map[string][]byte, pem []byte) map[string][]byte { d["release-key.pem"] = pem; return d }
	webOnly := map[string][]byte{"ngc-web-all.tar": releaseFile}
	// unusedBit sets an unused bit of the signature's last letter: the same
	// bytes, a second spelling, which enc.B64 refuses as strictly as the
	// script's b64url does.
	unusedBit := func(m map[string]string) {
		const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
		s := m["sig"]
		m["sig"] = s[:len(s)-1] + string(alphabet[strings.IndexByte(alphabet, s[len(s)-1])|1])
	}

	cases := []scriptCase{
		{name: "release vector", files: map[string][]byte{"release.json": vector["release.json"],
			"release-key.pem": vector["release-key.pem"], "ngc-web-all.tar": vector["ngc-web-all.tar"]}},
		{name: "url of https", files: dir(release(payload(webAt("https://example.org/1.1.0/ngc-web-all.tar")), nil), webOnly)},
		{name: "url with a directory", files: dir(release(payload(webAt("sub/ngc-web-all.tar")), nil), webOnly)},
		{name: "files of other parts missing", files: dir(release(payload(web, apiAMD, apiARM), nil),
			map[string][]byte{"ngc-api-amd64.tar": api}), part: store.ReleaseAPI, arch: "amd64"},
		{name: "file of another part changed", files: dir(release(payload(web, apiAMD), nil),
			map[string][]byte{"ngc-web-all.tar": releaseFile, "ngc-api-amd64.tar": []byte("changed")})},
		{name: "files not listed", files: dir(release(payload(web), nil), map[string][]byte{"ngc-web-all.tar": releaseFile,
			"evil.sh": []byte("rm -rf /"), "payload.json": []byte("{}"), "sub/ngc-web-all.tar": []byte("other")})},
		{name: "file named like a work file", files: dir(release(payload(webAt("payload.json")), nil),
			map[string][]byte{"payload.json": releaseFile})},

		{name: "tampered payload", files: dir(release(payload(web), func(m map[string]string) {
			p, _ := enc.B64.DecodeString(m["payload"])
			m["payload"] = enc.B64.EncodeToString(bytes.Replace(p, []byte(`"1.1.0"`), []byte(`"9.9.9"`), 1))
		}), webOnly), why: "signature does not verify"},
		{name: "wrong key", files: withKey(dir(release(payload(web), nil), webOnly), otherPEM),
			why: "signature does not verify"},
		{name: "changed signature", files: dir(release(payload(web), func(m map[string]string) {
			s, _ := enc.B64.DecodeString(m["sig"])
			s[0] ^= 1
			m["sig"] = enc.B64.EncodeToString(s)
		}), webOnly), why: "signature does not verify"},
		{name: "signature with an unused bit set", files: dir(release(payload(web), unusedBit), webOnly),
			why: "signature not base64url"},
		{name: "payload with padding", files: dir(release(payload(web), func(m map[string]string) {
			m["payload"] += "="
		}), webOnly), why: "payload not base64url"},
		{name: "payload ending in a newline", files: dir(release(payload(web), func(m map[string]string) {
			m["payload"] += "\n"
		}), webOnly), why: "payload not base64url"},
		{name: "signature ending in a newline", files: dir(release(payload(web), func(m map[string]string) {
			m["sig"] += "\n"
		}), webOnly), why: "signature not base64url"},
		{name: "payload with a NUL byte", files: dir(release(payload(web), func(m map[string]string) {
			m["payload"] += "\x00"
		}), webOnly), why: "payload not base64url"},
		{name: "no payload", files: dir(release(payload(web), func(m map[string]string) {
			delete(m, "payload")
		}), webOnly), why: "no payload"},
		{name: "wrong hash", files: dir(release(payload(web), nil),
			map[string][]byte{"ngc-web-all.tar": []byte("NGC web test filE\n")}), why: "checksum wrong"},
		{name: "bytes appended", files: dir(release(payload(web), nil),
			map[string][]byte{"ngc-web-all.tar": append(slices.Clone(releaseFile), 'x')}), why: "checksum wrong"},
		{name: "wrong size", files: dir(release(payload(func() store.File { f := web; f.Size++; return f }()), nil), webOnly),
			why: "size wrong"},
		{name: "missing file", files: dir(release(payload(web), nil), nil), why: "file missing"},
		{name: "file only in the url's directory", files: dir(release(payload(webAt("sub/ngc-web-all.tar")), nil),
			map[string][]byte{"sub/ngc-web-all.tar": releaseFile}), why: "file missing"},
		{name: "files null", files: dir(release(payload(), nil), webOnly), why: "no single valid entry"},
		{name: "files empty", files: dir(release(payload([]store.File{}...), nil), webOnly), why: "no single valid entry"},
		{name: "part not listed", files: dir(release(payload(web), nil), webOnly),
			part: store.ReleaseController, arch: "amd64", why: "no single valid entry"},
		{name: "two entries for the part", files: dir(release(payload(web, web), nil), webOnly), why: "no single valid entry"},
		{name: "size as a string", files: dir(release([]byte(strings.Replace(string(payload(web)),
			`"size":18`, `"size":"18"`, 1)), nil), webOnly), why: "no single valid entry"},
		{name: "name with a leading dash", files: dir(release(payload(webAt("-x.tar")), nil),
			map[string][]byte{"-x.tar": releaseFile}), why: "file name not allowed"},
		{name: "name --help", files: dir(release(payload(webAt("https://example.org/--help")), nil),
			map[string][]byte{"--help": releaseFile}), why: "file name not allowed"},
		{name: "url ending in a slash", files: dir(release(payload(webAt("https://example.org/")), nil), webOnly),
			why: "file name not allowed"},
		{name: "no part and arch", files: dir(release(payload(web), nil), webOnly), part: "-", arch: "-", why: "usage"},
		// The one case where Check names another reason: store.ValidPath
		// refuses a last segment that begins with a dot, so the whole release
		// is no valid release and the file name is never reached.
		{name: "hidden name", files: dir(release(payload(webAt(".x.tar")), nil),
			map[string][]byte{".x.tar": releaseFile}), why: "file name not allowed",
			goWhy: "no single valid entry: the release is no valid release"},
	}
	if len(cases) != 31 {
		t.Fatalf("%d cases, want the 31 of TestReleaseScript", len(cases))
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d := t.TempDir()
			for name, b := range c.files {
				p := filepath.Join(d, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			part, arch := c.part, c.arch
			if part == "" && arch == "" {
				part, arch = store.ReleaseWeb, "all"
			}
			if part == "-" && arch == "-" {
				part, arch = "", ""
			}
			err := Check(d, part, arch)
			if c.why == "" {
				if err != nil {
					t.Fatalf("the script passes this, Check says %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("the script refuses this with %q, Check passes it", c.why)
				}
				want := c.why
				if c.goWhy != "" {
					want = c.goWhy
				}
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Check says %q, want it to hold %q", err, want)
				}
			}
			// Nothing is written into the release directory.
			left := 0
			if err := filepath.WalkDir(d, func(_ string, e os.DirEntry, err error) error {
				if err == nil && !e.IsDir() {
					left++
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if left != len(c.files) {
				t.Errorf("%d files in the release directory, want %d", left, len(c.files))
			}
		})
	}
	t.Run("script", func(t *testing.T) {
		// Not a silent skip: the reason is printed, and the parent case has
		// already judged all 31 cases without it.
		t.Skipf("verify.sh itself is not run here: jq is missing on this machine, and a stand-in " +
			"written for this case could make the two agree by being wrong. The script is measured " +
			"in shared/store (TestReleaseScript), and the reasons of that run are this table.")
	})
}
