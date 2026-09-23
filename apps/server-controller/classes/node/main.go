// Package node wires the controller together: commands, the links to the
// APIs, and what the server keeps doing without an API.
package node

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// Options carries what one command took from the command line. The later tasks
// work from it, so the arguments are read in exactly one place.
type Options struct {
	Config string // where controller.json is
	Pair   string // pairing code of "pair"; a secret, so it is never printed
	Arch   string // target of "image", one out of arches
}

// arches are the targets NGC is built for. A fixed list, because the value
// becomes an argument of the image build later on.
var arches = []string{"amd64", "arm64"}

// Wrong use is answered with one German line, the same one for every mistake:
// a second line with the cause could carry the pairing code back to the screen.
const (
	usagePair  = "Aufruf: ngc-controller pair -code <Kopplungscode>"
	usageImage = "Aufruf: ngc-controller image -arch <amd64|arm64> -out <Datei>"
)

var (
	errPairUse  = errors.New("pair takes -code and nothing else")
	errImageUse = errors.New("image takes -arch and -out and nothing else")
)

// Main runs one command and returns the exit code: 0 success, 1 failure,
// 2 wrong use. It never calls os.Exit, so tests can drive it.
func Main(version string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ngc-controller", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := fs.String("config", config.ControllerFile, "configuration file")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "ngc-controller %s (contract %d)\n", version, tunnel.Contract)
		return 0
	}
	switch cmd := fs.Arg(0); cmd {
	case "":
		return run(version, *cfgPath, stdout, stderr)
	case "pair":
		return pair(version, *cfgPath, fs.Args()[1:], stdout, stderr)
	case "terminal":
		return runTerminal(fs.Args()[1:], stdout, stderr)
	case "hold":
		return hold()
	case "image":
		return buildImage(version, fs.Args()[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", cmd)
		return 2
	}
}

// run is the controller itself: tunnels to the APIs, services, updates. It is
// what starts without a command. Task 24 wires it out of the parts the tasks
// before it bring.
func run(version, cfgPath string, stdout, stderr io.Writer) int {
	return notReady(stderr, "run")
}

// pair joins this server to an API with the code ulh shows. Task 4 builds it;
// the flags are read here already, so a wrong call is turned away the same way
// now as it will be then.
func pair(version, cfgPath string, args []string, stdout, stderr io.Writer) int {
	if _, err := parsePair(cfgPath, args); err != nil {
		fmt.Fprintln(stderr, usagePair)
		return 2
	}
	return notReady(stderr, "pair")
}

// parsePair reads the flags of "pair". It writes nothing at all: the flag
// package would print the whole command line, and a mistyped pairing code is
// still a secret.
func parsePair(cfgPath string, args []string) (Options, error) {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	code := fs.String("code", "", "pairing code of the API")
	if err := fs.Parse(args); err != nil {
		return Options{}, err
	}
	if *code == "" || fs.NArg() != 0 {
		return Options{}, errPairUse
	}
	return Options{Config: cfgPath, Pair: *code}, nil
}

// runTerminal is the service of the terminal app. It runs inside the container
// of that app, not on the server. Task 23 builds it.
func runTerminal(args []string, stdout, stderr io.Writer) int {
	return notReady(stderr, "terminal")
}

// hold waits until its container stops, so an app image needs no program of
// its own. Task 23 builds it. It takes no writers because the finished command
// says nothing; only this placeholder does, on os.Stderr.
func hold() int {
	return notReady(os.Stderr, "hold")
}

// buildImage writes the image of the terminal app for one architecture. Task 23
// makes that image with podman on a build machine instead, so no later task
// fills this command: which of the two ways stays is the coordinator's call.
func buildImage(version string, args []string, stdout, stderr io.Writer) int {
	if _, _, err := parseImage(args); err != nil {
		fmt.Fprintln(stderr, usageImage)
		return 2
	}
	return notReady(stderr, "image")
}

// parseImage reads the flags of "image" and returns the options and the file
// to write. The architecture has to be one of arches: it later becomes an
// argument of another program, so only known values get through.
func parseImage(args []string) (Options, string, error) {
	fs := flag.NewFlagSet("image", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	arch := fs.String("arch", "", "architecture of the image")
	out := fs.String("out", "", "file to write the image to")
	if err := fs.Parse(args); err != nil {
		return Options{}, "", err
	}
	if !slices.Contains(arches, *arch) || *out == "" || fs.NArg() != 0 {
		return Options{}, "", errImageUse
	}
	return Options{Arch: *arch}, *out, nil
}

// notReady answers a command that a later task still has to build. German,
// because an admin reads it on the server.
func notReady(stderr io.Writer, name string) int {
	fmt.Fprintf(stderr, "%s: Dieser Teil ist noch nicht gebaut.\n", name)
	return 1
}
