package serve

import (
	"sync"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The bound is of the SERVER and holds however many channels arrive at once.
// It is driven with far more goroutines than places, because a bound that is
// read and then raised lets two channels through on the last place and this is
// the shape that finds it.
func TestTheBoundOfAppChannelsHoldsUnderManyAtOnce(t *testing.T) {
	const (
		max   = 64
		tries = 512
	)
	s := New(Deps{MaxApps: max})
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		got int
	)
	start := make(chan struct{})
	for range tries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if s.takeApp() {
				mu.Lock()
				got++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	if got != max {
		t.Fatalf("%d channels got a place, want %d", got, max)
	}
	if s.AppChannels() != max {
		t.Fatalf("the server counts %d channels, want %d", s.AppChannels(), max)
	}
	if s.AppRefusals() != tries-max {
		t.Fatalf("%d channels were refused, want %d", s.AppRefusals(), tries-max)
	}
	// Every place comes back, and the counter is where it started.
	for range max {
		s.dropApp()
	}
	if s.AppChannels() != 0 {
		t.Fatalf("the server counts %d channels after every one ended", s.AppChannels())
	}
	// And the places are free again: the bound is of what runs now, not of
	// what ever ran.
	if !s.takeApp() {
		t.Fatal("no channel got a place after every earlier one ended")
	}
}

// The number this server holds to when nothing says otherwise is the one of
// this package, and a negative number is the way a case says "no bound".
func TestTheDefaultBoundIsTheOneOfThisPackage(t *testing.T) {
	if got := New(Deps{}).maxApps(); got != DefaultMaxApps {
		t.Fatalf("a server without a number bounds at %d, want %d", got, DefaultMaxApps)
	}
	if got := New(Deps{MaxApps: 7}).maxApps(); got != 7 {
		t.Fatalf("a server bounds at %d, want 7", got)
	}
	s := New(Deps{MaxApps: -1})
	for range DefaultMaxApps + 1 {
		if !s.takeApp() {
			t.Fatal("a server without a bound refused a channel")
		}
	}
	if s.AppRefusals() != 0 {
		t.Fatalf("a server without a bound refused %d channels", s.AppRefusals())
	}
}

// The bound reaches a real channel, and it reaches it BEFORE the end-to-end
// handshake: the CLOSE is the first frame of that channel, no credit was given
// for it, and this server paid no Noise operation for it either - which is
// what the handshake counter standing at 0 says.
func TestAnAppChannelPastTheBoundIsRefusedBeforeTheHandshake(t *testing.T) {
	w := newWorld(t)
	w.rebuild(func(d *Deps) { d.MaxApps = 1 })
	// The one channel that gets a place waits for the first inner message of
	// a shell that never sends one; the wait is run out in a fraction of a
	// second instead of the wire.E2EFirstTimeout of the contract.
	w.srv.first = 100 * time.Millisecond
	open1, _ := w.appOpen(t, nil)
	open2, _ := w.appOpen(t, nil)
	ended := w.tunnel(t, func(a *apiSide) {
		// The first app channel takes the only place: its first frame is the
		// GRANT that carries Noise message 2.
		if _, hs := a.open(1, open1); len(hs) == 0 {
			t.Error("the first app channel got no handshake in its GRANT")
		}
		// The second gets nothing but its CLOSE.
		a.send(wire.Frame{Type: wire.TypeOpen, Channel: 2, Payload: open2})
		f := a.want(wire.TypeClose, 2)
		cl, err := wire.ParseClose(f.Payload)
		if err != nil {
			t.Errorf("CLOSE of channel 2: %v", err)
		}
		wantCode(t, cl, wire.CodeLimit)
		a.shut(2)
		// The first channel runs its wait out and ends by itself.
		wantCode(t, a.closed(1), wire.CodeTimeout)
		a.shut(1)
		a.bye()
	})
	if err := waitFor(t, ended); err != nil {
		t.Fatalf("Run = %v, want an orderly end after bye", err)
	}
	if w.srv.AppRefusals() != 1 {
		t.Fatalf("%d channels were refused, want 1", w.srv.AppRefusals())
	}
	if w.srv.HandshakeFailures() != 0 {
		t.Fatalf("%d handshakes were paid for, want 0", w.srv.HandshakeFailures())
	}
	// The place of the first channel came back when it ended.
	if w.srv.AppChannels() != 0 {
		t.Fatalf("the server counts %d channels after both ended", w.srv.AppChannels())
	}
}
