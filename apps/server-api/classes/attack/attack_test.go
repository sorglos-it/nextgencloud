package attack

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// termApp is the app a root task of these cases names. Nothing has to be
// installed for it: ngc.store/controller/deactivate is a core action of
// service controller, so the API forwards it without reading an app row.
const termApp = "org.sorglos.terminal"

// deactivateOpen is the channel every task attack is measured on, and it was
// picked for one reason: it is a ROOT action of service controller, so the way
// to it runs through task.Check and ends in package relay. No tunnel is open
// in a test, so a channel that got all the way through ends with
// E_UNREACHABLE -- a code no wall before the relay answers. That makes "the
// way was open" measurable instead of asserted.
func deactivateOpen(env *sign.Envelope) wire.Open {
	return wire.Open{App: core.AppStore, Service: core.ServiceController,
		Action: core.ActDeactivate, Task: env}
}

// deactivateTask is the task of that channel. fill makes the one-time ID,
// because two attacks on one session must not spend each other's.
func deactivateTask(t *testing.T, app string, fill byte) task.Task {
	t.Helper()
	params, err := json.Marshal(core.AppParams{App: app})
	if err != nil {
		t.Fatalf("params: %v", err)
	}
	return task.Task{ID: enc.Bytes(bytes.Repeat([]byte{fill}, 16)), Node: ctlNode,
		App: core.AppStore, Service: core.ServiceController, Action: core.ActDeactivate,
		Params: params}
}

// TestInjectedSQLDoesNothing sends SQL into every name the API takes from a
// caller: the login name, the user filter of the log, and the role of
// set-rights. None of them reaches a query -- the API speaks only through
// dbapi -- and each has to keep its grammar before anything reads a row.
//
// The reachability check is per name, and it is the same request with a name
// that keeps the grammar: it has to be ANSWERED, not refused. Without it a
// green case would also pass against an API that refuses every name there is.
func TestInjectedSQLDoesNothing(t *testing.T) {
	a := newAPI(t)
	const inject = `admin' OR 1=1; DROP TABLE users;--`

	t.Run("login name", func(t *testing.T) {
		res := a.post(t, session.PathLoginStart, `{"name":`+quote(t, inject)+`}`)
		if res.Code == http.StatusOK {
			t.Fatalf("the injected login name was answered %d %s", res.Code, res.Body)
		}
		if got := codeOf(t, res); got != wire.CodeProtocol {
			t.Fatalf("the injected login name answered %q, want %q", got, wire.CodeProtocol)
		}
		// Reachability: the same request with the admin's own name goes
		// through, so the refusal above is about the name and not about the
		// path, the method or the body.
		if res := a.post(t, session.PathLoginStart,
			`{"name":`+quote(t, a.AdminName)+`}`); res.Code != http.StatusOK {
			t.Fatalf("the clean login name answered %d %s", res.Code, res.Body)
		}
		// And the user rows are untouched: still the two of the trust list.
		users, err := a.DB.UserList(ctx)
		if err != nil {
			t.Fatalf("users: %v", err)
		}
		if len(users) != len(a.List.Users) {
			t.Fatalf("%d user rows, want %d", len(users), len(a.List.Users))
		}
	})

	t.Run("log filter", func(t *testing.T) {
		params, err := json.Marshal(core.LogQuery{Limit: 10, User: inject})
		if err != nil {
			t.Fatalf("params: %v", err)
		}
		got := a.callCode(t, wire.Open{App: core.AppUsers, Service: core.ServiceAPI,
			Action: core.ActLog, Params: params})
		if got != wire.CodeProtocol {
			t.Fatalf("the injected log filter answered %q, want %q", got, wire.CodeProtocol)
		}
		// Reachability: the same channel with the admin's own ID answers a
		// result, so what refused above was the parameter and not an earlier
		// wall.
		clean, err := json.Marshal(core.LogQuery{Limit: 10, User: a.AdminID})
		if err != nil {
			t.Fatalf("params: %v", err)
		}
		if body := a.call(t, wire.Open{App: core.AppUsers, Service: core.ServiceAPI,
			Action: core.ActLog, Params: clean}); len(body) == 0 {
			t.Fatal("the clean log filter answered an empty result")
		}
	})

	t.Run("role name", func(t *testing.T) {
		params, err := json.Marshal(core.RoleRights{Role: inject,
			Rights: []string{termApp + "/use"}})
		if err != nil {
			t.Fatalf("params: %v", err)
		}
		got := a.callCode(t, wire.Open{App: core.AppUsers, Service: core.ServiceAPI,
			Action: core.ActSetRights, Params: params})
		if got != wire.CodeProtocol {
			t.Fatalf("the injected role answered %q, want %q", got, wire.CodeProtocol)
		}
		// Reachability: the same channel with the role of the trust list is
		// taken, which also shows the rights row is writable -- so what
		// refused the injection was the role and not the writing.
		clean, err := json.Marshal(core.RoleRights{Role: trust.RoleAdmin,
			Rights: []string{termApp + "/use"}})
		if err != nil {
			t.Fatalf("params: %v", err)
		}
		a.call(t, wire.Open{App: core.AppUsers, Service: core.ServiceAPI,
			Action: core.ActSetRights, Params: clean})
		// And the rows say the same: the admin's row is there -- so the
		// writing works and this half measures something -- and no row stands
		// under the injected name.
		if _, err := a.DB.BlobGet(ctx, dbapi.BlobRights, trust.RoleAdmin); err != nil {
			t.Fatalf("the rights row of the admin role is not there: %v", err)
		}
		if _, err := a.DB.BlobGet(ctx, dbapi.BlobRights, inject); !errors.Is(err, dbapi.ErrNotFound) {
			t.Fatalf("a rights row under the injected name answered %v, want %v",
				err, dbapi.ErrNotFound)
		}
	})
}

// TestForgedSignaturesAreRefused signs with a key the installation never
// trusted, once per thing the API verifies: the trust list, the task of a
// channel, the catalog of a store, a licence and a release.
//
// The vectors of the last three are the ones shared/testdata/store.json
// carries, with the store, the release key and the installation that file
// names -- not the fixture's, because then the sound documents beside the
// rejects could not verify, and a verifier that refuses EVERYTHING would read
// as a verifier that caught every forgery.
func TestForgedSignaturesAreRefused(t *testing.T) {
	a := newAPI(t)
	stranger := trusttest.NewSigner(0x66)

	t.Run("trust list", func(t *testing.T) {
		next := trusttest.Next(t, a.Admin.Sign(t, a.List))
		if _, err := a.Srv.State.AddVersion(ctx, stranger.Sign(t, next)); err == nil {
			t.Fatal("a trust list signed by a stranger was taken")
		}
		// Reachability: the same version signed by the admin of the list is
		// taken, so what refused above was the signer and not the version.
		if _, err := a.Srv.State.AddVersion(ctx, a.Admin.Sign(t, next)); err != nil {
			t.Fatalf("the admin's own version 2 was refused: %v", err)
		}
	})

	t.Run("task of a channel", func(t *testing.T) {
		forged := a.taskSignedBy(t, deactivateTask(t, termApp, 0x41), stranger)
		if got := a.callCode(t, deactivateOpen(forged)); got != wire.CodeVerify {
			t.Fatalf("the forged task answered %q, want %q", got, wire.CodeVerify)
		}
		// Reachability: the same channel with the admin's own signature goes
		// all the way to package relay, which ends it with E_UNREACHABLE
		// because no controller is paired. No wall before the relay answers
		// that code.
		good := a.Task(t, deactivateTask(t, termApp, 0x42))
		if got := a.callCode(t, deactivateOpen(good)); got != wire.CodeUnreachable {
			t.Fatalf("the signed task answered %q, want %q", got, wire.CodeUnreachable)
		}
	})

	t.Run("catalog, licence and release", func(t *testing.T) {
		v := loadVectors(t)
		st := v.vectorStore()
		// Reachability FIRST, and for each of the three verifiers: the sound
		// documents of the same file go through. This half is what keeps the
		// three loops below from reading as a success when they are not:
		// handed a store key the vectors were never signed with, every
		// verifier refuses everything, and all three loops stay green while
		// measuring nothing. That is why nothing here is the fixture's key.
		if _, err := store.VerifyCatalog(v.Catalog, st, v.CatalogLastSeq); err != nil {
			t.Fatalf("the sound catalog of the vector file was refused: %v", err)
		}
		if _, err := store.VerifyLicense(v.License, st, v.Installation, v.LicenseApp,
			v.InstallKey); err != nil {
			t.Fatalf("the sound licence of the vector file was refused: %v", err)
		}
		if _, err := store.VerifyRelease(v.Release, v.releasePub(), v.ReleaseLastSeq); err != nil {
			t.Fatalf("the sound release of the vector file was refused: %v", err)
		}
		forged := 0
		for _, c := range v.CatalogRejects {
			if _, err := store.VerifyCatalog(c.Catalog, st, c.LastSeq); err == nil {
				t.Errorf("catalog vector %q was taken", c.Name)
			}
			forged++
		}
		for _, c := range v.LicenseRejects {
			if _, err := store.VerifyLicense(c.License, st, v.Installation, v.LicenseApp,
				v.InstallKey); err == nil {
				t.Errorf("licence vector %q was taken", c.Name)
			}
			forged++
		}
		for _, c := range v.ReleaseRejects {
			if _, err := store.VerifyRelease(c.Release, v.releasePub(), c.LastSeq); err == nil {
				t.Errorf("release vector %q was taken", c.Name)
			}
			forged++
		}
		t.Logf("%d rejected vectors refused, 3 sound ones taken", forged)
	})
}

// TestReplayedMessagesDoNothing sends the same proof twice: the nonce of a
// login, the ID of a signed task, the secret of a pairing and the secret of a
// join. The first of each pair is the reachability check -- it has to work,
// and only then does the second one mean anything.
func TestReplayedMessagesDoNothing(t *testing.T) {
	a := newAPI(t)

	t.Run("login nonce", func(t *testing.T) {
		res := a.post(t, session.PathLoginStart, `{"name":`+quote(t, a.AdminName)+`}`)
		if res.Code != http.StatusOK {
			t.Fatalf("login/start: %d %s", res.Code, res.Body)
		}
		var c session.LoginChallenge
		if err := json.Unmarshal(res.Body.Bytes(), &c); err != nil {
			t.Fatalf("challenge: %v", err)
		}
		cert := a.certBytes(t, c)
		body, err := json.Marshal(session.LoginFinish{Cert: enc.Bytes(cert),
			Proof: a.Admin.Prove(sign.PurposeClientCert, cert)})
		if err != nil {
			t.Fatalf("finish body: %v", err)
		}
		// Reachability: the first use of the nonce makes a session.
		if first := a.post(t, session.PathLoginFinish, string(body)); first.Code != http.StatusOK {
			t.Fatalf("the first login/finish answered %d %s", first.Code, first.Body)
		}
		// The replay: the very same bytes again.
		if second := a.post(t, session.PathLoginFinish, string(body)); second.Code == http.StatusOK {
			t.Fatal("the same login nonce made a second session")
		}
	})

	t.Run("task ID", func(t *testing.T) {
		tk := deactivateTask(t, termApp, 0x51)
		// Reachability: the first use of the ID reaches the relay.
		if got := a.callCode(t, deactivateOpen(a.Task(t, tk))); got != wire.CodeUnreachable {
			t.Fatalf("the first use answered %q, want %q", got, wire.CodeUnreachable)
		}
		// The replay. The task is signed again over the same ID, so the
		// signature is sound and only the one-time value can refuse it.
		if got := a.callCode(t, deactivateOpen(a.Task(t, tk))); got != wire.CodeVerify {
			t.Fatalf("the replayed task ID answered %q, want %q", got, wire.CodeVerify)
		}
	})

	t.Run("pairing secret", func(t *testing.T) {
		code := a.pairCode(t)
		ctrl, _ := trusttest.NodeKeys(20)
		body := pairBody(t, code, ctrl.Public, code.Secret)
		// Reachability: the first pairing is taken and listed.
		if res := a.postBody(t, pairPath(code), body); res.Code != http.StatusNoContent {
			t.Fatalf("the first pairing answered %d %s", res.Code, res.Body)
		}
		before, err := a.DB.PendingList(ctx)
		if err != nil || len(before) != 1 {
			t.Fatalf("pending after the first pairing: %v %v", before, err)
		}
		// The replay: the same body with the same secret.
		if res := a.postBody(t, pairPath(code), body); res.Code == http.StatusNoContent {
			t.Fatal("the same pairing secret was taken twice")
		}
		after, err := a.DB.PendingList(ctx)
		if err != nil || len(after) != 1 {
			t.Fatalf("%d pending rows after the replay, want 1 (%v)", len(after), err)
		}
	})

	t.Run("join secret", func(t *testing.T) {
		code := a.joinCode(t)
		body := joinBody(t, code)
		// Reachability: the first join request is kept.
		if res := a.postBody(t, joinPath(code), body); res.Code != http.StatusNoContent {
			t.Fatalf("the first join answered %d %s", res.Code, res.Body)
		}
		if _, err := a.DB.BlobGet(ctx, dbapi.BlobJoin, joinNode); err != nil {
			t.Fatalf("the first join request was not kept: %v", err)
		}
		// The replay.
		if res := a.postBody(t, joinPath(code), body); res.Code == http.StatusNoContent {
			t.Fatal("the same join secret was taken twice")
		}
	})
}

// TestForeignLicenseDoesNotOpenAnApp takes the licence vectors that are bound
// to ANOTHER installation and another install key and holds that they verify
// for neither: the installation and the install key are part of what the
// signature covers, so a licence bought for one installation opens nothing in
// another, and no content key comes out of it.
//
// The reachability check is the licence of THIS installation over the same app
// and the same install key: it verifies, and its wrapped key unwraps to a
// content key. Without that half the case would also be green against a
// verifier that refuses every licence.
//
// NOT MEASURED HERE, and said instead of left out: the channel
// ngc.store/api/license, which is how a licence really arrives, has no handler
// registered in coreapp.New and answers E_NOTFOUND whatever is sent -- see
// TestTheStoreChannelsHaveNoHandlerYet. An attack through it would measure the
// missing handler. What is measured is the verifier that handler will call.
func TestForeignLicenseDoesNotOpenAnApp(t *testing.T) {
	v := loadVectors(t)
	st := v.vectorStore()

	// Reachability first: the licence of this installation verifies.
	lic, err := store.VerifyLicense(v.License, st, v.Installation, v.LicenseApp, v.InstallKey)
	if err != nil {
		t.Fatalf("the own licence was refused: %v", err)
	}
	if lic.Key == nil {
		t.Fatal("the own licence carries no wrapped content key, so the next half measures nothing")
	}

	// The attack: every vector whose name says it is bound elsewhere.
	foreign := 0
	for _, c := range v.LicenseRejects {
		if !strings.Contains(c.Name, "other installation") &&
			!strings.Contains(c.Name, "other install key") {
			continue
		}
		foreign++
		if _, err := store.VerifyLicense(c.License, st, v.Installation, v.LicenseApp,
			v.InstallKey); err == nil {
			t.Fatalf("the licence %q was taken", c.Name)
		}
	}
	if foreign != 2 {
		t.Fatalf("%d licences bound elsewhere in the vector file, want 2", foreign)
	}

	// And the other way round, which is the half that names the installation:
	// the sound licence of this installation is refused the moment it is
	// checked for a DIFFERENT installation. Same bytes, same signature, same
	// store -- only the installation the API asks for changed.
	if _, err := store.VerifyLicense(v.License, st, trusttest.ID(0x12), v.LicenseApp,
		v.InstallKey); err == nil {
		t.Fatal("the own licence verified for another installation")
	}
}

// TestChangedParametersBreakTheChannel changes the parameters of a signed task
// after it was signed, in the two shapes an attacker has: the bytes of the
// task's own payload, and a second set of parameters put beside the task in
// the OPEN.
//
// The reachability check is the untouched task of the same channel, which
// reaches the relay (E_UNREACHABLE).
func TestChangedParametersBreakTheChannel(t *testing.T) {
	a := newAPI(t)

	// Reachability first: the untouched task of this channel, with no
	// parameters beside it, reaches the relay.
	if got := a.callCode(t, deactivateOpen(a.Task(t, deactivateTask(t, termApp, 0x63)))); got != wire.CodeUnreachable {
		t.Fatalf("the untouched task answered %q, want %q", got, wire.CodeUnreachable)
	}

	t.Run("the payload of the task", func(t *testing.T) {
		env := a.Task(t, deactivateTask(t, termApp, 0x61))
		// The app swapped for another name of the same length, so the JSON
		// stays valid and only the signature can tell.
		bentPayload := []byte(strings.Replace(string(env.Payload), termApp,
			strings.Repeat("z", len(termApp)), 1))
		if bytes.Equal(bentPayload, env.Payload) {
			t.Fatal("the payload was not changed at all, so this case measures nothing")
		}
		bent := *env
		bent.Payload = bentPayload
		if got := a.callCode(t, deactivateOpen(&bent)); got != wire.CodeVerify {
			t.Fatalf("the bent payload answered %q, want %q", got, wire.CodeVerify)
		}
	})

	t.Run("parameters beside the task", func(t *testing.T) {
		env := a.Task(t, deactivateTask(t, termApp, 0x62))
		params, err := json.Marshal(core.AppParams{App: "org.sorglos.other"})
		if err != nil {
			t.Fatalf("params: %v", err)
		}
		o := deactivateOpen(env)
		o.Params = params
		// MEASURED, and not what was expected when this case was written:
		// the answer is E_PROTOCOL and not E_VERIFY. wire.Open.Validate
		// refuses an OPEN that carries a task AND parameters of its own, and
		// that runs in package browser, before the dispatcher and before
		// rights.Check ever see the channel.
		//
		// So rights.Check's own second guard against this shape (check.go,
		// "Checked again here, so the decision does not rest on how careful
		// the caller was") is NOT reachable from a shell: the frame never
		// gets that far. It is a second bolt behind the first, which is worth
		// having and is not what this case measures. What matters for the
		// attack either way: the extra parameters open nothing.
		if got := a.callCode(t, o); got != wire.CodeProtocol {
			t.Fatalf("task plus params answered %q, want %q", got, wire.CodeProtocol)
		}
	})
}

// TestTruncatedDataIsAnError breaks a package off in the middle and holds the
// two halves the contract draws apart (shared/store, Walk):
//
//   - The encrypted stream carries its own end, so a stream cut short is
//     refused by the decrypter and never reaches a reader as if it were all
//     of it.
//   - The ARCHIVE does not. A package cut short parses as a shorter, complete
//     one, and only the checksum the catalog lists catches it. That is
//     measured here as it stands -- including the uncomfortable half, that
//     Walk alone takes it -- because a case that claimed Walk catches it
//     would be a sentence against the code.
//
// NOT MEASURED, and the plan asks for it: the second half of the attack, "an
// end-to-end stream without the controller's end counts as cut short". That
// needs a controller on the far side of a tunnel, and nothing in this package
// pairs one -- which is the same gap that makes E_UNREACHABLE the reachability
// wall everywhere else here. It stays open.
func TestTruncatedDataIsAnError(t *testing.T) {
	key := bytes.Repeat([]byte{0x9}, 32)
	plain := testPackage(t)
	listed := sha(plain) // what a catalog would carry for this package

	// Reachability: the whole package walks and carries its files.
	paths := walkPlain(t, plain)
	if !hasPath(paths, "ui/index.html") || !hasPath(paths, "services/amd64.tar") {
		t.Fatalf("the whole package walked to %v", paths)
	}

	var sealed bytes.Buffer
	if err := store.Encrypt(&sealed, bytes.NewReader(plain), key, zeroReader{}); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// Reachability of the sealed way: decrypt plus walk gives the same files.
	if got := walkSealed(t, sealed.Bytes(), key); !hasPath(got, "ui/index.html") {
		t.Fatalf("the whole sealed package walked to %v", got)
	}

	t.Run("the encrypted stream cut short", func(t *testing.T) {
		for _, cut := range []int{1, 16, sealed.Len() / 2} {
			short := sealed.Bytes()[:sealed.Len()-cut]
			if got := walkSealed(t, short, key); got != nil {
				t.Errorf("a package %d bytes short came through as %v", cut, got)
			}
		}
	})

	t.Run("one byte of a file changed", func(t *testing.T) {
		bent := bytes.Clone(plain)
		i := bytes.Index(bent, []byte(packBody))
		if i < 0 {
			t.Fatal("the test package does not carry its own body")
		}
		bent[i] ^= 0xFF
		var bentSealed bytes.Buffer
		if err := store.Encrypt(&bentSealed, bytes.NewReader(bent), key, zeroReader{}); err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		// Sealed afresh, so the seal holds and the walk goes through: the
		// archive itself says nothing about a changed byte.
		if got := walkSealed(t, bentSealed.Bytes(), key); got == nil {
			t.Fatal("the resealed package did not walk, so the next half measures nothing")
		}
		// What catches it is the checksum the catalog lists.
		if sha(bent) == listed {
			t.Fatal("a changed package has the checksum of the sound one")
		}
	})

	t.Run("the archive cut short", func(t *testing.T) {
		// Cut inside the last file's body, so the tar ends without its
		// end-of-archive blocks.
		short := plain[:len(plain)-1024]
		walked := walkPlain(t, short) != nil
		if sha(short) == listed {
			t.Fatal("a package cut short has the checksum of the whole one")
		}
		// The uncomfortable half, written down because it is what the code
		// does: Walk may well take a shortened archive. If it ever refuses
		// one, this says so instead of staying silent -- and either way the
		// checksum above is the wall.
		t.Logf("a package cut short by 1024 bytes: Walk took it = %v; its checksum differs = true",
			walked)
	})
}

// TestBeyondTheChannelLimitEndsTheLink nails both sides of
// wire.MaxSessionChannels: the last channel inside the limit opens, and the
// first one past it ends the whole link. A case that only held the overrun
// would also be green if the limit stood at one.
func TestBeyondTheChannelLimitEndsTheLink(t *testing.T) {
	a := newAPI(t)
	// Every channel is a call of ngc.desktop/me, the cheapest action there is,
	// and each is read to its close before the next opens: what is measured is
	// how many numbers one link hands out, not how many run at once.
	for i := 1; i <= wire.MaxSessionChannels; i++ {
		c := a.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI,
			Action: core.ActMe})
		c.eof(t)
		if body := c.result(t); len(body) == 0 {
			t.Fatalf("channel %d of %d answered nothing", i, wire.MaxSessionChannels)
		}
	}
	// One past the limit: the link ends, and not just this channel.
	a.num++
	a.peer.writeOpen(t, a.num, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI,
		Action: core.ActMe, Window: wire.MinWindow})
	wantLinkEnded(t, a.peer)
}

// TestManInTheMiddleAtSetup puts somebody between browser and API during the
// first setup, in the four shapes that has: another code on the offer, other
// bytes in the offer, another code on version 1, and other bytes in version 1.
//
// The reachability check is the whole setup with the right code over the
// offered bytes, which is taken (204) -- and it runs LAST, because it closes
// the setup for good.
func TestManInTheMiddleAtSetup(t *testing.T) {
	a := build(t)
	res := a.get(t, session.PathSetup)
	if res.Code != http.StatusOK {
		t.Fatalf("the offer answered %d %s", res.Code, res.Body)
	}
	offer := bytes.Clone(res.Body.Bytes())

	// Reachability of the offer: under the code of this API it parses.
	info, err := session.ParseSetupOffer(offer, a.Code)
	if err != nil {
		t.Fatalf("the offer was refused under its own code: %v", err)
	}

	// 1. The offer under another code: the MAC is checked before anything of
	// Info is decoded, so the keys a man in the middle would offer are never
	// even read.
	if _, err := session.ParseSetupOffer(offer, "AAAAA-BBBBB-CCCCC-DDDDD"); err == nil {
		t.Fatal("the offer was accepted under another code")
	}

	// 2. Other bytes in the offer: one byte of Info changed, the MAC kept.
	var o session.SetupOffer
	if err := json.Unmarshal(offer, &o); err != nil {
		t.Fatalf("offer: %v", err)
	}
	bentInfo := bytes.Clone(o.Info)
	bentInfo[len(bentInfo)/2] ^= 0x01
	bent, err := json.Marshal(session.SetupOffer{Info: bentInfo, MAC: o.MAC})
	if err != nil {
		t.Fatalf("bent offer: %v", err)
	}
	if _, err := session.ParseSetupOffer(bent, a.Code); err == nil {
		t.Fatal("an offer whose info was changed was accepted")
	}

	admin := trusttest.NewSigner(3)

	// 3. Version 1 sealed with another code.
	if res := a.postBody(t, session.PathSetup,
		firstVersion(t, info, admin, "AAAAA-BBBBB-CCCCC-DDDDD")); res.Code == http.StatusNoContent {
		t.Fatal("version 1 was taken under another code")
	}
	if _, err := a.Srv.State.Trust(ctx); err == nil {
		t.Fatal("a refused version 1 closed the setup")
	}

	// 4. Version 1 over OTHER bytes than the offer: the API's own Noise key
	// swapped for the attacker's, sealed with the RIGHT code. VerifySetup
	// compares the list against the offer, so holding the code is not enough.
	clearSetupLock(t, a)
	otherNoise, _ := trusttest.NodeKeys(0x77)
	elsewhere := info
	elsewhere.Nodes = []trust.Node{{ID: info.Nodes[0].ID, Kind: trust.NodeAPI,
		Name: info.Nodes[0].Name, Noise: enc.Bytes(otherNoise.Public),
		Sign: info.Nodes[0].Sign, URL: info.Nodes[0].URL, TLS: info.Nodes[0].TLS}}
	if res := a.postBody(t, session.PathSetup,
		firstVersion(t, elsewhere, admin, a.Code)); res.Code == http.StatusNoContent {
		t.Fatal("version 1 with another API key was taken")
	}
	if _, err := a.Srv.State.Trust(ctx); err == nil {
		t.Fatal("a refused version 1 closed the setup")
	}

	// Reachability, last: the sound setup is taken. The lock is cleared first,
	// because every wrong attempt above costs one (package setup, take: "Every
	// wrong attempt costs") -- a case that read E_LOCKED as a refusal would be
	// measuring the lock and calling it the signature.
	clearSetupLock(t, a)
	if res := a.postBody(t, session.PathSetup,
		firstVersion(t, info, admin, a.Code)); res.Code != http.StatusNoContent {
		t.Fatalf("the sound setup answered %d %s", res.Code, res.Body)
	}
	v, err := a.Srv.State.Trust(ctx)
	if err != nil || v.List.Seq != 1 {
		t.Fatalf("trust after the sound setup: %v", err)
	}
}

// TestTheStoreChannelsHaveNoHandlerYet is the measurement behind every "not
// reachable" of this file, and it is a case rather than a sentence so that it
// stops being true the moment somebody registers the handlers.
//
// coreapp.New registers the actions of ngc.desktop and ngc.users; the actions
// of ngc.store/api are registered by nobody yet, so the handler lookup of the
// dispatcher misses and the channel closes with E_NOTFOUND. The user of this
// fixture is an admin, so no earlier wall can be the reason -- which the
// reachability half shows: ngc.desktop/me, on the same session, answers.
//
// When this case goes red because a store action no longer answers E_NOTFOUND,
// the attacks marked "not reachable" can be measured for real.
func TestTheStoreChannelsHaveNoHandlerYet(t *testing.T) {
	a := newAPI(t)
	if body := a.call(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI,
		Action: core.ActMe}); len(body) == 0 {
		t.Fatal("ngc.desktop/me answered nothing on this session")
	}
	for _, act := range []string{core.ActSources, core.ActInstalled, core.ActInstallKey,
		core.ActUpdates} {
		got := a.callCode(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI,
			Action: act})
		if got != wire.CodeNotFound {
			t.Fatalf("ngc.store/api/%s answered %q, want %q -- the handlers are wired now, "+
				"and the attacks marked \"not reachable\" in this file can be measured",
				act, got, wire.CodeNotFound)
		}
	}
}

// --- the two codes a case asks the API for, over the channel the shell uses ---

// pairCode asks for a pairing code over ngc.users/pair-code, so the code of a
// case is one this API really handed out.
func (a *api) pairCode(t *testing.T) tunnel.PairCode {
	t.Helper()
	body := a.call(t, wire.Open{App: core.AppUsers, Service: core.ServiceAPI,
		Action: core.ActPairCode})
	var res core.PairCodeResult
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("pair code %q: %v", body, err)
	}
	code, err := tunnel.ParsePairCode(res.Code)
	if err != nil {
		t.Fatalf("parse pair code: %v", err)
	}
	return code
}

// joinCode is pairCode for a new API server.
func (a *api) joinCode(t *testing.T) tunnel.JoinCode {
	t.Helper()
	body := a.call(t, wire.Open{App: core.AppUsers, Service: core.ServiceAPI,
		Action: core.ActJoinCode})
	var res core.PairCodeResult
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("join code %q: %v", body, err)
	}
	code, err := tunnel.ParseJoinCode(res.Code)
	if err != nil {
		t.Fatalf("parse join code: %v", err)
	}
	return code
}
