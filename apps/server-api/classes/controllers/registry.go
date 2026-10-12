// Package controllers holds the tunnels to the controllers: the WebSocket at
// tunnel.PathTunnel with Noise inside, the pairing at tunnel.PathPair, the
// hello exchange and what a controller reports. Every controller dials every
// API, so each API reaches each controller directly; nothing here is state the
// installation depends on, only what is connected right now.
//
// Like package browser, this package keeps the link and the channel table of a
// tunnel in unexported fields, and that is what holds the caller conditions of
// link.Table: whoever cannot reach the link cannot write a frame, and whoever
// cannot reach the table cannot raise what may arrive or note a CLOSE that
// never went out. A dispatcher gets the named ways out of this package and
// nothing else. The writing side of numbered channels comes with the relay
// (task 15); until then the table is read for one thing only, and that is in
// tunnel.go: the API is the opener on a tunnel, so a frame for a number the API
// never opened ends the tunnel.
package controllers

import (
	"sync"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// Tunnel is one open tunnel.
type Tunnel struct {
	// Node is the node ID of the controller, empty for one that paired and
	// waits to be listed. It is set before the tunnel is handed anywhere and
	// never changes, so it needs no lock.
	Node string

	// key is the peer's Noise key as the trust list or the pending list named
	// it. It is kept so that an open tunnel can be asked again whether that key
	// is still trusted: a key the admin withdrew must not keep a tunnel.
	key []byte

	link  *link.Link
	table *link.Table

	mu     sync.Mutex
	hello  tunnel.Hello
	status tunnel.Status
}

// Hello returns the last hello of the controller.
func (t *Tunnel) Hello() tunnel.Hello { t.mu.Lock(); defer t.mu.Unlock(); return t.hello }

// Status returns the last status of the controller.
func (t *Tunnel) Status() tunnel.Status { t.mu.Lock(); defer t.mu.Unlock(); return t.status }

func (t *Tunnel) setHello(h tunnel.Hello)   { t.mu.Lock(); t.hello = h; t.mu.Unlock() }
func (t *Tunnel) setStatus(s tunnel.Status) { t.mu.Lock(); t.status = s; t.mu.Unlock() }

// Registry holds the tunnels this API has right now.
type Registry struct {
	mu   sync.Mutex
	open map[string]*Tunnel
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{open: map[string]*Tunnel{}} }

// Get returns the tunnel to a node.
func (r *Registry) Get(node string) (*Tunnel, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.open[node]
	return t, ok
}

// Nodes returns the nodes with an open tunnel, in no order of its own.
func (r *Registry) Nodes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := make([]string, 0, len(r.open))
	for node := range r.open {
		list = append(list, node)
	}
	return list
}

// add records a tunnel and returns the one it replaced, if any: a controller
// that reconnects after a broken link must not leave the old one behind.
func (r *Registry) add(t *Tunnel) *Tunnel {
	r.mu.Lock()
	defer r.mu.Unlock()
	old := r.open[t.Node]
	r.open[t.Node] = t
	return old
}

// remove forgets a tunnel, unless another one took its place already.
func (r *Registry) remove(t *Tunnel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cur, ok := r.open[t.Node]; ok && cur == t {
		delete(r.open, t.Node)
	}
}

// Each calls fn for every open tunnel. The lock is held only while the list is
// copied, for the reason browser.Registry.Each names: fn may write to a link,
// which can block for as long as a slow controller takes, and a registry locked
// over that would stop every other tunnel from being added or removed.
func (r *Registry) Each(fn func(*Tunnel)) {
	r.mu.Lock()
	list := make([]*Tunnel, 0, len(r.open))
	for _, t := range r.open {
		list = append(list, t)
	}
	r.mu.Unlock()
	for _, t := range list {
		fn(t)
	}
}

// Count returns how many tunnels are open. It takes the same lock as the rest,
// so a caller that holds nothing of its own still waits here for whoever does.
func (r *Registry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.open)
}
