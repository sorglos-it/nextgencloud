package rights

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

func TestRightsOfARole(t *testing.T) {
	fx := newFixture(t)
	if err := Set(ctx, fx.DB, core.RoleRights{Role: "office",
		Rights: []string{"org.sorglos.terminal/use", "org.sorglos.terminal/shell"}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, admin, err := Of(ctx, fx.DB, fx.List, fx.OfficeUser)
	if err != nil {
		t.Fatalf("of: %v", err)
	}
	if admin {
		t.Fatal("an office user is no admin")
	}
	if !got["org.sorglos.terminal/use"] || !got["org.sorglos.terminal/shell"] {
		t.Fatalf("rights %v", got)
	}
	if got["org.sorglos.other/use"] {
		t.Fatalf("a right appeared out of nowhere: %v", got)
	}
	// An admin needs no row: the role admin may use every core action.
	if _, adminToo, err := Of(ctx, fx.DB, fx.List, fx.AdminUser); err != nil || !adminToo {
		t.Fatalf("admin: %v %v", adminToo, err)
	}
}

// TestRightsOfAStranger: a user the trust list does not know has no rights and
// is no admin, and that is not an error.
func TestRightsOfAStranger(t *testing.T) {
	fx := newFixture(t)
	got, admin, err := Of(ctx, fx.DB, fx.List, "no-such-user")
	if got != nil || admin || err != nil {
		t.Fatalf("stranger: %v %v %v", got, admin, err)
	}
}

// TestSetRefusesAnInvalidRole: Set is the only way a row is written, so it
// keeps out what read would later have to refuse.
func TestSetRefusesAnInvalidRole(t *testing.T) {
	fx := newFixture(t)
	for _, r := range []core.RoleRights{
		{Role: "no name at all"},
		{Role: "office", Rights: []string{"ngc.store/use"}},        // a core app grants no rights
		{Role: "office", Rights: []string{"org.sorglos.terminal"}}, // no action
	} {
		if err := Set(ctx, fx.DB, r); !errors.Is(err, ErrRights) {
			t.Fatalf("set %v: %v", r, err)
		}
	}
	if _, err := fx.DB.BlobGet(ctx, dbapi.BlobRights, "office"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("a refused Set wrote a row: %v", err)
	}
}

// TestRightsRefuseARowTheDatabaseCannotHold: a row that is not a valid
// RoleRights of this very role is an error, never silently no rights.
func TestRightsRefuseARowTheDatabaseCannotHold(t *testing.T) {
	for _, data := range []string{
		`not json`,
		`{"role":"office","rights":["nope"]}`,       // Validate refuses the right
		`{"role":"admin","rights":["org.a.b/use"]}`, // the row of another role
	} {
		fx := newFixture(t)
		if err := fx.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobRights, ID: "office", Data: []byte(data)}); err != nil {
			t.Fatalf("put: %v", err)
		}
		if _, _, err := Of(ctx, fx.DB, fx.List, fx.OfficeUser); !errors.Is(err, ErrRights) {
			t.Fatalf("row %s: %v", data, err)
		}
		if _, err := All(ctx, fx.DB, fx.List); !errors.Is(err, ErrRights) {
			t.Fatalf("all, row %s: %v", data, err)
		}
		// A broken row of one role must not let a channel open either.
		if code := fx.CheckApp(t, fx.OfficeUser, termAct); code != wire.CodeUnreachable {
			t.Fatalf("check, row %s: %q", data, code)
		}
	}
}

// TestAllRoles: every role of the trust list appears, a role without a row
// with an empty list, so the shell can show it.
func TestAllRoles(t *testing.T) {
	fx := newFixture(t)
	if err := Set(ctx, fx.DB, core.RoleRights{Role: "office", Rights: []string{"org.sorglos.terminal/use"}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	all, err := All(ctx, fx.DB, fx.List)
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != len(fx.List.Roles) {
		t.Fatalf("%d rows for %d roles: %v", len(all), len(fx.List.Roles), all)
	}
	for _, r := range all {
		switch r.Role {
		case trust.RoleAdmin:
			if len(r.Rights) != 0 {
				t.Fatalf("admin has a row: %v", r)
			}
		case roleOffice:
			if len(r.Rights) != 1 || r.Rights[0] != "org.sorglos.terminal/use" {
				t.Fatalf("office: %v", r)
			}
		default:
			t.Fatalf("unknown role %q", r.Role)
		}
	}
}

func TestCheckRefusesACoreActionWithoutAdmin(t *testing.T) {
	fx := newFixture(t)
	action, _ := core.Lookup(core.AppUsers, core.ServiceAPI, core.ActList)
	code := checkAs(t, fx, fx.OfficeUser, action, nil)
	if code != wire.CodeForbidden {
		t.Fatalf("code %q", code)
	}
	if code := checkAs(t, fx, fx.AdminUser, action, nil); code != "" {
		t.Fatalf("an admin was refused: %q", code)
	}
}

func TestCheckNeedsATaskForARootAction(t *testing.T) {
	fx := newFixture(t)
	action, _ := core.Lookup(core.AppStore, core.ServiceController, core.ActDeactivate)
	if code := checkAs(t, fx, fx.AdminUser, action, nil); code != wire.CodeConfirm {
		t.Fatalf("without a task: %q", code)
	}
	// With a signed task from an admin who may run root tasks on the node it
	// passes, and the task ID is spent once.
	code := checkAs(t, fx, fx.AdminUser, action, fx.DeactivateTask(t))
	if code != "" {
		t.Fatalf("with a task: %q", code)
	}
	if code := checkAs(t, fx, fx.AdminUser, action, fx.DeactivateTask(t)); code != wire.CodeVerify {
		t.Fatalf("a replayed task answered %q", code)
	}
}

// TestCheckRefusesARootTaskOnAnotherNode: the task is good, but a user may run
// root tasks only where the trust list says so.
func TestCheckRefusesARootTaskOnAnotherNode(t *testing.T) {
	fx := newFixture(t)
	for i := range fx.List.Nodes {
		if fx.List.Nodes[i].ID == ctlNode {
			fx.List.Nodes[i].RootRoles = nil
		}
	}
	action, _ := core.Lookup(core.AppStore, core.ServiceController, core.ActDeactivate)
	if code := checkAs(t, fx, fx.AdminUser, action, fx.DeactivateTask(t)); code != wire.CodeForbidden {
		t.Fatalf("code %q", code)
	}
}

func TestCheckRefusesAChannelTheShellMayNotOpen(t *testing.T) {
	fx := newFixture(t)
	action, _ := core.Lookup(core.AppStore, core.ServiceController, core.ActStage)
	if core.ShellOpens(action) {
		t.Fatal("the contract changed: the shell may open stage")
	}
	if code := checkAs(t, fx, fx.AdminUser, action, nil); code != wire.CodeForbidden {
		t.Fatalf("code %q", code)
	}
}

// TestCheckWithoutAUser: a channel the API opens itself may run only the
// actions core.WithoutUser allows, and never an app's.
func TestCheckWithoutAUser(t *testing.T) {
	fx := newFixture(t)
	stage, _ := core.Lookup(core.AppStore, core.ServiceController, core.ActStage)
	if !core.WithoutUser(stage) {
		t.Fatal("the contract changed: the API may not open stage without a user")
	}
	req := Request{Action: stage, Node: ctlNode,
		Open: wire.Open{App: stage.App, Service: stage.Service, Action: stage.Name, Window: wire.MinWindow}}
	if _, code := Check(ctx, fx.DB, fx.List, req, now); code != "" {
		t.Fatalf("stage without a user: %q", code)
	}
	list, _ := core.Lookup(core.AppUsers, core.ServiceAPI, core.ActList)
	req.Action, req.Node = list, ""
	req.Open = wire.Open{App: list.App, Service: list.Service, Action: list.Name, Window: wire.MinWindow}
	if _, code := Check(ctx, fx.DB, fx.List, req, now); code != wire.CodeForbidden {
		t.Fatalf("list without a user: %q", code)
	}
	if _, code := Check(ctx, fx.DB, fx.List, Request{App: fx.App, Manifest: fx.Manifest, Node: ctlNode,
		Open: wire.Open{App: termApp, Service: termSvc, Action: termAct, Window: wire.MinWindow}}, now); code != wire.CodeForbidden {
		t.Fatalf("app channel without a user: %q", code)
	}
}

func TestCheckRefusesAnAppWithoutTheRight(t *testing.T) {
	fx := newFixture(t)
	if code := fx.CheckApp(t, fx.OfficeUser, "shell"); code != wire.CodeForbidden {
		t.Fatalf("without the right: %q", code)
	}
	if err := Set(ctx, fx.DB, core.RoleRights{Role: "office",
		Rights: []string{"org.sorglos.terminal/use", "org.sorglos.terminal/shell"}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if code := fx.CheckApp(t, fx.OfficeUser, "shell"); code != "" {
		t.Fatalf("with the right: %q", code)
	}
	// An action the manifest does not have is not found, not forbidden.
	if code := fx.CheckApp(t, fx.OfficeUser, "nothing"); code != wire.CodeNotFound {
		t.Fatalf("unknown action: %q", code)
	}
}

// TestCheckNeedsBothRights: "use" lets a user see the app and the action right
// opens the channel; neither stands for the other, so each alone is refused.
func TestCheckNeedsBothRights(t *testing.T) {
	for _, right := range []string{core.RightUse, termAct} {
		fx := newFixture(t)
		if err := Set(ctx, fx.DB, core.RoleRights{Role: roleOffice, Rights: []string{termApp + "/" + right}}); err != nil {
			t.Fatalf("set: %v", err)
		}
		if code := fx.CheckApp(t, fx.OfficeUser, termAct); code != wire.CodeForbidden {
			t.Fatalf("only %q: %q", right, code)
		}
	}
}

// TestCheckHidesWhetherAnAppIsThere: an app without a row, without a manifest
// or no longer active answers exactly like a missing right, so the answer
// tells a user nothing about the installation.
//
// An admin sees the same here, and the reason is in the fixture: an admin
// holds the right of every INSTALLED app (rights.Of, adminApps), and this
// case writes no app row at all -- the rows it varies live in the Request and
// never in the database. What is measured is the answer of a broken request,
// which is the same for everybody.
func TestCheckHidesWhetherAnAppIsThere(t *testing.T) {
	fx := newFixture(t)
	open := wire.Open{App: termApp, Service: termSvc, Action: termAct, Window: wire.MinWindow}
	// The yardstick: installed and active, but without the right.
	_, want := fx.checkAppOpen(t, fx.OfficeUser, open)
	if want != wire.CodeForbidden {
		t.Fatalf("no right: %q", want)
	}
	for name, req := range map[string]Request{
		"no app":      {Manifest: fx.Manifest, Node: ctlNode, Open: open},
		"no manifest": {App: fx.App, Node: ctlNode, Open: open},
		"inactive":    {App: &dbapi.App{ID: termApp, Node: ctlNode}, Manifest: fx.Manifest, Node: ctlNode, Open: open},
	} {
		for _, user := range []string{fx.OfficeUser, fx.AdminUser} {
			req.Session = fx.session(t, user)
			if _, code := Check(ctx, fx.DB, fx.List, req, now); code != want {
				t.Fatalf("%s as %s: %q instead of %q", name, user, code, want)
			}
		}
	}
	// And with every right of the app: the plain OPEN opens, the three answer
	// as before, so each of them refuses on its own and not because a right
	// was missing.
	if err := Set(ctx, fx.DB, core.RoleRights{Role: roleOffice,
		Rights: []string{termApp + "/" + core.RightUse, termApp + "/" + termAct}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, code := fx.checkAppOpen(t, fx.OfficeUser, open); code != "" {
		t.Fatalf("with the rights: %q", code)
	}
	for name, req := range map[string]Request{
		"no app":      {Manifest: fx.Manifest, Node: ctlNode, Open: open},
		"no manifest": {App: fx.App, Node: ctlNode, Open: open},
		"inactive":    {App: &dbapi.App{ID: termApp, Node: ctlNode}, Manifest: fx.Manifest, Node: ctlNode, Open: open},
	} {
		req.Session = fx.session(t, fx.OfficeUser)
		if _, code := Check(ctx, fx.DB, fx.List, req, now); code != want {
			t.Fatalf("%s with the rights: %q instead of %q", name, code, want)
		}
	}
}

// TestCheckRefusesAnActionNamedUse holds the namespace collision: a manifest
// may name an action "use", and core.RightUse is "use" as well, so the right
// <app>/use would be the action's right too. Such a channel never opens, not
// even for a user who holds every right of the app.
func TestCheckRefusesAnActionNamedUse(t *testing.T) {
	fx := newFixture(t)
	const collide = `{"id":"` + termApp + `","name":"Terminal","version":"1.0.0","publisher":"Sorglos",` +
		`"actions":[{"name":"use","title":"Benutzen","service":"` + termSvc + `"}],` +
		`"services":[{"name":"` + termSvc + `","command":["/usr/bin/terminal"]}]}`
	m, err := store.ParseManifest([]byte(collide))
	if err != nil {
		t.Fatalf("the contract changed: a manifest may no longer name an action use: %v", err)
	}
	if _, ok := m.Action(core.RightUse); !ok {
		t.Fatal("the manifest lost its action")
	}
	for _, rights := range [][]string{
		{termApp + "/" + core.RightUse},
		{termApp + "/" + core.RightUse, termApp + "/" + core.RightUse + "x"},
	} {
		if err := Set(ctx, fx.DB, core.RoleRights{Role: roleOffice, Rights: rights}); err != nil {
			t.Fatalf("set: %v", err)
		}
		_, code := Check(ctx, fx.DB, fx.List, Request{App: fx.App, Manifest: m, Node: ctlNode,
			Session: fx.session(t, fx.OfficeUser),
			Open:    wire.Open{App: termApp, Service: termSvc, Action: core.RightUse, Window: wire.MinWindow}}, now)
		if code != wire.CodeNotFound {
			t.Fatalf("rights %v: %q", rights, code)
		}
	}
}

// TestCheckRefusesAnOpenWithTaskAndParams: wire.Open.Validate already says an
// OPEN with a task carries no params of its own. Check does not rely on the
// caller having seen to that.
func TestCheckRefusesAnOpenWithTaskAndParams(t *testing.T) {
	fx := newFixture(t)
	if err := Set(ctx, fx.DB, core.RoleRights{Role: roleOffice,
		Rights: []string{termApp + "/" + core.RightUse, termApp + "/" + termAct}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	open := wire.Open{App: termApp, Service: termSvc, Action: termAct, Window: wire.MinWindow,
		Task: fx.appTask(t, fx.OfficeUser, termAct), Params: json.RawMessage(`{}`)}
	// The params come first: a task counts once, so the other order would read
	// the replay guard and not this one.
	if _, code := fx.checkAppOpen(t, fx.OfficeUser, open); code != wire.CodeVerify {
		t.Fatalf("task and params: %q", code)
	}
	// The same task without params opens, so the params are what was refused —
	// and the task was not spent on the refusal.
	open.Params = nil
	if _, code := fx.checkAppOpen(t, fx.OfficeUser, open); code != "" {
		t.Fatalf("task alone: %q", code)
	}
}

// TestCheckOpensOnASignedReconfirmation is the other half of the promise: an
// unsigned part of the OPEN moves nothing, a signed one does. The role asks
// for every change to be re-confirmed, so the plain OPEN is turned away with
// E_CONFIRM, the signed task alone is still E_CONFIRM, and only the user's own
// re-confirmation of that very task opens the channel.
func TestCheckOpensOnASignedReconfirmation(t *testing.T) {
	fx := newFixture(t)
	fx.setLevel(t, roleOffice, trust.LevelHardcore)
	if err := Set(ctx, fx.DB, core.RoleRights{Role: roleOffice,
		Rights: []string{termApp + "/" + core.RightUse, termApp + "/" + termAct}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	open := wire.Open{App: termApp, Service: termSvc, Action: termAct, Window: wire.MinWindow}
	if _, code := fx.checkAppOpen(t, fx.OfficeUser, open); code != wire.CodeConfirm {
		t.Fatalf("plain: %q", code)
	}
	open.Task = fx.appTask(t, fx.OfficeUser, termAct)
	if _, code := fx.checkAppOpen(t, fx.OfficeUser, open); code != wire.CodeConfirm {
		t.Fatalf("task alone: %q", code)
	}
	// Another user's key is no answer, even for the same task.
	open.Confirm = fx.confirm(t, fx.AdminUser, open.Task)
	if _, code := fx.checkAppOpen(t, fx.OfficeUser, open); code != wire.CodeConfirm {
		t.Fatalf("someone else confirmed: %q", code)
	}
	open.Confirm = fx.confirm(t, fx.OfficeUser, open.Task)
	tk, code := fx.checkAppOpen(t, fx.OfficeUser, open)
	if code != "" || tk == nil {
		t.Fatalf("task and confirmation: %q %v", code, tk)
	}
}

// TestCheckIgnoresTheMembersOfTheOpen is the promise of this package: the
// decision comes from user, role, app and action, which the API resolved
// itself, never from what the OPEN carries besides the action's name. The
// members below answer exactly like the plain OPEN, before and after the right
// is granted — app and service too, which Check takes from the app's row and
// its manifest and not from the OPEN.
//
// One member of the OPEN does move the answer, and leaving it unsaid here
// would send the next reader to the wrong place: an OPEN that carries a TASK
// and params of its own is refused with E_VERIFY, by the LENGTH of the params
// and never by their content. None of the OPENs below carries a task, so none
// of them meets that wall; TestCheckRefusesAnOpenWithTaskAndParams is where it
// is measured.
func TestCheckIgnoresTheMembersOfTheOpen(t *testing.T) {
	fx := newFixture(t)
	plain := wire.Open{App: termApp, Service: termSvc, Action: termAct, Window: wire.MinWindow}
	ignored := map[string]wire.Open{
		"params":      {App: termApp, Service: termSvc, Action: termAct, Window: wire.MinWindow, Params: json.RawMessage(`{"admin":true}`)},
		"window":      {App: termApp, Service: termSvc, Action: termAct, Window: wire.MaxWindow},
		"e2e":         {App: termApp, Service: termSvc, Action: termAct, Window: wire.MinWindow, E2E: make([]byte, 8)},
		"another app": {App: core.AppStore, Service: termSvc, Action: termAct, Window: wire.MinWindow},
		"another svc": {App: termApp, Service: core.ServiceAPI, Action: termAct, Window: wire.MinWindow},
	}
	for _, granted := range []bool{false, true} {
		if granted {
			if err := Set(ctx, fx.DB, core.RoleRights{Role: roleOffice,
				Rights: []string{termApp + "/" + core.RightUse, termApp + "/" + termAct}}); err != nil {
				t.Fatalf("set: %v", err)
			}
		}
		_, want := fx.checkAppOpen(t, fx.OfficeUser, plain)
		if granted != (want == "") {
			t.Fatalf("granted=%v answered %q", granted, want)
		}
		for name, open := range ignored {
			if _, code := fx.checkAppOpen(t, fx.OfficeUser, open); code != want {
				t.Fatalf("granted=%v, %s: %q instead of %q", granted, name, code, want)
			}
		}
	}
}

// TestCheckNeverGainsFromATaskInTheOpen: a task in the OPEN is evidence
// task.Check verifies, so it can only tighten the answer. A task for another
// channel refuses a channel that was allowed, and it never turns a refusal
// into permission.
func TestCheckNeverGainsFromATaskInTheOpen(t *testing.T) {
	fx := newFixture(t)
	foreign := wire.Open{App: termApp, Service: termSvc, Action: termAct,
		Window: wire.MinWindow, Task: fx.DeactivateTask(t)}
	// Without the right it stays forbidden: the right is checked before any
	// task is read, so the root task of an admin buys the office user nothing.
	if _, code := fx.checkAppOpen(t, fx.OfficeUser, foreign); code != wire.CodeForbidden {
		t.Fatalf("without the right: %q", code)
	}
	if err := Set(ctx, fx.DB, core.RoleRights{Role: roleOffice,
		Rights: []string{termApp + "/" + core.RightUse, termApp + "/" + termAct}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	// With the right the plain OPEN may open, and the foreign task is refused
	// because it names another channel.
	if code := fx.CheckApp(t, fx.OfficeUser, termAct); code != "" {
		t.Fatalf("plain: %q", code)
	}
	if _, code := fx.checkAppOpen(t, fx.OfficeUser, foreign); code != wire.CodeVerify {
		t.Fatalf("with the right: %q", code)
	}
}

// TestCheckTakesTheChannelFromTheRowNotTheOpen: the OPEN claims another app
// and service, and carries a task that matches exactly that claim. The channel
// is still the app's own, so the task names another channel and is refused.
// Were app and service read from the OPEN, this would open.
func TestCheckTakesTheChannelFromTheRowNotTheOpen(t *testing.T) {
	fx := newFixture(t)
	if err := Set(ctx, fx.DB, core.RoleRights{Role: roleOffice,
		Rights: []string{termApp + "/" + core.RightUse, termApp + "/" + termAct}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	open := wire.Open{App: core.AppStore, Service: core.ServiceController, Action: termAct, Window: wire.MinWindow}
	open.Task = fx.signTask(t, fx.OfficeUser, task.Task{ID: bytes.Repeat([]byte{0x62}, task.IDSize),
		Node: ctlNode, App: open.App, Service: open.Service, Action: open.Action})
	if _, code := fx.checkAppOpen(t, fx.OfficeUser, open); code != wire.CodeVerify {
		t.Fatalf("code %q", code)
	}
}

// --- the implicit rights of an admin --------------------------------------
//
// The circle these cases hold open: an app only reached the menu with
// <app>/use (coreapp.Dispatcher.apps), and the page that hands rights out
// built its list of apps from that menu -- so a freshly installed app was in
// nobody's menu and its right in nobody's list. Measured on the Debian VM on
// 21.10.2026: org.sorglos.terminal was set up and the rights page said "Es
// gibt noch keine App, für die es Rechte gäbe."

// TestAnAdminHoldsTheRightsOfAnInstalledAppWithoutARow is the decisive case:
// no row grants anything, and the admin holds use AND the action of the
// installed app. The office user is the control in the same case -- what
// grants the right is being an admin and not the app row.
func TestAnAdminHoldsTheRightsOfAnInstalledAppWithoutARow(t *testing.T) {
	fx := newFixture(t)
	if err := fx.DB.AppPut(ctx, *fx.App); err != nil {
		t.Fatalf("app row: %v", err)
	}
	got, admin, err := Of(ctx, fx.DB, fx.List, fx.AdminUser)
	if err != nil || !admin {
		t.Fatalf("of: %v %v", admin, err)
	}
	if !got[termApp+"/"+core.RightUse] {
		t.Fatalf("an admin does not hold %q: %v", termApp+"/"+core.RightUse, got)
	}
	if !got[termApp+"/"+termAct] {
		t.Fatalf("an admin does not hold the action %q: %v", termApp+"/"+termAct, got)
	}
	// Nothing is invented: an action the manifest does not name is not there.
	if got[termApp+"/nosuchaction"] {
		t.Fatalf("a right appeared that no manifest names: %v", got)
	}
	// The control: the same app row, a user who is no admin, no row -- and
	// nothing at all. Without this half the case would also pass if Of handed
	// every app to everybody.
	other, otherAdmin, err := Of(ctx, fx.DB, fx.List, fx.OfficeUser)
	if err != nil || otherAdmin {
		t.Fatalf("office: %v %v", otherAdmin, err)
	}
	if other[termApp+"/"+core.RightUse] || other[termApp+"/"+termAct] {
		t.Fatalf("a user who is no admin holds the app's rights: %v", other)
	}
}

// TestTheRightOfAnAdminSurvivesAnEmptyRow: the right lives in no row, so the
// row the rights page writes when nothing is ticked cannot take it away.
// Before and after in ONE case, so a green run cannot come from the right
// never having been there.
func TestTheRightOfAnAdminSurvivesAnEmptyRow(t *testing.T) {
	fx := newFixture(t)
	if err := fx.DB.AppPut(ctx, *fx.App); err != nil {
		t.Fatalf("app row: %v", err)
	}
	use := termApp + "/" + core.RightUse
	before, _, err := Of(ctx, fx.DB, fx.List, fx.AdminUser)
	if err != nil || !before[use] {
		t.Fatalf("before: %v %v", before, err)
	}
	if err := Set(ctx, fx.DB, core.RoleRights{Role: trust.RoleAdmin}); err != nil {
		t.Fatalf("empty row: %v", err)
	}
	after, _, err := Of(ctx, fx.DB, fx.List, fx.AdminUser)
	if err != nil {
		t.Fatalf("after: %v", err)
	}
	if !after[use] {
		t.Fatalf("an empty row for the role admin took %q away: %v", use, after)
	}
}

// TestAnAdminHoldsNothingOfARowTheMenuWouldNotShow: the implicit right follows
// the row the menu shows and not the installation as such. Each state is
// measured against the SAME app, with the working state last, so the case
// cannot be green because this app never grants anything.
func TestAnAdminHoldsNothingOfARowTheMenuWouldNotShow(t *testing.T) {
	use := termApp + "/" + core.RightUse
	for _, c := range []struct {
		name string
		row  func(a *dbapi.App)
		want bool
	}{
		{"deactivated", func(a *dbapi.App) { a.Active = false }, false},
		{"no version", func(a *dbapi.App) { a.Version = "" }, false},
		{"broken manifest", func(a *dbapi.App) { a.Manifest = []byte("{not json") }, false},
		{"as installed", func(a *dbapi.App) {}, true},
	} {
		fx := newFixture(t)
		row := *fx.App
		c.row(&row)
		if err := fx.DB.AppPut(ctx, row); err != nil {
			t.Fatalf("%s: app row: %v", c.name, err)
		}
		got, _, err := Of(ctx, fx.DB, fx.List, fx.AdminUser)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got[use] != c.want {
			t.Errorf("%s: holds %q = %v, want %v", c.name, use, got[use], c.want)
		}
	}
}

// TestAnAdminOpensAnInstalledAppWithoutARow goes one step further than Of: the
// right is not only in the map, it opens the channel. Without this case the
// menu could show an app that refuses every click.
func TestAnAdminOpensAnInstalledAppWithoutARow(t *testing.T) {
	fx := newFixture(t)
	if err := fx.DB.AppPut(ctx, *fx.App); err != nil {
		t.Fatalf("app row: %v", err)
	}
	if code := fx.CheckApp(t, fx.AdminUser, termAct); code != "" {
		t.Fatalf("an admin cannot open the installed app: %q", code)
	}
	// The control in the same shape: a user who is no admin is still refused.
	if code := fx.CheckApp(t, fx.OfficeUser, termAct); code != wire.CodeForbidden {
		t.Fatalf("a user who is no admin got in: %q", code)
	}
}
