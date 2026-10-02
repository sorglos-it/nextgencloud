package link

import (
	"fmt"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/wire"
)

func TestChannelFreesItsNumberOnlyAfterBothCloses(t *testing.T) {
	tb := NewTable(2, false)
	c, err := tb.Open(1, wire.MinWindow)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if tb.Count() != 1 {
		t.Fatalf("count %d", tb.Count())
	}
	c.SentClose()
	if !c.Closing() || tb.Count() != 1 {
		t.Fatalf("closing %v count %d", c.Closing(), tb.Count())
	}
	// A frame for a closing number is dropped, not answered.
	if _, ok := tb.Get(1); !ok {
		t.Fatal("the number is gone too early")
	}
	c.RecvClose()
	if tb.Count() != 0 {
		t.Fatalf("count after both closes %d", tb.Count())
	}
	if _, ok := tb.Get(1); ok {
		t.Fatal("the number is still there")
	}
	// The number may be used again once it is free.
	again, err := tb.Open(1, wire.MinWindow)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	// The old channel must not free the new number: a late CLOSE for the
	// channel that is gone would otherwise take the one that took its place.
	c.RecvClose()
	c.SentClose()
	if _, ok := tb.Get(1); !ok {
		t.Fatal("a closed channel freed the number of its successor")
	}
	// The other order holds the same rule, and only this way round catches a
	// RecvClose that frees on its own: the peer closes first, we have not
	// answered yet, so the number is still ours.
	again.RecvClose()
	if tb.Count() != 1 {
		t.Fatalf("the peer's CLOSE alone freed the number, count %d", tb.Count())
	}
	if _, ok := tb.Get(1); !ok {
		t.Fatal("the number is gone before we answered the CLOSE")
	}
	again.SentClose()
	if tb.Count() != 0 {
		t.Fatalf("count after the successor closed %d", tb.Count())
	}
}

func TestOpenRefusesADoubleNumberAndTheLimit(t *testing.T) {
	tb := NewTable(2, false)
	if _, err := tb.Open(1, wire.MinWindow); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Open(1, wire.MinWindow); err != ErrProtocol {
		t.Fatalf("the same number twice answered %v", err)
	}
	if _, err := tb.Open(wire.ControlChannel, wire.MinWindow); err != ErrProtocol {
		t.Fatal("channel 0 was opened")
	}
	// A window outside MinWindow..MaxWindow is a protocol error, not a limit.
	if _, err := tb.Open(2, wire.MinWindow-1); err != ErrProtocol {
		t.Fatalf("a window below MinWindow answered %v", err)
	}
	if _, err := tb.Open(2, wire.MaxWindow+1); err != ErrProtocol {
		t.Fatalf("a window above MaxWindow answered %v", err)
	}
	if _, err := tb.Open(2, wire.MinWindow); err != nil {
		t.Fatalf("second: %v", err)
	}
	if _, err := tb.Open(3, wire.MinWindow); err != ErrLimit {
		t.Fatalf("beyond the limit answered %v", err)
	}
	// The side that opens channels never accepts an OPEN.
	if _, err := NewTable(2, true).Open(1, wire.MinWindow); err != ErrProtocol {
		t.Fatal("the opener accepted an OPEN")
	}
}

func TestCredit(t *testing.T) {
	tb := NewTable(4, false)
	c, err := tb.Open(1, wire.MinWindow)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// The other side granted MinWindow with its OPEN: that is what we may send.
	if !c.SendAllowed(wire.MinWindow) || c.SendAllowed(wire.MinWindow+1) {
		t.Fatal("wrong allowance after the OPEN")
	}
	if !c.Spend(wire.MinWindow) || c.Spend(1) {
		t.Fatal("spending beyond the allowance")
	}
	if !c.Grant(wire.MinWindow) || !c.Spend(wire.MinWindow) {
		t.Fatal("a grant did not add")
	}
	// A grant that would lift the allowance above MaxWindow is a protocol error.
	if !c.Grant(wire.MaxWindow) {
		t.Fatal("a grant up to MaxWindow was refused")
	}
	if c.Grant(1) {
		t.Fatal("a grant beyond MaxWindow was taken")
	}
	// A GRANT of nothing is no GRANT; wire refuses it before we see it.
	if c.Grant(0) {
		t.Fatal("a grant of zero was taken")
	}
	// The other direction: an OPEN that arrived grants us nothing, so nothing
	// may arrive before our own first GRANT.
	if c.Take(1) {
		t.Fatal("data arrived before we granted anything")
	}
	if !c.Allow(wire.MinWindow) {
		t.Fatal("our own first GRANT was refused")
	}
	if !c.Take(wire.MinWindow) {
		t.Fatal("our own window did not count")
	}
	if c.Take(1) {
		t.Fatal("more arrived than we granted")
	}
	// MaxWindow bounds both directions, so Allow refuses what Grant refuses. It
	// has to, or Take holds nothing: the side that counts what may arrive is the
	// same side that raises the figure.
	if c.Allow(0) {
		t.Fatal("a grant of zero was sent")
	}
	if !c.Allow(wire.MaxWindow) {
		t.Fatal("granting up to MaxWindow was refused")
	}
	if c.Allow(1) {
		t.Fatal("we granted more than MaxWindow")
	}
	if c.Take(wire.MaxWindow + 1) {
		t.Fatal("more than MaxWindow may arrive at once")
	}
	if !c.Take(wire.MaxWindow) {
		t.Fatal("MaxWindow did not arrive")
	}
}

func TestNextWrapsPastTheLastNumber(t *testing.T) {
	tb := NewTable(4, true)
	a, err := tb.Next(wire.MinWindow)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, err := tb.Next(wire.MinWindow)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	// The last number of the range, so that the next call has to wrap. Reached
	// from the inside, because a link would need four billion channels for it.
	tb.next = ^uint32(0)
	last, err := tb.Next(wire.MinWindow)
	if err != nil || last.Num != ^uint32(0) {
		t.Fatalf("the last number came back as %v: %v", last, err)
	}
	after, err := tb.Next(wire.MinWindow)
	if err != nil {
		t.Fatalf("after the wrap: %v", err)
	}
	// Channel 0 is no channel, so the wrap must not hand it out.
	if after.Num == wire.ControlChannel {
		t.Fatal("the wrap handed out channel 0")
	}
	// And it must not take a number that is still open: that would replace a
	// live channel, and neither of the two would ever free the number again.
	if after.Num == a.Num || after.Num == b.Num {
		t.Fatalf("the wrap took number %d, which is still open", after.Num)
	}
	if got, ok := tb.Get(a.Num); !ok || got != a {
		t.Fatal("an open channel was replaced after the wrap")
	}
	if tb.Count() != 4 {
		t.Fatalf("count %d", tb.Count())
	}
}

func TestATableWithoutRoomCarriesNothing(t *testing.T) {
	// A forgotten limit must refuse channels, never grant them without end -- and
	// which refusal comes back depends on the call, not on max, because the
	// checks that do not look at the table answer first. The case therefore asks
	// every call, not only the two whose answer is ErrLimit.
	for _, max := range []int{0, -1} {
		accept, open := NewTable(max, false), NewTable(max, true)
		// Past the checks that answer without the table: here max decides.
		if _, err := accept.Open(1, wire.MinWindow); err != ErrLimit {
			t.Errorf("NewTable(%d, false).Open(1) answered %v, want ErrLimit", max, err)
		}
		if err := accept.Reserve(1); err != ErrLimit {
			t.Errorf("NewTable(%d, false).Reserve(1) answered %v, want ErrLimit", max, err)
		}
		// Next refuses too, but this arm does not hold the limit check of Next:
		// at max <= 0 the loop over the candidates runs no round either, so
		// ErrLimit comes back even without that check -- measured, a mutant on
		// that one line leaves this case green. What holds it is
		// TestNextKeepsTheLimit, where the table is full and the loop would run.
		// This arm holds the answer a caller sees, and that is all it claims.
		if _, err := open.Next(wire.MinWindow); err != ErrLimit {
			t.Errorf("NewTable(%d, true).Next answered %v, want ErrLimit", max, err)
		}
		// And the refusals that are not about room stay what they are, so that
		// an empty table cannot turn a protocol error into a limit.
		if _, err := accept.Open(wire.ControlChannel, wire.MinWindow); err != ErrProtocol {
			t.Errorf("NewTable(%d, false).Open(0) answered %v, want ErrProtocol", max, err)
		}
		if _, err := accept.Open(1, wire.MinWindow-1); err != ErrProtocol {
			t.Errorf("NewTable(%d, false).Open with a small window answered %v, want ErrProtocol", max, err)
		}
		if _, err := open.Open(1, wire.MinWindow); err != ErrProtocol {
			t.Errorf("NewTable(%d, true).Open answered %v, want ErrProtocol", max, err)
		}
		if err := accept.Reserve(wire.ControlChannel); err != ErrProtocol {
			t.Errorf("NewTable(%d, false).Reserve(0) answered %v, want ErrProtocol", max, err)
		}
		if _, err := accept.Next(wire.MinWindow); err != ErrProtocol {
			t.Errorf("NewTable(%d, false).Next answered %v, want ErrProtocol", max, err)
		}
		if accept.Count() != 0 || open.Count() != 0 {
			t.Errorf("a refused call took a number: %d / %d", accept.Count(), open.Count())
		}
	}
}

func TestTheCountersHoldUnderTwoGoroutines(t *testing.T) {
	// A link has one reader and one writer on the same channel, so every counter
	// is touched from two goroutines. Without this case -race has nothing to
	// look at here, and the locks are a claim, not a measurement. Named one by
	// one, because "every" is only true if the case really drives them all:
	// out through Grant against Spend, in through Allow against Take, and the
	// two close flags through SentClose against RecvClose. Measured with the
	// lock of Grant removed: four DATA RACE between Grant and Spend.
	//
	// All of it runs in a goroutine of its own, and the case itself only waits
	// for the answer with a deadline. That shape is the point: the plausible
	// mistake is not losing both lock lines, it is keeping Lock and losing
	// Unlock, and then every call on this channel blocks -- including the ones
	// the waiting code would make. A deadline that has to be reached through
	// Spend can never be reached. And a case that blocks instead of failing is
	// worse than a wrong one: the timeout panic of go test ends the whole
	// package run and takes the answer of the other cases with it.
	tb := NewTable(2, false)
	c, err := tb.Open(1, wire.MinWindow)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	const rounds = 1000
	const total = rounds + wire.MinWindow

	fault := make(chan string, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		fail := func(format string, a ...any) {
			select {
			case fault <- fmt.Sprintf(format, a...):
			default:
			}
		}
		// out: the reading side grants, the writing side spends what it got.
		grants := make(chan struct{})
		go func() {
			defer close(grants)
			for i := 0; i < rounds; i++ {
				c.Grant(1)
			}
		}()
		spent := 0
		for spent < total {
			if c.Spend(1) {
				spent++
				continue
			}
			<-grants
			if !c.Spend(1) {
				fail("spent %d of %d after the grants were in", spent, total)
				return
			}
			spent++
		}
		<-grants
		if c.SendAllowed(1) {
			fail("allowance left over after everything granted was spent")
			return
		}
		// in: the other direction, which had no second goroutine either.
		allows := make(chan struct{})
		go func() {
			defer close(allows)
			for i := 0; i < rounds; i++ {
				c.Allow(1)
			}
		}()
		took := 0
		for took < rounds {
			if c.Take(1) {
				took++
				continue
			}
			<-allows
			if !c.Take(1) {
				fail("took %d of %d after our own grants were in", took, rounds)
				return
			}
			took++
		}
		<-allows
		if c.Take(1) {
			fail("more may arrive than everything we granted")
			return
		}
		// Both endings at once must still free the number exactly once.
		ends := make(chan struct{}, 2)
		go func() { c.SentClose(); ends <- struct{}{} }()
		go func() { c.RecvClose(); ends <- struct{}{} }()
		<-ends
		<-ends
		if n := tb.Count(); n != 0 {
			fail("count after both endings %d", n)
		}
	}()

	select {
	case <-finished:
		select {
		case msg := <-fault:
			t.Fatal(msg)
		default:
		}
	case <-time.After(30 * time.Second):
		// A counter whose lock is broken does not answer wrongly, it does not
		// answer at all, so this is the only way this case can see it.
		t.Fatal("the counters never came back; a lock is held and never given up")
	}
}

func TestNextPicksFreeNumbers(t *testing.T) {
	tb := NewTable(wire.MaxTunnelChannels, true)
	first, err := tb.Next(wire.MinWindow)
	if err != nil || first.Num != 1 {
		t.Fatalf("first %v: %v", first, err)
	}
	second, err := tb.Next(wire.MinWindow)
	if err != nil || second.Num != 2 {
		t.Fatalf("second %v: %v", second, err)
	}
	// What we grant with our own OPEN bounds what may arrive, and we may send
	// nothing until the other side's first GRANT.
	if !second.Take(wire.MinWindow) || second.Take(1) {
		t.Fatal("our own OPEN window did not bound what may arrive")
	}
	if second.SendAllowed(1) {
		t.Fatal("we may send before the first GRANT of the other side")
	}
	first.SentClose()
	first.RecvClose()
	third, err := tb.Next(wire.MinWindow)
	if err != nil || third.Num == second.Num {
		t.Fatalf("third %v: %v", third, err)
	}
	// The side that does not open channels never picks a number.
	if _, err := NewTable(2, false).Next(wire.MinWindow); err != ErrProtocol {
		t.Fatal("the accepting side picked a number")
	}
}

func TestNextKeepsTheLimit(t *testing.T) {
	tb := NewTable(2, true)
	a, err := tb.Next(wire.MinWindow)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := tb.Next(wire.MinWindow); err != nil {
		t.Fatalf("second: %v", err)
	}
	if _, err := tb.Next(wire.MinWindow); err != ErrLimit {
		t.Fatalf("third answered %v", err)
	}
	a.SentClose()
	a.RecvClose()
	if _, err := tb.Next(wire.MinWindow); err != nil {
		t.Fatalf("after a number came free: %v", err)
	}
}

func TestReserveHoldsTheNumberOfAProtocolError(t *testing.T) {
	tb := NewTable(2, false)
	// Channel 0 is no number of this table, so a protocol error on it ends the
	// link; reserving it would have held a number that cannot exist, while Open
	// refuses the same number.
	if err := tb.Reserve(wire.ControlChannel); err != ErrProtocol {
		t.Fatalf("channel 0 was reserved: %v", err)
	}
	if tb.Count() != 0 {
		t.Fatalf("the refused channel 0 still took a number, count %d", tb.Count())
	}
	if err := tb.Reserve(7); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	// It counts toward the limit until the other side's CLOSE arrives.
	if tb.Count() != 1 {
		t.Fatalf("count %d", tb.Count())
	}
	c, ok := tb.Get(7)
	if !ok {
		t.Fatal("the reserved number is not there")
	}
	// Reserving twice is no error and does not count twice.
	if err := tb.Reserve(7); err != nil {
		t.Fatalf("reserve again: %v", err)
	}
	if tb.Count() != 1 {
		t.Fatalf("count after reserving twice %d", tb.Count())
	}
	// Nothing may be sent or arrive on a number we only answered with CLOSE.
	if c.SendAllowed(1) || c.Take(1) {
		t.Fatal("a reserved number carries credit")
	}
	c.SentClose()
	c.RecvClose()
	if tb.Count() != 0 {
		t.Fatalf("count after both closes %d", tb.Count())
	}
	// A stray frame that would make one number too many ends the link.
	if err := tb.Reserve(1); err != nil {
		t.Fatalf("reserve 1: %v", err)
	}
	if err := tb.Reserve(2); err != nil {
		t.Fatalf("reserve 2: %v", err)
	}
	if err := tb.Reserve(3); err != ErrLimit {
		t.Fatalf("beyond the limit answered %v", err)
	}
}
