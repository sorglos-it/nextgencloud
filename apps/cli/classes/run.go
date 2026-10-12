package classes

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
)

// Usage is what ngc-store prints for a wrong call. Messages of ngc-store are
// German: they are read by whoever runs their own store.
const Usage = `ngc-store packt Apps und unterschreibt Stores.

Aufrufe:
  ngc-store pack <Verzeichnis> <Paket.ngc>        Paket einer App schreiben
  ngc-store encrypt <Paket.ngc> <Schlüsseldatei>  Kauf-App verschlüsseln
  ngc-store catalog <Verzeichnis> <Schlüsseldatei> <Store> <Folge> [Basisadresse]
                                                  Katalog schreiben und unterschreiben
  ngc-store license <App> <Installation> <Schlüsseldatei> <Inhaltsschlüssel> <Installationsschlüssel> <Store>
                                                  Lizenz für einen Test-Store
  ngc-store release <Verzeichnis> <Schlüsseldatei>  Release zum Testen unterschreiben
  ngc-store check <Verzeichnis> <Teil> <Architektur>  Release prüfen wie verify.sh

Zusätze von "license", vor den Angaben:
  -per site|user   Lizenz je Anlage (Vorgabe) oder je Nutzer
  -users <Zahl>    je Nutzer: wie viele Nutzer das Recht bekommen dürfen
  -expires <Zeit>  Abo: Unix-Sekunden, ab wann sie abläuft
  -grace <Sek.>    Nachfrist; 0 nimmt die Vorgabe des Vertrags
  -id <Kennung>    Kennung der Lizenz; leer zieht eine zufällige
  -issued <Zeit>   Unix-Sekunden der Ausstellung; Vorgabe ist jetzt

Geheime Schlüssel stehen nie in einem Aufruf, nur in einer Datei, die
ngc-store mit Rechten 0600 anlegt (Windows kann diese Rechte nicht, dort
schützt nur der Ordner): ein Aufruf steht in der Prozessliste und ist für
jeden anderen lesbar. <Installationsschlüssel> ist der öffentliche Schlüssel
der Installation und darf darum im Aufruf stehen. Fehlt eine Schlüsseldatei,
legt ngc-store sie an und nennt nur ihren Pfad, nie ihren Inhalt.
`

// Run carries out one call of ngc-store and returns the exit status: 0 for
// done, 1 for a call that failed, 2 for a call Run does not understand.
//
// Run is the one place that reads the clock (the issue time of a licence)
// and the one place that creates a missing key file, so the functions behind
// it stay reproducible and never surprise a caller with a new key.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, Usage)
		return 2
	}
	switch args[0] {
	case "pack":
		if len(args) != 3 {
			fmt.Fprint(stderr, Usage)
			return 2
		}
		m, sum, err := Pack(args[1], args[2])
		if err != nil {
			fmt.Fprintf(stderr, "Paket nicht geschrieben: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s %s\n%s\n", m.ID, m.Version, hex.EncodeToString(sum))
		return 0
	case "catalog":
		if len(args) != 5 && len(args) != 6 {
			fmt.Fprint(stderr, Usage)
			return 2
		}
		seq, err := strconv.ParseUint(args[4], 10, 64)
		if err != nil {
			fmt.Fprint(stderr, Usage)
			return 2
		}
		base := ""
		if len(args) == 6 {
			base = args[5]
		}
		// The time of signing, not of packing: a catalog carries issued and
		// seq, so it is a document of a moment. The checksums in it do not
		// depend on a clock (Pack).
		opts := CatalogOpts{Store: args[3], Seq: seq, Issued: time.Now().Unix(), BaseURL: base}
		env, err := Catalog(args[1], args[2], opts)
		if err != nil {
			fmt.Fprintf(stderr, "Katalog nicht geschrieben: %v\n", err)
			return 1
		}
		out := filepath.Join(args[1], CatalogFile)
		if err := writeCatalog(out, env); err != nil {
			fmt.Fprintf(stderr, "Katalog nicht geschrieben: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s %d %s\n", opts.Store, seq, out)
		return 0
	case "encrypt":
		if len(args) != 3 {
			fmt.Fprint(stderr, Usage)
			return 2
		}
		return runEncrypt(args[1], args[2], stdout, stderr)
	case "license":
		return runLicense(args[1:], stdout, stderr)
	case "release", "check":
		// Task 29 of the API plan fills these in; until then the name is known
		// but does nothing.
		fmt.Fprintf(stderr, "ngc-store %s ist noch nicht gebaut\n", args[0])
		return 1
	}
	fmt.Fprint(stderr, Usage)
	return 2
}

// writeCatalog puts the signed catalog beside the packages: first a
// neighbouring file, then the rename. A run that dies half way must never
// leave a catalog.json that is half a document — the next run reads it to
// find the last sequence number.
func writeCatalog(path string, env sign.Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// runEncrypt seals a package. The output is the package's own name with
// EncSuffix behind it ("app.ngc" becomes "app.ngc.enc"), beside it: that is
// the name Catalog of Task 28 looks for, and both halves are merged now, so
// this is the one constant and not a literal. Encrypt itself takes the
// output path, so anything that needs another name says so.
//
// A missing key file is created here (0600, never overwritten), because
// whoever encrypts their first paid app has no key yet. What is printed is
// the path, never the key.
func runEncrypt(in, keyFile string, stdout, stderr io.Writer) int {
	if !strings.HasSuffix(in, ".ngc") {
		fmt.Fprint(stderr, "Das Paket muss auf .ngc enden\n")
		return 1
	}
	if _, err := os.Stat(keyFile); errors.Is(err, os.ErrNotExist) {
		if _, err := NewContentKey(keyFile); err != nil {
			fmt.Fprintf(stderr, "Schlüsseldatei nicht angelegt: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "Schlüsseldatei angelegt: %s\n", keyFile)
	}
	out := in + EncSuffix
	if err := Encrypt(in, out, keyFile); err != nil {
		fmt.Fprintf(stderr, "Nicht verschlüsselt: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s\n", out)
	return 0
}

// runLicense signs one licence and prints the envelope as JSON. A missing
// signing key file is created here and its public key printed, because that
// is what goes into the trust list of the installation (trust.Store.Key).
func runLicense(args []string, stdout, stderr io.Writer) int {
	var opts LicenseOpts
	fs := flag.NewFlagSet("license", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, Usage) }
	fs.StringVar(&opts.Per, "per", "", "site or user")
	fs.IntVar(&opts.Users, "users", 0, "per user: how many users")
	fs.Int64Var(&opts.Expires, "expires", 0, "Unix seconds the licence expires")
	fs.Int64Var(&opts.Grace, "grace", 0, "grace period in seconds")
	fs.StringVar(&opts.ID, "id", "", "licence ID")
	fs.Int64Var(&opts.Issued, "issued", 0, "Unix seconds of issue")
	if fs.Parse(args) != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) != 6 {
		fmt.Fprint(stderr, Usage)
		return 2
	}
	app, installation, keyFile, contentKeyFile, installKey := rest[0], rest[1], rest[2], rest[3], rest[4]
	opts.Store = rest[5]
	if opts.Issued == 0 {
		opts.Issued = time.Now().Unix()
	}
	if _, err := os.Stat(keyFile); errors.Is(err, os.ErrNotExist) {
		priv, err := NewStoreKey(keyFile)
		if err != nil {
			fmt.Fprintf(stderr, "Schlüsseldatei nicht angelegt: %v\n", err)
			return 1
		}
		pub := priv.Public().(ed25519.PublicKey)
		fmt.Fprintf(stdout, "Schlüsseldatei angelegt: %s\nÖffentlicher Schlüssel: %s\nKennung: %s\n",
			keyFile, enc.B64.EncodeToString(pub), sign.KeyID(pub))
	}
	env, err := License(app, installation, keyFile, contentKeyFile, installKey, opts)
	if err != nil {
		fmt.Fprintf(stderr, "Lizenz nicht unterschrieben: %v\n", err)
		return 1
	}
	return writeEnvelope(env, stdout, stderr)
}

// writeEnvelope prints a signed envelope as one line of JSON.
func writeEnvelope(env sign.Envelope, stdout, stderr io.Writer) int {
	b, err := json.Marshal(env)
	if err != nil {
		fmt.Fprintf(stderr, "Ergebnis nicht geschrieben: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s\n", b)
	return 0
}
