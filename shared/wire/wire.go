// Package wire defines frames and channels, the unit of transfer between
// browser, API and controller (contracts V1 and V2).
//
// Links. A session link joins a browser and an API (WebSocket
// session.PathSocket, subprotocol session.Subprotocol); a tunnel joins a
// controller and an API (tunnel.PathTunnel, tunnel.Subprotocol). The first
// binary WebSocket message is Noise message 1 of the initiator (browser,
// controller), the second is Noise message 2 of the API; both carry an empty
// payload and are exactly HandshakeSize bytes (package noisep). After that
// every binary WebSocket message is exactly one Noise transport message whose
// plaintext is exactly one frame. A text message, a frame that does not decode
// or a failed decryption ends the link. Noise_KK authenticates both ends of
// both links; the TLS they run in only carries them through firewalls and
// proxies, and is verified all the same, never switched off: the browser
// checks the web server's certificate, Apache and the controller check the
// API's certificate against its pin (trust.Node.TLS, tunnel.PinnedTLS).
//
// Channel 0 is always open and carries Control messages; ControlAllowed says
// who may send which. On a tunnel both sides send hello first.
//
// Channels. Only one side opens channels: the browser on a session link, the
// API on a tunnel. The opener picks a free number >= 1. A channel runs: OPEN
// (its Window is the credit the opener grants) -> the other side's first GRANT
// (credit at least MinWindow; on an end-to-end channel also Noise message 2)
// or a CLOSE with an error -> DATA both ways -> EOF from each side that has
// nothing more to send -> CLOSE from the side that accepted the channel.
// Either side may send CLOSE with an error at any time; after its CLOSE a side
// sends nothing more on the channel.
//
// Closing. A side answers every CLOSE with a CLOSE, unless it has sent one for
// that channel already. A number is free again only once a CLOSE for it was
// both sent and received, so that frames still under way can never reach a
// new channel with the same number (on a tunnel perhaps another user's).
// Frames for a channel that arrive after one's own CLOSE are dropped without
// an answer, but the other side's CLOSE among them still counts as received
// and frees the number. A protocol error on a number is a frame for a number
// that is neither open nor closing (never opened, or free again), an OPEN for
// a number that is still open, or an OPEN from the side that does not open
// channels. The receiver answers it with CLOSE E_PROTOCOL for that number,
// which ends the channel if there is one, and frees the number once the other
// side's CLOSE for it has arrived.
//
// Limits. A number counts toward the channel limit (MaxSessionChannels on a
// session link, MaxTunnelChannels on a tunnel) until it is free again, while
// it is closing too. A frame that would exceed the limit ends the link with a
// protocol error: an OPEN beyond it, or a stray frame whose CLOSE E_PROTOCOL
// would make one number too many. Correct peers are unaffected, because an
// ordered link frees a number at the other side before the opener's next OPEN
// arrives, so an opener that keeps the same count never exceeds it. An API
// whose tunnel to a controller is at the limit answers a browser's OPEN for
// that controller with CLOSE E_LIMIT instead of relaying it.
//
// Credit is kept per hop and direction and counted in bytes of DATA payload as
// framed (on end-to-end channels including the 16-byte Noise tag). OPEN.Window
// and every GRANT add to what the other side may send; that allowance never
// exceeds MaxWindow. More DATA than granted, or a GRANT that would lift the
// allowance above MaxWindow, closes the channel with CLOSE E_PROTOCOL. EOF,
// CLOSE, GRANT and channel 0 do not count.
//
// Relay. The API relays a browser's channel to a controller: it puts the
// browser's OPEN payload unchanged into tunnel.Open, relays DATA and EOF
// unchanged and one to one (each DATA payload as one DATA frame, each EOF as
// one EOF), keeps its own credit on each hop, grants the controller no more
// than it can hold until the browser takes it, sends its first GRANT to the
// browser only after the controller's first GRANT arrived (copying Noise
// message 2 from it on an end-to-end channel), or passes on the controller's
// CLOSE instead, and passes on a CLOSE with the same code. One exception: on
// a channel of a core action with flow transfer that the shell opened
// (core.ShellOpens; ngc.store/controller/activate), the shell sends EOF right
// after the first GRANT and no DATA — DATA from it is a protocol error (CLOSE
// E_PROTOCOL) — and the API itself writes the transfer header and the file
// on the tunnel hop, then EOF (core.FlowTransfer).
//
// End to end. Channels to apps other than core apps are encrypted between shell
// and controller (noisep.E2EPrologue binds controller, app, service, action and
// the exact OPEN params and task). Noise message 1 travels in Open.E2E, message
// 2 in the first GRANT of the side that accepted the channel, which therefore
// has 4+HandshakeSize bytes. That GRANT without it, and a handshake in any
// other GRANT (a later one, one from the opener, or one on a core-app channel),
// is a protocol error (CLOSE E_PROTOCOL). Every DATA payload is then one Noise
// transport message whose plaintext is one inner message (E2E, ParseE2E).
// Right after reading message 2 the shell sends its first inner message — an
// E2EData, empty if it has no input yet, or its input and E2EEnd — and the
// controller starts the service only once that message has decrypted (waiting
// at most E2EFirstTimeout seconds). E2EEnd from the shell ends the input;
// E2EEnd from the controller carries the outcome. Nothing may follow E2EEnd
// from that side: an inner message a side sends after its own E2EEnd is a
// protocol error (CLOSE E_PROTOCOL), while the controller's output still
// follows the shell's E2EEnd. A side that sees EOF or CLOSE of the channel
// before the other side's E2EEnd treats the data as cut off and reports an
// error: EOF and CLOSE only end the transport.
//
// Core apps (CoreApp) have no end-to-end handshake; their DATA is plain inside
// the link's Noise session and a result is all DATA up to the other side's EOF
// (package core, Flow).
//
// Results. A result counts only if its channel ends in success: on a
// core-app channel with CLOSE status ok, on an end-to-end channel with the
// controller's E2EEnd of status ok; after an error the receiver discards
// what came. Every call-style result is at most MaxCallResult bytes, on
// core-app and app channels alike: on an end-to-end channel it is the
// controller's output up to its E2EEnd, for every action that is not a
// stream (store.Action.Stream). A sender with more ends with CodeTooLarge
// (on an end-to-end channel in its E2EEnd).
// The receiver enforces the limit and closes the channel with CodeTooLarge
// when more arrives: the API for a controller's result on a core-app channel,
// before passing it on; the shell for the API's own results and on every
// end-to-end channel, whose plaintext the API cannot see.
package wire

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
)

// Type is the kind of a frame.
type Type uint8

const (
	TypeOpen  Type = 1 // opens a channel; payload: Open as JSON (on a tunnel: tunnel.Open)
	TypeData  Type = 2 // channel data; on channel 0: Control as JSON
	TypeEOF   Type = 3 // the sender will send no more data; empty payload
	TypeClose Type = 4 // closes the channel; payload: Close as JSON
	TypeGrant Type = 5 // credit (uint32, big endian); the first GRANT of an end-to-end channel also carries Noise message 2
)

// Limits. Sizes in bytes, times in seconds.
const (
	HeaderSize         = 5
	MaxFrame           = 65519 // largest plaintext of one Noise transport message (65535 - 16)
	MaxPayload         = MaxFrame - HeaderSize
	ControlChannel     = 0
	DefaultWindow      = 256 << 10
	MinWindow          = 4 << 10
	MaxWindow          = 1 << 20  // most credit a side may have outstanding per channel and direction
	MaxSessionChannels = 64       // channel numbers not free (open or closing) on one browser session link
	MaxTunnelChannels  = 1024     // channel numbers not free (open or closing) on one API–controller tunnel
	MaxOpenPayload     = 40 << 10 // tunnel.Open: the largest browser OPEN in base64 plus certificate and proof
	MaxBrowserOpen     = 16 << 10
	MaxClosePayload    = 4 << 10
	MaxParams          = 8 << 10
	MaxMessageText     = 256
	HandshakeSize      = 48              // Noise_KK message 1 or 2 with an empty payload: 32-byte key, 16-byte tag
	MaxE2EPlaintext    = MaxPayload - 16 // 65498: plaintext of one end-to-end Noise message in a DATA frame
	MaxCallResult      = 8 << 20         // result of a call-style channel (core.Flow)
	E2EFirstTimeout    = 30              // seconds the controller waits for the shell's first end-to-end message
)

// Inner end-to-end messages: the first plaintext byte.
const (
	E2EData byte = 1
	E2EEnd  byte = 2
)

// maxTask bounds the payload of the task envelope in an OPEN: task.MaxTask;
// wire cannot import task.
const maxTask = 16 << 10

// Frame is one unit on a link.
type Frame struct {
	Type    Type
	Channel uint32
	Payload []byte // from Decode: aliases the decoded buffer, valid only until it is reused
}

// Every parser below answers any failure with its own error alone: the JSON
// decoder's errors quote member names and parts of the input.
var (
	ErrFrame   = errors.New("wire: malformed frame")
	ErrOpen    = errors.New("wire: malformed OPEN payload")
	ErrClose   = errors.New("wire: malformed CLOSE payload")
	ErrControl = errors.New("wire: malformed or unknown control message")
	ErrE2E     = errors.New("wire: malformed end-to-end message")
)

// Encode serializes f after checking the rules for its type.
func (f Frame) Encode() ([]byte, error) {
	if err := f.check(); err != nil {
		return nil, err
	}
	b := make([]byte, HeaderSize+len(f.Payload))
	b[0] = byte(f.Type)
	binary.BigEndian.PutUint32(b[1:HeaderSize], f.Channel)
	copy(b[HeaderSize:], f.Payload)
	return b, nil
}

// Decode parses one frame and checks the rules for its type. The Payload of
// the returned Frame points into b and is only valid until b is reused.
func Decode(b []byte) (Frame, error) {
	if len(b) < HeaderSize || len(b) > MaxFrame {
		return Frame{}, ErrFrame
	}
	f := Frame{Type: Type(b[0]), Channel: binary.BigEndian.Uint32(b[1:HeaderSize]), Payload: b[HeaderSize:]}
	if err := f.check(); err != nil {
		return Frame{}, err
	}
	return f, nil
}

func (f Frame) check() error {
	if len(f.Payload) > MaxPayload {
		return ErrFrame
	}
	switch f.Type {
	case TypeData:
		return nil
	case TypeOpen:
		if f.Channel == ControlChannel || len(f.Payload) == 0 || len(f.Payload) > MaxOpenPayload {
			return ErrFrame
		}
	case TypeEOF:
		if f.Channel == ControlChannel || len(f.Payload) != 0 {
			return ErrFrame
		}
	case TypeClose:
		if f.Channel == ControlChannel || len(f.Payload) == 0 || len(f.Payload) > MaxClosePayload {
			return ErrFrame
		}
	case TypeGrant:
		if f.Channel == ControlChannel || (len(f.Payload) != 4 && len(f.Payload) != 4+HandshakeSize) {
			return ErrFrame
		}
	default:
		return ErrFrame
	}
	return nil
}

// Grant builds the payload of a GRANT frame: credit, then nothing or the
// HandshakeSize-byte Noise message 2.
func Grant(credit uint32, handshake []byte) []byte {
	b := make([]byte, 4, 4+len(handshake))
	binary.BigEndian.PutUint32(b, credit)
	return append(b, handshake...)
}

// ParseGrant splits a GRANT payload: 4 bytes credit (big endian,
// 1..MaxWindow), then nothing or the HandshakeSize-byte Noise message 2.
func ParseGrant(p []byte) (uint32, []byte, error) {
	if len(p) != 4 && len(p) != 4+HandshakeSize {
		return 0, nil, ErrFrame
	}
	credit := binary.BigEndian.Uint32(p)
	if credit == 0 || credit > MaxWindow {
		return 0, nil, ErrFrame
	}
	if len(p) == 4 {
		return credit, nil, nil
	}
	return credit, p[4:], nil
}

var (
	appIDPattern = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9-]+)+$`)
	namePattern  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
)

// ValidAppID reports whether s is a well-formed app id: reverse domain, at most 100 characters.
func ValidAppID(s string) bool { return len(s) <= 100 && appIDPattern.MatchString(s) }

// ValidName reports whether s is a well-formed service or action name.
func ValidName(s string) bool { return namePattern.MatchString(s) }

// ValidText reports whether s is 1 to max bytes of printable UTF-8: every
// rune unicode.IsPrint (letters, marks, numbers, punctuation, symbols and
// the ASCII space; no control characters, no line breaks). Names, titles and
// messages meant for people use it; store.ValidText is the same rule.
//
// The reference tables are Go's: Unicode 17.0 with Go 1.27 (unicode.Version).
// In JavaScript every code point (not every UTF-16 unit) must match
// /^[\p{L}\p{M}\p{N}\p{P}\p{S} ]$/u, and the length counts UTF-8 bytes. A
// code point the checker's tables do not know counts as not printable; the
// noncharacter U+FDD0, which no table will ever assign, is never printable.
// A checker with older tables therefore refuses some text that Go accepts,
// so senders whose text others check should keep to long-assigned
// characters.
func ValidText(s string, max int) bool {
	// Ranging over invalid UTF-8 yields U+FFFD, which is printable, so the
	// encoding is checked first.
	if s == "" || len(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// CoreApp reports whether app belongs to NGC itself. The API serves these
// channels; they carry no end-to-end handshake of their own because the
// session to the API is already encrypted end to end.
func CoreApp(app string) bool { return strings.HasPrefix(app, "ngc.") }

// Open is the payload of an OPEN frame from the browser. The API forwards it
// to a controller byte for byte inside tunnel.Open.
type Open struct {
	App     string          `json:"app"`
	Service string          `json:"service"`
	Action  string          `json:"action"`
	Params  json.RawMessage `json:"params,omitempty"`  // JSON object of at most MaxParams bytes; none with a task, whose params are the params
	Window  uint32          `json:"window"`            // credit the opener grants the other end, MinWindow..MaxWindow
	E2E     enc.Bytes       `json:"e2e,omitempty"`     // Noise message 1 of the end-to-end handshake, HandshakeSize bytes; empty for core apps
	Task    *sign.Envelope  `json:"task,omitempty"`    // signed task (package task), for root tasks and re-confirmation
	Confirm *proof.Proof    `json:"confirm,omitempty"` // the user's re-confirmation of Task (task.VerifyConfirm)

	task []byte // the task member as the OPEN carried it (ParseOpen)
}

// TaskBytes returns the value of the task member exactly as it stood in the
// OPEN payload ParseOpen parsed, from '{' to '}': the bytes
// noisep.E2EPrologue hashes. It is nil without a task, and for an Open that
// ParseOpen did not make.
func (o Open) TaskBytes() []byte { return o.task }

// openDoc is an OPEN as it is decoded: the same members, but the task keeps
// its bytes.
type openDoc struct {
	App     string          `json:"app"`
	Service string          `json:"service"`
	Action  string          `json:"action"`
	Params  json.RawMessage `json:"params,omitempty"`
	Window  uint32          `json:"window"`
	E2E     enc.Bytes       `json:"e2e,omitempty"`
	Task    *openTask       `json:"task,omitempty"`
	Confirm *proof.Proof    `json:"confirm,omitempty"`
}

// openTask is the task member of an OPEN: the envelope and its bytes.
type openTask struct {
	env sign.Envelope
	raw []byte
}

// taskBudget bounds the task member while it is decoded on its own: an
// envelope has no array.
var taskBudget = enc.Budget{Size: MaxBrowserOpen}

// UnmarshalJSON implements json.Unmarshaler. encoding/json/v2 hands it the
// member's value exactly as it stands in the input, after the checks of the
// whole OPEN; the envelope is decoded as strictly as the rest.
func (t *openTask) UnmarshalJSON(b []byte) error {
	if err := taskBudget.Decode(b, &t.env); err != nil {
		return err
	}
	t.raw = bytes.Clone(b)
	return nil
}

// openBudget bounds an OPEN while it is decoded (enc.Budget). It has no array;
// its params stay raw, byte for byte as noisep.E2EPrologue hashes them, and
// the whole OPEN is small enough to bound what decoding them costs. The task
// is a struct here, so its members are counted before its bytes are kept.
var openBudget = enc.Budget{Size: MaxBrowserOpen, Items: map[string]int{"params": enc.Raw}}

// ParseOpen decodes and validates an OPEN payload from the browser. Params
// and TaskBytes keep the bytes as they arrived. Every failure is ErrOpen.
func ParseOpen(p []byte) (Open, error) {
	var d openDoc
	if openBudget.Decode(p, &d) != nil {
		return Open{}, ErrOpen
	}
	o := Open{App: d.App, Service: d.Service, Action: d.Action, Params: d.Params, Window: d.Window, E2E: d.E2E,
		Confirm: d.Confirm}
	if d.Task != nil {
		o.Task, o.task = &d.Task.env, d.Task.raw
	}
	// DecodeStrict has checked the params in full: one JSON value, valid
	// UTF-8, no repeated names. Scanning them again would double the cost of
	// the largest params, whose names are what makes them expensive.
	if err := o.validate(false); err != nil {
		return Open{}, err
	}
	return o, nil
}

// Validate checks the fields of o.
func (o Open) Validate() error { return o.validate(true) }

// validate checks the fields of o; scanParams says whether the params still
// need the full scan of ValidParams.
func (o Open) validate(scanParams bool) error {
	params := paramsShape(o.Params)
	if scanParams {
		params = ValidParams(o.Params)
	}
	if !ValidAppID(o.App) || !ValidName(o.Service) || !ValidName(o.Action) || !params ||
		o.Window < MinWindow || o.Window > MaxWindow {
		return ErrOpen
	}
	// Core apps run in the API, inside the session's own Noise session; every
	// other app gets Noise message 1 of its end-to-end handshake.
	if CoreApp(o.App) {
		if len(o.E2E) != 0 {
			return ErrOpen
		}
	} else if len(o.E2E) != HandshakeSize {
		return ErrOpen
	}
	// With a task, only the signed params count (task.Check), so the OPEN
	// carries none of its own.
	if o.Task != nil && (len(o.Params) != 0 || len(o.Task.Payload) == 0 || len(o.Task.Payload) > maxTask ||
		len(o.Task.Sig) != ed25519.SignatureSize) {
		return ErrOpen
	}
	if o.Confirm != nil && (o.Task == nil || o.Confirm.Validate() != nil) {
		return ErrOpen
	}
	return nil
}

// ValidParams reports whether raw is empty or exactly one JSON object of at
// most MaxParams bytes: valid UTF-8, no duplicate member names at any depth.
func ValidParams(raw json.RawMessage) bool {
	return paramsShape(raw) && (len(raw) == 0 || jsontext.Value(raw).IsValid())
}

// paramsShape is ValidParams without the scan of the whole value, for params
// that enc.DecodeStrict has already checked: empty, or at most MaxParams bytes
// of a JSON object.
func paramsShape(raw json.RawMessage) bool {
	return len(raw) == 0 || (len(raw) <= MaxParams && jsontext.Value(raw).Kind() == '{')
}

// Status of a closed channel.
type Status string

const (
	StatusOK    Status = "ok"
	StatusError Status = "error"
)

// Code names an error.
type Code string

const (
	CodeForbidden   Code = "E_FORBIDDEN"
	CodeUnreachable Code = "E_UNREACHABLE"
	CodeExit        Code = "E_EXIT"
	CodeTimeout     Code = "E_TIMEOUT"
	CodeLimit       Code = "E_LIMIT"
	CodeProtocol    Code = "E_PROTOCOL"
	CodeConfirm     Code = "E_CONFIRM"
	CodeLicense     Code = "E_LICENSE"
	CodeNotFound    Code = "E_NOTFOUND"
	CodeVerify      Code = "E_VERIFY"
	CodeInternal    Code = "E_INTERNAL"
	CodeTooLarge    Code = "E_TOOLARGE"
	// Answers before the WebSocket. E_LOGIN is the same for every failed
	// login and for a missing or expired session, so that no answer tells
	// which part failed or whether a user exists. E_LOCKED answers after too
	// many failed attempts; the HTTP answer says in Retry-After when to try
	// again.
	CodeLogin  Code = "E_LOGIN"
	CodeLocked Code = "E_LOCKED"
)

var messages = map[Code]string{
	CodeForbidden:   "Keine Berechtigung.",
	CodeUnreachable: "Dienst nicht erreichbar.",
	CodeExit:        "Dienst mit Fehler beendet.",
	CodeTimeout:     "Zeitlimit überschritten.",
	CodeLimit:       "Zu viele offene Verbindungen.",
	CodeProtocol:    "Ungültige Nachricht.",
	CodeConfirm:     "Bitte erneut bestätigen.",
	CodeLicense:     "Keine gültige Lizenz.",
	CodeNotFound:    "Nicht gefunden.",
	CodeVerify:      "Unterschrift ungültig.",
	CodeInternal:    "Interner Fehler.",
	CodeTooLarge:    "Zu viele Daten.",
	CodeLogin:       "Anmeldung fehlgeschlagen.",
	CodeLocked:      "Zu viele Fehlversuche. Bitte später erneut versuchen.",
}

// Message returns the German text shown to users.
func (c Code) Message() string { return messages[c] }

// Close is the payload of a CLOSE frame.
type Close struct {
	Status   Status `json:"status"`
	Code     Code   `json:"code,omitempty"`
	Message  string `json:"message,omitempty"`
	ExitCode *int   `json:"exitCode,omitempty"`
}

// Error returns a CLOSE payload for code with its German message.
func Error(code Code) Close {
	return Close{Status: StatusError, Code: code, Message: code.Message()}
}

// closeBudget bounds a CLOSE payload while it is decoded: it has no array.
var closeBudget = enc.Budget{Size: MaxClosePayload}

// ParseClose decodes and validates a CLOSE payload. Every failure is ErrClose.
func ParseClose(p []byte) (Close, error) {
	var c Close
	if closeBudget.Decode(p, &c) != nil {
		return Close{}, ErrClose
	}
	if err := c.Validate(); err != nil {
		return Close{}, err
	}
	return c, nil
}

// Validate checks the fields of c.
func (c Close) Validate() error {
	switch c.Status {
	case StatusOK:
		if c.Code != "" {
			return ErrClose
		}
	case StatusError:
		if _, ok := messages[c.Code]; !ok {
			return ErrClose
		}
	default:
		return ErrClose
	}
	// The shell shows the message to people, so it is text for people like
	// every name and title (ValidText), which also bounds its length.
	if (c.Message != "" && !ValidText(c.Message, MaxMessageText)) ||
		(c.ExitCode != nil && (*c.ExitCode < 0 || *c.ExitCode > 255)) {
		return ErrClose
	}
	return nil
}

// E2EMessage is one inner end-to-end message.
type E2EMessage struct {
	Kind   byte   // E2EData or E2EEnd
	Data   []byte // E2EData: the data; aliases the plaintext
	Status Close  // E2EEnd from the controller: the outcome
}

// E2E builds one end-to-end plaintext. For E2EEnd, body is empty (shell) or
// a valid CLOSE payload (controller).
func E2E(kind byte, body []byte) ([]byte, error) {
	switch kind {
	case E2EData:
		if len(body) > MaxE2EPlaintext-1 {
			return nil, ErrE2E
		}
	case E2EEnd:
		if len(body) != 0 {
			if _, err := ParseClose(body); err != nil {
				return nil, ErrE2E
			}
		}
	default:
		return nil, ErrE2E
	}
	b := make([]byte, 1+len(body))
	b[0] = kind
	copy(b[1:], body)
	return b, nil
}

// ParseE2E checks one end-to-end plaintext; fromController says who sent it.
// E2EData carries 0..MaxE2EPlaintext-1 bytes of data. E2EEnd from the shell
// carries nothing; from the controller it carries a CLOSE payload of 1 to
// MaxClosePayload bytes with the outcome. Every failure is ErrE2E.
func ParseE2E(plain []byte, fromController bool) (E2EMessage, error) {
	if len(plain) == 0 || len(plain) > MaxE2EPlaintext {
		return E2EMessage{}, ErrE2E
	}
	m := E2EMessage{Kind: plain[0]}
	body := plain[1:]
	switch m.Kind {
	case E2EData:
		m.Data = body
	case E2EEnd:
		if !fromController {
			if len(body) != 0 {
				return E2EMessage{}, ErrE2E
			}
			return m, nil
		}
		c, err := ParseClose(body)
		if err != nil {
			return E2EMessage{}, ErrE2E
		}
		m.Status = c
	default:
		return E2EMessage{}, ErrE2E
	}
	return m, nil
}

// Control is a message on channel 0.
type Control struct {
	T string          `json:"t"`
	B json.RawMessage `json:"b,omitempty"` // the body; the parser of its type checks it
}

// Control message types. ControlAllowed says who may send which.
const (
	// CtlHello: tunnel only, the first message both ways (tunnel.Hello).
	CtlHello = "hello"
	// CtlTrust: API -> shell only, a newer trust list version exists (body
	// session.TrustHint); the shell fetches it with GET session.PathTrust.
	// Controllers get versions on the channel ngc.users/controller/trust.
	CtlTrust = "trust"
	// CtlNotify: API -> shell and controller -> API (core.Notice).
	CtlNotify = "notify"
	// CtlStatus: controller -> API (tunnel.Status).
	CtlStatus = "status"
	// CtlUpdate: controller -> API, update progress (store.UpdateReport).
	CtlUpdate = "update"
	// CtlBye: both ways, both links; the sender closes the link.
	CtlBye = "bye"
)

// Links, for ControlAllowed.
const (
	LinkSession = "session" // browser ↔ API
	LinkTunnel  = "tunnel"  // controller ↔ API
)

// ControlAllowed reports whether a control message of type t may travel on
// link from this side (fromAPI: sent by the API). Anything else ends the link.
func ControlAllowed(link string, fromAPI bool, t string) bool {
	switch {
	case link == LinkSession && fromAPI:
		return t == CtlTrust || t == CtlNotify || t == CtlBye
	case link == LinkSession:
		return t == CtlBye
	case link == LinkTunnel && fromAPI:
		return t == CtlHello || t == CtlBye
	case link == LinkTunnel:
		return t == CtlHello || t == CtlStatus || t == CtlUpdate || t == CtlNotify || t == CtlBye
	}
	return false
}

// controlBudget bounds a control message while it is decoded: it has no
// array, and its body stays raw for the parser of its type, bounded by the
// frame. Decoding a raw body costs up to about 20 times its size (enc.Budget),
// about 1.4 MiB for a body of one frame.
var controlBudget = enc.Budget{Size: MaxPayload, Items: map[string]int{"b": enc.Raw}}

// ParseControl decodes a control message and checks its type. Every failure
// is ErrControl.
func ParseControl(p []byte) (Control, error) {
	var c Control
	if controlBudget.Decode(p, &c) != nil {
		return Control{}, ErrControl
	}
	switch c.T {
	case CtlHello, CtlTrust, CtlNotify, CtlStatus, CtlUpdate, CtlBye:
		return c, nil
	}
	return Control{}, ErrControl
}
