package classes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The version number of this app lives in THREE files, which breaks the rule
// that a number belongs in one place. The exception is deliberate: the window's
// bytes come out of the package the catalogue vouched for, so a window reading
// "Fassung 1.0.1" proves which UI was delivered — and that cannot be proved by
// asking the service, because the service is a different file in the same
// package. What the exception costs is that every raise has to touch three
// files, and the third one is easy to forget: nothing breaks, the window just
// states a version that is not the one running.
//
// These cases are the guard for that. They make a forgotten file red instead of
// silently wrong, so the exception is watched and not merely argued.

// fassungsZeile finds the version the window shows. The paragraph's class is
// part of the pattern: a bare "Fassung 1.0.1" somewhere in a comment must not
// satisfy this case.
var fassungsZeile = regexp.MustCompile(`class="fassung">\s*Fassung\s+(\d+\.\d+\.\d+)\s*<`)

// semver is here so an empty or truncated read cannot pass by being equal to
// another empty read. Three files that all yield "" agree, and agreeing on
// nothing is the one way this case could pass while measuring nothing.
var semver = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// appDir is the app's root as seen from this package's directory, which is
// where `go test` runs.
const appDir = ".."

// readVersionFile returns the contents of VERSION, which is the one place a
// human edits first and the place the build reads.
func readVersionFile(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(appDir, "VERSION"))
	if err != nil {
		t.Fatalf("VERSION: %v", err)
	}
	return strings.TrimSpace(string(b))
}

// readManifestVersion returns the version the installation reads, which decides
// whether a raise is accepted at all: activate.go refuses anything not newer.
func readManifestVersion(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(appDir, "templates", "manifest.json"))
	if err != nil {
		t.Fatalf("manifest.json: %v", err)
	}
	var m struct{ Version string }
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("manifest.json is not readable as JSON: %v", err)
	}
	return strings.TrimSpace(m.Version)
}

// readWindowVersion returns the version a person reads in the window, which is
// the only one of the three that is evidence about the delivered UI.
func readWindowVersion(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(appDir, "templates", "ui", "index.html"))
	if err != nil {
		t.Fatalf("index.html: %v", err)
	}
	m := fassungsZeile.FindSubmatch(b)
	if m == nil {
		t.Fatal(`index.html has no <p class="fassung">Fassung x.y.z</p>; either the line went away or its shape changed, and then the window proves nothing about which UI was delivered`)
	}
	return string(m[1])
}

// TestFassungIstEineZahl is the floor under the comparison below: each of the
// three files must yield something that looks like a version. Without this a
// raise that empties two of them would still be "consistent".
func TestFassungIstEineZahl(t *testing.T) {
	for _, c := range []struct {
		was string
		got string
	}{
		{"VERSION", readVersionFile(t)},
		{"manifest.json", readManifestVersion(t)},
		{"index.html", readWindowVersion(t)},
	} {
		if !semver.MatchString(c.got) {
			t.Errorf("%s = %q, want a version like 1.0.1", c.was, c.got)
		}
	}
}

// TestDreiFassungenStimmenUeberein is the case that catches the forgotten file.
// It names which file disagrees, because "the versions differ" sends a reader
// looking through all three.
func TestDreiFassungenStimmenUeberein(t *testing.T) {
	version := readVersionFile(t)
	if got := readManifestVersion(t); got != version {
		t.Errorf("manifest.json says %q, VERSION says %q; the installation would accept a different version than the one meant", got, version)
	}
	if got := readWindowVersion(t); got != version {
		t.Errorf("index.html says %q, VERSION says %q; the window would state a version that is not the one running, and the window is the evidence about the delivered UI", got, version)
	}
}
