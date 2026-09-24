// Channel 0 of a tunnel: which of its five messages may travel which way,
// which version of the contract the two ends speak, and the one rule that holds
// for everything this side puts on it - nothing goes out that the API would
// have to refuse.
//
// What comes in is checked in link.go, and by two lines there rather than one:
// takeMessage lets through the two types the API may send (wire.ControlAllowed:
// the hello and the goodbye), and after the dial Run lets only the goodbye
// through, because the hello comes once and before it. Anything else ends the
// link at once and without an answer. Measured with both of them taken out in
// turn: either one alone still ends the link over a status of the API, and only
// with both gone does it get through.
//
// What goes out is checked here, in checkBody, which every control message of
// this side goes through - the hello of the dial included.

package link

import (
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The German lines an admin reads when the two ends are more than one version
// of the contract apart. Neither of them quotes anything that came in: the
// version of the API went through tunnel.ParseHello, but no log line of this
// package repeats a value of the tunnel, and that holds here too (see the
// package comment). Which of the two lines is written does follow from that
// version - a single bit, and the one the admin needs, because it says which of
// the two ends has to be brought up.
const (
	msgAPINewer = "Die API spricht eine neuere Fassung des Vertrags, als dieser Controller versteht. Bitte diesen Server aktualisieren."
	msgAPIOlder = "Die API spricht eine ältere Fassung des Vertrags, als dieser Controller versteht. Bitte die API aktualisieren."
)

// speak says which version of the contract the two ends talk and whether they
// can talk at all: the newer side speaks the version of the older one, and more
// than one version apart there is nothing left in common. Every part of NGC
// understands the version before its own and no further back than that.
//
// theirs comes from a hello and is therefore at least 1 already
// (tunnel.ParseHello); it is asked again so that this holds of every way into
// this function, and not only of the one that runs today.
func speak(ours, theirs int) (int, bool) {
	if theirs < 1 || theirs > ours+1 || theirs < ours-1 {
		return 0, false
	}
	return min(ours, theirs), true
}

// Speaks is the version of the contract this tunnel talks, the older of the two
// ends. It is settled at the dial, before the tunnel is handed out, and stands
// for the whole life of it.
func (t *Tunnel) Speaks() int { return t.speaks }

// agree settles that version once the hello of the API is in, and ends the dial
// when the two ends are too far apart: there is nothing they could say to each
// other that the other one would take.
func (t *Tunnel) agree(peer tunnel.Hello) error {
	speaks, ok := speak(tunnel.Contract, peer.Contract)
	if !ok {
		msg := msgAPIOlder
		if peer.Contract > tunnel.Contract {
			msg = msgAPINewer
		}
		// The address is out of this controller's own configuration and the
		// version is this build's own: neither came in over the tunnel.
		t.cfg.logger().Error(msg, "api", t.cfg.API.URL, "contract", tunnel.Contract)
		return ErrLink
	}
	t.speaks = speaks
	return nil
}

// checkBody runs the body of a control message through the parser of its
// contract - the very one the API runs on it - and refuses what fails. A
// message the other end has to refuse costs the tunnel, and a controller has
// nothing to gain from finding that out on the wire.
//
// b is the body as it will travel, nil where there is none. Of the five types a
// controller may send, four carry one and the goodbye carries none: a body on a
// goodbye is nothing this contract knows, so it is refused rather than sent and
// left to the other end to make something of.
//
// A type that is none of the five is refused as well. Control turns those away
// before this (wire.ControlAllowed), so the only way here is the dial's own
// hello - and that one has a type this switch knows.
func checkBody(typ string, b []byte) error {
	var err error
	switch typ {
	case wire.CtlHello:
		_, err = tunnel.ParseHello(b)
	case wire.CtlStatus:
		_, err = tunnel.ParseStatus(b)
	case wire.CtlUpdate:
		_, err = store.ParseUpdateReport(b)
	case wire.CtlNotify:
		_, err = core.ParseNotice(b)
	case wire.CtlBye:
		if len(b) != 0 {
			return ErrConfig
		}
		return nil
	default:
		return ErrConfig
	}
	// The error of the parser stays here. What it says is about a value of this
	// controller and not about the tunnel, but the callers of this package know
	// three errors and no more, and ErrConfig is the one for a value this
	// controller cannot send with.
	if err != nil {
		return ErrConfig
	}
	return nil
}
