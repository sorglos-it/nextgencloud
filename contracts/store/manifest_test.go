package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func terminalManifest() *Manifest {
	return &Manifest{
		ID: "org.sorglos.terminal", Name: "Terminal", Version: "1.0.0", Publisher: "Sorglos IT",
		Icon: "icon.svg", UI: &UI{Entry: "index.html", Width: 720, Height: 480},
		Actions:  []Action{{Name: "shell", Title: "Befehle auf dem Server ausführen", Service: "terminal", Stream: true}},
		Services: []Service{{Name: "terminal", Command: []string{"/usr/local/bin/ngc-terminal"}}},
		Memory:   128,
	}
}

func TestManifest(t *testing.T) {
	b, _ := json.Marshal(terminalManifest())
	m, err := ParseManifest(b)
	if err != nil {
		t.Fatal(err)
	}
	if a, ok := m.Action("shell"); !ok || !a.Stream {
		t.Error("action lookup")
	}
	calculator := &Manifest{ID: "org.sorglos.calculator", Name: "Rechner", Version: "1.0.0", Publisher: "Sorglos IT", UI: &UI{Entry: "index.html"}}
	if err := calculator.Validate(); err != nil {
		t.Errorf("UI-only app: %v", err)
	}
}

func TestManifestRejects(t *testing.T) {
	cases := map[string]func(m *Manifest){
		"core app id":         func(m *Manifest) { m.ID = "ngc.store" },
		"version":             func(m *Manifest) { m.Version = "1.0" },
		"leading zero":        func(m *Manifest) { m.Version = "1.01.0" },
		"path traversal":      func(m *Manifest) { m.UI.Entry = "../index.html" },
		"absolute icon":       func(m *Manifest) { m.Icon = "/etc/passwd" },
		"hidden file":         func(m *Manifest) { m.Icon = ".icon.svg" },
		"unknown service":     func(m *Manifest) { m.Actions[0].Service = "mail" },
		"stream with timeout": func(m *Manifest) { m.Actions[0].Timeout = 10 },
		"relative command":    func(m *Manifest) { m.Services[0].Command = []string{"ngc-terminal"} },
		"duplicate action":    func(m *Manifest) { m.Actions = append(m.Actions, m.Actions[0]) },
		"unknown need":        func(m *Manifest) { m.Needs = []string{"host"} },
		"lxc not yet":         func(m *Manifest) { m.Runtime = "lxc" },
		"start is a stream":   func(m *Manifest) { m.Start = "shell" },
		"memory":              func(m *Manifest) { m.Memory = 1 },
		"nothing to do":       func(m *Manifest) { m.UI, m.Actions, m.Services = nil, nil, nil },
		"ui width":            func(m *Manifest) { m.UI.Width = 100 },
		"cpu":                 func(m *Manifest) { m.CPU = 50 },
		"minNgc":              func(m *Manifest) { m.MinNGC = "abc" },
		"description":         func(m *Manifest) { m.Description = strings.Repeat("a", 1001) },
		"bare root command":   func(m *Manifest) { m.Services[0].Command = []string{"/"} },
		"too many actions": func(m *Manifest) {
			m.Actions = nil
			for i := 0; i <= MaxActions; i++ {
				m.Actions = append(m.Actions, Action{Name: fmt.Sprintf("a%d", i), Title: "t", Service: "terminal"})
			}
		},
		"too many services": func(m *Manifest) {
			m.Services = nil
			for i := 0; i <= MaxServices; i++ {
				m.Services = append(m.Services, Service{Name: fmt.Sprintf("s%d", i), Command: []string{"/bin/x"}})
			}
		},
	}
	for name, mutate := range cases {
		m := terminalManifest()
		mutate(m)
		if err := m.Validate(); err != ErrManifest {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ParseManifest([]byte(`{"id":"org.sorglos.terminal","script":"rm -rf /"}`)); err != ErrManifest {
		t.Errorf("unknown field: %v", err)
	}
	b, _ := json.Marshal(terminalManifest())
	b = append(b[:len(b)-1], []byte(`,"unknown":true}`)...)
	if _, err := ParseManifest(b); err != ErrManifest {
		t.Errorf("unknown field in an otherwise valid manifest: %v", err)
	}
}

func TestVersions(t *testing.T) {
	cases := []struct {
		a, b  string
		newer bool
	}{
		{"1.0.1", "1.0.0", true},
		{"1.10.0", "1.9.9", true},
		{"2.0.0", "10.0.0", false},
		{"1.0.0", "1.0.0", false},
		{"0.9.0", "1.0.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.newer {
			t.Errorf("Newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
	for _, v := range []string{"1", "1.0", "v1.0.0", "1.0.0-beta", "01.0.0", "1.0.0.0", "1234567.0.0"} {
		if ValidVersion(v) {
			t.Errorf("%s accepted", v)
		}
	}
}

func FuzzParseManifest(f *testing.F) {
	b, _ := json.Marshal(terminalManifest())
	f.Add(b)
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParseManifest(b)
	})
}
