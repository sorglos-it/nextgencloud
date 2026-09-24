package login

import (
	"net/http"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Trust answers GET session.PathTrust?from=N with the versions of the trust
// list from N on, and only with a session: that list names every user, every
// key and every node of the installation.
//
// The session comes first and the number after it, so a request without one is
// turned away before anything of it is read. What the session holds is nothing
// this path needs; that there is one is the whole of it.
//
// Three answers besides the chain, all of them from the contract's table: a
// number that is no version is E_PROTOCOL, the one past the newest is 204 --
// the shell asks with the number after the last it has, and that is the
// ordinary "nothing new" -- and anything beyond that is E_NOTFOUND.
func (h *Handler) Trust(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, ok := h.Live(ctx, r); !ok {
		httpapi.Fail(w, wire.CodeLogin)
		return
	}
	from, err := session.ParseFrom(r.URL.Query().Get("from"))
	if err != nil {
		httpapi.Fail(w, wire.CodeProtocol)
		return
	}
	chain, err := h.S.Chain(ctx, from)
	if err != nil {
		// httpapi.Fault is the one place that picks the code for an error of
		// package state, so no path answers one of them its own way.
		// FromRows says which side the version came from: it is read out of
		// this installation's own rows, and the number in the request is the
		// only part of it the caller brought. That is what makes
		// state.ErrOldVersion the 404 of the contract's table here, where the
		// same error on the setup path is the caller's bad signature.
		httpapi.Fail(w, httpapi.Fault(err, httpapi.FromRows))
		return
	}
	if len(chain.Versions) == 0 {
		httpapi.NoContent(w)
		return
	}
	b := mustJSON(w, chain)
	if b == nil {
		return
	}
	httpapi.Answer(w, http.StatusOK, b)
}
