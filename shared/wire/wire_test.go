package wire

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/internal/golden"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
)

var update = flag.Bool("update", false, "rewrite shared/testdata")

func testOpen() Open {
	return Open{
		App:     "org.sorglos.terminal",
		Service: "terminal",
		Action:  "shell",
		Params:  json.RawMessage(`{"cols":80,"rows":24}`),
		Window:  DefaultWindow,
		E2E:     bytes.Repeat([]byte{7}, HandshakeSize),
	}
}

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// taskEnvelope returns a task envelope as the shell sends it, signed with a
// fixed key; wire checks only its sizes.
func taskEnvelope(t testing.TB) *sign.Envelope {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x42}, ed25519.SeedSize))
	env, err := sign.Sign(priv, sign.PurposeTask, []byte(`{"v":1}`))
	if err != nil {
		t.Fatal(err)
	}
	return &env
}

// confirmProof returns a password proof of the right shape; wire checks only
// its structure (proof.Proof.Validate).
func confirmProof() *proof.Proof {
	return &proof.Proof{Kind: proof.KindPassword, KeyID: enc.B64.EncodeToString(bytes.Repeat([]byte{0x43}, 16)),
		Sig: bytes.Repeat([]byte{0x44}, ed25519.SignatureSize)}
}

func TestFrameRoundTrip(t *testing.T) {
	open := mustJSON(t, testOpen())
	closeOK := mustJSON(t, Close{Status: StatusOK})
	for _, f := range []Frame{
		{Type: TypeOpen, Channel: 1, Payload: open},
		{Type: TypeData, Channel: 1, Payload: []byte("ls -la\n")},
		{Type: TypeData, Channel: ControlChannel, Payload: []byte(`{"t":"bye"}`)},
		{Type: TypeEOF, Channel: 1},
		{Type: TypeClose, Channel: 1, Payload: closeOK},
		{Type: TypeGrant, Channel: 1, Payload: Grant(DefaultWindow, nil)},
		{Type: TypeGrant, Channel: 1, Payload: Grant(DefaultWindow, bytes.Repeat([]byte{9}, HandshakeSize))},
	} {
		b, err := f.Encode()
		if err != nil {
			t.Fatalf("encode %v: %v", f.Type, err)
		}
		g, err := Decode(b)
		if err != nil {
			t.Fatalf("decode %v: %v", f.Type, err)
		}
		if g.Type != f.Type || g.Channel != f.Channel || !bytes.Equal(g.Payload, f.Payload) {
			t.Fatalf("round trip %v: %+v", f.Type, g)
		}
	}
}

func TestFrameRules(t *testing.T) {
	bad := []Frame{
		{Type: 9, Channel: 1},
		{Type: TypeOpen, Channel: ControlChannel, Payload: []byte("{}")},
		{Type: TypeOpen, Channel: 1},
		{Type: TypeOpen, Channel: 1, Payload: make([]byte, MaxOpenPayload+1)},
		{Type: TypeEOF, Channel: 1, Payload: []byte("x")},
		{Type: TypeEOF, Channel: ControlChannel},
		{Type: TypeClose, Channel: 1},
		{Type: TypeClose, Channel: 1, Payload: make([]byte, MaxClosePayload+1)},
		{Type: TypeClose, Channel: ControlChannel, Payload: []byte("{}")},
		{Type: TypeGrant, Channel: 1, Payload: []byte{1, 2}},
		{Type: TypeGrant, Channel: 1, Payload: make([]byte, 4+HandshakeSize-1)},
		{Type: TypeGrant, Channel: 1, Payload: make([]byte, 4+HandshakeSize+1)},
		{Type: TypeGrant, Channel: 1, Payload: make([]byte, MaxPayload)},
		{Type: TypeGrant, Channel: ControlChannel, Payload: Grant(DefaultWindow, nil)},
		{Type: TypeData, Channel: 1, Payload: make([]byte, MaxPayload+1)},
	}
	for _, f := range bad {
		if _, err := f.Encode(); err != ErrFrame {
			t.Errorf("%v with %d bytes on channel %d: got %v", f.Type, len(f.Payload), f.Channel, err)
		}
	}
	if _, err := Decode([]byte{2, 0, 0}); err != ErrFrame {
		t.Errorf("short frame: %v", err)
	}
	if _, err := Decode(make([]byte, MaxFrame+1)); err != ErrFrame {
		t.Errorf("frame over MaxFrame: %v", err)
	}
	good := []Frame{
		{Type: TypeData, Channel: 1, Payload: make([]byte, MaxPayload)},
		{Type: TypeData, Channel: ControlChannel},
		{Type: TypeOpen, Channel: 1, Payload: make([]byte, MaxOpenPayload)},
		{Type: TypeClose, Channel: 1, Payload: make([]byte, MaxClosePayload)},
		{Type: TypeGrant, Channel: 1, Payload: make([]byte, 4)},
		{Type: TypeGrant, Channel: 1, Payload: make([]byte, 4+HandshakeSize)},
	}
	for _, f := range good {
		if _, err := f.Encode(); err != nil {
			t.Errorf("%v with %d bytes: got %v", f.Type, len(f.Payload), err)
		}
	}
}

type grantCase struct {
	name    string
	payload []byte
	valid   bool
}

// grantCases: one case per rule of ParseGrant, and the valid shapes.
func grantCases() []grantCase {
	hs := func(n int) []byte { return bytes.Repeat([]byte{9}, n) }
	return []grantCase{
		{"credit only", Grant(DefaultWindow, nil), true},
		{"credit and handshake", Grant(DefaultWindow, hs(HandshakeSize)), true},
		{"credit 1", Grant(1, nil), true},
		{"credit MaxWindow", Grant(MaxWindow, nil), true},
		{"credit 0", Grant(0, nil), false},
		{"credit above MaxWindow", Grant(MaxWindow+1, nil), false},
		{"47-byte handshake", Grant(DefaultWindow, hs(HandshakeSize-1)), false},
		{"49-byte handshake", Grant(DefaultWindow, hs(HandshakeSize+1)), false},
		{"3-byte handshake", Grant(1, []byte{1, 2, 3}), false},
	}
}

func TestGrant(t *testing.T) {
	for _, c := range grantCases() {
		credit, hs, err := ParseGrant(c.payload)
		switch {
		case !c.valid && err != ErrFrame:
			t.Errorf("%s: got %v, want %v", c.name, err, ErrFrame)
		case c.valid && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.valid && (credit != binaryCredit(c.payload) || !bytes.Equal(hs, c.payload[4:])):
			t.Errorf("%s: got credit %d, handshake %x", c.name, credit, hs)
		}
	}
	if _, hs, _ := ParseGrant(Grant(DefaultWindow, nil)); hs != nil {
		t.Errorf("credit only: handshake %x, want nil", hs)
	}
}

func binaryCredit(p []byte) uint32 {
	return uint32(p[0])<<24 | uint32(p[1])<<16 | uint32(p[2])<<8 | uint32(p[3])
}

type openCase struct {
	name    string
	payload []byte
	valid   bool
}

// openCases: valid OPENs, and one case per rule of ParseOpen, each valid
// apart from its rule.
func openCases(t testing.TB) []openCase {
	marshal := func(mutate func(*Open)) []byte {
		o := testOpen()
		mutate(&o)
		return mustJSON(t, o)
	}
	withTask := func(o *Open) { o.Params, o.Task = nil, taskEnvelope(t) }
	valid := mustJSON(t, testOpen())
	replace := func(old, new string) []byte {
		if !bytes.Contains(valid, []byte(old)) {
			t.Fatalf("%s not in %s", old, valid)
		}
		return bytes.Replace(valid, []byte(old), []byte(new), 1)
	}
	// params of exactly n bytes: {"x":"xxx…"}
	params := func(n int) json.RawMessage {
		return json.RawMessage(`{"x":"` + strings.Repeat("x", n-len(`{"x":""}`)) + `"}`)
	}
	return []openCase{
		{"valid terminal", valid, true},
		{"valid core app", marshal(func(o *Open) { o.App, o.Service, o.Action, o.Params, o.E2E = "ngc.desktop", "api", "apps", nil, nil }), true},
		{"valid with task and confirmation", marshal(func(o *Open) { withTask(o); o.Confirm = confirmProof() }), true},
		{"valid window MinWindow", marshal(func(o *Open) { o.Window = MinWindow }), true},
		{"valid window MaxWindow", marshal(func(o *Open) { o.Window = MaxWindow }), true},
		{"valid params of MaxParams bytes", marshal(func(o *Open) { o.Params = params(MaxParams) }), true},
		{"app id", marshal(func(o *Open) { o.App = "Terminal" }), false},
		{"service", marshal(func(o *Open) { o.Service = "a b" }), false},
		{"action", marshal(func(o *Open) { o.Action = "Shell" }), false},
		{"window below MinWindow", marshal(func(o *Open) { o.Window = MinWindow - 1 }), false},
		{"window above MaxWindow", marshal(func(o *Open) { o.Window = MaxWindow + 1 }), false},
		{"core app with e2e", marshal(func(o *Open) { o.App = "ngc.desktop" }), false},
		{"app without e2e", marshal(func(o *Open) { o.E2E = nil }), false},
		{"e2e of 47 bytes", marshal(func(o *Open) { o.E2E = o.E2E[:HandshakeSize-1] }), false},
		{"e2e of 49 bytes", marshal(func(o *Open) { o.E2E = append(o.E2E, 7) }), false},
		{"task with params", marshal(func(o *Open) { withTask(o); o.Params = testOpen().Params }), false},
		{"task with empty payload", marshal(func(o *Open) { withTask(o); o.Task.Payload = nil }), false},
		{"task signature of 63 bytes", marshal(func(o *Open) { withTask(o); o.Task.Sig = o.Task.Sig[:ed25519.SignatureSize-1] }), false},
		{"confirm without task", marshal(func(o *Open) { o.Confirm = confirmProof() }), false},
		{"malformed confirm", marshal(func(o *Open) { withTask(o); o.Confirm = confirmProof(); o.Confirm.Sig = nil }), false},
		{"params not an object", marshal(func(o *Open) { o.Params = json.RawMessage(`[80,24]`) }), false},
		{"params with duplicate names", replace(`{"cols":80,"rows":24}`, `{"cols":80,"cols":24}`), false},
		{"null inside params", replace(`"cols":80`, `"cols":null`), false},
		{"invalid UTF-8 inside params", replace(`"cols":80`, "\"cols\":\"\xff\""), false},
		{"params over MaxParams", marshal(func(o *Open) { o.Params = params(MaxParams + 1) }), false},
		{"number beyond MaxSafeInteger", replace(`"cols":80`, `"cols":9007199254740992`), false},
		{"unknown field", replace(`{"app":`, `{"extra":1,"app":`), false},
		{"duplicate field", replace(`{"app":"org.sorglos.terminal",`, `{"app":"org.sorglos.terminal","app":"org.sorglos.terminal",`), false},
		{"field name in other case", replace(`"app":`, `"App":`), false},
		{"valid OPEN of MaxBrowserOpen bytes", padded(valid, MaxBrowserOpen), true},
		{"OPEN over MaxBrowserOpen", padded(valid, MaxBrowserOpen+1), false},
	}
}

// padded returns b followed by spaces up to n bytes.
func padded(b []byte, n int) []byte {
	return append(bytes.Clone(b), bytes.Repeat([]byte(" "), n-len(b))...)
}

func TestParseOpen(t *testing.T) {
	for _, c := range openCases(t) {
		_, err := ParseOpen(c.payload)
		if c.valid && err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if !c.valid && err != ErrOpen {
			t.Errorf("%s: got %v, want %v", c.name, err, ErrOpen)
		}
	}
	// params null break two rules (no null, params an object), so they are
	// no vector, but ParseOpen refuses them as well.
	if _, err := ParseOpen(bytes.Replace(mustJSON(t, testOpen()), []byte(`{"cols":80,"rows":24}`), []byte(`null`), 1)); err != ErrOpen {
		t.Errorf("params null: got %v, want %v", err, ErrOpen)
	}
}

// TestParseOpenTaskBytes: ParseOpen keeps the task member exactly as the OPEN
// carried it, because noisep.E2EPrologue hashes those bytes, and decodes the
// envelope as strictly as the rest of the OPEN.
func TestParseOpenTaskBytes(t *testing.T) {
	env := taskEnvelope(t)
	compact := mustJSON(t, env)
	spaced := `{ "sig" : "` + enc.B64.EncodeToString(env.Sig) + `", "kid": "` + env.KeyID + `",` + "\n" +
		`"payload":"` + enc.B64.EncodeToString(env.Payload) + `" }`
	open := func(task string) []byte {
		return []byte(`{"app":"org.sorglos.terminal","service":"terminal","action":"shell","window":262144,"e2e":"` +
			enc.B64.EncodeToString(bytes.Repeat([]byte{7}, HandshakeSize)) + `","task":` + task + `}`)
	}
	for name, task := range map[string]string{"as json.Marshal writes it": string(compact), "white space, other order": spaced} {
		o, err := ParseOpen(open(task))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(o.TaskBytes()) != task {
			t.Errorf("%s: task bytes %q, want %q", name, o.TaskBytes(), task)
		}
		if o.Task == nil || !bytes.Equal(o.Task.Payload, env.Payload) || o.Task.KeyID != env.KeyID || !bytes.Equal(o.Task.Sig, env.Sig) {
			t.Errorf("%s: envelope %+v", name, o.Task)
		}
	}
	if o, err := ParseOpen(mustJSON(t, testOpen())); err != nil || o.TaskBytes() != nil {
		t.Errorf("no task: %q, %v", o.TaskBytes(), err)
	}
	withTask := testOpen()
	withTask.Params, withTask.Task = nil, env
	if withTask.TaskBytes() != nil {
		t.Error("an Open ParseOpen did not make has task bytes")
	}
	many := `{"payload":"eA"`
	for i := 0; i < 70; i++ {
		many += `,"x` + strconv.Itoa(i) + `":0`
	}
	for name, task := range map[string]string{
		"unknown member":         strings.Replace(string(compact), `{"payload"`, `{"extra":1,"payload"`, 1),
		"duplicate member":       strings.Replace(string(compact), `{"payload"`, `{"kid":"x","payload"`, 1),
		"member in other case":   strings.Replace(string(compact), `"kid"`, `"Kid"`, 1),
		"null member":            strings.Replace(string(compact), `"kid":"`+env.KeyID+`"`, `"kid":null`, 1),
		"not an object":          `"task"`,
		"an array":               `[` + string(compact) + `]`,
		"payload not base64url":  strings.Replace(string(compact), `{"payload":"`, `{"payload":"=`, 1),
		"more than 64 members":   many + `}`,
		"trailing data in value": string(compact) + ` {}`,
	} {
		if _, err := ParseOpen(open(task)); err != ErrOpen {
			t.Errorf("%s: got %v, want %v", name, err, ErrOpen)
		}
	}
}

// TestOpenValidateTask pins the task size rule, which no OPEN of at most
// MaxBrowserOpen bytes can break by itself.
func TestOpenValidateTask(t *testing.T) {
	o := testOpen()
	o.Params, o.Task = nil, taskEnvelope(t)
	o.Task.Payload = make([]byte, maxTask)
	if err := o.Validate(); err != nil {
		t.Errorf("task of maxTask bytes: %v", err)
	}
	o.Task.Payload = make([]byte, maxTask+1)
	if err := o.Validate(); err != ErrOpen {
		t.Errorf("task over maxTask: got %v, want %v", err, ErrOpen)
	}
}

// TestParseOpenScansParamsOnce: the params that cost most to check are an
// object of MaxParams bytes with as many names as fit. DecodeStrict checks
// them already, so ParseOpen must cost no more than that decode plus a part
// of it, not a second scan of the params. Validate, which gets params from
// anywhere, still scans them.
func TestParseOpenScansParamsOnce(t *testing.T) {
	var p strings.Builder
	p.WriteString("{")
	for i := 0; ; i++ {
		m := `"` + strconv.FormatInt(int64(i), 36) + `":0`
		if i > 0 {
			m = "," + m
		}
		if p.Len()+len(m)+1 > MaxParams {
			break
		}
		p.WriteString(m)
	}
	p.WriteString("}")
	o := testOpen()
	o.Params = json.RawMessage(p.String())
	doc := mustJSON(t, o)
	var err error
	// Allocations only grow by chance: a GC empties sync.Pool, and under
	// -race sync.Pool also drops items at random. Measured one run each
	// under -race, ParseOpen often allocated some 10% more than the decode,
	// and in 1 of 200 tries almost three times as much. The least of five
	// runs is the cost itself.
	least := func(f func()) uint64 {
		n := allocated(f)
		for range 4 {
			n = min(n, allocated(f))
		}
		return n
	}
	decode := least(func() { err = openBudget.Decode(doc, &openDoc{}) })
	if err != nil {
		t.Fatal(err)
	}
	parse := least(func() { _, err = ParseOpen(doc) })
	t.Logf("params of %d bytes: DecodeStrict %d bytes, ParseOpen %d bytes allocated", p.Len(), decode, parse)
	if err != nil {
		t.Fatal(err)
	}
	// A second scan of the params allocates about as much as the decode
	// again. Half a decode more lies between the two, far from both, so the
	// bound holds with and without -race.
	if parse > decode+decode/2 {
		t.Errorf("ParseOpen allocates %d bytes, the decode alone %d: the params are scanned twice", parse, decode)
	}
	o.Params = json.RawMessage(`{"a":1,"a":2}`)
	if err := o.Validate(); err != ErrOpen {
		t.Errorf("Validate, params with duplicate names: got %v, want %v", err, ErrOpen)
	}
}

func TestValidParams(t *testing.T) {
	cases := map[string]bool{
		``:                    true,
		`{}`:                  true,
		`{"a":{"b":[1,2]}}`:   true,
		` {"a":1} `:           true,
		`[1]`:                 false,
		`"x"`:                 false,
		`null`:                false,
		`{"a":1} {}`:          false,
		`{"a":1,"a":2}`:       false,
		`{"a":{"b":1,"b":2}}`: false,
		"{\"a\":\"\xff\"}":    false,
		`{"a":1`:              false,
		`{"a":"` + strings.Repeat("x", MaxParams) + `"}`: false,
	}
	for s, want := range cases {
		if got := ValidParams(json.RawMessage(s)); got != want {
			t.Errorf("%.40q: got %v, want %v", s, got, want)
		}
	}
}

type closeCase struct {
	name    string
	payload []byte
	valid   bool
}

// closeCases: valid CLOSE payloads, and one case per rule of ParseClose, each
// valid apart from its rule. Messages are bounded in bytes, so the umlaut
// cases catch a count of characters.
func closeCases() []closeCase {
	message := func(text string) []byte {
		return []byte(`{"status":"error","code":"E_EXIT","message":"` + text + `"}`)
	}
	ok := []byte(`{"status":"ok"}`)
	return []closeCase{
		{"ok", ok, true},
		{"error with its message", []byte(`{"status":"error","code":"E_FORBIDDEN","message":"Keine Berechtigung."}`), true},
		{"too large", []byte(`{"status":"error","code":"E_TOOLARGE","message":"Zu viele Daten."}`), true},
		{"exit code 0", []byte(`{"status":"error","code":"E_EXIT","exitCode":0}`), true},
		{"exit code 255", []byte(`{"status":"error","code":"E_EXIT","exitCode":255}`), true},
		{"message of 256 bytes", message(strings.Repeat("x", MaxMessageText)), true},
		{"message of 128 umlauts, 256 bytes", message(strings.Repeat("ü", MaxMessageText/2)), true},
		{"CLOSE of MaxClosePayload bytes", padded(ok, MaxClosePayload), true},
		{"status ok with a code", []byte(`{"status":"ok","code":"E_EXIT"}`), false},
		{"unknown code", []byte(`{"status":"error","code":"E_WHAT"}`), false},
		{"unknown status", []byte(`{"status":"maybe"}`), false},
		{"exit code -1", []byte(`{"status":"error","code":"E_EXIT","exitCode":-1}`), false},
		{"exit code 256", []byte(`{"status":"error","code":"E_EXIT","exitCode":256}`), false},
		{"message of 257 bytes", message(strings.Repeat("x", MaxMessageText+1)), false},
		{"message of 129 umlauts, 258 bytes", message(strings.Repeat("ü", MaxMessageText/2+1)), false},
		{"null", []byte(`{"status":"ok","code":null}`), false},
		{"invalid UTF-8", message("\xff"), false},
		{"CLOSE over MaxClosePayload", padded(ok, MaxClosePayload+1), false},
		{"duplicate member", []byte(`{"status":"ok","status":"ok"}`), false},
		{"unknown member", []byte(`{"status":"ok","extra":1}`), false},
		{"member name in other case", []byte(`{"Status":"ok"}`), false},
	}
}

func TestParseClose(t *testing.T) {
	code := 2
	for _, c := range []Close{{Status: StatusOK}, Error(CodeForbidden), Error(CodeTooLarge),
		{Status: StatusError, Code: CodeExit, Message: "Dienst mit Code 2 beendet.", ExitCode: &code}} {
		b := mustJSON(t, c)
		if _, err := ParseClose(b); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
	for _, c := range closeCases() {
		_, err := ParseClose(c.payload)
		if c.valid && err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if !c.valid && err != ErrClose {
			t.Errorf("%s: got %v, want %v", c.name, err, ErrClose)
		}
	}
	for _, s := range []string{`{"status":"ok"} {}`, ``} {
		if _, err := ParseClose([]byte(s)); err != ErrClose {
			t.Errorf("%.60s: got %v, want %v", s, err, ErrClose)
		}
	}
	if Error(CodeForbidden).Message != "Keine Berechtigung." || Error(CodeTooLarge).Message != "Zu viele Daten." {
		t.Error("German message missing")
	}
}

func TestParseControl(t *testing.T) {
	for _, s := range []string{`{"t":"hello","b":{"version":"1.0.0"}}`, `{"t":"trust","b":{"seq":4}}`, `{"t":"bye"}`} {
		if _, err := ParseControl([]byte(s)); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	for _, s := range []string{
		`{"t":"shutdown"}`,
		`{"t":"Hello"}`,
		`{"t":"bye","b":null}`,
		`{"t":"bye","x":1}`,
		`{"t":"bye","t":"bye"}`,
		`{"T":"bye"}`,
		`{"t":"bye"} {}`,
		`{"t":"hello","b":{"n":9007199254740992}}`,
		`{"t":"bye"}` + strings.Repeat(" ", MaxPayload),
	} {
		if _, err := ParseControl([]byte(s)); err != ErrControl {
			t.Errorf("%.60s: got %v, want %v", s, err, ErrControl)
		}
	}
}

type controlCase struct {
	link    string
	fromAPI bool
	t       string
	allowed bool
}

// controlCases: every link, direction and type of the table, and an unknown
// link and type.
func controlCases() []controlCase {
	all := []string{CtlHello, CtlTrust, CtlNotify, CtlStatus, CtlUpdate, CtlBye}
	allowed := []struct {
		link    string
		fromAPI bool
		types   []string
	}{
		{LinkSession, true, []string{CtlTrust, CtlNotify, CtlBye}},
		{LinkSession, false, []string{CtlBye}},
		{LinkTunnel, true, []string{CtlHello, CtlBye}},
		{LinkTunnel, false, []string{CtlHello, CtlStatus, CtlUpdate, CtlNotify, CtlBye}},
	}
	var cs []controlCase
	for _, a := range allowed {
		for _, typ := range all {
			cs = append(cs, controlCase{a.link, a.fromAPI, typ, slices.Contains(a.types, typ)})
		}
	}
	return append(cs, controlCase{"other", true, CtlBye, false}, controlCase{LinkTunnel, false, "shutdown", false})
}

func TestControlAllowed(t *testing.T) {
	for _, c := range controlCases() {
		if got := ControlAllowed(c.link, c.fromAPI, c.t); got != c.allowed {
			t.Errorf("%s, from API %v, %s: got %v, want %v", c.link, c.fromAPI, c.t, got, c.allowed)
		}
	}
	if ControlAllowed(LinkSession, false, CtlStatus) {
		t.Error("browser sends status")
	}
}

type e2eCase struct {
	name           string
	plain          []byte
	fromController bool
	valid          bool
}

// e2eCases: valid inner messages, and one case per rule of ParseE2E.
func e2eCases() []e2eCase {
	msg := func(kind byte, body string) []byte { return append([]byte{kind}, body...) }
	exit2 := `{"status":"error","code":"E_EXIT","message":"Dienst mit Code 2 beendet.","exitCode":2}`
	return []e2eCase{
		{"data", msg(E2EData, "ls -la\n"), false, true},
		{"data from controller", msg(E2EData, "total 0\n"), true, true},
		{"empty data", msg(E2EData, ""), false, true},
		{"end from shell", msg(E2EEnd, ""), false, true},
		{"end from controller ok", msg(E2EEnd, `{"status":"ok"}`), true, true},
		{"end from controller with exit code 2", msg(E2EEnd, exit2), true, true},
		{"empty plaintext", []byte{}, false, false},
		{"kind 0", msg(0, "ls -la\n"), false, false},
		{"kind 3", msg(3, "ls -la\n"), false, false},
		{"end from shell with body", msg(E2EEnd, `{"status":"ok"}`), false, false},
		{"end from controller without status", msg(E2EEnd, ""), true, false},
		{"end from controller with status maybe", msg(E2EEnd, `{"status":"maybe"}`), true, false},
		{"end from controller with a CLOSE over MaxClosePayload", append([]byte{E2EEnd}, padded([]byte(`{"status":"ok"}`), MaxClosePayload+1)...), true, false},
	}
}

func TestE2E(t *testing.T) {
	for _, c := range []struct {
		name           string
		kind           byte
		body           []byte
		fromController bool
	}{
		{"data", E2EData, []byte("ls -la\n"), false},
		{"empty data", E2EData, nil, false},
		{"largest data", E2EData, make([]byte, MaxE2EPlaintext-1), true},
		{"shell end", E2EEnd, nil, false},
		{"controller end", E2EEnd, []byte(`{"status":"ok"}`), true},
	} {
		plain, err := E2E(c.kind, c.body)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		m, err := ParseE2E(plain, c.fromController)
		if err != nil || m.Kind != c.kind {
			t.Fatalf("%s: got %+v, %v", c.name, m, err)
		}
		if c.kind == E2EData && !bytes.Equal(m.Data, c.body) {
			t.Errorf("%s: data %q", c.name, m.Data)
		}
		if c.kind == E2EEnd && c.fromController && m.Status.Status != StatusOK {
			t.Errorf("%s: status %+v", c.name, m.Status)
		}
	}
	for _, c := range e2eCases() {
		_, err := ParseE2E(c.plain, c.fromController)
		if c.valid && err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if !c.valid && err != ErrE2E {
			t.Errorf("%s: got %v, want %v", c.name, err, ErrE2E)
		}
	}
	long := append([]byte{E2EData}, make([]byte, MaxE2EPlaintext)...)
	if _, err := ParseE2E(long, false); err != ErrE2E {
		t.Errorf("data longer than MaxE2EPlaintext-1: got %v, want %v", err, ErrE2E)
	}
	for name, build := range map[string]func() ([]byte, error){
		"kind 3":               func() ([]byte, error) { return E2E(3, nil) },
		"data too long":        func() ([]byte, error) { return E2E(E2EData, make([]byte, MaxE2EPlaintext)) },
		"end with a bad CLOSE": func() ([]byte, error) { return E2E(E2EEnd, []byte(`{"status":"maybe"}`)) },
		"end with a CLOSE too long": func() ([]byte, error) {
			return E2E(E2EEnd, []byte(`{"status":"ok"}`+strings.Repeat(" ", MaxClosePayload)))
		},
	} {
		if _, err := build(); err != ErrE2E {
			t.Errorf("E2E, %s: got %v, want %v", name, err, ErrE2E)
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

// hostile returns prefix, then as many empty objects as fit, then suffix:
// exactly size bytes.
func hostile(prefix, suffix string, size int) []byte {
	var b bytes.Buffer
	b.Grow(size)
	b.WriteString(prefix + "{}")
	for b.Len()+len(",{}")+len(suffix) <= size {
		b.WriteString(",{}")
	}
	for b.Len()+len(suffix) < size {
		b.WriteByte(' ')
	}
	b.WriteString(suffix)
	return b.Bytes()
}

// TestParsersBoundAllocation feeds each parser a document of its full size
// limit with an array or an oversized object where it allows none: refusing
// it must allocate next to nothing.
func TestParsersBoundAllocation(t *testing.T) {
	// An object with far more than enc.MaxMembers members where a struct is.
	members := func(size int) []byte {
		var b bytes.Buffer
		b.WriteString(`{"task":{"x0":0`)
		for i := 1; b.Len()+len(`,"x`+strconv.Itoa(i)+`":0}}`) <= size; i++ {
			b.WriteString(`,"x` + strconv.Itoa(i) + `":0`)
		}
		for b.Len()+len(`}}`) < size {
			b.WriteByte(' ')
		}
		b.WriteString(`}}`)
		return b.Bytes()
	}
	cases := []struct {
		name  string
		in    []byte
		parse func([]byte) error
		want  error
	}{
		{"ParseOpen, array", hostile(`{"e2e":[`, `]}`, MaxBrowserOpen),
			func(b []byte) error { _, err := ParseOpen(b); return err }, ErrOpen},
		{"ParseOpen, members", members(MaxBrowserOpen),
			func(b []byte) error { _, err := ParseOpen(b); return err }, ErrOpen},
		{"ParseClose", hostile(`{"exitCode":[`, `]}`, MaxClosePayload),
			func(b []byte) error { _, err := ParseClose(b); return err }, ErrClose},
		{"ParseControl", hostile(`{"t":[`, `]}`, MaxPayload),
			func(b []byte) error { _, err := ParseControl(b); return err }, ErrControl},
		{"ParseE2E", append([]byte{E2EEnd}, hostile(`{"code":[`, `]}`, MaxClosePayload)...),
			func(b []byte) error { _, err := ParseE2E(b, true); return err }, ErrE2E},
	}
	for _, c := range cases {
		if len(c.in) == 0 {
			t.Fatalf("%s: empty input", c.name)
		}
		var err error
		n := allocated(func() { err = c.parse(c.in) })
		t.Logf("%s: %d bytes allocated", c.name, n)
		if n > 64<<10 {
			t.Errorf("%s: %d bytes allocated", c.name, n)
		}
		if err != c.want {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
}

type frameVector struct {
	Name    string    `json:"name"`
	Type    Type      `json:"type"`
	Channel uint32    `json:"channel"`
	Payload enc.Bytes `json:"payload"`
	Encoded enc.Bytes `json:"encoded"`
}

type badFrameVector struct {
	Name    string    `json:"name"`
	Encoded enc.Bytes `json:"encoded"`
}

type grantVector struct {
	Name    string    `json:"name"`
	Payload enc.Bytes `json:"payload"`
	Valid   bool      `json:"valid"`
}

type openVector struct {
	Name    string    `json:"name"`
	Payload enc.Bytes `json:"payload"`
	Valid   bool      `json:"valid"`
}

type closeVector struct {
	Name    string    `json:"name"`
	Payload enc.Bytes `json:"payload"`
	Valid   bool      `json:"valid"`
}

type e2eVector struct {
	Name           string    `json:"name"`
	Plain          enc.Bytes `json:"plain"`
	FromController bool      `json:"fromController"`
	Valid          bool      `json:"valid"`
}

type controlVector struct {
	Link    string `json:"link"`
	FromAPI bool   `json:"fromApi"`
	T       string `json:"t"`
	Allowed bool   `json:"allowed"`
}

type vectors struct {
	Frames    []frameVector    `json:"frames"`
	BadFrames []badFrameVector `json:"badFrames"`
	Grants    []grantVector    `json:"grants"`
	Opens     []openVector     `json:"opens"`
	Closes    []closeVector    `json:"closes"`
	E2E       []e2eVector      `json:"e2e"`
	Controls  []controlVector  `json:"controls"`
}

// raw builds a frame without Encode's checks.
func raw(typ Type, channel uint32, payload []byte) []byte {
	return append([]byte{byte(typ), byte(channel >> 24), byte(channel >> 16), byte(channel >> 8), byte(channel)}, payload...)
}

func TestVectors(t *testing.T) {
	var v vectors
	open := mustJSON(t, testOpen())
	closeErr := mustJSON(t, Error(CodeForbidden))
	for _, f := range []struct {
		name string
		f    Frame
	}{
		{"open", Frame{Type: TypeOpen, Channel: 1, Payload: open}},
		{"data", Frame{Type: TypeData, Channel: 1, Payload: []byte("ls\n")}},
		{"eof", Frame{Type: TypeEOF, Channel: 1}},
		{"close", Frame{Type: TypeClose, Channel: 1, Payload: closeErr}},
		{"grant with handshake", Frame{Type: TypeGrant, Channel: 1, Payload: Grant(DefaultWindow, bytes.Repeat([]byte{9}, HandshakeSize))}},
		{"grant without handshake", Frame{Type: TypeGrant, Channel: 1, Payload: Grant(DefaultWindow, nil)}},
		{"control", Frame{Type: TypeData, Channel: 0, Payload: []byte(`{"t":"bye"}`)}},
		{"control trust hint", Frame{Type: TypeData, Channel: 0, Payload: []byte(`{"t":"trust","b":{"seq":4}}`)}},
	} {
		b, err := f.f.Encode()
		if err != nil {
			t.Fatal(err)
		}
		v.Frames = append(v.Frames, frameVector{Name: f.name, Type: f.f.Type, Channel: f.f.Channel, Payload: f.f.Payload, Encoded: b})
	}
	grant := Grant(DefaultWindow, nil)
	closeOK := []byte(`{"status":"ok"}`)
	for _, f := range []struct {
		name    string
		encoded []byte
	}{
		{"4 bytes", []byte{byte(TypeData), 0, 0, 0}},
		{"type 6", raw(6, 1, nil)},
		{"open on channel 0", raw(TypeOpen, 0, open)},
		{"open without payload", raw(TypeOpen, 1, nil)},
		{"eof with payload", raw(TypeEOF, 1, []byte("x"))},
		{"eof on channel 0", raw(TypeEOF, 0, nil)},
		{"close on channel 0", raw(TypeClose, 0, closeOK)},
		{"close without payload", raw(TypeClose, 1, nil)},
		{"close over MaxClosePayload", raw(TypeClose, 1, append(closeOK, bytes.Repeat([]byte(" "), MaxClosePayload+1-len(closeOK))...))},
		{"grant on channel 0", raw(TypeGrant, 0, grant)},
		{"grant of 3 bytes", raw(TypeGrant, 1, grant[:3])},
		{"grant of 51 bytes", raw(TypeGrant, 1, Grant(DefaultWindow, bytes.Repeat([]byte{9}, HandshakeSize-1)))},
		{"grant of 53 bytes", raw(TypeGrant, 1, Grant(DefaultWindow, bytes.Repeat([]byte{9}, HandshakeSize+1)))},
	} {
		v.BadFrames = append(v.BadFrames, badFrameVector{Name: f.name, Encoded: f.encoded})
	}
	for _, c := range grantCases() {
		v.Grants = append(v.Grants, grantVector{Name: c.name, Payload: c.payload, Valid: c.valid})
	}
	for _, c := range openCases(t) {
		v.Opens = append(v.Opens, openVector{Name: c.name, Payload: c.payload, Valid: c.valid})
	}
	for _, c := range closeCases() {
		v.Closes = append(v.Closes, closeVector{Name: c.name, Payload: c.payload, Valid: c.valid})
	}
	for _, c := range e2eCases() {
		v.E2E = append(v.E2E, e2eVector{Name: c.name, Plain: c.plain, FromController: c.fromController, Valid: c.valid})
	}
	for _, c := range controlCases() {
		v.Controls = append(v.Controls, controlVector{Link: c.link, FromAPI: c.fromAPI, T: c.t, Allowed: c.allowed})
	}
	checkVectors(t, v)
	golden.Check(t, *update, "wire.json", v)
}

// checkVectors holds every vector against the Go functions, as an
// implementation in another language holds them against its own.
func checkVectors(t *testing.T, v vectors) {
	for _, f := range v.Frames {
		g, err := Decode(f.Encoded)
		if err != nil || g.Type != f.Type || g.Channel != f.Channel || !bytes.Equal(g.Payload, f.Payload) {
			t.Errorf("frame %s: got %+v, %v", f.Name, g, err)
			continue
		}
		switch {
		case g.Type == TypeOpen:
			_, err = ParseOpen(g.Payload)
		case g.Type == TypeClose:
			_, err = ParseClose(g.Payload)
		case g.Type == TypeGrant:
			_, _, err = ParseGrant(g.Payload)
		case g.Type == TypeData && g.Channel == ControlChannel:
			_, err = ParseControl(g.Payload)
		}
		if err != nil {
			t.Errorf("frame %s: payload: %v", f.Name, err)
		}
	}
	for _, f := range v.BadFrames {
		if _, err := Decode(f.Encoded); err != ErrFrame {
			t.Errorf("bad frame %s: got %v, want %v", f.Name, err, ErrFrame)
		}
	}
	for _, g := range v.Grants {
		if _, _, err := ParseGrant(g.Payload); (err == nil) != g.Valid {
			t.Errorf("grant %s: got %v, want valid %v", g.Name, err, g.Valid)
		}
	}
	for _, o := range v.Opens {
		if _, err := ParseOpen(o.Payload); (err == nil) != o.Valid {
			t.Errorf("open %s: got %v, want valid %v", o.Name, err, o.Valid)
		}
	}
	for _, c := range v.Closes {
		if _, err := ParseClose(c.Payload); (err == nil) != c.Valid {
			t.Errorf("close %s: got %v, want valid %v", c.Name, err, c.Valid)
		}
	}
	for _, e := range v.E2E {
		if _, err := ParseE2E(e.Plain, e.FromController); (err == nil) != e.Valid {
			t.Errorf("e2e %s: got %v, want valid %v", e.Name, err, e.Valid)
		}
	}
	for _, c := range v.Controls {
		if ControlAllowed(c.Link, c.FromAPI, c.T) != c.Allowed {
			t.Errorf("control %s, from API %v, %s: want allowed %v", c.Link, c.FromAPI, c.T, c.Allowed)
		}
	}
}

func FuzzDecode(f *testing.F) {
	f.Add([]byte{1, 0, 0, 0, 1, '{', '}'})
	f.Add(raw(TypeData, 1, append([]byte{E2EEnd}, `{"status":"ok"}`...)))
	f.Add(raw(TypeGrant, 1, Grant(DefaultWindow, bytes.Repeat([]byte{9}, HandshakeSize))))
	f.Fuzz(func(t *testing.T, b []byte) {
		fr, err := Decode(b)
		if err != nil {
			return
		}
		out, err := fr.Encode()
		if err != nil || !bytes.Equal(out, b) {
			t.Fatalf("re-encode differs: %v", err)
		}
		switch {
		case fr.Type == TypeOpen:
			_, _ = ParseOpen(fr.Payload)
		case fr.Type == TypeClose:
			_, _ = ParseClose(fr.Payload)
		case fr.Type == TypeGrant:
			_, _, _ = ParseGrant(fr.Payload)
		case fr.Type == TypeData && fr.Channel == ControlChannel:
			_, _ = ParseControl(fr.Payload)
		case fr.Type == TypeData:
			_, _ = ParseE2E(fr.Payload, true)
			_, _ = ParseE2E(fr.Payload, false)
		}
	})
}

// FuzzParseE2E checks that every plaintext ParseE2E accepts is exactly what
// E2E builds from its parts.
func FuzzParseE2E(f *testing.F) {
	for _, c := range e2eCases() {
		f.Add(c.plain, c.fromController)
	}
	f.Fuzz(func(t *testing.T, plain []byte, fromController bool) {
		m, err := ParseE2E(plain, fromController)
		if err != nil {
			return
		}
		out, err := E2E(m.Kind, plain[1:])
		if err != nil || !bytes.Equal(out, plain) {
			t.Fatalf("E2E(%d, body) = %x, %v; want %x", m.Kind, out, err, plain)
		}
		if m.Kind == E2EData && !bytes.Equal(m.Data, plain[1:]) {
			t.Fatalf("data %x, want %x", m.Data, plain[1:])
		}
		if m.Kind == E2EEnd && fromController && m.Status.Validate() != nil {
			t.Fatalf("status %+v", m.Status)
		}
	})
}
