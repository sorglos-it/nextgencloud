package chain

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
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

// maxVectors bounds shared/testdata/trust.json while it is read. Its 24 cases
// need about 120 KiB; the room is generous so a new case does not fail the
// test for its size, and it still keeps a damaged file from being decoded.
const maxVectors = 4 << 20

// vector is one case of shared/testdata/trust.json, with the fields this test
// needs. The file is decoded strictly, so every member it has stands here.
type vector struct {
	Name   string         `json:"name"`
	Anchor trust.Anchor   `json:"anchor"`
	Chain  []trust.Signed `json:"chain"`
	Valid  bool           `json:"valid"`
	Seq    uint64         `json:"seq,omitempty"`
}

// newKeeper returns a keeper over a fresh state and that state.
func newKeeper(t *testing.T, a trust.Anchor) (*Keeper, *state.Store) {
	t.Helper()
	st := state.New(memstore.New())
	t.Cleanup(func() { _ = st.Close() })
	return New(st, a), st
}

func TestAgainstVectors(t *testing.T) {
	ctx := context.Background()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "shared", "testdata", "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vs []vector
	if err := enc.DecodeStrict(b, maxVectors, &vs); err != nil {
		t.Fatal(err)
	}
	if len(vs) == 0 {
		t.Fatal("trust.json holds no case")
	}
	for _, v := range vs {
		k, st := newKeeper(t, v.Anchor)
		// Nothing is stored yet, so the keeper starts empty and waits.
		if err := k.Load(ctx); err != nil || k.Seq() != 0 {
			t.Fatalf("%s: Load of an empty state: seq = %d, err = %v", v.Name, k.Seq(), err)
		}
		body, err := json.Marshal(session.TrustChain{Versions: v.Chain})
		if err != nil {
			t.Fatal(err)
		}
		seq, err := k.Apply(ctx, body)
		switch {
		case v.Valid && (err != nil || seq != v.Seq):
			t.Errorf("%s: Apply = %d, %v, want %d, nil", v.Name, seq, err, v.Seq)
			continue
		case !v.Valid && err == nil:
			t.Errorf("%s: Apply took a chain the contract calls invalid", v.Name)
			continue
		}
		want := uint64(0)
		if v.Valid {
			want = v.Seq
		}
		if k.Seq() != want {
			t.Errorf("%s: keeper holds %d, want %d", v.Name, k.Seq(), want)
		}
		// What was taken has to come back on its own after a restart; what was
		// refused must have left nothing behind.
		again := New(st, v.Anchor)
		if err := again.Load(ctx); err != nil {
			t.Errorf("%s: after a restart: %v", v.Name, err)
			continue
		}
		if again.Seq() != want {
			t.Errorf("%s: after a restart the keeper holds %d, want %d", v.Name, again.Seq(), want)
		}
	}
}

// FuzzApply throws bytes at a keeper whose anchor names a hash no payload can
// have, so nothing may ever get through: a path that skipped the anchor would
// show here as a chain that was taken.
func FuzzApply(f *testing.F) {
	f.Add([]byte(`{"versions":[]}`))
	admin := trusttest.NewSigner(1)
	if b, err := json.Marshal(session.TrustChain{Versions: []trust.Signed{admin.Sign(f, trusttest.Genesis(admin))}}); err == nil {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		ctx := context.Background()
		st := state.New(memstore.New())
		k := New(st, trust.Anchor{Seq: 1, Hash: make([]byte, 32)})
		if seq, err := k.Apply(ctx, b); err == nil {
			t.Fatalf("took version %d although no payload hashes to the anchor", seq)
		}
		if k.Seq() != 0 {
			t.Fatalf("keeper holds %d after a refused chain", k.Seq())
		}
		// A refused chain leaves nothing in the state either, so the next
		// start finds no version the keeper never verified.
		if seq, err := st.NewestSeq(ctx); err != nil || seq != 0 {
			t.Fatalf("a refused chain left version %d behind (%v)", seq, err)
		}
	})
}

// FuzzApplyFromAnchor fuzzes against the anchor of a real version, so the
// accepting path is reachable: whatever the keeper takes has to verify again
// from the state on its own.
func FuzzApplyFromAnchor(f *testing.F) {
	admin := trusttest.NewSigner(1)
	v1 := admin.Sign(f, trusttest.Genesis(admin))
	v2 := admin.Sign(f, trusttest.Next(f, v1))
	anchor := trust.Anchor{Seq: 1, Hash: trust.Hash(v1)}
	for _, versions := range [][]trust.Signed{{v1}, {v1, v2}} {
		b, err := json.Marshal(session.TrustChain{Versions: versions})
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		ctx := context.Background()
		st := state.New(memstore.New())
		k := New(st, anchor)
		seq, err := k.Apply(ctx, b)
		if err != nil {
			return
		}
		if seq != k.Seq() {
			t.Fatalf("Apply returned %d, the keeper holds %d", seq, k.Seq())
		}
		again := New(st, anchor)
		if err := again.Load(ctx); err != nil {
			t.Fatalf("what Apply took came back as %v", err)
		}
		if again.Seq() != seq {
			t.Fatalf("after a restart the keeper holds %d, want %d", again.Seq(), seq)
		}
	})
}
