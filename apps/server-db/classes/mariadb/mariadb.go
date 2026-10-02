// Package mariadb opens the MariaDB Galera cluster of an NGC installation:
// every connection over TLS against the CA of the installation, spread over
// the nodes, with the next node taking over when one is down.
package mariadb

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// maxPacket has to be above the largest blob: a canonical ui-tar is at most
// store.MaxUITotal (64 MiB). The server is set to the same value
// (max_allowed_packet in templates/galera.cnf).
const maxPacket = 128 << 20

// OpenDB opens the cluster. It creates nothing: the schema comes from the
// migrations, and the ngc account may not create anything anyway.
func OpenDB(cfg config.DB) (*sql.DB, error) {
	if cfg.Driver != config.DriverMariaDB {
		return nil, fmt.Errorf("mariadb: driver %q, want %q", cfg.Driver, config.DriverMariaDB)
	}
	if len(cfg.Nodes) == 0 {
		return nil, errors.New("mariadb: no node in the configuration")
	}
	nodes := make([]driver.Connector, 0, len(cfg.Nodes))
	for _, node := range cfg.Nodes {
		c, err := connector(cfg, node)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, c)
	}
	db := sql.OpenDB(&balancer{
		nodes:  nodes,
		pick:   &picker{count: len(nodes)},
		now:    time.Now,
		driver: mysql.MySQLDriver{},
	})
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(4)
	// A connection is retired after a while, so the load finds its way back
	// to a node that has come up again instead of sticking where it was.
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(time.Minute)
	return db, nil
}

// connector builds the connector of one node from nodeConfig.
func connector(cfg config.DB, node string) (driver.Connector, error) {
	c, err := nodeConfig(cfg, node)
	if err != nil {
		return nil, err
	}
	return mysql.NewConnector(c)
}

// nodeConfig builds the driver configuration of one node. The TLS
// configuration comes from config.DB.TLSConfig with that node's host as the
// server name, so every node is checked against the CA of the installation and
// its own name. It is its own function because the driver's connector keeps
// its configuration to itself: this is the shape the tests can read.
func nodeConfig(cfg config.DB, node string) (*mysql.Config, error) {
	if !trust.ValidHostPort(node) {
		return nil, fmt.Errorf("mariadb: %q is no host:port", node)
	}
	host, _, err := net.SplitHostPort(node)
	if err != nil {
		return nil, err
	}
	tlsCfg, err := cfg.TLSConfig(host)
	if err != nil {
		return nil, err
	}
	c := mysql.NewConfig()
	c.User = cfg.User
	c.Passwd = cfg.Password.Text()
	c.Net = "tcp"
	c.Addr = node
	c.DBName = cfg.Name
	c.TLS = tlsCfg // never a connection without a checked certificate
	c.AllowFallbackToPlaintext = false
	c.AllowAllFiles = false
	c.AllowCleartextPasswords = false
	c.AllowOldPasswords = false
	c.MultiStatements = false
	// The values never become part of the statement: the server prepares it
	// and gets the values on their own.
	c.InterpolateParams = false
	// An UPDATE has to report the rows it matched, not the ones it changed,
	// or writing the same values again would look like "no such row" - see
	// sqlstore.put and LockTake.
	c.ClientFoundRows = true
	c.CheckConnLiveness = true
	c.ParseTime = false // timestamps are numbers
	c.Loc = time.UTC
	c.MaxAllowedPacket = maxPacket
	c.Timeout = 5 * time.Second
	c.ReadTimeout = 60 * time.Second
	c.WriteTimeout = 60 * time.Second
	// The driver writes to stderr by default; NGC logs on its own and never
	// wants a driver message near a password.
	c.Logger = &mysql.NopLogger{}
	c.Params = map[string]string{
		// A read waits for what the cluster already took: an API that wrote a
		// session on one node has to find it on another one right away.
		"wsrep_sync_wait": "1",
		// Nothing is quietly cut or rounded.
		"sql_mode":  "'STRICT_ALL_TABLES,NO_ENGINE_SUBSTITUTION'",
		"time_zone": "'+00:00'",
	}
	return c, nil
}

// New wraps an open cluster as a store.
func New(db *sql.DB) dbapi.Store { return sqlstore.New(db, sqlstore.MariaDB) }

// Open opens the cluster and returns the store. It runs no migration: that is
// the job of the ngc_migrate account, which the API does not have.
func Open(cfg config.DB) (dbapi.Store, error) {
	db, err := OpenDB(cfg)
	if err != nil {
		return nil, err
	}
	return New(db), nil
}
