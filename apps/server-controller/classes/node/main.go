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
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/pairing"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	// Named, because this package already has a terminal: ask.go asks whether
	// a reader is the admin's terminal. Two unrelated things, one word - the
	// same collision that broke the build when tasks 15 and 16 both wrote a
	// disk. The import carries the name, so nothing of the older code moves.
	ctrlrt "github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	termsvc "github.com/sorglos-it/nextgencloud/apps/server-controller/classes/terminal"
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

// arches are the targets NGC is built for. The list itself stands in the
// runtime package, where the value becomes an argument of podman: two lists
// that have to agree are two places to change and one to forget.
var arches = ctrlrt.Arches

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
	// Both commands run inside a container and take nothing: the channel is
	// their input. A word behind them would be one nobody put there on
	// purpose, so it is answered instead of ignored.
	usageTerminal = "Aufruf: ngc-controller terminal (ohne weitere Angaben, läuft im Container)"
	usageHold     = "Aufruf: ngc-controller hold (ohne weitere Angaben, läuft im Container)"
)

// What "pair" answers. Each is one fixed German line: the cause would name the
// file it stumbled over, and that file name may be the pairing code itself
// (-config), or it would name the address and the answer of a server the code
// points at. Each line says what to do next, because whoever reads it is
// setting up a server and needs the next step, not a report of what happened.
const (
	msgNoConfig = "Die Konfiguration dieses Servers fehlt oder ist unvollständig. Bitte den Server mit ulh einrichten."
	msgNoKey    = "Der Schlüssel dieses Servers ließ sich nicht erzeugen. Bitte den Server neu starten und es noch einmal versuchen."
	// What the pairing answers when it did not come off. One line per
	// CATEGORY of pairing.Do, and every one of them says what to do next --
	// that was the rule of the single line this replaces and it stays the
	// rule. None of them names a value: not the address, not the path of a
	// file, not the code, no answer of a server and no stack. Measured on
	// 17.10.2026, the error behind them quotes the address AND the query, and
	// that query is derived from the one-time secret; it never leaves
	// classes/pairing.
	//
	// The one line before them named two possibilities and ruled out neither,
	// and the real cause of 17.10.2026 -- a missing query -- was none of the
	// two. These lines name no cause either, but they do say what the pairing
	// got as far as: nothing sent, no answer, the wrong server, or a refusal.
	// Whether the code is spent follows from that, and that is the one thing
	// the admin needs for his next step.
	msgNotPaired    = "Die Kopplung ist gescheitert. Bitte einen neuen Kopplungscode in NextGenCloud erzeugen und es noch einmal versuchen."
	msgPairNothing  = "Die Kopplung wurde nicht abgeschickt: dieser Server passt mit seinen eigenen Angaben nicht zu einer Kopplung. Bitte den Server mit ulh neu einrichten. Der Kopplungscode ist noch gültig."
	msgPairNoAnswer = "Die API hat nicht geantwortet. Bitte prüfen, ob der Server von NextGenCloud läuft und von hier aus erreichbar ist, dann noch einmal versuchen. Der Kopplungscode ist noch gültig."
	msgPairTooSlow  = "Die API hat nicht rechtzeitig geantwortet. Bitte in ein paar Minuten noch einmal versuchen. Ob der Kopplungscode dabei verbraucht wurde, lässt sich von hier aus nicht sagen; falls es wieder scheitert, einen neuen erzeugen."
	msgPairWrongAPI = "Es hat ein anderer Server geantwortet als der, den der Kopplungscode nennt. Bitte nicht wiederholen: in NextGenCloud einen neuen Kopplungscode erzeugen und, wenn es wieder geschieht, das Netz dieses Servers prüfen."
	msgPairRefused  = "Die API hat den Kopplungscode abgelehnt. Er ist wahrscheinlich schon benutzt oder abgelaufen. Bitte in NextGenCloud einen neuen erzeugen und diesen einfügen."
	// msgWorked is the only word of success on this way, and all three lines
	// that get past the API open with it: the two lines on stdout are
	// fingerprints to compare, and without this half sentence each of the
	// three reads like the report of a failure beside them. It stands here
	// once, so a rewording cannot say something different in one of them.
	msgWorked   = "Die Kopplung hat geklappt"
	msgNotSaved = msgWorked + ", aber die Konfiguration ließ sich nicht speichern. Bitte mit einem neuen Kopplungscode wiederholen."
	msgNoAnchor = msgWorked + ", aber der Anker ließ sich nicht speichern. Bitte Platz und Rechte auf dem Server prüfen und mit einem neuen Kopplungscode wiederholen."
	// The code no longer stands in the arguments. Whoever still gives the old
	// flag is told where it goes now; the value behind it is never repeated,
	// because that value is the code.
	msgCodeFlag = "Der Kopplungscode wird nicht mehr in der Kommandozeile übergeben. Bitte ngc-controller pair ohne weitere Angaben aufrufen; das Programm fragt den Code dann ab."
	// Nothing came in, or what came in is no pairing code. It names none of
	// it: what was typed may be a code with one letter wrong. Where the code
	// comes from is named the way the line below names it: whoever reads "die
	// Shell" at a bash prompt takes it for the shell he is standing in.
	msgBadCode = "Kein gültiger Kopplungscode. Bitte den Code aus NextGenCloud vollständig einfügen und es noch einmal versuchen."
	// Pairing makes a new key, so a server that was already listed is out of
	// every trust list; it keeps the one API of the code and nothing else.
	// Only an admin can put it back, and nothing else says so.
	//
	// "eintragen" fits both pairings, the first one and a later one: the first
	// time the server stood in no list before either. Where that happens is
	// said as far as it is settled - an admin signed in to NextGenCloud -
	// because the contract knows the list (ngc.users/nodes) but no way through
	// a menu yet.
	msgListAgain = msgWorked + ". Dieser Server steht jetzt in keiner Vertrauensliste. Bitte als Administrator bei NextGenCloud anmelden und diesen Server dort eintragen, sonst kann er seine Aufgaben nicht übernehmen."
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
	case "backup":
		return backup(*cfgPath, fs.Args()[1:], stdout, stderr)
	case "terminal":
		return runTerminal(fs.Args()[1:], stdin, stdout, stderr)
	case "hold":
		return hold(fs.Args()[1:], stderr)
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
  backup <Datei>                           die eigene Datenbank dieses Servers
                                           sichern; der Controller darf dabei
                                           weiterlaufen
  terminal                                 Dienst des Terminals, läuft im Container
  hold                                     im Container warten, bis er beendet wird
  image -arch <amd64|arm64> -out <Datei>   Abbild der Terminal-App schreiben;
                                           braucht podman, läuft auf einem
                                           Baurechner, nie auf dem Zielserver

Flags:
  -config <Datei>   Konfiguration, Vorgabe %s
  -version          Version anzeigen und beenden
`, config.ControllerFile)
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
	// The call is answered before anything at all is read, and the value
	// behind a flag is never looked at: that value may be the code.
	if err := pairUse(args); err != nil {
		if errors.Is(err, errCodeFlag) {
			fmt.Fprintln(stderr, msgCodeFlag)
		} else {
			fmt.Fprintln(stderr, usagePair)
		}
		return 2
	}
	// The configuration is read before the admin is asked. A code is good for
	// one pairing and for tunnel.PairLifetime (86400 seconds, so 24 hours -
	// measured 12.10.2026; this said "thirty seconds" before, which is
	// pairing.Timeout, the bound of the HTTP request): whoever pastes it into
	// the question on a server without controller.json has spent it and needs
	// a new one, and nothing of the pairing could have been written anyway.
	cfg, err := config.LoadController(cfgPath)
	if err != nil {
		fmt.Fprintln(stderr, msgNoConfig)
		return 1
	}
	// Only opts.Pair carries the code here, and its String hides it. A parsed
	// tunnel.PairCode does the opposite, its String being the code itself, so
	// none is made in this function: pairing.Do reads the text, and
	// pairing.Show takes the anchor alone.
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
		fmt.Fprintln(stderr, pairFailure(err))
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

// pairFailure picks the line for what pairing.Do came back with. It asks the
// categories and never the text: the text of a transport error carries the
// address of the API and the query derived from the one-time secret, which is
// why pairing.Do does not hand it out in the first place. An error that is
// none of the categories gets the line that asks for a new code -- the
// cautious answer, because an unknown failure may well have spent it.
func pairFailure(err error) string {
	switch {
	case errors.Is(err, pairing.ErrInput):
		return msgPairNothing
	case errors.Is(err, tunnel.ErrPairCode):
		return msgBadCode
	case errors.Is(err, pairing.ErrNoAnswer):
		return msgPairNoAnswer
	case errors.Is(err, pairing.ErrTooSlow):
		return msgPairTooSlow
	case errors.Is(err, pairing.ErrWrongAPI):
		return msgPairWrongAPI
	case errors.Is(err, tunnel.ErrPair):
		return msgPairRefused
	}
	return msgNotPaired
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
	if err := pairUse(args); err != nil {
		return Options{}, err
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

// pairUse reads the call of "pair" and nothing else: the command takes no
// flag and no further word. It touches no input, so a wrong call is answered
// before the admin is asked for anything and before any file is opened.
func pairUse(args []string) error {
	if codeFlag(args) {
		return errCodeFlag
	}
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return errPairUse
	}
	if fs.NArg() != 0 {
		return errPairUse
	}
	return nil
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
// of that app, not on the server: stdin carries the frames of the channel and
// stdout the raw output of the shell (package terminal). Its own lines go to
// os.Stderr, which is the pts of that terminal in the container, so nothing of
// the channel can reach the log of this server.
func runTerminal(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, usageTerminal)
		return 2
	}
	ctx, stop := stopOnSignal()
	defer stop()
	return termsvc.Run(ctx, stdin, stdout)
}

// hold waits until its container stops, so an app image needs no program of
// its own. stderr is where a wrong call is answered; the wait itself says
// nothing, because a line every time a container starts would be a line
// nobody reads.
func hold(args []string, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, usageHold)
		return 2
	}
	ctx, stop := stopOnSignal()
	defer stop()
	return termsvc.Hold(ctx)
}

// stopOnSignal is the context both container commands run under. The engine
// stops a container with SIGTERM, and an admin who runs the command by hand
// stops it with SIGINT; neither is an error, so both end the command with the
// code of what it was doing.
func stopOnSignal() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
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

// Nothing answers "Dieser Teil ist noch nicht gebaut." any more: "image" was
// the last command that did, and it is built (image.go). The helper is gone on
// purpose - a command that is not built is easier to find as a missing case of
// the switch than as a line that answers politely.

// usageBackup is the one answer a wrong call to backup gets. It names the
// whole call, because an admin who typed "ngc-controller backup" has a backup
// plan in mind and the worst answer is a file under a default name they never
// find again.
const usageBackup = "Aufruf: ngc-controller backup <Datei>" + "\n" +
	"Die Datei ist der Name der Sicherung, die geschrieben wird - ein eigener Name, nicht der der Datenbank."

// backup is ngc-controller backup <Datei>: it copies this server's own
// database and then says the one sentence the copy cannot say about itself.
//
// Why the sentence is here and not only in the documentation: the file is
// encrypted with dbKey of controller.json and carries nothing of that key. A
// backup kept without that file opens on no machine, by anybody, ever - and a
// plan that keeps only the database is exactly what an admin builds who was
// never told. It is printed AFTER the copy, so it is read as what happened
// and not as advice that might apply.
//
// The key is not printed and its own path is not printed either: a backup run
// goes into a journal, a report and a screen somebody photographs. What is
// named is the file to keep beside the copy, and that path the admin just
// typed themselves.
func backup(cfgPath string, args []string, stdout, stderr io.Writer) int {
	// The call is answered before the configuration is read: a second file
	// name, or none, is a question nobody can answer by guessing.
	if len(args) != 1 || args[0] == "" || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(stderr, usageBackup)
		return 2
	}
	cfg, err := config.LoadController(cfgPath)
	if err != nil {
		fmt.Fprintln(stderr, msgNoConfig)
		return 1
	}
	// The same configuration the controller itself opens its store from, and
	// built in the same place: openState names driver, path and key, and a
	// second description of them here could drift from it.
	if err := state.Backup(config.DB{
		Driver: config.DriverSQLite,
		Path:   cfg.DB,
		Key:    cfg.DBKey,
	}, args[0]); err != nil {
		fmt.Fprintf(stderr, "Die Sicherung ist fehlgeschlagen: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Die Sicherung liegt in %s.\n", args[0])
	fmt.Fprintf(stdout, "WICHTIG: Diese Datei ist verschlüsselt, und der Schlüssel steht NICHT darin.\n"+
		"Er steht in %s. Ohne diese Konfiguration lässt sich die Sicherung auf keinem\n"+
		"Rechner mehr öffnen - von niemandem, auch nicht von NextGenCloud. Bewahren Sie\n"+
		"beide Dateien auf, und den Schlüssel an einem anderen Ort als die Sicherung.\n", cfgPath)
	return 0
}
