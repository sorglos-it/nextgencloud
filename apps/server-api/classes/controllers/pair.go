package controllers

import (
	"context"
	"crypto/ecdh"
	"crypto/sha256"
	"net/http"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbwait"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// NewPairCode makes a pairing code for the ulh script of a new controller: it
// names this API, its TLS pin and the newest version of the trust list, so the
// shell can check the code against what it verified itself before it shows it.
//
// The secret travels in the code and nowhere else. What the database keeps is
// its SHA-256 as the one-time value, and the secret itself as that value's
// data, so the API can check a MAC against it exactly once.
func (srv *Server) NewPairCode(ctx context.Context) (tunnel.PairCode, int64, error) {
	v, err := srv.S.Trust(ctx)
	if err != nil {
		return tunnel.PairCode{}, 0, err
	}
	secret, err := secrets.Bytes(tunnel.SecretSize)
	if err != nil {
		return tunnel.PairCode{}, 0, err
	}
	pub, err := srv.noisePublic()
	if err != nil {
		return tunnel.PairCode{}, 0, err
	}
	expires := srv.S.Now() + tunnel.PairLifetime
	h := sha256.Sum256(secret)
	if err := srv.S.DB.OnceCreate(ctx, dbapi.OncePair, h[:], enc.B64.EncodeToString(secret), expires); err != nil {
		return tunnel.PairCode{}, 0, err
	}
	return tunnel.PairCode{
		URL: srv.S.Cfg.URL, Key: pub, TLS: enc.Bytes(srv.S.Pin), Secret: enc.Bytes(secret),
		Anchor: trust.Anchor{Seq: v.List.Seq, Hash: enc.Bytes(v.Hash)},
	}, expires, nil
}

// Pair answers POST tunnel.PathPair?secret=<base64url of the SHA-256 of the
// pairing secret>. The query names which one-time value to spend; the MAC
// proves that the sender holds the secret itself, which never travels back.
//
// The order is the point of this handler: the one-time value is spent BEFORE
// the MAC is checked, so a wrong MAC costs the attempt. Whoever holds the hash
// of a secret but not the secret can therefore burn a pairing, which is the
// safe half of that trade -- an admin makes a new code -- while the other order
// would let anyone who ever saw a code guess at the MAC for a day.
func (srv *Server) Pair(w http.ResponseWriter, r *http.Request) {
	body, ok := httpapi.Body(w, r)
	if !ok {
		return
	}
	// The deadline of a path somebody is waiting on, set HERE rather than on
	// the route because this path is one of the four that do not go over
	// httpapi.Routes.Mux (server.routes) -- so the deadline point 49 gave the
	// five session routes never reached it. Without one a store call below
	// waits busy_timeout times maxTries, 80,6 s measured (point 48), and then
	// answers the word for a database that is AWAY.
	//
	// After Body and not before it: Body may take httpapi.readLimit (15 s)
	// for a body that trickles in, and a deadline started before it would
	// spend itself on the reading and then call a slow controller's database
	// busy.
	ctx, cancel := dbwait.User(r.Context())
	defer cancel()
	req, err := tunnel.ParsePairRequest(body)
	if err != nil {
		httpapi.Fail(w, wire.CodeProtocol)
		return
	}
	hash, err := enc.B64.DecodeString(r.URL.Query().Get("secret"))
	if err != nil || len(hash) != sha256.Size {
		httpapi.Fail(w, wire.CodeProtocol)
		return
	}
	pub, err := srv.noisePublic()
	if err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return
	}
	data, err := srv.S.DB.OnceUse(ctx, dbapi.OncePair, hash, srv.S.Now())
	if err != nil {
		// Unknown, expired and already used are one answer: telling them apart
		// would say whether a hash was ever a secret of this installation.
		//
		// A database that did not come free is NOT one of those three, and
		// answering E_VERIFY for it told a controller that is pairing
		// correctly that its code was wrong -- measured, 403 while a second
		// program held the file, and an admin would have made a new code for
		// nothing.
		//
		// It opens no oracle, and that rests on TWO assurances in two
		// packages, because neither alone is enough. Here:
		// TestPairSaysNothingAboutWhetherACodeExists holds that this handler
		// makes the same store calls in the same order and writes the same
		// status, headers and body for a hash that exists and one that never
		// did -- with slices.Equal and maps.EqualFunc, not with a sentence.
		// But its store is a stub that blocks every call, so it cannot say
		// anything about the store itself.
		// The other half is the store's: sqlstore.OnceUse does its whole
		// lookup inside ONE write transaction, so the lock is met before any
		// row is read. That is held where it belongs, by
		// TestOnceUseMeetsTheLockBeforeItReadsARow in
		// apps/server-db/classes/sqlstore -- against a real file under a real
		// foreign lock, and a mutant that reads the row before the write dies
		// on it with "not found" for the unknown value.
		if dbwait.Busy(err) {
			httpapi.FailCoded(w, wire.CodeUnreachable, err)
			return
		}
		httpapi.Fail(w, wire.CodeVerify)
		return
	}
	secret, err := enc.B64.DecodeString(data)
	if err != nil || req.Verify(secret, pub) != nil {
		httpapi.Fail(w, wire.CodeVerify)
		return
	}
	if err := srv.S.DB.PendingPut(ctx, dbapi.Pending{Name: req.Name, Key: req.Noise,
		Since: srv.S.Now()}); err != nil {
		// The code is the same E_UNREACHABLE as before -- a word of its own
		// for a busy database is a new wire.Code and that list is in frozen
		// shared/wire/wire.go:450-462, BLOCKED, P9 of the v1.3 row. What the
		// error adds is the Retry-After and the reason in the log.
		httpapi.FailCoded(w, wire.CodeUnreachable, err)
		return
	}
	httpapi.NoContent(w)
}

// noisePublic returns this API's public Noise key, derived from the private
// half in the configuration so that nothing has to keep the two in step.
func (srv *Server) noisePublic() (enc.Bytes, error) {
	k, err := ecdh.X25519().NewPrivateKey(srv.S.Cfg.NoiseKey.Bytes())
	if err != nil {
		return nil, err
	}
	return enc.Bytes(k.PublicKey().Bytes()), nil
}
