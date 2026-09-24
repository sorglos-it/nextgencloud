package sqlstore

import (
	"errors"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	appstore "github.com/sorglos-it/nextgencloud/shared/store" // store is the type of this package
)

// The bound of every column kind. Go checks each value before it goes into a
// statement, so both databases refuse the same input: MariaDB would cut or
// complain, SQLite would take anything, and the conformance tests have to
// come out the same on both.
const (
	MaxKey  = 255     // every {{key}} column: IDs, names, kinds, tokens, hashes
	MaxText = 1024    // every {{text}} column: owner of a lock, detail of a log entry
	MaxDoc  = 1 << 20 // every {{doc}} column: manifest, certificates, keys, lists
	// MaxData is Blob.Data: store.MaxUITotal (64 MiB) plus the tar headers.
	// Server and driver both have to allow more than this in one packet.
	// max_allowed_packet is 64 MiB by default on either side, and a full-size
	// interface already fills those 64 MiB on its own, so without raising it
	// the largest blob comes back as ErrPktTooLarge instead of being written.
	// Setting it is part of the cluster setup and of the DSN.
	MaxData = 80 << 20
	MaxList = 256 // entries of User.Roles or App.Grants (trust allows 256 roles)
	MaxItem = 64  // one entry of such a list (wire.ValidName is at most 32)
)

func okKey(s string) bool  { return len(s) <= MaxKey }
func okText(s string) bool { return len(s) <= MaxText }
func okDoc(b []byte) bool  { return len(b) <= MaxDoc }
func okData(b []byte) bool { return len(b) <= MaxData }

// blob keeps a nil slice out of the driver: a nil []byte binds as NULL, and
// every column is NOT NULL.
func blob(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}

// joinList packs a short list of names into one column, separated by a line
// feed. Every entry a contract allows here (roles: wire.ValidName, grants:
// appstore.NeedRoot and appstore.NeedNetwork) is printable without line
// breaks, so the separator can never appear inside an entry; joinList refuses
// anything else instead of writing something splitList could not take apart.
func joinList(list []string) (string, bool) {
	if len(list) > MaxList {
		return "", false
	}
	for _, s := range list {
		if s == "" || len(s) > MaxItem || strings.ContainsAny(s, "\n\r") {
			return "", false
		}
	}
	return strings.Join(list, "\n"), true
}

// splitList takes a column written by joinList apart. An empty column is an
// empty list, never a list with one empty entry.
func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// errBrokenList says a column does not carry a list this store wrote.
var errBrokenList = errors.New("sqlstore: the list in a column is not one this store wrote")

// readList is the way back out of the database, and it is the only one. A row
// is a value that comes in like any other - the column is a {{doc}}, which
// holds a megabyte on MariaDB, while joinList never writes more than MaxList
// entries of MaxItem bytes. So the length is looked at before the work on it,
// and what comes out has to be a list joinList would write exactly that way
// again; anything else is a row nobody of ours wrote, and it is refused
// instead of turned into a list of any size.
func readList(s string) ([]string, error) {
	if len(s) > MaxList*(MaxItem+1) {
		return nil, errBrokenList
	}
	list := splitList(s)
	if _, ok := joinList(list); !ok {
		return nil, errBrokenList
	}
	return list, nil
}

// checkSeq guards Blob.Seq: it is a uint64 in the contract, but no unsigned
// 64-bit column exists in all five databases, so everything above the signed
// range is refused instead of wrapping around.
func checkSeq(seq uint64) error {
	if seq > 1<<63-1 {
		return dbapi.ErrInvalid
	}
	return nil
}

// errBrokenSeq says a row carries a sequence number this store never wrote.
var errBrokenSeq = errors.New("sqlstore: the sequence number in a row is below zero")

// readSeq is checkSeq the other way round, for the value coming back out of
// the column. checkSeq refuses everything above the signed range so that
// nothing wraps on the way in; a negative number in the row would wrap on the
// way out, into a sequence number near 2^64 that no caller could make sense
// of. Nothing of ours writes one, so it is a row nobody of ours wrote.
func readSeq(n int64) (uint64, error) {
	if n < 0 {
		return 0, errBrokenSeq
	}
	return uint64(n), nil
}

// errBrokenGrants says a column carries a grant this store never wrote.
var errBrokenGrants = errors.New("sqlstore: a grant in a column is not one this store wrote")

// grantsOK reports whether every grant is one the contract knows; an app
// carries at most appstore.NeedRoot and appstore.NeedNetwork.
func grantsOK(list []string) bool {
	for _, g := range list {
		if g != appstore.NeedRoot && g != appstore.NeedNetwork {
			return false
		}
	}
	return true
}
