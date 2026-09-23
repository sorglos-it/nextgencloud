// Package migrate applies the numbered migrations of apps/server-db/sql. They
// belong to the version of ngc-api and travel inside its release file: one
// controller with role db runs them before the new API starts, and the
// installation runs them once.
package migrate

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	migrations "github.com/sorglos-it/nextgencloud/apps/server-db/sql"
)

// File is one migration: its number, its file name and its statements in
// order.
type File struct {
	Version    int64
	Name       string
	Statements []string
}

// List reads every migration and checks it against the rules. A migration
// that breaks one is an error, so a wrong file never reaches a database.
func List() ([]File, error) {
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return nil, err
	}
	var out []File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || path.Ext(name) != ".sql" {
			continue
		}
		number, _, ok := strings.Cut(name, "-")
		version, err := strconv.ParseInt(number, 10, 64)
		if !ok || err != nil || version < 1 || len(number) != 4 {
			return nil, fmt.Errorf("migrate: %q is not named 0001-<name>.sql", name)
		}
		body, err := migrations.Files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		stmts, err := split(string(body))
		if err != nil {
			return nil, fmt.Errorf("migrate: %s: %w", name, err)
		}
		out = append(out, File{Version: version, Name: name, Statements: stmts})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i, f := range out {
		if f.Version != int64(i+1) {
			return nil, fmt.Errorf("migrate: migration %d is missing", i+1)
		}
	}
	return out, nil
}

// split cuts a migration into statements. A statement ends at a semicolon;
// checkStatement refuses quotes, so a semicolon can never sit inside a value.
// A line that starts with -- is a comment.
func split(body string) ([]string, error) {
	var keep []string
	for _, line := range strings.Split(body, "\n") {
		if t := strings.TrimSpace(line); t != "" && !strings.HasPrefix(t, "--") {
			keep = append(keep, line)
		}
	}
	var out []string
	for _, raw := range strings.Split(strings.Join(keep, "\n"), ";") {
		stmt := strings.Join(strings.Fields(raw), " ")
		if stmt == "" {
			continue
		}
		if err := checkStatement(stmt); err != nil {
			return nil, err
		}
		out = append(out, stmt)
	}
	return out, nil
}
