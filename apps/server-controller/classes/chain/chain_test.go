package chain

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
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
	if k.TrustSeq() != 1 {
		t.Fatalf("seq = %d, want 1", k.TrustSeq())
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
	if k2.TrustSeq() != 2 {
		t.Fatalf("after reload seq = %d, want 2", k2.TrustSeq())
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
	if k.TrustSeq() != 1 {
		t.Fatalf("seq = %d after a refused version, want 1", k.TrustSeq())
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
		// Nothing is stored yet, so the keeper starts empty and waits — and it
		// asks for the chain from its anchor on, which is what the number
		// below the anchor says. The two cases with an anchor of 2 are what
		// hold this apart from a plain 0.
		if err := k.Load(ctx); err != nil || k.TrustSeq() != v.Anchor.Seq-1 {
			t.Fatalf("%s: Load of an empty state: seq = %d, want %d, err = %v",
				v.Name, k.TrustSeq(), v.Anchor.Seq-1, err)
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
		// What was refused leaves the keeper asking from its anchor on, the
		// same as before the chain arrived.
		want := v.Anchor.Seq - 1
		if v.Valid {
			want = v.Seq
		}
		if k.TrustSeq() != want {
			t.Errorf("%s: keeper holds %d, want %d", v.Name, k.TrustSeq(), want)
		}
		// What was taken has to come back on its own after a restart; what was
		// refused must have left nothing behind.
		again := New(st, v.Anchor)
		if err := again.Load(ctx); err != nil {
			t.Errorf("%s: after a restart: %v", v.Name, err)
			continue
		}
		if again.TrustSeq() != want {
			t.Errorf("%s: after a restart the keeper holds %d, want %d", v.Name, again.TrustSeq(), want)
		}
		if len(v.Chain) < 2 {
			continue
		}
		// The same case over the other path: the first version opens the chain
		// from the anchor, the rest advances from what the keeper then holds.
		// A forgery further along has to be turned away here too.
		split, _ := newKeeper(t, v.Anchor)
		if err := split.Load(ctx); err != nil {
			t.Errorf("%s: Load of an empty state: %v", v.Name, err)
			continue
		}
		_, first := split.Apply(ctx, chainBytes(t, v.Chain[0]))
		last, rest := split.Apply(ctx, chainBytes(t, v.Chain[1:]...))
		switch {
		case v.Valid && (first != nil || rest != nil || last != v.Seq):
			t.Errorf("%s: over Advance = %d, %v / %v, want %d, nil", v.Name, last, first, rest, v.Seq)
		case !v.Valid && first == nil && rest == nil:
			t.Errorf("%s: Advance took a chain the contract calls invalid", v.Name)
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
		// Apply runs only on a keeper that read the state; on an empty one
		// that changes nothing, so the anchor stays the only thing in the way.
		if err := k.Load(ctx); err != nil || k.TrustSeq() != 0 {
			t.Fatalf("Load of an empty state: seq = %d, err = %v", k.TrustSeq(), err)
		}
		if seq, err := k.Apply(ctx, b); err == nil {
			t.Fatalf("took version %d although no payload hashes to the anchor", seq)
		}
		if k.TrustSeq() != 0 {
			t.Fatalf("keeper holds %d after a refused chain", k.TrustSeq())
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
		if err := k.Load(ctx); err != nil {
			t.Fatalf("Load of an empty state: %v", err)
		}
		seq, err := k.Apply(ctx, b)
		if err != nil {
			return
		}
		if seq != k.TrustSeq() {
			t.Fatalf("Apply returned %d, the keeper holds %d", seq, k.TrustSeq())
		}
		again := New(st, anchor)
		if err := again.Load(ctx); err != nil {
			t.Fatalf("what Apply took came back as %v", err)
		}
		if again.TrustSeq() != seq {
			t.Fatalf("after a restart the keeper holds %d, want %d", again.TrustSeq(), seq)
		}
	})
}

func TestReadWhileApplying(t *testing.T) {
	ctx := context.Background()
	admin := trusttest.NewSigner(1)
	v1 := admin.Sign(t, trusttest.Genesis(admin))
	k, st := newKeeper(t, trust.Anchor{Seq: 1, Hash: trust.Hash(v1)})
	if err := st.PutVersion(ctx, v1, 1); err != nil {
		t.Fatal(err)
	}
	if err := k.Load(ctx); err != nil {
		t.Fatal(err)
	}
	bodies := make([][]byte, 0, 8)
	for prev := v1; len(bodies) < cap(bodies); {
		prev = admin.Sign(t, trusttest.Next(t, prev))
		b, err := json.Marshal(session.TrustChain{Versions: []trust.Signed{prev}})
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, b)
	}
	// Several tunnels read the held version while one chain is applied. The
	// version they see may only ever move forward, and -race (on the Linux
	// servers) has to find nothing here.
	var (
		wg      sync.WaitGroup
		back    atomic.Bool
		stop    = make(chan struct{})
		readers = 4
	)
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			last := uint64(0)
			for {
				select {
				case <-stop:
					return
				default:
				}
				if v, ok := k.Current(); ok {
					if v.List.Seq < last {
						back.Store(true)
						return
					}
					last = v.List.Seq
				}
				_, _ = k.List(), k.TrustSeq()
			}
		}()
	}
	for _, b := range bodies {
		if _, err := k.Apply(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	if back.Load() {
		t.Fatal("a reader saw the held version go backwards")
	}
	if k.TrustSeq() != uint64(1+len(bodies)) {
		t.Fatalf("seq = %d, want %d", k.TrustSeq(), 1+len(bodies))
	}
}

// chainBytes packs versions as the body of a session.TrustChain, the way an
// API sends them.
func chainBytes(t testing.TB, versions ...trust.Signed) []byte {
	t.Helper()
	b, err := json.Marshal(session.TrustChain{Versions: versions})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// errStopped is what the local state gives once it takes no more writes. A
// full disk, a closed database and the cancelled context of a tunnel all look
// like this to the keeper.
var errStopped = errors.New("test: the database takes no more writes")

// haltingStore is a local state that can stop in the middle: writesLeft counts
// the writes that still go through (below zero: all of them), and empty makes
// it hand out no rows, as a database wiped under a running keeper.
type haltingStore struct {
	dbapi.Local
	writesLeft int
	empty      bool
}

func newHaltingStore() *haltingStore {
	return &haltingStore{Local: memstore.New(), writesLeft: -1}
}

func (s *haltingStore) BlobPut(ctx context.Context, b dbapi.Blob) error {
	if s.writesLeft == 0 {
		return errStopped
	}
	if s.writesLeft > 0 {
		s.writesLeft--
	}
	return s.Local.BlobPut(ctx, b)
}

func (s *haltingStore) BlobFrom(ctx context.Context, kind string, seq uint64) ([]dbapi.Blob, error) {
	if s.empty {
		return nil, nil
	}
	return s.Local.BlobFrom(ctx, kind, seq)
}

// TestHalfWrittenChainStaysReadable plays through a write that fails in the
// middle: versions 2 and 3 are stored, version 4 is not, and the keeper still
// holds version 1. dbapi.Local has no transaction, so the half chain stays.
// Nothing may now take a second, different version 2 — it would replace the
// stored one, version 3 would point at a version that is gone, and no later
// start could read the list again.
func TestHalfWrittenChainStaysReadable(t *testing.T) {
	ctx := context.Background()
	admin := trusttest.NewSigner(1)
	v1 := admin.Sign(t, trusttest.Genesis(admin))
	v2 := admin.Sign(t, trusttest.Next(t, v1))
	v3 := admin.Sign(t, trusttest.Next(t, v2))
	v4 := admin.Sign(t, trusttest.Next(t, v3))
	db := newHaltingStore()
	st := state.New(db)
	anchor := trust.Anchor{Seq: 1, Hash: trust.Hash(v1)}
	if err := st.PutVersion(ctx, v1, 1); err != nil {
		t.Fatal(err)
	}
	k := New(st, anchor)
	if err := k.Load(ctx); err != nil {
		t.Fatal(err)
	}
	db.writesLeft = 2 // versions 2 and 3 go in, version 4 does not
	if _, err := k.Apply(ctx, chainBytes(t, v2, v3, v4)); err == nil {
		t.Fatal("Apply reported success although a write failed")
	}
	db.writesLeft = -1
	// A second version 2: the same predecessor, another time of issue, signed
	// by the same admin. The verification cannot turn it away — the keeper
	// still holds version 1, so it fits — and the number it would be stored
	// under is taken.
	l := trusttest.Next(t, v1)
	l.Issued++
	other2 := admin.Sign(t, l)
	if _, err := k.Apply(ctx, chainBytes(t, other2)); err == nil {
		t.Fatal("Apply took a second version 2 over one that is already stored")
	}
	// What was stored is a chain from the anchor on, so a restart picks it up
	// instead of failing for good.
	again := New(st, anchor)
	if err := again.Load(ctx); err != nil {
		t.Fatalf("the stored chain is unreadable after a failed write: %v", err)
	}
	if again.TrustSeq() != 3 {
		t.Fatalf("after a restart the keeper holds %d, want 3", again.TrustSeq())
	}
}

// TestLoadForgetsWhatTheStateLost holds Load to what is stored: comes the
// state back empty, the keeper may not keep a version from before. It would
// store the next one under a number with nothing before it, and that gap no
// later Load could read past.
func TestLoadForgetsWhatTheStateLost(t *testing.T) {
	ctx := context.Background()
	admin := trusttest.NewSigner(1)
	v1 := admin.Sign(t, trusttest.Genesis(admin))
	db := newHaltingStore()
	st := state.New(db)
	if err := st.PutVersion(ctx, v1, 1); err != nil {
		t.Fatal(err)
	}
	k := New(st, trust.Anchor{Seq: 1, Hash: trust.Hash(v1)})
	if err := k.Load(ctx); err != nil || k.TrustSeq() != 1 {
		t.Fatalf("Load = %v, seq = %d, want nil, 1", err, k.TrustSeq())
	}
	db.empty = true
	if err := k.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if k.TrustSeq() != 0 {
		t.Fatalf("seq = %d after the state came back empty, want 0", k.TrustSeq())
	}
}

// TestApplyTellsStoreErrorsApart keeps a failed write apart from a failed
// verification. The caller answers the errors of trust with E_VERIFY, whose
// text reads "Unterschrift ungültig."; an admin who reads that after a full
// disk signs the change again, and a second version under a stored number is
// exactly what breaks the chain for good.
func TestApplyTellsStoreErrorsApart(t *testing.T) {
	ctx := context.Background()
	admin := trusttest.NewSigner(1)
	v1 := admin.Sign(t, trusttest.Genesis(admin))
	db := newHaltingStore()
	k := New(state.New(db), trust.Anchor{Seq: 1, Hash: trust.Hash(v1)})
	if err := k.Load(ctx); err != nil {
		t.Fatal(err)
	}
	db.writesLeft = 0 // nothing goes in any more
	_, err := k.Apply(ctx, chainBytes(t, v1))
	if !errors.Is(err, ErrStore) {
		t.Fatalf("Apply on a state that takes no writes = %v, want ErrStore", err)
	}
	// It must look like none of the errors that mean a bad version.
	for _, verify := range []error{trust.ErrAnchor, trust.ErrChain, trust.ErrSigner,
		trust.ErrInvalid, proof.ErrSignature, session.ErrMessage} {
		if errors.Is(err, verify) {
			t.Fatalf("a failed write reports %v", verify)
		}
	}
	// The reason stays readable for the server's own diagnosis.
	if !errors.Is(err, errStopped) {
		t.Fatalf("Apply dropped the reason of the failed write: %v", err)
	}
}

// TestAFreshKeeperAsksFromItsAnchorOn holds the number a controller reports
// between pairing and its first chain, and the reason it is not 0: an API
// answers a hello with the versions after TrustSeq, and the first of them has
// to be the anchored one. A controller paired once the list has moved past its
// first version is anchored above 1, so 0 would ask for a chain it must
// refuse.
func TestAFreshKeeperAsksFromItsAnchorOn(t *testing.T) {
	ctx := context.Background()
	admin := trusttest.NewSigner(1)
	v1 := admin.Sign(t, trusttest.Genesis(admin))
	v2 := admin.Sign(t, trusttest.Next(t, v1))
	v3 := admin.Sign(t, trusttest.Next(t, v2))

	k, _ := newKeeper(t, trust.Anchor{Seq: 2, Hash: trust.Hash(v2)})
	if err := k.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if got := k.TrustSeq(); got != 1 {
		t.Fatalf("a keeper anchored at 2 and holding nothing asks from %d on, want 2", got+1)
	}
	if k.List() != nil {
		t.Fatal("a keeper that verified nothing hands out a list")
	}
	// What an API sends after a reported 0: the chain from version 1 on. It is
	// signed just as well as any other and is still not this controller's
	// chain, so it is the sender's fault and not this server's — which is why
	// it may not look like the anchor this server itself carries.
	_, err := k.Apply(ctx, chainBytes(t, v1, v2))
	if !errors.Is(err, trust.ErrAnchor) {
		t.Fatalf("a chain from version 1 to a keeper anchored at 2 = %v, want trust.ErrAnchor", err)
	}
	if errors.Is(err, ErrNoAnchor) {
		t.Fatal("a chain that misses the anchor is reported as this server's own anchor failing")
	}
	if got := k.TrustSeq(); got != 1 {
		t.Fatalf("after the refused chain the keeper asks from %d on, want 2", got+1)
	}
	// And what it sends after the reported 1: the chain from the anchored
	// version on, which is the one trust.Open takes.
	seq, err := k.Apply(ctx, chainBytes(t, v2, v3))
	if err != nil || seq != 3 {
		t.Fatalf("the chain from the anchor on = %d, %v, want 3, nil", seq, err)
	}
	if got := k.TrustSeq(); got != 3 {
		t.Fatalf("the keeper reports %d, want the version it verified", got)
	}
}

// TestAnAnchorThatPinsNothingIsThisServersFault holds the other half of the
// split: an anchor without a number or without a hash is this installation's
// own, and it may not be told as a fault of what arrived (ErrNoAnchor ->
// E_INTERNAL, trust.ErrAnchor -> E_VERIFY).
func TestAnAnchorThatPinsNothingIsThisServersFault(t *testing.T) {
	ctx := context.Background()
	admin := trusttest.NewSigner(1)
	v1 := admin.Sign(t, trusttest.Genesis(admin))
	for _, a := range []trust.Anchor{
		{Seq: 0, Hash: trust.Hash(v1)},
		{Seq: 1},
		{Seq: 1, Hash: []byte{0x01}},
	} {
		k, _ := newKeeper(t, a)
		if err := k.Load(ctx); !errors.Is(err, ErrNoAnchor) {
			t.Errorf("Load with anchor %+v = %v, want ErrNoAnchor", a, err)
		}
		if _, err := k.Apply(ctx, chainBytes(t, v1)); !errors.Is(err, ErrNoAnchor) {
			t.Errorf("Apply with anchor %+v = %v, want ErrNoAnchor", a, err)
		}
		// Nothing below 1 exists to ask from, so such a keeper reports 0 and
		// the number never wraps around.
		if a.Seq == 0 && k.TrustSeq() != 0 {
			t.Errorf("a keeper without an anchor number reports %d, want 0", k.TrustSeq())
		}
	}
}

// TestApplyRefusesUntilTheStateWasRead holds what Load is for: a keeper that
// never read the state does not know what is already stored, so it may not
// verify from the anchor and write the first version over one that is there.
// It answers ErrStore, which the caller names E_INTERNAL — this server's
// fault, not the sender's.
func TestApplyRefusesUntilTheStateWasRead(t *testing.T) {
	ctx := context.Background()
	admin := trusttest.NewSigner(1)
	v1 := admin.Sign(t, trusttest.Genesis(admin))
	k, st := newKeeper(t, trust.Anchor{Seq: 1, Hash: trust.Hash(v1)})
	if _, err := k.Apply(ctx, chainBytes(t, v1)); !errors.Is(err, ErrStore) {
		t.Fatalf("Apply before Load = %v, want ErrStore", err)
	}
	// And nothing of the chain went into the state on the way.
	if seq, err := st.NewestSeq(ctx); err != nil || seq != 0 {
		t.Fatalf("Apply before Load left version %d behind (%v)", seq, err)
	}
	// After Load the same chain goes through, so what stood in the way was the
	// unread state and nothing about the chain.
	if err := k.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if seq, err := k.Apply(ctx, chainBytes(t, v1)); err != nil || seq != 1 {
		t.Fatalf("Apply after Load = %d, %v, want 1, nil", seq, err)
	}
}
