package core

import (
	"encoding/binary"
	"encoding/json"
	"io"

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
	Roles         []string    `json:"roles"`
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
	Width   int      `json:"width,omitempty"`
	Height  int      `json:"height,omitempty"`
	Actions []string `json:"actions"` // actions this user may use
	State   string   `json:"state"`
	Core    bool     `json:"core,omitempty"` // app store and users & rights; their UI is part of the shell
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
	Roles     []string `json:"roles"`
	TOTP      bool     `json:"totp"`
	Locked    bool     `json:"locked"`
	LastLogin int64    `json:"lastLogin,omitempty"`
}

// PairCodeResult answers ngc.users/pair-code.
type PairCodeResult struct {
	Code    string `json:"code"` // tunnel.PairCode as string
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
	Time   int64  `json:"time"`
	User   string `json:"user,omitempty"`
	Kind   string `json:"kind"`
	App    string `json:"app,omitempty"`
	Action string `json:"action,omitempty"`
	Node   string `json:"node,omitempty"`
	Result string `json:"result"` // "ok" or a wire.Code
	Detail string `json:"detail,omitempty"`
}

// SourceInfo is a store in ngc.store/sources.
type SourceInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Seq     uint64 `json:"seq"`               // newest catalog seen
	Checked int64  `json:"checked,omitempty"` // last successful fetch
	Error   string `json:"error,omitempty"`   // German
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
	KeptData int64  `json:"keptData,omitempty"` // bytes of data kept after uninstalling
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

// TrustResult answers ngc.users/trust.
type TrustResult struct {
	Seq uint64 `json:"seq"`
}

// Notice is the body of a "notify" control message: the API tells the
// shell, a controller tells the API.
type Notice struct {
	Level string `json:"level"`
	Text  string `json:"text"` // German
	App   string `json:"app,omitempty"`
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

// ReadTransfer reads exactly the header; the file follows in r.
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
	var t Transfer
	if err := enc.DecodeStrict(b, MaxTransferHeader, &t); err != nil {
		return Transfer{}, ErrTransfer
	}
	return t, t.Validate()
}
