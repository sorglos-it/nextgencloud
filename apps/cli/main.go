// Command ngc-store packs apps, encrypts paid apps and signs catalogs and
// licenses. With it anyone can run their own store.
package main

import (
	"flag"
	"fmt"
	"os"
)

// Version is set when building.
var Version = "0.0.0-dev"

const usage = `ngc-store packs apps and signs stores.

Usage:
  ngc-store pack <directory> <package.ngc>       build an app package
  ngc-store encrypt <package.ngc> <key file>     encrypt a paid app
  ngc-store catalog <directory> <key file>       write and sign a catalog
  ngc-store license <app> <installation> <key>   issue a license for a test store
`

func main() {
	version := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()
	if *version {
		fmt.Printf("ngc-store %s\n", Version)
		return
	}
	fmt.Fprint(os.Stderr, usage)
	fmt.Fprintln(os.Stderr, "\nngc-store is not built yet, see apps/cli/CLAUDE.md")
	os.Exit(1)
}
