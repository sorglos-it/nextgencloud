// Package reason holds the German sentences the API says about a refusal, as a
// CLOSED list. A wire.Code alone is no reason: "E_INTERNAL" tells an operator
// nothing, and the line has to say WHICH check said no.
//
// Measured on 19.-21.10.2026 on the Debian machine during the first real
// installation: three refusals in a row, and neither ngc-api nor
// ngc-controller wrote a single line about any of them. One of the three was a
// user with no root grant on that controller -- a decision this package can
// now name (NotRoot).
//
// Why the list is closed, and not the error of whoever said no: an
// administrator reads these sentences in the log of "Benutzer & Rechte", and a
// text a stranger can fill is a tool against that reader -- a line break in it
// forges a second entry, and a log is the easiest place in the system to lose
// a secret. The same argument stands in appstore/sources.go for the reasons of
// a store, and this package follows it. So no error text, no path, no name a
// caller sent and no value of a key, a code or a password ever becomes a
// reason; every sentence here is written in this file and chosen by a switch.
//
// TWO READERS, TWO LISTS. An administrator reads the log, and an operator
// reads the screen of their own session -- and what may go on a screen is
// narrower than what may go into the log:
//
//   - The log gets every reason, AppUnknown among them.
//   - A screen gets a reason only when Screen says so. AppUnknown does not go
//     out: package rights answers an app that is not installed exactly like a
//     missing right on purpose, so that a user without the right learns
//     nothing about the installation. A sentence on the screen that told the
//     two apart would undo that in one line, and the log is where the
//     difference belongs -- its reader is an admin of this installation
//     already.
package reason

import (
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Reason is one sentence of the closed list below, or None.
type Reason string

// None is "nothing said". It is what a channel that ran carries, and the one
// value that is no sentence.
const None Reason = ""

// MaxReason bounds every sentence of the list. It is the bound of a detail of
// the log (audit.maxDetail), because that is the narrower of the two places a
// reason goes -- a message of a CLOSE may be wire.MaxMessageText long. The
// number is NOT checked against itself: audit's own case holds that every
// reason of this list survives its grammar unchanged
// (TestEveryReasonSurvivesAsADetail), so the two bounds cannot drift apart
// unnoticed.
const MaxReason = 100

// The reasons. Each one names the check that said no, in the words of this
// program -- never the words of a store, a controller, a database or a user.
const (
	// Of the installation itself.
	Trust    Reason = "Die Vertrauensliste konnte nicht gelesen werden."
	DB       Reason = "Die Datenbank hat nicht geantwortet."
	Result   Reason = "Das Ergebnis der Aktion war nicht kodierbar."
	Deliver  Reason = "Die Oberfläche der App konnte nicht ausgeliefert werden."
	OwnTask  Reason = "Der Anlage fehlt die Aufgabe für ihren eigenen Aufruf."
	OwnProof Reason = "Der Sitzungsnachweis in der Datenbank ist unbrauchbar."

	// Of the action and its parameters.
	ActionUnknown  Reason = "Diese Aktion gibt es in dieser Anlage nicht."
	ActionUnserved Reason = "Diese Aktion bedient dieser Server nicht."
	Params         Reason = "Die Angaben passen nicht zum Vertrag dieser Aktion."
	TooLarge       Reason = "Die gesendeten Daten überschreiten die Obergrenze der Aktion."

	// Of rights, roles and tasks.
	AdminOnly   Reason = "Diese Aktion ist Verwaltern vorbehalten."
	ShellClosed Reason = "Diese Aktion kann die Oberfläche nicht öffnen."
	NoRight     Reason = "Dem Benutzer fehlt das Recht für diese Aktion."
	AppUnknown  Reason = "Diese App ist auf dieser Anlage nicht installiert."
	NotRoot     Reason = "Dieser Benutzer darf auf diesem Rechner keine Root-Aufgaben ausführen."
	NoTask      Reason = "Für diese Aktion fehlt die unterschriebene Aufgabe."
	NoConfirm   Reason = "Die erneute Bestätigung fehlt oder gilt nicht."
	BadTask     Reason = "Die unterschriebene Aufgabe passt nicht zu diesem Kanal."
	TaskSpent   Reason = "Diese Aufgabe wurde schon verbraucht."
	WithParams  Reason = "Neben der Aufgabe kamen eigene Angaben; die Aufgabe entscheidet."

	// Of users, rows and versions.
	UserUnknown  Reason = "Dieser Benutzer steht nicht in der Vertrauensliste."
	RoleUnknown  Reason = "Diese Rolle steht nicht in der Vertrauensliste."
	RowGone      Reason = "Die Zeile zu dieser App oder Version fehlt in der Datenbank."
	ProofGone    Reason = "Der Nachweis zu dieser Oberfläche fehlt oder gilt nicht mehr."
	Factor       Reason = "Der zweite Faktor stimmt nicht."
	FactorBusy   Reason = "Es laufen zu viele Einrichtungen des zweiten Faktors."
	TrustRefused Reason = "Die neue Vertrauensliste wurde nicht angenommen."

	// Of a store, a licence or a controller.
	StoreAnswer Reason = "Der Store hat nicht oder nicht brauchbar geantwortet."
	Signature   Reason = "Paket, Katalog oder Unterschrift haben die Prüfung nicht bestanden."
	License     Reason = "Die Lizenz dieser App deckt diesen Schritt nicht."
	NodeAway    Reason = "Der Rechner für diese Aktion ist gerade nicht verbunden."
	NodeRefused Reason = "Der Rechner hat die Aufgabe abgelehnt."

	// The backstops. Each one names the layer, which is the least a line may
	// say; a refusal that gets one of these is a place this package has not
	// been taken to yet, and that is visible in the log as such.
	InsideAPI  Reason = "Eine Prüfung dieses Servers hat abgelehnt, ohne den Grund zu nennen."
	InsideNode Reason = "Der Rechner hat abgebrochen, ohne den Grund zu nennen."
)

// all is the whole list, so a case can show that no other sentence ever
// reaches a reader (TestTheListIsClosed).
var all = []Reason{Trust, DB, Result, Deliver, OwnTask, OwnProof,
	ActionUnknown, ActionUnserved, Params, TooLarge,
	AdminOnly, ShellClosed, NoRight, AppUnknown, NotRoot, NoTask, NoConfirm, BadTask,
	TaskSpent, WithParams,
	UserUnknown, RoleUnknown, RowGone, ProofGone, Factor, FactorBusy, TrustRefused,
	StoreAnswer, Signature, License, NodeAway, NodeRefused,
	InsideAPI, InsideNode}

// All returns the whole list. It is a copy, so no caller can bend the list a
// case measures.
func All() []Reason { return append([]Reason(nil), all...) }

// Known reports whether r is one of the list. None is not: it is the absence
// of a reason, and a caller that wants to allow it says so itself.
func Known(r Reason) bool {
	for _, k := range all {
		if k == r {
			return true
		}
	}
	return false
}

// offScreen are the reasons that go into the log and NEVER onto a screen. The
// list is written the other way round on purpose: a reason added later is on
// the screen unless somebody names it here, and the alternative -- a new
// reason that silently says nothing to the operator -- is the very hole this
// package was written for.
var offScreen = []Reason{AppUnknown}

// Screen reports whether r may be shown to the operator of a session. See the
// package comment for the one difference between the two readers.
func Screen(r Reason) bool {
	if !Known(r) {
		return false
	}
	for _, o := range offScreen {
		if o == r {
			return false
		}
	}
	return true
}

// ForTask names the check of package task that said no. It is the mapping the
// case of 19.10.2026 asked for: task.ErrForbidden is "no root permission on
// this server", and that sentence never reached anybody.
func ForTask(err error) Reason {
	switch {
	case err == nil:
		return None
	case errorIs(err, task.ErrForbidden):
		return NotRoot
	case errorIs(err, task.ErrNoTask):
		return NoTask
	case errorIs(err, task.ErrConfirm):
		return NoConfirm
	}
	// Everything else of package task is a task that does not fit this
	// channel: a wrong app, service, action or node, an expiry, a signature
	// that does not verify.
	return BadTask
}

// ForCode is the backstop: the least a line may say when no site named a
// reason. It names the layer the code comes from and says that the reason is
// missing, so a gap reads as a gap and not as an explanation.
func ForCode(code wire.Code) Reason {
	switch code {
	case "":
		return None
	case wire.CodeUnreachable, wire.CodeTimeout:
		return NodeAway
	}
	return InsideAPI
}

// errorIs compares as package rights does (codeOf there): the errors of
// package task are sentinel values returned as they stand, and this package
// adds no wrapping of its own.
func errorIs(err, target error) bool { return err == target }
