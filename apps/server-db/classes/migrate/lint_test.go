package migrate

import (
	"fmt"
	"go/ast"
	"go/parser"
	gotoken "go/token" // token is a type of this package
	"go/types"
	"regexp"
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
		"ALTER TABLE ngc_app ADD COLUMN node {{key}}",
		// MariaDB, MySQL and SQLite all take ADD without the word COLUMN, so
		// the rules take it too - and measure the column all the same.
		"ALTER TABLE ngc_app ADD node {{key}}",
	} {
		if _, err := checkStatement(s, fromMigration); err != nil {
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
		"a column added without the word COLUMN": "ALTER TABLE ngc_app ADD nonsense {{key}}",
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
		"a column added the MySQL way":   "ALTER TABLE ngc_app ADD COLUMN node {{key}} AFTER id",
		"an index without columns":       "CREATE INDEX ngc_blob_x ON ngc_blob",
		"an index with a way of its own": "CREATE INDEX ngc_blob_x ON ngc_blob (kind) USING BTREE",
		// MariaDB runs what stands in /*! ... */ and reads it as SQL.
		"a comment MariaDB runs": "CREATE INDEX ngc_blob_x ON ngc_blob (kind) /*!*/",
		// SQLite takes one ADD in a statement, so the form takes one.
		"two columns added at once": "ALTER TABLE ngc_app ADD COLUMN node {{key}}, ADD COLUMN id {{key}}",
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
		if _, err := checkStatement(s, fromMigration); err == nil {
			t.Errorf("%s was accepted: %q", name, s)
		}
	}
	// The message has to say what the rule is, not just that something is
	// wrong: whoever wrote the migration reads it.
	if _, err := checkStatement("DROP TABLE ngc_blob", fromMigration); err == nil || !strings.Contains(err.Error(), "only add") {
		t.Errorf("the message does not say what the rule is: %v", err)
	}
	// A word that does not belong is refused, and the message names it: who
	// wrote the migration has to see where to look. WHERE is nothing anybody
	// meant as a column, and a message calling it one would send the author
	// looking in the wrong place.
	_, err := checkStatement("CREATE INDEX ngc_blob_x ON ngc_blob (kind) WHERE nonsense", fromMigration)
	if err == nil {
		t.Fatal("a statement with a WHERE was accepted")
	}
	if !strings.Contains(err.Error(), `"WHERE"`) || !strings.Contains(err.Error(), "over") {
		t.Errorf("the message does not say that a word stands where the statement is over: %v", err)
	}
	// A name standing where a column belongs is named as what it is not.
	if _, err := checkStatement("CREATE INDEX ngc_blob_x ON ngc_blob (nonsense)", fromMigration); err == nil ||
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
		"a type where the column belongs": "ALTER TABLE ngc_app ADD {{key}} node",
		"the ending as a column type":     "CREATE TABLE ngc_blob (kind {{opts}} NOT NULL){{opts}}",
	} {
		if _, err := checkStatement(s, fromMigration); err == nil {
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

// oneWord is the shape of a column type and knownEndings are the endings
// there are, both written out here on purpose: the oracle below measures what
// a database was really given, so what it takes for known has to come from
// somewhere other than the rules it is measuring.
var (
	oneWord      = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\([0-9]+\))?$`)
	knownEndings = []string{"", "ENGINE=InnoDB ROW_FORMAT=DYNAMIC"}
)

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
	// The words the dialect's own column types bring along - but only out of
	// a type that keeps the form every type has to keep, and out of an ending
	// that is one of the two there are. A dialect that answers
	// "BLOB , heimlich BLOB" would otherwise hand the oracle its own extra
	// column as a known name, and the oracle would look past exactly what it
	// is here for. Both are written out again below and not taken from the
	// rules, so that this cannot go wrong the same way the rules could.
	for _, p := range sqlstore.Placeholders {
		typ := d.Type(p)
		if p == "opts" {
			if !slices.Contains(knownEndings, typ) {
				continue
			}
		} else if !oneWord.MatchString(typ) {
			continue
		}
		for _, w := range namesIn(typ) {
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
	all := []struct {
		s string
		w who
	}{{createSchema, fromRunner}, {createLock, fromRunner}}
	for _, f := range files {
		for _, s := range f.Statements {
			all = append(all, struct {
				s string
				w who
			}{s, fromMigration})
		}
	}
	for _, c := range all {
		for _, d := range []sqlstore.Dialect{sqlstore.MariaDB, sqlstore.SQLite} {
			sql, err := fill(d, c.s, c.w)
			if err != nil {
				t.Errorf("%.40q: %v", c.s, err)
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
		if _, err := fill(sqlstore.SQLite, s, fromMigration); err == nil {
			t.Errorf("%s was filled and would have run: %q", name, s)
		}
	}
	// A statement whose pieces are right but which does not stand in the form
	// the rules wrote is refused for that reason and told so. The author has
	// to read where to look: the names are all fine, the form is not.
	_, err := fill(sqlstore.SQLite, "CREATE TABLE ngc_blob (kind {{key}} NOT NULL){{opts}}", fromMigration)
	if err == nil {
		t.Fatal("a statement in another form was filled and would have run")
	}
	if !strings.Contains(err.Error(), "does not stand in the form it was measured in") {
		t.Errorf("the message does not say that the form is the problem: %v", err)
	}
	// What the rules did write runs, and it runs unchanged.
	form, err := checkStatement("CREATE TABLE ngc_blob (kind {{key}} NOT NULL){{opts}}", fromMigration)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := fill(sqlstore.SQLite, form, fromMigration); err != nil {
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
	form, err := checkStatement("CREATE TABLE ngc_blob (kind {{key}} NOT NULL){{opts}}", fromMigration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fill(&changing{Dialect: sqlstore.SQLite}, form, fromMigration); err == nil {
		t.Error("a dialect that changed its answer was filled and would have run")
	}
	if _, err := fill(rogue{Dialect: sqlstore.SQLite}, form, fromMigration); err == nil {
		t.Error("a column type carrying a second statement was filled and would have run")
	}
}

// TestFillingNeverGrowsTwoPiecesTogether is the sentence of this round as a
// property instead of as a list of examples, and it is measured on every list
// of pieces there is up to six long: two pieces can never grow into one,
// whatever stood between them in the file.
//
// Exhaustive and not made up at random. Eight kinds of piece, lists of one to
// six: 8 + 8^2 + ... + 8^6 = 299592 lists, every one of them measured. There
// is no threshold to set and nothing to miss - a random draw could only ever
// say that it did not happen to find one.
//
// Two things are measured for each list. Writing it out and reading it back
// gives the same pieces, so the text a database is given and the text the
// rules read are the same thing. And filling the written-out text gives
// exactly the pieces with this dialect's types in them, so no type can grow
// together with the name in front of it or behind it.
func TestFillingNeverGrowsTwoPiecesTogether(t *testing.T) {
	// Every kind of piece there is, with two bare words so that a word can
	// stand next to a word: that is the whole alphabet a statement is made of.
	alphabet := []token{
		{kind: tokWord, text: "a"},
		{kind: tokWord, text: "NOT"},
		{kind: tokType, text: "key"},
		{kind: tokType, text: "int"},
		{kind: tokType, text: "opts"},
		{kind: tokOpen},
		{kind: tokClose},
		{kind: tokComma},
	}
	const longest = 6
	dialects := []sqlstore.Dialect{sqlstore.MariaDB, sqlstore.SQLite}
	list := make([]token, 0, longest)
	count := 0
	var walk func()
	walk = func() {
		if len(list) > 0 {
			count++
			form := write(list)
			again, err := tokens(form)
			if err != nil || !slices.Equal(again, list) {
				t.Fatalf("the written-back form is read differently: %q -> %v (%v)", form, again, err)
			}
			for _, d := range dialects {
				want, err := writeFilled(list, d)
				if err != nil {
					t.Fatalf("a real dialect was refused for %q: %v", form, err)
				}
				got, err := sqlstore.Fill(d, form)
				if err != nil || got != want {
					t.Fatalf("filling took %q apart differently: %q instead of %q (%v)", form, got, want, err)
				}
			}
		}
		if len(list) == longest {
			return
		}
		for _, tok := range alphabet {
			list = append(list, tok)
			walk()
			list = list[:len(list)-1]
		}
	}
	walk()
	if want := 299592; count != want {
		t.Fatalf("%d lists of pieces were measured, and there are %d", count, want)
	}
}

// wrongType is a dialect that answers a placeholder with something that is
// not a column type, and wrongEnding one that writes something else where a
// CREATE TABLE ends. Neither is anything sqlstore does today: they stand for
// the one piece of a finished statement that comes out of no migration and
// that no rule of a migration ever reads. Every line below went through the
// list of forbidden characters that used to stand guard here - the reviewer
// put "BLOB , heimlich BLOB" through it and read the extra column back out of
// a real SQLite - which is why the type is held to a form now and the ending
// to a list.
type wrongType struct {
	sqlstore.Dialect
	typ string
}

// Only {{key}} is answered wrongly, the way the reviewer measured it: with
// every column of the statement carrying the same made-up name, SQLite would
// refuse it for the doubled name and the measurement would be saved by an
// accident instead of by the guard.
func (d wrongType) Type(name string) string {
	if name == "key" {
		return d.typ
	}
	return d.Dialect.Type(name)
}

type wrongEnding struct {
	sqlstore.Dialect
	ending string
}

func (d wrongEnding) Type(name string) string {
	if name == "opts" {
		return d.ending
	}
	return d.Dialect.Type(name)
}

var wrongTypes = map[string]string{
	"a second column behind a comma":      "BLOB , heimlich BLOB",
	"a second column behind a space":      "BLOB heimlich",
	"a second column behind a line break": "BLOB\nheimlich",
	// A letter above ASCII stands in a MariaDB name without quotes.
	"a letter outside ASCII": "BLOBäx",
	// MariaDB runs what stands in /*! ... */ and SQLite reads it as a
	// comment: the same migration, two schemas.
	"a comment MariaDB runs":       "BLOB /*!99999 , heimlich BLOB */",
	"a second statement":           "BLOB; CREATE TABLE nonsense (x BLOB)",
	"a default of its own":         "BLOB DEFAULT 0",
	"nothing at all":               "",
	"a length nobody could mean":   "BLOB(" + strings.Repeat("9", 100) + ")",
	"a name that is no name":       "(255)",
	"a type with a table in front": "other.BLOB",
	// The edges of the form itself.
	"a bracket that never closes":     "BLOB(1",
	"a length with nothing in it":     "BLOB()",
	"something behind the length":     "BLOB(1) ",
	"a placeholder of its own":        "BLOB{{key}}",
	"a comment of its own":            "BLOB--x",
	"a name beginning with no letter": "_BLOB",
}

var wrongEndings = map[string]string{
	"a second statement":        "; CREATE TABLE nonsense ( x BLOB )",
	"a column behind the table": "ENGINE=InnoDB ROW_FORMAT=DYNAMIC , heimlich BLOB",
	"an ending nobody listed":   "STRICT",
	// The ending carries no space of its own any more: a statement runs with
	// one between every two of its pieces, so the old value would write two.
	// A dialect that brings it back is refused here and not quietly taken.
	"the old ending with its own space": " ENGINE=InnoDB ROW_FORMAT=DYNAMIC",
	"a comment MariaDB runs":            "/*!99999 , heimlich BLOB */",
	"the ending of another kind":        "ENGINE=MyISAM",
}

// TestAColumnTypeKeepsItsForm: the column type of the dialect is the one
// piece of a finished statement that no rule of a migration ever reads, so it
// is measured where it is written in. A list of characters it may not carry
// can never be finished - there is always one more way to write a second
// column - so it is held to a form instead: one word, at most with a length
// in brackets behind it. The ending of a CREATE TABLE has no form of its own
// and is held to a list.
//
// Nothing here trusts the guard: whatever it lets through is run on a real
// SQLite, and then the database is asked what it made of it.
func TestAColumnTypeKeepsItsForm(t *testing.T) {
	form, err := checkStatement("CREATE TABLE ngc_blob (kind {{key}} NOT NULL, seq {{int}} NOT NULL, PRIMARY KEY (kind)){{opts}}", fromMigration)
	if err != nil {
		t.Fatal(err)
	}
	for name, typ := range wrongTypes {
		d := wrongType{Dialect: sqlstore.SQLite, typ: typ}
		sql, err := fill(d, form, fromMigration)
		if err != nil {
			continue
		}
		t.Errorf("a column type that is %s was filled and would have run: %q -> %q", name, typ, sql)
		if bad := runOnSQLite(t, d, []string{form}, false); len(bad) > 0 {
			t.Errorf("   the database now carries %v", bad)
		}
	}
	for name, ending := range wrongEndings {
		d := wrongEnding{Dialect: sqlstore.SQLite, ending: ending}
		sql, err := fill(d, form, fromMigration)
		if err != nil {
			continue
		}
		t.Errorf("an ending that is %s was filled and would have run: %q -> %q", name, ending, sql)
		if bad := runOnSQLite(t, d, []string{form}, false); len(bad) > 0 {
			t.Errorf("   the database now carries %v", bad)
		}
	}
	// What the two real dialects answer keeps the form and runs.
	for _, d := range []sqlstore.Dialect{sqlstore.MariaDB, sqlstore.SQLite} {
		if _, err := fill(d, form, fromMigration); err != nil {
			t.Errorf("a real dialect was refused: %v", err)
		}
	}
}

// TestTheOracleSeesWhatTheDialectBringsAlong is a test of a test. unlisted
// takes the names a dialect's column types bring along as known, which is
// right for BLOB and VARBINARY - and was wrong for everything else the
// dialect wrote next to them: a dialect that answers "BLOB , heimlich BLOB"
// listed its own extra column as known, so the oracle looked past exactly
// what it is there for. It now takes those names from the form the type has
// to keep, and the form is written out here and not borrowed from the rules,
// so the oracle cannot go wrong the same way the rules could.
func TestTheOracleSeesWhatTheDialectBringsAlong(t *testing.T) {
	d := wrongType{Dialect: sqlstore.SQLite, typ: "BLOB , heimlich BLOB"}
	sql := "CREATE TABLE ngc_blob ( kind " + d.Type("key") + " NOT NULL )"
	if bad := unlisted(d, sql); !slices.Contains(bad, "heimlich") {
		t.Errorf("the oracle takes what the dialect brought along as listed, so it looks past it: %v", bad)
	}
	// And it stays quiet on what the real dialects write.
	form, err := checkStatement("CREATE TABLE ngc_blob (kind {{key}} NOT NULL){{opts}}", fromMigration)
	if err != nil {
		t.Fatal(err)
	}
	for _, real := range []sqlstore.Dialect{sqlstore.MariaDB, sqlstore.SQLite} {
		s, err := sqlstore.Fill(real, form)
		if err != nil {
			t.Fatal(err)
		}
		if bad := unlisted(real, s); len(bad) > 0 {
			t.Errorf("the oracle calls %v unlisted in %q", bad, s)
		}
	}
}

// TestAColumnIsAddedNullable: a column that comes later is nullable. NOT NULL
// on an ALTER TABLE is the one thing in this guard that runs on one database
// and not on the other - SQLite refuses it on a table that holds rows,
// MariaDB fills in a silent default - so a test on an empty database goes
// green and the real upgrade breaks. What that costs is nothing: a column
// added later has no value for the rows that are already there, whatever the
// migration says.
//
// A CREATE TABLE keeps NOT NULL: a table that is being made has no rows to
// break.
func TestAColumnIsAddedNullable(t *testing.T) {
	for _, s := range []string{
		"ALTER TABLE ngc_app ADD COLUMN node {{key}}",
		"ALTER TABLE ngc_app ADD node {{key}}",
	} {
		if _, err := checkStatement(s, fromMigration); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	for _, s := range []string{
		"ALTER TABLE ngc_app ADD COLUMN node {{key}} NOT NULL",
		"ALTER TABLE ngc_app ADD node {{key}} NOT NULL",
	} {
		_, err := checkStatement(s, fromMigration)
		if err == nil {
			t.Errorf("a column was added as NOT NULL: %q", s)
			continue
		}
		if !strings.Contains(err.Error(), "nullable") {
			t.Errorf("the message does not say what the rule is: %v", err)
		}
	}
	if _, err := checkStatement("CREATE TABLE ngc_blob (kind {{key}} NOT NULL, PRIMARY KEY (kind)){{opts}}", fromMigration); err != nil {
		t.Errorf("a CREATE TABLE lost its NOT NULL: %v", err)
	}
}

// TestAMigrationLeavesTheRunnerTablesAlone: ngc_schema and ngc_lock are the
// runner's own, made before the first migration runs. A statement of a
// migration about either of them is therefore always either an error or
// nothing - and the two databases do not even agree on which. SQLite answers
// "already exists", rolls the whole migration back and the server does not
// start; MariaDB gives error 1050, the runner steps over it and records the
// migration as done. That is exactly the split this guard is built against.
// New columns are impossible for the two anyway, because sqlstore.Columns
// carries only the ones they have.
func TestAMigrationLeavesTheRunnerTablesAlone(t *testing.T) {
	for name, s := range map[string]string{
		"the table of the runner once more": "CREATE TABLE ngc_schema ( version {{int}} NOT NULL , PRIMARY KEY ( version ) ) {{opts}}",
		"the lock table once more":          "CREATE TABLE ngc_lock ( name {{key}} NOT NULL , PRIMARY KEY ( name ) ) {{opts}}",
		"a column on the lock":              "ALTER TABLE ngc_lock ADD until {{int}}",
		"an index on the lock":              "CREATE INDEX ngc_lock_until ON ngc_lock (until)",
		"an index on the schema":            "CREATE UNIQUE INDEX ngc_schema_done ON ngc_schema (done)",
	} {
		if _, err := checkStatement(s, fromMigration); err == nil {
			t.Errorf("a migration was allowed to name %s: %q", name, s)
		}
	}
}

// TestABoundBitesBeforeTheWork: a bound that is looked at after the work on
// the piece past it is not a bound. The migration below carries one statement
// too many, and that one is a DROP: whichever message comes back says which
// of the two was looked at first.
func TestABoundBitesBeforeTheWork(t *testing.T) {
	one := "CREATE INDEX ngc_blob_seq ON ngc_blob (kind);\n"
	_, err := split(strings.Repeat(one, maxStatements) + "DROP TABLE ngc_blob;\n")
	if err == nil {
		t.Fatal("a migration past the bound was read")
	}
	if !strings.Contains(err.Error(), "more than") {
		t.Errorf("the statement past the bound was measured before anybody counted: %v", err)
	}
}

// TestTheRunnerDoorIsOpenedOnce reads this package's own source and counts
// where the runner's door is opened. who is a bool, so a bare true at a call
// site would let a migration name ngc_schema and ngc_lock: the compiler takes
// it, every test of behaviour still passes, and the guard of takeTable is
// gone without a word.
//
// Measured is every function of this package's **scope** that takes a who,
// and the list of them is not written by hand: go/types is asked which ones
// they are, so a package function that gets the mark tomorrow is measured the
// day it is written.
//
// Two carriers do not stand in that scope and are therefore not measured but
// refused: a method, and a function literal. Both bans are below, and neither
// looks for a way in - they simply say where the mark may stand. Nothing else
// is claimed here: a carrier this package might one day get in some third
// shape would be neither measured nor refused, and the reading would have to
// be widened for it.
//
// At each measured call the who is either one of the two constants or the who
// the function around the call got itself and hands on - nothing else, and no
// value at all. Taking such a function as a value instead of calling it is
// refused as well. fromRunner stands exactly once, in bootstrap, which makes
// the two tables before the first migration can run.
//
// What an identifier stands for comes from go/types, not from its name: this
// package has a local w of its own in measure, and only the object behind the
// name says whether a w at a call is the mark or something else that took the
// name.
func TestTheRunnerDoorIsOpenedOnce(t *testing.T) {
	fset := gotoken.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			if !strings.HasSuffix(name, "_test.go") {
				files = append(files, file)
			}
		}
	}
	slices.SortFunc(files, func(a, b *ast.File) int {
		return strings.Compare(fset.Position(a.Pos()).Filename, fset.Position(b.Pos()).Filename)
	})
	info := &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
		Uses:  map[*ast.Ident]types.Object{},
		Defs:  map[*ast.Ident]types.Object{},
	}
	// Nothing asked about here lives outside this package: who, its two
	// constants and every function that carries it are declared in lint.go, so
	// the imports are left unresolved and the errors that come of that are
	// ignored. An identifier the reading below cannot resolve is reported.
	conf := types.Config{
		Importer:                 nowhere{},
		Error:                    func(error) {},
		DisableUnusedImportCheck: true,
	}
	pkg, _ := conf.Check("migrate", fset, files, info)
	if pkg == nil {
		t.Fatal("the package could not be read at all")
	}
	marks := map[string]types.Object{
		fromMigrationName: pkg.Scope().Lookup(fromMigrationName),
		fromRunnerName:    pkg.Scope().Lookup(fromRunnerName),
	}
	for name, obj := range marks {
		if obj == nil {
			t.Fatalf("%s is not declared in this package", name)
		}
	}
	whoType := pkg.Scope().Lookup("who")
	if whoType == nil {
		t.Fatal("who is not declared in this package")
	}
	// The doors, counted out by go/types instead of by hand: every function of
	// this package with a parameter of type who, and where that parameter
	// stands in a call.
	doors := map[*types.Func]int{}
	for _, name := range pkg.Scope().Names() {
		fn, ok := pkg.Scope().Lookup(name).(*types.Func)
		if !ok {
			continue
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok {
			continue
		}
		for i := range sig.Params().Len() {
			if sig.Params().At(i).Type() != whoType.Type() {
				continue
			}
			if _, twice := doors[fn]; twice {
				t.Errorf("%s takes the mark twice, and the guard measures one place only", name)
			}
			doors[fn] = i
		}
	}
	if len(doors) == 0 {
		t.Fatal("no function of this package takes a who; the guard would measure nothing")
	}
	// A method and a function literal both stand outside the package scope and
	// outside the counting above. Neither is looked for, both are refused: the
	// mark belongs on a package function, and there it is measured. A literal
	// is the cheaper of the two ways past the counting - var f = func(s
	// string, w who) … and then f(s, true) opens the runner's door without a
	// word.
	takesWho := func(params *ast.FieldList) bool {
		for _, field := range params.List {
			if info.Types[field.Type].Type == whoType.Type() {
				return true
			}
		}
		return false
	}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncDecl:
				if x.Recv != nil && takesWho(x.Type.Params) {
					t.Errorf("%s: the method %s takes a who; the mark belongs on a function of the "+
						"package, which is where it is measured", fset.Position(x.Pos()), x.Name.Name)
				}
			case *ast.FuncLit:
				if takesWho(x.Type.Params) {
					t.Errorf("%s: a function literal takes a who; the mark belongs on a function of "+
						"the package, which is where it is measured", fset.Position(x.Pos()))
				}
			}
			return true
		})
	}
	called := map[*types.Func]int{}
	callee := map[*ast.Ident]bool{}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok {
					callee[id] = true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					callee[sel.Sel] = true
				}
			}
			return true
		})
	}
	// A function that carries the mark and is taken as a value is opened
	// somewhere no reading of arguments can follow.
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || callee[id] || info.Defs[id] != nil {
				return true
			}
			if fn, ok := info.Uses[id].(*types.Func); ok {
				if _, door := doors[fn]; door {
					t.Errorf("%s: %s carries the mark and is taken as a value here; "+
						"what it is then given stands nowhere the guard can read",
						fset.Position(id.Pos()), fn.Name())
				}
			}
			return true
		})
	}
	runner, seen := 0, 0
	for _, file := range files {
		var stack []ast.Node
		ast.Inspect(file, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return false
			}
			stack = append(stack, n)
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			obj, ok := info.Uses[fn].(*types.Func)
			if !ok {
				return true
			}
			i, ok := doors[obj]
			if !ok {
				return true
			}
			seen++
			called[obj]++
			if i >= len(call.Args) {
				t.Errorf("%s: %s is called without a who", fset.Position(call.Pos()), fn.Name)
				return true
			}
			arg, ok := call.Args[i].(*ast.Ident)
			if !ok {
				t.Errorf("%s: %s is told whose statement it is with something that is no name at all",
					fset.Position(call.Args[i].Pos()), fn.Name)
				return true
			}
			switch obj := info.Uses[arg]; {
			case obj == nil:
				t.Errorf("%s: the guard cannot tell what %s stands for",
					fset.Position(arg.Pos()), arg.Name)
			case obj == marks[fromMigrationName]:
			case obj == marks[fromRunnerName]:
				runner++
			case isWhoParam(info, obj, whoType, stack):
				// The who the function around the call got itself: the door
				// it opens is the one its own caller opened, and that call is
				// measured here too.
			default:
				t.Errorf("%s: %s is told whose statement it is with %s, which is neither %s nor %s "+
					"nor the who this function was given", fset.Position(arg.Pos()), fn.Name, arg.Name,
					fromMigrationName, fromRunnerName)
			}
			return true
		})
	}
	// A guard that measured nothing says nothing. Every door has to be opened
	// somewhere, and the count follows the doors instead of a number written
	// down once.
	if seen < len(doors) {
		t.Errorf("%d functions carry the mark and only %d calls of them were measured", len(doors), seen)
	}
	for fn := range doors {
		if called[fn] == 0 {
			t.Errorf("%s carries the mark and nobody calls it", fn.Name())
		}
	}
	if runner != 1 {
		t.Errorf("%s stands at %d call sites; it belongs at the one that makes ngc_schema and ngc_lock",
			fromRunnerName, runner)
	}
}

// isWhoParam reports whether obj is a parameter of type who of one of the
// functions around the call.
func isWhoParam(info *types.Info, obj types.Object, whoType types.Object, stack []ast.Node) bool {
	v, ok := obj.(*types.Var)
	if !ok || v.Type() != whoType.Type() {
		return false
	}
	for _, n := range stack {
		var sig *types.Signature
		switch f := n.(type) {
		case *ast.FuncDecl:
			if fn, ok := info.Defs[f.Name].(*types.Func); ok {
				sig, _ = fn.Type().(*types.Signature)
			}
		case *ast.FuncLit:
			sig, _ = info.Types[f].Type.(*types.Signature)
		}
		if sig == nil {
			continue
		}
		for i := range sig.Params().Len() {
			if sig.Params().At(i) == v {
				return true
			}
		}
	}
	return false
}

// nowhere resolves no import at all. See the note in the test above.
type nowhere struct{}

func (nowhere) Import(path string) (*types.Package, error) {
	return nil, fmt.Errorf("migrate: %s is not read here", path)
}

// The two names as the source writes them. A test that reads source code
// looks them up by name once and works with the objects behind them.
const (
	fromMigrationName = "fromMigration"
	fromRunnerName    = "fromRunner"
)
