package state

import (
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
	// ErrOldVersion reports a version that is not the one after the newest.
	ErrOldVersion = errors.New("state: version is not the next one")
)

// chainOverhead is what the JSON of a TrustChain costs besides the stored
// versions: {"versions":[]} and ,"more":true are 27 bytes, and the commas
// between at most session.MaxChainVersions versions are one each. Chain keeps
// that much of session.MaxChain free, so an answer it fills to the brim still
// passes session.ParseTrustChain on the other side.
const chainOverhead = 27 + session.MaxChainVersions

// Trust returns the newest verified version, from the cache if there is one.
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
// chain is verified only when nothing is cached yet (after a start).
func (s *State) Refresh(ctx context.Context) (trust.Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	from := uint64(1)
	if s.cur.List != nil {
		from = s.cur.List.Seq + 1
	}
	blobs, err := s.DB.BlobFrom(ctx, dbapi.BlobTrust, from)
	if err != nil {
		return trust.Version{}, err
	}
	if len(blobs) == 0 {
		if s.cur.List == nil {
			return trust.Version{}, ErrNoTrust
		}
		return s.cur, nil
	}
	chain := make([]trust.Signed, 0, len(blobs))
	for _, b := range blobs {
		// The database is ours, but a row is still input: the same parser as
		// everywhere else reads it.
		sg, err := trust.ParseSigned(b.Data)
		if err != nil {
			return trust.Version{}, ErrSetting
		}
		chain = append(chain, sg)
	}
	var v trust.Version
	if s.cur.List == nil {
		// Version 1 is the anchor of this installation: it signs itself, and
		// trust.Open checks that an admin of it did.
		v, err = trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(chain[0])}, chain)
	} else {
		v, err = trust.Advance(s.cur, chain)
	}
	if err != nil {
		return trust.Version{}, err
	}
	s.cur = v
	return v, nil
}

// AddVersion verifies sg as the version after the newest and stores it.
// Version 1 is taken only while there is none. The caller has read sg with
// trust.ParseSigned or session.ParseTrustChain.
func (s *State) AddVersion(ctx context.Context, sg trust.Signed) (trust.Version, error) {
	if _, err := s.Refresh(ctx); err != nil && err != ErrNoTrust {
		return trust.Version{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Refresh gave the lock back in between, so the cache is read here again
	// rather than carried over: another caller may have moved it meanwhile.
	cur := s.cur
	var (
		next trust.Version
		err  error
	)
	if cur.List == nil {
		next, err = trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(sg)}, []trust.Signed{sg})
	} else {
		next, err = trust.Advance(cur, []trust.Signed{sg})
	}
	switch {
	// trust.Advance checks the proof before it looks at the payload, so
	// ErrChain means: a listed key signed this, but it does not follow the
	// newest version. A replay of a stored version lands here, and so does one
	// that skips a number. While there is no version at all, trust.Open says
	// the same with ErrAnchor, since the anchor carries this version's own
	// hash and only its number can miss.
	case errors.Is(err, trust.ErrChain), cur.List == nil && errors.Is(err, trust.ErrAnchor):
		return trust.Version{}, ErrOldVersion
	case err != nil:
		return trust.Version{}, err
	}
	seq := next.List.Seq
	// Refresh has just read every stored version, so a row at this number
	// means another API wrote it in between: hand back ErrOldVersion instead
	// of replacing what it stored. Reading and writing are two steps, so this
	// narrows that window, it does not close it.
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

// Chain answers GET session.PathTrust: the versions from from on, at most
// session.MaxChainVersions and session.MaxChain bytes, with More when older
// answers follow. from one past the newest gives an empty chain (the caller
// answers 204); anything beyond is ErrOldVersion (404).
func (s *State) Chain(ctx context.Context, from uint64) (session.TrustChain, error) {
	cur, err := s.Refresh(ctx)
	if err != nil {
		return session.TrustChain{}, err
	}
	if from > cur.List.Seq+1 {
		return session.TrustChain{}, ErrOldVersion
	}
	if from == cur.List.Seq+1 {
		return session.TrustChain{}, nil
	}
	blobs, err := s.DB.BlobFrom(ctx, dbapi.BlobTrust, from)
	if err != nil {
		return session.TrustChain{}, err
	}
	var out session.TrustChain
	size := chainOverhead
	for _, b := range blobs {
		// A version is written out as it is stored, so the stored size plus
		// the wrapper is what the answer costs.
		if len(out.Versions) == session.MaxChainVersions || size+len(b.Data) > session.MaxChain {
			out.More = true
			break
		}
		sg, err := trust.ParseSigned(b.Data)
		if err != nil {
			return session.TrustChain{}, ErrSetting
		}
		out.Versions = append(out.Versions, sg)
		size += len(b.Data)
	}
	// from is at most the newest version, so at least one row belongs in the
	// answer. None means the database lost rows; an empty chain would be a 200
	// whose body session.ParseTrustChain refuses.
	if len(out.Versions) == 0 {
		return session.TrustChain{}, ErrSetting
	}
	return out, nil
}
