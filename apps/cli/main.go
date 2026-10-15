// Command ngc-store packs apps, encrypts paid apps and signs catalogs and
// licenses. With it anyone can run their own store.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sorglos-it/nextgencloud/apps/cli/classes"
)

// Version is set when building.
var Version = "0.0.0-dev"

func main() {
	version := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() { fmt.Fprint(os.Stderr, classes.Usage) }
	flag.Parse()
	if *version {
		fmt.Printf("ngc-store %s\n", Version)
		return
	}
	os.Exit(classes.Run(flag.Args(), os.Stdout, os.Stderr))
}
