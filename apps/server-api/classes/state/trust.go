package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

var (
	// ErrNoTrust reports that the installation has no trust list yet: only the
	// setup may run.
	ErrNoTrust = errors.New("state: no trust list yet")
	// ErrOldVersion reports a version whose number is not the one after the
	// newest: an older one played again, or one that skips a number.
	ErrOldVersion = errors.New("state: version is not the next one")
	// ErrFork reports a version with the right number that names another
	// version as the one before it. Two chains claim the same number, and only
	// one of them can be this installation's.
	ErrFork = errors.New("state: version follows a different version")
	// ErrInstallation reports a version of another installation. It is its own
	// error because it is the one fault that points at a swapped list rather
	// than at a stale or hasty caller.
	ErrInstallation = errors.New("state: version belongs to another installation")
	// ErrBadVersion reports a number no version ever has: they are counted
	// from 1. session.ParseFrom refuses it in the query already, so it is a
	// mistake inside the API -- and a mistake must never read like a damaged
	// database.
	ErrBadVersion = errors.New("state: version numbers start at 1")
)

// trustLock is the lock that makes reading, verifying and writing one version
// a single step for the whole installation, and trustLease the seconds it is
// held: verifying one version and writing one row takes milliseconds, so a
// short lease frees the list again quickly if an API dies while holding it.
const (
	trustLock  = "trust"
	trustLease = 30
)

// gapProbes is how many numbers above a missing one checkGap asks for before
// it calls the chain ended. Eight empty reads cost the same whether the chain
// is whole or not, and they find every hole of up to eight versions for
// certain. A larger hole needs what the contract has no query for: the newest
// number the database holds, or an anchor outside it.
const gapProbes = 8

// chainOverhead is what the JSON of a TrustChain costs besides the stored
// versions: {"versions":[]} and ,"more":true are 27 bytes, and the commas
// between at most session.MaxChainVersions versions are one each. Chain keeps
// that much of session.MaxChain free, so an answer it fills to the brim still
// passes session.ParseTrustChain on the other side.
const chainOverhead = 27 + session.MaxChainVersions

// Trust returns the newest verified version, from the cache if there is one.
// Its List is the cached one, which every caller shares: read it, never change
// it.
func (s *State) Trust(ctx context.Context) (trust.Version, error) {
	s.mu.RLock()
	cur := s.cur
	s.mu.RUnlock()
	if cur.List != nil {
		return cur, nil
	}
	return s.Refresh(ctx)
}

// Refresh reads the versions the database holds beyond the cached one and
// verifies them, so an API sees at once what another API wrote. The whole
// chain is verified only when nothing is cached yet (after a start), in blocks
// of session.MaxChainVersions. Nothing is cached unless every block verified.
// The returned List is the cached one and is shared: read it, never change it.
func (s *State) Refresh(ctx context.Context) (trust.Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshLocked(ctx)
}

// refreshLocked is Refresh with s.mu already held, so that AddVersion can
// refresh without giving the cache up in the middle of its own step.
func (s *State) refreshLocked(ctx context.Context) (trust.Version, error) {
	cur := s.cur
	from := uint64(1)
	if cur.List != nil {
		from = cur.List.Seq + 1
	}
	// retried marks the one retry checkGap may ask for. More than one means
	// a row keeps coming and going while this read runs, and then nothing
	// read here is worth keeping.
	retried := false
	for {
		chain, err := s.readBlock(ctx, from)
		if err != nil {
			return trust.Version{}, err
		}
		if len(chain) > 0 {
			if cur.List == nil {
				// Version 1 is the anchor of this installation: it signs
				// itself, and trust.Open checks that an admin of it did.
				cur, err = trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(chain[0])}, chain)
				err = ownRows(err)
				if err == nil {
					// Here both sides come out of this installation's own
					// rows, so a mismatch is not a list somebody handed in:
					// the rows contradict each other. AddVersion refreshes
					// before it takes anything, so without this its caller
					// would be told his signature was invalid for a database
					// that is damaged.
					if err = s.checkInstallation(ctx, cur.List.Installation); errors.Is(err, ErrInstallation) {
						err = ErrDamaged
					}
				}
			} else {
				cur, err = trust.Advance(cur, chain)
				err = ownRows(err)
			}
			if err != nil {
				return trust.Version{}, err
			}
			from = cur.List.Seq + 1
			if len(chain) == session.MaxChainVersions {
				// A full block says nothing about what lies above it.
				continue
			}
		}
		// The database does not hold number from, and from below the end of
		// the chain and a hole in it look the same.
		again, err := s.checkGap(ctx, from)
		if err != nil {
			return trust.Version{}, err
		}
		if !again {
			break
		}
		if retried {
			return trust.Version{}, ErrSetting
		}
		retried = true
	}
	if cur.List == nil {
		return trust.Version{}, ErrNoTrust
	}
	s.cur = cur
	return cur, nil
}

// ownRows is what a fault of trust.Open or trust.Advance means inside a
// refresh: both the version held so far and the rows just read came out of
// this installation's own store, so nothing about it is a list somebody handed
// in -- it is the stored rows no longer holding together, which is the same
// damage readBlock answers when a row does not even parse. Nobody outside can
// mend it by signing again, and ErrSetting is the word for that here.
//
// It matters past the wording: with it, the only errors that leave this
// package unnamed are the store's own, and httpapi.Fault reads them as a store
// that did not answer. AddVersion keeps trust's errors as they are -- there
// the version came from the caller, and his signature is his business.
func ownRows(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrSetting, err)
}

// readBlock reads at most session.MaxChainVersions versions from from on, row
// by row, and stops at the first number that is not there; the caller looks
// above that number with checkGap. dbapi.BlobFrom would hand out the whole
// history in one allocation, and each version may be megabytes: every caller
// of Chain would pay for all of them.
func (s *State) readBlock(ctx context.Context, from uint64) ([]trust.Signed, error) {
	out := make([]trust.Signed, 0, session.MaxChainVersions)
	for i := uint64(0); i < session.MaxChainVersions; i++ {
		b, err := s.DB.BlobGet(ctx, dbapi.BlobTrust, strconv.FormatUint(from+i, 10))
		if errors.Is(err, dbapi.ErrNotFound) {
			break
		}
		if err != nil {
			return nil, err
		}
		// The database is ours, but a row is still input: the same parser as
		// everywhere else reads it.
		sg, err := trust.ParseSigned(b.Data)
		if err != nil {
			return nil, ErrSetting
		}
		out = append(out, sg)
	}
	return out, nil
}

// checkGap looks above a number the database does not hold. Versions are
// counted without holes, so a row further up means the database lost the one
// in between -- and a read that stops at a hole puts an older trust list back
// in force without a word: keys an admin withdrew would count again, which is
// what the anchor stands against. Dropping one row is the cheapest way to do
// that, so it is the one this catches for certain.
//
// It answers true when the number is there after all: versions are written in
// order, so a row above it means another API had just added both, and the
// caller reads on. A row above a number that stays missing is ErrSetting.
func (s *State) checkGap(ctx context.Context, missing uint64) (bool, error) {
	for i := uint64(1); i <= gapProbes; i++ {
		above, err := s.rowThere(ctx, missing+i)
		if err != nil {
			return false, err
		}
		if !above {
			continue
		}
		again, err := s.rowThere(ctx, missing)
		if err != nil {
			return false, err
		}
		if again {
			return true, nil
		}
		return false, ErrSetting
	}
	return false, nil
}

// rowThere reports whether the database holds this version. It does not read
// it as one: here only its presence counts, and a row that is there is read
// and verified in the round that follows.
func (s *State) rowThere(ctx context.Context, seq uint64) (bool, error) {
	_, err := s.DB.BlobGet(ctx, dbapi.BlobTrust, strconv.FormatUint(seq, 10))
	if errors.Is(err, dbapi.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// checkInstallation binds the root of the chain to this installation. Version
// 1 anchors itself, so trust.Open can only say that the list is whole and that
// an admin of it signed it: any well-formed first version passes that,
// including one a stranger wrote for himself. The installation ID the setup
// stored before version 1 was signed is the one value here that does not come
// out of the same row, so it decides. Whoever can write the database can
// change that row too; the anchor that would settle it outside the database is
// not in the contract yet.
func (s *State) checkInstallation(ctx context.Context, want string) error {
	id, err := s.Installation(ctx)
	// No ID at all means the setup did not run before version 1 was signed.
	// That is a missing setting, not a lost document: only Installation itself
	// hands dbapi.ErrNotFound on, because EnsureInstallation acts on it.
	if errors.Is(err, dbapi.ErrNotFound) {
		return ErrSetting
	}
	if err != nil {
		return err
	}
	if id != want {
		return ErrInstallation
	}
	return nil
}

// AddVersion verifies sg as the version after the newest and stores it.
// Version 1 is taken only while there is none, and only for this installation.
// The caller has read sg with trust.ParseSigned or session.ParseTrustChain.
func (s *State) AddVersion(ctx context.Context, sg trust.Signed) (trust.Version, error) {
	// Reading, verifying and writing is one step for the whole installation.
	// dbapi.LockTake is atomic across the APIs on one database, but it lets
	// the same owner take a lock it already holds, so it alone would not keep
	// two callers inside one API apart: s.mu does that, and it is taken first
	// and held to the end for exactly that reason.
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	ok, err := s.DB.LockTake(ctx, trustLock, s.Cfg.Node, now, now+trustLease)
	if err != nil {
		return trust.Version{}, err
	}
	if !ok {
		return trust.Version{}, ErrBusy
	}
	defer func() { _ = s.DB.LockFree(ctx, trustLock, s.Cfg.Node) }()
	if _, err := s.refreshLocked(ctx); err != nil && !errors.Is(err, ErrNoTrust) {
		return trust.Version{}, err
	}
	cur := s.cur
	var next trust.Version
	if cur.List == nil {
		next, err = trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(sg)}, []trust.Signed{sg})
		if err == nil {
			err = s.checkInstallation(ctx, next.List.Installation)
		}
	} else {
		next, err = trust.Advance(cur, []trust.Signed{sg})
	}
	switch {
	case err == nil:
	case errors.Is(err, trust.ErrChain):
		return trust.Version{}, chainFault(cur, sg)
	// While there is no version at all, the anchor carries this version's own
	// hash, so trust.Open can only fault its number.
	case cur.List == nil && errors.Is(err, trust.ErrAnchor):
		return trust.Version{}, ErrOldVersion
	default:
		return trust.Version{}, err
	}
	seq := next.List.Seq
	// The lock is held and every writer takes it, so a row at this number is
	// one no refresh of this API ever saw: refuse instead of replacing it.
	if _, err := s.DB.BlobGet(ctx, dbapi.BlobTrust, strconv.FormatUint(seq, 10)); err == nil {
		return trust.Version{}, ErrOldVersion
	} else if !errors.Is(err, dbapi.ErrNotFound) {
		return trust.Version{}, err
	}
	data, err := json.Marshal(sg)
	if err != nil {
		return trust.Version{}, err
	}
	if err := s.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobTrust, ID: strconv.FormatUint(seq, 10),
		Seq: seq, Data: data, Updated: s.Now()}); err != nil {
		return trust.Version{}, err
	}
	s.cur = next
	return next, nil
}

// chainFault tells the three faults apart that trust.Advance sums up as
// ErrChain, because they mean different things to whoever reads the answer: a
// caller that is behind, a second chain under the same number, or a list of
// another installation. It reads the payload, which is safe here and only
// here: Advance answers ErrChain only after it checked the proof over exactly
// these bytes, so a key of the current list stands behind them.
func chainFault(cur trust.Version, sg trust.Signed) error {
	next, err := trust.Parse(sg.Payload)
	if err != nil {
		return err
	}
	switch {
	case next.Installation != cur.List.Installation:
		return ErrInstallation
	case next.Seq != cur.List.Seq+1:
		return ErrOldVersion
	case !bytes.Equal(next.Prev, cur.Hash):
		return ErrFork
	}
	// trust.Advance saw something this does not know: hand its error on rather
	// than name a fault that may not be the one.
	return trust.ErrChain
}

// Chain answers GET session.PathTrust: the versions from from on, at most
// session.MaxChainVersions and session.MaxChain bytes, with More when older
// answers follow. from one past the newest gives an empty chain (the caller
// answers 204); anything beyond is ErrOldVersion (404), and from below 1 is
// ErrBadVersion, a number that is no version at all.
//
// cur is the version the caller brought up to date itself (Refresh), not one
// Chain fetches for itself. Whoever answers with a chain has decided something
// on that list first and is up to date already, so fetching it here again
// would walk the trust rows a second time for one request -- and it would
// answer out of a list the caller never decided on. A caller with no version
// hands in the zero one and is told so rather than read as version 0.
func (s *State) Chain(ctx context.Context, cur trust.Version, from uint64) (session.TrustChain, error) {
	// A number no version has is answered before the database is asked at
	// all: nothing about it can be found there.
	if from < 1 {
		return session.TrustChain{}, ErrBadVersion
	}
	if cur.List == nil {
		return session.TrustChain{}, ErrNoTrust
	}
	newest := cur.List.Seq
	if from > newest+1 {
		return session.TrustChain{}, ErrOldVersion
	}
	if from == newest+1 {
		return session.TrustChain{}, nil
	}
	var out session.TrustChain
	size := chainOverhead
	seq := from
	for ; seq <= newest && len(out.Versions) < session.MaxChainVersions; seq++ {
		b, err := s.DB.BlobGet(ctx, dbapi.BlobTrust, strconv.FormatUint(seq, 10))
		if err != nil {
			// Every version up to the newest was verified out of the database,
			// so a row that is gone now means the database lost it.
			if errors.Is(err, dbapi.ErrNotFound) {
				return session.TrustChain{}, ErrSetting
			}
			return session.TrustChain{}, err
		}
		// A version is written out as it is stored, so the stored size plus
		// the wrapper is what the answer costs.
		if size+len(b.Data) > session.MaxChain {
			break
		}
		sg, err := trust.ParseSigned(b.Data)
		if err != nil {
			return session.TrustChain{}, ErrSetting
		}
		out.Versions = append(out.Versions, sg)
		size += len(b.Data)
	}
	// seq is the first version this answer leaves out.
	out.More = seq <= newest
	// One version is at most the base64 of sign.MaxPayload plus its proof,
	// well under session.MaxChain, so today the loop always takes the first
	// one. The guard keeps the answer from ever being an empty 200, which
	// session.ParseTrustChain refuses, should those limits move.
	if len(out.Versions) == 0 {
		return session.TrustChain{}, ErrSetting
	}
	return out, nil
}
