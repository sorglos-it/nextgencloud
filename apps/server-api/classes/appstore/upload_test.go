package appstore

import (
	"archive/tar"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

// newStores is one API with no store served: everything an upload or a proof
// needs is in its own database and its own trust list. The list is the one of
// shared/testdata/uiproof.json -- Genesis plus the role user, the user alice
// and the store other -- because the proofs of those vectors are signed
// against it and nothing here may sign its own evidence.
func newStores(t *testing.T) (*Stores, *state.State) {
	t.Helper()
	box, err := secrets.NewBox(config.NewSecret(make([]byte, 32)))
	if err != nil {
		t.Fatalf("box: %v", err)
	}
	noiseKey, signKey := trusttest.NodeKeys(0xA1)
	_, apiPin := trusttest.APITLS(0xE1, "api-1.test")
	_, otherStore := trusttest.NodeKeys(0xB2)
	cfg := &config.API{
		Node: "api-1", URL: "wss://api-1.test:7443/tunnel", Listen: ":7443",
		Cert: "/x", CertKey: "/y",
		NoiseKey: config.NewSecret(noiseKey.Private), SignKey: config.NewSecret(signKey.Seed()),
		SecretKey:       config.NewSecret(make([]byte, 32)),
		ChallengeKey:    config.NewSecret(bytes.Repeat([]byte{7}, 32)),
		SessionLifetime: 43200, IdleTimeout: 1800,
	}
	s := state.New(cfg, memstore.New(), box, apiPin)
	s.Now = func() int64 { return testNow }
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	l.Roles = append(l.Roles, trust.Role{Name: "user", Level: trust.LevelOff})
	l.Users = append(l.Users, trust.User{ID: trusttest.ID(0x30), Name: "alice", Roles: []string{"user"},
		Keys: []proof.UserKey{trusttest.NewSigner(0x31).Key}})
	l.Stores = append(l.Stores, trust.Store{ID: "other", Name: "Other Store",
		URL: "https://store.other.example", Key: enc.Bytes(otherStore.Public().(ed25519.PublicKey))})
	if err := s.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallation,
		Data: []byte(l.Installation), Updated: testNow}); err != nil {
		t.Fatalf("installation: %v", err)
	}
	if _, err := s.AddVersion(ctx, admin.Sign(t, l)); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	return New(s), s
}

// nextVersion is the list of the newest version, ready to be changed and
// signed again. It reads the stored version rather than keeping one around, so
// a case can ask for it after any number of changes.
func nextVersion(t *testing.T, s *state.State) *trust.List {
	t.Helper()
	v, err := s.Trust(ctx)
	if err != nil {
		t.Fatalf("Trust: %v", err)
	}
	b, err := s.DB.BlobGet(ctx, dbapi.BlobTrust, strconv.FormatUint(v.List.Seq, 10))
	if err != nil {
		t.Fatalf("version row: %v", err)
	}
	sg, err := trust.ParseSigned(b.Data)
	if err != nil {
		t.Fatalf("version row: %v", err)
	}
	return trusttest.Next(t, sg)
}

// packEntry is one entry of a package this test builds. The attacks below are
// built here and not read from vectors, because shared/testdata has no
// unsafe packages: every case changes exactly one entry of goodPackage, so a
// refusal cannot come from a package that was broken to begin with.
type packEntry struct {
	name string
	body []byte
	kind byte
	pax  map[string]string
}

func buildPackage(t *testing.T, entries []packEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)),
			Typeflag: e.kind, PAXRecords: e.pax, Format: tar.FormatPAX}
		switch e.kind {
		case tar.TypeDir:
			h.Mode, h.Size = 0o755, 0
		case tar.TypeSymlink, tar.TypeLink:
			h.Linkname, h.Size = "/etc/shadow", 0
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}
		if e.kind == tar.TypeReg {
			if _, err := w.Write(e.body); err != nil {
				t.Fatalf("%s: %v", e.name, err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// rawHeader is a 512-byte ustar header. A sparse entry cannot come from
// archive/tar's writer: it drops the GNU.sparse.* records of a PAX header, and
// an entry of type 'S' its own reader refuses as a broken header -- which
// would measure archive/tar and not the rule of the contract.
func rawHeader(name string, size int64, flag byte) []byte {
	h := make([]byte, 512)
	copy(h, name)
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

// pad512 fills b up to a whole block.
func pad512(b []byte) []byte {
	return append(b, make([]byte, (512-len(b)%512)%512)...)
}

func paxRecord(key, value string) string {
	rest := " " + key + "=" + value + "\n"
	return strconv.Itoa(len(rest)+len(strconv.Itoa(len(rest)))) + rest
}

// sparsePackage is goodEntries with a PAX 1.0 sparse file appended: it claims
// size bytes and carries one block, so archive/tar alone would hand out size
// bytes of holes. The 1024 zero bytes at the end of the good archive are its
// end marker and are cut away, so the entry is part of the same archive.
func sparsePackage(t *testing.T, name string, size int64) []byte {
	t.Helper()
	good := buildPackage(t, goodEntries(t))
	out := good[: len(good)-1024 : len(good)-1024]
	pax := paxRecord("GNU.sparse.major", "1") + paxRecord("GNU.sparse.minor", "0") +
		paxRecord("GNU.sparse.name", name) + paxRecord("GNU.sparse.realsize", strconv.FormatInt(size, 10))
	out = append(out, rawHeader("PaxHeaders/0", int64(len(pax)), tar.TypeXHeader)...)
	out = append(out, pad512([]byte(pax))...)
	sparseMap := pad512([]byte("0\n")) // no segments: the whole file is a hole
	out = append(out, rawHeader(name, int64(len(sparseMap)), tar.TypeReg)...)
	out = append(out, sparseMap...)
	return append(out, make([]byte, 1024)...)
}

// plainRawPackage is goodEntries with one more file appended the same way
// sparsePackage appends its entry, only without the sparse records. It has to
// be taken, or the headers written by hand above are what refuses and not the
// rule they are there to reach.
func plainRawPackage(t *testing.T, name string) []byte {
	t.Helper()
	good := buildPackage(t, goodEntries(t))
	out := good[: len(good)-1024 : len(good)-1024]
	body := pad512([]byte("a{}\n"))
	out = append(out, rawHeader(name, int64(len(body)), tar.TypeReg)...)
	out = append(out, body...)
	return append(out, make([]byte, 1024)...)
}

// testManifest is the manifest of the package this test builds: an app with a
// UI and no service, so no image has to be carried along.
func testManifest() *store.Manifest {
	return &store.Manifest{ID: "org.example.notes", Name: "Notizen", Version: "0.1.0",
		Publisher: "Sorglos IT", UI: &store.UI{Entry: "index.html", Width: 720, Height: 480}}
}

func manifestEntry(t *testing.T, m *store.Manifest) packEntry {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return packEntry{name: store.ManifestFile, body: b, kind: tar.TypeReg}
}

// goodEntries is a package store.Walk takes.
func goodEntries(t *testing.T) []packEntry {
	return []packEntry{
		manifestEntry(t, testManifest()),
		{name: "ui", kind: tar.TypeDir},
		{name: "ui/index.html", body: []byte("<!doctype html><title>Notizen</title>"), kind: tar.TypeReg},
		{name: "ui/app.css", body: []byte("body{margin:0}\n"), kind: tar.TypeReg},
	}
}

// uploadVectors is shared/testdata/packui.json: whole packages, the canonical
// ui-tar of each and its SHA-256, as ngc-store, the controller and the shell
// compute it.
type uploadVectors []struct {
	Name    string    `json:"name"`
	Package enc.Bytes `json:"package"`
	UI      enc.Bytes `json:"ui"`   // the canonical ui-tar
	Hash    enc.Bytes `json:"hash"` // its SHA-256
}

// wantPackages is the number of packages in packui.json, so a vector file that
// lost one does not read as a full run.
const wantPackages = 3

func loadUploadVectors(t *testing.T) uploadVectors {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "shared", "testdata", "packui.json"))
	if err != nil {
		t.Fatalf("packui.json: %v", err)
	}
	var v uploadVectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("packui.json: %v", err)
	}
	if len(v) != wantPackages {
		t.Fatalf("vectors: %d packages, want %d", len(v), wantPackages)
	}
	return v
}

// manifestOfPackage reads the first entry of a package with archive/tar and
// encoding/json alone. The ID and the version of the result must come from
// somewhere other than the code under test: store.PackageUI is what Upload
// uses, so asking it would be asking the answer.
func manifestOfPackage(t *testing.T, pkg []byte) (string, string) {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(pkg))
	h, err := tr.Next()
	if err != nil || h.Name != store.ManifestFile {
		t.Fatalf("first entry: %v", err)
	}
	var m struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(tr).Decode(&m); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	return m.ID, m.Version
}

// TestUploadReadsThePackage: every package of packui.json through Upload. The
// result names the ID and the version of the manifest, the SHA-256 of the file
// and the UI hash of the vectors, and the canonical ui-tar lies under
// dbapi.BlobUI byte for byte.
func TestUploadReadsThePackage(t *testing.T) {
	for _, c := range loadUploadVectors(t) {
		st, s := newStores(t)
		id, version := manifestOfPackage(t, c.Package)
		res, err := st.Upload(ctx, bytes.NewReader(c.Package))
		if err != nil {
			t.Errorf("%s: Upload: %v", c.Name, err)
			continue
		}
		sum := sha256.Sum256(c.Package)
		if res.ID != id || res.Version != version {
			t.Errorf("%s: read %q %q, want %q %q", c.Name, res.ID, res.Version, id, version)
		}
		if !bytes.Equal(res.SHA256, sum[:]) {
			t.Errorf("%s: SHA-256 of the file is not the file's", c.Name)
		}
		if !bytes.Equal(res.UI, c.Hash) {
			t.Errorf("%s: UI hash %x, want %x", c.Name, []byte(res.UI), []byte(c.Hash))
		}
		b, err := s.DB.BlobGet(ctx, dbapi.BlobUI, id+"@"+version)
		if err != nil {
			t.Errorf("%s: no ui-tar: %v", c.Name, err)
			continue
		}
		if !bytes.Equal(b.Data, c.UI) {
			t.Errorf("%s: the kept ui-tar is not the canonical one (%d bytes against %d)",
				c.Name, len(b.Data), len(c.UI))
		}
	}
}

// TestUploadRefusesAnUnsafeArchive: absolute paths, "..", links, sparse files
// and more directories than store.MaxDirs are store.Walk's refusals, and what
// this case measures is that they reach Upload and that nothing is kept when
// they do. Each package differs from the one that is taken above it by one
// entry, so the builder itself is not what refuses.
func TestUploadRefusesAnUnsafeArchive(t *testing.T) {
	good := buildPackage(t, goodEntries(t))
	st, s := newStores(t)
	if _, err := st.Upload(ctx, bytes.NewReader(good)); err != nil {
		t.Fatalf("the package this case changes is itself refused: %v", err)
	}
	if _, err := s.DB.BlobGet(ctx, dbapi.BlobUI, "org.example.notes@0.1.0"); err != nil {
		t.Fatalf("the package was taken, yet no ui-tar: %v", err)
	}
	deep := goodEntries(t)
	for i := range store.MaxDirs {
		deep = append(deep, packEntry{name: "ui/d" + strconv.Itoa(i), kind: tar.TypeDir},
			packEntry{name: "ui/d" + strconv.Itoa(i) + "/x.css", body: []byte("a{}"), kind: tar.TypeReg})
	}
	// The raw headers of the sparse case are sound: the same entry without
	// the sparse records is taken, so what refuses below is the rule and not
	// a header written by hand.
	st, s = newStores(t)
	if _, err := st.Upload(ctx, bytes.NewReader(plainRawPackage(t, "ui/holes.bin"))); err != nil {
		t.Fatalf("the hand-written header is itself refused: %v", err)
	}
	unsafe := []struct {
		name string
		pkg  []byte
	}{
		{"absolute path", buildPackage(t, append(goodEntries(t),
			packEntry{name: "/etc/cron.d/ngc", body: []byte("x"), kind: tar.TypeReg}))},
		{"dot dot", buildPackage(t, append(goodEntries(t),
			packEntry{name: "ui/../../etc/passwd", body: []byte("x"), kind: tar.TypeReg}))},
		{"symlink", buildPackage(t, append(goodEntries(t),
			packEntry{name: "ui/shadow", kind: tar.TypeSymlink}))},
		{"hard link", buildPackage(t, append(goodEntries(t),
			packEntry{name: "ui/shadow", kind: tar.TypeLink}))},
		{"sparse file", sparsePackage(t, "ui/holes.bin", 100<<20)},
		{"beyond MaxDirs directories", buildPackage(t, deep)},
	}
	for _, u := range unsafe {
		st, s := newStores(t)
		res, err := st.Upload(ctx, bytes.NewReader(u.pkg))
		if err == nil {
			t.Errorf("%s: taken, result %+v", u.name, res)
		}
		blobs, ferr := s.DB.BlobFrom(ctx, dbapi.BlobUI, 0)
		if ferr != nil || len(blobs) != 0 {
			t.Errorf("%s: refused, yet %d ui-tars were kept (%v)", u.name, len(blobs), ferr)
		}
	}
}

// TestUploadRefusesBeyondMaxPackage: the bound holds on both sides of itself.
// The package of the case is one store.Walk takes, with 2 KiB of padding
// behind it, so what the two runs differ in is the length of the file and
// nothing else -- a package the limit cuts in half would be refused by its own
// broken shape instead.
func TestUploadRefusesBeyondMaxPackage(t *testing.T) {
	pkg := append(buildPackage(t, goodEntries(t)), make([]byte, 2048)...)
	st, s := newStores(t)
	st.MaxUpload = int64(len(pkg))
	res, err := st.Upload(ctx, bytes.NewReader(pkg))
	if err != nil {
		t.Fatalf("at the bound: %v", err)
	}
	sum := sha256.Sum256(pkg)
	// The checksum is of the whole file, padding included: the shell
	// computes it over the file it sent and refuses a result that differs.
	if !bytes.Equal(res.SHA256, sum[:]) {
		t.Fatalf("the checksum left the padding out")
	}
	if _, err := s.DB.BlobGet(ctx, dbapi.BlobUI, "org.example.notes@0.1.0"); err != nil {
		t.Fatalf("at the bound: no ui-tar: %v", err)
	}
	st, s = newStores(t)
	st.MaxUpload = int64(len(pkg)) - 1
	if _, err := st.Upload(ctx, bytes.NewReader(pkg)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("one byte over the bound: got %v, want ErrTooLarge", err)
	}
	blobs, err := s.DB.BlobFrom(ctx, dbapi.BlobUI, 0)
	if err != nil || len(blobs) != 0 {
		t.Fatalf("one byte over the bound: %d ui-tars kept (%v)", len(blobs), err)
	}
}

// TestTheUploadBoundIsTheContractsOwn: the bound no case overrides. MaxUpload
// is there so a case does not have to send 4 GiB, and that is exactly why the
// value that holds in service is the one nothing here would otherwise measure.
// store.MaxPackage is the last word: a caller that sets more gets that bound,
// not its own.
func TestTheUploadBoundIsTheContractsOwn(t *testing.T) {
	st, _ := newStores(t)
	if st.MaxUpload != store.MaxPackage {
		t.Fatalf("New sets MaxUpload to %d, want store.MaxPackage (%d)", st.MaxUpload, store.MaxPackage)
	}
	for _, v := range []int64{0, -1, store.MaxPackage + 1, 1 << 62} {
		st.MaxUpload = v
		if got := st.uploadLimit(); got != store.MaxPackage {
			t.Errorf("MaxUpload %d: limit %d, want %d", v, got, store.MaxPackage)
		}
	}
}

// zeroes is the body of a large image entry, made as it is read: a package of
// this size as a slice would be in this test's own memory, and the point of
// the case below is that it is nowhere.
type zeroes struct{}

func (zeroes) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// streamPackage writes a package with one image entry of size bytes into a
// pipe and returns its read end.
func streamPackage(t *testing.T, size int64) io.Reader {
	t.Helper()
	m := testManifest()
	m.Services = []store.Service{{Name: "notes", Command: []string{"/usr/local/bin/ngc-notes"}}}
	head := goodEntries(t)
	head[0] = manifestEntry(t, m)
	pr, pw := io.Pipe()
	go func() {
		w := tar.NewWriter(pw)
		var err error
		for _, e := range head {
			h := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: e.kind}
			if e.kind == tar.TypeDir {
				h.Mode, h.Size = 0o755, 0
			}
			if err = w.WriteHeader(h); err != nil {
				break
			}
			if e.kind == tar.TypeReg {
				if _, err = w.Write(e.body); err != nil {
					break
				}
			}
		}
		if err == nil {
			err = w.WriteHeader(&tar.Header{Name: "services/amd64.tar", Mode: 0o644,
				Size: size, Typeflag: tar.TypeReg})
		}
		if err == nil {
			_, err = io.CopyN(w, zeroes{}, size)
		}
		if err == nil {
			err = w.Close()
		}
		_ = pw.CloseWithError(err)
	}()
	return pr
}

// peakHeap runs fn and returns the largest HeapAlloc seen while it ran, above
// what was in use before it started.
func peakHeap(fn func()) uint64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)
	base := m.HeapAlloc
	done := make(chan struct{})
	peak := make(chan uint64, 1)
	go func() {
		var seen uint64
		var s runtime.MemStats
		for {
			runtime.ReadMemStats(&s)
			if s.HeapAlloc > base && s.HeapAlloc-base > seen {
				seen = s.HeapAlloc - base
			}
			select {
			case <-done:
				peak <- seen
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()
	fn()
	close(done)
	return <-peak
}

// uploadHeld is the ceiling the case below holds Upload to: a number of its
// own, not one derived from the package, or it would grow along with it.
const uploadHeld = 48 << 20

// TestUploadHoldsNoPackage: store.MaxPackage is 4 GiB, so a package must never
// be in memory as a whole -- neither to be hashed nor to be walked. Three
// sizes sixteen apart say more than one: a single measurement gives a height,
// only a row gives a slope. The control reads the same stream into a slice and
// has to be seen, or the measurement would say nothing about the runs above
// it.
func TestUploadHoldsNoPackage(t *testing.T) {
	sizes := []int64{32 << 20, 128 << 20, 512 << 20}
	var held []uint64
	for _, size := range sizes {
		st, _ := newStores(t)
		r := streamPackage(t, size)
		var err error
		peak := peakHeap(func() { _, err = st.Upload(ctx, r) })
		if err != nil {
			t.Fatalf("%d bytes: Upload: %v", size, err)
		}
		held = append(held, peak)
		t.Logf("package of %d MiB: %d MiB held", size>>20, peak>>20)
		if peak > uploadHeld {
			t.Errorf("package of %d MiB: %d MiB held, more than the %d MiB ceiling",
				size>>20, peak>>20, uploadHeld>>20)
		}
	}
	// The null control: the same instrument over a reader that does hold its
	// input must report it. Without this the three numbers above could be the
	// instrument reporting nothing at all.
	control := peakHeap(func() {
		b, err := io.ReadAll(streamPackage(t, sizes[1]))
		if err != nil || int64(len(b)) < sizes[1] {
			t.Errorf("control: %d bytes, %v", len(b), err)
		}
		runtime.KeepAlive(b)
	})
	t.Logf("control holding %d MiB: %d MiB held", sizes[1]>>20, control>>20)
	if control <= uploadHeld {
		t.Fatalf("the control held only %d MiB: this measurement cannot see a package in memory",
			control>>20)
	}
	if held[2] > 2*held[0]+(1<<20) {
		t.Errorf("sixteen times the package held %d MiB against %d MiB: it grows with the input",
			held[2]>>20, held[0]>>20)
	}
}

// TestUploadRefusesTheRealBound: the bound that holds in service, run once at
// its real value. The case above nails both sides of the mechanism at a small
// limit; this one shows that the limit nothing overrides is the contract's 4
// GiB and that a file past it is refused there too -- a package of just under
// 4 GiB with 2 MiB behind it, through the default limit.
//
// The refusal is ErrTooLarge and not store.ErrPackage, so it is this bound
// that refused and not store.Walk's own reading limit: the way to the line
// under measurement was open. ErrTooLarge and not ErrStore either, because
// task 22 maps a bound that was passed to E_TOOLARGE and an unusable store to
// E_UNREACHABLE (CodeFor), and one error for both would send an uploader
// looking for another store. It costs about four seconds, which is what reading 4
// GiB of nothing through SHA-256 costs.
func TestUploadRefusesTheRealBound(t *testing.T) {
	st, s := newStores(t)
	if st.MaxUpload != store.MaxPackage {
		t.Fatalf("MaxUpload is %d, not the contract's bound", st.MaxUpload)
	}
	pkg := io.MultiReader(streamPackage(t, store.MaxPackage-(1<<20)), io.LimitReader(zeroes{}, 2<<20))
	if _, err := st.Upload(ctx, pkg); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("got %v, want ErrTooLarge", err)
	}
	blobs, err := s.DB.BlobFrom(ctx, dbapi.BlobUI, 0)
	if err != nil || len(blobs) != 0 {
		t.Fatalf("%d ui-tars kept (%v)", len(blobs), err)
	}
}
