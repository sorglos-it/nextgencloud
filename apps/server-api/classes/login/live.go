package login

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
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

// sessionFault is the code for an error of one of the three calls that read,
// touch or remove the session row. All three ask about that one row, so the
// store's own words mean here what the contract says they mean about a
// session, and not what the same word means about any other row.
//
// dbapi.ErrNotFound is a session that is not there, and the contract gives
// E_LOGIN for a session that is missing or has run out (shared/README.md). It
// is that answer however the row went: never written at all, or taken away
// while this request ran -- a logout of the same cookie on another node, or
// the housekeeping round past its expiry (Purge, dbapi.SessionPurge). A
// session that ran out and still has its row is refused by sessionRow's own
// check of the three ends, with this same word.
//
// httpapi.Fault reads the same dbapi.ErrNotFound as
// E_INTERNAL and is right to: it is handed an error and not a question, so it
// cannot know that a missing row was the answer here, and its own comment says
// the handler has to say so before it asks.
//
// The store's other two words are answers as well, and no waiting turns either
// into a row: the call was wrong (ErrInvalid), the value was taken already
// (ErrUsed). Neither is a session that is missing, so neither may carry
// E_LOGIN; neither is a database that is away, so neither may carry
// E_UNREACHABLE. That is the reading httpapi.Fault gives dbapi's whole set,
// and this is that same reading rather than a second one beside it.
//
// Everything left never reached the store or was never answered by it, and it
// is over when the store answers again: E_UNREACHABLE.
func sessionFault(err error) wire.Code {
	switch {
	case errors.Is(err, dbapi.ErrNotFound):
		return wire.CodeLogin
	case errors.Is(err, dbapi.ErrInvalid), errors.Is(err, dbapi.ErrUsed):
		return wire.CodeInternal
	}
	return wire.CodeUnreachable
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
// dbapi.ErrNotFound is the store's word for a row that is not there, and
// sessionFault keeps it the only one of the store's answers that leads to
// E_LOGIN at all.
func (h *Handler) sessionRow(ctx context.Context, r *http.Request, now int64) ([]byte, dbapi.Session, wire.Code, error, bool) {
	value, ok := httpapi.Cookie(r)
	if !ok {
		return nil, dbapi.Session{}, wire.CodeLogin, nil, false
	}
	raw, err := enc.B64.DecodeString(value)
	if err != nil || len(raw) != session.TokenSize {
		return nil, dbapi.Session{}, wire.CodeLogin, nil, false
	}
	// Only the hash of the token is stored, so a dump of the database holds
	// nothing anybody could send back as a cookie.
	token := session.TokenHash(raw)
	row, err := h.S.DB.SessionGet(ctx, token)
	if err != nil {
		// The error goes up beside the code and is not dropped here: busy and
		// away are one code (E_UNREACHABLE, no word of its own -- P9), and
		// only the error tells them apart. This is the read of the
		// installation that every session path makes.
		return nil, dbapi.Session{}, sessionFault(err), err, false
	}
	// The three ends of a session: the time the certificate itself names, the
	// idle time and the whole lifetime. The last is not the second reading of
	// the first -- login/finish takes no certificate reaching further than the
	// lifetime configured then, and an installation that has shortened it
	// since ends the sessions it handed out under the longer one here.
	if now >= row.Expires || now-row.Seen > h.S.Cfg.IdleTimeout || now-row.Created > h.S.Cfg.SessionLifetime {
		_ = h.S.DB.SessionDelete(ctx, token)
		return nil, dbapi.Session{}, wire.CodeLogin, nil, false
	}
	return token, row, "", nil, true
}

// Live returns the session of the request's cookie, and with it the code the
// caller answers when there is none -- and, beside the code, the error of the
// store where one of the store's own failures stood behind it.
//
// The error is there because the code alone cannot be told apart: a database
// another program holds and one that is gone are both E_UNREACHABLE, since a
// word of its own for "busy" lies in frozen shared/wire (P9 of
// docs/vertraege-v1-3-vorschlag.md). Until this it was dropped here, so both
// callers answered httpapi.Fail and neither could set a Retry-After -- the
// open post of point 52. It is nil wherever no store failure stood behind the
// code, and it is never the thing that picks the code: the callers hand both
// to httpapi.FailCoded, which answers the code and reads the error for the
// header and the log line only. Nothing of it goes to the caller of the route;
// an error on this path can carry a path or a configuration.
//
// It ends a session that is too old, has been idle too long or whose user the
// trust list no longer holds, and it hands nothing back for any other failure
// either.
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
// The store saying the row is not there is on the other side of that line, in
// both steps that ask about it: it is an answer and it is the very thing the
// contract gives E_LOGIN for, so it carries it (sessionFault). The row is
// then gone and nothing here threw it away.
//
// Four of the steps get their code from this file, because the error is the
// store's own or this API's own and only the step knows what it was asking
// for. Reading the session row and touching the session both ask about that
// one row, so both go through sessionFault, which says what each of the
// store's own three words means about a session; only a store that did not
// answer at all is E_UNREACHABLE there. A stored proof this API cannot read
// back and a session key it cannot open are E_INTERNAL -- the database
// against this API's own row and its own key. What the fifth, the trust list,
// answers is picked by httpapi.Fault instead, which is the one place that
// reads an error of package state, so that the same damage does not answer
// one thing here and another where the same list is read out
// (login/trust.go). One thing of that fifth
// step is said here before Fault is asked, because only this step knows it: a
// list that is not there at all means an installation before its setup
// everywhere else, and here it cannot, because a session got this far. The
// line that turns it into state.ErrSetting says the rest.
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
// cache (state.Trust) would have cost nothing: two questions of the trust
// rows per request, both of which come back empty once nothing new has
// landed, and state.Refresh holds the write mutex of its package over all of
// them, so the requests of one API wait for one another instead of
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
func (h *Handler) Live(ctx context.Context, r *http.Request) (*Live, wire.Code, error, bool) {
	now := h.S.Now()
	token, row, code, err, ok := h.sessionRow(ctx, r, now)
	if !ok {
		return nil, code, err, false
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
		return nil, wire.CodeInternal, nil, false
	}
	// The list is read out of the database and not out of the cache
	// (state.Trust), because the cache of an API is only as new as the last
	// refresh on that node: the node where an admin withdraws a key has the
	// new list at once, every other node has the old one until something
	// refreshes it. Live is the step that decides, so it is the step that has
	// to ask.
	//
	// What it asks for is what lies above the cached version, never the
	// cached version itself. So a node that holds one decides on it even
	// where the rows below are gone: with every trust row of the
	// installation dropped, state.Refresh here hands back the cached version
	// and no error at all -- measured, version 3 and err nil.
	//
	// What answers 500 for GET session.PathTrust is the step after this one,
	// state.Chain -- but only where the request asks for a version whose row
	// is gone. The query the shell really sends asks for none of them: it asks
	// from the number after the last version it holds, and that one Chain
	// answers out of the version alone (204, nothing newer); a from asking
	// further ahead than that is answered the same way, out of the version
	// alone and no row read for it either (404, state.ErrOldVersion). So the
	// ordinary poll of a shell that is up to date comes back 204 on a node
	// whose list the store has lost, measured, and a caller of Live that never
	// goes on to Chain at all -- the WebSocket above -- decides the session on
	// that same list without an error either.
	//
	// Both are one gap and not two, and it is the gap
	// TestKnownGap_AWarmNodeDecidesOnAListTheStoreLost fences. Making Chain go
	// and look would cost the newest row read back on that poll, which is the
	// most common request of the installation and a row that may be megabytes
	// (state.readBlock), and it would still catch only a store that lost the
	// row: a writer who leaves one standing gets past it either way
	// (state.checkGap names two such forms, both fenced in knowngap_test.go).
	// What settles it is an anchor outside the database, which is on the V1.2
	// list and which knowngap_test.go names by its point. Nothing here pays
	// that read, and nothing here pretends to.
	// TestTheTwoNodesPartWhereTheAnswerNeedsNoRow holds the 204 half of it
	// under damage, and TestTrustLooksAtTheListOnce counts it on the ordinary
	// path. The far side, a from asking further ahead still, rests on being
	// the same shape in state.Chain alone and carries no count of its own.
	v, err := h.S.Refresh(ctx)
	if errors.Is(err, state.ErrNoTrust) {
		// Here that error cannot mean what it means everywhere else. An
		// installation with no list yet has handed out no session either --
		// login/finish asks for the list before it writes the row (state.Trust)
		// -- and a row of the request's cookie is what got this far. So a list
		// that is not there now is one the rows lost, which is the word
		// ErrSetting carries.
		//
		// The old error is said in words and not wrapped with %w on purpose:
		// httpapi.Fault asks for state.ErrNoTrust before it asks for
		// state.ErrSetting, so one that still carried it would come out
		// E_NOTFOUND all the same. Measured -- it did.
		//
		// It matters to the shell and not only to the log. E_NOTFOUND is what
		// the contract's table gives for a from above the newest version: "you
		// are ahead, there is nothing newer". A shell told that goes on working
		// from the list it holds, while the installation has lost its own --
		// the same rule this package writes down at state.ErrBadVersion, that a
		// fault of the installation must never read like an ordinary answer.
		//
		// It is also what makes the two nodes agree wherever the answer needs
		// a row the damage took away. One that still holds a list finds the
		// row gone in state.Chain (E_INTERNAL); one with an empty cache finds
		// no list at all, and without this line it answered E_NOTFOUND, the
		// one thing the other never says.
		//
		// Where the answer needs no row they part all the same, and that is
		// not this line's doing: the note at the Refresh call above says what
		// the ordinary poll gets on each of them, and why it is left that way.
		err = fmt.Errorf("%w: the installation has a session and no trust list", state.ErrSetting)
	}
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
		return nil, httpapi.Fault(err, httpapi.FromRows), err, false
	}
	verified, err := session.VerifyClient(row.Cert, p, v.List, now)
	if err != nil {
		_ = h.S.DB.SessionDelete(ctx, token)
		return nil, wire.CodeLogin, nil, false
	}
	key, err := h.S.Box.Open(row.APIKey)
	if err != nil {
		return nil, wire.CodeInternal, nil, false
	}
	// Seen moves only once everything above has held, so a request that is
	// refused never keeps a session alive. TestARefusedRequestDoesNotMoveSeen
	// holds this step under the one above it, the only one of the four that
	// leaves the row where it is and sits directly above the touch.
	//
	// The row can be gone by the time this write lands -- a logout of the same
	// cookie on another node, or the housekeeping round, between the read
	// above and here -- and then the session is over rather than the store
	// away, which is what sessionFault answers for the store's own word.
	if err := h.S.DB.SessionTouch(ctx, token, now); err != nil {
		return nil, sessionFault(err), err, false
	}
	row.Seen = now
	return &Live{Token: token, Row: row, Trust: v, Verified: verified, APIKey: key}, "", nil, true
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
// there is none left to end. Two steps can fail past that, and both go through
// sessionFault: a store that will not say whether there is a row leaves
// nothing to end, and one that will not remove the row leaves the session
// open, so a store that does not answer at all is E_UNREACHABLE either way --
// E_LOGIN would tell the shell, for either of them, the one thing about the
// session that is not so. The one failure of the second step that is about
// the session is the row being gone already, a second logout of the same
// cookie or the housekeeping round between the read and the delete, and there
// E_LOGIN is right by the same table: there is nothing left to end, which is
// what this path answers for a session that had run out anyway. A store that
// calls the call itself wrong (dbapi.ErrInvalid, dbapi.ErrUsed) is E_INTERNAL
// instead, the same as sessionFault gives for every other of the three
// calls on this one row -- measured for the read, which answers 500
// through the logout just as it does through Live
// (TestTheStoresOwnWordAboutASessionIsNotADatabaseThatIsAway).
//
// TestLogoutEndsASessionTheRestOfLiveCannotRead holds both steps.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token, _, code, err, ok := h.sessionRow(ctx, r, h.S.Now())
	if !ok {
		// FailCoded and not Fail: a read that only did not come free in time
		// gets the Retry-After and the log reason that tell it from a database
		// that is away (httpapi/busy.go). The code is sessionFault's and not
		// recomputed, so a session that is simply not there stays E_LOGIN.
		httpapi.FailCoded(w, code, err)
		return
	}
	if err := h.S.DB.SessionDelete(ctx, token); err != nil {
		// The cookie stays with the session either way: beside a row that is
		// still there, clearing it would leave a session open that nobody can
		// reach to end; beside a row already gone, it names nothing left to
		// end, and sessionRow refuses it again on the next request regardless.
		httpapi.FailCoded(w, sessionFault(err), err)
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
