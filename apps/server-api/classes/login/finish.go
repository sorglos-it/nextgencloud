package login

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	json "encoding/json/v2"
	"errors"
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

// errSignKey reports a signing key of the wrong size in the configuration;
// config.API.Validate refuses such a file, so it can only mean the key was
// swapped underneath us.
var errSignKey = errors.New("login: signing key of the wrong size")

// Finish answers POST session.PathLoginFinish. Here the checks of a login come
// together: the proof over the client certificate against the trust list, the
// password of a role that asks for it, the TOTP code of a user who holds a
// secret, and the nonce, which is spent exactly once. Only then are the
// session, the API certificate and the cookie made.
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
		_ = lock.Fail(ctx, h.S.DB, bucket, now)
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
	// under (trust.List.KeyOwner). So the name below is theirs.
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
	// A login that worked ends the wait and writes the row back without the
	// failures. A Clear that does not work leaves the count standing, which
	// costs the user a wait they can see and wait out -- nothing is let
	// through by it, so it is no reason to refuse a login that was right.
	_ = lock.Clear(ctx, h.S.DB, u.Name)
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
