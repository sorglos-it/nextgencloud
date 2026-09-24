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
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Live is a session that is open right now.
type Live struct {
	Token    []byte           // session.TokenHash of the cookie value
	Row      dbapi.Session    // as the database holds it, with Seen set to now
	Verified session.Verified // the chain back to the user's key
	APIKey   []byte           // the API's private session key, opened
}

// Live returns the session of the request's cookie. It ends a session that is
// too old, has been idle too long or whose user the trust list no longer
// holds, and it hands nothing back for any other failure either.
//
// Which failures end the session and which leave the row where it is, is the
// difference between the session being wrong and this installation being
// unable to answer. A time that has passed and a chain that no longer holds
// the user belong to the session; a database that cannot be reached, a proof
// this API itself wrote and cannot read back, and a session key it cannot open
// belong to the installation -- and a session thrown away for one of those
// does not come back when the cause is mended. The wrong secret key in a
// configuration would otherwise cost every session of the installation the
// first time anybody asked, where a restart with the right one costs none.
//
// Either way no session is handed back, so nothing rests on that choice but
// whether the row survives. Nothing says any of it out loud: a delete that
// fails leaves a row Live refuses on every later request just the same, so
// there is no protection here that could be switched off quietly.
//
// The chain is checked again on every request and not only at the login: a
// user the trust list no longer holds, or whose key an admin took away, loses
// the session with the next request instead of at the end of the day. The
// price is chosen and not overlooked -- that is the user's proof verified once
// per request, the same work the login did, with no cache in front of it.
//
// No path here carries a login name, and the only string the caller picks is
// the cookie: httpapi.Cookie takes nothing but base64url no longer than a
// token, and the length below nothing but exactly a token. So the question the
// login paths have to keep quiet -- does this name exist -- is not asked here
// at all, and what a request costs follows only whether a token this API
// handed out came back, which nobody can pick.
func (h *Handler) Live(ctx context.Context, r *http.Request) (*Live, bool) {
	value, ok := httpapi.Cookie(r)
	if !ok {
		return nil, false
	}
	raw, err := enc.B64.DecodeString(value)
	if err != nil || len(raw) != session.TokenSize {
		return nil, false
	}
	// Only the hash of the token is stored, so a dump of the database holds
	// nothing anybody could send back as a cookie.
	token := session.TokenHash(raw)
	row, err := h.S.DB.SessionGet(ctx, token)
	if err != nil {
		return nil, false
	}
	now := h.S.Now()
	// The three ends of a session: the time the certificate itself names, the
	// idle time and the whole lifetime. The last is not the second reading of
	// the first -- login/finish takes no certificate reaching further than the
	// lifetime configured then, and an installation that has shortened it
	// since ends the sessions it handed out under the longer one here.
	if now >= row.Expires || now-row.Seen > h.S.Cfg.IdleTimeout || now-row.Created > h.S.Cfg.SessionLifetime {
		_ = h.S.DB.SessionDelete(ctx, token)
		return nil, false
	}
	v, err := h.S.Trust(ctx)
	if err != nil {
		return nil, false
	}
	// The proof is this API's own row, written with this same encoder from a
	// proof session.ParseLoginFinish had already bounded, so nothing unbounded
	// is read back here. Validate all the same: the row is what the check
	// below is given, and a row that has been damaged must not reach it.
	var p proof.Proof
	if err := json.Unmarshal(row.Proof, &p); err != nil || p.Validate() != nil {
		return nil, false
	}
	verified, err := session.VerifyClient(row.Cert, p, v.List, now)
	if err != nil {
		_ = h.S.DB.SessionDelete(ctx, token)
		return nil, false
	}
	key, err := h.S.Box.Open(row.APIKey)
	if err != nil {
		return nil, false
	}
	// Seen moves only once everything above has held, so a request that is
	// refused never keeps a session alive.
	if err := h.S.DB.SessionTouch(ctx, token, now); err != nil {
		return nil, false
	}
	row.Seen = now
	return &Live{Token: token, Row: row, Verified: verified, APIKey: key}, true
}

// Logout answers POST session.PathLogout: the session row is removed and the
// cookie deleted. Without a session it is E_LOGIN, which is what the contract's
// table gives -- and it is also the answer when Live ended the session itself,
// because then there is none left to end.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	live, ok := h.Live(ctx, r)
	if !ok {
		httpapi.Fail(w, wire.CodeLogin)
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
// of failed logins. It is meant to run every minute at one API of the
// installation; nothing in this package wires it up.
//
// Nothing hangs on it running, which is what makes the times below free to
// choose at all. A session past one of its ends is refused by Live whether or
// not anything ever removed it, and a row of the counting past its keep time
// reads as none before it is read (classes/lock, read). What Purge frees is
// the space those rows take.
//
// Expired one-time values are freed only session.MaxSkew after their time.
// They are the one kind here that another API still has to find: a nonce or a
// task ID is taken exactly once, and an API whose clock runs behind accepts it
// until its own clock reaches the expiry, so a faster API's walk must not have
// freed it by then (dbapi.Local.OncePurge).
//
// Sessions and the counting take the clock as it is, and what that costs is
// the same room, not taken. A session freed early is one every API refuses
// anyway: each reads the end out of the row itself. A row of the counting
// freed early puts a bucket back to zero up to that much before its keep time
// would have, which is the fall to zero that keep time makes a moment later --
// as long as no wait the counting lays reaches that far. Nothing here holds
// those two to each other; whoever moves maxWait or keepFor in classes/lock
// looks at this line.
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
