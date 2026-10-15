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
// shared/ is frozen, so this package does not touch the contract. It carries
// the OTHER half: the translation of a house number into a number the
// contract accepts, and the refusal of one it cannot carry. Which of the two
// ways the project takes is Thomas' decision (docs/release-weg.md, D4).
//
// WHAT THE OTHER WAY WOULD COST, measured and not guessed. The contract was
// probed with an overlay (nothing on disk was changed) that lets
// versionPattern take three OR four places and widens parseVersion to
// [4]int - two edits, shared/store/manifest.go:224 and :260. Then:
//
//	go test ./shared/...  ->  ONE case red: TestVersions, at the literal
//	                          "1.0.0.0" in its refusal list
//	                          (shared/store/manifest_test.go:204)
//	go test ./apps/...    ->  nothing red
//	go test ./tools/...   ->  two cases red, both in this package and both
//	                          written to go red exactly then
//
// So the four-place contract is three lines in two files, not the 550
// three-place literals the test tree spells - those stay valid, and so does
// the signed vector shared/testdata/release/release.json (version 1.1.0).
// That is the price of the one way; the price of the other is this file.

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
