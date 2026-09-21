// Package config defines the configuration files, paths and ports of NGC
// (contract V6). The ulh scripts write them, the parts read them.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/tunnel"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
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

	ServiceAPI        = "ngc-api.service"
	ServiceController = "ngc-controller.service"

	MaxConfig              = 1 << 20
	MaxField               = 256   // bounds every free-form string field below that carries no file-size limit of its own
	DefaultSessionLifetime = 43200 // 12 hours
	DefaultIdleTimeout     = 1800
	DefaultPodman          = "/usr/bin/podman"
)

// Database drivers.
const (
	DriverMariaDB = "mariadb"
	DriverSQLite  = "sqlite"
)

// ErrConfig reports a configuration that does not work.
var ErrConfig = errors.New("config: invalid configuration")

// Web is /etc/ngc/web.json; the PHP of the web interface reads it too.
type Web struct {
	APIs []string `json:"apis"` // https://host:port, Apache balances over them
	RP   proof.RP `json:"rp"`   // name and origins for passkeys; empty without a name
}

// DB tells an API which database to use.
type DB struct {
	Driver   string    `json:"driver"`
	Nodes    []string  `json:"nodes,omitempty"` // MariaDB: host:port of the Galera nodes
	Name     string    `json:"name,omitempty"`
	User     string    `json:"user,omitempty"`
	Password string    `json:"password,omitempty"`
	Path     string    `json:"path,omitempty"` // SQLite: file
	Key      enc.Bytes `json:"key,omitempty"`  // SQLite: 64 bytes for AES-256-XTS
}

// API is /etc/ngc/api.json. It holds keys, so the file belongs to root with mode 0600.
type API struct {
	Node            string    `json:"node"` // node ID in the trust list
	Listen          string    `json:"listen"`
	Cert            string    `json:"cert"`
	CertKey         string    `json:"certKey"`
	DB              DB        `json:"db"`
	NoiseKey        enc.Bytes `json:"noiseKey"`   // X25519 private key for the tunnel
	SignKey         enc.Bytes `json:"signKey"`    // Ed25519 private key for API certificates
	SecretKey       enc.Bytes `json:"secretKey"`  // AES-256 key for secrets in the database, shared by all APIs
	ReleaseKey      enc.Bytes `json:"releaseKey"` // public key of NGC releases
	StoreKey        enc.Bytes `json:"storeKey"`   // public key of the global store
	StoreURL        string    `json:"storeUrl"`
	SessionLifetime int64     `json:"sessionLifetime,omitempty"`
	IdleTimeout     int64     `json:"idleTimeout,omitempty"`
}

// APIRef is an API a controller dials.
type APIRef struct {
	URL string    `json:"url"`
	Key enc.Bytes `json:"key"` // its Noise key
}

// Controller is /etc/ngc/controller.json, also root and 0600.
type Controller struct {
	Node       string       `json:"node,omitempty"` // empty until an admin put it in the trust list
	Name       string       `json:"name"`
	NoiseKey   enc.Bytes    `json:"noiseKey"`
	APIs       []APIRef     `json:"apis"` // from the pairing code; later from the trust list
	Anchor     trust.Anchor `json:"anchor"`
	DB         string       `json:"db"`
	DBKey      enc.Bytes    `json:"dbKey"` // 64 bytes for AES-256-XTS
	ReleaseKey enc.Bytes    `json:"releaseKey"`
	Parts      []string     `json:"parts"` // tunnel.RoleWeb, RoleAPI, RoleDB, RoleServices
	Podman     string       `json:"podman,omitempty"`
	WebConfig  string       `json:"webConfig,omitempty"` // web server: path of web.json for the balancer
}

// decode strictly-JSON-decodes b into v, wrapping any failure as ErrConfig.
func decode(b []byte, v any) error {
	if err := enc.DecodeStrict(b, MaxConfig, v); err != nil {
		return ErrConfig
	}
	return nil
}

// parseWeb decodes and validates the payload of web.json. It is split out of
// LoadWeb so fuzzing can call it directly, in memory, without touching disk.
func parseWeb(b []byte) (*Web, error) {
	var w Web
	if err := decode(b, &w); err != nil {
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
	if err := decode(b, &a); err != nil {
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
	if err := decode(b, &c); err != nil {
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
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

// LoadAnchor reads trust-anchor.json.
func LoadAnchor(path string) (trust.Anchor, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return trust.Anchor{}, err
	}
	var a trust.Anchor
	if err := decode(b, &a); err != nil {
		return trust.Anchor{}, err
	}
	if a.Seq == 0 || len(a.Hash) != 32 {
		return trust.Anchor{}, ErrConfig
	}
	return a, nil
}

// SaveAnchor writes trust-anchor.json; the PHP of the web interface sends it
// to the browser, so it is readable for everyone on that server.
func SaveAnchor(path string, a trust.Anchor) error {
	if a.Seq == 0 || len(a.Hash) != 32 {
		return ErrConfig
	}
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Validate checks the web configuration.
func (w *Web) Validate() error {
	if len(w.APIs) == 0 || len(w.APIs) > 16 {
		return ErrConfig
	}
	for _, u := range w.APIs {
		if !strings.HasPrefix(u, "https://") || len(u) > MaxField {
			return ErrConfig
		}
	}
	for _, o := range w.RP.Origins {
		if !strings.HasPrefix(o, "https://") {
			return ErrConfig
		}
	}
	return nil
}

// Validate checks the API configuration.
func (a *API) Validate() error {
	if !wire.ValidName(a.Node) ||
		a.Listen == "" || len(a.Listen) > MaxField ||
		a.Cert == "" || len(a.Cert) > MaxField ||
		a.CertKey == "" || len(a.CertKey) > MaxField ||
		len(a.NoiseKey) != 32 || len(a.SignKey) != 32 || len(a.SecretKey) != 32 ||
		len(a.ReleaseKey) != 32 || len(a.StoreKey) != 32 ||
		!strings.HasPrefix(a.StoreURL, "https://") || len(a.StoreURL) > MaxField ||
		a.SessionLifetime < 300 || a.IdleTimeout < 60 || a.IdleTimeout > a.SessionLifetime {
		return ErrConfig
	}
	return a.DB.Validate()
}

// Validate checks the database part.
func (d *DB) Validate() error {
	switch d.Driver {
	case DriverMariaDB:
		if len(d.Nodes) == 0 || len(d.Nodes) > 16 ||
			d.Name == "" || len(d.Name) > MaxField ||
			d.User == "" || len(d.User) > MaxField ||
			d.Password == "" || len(d.Password) > MaxField ||
			d.Path != "" || len(d.Key) != 0 {
			return ErrConfig
		}
		for _, n := range d.Nodes {
			if !strings.Contains(n, ":") || len(n) > MaxField {
				return ErrConfig
			}
		}
	case DriverSQLite:
		if !strings.HasPrefix(d.Path, "/") || len(d.Key) != 64 || len(d.Nodes) != 0 ||
			d.Name != "" || d.User != "" || d.Password != "" {
			return ErrConfig
		}
	default:
		return ErrConfig
	}
	return nil
}

// Validate checks the controller configuration.
func (c *Controller) Validate() error {
	if (c.Node != "" && !wire.ValidName(c.Node)) || !wire.ValidName(c.Name) || len(c.NoiseKey) != 32 ||
		len(c.APIs) == 0 || len(c.APIs) > 16 || c.Anchor.Seq == 0 || len(c.Anchor.Hash) != 32 ||
		!strings.HasPrefix(c.DB, "/") || len(c.DB) > MaxField || len(c.DBKey) != 64 || len(c.ReleaseKey) != 32 ||
		len(c.Parts) == 0 || !strings.HasPrefix(c.Podman, "/") || len(c.Podman) > MaxField ||
		len(c.WebConfig) > MaxField {
		return ErrConfig
	}
	for _, a := range c.APIs {
		if !strings.HasPrefix(a.URL, "wss://") || len(a.URL) > MaxField || len(a.Key) != 32 {
			return ErrConfig
		}
	}
	for i, p := range c.Parts {
		if !slices.Contains([]string{tunnel.RoleWeb, tunnel.RoleAPI, tunnel.RoleDB, tunnel.RoleServices}, p) ||
			slices.Contains(c.Parts[:i], p) {
			return ErrConfig
		}
	}
	if slices.Contains(c.Parts, tunnel.RoleWeb) && c.WebConfig == "" {
		return ErrConfig
	}
	return nil
}
