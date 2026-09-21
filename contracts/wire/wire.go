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
// or a failed decryption ends the link.
//
// Channel 0 is always open and carries Control messages; ControlAllowed says
// who may send which. On a tunnel both sides send hello first.
//
// Channels. Only one side opens channels: the browser on a session link, the
// API on a tunnel. The opener picks a number >= 1 not in use; it is free again
// once a CLOSE for it was sent or received. At most MaxSessionChannels are open
// on a session link and MaxTunnelChannels on a tunnel; beyond that the other
// side answers the OPEN with CLOSE E_LIMIT. A channel runs: OPEN (its Window is
// the credit the opener grants) -> the other side's first GRANT (credit at
// least MinWindow; on an end-to-end channel also Noise message 2) or a CLOSE
// with an error -> DATA both ways -> EOF from each side that has nothing more
// to send -> CLOSE from the side that accepted the channel. Either side may
// send CLOSE with an error at any time; after CLOSE nothing more is sent.
//
// Credit is kept per hop and direction and counted in bytes of DATA payload as
// framed (on end-to-end channels including the 16-byte Noise tag). OPEN.Window
// and every GRANT add to what the other side may send; that allowance never
// exceeds MaxWindow. More DATA than granted, or a GRANT that would lift the
// allowance above MaxWindow, closes the channel with CLOSE E_PROTOCOL. EOF,
// CLOSE, GRANT and channel 0 do not count.
//
// Relay. The API relays a browser's channel to a controller: it puts the
// browser's OPEN payload unchanged into tunnel.Open, keeps its own credit on
// each hop, grants the controller no more than it can hold until the browser
// takes it, copies Noise message 2 from the controller's first GRANT into its
// own first GRANT to the browser and passes on a CLOSE with the same code.
//
// End to end. Channels to apps other than core apps are encrypted between shell
// and controller (noisep.E2EPrologue binds controller, app, service, action and
// the exact OPEN params). Noise message 1 travels in Open.E2E, message 2 in the
// first GRANT; every DATA payload is then one Noise transport message whose
// plaintext is one inner message (E2E, ParseE2E). Right after reading message 2
// the shell sends its first inner message — an E2EData, empty if it has no
// input yet, or its input and E2EEnd — and the controller starts the service
// only once that message has decrypted (waiting at most E2EFirstTimeout
// seconds). E2EEnd from the shell ends the input; E2EEnd from the controller
// carries the outcome. A side that sees EOF or CLOSE of the channel before the
// other side's E2EEnd treats the data as cut off and reports an error: EOF and
// CLOSE only end the transport.
//
// Core apps (CoreApp) have no end-to-end handshake; their DATA is plain inside
// the link's Noise session and a result is all DATA up to the other side's EOF
// (package core, Flow). A call-style result is at most MaxCallResult bytes;
// beyond that the sender ends with CodeTooLarge.
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

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
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
	MaxSessionChannels = 64       // open channels on one browser session link
	MaxTunnelChannels  = 1024     // open channels on one API–controller tunnel
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
	if err := o.Validate(); err != nil {
		return Open{}, err
	}
	return o, nil
}

// Validate checks the fields of o.
func (o Open) Validate() error {
	if !ValidAppID(o.App) || !ValidName(o.Service) || !ValidName(o.Action) || !ValidParams(o.Params) ||
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
	if len(raw) == 0 {
		return true
	}
	v := jsontext.Value(raw)
	return len(raw) <= MaxParams && v.IsValid() && v.Kind() == '{'
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
	if len(c.Message) > MaxMessageText || (c.ExitCode != nil && (*c.ExitCode < 0 || *c.ExitCode > 255)) {
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
