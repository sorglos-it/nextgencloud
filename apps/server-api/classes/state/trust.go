package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
)

// trustLock is the lock that makes reading, verifying and writing one version
// a single step for the whole installation, and trustLease the seconds it is
// held: verifying one version and writing one row takes milliseconds, so a
// short lease frees the list again quickly if an API dies while holding it.
const (
	trustLock  = "trust"
	trustLease = 30
)

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
	for {
		chain, err := s.readBlock(ctx, from)
		if err != nil {
			return trust.Version{}, err
		}
		if len(chain) == 0 {
			break
		}
		if cur.List == nil {
			// Version 1 is the anchor of this installation: it signs itself,
			// and trust.Open checks that an admin of it did.
			cur, err = trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(chain[0])}, chain)
			if err == nil {
				err = s.checkInstallation(ctx, cur.List.Installation)
			}
		} else {
			cur, err = trust.Advance(cur, chain)
		}
		if err != nil {
			return trust.Version{}, err
		}
		from = cur.List.Seq + 1
		if len(chain) < session.MaxChainVersions {
			break
		}
	}
	if cur.List == nil {
		return trust.Version{}, ErrNoTrust
	}
	s.cur = cur
	return cur, nil
}

// readBlock reads at most session.MaxChainVersions versions from from on, row
// by row, and stops at the first number that is not there. dbapi.BlobFrom
// would hand out the whole history in one allocation, and each version may be
// megabytes: every caller of Chain would pay for all of them.
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
// answers 204); anything beyond is ErrOldVersion (404).
func (s *State) Chain(ctx context.Context, from uint64) (session.TrustChain, error) {
	cur, err := s.Refresh(ctx)
	if err != nil {
		return session.TrustChain{}, err
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
