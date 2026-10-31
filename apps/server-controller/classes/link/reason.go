package link

import "errors"

// Reason is WHICH clause of the tunnel contract the API broke.
//
// Why it exists, and it is a measurement and not a preference: ErrLink is
// thrown at thirteen places in this package, and until now all thirteen
// reached the journal as the same one sentence. Measured on the distributed
// plant on 24.10.2026: svc-1 wrote "tunnel failed ... err=link: the API broke
// the contract of the tunnel" and nothing else, and from that line it was not
// decidable which of the thirteen had fired - a wrong subprotocol, a frame
// that would not decrypt, a control message of a type that is not allowed, or
// one channel too many. A fault nobody can place is a fault nobody can
// reproduce, and three of this week's belonged to that kind.
//
// WHO READS IT: the journal of this server, and nobody else.
//
//   - It does NOT go out on the tunnel. The head of link.go says what that
//     costs: "none of them says which step failed to whoever is at the other
//     end". A peer that learns which clause it tripped learns where the
//     grammar of this side is thin, and it learns it for free. So Error()
//     below is UNCHANGED - a Reason reaches a caller only by asking for it
//     (ReasonOf), never by printing the error.
//   - It is NOT a line for an administrator either. What an administrator
//     reads stays narrower than what the journal may hold: these are names of
//     places in a contract, not sentences about what to do, and the screen of
//     an administrator gets the sentence it got before.
//
// Every value is a fixed word of THIS file. Nothing that came in over the
// tunnel is ever put in one, so no text of a peer's can ride in on this
// (the same closed-list rule as appstore.reasons). The words carry no value
// either: not a length, not a channel number, not a type that arrived - a
// number from the wire is a number a stranger chose.
type Reason string

// The whole list. A reader of the journal sees exactly one of these words.
const (
	// At the dial, before any Noise: the server does not speak the
	// subprotocol of the tunnel.
	ReasonSubprotocol Reason = "subprotocol"
	// Message 2 of the handshake is not a binary message of the right size.
	ReasonHandshake Reason = "handshake-message"
	// Message 2 is of the right shape and the Noise read of it failed, or it
	// carried a payload where this contract has none.
	ReasonNoise Reason = "handshake-noise"
	// The first transport message is not the hello it has to be.
	ReasonHello Reason = "hello"
	// It is a hello and its body does not parse.
	ReasonHelloBody Reason = "hello-body"
	// A control message on channel 0 that is not the goodbye - the only type
	// left there once the hello is in.
	ReasonBye Reason = "control-after-hello"
	// A websocket message that is not binary.
	ReasonNotBinary Reason = "not-binary"
	// A binary message that would not decrypt. This is the one that would be
	// load-dependent if anything here is, so it is worth its own word.
	ReasonDecrypt Reason = "decrypt"
	// A decrypted message that is not exactly one frame.
	ReasonFrame Reason = "frame"
	// A frame on channel 0 that is not DATA.
	ReasonControlType Reason = "control-type"
	// A control message whose body does not parse, or whose type the API may
	// not send on a tunnel.
	ReasonControlBody Reason = "control-body"
	// One channel number more than wire.MaxTunnelChannels allows.
	ReasonChannels Reason = "too-many-channels"
	// The two ends are too far apart in the contract to say anything to each
	// other. This one also writes its own line of its own (see agree).
	ReasonContract Reason = "contract-version"
)

// reasons is the whole list, so a test can show that nothing else ever reaches
// the journal (TestEveryReasonIsFromTheClosedList).
var reasons = []Reason{ReasonSubprotocol, ReasonHandshake, ReasonNoise, ReasonHello,
	ReasonHelloBody, ReasonBye, ReasonNotBinary, ReasonDecrypt, ReasonFrame,
	ReasonControlType, ReasonControlBody, ReasonChannels, ReasonContract}

// linkError is ErrLink with the place it came from. It is deliberately NOT a
// wrapper of the %w kind: Error() answers exactly what ErrLink answers, so
// every existing path that prints or compares this error is untouched and the
// reason cannot ride out on one of them. The way to it is ReasonOf.
type linkError struct{ r Reason }

// Error is ErrLink's own text and nothing more. See the type comment: a
// printed error must not carry the place.
func (e linkError) Error() string { return ErrLink.Error() }

// Is makes errors.Is(err, ErrLink) true for every one of these, which is what
// the whole package and its callers already test for (link.go:494 among them).
func (e linkError) Is(target error) bool { return target == ErrLink }

// broke is how this package throws ErrLink from here on: with its place.
func broke(r Reason) error { return linkError{r} }

// ReasonOf answers which clause was broken, or the empty Reason when err is
// not a broken contract of this package - including a plain ErrLink from
// somewhere that names no place, which is not an error but a reason that is
// simply not known.
func ReasonOf(err error) Reason {
	var e linkError
	if errors.As(err, &e) {
		return e.r
	}
	return ""
}
