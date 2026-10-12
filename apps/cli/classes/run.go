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
  ngc-store release -version <Fassung> -seq <Folge> <Verzeichnis> <Schlüsseldatei>
                                                  Release zum Testen unterschreiben
  ngc-store check <Verzeichnis> <Teil> <Architektur>  Release prüfen wie verify.sh
  ngc-store check -store <Store> <Verzeichnis>    ganzen Store prüfen

Zusätze von "release", vor den Angaben:
  -version <Fassung>  die NGC-Fassung dieses Release; nötig
  -seq <Folge>        Folgenummer, höher als die letzte; nötig
  -notes <Text>       was sich geändert hat
  -base <Adresse>     wo die Dateien liegen; leer schreibt nur die Namen
  -issued <Zeit>      Unix-Sekunden der Ausstellung; Vorgabe ist jetzt

Zusätze von "check":
  -store <Store>      Kennung des Stores; prüft catalog.json statt release.json
  -seq <Folge>        die neueste schon gesehene Folgenummer

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
		// A missing key file is created here, the same way encrypt, license
		// and release do it: catalog was the one subcommand that failed with
		// the bare os.Open error instead, which reads like a broken tool to
		// whoever signs their first catalog. Never overwritten, and what is
		// printed is the path and the public half, never the key.
		if _, err := os.Stat(args[2]); errors.Is(err, os.ErrNotExist) {
			priv, err := NewStoreKey(args[2])
			if err != nil {
				fmt.Fprintf(stderr, "Schlüsseldatei nicht angelegt: %v\n", err)
				return 1
			}
			pub := priv.Public().(ed25519.PublicKey)
			fmt.Fprintf(stdout, "Schlüsseldatei angelegt: %s\nÖffentlicher Schlüssel: %s\nKennung: %s\n",
				args[2], enc.B64.EncodeToString(pub), sign.KeyID(pub))
		}
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
	case "release":
		return runRelease(args[1:], stdout, stderr)
	case "check":
		return runCheck(args[1:], stdout, stderr)
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
	return writeFileAtomic(path, b)
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

// runRelease signs a release for testing and writes it beside the files, as
// the catalog subcommand writes the catalog: the same neighbouring file and
// rename, for the same reason (writeCatalog). It also writes the public half
// of the key as ReleaseKeyFile, because a release directory without it
// cannot be checked — neither by Check nor by verify.sh on a Linux server —
// and a release nobody can check is the one thing this tool must not
// produce.
//
// Only the public half is written. The secret seed stays in the key file the
// caller named, and a missing one is created here (0600, never overwritten),
// because whoever signs their first test release has no key yet. What is
// printed is the path and the public key, never the seed.
//
// The real release key of NGC lies on a hardware stick and never on a
// server (Entwurf 4.8). This subcommand signs releases for testing, and the
// usage says so.
func runRelease(args []string, stdout, stderr io.Writer) int {
	var opts ReleaseOpts
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, Usage) }
	fs.StringVar(&opts.Version, "version", "", "the NGC version of this release")
	fs.Uint64Var(&opts.Seq, "seq", 0, "sequence number, higher than the last one")
	fs.StringVar(&opts.Notes, "notes", "", "what changed, shown to admins")
	fs.StringVar(&opts.BaseURL, "base", "", "where the files lie; empty writes names")
	fs.Int64Var(&opts.Issued, "issued", 0, "Unix seconds of issue; now by default")
	if fs.Parse(args) != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) != 2 || opts.Version == "" || opts.Seq == 0 {
		fmt.Fprint(stderr, Usage)
		return 2
	}
	dir, keyFile := rest[0], rest[1]
	// The time of signing, not of building: a release carries issued and
	// seq, so it is a document of a moment. The checksums in it do not
	// depend on a clock.
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
	env, err := Release(dir, keyFile, opts)
	if err != nil {
		fmt.Fprintf(stderr, "Release nicht unterschrieben: %v\n", err)
		return 1
	}
	if err := writePublicKey(dir, ReleaseKeyFile, keyFile); err != nil {
		fmt.Fprintf(stderr, "Release nicht geschrieben: %v\n", err)
		return 1
	}
	out := filepath.Join(dir, ReleaseFile)
	if err := writeCatalog(out, env); err != nil {
		fmt.Fprintf(stderr, "Release nicht geschrieben: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s %d %s\n", opts.Version, opts.Seq, out)
	return 0
}

// writePublicKey puts the public half of the signing key in keyFile into the
// directory under name. It reads the key file and throws the seed away right
// after taking the public half (readStoreKey clears what it read, clear
// takes the rest): nothing of the secret reaches the directory a release is
// served from.
func writePublicKey(dir, name, keyFile string) error {
	priv, err := readStoreKey(keyFile)
	if err != nil {
		return err
	}
	defer clear(priv)
	b, err := PublicKeyPEM(priv.Public().(ed25519.PublicKey))
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, name), b)
}

// runCheck is the one subcommand that is meant to go red: it is what whoever
// runs a store or a release server uses to find out that it is broken
// before an installation does.
//
// Two calls, told apart by a flag and not by counting arguments: a store ID
// may look like the name of a part, and a check that guessed which of the
// two it was asked for could check the wrong thing and print "ok".
//
//	check <Verzeichnis> <Teil> <Architektur>   one file of a release
//	check -store <Store> [-seq <Folge>] <Verzeichnis>   a whole store
func runCheck(args []string, stdout, stderr io.Writer) int {
	var storeID string
	var seq uint64
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, Usage) }
	fs.StringVar(&storeID, "store", "", "check a store of this ID instead of a release")
	fs.Uint64Var(&seq, "seq", 0, "the newest sequence number already seen")
	if fs.Parse(args) != nil {
		return 2
	}
	rest := fs.Args()
	if storeID != "" {
		if len(rest) != 1 {
			fmt.Fprint(stderr, Usage)
			return 2
		}
		if err := CheckStore(rest[0], storeID, seq); err != nil {
			fmt.Fprintf(stderr, "store wrong: %v\n", err)
			return 1
		}
		fmt.Fprint(stdout, "store ok\n")
		return 0
	}
	if len(rest) != 3 {
		fmt.Fprint(stderr, Usage)
		return 2
	}
	if err := Check(rest[0], rest[1], rest[2]); err != nil {
		fmt.Fprintf(stderr, "release wrong: %v\n", err)
		return 1
	}
	fmt.Fprint(stdout, "release ok\n")
	return 0
}

// writeFileAtomic writes b to path through a neighbouring file and a
// rename. A run that dies half way must never leave half a document: the
// next run of catalog or release reads what lies there to find the last
// sequence number, and Check reads it to judge the directory.
func writeFileAtomic(path string, b []byte) error {
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
