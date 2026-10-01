package migrate_test

import (
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/migrate"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlite"
)

// The tests of package migrate run on the encrypted file that
// apps/server-db really opens, and this is where that opener comes from.
// Package migrate itself may not import sqlite: sqlite imports migrate, and
// an import back from the package under test is an import cycle to go test.
// This file is in the external test package, where the same import is a plain
// dependency - migrate_test imports sqlite imports migrate, and nothing
// closes the ring.
//
// Both test packages are linked into one binary and every init runs before
// the first test, so openFile has the opener by the time it is called. If
// this file goes, openFile says so and stops instead of opening a file of its
// own.
func init() { migrate.OpenEncrypted = sqlite.OpenDB }
