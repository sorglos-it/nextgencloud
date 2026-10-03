package e2e

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

const (
	// node is the controller of trusttest.Genesis these tests run on; the
	// prologue binds its ID.
	node = "ctl-1"
	// The channel shared/testdata/noise.json writes its vectors for.
	vecApp     = "org.sorglos.terminal"
	vecService = "terminal"
	vecAction  = "shell"

	issued = 1790000000
	now    = issued + 10
)

// terminalParams are the params of the vectors' OPEN, byte for byte as the
// shell writes them: the prologue hashes exactly these.
var terminalParams = []byte(`{"cols":80,"rows":24}`)

// maxVector bounds shared/testdata/noise.json while it is read. The file
// needs about 6 KiB; the room is generous so a new case does not fail the
// test for its size, and it still keeps a damaged file from being decoded.
const maxVector = 1 << 20

type keyVector struct {
	Private enc.Bytes `json:"private"`
	Public  enc.Bytes `json:"public"`
}

type transportVector struct {
	From       string    `json:"from"`
	Plaintext  enc.Bytes `json:"plaintext"`
	Ciphertext enc.Bytes `json:"ciphertext"`
}

type prologueVector struct {
	Name         string    `json:"name"`
	ControllerID string    `json:"controllerId"`
	Open         enc.Bytes `json:"open"`
	Params       enc.Bytes `json:"params"`
	Task         enc.Bytes `json:"task"`
	Prologue     enc.Bytes `json:"prologue"`
	Msg1         enc.Bytes `json:"msg1"`
}

// noiseVector mirrors shared/testdata/noise.json. The file is decoded
// strictly, so every member it has stands here.
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

// vectors reads shared/testdata/noise.json.
func vectors(t testing.TB) noiseVector {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "shared", "testdata", "noise.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v noiseVector
	if err := enc.DecodeStrict(b, maxVector, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Prologues) == 0 || len(v.Transport) == 0 {
		t.Fatal("noise.json holds no case")
	}
	return v
}

// key turns a key of the file into a Noise key pair.
func key(k keyVector) noise.DHKey { return noise.DHKey{Private: k.Private, Public: k.Public} }

// opened wraps an OPEN payload the way the API forwards it and runs it
// through tunnel.VerifyOpen, so its session is one session.VerifyClient made
// and its Params and TaskBytes are the bytes of the payload. dh is the Noise
// key of the certificate: the peer key of the end-to-end handshake.
func opened(t testing.TB, payload, dh []byte) tunnel.Opened {
	t.Helper()
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	sessSig := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x51}, ed25519.SeedSize))
	client := mustJSON(t, session.ClientCert{Installation: l.Installation, User: trusttest.ID(0x21),
		Nonce: bytes.Repeat([]byte{0x53}, session.NonceSize), Sig: enc.Bytes(sessSig.Public().(ed25519.PublicKey)),
		DH: dh, Issued: issued, Expires: issued + 3600})
	p := admin.Prove(sign.PurposeClientCert, client)
	o, err := tunnel.VerifyOpen(mustJSON(t, tunnel.Open{Open: payload, Window: wire.DefaultWindow,
		Client: client, Proof: &p}), l, now)
	if err != nil {
		t.Fatalf("the test OPEN does not verify: %v", err)
	}
	return o
}

// openJSON writes an OPEN by hand, the way a shell does: params and task
// stand in it exactly as given, because those are the bytes the prologue
// hashes — and only writing the OPEN itself puts white space where the
// vectors have it.
func openJSON(params, task, msg1 []byte) []byte {
	b := []byte(`{"app":"` + vecApp + `","service":"` + vecService + `","action":"` + vecAction + `"`)
	if params != nil {
		b = append(append(b, `,"params":`...), params...)
	}
	b = append(b, `,"window":`+strconv.Itoa(wire.DefaultWindow)+`,"e2e":"`...)
	b = append(append(b, enc.B64.EncodeToString(msg1)...), '"')
	if task != nil {
		b = append(append(b, `,"task":`...), task...)
	}
	return append(b, '}')
}

// shellOpen plays the shell: it builds message 1 for the prologue of this
// OPEN and returns the OPEN payload and the handshake waiting for message 2.
func shellOpen(t testing.TB, client, ctl noise.DHKey, params, task []byte) ([]byte, *noise.HandshakeState) {
	t.Helper()
	hs, err := noisep.Handshake(true, noisep.E2EPrologue(node, vecApp, vecService, vecAction, params, task),
		client, ctl.Public, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return openJSON(params, task, msg1), hs
}

// finish completes the shell's side with message 2 and returns its transport.
func finish(t testing.TB, hs *noise.HandshakeState, msg2 []byte) *noisep.Transport {
	t.Helper()
	payload, cs1, cs2, err := hs.ReadMessage(nil, msg2)
	if err != nil || len(payload) != 0 {
		t.Fatalf("message 2: %v, payload %d bytes", err, len(payload))
	}
	return noisep.NewTransport(true, cs1, cs2)
}

// channel is an accepted channel with both ends live: the controller's
// session and the shell's transport.
func channel(t testing.TB, params, task []byte) (*Session, *noisep.Transport) {
	t.Helper()
	client, _ := trusttest.NodeKeys(0x52)
	ctl, _ := trusttest.NodeKeys(0xC1)
	payload, hs := shellOpen(t, client, ctl, params, task)
	s, msg2, err := Accept(opened(t, payload, client.Public), node, ctl)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	return s, finish(t, hs, msg2)
}

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// testTask returns a task envelope as the shell writes it into an OPEN,
// signed by the key of the certificate: a task valid on its own. The
// prologue only hashes its bytes, so the payload need not be a real task.
func testTask(t testing.TB, payload string) []byte {
	t.Helper()
	env, err := sign.Sign(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x51}, ed25519.SeedSize)),
		sign.PurposeTask, []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	return mustJSON(t, env)
}

func TestAcceptMatchesTheVectors(t *testing.T) {
	v := vectors(t)
	for _, c := range v.Prologues {
		t.Run(c.Name, func(t *testing.T) {
			o := opened(t, c.Open, v.InitStatic.Public)
			// The prologue this controller builds from the OPEN it was
			// handed has to be the one the file wrote.
			got := noisep.E2EPrologue(c.ControllerID, o.Open.App, o.Open.Service, o.Open.Action,
				o.Open.Params, o.Open.TaskBytes())
			if !bytes.Equal(got, c.Prologue) {
				t.Errorf("prologue %x, want %x", got, c.Prologue)
			}
			if !bytes.Equal(o.Open.E2E, c.Msg1) {
				t.Fatalf("the OPEN's e2e is not message 1 of the case")
			}
			// Message 1 of the file only opens under that prologue and the
			// keys of the file, so a successful Accept proves both.
			s, msg2, err := Accept(o, c.ControllerID, key(v.RespStatic))
			if err != nil {
				t.Fatalf("Accept: %v", err)
			}
			if len(msg2) != wire.HandshakeSize {
				t.Errorf("message 2 has %d bytes, want %d", len(msg2), wire.HandshakeSize)
			}
			if s.Sent() != 0 {
				t.Errorf("Sent = %d on a fresh session, want 0", s.Sent())
			}
		})
	}
}

func TestPrologueBindsParams(t *testing.T) {
	v := vectors(t)
	client, ctl := key(v.InitStatic), key(v.RespStatic)
	payload, _ := shellOpen(t, client, ctl, terminalParams, nil)
	if _, _, err := Accept(opened(t, payload, client.Public), node, ctl); err != nil {
		t.Fatalf("Accept of the OPEN the shell wrote: %v", err)
	}
	// The same OPEN with one byte of params changed: 80 columns become 81.
	// Message 1 stays the one the shell wrote for 80.
	changed := bytes.Replace(payload, []byte(`"cols":80`), []byte(`"cols":81`), 1)
	if bytes.Equal(changed, payload) {
		t.Fatal("the test changed nothing")
	}
	if _, _, err := Accept(opened(t, changed, client.Public), node, ctl); err == nil {
		t.Fatal("Accept took an OPEN whose params were changed under the handshake")
	}
}

func TestPrologueBindsTask(t *testing.T) {
	v := vectors(t)
	client, ctl := key(v.InitStatic), key(v.RespStatic)
	mine, other := testTask(t, `{"v":1}`), testTask(t, `{"v":2}`)
	payload, _ := shellOpen(t, client, ctl, nil, mine)
	if _, _, err := Accept(opened(t, payload, client.Public), node, ctl); err != nil {
		t.Fatalf("Accept of the OPEN the shell wrote: %v", err)
	}
	// Another task of the same user, valid on its own, put in place of the
	// one the shell signed its handshake over.
	swapped := bytes.Replace(payload, mine, other, 1)
	if bytes.Equal(swapped, payload) {
		t.Fatal("the test swapped nothing")
	}
	if _, _, err := Accept(opened(t, swapped, client.Public), node, ctl); err == nil {
		t.Fatal("Accept took an OPEN with another task under the handshake")
	}
}

func TestEndCarriesTheOutcome(t *testing.T) {
	v := vectors(t)
	s, shell := channel(t, terminalParams, nil)
	ct, err := s.End(wire.Close{Status: wire.StatusOK})
	if err != nil {
		t.Fatalf("End: %v", err)
	}
	plain, err := shell.Open(ct.WholePayload())
	if err != nil {
		t.Fatalf("the shell cannot open the end: %v", err)
	}
	m, err := wire.ParseE2E(plain, true)
	if err != nil {
		t.Fatalf("ParseE2E: %v", err)
	}
	if m.Kind != wire.E2EEnd || m.Status.Status != wire.StatusOK {
		t.Errorf("kind %d status %q, want %d and %q", m.Kind, m.Status.Status, wire.E2EEnd, wire.StatusOK)
	}
	// The same inner message the file holds for the controller's end.
	if want := lastFrom(t, v, "responder"); !bytes.Equal(plain, want) {
		t.Errorf("inner message %x, the vector has %x", plain, want)
	}
}

// lastFrom returns the plaintext of the last transport message of that side.
func lastFrom(t testing.TB, v noiseVector, from string) []byte {
	t.Helper()
	var last []byte
	for _, m := range v.Transport {
		if m.From == from {
			last = m.Plaintext
		}
	}
	if last == nil {
		t.Fatalf("noise.json holds no transport message from the %s", from)
	}
	return last
}

func TestNothingAfterOwnEnd(t *testing.T) {
	s, _ := channel(t, terminalParams, nil)
	if _, err := s.End(wire.Close{Status: wire.StatusOK}); err != nil {
		t.Fatalf("End: %v", err)
	}
	if _, err := s.Data([]byte("late\n")); err == nil {
		t.Error("Data wrote after the end")
	}
	if _, err := s.End(wire.Close{Status: wire.StatusOK}); err == nil {
		t.Error("End came twice")
	}
}

func TestSentCountsTheOutput(t *testing.T) {
	s, _ := channel(t, terminalParams, nil)
	for i, b := range [][]byte{[]byte("total 0\n"), nil, []byte("x")} {
		if _, err := s.Data(b); err != nil {
			t.Fatalf("Data %d: %v", i, err)
		}
	}
	if s.Sent() != 9 {
		t.Errorf("Sent = %d, want 9", s.Sent())
	}
	// Only the output counts, so the end does not move the number the
	// caller holds wire.MaxCallResult against.
	if _, err := s.End(wire.Close{Status: wire.StatusOK}); err != nil {
		t.Fatal(err)
	}
	if s.Sent() != 9 {
		t.Errorf("Sent = %d after the end, want 9", s.Sent())
	}
}

func TestDataStopsAboveTheLimit(t *testing.T) {
	s, shell := channel(t, terminalParams, nil)
	// One byte of the plaintext is the kind, so this is the largest Data
	// that still fits, and one more is refused.
	ct, err := s.Data(make([]byte, wire.MaxE2EPlaintext-1))
	if err != nil {
		t.Fatalf("Data at the limit: %v", err)
	}
	plain, err := shell.Open(ct.WholePayload())
	if err != nil {
		t.Fatalf("the shell cannot open it: %v", err)
	}
	m, err := wire.ParseE2E(plain, false)
	if err != nil || m.Kind != wire.E2EData || len(m.Data) != wire.MaxE2EPlaintext-1 {
		t.Fatalf("ParseE2E = %d, %d bytes, %v", m.Kind, len(m.Data), err)
	}
	if _, err := s.Data(make([]byte, wire.MaxE2EPlaintext)); err == nil {
		t.Error("Data wrote a plaintext above wire.MaxE2EPlaintext")
	}
	if s.Sent() != wire.MaxE2EPlaintext-1 {
		t.Errorf("Sent = %d, want %d: what was refused does not count", s.Sent(), wire.MaxE2EPlaintext-1)
	}
}

func TestOpenReadsTheShellsMessages(t *testing.T) {
	v := vectors(t)
	s, shell := channel(t, terminalParams, nil)
	// The inner message the file holds for the shell's first DATA.
	data := v.Transport[0].Plaintext
	ct, err := shell.Seal(data)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Open(ct)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if m.Kind != wire.E2EData || !bytes.Equal(m.Data, data[1:]) {
		t.Errorf("kind %d data %q, want %d and %q", m.Kind, m.Data, wire.E2EData, data[1:])
	}
	// Nothing the shell did not seal opens.
	if _, err := s.Open(bytes.Repeat([]byte{0}, len(ct))); err == nil {
		t.Error("Open took a ciphertext of nobody")
	}
}

func TestNothingAfterTheShellsEnd(t *testing.T) {
	s, shell := channel(t, terminalParams, nil)
	end, err := wire.E2E(wire.E2EEnd, nil)
	if err != nil {
		t.Fatal(err)
	}
	ct, err := shell.Seal(end)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Open(ct)
	if err != nil || m.Kind != wire.E2EEnd {
		t.Fatalf("Open of the shell's end = %d, %v", m.Kind, err)
	}
	// The shell's input is over; another inner message from it is a
	// protocol error, while the controller's own output goes on.
	more, err := wire.E2E(wire.E2EData, []byte("late\n"))
	if err != nil {
		t.Fatal(err)
	}
	ct, err = shell.Seal(more)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(ct); err == nil {
		t.Error("Open took a message after the shell's own end")
	}
	if _, err := s.Data([]byte("total 0\n")); err != nil {
		t.Errorf("the controller may still write after the shell's end: %v", err)
	}
}

func TestReaderAndWriterRunAtOnce(t *testing.T) {
	// The doc of Session says a goroutine that reads and one that writes may
	// run at the same time. Everything is sealed before they start, so what
	// they share is the session alone; go test -race is what makes this bite.
	const n = 64
	s, shell := channel(t, terminalParams, nil)
	plain, err := wire.E2E(wire.E2EData, []byte("ls -la\n"))
	if err != nil {
		t.Fatal(err)
	}
	in := make([][]byte, n)
	for i := range in {
		if in[i], err = shell.Seal(plain); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	// Both goroutines wait on start, so neither begins before the other is
	// released: without it the first may be through before the second has
	// even been spawned. It does not force an overlap — close(start) frees
	// both, and the scheduler may still run all 64 rounds of the one before
	// the other — it only makes one likely enough for the detector to see.
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for _, ct := range in {
			if _, err := s.Open(ct); err != nil {
				errs[0] = err
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for range n {
			if _, err := s.Data([]byte("total 0\n")); err != nil {
				errs[1] = err
				return
			}
		}
	}()
	close(start)
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("reading %v, writing %v", errs[0], errs[1])
	}
	if s.Sent() != n*8 {
		t.Errorf("Sent = %d, want %d", s.Sent(), n*8)
	}
}

func TestAcceptNeedsASession(t *testing.T) {
	v := vectors(t)
	o, err := wire.ParseOpen(v.Prologues[0].Open)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Accept(tunnel.Opened{Open: o, Window: wire.DefaultWindow}, node, key(v.RespStatic)); err == nil {
		t.Fatal("Accept opened a channel without a session")
	}
}

func TestAcceptRefusesAMessageOneOfAnotherSize(t *testing.T) {
	v := vectors(t)
	client, ctl := key(v.InitStatic), key(v.RespStatic)
	// Message 1 with a payload: Noise would read it, the contract does not.
	hs, err := noisep.Handshake(true, noisep.E2EPrologue(node, vecApp, vecService, vecAction, terminalParams, nil),
		client, ctl.Public, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	msg1, _, _, err := hs.WriteMessage(nil, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if len(msg1) == wire.HandshakeSize {
		t.Fatal("message 1 with a payload is not longer")
	}
	// wire.ParseOpen keeps such an OPEN out — measured here, because the doc
	// of ErrChannel rests on it — so the Opened is built by hand the way a
	// caller of this package could hand one over.
	if _, err := wire.ParseOpen(openJSON(terminalParams, nil, msg1)); err == nil {
		t.Fatal("wire.ParseOpen read an app OPEN whose message 1 has another size")
	}
	o := opened(t, openJSON(terminalParams, nil, make([]byte, wire.HandshakeSize)), client.Public)
	o.Open.E2E = msg1
	if _, _, err := Accept(o, node, ctl); err == nil {
		t.Fatal("Accept read a message 1 that carried a payload")
	}
}

// TestAcceptCannotSayWhoseFaultTheHandshakeWas builds three of the ways
// message 1 fails to open — one of them the shell's, two of them this
// controller's own configuration — and holds all three to the one value
// ErrHandshake. The doc of that error rests on this: as Accept stands,
// nothing here tells them apart, so the caller may not read a protocol error
// of the shell out of it.
//
// The name promises more than the measurement, and the doc of ErrHandshake
// says which part: our own two are catchable where the configuration is
// loaded, and belong there. What is left over for Accept is the shell's
// damaged message 1 and an OPEN the API put on the wrong controller — those
// two do look alike from here. Whoever tells them apart in Accept fails
// here, and changes that doc with it.
func TestAcceptCannotSayWhoseFaultTheHandshakeWas(t *testing.T) {
	client, _ := trusttest.NodeKeys(0x52)
	ctl, _ := trusttest.NodeKeys(0xC1)
	payload, _ := shellOpen(t, client, ctl, terminalParams, nil)
	// The shell's doing: one flipped bit in message 1.
	damaged := opened(t, payload, client.Public)
	damaged.Open.E2E = bytes.Clone(damaged.Open.E2E)
	damaged.Open.E2E[0] ^= 1
	_, _, shellsFault := Accept(damaged, node, ctl)
	// Ours: a static key of this controller that is no X25519 key at all.
	_, _, shortKey := Accept(opened(t, payload, client.Public), node,
		noise.DHKey{Private: ctl.Private[:16], Public: ctl.Public})
	// Ours again: the right key, the wrong node ID, so this controller hashes
	// another prologue than the shell. A check does catch this one, only not
	// in here — our static key is what our node ID carries in the trust list
	// (trust.Node.Noise). From inside Accept it is bit for bit the API
	// putting another controller's OPEN on us, and that one is not ours.
	_, _, wrongNode := Accept(opened(t, payload, client.Public), "ctl-2", ctl)
	for _, c := range []struct {
		name string
		err  error
	}{{"a damaged message 1", shellsFault}, {"a static key of 16 bytes", shortKey}, {"the wrong node ID", wrongNode}} {
		if !errors.Is(c.err, ErrHandshake) {
			t.Fatalf("%s gives %v, want ErrHandshake", c.name, c.err)
		}
	}
}

// TestEndRefusesAnOutcomeThatIsNoClose guards what seal promises: End goes
// through wire.E2E, which reads the outcome as a CLOSE payload, so no
// malformed end reaches the wire. An End that built its inner message itself
// would let all three of these through.
func TestEndRefusesAnOutcomeThatIsNoClose(t *testing.T) {
	for _, cl := range []wire.Close{
		{},                // no status at all
		{Status: "weird"}, // a status wire does not know
		{Status: wire.StatusOK, Code: wire.CodeProtocol}, // ok carries no code
	} {
		s, _ := channel(t, terminalParams, nil)
		ct, err := s.End(cl)
		if err == nil {
			t.Errorf("End(%+v) sealed %d bytes, want a refusal", cl, len(ct.WholePayload()))
		}
	}
}

// TestEachFailureNamesItsOwnError holds the four errors apart. Which CLOSE
// the caller ends the channel with hangs on them alone (see their doc), so a
// site answering with the wrong one — or two of them that are one value —
// sends the caller the wrong way. The wanted error is named, not compared by
// value: two that were made equal have to fail here, not cancel out.
func TestEachFailureNamesItsOwnError(t *testing.T) {
	named := []struct {
		name string
		err  error
	}{{"ErrChannel", ErrChannel}, {"ErrHandshake", ErrHandshake}, {"ErrEnded", ErrEnded}, {"ErrMessage", ErrMessage}}
	// inner builds one inner message of the shell, sealed for s.
	inner := func(t testing.TB, shell *noisep.Transport, kind byte, body []byte) []byte {
		t.Helper()
		plain, err := wire.E2E(kind, body)
		if err != nil {
			t.Fatal(err)
		}
		ct, err := shell.Seal(plain)
		if err != nil {
			t.Fatal(err)
		}
		return ct
	}
	// closed is a session whose own end has gone out.
	closed := func(t testing.TB) *Session {
		t.Helper()
		s, _ := channel(t, terminalParams, nil)
		if _, err := s.End(wire.Close{Status: wire.StatusOK}); err != nil {
			t.Fatal(err)
		}
		return s
	}
	// afterTheShellsEnd is a session that has read the shell's end.
	afterTheShellsEnd := func(t testing.TB) (*Session, *noisep.Transport) {
		t.Helper()
		s, shell := channel(t, terminalParams, nil)
		if _, err := s.Open(inner(t, shell, wire.E2EEnd, nil)); err != nil {
			t.Fatal(err)
		}
		return s, shell
	}
	cases := []struct {
		name string
		want string
		run  func(t *testing.T) error
	}{
		{"a channel without a session", "ErrChannel", func(t *testing.T) error {
			v := vectors(t)
			o, err := wire.ParseOpen(v.Prologues[0].Open)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = Accept(tunnel.Opened{Open: o, Window: wire.DefaultWindow}, node, key(v.RespStatic))
			return err
		}},
		{"a message 1 of another size", "ErrChannel", func(t *testing.T) error {
			client, _ := trusttest.NodeKeys(0x52)
			ctlKey, _ := trusttest.NodeKeys(0xC1)
			payload, _ := shellOpen(t, client, ctlKey, terminalParams, nil)
			o := opened(t, payload, client.Public)
			o.Open.E2E = append(bytes.Clone(o.Open.E2E), 'x')
			_, _, err := Accept(o, node, ctlKey)
			return err
		}},
		{"a damaged message 1", "ErrHandshake", func(t *testing.T) error {
			client, _ := trusttest.NodeKeys(0x52)
			ctlKey, _ := trusttest.NodeKeys(0xC1)
			payload, _ := shellOpen(t, client, ctlKey, terminalParams, nil)
			o := opened(t, payload, client.Public)
			o.Open.E2E = bytes.Clone(o.Open.E2E)
			o.Open.E2E[0] ^= 1
			_, _, err := Accept(o, node, ctlKey)
			return err
		}},
		{"the shell writes after its own end", "ErrEnded", func(t *testing.T) error {
			s, shell := afterTheShellsEnd(t)
			_, err := s.Open(inner(t, shell, wire.E2EData, []byte("late\n")))
			return err
		}},
		{"Data after our own end", "ErrEnded", func(t *testing.T) error {
			_, err := closed(t).Data([]byte("late\n"))
			return err
		}},
		{"End after our own end", "ErrEnded", func(t *testing.T) error {
			_, err := closed(t).End(wire.Close{Status: wire.StatusOK})
			return err
		}},
		{"a ciphertext nobody sealed", "ErrMessage", func(t *testing.T) error {
			s, _ := channel(t, terminalParams, nil)
			_, err := s.Open(bytes.Repeat([]byte{0}, wire.HandshakeSize))
			return err
		}},
		{"a plaintext that is no inner message", "ErrMessage", func(t *testing.T) error {
			s, shell := channel(t, terminalParams, nil)
			// Kind 9 is neither E2EData nor E2EEnd, so wire.E2E would not
			// build it and the shell has to seal it by hand.
			ct, err := shell.Seal([]byte{9})
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.Open(ct)
			return err
		}},
		{"Data above the plaintext limit", "ErrMessage", func(t *testing.T) error {
			s, _ := channel(t, terminalParams, nil)
			_, err := s.Data(make([]byte, wire.MaxE2EPlaintext))
			return err
		}},
		{"an end that is no CLOSE", "ErrMessage", func(t *testing.T) error {
			s, _ := channel(t, terminalParams, nil)
			_, err := s.End(wire.Close{Status: "weird"})
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.run(t)
			if err == nil {
				t.Fatalf("no error, want %s", c.want)
			}
			for _, n := range named {
				if got, want := errors.Is(err, n.err), n.name == c.want; got != want {
					t.Errorf("errors.Is(err, %s) = %v, want %v (err is %v)", n.name, got, want, err)
				}
			}
		})
	}
}

func FuzzInner(f *testing.F) {
	f.Add([]byte{1})
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = wire.ParseE2E(b, false) })
}

func FuzzOpenCiphertext(f *testing.F) {
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{0}, wire.HandshakeSize))
	f.Fuzz(func(t *testing.T, b []byte) {
		// A fresh session per input: a failed Open leaves the receiving
		// nonce where it was, but nothing here may depend on that.
		s, _ := channel(t, terminalParams, nil)
		_, _ = s.Open(b)
	})
}
