package login

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	json "encoding/json/v2"
	"errors"
	"log/slog"
	"net/http"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// lockWarnTTL is how seldom a failed login that could not be counted is said
// out loud. The line stands for a state of the database and not for a request,
// so one of them per five minutes says everything there is to say -- and every
// failed login would say it, which is a line per wrong password.
const lockWarnTTL = 300

// blindKey and blindSig are what a proof by a key nobody holds is measured
// against. session.VerifyClient turns such a proof away with session.ErrUser
// before it ever checks a signature, and for many proofs a key the list holds
// pays a whole verification there instead -- for many and not for all, which is
// the whole difficulty and is written out at blindProof. And login/start hands
// the key ID of a passkey out, real for a name that exists and made up for one
// nobody has (session.PasskeyRef), so the difference is the question "does this
// name exist" once more, asked with a clock: measured at 108 µs against 40 µs
// on one machine before this was here.
//
// The key is this package's own, from a fixed seed that is worth nothing --
// nothing of NGC is signed with it, and the answer of the verification is
// thrown away. blindSig really is a signature, so the verification runs to its
// end instead of stopping at one that is malformed.
var blindKey, blindSig = blindPair()

func blindPair() (ed25519.PublicKey, []byte) {
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x5B}, ed25519.SeedSize))
	return priv.Public().(ed25519.PublicKey), ed25519.Sign(priv, nil)
}

// blindProof does one signature verification over the bytes the real one would
// have covered. It gives the answer back so that the call is not work a
// compiler may drop; it is never true, and no caller reads it.
//
// It is one Ed25519 verification whatever the proof says it is, and that is
// where it stops being an exact mirror. package proof refuses some proofs
// before any cryptography, and with a key the list holds those then cost
// nothing at all where this pays in full. Three such shapes are known and
// measured:
//
//   - a passkey whose client data is nonsense
//   - a proof of a kind the key is not
//   - a passkey whose authenticator data names another relying party
//
// All three are this function's own doing: before it was here the two sides
// were alike there, both cheap. Closing them needs more than a line and belongs
// with the question login/start leaves open, so
// TestFinishTakesTheSameTimeForAKeyNobodyHolds carries them as the shapes it
// does not hold to the factor and counts them in openShapes -- three today, and
// a number rather than a sentence because a sentence said "one" twice while it
// was two and then three.
func blindProof(payload []byte) bool {
	return ed25519.Verify(blindKey, sign.Message(sign.PurposeClientCert, payload), blindSig)
}

// errSignKey reports a signing key of the wrong size in the configuration;
// config.API.Validate refuses such a file, so it can only mean the key was
// swapped underneath us.
var errSignKey = errors.New("login: signing key of the wrong size")

// Finish answers POST session.PathLoginFinish. Here the checks of a login come
// together, in this order: the proof over the client certificate against the
// trust list, the end that certificate names against this API's own session
// lifetime, the password of a role that asks for it, the TOTP code of a user
// who holds a secret, and the nonce, which is spent exactly once. Only then are
// the session, the API certificate and the cookie made.
//
// Every one of those checks answers E_LOGIN with the same body, whatever was
// wrong (shared/README.md, "Vor dem WebSocket"): an unknown name, a proof by a
// key nobody holds, a missing password, a wrong code, a nonce that was never
// handed out and one that was spent are all one answer.
func (h *Handler) Finish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, ok := httpapi.Body(w, r)
	if !ok {
		return
	}
	f, err := session.ParseLoginFinish(body)
	if err != nil {
		httpapi.Fail(w, wire.CodeProtocol)
		return
	}
	// Before the setup has run there is no list and no login. E_UNREACHABLE
	// and not E_NOTFOUND, for the reason login/start gives: the contract's
	// table has no 404 for this path at all.
	v, err := h.S.Trust(ctx)
	if err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	now := h.S.Now()
	// Whose wait this attempt counts against. A key the trust list holds names
	// its owner, and then it is the owner's login name: the contract counts per
	// login name, and login/start reads that same row by the name it was asked
	// for, so a run of failed logins is a wait there too.
	//
	// A key the list does not hold names nobody, and then the key ID counts for
	// itself -- in the counting of the key IDs, which is a set of rows of its
	// own (lock.CheckKey, lock.FailKey). Both halves of that matter:
	//
	//   - Not the user the certificate claims to be: anybody may write any user
	//     into a certificate, so that would let anybody lock out any name.
	//   - And not the key ID in the counting of the names either, which is what
	//     the first version of this file did. lock.key hashes every string into
	//     the same 4096 rows, and the key ID comes straight out of the request:
	//     a few thousand tries at home find one that falls into the row of any
	//     name one likes, and three requests then lay that name's wait -- no
	//     key, no session, no passkey. Measured before it was split, and it is
	//     what TestFinishCannotLockANameWithAChosenKeyID holds shut.
	//
	// What no choice of row closes: login/start hands the key ID of a passkey
	// out (session.PasskeyRef), real for a name that exists and made up for one
	// nobody has, so failed logins carrying a real one lock that name and the
	// answer of login/start afterwards says so. Closing that needs something
	// this path is not given -- login/finish learns no login name, only the
	// user ID in the certificate -- and it is a question for V1.2.
	owner, _, known := v.List.KeyOwner(f.Proof.KeyID)
	wait, err := waitOf(ctx, h.S.DB, owner.Name, f.Proof.KeyID, known, now)
	if err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	if wait > 0 {
		httpapi.FailRetry(w, wire.CodeLocked, wait)
		return
	}
	deny := func() {
		// The answer is E_LOGIN whether the failure was counted or not: the
		// login failed, and that is what E_LOGIN says. A 503 would tell the
		// user the service is down when it was their password, and it would be
		// the one status of this handler that follows the state of the
		// database instead of the login.
		//
		// Swallowed, but not silent. With the counting off, nothing slows
		// guessing down at all and no answer says so -- every attempt is
		// answered exactly as before. So it is said out loud, once per
		// lockWarnTTL and without a word of the request, so that it costs no
		// answer and cannot be made to fill a disk by asking often.
		//
		// The line is the fixed text and nothing else, the error of the
		// database included. A driver may quote the values of the statement
		// back, and the values of this one are the row of a login name or of a
		// key ID and the count in it; whoever needs the cause gets it from the
		// database itself.
		if err := countFailure(ctx, h.S.DB, owner.Name, f.Proof.KeyID, known, now); err != nil &&
			h.warnLock.take(now, lockWarnTTL) {
			slog.Error("a failed login could not be counted; nothing holds up guessing until the database can be written again")
		}
		httpapi.Fail(w, wire.CodeLogin)
	}

	verified, err := session.VerifyClient(f.Cert, f.Proof, v.List, now)
	if err != nil {
		// session.ErrUser is the one answer session.verifyCert gives after it
		// has read the certificate and before it looks at any signature: the
		// key belongs to nobody, or it belongs to another user than the
		// certificate names, or a role wants a passkey and this is not one. A
		// key nobody holds gives it, and a certificate naming the wrong user
		// with a key the list does hold gives it too -- so a verification
		// spent here is spent on both sides of the question "does this name
		// exist", which is what it is for.
		//
		// It hung on the key alone before, and that closed one shape and tore
		// another open, that one with the known side the cheap one. What is
		// still not closed, and is not claimed to be: package proof refuses
		// some proofs cheaply as well, and there the known side pays nothing
		// while this side pays a whole verification. Three such shapes are
		// known; they are listed at blindProof, measured in
		// TestFinishTakesTheSameTimeForAKeyNobodyHolds and counted there, so
		// the number cannot quietly grow again.
		if errors.Is(err, session.ErrUser) {
			_ = blindProof(f.Cert)
		}
		deny()
		return
	}
	cert := verified.Cert()
	// The browser writes the end of the session into the certificate itself,
	// and session.ClientCert.Validate holds it only to session.MaxLifetime.
	// This installation may have configured less, and then a certificate that
	// reaches further makes no session.
	if cert.Expires > now+h.S.Cfg.SessionLifetime {
		deny()
		return
	}
	// A role that asks for the password too: the proof above was by a passkey
	// (session.VerifyClient sees to that), and the password comes on top.
	if v.List.PasswordToo(cert.User) {
		if f.Password == nil || session.VerifyPassword(verified, *f.Password, v.List) != nil {
			deny()
			return
		}
	}
	// The row holds what the trust list does not: the TOTP secret. A row that
	// is not there is no failure -- the list says who may log in, and a user of
	// the list who has none simply holds no second factor. Any other error is
	// the database's and says nothing about the login, so it is answered as
	// such: the certificate was verified by now, so whoever reads that answer
	// already knows this user exists.
	row, err := h.S.DB.UserGet(ctx, cert.User)
	if err != nil && !isNotFound(err) {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	if len(row.TOTP) > 0 && !h.checkTOTP(row, f.TOTP, now) {
		deny()
		return
	}
	// The nonce is spent exactly once and has to be the one this user got:
	// OnceUse hands back the user ID login/start kept beside it, and a nonce
	// that was never kept -- every one of a made-up challenge -- is not there
	// at all.
	nonceUser, err := h.S.DB.OnceUse(ctx, dbapi.OnceNonce, cert.Nonce, now)
	if err != nil || nonceUser != cert.User {
		deny()
		return
	}
	// owner is this user, and the list is where it comes from:
	// session.VerifyClient took the certificate against a key of theirs, and
	// verifying it means the key was found and owner.ID == cert.User. So the
	// row written below carries the name and the roles the list says, not
	// those an older row may still hold.
	res, token, err := h.newSession(ctx, verified, f.Cert, f.Proof)
	if err != nil {
		// Of the things newSession can fail at -- the write of the session
		// row, the random the keys come from, the signing key of this node --
		// the write is the one that happens, and it is the one a second try
		// can get past. None of them is the caller's doing, so this answer
		// says nothing about the login either.
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	// The counting is left where it is: a login that worked clears no bucket.
	// A bucket holds every name that falls into it and a login name is free to
	// choose, so whoever puts a name of their own into every bucket would have
	// their count wiped by every successful login anybody makes. Nothing has to
	// be cleared either -- a row past its keep time reads as none by itself
	// (classes/lock, read) -- and the price is named: the user's own bucket
	// stays counted until then (apps/server-api/CLAUDE.md, the login section).
	//
	// Failures and Locked are the row's own two numbers and not that counting;
	// nothing maintains them today, so a login sets them where a login leaves
	// them. LastLogin is what ngc.users/list shows (core.UserInfo).
	row.ID, row.Name, row.Roles = owner.ID, owner.Name, owner.Roles
	row.Failures, row.Locked, row.LastLogin = 0, 0, now
	if err := h.S.DB.UserPut(ctx, row); err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	b := mustJSON(w, res)
	if b == nil {
		return
	}
	// The cookie is set only once the answer is known to be writable: a cookie
	// beside an error answer would name a session the shell has no API
	// certificate for.
	httpapi.SetCookie(w, token)
	httpapi.Answer(w, http.StatusOK, b)
}

// waitOf and countFailure are the one place that picks between the two
// countings, so the choice cannot be made differently in two spots. known is
// what trust.List.KeyOwner said: with it the login name counts, without it the
// key ID, and the two never share a row (classes/lock, nameSpace and keySpace).
//
// Both are asked on every attempt, whichever way known fell, so the two make
// the same one read and the same one read-and-write -- a difference there would
// say over the clock what the answers keep quiet.
func waitOf(ctx context.Context, db dbapi.Local, name, keyID string, known bool, now int64) (int64, error) {
	if known {
		return lock.Check(ctx, db, name, now)
	}
	return lock.CheckKey(ctx, db, keyID, now)
}

func countFailure(ctx context.Context, db dbapi.Local, name, keyID string, known bool, now int64) error {
	if known {
		return lock.Fail(ctx, db, name, now)
	}
	return lock.FailKey(ctx, db, keyID, now)
}

// newSession makes the keys of the API's side of the session, signs the API
// certificate with the key of this node and writes the session row. It gives
// back the answer for the shell and the value of the cookie; the row keeps
// only session.TokenHash of that value, so a dump of the database holds no
// cookie anybody could use.
func (h *Handler) newSession(ctx context.Context, v session.Verified, client []byte, p proof.Proof) (session.LoginResult, string, error) {
	cert := v.Cert()
	now := h.S.Now()
	pair, err := noisep.Keypair(rand.Reader)
	if err != nil {
		return session.LoginResult{}, "", err
	}
	apiCert := session.APICert{
		Installation: cert.Installation, Node: h.S.Cfg.Node,
		Client: enc.Bytes(session.CertHash(client)), DH: enc.Bytes(pair.Public),
		Issued: now, Expires: cert.Expires,
	}
	seed := h.S.Cfg.SignKey.Bytes()
	if len(seed) != ed25519.SeedSize {
		return session.LoginResult{}, "", errSignKey
	}
	env, err := session.SignAPI(ed25519.NewKeyFromSeed(seed), apiCert)
	if err != nil {
		return session.LoginResult{}, "", err
	}
	certJSON, err := json.Marshal(env)
	if err != nil {
		return session.LoginResult{}, "", err
	}
	proofJSON, err := json.Marshal(p)
	if err != nil {
		return session.LoginResult{}, "", err
	}
	// Any API serves any session, so the private session key lies in the
	// database, sealed with the key every API of the installation shares.
	sealed, err := h.S.Box.Seal(pair.Private)
	if err != nil {
		return session.LoginResult{}, "", err
	}
	raw, err := secrets.Bytes(session.TokenSize)
	if err != nil {
		return session.LoginResult{}, "", err
	}
	row := dbapi.Session{
		Token: session.TokenHash(raw), User: cert.User, Cert: client, Proof: proofJSON,
		APIKey: sealed, APICert: certJSON, Created: now, Expires: cert.Expires, Seen: now,
	}
	if err := h.S.DB.SessionPut(ctx, row); err != nil {
		return session.LoginResult{}, "", err
	}
	return session.LoginResult{API: env, PasswordHint: p.Kind == proof.KindPassword},
		enc.B64.EncodeToString(raw), nil
}
