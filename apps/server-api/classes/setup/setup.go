// Package setup serves the first setup: while the installation has no admin,
// GET session.PathSetup offers what version 1 of the trust list is built from,
// and POST takes that version. The one-time code never travels; both sides
// prove they hold it with a MAC over the exact bytes (session.OfferMAC,
// session.SetupMAC). The code lies in a file only root can read and never in
// the database, never in a log.
package setup

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	json "encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// lockName is the bucket of failed setup attempts; it is no login name, so it
// can never collide with one (a name has no '/').
const lockName = "setup/code"

// maxCodeFile bounds what the code file may hold before a byte of it is read:
// the canonical code is 23 characters, and a little white space around it is
// all that may stand besides. Anything larger is no code file of ours.
const maxCodeFile = 64

// storeName is the name of the global store, as the contract names it in
// session.SetupInfo.Store.
const storeName = "NGC Store"

// errKey reports a key of the configuration this API cannot use.
// config.API.Validate takes only keys of the right size, so it can mean
// nothing but a configuration that never went through it.
var errKey = errors.New("setup: key of the wrong size")

// Handler serves both methods of session.PathSetup.
type Handler struct {
	S        *state.State
	CodeFile string // /var/lib/ngc/setup-code
}

// startContext is the context of the start, where no request carries one.
func startContext() context.Context { return context.Background() }

// EnsureCode makes the one-time code if the installation has no trust list
// yet, and reports whether a code now exists. With a trust list it removes a
// left-over code file: from then on the setup is closed.
func (h *Handler) EnsureCode() (made bool, err error) {
	ctx := startContext()
	if _, err := h.S.Trust(ctx); err == nil {
		return false, h.removeCode()
	} else if !errors.Is(err, state.ErrNoTrust) {
		return false, err
	}
	if _, err := h.S.EnsureInstallation(ctx); err != nil {
		return false, err
	}
	// O_EXCL, and no look at the name before it: a name that is taken is
	// never written through, so a link somebody laid out cannot catch the
	// code, and of two starts at once only one makes one. What is there
	// stays, and what it holds decides whether a code exists.
	var f *os.File
	f, err = os.OpenFile(h.CodeFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		_, ok := h.code()
		return ok, nil
	}
	if err != nil {
		return false, err
	}
	// A file that was not written to the end holds no code anybody can use,
	// and O_EXCL would keep the next start from writing one: it goes at once.
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			made = false
			_ = h.removeCode()
		}
	}()
	// The mode is set before the first byte, so the code is never readable
	// for others, not even for a moment.
	if err = f.Chmod(0o600); err != nil {
		return false, err
	}
	// The code is drawn once the file that takes it is open and only ours:
	// one that is never written is one that nobody could have been given.
	var code string
	if code, err = secrets.SetupCode(); err != nil {
		return false, err
	}
	if _, err = f.WriteString(code + "\n"); err != nil {
		return false, err
	}
	if err = f.Sync(); err != nil {
		return false, err
	}
	return true, nil
}

// code reads the one-time code; an unreadable or malformed file means there
// is no setup.
func (h *Handler) code() (string, bool) {
	f, err := os.Open(h.CodeFile)
	if err != nil {
		return "", false
	}
	defer f.Close()
	// One byte over the limit is enough to tell a file that holds a code from
	// one that holds something else, and nothing larger is ever read.
	b, err := io.ReadAll(io.LimitReader(f, maxCodeFile+1))
	if err != nil || len(b) > maxCodeFile {
		return "", false
	}
	c := strings.TrimSpace(string(b))
	if !session.ValidSetupCode(c) {
		return "", false
	}
	return c, true
}

func (h *Handler) removeCode() error {
	if err := os.Remove(h.CodeFile); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Serve answers GET and POST of session.PathSetup.
func (h *Handler) Serve(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Once a trust list exists there is an admin, and the setup is over.
	if _, err := h.S.Trust(ctx); err == nil {
		httpapi.Fail(w, wire.CodeNotFound)
		return
	} else if !errors.Is(err, state.ErrNoTrust) {
		// The list was read out of our own rows, so nothing about it is the
		// caller's doing -- least of all a signature of his.
		httpapi.Fail(w, httpapi.Fault(err, httpapi.FromRows))
		return
	}
	code, ok := h.code()
	if !ok {
		httpapi.Fail(w, wire.CodeNotFound)
		return
	}
	// The router in front lets only these two through, and this answers the
	// same on its own: a handler that took every other method for a POST
	// would take one the day it is wired somewhere else.
	switch r.Method {
	case http.MethodGet:
		h.offer(w, r, code)
	case http.MethodPost:
		h.take(w, r, code)
	default:
		httpapi.Fail(w, wire.CodeProtocol)
	}
}

// offer answers GET: the SetupInfo as JSON and the MAC over exactly those
// bytes, so nobody between browser and API can offer other keys.
func (h *Handler) offer(w http.ResponseWriter, r *http.Request, code string) {
	info, err := h.info(r.Context())
	if err != nil {
		httpapi.Fail(w, httpapi.Fault(err, httpapi.FromRows))
		return
	}
	raw, err := json.Marshal(info)
	if err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return
	}
	body, err := json.Marshal(session.SetupOffer{Info: raw, MAC: session.OfferMAC(code, raw)})
	if err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return
	}
	httpapi.Answer(w, http.StatusOK, body)
}

// take answers POST: version 1 as the new admin signed it.
func (h *Handler) take(w http.ResponseWriter, r *http.Request, code string) {
	ctx := r.Context()
	now := h.S.Now()
	// The wait stands before the body is read: a caller who has to wait costs
	// this API one look at one row and nothing else.
	wait, err := lock.Check(ctx, h.S.DB, lockName, now)
	if err != nil {
		httpapi.Fail(w, lockFault(err))
		return
	}
	if wait > 0 {
		httpapi.FailRetry(w, wire.CodeLocked, wait)
		return
	}
	body, ok := httpapi.Body(w, r)
	if !ok {
		return
	}
	// fail counts one wrong attempt and answers it. Every wrong attempt
	// costs, whatever was wrong about it; and one that cannot be counted is
	// turned away instead, so that a database which does not count never
	// hands out free tries.
	fail := func(answer wire.Code) {
		if err := lock.Fail(ctx, h.S.DB, lockName, now); err != nil {
			httpapi.Fail(w, lockFault(err))
			return
		}
		httpapi.Fail(w, answer)
	}
	s, err := session.ParseSetup(body)
	if err != nil {
		fail(wire.CodeProtocol)
		return
	}
	info, err := h.info(ctx)
	if err != nil {
		// Our own rows and our own configuration: the caller brought none of
		// it, and counting his attempt for it would be wrong too.
		httpapi.Fail(w, httpapi.Fault(err, httpapi.FromRows))
		return
	}
	if _, err := session.VerifySetup(s, info, code); err != nil {
		fail(wire.CodeVerify)
		return
	}
	if _, err := h.S.AddVersion(ctx, s.Trust); err != nil {
		// A version 1 that is already there: somebody was quicker and the
		// setup is over. His signature was good, so E_VERIFY would be a lie.
		if errors.Is(err, state.ErrOldVersion) {
			httpapi.Fail(w, wire.CodeNotFound)
			return
		}
		// This version came from the caller, so what does not fit this
		// installation's chain is his. state.ErrDamaged is not: rows of our
		// own that contradict each other fall to E_INTERNAL, because no
		// version he can sign would mend them.
		httpapi.Fail(w, httpapi.Fault(err, httpapi.FromRequest))
		return
	}
	// The code has done its work, and it goes before the answer leaves: no
	// answer ever says the setup is done while a second one could still start.
	if err := h.removeCode(); err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return
	}
	_ = lock.Clear(ctx, h.S.DB, lockName)
	httpapi.NoContent(w)
}

// info is what version 1 is built from: this installation, this API as a node
// of the trust list, and the release key and the global store of the
// configuration. The offer and the check of what comes back both go through
// it, so the shell signs exactly what session.VerifySetup compares against.
func (h *Handler) info(ctx context.Context) (session.SetupInfo, error) {
	installation, err := h.S.EnsureInstallation(ctx)
	if err != nil {
		return session.SetupInfo{}, err
	}
	noise, signPub, err := h.nodeKeys()
	if err != nil {
		return session.SetupInfo{}, err
	}
	return session.SetupInfo{
		Installation: installation,
		Nodes: []trust.Node{{ID: h.S.Cfg.Node, Kind: trust.NodeAPI, Name: h.S.Cfg.Node,
			Noise: noise, Sign: signPub, URL: h.S.Cfg.URL, TLS: enc.Bytes(h.S.Pin)}},
		Now:        h.S.Now(),
		ReleaseKey: h.S.Cfg.ReleaseKey,
		Store: &trust.Store{ID: trust.StoreGlobal, Name: storeName,
			URL: h.S.Cfg.StoreURL, Key: h.S.Cfg.StoreKey},
	}, nil
}

// nodeKeys returns this API's public Noise and signing keys, which the offer
// names: a controller dials the Noise key, and the signing key checks API
// certificates.
func (h *Handler) nodeKeys() (enc.Bytes, enc.Bytes, error) {
	noise, err := ecdh.X25519().NewPrivateKey(h.S.Cfg.NoiseKey.Bytes())
	if err != nil {
		return nil, nil, errKey
	}
	seed := h.S.Cfg.SignKey.Bytes()
	if len(seed) != ed25519.SeedSize {
		return nil, nil, errKey
	}
	priv := ed25519.NewKeyFromSeed(seed)
	return enc.Bytes(noise.PublicKey().Bytes()), enc.Bytes(priv.Public().(ed25519.PublicKey)), nil
}

// lockFault tells a row of ours that is damaged from a database that does not
// answer: the first is this installation's own (E_INTERNAL), the second the
// database's (E_UNREACHABLE). Neither is ever E_VERIFY, because nothing the
// caller signed is at fault for either.
func lockFault(err error) wire.Code {
	if errors.Is(err, lock.ErrLock) {
		return wire.CodeInternal
	}
	return wire.CodeUnreachable
}
