package migrate

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
)

// forbidden are the words a migration may never carry. Some are not portable
// (IF NOT EXISTS, AUTO_INCREMENT, ENGINE), the others would break the promise
// that the previous API version keeps working while the migration runs: it
// still reads the tables and columns it knows.
var forbidden = []string{
	"DROP", "RENAME", "TRUNCATE", "AUTO_INCREMENT", "IF NOT EXISTS", "IF EXISTS",
	"ENGINE=", "SERIAL", "IDENTITY", "GENERATED", "CURRENT_TIMESTAMP", "NOW(", "MERGE ",
}

// errOnlyAdd says what the rule is in one sentence the author can act on.
var errOnlyAdd = errors.New("a migration may only add: no DROP, RENAME or TRUNCATE, because the previous API version keeps running")

// checkStatement checks one statement of a migration.
func checkStatement(s string) error {
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
	// An opening {{ without its }} would leave the rest of the statement
	// unread by placeholdersIn and by stripTypes, so it is refused here
	// rather than silently skipped.
	if strings.Count(s, "{{") != strings.Count(s, "}}") {
		return fmt.Errorf("a placeholder is not closed: %.40q", s)
	}
	for _, p := range placeholdersIn(s) {
		if !slices.Contains(sqlstore.Placeholders, p) {
			return fmt.Errorf("unknown placeholder %q: %.40q", p, s)
		}
	}
	return checkNames(s)
}

// checkNames refuses every word that looks like a table or column of ours but
// is in neither fixed list. It is deliberately blunt: a statement names
// exactly one table from the list, every other word starting with ngc_ has to
// be an index named after that table, and every bare lower-case word inside
// the brackets has to be a column of it.
func checkNames(s string) error {
	var names []string
	for _, w := range words(s) {
		// SQL does not care how an identifier is written, so every name is
		// measured in lower case: NGC_OTHER_X names what ngc_other_x names,
		// and upper case is no way past the fixed list.
		if lower := strings.ToLower(w); strings.HasPrefix(lower, "ngc_") {
			names = append(names, lower)
		}
	}
	// The table is looked up before the rest is measured against it, because
	// CREATE INDEX names the index first: ngc_blob_seq stands before ngc_blob.
	table := ""
	for _, w := range names {
		if slices.Contains(sqlstore.Tables, w) {
			table = w
			break
		}
	}
	if table == "" {
		return fmt.Errorf("a statement names no table: %.40q", s)
	}
	for _, w := range names {
		if w != table && !strings.HasPrefix(w, table+"_") {
			return fmt.Errorf("%q is in no fixed list: %.40q", w, s)
		}
	}
	for _, w := range columnWords(s) {
		if !slices.Contains(sqlstore.Columns[table], w) {
			return fmt.Errorf("column %q is not listed for %q", w, table)
		}
	}
	return nil
}

// placeholdersIn returns the names between {{ and }}.
func placeholdersIn(s string) []string {
	var out []string
	for {
		i := strings.Index(s, "{{")
		if i < 0 {
			return out
		}
		s = s[i+2:]
		j := strings.Index(s, "}}")
		if j < 0 {
			return out
		}
		out, s = append(out, s[:j]), s[j+2:]
	}
}

// stripTypes takes every {{...}} out of a statement. checkStatement has
// already looked at the names inside them; without them no type is read as a
// column, and a column called data or text is checked like any other.
func stripTypes(s string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, "{{")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		rest := s[i+2:]
		j := strings.Index(rest, "}}")
		if j < 0 {
			return b.String()
		}
		s = rest[j+2:]
	}
}

// words returns every bare word of a statement, case kept.
func words(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	})
}

// keywords are the words that stand between the columns of a CREATE TABLE.
var keywords = []string{"not", "null", "primary", "key", "unique", "add", "column"}

// columnWords returns the words a statement uses as columns: the word in
// front of every column type, the column of an ALTER TABLE ... ADD, and every
// bare word inside the brackets that is not a name of ours. Everything is
// measured in lower case, because SQL does not care how an identifier is
// written.
func columnWords(s string) []string {
	out := append(typedWords(s), addWords(s)...)
	bare := stripTypes(s)
	open := strings.Index(bare, "(")
	if open < 0 {
		return out
	}
	// Between the columns of a CREATE TABLE stand NOT NULL, PRIMARY KEY and
	// UNIQUE, so those words are skipped there. A statement without a column
	// type - CREATE INDEX - carries nothing but columns in its brackets, and
	// there even a column called key is measured against the list.
	defines := strings.Contains(s, "{{")
	for _, w := range words(bare[open:]) {
		lower := strings.ToLower(w)
		if strings.HasPrefix(lower, "ngc_") || (defines && slices.Contains(keywords, lower)) {
			continue
		}
		out = append(out, lower)
	}
	return out
}

// typedWords returns the word in front of every column type: in
// "kind {{key}} NOT NULL" that is kind. A column is written that way whether
// it stands in brackets or behind ADD, so this is what catches a column that
// the brackets alone would miss. {{opts}} is no column type and is skipped.
func typedWords(s string) []string {
	var out []string
	for {
		i := strings.Index(s, "{{")
		if i < 0 {
			return out
		}
		before, rest := s[:i], s[i+2:]
		j := strings.Index(rest, "}}")
		if j < 0 {
			return out
		}
		name := rest[:j]
		s = rest[j+2:]
		if name == "opts" {
			continue
		}
		if f := words(before); len(f) > 0 {
			out = append(out, strings.ToLower(f[len(f)-1]))
		}
	}
}

// addWords returns the column of an ALTER TABLE ... ADD. MariaDB, MySQL and
// SQLite all take ADD without the word COLUMN, so a column added that way is
// measured like every other one.
func addWords(s string) []string {
	var out []string
	f := words(s)
	for i, w := range f {
		if !strings.EqualFold(w, "add") {
			continue
		}
		j := i + 1
		if j < len(f) && strings.EqualFold(f[j], "column") {
			j++
		}
		if j < len(f) {
			out = append(out, strings.ToLower(f[j]))
		}
	}
	return out
}
