package migrate

import (
	"context"
	"database/sql"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
)

// silentDialect is a dialect that cannot say what its database holds - the
// shape MariaDB has today. It carries no SchemaText at all.
type silentDialect struct{ sqlstore.Dialect }

// emptyTextDialect carries the method and answers nothing, which is the other
// way a dialect says "I cannot tell you".
type emptyTextDialect struct{ sqlstore.Dialect }

func (emptyTextDialect) SchemaText() string { return "" }

// neverAsked is a rowsQuerier that must not be reached. A dialect that cannot
// say what it holds keeps the old step-over, and the old step-over asks the
// database nothing at all; a stepOver that queried anyway would be asking a
// statement this dialect never answered.
type neverAsked struct{ t *testing.T }

func (n neverAsked) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	n.t.Error("stepOver asked a dialect that cannot say what it holds")
	return nil, nil
}

// TestTheStepOverStaysTheOldOneForADialectThatCannotSay is the WATCHMAN over
// the half of W-1 that is open: where the database hands back the text of its
// own schema, the step-over asks instead of assuming - and where it does not,
// the old sentence stays and nothing holds it. That hole is named at the line
// in already.go. What is measured here is that the hole is exactly this shape
// and no wider: such a dialect is let through, and it is let through WITHOUT
// a question being put to the database.
//
// This is not the MariaDB half being closed. It cannot be closed here: there
// is no MariaDB server on this machine, and a SchemaText written for MariaDB
// out of documentation alone would be the very kind of promise W-1 removed.
func TestTheStepOverStaysTheOldOneForADialectThatCannotSay(t *testing.T) {
	for name, d := range map[string]sqlstore.Dialect{
		"no SchemaText at all": silentDialect{sqlstore.MariaDB},
		"SchemaText is empty":  emptyTextDialect{sqlstore.MariaDB},
	} {
		t.Run(name, func(t *testing.T) {
			if err := stepOver(context.Background(), neverAsked{t}, d, "CREATE INDEX x ON y (z)"); err != nil {
				t.Errorf("the step-over refused a dialect that cannot say what it holds: %v", err)
			}
		})
	}
}

// TestExactlyOneDialectCanSayWhatItHolds pins WHICH dialect sits in that hole,
// so that it cannot grow or be closed quietly. NGC has two dialects and no
// more (sqlstore.SQLite and sqlstore.MariaDB), and today exactly one of them
// answers SchemaText.
//
// If this case ever goes red because MariaDB answers it: that is not a
// failure, it is the hole being closed - and the thing to do is to measure the
// new answer AGAINST A REAL SERVER (SHOW CREATE TABLE and information_schema
// do not hand back the text a statement was given, which is why there is no
// answer here today), then say so at the line in already.go and in the ledger
// of the package. The point of this case is that nobody closes it out of
// documentation without passing this line.
func TestExactlyOneDialectCanSayWhatItHolds(t *testing.T) {
	k, ok := sqlstore.SQLite.(knowsItsSchema)
	if !ok || k.SchemaText() == "" {
		t.Error("SQLite no longer says what it holds, so the step-over is back to a sentence for BOTH dialects")
	}
	if _, ok := sqlstore.MariaDB.(knowsItsSchema); ok {
		t.Error("MariaDB answers SchemaText now: measure it against a real server, then rewrite the open half at its line in already.go and in the ledger")
	}
}
