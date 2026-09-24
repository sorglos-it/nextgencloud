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
	Token    []byte           // session.TokenHash of the cookie value
	Row      dbapi.Session    // as the database holds it, with Seen set to now
	Trust    trust.Version    // the trust list this session was decided on
	Verified session.Verified // the chain back to the user's key
	APIKey   []byte           // the API's private session key, opened
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
// The code is that same rule said to the caller. E_LOGIN is the contract's
// word for a session that is missing or has run out (shared/README.md), so
// only a no carries it. A question that could not be asked is E_UNREACHABLE
// where the store did not answer -- the session row, the trust list, the touch
// -- and E_INTERNAL where this API's own row or its own key is unusable: the
// first is over once the database answers again, the second is not. Without
// that difference a hiccup of the database would send every user of the
// installation to the login screen, and refuse the logout that would end the
// session properly in the same breath.
//
// Nothing of the session itself is told apart that way. Every cookie that is
// no session of this API gets the same E_LOGIN, whether a row was there or
// not: dbapi.ErrNotFound is the store's answer that there is none, and it is
// the only one of its answers that leads there.
//
// A delete that fails is not said out loud, and nothing rests on it: a row
// Live refuses once it refuses on every later request too, so there is no
// protection here that could be switched off quietly.
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
// token, and the length below nothing but exactly a token. So the question the
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
	value, ok := httpapi.Cookie(r)
	if !ok {
		return nil, wire.CodeLogin, false
	}
	raw, err := enc.B64.DecodeString(value)
	if err != nil || len(raw) != session.TokenSize {
		return nil, wire.CodeLogin, false
	}
	// Only the hash of the token is stored, so a dump of the database holds
	// nothing anybody could send back as a cookie.
	token := session.TokenHash(raw)
	row, err := h.S.DB.SessionGet(ctx, token)
	if err != nil {
		if isNotFound(err) {
			return nil, wire.CodeLogin, false
		}
		return nil, wire.CodeUnreachable, false
	}
	now := h.S.Now()
	// The three ends of a session: the time the certificate itself names, the
	// idle time and the whole lifetime. The last is not the second reading of
	// the first -- login/finish takes no certificate reaching further than the
	// lifetime configured then, and an installation that has shortened it
	// since ends the sessions it handed out under the longer one here.
	if now >= row.Expires || now-row.Seen > h.S.Cfg.IdleTimeout || now-row.Created > h.S.Cfg.SessionLifetime {
		_ = h.S.DB.SessionDelete(ctx, token)
		return nil, wire.CodeLogin, false
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
		// One code for every cause of it, and it is never E_LOGIN. The store
		// not answering is the plain case; rows of the trust list that
		// contradict each other end up here too, and both are mended from
		// outside this request, so both leave the caller the same thing to do.
		return nil, wire.CodeUnreachable, false
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
// cookie deleted. Without a session it is E_LOGIN, which is what the contract's
// table gives -- and it is also the answer when Live ended the session itself,
// because then there is none left to end. A step Live could not take carries
// its own code instead: the session is still there, and E_LOGIN would tell the
// shell the one thing about it that is not so.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	live, code, ok := h.Live(ctx, r)
	if !ok {
		httpapi.Fail(w, code)
		return
	}
	if err := h.S.DB.SessionDelete(ctx, live.Token); err != nil {
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
