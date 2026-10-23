package mariadb

import (
	"database/sql"
	"testing"
)

// TestMariaDBKeepsItsOnePoolOfSixteen is the counterweight to the split
// handle of SQLite (point 48, 13.10.2026). There, a pool of four was the
// limit of the process: a writer waiting for the file lock held one of the
// four connections, so four waiting writers stopped every read. SQLite
// therefore got a write handle of one connection and a read handle of its
// own.
//
// None of that holds here, and nothing of it was changed here. MariaDB has no
// single-writer file: a writer that waits waits on the server, the pool is a
// pool over the nodes of a cluster, and sixteen connections are sixteen
// conversations the server really runs at once. This case is the one that
// says so out loud, so that a later hand does not carry the SQLite number
// over: mariadb.New builds the store through sqlstore.New, which reads and
// writes through the ONE handle, and the pool stays at sixteen.
func TestMariaDBKeepsItsOnePoolOfSixteen(t *testing.T) {
	// A pool that opens nothing: sql.OpenDB does not dial, and this case asks
	// only about the numbers, so no node has to answer.
	ca := newAuthority(t, "ngc-db-ca")
	node := newFakeNode(t, ca, nodeGood, "127.0.0.1", loopbackCert, nil)
	db := openCluster(t, cluster(ca, node))
	if got := db.Stats().MaxOpenConnections; got != maxOpenConns {
		t.Errorf("MariaDB has %d connections at once, want 16 - the SQLite "+
			"change of point 48 must not reach this driver", got)
	}
	// And the store on it is the one-handle shape, not the split one. There
	// is no second handle to compare against, so what this holds is that
	// mariadb.New takes exactly one: a split store would need two.
	var _ func(*sql.DB) = func(d *sql.DB) { New(d) }
}
