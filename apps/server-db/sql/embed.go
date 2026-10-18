// Package migrations carries the numbered migrations of NGC. They travel
// inside the release file ngc-api, because they belong to its version: one
// controller with role db runs them before the new API starts.
//
// The folder is called sql because the project structure puts migrations
// there; the package is called migrations so that nothing confuses it with
// database/sql. go:embed cannot reach outside its own folder, which is why
// this file lives here and not next to the runner.
package migrations

import "embed"

// Files holds every migration, named <number>-<name>.sql.
//
//go:embed *.sql
var Files embed.FS
