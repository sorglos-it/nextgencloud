package login

import (
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
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// lockWarnTTL is how seldom a failed login that could not be counted is said
// out loud. The line stands for a state of the database and not for a request,
// so one of them per five minutes says everything there is to say -- and every
// failed login would say it, which is a line per wrong password.
const lockWarnTTL = 300

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
	// The bucket of the wait is the login name of the key's owner: the
	// contract counts per login name, and login/start reads the same bucket by
	// that name, so a run of failed logins is a wait there too.
	//
	// A key the list does not hold names no user, and then the bucket is the
	// key ID. Not the user the certificate claims to be: anybody may write any
	// user into a certificate, so that would let anybody lock out any name --
	// and the wait login/start answers afterwards would say whether that name
	// exists, because a name nobody has gets a made-up user ID nothing else
	// ever counts for. A key ID is of one shape for every attempt and says
	// nothing about anybody.
	//
	// What this does not close, and what no bucket taken from a body can: a
	// challenge that carries a passkey hands its key ID out
	// (session.PasskeyRef), and for a name nobody has that ID is made up and
	// belongs to no user. So failed logins carrying it lock the name if it
	// exists and nothing at all if it does not, and login/start afterwards
	// answers 429 against 200 -- measured, one challenge, three failures and
	// one more challenge. The wait per login name costs the other half of that
	// anyway: whoever knows the names of an installation locks them out one by
	// one (classes/lock, buckets). What is new is that it also says which names
	// those are. Closing it needs something this path is not given: login/finish
	// learns no login name, only the user ID in the certificate, where the
	// contract counts per name (shared/README.md, "Vor dem WebSocket"). It is a
	// question for V1.2 and not one to be settled here.
	bucket := "key/" + f.Proof.KeyID
	if owner, _, found := v.List.KeyOwner(f.Proof.KeyID); found {
		bucket = owner.Name
	}
	wait, err := lock.Check(ctx, h.S.DB, bucket, now)
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
		// back, and the values of this one are the bucket of a login name and
		// its count; whoever needs the cause gets it from the database itself.
		if err := lock.Fail(ctx, h.S.DB, bucket, now); err != nil && h.warnLock.take(now, lockWarnTTL) {
			slog.Error("a failed login could not be counted; nothing holds up guessing until the database can be written again")
		}
		httpapi.Fail(w, wire.CodeLogin)
	}

	verified, err := session.VerifyClient(f.Cert, f.Proof, v.List, now)
	if err != nil {
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
	owner, err := h.S.DB.OnceUse(ctx, dbapi.OnceNonce, cert.Nonce, now)
	if err != nil || owner != cert.User {
		deny()
		return
	}
	// The list holds this user: session.VerifyClient took the certificate
	// against a key of theirs, and a key belongs to the user it is listed
	// under (trust.List.KeyOwner). So the row written below carries the name
	// and the roles the list says, not those an older row may still hold.
	u, _ := v.List.User(cert.User)
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
	row.ID, row.Name, row.Roles = u.ID, u.Name, u.Roles
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
