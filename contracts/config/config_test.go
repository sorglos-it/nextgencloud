package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/trust"
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

func TestValidate(t *testing.T) {
	w, _ := LoadWeb(example(t, "web.json"))
	a, _ := LoadAPI(example(t, "api.json"))
	c, _ := LoadController(example(t, "controller.json"))
	cases := map[string]func(){
		"web without apis":     func() { w.APIs = nil },
		"web with plain http":  func() { w.APIs = []string{"http://api-1.ngc.test:7443"} },
		"api without node":     func() { a.Node = "" },
		"api with short key":   func() { a.NoiseKey = []byte{1} },
		"api with sqlite path": func() { a.DB.Driver, a.DB.Path = DriverSQLite, "" },
		"api with db nodes":    func() { a.DB.Driver, a.DB.Nodes = DriverSQLite, []string{"db-1:3306"} },
		"controller name":      func() { c.Name = "Server 1" },
		"controller api url":   func() { c.APIs[0].URL = "https://api-1.ngc.test:7443/tunnel" },
		"controller parts":     func() { c.Parts = []string{"mail"} },
		"controller db key":    func() { c.DBKey = make([]byte, 32) },
	}
	for name, mutate := range cases {
		w2, a2, c2 := *w, *a, *c
		mutate()
		var err error
		switch {
		case name[:3] == "web":
			err = w.Validate()
		case name[:3] == "api":
			err = a.Validate()
		default:
			err = c.Validate()
		}
		if err != ErrConfig {
			t.Errorf("%s: %v", name, err)
		}
		*w, *a, *c = w2, a2, c2
	}
}

func FuzzLoad(f *testing.F) {
	b, err := os.ReadFile(filepath.Join("..", "testdata", "config", "api.json"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(b)
	f.Fuzz(func(t *testing.T, b []byte) {
		path := filepath.Join(t.TempDir(), "c.json")
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Skip()
		}
		_, _ = LoadWeb(path)
		_, _ = LoadAPI(path)
		_, _ = LoadController(path)
	})
}
