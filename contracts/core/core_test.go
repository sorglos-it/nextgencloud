package core

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"io"
	"runtime"
	"strconv"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/session"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/store"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

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

	join := action(t, AppUsers, ServiceAPI, ActJoinCode)
	if !join.Admin || join.Root || !join.Changes || join.Flow != FlowCall || join.Params != nil {
		t.Errorf("join-code: %+v", join)
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
	return json.RawMessage(`{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","store":"global"` + extra + `}`)
}

func TestParseParams(t *testing.T) {
	sum := enc.B64.EncodeToString(bytes.Repeat([]byte{1}, 32))
	good := []struct{ app, service, name, params string }{
		{AppDesktop, ServiceAPI, ActMe, ``},
		{AppDesktop, ServiceAPI, ActMe, `{}`},
		{AppDesktop, ServiceAPI, ActMe, `{ }`},
		{AppDesktop, ServiceAPI, ActUI, `{"app":"org.sorglos.terminal"}`},
		{AppDesktop, ServiceAPI, ActTOTPConfirm, `{"code":"123456"}`},
		{AppUsers, ServiceAPI, ActSetRights, `{"role":"user","rights":["org.sorglos.calculator/use","org.sorglos.terminal/shell"]}`},
		{AppUsers, ServiceAPI, ActSetRights, `{"role":"user","rights":[]}`},
		{AppUsers, ServiceAPI, ActSetRights, `{"role":"user"}`},
		{AppUsers, ServiceAPI, ActLog, `{"limit":100}`},
		{AppStore, ServiceAPI, ActSetUpdate, `{"target":"ngc-api","mode":"auto","window":"02:00-04:00"}`},
		{AppStore, ServiceController, ActActivate, `{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","store":"global","grants":["network"]}`},
		{AppStore, ServiceController, ActActivate, `{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","store":"local","grants":["root","network"]}`},
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
		{AppDesktop, ServiceAPI, ActTOTPConfirm, `{"code":"12345"}`},
		{AppUsers, ServiceAPI, ActSetRights, `{"role":"user","rights":["everything"]}`},
		{AppUsers, ServiceAPI, ActSetRights, `{"role":"user","rights":["org.sorglos.terminal/use","org.sorglos.terminal/use"]}`},
		{AppUsers, ServiceAPI, ActSetRights, `{"role":"user","rights":null}`},
		{AppUsers, ServiceAPI, ActLog, `{"limit":0}`},
		{AppUsers, ServiceAPI, ActLog, `{"limit":501}`},
		{AppStore, ServiceAPI, ActSetUpdate, `{"target":"ngc-api","mode":"always"}`},
		{AppStore, ServiceController, ActActivate, `{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","store":"global","grants":["host"]}`},
		{AppStore, ServiceController, ActActivate, `{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","store":"global","grants":["root","root"]}`},
		{AppStore, ServiceController, ActActivate, `{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","store":"global","grants":["root","network","root"]}`},
		{AppStore, ServiceController, ActInstall, `{"target":"system","version":"1.0.0"}`},
		{AppStore, ServiceController, ActInstall, `{"target":"ngc-controller","version":"1.1.0"}`},
		{AppStore, ServiceController, ActInstall, `{"target":"ngc-controller","version":"1.1.0","sha256":"` + sum + `","size":1}`},
	}
	for _, c := range bad {
		if _, err := ParseParams(action(t, c.app, c.service, c.name), json.RawMessage(c.params)); err != ErrParams {
			t.Errorf("%s %s: got %v, want %v", c.name, c.params, err, ErrParams)
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
		{"upload is critical with empty params", upload, json.RawMessage(`{}`), true, nil},
		{"params for an action without any give an error", upload, json.RawMessage(`{"app":"org.sorglos.terminal"}`), false, ErrParams},
	}
	for _, c := range cases {
		got, err := RuleFor(c.a).Critical(c.params)
		if got != c.want || err != c.wantErr {
			t.Errorf("%s: got %v, %v; want %v, %v", c.name, got, err, c.want, c.wantErr)
		}
	}
	// nil params ask Critical(a, nil), and Root and Changes come from the table.
	for _, a := range All() {
		r := RuleFor(a)
		got, err := r.Critical(nil)
		if err != nil || got != Critical(a, nil) || r.Root != a.Root || r.Changes != a.Changes {
			t.Errorf("%s/%s/%s: rule %+v, Critical(nil) = %v, %v", a.App, a.Service, a.Name, r, got, err)
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

func testTransfer() Transfer {
	cat := sign.Envelope{Payload: []byte(`{}`), KeyID: "k", Sig: bytes.Repeat([]byte{1}, 64)}
	lic := sign.Envelope{Payload: []byte(`{}`), KeyID: "k", Sig: bytes.Repeat([]byte{2}, 64)}
	return Transfer{Catalog: &cat, Store: "global", License: &lic, Key: bytes.Repeat([]byte{3}, 32), Size: 5000000, SHA256: bytes.Repeat([]byte{4}, 32)}
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
	} {
		tr := testTransfer()
		mutate(&tr)
		if tr.Validate() != ErrTransfer {
			t.Errorf("%s accepted", name)
		}
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

// TestExamples writes one example result per call action, so the web
// interface and the API test against the same JSON.
func TestExamples(t *testing.T) {
	examples := map[string]any{
		"ngc.desktop/me":         Me{ID: "ISEhISEhISEhISEhISEhIQ", Name: "admin", Roles: []string{trust.RoleAdmin}, Admin: true, PasswordLogin: true, Level: trust.LevelCritical},
		"ngc.desktop/apps":       []DesktopApp{{ID: "org.sorglos.terminal", Name: "Terminal", Version: "1.0.0", Icon: "icon.svg", Width: 720, Height: 480, Actions: []string{"shell"}, State: AppOK}},
		"ngc.desktop/totp-begin": TOTPBegin{Secret: "JBSWY3DPEHPK3PXP", URI: "otpauth://totp/NGC:admin?secret=JBSWY3DPEHPK3PXP&issuer=NGC"},
		"ngc.users/list":         []UserInfo{{ID: "ISEhISEhISEhISEhISEhIQ", Name: "admin", Roles: []string{trust.RoleAdmin}, TOTP: true, LastLogin: 1790000000}},
		"ngc.users/rights":       []RoleRights{{Role: "user", Rights: []string{"org.sorglos.calculator/use"}}},
		"ngc.users/trust":        TrustResult{Seq: 4},
		"ngc.users/pair-code":    PairCodeResult{Code: "ngc-pair:…", Expires: 1790086400},
		"ngc.users/pending":      []Pending{{Name: "dienste", Key: bytes.Repeat([]byte{0xC1}, 32), Fingerprint: "k3y…", Since: 1790000000}},
		"ngc.users/log":          []LogEntry{{Time: 1790000000, User: "ISEhISEhISEhISEhISEhIQ", Kind: LogTask, App: "ngc.store", Action: "activate", Node: "ctl-1", Result: "ok"}},
		"ngc.store/sources":      []SourceInfo{{ID: "global", Name: "NGC Store", URL: "https://store.ngc.test", Seq: 5, Checked: 1790000000}},
		"ngc.store/catalog":      CatalogResult{Catalog: sign.Envelope{Payload: []byte(`{}`), KeyID: "k", Sig: bytes.Repeat([]byte{7}, 64)}},
		"ngc.store/installed":    []InstalledApp{{ID: "org.sorglos.terminal", Version: "1.0.0", Store: "global", Node: "ctl-1", Active: true, State: AppOK, License: LicenseValid}},
		"ngc.store/upload":       UploadResult{ID: "org.example.notes", Version: "0.1.0", SHA256: bytes.Repeat([]byte{5}, 32)},
		"ngc.store/license":      LicenseResult{App: "org.sorglos.terminal", State: LicenseValid},
		"ngc.store/install-key":  InstallKey{Installation: "EREREREREREREREREREREQ", Key: bytes.Repeat([]byte{6}, 32)},
		"ngc.store/updates":      []UpdateInfo{{Target: "ngc-api", Installed: "1.0.0", Available: "1.1.0", Mode: store.ModeAuto, Window: "02:00-04:00", State: store.UpdateAvailable}},
		"notice":                 Notice{Level: NoticeWarn, Text: "Die Lizenz für Terminal läuft in 3 Tagen ab.", App: "org.sorglos.terminal"},
	}
	golden.Check(t, *update, "core.json", examples)
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

func FuzzReadTransfer(f *testing.F) {
	var buf bytes.Buffer
	_ = WriteTransfer(&buf, testTransfer())
	f.Add(buf.Bytes())
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ReadTransfer(bytes.NewReader(b))
	})
}
