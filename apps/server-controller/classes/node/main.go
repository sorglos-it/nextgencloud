// Package node wires the controller together: commands, the links to the
// APIs, and what the server keeps doing without an API.
package node

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/pairing"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// Options carries what one command took from the command line. The later tasks
// work from it, so the arguments are read in exactly one place.
type Options struct {
	Config string            // where controller.json is
	Pair   config.SecretText // pairing code of "pair", read by readCode; only Text() gives it
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
	usagePair  = "Aufruf: ngc-controller pair (den Kopplungscode fragt das Programm danach ab)"
	usageImage = "Aufruf: ngc-controller image -arch <amd64|arm64> -out <Datei>"
)

// What "pair" answers. Each is one fixed German line: the cause would name the
// file it stumbled over, and that file name may be the pairing code itself
// (-config), or it would name the address and the answer of a server the code
// points at. Each line says what to do next, because whoever reads it is
// setting up a server and needs the next step, not a report of what happened.
const (
	msgNoConfig  = "Die Konfiguration dieses Servers fehlt oder ist unvollständig. Bitte den Server mit ulh einrichten."
	msgNoKey     = "Der Schlüssel dieses Servers ließ sich nicht erzeugen. Bitte den Server neu starten und es noch einmal versuchen."
	msgNotPaired = "Die Kopplung ist gescheitert. Ist der Kopplungscode noch gültig und die API erreichbar?"
	msgNotSaved  = "Die Kopplung hat geklappt, aber die Konfiguration ließ sich nicht speichern. Bitte mit einem neuen Kopplungscode wiederholen."
	msgNoAnchor  = "Die Kopplung hat geklappt, aber der Anker ließ sich nicht speichern. Bitte Platz und Rechte auf dem Server prüfen und mit einem neuen Kopplungscode wiederholen."
	// The code no longer stands in the arguments. Whoever still gives the old
	// flag is told where it goes now; the value behind it is never repeated,
	// because that value is the code.
	msgCodeFlag = "Der Kopplungscode wird nicht mehr in der Kommandozeile übergeben. Bitte ngc-controller pair ohne weitere Angaben aufrufen; das Programm fragt den Code dann ab."
	// Nothing came in, or what came in is no pairing code. It names none of
	// it: what was typed may be a code with one letter wrong.
	msgBadCode = "Kein gültiger Kopplungscode. Bitte den Code aus der Shell vollständig einfügen und es noch einmal versuchen."
	// Pairing makes a new key, so a server that was already listed is out of
	// every trust list; it keeps the one API of the code and nothing else.
	// Only an admin can put it back, and nothing else says so.
	msgListAgain = "Dieser Server steht jetzt in keiner Vertrauensliste. Bitte ihn in der Verwaltung von NextGenCloud neu eintragen, sonst kann er seine Aufgaben nicht übernehmen."
)

var (
	errImageUse = errors.New("image takes -arch and -out and nothing else")
	errPairUse  = errors.New("pair takes nothing but the command")
	// errCodeFlag reports the flag that once carried the pairing code.
	errCodeFlag = errors.New("pair no longer takes -code")
)

// Main runs one command and returns the exit code: 0 success, 1 failure,
// 2 wrong use. stdin is where "pair" reads the pairing code. It never calls
// os.Exit, so tests can drive it.
func Main(version string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
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
		return pair(*cfgPath, config.AnchorFile, fs.Args()[1:], stdin, stdout, stderr)
	case "terminal":
		return runTerminal(fs.Args()[1:], stdout, stderr)
	case "hold":
		return hold(stderr)
	case "image":
		return buildImage(version, fs.Args()[1:], stdout, stderr)
	default:
		// Named only if it reads like a command. ulh shows the pairing code
		// right before this call, so the word here may be that code, pasted
		// instead of the command "pair"; it would then stand in the terminal
		// and in the journal, and it is what opens a tunnel to the API.
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
  pair                                     diesen Server mit einer API koppeln
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

// pair joins this server to an API with the code ulh shows: it makes the key
// of this controller, pairs with the API of the code and writes what came
// back into controller.json and the anchor file. Everything else of this
// server (name, roles, database, release key) stands in controller.json
// already, so the file has to be there and to be whole: a pairing whose
// result could not be written would spend the one-time code for nothing.
// anchorPath is config.AnchorFile; only a test points it elsewhere. stdin is
// where the code comes in: the terminal of the admin or the ulh script.
func pair(cfgPath, anchorPath string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	opts, err := parsePair(cfgPath, args, stdin, stderr)
	if err != nil {
		switch {
		case errors.Is(err, errCodeFlag):
			fmt.Fprintln(stderr, msgCodeFlag)
		case errors.Is(err, errNoCode), errors.Is(err, tunnel.ErrPairCode):
			fmt.Fprintln(stderr, msgBadCode)
		default:
			fmt.Fprintln(stderr, usagePair)
		}
		return 2
	}
	// Only opts.Pair carries the code here, and its String hides it. A parsed
	// tunnel.PairCode does the opposite, its String being the code itself, so
	// none is made in this function: pairing.Do reads the text, and
	// pairing.Show takes the anchor alone.
	cfg, err := config.LoadController(opts.Config)
	if err != nil {
		fmt.Fprintln(stderr, msgNoConfig)
		return 1
	}
	key, err := noisep.Keypair(rand.Reader)
	if err != nil {
		fmt.Fprintln(stderr, msgNoKey)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), pairing.Timeout)
	defer cancel()
	res, err := pairing.Do(ctx, pairing.Input{
		Code: opts.Pair.Text(), Name: cfg.Name, Roles: cfg.Roles, Noise: key,
	})
	if err != nil {
		fmt.Fprintln(stderr, msgNotPaired)
		return 1
	}
	// The node ID named this server in a trust list under its old key. With a
	// new key and a new API it belongs to no one until an admin lists it.
	cfg.Node = ""
	cfg.NoiseKey = config.NewSecret(key.Private)
	cfg.APIs = res.APIs
	cfg.Anchor = res.Anchor
	if err := config.SaveController(opts.Config, cfg); err != nil {
		fmt.Fprintln(stderr, msgNotSaved)
		return 1
	}
	if err := config.SaveAnchor(anchorPath, res.Anchor); err != nil {
		fmt.Fprintln(stderr, msgNoAnchor)
		return 1
	}
	// The two lines the admin compares go to stdout, the one thing left to do
	// beside them: a pairing that looked like a success and left the server
	// out of every trust list would be a trap.
	fmt.Fprint(stdout, pairing.Show(res.Anchor, key.Public))
	fmt.Fprintln(stderr, msgListAgain)
	return 0
}

// parsePair reads the call of "pair" and then the pairing code itself. The
// command takes nothing but its own name: the code comes in over stdin
// (readCode), and prompt is the only thing it writes, the question at the
// terminal. The code goes through the contract that owns it before anything
// else happens, so nothing but a pairing code gets any further, and no message
// here names what came in: that is the code.
func parsePair(cfgPath string, args []string, stdin io.Reader, prompt io.Writer) (Options, error) {
	// Before the input is touched: a call with the old flag is answered, not
	// half carried out, and the value of that flag is never looked at.
	if codeFlag(args) {
		return Options{}, errCodeFlag
	}
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return Options{}, errPairUse
	}
	if fs.NArg() != 0 {
		return Options{}, errPairUse
	}
	code, err := readCode(stdin, prompt)
	if err != nil {
		return Options{}, err
	}
	if _, err := tunnel.ParsePairCode(code); err != nil {
		return Options{}, err
	}
	return Options{Config: cfgPath, Pair: config.NewSecretText(code)}, nil
}

// codeFlag reports whether the call still carries -code, the flag that once
// took the pairing code. It reads the words of the call, never their values: a
// value here may be the code itself.
func codeFlag(args []string) bool {
	for _, a := range args {
		if a == "--" || len(a) < 2 || a[0] != '-' {
			return false // the flag package stops here too
		}
		if name, _, _ := strings.Cut(strings.TrimPrefix(a[1:], "-"), "="); name == "code" {
			return true
		}
	}
	return false
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
