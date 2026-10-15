//go:build race

package login

// timedRuns under -race. Race detection costs a request a multiple of what it
// costs without, so far fewer of them already clear timedFloor -- and the whole
// measurement stays a test and not a wait. The rule is untouched: the number of
// requests moves, the floor does not.
const timedRuns = 500
