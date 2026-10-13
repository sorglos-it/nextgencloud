package login

import (
	"log/slog"
	"net/http"
	"sync"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbwait"
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
		// FailCoded and not Fail: a read that only did not come free in time
		// gets the Retry-After and the log reason of httpapi/busy.go. The code
		// stays the one written here -- there is no word of its own for busy
		// (P9) -- and the error only picks the header. It cannot tell the two
		// names apart for the reason just given: the look costs the same for a
		// name that exists and one that does not, so the failure of it does
		// too (TestStartAnswersTheSameWhenTheCountingIsBusy).
		httpapi.FailCoded(w, wire.CodeUnreachable, err)
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
		// The list is read before the name is looked up in it, so this one is
		// alike for every name as well.
		httpapi.FailCoded(w, wire.CodeUnreachable, err)
		return
	}
	nonce, err := secrets.Bytes(session.NonceSize)
	if err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return
	}
	// decoy is the ID the nonce of a MADE-UP challenge is kept under, and it
	// is drawn here, before anything is known about the name, so that the
	// draw itself is not one of the things that differ between two names.
	//
	// A fresh secrets.ID and not a fixed word: it is the shape a real user ID
	// has, 16 random bytes as base64url, so the row of a made-up challenge is
	// not the odd one out in the table either -- a fixed sentinel would be a
	// name oracle for whoever reads the database, which is a smaller circle
	// than the internet but not an empty one. It is the ID of nobody by the
	// same argument that makes a nonce unguessable, and even a collision would
	// let nothing through: login/finish holds the ID beside the nonce against
	// the user of a certificate it has ALREADY verified against the trust list
	// (finish.go, at OnceUse), so the user of that certificate is always one
	// the list has and never a number drawn here.
	decoy, err := secrets.ID()
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
	// The nonce is kept for EVERY name, and that is the one thing this path
	// does differently since point 50. It used to be the single call a known
	// name made that an unknown one did not, and that made the failure of
	// that call unanswerable: a database that reads but cannot write would
	// have answered every name that exists E_UNREACHABLE and every other one
	// a challenge -- a perfect oracle, one request per name, no measuring.
	// So the failure was swallowed, and the price was paid at the other end:
	// the login that follows finds no nonce and fails with E_LOGIN, the
	// browser reads "Anmeldung fehlgeschlagen.", and that is the sentence a
	// WRONG PASSWORD gets. The person is told something untrue about his own
	// password while the installation is the thing that is broken.
	//
	// Making the write for both names is what buys the way out. For a name
	// nobody has the row is kept under decoy, so it is a nonce for nobody and
	// denies at login/finish exactly as a nonce that was never kept at all.
	// What it costs is named: a request for any name now writes a row, where
	// before only a name that exists did. The rows expire with the challenge
	// (session.ChallengeLifetime) and OncePurge frees them, the path in front
	// is counted (package lock, read above), and the write is the same write
	// the known name always made -- so this is more of a load that was
	// already there, not a new kind of one.
	id := u.ID
	if !known {
		id = decoy
	}
	if err := h.S.DB.OnceCreate(ctx, dbapi.OnceNonce, nonce, id, now+session.ChallengeLifetime); err != nil {
		// BUSY is the one failure of this write that may become an answer,
		// and the reason is not that it is the likeliest: it is a state of
		// the INSTALLATION -- another program holds the file, measured at up
		// to 80,6 s in point 48 of docs/vertraege-v1.2.md -- and never a
		// state of the row that was handed in. A locked file is locked for
		// both names alike, so it cannot fall one way for a name that exists
		// and the other way for one that does not, and an answer built on it
		// tells nobody which name was asked for.
		//
		// Every other failure stays swallowed, and that is deliberate rather
		// than left over. A full disk, a read replica, a quota, a store that
		// dislikes THIS row: all of those can depend on what was written, and
		// what is written differs between the two names by exactly one value
		// -- the ID. A store that takes a real ID and refuses decoy, or the
		// other way round, would hand the oracle straight back, inverted.
		// Busy is the only name in that error we may read, because it is the
		// only one that cannot be about the row.
		//
		// E_UNREACHABLE and no word of its own: there is none to use. A code
		// E_BUSY lies in frozen shared/wire/wire.go:450-462 -- BLOCKED, and
		// P9 of docs/vertraege-v1-3-vorschlag.md is the decision. So what the
		// caller gets is the 503 of "the database cannot be reached" with the
		// Retry-After beside it that point 49 built, which is enough for a
		// client that reads headers and for an admin reading the log, and not
		// yet enough for one that reads only the code. It is still better
		// than the answer before it, which was a lie about a password.
		if dbwait.Busy(err) {
			// httpapi.FailFault and not an answer of this file's own: it is
			// the one place that puts Retry-After and the reason db-busy
			// together, and the log line it writes carries the reason and the
			// number only, nothing of the request (point 49).
			httpapi.FailFault(w, err, httpapi.FromRows)
			return
		}
		// Swallowed, but not silent. Every login of the installation then
		// fails with E_LOGIN and nothing outside says why, so it is said out
		// loud -- once per nonceWarnTTL and without a word of the request, so
		// that it costs no answer and cannot be made to fill a disk by asking
		// often.
		//
		// The line is the fixed text and nothing else, the error of the
		// database included. A driver may quote the values of the statement
		// back in its error, and the values of this one are the nonce and the
		// user ID: a nonce belongs in no log, not even shortened (CLAUDE.md,
		// "Regeln fuer sicheres Programmieren"). What the line has to say --
		// the database cannot be written -- it says without them, and whoever
		// needs the cause gets it from the database itself.
		if h.warnNonce.take(now, nonceWarnTTL) {
			slog.Error("a login challenge could not be kept; logins fail until the database can be written again")
		}
	}
	if b := mustJSON(w, c); b != nil {
		httpapi.Answer(w, http.StatusOK, b)
	}
}
