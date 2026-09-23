package sqlstore

import (
	"slices"
	"strings"
	"testing"
)

// TestNames guards the rule "table and column names only from fixed lists":
// every name is a plain lower-case identifier, short enough for Oracle's
// 30 characters, and none is a word one of the five databases reserves.
func TestNames(t *testing.T) {
	for _, table := range Tables {
		cols, ok := Columns[table]
		if !ok || len(cols) == 0 {
			t.Errorf("table %q has no columns", table)
		}
		checkIdent(t, table)
		for i, c := range cols {
			checkIdent(t, c)
			if slices.Contains(cols[:i], c) {
				t.Errorf("table %q names column %q twice", table, c)
			}
		}
	}
	if len(Columns) != len(Tables) {
		t.Errorf("Columns has %d tables, Tables %d", len(Columns), len(Tables))
	}
}

func checkIdent(t *testing.T, s string) {
	t.Helper()
	switch {
	case s == "" || len(s) > 30:
		t.Errorf("%q: 1 to 30 bytes", s)
	case s[0] < 'a' || s[0] > 'z':
		t.Errorf("%q: must start with a letter", s)
	case strings.Trim(s, "abcdefghijklmnopqrstuvwxyz0123456789_") != "":
		t.Errorf("%q: only a-z, 0-9 and _", s)
	case slices.Contains(reserved, strings.ToUpper(s)):
		t.Errorf("%q is reserved in one of the five databases", s)
	}
}

// TestFill checks that a migration is filled from the dialect's table only.
func TestFill(t *testing.T) {
	got, err := Fill(MariaDB, "CREATE TABLE ngc_lock (name {{key}} NOT NULL, until {{int}} NOT NULL){{opts}}")
	want := "CREATE TABLE ngc_lock (name VARBINARY(255) NOT NULL, until BIGINT NOT NULL) ENGINE=InnoDB ROW_FORMAT=DYNAMIC"
	if err != nil || got != want {
		t.Errorf("MariaDB: %q, %v; want %q", got, err, want)
	}
	if got, err := Fill(SQLite, "a {{key}} b {{int}}{{opts}}"); err != nil || got != "a BLOB b INTEGER" {
		t.Errorf("SQLite: %q, %v", got, err)
	}
	for _, bad := range []string{"x {{blob}}", "x {{}}", "x {{ key }}", "x {{key"} {
		if _, err := Fill(MariaDB, bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}
