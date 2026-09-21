package wire_test

import (
	"bytes"
	"crypto/ed25519"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/task"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// TestMaxTaskIsTaskMaxTask pins wire's own bound on a task in an OPEN to
// task.MaxTask, which wire cannot import: an OPEN may carry a task of exactly
// task.MaxTask bytes, and not one more.
func TestMaxTaskIsTaskMaxTask(t *testing.T) {
	o := wire.Open{App: "org.sorglos.terminal", Service: "terminal", Action: "shell", Window: wire.DefaultWindow,
		E2E:  bytes.Repeat([]byte{7}, wire.HandshakeSize),
		Task: &sign.Envelope{Payload: make([]byte, task.MaxTask), KeyID: "k", Sig: make([]byte, ed25519.SignatureSize)}}
	if err := o.Validate(); err != nil {
		t.Fatalf("task of task.MaxTask bytes: %v", err)
	}
	o.Task.Payload = make([]byte, task.MaxTask+1)
	if err := o.Validate(); err != wire.ErrOpen {
		t.Fatalf("task of task.MaxTask+1 bytes: got %v, want %v", err, wire.ErrOpen)
	}
}
