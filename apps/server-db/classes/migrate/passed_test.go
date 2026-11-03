package migrate

import (
	"errors"
	"testing"
)

// TestPassedReadsTheColumnAndNotADriversNumber holds the second bolt of take
// on its own, because it cannot be reached through Apply as long as the
// statement says "until <= ?": measured on both databases, such a row is
// stopped by the SQL first - SQLite orders every text behind every number and
// the runner reports "the lock is held", MariaDB refuses the comparison with
// Error 1292 (docs/schloss-ablauf-riegel.md). The bolt is aimed at the day
// somebody writes "until > ?" for "held", and until that day this is the only
// place that can say whether it judges right.
//
// Why bytes and not an int64: what the column really holds is what has to be
// judged, and a Scan into an int64 would hand a text where a number belongs
// to the driver - a different error on every database - instead of to this
// one sentence.
func TestPassedReadsTheColumnAndNotADriversNumber(t *testing.T) {
	const now = int64(1790000000)
	cases := []struct {
		name    string
		until   string
		passed  bool
		wantErr error
	}{
		{"one second left", "1790000001", false, nil},
		{"the deadline is now", "1790000000", true, nil},
		{"one second ago", "1789999999", true, nil},
		{"the whole lease left", "1790000900", false, nil},
		{"long gone", "1", true, nil},
		{"zero", "0", true, nil},
		{"negative", "-5", true, nil},
		{"no number at all", "irgendwann", false, errLockBent},
		{"empty", "", false, errLockBent},
		{"a number with a word behind it", "1790000900x", false, errLockBent},
		{"a number with a space in front", " 1789999999", false, errLockBent},
		{"a decimal point", "1789999999.0", false, errLockBent},
		{"hexadecimal", "0x6a9a0000", false, errLockBent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := passed([]byte(c.until), now)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("error: %v, want %v", err, c.wantErr)
			}
			if got != c.passed {
				t.Errorf("passed: %v, want %v", got, c.passed)
			}
		})
	}
}

// TestPassedSaysSomethingElseThanTheOtherAnswers: a deadline that is no
// number must not read as "somebody else is migrating". That answer sends
// whoever reads the journal looking for a second server, and here there is
// none - there is a row, or a column, that is not what the migrations write.
//
// It is held on the sentence and not only on the sort, because the sentence
// is what reaches a person.
func TestPassedSaysSomethingElseThanTheOtherAnswers(t *testing.T) {
	_, err := passed([]byte("irgendwann"), 1790000000)
	if err == nil {
		t.Fatal("a deadline that is no number was accepted")
	}
	if errors.Is(err, errLockLost) {
		t.Error("it reads as a lost lock")
	}
	for _, wrong := range []string{"another server", "is running the migrations"} {
		if contains(err.Error(), wrong) {
			t.Errorf("the sentence carries %q: %v", wrong, err)
		}
	}
	if !contains(err.Error(), "no number") {
		t.Errorf("the sentence does not say what is wrong: %v", err)
	}
}

// contains is strings.Contains, spelled out so that this file needs no import
// for one call.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
