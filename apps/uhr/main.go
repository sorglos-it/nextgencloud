// Command ngc-uhr is the service of the app org.sorglos.uhr: it answers with
// the time of the server it runs on.
package main

import (
	"os"

	"github.com/sorglos-it/nextgencloud/apps/uhr/classes"
)

// Version is set when building.
var Version = "0.0.0-dev"

func main() { os.Exit(classes.Main(Version, os.Args[1:], os.Stdout, os.Stderr)) }
