package state

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

// genesis returns version 1 signed by the passkey admin of trusttest.
func genesis(tb testing.TB) (trusttest.Signer, trust.Signed) {
	tb.Helper()
	admin := trusttest.NewPasskey(9)
	l := trusttest.Genesis(admin)
	return admin, admin.Sign(tb, l)
}

// genesisSeed returns version 1 as a stored row, the seed of the fuzz target.
func genesisSeed(tb testing.TB) []byte {
	tb.Helper()
	_, sg := genesis(tb)
	b, err := json.Marshal(sg)
	if err != nil {
		tb.Fatalf("marshal: %v", err)
	}
	return b
}

// genesisID returns the installation the genesis list of trusttest names.
func genesisID(tb testing.TB) string {
	tb.Helper()
	_, sg := genesis(tb)
	l, err := trust.Parse(sg.Payload)
	if err != nil {
		tb.Fatalf("genesis: %v", err)
	}
	return l.Installation
}

// newTrusted returns a State whose installation is the one the genesis list
// names, as the setup writes it before version 1 is signed. Without it no
// version 1 binds (checkInstallation).
func newTrusted(tb testing.TB) (*State, dbapi.Store) {
	tb.Helper()
	s, db := newState(tb)
	if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallation,
		Data: []byte(genesisID(tb)), Updated: s.Now()}); err != nil {
		tb.Fatalf("installation: %v", err)
	}
	return s, db
}

// upToDate brings the state up to date and hands the version on, the way
// every caller of Chain has to before it may answer at all.
func upToDate(tb testing.TB, s *State) trust.Version {
	tb.Helper()
	v, err := s.Refresh(ctx)
	if err != nil {
		tb.Fatalf("refresh: %v", err)
	}
	return v
}

func TestTrustWithoutVersion(t *testing.T) {
	s, _ := newState(t)
	if _, err := s.Trust(ctx); !errors.Is(err, ErrNoTrust) {
		t.Fatalf("empty database answered %v", err)
	}
}

func TestAddVersionAndChain(t *testing.T) {
	s, _ := newTrusted(t)
	admin, first := genesis(t)
	v, err := s.AddVersion(ctx, first)
	if err != nil || v.List.Seq != 1 {
		t.Fatalf("version 1: %v", err)
	}
	next := trusttest.Next(t, first)
	second := admin.Sign(t, next)
	if _, err := s.AddVersion(ctx, second); err != nil {
		t.Fatalf("version 2: %v", err)
	}
	// A version that is not the next one is refused, so no one can replay an
	// old list or skip one.
	if _, err := s.AddVersion(ctx, second); !errors.Is(err, ErrOldVersion) {
		t.Fatalf("version 2 twice answered %v", err)
	}
	cur := upToDate(t, s)
	chain, err := s.Chain(ctx, cur, 1)
	if err != nil || len(chain.Versions) != 2 || chain.More {
		t.Fatalf("chain from 1: %v %d %v", err, len(chain.Versions), chain.More)
	}
	if _, err := trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(chain.Versions[0])}, chain.Versions); err != nil {
		t.Fatalf("chain does not verify: %v", err)
	}
	from2, err := s.Chain(ctx, cur, 2)
	if err != nil || len(from2.Versions) != 1 {
		t.Fatalf("chain from 2: %v", err)
	}
	// One past the newest is no error: the caller answers 204.
	if c, err := s.Chain(ctx, cur, 3); err != nil || len(c.Versions) != 0 {
		t.Fatalf("chain from 3: %v %d", err, len(c.Versions))
	}
	if _, err := s.Chain(ctx, cur, 4); !errors.Is(err, ErrOldVersion) {
		t.Fatalf("chain beyond the newest answered %v", err)
	}
}

// TestAddVersionSkipped proves that a gap is refused too: version 3 does not
// go in while version 2 is missing.
func TestAddVersionSkipped(t *testing.T) {
	s, _ := newTrusted(t)
	admin, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	second := admin.Sign(t, trusttest.Next(t, first))
	third := admin.Sign(t, trusttest.Next(t, second))
	if _, err := s.AddVersion(ctx, third); !errors.Is(err, ErrOldVersion) {
		t.Fatalf("version 3 without version 2 answered %v", err)
	}
}

// TestAddVersionFork proves that a second version with the right number but
// another version before it is named as what it is, not as an old one: here
// two chains claim the same number.
func TestAddVersionFork(t *testing.T) {
	s, _ := newTrusted(t)
	admin, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	l := trusttest.Next(t, first)
	l.Prev[0] ^= 1 // points at a version that is not the stored one
	forked := admin.Sign(t, l)
	if _, err := s.AddVersion(ctx, forked); !errors.Is(err, ErrFork) {
		t.Fatalf("fork answered %v", err)
	}
}

// TestAddVersionOtherInstallation proves that a version of another
// installation is named as such, although its number and signer fit.
func TestAddVersionOtherInstallation(t *testing.T) {
	s, _ := newTrusted(t)
	admin, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	l := trusttest.Next(t, first)
	l.Installation = trusttest.ID(0x12)
	other := admin.Sign(t, l)
	if _, err := s.AddVersion(ctx, other); !errors.Is(err, ErrInstallation) {
		t.Fatalf("other installation answered %v", err)
	}
}

// TestForeignRoot proves the one thing that binds version 1: it anchors
// itself, so a whole list a stranger signed for himself passes trust.Open.
// Only the installation the setup stored tells it from ours -- both when it
// comes in through AddVersion and when the row is swapped in the database.
func TestForeignRoot(t *testing.T) {
	s, db := newTrusted(t)
	admin, first := genesis(t)
	l, err := trust.Parse(first.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	l.Installation = trusttest.ID(0x12)
	foreign := admin.Sign(t, l)
	// It is a list in its own right: an admin of itself signed it.
	if _, err := trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(foreign)}, []trust.Signed{foreign}); err != nil {
		t.Fatalf("the foreign list is not whole, the test proves nothing: %v", err)
	}
	if _, err := s.AddVersion(ctx, foreign); !errors.Is(err, ErrInstallation) {
		t.Fatalf("foreign version 1 answered %v", err)
	}
	data, err := json.Marshal(foreign)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobTrust, ID: "1", Seq: 1, Data: data,
		Updated: s.Now()}); err != nil {
		t.Fatalf("put: %v", err)
	}
	// Read out of our own rows it is no longer a list somebody handed in: the
	// ID and version 1 both come from this installation, and they contradict
	// each other.
	if _, err := s.Refresh(ctx); !errors.Is(err, ErrDamaged) {
		t.Fatalf("swapped row answered %v", err)
	}
	if _, err := s.Refresh(ctx); errors.Is(err, ErrInstallation) {
		t.Fatal("a damaged row answers like a list of another installation")
	}
}

// TestAddVersionOnDamagedRows proves that the damage of our own rows does not
// turn into the caller's fault on the way in. AddVersion refreshes before it
// takes anything, and that refresh reads version 1 out of the database and
// holds it against the stored installation ID. Whoever hears "signature
// invalid" for that would sign again -- and no version he could sign would
// mend a database that contradicts itself.
func TestAddVersionOnDamagedRows(t *testing.T) {
	s, db := newTrusted(t)
	admin, first := genesis(t)
	l, err := trust.Parse(first.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	l.Installation = trusttest.ID(0x12)
	foreign := admin.Sign(t, l)
	data, err := json.Marshal(foreign)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobTrust, ID: "1", Seq: 1, Data: data,
		Updated: s.Now()}); err != nil {
		t.Fatalf("put: %v", err)
	}
	// What is handed in is beside the point: the refresh in front of it
	// already fails on the stored rows.
	_, err = s.AddVersion(ctx, first)
	if !errors.Is(err, ErrDamaged) {
		t.Fatalf("damaged rows answered %v", err)
	}
	if errors.Is(err, ErrInstallation) {
		t.Fatal("damaged rows answer like a list of another installation")
	}
}

// TestRootWithoutInstallation proves that version 1 is not taken while the
// setup wrote no installation ID: nothing would bind it.
func TestRootWithoutInstallation(t *testing.T) {
	s, _ := newState(t)
	_, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); !errors.Is(err, ErrSetting) {
		t.Fatalf("version 1 without an installation answered %v", err)
	}
}

// TestAddVersionForeignSigner proves that a version signed by a key the
// current list does not hold is refused, and not as ErrOldVersion: the number
// is right, the signer is not.
func TestAddVersionForeignSigner(t *testing.T) {
	s, _ := newTrusted(t)
	_, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	stranger := trusttest.NewSigner(0x5A)
	forged := stranger.Sign(t, trusttest.Next(t, first))
	_, err := s.AddVersion(ctx, forged)
	if !errors.Is(err, trust.ErrSigner) {
		t.Fatalf("foreign signer answered %v", err)
	}
	// And it says whose fault it is, which package trust's own error does not:
	// the version was handed in. Without that name it would leave this package
	// unnamed, and an error with no name of this package is read one package
	// further out (httpapi.Fault) as the store not answering -- come back in a
	// moment, for a signature no moment mends.
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("a version refused by package trust carries no name of ours: %v", err)
	}
}

// TestAddVersionNotOne proves that the first version an installation takes is
// version 1 and nothing else.
func TestAddVersionNotOne(t *testing.T) {
	s, _ := newTrusted(t)
	admin, first := genesis(t)
	second := admin.Sign(t, trusttest.Next(t, first))
	if _, err := s.AddVersion(ctx, second); !errors.Is(err, ErrOldVersion) {
		t.Fatalf("version 2 as the first one answered %v", err)
	}
}

// TestAddVersionBusy proves that an API which does not get the lock says so
// instead of writing next to the one that holds it.
func TestAddVersionBusy(t *testing.T) {
	s, db := newTrusted(t)
	_, first := genesis(t)
	now := s.Now()
	ok, err := db.LockTake(ctx, trustLock, "api-2", now, now+trustLease)
	if err != nil || !ok {
		t.Fatalf("lock: %v", err)
	}
	if _, err := s.AddVersion(ctx, first); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy lock answered %v", err)
	}
}

// TestRefreshSeesAnotherAPI proves that an API notices what another API wrote
// into the shared database.
func TestRefreshSeesAnotherAPI(t *testing.T) {
	s, db := newTrusted(t)
	admin, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	other := New(s.Cfg, db, s.Box, s.Pin)
	other.Now = s.Now
	if _, err := other.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	second := admin.Sign(t, trusttest.Next(t, first))
	if _, err := s.AddVersion(ctx, second); err != nil {
		t.Fatalf("version 2: %v", err)
	}
	v, err := other.Refresh(ctx)
	if err != nil || v.List.Seq != 2 {
		t.Fatalf("other API is at %v: %v", v.List, err)
	}
}

// TestChainPages proves that one answer holds at most
// session.MaxChainVersions versions and says that more follow, and that a
// fresh API verifies a chain longer than one block up to its newest version.
func TestChainPages(t *testing.T) {
	s, db := newTrusted(t)
	admin, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	total := uint64(session.MaxChainVersions + 2)
	prev := first
	for seq := uint64(2); seq <= total; seq++ {
		prev = admin.Sign(t, trusttest.Next(t, prev))
		if _, err := s.AddVersion(ctx, prev); err != nil {
			t.Fatalf("version %d: %v", seq, err)
		}
	}
	cur := upToDate(t, s)
	page, err := s.Chain(ctx, cur, 1)
	if err != nil || len(page.Versions) != session.MaxChainVersions || !page.More {
		t.Fatalf("first page: %v %d %v", err, len(page.Versions), page.More)
	}
	rest, err := s.Chain(ctx, cur, session.MaxChainVersions+1)
	if err != nil || len(rest.Versions) != 2 || rest.More {
		t.Fatalf("second page: %v %d %v", err, len(rest.Versions), rest.More)
	}
	// A fresh API reads the chain in blocks and arrives at the newest version.
	other := New(s.Cfg, db, s.Box, s.Pin)
	other.Now = s.Now
	v, err := other.Refresh(ctx)
	if err != nil || v.List.Seq != total {
		t.Fatalf("fresh API is at %v: %v", v.List, err)
	}
}

// TestTrustUsesTheCache proves that Trust answers from the cache: a row that
// was removed behind its back does not make it forget the version it verified.
func TestTrustUsesTheCache(t *testing.T) {
	s, db := newTrusted(t)
	_, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	if err := db.BlobDelete(ctx, dbapi.BlobTrust, "1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	v, err := s.Trust(ctx)
	if err != nil || v.List.Seq != 1 {
		t.Fatalf("trust after the row went: %v %v", v.List, err)
	}
	// The answer needs the rows themselves, so Chain says so instead of
	// handing out an empty chain the shell refuses.
	if _, err := s.Chain(ctx, v, 1); !errors.Is(err, ErrSetting) {
		t.Fatalf("chain without its rows answered %v", err)
	}
}

// chainOf stores count versions after first, signed by admin, and returns the
// newest one. The rows it leaves behind are what a fresh API reads back.
func chainOf(tb testing.TB, s *State, admin trusttest.Signer, first trust.Signed, count int) trust.Signed {
	tb.Helper()
	prev := first
	for i := 0; i < count; i++ {
		prev = admin.Sign(tb, trusttest.Next(tb, prev))
		if _, err := s.AddVersion(ctx, prev); err != nil {
			tb.Fatalf("version %d: %v", i+2, err)
		}
	}
	return prev
}

// dropRow removes one stored version, the cheapest way for whoever can write
// the database to put the trust list back on an older state.
func dropRow(tb testing.TB, db dbapi.Store, seq uint64) {
	tb.Helper()
	if err := db.BlobDelete(ctx, dbapi.BlobTrust, strconv.FormatUint(seq, 10)); err != nil {
		tb.Fatalf("delete %d: %v", seq, err)
	}
}

// TestRefreshFindsAHole proves that a missing row is named instead of cutting
// the chain short in silence. A short read would put an older version of the
// trust list back in force -- keys an admin withdrew would count again -- and
// the answer would claim to be the whole chain.
func TestRefreshFindsAHole(t *testing.T) {
	for _, c := range []struct {
		name string
		gone uint64
	}{
		{"in the middle", 3},
		{"the very first", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, db := newTrusted(t)
			admin, first := genesis(t)
			if _, err := s.AddVersion(ctx, first); err != nil {
				t.Fatalf("version 1: %v", err)
			}
			chainOf(t, s, admin, first, 4) // versions 1..5
			dropRow(t, db, c.gone)
			fresh := New(s.Cfg, db, s.Box, s.Pin)
			fresh.Now = s.Now
			if _, err := fresh.Refresh(ctx); !errors.Is(err, ErrSetting) {
				t.Fatalf("refresh over the hole answered %v", err)
			}
			// The whole point is the answer to the shell: it must not get a
			// short chain that calls itself complete. The refresh above is
			// what finds the hole, and a caller whose refresh failed has no
			// version to hand Chain -- which is the one thing Chain still
			// answers by itself.
			if _, err := fresh.Chain(ctx, trust.Version{}, 1); !errors.Is(err, ErrNoTrust) {
				t.Fatalf("chain without a version answered %v", err)
			}
		})
	}
}

// TestRefreshFindsAHoleAboveABlock proves the same where the hole sits right
// above a full block: there the read stops between two blocks, not inside one.
func TestRefreshFindsAHoleAboveABlock(t *testing.T) {
	s, db := newTrusted(t)
	admin, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	chainOf(t, s, admin, first, session.MaxChainVersions+1) // versions 1..66
	dropRow(t, db, session.MaxChainVersions+1)              // the first one of the second block
	fresh := New(s.Cfg, db, s.Box, s.Pin)
	fresh.Now = s.Now
	if _, err := fresh.Refresh(ctx); !errors.Is(err, ErrSetting) {
		t.Fatalf("refresh over the hole answered %v", err)
	}
}

// TestRefreshFindsAHoleWiderThanTheProbes proves a hole is found however wide
// it is. Dropping one row was what the look above a missing number caught for
// certain, and dropping ten asks of whoever can write the database nine more
// statements and nothing else -- the same access, the same moment. What those
// nine bought him was the whole point of the look: every node with an empty cache -- which is every node after a
// restart -- read version 1 as the newest and said no error at all, so every
// key an admin withdrew in 2..11 counted again.
func TestRefreshFindsAHoleWiderThanTheProbes(t *testing.T) {
	s, db := newTrusted(t)
	admin, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	chainOf(t, s, admin, first, 11) // versions 1..12
	for seq := uint64(2); seq <= 11; seq++ {
		dropRow(t, db, seq)
	}
	fresh := New(s.Cfg, db, s.Box, s.Pin)
	fresh.Now = s.Now
	v, err := fresh.Refresh(ctx)
	if !errors.Is(err, ErrSetting) {
		seq := uint64(0)
		if v.List != nil {
			seq = v.List.Seq
		}
		t.Fatalf("a refresh over a hole of ten answered version %d, %v", seq, err)
	}
}

// noRow answers every read of a trust row with an error of its own: the store
// is away, not wrong. dbapi.ErrNotFound is the one answer it does not give,
// because that one says the row is not there, which is an answer.
type noRow struct{ dbapi.Store }

func (n noRow) BlobGet(ctx context.Context, kind, id string) (dbapi.Blob, error) {
	if kind == dbapi.BlobTrust {
		return dbapi.Blob{}, errNoAnswer
	}
	return n.Store.BlobGet(ctx, kind, id)
}

// A refresh asks the trust rows two ways, and a store that is away answers
// neither: without this the double would still hand out the query checkGap
// makes, and which of the two failed first would decide the test.
func (n noRow) BlobFrom(ctx context.Context, kind string, seq uint64) ([]dbapi.Blob, error) {
	if kind == dbapi.BlobTrust {
		return nil, errNoAnswer
	}
	return n.Store.BlobFrom(ctx, kind, seq)
}

var errNoAnswer = errors.New("memstore: the database is not answering")

// TestRefreshNamesTheFaultOfItsOwnRows proves a refresh says which of the two
// it ran into, because past this package there is nothing but the name to go
// by: rows of this installation's own that do not hold together are ErrSetting,
// and an error carrying no name of this package is the store itself.
//
// Both sides of a refresh come out of the same store, so a chain that does not
// chain is never a caller's doing and nothing anybody signs will mend it.
// Handed on as package trust's own error it would read one package further out
// (httpapi.Fault) like a store that did not answer -- something to try again
// in a moment, which it is not.
func TestRefreshNamesTheFaultOfItsOwnRows(t *testing.T) {
	s, db := newTrusted(t)
	_, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	// Version 1 stored a second time as version 2: it reads as a version and
	// it is signed by an admin of the list, and it does not follow the one
	// before it. That is what trust.Open sees, and it is our own row.
	b, err := db.BlobGet(ctx, dbapi.BlobTrust, "1")
	if err != nil {
		t.Fatalf("row 1: %v", err)
	}
	b.ID, b.Seq = "2", 2
	if err := db.BlobPut(ctx, b); err != nil {
		t.Fatalf("row 2: %v", err)
	}
	fresh := New(s.Cfg, db, s.Box, s.Pin)
	fresh.Now = s.Now
	if _, err := fresh.Refresh(ctx); !errors.Is(err, ErrSetting) {
		t.Fatalf("a stored chain that does not hold together answered %v", err)
	}
	// The other half of the same sentence, and the one that makes it worth
	// anything: a store that did not answer must keep none of this package's
	// names, or the place that reads them could not tell the two apart.
	away := New(s.Cfg, noRow{Store: db}, s.Box, s.Pin)
	away.Now = s.Now
	_, err = away.Refresh(ctx)
	if err == nil {
		t.Fatal("a store that answers nothing was refreshed from")
	}
	for _, named := range []error{ErrSetting, ErrDamaged, ErrNoTrust, ErrBusy,
		ErrBadVersion, ErrOldVersion, ErrFork, ErrInstallation} {
		if errors.Is(err, named) {
			t.Fatalf("a store that did not answer came back as %v", named)
		}
	}
}

// TestChainFromZero proves that a number no version ever has is answered as
// the mistake it is and not as a damaged database. session.ParseFrom refuses
// 0 in the query already; this is the second line behind it.
func TestChainFromZero(t *testing.T) {
	s, _ := newTrusted(t)
	_, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	if _, err := s.Chain(ctx, upToDate(t, s), 0); !errors.Is(err, ErrBadVersion) {
		t.Fatalf("chain from 0 answered %v", err)
	}
}

// lateRow hides one trust row from the first look and hands it out from the
// second on. It stands for another API adding versions while this one reads
// past their numbers: versions are written in order, so a row above a number
// means that number is there too, and the read must go on instead of calling
// the chain damaged.
type lateRow struct {
	dbapi.Store
	id   string
	seen int
}

func (l *lateRow) BlobGet(ctx context.Context, kind, id string) (dbapi.Blob, error) {
	if kind == dbapi.BlobTrust && id == l.id {
		if l.seen++; l.seen == 1 {
			return dbapi.Blob{}, dbapi.ErrNotFound
		}
	}
	return l.Store.BlobGet(ctx, kind, id)
}

// TestRefreshReadsOnAfterARace proves that the look above a missing number
// does not turn a busy database into a broken one: a number that is there on
// the second look is read, and the API arrives at the newest version.
func TestRefreshReadsOnAfterARace(t *testing.T) {
	s, db := newTrusted(t)
	admin, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	chainOf(t, s, admin, first, 2) // versions 1..3
	fresh := New(s.Cfg, &lateRow{Store: db, id: "2"}, s.Box, s.Pin)
	fresh.Now = s.Now
	v, err := fresh.Refresh(ctx)
	if err != nil || v.List.Seq != 3 {
		t.Fatalf("refresh after the race is at %v: %v", v.List, err)
	}
}
