package appstore

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

// The rollback bar is the one wall a signature cannot stand in for, and these
// cases measure what it costs: once a catalog with a high Seq is on record,
// every later catalog of the same store with a lower Seq is refused -- the
// store's own, healthy one included. Who put the high number there does not
// enter into it: the code sees a number, not an attacker.
//
// Every catalog here is signed by this test with a key of its own, because
// that is exactly the threat model: whoever holds the store key writes
// catalogs. The key goes into the trust list for the store, so the signature
// check is passed on purpose and the Seq check is the wall being measured.
//
// What is NOT measured here: the TLS chain. These cases inherit
// fx.St.Client.Transport = fx.HTTP.Client().Transport from newFixture, so the
// chain is not judged. The bar under test is the stored Seq, not the transport.

// storeKeys returns a reproducible ed25519 pair for the store.
func storeKeys(t *testing.T, fill byte) (ed25519.PrivateKey, enc.Bytes) {
	t.Helper()
	_, priv := trusttest.NodeKeys(fill)
	return priv, enc.Bytes(priv.Public().(ed25519.PublicKey))
}

// catalogOf returns the bytes a store would serve: a catalog of id with Seq
// seq, signed with priv.
func catalogOf(t *testing.T, priv ed25519.PrivateKey, id string, seq uint64) []byte {
	t.Helper()
	payload, err := json.Marshal(store.Catalog{Store: id, Seq: seq, Issued: testNow})
	if err != nil {
		t.Fatalf("catalog %d: %v", seq, err)
	}
	env, err := sign.Sign(priv, sign.PurposeCatalog, payload)
	if err != nil {
		t.Fatalf("sign %d: %v", seq, err)
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("envelope %d: %v", seq, err)
	}
	return b
}

// nextList returns the version after the newest one, ready to be changed and
// signed. The hash of the version before it is the one the API verified, so
// the chain holds without this test keeping the signed bytes itself.
func nextList(t *testing.T, fx fixture) *trust.List {
	t.Helper()
	cur, err := fx.S.Trust(ctx)
	if err != nil {
		t.Fatalf("trust: %v", err)
	}
	l := *cur.List
	l.Seq++
	l.Prev = cur.Hash
	l.Issued++
	return &l
}

// setStores puts stores into a new version of the trust list, signed by the
// admin of the genesis list.
func setStores(t *testing.T, fx fixture, stores []trust.Store) {
	t.Helper()
	l := nextList(t, fx)
	l.Stores = stores
	if _, err := fx.S.AddVersion(ctx, trusttest.NewSigner(1).Sign(t, l)); err != nil {
		t.Fatalf("trust version %d: %v", l.Seq, err)
	}
}

// ownStore swaps the store of the trust list for one this test holds the key
// of, keeping ID and URL, and returns the private key.
func ownStore(t *testing.T, fx fixture, v vectors) ed25519.PrivateKey {
	t.Helper()
	priv, pub := storeKeys(t, 0xB7)
	setStores(t, fx, []trust.Store{{ID: v.Store, Name: "NGC Store", URL: fx.HTTP.URL, Key: pub}})
	s := fx.theStore(t)
	if string(s.Key) != string(pub) {
		t.Fatal("the trust list did not take the test's store key")
	}
	return priv
}

// TestAHighSeqLocksTheHealthyStoreOut is the trap itself, in the order it
// happens in the field, with the intermediate step that makes the last one a
// rollback at all.
func TestAHighSeqLocksTheHealthyStoreOut(t *testing.T) {
	fx, v := newFixture(t)
	priv := ownStore(t, fx, v)
	st := fx.theStore(t)

	// 1. The healthy store, Seq 1. This is the intermediate check: the same
	// store, the same key, the same catalog shape goes through, so a refusal
	// further down is not the signature, the URL or the shape.
	fx.setFile(CatalogFile, catalogOf(t, priv, v.Store, 1))
	if err := fx.St.refreshOne(ctx, st); err != nil {
		t.Fatalf("the healthy store at Seq 1 was refused, so nothing below measures the Seq bar: %v", err)
	}
	if got := fx.keptSeq(t, v.Store); got != 1 {
		t.Fatalf("kept Seq after the healthy store: got %d, want 1", got)
	}

	// 2. A catalog with Seq 57, signed with the same key.
	fx.setFile(CatalogFile, catalogOf(t, priv, v.Store, 57))
	if err := fx.St.refreshOne(ctx, st); err != nil {
		t.Fatalf("Seq 57 was refused, so the trap is not set: %v", err)
	}
	if got := fx.keptSeq(t, v.Store); got != 57 {
		t.Fatalf("kept Seq after 57: got %d, want 57", got)
	}

	// 3. The healthy store again, unchanged, Seq 1.
	fx.setFile(CatalogFile, catalogOf(t, priv, v.Store, 1))
	before := fx.hits()
	err := fx.St.refreshOne(ctx, st)
	if fx.hits() == before {
		t.Fatal("refused before the request: this measures an earlier wall")
	}
	if !errors.Is(err, store.ErrRollback) {
		t.Fatalf("got %v, want store.ErrRollback", err)
	}
	if got := fx.keptSeq(t, v.Store); got != 57 {
		t.Fatalf("kept Seq after the healthy store came back: got %d, want 57", got)
	}

	// 4. And it stays that way: a second and a third try change nothing.
	for i := 0; i < 2; i++ {
		if err := fx.St.refreshOne(ctx, st); !errors.Is(err, store.ErrRollback) {
			t.Fatalf("try %d: got %v, want store.ErrRollback", i+2, err)
		}
	}

	// Refresh itself reports nil over the same store: err == nil must never
	// be read as "the catalog was taken".
	if err := fx.St.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: got %v, want nil", err)
	}
	if got := fx.keptSeq(t, v.Store); got != 57 {
		t.Fatalf("kept Seq after Refresh: got %d, want 57", got)
	}
	// The reason an admin is shown, in German.
	src, err := fx.St.Sources(ctx)
	if err != nil {
		t.Fatalf("Sources: %v", err)
	}
	if len(src) != 1 || src[0].Error == "" || src[0].Seq != 57 {
		t.Fatalf("Sources: %+v", src)
	}
	t.Logf("locked out: seq=%d reason=%q", src[0].Seq, src[0].Error)
}

// TestTheSameSeqGoesThroughWithoutTheHighOne is the counter-measurement to the
// case above: Seq 1 against a stored 1 is no rollback, so step 3 there fails
// because of the 57 and for no other reason.
func TestTheSameSeqGoesThroughWithoutTheHighOne(t *testing.T) {
	fx, v := newFixture(t)
	priv := ownStore(t, fx, v)
	st := fx.theStore(t)
	fx.setFile(CatalogFile, catalogOf(t, priv, v.Store, 1))
	if err := fx.St.refreshOne(ctx, st); err != nil {
		t.Fatalf("first time: %v", err)
	}
	before := fx.hits()
	if err := fx.St.refreshOne(ctx, st); err != nil {
		t.Fatalf("the same Seq a second time: got %v, want nil", err)
	}
	if fx.hits() == before {
		t.Fatal("no request went out, so nothing was judged")
	}
	if got := fx.keptSeq(t, v.Store); got != 1 {
		t.Fatalf("kept Seq: got %d, want 1", got)
	}
}

// TestNoAttackerIsNeededForTheLockout: the high number need not come from
// anybody's attack. A store that counts its own catalog up by mistake -- a
// test run against the live store, a restored backup, a second machine
// serving -- and is then put right again produces the same lockout, with its
// own key throughout. The code cannot tell the two apart, and that is the
// finding.
func TestNoAttackerIsNeededForTheLockout(t *testing.T) {
	fx, v := newFixture(t)
	priv := ownStore(t, fx, v)
	st := fx.theStore(t)
	// The store at its true number.
	fx.setFile(CatalogFile, catalogOf(t, priv, v.Store, 7))
	if err := fx.St.refreshOne(ctx, st); err != nil {
		t.Fatalf("Seq 7: %v", err)
	}
	// One catalog from the store's own test run, numbered far too high, and
	// served for one single refresh.
	fx.setFile(CatalogFile, catalogOf(t, priv, v.Store, 57))
	if err := fx.St.refreshOne(ctx, st); err != nil {
		t.Fatalf("Seq 57 from the store's own mistake: %v", err)
	}
	// The mistake is noticed and undone: the store serves its true catalog
	// again, one number further on than before.
	fx.setFile(CatalogFile, catalogOf(t, priv, v.Store, 8))
	if err := fx.St.refreshOne(ctx, st); !errors.Is(err, store.ErrRollback) {
		t.Fatalf("got %v, want store.ErrRollback", err)
	}
	if got := fx.keptSeq(t, v.Store); got != 57 {
		t.Fatalf("kept Seq: got %d, want 57", got)
	}
	// It does not even heal when the store keeps counting: 9, 10, ... are all
	// below 57, and 49 more catalogs is years of a store's life.
	for _, seq := range []uint64{9, 10, 56} {
		fx.setFile(CatalogFile, catalogOf(t, priv, v.Store, seq))
		if err := fx.St.refreshOne(ctx, st); !errors.Is(err, store.ErrRollback) {
			t.Fatalf("Seq %d: got %v, want store.ErrRollback", seq, err)
		}
	}
	// 57 itself goes through again, because the bar is "lower than", not
	// "not newer than". That is the only way out that needs no admin: the
	// store has to count past the number somebody else put on record.
	fx.setFile(CatalogFile, catalogOf(t, priv, v.Store, 57))
	if err := fx.St.refreshOne(ctx, st); err != nil {
		t.Fatalf("Seq 57 again: got %v, want nil", err)
	}
}

// TestRemovingAndReaddingTheStoreIsTheWayBack is the way back, and it is the
// only one an administrator can walk through an action: the trust list
// (ngc.users/api/trust). Taking the store out drops the catalog on record
// (Stores.forget), so the store put back under the SAME ID is measured from
// zero again and its healthy catalog is taken.
//
// Before forget existed this case read the other way round: the row survived
// the removal, and Seq 1 after it was store.ErrRollback for good.
func TestRemovingAndReaddingTheStoreIsTheWayBack(t *testing.T) {
	fx, v := newFixture(t)
	priv := ownStore(t, fx, v)
	st := fx.theStore(t)
	fx.setFile(CatalogFile, catalogOf(t, priv, v.Store, 57))
	if err := fx.St.refreshOne(ctx, st); err != nil {
		t.Fatalf("Seq 57: %v", err)
	}
	// The intermediate check: the trap is really set before the way back is
	// walked, so a success below is not a store that was never locked out.
	fx.setFile(CatalogFile, catalogOf(t, priv, v.Store, 1))
	if err := fx.St.refreshOne(ctx, st); !errors.Is(err, store.ErrRollback) {
		t.Fatalf("the trap is not set: got %v, want store.ErrRollback", err)
	}

	// Out of the trust list, one refresh.
	setStores(t, fx, nil)
	if err := fx.St.Refresh(ctx); err != nil {
		t.Fatalf("Refresh without stores: %v", err)
	}
	if got := fx.keptSeq(t, v.Store); got != 0 {
		t.Fatalf("the catalog of the removed store is still on record: Seq %d", got)
	}

	// Back in, same ID, same key, same address. The healthy catalog is taken.
	setStores(t, fx, []trust.Store{{ID: v.Store, Name: "NGC Store", URL: fx.HTTP.URL,
		Key: enc.Bytes(priv.Public().(ed25519.PublicKey))}})
	st = fx.theStore(t)
	before := fx.hits()
	if err := fx.St.refreshOne(ctx, st); err != nil {
		t.Fatalf("after the way back the healthy store is still refused: %v", err)
	}
	if fx.hits() == before {
		t.Fatal("no request went out, so nothing was judged")
	}
	if got := fx.keptSeq(t, v.Store); got != 1 {
		t.Fatalf("kept Seq: got %d, want 1", got)
	}
}

// TestForgettingSparesTheStoresOfTheList is the other half of forget: it must
// take the row of the store that LEFT and no other. Two stores, one removed,
// and the one that stays keeps its number -- otherwise every refresh would
// quietly clear the bar of every store.
func TestForgettingSparesTheStoresOfTheList(t *testing.T) {
	fx, v := newFixture(t)
	priv := ownStore(t, fx, v)
	pub := enc.Bytes(priv.Public().(ed25519.PublicKey))
	const second = "zweiter"
	setStores(t, fx, []trust.Store{
		{ID: v.Store, Name: "NGC Store", URL: fx.HTTP.URL, Key: pub},
		{ID: second, Name: "Zweiter Store", URL: fx.HTTP.URL, Key: pub},
	})
	// Both have a catalog on record.
	for _, s := range []struct {
		id  string
		seq uint64
	}{{v.Store, 11}, {second, 22}} {
		fx.setFile(CatalogFile, catalogOf(t, priv, s.id, s.seq))
		var found bool
		ver, err := fx.S.Trust(ctx)
		if err != nil {
			t.Fatalf("trust: %v", err)
		}
		for _, ts := range ver.List.Stores {
			if ts.ID != s.id {
				continue
			}
			found = true
			if err := fx.St.refreshOne(ctx, ts); err != nil {
				t.Fatalf("%s at Seq %d: %v", s.id, s.seq, err)
			}
		}
		if !found {
			t.Fatalf("the trust list does not name %q", s.id)
		}
		if got := fx.keptSeq(t, s.id); got != s.seq {
			t.Fatalf("%s: kept Seq %d, want %d", s.id, got, s.seq)
		}
	}
	// Only the second one leaves.
	setStores(t, fx, []trust.Store{{ID: v.Store, Name: "NGC Store", URL: fx.HTTP.URL, Key: pub}})
	fx.setFile(CatalogFile, catalogOf(t, priv, v.Store, 11))
	if err := fx.St.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got := fx.keptSeq(t, v.Store); got != 11 {
		t.Fatalf("the store that stayed lost its number: Seq %d, want 11", got)
	}
	if got := fx.keptSeq(t, second); got != 0 {
		t.Fatalf("the store that left kept its number: Seq %d", got)
	}
}

// TestAnotherStoreIDStartsAtZero is the way back that does work today, and
// what it costs: a store added under a NEW ID has no row of its own, so its
// Seq starts at 0 and its catalog is taken. The price is the ID itself --
// dbapi.App.Store names the store an installed app came from, so the rows of
// the apps already installed go on naming the old ID.
func TestAnotherStoreIDStartsAtZero(t *testing.T) {
	fx, v := newFixture(t)
	priv := ownStore(t, fx, v)
	st := fx.theStore(t)
	fx.setFile(CatalogFile, catalogOf(t, priv, v.Store, 57))
	if err := fx.St.refreshOne(ctx, st); err != nil {
		t.Fatalf("Seq 57: %v", err)
	}
	const other = "global2"
	setStores(t, fx, []trust.Store{{ID: other, Name: "NGC Store", URL: fx.HTTP.URL,
		Key: enc.Bytes(priv.Public().(ed25519.PublicKey))}})
	st2 := fx.theStore(t)
	if st2.ID != other {
		t.Fatalf("store ID: got %q, want %q", st2.ID, other)
	}
	fx.setFile(CatalogFile, catalogOf(t, priv, other, 1))
	if err := fx.St.refreshOne(ctx, st2); err != nil {
		t.Fatalf("the new ID was refused too: %v", err)
	}
	if got := fx.keptSeq(t, other); got != 1 {
		t.Fatalf("kept Seq of the new ID: got %d, want 1", got)
	}
	// The old row is still there, untouched, for whenever the old ID returns.
	if got := fx.keptSeq(t, v.Store); got != 57 {
		t.Fatalf("the row of the old ID: got %d, want 57", got)
	}
	_ = st
}

// TestDeletingTheCatalogRowIsTheWayBack measures what a reset would have to
// do, and that the database layer allows it: with the row gone, the healthy
// store is taken again. No action of core.All() does this today -- the only
// caller of BlobDelete over a catalog would be a reset that is not built.
func TestDeletingTheCatalogRowIsTheWayBack(t *testing.T) {
	fx, v := newFixture(t)
	priv := ownStore(t, fx, v)
	st := fx.theStore(t)
	fx.setFile(CatalogFile, catalogOf(t, priv, v.Store, 57))
	if err := fx.St.refreshOne(ctx, st); err != nil {
		t.Fatalf("Seq 57: %v", err)
	}
	fx.setFile(CatalogFile, catalogOf(t, priv, v.Store, 1))
	if err := fx.St.refreshOne(ctx, st); !errors.Is(err, store.ErrRollback) {
		t.Fatalf("the trap is not set: got %v", err)
	}
	if err := fx.S.DB.BlobDelete(ctx, dbapi.BlobCatalog, v.Store); err != nil {
		t.Fatalf("BlobDelete: %v", err)
	}
	if got := fx.keptSeq(t, v.Store); got != 0 {
		t.Fatalf("the row survived the delete: Seq %d", got)
	}
	if err := fx.St.refreshOne(ctx, st); err != nil {
		t.Fatalf("after the reset the healthy store is still refused: %v", err)
	}
	if got := fx.keptSeq(t, v.Store); got != 1 {
		t.Fatalf("kept Seq: got %d, want 1", got)
	}
}
