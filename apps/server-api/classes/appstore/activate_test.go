package appstore

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/login"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/trust"

	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// ---------------------------------------------------------------------------
// The controller, faked at the one seam this package has (Sender).
// ---------------------------------------------------------------------------

// sent is one thing the API handed to a controller, with everything that
// travelled. The file is read to its end and kept, so a case compares the
// bytes the controller would have got and not a reader.
type sent struct {
	node, action string
	params       json.RawMessage
	head         core.Transfer
	file         []byte
	task         *sign.Envelope
	client       []byte
	proof        *proof.Proof
	transfer     bool
}

// fakeSender stands in for the relay. It is the whole way out of the package,
// so a case that finds nothing in sents knows nothing left the API at all --
// which is how "refused before the bytes travel" is measured rather than
// assumed.
type fakeSender struct {
	// roles is node -> the roles of its server. A node that is not a key has
	// no tunnel, which is the only thing this package reads as offline.
	roles map[string][]string
	code  wire.Code // what the controller answers; "" is success
	sents []sent
	// readErr is handed back instead of reading the file, so a case can make
	// the send fail halfway.
	readErr error
}

func newSender(roles map[string][]string) *fakeSender {
	return &fakeSender{roles: roles}
}

func (f *fakeSender) Node(node string) (tunnel.Hello, bool) {
	r, ok := f.roles[node]
	if !ok {
		return tunnel.Hello{}, false
	}
	return tunnel.Hello{Version: "1.0.0", Arch: "amd64", Node: node, Roles: r}, true
}

func (f *fakeSender) Transfer(_ context.Context, node, action string, params json.RawMessage,
	head core.Transfer, file io.Reader, tk *sign.Envelope, client []byte, pr *proof.Proof) ([]byte, wire.Code) {
	s := sent{node: node, action: action, params: bytes.Clone(params), head: head,
		task: tk, client: bytes.Clone(client), proof: pr, transfer: true}
	if f.readErr != nil {
		f.sents = append(f.sents, s)
		return nil, wire.CodeUnreachable
	}
	b, err := io.ReadAll(file)
	if err != nil {
		f.sents = append(f.sents, s)
		return nil, wire.CodeUnreachable
	}
	s.file = b
	f.sents = append(f.sents, s)
	return nil, f.code
}

func (f *fakeSender) Call(_ context.Context, node, action string, params json.RawMessage,
	tk *sign.Envelope, client []byte, pr *proof.Proof) ([]byte, wire.Code) {
	f.sents = append(f.sents, sent{node: node, action: action, params: bytes.Clone(params),
		task: tk, client: bytes.Clone(client), proof: pr})
	return nil, f.code
}

var _ Sender = (*fakeSender)(nil)

// ---------------------------------------------------------------------------
// The fixture: one API, one store on a real TLS server, one session of the
// admin, one controller with the role services.
// ---------------------------------------------------------------------------

const (
	ctlNode     = "ctl-1"     // the controller of trusttest.Genesis, where admins may run root tasks
	plainNode   = "ctl-plain" // a controller without the role services
	adminSeed   = 0x44        // seed of the session key the tasks are signed with
	catalogFile = "pkg.ngc"   // the file name the minted catalog points at
)

// actFixture is newFixture with a signed-in admin, a faked controller and a
// spool of its own.
type actFixture struct {
	fixture
	Send   *fakeSender
	Live   *login.Live
	seskey ed25519.PrivateKey
	client []byte
	pr     proof.Proof
	Spool  string
}

func newActFixture(t *testing.T) actFixture {
	t.Helper()
	fx, v := newFixture(t)
	// The store key of the vectors must be the one these cases sign catalogs
	// with, or nothing below measures what it says it does.
	if !bytes.Equal(storePriv.Public().(ed25519.PublicKey), v.StoreKey) {
		t.Fatalf("storeKey of the vectors is not the seed 0x71")
	}
	af := actFixture{fixture: fx, Spool: t.TempDir()}
	af.St.Spool = af.Spool
	// A second controller, in the trust list like the first, whose SERVER has
	// no role services. Without it in the list the cases below would measure
	// a node nobody may run a root task on, and every refusal would read as
	// the missing role.
	plainDH, _ := trusttest.NodeKeys(0xC2)
	l3 := nextVersion(t, fx.S)
	l3.Nodes = append(l3.Nodes, trust.Node{ID: plainNode, Kind: trust.NodeController,
		Name: "Datenbank", Noise: plainDH.Public, RootRoles: []string{trust.RoleAdmin}})
	if _, err := fx.S.AddVersion(ctx, trusttest.NewSigner(1).Sign(t, l3)); err != nil {
		t.Fatalf("version 3: %v", err)
	}
	af.Send = newSender(map[string][]string{
		ctlNode:   {tunnel.RoleServices},
		plainNode: {tunnel.RoleDB},
	})
	af.St.Send = af.Send
	ver, err := fx.S.Trust(ctx)
	if err != nil {
		t.Fatalf("trust: %v", err)
	}
	// The admin of trusttest.Genesis signs in: a client certificate of a
	// session key, with the admin's own key vouching for it, exactly as
	// package login builds one.
	af.seskey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{adminSeed}, ed25519.SeedSize))
	dh, _ := trusttest.NodeKeys(adminSeed ^ 0xFF)
	client, err := json.Marshal(session.ClientCert{Installation: ver.List.Installation,
		User: trusttest.ID(0x21), Nonce: bytes.Repeat([]byte{adminSeed}, session.NonceSize),
		Sig: enc.Bytes(af.seskey.Public().(ed25519.PublicKey)), DH: dh.Public,
		Issued: testNow, Expires: testNow + 3600})
	if err != nil {
		t.Fatal(err)
	}
	af.pr = trusttest.NewSigner(1).Prove(sign.PurposeClientCert, client)
	verified, err := session.VerifyClient(client, af.pr, ver.List, testNow)
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	af.client = client
	af.Live = &login.Live{Verified: verified}
	// The one thing the whole UI proof of an uploaded app hangs off: this
	// user may run root tasks on this node. Said out loud, because a case that
	// lost it would measure a refusal and call it a pass.
	if !ver.List.MayRoot(trusttest.ID(0x21), ctlNode) {
		t.Fatalf("the admin may not run root tasks on %s", ctlNode)
	}
	return af
}

// carried is the task, the certificate and the proof of one root task of this
// admin on node, signed with the session key.
func (af actFixture) carried(t *testing.T, node, action string, params any, id byte) Carried {
	t.Helper()
	pb, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	tk := task.Task{ID: bytes.Repeat([]byte{id}, task.IDSize),
		Installation: af.Live.Verified.Cert().Installation,
		Cert:         session.CertHash(af.Live.Verified.Payload()), Node: node,
		App: core.AppStore, Service: core.ServiceController, Action: action,
		Params: pb, Issued: testNow, Expires: testNow + 60}
	b, err := json.Marshal(tk)
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.Sign(af.seskey, sign.PurposeTask, b)
	if err != nil {
		t.Fatal(err)
	}
	p := af.pr
	return Carried{Task: &env, Client: af.client, Proof: &p, Node: node, Live: af.Live}
}

// vectorPackage returns one package of shared/testdata/packui.json with its
// canonical ui-tar: nothing here packs its own, so the ui-tar a case compares
// against is the one ngc-store, the controller and the shell compute.
func vectorPackage(t *testing.T, name string) (pkg, ui []byte) {
	t.Helper()
	for _, c := range loadUploadVectors(t) {
		if c.Name == name {
			return c.Package, c.UI
		}
	}
	t.Fatalf("no package vector named %q", name)
	return nil, nil
}

// manifestBytesOf reads manifest.json out of a package with archive/tar
// alone, so what a case compares dbapi.App.Manifest against does not come
// from the code under test.
func manifestBytesOf(t *testing.T, pkg []byte) []byte {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(pkg))
	h, err := tr.Next()
	if err != nil || h.Name != store.ManifestFile {
		t.Fatalf("first entry: %v", err)
	}
	b, err := io.ReadAll(tr)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// listCatalog puts a catalog of the global store into the database that lists
// app at version with the size, the checksum and the UI hash of pkg, and
// serves pkg under catalogFile. It is signed with the store key of the
// vectors, because the vectors carry no entry whose checksum belongs to a
// real package.
func (af actFixture) listCatalog(t *testing.T, app, version string, pkg, ui []byte,
	needs []string, price *store.Price) sign.Envelope {
	t.Helper()
	sum := sha256.Sum256(pkg)
	// UI is the HASH of the canonical ui-tar, not the tar: 32 bytes, as
	// store.Entry.valid asks for and as the controller recomputes it.
	uiSum := sha256.Sum256(ui)
	c := store.Catalog{Store: store.GlobalID, Seq: 9, Issued: testNow,
		Apps: []store.Entry{{ID: app, Name: "Testapp", Publisher: "Sorglos IT", Price: price,
			Versions: []store.Package{{Version: version, URL: catalogFile, Size: int64(len(pkg)),
				SHA256: sum[:], UI: uiSum[:], Encrypted: price != nil, Needs: needs}}}}}
	payload, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.Sign(storePriv, sign.PurposeCatalog, payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := af.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobCatalog, ID: store.GlobalID,
		Seq: c.Seq, Data: raw, Updated: testNow}); err != nil {
		t.Fatalf("catalog row: %v", err)
	}
	af.setFile(catalogFile, pkg)
	// The way to the line under measurement has to be open: a catalog this
	// API cannot read would make every case below pass for the wrong reason.
	if _, _, err := af.St.Catalog(ctx, store.GlobalID); err != nil {
		t.Fatalf("the stored catalog does not verify: %v", err)
	}
	return env
}

// storeRequest is an activation of a store app, with the parameters the signed
// task carries.
func (af actFixture) storeRequest(t *testing.T, node, app, version string, pkg, ui []byte,
	grants []string, id byte) ActivateRequest {
	t.Helper()
	sum := sha256.Sum256(pkg)
	uiSum := sha256.Sum256(ui)
	p := &core.ActivateParams{App: app, Version: version, SHA256: sum[:], UI: uiSum[:],
		Store: store.GlobalID, Grants: grants}
	return ActivateRequest{Carried: af.carried(t, node, core.ActActivate, p, id), Params: p}
}

// uploadRequest is an activation of an uploaded app: the package is the file
// the caller holds, and no store is asked.
func (af actFixture) uploadRequest(t *testing.T, node string, pkg, ui []byte,
	app, version string, grants []string, id byte) ActivateRequest {
	t.Helper()
	sum := sha256.Sum256(pkg)
	uiSum := sha256.Sum256(ui)
	p := &core.ActivateParams{App: app, Version: version, SHA256: sum[:], UI: uiSum[:],
		Store: core.LocalStore, Grants: grants}
	return ActivateRequest{Carried: af.carried(t, node, core.ActActivate, p, id), Params: p,
		File: bytes.NewReader(pkg), Size: int64(len(pkg))}
}

// spoolFiles is what is lying in the spool. Every case checks it: a package of
// up to 4 GiB left behind would be invisible otherwise.
func (af actFixture) spoolFiles(t *testing.T) []string {
	t.Helper()
	es, err := os.ReadDir(af.Spool)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// ---------------------------------------------------------------------------
// The inventory, for measuring what an uninstall takes and what it leaves.
// ---------------------------------------------------------------------------

// blobKinds are the kinds of document dbapi knows (dbapi.Blob*, read off
// shared/dbapi/dbapi.go on 12.10.2026). The inventory walks all of them, so a
// row an uninstall deletes somewhere nobody looked still shows up.
var blobKinds = []string{dbapi.BlobSetting, dbapi.BlobTrust, dbapi.BlobCatalog,
	dbapi.BlobLicense, dbapi.BlobRights, dbapi.BlobUI, dbapi.BlobApproval, dbapi.BlobJoin}

// wantBlobKinds is how many kinds there are. A kind added to the contract
// without being added here would leave a blind spot in every inventory below,
// and this is what makes that visible instead of silent.
const wantBlobKinds = 8

// inventory is every row of the database, by "<kind>/<id>" and "app/<id>",
// with the checksum of its content, so a case can say exactly what changed.
func inventory(t *testing.T, s dbapi.Store) map[string]string {
	t.Helper()
	if len(blobKinds) != wantBlobKinds {
		t.Fatalf("blobKinds has %d entries, want %d", len(blobKinds), wantBlobKinds)
	}
	out := map[string]string{}
	for _, k := range blobKinds {
		bs, err := s.BlobFrom(ctx, k, 0)
		if err != nil {
			t.Fatalf("blobs of %s: %v", k, err)
		}
		for _, b := range bs {
			sum := sha256.Sum256(b.Data)
			out[k+"/"+b.ID] = enc.B64.EncodeToString(sum[:])
		}
	}
	apps, err := s.AppList(ctx)
	if err != nil {
		t.Fatalf("apps: %v", err)
	}
	for _, a := range apps {
		b, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		out["app/"+a.ID] = enc.B64.EncodeToString(sum[:])
	}
	return out
}

// diff names what is gone, what is new and what changed between two
// inventories, each sorted, so a case asserts a set and not a count.
func diff(before, after map[string]string) (gone, added, changed []string) {
	for k, v := range before {
		w, ok := after[k]
		switch {
		case !ok:
			gone = append(gone, k)
		case w != v:
			changed = append(changed, k)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			added = append(added, k)
		}
	}
	slices.Sort(gone)
	slices.Sort(added)
	slices.Sort(changed)
	return gone, added, changed
}

// failDB fails one named operation and passes everything else through, so a
// case can break an uninstall or an activation in the middle and then measure
// what holds.
type failDB struct {
	dbapi.Store
	fail string // "blobdelete:<kind>", "appdelete", "appput", "blobput:<kind>"
}

var errBroken = errors.New("test: the database stopped here")

func (f *failDB) BlobDelete(ctx context.Context, kind, id string) error {
	if f.fail == "blobdelete:"+kind {
		return errBroken
	}
	return f.Store.BlobDelete(ctx, kind, id)
}

func (f *failDB) BlobPut(ctx context.Context, b dbapi.Blob) error {
	if f.fail == "blobput:"+b.Kind {
		return errBroken
	}
	return f.Store.BlobPut(ctx, b)
}

func (f *failDB) AppDelete(ctx context.Context, id string) error {
	if f.fail == "appdelete" {
		return errBroken
	}
	return f.Store.AppDelete(ctx, id)
}

func (f *failDB) AppPut(ctx context.Context, a dbapi.App) error {
	if f.fail == "appput" {
		return errBroken
	}
	return f.Store.AppPut(ctx, a)
}

// ---------------------------------------------------------------------------
// Activating
// ---------------------------------------------------------------------------

// TestActivateSendsHeaderAndFile: the header of a store app names the catalog
// and the store, the size and the checksum of the file, and the file that
// follows is the package byte for byte. The header goes through the writer and
// the reader of the contract as well, because that is what the relay does with
// it before the channel exists at all.
func TestActivateSendsHeaderAndFile(t *testing.T) {
	af := newActFixture(t)
	pkg, ui := vectorPackage(t, "long file name") // org.sorglos.calculator, no services
	env := af.listCatalog(t, "org.sorglos.calculator", "1.0.0", pkg, ui, nil, nil)
	rq := af.storeRequest(t, ctlNode, "org.sorglos.calculator", "1.0.0", pkg, ui, nil, 0x61)

	code, err := af.St.Activate(ctx, rq)
	if code != "" || err != nil {
		t.Fatalf("Activate: code %q, err %v", code, err)
	}
	if len(af.Send.sents) != 1 {
		t.Fatalf("%d channels to the controller, want 1", len(af.Send.sents))
	}
	s := af.Send.sents[0]
	if !s.transfer || s.node != ctlNode || s.action != core.ActActivate {
		t.Fatalf("sent %q to %q, transfer %v", s.action, s.node, s.transfer)
	}
	sum := sha256.Sum256(pkg)
	if s.head.Size != int64(len(pkg)) || !bytes.Equal(s.head.SHA256, sum[:]) {
		t.Fatalf("header names size %d and another checksum", s.head.Size)
	}
	if s.head.Store != store.GlobalID || s.head.Catalog == nil {
		t.Fatalf("header carries store %q, catalog %v", s.head.Store, s.head.Catalog != nil)
	}
	if !bytes.Equal(s.head.Catalog.Payload, env.Payload) || !bytes.Equal(s.head.Catalog.Sig, env.Sig) {
		t.Fatalf("the catalog in the header is not the one the store signed")
	}
	if s.head.License != nil || len(s.head.Key) != 0 {
		t.Fatalf("a free app got a licence or a content key")
	}
	if !bytes.Equal(s.file, pkg) {
		t.Fatalf("the file is %d bytes, the package %d", len(s.file), len(pkg))
	}
	// The task, the certificate and the proof travel as they stand.
	if s.task != rq.Task || !bytes.Equal(s.client, af.client) || s.proof != rq.Proof {
		t.Fatalf("the task or the user was changed on the way")
	}
	// The header and the file as the controller reads them: four bytes of
	// length, the JSON, then the file.
	var wire1 bytes.Buffer
	if err := core.WriteTransfer(&wire1, s.head); err != nil {
		t.Fatalf("the header does not go through WriteTransfer: %v", err)
	}
	wire1.Write(s.file)
	back, err := core.ReadTransfer(&wire1)
	if err != nil {
		t.Fatalf("ReadTransfer: %v", err)
	}
	if back.Size != s.head.Size || !bytes.Equal(back.SHA256, s.head.SHA256) || back.Store != s.head.Store {
		t.Fatalf("the header does not come back as it went out")
	}
	rest, err := io.ReadAll(&wire1)
	if err != nil || !bytes.Equal(rest, pkg) {
		t.Fatalf("what follows the header is not the package (%d bytes, %v)", len(rest), err)
	}
	if got := af.spoolFiles(t); len(got) != 0 {
		t.Fatalf("the spool still holds %v", got)
	}
}

// TestActivateSendsTheLicenceAndTheKeyOfAPaidApp: a paid app gets its licence
// envelope and the content key the API unwrapped, and only the API can unwrap
// it. The counter-case is in TestActivateSendsHeaderAndFile: a free app gets
// neither.
func TestActivateSendsTheLicenceAndTheKeyOfAPaidApp(t *testing.T) {
	af := newActFixture(t)
	// The installation's private key, so ContentKey has something to unwrap
	// with, and the licence of the vectors with a real wrapped key.
	sealed, err := af.S.Box.Seal(installPair.Private)
	if err != nil {
		t.Fatal(err)
	}
	if err := af.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallKey,
		Data: sealed, Updated: testNow}); err != nil {
		t.Fatal(err)
	}
	lic := mint(t, "org.sorglos.terminal", nil)
	if err := af.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobLicense, ID: "org.sorglos.terminal",
		Data: lic, Updated: testNow}); err != nil {
		t.Fatal(err)
	}
	pkg, ui := vectorPackage(t, "files, directories and an image") // terminal, with services
	af.listCatalog(t, "org.sorglos.terminal", "1.0.0", pkg, ui, nil,
		&store.Price{Model: store.PriceSubscription, Per: store.PerSite, Amount: 499,
			Currency: "EUR", Period: store.PeriodMonth, Shop: "https://shop.example/terminal"})
	rq := af.storeRequest(t, ctlNode, "org.sorglos.terminal", "1.0.0", pkg, ui, nil, 0x62)

	code, err := af.St.Activate(ctx, rq)
	if code != "" || err != nil {
		t.Fatalf("Activate: code %q, err %v", code, err)
	}
	s := af.Send.sents[0]
	if s.head.License == nil {
		t.Fatalf("the paid app got no licence")
	}
	if !bytes.Equal(s.head.Key, contentKey) {
		t.Fatalf("the content key in the header is not the one the licence wraps")
	}
	// A header that carries a key must carry a catalog with it, which is what
	// the contract holds; running it through Validate says so out loud.
	if err := s.head.Validate(); err != nil {
		t.Fatalf("the header does not validate: %v", err)
	}
}

// TestActivateKeepsTheAppRowAndTheProof: after a successful activation the app
// row names store, node, grants, manifest and checksum, the ui-tar lies under
// dbapi.BlobUI byte for byte, and the evidence under dbapi.BlobApproval is one
// the real verifier hands back out (Proof).
func TestActivateKeepsTheAppRowAndTheProof(t *testing.T) {
	af := newActFixture(t)
	pkg, ui := vectorPackage(t, "long file name")
	env := af.listCatalog(t, "org.sorglos.calculator", "1.0.0", pkg, ui, nil, nil)
	rq := af.storeRequest(t, ctlNode, "org.sorglos.calculator", "1.0.0", pkg, ui, nil, 0x63)
	if code, err := af.St.Activate(ctx, rq); code != "" || err != nil {
		t.Fatalf("Activate: code %q, err %v", code, err)
	}
	row, err := af.S.DB.AppGet(ctx, "org.sorglos.calculator")
	if err != nil {
		t.Fatalf("AppGet: %v", err)
	}
	sum := sha256.Sum256(pkg)
	if row.Version != "1.0.0" || row.Store != store.GlobalID || row.Node != ctlNode || !row.Active {
		t.Fatalf("row: %+v", row)
	}
	if !bytes.Equal(row.SHA256, sum[:]) {
		t.Fatalf("the row names another checksum than the package")
	}
	if !bytes.Equal(row.Manifest, manifestBytesOf(t, pkg)) {
		t.Fatalf("the manifest of the row is not the one the package carries")
	}
	if len(row.Grants) != 0 {
		t.Fatalf("grants: %v, want none", row.Grants)
	}
	b, err := af.S.DB.BlobGet(ctx, dbapi.BlobUI, "org.sorglos.calculator@1.0.0")
	if err != nil {
		t.Fatalf("ui-tar: %v", err)
	}
	if !bytes.Equal(b.Data, ui) {
		t.Fatalf("the ui-tar is not the canonical one of the vectors")
	}
	// Not the stored bytes but what the verifier gives back, against the
	// trust list as it reads now.
	p, err := af.St.Proof(ctx, "org.sorglos.calculator", "1.0.0")
	if err != nil {
		t.Fatalf("Proof: %v", err)
	}
	if p.Store != store.GlobalID || p.Catalog == nil || !bytes.Equal(p.Catalog.Sig, env.Sig) {
		t.Fatalf("the evidence is not the catalog of the store: %+v", p)
	}
	if p.Task != nil || len(p.Client) != 0 || p.Proof != nil {
		t.Fatalf("a store app kept a task as its evidence")
	}
}

// TestActivateKeepsTheTaskAsTheProofOfAnUploadedApp: an uploaded app has no
// catalog, so its evidence is the signed activate task with the client
// certificate and the user's proof -- and it has to hold against the trust
// list, which is what Proof answers.
func TestActivateKeepsTheTaskAsTheProofOfAnUploadedApp(t *testing.T) {
	af := newActFixture(t)
	pkg, ui := vectorPackage(t, "long file name")
	rq := af.uploadRequest(t, ctlNode, pkg, ui, "org.sorglos.calculator", "1.0.0", nil, 0x64)
	code, err := af.St.Activate(ctx, rq)
	if code != "" || err != nil {
		t.Fatalf("Activate: code %q, err %v", code, err)
	}
	s := af.Send.sents[0]
	if s.head.Catalog != nil || s.head.Store != "" {
		t.Fatalf("an uploaded app got a catalog or a store in its header")
	}
	if !bytes.Equal(s.file, pkg) {
		t.Fatalf("the uploaded file did not travel as it stands")
	}
	p, err := af.St.Proof(ctx, "org.sorglos.calculator", "1.0.0")
	if err != nil {
		t.Fatalf("Proof: %v", err)
	}
	if p.Store != core.LocalStore || p.Task == nil || p.Proof == nil || len(p.Client) == 0 {
		t.Fatalf("the evidence of an uploaded app is not the task: %+v", p)
	}
	if !bytes.Equal(p.Client, af.client) {
		t.Fatalf("the certificate in the evidence is not the one of the session")
	}
	row, err := af.S.DB.AppGet(ctx, "org.sorglos.calculator")
	if err != nil || row.Store != core.LocalStore {
		t.Fatalf("row: %+v (%v)", row, err)
	}
}

// TestActivateOnlyOnANodeWithRoleServices nails both sides: an app with
// services is refused on a controller without the role and nothing travels,
// the same app is taken on a controller with it, and an app without services
// is taken on the controller without the role. Without the last two the case
// would stay green with a wall that refuses everything.
func TestActivateOnlyOnANodeWithRoleServices(t *testing.T) {
	svc, svcUI := vectorPackage(t, "no ui files")        // org.sorglos.backup, services, no ui
	plain, plainUI := vectorPackage(t, "long file name") // org.sorglos.calculator, no services

	af := newActFixture(t)
	rq := af.uploadRequest(t, plainNode, svc, svcUI, "org.sorglos.backup", "1.0.0", nil, 0x65)
	code, err := af.St.Activate(ctx, rq)
	if code != wire.CodeForbidden || err != nil {
		t.Fatalf("services on a node without the role: code %q, err %v", code, err)
	}
	if len(af.Send.sents) != 0 {
		t.Fatalf("the package travelled anyway: %d channels", len(af.Send.sents))
	}
	if _, err := af.S.DB.AppGet(ctx, "org.sorglos.backup"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("a row was written: %v", err)
	}
	if got := af.spoolFiles(t); len(got) != 0 {
		t.Fatalf("the refused package stayed in the spool: %v", got)
	}

	af2 := newActFixture(t)
	rq2 := af2.uploadRequest(t, ctlNode, svc, svcUI, "org.sorglos.backup", "1.0.0", nil, 0x66)
	if code, err := af2.St.Activate(ctx, rq2); code != "" || err != nil {
		t.Fatalf("the same app on a node with the role: code %q, err %v", code, err)
	}

	af3 := newActFixture(t)
	rq3 := af3.uploadRequest(t, plainNode, plain, plainUI, "org.sorglos.calculator", "1.0.0", nil, 0x67)
	if code, err := af3.St.Activate(ctx, rq3); code != "" || err != nil {
		t.Fatalf("an app without services on a node without the role: code %q, err %v", code, err)
	}
}

// TestActivateRefusesAnOlderVersion: over a newer installed version nothing
// happens; the same version again is how an admin re-approves one, and that
// must go through, or the case would also pass with a wall at every version.
func TestActivateRefusesAnOlderVersion(t *testing.T) {
	af := newActFixture(t)
	pkg, ui := vectorPackage(t, "long file name") // calculator 1.0.0
	af.listCatalog(t, "org.sorglos.calculator", "1.0.0", pkg, ui, nil, nil)
	if err := af.S.DB.AppPut(ctx, dbapi.App{ID: "org.sorglos.calculator", Version: "1.1.0",
		Store: store.GlobalID, Node: ctlNode, Active: true, Manifest: []byte(`{}`),
		SHA256: bytes.Repeat([]byte{9}, 32), Updated: testNow}); err != nil {
		t.Fatal(err)
	}
	rq := af.storeRequest(t, ctlNode, "org.sorglos.calculator", "1.0.0", pkg, ui, nil, 0x68)
	code, err := af.St.Activate(ctx, rq)
	if code != wire.CodeForbidden || err != nil {
		t.Fatalf("older version: code %q, err %v", code, err)
	}
	if len(af.Send.sents) != 0 {
		t.Fatalf("the older package travelled: %d channels", len(af.Send.sents))
	}
	row, err := af.S.DB.AppGet(ctx, "org.sorglos.calculator")
	if err != nil || row.Version != "1.1.0" {
		t.Fatalf("the installed row changed: %+v (%v)", row, err)
	}
	// The same version again, in a fixture of its own, so the row above
	// cannot carry over.
	af2 := newActFixture(t)
	af2.listCatalog(t, "org.sorglos.calculator", "1.0.0", pkg, ui, nil, nil)
	if err := af2.S.DB.AppPut(ctx, dbapi.App{ID: "org.sorglos.calculator", Version: "1.0.0",
		Store: store.GlobalID, Node: ctlNode, Active: true, Manifest: []byte(`{}`),
		SHA256: bytes.Repeat([]byte{9}, 32), Updated: testNow}); err != nil {
		t.Fatal(err)
	}
	rq2 := af2.storeRequest(t, ctlNode, "org.sorglos.calculator", "1.0.0", pkg, ui, nil, 0x69)
	if code, err := af2.St.Activate(ctx, rq2); code != "" || err != nil {
		t.Fatalf("the same version again: code %q, err %v", code, err)
	}
}

// TestActivateRefusesWhatTheTaskDoesNotName: the checksum and the UI hash of
// the signed task are the last word on the bytes. A package whose checksum or
// whose UI hash is another one is refused, and nothing travels. The case
// runs the same package unchanged first, so a refusal that came from
// somewhere else would show up.
func TestActivateRefusesWhatTheTaskDoesNotName(t *testing.T) {
	pkg, ui := vectorPackage(t, "long file name")
	ok := newActFixture(t)
	if code, _ := ok.St.Activate(ctx, ok.uploadRequest(t, ctlNode, pkg, ui,
		"org.sorglos.calculator", "1.0.0", nil, 0x6a)); code != "" {
		t.Fatalf("the way is not open: code %q", code)
	}
	for _, c := range []struct {
		name  string
		spoil func(p *core.ActivateParams)
	}{
		{"another checksum", func(p *core.ActivateParams) { p.SHA256 = bytes.Repeat([]byte{3}, 32) }},
		{"another ui hash", func(p *core.ActivateParams) { p.UI = bytes.Repeat([]byte{4}, 32) }},
		{"another app", func(p *core.ActivateParams) { p.App = "org.example.other" }},
		{"another version", func(p *core.ActivateParams) { p.Version = "9.9.9" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			af := newActFixture(t)
			rq := af.uploadRequest(t, ctlNode, pkg, ui, "org.sorglos.calculator", "1.0.0", nil, 0x6b)
			c.spoil(rq.Params)
			// The task is signed over the changed parameters, so the case
			// measures this API and not a signature.
			rq.Carried = af.carried(t, ctlNode, core.ActActivate, rq.Params, 0x6b)
			code, err := af.St.Activate(ctx, rq)
			if code != wire.CodeVerify || err != nil {
				t.Fatalf("code %q, err %v", code, err)
			}
			if len(af.Send.sents) != 0 {
				t.Fatalf("it travelled anyway")
			}
			if got := af.spoolFiles(t); len(got) != 0 {
				t.Fatalf("the spool still holds %v", got)
			}
		})
	}
}

// TestActivateGrantsMustEqualTheNeeds nails both directions of the set: too
// few and too many are refused, exactly right goes through.
func TestActivateGrantsMustEqualTheNeeds(t *testing.T) {
	m := testManifest()
	m.Needs = []string{store.NeedNetwork}
	es := goodEntries(t)
	es[0] = manifestEntry(t, m)
	pkg := buildPackage(t, es)
	var uiBuf bytes.Buffer
	if _, _, err := store.PackageUI(bytes.NewReader(pkg), &uiBuf); err != nil {
		t.Fatalf("own package: %v", err)
	}
	ui := uiBuf.Bytes()
	for _, c := range []struct {
		name   string
		grants []string
		want   wire.Code
	}{
		{"exactly the needs", []string{store.NeedNetwork}, ""},
		{"none at all", nil, wire.CodeForbidden},
		{"one too many", []string{store.NeedNetwork, store.NeedRoot}, wire.CodeForbidden},
		{"the wrong one", []string{store.NeedRoot}, wire.CodeForbidden},
	} {
		t.Run(c.name, func(t *testing.T) {
			af := newActFixture(t)
			rq := af.uploadRequest(t, ctlNode, pkg, ui, m.ID, m.Version, c.grants, 0x6c)
			code, err := af.St.Activate(ctx, rq)
			if code != c.want || err != nil {
				t.Fatalf("code %q, want %q (err %v)", code, c.want, err)
			}
			if c.want != "" && len(af.Send.sents) != 0 {
				t.Fatalf("it travelled anyway")
			}
			if c.want == "" {
				row, err := af.S.DB.AppGet(ctx, m.ID)
				if err != nil || !slices.Equal(row.Grants, c.grants) {
					t.Fatalf("the row kept grants %v (%v)", row.Grants, err)
				}
			}
		})
	}
}

// TestActivateWritesNothingWhenTheControllerRefuses: a code from the
// controller comes back as it stands and leaves no row and no file behind.
func TestActivateWritesNothingWhenTheControllerRefuses(t *testing.T) {
	for _, code := range []wire.Code{wire.CodeForbidden, wire.CodeVerify, wire.CodeExit} {
		af := newActFixture(t)
		af.Send.code = code
		pkg, ui := vectorPackage(t, "long file name")
		rq := af.uploadRequest(t, ctlNode, pkg, ui, "org.sorglos.calculator", "1.0.0", nil, 0x6d)
		got, err := af.St.Activate(ctx, rq)
		if got != code || err != nil {
			t.Fatalf("got %q, want %q (err %v)", got, code, err)
		}
		before := inventory(t, af.S.DB)
		if _, ok := before["app/org.sorglos.calculator"]; ok {
			t.Fatalf("a row was written for a refused activation")
		}
		if _, ok := before["ui/org.sorglos.calculator@1.0.0"]; ok {
			t.Fatalf("a ui-tar was written for a refused activation")
		}
		if got := af.spoolFiles(t); len(got) != 0 {
			t.Fatalf("the spool still holds %v", got)
		}
	}
}

// TestActivateNeedsATunnel: a node with no tunnel is unreachable, and nothing
// is fetched or written. The same request on the node with a tunnel goes
// through, so the refusal is the tunnel and not something else.
func TestActivateNeedsATunnel(t *testing.T) {
	af := newActFixture(t)
	pkg, ui := vectorPackage(t, "long file name")
	rq := af.uploadRequest(t, "ctl-gone", pkg, ui, "org.sorglos.calculator", "1.0.0", nil, 0x6e)
	if code, err := af.St.Activate(ctx, rq); code != wire.CodeUnreachable || err != nil {
		t.Fatalf("code %q, err %v", code, err)
	}
	if len(af.Send.sents) != 0 || len(af.spoolFiles(t)) != 0 {
		t.Fatalf("something happened for an unreachable node")
	}
	ok := af.uploadRequest(t, ctlNode, pkg, ui, "org.sorglos.calculator", "1.0.0", nil, 0x6f)
	if code, err := af.St.Activate(ctx, ok); code != "" || err != nil {
		t.Fatalf("the way is not open: code %q, err %v", code, err)
	}
}

// TestActivateWithoutASenderDoesNothing: the seam is nil until the server
// wires it, and then every activation is an internal error rather than a
// silent success.
func TestActivateWithoutASenderDoesNothing(t *testing.T) {
	af := newActFixture(t)
	af.St.Send = nil
	pkg, ui := vectorPackage(t, "long file name")
	rq := af.uploadRequest(t, ctlNode, pkg, ui, "org.sorglos.calculator", "1.0.0", nil, 0x70)
	if code, err := af.St.Activate(ctx, rq); code != wire.CodeInternal || err != nil {
		t.Fatalf("code %q, err %v", code, err)
	}
	if _, err := af.S.DB.AppGet(ctx, "org.sorglos.calculator"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("a row was written without a controller: %v", err)
	}
}

// TestActivateRefusesACatalogThatNamesAnotherFile: the task and the catalog
// have to mean the same file. A catalog that signs another checksum for this
// version is refused before anything is fetched -- measured by the store
// seeing no request at all.
func TestActivateRefusesACatalogThatNamesAnotherFile(t *testing.T) {
	af := newActFixture(t)
	pkg, ui := vectorPackage(t, "long file name")
	other := append(bytes.Clone(pkg), 0)
	af.listCatalog(t, "org.sorglos.calculator", "1.0.0", other, ui, nil, nil)
	hits := af.hits()
	rq := af.storeRequest(t, ctlNode, "org.sorglos.calculator", "1.0.0", pkg, ui, nil, 0x71)
	if code, err := af.St.Activate(ctx, rq); code != wire.CodeVerify || err != nil {
		t.Fatalf("code %q, err %v", code, err)
	}
	if af.hits() != hits {
		t.Fatalf("the store was asked anyway: %d requests", af.hits()-hits)
	}
	if len(af.spoolFiles(t)) != 0 {
		t.Fatalf("the spool is not empty")
	}
}

// ---------------------------------------------------------------------------
// Deactivating, uninstalling and the list
// ---------------------------------------------------------------------------

// activated is one app, activated for real, so the rows a case measures were
// written by the code under test and not by the case.
func (af actFixture) activated(t *testing.T, name, app string, id byte) (pkg, ui []byte) {
	t.Helper()
	pkg, ui = vectorPackage(t, name)
	rq := af.uploadRequest(t, ctlNode, pkg, ui, app, "1.0.0", nil, id)
	if code, err := af.St.Activate(ctx, rq); code != "" || err != nil {
		t.Fatalf("activate %s: code %q, err %v", app, code, err)
	}
	return pkg, ui
}

// TestDeactivateKeepsTheRow: the app stops, the row stays whole and only
// Active turns false.
func TestDeactivateKeepsTheRow(t *testing.T) {
	af := newActFixture(t)
	af.activated(t, "long file name", "org.sorglos.calculator", 0x72)
	before, err := af.S.DB.AppGet(ctx, "org.sorglos.calculator")
	if err != nil {
		t.Fatal(err)
	}
	p := &core.AppParams{App: "org.sorglos.calculator"}
	rq := DeactivateRequest{Carried: af.carried(t, ctlNode, core.ActDeactivate, p, 0x73), Params: p}
	if code, err := af.St.Deactivate(ctx, rq); code != "" || err != nil {
		t.Fatalf("Deactivate: code %q, err %v", code, err)
	}
	after, err := af.S.DB.AppGet(ctx, "org.sorglos.calculator")
	if err != nil {
		t.Fatal(err)
	}
	if after.Active {
		t.Fatalf("the app is still active")
	}
	if after.Version != before.Version || !bytes.Equal(after.Manifest, before.Manifest) ||
		!bytes.Equal(after.SHA256, before.SHA256) || after.Store != before.Store {
		t.Fatalf("deactivating changed more than Active")
	}
	if _, err := af.S.DB.BlobGet(ctx, dbapi.BlobUI, "org.sorglos.calculator@1.0.0"); err != nil {
		t.Fatalf("deactivating took the ui-tar: %v", err)
	}
}

// TestUninstallKeepsOrDeletesData measures both directions against a full
// inventory of the database: every kind of row dbapi knows is read before and
// after, so what the step takes and what it leaves is a named set and not a
// count.
//
// Without KeepData the app row goes; with it the row stays, Version empty and
// everything that described the gone version cleared. In both directions the
// ui-tar and the evidence of that version go, and the licence stays.
func TestUninstallKeepsOrDeletesData(t *testing.T) {
	for _, keep := range []bool{false, true} {
		name := "without KeepData"
		if keep {
			name = "with KeepData"
		}
		t.Run(name, func(t *testing.T) {
			af := newActFixture(t)
			af.activated(t, "long file name", "org.sorglos.calculator", 0x74)
			// A second app and a licence, so the case sees whether the step
			// reaches beyond the app it was asked about.
			af.activated(t, "no ui files", "org.sorglos.backup", 0x75)
			if err := af.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobLicense,
				ID: "org.sorglos.calculator", Data: mint(t, "org.sorglos.calculator", nil),
				Updated: testNow}); err != nil {
				t.Fatal(err)
			}
			before := inventory(t, af.S.DB)
			p := &core.UninstallParams{App: "org.sorglos.calculator", KeepData: keep}
			rq := UninstallRequest{Carried: af.carried(t, ctlNode, core.ActUninstall, p, 0x76),
				Params: p}
			if code, err := af.St.Uninstall(ctx, rq); code != "" || err != nil {
				t.Fatalf("Uninstall: code %q, err %v", code, err)
			}
			after := inventory(t, af.S.DB)
			gone, added, changed := diff(before, after)
			wantGone := []string{"approval/org.sorglos.calculator@1.0.0",
				"ui/org.sorglos.calculator@1.0.0"}
			wantChanged := []string(nil)
			if keep {
				wantChanged = []string{"app/org.sorglos.calculator"}
			} else {
				wantGone = append(wantGone, "app/org.sorglos.calculator")
				slices.Sort(wantGone)
			}
			// The log row is a blob of no kind, so nothing new may appear.
			if len(added) != 0 {
				t.Fatalf("new rows: %v", added)
			}
			if !slices.Equal(gone, wantGone) {
				t.Fatalf("gone %v, want %v", gone, wantGone)
			}
			if !slices.Equal(changed, wantChanged) {
				t.Fatalf("changed %v, want %v", changed, wantChanged)
			}
			// The licence is named in the list above only by not being in it;
			// said out loud here, because that is the decision.
			if _, err := af.S.DB.BlobGet(ctx, dbapi.BlobLicense, "org.sorglos.calculator"); err != nil {
				t.Fatalf("the licence was taken with it: %v", err)
			}
			row, err := af.S.DB.AppGet(ctx, "org.sorglos.calculator")
			if !keep {
				if !errors.Is(err, dbapi.ErrNotFound) {
					t.Fatalf("the row survived an uninstall without KeepData: %+v (%v)", row, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("the row of kept data is gone: %v", err)
			}
			if row.Version != "" || row.Active || len(row.Manifest) != 0 ||
				len(row.SHA256) != 0 || len(row.Grants) != 0 {
				t.Fatalf("the row still describes an installed app: %+v", row)
			}
			if row.Store != core.LocalStore || row.Node != ctlNode {
				t.Fatalf("the row lost where the data lies: %+v", row)
			}
		})
	}
}

// TestUninstallTouchesNoOtherVersionAndNoOtherApp: the rows of another app and
// of another version of the same app stay. Both are rows this step could reach
// by name, and both are named with the app it was asked about.
func TestUninstallTouchesNoOtherVersionAndNoOtherApp(t *testing.T) {
	af := newActFixture(t)
	af.activated(t, "long file name", "org.sorglos.calculator", 0x77)
	af.activated(t, "no ui files", "org.sorglos.backup", 0x78)
	// An older version of the same app, as it is left after an update.
	if err := af.St.KeepUI(ctx, "org.sorglos.calculator", "0.9.0", []byte("old")); err != nil {
		t.Fatal(err)
	}
	p := &core.UninstallParams{App: "org.sorglos.calculator"}
	rq := UninstallRequest{Carried: af.carried(t, ctlNode, core.ActUninstall, p, 0x79), Params: p}
	if code, err := af.St.Uninstall(ctx, rq); code != "" || err != nil {
		t.Fatalf("Uninstall: code %q, err %v", code, err)
	}
	if _, err := af.S.DB.BlobGet(ctx, dbapi.BlobUI, "org.sorglos.calculator@0.9.0"); err != nil {
		t.Fatalf("the ui-tar of another version went with it: %v", err)
	}
	if _, err := af.S.DB.BlobGet(ctx, dbapi.BlobUI, "org.sorglos.backup@1.0.0"); err != nil {
		t.Fatalf("the ui-tar of another app went with it: %v", err)
	}
	if _, err := af.S.DB.AppGet(ctx, "org.sorglos.backup"); err != nil {
		t.Fatalf("the row of another app went with it: %v", err)
	}
}

// TestUninstallBrokenOffInTheMiddle: the step is broken at each of its three
// writes in turn, and what holds afterwards is measured. The one state that
// must never come about is an app without a row -- it could then be neither
// seen nor removed -- so a failure before the app row leaves the row standing
// and a repeat finishes the job.
func TestUninstallBrokenOffInTheMiddle(t *testing.T) {
	for _, c := range []struct {
		fail    string
		rowLeft bool
		uiLeft  bool
	}{
		{"blobdelete:ui", true, true},
		{"blobdelete:approval", true, false},
		{"appdelete", true, false},
	} {
		t.Run(c.fail, func(t *testing.T) {
			af := newActFixture(t)
			af.activated(t, "long file name", "org.sorglos.calculator", 0x7a)
			af.S.DB = &failDB{Store: af.S.DB, fail: c.fail}
			p := &core.UninstallParams{App: "org.sorglos.calculator"}
			rq := UninstallRequest{Carried: af.carried(t, ctlNode, core.ActUninstall, p, 0x7b),
				Params: p}
			code, err := af.St.Uninstall(ctx, rq)
			if code != wire.CodeInternal || !errors.Is(err, errBroken) {
				t.Fatalf("code %q, err %v", code, err)
			}
			_, uiErr := af.S.DB.BlobGet(ctx, dbapi.BlobUI, "org.sorglos.calculator@1.0.0")
			if (uiErr == nil) != c.uiLeft {
				t.Fatalf("ui-tar there: %v, want %v", uiErr == nil, c.uiLeft)
			}
			_, rowErr := af.S.DB.AppGet(ctx, "org.sorglos.calculator")
			if (rowErr == nil) != c.rowLeft {
				t.Fatalf("app row there: %v, want %v", rowErr == nil, c.rowLeft)
			}
			// The repeat: with the database whole again the same uninstall
			// finishes, which is what makes the broken state recoverable
			// rather than merely survivable.
			af.S.DB = af.S.DB.(*failDB).Store
			p2 := &core.UninstallParams{App: "org.sorglos.calculator"}
			rq2 := UninstallRequest{Carried: af.carried(t, ctlNode, core.ActUninstall, p2, 0x7c),
				Params: p2}
			if code, err := af.St.Uninstall(ctx, rq2); code != "" || err != nil {
				t.Fatalf("the repeat did not finish: code %q, err %v", code, err)
			}
			if _, err := af.S.DB.BlobGet(ctx, dbapi.BlobUI, "org.sorglos.calculator@1.0.0"); err == nil {
				t.Fatalf("the ui-tar outlived the repeat")
			}
			if _, err := af.S.DB.AppGet(ctx, "org.sorglos.calculator"); !errors.Is(err, dbapi.ErrNotFound) {
				t.Fatalf("the row outlived the repeat: %v", err)
			}
		})
	}
}

// TestActivateBrokenOffInTheMiddle: the controller has the app, and the
// bookkeeping fails. The app row comes first, so the app can always be seen
// and removed; the UI rows are what a repeat of the same activation writes
// again.
func TestActivateBrokenOffInTheMiddle(t *testing.T) {
	for _, c := range []struct {
		fail    string
		rowLeft bool
		uiLeft  bool
	}{
		// The app row comes first, so a failure at it leaves no ui-tar
		// either: nothing of this version is written before the row exists.
		{"appput", false, false},
		{"blobput:ui", true, false},
		{"blobput:approval", true, true},
	} {
		t.Run(c.fail, func(t *testing.T) {
			af := newActFixture(t)
			pkg, ui := vectorPackage(t, "long file name")
			af.S.DB = &failDB{Store: af.S.DB, fail: c.fail}
			rq := af.uploadRequest(t, ctlNode, pkg, ui, "org.sorglos.calculator", "1.0.0", nil, 0x7d)
			code, _ := af.St.Activate(ctx, rq)
			if code == "" {
				t.Fatalf("a broken write read as a success")
			}
			if len(af.Send.sents) != 1 {
				t.Fatalf("the app did not reach the controller at all")
			}
			_, rowErr := af.S.DB.AppGet(ctx, "org.sorglos.calculator")
			if (rowErr == nil) != c.rowLeft {
				t.Fatalf("app row there: %v, want %v", rowErr == nil, c.rowLeft)
			}
			_, uiErr := af.S.DB.BlobGet(ctx, dbapi.BlobUI, "org.sorglos.calculator@1.0.0")
			if (uiErr == nil) != c.uiLeft {
				t.Fatalf("ui-tar there: %v, want %v", uiErr == nil, c.uiLeft)
			}
			if got := af.spoolFiles(t); len(got) != 0 {
				t.Fatalf("the spool still holds %v", got)
			}
			af.S.DB = af.S.DB.(*failDB).Store
			rq2 := af.uploadRequest(t, ctlNode, pkg, ui, "org.sorglos.calculator", "1.0.0", nil, 0x7e)
			if code, err := af.St.Activate(ctx, rq2); code != "" || err != nil {
				t.Fatalf("the repeat did not finish: code %q, err %v", code, err)
			}
			if _, err := af.St.Proof(ctx, "org.sorglos.calculator", "1.0.0"); err != nil {
				t.Fatalf("the repeat did not write the evidence: %v", err)
			}
		})
	}
}

// TestUninstallOnAnotherNodeIsRefused: a task that names a node the row does
// not would otherwise take the row of an app that keeps running elsewhere.
func TestUninstallOnAnotherNodeIsRefused(t *testing.T) {
	af := newActFixture(t)
	af.activated(t, "long file name", "org.sorglos.calculator", 0x7f)
	p := &core.UninstallParams{App: "org.sorglos.calculator"}
	rq := UninstallRequest{Carried: af.carried(t, plainNode, core.ActUninstall, p, 0x80), Params: p}
	if code, err := af.St.Uninstall(ctx, rq); code != wire.CodeForbidden || err != nil {
		t.Fatalf("code %q, err %v", code, err)
	}
	if len(af.Send.sents) != 1 {
		t.Fatalf("the uninstall reached a controller: %d channels (1 is the activation)",
			len(af.Send.sents))
	}
	if _, err := af.S.DB.AppGet(ctx, "org.sorglos.calculator"); err != nil {
		t.Fatalf("the row went anyway: %v", err)
	}
}

// TestUninstallOfWhatIsNotThere: an app with no row is E_NOTFOUND, and no
// controller is asked.
func TestUninstallOfWhatIsNotThere(t *testing.T) {
	af := newActFixture(t)
	p := &core.UninstallParams{App: "org.example.ghost"}
	rq := UninstallRequest{Carried: af.carried(t, ctlNode, core.ActUninstall, p, 0x81), Params: p}
	if code, err := af.St.Uninstall(ctx, rq); code != wire.CodeNotFound || err != nil {
		t.Fatalf("code %q, err %v", code, err)
	}
	if len(af.Send.sents) != 0 {
		t.Fatalf("a controller was asked about an app with no row")
	}
}

// TestUninstallKeepsNothingWhenTheControllerRefuses: a code from the
// controller leaves every row where it was.
func TestUninstallKeepsNothingWhenTheControllerRefuses(t *testing.T) {
	af := newActFixture(t)
	af.activated(t, "long file name", "org.sorglos.calculator", 0x82)
	before := inventory(t, af.S.DB)
	af.Send.code = wire.CodeForbidden
	p := &core.UninstallParams{App: "org.sorglos.calculator"}
	rq := UninstallRequest{Carried: af.carried(t, ctlNode, core.ActUninstall, p, 0x83), Params: p}
	if code, err := af.St.Uninstall(ctx, rq); code != wire.CodeForbidden || err != nil {
		t.Fatalf("code %q, err %v", code, err)
	}
	gone, added, changed := diff(before, inventory(t, af.S.DB))
	if len(gone)+len(added)+len(changed) != 0 {
		t.Fatalf("a refused uninstall changed the database: gone %v, added %v, changed %v",
			gone, added, changed)
	}
}

// TestInstalledRows: one row per app, by ID, with the state of its node and
// of its licence. Three states and three licence states, each in one row.
func TestInstalledRows(t *testing.T) {
	af := newActFixture(t)
	af.activated(t, "long file name", "org.sorglos.calculator", 0x84)
	af.activated(t, "no ui files", "org.sorglos.backup", 0x85)
	// A third app on a node with no tunnel, and kept data of a fourth.
	if err := af.S.DB.AppPut(ctx, dbapi.App{ID: "org.example.away", Version: "2.0.0",
		Store: store.GlobalID, Node: "ctl-gone", Active: true, Manifest: []byte(`{}`),
		SHA256: bytes.Repeat([]byte{2}, 32), Updated: testNow}); err != nil {
		t.Fatal(err)
	}
	if err := af.S.DB.AppPut(ctx, dbapi.App{ID: "org.example.kept", Store: store.GlobalID,
		Node: ctlNode, KeptData: 4096, Updated: testNow}); err != nil {
		t.Fatal(err)
	}
	rows, err := af.St.Installed(ctx)
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("%d rows, want 4", len(rows))
	}
	ids := []string{}
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	if !slices.IsSorted(ids) {
		t.Fatalf("rows are not in the order of the database: %v", ids)
	}
	by := map[string]core.InstalledApp{}
	for _, r := range rows {
		by[r.ID] = r
	}
	if r := by["org.sorglos.calculator"]; r.State != core.AppOK || r.License != core.LicenseNone ||
		!r.Active || r.Version != "1.0.0" || r.Store != core.LocalStore {
		t.Fatalf("calculator: %+v", r)
	}
	if r := by["org.example.away"]; r.State != core.AppUnreachable {
		t.Fatalf("an app on a node without a tunnel: %+v", r)
	}
	if r := by["org.example.kept"]; r.Version != "" || r.KeptData != 4096 {
		t.Fatalf("kept data: %+v", r)
	}
}

// TestInstalledReadsAnUnreadableLicenceAsExpired: a licence row this API
// cannot judge any more must never read as valid, and the app then counts as
// unlicensed.
func TestInstalledReadsAnUnreadableLicenceAsExpired(t *testing.T) {
	af := newActFixture(t)
	af.activated(t, "long file name", "org.sorglos.calculator", 0x86)
	if err := af.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobLicense,
		ID: "org.sorglos.calculator", Data: []byte("not an envelope"), Updated: testNow}); err != nil {
		t.Fatal(err)
	}
	rows, err := af.St.Installed(ctx)
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	for _, r := range rows {
		if r.ID != "org.sorglos.calculator" {
			continue
		}
		if r.License != core.LicenseExpired || r.State != core.AppUnlicensed {
			t.Fatalf("licence %q, state %q", r.License, r.State)
		}
		return
	}
	t.Fatalf("no row for the app")
}

// ---------------------------------------------------------------------------
// The mapping onto wire codes
// ---------------------------------------------------------------------------

// TestCodeForEveryError nails the mapping task 20 left open, both ways: every
// error the steps of this package answer with gets the code named here, and
// nothing maps to E_VERIFY by accident.
func TestCodeForEveryError(t *testing.T) {
	for _, c := range []struct {
		err  error
		want wire.Code
	}{
		{nil, ""},
		{ErrTooLarge, wire.CodeTooLarge},
		{ErrLicense, wire.CodeLicense},
		{ErrStore, wire.CodeUnreachable},
		{dbapi.ErrNotFound, wire.CodeNotFound},
		{store.ErrPackage, wire.CodeVerify},
		{store.ErrManifest, wire.CodeVerify},
		{store.ErrEncrypted, wire.CodeVerify},
		{store.ErrCatalog, wire.CodeVerify},
		{store.ErrRelease, wire.CodeVerify},
		{store.ErrRollback, wire.CodeVerify},
		{store.ErrLicense, wire.CodeVerify},
		{core.ErrUIProof, wire.CodeVerify},
		{core.ErrTransfer, wire.CodeVerify},
		{sign.ErrBadSignature, wire.CodeVerify},
		{sign.ErrWrongKey, wire.CodeVerify},
		{sign.ErrEnvelope, wire.CodeVerify},
		{dbapi.ErrInvalid, wire.CodeInternal},
		{errBroken, wire.CodeInternal},
	} {
		if got := CodeFor(c.err); got != c.want {
			t.Fatalf("CodeFor(%v) = %q, want %q", c.err, got, c.want)
		}
		// Wrapped, which is how an error arrives from a step below.
		if c.err == nil {
			continue
		}
		if got := CodeFor(wrapped{c.err}); got != c.want {
			t.Fatalf("CodeFor(wrapped %v) = %q, want %q", c.err, got, c.want)
		}
	}
}

// wrapped is an error with another one inside, so the mapping is measured
// through errors.Is and not through equality.
type wrapped struct{ in error }

func (w wrapped) Error() string { return "wrapped: " + w.in.Error() }
func (w wrapped) Unwrap() error { return w.in }

// TestAnUnsafePackageIsEVerifyOnTheWay: the open end of task 20 run through
// this package -- an archive store.Walk refuses comes out of an activation as
// E_VERIFY, and nothing is written or sent.
func TestAnUnsafePackageIsEVerifyOnTheWay(t *testing.T) {
	af := newActFixture(t)
	bad := buildPackage(t, append(goodEntries(t),
		packEntry{name: "ui/../../etc/passwd", body: []byte("x"), kind: tar.TypeReg}))
	var uiBuf bytes.Buffer
	if _, _, err := store.PackageUI(bytes.NewReader(bad), &uiBuf); !errors.Is(err, store.ErrPackage) {
		t.Fatalf("the package is not the one the contract refuses: %v", err)
	}
	sum := sha256.Sum256(bad)
	p := &core.ActivateParams{App: "org.example.notes", Version: "0.1.0", SHA256: sum[:],
		UI: bytes.Repeat([]byte{1}, 32), Store: core.LocalStore}
	rq := ActivateRequest{Carried: af.carried(t, ctlNode, core.ActActivate, p, 0x87), Params: p,
		File: bytes.NewReader(bad), Size: int64(len(bad))}
	if code, err := af.St.Activate(ctx, rq); code != wire.CodeVerify || err != nil {
		t.Fatalf("code %q, err %v", code, err)
	}
	if len(af.Send.sents) != 0 || len(af.spoolFiles(t)) != 0 {
		t.Fatalf("an unsafe package travelled or stayed behind")
	}
}

// TestActivateRefusesBeyondTheBound: the bound of the frame, from this side.
// A file longer than what this API takes is E_TOOLARGE and not E_UNREACHABLE,
// which is the whole reason ErrTooLarge exists. Both sides: at the bound it
// goes through.
func TestActivateRefusesBeyondTheBound(t *testing.T) {
	pkg, ui := vectorPackage(t, "long file name")
	af := newActFixture(t)
	af.St.MaxUpload = int64(len(pkg))
	if code, err := af.St.Activate(ctx, af.uploadRequest(t, ctlNode, pkg, ui,
		"org.sorglos.calculator", "1.0.0", nil, 0x88)); code != "" || err != nil {
		t.Fatalf("at the bound: code %q, err %v", code, err)
	}
	af2 := newActFixture(t)
	af2.St.MaxUpload = int64(len(pkg)) - 1
	code, err := af2.St.Activate(ctx, af2.uploadRequest(t, ctlNode, pkg, ui,
		"org.sorglos.calculator", "1.0.0", nil, 0x89))
	if code != wire.CodeTooLarge || err != nil {
		t.Fatalf("one byte over the bound: code %q, err %v", code, err)
	}
	if len(af2.Send.sents) != 0 || len(af2.spoolFiles(t)) != 0 {
		t.Fatalf("the oversized file travelled or stayed behind")
	}
}

// TestActivateRefusesALicenceOfAnotherStore: a licence is only ever the one
// of the store the app comes from. The activation names that store in its
// signed task, and a licence of another store of the list is refused -- the
// property task 21 holds for a licence read off the app's row must not be
// lost on the way where there is no row yet.
func TestActivateRefusesALicenceOfAnotherStore(t *testing.T) {
	af := newActFixture(t)
	sealed, err := af.S.Box.Seal(installPair.Private)
	if err != nil {
		t.Fatal(err)
	}
	if err := af.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallKey,
		Data: sealed, Updated: testNow}); err != nil {
		t.Fatal(err)
	}
	lic := mint(t, "org.sorglos.terminal", func(l *store.License) { l.Store = "elsewhere" })
	if err := af.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobLicense, ID: "org.sorglos.terminal",
		Data: lic, Updated: testNow}); err != nil {
		t.Fatal(err)
	}
	pkg, ui := vectorPackage(t, "files, directories and an image")
	af.listCatalog(t, "org.sorglos.terminal", "1.0.0", pkg, ui, nil,
		&store.Price{Model: store.PriceSubscription, Per: store.PerSite, Amount: 499,
			Currency: "EUR", Period: store.PeriodMonth, Shop: "https://shop.example/terminal"})
	rq := af.storeRequest(t, ctlNode, "org.sorglos.terminal", "1.0.0", pkg, ui, nil, 0x8a)
	code, err := af.St.Activate(ctx, rq)
	if code != wire.CodeLicense || err != nil {
		t.Fatalf("code %q, err %v", code, err)
	}
	if len(af.Send.sents) != 0 {
		t.Fatalf("the paid app travelled without a licence that holds")
	}
}
