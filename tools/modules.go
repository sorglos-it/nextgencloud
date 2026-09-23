//go:build modules

// Package tools is not built into any program. It names the vendored modules
// that no part imports yet, so that go mod tidy keeps them in go.mod and
// go mod vendor keeps their code in vendor/. Every line moves to the part that
// uses it as soon as that part is written; the file disappears with the last
// line. Versions and licences: THIRD-PARTY.md.
package tools

import (
	_ "github.com/coder/websocket"               // sessions and tunnel
	_ "github.com/go-webauthn/webauthn/webauthn" // passkeys
	_ "github.com/ncruces/go-sqlite3/driver"     // SQLite without cgo
	_ "github.com/ncruces/go-sqlite3/embed"      // the SQLite build the driver runs
	_ "github.com/ncruces/go-sqlite3/vfs/xts"    // AES-256-XTS on the database file
	_ "github.com/pquerna/otp/totp"              // TOTP
)
