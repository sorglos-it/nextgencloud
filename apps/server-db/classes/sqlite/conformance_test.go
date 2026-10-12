package sqlite_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlite"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/dbtest"
)

// TestConformance runs the contract's own tests, the same ones MariaDB has to
// pass. Every subtest gets a fresh, empty file, and the schema comes from the
// migrations.
func TestConformance(t *testing.T) {
	dbtest.Run(t, func(t *testing.T) dbapi.Store {
		cfg := config.DB{
			Driver: config.DriverSQLite,
			Path:   filepath.Join(t.TempDir(), "state.db"),
			Key:    config.NewSecret(bytes.Repeat([]byte{9}, 64)),
		}
		s, err := sqlite.Open(cfg, "test", 1790000000)
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}

// TestLocal holds the promise that the controller only ever needs dbapi.Local
// from this package.
func TestLocal(t *testing.T) {
	cfg := config.DB{
		Driver: config.DriverSQLite,
		Path:   filepath.Join(t.TempDir(), "state.db"),
		Key:    config.NewSecret(bytes.Repeat([]byte{9}, 64)),
	}
	s, err := sqlite.Open(cfg, "test", 1790000000)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var local dbapi.Local = s
	if err := local.BlobPut(context.Background(), dbapi.Blob{Kind: dbapi.BlobTrust, ID: "1", Seq: 1}); err != nil {
		t.Error(err)
	}
}
