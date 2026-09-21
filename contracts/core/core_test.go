package core

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"io"
	"reflect"
	"runtime"
	"strconv"
	"strings"
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
		&PairCodeResult{}, &TOTPBegin{}, &Notice{}, &Transfer{}} {
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
		"ngc.desktop/apps":       []DesktopApp{{ID: "org.sorglos.terminal", Name: "Terminal", Version: "1.0.0", Icon: "icon.svg", Width: 720, Height: 480, Actions: []string{"shell"}, State: AppOK, Node: "ctl-1"}},
		"ngc.desktop/totp-begin": TOTPBegin{Secret: "JBSWY3DPEHPK3PXP", URI: "otpauth://totp/NGC:admin?secret=JBSWY3DPEHPK3PXP&issuer=NGC"},
		"ngc.users/list":         []UserInfo{{ID: "ISEhISEhISEhISEhISEhIQ", Name: "admin", Roles: []string{trust.RoleAdmin}, TOTP: true, LastLogin: 1790000000}},
		"ngc.users/rights":       []RoleRights{{Role: "user", Rights: []string{"org.sorglos.calculator/use"}}},
		"ngc.users/trust":        TrustResult{Seq: 4},
		"ngc.users/pair-code":    PairCodeResult{Code: "ngc-pair:…", Expires: 1790086400},
		"ngc.users/join-code":    PairCodeResult{Code: "ngc-join:…", Expires: 1790086400},
		"ngc.users/pending":      []Pending{{Name: "dienste", Key: bytes.Repeat([]byte{0xC1}, 32), Fingerprint: "k3y…", Since: 1790000000}},
		"ngc.users/log":          []LogEntry{{ID: "000102030405060708090a0b0c0d0e0f", Time: 1790000000, User: "ISEhISEhISEhISEhISEhIQ", Kind: LogTask, App: "ngc.store", Action: "activate", Node: "ctl-1", Result: "ok"}},
		"ngc.store/sources":      []SourceInfo{{ID: "global", Name: "NGC Store", URL: "https://store.ngc.test", Seq: 5, Checked: 1790000000}},
		"ngc.store/catalog":      CatalogResult{Catalog: sign.Envelope{Payload: []byte(`{}`), KeyID: "k", Sig: bytes.Repeat([]byte{7}, 64)}},
		"ngc.store/installed":    []InstalledApp{{ID: "org.sorglos.terminal", Version: "1.0.0", Store: "global", Node: "ctl-1", Active: true, State: AppOK, License: LicenseValid}},
		"ngc.store/upload":       UploadResult{ID: "org.example.notes", Version: "0.1.0", SHA256: bytes.Repeat([]byte{5}, 32)},
		"ngc.store/license":      LicenseResult{App: "org.sorglos.terminal", State: LicenseValid},
		"ngc.store/install-key":  InstallKey{Installation: "EREREREREREREREREREREQ", Key: bytes.Repeat([]byte{6}, 32)},
		"ngc.store/updates":      []UpdateInfo{{Target: "ngc-api", Installed: "1.0.0", Available: "1.1.0", Mode: store.ModeAuto, Window: "02:00-04:00", State: store.UpdateAvailable}},
		"notice":                 Notice{Level: NoticeWarn, Text: "Die Lizenz für Terminal läuft in 3 Tagen ab.", App: "org.sorglos.terminal"},
	}
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
	var buf bytes.Buffer
	_ = WriteTransfer(&buf, testTransfer())
	f.Add(buf.Bytes())
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ReadTransfer(bytes.NewReader(b))
	})
}
