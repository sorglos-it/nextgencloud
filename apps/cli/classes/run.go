package classes

import (
	"encoding/hex"
	"fmt"
	"io"
)

// Usage is what ngc-store prints for a wrong call. Messages of ngc-store are
// German: they are read by whoever runs their own store.
const Usage = `ngc-store packt Apps und unterschreibt Stores.

Aufrufe:
  ngc-store pack <Verzeichnis> <Paket.ngc>        Paket einer App schreiben
  ngc-store encrypt <Paket.ngc> <Schlüsseldatei>  Kauf-App verschlüsseln
  ngc-store catalog <Verzeichnis> <Schlüsseldatei>  Katalog schreiben und unterschreiben
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
	case "encrypt", "catalog", "license", "release", "check":
		// Tasks 27 to 29 of the API plan fill these in; until then the name is
		// known but does nothing.
		fmt.Fprintf(stderr, "ngc-store %s ist noch nicht gebaut\n", args[0])
		return 1
	}
	fmt.Fprint(stderr, Usage)
	return 2
}
