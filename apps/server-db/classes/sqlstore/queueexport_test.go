package sqlstore

// What the case next door needs from the inside. The queue bound is no part of
// the contract, and the case that holds its FIGURE needs an open database,
// which this package cannot have in a test: both sqlite and migrate import
// sqlstore.
import "time"

const (
	// MaxTries is maxTries, the number of tries of one chain.
	MaxTries = maxTries
)

// QueueWait is queueWait, the bound on waiting for a turn to write.
func QueueWait() time.Duration { return queueWait }
