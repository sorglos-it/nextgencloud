package coreapp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/appstore"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/controllers"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/rights"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The non-admin of the cases below: a role "user" and a user "bob" that
// version 2 of the trust list carries.
const (
	userRole = "user"
	bobName  = "bob"
	bobFill  = 2
)

var bobID = trusttest.ID(0x22)

// --- what the cases need beside the shared fixture ------------------------

// fakeTunnels answers Get out of a map a case fills. It stands in for
// *controllers.Registry because a controller cannot be added to a registry
// from outside package controllers -- the reason the Tunnels interface exists
// at all (dispatch.go).
//
// What it CANNOT do is give a tunnel a hello: setHello is unexported, so no
// case of this package can put Roles, Version or Arch on one. The nodes
// handler copies those three out of Tunnel.Hello(), and that copying is
// therefore NOT measured here; what is measured is Online and which nodes
// appear at all.
type fakeTunnels struct {
	by map[string]*controllers.Tunnel
}

func (f *fakeTunnels) Get(node string) (*controllers.Tunnel, bool) {
	t, ok := f.by[node]
	return t, ok
}

// fakeJoin stands in for *join.Join, which is task 18. The code it hands out
// is a real tunnel.JoinCode, so a case can read it with the contract's own
// parser and then look for its secret in the log.
type fakeJoin struct {
	code    tunnel.JoinCode
	expires int64
	err     error
}

func (f *fakeJoin) NewJoinCode(ctx context.Context) (tunnel.JoinCode, int64, error) {
	return f.code, f.expires, f.err
}

// usersFx is the shared fixture with the three dependencies of ngc.users
// wired: the pair codes of the controller server, the tunnels a case fills
// itself, and a stand-in for the join codes of task 18.
type usersFx struct {
	*harness
	Tunnels *fakeTunnels
	Join    *fakeJoin
}

func usersFixture(t *testing.T) *usersFx {
	t.Helper()
	fx := &usersFx{harness: newFixture(t), Tunnels: &fakeTunnels{by: map[string]*controllers.Tunnel{}}}
	_, api1 := trusttest.APITLS(0xE1, "api-1.test")
	fx.Join = &fakeJoin{expires: testNow + tunnel.PairLifetime, code: tunnel.JoinCode{
		URL: fx.S.Cfg.URL, Key: enc.Bytes(bytes.Repeat([]byte{3}, 32)), TLS: enc.Bytes(api1),
		Secret: enc.Bytes(bytes.Repeat([]byte{9}, tunnel.SecretSize)),
		Anchor: trust.Anchor{Seq: 1, Hash: enc.Bytes(bytes.Repeat([]byte{1}, 32))},
	}}
	fx.D.Tunnels = fx.Tunnels
	fx.D.Controllers = &controllers.Server{S: fx.S, Tunnels: controllers.NewRegistry()}
	fx.D.Join = fx.Join
	return fx
}

// userCall opens a core channel of ngc.users and returns its result.
func (h *harness) userCall(t *testing.T, action string, params any) ([]byte, []uint64) {
	t.Helper()
	c := h.Open(t, usersOpen(t, action, params))
	return h.finish(t, c)
}

// usersOpen builds the OPEN of one action of ngc.users.
func usersOpen(t *testing.T, action string, params any) wire.Open {
	t.Helper()
	o := wire.Open{App: core.AppUsers, Service: core.ServiceAPI, Action: action, Window: wire.MinWindow}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		o.Params = b
	}
	return o
}

// finish reads one channel to its CLOSE and fails the case on anything but
// ok, keeping the hints that arrived while it ran.
//
// Control messages of channel 0 are kept apart on purpose: a trust push hints
// every open shell, and that frame can land between the DATA and the CLOSE of
// the very channel that caused it -- a reader that counted it as channel data
// would measure a corrupted result.
func (h *harness) finish(t *testing.T, c *chann) ([]byte, []uint64) {
	t.Helper()
	var body []byte
	var hints []uint64
	for {
		f := c.pull(t)
		if f.Channel == wire.ControlChannel {
			hints = append(hints, hintOf(t, f))
			continue
		}
		switch f.Type {
		case wire.TypeData:
			body = append(body, f.Payload...)
		case wire.TypeClose:
			cl, err := wire.ParseClose(f.Payload)
			if err != nil {
				t.Fatalf("close payload: %v", err)
			}
			if cl.Status != wire.StatusOK {
				t.Fatalf("close %+v", cl)
			}
			return body, hints
		}
	}
}

// refuse opens a channel and returns the code it closed with.
func (h *harness) refuse(t *testing.T, action string, params any) wire.Code {
	t.Helper()
	c := h.Open(t, usersOpen(t, action, params))
	for {
		f := c.pull(t)
		if f.Channel == wire.ControlChannel {
			continue
		}
		if f.Type == wire.TypeClose {
			cl, err := wire.ParseClose(f.Payload)
			if err != nil {
				t.Fatalf("close payload: %v", err)
			}
			return cl.Code
		}
	}
}

// sendTrust opens ngc.users/api/trust, sends one signed version and returns
// the result, the hints that arrived and the code the channel closed with --
// "" when it answered.
func (h *harness) sendTrust(t *testing.T, sg trust.Signed) ([]byte, []uint64, wire.Code) {
	t.Helper()
	body, err := json.Marshal(sg)
	if err != nil {
		t.Fatal(err)
	}
	c := h.Open(t, wire.Open{App: core.AppUsers, Service: core.ServiceAPI, Action: core.ActTrust,
		Window: wire.MinWindow})
	var out []byte
	var hints []uint64
	sent := false
	for {
		f := c.pull(t)
		if f.Channel == wire.ControlChannel {
			hints = append(hints, hintOf(t, f))
			continue
		}
		switch f.Type {
		case wire.TypeGrant:
			// The version is one frame: the fixture's window is larger than
			// any list these cases build.
			if !sent && c.credit >= uint64(len(body)) {
				sent = true
				c.credit -= uint64(len(body))
				h.peer.write(t, wire.Frame{Type: wire.TypeData, Channel: c.num, Payload: body})
				h.peer.write(t, wire.Frame{Type: wire.TypeEOF, Channel: c.num})
			}
		case wire.TypeData:
			out = append(out, f.Payload...)
		case wire.TypeClose:
			cl, err := wire.ParseClose(f.Payload)
			if err != nil {
				t.Fatalf("close payload: %v", err)
			}
			if cl.Status != wire.StatusOK {
				return nil, hints, cl.Code
			}
			return out, hints, ""
		}
	}
}

// hintOf reads the seq out of a "trust" control message.
func hintOf(t *testing.T, f wire.Frame) uint64 {
	t.Helper()
	c, err := wire.ParseControl(f.Payload)
	if err != nil {
		t.Fatalf("control: %v", err)
	}
	if c.T != wire.CtlTrust {
		t.Fatalf("control message %q on channel 0", c.T)
	}
	var h session.TrustHint
	if err := json.Unmarshal(c.B, &h); err != nil {
		t.Fatalf("hint: %v", err)
	}
	return h.Seq
}

// waitHint waits for the hint of a trust push on the session link.
func (h *harness) waitHint(t *testing.T) uint64 {
	t.Helper()
	deadline := time.Now().Add(readWait)
	for time.Now().Before(deadline) {
		if f := h.peer.read(t); f.Channel == wire.ControlChannel {
			return hintOf(t, f)
		}
	}
	t.Fatal("no trust hint reached the shell")
	return 0
}

// emptyResult returns what shared/testdata/core.json holds under "empty" for
// one result. It is read off the disk on purpose: the contract's own file is
// the reference, not a copy of it here.
func emptyResult(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../../../shared/testdata/core.json")
	if err != nil {
		t.Fatalf("testdata: %v", err)
	}
	var file struct {
		Empty map[string]json.RawMessage `json:"empty"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatalf("testdata: %v", err)
	}
	v, ok := file.Empty[name]
	if !ok {
		t.Fatalf("shared/testdata/core.json holds no empty value for %s", name)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, v); err != nil {
		t.Fatal(err)
	}
	return compact.Bytes()
}

// nextVersion builds the version after the newest, with the role "user" and
// the user bob in it, so the list carries somebody who is no admin. Called
// again on a list that already holds them it only raises the number, so a
// case can go on building versions.
func nextVersion(t *testing.T, fx *harness) *trust.List {
	t.Helper()
	v, err := fx.S.Trust(ctx)
	if err != nil {
		t.Fatalf("trust: %v", err)
	}
	l := *v.List
	l.Seq = v.List.Seq + 1
	l.Prev = enc.Bytes(v.Hash)
	l.Issued = testNow
	l.Roles = slices.Clone(l.Roles)
	l.Users = slices.Clone(l.Users)
	if _, ok := l.Role(userRole); !ok {
		l.Roles = append(l.Roles, trust.Role{Name: userRole, Level: trust.LevelOff})
		slices.SortFunc(l.Roles, func(a, b trust.Role) int { return strings.Compare(a.Name, b.Name) })
	}
	if _, ok := l.User(bobID); !ok {
		l.Users = append(l.Users, trust.User{ID: bobID, Name: bobName,
			Roles: []string{userRole}, Keys: []proof.UserKey{trusttest.NewSigner(bobFill).Key}})
		slices.SortFunc(l.Users, func(a, b trust.User) int { return strings.Compare(a.ID, b.ID) })
	}
	if err := l.Validate(); err != nil {
		t.Fatalf("the version this case builds is no trust list: %v", err)
	}
	return &l
}

// nonAdmin puts version 2 with bob into the installation and returns a
// session of bob's own.
func nonAdmin(t *testing.T, fx *usersFx) *usersFx {
	t.Helper()
	l := nextVersion(t, fx.harness)
	if _, err := fx.S.AddVersion(ctx, fx.Admin.Sign(t, l)); err != nil {
		t.Fatalf("version 2: %v", err)
	}
	if err := fx.S.DB.UserPut(ctx, dbapi.User{ID: bobID, Name: bobName, Roles: []string{userRole}}); err != nil {
		t.Fatalf("user bob: %v", err)
	}
	return &usersFx{harness: fx.As(t, trusttest.NewSigner(bobFill), bobID, bobName),
		Tunnels: fx.Tunnels, Join: fx.Join}
}

// --- list and rights ------------------------------------------------------

// TestListAndRights holds ngc.users/list and /rights: the users come out of
// the trust list, what the database adds comes out of the row, and every role
// of the list appears in the rights -- one without a row with no rights at
// all, which the result leaves out rather than writing null.
func TestListAndRights(t *testing.T) {
	fx := usersFixture(t)
	row, err := fx.S.DB.UserGet(ctx, fx.AdminID)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	row.TOTP, row.Locked, row.LastLogin = []byte("sealed"), testNow+60, testNow-5
	if err := fx.S.DB.UserPut(ctx, row); err != nil {
		t.Fatalf("user: %v", err)
	}
	body, _ := fx.userCall(t, core.ActList, nil)
	var users []core.UserInfo
	if err := json.Unmarshal(body, &users); err != nil {
		t.Fatalf("list %s: %v", body, err)
	}
	if len(users) != len(fx.List.Users) {
		t.Fatalf("%d rows for %d users of the list: %s", len(users), len(fx.List.Users), body)
	}
	var me core.UserInfo
	for _, u := range users {
		if u.ID == fx.AdminID {
			me = u
		}
	}
	if me.Name != fx.AdminName || !slices.Equal(me.Roles, []string{trust.RoleAdmin}) {
		t.Fatalf("the admin's row is %+v", me)
	}
	if !me.TOTP || !me.Locked || me.LastLogin != testNow-5 {
		t.Fatalf("the row of the database did not reach the result: %+v", me)
	}
	// The counter-control of the three fields above: the other user's row
	// carries no secret, no login -- and a lock whose time has PASSED, which
	// is not a lock. Without that last value a handler that asked whether the
	// field is set at all instead of comparing it with now would pass.
	other := ""
	for _, u := range fx.List.Users {
		if u.ID != fx.AdminID {
			other = u.ID
		}
	}
	if err := fx.S.DB.UserPut(ctx, dbapi.User{ID: other, Name: "gone-" + bobName,
		Roles: []string{trust.RoleAdmin}, Locked: testNow - 1}); err != nil {
		t.Fatalf("other user: %v", err)
	}
	body, _ = fx.userCall(t, core.ActList, nil)
	if err := json.Unmarshal(body, &users); err != nil {
		t.Fatalf("list %s: %v", body, err)
	}
	for _, u := range users {
		if u.ID != fx.AdminID && (u.TOTP || u.Locked || u.LastLogin != 0) {
			t.Fatalf("a user without those values carries %+v", u)
		}
	}
	body, _ = fx.userCall(t, core.ActRights, nil)
	if string(body) != `[{"role":"admin"}]` {
		t.Fatalf("rights %s", body)
	}
	fx.Grant(t, appY+"/"+core.RightUse)
	body, _ = fx.userCall(t, core.ActRights, nil)
	if string(body) != `[{"role":"admin","rights":["`+appY+`/use"]}]` {
		t.Fatalf("rights after a grant: %s", body)
	}
}

// TestEmptyListsAreArrays holds the promise of the "empty" section of
// shared/testdata/core.json for the two results of ngc.users that really are
// empty in a fresh installation: [] and never null.
func TestEmptyListsAreArrays(t *testing.T) {
	fx := usersFixture(t)
	for _, c := range []struct{ action, name string }{
		{core.ActPending, "ngc.users/pending"},
		{core.ActPendingAPI, "ngc.users/pending-api"},
	} {
		body, _ := fx.userCall(t, c.action, nil)
		if want := emptyResult(t, c.name); !bytes.Equal(body, want) {
			t.Fatalf("%s answered %s, the contract says %s", c.action, body, want)
		}
	}
}

// TestSetRightsWritesAndLogs holds that set-rights writes the row and notes it
// as core.LogRights. The entry goes in through audit.Log.Note, the one entry
// point for changes to users, rights, apps, stores and licences; a kind of its
// own would be refused with audit.ErrEntry and write nothing.
func TestSetRightsWritesAndLogs(t *testing.T) {
	fx := usersFixture(t)
	// The control of the case: before the call there is no such entry, so the
	// one found afterwards is this call's.
	if _, ok := noteOf(fx.logEntries(t), core.LogRights); ok {
		t.Fatal("the fixture already holds a rights entry")
	}
	want := []string{appY + "/" + appAct, appY + "/" + core.RightUse}
	body, _ := fx.userCall(t, core.ActSetRights, core.RoleRights{Role: trust.RoleAdmin, Rights: want})
	if string(body) != `{}` {
		t.Fatalf("set-rights answered %s", body)
	}
	v, err := fx.S.Trust(ctx)
	if err != nil {
		t.Fatalf("trust: %v", err)
	}
	got, admin, err := rights.Of(ctx, fx.S.DB, v.List, fx.AdminID)
	if err != nil {
		t.Fatalf("rights: %v", err)
	}
	if !admin {
		t.Fatal("the admin lost the role")
	}
	for _, r := range want {
		if !got[r] {
			t.Fatalf("the row holds %v, not %q", got, r)
		}
	}
	e, ok := noteOf(fx.logEntries(t), core.LogRights)
	if !ok {
		t.Fatalf("no rights entry: %+v", fx.logEntries(t))
	}
	if e.User != fx.AdminID || e.Detail != trust.RoleAdmin {
		t.Fatalf("the entry is %+v", e)
	}
}

// TestSetRightsRefusesARoleTheListDoesNotHold holds the one refusal this
// handler adds to the contract's own checks: a row for a role nobody has
// grants nothing, rights.All leaves it out and nothing in package rights can
// delete it again, so it is not written in the first place.
func TestSetRightsRefusesARoleTheListDoesNotHold(t *testing.T) {
	fx := usersFixture(t)
	// The open-way check: the role the list does hold goes through, so the
	// refusal below is about the role and no wall in front of it.
	fx.userCall(t, core.ActSetRights, core.RoleRights{Role: trust.RoleAdmin})
	if code := fx.refuse(t, core.ActSetRights, core.RoleRights{Role: userRole}); code != wire.CodeNotFound {
		t.Fatalf("close code %q", code)
	}
	if _, err := fx.S.DB.BlobGet(ctx, dbapi.BlobRights, userRole); err == nil {
		t.Fatal("a row was written for a role the trust list does not hold")
	}
}

// TestSetRightsRefusesAUseRightTheLicenceHasNoSeatFor: the seat of a per-user
// licence. Before this the count of appstore.UsersAllowed limited nobody --
// the function had no caller at all -- and License.Users was a number in a
// database and nothing else.
//
// Three answers in one case, because the open side matters as much as the
// closed one: a free app (no licence on record) goes through, a licence
// without a free seat is refused AND the row is put back, and a licence this
// API cannot judge is refused as well.
func TestSetRightsRefusesAUseRightTheLicenceHasNoSeatFor(t *testing.T) {
	fx := usersFixture(t)
	st := &fakeStore{seats: map[string]seatAnswer{}}
	fx.D.Store = st
	use := appY + "/" + core.RightUse
	free := []string{appY + "/" + appAct}

	// The way is open: a right that is not a "use" right asks no licence at
	// all, so everything below is about the seat and not about a wall in
	// front of it.
	fx.userCall(t, core.ActSetRights, core.RoleRights{Role: trust.RoleAdmin, Rights: free})
	if len(st.seatsAsked) != 0 {
		t.Fatalf("a right that is not %q asked for a seat: %v", core.RightUse, st.seatsAsked)
	}

	// A free app: no licence on record, so nothing limits it.
	fx.userCall(t, core.ActSetRights, core.RoleRights{Role: trust.RoleAdmin, Rights: []string{use}})
	if len(st.seatsAsked) != 1 || st.seatsAsked[0] != appY {
		t.Fatalf("the seat of %q was not asked: %v", appY, st.seatsAsked)
	}
	if !rightsHold(t, fx, use) {
		t.Fatal("the free app lost its use right")
	}

	// Back to the state before, so the refusal below has something to put back.
	fx.userCall(t, core.ActSetRights, core.RoleRights{Role: trust.RoleAdmin, Rights: free})

	// A per-user licence with no free seat.
	st.seats[appY] = seatAnswer{n: 4, allowed: false}
	if code := fx.refuse(t, core.ActSetRights,
		core.RoleRights{Role: trust.RoleAdmin, Rights: []string{use}}); code != wire.CodeLicense {
		t.Fatalf("close code %q, want %q", code, wire.CodeLicense)
	}
	if rightsHold(t, fx, use) {
		t.Fatal("the refused right stands in the database")
	}
	// And what stood before is back, not an empty row.
	if !rightsHold(t, fx, free[0]) {
		t.Fatalf("%q was lost when the refused change was put back", free[0])
	}

	// A licence this API cannot judge is not a free seat either.
	st.seats[appY] = seatAnswer{err: appstore.ErrLicense}
	if code := fx.refuse(t, core.ActSetRights,
		core.RoleRights{Role: trust.RoleAdmin, Rights: []string{use}}); code != wire.CodeUnreachable {
		t.Fatalf("close code %q, want %q", code, wire.CodeUnreachable)
	}

	// And with a free seat the same call goes through, so the two refusals
	// above are the answers of the licence and not the shape of the call.
	st.seats[appY] = seatAnswer{n: 1, allowed: true}
	fx.userCall(t, core.ActSetRights, core.RoleRights{Role: trust.RoleAdmin, Rights: []string{use}})
	if !rightsHold(t, fx, use) {
		t.Fatal("the allowed right did not stand")
	}
}

// rightsHold reports whether the admin of the fixture holds right.
func rightsHold(t *testing.T, fx *usersFx, right string) bool {
	t.Helper()
	v, err := fx.S.Trust(ctx)
	if err != nil {
		t.Fatalf("trust: %v", err)
	}
	got, _, err := rights.Of(ctx, fx.S.DB, v.List, fx.AdminID)
	if err != nil {
		t.Fatalf("rights: %v", err)
	}
	return got[right]
}

// --- the trust list -------------------------------------------------------

// TestTrustTakesTheNextVersionOnly holds that ngc.users/api/trust takes
// exactly the version after the newest: the next one is stored and answered
// with its seq, the same one again is refused. What is noted is the RESOLVED
// number -- the one trust.Advance verified -- and not what the payload claims.
func TestTrustTakesTheNextVersionOnly(t *testing.T) {
	fx := usersFixture(t)
	l := nextVersion(t, fx.harness)
	sg := fx.Admin.Sign(t, l)
	body, _, code := fx.sendTrust(t, sg)
	if code != "" {
		t.Fatalf("the next version was refused with %q", code)
	}
	var res core.TrustResult
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("result %s: %v", body, err)
	}
	if res.Seq != l.Seq {
		t.Fatalf("answered seq %d, the version is %d", res.Seq, l.Seq)
	}
	v, err := fx.S.Trust(ctx)
	if err != nil || v.List.Seq != l.Seq {
		t.Fatalf("the installation stands at %+v (%v)", v.List, err)
	}
	// The same version a second time: it is no longer the next one.
	if _, _, code := fx.sendTrust(t, sg); code != wire.CodeVerify {
		t.Fatalf("the version again closed with %q", code)
	}
	e, ok := noteOf(fx.logEntries(t), core.LogTrust)
	if !ok {
		t.Fatalf("no trust entry: %+v", fx.logEntries(t))
	}
	if e.User != fx.AdminID || e.Detail != "2" {
		t.Fatalf("the entry is %+v", e)
	}
}

// TestTrustRefusesAForeignSignature holds that a version signed by a key the
// list does not carry is refused. The open-way check stands in the same case:
// the very same list signed by the admin goes through afterwards, so what is
// measured is the signature and not the version's shape.
func TestTrustRefusesAForeignSignature(t *testing.T) {
	fx := usersFixture(t)
	l := nextVersion(t, fx.harness)
	stranger := trusttest.NewSigner(0x5A)
	if _, _, code := fx.sendTrust(t, stranger.Sign(t, l)); code != wire.CodeVerify {
		t.Fatalf("a foreign signature closed with %q", code)
	}
	if v, err := fx.S.Trust(ctx); err != nil || v.List.Seq != 1 {
		t.Fatalf("the installation moved to %+v (%v)", v.List, err)
	}
	if _, _, code := fx.sendTrust(t, fx.Admin.Sign(t, l)); code != "" {
		t.Fatalf("the admin's signature over the same list closed with %q", code)
	}
}

// TestTrustPushesTheChainAndHintsTheShells holds what pushTrust does: every
// controller of the trust list that has a tunnel gets the chain on
// ngc.users/controller/trust, and every open shell is told the new number.
func TestTrustPushesTheChainAndHintsTheShells(t *testing.T) {
	fx := usersFixture(t)
	fx.Tunnels.by[ctlNode] = &controllers.Tunnel{Node: ctlNode}
	l := nextVersion(t, fx.harness)
	_, hints, code := fx.sendTrust(t, fx.Admin.Sign(t, l))
	if code != "" {
		t.Fatalf("the next version was refused with %q", code)
	}
	p := fx.Relay.called(t)
	if p.Node != ctlNode || p.App != core.AppUsers || p.Service != core.ServiceController ||
		p.Action != core.ActTrust {
		t.Fatalf("the push went to %+v", p)
	}
	chain, err := session.ParseTrustChain(p.In)
	if err != nil {
		t.Fatalf("the push is no trust chain: %v", err)
	}
	// The tunnel's hello says nothing, so the chain starts at version 1 and
	// carries both versions: a controller that has none can verify it.
	if len(chain.Versions) != 2 {
		t.Fatalf("the chain carries %d versions", len(chain.Versions))
	}
	if len(hints) == 0 {
		hints = append(hints, fx.waitHint(t))
	}
	if hints[0] != l.Seq {
		t.Fatalf("the shell was hinted %d, the version is %d", hints[0], l.Seq)
	}
}

// TestNoPushWithoutATunnel is the counter-control of the case above: the same
// version with no tunnel open reaches no controller, and the shells are told
// all the same.
func TestNoPushWithoutATunnel(t *testing.T) {
	fx := usersFixture(t)
	l := nextVersion(t, fx.harness)
	_, hints, code := fx.sendTrust(t, fx.Admin.Sign(t, l))
	if code != "" {
		t.Fatalf("the next version was refused with %q", code)
	}
	if len(hints) == 0 {
		hints = append(hints, fx.waitHint(t))
	}
	if hints[0] != l.Seq {
		t.Fatalf("the shell was hinted %d, the version is %d", hints[0], l.Seq)
	}
	if p, ok := fx.Relay.maybeCalled(); ok {
		t.Fatalf("a controller was called although no tunnel is open: %+v", p)
	}
}

// --- the one-time codes ---------------------------------------------------

// TestPairCodeFitsTheTrustList holds that pair-code and join-code answer a
// code the shell can read with the contract's own parser, and that the
// pairing code pins the version the API verified itself.
func TestPairCodeFitsTheTrustList(t *testing.T) {
	fx := usersFixture(t)
	v, err := fx.S.Trust(ctx)
	if err != nil {
		t.Fatalf("trust: %v", err)
	}
	body, _ := fx.userCall(t, core.ActPairCode, nil)
	var res core.PairCodeResult
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("pair-code %s: %v", body, err)
	}
	code, err := tunnel.ParsePairCode(res.Code)
	if err != nil {
		t.Fatalf("the code is none: %v", err)
	}
	if code.Anchor.Seq != v.List.Seq || !bytes.Equal(code.Anchor.Hash, v.Hash) {
		t.Fatalf("the code pins %+v, the API stands at %d", code.Anchor, v.List.Seq)
	}
	if res.Expires <= testNow {
		t.Fatalf("the code expires at %d, now is %d", res.Expires, testNow)
	}
	body, _ = fx.userCall(t, core.ActJoinCode, nil)
	var join core.PairCodeResult
	if err := json.Unmarshal(body, &join); err != nil {
		t.Fatalf("join-code %s: %v", body, err)
	}
	if _, err := tunnel.ParseJoinCode(join.Code); err != nil {
		t.Fatalf("the join code is none: %v", err)
	}
	if join.Expires != fx.Join.expires {
		t.Fatalf("join-code expires at %d, the join codes say %d", join.Expires, fx.Join.expires)
	}
}

// TestAOneTimeCodeReachesNoLog is the promise of this task that is easiest to
// break: a one-time code must stand in no log entry, in no message and in no
// URL. The handlers write no entry of their own for a code, so the only entry
// left is the channel entry, which carries app, action, node and result and no
// detail a handler fills.
//
// The control of the case is the first check: the codes really are there and
// really are long, so "not in the log" is not true of an empty string.
func TestAOneTimeCodeReachesNoLog(t *testing.T) {
	fx := usersFixture(t)
	var secret []string
	for _, action := range []string{core.ActPairCode, core.ActJoinCode} {
		body, _ := fx.userCall(t, action, nil)
		var res core.PairCodeResult
		if err := json.Unmarshal(body, &res); err != nil {
			t.Fatalf("%s %s: %v", action, body, err)
		}
		if len(res.Code) < 32 {
			t.Fatalf("%s answered %q, which is no code", action, res.Code)
		}
		secret = append(secret, res.Code)
		if action == core.ActPairCode {
			c, err := tunnel.ParsePairCode(res.Code)
			if err != nil {
				t.Fatalf("pair code: %v", err)
			}
			secret = append(secret, enc.B64.EncodeToString(c.Secret))
		} else {
			c, err := tunnel.ParseJoinCode(res.Code)
			if err != nil {
				t.Fatalf("join code: %v", err)
			}
			secret = append(secret, enc.B64.EncodeToString(c.Secret))
		}
	}
	entries := fx.logEntries(t)
	if len(entries) == 0 {
		t.Fatal("the two channels wrote no log entry at all, so this case measures nothing")
	}
	flat, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range secret {
		if bytes.Contains(flat, []byte(s)) {
			t.Fatalf("a one-time code stands in the log: %s", flat)
		}
	}
	// And the stronger statement, because the one above alone is not enough:
	// a detail longer than audit.maxDetail is replaced by the placeholder, so
	// a handler that noted the WHOLE code would leave no trace of it here and
	// the search above would still be green (measured: that mutant stayed
	// alive). What holds it is that these two actions write no entry of their
	// own at all -- only the dispatcher's channel entry, which fills no
	// detail.
	for _, e := range entries {
		if e.Kind != core.LogChannel {
			t.Fatalf("a code action wrote an entry of its own: %+v", e)
		}
		if e.Detail != "" {
			t.Fatalf("the channel entry of a code action carries a detail: %+v", e)
		}
	}
}

// --- pending, nodes, log --------------------------------------------------

// TestPendingShowsWaitingControllers holds ngc.users/pending: the rows of the
// database with the Noise key, and no fingerprint -- the shell computes that
// itself (shared/README.md, "Selbst rechnen").
func TestPendingShowsWaitingControllers(t *testing.T) {
	fx := usersFixture(t)
	key := bytes.Repeat([]byte{0xC1}, 32)
	if err := fx.S.DB.PendingPut(ctx, dbapi.Pending{Name: "dienste", Key: key, Since: testNow - 3}); err != nil {
		t.Fatalf("pending: %v", err)
	}
	body, _ := fx.userCall(t, core.ActPending, nil)
	var list []core.Pending
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("pending %s: %v", body, err)
	}
	if len(list) != 1 || list[0].Name != "dienste" || !bytes.Equal(list[0].Key, key) ||
		list[0].Since != testNow-3 {
		t.Fatalf("pending %s", body)
	}
	if bytes.Contains(bytes.ToLower(body), []byte("fingerprint")) {
		t.Fatalf("the result carries a fingerprint: %s", body)
	}
}

// TestPendingAPIShowsWaitingServers holds ngc.users/pending-api: the join
// requests of dbapi.BlobJoin, read with the contract's parser, without the
// MAC -- that is the proof the API checked and nothing the shell may see.
func TestPendingAPIShowsWaitingServers(t *testing.T) {
	fx := usersFixture(t)
	req := tunnel.JoinRequest{Node: "api-2", Noise: bytes.Repeat([]byte{1}, 32),
		Sign: bytes.Repeat([]byte{2}, 32), TLS: bytes.Repeat([]byte{3}, 32),
		URL: "wss://api-2.test:7443/tunnel", MAC: bytes.Repeat([]byte{4}, 32)}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := fx.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobJoin, ID: req.Node, Data: raw,
		Updated: testNow - 7}); err != nil {
		t.Fatalf("join row: %v", err)
	}
	body, _ := fx.userCall(t, core.ActPendingAPI, nil)
	var list []core.PendingAPI
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("pending-api %s: %v", body, err)
	}
	if len(list) != 1 || list[0].Node != "api-2" || list[0].URL != req.URL ||
		!bytes.Equal(list[0].Noise, req.Noise) || !bytes.Equal(list[0].Sign, req.Sign) ||
		!bytes.Equal(list[0].TLS, req.TLS) || list[0].Since != testNow-7 {
		t.Fatalf("pending-api %s", body)
	}
	if bytes.Contains(bytes.ToLower(body), []byte(`"mac"`)) {
		t.Fatalf("the result carries the MAC: %s", body)
	}
	// A row the contract's parser refuses is left out rather than handed on
	// as something the shell must trust.
	if err := fx.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobJoin, ID: "api-3",
		Data: []byte(`{"node":"api-3"}`), Updated: testNow}); err != nil {
		t.Fatalf("broken row: %v", err)
	}
	body, _ = fx.userCall(t, core.ActPendingAPI, nil)
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("pending-api %s: %v", body, err)
	}
	if len(list) != 1 {
		t.Fatalf("a broken row reached the shell: %s", body)
	}
}

// TestNodesShowsOnlineAndVersion holds ngc.users/nodes: one row per
// CONTROLLER of the trust list -- the API node of the list is none -- and
// Online out of the tunnels this API has right now.
//
// Roles, Version and Arch are not measured here: they come out of
// controllers.Tunnel.Hello(), and no case of this package can set a hello
// (fakeTunnels says why).
func TestNodesShowsOnlineAndVersion(t *testing.T) {
	fx := usersFixture(t)
	if len(fx.List.Nodes) < 2 {
		t.Fatalf("the fixture's list holds %d nodes, so this case cannot tell kinds apart",
			len(fx.List.Nodes))
	}
	body, _ := fx.userCall(t, core.ActNodes, nil)
	var list []core.NodeInfo
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("nodes %s: %v", body, err)
	}
	if len(list) != 1 || list[0].Node != ctlNode {
		t.Fatalf("nodes %s", body)
	}
	if list[0].Online {
		t.Fatalf("a controller without a tunnel is online: %s", body)
	}
	fx.Tunnels.by[ctlNode] = &controllers.Tunnel{Node: ctlNode}
	body, _ = fx.userCall(t, core.ActNodes, nil)
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("nodes %s: %v", body, err)
	}
	if len(list) != 1 || !list[0].Online {
		t.Fatalf("a controller with a tunnel is not online: %s", body)
	}
}

// TestLogPages holds ngc.users/log: the newest entries first, at most Limit of
// them, and the next page starting after the last entry of the one before.
func TestLogPages(t *testing.T) {
	fx := usersFixture(t)
	for i := 0; i < 5; i++ {
		if err := fx.Log.Note(ctx, core.LogRights, fx.AdminID, "", "", trust.RoleAdmin); err != nil {
			t.Fatalf("note: %v", err)
		}
	}
	all := fx.logEntries(t)
	if len(all) < 5 {
		t.Fatalf("the fixture holds %d entries", len(all))
	}
	body, _ := fx.userCall(t, core.ActLog, core.LogQuery{Limit: 2})
	var page []core.LogEntry
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("log %s: %v", body, err)
	}
	if len(page) != 2 || page[0].ID != all[0].ID || page[1].ID != all[1].ID {
		t.Fatalf("the first page is %s", body)
	}
	body, _ = fx.userCall(t, core.ActLog, core.LogQuery{Limit: 2, BeforeTime: page[1].Time,
		BeforeID: page[1].ID})
	var second []core.LogEntry
	if err := json.Unmarshal(body, &second); err != nil {
		t.Fatalf("log %s: %v", body, err)
	}
	// The second page is held by the rule of the cursor and not by a list of
	// IDs: every channel of this case writes its own entry at the same second,
	// with a random ID, so a page named by position would measure that chance
	// and not the paging (it did, on the first run).
	if len(second) != 2 {
		t.Fatalf("the second page is %s", body)
	}
	for _, e := range append(append([]core.LogEntry{}, page...), second...) {
		if e.Time > page[0].Time || (e.Time == page[0].Time && e.ID > page[0].ID) {
			t.Fatalf("entry %s stands before the newest of the first page: %s", e.ID, body)
		}
	}
	for _, e := range second {
		if e.Time > page[1].Time || (e.Time == page[1].Time && e.ID >= page[1].ID) {
			t.Fatalf("the second page holds %s, which is not after the cursor", e.ID)
		}
	}
	if second[0].Time < second[1].Time || (second[0].Time == second[1].Time && second[0].ID <= second[1].ID) {
		t.Fatalf("the second page is not newest first: %s", body)
	}
	// A user nobody logged under gives an empty page, and it is [] and not
	// null (shared/testdata/core.json, "empty").
	body, _ = fx.userCall(t, core.ActLog, core.LogQuery{Limit: 2, User: bobID})
	if want := emptyResult(t, "ngc.users/log"); !bytes.Equal(body, want) {
		t.Fatalf("the log of an unknown user is %s", body)
	}
}

// --- the admin wall -------------------------------------------------------

// TestOnlyAdminsGetThese holds that every action of ngc.users is refused for a
// user who is no admin, with the one code that tells them nothing: E_FORBIDDEN.
//
// The open-way check of each case stands beside it: the same action on the
// admin's session of the same API is served, so what is measured is the role
// and not a wall in front of it.
func TestOnlyAdminsGetThese(t *testing.T) {
	fx := usersFixture(t)
	bob := nonAdmin(t, fx)
	cases := []struct {
		action string
		params any
	}{
		{core.ActList, nil},
		{core.ActRights, nil},
		{core.ActSetRights, core.RoleRights{Role: trust.RoleAdmin}},
		{core.ActPairCode, nil},
		{core.ActJoinCode, nil},
		{core.ActPending, nil},
		{core.ActPendingAPI, nil},
		{core.ActNodes, nil},
		{core.ActLog, core.LogQuery{Limit: 10}},
	}
	for _, c := range cases {
		t.Run(c.action, func(t *testing.T) {
			fx.userCall(t, c.action, c.params) // the way is open for an admin
			if code := bob.refuse(t, c.action, c.params); code != wire.CodeForbidden {
				t.Fatalf("%s closed with %q for a user who is no admin", c.action, code)
			}
		})
	}
	// trust is the tenth action; it carries its version as data instead of
	// parameters, and the admin's open way for it is measured in
	// TestTrustTakesTheNextVersionOnly.
	l := nextVersion(t, fx.harness)
	if _, _, code := bob.sendTrust(t, fx.Admin.Sign(t, l)); code != wire.CodeForbidden {
		t.Fatalf("trust closed with %q for a user who is no admin", code)
	}
	if v, err := fx.S.Trust(ctx); err != nil || v.List.Seq != 2 {
		t.Fatalf("the installation stands at %+v (%v)", v.List, err)
	}
}

// noteOf finds the newest entry of a kind.
func noteOf(entries []core.LogEntry, kind string) (core.LogEntry, bool) {
	for _, e := range entries {
		if e.Kind == kind {
			return e, true
		}
	}
	return core.LogEntry{}, false
}
