package sign

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/internal/golden"
)

var update = flag.Bool("update", false, "rewrite shared/testdata")

func testKey(fill byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = fill + byte(i)
	}
	return ed25519.NewKeyFromSeed(seed)
}

func TestSignVerify(t *testing.T) {
	priv := testKey(0)
	pub := priv.Public().(ed25519.PublicKey)
	env, err := Sign(priv, PurposeTask, []byte(`{"v":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(env, pub, PurposeTask); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := Verify(env, pub, PurposeConfirm); err != ErrBadSignature {
		t.Errorf("other purpose: %v", err)
	}
	other := testKey(100).Public().(ed25519.PublicKey)
	if err := Verify(env, other, PurposeTask); err != ErrWrongKey {
		t.Errorf("other key: %v", err)
	}
	env.Payload = []byte(`{"v":2}`)
	if err := Verify(env, pub, PurposeTask); err != ErrBadSignature {
		t.Errorf("tampered payload: %v", err)
	}
	env.Payload = make([]byte, MaxPayload+1)
	if err := Verify(env, pub, PurposeTask); err != ErrTooLarge {
		t.Errorf("oversized payload: %v", err)
	}
}

func TestSignTooLarge(t *testing.T) {
	if _, err := Sign(testKey(0), PurposeTask, make([]byte, MaxPayload+1)); err != ErrTooLarge {
		t.Fatalf("got %v", err)
	}
}

func TestSignVerifyEmptyPayload(t *testing.T) {
	priv := testKey(0)
	pub := priv.Public().(ed25519.PublicKey)
	env, err := Sign(priv, PurposeTask, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(env, pub, PurposeTask); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyBadKeyLength(t *testing.T) {
	priv := testKey(0)
	env, err := Sign(priv, PurposeTask, []byte(`{"v":1}`))
	if err != nil {
		t.Fatal(err)
	}
	short := ed25519.PublicKey{1, 2, 3}
	env.KeyID = KeyID(short) // matches, so the KeyID check doesn't hide the length check below it
	if err := Verify(env, short, PurposeTask); err != ErrBadSignature {
		t.Errorf("short key: %v", err)
	}
}

func TestParseEnvelope(t *testing.T) {
	priv := testKey(0)
	env, err := Sign(priv, PurposeLicense, []byte(`{"v":1}`))
	if err != nil {
		t.Fatal(err)
	}
	good, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseEnvelope(good, len(env.Payload))
	if err != nil {
		t.Fatalf("valid envelope: %v", err)
	}
	if !bytes.Equal(got.Payload, env.Payload) || got.KeyID != env.KeyID || !bytes.Equal(got.Sig, env.Sig) {
		t.Fatalf("got %+v, want %+v", got, env)
	}
	if err := Verify(got, priv.Public().(ed25519.PublicKey), PurposeLicense); err != nil {
		t.Errorf("verify the parsed envelope: %v", err)
	}

	payload := `"payload":"` + enc.B64.EncodeToString(env.Payload) + `"`
	kid := `"kid":"` + env.KeyID + `"`
	sig := `"sig":"` + enc.B64.EncodeToString(env.Sig) + `"`
	doc := func(members ...string) []byte { return []byte("{" + strings.Join(members, ",") + "}") }
	for name, in := range map[string][]byte{
		"members in another order": doc(sig, payload, kid),
		"white space":              []byte(" {\n" + payload + " , " + kid + ",\t" + sig + "}\r\n"),
	} {
		if _, err := ParseEnvelope(in, 7); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	cases := []struct {
		name       string
		in         []byte
		maxPayload int
	}{
		{"payload one byte over the bound", good, 6},
		{"bound below 1", good, 0},
		{"empty payload", doc(`"payload":""`, kid, sig), 7},
		{"no payload", doc(kid, sig), 7},
		{"no key ID", doc(payload, sig), 7},
		{"no signature", doc(payload, kid), 7},
		{"key ID one character short", doc(payload, `"kid":"`+env.KeyID[1:]+`"`, sig), 7},
		{"key ID with bits after its 16 bytes", doc(payload, `"kid":"`+strings.Repeat("A", 21)+`B"`, sig), 7},
		{"key ID outside the alphabet", doc(payload, `"kid":"`+strings.Repeat("+", 22)+`"`, sig), 7},
		{"signature of 63 bytes", doc(payload, kid, `"sig":"`+enc.B64.EncodeToString(env.Sig[:63])+`"`), 7},
		{"payload with padding", doc(`"payload":"`+enc.B64.EncodeToString(env.Payload)+`="`, kid, sig), 7},
		{"unknown member", doc(payload, kid, sig, `"x":"y"`), 7},
		{"duplicate member", doc(payload, kid, kid, sig), 7},
		{"member name in another case", doc(payload, `"Kid":"`+env.KeyID+`"`, sig), 7},
		{"null", doc(payload, kid, `"sig":null`), 7},
		{"trailing data", append(bytes.Clone(good), `{}`...), 7},
		{"not an object", []byte(`[]`), 7},
		{"over the size of its budget", append(bytes.Clone(good), bytes.Repeat([]byte(" "), enc.B64.EncodedLen(7)+envelopeSlack)...), 7},
	}
	for _, c := range cases {
		if _, err := ParseEnvelope(c.in, c.maxPayload); err != ErrEnvelope {
			t.Errorf("%s: got %v, want %v", c.name, err, ErrEnvelope)
		}
	}
}

// TestParseEnvelopeBound: a bound above MaxPayload counts as MaxPayload.
func TestParseEnvelopeBound(t *testing.T) {
	env, err := Sign(testKey(0), PurposeRelease, make([]byte, MaxPayload))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseEnvelope(b, 2*MaxPayload); err != nil {
		t.Fatalf("payload of MaxPayload bytes: %v", err)
	}
	env.Payload = make([]byte, MaxPayload+1)
	if b, err = json.Marshal(env); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseEnvelope(b, 2*MaxPayload); err != ErrEnvelope {
		t.Fatalf("payload of MaxPayload+1 bytes: got %v, want %v", err, ErrEnvelope)
	}
}

// TestEnvelopeValidate: one case per rule, each valid apart from its rule,
// for envelopes that reach Validate without ParseEnvelope, inside another
// document.
func TestEnvelopeValidate(t *testing.T) {
	env, err := Sign(testKey(0), PurposeCatalog, []byte(`{"v":1}`))
	if err != nil {
		t.Fatal(err)
	}
	full, err := Sign(testKey(0), PurposeCatalog, make([]byte, MaxPayload))
	if err != nil {
		t.Fatal(err)
	}
	// with returns base changed by change, leaving base as it is.
	with := func(base Envelope, change func(e *Envelope)) Envelope {
		base.Payload, base.Sig = bytes.Clone(base.Payload), bytes.Clone(base.Sig)
		change(&base)
		return base
	}
	cases := []struct {
		name       string
		env        Envelope
		maxPayload int
		valid      bool
	}{
		{"valid", env, 7, true},
		{"payload of MaxPayload bytes, bound above MaxPayload", full, 2 * MaxPayload, true},
		{"payload over MaxPayload, bound above MaxPayload",
			with(full, func(e *Envelope) { e.Payload = append(e.Payload, 0) }), 2 * MaxPayload, false},
		{"empty payload", with(env, func(e *Envelope) { e.Payload = nil }), 7, false},
		{"payload over maxPayload", env, 6, false},
		{"maxPayload 0", env, 0, false},
		{"key ID of 21 characters", with(env, func(e *Envelope) { e.KeyID = e.KeyID[1:] }), 7, false},
		{"key ID not base64url", with(env, func(e *Envelope) { e.KeyID = strings.Repeat("+", 22) }), 7, false},
		{"key ID with bits after its 16 bytes", with(env, func(e *Envelope) { e.KeyID = strings.Repeat("A", 21) + "B" }), 7, false},
		{"signature of 63 bytes", with(env, func(e *Envelope) { e.Sig = e.Sig[:63] }), 7, false},
	}
	for _, c := range cases {
		err := c.env.Validate(c.maxPayload)
		if c.valid && err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if !c.valid && err != ErrEnvelope {
			t.Errorf("%s: got %v, want %v", c.name, err, ErrEnvelope)
		}
	}
}

// allocated returns how many bytes f allocates.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// fill returns prefix, then elem as often as fits, then suffix: exactly size
// bytes. Each elem after the first is written by next(i).
func fill(prefix, suffix string, size int, next func(i int) string) []byte {
	var b bytes.Buffer
	b.Grow(size)
	b.WriteString(prefix + next(0))
	for i := 1; b.Len()+len(next(i))+len(suffix) <= size; i++ {
		b.WriteString(next(i))
	}
	for b.Len()+len(suffix) < size {
		b.WriteByte(' ')
	}
	b.WriteString(suffix)
	return b.Bytes()
}

// TestParseEnvelopeBoundsAllocation feeds ParseEnvelope documents of the full
// size its budget allows at MaxPayload, in shapes the budget refuses:
// refusing them must allocate next to nothing.
func TestParseEnvelopeBoundsAllocation(t *testing.T) {
	size := enc.B64.EncodedLen(MaxPayload) + envelopeSlack
	elems := func(elem string) func(int) string {
		return func(i int) string {
			if i == 0 {
				return elem
			}
			return "," + elem
		}
	}
	distinct := func(i int) string {
		if i == 0 {
			return `"x0":"1"`
		}
		return `,"x` + strconv.Itoa(i) + `":"1"`
	}
	nested := strings.Repeat(`{"a":`, 40) + `1` + strings.Repeat(`}`, 40)
	cases := []struct {
		name string
		in   []byte
	}{
		{"payload as an array", fill(`{"payload":[`, `]}`, size, elems(`{}`))},
		{"array at the top", fill(`[`, `]`, size, elems(`{}`))},
		{"too many members", fill(`{`, `}`, size, distinct)},
		{"nested too deep", append(bytes.Repeat([]byte(" "), size-len(nested)), nested...)},
		{"a member name too long", []byte(`{"` + strings.Repeat("a", size-6) + `":1}`)},
	}
	for _, c := range cases {
		if len(c.in) != size {
			t.Fatalf("%s: %d bytes, want %d", c.name, len(c.in), size)
		}
		var err error
		n := allocated(func() { _, err = ParseEnvelope(c.in, MaxPayload) })
		t.Logf("%s: %d bytes allocated", c.name, n)
		if n > 64<<10 {
			t.Errorf("%s: %d bytes allocated", c.name, n)
		}
		if err != ErrEnvelope {
			t.Errorf("%s: got %v, want %v", c.name, err, ErrEnvelope)
		}
	}
}

func FuzzParseEnvelope(f *testing.F) {
	env, err := Sign(testKey(0), PurposeCatalog, []byte(`{"v":1}`))
	if err != nil {
		f.Fatal(err)
	}
	b, err := json.Marshal(env)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(b, 64)
	f.Fuzz(func(t *testing.T, b []byte, maxPayload int) {
		env, err := ParseEnvelope(b, maxPayload)
		if err != nil {
			return
		}
		// What ParseEnvelope accepts, it accepts again as encoding/json
		// writes it, with the same fields.
		again, err := json.Marshal(env)
		if err != nil {
			t.Fatal(err)
		}
		back, err := ParseEnvelope(again, maxPayload)
		if err != nil || !bytes.Equal(back.Payload, env.Payload) || back.KeyID != env.KeyID || !bytes.Equal(back.Sig, env.Sig) {
			t.Fatalf("%s does not read back: %v", again, err)
		}
	})
}

type signVector struct {
	Seed     enc.Bytes `json:"seed"`
	Public   enc.Bytes `json:"public"`
	Purpose  string    `json:"purpose"`
	Message  enc.Bytes `json:"message"`
	Envelope Envelope  `json:"envelope"`
}

func TestVectors(t *testing.T) {
	priv := testKey(0)
	payload := []byte(`{"v":1,"seq":1}`)
	env, err := Sign(priv, PurposeTrustList, payload)
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, *update, "sign.json", signVector{
		Seed:     priv.Seed(),
		Public:   []byte(priv.Public().(ed25519.PublicKey)),
		Purpose:  PurposeTrustList,
		Message:  Message(PurposeTrustList, payload),
		Envelope: env,
	})
}
