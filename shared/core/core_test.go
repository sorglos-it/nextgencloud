package core

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/internal/golden"
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

var update = flag.Bool("update", false, "rewrite shared/testdata")

func TestTable(t *testing.T) {
	seen := map[string]bool{}
	for _, a := range All() {
		key := a.App + "/" + a.Service + "/" + a.Name
		if seen[key] || !wire.CoreApp(a.App) || !wire.ValidName(a.Service) || !wire.ValidName(a.Name) {
			t.Errorf("%s: duplicate or invalid", key)
		}
		seen[key] = true
		if a.Root && a.Service != ServiceController {
			t.Errorf("%s: root tasks run on controllers", key)
		}
		if a.App != AppDesktop && !a.Admin {
			t.Errorf("%s: only admins", key)
		}
		if got, ok := Lookup(a.App, a.Service, a.Name); !ok || got.Name != a.Name {
			t.Errorf("%s: lookup", key)
		}
		// Only an opener that sends data before its EOF has a bound for it.
		if (a.MaxIn > 0) != (a.Flow == FlowIn || a.Flow == FlowTransfer) || a.MaxIn < 0 {
			t.Errorf("%s: MaxIn %d with flow %s", key, a.MaxIn, a.Flow)
		}
	}
	if _, ok := Lookup(AppStore, ServiceAPI, ActActivate); ok {
		t.Error("activate belongs to the controller service")
	}

	// Every action whose opener sends data, with its bound.
	maxIn := map[string]int64{
		AppUsers + "/" + ServiceAPI + "/" + ActTrust:           6 << 20,
		AppUsers + "/" + ServiceController + "/" + ActTrust:    session.MaxChain,
		AppStore + "/" + ServiceAPI + "/" + ActUpload:          store.MaxPackage,
		AppStore + "/" + ServiceAPI + "/" + ActLicense:         16 << 10,
		AppStore + "/" + ServiceController + "/" + ActActivate: 4 + MaxTransferHeader + store.MaxPackage,
		AppStore + "/" + ServiceController + "/" + ActStage:    4 + MaxTransferHeader + store.MaxPackage,
	}
	for _, a := range All() {
		key := a.App + "/" + a.Service + "/" + a.Name
		if want, ok := maxIn[key]; ok != (a.MaxIn > 0) || a.MaxIn != want {
			t.Errorf("%s: MaxIn %d, want %d", key, a.MaxIn, want)
		}
	}

	// The actions the API may open without a user, and no others.
	without := map[string]bool{
		AppUsers + "/" + ServiceController + "/" + ActTrust:   true,
		AppStore + "/" + ServiceController + "/" + ActStage:   true,
		AppStore + "/" + ServiceController + "/" + ActInstall: true,
	}
	n := 0
	for _, a := range All() {
		key := a.App + "/" + a.Service + "/" + a.Name
		if WithoutUser(a) != without[key] {
			t.Errorf("%s: WithoutUser %v", key, WithoutUser(a))
		}
		if without[key] {
			n++
		}
	}
	if n != len(without) {
		t.Errorf("%d of the %d actions without a user exist", n, len(without))
	}

	// Who opens: the shell every action of service api and the root tasks on
	// controllers, each with a signed task; stage and ngc.users/controller/trust
	// only the API, without a user. Every action has someone who opens it.
	shellRoot := map[string]bool{
		AppStore + "/" + ServiceController + "/" + ActActivate:   true,
		AppStore + "/" + ServiceController + "/" + ActDeactivate: true,
		AppStore + "/" + ServiceController + "/" + ActUninstall:  true,
		AppStore + "/" + ServiceController + "/" + ActDeleteData: true,
		AppStore + "/" + ServiceController + "/" + ActInstall:    true,
	}
	n = 0
	for _, a := range All() {
		key := a.App + "/" + a.Service + "/" + a.Name
		if want := a.Service == ServiceAPI || shellRoot[key]; ShellOpens(a) != want {
			t.Errorf("%s: ShellOpens %v", key, ShellOpens(a))
		}
		if !ShellOpens(a) && !WithoutUser(a) {
			t.Errorf("%s: nobody opens it", key)
		}
		if shellRoot[key] {
			n++
		}
	}
	if n != len(shellRoot) {
		t.Errorf("%d of the %d root tasks the shell opens exist", n, len(shellRoot))
	}
	for _, a := range []Action{action(t, AppStore, ServiceController, ActStage), action(t, AppUsers, ServiceController, ActTrust)} {
		if ShellOpens(a) {
			t.Errorf("%s/%s/%s: the shell must not open it", a.App, a.Service, a.Name)
		}
	}

	join := action(t, AppUsers, ServiceAPI, ActJoinCode)
	if !join.Admin || join.Root || !join.Changes || join.Flow != FlowCall || join.Params != nil {
		t.Errorf("join-code: %+v", join)
	}
	pendingAPI := action(t, AppUsers, ServiceAPI, ActPendingAPI)
	if !pendingAPI.Admin || pendingAPI.Root || pendingAPI.Changes || pendingAPI.Flow != FlowCall || pendingAPI.Params != nil || pendingAPI.MaxIn != 0 {
		t.Errorf("pending-api: %+v", pendingAPI)
	}
	uiProof := action(t, AppDesktop, ServiceAPI, ActUIProof)
	if uiProof.Admin || uiProof.Root || uiProof.Changes || uiProof.Flow != FlowCall || uiProof.Params == nil || uiProof.MaxIn != 0 {
		t.Errorf("ui-proof: %+v", uiProof)
	} else if _, ok := uiProof.Params().(*AppParams); !ok {
		t.Errorf("ui-proof takes %T", uiProof.Params())
	}
	ctlTrust := action(t, AppUsers, ServiceController, ActTrust)
	if !ctlTrust.Admin || ctlTrust.Root || !ctlTrust.Changes || ctlTrust.Flow != FlowIn || ctlTrust.Params != nil {
		t.Errorf("ngc.users/controller/trust: %+v", ctlTrust)
	}
	if LocalStore != trust.StoreLocal {
		t.Errorf("LocalStore %q", LocalStore)
	}
}

func action(t *testing.T, app, service, name string) Action {
	t.Helper()
	a, ok := Lookup(app, service, name)
	if !ok {
		t.Fatalf("%s/%s/%s missing", app, service, name)
	}
	return a
}

// activateParams returns valid params of ngc.store/controller/activate with
// extra members appended.
func activateParams(extra string) json.RawMessage {
	sum := enc.B64.EncodeToString(bytes.Repeat([]byte{1}, 32))
	ui := enc.B64.EncodeToString(bytes.Repeat([]byte{6}, 32))
	return json.RawMessage(`{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","ui":"` + ui + `","store":"global"` + extra + `}`)
}

func TestParseParams(t *testing.T) {
	sum := enc.B64.EncodeToString(bytes.Repeat([]byte{1}, 32))
	ui := enc.B64.EncodeToString(bytes.Repeat([]byte{6}, 32))
	ui31 := enc.B64.EncodeToString(bytes.Repeat([]byte{6}, 31))
	good := []struct{ app, service, name, params string }{
		{AppDesktop, ServiceAPI, ActMe, ``},
		{AppDesktop, ServiceAPI, ActMe, `{}`},
		{AppDesktop, ServiceAPI, ActMe, `{ }`},
		{AppDesktop, ServiceAPI, ActUI, `{"app":"org.sorglos.terminal"}`},
		{AppDesktop, ServiceAPI, ActUIProof, `{"app":"org.sorglos.terminal"}`},
		{AppDesktop, ServiceAPI, ActTOTPConfirm, `{"code":"123456"}`},
		{AppUsers, ServiceAPI, ActSetRights, `{"role":"user","rights":["org.sorglos.calculator/use","org.sorglos.terminal/shell"]}`},
		{AppUsers, ServiceAPI, ActSetRights, `{"role":"user","rights":[]}`},
		{AppUsers, ServiceAPI, ActSetRights, `{"role":"user"}`},
		{AppUsers, ServiceAPI, ActLog, `{"limit":100}`},
		{AppStore, ServiceAPI, ActSetUpdate, `{"target":"ngc-api","mode":"auto","window":"02:00-04:00"}`},
		{AppStore, ServiceController, ActActivate, `{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","ui":"` + ui + `","store":"global","grants":["network"]}`},
		{AppStore, ServiceController, ActActivate, `{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","ui":"` + ui + `","store":"local","grants":["root","network"]}`},
		{AppStore, ServiceController, ActUninstall, `{"app":"org.sorglos.terminal","keepData":true}`},
		{AppStore, ServiceController, ActInstall, `{"target":"system"}`},
		{AppStore, ServiceController, ActInstall, `{"target":"ngc-controller","version":"1.1.0","sha256":"` + sum + `"}`},
	}
	for _, c := range good {
		if _, err := ParseParams(action(t, c.app, c.service, c.name), json.RawMessage(c.params)); err != nil {
			t.Errorf("%s %s: %v", c.name, c.params, err)
		}
	}
	bad := []struct{ app, service, name, params string }{
		{AppDesktop, ServiceAPI, ActMe, `{"x":1}`},
		{AppDesktop, ServiceAPI, ActMe, `[]`},
		{AppDesktop, ServiceAPI, ActMe, `null`},
		{AppDesktop, ServiceAPI, ActMe, `{} {}`},
		{AppDesktop, ServiceAPI, ActUI, ``},
		{AppDesktop, ServiceAPI, ActUI, `{"app":"ngc.users"}`},
		{AppDesktop, ServiceAPI, ActUI, `{"app":null}`},
		{AppDesktop, ServiceAPI, ActUI, `{"app":"org.sorglos.terminal","app":"org.sorglos.mail"}`},
		{AppDesktop, ServiceAPI, ActUI, `{"App":"org.sorglos.terminal"}`},
		{AppDesktop, ServiceAPI, ActUIProof, ``},
		{AppDesktop, ServiceAPI, ActUIProof, `{"app":"ngc.store"}`},
		{AppDesktop, ServiceAPI, ActTOTPConfirm, `{"code":"12345"}`},
		{AppUsers, ServiceAPI, ActSetRights, `{"role":"user","rights":["everything"]}`},
		{AppUsers, ServiceAPI, ActSetRights, `{"role":"user","rights":["org.sorglos.terminal/use","org.sorglos.terminal/use"]}`},
		{AppUsers, ServiceAPI, ActSetRights, `{"role":"user","rights":null}`},
		{AppUsers, ServiceAPI, ActLog, `{"limit":0}`},
		{AppUsers, ServiceAPI, ActLog, `{"limit":501}`},
		{AppStore, ServiceAPI, ActSetUpdate, `{"target":"ngc-api","mode":"always"}`},
		{AppStore, ServiceController, ActActivate, `{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","ui":"` + ui + `","store":"global","grants":["host"]}`},
		{AppStore, ServiceController, ActActivate, `{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","ui":"` + ui + `","store":"global","grants":["root","root"]}`},
		{AppStore, ServiceController, ActActivate, `{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","ui":"` + ui + `","store":"global","grants":["root","network","root"]}`},
		// The UI hash is signed with every activation.
		{AppStore, ServiceController, ActActivate, `{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","store":"global"}`},
		{AppStore, ServiceController, ActActivate, `{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","ui":"` + ui31 + `","store":"global"}`},
		{AppStore, ServiceController, ActInstall, `{"target":"system","version":"1.0.0"}`},
		{AppStore, ServiceController, ActInstall, `{"target":"ngc-controller","version":"1.1.0"}`},
		{AppStore, ServiceController, ActInstall, `{"target":"ngc-controller","version":"1.1.0","sha256":"` + sum + `","size":1}`},
	}
	for _, c := range bad {
		if _, err := ParseParams(action(t, c.app, c.service, c.name), json.RawMessage(c.params)); err != ErrParams {
			t.Errorf("%s %s: got %v, want %v", c.name, c.params, err, ErrParams)
		}
	}

	// The log cursor, one rule per case.
	log := action(t, AppUsers, ServiceAPI, ActLog)
	const id = "000102030405060708090a0b0c0d0e0f"
	for _, c := range []struct {
		name, params string
		valid        bool
	}{
		{"log cursor time only", `{"limit":10,"beforeTime":1790000000}`, true},
		{"log cursor time and id", `{"limit":10,"beforeTime":1790000000,"beforeId":"` + id + `"}`, true},
		{"log cursor id without time", `{"limit":10,"beforeId":"` + id + `"}`, false},
		{"log cursor id not hex", `{"limit":10,"beforeTime":1790000000,"beforeId":"000102030405060708090a0b0c0d0e0g"}`, false},
		{"log cursor id upper case", `{"limit":10,"beforeTime":1790000000,"beforeId":"000102030405060708090A0B0C0D0E0F"}`, false},
		{"log cursor id of 31 characters", `{"limit":10,"beforeTime":1790000000,"beforeId":"` + id[1:] + `"}`, false},
		{"log cursor id of 33 characters", `{"limit":10,"beforeTime":1790000000,"beforeId":"` + id + `0"}`, false},
		{"log cursor time negative", `{"limit":10,"beforeTime":-1}`, false},
		{"log cursor time beyond 2^53-1", `{"limit":10,"beforeTime":9007199254740992}`, false},
		{"log cursor under the old name", `{"limit":10,"before":1790000000}`, false},
	} {
		if _, err := ParseParams(log, json.RawMessage(c.params)); (err == nil) != c.valid {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestValidLogID(t *testing.T) {
	for _, s := range []string{"000102030405060708090a0b0c0d0e0f", "ffffffffffffffffffffffffffffffff"} {
		if !ValidLogID(s) {
			t.Errorf("%s rejected", s)
		}
	}
	for _, s := range []string{"", "000102030405060708090a0b0c0d0e0", "000102030405060708090a0b0c0d0e0f0",
		"000102030405060708090A0B0C0D0E0F", "000102030405060708090a0b0c0d0e0g", "AAECAwQFBgcICQoLDA0ODw",
		"000102030405060708090a0b0c0d0e0f" + "\n"} {
		if ValidLogID(s) {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestParseNotice(t *testing.T) {
	umlauts := func(n int) string { return strings.Repeat("ä", n) } // two bytes each
	good := []struct{ name, body string }{
		{"warning about an app", `{"level":"warn","text":"Die Lizenz für Terminal läuft in 3 Tagen ab.","app":"org.sorglos.terminal"}`},
		{"info without app", `{"level":"info","text":"Update installiert."}`},
		{"error", `{"level":"error","text":"x"}`},
		{"text of MaxMessageText bytes", `{"level":"info","text":"` + umlauts(wire.MaxMessageText/2) + `"}`},
	}
	for _, c := range good {
		if _, err := ParseNotice([]byte(c.body)); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	bad := []struct{ name, body string }{
		{"unknown level", `{"level":"debug","text":"x"}`},
		{"no level", `{"text":"x"}`},
		{"empty text", `{"level":"info","text":""}`},
		{"text over MaxMessageText bytes", `{"level":"info","text":"` + umlauts(wire.MaxMessageText/2) + `a"}`},
		{"text with a line break", `{"level":"info","text":"a\nb"}`},
		{"text with a control character", `{"level":"info","text":"a\u0007b"}`},
		{"core app", `{"level":"info","text":"x","app":"ngc.store"}`},
		{"invalid app id", `{"level":"info","text":"x","app":"Terminal"}`},
		{"unknown member", `{"level":"info","text":"x","user":"admin"}`},
		{"member in other case", `{"Level":"info","text":"x"}`},
		{"duplicate member", `{"level":"info","level":"warn","text":"x"}`},
		{"null", `{"level":"info","text":"x","app":null}`},
		{"not an object", `["info","x"]`},
		{"trailing data", `{"level":"info","text":"x"} {}`},
		{"over MaxNotice", `{"level":"info","text":"x"}` + strings.Repeat(" ", MaxNotice)},
	}
	for _, c := range bad {
		if _, err := ParseNotice([]byte(c.body)); err != ErrNotice {
			t.Errorf("%s: got %v, want %v", c.name, err, ErrNotice)
		}
	}
}

func TestLicenseOf(t *testing.T) {
	const issued = 1790000000
	l := &store.License{Issued: issued, Expires: issued + 86400, Grace: 3600}
	cases := []struct {
		name string
		l    *store.License
		now  int64
		want string
	}{
		{"free app", nil, issued, LicenseNone},
		{"valid", l, issued + 1, LicenseValid},
		{"grace", l, issued + 86400, LicenseGrace},
		{"expired", l, issued + 86400 + 3600, LicenseExpired},
	}
	for _, c := range cases {
		if got := LicenseOf(c.l, c.now); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestResultsWithoutNull: a result the API fills with nothing is sent
// without null, which every strict decoder refuses, and reads back the same.
func TestResultsWithoutNull(t *testing.T) {
	budget := enc.Budget{Size: 1 << 20, Items: map[string]int{"roles": trust.MaxRoles, "actions": 64, "rights": maxRights}}
	for _, v := range []any{&Me{}, &DesktopApp{}, &UserInfo{}, &RoleRights{}, &LogEntry{}, &Pending{}, &SourceInfo{},
		&CatalogResult{}, &InstalledApp{}, &UploadResult{}, &LicenseResult{}, &InstallKey{}, &UpdateInfo{}, &TrustResult{},
		&PairCodeResult{}, &TOTPBegin{}, &Notice{}, &Transfer{}, &UIProof{}, &PendingAPI{}} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		// Empty bytes read back as empty, not nil, so the JSON is compared.
		back := reflect.New(reflect.TypeOf(v).Elem()).Interface()
		err = budget.Decode(b, back)
		again, _ := json.Marshal(back)
		if err != nil || !bytes.Equal(again, b) {
			t.Errorf("%T: %s does not read back: %s, %v", v, b, again, err)
		}
	}
}

// TestNoticeBoundsAllocation and TestTransferBoundsAllocation feed the
// parsers a document of their full size with an array or too many members
// where they allow none: refusing it must allocate next to nothing beyond
// the document itself.
func TestNoticeBoundsAllocation(t *testing.T) {
	for _, in := range [][]byte{hostile(`{"text":[`, `{}`, `]}`, MaxNotice), members(`{`, `}`, MaxNotice)} {
		var err error
		n := allocated(func() { _, err = ParseNotice(in) })
		t.Logf("%d bytes allocated", n)
		if n > 64<<10 || err != ErrNotice {
			t.Errorf("%d bytes allocated, %v", n, err)
		}
	}
}

func TestTransferBoundsAllocation(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
	}{
		{"payload as an array", hostile(`{"catalog":{"payload":[`, `{}`, `]}}`, MaxTransferHeader)},
		{"members of the catalog", members(`{"catalog":{`, `}}`, MaxTransferHeader)},
	}
	for _, c := range cases {
		var err error
		n := allocated(func() { _, err = parseTransfer(c.in) })
		t.Logf("%s: %d bytes allocated", c.name, n)
		if n > 64<<10 || err != ErrTransfer {
			t.Errorf("%s: %d bytes allocated, %v", c.name, n, err)
		}
		// ReadTransfer holds the header itself, and next to nothing besides.
		framed := binary.BigEndian.AppendUint32(nil, uint32(len(c.in)))
		framed = append(framed, c.in...)
		n = allocated(func() { _, err = ReadTransfer(bytes.NewReader(framed)) })
		t.Logf("%s, ReadTransfer: %d bytes allocated", c.name, n)
		if n > uint64(len(c.in))+64<<10 || err != ErrTransfer {
			t.Errorf("%s, ReadTransfer: %d bytes allocated, %v", c.name, n, err)
		}
	}
}

// TestRoleRightsWithoutRights: a role without rights is legitimate, so it
// is sent without the member instead of null, and read back.
func TestRoleRightsWithoutRights(t *testing.T) {
	b, err := json.Marshal(RoleRights{Role: "user"})
	if err != nil || string(b) != `{"role":"user"}` {
		t.Fatalf("got %s, %v", b, err)
	}
	p, err := ParseParams(action(t, AppUsers, ServiceAPI, ActSetRights), b)
	if err != nil || p.(*RoleRights).Role != "user" || len(p.(*RoleRights).Rights) != 0 {
		t.Errorf("round trip: %+v, %v", p, err)
	}
}

func TestCritical(t *testing.T) {
	activate := action(t, AppStore, ServiceController, ActActivate)
	uninstall := action(t, AppStore, ServiceController, ActUninstall)
	cases := []struct {
		name string
		a    Action
		p    Params
		want bool
	}{
		{"activate without grants", activate, &ActivateParams{}, false},
		{"activate with root", activate, &ActivateParams{Grants: []string{store.NeedRoot}}, true},
		{"uninstall, keep data", uninstall, &UninstallParams{KeepData: true}, false},
		{"uninstall, delete data", uninstall, &UninstallParams{}, true},
		{"upload", action(t, AppStore, ServiceAPI, ActUpload), nil, true},
		{"delete kept data", action(t, AppStore, ServiceController, ActDeleteData), &AppParams{}, true},
		{"list users", action(t, AppUsers, ServiceAPI, ActList), nil, false},
	}
	for _, c := range cases {
		if got := Critical(c.a, c.p); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestRuleFor(t *testing.T) {
	activate := action(t, AppStore, ServiceController, ActActivate)
	uninstall := action(t, AppStore, ServiceController, ActUninstall)
	upload := action(t, AppStore, ServiceAPI, ActUpload)
	cases := []struct {
		name    string
		a       Action
		params  json.RawMessage
		want    bool
		wantErr error
	}{
		{"activate with grants in the task is critical", activate, activateParams(`,"grants":["root"]`), true, nil},
		{"activate without grants is not", activate, activateParams(``), false, nil},
		{"uninstall without keepData is critical", uninstall, json.RawMessage(`{"app":"org.sorglos.terminal"}`), true, nil},
		{"uninstall with keepData is not", uninstall, json.RawMessage(`{"app":"org.sorglos.terminal","keepData":true}`), false, nil},
		{"invalid params give an error", activate, json.RawMessage(`{"app":"org.sorglos.terminal"}`), false, ErrParams},
		{"params that do not decode give an error", activate, json.RawMessage(`{"app":`), false, ErrParams},
		{"upload is critical without params", upload, nil, true, nil},
		{"activate without params gives an error", activate, nil, false, ErrParams},
		{"uninstall without params gives an error", uninstall, nil, false, ErrParams},
		{"upload is critical with empty params", upload, json.RawMessage(`{}`), true, nil},
		{"params for an action without any give an error", upload, json.RawMessage(`{"app":"org.sorglos.terminal"}`), false, ErrParams},
	}
	for _, c := range cases {
		got, err := RuleFor(c.a).Critical(c.params)
		if got != c.want || err != c.wantErr {
			t.Errorf("%s: got %v, %v; want %v, %v", c.name, got, err, c.want, c.wantErr)
		}
	}
	// nil params ask Critical(a, nil) where params do not decide, and Root
	// and Changes come from the table.
	for _, a := range All() {
		r := RuleFor(a)
		got, err := r.Critical(nil)
		decide := a.Name == ActActivate || a.Name == ActUninstall
		if (decide && err != ErrParams) || (!decide && (err != nil || got != Critical(a, nil))) ||
			r.Root != a.Root || r.Changes != a.Changes {
			t.Errorf("%s/%s/%s: rule %+v, Critical(nil) = %v, %v", a.App, a.Service, a.Name, r, got, err)
		}
		if decide && !a.Root {
			t.Errorf("%s: params decide, so it must be a root task", a.Name)
		}
	}
}

func TestUnattended(t *testing.T) {
	releaseKey := bytes.Repeat([]byte{0x72}, 32)
	storeKey := bytes.Repeat([]byte{0x71}, 32)
	sum := bytes.Repeat([]byte{1}, 32)
	approved := trusttest.Genesis(trusttest.NewSigner(1))
	approved.Auto = []trust.Auto{{Target: trust.AutoController, Key: releaseKey}, {Target: "org.sorglos.terminal", Key: storeKey}, {Target: trust.AutoSystem}}
	noSystem := trusttest.Genesis(trusttest.NewSigner(1))
	noSystem.Auto = []trust.Auto{{Target: trust.AutoController, Key: releaseKey}}
	controller := func(version string) *InstallParams {
		return &InstallParams{Target: trust.AutoController, Version: version, SHA256: sum}
	}
	system := &InstallParams{Target: trust.AutoSystem}
	cases := []struct {
		name      string
		p         *InstallParams
		l         *trust.List
		signer    []byte
		installed string
		want      bool
	}{
		{"approved and newer", controller("1.1.0"), approved, releaseKey, "1.0.0", true},
		{"approved but older", controller("1.1.0"), approved, releaseKey, "1.2.0", false},
		{"approved but same version", controller("1.1.0"), approved, releaseKey, "1.1.0", false},
		{"other key", controller("1.1.0"), approved, storeKey, "1.0.0", false},
		{"nothing installed yet", controller("1.1.0"), approved, releaseKey, "", false},
		{"system approved", system, approved, nil, "", true},
		{"system not approved", system, noSystem, nil, "", false},
		{"app approved for its store key", &InstallParams{Target: "org.sorglos.terminal", Version: "1.1.0", SHA256: sum}, approved, storeKey, "1.0.0", true},
		{"target without approval", &InstallParams{Target: trust.AutoAPI, Version: "1.1.0", SHA256: sum}, approved, releaseKey, "1.0.0", false},
		{"no signer", controller("1.1.0"), approved, nil, "1.0.0", false},
		{"invalid params", &InstallParams{Target: trust.AutoController, Version: "1.1.0"}, approved, releaseKey, "1.0.0", false},
		{"no params", nil, approved, releaseKey, "1.0.0", false},
		{"no list", controller("1.1.0"), nil, releaseKey, "1.0.0", false},
	}
	for _, c := range cases {
		if got := Unattended(c.p, c.l, c.signer, c.installed); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

// testKeyID is a key ID of the form sign.Envelope.Validate asks for.
var testKeyID = sign.KeyID([]byte("test key"))

func testTransfer() Transfer {
	cat := sign.Envelope{Payload: []byte(`{}`), KeyID: testKeyID, Sig: bytes.Repeat([]byte{1}, 64)}
	lic := sign.Envelope{Payload: []byte(`{}`), KeyID: testKeyID, Sig: bytes.Repeat([]byte{2}, 64)}
	return Transfer{Catalog: &cat, Store: "global", License: &lic, Key: bytes.Repeat([]byte{3}, 32), Size: 5000000, SHA256: bytes.Repeat([]byte{4}, 32)}
}

// testRelease is testTransfer with an NGC release instead of a paid app.
func testRelease() Transfer {
	tr := testTransfer()
	rel := sign.Envelope{Payload: []byte(`{}`), KeyID: testKeyID, Sig: bytes.Repeat([]byte{5}, 64)}
	tr.Catalog, tr.Store, tr.License, tr.Key, tr.Release = nil, "", nil, nil, &rel
	return tr
}

func TestTransfer(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteTransfer(&buf, testTransfer()); err != nil {
		t.Fatal(err)
	}
	buf.WriteString("file")
	got, err := ReadTransfer(&buf)
	if err != nil || got.Store != "global" || got.Size != 5000000 {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	if rest, _ := io.ReadAll(&buf); string(rest) != "file" {
		t.Errorf("reader consumed the file: %q", rest)
	}
	for name, mutate := range map[string]func(*Transfer){
		"key without license":   func(tr *Transfer) { tr.License = nil },
		"catalog and release":   func(tr *Transfer) { tr.Release = tr.Catalog },
		"catalog without store": func(tr *Transfer) { tr.Store = "" },
		"short checksum":        func(tr *Transfer) { tr.SHA256 = []byte{1} },
		"too large":             func(tr *Transfer) { tr.Size = store.MaxPackage + 1 },
		"zero size":             func(tr *Transfer) { tr.Size = 0 },
		"invalid store name":    func(tr *Transfer) { tr.Store = "BAD" },
		"short key":             func(tr *Transfer) { tr.Key = []byte{1} },
		// The envelopes inside keep the form sign.ParseEnvelope asks for.
		"catalog with an empty payload":          func(tr *Transfer) { tr.Catalog.Payload = nil },
		"catalog with a key ID of 21 characters": func(tr *Transfer) { tr.Catalog.KeyID = tr.Catalog.KeyID[:21] },
		"license payload over store.MaxLicense":  func(tr *Transfer) { tr.License.Payload = make([]byte, store.MaxLicense+1) },
		"license signature of 65 bytes":          func(tr *Transfer) { tr.License.Sig = append(tr.License.Sig, 0) },
	} {
		tr := testTransfer()
		mutate(&tr)
		if tr.Validate() != ErrTransfer {
			t.Errorf("%s accepted", name)
		}
	}
	release := testRelease()
	if err := release.Validate(); err != nil {
		t.Errorf("release: %v", err)
	}
	release.Release.Sig = release.Release.Sig[:63]
	if release.Validate() != ErrTransfer {
		t.Error("release signature of 63 bytes accepted")
	}
	atLimit := testTransfer()
	atLimit.License.Payload = make([]byte, store.MaxLicense)
	if err := atLimit.Validate(); err != nil {
		t.Errorf("license payload of store.MaxLicense bytes: %v", err)
	}
	// ReadTransfer refuses a header whose envelope WriteTransfer would not
	// write.
	bad := testTransfer()
	bad.Catalog.Payload = nil
	if err := WriteTransfer(io.Discard, bad); err != ErrTransfer {
		t.Errorf("WriteTransfer with an empty catalog: %v", err)
	}
	head, err := json.Marshal(bad)
	if err != nil {
		t.Fatal(err)
	}
	framed := append(binary.BigEndian.AppendUint32(nil, uint32(len(head))), head...)
	if _, err := ReadTransfer(bytes.NewReader(framed)); err != ErrTransfer {
		t.Errorf("ReadTransfer with an empty catalog: %v", err)
	}
	huge := make([]byte, 4)
	binary.BigEndian.PutUint32(huge, MaxTransferHeader+1)
	if _, err := ReadTransfer(bytes.NewReader(huge)); err != ErrTransfer {
		t.Errorf("huge header: %v", err)
	}
}

func TestRights(t *testing.T) {
	for _, r := range []string{"org.sorglos.terminal/use", "org.sorglos.terminal/shell"} {
		if !ValidRight(r) {
			t.Errorf("%s rejected", r)
		}
	}
	for _, r := range []string{"org.sorglos.terminal", "ngc.users/use", "org.sorglos.terminal/Shell", "/use"} {
		if ValidRight(r) {
			t.Errorf("%s accepted", r)
		}
	}
}

// The UI hashes of uiproof.json, one per version, so a reader that takes the
// ui of another entry fails.
var (
	uiCalculator = bytes.Repeat([]byte{0x07}, 32) // org.sorglos.calculator 1.0.0
	uiTerminal   = bytes.Repeat([]byte{0x08}, 32) // org.sorglos.terminal 1.0.0
	uiTerminal11 = bytes.Repeat([]byte{0x09}, 32) // org.sorglos.terminal 1.1.0
	uiNotes      = bytes.Repeat([]byte{0x0A}, 32) // org.example.notes 0.1.0, uploaded
	uiWeather    = bytes.Repeat([]byte{0x0B}, 32) // org.example.weather 2.0.0 of the store other
)

// uiFixture is the trust list of uiproof.json and the keys its proofs are
// made with. The list is version 1: Genesis plus the user alice (role user,
// level off, root nowhere) and the store other, signed by the admin.
type uiFixture struct {
	signed  trust.Signed
	list    *trust.List
	global  ed25519.PrivateKey // key of the global store in Genesis
	other   ed25519.PrivateKey // key of the store other
	sessSig ed25519.PrivateKey // session key of every client certificate
}

func newUIFixture(t testing.TB) uiFixture {
	t.Helper()
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	_, global := trusttest.NodeKeys(0xB1)
	_, other := trusttest.NodeKeys(0xB2)
	l.Roles = append(l.Roles, trust.Role{Name: "user", Level: trust.LevelOff})
	l.Users = append(l.Users, trust.User{ID: trusttest.ID(0x30), Name: "alice", Roles: []string{"user"},
		Keys: []proof.UserKey{trusttest.NewSigner(0x31).Key}})
	l.Stores = append(l.Stores, trust.Store{ID: "other", Name: "Other Store", URL: "https://store.other.example",
		Key: enc.Bytes(other.Public().(ed25519.PublicKey))})
	signed := admin.Sign(t, l)
	v, err := trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(signed)}, []trust.Signed{signed})
	if err != nil {
		t.Fatal(err)
	}
	return uiFixture{signed: signed, list: v.List, global: global, other: other,
		sessSig: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x51}, ed25519.SeedSize))}
}

// uiCatalog is the catalog of the global store in uiproof.json.
func uiCatalog() store.Catalog {
	return store.Catalog{Store: trust.StoreGlobal, Seq: 5, Issued: 1790000000, Apps: []store.Entry{
		{ID: "org.sorglos.calculator", Name: "Rechner", Publisher: "Sorglos IT", Versions: []store.Package{
			{Version: "1.0.0", URL: "org.sorglos.calculator-1.0.0.ngc", Size: 12345, SHA256: bytes.Repeat([]byte{0x01}, 32), UI: uiCalculator},
		}},
		{ID: "org.sorglos.terminal", Name: "Terminal", Publisher: "Sorglos IT", Versions: []store.Package{
			{Version: "1.0.0", URL: "org.sorglos.terminal-1.0.0.ngc", Size: 5000000, SHA256: bytes.Repeat([]byte{0x02}, 32), UI: uiTerminal},
			{Version: "1.1.0", URL: "org.sorglos.terminal-1.1.0.ngc", Size: 5100000, SHA256: bytes.Repeat([]byte{0x03}, 32), UI: uiTerminal11},
		}},
	}}
}

// otherCatalog is the catalog of the store other in uiproof.json.
func otherCatalog() store.Catalog {
	return store.Catalog{Store: "other", Seq: 2, Issued: 1790000000, Apps: []store.Entry{
		{ID: "org.example.weather", Name: "Wetter", Publisher: "Other Store", Versions: []store.Package{
			{Version: "2.0.0", URL: "org.example.weather-2.0.0.ngc", Size: 23456, SHA256: bytes.Repeat([]byte{0x04}, 32), UI: uiWeather},
		}},
	}}
}

// catalog signs c with key as a catalog.
func (f uiFixture) catalog(t testing.TB, c store.Catalog, key ed25519.PrivateKey) *sign.Envelope {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.Sign(key, sign.PurposeCatalog, b)
	if err != nil {
		t.Fatal(err)
	}
	return &env
}

// storeProof is the valid proof of a store app in uiproof.json and core.json:
// org.sorglos.terminal 1.0.0 with the catalog of the global store.
func (f uiFixture) storeProof(t testing.TB) UIProof {
	return UIProof{App: "org.sorglos.terminal", Version: "1.0.0", Store: trust.StoreGlobal, Catalog: f.catalog(t, uiCatalog(), f.global)}
}

// approval returns a proof of the uploaded app org.example.notes 0.1.0: a
// client certificate of user with the fixture's session key (issued
// 1790000000, valid for an hour), the proof of s over it, and the task of
// ngc.store/controller/<action> on ctl-1 with params, signed with the session
// key and bound to that certificate.
func (f uiFixture) approval(t testing.TB, user string, s trusttest.Signer, action string, params any) UIProof {
	t.Helper()
	dh, _ := trusttest.NodeKeys(0x52)
	client, err := json.Marshal(session.ClientCert{Installation: f.list.Installation, User: user,
		Nonce: bytes.Repeat([]byte{0x53}, session.NonceSize), Sig: enc.Bytes(f.sessSig.Public().(ed25519.PublicKey)),
		DH: dh.Public, Issued: 1790000000, Expires: 1790003600})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(task.Task{ID: bytes.Repeat([]byte{0x61}, task.IDSize), Installation: f.list.Installation,
		Cert: session.CertHash(client), Node: "ctl-1", App: AppStore, Service: ServiceController, Action: action,
		Params: raw, Issued: 1790000000, Expires: 1790000060})
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.Sign(f.sessSig, sign.PurposeTask, b)
	if err != nil {
		t.Fatal(err)
	}
	p := s.Prove(sign.PurposeClientCert, client)
	return UIProof{App: "org.example.notes", Version: "0.1.0", Store: LocalStore, Task: &env, Client: client, Proof: &p}
}

// uiProofCase is a case of uiproof.json with what Go makes of it.
type uiProofCase struct {
	name, app, version string
	proof              UIProof
	ui                 []byte // the hash a valid case proves
	parses             bool   // ParseUIProof accepts the proof
	want               error  // what verifyUIProof returns; nil for a valid case
}

// uiProofCases returns the cases of uiproof.json. Each invalid case changes
// one thing of a valid one.
func uiProofCases(t testing.TB, f uiFixture) []uiProofCase {
	t.Helper()
	const terminal, notes = "org.sorglos.terminal", "org.example.notes"
	admin, passkeyAdmin := trusttest.NewSigner(1), trusttest.NewPasskey(trusttest.GenesisPasskeyFill)
	passkeyID, adminID, aliceID := trusttest.ID(0x20), trusttest.ID(0x21), trusttest.ID(0x30)
	activate := func(app, version, st string) ActivateParams {
		return ActivateParams{App: app, Version: version, SHA256: bytes.Repeat([]byte{0x05}, 32), UI: uiNotes, Store: st}
	}
	withCatalog := func(app, version string, env *sign.Envelope) UIProof {
		return UIProof{App: app, Version: version, Store: trust.StoreGlobal, Catalog: env}
	}
	global := f.storeProof(t).Catalog
	byAdmin := f.approval(t, adminID, admin, ActActivate, activate(notes, "0.1.0", LocalStore))

	third := uiCatalog()
	third.Store = "third"
	_, thirdKey := trusttest.NodeKeys(0xB3)
	ofThird := withCatalog(terminal, "1.0.0", f.catalog(t, third, thirdKey))
	ofThird.Store = "third"

	flipped := *byAdmin.Task
	flipped.Sig = bytes.Clone(flipped.Sig)
	flipped.Sig[0] ^= 1
	withFlipped := byAdmin
	withFlipped.Task = &flipped

	// The signatures a verifier must check, not only the key IDs.
	badCatalog := *global
	badCatalog.Sig = bytes.Clone(badCatalog.Sig)
	badCatalog.Sig[0] ^= 1
	badCert := *byAdmin.Proof
	badCert.Sig = bytes.Clone(badCert.Sig)
	badCert.Sig[0] ^= 1
	withBadCert := byAdmin
	withBadCert.Proof = &badCert

	together := withCatalog(terminal, "1.0.0", global)
	together.Task, together.Client, together.Proof = byAdmin.Task, byAdmin.Client, byAdmin.Proof

	// The admin's activation of a store app, offered instead of its catalog.
	storeTask := f.approval(t, adminID, admin, ActActivate, ActivateParams{App: terminal, Version: "1.0.0",
		SHA256: bytes.Repeat([]byte{0x02}, 32), UI: uiTerminal, Store: trust.StoreGlobal})
	storeTask.App, storeTask.Version, storeTask.Store = terminal, "1.0.0", trust.StoreGlobal

	return []uiProofCase{
		{"store app from the global store", terminal, "1.0.0", withCatalog(terminal, "1.0.0", global), uiTerminal, true, nil},
		{"store app from another listed store", "org.example.weather", "2.0.0", UIProof{App: "org.example.weather", Version: "2.0.0",
			Store: "other", Catalog: f.catalog(t, otherCatalog(), f.other)}, uiWeather, true, nil},
		{"uploaded app activated by the admin", notes, "0.1.0", byAdmin, uiNotes, true, nil},
		{"uploaded app activated by the passkey admin", notes, "0.1.0",
			f.approval(t, passkeyID, passkeyAdmin, ActActivate, activate(notes, "0.1.0", LocalStore)), uiNotes, true, nil},

		{"catalog of a store not in the list", terminal, "1.0.0", ofThird, nil, true, errProofStore},
		{"catalog signed by another key", terminal, "1.0.0",
			withCatalog(terminal, "1.0.0", f.catalog(t, uiCatalog(), f.other)), nil, true, sign.ErrWrongKey},
		{"flipped catalog signature", terminal, "1.0.0", withCatalog(terminal, "1.0.0", &badCatalog), nil, true, sign.ErrBadSignature},
		{"version not in the catalog", terminal, "1.2.0", withCatalog(terminal, "1.2.0", global), nil, true, errProofListed},
		{"app not in the catalog", "org.sorglos.mail", "1.0.0", withCatalog("org.sorglos.mail", "1.0.0", global), nil, true, errProofListed},
		// The proof holds, but for what the shell did not ask for.
		{"store app proof for another version than asked", terminal, "1.2.0", withCatalog(terminal, "1.0.0", global), nil, true, errProofForm},
		{"store app proof for another app than asked", "org.sorglos.mail", "1.0.0", withCatalog(terminal, "1.0.0", global), nil, true, errProofForm},
		{"uploaded app activated by a user without root on the node", notes, "0.1.0",
			f.approval(t, aliceID, trusttest.NewSigner(0x31), ActActivate, activate(notes, "0.1.0", LocalStore)), nil, true, errProofRoot},
		{"task for another app", notes, "0.1.0",
			f.approval(t, adminID, admin, ActActivate, activate("org.example.other", "0.1.0", LocalStore)), nil, true, errProofTask},
		{"task for another version", notes, "0.1.0",
			f.approval(t, adminID, admin, ActActivate, activate(notes, "0.2.0", LocalStore)), nil, true, errProofTask},
		{"task that is no activate", notes, "0.1.0", // params of an activate, so only the action tells
			f.approval(t, adminID, admin, ActDeactivate, activate(notes, "0.1.0", LocalStore)), nil, true, errProofTask},
		{"params naming a store", notes, "0.1.0",
			f.approval(t, adminID, admin, ActActivate, activate(notes, "0.1.0", trust.StoreGlobal)), nil, true, errProofTask},
		{"certificate proof by a key not in the list", notes, "0.1.0",
			f.approval(t, adminID, trusttest.NewSigner(0x77), ActActivate, activate(notes, "0.1.0", LocalStore)), nil, true, session.ErrUser},
		{"flipped certificate proof", notes, "0.1.0", withBadCert, nil, true, proof.ErrSignature},
		{"flipped task signature", notes, "0.1.0", withFlipped, nil, true, sign.ErrBadSignature},
		{"uploaded app proof for another version than asked", notes, "0.2.0", byAdmin, nil, true, errProofForm},
		{"uploaded app proof for another app than asked", "org.example.other", "0.1.0", byAdmin, nil, true, errProofForm},
		{"catalog and task together", terminal, "1.0.0", together, nil, false, errProofForm},
		{"store app without a catalog", terminal, "1.0.0", storeTask, nil, false, errProofForm},
	}
}

// TestUIProof holds each case of uiproof.json against ParseUIProof and the
// reason verifyUIProof gives, so no case is refused by a rule other than its
// own, then checks the rules of ParseUIProof one by one.
func TestUIProof(t *testing.T) {
	f := newUIFixture(t)
	for _, c := range uiProofCases(t, f) {
		b, err := json.Marshal(c.proof)
		if err != nil {
			t.Fatal(err)
		}
		p, err := ParseUIProof(b)
		if (err == nil) != c.parses || (err != nil && err != ErrUIProof) {
			t.Errorf("%s: ParseUIProof %v", c.name, err)
		}
		if err != nil {
			p = c.proof // as a caller would pass it who skips the parser
		}
		ui, err := verifyUIProof(p, c.app, c.version, f.list)
		if !errors.Is(err, c.want) || (c.want == nil && !bytes.Equal(ui, c.ui)) {
			t.Errorf("%s: got %x, %v; want %v", c.name, ui, err, c.want)
		}
		// VerifyUIProof gives the hash or ErrUIProof alone.
		pub, err := VerifyUIProof(p, c.app, c.version, f.list)
		if (c.want == nil) != (err == nil) || (err != nil && (err != ErrUIProof || pub != nil)) || !bytes.Equal(pub, ui) {
			t.Errorf("%s: VerifyUIProof %x, %v", c.name, pub, err)
		}
	}

	// A valid proof holds only for the app and version it names, and only
	// against a trust list.
	sp := f.storeProof(t)
	for name, check := range map[string]func() ([]byte, error){
		"another version asked for": func() ([]byte, error) { return verifyUIProof(sp, sp.App, "1.1.0", f.list) },
		"another app asked for":     func() ([]byte, error) { return verifyUIProof(sp, "org.sorglos.calculator", sp.Version, f.list) },
		"no trust list":             func() ([]byte, error) { return verifyUIProof(sp, sp.App, sp.Version, nil) },
	} {
		if ui, err := check(); err != errProofForm || ui != nil {
			t.Errorf("%s: %x, %v", name, ui, err)
		}
	}

	// ParseUIProof, one rule per case, from the valid proofs of both kinds.
	storeApp := func(change func(p *UIProof)) UIProof {
		p := f.storeProof(t)
		change(&p)
		return p
	}
	uploaded := func(change func(p *UIProof)) UIProof {
		p := f.approval(t, trusttest.ID(0x21), trusttest.NewSigner(1), ActActivate,
			ActivateParams{App: "org.example.notes", Version: "0.1.0", SHA256: bytes.Repeat([]byte{0x05}, 32), UI: uiNotes, Store: LocalStore})
		change(&p)
		return p
	}
	same := func(*UIProof) {}
	bigTask, err := sign.Sign(f.sessSig, sign.PurposeTask, bytes.Repeat([]byte(" "), task.MaxTask+1))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		p     UIProof
		valid bool
	}{
		{"store app", storeApp(same), true},
		{"uploaded app", uploaded(same), true},
		{"client of session.MaxCert bytes", uploaded(func(p *UIProof) { p.Client = bytes.Repeat([]byte{'x'}, session.MaxCert) }), true},
		{"catalog of another store than global", storeApp(func(p *UIProof) { p.Store = "other" }), true},

		{"both kinds together", storeApp(func(p *UIProof) {
			u := uploaded(same)
			p.Task, p.Client, p.Proof = u.Task, u.Client, u.Proof
		}), false},
		{"catalog and a task", storeApp(func(p *UIProof) { p.Task = uploaded(same).Task }), false},
		{"catalog and a client certificate", storeApp(func(p *UIProof) { p.Client = uploaded(same).Client }), false},
		{"catalog and a proof", storeApp(func(p *UIProof) { p.Proof = uploaded(same).Proof }), false},
		{"neither kind", storeApp(func(p *UIProof) { p.Catalog = nil }), false},
		{"store local with a catalog", storeApp(func(p *UIProof) { p.Store = LocalStore }), false},
		{"task with a store other than local", uploaded(func(p *UIProof) { p.Store = trust.StoreGlobal }), false},
		{"client over session.MaxCert bytes", uploaded(func(p *UIProof) { p.Client = bytes.Repeat([]byte{'x'}, session.MaxCert+1) }), false},
		{"task without a client certificate", uploaded(func(p *UIProof) { p.Client = nil }), false},
		{"task without a proof", uploaded(func(p *UIProof) { p.Proof = nil }), false},
		{"proof of an unknown kind", uploaded(func(p *UIProof) { p.Proof.Kind = "otp" }), false},
		{"task over task.MaxTask", uploaded(func(p *UIProof) { p.Task = &bigTask }), false},
		{"task with a signature of 63 bytes", uploaded(func(p *UIProof) { p.Task.Sig = p.Task.Sig[:63] }), false},
		{"catalog with a key ID of 21 characters", storeApp(func(p *UIProof) { p.Catalog.KeyID = p.Catalog.KeyID[:21] }), false},
		{"core app", storeApp(func(p *UIProof) { p.App = AppStore }), false},
		{"version that is none", storeApp(func(p *UIProof) { p.Version = "1.0" }), false},
		{"store that is no name", storeApp(func(p *UIProof) { p.Store = "Global" }), false},
	} {
		b, err := json.Marshal(c.p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseUIProof(b); (err == nil) != c.valid || (err != nil && err != ErrUIProof) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	good, err := json.Marshal(f.storeProof(t))
	if err != nil {
		t.Fatal(err)
	}
	const head = `{"app":"org.sorglos.terminal",`
	if !strings.HasPrefix(string(good), head) {
		t.Fatalf("proof starts %.40s", good)
	}
	rest := string(good[len(head):]) // the members after app, and the closing brace
	for name, b := range map[string]string{
		"unknown member":        head + `"user":"admin",` + rest,
		"member in other case":  `{"App":"org.sorglos.terminal",` + rest,
		"duplicate member":      head + `"app":"org.sorglos.terminal",` + rest,
		"null member":           head + `"task":null,` + rest,
		"an array":              `[` + string(good) + `]`,
		"trailing data":         string(good) + ` {}`,
		"over MaxCallResult":    string(good) + strings.Repeat(" ", wire.MaxCallResult),
		"not JSON":              `app=org.sorglos.terminal`,
		"empty":                 ``,
		"payload with an array": `{"app":"org.sorglos.terminal","version":"1.0.0","store":"global","catalog":{"payload":[]}}`,
	} {
		if _, err := ParseUIProof([]byte(b)); err != ErrUIProof {
			t.Errorf("%s: %v", name, err)
		}
	}
	// An empty member counts as absent, as everywhere (tunnel.ParseOpen): a
	// catalog beside an empty client is still a catalog alone.
	if _, err := ParseUIProof([]byte(head + `"client":"",` + rest)); err != nil {
		t.Errorf("empty client beside a catalog: %v", err)
	}
}

// TestParseUIProofBoundsAllocation feeds ParseUIProof a proof of the full
// size with an array or too many members where it allows none: refusing it
// must allocate next to nothing beyond the document itself.
func TestParseUIProofBoundsAllocation(t *testing.T) {
	for _, c := range []struct {
		name string
		in   []byte
	}{
		{"payload as an array", hostile(`{"catalog":{"payload":[`, `{}`, `]}}`, wire.MaxCallResult)},
		{"members of the user's proof", members(`{"proof":{`, `}}`, wire.MaxCallResult)},
		{"members of the UI proof", members(`{`, `}`, wire.MaxCallResult)},
	} {
		var err error
		n := allocated(func() { _, err = ParseUIProof(c.in) })
		t.Logf("%s: %d bytes allocated", c.name, n)
		if n > 64<<10 || err != ErrUIProof {
			t.Errorf("%s: %d bytes allocated, %v", c.name, n, err)
		}
	}
}

// uiProofVector is uiproof.json: the trust list the web interface opens with
// the anchor {1, hash of trust}, and proofs to parse (core.ParseUIProof) and
// verify (core.VerifyUIProof) for app at version.
type uiProofVector struct {
	Trust trust.Signed        `json:"trust"`
	Cases []uiProofVectorCase `json:"cases"`
}

type uiProofVectorCase struct {
	Name    string    `json:"name"`
	App     string    `json:"app"`
	Version string    `json:"version"`
	Proof   enc.Bytes `json:"proof"`        // the UIProof as JSON
	UI      enc.Bytes `json:"ui,omitempty"` // the UI hash a valid proof gives
	Valid   bool      `json:"valid"`
}

// TestUIProofVectors writes uiproof.json and holds the file against the Go
// functions, as the web interface holds it against its own.
func TestUIProofVectors(t *testing.T) {
	f := newUIFixture(t)
	v := uiProofVector{Trust: f.signed}
	for _, c := range uiProofCases(t, f) {
		b, err := json.Marshal(c.proof)
		if err != nil {
			t.Fatal(err)
		}
		v.Cases = append(v.Cases, uiProofVectorCase{Name: c.name, App: c.app, Version: c.version, Proof: b, UI: c.ui,
			Valid: c.parses && c.want == nil})
	}
	golden.Check(t, *update, "uiproof.json", v)

	var file uiProofVector
	golden.Load(t, "uiproof.json", &file)
	latest, err := trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(file.Trust)}, []trust.Signed{file.Trust})
	if err != nil {
		t.Fatal(err)
	}
	valid := 0
	for _, c := range file.Cases {
		p, err := ParseUIProof(c.Proof)
		var ui []byte
		if err == nil {
			ui, err = VerifyUIProof(p, c.App, c.Version, latest.List)
		}
		if (err == nil) != c.Valid || !bytes.Equal(ui, c.UI) {
			t.Errorf("%s: got %x, %v; want valid %v", c.Name, ui, err, c.Valid)
		}
		if c.Valid {
			valid++
		}
	}
	if valid != 4 || len(file.Cases) != 23 {
		t.Errorf("%d cases, %d of them valid", len(file.Cases), valid)
	}
}

// allocated returns how many bytes f allocates.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// hostile returns prefix, then elem as often as fits, then suffix: exactly
// size bytes.
func hostile(prefix, elem, suffix string, size int) []byte {
	var b bytes.Buffer
	b.Grow(size)
	b.WriteString(prefix + elem)
	for b.Len()+1+len(elem)+len(suffix) <= size {
		b.WriteString("," + elem)
	}
	for b.Len()+len(suffix) < size {
		b.WriteByte(' ')
	}
	b.WriteString(suffix)
	return b.Bytes()
}

// members returns prefix, then as many members "xN":0 as fit, then suffix:
// exactly size bytes.
func members(prefix, suffix string, size int) []byte {
	var b bytes.Buffer
	b.WriteString(prefix + `"x0":0`)
	for i := 1; b.Len()+len(`,"x`+strconv.Itoa(i)+`":0`)+len(suffix) <= size; i++ {
		b.WriteString(`,"x` + strconv.Itoa(i) + `":0`)
	}
	for b.Len()+len(suffix) < size {
		b.WriteByte(' ')
	}
	b.WriteString(suffix)
	return b.Bytes()
}

// TestParseParamsBoundsAllocation feeds ParseParams params of the full size
// with more elements than an action allows, or an array or object where it
// allows none: refusing them must allocate next to nothing.
func TestParseParamsBoundsAllocation(t *testing.T) {
	cases := []struct {
		name string
		a    Action
		in   []byte
	}{
		{"rights", action(t, AppUsers, ServiceAPI, ActSetRights), hostile(`{"role":"user","rights":[`, `{}`, `]}`, wire.MaxParams)},
		{"grants", action(t, AppStore, ServiceController, ActActivate), hostile(`{"grants":[`, `""`, `]}`, wire.MaxParams)},
		{"an array no params have", action(t, AppDesktop, ServiceAPI, ActUI), hostile(`{"app":[`, `{}`, `]}`, wire.MaxParams)},
		{"members of params", action(t, AppDesktop, ServiceAPI, ActUI), members(`{`, `}`, wire.MaxParams)},
		{"members for an action without params", action(t, AppDesktop, ServiceAPI, ActMe), members(`{`, `}`, wire.MaxParams)},
	}
	for _, c := range cases {
		var err error
		n := allocated(func() { _, err = ParseParams(c.a, c.in) })
		t.Logf("%s: %d bytes allocated", c.name, n)
		if n > 64<<10 {
			t.Errorf("%s: %d bytes allocated", c.name, n)
		}
		if err != ErrParams {
			t.Errorf("%s: got %v, want %v", c.name, err, ErrParams)
		}
	}
}

// genesis opens version 1 as the "genesis" vector of trust.json has it:
// trusttest.Genesis signed by its admin NewSigner(1).
func genesis(t testing.TB) trust.Version {
	t.Helper()
	admin := trusttest.NewSigner(1)
	g := admin.Sign(t, trusttest.Genesis(admin))
	v, err := trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(g)}, []trust.Signed{g})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// examples returns one example result per action with a result, and a
// notice.
func examples(t testing.TB) map[string]any {
	f := newUIFixture(t)
	apiDH, apiSign := trusttest.NodeKeys(0xA2)
	_, apiPin := trusttest.APITLS(0xE2, "api-2.test")
	_, releaseKey := trusttest.NodeKeys(trusttest.ReleaseFill)
	// The codes of api-1, anchored in version 1 of trust.json ("genesis").
	api1, _ := trusttest.NodeKeys(0xA1)
	_, api1Pin := trusttest.APITLS(0xE1, "api-1.test")
	anchor := trust.Anchor{Seq: 1, Hash: genesis(t).Hash}
	pair := tunnel.PairCode{URL: "wss://api-1.test:7443/tunnel", Key: api1.Public, TLS: api1Pin,
		Secret: bytes.Repeat([]byte{0x5E}, tunnel.SecretSize), Anchor: anchor}
	join := tunnel.JoinCode{URL: "wss://api-1.test:7443/tunnel", Key: api1.Public, TLS: api1Pin,
		Secret: bytes.Repeat([]byte{0x5F}, tunnel.SecretSize), Anchor: anchor}
	return map[string]any{
		"ngc.desktop/me":         Me{ID: "ISEhISEhISEhISEhISEhIQ", Name: "admin", Roles: []string{trust.RoleAdmin}, Admin: true, PasswordLogin: true, Level: trust.LevelCritical},
		"ngc.desktop/apps":       []DesktopApp{{ID: "org.sorglos.terminal", Name: "Terminal", Version: "1.0.0", Icon: "icon.svg", Width: 720, Height: 480, Actions: []string{"shell"}, State: AppOK, Node: "ctl-1"}},
		"ngc.desktop/totp-begin": TOTPBegin{Secret: "JBSWY3DPEHPK3PXP", URI: "otpauth://totp/NGC:admin?secret=JBSWY3DPEHPK3PXP&issuer=NGC"},
		"ngc.desktop/ui-proof":   f.storeProof(t),
		"ngc.users/list":         []UserInfo{{ID: "ISEhISEhISEhISEhISEhIQ", Name: "admin", Roles: []string{trust.RoleAdmin}, TOTP: true, LastLogin: 1790000000}},
		"ngc.users/rights":       []RoleRights{{Role: "user", Rights: []string{"org.sorglos.calculator/use"}}},
		"ngc.users/trust":        TrustResult{Seq: 4},
		"ngc.users/pair-code":    PairCodeResult{Code: pair.String(), Expires: 1790086400},
		"ngc.users/join-code":    PairCodeResult{Code: join.String(), Expires: 1790086400},
		"ngc.users/pending":      []Pending{{Name: "dienste", Key: bytes.Repeat([]byte{0xC1}, 32), Since: 1790000000}},
		"ngc.users/pending-api": []PendingAPI{{Node: "api-2", Noise: apiDH.Public, Sign: enc.Bytes(apiSign.Public().(ed25519.PublicKey)),
			TLS: apiPin, URL: "wss://api-2.test:7443/tunnel", Since: 1790000000}},
		"ngc.users/log":         []LogEntry{{ID: "000102030405060708090a0b0c0d0e0f", Time: 1790000000, User: "ISEhISEhISEhISEhISEhIQ", Kind: LogTask, App: "ngc.store", Action: "activate", Node: "ctl-1", Result: "ok"}},
		"ngc.store/sources":     []SourceInfo{{ID: "global", Name: "NGC Store", URL: "https://store.ngc.test", Seq: 5, Checked: 1790000000}},
		"ngc.store/catalog":     CatalogResult{Catalog: *f.catalog(t, uiCatalog(), f.global)},
		"ngc.store/installed":   []InstalledApp{{ID: "org.sorglos.terminal", Version: "1.0.0", Store: "global", Node: "ctl-1", Active: true, State: AppOK, License: LicenseValid}},
		"ngc.store/upload":      UploadResult{ID: "org.example.notes", Version: "0.1.0", SHA256: bytes.Repeat([]byte{5}, 32), UI: uiNotes},
		"ngc.store/license":     LicenseResult{App: "org.sorglos.terminal", State: LicenseValid},
		"ngc.store/install-key": InstallKey{Installation: "EREREREREREREREREREREQ", Key: bytes.Repeat([]byte{6}, 32)},
		"ngc.store/updates": []UpdateInfo{
			{Target: trust.AutoController, Node: "ctl-1", Installed: "1.0.0", Available: "1.1.0", SHA256: bytes.Repeat([]byte{0x05}, 32),
				Key: enc.Bytes(releaseKey.Public().(ed25519.PublicKey)), Mode: store.ModeSemi, State: store.UpdateDownloaded},
			{Target: "org.sorglos.terminal", Node: "ctl-1", Installed: "1.0.0", Available: "1.1.0", Mode: store.ModeAuto, Window: "02:00-04:00",
				State: store.UpdateAvailable},
			{Target: trust.AutoSystem, Node: "ctl-1", Mode: store.ModeNotify, State: store.UpdateAvailable},
		},
		"notice": Notice{Level: NoticeWarn, Text: "Die Lizenz für Terminal läuft in 3 Tagen ab.", App: "org.sorglos.terminal"},
	}
}

// TestExamples writes one example result per call action, so the web
// interface and the API test against the same JSON. "empty" holds the
// results that are lists again, empty, as EncodeResult sends them.
func TestExamples(t *testing.T) {
	examples := examples(t)
	// The examples keep the rules the shell checks.
	for _, e := range examples["ngc.users/log"].([]LogEntry) {
		if !ValidLogID(e.ID) {
			t.Errorf("log entry ID %q", e.ID)
		}
	}
	for _, a := range examples["ngc.desktop/apps"].([]DesktopApp) {
		if !wire.ValidName(a.Node) {
			t.Errorf("desktop app node %q", a.Node)
		}
	}
	notice, err := json.Marshal(examples["notice"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseNotice(notice); err != nil {
		t.Errorf("notice: %v", err)
	}
	// The codes parse and fit version 1 of trust.json.
	g := genesis(t)
	pair, err := tunnel.ParsePairCode(examples["ngc.users/pair-code"].(PairCodeResult).Code)
	if err == nil {
		err = pair.Check(g)
	}
	if err != nil {
		t.Errorf("pairing code: %v", err)
	}
	join, err := tunnel.ParseJoinCode(examples["ngc.users/join-code"].(PairCodeResult).Code)
	if err == nil {
		err = join.Check(g)
	}
	if err != nil {
		t.Errorf("join code: %v", err)
	}
	// The catalog is real: its envelope has the strict form, and the global
	// store of that version signed it.
	cat, err := json.Marshal(examples["ngc.store/catalog"].(CatalogResult).Catalog)
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.ParseEnvelope(cat, sign.MaxPayload)
	if err == nil {
		err = trust.ErrInvalid // no global store in the list
		for _, st := range g.List.Stores {
			if st.ID == trust.StoreGlobal {
				_, err = store.VerifyCatalog(env, st, 0)
			}
		}
	}
	if err != nil {
		t.Errorf("catalog: %v", err)
	}
	// Every example reads back as the shell reads a result: strict JSON
	// through a budget, which knows the lists, and then the same encoding.
	lists := enc.Budget{Size: wire.MaxCallResult, Items: map[string]int{"": 64, "*/roles": trust.MaxRoles, "*/actions": 64, "*/rights": maxRights}}
	objects := enc.Budget{Size: wire.MaxCallResult, Items: map[string]int{"roles": trust.MaxRoles}}
	for name, v := range examples {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		budget := objects
		if reflect.TypeOf(v).Kind() == reflect.Slice {
			budget = lists
		}
		back := reflect.New(reflect.TypeOf(v))
		if err := budget.Decode(b, back.Interface()); err != nil {
			t.Errorf("%s does not read back: %v", name, err)
			continue
		}
		if again, err := json.Marshal(back.Elem().Interface()); err != nil || !bytes.Equal(again, b) {
			t.Errorf("%s reads back as %s, %v", name, again, err)
		}
	}
	// A pending API server becomes an API node with exactly its values and
	// Name = Node.
	for _, p := range examples["ngc.users/pending-api"].([]PendingAPI) {
		n := trust.Node{ID: p.Node, Kind: trust.NodeAPI, Name: p.Node, Noise: p.Noise, Sign: p.Sign, URL: p.URL, TLS: p.TLS}
		if !trust.ValidAPINode(n) {
			t.Errorf("pending API server %+v is no valid API node", p)
		}
	}
	// An update by click signs a row into InstallParams; a row's key is what
	// the standing approval of Genesis for its target names.
	for _, u := range examples["ngc.store/updates"].([]UpdateInfo) {
		if !store.ValidTarget(u.Target) || !wire.ValidName(u.Node) || (len(u.Key) != 0 && !store.Approved(g.List, u.Target, u.Key)) {
			t.Errorf("update row %+v", u)
		}
		click := InstallParams{Target: u.Target}
		if u.Target != trust.AutoSystem {
			click.Version, click.SHA256 = u.Available, u.SHA256
		}
		if (u.State == store.UpdateDownloaded || u.Target == trust.AutoSystem) != (click.Validate() == nil) {
			t.Errorf("update row %+v: install params %v", u, click.Validate())
		}
	}
	// The UI proof is the valid store case of uiproof.json; the global store
	// of Genesis signed it.
	b, err := json.Marshal(examples["ngc.desktop/ui-proof"])
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseUIProof(b)
	if err == nil {
		var ui []byte
		ui, err = VerifyUIProof(p, p.App, p.Version, g.List)
		if err == nil && !bytes.Equal(ui, uiTerminal) {
			t.Errorf("ui-proof proves %x", ui)
		}
	}
	if err != nil {
		t.Errorf("ui-proof: %v", err)
	}
	empty := map[string]json.RawMessage{}
	for name, v := range examples {
		if typ := reflect.TypeOf(v); typ.Kind() == reflect.Slice {
			b, err := EncodeResult(reflect.Zero(typ).Interface())
			if err != nil {
				t.Fatal(err)
			}
			empty[name] = b
		}
	}
	examples["empty"] = empty
	golden.Check(t, *update, "core.json", examples)
}

// TestEncodeResult: each of the nine results that are lists comes out as []
// when nil, where encoding/json writes null; every example comes out as
// encoding/json writes it, but for escapes; a null or a result too large is
// ErrResult.
func TestEncodeResult(t *testing.T) {
	var lists []string
	for name, v := range examples(t) {
		if typ := reflect.TypeOf(v); typ.Kind() == reflect.Slice {
			lists = append(lists, name)
			none := reflect.Zero(typ).Interface()
			if b, _ := json.Marshal(none); string(b) != "null" {
				t.Errorf("%s: encoding/json writes %s", name, b)
			}
			for _, list := range []any{none, reflect.MakeSlice(typ, 0, 0).Interface()} {
				if b, err := EncodeResult(list); err != nil || string(b) != "[]" {
					t.Errorf("%s: %s, %v", name, b, err)
				}
			}
		}
		// The shell tests with core.json, which encoding/json writes; the API
		// sends what EncodeResult writes.
		v1, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		v2, err := EncodeResult(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var a, b any
		if json.Unmarshal(v1, &a) != nil || json.Unmarshal(v2, &b) != nil || !reflect.DeepEqual(a, b) {
			t.Errorf("%s: EncodeResult writes %s, encoding/json %s", name, v2, v1)
		}
	}
	slices.Sort(lists)
	want := []string{"ngc.desktop/apps", "ngc.store/installed", "ngc.store/sources", "ngc.store/updates",
		"ngc.users/list", "ngc.users/log", "ngc.users/pending", "ngc.users/pending-api", "ngc.users/rights"}
	if !slices.Equal(lists, want) {
		t.Errorf("results that are lists: %q", lists)
	}
	for name, v := range map[string]any{
		"nil":           nil,
		"nil pointer":   (*Me)(nil),
		"null member":   struct{ P *int }{},
		"too large":     strings.Repeat("x", wire.MaxCallResult-1),
		"invalid UTF-8": "\xff",
		"cannot encode": func() {},
	} {
		if _, err := EncodeResult(v); err != ErrResult {
			t.Errorf("%s: got %v, want %v", name, err, ErrResult)
		}
	}
	// At the limit it still passes.
	if b, err := EncodeResult(strings.Repeat("x", wire.MaxCallResult-2)); err != nil || len(b) != wire.MaxCallResult {
		t.Errorf("result of wire.MaxCallResult bytes: %d, %v", len(b), err)
	}
}

// TestEncodeResultOmitsZero: an optional number or bool of a result is left
// out when it is 0 or false and written when it is set (rule 14 of strict
// JSON). encoding/json/v2, which EncodeResult uses, writes 0 and false for
// omitempty, so these fields need omitzero; the examples set most of them
// and would not notice omitempty.
func TestEncodeResultOmitsZero(t *testing.T) {
	app := DesktopApp{ID: "org.sorglos.terminal", Name: "Terminal", Version: "1.0.0", State: AppOK}
	sized, coreApp := app, app
	sized.Width, sized.Height = 720, 480
	coreApp.Core = true
	user := UserInfo{ID: "ISEhISEhISEhISEhISEhIQ", Name: "admin"}
	seen := user
	seen.LastLogin = 1790000000
	source := SourceInfo{ID: "global", Name: "NGC Store", URL: "https://store.ngc.test", Seq: 5}
	fetched := source
	fetched.Checked = 1790000000
	installed := InstalledApp{ID: "org.sorglos.terminal", Store: "global", Node: "ctl-1", State: AppOK, License: LicenseNone}
	kept := installed
	kept.KeptData = 4096
	cases := []struct {
		zero, set any // the result with the fields 0 or false, and with them set
		keys      []string
	}{
		{app, sized, []string{"width", "height"}},
		{app, coreApp, []string{"core"}},
		{user, seen, []string{"lastLogin"}},
		{source, fetched, []string{"checked"}},
		{installed, kept, []string{"keptData"}},
	}
	for _, c := range cases {
		for _, r := range []struct {
			v    any
			want bool
		}{{c.zero, false}, {c.set, true}} {
			b, err := EncodeResult(r.v)
			var m map[string]any
			if err != nil || json.Unmarshal(b, &m) != nil {
				t.Fatalf("%T: %s, %v", r.v, b, err)
			}
			for _, k := range c.keys {
				if _, ok := m[k]; ok != r.want {
					t.Errorf("%s: %s written %v, want %v", b, k, ok, r.want)
				}
			}
		}
	}
}

func FuzzParseParams(f *testing.F) {
	f.Add([]byte(`{"app":"org.sorglos.terminal","version":"1.0.0"}`))
	f.Add([]byte(`{"role":"user","rights":["org.sorglos.calculator/use"]}`))
	f.Add([]byte(activateParams(`,"grants":["root","network"]`)))
	f.Fuzz(func(t *testing.T, b []byte) {
		// The rule reads nil params as "none came"; the fuzzer's are bytes.
		if b == nil {
			b = []byte{}
		}
		for _, a := range All() {
			p, err := ParseParams(a, b)
			// Whatever passes also passes the rule of the action.
			if _, rerr := RuleFor(a).Critical(b); (err == nil) != (rerr == nil) {
				t.Fatalf("%s: ParseParams %v, rule %v", a.Name, err, rerr)
			}
			if err == nil && p != nil && p.Validate() != nil {
				t.Fatalf("%s: accepted params that do not validate", a.Name)
			}
		}
	})
}

func FuzzParseNotice(f *testing.F) {
	f.Add([]byte(`{"level":"warn","text":"Die Lizenz für Terminal läuft in 3 Tagen ab.","app":"org.sorglos.terminal"}`))
	f.Add([]byte(`{"level":"info","text":"x"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		n, err := ParseNotice(b)
		if err != nil {
			return
		}
		// What passes encodes to a notice that passes again, unchanged.
		out, err := json.Marshal(n)
		if err != nil {
			t.Fatal(err)
		}
		back, err := ParseNotice(out)
		if err != nil || back != n {
			t.Fatalf("%q -> %s: %+v, %v", b, out, back, err)
		}
	})
}

func FuzzReadTransfer(f *testing.F) {
	for _, tr := range []Transfer{testTransfer(), testRelease()} {
		var buf bytes.Buffer
		if err := WriteTransfer(&buf, tr); err != nil {
			f.Fatal(err)
		}
		f.Add(buf.Bytes())
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		tr, err := ReadTransfer(bytes.NewReader(b))
		if err != nil {
			return
		}
		// What passes, envelopes included, writes back and reads again.
		var buf bytes.Buffer
		if err := WriteTransfer(&buf, tr); err != nil {
			t.Fatalf("%+v: %v", tr, err)
		}
		if _, err := ReadTransfer(&buf); err != nil {
			t.Fatalf("%+v: %v", tr, err)
		}
	})
}

func FuzzParseUIProof(f *testing.F) {
	fx := newUIFixture(f)
	for _, c := range uiProofCases(f, fx) {
		b, err := json.Marshal(c.proof)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := ParseUIProof(b)
		if err != nil {
			return
		}
		// What passes encodes to a proof that passes again, unchanged.
		out, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		back, err := ParseUIProof(out)
		if err != nil {
			t.Fatalf("%q -> %s: %v", b, out, err)
		}
		again, err := json.Marshal(back)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("%s -> %s, %v", out, again, err)
		}
	})
}
