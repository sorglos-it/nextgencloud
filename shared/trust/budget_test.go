package trust

import "testing"

// TestTheBudgetAndTheCapsAgree holds the promise listBudget makes about
// itself: "every array holds at most what Validate allows". The two are
// separate walls -- the budget bounds an array while a version from OUTSIDE
// is decoded, the comparison in Validate bounds a list however it was built,
// including one the interface assembles in the browser and signs
// (coreapp/users.go). Raising one alone is what this case refuses to let
// happen quietly.
//
// WHY THIS IS NOT MEASURED THROUGH Parse, and it is a finding of its own:
// Parse maps BOTH refusals to ErrInvalid (trust.go:244 for the budget,
// trust.go:247 for Validate). From outside the package the two walls cannot
// be told apart, so no case that goes through Parse can say which one fired
// -- a drift between the budget and a cap would be invisible to it. This case
// is therefore in package trust and reads the map itself.
func TestTheBudgetAndTheCapsAgree(t *testing.T) {
	want := map[string]int{
		"rp/origins":        MaxOrigins,
		"users":             MaxUsers,
		"users/*/roles":     MaxRoles,
		"users/*/keys":      MaxKeysPerUser,
		"roles":             MaxRoles,
		"roles/*/black":     MaxEntries,
		"roles/*/white":     MaxEntries,
		"nodes":             MaxNodes,
		"nodes/*/rootUsers": MaxRoot,
		"nodes/*/rootRoles": MaxRoot,
		"stores":            MaxStores,
		"auto":              MaxAuto,
	}
	// The ground set first: an empty map would pass every loop below.
	if len(listBudget.Items) != len(want) || len(want) != 12 {
		t.Fatalf("the budget bounds %d arrays, this case knows %d", len(listBudget.Items), len(want))
	}
	for path, limit := range want {
		got, ok := listBudget.Items[path]
		switch {
		case !ok:
			t.Errorf("the budget does not bound %s", path)
		case got != limit:
			t.Errorf("the budget allows %d of %s, the cap is %d", got, path, limit)
		}
	}
	for path := range listBudget.Items {
		if _, ok := want[path]; !ok {
			t.Errorf("the budget bounds %s, which no cap of this case names", path)
		}
	}
	// A cap of zero would make every comparison above hold while the budget
	// let nothing through, so the values themselves are held against sanity.
	for path, limit := range want {
		if limit <= 0 {
			t.Errorf("%s is bounded at %d", path, limit)
		}
	}
}
