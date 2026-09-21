package noisep

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"testing"

	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

func fixedKey(t *testing.T, fill byte) noise.DHKey {
	t.Helper()
	k, err := Keypair(bytes.NewReader(bytes.Repeat([]byte{fill}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// pair runs a full Noise_KK handshake and returns both transports and the two handshake messages.
func pair(t *testing.T, prologue []byte, is, ie, rs, re noise.DHKey, p1, p2 []byte) (*Transport, *Transport, []byte, []byte) {
	t.Helper()
	init, err := newHandshake(true, prologue, is, rs.Public, rand.Reader, ie)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := newHandshake(false, prologue, rs, is.Public, rand.Reader, re)
	if err != nil {
		t.Fatal(err)
	}
	msg1, _, _, err := init.WriteMessage(nil, p1)
	if err != nil {
		t.Fatal(err)
	}
	got1, _, _, err := resp.ReadMessage(nil, msg1)
	if err != nil || !bytes.Equal(got1, p1) {
		t.Fatalf("read msg1: %v", err)
	}
	msg2, rcs1, rcs2, err := resp.WriteMessage(nil, p2)
	if err != nil {
		t.Fatal(err)
	}
	got2, ics1, ics2, err := init.ReadMessage(nil, msg2)
	if err != nil || !bytes.Equal(got2, p2) {
		t.Fatalf("read msg2: %v", err)
	}
	return NewTransport(true, ics1, ics2), NewTransport(false, rcs1, rcs2), msg1, msg2
}

func TestHandshakeAndTransport(t *testing.T) {
	var ie, re noise.DHKey
	it, rt, _, _ := pair(t, []byte(PrologueTunnel), fixedKey(t, 1), ie, fixedKey(t, 2), re, nil, nil)
	for _, msg := range []string{"hello", "", "world"} {
		c, err := it.Seal([]byte(msg))
		if err != nil {
			t.Fatal(err)
		}
		p, err := rt.Open(c)
		if err != nil || string(p) != msg {
			t.Fatalf("initiator → responder %q: %v", msg, err)
		}
		c, _ = rt.Seal([]byte(msg))
		if p, err := it.Open(c); err != nil || string(p) != msg {
			t.Fatalf("responder → initiator %q: %v", msg, err)
		}
	}
	if _, err := it.Seal(make([]byte, MaxPlaintext+1)); err != ErrTooLarge {
		t.Errorf("too large: %v", err)
	}
}

func TestWrongPrologueOrKeyFails(t *testing.T) {
	is, rs, other := fixedKey(t, 1), fixedKey(t, 2), fixedKey(t, 3)
	init, _ := Handshake(true, []byte(PrologueSession), is, rs.Public, rand.Reader)
	msg1, _, _, _ := init.WriteMessage(nil, nil)
	resp, _ := Handshake(false, []byte(PrologueTunnel), rs, is.Public, rand.Reader)
	if _, _, _, err := resp.ReadMessage(nil, msg1); err == nil {
		t.Error("wrong prologue accepted")
	}
	resp, _ = Handshake(false, []byte(PrologueSession), rs, other.Public, rand.Reader)
	if _, _, _, err := resp.ReadMessage(nil, msg1); err == nil {
		t.Error("wrong initiator key accepted")
	}
}

// terminalParams are the params of the terminal OPEN in wire.json.
var terminalParams = []byte(`{"cols":80,"rows":24}`)

func TestE2EPrologue(t *testing.T) {
	const target = "ngc/e2e/v1\x00ctl-svc\x00org.sorglos.terminal\x00terminal\x00shell\x00"
	h := sha256.Sum256(terminalParams)
	got := E2EPrologue("ctl-svc", "org.sorglos.terminal", "terminal", "shell", terminalParams)
	if want := append([]byte(target), h[:]...); !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}

	t.Run("empty params hash the empty string", func(t *testing.T) {
		emptyHash, _ := hex.DecodeString("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
		want := append([]byte(target), emptyHash...)
		for _, params := range [][]byte{nil, {}} {
			if got := E2EPrologue("ctl-svc", "org.sorglos.terminal", "terminal", "shell", params); !bytes.Equal(got, want) {
				t.Errorf("params %q: got %q", params, got)
			}
		}
	})

	t.Run("other params break the handshake", func(t *testing.T) {
		is, rs := fixedKey(t, 1), fixedKey(t, 2)
		prologue := func(params string) []byte {
			return E2EPrologue("ctl-svc", "org.sorglos.terminal", "terminal", "shell", []byte(params))
		}
		for _, c := range []struct {
			name, shell, controller string
			ok                      bool
		}{
			{"same params", string(terminalParams), string(terminalParams), true},
			{"other value", string(terminalParams), `{"cols":81,"rows":24}`, false},
			{"same value, other bytes", string(terminalParams), `{"cols":80, "rows":24}`, false},
			{"params only on one side", string(terminalParams), "", false},
		} {
			init, err := Handshake(true, prologue(c.shell), is, rs.Public, rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			msg1, _, _, err := init.WriteMessage(nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := Handshake(false, prologue(c.controller), rs, is.Public, rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := resp.ReadMessage(nil, msg1); (err == nil) != c.ok {
				t.Errorf("%s: got %v", c.name, err)
			}
		}
	})
}

type keyVector struct {
	Private enc.Bytes `json:"private"`
	Public  enc.Bytes `json:"public"`
}

type transportVector struct {
	From       string    `json:"from"`
	Plaintext  enc.Bytes `json:"plaintext"`
	Ciphertext enc.Bytes `json:"ciphertext"`
}

type noiseVector struct {
	Protocol      string            `json:"protocol"`
	ControllerID  string            `json:"controllerId"`
	App           string            `json:"app"`
	Service       string            `json:"service"`
	Action        string            `json:"action"`
	Params        enc.Bytes         `json:"params"`
	Prologue      enc.Bytes         `json:"prologue"`
	InitStatic    keyVector         `json:"initStatic"`
	InitEphemeral keyVector         `json:"initEphemeral"`
	RespStatic    keyVector         `json:"respStatic"`
	RespEphemeral keyVector         `json:"respEphemeral"`
	Msg1Payload   enc.Bytes         `json:"msg1Payload"`
	Msg1          enc.Bytes         `json:"msg1"`
	Msg2Payload   enc.Bytes         `json:"msg2Payload"`
	Msg2          enc.Bytes         `json:"msg2"`
	Transport     []transportVector `json:"transport"`
}

func kv(k noise.DHKey) keyVector { return keyVector{Private: k.Private, Public: k.Public} }

// TestVectors writes noise.json: an end-to-end handshake for the terminal
// OPEN of wire.json, both messages without payload, then one inner message
// (wire.E2E) per transport message, as shell and controller exchange them.
func TestVectors(t *testing.T) {
	is, ie, rs, re := fixedKey(t, 1), fixedKey(t, 2), fixedKey(t, 3), fixedKey(t, 4)
	v := noiseVector{
		Protocol:     "Noise_KK_25519_AESGCM_SHA256",
		ControllerID: "ctl-svc", App: "org.sorglos.terminal", Service: "terminal", Action: "shell", Params: terminalParams,
		InitStatic: kv(is), InitEphemeral: kv(ie), RespStatic: kv(rs), RespEphemeral: kv(re),
	}
	v.Prologue = E2EPrologue(v.ControllerID, v.App, v.Service, v.Action, v.Params)
	it, rt, msg1, msg2 := pair(t, v.Prologue, is, ie, rs, re, nil, nil)
	if len(msg1) != wire.HandshakeSize || len(msg2) != wire.HandshakeSize {
		t.Fatalf("handshake messages of %d and %d bytes", len(msg1), len(msg2))
	}
	v.Msg1, v.Msg2 = msg1, msg2
	for _, s := range []struct {
		from       string
		tr, peer   *Transport
		kind       byte
		body       string
		controller bool
	}{
		{"initiator", it, rt, wire.E2EData, "ls -la\n", false},
		{"responder", rt, it, wire.E2EData, "total 0\n", true},
		{"initiator", it, rt, wire.E2EEnd, "", false},
		{"responder", rt, it, wire.E2EEnd, `{"status":"ok"}`, true},
	} {
		plain, err := wire.E2E(s.kind, []byte(s.body))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := wire.ParseE2E(plain, s.controller); err != nil {
			t.Fatalf("%s %q: %v", s.from, plain, err)
		}
		c, err := s.tr.Seal(plain)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := s.peer.Open(c); err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("%s %q: %v", s.from, got, err)
		}
		v.Transport = append(v.Transport, transportVector{From: s.from, Plaintext: plain, Ciphertext: c})
	}
	golden.Check(t, *update, "noise.json", v)
}
