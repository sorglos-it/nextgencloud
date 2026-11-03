package sqlstore

import (
	"errors"
	"testing"
)

// TestFreeForTakeKnowsTheSameDifferenceAsTheSQL holds the second bolt of
// LockTake on its own, because the bolt cannot be reached through the method
// as long as the statement says "until <= ?": measured on 03.11.2026 on both
// databases, a bent column makes that comparison refuse too much, never too
// little, so no row gets past the SQL for the bolt to judge
// (docs/schloss-ablauf-riegel.md). The bolt is aimed at the day somebody
// writes "until > ?" for "held", and until that day this is the only place
// that can say whether it judges right.
//
// What it has to get right is not "has the deadline passed" alone. The
// contract (shared/dbapi) lets the SAME owner take the lock again and move
// the deadline, rollout.renewLease rests on it, and a bolt that forgot that
// would break the renewal of a round that ran longer than its lease - which
// is the one round that needs it.
func TestFreeForTakeKnowsTheSameDifferenceAsTheSQL(t *testing.T) {
	const now = int64(1790000000)
	cases := []struct {
		name      string
		prevOwner string
		prevUntil string
		owner     string
		free      bool
		wantErr   error
	}{
		{"own row, deadline still running", "api-1", "1790000600", "api-1", true, nil},
		{"own row, deadline passed", "api-1", "1789999999", "api-1", true, nil},
		{"own row, deadline no number", "api-1", "later", "api-1", true, nil},
		{"foreign row, deadline still running", "api-1", "1790000600", "api-2", false, nil},
		{"foreign row, deadline passed", "api-1", "1789999999", "api-2", true, nil},
		{"foreign row, deadline is now", "api-1", "1790000000", "api-2", true, nil},
		{"foreign row, one second left", "api-1", "1790000001", "api-2", false, nil},
		{"foreign row, deadline no number", "api-1", "later", "api-2", false, errLockBent},
		{"foreign row, deadline empty", "api-1", "", "api-2", false, errLockBent},
		{"foreign row, deadline with a space", "api-1", " 1789999999", "api-2", false, errLockBent},
		{"owner is a prefix of the row's", "api-12", "1790000600", "api-1", false, nil},
		{"row's owner is a prefix of ours", "api-1", "1790000600", "api-12", false, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			free, err := freeForTake([]byte(c.prevOwner), []byte(c.prevUntil), c.owner, now)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("error: %v, want %v", err, c.wantErr)
			}
			if free != c.free {
				t.Errorf("free: %v, want %v", free, c.free)
			}
		})
	}
}

// TestFreeForTakeReadsTheColumnAndNotADriversNumber: the bolt takes the
// deadline as bytes on purpose. A column that holds text where a number
// belongs is the case it exists for, and a Scan into an int64 would turn that
// case into an error of the driver - a different error on every database -
// instead of the one answer this package gives for it.
//
// So the number has to come out of the bytes here, and a value that is no
// number has to be refused here. Both halves are measured: a deadline of
// 1790000600 as text decides like the number it spells, and the text SQLite
// would also accept in a BLOB column - a number with something behind it -
// does not.
func TestFreeForTakeReadsTheColumnAndNotADriversNumber(t *testing.T) {
	const now = int64(1790000000)
	if free, err := freeForTake([]byte("api-1"), []byte("1790000600"), "api-2", now); err != nil || free {
		t.Errorf("text that spells a running deadline: %v, %v, want false, <nil>", free, err)
	}
	for _, bad := range []string{"1790000600x", "0x1790000600", "1790000600.0", "+1790000600 "} {
		if free, err := freeForTake([]byte("api-1"), []byte(bad), "api-2", now); !errors.Is(err, errLockBent) || free {
			t.Errorf("%q: %v, %v, want false, errLockBent", bad, free, err)
		}
	}
}
