// Command ngc-api is the API of NGC: sessions, rights, forwarding.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// Version is set when building: -ldflags "-X main.Version=1.0.0".
var Version = "0.0.0-dev"

func main() {
	version := flag.Bool("version", false, "print the version and exit")
	path := flag.String("config", config.APIFile, "configuration file")
	flag.Parse()
	if *version {
		fmt.Printf("ngc-api %s (contract %d)\n", Version, tunnel.Contract)
		return
	}
	cfg, err := config.LoadAPI(*path)
	if err != nil {
		slog.Error("cannot read the configuration", "file", *path, "error", err)
		os.Exit(1)
	}
	slog.Info("ngc-api starting", "version", Version, "node", cfg.Node, "listen", cfg.Listen)
	slog.Error("the API is not built yet, see apps/server-api/CLAUDE.md")
	os.Exit(1)
}
