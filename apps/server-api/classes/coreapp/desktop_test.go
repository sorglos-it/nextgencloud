package coreapp

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/controllers"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/rights"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// upTunnels is a stand-in for the open tunnels in which every node answers.
// Only the second value of Get is read, so the nil tunnel never travels.
type upTunnels struct{}

func (upTunnels) Get(string) (*controllers.Tunnel, bool) { return nil, true }

// call opens a core action of ngc.desktop and reads its result.
func (h *harness) call(t *testing.T, action string, params string) []byte {
	t.Helper()
	o := wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: action, Window: wire.MinWindow}
	if params != "" {
		o.Params = json.RawMessage(params)
	}
	c := h.Open(t, o)
	return c.result(t)
}

func TestMeNamesRolesAdminAndTheStrictestLevel(t *testing.T) {
	fx := newFixture(t)
	var me core.Me
	if err := json.Unmarshal(fx.call(t, core.ActMe, ""), &me); err != nil {
		t.Fatalf("me: %v", err)
	}
	if me.ID != fx.AdminID || !me.Admin {
		t.Fatalf("me %+v", me)
	}
	if len(me.Roles) != 1 || me.Roles[0] != trust.RoleAdmin {
		t.Fatalf("roles %+v", me.Roles)
	}
	// trusttest.Genesis puts the role admin on level critical, and the user
	// logs in with a password key, so the hint is shown and TOTP is off.
	if me.Level != trust.LevelCritical {
		t.Fatalf("level %q", me.Level)
	}
	if !me.PasswordLogin || me.TOTP {
		t.Fatalf("me %+v", me)
	}
}

func TestMeSaysTOTPIsOnOnceTheRowCarriesASecret(t *testing.T) {
	fx := newFixture(t)
	row, err := fx.S.DB.UserGet(ctx, fx.AdminID)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	row.TOTP = []byte("sealed")
	if err := fx.S.DB.UserPut(ctx, row); err != nil {
		t.Fatalf("put: %v", err)
	}
	var me core.Me
	if err := json.Unmarshal(fx.call(t, core.ActMe, ""), &me); err != nil {
		t.Fatalf("me: %v", err)
	}
	if !me.TOTP {
		t.Fatalf("me %+v", me)
	}
}

// TestTheStrictestLevelOfSeveralRolesCounts holds the loop of me: a user in
// two roles is judged by the stricter one. Without it a dispatcher that took
// the first or the last role would pass.
func TestTheStrictestLevelOfSeveralRolesCounts(t *testing.T) {
	fx := newFixture(t)
	if got := rank(trust.LevelHardcore); got <= rank(trust.LevelCritical) {
		t.Fatalf("hardcore ranks %d, critical %d", got, rank(trust.LevelCritical))
	}
	if stricter(trust.LevelCritical, trust.LevelHardcore) {
		t.Fatal("critical counted as stricter than hardcore")
	}
	if !stricter(trust.LevelCritical, trust.LevelOff) {
		t.Fatal("critical did not count as stricter than off")
	}
	_ = fx
}

// TestAppsHidesAnAppWithoutTheRightToUseIt measures the hiding rule on a user
// who is NO admin, which is everybody it still applies to: an admin holds the
// right of every installed app without a row (rights.Of, adminApps), so the
// session of this fixture cannot show the rule any more. The admin's side is
// TestAppsShowsAnInstalledAppToAnAdminWithoutARight.
func TestAppsHidesAnAppWithoutTheRightToUseIt(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	bob := nonAdminSession(t, fx)
	// First the open way: with the right the app is in the menu. Without this
	// half a menu that is empty for another reason would pass as well.
	grantRole(t, fx, userRole, appY+"/"+core.RightUse)
	if _, ok := appEntry(t, bob.call(t, core.ActApps, ""), appY); !ok {
		t.Fatal("the app is missing although the right is granted")
	}
	grantRole(t, fx, userRole)
	if e, ok := appEntry(t, bob.call(t, core.ActApps, ""), appY); ok {
		t.Fatalf("the app is in the menu without the right: %+v", e)
	}
}

// TestAppsShowsAnInstalledAppToAnAdminWithoutARight is the circle this change
// broke open. An app only reached the menu with <app>/use, and the page that
// hands rights out built its list of apps FROM that menu -- so a freshly
// installed app was in nobody's menu and its right was in nobody's list, and
// no app could ever be handed to anybody. Measured on the Debian VM on
// 21.10.2026: org.sorglos.terminal was installed and set up, and the rights
// page said "Es gibt noch keine App, für die es Rechte gäbe."
//
// The admin's rights carry no row here on purpose: Grant is never called, so
// what shows the app is the admin's role and nothing else.
func TestAppsShowsAnInstalledAppToAnAdminWithoutARight(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	e, ok := appEntry(t, fx.call(t, core.ActApps, ""), appY)
	if !ok {
		t.Fatal("a freshly installed app is missing from the admin's menu")
	}
	// The name comes with it, which is what the rights page shows instead of
	// the raw id (rightName in apps/web/.../users.js).
	if e.Name == "" || e.Name == appY {
		t.Fatalf("no name to show: %+v", e)
	}
	// And the actions come with it, so a per-action right can be offered at
	// all and not just "benutzen".
	if len(e.Actions) != 1 || e.Actions[0] != appAct {
		t.Fatalf("actions %+v, want the one of the manifest", e.Actions)
	}
}

// TestAnAdminKeepsTheAppRightAfterSavingTheRoleEmpty: the right lives in no
// row, so saving the role admin with an empty set of rights -- which is what
// the rights page sends when nothing is ticked -- cannot take it away.
func TestAnAdminKeepsTheAppRightAfterSavingTheRoleEmpty(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	fx.Grant(t) // the empty row the page would write
	if _, ok := appEntry(t, fx.call(t, core.ActApps, ""), appY); !ok {
		t.Fatal("an empty row for the role admin took the app away")
	}
}

// TestAppsHidesAnAppThatIsSwitchedOffFromAnAdmin: the implicit right follows
// the row the menu would show and not the installation as such, so a row that
// is deactivated grants an admin nothing either.
func TestAppsHidesAnAppThatIsSwitchedOffFromAnAdmin(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, false)
	if e, ok := appEntry(t, fx.call(t, core.ActApps, ""), appY); ok {
		t.Fatalf("a deactivated app is in the admin's menu: %+v", e)
	}
	// The way back, so the case is not green because the fixture never shows
	// this app at all.
	fx.PutApp(t, appY, true)
	if _, ok := appEntry(t, fx.call(t, core.ActApps, ""), appY); !ok {
		t.Fatal("the app stays hidden after it was switched on")
	}
}

// grantRole gives any role a set of rights; harness.Grant does it for the role
// admin, and the cases that measure a non-admin need another one.
func grantRole(t *testing.T, h *harness, role string, right ...string) {
	t.Helper()
	if err := rights.Set(ctx, h.S.DB, core.RoleRights{Role: role, Rights: right}); err != nil {
		t.Fatalf("rights of %s: %v", role, err)
	}
}

// TestAppsNamesTheActionsTheUserMayUse measures the per-action rights on a
// user who is no admin, for the same reason as the case above.
func TestAppsNamesTheActionsTheUserMayUse(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	bob := nonAdminSession(t, fx)
	grantRole(t, fx, userRole, appY+"/"+core.RightUse)
	e, ok := appEntry(t, bob.call(t, core.ActApps, ""), appY)
	if !ok {
		t.Fatal("the app is missing")
	}
	if len(e.Actions) != 0 {
		t.Fatalf("actions %+v without the right", e.Actions)
	}
	grantRole(t, fx, userRole, appY+"/"+core.RightUse, appY+"/"+appAct)
	e, _ = appEntry(t, bob.call(t, core.ActApps, ""), appY)
	if len(e.Actions) != 1 || e.Actions[0] != appAct {
		t.Fatalf("actions %+v", e.Actions)
	}
	// The entry of the manifest travels with it, so the shell knows what to
	// load; nothing of it is signed, and the shell starts no file the ui-tar
	// does not hold.
	if e.Entry != "index.html" || e.Icon != "logo.png" || e.Width != 800 || e.Height != 600 {
		t.Fatalf("entry %+v", e)
	}
	if e.Node != ctlNode {
		t.Fatalf("node %q", e.Node)
	}
}

func TestAppsSaysUnreachableWithoutATunnel(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	fx.Grant(t, appY+"/"+core.RightUse)
	e, ok := appEntry(t, fx.call(t, core.ActApps, ""), appY)
	if !ok {
		t.Fatal("the app is missing")
	}
	if e.State != core.AppUnreachable {
		t.Fatalf("state %q without a tunnel", e.State)
	}
}

func TestAppsSaysOkWhenTheControllerIsThere(t *testing.T) {
	fx := newFixture(t)
	fx.D.Tunnels = upTunnels{}
	fx.PutApp(t, appY, true)
	fx.Grant(t, appY+"/"+core.RightUse)
	e, _ := appEntry(t, fx.call(t, core.ActApps, ""), appY)
	if e.State != core.AppOK {
		t.Fatalf("state %q", e.State)
	}
}

// TestAppsSaysUnlicensedForALicenceThatDoesNotHold measures the third state.
// The licence row here is one the API cannot verify any anymore, and that
// counts as expired and never as valid: a row the API cannot verify must not
// unlock an app.
func TestAppsSaysUnlicensedForALicenceThatDoesNotHold(t *testing.T) {
	fx := newFixture(t)
	fx.D.Tunnels = upTunnels{}
	fx.PutApp(t, appY, true)
	fx.Grant(t, appY+"/"+core.RightUse)
	env, err := json.Marshal(sign.Envelope{Payload: []byte(`{}`), Sig: make([]byte, 64)})
	if err != nil {
		t.Fatal(err)
	}
	if err := fx.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobLicense, ID: appY, Data: env,
		Updated: testNow}); err != nil {
		t.Fatalf("licence: %v", err)
	}
	e, _ := appEntry(t, fx.call(t, core.ActApps, ""), appY)
	if e.State != core.AppUnlicensed {
		t.Fatalf("state %q", e.State)
	}
}

func TestAppsHoldsTheCoreAppsForAnAdmin(t *testing.T) {
	fx := newFixture(t)
	body := fx.call(t, core.ActApps, "")
	for _, id := range []string{core.AppStore, core.AppUsers} {
		e, ok := appEntry(t, body, id)
		if !ok {
			t.Fatalf("%s is missing", id)
		}
		if !e.Core || e.Node != "" || e.Entry != "" {
			t.Fatalf("%s: %+v", id, e)
		}
	}
}

// TestUIGivesTheBytesOfTheBlobAndWalkUIReadsThem holds both halves: the API
// hands out exactly what it kept, and what it kept is a ui-tar.
func TestUIGivesTheBytesOfTheBlobAndWalkUIReadsThem(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	fx.Grant(t, appY+"/"+core.RightUse)
	tar := uiTar(t, "hallo welt")
	if err := fx.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobUI, ID: appY + "@1.0.0", Data: tar,
		Updated: testNow}); err != nil {
		t.Fatalf("ui: %v", err)
	}
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: core.ActUI,
		Window: wire.MinWindow, Params: json.RawMessage(`{"app":"` + appY + `"}`)})
	got := c.result(t)
	if !bytes.Equal(got, tar) {
		t.Fatalf("%d bytes out, %d in; they are not the same", len(got), len(tar))
	}
	var files []string
	if err := store.WalkUI(bytes.NewReader(got), func(f store.PackFile, body io.Reader) error {
		b, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		if string(b) != "hallo welt" {
			t.Fatalf("body %q", b)
		}
		files = append(files, f.Path)
		return nil
	}); err != nil {
		t.Fatalf("WalkUI: %v", err)
	}
	if len(files) != 1 || files[0] != "ui/index.html" {
		t.Fatalf("files %+v", files)
	}
}

// TestUIOfAnAppWithoutTheRightIsNotFound: the UI of an app follows the same
// right, and is measured on a user who is no admin for the reason above.
func TestUIOfAnAppWithoutTheRightIsNotFound(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	bob := nonAdminSession(t, fx)
	if err := fx.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobUI, ID: appY + "@1.0.0",
		Data: uiTar(t, "x"), Updated: testNow}); err != nil {
		t.Fatalf("ui: %v", err)
	}
	c := bob.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: core.ActUI,
		Window: wire.MinWindow, Params: json.RawMessage(`{"app":"` + appY + `"}`)})
	if cl := c.closed(t); cl.Code != wire.CodeNotFound {
		t.Fatalf("close %+v", cl)
	}
}

// TestUIProofHandsOutEvidenceThatHoldsAgainstTheTrustList measures the way
// out: the API answers ui-proof, and what it answers verifies against the
// trust list. The verifying call below runs IN THIS CASE and not in the API --
// the case that holds the API's own check is
// TestUIProofThatNoLongerHoldsIsNotHandedOut, which bends a byte the API has
// to refuse. This one is the other half: evidence the shell can check itself,
// and the intermediate check that the way to the row is open at all.
func TestUIProofHandsOutEvidenceThatHoldsAgainstTheTrustList(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	fx.Grant(t, appY+"/"+core.RightUse)
	ui := bytes.Repeat([]byte{0x9a}, 32)
	fx.putApproval(t, appY, "1.0.0", ui)
	var p core.UIProof
	if err := json.Unmarshal(fx.call(t, core.ActUIProof, `{"app":"`+appY+`"}`), &p); err != nil {
		t.Fatalf("proof: %v", err)
	}
	got, err := core.VerifyUIProof(p, appY, "1.0.0", fx.List)
	if err != nil {
		t.Fatalf("VerifyUIProof: %v", err)
	}
	if !bytes.Equal(got, ui) {
		t.Fatalf("ui hash %x, not %x", got, ui)
	}
}

// TestUIProofOfABrokenRowIsInternal holds the parser in front of the row: a
// stored proof that does not read never reaches the shell as something it must
// trust.
func TestUIProofOfABrokenRowIsInternal(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	fx.Grant(t, appY+"/"+core.RightUse)
	if err := fx.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobApproval, ID: appY + "@1.0.0",
		Data: []byte(`{"app":"` + appY + `"}`), Updated: testNow}); err != nil {
		t.Fatalf("approval: %v", err)
	}
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: core.ActUIProof,
		Window: wire.MinWindow, Params: json.RawMessage(`{"app":"` + appY + `"}`)})
	if cl := c.closed(t); cl.Code != wire.CodeInternal {
		t.Fatalf("close %+v", cl)
	}
}

// TestUIProofThatNoLongerHoldsIsNotHandedOut is the API's own check on the way
// out (appstore.Stores.Proof). Before the seam was wired, ui-proof only PARSED
// the row, and this case is what measures the difference: one byte of the
// activate task's signature turned over, everything else the same row.
//
// The unbent round first, as the intermediate check -- without it a green run
// could mean that the right, the row or the parser refused and the signature
// was never looked at.
func TestUIProofThatNoLongerHoldsIsNotHandedOut(t *testing.T) {
	fx := newFixture(t)
	fx.PutApp(t, appY, true)
	fx.Grant(t, appY+"/"+core.RightUse)
	ui := bytes.Repeat([]byte{0x9a}, 32)
	fx.putApproval(t, appY, "1.0.0", ui)
	var p core.UIProof
	if err := json.Unmarshal(fx.call(t, core.ActUIProof, `{"app":"`+appY+`"}`), &p); err != nil {
		t.Fatalf("the unbent proof did not come back: %v", err)
	}
	if p.App != appY || p.Version != "1.0.0" {
		t.Fatalf("proof %+v", p)
	}
	fx.putApprovalAs(t, appY, "1.0.0", ui, func(p *core.UIProof) { p.Task.Sig[0] ^= 1 })
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI,
		Action: core.ActUIProof, Window: wire.MinWindow,
		Params: json.RawMessage(`{"app":"` + appY + `"}`)})
	if cl := c.closed(t); cl.Code != wire.CodeInternal {
		t.Fatalf("the API handed out a proof that does not hold: close %+v", cl)
	}
}

// --- TOTP ------------------------------------------------------------------

func TestTOTPGoesOnWithTheCodeOfTheSecretAndOffAgain(t *testing.T) {
	fx := newFixture(t)
	var begin core.TOTPBegin
	if err := json.Unmarshal(fx.call(t, core.ActTOTPBegin, ""), &begin); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if begin.Secret == "" || begin.URI == "" {
		t.Fatalf("begin %+v", begin)
	}
	// Nothing is on yet: a user who walks away keeps what they had.
	if fx.totpOf(t) != nil {
		t.Fatal("totp-begin wrote the secret into the row")
	}
	fx.call(t, core.ActTOTPConfirm, `{"code":"`+totpCode(t, begin.Secret)+`"}`)
	if fx.totpOf(t) == nil {
		t.Fatal("totp-confirm did not switch TOTP on")
	}
	fx.call(t, core.ActTOTPOff, `{"code":"`+totpCode(t, begin.Secret)+`"}`)
	if fx.totpOf(t) != nil {
		t.Fatal("totp-off did not switch TOTP off")
	}
}

func TestAWrongCodeChangesNothing(t *testing.T) {
	fx := newFixture(t)
	var begin core.TOTPBegin
	if err := json.Unmarshal(fx.call(t, core.ActTOTPBegin, ""), &begin); err != nil {
		t.Fatalf("begin: %v", err)
	}
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI,
		Action: core.ActTOTPConfirm, Window: wire.MinWindow, Params: json.RawMessage(`{"code":"000000"}`)})
	if cl := c.closed(t); cl.Code != wire.CodeForbidden {
		t.Fatalf("close %+v", cl)
	}
	if fx.totpOf(t) != nil {
		t.Fatal("a wrong code switched TOTP on")
	}
}

// TestASecondCodeForTheSameSecretIsRefused holds that the pending secret is
// spent: a code may be tried against one secret once, or six digits would be
// guessable at leisure.
func TestASecondCodeForTheSameSecretIsRefused(t *testing.T) {
	fx := newFixture(t)
	var begin core.TOTPBegin
	if err := json.Unmarshal(fx.call(t, core.ActTOTPBegin, ""), &begin); err != nil {
		t.Fatalf("begin: %v", err)
	}
	code := totpCode(t, begin.Secret)
	fx.call(t, core.ActTOTPConfirm, `{"code":"`+code+`"}`)
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI,
		Action: core.ActTOTPConfirm, Window: wire.MinWindow,
		Params: json.RawMessage(`{"code":"` + code + `"}`)})
	if cl := c.closed(t); cl.Code != wire.CodeNotFound {
		t.Fatalf("close %+v", cl)
	}
}

func TestTOTPOffWithoutTheFactorIsRefused(t *testing.T) {
	fx := newFixture(t)
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI,
		Action: core.ActTOTPOff, Window: wire.MinWindow, Params: json.RawMessage(`{"code":"123456"}`)})
	if cl := c.closed(t); cl.Code != wire.CodeForbidden {
		t.Fatalf("close %+v", cl)
	}
}

// --- helpers ---------------------------------------------------------------

// totpOf returns the sealed secret of the session's user, nil for none.
func (h *harness) totpOf(t *testing.T) []byte {
	t.Helper()
	row, err := h.S.DB.UserGet(ctx, h.AdminID)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	if len(row.TOTP) == 0 {
		return nil
	}
	return row.TOTP
}

// totpCode is the code of secret at the fixture's time, as an authenticator app
// would show it.
func totpCode(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCodeCustom(secret, time.Unix(testNow, 0).UTC(), totp.ValidateOpts{
		Period: session.TOTPPeriod, Skew: 1, Digits: otp.Digits(session.TOTPDigits),
		Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	return code
}

// putApproval writes the UI proof of an uploaded app: the activate task of
// this session's admin with the client certificate and the proof over it, as
// core.VerifyUIProof checks them.
func (h *harness) putApproval(t *testing.T, app, version string, ui []byte) {
	t.Helper()
	h.putApprovalAs(t, app, version, ui, nil)
}

// putApprovalAs is putApproval with one hand on the proof before it is
// written: bend may turn a byte over, so a case can measure what the API does
// with a row that no longer holds. A row written past KeepProof on purpose --
// KeepProof verifies before it writes, and the point here is the check on the
// way OUT.
func (h *harness) putApprovalAs(t *testing.T, app, version string, ui []byte,
	bend func(*core.UIProof)) {
	t.Helper()
	params, err := json.Marshal(core.ActivateParams{App: app, Version: version,
		SHA256: bytes.Repeat([]byte{0x11}, 32), UI: ui, Store: core.LocalStore})
	if err != nil {
		t.Fatal(err)
	}
	env := h.Task(t, task.Task{ID: taskID(0x71), Node: ctlNode, App: core.AppStore,
		Service: core.ServiceController, Action: core.ActActivate, Params: params})
	pr := h.Admin.Prove(sign.PurposeClientCert, h.cert)
	p := core.UIProof{App: app, Version: version, Store: core.LocalStore,
		Task: env, Client: enc.Bytes(h.cert), Proof: &pr}
	if bend != nil {
		bend(&p)
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobApproval, ID: app + "@" + version,
		Data: data, Updated: testNow}); err != nil {
		t.Fatalf("approval: %v", err)
	}
}

// uiTar is a ui-tar with one file ui/index.html, written by the one writer
// that keeps the rules of store.WalkUI.
func uiTar(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := store.NewUIWriter(&buf)
	if err := w.File("ui/index.html", int64(len(body)), bytes.NewReader([]byte(body))); err != nil {
		t.Fatalf("ui file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("ui close: %v", err)
	}
	return buf.Bytes()
}

// appEntry finds the entry of an app in the result of ngc.desktop/apps.
func appEntry(t *testing.T, body []byte, id string) (core.DesktopApp, bool) {
	t.Helper()
	var list []core.DesktopApp
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("apps: %v", err)
	}
	for _, e := range list {
		if e.ID == id {
			return e, true
		}
	}
	return core.DesktopApp{}, false
}
