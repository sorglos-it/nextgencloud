package sqlstore

import (
	"database/sql"
	"testing"
)

// TestNewReadsAndWritesThroughOneHandle is the assurance the MariaDB path
// rests on: New, which is what mariadb.New calls, must leave reads on the
// same handle as writes. The split handle belongs to SQLite alone (point 48,
// 13.10.2026), where one writer waiting for the file lock holds a connection
// a read would need; on MariaDB a waiting writer waits on the server and the
// pool is a pool over the nodes.
func TestNewReadsAndWritesThroughOneHandle(t *testing.T) {
	db := new(sql.DB)
	s, ok := New(db, MariaDB).(*store)
	if !ok {
		t.Fatal("New did not give a *store")
	}
	if s.db != db {
		t.Error("New writes through another handle than the one it was given")
	}
	if s.rdb != db {
		t.Error("New reads through another handle than the one it was given - " +
			"the split belongs to SQLite, not here")
	}
}

// TestNewSplitKeepsTheTwoHandlesApart is the other side: whoever asks for the
// split gets it, and in the right order - write writes, read reads. The two
// swapped would pass every functional case (both handles can do both) and
// undo the whole measurement, so this case reads the fields.
func TestNewSplitKeepsTheTwoHandlesApart(t *testing.T) {
	write, read := new(sql.DB), new(sql.DB)
	s, ok := NewSplit(write, read, SQLite).(*store)
	if !ok {
		t.Fatal("NewSplit did not give a *store")
	}
	if s.db != write {
		t.Error("NewSplit does not write through the write handle")
	}
	if s.rdb != read {
		t.Error("NewSplit does not read through the read handle")
	}
}
