// Package state holds what every part of the API needs: the configuration,
// the database, the secret box, the pin of this API's TLS key and the verified
// trust list. Nothing here is the only copy of anything: everything that
// matters lies in the database, so any API can replace any other.
package state

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"sync"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

var (
	// ErrSetting reports a row of this installation's own that is missing or
	// is not what it has to be: a setting the database does not hold or cannot
	// hold, and a stored version of the trust list that does not read or does
	// not hold together with the ones around it (refreshLocked, readBlock,
	// checkGap, Chain). The store answered; what it answered with is wrong,
	// and nobody outside mends that by asking again or by signing again.
	ErrSetting = errors.New("state: setting missing or malformed")
	// ErrBusy reports that another API holds the lock for this step. It is its
	// own error, never ErrSetting: here waiting a moment and asking again
	// helps, and nothing is broken.
	ErrBusy = errors.New("state: another API is at this step, try again")
	// ErrRefused reports a version handed in that package trust would not
	// take, for a reason chainFault puts no finer name to: the signature over
	// it, a payload that no longer reads, or something package trust saw that
	// this package will not name for it. The caller brought it, so it is
	// neither a row of ours nor the store -- and an error with no name of this
	// package reads as the store one package further out (httpapi.Fault). The
	// cause rides along for whoever reads it.
	ErrRefused = errors.New("state: the version handed in was refused")
	// ErrDamaged reports that this installation's own stored rows contradict
	// each other: the trust list in the database names an installation other
	// than the ID the setup stored. It is its own error and never
	// ErrInstallation, because the same mismatch means two different things.
	// Handed in, it is a list of a stranger and the caller's business; found
	// among our own rows it is damage, and whoever hears "signature invalid"
	// for that signs again over a database no version he can sign will mend.
	ErrDamaged = errors.New("state: stored rows contradict each other")
)

// installLock is the name of the lock EnsureInstallation takes, and
// installLease the seconds it holds it: long enough for one key pair and two
// writes, short enough that an API which died in between does not keep the
// next one out for long.
const (
	installLock  = "installation"
	installLease = 60
)

// trustRoot is the ID of the row that holds version 1 of the trust list, the
// version the installation ID is bound into.
const trustRoot = "1"

// State is the runtime state of one API process.
type State struct {
	Cfg *config.API
	DB  dbapi.Store
	Box *secrets.Box
	Pin []byte // trust.TLSPin of this API's TLS key

	// Now gives the time in Unix seconds; tests replace it.
	Now func() int64

	mu  sync.RWMutex
	cur trust.Version
}

// New returns a State for cfg.
func New(cfg *config.API, db dbapi.Store, box *secrets.Box, pin []byte) *State {
	return &State{Cfg: cfg, DB: db, Box: box, Pin: pin, Now: func() int64 { return time.Now().Unix() }}
}

// Installation returns the installation ID, ErrSetting while there is none.
func (s *State) Installation(ctx context.Context) (string, error) {
	b, err := s.DB.BlobGet(ctx, dbapi.BlobSetting, dbapi.SettingInstallation)
	if err != nil {
		return "", err
	}
	id := string(b.Data)
	if !trust.ValidID(id) {
		return "", ErrSetting
	}
	return id, nil
}

// InstallKey returns the installation's private X25519 key, which unwraps the
// content keys of paid apps. Only the API can open it (secrets.Box).
func (s *State) InstallKey(ctx context.Context) (*ecdh.PrivateKey, error) {
	b, err := s.DB.BlobGet(ctx, dbapi.BlobSetting, dbapi.SettingInstallKey)
	if err != nil {
		return nil, err
	}
	raw, err := s.Box.Open(b.Data)
	if err != nil {
		return nil, err
	}
	return ecdh.X25519().NewPrivateKey(raw)
}

// EnsureInstallation makes the installation ID and the installation's key
// pair if they are missing, and returns the ID. A lock keeps two APIs on one
// database from making two of them; the API that does not get it hears ErrBusy.
func (s *State) EnsureInstallation(ctx context.Context) (string, error) {
	if id, err := s.Installation(ctx); err == nil {
		return id, nil
	} else if !errors.Is(err, dbapi.ErrNotFound) {
		return "", err
	}
	now := s.Now()
	ok, err := s.DB.LockTake(ctx, installLock, s.Cfg.Node, now, now+installLease)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrBusy
	}
	defer func() { _ = s.DB.LockFree(ctx, installLock, s.Cfg.Node) }()
	// Another API may have been quicker between the two checks -- and it may
	// have left a broken row, which is no reason to write over it either.
	if id, err := s.Installation(ctx); err == nil {
		return id, nil
	} else if !errors.Is(err, dbapi.ErrNotFound) {
		return "", err
	}
	// The trust list is rooted in the installation ID: version 1 carries it
	// inside and is signed over it, and no later version may name another
	// one. Once that version is stored, a fresh ID would answer every Refresh
	// with ErrInstallation for good, and no version 2 could mend it. So next
	// to a stored version 1 a missing ID is a damaged installation, not a new
	// one -- whoever restores must bring the ID back.
	if _, err := s.DB.BlobGet(ctx, dbapi.BlobTrust, trustRoot); err == nil {
		return "", ErrSetting
	} else if !errors.Is(err, dbapi.ErrNotFound) {
		return "", err
	}
	id, err := secrets.ID()
	if err != nil {
		return "", err
	}
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	sealed, err := s.Box.Seal(key.Bytes())
	if err != nil {
		return "", err
	}
	// The key goes in first and the ID last: the ID is what every other part
	// asks for, so it appears only once the key it belongs to is there.
	if err := s.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallKey,
		Data: sealed, Updated: now}); err != nil {
		return "", err
	}
	if err := s.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallation,
		Data: []byte(id), Updated: now}); err != nil {
		return "", err
	}
	return id, nil
}
