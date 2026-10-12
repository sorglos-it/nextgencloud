package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"testing/iotest"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The release of shared/testdata/release: one file, part ngc-web, arch "all".
const (
	testVer   = "1.1.0"
	testSeq   = 3
	testApp   = "org.sorglos.notes"
	testStore = "global"
	testAppV  = "1.2.0"
)

// releaseDir is where the signed release of the test data lies, from this
// package's directory.
const releaseDir = "../../../../shared/testdata/release"

// --- the halves of state.Store this package uses, as a stand-in ------------

type fakeStore struct {
	apps    map[string]dbapi.App
	staged  map[string]state.Staged
	appErr  error
	putErr  error
	readErr error
	puts    int
}

func newStore() *fakeStore {
	return &fakeStore{apps: map[string]dbapi.App{}, staged: map[string]state.Staged{}}
}

func (f *fakeStore) App(_ context.Context, id string) (dbapi.App, error) {
	if f.appErr != nil {
		return dbapi.App{}, f.appErr
	}
	a, ok := f.apps[id]
	if !ok {
		return dbapi.App{}, dbapi.ErrNotFound
	}
	return a, nil
}

func (f *fakeStore) Staged(_ context.Context, target string) (state.Staged, error) {
	if f.readErr != nil {
		return state.Staged{}, f.readErr
	}
	st, ok := f.staged[target]
	if !ok {
		return state.Staged{}, dbapi.ErrNotFound
	}
	return st, nil
}

func (f *fakeStore) PutStaged(_ context.Context, st state.Staged) error {
	f.puts++
	if f.putErr != nil {
		return f.putErr
	}
	f.staged[st.Target] = st
	return nil
}

type fakeReports struct{ got []store.UpdateReport }

func (r *fakeReports) Update(u store.UpdateReport) { r.got = append(r.got, u) }

// last returns the newest report, or the zero value.
func (r *fakeReports) last() store.UpdateReport {
	if len(r.got) == 0 {
		return store.UpdateReport{}
	}
	return r.got[len(r.got)-1]
}

// --- fixtures --------------------------------------------------------------

func sha(b []byte) []byte { s := sha256.Sum256(b); return s[:] }

// signer returns a deterministic Ed25519 key, so a case can sign with the
// right one and with a foreign one.
func signer(fill byte) (ed25519.PublicKey, ed25519.PrivateKey) {
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{fill}, ed25519.SeedSize))
	return priv.Public().(ed25519.PublicKey), priv
}

// releaseKey reads the release key of the test data out of its PEM file, the
// same key shared/testdata/release/verify.sh hands to openssl.
func releaseKey(t testing.TB) ed25519.PublicKey {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(releaseDir, "release-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	blk, _ := pem.Decode(b)
	if blk == nil || blk.Type != "PUBLIC KEY" {
		t.Fatal("release-key.pem is no PUBLIC KEY block")
	}
	pub, err := x509.ParsePKIXPublicKey(blk.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := pub.(ed25519.PublicKey)
	if !ok {
		t.Fatalf("release-key.pem holds %T, not an Ed25519 key", pub)
	}
	return key
}

// testRelease reads the signed release and the file it names out of the test
// data, and checks that the two still belong together: a case that measures a
// bent checksum has to know that the unbent one is right.
func testRelease(t testing.TB) (sign.Envelope, []byte) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(releaseDir, "release.json"))
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.ParseEnvelope(b, sign.MaxPayload)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.ReadFile(filepath.Join(releaseDir, "ngc-web-all.tar"))
	if err != nil {
		t.Fatal(err)
	}
	rel, err := store.VerifyRelease(env, releaseKey(t), 0)
	if err != nil {
		t.Fatalf("the test data itself does not verify: %v", err)
	}
	if rel.Version != testVer || rel.Seq != testSeq || len(rel.Files) != 1 ||
		rel.Files[0].Part != trust.AutoWeb || rel.Files[0].Arch != "all" ||
		!bytes.Equal(rel.Files[0].SHA256, sha(file)) || rel.Files[0].Size != int64(len(file)) {
		t.Fatalf("the test data is not the release these cases were written for: %+v", rel)
	}
	return env, file
}

// fixture is one call to Stage. Every case bends one field and calls run, so
// what the case measures is that one thing.
type fixture struct {
	t      testing.TB
	target string
	ver    string
	file   []byte
	tr     core.Transfer
	st     *fakeStore
	rep    *fakeReports
	dir    string
	deps   Deps
}

// releaseFixture is an NGC part Stage must lay ready: the signed release of
// the test data, its file, and a manager whose release key is the one that
// signed it.
func releaseFixture(t testing.TB) *fixture {
	t.Helper()
	env, file := testRelease(t)
	f := &fixture{t: t, target: trust.AutoWeb, ver: testVer, file: file,
		st: newStore(), rep: &fakeReports{}, dir: t.TempDir()}
	f.tr = core.Transfer{Release: &env, Size: int64(len(file)), SHA256: sha(file)}
	f.deps = Deps{State: f.st, List: &trust.List{}, ReleaseKey: releaseKey(t),
		StageDir: f.dir, Arch: "all", Max: store.MaxPackage, Report: f.rep,
		Now: func() int64 { return 1790000500 }}
	return f
}

// appFixture is an app update Stage must lay ready: a catalogue of the store
// the app was activated from, signed with that store's key out of the trust
// list.
func appFixture(t testing.TB) *fixture {
	t.Helper()
	pkg := []byte("package of the new version")
	pub, priv := signer(1)
	f := &fixture{t: t, target: testApp, ver: testAppV, file: pkg,
		st: newStore(), rep: &fakeReports{}, dir: t.TempDir()}
	f.st.apps[testApp] = dbapi.App{ID: testApp, Version: "1.1.0", Store: testStore,
		Active: true, Grants: []string{"net"}}
	cat := store.Catalog{Store: testStore, Seq: 9, Issued: 1, Apps: []store.Entry{{
		ID: testApp, Name: "Notizen", Publisher: "Sorglos IT",
		Versions: []store.Package{{Version: testAppV, URL: "pkg.tar", Size: int64(len(pkg)),
			SHA256: sha(pkg), UI: sha([]byte("ui"))}}}}}
	f.tr = core.Transfer{Catalog: envelopeOf(t, priv, sign.PurposeCatalog, cat),
		Store: testStore, Size: int64(len(pkg)), SHA256: sha(pkg)}
	f.deps = Deps{State: f.st, List: &trust.List{Stores: []trust.Store{{ID: testStore,
		Name: "Global", URL: "https://store.example.org", Key: enc.Bytes(pub)}}},
		ReleaseKey: releaseKey(t), StageDir: f.dir, Arch: goruntime.GOARCH,
		Max: store.MaxPackage, Report: f.rep, Now: func() int64 { return 1790000500 }}
	return f
}

// envelopeOf signs v for purpose, the way a store signs its catalogue.
func envelopeOf(t testing.TB, priv ed25519.PrivateKey, purpose string, v any) *sign.Envelope {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.Sign(priv, purpose, b)
	if err != nil {
		t.Fatal(err)
	}
	return &env
}

// decision is the checked channel of a stage: no user, no task, the params in
// the OPEN (core.WithoutUser).
func (f *fixture) decision() access.Decision {
	f.t.Helper()
	a, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActStage)
	if !ok {
		f.t.Fatal("the contract has no stage action")
	}
	params, err := json.Marshal(core.StageParams{Target: f.target, Version: f.ver})
	if err != nil {
		f.t.Fatal(err)
	}
	return access.Decision{
		Opened: tunnel.Opened{Open: wire.Open{App: core.AppStore, Service: core.ServiceController,
			Action: core.ActStage, Params: params}},
		Action: a, Core: true,
	}
}

// body is what the API sends: the transfer header, then the file.
func (f *fixture) body() io.Reader {
	f.t.Helper()
	var buf bytes.Buffer
	if err := core.WriteTransfer(&buf, f.tr); err != nil {
		f.t.Fatal(err)
	}
	buf.Write(f.file)
	return &buf
}

func (f *fixture) run() ([]byte, error) {
	f.t.Helper()
	return New(f.deps).Stage(context.Background(), f.decision(), f.body())
}

// staged is the path the file would lie at.
func (f *fixture) staged() string { return f.dir + "/" + f.target }

// dirEntries lists what lies in the staging directory, so a case can say that
// nothing at all was left behind - a half-written temporary file included.
func (f *fixture) dirEntries() []string {
	f.t.Helper()
	ents, err := os.ReadDir(f.dir)
	if err != nil {
		f.t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

// --- the cases of the brief ------------------------------------------------

func TestStageVerifiesTheReleaseAndRemembersWhatItSaw(t *testing.T) {
	f := releaseFixture(t)
	out, err := f.run()
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if string(out) != "{}" {
		t.Errorf("result %q, want {}", out)
	}
	got, err := os.ReadFile(f.staged())
	if err != nil {
		t.Fatalf("the file was not laid ready: %v", err)
	}
	if !bytes.Equal(got, f.file) {
		t.Errorf("the file on disk is not the one that was sent")
	}
	st := f.st.staged[trust.AutoWeb]
	if st.Target != trust.AutoWeb || st.Version != testVer || st.Size != int64(len(f.file)) ||
		!bytes.Equal(st.SHA256, sha(f.file)) || st.File != f.staged() ||
		st.State != store.UpdateDownloaded || st.Time != 1790000500 {
		t.Errorf("remembered %+v", st)
	}
	if !bytes.Equal(st.Signer, releaseKey(t)) {
		t.Errorf("signer %x, want the release key %x", st.Signer, releaseKey(t))
	}
	if st.Seq != testSeq {
		t.Errorf("seq %d, want %d", st.Seq, testSeq)
	}
	if r := f.rep.last(); r.Target != trust.AutoWeb || r.Version != testVer ||
		r.State != store.UpdateDownloaded {
		t.Errorf("report %+v", r)
	}
}

func TestStageVerifiesTheCatalogOfTheAppsOwnStore(t *testing.T) {
	f := appFixture(t)
	if _, err := f.run(); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	st := f.st.staged[testApp]
	if st.Target != testApp || st.Version != testAppV || st.Seq != 9 ||
		st.State != store.UpdateDownloaded || st.File != f.staged() {
		t.Errorf("remembered %+v", st)
	}
	pub, _ := signer(1)
	if !bytes.Equal(st.Signer, pub) {
		t.Errorf("signer %x, want the store key %x", st.Signer, pub)
	}
}

func TestStageRefusesAnOlderSeq(t *testing.T) {
	// Zwischenpruefung: the same fixture with an older number lying ready goes
	// through, so the wall this case measures is the only one in the way.
	open := releaseFixture(t)
	open.st.staged[trust.AutoWeb] = state.Staged{Target: trust.AutoWeb, Version: "1.0.0",
		SHA256: sha([]byte("old")), Size: 3, Seq: testSeq - 1, File: open.dir + "/" + trust.AutoWeb,
		State: store.UpdateDownloaded, Time: 1}
	if _, err := open.run(); err != nil {
		t.Fatalf("the way to the seq check is not open: %v", err)
	}

	f := releaseFixture(t)
	f.st.staged[trust.AutoWeb] = state.Staged{Target: trust.AutoWeb, Version: "1.2.0",
		SHA256: sha([]byte("newer")), Size: 5, Seq: testSeq + 1, File: f.dir + "/" + trust.AutoWeb,
		State: store.UpdateDownloaded, Time: 1}
	_, err := f.run()
	if !errors.Is(err, store.ErrRollback) {
		t.Fatalf("err %v, want store.ErrRollback", err)
	}
	if _, serr := os.Stat(f.staged()); !os.IsNotExist(serr) {
		t.Errorf("a file was laid ready all the same: %v", serr)
	}
	if names := f.dirEntries(); len(names) != 0 {
		t.Errorf("left behind %v", names)
	}
	if st := f.st.staged[trust.AutoWeb]; st.Seq != testSeq+1 {
		t.Errorf("the remembered update was overwritten: %+v", st)
	}
}

func TestStageRefusesAWrongSignature(t *testing.T) {
	// Zwischenpruefung: the unbent fixture is accepted (the case above), so
	// only the signature is in the way here.
	f := releaseFixture(t)
	_, foreign := signer(9)
	env, err := sign.Sign(foreign, sign.PurposeRelease, f.tr.Release.Payload)
	if err != nil {
		t.Fatal(err)
	}
	f.tr.Release = &env
	_, err = f.run()
	if !errors.Is(err, ErrVerify) {
		t.Fatalf("err %v, want ErrVerify", err)
	}
	if _, serr := os.Stat(f.staged()); !os.IsNotExist(serr) {
		t.Errorf("a file was laid ready all the same: %v", serr)
	}
	if r := f.rep.last(); r.State != store.UpdateRejected || r.Target != trust.AutoWeb {
		t.Errorf("report %+v, want %s", r, store.UpdateRejected)
	}
	if r := f.rep.last(); r.Message == "" || !store.ValidText(r.Message, wire.MaxMessageText) {
		t.Errorf("message %q is not a text for admins", r.Message)
	}
	if f.st.puts != 0 {
		t.Errorf("%d updates were remembered", f.st.puts)
	}
}

func TestStageRefusesAWrongChecksum(t *testing.T) {
	f := releaseFixture(t)
	// The header still names the checksum of the release entry, so every wall
	// before the bytes is open: only what arrives is another file.
	f.file = bytes.Repeat([]byte("x"), len(f.file))
	if bytes.Equal(sha(f.file), f.tr.SHA256) {
		t.Fatal("the bent file has the same checksum")
	}
	_, err := f.run()
	if !errors.Is(err, ErrVerify) {
		t.Fatalf("err %v, want ErrVerify", err)
	}
	if _, serr := os.Stat(f.staged()); !os.IsNotExist(serr) {
		t.Errorf("the refused file stayed: %v", serr)
	}
	if names := f.dirEntries(); len(names) != 0 {
		t.Errorf("left behind %v", names)
	}
	if f.st.puts != 0 {
		t.Errorf("%d updates were remembered", f.st.puts)
	}
	if r := f.rep.last(); r.State != store.UpdateRejected {
		t.Errorf("report %+v", r)
	}
}

func TestStageIsWithoutUser(t *testing.T) {
	a, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActStage)
	if !ok {
		t.Fatal("the contract has no stage action")
	}
	if !core.WithoutUser(a) {
		t.Error("stage is not a channel without a user")
	}
	if core.ShellOpens(a) {
		t.Error("the shell may open stage, so an OPEN of it is not E_FORBIDDEN in access.Check")
	}
	// And the call itself runs on a decision that carries neither a user nor a
	// task: the params of the OPEN are all there is.
	f := releaseFixture(t)
	d := f.decision()
	if d.Task != nil || d.User != "" || d.Opened.Session != nil {
		t.Fatal("this fixture is not a channel without a user")
	}
	if _, err := New(f.deps).Stage(context.Background(), d, f.body()); err != nil {
		t.Fatalf("Stage: %v", err)
	}
}

// --- what a bent update must not reach ------------------------------------

func TestStageRefusesAnotherVersionThanTheReleaseNames(t *testing.T) {
	f := releaseFixture(t)
	f.ver = "9.9.9" // the OPEN is not signed; the release says 1.1.0
	if _, err := f.run(); !errors.Is(err, ErrVerify) {
		t.Fatalf("err %v, want ErrVerify", err)
	}
	if names := f.dirEntries(); len(names) != 0 {
		t.Errorf("left behind %v", names)
	}
}

// partFixture is a release of our own key that carries ngc-api for one
// architecture only, so a case can ask for the other one.
func partFixture(t testing.TB, arch string) *fixture {
	t.Helper()
	bin := []byte("the ngc-api of the new version")
	pub, priv := signer(2)
	f := &fixture{t: t, target: trust.AutoAPI, ver: testVer, file: bin,
		st: newStore(), rep: &fakeReports{}, dir: t.TempDir()}
	rel := store.Release{Seq: 4, Issued: 1790000000, Version: testVer,
		Files: []store.File{{Part: trust.AutoAPI, Arch: arch, URL: "ngc-api-" + arch + ".tar",
			Size: int64(len(bin)), SHA256: sha(bin)}}}
	f.tr = core.Transfer{Release: envelopeOf(t, priv, sign.PurposeRelease, rel),
		Size: int64(len(bin)), SHA256: sha(bin)}
	f.deps = Deps{State: f.st, List: &trust.List{}, ReleaseKey: pub, StageDir: f.dir,
		Arch: arch, Max: store.MaxPackage, Report: f.rep,
		Now: func() int64 { return 1790000500 }}
	return f
}

func TestStageRefusesAPartTheReleaseDoesNotCarryForThisArch(t *testing.T) {
	// Zwischenpruefung: the same release on the machine it is built for goes
	// through, so the architecture is the only thing in the way below.
	open := partFixture(t, "amd64")
	if _, err := open.run(); err != nil {
		t.Fatalf("the way to the architecture check is not open: %v", err)
	}

	f := partFixture(t, "amd64")
	f.deps.Arch = "arm64" // the signed release carries amd64 only
	if _, err := f.run(); !errors.Is(err, ErrVerify) {
		t.Fatalf("err %v, want ErrVerify", err)
	}
	if names := f.dirEntries(); len(names) != 0 {
		t.Errorf("left behind %v", names)
	}
}

// TestStageTakesTheWebInterfaceAsArchAll: ngc-web has no machine code, so the
// architecture of the server must not decide anything about it.
func TestStageTakesTheWebInterfaceAsArchAll(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		f := releaseFixture(t)
		f.deps.Arch = arch
		if _, err := f.run(); err != nil {
			t.Errorf("arch %s: %v", arch, err)
		}
	}
}

func TestStageRefusesACatalogFromAnotherStore(t *testing.T) {
	f := appFixture(t)
	f.st.apps[testApp] = dbapi.App{ID: testApp, Version: "1.1.0", Store: "other",
		Active: true, Grants: []string{"net"}}
	if _, err := f.run(); !errors.Is(err, ErrVerify) {
		t.Fatalf("err %v, want ErrVerify", err)
	}
	if names := f.dirEntries(); len(names) != 0 {
		t.Errorf("left behind %v", names)
	}
}

// TestStageRefusesAnUploadedApp: an app an admin uploaded has no store that
// signs anything, so no update can be laid ready for it over this channel.
func TestStageRefusesAnUploadedApp(t *testing.T) {
	f := appFixture(t)
	f.st.apps[testApp] = dbapi.App{ID: testApp, Version: "1.1.0", Store: core.LocalStore,
		Active: true, Grants: []string{"net"}}
	// As the API sends it for an uploaded app: no catalogue and no store
	// (core.Transfer.Store).
	f.tr = core.Transfer{Size: int64(len(f.file)), SHA256: sha(f.file)}
	if _, err := f.run(); !errors.Is(err, ErrVerify) {
		t.Fatalf("err %v, want ErrVerify", err)
	}
	if names := f.dirEntries(); len(names) != 0 {
		t.Errorf("left behind %v", names)
	}
}

func TestStageRefusesAnAppThisControllerDoesNotRun(t *testing.T) {
	f := appFixture(t)
	delete(f.st.apps, testApp)
	if _, err := f.run(); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err %v, want ErrForbidden", err)
	}
}

func TestStageRefusesAReleaseForAnApp(t *testing.T) {
	// A release is signed with the release key and names no app: offered as an
	// app update it must not pass.
	f := appFixture(t)
	env, file := testRelease(t)
	f.tr = core.Transfer{Release: &env, Size: int64(len(file)), SHA256: sha(file)}
	f.file = file
	if _, err := f.run(); !errors.Is(err, ErrVerify) {
		t.Fatalf("err %v, want ErrVerify", err)
	}
}

func TestStageRefusesACatalogForAnNGCPart(t *testing.T) {
	f := releaseFixture(t)
	app := appFixture(t)
	f.tr = app.tr
	f.file = app.file
	if _, err := f.run(); !errors.Is(err, ErrVerify) {
		t.Fatalf("err %v, want ErrVerify", err)
	}
}

func TestStageRefusesTheSystemTarget(t *testing.T) {
	// apt brings the system packages; nothing is laid ready for them.
	f := releaseFixture(t)
	f.target = trust.AutoSystem
	if _, err := f.run(); !errors.Is(err, ErrVerify) {
		t.Fatalf("err %v, want ErrVerify", err)
	}
}

// TestStageLeavesNothingFinishedAfterABrokenTransfer is the one about a half
// laid update: the API stops sending in the middle, and what is left must not
// look like something that can be installed.
func TestStageLeavesNothingFinishedAfterABrokenTransfer(t *testing.T) {
	f := releaseFixture(t)
	var buf bytes.Buffer
	if err := core.WriteTransfer(&buf, f.tr); err != nil {
		t.Fatal(err)
	}
	buf.Write(f.file[:len(f.file)-4]) // four bytes short, then EOF
	_, err := New(f.deps).Stage(context.Background(), f.decision(), &buf)
	if !errors.Is(err, ErrVerify) {
		t.Fatalf("err %v, want ErrVerify", err)
	}
	if names := f.dirEntries(); len(names) != 0 {
		t.Errorf("a half update stayed behind: %v", names)
	}
	if f.st.puts != 0 {
		t.Errorf("%d updates were remembered", f.st.puts)
	}
}

// TestStageKeepsNoFileWithoutItsRow keeps the other order out: a file that
// lies ready while nothing remembers it.
func TestStageKeepsNoFileWithoutItsRow(t *testing.T) {
	f := releaseFixture(t)
	f.st.putErr = errors.New("the local store is down")
	if _, err := f.run(); err == nil {
		t.Fatal("Stage said yes although nothing was remembered")
	}
	if names := f.dirEntries(); len(names) != 0 {
		t.Errorf("the file stayed without a row: %v", names)
	}
}

// probeReader looks into the staging directory on every single read, so a
// case can say what lay there WHILE the bytes were still running in - not
// only afterwards.
type probeReader struct {
	r    io.Reader
	look func()
}

func (p *probeReader) Read(b []byte) (int, error) {
	p.look()
	return p.r.Read(b)
}

// TestStageNeverWritesTheFinalNameWhileItReads is the other half of the half
// laid update: a file under the name an installer looks for must not exist
// before its checksum held. Afterwards both orders look alike - a broken
// transfer cleans up either way - so this is the only place the difference
// can be seen.
func TestStageNeverWritesTheFinalNameWhileItReads(t *testing.T) {
	f := releaseFixture(t)
	reads, seen := 0, 0
	body := &probeReader{r: iotest.OneByteReader(f.body()), look: func() {
		reads++
		if _, err := os.Stat(f.staged()); err == nil {
			seen++
		}
	}}
	if _, err := New(f.deps).Stage(context.Background(), f.decision(), body); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	// Zwischenpruefung: the probe really ran, and often enough to have caught
	// a file written byte by byte.
	if reads < len(f.file) {
		t.Fatalf("the probe looked %d times for %d bytes - it would have missed the file",
			reads, len(f.file))
	}
	if seen != 0 {
		t.Errorf("the staged name existed during %d of %d reads", seen, reads)
	}
	if _, err := os.Stat(f.staged()); err != nil {
		t.Fatalf("and afterwards it is not there either: %v", err)
	}
}

func TestStageRefusesMoreBytesThanAnnounced(t *testing.T) {
	f := releaseFixture(t)
	var buf bytes.Buffer
	if err := core.WriteTransfer(&buf, f.tr); err != nil {
		t.Fatal(err)
	}
	buf.Write(f.file)
	buf.Write([]byte("and more"))
	if _, err := New(f.deps).Stage(context.Background(), f.decision(), &buf); !errors.Is(err, ErrVerify) {
		t.Fatalf("err %v, want ErrVerify", err)
	}
	if names := f.dirEntries(); len(names) != 0 {
		t.Errorf("left behind %v", names)
	}
}

func TestStageWithoutItsDependenciesIsInternal(t *testing.T) {
	good := releaseFixture(t)
	cases := map[string]func(d *Deps){
		"no state":   func(d *Deps) { d.State = nil },
		"no list":    func(d *Deps) { d.List = nil },
		"no dir":     func(d *Deps) { d.StageDir = "" },
		"no max":     func(d *Deps) { d.Max = 0 },
		"no report":  func(d *Deps) { d.Report = nil },
		"no clock":   func(d *Deps) { d.Now = nil },
		"no release": func(d *Deps) { d.ReleaseKey = nil },
	}
	for name, bend := range cases {
		t.Run(name, func(t *testing.T) {
			f := releaseFixture(t)
			bend(&f.deps)
			if _, err := f.run(); !errors.Is(err, ErrInternal) {
				t.Fatalf("err %v, want ErrInternal", err)
			}
		})
	}
	if _, err := New(good.deps).Stage(context.Background(), good.decision(), nil); !errors.Is(err, ErrInternal) {
		t.Fatalf("err %v, want ErrInternal", err)
	}
	var nilManager *Manager
	if _, err := nilManager.Stage(context.Background(), good.decision(), good.body()); !errors.Is(err, ErrInternal) {
		t.Fatalf("err %v, want ErrInternal", err)
	}
}

func TestStageRefusesParamsOfAnotherShape(t *testing.T) {
	f := releaseFixture(t)
	a, _ := core.Lookup(core.AppStore, core.ServiceController, core.ActStage)
	d := access.Decision{Opened: tunnel.Opened{Open: wire.Open{App: core.AppStore,
		Service: core.ServiceController, Action: core.ActStage,
		Params: json.RawMessage(`{"target":"ngc-web","version":"1.1.0","extra":1}`)}},
		Action: a, Core: true}
	if _, err := New(f.deps).Stage(context.Background(), d, f.body()); !errors.Is(err, ErrVerify) {
		t.Fatalf("err %v, want ErrVerify", err)
	}
}

func TestStageRefusesAFileBeyondTheBound(t *testing.T) {
	f := releaseFixture(t)
	f.deps.Max = int64(len(f.file)) - 1
	if _, err := f.run(); !errors.Is(err, ErrVerify) {
		t.Fatalf("err %v, want ErrVerify", err)
	}
	if names := f.dirEntries(); len(names) != 0 {
		t.Errorf("left behind %v", names)
	}
}

// TestStageRefusesAHeaderThatUnderstatesTheSize: the bound is held against
// the header, the reading against the signed entry, so a header that names
// fewer bytes than the entry would get a big file past a small bound.
func TestStageRefusesAHeaderThatUnderstatesTheSize(t *testing.T) {
	// Zwischenpruefung: with the honest header the same file goes through at a
	// bound that covers it.
	open := releaseFixture(t)
	open.deps.Max = int64(len(open.file))
	if _, err := open.run(); err != nil {
		t.Fatalf("the way to the size check is not open: %v", err)
	}

	f := releaseFixture(t)
	f.deps.Max = 1
	f.tr.Size = 1 // the release entry says 18
	if _, err := f.run(); !errors.Is(err, ErrVerify) {
		t.Fatalf("err %v, want ErrVerify", err)
	}
	if names := f.dirEntries(); len(names) != 0 {
		t.Errorf("left behind %v", names)
	}
}

func TestStagePassesOnAFailureOfTheLocalStore(t *testing.T) {
	f := releaseFixture(t)
	f.st.readErr = errors.New("the local store is down")
	if _, err := f.run(); err == nil || errors.Is(err, ErrVerify) {
		t.Fatalf("err %v, want the failure of the store", err)
	}
	if names := f.dirEntries(); len(names) != 0 {
		t.Errorf("left behind %v", names)
	}
}

// TestStageLaysTheFileDownForRootOnly: the file is handed to a program running
// as root later, so nobody else may write it.
func TestStageLaysTheFileDownForRootOnly(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("laut uebersprungen: Windows kennt die Unix-Rechte dieser Datei nicht")
	}
	f := releaseFixture(t)
	if _, err := f.run(); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	fi, err := os.Stat(f.staged())
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
}

// TestStageReplacesWhatLayReadyBefore: one file and one row per target.
func TestStageReplacesWhatLayReadyBefore(t *testing.T) {
	f := releaseFixture(t)
	if err := os.WriteFile(f.staged(), []byte("the update before"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.run(); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	got, err := os.ReadFile(f.staged())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, f.file) {
		t.Error("the file before was not replaced")
	}
	if names := f.dirEntries(); len(names) != 1 {
		t.Errorf("the staging directory holds %v", names)
	}
}

// TestStageMessagesFitTheContract: every line an admin may read has to come
// back out of store.ParseUpdateReport, or the report is dropped on the way.
func TestStageMessagesFitTheContract(t *testing.T) {
	for _, err := range []error{store.ErrRollback, ErrForbidden, ErrVerify, ErrDisk,
		errors.New("something else")} {
		msg := message(err)
		b, merr := json.Marshal(store.UpdateReport{Target: trust.AutoWeb, Version: testVer,
			State: store.UpdateRejected, Message: msg})
		if merr != nil {
			t.Fatal(merr)
		}
		if _, perr := store.ParseUpdateReport(b); perr != nil {
			t.Errorf("message %q of %v: %v", msg, err, perr)
		}
	}
	// And the five are five different lines: a single text would make the
	// walls indistinguishable for whoever reads them.
	seen := map[string]bool{}
	for _, err := range []error{store.ErrRollback, ErrForbidden, ErrVerify, ErrDisk,
		errors.New("something else")} {
		msg := message(err)
		if seen[msg] {
			t.Errorf("message %q is used twice", msg)
		}
		seen[msg] = true
	}
}
