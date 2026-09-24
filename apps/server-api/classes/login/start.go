package login

import (
	"log/slog"
	"net/http"
	"sync"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// nonceWarnTTL is how seldom a login challenge that could not be kept is said
// out loud. The line stands for a state of the database and not for a request,
// so one of them per five minutes says everything there is to say -- and
// login/start has no brake in front of it, so anything oftener would be a way
// to fill a disk by asking.
const nonceWarnTTL = 300

// warnThrottle hands out the turn to say one line, at most one per window.
type warnThrottle struct {
	mu   sync.Mutex
	last int64
	said bool
}

// take reports whether the line may be said now, and takes the turn if so.
func (t *warnThrottle) take(now, every int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.said && now-t.last < every {
		return false
	}
	t.last, t.said = now, true
	return true
}

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
	// The row is read for every name by the name, never by the ID of the user
	// the list gave: a name nobody has has no ID. What an empty ID gives is not
	// in the contract -- dbapi/dbtest pins UserGet only for a name that is not
	// there, and memstore refuses an empty ID in UserPut with dbapi.ErrInvalid,
	// so a store that refuses it here too is within the contract. It would then
	// answer every unknown name E_UNREACHABLE and every known one a challenge.
	// By the name both go the same way, and the database's own query log no
	// longer carries a "where id = ''" that only unknown names produce.
	//
	// No error of this call becomes an answer of its own either, and that is
	// the same rule one door further on: what UserByName gives for a name
	// nobody has is not in the contract at all -- it carries no doc comment,
	// and dbapi/dbtest pins it only for a name that exists. A store that says
	// so with an error of its own instead of dbapi.ErrNotFound would tell the
	// two names apart again. A database that is really gone misses both names
	// alike, the challenge is built either way, and the login that follows
	// fails with E_LOGIN.
	//
	// A row that is not there means no TOTP, and for a name the list does not
	// know not even that matters: session.Challenge ignores the TOTP of a name
	// it cannot place and draws it from the share instead. What is left between
	// the two -- handing a row back against saying there is none, and the one
	// write below -- came out under what two batches of the same name differ by
	// on the machine this was measured on
	// (TestStartTakesTheSameTimeForAnUnknownName measures both names and the
	// same name twice).
	u, known := userByName(v.List, req.Name)
	row, _ := h.S.DB.UserByName(ctx, req.Name)
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
		//
		// A row that cannot be written changes nothing about the answer, and
		// that is the whole point: this is the one call a known name makes
		// that an unknown one does not, so it is the one call whose failure
		// must not show. A database that reads but cannot write -- a full
		// disk, a read replica, a locked table, a quota that ran out -- would
		// otherwise answer every name that exists E_UNREACHABLE and every
		// other one with a challenge: one request per name, no measuring, no
		// noise, and the whole made-up answer undone. The login that follows
		// finds no nonce and fails with E_LOGIN, which is what a wrong
		// password gets, so nothing is let through either.
		//
		// Swallowed, but not silent. Every login of the installation then
		// fails with E_LOGIN and nothing outside says why, where the 503 used
		// to; whoever runs it would be left with "nothing works and I do not
		// know why". So it is said out loud -- once per nonceWarnTTL and
		// without a word of the request, so that it costs no answer and cannot
		// be made to fill a disk by asking often. What the line carries besides
		// the fixed text is the database's own error.
		if err := h.S.DB.OnceCreate(ctx, dbapi.OnceNonce, nonce, u.ID, now+session.ChallengeLifetime); err != nil &&
			h.warn.take(now, nonceWarnTTL) {
			slog.Error("a login challenge could not be kept; logins fail until the database can be written again",
				"error", err)
		}
	}
	if b := mustJSON(w, c); b != nil {
		httpapi.Answer(w, http.StatusOK, b)
	}
}
