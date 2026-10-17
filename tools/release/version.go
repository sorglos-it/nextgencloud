package release

import (
	"errors"
	"strconv"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

// The house numbering of this user (Z:\GitHub\claude-md\version.md) has FOUR
// places, v.x.y.z. The contract of this repo has THREE: store.ValidVersion
// takes MAJOR.MINOR.PATCH and nothing else (shared/store/manifest.go:224).
// Both numbers are measured, not assumed:
//
//	store.ValidVersion("0.0.1.0")      = false
//	store.Newer("0.0.1.0", "0.0.0.9")  = false
//	store.Newer("0.0.1.0", "1.0.0")    = false
//	store.Newer("1.0.0", "0.0.1.0")    = false
//
// The second group is the one that costs: a four-place number does not only
// fail at the gate, it also never compares HIGHER than anything. store.Newer
// answers false for an invalid version on either side, and the controller
// installs an update only when store.Newer says yes
// (apps/server-controller/classes/update/install.go:353,
// apps/server-api/classes/updates/rollout.go:253). So a release that carried a
// four-place number would be refused by Validate - and if a place ever
// stopped validating, it would be silently never installed instead. Silence
// is the worse of the two.
//
// THE DECISION IS TAKEN. Thomas chose on 15.10.2026: the CONTRACT learns four
// places, and this file is the bridge until it does. So the translation below
// is a stopgap with an end, not the answer - and the two cases that go red
// when the contract changes are what announces that the end has come
// (TestTheContractRefusesAFourPlaceNumber,
// TestTheWayBackRefusesWhatNoHouseNumberMade).
//
// BLOCKED - the item for V1.3, three lines in two files of frozen shared/.
// This session measured the cost and changed nothing there; only the
// coordinator writes shared/.
//
//	shared/store/manifest.go:224  versionPattern takes a fourth place:
//	                              ^(0|[1-9][0-9]{0,5})(\.(0|[1-9][0-9]{0,5})){2}
//	                              becomes the same with an optional
//	                              (\.(0|[1-9][0-9]{0,5}))? at the end, so
//	                              three places stay valid and four become so
//	shared/store/manifest.go:260  parseVersion returns [4]int instead of
//	                              [3]int; the loop over strings.Split needs
//	                              no other change, and a three-place number
//	                              pads with 0, so "1.0.0.1" sorts above
//	                              "1.0.0"
//	shared/store/manifest_test.go:204  the literal "1.0.0.0" leaves the
//	                              refusal list of TestVersions - it is the
//	                              one guard that asserts the old rule
//
// MEASURED, with an overlay, so nothing on disk was changed (15.10.2026,
// worktree on the local disk D:, main 48cb3aa):
//
//	go test ./shared/...  ->  ONE case red: TestVersions, at that literal,
//	                          every other shared package green
//	go test ./apps/...    ->  nothing red
//	go test ./tools/...   ->  two cases red, both in this package and both
//	                          written to go red exactly then
//
// So the four-place contract is three lines, not the 550 three-place literals
// the test tree spells - those stay valid, and so does the signed vector
// shared/testdata/release/release.json (version 1.1.0).
//
// WHAT THE ITEM STILL OWES, and it is not measured here: the order between a
// three-place and a four-place number. The probe pads with 0, which makes
// "1.0.0.1" newer than "1.0.0"; whoever writes the three lines writes a case
// for that pair, because store.Newer is what decides whether an update is
// installed at all.

// ErrHouseVersion says a house number cannot become a store version: it is
// not four places of digits, or a place is outside the range that survives
// the translation.
var ErrHouseVersion = errors.New("release: no store version for this house number")

// houseMax is the largest value each place of a house number may have.
//
// v and x become places of their own, so they may be as large as the contract
// allows there (0 to 999999, shared/store/manifest.go:224). y and z are
// folded into ONE place, so each is bounded by housePack: 999*1000+999 =
// 999999 is exactly the largest PATCH the contract takes. A place over its
// bound is REFUSED, never wrapped - a wrap would turn 0.0.1.0 and 0.0.0.1000
// into the same store version, and then two different releases would compare
// equal.
const (
	houseMaxOuter = 999999
	houseMaxInner = 999
	housePack     = 1000
)

// StoreVersion translates a four-place house number v.x.y.z into the
// MAJOR.MINOR.PATCH the contract accepts, and reports ErrHouseVersion for one
// it cannot carry.
//
// The mapping folds the two right-hand places into PATCH:
//
//	v.x.y.z  ->  v "." x "." (y*housePack + z)
//
// Two properties make it usable, and both are held by cases beside this file:
//
//   - It is reversible (HouseVersion), so nothing of the house number is lost.
//   - It keeps the order: house A sorts above house B exactly when
//     store.Newer says so of their translations. That is what matters, because
//     store.Newer is what decides whether an update is installed at all.
//
// The result is handed to store.ValidVersion before it is returned, so this
// function can never produce a version the contract would refuse - the
// contract judges, not the arithmetic here.
func StoreVersion(house string) (string, error) {
	p, err := parseHouse(house)
	if err != nil {
		return "", err
	}
	out := strconv.Itoa(p[0]) + "." + strconv.Itoa(p[1]) + "." + strconv.Itoa(p[2]*housePack+p[3])
	if !store.ValidVersion(out) {
		// Unreachable from outside: parseHouse bounds every place so that the
		// three results stay within what the contract takes. It is here
		// because the promise above is "the contract judges", and a promise
		// that is not checked is a comment.
		return "", ErrHouseVersion
	}
	return out, nil
}

// HouseVersion is the way back: the four-place house number a store version
// came from. A version the contract refuses, or a PATCH that no house number
// could produce, gives ErrHouseVersion.
func HouseVersion(version string) (string, error) {
	if !store.ValidVersion(version) {
		return "", ErrHouseVersion
	}
	parts := strings.Split(version, ".")
	patch, err := strconv.Atoi(parts[2])
	if err != nil || patch > houseMaxInner*housePack+houseMaxInner {
		return "", ErrHouseVersion
	}
	return parts[0] + "." + parts[1] + "." +
		strconv.Itoa(patch/housePack) + "." + strconv.Itoa(patch%housePack), nil
}

// parseHouse reads the four places of a house number. It is strict on
// purpose: exactly four groups, digits only, no sign, no leading zeros except
// the single digit 0, and every place inside its bound. version.md spells
// numbers that way, and a number spelled any other way is somebody's typo and
// not a release.
func parseHouse(house string) ([4]int, error) {
	var out [4]int
	parts := strings.Split(house, ".")
	if len(parts) != 4 {
		return out, ErrHouseVersion
	}
	for i, s := range parts {
		if s == "" || (len(s) > 1 && s[0] == '0') {
			return out, ErrHouseVersion
		}
		for j := 0; j < len(s); j++ {
			if s[j] < '0' || s[j] > '9' {
				return out, ErrHouseVersion
			}
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return out, ErrHouseVersion
		}
		max := houseMaxOuter
		if i >= 2 {
			max = houseMaxInner
		}
		if n > max {
			return out, ErrHouseVersion
		}
		out[i] = n
	}
	return out, nil
}
