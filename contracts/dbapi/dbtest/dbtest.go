// Package dbtest checks an implementation of dbapi against the contract.
// Every implementation in db/ has to pass Run, against MariaDB and SQLite.
package dbtest

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/core"
	"github.com/sorglos-it/nextgencloud/contracts/dbapi"
)

var ctx = context.Background()

// Run checks a store. open returns a fresh, empty store for every subtest.
func Run(t *testing.T, open func(t *testing.T) dbapi.Store) {
	t.Helper()
	tests := map[string]func(*testing.T, dbapi.Store){
		"blobs": testBlobs, "apps": testApps, "once": testOnce, "users": testUsers,
		"sessions": testSessions, "pending": testPending, "log": testLog, "locks": testLocks,
		"aliasing": testAliasing, "concurrent once": testConcurrentOnce, "concurrent lock": testConcurrentLock,
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) {
			s := open(t)
			t.Cleanup(func() {
				if err := s.Close(); err != nil {
					t.Errorf("close: %v", err)
				}
			})
			fn(t, s)
		})
	}
}

func testBlobs(t *testing.T, s dbapi.Store) {
	if _, err := s.BlobGet(ctx, dbapi.BlobSetting, "nothing"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("missing blob: %v", err)
	}
	put := func(kind, id string, seq uint64, data string) {
		t.Helper()
		if err := s.BlobPut(ctx, dbapi.Blob{Kind: kind, ID: id, Seq: seq, Data: []byte(data), Updated: 1790000000}); err != nil {
			t.Fatalf("put %s/%s: %v", kind, id, err)
		}
	}
	put(dbapi.BlobSetting, dbapi.SettingInstallation, 0, "inst-1")
	put(dbapi.BlobTrust, "1", 1, "v1")
	put(dbapi.BlobTrust, "2", 2, "v2")
	put(dbapi.BlobTrust, "3", 3, "v3")
	b, err := s.BlobGet(ctx, dbapi.BlobSetting, dbapi.SettingInstallation)
	if err != nil || string(b.Data) != "inst-1" || b.Updated != 1790000000 {
		t.Fatalf("get: %+v, %v", b, err)
	}
	put(dbapi.BlobSetting, dbapi.SettingInstallation, 0, "inst-2")
	if b, _ := s.BlobGet(ctx, dbapi.BlobSetting, dbapi.SettingInstallation); string(b.Data) != "inst-2" {
		t.Error("put must replace")
	}
	list, err := s.BlobFrom(ctx, dbapi.BlobTrust, 2)
	if err != nil || len(list) != 2 || list[0].Seq != 2 || list[1].Seq != 3 {
		t.Fatalf("from: %+v, %v", list, err)
	}
	if list, err := s.BlobFrom(ctx, dbapi.BlobLicense, 0); err != nil || len(list) != 0 {
		t.Errorf("other kind: %+v, %v", list, err)
	}
	if err := s.BlobDelete(ctx, dbapi.BlobTrust, "2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BlobGet(ctx, dbapi.BlobTrust, "2"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
	if err := s.BlobDelete(ctx, dbapi.BlobTrust, "2"); err != nil {
		t.Errorf("deleting twice must be fine: %v", err)
	}
	if err := s.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("empty ID: %v", err)
	}
	big := bytes.Repeat([]byte{7}, 1<<20)
	if err := s.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobUI, ID: "org.sorglos.terminal@1.0.0", Data: big}); err != nil {
		t.Fatalf("blob of 1 MiB: %v", err)
	}
	if b, err := s.BlobGet(ctx, dbapi.BlobUI, "org.sorglos.terminal@1.0.0"); err != nil || !bytes.Equal(b.Data, big) {
		t.Errorf("blob of 1 MiB: %v", err)
	}
}

func testApps(t *testing.T, s dbapi.Store) {
	if _, err := s.AppGet(ctx, "org.sorglos.terminal"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("missing app: %v", err)
	}
	a := dbapi.App{ID: "org.sorglos.terminal", Version: "1.0.0", Store: "global", Node: "ctl-1", Active: true,
		Manifest: []byte(`{"id":"org.sorglos.terminal"}`), SHA256: bytes.Repeat([]byte{1}, 32),
		Grants: []string{"network"}, Updated: 1790000000}
	if err := s.AppPut(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.AppPut(ctx, dbapi.App{ID: "org.sorglos.calculator", Version: "1.0.0", Store: "global", Node: "ctl-1"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.AppGet(ctx, a.ID)
	if err != nil || got.Version != "1.0.0" || !got.Active || len(got.Grants) != 1 || !bytes.Equal(got.SHA256, a.SHA256) {
		t.Fatalf("get: %+v, %v", got, err)
	}
	a.Active, a.Version = false, "1.1.0"
	if err := s.AppPut(ctx, a); err != nil {
		t.Fatal(err)
	}
	list, err := s.AppList(ctx)
	if err != nil || len(list) != 2 || list[0].ID != "org.sorglos.calculator" || list[1].Active {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if err := s.AppDelete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.AppList(ctx); len(list) != 1 {
		t.Error("delete")
	}
	if err := s.AppPut(ctx, dbapi.App{}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("app without ID: %v", err)
	}
}

func testOnce(t *testing.T, s dbapi.Store) {
	nonce := bytes.Repeat([]byte{0x53}, 32)
	if err := s.OnceCreate(ctx, dbapi.OnceNonce, nonce, "user-1", 1790000100); err != nil {
		t.Fatal(err)
	}
	if err := s.OnceCreate(ctx, dbapi.OnceNonce, nonce, "user-2", 1790000100); !errors.Is(err, dbapi.ErrUsed) {
		t.Errorf("second create: %v", err)
	}
	if err := s.OnceCreate(ctx, dbapi.OnceTask, nonce, "", 1790000100); err != nil {
		t.Errorf("same value, other kind: %v", err)
	}
	data, err := s.OnceUse(ctx, dbapi.OnceNonce, nonce, 1790000000)
	if err != nil || data != "user-1" {
		t.Fatalf("use: %q, %v", data, err)
	}
	if _, err := s.OnceUse(ctx, dbapi.OnceNonce, nonce, 1790000000); !errors.Is(err, dbapi.ErrUsed) {
		t.Errorf("second use: %v", err)
	}
	if _, err := s.OnceUse(ctx, dbapi.OnceTask, nonce, 1790000100); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("expired: %v", err)
	}
	if _, err := s.OnceUse(ctx, dbapi.OnceNonce, []byte("other"), 1790000000); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	if err := s.OncePurge(ctx, 1790000100); err != nil {
		t.Fatal(err)
	}
	if err := s.OnceCreate(ctx, dbapi.OnceNonce, nonce, "user-3", 1790000200); err != nil {
		t.Errorf("after purge the value is free again: %v", err)
	}
}

func testUsers(t *testing.T, s dbapi.Store) {
	if _, err := s.UserGet(ctx, "nobody"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("missing user: %v", err)
	}
	admin := dbapi.User{ID: "u-admin", Name: "admin", Roles: []string{"admin"}, TOTP: []byte("secret"), LastLogin: 1790000000}
	alice := dbapi.User{ID: "u-alice", Name: "alice", Roles: []string{"user"}}
	for _, u := range []dbapi.User{admin, alice} {
		if err := s.UserPut(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.UserByName(ctx, "admin")
	if err != nil || got.ID != "u-admin" || string(got.TOTP) != "secret" {
		t.Fatalf("by name: %+v, %v", got, err)
	}
	admin.Failures, admin.Locked = 3, 1790000600
	if err := s.UserPut(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.UserGet(ctx, "u-admin"); got.Failures != 3 || got.Locked != 1790000600 {
		t.Errorf("update: %+v", got)
	}
	if err := s.UserPut(ctx, dbapi.User{ID: "u-evil", Name: "admin"}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("name taken: %v", err)
	}
	list, err := s.UserList(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "admin" {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if err := s.UserDelete(ctx, "u-alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserGet(ctx, "u-alice"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
	if err := s.UserPut(ctx, dbapi.User{ID: "u-x"}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("user without name: %v", err)
	}
}

func testSessions(t *testing.T, s dbapi.Store) {
	token := bytes.Repeat([]byte{0x11}, 32)
	v := dbapi.Session{Token: token, User: "u-admin", Cert: []byte(`{"user":"u-admin"}`), Proof: []byte(`{}`),
		APIKey: bytes.Repeat([]byte{2}, 48), APICert: []byte(`{}`), Created: 1790000000, Expires: 1790003600, Seen: 1790000000}
	if err := s.SessionPut(ctx, v); err != nil {
		t.Fatal(err)
	}
	got, err := s.SessionGet(ctx, token)
	if err != nil || got.User != "u-admin" || !bytes.Equal(got.APIKey, v.APIKey) {
		t.Fatalf("get: %+v, %v", got, err)
	}
	if err := s.SessionTouch(ctx, token, 1790000500); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SessionGet(ctx, token); got.Seen != 1790000500 {
		t.Errorf("touch: %+v", got)
	}
	if err := s.SessionTouch(ctx, []byte("nope"), 1790000500); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("touching an unknown session: %v", err)
	}
	if err := s.SessionPurge(ctx, 1790001000); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionGet(ctx, token); err != nil {
		t.Errorf("purge removed a session that is still valid: %v", err)
	}
	if err := s.SessionPurge(ctx, 1790003600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionGet(ctx, token); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("after purge: %v", err)
	}
	if err := s.SessionPut(ctx, dbapi.Session{Token: token}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("session without user: %v", err)
	}
	if err := s.SessionDelete(ctx, token); err != nil {
		t.Errorf("deleting what is gone: %v", err)
	}
}

func testPending(t *testing.T, s dbapi.Store) {
	k1, k2 := bytes.Repeat([]byte{0xC1}, 32), bytes.Repeat([]byte{0xC2}, 32)
	if err := s.PendingPut(ctx, dbapi.Pending{Name: "dienste", Key: k1, Since: 1790000100}); err != nil {
		t.Fatal(err)
	}
	if err := s.PendingPut(ctx, dbapi.Pending{Name: "web", Key: k2, Since: 1790000000}); err != nil {
		t.Fatal(err)
	}
	list, err := s.PendingList(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "web" {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if err := s.PendingDelete(ctx, k2); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.PendingList(ctx); len(list) != 1 || list[0].Name != "dienste" {
		t.Error("delete")
	}
	if err := s.PendingPut(ctx, dbapi.Pending{Name: "x"}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("without key: %v", err)
	}
}

// logID returns the log entry ID of n: 32 lower-case hex characters.
func logID(n byte) string { return strings.Repeat(hex.EncodeToString([]byte{n}), 16) }

// ids returns the IDs of entries, in order.
func ids(entries []core.LogEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}

func testLog(t *testing.T, s dbapi.Store) {
	entries := []core.LogEntry{
		{ID: logID(0x31), Time: 1790000001, User: "u-admin", Kind: core.LogLogin, Result: "ok"},
		{ID: logID(0x32), Time: 1790000002, User: "u-alice", Kind: core.LogChannel, App: "org.sorglos.terminal", Action: "shell", Node: "ctl-1", Result: "ok", Detail: "80x24"},
		{ID: logID(0x33), Time: 1790000003, User: "u-admin", Kind: core.LogTask, App: "ngc.store", Action: "activate", Node: "ctl-1", Result: "ok"},
	}
	for _, e := range entries {
		if err := s.LogAdd(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.LogQuery(ctx, core.LogQuery{Limit: 10})
	if err != nil || len(got) != 3 || got[0].Time != 1790000003 || got[1] != entries[1] {
		t.Fatalf("newest first: %+v, %v", got, err)
	}
	if got, _ := s.LogQuery(ctx, core.LogQuery{Limit: 2}); len(got) != 2 {
		t.Error("limit")
	}
	if got, _ := s.LogQuery(ctx, core.LogQuery{Limit: 10, BeforeTime: 1790000003}); len(got) != 2 || got[0].Time != 1790000002 {
		t.Error("before")
	}
	if got, _ := s.LogQuery(ctx, core.LogQuery{Limit: 10, User: "u-alice"}); len(got) != 1 || got[0].App != "org.sorglos.terminal" {
		t.Error("user")
	}
	for name, e := range map[string]core.LogEntry{
		"entry without id":         {Time: 1790000004, Kind: core.LogLogin, Result: "ok"},
		"entry with an invalid id": {ID: strings.ToUpper(logID(0x3a)), Time: 1790000004, Kind: core.LogLogin, Result: "ok"},
		"entry without kind":       {ID: logID(0x34), Time: 1790000004},
		"entry without time":       {ID: logID(0x35), Kind: core.LogLogin, Result: "ok"},
	} {
		if err := s.LogAdd(ctx, e); !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := s.LogAdd(ctx, core.LogEntry{ID: logID(0x31), Time: 1790000009, Kind: core.LogLogin, Result: "ok"}); !errors.Is(err, dbapi.ErrUsed) {
		t.Errorf("id twice: %v", err)
	}
	if got, _ := s.LogQuery(ctx, core.LogQuery{Limit: 10}); len(got) != 3 {
		t.Errorf("refused entries were kept: %+v", got)
	}
	for name, q := range map[string]core.LogQuery{
		"query without limit":       {},
		"cursor id without time":    {Limit: 10, BeforeID: logID(0x33)},
		"cursor with an invalid id": {Limit: 10, BeforeTime: 1790000003, BeforeID: "33"},
		"cursor time below 0":       {Limit: 10, BeforeTime: -1},
	} {
		if _, err := s.LogQuery(ctx, q); !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}

	// Five entries in the same second, added out of ID order.
	const second = 1790000010
	for _, n := range []byte{0xc3, 0x0a, 0xf1, 0x5e, 0x7b} {
		if err := s.LogAdd(ctx, core.LogEntry{ID: logID(n), Time: second, User: "u-bob", Kind: core.LogChannel, Result: "ok"}); err != nil {
			t.Fatal(err)
		}
	}
	// Paging with Limit 2 returns all five exactly once, in ID-descending order.
	var paged []string
	q := core.LogQuery{Limit: 2, User: "u-bob"}
	for range 5 {
		page, err := s.LogQuery(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		paged = append(paged, ids(page)...)
		q.BeforeTime, q.BeforeID = page[len(page)-1].Time, page[len(page)-1].ID
	}
	if want := []string{logID(0xf1), logID(0xc3), logID(0x7b), logID(0x5e), logID(0x0a)}; !slices.Equal(paged, want) {
		t.Errorf("five entries in the same second: paged %v, want %v", paged, want)
	}
	// A cursor inside a second: the rest of that second, then older seconds.
	got, err = s.LogQuery(ctx, core.LogQuery{Limit: 10, BeforeTime: second, BeforeID: logID(0x7b)})
	if want := []string{logID(0x5e), logID(0x0a), logID(0x33), logID(0x32), logID(0x31)}; err != nil || !slices.Equal(ids(got), want) {
		t.Errorf("cursor inside a second: %v, %v; want %v", ids(got), err, want)
	}
	// A cursor time without ID leaves out its whole second.
	got, err = s.LogQuery(ctx, core.LogQuery{Limit: 10, BeforeTime: second})
	if want := []string{logID(0x33), logID(0x32), logID(0x31)}; err != nil || !slices.Equal(ids(got), want) {
		t.Errorf("cursor time without id: %v, %v; want %v", ids(got), err, want)
	}
}

func testLocks(t *testing.T, s dbapi.Store) {
	const now = 1790000000
	ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-1", now, now+60)
	if err != nil || !ok {
		t.Fatalf("take: %v, %v", ok, err)
	}
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-2", now, now+60); err != nil || ok {
		t.Errorf("two owners: %v, %v", ok, err)
	}
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-1", now, now+120); err != nil || !ok {
		t.Errorf("same owner again: %v, %v", ok, err)
	}
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-2", now+121, now+180); err != nil || !ok {
		t.Errorf("after the time ran out: %v, %v", ok, err)
	}
	if err := s.LockFree(ctx, dbapi.LockUpdate, "api-1"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.LockTake(ctx, dbapi.LockUpdate, "api-1", now+121, now+180); ok {
		t.Error("the wrong owner must not free the lock")
	}
	if err := s.LockFree(ctx, dbapi.LockUpdate, "api-2"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.LockTake(ctx, dbapi.LockUpdate, "api-1", now+121, now+180); !ok {
		t.Error("after LockFree")
	}
	if _, err := s.LockTake(ctx, dbapi.LockUpdate, "api-1", now, now); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("lock without time: %v", err)
	}
}

// racers is how many goroutines each race starts at once, and rounds how
// many races a concurrency test runs, each on a fresh value or lock: a check
// and a write that are not one step lose only a few races. More goroutines
// per race make a lost race rarer, so the rounds are what catch it.
const (
	racers = 16
	rounds = 256
)

// race runs f(i) for i < racers in goroutines released together, and returns
// their results.
func race[T any](f func(i int) T) []T {
	out := make([]T, racers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			out[i] = f(i)
		}()
	}
	close(start)
	wg.Wait()
	return out
}

// testConcurrentOnce: of concurrent OnceCreate calls for the same kind and
// value, exactly one succeeds and every other one gives ErrUsed.
func testConcurrentOnce(t *testing.T, s dbapi.Store) {
	for round := range rounds {
		value := append(bytes.Repeat([]byte{0x5a}, 30), byte(round>>8), byte(round))
		errs := race(func(i int) error {
			return s.OnceCreate(ctx, dbapi.OnceTask, value, strconv.Itoa(i), 1790000100)
		})
		ok, used := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, dbapi.ErrUsed):
				used++
			default:
				t.Errorf("round %d, OnceCreate: %v", round, err)
			}
		}
		if ok != 1 || used != racers-1 {
			t.Fatalf("round %d: %d created, %d used, want 1 and %d", round, ok, used, racers-1)
		}
	}
}

// testConcurrentLock: of concurrent LockTake calls of different owners for a
// free lock, exactly one gets it.
func testConcurrentLock(t *testing.T, s dbapi.Store) {
	const now = 1790000000
	type result struct {
		ok  bool
		err error
	}
	for round := range rounds {
		name := dbapi.LockUpdate + "-" + strconv.Itoa(round)
		results := race(func(i int) result {
			ok, err := s.LockTake(ctx, name, "owner-"+strconv.Itoa(i), now, now+60)
			return result{ok, err}
		})
		taken := 0
		for _, r := range results {
			if r.err != nil {
				t.Errorf("round %d, LockTake: %v", round, r.err)
			}
			if r.ok {
				taken++
			}
		}
		if taken != 1 {
			t.Fatalf("round %d: %d owners took the lock, want 1", round, taken)
		}
	}
}

// flip changes the first byte of b in place, so a shared backing array shows
// it; b is left alone if empty.
func flip(b []byte) {
	if len(b) > 0 {
		b[0]++
	}
}

// flipStr does the same for the first element of a []string.
func flipStr(s []string) {
	if len(s) > 0 {
		s[0] += "!"
	}
}

// testAliasing checks the rule every implementation has to follow: Put/Create
// must copy the caller's slice instead of keeping a reference to it ("in"),
// and Get/List must return a copy the caller cannot use to reach back into
// the store ("out"). Each check mutates the slice actually handed across the
// call, not a copy of it, so an implementation that forgot to clone would
// have its stored or returned data change out from under it.
func testAliasing(t *testing.T, s dbapi.Store) {
	// Blob.Data: Put/Get, then one BlobFrom out check.
	wantData := []byte("blob-data")
	putData := bytes.Clone(wantData)
	if err := s.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobCatalog, ID: "a", Data: putData}); err != nil {
		t.Fatal(err)
	}
	flip(putData) // mutate what we handed to BlobPut
	if b, err := s.BlobGet(ctx, dbapi.BlobCatalog, "a"); err != nil || !bytes.Equal(b.Data, wantData) {
		t.Errorf("Blob.Data in: %q, want %q (%v)", b.Data, wantData, err)
	}
	outBlob, err := s.BlobGet(ctx, dbapi.BlobCatalog, "a")
	if err != nil {
		t.Fatal(err)
	}
	flip(outBlob.Data) // mutate what BlobGet handed back
	if b, err := s.BlobGet(ctx, dbapi.BlobCatalog, "a"); err != nil || !bytes.Equal(b.Data, wantData) {
		t.Errorf("Blob.Data out: %q, want %q (%v)", b.Data, wantData, err)
	}
	fromList, err := s.BlobFrom(ctx, dbapi.BlobCatalog, 0)
	if err != nil || len(fromList) != 1 {
		t.Fatalf("BlobFrom: %+v, %v", fromList, err)
	}
	flip(fromList[0].Data)
	if l, err := s.BlobFrom(ctx, dbapi.BlobCatalog, 0); err != nil || len(l) != 1 || !bytes.Equal(l[0].Data, wantData) {
		t.Errorf("BlobFrom out: %+v, want %q (%v)", l, wantData, err)
	}

	// App.Manifest, App.SHA256, App.Grants: Put/Get, then one AppList out check.
	wantManifest, wantSHA, wantGrants := []byte(`{"id":"app-a"}`), bytes.Repeat([]byte{9}, 32), []string{"network", "root"}
	putApp := dbapi.App{ID: "app-a", Manifest: bytes.Clone(wantManifest), SHA256: bytes.Clone(wantSHA), Grants: slices.Clone(wantGrants)}
	if err := s.AppPut(ctx, putApp); err != nil {
		t.Fatal(err)
	}
	flip(putApp.Manifest)
	flip(putApp.SHA256)
	flipStr(putApp.Grants)
	if a, err := s.AppGet(ctx, "app-a"); err != nil || !bytes.Equal(a.Manifest, wantManifest) || !bytes.Equal(a.SHA256, wantSHA) || !slices.Equal(a.Grants, wantGrants) {
		t.Errorf("App in: %+v (%v)", a, err)
	}
	outApp, err := s.AppGet(ctx, "app-a")
	if err != nil {
		t.Fatal(err)
	}
	flip(outApp.Manifest)
	flip(outApp.SHA256)
	flipStr(outApp.Grants)
	if a, err := s.AppGet(ctx, "app-a"); err != nil || !bytes.Equal(a.Manifest, wantManifest) || !bytes.Equal(a.SHA256, wantSHA) || !slices.Equal(a.Grants, wantGrants) {
		t.Errorf("App out: %+v (%v)", a, err)
	}
	appList, err := s.AppList(ctx)
	if err != nil || len(appList) != 1 {
		t.Fatalf("AppList: %+v, %v", appList, err)
	}
	flip(appList[0].Manifest)
	if l, err := s.AppList(ctx); err != nil || len(l) != 1 || !bytes.Equal(l[0].Manifest, wantManifest) {
		t.Errorf("AppList out: %+v, want %q (%v)", l, wantManifest, err)
	}

	// Session.Token, Session.APIKey. Token doubles as the lookup key, so every
	// SessionGet below uses wantToken, the original that is never mutated.
	wantToken, wantAPIKey := bytes.Repeat([]byte{0xAA}, 32), bytes.Repeat([]byte{0xBB}, 48)
	putToken, putAPIKey := bytes.Clone(wantToken), bytes.Clone(wantAPIKey)
	if err := s.SessionPut(ctx, dbapi.Session{Token: putToken, User: "u-a", APIKey: putAPIKey}); err != nil {
		t.Fatal(err)
	}
	flip(putToken)
	flip(putAPIKey)
	if v, err := s.SessionGet(ctx, wantToken); err != nil || !bytes.Equal(v.Token, wantToken) || !bytes.Equal(v.APIKey, wantAPIKey) {
		t.Errorf("Session in: %+v (%v)", v, err)
	}
	outSess, err := s.SessionGet(ctx, wantToken)
	if err != nil {
		t.Fatal(err)
	}
	flip(outSess.Token)
	flip(outSess.APIKey)
	if v, err := s.SessionGet(ctx, wantToken); err != nil || !bytes.Equal(v.Token, wantToken) || !bytes.Equal(v.APIKey, wantAPIKey) {
		t.Errorf("Session out: %+v (%v)", v, err)
	}

	// User.Roles, User.TOTP: Put/Get, then one UserList out check.
	wantRoles, wantTOTP := []string{"admin", "user"}, []byte("totp-secret")
	putUser := dbapi.User{ID: "u-a", Name: "alias-user", Roles: slices.Clone(wantRoles), TOTP: bytes.Clone(wantTOTP)}
	if err := s.UserPut(ctx, putUser); err != nil {
		t.Fatal(err)
	}
	flipStr(putUser.Roles)
	flip(putUser.TOTP)
	if u, err := s.UserGet(ctx, "u-a"); err != nil || !slices.Equal(u.Roles, wantRoles) || !bytes.Equal(u.TOTP, wantTOTP) {
		t.Errorf("User in: %+v (%v)", u, err)
	}
	outUser, err := s.UserGet(ctx, "u-a")
	if err != nil {
		t.Fatal(err)
	}
	flipStr(outUser.Roles)
	flip(outUser.TOTP)
	if u, err := s.UserGet(ctx, "u-a"); err != nil || !slices.Equal(u.Roles, wantRoles) || !bytes.Equal(u.TOTP, wantTOTP) {
		t.Errorf("User out: %+v (%v)", u, err)
	}
	userList, err := s.UserList(ctx)
	if err != nil || len(userList) != 1 {
		t.Fatalf("UserList: %+v, %v", userList, err)
	}
	flipStr(userList[0].Roles)
	if l, err := s.UserList(ctx); err != nil || len(l) != 1 || !slices.Equal(l[0].Roles, wantRoles) {
		t.Errorf("UserList out: %+v, want %v (%v)", l, wantRoles, err)
	}

	// Pending.Key: no single Get exists, so PendingList carries both checks.
	wantPendKey := bytes.Repeat([]byte{0xCC}, 32)
	putPending := dbapi.Pending{Name: "alias-pending", Key: bytes.Clone(wantPendKey)}
	if err := s.PendingPut(ctx, putPending); err != nil {
		t.Fatal(err)
	}
	flip(putPending.Key)
	pendList, err := s.PendingList(ctx)
	if err != nil || len(pendList) != 1 {
		t.Fatalf("PendingList: %+v, %v", pendList, err)
	}
	if !bytes.Equal(pendList[0].Key, wantPendKey) {
		t.Errorf("Pending in: %x, want %x", pendList[0].Key, wantPendKey)
	}
	flip(pendList[0].Key)
	if l, err := s.PendingList(ctx); err != nil || len(l) != 1 || !bytes.Equal(l[0].Key, wantPendKey) {
		t.Errorf("PendingList out: %+v, want %x (%v)", l, wantPendKey, err)
	}
}
