package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/install"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/link"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// fakeInstaller records the one call and what it managed to read, and answers
// the error a case gave it.
type fakeInstaller struct {
	called bool
	// list is the trust list the server handed over, kept so a case can see
	// WHICH version an activation ran against.
	list *trust.List
	read []byte
	// limit, where it is above zero, is how much this installer reads before
	// it answers -- which is how a case measures that the rest is drained.
	limit int
	err   error
}

func (f *fakeInstaller) Activate(_ context.Context, list *trust.List, _ access.Decision,
	r io.Reader) ([]byte, error) {
	f.list = list
	f.called = true
	var err error
	if f.limit > 0 {
		f.read = make([]byte, f.limit)
		var n int
		n, err = io.ReadFull(r, f.read)
		f.read = f.read[:n]
	} else {
		f.read, err = io.ReadAll(r)
	}
	if f.err != nil {
		return nil, f.err
	}
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	return []byte("{}"), nil
}

// activateAction is the action of the contract, so a decision is built from
// the row and not from three strings written here.
func activateAction(t *testing.T) core.Action {
	t.Helper()
	a, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActActivate)
	if !ok {
		t.Fatal("the contract has no activate action")
	}
	return a
}

func activateDecision(t *testing.T) access.Decision {
	t.Helper()
	return access.Decision{Core: true, Action: activateAction(t)}
}

// THE ACTION IS SERVED AT ALL, and that is the assurance this whole file
// hangs from. Until 18.10.2026 ngc.store/controller/activate stood in neither
// map of this server: Server.core looked it up, found nothing and answered
// E_INTERNAL, so install.Manager.Activate was never called by anything.
// Measured on a running installation that day -- a package was uploaded, the
// API found it and sent it, and the channel ended "Interner Fehler." A probe
// that logged every one of the nine refusals inside install saw not one line.
func TestActivateIsServed(t *testing.T) {
	// WITH an installer, because that is what a built server has: a server
	// without one answers E_INTERNAL, and the map says so (New).
	s := New(Deps{Installer: &fakeInstaller{}})
	if _, ok := s.flows[activateKey]; !ok {
		t.Fatal("no handler for ngc.store/controller/activate -- nothing can activate an app")
	}
	if !s.Serves(core.AppStore, core.ServiceController, core.ActActivate) {
		t.Fatal("Serves says this build does not activate, while the map holds it")
	}
	// And a server wired WITHOUT one does not claim to: the question is
	// asked by whoever wires it, and a yes that is not true is worse than
	// no answer.
	if New(Deps{}).Serves(core.AppStore, core.ServiceController, core.ActActivate) {
		t.Fatal("a server without an installer claims to serve activations")
	}
	a := activateAction(t)
	if activateKey != (name{a.App, a.Service, a.Name}) {
		t.Fatalf("the key %+v is not the action of the contract %q/%q/%q",
			activateKey, a.App, a.Service, a.Name)
	}
	// It carries a file, and that is why it is in flows and not in acts: an
	// action of acts is handed its whole input as bytes.
	if a.Flow != core.FlowTransfer {
		t.Fatalf("activate is %q, and this file is built for a flow that carries a file", a.Flow)
	}
}

// NO ACTION IS SERVED TWICE. The two maps are read one after the other, so an
// action in both would be served by whichever is read first -- a decision
// nobody wrote down.
func TestNoActionIsServedTwice(t *testing.T) {
	s := New(Deps{Installer: &fakeInstaller{}})
	for k := range s.flows {
		if _, both := s.acts[k]; both {
			t.Errorf("%+v stands in acts and in flows", k)
		}
	}
	if len(s.flows) == 0 || len(s.acts) == 0 {
		t.Fatalf("acts %d, flows %d -- a comparison of empty maps says nothing",
			len(s.acts), len(s.flows))
	}
}

// The installer is reached, it gets the package, and the channel ends well.
func TestActivateReachesTheInstallerWithThePackage(t *testing.T) {
	f := &fakeInstaller{}
	s := New(Deps{Installer: f})
	pkg := bytes.Repeat([]byte{7}, 4096)
	v, cl := s.activate(context.Background(), nil, activateDecision(t), bytes.NewReader(pkg))
	if cl != nil {
		t.Fatalf("the channel ended %+v, want a result", cl)
	}
	if _, ok := v.(emptyResult); !ok {
		t.Fatalf("the result is %T, want the empty object every core action answers", v)
	}
	if !f.called {
		t.Fatal("the installer was not called")
	}
	if !bytes.Equal(f.read, pkg) {
		t.Fatalf("the installer read %d bytes of %d, and they are %v the package",
			len(f.read), len(pkg), bytes.Equal(f.read, pkg))
	}
}

// A controller without an installer is this program wired wrong and says so
// with the code of its own faults -- the same code the channel got while the
// action had no handler at all, so a build without one is no new state for an
// API.
func TestActivateWithoutAnInstallerIsThisServersFault(t *testing.T) {
	s := New(Deps{})
	_, cl := s.activate(context.Background(), nil, activateDecision(t), bytes.NewReader(nil))
	if cl == nil || cl.Code != wire.CodeInternal {
		t.Fatalf("the channel ended %+v, want %s", cl, wire.CodeInternal)
	}
}

// A decision for another action never reaches the installer: access matched
// the OPEN against core.All already, so one that names something else here is
// this program wired wrong.
func TestActivateRefusesADecisionForAnotherAction(t *testing.T) {
	f := &fakeInstaller{}
	s := New(Deps{Installer: f})
	other, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActDeactivate)
	if !ok {
		t.Fatal("the contract has no deactivate action")
	}
	d := activateDecision(t)
	d.Action = other
	_, cl := s.activate(context.Background(), nil, d, bytes.NewReader(nil))
	if cl == nil || cl.Code != wire.CodeInternal {
		t.Fatalf("the channel ended %+v, want %s", cl, wire.CodeInternal)
	}
	if f.called {
		t.Fatal("the installer ran over a decision for another action")
	}
}

// Which failure of an activation the API is told by which code. The errors are
// package install's own, and code() is what turns them into the CLOSE -- the
// same function the install channel beside it uses, so the two cannot drift.
func TestEveryFailureOfAnActivationHasItsCode(t *testing.T) {
	for _, c := range []struct {
		err  error
		want wire.Code
	}{
		{install.ErrVerify, wire.CodeVerify},
		{install.ErrForbidden, wire.CodeForbidden},
		{install.ErrInternal, wire.CodeInternal},
	} {
		f := &fakeInstaller{err: c.err}
		s := New(Deps{Installer: f})
		_, cl := s.activate(context.Background(), nil, activateDecision(t), bytes.NewReader([]byte("x")))
		if cl == nil || cl.Code != c.want {
			t.Errorf("%v ended the channel %+v, want %s", c.err, cl, c.want)
		}
	}
}

// ---- the reader of a channel that carries a file -------------------------

// fakeFrames is a channel as this reader sees it: the inbound messages a case
// wrote down, and the credit it gave back.
type fakeFrames struct {
	in       []link.Inbound
	at       int
	granted  []uint32
	grantErr error
	recvErr  error
}

func (f *fakeFrames) Recv(context.Context) (link.Inbound, error) {
	if f.at >= len(f.in) {
		if f.recvErr != nil {
			return link.Inbound{}, f.recvErr
		}
		return link.Inbound{}, io.EOF
	}
	m := f.in[f.at]
	f.at++
	return m, nil
}

func (f *fakeFrames) Grant(credit uint32, _ []byte) error {
	f.granted = append(f.granted, credit)
	return f.grantErr
}

func data(b ...byte) link.Inbound { return link.Inbound{Kind: wire.TypeData, Data: b} }

var eofFrame = link.Inbound{Kind: wire.TypeEOF}

// THE PACKAGE FLOWS and is never held whole: what the reader keeps is one
// frame at most. The credit of every frame goes back as the frame is taken off
// the channel, which is what keeps the API sending.
func TestTheInputOfAFlowReadsAsItFlows(t *testing.T) {
	f := &fakeFrames{in: []link.Inbound{data(1, 2, 3), data(4, 5), eofFrame}}
	in := &chanInput{ctx: context.Background(), ch: f, max: 100}
	got, err := io.ReadAll(in)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, []byte{1, 2, 3, 4, 5}) {
		t.Fatalf("read %v, want the five bytes of the two frames", got)
	}
	if len(f.granted) != 2 || f.granted[0] != 3 || f.granted[1] != 2 {
		t.Fatalf("granted %v, want one grant per frame of its own size", f.granted)
	}
	if cl := in.drain(); cl != nil {
		t.Fatalf("after the EOF the channel owes %+v, want nothing", cl)
	}
}

// A read with less room than the frame has takes the rest next time, and the
// credit is still given back once -- the grant belongs to the frame, not to
// the reader.
func TestAFrameLargerThanTheReadIsKeptForTheNextOne(t *testing.T) {
	f := &fakeFrames{in: []link.Inbound{data(1, 2, 3, 4), eofFrame}}
	in := &chanInput{ctx: context.Background(), ch: f, max: 100}
	one := make([]byte, 2)
	n, err := in.Read(one)
	if n != 2 || err != nil {
		t.Fatalf("first Read = %d, %v", n, err)
	}
	rest, err := io.ReadAll(in)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(append(one, rest...), []byte{1, 2, 3, 4}) {
		t.Fatalf("read %v then %v, want the frame in order", one, rest)
	}
	if len(f.granted) != 1 || f.granted[0] != 4 {
		t.Fatalf("granted %v, want one grant of the whole frame", f.granted)
	}
}

// An empty DATA is no grant: a GRANT of nothing is none of the contract, and
// an empty frame took no credit to give back.
func TestAnEmptyDataGrantsNothing(t *testing.T) {
	f := &fakeFrames{in: []link.Inbound{data(), data(9), eofFrame}}
	in := &chanInput{ctx: context.Background(), ch: f, max: 100}
	got, err := io.ReadAll(in)
	if err != nil || !bytes.Equal(got, []byte{9}) {
		t.Fatalf("read %v, %v", got, err)
	}
	if len(f.granted) != 1 || f.granted[0] != 1 {
		t.Fatalf("granted %v, want the one grant of the one real frame", f.granted)
	}
}

// WHAT COMES PAST THE BOUND IS REFUSED, counted before a byte is handed on, and
// the CLOSE this side owes says E_TOOLARGE.
func TestMoreThanTheBoundEndsTooLarge(t *testing.T) {
	f := &fakeFrames{in: []link.Inbound{data(1, 2, 3), data(4, 5, 6), eofFrame}}
	in := &chanInput{ctx: context.Background(), ch: f, max: 4}
	got, _ := io.ReadAll(in)
	if len(got) > 4 {
		t.Fatalf("%d bytes came through a bound of 4", len(got))
	}
	cl := in.drain()
	if cl == nil || cl.Code != wire.CodeTooLarge {
		t.Fatalf("the channel owes %+v, want %s", cl, wire.CodeTooLarge)
	}
}

// A channel that ends before its input did is E_PROTOCOL: no action runs on
// half a package, and the one CLOSE this side owes says so.
func TestACutOffInputEndsAsProtocol(t *testing.T) {
	f := &fakeFrames{in: []link.Inbound{data(1, 2)}, recvErr: errors.New("the tunnel went")}
	in := &chanInput{ctx: context.Background(), ch: f, max: 100}
	if _, err := io.ReadAll(in); err == nil {
		t.Fatal("ReadAll came back without an error over a cut-off channel")
	}
	cl := in.drain()
	if cl == nil || cl.Code != wire.CodeProtocol {
		t.Fatalf("the channel owes %+v, want %s", cl, wire.CodeProtocol)
	}
}

// A grant that fails because the channel is already closed is the API's doing
// and reads as a cut-off input, not as a fault of this server: its CLOSE comes
// in while what it sent before is still being handed out.
func TestAGrantOnAClosedChannelReadsAsCutOff(t *testing.T) {
	f := &fakeFrames{in: []link.Inbound{data(1, 2), eofFrame}, grantErr: link.ErrClosed}
	in := &chanInput{ctx: context.Background(), ch: f, max: 100}
	if _, err := io.ReadAll(in); err == nil {
		t.Fatal("ReadAll came back without an error after a grant on a closed channel")
	}
	cl := in.drain()
	if cl == nil || cl.Code != wire.CodeProtocol {
		t.Fatalf("the channel owes %+v, want %s", cl, wire.CodeProtocol)
	}
	// A grant that failed for another reason is this server's fault instead.
	g := &fakeFrames{in: []link.Inbound{data(1, 2), eofFrame}, grantErr: errors.New("something else")}
	other := &chanInput{ctx: context.Background(), ch: g, max: 100}
	_, _ = io.ReadAll(other)
	if cl := other.drain(); cl == nil || cl.Code != wire.CodeInternal {
		t.Fatalf("the channel owes %+v, want %s", cl, wire.CodeInternal)
	}
}

// WHAT THE INSTALLER DID NOT READ IS DRAINED, and it has to be: an installer
// that refused a package after its first bytes leaves the rest of it on the
// channel, and a channel closed over unread DATA is one the API is still
// sending into.
func TestWhatTheInstallerLeftIsDrained(t *testing.T) {
	f := &fakeInstaller{limit: 2}
	s := New(Deps{Installer: f})
	frames := &fakeFrames{in: []link.Inbound{data(1, 2, 3, 4), data(5, 6), eofFrame}}
	in := &chanInput{ctx: context.Background(), ch: frames, max: 100}
	_, cl := s.activate(context.Background(), nil, activateDecision(t), in)
	if cl != nil {
		t.Fatalf("the channel ended %+v, want a result", cl)
	}
	if len(f.read) != 2 {
		t.Fatalf("the installer read %d bytes, want the 2 it asked for", len(f.read))
	}
	if sent := in.drain(); sent != nil {
		t.Fatalf("after the drain the channel owes %+v, want nothing", sent)
	}
	// Every frame was taken off the channel and its credit given back, which
	// is the whole point of draining.
	if len(frames.granted) != 2 {
		t.Fatalf("granted %v, want one grant per frame", frames.granted)
	}
	if frames.at != len(frames.in) {
		t.Fatalf("%d of %d frames were taken off the channel", frames.at, len(frames.in))
	}
}

// WHAT THE API DID OUTWEIGHS WHAT THE INSTALLER MADE OF IT: a package over the
// bound ends the channel as E_TOOLARGE even where the installer answered its
// own refusal first, because the installer saw half a file and cannot know
// that.
func TestTheBoundBeatsTheInstallersAnswer(t *testing.T) {
	f := &fakeInstaller{limit: 2, err: install.ErrVerify}
	s := New(Deps{Installer: f})
	frames := &fakeFrames{in: []link.Inbound{data(1, 2, 3), data(4, 5, 6), eofFrame}}
	in := &chanInput{ctx: context.Background(), ch: frames, max: 4}
	_, cl := s.activate(context.Background(), nil, activateDecision(t), in)
	if cl == nil || cl.Code != wire.CodeVerify {
		t.Fatalf("the installer answered %+v", cl)
	}
	// And this is the order flow() keeps: the drain's code wins.
	sent := in.drain()
	if sent == nil || sent.Code != wire.CodeTooLarge {
		t.Fatalf("the channel owes %+v, want %s", sent, wire.CodeTooLarge)
	}
}

// The params of this action are the peer's bytes, and nothing here parses them
// a second time: install.Manager reads them through the contract's own parser
// and answers ErrVerify for what will not parse. This holds that the handler
// hands the decision over untouched rather than deciding on a copy.
func TestActivateHandsTheDecisionOverUntouched(t *testing.T) {
	var seen access.Decision
	f := &recordingInstaller{onCall: func(d access.Decision) { seen = d }}
	s := New(Deps{Installer: f})
	d := activateDecision(t)
	d.Opened.Open.Params = json.RawMessage(`{"app":"de.example.notes"}`)
	if _, cl := s.activate(context.Background(), nil, d, bytes.NewReader(nil)); cl != nil {
		t.Fatalf("the channel ended %+v", cl)
	}
	if string(seen.Params()) != `{"app":"de.example.notes"}` {
		t.Fatalf("the installer got the params %q", seen.Params())
	}
}

// twoVersions is a chain that hands out version 1 on the FIRST read and
// version 2 on every later one, and counts the reads. It stands for the one
// thing a real keeper does that a case cannot time: a new version landing
// while a channel runs.
type twoVersions struct {
	first, later *trust.List
	reads        int
}

func (c *twoVersions) List() *trust.List {
	c.reads++
	if c.reads == 1 {
		return c.first
	}
	return c.later
}

func (c *twoVersions) Apply(context.Context, []byte) (uint64, error) { return 0, nil }

// A VERSION THAT LANDS WHILE THE CHANNEL RUNS CHANGES NOTHING FOR IT. This is
// the case that holds the ORDER and not only the handover: the list is read
// once, before the check, and what runs afterwards gets that one. A handler
// that read the chain again at its own line would install against a version
// nobody checked the task against -- and until this fixture existed no case
// could tell the two apart, because Deps.Chain was the keeper itself and
// nothing could put two versions in front of one channel.
func TestAVersionThatLandsWhileTheChannelRunsIsNotTheOneItInstallsWith(t *testing.T) {
	w := newWorld(t)
	f := &fakeInstaller{}
	// The version the channel is decided on is the keeper's real one -- the
	// task of the OPEN is signed against it. What comes AFTER the first read
	// is another list, and it must not reach the installer.
	decided := w.keep.List()
	later := &trust.List{Installation: "spaeter"}
	c := &twoVersions{first: decided, later: later}
	w.rebuild(func(d *Deps) { d.Installer, d.Chain = f, c })
	ended := w.tunnel(t, func(a *apiSide) {
		a.open(1, activateChannelOpen(t, w, 0x56))
		a.eof(1)
		a.result(1)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	// ONCE, and that is the assurance itself: a second read is a second
	// version, whatever it then does with it. The counter is also what says
	// the fixture worked -- a chain that was never asked would let this case
	// pass without measuring anything.
	if c.reads != 1 {
		t.Fatalf("the chain was read %d times for one channel, want once", c.reads)
	}
	if !f.called {
		t.Fatal("the installer was never called -- then this case measures no version")
	}
	if f.list == later {
		t.Fatal("the installer installs against the version that landed later, not the decided one")
	}
	if f.list != decided {
		t.Fatalf("the installer got %v, want the version the channel was decided on", f.list)
	}
}

// AND IT IS THE LIST OF THE DECISION, over a real channel. The case above
// hands the list to Server.activate itself, so it cannot see whether Open
// passes on the version it decided the channel on -- a mutant that handed
// core() a nil list lived on it. This one goes through the whole way, and the
// list that arrives is the keeper's own version.
func TestTheVersionAChannelWasDecidedOnIsTheOneTheInstallerGets(t *testing.T) {
	w := newWorld(t)
	f := &fakeInstaller{}
	w.rebuild(func(d *Deps) { d.Installer = f })
	ended := w.tunnel(t, func(a *apiSide) {
		a.open(1, activateChannelOpen(t, w, 0x4c))
		a.eof(1)
		a.result(1)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if !f.called {
		t.Fatal("the installer was never called -- then this case measures no list")
	}
	if f.list == nil {
		t.Fatal("the installer got no trust list, although the channel was decided on one")
	}
	if f.list != w.keep.List() {
		t.Fatalf("the installer got %v, want the version the channel was decided on",
			f.list.Installation)
	}
}

// THE LIST OF THE CHANNEL REACHES THE INSTALLER, byte-identical and not a
// version read anew. Server.Open decides a whole channel on one version, and
// an activation runs long enough for the next one to land in between: an
// installer handed the newest list instead would check the package against
// another version than the one that verified the task.
func TestTheListOfTheChannelReachesTheInstaller(t *testing.T) {
	f := &fakeInstaller{}
	s := New(Deps{Installer: f})
	mine := &trust.List{Installation: "i-1"}
	if _, cl := s.activate(context.Background(), mine, activateDecision(t),
		bytes.NewReader(nil)); cl != nil {
		t.Fatalf("the channel ended %+v", cl)
	}
	if f.list != mine {
		t.Fatalf("the installer got %v, want the list of the channel", f.list)
	}
}

type recordingInstaller struct{ onCall func(access.Decision) }

func (r *recordingInstaller) Activate(_ context.Context, _ *trust.List, d access.Decision,
	_ io.Reader) ([]byte, error) {
	r.onCall(d)
	return []byte("{}"), nil
}

// WHAT THE API DID OUTWEIGHS WHAT THE ACTION MADE OF IT, and this is the one
// case that holds the decision: closeOf reads the REST out first and lets its
// code win. A mutant that looked at chanInput.cl instead -- what has been
// noticed so far, rather than what is still on the channel -- passed every
// other case of this file on 18.10.2026.
func TestTheRestOfTheChannelDecidesBeforeTheAction(t *testing.T) {
	// The action refused after two bytes; the rest of the package is over the
	// bound and nobody has looked at it yet.
	f := &fakeFrames{in: []link.Inbound{data(1, 2), data(3, 4, 5), eofFrame}}
	in := &chanInput{ctx: context.Background(), ch: f, max: 3}
	two := make([]byte, 2)
	if _, err := io.ReadFull(in, two); err != nil {
		t.Fatalf("the action could not read its two bytes: %v", err)
	}
	if in.cl != nil {
		t.Fatalf("the bound was already noticed (%+v) -- then this case measures nothing", in.cl)
	}
	got := closeOf(in, refuse(wire.CodeVerify))
	if got == nil || got.Code != wire.CodeTooLarge {
		t.Fatalf("the channel ends %+v, want the %s of the rest", got, wire.CodeTooLarge)
	}
	// And where the rest is in order, the action's own refusal is the answer.
	g := &fakeFrames{in: []link.Inbound{data(1), eofFrame}}
	ok := &chanInput{ctx: context.Background(), ch: g, max: 10}
	if got := closeOf(ok, refuse(wire.CodeVerify)); got == nil || got.Code != wire.CodeVerify {
		t.Fatalf("with the rest in order the channel ends %+v, want the action's %s",
			got, wire.CodeVerify)
	}
	// And a flow that went through answers nothing at all.
	h := &fakeFrames{in: []link.Inbound{data(1), eofFrame}}
	done := &chanInput{ctx: context.Background(), ch: h, max: 10}
	if got := closeOf(done, nil); got != nil {
		t.Fatalf("a flow that went through owes %+v, want nothing", got)
	}
}
