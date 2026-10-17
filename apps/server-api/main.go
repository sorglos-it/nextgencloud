// Command ngc-api is the API of NGC: sessions, rights, forwarding.
//
// Three ways to start it, and no more:
//
//	ngc-api                  run the API
//	ngc-api -join <code>     join an installation and fetch its shared keys
//	ngc-api migrate          bring the database schema up to this release
//	ngc-api backup <file>    write a copy of the database to <file>
//
// migrate is a subcommand of this program and not a library the controller
// calls: one runner over one schema (Verträge V1.2, Punkt 28). The controller
// starts it like any other program.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/apitls"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbopen"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/join"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/server"
	"github.com/sorglos-it/nextgencloud/shared/config"

	// The database part of this installation. Blank, because nothing of this
	// program calls it: it registers the opener and the migrator with
	// classes/dbopen in its init, and that import is the whole decision that
	// THIS program carries the drivers and the migrations. Without the line
	// server.Build comes back with dbopen.ErrNoOpener and ngc-api does not
	// start -- which is exactly what it did until this line existed.
	_ "github.com/sorglos-it/nextgencloud/apps/server-db/classes/register"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// Version is set when building: -ldflags "-X main.Version=1.0.0".
var Version = "0.0.0-dev"

// Where ngc-api -join puts the two private keys it makes. They go to the run
// directory and not next to api.json: the ulh script reads all three files
// (these two and join.SecretFile) and writes them into api.json itself, and
// a reboot clears what is left there.
var (
	joinNoiseFile = filepath.Join(config.RunDir, "join-noise")
	joinSignFile  = filepath.Join(config.RunDir, "join-sign")
)

func main() { os.Exit(run(os.Args[1:])) }

// run is main with the exit code as a value, so a test can drive it.
func run(args []string) int {
	args, cmd, arg, ok := subcommand(args)
	if !ok {
		return 2
	}
	fs := flag.NewFlagSet("ngc-api", flag.ContinueOnError)
	version := fs.Bool("version", false, "print the version and exit")
	path := fs.String("config", config.APIFile, "configuration file")
	joinCode := fs.String("join", "", "join an installation with a join code and fetch its shared keys")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *version {
		fmt.Printf("ngc-api %s (contract %d)\n", Version, tunnel.Contract)
		return 0
	}
	// signal.NotifyContext is what makes SIGTERM a clean stop: systemd sends
	// it, the context ends, the listener takes no new request and the open
	// ones get server.ShutdownWait to finish. A second signal kills the
	// process, which is the operating system's business and not ours.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch {
	case cmd == "migrate":
		return migrate(ctx, *path)
	case cmd == "backup":
		return backupRun(ctx, *path, arg)
	case *joinCode != "":
		return joinRun(ctx, *path, *joinCode)
	}
	return serve(ctx, *path)
}

// subcommand takes the one subcommand out of the arguments, wherever it
// stands, and gives the rest back for the flags.
//
// It is lifted out and not left to flag.FlagSet for one measured reason: that
// package stops at the first argument which is not a flag, so "ngc-api
// migrate -config /etc/ngc/other.json" would reach migrate with the DEFAULT
// configuration -- and that is a migration of the wrong database, silently.
// Anything other than one known subcommand is refused here rather than taken
// for "run the API".
func subcommand(args []string) (rest []string, cmd, arg string, ok bool) {
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "" || a[0] == '-' {
			rest = append(rest, a)
			// A flag that takes a value may carry it as the next argument,
			// and that value must not be read as a subcommand.
			if (a == "-config" || a == "--config" || a == "-join" || a == "--join") && i+1 < len(args) {
				i++
				rest = append(rest, args[i])
			}
			continue
		}
		// The one word that is not a flag and not the subcommand belongs to
		// "backup": the file it writes. It is an argument and not a flag
		// because it is not optional -- a backup without a target is not a
		// backup with a default but a question nobody asked. "migrate" takes
		// none, and a second word for either is refused rather than dropped:
		// "ngc-api backup /mnt/a /mnt/b" would otherwise write one of the
		// two and say nothing about the other.
		switch {
		case cmd == "":
			if a != "migrate" && a != "backup" {
				fmt.Fprintf(os.Stderr, "ngc-api: unbekannter Befehl %q\n", a)
				return nil, "", "", false
			}
			cmd = a
		case cmd == "backup" && arg == "":
			arg = a
		default:
			fmt.Fprintf(os.Stderr, "ngc-api: zu viele Angaben hinter %q: %q\n", cmd, a)
			return nil, "", "", false
		}
	}
	// A target is required, and the refusal names the whole call: an admin who
	// typed "ngc-api backup" has a backup plan in mind, and the worst answer
	// here is a default path they never see again.
	if cmd == "backup" && arg == "" {
		fmt.Fprintln(os.Stderr, "ngc-api: backup braucht den Namen der Datei, die geschrieben werden soll: ngc-api backup /pfad/sicherung.db")
		return nil, "", "", false
	}
	return rest, cmd, arg, true
}

// load reads api.json and says so in German when it cannot.
func load(path string) (*config.API, bool) {
	cfg, err := config.LoadAPI(path)
	if err != nil {
		slog.Error("die Konfiguration lässt sich nicht lesen", "file", path, "error", err)
		return nil, false
	}
	return cfg, true
}

// serve runs the API.
func serve(ctx context.Context, path string) int {
	cfg, ok := load(path)
	if !ok {
		return 1
	}
	slog.Info("ngc-api starting", "version", Version, "node", cfg.Node, "listen", cfg.Listen)
	srv, err := server.Build(ctx, cfg, Version)
	if err != nil {
		slog.Error("die API kann nicht starten", "error", err)
		return 1
	}
	if err := srv.Run(ctx); err != nil {
		slog.Error("die API ist abgebrochen", "error", err)
		return 1
	}
	return 0
}

// migrate brings the schema up to date and prints what it did.
func migrate(ctx context.Context, path string) int {
	cfg, ok := load(path)
	if !ok {
		return 1
	}
	if err := dbopen.Migrate(ctx, cfg.DB, dbopen.OwnerOf(cfg.Node), os.Stdout); err != nil {
		if errors.Is(err, dbopen.ErrNoMigrator) {
			slog.Error("dieses Programm wurde ohne die Datenbank-Migrationen gebaut")
			return 1
		}
		slog.Error("die Migration ist fehlgeschlagen", "error", err)
		return 1
	}
	return 0
}

// joinRun is ngc-api -join: it asks a running API of the installation to take
// this server in and waits for an admin to sign it into the trust list.
//
// api.json is already there when this runs -- the ulh script writes it first,
// with keys of its own -- but the shared keys in it are not yet the
// installation's. That is what this fetches, and the script writes them in.
func joinRun(ctx context.Context, path, code string) int {
	cfg, ok := load(path)
	if !ok {
		return 1
	}
	host, err := apitls.Host(cfg.URL)
	if err != nil {
		slog.Error("die Adresse url in api.json ist keine Tunnel-Adresse", "error", err)
		return 1
	}
	// The pin the request carries has to be the pin of the key this server
	// will really answer with, so it comes from the same call the running
	// server uses and not from a second reading of its own.
	_, pin, err := apitls.Ensure(cfg.Cert, cfg.CertKey, host, time.Now())
	if err != nil {
		slog.Error("das TLS-Zertifikat der API lässt sich nicht anlegen oder lesen", "error", err)
		return 1
	}
	err = join.Run(ctx, join.JoinConfig{
		Code: code, Node: cfg.Node, URL: cfg.URL, TLS: pin,
		NoiseFile: joinNoiseFile, SignFile: joinSignFile,
	})
	if err != nil {
		slog.Error("der Beitritt ist fehlgeschlagen", "error", err)
		return 1
	}
	return 0
}

// backupRun is ngc-api backup <file>. It writes a copy of the database this
// configuration names and then says the one sentence the file itself cannot.
//
// THAT SENTENCE IS THE POINT OF THIS FUNCTION as much as the copy is. The
// file is encrypted with db.key of api.json, and nothing of that key is in
// it. A backup kept without the key opens on no machine, ever, by anybody --
// and a plan that keeps only the database is exactly what an admin builds who
// was never told. It is printed to stdout, after the copy and not before, so
// it is read as what happened and not as advice that might apply.
//
// The key itself is NOT printed, and not the path it came from either: a
// backup run goes into a journal, a report and a screen somebody photographs.
// What is named is the file to keep beside the copy, which is api.json, and
// that path the admin already has -- it is the -config they just typed.
func backupRun(ctx context.Context, path, target string) int {
	cfg, ok := load(path)
	if !ok {
		return 1
	}
	if err := dbopen.Backup(ctx, cfg.DB, target); err != nil {
		if errors.Is(err, dbopen.ErrNoBackuper) {
			slog.Error("dieses Programm wurde ohne den Datenbankteil gebaut, es kann nicht sichern")
			return 1
		}
		slog.Error("die Sicherung ist fehlgeschlagen", "error", err)
		return 1
	}
	fmt.Printf("Die Sicherung liegt in %s.\n", target)
	fmt.Printf("WICHTIG: Diese Datei ist verschlüsselt, und der Schlüssel steht NICHT darin.\n"+
		"Er steht in %s. Ohne diese Konfiguration lässt sich die Sicherung auf keinem\n"+
		"Rechner mehr öffnen - von niemandem, auch nicht von NextGenCloud. Bewahren Sie\n"+
		"beide Dateien auf, und den Schlüssel an einem anderen Ort als die Sicherung.\n", path)
	return 0
}
