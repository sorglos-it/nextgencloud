package migrate

import (
	"math/rand/v2"
	"slices"
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
		if _, err := checkStatement(s); err != nil {
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
		// A PRIMARY KEY is a rule of the table and stands last and once.
		// SQLite answers anything else with a syntax error, which the runner
		// would only find out on the database, in the middle of a migration.
		"a PRIMARY KEY in front of the columns": "CREATE TABLE ngc_blob (PRIMARY KEY (kind), kind {{key}} NOT NULL){{opts}}",
		"a column behind the PRIMARY KEY":       "CREATE TABLE ngc_blob (kind {{key}} NOT NULL, PRIMARY KEY (kind), id {{key}} NOT NULL){{opts}}",
		"two PRIMARY KEY":                       "CREATE TABLE ngc_blob (kind {{key}} NOT NULL, PRIMARY KEY (kind), PRIMARY KEY (id)){{opts}}",
		// A name given twice is where the five databases stop agreeing:
		// SQLite takes an index on one column twice, MariaDB refuses it.
		"the same column twice":                  "CREATE TABLE ngc_blob (kind {{key}} NOT NULL, kind {{key}} NOT NULL){{opts}}",
		"the same column twice in an index":      "CREATE INDEX ngc_blob_x ON ngc_blob (kind, kind)",
		"the same column twice in a PRIMARY KEY": "CREATE TABLE ngc_blob (kind {{key}} NOT NULL, PRIMARY KEY (kind, kind)){{opts}}",
		// MariaDB takes 64 characters for a name, SQLite as many as one likes.
		"an index name longer than MariaDB takes": "CREATE INDEX ngc_blob_" + strings.Repeat("x", 200) + " ON ngc_blob (kind)",
	} {
		if _, err := checkStatement(s); err == nil {
			t.Errorf("%s was accepted: %q", name, s)
		}
	}
	// The message has to say what the rule is, not just that something is
	// wrong: whoever wrote the migration reads it.
	if _, err := checkStatement("DROP TABLE ngc_blob"); err == nil || !strings.Contains(err.Error(), "only add") {
		t.Errorf("the message does not say what the rule is: %v", err)
	}
	// A word that does not belong is refused, and the message names it: who
	// wrote the migration has to see where to look. WHERE is nothing anybody
	// meant as a column, and a message calling it one would send the author
	// looking in the wrong place.
	_, err := checkStatement("CREATE INDEX ngc_blob_x ON ngc_blob (kind) WHERE nonsense")
	if err == nil {
		t.Fatal("a statement with a WHERE was accepted")
	}
	if !strings.Contains(err.Error(), `"WHERE"`) || !strings.Contains(err.Error(), "over") {
		t.Errorf("the message does not say that a word stands where the statement is over: %v", err)
	}
	// A name standing where a column belongs is named as what it is not.
	if _, err := checkStatement("CREATE INDEX ngc_blob_x ON ngc_blob (nonsense)"); err == nil ||
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
		if _, err := checkStatement(s); err == nil {
			t.Errorf("%s was accepted: %q", name, s)
		}
	}
}

// glued are the statements of this round: a column type written against the
// word in front of it or behind it. The rules read two pieces there, Fill
// makes one name out of them, and the database is given a name no list
// carries. Every line here is measured the whole way in
// TestAGluedTypeNeverReachesTheDatabase and in
// TestNothingUnlistedIsEverCreated.
var glued = map[string]string{
	"a column type against the column name":        "CREATE TABLE ngc_blob (kind{{key}} NOT NULL, id {{key}} NOT NULL){{opts}}",
	"a column added with the type against it":      "ALTER TABLE ngc_app ADD node{{key}}",
	"NOT NULL against the column type":             "CREATE TABLE ngc_blob (kind {{key}}NOT NULL, id {{key}} NOT NULL){{opts}}",
	"NOT NULL against the type of an added column": "ALTER TABLE ngc_app ADD node {{key}}NOT NULL",
	"a word behind the ending":                     "CREATE TABLE ngc_blob (kind {{key}} NOT NULL){{opts}}x",
	"a second ending against the first":            "CREATE TABLE ngc_blob (kind {{key}} NOT NULL){{opts}}{{opts}}",
	"a digit against the column type":              "ALTER TABLE ngc_app ADD node {{int}}8 NOT NULL",
	"an underscore against the column type":        "ALTER TABLE ngc_app ADD node {{key}}_x NOT NULL",
	"the word COLUMN with the type against it":     "ALTER TABLE ngc_app ADD COLUMN{{key}} node NOT NULL",
	"the table name with the type against it":      "CREATE TABLE ngc_blob{{key}} (kind {{key}} NOT NULL){{opts}}",
	"the index name with the type against it":      "CREATE INDEX ngc_blob_{{key}} ON ngc_blob (kind)",
}

// nameByte says whether a byte may stand inside a name a database reads. It
// is wider than the rules of a migration on purpose: MariaDB takes a dollar
// sign and every byte above ASCII in a name written without quotes, so a test
// that asks what the database sees has to cut the same way the database does.
func nameByte(c byte) bool {
	return c == '_' || c == '$' || c >= 0x80 ||
		c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// namesIn cuts finished SQL into the names and words a database reads in it.
func namesIn(sql string) []string {
	var out []string
	for i := 0; i < len(sql); {
		if !nameByte(sql[i]) {
			i++
			continue
		}
		j := i
		for j < len(sql) && nameByte(sql[j]) {
			j++
		}
		out, i = append(out, sql[i:j]), j
	}
	return out
}

// unlisted measures finished SQL - the text a database is given, after Fill -
// and gives back every name in it that no fixed list carries. It is written
// the other way round from the rules on purpose: it knows nothing about
// tokens or forms, it only cuts the text the way a database does and looks
// every piece up. A rule that reads a statement differently from the way the
// database reads it shows up here.
func unlisted(d sqlstore.Dialect, sql string) []string {
	ok := map[string]bool{}
	// the words the four forms are made of
	for _, w := range []string{"CREATE", "TABLE", "UNIQUE", "INDEX", "ALTER", "ADD",
		"COLUMN", "NOT", "NULL", "PRIMARY", "KEY", "ON"} {
		ok[w] = true
	}
	// the words the dialect's own column types bring along
	for _, p := range sqlstore.Placeholders {
		for _, w := range namesIn(d.Type(p)) {
			ok[w] = true
		}
	}
	table := ""
	for _, w := range namesIn(sql) {
		if slices.Contains(sqlstore.Tables, w) {
			table = w
		}
	}
	for _, c := range sqlstore.Columns[table] {
		ok[c] = true
	}
	var bad []string
	for _, w := range namesIn(sql) {
		switch {
		case ok[w] || ok[strings.ToUpper(w)]:
		case slices.Contains(sqlstore.Tables, w):
		case table != "" && strings.HasPrefix(w, table+"_"):
		default:
			bad = append(bad, w)
		}
	}
	return bad
}

// TestAGluedTypeNeverReachesTheDatabase is the sentence of this round as a
// test: what is measured has to be what runs. Every line above has to be
// refused - and so that the test would notice if it were not, whatever gets
// through is filled for both dialects and taken apart again, and every name
// in it is looked up.
func TestAGluedTypeNeverReachesTheDatabase(t *testing.T) {
	for name, body := range glued {
		stmts, err := split(body)
		if err != nil {
			continue
		}
		t.Errorf("%s was accepted: %q", name, body)
		for _, s := range stmts {
			for _, d := range []sqlstore.Dialect{sqlstore.MariaDB, sqlstore.SQLite} {
				sql, err := sqlstore.Fill(d, s)
				if err != nil {
					continue
				}
				if bad := unlisted(d, sql); len(bad) > 0 {
					t.Errorf("   it would put %v into the database: %q", bad, sql)
				}
			}
		}
	}
}

// TestEveryStatementRunsAsItWasMeasured is the standing guarantee, on
// everything that really reaches a database: the statements of every
// migration on disk and the two tables the runner brings along, for both
// dialects. Each one is filled the way Apply fills it, and then the finished
// SQL is taken apart again and every name in it is looked up in the fixed
// lists. Nothing here trusts the rules: it reads the text the database is
// given.
func TestEveryStatementRunsAsItWasMeasured(t *testing.T) {
	files, err := List()
	if err != nil {
		t.Fatal(err)
	}
	all := []string{createSchema, createLock}
	for _, f := range files {
		all = append(all, f.Statements...)
	}
	for _, s := range all {
		for _, d := range []sqlstore.Dialect{sqlstore.MariaDB, sqlstore.SQLite} {
			sql, err := fill(d, s)
			if err != nil {
				t.Errorf("%.40q: %v", s, err)
				continue
			}
			if bad := unlisted(d, sql); len(bad) > 0 {
				t.Errorf("%v would go into the database: %q", bad, sql)
			}
		}
	}
}

// TestFillRefusesWhatWasNotMeasured: fill is the only way a statement reaches
// a database, and it hands on nothing it has not measured itself. A statement
// that does not stand in the form the rules wrote is refused there, however
// it got that far - a migration read by an older version of the rules, a
// constant somebody wrote by hand, a string put together somewhere else.
func TestFillRefusesWhatWasNotMeasured(t *testing.T) {
	for name, s := range map[string]string{
		"a column type against the name": "CREATE TABLE ngc_blob ( kind{{key}} NOT NULL ) {{opts}}",
		"a word too much":                "CREATE TABLE ngc_blob ( kind {{key}} NOT NULL ) {{opts}} x",
	} {
		if _, err := fill(sqlstore.SQLite, s); err == nil {
			t.Errorf("%s was filled and would have run: %q", name, s)
		}
	}
	// A statement whose pieces are right but which does not stand in the form
	// the rules wrote is refused for that reason and told so. The author has
	// to read where to look: the names are all fine, the form is not.
	_, err := fill(sqlstore.SQLite, "CREATE TABLE ngc_blob (kind {{key}} NOT NULL){{opts}}")
	if err == nil {
		t.Fatal("a statement in another form was filled and would have run")
	}
	if !strings.Contains(err.Error(), "does not stand in the form it was measured in") {
		t.Errorf("the message does not say that the form is the problem: %v", err)
	}
	// What the rules did write runs, and it runs unchanged.
	form, err := checkStatement("CREATE TABLE ngc_blob (kind {{key}} NOT NULL){{opts}}")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := fill(sqlstore.SQLite, form); err != nil {
		t.Errorf("what the rules wrote does not run: %v", err)
	}
}

// TestASplitMigrationIsBounded: the migrations are built in and come from
// nobody outside, but a bound belongs on every count all the same.
func TestASplitMigrationIsBounded(t *testing.T) {
	one := "CREATE INDEX ngc_blob_seq ON ngc_blob (kind);\n"
	if _, err := split(strings.Repeat(one, maxStatements)); err != nil {
		t.Errorf("%d statements were refused: %v", maxStatements, err)
	}
	if _, err := split(strings.Repeat(one, maxStatements+1)); err == nil {
		t.Errorf("%d statements were taken", maxStatements+1)
	}
	if _, err := split(strings.Repeat("-- comment\n", maxFile)); err == nil {
		t.Error("a migration far past the bound was read")
	}
}

// changing is a dialect that does not give the same answer twice, and rogue
// is one that writes a second statement into the column type. Neither is
// anything sqlstore does today - they stand for what fill must not have to
// trust: the dialect is the one piece of a finished statement that comes from
// no migration and that no rule ever reads.
type changing struct {
	sqlstore.Dialect
	calls int
}

func (d *changing) Type(name string) string {
	d.calls++
	if d.calls > 1 {
		return "INTEGER"
	}
	return d.Dialect.Type(name)
}

type rogue struct{ sqlstore.Dialect }

func (rogue) Type(string) string { return "BLOB; CREATE TABLE nonsense (x BLOB)" }

// TestFillTrustsNobodyForTheFinishedText: what runs has to be what was
// measured, and that holds for the piece the dialect writes as well. A
// dialect that answers differently the second time is caught by the
// comparison itself, and one that writes a whole statement into a column type
// is refused before anything runs.
func TestFillTrustsNobodyForTheFinishedText(t *testing.T) {
	form, err := checkStatement("CREATE TABLE ngc_blob (kind {{key}} NOT NULL){{opts}}")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fill(&changing{Dialect: sqlstore.SQLite}, form); err == nil {
		t.Error("a dialect that changed its answer was filled and would have run")
	}
	if _, err := fill(rogue{Dialect: sqlstore.SQLite}, form); err == nil {
		t.Error("a column type carrying a second statement was filled and would have run")
	}
}

// TestFillingNeverGrowsTwoPiecesTogether is the sentence of this round as a
// property instead of as a list of examples, and it is measured on made-up
// text, not on anything anybody thought of: whenever the rules can take a
// text apart at all, writing the pieces back and filling them gives exactly
// the pieces with the dialect's types in them. Two pieces can never grow into
// one, whatever stood between them in the file.
//
// The second half is that taking the written-back text apart again gives the
// same pieces. Together the two say that the text a database is given and the
// text the rules read are the same thing.
func TestFillingNeverGrowsTwoPiecesTogether(t *testing.T) {
	// A small alphabet, so that the pieces really run into each other: bare
	// words, brackets, spaces and whole column types.
	alphabet := []string{"a", "b", "_", "0", "(", ")", ",", " ", "{", "}",
		"{{key}}", "{{int}}", "{{opts}}", "kind", "NOT"}
	r := rand.New(rand.NewPCG(20260923, 4))
	taken := 0
	for i := 0; i < 200000; i++ {
		var b strings.Builder
		for n := 1 + r.IntN(10); n > 0; n-- {
			b.WriteString(alphabet[r.IntN(len(alphabet))])
		}
		s := b.String()
		pieces, err := tokens(s)
		if err != nil {
			continue
		}
		taken++
		form := write(pieces)
		again, err := tokens(form)
		if err != nil || len(again) != len(pieces) {
			t.Fatalf("the written-back form is read differently: %q -> %q: %v", s, form, err)
		}
		for j := range pieces {
			if again[j] != pieces[j] {
				t.Fatalf("the written-back form is read differently: %q -> %q", s, form)
			}
		}
		for _, d := range []sqlstore.Dialect{sqlstore.MariaDB, sqlstore.SQLite} {
			want, err := writeFilled(pieces, d)
			if err != nil {
				continue
			}
			got, err := sqlstore.Fill(d, form)
			if err != nil || got != want {
				t.Fatalf("filling took %q apart differently: %q instead of %q (%v)", form, got, want, err)
			}
		}
	}
	if taken < 1000 {
		t.Fatalf("only %d texts could be taken apart, so the property was hardly measured", taken)
	}
	t.Logf("%d of 200000 made-up texts could be taken apart and all of them fill as measured", taken)
}
