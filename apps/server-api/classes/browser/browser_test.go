package browser

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

func TestSocketNeedsASession(t *testing.T) {
	h := newServer(t)
	// Without a cookie the upgrade never happens: 401 with the body of the
	// contract.
	w, r := socketRequest("")
	h.Socket(w, r)
	if w.Code != 401 {
		t.Fatalf("status %d", w.Code)
	}
	var c wire.Close
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("body %q: %v", w.Body, err)
	}
	if c.Code != wire.CodeLogin {
		t.Fatalf("code %q", c.Code)
	}
}

func TestSocketCarriesAnOpenAndAnswers(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	f := c.read(t)
	if f.Type != wire.TypeGrant || f.Channel != 1 {
		t.Fatalf("frame %+v", f)
	}
}

func TestSocketEndsOnAForbiddenControlMessage(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	// Only "bye" may come from the browser; anything else ends the link.
	c.writeControl(t, wire.CtlStatus, []byte("{}"))
	wantEnded(t, c)
}

func TestSocketEndsOnBye(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeControl(t, wire.CtlBye, nil)
	wantEnded(t, c)
}

func TestSocketEndsBeyondTheChannelLimit(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	open := openOf()
	for i := 1; i <= wire.MaxSessionChannels+1; i++ {
		// The write of the frame that ends the link may itself fail, which is
		// not the finding: the finding is that the link ended.
		if err := c.writeErr(wire.Frame{Type: wire.TypeOpen, Channel: uint32(i),
			Payload: openPayload(t, open)}); err != nil {
			break
		}
	}
	wantEnded(t, c)
}

// openPayload is the JSON of an OPEN.
func openPayload(t *testing.T, o wire.Open) []byte {
	t.Helper()
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return b
}

// --- the three conditions link.Table puts on its caller ---

// TestGrantRaisesWhatMayArriveBeforeItGoesOut holds the first sentence by its
// effect: the browser sees the GRANT and may at once send exactly that many
// bytes, and one byte more closes the channel. Had the GRANT gone out without
// Allow, nothing would have been allowed and the first DATA frame would have
// been the protocol error.
func TestGrantRaisesWhatMayArriveBeforeItGoesOut(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	f := c.read(t)
	credit, handshake, err := wire.ParseGrant(f.Payload)
	if f.Type != wire.TypeGrant || err != nil || len(handshake) != 0 {
		t.Fatalf("frame %+v credit %d handshake %d err %v", f, credit, len(handshake), err)
	}
	if credit != wire.MinWindow {
		t.Fatalf("credit %d", credit)
	}
	c.write(t, wire.Frame{Type: wire.TypeData, Channel: 1, Payload: make([]byte, credit)})
	// One byte beyond the allowance is a protocol error for that channel.
	c.write(t, wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte{0}})
	wantClose(t, c, 1, wire.CodeProtocol)
}

// TestEveryGrantGoesThroughSendGrant is the loop behind the word "every". The
// first sentence of the entry condition is a claim about every GRANT this
// package writes, and no single case can see them all, so this one reads the
// package's own source: a GRANT frame is built in exactly one function, and that
// function is the one that calls Allow first.
func TestEveryGrantGoesThroughSendGrant(t *testing.T) {
	writers := funcsContaining(t, ".", "Type: wire.TypeGrant")
	if len(writers) != 1 || writers[0] != "SendGrant" {
		t.Fatalf("GRANT frames are written in %v, not in SendGrant alone", writers)
	}
	allow := funcsContaining(t, ".", ".Allow(")
	if len(allow) != 1 || allow[0] != "SendGrant" {
		t.Fatalf("Allow is called in %v, not in SendGrant alone", allow)
	}
	body := funcBody(t, ".", "SendGrant")
	iAllow := strings.Index(body, ".Allow(")
	iWrite := strings.Index(body, "Type: wire.TypeGrant")
	if iAllow < 0 || iWrite < 0 || iAllow > iWrite {
		t.Fatalf("SendGrant builds the frame before it asks Allow (%d, %d)", iAllow, iWrite)
	}
}

// TestNoGrantGoesOutWhenAllowSaysNo holds the second sentence: Allow says false
// for a credit of zero and for one that would lift the allowance above
// wire.MaxWindow, and in both cases nothing reaches the browser. A GRANT that
// did reach it would be answered with CLOSE E_PROTOCOL -- our own protocol
// error.
func TestNoGrantGoesOutWhenAllowSaysNo(t *testing.T) {
	h := newServer(t)
	grantNothing(t, h)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	o := firstOpened(t, h)
	if err := o.s.SendGrant(ctx, o.c, 0); !errors.Is(err, ErrGrant) {
		t.Fatalf("a credit of zero answered %v", err)
	}
	if err := o.s.SendGrant(ctx, o.c, wire.MaxWindow+1); !errors.Is(err, ErrGrant) {
		t.Fatalf("a credit above the window answered %v", err)
	}
	// The next frame the browser sees must be the first GRANT that was allowed,
	// not one of the two that were refused.
	if err := o.s.SendGrant(ctx, o.c, wire.MinWindow); err != nil {
		t.Fatalf("a credit inside the window: %v", err)
	}
	f := c.read(t)
	if f.Type != wire.TypeGrant || f.Channel != 1 || !bytes.Equal(f.Payload, wire.Grant(wire.MinWindow, nil)) {
		t.Fatalf("frame %+v payload %v", f, f.Payload)
	}
}

// TestEveryReserveIsFollowedBySentClose is the loop behind the third sentence.
// Reserve holds a number until the other side's CLOSE arrives, and a Reserve
// without a SentClose would hold it for the life of the link; no single case can
// see every Reserve, so this one reads the source: one function reserves, it
// sends the CLOSE in the same breath, and SentClose is noted in that one place.
func TestEveryReserveIsFollowedBySentClose(t *testing.T) {
	reserve := funcsContaining(t, ".", ".Reserve(")
	if len(reserve) != 1 || reserve[0] != "protocolError" {
		t.Fatalf("Reserve is called in %v, not in protocolError alone", reserve)
	}
	if body := funcBody(t, ".", "protocolError"); !strings.Contains(body, "s.Close(ctx,") {
		t.Fatal("protocolError reserves a number without sending the CLOSE that frees it again")
	}
	sent := funcsContaining(t, ".", ".SentClose()")
	if len(sent) != 1 || sent[0] != "Close" {
		t.Fatalf("SentClose is called in %v, not in Close alone", sent)
	}
}

// TestProtocolErrorGivesTheNumberBackAfterTheBrowsersClose holds the third
// sentence by its effect. The number of a stray frame counts toward the limit
// while the CLOSE E_PROTOCOL is unanswered, is answered exactly once however
// many strays follow, and is free again once the browser's CLOSE arrives --
// which only happens because SentClose was noted beside RecvClose.
func TestProtocolErrorGivesTheNumberBackAfterTheBrowsersClose(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	o := firstOpened(t, h)
	if f := c.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("frame %+v", f)
	}
	// A DATA frame for a number that is neither open nor closing.
	c.write(t, wire.Frame{Type: wire.TypeData, Channel: 2, Payload: []byte("x")})
	wantClose(t, c, 2, wire.CodeProtocol)
	if n := o.s.Table.Count(); n != 2 {
		t.Fatalf("the reserved number does not count: %d", n)
	}
	// A second stray for the same number is dropped without an answer, so the
	// next frame is the GRANT of channel 3 and not a second CLOSE.
	c.write(t, wire.Frame{Type: wire.TypeData, Channel: 2, Payload: []byte("x")})
	c.writeOpen(t, 3, openOf())
	if f := c.read(t); f.Type != wire.TypeGrant || f.Channel != 3 {
		t.Fatalf("a second stray was answered: %+v", f)
	}
	// Now the browser's CLOSE. Channel 4 after it says when the reader is past
	// it, so nothing here waits on a clock.
	body, err := json.Marshal(wire.Error(wire.CodeProtocol))
	if err != nil {
		t.Fatalf("close body: %v", err)
	}
	c.write(t, wire.Frame{Type: wire.TypeClose, Channel: 2, Payload: body})
	c.writeOpen(t, 4, openOf())
	<-h.Opened // channel 3
	<-h.Opened // channel 4
	if _, ok := o.s.Table.Get(2); ok {
		t.Fatal("the number is still held after both CLOSEs")
	}
	if n := o.s.Table.Count(); n != 3 {
		t.Fatalf("open numbers %d, want 1, 3 and 4", n)
	}
}

// --- what the browser may not do on a channel ---

func TestGrantFromTheBrowserMayCarryNoHandshake(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	if f := c.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("frame %+v", f)
	}
	// Noise message 2 travels only in the first GRANT of the side that accepted
	// the channel, never from the opener.
	c.write(t, wire.Frame{Type: wire.TypeGrant, Channel: 1,
		Payload: wire.Grant(wire.MinWindow, make([]byte, wire.HandshakeSize))})
	wantClose(t, c, 1, wire.CodeProtocol)
}

func TestGrantFromTheBrowserMayNotLiftAboveTheWindow(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	if f := c.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("frame %+v", f)
	}
	// The OPEN already granted MinWindow, so the whole window on top of it is
	// one too many.
	c.write(t, wire.Frame{Type: wire.TypeGrant, Channel: 1, Payload: wire.Grant(wire.MaxWindow, nil)})
	wantClose(t, c, 1, wire.CodeProtocol)
}

func TestOpenForANumberThatIsOpenIsAProtocolError(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	if f := c.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("frame %+v", f)
	}
	c.write(t, wire.Frame{Type: wire.TypeOpen, Channel: 1, Payload: openPayload(t, openOf())})
	wantClose(t, c, 1, wire.CodeProtocol)
}

// TestUnknownFrameTypeOnAChannelIsAProtocolError calls feed directly, because no
// browser can reach that arm: wire.Frame.check refuses every type outside 1..5,
// so wire.Decode never hands one up and link.Read cannot produce the frame this
// case builds. The arm stays all the same -- feed must not depend on another
// package's list of types staying what it is -- and the CLOSE is what says it
// spoke.
func TestUnknownFrameTypeOnAChannelIsAProtocolError(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	o := firstOpened(t, h)
	if f := c.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("frame %+v", f)
	}
	f := wire.Frame{Type: wire.Type(99), Channel: 1}
	if _, err := f.Encode(); err == nil {
		t.Fatal("a frame of an unknown type can be encoded after all")
	}
	// The channel is closed, the link lives on, so feed says true.
	if !o.s.feed(ctx, o.c, f) {
		t.Fatal("an unknown frame type ended the whole link")
	}
	wantClose(t, c, 1, wire.CodeProtocol)
}

func TestOpenWithABrokenPayloadIsAProtocolError(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.write(t, wire.Frame{Type: wire.TypeOpen, Channel: 1, Payload: []byte("{")})
	wantClose(t, c, 1, wire.CodeProtocol)
}

// TestControlChannelTakesOnlyData holds the type check of channel 0, and it
// calls control directly because no browser can reach it: wire.Frame.check
// refuses OPEN, EOF, CLOSE and GRANT on channel 0 outright, so such a frame
// cannot be encoded and link.Read never hands one up. Measured: Encode answers
// wire: malformed frame. The check stays all the same -- it is this package's
// own statement that channel 0 carries control messages as DATA and nothing
// else, and it must not depend on a bound of another package staying where it
// is -- and this case is the only caller that can make it speak.
func TestControlChannelTakesOnlyData(t *testing.T) {
	h := newServer(t)
	for _, typ := range []wire.Type{wire.TypeOpen, wire.TypeEOF, wire.TypeClose, wire.TypeGrant} {
		f := wire.Frame{Type: typ, Channel: wire.ControlChannel}
		if _, err := f.Encode(); err == nil {
			t.Fatalf("type %d on channel 0 can be encoded after all", typ)
		}
		if h.Srv.control(ctx, &Session{}, f) {
			t.Fatalf("type %d was taken on channel 0", typ)
		}
	}
}

// --- the registry ---

func TestRegistryHoldsASessionWhileItRuns(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	c.writeOpen(t, 1, openOf())
	firstOpened(t, h)
	if n := h.Srv.Sessions.Count(); n != 1 {
		t.Fatalf("open sessions %d", n)
	}
	// A notice reaches the open shell through the registry.
	h.Srv.Sessions.Broadcast(ctx, wire.CtlNotify, core.Notice{Level: "info", Text: "Hallo"})
	var got wire.Control
	for {
		f := c.read(t)
		if f.Channel != wire.ControlChannel {
			continue
		}
		if err := json.Unmarshal(f.Payload, &got); err != nil {
			t.Fatalf("control %q: %v", f.Payload, err)
		}
		break
	}
	if got.T != wire.CtlNotify {
		t.Fatalf("control %q", got.T)
	}
	c.close()
	// The link ends and the session is forgotten. The waiting part runs in a
	// goroutine of its own and the case only waits: Count takes the registry's
	// lock, so a lock that is never given back would block the case itself, and
	// a deadline behind the blocking call is never reached (measured on the
	// mutant that forgets the Unlock: the whole package ran into its timeout).
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		for h.Srv.Sessions.Count() != 0 {
			time.Sleep(time.Millisecond)
		}
	}()
	select {
	case <-gone:
	case <-time.After(readWait):
		t.Fatal("the session is still in the registry, or the registry no longer gives its lock back")
	}
}

func TestRegistryAddsAndRemoves(t *testing.T) {
	r := NewRegistry()
	a, b := &Session{}, &Session{}
	r.Add(a)
	r.Add(b)
	if n := r.Count(); n != 2 {
		t.Fatalf("count %d", n)
	}
	seen := 0
	r.Each(func(*Session) { seen++ })
	if seen != 2 {
		t.Fatalf("Each saw %d", seen)
	}
	r.Remove(a)
	if n := r.Count(); n != 1 {
		t.Fatalf("count after remove %d", n)
	}
}

// --- helpers of the cases ---

// wantClose reads until a CLOSE for a channel arrives and checks its code.
// Frames of other channels in between are the GRANTs of channels this case
// opened and are skipped.
func wantClose(t *testing.T, p *peer, num uint32, code wire.Code) {
	t.Helper()
	for {
		f, err := p.readErr()
		if err != nil {
			t.Fatalf("no CLOSE for channel %d: %v", num, err)
		}
		if f.Channel != num {
			continue
		}
		if f.Type != wire.TypeClose {
			t.Fatalf("channel %d answered %+v", num, f)
		}
		c, err := wire.ParseClose(f.Payload)
		if err != nil {
			t.Fatalf("close payload: %v", err)
		}
		if c.Code != code {
			t.Fatalf("code %q, want %q", c.Code, code)
		}
		return
	}
}

// sources returns the package's own source, test files left out.
func sources(t *testing.T, dir string) map[string]string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	out := map[string]string{}
	for _, n := range names {
		if strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		out[n] = string(b)
	}
	if len(out) == 0 {
		t.Fatal("no source of this package was read")
	}
	return out
}

var funcLine = regexp.MustCompile(`^func (?:\([^)]*\) )?([A-Za-z_][A-Za-z0-9_]*)`)

// funcsContaining names every top-level function of this package whose body
// holds needle, comments left out. It is how a case can hold a sentence about
// "every" place in the package instead of about one place it happened to look
// at.
func funcsContaining(t *testing.T, dir, needle string) []string {
	t.Helper()
	var found []string
	for _, src := range sources(t, dir) {
		cur := ""
		for _, line := range strings.Split(src, "\n") {
			if m := funcLine.FindStringSubmatch(line); m != nil {
				cur = m[1]
			}
			code := line
			if i := strings.Index(code, "//"); i >= 0 {
				code = code[:i]
			}
			if cur != "" && strings.Contains(code, needle) &&
				!contains(found, cur) {
				found = append(found, cur)
			}
		}
	}
	return found
}

// funcBody returns the code of one top-level function, comments left out.
func funcBody(t *testing.T, dir, name string) string {
	t.Helper()
	for _, src := range sources(t, dir) {
		cur, keep := "", false
		var b strings.Builder
		for _, line := range strings.Split(src, "\n") {
			if m := funcLine.FindStringSubmatch(line); m != nil {
				cur = m[1]
				keep = cur == name
			}
			if !keep {
				continue
			}
			code := line
			if i := strings.Index(code, "//"); i >= 0 {
				code = code[:i]
			}
			b.WriteString(code)
			b.WriteString("\n")
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	t.Fatalf("no function %s in this package", name)
	return ""
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TestTheSourceLoopCanGoRed is the self-test of the two cases above. They read
// source and count, so a loop that silently finds nothing would pass whatever
// the package did. This one hands it a package that breaks both sentences and
// checks that it says so.
func TestTheSourceLoopCanGoRed(t *testing.T) {
	dir := t.TempDir()
	bad := "package x\n\n" +
		"func SendGrant() {\n\t_ = wire.Frame{Type: wire.TypeGrant}\n\tc.Allow(1)\n}\n\n" +
		"func elsewhere() {\n\t_ = wire.Frame{Type: wire.TypeGrant}\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(bad), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	writers := funcsContaining(t, dir, "Type: wire.TypeGrant")
	if len(writers) != 2 {
		t.Fatalf("a second writer went unseen: %v", writers)
	}
	body := funcBody(t, dir, "SendGrant")
	if strings.Index(body, ".Allow(") < strings.Index(body, "Type: wire.TypeGrant") {
		t.Fatal("the order check cannot see a frame built before Allow")
	}
	if n := funcsContaining(t, dir, "nothing here says this"); len(n) != 0 {
		t.Fatalf("a needle nobody holds was found in %v", n)
	}
}

// TestSocketRefusesABrokenAPICertificate holds the one answer of Socket that no
// browser can bring about: a session row whose API certificate this API cannot
// read. The handshake could only fail, so the request is refused before the
// upgrade, with E_INTERNAL -- the damage is this API's own row.
func TestSocketRefusesABrokenAPICertificate(t *testing.T) {
	h := newServer(t)
	cookie, _ := loginOnce(t, h)
	raw, err := enc.B64.DecodeString(cookie)
	if err != nil {
		t.Fatalf("cookie: %v", err)
	}
	row, err := h.Srv.S.DB.SessionGet(ctx, session.TokenHash(raw))
	if err != nil {
		t.Fatalf("session row: %v", err)
	}
	row.APICert = []byte("{")
	if err := h.Srv.S.DB.SessionPut(ctx, row); err != nil {
		t.Fatalf("session put: %v", err)
	}
	w, r := socketRequest(cookie)
	h.Socket(w, r)
	if w.Code != 500 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var c wire.Close
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("body %q: %v", w.Body, err)
	}
	if c.Code != wire.CodeInternal {
		t.Fatalf("code %q", c.Code)
	}
}
