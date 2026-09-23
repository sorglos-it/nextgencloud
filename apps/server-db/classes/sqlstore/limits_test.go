package sqlstore

import (
	"slices"
	"strings"
	"testing"
)

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
