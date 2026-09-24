// Package login serves the login and what hangs on it over HTTP: today
// login/start, and in this package later login/finish, logout and GET
// session.PathTrust. Every failed login answers E_LOGIN alike
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
}

// New returns a Handler.
func New(s *state.State) *Handler { return &Handler{S: s} }

// userOf finds a user of the trust list by ID.
func (h *Handler) userOf(l *trust.List, id string) (trust.User, bool) { return l.User(id) }

// userByName finds a user of the trust list by login name. It looks at every
// user whether the first matches, the last or none, where trust.List.UserByName
// stops at the one it finds. session.Challenge keeps a walk of its own for the
// same reason and says so; TestUserByNameLooksAtEveryUser holds this one to it
// without a clock.
//
// Measured on a list of the ten thousand users one may hold (trust.MaxUsers),
// next to the 70 to 90 microseconds session.Challenge takes on that list:
// trust.List.UserByName answers a name at the front of the list in a time too
// short to measure and walks the whole list -- 18 microseconds -- for one
// nobody has, so the time says what the answer keeps quiet. Through this walk
// the same two names cost 20.6 and 20.6 microseconds.
//
// What no walk makes even is how long one comparison takes, which follows the
// letters of the name and not the list: a name that shares its length and its
// first bytes with many of the list's own costs 41 microseconds where a short
// one costs 21 -- and it costs that whether it exists or not, which is the
// question this walk is about.
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
// session.LoginResult): fixed structs of strings, numbers and bytes, so
// encoding cannot fail; a failure all the same answers E_INTERNAL and gives
// nil back, and the caller writes nothing more. session.Challenge has already
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
