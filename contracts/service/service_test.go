package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

func testCall() Call {
	return Call{User: "ISEhISEhISEhISEhISEhIQ", UserName: "admin", App: "org.sorglos.terminal", Service: "terminal",
		Action: "shell", Version: "1.0.0", Params: json.RawMessage(`{"cols":80}`)}
}

func TestEnv(t *testing.T) {
	got := testCall().Env()
	want := []string{
		"LANG=C.UTF-8",
		"NGC_ACTION=shell",
		"NGC_APP=org.sorglos.terminal",
		"NGC_DATA=/data",
		`NGC_PARAMS={"cols":80}`,
		"NGC_SERVICE=terminal",
		"NGC_START=0",
		"NGC_USER=ISEhISEhISEhISEhISEhIQ",
		"NGC_USER_NAME=admin",
		"NGC_VERSION=1.0.0",
		"PATH=" + Path,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
	start := Call{App: "org.sorglos.firewall", Service: "rules", Action: "start", Version: "2.1.0", Start: true}
	if err := start.Validate(); err != nil || !slices.Contains(start.Env(), "NGC_START=1") || !slices.Contains(start.Env(), "NGC_USER=") {
		t.Errorf("start action: %v %q", err, start.Env())
	}
}

func TestValidate(t *testing.T) {
	for name, mutate := range map[string]func(*Call){
		"app":               func(c *Call) { c.App = "Terminal" },
		"action":            func(c *Call) { c.Action = "rm -rf" },
		"params":            func(c *Call) { c.Params = json.RawMessage(`[1]`) },
		"user":              func(c *Call) { c.User = "root" },
		"start with user":   func(c *Call) { c.Start = true },
		"name with newline": func(c *Call) { c.UserName = "admin\nNGC_USER=x" },
	} {
		c := testCall()
		mutate(&c)
		if err := c.Validate(); err != ErrCall {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestResult(t *testing.T) {
	if c := Result(0, false, nil); c.Status != wire.StatusOK {
		t.Errorf("exit 0: %+v", c)
	}
	c := Result(2, false, nil)
	if c.Code != wire.CodeExit || c.ExitCode == nil || *c.ExitCode != 2 || c.Message != "Dienst mit Code 2 beendet." || c.Validate() != nil {
		t.Errorf("exit 2: %+v", c)
	}
	if c := Result(-1, true, nil); c.Code != wire.CodeTimeout {
		t.Errorf("timeout: %+v", c)
	}
	if c := Result(-1, false, errors.New("no container")); c.Code != wire.CodeUnreachable {
		t.Errorf("start error: %+v", c)
	}
	if c := Result(-1, false, nil); c.Code != wire.CodeExit || c.ExitCode != nil || c.Validate() != nil {
		t.Errorf("killed: %+v", c)
	}
}

// TestEchoService runs the reference service the way the controller does,
// only without a container.
func TestEchoService(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("reference service needs a POSIX shell")
	}
	for _, exit := range []int{0, 2} {
		c := testCall()
		c.Params = json.RawMessage(`{"exit":` + string(rune('0'+exit)) + `}`)
		cmd := exec.Command("sh", filepath.Join("..", "testdata", "service", "echo.sh"))
		cmd.Env = c.Env()
		cmd.Stdin = bytes.NewReader([]byte("cp -r a b\n"))
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			t.Fatal(err)
		}
		got := Result(cmd.ProcessState.ExitCode(), false, nil)
		if stdout.String() != "cp -r a b\n" || (exit == 0) != (got.Status == wire.StatusOK) {
			t.Errorf("exit %d: stdout %q, close %+v, stderr %q", exit, stdout.String(), got, stderr.String())
		}
	}
}
