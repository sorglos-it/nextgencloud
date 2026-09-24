package sqlstore_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/migrate"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlite"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

var ctx = context.Background()

// open gives a test a fresh store on its own encrypted file. The schema comes
// from the migrations, never from a CREATE TABLE in a test. It takes a
// testing.TB and not a *testing.T, because a fuzz target opens its one store
// on the *testing.F.
func open(t testing.TB) dbapi.Store {
	t.Helper()
	s, _ := openDB(t)
	return s
}

// openDB gives the database along with the store, for the tests that read the
// rows back the way the database really holds them instead of through the
// store that wrote them.
func openDB(t testing.TB) (dbapi.Store, *sql.DB) {
	t.Helper()
	cfg := config.DB{
		Driver: config.DriverSQLite,
		Path:   filepath.Join(t.TempDir(), "state.db"),
		Key:    config.NewSecret(bytes.Repeat([]byte{3}, 64)),
	}
	db, err := sqlite.OpenDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Apply(ctx, db, sqlstore.SQLite, "test", 1790000000); err != nil {
		t.Fatal(err)
	}
	s := sqlstore.New(db, sqlstore.SQLite)
	t.Cleanup(func() { s.Close() })
	return s, db
}

func TestBlobRoundTrip(t *testing.T) {
	s := open(t)
	if _, err := s.BlobGet(ctx, dbapi.BlobSetting, "nothing"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("missing blob: %v", err)
	}
	b := dbapi.Blob{Kind: dbapi.BlobTrust, ID: "1", Seq: 1, Data: []byte("v1"), Updated: 1790000000}
	if err := s.BlobPut(ctx, b); err != nil {
		t.Fatal(err)
	}
	// Writing the very same row again must work; it must not turn into a
	// duplicate key, and it must not loop between UPDATE and INSERT.
	if err := s.BlobPut(ctx, b); err != nil {
		t.Fatalf("writing the same row again: %v", err)
	}
	got, err := s.BlobGet(ctx, dbapi.BlobTrust, "1")
	if err != nil || got.Seq != 1 || string(got.Data) != "v1" || got.Updated != 1790000000 {
		t.Fatalf("get: %+v, %v", got, err)
	}
	// Order of BlobFrom: by Seq, then by ID, byte for byte.
	for _, id := range []string{"10", "2", "3"} {
		seq, _ := strconv.ParseUint(id, 10, 64)
		if err := s.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobTrust, ID: id, Seq: seq}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.BlobFrom(ctx, dbapi.BlobTrust, 2)
	if err != nil || len(list) != 3 || list[0].ID != "2" || list[1].ID != "3" || list[2].ID != "10" {
		t.Fatalf("from: %+v, %v", list, err)
	}
}

// TestLimits: both databases refuse the same input, so the conformance tests
// come out the same on MariaDB and on SQLite.
func TestLimits(t *testing.T) {
	s := open(t)
	long := string(bytes.Repeat([]byte("x"), sqlstore.MaxKey+1))
	for name, err := range map[string]error{
		"empty kind":         s.BlobPut(ctx, dbapi.Blob{ID: "a"}),
		"empty id":           s.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting}),
		"id too long":        s.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: long}),
		"sequence too big":   s.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: "a", Seq: 1 << 63}),
		"app without id":     s.AppPut(ctx, dbapi.App{}),
		"grant nobody knows": s.AppPut(ctx, dbapi.App{ID: "org.sorglos.terminal", Grants: []string{"everything"}}),
	} {
		if !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
}

// TestLongKeyNeverReachesTheDatabase: a key longer than the column can hold
// matches no row, and that answer is given in Go. Otherwise the two databases
// would part company over it - SQLite compares any length and finds nothing,
// MariaDB is handed a value past its packet and answers with an error - and a
// bound that is looked at after the work is no bound.
//
// The database is closed first, so the measurement is not "the same answer"
// but "without asking": a key of the right size reaches the closed database
// and comes back as an error, a key past the bound never gets that far.
func TestLongKeyNeverReachesTheDatabase(t *testing.T) {
	s := open(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("x", sqlstore.MaxKey+1)
	if _, err := s.BlobGet(ctx, dbapi.BlobSetting, "a"); err == nil || errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("a key of the right size did not reach the database: %v", err)
	}
	if _, err := s.BlobGet(ctx, dbapi.BlobSetting, long); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("BlobGet: %v, want ErrNotFound", err)
	}
	if _, err := s.BlobGet(ctx, long, "a"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("BlobGet, long kind: %v, want ErrNotFound", err)
	}
	if list, err := s.BlobFrom(ctx, long, 0); err != nil || len(list) != 0 {
		t.Errorf("BlobFrom: %+v, %v", list, err)
	}
	if err := s.BlobDelete(ctx, dbapi.BlobSetting, long); err != nil {
		t.Errorf("BlobDelete: %v", err)
	}
	if _, err := s.AppGet(ctx, long); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("AppGet: %v, want ErrNotFound", err)
	}
	if err := s.AppDelete(ctx, long); err != nil {
		t.Errorf("AppDelete: %v", err)
	}
}

func TestAppRoundTrip(t *testing.T) {
	s := open(t)
	a := dbapi.App{ID: "org.sorglos.terminal", Version: "1.0.0", Store: "global", Node: "ctl-1", Active: true,
		Manifest: []byte(`{"id":"org.sorglos.terminal"}`), SHA256: bytes.Repeat([]byte{1}, 32),
		Grants: []string{"network"}, KeptData: 42, Updated: 1790000000}
	if err := s.AppPut(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := s.AppGet(ctx, a.ID)
	if err != nil || got.Version != a.Version || got.Store != a.Store || got.Node != a.Node ||
		!got.Active || got.KeptData != 42 || !bytes.Equal(got.SHA256, a.SHA256) ||
		len(got.Grants) != 1 || got.Grants[0] != "network" {
		t.Fatalf("get: %+v, %v", got, err)
	}
	a.Active, a.Grants = false, nil
	if err := s.AppPut(ctx, a); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.AppGet(ctx, a.ID); got.Active || len(got.Grants) != 0 {
		t.Errorf("update: %+v", got)
	}
	if err := s.AppDelete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppGet(ctx, a.ID); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
	if err := s.AppDelete(ctx, a.ID); err != nil {
		t.Errorf("deleting twice must be fine: %v", err)
	}
	if list, err := s.AppList(ctx); err != nil || len(list) != 0 {
		t.Errorf("list: %+v, %v", list, err)
	}
}

// TestAppListReadsTheSameRow: AppList and AppGet read the same columns, so
// what one of them brings back has to be what the other does. A list that
// forgot to read active or grants would pass a test that only counts rows.
func TestAppListReadsTheSameRow(t *testing.T) {
	s := open(t)
	apps := []dbapi.App{
		{ID: "org.sorglos.terminal", Version: "1.0.0", Store: "global", Node: "ctl-1", Active: true,
			Manifest: []byte(`{"id":"org.sorglos.terminal"}`), SHA256: bytes.Repeat([]byte{1}, 32),
			Grants: []string{"network", "root"}, KeptData: 42, Updated: 1790000000},
		{ID: "org.sorglos.calculator", Version: "2.0.0", Store: "global", Node: "ctl-2"},
	}
	for _, a := range apps {
		if err := s.AppPut(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.AppList(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %+v, %v", list, err)
	}
	// Ordered by ID, byte for byte.
	if list[0].ID != "org.sorglos.calculator" || list[1].ID != "org.sorglos.terminal" {
		t.Fatalf("order: %q, %q", list[0].ID, list[1].ID)
	}
	for _, a := range list {
		got, err := s.AppGet(ctx, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Version != a.Version || got.Store != a.Store || got.Node != a.Node || got.Active != a.Active ||
			!bytes.Equal(got.Manifest, a.Manifest) || !bytes.Equal(got.SHA256, a.SHA256) ||
			strings.Join(got.Grants, ",") != strings.Join(a.Grants, ",") ||
			got.KeptData != a.KeptData || got.Updated != a.Updated {
			t.Errorf("%s: list %+v, get %+v", a.ID, a, got)
		}
	}
}

// TestWhatStandsInTheDatabase reads the rows the way the database holds them,
// not the way the store reads them back. A round trip through code of ours
// can agree with itself and still have written something else into the row -
// and what MariaDB then makes of it, no test on SQLite would ever see.
//
// Three things are measured: no column is NULL, although the App carries
// nothing but its ID; the same row written twice is one row; and the store
// made nothing of its own - the schema is what the migrations left behind.
//
// The NULL is the one check here that proves nothing on SQLite and cannot
// fail on it: this driver binds a nil []byte as an empty blob, measured.
// MariaDB's binds it as NULL, and every column is NOT NULL, so a row without
// blob() would be written on SQLite and refused on MariaDB. The check stands
// for the database that is not in this test; the one that does measure it
// here is TestEveryByteSliceComesFromBlob, which reads the source.
func TestWhatStandsInTheDatabase(t *testing.T) {
	s, db := openDB(t)
	full := dbapi.App{ID: "org.sorglos.terminal", Version: "1.0.0", Store: "global", Node: "ctl-1",
		Active: true, Manifest: []byte(`{"id":"org.sorglos.terminal"}`), SHA256: bytes.Repeat([]byte{1}, 32),
		Grants: []string{"network", "root"}, KeptData: 42, Updated: 1790000000}
	for _, a := range []dbapi.App{full, {ID: "org.sorglos.empty"}} {
		if err := s.AppPut(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	var version, grants []byte
	var active, kept int64
	if err := db.QueryRowContext(ctx, "SELECT version, active, grants, kept_data FROM ngc_app WHERE id = ?",
		[]byte(full.ID)).Scan(&version, &active, &grants, &kept); err != nil {
		t.Fatal(err)
	}
	// The list is one column with a line feed between the entries, the flag
	// is a number, and both stand in the order they were given.
	if string(version) != "1.0.0" || active != 1 || string(grants) != "network\nroot" || kept != 42 {
		t.Errorf("the row carries %q, %d, %q, %d", version, active, grants, kept)
	}
	var nulls int64
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM ngc_app WHERE version IS NULL OR store IS NULL "+
		"OR node IS NULL OR manifest IS NULL OR sha256 IS NULL OR grants IS NULL").Scan(&nulls); err != nil {
		t.Fatal(err)
	}
	if nulls != 0 {
		t.Errorf("%d rows carry a NULL, and every column is NOT NULL", nulls)
	}
	b := dbapi.Blob{Kind: dbapi.BlobTrust, ID: "1", Seq: 1, Data: []byte("v1"), Updated: 1790000000}
	for range 2 {
		if err := s.BlobPut(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	var rows int64
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM ngc_blob").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("writing the same row twice left %d rows", rows)
	}
	var tables int64
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type = ?", "table").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != int64(len(sqlstore.Tables)) {
		t.Errorf("the database holds %d tables, the fixed list carries %d", tables, len(sqlstore.Tables))
	}
}

// TestARowNobodyOfOursWrote: a column of the store's own database is a value
// from outside like any other. A sequence number below zero would wrap into
// one near 2^64 on the way out, and a grants column longer than joinList ever
// writes would become a list of any size. Both are refused, and the row is
// planted here with SQL of the test's own - the store has no way of writing
// one.
func TestARowNobodyOfOursWrote(t *testing.T) {
	s, db := openDB(t)
	if _, err := db.ExecContext(ctx, "INSERT INTO ngc_blob (kind, id, seq, data, updated) VALUES (?, ?, ?, ?, ?)",
		[]byte(dbapi.BlobTrust), []byte("1"), int64(-1), []byte("v1"), int64(1790000000)); err != nil {
		t.Fatal(err)
	}
	if b, err := s.BlobGet(ctx, dbapi.BlobTrust, "1"); err == nil {
		t.Errorf("BlobGet took a sequence number below zero as %d", b.Seq)
	}
	// BlobFrom never gets that far: it asks for seq >= 0 and up, so the
	// database leaves the row out. That is where the number is measured a
	// second time, and this says why it is never the first.
	if list, err := s.BlobFrom(ctx, dbapi.BlobTrust, 0); err != nil || len(list) != 0 {
		t.Errorf("BlobFrom: %+v, %v", list, err)
	}
	grants := strings.Repeat("root\n", sqlstore.MaxList+1) + "root"
	if _, err := db.ExecContext(ctx, "INSERT INTO ngc_app (id, version, store, node, active, manifest, "+
		"sha256, grants, kept_data, updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		[]byte("org.sorglos.terminal"), []byte(""), []byte(""), []byte(""), int64(0), []byte(""),
		[]byte(""), []byte(grants), int64(0), int64(0)); err != nil {
		t.Fatal(err)
	}
	if a, err := s.AppGet(ctx, "org.sorglos.terminal"); err == nil {
		t.Errorf("AppGet took %d grants out of one column", len(a.Grants))
	}
	if list, err := s.AppList(ctx); err == nil {
		t.Errorf("AppList took the same row: %+v", list)
	}
}

// TestAGrantNobodyKnowsDoesNotComeBackOut: AppPut refuses a grant the
// contract does not know, and the way back out refuses it too. What is
// forbidden while writing may not appear while reading: the list of AppList
// goes on into the interface. The row is planted with SQL of the test's own,
// because the store has no way of writing one.
func TestAGrantNobodyKnowsDoesNotComeBackOut(t *testing.T) {
	s, db := openDB(t)
	bad := dbapi.App{ID: "org.sorglos.terminal", Grants: []string{"\x00"}}
	if err := s.AppPut(ctx, bad); !errors.Is(err, dbapi.ErrInvalid) {
		t.Fatalf("writing it: %v, want ErrInvalid", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO ngc_app (id, version, store, node, active, manifest, "+
		"sha256, grants, kept_data, updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		[]byte(bad.ID), []byte(""), []byte(""), []byte(""), int64(0), []byte(""),
		[]byte(""), []byte("\x00"), int64(0), int64(0)); err != nil {
		t.Fatal(err)
	}
	if a, err := s.AppGet(ctx, bad.ID); err == nil {
		t.Errorf("AppGet handed out the grants %q", a.Grants)
	}
	if list, err := s.AppList(ctx); err == nil {
		t.Errorf("AppList handed out the same row: %+v", list)
	}
}

// TestTheRestIsNotWrittenYet: every method of dbapi.Store that task 7 brings
// answers errors.ErrUnsupported until then, and nothing in here is allowed to
// look like it works. Task 5 took Once... and Lock... out of the list, task 6
// User..., Session... and Pending...; they are measured in once_test.go and
// user_test.go now.
func TestTheRestIsNotWrittenYet(t *testing.T) {
	s := open(t)
	_, logQuery := s.LogQuery(ctx, core.LogQuery{Limit: 1})
	for name, err := range map[string]error{
		"LogAdd":   s.LogAdd(ctx, core.LogEntry{Kind: "app"}),
		"LogQuery": logQuery,
	} {
		if !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
}
