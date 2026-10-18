// The activate action over a REAL channel of a tunnel, from the OPEN to the
// CLOSE.
//
// Why this file is beside activate_test.go and not in it: the cases there
// call Server.activate and chanInput directly, with a decision built in the
// test. That measures the action and the reader, and it is the right way to
// measure them - but it steps over Server.core, so the fork that picks the
// flows map out of the two is reached by no case. The mutant of that fork
// (activate served out of acts instead of flows, or not served at all) lived
// because of it.
//
// What it costs to close that hole is a signed root task: activate is
// Root in core.All, so access.Check refuses every OPEN without one, and
// nothing behind it runs. The helpers for that already exist in
// attack_test.go (atkCert, atkTask), which is why these cases are cheap.
package serve

import (
	"bytes"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// chanParams are the params of a package the fixture's admin approved. No
// grants: an activation with grants is critical (core.Critical), and a
// critical one would want the re-confirmation on top of the task.
func chanParams() core.ActivateParams {
	return core.ActivateParams{App: "org.sorglos.notes", Store: trust.StoreGlobal, Version: "1.0.0",
		SHA256: bytes.Repeat([]byte{0x7c}, 32), UI: bytes.Repeat([]byte{0x6d}, 32)}
}

// activateChannelOpen is the OPEN of ngc.store/controller/activate with a
// root task of the fixture's admin, the way the shell sends it.
//
// Two things about it are measured and not obvious, and both end E_PROTOCOL
// before the action if they are got wrong - which looks like a finding about
// the fork and is one about the OPEN:
//
//   - The OPEN carries NO params of its own. With a task only the signed
//     params count, and wire.Open.validate refuses an OPEN that has both.
//   - The channel number of a task is used once. access.Check remembers the
//     ID of every task that ran (dbapi.ErrUsed), so each case needs its own.
func activateChannelOpen(t testing.TB, w *world, id byte) []byte {
	t.Helper()
	params := mustJSON(t, chanParams())
	env := atkTask(t, w, sessionSeed, id, node, core.AppStore, core.ServiceController,
		core.ActActivate, params)
	inner := mustJSON(t, wire.Open{App: core.AppStore, Service: core.ServiceController,
		Action: core.ActActivate, Window: wire.DefaultWindow, Task: env})
	client := atkCert(t, w)
	p := w.admin.Prove(sign.PurposeClientCert, client)
	return mustJSON(t, tunnel.Open{Open: inner, Window: wire.MaxWindow, Client: client, Proof: &p})
}

// An activate channel of a tunnel reaches the installer and ends ok. This is
// the case the fork mutant dies on: with activate out of flows the same
// channel never reaches Server.activate at all.
func TestAnActivateChannelOfATunnelReachesTheInstaller(t *testing.T) {
	w := newWorld(t)
	f := &fakeInstaller{}
	w.rebuild(func(d *Deps) { d.Installer = f })
	pkg := bytes.Repeat([]byte{0x5a}, 3000)
	ended := w.tunnel(t, func(a *apiSide) {
		// open waits for the first GRANT before anything is sent. Without
		// that credit the DATA below is a frame this channel never allowed,
		// and link ends it E_PROTOCOL before the action is reached.
		credit, _ := a.open(1, activateChannelOpen(t, w, 0x41))
		if credit < int64(len(pkg)) {
			t.Fatalf("the first GRANT is %d bytes, too little for the %d of this case",
				credit, len(pkg))
		}
		a.data(1, pkg)
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
	if !f.called {
		t.Fatal("the installer was never reached")
	}
	if !bytes.Equal(f.read, pkg) {
		t.Errorf("the installer read %d bytes, want the %d of the package", len(f.read), len(pkg))
	}
}

// The intermediate check that makes the case above worth anything: the same
// channel on a server WITHOUT an installer ends E_INTERNAL.
//
// It is here because E_INTERNAL is also what an unserved action answers
// (Server.core), so these two cases together say which of the two happened:
// if the channel above had been refused by access - a task this fixture
// cannot sign, a node that is not this one, a missing re-confirmation - this
// one would end E_VERIFY, E_FORBIDDEN or E_CONFIRM instead, and the pair
// would not read as "served" against "not served".
func TestAnActivateChannelWithoutAnInstallerEndsInternalAndNotRefused(t *testing.T) {
	w := newWorld(t)
	ended := w.tunnel(t, func(a *apiSide) {
		a.send(wire.Frame{Type: wire.TypeOpen, Channel: 1,
			Payload: activateChannelOpen(t, w, 0x42)})
		cl := a.closed(1)
		wantCode(t, cl, wire.CodeInternal)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
}

// A package larger than the credit of one GRANT gets through a real channel.
//
// The cases on chanInput hold the same arithmetic against a stand-in that
// always grants; this one holds it against link, which keeps the books of
// the credit itself and ends the channel when a frame arrives without any.
// 6 x 60000 = 360000 bytes against a first GRANT of wire.DefaultWindow
// (262144), written out so this case does not reckon with the constant it
// measures.
func TestAPackageLargerThanOneWindowGetsThroughARealChannel(t *testing.T) {
	w := newWorld(t)
	f := &fakeInstaller{}
	w.rebuild(func(d *Deps) { d.Installer = f })
	const frames, size = 6, 60000
	if frames*size <= wire.DefaultWindow {
		t.Fatalf("%d bytes fit in one window of %d: this case measures nothing",
			frames*size, wire.DefaultWindow)
	}
	ended := w.tunnel(t, func(a *apiSide) {
		a.open(1, activateChannelOpen(t, w, 0x43))
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
