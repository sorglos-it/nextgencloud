package license

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/node"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// What every case starts from. testInstall is 16 bytes in base64url, which is
// what trust.ValidID asks of an installation.
const (
	testApp     = "org.sorglos.notes"
	testVer     = "1.0.0"
	testStore   = "global"
	testOther   = "second"
	testInstall = "AAAAAAAAAAAAAAAAAAAAAA"
	testNow     = int64(1_700_000_000)
	// long ago, so a licence may have run out past its grace period and still
	// have been issued before it expired (store.License.Validate refuses
	// Expires <= Issued).
	long = int64(400 * 86400)
)

// stand is one installation: a local state, a trust list with one store, and
// the private key of that store.
type stand struct {
	t    *testing.T
	st   *state.Store
	list *trust.List
	priv ed25519.PrivateKey
}

func newStand(t *testing.T) *stand {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	s := state.New(memstore.New())
	t.Cleanup(func() { _ = s.Close() })
	return &stand{t: t, st: s, priv: priv,
		list: &trust.List{Stores: []trust.Store{
			{ID: testStore, Name: "Global", URL: "https://store.example.org", Key: enc.Bytes(pub)}}}}
}

// licence signs one licence of this stand's store. issued and expires are
// seconds; expires 0 is a bought app that never runs out.
func (s *stand) licence(issued, expires int64, bend func(*store.License)) sign.Envelope {
	s.t.Helper()
	l := store.License{ID: "lic-1", Store: testStore, App: testApp, Installation: testInstall,
		InstallKey: enc.Bytes(fill(0x11, 32)), Per: store.PerSite, Issued: issued, Expires: expires,
		Key: &store.WrappedKey{Ephemeral: enc.Bytes(fill(0x22, 32)), Nonce: enc.Bytes(fill(0x33, 12)),
			Sealed: enc.Bytes(fill(0x44, 48))}}
	if bend != nil {
		bend(&l)
	}
	body, err := json.Marshal(l)
	if err != nil {
		s.t.Fatal(err)
	}
	env, err := sign.Sign(s.priv, sign.PurposeLicense, body)
	if err != nil {
		s.t.Fatal(err)
	}
	return env
}

func fill(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// appRow is the row of an activated app of this stand's store.
func appRow() dbapi.App {
	return dbapi.App{ID: testApp, Version: testVer, Store: testStore, Node: "ctl-1", Active: true}
}

// issuedIn reads back what the state kept, so a case measures the store and
// not the answer of Keep alone.
func (s *stand) issuedIn() int64 {
	s.t.Helper()
	_, issued, err := s.st.License(context.Background(), testApp)
	if err != nil {
		s.t.Fatalf("no licence kept: %v", err)
	}
	return issued
}

// Only a newer licence replaces the one that is kept, and the answer always
// names the state of the licence that is KEPT - not of the one that came in.
func TestKeepTakesOnlyTheNewestLicense(t *testing.T) {
	s := newStand(t)
	ctx := context.Background()
	// The first one: valid for another year.
	first := s.licence(testNow-long-86400, testNow+365*86400, nil)
	res, err := Keep(ctx, s.st, s.list, testInstall, testApp, testStore, first, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.App != testApp || res.State != core.LicenseValid {
		t.Fatalf("first licence answered %+v", res)
	}
	if got := s.issuedIn(); got != testNow-long-86400 {
		t.Fatalf("kept issued %d, want %d", got, testNow-long-86400)
	}
	// A newer one, already run out past its grace period: it replaces the
	// first, and the answer is the state of THIS one.
	newer := s.licence(testNow-long, testNow-store.GraceDefault-1, func(l *store.License) { l.ID = "lic-2" })
	res, err = Keep(ctx, s.st, s.list, testInstall, testApp, testStore, newer, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != core.LicenseExpired {
		t.Fatalf("after the newer licence the state is %q, want %q", res.State, core.LicenseExpired)
	}
	if got := s.issuedIn(); got != testNow-long {
		t.Fatalf("kept issued %d, want the newer %d", got, testNow-long)
	}
	// An older one changes nothing, and neither does one of the same age -
	// the answer stays the state of the licence that is kept.
	for name, env := range map[string]sign.Envelope{
		"older":    s.licence(testNow-long-7200, testNow+365*86400, func(l *store.License) { l.ID = "lic-3" }),
		"same age": s.licence(testNow-long, testNow+365*86400, func(l *store.License) { l.ID = "lic-4" }),
	} {
		res, err := Keep(ctx, s.st, s.list, testInstall, testApp, testStore, env, testNow)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.State != core.LicenseExpired {
			t.Fatalf("%s answered %q, want the kept %q", name, res.State, core.LicenseExpired)
		}
		if got := s.issuedIn(); got != testNow-long {
			t.Fatalf("%s replaced the kept licence: issued %d", name, got)
		}
	}
}

// A licence made out for another installation, or by another store, or signed
// with another key is refused - and the licence that is kept stays.
func TestKeepRefusesALicenseForAnotherInstallation(t *testing.T) {
	s := newStand(t)
	ctx := context.Background()
	good := s.licence(testNow-86400, testNow+365*86400, nil)
	if _, err := Keep(ctx, s.st, s.list, testInstall, testApp, testStore, good, testNow); err != nil {
		t.Fatal(err)
	}

	other := "BBBBBBBBBBBBBBBBBBBBBB"
	_, foreignPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(store.License{ID: "lic-x", Store: testStore, App: testApp,
		Installation: testInstall, InstallKey: enc.Bytes(fill(0x11, 32)), Per: store.PerSite,
		Issued: testNow, Key: &store.WrappedKey{Ephemeral: enc.Bytes(fill(0x22, 32)),
			Nonce: enc.Bytes(fill(0x33, 12)), Sealed: enc.Bytes(fill(0x44, 48))}})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := sign.Sign(foreignPriv, sign.PurposeLicense, body)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		env     sign.Envelope
		storeID string
	}{
		"another installation": {s.licence(testNow, 0, func(l *store.License) { l.Installation = other }), testStore},
		"another app":          {s.licence(testNow, 0, func(l *store.License) { l.App = "org.sorglos.photos" }), testStore},
		"another store":        {s.licence(testNow, 0, func(l *store.License) { l.Store = testOther }), testStore},
		"a store not listed":   {s.licence(testNow, 0, nil), testOther},
		"another key":          {foreign, testStore},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Keep(ctx, s.st, s.list, testInstall, testApp, c.storeID, c.env, testNow); !errors.Is(err, ErrVerify) {
				t.Fatalf("answered %v, want ErrVerify", err)
			}
			if got := s.issuedIn(); got != testNow-86400 {
				t.Fatalf("the kept licence changed: issued %d", got)
			}
		})
	}
}

// Allowed follows the state of the licence, and nothing else: valid and grace
// may run, expired may not. An app without a licence is a free app and may.
func TestAllowedFollowsTheState(t *testing.T) {
	ctx := context.Background()
	cases := map[string]struct {
		expires int64
		want    bool
	}{
		"valid":   {testNow + 86400, true},
		"grace":   {testNow - 60, true},
		"expired": {testNow - store.GraceDefault - 1, false},
		"bought":  {0, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newStand(t)
			if _, err := Keep(ctx, s.st, s.list, testInstall, testApp, testStore,
				s.licence(testNow-long, c.expires, nil), testNow); err != nil {
				t.Fatal(err)
			}
			got, err := Allowed(ctx, s.st, s.list, testInstall, appRow(), testNow)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Fatalf("Allowed answered %v, want %v", got, c.want)
			}
		})
	}
	t.Run("free app", func(t *testing.T) {
		s := newStand(t)
		got, err := Allowed(ctx, s.st, s.list, testInstall, appRow(), testNow)
		if err != nil {
			t.Fatal(err)
		}
		if !got {
			t.Fatal("an app without a licence may not run")
		}
	})
}

// Without a trust list in reach a paid app does not run: its licence cannot be
// judged, and the safer answer is no. A free app is not touched by it.
func TestAllowedWithoutAListRefusesOnlyThePaidApp(t *testing.T) {
	ctx := context.Background()
	s := newStand(t)
	if _, err := Keep(ctx, s.st, s.list, testInstall, testApp, testStore,
		s.licence(testNow-86400, 0, nil), testNow); err != nil {
		t.Fatal(err)
	}
	got, err := Allowed(ctx, s.st, nil, testInstall, appRow(), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("a paid app runs without a trust list to judge its licence")
	}
	// The same, one step less far: the list is there, but the store of the app
	// is not in it any more. Its licence cannot be judged either.
	row := appRow()
	row.Store = "gone"
	got, err = Allowed(ctx, s.st, s.list, testInstall, row, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("a paid app runs although its store is not in the trust list")
	}
	free := newStand(t)
	for name, list := range map[string]*trust.List{"no list": nil, "a list": free.list} {
		got, err := Allowed(ctx, free.st, list, testInstall, appRow(), testNow)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !got {
			t.Fatalf("a free app is held back (%s)", name)
		}
	}
}

// What Allowed answers reaches the API as a word of the contract: an app whose
// licence ran out is unlicensed in the status, not stopped.
func TestAnExpiredLicenseMakesTheAppUnlicensed(t *testing.T) {
	ctx := context.Background()
	s := newStand(t)
	if _, err := Keep(ctx, s.st, s.list, testInstall, testApp, testStore,
		s.licence(testNow-long, testNow-store.GraceDefault-1, nil), testNow); err != nil {
		t.Fatal(err)
	}
	if err := s.st.PutApp(ctx, appRow()); err != nil {
		t.Fatal(err)
	}
	r := node.NewReporter(node.ReportConfig{State: s.st, Log: slog.New(slog.DiscardHandler),
		Licensed: func(ctx context.Context, a dbapi.App) (bool, error) {
			return Allowed(ctx, s.st, s.list, testInstall, a, testNow)
		}})
	st, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Apps) != 1 || st.Apps[0].State != tunnel.StateUnlicensed {
		t.Fatalf("the status says %+v, want one app %q", st.Apps, tunnel.StateUnlicensed)
	}
}
