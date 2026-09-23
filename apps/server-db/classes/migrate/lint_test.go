package migrate

import (
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
)

// TestList checks every migration on disk: numbered without a gap, only
// known placeholders, only names from the fixed lists, and nothing that one
// of the five databases would not understand.
func TestList(t *testing.T) {
	files, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no migration found")
	}
	for i, f := range files {
		if f.Version != int64(i+1) {
			t.Errorf("migration %d is numbered %d", i+1, f.Version)
		}
		if len(f.Statements) == 0 {
			t.Errorf("%s has no statement", f.Name)
		}
		for _, s := range f.Statements {
			for _, d := range []sqlstore.Dialect{sqlstore.MariaDB, sqlstore.SQLite} {
				if _, err := sqlstore.Fill(d, s); err != nil {
					t.Errorf("%s: %v", f.Name, err)
				}
			}
		}
	}
}

// TestCheckStatement is the rule set itself, on made-up statements.
func TestCheckStatement(t *testing.T) {
	for _, s := range []string{
		"CREATE TABLE ngc_blob (kind {{key}} NOT NULL, PRIMARY KEY (kind)){{opts}}",
		"CREATE INDEX ngc_blob_seq ON ngc_blob (kind, seq, id)",
		"CREATE UNIQUE INDEX ngc_user_name ON ngc_user (name)",
		"ALTER TABLE ngc_app ADD COLUMN node {{key}} NOT NULL",
		// MariaDB, MySQL and SQLite all take ADD without the word COLUMN, so
		// the rules take it too - and measure the column all the same.
		"ALTER TABLE ngc_app ADD node {{key}} NOT NULL",
	} {
		if err := checkStatement(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	for name, s := range map[string]string{
		"a table nobody listed":  "CREATE TABLE ngc_other (kind {{int}} NOT NULL){{opts}}",
		"a column nobody listed": "CREATE TABLE ngc_blob (nonsense {{key}} NOT NULL){{opts}}",
		"an index nobody listed": "CREATE INDEX ngc_other_seq ON ngc_blob (kind)",
		"a string literal":       "INSERT INTO ngc_blob (kind) VALUES ('x')",
		"dropping a table":       "DROP TABLE ngc_blob",
		"dropping a column":      "ALTER TABLE ngc_app DROP COLUMN node",
		"renaming":               "ALTER TABLE ngc_app RENAME TO ngc_app_old",
		"two statements":         "CREATE INDEX ngc_blob_seq ON ngc_blob (kind); DROP TABLE ngc_blob",
		"an engine of its own":   "CREATE TABLE ngc_blob (kind {{key}} NOT NULL) ENGINE=MyISAM",
		"IF NOT EXISTS":          "CREATE TABLE IF NOT EXISTS ngc_blob (kind {{key}} NOT NULL){{opts}}",
		"AUTO_INCREMENT":         "CREATE TABLE ngc_blob (seq {{int}} NOT NULL AUTO_INCREMENT){{opts}}",
		"an unknown placeholder": "CREATE TABLE ngc_blob (kind {{blob}} NOT NULL){{opts}}",
		// A column added without the word COLUMN sits in no brackets, so only
		// the type in front of it shows what it is.
		"a column added without the word COLUMN": "ALTER TABLE ngc_app ADD nonsense {{key}} NOT NULL",
		// SQL does not care how an identifier is written: Nonsense and
		// NGC_OTHER_X are a column and an index like any other.
		"a column in mixed case": "CREATE TABLE ngc_blob (Nonsense {{key}} NOT NULL){{opts}}",
		"an index in upper case": "CREATE INDEX NGC_OTHER_X ON ngc_blob (kind)",
		// A column may carry the name of a word the rules skip between the
		// columns; it is still a column nobody listed.
		"a column named like a keyword": "CREATE TABLE ngc_blob (key {{key}} NOT NULL){{opts}}",
		"an index on such a column":     "CREATE INDEX ngc_blob_x ON ngc_blob (key)",
		// ENGINE = MyISAM says the same as ENGINE=MyISAM.
		"an engine with spaces": "CREATE TABLE ngc_blob (kind {{key}} NOT NULL) ENGINE = MyISAM",
	} {
		if err := checkStatement(s); err == nil {
			t.Errorf("%s was accepted: %q", name, s)
		}
	}
	// The message has to say what the rule is, not just that something is
	// wrong: whoever wrote the migration reads it.
	if err := checkStatement("DROP TABLE ngc_blob"); err == nil || !strings.Contains(err.Error(), "only add") {
		t.Errorf("the message does not say what the rule is: %v", err)
	}
}
