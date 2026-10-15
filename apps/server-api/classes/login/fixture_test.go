package login

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/pquerna/otp/totp"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbwait"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

var ctx = context.Background()

// testNow is the fixed time of every login test.
const testNow = int64(1790000000)

// fixture is the installation the login tests work against: trusttest.Genesis
// holds two admins sorted by ID, Users[0] with a passkey and Users[1] with a
// password key (CLAUDE.md, "Tests").
type fixture struct {
	Installation string
	Passkey      trusttest.Signer
	PasskeyUser  string
	PasskeyName  string
	Password     trusttest.Signer
	PasswordUser string
	PasswordName string
	List         *trust.List
	Signed       trust.Signed
}

// newFixture takes testing.TB and not *testing.T so that a fuzz target can
// build its one handler from *testing.F; building one per body would cost an
// Ed25519 verification of the whole trust list for every run.
func newFixture(tb testing.TB) (*Handler, fixture) {
	tb.Helper()
	box, err := secrets.NewBox(config.NewSecret(make([]byte, 32)))
	if err != nil {
		tb.Fatalf("box: %v", err)
	}
	// The keys of "api-1" in trusttest.Genesis, not keys of their own: the API
	// certificate this node signs has to verify against the node of that name
	// in the trust list (session.VerifyAPI), and a key nobody there holds
	// would leave every session unverifiable.
	noise, signKey := trusttest.NodeKeys(0xA1)
	cfg := &config.API{
		Node: "api-1", URL: "wss://api-1.ngc.test:7443/tunnel", Listen: ":7443",
		Cert: "/x", CertKey: "/y",
		NoiseKey: config.NewSecret(noise.Private), SignKey: config.NewSecret(signKey.Seed()),
		SecretKey: config.NewSecret(make([]byte, 32)),
		// Its own key, not SecretKey: session.Challenge draws the made-up
		// answer from it, and the same key in every API node makes all of them
		// answer one name alike.
		ChallengeKey: config.NewSecret(bytes.Repeat([]byte{7}, 32)),
		ReleaseKey:   enc.Bytes(make([]byte, 32)), StoreKey: enc.Bytes(make([]byte, 32)),
		StoreURL: "https://store.ngc.test", SessionLifetime: 43200, IdleTimeout: 1800,
	}
	s := state.New(cfg, memstore.New(), box, make([]byte, 32))
	s.Now = func() int64 { return testNow }

	// trusttest.Genesis takes the signer of Users[1] and gives Users[0] its
	// own passkey (trusttest.GenesisPasskeyFill). The password admin has to be
	// the one with the password key, so NewSigner(1) goes in -- a passkey here
	// would leave the list without any password key at all, and every KDF of
	// these tests would be a made-up one.
	password := trusttest.NewSigner(1)
	l := trusttest.Genesis(password)
	signed := password.Sign(tb, l)
	// Version 1 is rooted in the installation ID: the setup writes it before
	// version 1 is signed, and state.AddVersion takes no version that names
	// another one (checkInstallation). So the row goes in first;
	// EnsureInstallation would roll an ID of its own, which no version of
	// trusttest names.
	if err := s.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallation,
		Data: []byte(l.Installation), Updated: testNow}); err != nil {
		tb.Fatalf("installation: %v", err)
	}
	if _, err := s.AddVersion(ctx, signed); err != nil {
		tb.Fatalf("version 1: %v", err)
	}
	fx := fixture{
		Installation: l.Installation,
		Passkey:      trusttest.NewPasskey(trusttest.GenesisPasskeyFill),
		PasskeyUser:  l.Users[0].ID, PasskeyName: l.Users[0].Name,
		Password: password, PasswordUser: l.Users[1].ID, PasswordName: l.Users[1].Name,
		List: l, Signed: signed,
	}
	// Every user of the trust list also has a row, which holds what the list
	// does not: TOTP secret, failures, last login.
	for _, u := range l.Users {
		if err := s.DB.UserPut(ctx, dbapi.User{ID: u.ID, Name: u.Name, Roles: u.Roles}); err != nil {
			tb.Fatalf("user %s: %v", u.ID, err)
		}
	}
	return New(s), fx
}

// zeroReader gives a fixed session key, so the tests stay deterministic; the
// API itself always uses crypto/rand.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 7
	}
	return len(p), nil
}

// sigPub returns the Ed25519 key the session signs tasks with. The tests reuse
// the user's own key, which is enough: the API only checks that the proof over
// the certificate holds.
func sigPub(s trusttest.Signer) []byte { return s.Priv.Public().(ed25519.PublicKey) }

// codeOf returns the TOTP code of secret at testNow.
func codeOf(secret string) (string, error) { return totp.GenerateCode(secret, timeOf(testNow)) }

// failLogin counts one failed attempt for name, as login/finish does.
func failLogin(t *testing.T, h *Handler, name string) {
	t.Helper()
	if err := lock.Fail(ctx, h.S.DB, name, h.S.Now()); err != nil {
		t.Fatalf("fail: %v", err)
	}
}

// The databases below stand for the states a real one gets into. Each wraps
// the fixture's store and changes one call, so everything else still works and
// a request runs to its end.

// callLog writes down every call a request makes, in order. It is how the path
// of a known name is held against the path of one nobody has without a clock:
// a clock of 1 ms resolution sees nothing of a request of 30 µs, a list of
// calls sees everything.
type callLog struct {
	dbapi.Store
	mu sync.Mutex
	in []string
}

func (c *callLog) add(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.in = append(c.in, s)
}

// list returns what was called since the last reset.
func (c *callLog) list() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.in)
}

func (c *callLog) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.in = nil
}

func (c *callLog) BlobGet(ctx context.Context, kind, id string) (dbapi.Blob, error) {
	c.add("BlobGet " + kind)
	return c.Store.BlobGet(ctx, kind, id)
}

// UserGet names an empty ID apart, because that is the call this must catch:
// a name nobody has has no user and therefore no ID, so a lookup by the ID
// asks with an empty one -- which is what it did before, and what told the two
// names apart on any store that refuses it.
func (c *callLog) UserGet(ctx context.Context, id string) (dbapi.User, error) {
	if id == "" {
		c.add("UserGet(empty id)")
	} else {
		c.add("UserGet")
	}
	return c.Store.UserGet(ctx, id)
}

func (c *callLog) UserByName(ctx context.Context, name string) (dbapi.User, error) {
	c.add("UserByName")
	return c.Store.UserByName(ctx, name)
}

func (c *callLog) UserList(ctx context.Context) ([]dbapi.User, error) {
	c.add("UserList")
	return c.Store.UserList(ctx)
}

func (c *callLog) OnceCreate(ctx context.Context, kind string, value []byte, data string, expires int64) error {
	c.add("OnceCreate " + kind)
	return c.Store.OnceCreate(ctx, kind, value, data, expires)
}

func (c *callLog) OnceUse(ctx context.Context, kind string, value []byte, now int64) (string, error) {
	c.add("OnceUse " + kind)
	return c.Store.OnceUse(ctx, kind, value, now)
}

func (c *callLog) LogAdd(ctx context.Context, e core.LogEntry) error {
	c.add("LogAdd")
	return c.Store.LogAdd(ctx, e)
}

// trustReads counts what the trust rows are asked, both ways a look at them
// asks: row by row and the one query that walks above the newest
// (state.checkGap). Both are counted because either alone would let a change
// in package state move cost from one to the other without a test noticing.
// What one look costs is still none of this package's business: what its tests
// assert is how many looks there were.
type trustReads struct {
	dbapi.Store
	mu sync.Mutex
	n  int
}

func (c *trustReads) count() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *trustReads) BlobGet(ctx context.Context, kind, id string) (dbapi.Blob, error) {
	if kind == dbapi.BlobTrust {
		c.count()
	}
	return c.Store.BlobGet(ctx, kind, id)
}

func (c *trustReads) BlobFrom(ctx context.Context, kind string, seq uint64) ([]dbapi.Blob, error) {
	if kind == dbapi.BlobTrust {
		c.count()
	}
	return c.Store.BlobFrom(ctx, kind, seq)
}

// take returns what was read since the last take and starts counting again.
func (c *trustReads) take() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.n
	c.n = 0
	return n
}

// sessionWatch says whether a session was ever written. It is the canary of
// FuzzFinish: it is asked after every single body, and no body the fuzzer can
// build steers around it -- a session is written here or nowhere.
type sessionWatch struct {
	dbapi.Store
	mu   sync.Mutex
	seen bool
}

func (s *sessionWatch) SessionPut(ctx context.Context, row dbapi.Session) error {
	s.mu.Lock()
	s.seen = true
	s.mu.Unlock()
	return s.Store.SessionPut(ctx, row)
}

// made reports whether a session was written since this store was wrapped.
func (s *sessionWatch) made() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen
}

// clearCounting removes what the counting of failed logins left behind. A day
// past the clock of these tests is past the keep time of every row they write,
// and lock.Purge touches its own rows and no other setting.
func clearCounting(tb testing.TB, h *Handler) {
	tb.Helper()
	if err := lock.Purge(ctx, h.S.DB, testNow+24*3600); err != nil {
		tb.Fatalf("purge: %v", err)
	}
}

// noOnce reads but cannot write a one-time value: a full disk, a read replica,
// a locked table, a quota that ran out. Its error stands for the one a real
// driver hands back, which may quote the values of the statement -- here the
// nonce and the user ID -- and which therefore never reaches the log.
type noOnce struct{ dbapi.Store }

const noOnceError = "memstore: the one-time values cannot be written"

func (noOnce) OnceCreate(context.Context, string, []byte, string, int64) error {
	return errors.New(noOnceError)
}

// noCount reads but cannot write a setting. The counting of failed logins
// keeps its buckets among the settings (classes/lock), so with this store no
// failure is counted at all and nothing holds up guessing. Its error stands
// for the one a real driver hands back, which may quote the values of the
// statement -- here the bucket of a login name and the count -- and which
// therefore never reaches the log.
type noCount struct{ dbapi.Store }

const noCountError = "memstore: the settings cannot be written"

func (noCount) BlobPut(context.Context, dbapi.Blob) error { return errors.New(noCountError) }

// The four below take one step of Live, or the one step past it the logout
// takes, away from it. Each stands for a database that is gone, or gone for
// one table, and their error stands for the one a real driver hands back. None
// of the store's own three words is among them: those say something about the
// row, and these say nobody answered.

const storeDownError = "memstore: the database is not answering"

// noSession cannot read a session row.
type noSession struct{ dbapi.Store }

func (noSession) SessionGet(context.Context, []byte) (dbapi.Session, error) {
	return dbapi.Session{}, errors.New(storeDownError)
}

// noTrust cannot read a row of the trust list, neither one by one nor through
// the query that walks above the newest (state.checkGap). It leaves every
// other row alone, so a request gets as far as the list and no further.
type noTrust struct{ dbapi.Store }

func (n noTrust) BlobGet(ctx context.Context, kind, id string) (dbapi.Blob, error) {
	if kind == dbapi.BlobTrust {
		return dbapi.Blob{}, errors.New(storeDownError)
	}
	return n.Store.BlobGet(ctx, kind, id)
}

func (n noTrust) BlobFrom(ctx context.Context, kind string, seq uint64) ([]dbapi.Blob, error) {
	if kind == dbapi.BlobTrust {
		return nil, errors.New(storeDownError)
	}
	return n.Store.BlobFrom(ctx, kind, seq)
}

// noTouch cannot write the time a session was last seen, which is the last
// step of Live and the only one of the three that comes after the session has
// been decided on.
type noTouch struct{ dbapi.Store }

func (noTouch) SessionTouch(context.Context, []byte, int64) error {
	return errors.New(storeDownError)
}

// noDelete cannot remove a session row. It is the second of the two steps the
// logout takes, and the one no test reached before: a logout that answered
// 204 over it would tell its owner a session was ended that is still open.
type noDelete struct{ dbapi.Store }

func (noDelete) SessionDelete(context.Context, []byte) error {
	return errors.New(storeDownError)
}

// The three below answer with the store's own words instead of an error of a
// driver, and that is the whole difference to the four above: each of them is
// an answer about the row, and the contract's table has an answer of its own
// for it (login/live.go, sessionFault).

// touchGone loses the session row between the read and the touch: a logout of
// the same cookie on another node, or the housekeeping round, at that moment.
type touchGone struct{ dbapi.Store }

func (touchGone) SessionTouch(context.Context, []byte, int64) error { return dbapi.ErrNotFound }

// deleteGone loses the session row before the logout removes it: the same two
// neighbours, one step further on.
type deleteGone struct{ dbapi.Store }

func (deleteGone) SessionDelete(context.Context, []byte) error { return dbapi.ErrNotFound }

// oddGet calls the session read a wrong call. Nothing this package sends can
// earn it -- what reaches SessionGet is session.TokenHash of session.TokenSize
// random bytes this API drew itself -- so it is the installation and never
// the session, and no waiting mends it.
// Nothing in the contract forbids a store from saying it: dbapi.ErrInvalid is
// one of the three words dbapi gives its stores, and dbapi.Store.SessionGet
// carries no doc comment that rules it out.
type oddGet struct{ dbapi.Store }

func (oddGet) SessionGet(context.Context, []byte) (dbapi.Session, error) {
	return dbapi.Session{}, dbapi.ErrInvalid
}

// strictID refuses an empty user ID. Nothing in the contract says it may not:
// dbapi/dbtest pins UserGet only for a name nobody has, never for the empty ID,
// and memstore refuses an empty ID in UserPut with dbapi.ErrInvalid, so a store
// that refuses it here too is within the contract. A login that asks with an
// empty ID would then answer one thing for a known name and another for every
// other.
type strictID struct{ dbapi.Store }

func (s strictID) UserGet(ctx context.Context, id string) (dbapi.User, error) {
	if id == "" {
		return dbapi.User{}, dbapi.ErrInvalid
	}
	return s.Store.UserGet(ctx, id)
}

// oddMiss answers a name it does not hold with an error of its own instead of
// dbapi.ErrNotFound. Nothing in the contract forbids it: dbapi.Store.UserByName
// carries no doc comment at all, and dbapi/dbtest pins it only for a name that
// exists -- there is no negative case. A login that turned this into an answer
// of its own would tell known names from unknown ones again.
type oddMiss struct{ dbapi.Store }

func (o oddMiss) UserByName(ctx context.Context, name string) (dbapi.User, error) {
	u, err := o.Store.UserByName(ctx, name)
	if errors.Is(err, dbapi.ErrNotFound) {
		return dbapi.User{}, errors.New("server-db: unknown user")
	}
	return u, err
}

// capturedLog sends what the API says out loud into a buffer, for this test
// only. No test of this package runs in parallel, so the default logger is
// this test's alone while it runs.
func capturedLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	before := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(before) })
	return &buf
}

// loginOnce runs the whole login and returns the cookie value.
func loginOnce(t testing.TB, h *Handler, fx fixture) string {
	t.Helper()
	c := start(t, h, fx.PasswordName)
	w := httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(finishBody(t, fx, fx.Password, c, ""))))
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies %+v", cookies)
	}
	return cookies[0].Value
}

// getCookie builds a GET with the session cookie.
func getCookie(path, cookie string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	return r
}

// jsonPostCookie builds a POST with the session cookie and no body.
func jsonPostCookie(path, cookie string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	return r
}

// busyOnce reads but cannot write a one-time value because another program
// holds the database: the one write failure that is a state of the
// INSTALLATION and never of the row handed in. Its error carries
// dbwait.ErrBusy the way package sqlstore hands the named cause of the
// deadline back (point 49), which is the only thing login/start reads it for.
type busyOnce struct{ dbapi.Store }

func (busyOnce) OnceCreate(context.Context, string, []byte, string, int64) error {
	return fmt.Errorf("sqlstore: the write did not get through: %w", dbwait.ErrBusy)
}

// seeOnce remembers what was written beside the nonce, so a test can hold the
// ID of a made-up challenge against the users that exist.
type seeOnce struct {
	dbapi.Store
	mu   sync.Mutex
	data []string
}

func (s *seeOnce) OnceCreate(ctx context.Context, kind string, value []byte, data string, expires int64) error {
	s.mu.Lock()
	s.data = append(s.data, data)
	s.mu.Unlock()
	return s.Store.OnceCreate(ctx, kind, value, data, expires)
}

func (s *seeOnce) written() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data)
}
