package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/join"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
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

// Every category of join.Run gets its own line, and a failure the program
// does not know gets the cautious one. The line this replaces printed the
// error of net/http word for word into the journal, and that error carries
// the address of the issuing API together with the hash of the one-time
// secret the API spends.
func TestJoinAnswersEachFailureWithItsOwnLine(t *testing.T) {
	seen := map[string]error{}
	for _, c := range []struct {
		err   error
		want  string
		again bool // meant to land on a line already taken
	}{
		{join.ErrJoinInput, msgJoinNothing, false},
		{tunnel.ErrJoinCode, msgJoinBadCode, false},
		{join.ErrJoinNoAnswer, msgJoinNoAnswer, false},
		{join.ErrJoinTooSlow, msgJoinTooSlow, false},
		{join.ErrJoinWrongAPI, msgJoinWrongAPI, false},
		{join.ErrJoinRefused, msgJoinRefused, false},
		{join.ErrJoinSecret, msgJoinSecret, false},
		{join.ErrJoinTimeout, msgJoinNotListed, false},
		{errors.New("something nobody wrote a line for"), msgJoinFailed, false},
		// Wrapped is how it really arrives when a caller adds its own word.
		{fmt.Errorf("while joining: %w", join.ErrJoinNoAnswer), msgJoinNoAnswer, true},
	} {
		got := joinFailure(c.err)
		if got != c.want {
			t.Errorf("joinFailure(%v) = %q, want %q", c.err, got, c.want)
		}
		if c.again {
			continue
		}
		if other, ok := seen[got]; ok {
			t.Errorf("joinFailure gives the same line to %v and to %v", c.err, other)
		}
		seen[got] = c.err
	}
	// Eight categories plus the cautious line. Two that fell together would
	// be a category that tells the admin nothing new.
	if len(seen) != 9 {
		t.Errorf("the lines are %d different ones, want 9", len(seen))
	}
}

// What the admin reads in the journal may carry no value, and this checks for
// the ABSENCE of each: the address of the API, the path of the files the two
// private keys go to, the join code and the hash of its one-time secret that
// the query names. The values are built here and looked for; none of them is
// printed, because a journal line and a test report travel the same way.
func TestJoinNamesNoValueWhateverGoesWrong(t *testing.T) {
	secret := bytes.Repeat([]byte{7}, tunnel.SecretSize)
	sum := sha256.Sum256(secret)
	quiet := []struct{ what, text string }{
		{"hash of the one-time secret", base64.RawURLEncoding.EncodeToString(sum[:])},
		{"one-time secret", base64.RawURLEncoding.EncodeToString(secret)},
		{"path of the Noise key", joinNoiseFile},
		{"path of the signing key", joinSignFile},
		{"file of the fetched keys", join.SecretFile},
		{"path of the join request", tunnel.PathJoin},
		{"directory of the run files", config.RunDir},
	}
	lines := []string{msgJoinFailed}
	for _, err := range []error{
		join.ErrJoinInput, tunnel.ErrJoinCode, join.ErrJoinNoAnswer, join.ErrJoinTooSlow,
		join.ErrJoinWrongAPI, join.ErrJoinRefused, join.ErrJoinSecret, join.ErrJoinTimeout,
	} {
		lines = append(lines, joinFailure(err))
	}
	for i, line := range lines {
		for _, q := range quiet {
			if len(q.text) >= 4 && strings.Contains(line, q.text) {
				t.Errorf("line %d names the %s", i, q.what)
			}
		}
		// A line that does not say what to do next is the line this replaced.
		if !strings.Contains(line, "Bitte ") {
			t.Errorf("line %d says nothing to do next", i)
		}
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
