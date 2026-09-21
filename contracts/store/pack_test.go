package store

import (
	"archive/tar"
	"bytes"
	"crypto/ecdh"
	"encoding/json"
	"io"
	"slices"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
)

type entry struct {
	name string
	body []byte
	kind byte
}

func buildTar(t testing.TB, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: e.kind}
		if e.kind == tar.TypeSymlink {
			h.Linkname, h.Size = "/etc/shadow", 0
		}
		if e.kind == tar.TypeDir {
			h.Mode, h.Size = 0o755, 0
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.kind == tar.TypeReg {
			if _, err := w.Write(e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func manifestEntry(t testing.TB) entry {
	b, err := json.Marshal(terminalManifest())
	if err != nil {
		t.Fatal(err)
	}
	return entry{ManifestFile, b, tar.TypeReg}
}

func goodEntries(t testing.TB) []entry {
	return []entry{
		manifestEntry(t),
		{"ui", nil, tar.TypeDir},
		{"ui/index.html", []byte("<!doctype html><title>Terminal</title>"), tar.TypeReg},
		{"ui/icon.svg", []byte("<svg/>"), tar.TypeReg},
		{"services/amd64.tar", []byte("image"), tar.TypeReg},
	}
}

func TestWalk(t *testing.T) {
	var got []PackFile
	m, err := Walk(bytes.NewReader(buildTar(t, goodEntries(t))), func(m *Manifest, f PackFile, body io.Reader) error {
		b, err := io.ReadAll(body)
		if err != nil || int64(len(b)) != f.Size {
			t.Errorf("%s: %v", f.Path, err)
		}
		got = append(got, f)
		return nil
	})
	if err != nil || m.ID != "org.sorglos.terminal" || len(got) != 3 {
		t.Fatalf("got %v, %v", got, err)
	}
	if got[2].Kind != KindImage || got[2].Arch != "amd64" || got[0].Kind != KindUI {
		t.Errorf("kinds: %+v", got)
	}
}

func TestWalkRejects(t *testing.T) {
	good := goodEntries(t)
	cases := map[string][]entry{
		"manifest not first": {good[2], good[0], good[3], good[4]},
		"symlink":            append(good[:1:1], entry{"ui/index.html", nil, tar.TypeSymlink}),
		"parent dir":         append(slices.Clone(good), entry{"ui/../../etc/cron.d/x", []byte("x"), tar.TypeReg}),
		"absolute":           append(slices.Clone(good), entry{"/etc/passwd", []byte("x"), tar.TypeReg}),
		"unknown top level":  append(slices.Clone(good), entry{"bin/sh", []byte("x"), tar.TypeReg}),
		"duplicate":          append(slices.Clone(good), good[2]),
		"unknown arch":       append(slices.Clone(good), entry{"services/riscv64.tar", []byte("x"), tar.TypeReg}),
		"ui entry missing":   {good[0], good[3], good[4]},
		"image missing":      {good[0], good[2], good[3]},
		"ui file too large":  append(slices.Clone(good), entry{"ui/big.js", make([]byte, MaxUIFile+1), tar.TypeReg}),
	}
	for name, entries := range cases {
		_, err := Walk(bytes.NewReader(buildTar(t, entries)), func(*Manifest, PackFile, io.Reader) error { return nil })
		if err != ErrPackage {
			t.Errorf("%s: %v", name, err)
		}
	}
}

var contentKey = bytes.Repeat([]byte{0x42}, KeySize)

func fixedRand(b byte) io.Reader { return bytes.NewReader(bytes.Repeat([]byte{b}, 1024)) }

func encrypt(t testing.TB, plain []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := Encrypt(&out, bytes.NewReader(plain), contentKey, fixedRand(0x24)); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func decrypt(sealed, key []byte) ([]byte, error) {
	r, err := NewDecrypter(bytes.NewReader(sealed), key)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func TestEncrypt(t *testing.T) {
	for _, n := range []int{0, 1, ChunkSize - 1, ChunkSize, ChunkSize + 1, 3*ChunkSize + 5} {
		plain := bytes.Repeat([]byte{byte(n)}, n)
		got, err := decrypt(encrypt(t, plain), contentKey)
		if err != nil || !bytes.Equal(got, plain) {
			t.Errorf("size %d: %v", n, err)
		}
	}
	sealed := encrypt(t, bytes.Repeat([]byte{7}, 2*ChunkSize))
	block := ChunkSize + 16
	header := len("NGCENC1\n") + FileIDSize
	flipped := bytes.Clone(sealed)
	flipped[header+5] ^= 1
	attacks := map[string][]byte{
		"flipped bit": flipped,
		"truncated":   sealed[:header+block],
		"appended":    append(bytes.Clone(sealed), sealed[header:header+block]...),
		"swapped":     append(append(bytes.Clone(sealed[:header]), sealed[header+block:header+2*block]...), sealed[header:header+block]...),
		"bad magic":   append([]byte("NGCENC2\n"), sealed[8:]...),
		"only header": sealed[:header],
	}
	for name, s := range attacks {
		if _, err := decrypt(s, contentKey); err != ErrEncrypted {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := decrypt(sealed, bytes.Repeat([]byte{1}, KeySize)); err != ErrEncrypted {
		t.Errorf("wrong key: %v", err)
	}
}

func TestWrapKey(t *testing.T) {
	priv, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x91}, 32))
	if err != nil {
		t.Fatal(err)
	}
	w, err := WrapKey(contentKey, priv.PublicKey().Bytes(), "org.sorglos.terminal", fixedRand(0x33))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := UnwrapKey(w, priv, "org.sorglos.terminal"); err != nil || !bytes.Equal(got, contentKey) {
		t.Fatalf("unwrap: %v", err)
	}
	other, _ := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x92}, 32))
	if _, err := UnwrapKey(w, other, "org.sorglos.terminal"); err != ErrEncrypted {
		t.Errorf("other installation: %v", err)
	}
	if _, err := UnwrapKey(w, priv, "org.sorglos.mail"); err != ErrEncrypted {
		t.Errorf("other app: %v", err)
	}
}

type packVector struct {
	ContentKey  enc.Bytes   `json:"contentKey"`
	Plain       enc.Bytes   `json:"plain"`
	Sealed      enc.Bytes   `json:"sealed"`
	InstallSeed enc.Bytes   `json:"installSeed"` // X25519 private key of the test installation
	App         string      `json:"app"`
	Wrapped     *WrappedKey `json:"wrapped"`
}

func TestPackVectors(t *testing.T) {
	priv, _ := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x91}, 32))
	w, err := WrapKey(contentKey, priv.PublicKey().Bytes(), "org.sorglos.terminal", fixedRand(0x33))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("NGC Kauf-App, Testinhalt")
	golden.Check(t, *update, "pack.json", packVector{ContentKey: contentKey, Plain: plain, Sealed: encrypt(t, plain),
		InstallSeed: bytes.Repeat([]byte{0x91}, 32), App: "org.sorglos.terminal", Wrapped: w})
}

func FuzzWalk(f *testing.F) {
	f.Add(buildTar(f, goodEntries(f)))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = Walk(bytes.NewReader(b), func(_ *Manifest, _ PackFile, body io.Reader) error {
			_, err := io.Copy(io.Discard, body)
			return err
		})
	})
}

func FuzzDecrypt(f *testing.F) {
	f.Add(encrypt(f, []byte("hello")))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = decrypt(b, contentKey)
	})
}
