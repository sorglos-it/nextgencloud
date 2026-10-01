package link

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/coder/websocket"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// seedFrames returns the frame plaintexts of shared/testdata/wire.json, the
// good ones and the bad ones: those vectors are what a link carries, so the
// fuzzer starts from both sides of wire.Decode instead of guessing them. The
// file is this repository's own fixture, not input from outside, so it is read
// with encoding/json the way golden.Load reads the same files in shared; that
// helper itself is closed to this package, because it lives under
// shared/internal.
func seedFrames(f *testing.F) [][]byte {
	f.Helper()
	path := filepath.Join("..", "..", "..", "..", "shared", "testdata", "wire.json")
	data, err := os.ReadFile(path)
	if err != nil {
		f.Fatalf("%s: %v", path, err)
	}
	type vector struct {
		Encoded enc.Bytes `json:"encoded"`
	}
	var v struct {
		Frames    []vector `json:"frames"`
		BadFrames []vector `json:"badFrames"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		f.Fatalf("%s: %v", path, err)
	}
	out := make([][]byte, 0, len(v.Frames)+len(v.BadFrames))
	for _, fr := range append(v.Frames, v.BadFrames...) {
		out = append(out, fr.Encoded)
	}
	if len(out) == 0 {
		f.Fatalf("%s: no frames", path)
	}
	return out
}

// FuzzLinkMessage feeds a link with sealed messages of any content: Read must
// answer a frame the contract allows or an error, and never panic. What it
// answers is held against wire.Decode over the same bytes, because that is the
// one rule of the link above the WebSocket: it hands up the frames wire reads
// from the plaintexts, unchanged, and nothing else.
func FuzzLinkMessage(f *testing.F) {
	frame, err := wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("x")}.Encode()
	if err != nil {
		f.Fatalf("encode: %v", err)
	}
	f.Add(frame)
	f.Add([]byte{})
	f.Add([]byte{9, 0, 0, 0, 1})
	for _, seed := range seedFrames(f) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, plain []byte) {
		l, tr, conn, done := dialTest(t)
		defer done()
		sealed, err := tr.Seal(plain)
		if err != nil {
			t.Skip()
		}
		if err := conn.Write(ctx, websocket.MessageBinary, sealed); err != nil {
			t.Skip()
		}
		// A real peer reads, so it answers the close frame a refused message
		// brings; without an answer websocket.Close waits five seconds for it.
		// Nothing else arrives here, so this takes no message from the test.
		go func() { _, _, _ = conn.Read(ctx) }()
		want, wantErr := wire.Decode(plain)
		got, err := l.Read(ctx)
		if (err == nil) != (wantErr == nil) {
			t.Fatalf("Read answered %v, wire.Decode %v", err, wantErr)
		}
		if err != nil {
			return
		}
		if got.Type != want.Type || got.Channel != want.Channel || !bytes.Equal(got.Payload, want.Payload) {
			t.Fatalf("Read gave %+v, wire.Decode %+v", got, want)
		}
		if _, err := got.Encode(); err != nil {
			t.Fatalf("Read gave a frame Encode refuses: %+v", got)
		}
	})
}
