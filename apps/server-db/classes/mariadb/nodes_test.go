package mariadb

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestOrder: every connection starts at the next node, so the load spreads;
// a node that just failed goes to the back until its pause is over, but it is
// never dropped - with every node broken, all of them are tried again.
func TestOrder(t *testing.T) {
	now := time.Unix(1790000000, 0)
	p := &picker{count: 3}
	if got := p.order(now); !slices.Equal(got, []int{0, 1, 2}) {
		t.Fatalf("first: %v", got)
	}
	if got := p.order(now); !slices.Equal(got, []int{1, 2, 0}) {
		t.Fatalf("second: %v", got)
	}
	if got := p.order(now); !slices.Equal(got, []int{2, 0, 1}) {
		t.Fatalf("third: %v", got)
	}
	// Node 0 fails: it comes last while the pause lasts.
	p.failed(0, now)
	got := p.order(now.Add(time.Second))
	if len(got) != 3 || got[len(got)-1] != 0 {
		t.Fatalf("after a failure: %v", got)
	}
	// After the pause it is in turn again: nothing is held at the back any
	// more, so the order is the plain round robin.
	if got := p.order(now.Add(failPause + time.Second)); !rotation(got, 3) {
		t.Errorf("after the pause: %v", got)
	}
	// A node that answered is in turn again at once, without waiting out the
	// pause: node 0 still rests and goes last, node 1 does not.
	p.failed(0, now)
	p.failed(1, now)
	p.works(1)
	if got := p.order(now); len(got) != 3 || got[len(got)-1] != 0 {
		t.Errorf("after an answer: %v", got)
	}
	// Every node broken: all of them are still tried.
	for i := range 3 {
		p.failed(i, now)
	}
	if got := p.order(now); len(got) != 3 {
		t.Errorf("with every node broken: %v", got)
	}
}

// rotation reports whether got is the plain round robin over n nodes: n
// different indices, each the one before plus one, modulo n.
func rotation(got []int, n int) bool {
	if len(got) != n {
		return false
	}
	for i, v := range got {
		if v != (got[0]+i)%n {
			return false
		}
	}
	return true
}

// TestOrderWithoutANodeDoesNotDivideByZero: the picker of an empty cluster
// returns nothing instead of dividing by zero, so Connect reaches its own
// error for it.
func TestOrderWithoutANodeDoesNotDivideByZero(t *testing.T) {
	p := &picker{count: 0}
	if got := p.order(time.Unix(1790000000, 0)); len(got) != 0 {
		t.Fatalf("empty cluster: %v", got)
	}
}

// fakeConn is a connector that answers, or refuses with a text that must never
// reach the caller of Connect.
type fakeConn struct {
	mu    sync.Mutex
	err   error
	tries int
}

func (f *fakeConn) Connect(context.Context) (driver.Conn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tries++
	if f.err != nil {
		return nil, f.err
	}
	return &fakeDriverConn{}, nil
}

func (f *fakeConn) Driver() driver.Driver { return nil }

// fakeDriverConn is what a node that answers hands back. There is no server
// behind these tests, and Connect only passes the connection on, so nothing
// of it is ever used.
type fakeDriverConn struct{}

func (*fakeDriverConn) Prepare(string) (driver.Stmt, error) { return nil, errors.ErrUnsupported }
func (*fakeDriverConn) Close() error                        { return nil }
func (*fakeDriverConn) Begin() (driver.Tx, error)           { return nil, errors.ErrUnsupported }

func (f *fakeConn) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tries
}

// TestConnectTriesEveryNodeAndHidesTheDriverText: with every node refusing,
// Connect asks all of them, and the error it returns carries neither the
// address nor the user name the driver would name.
func TestConnectTriesEveryNodeAndHidesTheDriverText(t *testing.T) {
	secret := errors.New("dial tcp db-1.ngc.test:3306: user 'ngc' rejected")
	a, b := &fakeConn{err: secret}, &fakeConn{err: secret}
	now := time.Unix(1790000000, 0)
	bal := &balancer{nodes: []driver.Connector{a, b}, pick: &picker{count: 2},
		now: func() time.Time { return now }}
	_, err := bal.Connect(context.Background())
	if err == nil {
		t.Fatal("every node refused and Connect answered without an error")
	}
	if a.count() != 1 || b.count() != 1 {
		t.Errorf("tries: %d and %d, want one each", a.count(), b.count())
	}
	for _, leak := range []string{"db-1.ngc.test", "3306", "ngc"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the error names %q: %v", leak, err)
		}
	}
}

// TestConnectWithoutANodeSaysSo: a balancer without a node answers with its
// own error and not with a panic.
func TestConnectWithoutANodeSaysSo(t *testing.T) {
	bal := &balancer{pick: &picker{count: 0}, now: time.Now}
	_, err := bal.Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no node configured") {
		t.Fatalf("without a node: %v", err)
	}
}

// TestConnectStopsAtTheFirstNodeThatAnswers: the second node is never asked
// once the first one answered, so one reachable node carries the load.
func TestConnectStopsAtTheFirstNodeThatAnswers(t *testing.T) {
	first, second := &fakeConn{}, &fakeConn{err: errors.New("refused")}
	now := time.Unix(1790000000, 0)
	bal := &balancer{nodes: []driver.Connector{first, second}, pick: &picker{count: 2},
		now: func() time.Time { return now }}
	conn, err := bal.Connect(context.Background())
	if err != nil || conn == nil {
		t.Fatalf("the first node answered, Connect gave %v, %v", conn, err)
	}
	if second.count() != 0 {
		t.Errorf("the second node was asked %d times", second.count())
	}
}

// TestPickerUnderLoad runs the picker from several goroutines at once, so the
// race detector sees its lock. Without a lock this is a data race.
func TestPickerUnderLoad(t *testing.T) {
	p := &picker{count: 4}
	now := time.Unix(1790000000, 0)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for range 200 {
				if got := p.order(now); len(got) != 4 {
					t.Errorf("order returned %v", got)
					return
				}
				p.failed(g%4, now)
				p.works(g % 4)
			}
		}(g)
	}
	wg.Wait()
}

// TestConnectFollowsThePickerOrder: Connect asks the nodes in the order the
// picker gives, not in the order of the slice. The picker is put into a turned
// position first - node 0 is resting after a failure, so it comes last - and
// the only node that answers is exactly that one. A Connect that walked the
// slice would find node 0 right away and never touch node 1.
func TestConnectFollowsThePickerOrder(t *testing.T) {
	now := time.Unix(1790000000, 0)
	answers := &fakeConn{}
	refuses := &fakeConn{err: errors.New("refused")}
	pick := &picker{count: 2}
	pick.failed(0, now) // node 0 to the back, node 1 first
	bal := &balancer{nodes: []driver.Connector{answers, refuses}, pick: pick,
		now: func() time.Time { return now }}
	conn, err := bal.Connect(context.Background())
	if err != nil || conn == nil {
		t.Fatalf("Connect: %v, %v", conn, err)
	}
	if refuses.count() != 1 {
		t.Errorf("node 1 was asked %d times, want 1: the order of the picker was not followed",
			refuses.count())
	}
	if answers.count() != 1 {
		t.Errorf("node 0 was asked %d times, want 1", answers.count())
	}
}

// TestOrderKeepsTheRoundRobinAmongManyNodes: with more nodes than a short
// insertion sort covers, the nodes that are fine have to keep their round
// robin order. An unstable sort would shuffle them, and the load would stop
// spreading evenly without anything looking broken.
func TestOrderKeepsTheRoundRobinAmongManyNodes(t *testing.T) {
	const n = 40
	now := time.Unix(1790000000, 0)
	p := &picker{count: n}
	// Every third node is resting, the rest are fine.
	for i := 0; i < n; i += 3 {
		p.failed(i, now)
	}
	got := p.order(now)
	if len(got) != n {
		t.Fatalf("order returned %d nodes", len(got))
	}
	var fine, resting []int
	for _, i := range got {
		if i%3 == 0 {
			resting = append(resting, i)
		} else {
			fine = append(fine, i)
		}
	}
	if len(fine)+len(resting) != n || len(resting) != (n+2)/3 {
		t.Fatalf("%d fine and %d resting of %d", len(fine), len(resting), n)
	}
	// The nodes that are fine come first, and among them the round robin
	// order is kept: starting at the first one, every next index is the next
	// one that is not resting, counting upwards modulo n.
	for i, v := range got[:len(fine)] {
		if v != fine[i] {
			t.Fatalf("a resting node stands among the healthy ones: %v", got)
		}
	}
	want := make([]int, 0, len(fine))
	for k := range n {
		if i := (fine[0] + k) % n; i%3 != 0 {
			want = append(want, i)
		}
	}
	if !slices.Equal(fine, want) {
		t.Errorf("the round robin among the healthy nodes was shuffled:\n got %v\nwant %v", fine, want)
	}
}

// TestConnectLogsTheDriverTextOncePerNodeAndReturnsAFixedText: the exact text
// of the driver goes to the debug log, once for every node that refused, with
// the index of that node. The text the caller gets is the fixed one, so a
// caller that logs its error logs neither address nor user. That the debug
// line does carry them is the decision, not an accident: the debug log is
// turned on deliberately.
func TestConnectLogsTheDriverTextOncePerNodeAndReturnsAFixedText(t *testing.T) {
	driverErr := errors.New("dial tcp db-1.ngc.test:3306: user 'ngcreader' rejected")
	a, b := &fakeConn{err: driverErr}, &fakeConn{err: driverErr}
	now := time.Unix(1790000000, 0)
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)

	bal := &balancer{nodes: []driver.Connector{a, b}, pick: &picker{count: 2},
		now: func() time.Time { return now }}
	_, err := bal.Connect(context.Background())
	if err == nil || err.Error() != "mariadb: no database node can be reached" {
		t.Fatalf("the returned error is %v", err)
	}
	out := buf.String()
	if n := strings.Count(out, "a node refused the connection"); n != 2 {
		t.Errorf("%d debug lines, want one for each of the two nodes that refused", n)
	}
	for _, want := range []string{"node=0", "node=1", "error="} {
		if !strings.Contains(out, want) {
			t.Errorf("the debug log misses %q", want)
		}
	}
	if !strings.Contains(out, "ngcreader") {
		t.Error("the driver's own text did not reach the debug log")
	}
	if strings.Contains(err.Error(), "db-1.ngc.test") || strings.Contains(err.Error(), "ngcreader") {
		t.Errorf("the returned error names the node or the user: %v", err)
	}
}
