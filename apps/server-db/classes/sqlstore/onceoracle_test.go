package sqlstore_test

// The one assurance the pairing paths of the API rest on and nothing held.
//
// join.Accept and controllers.Pair answer a busy database differently from a
// code that does not verify, and both say in a comment that this opens no
// oracle because "OnceUse does its whole lookup inside ONE write transaction,
// so the lock is met before any row is read". Their own cases cannot hold
// that: the store under them is a stub that blocks every call, so it makes the
// sentence true by construction. The property belongs to this package, and
// this is where it is measured.

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// TestOnceUseMeetsTheLockBeforeItReadsARow runs the real store against a real
// foreign lock and asks whether a value that EXISTS and one that never did get
// the same answer.
//
// The intermediate check comes first and is the half that makes the rest mean
// anything: without the lock the two answers DIFFER, so the case can tell them
// apart. Only then is the lock taken and the sameness measured. Without that
// step a store that answered "not found" to everything would pass.
func TestOnceUseMeetsTheLockBeforeItReadsARow(t *testing.T) {
	const kind = "pair"
	known := bytes.Repeat([]byte{3}, 32)
	unknown := bytes.Repeat([]byte{4}, 32) // the same shape, nothing behind it
	const now = int64(1790000000)

	s, _, path := write1Fixture(t, sqlstore.SQLite)
	if err := s.OnceCreate(ctx, kind, known, "a secret of this installation", now+3600); err != nil {
		t.Fatalf("the value under test could not be created: %v", err)
	}

	// Free database: the two have to be tellable apart here, or nothing below
	// is an assurance.
	if _, err := s.OnceUse(ctx, kind, known, now); err != nil {
		t.Fatalf("a value that exists answered %v on a free database", err)
	}
	if _, err := s.OnceUse(ctx, kind, unknown, now); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("a value that never existed answered %v on a free database, want %v",
			err, dbapi.ErrNotFound)
	}

	// A second value that exists, because the first one is spent now.
	known2 := bytes.Repeat([]byte{5}, 32)
	if err := s.OnceCreate(ctx, kind, known2, "a second secret", now+3600); err != nil {
		t.Fatalf("the second value could not be created: %v", err)
	}

	write1HoldLock(t, path)

	// A deadline of its own, because what is measured is the ANSWER and not
	// the retry chain: without one both calls would take the measured 80,6 s.
	// The same figure for both, so the deadline cannot be the difference.
	ask := func(value []byte) error {
		c, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		_, err := s.OnceUse(c, kind, value, now)
		return err
	}
	gotKnown, gotUnknown := ask(known2), ask(unknown)

	// Neither may be an answer about the VALUE: under the lock the row was
	// never reached, so saying "not found" or "used" would be saying whether
	// the value is a secret of this installation.
	for name, err := range map[string]error{"known": gotKnown, "unknown": gotUnknown} {
		if err == nil {
			t.Fatalf("the %s value got through although the file is locked; nothing measured", name)
		}
		if errors.Is(err, dbapi.ErrNotFound) || errors.Is(err, dbapi.ErrUsed) {
			t.Errorf("under a foreign lock the %s value is answered with %v -- that is an "+
				"answer about the value, and the pairing paths of the API rest on there "+
				"being none", name, err)
		}
	}
	if gotKnown.Error() != gotUnknown.Error() {
		t.Errorf("under a foreign lock a value that exists is answered %v and one that "+
			"never did %v; the two must not be tellable apart", gotKnown, gotUnknown)
	}
}
