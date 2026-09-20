// Package wire defines frames, the unit of transfer between browser, API and
// controller. Every frame is the plaintext of one Noise transport message;
// channel data is end-to-end encrypted in addition (see package noisep).
package wire

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
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
	TypeOpen  Type = 1 // opens a channel; payload: Open as JSON
	TypeData  Type = 2 // channel data; on channel 0: Control as JSON
	TypeEOF   Type = 3 // the sender will send no more data; empty payload
	TypeClose Type = 4 // closes the channel; payload: Close as JSON
	TypeGrant Type = 5 // credit (uint32, big endian); the first GRANT also carries the end-to-end handshake reply
)

// Limits.
const (
	HeaderSize      = 5
	MaxFrame        = 65519 // largest plaintext of one Noise transport message (65535 - 16)
	MaxPayload      = MaxFrame - HeaderSize
	ControlChannel  = 0
	DefaultWindow   = 256 << 10
	MinWindow       = 4 << 10
	MaxChannels     = 64 // open channels per link and session
	MaxOpenPayload  = 32 << 10
	MaxBrowserOpen  = 16 << 10
	MaxClosePayload = 4 << 10
	MaxParams       = 8 << 10
	MaxMessageText  = 256
	MinHandshake    = 48 // Noise_KK message without payload: 32-byte ephemeral key and 16-byte tag
	MaxHandshake    = 1024
)

// Frame is one unit on a link.
type Frame struct {
	Type    Type
	Channel uint32
	Payload []byte // from Decode: aliases the decoded buffer, valid only until it is reused
}

var (
	ErrFrame   = errors.New("wire: malformed frame")
	ErrOpen    = errors.New("wire: malformed OPEN payload")
	ErrClose   = errors.New("wire: malformed CLOSE payload")
	ErrControl = errors.New("wire: unknown control message")
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
		if f.Channel == ControlChannel || len(f.Payload) < 4 {
			return ErrFrame
		}
	default:
		return ErrFrame
	}
	return nil
}

// Grant builds the payload of a GRANT frame.
func Grant(credit uint32, handshake []byte) []byte {
	b := make([]byte, 4, 4+len(handshake))
	binary.BigEndian.PutUint32(b, credit)
	return append(b, handshake...)
}

// ParseGrant splits a GRANT payload into credit and the optional handshake reply.
func ParseGrant(p []byte) (uint32, []byte, error) {
	if len(p) < 4 {
		return 0, nil, ErrFrame
	}
	hs := p[4:]
	if len(hs) != 0 && (len(hs) < MinHandshake || len(hs) > MaxHandshake) {
		return 0, nil, ErrFrame
	}
	return binary.BigEndian.Uint32(p[:4]), hs, nil
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
// to the controller with the fields of tunnel.Open added.
type Open struct {
	App     string          `json:"app"`
	Service string          `json:"service"`
	Action  string          `json:"action"`
	Params  json.RawMessage `json:"params,omitempty"` // JSON object, at most MaxParams bytes
	Window  uint32          `json:"window"`           // credit the opener grants the other end
	E2E     enc.Bytes       `json:"e2e,omitempty"`    // Noise_KK message 1 of the end-to-end handshake; empty for core apps
	Task    *sign.Envelope  `json:"task,omitempty"`   // signed task (package task), for root tasks and re-confirmation
	Confirm *proof.Proof    `json:"confirm,omitempty"`
}

// ParseOpen decodes and validates an OPEN payload from the browser.
func ParseOpen(p []byte) (Open, error) {
	var o Open
	if err := enc.DecodeStrict(p, MaxBrowserOpen, &o); err != nil {
		return Open{}, err
	}
	return o, o.Validate()
}

// Validate checks the fields of o.
func (o Open) Validate() error {
	if !ValidAppID(o.App) || !ValidName(o.Service) || !ValidName(o.Action) {
		return ErrOpen
	}
	if !ValidParams(o.Params) {
		return ErrOpen
	}
	if o.Window < MinWindow || len(o.E2E) > MaxHandshake || CoreApp(o.App) != (len(o.E2E) == 0) ||
		(len(o.E2E) != 0 && len(o.E2E) < MinHandshake) {
		return ErrOpen
	}
	return nil
}

// ValidParams reports whether raw is empty or a JSON object of at most MaxParams bytes.
func ValidParams(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	t := bytes.TrimSpace(raw)
	if len(raw) > MaxParams || len(t) == 0 || t[0] != '{' {
		return false
	}
	var m map[string]json.RawMessage
	return json.Unmarshal(t, &m) == nil
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

// ParseClose decodes and validates a CLOSE payload.
func ParseClose(p []byte) (Close, error) {
	var c Close
	if err := enc.DecodeStrict(p, MaxClosePayload, &c); err != nil {
		return Close{}, err
	}
	return c, c.Validate()
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

// Control is a message on channel 0.
type Control struct {
	T string          `json:"t"`
	B json.RawMessage `json:"b,omitempty"`
}

// Control message types.
const (
	CtlHello  = "hello"  // first message on a link (package tunnel: Hello)
	CtlTrust  = "trust"  // a new trust list version (trust.Signed)
	CtlNotify = "notify" // notice for the user, e.g. an update is available
	CtlStatus = "status" // controller status report
	CtlUpdate = "update" // update instruction or progress
	CtlBye    = "bye"    // the sender closes the link
)

// ParseControl decodes a control message and checks its type.
func ParseControl(p []byte) (Control, error) {
	var c Control
	if err := enc.DecodeStrict(p, MaxPayload, &c); err != nil {
		return Control{}, err
	}
	switch c.T {
	case CtlHello, CtlTrust, CtlNotify, CtlStatus, CtlUpdate, CtlBye:
		return c, nil
	}
	return Control{}, ErrControl
}
