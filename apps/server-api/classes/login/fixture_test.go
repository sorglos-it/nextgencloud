package login

import (
	"bytes"
	"context"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
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
