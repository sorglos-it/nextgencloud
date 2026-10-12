package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// TestCallAsPutsTheUserOnTheWire is the whole difference between CallAs and
// CallController, and it is measured on the OPEN the controller really gets,
// not on the arguments: the certificate, the proof and the signed task must be
// in the payload, because ngc.store/controller/deactivate and .../uninstall
// are NOT in core.WithoutUser and a controller refuses them without a user.
func TestCallAsPutsTheUserOnTheWire(t *testing.T) {
	rl := newRelay()
	hop := newHop(wire.MaxTunnelChannels)
	task := &sign.Envelope{Payload: []byte(`{"app":"vendor.app"}`), Sig: bytes.Repeat([]byte{9}, 64)}
	// The contract takes a client certificate and a proof over it both or
	// neither (tunnel.ParseOpen), so both are here and in the shape the
	// parsers want.
	client := []byte(`{"user":"u1"}`)
	pr := &proof.Proof{Kind: proof.KindPassword,
		KeyID: enc.B64.EncodeToString(make([]byte, sign.KeyIDSize)), Sig: []byte("sig")}
	go func() {
		hop.await(t, "open")
		hop.send(grant(wire.MaxWindow, nil))
		hop.await(t, "eof")
		hop.send(wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("ok")})
		hop.send(wire.Frame{Type: wire.TypeEOF, Channel: 1})
		cl, _ := json.Marshal(wire.Close{Status: wire.StatusOK})
		hop.send(wire.Frame{Type: wire.TypeClose, Channel: 1, Payload: cl})
	}()
	body, code := rl.CallAs(context.Background(), hop, core.AppStore, core.ServiceController,
		core.ActDeactivate, nil, task, client, pr)
	if code != "" {
		t.Fatalf("code %q, want none", code)
	}
	if string(body) != "ok" {
		t.Fatalf("result %q, want %q", body, "ok")
	}
	var open []byte
	for _, e := range hop.events() {
		if e.kind == "open" {
			open = e.payload
		}
	}
	if open == nil {
		t.Fatalf("no OPEN reached the tunnel")
	}
	outer, o, err := tunnel.ParseOpen(open)
	if err != nil {
		t.Fatalf("the OPEN is none of the contract: %v", err)
	}
	if !bytes.Equal(outer.Client, client) {
		t.Fatalf("client certificate on the wire %q, want %q", outer.Client, client)
	}
	if outer.Proof == nil || outer.Proof.Kind != proof.KindPassword {
		t.Fatalf("no proof of the user on the wire: %+v", outer.Proof)
	}
	if o.Task == nil || !bytes.Equal(o.Task.Sig, task.Sig) {
		t.Fatalf("the signed task did not travel: %+v", o.Task)
	}
	// And the cross-check against the other way round: the same call through
	// CallController carries none of the three, which is why it could not be
	// used for this action. Without this half the case would also pass against
	// a CallController that had quietly started sending a user.
	hop2 := newHop(wire.MaxTunnelChannels)
	go func() {
		hop2.await(t, "open")
		hop2.send(grant(wire.MaxWindow, nil))
		hop2.await(t, "eof")
		hop2.send(wire.Frame{Type: wire.TypeEOF, Channel: 1})
		cl, _ := json.Marshal(wire.Close{Status: wire.StatusOK})
		hop2.send(wire.Frame{Type: wire.TypeClose, Channel: 1, Payload: cl})
	}()
	if _, code := rl.CallController(context.Background(), hop2, core.AppStore,
		core.ServiceController, core.ActDeactivate, json.RawMessage(`{"app":"vendor.app"}`),
		nil); code != "" {
		t.Fatalf("CallController: code %q", code)
	}
	for _, e := range hop2.events() {
		if e.kind != "open" {
			continue
		}
		outer2, _, err := tunnel.ParseOpen(e.payload)
		if err != nil {
			t.Fatalf("the OPEN is none of the contract: %v", err)
		}
		if outer2.Client != nil || outer2.Proof != nil {
			t.Fatalf("CallController sent a user after all: %q / %+v", outer2.Client, outer2.Proof)
		}
	}
}

// TestCallAsRefusesWhatIsNoCoreApp: the API holds no end-to-end key, so a
// channel to anything but a core app cannot be opened by it at all, with a
// user as without one.
func TestCallAsRefusesWhatIsNoCoreApp(t *testing.T) {
	rl := newRelay()
	hop := newHop(wire.MaxTunnelChannels)
	_, code := rl.CallAs(context.Background(), hop, "vendor.app", "main", "run", nil, nil, nil, nil)
	if code != wire.CodeInternal {
		t.Fatalf("code %q, want %q", code, wire.CodeInternal)
	}
	if len(hop.events()) != 0 {
		t.Fatalf("something travelled although the call was refused: %v", hop.kinds())
	}
}
