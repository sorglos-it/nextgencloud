package login

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

func TestFinishMakesASession(t *testing.T) {
	h, fx := newFixture(t)
	c := start(t, h, fx.PasswordName)
	body := finishBody(t, fx, fx.Password, c, "")
	w := httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
	if w.Code != 200 {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	var res session.LoginResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("result: %v", err)
	}
	if !res.PasswordHint {
		t.Fatal("a password login must show the hint to switch to passkeys")
	}
	v, _ := h.S.Trust(ctx)
	cert, err := session.VerifyAPI(res.API, v.List, certBytes(t, fx, c, fx.Password), h.S.Now())
	if err != nil {
		t.Fatalf("api certificate: %v", err)
	}
	if cert.Node != "api-1" {
		t.Fatalf("node %q", cert.Node)
	}
	cookie := w.Result().Cookies()
	if len(cookie) != 1 || cookie[0].Name != session.CookieName || cookie[0].Value == "" {
		t.Fatalf("cookie %+v", cookie)
	}
	raw, err := enc.B64.DecodeString(cookie[0].Value)
	if err != nil || len(raw) != session.TokenSize {
		t.Fatalf("cookie value: %v", err)
	}
	row, err := h.S.DB.SessionGet(ctx, session.TokenHash(raw))
	if err != nil || row.User != fx.PasswordUser {
		t.Fatalf("session row: %v %+v", err, row)
	}
	if string(row.APIKey) == "" {
		t.Fatal("the API session key is missing")
	}
	// The private key of the session lies sealed: any API of the installation
	// opens it with the shared key, a dump of the database with none.
	if bytes.Contains(row.APIKey, sessionKeyOf(t, h, row)) {
		t.Fatal("the private session key lies in the clear")
	}
	// A login that worked writes the row back: the row's own two numbers at
	// zero and the time of this login, which ngc.users/list shows. What the
	// wait is counted in is another thing entirely and stays where it is --
	// see TestFinishLeavesTheCountingAlone.
	user, err := h.S.DB.UserGet(ctx, fx.PasswordUser)
	if err != nil || user.Failures != 0 || user.LastLogin != h.S.Now() {
		t.Fatalf("user row %+v (%v)", user, err)
	}
}

// sessionKeyOf opens the sealed session key, so a test can say what must not
// stand next to it in the row.
func sessionKeyOf(t *testing.T, h *Handler, row dbapi.Session) []byte {
	t.Helper()
	key, err := h.S.Box.Open(row.APIKey)
	if err != nil {
		t.Fatalf("session key: %v", err)
	}
	return key
}

func TestFinishUsesTheNonceOnlyOnce(t *testing.T) {
	h, fx := newFixture(t)
	c := start(t, h, fx.PasswordName)
	body := finishBody(t, fx, fx.Password, c, "")
	w := httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
	if w.Code != 200 {
		t.Fatalf("first: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
	if w.Code != 401 {
		t.Fatalf("replay answered %d", w.Code)
	}
}

func TestFinishRefusesAForeignNonce(t *testing.T) {
	h, fx := newFixture(t)
	// A nonce the API handed to another user does not make this user a session.
	other := start(t, h, fx.PasskeyName)
	c := start(t, h, fx.PasswordName)
	c.Nonce = other.Nonce
	body := finishBody(t, fx, fx.Password, c, "")
	w := httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
	if w.Code != 401 {
		t.Fatalf("foreign nonce answered %d", w.Code)
	}
}

// TestFinishRefusesACertificateBeyondTheSessionLifetime: the browser writes
// the end of the session into the certificate itself, so the API holds it to
// the lifetime of its own configuration and not to what the challenge said.
func TestFinishRefusesACertificateBeyondTheSessionLifetime(t *testing.T) {
	h, fx := newFixture(t)
	c := start(t, h, fx.PasswordName)
	c.MaxLifetime = h.S.Cfg.SessionLifetime + 1
	body := finishBody(t, fx, fx.Password, c, "")
	w := httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
	if w.Code != 401 {
		t.Fatalf("a certificate one second too long answered %d", w.Code)
	}
}

// TestFinishTakesTheTOTPCode: a user who holds a second factor gets in with
// the code of the moment, and only with it.
func TestFinishTakesTheTOTPCode(t *testing.T) {
	h, fx := newFixture(t)
	code := withTOTP(t, h, fx.PasswordUser, fx.PasswordName)
	c := start(t, h, fx.PasswordName)
	if !c.TOTP {
		t.Fatal("the challenge does not ask for the code")
	}
	w := httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(finishBody(t, fx, fx.Password, c, code))))
	if w.Code != 200 {
		t.Fatalf("the right code answered %d: %s", w.Code, w.Body)
	}
}

// TestFinishAsksForThePasswordToo holds the second proof of a role with
// passwordToo: the passkey alone is not enough, the password key alone is not
// either, and only the two together make a session.
func TestFinishAsksForThePasswordToo(t *testing.T) {
	h, fx := newFixture(t)
	passkey, password := passwordToo(t, h, fx)
	c := start(t, h, fx.PasskeyName)
	if !c.PasswordToo {
		t.Fatal("the challenge does not ask for the password")
	}
	// The passkey alone.
	w := httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(finishBody(t, fx, passkey, c, ""))))
	if w.Code != 401 {
		t.Fatalf("the passkey alone answered %d", w.Code)
	}
	// The password key alone: session.VerifyClient refuses it for such a role,
	// so it never even reaches the second proof.
	c = start(t, h, fx.PasskeyName)
	w = httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(finishBody(t, fx, password, c, ""))))
	if w.Code != 401 {
		t.Fatalf("the password key alone answered %d", w.Code)
	}
	// Both, and the login goes through.
	c = start(t, h, fx.PasskeyName)
	w = httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(finishWith(t, fx, passkey, &password, c, ""))))
	if w.Code != 200 {
		t.Fatalf("passkey and password answered %d: %s", w.Code, w.Body)
	}
	var res session.LoginResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("result: %v", err)
	}
	// The hint follows the first proof, which is the passkey here: whoever
	// logs in with one is not told to switch to one.
	if res.PasswordHint {
		t.Fatal("a passkey login shows the hint to switch to passkeys")
	}
}

// TestFinishAnswersEveryFailureAlike is the rule of the contract: E_LOGIN
// stands for every failed login alike (shared/README.md, "Vor dem WebSocket").
// Each case gets a fixture of its own, so the counting of one is no part of
// the next, and all of them are held against the first -- status, headers and
// body. Nothing in an answer says which part failed, and nothing says whether
// the name exists.
func TestFinishAnswersEveryFailureAlike(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T, h *Handler, fx fixture) []byte
	}{
		{"a name nobody has", func(t *testing.T, h *Handler, fx fixture) []byte {
			return finishBody(t, fx, fx.Password, start(t, h, "nobody"), "")
		}},
		{"a key of nobody", func(t *testing.T, h *Handler, fx fixture) []byte {
			return finishBody(t, fx, trusttest.NewSigner(200), start(t, h, fx.PasswordName), "")
		}},
		{"a nonce nobody handed out", func(t *testing.T, h *Handler, fx fixture) []byte {
			c := start(t, h, fx.PasswordName)
			c.Nonce = madeUpNonce
			return finishBody(t, fx, fx.Password, c, "")
		}},
		{"a nonce that was spent", func(t *testing.T, h *Handler, fx fixture) []byte {
			body := finishBody(t, fx, fx.Password, start(t, h, fx.PasswordName), "")
			w := httptest.NewRecorder()
			h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
			if w.Code != 200 {
				t.Fatalf("the login that spends the nonce answered %d", w.Code)
			}
			return body
		}},
		{"a wrong TOTP code", func(t *testing.T, h *Handler, fx fixture) []byte {
			withTOTP(t, h, fx.PasswordUser, fx.PasswordName)
			return finishBody(t, fx, fx.Password, start(t, h, fx.PasswordName), "000000")
		}},
	}
	var first *httptest.ResponseRecorder
	for _, c := range cases {
		h, fx := newFixture(t)
		w := httptest.NewRecorder()
		h.Finish(w, jsonPost(session.PathLoginFinish, string(c.build(t, h, fx))))
		if w.Code != 401 {
			t.Fatalf("%s answered %d: %s", c.name, w.Code, w.Body)
		}
		if first == nil {
			first = w
			continue
		}
		if w.Body.String() != first.Body.String() {
			t.Fatalf("%s answered %s, the first case %s", c.name, w.Body, first.Body)
		}
		if !maps.EqualFunc(w.Header(), first.Header(), slices.Equal[[]string]) {
			t.Fatalf("%s answered with the headers %v, the first case %v", c.name, w.Header(), first.Header())
		}
	}
}

// TestFinishLocksAfterFailures: the wait is counted per login name
// (shared/README.md), so a failed login of a user holds their name at
// login/start too. The attempts here carry the user's own key and a nonce
// nobody handed out -- with a key of nobody there is no name to count it for
// (see the bucket in Finish), which is what
// TestFinishDoesNotLockANameAKeyOfNobodyNamed holds.
func TestFinishLocksAfterFailures(t *testing.T) {
	h, fx := newFixture(t)
	c := start(t, h, fx.PasswordName)
	c.Nonce = madeUpNonce
	body := finishBody(t, fx, fx.Password, c, "")
	for i := range 4 {
		w := httptest.NewRecorder()
		h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
		if w.Code != 401 && w.Code != 429 {
			t.Fatalf("attempt %d answered %d", i, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.Start(w, jsonPost(session.PathLoginStart, `{"name":"`+fx.PasswordName+`"}`))
	if w.Code != 429 {
		t.Fatalf("the name is not locked: %d", w.Code)
	}
}

// TestFinishLeavesTheCountingAlone: a login that worked must not clear the
// bucket of the name. A bucket holds every name that falls into it
// (classes/lock, buckets) and a login name is free to choose, so whoever puts
// a name of their own into every bucket would have their count wiped by every
// successful login anybody makes. Clearing is not needed either: a row past
// its keep time reads as none by itself (apps/server-api/CLAUDE.md, the login
// section). Only the setup clears its own bucket, which is no login name.
func TestFinishLeavesTheCountingAlone(t *testing.T) {
	h, fx := newFixture(t)
	failLogin(t, h, fx.PasswordName)
	failLogin(t, h, fx.PasswordName)
	c := start(t, h, fx.PasswordName)
	w := httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(finishBody(t, fx, fx.Password, c, ""))))
	if w.Code != 200 {
		t.Fatalf("the login answered %d: %s", w.Code, w.Body)
	}
	// The third failure of the bucket is the one that lays the wait -- unless
	// the login took the first two away.
	failLogin(t, h, fx.PasswordName)
	w = httptest.NewRecorder()
	h.Start(w, jsonPost(session.PathLoginStart, `{"name":"`+fx.PasswordName+`"}`))
	if w.Code != 429 {
		t.Fatalf("the login cleared what was counted for the bucket: %d", w.Code)
	}
}

// TestFinishCannotLockANameWithAChosenKeyID is the deterministic form of the
// property the first version of this file got wrong. A failed login whose key
// the trust list does not hold must not reach the counting of any login name
// -- otherwise three requests lock any name out, with no key, no session and
// no passkey, and the answer of login/start afterwards says whether that name
// exists at all.
//
// The key ID is not left to chance: it is searched until the counting would
// put it in the same row as the victim's name, in both shapes that matter --
// the bare key ID and the "key/" the first version built. A test that picks an
// arbitrary key ID misses that row with 4095 of 4096 and is green for nothing.
func TestFinishCannotLockANameWithAChosenKeyID(t *testing.T) {
	shapes := []struct {
		name string
		of   func(string) string
	}{
		{"the bare key ID", func(id string) string { return id }},
		{`"key/" and the key ID`, func(id string) string { return "key/" + id }},
	}
	for _, shape := range shapes {
		h, fx := newFixture(t)
		kid := keyIDInTheRowOf(t, fx.PasswordName, shape.of)
		c := start(t, h, fx.PasswordName)
		body := finishWithKeyID(t, fx, c, kid)
		for i := range 4 {
			w := httptest.NewRecorder()
			h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
			if w.Code != 401 && w.Code != 429 {
				t.Fatalf("%s: attempt %d answered %d", shape.name, i, w.Code)
			}
		}
		w := httptest.NewRecorder()
		h.Start(w, jsonPost(session.PathLoginStart, `{"name":"`+fx.PasswordName+`"}`))
		if w.Code != 200 {
			t.Fatalf("%s: %q was locked out by a key ID nobody holds: %d",
				shape.name, fx.PasswordName, w.Code)
		}
	}
}

// keyIDInTheRowOf returns a key ID the counting would keep in the same row as
// name, once of is applied to it. It asks the counting itself instead of
// rebuilding its hash here, so it cannot drift from it: one failure leaves one
// row behind, and clearing name's row takes that row away only when the two
// share it. There are 4096 rows, so a few thousand tries find one.
func keyIDInTheRowOf(tb testing.TB, name string, of func(string) string) string {
	tb.Helper()
	var raw [16]byte
	for i := range 1 << 20 {
		binary.BigEndian.PutUint64(raw[8:], uint64(i))
		id := enc.B64.EncodeToString(raw[:])
		db := memstore.New()
		if err := lock.Fail(ctx, db, of(id), testNow); err != nil {
			tb.Fatalf("fail: %v", err)
		}
		if err := lock.Clear(ctx, db, name); err != nil {
			tb.Fatalf("clear: %v", err)
		}
		rows, err := db.BlobFrom(ctx, dbapi.BlobSetting, 0)
		if err != nil {
			tb.Fatalf("rows: %v", err)
		}
		if len(rows) == 0 {
			return id
		}
	}
	tb.Fatalf("no key ID found that shares the row of %q", name)
	return ""
}

// finishWithKeyID builds a body whose proof names kid. The signature is one of
// the user's own and holds for nothing under that name, which is all this
// needs: a key ID nobody holds is turned away before any signature is looked
// at.
func finishWithKeyID(tb testing.TB, fx fixture, c session.LoginChallenge, kid string) []byte {
	tb.Helper()
	payload := certBytes(tb, fx, c, fx.Password)
	p := fx.Password.Prove(sign.PurposeClientCert, payload)
	p.KeyID = kid
	b, err := json.Marshal(session.LoginFinish{Cert: enc.Bytes(payload), Proof: p})
	if err != nil {
		tb.Fatalf("marshal: %v", err)
	}
	return b
}

// TestFinishSaysSoWhenAFailureCannotBeCounted holds the one thing login/finish
// swallows to the rule login/start keeps for the nonce it cannot store. A
// failure that cannot be counted stays E_LOGIN: the login failed, and that is
// what E_LOGIN says; a 503 would tell the user the service is down when it was
// their password.
//
// Swallowed, but not silent: with the counting off nothing at all slows
// guessing down, and no answer says so. The line is said once per window and
// names nothing of the request, so it costs no answer and cannot be made to
// fill a disk by asking often.
func TestFinishSaysSoWhenAFailureCannotBeCounted(t *testing.T) {
	h, fx := newFixture(t)
	now := testNow
	h.S.Now = func() int64 { return now }
	c := start(t, h, fx.PasswordName)
	c.Nonce = madeUpNonce
	body := finishBody(t, fx, fx.Password, c, "")
	// Only now, so that the challenge above is a real one: what this stands
	// for is a database that has stopped taking settings, not one that never
	// took any.
	h.S.DB = noCount{Store: h.S.DB}
	log := capturedLog(t)
	try := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
		return w
	}
	for range 20 {
		if w := try(); w.Code != 401 {
			t.Fatalf("a failure that cannot be counted answered %d: %s", w.Code, w.Body)
		}
	}
	if lines := strings.Count(log.String(), "\n"); lines != 1 {
		t.Fatalf("%d lines for 20 requests, want 1:\n%s", lines, log)
	}
	if s := log.String(); strings.Contains(s, fx.PasswordName) || strings.Contains(s, fx.PasswordUser) {
		t.Fatalf("the line names the request: %s", s)
	}
	// Nor the error of the database: a driver may quote the values of the
	// statement back, and those are the bucket of a login name and its count.
	if s := log.String(); strings.Contains(s, noCountError) {
		t.Fatalf("the line carries the error of the database: %s", s)
	}
	// One second before the window is over it is still quiet.
	now = testNow + lockWarnTTL - 1
	try()
	if lines := strings.Count(log.String(), "\n"); lines != 1 {
		t.Fatalf("%d lines after %d seconds, want 1", lines, lockWarnTTL-1)
	}
	// On the second itself it is said again, so a failure that lasts does not
	// fall silent after the first window.
	now = testNow + lockWarnTTL
	try()
	if lines := strings.Count(log.String(), "\n"); lines != 2 {
		t.Fatalf("%d lines after %d seconds, want 2:\n%s", lines, lockWarnTTL, log)
	}
}

// TestFinishAndStartDoNotSilenceEachOther: the two lines stand for two
// different failures of the database -- one-time values that cannot be kept
// and settings that cannot be written -- and a store can have one without the
// other. So neither may take the other's turn to speak.
func TestFinishAndStartDoNotSilenceEachOther(t *testing.T) {
	h, fx := newFixture(t)
	c := start(t, h, fx.PasswordName)
	c.Nonce = madeUpNonce
	body := finishBody(t, fx, fx.Password, c, "")
	h.S.DB = noCount{Store: noOnce{Store: h.S.DB}}
	log := capturedLog(t)
	w := httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
	raw(t, h, fx.PasswordName)
	if lines := strings.Count(log.String(), "\n"); lines != 2 {
		t.Fatalf("%d lines for both failures, want 2:\n%s", lines, log)
	}
}

func TestFinishRefusesABadBody(t *testing.T) {
	h, _ := newFixture(t)
	for _, body := range []string{`{}`, `{"cert":""}`, `null`, `{"cert":"AA","proof":{}}`,
		`{"cert":"AA","proof":{"kind":"password","kid":"AAAAAAAAAAAAAAAAAAAAAA","sig":"AA"},"totp":"12345"}`} {
		w := httptest.NewRecorder()
		h.Finish(w, jsonPost(session.PathLoginFinish, body))
		if w.Code != 400 {
			t.Fatalf("%s answered %d", body, w.Code)
		}
	}
}

// madeUpNonce is a nonce of the right size the API never handed out, so a
// certificate that carries it is refused at the last door and nowhere earlier.
var madeUpNonce = bytes.Repeat([]byte{0xA5}, session.NonceSize)

// withTOTP gives the user a second factor and returns the code of testNow.
func withTOTP(tb testing.TB, h *Handler, user, name string) string {
	tb.Helper()
	begin, sealed, err := h.BeginTOTP(name)
	if err != nil {
		tb.Fatalf("begin: %v", err)
	}
	row, err := h.S.DB.UserGet(ctx, user)
	if err != nil {
		tb.Fatalf("user: %v", err)
	}
	row.TOTP = sealed
	if err := h.S.DB.UserPut(ctx, row); err != nil {
		tb.Fatalf("user: %v", err)
	}
	code, err := codeOf(begin.Secret)
	if err != nil {
		tb.Fatalf("code: %v", err)
	}
	return code
}

// passwordToo installs a version of the trust list whose admin role asks for
// the password in addition. Every member of such a role has to hold a passkey
// and a password key (trust.List.Validate), so both admins get the key they
// were missing. It returns the two keys of fx.PasskeyUser.
func passwordToo(tb testing.TB, h *Handler, fx fixture) (passkey, password trusttest.Signer) {
	tb.Helper()
	extra, mate := trusttest.NewSigner(3), trusttest.NewPasskey(4)
	l := trusttest.Next(tb, fx.Signed)
	l.Roles[0].PasswordToo = true
	l.Users[0].Keys = append(l.Users[0].Keys, extra.Key)
	l.Users[1].Keys = append(l.Users[1].Keys, mate.Key)
	if _, err := h.S.AddVersion(ctx, fx.Password.Sign(tb, l)); err != nil {
		tb.Fatalf("version 2: %v", err)
	}
	return fx.Passkey, extra
}

// everything is the installation the fuzz target works against: a role that
// asks for the password in addition, and a user of that role who holds a TOTP
// secret. Without both, the guards in front of session.VerifyPassword and
// checkTOTP are false for every body there is -- they hang on the installation
// and not on the request -- and no run of the fuzzer enters either. It returns
// the two keys of fx.PasskeyUser and that user's code at testNow.
func everything(tb testing.TB) (*Handler, fixture, trusttest.Signer, trusttest.Signer, string) {
	tb.Helper()
	h, fx := newFixture(tb)
	passkey, password := passwordToo(tb, h, fx)
	return h, fx, passkey, password, withTOTP(tb, h, fx.PasskeyUser, fx.PasskeyName)
}

// certBytes builds the client certificate the user signs. It takes testing.TB
// and not *testing.T, because FuzzFinish builds its seeds from *testing.F.
func certBytes(tb testing.TB, fx fixture, c session.LoginChallenge, s trusttest.Signer) []byte {
	tb.Helper()
	dh, err := noisep.Keypair(zeroReader{})
	if err != nil {
		tb.Fatalf("keypair: %v", err)
	}
	cert := session.ClientCert{
		Installation: fx.Installation, User: c.User, Nonce: c.Nonce,
		Sig: enc.Bytes(sigPub(s)), DH: enc.Bytes(dh.Public),
		Issued: c.Now, Expires: c.Now + c.MaxLifetime,
	}
	b, err := json.Marshal(cert)
	if err != nil {
		tb.Fatalf("marshal: %v", err)
	}
	return b
}

// finishBody builds the body of login/finish: the certificate, the proof over
// exactly its bytes and, if wanted, a TOTP code.
func finishBody(tb testing.TB, fx fixture, s trusttest.Signer, c session.LoginChallenge, code string) []byte {
	tb.Helper()
	return finishWith(tb, fx, s, nil, c, code)
}

// finishWith adds the second proof a role with passwordToo asks for; it covers
// exactly the same bytes as the first.
func finishWith(tb testing.TB, fx fixture, s trusttest.Signer, pw *trusttest.Signer,
	c session.LoginChallenge, code string) []byte {
	tb.Helper()
	payload := certBytes(tb, fx, c, s)
	f := session.LoginFinish{Cert: enc.Bytes(payload), Proof: s.Prove(sign.PurposeClientCert, payload), TOTP: code}
	if pw != nil {
		p := pw.Prove(sign.PurposeClientCert, payload)
		f.Password = &p
	}
	b, err := json.Marshal(f)
	if err != nil {
		tb.Fatalf("marshal: %v", err)
	}
	return b
}

// nobodysKeyID is a key ID of the right shape that no trust list of these
// tests holds.
var nobodysKeyID = enc.B64.EncodeToString(bytes.Repeat([]byte{0x5A}, 16))

// The ways proof.Verify can answer a proof the certificate check let through
// without ever doing the public-key operation. They are read off its code, one
// by one, and not collected from what somebody happened to find: that number
// was guessed three times and was too small three times. Each of them costs a
// key the trust list holds almost nothing, where a key nobody holds pays the
// whole of blindProof -- so each is a shape login/finish answers in two
// different times.
//
//	proof.Verify
//	  p.Kind != key.Kind                     -> ErrKeyMismatch      [1]
//	  password: ed25519.Verify says no        -> ErrSignature        [6]
//	                without reading the message, for a signature that is
//	                not 64 bytes or whose last byte has high bits set
//	  passkey:  verifyAssertion
//	    client data is no JSON               -> ErrClientData       [2]
//	    type, crossOrigin, challenge, origin -> ErrClientData       [3]
//	    the RP ID hashes to something else   -> ErrAuthData         [4]
//	    the flags say not present/verified   -> ErrNoUV             [5]
//	    the primitive says no                -> ErrSignature        [7]
//	                without the curve, for a signature ASN.1 cannot read or
//	                whose length the algorithm refuses
//
// Every other early answer of proof.Verify is held off before the request ever
// reaches it, and that is what makes this list a list and not a sample:
//
//   - key.ID != sign.KeyID(key.Public), a password key of the wrong algorithm
//     or length, a public key parsePublic refuses, and the two default arms:
//     trust.List.Validate calls proof.UserKey.Validate on every key of every
//     version, and it asks all of those.
//   - p.KeyID != key.ID: trust.List.KeyOwner looked the key up by that very ID.
//   - len(p.Sig) > MaxSig, client data longer than MaxClientData, authenticator
//     data outside 37..MaxAuthData: session.ParseLoginFinish calls
//     proof.Proof.Validate before any of this runs.
//
// The two ErrSignature lines are the ones the enumeration nearly missed. They
// are not "before the signature check" -- they are inside it, and they cost
// nothing all the same, which is what the clock sees.
const (
	branchKeyMismatch  = "ErrKeyMismatch: the proof is of a kind the key is not"
	branchClientParse  = "ErrClientData: the client data is no JSON"
	branchClientFields = "ErrClientData: the client data says something else"
	branchAuthData     = "ErrAuthData: the authenticator data names another relying party"
	branchNoUV         = "ErrNoUV: the authenticator data says nobody was verified"
	branchPasswordSig  = "ErrSignature: ed25519.Verify refuses the signature unread"
	branchPasskeySig   = "ErrSignature: the assertion's own primitive refuses the signature unread"
)

// openBranches is that enumeration as a list the test can hold itself to.
var openBranches = []string{branchKeyMismatch, branchClientParse, branchClientFields,
	branchAuthData, branchNoUV, branchPasswordSig, branchPasskeySig}

// openShapes is how many bodies below stand for those branches -- one more than
// there are branches, because two of them reach ErrClientData by its second
// door. It is a number and not a remark, so that closing one of them without
// taking its flag out is a failing test and not a comment nobody reads.
const openShapes = 8

// TestFinishTakesTheSameTimeForAKeyNobodyHolds is the question login/start
// answers alike for every name, asked once more with a clock: login/start hands
// the key ID of a passkey out, real for a name that exists and made up for one
// nobody has (session.PasskeyRef), so what a proof carrying it costs is that
// same question. Every pair below differs in that one field and in nothing
// else.
//
// Thirteen shapes, and the number stopped being a guess with this list. One
// shape was measured first and was green while two were wide open -- one of
// them torn open by the very change that closed the first. Then four, and two
// more were open. Then eight, and five more were open. Each time the answer was
// "add one and look", and each time the next look found more.
//
// So this time the shapes were not collected, they were read off proof.Verify
// branch by branch: openBranches above names every way it can answer without
// ever doing the public-key operation, and says for every other early answer
// which check ahead of it makes that answer unreachable. The table below has a
// body for each of them, and the test holds itself to that above the
// measurement: every open shape names a branch of the list, and every branch of
// the list has a shape.
//
// Five shapes are alike today and are held to the factor:
//
//   - a password key with the user the certificate names: the known side runs
//     proof.Verify inside session.VerifyClient
//   - a passkey whose assertion is whole and whose signature is of the right
//     length but wrong: the known side runs the whole assertion
//   - a certificate naming another user than the key belongs to: session.
//     verifyCert gives ErrUser without ever checking a signature, and so this
//     side spends one
//   - a certificate the check refuses before it looks the key up at all
//     (session.ErrCert), and one whose time has passed (session.ErrExpired):
//     two controls, not findings -- they are what the choice at blindProof
//     rests on, and nobody had measured them
//
// Eight are not alike, and all eight for one reason: proof.Verify answers them
// without the public-key operation, so the known side pays almost nothing where
// this one pays the whole of blindProof. They carry the branch they reach, and
// openShapes counts them, so closing one without taking its flag out fails.
//
// What this test cannot do, and must not be read as doing: find a shape nobody
// wrote down. That is what the enumeration above is for, and the enumeration is
// read by people.
//
// The factor of two and the null control are those of
// TestStartTakesTheSameTimeForAnUnknownName, for the reasons given there. What
// no clock here measures is a database that is not a map: UserGet and OnceUse
// lie on the path of a proof that holds and not on the other, and on a real
// database those are two round trips (report of task 7, finding 6).
func TestFinishTakesTheSameTimeForAKeyNobodyHolds(t *testing.T) {
	forms := []struct {
		name string
		// branch is empty where the two sides have to cost the same today, and
		// otherwise names the answer of proof.Verify that makes them differ
		// (see openBranches). A shape that is not alike is written down with
		// its branch instead of being left out: closing them needs more than a
		// line, and it belongs with the question login/start leaves open
		// (report of task 7, findings 1 and 2).
		branch string
		build  func(t *testing.T, h *Handler, fx fixture) (held, nobodys []byte)
	}{
		{"a password key over the user it belongs to", "",
			func(t *testing.T, h *Handler, fx fixture) ([]byte, []byte) {
				c := start(t, h, fx.PasswordName)
				c.Nonce = madeUpNonce
				return finishBody(t, fx, fx.Password, c, ""), finishWithKeyID(t, fx, c, nobodysKeyID)
			}},
		{"a passkey whose signature is wrong", "",
			func(t *testing.T, h *Handler, fx fixture) ([]byte, []byte) {
				c := start(t, h, fx.PasskeyName)
				c.Nonce = madeUpNonce
				return twoBodies(t, fx, c, fx.Passkey, func(p *proof.Proof) { p.Sig[0] ^= 0xFF })
			}},
		{"a passkey whose client data is nonsense", branchClientFields,
			func(t *testing.T, h *Handler, fx fixture) ([]byte, []byte) {
				c := start(t, h, fx.PasskeyName)
				c.Nonce = madeUpNonce
				return twoBodies(t, fx, c, fx.Passkey, func(p *proof.Proof) { p.ClientData = enc.Bytes(`{}`) })
			}},
		{"a certificate naming another user than the key", "",
			func(t *testing.T, h *Handler, fx fixture) ([]byte, []byte) {
				// The certificate is of the passkey user, the proof by the
				// password user's key: session.verifyCert finds the key, sees
				// the two names do not match and gives ErrUser -- without
				// checking a signature.
				c := start(t, h, fx.PasskeyName)
				c.Nonce = madeUpNonce
				return twoBodies(t, fx, c, fx.Password, nil)
			}},
		{"a proof of a kind the key is not", branchKeyMismatch,
			func(t *testing.T, h *Handler, fx fixture) ([]byte, []byte) {
				// A passkey's key ID under a password proof: verifyCert lets
				// it through -- the key is a passkey, which is what the roles
				// ask about -- and proof.Verify turns it away at p.Kind
				// against key.Kind, before any cryptography.
				c := start(t, h, fx.PasskeyName)
				c.Nonce = madeUpNonce
				return twoBodies(t, fx, c, fx.Passkey, func(p *proof.Proof) {
					p.Kind = proof.KindPassword
					p.AuthData, p.ClientData = nil, nil
				})
			}},
		{"a passkey naming another relying party", branchAuthData,
			func(t *testing.T, h *Handler, fx fixture) ([]byte, []byte) {
				// The assertion is whole and its client data holds; only the
				// hash of the RP ID at the front of the authenticator data is
				// another one. package proof refuses that before the
				// signature.
				c := start(t, h, fx.PasskeyName)
				c.Nonce = madeUpNonce
				return twoBodies(t, fx, c, fx.Passkey, func(p *proof.Proof) {
					copy(p.AuthData[:32], bytes.Repeat([]byte{0x3C}, 32))
				})
			}},
		{"client data that is not JSON at all", branchClientParse,
			func(t *testing.T, h *Handler, fx fixture) ([]byte, []byte) {
				c := start(t, h, fx.PasskeyName)
				c.Nonce = madeUpNonce
				return twoBodies(t, fx, c, fx.Passkey, func(p *proof.Proof) {
					p.ClientData = enc.Bytes("not json at all")
				})
			}},
		{"client data that parses and crosses origins", branchClientFields,
			func(t *testing.T, h *Handler, fx fixture) ([]byte, []byte) {
				c := start(t, h, fx.PasskeyName)
				c.Nonce = madeUpNonce
				return twoBodies(t, fx, c, fx.Passkey, func(p *proof.Proof) {
					p.ClientData = enc.Bytes(`{"type":"webauthn.get","crossOrigin":true}`)
				})
			}},
		{"authenticator data saying nobody was verified", branchNoUV,
			func(t *testing.T, h *Handler, fx fixture) ([]byte, []byte) {
				c := start(t, h, fx.PasskeyName)
				c.Nonce = madeUpNonce
				return twoBodies(t, fx, c, fx.Passkey, func(p *proof.Proof) { p.AuthData[32] = 0 })
			}},
		{"a password signature of one byte", branchPasswordSig,
			func(t *testing.T, h *Handler, fx fixture) ([]byte, []byte) {
				c := start(t, h, fx.PasswordName)
				c.Nonce = madeUpNonce
				return twoBodies(t, fx, c, fx.Password, func(p *proof.Proof) { p.Sig = p.Sig[:1] })
			}},
		{"a passkey signature of one byte", branchPasskeySig,
			func(t *testing.T, h *Handler, fx fixture) ([]byte, []byte) {
				c := start(t, h, fx.PasskeyName)
				c.Nonce = madeUpNonce
				return twoBodies(t, fx, c, fx.Passkey, func(p *proof.Proof) { p.Sig = p.Sig[:1] })
			}},
		// The two below are controls, not findings: they are the ways out of
		// session.VerifyClient that the choice at blindProof rests on being
		// alike, and nobody had ever measured them.
		{"a certificate the check refuses before the key", "",
			func(t *testing.T, h *Handler, fx fixture) ([]byte, []byte) {
				// session.ErrCert: the certificate is no certificate, and
				// verifyCert stops before it looks up the key at all.
				c := start(t, h, fx.PasswordName)
				c.Nonce = madeUpNonce
				payload := certBytes(t, fx, c, fx.Password)
				p := fx.Password.Prove(sign.PurposeClientCert, payload)
				one := session.LoginFinish{Cert: enc.Bytes("not a certificate"), Proof: p}
				other := one
				other.Proof.KeyID = nobodysKeyID
				return mustMarshal(t, one), mustMarshal(t, other)
			}},
		{"a certificate whose time has passed", "",
			func(t *testing.T, h *Handler, fx fixture) ([]byte, []byte) {
				// session.ErrExpired: the proof is checked in full and the
				// time is looked at afterwards, so the known side has paid
				// for a verification by then.
				c := start(t, h, fx.PasswordName)
				c.Nonce = madeUpNonce
				c.Now = testNow - 2*h.S.Cfg.SessionLifetime
				return twoBodies(t, fx, c, fx.Password, nil)
			}},
	}
	// The table is held to the enumeration before a single request is sent, so
	// a machine that cannot measure anything today still says whether somebody
	// has edited the list: every open shape names a branch of openBranches, and
	// every branch has a shape.
	open, covered := 0, map[string]bool{}
	for _, form := range forms {
		if form.branch == "" {
			continue
		}
		open++
		if !slices.Contains(openBranches, form.branch) {
			t.Errorf("%q names the branch %q, which openBranches does not have", form.name, form.branch)
		}
		covered[form.branch] = true
	}
	if open != openShapes {
		t.Errorf("%d shapes are written down as open, want %d: whoever closes one takes its flag out here",
			open, openShapes)
	}
	for _, b := range openBranches {
		if !covered[b] {
			t.Errorf("no shape reaches %q: an enumerated branch nobody measures is one nobody knows about", b)
		}
	}

	h, fx := newFixture(t)
	// Every shape is measured, and a shape that fails does not end the run:
	// the whole table is what says which of them are alike and which are not,
	// and a run that stops at the first one hides the rest.
	measured := 0
	for _, form := range forms {
		held, nobodys := form.build(t, h, fx)
		var a, b, again time.Duration
		alike := false
		// The null control first: one body against itself, measured the same
		// way and in the same turns. It is held to the factor of the
		// comparison, so the test can tell "the two are alike" from "this
		// machine cannot tell anything apart today" -- and it is measured at
		// most twice, for the reason given at the same place in
		// TestStartTakesTheSameTimeForAnUnknownName.
		for try := 1; try <= 2 && !alike; try++ {
			a, b, again = timeThemFinish(t, h, form.name, held, nobodys)
			t.Logf("%s: a key the list holds %v, a key nobody holds %v (the same body twice: %v)",
				form.name, a, b, again)
			alike = a <= 2*again && again <= 2*a
		}
		if !alike {
			// Loud, and not red: see unmeasurable. One shape the machine
			// cannot resolve does not take the other twelve down with it, so
			// this is a line and not a skip -- but it IS a line check.sh
			// greps, because this shape's guard did not run.
			t.Logf("%s: %s, the same body twice: %v and %v -- this machine cannot measure a factor of two for this shape today",
				unmeasurable, form.name, a, again)
			continue
		}
		if form.branch != "" {
			continue
		}
		measured++
		if a > 2*b || b > 2*a {
			t.Errorf("%s: a key the list holds %v, a key nobody holds %v (the same body twice: %v)",
				form.name, a, b, again)
		}
	}
	// How many of the five shapes that have to be alike really carried the
	// guard. The number is logged on every run and not only when it is zero:
	// "green" means something different at five than at one.
	t.Logf("%d of the shapes that have to be alike were measured", measured)
	if measured == 0 {
		t.Skipf("%s: not one shape that has to be alike could be measured today -- the guard did not run",
			unmeasurable)
	}
}

// twoBodies builds a body and its twin, which names a key ID nobody holds and
// differs in nothing else. bend changes the proof after it was made, so a
// signature that does not hold or client data that is nonsense reaches the
// handler as the browser would have sent it.
func twoBodies(tb testing.TB, fx fixture, c session.LoginChallenge, s trusttest.Signer,
	bend func(*proof.Proof)) (held, nobodys []byte) {
	tb.Helper()
	payload := certBytes(tb, fx, c, s)
	p := s.Prove(sign.PurposeClientCert, payload)
	if bend != nil {
		bend(&p)
	}
	one := session.LoginFinish{Cert: enc.Bytes(payload), Proof: p}
	other := one
	other.Proof.KeyID = nobodysKeyID
	return mustMarshal(tb, one), mustMarshal(tb, other)
}

func mustMarshal(tb testing.TB, f session.LoginFinish) []byte {
	tb.Helper()
	b, err := json.Marshal(f)
	if err != nil {
		tb.Fatalf("marshal: %v", err)
	}
	return b
}

// timedBatches is how many batches each of the three measurements of a shape
// takes, and how many login/start takes too. The shortest of them is the one
// a busy machine disturbed least (see timeThem); more batches therefore mean a
// cleaner number, not just a steadier one, and five is what fits in the time
// the whole table may take.
const timedBatches = 5

// timeThemFinish measures the two bodies and the first one again, turn by turn
// and not one block after another: a machine that gets slower or faster while
// the test runs does so for all three, where three blocks in a row would hand
// that drift to the comparison as a finding. It is the shape timeThem uses for
// login/start, and it was lost once here -- the numbers it gave were then a
// third block against a first.
//
// It returns the SHORTEST of the batches and no longer the median, for the
// reason timeThem gives: the noise on this clock only ever adds time.
func timeThemFinish(t *testing.T, h *Handler, what string, held, nobodys []byte) (a, b, again time.Duration) {
	t.Helper()
	da := make([]time.Duration, timedBatches)
	db := make([]time.Duration, timedBatches)
	dc := make([]time.Duration, timedBatches)
	for i := range timedBatches {
		da[i] = timedFinish(t, h, what, held)
		db[i] = timedFinish(t, h, what, nobodys)
		dc[i] = timedFinish(t, h, what, held)
	}
	slices.Sort(da)
	slices.Sort(db)
	slices.Sort(dc)
	return da[0], db[0], dc[0]
}

// timedFinish runs timedRuns requests with body and returns what one took. It
// refuses a batch that was not a refused login and one the clock did not see,
// for the reasons timed gives.
func timedFinish(t *testing.T, h *Handler, what string, body []byte) time.Duration {
	t.Helper()
	first := 0
	start := time.Now()
	for i := range timedRuns {
		// The counting is put back to nothing before every request: after
		// three failures the row answers 429 in front of the whole login, and
		// what followed would be the time of two refusals. It costs both
		// bodies the same.
		clearCounting(t, h)
		w := httptest.NewRecorder()
		h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
		if i == 0 {
			first = w.Code
		}
	}
	d := time.Since(start)
	if first != http.StatusUnauthorized {
		t.Fatalf("%s answered %d: what follows would not be the time of a refused login", what, first)
	}
	if d < timedFloor {
		t.Fatalf("%d requests for %s took %v: the clock did not see them", timedRuns, what, d)
	}
	return d / timedRuns
}

// TestFinishLetsNoMoreThroughAtOnceThanOneAfterTheOther: the counting of
// classes/lock gives the first freeCount failures free and then lays a wait, so
// a run of wrong proofs is answered 401 a fixed number of times and 429 after
// that. That number must not depend on how many of them are in flight at the
// same moment.
//
// It did. Measured on the running installation on 17.10.2026 against a real
// ngc-api on Debian 13: one after the other 3 attempts were answered 401 and
// the rest 429, while 20 at once were answered 401 between 7 and 14 times in
// eight runs of 20 - so the free attempts an installation hands out grew with
// the attacker's concurrency instead of standing at freeCount.
//
// The number this is held against is MEASURED in the same test, not written
// down here: the serial run on its own fixture says how many may get through,
// and the burst must not beat it. A number in this file would be a second copy
// of freeCount and would stop saying anything the day that constant moves.
func TestFinishLetsNoMoreThroughAtOnceThanOneAfterTheOther(t *testing.T) {
	const tries = 20
	// through runs tries wrong proofs against a fresh installation and returns
	// how many of them were answered 401, that is: how many were really
	// weighed. atOnce decides whether they are in flight together.
	through := func(t *testing.T, atOnce bool) int {
		t.Helper()
		h, fx := newFixture(t)
		c := start(t, h, fx.PasswordName)
		c.Nonce = madeUpNonce
		body := string(finishBody(t, fx, fx.Password, c, ""))
		codes := make([]int, tries)
		if atOnce {
			// A start barrier, because the window is the stretch between the
			// read of the wait and the write of the count, and that is tens of
			// microseconds wide: goroutines started one by one are through it
			// before the next one begins, and the burst would measure nothing.
			// They are all parked on one channel and let go at once.
			var wg, ready sync.WaitGroup
			start := make(chan struct{})
			wg.Add(tries)
			ready.Add(tries)
			for i := range tries {
				go func() {
					defer wg.Done()
					ready.Done()
					<-start
					w := httptest.NewRecorder()
					h.Finish(w, jsonPost(session.PathLoginFinish, body))
					codes[i] = w.Code
				}()
			}
			ready.Wait()
			close(start)
			wg.Wait()
		} else {
			for i := range tries {
				w := httptest.NewRecorder()
				h.Finish(w, jsonPost(session.PathLoginFinish, body))
				codes[i] = w.Code
			}
		}
		n := 0
		for i, code := range codes {
			switch code {
			case 401:
				n++
			case 429:
			default:
				// Anything else is an earlier wall, and then this test is
				// measuring that one instead of the counting.
				t.Fatalf("atOnce=%v: attempt %d answered %d, which is neither a weighed login nor a wait",
					atOnce, i, code)
			}
		}
		return n
	}
	serial := through(t, false)
	// Without this the test would pass on an installation that answers 429 to
	// everything, where both numbers are 0 and nothing was ever weighed.
	if serial < 1 || serial >= tries {
		t.Fatalf("one after the other %d of %d were weighed; that is no counting to hold a burst against", serial, tries)
	}
	// A race shows itself in the worst of several runs, not in one. The rate
	// belongs in the message, so whoever reads a failure knows how often it
	// was seen and does not take one run for the whole truth.
	burst, hits := 0, 0
	const rounds = 10
	for range rounds {
		if n := through(t, true); n > burst {
			burst = n
		}
		if burst > serial {
			hits++
		}
	}
	if burst > serial {
		t.Errorf("%d of %d wrong proofs were weighed when they came at once, but only %d when they came one after the other (%d of %d rounds over the serial number): the free attempts grow with concurrency",
			burst, tries, serial, hits, rounds)
	}
}
