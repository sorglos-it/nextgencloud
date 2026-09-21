// Package memstore implements dbapi.Store in memory. It is the reference for
// every other implementation, and the API can test against it without a
// database. It is not meant for production.
package memstore

import (
	"bytes"
	"cmp"
	"context"
	"encoding/hex"
	"slices"
	"strings"
	"sync"

	"github.com/sorglos-it/nextgencloud/contracts/core"
	"github.com/sorglos-it/nextgencloud/contracts/dbapi"
)

type store struct {
	mu       sync.Mutex
	blobs    map[string]dbapi.Blob
	users    map[string]dbapi.User
	sessions map[string]dbapi.Session
	apps     map[string]dbapi.App
	pending  map[string]dbapi.Pending
	once     map[string]onceValue
	locks    map[string]lockValue
	log      []core.LogEntry
}

type onceValue struct {
	data    string
	expires int64
	used    bool
}

type lockValue struct {
	owner string
	until int64
}

// New returns an empty store.
func New() dbapi.Store {
	return &store{
		blobs: map[string]dbapi.Blob{}, users: map[string]dbapi.User{}, sessions: map[string]dbapi.Session{},
		apps: map[string]dbapi.App{}, pending: map[string]dbapi.Pending{}, once: map[string]onceValue{},
		locks: map[string]lockValue{},
	}
}

func key(kind, id string) string { return kind + "\x00" + id }

func (s *store) Close() error { return nil }

func (s *store) BlobPut(_ context.Context, b dbapi.Blob) error {
	if b.Kind == "" || b.ID == "" {
		return dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b.Data = bytes.Clone(b.Data)
	s.blobs[key(b.Kind, b.ID)] = b
	return nil
}

func (s *store) BlobGet(_ context.Context, kind, id string) (dbapi.Blob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.blobs[key(kind, id)]
	if !ok {
		return dbapi.Blob{}, dbapi.ErrNotFound
	}
	b.Data = bytes.Clone(b.Data)
	return b, nil
}

func (s *store) BlobFrom(_ context.Context, kind string, seq uint64) ([]dbapi.Blob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []dbapi.Blob
	for _, b := range s.blobs {
		if b.Kind == kind && b.Seq >= seq {
			b.Data = bytes.Clone(b.Data)
			out = append(out, b)
		}
	}
	slices.SortFunc(out, func(a, b dbapi.Blob) int {
		if a.Seq != b.Seq {
			return cmp.Compare(a.Seq, b.Seq)
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

func (s *store) BlobDelete(_ context.Context, kind, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.blobs, key(kind, id))
	return nil
}

func cloneApp(a dbapi.App) dbapi.App {
	a.Manifest, a.SHA256, a.Grants = bytes.Clone(a.Manifest), bytes.Clone(a.SHA256), slices.Clone(a.Grants)
	return a
}

func (s *store) AppPut(_ context.Context, a dbapi.App) error {
	if a.ID == "" {
		return dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apps[a.ID] = cloneApp(a)
	return nil
}

func (s *store) AppGet(_ context.Context, id string) (dbapi.App, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.apps[id]
	if !ok {
		return dbapi.App{}, dbapi.ErrNotFound
	}
	return cloneApp(a), nil
}

func (s *store) AppList(_ context.Context) ([]dbapi.App, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]dbapi.App, 0, len(s.apps))
	for _, a := range s.apps {
		out = append(out, cloneApp(a))
	}
	slices.SortFunc(out, func(a, b dbapi.App) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

func (s *store) AppDelete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.apps, id)
	return nil
}

func (s *store) OnceCreate(_ context.Context, kind string, value []byte, data string, expires int64) error {
	if kind == "" || len(value) == 0 {
		return dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(kind, hex.EncodeToString(value))
	if _, ok := s.once[k]; ok {
		return dbapi.ErrUsed
	}
	s.once[k] = onceValue{data: data, expires: expires}
	return nil
}

func (s *store) OnceUse(_ context.Context, kind string, value []byte, now int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(kind, hex.EncodeToString(value))
	v, ok := s.once[k]
	switch {
	case !ok || v.expires <= now:
		return "", dbapi.ErrNotFound
	case v.used:
		return "", dbapi.ErrUsed
	}
	v.used = true
	s.once[k] = v
	return v.data, nil
}

func (s *store) OncePurge(_ context.Context, now int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.once {
		if v.expires <= now {
			delete(s.once, k)
		}
	}
	return nil
}

func (s *store) UserPut(_ context.Context, u dbapi.User) error {
	if u.ID == "" || u.Name == "" {
		return dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, other := range s.users {
		if other.Name == u.Name && id != u.ID {
			return dbapi.ErrInvalid
		}
	}
	u.Roles, u.TOTP = slices.Clone(u.Roles), bytes.Clone(u.TOTP)
	s.users[u.ID] = u
	return nil
}

func (s *store) UserGet(_ context.Context, id string) (dbapi.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return dbapi.User{}, dbapi.ErrNotFound
	}
	u.Roles, u.TOTP = slices.Clone(u.Roles), bytes.Clone(u.TOTP)
	return u, nil
}

// UserByName looks up the ID under the lock and then calls UserGet, which
// locks again; that two-step shape is only needed because sync.Mutex is not
// reentrant — an SQL implementation does this with one "WHERE name = ?" query.
func (s *store) UserByName(ctx context.Context, name string) (dbapi.User, error) {
	s.mu.Lock()
	id := ""
	for _, u := range s.users {
		if u.Name == name {
			id = u.ID
		}
	}
	s.mu.Unlock()
	if id == "" {
		return dbapi.User{}, dbapi.ErrNotFound
	}
	return s.UserGet(ctx, id)
}

func (s *store) UserList(_ context.Context) ([]dbapi.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]dbapi.User, 0, len(s.users))
	for _, u := range s.users {
		u.Roles, u.TOTP = slices.Clone(u.Roles), bytes.Clone(u.TOTP)
		out = append(out, u)
	}
	slices.SortFunc(out, func(a, b dbapi.User) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (s *store) UserDelete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.users, id)
	return nil
}

func cloneSession(s dbapi.Session) dbapi.Session {
	s.Token, s.Cert, s.Proof = bytes.Clone(s.Token), bytes.Clone(s.Cert), bytes.Clone(s.Proof)
	s.APIKey, s.APICert = bytes.Clone(s.APIKey), bytes.Clone(s.APICert)
	return s
}

func (s *store) SessionPut(_ context.Context, v dbapi.Session) error {
	if len(v.Token) == 0 || v.User == "" {
		return dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[hex.EncodeToString(v.Token)] = cloneSession(v)
	return nil
}

func (s *store) SessionGet(_ context.Context, token []byte) (dbapi.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[hex.EncodeToString(token)]
	if !ok {
		return dbapi.Session{}, dbapi.ErrNotFound
	}
	return cloneSession(v), nil
}

func (s *store) SessionTouch(_ context.Context, token []byte, now int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := hex.EncodeToString(token)
	v, ok := s.sessions[k]
	if !ok {
		return dbapi.ErrNotFound
	}
	v.Seen = now
	s.sessions[k] = v
	return nil
}

func (s *store) SessionDelete(_ context.Context, token []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, hex.EncodeToString(token))
	return nil
}

func (s *store) SessionPurge(_ context.Context, now int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.sessions {
		if v.Expires <= now {
			delete(s.sessions, k)
		}
	}
	return nil
}

func (s *store) PendingPut(_ context.Context, p dbapi.Pending) error {
	if p.Name == "" || len(p.Key) == 0 {
		return dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p.Key = bytes.Clone(p.Key)
	s.pending[hex.EncodeToString(p.Key)] = p
	return nil
}

func (s *store) PendingList(_ context.Context) ([]dbapi.Pending, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]dbapi.Pending, 0, len(s.pending))
	for _, p := range s.pending {
		p.Key = bytes.Clone(p.Key)
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b dbapi.Pending) int {
		if a.Since != b.Since {
			return cmp.Compare(a.Since, b.Since)
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

func (s *store) PendingDelete(_ context.Context, k []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, hex.EncodeToString(k))
	return nil
}

func (s *store) LogAdd(_ context.Context, e core.LogEntry) error {
	if e.Kind == "" || e.Time <= 0 {
		return dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = append(s.log, e)
	return nil
}

func (s *store) LogQuery(_ context.Context, q core.LogQuery) ([]core.LogEntry, error) {
	if q.Limit <= 0 {
		return nil, dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.LogEntry, 0, q.Limit)
	rows := slices.Clone(s.log)
	slices.SortFunc(rows, func(a, b core.LogEntry) int { return cmp.Compare(b.Time, a.Time) })
	for _, e := range rows {
		if (q.Before != 0 && e.Time >= q.Before) || (q.User != "" && e.User != q.User) {
			continue
		}
		if out = append(out, e); len(out) == q.Limit {
			break
		}
	}
	return out, nil
}

func (s *store) LockTake(_ context.Context, name, owner string, now, until int64) (bool, error) {
	if name == "" || owner == "" || until <= now {
		return false, dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.locks[name]; ok && l.owner != owner && l.until > now {
		return false, nil
	}
	s.locks[name] = lockValue{owner: owner, until: until}
	return true, nil
}

func (s *store) LockFree(_ context.Context, name, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.locks[name]; ok && l.owner == owner {
		delete(s.locks, name)
	}
	return nil
}
