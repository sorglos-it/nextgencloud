// Package service defines how the controller runs the program of a service
// inside its app container (contract V3).
package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"unicode"
	"unicode/utf8"

	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// Environment variables a service program gets; there are no others.
const (
	EnvUser     = "NGC_USER"      // user ID; empty for the start action
	EnvUserName = "NGC_USER_NAME" // login name, for display only
	EnvApp      = "NGC_APP"
	EnvService  = "NGC_SERVICE"
	EnvAction   = "NGC_ACTION"
	EnvParams   = "NGC_PARAMS" // JSON object or empty
	EnvVersion  = "NGC_VERSION"
	EnvData     = "NGC_DATA"  // always DataDir
	EnvStart    = "NGC_START" // "1" for the start action, else "0"
)

// Paths and limits. Times in seconds.
const (
	DataDir        = "/data"
	DefaultTimeout = 30
	MaxTimeout     = 3600
	MaxStderr      = 64 << 10
	Path           = "/usr/local/bin:/usr/bin:/bin"
)

// Call describes one run of a service program.
type Call struct {
	User, UserName, App, Service, Action, Version string
	// Params are the task's params when a task came, else the OPEN's params.
	// On end-to-end channels noisep.E2EPrologue binds the OPEN's params
	// member, or with a task the task's bytes, which carry its params.
	Params json.RawMessage
	Start  bool
}

// ErrCall reports a call the controller must not start.
var ErrCall = errors.New("service: invalid call")

// Validate checks c before anything is started.
func (c Call) Validate() error {
	if !wire.ValidAppID(c.App) || !wire.ValidName(c.Service) || !wire.ValidName(c.Action) ||
		!wire.ValidParams(c.Params) || !printable(c.Version) || !printable(c.UserName) {
		return ErrCall
	}
	if c.Start != (c.User == "") || (c.User != "" && !trust.ValidID(c.User)) {
		return ErrCall
	}
	return nil
}

func printable(s string) bool {
	if len(s) > 64 || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// Env returns the complete, sorted environment for the program.
func (c Call) Env() []string {
	start := "0"
	if c.Start {
		start = "1"
	}
	// Compact JSON: one line without insignificant whitespace, as the service
	// reads it. The controller never logs NGC_PARAMS: params can carry user data.
	var paramsBuf bytes.Buffer
	params := ""
	if err := json.Compact(&paramsBuf, c.Params); err == nil {
		params = paramsBuf.String()
	}
	env := []string{
		"LANG=C.UTF-8",
		"PATH=" + Path,
		EnvUser + "=" + c.User,
		EnvUserName + "=" + c.UserName,
		EnvApp + "=" + c.App,
		EnvService + "=" + c.Service,
		EnvAction + "=" + c.Action,
		EnvParams + "=" + params,
		EnvVersion + "=" + c.Version,
		EnvData + "=" + DataDir,
		EnvStart + "=" + start,
	}
	slices.Sort(env)
	return env
}

// Result maps how the program ended to the CLOSE payload: startErr if it
// could not be started, timedOut if the controller stopped it, else the
// exit code (negative if it was killed by a signal).
func Result(exitCode int, timedOut bool, startErr error) wire.Close {
	switch {
	case startErr != nil:
		return wire.Error(wire.CodeUnreachable)
	case timedOut:
		return wire.Error(wire.CodeTimeout)
	case exitCode == 0:
		return wire.Close{Status: wire.StatusOK}
	case exitCode < 0 || exitCode > 255:
		return wire.Error(wire.CodeExit)
	}
	code := exitCode
	return wire.Close{Status: wire.StatusError, Code: wire.CodeExit, Message: fmt.Sprintf("Dienst mit Code %d beendet.", code), ExitCode: &code}
}
