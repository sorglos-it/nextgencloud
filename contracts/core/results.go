package core

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"regexp"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/store"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// States of an app for the desktop and the app store.
const (
	AppOK          = "ok"
	AppUnreachable = "unreachable" // its controller has no tunnel to this API
	AppUnlicensed  = "unlicensed"
)

// License states in InstalledApp and LicenseResult.
const (
	LicenseNone    = "none" // free app
	LicenseValid   = "valid"
	LicenseGrace   = "grace"
	LicenseExpired = "expired"
)

// Kinds of log entries.
const (
	LogLogin       = "login"
	LogLoginFailed = "login-failed"
	LogChannel     = "channel"
	LogTask        = "task" // signed task, with its signature kept by the API
	LogTrust       = "trust"
	LogRights      = "rights"
	LogApp         = "app"
	LogStore       = "store"
	LogLicense     = "license"
	LogUpdate      = "update"
)

// Levels of a notice.
const (
	NoticeInfo  = "info"
	NoticeWarn  = "warn"
	NoticeError = "error"
)

// Me answers ngc.desktop/me.
type Me struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Roles         []string    `json:"roles,omitempty"` // from the trust list, where they are never empty; nil is left out, never null
	Admin         bool        `json:"admin"`
	PasswordLogin bool        `json:"passwordLogin"` // show the hint to switch to passkeys
	TOTP          bool        `json:"totp"`
	Level         trust.Level `json:"level"` // strictest re-confirmation level of the user's roles
}

// DesktopApp is an entry of the start menu (ngc.desktop/apps).
type DesktopApp struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Icon    string   `json:"icon,omitempty"` // path below ui/
	Width   int      `json:"width,omitzero"`
	Height  int      `json:"height,omitzero"`
	Actions []string `json:"actions,omitempty"` // actions this user may use
	State   string   `json:"state"`
	Core    bool     `json:"core,omitzero"` // app store and users & rights; their UI is part of the shell
	// Node is the controller the app's services run on (trust list node ID);
	// the shell needs it for noisep.E2EPrologue and the controller's Noise
	// key and never shows it; empty for apps without services.
	Node string `json:"node,omitempty"`
}

// TOTPBegin answers ngc.desktop/totp-begin.
type TOTPBegin struct {
	Secret string `json:"secret"` // base32, for typing in
	URI    string `json:"uri"`    // otpauth://totp/…, for the QR code
}

// UserInfo is a row of ngc.users/list; keys and roles come from the trust list.
type UserInfo struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Roles     []string `json:"roles,omitempty"`
	TOTP      bool     `json:"totp"`
	Locked    bool     `json:"locked"`
	LastLogin int64    `json:"lastLogin,omitzero"`
}

// PairCodeResult answers ngc.users/pair-code and ngc.users/join-code.
type PairCodeResult struct {
	Code    string `json:"code"` // tunnel.PairCode, for join-code tunnel.JoinCode, as string
	Expires int64  `json:"expires"`
}

// Pending is a controller that paired but is not in the trust list yet.
type Pending struct {
	Name        string    `json:"name"`
	Key         enc.Bytes `json:"key"`
	Fingerprint string    `json:"fingerprint"` // the admin compares it with the output of the ulh script
	Since       int64     `json:"since"`
}

// LogEntry is a row of ngc.users/log.
type LogEntry struct {
	// ID is 32 lower-case hex characters from 16 random bytes, made by the
	// API; hex so every database collation orders it like Go.
	ID     string `json:"id"`
	Time   int64  `json:"time"`
	User   string `json:"user,omitempty"`
	Kind   string `json:"kind"`
	App    string `json:"app,omitempty"`
	Action string `json:"action,omitempty"`
	Node   string `json:"node,omitempty"`
	Result string `json:"result"` // "ok" or a wire.Code
	Detail string `json:"detail,omitempty"`
}

var logID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ValidLogID reports whether s is the ID of a log entry: 32 lower-case hex
// characters.
func ValidLogID(s string) bool { return logID.MatchString(s) }

// SourceInfo is a store in ngc.store/sources.
type SourceInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Seq     uint64 `json:"seq"`              // newest catalog seen
	Checked int64  `json:"checked,omitzero"` // last successful fetch
	Error   string `json:"error,omitempty"`  // German
}

// CatalogResult answers ngc.store/catalog. The shell verifies it with the
// store key from the trust list before showing it.
type CatalogResult struct {
	Catalog sign.Envelope `json:"catalog"`
}

// InstalledApp is a row of ngc.store/installed.
type InstalledApp struct {
	ID       string `json:"id"`
	Version  string `json:"version,omitempty"` // empty if only kept data remains
	Store    string `json:"store"`
	Node     string `json:"node"`
	Active   bool   `json:"active"`
	State    string `json:"state"`
	License  string `json:"license"`
	KeptData int64  `json:"keptData,omitzero"` // bytes of data kept after uninstalling
}

// UploadResult answers ngc.store/upload.
type UploadResult struct {
	ID      string    `json:"id"`
	Version string    `json:"version"`
	SHA256  enc.Bytes `json:"sha256"` // goes into ActivateParams
}

// LicenseResult answers ngc.store/license.
type LicenseResult struct {
	App   string `json:"app"`
	State string `json:"state"`
}

// InstallKey answers ngc.store/install-key; the admin gives it to the shop when buying.
type InstallKey struct {
	Installation string    `json:"installation"`
	Key          enc.Bytes `json:"key"` // X25519
}

// UpdateInfo is a row of ngc.store/updates.
type UpdateInfo struct {
	Target    string `json:"target"`
	Installed string `json:"installed,omitempty"`
	Available string `json:"available,omitempty"`
	Mode      string `json:"mode"`
	Window    string `json:"window,omitempty"`
	State     string `json:"state,omitempty"` // store.Update… of the newest report
}

// TrustResult answers ngc.users/trust, and ngc.users/controller/trust with
// the newest version the controller verified.
type TrustResult struct {
	Seq uint64 `json:"seq"`
}

// ErrResult reports a result EncodeResult does not send.
var ErrResult = errors.New("core: result the shell would refuse")

// EncodeResult encodes the result of a core action as the API sends it.
// encoding/json writes a nil slice as null, which the shell refuses (no null,
// enc.Budget); EncodeResult writes an empty or nil list as [] and an empty or
// nil map as {}, so the eight results that are lists (ngc.desktop/apps,
// ngc.users/list, log, pending and rights, ngc.store/sources, installed and
// updates) are [] when empty. It encodes with encoding/json/v2, whose
// omitempty keeps false and 0; the result types mark optional bools and
// numbers omitzero, which both versions leave out alike. Every failure is
// ErrResult: a value encoding/json/v2 refuses (invalid UTF-8, a func), a null
// anywhere else (a nil pointer or interface outside an omitempty field), and
// a result of more than wire.MaxCallResult bytes.
func EncodeResult(v any) ([]byte, error) {
	b, err := jsonv2.Marshal(v, jsonv2.Deterministic(true))
	if err != nil || len(b) > wire.MaxCallResult || !nullFree(b) {
		return nil, ErrResult
	}
	return b, nil
}

// nullFree reports whether the JSON value b holds no null.
func nullFree(b []byte) bool {
	dec := jsontext.NewDecoder(bytes.NewReader(b))
	for {
		tok, err := dec.ReadToken()
		if err == io.EOF {
			return true
		}
		if err != nil || tok.Kind() == 'n' {
			return false
		}
	}
}

// Notice is the body of a "notify" control message: the API tells the
// shell, a controller tells the API.
type Notice struct {
	Level string `json:"level"`
	Text  string `json:"text"` // German
	App   string `json:"app,omitempty"`
}

// MaxNotice limits the body of a "notify" control message.
const MaxNotice = 1024

// ErrNotice reports a malformed notice.
var ErrNotice = errors.New("core: malformed notice")

// noticeBudget bounds a notice while it is decoded (enc.Budget): it has no
// array.
var noticeBudget = enc.Budget{Size: MaxNotice}

// ParseNotice checks the body of a "notify" control message: at most
// MaxNotice bytes of strict JSON (enc.Budget); Level info, warn or error;
// Text store.ValidText of at most wire.MaxMessageText bytes; App empty or a
// valid non-core app ID. Every failure is ErrNotice.
func ParseNotice(b []byte) (Notice, error) {
	var n Notice
	if noticeBudget.Decode(b, &n) != nil {
		return Notice{}, ErrNotice
	}
	switch n.Level {
	case NoticeInfo, NoticeWarn, NoticeError:
	default:
		return Notice{}, ErrNotice
	}
	if !store.ValidText(n.Text, wire.MaxMessageText) || (n.App != "" && !validApp(n.App)) {
		return Notice{}, ErrNotice
	}
	return n, nil
}

// LicenseOf maps a license to the strings in InstalledApp and LicenseResult:
// nil -> LicenseNone; store.LicenseValid/Grace/Expired -> LicenseValid/Grace/Expired.
func LicenseOf(l *store.License, now int64) string {
	if l == nil {
		return LicenseNone
	}
	switch l.State(now) {
	case store.LicenseValid:
		return LicenseValid
	case store.LicenseGrace:
		return LicenseGrace
	}
	return LicenseExpired
}

// MaxTransferHeader limits the JSON header of a transfer.
const MaxTransferHeader = 8 << 20

// Transfer is the header of a file the API sends to a controller.
// Exactly one source: Catalog with Store (app from a store), Release (NGC
// update) or neither (uploaded app, the signed task names its checksum).
type Transfer struct {
	Catalog *sign.Envelope `json:"catalog,omitempty"`
	Store   string         `json:"store,omitempty"` // empty for an uploaded app; ActivateParams.Store is LocalStore there instead.
	Release *sign.Envelope `json:"release,omitempty"`
	License *sign.Envelope `json:"license,omitempty"` // paid apps
	Key     enc.Bytes      `json:"key,omitempty"`     // paid apps: content key, unwrapped by the API
	Size    int64          `json:"size"`
	SHA256  enc.Bytes      `json:"sha256"`
}

// Validate checks t.
func (t Transfer) Validate() error {
	ok := t.Size > 0 && t.Size <= store.MaxPackage && len(t.SHA256) == 32 &&
		!(t.Catalog != nil && t.Release != nil) && (t.Catalog != nil) == (t.Store != "") &&
		(t.Store == "" || wire.ValidName(t.Store)) &&
		(len(t.Key) == 0) == (t.License == nil) && (len(t.Key) == 0 || (len(t.Key) == store.KeySize && t.Catalog != nil))
	if !ok {
		return ErrTransfer
	}
	return nil
}

// WriteTransfer writes the header: 4 bytes length (big endian), then the JSON.
func WriteTransfer(w io.Writer, t Transfer) error {
	if err := t.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if len(b) > MaxTransferHeader {
		return ErrTransfer
	}
	head := binary.BigEndian.AppendUint32(nil, uint32(len(b)))
	_, err = w.Write(append(head, b...))
	return err
}

// ReadTransfer reads exactly the header; the file follows in r. Every
// failure is ErrTransfer.
func ReadTransfer(r io.Reader) (Transfer, error) {
	var n [4]byte
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return Transfer{}, ErrTransfer
	}
	size := binary.BigEndian.Uint32(n[:])
	if size == 0 || size > MaxTransferHeader {
		return Transfer{}, ErrTransfer
	}
	b := make([]byte, size)
	if _, err := io.ReadFull(r, b); err != nil {
		return Transfer{}, ErrTransfer
	}
	return parseTransfer(b)
}

// transferBudget bounds a transfer header while it is decoded (enc.Budget):
// it has no array, and its envelopes hold bytes.
var transferBudget = enc.Budget{Size: MaxTransferHeader}

// parseTransfer decodes and checks the JSON of a transfer header.
func parseTransfer(b []byte) (Transfer, error) {
	var t Transfer
	if transferBudget.Decode(b, &t) != nil {
		return Transfer{}, ErrTransfer
	}
	if err := t.Validate(); err != nil {
		return Transfer{}, err
	}
	return t, nil
}
