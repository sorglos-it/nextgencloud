package httpapi

import (
	"net/http"

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
func (rt Routes) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	add := func(path string, methods []string, h http.HandlerFunc) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
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
	return mux
}
