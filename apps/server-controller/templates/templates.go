// Package templates carries the configuration files this controller writes
// for other programs. It is a package of its own because go:embed reads only
// files beside the Go file that embeds them, so classes/apache cannot reach
// templates/ by itself; keeping one copy here is worth the extra package,
// because a second copy beside the code would drift away from this one.
package templates

import _ "embed"

// ApacheAPIs is the balancer over the API servers (config.BalancerFile). The
// web interface writes the same file at install time; apache.Render fills
// this template, so both spellings stay one.
//
//go:embed apache-apis.conf.tmpl
var ApacheAPIs string

// TerminalManifest is manifest.json of the terminal app. It is embedded and
// not read off the disk on purpose: embedded, it is part of the build graph of
// whoever parses it, so a case that checks it against the contract checks the
// file that is shipped - and a mutation of that file is seen. A case that
// opened the path instead would read past any overlay.
//
//go:embed terminal/manifest.json
var TerminalManifest []byte
