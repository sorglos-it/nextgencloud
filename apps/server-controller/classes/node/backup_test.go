package node

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// controllerWithADatabase writes a controller.json whose database really
// exists and holds an app, and gives both paths back. The database is made
// through state.Open, so what the cases copy is a file of the format this
// controller runs on and not something shaped like one.
//
// It cannot run on Windows, and that is not a weakness of the case but two
// lines of the contract pulling in opposite directions: config.DB.Validate
// wants a path that begins with "/" (shared/config/config.go, the check on
// DB.Path) and sqlite.validPath wants filepath.IsAbs
// (apps/server-db/classes/sqlite/sqlite.go, validPath). No Windows path is
// both. So the cases that need a real file on disk say so and skip HERE,
// loudly, instead of passing on a platform where they measured nothing --
// and the numbers they stand for were taken on linux/amd64 (WSL, ext4) and
// on the running installation, not on C: and not on Z:.
func controllerWithADatabase(t *testing.T) (cfgPath, dbPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skipf("a controller database cannot be named on windows: config.DB.Validate wants a leading %q and sqlite.validPath wants filepath.IsAbs - measured on linux/amd64 instead", "/")
	}
	dir := t.TempDir()
	cfgPath = filepath.Join(dir, "controller.json")
	c := writeController(t, cfgPath)
	dbPath = filepath.Join(dir, "state.db")
	c.DB = dbPath
	if err := config.SaveController(cfgPath, c); err != nil {
		t.Fatalf("the case did not set up what it measures: %v", err)
	}
	cfg := config.DB{Driver: config.DriverSQLite, Path: dbPath, Key: c.DBKey}
	st, err := state.Open(cfg, "ngc-controller/test", 1_790_000_000)
	if err != nil {
		t.Fatalf("the case did not set up what it measures: %v", err)
	}
	if err := st.PutApp(context.Background(), dbapi.App{
		ID: "org.sorglos.notes", Version: "1.0.0", Store: "global"}); err != nil {
		t.Fatalf("the case did not set up what it measures: %v", err)
	}
	st.Close()
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("the case did not set up what it measures: %v", err)
	}
	return cfgPath, dbPath
}

// TestMainBackupWritesTheCopyAndWarnsAboutTheKey is the whole command: the
// file is written, and the sentence about the key is said. The sentence is
// measured and not assumed, because it is the only thing standing between a
// backup plan and a backup nobody can open: the copy is encrypted with dbKey
// of controller.json and carries nothing of it.
func TestMainBackupWritesTheCopyAndWarnsAboutTheKey(t *testing.T) {
	cfgPath, dbPath := controllerWithADatabase(t)
	target := filepath.Join(filepath.Dir(dbPath), "sicherung.db")
	var out, errOut bytes.Buffer
	if code := Main("1.2.3", []string{"-config", cfgPath, "backup", target}, noInput(), &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr %q", code, errOut.String())
	}
	st, err := os.Stat(target)
	if err != nil || st.Size() == 0 {
		t.Fatalf("the backup %q: %v", target, err)
	}
	got := out.String()
	if !strings.Contains(got, target) {
		t.Errorf("the answer does not name the file that was written: %q", got)
	}
	// The three things the sentence has to carry: that the file is encrypted,
	// WHERE the key is, and that nobody opens it without that file.
	for _, want := range []string{"verschlüsselt", cfgPath, "auf keinem"} {
		if !strings.Contains(got, want) {
			t.Errorf("the warning is missing %q: %q", want, got)
		}
	}
	// And what must never be in it: the key itself. config.Secret prints
	// "[secret]", so a key that reached the output would show up as that.
	if strings.Contains(got, "[secret]") || strings.Contains(errOut.String(), "[secret]") {
		t.Errorf("a secret reached the output: %q %q", got, errOut.String())
	}
}

// TestMainBackupRefusesACallWithoutOneFile holds the command line down. A
// backup is not a call with a default: a file written under a name the admin
// never saw is a backup nobody finds again, and two names would write one of
// the two silently.
func TestMainBackupRefusesACallWithoutOneFile(t *testing.T) {
	// No database here on purpose: the call is answered before the
	// configuration is read, so this case runs on every platform and holds
	// the command line even where a file cannot be named.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "controller.json")
	writeController(t, cfgPath)
	cases := [][]string{
		{"backup"},
		{"backup", filepath.Join(dir, "a.db"), filepath.Join(dir, "b.db")},
		{"backup", ""},
		{"backup", "-out"},
	}
	for _, args := range cases {
		var out, errOut bytes.Buffer
		code := Main("1.2.3", append([]string{"-config", cfgPath}, args...), noInput(), &out, &errOut)
		if code != 2 {
			t.Errorf("Main(%q) = %d, want 2", args, code)
		}
		if !strings.Contains(errOut.String(), "backup <Datei>") {
			t.Errorf("Main(%q) did not say how the call goes: %q", args, errOut.String())
		}
		// Nothing was written, under no name of the two.
		for _, p := range []string{filepath.Join(dir, "a.db"), filepath.Join(dir, "b.db")} {
			if _, err := os.Stat(p); err == nil {
				t.Errorf("Main(%q) wrote %q although the call was refused", args, p)
			}
		}
	}
}

// TestMainBackupWithoutAConfigurationSaysSo: the configuration is read before
// anything is copied, and a missing one is this server's own state, not a
// broken database. The one fixed line is the same one pair gives.
func TestMainBackupWithoutAConfigurationSaysSo(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "controller.json")
	target := filepath.Join(dir, "sicherung.db")
	var out, errOut bytes.Buffer
	if code := Main("1.2.3", []string{"-config", missing, "backup", target}, noInput(), &out, &errOut); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if got := errOut.String(); got != msgNoConfig+"\n" {
		t.Fatalf("stderr = %q, want %q", got, msgNoConfig+"\n")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("a backup was written without a configuration: %v", err)
	}
}

// TestTheUsageNamesTheBackup: the list of commands is where an admin looks,
// and a command that is not in it does not exist for them. This is the half
// of the finding that was never code -- sqlite.Backup was built and no
// program offered it.
func TestTheUsageNamesTheBackup(t *testing.T) {
	var out bytes.Buffer
	if code := Main("1.2.3", []string{"-h"}, noInput(), &out, &out); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, want := range []string{"backup <Datei>", "sichern"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the list of commands is missing %q:\n%s", want, out.String())
		}
	}
}
