//go:build !race

package login

// timedRuns is how many requests one measurement of
// TestStartTakesTheSameTimeForAnUnknownName takes. A request takes tens of
// microseconds and a clock may resolve a millisecond: 25 requests are then 25
// zeroes, and "0 > 2*0" is false, so a comparison of them passes whatever the
// handler does. Two thousand requests are tens of milliseconds, which every
// clock a test runs on sees.
//
// Should a machine ever be quick enough that two thousand stay under
// timedFloor, this number goes up -- never the floor down.
const timedRuns = 2000
