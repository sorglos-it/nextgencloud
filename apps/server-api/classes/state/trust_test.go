package state

import (
	"encoding/json"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

// genesis returns version 1 signed by the passkey admin of trusttest.
func genesis(tb testing.TB) (trusttest.Signer, trust.Signed) {
	tb.Helper()
	admin := trusttest.NewPasskey(9)
	l := trusttest.Genesis(admin)
	return admin, admin.Sign(tb, l)
}

// genesisSeed returns version 1 as a stored row, the seed of the fuzz target.
func genesisSeed(tb testing.TB) []byte {
	tb.Helper()
	_, sg := genesis(tb)
	b, err := json.Marshal(sg)
	if err != nil {
		tb.Fatalf("marshal: %v", err)
	}
	return b
}

func TestTrustWithoutVersion(t *testing.T) {
	s, _ := newState(t)
	if _, err := s.Trust(ctx); err != ErrNoTrust {
		t.Fatalf("empty database answered %v", err)
	}
}

func TestAddVersionAndChain(t *testing.T) {
	s, _ := newState(t)
	admin, first := genesis(t)
	v, err := s.AddVersion(ctx, first)
	if err != nil || v.List.Seq != 1 {
		t.Fatalf("version 1: %v", err)
	}
	next := trusttest.Next(t, first)
	second := admin.Sign(t, next)
	if _, err := s.AddVersion(ctx, second); err != nil {
		t.Fatalf("version 2: %v", err)
	}
	// A version that is not the next one is refused, so no one can replay an
	// old list or skip one.
	if _, err := s.AddVersion(ctx, second); err != ErrOldVersion {
		t.Fatalf("version 2 twice answered %v", err)
	}
	chain, err := s.Chain(ctx, 1)
	if err != nil || len(chain.Versions) != 2 || chain.More {
		t.Fatalf("chain from 1: %v %d %v", err, len(chain.Versions), chain.More)
	}
	if _, err := trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(chain.Versions[0])}, chain.Versions); err != nil {
		t.Fatalf("chain does not verify: %v", err)
	}
	from2, err := s.Chain(ctx, 2)
	if err != nil || len(from2.Versions) != 1 {
		t.Fatalf("chain from 2: %v", err)
	}
	// One past the newest is no error: the caller answers 204.
	if c, err := s.Chain(ctx, 3); err != nil || len(c.Versions) != 0 {
		t.Fatalf("chain from 3: %v %d", err, len(c.Versions))
	}
	if _, err := s.Chain(ctx, 4); err == nil {
		t.Fatal("chain beyond the newest accepted")
	}
}

// TestAddVersionSkipped proves that a gap is refused too: version 3 does not
// go in while version 2 is missing.
func TestAddVersionSkipped(t *testing.T) {
	s, _ := newState(t)
	admin, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	second := admin.Sign(t, trusttest.Next(t, first))
	third := admin.Sign(t, trusttest.Next(t, second))
	if _, err := s.AddVersion(ctx, third); err != ErrOldVersion {
		t.Fatalf("version 3 without version 2 answered %v", err)
	}
}

// TestAddVersionForeignSigner proves that a version signed by a key the
// current list does not hold is refused, and not as ErrOldVersion: the number
// is right, the signer is not.
func TestAddVersionForeignSigner(t *testing.T) {
	s, _ := newState(t)
	_, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	stranger := trusttest.NewSigner(0x5A)
	forged := stranger.Sign(t, trusttest.Next(t, first))
	if _, err := s.AddVersion(ctx, forged); err != trust.ErrSigner {
		t.Fatalf("foreign signer answered %v", err)
	}
}

// TestAddVersionNotOne proves that the first version an installation takes is
// version 1 and nothing else.
func TestAddVersionNotOne(t *testing.T) {
	s, _ := newState(t)
	admin, first := genesis(t)
	second := admin.Sign(t, trusttest.Next(t, first))
	if _, err := s.AddVersion(ctx, second); err != ErrOldVersion {
		t.Fatalf("version 2 as the first one answered %v", err)
	}
}

// TestRefreshSeesAnotherAPI proves that an API notices what another API wrote
// into the shared database.
func TestRefreshSeesAnotherAPI(t *testing.T) {
	s, db := newState(t)
	admin, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	other := New(s.Cfg, db, s.Box, s.Pin)
	other.Now = s.Now
	if _, err := other.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	second := admin.Sign(t, trusttest.Next(t, first))
	if _, err := s.AddVersion(ctx, second); err != nil {
		t.Fatalf("version 2: %v", err)
	}
	v, err := other.Refresh(ctx)
	if err != nil || v.List.Seq != 2 {
		t.Fatalf("other API is at %v: %v", v.List, err)
	}
}

// TestTrustUsesTheCache proves that Trust answers from the cache: a row that
// was removed behind its back does not make it forget the version it verified.
func TestTrustUsesTheCache(t *testing.T) {
	s, db := newState(t)
	_, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	if err := db.BlobDelete(ctx, dbapi.BlobTrust, "1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	v, err := s.Trust(ctx)
	if err != nil || v.List.Seq != 1 {
		t.Fatalf("trust after the row went: %v %v", v.List, err)
	}
	// The answer needs the rows themselves, so Chain says so instead of
	// handing out an empty chain the shell refuses.
	if _, err := s.Chain(ctx, 1); err != ErrSetting {
		t.Fatalf("chain without its rows answered %v", err)
	}
}
