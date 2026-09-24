// Package login serves the login and what hangs on it over HTTP: login/start,
// login/finish, logout and GET session.PathTrust, and with them the question
// every later path asks first -- which session is behind this cookie (Live).
// Every failed login answers E_LOGIN alike
// (shared/README.md, "Vor dem WebSocket"), so no answer tells which part
// failed or whether a user exists.
package login

import (
	json "encoding/json/v2"
	"errors"
	"net/http"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Handler serves the paths of the login.
type Handler struct {
	S     *state.State
	share shareCache
	// Two turns to speak, not one: a login challenge that cannot be kept
	// (login/start) and a failed login that cannot be counted (login/finish)
	// are two failures of the database, and a store can have one without the
	// other -- the two are written by calls of their own, OnceCreate and
	// BlobPut. Sharing a turn would let either of them keep the other quiet,
	// and the second one is the one nothing else says out loud.
	warnNonce warnThrottle
	warnLock  warnThrottle
}

// New returns a Handler. One per process: everything the Handler holds besides
// the state counts or throttles for the whole installation -- the two turns to
// speak above and the kept TOTP share (shareCache). A second Handler of the
// same process would say every line a second time and count the share a second
// time, so whoever wires the routes hands the same one to every path.
func New(s *state.State) *Handler { return &Handler{S: s} }

// userByName finds a user of the trust list by login name. It looks at every
// user whether the first matches, the last or none, where trust.List.UserByName
// stops at the one it finds. session.Challenge keeps a walk of its own for the
// same reason and says so; TestUserByNameLooksAtEveryUser holds this one to it
// without a clock.
//
// Measured on a list of the ten thousand users one may hold (trust.MaxUsers):
// trust.List.UserByName answers a name at the front of that list in a time too
// short to measure at all and walks the whole list for one nobody has, and that
// walk is a part of what session.Challenge itself costs on the same list, not a
// rounding error of it -- so the time says what the answer keeps quiet. Through
// the walk here those same two names came out alike.
//
// What no walk makes even is how long one comparison takes, which follows the
// letters of the name and not the list: a name that shares its length and its
// first bytes with many of the list's own costs a multiple of what a short one
// does -- and it costs that whether it exists or not, which is the question
// this walk is about.
//
// Neither of the two says by how much, on purpose. On the machine this was
// measured on the walk came to a fifth to a quarter of session.Challenge, and
// the long name to about twice the short one; a second machine measured that
// second ratio at five. The ratios move with the machine as much as the
// microseconds do, so a number here would read as a promise about the next
// machine.
func userByName(l *trust.List, name string) (trust.User, bool) {
	var found trust.User
	known := false
	for _, u := range l.Users {
		if u.Name == name {
			found, known = u, true
		}
	}
	return found, known
}

// mustJSON encodes an answer of the API (session.LoginChallenge,
// session.LoginResult, session.TrustChain): fixed structs of strings, numbers,
// bools and bytes, and one list of such structs, so encoding cannot fail; a
// failure all the same answers E_INTERNAL and gives nil back, and the caller
// writes nothing more. session.Challenge has already
// held its answer against session.ParseLoginChallenge, so what is written here
// is what the browser accepts -- FuzzStartBody reads every answer back with
// that same parser.
func mustJSON(w http.ResponseWriter, v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return nil
	}
	return b
}

// isNotFound reports the "not found" of dbapi, which is no failure where a row
// may be missing.
func isNotFound(err error) bool { return errors.Is(err, dbapi.ErrNotFound) }
