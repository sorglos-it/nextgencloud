package core

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"io"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/store"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
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
	}
	if _, ok := Lookup(AppStore, ServiceAPI, ActActivate); ok {
		t.Error("activate belongs to the controller service")
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

func TestParseParams(t *testing.T) {
	sum := enc.B64.EncodeToString(bytes.Repeat([]byte{1}, 32))
	good := []struct{ app, service, name, params string }{
		{AppDesktop, ServiceAPI, ActMe, ``},
		{AppDesktop, ServiceAPI, ActUI, `{"app":"org.sorglos.terminal"}`},
		{AppDesktop, ServiceAPI, ActTOTPConfirm, `{"code":"123456"}`},
		{AppUsers, ServiceAPI, ActSetRights, `{"role":"user","rights":["org.sorglos.calculator/use","org.sorglos.terminal/shell"]}`},
		{AppUsers, ServiceAPI, ActLog, `{"limit":100}`},
		{AppStore, ServiceAPI, ActSetUpdate, `{"target":"ngc-api","mode":"auto","window":"02:00-04:00"}`},
		{AppStore, ServiceController, ActActivate, `{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","store":"global","grants":["network"]}`},
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
		{AppDesktop, ServiceAPI, ActUI, ``},
		{AppDesktop, ServiceAPI, ActUI, `{"app":"ngc.users"}`},
		{AppDesktop, ServiceAPI, ActTOTPConfirm, `{"code":"12345"}`},
		{AppUsers, ServiceAPI, ActSetRights, `{"role":"user","rights":["everything"]}`},
		{AppUsers, ServiceAPI, ActLog, `{"limit":0}`},
		{AppStore, ServiceAPI, ActSetUpdate, `{"target":"ngc-api","mode":"always"}`},
		{AppStore, ServiceController, ActActivate, `{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","store":"global","grants":["host"]}`},
		{AppStore, ServiceController, ActInstall, `{"target":"system","version":"1.0.0"}`},
		{AppStore, ServiceController, ActInstall, `{"target":"ngc-controller","version":"1.1.0"}`},
	}
	for _, c := range bad {
		if _, err := ParseParams(action(t, c.app, c.service, c.name), json.RawMessage(c.params)); err == nil {
			t.Errorf("%s %s: accepted", c.name, c.params)
		}
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
	f.Fuzz(func(t *testing.T, b []byte) {
		for _, a := range All() {
			_, _ = ParseParams(a, b)
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
