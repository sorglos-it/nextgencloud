// Package store defines app packages, store catalogs, licenses, NGC
// releases and the encryption of paid apps (contract V5).
package store

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/service"
	"github.com/sorglos-it/nextgencloud/contracts/task"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// Names and limits of the manifest.
const (
	ManifestFile  = "manifest.json"
	MaxManifest   = 64 << 10
	MaxActions    = 64 // actions per manifest
	MaxServices   = 16 // services per manifest
	RuntimePodman = "podman"
	NeedRoot      = "root"    // root inside the container
	NeedNetwork   = "network" // access to the network
	DefaultCPU    = 1000      // thousandths of a core
	DefaultMemory = 256       // MiB

	maxCommand = 32 // path and arguments of a service
	maxNeeds   = 2  // NeedRoot and NeedNetwork, each at most once
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

// manifestBudget bounds a manifest while it is decoded (enc.Budget).
var manifestBudget = enc.Budget{Size: MaxManifest, Items: map[string]int{
	"actions":            MaxActions,
	"services":           MaxServices,
	"services/*/command": maxCommand,
	"needs":              maxNeeds,
}}

// ParseManifest decodes and validates manifest.json. Every failure is
// ErrManifest.
func ParseManifest(b []byte) (*Manifest, error) {
	var m Manifest
	if manifestBudget.Decode(b, &m) != nil {
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

// RuleFor returns the task.Rule for an action of an installed app: Root if the
// manifest needs NeedRoot (activation always grants all needs), Critical from
// the manifest's action whatever the params, Changes always. ErrManifest for
// an action the manifest does not have.
func RuleFor(m *Manifest, action string) (task.Rule, error) {
	if m == nil {
		return task.Rule{}, ErrManifest
	}
	a, ok := m.Action(action)
	if !ok {
		return task.Rule{}, ErrManifest
	}
	critical := a.Critical
	return task.Rule{Root: slices.Contains(m.Needs, NeedRoot), Changes: true,
		Critical: func(json.RawMessage) (bool, error) { return critical, nil }}, nil
}

// Validate checks m.
func (m *Manifest) Validate() error {
	if !wire.ValidAppID(m.ID) || wire.CoreApp(m.ID) || !ValidText(m.Name, 64) || !ValidText(m.Publisher, 64) ||
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
	if len(m.Actions) > MaxActions || len(m.Services) > MaxServices {
		return ErrManifest
	}
	services := map[string]bool{}
	for _, s := range m.Services {
		if !wire.ValidName(s.Name) || services[s.Name] || len(s.Command) == 0 || len(s.Command) > maxCommand ||
			len(s.Command[0]) < 2 || !strings.HasPrefix(s.Command[0], "/") {
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
		if !wire.ValidName(a.Name) || actions[a.Name] || !ValidText(a.Title, 100) || !services[a.Service] ||
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

// ValidText reports whether s is 1 to max bytes of printable UTF-8: every
// rune unicode.IsPrint (letters, marks, numbers, punctuation, symbols and the
// ASCII space). Names, titles and messages meant for people use it.
func ValidText(s string, max int) bool {
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
