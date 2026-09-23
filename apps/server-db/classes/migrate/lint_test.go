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
		// The name of an index is measured whether or not it starts with
		// ngc_: x is a name a migration made up like any other, and an index
		// belongs to the table it is named after.
		"an index with a name of its own":       "CREATE INDEX x ON ngc_blob (kind)",
		"a unique index with a name of its own": "CREATE UNIQUE INDEX nonsense ON ngc_blob (kind)",
		// Four kinds of statement are allowed at all. Everything else falls
		// on the first words, however harmless it looks and whatever it
		// creates - nobody has to think of it beforehand.
		"a view":  "CREATE VIEW nonsense AS SELECT kind FROM ngc_blob",
		"a right": "GRANT ALL ON ngc_blob TO nonsense",
		// A table made out of another one: it carries the rows along - TOTP
		// secrets, the owner of a lock - it stands in no list, and because a
		// migration may not DROP, no later one could ever take it away again.
		// It has no bracket, so every rule that reads the brackets looks past
		// it; only the form catches it.
		"a table made from another":   "CREATE TABLE ngc_blob_copy AS SELECT * FROM ngc_blob",
		"a table made from the users": "CREATE TABLE ngc_user_copy AS SELECT id, name, totp FROM ngc_user",
		"a table made from the locks": "CREATE TABLE ngc_lock_copy AS SELECT owner FROM ngc_lock",
		// A dot in front of the name puts the object in another database or
		// schema: the listed name is written down, and something else is what
		// comes into being.
		"a table in another schema":        "CREATE TABLE other.ngc_blob (kind {{key}} NOT NULL){{opts}}",
		"an index in another schema":       "CREATE UNIQUE INDEX ngc_blob_x ON other.ngc_blob (kind)",
		"a column added in another schema": "ALTER TABLE other.ngc_blob ADD kind {{key}} NOT NULL",
		"a column with the table in front": "CREATE INDEX ngc_blob_x ON ngc_blob (ngc_blob.kind)",
		// MariaDB takes $ and every letter above ASCII in a name that stands
		// without quotes, so ngc_blob$x and ngc_blobae are tables of their
		// own - and in no list.
		"a table name with a dollar sign": "CREATE TABLE ngc_blob$x (kind {{key}} NOT NULL){{opts}}",
		"a table name outside ASCII":      "CREATE TABLE ngc_blobä (kind {{key}} NOT NULL){{opts}}",
		// Whatever follows the form is something no rule has read.
		"a column added the MySQL way":   "ALTER TABLE ngc_app ADD COLUMN node {{key}} NOT NULL AFTER id",
		"an index without columns":       "CREATE INDEX ngc_blob_x ON ngc_blob",
		"an index with a way of its own": "CREATE INDEX ngc_blob_x ON ngc_blob (kind) USING BTREE",
		// MariaDB runs what stands in /*! ... */ and reads it as SQL.
		"a comment MariaDB runs": "CREATE INDEX ngc_blob_x ON ngc_blob (kind) /*!*/",
		// SQLite takes one ADD in a statement, so the form takes one.
		"two columns added at once": "ALTER TABLE ngc_app ADD COLUMN node {{key}} NOT NULL, ADD COLUMN id {{key}} NOT NULL",
		// A table written in another case is another table: MariaDB on Linux
		// tells ngc_blob and NGC_BLOB apart.
		"a table in upper case": "CREATE TABLE NGC_BLOB (kind {{key}} NOT NULL){{opts}}",
		// Without {{opts}} MariaDB takes whatever engine the server has set
		// as its default - and that is the one thing Galera does not
		// replicate.
		"a table without the ending": "CREATE TABLE ngc_blob (kind {{key}} NOT NULL)",
		"a table without a column":   "CREATE TABLE ngc_blob (PRIMARY KEY (kind)){{opts}}",
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
	// A word that does not belong is refused, and the message names it: who
	// wrote the migration has to see where to look. WHERE is nothing anybody
	// meant as a column, and a message calling it one would send the author
	// looking in the wrong place.
	err := checkStatement("CREATE INDEX ngc_blob_x ON ngc_blob (kind) WHERE nonsense")
	if err == nil {
		t.Fatal("a statement with a WHERE was accepted")
	}
	if !strings.Contains(err.Error(), `"WHERE"`) || !strings.Contains(err.Error(), "over") {
		t.Errorf("the message does not say that a word stands where the statement is over: %v", err)
	}
	// A name standing where a column belongs is named as what it is not.
	if err := checkStatement("CREATE INDEX ngc_blob_x ON ngc_blob (nonsense)"); err == nil ||
		!strings.Contains(err.Error(), "no column of") {
		t.Errorf("the message does not say that the name is no column: %v", err)
	}
}

// TestAColumnTypeIsNeverAWord: a column type is a piece of the statement in
// its own right, never a word of it. So no rule can hang on what the
// placeholders happen to be called - {{add}} does not turn the word behind it
// into a column - and the ending of a CREATE TABLE cannot stand where a
// column type belongs, where Fill would write the engine of the dialect into
// the middle of the column list.
func TestAColumnTypeIsNeverAWord(t *testing.T) {
	for name, s := range map[string]string{
		"a type nobody listed":            "CREATE TABLE ngc_blob (kind {{add}} NOT NULL){{opts}}",
		"a type where the column belongs": "ALTER TABLE ngc_app ADD {{key}} node NOT NULL",
		"the ending as a column type":     "CREATE TABLE ngc_blob (kind {{opts}} NOT NULL){{opts}}",
	} {
		if err := checkStatement(s); err == nil {
			t.Errorf("%s was accepted: %q", name, s)
		}
	}
}
