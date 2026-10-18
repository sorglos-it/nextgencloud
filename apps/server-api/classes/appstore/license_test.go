package appstore

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

// licenseVectors is the part of shared/testdata/store.json this file works
// with. It is read with its own type and not through vectors, so the cases of
// task 19 and these do not share a struct.
type licenseVectors struct {
	Store        string    `json:"store"`
	StoreKey     enc.Bytes `json:"storeKey"`
	Installation string    `json:"installation"`
	// InstallKey is the public X25519 key of the installation the licence of
	// the vectors is issued for.
	InstallKey enc.Bytes   `json:"installKey"`
	LicenseApp string      `json:"licenseApp"`
	License    envelopeRaw `json:"license"`
	Rejects    []struct {
		Name    string      `json:"name"`
		License envelopeRaw `json:"license"`
		Valid   bool        `json:"valid"`
	} `json:"licenseRejects"`
}

func loadLicenseVectors(t *testing.T) licenseVectors {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "shared", "testdata", "store.json"))
	if err != nil {
		t.Fatalf("store.json: %v", err)
	}
	var v licenseVectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("store.json: %v", err)
	}
	if len(v.Rejects) == 0 || len(v.InstallKey) != 32 || v.LicenseApp == "" {
		t.Fatalf("store.json carries no licence vectors")
	}
	return v
}

// The two secrets of the vectors' installation, as shared/store's own cases
// make them: the private X25519 key behind installKey, and the Ed25519 key
// behind storeKey. Both are checked against the file in licenseFixture, so a
// changed vector makes every case here fail loudly instead of quietly signing
// its own licences.
var (
	installPair, _ = trusttest.NodeKeys(0x91)
	storePriv      = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, ed25519.SeedSize))
)

// countingDB counts the lookups of an app row, so a case can tell a refusal
// before the database from one after it: an app ID out of a request must not
// reach a query at all.
type countingDB struct {
	dbapi.Store
	appGets int
}

func (c *countingDB) AppGet(ctx context.Context, id string) (dbapi.App, error) {
	c.appGets++
	return c.Store.AppGet(ctx, id)
}

// licFixture is newFixture with everything a licence needs: the installation's
// private key in the box and the paid app installed on a controller.
type licFixture struct {
	fixture
	V   licenseVectors
	DB  *countingDB
	App string
}

func newLicenseFixture(t *testing.T) licFixture {
	t.Helper()
	fx, _ := newFixture(t)
	v := loadLicenseVectors(t)
	// The vectors are the measurement's own ground: if the file no longer
	// holds the key pair these cases sign with, nothing below measures what
	// it claims to.
	if !bytes.Equal(installPair.Public, v.InstallKey) {
		t.Fatalf("installKey of the vectors is not NodeKeys(0x91)")
	}
	if !bytes.Equal(storePriv.Public().(ed25519.PublicKey), v.StoreKey) {
		t.Fatalf("storeKey of the vectors is not the seed 0x71")
	}
	id, err := fx.S.Installation(ctx)
	if err != nil {
		t.Fatalf("installation: %v", err)
	}
	if id != v.Installation {
		t.Fatalf("installation: got %q, want the vectors' %q", id, v.Installation)
	}
	sealed, err := fx.S.Box.Seal(installPair.Private)
	if err != nil {
		t.Fatalf("seal install key: %v", err)
	}
	if err := fx.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallKey,
		Data: sealed, Updated: testNow}); err != nil {
		t.Fatalf("install key: %v", err)
	}
	lf := licFixture{fixture: fx, V: v, App: v.LicenseApp}
	lf.putApp(t, v.LicenseApp, v.Store, "ctl-1")
	// The counter sits in front of the database the Stores use, after the
	// rows above are written, so it counts only what a case triggers.
	lf.DB = &countingDB{Store: fx.S.DB}
	fx.S.DB = lf.DB
	return lf
}

func (lf licFixture) putApp(t *testing.T, app, src, node string) {
	t.Helper()
	if err := lf.S.DB.AppPut(ctx, dbapi.App{ID: app, Version: "1.0.0", Store: src, Node: node,
		Active: true, Manifest: []byte(`{}`), SHA256: bytes.Repeat([]byte{1}, 32),
		Grants: []string{}, Updated: testNow}); err != nil {
		t.Fatalf("app row %q: %v", app, err)
	}
}

// theLicense is the licence of the vectors, as a store would hand it in.
func (lf licFixture) theLicense() []byte { return lf.V.License.bytes() }

// reject returns the licence vector of that name.
func (lf licFixture) reject(t *testing.T, name string) []byte {
	t.Helper()
	for _, r := range lf.V.Rejects {
		if r.Name == name {
			if r.Valid {
				t.Fatalf("licence vector %q is not a reject", name)
			}
			return r.License.bytes()
		}
	}
	t.Fatalf("no licence reject named %q", name)
	return nil
}

// contentKey is the content key the cases wrap: short on purpose, so a log
// entry that carried the whole of it would still be under audit's limit of
// 100 bytes for a detail and could not pass as "dropped for its length".
var contentKey = bytes.Repeat([]byte{0x5A}, store.KeySize)

// mint signs a licence with the store key of the vectors, so a case can have
// one the vectors do not carry: a real wrapped key, another issue time, a
// per-site licence. change gets the licence of the vectors' shape.
func mint(t *testing.T, app string, change func(l *store.License)) []byte {
	t.Helper()
	w, err := store.WrapKey(contentKey, installPair.Public, app, bytes.NewReader(bytes.Repeat([]byte{0x33}, 64)))
	if err != nil {
		t.Fatalf("wrap key: %v", err)
	}
	l := store.License{ID: "L-2", Store: store.GlobalID, App: app, Installation: trusttest.ID(0x11),
		InstallKey: installPair.Public, Per: store.PerUser, Users: 5,
		Issued: testNow, Expires: testNow + 30*86400, Key: w}
	if change != nil {
		change(&l)
	}
	payload, err := json.Marshal(l)
	if err != nil {
		t.Fatalf("marshal licence: %v", err)
	}
	env, err := sign.Sign(storePriv, sign.PurposeLicense, payload)
	if err != nil {
		t.Fatalf("sign licence: %v", err)
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return b
}

// keptLicense is the stored licence row, or ok false where none is stored.
func (lf licFixture) keptLicense(t *testing.T, app string) (dbapi.Blob, bool) {
	t.Helper()
	b, err := lf.S.DB.BlobGet(ctx, dbapi.BlobLicense, app)
	if errors.Is(err, dbapi.ErrNotFound) {
		return dbapi.Blob{}, false
	}
	if err != nil {
		t.Fatalf("licence row: %v", err)
	}
	return b, true
}

func TestTakeLicenseVerifiesEverything(t *testing.T) {
	lf := newLicenseFixture(t)
	// The way to the check must be open: nothing is stored before.
	if _, ok := lf.keptLicense(t, lf.App); ok {
		t.Fatal("a licence is stored before the case ran")
	}
	res, err := lf.St.TakeLicense(ctx, lf.App, lf.theLicense())
	if err != nil {
		t.Fatalf("take the licence of the vectors: %v", err)
	}
	if res.App != lf.App || res.State != core.LicenseValid {
		t.Errorf("result: got %+v, want app %q state %q", res, lf.App, core.LicenseValid)
	}
	row, ok := lf.keptLicense(t, lf.App)
	if !ok {
		t.Fatal("the licence was taken but not kept")
	}
	// The row carries the envelope the store signed, under the issue time as
	// its sequence: that is what the newest-wins check reads.
	if row.Seq != 1790000000 {
		t.Errorf("row sequence: got %d, want the issue time 1790000000", row.Seq)
	}
	// The row holds the envelope the store signed. It is compared field by
	// field and not byte for byte, because the row is the envelope written out
	// again (json.Marshal), not the bytes of the upload.
	kept, err := sign.ParseEnvelope(row.Data, store.MaxLicense)
	if err != nil {
		t.Fatalf("the kept row is no envelope: %v", err)
	}
	served := lf.V.License.envelope(t)
	if !bytes.Equal(kept.Payload, served.Payload) || !bytes.Equal(kept.Sig, served.Sig) ||
		kept.KeyID != served.KeyID {
		t.Error("the kept row is not the envelope the store served")
	}
	// License reads it back and checks it again, against the key the trust
	// list names today.
	l, env, held, err := lf.St.License(ctx, lf.App)
	if err != nil || !held {
		t.Fatalf("read the licence back: %v, held %v", err, held)
	}
	if l.App != lf.App || l.Per != store.PerUser || l.Users != 5 {
		t.Errorf("licence read back: got %+v", l)
	}
	if len(env.Sig) == 0 {
		t.Error("the envelope read back carries no signature")
	}
}

// TestTheAppIDIsNotPutToTheDatabase nails the wall in front of the lookup: an
// app ID out of a request is checked before any row is read.
func TestTheAppIDIsNotPutToTheDatabase(t *testing.T) {
	lf := newLicenseFixture(t)
	before := lf.DB.appGets
	if _, err := lf.St.TakeLicense(ctx, "../etc/passwd", lf.theLicense()); !errors.Is(err, ErrLicense) {
		t.Errorf("app ID outside the grammar: got %v, want ErrLicense", err)
	}
	if lf.DB.appGets != before {
		t.Errorf("app rows read: got %d, want %d -- the ID reached a query", lf.DB.appGets-before, 0)
	}
	// The counter does move for an ID the grammar allows, so the check above
	// is not measuring a counter that never moves.
	if _, err := lf.St.TakeLicense(ctx, lf.App, lf.theLicense()); err != nil {
		t.Fatalf("the licence of the vectors: %v", err)
	}
	if lf.DB.appGets <= before {
		t.Error("no app row was read for a valid ID: the counter measures nothing")
	}
}

func TestForeignLicenseIsRefused(t *testing.T) {
	v := loadLicenseVectors(t)
	if len(v.Rejects) != 6 {
		t.Fatalf("licence rejects: got %d, want the 6 of the vectors", len(v.Rejects))
	}
	for _, r := range v.Rejects {
		t.Run(r.Name, func(t *testing.T) {
			// One fixture per case: a licence taken in one case would decide
			// the next one through the newest-wins rule.
			lf := newLicenseFixture(t)
			if _, err := lf.St.TakeLicense(ctx, lf.App, r.License.bytes()); !errors.Is(err, ErrLicense) {
				t.Errorf("got %v, want ErrLicense", err)
			}
			if _, ok := lf.keptLicense(t, lf.App); ok {
				t.Error("a refused licence was kept")
			}
			// The same fixture takes the good licence, so the refusal above
			// was the vector and not an earlier wall.
			if _, err := lf.St.TakeLicense(ctx, lf.App, lf.theLicense()); err != nil {
				t.Fatalf("the licence of the vectors in the same fixture: %v", err)
			}
		})
	}
}

// TestALicenseOfAnotherAppIsRefused is the other side of the app binding: the
// licence is the real one of the vectors, handed in under another app's name.
func TestALicenseOfAnotherAppIsRefused(t *testing.T) {
	lf := newLicenseFixture(t)
	const other = "org.sorglos.mail"
	lf.putApp(t, other, lf.V.Store, "ctl-1")
	if _, err := lf.St.TakeLicense(ctx, other, lf.theLicense()); !errors.Is(err, ErrLicense) {
		t.Errorf("licence of another app: got %v, want ErrLicense", err)
	}
	if _, ok := lf.keptLicense(t, other); ok {
		t.Error("the licence was kept under another app")
	}
}

// TestALicenseOfAnotherStoreIsRefused: the app row names the store a licence
// must come from. An app installed from a store that is not in the trust list
// takes no licence at all.
func TestALicenseOfAnotherStoreIsRefused(t *testing.T) {
	lf := newLicenseFixture(t)
	lf.putApp(t, lf.App, "elsewhere", "ctl-1")
	if _, err := lf.St.TakeLicense(ctx, lf.App, lf.theLicense()); !errors.Is(err, ErrLicense) {
		t.Errorf("app of a store outside the trust list: got %v, want ErrLicense", err)
	}
}

func TestOnlyTheNewestLicenseIsKept(t *testing.T) {
	lf := newLicenseFixture(t)
	// Newest first: the one of the vectors (issued 1790000000) then an older
	// one, and the other way round in the second half.
	newer := mint(t, lf.App, func(l *store.License) { l.Issued = testNow + 100; l.Expires = testNow + 86400 })
	if _, err := lf.St.TakeLicense(ctx, lf.App, newer); err != nil {
		t.Fatalf("newer licence: %v", err)
	}
	res, err := lf.St.TakeLicense(ctx, lf.App, lf.theLicense())
	if err != nil {
		t.Fatalf("older licence: %v", err)
	}
	row, _ := lf.keptLicense(t, lf.App)
	if row.Seq != uint64(testNow+100) {
		t.Errorf("after the older licence the row is at %d, want %d", row.Seq, testNow+100)
	}
	if !bytes.Equal(row.Data, newer) {
		t.Error("the older licence replaced the newer one")
	}
	// The answer is the state of the licence that is held, not of the one
	// handed in.
	if res.State != core.LicenseValid {
		t.Errorf("state after the older licence: got %q", res.State)
	}
	// A licence of the same issue time does not replace either: equal is not
	// newer.
	same := mint(t, lf.App, func(l *store.License) { l.Issued = testNow + 100; l.Users = 99 })
	if _, err := lf.St.TakeLicense(ctx, lf.App, same); err != nil {
		t.Fatalf("licence of the same age: %v", err)
	}
	if row, _ := lf.keptLicense(t, lf.App); !bytes.Equal(row.Data, newer) {
		t.Error("a licence of the same age replaced the one held")
	}

	// The other way round, in a fixture of its own: a newer licence does
	// replace an older one, or the rule above would also hold with nothing
	// ever being stored.
	lf2 := newLicenseFixture(t)
	if _, err := lf2.St.TakeLicense(ctx, lf2.App, lf2.theLicense()); err != nil {
		t.Fatalf("first licence: %v", err)
	}
	if _, err := lf2.St.TakeLicense(ctx, lf2.App, newer); err != nil {
		t.Fatalf("newer licence after the older one: %v", err)
	}
	if row, _ := lf2.keptLicense(t, lf2.App); !bytes.Equal(row.Data, newer) {
		t.Error("the newer licence did not replace the older one")
	}
}

func TestContentKeyOpensOnlyWithTheInstallationKey(t *testing.T) {
	lf := newLicenseFixture(t)
	if _, err := lf.St.TakeLicense(ctx, lf.App, mint(t, lf.App, nil)); err != nil {
		t.Fatalf("take the licence: %v", err)
	}
	key, err := lf.St.ContentKey(ctx, lf.App)
	if err != nil {
		t.Fatalf("content key: %v", err)
	}
	if !bytes.Equal(key, contentKey) {
		t.Error("the content key is not the one the store wrapped")
	}
	// Another private key opens nothing. The row stays as it is, so what
	// changes is only the key the API holds.
	other, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x44}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := lf.S.Box.Seal(other.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err := lf.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallKey,
		Data: sealed, Updated: testNow}); err != nil {
		t.Fatal(err)
	}
	if _, err := lf.St.ContentKey(ctx, lf.App); err == nil {
		t.Error("another private key opened the content key")
	}
}

// TestNoContentKeyPastTheGracePeriod: once the licence is expired the key is
// not handed out any more, so no controller can start the services again.
func TestNoContentKeyPastTheGracePeriod(t *testing.T) {
	lf := newLicenseFixture(t)
	if _, err := lf.St.TakeLicense(ctx, lf.App, mint(t, lf.App, nil)); err != nil {
		t.Fatalf("take the licence: %v", err)
	}
	// Expires is testNow+30 days, grace is store.GraceDefault. Both sides of
	// that line are nailed down, or the case would also pass with no limit at
	// all.
	end := int64(testNow) + 30*86400 + store.GraceDefault
	lf.S.Now = func() int64 { return end - 1 }
	if _, err := lf.St.ContentKey(ctx, lf.App); err != nil {
		t.Errorf("one second before the end of the grace period: %v", err)
	}
	lf.S.Now = func() int64 { return end }
	if _, err := lf.St.ContentKey(ctx, lf.App); !errors.Is(err, ErrLicense) {
		t.Errorf("at the end of the grace period: got %v, want ErrLicense", err)
	}
}

// TestNoLicenseNoContentKey: an app without a licence gets no key, and the
// answer is the same error as a refused licence.
func TestNoLicenseNoContentKey(t *testing.T) {
	lf := newLicenseFixture(t)
	if _, err := lf.St.ContentKey(ctx, lf.App); !errors.Is(err, ErrLicense) {
		t.Errorf("app without a licence: got %v, want ErrLicense", err)
	}
	if _, _, held, err := lf.St.License(ctx, lf.App); err != nil || held {
		t.Errorf("License for an app without one: held %v, %v", held, err)
	}
}

func TestUsersAllowedCountsPerUserLicences(t *testing.T) {
	lf := newLicenseFixture(t)
	if _, err := lf.St.TakeLicense(ctx, lf.App, mint(t, lf.App, nil)); err != nil {
		t.Fatalf("take the licence: %v", err)
	}
	// The licence allows five users. Exactly five must pass and six must not:
	// a case that only checked six would also pass with the limit at zero.
	for _, c := range []struct {
		users   int
		wantOK  bool
		comment string
	}{{5, true, "on the line"}, {6, false, "one over the line"}} {
		lf2 := newLicenseFixture(t)
		if _, err := lf2.St.TakeLicense(ctx, lf2.App, mint(t, lf2.App, nil)); err != nil {
			t.Fatalf("take the licence: %v", err)
		}
		lf2.giveUse(t, lf2.App, c.users)
		n, ok, err := lf2.St.UsersAllowed(ctx, lf2.App)
		if err != nil {
			t.Fatalf("%s: %v", c.comment, err)
		}
		if n != c.users || ok != c.wantOK {
			t.Errorf("%s: got %d users, allowed %v; want %d, %v", c.comment, n, ok, c.users, c.wantOK)
		}
	}
	// A per-site licence counts no users: the same six users are allowed.
	lf3 := newLicenseFixture(t)
	site := mint(t, lf3.App, func(l *store.License) { l.Per = store.PerSite; l.Users = 0 })
	if _, err := lf3.St.TakeLicense(ctx, lf3.App, site); err != nil {
		t.Fatalf("per-site licence: %v", err)
	}
	lf3.giveUse(t, lf3.App, 6)
	if n, ok, err := lf3.St.UsersAllowed(ctx, lf3.App); err != nil || !ok || n != 6 {
		t.Errorf("per-site licence with six users: got %d, %v, %v", n, ok, err)
	}
	// An app with no licence on record is not allowed, and says so.
	if n, ok, err := lf.St.UsersAllowed(ctx, "org.sorglos.mail"); ok || !errors.Is(err, ErrLicense) {
		t.Errorf("app without a licence: got %d, %v, %v", n, ok, err)
	}
}

// giveUse puts n users into the trust list with a role that holds "<app>/use".
func (lf licFixture) giveUse(t *testing.T, app string, n int) {
	t.Helper()
	ver, err := lf.S.Trust(ctx)
	if err != nil {
		t.Fatalf("trust: %v", err)
	}
	l := *ver.List
	l.Seq++
	l.Prev = ver.Hash
	l.Issued++
	const role = "nutzer"
	l.Roles = append(append([]trust.Role{}, l.Roles...), trust.Role{Name: role, Level: trust.LevelOff})
	users := append([]trust.User{}, l.Users...)
	for i := 0; i < n; i++ {
		users = append(users, trust.User{ID: trusttest.ID(byte(0x30 + i)), Name: "u" + string(rune('a'+i)),
			Roles: []string{role}, Keys: []proof.UserKey{trusttest.NewSigner(byte(0x40 + i)).Key}})
	}
	l.Users = users
	admin := trusttest.NewSigner(1)
	if _, err := lf.S.AddVersion(ctx, admin.Sign(t, &l)); err != nil {
		t.Fatalf("trust version with %d users: %v", n, err)
	}
	if err := putRights(lf, role, []string{app + "/" + core.RightUse}); err != nil {
		t.Fatalf("rights row: %v", err)
	}
}

func putRights(lf licFixture, role string, list []string) error {
	data, err := json.Marshal(core.RoleRights{Role: role, Rights: list})
	if err != nil {
		return err
	}
	return lf.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobRights, ID: role, Data: data, Updated: testNow})
}

func TestPushLicensesSendsToEveryNodeRunningTheApp(t *testing.T) {
	lf := newLicenseFixture(t)
	const second = "org.sorglos.mail"
	const free = "org.sorglos.calculator"
	lf.putApp(t, second, lf.V.Store, "ctl-2")
	lf.putApp(t, free, lf.V.Store, "ctl-3")
	if _, err := lf.St.TakeLicense(ctx, lf.App, mint(t, lf.App, nil)); err != nil {
		t.Fatalf("licence of the first app: %v", err)
	}
	if _, err := lf.St.TakeLicense(ctx, second, mint(t, second, nil)); err != nil {
		t.Fatalf("licence of the second app: %v", err)
	}
	type sent struct {
		node, app string
		env       sign.Envelope
	}
	var got []sent
	if err := lf.St.PushLicenses(ctx, func(node, app string, env sign.Envelope) error {
		got = append(got, sent{node, app, env})
		return nil
	}); err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("sends: got %d, want 2 (the two paid apps, not the free one)", len(got))
	}
	for _, s := range got {
		switch s.app {
		case lf.App:
			if s.node != "ctl-1" {
				t.Errorf("%s went to %q, want ctl-1", s.app, s.node)
			}
		case second:
			if s.node != "ctl-2" {
				t.Errorf("%s went to %q, want ctl-2", s.app, s.node)
			}
		default:
			t.Errorf("a licence was sent for %q", s.app)
		}
		if len(s.env.Sig) == 0 {
			t.Errorf("%s: the envelope sent carries no signature", s.app)
		}
	}
	// One node nobody can reach must not cost the others their licence.
	var reached []string
	err := lf.St.PushLicenses(ctx, func(node, app string, env sign.Envelope) error {
		if node == "ctl-1" {
			return errors.New("tunnel down")
		}
		reached = append(reached, node)
		return nil
	})
	if err == nil {
		t.Error("a failed send is not reported")
	}
	if len(reached) != 1 || reached[0] != "ctl-2" {
		t.Errorf("after a failed send: reached %v, want [ctl-2]", reached)
	}
}

func TestInstallKeyGivesOnlyThePublicHalf(t *testing.T) {
	lf := newLicenseFixture(t)
	k, err := lf.St.InstallKey(ctx)
	if err != nil {
		t.Fatalf("install key: %v", err)
	}
	if k.Installation != lf.V.Installation {
		t.Errorf("installation: got %q, want %q", k.Installation, lf.V.Installation)
	}
	if !bytes.Equal(k.Key, installPair.Public) {
		t.Error("the key given out is not the installation's public key")
	}
	b, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	if containsSecret(string(b), installPair.Private) {
		t.Error("the answer carries the private key")
	}
}

// containsSecret reports whether s holds the secret in any of the three shapes
// it could leave in: raw bytes, base64url (as every key travels in NGC) or
// hex. A search for the raw bytes alone would miss the encoded one, which is
// exactly the shape a log line would carry.
func containsSecret(s string, secret []byte) bool {
	if len(secret) == 0 {
		return false
	}
	return strings.Contains(s, string(secret)) ||
		strings.Contains(s, enc.B64.EncodeToString(secret)) ||
		strings.Contains(s, hex.EncodeToString(secret)) ||
		strings.Contains(s, strings.ToUpper(hex.EncodeToString(secret)))
}

// TestNoKeyAndNoLicenseReachesTheLog is the measurement behind the promise
// that nothing secret is written down. It runs every function of this file
// that touches a key, then reads every log entry there is and looks for each
// secret in all three shapes. The content key is 32 bytes, so its base64url
// is 43 characters and its hex 64 -- both well under audit's limit of 100
// bytes for a detail, so a line that carried the whole of it would be written
// and would be found here.
func TestNoKeyAndNoLicenseReachesTheLog(t *testing.T) {
	lf := newLicenseFixture(t)
	body := mint(t, lf.App, nil)
	if _, err := lf.St.TakeLicense(ctx, lf.App, body); err != nil {
		t.Fatalf("take: %v", err)
	}
	if _, err := lf.St.ContentKey(ctx, lf.App); err != nil {
		t.Fatalf("content key: %v", err)
	}
	if _, err := lf.St.InstallKey(ctx); err != nil {
		t.Fatalf("install key: %v", err)
	}
	if _, _, _, err := lf.St.License(ctx, lf.App); err != nil {
		t.Fatalf("licence: %v", err)
	}
	if err := lf.St.PushLicenses(ctx, func(string, string, sign.Envelope) error { return nil }); err != nil {
		t.Fatalf("push: %v", err)
	}
	// A refused licence too: a refusal is the moment a line about the input
	// is most tempting.
	if _, err := lf.St.TakeLicense(ctx, lf.App, lf.reject(t, "other install key")); !errors.Is(err, ErrLicense) {
		t.Fatalf("reject: %v", err)
	}
	entries, err := lf.St.Log.Query(ctx, core.LogQuery{Limit: 500})
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no log entry at all: this case would measure nothing")
	}
	all, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	l, _, _, err := lf.St.License(ctx, lf.App)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct {
		name   string
		secret []byte
	}{
		{"the content key", contentKey},
		{"the installation's private key", installPair.Private},
		{"the sealed content key", l.Key.Sealed},
		{"the one-time key of the wrapping", l.Key.Ephemeral},
	} {
		if containsSecret(string(all), s.secret) {
			t.Errorf("%s is in the log", s.name)
		}
	}
	// The whole envelope must not be in there either, and nor may a detail
	// outside the closed list this file writes.
	for _, e := range entries {
		if containsSecret(e.Detail, body) {
			t.Error("a log entry carries the licence it was handed")
		}
		if e.Kind == core.LogLicense && !licenseNote(e.Detail) {
			t.Errorf("licence entry with a detail outside the list: %q", e.Detail)
		}
	}
	// And the same for the errors: a refusal says nothing about the input.
	_, err = lf.St.TakeLicense(ctx, lf.App, lf.reject(t, "other install key"))
	if containsSecret(err.Error(), installPair.Private) || containsSecret(err.Error(), contentKey) {
		t.Errorf("the error carries a secret: %q", err)
	}
}

// TestOnlyTakeLicenseWritesToTheLog is the second half of the same promise:
// the functions that handle the key write no entry of their own at all, so
// there is no line in which a secret could appear later.
func TestOnlyTakeLicenseWritesToTheLog(t *testing.T) {
	lf := newLicenseFixture(t)
	if _, err := lf.St.TakeLicense(ctx, lf.App, mint(t, lf.App, nil)); err != nil {
		t.Fatalf("take: %v", err)
	}
	before, err := lf.St.Log.Query(ctx, core.LogQuery{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(before) == 0 {
		t.Fatal("TakeLicense wrote nothing: the counter below measures nothing")
	}
	if _, err := lf.St.ContentKey(ctx, lf.App); err != nil {
		t.Fatal(err)
	}
	if _, err := lf.St.InstallKey(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := lf.St.License(ctx, lf.App); err != nil {
		t.Fatal(err)
	}
	if _, _, err := lf.St.UsersAllowed(ctx, lf.App); err != nil {
		t.Fatal(err)
	}
	if err := lf.St.PushLicenses(ctx, func(string, string, sign.Envelope) error { return nil }); err != nil {
		t.Fatal(err)
	}
	after, err := lf.St.Log.Query(ctx, core.LogQuery{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("entries written by the key functions: %d, want 0", len(after)-len(before))
	}
}

// TestALicenseIssuedInTheFutureIsRefused holds the wall in front of the trap a
// wrong clock sets. TakeLicense keeps only the newest licence, and "newest" is
// store.License.Issued -- a number the store writes, which nobody held against
// this API's own clock. A licence issued ahead of the installation (a store
// whose clock runs fast, a licence minted for later, or a clock that was
// stepped forward while it was taken) is therefore kept for good: every later
// licence has a smaller Issued and is turned away as "not newer", while the
// state keeps reading valid until the future licence itself expires.
//
// The tolerance is session.MaxSkew, the same number the whole installation
// uses for two clocks that disagree, so a store a few seconds ahead still
// works. What lies further ahead is no licence of this installation's present.
func TestALicenseIssuedInTheFutureIsRefused(t *testing.T) {
	lf := newLicenseFixture(t)
	// The way to the check must be open: nothing is stored before, so a green
	// case cannot come from an earlier wall.
	if _, ok := lf.keptLicense(t, lf.App); ok {
		t.Fatal("a licence is stored before the case ran")
	}
	// One second within the tolerance is taken: the wall sits at MaxSkew, not
	// at "any second ahead".
	edge := mint(t, lf.App, func(l *store.License) {
		l.Issued = testNow + session.MaxSkew
		l.Expires = l.Issued + 30*86400
	})
	if _, err := lf.St.TakeLicense(ctx, lf.App, edge); err != nil {
		t.Fatalf("licence at the edge of the tolerance: %v", err)
	}
	ahead := mint(t, lf.App, func(l *store.License) {
		l.Issued = testNow + session.MaxSkew + 1
		l.Expires = l.Issued + 30*86400
	})
	if _, err := lf.St.TakeLicense(ctx, lf.App, ahead); !errors.Is(err, ErrLicense) {
		t.Errorf("licence issued past the tolerance: got %v, want ErrLicense", err)
	}
	// And the damage it would do if it came through: nothing of it is stored,
	// so a licence of the present is still newer and still gets in.
	row, ok := lf.keptLicense(t, lf.App)
	if !ok {
		t.Fatal("the licence of the edge is gone")
	}
	if row.Seq != uint64(testNow+session.MaxSkew) {
		t.Errorf("the stored row is at %d, want the edge's %d", row.Seq, testNow+session.MaxSkew)
	}
	if !bytes.Equal(row.Data, edge) {
		t.Error("the licence from the future was stored")
	}
}
