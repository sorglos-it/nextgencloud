package register

import (
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbwait"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/mariadb"
)

// clusterNodes is the shape the installation RUNS and the smallest shape
// Galera can keep a majority in: three nodes (10.2.3.212, .217, .218, read
// 25.10.2026). It is deliberately NOT config's own bound on db.nodes, which
// is sixteen: no dial timeout worth having covers fifteen dead nodes inside a
// five second user budget, and shared/ is frozen, so that bound cannot be
// brought down here. What this file holds is the operating shape, and the
// gap to the configured bound is named in the case below instead of hidden.
const clusterNodes = 3

// TestADeadNodeLeavesTheUserBudgetIntact is the case the 503 of 24.10.2026
// would have failed. Two of three nodes gone, the third fine: the dials of
// the dead ones have to fit inside the deadline of a user path with the
// healthy node's own work still to come, or the caller meets the deadline
// instead of the answer - which is what 5,03 s and reason=db-busy were.
//
// The two numbers live in two programs (apps/server-db and apps/server-api)
// and nothing but this file holds them against each other. It is HERE because
// register is the one package that already sees both sides: it hands
// apps/server-api's dbopen the opener of apps/server-db.
func TestADeadNodeLeavesTheUserBudgetIntact(t *testing.T) {
	spent := time.Duration(clusterNodes-1) * mariadb.DialTimeout
	// Half the budget is the floor, and it is a floor and not an exact figure
	// on purpose: what the healthy node then needs - dial, TLS handshake,
	// authentication, the SET of wsrep_sync_wait and the statement itself -
	// is a handful of round trips on this LAN and nothing here measures it.
	// Leaving half says "there is room for all of that several times over"
	// without pretending to know the figure.
	if left := dbwait.UserWait - spent; left < dbwait.UserWait/2 {
		t.Errorf("with %d of %d nodes dead the dials spend %v of the %v a user path has, "+
			"leaving %v for the healthy node - a caller meets the deadline instead of the answer",
			clusterNodes-1, clusterNodes, spent, dbwait.UserWait, left)
	}
}

// TestTheDialTimeoutIsNotBelowWhatAHealthyDialCosts is the other wall, and it
// is the one the warning is about: a deadline short enough to turn a node
// that is merely slow into a dead one buys nothing. Measured 25.10.2026, 30
// dials from api-1 to the three nodes: 0,101 ms at best, 4,129 ms at worst.
// The case asks for a hundred times the worst of those, which is the margin
// the constant claims to have, not the measurement itself.
func TestTheDialTimeoutIsNotBelowWhatAHealthyDialCosts(t *testing.T) {
	const worstHealthyDial = 4129 * time.Microsecond
	if mariadb.DialTimeout < 100*worstHealthyDial {
		t.Errorf("a dial timeout of %v is under a hundred times the worst healthy dial measured (%v), "+
			"so a loaded node or an ARP miss is read as a dead machine",
			mariadb.DialTimeout, worstHealthyDial)
	}
}
