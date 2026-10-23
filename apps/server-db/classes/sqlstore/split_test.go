package sqlstore

import (
	"context"
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

// TestNewGivesTheMariaDBPathNoTurnQueue is the second half of the MariaDB
// assurance, and it is about the queue rather than the handles. NewSplit
// bounds how long a writer waits in FRONT of the write handle, because a
// SQLite write handle of one connection is a queue of one. MariaDB has no
// such queue: a writer that waits there waits on the server, the pool is a
// pool over the nodes, and sixteen connections are sixteen conversations the
// server really runs. A turn channel on that path would be a second, narrower
// limit nobody measured.
//
// What is held: New leaves turn nil, so takeTurn is a no-op and ErrTurn can
// never reach a MariaDB caller. Read off the field, because the behaviour it
// guards (a writer refused after one chain) cannot be produced without a
// server.
func TestNewGivesTheMariaDBPathNoTurnQueue(t *testing.T) {
	// A handle that would answer MaxOpenConnections, which is what NewSplit
	// reads to size its queue: if New ever started sizing one too, this is
	// the shape it would do it from.
	db := new(sql.DB)
	db.SetMaxOpenConns(16)
	s, ok := New(db, MariaDB).(*store)
	if !ok {
		t.Fatal("New did not give a *store")
	}
	if s.turn != nil {
		t.Error("New queues MariaDB writers in front of the handle; " +
			"the queue belongs to the single-writer file, not here")
	}
	if s.turnWait != 0 {
		t.Errorf("New carries a turn bound of %v on the MariaDB path", s.turnWait)
	}
	// And the no-op really is one: takeTurn must let a writer through at
	// once and hand back a function that can be called.
	done, err := s.takeTurn(context.Background())
	if err != nil {
		t.Fatalf("a MariaDB writer was refused a turn: %v", err)
	}
	done()
}
