package updates

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"sync"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/appstore"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/audit"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

var ctx = context.Background()

// testNow is 14:13:20 UTC on 17.10.2026. The hour matters: the window cases run
// against a fixed zone (fx.U.Zone = time.UTC), so the clock of the machine
// this runs on never decides anything.
const testNow = int64(1792246400)

const (
	ctlNode   = "ctl-1"  // the controller of trusttest.Genesis: roles services + api
	dbNode    = "ctl-db" // a second controller, role db
	webNode   = "ctl-w"  // a third controller, role web
	theApp    = "org.sorglos.calculator"
	otherApp  = "org.sorglos.terminal"
	pkgFile   = "pkg.ngc"
	partFile  = "ngc-api-amd64.tar"
	partFile2 = "ngc-controller-amd64.tar"
	partFile3 = "ngc-controller-arm64.tar"
	webFile   = "ngc-web-all.tar"
)

// The keys these cases sign with. They are the fixed seeds of trusttest and of
// the vectors of shared/testdata, not keys made up here: relPriv is the key
// trusttest.Genesis names in its standing approval for ngc-controller, so a
// release signed with it is one this installation really approves, and a
// release signed with anything else is really a stranger's.
var (
	relPriv   = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{trusttest.ReleaseFill}, ed25519.SeedSize))
	storePriv = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, ed25519.SeedSize))
	alienPriv = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x5A}, ed25519.SeedSize))
)

// ---------------------------------------------------------------------------
// The fleet: what is connected right now.
// ---------------------------------------------------------------------------

type seenNode struct {
	hello  tunnel.Hello
	status tunnel.Status
}

type fakeFleet struct {
	mu    sync.Mutex
	nodes map[string]seenNode
}

func newFleet() *fakeFleet { return &fakeFleet{nodes: map[string]seenNode{}} }

// put makes node online with these roles, this architecture, this NGC version
// and these installed components.
func (f *fakeFleet) put(node, arch, version string, roles []string, comp map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nodes[node] = seenNode{
		hello:  tunnel.Hello{Node: node, Version: version, Arch: arch, Roles: roles},
		status: tunnel.Status{Components: comp},
	}
}

func (f *fakeFleet) drop(node string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.nodes, node)
}

func (f *fakeFleet) Seen(node string) (tunnel.Hello, tunnel.Status, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[node]
	return n.hello, n.status, ok
}

func (f *fakeFleet) Online() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.nodes))
	for n := range f.nodes {
		out = append(out, n)
	}
	return out
}

// The compiler holds that the registry of operation fits the same interface.
var _ Fleet = (*fakeFleet)(nil)

// ---------------------------------------------------------------------------
// The sender: the whole way out of this package.
// ---------------------------------------------------------------------------

// sent is one call that left the API. A case that finds nothing in sents knows
// nothing left it at all, which is how "refused before anything travelled" is
// measured rather than assumed.
type sent struct {
	node, action string
	params       json.RawMessage
	head         core.Transfer
	file         []byte
	transfer     bool
	hadUser      bool // a task, a client certificate or a proof travelled
}

type fakeSender struct {
	mu     sync.Mutex
	fleet  *fakeFleet
	sents  []sent
	codes  map[string]wire.Code // action -> the code the controller answers
	onSend func(s sent)         // runs after a call was recorded, inside no lock
}

func (f *fakeSender) Node(node string) (tunnel.Hello, bool) {
	h, _, ok := f.fleet.Seen(node)
	return h, ok
}

func (f *fakeSender) record(s sent) wire.Code {
	f.mu.Lock()
	f.sents = append(f.sents, s)
	code := f.codes[s.action]
	hook := f.onSend
	f.mu.Unlock()
	if hook != nil {
		hook(s)
	}
	return code
}

func (f *fakeSender) Transfer(_ context.Context, node, action string, params json.RawMessage,
	head core.Transfer, file io.Reader, tk *sign.Envelope, client []byte,
	pr, confirm *proof.Proof) ([]byte, wire.Code) {
	b, err := io.ReadAll(file)
	if err != nil {
		return nil, wire.CodeUnreachable
	}
	return nil, f.record(sent{node: node, action: action, params: bytes.Clone(params), head: head,
		file: b, transfer: true, hadUser: tk != nil || client != nil || pr != nil})
}

func (f *fakeSender) Call(_ context.Context, node, action string, params json.RawMessage,
	tk *sign.Envelope, client []byte, pr, confirm *proof.Proof) ([]byte, wire.Code) {
	return nil, f.record(sent{node: node, action: action, params: bytes.Clone(params),
		hadUser: tk != nil || client != nil || pr != nil})
}

func (f *fakeSender) list() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sent(nil), f.sents...)
}

// only returns the calls of one action.
func (f *fakeSender) only(action string) []sent {
	var out []sent
	for _, s := range f.list() {
		if s.action == action {
			out = append(out, s)
		}
	}
	return out
}

func (f *fakeSender) answer(action string, code wire.Code) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes[action] = code
}

var _ appstore.Sender = (*fakeSender)(nil)

// ---------------------------------------------------------------------------
// The store: a real TLS server that serves files.
// ---------------------------------------------------------------------------

type served struct {
	mu    sync.Mutex
	files map[string][]byte
	hits  int
	// last is the whole request line of the last request, not only the name
	// the answer is picked by. Without it this fixture answered by the base
	// name of the path alone, so a release fetched out of a directory of
	// someone else's choosing, or with a token appended to the address, was
	// served and every case stayed green -- the blindness that let the pairing
	// request travel without its ?secret= (point 61).
	last seenRequest
}

// seenRequest is a request line of the store in the three parts a store
// address is made of.
type seenRequest struct {
	Method   string
	Path     string
	RawQuery string
}

func (sv *served) put(name string, b []byte) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	sv.files[name] = b
}

func (sv *served) seen() seenRequest {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	return sv.last
}

func (sv *served) hitCount() int {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	return sv.hits
}

// ---------------------------------------------------------------------------
// The fixture.
// ---------------------------------------------------------------------------

type fixture struct {
	U     *Updates
	S     *state.State
	St    *appstore.Stores
	Fleet *fakeFleet
	Send  *fakeSender
	Serve *served
	URL   string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	sv := &served{files: map[string][]byte{}}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sv.mu.Lock()
		sv.hits++
		name := r.URL.Path
		if i := len(name) - 1; i >= 0 {
			for j := i; j >= 0; j-- {
				if name[j] == '/' {
					name = name[j+1:]
					break
				}
			}
		}
		b, ok := sv.files[name]
		sv.last = seenRequest{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery}
		sv.mu.Unlock()
		// The rule, kept here so that every case of this package runs through
		// it: a store is read with GET, the file lies directly under the
		// address of the store, and nothing is appended to that address
		// (trust.ValidStoreURL has no query and no trailing slash).
		if r.Method != http.MethodGet {
			t.Errorf("the store was asked with %s; it is read with GET only", r.Method)
		}
		if r.URL.Path != "/"+name {
			t.Errorf("the store was asked for %q, not for /%s: the address of a store carries no directory of its own", r.URL.Path, name)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("the store was asked with the query %q; a store address has none, so nothing may append one", r.URL.RawQuery)
		}
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)

	box, err := secrets.NewBox(config.NewSecret(make([]byte, 32)))
	if err != nil {
		t.Fatalf("box: %v", err)
	}
	noiseKey, signKey := trusttest.NodeKeys(0xA1)
	_, apiPin := trusttest.APITLS(0xE1, "api-1.test")
	relPub := relPriv.Public().(ed25519.PublicKey)
	cfg := &config.API{
		Node: "api-1", URL: "wss://api-1.test:7443/tunnel", Listen: ":7443",
		Cert: "/x", CertKey: "/y",
		NoiseKey: config.NewSecret(noiseKey.Private), SignKey: config.NewSecret(signKey.Seed()),
		SecretKey:    config.NewSecret(make([]byte, 32)),
		ChallengeKey: config.NewSecret(bytes.Repeat([]byte{7}, 32)),
		ReleaseKey:   enc.Bytes(relPub),
		StoreKey:     enc.Bytes(storePriv.Public().(ed25519.PublicKey)),
		StoreURL:     srv.URL, SessionLifetime: 43200, IdleTimeout: 1800,
	}
	s := state.New(cfg, memstore.New(), box, apiPin)
	s.Now = func() int64 { return testNow }

	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	signed := admin.Sign(t, l)
	if err := s.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallation,
		Data: []byte(l.Installation), Updated: testNow}); err != nil {
		t.Fatalf("installation: %v", err)
	}
	if _, err := s.AddVersion(ctx, signed); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	// The standing approval of Genesis for ngc-controller names
	// NodeKeys(trusttest.ReleaseFill); the configuration has to carry that
	// same key, or every case below would measure a mismatch instead of what
	// it says it measures.
	if !store.Approved(l, trust.AutoController, cfg.ReleaseKey) {
		t.Fatalf("the genesis approval does not name the configured release key")
	}
	// Version 2 points the global store at this test's server, gives it the
	// key these cases sign catalogs with, and adds the two other controllers.
	dbDH, _ := trusttest.NodeKeys(0xC2)
	webDH, _ := trusttest.NodeKeys(0xC3)
	l2 := trusttest.Next(t, signed)
	l2.Stores = []trust.Store{{ID: store.GlobalID, Name: "NGC Store", URL: srv.URL,
		Key: enc.Bytes(storePriv.Public().(ed25519.PublicKey))}}
	l2.Nodes = append(l2.Nodes,
		trust.Node{ID: dbNode, Kind: trust.NodeController, Name: "Datenbank", Noise: dbDH.Public},
		trust.Node{ID: webNode, Kind: trust.NodeController, Name: "Web", Noise: webDH.Public})
	if _, err := s.AddVersion(ctx, admin.Sign(t, l2)); err != nil {
		t.Fatalf("version 2: %v", err)
	}

	st := appstore.New(s)
	st.Client.Transport = srv.Client().Transport
	fleet := newFleet()
	send := &fakeSender{fleet: fleet, codes: map[string]wire.Code{}}
	fx := fixture{S: s, St: st, Fleet: fleet, Send: send, Serve: sv, URL: srv.URL}
	fx.U = &Updates{S: s, Stores: st, Tunnels: fleet, Send: send,
		Log: &audit.Log{DB: s.DB, Now: s.Now}, Spool: t.TempDir()}
	return fx
}

// nextVersion is the newest stored list, ready to be changed and signed again.
func (fx fixture) nextVersion(t *testing.T) *trust.List {
	t.Helper()
	v, err := fx.S.Trust(ctx)
	if err != nil {
		t.Fatalf("Trust: %v", err)
	}
	b, err := fx.S.DB.BlobGet(ctx, dbapi.BlobTrust, strconv.FormatUint(v.List.Seq, 10))
	if err != nil {
		t.Fatalf("version row: %v", err)
	}
	sg, err := trust.ParseSigned(b.Data)
	if err != nil {
		t.Fatalf("version row: %v", err)
	}
	return trusttest.Next(t, sg)
}

// sign writes a new trust list version.
func (fx fixture) signList(t *testing.T, l *trust.List) {
	t.Helper()
	if _, err := fx.S.AddVersion(ctx, trusttest.NewSigner(1).Sign(t, l)); err != nil {
		t.Fatalf("new version: %v", err)
	}
}

// setAuto puts a standing approval for target with key into the trust list,
// replacing the one that is there. trust.List keeps Auto sorted by target and
// refuses a duplicate, so this helper sorts and replaces instead of appending.
func (fx fixture) setAuto(t *testing.T, target string, key ed25519.PublicKey) {
	t.Helper()
	l := fx.nextVersion(t)
	next := make([]trust.Auto, 0, len(l.Auto)+1)
	for _, a := range l.Auto {
		if a.Target != target {
			next = append(next, a)
		}
	}
	next = append(next, trust.Auto{Target: target, Key: enc.Bytes(key)})
	sort.Slice(next, func(i, j int) bool { return next[i].Target < next[j].Target })
	l.Auto = next
	fx.signList(t, l)
}

// approve adds a standing approval for each target with the release key.
func (fx fixture) approve(t *testing.T, targets ...string) {
	t.Helper()
	for _, target := range targets {
		fx.setAuto(t, target, relPriv.Public().(ed25519.PublicKey))
	}
}

// serveRelease signs a release with priv, serves it and the files it names,
// and makes sure the API really reads it: a release this API would refuse
// would make every case below pass for the wrong reason.
func (fx fixture) serveRelease(t *testing.T, priv ed25519.PrivateKey, seq uint64, version string,
	bodies map[string][]byte) *store.Release {
	t.Helper()
	rel := store.Release{Seq: seq, Issued: testNow, Version: version}
	for name, body := range bodies {
		sum := sha256.Sum256(body)
		part, arch := partOf(t, name)
		rel.Files = append(rel.Files, store.File{Part: part, Arch: arch, URL: name,
			Size: int64(len(body)), SHA256: sum[:]})
		fx.Serve.put(name, body)
	}
	payload, err := json.Marshal(rel)
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.Sign(priv, sign.PurposeRelease, payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	fx.Serve.put(appstore.ReleaseFile, raw)
	return &rel
}

// partOf is the part and the architecture of one of the file names these cases
// use. A name no case named is a mistake in the case, not a finding.
func partOf(t *testing.T, name string) (string, string) {
	t.Helper()
	switch name {
	case partFile:
		return trust.AutoAPI, "amd64"
	case partFile2:
		return trust.AutoController, "amd64"
	case partFile3:
		return trust.AutoController, "arm64"
	case webFile:
		return trust.AutoWeb, "all"
	}
	t.Fatalf("unknown release file %q", name)
	return "", ""
}

// listCatalog signs a catalog of the global store with priv, stores it and
// checks that this API reads it back.
func (fx fixture) listCatalog(t *testing.T, priv ed25519.PrivateKey, seq uint64, entries ...store.Entry) {
	t.Helper()
	c := store.Catalog{Store: store.GlobalID, Seq: seq, Issued: testNow, Apps: entries}
	payload, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.Sign(priv, sign.PurposeCatalog, payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := fx.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobCatalog, ID: store.GlobalID,
		Seq: seq, Data: raw, Updated: testNow}); err != nil {
		t.Fatalf("catalog row: %v", err)
	}
}

// entry is one app of a catalog with one version.
func entry(app, version string, needs []string, minNGC string) store.Entry {
	body := []byte("package of " + app + " " + version)
	sum := sha256.Sum256(body)
	ui := sha256.Sum256([]byte("ui of " + app + " " + version))
	return store.Entry{ID: app, Name: "Testapp", Publisher: "Sorglos IT",
		Versions: []store.Package{{Version: version, URL: pkgFile, Size: int64(len(body)),
			SHA256: sum[:], UI: ui[:], Needs: needs, MinNGC: minNGC}}}
}

// putApp writes the row of an installed app.
func (fx fixture) putApp(t *testing.T, app, version, from, node string, grants []string) {
	t.Helper()
	if err := fx.S.DB.AppPut(ctx, dbapi.App{ID: app, Version: version, Store: from, Node: node,
		Active: true, Grants: grants, Updated: testNow}); err != nil {
		t.Fatalf("app row: %v", err)
	}
}

// rowOf returns the row of one target on one node.
func rowOf(t *testing.T, rows []core.UpdateInfo, target, node string) core.UpdateInfo {
	t.Helper()
	for _, r := range rows {
		if r.Target == target && r.Node == node {
			return r
		}
	}
	t.Fatalf("no row for %s on %s; rows: %s", target, node, names(rows))
	return core.UpdateInfo{}
}

func findRow(rows []core.UpdateInfo, target, node string) (core.UpdateInfo, bool) {
	for _, r := range rows {
		if r.Target == target && r.Node == node {
			return r, true
		}
	}
	return core.UpdateInfo{}, false
}

func names(rows []core.UpdateInfo) string {
	var b bytes.Buffer
	for _, r := range rows {
		b.WriteString(r.Target + "@" + r.Node + " ")
	}
	return b.String()
}

// stageParams reads the params of a staged call back through the parser of the
// contract, so a case never reads a field this package wrote without the
// contract agreeing it is one.
func stageParams(t *testing.T, s sent) core.StageParams {
	t.Helper()
	var p core.StageParams
	if err := json.Unmarshal(s.params, &p); err != nil {
		t.Fatalf("stage params: %v", err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("stage params do not validate: %v", err)
	}
	return p
}

func installParams(t *testing.T, s sent) core.InstallParams {
	t.Helper()
	var p core.InstallParams
	if err := json.Unmarshal(s.params, &p); err != nil {
		t.Fatalf("install params: %v", err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("install params do not validate: %v", err)
	}
	return p
}

// alienPub and storePub are the public halves of the two keys above.
func alienPub() ed25519.PublicKey { return alienPriv.Public().(ed25519.PublicKey) }
func storePub() ed25519.PublicKey { return storePriv.Public().(ed25519.PublicKey) }

// putCatalogFor stores a catalog of another store, signed with priv.
func (fx fixture) putCatalogFor(t *testing.T, id string, priv ed25519.PrivateKey, seq uint64,
	entries ...store.Entry) {
	t.Helper()
	c := store.Catalog{Store: id, Seq: seq, Issued: testNow, Apps: entries}
	payload, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.Sign(priv, sign.PurposeCatalog, payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := fx.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobCatalog, ID: id,
		Seq: seq, Data: raw, Updated: testNow}); err != nil {
		t.Fatalf("catalog row: %v", err)
	}
}

// settings are the IDs of the stored update settings, sorted. It is the
// measuring instrument of "nothing of a row is stored": a row that was kept
// anywhere would have to be a setting row, because this package writes no
// other kind.
func (fx fixture) settings(t *testing.T) []string {
	t.Helper()
	bs, err := fx.S.DB.BlobFrom(ctx, dbapi.BlobSetting, 0)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	var out []string
	for _, b := range bs {
		if len(b.ID) >= len(dbapi.SettingUpdate) && b.ID[:len(dbapi.SettingUpdate)] == dbapi.SettingUpdate {
			out = append(out, b.ID)
		}
	}
	sort.Strings(out)
	return out
}

// relPub is the public half of the release key these cases sign with.
func relPub() ed25519.PublicKey { return relPriv.Public().(ed25519.PublicKey) }

// TestTheStoreIsAskedForOneFileWithGetAndNoQuery says out loud what the
// fixture now holds for every case of this package: the whole request line is
// looked at, not only the file name the answer is picked by. The pairing
// request of the controller travelled without its ?secret= for as long as it
// did because its own test stand read path, method and body and never the
// address (point 61); this fixture had the same blindness from the other side.
func TestTheStoreIsAskedForOneFileWithGetAndNoQuery(t *testing.T) {
	fx := newFixture(t)
	fx.theFleet()
	fx.serveRelease(t, relPriv, 3, "1.1.0", map[string][]byte{partFile: []byte("api")})
	if _, err := fx.U.Rows(ctx); err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if fx.Serve.hitCount() == 0 {
		t.Fatal("the store saw no request, so this case measures an earlier wall")
	}
	want := seenRequest{Method: http.MethodGet, Path: "/" + appstore.ReleaseFile}
	if got := fx.Serve.seen(); got != want {
		t.Fatalf("the store saw %+v, want %+v", got, want)
	}
}
