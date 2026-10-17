package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRunKnowsFourWaysAndNoMore nails the command line down: what exists,
// what the exit code of each is, and that anything else is refused instead of
// being quietly taken for "run the API".
func TestRunKnowsFourWaysAndNoMore(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "api.json")
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("the case did not set up what it measures")
	}
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"version", []string{"-version"}, 0},
		// With a configuration that is not there, every way that reads one
		// stops at 1. That is what makes them measurable here at all: without
		// a database and without a listener none of them could run through,
		// and a case that pretended otherwise would measure nothing.
		{"run", nil, 1},
		{"migrate", []string{"migrate"}, 1},
		{"backup", []string{"backup", "/tmp/sicherung.db"}, 1},
		// A backup without a target is refused at the command line and never
		// reaches a default path: the admin would not see which file they got.
		{"backup without a target", []string{"backup"}, 2},
		{"backup with two targets", []string{"backup", "/tmp/a.db", "/tmp/b.db"}, 2},
		{"join", []string{"-join", "NGCJOIN-xyz"}, 1},
		{"an unknown subcommand", []string{"zerstoere-alles"}, 2},
		{"a subcommand with an argument too many", []string{"migrate", "now"}, 2},
		{"an unknown flag", []string{"-wipe"}, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := append([]string{"-config", missing}, c.args...)
			if got := run(args); got != c.want {
				t.Fatalf("run(%v): got %d, want %d", args, got, c.want)
			}
		})
	}
}

// TestMigrateTakesTheConfigurationBehindIt is the reason subcommand exists:
// flag.FlagSet stops at the first argument that is no flag, so with the
// subcommand left to it "ngc-api migrate -config other.json" would migrate
// the DEFAULT database and say nothing about it.
func TestMigrateTakesTheConfigurationBehindIt(t *testing.T) {
	named := filepath.Join(t.TempDir(), "other.json")
	rest, cmd, _, ok := subcommand([]string{"migrate", "-config", named})
	if !ok || cmd != "migrate" {
		t.Fatalf("subcommand: cmd %q, ok %v", cmd, ok)
	}
	if len(rest) != 2 || rest[0] != "-config" || rest[1] != named {
		t.Fatalf("the flags behind the subcommand were lost: %v", rest)
	}
	// And the other way round: a value that happens to read like a
	// subcommand belongs to its flag and is not one.
	rest, cmd, _, ok = subcommand([]string{"-config", "migrate"})
	if !ok || cmd != "" {
		t.Fatalf("the value of -config was taken for a subcommand: cmd %q", cmd)
	}
	if len(rest) != 2 || rest[1] != "migrate" {
		t.Fatalf("rest %v", rest)
	}
}

// TestBackupTakesItsTargetAndTheConfigurationBehindIt is the same measured
// reason TestMigrateTakesTheConfigurationBehindIt names, with one more thing
// to lose: the target. "ngc-api backup /mnt/stick/a.db -config other.json"
// has to reach backupRun with BOTH, or it copies the wrong database or writes
// to the wrong file -- and either way it reports success.
func TestBackupTakesItsTargetAndTheConfigurationBehindIt(t *testing.T) {
	named := filepath.Join(t.TempDir(), "other.json")
	const target = "/mnt/stick/a.db"
	rest, cmd, arg, ok := subcommand([]string{"backup", target, "-config", named})
	if !ok || cmd != "backup" {
		t.Fatalf("subcommand: cmd %q, ok %v", cmd, ok)
	}
	if arg != target {
		t.Fatalf("the target became %q", arg)
	}
	if len(rest) != 2 || rest[0] != "-config" || rest[1] != named {
		t.Fatalf("the flags behind the subcommand were lost: %v", rest)
	}
	// The target may stand behind the flags as well, and it is still the
	// target and not a second subcommand.
	_, cmd, arg, ok = subcommand([]string{"backup", "-config", named, target})
	if !ok || cmd != "backup" || arg != target {
		t.Fatalf("target behind the flags: cmd %q, arg %q, ok %v", cmd, arg, ok)
	}
	// And the value of -config is never the target.
	_, _, arg, ok = subcommand([]string{"backup", "-config", target, "/mnt/b.db"})
	if !ok || arg != "/mnt/b.db" {
		t.Fatalf("the value of -config was taken for the target: arg %q", arg)
	}
}
