package task_test

import (
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/internal/golden"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/task"
)

// TestVectorParamsAreValid keeps the task in task.json one a verifier would
// accept: a core action, whichever it is, whose params core accepts. The task
// is ngc.store/controller/deactivate; the example of activate, whose params
// carry the UI hash, lives in uiproof.json. It is an external test package so
// that core may import task.
func TestVectorParamsAreValid(t *testing.T) {
	var v struct {
		Task sign.Envelope `json:"task"`
	}
	golden.Load(t, "task.json", &v)
	var tk task.Task
	if err := enc.DecodeStrict(v.Task.Payload, task.MaxTask, &tk); err != nil {
		t.Fatal(err)
	}
	if err := tk.Validate(); err != nil {
		t.Fatal(err)
	}
	a, ok := core.Lookup(tk.App, tk.Service, tk.Action)
	if !ok {
		t.Fatalf("%s/%s/%s is no core action", tk.App, tk.Service, tk.Action)
	}
	if _, err := core.ParseParams(a, tk.Params); err != nil {
		t.Fatalf("params %s: %v", tk.Params, err)
	}
}
