package mariadb

import (
	"context"
	"database/sql/driver"
	"errors"
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
