package httpapi

import (
	"errors"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The errors of package state get their code here. Which code one deserves
// depends on where the version it speaks of came from: one the caller handed
// in is the caller's business, one the API read out of its own rows is the
// installation's. state.ErrInstallation and state.ErrOldVersion arise on both
// paths and mean something else on each, so each path has its own function
// instead of one that would have to guess.
//
// Neither reads the errors of other packages. A bad signature is E_VERIFY on
// the setup path and E_LOGIN on the login paths, because no answer of a login
// may say what failed; a missing row (dbapi.ErrNotFound) is the normal case
// for the setup and a damaged database for the trust list. Only the handler
// knows which, so it answers those itself.

// TakeFault is the code for an error of package state on a path that takes
// something in: POST session.PathSetup and every later path that hands the
// state a version of the trust list. There the fault lies in what the caller
// brought, so a version that does not fit this installation's chain is
// E_VERIFY.
func TakeFault(err error) wire.Code {
	switch {
	case errors.Is(err, state.ErrBadVersion):
		// A number no version has does not come out of a row: it comes out of
		// the request, where session.ParseFrom refuses it already. E_INTERNAL
		// would blame the installation for a request that was wrong.
		return wire.CodeProtocol
	case errors.Is(err, state.ErrNoTrust):
		// There is no list to add to yet, and the contract answers a thing
		// that is not there with 404.
		return wire.CodeNotFound
	case errors.Is(err, state.ErrFork), errors.Is(err, state.ErrInstallation),
		errors.Is(err, state.ErrOldVersion):
		return wire.CodeVerify
	}
	// state.ErrSetting, state.ErrBusy and anything else: the caller can do
	// nothing about any of them. ErrBusy would pass in a moment, but E_LOCKED
	// belongs to the failed logins alone, and with one API on one database
	// nobody else holds the lock anyway.
	return wire.CodeInternal
}

// ReadFault is the code for an error of package state on a path that only
// reads: GET session.PathTrust and the lookups behind a login. There every
// version is one this installation wrote itself, so only the number in the
// request can be the caller's fault.
func ReadFault(err error) wire.Code {
	switch {
	case errors.Is(err, state.ErrBadVersion):
		return wire.CodeProtocol
	case errors.Is(err, state.ErrOldVersion), errors.Is(err, state.ErrNoTrust):
		// A number above the newest version, and no list at all: the contract
		// answers both 404, and neither answer says how many versions there
		// are.
		return wire.CodeNotFound
	}
	// state.ErrInstallation and state.ErrFork speak here of rows this
	// installation holds itself, so they say the database is damaged, not
	// that anybody sent anything wrong: E_VERIFY would send the shell looking
	// for a fault of its own that it will not find.
	return wire.CodeInternal
}
