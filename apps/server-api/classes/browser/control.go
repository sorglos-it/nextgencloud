package browser

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// control handles a frame on channel 0; false ends the link. Channel 0 carries
// control messages as DATA and nothing else, so every other type is a broken
// rule of the link and not a protocol error of a channel: channel 0 is no number
// of the table, and link.Table.Reserve refuses it for that reason.
//
// The type check is kept although nothing today can get past it, and the reason
// is measured, not assumed: wire.Frame.check refuses OPEN, EOF, CLOSE and GRANT
// on channel 0, so such a frame cannot be encoded at all and link.Read never
// hands one up; and even with the check gone, ParseControl refuses the payload
// of any of them one line later, while the only type a browser may send,
// CtlBye, ends the link either way -- a mutant that drops this line therefore
// stays alive, and no case of this package can kill it. It stays because it is
// this package's own statement about channel 0, which must not rest on a list of
// frame types in another package staying what it is.
func (srv *Server) control(ctx context.Context, s *Session, f wire.Frame) bool {
	if f.Type != wire.TypeData {
		return false
	}
	c, err := wire.ParseControl(f.Payload)
	if err != nil || !wire.ControlAllowed(wire.LinkSession, false, c.T) {
		return false
	}
	// The browser may only say goodbye; after that it closes the link itself,
	// and so do we.
	return c.T != wire.CtlBye
}

// SendControl sends a control message to the shell.
func (s *Session) SendControl(ctx context.Context, t string, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(wire.Control{T: t, B: raw})
	if err != nil {
		return err
	}
	return s.link.Write(ctx, wire.Frame{Type: wire.TypeData, Channel: wire.ControlChannel, Payload: payload})
}

// Hint tells the shell that a newer version of the trust list exists.
func (s *Session) Hint(ctx context.Context, seq uint64) error {
	return s.SendControl(ctx, wire.CtlTrust, session.TrustHint{Seq: seq})
}

// Notify sends a notice to the shell.
func (s *Session) Notify(ctx context.Context, n core.Notice) error {
	return s.SendControl(ctx, wire.CtlNotify, n)
}

// Registry holds the sessions this API serves right now, so a new version of
// the trust list or a notice reaches every open shell.
type Registry struct {
	mu   sync.Mutex
	open map[*Session]bool
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{open: map[*Session]bool{}} }

// Add records a session.
func (r *Registry) Add(s *Session) { r.mu.Lock(); r.open[s] = true; r.mu.Unlock() }

// Remove forgets a session.
func (r *Registry) Remove(s *Session) { r.mu.Lock(); delete(r.open, s); r.mu.Unlock() }

// Count returns how many sessions are open. It takes the same lock as Add,
// Remove and Each, so a caller that holds nothing of its own still waits here for
// whoever holds it -- which is why the cases of this package call it from a
// goroutine of their own.
func (r *Registry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.open)
}

// Each calls fn for every open session. The lock is held only while the list is
// copied, and this sentence is about Each alone: fn writes to a link, which can
// block for as long as a slow browser takes, and a registry locked over that
// would stop every other session from being added, removed or counted. Add,
// Remove and Count hold the lock for the whole of their own work, which is three
// map operations and nothing that waits.
func (r *Registry) Each(fn func(*Session)) {
	r.mu.Lock()
	list := make([]*Session, 0, len(r.open))
	for s := range r.open {
		list = append(list, s)
	}
	r.mu.Unlock()
	for _, s := range list {
		fn(s)
	}
}

// HintAll tells every open shell about a new version; a shell that cannot be
// reached simply fetches it at its next request.
func (r *Registry) HintAll(ctx context.Context, seq uint64) {
	r.Each(func(s *Session) { _ = s.Hint(ctx, seq) })
}

// Broadcast sends one control message to every open shell.
func (r *Registry) Broadcast(ctx context.Context, t string, body any) {
	r.Each(func(s *Session) { _ = s.SendControl(ctx, t, body) })
}
