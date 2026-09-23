package dbopen

import (
	"context"
	"errors"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
)

var ctx = context.Background()

// register puts o in place for one test and takes it out again, so tests do
// not hand their opener on to the next one.
func register(t *testing.T, o Opener) {
	t.Helper()
	Register(o)
	t.Cleanup(func() { Register(nil) })
}

// TestOpenWithoutOpener proves that an API without the database part refuses
// to start instead of running half way.
func TestOpenWithoutOpener(t *testing.T) {
	register(t, nil)
	if _, err := Open(ctx, config.DB{}); !errors.Is(err, ErrNoOpener) {
		t.Fatalf("without an opener: %v", err)
	}
}

func TestOpenUsesTheOpener(t *testing.T) {
	want := config.DB{Driver: "sqlite", Path: "/var/lib/ngc/ngc.db"}
	var got config.DB
	db := memstore.New()
	register(t, func(_ context.Context, cfg config.DB) (dbapi.Store, error) {
		got = cfg
		return db, nil
	})
	out, err := Open(ctx, want)
	if err != nil || out != db {
		t.Fatalf("open: %v", err)
	}
	if got.Driver != want.Driver || got.Path != want.Path {
		t.Fatalf("opener saw %+v", got)
	}
}

// TestOpenPassesTheError proves that Open hands the opener's error on
// untouched, so a caller sees why the database stayed shut.
func TestOpenPassesTheError(t *testing.T) {
	fail := errors.New("no database")
	register(t, func(context.Context, config.DB) (dbapi.Store, error) { return nil, fail })
	if _, err := Open(ctx, config.DB{}); !errors.Is(err, fail) {
		t.Fatalf("open answered %v", err)
	}
}
