package state

import (
	"context"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

var ctx = context.Background()

// newState returns a State on a fresh store with a fixed clock. It takes a
// testing.TB so that a fuzz target, which has a *testing.F, builds one per run.
func newState(tb testing.TB) (*State, dbapi.Store) {
	tb.Helper()
	db := memstore.New()
	box, err := secrets.NewBox(config.NewSecret(make([]byte, 32)))
	if err != nil {
		tb.Fatalf("box: %v", err)
	}
	cfg := &config.API{Node: "api-1", SessionLifetime: 43200, IdleTimeout: 1800}
	s := New(cfg, db, box, make([]byte, 32))
	s.Now = func() int64 { return 1790000000 }
	return s, db
}

func TestEnsureInstallation(t *testing.T) {
	s, db := newState(t)
	id, err := s.EnsureInstallation(ctx)
	if err != nil || !trust.ValidID(id) {
		t.Fatalf("installation %q: %v", id, err)
	}
	again, err := s.EnsureInstallation(ctx)
	if err != nil || again != id {
		t.Fatalf("second call made another installation: %q %v", again, err)
	}
	key, err := s.InstallKey(ctx)
	if err != nil || key == nil {
		t.Fatalf("install key: %v", err)
	}
	// The private key never lies in the clear in the database.
	blob, err := db.BlobGet(ctx, dbapi.BlobSetting, dbapi.SettingInstallKey)
	if err != nil {
		t.Fatalf("blob: %v", err)
	}
	if string(blob.Data) == string(key.Bytes()) {
		t.Fatal("install key stored in the clear")
	}
}

// TestInstallationWithoutSetting proves that an installation which was never
// set up answers dbapi.ErrNotFound, which EnsureInstallation tells from a
// broken setting.
func TestInstallationWithoutSetting(t *testing.T) {
	s, _ := newState(t)
	if _, err := s.Installation(ctx); err != dbapi.ErrNotFound {
		t.Fatalf("empty database answered %v", err)
	}
	if _, err := s.InstallKey(ctx); err != dbapi.ErrNotFound {
		t.Fatalf("install key of an empty database answered %v", err)
	}
}

// TestInstallationMalformed proves that a setting which is no ID is refused
// instead of being handed on as one.
func TestInstallationMalformed(t *testing.T) {
	s, db := newState(t)
	if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallation,
		Data: []byte("not-an-id"), Updated: s.Now()}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := s.Installation(ctx); err != ErrSetting {
		t.Fatalf("malformed installation answered %v", err)
	}
}

// TestInstallKeyWrongBox proves that a key sealed with another installation's
// secret key is refused, not used.
func TestInstallKeyWrongBox(t *testing.T) {
	s, _ := newState(t)
	if _, err := s.EnsureInstallation(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	other, err := secrets.NewBox(config.NewSecret(append(make([]byte, 31), 1)))
	if err != nil {
		t.Fatalf("box: %v", err)
	}
	s.Box = other
	if _, err := s.InstallKey(ctx); err != secrets.ErrSealed {
		t.Fatalf("key of another installation answered %v", err)
	}
}
