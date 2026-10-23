// Why an app channel ended. An app channel that does not end with ok is what
// the person in front of the browser sees as "Die Verbindung wurde
// abgeschnitten." - one sentence, and on purpose: which rule of which station
// broke is nothing anybody in a browser can act on
// (apps/web/assets/apps/terminal/app.js).
//
// THE ADMINISTRATOR OF THE SERVER IS A DIFFERENT PERSON, and until this file
// existed they got nothing at all: measured on 21.10.2026 on the Debian
// machine, a terminal that was cut left NO line in the journal of the
// controller and none in the journal of the API, so the only thing anybody
// could work with was the sentence in the browser. Every station below now
// leaves exactly one line.
//
// WHERE THE LINE IS WRITTEN, AND WHERE IT IS NOT. Only a channel that got
// PAST THE FIRST INNER MESSAGE of the shell leaves one, and that is the line
// the doc of this package draws rather than one moved here: which channels
// arrive is the API's to choose, so a line per OPEN would be a peer's to write
// (Server.handshakes, Server.appRefused). A shell past its first inner message
// has proved it holds the keys of a channel access let through for an
// installed app, and every station behind that point is a fault of THIS
// machine: a manifest that names no runnable service, a runtime directory that
// is not there, a container that will not start, a result too large, an end
// that did not get out, a service that failed. Those are an operator's to act
// on, which is the reason the doc gives for writing anything at all. What
// stays counted and unwritten is everything before it: the bound of the server
// (E_LIMIT), a handshake that does not come off, and a first message that
// never arrived or did not decrypt.
//
// TWO BOUNDS, THE SAME ONES appstore/sources.go holds for the reasons it shows
// an administrator:
//
//   - The reasons are a CLOSED LIST. Nothing of an app, of a manifest, of a
//     store or of a peer is built into one, so no text somebody else wrote can
//     end up in a line an administrator reads as the server's own words.
//     TestEveryReasonIsFromTheClosedList holds it.
//   - NOTHING SECRET goes into a line. What is written is the app id - which
//     has been through wire.ValidAppID before it reaches here - the code of
//     the contract, and one reason out of the list. Not the service, not the
//     action, not a parameter, not the output of the program: a service of an
//     app names itself in the manifest, and a manifest is somebody else's
//     text.
package serve

import (
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/link"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The reasons an app channel ended, in German, for the journal of this
// controller. They are a closed list (see the head of this file).
const (
	ReasonPlan      = "Die App nennt für diese Aktion keinen ausführbaren Dienst."
	ReasonRunDir    = "Das Laufzeitverzeichnis des Containers konnte nicht vorbereitet werden."
	ReasonContainer = "Der Container der App läuft nicht und konnte nicht gestartet werden."
	ReasonTooLarge  = "Der Dienst hat mehr geantwortet, als eine Antwort tragen darf."
	ReasonCut       = "Die Verbindung brach ab, bevor das Ende des Dienstes hinausging."
	ReasonService   = "Der Dienst der App endete mit einem Fehler."
	ReasonInternal  = "Die Verbindung konnte serverseitig nicht geführt werden."
)

// reasons is the whole list, so a case can show that nothing else ever reaches
// an administrator (TestEveryReasonIsFromTheClosedList).
var reasons = []string{ReasonPlan, ReasonRunDir, ReasonContainer, ReasonTooLarge, ReasonCut,
	ReasonService, ReasonInternal}

// cutApp ends an app channel that did not end ok and leaves ONE line in the
// journal saying why. The line is written before the CLOSE goes out, so a
// channel whose CLOSE is the last thing that happens before the tunnel goes
// has its reason written down all the same.
func (s *Server) cutApp(ch *link.Channel, app, reason string, cl wire.Close) error {
	s.sayCut(app, reason, cl)
	return s.end(ch, cl)
}

// sayCut writes the one line. It is its own function because two callers have
// no channel to end any more: answer has already sent the end of the service
// inside the session, and the CLOSE behind it carries ok.
func (s *Server) sayCut(app, reason string, cl wire.Close) {
	s.log().Warn("Die Verbindung einer App wurde abgebrochen.",
		"app", app, "code", string(cl.Code), "grund", reason)
}
