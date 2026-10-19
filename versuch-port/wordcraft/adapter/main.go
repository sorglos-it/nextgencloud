// Command adapter is what a port of a foreign program really costs, in one
// file. NGC hands a service the action in NGC_ACTION and its parameters as
// JSON in NGC_PARAMS (shared/service/service.go); wordcraft-cli, like almost
// every foreign program, wants them as command line arguments. Nothing
// bridges that gap, so every port writes this.
//
// It is NOT a shell script, and that is the point of the exercise: a shell
// cannot parse NGC_PARAMS safely, and a file name pasted into a shell command
// is how a document turns into a command. Here the parameters are decoded
// with encoding/json and the program is started with an argument vector, with
// no shell anywhere.
//
// The names of the variables are copied here rather than imported from
// shared/service, because this module is deliberately outside NGC's Go
// module. In a real app they would be imported.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
)

// The program being wrapped, and where the app's data lives.
const (
	tool    = "/usr/lib/ngc/wordcraft-cli"
	dataEnv = "NGC_DATA"
)

// params are the members this adapter understands. Anything else in
// NGC_PARAMS is refused rather than ignored: a parameter nobody reads is a
// parameter a caller believes in.
type params struct {
	File string `json:"file"`
	Out  string `json:"out,omitempty"`
}

func main() {
	if err := run(os.Stdout); err != nil {
		// The message goes to stderr, which NGC keeps in a ring of 64 KiB and
		// does not log. The exit code is what the browser sees.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out io.Writer) error {
	data := os.Getenv(dataEnv)
	if data == "" {
		return errors.New("adapter: NGC_DATA is not set")
	}
	var p params
	if raw := os.Getenv("NGC_PARAMS"); raw != "" {
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&p); err != nil {
			return errors.New("adapter: parameters are not the expected object")
		}
	}
	args, err := argv(os.Getenv("NGC_ACTION"), data, p)
	if err != nil {
		return err
	}
	cmd := exec.Command(tool, args...)
	cmd.Dir = data
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	// A fresh environment: the variables NGC set carry the user and must not
	// reach a program that was never written for them.
	cmd.Env = []string{"LANG=C.UTF-8", "PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + data}
	return cmd.Run()
}

// argv builds the argument vector for one action. Every path is resolved
// below data and checked to stay there, so a file name can name no file of
// the container outside the app's own folder.
func argv(action, data string, p params) ([]string, error) {
	in, err := under(data, p.File)
	if err != nil {
		return nil, err
	}
	switch action {
	case "text":
		return []string{"text", in}, nil
	case "info":
		return []string{"info", in}, nil
	case "convert":
		dst, err := under(data, p.Out)
		if err != nil {
			return nil, err
		}
		return []string{"convert", in, dst}, nil
	default:
		return nil, errors.New("adapter: unknown action")
	}
}

// under resolves name below dir. The adapter runs on Linux, so this works in
// package path and never in path/filepath: filepath follows the rules of the
// machine it was BUILT on, and a check whose answer depends on that is a
// check nobody can read. Measured, and it is why this function was rewritten:
// built on Windows, filepath.IsAbs("/etc/passwd") is false and
// filepath.Join turned "../etc/passwd" into "\data\etc\passwd".
//
// And it REFUSES rather than cleans. Rooting the name at "/" and calling
// Clean would make "../etc/passwd" into "/data/etc/passwd", which stays
// inside the folder and is therefore safe - but it is a different file than
// the caller asked for, and a name that tried to leave is a mistake or an
// attack. Neither should come back as a quiet success.
func under(dir, name string) (string, error) {
	if name == "" || strings.HasPrefix(name, "/") {
		return "", errors.New("adapter: the file name is empty or absolute")
	}
	for _, part := range strings.Split(name, "/") {
		switch part {
		case "", ".":
			// An empty segment is "a//b" or a trailing slash, a "." is
			// "./a": both are names nobody typed on purpose.
			return "", errors.New("adapter: the file name has an empty segment")
		case "..":
			return "", errors.New("adapter: the file name leaves the data folder")
		}
	}
	return path.Join(dir, name), nil
}
