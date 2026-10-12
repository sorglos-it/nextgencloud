package updates

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// theFleet puts the three controllers of the fixture online: one that runs the
// API and the app containers, one database server, one web server.
func (fx fixture) theFleet() {
	fx.Fleet.put(ctlNode, "amd64", "1.0.0", []string{tunnel.RoleAPI, tunnel.RoleServices},
		map[string]string{trust.AutoAPI: "1.0.0", trust.AutoController: "1.0.0"})
	fx.Fleet.put(dbNode, "amd64", "1.0.0", []string{tunnel.RoleDB},
		map[string]string{trust.AutoController: "1.0.0"})
	fx.Fleet.put(webNode, "arm64", "1.0.0", []string{tunnel.RoleWeb},
		map[string]string{trust.AutoController: "1.0.0", trust.AutoWeb: "1.0.0"})
}

func TestRowsPerTargetAndNode(t *testing.T) {
	fx := newFixture(t)
	fx.theFleet()
	fx.serveRelease(t, relPriv, 3, "1.1.0", map[string][]byte{
		partFile: []byte("api"), partFile2: []byte("controller"), webFile: []byte("web")})
	fx.listCatalog(t, storePriv, 9, entry(theApp, "1.2.0", nil, ""))
	fx.putApp(t, theApp, "1.0.0", store.GlobalID, ctlNode, nil)

	rows, err := fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	// One row per NGC part the node's roles name (tunnel.Components), system
	// on every node, the app on the node it runs on -- as a SET, so a row
	// nobody expected is a failure too.
	want := []string{
		theApp + "@" + ctlNode,
		trust.AutoAPI + "@" + ctlNode,
		trust.AutoController + "@" + ctlNode,
		trust.AutoController + "@" + dbNode,
		trust.AutoController + "@" + webNode,
		trust.AutoWeb + "@" + webNode,
		trust.AutoSystem + "@" + ctlNode,
		trust.AutoSystem + "@" + dbNode,
		trust.AutoSystem + "@" + webNode,
	}
	var got []string
	for _, r := range rows {
		got = append(got, r.Target+"@"+r.Node)
	}
	sorted := slices.Clone(got)
	sort.Strings(want)
	sortedWant := slices.Clone(want)
	sort.Strings(sorted)
	sort.Strings(sortedWant)
	if !slices.Equal(sorted, sortedWant) {
		t.Fatalf("rows:\n got %v\nwant %v", sorted, sortedWant)
	}
	// The order the shell sees: target, then node.
	if !slices.IsSortedFunc(rows, func(a, b core.UpdateInfo) int {
		if a.Target != b.Target {
			return bytes.Compare([]byte(a.Target), []byte(b.Target))
		}
		return bytes.Compare([]byte(a.Node), []byte(b.Node))
	}) {
		t.Fatalf("rows are not sorted by target then node: %v", got)
	}
	// The node that runs no API gets no ngc-api row, and that is the half of
	// this that an "it contains" check would miss.
	if _, ok := findRow(rows, trust.AutoAPI, dbNode); ok {
		t.Fatalf("%s has no role api and must have no %s row", dbNode, trust.AutoAPI)
	}
	// And the versions: installed out of the status, available out of the
	// signed release, per architecture.
	api := rowOf(t, rows, trust.AutoAPI, ctlNode)
	if api.Installed != "1.0.0" || api.Available != "1.1.0" {
		t.Fatalf("%s on %s: installed %q available %q", trust.AutoAPI, ctlNode, api.Installed, api.Available)
	}
	// The web node is arm64 and the release carries ngc-web as arch "all":
	// without that rule the row would offer nothing.
	web := rowOf(t, rows, trust.AutoWeb, webNode)
	if web.Available != "1.1.0" {
		t.Fatalf("%s on %s: available %q, want 1.1.0", trust.AutoWeb, webNode, web.Available)
	}
	// system has no version to compare at all.
	sys := rowOf(t, rows, trust.AutoSystem, ctlNode)
	if sys.Installed != "" || sys.Available != "" {
		t.Fatalf("system row carries versions: %+v", sys)
	}
	app := rowOf(t, rows, theApp, ctlNode)
	if app.Installed != "1.0.0" || app.Available != "1.2.0" {
		t.Fatalf("app row: installed %q available %q", app.Installed, app.Available)
	}
}

func TestARowOffersNothingForAnArchitectureTheReleaseDoesNotCarry(t *testing.T) {
	fx := newFixture(t)
	// Only amd64 files are in this release; the controller is arm64.
	fx.Fleet.put(ctlNode, "arm64", "1.0.0", []string{tunnel.RoleAPI},
		map[string]string{trust.AutoAPI: "1.0.0", trust.AutoController: "1.0.0"})
	fx.serveRelease(t, relPriv, 3, "1.1.0", map[string][]byte{partFile: []byte("api")})

	rows, err := fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if r := rowOf(t, rows, trust.AutoAPI, ctlNode); r.Available != "" {
		t.Fatalf("an arm64 node was offered the amd64 file: %q", r.Available)
	}
	// The way to the line under measurement has to be open: the SAME release
	// on an amd64 node does offer 1.1.0, so the empty answer above is the
	// architecture and not a release this API refuses.
	fx.Fleet.put(ctlNode, "amd64", "1.0.0", []string{tunnel.RoleAPI},
		map[string]string{trust.AutoAPI: "1.0.0", trust.AutoController: "1.0.0"})
	rows, err = fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if r := rowOf(t, rows, trust.AutoAPI, ctlNode); r.Available != "1.1.0" {
		t.Fatalf("control: amd64 node got %q, want 1.1.0", r.Available)
	}
}

func TestRowLeavesOutAVersionWithAHigherMinNGC(t *testing.T) {
	cases := []struct {
		name, ngc, minNGC, want string
	}{
		{"older NGC", "1.0.0", "1.1.0", ""},
		{"exactly the minimum", "1.1.0", "1.1.0", "1.2.0"},
		{"newer NGC", "1.2.0", "1.1.0", "1.2.0"},
		{"no minimum at all", "1.0.0", "", "1.2.0"},
		// A build that names itself 0.0.0-dev is no release version and suits
		// no minNgc (store.SupportsMinNGC).
		{"a development build", "0.0.0-dev", "1.0.0", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fx := newFixture(t)
			fx.Fleet.put(ctlNode, "amd64", c.ngc, []string{tunnel.RoleServices}, nil)
			fx.listCatalog(t, storePriv, 9, entry(theApp, "1.2.0", nil, c.minNGC))
			fx.putApp(t, theApp, "1.0.0", store.GlobalID, ctlNode, nil)
			rows, err := fx.U.Rows(ctx)
			if err != nil {
				t.Fatalf("Rows: %v", err)
			}
			if got := rowOf(t, rows, theApp, ctlNode).Available; got != c.want {
				t.Fatalf("available %q, want %q", got, c.want)
			}
		})
	}
}

func TestRowOnlyFromTheOwnStore(t *testing.T) {
	fx := newFixture(t)
	fx.Fleet.put(ctlNode, "amd64", "1.0.0", []string{tunnel.RoleServices}, nil)
	// A second store, in the trust list and trusted, whose catalog carries a
	// NEWER version of the same app.
	l := fx.nextVersion(t)
	l.Stores = append(l.Stores, trust.Store{ID: "other", Name: "Fremder Store", URL: fx.URL,
		Key: enc.Bytes(alienPub())})
	fx.signList(t, l)
	fx.listCatalog(t, storePriv, 9, entry(theApp, "1.1.0", nil, ""))
	fx.putCatalogFor(t, "other", alienPriv, 9, entry(theApp, "9.9.9", nil, ""))
	fx.putApp(t, theApp, "1.0.0", store.GlobalID, ctlNode, nil)

	rows, err := fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if got := rowOf(t, rows, theApp, ctlNode).Available; got != "1.1.0" {
		t.Fatalf("available %q, want 1.1.0 -- the newer 9.9.9 is another store's", got)
	}
	// The way is open: the same app activated FROM that store does get 9.9.9,
	// so the refusal above is the store and not a catalog this API cannot
	// read.
	fx.putApp(t, theApp, "1.0.0", "other", ctlNode, nil)
	rows, err = fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if got := rowOf(t, rows, theApp, ctlNode).Available; got != "9.9.9" {
		t.Fatalf("control: available %q, want 9.9.9", got)
	}
}

func TestRowNeedsGrants(t *testing.T) {
	fx := newFixture(t)
	fx.Fleet.put(ctlNode, "amd64", "1.0.0", []string{tunnel.RoleServices}, nil)
	fx.listCatalog(t, storePriv, 9, entry(theApp, "1.1.0", []string{store.NeedRoot}, ""))
	fx.putApp(t, theApp, "1.0.0", store.GlobalID, ctlNode, nil)

	rows, err := fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	r := rowOf(t, rows, theApp, ctlNode)
	if r.State != store.UpdateNeedsGrants || r.Available != "1.1.0" {
		t.Fatalf("state %q available %q, want %q and 1.1.0", r.State, r.Available, store.UpdateNeedsGrants)
	}
	// And nothing is installed for such a row: Rollout touches apps at all
	// only through the shell, and a round of NGC parts leaves it alone.
	if sents := fx.Send.list(); len(sents) != 0 {
		t.Fatalf("something travelled for a needsgrants row: %+v", sents)
	}
	// The way is open: with the grant the app has, the same version is
	// offered as a plain update.
	fx.putApp(t, theApp, "1.0.0", store.GlobalID, ctlNode, []string{store.NeedRoot})
	rows, err = fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if r := rowOf(t, rows, theApp, ctlNode); r.State == store.UpdateNeedsGrants {
		t.Fatalf("control: still needsgrants although the grant is there")
	}
}

func TestRowsAreNotStored(t *testing.T) {
	fx := newFixture(t)
	fx.theFleet()
	fx.serveRelease(t, relPriv, 3, "1.1.0", map[string][]byte{partFile: []byte("api")})
	fx.listCatalog(t, storePriv, 9, entry(theApp, "1.2.0", nil, ""))
	fx.putApp(t, theApp, "1.0.0", store.GlobalID, ctlNode, nil)
	if err := fx.U.SetSetting(ctx, core.UpdateSetting{Target: trust.AutoController,
		Mode: store.ModeSemi}); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	before := fx.settings(t)

	if _, err := fx.U.Rows(ctx); err != nil {
		t.Fatalf("Rows: %v", err)
	}
	fx.U.Report(ctx, ctlNode, store.UpdateReport{Target: trust.AutoAPI, Version: "1.1.0",
		State: store.UpdateDownloaded})
	if _, err := fx.U.Rows(ctx); err != nil {
		t.Fatalf("Rows: %v", err)
	}
	after := fx.settings(t)
	// Exactly the settings this case wrote, and nothing else of the rows: not
	// the reported state, not the available version, not the checksum.
	if !slices.Equal(before, after) {
		t.Fatalf("rows left something behind:\nbefore %v\nafter  %v", before, after)
	}
	if len(after) != 1 || after[0] != dbapi.SettingUpdate+trust.AutoController {
		t.Fatalf("stored settings %v, want only the one that was set", after)
	}
	// A report is volatile on purpose: a fresh Updates over the same database
	// knows nothing of it.
	fresh := &Updates{S: fx.S, Stores: fx.St, Tunnels: fx.Fleet, Send: fx.Send}
	rows, err := fresh.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if r := rowOf(t, rows, trust.AutoAPI, ctlNode); r.State != "" {
		t.Fatalf("a report survived into another process: %q", r.State)
	}
	// ... and the setting did survive, so the comparison above is not simply
	// an empty database.
	if r := rowOf(t, rows, trust.AutoController, ctlNode); r.Mode != store.ModeSemi {
		t.Fatalf("the setting did not survive: %q", r.Mode)
	}
}

func TestKeyOfANGCPartIsTheReleaseKey(t *testing.T) {
	fx := newFixture(t)
	fx.theFleet()
	fx.serveRelease(t, relPriv, 3, "1.1.0", map[string][]byte{partFile: []byte("api")})
	fx.listCatalog(t, storePriv, 9, entry(theApp, "1.2.0", nil, ""))
	fx.putApp(t, theApp, "1.0.0", store.GlobalID, ctlNode, nil)
	rows, err := fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	for _, r := range rows {
		switch {
		case ngcPart(r.Target):
			if !bytes.Equal(r.Key, fx.S.Cfg.ReleaseKey) {
				t.Fatalf("%s on %s carries key %x, want the release key %x",
					r.Target, r.Node, []byte(r.Key), []byte(fx.S.Cfg.ReleaseKey))
			}
		default:
			if len(r.Key) != 0 {
				t.Fatalf("%s on %s carries a key: %x", r.Target, r.Node, []byte(r.Key))
			}
		}
	}
}

// TestTheChecksumOfARowComesOutOfTheSignedSource is the attack that matters
// most about these rows: the shell signs core.UpdateInfo.SHA256 into
// core.InstallParams, so a controller that could name a checksum here could
// name the file an admin then approves.
func TestTheChecksumOfARowComesOutOfTheSignedSource(t *testing.T) {
	fx := newFixture(t)
	fx.theFleet()
	body := []byte("the api of 1.1.0")
	fx.serveRelease(t, relPriv, 3, "1.1.0", map[string][]byte{partFile: body})
	want := sha256.Sum256(body)

	// The node reports a version NOBODY signed.
	fx.U.Report(ctx, ctlNode, store.UpdateReport{Target: trust.AutoAPI, Version: "9.9.9",
		State: store.UpdateDownloaded})
	rows, err := fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	r := rowOf(t, rows, trust.AutoAPI, ctlNode)
	if r.Available != "1.1.0" {
		t.Fatalf("a report moved the offered version to %q", r.Available)
	}
	if len(r.SHA256) != 0 {
		t.Fatalf("a checksum for a version nobody signed: %x", []byte(r.SHA256))
	}
	// The way is open: the same report for the version the release signs does
	// carry the checksum, and it is the one of the release entry.
	fx.U.Report(ctx, ctlNode, store.UpdateReport{Target: trust.AutoAPI, Version: "1.1.0",
		State: store.UpdateDownloaded})
	rows, err = fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	r = rowOf(t, rows, trust.AutoAPI, ctlNode)
	if !bytes.Equal(r.SHA256, want[:]) {
		t.Fatalf("checksum %x, want the one of the signed entry %x", []byte(r.SHA256), want[:])
	}
	// And a state that is not "downloaded" names no file at all.
	fx.U.Report(ctx, ctlNode, store.UpdateReport{Target: trust.AutoAPI, Version: "1.1.0",
		State: store.UpdateAvailable})
	rows, err = fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if r := rowOf(t, rows, trust.AutoAPI, ctlNode); len(r.SHA256) != 0 {
		t.Fatalf("a checksum for a file that is not downloaded: %x", []byte(r.SHA256))
	}
}

// TestAReleaseOfAStrangerReachesNoRow: a release signed with another key is
// refused by store.VerifyRelease, and nothing of NGC is then offered. The apps
// still are -- a store that is away must not empty the whole page.
func TestAReleaseOfAStrangerReachesNoRow(t *testing.T) {
	fx := newFixture(t)
	fx.theFleet()
	fx.listCatalog(t, storePriv, 9, entry(theApp, "1.2.0", nil, ""))
	fx.putApp(t, theApp, "1.0.0", store.GlobalID, ctlNode, nil)
	fx.serveRelease(t, alienPriv, 3, "9.9.9", map[string][]byte{partFile: []byte("api")})

	rows, err := fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if r := rowOf(t, rows, trust.AutoAPI, ctlNode); r.Available != "" {
		t.Fatalf("a release of a stranger was offered: %q", r.Available)
	}
	if r := rowOf(t, rows, theApp, ctlNode); r.Available != "1.2.0" {
		t.Fatalf("the apps were lost with the release: %q", r.Available)
	}
	// The way is open: the SAME release signed with this installation's own
	// key is offered, so the empty answer above is the signature.
	fx.serveRelease(t, relPriv, 4, "9.9.9", map[string][]byte{partFile: []byte("api")})
	rows, err = fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if r := rowOf(t, rows, trust.AutoAPI, ctlNode); r.Available != "9.9.9" {
		t.Fatalf("control: available %q, want 9.9.9", r.Available)
	}
}

// TestAReleaseTurnedBackReachesNoRow: a release whose sequence number is older
// than the newest one seen is store.ErrRollback and offers nothing -- the wall
// against an old, signed, vulnerable version being served again.
func TestAReleaseTurnedBackReachesNoRow(t *testing.T) {
	fx := newFixture(t)
	fx.theFleet()
	fx.serveRelease(t, relPriv, 7, "1.1.0", map[string][]byte{partFile: []byte("api")})
	if _, err := fx.U.Rows(ctx); err != nil {
		t.Fatalf("Rows: %v", err)
	}
	// Now the store serves an older, correctly signed release.
	fx.serveRelease(t, relPriv, 6, "1.0.5", map[string][]byte{partFile: []byte("old api")})
	rows, err := fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if r := rowOf(t, rows, trust.AutoAPI, ctlNode); r.Available != "" {
		t.Fatalf("a release turned back was offered: %q", r.Available)
	}
	// The way is open: at the same number as the one seen it is taken again.
	fx.serveRelease(t, relPriv, 7, "1.0.5", map[string][]byte{partFile: []byte("old api")})
	rows, err = fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if r := rowOf(t, rows, trust.AutoAPI, ctlNode); r.Available != "1.0.5" {
		t.Fatalf("control: available %q, want 1.0.5", r.Available)
	}
}

func TestSettingDefaultsToAutoAndIsKeptPerTarget(t *testing.T) {
	fx := newFixture(t)
	s, err := fx.U.Setting(ctx, trust.AutoController)
	if err != nil {
		t.Fatalf("Setting: %v", err)
	}
	if s.Mode != store.ModeAuto || s.Target != trust.AutoController || s.Window != "" {
		t.Fatalf("default setting %+v", s)
	}
	if err := fx.U.SetSetting(ctx, core.UpdateSetting{Target: trust.AutoController,
		Mode: store.ModeSemi, Window: "02:00-04:00"}); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	s, err = fx.U.Setting(ctx, trust.AutoController)
	if err != nil {
		t.Fatalf("Setting: %v", err)
	}
	if s.Mode != store.ModeSemi || s.Window != "02:00-04:00" {
		t.Fatalf("setting %+v", s)
	}
	// Per target: another one is untouched.
	other, err := fx.U.Setting(ctx, trust.AutoWeb)
	if err != nil {
		t.Fatalf("Setting: %v", err)
	}
	if other.Mode != store.ModeAuto {
		t.Fatalf("the setting of one target reached another: %+v", other)
	}
	// A row of this installation's own that does not read is an error, never
	// the default: a default in its place would turn "notify" back into
	// "auto" without anybody seeing it.
	if err := fx.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting,
		ID: dbapi.SettingUpdate + trust.AutoController, Data: []byte("{"), Updated: testNow}); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.U.Setting(ctx, trust.AutoController); err == nil {
		t.Fatalf("a damaged setting read as a value")
	}
	// A row that names ANOTHER target is the same case: it is not read as the
	// one that was asked for.
	b, _ := json.Marshal(core.UpdateSetting{Target: trust.AutoWeb, Mode: store.ModeNotify})
	if err := fx.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting,
		ID: dbapi.SettingUpdate + trust.AutoController, Data: b, Updated: testNow}); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.U.Setting(ctx, trust.AutoController); err == nil {
		t.Fatalf("a setting of another target was read as this one")
	}
}

func TestSetSettingTakesOnlyWhatTheContractTakes(t *testing.T) {
	fx := newFixture(t)
	bad := []core.UpdateSetting{
		{Target: trust.AutoController, Mode: "sometimes"},
		{Target: "../etc/passwd", Mode: store.ModeSemi},
		{Target: trust.AutoController, Mode: store.ModeSemi, Window: "24:00-02:00"},
		{Target: trust.AutoController, Mode: store.ModeSemi, Window: "2:00-4:00"},
	}
	for _, s := range bad {
		if err := fx.U.SetSetting(ctx, s); err == nil {
			t.Fatalf("taken: %+v", s)
		}
	}
	if got := fx.settings(t); len(got) != 0 {
		t.Fatalf("a refused setting was written: %v", got)
	}
}

// TestAutoNeedsAStandingApproval: "auto" means "install without asking
// anybody", and the trust list is the only place this installation says that
// is allowed. trusttest.Genesis approves ngc-controller and system and nothing
// else, so ngc-api and ngc-web are the measured negative.
func TestAutoNeedsAStandingApproval(t *testing.T) {
	fx := newFixture(t)
	for _, target := range []string{trust.AutoController, trust.AutoSystem} {
		if err := fx.U.SetSetting(ctx, core.UpdateSetting{Target: target, Mode: store.ModeAuto}); err != nil {
			t.Fatalf("%s: %v", target, err)
		}
	}
	for _, target := range []string{trust.AutoAPI, trust.AutoWeb} {
		if err := fx.U.SetSetting(ctx, core.UpdateSetting{Target: target,
			Mode: store.ModeAuto}); err == nil {
			t.Fatalf("%s took auto without a standing approval", target)
		}
		// But semi and notify need none: they install nothing unasked.
		if err := fx.U.SetSetting(ctx, core.UpdateSetting{Target: target,
			Mode: store.ModeSemi}); err != nil {
			t.Fatalf("%s semi: %v", target, err)
		}
	}
	// The way is open: once the list approves ngc-api, auto is taken.
	fx.approve(t, trust.AutoAPI)
	if err := fx.U.SetSetting(ctx, core.UpdateSetting{Target: trust.AutoAPI,
		Mode: store.ModeAuto}); err != nil {
		t.Fatalf("control: %v", err)
	}
}

// TestAutoForAnAppNeedsTheApprovalOfItsOwnStoreKey: the key a standing
// approval names has to be the key of the store the app came from, not just
// any key in the list.
func TestAutoForAnAppNeedsTheApprovalOfItsOwnStoreKey(t *testing.T) {
	fx := newFixture(t)
	fx.putApp(t, theApp, "1.0.0", store.GlobalID, ctlNode, nil)
	// An approval for the app that names the RELEASE key, not the store key.
	fx.approve(t, theApp)
	if err := fx.U.SetSetting(ctx, core.UpdateSetting{Target: theApp, Mode: store.ModeAuto}); err == nil {
		t.Fatalf("auto taken although the approval names another key")
	}
	// The way is open: an approval with the key of the app's own store is
	// taken.
	fx.setAuto(t, theApp, storePub())
	if err := fx.U.SetSetting(ctx, core.UpdateSetting{Target: theApp, Mode: store.ModeAuto}); err != nil {
		t.Fatalf("control: %v", err)
	}
	// An app this installation does not know has no key and is refused.
	if err := fx.U.SetSetting(ctx, core.UpdateSetting{Target: otherApp,
		Mode: store.ModeAuto}); err == nil || !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("unknown app: %v", err)
	}
}

// TestReportTakesOnlyWhatTheContractTakes: a report with a target the contract
// does not know changes nothing. store.ParseUpdateReport runs before this
// method in operation; this is the second floor.
func TestReportTakesOnlyWhatTheContractTakes(t *testing.T) {
	fx := newFixture(t)
	fx.theFleet()
	fx.U.Report(ctx, ctlNode, store.UpdateReport{Target: "../etc", State: store.UpdateDownloaded})
	fx.U.Report(ctx, "", store.UpdateReport{Target: trust.AutoAPI, State: store.UpdateDownloaded})
	if _, ok := fx.U.report(ctlNode, "../etc"); ok {
		t.Fatalf("an invalid target was kept")
	}
	if _, ok := fx.U.report("", trust.AutoAPI); ok {
		t.Fatalf("a report without a node was kept")
	}
	// The way is open: a report the contract takes is kept.
	fx.U.Report(ctx, ctlNode, store.UpdateReport{Target: trust.AutoAPI, State: store.UpdateFailed})
	if r, ok := fx.U.report(ctlNode, trust.AutoAPI); !ok || r.State != store.UpdateFailed {
		t.Fatalf("control: %+v %v", r, ok)
	}
}

// TestAnAppWithOnlyKeptDataHasNoRow: after an uninstall with KeepData the row
// of the app stays, with an empty version. There is nothing installed to
// update, so there is nothing to offer.
func TestAnAppWithOnlyKeptDataHasNoRow(t *testing.T) {
	fx := newFixture(t)
	fx.Fleet.put(ctlNode, "amd64", "1.0.0", []string{tunnel.RoleServices}, nil)
	fx.listCatalog(t, storePriv, 9, entry(theApp, "1.2.0", nil, ""))
	fx.putApp(t, theApp, "", store.GlobalID, ctlNode, nil)
	rows, err := fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if _, ok := findRow(rows, theApp, ctlNode); ok {
		t.Fatalf("an app with only kept data got a row")
	}
	// The way is open: with a version installed the same app does get one.
	fx.putApp(t, theApp, "1.0.0", store.GlobalID, ctlNode, nil)
	rows, err = fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if _, ok := findRow(rows, theApp, ctlNode); !ok {
		t.Fatalf("control: no row for an installed app")
	}
}
