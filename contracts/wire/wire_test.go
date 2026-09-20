package wire

import (
	"bytes"
	"encoding/json"
	"flag"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

func testOpen() Open {
	return Open{
		App:     "org.sorglos.terminal",
		Service: "terminal",
		Action:  "shell",
		Params:  json.RawMessage(`{"cols":80,"rows":24}`),
		Window:  DefaultWindow,
		E2E:     bytes.Repeat([]byte{7}, MinHandshake),
	}
}

func TestFrameRoundTrip(t *testing.T) {
	open, _ := json.Marshal(testOpen())
	closeOK, _ := json.Marshal(Close{Status: StatusOK})
	for _, f := range []Frame{
		{Type: TypeOpen, Channel: 1, Payload: open},
		{Type: TypeData, Channel: 1, Payload: []byte("ls -la\n")},
		{Type: TypeData, Channel: ControlChannel, Payload: []byte(`{"t":"bye"}`)},
		{Type: TypeEOF, Channel: 1},
		{Type: TypeClose, Channel: 1, Payload: closeOK},
		{Type: TypeGrant, Channel: 1, Payload: Grant(DefaultWindow, nil)},
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
		{Type: TypeEOF, Channel: 1, Payload: []byte("x")},
		{Type: TypeEOF, Channel: ControlChannel},
		{Type: TypeClose, Channel: 1},
		{Type: TypeClose, Channel: ControlChannel, Payload: []byte("{}")},
		{Type: TypeGrant, Channel: 1, Payload: []byte{1, 2}},
		{Type: TypeGrant, Channel: ControlChannel, Payload: Grant(DefaultWindow, nil)},
		{Type: TypeData, Channel: 1, Payload: make([]byte, MaxPayload+1)},
	}
	for _, f := range bad {
		if _, err := f.Encode(); err != ErrFrame {
			t.Errorf("%+v: got %v", f.Type, err)
		}
	}
	if _, err := Decode([]byte{2, 0, 0}); err != ErrFrame {
		t.Errorf("short frame: %v", err)
	}
	good := []Frame{
		{Type: TypeData, Channel: 1, Payload: make([]byte, MaxPayload)},
		{Type: TypeGrant, Channel: 1, Payload: make([]byte, MaxPayload)},
	}
	for _, f := range good {
		if _, err := f.Encode(); err != nil {
			t.Errorf("%+v: got %v", f.Type, err)
		}
	}
}

func TestGrant(t *testing.T) {
	hs := bytes.Repeat([]byte{1}, MinHandshake)
	credit, got, err := ParseGrant(Grant(4096, hs))
	if err != nil || credit != 4096 || !bytes.Equal(got, hs) {
		t.Fatalf("got %d %x %v", credit, got, err)
	}
	if _, _, err := ParseGrant(Grant(1, []byte{1, 2, 3})); err != ErrFrame {
		t.Errorf("short handshake: %v", err)
	}
}

func TestParseOpen(t *testing.T) {
	good, _ := json.Marshal(testOpen())
	if _, err := ParseOpen(good); err != nil {
		t.Fatalf("valid: %v", err)
	}
	mutations := map[string]func(*Open){
		"app id":      func(o *Open) { o.App = "Terminal" },
		"service":     func(o *Open) { o.Service = "a b" },
		"params list": func(o *Open) { o.Params = json.RawMessage(`[1]`) },
		"params null": func(o *Open) { o.Params = json.RawMessage(`null`) },
		"window":      func(o *Open) { o.Window = 10 },
		"e2e":         func(o *Open) { o.E2E = []byte{1} },
	}
	for name, mutate := range mutations {
		o := testOpen()
		mutate(&o)
		b, _ := json.Marshal(o)
		if _, err := ParseOpen(b); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := ParseOpen([]byte(`{"app":"a.b","extra":1}`)); err == nil {
		t.Error("unknown field accepted")
	}
	core := testOpen()
	core.App, core.E2E = "ngc.users", nil
	b, _ := json.Marshal(core)
	if _, err := ParseOpen(b); err != nil {
		t.Errorf("core app: %v", err)
	}
	core.E2E = bytes.Repeat([]byte{7}, MinHandshake)
	b, _ = json.Marshal(core)
	if _, err := ParseOpen(b); err == nil {
		t.Error("core app with end-to-end handshake accepted")
	}
}

func TestParseClose(t *testing.T) {
	code := 2
	for _, c := range []Close{{Status: StatusOK}, Error(CodeForbidden), {Status: StatusError, Code: CodeExit, Message: "Dienst mit Code 2 beendet.", ExitCode: &code}} {
		b, _ := json.Marshal(c)
		if _, err := ParseClose(b); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
	for _, s := range []string{`{"status":"ok","code":"E_EXIT"}`, `{"status":"error","code":"E_WHAT"}`, `{"status":"maybe"}`} {
		if _, err := ParseClose([]byte(s)); err == nil {
			t.Errorf("%s: accepted", s)
		}
	}
	if Error(CodeForbidden).Message != "Keine Berechtigung." {
		t.Error("German message missing")
	}
}

func TestParseControl(t *testing.T) {
	if _, err := ParseControl([]byte(`{"t":"hello","b":{"version":"1.0.0"}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseControl([]byte(`{"t":"shutdown"}`)); err != ErrControl {
		t.Errorf("unknown type: %v", err)
	}
}

type frameVector struct {
	Name    string    `json:"name"`
	Type    Type      `json:"type"`
	Channel uint32    `json:"channel"`
	Payload enc.Bytes `json:"payload"`
	Encoded enc.Bytes `json:"encoded"`
}

func TestVectors(t *testing.T) {
	open, _ := json.Marshal(testOpen())
	closeErr, _ := json.Marshal(Error(CodeForbidden))
	var vs []frameVector
	for _, f := range []struct {
		name string
		f    Frame
	}{
		{"open", Frame{Type: TypeOpen, Channel: 1, Payload: open}},
		{"data", Frame{Type: TypeData, Channel: 1, Payload: []byte("ls\n")}},
		{"eof", Frame{Type: TypeEOF, Channel: 1}},
		{"close", Frame{Type: TypeClose, Channel: 1, Payload: closeErr}},
		{"grant with handshake", Frame{Type: TypeGrant, Channel: 1, Payload: Grant(DefaultWindow, bytes.Repeat([]byte{9}, MinHandshake))}},
		{"grant without handshake", Frame{Type: TypeGrant, Channel: 1, Payload: Grant(DefaultWindow, nil)}},
		{"control", Frame{Type: TypeData, Channel: 0, Payload: []byte(`{"t":"bye"}`)}},
	} {
		b, err := f.f.Encode()
		if err != nil {
			t.Fatal(err)
		}
		vs = append(vs, frameVector{Name: f.name, Type: f.f.Type, Channel: f.f.Channel, Payload: f.f.Payload, Encoded: b})
	}
	golden.Check(t, *update, "wire.json", vs)
}

func FuzzDecode(f *testing.F) {
	f.Add([]byte{1, 0, 0, 0, 1, '{', '}'})
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
		}
	})
}
