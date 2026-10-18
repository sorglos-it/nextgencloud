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
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// fakeInbox hands out these messages in order and then says the channel is
// over. grants counts the credit it was given back, so a case can see that a
// frame was paid for only once it had left the reader.
type fakeInbox struct {
	in     []link.Inbound
	at     int
	grants []uint32
	err    error // what Grant answers
}

func (b *fakeInbox) Recv(context.Context) (link.Inbound, error) {
	if b.at >= len(b.in) {
		return link.Inbound{}, io.ErrUnexpectedEOF
	}
	m := b.in[b.at]
	b.at++
	return m, nil
}

func (b *fakeInbox) Grant(credit uint32, _ []byte) error {
	if b.err != nil {
		return b.err
	}
	b.grants = append(b.grants, credit)
	return nil
}

// fakeInstaller stands in for install.Manager. It writes down the list it was
// handed and everything it read off the channel, and answers what a case told
// it to.
type fakeInstaller struct {
	calls int
	list  *trust.List
	read  []byte
	err   error
	// readErr says that this installer stops reading after the first error
	// of the channel, the way install.Accept does.
	readErr error
}

func (f *fakeInstaller) Activate(_ context.Context, l *trust.List, _ access.Decision,
	r io.Reader) ([]byte, error) {
	f.calls++
	f.list = l
	b, err := io.ReadAll(r)
	f.read = b
	f.readErr = err
	return []byte("{}"), f.err
}

// activateParams are the params of a package the fixture's admin approved.
func activateParams() core.ActivateParams {
	return core.ActivateParams{App: "org.sorglos.notes", Store: trust.StoreGlobal, Version: "1.0.0",
		SHA256: bytes.Repeat([]byte{0x7c}, 32), UI: bytes.Repeat([]byte{0x6d}, 32)}
}

// activateOpen is the OPEN of ngc.store/controller/activate with a root task
// of the fixture's admin: the one way this action may be opened at all
// (core.Action.Root).
func activateOpen(t testing.TB, w *world, id byte, p core.ActivateParams) []byte {
	t.Helper()
	params := json.RawMessage(mustJSON(t, p))
	env := atkTask(t, w, sessionSeed, id, node, core.AppStore, core.ServiceController,
		core.ActActivate, params)
	// The OPEN carries no params of its own: with a task only the signed
	// params count, and wire.Open.validate refuses an OPEN that carries both.
	inner := mustJSON(t, wire.Open{App: core.AppStore, Service: core.ServiceController,
		Action: core.ActActivate, Window: wire.DefaultWindow, Task: env})
	client := atkCert(t, w)
	pr := w.admin.Prove(sign.PurposeClientCert, client)
	return mustJSON(t, tunnel.Open{Open: inner, Window: wire.MaxWindow, Client: client, Proof: &pr})
}

// An activate channel runs the installer of this controller and answers the
// empty object: the action is carried out and not refused.
//
// This is the assurance of the wiring itself. Before the installer stood in
// Server.streams the same channel ended E_INTERNAL without ever reaching
// install.Manager — measured 18.10.2026 on the running installation, which is
// what the user read as "Das hat der Rechner nicht angenommen."
func TestAnActivateChannelRunsTheInstaller(t *testing.T) {
	w := newWorld(t)
	f := &fakeInstaller{}
	w.rebuild(func(d *Deps) { d.Installer = f })
	pkg := bytes.Repeat([]byte{0x5a}, 3000)
	ended := w.tunnel(t, func(a *apiSide) {
		// open waits for the first GRANT before anything is sent: without
		// that credit the DATA below would be a frame this channel never
		// allowed, and link would end it E_PROTOCOL.
		credit, _ := a.open(1, activateOpen(t, w, 0x31, activateParams()))
		if credit < int64(len(pkg)) {
			t.Fatalf("the first GRANT is %d bytes, too little for the %d of this case", credit, len(pkg))
		}
		a.data(1, pkg)
		a.eof(1)
		out, cl := a.result(1)
		if string(out) != "{}" {
			t.Errorf("result = %q, want the empty object", out)
		}
		if cl.Status != wire.StatusOK {
			t.Errorf("CLOSE = %+v, want ok", cl)
		}
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if f.calls != 1 {
		t.Fatalf("the installer ran %d times, want 1", f.calls)
	}
	if !bytes.Equal(f.read, pkg) {
		t.Errorf("the installer read %d bytes, want the %d of the package", len(f.read), len(pkg))
	}
	if f.readErr != nil {
		t.Errorf("reading the package gave %v, want the EOF of the API", f.readErr)
	}
	// The list the installer got is the one the channel was decided on, and
	// not whatever the keeper holds when the installer reaches for it: a
	// version that changed in between would check the package against one
	// list and the task against another.
	if f.list != w.keep.List() {
		t.Error("the installer got another trust list than the channel was decided on")
	}
}

// A controller without an installer refuses the channel as this program wired
// wrong, and reads nothing: which halves a server was built with is nothing an
// API decides.
func TestAnActivateChannelWithoutAnInstallerIsRefused(t *testing.T) {
	w := newWorld(t)
	ended := w.tunnel(t, func(a *apiSide) {
		a.send(wire.Frame{Type: wire.TypeOpen, Channel: 1,
			Payload: activateOpen(t, w, 0x32, activateParams())})
		wantCode(t, a.closed(1), wire.CodeInternal)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// Every error of package install has one code of the contract, and the three
// walls are told apart: what was not approved is E_VERIFY, what was approved
// and still may not run is E_FORBIDDEN, the paid app without a licence is
// E_LICENSE, and what went wrong on this machine is E_INTERNAL.
func TestEachInstallErrorHasItsOwnCode(t *testing.T) {
	for name, c := range map[string]struct {
		err  error
		want wire.Code
	}{
		"not what was approved": {install.ErrVerify, wire.CodeVerify},
		"no approved source":    {install.ErrSource, wire.CodeVerify},
		"not allowed":           {install.ErrForbidden, wire.CodeForbidden},
		"no usable licence":     {install.ErrLicense, wire.CodeLicense},
		"wired wrong":           {install.ErrInternal, wire.CodeInternal},
		"no room on the disk":   {install.ErrDisk, wire.CodeInternal},
		"not enough free space": {install.ErrSpace, wire.CodeInternal},
		"wrapped":               {errors.Join(install.ErrForbidden, errors.New("x")), wire.CodeForbidden},
		"something else":        {errors.New("x"), wire.CodeInternal},
	} {
		cl := activateCode(c.err)
		if cl == nil {
			t.Errorf("%s: no CLOSE at all", name)
			continue
		}
		if cl.Code != c.want {
			t.Errorf("%s: code %q, want %q", name, cl.Code, c.want)
		}
	}
}

// The two maps of this server are one namespace: core looks in streams first
// and then in acts, so an action in both would be served by the stream half
// and the other half would be a line no case can reach.
func TestNoActionIsBothBufferedAndStreamed(t *testing.T) {
	s := New(Deps{Installer: &fakeInstaller{}})
	if len(s.streams) == 0 {
		t.Fatal("no streamed action at all: the fork of this build is gone")
	}
	for k := range s.streams {
		if _, both := s.acts[k]; both {
			t.Errorf("%v stands in both maps", k)
		}
	}
}

// Every action of the two maps is one of core.All, and it is the action the
// contract names: a key built by hand is a key that can be built wrong, and a
// server whose map says "activate" of another app would serve a channel access
// let through for something else.
func TestEveryServedActionIsOneOfTheContract(t *testing.T) {
	s := New(Deps{Installer: &fakeInstaller{}})
	keys := make([]name, 0, len(s.acts)+len(s.streams))
	for k := range s.acts {
		keys = append(keys, k)
	}
	for k := range s.streams {
		keys = append(keys, k)
	}
	for _, k := range keys {
		if _, ok := core.Lookup(k[0], k[1], k[2]); !ok {
			t.Errorf("%v is no action of the contract", k)
		}
	}
	// And the one this file is about is served at all.
	if _, ok := s.streams[name{core.AppStore, core.ServiceController, core.ActActivate}]; !ok {
		t.Error("activate is not served by this build")
	}
}

// A package larger than the credit of the first GRANT gets through, because
// every frame handed to the installer is paid back at once. Without that the
// API runs out of credit after the first wire.DefaultWindow bytes and link
// ends the channel: TestAPackageLargerThanOneWindowGetsThrough is what holds
// chanReader.give.
func TestAPackageLargerThanOneWindowGetsThrough(t *testing.T) {
	w := newWorld(t)
	f := &fakeInstaller{}
	w.rebuild(func(d *Deps) { d.Installer = f })
	// Six frames of nearly the largest payload the contract allows are more
	// than wire.DefaultWindow (262144) together: 6 * 60000 = 360000. The
	// numbers are written out so the case does not reckon with the same
	// constant as the code it measures.
	const frames, size = 6, 60000
	if frames*size <= wire.DefaultWindow {
		t.Fatalf("%d bytes fit in one window of %d: this case measures nothing",
			frames*size, wire.DefaultWindow)
	}
	ended := w.tunnel(t, func(a *apiSide) {
		a.open(1, activateOpen(t, w, 0x33, activateParams()))
		for i := 0; i < frames; i++ {
			a.data(1, bytes.Repeat([]byte{byte(i)}, size))
		}
		a.eof(1)
		out, cl := a.result(1)
		if cl.Status != wire.StatusOK {
			t.Errorf("CLOSE = %+v, want ok", cl)
		}
		if string(out) != "{}" {
			t.Errorf("result = %q, want the empty object", out)
		}
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if len(f.read) != frames*size {
		t.Errorf("the installer read %d bytes, want %d", len(f.read), frames*size)
	}
}

// An input that ends before its EOF is the channel's fault and not the
// package's: the CLOSE says E_PROTOCOL, and what the installer made of the
// half it got is not what the API is told. An admin who read "Paket passt
// nicht" here would go looking at a package that is whole.
func TestAnActivateInputCutOffIsTheChannelsFaultAndNotThePackages(t *testing.T) {
	w := newWorld(t)
	// The installer answers the error of a package that does not check out,
	// so the case can tell which of the two reached the API.
	f := &fakeInstaller{err: install.ErrVerify}
	w.rebuild(func(d *Deps) { d.Installer = f })
	ended := w.tunnel(t, func(a *apiSide) {
		a.open(1, activateOpen(t, w, 0x34, activateParams()))
		a.data(1, bytes.Repeat([]byte{0x11}, 100))
		// The CLOSE of the API in place of its EOF: the input stops halfway.
		a.shut(1)
		wantCode(t, a.closed(1), wire.CodeProtocol)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if f.calls != 1 {
		t.Fatalf("the installer ran %d times, want 1", f.calls)
	}
	if f.readErr == nil || errors.Is(f.readErr, io.EOF) {
		t.Errorf("the installer read to %v, want an error of the cut channel", f.readErr)
	}
}

// The bound of the action holds on a streamed input too, and it holds on what
// went past rather than on what is held: a reader that never keeps more than
// one frame would otherwise take any number of frames.
func TestAStreamedInputIsBoundedByTheActionsMaxIn(t *testing.T) {
	a, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActActivate)
	if !ok {
		t.Fatal("the contract has no activate action of the controller")
	}
	// The bound of the contract is 4303355908 bytes, far too much to send in
	// a case, so the reader is built with the action cut down to a number a
	// case can reach. What is measured is the line that counts, not the
	// number in the contract.
	small := a
	small.MaxIn = 1000
	box := &fakeInbox{in: []link.Inbound{
		{Kind: wire.TypeData, Data: bytes.Repeat([]byte{0x22}, 600)},
		{Kind: wire.TypeData, Data: bytes.Repeat([]byte{0x22}, 600)},
		{Kind: wire.TypeEOF},
	}}
	r := newChanReader(context.Background(), box, small)
	n, err := io.Copy(io.Discard, r)
	if err == nil {
		t.Fatalf("the reader took %d bytes and all of them, want a refusal past %d", n, small.MaxIn)
	}
	if !errors.Is(err, errTooLarge) {
		t.Errorf("the reader gave %v, want %v", err, errTooLarge)
	}
	cl := r.close()
	if cl == nil || cl.Code != wire.CodeTooLarge {
		t.Errorf("the channel owes %+v, want E_TOOLARGE", cl)
	}
	if n > small.MaxIn {
		t.Errorf("%d bytes went past a bound of %d", n, small.MaxIn)
	}
}

// A frame is paid for when it has left the reader and not when it arrived:
// until then its bytes are still here, and credit given back early is credit
// for memory this side still holds.
func TestAFrameIsPaidForOnlyOnceItHasLeftTheReader(t *testing.T) {
	a, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActActivate)
	if !ok {
		t.Fatal("the contract has no activate action of the controller")
	}
	box := &fakeInbox{in: []link.Inbound{
		{Kind: wire.TypeData, Data: bytes.Repeat([]byte{0x33}, 10)},
		{Kind: wire.TypeEOF},
	}}
	r := newChanReader(context.Background(), box, a)
	// Half the frame out: nothing is paid for yet.
	half := make([]byte, 4)
	if n, err := r.Read(half); n != 4 || err != nil {
		t.Fatalf("Read = %d, %v, want 4 bytes", n, err)
	}
	if len(box.grants) != 0 {
		t.Errorf("%v was given back while 6 bytes of the frame were still here", box.grants)
	}
	rest := make([]byte, 16)
	if n, err := r.Read(rest); n != 6 || err != nil {
		t.Fatalf("Read = %d, %v, want the remaining 6 bytes", n, err)
	}
	if len(box.grants) != 1 || box.grants[0] != 10 {
		t.Errorf("credit given back: %v, want one grant of 10", box.grants)
	}
	if _, err := r.Read(rest); !errors.Is(err, io.EOF) {
		t.Errorf("after the EOF of the API the reader gave %v, want io.EOF", err)
	}
	if len(box.grants) != 1 {
		t.Errorf("credit given back: %v, want no second grant", box.grants)
	}
}

// An empty DATA takes no credit and is no end: the contract has no GRANT of
// nothing, so the reader skips it and reads on.
func TestAnEmptyDataFrameIsSkippedAndPaysNothing(t *testing.T) {
	a, _ := core.Lookup(core.AppStore, core.ServiceController, core.ActActivate)
	box := &fakeInbox{in: []link.Inbound{
		{Kind: wire.TypeData, Data: nil},
		{Kind: wire.TypeData, Data: []byte("ab")},
		{Kind: wire.TypeEOF},
	}}
	b, err := io.ReadAll(newChanReader(context.Background(), box, a))
	if err != nil || string(b) != "ab" {
		t.Errorf("the reader gave %q, %v, want \"ab\"", b, err)
	}
	if len(box.grants) != 1 || box.grants[0] != 2 {
		t.Errorf("credit given back: %v, want one grant of 2", box.grants)
	}
}

// A channel whose credit cannot be given back is over, and the reader says
// which of the two it was: the API closed it (E_PROTOCOL, the cut channel) or
// this server failed (E_INTERNAL). The two are told apart because an admin
// reads one of them and goes looking at the wrong half for the other.
func TestCreditThatCannotBeGivenBackEndsTheChannel(t *testing.T) {
	a, _ := core.Lookup(core.AppStore, core.ServiceController, core.ActActivate)
	for name, c := range map[string]struct {
		err  error
		want wire.Code
	}{
		"the API closed it":  {link.ErrClosed, wire.CodeProtocol},
		"this server failed": {errors.New("x"), wire.CodeInternal},
	} {
		box := &fakeInbox{err: c.err, in: []link.Inbound{
			{Kind: wire.TypeData, Data: []byte("abc")},
			{Kind: wire.TypeEOF},
		}}
		r := newChanReader(context.Background(), box, a)
		if _, err := io.ReadAll(r); err == nil {
			t.Errorf("%s: the reader read to the end, want the failed grant", name)
			continue
		}
		cl := r.close()
		if cl == nil || cl.Code != c.want {
			t.Errorf("%s: the channel owes %+v, want %q", name, cl, c.want)
		}
	}
}

// WHAT THE MAP SAYS IS WHAT THIS BUILD CAN DO. A server wired without an
// installer refuses every activate channel (activate.go, and since the map is
// conditional it does not even reach the action), so it may not answer "yes"
// when it is asked whether it serves activations. Whoever wires this server
// reads it back here instead of reading the literal it passed to New.
//
// The two buffered actions are in the same assertion on purpose: they are
// wired unconditionally, so a Serves that answered "no" to everything would
// pass the half above and fail here.
func TestServesSaysWhatThisBuildCarriesOut(t *testing.T) {
	without := New(Deps{})
	if without.Serves(core.AppStore, core.ServiceController, core.ActActivate) {
		t.Error("a server without an installer says it serves activations")
	}
	if len(without.streams) != 0 {
		t.Errorf("streams of a server without an installer: %v", without.streams)
	}
	with := New(Deps{Installer: &fakeInstaller{}})
	if !with.Serves(core.AppStore, core.ServiceController, core.ActActivate) {
		t.Error("a server with an installer says it does not serve activations")
	}
	for _, k := range []name{trustKey, installKey} {
		if !with.Serves(k[0], k[1], k[2]) {
			t.Errorf("%v is wired and Serves says no", k)
		}
	}
	// An action of another app is none of this server's, whatever its name.
	if with.Serves(core.AppUsers, core.ServiceController, core.ActActivate) {
		t.Error("an activate of another app is served")
	}
}

// partialInstaller reads n bytes and then stops with its own error, the way
// install.Accept stops once the transfer header or the manifest of a package
// is not what the task named. What it leaves on the channel is the rest of the
// package.
type partialInstaller struct {
	n    int
	err  error
	read int
}

func (p *partialInstaller) Activate(_ context.Context, _ *trust.List, _ access.Decision,
	r io.Reader) ([]byte, error) {
	b := make([]byte, p.n)
	n, _ := io.ReadFull(r, b)
	p.read = n
	return nil, p.err
}

// WHAT THE ACTION DID NOT READ IS STILL ANSWERED. An installer that refuses a
// package after its first bytes leaves the rest of it on the channel, and over
// that rest the API may have gone past the bound of the action or been cut off
// -- neither of which this side has noticed while it lies unread. So the rest
// is read out (chanReader.drain) and what it says outranks the refusal of the
// action.
//
// This is the case the mutant needs: an activate that answers chanReader.close
// instead of drain reads a cl that is still nil here and sends the installer's
// own code out over a channel that was cut off or overran. It passed every
// other case of this package on 18.10.2026.
func TestWhatTheActionDidNotReadIsStillAnswered(t *testing.T) {
	a, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActActivate)
	if !ok {
		t.Fatal("the contract has no activate action of the controller")
	}
	// The bound of the contract is 4303355908 bytes, too much to send in a
	// case, so the action is cut down to a number a case can reach. What is
	// measured is the line that counts, not the number in the contract.
	small := a
	small.MaxIn = 1000
	for name, c := range map[string]struct {
		in   []link.Inbound
		act  core.Action
		want wire.Code
	}{
		// The rest of the input goes past the bound: the action saw only its
		// first 10 bytes and cannot know.
		"the rest overran the bound": {
			in: []link.Inbound{
				{Kind: wire.TypeData, Data: bytes.Repeat([]byte{0x41}, 600)},
				{Kind: wire.TypeData, Data: bytes.Repeat([]byte{0x41}, 600)},
				{Kind: wire.TypeEOF},
			},
			act:  small,
			want: wire.CodeTooLarge,
		},
		// The channel ended before its input did: there is no EOF frame, so
		// the inbox is over while DATA was still owed.
		"the rest was cut off": {
			in: []link.Inbound{
				{Kind: wire.TypeData, Data: bytes.Repeat([]byte{0x41}, 600)},
			},
			act:  a,
			want: wire.CodeProtocol,
		},
	} {
		// install.ErrVerify is a code of its own (E_VERIFY), so a wrong
		// answer here is told apart from the right one and not just absent.
		f := &partialInstaller{n: 10, err: install.ErrVerify}
		s := New(Deps{Installer: f})
		r := newChanReader(context.Background(), &fakeInbox{in: c.in}, c.act)
		d := access.Decision{Action: c.act}
		v, cl := s.activate(context.Background(), nil, d, r)
		if f.read != 10 {
			t.Fatalf("%s: the installer read %d bytes, want the 10 it asked for", name, f.read)
		}
		if v != nil {
			t.Errorf("%s: result %v went out over a channel that owes a CLOSE", name, v)
		}
		if cl == nil {
			t.Fatalf("%s: no CLOSE at all, want %q", name, c.want)
		}
		if cl.Code != c.want {
			t.Errorf("%s: CLOSE %q, want %q of the unread rest", name, cl.Code, c.want)
		}
	}
}

// And the other side of it: where the action read its input whole, draining
// finds nothing and the action's own refusal is what goes out. Without this
// the case above would pass on a drain that answered E_PROTOCOL for every
// channel.
func TestAnInstallerThatReadEverythingKeepsItsOwnCode(t *testing.T) {
	a, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActActivate)
	if !ok {
		t.Fatal("the contract has no activate action of the controller")
	}
	f := &fakeInstaller{err: install.ErrVerify}
	s := New(Deps{Installer: f})
	box := &fakeInbox{in: []link.Inbound{
		{Kind: wire.TypeData, Data: bytes.Repeat([]byte{0x42}, 40)},
		{Kind: wire.TypeEOF},
	}}
	r := newChanReader(context.Background(), box, a)
	_, cl := s.activate(context.Background(), nil, access.Decision{Action: a}, r)
	if cl == nil || cl.Code != wire.CodeVerify {
		t.Errorf("CLOSE = %+v, want the E_VERIFY of the installer", cl)
	}
	if len(f.read) != 40 {
		t.Errorf("the installer read %d bytes, want all 40", len(f.read))
	}
}
