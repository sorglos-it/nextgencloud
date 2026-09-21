package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
	if err != nil || c.Name != "dienste" || len(c.APIs) != 1 || c.Anchor.Seq != 1 || c.Podman != DefaultPodman {
		t.Fatalf("controller: %+v, %v", c, err)
	}
}

func TestSaveController(t *testing.T) {
	c, err := LoadController(example(t, "controller.json"))
	if err != nil {
		t.Fatal(err)
	}
	c.Node = "ctl-1"
	path := filepath.Join(t.TempDir(), "controller.json")
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
	if runtime.GOOS != "windows" {
		if info.Mode().Perm() != 0o600 {
			t.Errorf("mode %v: the file holds keys", info.Mode().Perm())
		}
	}
	again, err := LoadController(path)
	if err != nil || again.Node != "ctl-1" {
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
	path := filepath.Join(t.TempDir(), "controller.json")
	if err := SaveController(path, c); err != ErrConfig {
		t.Fatalf("got %v, want ErrConfig", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file exists after a refused save: %v", err)
	}
}

func TestAnchor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust-anchor.json")
	want := trust.Anchor{Seq: 7, Hash: make([]byte, 32)}
	if err := SaveAnchor(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAnchor(path)
	if err != nil || got.Seq != 7 || len(got.Hash) != 32 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := LoadAnchor(filepath.Join(t.TempDir(), "gone.json")); err == nil {
		t.Error("missing file")
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

// TestValidate loads a fresh, known-valid Web/API/Controller from the
// example files for every case (rather than mutating and restoring one
// shared value), so a case can never leak a change into a later one: each
// case changes exactly the field(s) its closure touches.
func TestValidate(t *testing.T) {
	webCases := map[string]func(*Web){
		"without apis":     func(w *Web) { w.APIs = nil },
		"plain http":       func(w *Web) { w.APIs = []string{"http://api-1.ngc.test:7443"} },
		"origin not https": func(w *Web) { w.RP.Origins = []string{"http://ngc.example.org"} },
	}
	for name, mutate := range webCases {
		w, err := LoadWeb(example(t, "web.json"))
		if err != nil {
			t.Fatal(err)
		}
		mutate(w)
		if err := w.Validate(); err != ErrConfig {
			t.Errorf("web %s: %v", name, err)
		}
	}

	apiCases := map[string]func(*API){
		"without node":          func(a *API) { a.Node = "" },
		"short key":             func(a *API) { a.NoiseKey = []byte{1} },
		"mariadb node no colon": func(a *API) { a.DB.Nodes = []string{"db-1"} },
		"db password too long":  func(a *API) { a.DB.Password = strings.Repeat("x", MaxField+1) },
		"store url too long":    func(a *API) { a.StoreURL = "https://" + strings.Repeat("x", MaxField) },
	}
	for name, mutate := range apiCases {
		a, err := LoadAPI(example(t, "api.json"))
		if err != nil {
			t.Fatal(err)
		}
		mutate(a)
		if err := a.Validate(); err != ErrConfig {
			t.Errorf("api %s: %v", name, err)
		}
	}

	controllerCases := map[string]func(*Controller){
		"name":                func(c *Controller) { c.Name = "Server 1" },
		"api url":             func(c *Controller) { c.APIs[0].URL = "https://api-1.ngc.test:7443/tunnel" },
		"parts":               func(c *Controller) { c.Parts = []string{"mail"} },
		"db key":              func(c *Controller) { c.DBKey = make([]byte, 32) },
		"web without webconf": func(c *Controller) { c.Parts = []string{tunnel.RoleWeb} },
	}
	for name, mutate := range controllerCases {
		c, err := LoadController(example(t, "controller.json"))
		if err != nil {
			t.Fatal(err)
		}
		mutate(c)
		if err := c.Validate(); err != ErrConfig {
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

// FuzzLoad fuzzes the three parse functions directly, in memory: no file is
// written per iteration, only JSON bytes are decoded and validated.
func FuzzLoad(f *testing.F) {
	for _, name := range []string{"web.json", "api.json", "controller.json"} {
		b, err := os.ReadFile(filepath.Join("..", "testdata", "config", name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = parseWeb(b)
		_, _ = parseAPI(b)
		_, _ = parseController(b)
	})
}
