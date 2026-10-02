package link

import (
	"testing"

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
	c.Allow(wire.MinWindow)
	if !c.Take(wire.MinWindow) {
		t.Fatal("our own window did not count")
	}
	if c.Take(1) {
		t.Fatal("more arrived than we granted")
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
