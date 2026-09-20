package noisep

import (
	"bytes"
	"crypto/rand"
	"flag"
	"testing"

	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
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

func TestE2EPrologue(t *testing.T) {
	got := string(E2EPrologue("ctl-svc", "org.sorglos.terminal", "terminal", "shell"))
	want := "ngc/e2e/v1\x00ctl-svc\x00org.sorglos.terminal\x00terminal\x00shell"
	if got != want {
		t.Fatalf("got %q", got)
	}
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

func TestVectors(t *testing.T) {
	is, ie, rs, re := fixedKey(t, 1), fixedKey(t, 2), fixedKey(t, 3), fixedKey(t, 4)
	prologue := E2EPrologue("ctl-svc", "org.sorglos.terminal", "terminal", "shell")
	it, rt, msg1, msg2 := pair(t, prologue, is, ie, rs, re, []byte("m1"), []byte("m2"))
	v := noiseVector{
		Protocol: "Noise_KK_25519_AESGCM_SHA256", Prologue: prologue,
		InitStatic: kv(is), InitEphemeral: kv(ie), RespStatic: kv(rs), RespEphemeral: kv(re),
		Msg1Payload: []byte("m1"), Msg1: msg1, Msg2Payload: []byte("m2"), Msg2: msg2,
	}
	for _, s := range []struct {
		from string
		tr   *Transport
		msg  string
	}{{"initiator", it, "ls -la\n"}, {"responder", rt, "total 0\n"}, {"initiator", it, ""}} {
		c, err := s.tr.Seal([]byte(s.msg))
		if err != nil {
			t.Fatal(err)
		}
		v.Transport = append(v.Transport, transportVector{From: s.from, Plaintext: []byte(s.msg), Ciphertext: c})
	}
	golden.Check(t, *update, "noise.json", v)
}
