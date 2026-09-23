package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strconv"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
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
	// Fassungen 1 bis 3 ablegen, ab 2 lesen: genau zwei, aufsteigend.
	// (Testdaten aus trusttest, siehe Aufgabe 3; hier reichen Rohbytes.)
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
		File: "/var/lib/ngc/staged/ngc-api", State: "downloaded", Time: 100}
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
		{"relative file", func(st *Staged) { st.File = "staged/ngc-api" }},
		{"file that climbs out", func(st *Staged) { st.File = "/var/lib/ngc/../../etc/shadow" }},
		{"unknown state", func(st *Staged) { st.State = "halb" }},
	}
	for _, tc := range cases {
		st := good
		tc.damage(&st)
		if err := s.PutStaged(ctx, st); !errors.Is(err, dbapi.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", tc.name, err)
		}
	}
	// The system packages come from apt: no version, no checksum, no signer.
	system := Staged{Target: "system", File: "/var/lib/ngc/staged/system", State: "available", Time: 1}
	if err := s.PutStaged(ctx, system); err != nil {
		t.Errorf("system: %v", err)
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
	for _, data := range []string{
		`{"target":"ngc-api","version":"1.0.0","sha256":"","size":0,"file":"/x","state":"downloaded","time":1}`,
		`{"target":"ngc-api","version":"1.0.0","sha256":"` + sha + `","size":0,"file":"../etc/shadow","state":"downloaded","time":1}`,
		`{"was":"ganz anderes"}`,
		`nicht einmal JSON`,
	} {
		if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: "staged.ngc-api", Data: []byte(data)}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Staged(ctx, "ngc-api"); !errors.Is(err, ErrDocument) {
			t.Errorf("%.40s: err = %v, want ErrDocument", data, err)
		}
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
	f.Add([]byte(`{"target":"ngc-api","version":"1.2.0","sha256":"` + enc.B64.EncodeToString(make([]byte, 32)) +
		`","size":17,"seq":4,"file":"/var/lib/ngc/staged/ngc-api","state":"downloaded","time":1}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		st, err := decodeStaged(b)
		if err != nil {
			return
		}
		// The target of an accepted row becomes part of a document ID, so it
		// must stay inside the store's own namespace.
		if id := stagedID(st.Target); id == componentsKey || id != stagedPrefix+st.Target {
			t.Fatalf("target %q leaves the staged namespace: %q", st.Target, id)
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
