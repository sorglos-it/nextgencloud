package mariadb

// What balancer.Connect does with the picker. Three lines of Connect were held
// by nothing: it records a failure, it records an answer, and it gives up when
// the caller's context is done. A reviewer removed each of them and all 236
// cases of this strand stayed green - so these three cases exist.
//
// They drive the balancer directly with a clock of their own, because the
// effect of a recorded failure is only visible in the order of the NEXT
// connection, and failPause is thirty seconds of wall clock.

import (
	"context"
	"database/sql/driver"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// switchConn is a node whose answer the case changes while it runs: a node
// that refuses now and answers later, which is what a node coming back looks
// like. nodes_test.go's fakeConn keeps one fixed answer.
type switchConn struct {
	mu    sync.Mutex
	err   error
	tries int
}

func (s *switchConn) Connect(context.Context) (driver.Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tries++
	if s.err != nil {
		return nil, s.err
	}
	return &fakeDriverConn{}, nil
}

func (s *switchConn) Driver() driver.Driver { return nil }

func (s *switchConn) set(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *switchConn) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tries
}

// refused is the error a node that is not there gives.
var refused = errors.New("refused")

// TestAFailedNodeGoesToTheBackByItself: Connect has to record a failure, not
// only survive it. Without the record failPause never takes effect, a dead
// node is first in line again at every connection, and together with the ten
// seconds a mute node is allowed it would be paid on EVERY connection instead
// of once per pause.
//
// The third connection is the one that counts: the round robin would put the
// broken node first again, and it must not be asked, because it is resting.
func TestAFailedNodeGoesToTheBackByItself(t *testing.T) {
	now := time.Unix(1790000000, 0)
	broken, works := &switchConn{err: refused}, &switchConn{}
	pick := &picker{count: 2}
	bal := &balancer{nodes: []driver.Connector{broken, works}, pick: pick,
		now: func() time.Time { return now }}
	for i := 1; i <= 3; i++ {
		if _, err := bal.Connect(context.Background()); err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
	}
	// Asked once, on the first connection; after that it rests. The clock
	// never moves, so the pause cannot run out.
	if got := broken.count(); got != 1 {
		t.Errorf("the broken node was asked %d times, want 1: a failure is not recorded, "+
			"so the pause of %v never takes effect", got, failPause)
	}
	if got := works.count(); got != 3 {
		t.Errorf("the node that works was asked %d times, want 3", got)
	}
}

// TestANodeThatAnsweredIsInTurnAgainAtOnce: Connect has to record an answer as
// well. Without that record a node stays at the back for the whole pause
// although it is reachable again, and the load keeps piling on the others.
//
// The case puts the picker into the one position where the two cases differ:
// the round robin would ask the node that is resting first, and only a
// recorded answer moves the other one in front of it.
func TestANodeThatAnsweredIsInTurnAgainAtOnce(t *testing.T) {
	now := time.Unix(1790000000, 0)
	first, second := &switchConn{err: refused}, &switchConn{}
	pick := &picker{count: 2}
	bal := &balancer{nodes: []driver.Connector{first, second}, pick: pick,
		now: func() time.Time { return now }}
	// 1: node 0 refuses and is recorded as broken, node 1 answers.
	if _, err := bal.Connect(context.Background()); err != nil {
		t.Fatalf("first connection: %v", err)
	}
	// 2: now the other way round. Node 1 refuses and is recorded, node 0
	// answers - and that answer is what this case is about.
	first.set(nil)
	second.set(refused)
	if _, err := bal.Connect(context.Background()); err != nil {
		t.Fatalf("second connection: %v", err)
	}
	// Turn the round robin so that the next connection would start at node 1.
	// Without the recorded answer both nodes are resting, the comparison says
	// nothing, and node 1 stays in front.
	pick.order(now)
	second.set(nil)
	before := second.count()
	if _, err := bal.Connect(context.Background()); err != nil {
		t.Fatalf("third connection: %v", err)
	}
	if got := second.count(); got != before {
		t.Errorf("the resting node was asked again (%d instead of %d times): an answer is "+
			"not recorded, so the node that answered stays at the back for %v",
			got, before, failPause)
	}
	// Asked on all three connections: it refused once, then answered twice.
	// Without the recorded answer the third connection would go to the
	// resting node instead and this would stay at two.
	if first.count() != 3 {
		t.Errorf("the node that answered was asked %d times, want 3", first.count())
	}
}

// TestConnectStopsWhenTheContextIsDone: with the caller gone there is no point
// in asking the other nodes, and each of them may cost connectDeadline.
// Without that line a cancelled caller still pays for every node in the list.
func TestConnectStopsWhenTheContextIsDone(t *testing.T) {
	now := time.Unix(1790000000, 0)
	list := []*switchConn{{err: refused}, {err: refused}, {err: refused}}
	nodes := make([]driver.Connector, len(list))
	for i, n := range list {
		nodes[i] = n
	}
	bal := &balancer{nodes: nodes, pick: &picker{count: len(nodes)},
		now: func() time.Time { return now }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := bal.Connect(ctx); err == nil {
		t.Fatal("every node refused and Connect answered without an error")
	}
	if got := list[0].count(); got != 1 {
		t.Errorf("the first node was asked %d times, want 1", got)
	}
	for i := 1; i < len(list); i++ {
		if got := list[i].count(); got != 0 {
			t.Errorf("node %d was asked %d times although the caller was already gone", i, got)
		}
	}
}

// TestTheConnectDeadlineStaysBetweenTheDialAndTheRead holds the two bounds the
// number has to sit between, both written out: above config.DB's dial timeout,
// because it has to let a normal connection finish, and well below the
// driver's read timeout, because otherwise it changes nothing. Both numbers
// are read off nodeConfig, so a change there cannot slip past.
func TestTheConnectDeadlineStaysBetweenTheDialAndTheRead(t *testing.T) {
	if connectDeadline != 10*time.Second {
		t.Errorf("connectDeadline is %v, want 10s", connectDeadline)
	}
	cfg := config.DB{Driver: config.DriverMariaDB, Nodes: []string{"db-1.ngc.test:3306"},
		Name: "ngc", User: "ngc", Password: config.NewSecretText("x"), CA: testCA(t)}
	mc, err := nodeConfig(cfg, cfg.Nodes[0])
	if err != nil {
		t.Fatalf("nodeConfig: %v", err)
	}
	if connectDeadline <= mc.Timeout {
		t.Errorf("connectDeadline %v is not above the dial timeout %v: a connection that "+
			"dials slowly would be cut off", connectDeadline, mc.Timeout)
	}
	if connectDeadline >= mc.ReadTimeout {
		t.Errorf("connectDeadline %v is not below the read timeout %v, so it bounds nothing",
			connectDeadline, mc.ReadTimeout)
	}
}
