package sqlstore_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
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
//
// The three blocks after the blob and app lines hold the bounds task 6
// brought: fifteen lines for fourteen checks of user.go, and every one of
// those checks came without a measurement - taken out, not one test of this
// package and no group of the contract went red. Two of the lines fall to one
// check, the joinList of UserPut: joinList refuses four things about a roles
// list - too many entries, an empty entry, an entry too long, a line break
// inside one - and these two lines take two of the four. All four stand in
// TestList, which asks joinList itself, and readList meets them from the
// other side in FuzzReadList; what nothing asked was whether UserPut asks at
// all.
//
// "user without an id" and "pending without a name" are the two of the six
// emptiness checks that nothing held: before these two lines stood here both
// could be taken out of user.go with nothing in this package going red and no
// group of the contract either, while an empty name, an empty token, an empty
// session user and an empty pending key all bite somewhere. The first of them
// carries the ground TestADuplicateNameIsErrInvalid stands on - that UserPut
// writes no row with an empty ID, so such a row has to be planted with SQL of
// the test's own.
//
// On SQLite nothing else would have caught any of them either: it takes a
// value of any length where MariaDB cuts it or answers with an error, which
// is the whole reason these checks are made in Go and not left to the
// database.
//
// The two counts above that name a group of the contract came by hand. When
// they were taken, dbtest.Run had one call site in the whole tree, against
// memstore, and nothing under apps/server-db imported dbtest. Task 7 gave it a
// second one - TestConformance in classes/sqlite, on the store this package
// builds - so go test ./... now drives all eleven groups of the contract
// against it, and a count like those two no longer needs a run made by hand.
// The note at TestOneWriteAndOneDelete in user_test.go says it in full.
func TestLimits(t *testing.T) {
	s := open(t)
	long := string(bytes.Repeat([]byte("x"), sqlstore.MaxKey+1))
	longKey := bytes.Repeat([]byte{1}, sqlstore.MaxKey+1)
	// One document past the bound, shared by the five {{doc}} columns that a
	// caller fills: a megabyte is worth allocating once.
	longDoc := bytes.Repeat([]byte{2}, sqlstore.MaxDoc+1)
	key := bytes.Repeat([]byte{3}, 32)
	manyRoles := make([]string, sqlstore.MaxList+1)
	for i := range manyRoles {
		manyRoles[i] = "admin"
	}
	// One {{text}} column past its bound, and a log ID that is valid, so each
	// log row below fails on the one field it is about and on nothing else.
	longText := string(bytes.Repeat([]byte("y"), sqlstore.MaxText+1))
	entry := core.LogEntry{ID: strings.Repeat("ab", 16), Time: 1790000001, Kind: "login", Result: "ok"}
	withUser := entry
	withUser.User = long
	withKind := entry
	withKind.Kind = long
	withApp := entry
	withApp.App = long
	withAction := entry
	withAction.Action = long
	withNode := entry
	withNode.Node = long
	withResult := entry
	withResult.Result = long
	withDetail := entry
	withDetail.Detail = longText
	// LogQuery answers with a page as well, so its refusal is taken apart
	// from the page here and put in the table below like any other.
	_, queryUser := s.LogQuery(ctx, core.LogQuery{Limit: 1, User: long})
	// One past sqlstore.MaxPage. The contract draws the same line in
	// core.LogQuery.Validate, and this store draws it again; the comment at
	// LogQuery says why both are there. Measured before the check existed:
	// this store and memstore both answered <nil> here, while
	// core.LogQuery.Validate already answered "core: invalid parameters".
	_, queryLimit := s.LogQuery(ctx, core.LogQuery{Limit: sqlstore.MaxPage + 1})
	for name, err := range map[string]error{
		"empty kind":         s.BlobPut(ctx, dbapi.Blob{ID: "a"}),
		"empty id":           s.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting}),
		"id too long":        s.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: long}),
		"sequence too big":   s.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: "a", Seq: 1 << 63}),
		"app without id":     s.AppPut(ctx, dbapi.App{}),
		"grant nobody knows": s.AppPut(ctx, dbapi.App{ID: "org.sorglos.terminal", Grants: []string{"everything"}}),

		"user without an id":  s.UserPut(ctx, dbapi.User{Name: "admin"}),
		"user id too long":    s.UserPut(ctx, dbapi.User{ID: long, Name: "admin"}),
		"user name too long":  s.UserPut(ctx, dbapi.User{ID: "u-admin", Name: long}),
		"totp too long":       s.UserPut(ctx, dbapi.User{ID: "u-admin", Name: "admin", TOTP: longDoc}),
		"too many roles":      s.UserPut(ctx, dbapi.User{ID: "u-admin", Name: "admin", Roles: manyRoles}),
		"role with a newline": s.UserPut(ctx, dbapi.User{ID: "u-admin", Name: "admin", Roles: []string{"ad\nmin"}}),

		"token too long":    s.SessionPut(ctx, dbapi.Session{Token: longKey, User: "u-admin"}),
		"session user long": s.SessionPut(ctx, dbapi.Session{Token: key, User: long}),
		"cert too long":     s.SessionPut(ctx, dbapi.Session{Token: key, User: "u-admin", Cert: longDoc}),
		"proof too long":    s.SessionPut(ctx, dbapi.Session{Token: key, User: "u-admin", Proof: longDoc}),
		"api key too long":  s.SessionPut(ctx, dbapi.Session{Token: key, User: "u-admin", APIKey: longDoc}),
		"api cert too long": s.SessionPut(ctx, dbapi.Session{Token: key, User: "u-admin", APICert: longDoc}),

		"pending without a name": s.PendingPut(ctx, dbapi.Pending{Key: key}),
		"pending name too long":  s.PendingPut(ctx, dbapi.Pending{Name: long, Key: key}),
		"pending key too long":   s.PendingPut(ctx, dbapi.Pending{Name: "web", Key: longKey}),

		"log user too long":                   s.LogAdd(ctx, withUser),
		"log kind too long":                   s.LogAdd(ctx, withKind),
		"log app too long":                    s.LogAdd(ctx, withApp),
		"log action too long":                 s.LogAdd(ctx, withAction),
		"log node too long":                   s.LogAdd(ctx, withNode),
		"log result too long":                 s.LogAdd(ctx, withResult),
		"log detail too long":                 s.LogAdd(ctx, withDetail),
		"log query user too long":             queryUser,
		"log query limit over the page bound": queryLimit,
	} {
		if !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
}

// TestTheLargestPageIsStillAPage holds the far side of the bound LogQuery
// draws, and that the bound sits on the same number as the contract's.
//
// Only "one more than MaxPage" was held before this test: with the bound
// written ">=" instead of ">" nothing in the tree went red, and a caller asking
// for the largest page the contract allows would have been told its query is
// invalid. So both sides are asked here - MaxPage goes through, MaxPage+1 does
// not - and core.LogQuery.Validate is asked the same two, because the point of
// writing the number twice is that the two lines sit on top of each other. If
// the contract ever moves its 500, this test is what says so instead of the two
// quietly drifting apart.
func TestTheLargestPageIsStillAPage(t *testing.T) {
	s := open(t)
	e := core.LogEntry{ID: strings.Repeat("5c", 16), Time: 1790000030, Kind: "login", Result: "ok"}
	if err := s.LogAdd(ctx, e); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LogQuery(ctx, core.LogQuery{Limit: sqlstore.MaxPage}); err != nil || len(got) != 1 {
		t.Errorf("a page of %d: %d entries, %v; want 1 and no error", sqlstore.MaxPage, len(got), err)
	}
	if _, err := s.LogQuery(ctx, core.LogQuery{Limit: sqlstore.MaxPage + 1}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("a page of %d: %v, want ErrInvalid", sqlstore.MaxPage+1, err)
	}
	allowed := core.LogQuery{Limit: sqlstore.MaxPage}
	refused := core.LogQuery{Limit: sqlstore.MaxPage + 1}
	if allowed.Validate() != nil || refused.Validate() == nil {
		t.Errorf("the contract draws its line elsewhere: %d gives %v, %d gives %v",
			sqlstore.MaxPage, allowed.Validate(), sqlstore.MaxPage+1, refused.Validate())
	}
}

// TestEverySixLogStatementsSortByTimeThenID holds both halves of the order the
// contract asks for - (Time, ID) descending - on all six reading statements.
//
// Two of the twelve halves were measured missing when this test was first
// written, and four more when it was reviewed, because the first version of it
// claimed more than it held: it said "holds the two halves ... on the two
// statements", and for logUserTime only the ID half was reachable, because its
// one query got a single second back. The same overclaim went out in the
// message of 42ba937, which cannot be corrected where it stands; the report of
// this task says so.
//
// What the contract's own log group leaves open, each measured green with the
// mutation named:
//
//   - logAll, both halves: it drives logAll only while every entry sits in a
//     second of its own, so "id ASC" changes nothing there.
//   - logUserTime, both halves: it never asks for a user together with a time
//     and no cursor, so that statement is not reached by it at all - "ts > ?",
//     "ts ASC", "id ASC" and dropping the user from the WHERE all stayed green.
//   - logUser and logUserBefore, the time half: it asks those two only inside
//     one second (the five u-bob entries) or for a single row (u-alice), so
//     "ts ASC" stayed green in both.
//   - logTime, the ID half: it asks that one only with rows that each sit in a
//     second of their own, so "id ASC" stayed green.
//
// Held there, and not repeated here for their own sake: "id ASC" in logUser,
// logUserBefore and logBefore, "ts ASC" in logBefore, and "ts <= ?" in
// logTime each turn the group red.
//
// So the fixture spans three seconds and gives both users more than one entry
// in more than one second. Every query below gets rows from at least two
// seconds and at least two rows inside one second, which is what makes both
// halves visible at once: get either of them wrong and the sequence changes.
func TestEverySixLogStatementsSortByTimeThenID(t *testing.T) {
	s := open(t)
	// Three seconds, newest first. t1 is the oldest.
	const t3, t2, t1 = 1790000030, 1790000020, 1790000010
	id := func(pair string) string { return strings.Repeat(pair, 16) }
	// Byte order of the IDs, ascending: 11 < 22 < 55 < 88 < cc < dd < ee < ff.
	// Written in an order that is neither the one asked for nor its reverse, so
	// a store that hands rows back as they were inserted fails as well.
	for _, e := range []core.LogEntry{
		{ID: id("22"), Time: t1, User: "u-one", Kind: "login", Result: "ok"},
		{ID: id("ee"), Time: t3, User: "u-two", Kind: "login", Result: "ok"},
		{ID: id("dd"), Time: t2, User: "u-one", Kind: "login", Result: "ok"},
		{ID: id("ff"), Time: t2, User: "u-two", Kind: "login", Result: "ok"},
		{ID: id("55"), Time: t1, User: "u-one", Kind: "login", Result: "ok"},
		{ID: id("11"), Time: t3, User: "u-one", Kind: "login", Result: "ok"},
		{ID: id("cc"), Time: t1, User: "u-two", Kind: "login", Result: "ok"},
		{ID: id("88"), Time: t2, User: "u-one", Kind: "login", Result: "ok"},
	} {
		if err := s.LogAdd(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	logIDs := func(entries []core.LogEntry) []string {
		out := make([]string, len(entries))
		for i, e := range entries {
			out[i] = e.ID
		}
		return out
	}
	// One case per statement, named after the statement it reaches, so a red
	// line says which constant is wrong.
	for _, c := range []struct {
		stmt string
		q    core.LogQuery
		want []string
	}{
		{"logAll", core.LogQuery{Limit: 10},
			[]string{id("ee"), id("11"), id("ff"), id("dd"), id("88"), id("cc"), id("55"), id("22")}},
		{"logTime", core.LogQuery{Limit: 10, BeforeTime: t3},
			[]string{id("ff"), id("dd"), id("88"), id("cc"), id("55"), id("22")}},
		{"logBefore", core.LogQuery{Limit: 10, BeforeTime: t2, BeforeID: id("ff")},
			[]string{id("dd"), id("88"), id("cc"), id("55"), id("22")}},
		{"logUser", core.LogQuery{Limit: 10, User: "u-one"},
			[]string{id("11"), id("dd"), id("88"), id("55"), id("22")}},
		{"logUserTime", core.LogQuery{Limit: 10, User: "u-one", BeforeTime: t3},
			[]string{id("dd"), id("88"), id("55"), id("22")}},
		{"logUserBefore", core.LogQuery{Limit: 10, User: "u-one", BeforeTime: t2, BeforeID: id("dd")},
			[]string{id("88"), id("55"), id("22")}},
	} {
		got, err := s.LogQuery(ctx, c.q)
		if err != nil || !slices.Equal(logIDs(got), c.want) {
			t.Errorf("%s: %v, %v; want %v", c.stmt, logIDs(got), err, c.want)
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
