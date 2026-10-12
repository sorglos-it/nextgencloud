package classes

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

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
  ngc-store license <App> <Installation> <Schlüsseldatei>  Lizenz für einen Test-Store
  ngc-store release <Verzeichnis> <Schlüsseldatei>  Release zum Testen unterschreiben
  ngc-store check <Verzeichnis> <Teil> <Architektur>  Release prüfen wie verify.sh
`

// Run carries out one call of ngc-store and returns the exit status: 0 for
// done, 1 for a call that failed, 2 for a call Run does not understand.
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
	case "encrypt", "license", "release", "check":
		// Tasks 27 to 29 of the API plan fill these in; until then the name is
		// known but does nothing.
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
