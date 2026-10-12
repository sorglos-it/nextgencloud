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

// Opener opens a store for cfg.
type Opener func(ctx context.Context, cfg config.DB) (dbapi.Store, error)

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

// Open opens the database of cfg.
func Open(ctx context.Context, cfg config.DB) (dbapi.Store, error) {
	mu.Lock()
	o := opener
	mu.Unlock()
	if o == nil {
		return nil, ErrNoOpener
	}
	return o(ctx, cfg)
}
