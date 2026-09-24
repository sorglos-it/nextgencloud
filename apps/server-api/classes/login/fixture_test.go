package login

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
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
	noise, signKey := trusttest.NodeKeys(1)
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

func (c *callLog) LogAdd(ctx context.Context, e core.LogEntry) error {
	c.add("LogAdd")
	return c.Store.LogAdd(ctx, e)
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
