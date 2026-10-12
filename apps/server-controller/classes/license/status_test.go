// The one case of this directory that needs the other side of the seam: what
// Allowed answers has to reach the API as a word of the contract, and the part
// that turns it into one is node.Reporter.
//
// It is an EXTERNAL test package, and that is the whole reason this file
// exists beside license_test.go: package node wires the controller together
// and therefore calls license.Allowed itself (node/run.go), so an internal
// test file that imported node would close a cycle. An external test package
// may import a package that imports the one under test, and nothing of the
// built packages changes for it.
package license_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/license"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/node"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// An app whose licence ran out is unlicensed in the status, not stopped.
func TestAnExpiredLicenseMakesTheAppUnlicensed(t *testing.T) {
	ctx := context.Background()
	st, _, allowed := license.ExpiredStand(t)
	r := node.NewReporter(node.ReportConfig{State: st, Log: slog.New(slog.DiscardHandler), Licensed: allowed})
	s, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Apps) != 1 || s.Apps[0].State != tunnel.StateUnlicensed {
		t.Fatalf("the status says %+v, want one app %q", s.Apps, tunnel.StateUnlicensed)
	}
}
