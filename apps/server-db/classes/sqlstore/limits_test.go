package sqlstore

import (
	"slices"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/store"
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
	if !grantsOK(nil) || !grantsOK([]string{store.NeedRoot, store.NeedNetwork}) {
		t.Error("a list the contract allows was refused")
	}
	for _, bad := range [][]string{{""}, {"admin"}, {"Root"}, {store.NeedRoot + " "}, {store.NeedRoot, "admin"}} {
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
