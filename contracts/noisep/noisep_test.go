package noisep

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"slices"
	"testing"

	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
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

// testTask returns a task envelope as the shell writes it into an OPEN: the
// JSON of a sign.Envelope, signed with a fixed key. The prologue only hashes
// its bytes, so the payload need not be a real task.
func testTask(t testing.TB, payload string) []byte {
	t.Helper()
	env, err := sign.Sign(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x42}, ed25519.SeedSize)), sign.PurposeTask, []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestE2EPrologue(t *testing.T) {
	const target = "ngc/e2e/v1\x00ctl-svc\x00org.sorglos.terminal\x00terminal\x00shell\x00"
	emptyHash, _ := hex.DecodeString("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	prologue := func(params, task []byte) []byte {
		return E2EPrologue("ctl-svc", "org.sorglos.terminal", "terminal", "shell", params, task)
	}
	task := testTask(t, `{"v":1}`)
	ph, th := sha256.Sum256(terminalParams), sha256.Sum256(task)
	for _, c := range []struct {
		name         string
		params, task []byte
		want         []byte
	}{
		{"params, no task", terminalParams, nil, slices.Concat([]byte(target), ph[:], emptyHash)},
		{"task, no params", nil, task, slices.Concat([]byte(target), emptyHash, th[:])},
		{"empty members hash the empty string", []byte{}, []byte{}, slices.Concat([]byte(target), emptyHash, emptyHash)},
		{"absent members hash the empty string", nil, nil, slices.Concat([]byte(target), emptyHash, emptyHash)},
	} {
		if got := prologue(c.params, c.task); !bytes.Equal(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}

	// The responder (controller) builds its prologue from the OPEN it got;
	// anything that differs from what the shell hashed breaks message 1.
	t.Run("other params or task break the handshake", func(t *testing.T) {
		is, rs := fixedKey(t, 1), fixedKey(t, 2)
		other := testTask(t, `{"v":2}`)
		spaced := bytes.ReplaceAll(task, []byte(`,`), []byte(`, `))
		for _, c := range []struct {
			name              string
			shell, controller [2][]byte // params, task
			ok                bool
		}{
			{"same params", [2][]byte{terminalParams, nil}, [2][]byte{terminalParams, nil}, true},
			{"same task", [2][]byte{nil, task}, [2][]byte{nil, task}, true},
			{"other params value", [2][]byte{terminalParams, nil}, [2][]byte{[]byte(`{"cols":81,"rows":24}`), nil}, false},
			{"same params, other bytes", [2][]byte{terminalParams, nil}, [2][]byte{[]byte(`{"cols":80, "rows":24}`), nil}, false},
			{"params only on one side", [2][]byte{terminalParams, nil}, [2][]byte{nil, nil}, false},
			{"other task", [2][]byte{nil, task}, [2][]byte{nil, other}, false},
			{"same task, other bytes", [2][]byte{nil, task}, [2][]byte{nil, spaced}, false},
			{"task added", [2][]byte{nil, nil}, [2][]byte{nil, task}, false},
			{"task dropped", [2][]byte{nil, task}, [2][]byte{nil, nil}, false},
			{"task bytes given as params", [2][]byte{nil, task}, [2][]byte{task, nil}, false},
		} {
			init, err := Handshake(true, prologue(c.shell[0], c.shell[1]), is, rs.Public, rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			msg1, _, _, err := init.WriteMessage(nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := Handshake(false, prologue(c.controller[0], c.controller[1]), rs, is.Public, rand.Reader)
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

// prologueVector is one OPEN as the shell wrote it, with the prologue its
// params and task members give and message 1 over that prologue (the keys of
// the main vector). The OPEN's e2e member is that message 1.
type prologueVector struct {
	Name         string    `json:"name"`
	ControllerID string    `json:"controllerId"`
	Open         enc.Bytes `json:"open"`
	Prologue     enc.Bytes `json:"prologue"`
	Msg1         enc.Bytes `json:"msg1"`
}

type noiseVector struct {
	Protocol      string            `json:"protocol"`
	ControllerID  string            `json:"controllerId"`
	App           string            `json:"app"`
	Service       string            `json:"service"`
	Action        string            `json:"action"`
	Params        enc.Bytes         `json:"params"`
	Task          enc.Bytes         `json:"task"`
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
	Prologues     []prologueVector  `json:"prologues"`
}

func kv(k noise.DHKey) keyVector { return keyVector{Private: k.Private, Public: k.Public} }

// prologueVectors builds one OPEN per rule of E2EPrologue that an
// implementation could get wrong, each differing from a neighbour in that
// rule alone, and checks that the sloppy reading gives another prologue.
func prologueVectors(t *testing.T, is, ie, rs noise.DHKey) []prologueVector {
	const ctl = "ctl-svc"
	task := testTask(t, `{"v":1}`)
	spacedTask := bytes.ReplaceAll(bytes.ReplaceAll(task, []byte(`:`), []byte(`: `)), []byte(`,`), []byte(`, `))
	cases := []struct {
		name          string
		params, task  []byte // the members as written; nil: left out
		sloppy        [2][]byte
		sloppyReading string
	}{
		{"params", terminalParams, nil, [2][]byte{terminalParams, []byte(`{}`)}, "absent task hashed as {}"},
		{"params with white space", []byte(`{ "cols": 80, "rows": 24 }`), nil, [2][]byte{terminalParams, nil}, "params re-encoded"},
		{"no params", nil, nil, [2][]byte{[]byte(`{}`), nil}, "absent params hashed as {}"},
		{"empty params", []byte(`{}`), nil, [2][]byte{nil, nil}, "{} taken for absent params"},
		{"task", nil, task, [2][]byte{nil, nil}, "task left out of the prologue"},
		{"task with white space", nil, spacedTask, [2][]byte{nil, task}, "task re-encoded"},
	}
	var vs []prologueVector
	for _, c := range cases {
		prologue := E2EPrologue(ctl, "org.sorglos.terminal", "terminal", "shell", c.params, c.task)
		if bytes.Equal(prologue, E2EPrologue(ctl, "org.sorglos.terminal", "terminal", "shell", c.sloppy[0], c.sloppy[1])) {
			t.Fatalf("%s: the vector does not catch %s", c.name, c.sloppyReading)
		}
		init, err := newHandshake(true, prologue, is, rs.Public, rand.Reader, ie)
		if err != nil {
			t.Fatal(err)
		}
		msg1, _, _, err := init.WriteMessage(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		open := `{"app":"org.sorglos.terminal","service":"terminal","action":"shell"`
		if c.params != nil {
			open += `,"params":` + string(c.params)
		}
		open += `,"window":262144,"e2e":"` + enc.B64.EncodeToString(msg1) + `"`
		if c.task != nil {
			open += `,"task":` + string(c.task)
		}
		open += `}`
		// The controller's side: the members as wire.ParseOpen keeps them.
		o, err := wire.ParseOpen([]byte(open))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !bytes.Equal(E2EPrologue(ctl, o.App, o.Service, o.Action, o.Params, o.TaskBytes()), prologue) {
			t.Fatalf("%s: the controller's prologue differs", c.name)
		}
		vs = append(vs, prologueVector{Name: c.name, ControllerID: ctl, Open: []byte(open), Prologue: prologue, Msg1: msg1})
	}
	return vs
}

// TestVectors writes noise.json: an end-to-end handshake for the terminal
// OPEN of wire.json, both messages without payload, then one inner message
// (wire.E2E) per transport message, as shell and controller exchange them;
// and the prologues of OPENs with and without params and task.
func TestVectors(t *testing.T) {
	is, ie, rs, re := fixedKey(t, 1), fixedKey(t, 2), fixedKey(t, 3), fixedKey(t, 4)
	v := noiseVector{
		Protocol:     "Noise_KK_25519_AESGCM_SHA256",
		ControllerID: "ctl-svc", App: "org.sorglos.terminal", Service: "terminal", Action: "shell", Params: terminalParams,
		InitStatic: kv(is), InitEphemeral: kv(ie), RespStatic: kv(rs), RespEphemeral: kv(re),
	}
	v.Prologue = E2EPrologue(v.ControllerID, v.App, v.Service, v.Action, v.Params, v.Task)
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
	v.Prologues = prologueVectors(t, is, ie, rs)
	golden.Check(t, *update, "noise.json", v)
}
