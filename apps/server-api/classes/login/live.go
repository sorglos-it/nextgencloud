package login

import (
	"context"
	json "encoding/json/v2"
	"net/http"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Live is a session that is open right now.
type Live struct {
	Token []byte        // session.TokenHash of the cookie value
	Row   dbapi.Session // as the database holds it, with Seen set to now
	// Trust is the trust list this session was decided on. Its List is the
	// cached one of package state, and state.Trust and state.Refresh say the
	// same thing about it at both ends it comes from: read it, never change
	// it. Nothing on the way here copies it, so this field carries that rule
	// into every handler -- a write through it would reach the list every
	// other request of this process is holding at that moment, and no test of
	// this package would see it happen.
	Trust    trust.Version
	Verified session.Verified // the chain back to the user's key
	APIKey   []byte           // the API's private session key, opened
}

// sessionRow is the row of the request's cookie, and with it the code the
// caller answers when there is none. It is the first steps of Live, and
// everything the logout asks before it deletes.
//
// The clock comes in rather than being read here, so that one request asks it
// once: the three ends below, the certificate session.VerifyClient proves and
// the time Live writes back are then the same moment, not three readings of a
// clock that may cross a second in between.
//
// Nothing about the session itself is told apart. A cookie no row answers and
// a cookie no parser takes get the same E_LOGIN, so the one thing a stranger
// could ask -- is this token one of yours -- stays unanswered:
// dbapi.ErrNotFound is the store's word for a row that is not there, and it is
// the only one of the store's answers that leads to E_LOGIN at all.
func (h *Handler) sessionRow(ctx context.Context, r *http.Request, now int64) ([]byte, dbapi.Session, wire.Code, bool) {
	value, ok := httpapi.Cookie(r)
	if !ok {
		return nil, dbapi.Session{}, wire.CodeLogin, false
	}
	raw, err := enc.B64.DecodeString(value)
	if err != nil || len(raw) != session.TokenSize {
		return nil, dbapi.Session{}, wire.CodeLogin, false
	}
	// Only the hash of the token is stored, so a dump of the database holds
	// nothing anybody could send back as a cookie.
	token := session.TokenHash(raw)
	row, err := h.S.DB.SessionGet(ctx, token)
	if err != nil {
		if isNotFound(err) {
			return nil, dbapi.Session{}, wire.CodeLogin, false
		}
		return nil, dbapi.Session{}, wire.CodeUnreachable, false
	}
	// The three ends of a session: the time the certificate itself names, the
	// idle time and the whole lifetime. The last is not the second reading of
	// the first -- login/finish takes no certificate reaching further than the
	// lifetime configured then, and an installation that has shortened it
	// since ends the sessions it handed out under the longer one here.
	if now >= row.Expires || now-row.Seen > h.S.Cfg.IdleTimeout || now-row.Created > h.S.Cfg.SessionLifetime {
		_ = h.S.DB.SessionDelete(ctx, token)
		return nil, dbapi.Session{}, wire.CodeLogin, false
	}
	return token, row, "", true
}

// Live returns the session of the request's cookie, and with it the code the
// caller answers when there is none. It ends a session that is too old, has
// been idle too long or whose user the trust list no longer holds, and it
// hands nothing back for any other failure either.
//
// Which failures end the session and which leave the row where it is, is one
// rule: a no ends it, a question that could not be asked does not. The clock
// and the configuration say no, and so does session.VerifyClient -- whether
// because an admin took the user's key away or because the row itself no
// longer reads as a certificate of this installation. A database that cannot
// be reached, a trust list that cannot be read, a proof this API wrote and
// cannot read back, and a session key that cannot be opened are not answers at
// all, and a session thrown away for one of them does not come back when the
// cause is mended: the wrong secret key in a configuration would cost every
// session of the installation the first time anybody asked, where a restart
// with the right one costs none.
//
// The code is that same rule said to the caller, and a question that could
// not be asked never carries E_LOGIN: the contract keeps that word for a
// session that is missing or has run out (shared/README.md), and this one is
// neither. Without that difference a hiccup of the database would send every
// user of the installation to the login screen.
//
// Four of the steps get their code from this file, because the error is the
// store's own or this API's own and only the step knows what it was asking
// for: reading the session row and touching the session are E_UNREACHABLE, a
// stored proof this API cannot read back and a session key it cannot open are
// E_INTERNAL -- the database against this API's own row and its own key. What
// the fifth, the trust list, answers is picked by
// httpapi.Fault instead, which is the one place that reads an error of package
// state, so that the same damage does not answer one thing here and another
// where the same list is read out (login/trust.go). That also brings in a code
// no failure of this step carried before: an installation with no trust list
// at all is E_NOTFOUND, the contract's word for a thing that is not there.
//
// Nothing about the session itself is told apart, and the cookie and the row
// are read by sessionRow, which says why.
//
// A delete that fails is not said out loud, and nothing rests on it: nothing
// here is kept out by the row being gone. Every later request reads the row
// again and asks the clock, the configuration and the list afresh, so a row
// that stayed behind is refused again for as long as its reason stands -- and
// taken again once the reason is gone, which an admin who puts a withdrawn key
// back can do.
//
// The chain is checked again on every request and not only at the login: a
// user the trust list no longer holds, or whose key an admin took away, loses
// the session with the next request instead of at the end of the day.
//
// Two prices are chosen for that, not overlooked. One is the user's proof
// verified once per request, the same public-key work the login paid for;
// nothing here caches that answer. The other is the list it is checked
// against, read out of the database on every call (state.Refresh) where the
// cache (state.Trust) would have cost nothing: a handful of reads of the trust
// rows per request, and state.Refresh holds the write mutex of its package
// over all of them, so the requests of one API wait for one another instead of
// running side by side. GET session.PathTrust pays that once and not twice --
// state.Chain is handed the version this step brought up to date (Live.Trust)
// rather than fetching one of its own. Whoever builds the WebSocket calls Live
// when the connection opens, not once per message.
//
// Reading it is also a question that can now fail on every request, where the
// cache could only fail until it was filled once. A failed read leaves the
// session where it is, by the rule above.
//
// No path here carries a login name, and the only string the caller picks is
// the cookie: httpapi.Cookie takes nothing but base64url no longer than a
// token, and sessionRow nothing but exactly a token. So the question the
// login paths have to keep quiet -- does this name exist -- is not asked here
// at all. For anybody who holds no token, what a request costs follows one
// thing: whether a token this API itself drew came back, and session.TokenSize
// random bytes are not something anybody picks.
//
// Past that, the work is not the same for everybody. session.VerifyClient
// walks the users of the list and their keys until it reaches the one the
// proof names and stops there (trust.List.KeyOwner), asks the roles of that
// user when the key is a password key (PasswordToo), and proves the
// certificate with a single ed25519.Verify for a password key where a passkey
// is a whole WebAuthn assertion (proof.Verify). Every user and every key is
// what that walk costs at worst, for a key nobody holds, and the stop is why
// the rest is not the same for everybody: what an answered request costs
// follows whose session it is, and most of all which kind of key opened it.
// Whoever could time that holds the cookie already and no name stands behind
// it -- but the difference is real, and nothing here evens it out.
func (h *Handler) Live(ctx context.Context, r *http.Request) (*Live, wire.Code, bool) {
	now := h.S.Now()
	token, row, code, ok := h.sessionRow(ctx, r, now)
	if !ok {
		return nil, code, false
	}
	// The proof is this API's own row, written with this same encoder from a
	// proof session.ParseLoginFinish had already bounded, so nothing unbounded
	// is read back here. Validate all the same: the row is what the check
	// below is given, and a row that has been damaged must not reach it.
	//
	// It comes before the list because it is the cheaper of the two and it
	// needs nothing from it: session.VerifyClient is the first step that wants
	// both, so a row that never gets that far costs no look at the trust rows.
	var p proof.Proof
	if err := json.Unmarshal(row.Proof, &p); err != nil || p.Validate() != nil {
		return nil, wire.CodeInternal, false
	}
	// The list is read out of the database and not out of the cache
	// (state.Trust), because the cache of an API is only as new as the last
	// refresh on that node: the node where an admin withdraws a key has the
	// new list at once, every other node has the old one until something
	// refreshes it. Live is the step that decides, so it is the step that has
	// to ask.
	v, err := h.S.Refresh(ctx)
	if err != nil {
		// httpapi.Fault picks it, the same one place that picks the code when
		// this very list is read out for the answer (login/trust.go). The two
		// steps meet one damage at different times -- a node with nothing
		// cached finds a missing version here, one that is up to date finds it
		// down there -- and going through one place is what keeps the answer
		// from following which node was asked. FromRows, because the version
		// came out of this installation's own rows and the caller brought
		// nothing to it.
		//
		// None of what it can answer here is E_LOGIN, which is the whole point
		// of reading it out of the error at all.
		return nil, httpapi.Fault(err, httpapi.FromRows), false
	}
	verified, err := session.VerifyClient(row.Cert, p, v.List, now)
	if err != nil {
		_ = h.S.DB.SessionDelete(ctx, token)
		return nil, wire.CodeLogin, false
	}
	key, err := h.S.Box.Open(row.APIKey)
	if err != nil {
		return nil, wire.CodeInternal, false
	}
	// Seen moves only once everything above has held, so a request that is
	// refused never keeps a session alive.
	if err := h.S.DB.SessionTouch(ctx, token, now); err != nil {
		return nil, wire.CodeUnreachable, false
	}
	row.Seen = now
	return &Live{Token: token, Row: row, Trust: v, Verified: verified, APIKey: key}, "", true
}

// Logout answers POST session.PathLogout: the session row is removed and the
// cookie deleted.
//
// It takes sessionRow and not Live, and that is the whole difference between
// the two paths of this package. Removing the row of one's own cookie wants no
// trust list and no proof that still reads back, so neither may keep a session
// alive that its owner is ending: what Live checks past that point decides who
// is told things, and none of it decides who may stop being told them. Before
// that split a damaged proof (E_INTERNAL) or a store that would not hand out
// the trust rows (E_UNREACHABLE) left the session standing until its own
// expiry, with nothing its owner could do about it.
//
// Nobody past the row is asked about either, and nothing is given up by that.
// The cookie is the session: whoever holds it either opens the session with it
// or, where the list no longer holds its user, has it ended for him at the
// next request anyway (Live). Closing it himself takes nothing away.
//
// Without a session it is E_LOGIN, which is what the contract's table gives --
// and it is also the answer when the session had run out already, because then
// there is none left to end. Two steps are left that can fail without the
// session being the reason, and both carry E_UNREACHABLE: a store that will
// not say whether there is a row leaves nothing to end, and one that will not
// remove the row leaves the session open. E_LOGIN would tell the shell, for
// either of them, the one thing about the session that is not so.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token, _, code, ok := h.sessionRow(ctx, r, h.S.Now())
	if !ok {
		httpapi.Fail(w, code)
		return
	}
	if err := h.S.DB.SessionDelete(ctx, token); err != nil {
		// The cookie stays with the session: deleting it beside a row that is
		// still there would leave a session open that nobody can reach to end.
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	httpapi.ClearCookie(w)
	httpapi.NoContent(w)
}

// Purge is the housekeeping round: sessions, one-time values and the counting
// of failed logins. It runs at one API of the installation, as often as
// whoever wires it up likes; nothing in this package wires it up, and nothing
// hangs on the cadence.
//
// Nothing hangs on it running at all. All three refuse what is past its time
// without it: Live reads the end of a session out of the row,
// dbapi.Local.OnceUse answers an expired value ErrNotFound, and a row of the
// counting past its keep time reads as none before anything of it is read
// (classes/lock, read). What Purge frees is the space those rows take.
//
// That does not leave the times below free to choose. One of the three has to
// be held back, and a test holds it there: hand OncePurge the plain clock and
// TestPurgeFreesOnlyWhatIsPastItsTime goes red.
//
// Expired one-time values are freed only session.MaxSkew after their time.
// They are the one kind here another API still has to find: a nonce or a task
// ID is taken exactly once, and an API whose clock runs behind accepts it
// until its own clock reaches the expiry, so a faster API's walk must not have
// freed it by then (dbapi.Local.OncePurge).
//
// Sessions and the counting take the clock as it is, and the price is named.
// A session freed by a fast clock is gone for an API whose clock runs behind
// as well, which logs its user out up to that much early -- the error on the
// safe side, and the only one of the two: nothing is let in that would have
// been kept out. For the counting the same early walk puts a bucket back to
// zero before its keep time would have, which is the fall to zero the keep
// time makes a moment later -- and it cannot drop a wait that is still
// running, because a row is freed only once it is past its keep time and no
// wait the counting lays runs that long. That holds while classes/lock's
// keepFor stays at least its maxWait plus session.MaxSkew. Nothing holds those
// three to each other, so whoever moves one of them reads this line.
func (h *Handler) Purge(ctx context.Context) error {
	now := h.S.Now()
	if err := h.S.DB.SessionPurge(ctx, now); err != nil {
		return err
	}
	if err := h.S.DB.OncePurge(ctx, now-session.MaxSkew); err != nil {
		return err
	}
	return lock.Purge(ctx, h.S.DB, now)
}
