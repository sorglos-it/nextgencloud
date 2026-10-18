package httpapi

import (
	"errors"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
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
// Of other packages it reads one, and only what that one says about itself:
// the three sentinels package dbapi gives its stores. Everything else of
// another package is the handler's to answer. A bad signature is E_VERIFY on
// the setup path and E_LOGIN on the login paths, because no answer of a login
// may say what failed; a missing row (dbapi.ErrNotFound) is the normal case
// for the setup and a damaged database for the trust list, and only the
// handler knows which, so it says so itself before it asks here at all.
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
	case errors.Is(err, state.ErrRefused):
		// Package trust would not take the version that was handed in, and
		// state puts no finer name to it. By the rule above that is the
		// caller's -- but E_INTERNAL is what it answered before it had a name
		// at all, and saying E_VERIFY for it is the setup path's decision and
		// not this one's. What matters here is that it is not the store: no
		// waiting mends a version that does not verify.
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
	case errors.Is(err, dbapi.ErrNotFound), errors.Is(err, dbapi.ErrUsed),
		errors.Is(err, dbapi.ErrInvalid):
		// The store answered, and all three say so: the row is not there, the
		// value was taken already, the call was wrong. None of them is a
		// database that is away, and asking again brings the same answer --
		// ErrInvalid and ErrUsed because nothing about the call changes by
		// waiting, ErrNotFound because a row that is not there is an answer.
		//
		// These three are dbapi's whole set, which is the reason they stand
		// here rather than in a handler: what the arm below claims is right
		// only if what must stay out of it can be listed to the end, and for
		// the store's own words it can be. An error of a package that is not
		// dbapi or state is its handler's, and the fault function of package
		// setup is one place this API does that -- login/start.go and
		// login/login.go answer their own errors from secrets and json the
		// same way, without coming through here.
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
	// It rests on two things, and both are kept elsewhere. Package state names
	// the faults of its own that a run can reach: the errors above, and nothing
	// of package trust left unnamed on the way out (state.ownRows for a row of
	// ours, state.handedIn for a version brought in). And whoever hands Fault
	// an error of a package it does not read has to answer that one himself.
	// This API has two, a configured key that will not parse and a damaged row
	// of the counting, and the fault function of package setup answers both --
	// no longer by name and no longer before this call, since 9b60c69: it asks
	// here first and then turns every E_UNREACHABLE of the whole path into
	// E_INTERNAL, which is the right word for those two and for a setup that
	// cannot write. The login paths answer their own the same way, one step
	// earlier: login.sessionFault says what each of the three above means about
	// a session row before anything of it reaches here. The store's own three
	// are the exception, and they are read above rather than left to the
	// handlers because every path that touches a store can meet them.
	//
	// What is left over are the guards package state keeps for cases that
	// cannot arise -- writing out a version it has just read, rolling an ID,
	// sealing a key it made a line earlier. Should one of those ever fail, the
	// word here would be the wrong one; naming each of them is the rebuild of
	// that package's errors this arm was meant to save.
	return wire.CodeUnreachable
}
