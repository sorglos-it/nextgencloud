package state

import (
	"context"
	"errors"
	"path/filepath"
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
	// A path of this run that nothing wrote: so no case below depends on
	// whether the machine it runs on happens to carry config.AnchorFile, and
	// a case that WANTS an anchor writes this one.
	s.AnchorFile = filepath.Join(tb.TempDir(), "trust-anchor.json")
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

// TestEnsureInstallationBusy proves that an API which does not get the lock
// hears that waiting helps, and not that a setting is broken.
func TestEnsureInstallationBusy(t *testing.T) {
	s, db := newState(t)
	now := s.Now()
	ok, err := db.LockTake(ctx, installLock, "api-2", now, now+installLease)
	if err != nil || !ok {
		t.Fatalf("lock: %v", err)
	}
	if _, err := s.EnsureInstallation(ctx); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy lock answered %v", err)
	}
}

// TestInstallationWithoutSetting proves that an installation which was never
// set up answers dbapi.ErrNotFound, which EnsureInstallation tells from a
// broken setting.
func TestInstallationWithoutSetting(t *testing.T) {
	s, _ := newState(t)
	if _, err := s.Installation(ctx); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("empty database answered %v", err)
	}
	if _, err := s.InstallKey(ctx); !errors.Is(err, dbapi.ErrNotFound) {
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
	if _, err := s.Installation(ctx); !errors.Is(err, ErrSetting) {
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
	if _, err := s.InstallKey(ctx); !errors.Is(err, secrets.ErrSealed) {
		t.Fatalf("key of another installation answered %v", err)
	}
}

// TestEnsureInstallationKeepsTheRoot proves that a missing installation ID is
// not rolled anew while a version 1 is stored. Version 1 carries the old ID
// and can never be signed again, so a fresh ID would answer every later
// Refresh with ErrInstallation for good -- one lost row would cost the
// installation its trust list.
func TestEnsureInstallationKeepsTheRoot(t *testing.T) {
	s, db := newState(t)
	if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobTrust, ID: "1", Seq: 1,
		Data: genesisSeed(t), Updated: s.Now()}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := s.EnsureInstallation(ctx); !errors.Is(err, ErrSetting) {
		t.Fatalf("missing ID next to version 1 answered %v", err)
	}
	if _, err := db.BlobGet(ctx, dbapi.BlobSetting, dbapi.SettingInstallation); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("wrote an installation ID although version 1 is stored: %v", err)
	}
}

// lateSetting hands out a malformed installation ID from the second look on.
// It stands for another API writing the row between the two looks
// EnsureInstallation takes -- the only way the second one sees anything but
// what the first one saw.
type lateSetting struct {
	dbapi.Store
	seen int
}

func (l *lateSetting) BlobGet(ctx context.Context, kind, id string) (dbapi.Blob, error) {
	if kind == dbapi.BlobSetting && id == dbapi.SettingInstallation {
		if l.seen++; l.seen > 1 {
			return dbapi.Blob{Kind: kind, ID: id, Data: []byte("not-an-id")}, nil
		}
	}
	return l.Store.BlobGet(ctx, kind, id)
}

// TestEnsureInstallationBrokenID proves that a broken ID is refused under the
// lock too, instead of being written over with a fresh one: whatever the old
// ID was, a version 1 signed with it may already exist somewhere.
func TestEnsureInstallationBrokenID(t *testing.T) {
	s, db := newState(t)
	s.DB = &lateSetting{Store: db}
	if _, err := s.EnsureInstallation(ctx); !errors.Is(err, ErrSetting) {
		t.Fatalf("broken ID under the lock answered %v", err)
	}
	if _, err := db.BlobGet(ctx, dbapi.BlobSetting, dbapi.SettingInstallation); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("rolled a new installation over the broken one: %v", err)
	}
}
