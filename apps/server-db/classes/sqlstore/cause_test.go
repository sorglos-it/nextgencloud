package sqlstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// errWhy is the reason a caller names for his own deadline, the way
// apps/server-api/classes/dbwait names dbwait.ErrBusy. It is of this test and
// not of that package, so what is measured here is the mechanism and not one
// package's use of it.
var errWhy = errors.New("test: the caller's own reason")

// TestAStoreCallHandsBackTheReasonTheCallerNamed is the assurance. Without it
// a caller cannot tell WHY his call ended: database/sql answers a context
// that is done with the bare context.DeadlineExceeded, which says that a
// deadline ran out and nothing about which deadline or what it was for. One
// step further out that is the difference between "the database is busy, come
// back" and "the database is away" -- measured as one and the same
// E_UNREACHABLE on 13.10.2026 (docs/vertraege-v1.2.md, point 48).
func TestAStoreCallHandsBackTheReasonTheCallerNamed(t *testing.T) {
	s := open(t)
	b := dbapi.Blob{Kind: dbapi.BlobTrust, ID: "1", Seq: 1, Data: []byte("v1"), Updated: 1790000000}

	// The intermediate check first, so that nothing below can be green only
	// because the way to the meant line is shut: with a context that is open,
	// both calls go through.
	if err := s.BlobPut(ctx, b); err != nil {
		t.Fatalf("the way to the meant line is shut, a plain write already fails: %v", err)
	}
	if _, err := s.BlobGet(ctx, dbapi.BlobTrust, "1"); err != nil {
		t.Fatalf("the way to the meant line is shut, a plain read already fails: %v", err)
	}

	for _, c := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"write", func(c context.Context) error { return s.BlobPut(c, b) }},
		{"read", func(c context.Context) error {
			_, err := s.BlobGet(c, dbapi.BlobTrust, "1")
			return err
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Already past: the store asks the database once and gets the
			// context error straight out of database/sql, which is the half
			// of this that never passes a line of package sqlstore.
			dctx, cancel := context.WithDeadlineCause(context.Background(), time.Now().Add(-time.Second), errWhy)
			defer cancel()
			err := c.run(dctx)
			if !errors.Is(err, errWhy) {
				t.Fatalf("the caller's reason is gone, he got: %v", err)
			}
		})

		t.Run(c.name+" without a reason", func(t *testing.T) {
			// The counter-measurement: a deadline with no reason named must
			// still answer context.DeadlineExceeded. Otherwise this would be
			// an arm that invents a reason for every caller.
			dctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancel()
			err := c.run(dctx)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("a deadline with no reason named: %v", err)
			}
			if errors.Is(err, errWhy) {
				t.Fatal("a reason nobody named came back all the same")
			}
		})

		t.Run(c.name+" an error of its own is untouched", func(t *testing.T) {
			// An error that is not the context ending passes through: here a
			// row that is not there, with the context wide open.
			if _, err := s.BlobGet(ctx, dbapi.BlobSetting, "nothing"); !errors.Is(err, dbapi.ErrNotFound) {
				t.Fatalf("a row that is not there: %v", err)
			}
		})
	}
}
