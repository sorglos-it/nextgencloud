// sicherung-zurueck is the one step of a restore that nothing in NGC does, and
// it does it with NGC's own code and not with a rebuilt version of it.
//
// WHAT IS MISSING AND HOW IT WAS FOUND. A backed-up state.db put back in place
// opens on a machine that never belonged to this installation - measured on a
// fresh VM out of the Proxmox template, with the dbKey out of the same backup.
// The controller then reads the app rows out of it and tries to bring both apps
// up, and gets exactly this far:
//
//	WARN the runtime directory of an app could not be made, it stays off
//	     app=org.sorglos.terminal
//	     err="runtime: no system user for this container
//	          user: unknown user ngc-terminal-439982dd"
//
// The system user of an app, and the encrypted data directory that is its home,
// are made by the ACTIVATION (install/activate.go) and by nothing else.
// node.Restore looks the user up and makes none - the comment at runtime.RunForApp
// says so in capitals. So after a restore every app stays off for good, and the
// only thing in the product that would put it right is an admin activating the
// app anew out of the store: a round over the network, a licence, a package, for
// two rows that were already in the backup.
//
// This closes that gap and nothing else. For every app id it is given:
//
//  1. runtime.EnsureUser - the system user, with the name the app id gives
//     (sha256 of the id, first 8 hex characters), its home at the data
//     directory, nologin, plus one range in /etc/subuid and /etc/subgid.
//  2. runtime.EnsureData - the empty data directory with its fscrypt policy,
//     the key derived from the dbKey of THIS server, handed over to that user.
//
// Both are the product's own functions. That is the whole point: a restore that
// made the user with its own useradd line would be a second answer to "what is
// an app's user", and the day activate.go changed one of them the two would
// drift apart without anybody noticing.
//
// WHAT IT DOES NOT DO. It puts no data back. Step 2 leaves an EMPTY directory,
// because the kernel refuses to set a policy on a directory that is not - so
// the contents go in afterwards, with tar, and the manifest of
// tools/sicherung.sh says what the tar holds. It also does not ask whether the
// app may run: the licence is the controller's question and it asks it at every
// start.
//
// Aufruf, als root auf dem wiederhergestellten Server:
//
//	sicherung-zurueck -config /etc/ngc/controller.json <app-id> [<app-id> ...]
//
// It is safe to run again: both steps answer for something that is already
// there without changing it, which is what an activation that failed halfway
// needs of them.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

func main() {
	fs := flag.NewFlagSet("sicherung-zurueck", flag.ContinueOnError)
	cfgPath := fs.String("config", config.ControllerFile, "Konfiguration dieses Servers")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "Aufruf: sicherung-zurueck [-config <Datei>] <app-id> [<app-id> ...]")
		os.Exit(2)
	}
	cfg, err := config.LoadController(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Die Konfiguration dieses Servers fehlt oder ist unvollständig.")
		os.Exit(1)
	}
	// The app ids are answered before anything is made: a typo in one of them
	// would otherwise leave a user and a directory behind under a name that
	// belongs to no app.
	for _, app := range fs.Args() {
		if !wire.ValidAppID(app) {
			fmt.Fprintf(os.Stderr, "%q ist keine App-Kennung.\n", app)
			os.Exit(2)
		}
	}
	ctx := context.Background()
	code := 0
	for _, app := range fs.Args() {
		if err := one(ctx, app, cfg.DBKey.Bytes()); err != nil {
			// One app that cannot be put right is one app; the others are not
			// held back by it, for the reason node.Restore gives for the same
			// choice. The exit code still says it, so a script notices.
			fmt.Fprintf(os.Stderr, "%s: %v\n", app, err)
			code = 1
			continue
		}
	}
	if code == 0 {
		fmt.Println("sicherung-zurueck: ok")
	}
	os.Exit(code)
}

// one makes the user and the data directory of a single app.
func one(ctx context.Context, app string, serverKey []byte) error {
	u, err := runtime.EnsureUser(ctx, app)
	if err != nil {
		return err
	}
	fmt.Printf("%-24s Benutzer %s (uid %d, gid %d)\n", app, runtime.UserName(app), u.UID, u.GID)
	dir, err := runtime.EnsureData(ctx, app, u, serverKey)
	if err != nil {
		// The one answer worth its own sentence: the disk cannot encrypt a
		// directory. Every other failure names itself, this one names a
		// filesystem feature the admin has to go and turn on, and
		// NoFscryptText is the sentence the rest of NGC shows for it.
		if errors.Is(err, runtime.ErrNoFscrypt) {
			return errors.New(runtime.NoFscryptText)
		}
		return err
	}
	fmt.Printf("%-24s Datenordner %s, verschlüsselt, leer - Inhalt jetzt mit tar hinein\n", app, dir)
	return nil
}
