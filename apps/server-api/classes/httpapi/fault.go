package httpapi

import (
	"errors"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Origin says where the version an error of package state speaks of came
// from. Two of those errors mean something else on each path, and the wrong
// choice is silent, so the call site names the origin instead of a function
// whose name it has to remember.
type Origin string

const (
	// FromRequest: the caller handed the version in -- POST session.PathSetup
	// and every later path that hands the state a version of the trust list.
	// There the fault lies in what the caller brought, so a version that does
	// not fit this installation's chain is E_VERIFY.
	FromRequest Origin = "request"
	// FromRows: the API read the version out of its own rows -- GET
	// session.PathTrust and the lookups behind a login. There only the number
	// in the request can be the caller's fault; everything else is the
	// installation's.
	FromRows Origin = "rows"
)

// Fault is the code for an error of package state. Only E_VERIFY tells the
// caller his signature was wrong, so only FromRequest can lead to it: an
// origin nobody named is answered like FromRows, which blames nobody.
//
// It also tells the two kinds of "nobody's fault" apart, which is what every
// caller of it would otherwise do differently: a store that did not answer is
// E_UNREACHABLE and over when it answers again, rows that are wrong are
// E_INTERNAL and stay wrong until somebody mends them.
//
// Fault reads no errors of other packages. A bad signature is E_VERIFY on the
// setup path and E_LOGIN on the login paths, because no answer of a login may
// say what failed; a missing row (dbapi.ErrNotFound) is the normal case for
// the setup and a damaged database for the trust list. Only the handler knows
// which, so it answers those itself.
func Fault(err error, from Origin) wire.Code {
	switch {
	case errors.Is(err, state.ErrBadVersion):
		// A number no version has does not come out of a row: it comes out of
		// the request, where session.ParseFrom refuses it already. E_INTERNAL
		// would blame the installation for a request that was wrong.
		return wire.CodeProtocol
	case errors.Is(err, state.ErrNoTrust):
		// There is no list to show or to add to, and the contract answers a
		// thing that is not there with 404.
		return wire.CodeNotFound
	case errors.Is(err, state.ErrOldVersion):
		// Handed in, a number that is not the next one is a version played
		// again or one that skips. Read, it is a number above the newest --
		// exactly the 404 of the contract's table, and it says nothing about
		// how many versions there are.
		if from == FromRequest {
			return wire.CodeVerify
		}
		return wire.CodeNotFound
	case errors.Is(err, state.ErrFork), errors.Is(err, state.ErrInstallation):
		// A second chain under the same number, and a list of another
		// installation: both can only be faults of what was handed in.
		// state.ErrDamaged is what the same mismatch is called among our own
		// rows, and it falls to E_INTERNAL below.
		if from == FromRequest {
			return wire.CodeVerify
		}
		return wire.CodeInternal
	case errors.Is(err, state.ErrDamaged), errors.Is(err, state.ErrSetting),
		errors.Is(err, state.ErrBusy):
		// The store answered, and what it answered with is wrong: rows of this
		// installation's own that contradict each other or do not read. The
		// caller can do nothing about any of them, and neither can a retry.
		//
		// ErrBusy rests on there being one API on one database, where nobody
		// else holds the lock: with a second one it would pass by itself in a
		// moment, and 500 tells the far side not to try again. On the day a
		// second API joins, ErrBusy belongs apart with its own answer --
		// E_LOCKED is not it, that one belongs to the failed logins alone
		// (with Retry-After, counted in the buckets of package lock).
		return wire.CodeInternal
	}
	// Nothing of package state's own is left, so this came up through it from
	// the store: a database that is away, a table that is locked, a disk that
	// is full. None of that is broken for good, so it is the one answer that
	// says come back -- and the difference is not decoration. The same damage
	// is met at two different steps depending on what a node has cached, and
	// without this arm the step would decide the answer: a hole in the trust
	// rows reads as ErrSetting either way (500), while a store that says
	// nothing reads as its own error either way (503).
	//
	// It rests on package state naming its own faults: the errors above, and
	// nothing of package trust left unnamed on the way out (state.ownRows).
	return wire.CodeUnreachable
}
