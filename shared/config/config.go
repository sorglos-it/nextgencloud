// Package config defines the configuration files, paths and ports of NGC
// (contract V6). The ulh scripts write them, the parts read them.
package config

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Files, directories, services and ports.
const (
	Dir            = "/etc/ngc"
	WebFile        = "/etc/ngc/web.json"
	AnchorFile     = "/etc/ngc/trust-anchor.json"
	APIFile        = "/etc/ngc/api.json"
	ControllerFile = "/etc/ngc/controller.json"
	WebRoot        = "/var/www/ngc"
	BinDir         = "/usr/lib/ngc"
	DataDir        = "/var/lib/ngc"
	AppDataDir     = "/var/lib/ngc/apps"   // <app>/ per app, encrypted, mounted as /data
	StageDir       = "/var/lib/ngc/staged" // checked update files
	RunDir         = "/run/ngc"
	APIPort        = 7443

	// APITrustFile holds the certificates of the API servers that Apache's
	// proxy trusts, each checked against its pin (Web.APIs[i].TLS, later
	// trust.Node.TLS) before it is written: by ngc-web, then by the controller
	// with role web.
	APITrustFile = "/etc/ngc/tls/api-trust.pem"
	// BalancerFile is Apache's balancer for /api/ with its proxy TLS settings
	// (shared/ulh.md, "Lastverteilung in Apache"): written only by ngc-web at
	// install and afterwards by the controller with role web, both from web.json,
	// whose addresses pass trust.ValidWebURL; ngc-web's virtual host includes it.
	BalancerFile = "/etc/ngc/apache-apis.conf"
	MaxCA        = 16 << 10 // DB.CA

	ServiceAPI        = "ngc-api.service"
	ServiceController = "ngc-controller.service"

	MaxConfig = 1 << 20
	// MaxField bounds these strings: WebAPI.URL and each of Web.RP.Origins;
	// API.URL, Listen, Cert, CertKey and StoreURL; DB.Name, User, Password,
	// Path and each of DB.Nodes; APIRef.URL; Controller.DB, Podman and
	// WebConfig. The addresses among them keep the grammar of package trust:
	// ValidTunnelURL, ValidWebURL, ValidStoreURL and ValidOrigin carry this
	// same bound of 256 bytes themselves, ValidHostPort carries none (its
	// longest is 259 bytes, a host of 253 and a port of 5), so DB.Validate
	// bounds a node here.
	MaxField               = 256
	DefaultSessionLifetime = 43200 // 12 hours
	DefaultIdleTimeout     = 1800
	DefaultPodman          = "/usr/bin/podman"
)

// maxList is the most APIs web.json and controller.json name, and the most
// Galera nodes a DB names.
const maxList = 16

// maxRoles is the number of roles a server can carry (tunnel.RoleWeb,
// RoleAPI, RoleDB, RoleServices).
const maxRoles = 4

// maxCACerts is the most certificates DB.CA holds: the installation's CA,
// and while it is replaced its successor, with room to spare.
const maxCACerts = 4

// Database drivers.
const (
	DriverMariaDB = "mariadb"
	DriverSQLite  = "sqlite"
)

// ErrConfig reports a configuration that does not work.
var ErrConfig = errors.New("config: invalid configuration")

// Secret is a key of a configuration file, base64url there (enc.Bytes). Only
// the file shows it: fmt with any verb, log/slog and encoding/json show
// "[secret]", or "" while it is unset. The key sits inside a function, which
// even printing by reflection (a Secret in an unexported field, a verb that
// does not fit) shows only as an address. Bytes gives the key.
type Secret struct{ key func() []byte }

// NewSecret returns a Secret holding a copy of b.
func NewSecret(b []byte) Secret {
	if len(b) == 0 {
		return Secret{}
	}
	c := bytes.Clone(b)
	return Secret{func() []byte { return c }}
}

// Bytes returns a copy of the key, nil while s is unset. Changing the copy
// changes nothing in s, nor in any Secret that shares its key.
func (s Secret) Bytes() []byte {
	if s.key == nil {
		return nil
	}
	return bytes.Clone(s.key())
}

// IsZero reports whether s is unset (the omitzero option of encoding/json).
// NewSecret keeps no empty key, so an unset Secret has no function.
func (s Secret) IsZero() bool { return s.key == nil }

// String returns "[secret]", or "" while s is unset.
func (s Secret) String() string { return shown(!s.IsZero()) }

// Format prints String with every verb and flag of package fmt.
func (s Secret) Format(f fmt.State, verb rune) { fmt.Fprintf(f, fmt.FormatString(f, verb), s.String()) }

// LogValue implements slog.LogValuer.
func (s Secret) LogValue() slog.Value { return slog.StringValue(s.String()) }

// MarshalJSON encodes String; only marshalFile writes the key itself.
func (s Secret) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

// UnmarshalJSON decodes the key from base64url, as enc.Bytes does.
func (s *Secret) UnmarshalJSON(data []byte) error {
	var b enc.Bytes
	if err := b.UnmarshalJSON(data); err != nil {
		return err
	}
	*s = NewSecret(b)
	return nil
}

// SecretText is a password of a configuration file, a JSON string there. It
// shows itself nowhere else, as Secret does. Text gives the password.
type SecretText struct{ text func() string }

// NewSecretText returns a SecretText holding t.
func NewSecretText(t string) SecretText {
	if t == "" {
		return SecretText{}
	}
	return SecretText{func() string { return t }}
}

// Text returns the password, "" while s is unset.
func (s SecretText) Text() string {
	if s.text == nil {
		return ""
	}
	return s.text()
}

// IsZero reports whether s is unset (the omitzero option of encoding/json).
// NewSecretText keeps no empty password, so an unset one has no function.
func (s SecretText) IsZero() bool { return s.text == nil }

// String returns "[secret]", or "" while s is unset.
func (s SecretText) String() string { return shown(!s.IsZero()) }

// Format prints String with every verb and flag of package fmt.
func (s SecretText) Format(f fmt.State, verb rune) {
	fmt.Fprintf(f, fmt.FormatString(f, verb), s.String())
}

// LogValue implements slog.LogValuer.
func (s SecretText) LogValue() slog.Value { return slog.StringValue(s.String()) }

// MarshalJSON encodes String; only marshalFile writes the password itself.
func (s SecretText) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

// errRedacted reports the stand-in "[secret]" where a password belongs: the
// file was written by encoding/json, which leaves the secrets out, instead
// of by SaveController.
var errRedacted = errors.New("config: the stand-in for a secret instead of the password")

// UnmarshalJSON decodes the password from a JSON string. It refuses the
// stand-in "[secret]", so that a configuration that went through
// encoding/json fails to load instead of taking it for the password (a key
// fails anyway: the stand-in is no base64url).
func (s *SecretText) UnmarshalJSON(data []byte) error {
	var t string
	if err := jsonv2.Unmarshal(data, &t); err != nil {
		return err
	}
	if t == shown(true) {
		return errRedacted
	}
	*s = NewSecretText(t)
	return nil
}

// shown is what a Secret or SecretText shows of itself.
func shown(set bool) string {
	if set {
		return "[secret]"
	}
	return ""
}

// Web is /etc/ngc/web.json; the PHP of the web interface reads it too.
type Web struct {
	APIs []WebAPI `json:"apis"` // Apache balances over them
	RP   proof.RP `json:"rp"`   // ID and origins for passkeys: both set, or both empty without passkeys
}

// WebAPI is an API behind Apache.
type WebAPI struct {
	URL string    `json:"url"` // https://<host>:<port> (trust.ValidWebURL), which trust.WebURL makes from the node's tunnel address
	TLS enc.Bytes `json:"tls"` // pin of its TLS key (trust.TLSPin); Apache trusts only a certificate with it (APITrustFile)
}

// DB tells an API which database to use.
type DB struct {
	Driver   string     `json:"driver"`
	Nodes    []string   `json:"nodes,omitempty"` // MariaDB: host:port of the Galera nodes (trust.ValidHostPort)
	Name     string     `json:"name,omitempty"`
	User     string     `json:"user,omitempty"`
	Password SecretText `json:"password,omitzero"`
	Path     string     `json:"path,omitempty"` // SQLite: file
	Key      Secret     `json:"key,omitzero"`   // SQLite: 64 bytes for AES-256-XTS
	// CA is the certificate of the installation's database CA in PEM, which
	// ngc-db made (MariaDB only). A client verifies every node's certificate
	// against it and the node's host name (TLSConfig), never without.
	CA string `json:"ca,omitempty"`
}

// API is /etc/ngc/api.json. It holds keys, so the file belongs to root with mode 0600.
type API struct {
	Node string `json:"node"` // node ID in the trust list
	// URL is this API's tunnel address exactly as its node in the trust list
	// names it: wss://<host>:<port of Listen>/tunnel (trust.ValidTunnelURL).
	// Controllers dial it, and the API puts it into session.SetupInfo and its
	// pairing and join codes.
	URL       string `json:"url"`
	Listen    string `json:"listen"`
	Cert      string `json:"cert"`
	CertKey   string `json:"certKey"`
	DB        DB     `json:"db"`
	NoiseKey  Secret `json:"noiseKey"`  // X25519 private key for the tunnel
	SignKey   Secret `json:"signKey"`   // Ed25519 private key for API certificates
	SecretKey Secret `json:"secretKey"` // AES-256 key for secrets in the database, shared by all APIs
	// ChallengeKey keys the made-up login answer for a name no user has
	// (session.Challenge), 32 bytes, shared by all APIs of the installation
	// so that each of them answers one name alike. It is its own key and
	// not SecretKey: rotating the database key would draw every made-up
	// answer afresh while real ones stayed put, and whoever asked before
	// and after would have every name sorted.
	ChallengeKey Secret    `json:"challengeKey"`
	ReleaseKey   enc.Bytes `json:"releaseKey"` // public key of NGC releases
	// StoreKey and StoreURL name the global store (ID trust.StoreGlobal); the
	// API hands it to the shell in session.SetupInfo.Store. StoreURL follows
	// trust.ValidStoreURL, as trust.Store.URL does.
	StoreKey        enc.Bytes `json:"storeKey"`
	StoreURL        string    `json:"storeUrl"`
	SessionLifetime int64     `json:"sessionLifetime,omitempty"` // seconds, at most session.MaxLifetime
	IdleTimeout     int64     `json:"idleTimeout,omitempty"`
}

// APIRef is an API a controller dials.
type APIRef struct {
	URL string    `json:"url"` // its tunnel address (trust.ValidTunnelURL)
	Key enc.Bytes `json:"key"` // its Noise key
	// TLS is the pin of its TLS key (trust.TLSPin): from the pairing code,
	// later from the trust list; the controller dials with tunnel.PinnedTLS.
	TLS enc.Bytes `json:"tls"`
}

// Controller is /etc/ngc/controller.json, also root and 0600.
type Controller struct {
	Node       string       `json:"node,omitempty"` // empty until an admin put it in the trust list
	Name       string       `json:"name"`
	NoiseKey   Secret       `json:"noiseKey"`
	APIs       []APIRef     `json:"apis"` // from the pairing code; later from the trust list
	Anchor     trust.Anchor `json:"anchor"`
	DB         string       `json:"db"`
	DBKey      Secret       `json:"dbKey"` // 64 bytes for AES-256-XTS
	ReleaseKey enc.Bytes    `json:"releaseKey"`
	Roles      []string     `json:"roles"` // one to four of tunnel.RoleWeb, RoleAPI, RoleDB, RoleServices
	Podman     string       `json:"podman,omitempty"`
	WebConfig  string       `json:"webConfig,omitempty"` // web server: path of web.json for the balancer
	// Migrate is the database access for the migrations of an update: the
	// ngc_migrate account (MariaDB) or the SQLite file and key. Only a server
	// with tunnel.RoleDB has it.
	Migrate *DB `json:"migrate,omitempty"`
}

// Budgets of the configuration files (enc.Budget), each with its lists.
var (
	webBudget        = enc.Budget{Size: MaxConfig, Items: map[string]int{"apis": maxList, "rp/origins": trust.MaxOrigins}}
	apiBudget        = enc.Budget{Size: MaxConfig, Items: map[string]int{"db/nodes": maxList}}
	controllerBudget = enc.Budget{Size: MaxConfig, Items: map[string]int{"apis": maxList, "roles": maxRoles, "migrate/nodes": maxList}}
	anchorBudget     = enc.Budget{Size: MaxConfig}
)

// decode decodes b into v within budget, wrapping any failure as ErrConfig.
func decode(budget enc.Budget, b []byte, v any) error {
	if err := budget.Decode(b, v); err != nil {
		return ErrConfig
	}
	return nil
}

// parseWeb decodes and validates the payload of web.json. It is split out of
// LoadWeb so fuzzing can call it directly, in memory, without touching disk.
func parseWeb(b []byte) (*Web, error) {
	var w Web
	if err := decode(webBudget, b, &w); err != nil {
		return nil, err
	}
	return &w, w.Validate()
}

// LoadWeb reads web.json.
func LoadWeb(path string) (*Web, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseWeb(b)
}

// parseAPI decodes api.json's payload, fills in the defaults and validates.
func parseAPI(b []byte) (*API, error) {
	var a API
	if err := decode(apiBudget, b, &a); err != nil {
		return nil, err
	}
	if a.SessionLifetime == 0 {
		a.SessionLifetime = DefaultSessionLifetime
	}
	if a.IdleTimeout == 0 {
		a.IdleTimeout = DefaultIdleTimeout
	}
	return &a, a.Validate()
}

// LoadAPI reads api.json and fills in the defaults.
func LoadAPI(path string) (*API, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseAPI(b)
}

// parseController decodes controller.json's payload, fills in the defaults
// and validates.
func parseController(b []byte) (*Controller, error) {
	var c Controller
	if err := decode(controllerBudget, b, &c); err != nil {
		return nil, err
	}
	if c.Podman == "" {
		c.Podman = DefaultPodman
	}
	return &c, c.Validate()
}

// LoadController reads controller.json and fills in the defaults.
func LoadController(path string) (*Controller, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseController(b)
}

// SaveController writes controller.json; the controller uses it after
// pairing to remember its node ID.
func SaveController(path string, c *Controller) error {
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := marshalFile(c)
	if err != nil {
		return err
	}
	return writeAtomic(path, b, 0o600)
}

// SaveWeb writes web.json (mode 0644, atomically as SaveController does); the
// controller with role web uses it when the API servers in the trust list change.
func SaveWeb(path string, w *Web) error {
	if err := w.Validate(); err != nil {
		return err
	}
	b, err := marshalFile(w)
	if err != nil {
		return err
	}
	// The PHP of the web interface reads it, and it holds no secret.
	return writeAtomic(path, b, 0o644)
}

// fileMarshalers write the secrets themselves, which only a configuration
// file holds.
var fileMarshalers = jsonv2.JoinMarshalers(
	jsonv2.MarshalFunc(func(s Secret) ([]byte, error) { return enc.Bytes(s.Bytes()).MarshalJSON() }),
	jsonv2.MarshalFunc(func(s SecretText) ([]byte, error) { return json.Marshal(s.Text()) }),
)

// marshalFile encodes v for a configuration file: as encoding/json does,
// indented and with a final newline, but with the secrets themselves.
func marshalFile(v any) ([]byte, error) {
	b, err := jsonv2.Marshal(v, json.DefaultOptionsV1(), jsonv2.WithMarshalers(fileMarshalers))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return nil, err
	}
	return append(out.Bytes(), '\n'), nil
}

// parseAnchor decodes and checks the payload of trust-anchor.json.
func parseAnchor(b []byte) (trust.Anchor, error) {
	var a trust.Anchor
	if err := decode(anchorBudget, b, &a); err != nil {
		return trust.Anchor{}, err
	}
	if a.Seq == 0 || len(a.Hash) != 32 {
		return trust.Anchor{}, ErrConfig
	}
	return a, nil
}

// LoadAnchor reads trust-anchor.json.
func LoadAnchor(path string) (trust.Anchor, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return trust.Anchor{}, err
	}
	return parseAnchor(b)
}

// SaveAnchor writes trust-anchor.json; the PHP of the web interface sends it
// to the browser, so it is readable for everyone on that server.
func SaveAnchor(path string, a trust.Anchor) error {
	if a.Seq == 0 || len(a.Hash) != 32 {
		return ErrConfig
	}
	b, err := marshalFile(a)
	if err != nil {
		return err
	}
	return writeAtomic(path, b, 0o644)
}

// writeAtomic replaces path so that a crash leaves the old or the new file,
// never a torn one, and the file ends up with mode perm even if it existed.
func writeAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	// The mode is set before any byte is written, so a file that holds keys
	// is never readable for others, not even for a moment.
	if err = f.Chmod(perm); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	if err = os.Chmod(path, perm); err != nil {
		return err
	}
	// The rename lasts through a crash only once the directory is synced;
	// some systems cannot sync a directory, and the file is complete anyway.
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// Validate checks the web configuration.
func (w *Web) Validate() error {
	if len(w.APIs) == 0 || len(w.APIs) > maxList {
		return ErrConfig
	}
	for i, a := range w.APIs {
		// An address twice is a mistake: Apache would balance over it twice,
		// and with two pins it would trust two keys for one server.
		if !trust.ValidWebURL(a.URL) || len(a.TLS) != 32 ||
			slices.ContainsFunc(w.APIs[:i], func(b WebAPI) bool { return b.URL == a.URL }) {
			return ErrConfig
		}
	}
	if (w.RP.ID == "") != (len(w.RP.Origins) == 0) || len(w.RP.Origins) > trust.MaxOrigins ||
		(w.RP.ID != "" && !proof.ValidRPID(w.RP.ID)) {
		return ErrConfig
	}
	for _, o := range w.RP.Origins {
		if !trust.ValidOrigin(o) {
			return ErrConfig
		}
	}
	return nil
}

// Validate checks the API configuration.
func (a *API) Validate() error {
	if !wire.ValidName(a.Node) ||
		!trust.ValidTunnelURL(a.URL) ||
		a.Listen == "" || len(a.Listen) > MaxField ||
		a.Cert == "" || len(a.Cert) > MaxField ||
		a.CertKey == "" || len(a.CertKey) > MaxField ||
		len(a.NoiseKey.Bytes()) != 32 || len(a.SignKey.Bytes()) != 32 || len(a.SecretKey.Bytes()) != 32 ||
		len(a.ChallengeKey.Bytes()) != 32 ||
		len(a.ReleaseKey) != 32 || len(a.StoreKey) != 32 ||
		!trust.ValidStoreURL(a.StoreURL) ||
		a.SessionLifetime < 300 || a.SessionLifetime > session.MaxLifetime ||
		a.IdleTimeout < 60 || a.IdleTimeout > a.SessionLifetime {
		return ErrConfig
	}
	return a.DB.Validate()
}

// Validate checks the database part.
func (d *DB) Validate() error {
	switch d.Driver {
	case DriverMariaDB:
		if len(d.Nodes) == 0 || len(d.Nodes) > maxList ||
			d.Name == "" || len(d.Name) > MaxField ||
			d.User == "" || len(d.User) > MaxField ||
			d.Password.IsZero() || len(d.Password.Text()) > MaxField ||
			d.Path != "" || !d.Key.IsZero() {
			return ErrConfig
		}
		for _, n := range d.Nodes {
			// The node goes into the DSN of the driver, where parameters would
			// follow the host: it holds nothing but host and port. The length is
			// this package's bound, since ValidHostPort has none of its own.
			if !trust.ValidHostPort(n) || len(n) > MaxField {
				return ErrConfig
			}
		}
		if _, err := parseCA(d.CA); err != nil {
			return err
		}
	case DriverSQLite:
		if !strings.HasPrefix(d.Path, "/") || len(d.Path) > MaxField || len(d.Key.Bytes()) != 64 || len(d.Nodes) != 0 ||
			d.Name != "" || d.User != "" || !d.Password.IsZero() || d.CA != "" {
			return ErrConfig
		}
	default:
		return ErrConfig
	}
	return nil
}

// pemSpace is the white space DB.CA may hold around its PEM blocks.
const pemSpace = " \t\r\n"

// parseCA returns the certificates of DB.CA: at most MaxCA bytes, 1 to
// maxCACerts PEM blocks of type CERTIFICATE without headers and with nothing
// but white space around them, each a certificate x509.ParseCertificate
// accepts with BasicConstraintsValid and IsCA; ErrConfig otherwise. Validate
// and TLSConfig both read the field through it, so a client trusts exactly
// the certificates Validate accepted.
func parseCA(ca string) ([]*x509.Certificate, error) {
	if len(ca) > MaxCA {
		return nil, ErrConfig
	}
	var certs []*x509.Certificate
	rest := []byte(ca)
	for {
		rest = bytes.TrimLeft(rest, pemSpace)
		if len(rest) == 0 {
			break
		}
		if len(certs) == maxCACerts {
			return nil, ErrConfig
		}
		block, after := pem.Decode(rest)
		// pem.Decode skips whatever comes before a block it can read, even a
		// broken block. So the block must start right after the white space:
		// what it consumed holds no later BEGIN line (the body of a block it
		// returns is base64, which has no '-').
		if block == nil || bytes.LastIndex(rest[:len(rest)-len(after)], []byte("-----BEGIN")) != 0 ||
			block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, ErrConfig
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !cert.BasicConstraintsValid || !cert.IsCA {
			return nil, ErrConfig
		}
		certs = append(certs, cert)
		rest = after
	}
	if len(certs) == 0 {
		return nil, ErrConfig
	}
	return certs, nil
}

// TLSConfig returns the TLS client configuration for the database node
// serverName: the host of an entry of Nodes as net.SplitHostPort gives it, so
// an IPv6 address without its brackets. TLS 1.3, and the node's certificate
// must chain to CA and name serverName. ErrConfig without a CA (one Validate
// accepts) or for a name trust.ValidHost refuses (with the brackets put back
// for IPv6).
func (d DB) TLSConfig(serverName string) (*tls.Config, error) {
	certs, err := parseCA(d.CA)
	// Without a name crypto/tls refuses to connect, but a database driver may
	// put in a name of its own; the caller names the node instead, and that is
	// the host of an entry the grammar accepted.
	host := serverName
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if err != nil || !trust.ValidHost(host) {
		return nil, ErrConfig
	}
	pool := x509.NewCertPool()
	for _, c := range certs {
		pool.AddCert(c)
	}
	return &tls.Config{RootCAs: pool, ServerName: serverName, MinVersion: tls.VersionTLS13}, nil
}

// Validate checks the controller configuration.
func (c *Controller) Validate() error {
	if (c.Node != "" && !wire.ValidName(c.Node)) || !wire.ValidName(c.Name) || len(c.NoiseKey.Bytes()) != 32 ||
		len(c.APIs) == 0 || len(c.APIs) > maxList || c.Anchor.Seq == 0 || len(c.Anchor.Hash) != 32 ||
		!strings.HasPrefix(c.DB, "/") || len(c.DB) > MaxField || len(c.DBKey.Bytes()) != 64 || len(c.ReleaseKey) != 32 ||
		len(c.Roles) == 0 || len(c.Roles) > maxRoles || !strings.HasPrefix(c.Podman, "/") || len(c.Podman) > MaxField ||
		len(c.WebConfig) > MaxField {
		return ErrConfig
	}
	for _, a := range c.APIs {
		if !trust.ValidTunnelURL(a.URL) || len(a.Key) != 32 || len(a.TLS) != 32 {
			return ErrConfig
		}
	}
	for i, r := range c.Roles {
		if !tunnel.ValidRole(r) || slices.Contains(c.Roles[:i], r) {
			return ErrConfig
		}
	}
	if slices.Contains(c.Roles, tunnel.RoleWeb) && c.WebConfig == "" {
		return ErrConfig
	}
	// Only a server with the database role runs the migrations of an update,
	// so no other server holds that access.
	if slices.Contains(c.Roles, tunnel.RoleDB) != (c.Migrate != nil) {
		return ErrConfig
	}
	if c.Migrate != nil {
		return c.Migrate.Validate()
	}
	return nil
}

// LogValue shows d for log/slog, without its password and key.
func (d DB) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("driver", d.Driver),
		slog.Any("nodes", d.Nodes),
		slog.String("name", d.Name),
		slog.String("user", d.User),
		slog.Attr{Key: "password", Value: d.Password.LogValue()},
		slog.String("path", d.Path),
		slog.Attr{Key: "key", Value: d.Key.LogValue()},
		// Only whether it is set: the certificate is public, but long.
		slog.Bool("ca", d.CA != ""),
	)
}

// String shows d as LogValue does, so that fmt never prints its secrets.
func (d DB) String() string { return d.LogValue().String() }

// LogValue shows a for log/slog, without its private keys and the database
// password.
func (a API) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("node", a.Node),
		slog.String("url", a.URL),
		slog.String("listen", a.Listen),
		slog.String("cert", a.Cert),
		slog.String("certKey", a.CertKey),
		slog.Attr{Key: "db", Value: a.DB.LogValue()},
		slog.Attr{Key: "noiseKey", Value: a.NoiseKey.LogValue()},
		slog.Attr{Key: "signKey", Value: a.SignKey.LogValue()},
		slog.Attr{Key: "secretKey", Value: a.SecretKey.LogValue()},
		slog.Attr{Key: "challengeKey", Value: a.ChallengeKey.LogValue()},
		slog.String("releaseKey", enc.B64.EncodeToString(a.ReleaseKey)),
		slog.String("storeKey", enc.B64.EncodeToString(a.StoreKey)),
		slog.String("storeUrl", a.StoreURL),
		slog.Int64("sessionLifetime", a.SessionLifetime),
		slog.Int64("idleTimeout", a.IdleTimeout),
	)
}

// String shows a as LogValue does, so that fmt never prints its secrets.
func (a API) String() string { return a.LogValue().String() }

// LogValue shows c for log/slog, without its private keys and the migration
// password.
func (c Controller) LogValue() slog.Value {
	apis := make([]string, len(c.APIs))
	for i, a := range c.APIs {
		apis[i] = a.URL + " " + enc.B64.EncodeToString(a.Key) + " " + enc.B64.EncodeToString(a.TLS)
	}
	attrs := []slog.Attr{
		slog.String("node", c.Node),
		slog.String("name", c.Name),
		{Key: "noiseKey", Value: c.NoiseKey.LogValue()},
		slog.Any("apis", apis),
		slog.Group("anchor", slog.Uint64("seq", c.Anchor.Seq), slog.String("hash", enc.B64.EncodeToString(c.Anchor.Hash))),
		slog.String("db", c.DB),
		{Key: "dbKey", Value: c.DBKey.LogValue()},
		slog.String("releaseKey", enc.B64.EncodeToString(c.ReleaseKey)),
		slog.Any("roles", c.Roles),
		slog.String("podman", c.Podman),
		slog.String("webConfig", c.WebConfig),
	}
	if c.Migrate != nil {
		attrs = append(attrs, slog.Attr{Key: "migrate", Value: c.Migrate.LogValue()})
	}
	return slog.GroupValue(attrs...)
}

// String shows c as LogValue does, so that fmt never prints its secrets.
func (c Controller) String() string { return c.LogValue().String() }
