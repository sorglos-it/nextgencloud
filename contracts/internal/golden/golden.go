// Package golden compares test output with files in contracts/testdata.
package golden

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Dir is the testdata directory as seen from a package directory directly
// below contracts/ (for example contracts/sign).
var Dir = filepath.Join("..", "testdata")

// Check encodes v as indented JSON and compares it with Dir/name. With update
// set it rewrites the file instead.
func Check(t *testing.T, update bool, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join(Dir, name)
	if update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update once to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from the golden file; if intended, run go test -update and review the diff", name)
	}
}

// Load reads Dir/name into v.
func Load(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(Dir, name))
	if err != nil {
		t.Fatalf("%v (run go test -update once to create it)", err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}
