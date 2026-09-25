// Package chain keeps the trust list of this installation. It verifies every
// version itself, from the anchor pinned in controller.json up to the newest
// one, so an API can hand the list on but never forge it. What it verified it
// keeps in the local state, so the server knows its users, nodes and stores
// again after a restart without asking anyone.
package chain

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// MaxVersions bounds how many versions one keeper verifies and holds, counted
// from the anchor on. Verification starts at the pinned anchor and every
// version names the hash of the one before it, so none in between may be
// dropped; a chain becomes short again only with a newer anchor
// (config.SaveAnchor), which this keeper does not write. The bound keeps a
// database that grew past that from being verified in full at every start.
const MaxVersions = 4096

// ErrTooMany reports a chain longer than MaxVersions. It is no fault of the
// version that hit the bound, so it is told apart from the errors of trust.
var ErrTooMany = errors.New("chain: more versions than one keeper holds")

// ErrStore reports that the local state could not be read or written, or that
// a failed write left it unread. It is no fault of the version that was
// offered, so the caller answers E_INTERNAL and not E_VERIFY: that one reads
// "Unterschrift ungültig." to the admin, who would then sign the change again
// — a second version under a number that is already stored, which no later
// start could read past.
var ErrStore = errors.New("chain: the local state could not be used")

// ErrNoAnchor reports that this controller's own anchor pins nothing: without
// a number or without a SHA-256 it matches no version at all (validAnchor,
// config.Controller). It is this server's fault, so the caller answers
// E_INTERNAL.
//
// It is told apart from trust.ErrAnchor on purpose: that one comes out of the
// verification and says that the chain which arrived does not begin at the
// anchored version. Since a controller that has verified nothing opens the
// trust channel too (access.firstChain), a peer can bring that one about with
// a chain that is signed perfectly well and simply starts somewhere else, and
// what a peer brings about is E_VERIFY and not "Interner Fehler."
var ErrNoAnchor = errors.New("chain: the anchor of this controller pins nothing")

// Keeper holds the newest verified version of the trust list. Several tunnels
// read it at once, so every access goes through mu.
type Keeper struct {
	st     *state.Store
	anchor trust.Anchor

	mu sync.RWMutex
	// loaded says whether cur agrees with what is stored. Only Load sets it;
	// a write that failed in the middle clears it, and Apply refuses until
	// the state was read again.
	loaded bool
	cur    trust.Version // List is nil until the first version is verified
}

// New returns a keeper over the local state, anchored at a. Load fills it.
func New(st *state.Store, a trust.Anchor) *Keeper { return &Keeper{st: st, anchor: a} }

// Load verifies what is stored, from the anchor on, and is what lets Apply
// run. While nothing is stored it leaves the keeper empty and gives no error:
// the controller then runs on until an API sends the first chain.
//
// It has to run before the first channel does: a keeper that never read the
// state keeps no version at all, and what it answers is ErrStore
// (TestApplyRefusesUntilTheStateWasRead). Not to every Apply, though: Apply
// reads the bytes it was given, counts the versions in them and looks at its
// own anchor before it ever asks whether the state was read, so a body that
// does not parse comes back as the parse error and an anchor that pins
// nothing as ErrNoAnchor — unread state or not, and one of them is E_VERIFY
// while the other is E_INTERNAL. Only a chain that got past all three meets
// this gate. Whoever builds the server around a keeper therefore calls this
// first and lets a failure of it end the start. No program does yet — only
// tests build a keeper at all, so "never loaded" is what a first wiring gets
// if it forgets this.
func (k *Keeper) Load(ctx context.Context) error {
	if !k.validAnchor() {
		return ErrNoAnchor
	}
	// The whole call holds the lock: Load and Apply both decide from what the
	// keeper holds, so the newer version must not be replaced by an older one
	// read in between.
	k.mu.Lock()
	defer k.mu.Unlock()
	// Until the state has been read through, Apply must not go on from what
	// the keeper holds: it could write a version under a number the state
	// already has.
	k.loaded = false
	chain, err := k.st.Versions(ctx, k.anchor.Seq)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrStore, err)
	}
	if len(chain) == 0 {
		// The state is the truth about what is stored. A version kept from
		// before would make the keeper write the next one into a gap.
		k.cur = trust.Version{}
		k.loaded = true
		return nil
	}
	if len(chain) > MaxVersions {
		return ErrTooMany
	}
	cur, err := trust.Open(k.anchor, chain)
	if err != nil {
		return err
	}
	k.cur = cur
	k.loaded = true
	return nil
}

// Apply takes the bytes of a session.TrustChain, verifies every version in it
// and keeps them. It returns the number of the newest version it verified.
// The errors of the verification come back as they are (trust.ErrChain,
// trust.ErrSigner, …) and the caller answers them with E_VERIFY; what the
// local state refused comes back as ErrStore and is answered with E_INTERNAL.
// Load runs first and runs again after an ErrStore: a keeper that has not read
// the state would start again at the anchor and report a number below the one
// already stored.
func (k *Keeper) Apply(ctx context.Context, b []byte) (uint64, error) {
	ch, err := session.ParseTrustChain(b)
	if err != nil {
		return 0, err
	}
	if len(ch.Versions) == 0 {
		// ParseTrustChain refuses this already; without the check the number
		// of the newest version (base + len - 1) would wrap around below.
		return 0, trust.ErrChain
	}
	if !k.validAnchor() {
		return 0, ErrNoAnchor
	}
	// One writer at a time: two chains arriving over two tunnels must not
	// interleave between the check and the write.
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.loaded {
		// Either Load never ran or a write failed in the middle: what the
		// state holds is then not what the keeper holds.
		return 0, ErrStore
	}
	// base is the number of the first version of ch. Open pins that one to the
	// anchor and Advance takes only the one after the held version, so after
	// either the number of every version of ch follows from base.
	var (
		base uint64
		next trust.Version
	)
	if k.cur.List == nil {
		base = k.anchor.Seq
		next, err = trust.Open(k.anchor, ch.Versions)
	} else {
		base = k.cur.List.Seq + 1
		next, err = trust.Advance(k.cur, ch.Versions)
	}
	if err != nil {
		return 0, err
	}
	// The number decides the key a version is stored under, so it is checked
	// and not assumed: a version under a foreign key would make the chain
	// unreadable at the next start.
	if next.List.Seq != base+uint64(len(ch.Versions))-1 {
		return 0, trust.ErrChain
	}
	if next.List.Seq-k.anchor.Seq+1 > MaxVersions {
		return 0, ErrTooMany
	}
	// Nothing is written before the whole chain is verified, and the keeper
	// takes the new version only once it is stored: a restart must not lose
	// what the keeper reported.
	for i, v := range ch.Versions {
		if err := k.st.PutVersion(ctx, v, base+uint64(i)); err != nil {
			// dbapi.Local has no transaction, so what was written before this
			// one stays while the keeper still holds the older version.
			// Verifying against that one again could put a second, different
			// version under a number that is taken, and no later Load would
			// read past it. What is stored is verified and chains from the
			// anchor, so only a new Load resolves this — it takes it up.
			k.loaded = false
			return 0, fmt.Errorf("%w: %w", ErrStore, err)
		}
	}
	k.cur = next
	return next.List.Seq, nil
}

// Current returns the newest verified version and whether there is one. The
// list inside it is shared with every other reader and must not be changed.
func (k *Keeper) Current() (trust.Version, bool) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.cur, k.cur.List != nil
}

// List returns the newest verified list, or nil while there is none. It is
// shared with every other reader and must not be changed.
func (k *Keeper) List() *trust.List {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.cur.List
}

// TrustSeq is the number this controller puts into tunnel.Hello.TrustSeq, and
// it is the number after which it needs versions: what this side asks of an
// API is the chain from TrustSeq+1 on. That is the contract's own way of
// counting through the list — session.TrustChain answers "versions from N
// (inclusive) on" and whoever read them asks again from the one after the
// last — and the only thing that changes for a controller is that the versions
// come on a channel instead of over session.PathTrust (package wire, CtlTrust).
//
// While a version is verified, that is its number. While none is — between
// pairing and the first chain — it is the one below the anchor, because the
// chain then has to begin at the anchored version itself: Apply opens it with
// trust.Open, which compares chain[0] against the anchor and refuses anything
// else. A controller that said 0 here would be sent the chain from version 1
// on, and a controller anchored above 1 would refuse it — which is every
// controller paired once the list has moved past its first version, since
// pairing hands out the anchor of the list as it stands then
// (tunnel.PairCode.Anchor). Anchored at 1, the two numbers are the same, and
// that is why nothing showed while only that case was measured.
//
// The number does not say that a version was verified: Current and List say
// that, and only they. An anchor without a number leaves it at 0 — there is
// nothing below 1 — and such an anchor pins nothing at all, so the keeper
// takes no chain either way (validAnchor, ErrNoAnchor).
func (k *Keeper) TrustSeq() uint64 {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if k.cur.List != nil {
		return k.cur.List.Seq
	}
	if k.anchor.Seq == 0 {
		return 0
	}
	return k.anchor.Seq - 1
}

// validAnchor reports whether the anchor can pin a version at all. An anchor
// without a number or without a SHA-256 matches nothing, so it is turned away
// where it is read instead of failing later as a chain that does not fit.
func (k *Keeper) validAnchor() bool {
	return k.anchor.Seq >= 1 && len(k.anchor.Hash) == sha256.Size
}
