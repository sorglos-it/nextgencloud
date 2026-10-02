package rights

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
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

// TestCheckRefusesAnAppThatIsNotThere: an app without a row, without a
// manifest or no longer active is not found — for an admin too, who is never
// asked for a right on it.
func TestCheckRefusesAnAppThatIsNotThere(t *testing.T) {
	fx := newFixture(t)
	open := wire.Open{App: termApp, Service: termSvc, Action: termAct, Window: wire.MinWindow}
	for name, req := range map[string]Request{
		"no app":      {Manifest: fx.Manifest, Node: ctlNode, Open: open},
		"no manifest": {App: fx.App, Node: ctlNode, Open: open},
		"inactive":    {App: &dbapi.App{ID: termApp, Node: ctlNode}, Manifest: fx.Manifest, Node: ctlNode, Open: open},
	} {
		req.Session = fx.session(t, fx.AdminUser)
		if _, code := Check(ctx, fx.DB, fx.List, req, now); code != wire.CodeNotFound {
			t.Fatalf("%s: %q", name, code)
		}
	}
}

// TestCheckIgnoresTheMembersOfTheOpen is the promise of this package: the
// decision comes from user, role, app and action, which the API resolved
// itself, never from what the OPEN carries besides the action's name. The
// members below answer exactly like the plain OPEN, before and after the right
// is granted — app and service too, which Check takes from the app's row and
// its manifest and not from the OPEN.
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
