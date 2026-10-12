//go:build modules

// Package tools is not built into any program. It names the vendored modules
// that no part imports yet, so that go mod tidy keeps them in go.mod and
// go mod vendor keeps their code in vendor/. Every line moves to the part that
// uses it as soon as that part is written; the file disappears with the last
// line. Versions and licences: THIRD-PARTY.md.
package tools

import (
	// Neither MariaDB nor the WebSocket is named here any more: this merge
	// brought the two halves that import them together. apps/server-db
	// imports go-sql-driver/mysql (classes/mariadb) and the controller's
	// classes/link imports coder/websocket, so a line for either would be a
	// line for a module that IS imported - which is the one thing this file
	// must not hold.
	_ "github.com/go-webauthn/webauthn/webauthn" // passkeys
)
