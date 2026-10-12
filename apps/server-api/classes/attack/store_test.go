package attack

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// TestTheStoreChannelsAreWired is the reachability check all the attacks of
// this file rest on, and it replaces a case that held the opposite: until the
// strand that wired the ngc.store handlers landed, coreapp.New registered
// none of them and every one of these channels answered E_NOTFOUND, so an
// attack through them measured the missing handler and nothing else.
//
// Now they answer. A channel that ran and closed in order carries the empty
// code, and that is what is held here -- per action, so the file says which
// walls exist to be attacked at all.
func TestTheStoreChannelsAreWired(t *testing.T) {
	a := newAPI(t)
	for _, act := range []string{core.ActSources, core.ActInstalled, core.ActInstallKey,
		core.ActUpdates} {
		if got := a.callCode(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI,
			Action: act}); got != "" {
			t.Errorf("ngc.store/api/%s answered %q, want a channel that ran", act, got)
		}
	}
	// catalog answers E_NOTFOUND here, and that is NOT "unwired": nothing has
	// ever fetched a catalog in this fixture, so the row is missing. What
	// matters for the attack below is that the answer is past the parameter
	// wall, which E_PROTOCOL would not be.
	params, err := json.Marshal(core.StoreParams{Store: trust.StoreGlobal})
	if err != nil {
		t.Fatalf("params: %v", err)
	}
	if got := a.callCode(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI,
		Action: core.ActCatalog, Params: params}); got != wire.CodeNotFound {
		t.Fatalf("ngc.store/api/catalog for the global store answered %q, want %q",
			got, wire.CodeNotFound)
	}
}

// TestInjectedSQLInAStoreNameDoesNothing is the SQL attack on the one store
// channel that takes a name from the caller. It lives here and not beside the
// other three because it only became measurable with the handlers.
func TestInjectedSQLInAStoreNameDoesNothing(t *testing.T) {
	a := newAPI(t)
	bent, err := json.Marshal(core.StoreParams{Store: `global'; DROP TABLE apps;--`})
	if err != nil {
		t.Fatalf("params: %v", err)
	}
	if got := a.callCode(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI,
		Action: core.ActCatalog, Params: bent}); got != wire.CodeProtocol {
		t.Fatalf("the injected store name answered %q, want %q", got, wire.CodeProtocol)
	}
	// Reachability: the same channel with the name of the trust list gets PAST
	// the parameter wall. It then says E_NOTFOUND, because no catalog was ever
	// fetched -- a later wall, and a different one, which is exactly what
	// tells the two apart.
	clean, err := json.Marshal(core.StoreParams{Store: trust.StoreGlobal})
	if err != nil {
		t.Fatalf("params: %v", err)
	}
	if got := a.callCode(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI,
		Action: core.ActCatalog, Params: clean}); got != wire.CodeNotFound {
		t.Fatalf("the clean store name answered %q, want %q", got, wire.CodeNotFound)
	}
}

// uploadOpen is the OPEN of ngc.store/api/upload. The task and the
// confirmation are not decoration: upload is core.Critical, so without them
// task.Check answers ErrConfirm and the channel ends with E_CONFIRM before a
// byte of the package is read -- measured, and the reason an upload attack
// without them would have measured the confirmation wall instead of the
// package.
func uploadOpen(t *testing.T, a *api, fill byte) wire.Open {
	t.Helper()
	env := a.Task(t, task.Task{ID: enc.Bytes(bytes.Repeat([]byte{fill}, 16)),
		App: core.AppStore, Service: core.ServiceAPI, Action: core.ActUpload})
	pr := a.Admin.Prove(sign.PurposeConfirm, env.Payload)
	return wire.Open{App: core.AppStore, Service: core.ServiceAPI, Action: core.ActUpload,
		Task: env, Confirm: &pr, Window: wire.MinWindow}
}

// upload sends one package over a fresh upload channel and answers the code
// the channel closed with and the result it sent.
func upload(t *testing.T, a *api, fill byte, pkg []byte) (wire.Code, []byte) {
	t.Helper()
	c := a.Open(t, uploadOpen(t, a, fill))
	c.send(t, pkg)
	body := c.collect(t)
	return c.code(t), body
}

// TestAHostilePackageOnTheUploadChannelIsRefused attacks
// ngc.store/api/upload, the channel an admin really hands a package over.
//
// The reachability check is the sound package of this package's own builder:
// it is taken, and the result names id, version and the checksum the API read.
//
// It also records a measurement that is NOT a refusal, because it is what the
// code does: a package cut short by 1024 bytes is TAKEN. It parses as a
// shorter, complete archive (shared/store, Walk says so), and what tells it
// from the sound one is the CHECKSUM the upload answers -- which differs. The
// case holds that difference instead of claiming the channel refuses the
// package.
func TestAHostilePackageOnTheUploadChannelIsRefused(t *testing.T) {
	a := newAPI(t)
	pkg := testPackage(t)

	// Reachability.
	code, body := upload(t, a, 0x71, pkg)
	if code != "" {
		t.Fatalf("the sound package answered %q", code)
	}
	var sound core.UploadResult
	if err := json.Unmarshal(body, &sound); err != nil {
		t.Fatalf("result %q: %v", body, err)
	}
	if sound.ID != "org.sorglos.attack" || sound.Version != "1.0.0" || len(sound.SHA256) == 0 {
		t.Fatalf("the sound package answered %+v", sound)
	}

	// All three refusals below are E_VERIFY, which is appstore.CodeFor over
	// store.ErrPackage: the walk refused the archive. The first readings of
	// this case said E_PROTOCOL for two of them, and that was a defect of the
	// MEASUREMENT and not a finding -- the channel bookkeeping of the fixture
	// read frames of an earlier channel, so a case answered the code of the
	// one before it. The numbers only became stable, and equal whether the
	// cases share a session or each gets its own, once chann.pull filtered on
	// the channel number. Nothing of the server changed.
	t.Run("a link out of the package", func(t *testing.T) {
		var buf bytes.Buffer
		w := tar.NewWriter(&buf)
		if err := w.WriteHeader(&tar.Header{Name: store.ManifestFile, Mode: 0o644,
			Size: int64(len(packManifest)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(packManifest)); err != nil {
			t.Fatal(err)
		}
		if err := w.WriteHeader(&tar.Header{Name: "ui/index.html", Mode: 0o777,
			Typeflag: tar.TypeSymlink, Linkname: "/etc/shadow"}); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if got, _ := upload(t, a, 0x72, buf.Bytes()); got != wire.CodeVerify {
			t.Fatalf("a link out of the package answered %q, want %q", got, wire.CodeVerify)
		}
	})

	t.Run("the manifest is not the first entry", func(t *testing.T) {
		bent := bytes.Clone(pkg)
		// The name field of a tar header starts at byte 0 of the archive.
		copy(bent, "other.json\x00")
		if got, _ := upload(t, a, 0x73, bent); got != wire.CodeVerify {
			t.Fatalf("a package without its manifest first answered %q, want %q",
				got, wire.CodeVerify)
		}
	})

	t.Run("cut to 200 bytes", func(t *testing.T) {
		if got, _ := upload(t, a, 0x74, pkg[:200]); got != wire.CodeVerify {
			t.Fatalf("a package of 200 bytes answered %q, want %q", got, wire.CodeVerify)
		}
	})

	t.Run("cut by 1024 bytes: taken, with another checksum", func(t *testing.T) {
		got, body := upload(t, a, 0x75, pkg[:len(pkg)-1024])
		if got != "" {
			t.Fatalf("the shortened package answered %q -- the channel refuses it now, "+
				"and this case has to be rewritten to hold the refusal", got)
		}
		var short core.UploadResult
		if err := json.Unmarshal(body, &short); err != nil {
			t.Fatalf("result %q: %v", body, err)
		}
		if bytes.Equal(short.SHA256, sound.SHA256) {
			t.Fatal("the shortened package answered the checksum of the sound one")
		}
		t.Logf("sound sha256 %s, shortened %s -- the upload takes it and the checksum is "+
			"what tells them apart", enc.B64.EncodeToString(sound.SHA256),
			enc.B64.EncodeToString(short.SHA256))
	})
}

// licenceOpen is the OPEN of ngc.store/api/license. The action carries no
// parameters of its own, so the app comes out of the licence.
func licenceOpen() wire.Open {
	return wire.Open{App: core.AppStore, Service: core.ServiceAPI, Action: core.ActLicense,
		Window: wire.MinWindow}
}

// storeSigned signs a licence the way the global store of trusttest.Genesis
// does: the key pair of NodeKeys(0xB1), whose public half the trust list
// carries as the store's key. Without that the API would refuse every licence
// of this case for its signature, and the attack below would measure the
// signature instead of the installation.
func storeSigned(t *testing.T, l store.License) []byte {
	t.Helper()
	_, storeKey := trusttest.NodeKeys(0xB1)
	env, err := sign.Sign(storeKey, sign.PurposeLicense, mustJSON(t, l))
	if err != nil {
		t.Fatalf("sign the licence: %v", err)
	}
	return mustJSON(t, env)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// TestAForeignLicenceOnTheChannelKeepsNothing attacks ngc.store/api/license,
// the channel a licence really arrives on. Everything is the same but one
// field: the same store KEY signs it (so the signature is sound, it is just
// over other bytes), the same app, the same install key -- and another
// installation.
//
// The reachability check is the licence of THIS installation over the same
// channel: it is taken and a row appears. Without it the refusal below could
// just as well be a licence the API refuses for its signature, its app row or
// its store.
func TestAForeignLicenceOnTheChannelKeepsNothing(t *testing.T) {
	a := newAPI(t)
	// The app has to be installed: appstore reads the store out of the app's
	// own row to know which key signed the licence.
	if err := a.DB.AppPut(ctx, dbapi.App{ID: termApp, Version: "1.0.0",
		Store: trust.StoreGlobal, Node: ctlNode, Active: true,
		Manifest: []byte(packManifest), Updated: testNow}); err != nil {
		t.Fatalf("app row: %v", err)
	}
	// The installation's own X25519 key, asked for over the channel the shell
	// uses, so the licence below is wrapped for the key this API really holds
	// and not for one the case made up.
	var key core.InstallKey
	if err := json.Unmarshal(a.call(t, wire.Open{App: core.AppStore, Service: core.ServiceAPI,
		Action: core.ActInstallKey}), &key); err != nil {
		t.Fatalf("install key: %v", err)
	}
	if len(key.Key) != 32 || key.Installation != a.Installation {
		t.Fatalf("install-key answered %+v", key)
	}
	lic := store.License{ID: "L-1", Store: trust.StoreGlobal, App: termApp,
		Installation: a.Installation, InstallKey: key.Key, Per: store.PerUser, Users: 5,
		Issued: testNow, Expires: testNow + 30*86400,
		Key: &store.WrappedKey{Ephemeral: bytes.Repeat([]byte{2}, 32),
			Nonce: bytes.Repeat([]byte{3}, 12), Sealed: bytes.Repeat([]byte{4}, 48)}}

	// The attack FIRST, while no row is there: a foreign licence must keep
	// nothing, and "nothing" is only readable before the sound one is taken.
	foreign := lic
	foreign.Installation = trusttest.ID(0x12)
	c := a.Open(t, licenceOpen())
	c.send(t, storeSigned(t, foreign))
	if got := c.code(t); got == "" {
		t.Fatal("a licence of another installation was taken")
	}
	if _, err := a.DB.BlobGet(ctx, dbapi.BlobLicense, termApp); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("the foreign licence left a row: %v", err)
	}

	// Reachability: the licence of this installation, everything else equal.
	c2 := a.Open(t, licenceOpen())
	c2.send(t, storeSigned(t, lic))
	body := c2.collect(t)
	if got := c2.code(t); got != "" {
		t.Fatalf("the own licence answered %q", got)
	}
	if _, err := a.DB.BlobGet(ctx, dbapi.BlobLicense, termApp); err != nil {
		t.Fatalf("the own licence left no row: %v", err)
	}
	t.Logf("the own licence answered %s", body)
}
