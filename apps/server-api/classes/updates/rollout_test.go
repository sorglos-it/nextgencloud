package updates

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// theBodies are the three release files these cases use.
func theBodies() map[string][]byte {
	return map[string][]byte{
		partFile:  []byte("the api of 1.1.0"),
		partFile2: []byte("the controller of 1.1.0"),
		partFile3: []byte("the arm64 controller of 1.1.0"),
		webFile:   []byte("the web interface of 1.1.0"),
	}
}

// readyToRoll is the whole arrangement one round needs: three controllers
// online on 1.0.0, a signed release 1.1.0 with a file per part, standing
// approvals for all three parts, a short wait and a fixed zone.
//
// It also hangs the controller of operation into the fake sender: an install
// that was accepted reports store.UpdateInstalled, the way a controller does
// on its control channel. Without that every node would cost the full wait,
// and the order below would be measured against a clock instead of against
// the reports.
func readyToRoll(t *testing.T) fixture {
	t.Helper()
	fx := newFixture(t)
	fx.theFleet()
	fx.serveRelease(t, relPriv, 3, "1.1.0", theBodies())
	fx.approve(t, trust.AutoAPI, trust.AutoController, trust.AutoWeb)
	fx.U.Wait = 2 * time.Second
	fx.U.Zone = time.UTC
	fx.Send.onSend = reportsLikeAController(&fx, func(string, string) string { return "" })
	return fx
}

// reportsLikeAController is the controller of operation as far as the reports
// go: a stage that was accepted reports store.UpdateDownloaded and an install
// store.UpdateInstalled, which is what apps/server-controller/classes/update
// sends on its control channel. override names a different state for one
// target on one node, for the cases that make a node fail.
func reportsLikeAController(fx *fixture, override func(node, target string) string) func(sent) {
	return func(s sent) {
		var target, version, st string
		switch s.action {
		case core.ActStage:
			var p core.StageParams
			if json.Unmarshal(s.params, &p) != nil {
				return
			}
			target, version, st = p.Target, p.Version, store.UpdateDownloaded
		case core.ActInstall:
			var p core.InstallParams
			if json.Unmarshal(s.params, &p) != nil {
				return
			}
			target, version, st = p.Target, p.Version, store.UpdateInstalled
		default:
			return
		}
		if o := override(s.node, target); o != "" {
			st = o
		}
		fx.U.Report(ctx, s.node, store.UpdateReport{Target: target, Version: version, State: st})
	}
}

// steps is the sequence of what left the API, as "action target node".
func steps(fx fixture, t *testing.T) []string {
	t.Helper()
	var out []string
	for _, s := range fx.Send.list() {
		target := ""
		switch s.action {
		case core.ActStage:
			target = stageParams(t, s).Target
		case core.ActInstall:
			target = installParams(t, s).Target
		default:
			t.Fatalf("an action nothing in this package sends: %q", s.action)
		}
		out = append(out, s.action+" "+target+" "+s.node)
	}
	return out
}

func TestRolloutTakesTheLock(t *testing.T) {
	fx := readyToRoll(t)
	// A second API on the same database, with its own node ID: it is the one
	// that must do nothing.
	other := &Updates{S: fx.S, Stores: fx.St, Tunnels: fx.Fleet, Send: fx.Send,
		Spool: t.TempDir(), Wait: time.Millisecond, Zone: time.UTC}
	// The lock is held by somebody else while this one asks.
	ok, err := fx.S.DB.LockTake(ctx, dbapi.LockUpdate, "api-2", testNow, testNow+600)
	if err != nil || !ok {
		t.Fatalf("LockTake: %v %v", ok, err)
	}
	if err := other.Rollout(ctx); !errors.Is(err, ErrNotDriving) {
		t.Fatalf("Rollout: %v, want ErrNotDriving", err)
	}
	if sents := fx.Send.list(); len(sents) != 0 {
		t.Fatalf("the API that does not drive sent something: %v", steps(fx, t))
	}
	// The way is open: with the lock free the same round does drive.
	if err := fx.S.DB.LockFree(ctx, dbapi.LockUpdate, "api-2"); err != nil {
		t.Fatal(err)
	}
	if err := fx.U.Rollout(ctx); err != nil {
		t.Fatalf("control: %v", err)
	}
	if len(fx.Send.list()) == 0 {
		t.Fatalf("control: nothing travelled")
	}
	// And the lock is given back, so the next round is not locked out by the
	// one that just finished.
	ok, err = fx.S.DB.LockTake(ctx, dbapi.LockUpdate, "api-3", testNow, testNow+600)
	if err != nil || !ok {
		t.Fatalf("the lock was not given back: %v %v", ok, err)
	}
}

func TestRolloutOrder(t *testing.T) {
	fx := readyToRoll(t)
	if err := fx.U.Rollout(ctx); err != nil {
		t.Fatalf("Rollout: %v", err)
	}
	got := steps(fx, t)
	// The database first (the ngc-api file laid ready on the db server, which
	// is no API itself and therefore gets nothing installed), then the APIs,
	// then the controllers, the web interface last. Per node: stage, then
	// install -- and the next node only after the previous one reported
	// installed, which is what the exact sequence says.
	want := []string{
		"stage " + trust.AutoAPI + " " + dbNode,
		"stage " + trust.AutoAPI + " " + ctlNode,
		"install " + trust.AutoAPI + " " + ctlNode,
		"stage " + trust.AutoController + " " + ctlNode,
		"install " + trust.AutoController + " " + ctlNode,
		"stage " + trust.AutoController + " " + dbNode,
		"install " + trust.AutoController + " " + dbNode,
		"stage " + trust.AutoController + " " + webNode,
		"install " + trust.AutoController + " " + webNode,
		"stage " + trust.AutoWeb + " " + webNode,
		"install " + trust.AutoWeb + " " + webNode,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("order:\n got %v\nwant %v", got, want)
	}
	// The db server is not an API and must never be handed an install of
	// ngc-api: the contract has no action that plays migrations in, and that
	// install would restart a service this machine does not run.
	if slices.Contains(got, "install "+trust.AutoAPI+" "+dbNode) {
		t.Fatalf("the database server was told to install the API")
	}
}

func TestMigrationNodeIsTheSmallestDBNode(t *testing.T) {
	fx := readyToRoll(t)
	if n, ok := fx.U.MigrationNode(); !ok || n != dbNode {
		t.Fatalf("MigrationNode %q %v, want %q", n, ok, dbNode)
	}
	// A second database server with a SMALLER ID takes over. ctl-1 < ctl-db.
	fx.Fleet.put(ctlNode, "amd64", "1.0.0", []string{tunnel.RoleDB},
		map[string]string{trust.AutoController: "1.0.0"})
	if n, ok := fx.U.MigrationNode(); !ok || n != ctlNode {
		t.Fatalf("MigrationNode %q %v, want %q", n, ok, ctlNode)
	}
	// If it is away, the next one.
	fx.Fleet.drop(ctlNode)
	if n, ok := fx.U.MigrationNode(); !ok || n != dbNode {
		t.Fatalf("MigrationNode %q %v, want %q", n, ok, dbNode)
	}
	// A node without a tunnel is no candidate at all: the file has to reach
	// it now.
	fx.Fleet.drop(dbNode)
	if n, ok := fx.U.MigrationNode(); ok {
		t.Fatalf("MigrationNode %q with no db server connected", n)
	}
	// And a connected server without the role db is none either.
	fx.Fleet.put(ctlNode, "amd64", "1.0.0", []string{tunnel.RoleAPI}, nil)
	if n, ok := fx.U.MigrationNode(); ok {
		t.Fatalf("MigrationNode %q for a server without the role db", n)
	}
}

func TestRolloutStagesThenInstalls(t *testing.T) {
	fx := readyToRoll(t)
	if err := fx.U.Rollout(ctx); err != nil {
		t.Fatalf("Rollout: %v", err)
	}
	bodies := theBodies()
	// Every stage: a transfer, the signed release in the header, the size and
	// the checksum of the entry of THIS part and THIS architecture, the file
	// byte for byte, and no user.
	for _, s := range fx.Send.only(core.ActStage) {
		p := stageParams(t, s)
		if !s.transfer {
			t.Fatalf("stage of %s on %s was no transfer", p.Target, s.node)
		}
		if s.hadUser {
			t.Fatalf("stage of %s on %s carried a user", p.Target, s.node)
		}
		if p.Version != "1.1.0" {
			t.Fatalf("stage names version %q, not the signed one", p.Version)
		}
		if s.head.Release == nil || s.head.Catalog != nil || s.head.Store != "" ||
			s.head.License != nil || len(s.head.Key) != 0 {
			t.Fatalf("stage header of %s is not a plain release header: %+v", p.Target, s.head)
		}
		if err := s.head.Validate(); err != nil {
			t.Fatalf("stage header does not validate: %v", err)
		}
		name := fileNameOf(t, p.Target, s.node, fx)
		body := bodies[name]
		sum := sha256.Sum256(body)
		if s.head.Size != int64(len(body)) || !bytes.Equal(s.head.SHA256, sum[:]) {
			t.Fatalf("stage header of %s on %s names size %d sum %x, want %d %x",
				p.Target, s.node, s.head.Size, []byte(s.head.SHA256), len(body), sum[:])
		}
		if !bytes.Equal(s.file, body) {
			t.Fatalf("stage of %s on %s sent %q, want %q", p.Target, s.node, s.file, body)
		}
		// The release in the header has to be the one this API verified: the
		// controller holds the whole file against it, so a header carrying
		// another release would decide which bytes are accepted.
		if _, err := store.VerifyRelease(*s.head.Release, storePub(), 0); err == nil {
			t.Fatalf("the staged release verifies under the STORE key")
		}
		rel, err := store.VerifyRelease(*s.head.Release, relPub(), 0)
		if err != nil {
			t.Fatalf("the staged release does not verify under the release key: %v", err)
		}
		if rel.Version != p.Version {
			t.Fatalf("the signed release says %q, the params say %q", rel.Version, p.Version)
		}
	}
	// Every install: a call, no user, the version and the checksum of the
	// same signed entry -- never anything else.
	for _, s := range fx.Send.only(core.ActInstall) {
		p := installParams(t, s)
		if s.transfer || s.hadUser {
			t.Fatalf("install of %s on %s: transfer %v user %v", p.Target, s.node, s.transfer, s.hadUser)
		}
		if p.Version != "1.1.0" {
			t.Fatalf("install names version %q", p.Version)
		}
		body := bodies[fileNameOf(t, p.Target, s.node, fx)]
		sum := sha256.Sum256(body)
		if !bytes.Equal(p.SHA256, sum[:]) {
			t.Fatalf("install of %s on %s names sum %x, want %x", p.Target, s.node,
				[]byte(p.SHA256), sum[:])
		}
	}
	// Nothing is left in the spool: a directory nobody empties fills the disk
	// of the one machine that then cannot update itself any more.
	if left := spoolFiles(t, fx.U.Spool); len(left) != 0 {
		t.Fatalf("files left in the spool: %v", left)
	}
}

// fileNameOf is the release file one part needs on one node.
func fileNameOf(t *testing.T, part, node string, fx fixture) string {
	t.Helper()
	h, _, ok := fx.Fleet.Seen(node)
	if !ok {
		t.Fatalf("%s is not online", node)
	}
	switch {
	case part == trust.AutoWeb:
		return webFile
	case part == trust.AutoAPI && h.Arch == "amd64":
		return partFile
	case part == trust.AutoController && h.Arch == "amd64":
		return partFile2
	case part == trust.AutoController && h.Arch == "arm64":
		return partFile3
	}
	t.Fatalf("no file of this test for %s on %s (arch %q)", part, node, h.Arch)
	return ""
}

func spoolFiles(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("spool: %v", err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// TestWhatTheModeStops: notify tells only, semi lays ready and waits for a
// click, auto installs. The mode is the whole promise of the setting, so all
// three are measured, not just the one that acts.
func TestWhatTheModeStops(t *testing.T) {
	cases := []struct {
		mode             string
		wantStage        int
		wantInstall      int
		needsApprovalFor bool
	}{
		{store.ModeNotify, 0, 0, false},
		{store.ModeSemi, 1, 0, false},
		{store.ModeAuto, 1, 1, true},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			fx := readyToRoll(t)
			// One controller only, so the counts are per target and not per
			// fleet.
			fx.Fleet.drop(ctlNode)
			fx.Fleet.drop(webNode)
			if err := fx.U.SetSetting(ctx, core.UpdateSetting{Target: trust.AutoController,
				Mode: c.mode}); err != nil {
				t.Fatalf("SetSetting: %v", err)
			}
			if err := fx.U.Rollout(ctx); err != nil {
				t.Fatalf("Rollout: %v", err)
			}
			if n := countFor(fx, t, core.ActStage, trust.AutoController); n != c.wantStage {
				t.Fatalf("%s: %d stages, want %d (%v)", c.mode, n, c.wantStage, steps(fx, t))
			}
			if n := countFor(fx, t, core.ActInstall, trust.AutoController); n != c.wantInstall {
				t.Fatalf("%s: %d installs, want %d (%v)", c.mode, n, c.wantInstall, steps(fx, t))
			}
		})
	}
}

// TestTheWindowHoldsBothEnds nails the maintenance window down on both sides:
// inside it the update installs, one minute outside it only lies ready. A case
// that measured the outside alone would stay green with a window of all day.
func TestTheWindowHoldsBothEnds(t *testing.T) {
	// testNow is 14:13:20 UTC (see the constant).
	cases := []struct {
		name, window string
		install      bool
	}{
		{"inside", "14:00-15:00", true},
		{"exactly the first minute", "14:13-14:13", true},
		{"one minute after the end", "13:00-14:12", false},
		{"one minute before the start", "14:14-15:00", false},
		{"over midnight, inside", "22:00-15:00", true},
		{"over midnight, outside", "22:00-14:12", false},
		{"no window at all", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fx := readyToRoll(t)
			fx.Fleet.drop(ctlNode)
			fx.Fleet.drop(webNode)
			if err := fx.U.SetSetting(ctx, core.UpdateSetting{Target: trust.AutoController,
				Mode: store.ModeAuto, Window: c.window}); err != nil {
				t.Fatalf("SetSetting: %v", err)
			}
			if err := fx.U.Rollout(ctx); err != nil {
				t.Fatalf("Rollout: %v", err)
			}
			// The file lies ready in every case: that is what the window is
			// for, so the install inside it has nothing left to fetch.
			if n := countFor(fx, t, core.ActStage, trust.AutoController); n != 1 {
				t.Fatalf("%d stages of the controller, want 1 (%v)", n, steps(fx, t))
			}
			got := countFor(fx, t, core.ActInstall, trust.AutoController) == 1
			if got != c.install {
				t.Fatalf("window %q at 14:13 UTC: installed %v, want %v", c.window, got, c.install)
			}
		})
	}
}

// TestWithoutAStandingApprovalNothingInstalls: the controller would refuse an
// install without a user anyway (core.Unattended), so this side does not even
// ask -- but the file still lies ready, so one entry in the trust list is all
// that is missing afterwards.
func TestWithoutAStandingApprovalNothingInstalls(t *testing.T) {
	fx := newFixture(t)
	fx.theFleet()
	fx.serveRelease(t, relPriv, 3, "1.1.0", theBodies())
	fx.U.Wait = time.Millisecond
	fx.U.Zone = time.UTC
	// trusttest.Genesis approves ngc-controller and system and nothing else.
	if err := fx.U.Rollout(ctx); err != nil {
		t.Fatalf("Rollout: %v", err)
	}
	for _, s := range fx.Send.only(core.ActInstall) {
		if p := installParams(t, s); p.Target != trust.AutoController {
			t.Fatalf("%s was installed without a standing approval", p.Target)
		}
	}
	// And the files of the parts nobody approved did travel, so the refusal
	// above is the approval and not a release this API could not read.
	var staged []string
	for _, s := range fx.Send.only(core.ActStage) {
		staged = append(staged, stageParams(t, s).Target)
	}
	if !slices.Contains(staged, trust.AutoAPI) || !slices.Contains(staged, trust.AutoWeb) {
		t.Fatalf("nothing was laid ready for the unapproved parts: %v", staged)
	}
}

// TestARolloutNeverGoesBackwards: a release older than what runs on a node is
// not laid ready and not installed. The node, not the round, decides what is
// older -- the version comes out of its own status.
func TestARolloutNeverGoesBackwards(t *testing.T) {
	fx := readyToRoll(t)
	fx.Fleet.drop(webNode)
	// ctl-1 already runs 1.2.0 of the controller; ctl-db runs 1.0.0.
	fx.Fleet.put(ctlNode, "amd64", "1.2.0", []string{tunnel.RoleServices},
		map[string]string{trust.AutoController: "1.2.0"})
	if err := fx.U.Rollout(ctx); err != nil {
		t.Fatalf("Rollout: %v", err)
	}
	for _, s := range fx.Send.list() {
		if s.node == ctlNode {
			t.Fatalf("something travelled to the node that is already newer: %v", steps(fx, t))
		}
	}
	// The way is open: the node that is older did get the update.
	if n := len(fx.Send.only(core.ActInstall)); n == 0 {
		t.Fatalf("control: the older node got nothing: %v", steps(fx, t))
	}
	// The same version again is not an update either.
	fx.Send.sents = nil
	fx.Fleet.put(dbNode, "amd64", "1.1.0", []string{tunnel.RoleDB},
		map[string]string{trust.AutoController: "1.1.0"})
	if err := fx.U.Rollout(ctx); err != nil {
		t.Fatalf("Rollout: %v", err)
	}
	if n := len(fx.Send.list()); n != 0 {
		t.Fatalf("the same version travelled again: %v", steps(fx, t))
	}
}

// TestAReleaseOfAStrangerReachesNoNode and the rollback case below are the two
// attacks this round has to survive. Each one is measured twice: once that
// nothing travelled, and once that the SAME round with the sound release does
// travel -- otherwise a round that refuses everything would pass.
func TestAReleaseOfAStrangerReachesNoNode(t *testing.T) {
	fx := readyToRoll(t)
	fx.serveRelease(t, alienPriv, 9, "9.9.9", theBodies())
	err := fx.U.Rollout(ctx)
	if err == nil {
		t.Fatalf("a release of a stranger drove a round")
	}
	if len(fx.Send.list()) != 0 {
		t.Fatalf("something travelled: %v", steps(fx, t))
	}
	// The lock is free again, so the next round is not shut out by the
	// refused one.
	ok, lerr := fx.S.DB.LockTake(ctx, dbapi.LockUpdate, "api-9", testNow, testNow+60)
	if lerr != nil || !ok {
		t.Fatalf("the lock was not given back: %v %v", ok, lerr)
	}
	if err := fx.S.DB.LockFree(ctx, dbapi.LockUpdate, "api-9"); err != nil {
		t.Fatal(err)
	}
	// The way is open.
	fx.serveRelease(t, relPriv, 9, "9.9.9", theBodies())
	if err := fx.U.Rollout(ctx); err != nil {
		t.Fatalf("control: %v", err)
	}
	if len(fx.Send.list()) == 0 {
		t.Fatalf("control: nothing travelled")
	}
}

func TestAReleaseTurnedBackReachesNoNode(t *testing.T) {
	fx := readyToRoll(t)
	fx.serveRelease(t, relPriv, 9, "1.1.0", theBodies())
	if err := fx.U.Rollout(ctx); err != nil {
		t.Fatalf("Rollout: %v", err)
	}
	if len(fx.Send.list()) == 0 {
		t.Fatalf("the first round sent nothing, so nothing below is measured")
	}
	fx.Send.sents = nil
	// Now an older, correctly signed release of a version that WOULD look
	// newer than what the nodes run.
	fx.serveRelease(t, relPriv, 8, "1.0.9", theBodies())
	fx.Fleet.put(ctlNode, "amd64", "1.0.0", []string{tunnel.RoleAPI, tunnel.RoleServices},
		map[string]string{trust.AutoAPI: "1.0.0", trust.AutoController: "1.0.0"})
	if err := fx.U.Rollout(ctx); err == nil {
		t.Fatalf("a release turned back drove a round")
	}
	if len(fx.Send.list()) != 0 {
		t.Fatalf("something travelled for a release turned back: %v", steps(fx, t))
	}
}

// TestARoundThatStopsLeavesNoClaimThatItFinished is the half-run update: a
// node reports that it rolled the update back, and the round stops there.
//
// What has to hold afterwards, and all of it is measured, not argued:
//   - the nodes BEHIND the one that failed were not touched,
//   - the ones BEFORE it keep what they installed -- each is a whole program
//     on its own machine, and the contract is built for a mixed installation,
//   - nothing anywhere says the round finished, because nothing of a round is
//     written down at all: the rows are computed from what the nodes report,
//   - the log carries the one entry of the stopped round,
//   - the lock is free, so the next round can pick the rest up.
func TestARoundThatStopsLeavesNoClaimThatItFinished(t *testing.T) {
	fx := readyToRoll(t)
	// The controller of ctl-1 rolls its update back. Everything before it
	// (the ngc-api of ctl-1) is already installed at that point.
	fx.Send.onSend = reportsLikeAController(&fx, func(node, target string) string {
		if node == ctlNode && target == trust.AutoController {
			return store.UpdateRolledBack
		}
		return ""
	})
	err := fx.U.Rollout(ctx)
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("Rollout: %v, want ErrAborted", err)
	}
	got := steps(fx, t)
	want := []string{
		"stage " + trust.AutoAPI + " " + dbNode,
		"stage " + trust.AutoAPI + " " + ctlNode,
		"install " + trust.AutoAPI + " " + ctlNode,
		"stage " + trust.AutoController + " " + ctlNode,
		"install " + trust.AutoController + " " + ctlNode,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the round went on past the failure:\n got %v\nwant %v", got, want)
	}
	// Nothing of the round is stored, so nothing can claim it finished.
	if s := fx.settings(t); len(s) != 0 {
		t.Fatalf("the round wrote settings: %v", s)
	}
	// The rows say what they always say: what the nodes reported.
	rows, rerr := fx.U.Rows(ctx)
	if rerr != nil {
		t.Fatalf("Rows: %v", rerr)
	}
	if r := rowOf(t, rows, trust.AutoAPI, ctlNode); r.State != store.UpdateInstalled {
		t.Fatalf("the part installed before the failure: state %q", r.State)
	}
	if r := rowOf(t, rows, trust.AutoController, ctlNode); r.State != store.UpdateRolledBack {
		t.Fatalf("the part that failed: state %q", r.State)
	}
	// The nodes behind it reported nothing, because nothing was sent to them.
	if r := rowOf(t, rows, trust.AutoWeb, webNode); r.State != "" {
		t.Fatalf("a node behind the failure has a state: %q", r.State)
	}
	// The one log entry of the stopped round.
	entries, lerr := fx.U.Log.Query(ctx, core.LogQuery{Limit: 50})
	if lerr != nil {
		t.Fatalf("log: %v", lerr)
	}
	var ups []core.LogEntry
	for _, e := range entries {
		if e.Kind == core.LogUpdate {
			ups = append(ups, e)
		}
	}
	if len(ups) != 1 {
		t.Fatalf("%d update log entries, want 1: %+v", len(ups), ups)
	}
	if d := ups[0].Detail; !bytes.Contains([]byte(d), []byte(store.UpdateRolledBack)) ||
		!bytes.Contains([]byte(d), []byte(ctlNode)) {
		t.Fatalf("the entry does not name node and cause: %q", d)
	}
	// The lock is free again.
	ok, lkerr := fx.S.DB.LockTake(ctx, dbapi.LockUpdate, "api-9", testNow, testNow+60)
	if lkerr != nil || !ok {
		t.Fatalf("the lock was not given back: %v %v", ok, lkerr)
	}
	if err := fx.S.DB.LockFree(ctx, dbapi.LockUpdate, "api-9"); err != nil {
		t.Fatal(err)
	}
	// And a repeat picks the rest up: with the fault gone, the round that
	// follows reaches the nodes the first one never got to.
	fx.Send.sents = nil
	fx.Fleet.put(ctlNode, "amd64", "1.1.0", []string{tunnel.RoleAPI, tunnel.RoleServices},
		map[string]string{trust.AutoAPI: "1.1.0", trust.AutoController: "1.1.0"})
	fx.Send.onSend = reportsLikeAController(&fx, func(string, string) string { return "" })
	if err := fx.U.Rollout(ctx); err != nil {
		t.Fatalf("the repeat: %v", err)
	}
	if !slices.Contains(steps(fx, t), "install "+trust.AutoWeb+" "+webNode) {
		t.Fatalf("the repeat did not reach the rest: %v", steps(fx, t))
	}
}

// TestEveryStateThatStopsARound enumerates the states instead of guessing how
// many there are: four of the eight of store stop a round, four do not.
func TestEveryStateThatStopsARound(t *testing.T) {
	stops := map[string]bool{
		store.UpdateInstalled:   false,
		store.UpdateAvailable:   false,
		store.UpdateDownloaded:  false,
		store.UpdateInstalling:  false,
		store.UpdateRolledBack:  true,
		store.UpdateFailed:      true,
		store.UpdateRejected:    true,
		store.UpdateNeedsGrants: true,
	}
	for st, wantStop := range stops {
		t.Run(st, func(t *testing.T) {
			fx := readyToRoll(t)
			fx.Fleet.drop(ctlNode)
			fx.Fleet.drop(webNode)
			// Only ctl-db is left, so exactly one install happens.
			fx.U.Wait = 50 * time.Millisecond
			// Only the install reports the state under measurement; the
			// stage reports normally, or a round would stop at the stage
			// and the case would measure the wrong step.
			fx.Send.onSend = func(se sent) {
				if se.action == core.ActStage {
					p := stageParams(t, se)
					fx.U.Report(ctx, se.node, store.UpdateReport{Target: p.Target,
						Version: p.Version, State: store.UpdateDownloaded})
					return
				}
				p := installParams(t, se)
				fx.U.Report(ctx, se.node, store.UpdateReport{Target: p.Target,
					Version: p.Version, State: st})
			}
			err := fx.U.Rollout(ctx)
			if gotStop := errors.Is(err, ErrAborted); gotStop != wantStop {
				t.Fatalf("state %q: aborted %v (%v), want %v", st, gotStop, err, wantStop)
			}
		})
	}
}

// TestANodeThatSaysNothingDoesNotStopTheRound: a controller that installs
// itself takes its own tunnel down with it, so silence is a report this side
// never saw and not a failure. The round goes on after the wait, and the row
// of that node never reads "installed" -- only the node itself may say that.
func TestANodeThatSaysNothingDoesNotStopTheRound(t *testing.T) {
	fx := readyToRoll(t)
	fx.Fleet.drop(webNode)
	fx.U.Wait = 30 * time.Millisecond
	fx.Send.onSend = nil // nobody reports anything
	if err := fx.U.Rollout(ctx); err != nil {
		t.Fatalf("Rollout: %v", err)
	}
	got := steps(fx, t)
	if !slices.Contains(got, "install "+trust.AutoController+" "+dbNode) {
		t.Fatalf("the round did not reach the second node: %v", got)
	}
	rows, err := fx.U.Rows(ctx)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	for _, r := range rows {
		if r.State == store.UpdateInstalled {
			t.Fatalf("a row claims installed although no node said so: %+v", r)
		}
	}
}

// TestAnAbortedContextStopsTheRound: the round is not a transaction, and a
// context that ends must not be answered as a finished round.
func TestAnAbortedContextStopsTheRound(t *testing.T) {
	fx := readyToRoll(t)
	cctx, cancel := context.WithCancel(ctx)
	fx.Send.onSend = func(s sent) {
		if s.action == core.ActInstall {
			cancel()
		}
	}
	fx.U.Wait = 5 * time.Second
	if err := fx.U.Rollout(cctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Rollout: %v, want context.Canceled", err)
	}
	// And it stopped at the node it was at, not after the whole fleet.
	if slices.Contains(steps(fx, t), "install "+trust.AutoWeb+" "+webNode) {
		t.Fatalf("the round ran on after the context ended: %v", steps(fx, t))
	}
}

// TestAControllerThatRefusesStopsTheRound: the refusal of the channel itself,
// as opposed to a report. Both halves matter, because a stage that fails must
// not be followed by an install of the same part.
func TestAControllerThatRefusesStopsTheRound(t *testing.T) {
	for _, action := range []string{core.ActStage, core.ActInstall} {
		t.Run(action, func(t *testing.T) {
			fx := readyToRoll(t)
			fx.Send.answer(action, wire.CodeVerify)
			if err := fx.U.Rollout(ctx); !errors.Is(err, ErrAborted) {
				t.Fatalf("Rollout: %v, want ErrAborted", err)
			}
			if action == core.ActStage && len(fx.Send.only(core.ActInstall)) != 0 {
				t.Fatalf("an install followed a stage that was refused: %v", steps(fx, t))
			}
			if left := spoolFiles(t, fx.U.Spool); len(left) != 0 {
				t.Fatalf("files left in the spool after a refusal: %v", left)
			}
		})
	}
}

// TestNothingTravelsToANodeWithoutATunnel: a node in the trust list that is
// not connected gets nothing, and the round does not fail over it.
func TestNothingTravelsToANodeWithoutATunnel(t *testing.T) {
	fx := readyToRoll(t)
	fx.Fleet.drop(webNode)
	if err := fx.U.Rollout(ctx); err != nil {
		t.Fatalf("Rollout: %v", err)
	}
	for _, s := range fx.Send.list() {
		if s.node == webNode {
			t.Fatalf("something travelled to a node without a tunnel: %v", steps(fx, t))
		}
	}
}

// TestRolloutWithoutASenderDoesNothing: Send is nil until the server wires it
// (task 24), and a round must then refuse rather than look finished.
func TestRolloutWithoutASenderDoesNothing(t *testing.T) {
	fx := readyToRoll(t)
	fx.U.Send = nil
	if err := fx.U.Rollout(ctx); !errors.Is(err, ErrAborted) {
		t.Fatalf("Rollout: %v, want ErrAborted", err)
	}
	if len(fx.Send.list()) != 0 {
		t.Fatalf("something travelled: %v", steps(fx, t))
	}
}

// countFor is how many calls of one action named one target.
func countFor(fx fixture, t *testing.T, action, target string) int {
	t.Helper()
	n := 0
	for _, s := range fx.Send.only(action) {
		switch action {
		case core.ActStage:
			if stageParams(t, s).Target == target {
				n++
			}
		case core.ActInstall:
			if installParams(t, s).Target == target {
				n++
			}
		}
	}
	return n
}

// TestAFileThatLiesReadyIsNotSentAgain, and the other half: a state that is
// NO file on the disk makes the next round lay it ready again. Without the
// second half a node that reported "rejected" would never be offered the file
// again, and nothing but a new release would mend it.
func TestAFileThatLiesReadyIsNotSentAgain(t *testing.T) {
	cases := []struct {
		state     string
		wantStage int
	}{
		{store.UpdateDownloaded, 0},
		{store.UpdateInstalling, 0},
		{store.UpdateInstalled, 0},
		{store.UpdateAvailable, 1},
		{store.UpdateRejected, 1},
		{store.UpdateFailed, 1},
		{store.UpdateRolledBack, 1},
	}
	for _, c := range cases {
		t.Run(c.state, func(t *testing.T) {
			fx := readyToRoll(t)
			fx.Fleet.drop(ctlNode)
			fx.Fleet.drop(webNode)
			// Only "notify" for the controller, so this case measures the
			// stage alone and no install follows to change the state again.
			if err := fx.U.SetSetting(ctx, core.UpdateSetting{Target: trust.AutoAPI,
				Mode: store.ModeNotify}); err != nil {
				t.Fatalf("SetSetting: %v", err)
			}
			if err := fx.U.SetSetting(ctx, core.UpdateSetting{Target: trust.AutoController,
				Mode: store.ModeNotify}); err != nil {
				t.Fatalf("SetSetting: %v", err)
			}
			// Notify sends nothing, so the state is set by hand and then the
			// setting is put back to semi for the round itself.
			fx.U.Report(ctx, dbNode, store.UpdateReport{Target: trust.AutoController,
				Version: "1.1.0", State: c.state})
			if err := fx.U.SetSetting(ctx, core.UpdateSetting{Target: trust.AutoController,
				Mode: store.ModeSemi}); err != nil {
				t.Fatalf("SetSetting: %v", err)
			}
			if err := fx.U.Rollout(ctx); err != nil {
				t.Fatalf("Rollout: %v", err)
			}
			if n := countFor(fx, t, core.ActStage, trust.AutoController); n != c.wantStage {
				t.Fatalf("state %q: %d stages, want %d (%v)", c.state, n, c.wantStage, steps(fx, t))
			}
		})
	}
}

// TestAFileThatLiesReadyIsStillInstalled: the round after the maintenance
// window opened must install what the round before laid ready, WITHOUT
// fetching and sending it a second time.
func TestAFileThatLiesReadyIsStillInstalled(t *testing.T) {
	fx := readyToRoll(t)
	fx.Fleet.drop(ctlNode)
	fx.Fleet.drop(webNode)
	fx.U.Report(ctx, dbNode, store.UpdateReport{Target: trust.AutoController,
		Version: "1.1.0", State: store.UpdateDownloaded})
	if err := fx.U.Rollout(ctx); err != nil {
		t.Fatalf("Rollout: %v", err)
	}
	if n := countFor(fx, t, core.ActStage, trust.AutoController); n != 0 {
		t.Fatalf("the file was sent again: %v", steps(fx, t))
	}
	if n := countFor(fx, t, core.ActInstall, trust.AutoController); n != 1 {
		t.Fatalf("what lies ready was not installed: %v", steps(fx, t))
	}
}

// TestOneNodeCostsThreeWaits is the measurement behind the comment at
// lockLease: ONE machine that carries the roles api and web costs a round
// THREE waits, not one, because tunnel.Components gives such a server
// ngc-controller, ngc-api and ngc-web. With InstallWait of 15 minutes that is
// 45 minutes against a lease of 40 -- which is why the round renews the lease
// instead of trusting the constant.
//
// Nothing reports here, so every step really costs the full wait; the case
// turns the wait down to a fraction of a second and measures the multiple.
func TestOneNodeCostsThreeWaits(t *testing.T) {
	fx := newFixture(t)
	fx.Fleet.put(ctlNode, "amd64", "1.0.0", []string{tunnel.RoleAPI, tunnel.RoleWeb},
		map[string]string{trust.AutoAPI: "1.0.0", trust.AutoController: "1.0.0",
			trust.AutoWeb: "1.0.0"})
	fx.serveRelease(t, relPriv, 3, "1.1.0", theBodies())
	fx.approve(t, trust.AutoAPI, trust.AutoController, trust.AutoWeb)
	fx.U.Zone = time.UTC
	const wait = 200 * time.Millisecond
	fx.U.Wait = wait
	// Intermediate check: the one node really carries three parts. Without
	// this the case could measure a fleet of one part and still pass.
	if comp := tunnel.Components([]string{tunnel.RoleAPI, tunnel.RoleWeb}); len(comp) != 3 {
		t.Fatalf("Components(api, web) = %v, want three parts", comp)
	}
	start := time.Now()
	if err := fx.U.Rollout(ctx); err != nil {
		t.Fatalf("Rollout: %v", err)
	}
	took := time.Since(start)
	if n := len(fx.Send.only(core.ActInstall)); n != 3 {
		t.Fatalf("%d installs for one api+web node, want 3: %v", n, steps(fx, t))
	}
	// Three waits, with room for the work around them and against a slow
	// machine, but clearly more than one.
	if took < 3*wait {
		t.Fatalf("the round took %v, less than three waits (%v) -- then the parts do "+
			"not wait one after the other and the sum below is wrong", took, 3*wait)
	}
	t.Logf("one api+web node cost %v = %.2f x wait of %v", took, float64(took)/float64(wait), wait)
}

// TestRolloutStopsWhenItLosesTheLease: the lease on dbapi.LockUpdate runs out
// in the middle of a round and another API takes the lock. The round must stop
// right there with ErrLostLease -- driving on would be the second round over
// the same controllers that ErrNotDriving exists to prevent.
func TestRolloutStopsWhenItLosesTheLease(t *testing.T) {
	fx := readyToRoll(t)
	reports := fx.Send.onSend
	var stolen bool
	fx.Send.onSend = func(s sent) {
		reports(s)
		if stolen {
			return
		}
		stolen = true
		// A second API takes the lock, which it may only do once this one's
		// lease has run out: that is what the late clock stands for.
		ok, err := fx.S.DB.LockTake(ctx, dbapi.LockUpdate, "api-2",
			testNow+lockLease, testNow+lockLease+600)
		if err != nil || !ok {
			t.Errorf("the second API could not take the lock: %v %v", ok, err)
		}
	}
	err := fx.U.Rollout(ctx)
	if !errors.Is(err, ErrLostLease) {
		t.Fatalf("Rollout: %v, want ErrLostLease", err)
	}
	// Intermediate check: the round really got going before it stopped, so
	// the error above is the lost lease and not a wall in front of it.
	if len(fx.Send.list()) == 0 {
		t.Fatalf("nothing travelled at all -- then the round stopped before the lease")
	}
	// And it did not stop at the first step either: the round walked on until
	// the renewal in front of a step found the lock gone.
	if !stolen {
		t.Fatalf("the lock was never taken away")
	}
	// The lock belongs to the second API now, and the round that lost it did
	// not free it on its way out.
	if ok, err := fx.S.DB.LockTake(ctx, dbapi.LockUpdate, "api-3", testNow, testNow+600); ok || err != nil {
		t.Fatalf("api-3 took the lock of api-2: %v %v", ok, err)
	}
}

// TestRolloutRenewsTheLeasePerStep runs a round whose clock is pushed almost
// a whole lease forward after every step. Without a renewal the lease would be
// long gone by the second node; with one, a second API is turned away at every
// moment of the round.
func TestRolloutRenewsTheLeasePerStep(t *testing.T) {
	fx := readyToRoll(t)
	// A clock the case moves by hand, so the number of readings inside the
	// round decides nothing.
	clock := int64(testNow)
	fx.S.Now = func() int64 { return clock }
	reports := fx.Send.onSend
	steps := 0
	fx.Send.onSend = func(s sent) {
		reports(s)
		// One step of one node is one stage plus one install; the clock
		// moves once per step, on the stage that opens it.
		if s.action != core.ActStage {
			return
		}
		steps++
		// Almost a whole lease passes while this step runs: every single
		// step of the round outlives the lease it started with.
		clock += lockLease - 10
		// And right here, in the middle of the round, a second API must
		// still be kept out.
		if ok, err := fx.S.DB.LockTake(ctx, dbapi.LockUpdate, "api-2", clock, clock+600); ok || err != nil {
			t.Errorf("after step %d (second %d) api-2 took the lock: %v %v", steps,
				clock-testNow, ok, err)
		}
	}
	if err := fx.U.Rollout(ctx); err != nil {
		t.Fatalf("Rollout: %v", err)
	}
	// Intermediate check: the round really had more steps than one lease
	// covers, otherwise the checks above never had anything to catch.
	if steps < 2 || clock-testNow <= lockLease {
		t.Fatalf("%d steps over %d seconds -- that does not outlive one lease of %d",
			steps, clock-testNow, lockLease)
	}
	t.Logf("%d steps over %d seconds, lease %d", steps, clock-testNow, lockLease)
}
