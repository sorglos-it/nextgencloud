package controllers

import (
	"context"
	"crypto/ecdh"
	"crypto/sha256"
	"net/http"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/audit"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/reason"
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
//
// Every attempt that gets as far as spending the one-time value is written to
// the log, and that is new: measured on 24.10.2026 at the installation of six
// VMs, twenty refused pairings left the log at exactly 338 lines, before and
// after (docs/messungen-m1-m3.md, M3). The "no failed attempts" in that
// measurement was therefore nothing seen, not nothing happening. See
// audit.Log.Pairing for why the line hangs on the code being spent and not on
// the request arriving.
func (srv *Server) Pair(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, ok := httpapi.Body(w, r)
	if !ok {
		return
	}
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
		httpapi.Fail(w, wire.CodeVerify)
		return
	}
	secret, err := enc.B64.DecodeString(data)
	if err != nil {
		// The code was spent and the row behind it does not read: that is
		// damage of ours, not a wrong proof, and the two must not share one
		// line. The answer outside stays the same either way.
		srv.note(ctx, wire.CodeVerify, reason.Defect)
		httpapi.Fail(w, wire.CodeVerify)
		return
	}
	if req.Verify(secret, pub) != nil {
		// THE line Frage 23 is about: a real code was burnt by somebody who
		// did not hold its secret.
		srv.note(ctx, wire.CodeVerify, reason.PairProof)
		httpapi.Fail(w, wire.CodeVerify)
		return
	}
	if err := srv.S.DB.PendingPut(ctx, dbapi.Pending{Name: req.Name, Key: req.Noise,
		Since: srv.S.Now()}); err != nil {
		// The code is gone and nothing was kept, so the admin has to make a
		// new one. A line that said nothing here would send him looking for a
		// pairing that never arrived.
		srv.note(ctx, wire.CodeUnreachable, reason.DB)
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	srv.note(ctx, "", reason.None)
	httpapi.NoContent(w)
}

// note writes one line of the log about a pairing. A log that does not answer
// never costs the pairing: the attempt is the event, and whoever looks for a
// hole does not need the line to be written, only the event to be missing.
// Log is nil in no program -- package server sets it -- and a nil one is
// stepped over all the same, so a test that builds a Server by hand does not
// crash on a path it is not measuring.
func (srv *Server) note(ctx context.Context, code wire.Code, why reason.Reason) {
	if srv.Log == nil {
		return
	}
	_ = srv.Log.Pairing(ctx, audit.ActionPair, code, why)
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
