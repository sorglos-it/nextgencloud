package main

import (
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/service"
)

// The folder comes from the contract, not from a copy of it.
const dataDir = service.DataDir

// TestUnderRefusesEscapes: the file name comes from the browser, so every way
// out of the data folder, and every name nobody typed on purpose, is refused
// rather than quietly turned into another file.
func TestUnderRefusesEscapes(t *testing.T) {
	for _, name := range []string{
		"", "/etc/passwd", "../etc/passwd", "../../etc/passwd",
		"a/../../etc/passwd", "./../x", "a//b.docx", "briefe/", "./a.docx",
	} {
		if got, err := under(dataDir, name); err == nil {
			t.Errorf("%q was taken as %q, want refused", name, got)
		}
	}
}

// TestUnderTakesPlainNames: nothing honest is refused, and the result is a
// Linux path whatever machine this was built on.
func TestUnderTakesPlainNames(t *testing.T) {
	for name, want := range map[string]string{
		"probe.docx":    dataDir + "/probe.docx",
		"briefe/a.docx": dataDir + "/briefe/a.docx",
		"a.b.c-1.docx":  dataDir + "/a.b.c-1.docx",
	} {
		got, err := under(dataDir, name)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", name, got, err, want)
		}
	}
}

// TestArgvUnknownAction: an action the manifest does not name must not reach
// the foreign program. "mcp" is the one that matters - it is a real
// subcommand of the tool and would open a server.
func TestArgvUnknownAction(t *testing.T) {
	for _, action := range []string{"mcp", "run", "", "TEXT"} {
		if got, err := argv(action, dataDir, params{File: "a.docx"}); err == nil {
			t.Errorf("the action %q was taken as %v, want refused", action, got)
		}
	}
}

// TestArgvConvertNeedsOut: convert without a target must not silently become
// a one-argument call.
func TestArgvConvertNeedsOut(t *testing.T) {
	if _, err := argv("convert", dataDir, params{File: "a.docx"}); err == nil {
		t.Error("convert without out was taken, want refused")
	}
}

// TestArgvPassesNoFlags: a file name that reads like an option must not
// arrive as one. The argument vector is built by position, so the tool never
// sees it as a flag - this holds that.
func TestArgvPassesNoFlags(t *testing.T) {
	got, err := argv("text", dataDir, params{File: "a.docx"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "text" || got[1] != dataDir+"/a.docx" {
		t.Errorf("got %q", got)
	}
}
