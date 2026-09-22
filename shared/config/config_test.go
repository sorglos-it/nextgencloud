package config

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

func example(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "testdata", "config", name)
}

func TestExamples(t *testing.T) {
	w, err := LoadWeb(example(t, "web.json"))
	if err != nil || len(w.APIs) != 2 || w.RP.ID != "ngc.example.org" {
		t.Fatalf("web: %+v, %v", w, err)
	}
	a, err := LoadAPI(example(t, "api.json"))
	if err != nil || a.Node != "api-1" || a.DB.Driver != DriverMariaDB || len(a.DB.Nodes) != 3 {
		t.Fatalf("api: %+v, %v", a, err)
	}
	if a.SessionLifetime != DefaultSessionLifetime || a.IdleTimeout != DefaultIdleTimeout {
		t.Errorf("defaults: %+v", a)
	}
	c, err := LoadController(example(t, "controller.json"))
	if err != nil || c.Name != "dienste" || len(c.APIs) != 1 || c.Anchor.Seq != 1 || c.Podman != DefaultPodman ||
		len(c.Roles) != 1 || c.Roles[0] != tunnel.RoleServices || c.Migrate != nil {
		t.Fatalf("controller: %+v, %v", c, err)
	}
	d, err := LoadController(example(t, "controller-db.json"))
	if err != nil || d.Name != "datenbank" || len(d.Roles) != 2 || d.Roles[0] != tunnel.RoleDB || d.Migrate == nil ||
		d.Migrate.Driver != DriverMariaDB || d.Migrate.User != "ngc_migrate" || len(d.Migrate.Nodes) != 3 {
		t.Fatalf("controller with the db role: %+v, %v", d, err)
	}
}

// dirEntries returns the names in dir.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestSaveController(t *testing.T) {
	c, err := LoadController(example(t, "controller-db.json"))
	if err != nil {
		t.Fatal(err)
	}
	c.Node = "ctl-1"
	dir := t.TempDir()
	path := filepath.Join(dir, "controller.json")
	// An older file that others may read: saving must take that away.
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveController(path, c); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// On Windows, os.Stat reports permissions derived only from the
	// read-only attribute, so it cannot reflect the 0600 mode SaveController
	// requested; run go test ./shared/config/ on Linux to check the modes.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v: the file holds keys", info.Mode().Perm())
	}
	if names := dirEntries(t, dir); len(names) != 1 || names[0] != "controller.json" {
		t.Errorf("files left: %v", names)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("null")) || bytes.Contains(b, []byte("[secret]")) {
		t.Errorf("saved file holds null or a redacted secret:\n%s", b)
	}
	// The file is the one place that holds the secrets themselves.
	for _, s := range []string{enc.B64.EncodeToString(c.NoiseKey.Bytes()), enc.B64.EncodeToString(c.DBKey.Bytes()),
		`"password": "` + c.Migrate.Password.Text() + `"`} {
		if !bytes.Contains(b, []byte(s)) {
			t.Errorf("saved file lacks %q:\n%s", s, b)
		}
	}
	again, err := LoadController(path)
	if err != nil || again.Node != "ctl-1" || again.String() != c.String() ||
		!bytes.Equal(again.NoiseKey.Bytes(), c.NoiseKey.Bytes()) || !bytes.Equal(again.DBKey.Bytes(), c.DBKey.Bytes()) ||
		again.Migrate.Password.Text() != c.Migrate.Password.Text() {
		t.Fatalf("round trip: %+v, %v", again, err)
	}
}

// TestSaveControllerInvalid checks that SaveController refuses to write a
// controller that fails Validate, and leaves no file behind when it refuses.
func TestSaveControllerInvalid(t *testing.T) {
	c, err := LoadController(example(t, "controller.json"))
	if err != nil {
		t.Fatal(err)
	}
	c.Name = "" // now fails wire.ValidName
	dir := t.TempDir()
	path := filepath.Join(dir, "controller.json")
	if err := SaveController(path, c); err != ErrConfig {
		t.Fatalf("got %v, want ErrConfig", err)
	}
	if names := dirEntries(t, dir); len(names) != 0 {
		t.Errorf("files after a refused save: %v", names)
	}
}

func TestAnchor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust-anchor.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := trust.Anchor{Seq: 7, Hash: make([]byte, 32)}
	if err := SaveAnchor(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAnchor(path)
	if err != nil || got.Seq != 7 || len(got.Hash) != 32 {
		t.Fatalf("got %+v, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// The web server's PHP reads the anchor; see TestSaveController for Windows.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
		t.Errorf("mode %v", info.Mode().Perm())
	}
	if names := dirEntries(t, dir); len(names) != 1 {
		t.Errorf("files left: %v", names)
	}
	if _, err := LoadAnchor(filepath.Join(t.TempDir(), "gone.json")); err == nil {
		t.Error("missing file")
	}
	for _, s := range []string{`{"seq":0,"hash":"` + enc.B64.EncodeToString(make([]byte, 32)) + `"}`, `{"seq":1,"hash":null}`,
		`{"seq":1,"hash":"AAAA"}`, `{"seq":1,"hash":[1]}`} {
		if _, err := parseAnchor([]byte(s)); err != ErrConfig {
			t.Errorf("%s: %v", s, err)
		}
	}
}

// TestSaveAnchorInvalid checks that SaveAnchor refuses a Seq of 0 and a
// too-short hash, and leaves no file behind either time.
func TestSaveAnchorInvalid(t *testing.T) {
	cases := map[string]trust.Anchor{
		"seq zero":   {Seq: 0, Hash: make([]byte, 32)},
		"short hash": {Seq: 7, Hash: make([]byte, 16)},
	}
	for name, a := range cases {
		path := filepath.Join(t.TempDir(), "trust-anchor.json")
		if err := SaveAnchor(path, a); err != ErrConfig {
			t.Errorf("%s: got %v, want ErrConfig", name, err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s: file exists after a refused save: %v", name, err)
		}
	}
}

func origins(n int) []string {
	o := make([]string, n)
	for i := range o {
		o[i] = fmt.Sprintf("https://ngc-%d.example.org", i)
	}
	return o
}

// TestValidate loads a fresh, known-valid Web/API/Controller from the
// example files for every case (rather than mutating and restoring one
// shared value), so a case can never leak a change into a later one: each
// case changes exactly the field(s) its closure touches.
func TestValidate(t *testing.T) {
	webCases := map[string]struct {
		mutate func(*Web)
		valid  bool
	}{
		"without passkeys":      {func(w *Web) { w.RP.ID, w.RP.Origins = "", nil }, true},
		"eight origins":         {func(w *Web) { w.RP.Origins = origins(trust.MaxOrigins) }, true},
		"RP ID of 253 bytes":    {func(w *Web) { w.RP.ID = strings.Repeat(strings.Repeat("a", 62)+".", 4) + "a" }, true},
		"without apis":          {func(w *Web) { w.APIs = nil }, false},
		"seventeen apis":        {func(w *Web) { w.APIs = append(origins(16), "https://api-17.ngc.test:7443") }, false},
		"plain http":            {func(w *Web) { w.APIs = []string{"http://api-1.ngc.test:7443"} }, false},
		"origin not https":      {func(w *Web) { w.RP.Origins = []string{"http://ngc.example.org"} }, false},
		"http origin":           {func(w *Web) { w.RP.Origins = append(w.RP.Origins, "http://ngc.example.org") }, false},
		"nine origins":          {func(w *Web) { w.RP.Origins = origins(trust.MaxOrigins + 1) }, false},
		"origin too long":       {func(w *Web) { w.RP.Origins = []string{"https://" + strings.Repeat("x", MaxField)} }, false},
		"RP ID with a space":    {func(w *Web) { w.RP.ID = "ngc example.org" }, false},
		"RP ID in upper case":   {func(w *Web) { w.RP.ID = "NGC.example.org" }, false},
		"RP ID with a port":     {func(w *Web) { w.RP.ID = "ngc.example.org:443" }, false},
		"RP ID of 254 bytes":    {func(w *Web) { w.RP.ID = strings.Repeat(strings.Repeat("a", 62)+".", 4) + "ab" }, false},
		"RP ID without origins": {func(w *Web) { w.RP.Origins = nil }, false},
		"origins without RP ID": {func(w *Web) { w.RP.ID = "" }, false},
	}
	for name, c := range webCases {
		w, err := LoadWeb(example(t, "web.json"))
		if err != nil {
			t.Fatal(err)
		}
		c.mutate(w)
		if err := w.Validate(); c.valid && err != nil || !c.valid && err != ErrConfig {
			t.Errorf("web %s: %v", name, err)
		}
	}

	apiCases := map[string]struct {
		mutate func(*API)
		valid  bool
	}{
		"session lifetime of 86400":    {func(a *API) { a.SessionLifetime = session.MaxLifetime }, true},
		"without node":                 {func(a *API) { a.Node = "" }, false},
		"short key":                    {func(a *API) { a.NoiseKey = NewSecret([]byte{1}) }, false},
		"mariadb node no colon":        {func(a *API) { a.DB.Nodes = []string{"db-1"} }, false},
		"db password too long":         {func(a *API) { a.DB.Password = NewSecretText(strings.Repeat("x", MaxField+1)) }, false},
		"store url too long":           {func(a *API) { a.StoreURL = "https://" + strings.Repeat("x", MaxField) }, false},
		"session lifetime above 86400": {func(a *API) { a.SessionLifetime = session.MaxLifetime + 1 }, false},
		"idle longer than the session": {func(a *API) { a.IdleTimeout = a.SessionLifetime + 1 }, false},
	}
	for name, c := range apiCases {
		a, err := LoadAPI(example(t, "api.json"))
		if err != nil {
			t.Fatal(err)
		}
		c.mutate(a)
		if err := a.Validate(); c.valid && err != nil || !c.valid && err != ErrConfig {
			t.Errorf("api %s: %v", name, err)
		}
	}

	sqlite := validSQLite()
	controllerCases := map[string]struct {
		mutate func(*Controller)
		valid  bool
	}{
		"all four roles": {func(c *Controller) {
			c.Roles, c.WebConfig = []string{tunnel.RoleWeb, tunnel.RoleAPI, tunnel.RoleDB, tunnel.RoleServices}, "/etc/ngc/web.json"
		}, true},
		"migrate to SQLite":         {func(c *Controller) { c.Migrate = &sqlite }, true},
		"name":                      {func(c *Controller) { c.Name = "Server 1" }, false},
		"api url":                   {func(c *Controller) { c.APIs[0].URL = "https://api-1.ngc.test:7443/tunnel" }, false},
		"without apis":              {func(c *Controller) { c.APIs = nil }, false},
		"unknown role":              {func(c *Controller) { c.Roles = []string{"mail"} }, false},
		"old part name":             {func(c *Controller) { c.Roles = []string{"ngc-controller"} }, false},
		"no roles":                  {func(c *Controller) { c.Roles, c.Migrate = nil, nil }, false},
		"duplicate role":            {func(c *Controller) { c.Roles = []string{tunnel.RoleDB, tunnel.RoleDB} }, false},
		"five roles":                {func(c *Controller) { c.Roles = append(c.Roles, tunnel.RoleWeb, tunnel.RoleAPI, tunnel.RoleWeb) }, false},
		"db key":                    {func(c *Controller) { c.DBKey = NewSecret(make([]byte, 32)) }, false},
		"web without webconf":       {func(c *Controller) { c.Roles = append(c.Roles, tunnel.RoleWeb) }, false},
		"db role without migrate":   {func(c *Controller) { c.Migrate = nil }, false},
		"migrate without db role":   {func(c *Controller) { c.Roles = []string{tunnel.RoleServices} }, false},
		"migrate with empty driver": {func(c *Controller) { c.Migrate.Driver = "" }, false},
		"migrate without password":  {func(c *Controller) { c.Migrate.Password = SecretText{} }, false},
	}
	for name, c := range controllerCases {
		ctl, err := LoadController(example(t, "controller-db.json"))
		if err != nil {
			t.Fatal(err)
		}
		c.mutate(ctl)
		if err := ctl.Validate(); c.valid && err != nil || !c.valid && err != ErrConfig {
			t.Errorf("controller %s: %v", name, err)
		}
	}
}

// validSQLite is a DB configuration that passes Validate(): SQLite, an
// absolute path, a 64-byte key, no Galera nodes and no MariaDB leftovers.
func validSQLite() DB {
	return DB{Driver: DriverSQLite, Path: "/var/lib/ngc/api/api.db", Key: NewSecret(make([]byte, 64))}
}

// TestValidateSQLite pins one SQLite rule per case: each starts from a fresh
// validSQLite() and changes exactly one field away from valid.
func TestValidateSQLite(t *testing.T) {
	base := validSQLite()
	if err := base.Validate(); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	cases := map[string]func(*DB){
		"relative path":     func(d *DB) { d.Path = "var/lib/ngc/api/api.db" },
		"wrong key length":  func(d *DB) { d.Key = NewSecret(make([]byte, 32)) },
		"nodes present":     func(d *DB) { d.Nodes = []string{"db-1:3306"} },
		"leftover password": func(d *DB) { d.Password = NewSecretText("leftover") },
		"path too long":     func(d *DB) { d.Path = "/" + strings.Repeat("x", MaxField) },
	}
	for name, mutate := range cases {
		d := validSQLite()
		mutate(&d)
		if err := d.Validate(); err != ErrConfig {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// secretForms returns how a key or password could show when printed: as
// base64url, standard base64, decimal and Go byte lists, and hex. A key of
// the examples repeats one byte, so the first 8 bytes stand for all of it.
func secretForms(keys [][]byte, passwords []string) []string {
	var forms []string
	for _, k := range keys {
		head := k[:8]
		forms = append(forms, enc.B64.EncodeToString(k), base64.StdEncoding.EncodeToString(k),
			strings.Trim(fmt.Sprint(head), "[]"), strings.Trim(fmt.Sprintf("%#v", head), "[]byte{}"),
			fmt.Sprintf("%x", head), fmt.Sprintf("%X", head))
	}
	for _, p := range passwords {
		forms = append(forms, p, fmt.Sprintf("%x", p), fmt.Sprintf("%X", p))
	}
	return forms
}

// TestRedaction prints and logs each configuration in every way at hand:
// every verb of package fmt, the configuration itself, by pointer, in a
// slice, in a map and in an exported and an unexported field of another
// struct, through both handlers of log/slog and through encoding/json. No
// secret of the examples may show; the node or name does. Only the file
// holds the secrets (TestSaveController).
func TestRedaction(t *testing.T) {
	api, err := LoadAPI(example(t, "api.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctl, err := LoadController(example(t, "controller-db.json"))
	if err != nil {
		t.Fatal(err)
	}
	sqlite := validSQLite()
	sqlite.Key = NewSecret(bytes.Repeat([]byte{0x5C}, 64))
	withSQLite, err := LoadController(example(t, "controller.json"))
	if err != nil {
		t.Fatal(err)
	}
	withSQLite.Roles, withSQLite.Migrate = []string{tunnel.RoleDB}, &sqlite

	type exported struct{ Cfg any }
	type unexported struct{ cfg any }
	verbs := []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%b", "%o", "%O", "%c", "%U", "%e", "%E",
		"%f", "%F", "%g", "%G", "%t", "%T", "%p", "% x", "%#x", "%+q", "%#q", "%-300s", "%.10v", "%300v", "%w"}
	cases := []struct {
		name    string
		value   any // the configuration itself
		pointer any
		shown   string
		secrets []string
	}{
		{"api", *api, api, "api-1",
			secretForms([][]byte{api.NoiseKey.Bytes(), api.SignKey.Bytes(), api.SecretKey.Bytes()}, []string{api.DB.Password.Text()})},
		{"controller", *ctl, ctl, "datenbank",
			secretForms([][]byte{ctl.NoiseKey.Bytes(), ctl.DBKey.Bytes()}, []string{ctl.Migrate.Password.Text()})},
		{"controller with SQLite", *withSQLite, withSQLite, "dienste",
			secretForms([][]byte{withSQLite.NoiseKey.Bytes(), withSQLite.DBKey.Bytes(), sqlite.Key.Bytes()}, nil)},
		{"db", api.DB, &api.DB, "ngc.test", secretForms(nil, []string{api.DB.Password.Text()})},
	}
	for _, c := range cases {
		var outs []string
		for _, arg := range []any{c.value, c.pointer, []any{c.value}, map[string]any{"cfg": c.value},
			exported{c.value}, unexported{c.value}} {
			for _, verb := range verbs {
				outs = append(outs, fmt.Sprintf(verb, arg))
			}
			var buf bytes.Buffer
			slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "cfg", arg)
			slog.New(slog.NewTextHandler(&buf, nil)).Info("x", "cfg", arg)
			b, err := json.Marshal(arg)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			outs = append(outs, buf.String(), string(b))
		}
		for _, out := range outs {
			for _, s := range c.secrets {
				if strings.Contains(out, s) {
					t.Errorf("%s: %.40q shows a secret:\n%.400s", c.name, s, out)
				}
			}
		}
		var logged bytes.Buffer
		slog.New(slog.NewJSONHandler(&logged, nil)).Info("x", "cfg", c.pointer)
		encoded, _ := json.Marshal(c.value)
		for _, out := range []string{fmt.Sprintf("%v", c.value), fmt.Sprintf("%+v", c.pointer), logged.String(), string(encoded)} {
			if !strings.Contains(out, c.shown) {
				t.Errorf("%s: %q missing:\n%s", c.name, c.shown, out)
			}
		}
		if s := fmt.Sprint(c.value); !strings.Contains(s, "[secret]") {
			t.Errorf("%s: [secret] missing: %s", c.name, s)
		}
	}
	empty := DB{Driver: DriverMariaDB}
	if s := empty.String(); strings.Contains(s, "[secret]") {
		t.Errorf("unset secrets shown as set: %s", s)
	}
}

// TestSecret: a secret keeps a copy of its value, shows it only through
// Bytes and Text, and an unset one is left out of a file.
func TestSecret(t *testing.T) {
	b := []byte{1, 2, 3}
	s := NewSecret(b)
	b[0] = 9
	if !bytes.Equal(s.Bytes(), []byte{1, 2, 3}) || s.String() != "[secret]" || fmt.Sprintf("%#v", s) != `"[secret]"` {
		t.Errorf("secret %v, %s", s.Bytes(), s)
	}
	if NewSecret(nil).Bytes() != nil || !NewSecret(nil).IsZero() || NewSecretText("").Text() != "" || (SecretText{}).String() != "" {
		t.Error("unset secrets")
	}
	var back struct {
		Key  Secret     `json:"key"`
		Word SecretText `json:"word"`
	}
	if err := decode(anchorBudget, []byte(`{"key":"AQID","word":"geheim"}`), &back); err != nil ||
		!bytes.Equal(back.Key.Bytes(), []byte{1, 2, 3}) || back.Word.Text() != "geheim" {
		t.Errorf("decode: %v, %v", back, err)
	}
	for _, in := range []string{`{"key":"AQI="}`, `{"key":"AQ\nID"}`, `{"key":1}`, `{"word":1}`, "{\"word\":\"\xff\"}"} {
		if err := decode(anchorBudget, []byte(in), &back); err != ErrConfig {
			t.Errorf("%q: got %v, want %v", in, err, ErrConfig)
		}
	}
	file, err := marshalFile(DB{Driver: DriverSQLite, Path: "/x.db", Key: NewSecret([]byte{1, 2, 3})})
	if err != nil || string(file) != "{\n  \"driver\": \"sqlite\",\n  \"path\": \"/x.db\",\n  \"key\": \"AQID\"\n}\n" {
		t.Errorf("file %q, %v", file, err)
	}
}

// TestSecretBytesIsACopy: whoever changes what Bytes returned changes neither
// the Secret nor a copy of it, which shares its key.
func TestSecretBytesIsACopy(t *testing.T) {
	s := NewSecret([]byte{1, 2, 3})
	shared := s
	got := s.Bytes()
	got[0] = 9
	if !bytes.Equal(s.Bytes(), []byte{1, 2, 3}) || !bytes.Equal(shared.Bytes(), []byte{1, 2, 3}) {
		t.Errorf("secret changed through Bytes: %v, %v", s.Bytes(), shared.Bytes())
	}
	if &s.Bytes()[0] == &s.Bytes()[0] {
		t.Error("Bytes hands out the same array twice")
	}
}

// TestRedactedFileRefused: encoding/json writes the stand-in "[secret]" for
// every secret. A file written that way must fail to load, never store the
// stand-in as a key or password; SaveController writes the real file.
func TestRedactedFileRefused(t *testing.T) {
	api, err := LoadAPI(example(t, "api.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctl, err := LoadController(example(t, "controller-db.json"))
	if err != nil {
		t.Fatal(err)
	}
	for name, parse := range map[string]func() error{
		"api.json": func() error { b, _ := json.Marshal(api); _, err := parseAPI(b); return err },
		"controller.json": func() error {
			b, _ := json.Marshal(ctl)
			_, err := parseController(b)
			return err
		},
		// The password alone: everything else of the database is valid.
		"database of api.json": func() error {
			b, _ := json.Marshal(api.DB)
			var d DB
			return decode(apiBudget, b, &d)
		},
		// The real api.json with only its password replaced by the stand-in.
		"api.json, password only": func() error {
			b, err := os.ReadFile(example(t, "api.json"))
			if err != nil {
				t.Fatal(err)
			}
			b = bytes.Replace(b, []byte(`"password": "changeme"`), []byte(`"password": "[secret]"`), 1)
			if !bytes.Contains(b, []byte(`"password": "[secret]"`)) {
				t.Fatal("password not replaced")
			}
			_, err = parseAPI(b)
			return err
		},
	} {
		if err := parse(); err != ErrConfig {
			t.Errorf("%s: got %v, want %v", name, err, ErrConfig)
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

// hostile returns prefix, then as many empty objects as fit, then suffix:
// exactly size bytes.
func hostile(prefix, suffix string, size int) []byte {
	var b bytes.Buffer
	b.Grow(size)
	b.WriteString(prefix + "{}")
	for b.Len()+len(",{}")+len(suffix) <= size {
		b.WriteString(",{}")
	}
	for b.Len()+len(suffix) < size {
		b.WriteByte(' ')
	}
	b.WriteString(suffix)
	return b.Bytes()
}

// TestParsersBoundAllocation feeds each parser a file of MaxConfig bytes with
// more list elements than its contract allows, or a list where it allows
// none: refusing it must allocate next to nothing.
func TestParsersBoundAllocation(t *testing.T) {
	cases := []struct {
		name  string
		in    []byte
		parse func([]byte) error
	}{
		{"parseWeb", hostile(`{"apis":[`, `]}`, MaxConfig), func(b []byte) error { _, err := parseWeb(b); return err }},
		{"parseWeb, origins", hostile(`{"rp":{"origins":[`, `]}}`, MaxConfig), func(b []byte) error { _, err := parseWeb(b); return err }},
		{"parseAPI", hostile(`{"db":{"nodes":[`, `]}}`, MaxConfig), func(b []byte) error { _, err := parseAPI(b); return err }},
		{"parseController", hostile(`{"roles":[`, `]}`, MaxConfig), func(b []byte) error { _, err := parseController(b); return err }},
		{"parseController, migrate", hostile(`{"migrate":{"nodes":[`, `]}}`, MaxConfig), func(b []byte) error { _, err := parseController(b); return err }},
		{"parseAnchor", hostile(`{"hash":[`, `]}`, MaxConfig), func(b []byte) error { _, err := parseAnchor(b); return err }},
	}
	for _, c := range cases {
		var err error
		n := allocated(func() { err = c.parse(c.in) })
		t.Logf("%s: %d bytes allocated", c.name, n)
		if n > 64<<10 {
			t.Errorf("%s: %d bytes allocated", c.name, n)
		}
		if err != ErrConfig {
			t.Errorf("%s: got %v, want %v", c.name, err, ErrConfig)
		}
	}
}

// FuzzLoad fuzzes the parse functions directly, in memory: no file is
// written per iteration, only JSON bytes are decoded and validated.
func FuzzLoad(f *testing.F) {
	for _, name := range []string{"web.json", "api.json", "controller.json", "controller-db.json"} {
		b, err := os.ReadFile(filepath.Join("..", "testdata", "config", name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Add([]byte(`{"seq":1,"hash":"` + enc.B64.EncodeToString(make([]byte, 32)) + `"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = parseWeb(b)
		_, _ = parseAPI(b)
		_, _ = parseController(b)
		_, _ = parseAnchor(b)
	})
}
