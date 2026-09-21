package store

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// Prices, limits and update states. Times in seconds.
const (
	PriceBuy          = "buy"
	PriceSubscription = "subscription"
	PerSite           = "site"
	PerUser           = "user"
	PeriodMonth       = "month"
	PeriodYear        = "year"
	MaxPackage        = 4 << 30
	MaxApps           = 10000
	MaxVersions       = 100
	MaxLicense        = 8 << 10
	GraceDefault      = 14 * 86400
	MaxGrace          = 90 * 86400

	ReleaseWeb        = "web"
	ReleaseAPI        = "api" // includes ngc-store and the database migrations
	ReleaseController = "controller"

	ModeAuto   = "auto"   // install in the maintenance window (default); needs a standing approval
	ModeSemi   = "semi"   // download and check in advance, install on click
	ModeNotify = "notify" // only tell; download and install on click

	UpdateAvailable  = "available"
	UpdateDownloaded = "downloaded"
	UpdateInstalling = "installing"
	UpdateInstalled  = "installed"
	UpdateRolledBack = "rolledback"
	UpdateFailed     = "failed"
	UpdateRejected   = "rejected" // signature, approval or version check failed
)

// Catalog lists the apps of a store.
type Catalog struct {
	Store  string  `json:"store"`
	Seq    uint64  `json:"seq"`
	Issued int64   `json:"issued"`
	Apps   []Entry `json:"apps"` // sorted by ID
}

// Entry is an app in a catalog.
type Entry struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Publisher   string    `json:"publisher"`
	Description string    `json:"description,omitempty"`
	Price       *Price    `json:"price,omitempty"` // nil: free
	Versions    []Package `json:"versions"`        // ascending
}

// Package is one version of an app.
type Package struct {
	Version   string    `json:"version"`
	URL       string    `json:"url"` // https URL or a path relative to the catalog
	Size      int64     `json:"size"`
	SHA256    enc.Bytes `json:"sha256"`              // of the file as downloaded
	Encrypted bool      `json:"encrypted,omitempty"` // paid apps (Task 15)
	Needs     []string  `json:"needs,omitempty"`     // as in the manifest, shown before download
	MinNGC    string    `json:"minNgc,omitempty"`
}

// Price of a paid app. Payment happens on the store's shop page, never in NGC.
type Price struct {
	Model    string `json:"model"`
	Per      string `json:"per"`
	Amount   int64  `json:"amount"`   // smallest unit of the currency, e.g. cents
	Currency string `json:"currency"` // ISO 4217
	Period   string `json:"period,omitempty"`
	Shop     string `json:"shop"`
}

// WrappedKey is the content key of a paid app, encrypted for one installation (Task 15).
type WrappedKey struct {
	Ephemeral enc.Bytes `json:"eph"`
	Nonce     enc.Bytes `json:"nonce"`
	Sealed    enc.Bytes `json:"sealed"`
}

// License allows one installation to use a paid app.
type License struct {
	ID           string      `json:"id"`
	Store        string      `json:"store"`
	App          string      `json:"app"`
	Installation string      `json:"installation"`
	InstallKey   enc.Bytes   `json:"installKey"` // X25519 key the content key is wrapped for
	Per          string      `json:"per"`
	Users        int         `json:"users,omitempty"` // per user: how many users may get the right "use"
	Issued       int64       `json:"issued"`
	Expires      int64       `json:"expires,omitempty"` // subscription; 0 for a bought app
	Grace        int64       `json:"grace,omitempty"`   // 0 means GraceDefault
	Key          *WrappedKey `json:"key"`
}

// LicenseState tells whether a paid app may run.
type LicenseState int

const (
	LicenseValid   LicenseState = iota
	LicenseGrace                // expired, but within the grace period: runs, NGC warns
	LicenseExpired              // controllers do not start the services
)

// Release is a signed NGC version.
type Release struct {
	Seq     uint64 `json:"seq"`
	Issued  int64  `json:"issued"`
	Version string `json:"version"`
	Notes   string `json:"notes,omitempty"`
	Files   []File `json:"files"`
}

// File is one part of NGC for one architecture ("all" for the web interface).
type File struct {
	Part   string    `json:"part"`
	Arch   string    `json:"arch"`
	URL    string    `json:"url"`
	Size   int64     `json:"size"`
	SHA256 enc.Bytes `json:"sha256"`
}

// UpdateReport is the body of an "update" control message from a controller.
type UpdateReport struct {
	Target  string `json:"target"`
	Version string `json:"version,omitempty"`
	State   string `json:"state"`
	Message string `json:"message,omitempty"` // German, shown to admins
}

var (
	ErrCatalog  = errors.New("store: invalid catalog")
	ErrLicense  = errors.New("store: invalid license")
	ErrRelease  = errors.New("store: invalid release")
	ErrUpdate   = errors.New("store: invalid update message")
	ErrRollback = errors.New("store: older than the last one seen")
)

var (
	currency  = regexp.MustCompile(`^[A-Z]{3}$`)
	licenseID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

func validURL(u string) bool {
	if strings.HasPrefix(u, "https://") {
		return len(u) > len("https://") && len(u) <= 512 && !strings.ContainsAny(u, " \t\r\n\"'<>\\")
	}
	return ValidPath(u)
}

// Validate checks c.
func (c *Catalog) Validate() error {
	if !wire.ValidName(c.Store) || c.Seq == 0 || c.Issued <= 0 || len(c.Apps) > MaxApps {
		return ErrCatalog
	}
	for i, e := range c.Apps {
		if (i > 0 && c.Apps[i-1].ID >= e.ID) || !e.valid() {
			return ErrCatalog
		}
	}
	return nil
}

func (e Entry) valid() bool {
	if !wire.ValidAppID(e.ID) || wire.CoreApp(e.ID) || !label(e.Name, 64) || !label(e.Publisher, 64) ||
		len(e.Description) > 1000 || !utf8.ValidString(e.Description) || len(e.Versions) == 0 || len(e.Versions) > MaxVersions ||
		(e.Price != nil && !e.Price.valid()) {
		return false
	}
	for i, p := range e.Versions {
		if !ValidVersion(p.Version) || (i > 0 && !Newer(p.Version, e.Versions[i-1].Version)) || !validURL(p.URL) ||
			p.Size <= 0 || p.Size > MaxPackage || len(p.SHA256) != 32 || p.Encrypted != (e.Price != nil) ||
			!validNeeds(p.Needs) || (p.MinNGC != "" && !ValidVersion(p.MinNGC)) {
			return false
		}
	}
	return true
}

func (p Price) valid() bool {
	sub := p.Model == PriceSubscription
	return (p.Model == PriceBuy || sub) && (p.Per == PerSite || p.Per == PerUser) && p.Amount > 0 &&
		currency.MatchString(p.Currency) && sub == (p.Period != "") &&
		(!sub || p.Period == PeriodMonth || p.Period == PeriodYear) &&
		strings.HasPrefix(p.Shop, "https://") && validURL(p.Shop)
}

func parseCatalog(b []byte) (*Catalog, error) {
	var c Catalog
	if err := enc.DecodeStrict(b, sign.MaxPayload, &c); err != nil {
		return nil, ErrCatalog
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// VerifyCatalog checks the signature of the store st and rejects catalogs
// older than lastSeq, the newest one seen from this store.
func VerifyCatalog(env sign.Envelope, st trust.Store, lastSeq uint64) (*Catalog, error) {
	if err := sign.Verify(env, ed25519.PublicKey(st.Key), sign.PurposeCatalog); err != nil {
		return nil, err
	}
	c, err := parseCatalog(env.Payload)
	if err != nil {
		return nil, err
	}
	if c.Store != st.ID {
		return nil, ErrCatalog
	}
	if c.Seq < lastSeq {
		return nil, ErrRollback
	}
	return c, nil
}

// Validate checks l.
func (l *License) Validate() error {
	if !licenseID.MatchString(l.ID) || !wire.ValidName(l.Store) || !wire.ValidAppID(l.App) ||
		!trust.ValidID(l.Installation) || len(l.InstallKey) != 32 || (l.Per != PerSite && l.Per != PerUser) ||
		(l.Per == PerUser) != (l.Users > 0) || l.Users > 100000 || l.Issued <= 0 ||
		(l.Expires != 0 && l.Expires <= l.Issued) || l.Grace < 0 || l.Grace > MaxGrace ||
		l.Key == nil || len(l.Key.Ephemeral) != 32 || len(l.Key.Nonce) != 12 || len(l.Key.Sealed) != 32+16 {
		return ErrLicense
	}
	return nil
}

// State tells whether the app may run at now.
func (l *License) State(now int64) LicenseState {
	if l.Expires == 0 || now < l.Expires {
		return LicenseValid
	}
	grace := l.Grace
	if grace == 0 {
		grace = GraceDefault
	}
	if now < l.Expires+grace {
		return LicenseGrace
	}
	return LicenseExpired
}

func parseLicense(b []byte) (*License, error) {
	var l License
	if err := enc.DecodeStrict(b, MaxLicense, &l); err != nil {
		return nil, ErrLicense
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return &l, nil
}

// VerifyLicense checks the signature of the store st and that the license
// belongs to this installation and its key.
func VerifyLicense(env sign.Envelope, st trust.Store, installation string, installKey []byte) (*License, error) {
	if err := sign.Verify(env, ed25519.PublicKey(st.Key), sign.PurposeLicense); err != nil {
		return nil, err
	}
	l, err := parseLicense(env.Payload)
	if err != nil {
		return nil, err
	}
	if l.Store != st.ID || l.Installation != installation || !bytes.Equal(l.InstallKey, installKey) {
		return nil, ErrLicense
	}
	return l, nil
}

// Validate checks r.
func (r *Release) Validate() error {
	if r.Seq == 0 || r.Issued <= 0 || !ValidVersion(r.Version) || len(r.Notes) > 10000 || !utf8.ValidString(r.Notes) ||
		len(r.Files) == 0 || len(r.Files) > 16 {
		return ErrRelease
	}
	seen := map[string]bool{}
	for _, f := range r.Files {
		arch := (f.Part == ReleaseWeb && f.Arch == "all") ||
			((f.Part == ReleaseAPI || f.Part == ReleaseController) && (f.Arch == "amd64" || f.Arch == "arm64"))
		key := f.Part + "/" + f.Arch
		if !arch || seen[key] || !validURL(f.URL) || f.Size <= 0 || f.Size > MaxPackage || len(f.SHA256) != 32 {
			return ErrRelease
		}
		seen[key] = true
	}
	return nil
}

func parseRelease(b []byte) (*Release, error) {
	var r Release
	if err := enc.DecodeStrict(b, sign.MaxPayload, &r); err != nil {
		return nil, ErrRelease
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

// VerifyRelease checks the signature of the release key and rejects releases older than lastSeq.
func VerifyRelease(env sign.Envelope, key ed25519.PublicKey, lastSeq uint64) (*Release, error) {
	if err := sign.Verify(env, key, sign.PurposeRelease); err != nil {
		return nil, err
	}
	r, err := parseRelease(env.Payload)
	if err != nil {
		return nil, err
	}
	if r.Seq < lastSeq {
		return nil, ErrRollback
	}
	return r, nil
}

// ValidTarget reports whether t names something that can be updated: an NGC
// part (trust.AutoWeb, AutoAPI, AutoController), the system packages
// (trust.AutoSystem) or an app.
func ValidTarget(t string) bool {
	return t == trust.AutoWeb || t == trust.AutoAPI || t == trust.AutoController || t == trust.AutoSystem ||
		(wire.ValidAppID(t) && !wire.CoreApp(t))
}

// ParseUpdateReport decodes and checks an update report.
func ParseUpdateReport(b []byte) (UpdateReport, error) {
	var r UpdateReport
	if err := enc.DecodeStrict(b, 4096, &r); err != nil {
		return UpdateReport{}, ErrUpdate
	}
	switch r.State {
	case UpdateAvailable, UpdateDownloaded, UpdateInstalling, UpdateInstalled, UpdateRolledBack, UpdateFailed, UpdateRejected:
	default:
		return UpdateReport{}, ErrUpdate
	}
	if !ValidTarget(r.Target) || (r.Version != "" && !ValidVersion(r.Version)) ||
		(r.Message != "" && !label(r.Message, wire.MaxMessageText)) {
		return UpdateReport{}, ErrUpdate
	}
	return r, nil
}

// Approved reports whether a standing approval in l lets updates for target,
// signed with key, install without a fresh signature of a user.
func Approved(l *trust.List, target string, key []byte) bool {
	for _, a := range l.Auto {
		if a.Target == target && (target == trust.AutoSystem || (len(key) > 0 && bytes.Equal(a.Key, key))) {
			return true
		}
	}
	return false
}
