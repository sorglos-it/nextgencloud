// The actions of ngc.users: the users of the trust list, the rights of the
// roles, the versions of the trust list itself, the one-time codes a new
// server joins with, and the log.
//
// Two sentences of this file are worth more than all its code.
//
// The first is about the one-time codes. A pairing code and a join code each
// carry a secret that is good for exactly one use, and the easiest place in
// the whole system to lose one is a log line (package audit says so itself).
// So nothing here ever hands a code, or anything derived from one, to the log,
// to a message or into a URL: pairCode and joinCode take the value, put it
// into the result and forget it. Neither writes an audit entry of its own --
// the channel is written down by the dispatcher, with app, action, node and
// result and no detail a handler fills -- and neither error path carries
// anything of the code: they answer a bare wire.Code.
// TestAOneTimeCodeReachesNoLog measures exactly that, over the code AND over
// its secret, with the control that the codes really were there.
//
// The second is about what is written down. The kinds of entry are closed
// (audit.kinds); a kind of this file's own would be refused with
// audit.ErrEntry and write NOTHING. So changes here go in through
// audit.Log.Note with core.LogTrust and core.LogRights, the kinds the design
// names for "changes to users, rights, apps, stores and licences", and the
// answer of Note is dropped on purpose: audit.ErrDropped means the entry was
// written with one field replaced, and whoever turned that report into a
// refusal would make a saved entry a lost one.
//
// What is noted is the RESOLVED value and never the claim of the payload: the
// seq of a new version is the one trust.Advance verified, read off the version
// package state returned, not the number the handed-in list writes about
// itself.
package coreapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/rights"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// registerUsers adds the actions of ngc.users.
func (d *Dispatcher) registerUsers() {
	d.Register(core.AppUsers, core.ServiceAPI, core.ActList, d.userList)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActRights, d.roleRights)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActSetRights, d.setRights)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActTrust, d.trust)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActPairCode, d.pairCode)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActJoinCode, d.joinCode)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActPending, d.pending)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActPendingAPI, d.pendingAPI)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActNodes, d.nodes)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActLog, d.log)
}

// userList answers ngc.users/list: the users of the trust list with what the
// database adds (TOTP, lock, last login). Who exists and which roles they
// hold comes out of the signed list and out of nothing else; the row only
// fills the three fields the list does not carry.
func (d *Dispatcher) userList(ctx context.Context, rq *Call) ([]byte, error) {
	v, err := rq.S.Trust(ctx)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	now := rq.S.Now()
	out := make([]core.UserInfo, 0, len(v.List.Users))
	for _, u := range v.List.Users {
		// A user of the list without a row is no fault: the row is made at
		// the first login, and until then the three fields below are simply
		// their zero values.
		row, err := rq.S.DB.UserGet(ctx, u.ID)
		if err != nil && !errors.Is(err, dbapi.ErrNotFound) {
			return nil, fail(wire.CodeUnreachable)
		}
		out = append(out, core.UserInfo{ID: u.ID, Name: u.Name, Roles: u.Roles,
			TOTP: len(row.TOTP) > 0, Locked: row.Locked > now, LastLogin: row.LastLogin})
	}
	return result(out)
}

// roleRights answers ngc.users/rights: every role of the trust list with the
// rights of its row, a role without a row with none.
func (d *Dispatcher) roleRights(ctx context.Context, rq *Call) ([]byte, error) {
	v, err := rq.S.Trust(ctx)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	all, err := rights.All(ctx, rq.S.DB, v.List)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	return result(all)
}

// setRights answers ngc.users/set-rights: it replaces the rights of one role.
//
// The role has to be one the trust list holds, and that is a refusal this
// handler adds rather than one the contract asks for. The reason is package
// rights: All shows only the roles of the list, and nothing in that package
// can delete a row, so a row written here for a role nobody has would stay
// invisible until somebody created a role of that name again -- and then grant
// what an admin granted long ago without meaning it. Only an admin reaches
// this at all (core.Action.Admin), so the refusal costs nothing but a typo.
func (d *Dispatcher) setRights(ctx context.Context, rq *Call) ([]byte, error) {
	p, ok := rq.Params.(*core.RoleRights)
	if !ok {
		return nil, fail(wire.CodeProtocol)
	}
	v, err := rq.S.Trust(ctx)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	if _, ok := v.List.Role(p.Role); !ok {
		return nil, fail(wire.CodeNotFound)
	}
	if err := rights.Set(ctx, rq.S.DB, *p); err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	// The detail is the role, a word of the program. The rights themselves do
	// not go in: a role may hold up to a thousand of them, far past
	// audit.maxDetail, and a detail over that length is dropped as a whole --
	// the entry would then say less than the role name does.
	_ = d.Log.Note(ctx, core.LogRights, rq.Live.Verified.Cert().User, "", "", p.Role)
	return result(struct{}{})
}

// trust answers ngc.users/api/trust: the shell sends one new version of the
// trust list, the API verifies it as the one after the newest and passes it on.
//
// Nothing of the handed-in bytes is believed before trust.ParseSigned read
// them and state.AddVersion verified the proof over them against the current
// list. What is answered and what is noted is the seq of the VERIFIED version,
// so a payload that writes a number about itself cannot put that number into
// the log.
func (d *Dispatcher) trust(ctx context.Context, rq *Call) ([]byte, error) {
	// The dispatcher already bounds a FlowIn channel at Action.MaxIn; this is
	// the same bound once more, so the handler does not depend on its caller
	// for the one limit that keeps a list out of memory.
	body, err := io.ReadAll(io.LimitReader(rq.In, core.MaxTrustIn+1))
	if err != nil || int64(len(body)) > core.MaxTrustIn {
		return nil, fail(wire.CodeTooLarge)
	}
	sg, err := trust.ParseSigned(body)
	if err != nil {
		return nil, fail(wire.CodeProtocol)
	}
	v, err := rq.S.AddVersion(ctx, sg)
	if err != nil {
		// One answer for every refusal of AddVersion -- a foreign signature, a
		// version that is not the next one, a fork, another installation. They
		// are told apart over HTTP (httpapi.Fault), not on a channel, and a
		// channel that named them would say which of the four a guess hit.
		return nil, fail(wire.CodeVerify)
	}
	seq := v.List.Seq
	_ = d.Log.Note(ctx, core.LogTrust, rq.Live.Verified.Cert().User, "", "",
		strconv.FormatUint(seq, 10))
	// The push outlives this channel on purpose: a new version has to reach
	// every controller and every open shell, and the admin's channel must not
	// wait for the slowest tunnel. WithoutCancel, because the channel's
	// context ends with the result.
	go d.pushTrust(context.WithoutCancel(ctx), seq)
	return result(core.TrustResult{Seq: seq})
}

// pairCode answers ngc.users/pair-code: a one-time code for the ulh script of
// a new controller. The code is a secret of one use; it travels in the result
// and nowhere else (the file comment).
func (d *Dispatcher) pairCode(ctx context.Context, rq *Call) ([]byte, error) {
	code, expires, err := d.Controllers.NewPairCode(ctx)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	return result(core.PairCodeResult{Code: code.String(), Expires: expires})
}

// joinCode answers ngc.users/join-code: the same for a new API server.
func (d *Dispatcher) joinCode(ctx context.Context, rq *Call) ([]byte, error) {
	code, expires, err := d.Join.NewJoinCode(ctx)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	return result(core.PairCodeResult{Code: code.String(), Expires: expires})
}

// pending answers ngc.users/pending: controllers that paired and wait to be
// listed. The fingerprint is not sent: the shell computes it from Key itself
// (shared/README.md, "Selbst rechnen"), and a fingerprint the API supplied
// would be the API's word for the key the admin is about to compare.
func (d *Dispatcher) pending(ctx context.Context, rq *Call) ([]byte, error) {
	list, err := rq.S.DB.PendingList(ctx)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	out := make([]core.Pending, 0, len(list))
	for _, p := range list {
		out = append(out, core.Pending{Name: p.Name, Key: enc.Bytes(p.Key), Since: p.Since})
	}
	return result(out)
}

// pendingAPI answers ngc.users/pending-api: API servers that sent a join
// request and wait to be listed (dbapi.BlobJoin).
//
// Every row goes through tunnel.ParseJoinRequest, although it was checked when
// it was stored: a row is outside input again once it has been in a database,
// and this is the contract's own parser for it. A row it refuses is left out
// instead of failing the whole list -- one broken row would otherwise keep the
// admin from listing any server at all.
//
// The MAC does not travel. It is the proof the API checked against the secret
// it issued; the admin compares tunnel.JoinFingerprint over the four values
// below, which the shell computes itself.
func (d *Dispatcher) pendingAPI(ctx context.Context, rq *Call) ([]byte, error) {
	rows, err := rq.S.DB.BlobFrom(ctx, dbapi.BlobJoin, 0)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	out := make([]core.PendingAPI, 0, len(rows))
	for _, b := range rows {
		r, err := tunnel.ParseJoinRequest(b.Data)
		if err != nil || r.Node != b.ID {
			continue
		}
		out = append(out, core.PendingAPI{Node: r.Node, Noise: enc.Bytes(r.Noise),
			Sign: enc.Bytes(r.Sign), TLS: enc.Bytes(r.TLS), URL: r.URL, Since: b.Updated})
	}
	return result(out)
}

// nodes answers ngc.users/nodes: one row per controller of the trust list, so
// an admin can pick the target of an activation and see what is up. API nodes
// are no row: nobody runs a task on one.
//
// Nothing here is signed beyond the node ID: Online is what this API sees of
// its own tunnels, and Roles, Version and Arch are what the controller last
// said in its hello. A controller refuses a task it cannot carry out, so a
// hello that lied can mislead an admin's choice and never widen a right.
func (d *Dispatcher) nodes(ctx context.Context, rq *Call) ([]byte, error) {
	v, err := rq.S.Trust(ctx)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	out := make([]core.NodeInfo, 0, len(v.List.Nodes))
	for _, n := range v.List.Nodes {
		if n.Kind != trust.NodeController {
			continue
		}
		row := core.NodeInfo{Node: n.ID}
		if t, ok := d.Tunnels.Get(n.ID); ok && t != nil {
			h := t.Hello()
			row.Online, row.Roles, row.Version, row.Arch = true, h.Roles, h.Version, h.Arch
		}
		out = append(out, row)
	}
	return result(out)
}

// log answers ngc.users/log. The query goes to audit.Log.Query, which checks
// it against the contract once more before a database sees it.
func (d *Dispatcher) log(ctx context.Context, rq *Call) ([]byte, error) {
	p, ok := rq.Params.(*core.LogQuery)
	if !ok {
		return nil, fail(wire.CodeProtocol)
	}
	entries, err := d.Log.Query(ctx, *p)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	return result(entries)
}

// pushTrust sends the new version to every connected controller
// (ngc.users/controller/trust, a channel without a user: the versions carry
// their own signatures) and tells every open shell that a newer number exists.
//
// It walks the controllers of the TRUST LIST and asks the registry for each
// one's tunnel, rather than walking the open tunnels: a tunnel of a node the
// list no longer holds gets nothing from here.
//
// How much of the chain travels comes out of the controller's own hello, which
// is unsigned -- and that is allowed because it can only pick HOW MANY signed
// versions are sent. Every version in the chain carries its proof and the
// controller verifies the chain from its own anchor, so a wrong number here
// makes a push useless and can never make a controller take a version it did
// not verify.
//
// Nothing in here may fail the round. A controller that is away, one that
// answers a code, one that is behind: each simply gets the chain again at its
// next hello (controllers.Server.OnHello). The answer is not read at all --
// what a controller has is what its next hello says it has.
func (d *Dispatcher) pushTrust(ctx context.Context, seq uint64) {
	v, err := d.S.Trust(ctx)
	if err == nil {
		for _, n := range v.List.Nodes {
			if n.Kind != trust.NodeController {
				continue
			}
			t, ok := d.Tunnels.Get(n.ID)
			if !ok || t == nil {
				continue
			}
			// Unmeasured, and the reason is here rather than dressed up as a
			// case: no test of this package can give a tunnel a hello
			// (controllers.Tunnel.setHello is unexported), so its TrustSeq is
			// always 0 here and a mutant that sends the chain from version 1
			// come what may stays ALIVE. What the number does is nothing but
			// save bytes; a controller that gets too much verifies it from its
			// own anchor and keeps the newest.
			chain, err := d.S.Chain(ctx, v, t.Hello().TrustSeq+1)
			if err != nil || len(chain.Versions) == 0 {
				continue
			}
			body, err := json.Marshal(chain)
			if err != nil {
				continue
			}
			_, _ = d.Relay.CallController(ctx, t, core.AppUsers, core.ServiceController,
				core.ActTrust, nil, body)
		}
	}
	// The shells are told even when no controller could be reached: a shell
	// that hears the number fetches the chain itself.
	d.Sessions.HintAll(ctx, seq)
}
