// Package dbopen opens the database the configuration names. The part
// apps/server-db implements dbapi for MariaDB and SQLite and registers its
// opener here, so the API itself holds no driver and no SQL. Without a
// registered opener the API refuses to start with a clear message instead of
// running half way.
package dbopen

import (
	"context"
	"errors"
	"sync"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// Opener opens a store for cfg. owner names who opens it and goes into the
// migration lock ngc_lock: on SQLite every Open migrates (apps/server-db
// classes/sqlite, Open), so the lock is what keeps two openers of one file
// apart, and owner is the only part of that row a human reads. It is an
// argument and not a value of apps/server-db because only the program knows
// its own node.
type Opener func(ctx context.Context, cfg config.DB, owner string) (dbapi.Store, error)

// Owner is the first part of what ngc-api writes into ngc_lock, and it is a
// DECISION of this file. It names the PROGRAM, as ngc-controller does for
// itself: the one thing an admin wants from that row is which program is
// migrating, and "ngc" would say nothing.
const Owner = "ngc-api"

// OwnerOf is that name for one node. The form is Owner + "/" + node, one part
// shorter than the controller's "ngc-controller/<name>[/<node>]", and the
// reason is in the contract rather than in taste: config.API has no Name
// field at all, and its Node is never empty -- (*API).Validate refuses a
// configuration without a node ID, so unlike a freshly paired controller an
// API always has this one name. A second part would have to be invented here,
// and an invented name in a lock is worse than a short one.
//
// It is NOT the database name or the file path: two APIs of one installation
// share the database, and whoever reads the lock wants to know which SERVER
// holds it.
func OwnerOf(node string) string { return Owner + "/" + node }

// ErrNoOpener reports that no part registered an opener.
var ErrNoOpener = errors.New("dbopen: no database implementation registered")

var (
	mu     sync.Mutex
	opener Opener
)

// Register makes o the opener. main.go calls it once with the opener of
// apps/server-db.
func Register(o Opener) {
	mu.Lock()
	defer mu.Unlock()
	opener = o
}

// Open opens the database of cfg. owner travels on to the migration lock;
// OwnerOf makes it out of the node ID.
func Open(ctx context.Context, cfg config.DB, owner string) (dbapi.Store, error) {
	mu.Lock()
	o := opener
	mu.Unlock()
	if o == nil {
		return nil, ErrNoOpener
	}
	return o(ctx, cfg, owner)
}
