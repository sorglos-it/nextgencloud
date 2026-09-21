package config

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/session"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/tunnel"
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
	// requested; the mode is verified on Linux (see scripts/check.sh).
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
	if bytes.Contains(b, []byte("null")) {
		t.Errorf("saved file holds null:\n%s", b)
	}
	again, err := LoadController(path)
	if err != nil || again.Node != "ctl-1" || again.String() != c.String() || again.Migrate.Password != c.Migrate.Password {
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
		"short key":                    {func(a *API) { a.NoiseKey = []byte{1} }, false},
		"mariadb node no colon":        {func(a *API) { a.DB.Nodes = []string{"db-1"} }, false},
		"db password too long":         {func(a *API) { a.DB.Password = strings.Repeat("x", MaxField+1) }, false},
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
		"db key":                    {func(c *Controller) { c.DBKey = make([]byte, 32) }, false},
		"web without webconf":       {func(c *Controller) { c.Roles = append(c.Roles, tunnel.RoleWeb) }, false},
		"db role without migrate":   {func(c *Controller) { c.Migrate = nil }, false},
		"migrate without db role":   {func(c *Controller) { c.Roles = []string{tunnel.RoleServices} }, false},
		"migrate with empty driver": {func(c *Controller) { c.Migrate.Driver = "" }, false},
		"migrate without password":  {func(c *Controller) { c.Migrate.Password = "" }, false},
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
	return DB{Driver: DriverSQLite, Path: "/var/lib/ngc/api/api.db", Key: make([]byte, 64)}
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
		"wrong key length":  func(d *DB) { d.Key = make([]byte, 32) },
		"nodes present":     func(d *DB) { d.Nodes = []string{"db-1:3306"} },
		"leftover password": func(d *DB) { d.Password = "leftover" },
	}
	for name, mutate := range cases {
		d := validSQLite()
		mutate(&d)
		if err := d.Validate(); err != ErrConfig {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestRedaction logs and prints each configuration: the secrets of the
// examples must not appear, in any of the usual encodings, but the node or
// name must.
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
	sqlite.Key = bytes.Repeat([]byte{0x5C}, 64)
	withSQLite, err := LoadController(example(t, "controller.json"))
	if err != nil {
		t.Fatal(err)
	}
	withSQLite.Roles, withSQLite.Migrate = []string{tunnel.RoleDB}, &sqlite

	secrets := func(keys ...[]byte) []string {
		var s []string
		for _, k := range keys {
			s = append(s, enc.B64.EncodeToString(k), fmt.Sprint(k), fmt.Sprintf("%x", k))
		}
		return s
	}
	cases := []struct {
		name    string
		cfg     any // as slog gets it
		value   any // the configuration itself, as fmt gets it
		shown   string
		secrets []string
	}{
		{"api", api, *api, "api-1", append(secrets(api.NoiseKey, api.SignKey, api.SecretKey), api.DB.Password)},
		{"controller", ctl, *ctl, "datenbank", append(secrets(ctl.NoiseKey, ctl.DBKey), ctl.Migrate.Password)},
		{"controller with SQLite", withSQLite, *withSQLite, "dienste", secrets(withSQLite.NoiseKey, withSQLite.DBKey, sqlite.Key)},
		{"db", &api.DB, api.DB, "ngc.test", []string{api.DB.Password}},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "cfg", c.cfg)
		for _, out := range []string{buf.String(), fmt.Sprintf("%+v", c.value), fmt.Sprintf("%v", c.value), fmt.Sprint(c.cfg)} {
			for _, s := range c.secrets {
				if strings.Contains(out, s) {
					t.Errorf("%s: %.60q shows a secret:\n%s", c.name, s, out)
				}
			}
			if !strings.Contains(out, c.shown) || !strings.Contains(out, "[secret]") {
				t.Errorf("%s: %q or [secret] missing:\n%s", c.name, c.shown, out)
			}
		}
	}
	empty := DB{Driver: DriverMariaDB}
	if s := empty.String(); strings.Contains(s, "[secret]") {
		t.Errorf("unset secrets shown as set: %s", s)
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
