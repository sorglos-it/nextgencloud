// Package core defines the actions of the built-in apps: what the shell
// asks the API for (contract V1) and the root tasks the API forwards to a
// controller (contract V2).
package core

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/session"
	"github.com/sorglos-it/nextgencloud/contracts/store"
	"github.com/sorglos-it/nextgencloud/contracts/task"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// Core apps, services, rights.
const (
	AppDesktop = "ngc.desktop"
	AppUsers   = "ngc.users"
	AppStore   = "ngc.store"

	ServiceAPI        = "api"
	ServiceController = "controller"

	RightUse   = "use"
	LocalStore = trust.StoreLocal // store ID of apps an admin uploaded
)

// Action names.
const (
	ActMe          = "me"
	ActApps        = "apps"
	ActUI          = "ui"
	ActTOTPBegin   = "totp-begin"
	ActTOTPConfirm = "totp-confirm"
	ActTOTPOff     = "totp-off"

	ActList      = "list"
	ActRights    = "rights"
	ActSetRights = "set-rights"
	ActTrust     = "trust"
	ActPairCode  = "pair-code"
	ActJoinCode  = "join-code"
	ActPending   = "pending"
	ActLog       = "log"

	ActSources    = "sources"
	ActCatalog    = "catalog"
	ActInstalled  = "installed"
	ActUpload     = "upload"
	ActLicense    = "license"
	ActInstallKey = "install-key"
	ActUpdates    = "updates"
	ActSetUpdate  = "set-update"

	ActActivate   = "activate"
	ActDeactivate = "deactivate"
	ActUninstall  = "uninstall"
	ActDeleteData = "delete-data"
	ActStage      = "stage"
	ActInstall    = "install"
)

// What the opener of a FlowIn channel may send (Action.MaxIn).
const (
	MaxTrustIn   = 6 << 20  // ngc.users/api/trust: one trust.Signed as JSON
	MaxLicenseIn = 16 << 10 // ngc.store/api/license: one signed license (sign.Envelope) as JSON
)

// maxTransferIn is what the API sends on a FlowTransfer channel: the length
// of the header, the header, the file.
const maxTransferIn = 4 + MaxTransferHeader + store.MaxPackage

// Flow is how data moves on the channel of an action.
type Flow string

const (
	// FlowCall: params in the OPEN (core API actions) or in the task. The
	// opener sends EOF at once. The result is all DATA up to the other side's
	// EOF, or up to the E2E end on app channels, at most wire.MaxCallResult.
	// Actions without a result answer {}.
	FlowCall Flow = "call"
	// FlowIn: the opener sends at most MaxIn bytes of DATA, then EOF; the
	// result is as for call.
	FlowIn Flow = "in"
	// FlowOut: the API sends data instead of a result; for ui, a tar that
	// passes store.WalkUI.
	FlowOut Flow = "out"
	// FlowTransfer: API -> controller, a Transfer header, then exactly size
	// bytes; the result is as for call.
	FlowTransfer Flow = "transfer"
)

// Params are the parameters of an action.
type Params interface{ Validate() error }

// Action describes a core action.
type Action struct {
	App, Service, Name string
	Admin              bool // only for the role admin
	Root               bool // root task on a controller: signed task required
	Changes            bool
	Flow               Flow
	Params             func() Params // nil: the action takes no parameters
	MaxIn              int64         // the most bytes of DATA the opener may send before its EOF; 0 for call and out
}

// newParams returns a constructor for the parameter type T.
func newParams[T any, PT interface {
	*T
	Params
}]() func() Params {
	return func() Params { return PT(new(T)) }
}

var actions = []Action{
	// App, Service, Name, Admin, Root, Changes, Flow, Params, MaxIn
	{AppDesktop, ServiceAPI, ActMe, false, false, false, FlowCall, nil, 0},
	{AppDesktop, ServiceAPI, ActApps, false, false, false, FlowCall, nil, 0},
	{AppDesktop, ServiceAPI, ActUI, false, false, false, FlowOut, newParams[AppParams](), 0},
	{AppDesktop, ServiceAPI, ActTOTPBegin, false, false, true, FlowCall, nil, 0},
	{AppDesktop, ServiceAPI, ActTOTPConfirm, false, false, true, FlowCall, newParams[CodeParams](), 0},
	{AppDesktop, ServiceAPI, ActTOTPOff, false, false, true, FlowCall, newParams[CodeParams](), 0},

	{AppUsers, ServiceAPI, ActList, true, false, false, FlowCall, nil, 0},
	{AppUsers, ServiceAPI, ActRights, true, false, false, FlowCall, nil, 0},
	{AppUsers, ServiceAPI, ActSetRights, true, false, true, FlowCall, newParams[RoleRights](), 0},
	{AppUsers, ServiceAPI, ActTrust, true, false, true, FlowIn, nil, MaxTrustIn},
	{AppUsers, ServiceAPI, ActPairCode, true, false, true, FlowCall, nil, 0},
	{AppUsers, ServiceAPI, ActJoinCode, true, false, true, FlowCall, nil, 0},
	{AppUsers, ServiceAPI, ActPending, true, false, false, FlowCall, nil, 0},
	{AppUsers, ServiceAPI, ActLog, true, false, false, FlowCall, newParams[LogQuery](), 0},

	// The API sends a session.TrustChain (session.ParseTrustChain); the
	// controller answers TrustResult with its newest verified seq, or ends
	// with E_VERIFY.
	{AppUsers, ServiceController, ActTrust, true, false, true, FlowIn, nil, session.MaxChain},

	{AppStore, ServiceAPI, ActSources, true, false, false, FlowCall, nil, 0},
	{AppStore, ServiceAPI, ActCatalog, true, false, false, FlowCall, newParams[StoreParams](), 0},
	{AppStore, ServiceAPI, ActInstalled, true, false, false, FlowCall, nil, 0},
	{AppStore, ServiceAPI, ActUpload, true, false, true, FlowIn, nil, store.MaxPackage},
	{AppStore, ServiceAPI, ActLicense, true, false, true, FlowIn, nil, MaxLicenseIn},
	{AppStore, ServiceAPI, ActInstallKey, true, false, false, FlowCall, nil, 0},
	{AppStore, ServiceAPI, ActUpdates, true, false, false, FlowCall, nil, 0},
	{AppStore, ServiceAPI, ActSetUpdate, true, false, true, FlowCall, newParams[UpdateSetting](), 0},

	{AppStore, ServiceController, ActActivate, true, true, true, FlowTransfer, newParams[ActivateParams](), maxTransferIn},
	{AppStore, ServiceController, ActDeactivate, true, true, true, FlowCall, newParams[AppParams](), 0},
	{AppStore, ServiceController, ActUninstall, true, true, true, FlowCall, newParams[UninstallParams](), 0},
	{AppStore, ServiceController, ActDeleteData, true, true, true, FlowCall, newParams[AppParams](), 0},
	{AppStore, ServiceController, ActStage, true, false, false, FlowTransfer, newParams[StageParams](), maxTransferIn},
	{AppStore, ServiceController, ActInstall, true, true, true, FlowCall, newParams[InstallParams](), 0},
}

// All returns every core action.
func All() []Action { return slices.Clone(actions) }

// Lookup finds a core action.
func Lookup(app, service, name string) (Action, bool) {
	for _, a := range actions {
		if a.App == app && a.Service == service && a.Name == name {
			return a, true
		}
	}
	return Action{}, false
}

// WithoutUser reports whether the API may open a channel for action a without
// a user (tunnel.Open without client certificate): ngc.users/controller/trust
// (the versions carry their own signatures), ngc.store/controller/stage
// (nothing is installed) and ngc.store/controller/install (see Unattended).
func WithoutUser(a Action) bool {
	if a.Service != ServiceController {
		return false
	}
	return (a.App == AppUsers && a.Name == ActTrust) || (a.App == AppStore && (a.Name == ActStage || a.Name == ActInstall))
}

// Unattended reports whether an install without a user may run: a standing
// approval in l covers p.Target for signer (the key that signed the staged
// file's release or catalog; nil for trust.AutoSystem), and p.Version is
// newer than installed (not checked for trust.AutoSystem; "" never is older).
func Unattended(p *InstallParams, l *trust.List, signer []byte, installed string) bool {
	if p == nil || l == nil || p.Validate() != nil || !store.Approved(l, p.Target, signer) {
		return false
	}
	return p.Target == trust.AutoSystem || store.Newer(p.Version, installed)
}

// RuleFor returns the task.Rule of a core action: Root and Changes from the
// table; Critical parses params with ParseParams (an error for invalid ones)
// and asks Critical. With nil params it answers Critical(a, nil) — Check only
// does that when no task came or the task has no params, and every
// param-dependent action is a root task, which never runs without a task. The
// executor parses the task's params again and refuses invalid ones.
func RuleFor(a Action) task.Rule {
	return task.Rule{Root: a.Root, Changes: a.Changes, Critical: func(params json.RawMessage) (bool, error) {
		if params == nil {
			return Critical(a, nil), nil
		}
		p, err := ParseParams(a, params)
		if err != nil {
			return false, err
		}
		return Critical(a, p), nil
	}}
}

var (
	ErrParams   = errors.New("core: invalid parameters")
	ErrTransfer = errors.New("core: invalid transfer")
)

// Limits of the parameters that Validate checks besides wire.MaxParams.
const (
	maxRights = 1000 // RoleRights.Rights
	maxGrants = 2    // ActivateParams.Grants: store.NeedRoot and store.NeedNetwork, each at most once
)

// paramsBudget bounds the params of every action while they are decoded
// (enc.Budget): only RoleRights and ActivateParams have an array.
var paramsBudget = enc.Budget{Size: wire.MaxParams, Items: map[string]int{"rights": maxRights, "grants": maxGrants}}

// ParseParams decodes the parameters of an action strictly. Actions without
// parameters accept nothing but an empty value or an empty object. Every
// failure is ErrParams.
func ParseParams(a Action, raw json.RawMessage) (Params, error) {
	if a.Params == nil {
		var none struct{}
		if len(raw) != 0 && paramsBudget.Decode(raw, &none) != nil {
			return nil, ErrParams
		}
		return nil, nil
	}
	v := a.Params()
	if paramsBudget.Decode(raw, v) != nil {
		return nil, ErrParams
	}
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return v, nil
}

// Critical reports whether an action needs re-confirmation at level
// "critical". Changes to the trust list are always signed with a user key
// and count as confirmed anyway.
func Critical(a Action, params Params) bool {
	switch v := params.(type) {
	case *ActivateParams:
		return len(v.Grants) > 0
	case *UninstallParams:
		return !v.KeepData
	}
	return a.App == AppStore && (a.Name == ActUpload || a.Name == ActDeleteData)
}

// ValidRight reports whether r is "<app>/use" or "<app>/<action>" for an app from a store.
func ValidRight(r string) bool {
	app, action, ok := strings.Cut(r, "/")
	return ok && wire.ValidAppID(app) && !wire.CoreApp(app) && (action == RightUse || wire.ValidName(action))
}

func validApp(app string) bool { return wire.ValidAppID(app) && !wire.CoreApp(app) }

// AppParams names an app.
type AppParams struct {
	App string `json:"app"`
}

func (p *AppParams) Validate() error { return check(validApp(p.App)) }

// CodeParams carries a TOTP code.
type CodeParams struct {
	Code string `json:"code"`
}

func (p *CodeParams) Validate() error { return check(session.ValidTOTP(p.Code)) }

// StoreParams names a store.
type StoreParams struct {
	Store string `json:"store"`
}

func (p *StoreParams) Validate() error { return check(wire.ValidName(p.Store)) }

// RoleRights are the rights of a role on apps (params of set-rights, result
// of rights). A role without rights leaves Rights out.
type RoleRights struct {
	Role   string   `json:"role"`
	Rights []string `json:"rights,omitempty"`
}

func (p *RoleRights) Validate() error {
	ok := wire.ValidName(p.Role) && len(p.Rights) <= maxRights
	for i, r := range p.Rights {
		ok = ok && ValidRight(r) && !slices.Contains(p.Rights[:i], r)
	}
	return check(ok)
}

// LogQuery selects log entries, newest first.
type LogQuery struct {
	Before int64  `json:"before,omitempty"` // entries older than this time
	Limit  int    `json:"limit"`
	User   string `json:"user,omitempty"`
}

func (p *LogQuery) Validate() error {
	return check(p.Before >= 0 && p.Limit >= 1 && p.Limit <= 500 && (p.User == "" || trust.ValidID(p.User)))
}

var window = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]-([01][0-9]|2[0-3]):[0-5][0-9]$`)

// UpdateSetting sets the update mode of a part or an app. Switching to
// store.ModeAuto also needs a standing approval in the trust list.
type UpdateSetting struct {
	Target string `json:"target"`
	Mode   string `json:"mode"`
	Window string `json:"window,omitempty"` // maintenance window, local time "HH:MM-HH:MM"
}

func (p *UpdateSetting) Validate() error {
	mode := p.Mode == store.ModeAuto || p.Mode == store.ModeSemi || p.Mode == store.ModeNotify
	return check(store.ValidTarget(p.Target) && mode && (p.Window == "" || window.MatchString(p.Window)))
}

// ActivateParams is the root task that installs and starts an app on a
// controller. Grants must equal the manifest's Needs as a set; the controller
// refuses activation otherwise (E_FORBIDDEN).
type ActivateParams struct {
	App     string    `json:"app"`
	Version string    `json:"version"`
	SHA256  enc.Bytes `json:"sha256"`           // of the package file as the store delivers it
	Store   string    `json:"store"`            // LocalStore for uploaded apps; Transfer.Store stays empty for those instead.
	Grants  []string  `json:"grants,omitempty"` // store.NeedRoot, store.NeedNetwork the admin allows
}

func (p *ActivateParams) Validate() error {
	ok := validApp(p.App) && store.ValidVersion(p.Version) && len(p.SHA256) == 32 && wire.ValidName(p.Store)
	for i, g := range p.Grants {
		ok = ok && (g == store.NeedRoot || g == store.NeedNetwork) && !slices.Contains(p.Grants[:i], g)
	}
	return check(ok)
}

// UninstallParams removes an app; KeepData keeps its encrypted data for a reinstall.
type UninstallParams struct {
	App      string `json:"app"`
	KeepData bool   `json:"keepData,omitempty"`
}

func (p *UninstallParams) Validate() error { return check(validApp(p.App)) }

// StageParams announces an update file the API sends ahead of installing it.
type StageParams struct {
	Target  string `json:"target"`
	Version string `json:"version"`
}

func (p *StageParams) Validate() error {
	return check(store.ValidTarget(p.Target) && p.Target != trust.AutoSystem && store.ValidVersion(p.Version))
}

// InstallParams installs a staged update; for trust.AutoSystem it runs apt.
type InstallParams struct {
	Target  string    `json:"target"`
	Version string    `json:"version,omitempty"`
	SHA256  enc.Bytes `json:"sha256,omitempty"`
}

func (p *InstallParams) Validate() error {
	if p.Target == trust.AutoSystem {
		return check(p.Version == "" && len(p.SHA256) == 0)
	}
	return check(store.ValidTarget(p.Target) && store.ValidVersion(p.Version) && len(p.SHA256) == 32)
}

func check(ok bool) error {
	if !ok {
		return ErrParams
	}
	return nil
}
