// Command ngc-controller runs on every server: tunnel to the APIs, services
// in containers, updates.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/sorglos-it/nextgencloud/contracts/config"
	"github.com/sorglos-it/nextgencloud/contracts/tunnel"
)

// Version is set when building.
var Version = "0.0.0-dev"

func main() {
	version := flag.Bool("version", false, "print the version and exit")
	path := flag.String("config", config.ControllerFile, "configuration file")
	flag.Parse()
	if *version {
		fmt.Printf("ngc-controller %s (contract %d)\n", Version, tunnel.Contract)
		return
	}
	cfg, err := config.LoadController(*path)
	if err != nil {
		slog.Error("cannot read the configuration", "file", *path, "error", err)
		os.Exit(1)
	}
	slog.Info("ngc-controller starting", "version", Version, "name", cfg.Name, "parts", cfg.Parts)
	slog.Error("the controller is not built yet, see controller/CLAUDE.md")
	os.Exit(1)
}
