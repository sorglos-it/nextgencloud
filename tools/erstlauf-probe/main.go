// erstlauf-probe runs the two stations of the Erstlauf that no grep can answer
// and that need no machine: whether the right "use" of a freshly set up app can
// be handed to somebody, and whether what a service writes to STDOUT really
// reaches the channel while its STDERR does not.
//
// WHY A PROGRAM AND NOT A grep, AND NOT A go test. The two obstacles of
// 23.10.2026 (docs/erstlauf.md, stations 9 and 10) are both about what the code
// DOES, and both were green in every text search while the installation stood:
//
//   - The rights page offered the right "use" only for apps one could already
//     see, and one could see an app only with that right. A grep for "use"
//     finds the word in a dozen places and says nothing about the circle.
//   - The output of the shell did not arrive in the browser: "muh" answered
//     "/bin/sh: 2: muh: not found" and "ls" answered NOTHING, while the same
//     "ls" in the container answered a listing. One of the two streams
//     travelled and the other did not, which is why both are measured here,
//     separately and never together: a check that reads "something arrived"
//     would have been green on exactly that fault.
//
// It is not a go test because tools/check.sh is a different question (see the
// head of tools/erstlauf.sh): the Erstlauf has to say what it measured along a
// way, and a station that exists only as a test case cannot be run from the
// run. The cases under apps/ stay where they are; this program asks the same
// packages the servers ask, through their exported doors, and prints one line
// per check.
//
// WHAT IT PRINTS, and tools/erstlauf.sh counts the lines rather than the exit
// code (messen.md: a run whose lines were lost must not become a green total):
//
//	check <id>: ok|FAIL|UNMEASURABLE -- <sentence>
//
// and on a fault of this program itself one line "broken: <sentence>" with exit
// 3 - a preparation that did not take leaves the run measuring nothing, and
// that is never a finding about NGC.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/rights"
	ctlruntime "github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/serve"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/service"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The ids of the checks. tools/erstlauf.sh holds this list as well and stops
// when one of them did not print - so a check dropped here cannot make the run
// quietly smaller.
const (
	idOffered = "recht-angeboten" // the right of a fresh app is in the offer at all
	idValid   = "recht-gueltig"   // and the contract lets it out
	idGranted = "recht-vergeben"  // and a plain user really holds it afterwards
	idStdout  = "ausgabe-stdout"  // what the service writes to stdout reaches the channel
	idStderr  = "ausgabe-stderr"  // and what it writes to stderr does not
)

var fails int

func pass(id, why string) { fmt.Printf("check %s: ok -- %s\n", id, why) }

func fail(id, why string) {
	fmt.Printf("check %s: FAIL -- %s\n", id, why)
	fails++
}

func unmeasurable(id, why string) { fmt.Printf("check %s: UNMEASURABLE -- %s\n", id, why) }

// broken ends the program: this is a fault of the probe, not a finding.
func broken(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "broken: "+format+"\n", a...)
	os.Exit(3)
}

func main() {
	ctx := context.Background()
	rechte(ctx)
	ausgabe(ctx)
	if fails != 0 {
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// station 9  Recht "benutzen" vergeben
//
// The obstacle, in one sentence: a freshly set up app stood installed, and the
// page that hands rights out said "Es gibt noch keine App, für die es Rechte
// gäbe." - because its list of apps came from the desktop's menu, and the menu
// shows an app only to somebody who already holds <app>/use. The first right of
// a new app could therefore never be given to anybody.
//
// The fix is rights.adminApps: an admin holds the right of every installed app
// WITHOUT a row of its own, so the menu shows it to an admin, so the page has
// it in its list, so it can be ticked for a role. That is one function, and
// that is what is asked of it here - not that the word stands somewhere.
//
// WHAT IS MEASURED HERE AND WHAT IS NOT. Measured: rights.Of, rights.All,
// rights.Set and core.ValidRight, the doors the API itself goes through. NOT
// measured in this program: that the menu's filter and the page's list still
// read what this depends on. That is not left to belief either -
// tools/erstlauf.sh checks both places before it runs this program and refuses
// to measure when they moved (its station 9 preparation), because a probe that
// restated the filter itself would be proving its own copy.
// ---------------------------------------------------------------------------
const (
	freshApp  = "de.example.frisch"
	rolePlain = "buero"
	manifest  = `{"id":"` + freshApp + `","name":"Frisch","version":"1.0.0","publisher":"Sorglos",` +
		`"actions":[{"name":"teilen","title":"Teilen","service":"frisch"}],` +
		`"services":[{"name":"frisch","command":["/usr/bin/frisch"]}]}`
)

// The two users as the trust list carries them. A trust ID is 16 bytes in
// base64 and nothing else (trust.ValidID), so they stand here as such and are
// held against that rule below rather than invented from a name.
const (
	adminID = "AAAAAAAAAAAAAAAAAAAAAA"
	plainID = "AQEBAQEBAQEBAQEBAQEBAQ"
)

func rechte(ctx context.Context) {
	db := memstore.New()
	for _, id := range []string{adminID, plainID} {
		if !trust.ValidID(id) {
			broken("%q is no trust ID, so this probe built a list the API would never hold", id)
		}
	}
	list := &trust.List{
		Seq: 1, Installation: "erstlauf",
		Users: []trust.User{
			{ID: adminID, Name: "chef", Roles: []string{trust.RoleAdmin}},
			{ID: plainID, Name: "buero", Roles: []string{rolePlain}},
		},
		Roles: []trust.Role{{Name: trust.RoleAdmin}, {Name: rolePlain}},
	}
	// The app as an activation leaves it: active, with a version and with its
	// manifest, and with NO rights row anywhere. That last part is the whole
	// case - a row would be the very thing that is missing in the field.
	if err := db.AppPut(ctx, dbapi.App{ID: freshApp, Version: "1.0.0", Store: "store",
		Node: "ctl", Active: true, Manifest: []byte(manifest)}); err != nil {
		broken("the app row could not be written into the memory store: %v", err)
	}
	all, err := rights.All(ctx, db, list)
	if err != nil {
		broken("rights.All did not answer for a list of two roles: %v", err)
	}
	if len(all) != 2 {
		broken("rights.All answered for %d roles where the list has 2 - this probe is asking the wrong thing", len(all))
	}
	for _, r := range all {
		if len(r.Rights) != 0 {
			broken("role %q already holds %d rights in a store this probe has just made - the fixture is not the fresh case", r.Role, len(r.Rights))
		}
	}

	use := freshApp + "/" + core.RightUse

	// 1. The offer. These are the rights the page's own list is built from: an
	// admin's, which is where the circle was closed.
	adminRights, admin, err := rights.Of(ctx, db, list, adminID)
	if err != nil {
		broken("rights.Of did not answer for the admin: %v", err)
	}
	if !admin {
		broken("rights.Of does not take the user with role %q for an admin - the fixture is wrong, not NGC", trust.RoleAdmin)
	}
	if adminRights[use] {
		pass(idOffered, "a freshly set up app with no rights row anywhere is in the admin's own rights ("+use+
			"), so it stands in the admin's menu, the rights page has it in its list and the right can be ticked for a role")
	} else {
		fail(idOffered, "the app "+freshApp+" is installed and active, and the admin does NOT hold "+use+
			" - so it is in nobody's menu, the rights page builds its list from that menu, and the right is offered for no app at all;"+
			" the first right of a freshly set up app can then never be given to anybody (rights.Of, adminApps)")
	}

	// 2. The contract. The page runs every right of its list through
	// core.validRight before it draws a box, so a right the contract refuses
	// is a right nobody is offered, however complete the list above was.
	if core.ValidRight(use) {
		pass(idValid, "core.ValidRight takes "+use+", so the page may draw a box for it and the API may be asked to store it")
	} else {
		fail(idValid, "core.ValidRight refuses "+use+", so the rights page drops it out of its own list and no box is ever drawn for it")
	}

	// 3. The handing over. A right that is offered and cannot be given is the
	// same dead end one step on, so the row is written the way the API writes
	// it and the plain user is asked again.
	if err := rights.Set(ctx, db, core.RoleRights{Role: rolePlain, Rights: []string{use}}); err != nil {
		fail(idGranted, "rights.Set refused to store "+use+" for the role "+rolePlain+": "+err.Error()+
			" - then the page can offer the right and still not hand it over")
		return
	}
	plainRights, plainAdmin, err := rights.Of(ctx, db, list, plainID)
	if err != nil {
		broken("rights.Of did not answer for the plain user: %v", err)
	}
	if plainAdmin {
		broken("rights.Of takes the user with role %q for an admin - then this check measures an admin again and not a plain user", rolePlain)
	}
	if plainRights[use] {
		pass(idGranted, "the right "+use+" was stored for the role "+rolePlain+
			" and the user of that role holds it afterwards, so the app reaches a second person's desktop")
	} else {
		fail(idGranted, "the right "+use+" was stored for the role "+rolePlain+
			" and the user of that role does NOT hold it - the app stays out of their menu")
	}
}

// ---------------------------------------------------------------------------
// station 10  Die App ist wirklich bedienbar
//
// The obstacle, measured on 23.10.2026: the terminal app opened, the window
// stood there, and the shell's answers did not arrive. "muh" came back as
// "/bin/sh: 2: muh: not found" and "ls" came back as NOTHING, while the very
// same "ls" in the container printed a listing. So an open window was no
// measure of anything: ONE of the two streams travelled.
//
// THEREFORE BOTH STREAMS, SEPARATELY. The seam they part at is
// runtime.Exec{Stdout, Stderr} in serve.RunService: stdout is the channel the
// person looks at, stderr is the controller's journal line and must NOT be on
// that channel - the output of an app may carry what the person typed
// (serve/call.go, LogStderr). A check that only asked "did anything arrive"
// would have been green on the fault that happened, and a check that only
// asked for stdout would stay green if the two were ever swapped. Hence two.
//
// THE FIXTURE, NAMED. The engine is this program's own: podman is what runs the
// container on a machine, and nothing of podman decides where the two streams
// go - RunService does, in those two fields. What is therefore NOT measured
// here is the podman engine's own wiring of them (runtime.Podman.Exec); that
// needs a container, and the machine phase says so at its own station.
// ---------------------------------------------------------------------------

// The two markers. Different on purpose, and neither can get into the other
// stream by accident.
const (
	outMark = "ERSTLAUF-STDOUT-5f3a9c"
	errMark = "ERSTLAUF-STDERR-7b1e42"
)

// probeEngine is a runtime.Engine that runs no container. Exec writes the two
// markers into the two streams it was handed and ends with 0 - so what is
// measured afterwards is where RunService put those streams and nothing else.
type probeEngine struct{ wrote error }

func (e *probeEngine) LoadImage(context.Context, string, string) (string, error) {
	return "", errors.New("probe engine loads no image")
}
func (e *probeEngine) RemoveImage(context.Context, string, string) error      { return nil }
func (e *probeEngine) Create(context.Context, ctlruntime.Container) error     { return nil }
func (e *probeEngine) Start(context.Context, string, ctlruntime.Limits) error { return nil }
func (e *probeEngine) Stop(context.Context, string) error                     { return nil }
func (e *probeEngine) Remove(context.Context, string) error                   { return nil }
func (e *probeEngine) Running(context.Context, string) (bool, error)          { return true, nil }

func (e *probeEngine) Exec(_ context.Context, x ctlruntime.Exec) (int, error) {
	// A probe that could not write at all would make both checks below red for
	// a reason that is this program's own, so the error is kept and the caller
	// turns it into "broken" instead of into a finding.
	if x.Stdout == nil || x.Stderr == nil {
		e.wrote = fmt.Errorf("RunService handed the engine Stdout=%v Stderr=%v",
			x.Stdout != nil, x.Stderr != nil)
		return 1, nil
	}
	if _, err := io.WriteString(x.Stdout, outMark+"\n"); err != nil {
		e.wrote = fmt.Errorf("writing to Stdout: %w", err)
	}
	if _, err := io.WriteString(x.Stderr, errMark+"\n"); err != nil {
		e.wrote = fmt.Errorf("writing to Stderr: %w", err)
	}
	return 0, nil
}

func ausgabe(ctx context.Context) {
	// The one thing RunService needs of the machine: the directory it writes
	// the environment of a call into, as this process. On a developer's
	// machine that path does not exist, and making it is preparation and not a
	// finding - it is said here rather than hidden, and where it cannot be
	// made the two checks say so instead of going green or quiet.
	if err := os.MkdirAll(config.RunDir, 0o700); err != nil {
		unmeasurable(idStdout, "the run directory "+config.RunDir+" could not be made on "+runtime.GOOS+
			" ("+err.Error()+"), and serve.RunService writes the environment of every call there")
		unmeasurable(idStderr, "the same reason as "+idStdout+": no call can be run here")
		return
	}

	eng := &probeEngine{}
	var out bytes.Buffer
	// The journal line of the call, with the text of the stderr in it: that is
	// LogStderr, and it is switched ON here on purpose. Without it the line
	// says only how many bytes there were, and then this probe could not tell
	// "the stderr went into the journal" from "the stderr went nowhere".
	var journal bytes.Buffer
	log := slog.New(slog.NewTextHandler(&journal, nil))

	end := serve.RunService(ctx, serve.CallInput{
		Engine: eng, Container: "frisch", Command: []string{"/usr/bin/frisch"},
		Call: service.Call{User: plainID, UserName: "buero", App: freshApp,
			Service: "frisch", Action: "teilen", Version: "1.0.0"},
		Out: &out, Log: log, LogStderr: true,
	})
	if eng.wrote != nil {
		broken("the probe's own engine could not write what this station measures: %v", eng.wrote)
	}
	if end.Status != wire.StatusOK {
		broken("serve.RunService ended a call of this probe with status %q - then nothing below is about the two streams", end.Status)
	}

	got := out.String()
	switch {
	case strings.Contains(got, outMark) && !strings.Contains(got, errMark):
		pass(idStdout, "what the service wrote to STDOUT arrived on the channel the person looks at, byte for byte ("+outMark+
			") -- measured through serve.RunService with this probe's own engine in place of podman")
	case !strings.Contains(got, outMark) && strings.Contains(got, errMark):
		fail(idStdout, "the channel carried the STDERR of the service and NOT its stdout - that is the fault of 23.10.2026 exactly:"+
			" an error message arrives, a listing does not, and the app looks alive while it answers nothing")
	case !strings.Contains(got, outMark):
		fail(idStdout, fmt.Sprintf("nothing of what the service wrote to STDOUT arrived on the channel (it carried %d bytes)"+
			" - the window opens and the app answers nothing", out.Len()))
	default:
		fail(idStdout, "the channel carried the stdout of the service AND its stderr - the journal's stream is then on the person's"+
			" screen, and the output of an app may hold what they typed (serve/call.go, LogStderr)")
	}

	// The other stream, on its own. It must not be on the channel, and it must
	// not be lost either: the journal line is where an operator reads why an
	// app is quiet, and a stderr that goes nowhere is the silent half of this
	// station.
	switch {
	case strings.Contains(got, errMark):
		fail(idStderr, "the STDERR of the service arrived on the person's channel ("+errMark+
			") - it belongs in the journal of the controller, because it may hold what the person typed")
	case strings.Contains(journal.String(), errMark):
		pass(idStderr, "the STDERR of the service stayed off the channel and reached the journal line of the call instead,"+
			" so an operator can read why an app is quiet")
	default:
		fail(idStderr, "the STDERR of the service reached neither the channel nor the journal line of the call,"+
			" so an app that only complains on stderr is silent everywhere")
	}
}
