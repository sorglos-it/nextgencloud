package mariadb

import (
	"context"
	"database/sql/driver"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// failPause is how long a node that refused a connection goes to the back of
// the line. Short enough that a node which comes back is used again soon,
// long enough that a dead node does not slow every connection down.
const failPause = 30 * time.Second

// picker decides which node a new connection goes to: round robin, so the
// load spreads over the Galera nodes, with a node that just failed at the
// back. It never drops a node, because all of them may be unreachable for a
// moment and the next try has to find them again.
type picker struct {
	mu    sync.Mutex
	count int
	next  int
	bad   []time.Time // when each node last refused, zero while it is fine
}

// order returns the nodes to try, best first. Without a node it returns
// nothing: the caller then has an error to report, and the round robin below
// would divide by zero.
func (p *picker) order(now time.Time) []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.count <= 0 {
		return nil
	}
	if p.bad == nil {
		p.bad = make([]time.Time, p.count)
	}
	list := make([]int, p.count)
	for i := range list {
		list[i] = (p.next + i) % p.count
	}
	p.next = (p.next + 1) % p.count
	// A stable sort keeps the round robin among the nodes that are fine.
	sort.SliceStable(list, func(a, b int) bool {
		return p.resting(list[a], now) != p.resting(list[b], now) && !p.resting(list[a], now)
	})
	return list
}

// resting reports whether a node is still in its pause after a failure.
func (p *picker) resting(i int, now time.Time) bool {
	return !p.bad[i].IsZero() && now.Sub(p.bad[i]) < failPause
}

// failed records that a node refused a connection.
func (p *picker) failed(i int, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bad == nil {
		p.bad = make([]time.Time, p.count)
	}
	p.bad[i] = now
}

// works records that a node answered.
func (p *picker) works(i int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bad != nil {
		p.bad[i] = time.Time{}
	}
}

// balancer is the driver.Connector database/sql asks for a connection. It
// hands the request to the next node that answers, so an API keeps working
// while one node is down.
type balancer struct {
	nodes  []driver.Connector
	pick   *picker
	now    func() time.Time
	driver driver.Driver
}

func (b *balancer) Driver() driver.Driver { return b.driver }

func (b *balancer) Connect(ctx context.Context) (driver.Conn, error) {
	var last error
	for _, i := range b.pick.order(b.now()) {
		conn, err := b.nodes[i].Connect(ctx)
		if err == nil {
			b.pick.works(i)
			return conn, nil
		}
		b.pick.failed(i, b.now())
		// The driver's own text goes to the debug log, once for every node
		// that refused, with the index of that node. It is kept out of the
		// error this function returns, because it may carry the user name and
		// the address: a caller logs what it gets, and the debug log is the
		// one place an operator turns on deliberately. What this line passes
		// is the driver's error and nothing else - no DSN is built or logged
		// here; what the driver puts into its own text is its business and
		// NOT measured on this machine.
		slog.Debug("mariadb: a node refused the connection", "node", i, "error", err)
		last = err
		if ctx.Err() != nil {
			break
		}
	}
	if last == nil {
		return nil, errors.New("mariadb: no node configured")
	}
	// This is what the caller gets, and it names neither address nor user:
	// the driver's text stayed in the debug log above. English, like the rest
	// of the code; a German sentence belongs where a person reads it.
	return nil, errors.New("mariadb: no database node can be reached")
}
