package release

import (
	"errors"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

// WHAT THIS FILE MEASURES.
//
// The contradiction of docs/release-weg.md D4, at the contract itself. These
// four lines are the measurement, not a claim, and they run on every go test:
// a four-place house number is refused by store.ValidVersion AND never
// compares higher with store.Newer. The second half is the dangerous one,
// because it is silent.
//
// This case is also the WATCHER of the V1.3 item (see the head of version.go):
// Thomas decided on 15.10.2026 that the contract learns four places, so this
// case is MEANT to go red the day those three lines land in shared/. A red
// here is then not a defect, it is the announcement - and what to do with it
// stands in the failure message, not only in a document.
func TestTheContractRefusesAFourPlaceNumber(t *testing.T) {
	if store.ValidVersion("0.0.1.0") {
		t.Error("store.ValidVersion takes a four-place number now: the V1.3 item has landed. " +
			"Then StoreVersion/HouseVersion are no longer needed as a bridge - delete them with " +
			"their cases and hand the house number to the contract directly (version.go, head)")
	}
	if !store.ValidVersion("1.0.0") {
		t.Fatal("store.ValidVersion refused a three-place number: the probe itself is broken")
	}
	// Both directions, so this is not read as "the older one simply lost".
	if store.Newer("0.0.1.0", "0.0.0.9") || store.Newer("1.0.0", "0.0.1.0") || store.Newer("0.0.1.0", "1.0.0") {
		t.Error("store.Newer answered true for a four-place number")
	}
}

// TestTheTranslationIsOneTheContractTakes is the assurance of StoreVersion:
// whatever comes out is a version store.ValidVersion accepts.
func TestTheTranslationIsOneTheContractTakes(t *testing.T) {
	for _, house := range []string{"0.0.0.0", "0.0.1.0", "0.2.0.0", "1.0.0.0", "7.3.12.400", "999999.999999.999.999"} {
		got, err := StoreVersion(house)
		if err != nil {
			t.Errorf("StoreVersion(%q) = error %v, want a version", house, err)
			continue
		}
		if !store.ValidVersion(got) {
			t.Errorf("StoreVersion(%q) = %q, which store.ValidVersion refuses", house, got)
		}
		t.Logf("%s -> %s", house, got)
	}
}

// TestTheTranslationLosesNothing holds the way back: every house number that
// goes in comes out again.
func TestTheTranslationLosesNothing(t *testing.T) {
	for _, house := range []string{"0.0.0.0", "0.0.1.0", "0.0.0.1", "0.0.999.999", "1.2.3.4", "999999.1.0.0"} {
		v, err := StoreVersion(house)
		if err != nil {
			t.Fatalf("StoreVersion(%q): %v", house, err)
		}
		back, err := HouseVersion(v)
		if err != nil {
			t.Fatalf("HouseVersion(%q): %v", v, err)
		}
		if back != house {
			t.Errorf("round trip %q -> %q -> %q", house, v, back)
		}
	}
}

// TestTheTranslationKeepsTheOrder is the property the controller depends on:
// it installs an update only when store.Newer says the new version is higher
// (install.go:353). A translation that scrambled the order would turn a newer
// release into one that is never installed - and nothing would go red.
//
// The pairs are written as "the left one is the newer house number", and the
// case asks store.Newer about the translations, not about the house numbers.
func TestTheTranslationKeepsTheOrder(t *testing.T) {
	pairs := [][2]string{
		{"0.0.0.1", "0.0.0.0"},
		{"0.0.1.0", "0.0.0.999"}, // the carry between the folded places
		{"0.1.0.0", "0.0.999.999"},
		{"1.0.0.0", "0.999999.999.999"},
		{"2.0.0.0", "1.999999.999.999"},
	}
	for _, p := range pairs {
		hi, err := StoreVersion(p[0])
		if err != nil {
			t.Fatalf("StoreVersion(%q): %v", p[0], err)
		}
		lo, err := StoreVersion(p[1])
		if err != nil {
			t.Fatalf("StoreVersion(%q): %v", p[1], err)
		}
		if !store.Newer(hi, lo) {
			t.Errorf("%s (%s) should be newer than %s (%s), store.Newer says no", p[0], hi, p[1], lo)
		}
		if store.Newer(lo, hi) {
			t.Errorf("%s (%s) must not be newer than %s (%s)", p[1], lo, p[0], hi)
		}
	}
}

// TestAnImpossibleHouseNumberIsRefused is the case the order asks for: an
// invalid number is REFUSED, and every reason has its own line so a refusal
// cannot stand in for another one.
func TestAnImpossibleHouseNumberIsRefused(t *testing.T) {
	refuse := map[string]string{
		"three places":      "1.0.0",
		"five places":       "1.0.0.0.0",
		"two places":        "1.0",
		"empty":             "",
		"empty place":       "1.0..0",
		"trailing dot":      "1.0.0.",
		"leading zero":      "1.0.01.0",
		"letters":           "1.0.0.x",
		"the dev fallback":  "0.0.0-dev",
		"a sign":            "1.0.0.-1",
		"space":             "1.0.0. 1",
		"third place over":  "1.0.1000.0",
		"fourth place over": "1.0.0.1000",
		"first place over":  "1000000.0.0.0",
	}
	for why, house := range refuse {
		got, err := StoreVersion(house)
		if !errors.Is(err, ErrHouseVersion) {
			t.Errorf("%s: StoreVersion(%q) = %q, %v; want ErrHouseVersion", why, house, got, err)
		}
		if got != "" {
			t.Errorf("%s: StoreVersion(%q) returned %q beside its error", why, house, got)
		}
	}
}

// TestTheWayBackRefusesWhatNoHouseNumberMade keeps HouseVersion honest: a
// version the contract refuses, and one whose PATCH is larger than the fold
// can produce, have no house number.
func TestTheWayBackRefusesWhatNoHouseNumberMade(t *testing.T) {
	for _, v := range []string{"0.0.1.0", "0.0.0-dev", "1.0", ""} {
		if _, err := HouseVersion(v); !errors.Is(err, ErrHouseVersion) {
			// "0.0.1.0" is the second watcher of the V1.3 item: once the
			// contract takes four places, store.ValidVersion lets it through
			// here too. See TestTheContractRefusesAFourPlaceNumber.
			t.Errorf("HouseVersion(%q) did not refuse", v)
		}
	}
	// 999999 is the largest PATCH both the contract and the fold allow, so it
	// has a house number; the bound is checked from the other side by
	// TestTheTranslationIsOneTheContractTakes, which translates
	// 999999.999999.999.999 successfully.
	if _, err := HouseVersion("1.1.999999"); err != nil {
		t.Errorf("HouseVersion(1.1.999999) = %v, want the house number 1.1.999.999", err)
	}
}
