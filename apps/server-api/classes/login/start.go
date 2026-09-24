package login

import (
	"net/http"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Start answers POST session.PathLoginStart with a challenge, for names that
// do not exist too.
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, ok := httpapi.Body(w, r)
	if !ok {
		return
	}
	req, err := session.ParseLoginStart(body)
	if err != nil {
		httpapi.Fail(w, wire.CodeProtocol)
		return
	}
	now := h.S.Now()
	// The wait is read before anything else is looked up, and it is read for
	// every name: the counting keeps its rows per bucket and not per name
	// (package lock), so this look costs the same whether a user of that name
	// exists or not. login/start never counts a failure of its own -- only
	// login/finish does -- so asking for a name never locks it.
	wait, err := lock.Check(ctx, h.S.DB, req.Name, now)
	if err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	if wait > 0 {
		httpapi.FailRetry(w, wire.CodeLocked, wait)
		return
	}
	// Before the setup has run there is no list, and then no name is known.
	// E_UNREACHABLE and not E_NOTFOUND: the contract's table gives login/start
	// no 404 at all (shared/README.md, "Vor dem WebSocket"), and one would be
	// an answer this path gives in no other case.
	v, err := h.S.Trust(ctx)
	if err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	nonce, err := secrets.Bytes(session.NonceSize)
	if err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return
	}
	// The name is looked up for two things and two only: the row that holds the
	// TOTP secret, and the nonce that is kept for a real user. The answer
	// itself comes from session.Challenge, which walks the list on its own.
	// userByName and not trust.List.UserByName: that one stops at the name it
	// finds, and the time it saves follows whether the name exists (see there).
	//
	// The row is read for every name, known or not, and a missing row is no
	// failure -- a name nobody has simply has none, and asking for it must cost
	// what asking for a real one costs.
	u, known := userByName(v.List, req.Name)
	row, err := h.S.DB.UserGet(ctx, u.ID)
	if err != nil && !isNotFound(err) {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	// session.Challenge builds the whole answer, for a name nobody has too:
	// it walks the list and builds the made-up answer either way, so neither
	// the shape of the answer nor the time it takes says whether the user
	// exists. The API adds nothing of its own here and takes no shortcut.
	c, err := session.Challenge(v.List, session.ChallengeInput{
		Name:        req.Name,
		TOTP:        len(row.TOTP) > 0,
		TOTPShare:   h.totpShare(ctx),
		Key:         h.S.Cfg.ChallengeKey.Bytes(),
		Nonce:       nonce,
		Now:         now,
		MaxLifetime: h.S.Cfg.SessionLifetime,
	})
	if err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return
	}
	if known {
		// Only a real nonce is kept. The made-up answer carries one that was
		// never stored, so login/finish fails with E_LOGIN like any other
		// wrong attempt -- the same answer a wrong password gets.
		if err := h.S.DB.OnceCreate(ctx, dbapi.OnceNonce, nonce, u.ID, now+session.ChallengeLifetime); err != nil {
			httpapi.Fail(w, wire.CodeUnreachable)
			return
		}
	}
	if b := mustJSON(w, c); b != nil {
		httpapi.Answer(w, http.StatusOK, b)
	}
}
