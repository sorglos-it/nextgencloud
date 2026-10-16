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
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbwait"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// lockName is the bucket of failed setup attempts; it is no login name, so it
// can never collide with one (a name has no '/').
const lockName = "setup/code"

// fault is httpapi.Fault with the one answer this handler may not give taken
// out. Fault reads the errors of package state and the store's own three;
// what is left it reads as the store not answering, which is E_UNREACHABLE --
// the contract's word for a database that cannot be reached
// (shared/README.md).
//
// This handler cannot stand behind that word. It writes as much as it reads:
// an installation ID and a key, version 1, and a row of the counting for every
// wrong attempt. A write that was refused is an answer and not a silence, and
// what refuses one -- a full disk, a read-only mount, a right that is missing,
// a constraint -- is not over when the database answers again. Two of them it
// does know by name, and both are broken rather than away: a key of the
// configuration that will not parse is mended in api.json, a lock row that
// does not read is mended in the database. For the rest it cannot tell which
// of the two it was, and a default is exactly what catches whatever nobody
// understood -- promising that it passes is a claim with nothing behind it.
//
// The imbalance settles it. An admin who hears 503 in the first setup is told
// to come back, and comes back to the same thing; his signed version 1 is
// gone and nothing said why. 500 costs him, in the passing case, the one try
// he makes anyway.
//
// What is given up with the arm was built for the other handler: there one
// damage is met at two steps depending on what a node holds in its cache, and
// E_UNREACHABLE is what makes both nodes answer alike (login/live.go).
//
// The split arises here too, and saying otherwise was wrong. Serve asks
// state.Trust, which answers out of the cache, so a node that ran the setup
// holds version 1 whether the row is still there or not. Built: node A runs
// the setup, the row of version 1 is dropped -- one statement -- and node B is
// started afterwards. A answers 404 and B answers 200 with a setup offer of
// its own.
//
// It is not a difference of status. The node that starts after the row is gone
// has the first setup open again, for the installation that stood: the
// installation ID is a row of its own and was not touched, so the offer names
// it, and a version 1 signed by a key nobody of that installation ever held is
// taken -- measured, 204, and the installation then runs on that stranger's
// list with his one admin in it. Whoever can drop a row can take the
// installation, and no answer this handler picks changes that. The line
// against it is the same one state.checkGap carries: a check that reads the
// database cannot hold against whoever writes it, and the anchor outside it
// (trust.Anchor, config.LoadAnchor) is in the contract and not wired here.
//
// What the arm costs is said too. The two it knows by name, errKey and
// lock.ErrLock, are not caught before it any more: 9b60c69 took both cases out
// of this function, so they reach httpapi.Fault like everything else, come
// back E_UNREACHABLE because it does not read them, and are turned into
// E_INTERNAL by the one line below. They are right about them -- but so is
// everything else the arm catches, a database that is genuinely away included,
// and this is the last step before the answer goes out. So nothing on this
// path can say 503 any more -- whoever one day wants a legitimate "come back
// later" here has to take the word back off this rule first, and with the two
// cases gone there is no finer place left to hang it on.
func fault(err error, from httpapi.Origin) wire.Code {
	if code := httpapi.Fault(err, from); code != wire.CodeUnreachable {
		return code
	}
	return wire.CodeInternal
}

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

// ErrNoCode reports a code file that is there and holds no code: a start cut
// short between making the file and writing it, or a file somebody else put
// under that name. The file is never written through -- the real name comes
// into being by a hard link onto a free one (EnsureCode) -- so the setup
// stays shut until it is removed by hand -- and whoever starts the API has to
// be told, or the admin sees nothing but 404 for good and never learns why.
//
// A file that cannot be read at all is not this: it may hold exactly the code
// the admin was given, and removing it over a fault of the rights or of the
// disk throws that code away. readCode keeps the two apart, and the error of
// the reading carries its own words.
var ErrNoCode = errors.New("setup: the code file holds no code")

// Handler serves both methods of session.PathSetup.
type Handler struct {
	S        *state.State
	CodeFile string // /var/lib/ngc/setup-code

	// The public keys of this API, derived once. Every derivation copies a
	// private key out of the configuration (config.Secret.Bytes), and nothing
	// ever clears those copies; the offer answers whoever asks, with no login
	// in front of it, so deriving them per request would let anybody strew as
	// many copies of the long-term keys through memory, core dump and swap
	// file as he sends requests.
	keys     sync.Once
	noisePub enc.Bytes
	signPub  enc.Bytes
	keysErr  error
}

// startContext is the context of the start, where no request carries one.
// It carries the deadline of the start (dbwait.Start) and not none at all:
// without one, a second program on the same database holds this call for the
// measured 80,6 s of a full retry chain and the start says nothing meanwhile.
// The caller cancels it, so nothing of the start outlives EnsureCode.
func startContext() (context.Context, context.CancelFunc) { return dbwait.Start(context.Background()) }

// EnsureCode makes the one-time code if the installation has no trust list
// yet, and reports whether a code now exists. With a trust list it removes a
// left-over code file: from then on the setup is closed. A file that is there
// and holds no code is ErrNoCode -- the setup stays shut, and the start says
// so instead of leaving the admin at a door that never opens again. One that
// cannot be read comes back with the error of the reading and no advice to
// remove anything: it may hold the code the admin was given.
func (h *Handler) EnsureCode() (made bool, err error) {
	ctx, cancel := startContext()
	defer cancel()
	if _, err := h.S.Trust(ctx); err == nil {
		return false, h.removeCode()
	} else if !errors.Is(err, state.ErrNoTrust) {
		return false, err
	}
	if _, err := h.S.EnsureInstallation(ctx); err != nil {
		return false, err
	}
	// What is there decides, and it is read before anything is made: a code
	// that stands is the code the admin was given, and ErrNoCode or a
	// reading that fails goes on to the start unchanged.
	//
	// It READS a name somebody may have laid out, and that is the one thing
	// this look adds over the O_EXCL it replaces. It never writes through it,
	// at most maxCodeFile+1 bytes are taken, and neither ErrNoCode nor the
	// error of the reading carries a byte of what stood there -- so a symlink
	// to somebody else's file buys a refused start and no word about its
	// contents. It is kept rather than dropped because the alternative is to
	// draw a code into a temporary file at EVERY start and remove it again,
	// which puts a secret on the disk that nobody was ever given.
	switch _, err := h.readCode(); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		// Nothing there yet; one is made below.
	default:
		return false, err
	}
	// The code is written under a name of our own and the real name comes
	// into being by a HARD LINK, so that name never exists half written.
	//
	// It was O_EXCL on the real name, and that is the race two ngc-api on one
	// data directory ran into -- measured over HTTP on 13.10.2026: O_EXCL
	// creates the name before the code is in it, the other start found it
	// there and EMPTY, read ErrNoCode out of that window and REFUSED TO
	// START, while nothing was broken. The admin was then sent to remove a
	// file that held the code of the start that won.
	//
	// What the O_EXCL bought is bought here too, and that is why it is a link
	// and not a rename. A name that is taken is still never written through
	// -- link fails with EEXIST, so a file or a symlink somebody laid out
	// cannot catch the code -- and of two starts at once still only one makes
	// one: the loser removes its own and reads the winner's.
	dir, name := filepath.Split(h.CodeFile)
	if dir == "" {
		dir = "."
	}
	// CreateTemp makes the file with mode 0600, so the code is never readable
	// for others, not even for a moment.
	f, err := os.CreateTemp(dir, "."+name+"-*")
	if err != nil {
		return false, err
	}
	tmp := f.Name()
	// The temporary name goes in every case: it carries a code nobody was
	// given, and one left lying is one more copy of a secret on the disk.
	defer os.Remove(tmp)
	defer f.Close()
	// The code is drawn once the file that takes it is open and only ours:
	// one that is never written is one that nobody could have been given.
	code, err := secrets.SetupCode()
	if err != nil {
		return false, err
	}
	if _, err := f.WriteString(code + "\n"); err != nil {
		return false, err
	}
	// Synced and closed BEFORE the link, so the name that appears carries the
	// code on the disk and not only in a buffer.
	if err := f.Sync(); err != nil {
		return false, err
	}
	if err := f.Close(); err != nil {
		return false, err
	}
	switch err := os.Link(tmp, h.CodeFile); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrExist):
		// Somebody was quicker, and what HE wrote is the code that counts.
		if _, err := h.readCode(); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, err
	}
}

// code reads the one-time code; a file that holds none and one that cannot be
// read both mean there is no setup. Which of the two it was matters only to
// the start, which says it (EnsureCode), never to an answer.
func (h *Handler) code() (string, bool) {
	c, err := h.readCode()
	return c, err == nil
}

// readCode reads the one-time code and says which way it failed: the file is
// not there (fs.ErrNotExist), it is there and holds no code (ErrNoCode), or
// it could not be read at all -- which is the fault of the rights or of the
// disk and no reason to remove a file that may hold the code the admin was
// given.
func (h *Handler) readCode() (string, error) {
	f, err := os.Open(h.CodeFile)
	if err != nil {
		return "", err
	}
	defer f.Close()
	// One byte over the limit is enough to tell a file that holds a code from
	// one that holds something else, and nothing larger is ever read.
	b, err := io.ReadAll(io.LimitReader(f, maxCodeFile+1))
	if err != nil {
		return "", fmt.Errorf("setup: the code file cannot be read: %w", err)
	}
	c := strings.TrimSpace(string(b))
	if len(b) > maxCodeFile || !session.ValidSetupCode(c) {
		return "", ErrNoCode
	}
	return c, nil
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
		httpapi.Fail(w, fault(err, httpapi.FromRows))
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
		httpapi.Fail(w, fault(err, httpapi.FromRows))
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
	// A wait that cannot be read is this installation's own fault, never the
	// caller's, and it is the same fault here as on every other path of this
	// handler: one error of the database, one status.
	wait, err := lock.Check(ctx, h.S.DB, lockName, now)
	if err != nil {
		httpapi.Fail(w, fault(err, httpapi.FromRows))
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
			httpapi.Fail(w, fault(err, httpapi.FromRows))
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
		httpapi.Fail(w, fault(err, httpapi.FromRows))
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
		httpapi.Fail(w, fault(err, httpapi.FromRequest))
		return
	}
	// The code has done its work and goes. What keeps the setup shut from here
	// on are the rows, which every request reads first; the file opens nothing
	// any more, and one that will not go is no reason to call a step undone
	// that stands. Saying otherwise would send the admin back to a door that
	// answers 404, with nothing to tell him his own version got through.
	//
	// It is said all the same: until the next start a code that counts lies
	// on the disk, and a database put back without a trust list in that
	// window opens the setup with the old code instead of a fresh one. The
	// name of the file is said and never what stands in it.
	if err := h.removeCode(); err != nil {
		slog.Error("the setup code file could not be removed", "file", h.CodeFile, "error", err)
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
// certificates. They are derived at the first call and held from then on, so
// no request of the open window copies a private key again.
func (h *Handler) nodeKeys() (enc.Bytes, enc.Bytes, error) {
	h.keys.Do(func() { h.noisePub, h.signPub, h.keysErr = publicKeys(h.S.Cfg) })
	return h.noisePub, h.signPub, h.keysErr
}

// publicKeys derives the two public keys from the private ones of cfg. The
// copies it takes are cleared once the public keys stand: they are the only
// copies of the long-term keys this package ever makes, and they last no
// longer than the two lines that need them.
func publicKeys(cfg *config.API) (enc.Bytes, enc.Bytes, error) {
	priv := cfg.NoiseKey.Bytes()
	defer clear(priv)
	noise, err := ecdh.X25519().NewPrivateKey(priv)
	if err != nil {
		return nil, nil, errKey
	}
	seed := cfg.SignKey.Bytes()
	defer clear(seed)
	if len(seed) != ed25519.SeedSize {
		return nil, nil, errKey
	}
	return enc.Bytes(noise.PublicKey().Bytes()), signPublic(ed25519.NewKeyFromSeed(seed)), nil
}

// signPublic returns the public key of sign and clears sign. An
// ed25519.PrivateKey is 64 bytes whose first 32 are the seed, so what is left
// lying is the signing key of this node itself; Public hands out a copy of
// its own, which outlives the clearing.
func signPublic(sign ed25519.PrivateKey) enc.Bytes {
	defer clear(sign)
	return enc.Bytes(sign.Public().(ed25519.PublicKey))
}
