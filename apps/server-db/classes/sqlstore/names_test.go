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
	for i, table := range Tables {
		if slices.Contains(Tables[:i], table) {
			t.Errorf("Tables names %q twice", table)
		}
		cols, ok := Columns[table]
		if !ok || len(cols) == 0 {
			t.Errorf("table %q has no columns", table)
		}
		checkIdent(t, table)
		for j, c := range cols {
			checkIdent(t, c)
			if slices.Contains(cols[:j], c) {
				t.Errorf("table %q names column %q twice", table, c)
			}
		}
	}
	// Both directions, because counting alone lets a doubled entry in Tables
	// pay for a key in Columns that no table of ours is called.
	for table := range Columns {
		if !slices.Contains(Tables, table) {
			t.Errorf("Columns knows table %q, Tables does not", table)
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
	// The ending carries no space of its own: a statement runs with one
	// between every two of its pieces, so the space stands in front of the
	// {{opts}} and not inside it.
	got, err := Fill(MariaDB, "CREATE TABLE ngc_lock (name {{key}} NOT NULL, until {{int}} NOT NULL) {{opts}}")
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

// TestPlaceholders keeps the list and the dialects from drifting apart. Fill
// asks the dialect and never the list, so a type a dialect knew but the list
// did not would slip through unseen. Every name of the list has to work in a
// migration; the other half can only sample, because no test can try every
// string a case could be written for.
func TestPlaceholders(t *testing.T) {
	for i, p := range Placeholders {
		if slices.Contains(Placeholders[:i], p) {
			t.Errorf("Placeholders names %q twice", p)
		}
	}
	for _, d := range []Dialect{MariaDB, SQLite} {
		for _, p := range Placeholders {
			if _, err := Fill(d, "x {{"+p+"}}"); err != nil {
				t.Errorf("%T: %q of Placeholders was refused: %v", d, p, err)
			}
			// "opts" is the one name a dialect may answer with nothing.
			if d.Type(p) == "" && p != "opts" {
				t.Errorf("%T has no type for %q", d, p)
			}
		}
		for _, p := range []string{"", " ", "blob", "varchar", "KEY", "key ", "Int", "opt", "options"} {
			if d.Type(p) != "" {
				t.Errorf("%T answers %q, which is not in Placeholders", d, p)
			}
			if _, err := Fill(d, "x {{"+p+"}}"); err == nil {
				t.Errorf("%T: Fill took %q, which is not in Placeholders", d, p)
			}
		}
	}
}

// TestColumnTypeCoversEveryColumn holds the shape of ColumnType: one entry per
// column of every table, and no entry for anything else. It says nothing about
// whether an entry is RIGHT - that is measured against the statements the
// migrations really carry, in migrate's
// TestColumnTypeIsWhatTheMigrationsWrite. Both halves are needed: this one
// catches a column added to Columns and forgotten here, that one catches an
// entry that names the wrong type.
func TestColumnTypeCoversEveryColumn(t *testing.T) {
	for _, table := range Tables {
		types, ok := ColumnType[table]
		if !ok {
			t.Errorf("ColumnType does not know table %q", table)
			continue
		}
		for _, c := range Columns[table] {
			p, ok := types[c]
			if !ok {
				t.Errorf("ColumnType does not know %s.%s", table, c)
				continue
			}
			// "opts" is the suffix of CREATE TABLE, never a column, so a
			// column that claims it is as wrong as one with a typo.
			if p == "opts" || !slices.Contains(Placeholders, p) {
				t.Errorf("%s.%s has placeholder %q, which is not a column type", table, c, p)
			}
		}
		for c := range types {
			if !slices.Contains(Columns[table], c) {
				t.Errorf("ColumnType knows %s.%s, Columns does not", table, c)
			}
		}
	}
	for table := range ColumnType {
		if !slices.Contains(Tables, table) {
			t.Errorf("ColumnType knows table %q, Tables does not", table)
		}
	}
}
