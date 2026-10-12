package relay

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// What these cases measure is the one number task 17 left open: how many
// goroutines a drain really holds, and for how long. Everything here is
// counted with runtime.NumGoroutine, never computed.
//
// -race says nothing about any of it: there is no C compiler on this machine,
// so the detector cannot run here at all. Counting goroutines works without
// it; the interleaving of the two directions stays unmeasured.

// drainChan is one forwarded channel of a drain measurement: its own shell
// (because fakeShell.channel always takes number 7) and its own hop number on
// the shared tunnel.
type drainChan struct {
	sh  *fakeShell
	c   *link.Channel
	num uint32
}

// drainGrant is the GRANT a controller sends on a given hop channel. The
// grant helper of the fixture is nailed to channel 1, and a drain measurement
// needs many channels at once.
func drainGrant(num uint32, credit uint32, hs []byte) wire.Frame {
	return wire.Frame{Type: wire.TypeGrant, Channel: num, Payload: wire.Grant(credit, hs)}
}

// drainOpenNum waits for the i-th OPEN on the hop (zero based) and answers its
// channel number. hop.await cannot do this: it answers the FIRST event of a
// kind, so from the second channel on it would hand back channel 1's OPEN and
// the case would grant on the wrong channel -- measured, that is exactly how
// the first run of this file failed.
func drainOpenNum(t *testing.T, hop *fakeHop, i int) uint32 {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		opens := []ev{}
		for _, e := range hop.events() {
			if e.kind == "open" {
				opens = append(opens, e)
			}
		}
		if len(opens) > i {
			return opens[i].num
		}
		select {
		case <-deadline:
			t.Fatalf("OPEN %d never arrived; %d are there", i, len(opens))
		case <-time.After(2 * time.Millisecond):
		}
	}
}

// drainStart opens n forwarded channels on one tunnel, lets each one get its
// first GRANT -- which is what starts the second goroutine, toController --
// and then ends each one with a protocol error the controller never answers,
// so every one of them enters the drain. It answers the channels and the
// channel that reports how many relays have returned.
//
// The second GRANT carries a handshake, and a handshake in any GRANT but the
// first is the protocol error of pump. That route is deliberate: the channel
// is already running with both goroutines when it fails, which is the state
// the open number is about. A failure on the FIRST grant would never start
// toController and would measure one goroutine instead of two.
func drainStart(t *testing.T, rl *Relay, n int) ([]drainChan, *fakeHop, chan wire.Code) {
	t.Helper()
	hop := newHop(wire.MaxTunnelChannels)
	chans := make([]drainChan, 0, n)
	done := make(chan wire.Code, n)
	for i := 0; i < n; i++ {
		sh := newShell()
		sh.noCredit = true
		c := sh.channel(t)
		fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
		go func() { done <- rl.ForwardRaw(context.Background(), sh, c, hop, fw) }()
		// The hop number this channel got. Open hands them out in order from
		// 1, and the case has to know it to send on the right one.
		num := drainOpenNum(t, hop, i)
		hop.send(drainGrant(num, wire.MinWindow, handshake()))
		sh.await(t, "grant")
		chans = append(chans, drainChan{sh: sh, c: c, num: num})
	}
	// Now break every one of them. Each relay sends its own CLOSE and then
	// waits for the controller's, which never comes.
	for _, dc := range chans {
		hop.send(drainGrant(dc.num, wire.MinWindow, handshake()))
	}
	return chans, hop, done
}

// drainSettle waits until the number of goroutines has stopped falling, so a
// baseline is not taken in the middle of someone else's teardown. It answers
// the number it settled on.
func drainSettle() int {
	last := runtime.NumGoroutine()
	for i := 0; i < 200; i++ {
		time.Sleep(10 * time.Millisecond)
		now := runtime.NumGoroutine()
		if now >= last {
			return now
		}
		last = now
	}
	return last
}

func TestDrainHoldsOneGoroutinePerChannelAndGivesThemBack(t *testing.T) {
	// The number task 17 left open. 128 channels, because that is the figure
	// point 29 of the contract notes settled on for app channels and the
	// question is whether this package needs one of its own.
	//
	// Three counts: before, at the peak while every channel is draining, and
	// after the drain. The third one is the one that matters: a peak that does
	// not fall again is a leak, and a leak is a different finding from a cost.
	const n = 128
	// Long enough that the peak is still standing when it is counted, short
	// enough that the case does not pay the real CloseDrain.
	rl := &Relay{Drain: 2 * time.Second}

	base := drainSettle()
	chans, hop, done := drainStart(t, rl, n)

	// The peak is only counted once every channel has sent its own CLOSE:
	// that is what proves all n really reached the drain, instead of the case
	// counting a moment when half of them were not there yet. A channel that
	// never got that far would make the peak look small and the fall look
	// clean, which is how this measurement could lie.
	_ = chans
	deadline := time.After(30 * time.Second)
	for {
		closes := 0
		for _, e := range hop.events() {
			if e.kind == "close" {
				closes++
			}
		}
		if closes >= n {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("only %d of %d channels reached the drain", closes, n)
		case <-time.After(5 * time.Millisecond):
		}
	}
	peak := runtime.NumGoroutine()

	// Count the hop numbers still held: a draining channel holds its number,
	// which is the cost point 29 was about.
	held := hop.tbl.Count()

	// Now let the drain run out. Every relay has to return.
	for i := 0; i < n; i++ {
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatalf("only %d of %d relays returned; %d goroutines stand", i, n, runtime.NumGoroutine())
		}
	}
	after := drainSettle()

	t.Logf("goroutines: before %d, peak %d (+%d for %d channels), after %d; hop numbers held at the peak %d",
		base, peak, peak-base, n, after, held)

	if peak-base < n {
		t.Fatalf("peak %d is only %d above the baseline %d, so %d channels did not all reach the drain",
			peak, peak-base, base, n)
	}
	// The fall is the finding. A margin of n/8 covers the runtime's own
	// goroutines coming and going; it does not cover a leak of one per channel.
	if after > base+n/8 {
		t.Fatalf("after the drain %d goroutines stand, baseline %d: %d did not come back",
			after, base, after-base)
	}
}

func TestDrainPeakLastsTheDrainAndTheCallerCanShortenIt(t *testing.T) {
	// The ten seconds are a decision, not a measurement. This case measures
	// how long the peak really stands, and that a caller can make it shorter.
	for _, d := range []time.Duration{200 * time.Millisecond, 800 * time.Millisecond} {
		rl := &Relay{Drain: d}
		_, _, done := drainStart(t, rl, 1)
		start := time.Now()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatalf("the relay never returned with Drain %v", d)
		}
		took := time.Since(start)
		t.Logf("Drain %v: the relay returned after %v", d, took)
		if took < d {
			t.Fatalf("with Drain %v the relay returned after %v, so the wait was not paid", d, took)
		}
		if took > d+5*time.Second {
			t.Fatalf("with Drain %v the relay took %v", d, took)
		}
	}
}

func TestDrainEndsAtOnceOnACancelledContext(t *testing.T) {
	// endHop derives its clock from the caller's context, so a context that
	// is already over must end the wait at once instead of paying the drain.
	// The drain is set far above the measured bound, so a case that passes
	// cannot be passing because the clock ran out.
	rl := &Relay{Drain: 30 * time.Second}
	hop := newHop(wire.MaxTunnelChannels)
	sh := newShell()
	sh.noCredit = true
	c := sh.channel(t)
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan wire.Code, 1)
	go func() { done <- rl.ForwardRaw(ctx, sh, c, hop, fw) }()
	ev := hop.await(t, "open")
	hop.send(drainGrant(ev.num, wire.MinWindow, handshake()))
	sh.await(t, "grant")
	// Cancel first, then break the channel: the relay reaches the drain with a
	// context that is already done.
	cancel()
	hop.send(drainGrant(ev.num, wire.MinWindow, handshake()))
	start := time.Now()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("the relay was still draining 10 s after the context was cancelled, Drain is %v", rl.Drain)
	}
	took := time.Since(start)
	t.Logf("cancelled context: the relay returned after %v, Drain was %v", took, rl.Drain)
	if took > 2*time.Second {
		t.Fatalf("a cancelled context took %v to end the drain", took)
	}
}

func TestAForwardedChannelHasNoIdleBound(t *testing.T) {
	// The claim of task 17: the quiet time of a forwarded channel has no
	// bound. The case holds it from this side: a channel that has had its
	// first GRANT and then hears nothing from either side is still standing
	// well past the longest clock this package has that could end it.
	//
	// It cannot measure "forever" -- no case can. What it measures is that no
	// clock of this package ends it, and the figure it runs for is chosen
	// against the two that could be mistaken for one: Drain and Wait are set
	// to 50 ms here, so if either of them reached a silent channel the relay
	// would be gone long before the case looks.
	rl := &Relay{Drain: 50 * time.Millisecond, Wait: 50 * time.Millisecond}
	hop := newHop(wire.MaxTunnelChannels)
	sh := newShell()
	sh.noCredit = true
	c := sh.channel(t)
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	base := drainSettle()
	done := make(chan wire.Code, 1)
	go func() { done <- rl.ForwardRaw(context.Background(), sh, c, hop, fw) }()
	ev := hop.await(t, "open")
	hop.send(drainGrant(ev.num, wire.MinWindow, handshake()))
	sh.await(t, "grant")
	// Not one further frame from either side.
	const quiet = 3 * time.Second
	select {
	case code := <-done:
		t.Fatalf("a silent channel ended on its own with %q after less than %v", code, quiet)
	case <-time.After(quiet):
	}
	// Still standing, still holding its hop number.
	if n := hop.tbl.Count(); n != 1 {
		t.Fatalf("hop numbers held %d, want 1", n)
	}
	// What a RUNNING forwarded channel costs, as opposed to a draining one:
	// both directions stand, so two goroutines instead of one.
	running := runtime.NumGoroutine()
	t.Logf("a silent forwarded channel was still open after %v with Drain and Wait at 50 ms; "+
		"nothing in this package ends it. goroutines: before %d, while it stands %d (+%d)",
		quiet, base, running, running-base)
	if running-base != 2 {
		t.Fatalf("a running forwarded channel holds %d goroutines, want 2", running-base)
	}
}
