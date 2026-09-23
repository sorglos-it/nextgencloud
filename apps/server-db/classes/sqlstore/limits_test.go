package sqlstore

import (
	"slices"
	"strings"
	"testing"

	appstore "github.com/sorglos-it/nextgencloud/shared/store" // store is the type of this package
)

// TestBounds pins every column bound at its edge. One byte more has to be
// refused here, in Go: MariaDB would cut the value or complain and SQLite
// would take anything, and the same input has to come out the same on both.
func TestBounds(t *testing.T) {
	for name, c := range map[string]struct {
		ok  func(int) bool
		max int
	}{
		"key":  {func(n int) bool { return okKey(strings.Repeat("x", n)) }, MaxKey},
		"text": {func(n int) bool { return okText(strings.Repeat("x", n)) }, MaxText},
		"doc":  {func(n int) bool { return okDoc(make([]byte, n)) }, MaxDoc},
		"data": {func(n int) bool { return okData(make([]byte, n)) }, MaxData},
	} {
		if !c.ok(0) {
			t.Errorf("%s: the empty value was refused", name)
		}
		if !c.ok(c.max) {
			t.Errorf("%s: %d bytes was refused", name, c.max)
		}
		if c.ok(c.max + 1) {
			t.Errorf("%s: %d bytes was accepted", name, c.max+1)
		}
	}
}

// TestBlob guards the one thing blob is for: every column is NOT NULL, and a
// nil slice would bind as NULL.
func TestBlob(t *testing.T) {
	if b := blob(nil); b == nil || len(b) != 0 {
		t.Errorf("blob(nil) = %v", b)
	}
	if b := blob([]byte{}); b == nil {
		t.Error("blob of an empty slice is nil")
	}
	if b := blob([]byte("x")); string(b) != "x" {
		t.Errorf("blob changed the value to %q", b)
	}
}

// TestGrants keeps the grants of an app to the two the contract knows.
func TestGrants(t *testing.T) {
	if !grantsOK(nil) || !grantsOK([]string{appstore.NeedRoot, appstore.NeedNetwork}) {
		t.Error("a list the contract allows was refused")
	}
	for _, bad := range [][]string{{""}, {"admin"}, {"Root"}, {appstore.NeedRoot + " "}, {appstore.NeedRoot, "admin"}} {
		if grantsOK(bad) {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestList(t *testing.T) {
	for _, list := range [][]string{nil, {"admin"}, {"admin", "user"}} {
		s, ok := joinList(list)
		if !ok || !slices.Equal(splitList(s), list) {
			t.Errorf("%v: %q, %v", list, s, ok)
		}
	}
	for name, bad := range map[string][]string{
		"empty entry":   {""},
		"line feed":     {"a\nb"},
		"return":        {"a\rb"},
		"entry too big": {strings.Repeat("x", MaxItem+1)},
		"too many":      make([]string, MaxList+1),
	} {
		if _, ok := joinList(bad); ok {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestCheckSeq(t *testing.T) {
	if err := checkSeq(1 << 63); err == nil {
		t.Error("a sequence above the signed range was accepted")
	}
	if err := checkSeq(1<<63 - 1); err != nil {
		t.Errorf("the largest sequence was refused: %v", err)
	}
}

// listSeeds are column values FuzzReadList starts from: the ones AppPut
// writes, and the ones only a row nobody of ours wrote could carry.
func listSeeds() []string {
	full := strings.TrimSuffix(strings.Repeat("a\n", MaxList), "\n")
	return []string{
		"", "a", appstore.NeedNetwork, appstore.NeedNetwork + "\n" + appstore.NeedRoot,
		"admin\nuser\nguest", strings.Repeat("x", MaxItem), full,
		"a\n", "\n", "a\n\nb", "a\rb", "a\r\nb", strings.Repeat("x", MaxItem+1),
		full + "\na", strings.Repeat("a\n", MaxList+1),
		strings.Repeat("x", MaxList*(MaxItem+1)+1), "\x00", "a\x00b",
	}
}

// TestReadList: a column of the store's own database is a value from outside
// like any other. The bound bites before the work, the round trip is exact,
// and the seeds of the fuzz target are counted - a guard that refuses
// everything would pass a fuzz run without ever having read a list.
func TestReadList(t *testing.T) {
	taken := 0
	for _, s := range listSeeds() {
		list, err := readList(s)
		if err != nil {
			if list != nil {
				t.Errorf("%.20q was refused and still gave %v", s, list)
			}
			continue
		}
		taken++
		if back, ok := joinList(list); !ok || back != s {
			t.Errorf("%.20q came back as %q, %v", s, back, ok)
		}
	}
	// Seven of the seeds are lists a store of ours writes; fewer means the
	// target is measuring nothing but refusals.
	if taken < 7 {
		t.Errorf("only %d of the seeds were read as a list", taken)
	}
	if _, err := readList(strings.Repeat("x", MaxList*(MaxItem+1)+1)); err == nil {
		t.Error("a column past the bound was taken apart")
	}
}

// FuzzReadList: whatever the column carries, readList either refuses it or
// gives back a list joinList writes exactly that way again - the list is
// bounded, every entry is one an app may carry, and nothing is invented or
// lost on the way.
func FuzzReadList(f *testing.F) {
	for _, s := range listSeeds() {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		list, err := readList(s)
		if err != nil {
			if list != nil {
				t.Fatalf("%.40q was refused and still gave %v", s, list)
			}
			return
		}
		if len(list) > MaxList || len(s) > MaxList*(MaxItem+1) {
			t.Fatalf("%.40q was taken as %d entries", s, len(list))
		}
		for _, e := range list {
			if e == "" || len(e) > MaxItem || strings.ContainsAny(e, "\n\r") {
				t.Fatalf("%.40q gave the entry %.40q", s, e)
			}
		}
		if back, ok := joinList(list); !ok || back != s {
			t.Fatalf("%.40q came back as %q, %v", s, back, ok)
		}
	})
}
