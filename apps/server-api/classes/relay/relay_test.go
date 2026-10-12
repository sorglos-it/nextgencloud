package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// rawOpen builds an OPEN payload by hand, with a space the JSON encoder would
// not write. That space is the whole point of the byte-for-byte cases: a relay
// that decodes and encodes again loses it, and nothing else about the OPEN
// would show the difference.
func rawOpen(t *testing.T, app, service, action string, e2e bool) []byte {
	t.Helper()
	raw := `{"app":"` + app + `", "service":"` + service + `","action":"` + action +
		`","params":{"app":"vendor.app"},"window":4096`
	if e2e {
		hs := make([]byte, wire.HandshakeSize)
		for i := range hs {
			hs[i] = byte(i)
		}
		raw += `,"e2e":"` + enc.B64.EncodeToString(hs) + `"`
	}
	raw += `}`
	if _, err := wire.ParseOpen([]byte(raw)); err != nil {
		t.Fatalf("the OPEN of the case is invalid: %v", err)
	}
	return []byte(raw)
}

// forwarded builds the input of ForwardRaw for one OPEN payload.
func forwarded(t *testing.T, raw []byte, node string) Forwarded {
	t.Helper()
	o, err := wire.ParseOpen(raw)
	if err != nil {
		t.Fatalf("ParseOpen: %v", err)
	}
	// The contract takes a client certificate and a proof over it both or
	// neither (tunnel.ParseOpen), so a case that leaves the proof out would
	// never get an OPEN onto the tunnel at all.
	return Forwarded{Raw: raw, Open: o, Node: node, Client: []byte(`{"user":"u1"}`),
		Proof: &proof.Proof{Kind: proof.KindPassword,
			KeyID: enc.B64.EncodeToString(make([]byte, sign.KeyIDSize)), Sig: []byte("sig")}}
}

func TestForwardPassesTheOpenByteForByte(t *testing.T) {
	rl := newRelay()
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	raw := rawOpen(t, "vendor.app", "main", "run", true)
	fw := forwarded(t, raw, "c1")
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)

	e := hop.await(t, "open")
	o, inner, err := tunnel.ParseOpen(e.payload)
	if err != nil {
		t.Fatalf("the OPEN on the tunnel does not parse: %v", err)
	}
	if !bytes.Equal(o.Open, raw) {
		t.Fatalf("OPEN not byte for byte:\n got %q\nwant %q", o.Open, raw)
	}
	// The control for the case: an OPEN that was decoded and encoded again is
	// a different byte string, so a relay that re-marshals fails the line
	// above. Without this, the case would also pass for a relay that does.
	again, err := json.Marshal(inner)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if bytes.Equal(again, raw) {
		t.Fatal("the case cannot tell a re-marshalled OPEN apart; pick another payload")
	}
	if !bytes.Equal(o.Client, fw.Client) {
		t.Errorf("client certificate: got %q want %q", o.Client, fw.Client)
	}
	if o.Window != wire.DefaultWindow {
		t.Errorf("window on the tunnel hop: got %d want %d", o.Window, wire.DefaultWindow)
	}
}

func TestFirstGrantWaitsForTheController(t *testing.T) {
	rl := newRelay()
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)

	hop.await(t, "open")
	// Nothing has gone to the shell yet, and that is the order the end-to-end
	// handshake needs: the API's first GRANT carries the controller's Noise
	// message 2, which does not exist before the controller's GRANT.
	if evs := sh.events(); len(evs) != 0 {
		t.Fatalf("the shell heard something before the controller's GRANT: %v", sh.kinds())
	}
	hs := handshake()
	hop.send(grant(wire.MinWindow, hs))
	e := sh.await(t, "grant")
	if !bytes.Equal(e.hs, hs) {
		t.Errorf("Noise message 2 not passed on byte for byte:\n got %x\nwant %x", e.hs, hs)
	}
}

func TestDataAndEOFGoOneToOne(t *testing.T) {
	rl := newRelay()
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	sh.noCredit = true
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)
	hop.await(t, "open")
	hop.send(grant(wire.MaxWindow, handshake()))
	sh.await(t, "grant")

	// Three payloads of very different size, one of them the largest a frame
	// can carry: each must arrive as one DATA frame with the same bytes.
	want := [][]byte{bytes.Repeat([]byte("a"), 1), bytes.Repeat([]byte("b"), 7),
		bytes.Repeat([]byte("c"), wire.MaxPayload)}
	for _, p := range want {
		hop.send(wire.Frame{Type: wire.TypeData, Channel: 1, Payload: p})
	}
	hop.send(wire.Frame{Type: wire.TypeEOF, Channel: 1})
	sh.await(t, "eof")

	got := [][]byte{}
	for _, e := range sh.events() {
		if e.kind == "data" {
			got = append(got, e.payload)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %d DATA frames, want %d", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Errorf("DATA %d: %d bytes, want %d equal bytes", i, len(got[i]), len(want[i]))
		}
	}
}

func TestDataOfTheShellGoesOneToOne(t *testing.T) {
	rl := newRelay()
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)
	hop.await(t, "open")
	hop.send(grant(wire.MinWindow, handshake()))
	sh.await(t, "grant")

	// One payload larger than the credit of the moment (MinWindow is 4 KiB).
	// It must travel whole: a relay that cuts it to what it may send right now
	// destroys an end-to-end message, and the shell then sees a failed
	// decryption and not a credit problem.
	big := bytes.Repeat([]byte("x"), wire.MinWindow+1)
	sh.send(t, c, wire.Frame{Type: wire.TypeData, Channel: 7, Payload: big})
	hop.send(grant(wire.MinWindow, nil))
	e := hop.await(t, "data")
	if !bytes.Equal(e.payload, big) {
		t.Fatalf("payload to the controller: %d bytes, want %d whole", len(e.payload), len(big))
	}
	sh.send(t, c, wire.Frame{Type: wire.TypeEOF, Channel: 7})
	hop.await(t, "eof")
}

func TestCloseKeepsTheCode(t *testing.T) {
	rl := newRelay()
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)
	hop.await(t, "open")
	hop.send(grant(wire.MinWindow, handshake()))
	sh.await(t, "grant")

	payload, err := json.Marshal(wire.Error(wire.CodeExit))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	hop.send(wire.Frame{Type: wire.TypeClose, Channel: 1, Payload: payload})
	e := sh.await(t, "close")
	if e.cl.Code != wire.CodeExit || e.cl.Status != wire.StatusError {
		t.Fatalf("close %+v, want %s", e.cl, wire.CodeExit)
	}
}

func TestFullTunnelAnswersLimit(t *testing.T) {
	rl := newRelay()
	sh, hop := newShell(), newHop(0) // a tunnel with no free number
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	c := sh.channel(t)
	if code := rl.ForwardRaw(context.Background(), sh, c, hop, fw); code != wire.CodeLimit {
		t.Fatalf("code %q, want %q", code, wire.CodeLimit)
	}
	e := sh.await(t, "closecode")
	if e.code != wire.CodeLimit {
		t.Fatalf("the shell was closed with %q, want %q", e.code, wire.CodeLimit)
	}
}

func TestResultOfAControllerIsBounded(t *testing.T) {
	rl := newRelay()
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	// This case measures the limit on the result, not the credit: the shell
	// grants nothing like 8 MiB, and the credit has cases of its own.
	sh.noCredit = true
	fw := forwarded(t, rawOpen(t, core.AppStore, core.ServiceController, core.ActDeactivate, false), "c1")
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)
	hop.await(t, "open")
	hop.send(grant(wire.MinWindow, nil))
	sh.await(t, "grant")

	go feed(hop, wire.MaxCallResult+1)
	e := sh.await(t, "closecode")
	if e.code != wire.CodeTooLarge {
		t.Fatalf("code %q, want %q", e.code, wire.CodeTooLarge)
	}
	// Both sides of the limit: not one byte over it reached the shell, and
	// something did reach it, so the case would not pass for a relay that
	// forwards nothing at all.
	total := 0
	for _, ev := range sh.events() {
		if ev.kind == "data" {
			total += len(ev.payload)
		}
	}
	if total > wire.MaxCallResult {
		t.Fatalf("%d bytes reached the shell, more than the limit of %d", total, wire.MaxCallResult)
	}
	if total == 0 {
		t.Fatal("nothing reached the shell")
	}
}

func TestShellSendsNoDataOnTransfer(t *testing.T) {
	rl := newRelay()
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	fw := forwarded(t, rawOpen(t, core.AppStore, core.ServiceController, core.ActActivate, false), "c1")
	fw.Head = &core.Transfer{Size: 3, SHA256: bytes.Repeat([]byte{1}, 32)}
	fw.File = bytes.NewReader([]byte("abc"))
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)
	hop.await(t, "open")
	hop.send(grant(wire.MaxWindow, nil))
	sh.await(t, "grant")
	// The API writes the header and the file itself; its EOF ends that input.
	hop.await(t, "eof")

	sh.send(t, c, wire.Frame{Type: wire.TypeData, Channel: 7, Payload: []byte("x")})
	e := sh.await(t, "closecode")
	if e.code != wire.CodeProtocol {
		t.Fatalf("code %q, want %q", e.code, wire.CodeProtocol)
	}
}

func TestTransferHeaderAndFileReachTheController(t *testing.T) {
	rl := newRelay()
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	fw := forwarded(t, rawOpen(t, core.AppStore, core.ServiceController, core.ActActivate, false), "c1")
	head := core.Transfer{Size: 5, SHA256: bytes.Repeat([]byte{2}, 32)}
	fw.Head, fw.File = &head, bytes.NewReader([]byte("hello"))
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)
	hop.await(t, "open")
	hop.send(grant(wire.MaxWindow, nil))
	hop.await(t, "eof")

	var stream bytes.Buffer
	for _, e := range hop.events() {
		if e.kind == "data" {
			stream.Write(e.payload)
		}
	}
	got, err := core.ReadTransfer(&stream)
	if err != nil {
		t.Fatalf("ReadTransfer: %v", err)
	}
	if got.Size != head.Size {
		t.Errorf("size %d, want %d", got.Size, head.Size)
	}
	if rest, _ := io.ReadAll(&stream); string(rest) != "hello" {
		t.Errorf("file %q, want %q", rest, "hello")
	}
}

func TestControllerCannotOpenAChannel(t *testing.T) {
	rl := newRelay()
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)
	hop.await(t, "open")
	hop.send(wire.Frame{Type: wire.TypeOpen, Channel: 1, Payload: []byte(`{"open":"","window":4096}`)})
	e := sh.await(t, "closecode")
	if e.code != wire.CodeProtocol {
		t.Fatalf("code %q, want %q", e.code, wire.CodeProtocol)
	}
}

func TestCreditOfTheHopIsCountedOnce(t *testing.T) {
	// The measurement behind "the relay does not count what arrives": a GRANT
	// of MinWindow must leave exactly MinWindow on the hop, not twice that.
	// The tunnel's own reader adds it before the frame is queued, so a relay
	// that adds it again would believe it may send more than the controller
	// allowed -- and the controller would answer that with E_PROTOCOL.
	//
	// Both sides of the figure are nailed down: MinWindow fits, one byte more
	// does not.
	rl := newRelay()
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)
	hop.await(t, "open")
	hop.send(grant(wire.MinWindow, handshake()))
	sh.await(t, "grant")

	tc, ok := hop.Get(1)
	if !ok {
		t.Fatal("the hop channel is gone")
	}
	if !tc.SendAllowed(wire.MinWindow) {
		t.Fatalf("the hop may not send %d bytes, and the controller granted that much", wire.MinWindow)
	}
	if tc.SendAllowed(wire.MinWindow + 1) {
		t.Fatalf("the hop may send more than %d bytes: the credit was counted twice", wire.MinWindow)
	}
}

func TestGrantBeyondTheWindowIsNotOursToAnswer(t *testing.T) {
	// Where that check went: a GRANT that would lift the allowance above
	// wire.MaxWindow is refused by the tunnel's reader, which ends the whole
	// tunnel (controllers.Tunnel.frame). It never reaches a channel, so this
	// package has no answer to give -- and what is measured here is that it
	// invents none: the credit does not grow, and no second GRANT goes to the
	// shell.
	rl := newRelay()
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)
	hop.await(t, "open")
	hop.send(grant(wire.MaxWindow, handshake()))
	sh.await(t, "grant")
	hop.send(grant(1, nil))

	tc, ok := hop.Get(1)
	if !ok {
		t.Fatal("the hop channel is gone")
	}
	if tc.SendAllowed(wire.MaxWindow + 1) {
		t.Fatal("the credit grew past wire.MaxWindow")
	}
	if n := len(sh.events()); n != 1 {
		t.Fatalf("the shell heard %d things, want the one first GRANT: %v", n, sh.kinds())
	}
}

func TestHandshakeInALaterGrantEndsTheChannel(t *testing.T) {
	rl := newRelay()
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)
	hop.await(t, "open")
	hop.send(grant(wire.MinWindow, handshake()))
	sh.await(t, "grant")
	hop.send(grant(wire.MinWindow, handshake()))
	e := sh.await(t, "closecode")
	if e.code != wire.CodeProtocol {
		t.Fatalf("code %q, want %q", e.code, wire.CodeProtocol)
	}
}

func TestChannelNumberIsFreeOnlyAfterBothCloses(t *testing.T) {
	rl := newRelay()
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	c := sh.channel(t)
	done := make(chan wire.Code, 1)
	go func() { done <- rl.ForwardRaw(context.Background(), sh, c, hop, fw) }()
	hop.await(t, "open")
	if n := hop.tbl.Count(); n != 1 {
		t.Fatalf("tunnel channels %d, want 1", n)
	}
	// A handshake in a later GRANT makes the API send its own CLOSE; that is
	// a rule of this package, unlike the window, which the tunnel's reader
	// answers by ending the tunnel.
	hop.send(grant(wire.MinWindow, handshake()))
	sh.await(t, "grant")
	hop.send(grant(wire.MinWindow, handshake()))
	sh.await(t, "closecode")

	before := len(sh.events())
	// A frame after our own CLOSE is dropped; the controller's CLOSE among
	// them still counts (the tunnel's reader notes it) and frees the number,
	// because our own CLOSE was noted when it went out.
	hop.send(wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("late")})
	payload, err := json.Marshal(wire.Error(wire.CodeProtocol))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	hop.send(wire.Frame{Type: wire.TypeClose, Channel: 1, Payload: payload})
	<-done
	if n := hop.tbl.Count(); n != 0 {
		t.Fatalf("tunnel channels %d after both closes, want 0", n)
	}
	for _, e := range sh.events()[before:] {
		if e.kind == "data" {
			t.Fatal("a frame after our own CLOSE reached the shell")
		}
	}
}

func TestCallControllerSendsItsInputAndCollectsTheResult(t *testing.T) {
	rl := newRelay()
	hop := newHop(wire.MaxTunnelChannels)
	go func() {
		hop.await(t, "open")
		hop.send(grant(wire.MaxWindow, nil))
		hop.await(t, "eof")
		hop.send(wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("done")})
		hop.send(wire.Frame{Type: wire.TypeEOF, Channel: 1})
		ok, _ := json.Marshal(wire.Close{Status: wire.StatusOK})
		hop.send(wire.Frame{Type: wire.TypeClose, Channel: 1, Payload: ok})
	}()
	body, code := rl.CallController(context.Background(), hop, core.AppStore,
		core.ServiceController, core.ActDeactivate, json.RawMessage(`{"app":"vendor.app"}`), []byte("in"))
	if code != "" {
		t.Fatalf("code %q, want none", code)
	}
	if string(body) != "done" {
		t.Fatalf("result %q, want %q", body, "done")
	}
	var sent bytes.Buffer
	for _, e := range hop.events() {
		if e.kind == "data" {
			sent.Write(e.payload)
		}
	}
	if sent.String() != "in" {
		t.Fatalf("input on the tunnel %q, want %q", sent.String(), "in")
	}
}

func TestCallControllerRefusesAnAppChannel(t *testing.T) {
	rl := newRelay()
	hop := newHop(wire.MaxTunnelChannels)
	// The API has no end-to-end handshake of its own, so an app that is not a
	// core app is E_INTERNAL here and never becomes an OPEN the controller
	// would have to refuse.
	_, code := rl.CallController(context.Background(), hop, "vendor.app", "main", "run", nil, nil)
	if code != wire.CodeInternal {
		t.Fatalf("code %q, want %q", code, wire.CodeInternal)
	}
	if n := len(hop.events()); n != 0 {
		t.Fatalf("%d frames went out, want none", n)
	}
}

func TestCallResultBeyondTheLimitIsTooLarge(t *testing.T) {
	rl := newRelay()
	hop := newHop(wire.MaxTunnelChannels)
	go func() {
		hop.await(t, "open")
		hop.send(grant(wire.MaxWindow, nil))
		feed(hop, wire.MaxCallResult+1)
	}()
	_, code := rl.CallController(context.Background(), hop, core.AppStore,
		core.ServiceController, core.ActDeactivate, nil, nil)
	if code != wire.CodeTooLarge {
		t.Fatalf("code %q, want %q", code, wire.CodeTooLarge)
	}
}

func TestCallResultCountsOnlyAfterCloseOK(t *testing.T) {
	rl := newRelay()
	hop := newHop(wire.MaxTunnelChannels)
	go func() {
		hop.await(t, "open")
		hop.send(grant(wire.MaxWindow, nil))
		hop.send(wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("half")})
		hop.send(wire.Frame{Type: wire.TypeEOF, Channel: 1})
		bad, _ := json.Marshal(wire.Error(wire.CodeExit))
		hop.send(wire.Frame{Type: wire.TypeClose, Channel: 1, Payload: bad})
	}()
	body, code := rl.CallController(context.Background(), hop, core.AppStore,
		core.ServiceController, core.ActDeactivate, nil, nil)
	if code != wire.CodeExit {
		t.Fatalf("code %q, want %q", code, wire.CodeExit)
	}
	if body != nil {
		t.Fatalf("a result of %d bytes came back from a failed channel", len(body))
	}
}

func TestDrainIsBoundedByDefault(t *testing.T) {
	// The cases above shorten this wait; the default is what runs in
	// production, and a relay without a bound here would hold a channel number
	// of the tunnel for its whole life.
	if d := (&Relay{}).drain(); d != CloseDrain {
		t.Fatalf("drain %v, want %v", d, CloseDrain)
	}
}

func TestBothDirectionsAtOnce(t *testing.T) {
	// Two hops, two directions, running at the same time: the one case that
	// gives -race something to find. Each payload is a different length and
	// carries its own number, so a pair that was swapped or joined is visible
	// and not only a changed total.
	const n = 64
	rl := newRelay()
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	sh.noCredit = true
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)
	hop.await(t, "open")
	hop.send(grant(wire.MaxWindow, handshake()))
	sh.await(t, "grant")

	want := make([][]byte, n)
	for i := range want {
		want[i] = bytes.Repeat([]byte{byte('a' + i%26)}, i+1)
	}
	go func() {
		for i := range want {
			hop.send(wire.Frame{Type: wire.TypeData, Channel: 1, Payload: want[i]})
		}
		hop.send(wire.Frame{Type: wire.TypeEOF, Channel: 1})
	}()
	go func() {
		for i := range want {
			sh.send(t, c, wire.Frame{Type: wire.TypeData, Channel: 7, Payload: want[i]})
		}
		sh.send(t, c, wire.Frame{Type: wire.TypeEOF, Channel: 7})
	}()
	sh.await(t, "eof")
	hop.await(t, "eof")

	for _, side := range []struct {
		name string
		evs  []ev
	}{{"to the shell", sh.events()}, {"to the controller", hop.events()}} {
		got := [][]byte{}
		for _, e := range side.evs {
			if e.kind == "data" {
				got = append(got, e.payload)
			}
		}
		if len(got) != n {
			t.Fatalf("%s: %d DATA frames, want %d", side.name, len(got), n)
		}
		for i := range want {
			if !bytes.Equal(got[i], want[i]) {
				t.Fatalf("%s: frame %d is %d bytes %q..., want %d bytes of %q",
					side.name, i, len(got[i]), got[i][:1], len(want[i]), want[i][:1])
			}
		}
	}
}

func TestAPayloadIsNeverCutToFitTheCredit(t *testing.T) {
	// The credit never grows to the size of the payload, so the only two
	// things a relay can do are wait until its clock runs out or cut the
	// payload. Cutting is what destroys an end-to-end message, so the case
	// asks for the clock -- and it asks without racing one, because the credit
	// of this channel never changes.
	rl := newRelay()
	rl.Wait = 50 * time.Millisecond
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)
	hop.await(t, "open")
	hop.send(grant(wire.MinWindow, handshake()))
	sh.await(t, "grant")

	sh.send(t, c, wire.Frame{Type: wire.TypeData, Channel: 7, Payload: bytes.Repeat([]byte("x"), wire.MinWindow+1)})
	e := sh.await(t, "closecode")
	if e.code != wire.CodeTimeout {
		t.Fatalf("code %q, want %q", e.code, wire.CodeTimeout)
	}
	for _, ev := range hop.events() {
		if ev.kind == "data" {
			t.Fatalf("%d bytes went to the controller, and the payload was %d",
				len(ev.payload), wire.MinWindow+1)
		}
	}
}

func TestWaitForCreditIsBoundedByDefault(t *testing.T) {
	if w := (&Relay{}).wait(); w != GrantWait {
		t.Fatalf("wait %v, want %v", w, GrantWait)
	}
	// The three clocks of this package differ in length on purpose: two of the
	// same length turn two error codes into a race.
	if GrantWait == CloseDrain || GrantWait == CallTimeout || CloseDrain == CallTimeout {
		t.Fatalf("two clocks of the same length: %v %v %v", GrantWait, CloseDrain, CallTimeout)
	}
}

func TestEndToEndResultIsNotBoundedHere(t *testing.T) {
	// On an end-to-end channel every payload is a Noise message, so the API
	// cannot count a result at all: wire.MaxCallResult is the shell's to
	// enforce there (package wire, "Results"). This case holds that the API
	// does not close such a channel when the bytes pass the limit, and it is
	// the counterpart of TestResultOfAControllerIsBounded.
	rl := newRelay()
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	sh.noCredit = true
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)
	hop.await(t, "open")
	hop.send(grant(wire.MaxWindow, handshake()))
	sh.await(t, "grant")

	go func() {
		feed(hop, wire.MaxCallResult+1)
		hop.send(wire.Frame{Type: wire.TypeEOF, Channel: 1})
	}()
	// The EOF only arrives if nothing closed the channel on the way.
	sh.await(t, "eof")
	total := 0
	for _, e := range sh.events() {
		switch e.kind {
		case "data":
			total += len(e.payload)
		case "closecode":
			t.Fatalf("the API closed an end-to-end channel with %q", e.code)
		}
	}
	if total <= wire.MaxCallResult {
		t.Fatalf("only %d bytes went through, so the case never passed the limit of %d",
			total, wire.MaxCallResult)
	}
}

func TestNormalCloseGivesTheNumberBackAtOnce(t *testing.T) {
	// The real clock, on purpose: a channel the controller closed in order
	// must not wait CloseDrain for a CLOSE that has already arrived. The
	// margin is the whole point -- two seconds against ten.
	rl := &Relay{}
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	c := sh.channel(t)
	done := make(chan wire.Code, 1)
	go func() { done <- rl.ForwardRaw(context.Background(), sh, c, hop, fw) }()
	hop.await(t, "open")
	hop.send(grant(wire.MinWindow, handshake()))
	sh.await(t, "grant")
	ok, err := json.Marshal(wire.Close{Status: wire.StatusOK})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	hop.send(wire.Frame{Type: wire.TypeClose, Channel: 1, Payload: ok})
	select {
	case code := <-done:
		if code != "" {
			t.Fatalf("code %q, want none", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("the channel was still waiting after 2 s, and CloseDrain is %v", CloseDrain)
	}
	if n := hop.tbl.Count(); n != 0 {
		t.Fatalf("tunnel channels %d, want 0", n)
	}
}

func TestAnOpenTheContractRefusesNeverGoesOut(t *testing.T) {
	// The shell's OPEN still fits wire.MaxBrowserOpen, but the certificate the
	// API attaches is past session.MaxCert, so the payload on the tunnel is one
	// the contract refuses. It must not go out: the controller answers a frame
	// like that by ending the whole tunnel, which would take every other
	// channel on it with it.
	rl := newRelay()
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	fw.Client = bytes.Repeat([]byte("c"), session.MaxCert+1)
	c := sh.channel(t)
	// In a goroutine with a clock on it: a relay that sends the OPEN anyway
	// then waits for an answer that never comes, and the case has to fail
	// rather than hang.
	done := make(chan wire.Code, 1)
	go func() { done <- rl.ForwardRaw(context.Background(), sh, c, hop, fw) }()
	select {
	case code := <-done:
		if code != wire.CodeInternal {
			t.Fatalf("code %q, want %q", code, wire.CodeInternal)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the channel was still running after 5 s")
	}
	for _, e := range hop.events() {
		if e.kind == "open" {
			t.Fatalf("an OPEN of %d bytes went onto the tunnel", len(e.payload))
		}
	}
	if e := sh.await(t, "closecode"); e.code != wire.CodeInternal {
		t.Fatalf("the shell was closed with %q, want %q", e.code, wire.CodeInternal)
	}
}

func TestTheShellsCloseTravelsUnchanged(t *testing.T) {
	// A CLOSE of the shell is passed on as it stands, and a CLOSE with status
	// ok is the case that catches the obvious shortcut: a relay that carries
	// only the code has no code to carry here and would invent one.
	for _, want := range []wire.Close{{Status: wire.StatusOK}, wire.Error(wire.CodeExit)} {
		rl := newRelay()
		sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
		fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
		c := sh.channel(t)
		done := make(chan wire.Code, 1)
		go func() { done <- rl.ForwardRaw(context.Background(), sh, c, hop, fw) }()
		hop.await(t, "open")
		hop.send(grant(wire.MinWindow, handshake()))
		sh.await(t, "grant")

		payload, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		sh.send(t, c, wire.Frame{Type: wire.TypeClose, Channel: 7, Payload: payload})
		e := hop.await(t, "close")
		got, err := wire.ParseClose(e.payload)
		if err != nil {
			t.Fatalf("the CLOSE on the tunnel does not parse: %v", err)
		}
		if got != want {
			t.Errorf("CLOSE on the tunnel %+v, want %+v", got, want)
		}
		// The shell said it, so the shell hears nothing back.
		for _, ev := range sh.events() {
			if ev.kind == "closecode" || ev.kind == "close" {
				t.Errorf("the shell was answered with %+v %q", ev.cl, ev.code)
			}
		}
	}
}

func TestTransferTheAPIMakesForItself(t *testing.T) {
	rl := newRelay()
	hop := newHop(wire.MaxTunnelChannels)
	go func() {
		hop.await(t, "open")
		hop.send(grant(wire.MaxWindow, nil))
		hop.await(t, "eof")
		hop.send(wire.Frame{Type: wire.TypeEOF, Channel: 1})
		ok, _ := json.Marshal(wire.Close{Status: wire.StatusOK})
		hop.send(wire.Frame{Type: wire.TypeClose, Channel: 1, Payload: ok})
	}()
	head := core.Transfer{Size: 4, SHA256: bytes.Repeat([]byte{3}, 32)}
	_, code := rl.Transfer(context.Background(), hop, core.AppStore, core.ServiceController,
		core.ActStage, json.RawMessage(`{"app":"vendor.app"}`), head,
		bytes.NewReader([]byte("file")), nil, nil, nil)
	if code != "" {
		t.Fatalf("code %q, want none", code)
	}
	var stream bytes.Buffer
	for _, e := range hop.events() {
		if e.kind == "data" {
			stream.Write(e.payload)
		}
	}
	got, err := core.ReadTransfer(&stream)
	if err != nil {
		t.Fatalf("ReadTransfer: %v", err)
	}
	if got.Size != head.Size {
		t.Errorf("size %d, want %d", got.Size, head.Size)
	}
	if rest, _ := io.ReadAll(&stream); string(rest) != "file" {
		t.Errorf("file %q, want %q", rest, "file")
	}
}

// feed sends DATA of wire.MaxPayload bytes until total bytes have gone, and it
// does so one window at a time. That is not decoration: since the seam of task
// 15 the tunnel's reader takes the bytes off what the controller may send
// BEFORE the frame is queued, so a case that fires 128 frames into the queue
// at once runs out of window after four of them and measures the window
// instead of what it meant to measure. The relay hands the credit back with a
// GRANT once the bytes are through, so waiting for that GRANT is waiting for
// room.
//
// It stops when the relay has closed the channel, which is what the cases
// about a limit are waiting for, and it answers how much really went.
func feed(hop *fakeHop, total int) int {
	chunk := bytes.Repeat([]byte("r"), wire.MaxPayload)
	sent, grants := 0, 0
	for sent < total {
		hop.send(wire.Frame{Type: wire.TypeData, Channel: 1, Payload: chunk})
		sent += len(chunk)
		grants++
		deadline := time.After(5 * time.Second)
		for {
			got, closed := 0, false
			for _, e := range hop.events() {
				switch e.kind {
				case "grant":
					got++
				case "close":
					closed = true
				}
			}
			if closed {
				return sent
			}
			if got >= grants {
				break
			}
			select {
			case <-hop.seen:
			case <-deadline:
				return sent
			}
		}
	}
	return sent
}

func TestArrivingDataIsCountedOnce(t *testing.T) {
	// The other half of "the relay does not count what arrives", and the one
	// that only shows over several frames: the tunnel's reader takes the bytes
	// off what the controller may send, and the relay hands the same amount
	// back with a GRANT once the shell has them. Net zero per frame, so a
	// controller may keep sending for ever.
	//
	// A relay that took the bytes a second time would spend twice and hand
	// back once -- net minus one frame per frame -- and the window of
	// wire.DefaultWindow would be gone after four frames of wire.MaxPayload.
	// Eight is comfortably past that.
	const frames = 8
	rl := newRelay()
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	sh.noCredit = true
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)
	hop.await(t, "open")
	hop.send(grant(wire.MaxWindow, handshake()))
	sh.await(t, "grant")

	if n := feed(hop, frames*wire.MaxPayload); n != frames*wire.MaxPayload {
		t.Fatalf("only %d of %d bytes got through: the window ran out", n, frames*wire.MaxPayload)
	}
	got := 0
	for _, e := range sh.events() {
		if e.kind == "data" {
			got += len(e.payload)
		}
	}
	if got != frames*wire.MaxPayload {
		t.Fatalf("%d bytes reached the shell, want %d", got, frames*wire.MaxPayload)
	}
}

func TestFramesAfterOurCloseAreTakenOutOfTheQueue(t *testing.T) {
	// Dropping a frame is not the same as leaving it lying. Between our CLOSE
	// and the controller's, whatever still arrives has to come out of the
	// queue: a queue nobody empties reaches link.MaxQueuedFrames, and the
	// tunnel's reader answers that by ending the TUNNEL -- every other
	// channel of every other user on it with it.
	//
	// EOF frames on purpose: they cost no credit, so the window cannot be what
	// stops them, and the count is the only thing left to measure.
	const stray = 64
	rl := &Relay{Drain: 2 * time.Second}
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)
	hop.await(t, "open")
	hop.send(grant(wire.MinWindow, handshake()))
	sh.await(t, "grant")
	hop.send(grant(wire.MinWindow, handshake())) // a later handshake: our CLOSE
	sh.await(t, "closecode")

	tc, ok := hop.Get(1)
	if !ok {
		t.Fatal("the hop channel is gone")
	}
	for range stray {
		hop.send(wire.Frame{Type: wire.TypeEOF, Channel: 1})
	}
	deadline := time.After(5 * time.Second)
	for tc.Queued() > 0 {
		select {
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatalf("%d of %d frames are still in the queue", tc.Queued(), stray)
		}
	}
}

func TestCreditWakesTheSender(t *testing.T) {
	// The wait for credit is woken by the GRANT that raises it, and this is
	// what that buys: a payload that has to wait travels as soon as the credit
	// is there, not when the clock runs out.
	//
	// The condition is established, not hoped for. The payload is handed over
	// first and the case waits until the relay has really taken it out of the
	// queue, then waits 200 ms more -- so the relay is inside its wait -- and
	// only then sends the credit. The margin of the assertion is 5 to 1, not
	// 25 to 1 as this line used to say: a relay that is not woken answers
	// after Wait, five seconds here, and the assertion below allows one
	// second. (The 25 to 1 is the other ratio in the case -- the 200 ms of
	// waiting against those five seconds -- and that one decides nothing.)
	rl := newRelay()
	rl.Wait = 5 * time.Second
	sh, hop := newShell(), newHop(wire.MaxTunnelChannels)
	fw := forwarded(t, rawOpen(t, "vendor.app", "main", "run", true), "c1")
	c := sh.channel(t)
	go rl.ForwardRaw(context.Background(), sh, c, hop, fw)
	hop.await(t, "open")
	hop.send(grant(wire.MinWindow, handshake()))
	sh.await(t, "grant")

	big := bytes.Repeat([]byte("x"), wire.MinWindow+1)
	sh.send(t, c, wire.Frame{Type: wire.TypeData, Channel: 7, Payload: big})
	deadline := time.After(5 * time.Second)
	for c.Queued() > 0 {
		select {
		case <-time.After(5 * time.Millisecond):
		case <-deadline:
			t.Fatal("the relay never took the payload out of the queue")
		}
	}
	time.Sleep(200 * time.Millisecond)

	start := time.Now()
	hop.send(grant(wire.MinWindow, nil))
	e := hop.await(t, "data")
	if took := time.Since(start); took > time.Second {
		t.Fatalf("the payload went %v after the credit, and the wait is %v: nothing woke it",
			took, rl.wait())
	}
	if !bytes.Equal(e.payload, big) {
		t.Fatalf("payload %d bytes, want %d", len(e.payload), len(big))
	}
}
