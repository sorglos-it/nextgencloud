package mariadb

// What OpenDB really does, measured against nodes that answer on a socket
// instead of against the configuration it builds. Task 9 held the
// configuration only; the plan says task 10 has to close that, and these
// cases are that payment. Each one goes through OpenDB, not through
// nodeConfig, so a value that is right in nodeConfig but lost on the way to
// the driver falls here.

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// loopbackCert is what a certificate of a fake node on 127.0.0.1 is good for.
var loopbackCert = []string{"127.0.0.1"}

// cluster builds the configuration of an installation whose nodes are the
// given fake ones, with ca as the CA of the installation.
func cluster(ca *testAuthority, nodes ...*fakeNode) config.DB {
	addrs := make([]string, len(nodes))
	for i, n := range nodes {
		addrs[i] = n.addr
	}
	return config.DB{Driver: config.DriverMariaDB, Nodes: addrs,
		Name: "ngc_other", User: "ngc_reader",
		Password: config.NewSecretText("not-the-real-one"), CA: ca.pem}
}

// openCluster opens the pool and fails the case if that already goes wrong.
func openCluster(t *testing.T, cfg config.DB) *sql.DB {
	t.Helper()
	db, err := OpenDB(cfg)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// holdConns opens n connections at the same time and keeps them open until the
// returned function is called, so that the pool really has to reach n nodes
// instead of handing the same connection back n times. Each connect and each
// ping carries its own deadline, so a case cannot hang here.
func holdConns(t *testing.T, db *sql.DB, n int) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conns := make([]*sql.Conn, 0, n)
	for i := 0; i < n; i++ {
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
		if err := c.PingContext(ctx); err != nil {
			t.Fatalf("ping %d: %v", i, err)
		}
		conns = append(conns, c)
	}
	return func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}
}

// TestNodeOverTLSWorksAndNothingElseDoes is the behaviour task 9 could not
// measure: a node whose certificate comes from the CA of the installation is
// reached, and each of the three ways a node can be wrong is refused. The
// wrong ones are checked on what the node SAW, not only on the error: a node
// that got a handshake response would have had the user name and the SHA1
// scramble of the password.
func TestNodeOverTLSWorksAndNothingElseDoes(t *testing.T) {
	ca := newAuthority(t, "ngc-test-ca")
	other := newAuthority(t, "ngc-foreign-ca")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("a node of the installation answers", func(t *testing.T) {
		n := newFakeNode(t, ca, nodeGood, "127.0.0.1", loopbackCert, nil)
		db := openCluster(t, cluster(ca, n))
		if err := db.PingContext(ctx); err != nil {
			t.Fatalf("ping: %v", err)
		}
		accepted, authOK := n.counts()
		if accepted != 1 || authOK != 1 {
			t.Errorf("accepted %d, authenticated %d, want 1 and 1", accepted, authOK)
		}
	})

	// Every one of these three must end without the node ever seeing a
	// handshake response. That packet carries the user name and the 20-byte
	// SHA1 scramble of mysql_native_password - not the password itself, but
	// enough for a listener to replay against this salt, and a name NGC does
	// not hand to anyone who did not show the certificate of the installation.
	for name, node := range map[string]func(*testing.T) *fakeNode{
		// A server that does not offer CLIENT_SSL. With
		// AllowFallbackToPlaintext the driver would carry on in plain text.
		"a server that cannot do TLS": func(t *testing.T) *fakeNode {
			return newFakeNode(t, ca, nodeNoTLS, "127.0.0.1", loopbackCert, nil)
		},
		// A certificate of a CA that is not the one of the installation.
		"a certificate of a foreign CA": func(t *testing.T) *fakeNode {
			return newFakeNode(t, other, nodeGood, "127.0.0.1", loopbackCert, nil)
		},
		// A certificate of the right CA, but for another address: the server
		// name has to be checked, not only the signature.
		"a certificate for another address": func(t *testing.T) *fakeNode {
			return newFakeNode(t, ca, nodeGood, "127.0.0.1", nil, []string{"db-other.ngc.test"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			n := node(t)
			db := openCluster(t, cluster(ca, n))
			if err := db.PingContext(ctx); err == nil {
				t.Fatal("the node was reached although it must not be")
			}
			accepted, authOK := n.counts()
			if accepted == 0 {
				t.Errorf("the node was never tried, so nothing was measured")
			}
			if authOK != 0 {
				t.Errorf("the node saw %d handshake responses, and each one carries the user name "+
					"and the scramble of the password", authOK)
			}
		})
	}
}

// TestEveryNodeGetsItsShareOfTheConnections measures the load spreading task 9
// could only read off the picker: thirteen nodes, thirteen connections at the
// same time, and every node has to get exactly one of them. Which node is
// first is the round robin's business and is held elsewhere; what is held here
// is that none is left out and none is used twice.
//
// Thirteen, not three, on purpose, because Go's sort switches to insertion
// sort below maxInsertion = 12 elements (Go 1.27) and insertion sort is stable
// whether one asks for it or not: with three nodes a picker that sorted
// unstably could not be told from one that sorted stably.
//
// Thirteen is not enough either, and that was measured, not assumed: with
// sort.SliceStable swapped for sort.Slice this case stayed GREEN, because with
// every node healthy the comparator is always false and Go's sort leaves an
// already-sorted slice alone. The stable sort is held by
// TestOrderKeepsTheRoundRobinAmongManyNodes in nodes_test.go, which gives some
// nodes a pause so the comparator has something to say. What this case holds
// is the share itself: every node of a healthy cluster gets one connection.
func TestEveryNodeGetsItsShareOfTheConnections(t *testing.T) {
	const nodes = 13
	ca := newAuthority(t, "ngc-test-ca")
	list := make([]*fakeNode, nodes)
	for i := range list {
		list[i] = newFakeNode(t, ca, nodeGood, "127.0.0.1", loopbackCert, nil)
	}
	db := openCluster(t, cluster(ca, list...))
	release := holdConns(t, db, nodes)
	defer release()
	for i, n := range list {
		accepted, authOK := n.counts()
		if accepted != 1 || authOK != 1 {
			t.Errorf("node %d: accepted %d, authenticated %d, want 1 and 1", i, accepted, authOK)
		}
	}
}

// TestTheClusterSurvivesEveryWayANodeCanFail is the failover task 9 could not
// measure. Each case has one broken node first in the list and two that work,
// and each one has to deliver three connections anyway. The broken node is
// also checked on how far it got, so a case cannot pass because the node was
// never tried.
func TestTheClusterSurvivesEveryWayANodeCanFail(t *testing.T) {
	ca := newAuthority(t, "ngc-test-ca")
	for name, tc := range map[string]struct {
		mode nodeMode
		// dead closes the listener before anything connects: the node is not
		// there at all, which is what a stopped mariadb looks like.
		dead bool
		// wantTried is whether the broken node must have been spoken to; a
		// dead one is not even accepted.
		wantTried bool
	}{
		"a node that is not there":   {mode: nodeGood, dead: true, wantTried: false},
		"a handshake that dies":      {mode: nodeHandshakeDies, wantTried: true},
		"a node that is not primary": {mode: nodeNonPrimary, wantTried: true},
	} {
		t.Run(name, func(t *testing.T) {
			bad := newFakeNode(t, ca, tc.mode, "127.0.0.1", loopbackCert, nil)
			if tc.dead {
				bad.close()
			}
			good := []*fakeNode{
				newFakeNode(t, ca, nodeGood, "127.0.0.1", loopbackCert, nil),
				newFakeNode(t, ca, nodeGood, "127.0.0.1", loopbackCert, nil),
			}
			db := openCluster(t, cluster(ca, bad, good[0], good[1]))
			release := holdConns(t, db, 3)
			defer release()
			// The node that is not primary is the one exception: it answers
			// the handshake and only refuses the SET behind it, so it HAS
			// authenticated - which is exactly what a Galera node outside the
			// primary component does.
			if _, authOK := bad.counts(); authOK != 0 && tc.mode != nodeNonPrimary {
				t.Errorf("the broken node authenticated %d connections", authOK)
			}
			accepted, _ := bad.counts()
			if tc.wantTried && accepted == 0 {
				t.Errorf("the broken node was never tried, so nothing was measured")
			}
			if tc.dead && accepted != 0 {
				t.Errorf("a closed listener accepted %d connections", accepted)
			}
			// All three connections live, and they live on the two nodes that
			// work: the sum of what those two authenticated is three.
			sum := 0
			for _, n := range good {
				_, authOK := n.counts()
				sum += authOK
			}
			if sum != 3 {
				t.Errorf("the working nodes authenticated %d connections, want 3", sum)
			}
		})
	}
}

// TestTheNodeReceivesEverySessionVariable: the three variables of nodeConfig
// arrive at the node as one SET, with exactly the values nodeConfig names.
// The loop runs over the map in mariadb.go's c.Params, so a variable added
// there without a value here cannot slip through unmeasured.
//
// What the server DOES with them is NOT measured: whether MariaDB accepts
// wsrep_sync_wait, sql_mode and time_zone needs a MariaDB, and there is none
// on this machine.
func TestTheNodeReceivesEverySessionVariable(t *testing.T) {
	ca := newAuthority(t, "ngc-test-ca")
	n := newFakeNode(t, ca, nodeGood, "127.0.0.1", loopbackCert, nil)
	cfg := cluster(ca, n)
	db := openCluster(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	got := strings.Join(n.allQueries(), "\n")
	if got == "" {
		t.Fatal("the node received no statement at all")
	}
	// Two halves, and neither does the other's work. The written-out list
	// holds that these three arrive with exactly these values: dropping a line
	// in nodeConfig or changing a value falls here. The loop over nodeConfig's
	// own map holds that nothing ELSE is sent unmeasured: a fourth variable
	// added there without a line here falls too.
	want := map[string]string{
		"wsrep_sync_wait": "1",
		"sql_mode":        "'STRICT_ALL_TABLES,NO_ENGINE_SUBSTITUTION'",
		"time_zone":       "'+00:00'",
	}
	for name, value := range want {
		if s := name + " = " + value; !strings.Contains(got, s) {
			t.Errorf("the node never received %q", s)
		}
	}
	mc, err := nodeConfig(cfg, n.addr)
	if err != nil {
		t.Fatalf("nodeConfig: %v", err)
	}
	if len(mc.Params) != len(want) {
		t.Errorf("nodeConfig sends %d session variables, and %d are measured here",
			len(mc.Params), len(want))
	}
	for name, value := range mc.Params {
		if w, ok := want[name]; !ok {
			t.Errorf("nodeConfig sends %q, which no line here measures", name)
		} else if w != value {
			t.Errorf("%s is %q in nodeConfig and %q here", name, value, w)
		}
	}
}

// TestTheNodeSeesTheFoundRowsCapability: the client asks for found-rows
// semantics in the capability bitmask of its handshake response. sqlstore.put
// and LockTake rest on it, and task 9 could only read the flag of the
// configuration.
//
// What the server then does with it - report matched instead of changed rows -
// is the server's semantics and is NOT measurable without a MariaDB.
func TestTheNodeSeesTheFoundRowsCapability(t *testing.T) {
	ca := newAuthority(t, "ngc-test-ca")
	n := newFakeNode(t, ca, nodeGood, "127.0.0.1", loopbackCert, nil)
	db := openCluster(t, cluster(ca, n))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	caps := n.allCaps()
	if len(caps) != 1 {
		t.Fatalf("the node saw %d handshake responses, want 1", len(caps))
	}
	if caps[0]&capFoundRows == 0 {
		t.Errorf("CLIENT_FOUND_ROWS is not set: writing the same row again looks like no such row")
	}
	// The other bits that must not be there, because nodeConfig switches the
	// options off that would set them.
	for name, bit := range map[string]uint32{
		"CLIENT_MULTI_STATEMENTS": 1 << 16,
		"CLIENT_COMPRESS":         1 << 5,
	} {
		if caps[0]&bit != 0 {
			t.Errorf("%s is set although the configuration switches it off", name)
		}
	}
	// And the TLS bit, which is the same statement the refusal cases make from
	// the other side: the client asked for TLS on a connection that worked.
	if caps[0]&capSSL == 0 {
		t.Errorf("CLIENT_SSL is not set although the connection went over TLS")
	}
}

// TestThePoolClosesTheConnectionsAboveMaxIdle measures that maxIdleConns
// really reached the pool. sql.DBStats does not give the setter back, but
// MaxIdleClosed counts its effect: of eight connections handed back at once,
// everything above maxIdleConns is closed. Task 9 had to leave this number
// write-only because it has no connection to count.
//
// connLifetime and connIdleTime are held by the two cases below, which go
// through openDB with milliseconds in place of the minutes. Both numbers
// themselves are held as numbers by TestPoolLimits.
func TestThePoolClosesTheConnectionsAboveMaxIdle(t *testing.T) {
	// Written out, not computed from the constants: eight connections handed
	// back with an idle limit of four leave four to close. Deriving the
	// expected number from maxIdleConns would move both sides together and
	// hold nothing.
	const held, wantClosed = 8, 4
	if maxIdleConns != 4 {
		t.Fatalf("maxIdleConns is %d; this case is written for 4", maxIdleConns)
	}
	if held > maxOpenConns {
		t.Fatalf("the case wants %d connections, the pool allows %d", held, maxOpenConns)
	}
	ca := newAuthority(t, "ngc-test-ca")
	n := newFakeNode(t, ca, nodeGood, "127.0.0.1", loopbackCert, nil)
	db := openCluster(t, cluster(ca, n))
	release := holdConns(t, db, held)
	release()
	if got := db.Stats().MaxIdleClosed; got != wantClosed {
		t.Errorf("the pool closed %d connections above the idle limit, want %d",
			got, wantClosed)
	}
}

// TestAnIPv6NodeSurvivesTheWholeWayThroughOpenDB: task 9 held the server name
// of "[2001:db8::5]:3306" by calling nodeConfig directly. Here the brackets go
// all the way through OpenDB, the driver and a real TLS handshake, against a
// node on the IPv6 loopback.
func TestAnIPv6NodeSurvivesTheWholeWayThroughOpenDB(t *testing.T) {
	ca := newAuthority(t, "ngc-test-ca")
	n := newFakeNode(t, ca, nodeGood, "::1", []string{"::1"}, nil)
	if !strings.HasPrefix(n.addr, "[::1]:") {
		t.Fatalf("the node is called %q, which is no bracketed IPv6 address", n.addr)
	}
	db := openCluster(t, cluster(ca, n))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping on %s: %v", n.addr, err)
	}
	if accepted, authOK := n.counts(); accepted != 1 || authOK != 1 {
		t.Errorf("accepted %d, authenticated %d, want 1 and 1", accepted, authOK)
	}
}

// TestAMuteNodeIsCutOffByOurOwnDeadline is the finding of task 10 turned into
// the barrier for the fix. Three statements in one case, because they cost ten
// seconds of wall clock together and thirty apart:
//
//  1. config.DB's dial timeout does NOT bound a node that accepts the
//     connection and then says nothing - it wraps the dialer only. The case
//     holds that the call is still running one second past that timeout, and
//     it reads the timeout off nodeConfig instead of writing 5s out, so a
//     change there cannot make this case lie.
//  2. connectDeadline does bound it. Measured before the fix with
//     context.Background(): the call came back after 1m0.004s - the driver's
//     read timeout - and the healthy nodes had seen nothing at all.
//  3. Silence is not an answer, so the healthy nodes behind the mute one have
//     to be reached and the connection has to work.
//
// The measuring part runs in its own goroutine and the case only waits, so a
// change that lets the call run on ends as a named failure instead of a
// hanging package.
func TestAMuteNodeIsCutOffByOurOwnDeadline(t *testing.T) {
	ca := newAuthority(t, "ngc-test-ca")
	mute := newFakeNode(t, ca, nodeSilent, "127.0.0.1", loopbackCert, nil)
	healthy := []*fakeNode{
		newFakeNode(t, ca, nodeGood, "127.0.0.1", loopbackCert, nil),
		newFakeNode(t, ca, nodeGood, "127.0.0.1", loopbackCert, nil),
	}
	cfg := cluster(ca, mute, healthy[0], healthy[1])
	db := openCluster(t, cfg)
	mc, err := nodeConfig(cfg, mute.addr)
	if err != nil {
		t.Fatalf("nodeConfig: %v", err)
	}
	if connectDeadline <= mc.Timeout {
		t.Fatalf("connectDeadline %v is not above the dial timeout %v, so this case cannot "+
			"tell the two apart", connectDeadline, mc.Timeout)
	}

	// No deadline of its own: that is the caller the finding is about.
	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- db.PingContext(context.Background()) }()

	select {
	case err := <-done:
		t.Fatalf("the call was back after %v (error %v), within the dial timeout of %v: "+
			"this case no longer measures what it claims", time.Since(start), err, mc.Timeout)
	case <-time.After(mc.Timeout + time.Second):
	}
	// Generous, because the mute node costs connectDeadline and the healthy
	// one still has to do TLS: what this bound rules out is the driver's read
	// timeout of mc.ReadTimeout, which is six times as long.
	wait := connectDeadline + 30*time.Second
	if wait >= mc.ReadTimeout {
		t.Fatalf("a wait of %v does not rule out the read timeout of %v", wait, mc.ReadTimeout)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the mute node took the whole cluster down: %v", err)
		}
		t.Logf("the call came back after %v", time.Since(start))
	case <-time.After(wait):
		t.Fatalf("after %v the mute node still holds the caller: connectDeadline (%v) "+
			"does not bound it", wait, connectDeadline)
	}
	if accepted, authOK := mute.counts(); accepted == 0 || authOK != 0 {
		t.Errorf("the mute node accepted %d and authenticated %d: want at least one accept "+
			"and no authentication", accepted, authOK)
	}
	sum := 0
	for _, n := range healthy {
		_, authOK := n.counts()
		sum += authOK
	}
	if sum != 1 {
		t.Errorf("the healthy nodes authenticated %d connections, want 1: silence behind "+
			"the first node left them unreachable", sum)
	}
}

// openShortCluster opens the pool with the two deadlines shortened, so that
// what the installation waits a minute and five minutes for happens inside a
// case. Nothing else changes: the same OpenDB body, the same connectors, the
// same nodes.
func openShortCluster(t *testing.T, cfg config.DB, life lifetime, idle idleTime) *sql.DB {
	t.Helper()
	db, err := openDB(cfg, life, idle)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestThePoolRetiresAConnectionAtItsLifetime measures that
// db.SetConnMaxLifetime reached the pool at all. It is the line connLifetime
// exists for: a connection is retired so that the next one goes through the
// picker again instead of one connection staying on the node it first
// reached. Whether the load then really moves back to a node that came up
// again is still NOT measured - that needs a cluster.
//
// The deadline is a millisecond instead of five minutes, and the wait is a
// real one: database/sql reads time.Now() itself. MaxLifetimeClosed is the
// counter database/sql keeps for exactly this reason, and the node's own
// count of accepted connections says the same thing from the other side.
func TestThePoolRetiresAConnectionAtItsLifetime(t *testing.T) {
	ca := newAuthority(t, "ngc-test-ca")
	n := newFakeNode(t, ca, nodeGood, "127.0.0.1", loopbackCert, nil)
	// The idle deadline is off, so nothing but the lifetime can close this
	// connection and the counter below names one cause only.
	db := openShortCluster(t, cluster(ca, n), lifetime(time.Millisecond), 0)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("first ping: %v", err)
	}
	time.Sleep(20 * time.Millisecond) // past a deadline of one millisecond
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("second ping: %v", err)
	}
	if got := db.Stats().MaxLifetimeClosed; got != 1 {
		t.Errorf("the pool retired %d connections at their lifetime, want 1", got)
	}
	if accepted, _ := n.counts(); accepted != 2 {
		t.Errorf("the node accepted %d connections, want 2: the retired one and its replacement", accepted)
	}
}

// TestThePoolDropsAConnectionThatSatIdle measures that
// db.SetConnMaxIdleTime reached the pool. Its own counter is
// MaxIdleTimeClosed, and it is a different one from MaxIdleClosed above: that
// one counts what the idle COUNT closes, this one what the idle TIME closes,
// and a case that read the wrong one would pass with this line deleted.
//
// The wait is seconds, not milliseconds, and that is database/sql, not
// slack: the cleaner that drops an idle connection runs at most once per
// second (sql.go, connectionCleaner: minInterval = time.Second), however
// short the deadline is. So the case polls up to waitFor instead of sleeping
// a fixed time, and it names the counter when it gives up.
func TestThePoolDropsAConnectionThatSatIdle(t *testing.T) {
	const waitFor = 15 * time.Second
	ca := newAuthority(t, "ngc-test-ca")
	n := newFakeNode(t, ca, nodeGood, "127.0.0.1", loopbackCert, nil)
	// The lifetime is off: MaxIdleTimeClosed is only ever raised by the idle
	// deadline, and with the lifetime running too this case could not say
	// which of the two lines it held.
	db := openShortCluster(t, cluster(ca, n), 0, idleTime(time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	deadline := time.Now().Add(waitFor)
	for db.Stats().MaxIdleTimeClosed == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the pool dropped no idle connection within %v", waitFor)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := db.Stats().MaxIdleTimeClosed; got != 1 {
		t.Errorf("the pool dropped %d connections for sitting idle, want 1", got)
	}
	// Nothing asked for a connection after the drop, so the node saw exactly
	// the one: this case measures a connection going away, not coming back.
	if accepted, _ := n.counts(); accepted != 1 {
		t.Errorf("the node accepted %d connections, want 1", accepted)
	}
}
