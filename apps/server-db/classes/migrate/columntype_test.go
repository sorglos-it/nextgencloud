package migrate

import (
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
)

// columnsOf reads the columns and their placeholders out of one statement of a
// migration. It answers nil for a statement that makes no column.
//
// It is built out of the helpers already.go uses to compare a statement
// against a database - norm, cut, pieces - so this test reads a statement the
// same way the runner does. A piece that is not "<name> {{<type>}} ..." is no
// column: the key of a table is written PRIMARY KEY ( ... ), and that bracket
// is one piece of its own.
func columnsOf(stmt string) (table string, cols map[string]string) {
	s := norm(stmt)
	switch {
	case strings.HasPrefix(s, "CREATE TABLE "):
		head, body, ok := cut(s)
		if !ok {
			return "", nil
		}
		table = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(head, "CREATE TABLE "), "("))
		cols = map[string]string{}
		for _, p := range pieces(body) {
			if name, typ, ok := colPiece(p); ok {
				cols[name] = typ
			}
		}
		return table, cols
	case strings.HasPrefix(s, "ALTER TABLE "):
		name, def, ok := alterParts(s)
		if !ok {
			return "", nil
		}
		col, typ, ok := colPiece(def)
		if !ok {
			return "", nil
		}
		return name, map[string]string{col: typ}
	}
	return "", nil
}

// colPiece reads "<name> {{<type>}} ..." and gives back the two names.
func colPiece(p string) (name, typ string, ok bool) {
	f := strings.Fields(p)
	if len(f) < 2 || !strings.HasPrefix(f[1], "{{") || !strings.HasSuffix(f[1], "}}") {
		return "", "", false
	}
	return f[0], strings.TrimSuffix(strings.TrimPrefix(f[1], "{{"), "}}"), true
}

// TestColumnTypeIsWhatTheMigrationsWrite is the half of the check on
// sqlstore.ColumnType that says whether an entry is RIGHT: every column of
// every statement the migrations really carry, plus the runner's own two
// tables, against the entry for it. sqlstore's
// TestColumnTypeCoversEveryColumn holds the other half, the shape.
//
// Why it reads the statements instead of a list: the placeholder a column was
// made with lived only in the TEXT of the .sql until 30.10.2026, and a second
// copy of a fact is a copy that drifts. This test is what keeps the data and
// the file one thing.
func TestColumnTypeIsWhatTheMigrationsWrite(t *testing.T) {
	files, err := List()
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{createSchema, createLock}
	for _, f := range files {
		stmts = append(stmts, f.Statements...)
	}

	seen := map[string]bool{}
	for _, stmt := range stmts {
		table, cols := columnsOf(stmt)
		if table == "" {
			continue
		}
		want, ok := sqlstore.ColumnType[table]
		if !ok {
			t.Errorf("statement makes table %q, sqlstore.ColumnType does not know it", table)
			continue
		}
		for col, typ := range cols {
			seen[table+"."+col] = true
			if got := want[col]; got != typ {
				t.Errorf("%s.%s: the migration writes {{%s}}, sqlstore.ColumnType says %q",
					table, col, typ, got)
			}
		}
	}

	// The reading above has to have worked, or this test is green over
	// nothing. 0001 is the whole schema of NGC, so every column of every
	// listed table has to have been met.
	if len(seen) == 0 {
		t.Fatal("no column was read out of any statement: the reading above is broken, not the data")
	}
	for _, table := range sqlstore.Tables {
		for _, col := range sqlstore.Columns[table] {
			if !seen[table+"."+col] {
				t.Errorf("no statement makes %s.%s, so nothing holds its entry", table, col)
			}
		}
	}
	t.Logf("%d columns read out of %d statements", len(seen), len(stmts))
}
