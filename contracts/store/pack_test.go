package store

import (
	"archive/tar"
	"bytes"
	"crypto/ecdh"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
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
		if e.kind == tar.TypeSymlink || e.kind == tar.TypeLink {
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

// uiOnlyEntries is a valid package for an app with a UI but no services, the
// same manifest shape TestManifest already accepts (the "UI-only app" case).
func uiOnlyEntries(t testing.TB) []entry {
	t.Helper()
	m := &Manifest{ID: "org.sorglos.calculator", Name: "Rechner", Version: "1.0.0", Publisher: "Sorglos IT", UI: &UI{Entry: "index.html"}}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return []entry{
		{ManifestFile, b, tar.TypeReg},
		{"ui", nil, tar.TypeDir},
		{"ui/index.html", []byte("<!doctype html><title>Calc</title>"), tar.TypeReg},
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

// buildTarUITotal keeps every individual UI file at or under MaxUIFile but
// pushes their sum past MaxUITotal, isolating the cumulative check from the
// per-file one.
func buildTarUITotal(t testing.TB) []byte {
	t.Helper()
	entries := []entry{manifestEntry(t), {"ui", nil, tar.TypeDir}}
	for i := range 4 {
		entries = append(entries, entry{"ui/big" + strconv.Itoa(i) + ".bin", make([]byte, MaxUIFile), tar.TypeReg})
	}
	entries = append(entries, entry{"ui/tiny.bin", []byte{0}, tar.TypeReg})
	return buildTar(t, entries)
}

// buildTarManyEntries writes a manifest followed by n unique, tiny UI files,
// to drive the entry-count limit without also tripping a size limit.
func buildTarManyEntries(t testing.TB, n int) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	mb, err := json.Marshal(terminalManifest())
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteHeader(&tar.Header{Name: ManifestFile, Mode: 0o644, Size: int64(len(mb)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(mb); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if err := w.WriteHeader(&tar.Header{Name: "ui/f" + strconv.Itoa(i), Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte{0}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// buildTarHugeImage declares an image entry bigger than MaxPackage but never
// writes its body: Walk must refuse it from the header alone, without ever
// asking for gigabytes of data.
func buildTarHugeImage(t testing.TB) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, e := range goodEntries(t)[:4] {
		h := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: e.kind}
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
	if err := w.WriteHeader(&tar.Header{Name: "services/amd64.tar", Mode: 0o644, Size: MaxPackage + 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes() // deliberately no body and no Close: the header alone must be enough to reject
}

// paxRecord formats one PAX record: its length in decimal, including itself.
func paxRecord(k, v string) string {
	s := " " + k + "=" + v + "\n"
	n := len(s) + 1
	for len(strconv.Itoa(n))+len(s) != n {
		n = len(strconv.Itoa(n)) + len(s)
	}
	return strconv.Itoa(n) + s
}

// rawHeader builds a USTAR header block by hand, for entries tar.Writer does
// not write.
func rawHeader(name string, size int64, flag byte) []byte {
	h := make([]byte, 512)
	copy(h[0:100], name)
	copy(h[100:108], "0000644\x00")
	copy(h[108:116], "0000000\x00")
	copy(h[116:124], "0000000\x00")
	copy(h[124:136], fmt.Sprintf("%011o\x00", size))
	copy(h[136:148], "00000000000\x00")
	h[156] = flag
	copy(h[257:265], "ustar\x0000")
	for i := 148; i < 156; i++ {
		h[i] = ' '
	}
	sum := 0
	for _, c := range h {
		sum += int(c)
	}
	copy(h[148:156], fmt.Sprintf("%06o\x00 ", sum))
	return h
}

func pad512(b []byte) []byte { return append(b, make([]byte, (512-len(b)%512)%512)...) }

// sparseFile is a PAX 1.0 sparse file of size bytes whose only data is data,
// at offset 0; the rest is a hole. The header names the file as the sparse
// record does, so a reader that ignores the GNU.sparse.* records finds
// nothing else wrong with it.
func sparseFile(name string, data []byte, size int64) []byte {
	pax := paxRecord("GNU.sparse.major", "1") + paxRecord("GNU.sparse.minor", "0") +
		paxRecord("GNU.sparse.name", name) + paxRecord("GNU.sparse.realsize", strconv.FormatInt(size, 10))
	out := append(rawHeader("PaxHeaders/0", int64(len(pax)), tar.TypeXHeader), pad512([]byte(pax))...)
	sparseMap := pad512([]byte("1\n0\n" + strconv.Itoa(len(data)) + "\n")) // one segment: offset 0, len(data) bytes
	out = append(out, rawHeader(name, int64(len(sparseMap)+len(data)), tar.TypeReg)...)
	return append(append(out, sparseMap...), pad512(slices.Clone(data))...)
}

// sparseEntry claims size bytes but carries one 512-byte block, its sparse map.
func sparseEntry(name string, size int64) []byte { return sparseFile(name, nil, size) }

// endMarker is the end of every archive: two zero blocks.
var endMarker = make([]byte, 1024)

// withoutEnd returns an archive of buildTar without its end marker, so raw
// entries can follow.
func withoutEnd(archive []byte) []byte { return slices.Clone(archive[:len(archive)-len(endMarker)]) }

// TestSparseEntryExpands shows why Walk refuses sparse entries: archive/tar
// alone reports sparseEntry as a regular file and hands out every byte it
// claims, from one block of data.
func TestSparseEntryExpands(t *testing.T) {
	b := slices.Concat(sparseEntry("services/amd64.tar", 100<<20), endMarker)
	tr := tar.NewReader(bytes.NewReader(b))
	h, err := tr.Next()
	if err != nil || h.Typeflag != tar.TypeReg || h.Name != "services/amd64.tar" || h.Size != 100<<20 {
		t.Fatalf("%+v, %v", h, err)
	}
	if n, err := io.Copy(io.Discard, tr); err != nil || n != 100<<20 {
		t.Errorf("%d bytes from an archive of %d: %v", n, len(b), err)
	}
	// The sparse manifest of smallBadPackages reads as the good manifest, so
	// only the sparse rule refuses it.
	tr = tar.NewReader(bytes.NewReader(smallBadPackages(t)["sparse manifest"]))
	if h, err := tr.Next(); err != nil || h.Name != ManifestFile || h.Typeflag != tar.TypeReg {
		t.Fatalf("%+v, %v", h, err)
	}
	if b, err := io.ReadAll(tr); err != nil || !bytes.Equal(b, manifestEntry(t).body) {
		t.Errorf("sparse manifest reads %q, %v", b, err)
	}
}

// smallBadPackages returns invalid packages cheap enough to build on every
// call (including once per fuzz run), keyed by the reason Walk must reject
// them. Each differs from a good package by exactly one thing.
func smallBadPackages(t testing.TB) map[string][]byte {
	t.Helper()
	good := goodEntries(t)
	otherName := manifestEntry(t)
	otherName.name = "other.json"
	cases := map[string][]entry{
		"manifest not first":      {good[2], good[0], good[3], good[4]},
		"manifest name":           {otherName},
		"manifest is a directory": {{ManifestFile, nil, tar.TypeDir}},
		"manifest too large":      {{ManifestFile, make([]byte, MaxManifest+1), tar.TypeReg}},
		"manifest does not parse": {{ManifestFile, []byte("not json"), tar.TypeReg}},
		"symlink":                 append(slices.Clone(good), entry{"ui/link", nil, tar.TypeSymlink}),
		"hard link":               append(slices.Clone(good), entry{"ui/hardlink", nil, tar.TypeLink}),
		"parent dir":              append(slices.Clone(good), entry{"ui/../../etc/cron.d/x", []byte("x"), tar.TypeReg}),
		"absolute":                append(slices.Clone(good), entry{"/etc/passwd", []byte("x"), tar.TypeReg}),
		"unknown top level":       append(slices.Clone(good), entry{"bin/sh", []byte("x"), tar.TypeReg}),
		"directory bad name":      append(slices.Clone(good), entry{"etc", nil, tar.TypeDir}),
		"duplicate":               append(slices.Clone(good), good[2]),
		"unknown arch":            append(slices.Clone(good), entry{"services/riscv64.tar", []byte("x"), tar.TypeReg}),
		"ui entry missing":        {good[0], good[3], good[4]},
		"missing icon":            {good[0], good[1], good[2], good[4]},
		"image missing":           {good[0], good[2], good[3]},
		"image without services":  append(uiOnlyEntries(t), entry{"services/amd64.tar", []byte("image"), tar.TypeReg}),
		"directory as ui entry":   {good[0], good[1], {"ui/index.html", nil, tar.TypeDir}, good[3], good[4]},
		"file directory clash":    append(slices.Clone(good), entry{"ui/icon.svg/x", []byte("x"), tar.TypeReg}),
	}
	out := make(map[string][]byte, len(cases))
	for name, entries := range cases {
		out[name] = buildTar(t, entries)
	}
	all := withoutEnd(buildTar(t, good))
	out["sparse image"] = slices.Concat(withoutEnd(buildTar(t, good[:4])), sparseEntry("services/amd64.tar", 100<<20), endMarker)
	out["sparse ui file"] = slices.Concat(all, sparseEntry("ui/x.bin", 4096), endMarker)
	// Read as a plain file, this manifest is the good one.
	out["sparse manifest"] = slices.Concat(sparseFile(ManifestFile, good[0].body, int64(len(good[0].body))), buildTar(t, good[1:]))
	out["directory with a size"] = slices.Concat(all, rawHeader("ui/img/", 1, tar.TypeDir), endMarker)
	out["file where a directory entry needs a directory"] = buildTar(t, append(slices.Clone(good),
		entry{"ui/img/icons/", nil, tar.TypeDir}, entry{"ui/img", []byte("x"), tar.TypeReg}))
	return out
}

// bigBadPackages returns invalid packages too large to build on every fuzz
// iteration; only TestWalkRejects uses them.
func bigBadPackages(t testing.TB) map[string][]byte {
	t.Helper()
	return map[string][]byte{
		"ui file too large":     buildTar(t, append(slices.Clone(goodEntries(t)), entry{"ui/big.js", make([]byte, MaxUIFile+1), tar.TypeReg})),
		"ui total":              buildTarUITotal(t),
		"entry count":           buildTarManyEntries(t, MaxEntries+1),
		"image over MaxPackage": buildTarHugeImage(t),
	}
}

func TestWalkRejects(t *testing.T) {
	cases := smallBadPackages(t)
	for name, b := range bigBadPackages(t) {
		cases[name] = b
	}
	for name, b := range cases {
		_, err := Walk(bytes.NewReader(b), func(*Manifest, PackFile, io.Reader) error { return nil })
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

	// Three full blocks plus one short (last) block: unlike the 2-block
	// "swapped" case below, swapping or dropping among the three full,
	// non-last blocks can only be caught by the block counter in the
	// nonce, never by the last-block flag (which is false for all three).
	sealed4 := encrypt(t, bytes.Repeat([]byte{9}, 3*ChunkSize+5))
	swappedNonLast := slices.Concat(sealed4[:header], sealed4[header+block:header+2*block], sealed4[header:header+block], sealed4[header+2*block:])
	droppedMiddle := slices.Concat(sealed4[:header+block], sealed4[header+2*block:])

	attacks := map[string][]byte{
		"flipped bit":          flipped,
		"truncated":            sealed[:header+block],
		"appended":             append(bytes.Clone(sealed), sealed[header:header+block]...),
		"swapped":              append(append(bytes.Clone(sealed[:header]), sealed[header+block:header+2*block]...), sealed[header:header+block]...),
		"swapped non-last":     swappedNonLast,
		"dropped middle block": droppedMiddle,
		"bad magic":            append([]byte("NGCENC2\n"), sealed[8:]...),
		"only header":          sealed[:header],
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
	if _, err := UnwrapKey(nil, priv, "org.sorglos.terminal"); err != ErrEncrypted {
		t.Errorf("nil wrapped key: %v", err)
	}
	if _, err := UnwrapKey(w, nil, "org.sorglos.terminal"); err != ErrEncrypted {
		t.Errorf("nil private key: %v", err)
	}
}

type packVector struct {
	ContentKey  enc.Bytes   `json:"contentKey"`
	Plain       enc.Bytes   `json:"plain"`
	Sealed      enc.Bytes   `json:"sealed"`
	Plain2      enc.Bytes   `json:"plain2"` // spans more than one block
	Sealed2     enc.Bytes   `json:"sealed2"`
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
	plain2 := bytes.Repeat([]byte{0xa5}, ChunkSize+100)
	golden.Check(t, *update, "pack.json", packVector{ContentKey: contentKey, Plain: plain, Sealed: encrypt(t, plain),
		Plain2: plain2, Sealed2: encrypt(t, plain2),
		InstallSeed: bytes.Repeat([]byte{0x91}, 32), App: "org.sorglos.terminal", Wrapped: w})
}

func FuzzWalk(f *testing.F) {
	f.Add(buildTar(f, goodEntries(f)))
	for _, b := range smallBadPackages(f) {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = Walk(bytes.NewReader(b), func(_ *Manifest, _ PackFile, body io.Reader) error {
			_, err := io.Copy(io.Discard, body)
			return err
		})
	})
}

func FuzzDecrypt(f *testing.F) {
	plain := []byte("hello")
	f.Add(encrypt(f, plain))
	f.Fuzz(func(t *testing.T, b []byte) {
		got, err := decrypt(b, contentKey)
		if err == nil && !bytes.Equal(got, plain) {
			t.Errorf("decrypted to a different plaintext than the seed: %q", got)
		}
	})
}
