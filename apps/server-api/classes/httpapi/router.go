package httpapi

import (
	"net/http"
	"path"

	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Routes are the handlers of the paths before the WebSocket.
type Routes struct {
	Setup  http.HandlerFunc // GET and POST session.PathSetup
	Login  http.HandlerFunc // POST session.PathLoginStart
	Finish http.HandlerFunc // POST session.PathLoginFinish
	Logout http.HandlerFunc // POST session.PathLogout
	Trust  http.HandlerFunc // GET session.PathTrust
	Socket http.HandlerFunc // GET (Upgrade) session.PathSocket
}

// Mux returns the router of the contract: every path with exactly the methods
// the contract lists, anything else E_PROTOCOL, an unknown path E_NOTFOUND.
//
// It hands out an http.Handler and not the *http.ServeMux inside it. That mux
// is only half the router: whoever held it could hang routes of his own next
// to these, and it answers two kinds of request itself, past everything this
// package promises. A path it has to clean -- //api/v1/setup, /api/v1/./setup
// -- it answers with a redirect that carries Location, Content-Type
// text/html and a body giving the query string of the request back, and "*"
// of OPTIONS * with a bare 400: no Cache-Control: no-store, no nosniff, no
// CLOSE payload, and the first one quotes the input. The guard in front takes
// both away from it, so every answer that leaves here is one of this package.
func (rt Routes) Mux() http.Handler {
	mux := http.NewServeMux()
	add := func(pattern string, methods []string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if h == nil {
				// A route nobody wired is a mistake of this program, not of
				// the caller. Answering it keeps the connection whole where a
				// nil handler would panic.
				Fail(w, wire.CodeInternal)
				return
			}
			for _, m := range methods {
				if r.Method == m {
					h(w, r)
					return
				}
			}
			Fail(w, wire.CodeProtocol)
		})
	}
	add(session.PathSetup, []string{http.MethodGet, http.MethodPost}, rt.Setup)
	add(session.PathLoginStart, []string{http.MethodPost}, rt.Login)
	add(session.PathLoginFinish, []string{http.MethodPost}, rt.Finish)
	add(session.PathLogout, []string{http.MethodPost}, rt.Logout)
	add(session.PathTrust, []string{http.MethodGet}, rt.Trust)
	add(session.PathSocket, []string{http.MethodGet}, rt.Socket)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { Fail(w, wire.CodeNotFound) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only a path that is already what path.Clean makes of it reaches the
		// mux, and "*" never does: everything else is a path no route of the
		// contract has. The escaped path is the one the mux compares, so it
		// is the one that is compared here.
		p := r.URL.EscapedPath()
		if p == "" || p[0] != '/' || p != path.Clean(p) {
			Fail(w, wire.CodeNotFound)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
