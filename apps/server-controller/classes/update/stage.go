// Laying an update ready: the one call that takes an update file from the API
// and puts it on the disk of this server, after it has proved that this
// installation signed it.
//
// The channel it runs on carries NO user (core.WithoutUser): there is no
// signed task behind it, and the params of the OPEN are not signed either. So
// nothing here decides anything out of those params. They name a target and a
// version; what the file is allowed to be - its size and its SHA-256 - comes
// out of the signed release or the signed catalogue alone, and a version in
// the OPEN that the signature does not confirm ends the call. That is the
// whole reason this action may run without a user: it installs nothing. The
// install is task 20 and asks for an approval of its own.
//
// The order below is the point of the file. Every refusal that CAN stand
// before the first byte reaches the disk does: signature, number, part,
// architecture, version, size and checksum all come out of the signed source
// first. What cannot is the checksum of the bytes themselves and how many
// there are, and those two are the reason the bytes go into a TEMPORARY file:
// it gets the name an installer looks for by a rename, after they held. So an
// update broken off in the middle leaves nothing that looks finished, and the
// row that says one lies ready is written after the rename - last of all.
package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	goruntime "runtime"
	"slices"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/access"
	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

var (
	// ErrVerify says the file is not what this installation signed: a
	// signature, a release or catalogue number, a part, an architecture, a
	// version, a size or a checksum that does not match. A caller maps it to
	// wire.CodeVerify (E_VERIFY).
	ErrVerify = errors.New("update: the update is not what this installation signed")
	// ErrForbidden says the target is not one this controller may lay an
	// update ready for - an app it does not run. A caller maps it to
	// wire.CodeForbidden (E_FORBIDDEN).
	ErrForbidden = errors.New("update: this update is not allowed")
	// ErrDisk says this node could not put the file down: no space, no
	// permission. Nothing is wrong with the update, so a caller must not
	// answer E_VERIFY to it. Its text names no path: the error travels to an
	// API.
	ErrDisk = errors.New("update: this node could not store the update")
	// ErrInternal says this program is wired wrong - a missing dependency, an
	// action this NGC does not know. None of it can be brought about by a
	// peer, so it is answered once and the same way.
	ErrInternal = errors.New("update: the updater is not wired up")
)

// diskError is ErrDisk with the cause behind it: Error() is the fixed text
// alone, while errors.Is(err, ErrDisk) holds and errors.As still finds the
// cause.
type diskError struct{ cause error }

func (e diskError) Error() string   { return ErrDisk.Error() }
func (e diskError) Unwrap() []error { return []error{ErrDisk, e.cause} }

// asDisk marks err as a failure of this node rather than of the update.
func asDisk(err error) error {
	if err == nil {
		return nil
	}
	return diskError{err}
}

// Apps is the row of an app whose update is laid ready: it says which store
// that app was activated from (apps/server-controller/classes/state.Store).
type Apps interface {
	App(ctx context.Context, id string) (dbapi.App, error)
}

// Updates is what this controller remembers about updates lying ready
// (state.Store). Staged answers what lies ready for a target - its Seq is the
// number of the release or catalogue that update was taken out of, the newest
// this server laid ready - and dbapi.ErrNotFound while nothing does.
type Updates interface {
	Staged(ctx context.Context, target string) (state.Staged, error)
	PutStaged(ctx context.Context, st state.Staged) error
}

// Local is the part of this controller's own store this package needs.
type Local interface {
	Apps
	Updates
}

// Reports tells the API how an update is getting on
// (apps/server-controller/classes/node.Reporter). It gives nothing back: a
// report that does not get out is logged there, and no refusal here depends
// on it.
type Reports interface {
	Update(u store.UpdateReport)
}

// Deps are what a Manager needs. Everything nil or empty in it is this program
// wired wrong and ends every call with ErrInternal.
type Deps struct {
	// State is this controller's own store: the row of an app and what lies
	// ready.
	State Local
	// List is the installation's own trust list, the only place a store key
	// may come from.
	List *trust.List
	// ReleaseKey is this installation's own release key
	// (config.Controller.ReleaseKey). Releases are verified with no other key.
	ReleaseKey []byte
	// StageDir is where the checked file ends up (config.StageDir).
	StageDir string
	// Arch is this server's architecture, as the files of a release name it
	// ("amd64", "arm64"). Empty is runtime.GOARCH. It is a field and not that
	// constant because a case has to be able to measure the other one.
	Arch string
	// Max bounds the file. Unlike an app package being activated, nothing is
	// unpacked here, so one file of n bytes costs n bytes of StageDir and not
	// twice that.
	Max int64
	// Report is the control channel to the API.
	Report Reports
	// Now is the clock, in seconds; it stamps what lies ready.
	Now func() int64
}

// Manager lays updates ready.
type Manager struct{ d Deps }

// New returns the manager of these dependencies.
func New(deps Deps) *Manager { return &Manager{d: deps} }

// emptyResult is the result of a stage: the contract gives it no fields, and
// an empty object is what a caller encodes.
var emptyResult = []byte("{}")

// Stage reads one update file from r and lays it ready. r carries the transfer
// header (core.ReadTransfer) and then the file, as the API sends them.
//
// The order:
//
//	1 the dependencies, then the params of the OPEN - nothing below trusts them
//	2 the header, and the bound on the file
//	3 the SIGNED source: release (NGC part) or catalogue (app), its number
//	  against the newest one seen, and the entry for this target
//	4 header against entry: size and checksum
//	-- only now does a byte reach the disk, and under a temporary name -----
//	5 the bytes, their length and their SHA-256 against that same entry
//	6 the rename: now the file has the name an installer looks for
//	7 the row: version, checksum, signer and number
//	8 the report on the control channel - store.UpdateDownloaded for a file
//	  that lies ready, store.UpdateRejected for every way out above
func (m *Manager) Stage(ctx context.Context, d access.Decision, r io.Reader) ([]byte, error) {
	if m == nil || m.d.State == nil || m.d.List == nil || m.d.StageDir == "" || m.d.Max <= 0 ||
		len(m.d.ReleaseKey) != ed25519.PublicKeySize || m.d.Report == nil || m.d.Now == nil || r == nil {
		return nil, ErrInternal
	}
	p, err := m.params(d)
	if err != nil {
		// No report: a report has to name a target that
		// store.ParseUpdateReport takes, and without the params there is
		// none to name.
		return nil, err
	}
	out, err := m.stage(ctx, p, r)
	if err != nil {
		m.d.Report.Update(store.UpdateReport{Target: p.Target, Version: p.Version,
			State: store.UpdateRejected, Message: message(err)})
		return nil, err
	}
	m.d.Report.Update(store.UpdateReport{Target: p.Target, Version: p.Version,
		State: store.UpdateDownloaded})
	return out, nil
}

// params reads the params of this channel. There is no signed task behind a
// stage (core.WithoutUser), so they come out of the OPEN - and they are read
// through the contract's own parser, like anything else from outside.
func (m *Manager) params(d access.Decision) (*core.StageParams, error) {
	a, ok := core.Lookup(core.AppStore, core.ServiceController, core.ActStage)
	if !ok {
		return nil, ErrInternal
	}
	// A decision for another action reaching this method is this program
	// wired wrong, not a peer's doing: the params below are read as this
	// action's, so they have to be this action's.
	if !d.Core || d.Action.App != a.App || d.Action.Service != a.Service || d.Action.Name != a.Name {
		return nil, ErrInternal
	}
	params, err := core.ParseParams(a, d.Params())
	if err != nil {
		return nil, errors.Join(ErrVerify, err)
	}
	p, ok := params.(*core.StageParams)
	if !ok {
		return nil, ErrInternal
	}
	// These three are MEASURED REDUNDANT and kept as a second floor: the
	// target becomes the name of a file that a program running as root is
	// handed later, so nothing below may build a path out of a name only
	// another package checked. Measured: no input makes one of them the
	// deciding wall. core.StageParams.Validate refuses all three already,
	// and even with that wall taken out of the contract, a target that is no
	// NGC part is sent to the catalogue branch and refused there - the
	// mutants M9b and M9c of the run in the report say so. They are a guard
	// against a later change of the contract, not a wall anything is known to
	// hit.
	if !store.ValidTarget(p.Target) || p.Target == trust.AutoSystem || !store.ValidVersion(p.Version) {
		return nil, ErrVerify
	}
	return p, nil
}

// source is what the checked release or catalogue says about this one file,
// gathered before anything is written.
type source struct {
	signer []byte
	seq    uint64
	size   int64
	sha    []byte
}

// ngcPart reports whether target is a part of NGC itself, which comes out of a
// signed release rather than out of a store's catalogue.
func ngcPart(target string) bool {
	return target == trust.AutoWeb || target == trust.AutoAPI || target == trust.AutoController
}

// stage is Stage without the reports, so that every way out of it is reported
// in one place.
func (m *Manager) stage(ctx context.Context, p *core.StageParams, r io.Reader) ([]byte, error) {
	tr, err := core.ReadTransfer(r)
	if err != nil {
		return nil, errors.Join(ErrVerify, err)
	}
	// The bound of the action (core.Action.MaxIn) is what the API may send at
	// all; this one is what this server is willing to write.
	if tr.Size > m.d.Max {
		return nil, ErrVerify
	}
	src, err := m.approved(ctx, p, tr)
	if err != nil {
		return nil, err
	}
	// The header is not signed, so it has to repeat what the signed entry
	// says. Without this line a swapped header would decide how many bytes
	// are read and which checksum they are held against.
	if tr.Size != src.size || !bytes.Equal(tr.SHA256, src.sha) {
		return nil, ErrVerify
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := m.put(ctx, p, src, r); err != nil {
		return nil, err
	}
	return emptyResult, nil
}

// approved checks where the file comes from: exactly one signed source, signed
// by a key of this installation, and newer than what this server has already
// seen for that target. Nothing here touches the disk.
func (m *Manager) approved(ctx context.Context, p *core.StageParams, tr core.Transfer) (source, error) {
	last, err := m.lastSeq(ctx, p.Target)
	if err != nil {
		return source{}, err
	}
	if ngcPart(p.Target) {
		return m.fromRelease(p, tr, last)
	}
	return m.fromCatalog(ctx, p, tr, last)
}

// lastSeq is the number of the newest release or catalogue this server has
// verified for that target, out of what lies ready; 0 while nothing does. A
// damaged row is an error and not a zero: a zero would let every number
// through again.
func (m *Manager) lastSeq(ctx context.Context, target string) (uint64, error) {
	st, err := m.d.State.Staged(ctx, target)
	switch {
	case err == nil:
		return st.Seq, nil
	case errors.Is(err, dbapi.ErrNotFound):
		return 0, nil
	default:
		return 0, err
	}
}

// fromRelease takes the entry of an NGC part out of a signed release. A
// release belongs to no store, so a header that names one is not a release.
func (m *Manager) fromRelease(p *core.StageParams, tr core.Transfer, last uint64) (source, error) {
	if tr.Release == nil || tr.Catalog != nil || tr.Store != "" {
		return source{}, ErrVerify
	}
	rel, err := store.VerifyRelease(*tr.Release, ed25519.PublicKey(m.d.ReleaseKey), last)
	if err != nil {
		// store.ErrRollback stays readable through this: a number older than
		// the newest one seen is a different thing from a broken signature,
		// and a caller may want to tell them apart.
		return source{}, errors.Join(ErrVerify, err)
	}
	// The release says which version this is; the OPEN only claimed it. One
	// of the two is signed.
	if rel.Version != p.Version {
		return source{}, ErrVerify
	}
	arch := m.arch()
	if p.Target == trust.AutoWeb {
		// The web interface has no machine code (store.File: part ngc-web is
		// arch "all").
		arch = "all"
	}
	// By part AND architecture, never by the checksum of the header: a
	// release lists one file per part and architecture, and picking the entry
	// by what the unsigned header carries would let an API hand this server
	// the signed file of the other architecture.
	i := slices.IndexFunc(rel.Files, func(f store.File) bool {
		return f.Part == p.Target && f.Arch == arch
	})
	if i < 0 {
		return source{}, ErrVerify
	}
	f := rel.Files[i]
	return source{signer: m.d.ReleaseKey, seq: rel.Seq, size: f.Size, sha: f.SHA256}, nil
}

// fromCatalog takes the entry of an app version out of the signed catalogue of
// the store that app was activated from.
func (m *Manager) fromCatalog(ctx context.Context, p *core.StageParams, tr core.Transfer,
	last uint64) (source, error) {
	if tr.Catalog == nil || tr.Release != nil {
		return source{}, ErrVerify
	}
	app, err := m.d.State.App(ctx, p.Target)
	switch {
	case err == nil:
	case errors.Is(err, dbapi.ErrNotFound):
		// Nothing is laid ready for an app this controller does not run: an
		// update is an update of something.
		return source{}, ErrForbidden
	default:
		return source{}, err
	}
	// The store comes out of this controller's own row of the app, never out
	// of the header; the header only says which store this catalogue claims
	// to be. An app an admin uploaded (core.LocalStore) never gets here: its
	// transfers carry neither a catalogue nor a store (core.Transfer.Store),
	// so the first line of this function already refused it
	// (TestStageRefusesAnUploadedApp).
	//
	// This line and the key lookup below are ONE wall in two halves, and
	// measured each half alone covers the other: with the identity held here
	// the lookup cannot be keyed on anything else, and with the lookup keyed
	// on the row the header's store decides nothing. So neither half can be
	// killed on its own (mutants M8 and M8b), while taking BOTH out stages
	// the catalogue of one store for an app of another (M8c, which dies).
	// Both stay: one of them is going to be edited some day.
	if app.Store == "" || app.Store != tr.Store {
		return source{}, ErrVerify
	}
	i := slices.IndexFunc(m.d.List.Stores, func(s trust.Store) bool { return s.ID == app.Store })
	if i < 0 {
		return source{}, ErrVerify
	}
	st := m.d.List.Stores[i]
	c, err := store.VerifyCatalog(*tr.Catalog, st, last)
	if err != nil {
		return source{}, errors.Join(ErrVerify, err)
	}
	a := slices.IndexFunc(c.Apps, func(e store.Entry) bool { return e.ID == p.Target })
	if a < 0 {
		return source{}, ErrVerify
	}
	v := slices.IndexFunc(c.Apps[a].Versions, func(e store.Package) bool { return e.Version == p.Version })
	if v < 0 {
		return source{}, ErrVerify
	}
	e := c.Apps[a].Versions[v]
	return source{signer: st.Key, seq: c.Seq, size: e.Size, sha: e.SHA256}, nil
}

// arch is this server's architecture as a release names it.
func (m *Manager) arch() string {
	if m.d.Arch != "" {
		return m.d.Arch
	}
	return goruntime.GOARCH
}

// markWriter tells a failed write to the disk apart from a failed read of the
// transfer: io.Copy returns one error for both, and the two must not get the
// same answer.
type markWriter struct {
	w      io.Writer
	failed bool
}

func (w *markWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if err != nil {
		w.failed = true
	}
	return n, err
}

// put writes the file, holds it against the checked entry, and only then gives
// it its name and remembers it.
//
// Nothing writes the final name directly. While the bytes run in, the file is
// a temporary one, and it becomes the staged file by a rename after the length
// and the checksum held - so an update broken off in the middle leaves no file
// that an installer would take for finished. The row comes last: a file
// without its row is a leftover the next stage overwrites, while a row without
// its file would name something to install that is not there.
func (m *Manager) put(ctx context.Context, p *core.StageParams, src source, r io.Reader) error {
	if err := os.MkdirAll(m.d.StageDir, 0o700); err != nil {
		return asDisk(err)
	}
	// Forward slashes and not filepath.Join: the path is held against
	// config.StageDir when it is written down (state.Staged), and that
	// comparison is spelled with slashes. p.Target cannot carry a separator
	// or a step upwards (store.ValidTarget: trust.Auto* or wire.ValidAppID).
	file := m.d.StageDir + "/" + p.Target
	tmp, err := os.CreateTemp(m.d.StageDir, "stage-")
	if err != nil {
		return asDisk(err)
	}
	done := false
	defer func() {
		if !done {
			os.Remove(tmp.Name())
		}
	}()

	h := sha256.New()
	mark := &markWriter{w: tmp}
	// The limit stops one byte PAST what the signed entry announces, so that
	// one extra byte still arrives and is counted: a file of size+1 is a
	// different file and has to be refused, and a limit of size exactly would
	// have read the first size bytes of it and called them the right ones.
	// Nothing beyond that one byte is read or written.
	n, cerr := io.Copy(io.MultiWriter(mark, h), io.LimitReader(r, src.size+1))
	if cerr = errors.Join(cerr, tmp.Close()); cerr != nil {
		if mark.failed {
			return asDisk(cerr)
		}
		// The transfer broke, so this is not the file that was approved.
		return errors.Join(ErrVerify, cerr)
	}
	// The length is MEASURED REDUNDANT beside the checksum - a file of
	// another length hashes differently, so the mutant that drops it stays
	// green (M11) - and it is kept because it says the size rule out loud and
	// is the only one of the two a reader can check without hashing anything.
	if n != src.size || !bytes.Equal(h.Sum(nil), src.sha) {
		return ErrVerify
	}
	// The mode of os.CreateTemp is 0600 already; it is set again because that
	// is the one thing about this file that matters later, and a reader of
	// this function should not have to know CreateTemp by heart. NOT measured
	// on this machine: the case that reads the mode skips loudly under
	// Windows, and the mutant that writes 0666 therefore stays green (M13).
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return asDisk(err)
	}
	if err := os.Rename(tmp.Name(), file); err != nil {
		return asDisk(err)
	}
	done = true

	st := state.Staged{Target: p.Target, Version: p.Version, SHA256: src.sha, Size: src.size,
		Signer: src.signer, Seq: src.seq, File: file, State: store.UpdateDownloaded, Time: m.d.Now()}
	if err := m.d.State.PutStaged(ctx, st); err != nil {
		// Nothing remembers this file, so nothing may find it either.
		os.Remove(file)
		return err
	}
	return nil
}

// message is the German line an admin reads about a refused update
// (store.UpdateReport.Message). It says which of the walls stopped it and
// nothing about this server's paths; the texts are bounded by
// wire.MaxMessageText, which store.ParseUpdateReport holds every report
// against (TestStageMessagesFitTheContract).
func message(err error) string {
	switch {
	case errors.Is(err, store.ErrRollback):
		return "Abgelehnt: Das Update ist älter als das zuletzt geprüfte."
	case errors.Is(err, ErrForbidden):
		return "Abgelehnt: Für dieses Ziel ist auf diesem Server nichts eingerichtet."
	case errors.Is(err, ErrVerify):
		return "Abgelehnt: Unterschrift, Version oder Prüfsumme passen nicht."
	case errors.Is(err, ErrDisk):
		return "Nicht abgelegt: Auf diesem Server fehlt Platz oder das Recht dazu."
	default:
		return "Nicht abgelegt: Dieser Server konnte das Update nicht prüfen."
	}
}
