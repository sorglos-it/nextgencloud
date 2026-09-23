// Package node wires the controller together: commands, the links to the
// APIs, and what the server keeps doing without an API.
package node

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// Options carries what one command took from the command line. The later tasks
// work from it, so the arguments are read in exactly one place.
type Options struct {
	Config string            // where controller.json is
	Pair   config.SecretText // pairing code of "pair"; only Text() gives it
	Arch   string            // target of "image", one out of arches
}

// arches are the targets NGC is built for. A fixed list, because the value
// becomes an argument of the image build later on.
var arches = []string{"amd64", "arm64"}

const (
	// maxCommand bounds what Main repeats back to the admin.
	maxCommand = 32
	// maxOutPath is where a Linux path ends.
	maxOutPath = 4096
)

// Wrong use is answered with one German line, the same one for every mistake:
// a second line with the cause could carry the pairing code back to the screen.
const (
	usagePair  = "Aufruf: ngc-controller pair -code <Kopplungscode>"
	usageImage = "Aufruf: ngc-controller image -arch <amd64|arm64> -out <Datei>"
)

var errImageUse = errors.New("image takes -arch and -out and nothing else")

// Main runs one command and returns the exit code: 0 success, 1 failure,
// 2 wrong use. It never calls os.Exit, so tests can drive it.
func Main(version string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ngc-controller", flag.ContinueOnError)
	// The flag package prints the value it stumbled over, and a pairing code
	// pasted onto a flag ("-version=<code>") is such a value. Every answer is
	// written here instead, in German.
	fs.SetOutput(io.Discard)
	cfgPath := fs.String("config", config.ControllerFile, "configuration file")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) { // -h asks a question, it is no mistake
			writeUsage(stdout)
			return 0
		}
		writeUsage(stderr)
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
		return hold(stderr)
	case "image":
		return buildImage(version, fs.Args()[1:], stdout, stderr)
	default:
		// Named only if it reads like a command. ulh shows the pairing code
		// right before this call, so the word here may be that code, pasted
		// without "pair -code"; it would then stand in the terminal and in the
		// journal, and it is what opens a tunnel to the API.
		if commandLike(cmd) {
			fmt.Fprintf(stderr, "Unbekanntes Kommando %q.\n", cmd)
		}
		writeUsage(stderr)
		return 2
	}
}

// commandLike reports whether s may be repeated to the admin: the name of a
// command is short, lower case and begins with a letter. A pairing or join
// code is none of that, and its prefix is turned away on top of it.
func commandLike(s string) bool {
	if s == "" || len(s) > maxCommand ||
		strings.HasPrefix(s, tunnel.PairPrefix) || strings.HasPrefix(s, tunnel.JoinPrefix) {
		return false
	}
	for i, r := range s {
		if (r < 'a' || r > 'z') && !(r == '-' && i > 0) {
			return false
		}
	}
	return true
}

// writeUsage lists the commands. German, because an admin reads it, and it
// never repeats what was typed.
func writeUsage(w io.Writer) {
	fmt.Fprintf(w, `Aufruf: ngc-controller [-config <Datei>] [<Kommando>]

Kommandos:
  (ohne Kommando)                          den Controller laufen lassen
  pair -code <Kopplungscode>               diesen Server mit einer API koppeln
  terminal                                 Dienst des Terminals, läuft im Container
  hold                                     im Container warten, bis er beendet wird
  image -arch <amd64|arm64> -out <Datei>   Image der Terminal-App schreiben

Flags:
  -config <Datei>   Konfiguration, Vorgabe %s
  -version          Version anzeigen und beenden
`, config.ControllerFile)
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

// parsePair reads the flags of "pair". The code goes through the contract that
// owns it, so nothing but a pairing code gets any further; it writes nothing
// at all, because every message here would carry that code.
func parsePair(cfgPath string, args []string) (Options, error) {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	code := fs.String("code", "", "pairing code of the API")
	if err := fs.Parse(args); err != nil {
		return Options{}, err
	}
	if fs.NArg() != 0 {
		return Options{}, tunnel.ErrPairCode
	}
	if _, err := tunnel.ParsePairCode(*code); err != nil {
		return Options{}, err
	}
	return Options{Config: cfgPath, Pair: config.NewSecretText(*code)}, nil
}

// runTerminal is the service of the terminal app. It runs inside the container
// of that app, not on the server. Task 23 builds it.
func runTerminal(args []string, stdout, stderr io.Writer) int {
	return notReady(stderr, "terminal")
}

// hold waits until its container stops, so an app image needs no program of
// its own. Task 23 builds it.
func hold(stderr io.Writer) int {
	return notReady(stderr, "hold")
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
// to write. Both values later become arguments of another program, so only
// known values get through.
func parseImage(args []string) (Options, string, error) {
	fs := flag.NewFlagSet("image", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	arch := fs.String("arch", "", "architecture of the image")
	out := fs.String("out", "", "file to write the image to")
	if err := fs.Parse(args); err != nil {
		return Options{}, "", err
	}
	if !slices.Contains(arches, *arch) || !validOut(*out) || fs.NArg() != 0 {
		return Options{}, "", errImageUse
	}
	return Options{Arch: *arch}, *out, nil
}

// validOut checks the file "image" writes to. It is opened later by a program
// running as root, so it stays inside the directory the admin named and does
// not grow past what a path can be.
func validOut(p string) bool {
	return p != "" && len(p) <= maxOutPath && !slices.Contains(strings.FieldsFunc(p, isSeparator), "..")
}

// isSeparator reports a path separator. Both count: the image is written on
// Linux, but the tests also run on Windows.
func isSeparator(r rune) bool { return r == '/' || r == '\\' }

// notReady answers a command that a later task still has to build. German,
// because an admin reads it on the server.
func notReady(stderr io.Writer, name string) int {
	fmt.Fprintf(stderr, "%s: Dieser Teil ist noch nicht gebaut.\n", name)
	return 1
}
