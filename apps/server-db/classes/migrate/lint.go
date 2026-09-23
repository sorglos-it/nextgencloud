package migrate

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
)

// maxStatement is the most one statement of a migration may hold. The
// migrations are built into the release file, so none of this comes from
// outside; a bound belongs on it all the same, and the widest table of the
// schema writes less than a tenth of it.
const maxStatement = 4096

// forbidden are words a migration may never carry. Some are not portable
// (IF NOT EXISTS, AUTO_INCREMENT, ENGINE), the others would break the promise
// that the previous API version keeps working while the migration runs: it
// still reads the tables and columns it knows. None of them would fit one of
// the four forms below either; the list stands in front of the forms so that
// the author reads why the word is refused and not only that the form does
// not fit.
var forbidden = []string{
	"DROP", "RENAME", "TRUNCATE", "AUTO_INCREMENT", "IF NOT EXISTS", "IF EXISTS",
	"ENGINE=", "SERIAL", "IDENTITY", "GENERATED", "CURRENT_TIMESTAMP", "NOW(", "MERGE ",
}

// kinds are the only statements a migration may carry, named for the message.
// Each one has a form of its own, see checkForm.
var kinds = []string{"CREATE TABLE", "CREATE UNIQUE INDEX", "CREATE INDEX", "ALTER TABLE"}

// errOnlyAdd says what the rule is in one sentence the author can act on.
var errOnlyAdd = errors.New("a migration may only add: no DROP, RENAME or TRUNCATE, because the previous API version keeps running")

// checkStatement checks one statement of a migration.
func checkStatement(s string) error {
	if len(s) > maxStatement {
		return fmt.Errorf("a statement is %d bytes long, %d are allowed: %.40q", len(s), maxStatement, s)
	}
	if strings.Contains(s, ";") {
		return fmt.Errorf("a statement holds a semicolon: %.40q", s)
	}
	// No string literal at all: every value goes in as a placeholder, and
	// without quotes the splitter never has to understand quoting.
	if strings.ContainsAny(s, "'\"`") {
		return fmt.Errorf("a statement holds a quote: %.40q", s)
	}
	// The words are looked for in one line, in upper case and without the
	// spaces around an =, because ") ENGINE = MyISAM" says exactly what
	// ") ENGINE=MyISAM" says.
	upper := strings.ToUpper(strings.Join(strings.Fields(s), " "))
	upper = strings.ReplaceAll(upper, " =", "=")
	upper = strings.ReplaceAll(upper, "= ", "=")
	for _, w := range forbidden {
		if strings.Contains(upper, w) {
			if w == "DROP" || w == "RENAME" || w == "TRUNCATE" {
				return fmt.Errorf("%w: %.40q", errOnlyAdd, s)
			}
			return fmt.Errorf("%q is not portable: %.40q", w, s)
		}
	}
	return checkForm(s)
}

// checkForm is the whole rule about names, and it works the other way round
// from looking for something suspicious: a statement has to have one of four
// forms, and anything that does not have one of them falls. Looking for the
// suspicious can never be finished - there is always one more way to write a
// name that no rule thought of. A form is finished: what it does not name, it
// does not allow.
//
// The four forms, in the words of the tokens below:
//
//	CREATE TABLE <table> ( <column> <type> [NOT NULL] {, ...}
//	    [, PRIMARY KEY ( <column> {, <column>} )] ) {{opts}}
//	CREATE INDEX <name> ON <table> ( <column> {, <column>} )
//	CREATE UNIQUE INDEX <name> ON <table> ( <column> {, <column>} )
//	ALTER TABLE <table> ADD [COLUMN] <column> <type> [NOT NULL]
//
// <table> is read where the form says a table stands and has to be one of
// sqlstore.Tables, <column> where a column stands and has to be one of
// sqlstore.Columns of that table, <name> has to begin with the table and an
// underscore. Nothing is read out of a stream of words any more, so no word
// can stand in for another by looking like it.
func checkForm(s string) error {
	t, err := tokens(s)
	if err != nil {
		return err
	}
	c := &cursor{t: t}
	switch {
	case c.takeKeyword("ALTER"):
		if c.takeKeyword("TABLE") {
			return alterForm(c, s)
		}
	case c.takeKeyword("CREATE"):
		if c.takeKeyword("TABLE") {
			return tableForm(c, s)
		}
		// CREATE UNIQUE INDEX and CREATE INDEX have the same form; only the
		// database treats the two differently.
		c.takeKeyword("UNIQUE")
		if c.takeKeyword("INDEX") {
			return indexForm(c, s)
		}
	}
	return fmt.Errorf("a migration carries only %s: %.40q", strings.Join(kinds, ", "), s)
}

// tableForm reads CREATE TABLE <table> ( ... ) {{opts}} and nothing else, so
// a CREATE TABLE ... AS SELECT falls here whichever way it is written - the
// name is in no list, or an AS stands where the form is over. That is the one
// thing a migration must never do: a table made out of a table of ours would
// carry the rows along, TOTP secrets and all, it would stand in no list, and
// because a migration may not DROP, no later one could take it away again.
func tableForm(c *cursor, s string) error {
	table, err := takeTable(c, s)
	if err != nil {
		return err
	}
	if !c.takeSign(tokOpen) {
		return unexpected(c, s, "the ( in front of the columns of "+table)
	}
	key, cols := false, 0
	for {
		if c.takeKeyword("PRIMARY") {
			if !c.takeKeyword("KEY") {
				return unexpected(c, s, "the word KEY behind PRIMARY")
			}
			if key {
				return fmt.Errorf("%q is given two PRIMARY KEY: %.40q", table, s)
			}
			key = true
			if err := columnList(c, table, s); err != nil {
				return err
			}
		} else if err := columnDef(c, table, s); err != nil {
			return err
		} else {
			cols++
		}
		if !c.takeSign(tokComma) {
			break
		}
	}
	if cols == 0 {
		return fmt.Errorf("the table %q is given no column: %.40q", table, s)
	}
	if !c.takeSign(tokClose) {
		return unexpected(c, s, "the ) behind the columns of "+table)
	}
	// The ending of the dialect follows, once and nothing else: on MariaDB
	// {{opts}} becomes the engine and the row format, and a table made
	// without it would take whatever the server has set as its default -
	// which is the one thing Galera would then not replicate. On SQLite it is
	// empty.
	typ, ok := c.takeType()
	if !ok {
		return unexpected(c, s, "{{opts}}, the ending every CREATE TABLE carries")
	}
	if typ != "opts" {
		return fmt.Errorf("only {{opts}} may stand behind a CREATE TABLE, not {{%s}}: %.40q", typ, s)
	}
	return atEnd(c, s)
}

// indexForm reads CREATE INDEX <name> ON <table> ( <columns> ) and nothing
// else. The name has to begin with the table and an underscore, so every
// index a migration hands out belongs to a table of ours by its name alone.
func indexForm(c *cursor, s string) error {
	name, ok := c.takeName()
	if !ok {
		return unexpected(c, s, "the name of the index")
	}
	if !c.takeKeyword("ON") {
		return unexpected(c, s, "the word ON and the table of the index")
	}
	table, err := takeTable(c, s)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(name, table+"_") || len(name) <= len(table)+1 {
		return fmt.Errorf("the index %q does not begin with %q, so it belongs to no table of ours: %.40q",
			name, table+"_", s)
	}
	if err := columnList(c, table, s); err != nil {
		return err
	}
	return atEnd(c, s)
}

// alterForm reads ALTER TABLE <table> ADD [COLUMN] <column> <type>
// [NOT NULL] and nothing else. MariaDB, MySQL and SQLite all take ADD without
// the word COLUMN, and SQLite takes one ADD in a statement, so the form takes
// one too.
func alterForm(c *cursor, s string) error {
	table, err := takeTable(c, s)
	if err != nil {
		return err
	}
	if !c.takeKeyword("ADD") {
		return unexpected(c, s, "the word ADD: an ALTER TABLE adds one column and does nothing else")
	}
	c.takeKeyword("COLUMN")
	if err := columnDef(c, table, s); err != nil {
		return err
	}
	return atEnd(c, s)
}

// columnDef reads one column with its type: the name, the type, and at most
// the words NOT NULL behind it. Everything else - AUTO_INCREMENT, DEFAULT,
// AFTER, a second type - is something the five databases do not all read the
// same way, so the form does not carry it.
func columnDef(c *cursor, table, s string) error {
	name, ok := c.takeName()
	if !ok {
		return unexpected(c, s, "the name of a column of "+table)
	}
	if err := column(table, name, s); err != nil {
		return err
	}
	typ, ok := c.takeType()
	if !ok {
		return unexpected(c, s, "the type of the column "+name+", written {{key}} or {{int}}")
	}
	if typ == "opts" {
		return fmt.Errorf("{{opts}} is the ending of a CREATE TABLE, not the type of the column %q: %.40q", name, s)
	}
	if c.takeKeyword("NOT") && !c.takeKeyword("NULL") {
		return unexpected(c, s, "the word NULL behind NOT")
	}
	return nil
}

// columnList reads ( <column> {, <column>} ) and measures every name in it
// against the columns of the table.
func columnList(c *cursor, table, s string) error {
	if !c.takeSign(tokOpen) {
		return unexpected(c, s, "the ( in front of the columns of "+table)
	}
	for {
		name, ok := c.takeName()
		if !ok {
			return unexpected(c, s, "a column of "+table)
		}
		if err := column(table, name, s); err != nil {
			return err
		}
		if !c.takeSign(tokComma) {
			break
		}
	}
	if !c.takeSign(tokClose) {
		return unexpected(c, s, "the ) behind the columns of "+table)
	}
	return nil
}

// takeTable reads the name standing where the form says a table stands and
// looks it up in the fixed list. The spelling has to be the one the list
// carries: MariaDB on Linux tells ngc_blob and NGC_BLOB apart, so a name
// written differently is a second table nobody knows about.
func takeTable(c *cursor, s string) (string, error) {
	name, ok := c.takeName()
	if !ok {
		return "", unexpected(c, s, "the name of a table")
	}
	if !slices.Contains(sqlstore.Tables, name) {
		return "", fmt.Errorf("%q is in no fixed list of tables: %.40q", name, s)
	}
	return name, nil
}

// column looks a name up in the columns of its table, in the spelling of the
// list.
func column(table, name, s string) error {
	if !slices.Contains(sqlstore.Columns[table], name) {
		return fmt.Errorf("%q is no column of %q, and a statement names nothing but its table, an index of it and its columns: %.40q",
			name, table, s)
	}
	return nil
}

// tokenKind tells what a piece of a statement is. A migration is made of
// these five and of nothing else.
type tokenKind int

const (
	tokWord  tokenKind = iota // a bare word: a name or a keyword
	tokType                   // a column type, written {{key}}
	tokOpen                   // (
	tokClose                  // )
	tokComma                  // ,
)

// token is one piece of a statement. text carries the word, or the name
// inside the {{ }}.
type token struct {
	kind tokenKind
	text string
}

// tokens cuts a statement into its pieces. Everything that is none of them is
// refused right here, before a rule has to guess what it could mean: a dot, a
// quote, an equals sign, the start of a comment, a character outside ASCII.
// That is why the forms above can be short - they never see anything they did
// not expect. It is also why a name can never be glued to something else:
// MariaDB takes a dollar sign and every letter above ASCII in a name that
// stands without quotes, so ngc_blob$x is a table of its own, and one nobody
// listed.
func tokens(s string) ([]token, error) {
	var out []token
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			out, i = append(out, token{kind: tokOpen}), i+1
		case c == ')':
			out, i = append(out, token{kind: tokClose}), i+1
		case c == ',':
			out, i = append(out, token{kind: tokComma}), i+1
		case isWordByte(c):
			j := i
			for j < len(s) && isWordByte(s[j]) {
				j++
			}
			out, i = append(out, token{kind: tokWord, text: s[i:j]}), j
		case c == '{':
			name, next, err := columnType(s, i)
			if err != nil {
				return nil, err
			}
			out, i = append(out, token{kind: tokType, text: name}), next
		case c == '.':
			return nil, fmt.Errorf("a name carries a dot, and a dot in front of it names another database or schema: %.40q", s)
		default:
			r, _ := utf8.DecodeRuneInString(s[i:])
			return nil, fmt.Errorf("%q belongs in no name and in no statement of a migration: %.40q", r, s)
		}
	}
	return out, nil
}

// columnType reads a {{name}} at i and gives back the name and where it ends.
// The name is looked up at once, so a type nobody listed never becomes a token
// and no form further on has to think about one.
func columnType(s string, i int) (string, int, error) {
	rest := s[i:]
	if !strings.HasPrefix(rest, "{{") {
		return "", 0, fmt.Errorf("a column type does not begin with {{ : %.40q", s)
	}
	end := strings.Index(rest, "}}")
	if end < 0 {
		return "", 0, fmt.Errorf("a column type has no closing }}: %.40q", s)
	}
	name := rest[2:end]
	if !slices.Contains(sqlstore.Placeholders, name) {
		return "", 0, fmt.Errorf("unknown placeholder %q: %.40q", name, s)
	}
	return name, i + end + 2, nil
}

// isWordByte says whether a byte may stand inside a bare word.
func isWordByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// cursor walks the tokens of one statement from front to back. Every name is
// read at the place where it stands, and that is what makes the rules a form
// instead of a search: a word standing where no form expects one is refused
// for where it stands, whatever it says.
type cursor struct {
	t []token
	i int
}

// peek gives the token the cursor stands on.
func (c *cursor) peek() (token, bool) {
	if c.i >= len(c.t) {
		return token{}, false
	}
	return c.t[c.i], true
}

// takeKeyword takes the word w if it stands here. Keywords are compared
// without regard to case, names never are.
func (c *cursor) takeKeyword(w string) bool {
	if t, ok := c.peek(); ok && t.kind == tokWord && strings.EqualFold(t.text, w) {
		c.i++
		return true
	}
	return false
}

// takeSign takes one of ( ) , if it stands here.
func (c *cursor) takeSign(k tokenKind) bool {
	if t, ok := c.peek(); ok && t.kind == k {
		c.i++
		return true
	}
	return false
}

// takeName takes a bare word and gives it back unchanged: a name has to be
// written the way the fixed list writes it.
func (c *cursor) takeName() (string, bool) {
	if t, ok := c.peek(); ok && t.kind == tokWord {
		c.i++
		return t.text, true
	}
	return "", false
}

// takeType takes a column type and gives back the name inside the {{ }}.
func (c *cursor) takeType() (string, bool) {
	if t, ok := c.peek(); ok && t.kind == tokType {
		c.i++
		return t.text, true
	}
	return "", false
}

// atEnd makes sure nothing follows. By now the form has read everything it
// carries, so whatever is left over is something no rule looked at - and that
// is what must never reach a database.
func atEnd(c *cursor, s string) error {
	if t, ok := c.peek(); ok {
		return fmt.Errorf("%s stands where the statement is over: %.40q", describe(t), s)
	}
	return nil
}

// unexpected says what stands there and what belongs there, because whoever
// wrote the migration reads it and has to see where to look.
func unexpected(c *cursor, s, want string) error {
	if t, ok := c.peek(); ok {
		return fmt.Errorf("%s stands where %s belongs: %.40q", describe(t), want, s)
	}
	return fmt.Errorf("the statement stops where %s belongs: %.40q", want, s)
}

// describe names a token the way it is written.
func describe(t token) string {
	switch t.kind {
	case tokWord:
		return fmt.Sprintf("the word %q", t.text)
	case tokType:
		return fmt.Sprintf("the column type {{%s}}", t.text)
	case tokOpen:
		return "a ("
	case tokClose:
		return "a )"
	default:
		return "a ,"
	}
}
