package link

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// greetWith answers the hello of the controller with one of contract c. It is
// greet with the one value this file turns: the hello of the API decides which
// version the two ends speak.
func greetWith(a *apiSide, c int) {
	f, err := a.read()
	if err != nil {
		a.t.Errorf("hello of the controller: %v", err)
		return
	}
	ctl, err := wire.ParseControl(f.Payload)
	if err != nil || ctl.T != wire.CtlHello {
		a.t.Errorf("the first message was %q (%v), want the hello", ctl.T, err)
		return
	}
	a.control(wire.CtlHello, tunnel.Hello{Version: "1.0.0", Contract: c, Node: "ctl-1", TrustSeq: 1})
}

// Of the five control messages a tunnel knows, the API may send two: the hello
// and the goodbye. The other three are the controller's alone, and every one of
// them ends the link where it arrives - there is nothing to answer a peer that
// does not keep to the contract it proved with its handshake.
func TestControlRefusesWhatTheAPIMayNotSend(t *testing.T) {
	for _, c := range []struct {
		name string
		typ  string
		body any
	}{
		{"status", wire.CtlStatus, tunnel.Status{DiskFree: 1}},
		{"update", wire.CtlUpdate, store.UpdateReport{Target: trust.AutoAPI, Version: "1.1.0", State: store.UpdateInstalled}},
		{"notify", wire.CtlNotify, core.Notice{Level: core.NoticeInfo, Text: "Alles in Ordnung."}},
	} {
		t.Run(c.name, func(t *testing.T) {
			// The contract has to agree with the case: a type the API may send
			// would prove nothing here.
			if wire.ControlAllowed(wire.LinkTunnel, true, c.typ) {
				t.Fatalf("the contract lets the API send %q, so this test guards nothing", c.typ)
			}
			_, ended := running(t, &keeper{}, func(a *apiSide) {
				a.control(c.typ, c.body)
				a.quiet()
			})
			if err := waitFor(t, ended); !errors.Is(err, ErrLink) {
				t.Fatalf("Run = %v, want the link ended", err)
			}
		})
	}
}

// Nothing leaves this controller that the API would have to refuse: the body of
// every control message goes through the parser of its contract first.
func TestControlSendsNothingTheAPIMustRefuse(t *testing.T) {
	for _, c := range []struct {
		name string
		typ  string
		body any
	}{
		// A status with a negative amount of free disk space, an update for a
		// target that is none, a notice whose level the contract does not
		// know, and a goodbye that carries a body although it has none.
		{"status", wire.CtlStatus, tunnel.Status{DiskFree: -1}},
		{"update", wire.CtlUpdate, store.UpdateReport{Target: "Weder App noch Teil", State: store.UpdateInstalled}},
		{"notify", wire.CtlNotify, core.Notice{Level: "laut", Text: "Etwas ist geschehen."}},
		{"bye", wire.CtlBye, tunnel.Status{DiskFree: 1}},
		{"status without a body", wire.CtlStatus, nil},
		// A second hello: the contract lets a controller send one, and the API
		// would end the link over it all the same, so it stays here.
		{"a second hello", wire.CtlHello, tunnel.Hello{Version: "1.0.0", Contract: tunnel.Contract}},
	} {
		t.Run(c.name, func(t *testing.T) {
			sent := make(chan []byte, 1)
			tn, ended := running(t, nil, func(a *apiSide) {
				f, err := a.read()
				if err != nil {
					a.t.Errorf("read: %v", err)
					return
				}
				sent <- bytes.Clone(f.Payload)
				a.quiet()
			})
			if err := tn.Control(c.typ, c.body); !errors.Is(err, ErrConfig) {
				t.Errorf("Control(%s) = %v, want it refused", c.name, err)
			}
			// The next message is the one this side really may send: if the
			// refused one had gone out, it would be read here instead.
			if err := tn.Control(wire.CtlStatus, tunnel.Status{DiskFree: 17}); err != nil {
				t.Fatalf("Control(status): %v", err)
			}
			select {
			case p := <-sent:
				ctl, err := wire.ParseControl(p)
				if err != nil || ctl.T != wire.CtlStatus {
					t.Fatalf("the API read %q (%v), want the status", ctl.T, err)
				}
				s, err := tunnel.ParseStatus(ctl.B)
				if err != nil || s.DiskFree != 17 {
					t.Errorf("the API read %+v (%v), want the second status", s, err)
				}
			case <-time.After(30 * time.Second):
				t.Fatal("the controller never sent the message")
			}
			tn.Close()
			waitFor(t, ended)
		})
	}
}

// Every message a controller may send goes out with a body the API takes: what
// the parsers above refuse, these must let through.
func TestControlSendsWhatTheContractAllows(t *testing.T) {
	want := []struct {
		typ   string
		body  any
		check func([]byte) error
	}{
		{wire.CtlStatus, tunnel.Status{DiskFree: 1}, func(b []byte) error { _, err := tunnel.ParseStatus(b); return err }},
		{wire.CtlUpdate, store.UpdateReport{Target: trust.AutoAPI, Version: "1.1.0", State: store.UpdateInstalled},
			func(b []byte) error { _, err := store.ParseUpdateReport(b); return err }},
		{wire.CtlNotify, core.Notice{Level: core.NoticeWarn, Text: "Die Platte wird knapp."},
			func(b []byte) error { _, err := core.ParseNotice(b); return err }},
		{wire.CtlBye, nil, func(b []byte) error {
			if len(b) != 0 {
				return errors.New("the goodbye carries a body")
			}
			return nil
		}},
	}
	read := make(chan []byte, len(want))
	tn, ended := running(t, nil, func(a *apiSide) {
		for range want {
			f, err := a.read()
			if err != nil {
				return
			}
			read <- bytes.Clone(f.Payload)
		}
		a.quiet()
	})
	for i, w := range want {
		if err := tn.Control(w.typ, w.body); err != nil {
			t.Fatalf("Control(%s): %v", w.typ, err)
		}
		select {
		case p := <-read:
			ctl, err := wire.ParseControl(p)
			if err != nil || ctl.T != w.typ {
				t.Fatalf("message %d was %q (%v), want %q", i, ctl.T, err, w.typ)
			}
			if err := w.check(ctl.B); err != nil {
				t.Errorf("the API cannot take the %s: %v", w.typ, err)
			}
		case <-time.After(30 * time.Second):
			t.Fatalf("the controller never sent the %s", w.typ)
		}
	}
	tn.Close()
	waitFor(t, ended)
}

// The newer side speaks the version of the older one, and one version apart is
// as far as the two may be.
func TestTheNewerSideSpeaksTheOlderContract(t *testing.T) {
	for _, c := range []struct {
		ours, theirs, speaks int
		ok                   bool
	}{
		{1, 1, 1, true},
		{1, 2, 1, true},
		{2, 1, 1, true},
		{3, 3, 3, true},
		{1, 3, 0, false},
		{3, 1, 0, false},
		{1, 0, 0, false},
	} {
		got, ok := speak(c.ours, c.theirs)
		if got != c.speaks || ok != c.ok {
			t.Errorf("speak(%d, %d) = %d, %v; want %d, %v", c.ours, c.theirs, got, ok, c.speaks, c.ok)
		}
	}
}

// A tunnel that stands says which version it speaks, and that is the older of
// the two.
func TestTheTunnelSaysWhichContractItSpeaks(t *testing.T) {
	for name, contract := range map[string]int{
		"the same version":  tunnel.Contract,
		"one version newer": tunnel.Contract + 1,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := serve(t, func(w http.ResponseWriter, r *http.Request) {
				a := accept(t, w, r)
				if a == nil {
					return
				}
				greetWith(a, contract)
				a.quiet()
			})
			tn, err := Dial(context.Background(), cfg)
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			defer tn.Close()
			if tn.Speaks() != tunnel.Contract {
				t.Errorf("Speaks = %d, want %d", tn.Speaks(), tunnel.Contract)
			}
		})
	}
}

// More than one version apart there is nothing left to speak: the tunnel ends,
// and the admin reads in the log which of the two ends has to be brought up.
func TestATunnelEndsWhenTheContractsAreTooFarApart(t *testing.T) {
	var mu sync.Mutex
	var out bytes.Buffer
	cfg := serve(t, func(w http.ResponseWriter, r *http.Request) {
		a := accept(t, w, r)
		if a == nil {
			return
		}
		greetWith(a, tunnel.Contract+2)
		a.quiet()
	})
	cfg.Log = slog.New(slog.NewTextHandler(&lockedWriter{mu: &mu, w: &out}, nil))
	if _, err := Dial(context.Background(), cfg); !errors.Is(err, ErrLink) {
		t.Fatalf("Dial = %v, want the link ended", err)
	}
	mu.Lock()
	line := out.String()
	mu.Unlock()
	if !strings.Contains(line, msgAPINewer) {
		t.Errorf("the log says %q, want the German line about a newer API", line)
	}
}

// lockedWriter lets the log of a tunnel and the test read and write the same
// buffer: the goroutines of a tunnel go on logging while the test looks.
type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
