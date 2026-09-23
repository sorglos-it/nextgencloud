// Command ngc-controller runs on every server: tunnel to the APIs, services
// in containers, updates.
package main

import (
	"os"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/node"
)

// Version is set when building.
var Version = "0.0.0-dev"

func main() { os.Exit(node.Main(Version, os.Args[1:], os.Stdout, os.Stderr)) }
