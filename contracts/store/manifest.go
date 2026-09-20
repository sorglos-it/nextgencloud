// Package store defines app packages, store catalogs, licenses, NGC
// releases and the encryption of paid apps (contract V5).
package store

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/service"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// Names and limits of the manifest.
const (
	ManifestFile  = "manifest.json"
	MaxManifest   = 64 << 10
	RuntimePodman = "podman"
	NeedRoot      = "root"    // root inside the container
	NeedNetwork   = "network" // access to the network
	DefaultCPU    = 1000      // thousandths of a core
	DefaultMemory = 256       // MiB
)

// Manifest describes an app. It is manifest.json in the app package.
type Manifest struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Publisher   string    `json:"publisher"`
	Description string    `json:"description,omitempty"`
	Icon        string    `json:"icon,omitempty"` // path below ui/
	UI          *UI       `json:"ui,omitempty"`
	Actions     []Action  `json:"actions,omitempty"`
	Services    []Service `json:"services,omitempty"`
	Start       string    `json:"start,omitempty"`   // action the controller runs after every container start
	Runtime     string    `json:"runtime,omitempty"` // RuntimePodman; "lxc" is reserved for later
	Needs       []string  `json:"needs,omitempty"`   // NeedRoot, NeedNetwork; the admin must allow them
	CPU         int       `json:"cpu,omitempty"`     // thousandths of a core, default DefaultCPU
	Memory      int       `json:"memory,omitempty"`  // MiB, default DefaultMemory
	MinNGC      string    `json:"minNgc,omitempty"`
}

// UI is the browser part of the app.
type UI struct {
	Entry  string `json:"entry"` // path below ui/
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// Action is something a user may be allowed to do; it becomes a right.
type Action struct {
	Name     string `json:"name"`
	Title    string `json:"title"` // shown when an admin grants the right
	Service  string `json:"service"`
	Stream   bool   `json:"stream,omitempty"`  // stays open, no time limit
	Timeout  int    `json:"timeout,omitempty"` // seconds, default service.DefaultTimeout
	Critical bool   `json:"critical,omitempty"`
}

// Service is a program in the app's image.
type Service struct {
	Name    string   `json:"name"`
	Command []string `json:"command"` // absolute path and arguments, started without a shell
}

// ErrManifest reports an invalid manifest.
var ErrManifest = errors.New("store: invalid manifest")

// ParseManifest decodes and validates manifest.json.
func ParseManifest(b []byte) (*Manifest, error) {
	var m Manifest
	if err := enc.DecodeStrict(b, MaxManifest, &m); err != nil {
		return nil, ErrManifest
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Action returns the action with name.
func (m *Manifest) Action(name string) (Action, bool) {
	i := slices.IndexFunc(m.Actions, func(a Action) bool { return a.Name == name })
	if i < 0 {
		return Action{}, false
	}
	return m.Actions[i], true
}

// Validate checks m.
func (m *Manifest) Validate() error {
	if !wire.ValidAppID(m.ID) || wire.CoreApp(m.ID) || !label(m.Name, 64) || !label(m.Publisher, 64) ||
		!ValidVersion(m.Version) || len(m.Description) > 1000 || !utf8.ValidString(m.Description) ||
		(m.Icon != "" && !ValidPath(m.Icon)) || (m.MinNGC != "" && !ValidVersion(m.MinNGC)) {
		return ErrManifest
	}
	if m.UI == nil && len(m.Services) == 0 {
		return ErrManifest
	}
	if m.UI != nil && (!ValidPath(m.UI.Entry) || !size(m.UI.Width) || !size(m.UI.Height)) {
		return ErrManifest
	}
	services := map[string]bool{}
	for _, s := range m.Services {
		if !wire.ValidName(s.Name) || services[s.Name] || len(s.Command) == 0 || len(s.Command) > 32 ||
			!strings.HasPrefix(s.Command[0], "/") {
			return ErrManifest
		}
		for _, arg := range s.Command {
			if arg == "" || len(arg) > 256 || strings.ContainsRune(arg, 0) {
				return ErrManifest
			}
		}
		services[s.Name] = true
	}
	actions := map[string]bool{}
	for _, a := range m.Actions {
		if !wire.ValidName(a.Name) || actions[a.Name] || !label(a.Title, 100) || !services[a.Service] ||
			a.Timeout < 0 || a.Timeout > service.MaxTimeout || (a.Stream && a.Timeout != 0) {
			return ErrManifest
		}
		actions[a.Name] = true
	}
	if m.Start != "" {
		if a, ok := m.Action(m.Start); !ok || a.Stream {
			return ErrManifest
		}
	}
	if m.Runtime != "" && m.Runtime != RuntimePodman {
		return ErrManifest
	}
	if !validNeeds(m.Needs) {
		return ErrManifest
	}
	if m.CPU != 0 && (m.CPU < 100 || m.CPU > 64000) || m.Memory != 0 && (m.Memory < 16 || m.Memory > 65536) {
		return ErrManifest
	}
	return nil
}

func size(v int) bool { return v == 0 || (v >= 200 && v <= 4000) }

func validNeeds(needs []string) bool {
	for i, n := range needs {
		if (n != NeedRoot && n != NeedNetwork) || slices.Contains(needs[:i], n) {
			return false
		}
	}
	return true
}

// label allows printable UTF-8 text of 1 to max bytes.
func label(s string, max int) bool {
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

var (
	versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})$`)
	pathPattern    = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]*(/[A-Za-z0-9_-][A-Za-z0-9._-]*)*$`)
)

// ValidVersion reports whether s is MAJOR.MINOR.PATCH without leading zeros.
func ValidVersion(s string) bool { return versionPattern.MatchString(s) }

// ValidPath reports whether p is a safe relative path: no leading slash, no
// "..", no hidden files, only letters, digits and . _ - (at most 200 bytes).
func ValidPath(p string) bool { return len(p) <= 200 && pathPattern.MatchString(p) }

// Newer reports whether version a is higher than version b. Invalid versions are never newer.
func Newer(a, b string) bool {
	x, okA := parseVersion(a)
	y, okB := parseVersion(b)
	if !okA || !okB {
		return false
	}
	return slices.Compare(x[:], y[:]) > 0
}

func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	if !ValidVersion(s) {
		return v, false
	}
	for i, part := range strings.Split(s, ".") {
		v[i], _ = strconv.Atoi(part)
	}
	return v, true
}
