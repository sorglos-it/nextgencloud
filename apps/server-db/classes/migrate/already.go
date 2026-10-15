package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
)

// rowsQuerier is what the question below needs of a database or of a
// transaction. Inside a transaction it asks through the transaction: the
// objects this run has just made are visible on that connection and nowhere
// else yet.
type rowsQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// knowsItsSchema is answered by a dialect whose database hands back the text
// of the statements its schema was made with. It is not part of
// sqlstore.Dialect on purpose: SQLite keeps that text, MariaDB has nothing of
// the kind to give, and a method every dialect had to carry would have to be
// answered there out of documentation - which is the very kind of promise
// this file exists to replace.
type knowsItsSchema interface {
	// SchemaText returns one statement that gives back the text of every
	// object of the schema, or "" for a database that cannot say.
	SchemaText() string
}

// stepOver answers whether a statement the dialect read as "already there"
// may be stepped over.
//
// The step-over is what design 4.8 rests on: an update replays every
// migration on a database that is being used, and the rule of task 3 is that
// a migration only ever adds. But "whatever is already there is what this run
// would have made" is a SENTENCE, and W-1 of the review of 10.10.2026
// measured what it is worth: an index of the migration's name over another
// column, with an empty ngc_schema, made the run answer err=nil, write
// done=1, and report "nothing to do" from then on, over a schema no migration
// of this release ever made. The linter that was named as the thing holding
// this reads the TEXT OF THE FILE; the collision stands between the file and
// a state of the database, and no reading of the file can see it.
//
// So the sentence is replaced by the question. Where the database hands back
// the text of its own schema, the object that is already there is compared
// against the one the statement describes, and only an object that IS the one
// this run would have made is stepped over.
//
// WHAT IS STILL OPEN, and it is open at the line and not in a ledger: a
// database that cannot answer SchemaText - MariaDB today - keeps the old
// step-over, with the old sentence and nothing holding it. Nothing here can
// reach a MariaDB server, so a check for it would be written out of
// documentation alone - and MariaDB has nothing of this shape to answer
// either: SHOW CREATE TABLE hands back a text the server writes itself and
// takes one table at a time, information_schema keeps no statement text at
// all. So this half STAYS OPEN, and it was looked at again on 12.10.2026 and
// left open on purpose.
//
// What it has instead of a closure is a watchman, in already_test.go:
// TestTheStepOverStaysTheOldOneForADialectThatCannotSay measures that the
// hole has exactly this shape - such a dialect is let through, and let
// through without a question being put to the database -, and
// TestExactlyOneDialectCanSayWhatItHolds pins WHICH of the two dialects sits
// in it, so that the hole can neither grow nor be closed quietly out of
// documentation. Whoever has a cluster closes that half, measures it against
// the server, and goes past that second case to do it.
// It answers nil when the statement may be stepped over and the reason to
// stop otherwise, so a caller that only wants to go on cannot read the
// answer the wrong way round.
func stepOver(ctx context.Context, q rowsQuerier, d sqlstore.Dialect, stmt string) error {
	k, ok := d.(knowsItsSchema)
	if !ok || k.SchemaText() == "" {
		return nil
	}
	rows, err := q.QueryContext(ctx, k.SchemaText())
	if err != nil {
		return err
	}
	defer rows.Close()
	var stored []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return err
		}
		stored = append(stored, s)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if sameObject(stmt, stored) {
		return nil
	}
	return fmt.Errorf("another object of that name is already there and it is not the one this statement makes: %.80q", stmt)
}

// sameObject reports whether one of the objects the database already holds is
// the one stmt makes.
//
// The two texts are not compared byte for byte, and that is measured, not
// chosen: SQLite keeps the text of a CREATE TABLE as it was given and
// REWRITES it when a later ALTER TABLE adds a column, so a replayed CREATE
// TABLE meets its own table carrying columns it does not name (measured
// 12.10.2026: the stored text of ngc_blob after one added column reads
// "... kind BLOB NOT NULL , seq INTEGER, PRIMARY KEY ( kind ) )"). A table is
// therefore the one this run would have made when every piece the statement
// names stands in it, columns the statement does not name being the ones a
// later migration added - which is exactly the rule that a migration only
// ever adds, read on the database instead of on the file.
//
// An index is compared whole: nothing rewrites one, so anything but the same
// text is another index of the same name. The column of an ALTER TABLE has to
// stand in the table as a piece of its own, which is what tells a column of
// the same name and another type from the one that was meant - the case the
// ledger measured and could not hold.
func sameObject(stmt string, stored []string) bool {
	want := norm(stmt)
	head, body, split := cut(want)
	for _, s := range stored {
		got := norm(s)
		switch {
		case strings.HasPrefix(want, "ALTER TABLE "):
			// "ALTER TABLE <name> ADD COLUMN <definition>": the table is
			// named by the statement, the piece looked for is what follows.
			name, def, ok := alterParts(want)
			if !ok {
				return false
			}
			gh, gb, gs := cut(got)
			if !gs || gh != "CREATE TABLE "+name+" (" {
				continue
			}
			return hasPiece(gb, def)
		case strings.HasPrefix(want, "CREATE TABLE "):
			gh, gb, gs := cut(got)
			if !split || !gs || gh != head {
				continue
			}
			for _, p := range pieces(body) {
				if !hasPiece(gb, p) {
					return false
				}
			}
			return true
		default:
			// Everything else a migration may carry is an index, and the
			// kinds are a closed list (see kinds in lint.go).
			if got == want {
				return true
			}
		}
	}
	return false
}

// norm writes a statement the one way the two sides can be compared: one
// space between every two pieces, and a comma a piece of its own, so that no
// comparison ever turns on whether a space stood in front of one. SQLite
// writes "seq INTEGER," where the migration writes "seq INTEGER ,".
func norm(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, ",", " , ")), " ")
}

// cut splits a normalized statement at its first bracket: the head up to and
// including it, and the body up to the bracket that closes it.
func cut(s string) (head, body string, ok bool) {
	i := strings.Index(s, "(")
	if i < 0 {
		return s, "", false
	}
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[:i+1], strings.TrimSpace(s[i+1 : j]), true
			}
		}
	}
	return s, "", false
}

// pieces splits the body of a bracket at the commas that are not inside a
// bracket of their own: the column definitions and the key of a table, each
// one on its own.
func pieces(body string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(body[start:i]))
				start = i + 1
			}
		}
	}
	if p := strings.TrimSpace(body[start:]); p != "" {
		out = append(out, p)
	}
	return out
}

// hasPiece reports whether want is one of the pieces of body. It asks for the
// whole piece and not for a part of it: "seq INTEGER" must not be found in
// "seq INTEGER NOT NULL", which is another column of the same name.
func hasPiece(body, want string) bool {
	for _, p := range pieces(body) {
		if p == want {
			return true
		}
	}
	return false
}

// alterParts takes the table and the column definition out of a normalized
// ALTER TABLE. The form is the one the rules of lint.go let through, and
// nothing else ever reaches here.
func alterParts(s string) (name, def string, ok bool) {
	const add = " ADD COLUMN "
	rest := strings.TrimPrefix(s, "ALTER TABLE ")
	i := strings.Index(rest, add)
	if i < 0 {
		return "", "", false
	}
	return rest[:i], strings.TrimSpace(rest[i+len(add):]), true
}
