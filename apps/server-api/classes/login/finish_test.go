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
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
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

// TestFinishTakesTheSameTimeForAKeyNobodyHolds is the question login/start
// answers alike for every name, asked once more with a clock. session.
// VerifyClient turns a proof by a key the list does not hold away before it
// ever checks a signature, where a proof by a key somebody holds costs a whole
// verification -- and login/start hands the key ID of a passkey out, real for a
// name that exists and made up for one nobody has (session.PasskeyRef). The two
// bodies below differ in that one field and in nothing else.
//
// The factor of two and the null control are those of
// TestStartTakesTheSameTimeForAnUnknownName, for the reasons given there. What
// this cannot measure is a database that is not a map: UserGet and OnceUse lie
// on the path of the key the list holds and not on the other, and on a real
// database those are two round trips (see the report of task 7, the difference
// between the four failures).
func TestFinishTakesTheSameTimeForAKeyNobodyHolds(t *testing.T) {
	h, fx := newFixture(t)
	c := start(t, h, fx.PasswordName)
	c.Nonce = madeUpNonce
	held := finishBody(t, fx, fx.Password, c, "")
	nobodys := finishWithKeyID(t, fx, c, nobodysKeyID)
	var a, b, again []time.Duration
	for range 5 {
		a = append(a, timedFinish(t, h, "a key the list holds", held))
		b = append(b, timedFinish(t, h, "a key nobody holds", nobodys))
		again = append(again, timedFinish(t, h, "a key the list holds", held))
	}
	slices.Sort(a)
	slices.Sort(b)
	slices.Sort(again)
	held0, nobodys0, null := a[2], b[2], again[2]
	// The null control first: one body against itself, measured the same way.
	if held0 > 2*null || null > 2*held0 {
		t.Fatalf("the same body twice: %v and %v -- this machine cannot measure a factor of two today",
			held0, null)
	}
	if held0 > 2*nobodys0 || nobodys0 > 2*held0 {
		t.Fatalf("a key the list holds %v, a key nobody holds %v (the same body twice: %v and %v)",
			held0, nobodys0, held0, null)
	}
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
