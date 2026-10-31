package rights

import (
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/reason"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// TestARootTaskWithoutTheGrantSaysSo is the case of 19.10.2026, staged. On the
// real Debian machine an admin ordered a root task on a controller whose
// RootRoles list was empty, Check answered E_FORBIDDEN, and the whole of what
// a person could read afterwards was "Das hat der Rechner nicht angenommen."
// The honest sentence was in task.ErrForbidden all along.
//
// The fixture's controller grants root to the role admin, so the grant is
// taken away here and NOTHING else is changed -- same user, same signed task,
// same action. The control below runs the same call with the grant in place
// and gets a channel: that is what says the path is open as far as the line
// this case is about, rather than being turned away at an earlier wall
// (messen.md, "Ein Fall, der vor der gemeinten Stelle abgewiesen wird").
func TestARootTaskWithoutTheGrantSaysSo(t *testing.T) {
	fx := newFixture(t)
	action, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActDeactivate)
	if !ok {
		t.Fatal("the contract has no ngc.store/controller/deactivate")
	}
	// The control FIRST, because a task counts once: the same ID may not be
	// spent before the measurement. Two fixtures, so the two calls share no
	// database and no spent ID.
	ctl := newFixture(t)
	_, code, why := Check(ctx, ctl.DB, ctl.List, Request{Action: action, Node: ctlNode,
		Session: ctl.session(t, ctl.AdminUser),
		Open: wire.Open{App: action.App, Service: action.Service, Action: action.Name,
			Window: wire.MinWindow, Task: ctl.DeactivateTask(t)}}, now)
	if code != "" || why != reason.None {
		t.Fatalf("the way to the root check is not open: %s / %q", code, why)
	}

	// And now the machine of 19.10.: the node grants root to nobody.
	for i := range fx.List.Nodes {
		if fx.List.Nodes[i].ID == ctlNode {
			fx.List.Nodes[i].RootRoles = nil
			fx.List.Nodes[i].RootUsers = nil
		}
	}
	if err := fx.List.Validate(); err != nil {
		t.Fatalf("trust list: %v", err)
	}
	if fx.List.MayRoot(fx.AdminUser, ctlNode) {
		t.Fatal("the grant is still there, so this case would measure nothing")
	}
	_, code, why = Check(ctx, fx.DB, fx.List, Request{Action: action, Node: ctlNode,
		Session: fx.session(t, fx.AdminUser),
		Open: wire.Open{App: action.App, Service: action.Service, Action: action.Name,
			Window: wire.MinWindow, Task: fx.DeactivateTask(t)}}, now)
	if code != wire.CodeForbidden {
		t.Fatalf("code %q, want %s", code, wire.CodeForbidden)
	}
	if why != reason.NotRoot {
		t.Fatalf("the refusal says %q, want %q", why, reason.NotRoot)
	}
	// The sentence has to reach the person, which is the other half of the
	// finding: a reason only an administrator may read would leave the
	// operator with the code again.
	if !reason.Screen(why) {
		t.Fatal("the sentence of this refusal reaches no operator")
	}
}

// TestEveryRefusalOfCheckNamesACheck walks the refusals of this package and
// holds that none of them answers a code without a sentence. A code alone is
// what the three findings of 19.-21.10.2026 have in common.
func TestEveryRefusalOfCheckNamesACheck(t *testing.T) {
	fx := newFixture(t)
	appAction, _ := core.Lookup(core.AppStore, core.ServiceAPI, core.ActInstalled)
	// The office role may USE the app, so the row about an unknown action
	// reaches the manifest instead of being turned away one check earlier.
	if err := Set(ctx, fx.DB, core.RoleRights{Role: roleOffice,
		Rights: []string{termApp + "/" + core.RightUse}}); err != nil {
		t.Fatalf("rights: %v", err)
	}
	for _, c := range []struct {
		name string
		req  Request
		want reason.Reason
	}{
		{"a channel without a user that is not the API's own",
			Request{Action: appAction}, reason.ShellClosed},
		{"an admin action without an admin", Request{Action: appAction,
			Session: fx.session(t, fx.OfficeUser),
			Open:    wire.Open{App: appAction.App, Service: appAction.Service, Action: appAction.Name}},
			reason.AdminOnly},
		{"an app that is not installed", Request{Node: ctlNode,
			Session: fx.session(t, fx.OfficeUser),
			Open:    wire.Open{App: termApp, Service: termSvc, Action: termAct}},
			reason.AppUnknown},
		{"a task beside params of its own", Request{App: fx.App, Manifest: fx.Manifest,
			Node: ctlNode, Session: fx.session(t, fx.OfficeUser),
			Open: wire.Open{App: termApp, Service: termSvc, Action: termAct,
				Params: []byte(`{"a":1}`), Task: fx.appTask(t, fx.OfficeUser, termAct)}},
			reason.WithParams},
		{"an action the manifest does not have", Request{App: fx.App, Manifest: fx.Manifest,
			Node: ctlNode, Session: fx.session(t, fx.OfficeUser),
			Open: wire.Open{App: termApp, Service: termSvc, Action: "nosuch"}},
			reason.ActionUnknown},
	} {
		_, code, why := Check(ctx, fx.DB, fx.List, c.req, now)
		if code == "" {
			t.Errorf("%s: the channel opened, so nothing is measured here", c.name)
			continue
		}
		if why == reason.None {
			t.Errorf("%s: %s and no reason", c.name, code)
			continue
		}
		if !reason.Known(why) {
			t.Errorf("%s: %q is no sentence of the list", c.name, why)
		}
		if why != c.want {
			t.Errorf("%s: says %q, want %q", c.name, why, c.want)
		}
	}
}

// TestTheOfficeUserIsNoAdmin is the intermediate check of the case above: the
// second row measures the admin wall, and it only measures it if that user is
// really no admin.
func TestTheOfficeUserIsNoAdmin(t *testing.T) {
	fx := newFixture(t)
	_, admin, err := Of(ctx, fx.DB, fx.List, fx.OfficeUser)
	if err != nil {
		t.Fatalf("rights: %v", err)
	}
	if admin {
		t.Fatal("the office user is an admin, so the row above measures nothing")
	}
	if fx.List.Roles[0].Name != trust.RoleAdmin {
		t.Fatal("the fixture has no admin role any more")
	}
}
