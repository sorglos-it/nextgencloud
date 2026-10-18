package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strconv"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s := New(memstore.New())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStagedRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	want := Staged{Target: "ngc-controller", Version: "1.2.0", SHA256: make([]byte, 32),
		Size: 17, Seq: 4, File: "/var/lib/ngc/staged/ngc-controller", State: "downloaded", Time: 100}
	if err := s.PutStaged(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Staged(ctx, "ngc-controller")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != want.Version || got.Seq != want.Seq || got.File != want.File {
		t.Fatalf("staged = %+v, want %+v", got, want)
	}
	if err := s.DeleteStaged(ctx, "ngc-controller"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Staged(ctx, "ngc-controller"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("after delete: err = %v, want ErrNotFound", err)
	}
}

func TestUseTaskRefusesReplay(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	id := []byte("0123456789abcdef")
	if err := s.UseTask(ctx, id, 200, 100); err != nil {
		t.Fatal(err)
	}
	if err := s.UseTask(ctx, id, 200, 100); !errors.Is(err, dbapi.ErrUsed) {
		t.Fatalf("second use: err = %v, want ErrUsed", err)
	}
}

func TestVersionsFrom(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	// Three signed versions, as a controller really keeps them.
	admin := trusttest.NewSigner(1)
	want := []trust.Signed{admin.Sign(t, trusttest.Genesis(admin))}
	for len(want) < 3 {
		want = append(want, admin.Sign(t, trusttest.Next(t, want[len(want)-1])))
	}
	for i, v := range want {
		if err := s.PutVersion(ctx, v, uint64(i+1)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Versions(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("versions from 2: %d entries, want 2", len(got))
	}
	for i, v := range got {
		if !bytes.Equal(v.Payload, want[i+1].Payload) {
			t.Fatalf("entry %d is not the version that was stored", i)
		}
		l, err := trust.Parse(v.Payload)
		if err != nil {
			t.Fatal(err)
		}
		// Ascending by number, so trust.Open and trust.Advance see the chain
		// in the order they verify it in.
		if l.Seq != uint64(i+2) {
			t.Fatalf("entry %d holds version %d, want %d", i, l.Seq, i+2)
		}
	}
	if seq, err := s.NewestSeq(ctx); err != nil || seq != 3 {
		t.Fatalf("newest seq = %d, err = %v, want 3, nil", seq, err)
	}
}

// signedVersion builds a version of the shape trust.ParseSigned takes: raw
// bytes as payload and a proof that carries no real signature. The store
// verifies none, so this is all it needs; task 3 repeats the test with signed
// data from trusttest.
func signedVersion(payload string) trust.Signed {
	return trust.Signed{
		Payload: []byte(payload),
		Proof:   proof.Proof{Kind: proof.KindPassword, KeyID: sign.KeyID([]byte("test")), Sig: make([]byte, 64)},
	}
}

func TestVersionsFromWithoutSignatures(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if seq, err := s.NewestSeq(ctx); err != nil || seq != 0 {
		t.Fatalf("empty store: seq = %d, err = %v, want 0, nil", seq, err)
	}
	for i := uint64(1); i <= 3; i++ {
		if err := s.PutVersion(ctx, signedVersion(`{"seq":`+strconv.FormatUint(i, 10)+`}`), i); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Versions(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || string(got[0].Payload) != `{"seq":2}` || string(got[1].Payload) != `{"seq":3}` {
		t.Fatalf("versions from 2: %d entries, %q", len(got), got)
	}
	if seq, err := s.NewestSeq(ctx); err != nil || seq != 3 {
		t.Fatalf("newest seq = %d, err = %v, want 3, nil", seq, err)
	}
}

func TestPutVersionRefusesUnusableVersion(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	good := signedVersion(`{}`)
	cases := []struct {
		name string
		v    trust.Signed
		seq  uint64
	}{
		{"version 0", good, 0},
		{"empty payload", trust.Signed{Proof: good.Proof}, 1},
		{"malformed proof", trust.Signed{Payload: good.Payload}, 1},
	}
	for _, tc := range cases {
		if err := s.PutVersion(ctx, tc.v, tc.seq); !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", tc.name, err)
		}
	}
}

func TestStagedRefusesWhatItCannotStage(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	good := Staged{Target: "ngc-api", Version: "1.2.0", SHA256: make([]byte, 32),
		File: config.StageDir + "/ngc-api", State: "downloaded", Time: 100}
	cases := []struct {
		name   string
		damage func(*Staged)
	}{
		{"no target at all", func(st *Staged) { st.Target = "" }},
		{"target that climbs out", func(st *Staged) { st.Target = "../components" }},
		{"core app as target", func(st *Staged) { st.Target = "ngc.store" }},
		{"no version", func(st *Staged) { st.Version = "" }},
		{"short checksum", func(st *Staged) { st.SHA256 = make([]byte, 31) }},
		{"signer that is no key", func(st *Staged) { st.Signer = make([]byte, 31) }},
		{"negative size", func(st *Staged) { st.Size = -1 }},
		{"no file", func(st *Staged) { st.File = "" }},
		{"relative file", func(st *Staged) { st.File = "staged/ngc-api" }},
		{"file outside the staging directory", func(st *Staged) { st.File = "/etc/shadow" }},
		{"file beside the staging directory", func(st *Staged) { st.File = config.StageDir + "-alt/ngc-api" }},
		{"file that climbs out", func(st *Staged) { st.File = config.StageDir + "/../../etc/shadow" }},
		{"unknown state", func(st *Staged) { st.State = "halb" }},
		{"no time", func(st *Staged) { st.Time = 0 }},
	}
	for _, tc := range cases {
		st := good
		tc.damage(&st)
		if err := s.PutStaged(ctx, st); !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", tc.name, err)
		}
	}
	// The system packages come from apt: nothing lies ready, so no file, no
	// version, no checksum and no signer.
	system := Staged{Target: "system", State: "available", Time: 1}
	if err := s.PutStaged(ctx, system); err != nil {
		t.Errorf("system: %v", err)
	}
	if got, err := s.Staged(ctx, "system"); err != nil || got.File != "" {
		t.Errorf("system read back: %+v, %v", got, err)
	}
	system.Version = "1.2.0"
	if err := s.PutStaged(ctx, system); !errors.Is(err, dbapi.ErrInvalid) {
		t.Error("system with a version: want ErrInvalid")
	}
	for _, target := range []string{"", "../components", "ngc.store"} {
		if _, err := s.Staged(ctx, target); !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("Staged(%q): want ErrInvalid", target)
		}
		if err := s.DeleteStaged(ctx, target); !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("DeleteStaged(%q): want ErrInvalid", target)
		}
	}
}

// TestStagedRefusesDamagedRow writes past the store, the way a broken database
// or a root shell would, and checks that nothing of it gets out again.
func TestStagedRefusesDamagedRow(t *testing.T) {
	ctx := context.Background()
	db := memstore.New()
	s := New(db)
	t.Cleanup(func() { _ = s.Close() })
	sha := enc.B64.EncodeToString(make([]byte, 32))
	row := func(file, tail string) string {
		return `{"target":"ngc-api","version":"1.0.0","sha256":"` + sha + `","size":0,"file":"` + file + `",` + tail + `}`
	}
	for _, data := range []string{
		`{"target":"ngc-api","version":"1.0.0","sha256":"","size":0,"file":"` + config.StageDir +
			`/ngc-api","state":"downloaded","time":1}`,
		row("/etc/shadow", `"state":"downloaded","time":1`),
		row(config.StageDir+`/../../etc/shadow`, `"state":"downloaded","time":1`),
		row(config.StageDir+`/ngc-api`, `"state":"downloaded","time":0`),
		`{"was":"ganz anderes"}`,
		`nicht einmal JSON`,
	} {
		if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: "staged.ngc-api", Data: []byte(data)}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Staged(ctx, "ngc-api"); !errors.Is(err, ErrDocument) {
			t.Errorf("%.60s: err = %v, want ErrDocument", data, err)
		}
	}
}

// TestStagedRefusesAnotherTarget guards the key itself: a row that was moved
// or edited under staged.ngc-api must not answer as the system update, or the
// caller would run apt where a downloaded file was meant to be installed.
func TestStagedRefusesAnotherTarget(t *testing.T) {
	ctx := context.Background()
	db := memstore.New()
	s := New(db)
	t.Cleanup(func() { _ = s.Close() })
	row := []byte(`{"target":"system","sha256":"","size":0,"state":"available","time":1}`)
	if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: "staged.ngc-api", Data: row}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Staged(ctx, "ngc-api"); !errors.Is(err, ErrDocument) {
		t.Fatalf("err = %v, want ErrDocument", err)
	}
	// Under its own key the very same row is good.
	if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: "staged.system", Data: row}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Staged(ctx, "system"); err != nil || got.Target != "system" {
		t.Fatalf("under its own key: %+v, %v", got, err)
	}
}

func TestComponentsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if _, err := s.Components(ctx); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatal("before the first write: want ErrNotFound")
	}
	want := map[string]string{"ngc-api": "1.2.0", "ngc-controller": "1.2.1", "ngc-web": "1.2.2"}
	if err := s.PutComponents(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Components(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(got, want) {
		t.Fatalf("components = %v, want %v", got, want)
	}
	if err := s.PutComponents(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Components(ctx); err != nil || len(got) != 0 {
		t.Fatalf("after clearing: %v, %v", got, err)
	}
	for _, bad := range []map[string]string{
		{"ngc-store": "1.2.0"},
		{"ngc-api": "1.2"},
		{"ngc-api": "0.0.0-dev"},
	} {
		if err := s.PutComponents(ctx, bad); !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("%v: want ErrInvalid", bad)
		}
	}
}

func TestComponentsRefusesDamagedRow(t *testing.T) {
	ctx := context.Background()
	db := memstore.New()
	s := New(db)
	t.Cleanup(func() { _ = s.Close() })
	for _, data := range []string{
		`{"ngc-api":"1.2.0","ngc-web":"1.2.0","ngc-controller":"1.2.0","ngc-db":"1.2.0"}`,
		`{"ngc-store":"1.2.0"}`,
		`null`,
		`[]`,
	} {
		if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: "components", Data: []byte(data)}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Components(ctx); !errors.Is(err, ErrDocument) {
			t.Errorf("%.40s: err = %v, want ErrDocument", data, err)
		}
	}
}

// licenseOf builds an envelope of the shape sign.Envelope.Validate takes; no
// signature is made, because the store verifies none.
func licenseOf(payload string) sign.Envelope {
	return sign.Envelope{Payload: []byte(payload), KeyID: sign.KeyID([]byte("store")), Sig: make([]byte, 64)}
}

func TestLicenseRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if _, _, err := s.License(ctx, "org.sorglos.terminal"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatal("unknown app: want ErrNotFound")
	}
	want := licenseOf(`{"app":"org.sorglos.terminal"}`)
	if err := s.PutLicense(ctx, "org.sorglos.terminal", want, 1700000000); err != nil {
		t.Fatal(err)
	}
	got, issued, err := s.License(ctx, "org.sorglos.terminal")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Payload, want.Payload) || got.KeyID != want.KeyID ||
		!bytes.Equal(got.Sig, want.Sig) || issued != 1700000000 {
		t.Fatalf("license = %+v, issued %d", got, issued)
	}
	cases := []struct {
		name   string
		app    string
		env    sign.Envelope
		issued int64
	}{
		{"no app", "", want, 1},
		{"app that is no app", "Terminal", want, 1},
		{"envelope without a signature", "org.sorglos.terminal", sign.Envelope{Payload: []byte(`{}`)}, 1},
		{"payload over store.MaxLicense", "org.sorglos.terminal",
			licenseOf(string(make([]byte, store.MaxLicense+1))), 1},
		{"issued before the epoch", "org.sorglos.terminal", want, 0},
	}
	for _, tc := range cases {
		if err := s.PutLicense(ctx, tc.app, tc.env, tc.issued); !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", tc.name, err)
		}
	}
}

func TestLicenseAtItsLargest(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	// A payload of store.MaxLicense bytes has to fit into the document, or the
	// largest licence a store may issue could not be kept at all.
	if err := s.PutLicense(ctx, "org.sorglos.terminal", licenseOf(string(make([]byte, store.MaxLicense))), 1); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.License(ctx, "org.sorglos.terminal")
	if err != nil || len(got.Payload) != store.MaxLicense {
		t.Fatalf("%d bytes back, err = %v", len(got.Payload), err)
	}
}

func TestLicenseRefusesDamagedRow(t *testing.T) {
	ctx := context.Background()
	db := memstore.New()
	s := New(db)
	t.Cleanup(func() { _ = s.Close() })
	env := func(payload string) string {
		return `{"payload":"` + payload + `","kid":"` + sign.KeyID([]byte("store")) +
			`","sig":"` + enc.B64.EncodeToString(make([]byte, 64)) + `"}`
	}
	for _, data := range []string{
		`{"envelope":null,"issued":1}`,
		`{"envelope":` + env("e30") + `,"issued":null}`,
		// enc.MaxSafeInteger + 1. Only the budget refuses this; DecodeStrict
		// on its own would put it into the int64 and hand it out.
		`{"envelope":` + env("e30") + `,"issued":9007199254740992}`,
		`{"envelope":` + env("") + `,"issued":1}`,
		`{"was":"ganz anderes"}`,
	} {
		if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobLicense, ID: "org.sorglos.terminal", Data: []byte(data)}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.License(ctx, "org.sorglos.terminal"); !errors.Is(err, ErrDocument) {
			t.Errorf("%.50s: err = %v, want ErrDocument", data, err)
		}
	}
}

func TestAppsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if _, err := s.App(ctx, "org.sorglos.terminal"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatal("unknown app: want ErrNotFound")
	}
	for _, id := range []string{"org.sorglos.wiki", "org.sorglos.terminal"} {
		if err := s.PutApp(ctx, dbapi.App{ID: id, Version: "1.0.0", Active: true}); err != nil {
			t.Fatal(err)
		}
	}
	apps, err := s.Apps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 || apps[0].ID != "org.sorglos.terminal" {
		t.Fatalf("apps = %v", apps)
	}
	if err := s.DeleteApp(ctx, "org.sorglos.wiki"); err != nil {
		t.Fatal(err)
	}
	if apps, err := s.Apps(ctx); err != nil || len(apps) != 1 {
		t.Fatalf("after delete: %v, %v", apps, err)
	}
	for _, id := range []string{"", "Terminal", "org/sorglos"} {
		if err := s.PutApp(ctx, dbapi.App{ID: id, Version: "1.0.0"}); !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("PutApp(%q): want ErrInvalid", id)
		}
		if _, err := s.App(ctx, id); !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("App(%q): want ErrInvalid", id)
		}
		if err := s.DeleteApp(ctx, id); !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("DeleteApp(%q): want ErrInvalid", id)
		}
	}
	if err := s.PutApp(ctx, dbapi.App{ID: "org.sorglos.wiki", Version: "1.0"}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Error("PutApp with a version that is none: want ErrInvalid")
	}
	// Only kept data remains: such a row carries no version at all.
	if err := s.PutApp(ctx, dbapi.App{ID: "org.sorglos.wiki", KeptData: 1}); err != nil {
		t.Errorf("app with kept data only: %v", err)
	}
}

// TestAppsRefuseDamagedRow writes past the store, the way a broken database or
// a root shell would: an app row is input from outside on the way out too.
func TestAppsRefuseDamagedRow(t *testing.T) {
	ctx := context.Background()
	db := memstore.New()
	s := New(db)
	t.Cleanup(func() { _ = s.Close() })
	if err := db.AppPut(ctx, dbapi.App{ID: "org.sorglos.wiki", Version: "kaputt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.App(ctx, "org.sorglos.wiki"); !errors.Is(err, ErrDocument) {
		t.Error("App with a version that is none: want ErrDocument")
	}
	if _, err := s.Apps(ctx); !errors.Is(err, ErrDocument) {
		t.Error("Apps with a version that is none: want ErrDocument")
	}
	if err := db.AppDelete(ctx, "org.sorglos.wiki"); err != nil {
		t.Fatal(err)
	}
	if err := db.AppPut(ctx, dbapi.App{ID: "Terminal", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apps(ctx); !errors.Is(err, ErrDocument) {
		t.Error("Apps with an ID that is none: want ErrDocument")
	}
}

func TestUseTaskRefusesWhatItCannotGuard(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	cases := []struct {
		name         string
		id           []byte
		expires, now int64
	}{
		{"id too short", make([]byte, 15), 200, 100},
		{"id too long", make([]byte, 17), 200, 100},
		{"expired already", make([]byte, 16), 100, 100},
	}
	for _, tc := range cases {
		if err := s.UseTask(ctx, tc.id, tc.expires, tc.now); !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", tc.name, err)
		}
	}
}

func TestPurgeFreesExpiredTasks(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	id := []byte("0123456789abcdef")
	if err := s.UseTask(ctx, id, 200, 100); err != nil {
		t.Fatal(err)
	}
	if err := s.Purge(ctx, 200); err != nil {
		t.Fatal(err)
	}
	// An ID is only remembered until the task expires; after that the signed
	// task itself is refused, so the same ID may come round again.
	if err := s.UseTask(ctx, id, 400, 300); err != nil {
		t.Fatalf("after purge: %v", err)
	}
}

func FuzzStaged(f *testing.F) {
	f.Add([]byte(`{"target":"system","sha256":"","size":0,"file":"/x","state":"downloaded","time":1}`))
	f.Add([]byte(`{"target":"system","sha256":"","size":0,"state":"available","time":1}`))
	f.Add([]byte(`{"target":"ngc-api","version":"1.2.0","sha256":"` + enc.B64.EncodeToString(make([]byte, 32)) +
		`","size":17,"seq":4,"file":"` + config.StageDir + `/ngc-api","state":"downloaded","time":1}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		st, err := decodeStaged(b)
		if err != nil {
			return
		}
		// Every row that gets out names something this server can update; the
		// document ID is built from that name.
		if !store.ValidTarget(st.Target) {
			t.Fatalf("accepted a row for %q, which is no update target", st.Target)
		}
		// What the reader hands out, its own writer must store and read again
		// unchanged: otherwise a row could mean one thing now and another later.
		again, err := json.Marshal(st)
		if err != nil {
			t.Fatal(err)
		}
		back, err := decodeStaged(again)
		if err != nil {
			t.Fatalf("its own document came back as %v", err)
		}
		if back.Target != st.Target || back.Version != st.Version || !bytes.Equal(back.SHA256, st.SHA256) ||
			back.Size != st.Size || !bytes.Equal(back.Signer, st.Signer) || back.Seq != st.Seq ||
			back.File != st.File || back.State != st.State || back.Time != st.Time {
			t.Fatalf("round trip changed it: %+v, was %+v", back, st)
		}
	})
}

func FuzzComponents(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"ngc-api":"1.2.0","ngc-controller":"1.2.1","ngc-web":"1.2.2"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		m, err := decodeComponents(b)
		if err != nil {
			return
		}
		again, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		back, err := decodeComponents(again)
		if err != nil {
			t.Fatalf("its own document came back as %v", err)
		}
		if !maps.Equal(back, m) {
			t.Fatalf("round trip changed it: %v, was %v", back, m)
		}
	})
}

func FuzzLicense(f *testing.F) {
	f.Add([]byte(`{"envelope":{"payload":"e30","kid":"` + sign.KeyID([]byte("store")) +
		`","sig":"` + enc.B64.EncodeToString(make([]byte, 64)) + `"},"issued":1700000000}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		env, issued, err := decodeLicense(b)
		if err != nil {
			return
		}
		again, err := json.Marshal(licenseDoc{Envelope: env, Issued: issued})
		if err != nil {
			t.Fatal(err)
		}
		back, backIssued, err := decodeLicense(again)
		if err != nil {
			t.Fatalf("its own document came back as %v", err)
		}
		if !bytes.Equal(back.Payload, env.Payload) || back.KeyID != env.KeyID ||
			!bytes.Equal(back.Sig, env.Sig) || backIssued != issued {
			t.Fatalf("round trip changed it: %+v %d, was %+v %d", back, backIssued, env, issued)
		}
	})
}

// THE MEMORY FOR A CATALOGUE NUMBER, which is what makes the roll-back guard a
// guard: store.VerifyCatalog refuses a catalogue below the number it is handed,
// and the number it is handed is this one.
func TestCatalogSeqRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	// A store nothing was ever activated from is "none seen", and that is 0
	// and no error: a first activation has to be able to happen.
	got, err := s.CatalogSeq(ctx, "global")
	if err != nil || got != 0 {
		t.Fatalf("CatalogSeq of an unseen store = %d, %v, want 0 and no error", got, err)
	}
	if err := s.KeepCatalogSeq(ctx, "global", 7); err != nil {
		t.Fatalf("KeepCatalogSeq = %v", err)
	}
	if got, err := s.CatalogSeq(ctx, "global"); err != nil || got != 7 {
		t.Fatalf("CatalogSeq = %d, %v, want 7", got, err)
	}
	// Each store has its own floor: one store's catalogue says nothing about
	// another's, and a shared floor would shut a store out over a number it
	// never issued.
	if got, err := s.CatalogSeq(ctx, "other"); err != nil || got != 0 {
		t.Errorf("CatalogSeq of another store = %d, %v, want 0", got, err)
	}
}

// IT NEVER LOWERS WHAT IS KEPT. A floor that can be lowered is no floor: an
// activation out of an older catalogue that got through for another reason
// must not take the bar down with it.
func TestKeepCatalogSeqNeverLowersTheFloor(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	if err := s.KeepCatalogSeq(ctx, "global", 9); err != nil {
		t.Fatalf("KeepCatalogSeq = %v", err)
	}
	for _, seq := range []uint64{1, 8, 9} {
		// Asking for something already true is no error: the caller wanted a
		// floor of at least that, and it has one.
		if err := s.KeepCatalogSeq(ctx, "global", seq); err != nil {
			t.Errorf("KeepCatalogSeq(%d) = %v, want no error", seq, err)
		}
		if got, _ := s.CatalogSeq(ctx, "global"); got != 9 {
			t.Fatalf("KeepCatalogSeq(%d) took the floor to %d, want it left at 9", seq, got)
		}
	}
	if err := s.KeepCatalogSeq(ctx, "global", 10); err != nil {
		t.Fatalf("KeepCatalogSeq(10) = %v", err)
	}
	if got, _ := s.CatalogSeq(ctx, "global"); got != 10 {
		t.Errorf("the floor is %d, want 10: a higher number has to go in", got)
	}
}

// What cannot be a floor is refused rather than written: a store without a
// name, and a number outside what a document can carry back
// (enc.MaxSafeInteger). 0 is "none seen" and is not something a caller may
// write - it is the answer for a store that has no row.
func TestKeepCatalogSeqRefusesWhatIsNoFloor(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	for name, c := range map[string]struct {
		store string
		seq   uint64
	}{
		"no store":             {"", 7},
		"zero":                 {"global", 0},
		"above a safe integer": {"global", enc.MaxSafeInteger + 1},
	} {
		if err := s.KeepCatalogSeq(ctx, c.store, c.seq); !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("%s: KeepCatalogSeq = %v, want dbapi.ErrInvalid", name, err)
		}
	}
	// And the read side refuses the same nameless store rather than answering
	// 0 for it: 0 would be "none seen" for every store at once.
	if _, err := s.CatalogSeq(ctx, ""); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("CatalogSeq(\"\") = %v, want dbapi.ErrInvalid", err)
	}
}

// A READ THAT FAILS IS HANDED ON AND NEVER TAKEN FOR 0. Taking a database
// hiccup for "none seen" would open exactly the door this number closes, and
// the caller on the other side of it fails closed (node.installer.lastSeq
// answers MaxUint64 and refuses every catalogue).
func TestCatalogSeqHandsOnAReadThatFailed(t *testing.T) {
	errRead := errors.New("the database could not be read")
	s := New(brokenRead{Local: memstore.New(), err: errRead})
	ctx := t.Context()
	got, err := s.CatalogSeq(ctx, "global")
	if !errors.Is(err, errRead) {
		t.Fatalf("CatalogSeq = %d, %v, want the error of the database", got, err)
	}
	if got != 0 {
		t.Errorf("CatalogSeq gave %d beside its error, want 0 and the error read", got)
	}
	// The write side hands it on too: it reads the floor before it raises it,
	// and a floor written over an unread one is a floor that could go down.
	if err := s.KeepCatalogSeq(ctx, "global", 9); !errors.Is(err, errRead) {
		t.Errorf("KeepCatalogSeq = %v, want the error of the database", err)
	}
}

// brokenRead is a database whose BlobGet fails and whose every other call is
// the real one: what is measured is the one read, not a store that answers
// nothing.
type brokenRead struct {
	dbapi.Local
	err error
}

func (b brokenRead) BlobGet(context.Context, string, string) (dbapi.Blob, error) {
	return dbapi.Blob{}, b.err
}
