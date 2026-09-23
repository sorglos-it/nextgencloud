package chain

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

func TestApplyAdvancesAndPersists(t *testing.T) {
	ctx := context.Background()
	admin := trusttest.NewSigner(1)
	v1 := admin.Sign(t, trusttest.Genesis(admin))
	st := state.New(memstore.New())
	if err := st.PutVersion(ctx, v1, 1); err != nil {
		t.Fatal(err)
	}
	k := New(st, trust.Anchor{Seq: 1, Hash: trust.Hash(v1)})
	if err := k.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if k.Seq() != 1 {
		t.Fatalf("seq = %d, want 1", k.Seq())
	}
	v2 := admin.Sign(t, trusttest.Next(t, v1))
	b, err := json.Marshal(session.TrustChain{Versions: []trust.Signed{v2}})
	if err != nil {
		t.Fatal(err)
	}
	seq, err := k.Apply(ctx, b)
	if err != nil || seq != 2 {
		t.Fatalf("Apply = %d, %v, want 2, nil", seq, err)
	}
	// Nach einem Neustart muss Fassung 2 aus dem Zustand kommen.
	k2 := New(st, trust.Anchor{Seq: 1, Hash: trust.Hash(v1)})
	if err := k2.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if k2.Seq() != 2 {
		t.Fatalf("after reload seq = %d, want 2", k2.Seq())
	}
}

func TestApplyRefusesForgedVersion(t *testing.T) {
	// Fassung 2 von einem fremden Schlüssel unterschrieben: Apply muss scheitern
	// und die gehaltene Fassung darf 1 bleiben.
	ctx := context.Background()
	admin, other := trusttest.NewSigner(1), trusttest.NewSigner(9)
	v1 := admin.Sign(t, trusttest.Genesis(admin))
	st := state.New(memstore.New())
	if err := st.PutVersion(ctx, v1, 1); err != nil {
		t.Fatal(err)
	}
	k := New(st, trust.Anchor{Seq: 1, Hash: trust.Hash(v1)})
	if err := k.Load(ctx); err != nil {
		t.Fatal(err)
	}
	bad := other.Sign(t, trusttest.Next(t, v1))
	b, _ := json.Marshal(session.TrustChain{Versions: []trust.Signed{bad}})
	if _, err := k.Apply(ctx, b); err == nil {
		t.Fatal("Apply took a version signed by an unknown key")
	}
	if k.Seq() != 1 {
		t.Fatalf("seq = %d after a refused version, want 1", k.Seq())
	}
}
