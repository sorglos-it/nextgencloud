package attack

import (
	"archive/tar"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// quote puts a string into a JSON body without the case writing escapes by
// hand: the injected names carry quotes and backslashes themselves.
func quote(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	return string(b)
}

// postBody is a POST through the router with a body of bytes.
func (a *api) postBody(t *testing.T, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	return a.post(t, path, string(body))
}

// --- the vectors of shared/testdata/store.json ---

// vectors is the part of store.json this package reads. The names are the
// ones shared/store writes; what is NOT read is left out, and nothing here
// invents a value: store, storeKey, installation, installKey and licenseApp
// all come out of the file, so the sound documents beside the rejects verify
// against exactly the inputs the rejects were built against. A case that put
// the fixture's own store key here would measure a verifier that refuses
// everything and call every reject caught.
type vectors struct {
	Store          string        `json:"store"`
	StoreKey       enc.Bytes     `json:"storeKey"`
	Catalog        sign.Envelope `json:"catalog"`
	CatalogLastSeq uint64        `json:"catalogLastSeq"`
	CatalogRejects []struct {
		Name    string        `json:"name"`
		Catalog sign.Envelope `json:"catalog"`
		LastSeq uint64        `json:"lastSeq"`
	} `json:"catalogRejects"`
	Installation   string        `json:"installation"`
	InstallKey     enc.Bytes     `json:"installKey"`
	LicenseApp     string        `json:"licenseApp"`
	License        sign.Envelope `json:"license"`
	LicenseRejects []struct {
		Name    string        `json:"name"`
		License sign.Envelope `json:"license"`
	} `json:"licenseRejects"`
	ReleaseKey     enc.Bytes     `json:"releaseKey"`
	Release        sign.Envelope `json:"release"`
	ReleaseLastSeq uint64        `json:"releaseLastSeq"`
	ReleaseRejects []struct {
		Name    string        `json:"name"`
		Release sign.Envelope `json:"release"`
		LastSeq uint64        `json:"lastSeq"`
	} `json:"releaseRejects"`
}

// loadVectors reads shared/testdata/store.json. A file that is not there is a
// failed case and never a skipped one: a measurement that switches itself off
// over a missing file is the worst kind there is.
func loadVectors(t *testing.T) vectors {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "shared", "testdata", "store.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the vectors of %s are not readable: %v", path, err)
	}
	var v vectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("the vectors do not parse: %v", err)
	}
	if len(v.CatalogRejects) == 0 || len(v.LicenseRejects) == 0 || len(v.ReleaseRejects) == 0 {
		t.Fatalf("the vector file carries %d catalog, %d licence and %d release rejects",
			len(v.CatalogRejects), len(v.LicenseRejects), len(v.ReleaseRejects))
	}
	return v
}

// vectorStore is the store the catalogs and licences of the file verify
// against.
func (v vectors) vectorStore() trust.Store {
	return trust.Store{ID: v.Store, Name: "NGC Store", Key: v.StoreKey}
}

// releasePub is the key the releases of the file verify against.
func (v vectors) releasePub() ed25519.PublicKey { return ed25519.PublicKey(v.ReleaseKey) }

// --- pairing and joining ---

// pairPath names which one-time value a pairing request spends.
func pairPath(code tunnel.PairCode) string {
	h := sha256.Sum256(code.Secret)
	return tunnel.PathPair + "?secret=" + enc.B64.EncodeToString(h[:])
}

// pairBody is a pairing request for a controller key, with the MAC made from
// the secret handed in.
func pairBody(t *testing.T, code tunnel.PairCode, ctrlKey, secret []byte) []byte {
	t.Helper()
	body, err := json.Marshal(tunnel.PairRequest{Name: "services-1", Noise: enc.Bytes(ctrlKey),
		MAC: enc.Bytes(tunnel.PairMAC(secret, ctrlKey, code.Key))})
	if err != nil {
		t.Fatalf("pair body: %v", err)
	}
	return body
}

// joinNode is the node a join request of these cases asks for.
const joinNode = "api-2"

// joinPath names which one-time value a join request spends.
func joinPath(code tunnel.JoinCode) string {
	h := sha256.Sum256(code.Secret)
	return tunnel.PathJoin + "?secret=" + enc.B64.EncodeToString(h[:])
}

// joinBody is a join request of a second API server, bound to the key of this
// one as the MAC covers it.
func joinBody(t *testing.T, code tunnel.JoinCode) []byte {
	t.Helper()
	newNoise, newSign := trusttest.NodeKeys(0xA2)
	tls := bytes.Repeat([]byte{0x2E}, sha256.Size)
	req := tunnel.JoinRequest{Node: joinNode, Noise: enc.Bytes(newNoise.Public),
		Sign: enc.Bytes(newSign.Public().(ed25519.PublicKey)), TLS: enc.Bytes(tls),
		URL: "wss://api-2.test:7443/tunnel"}
	req.MAC = enc.Bytes(tunnel.JoinMAC(code.Secret, req.Node, req.Noise, req.Sign, req.TLS,
		code.Key, req.URL))
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("join body: %v", err)
	}
	return body
}

// --- the first setup ---

// firstVersion builds version 1 of the trust list the way the shell does, out
// of the offer, and seals it with the code handed in -- the right one or
// another.
func firstVersion(t *testing.T, info session.SetupInfo, admin trusttest.Signer, code string) []byte {
	t.Helper()
	l := &trust.List{
		Seq: 1, Issued: info.Now, Installation: info.Installation,
		Users: []trust.User{{ID: trusttest.ID(1), Name: "admin", Roles: []string{trust.RoleAdmin},
			Keys: []proof.UserKey{admin.Key}}},
		Roles: []trust.Role{{Name: trust.RoleAdmin, Level: trust.LevelCritical}},
		Nodes: info.Nodes,
		// Sorted by target, as trust.List.Validate asks: "system" comes last.
		Auto: []trust.Auto{
			{Target: trust.AutoAPI, Key: info.ReleaseKey},
			{Target: trust.AutoController, Key: info.ReleaseKey},
			{Target: trust.AutoWeb, Key: info.ReleaseKey},
			{Target: trust.AutoSystem},
		},
	}
	if info.Store != nil {
		l.Stores = []trust.Store{*info.Store}
	}
	signed := admin.Sign(t, l)
	b, err := json.Marshal(session.Setup{MAC: session.SetupMAC(code, signed.Payload), Trust: signed})
	if err != nil {
		t.Fatalf("version 1: %v", err)
	}
	return b
}

// setupLockName is the name package setup counts its wrong attempts under. It
// is written out here because it is unexported there, and it is checked
// against the behaviour and not trusted: clearSetupLock fails the case if the
// clear did not actually free the door.
const setupLockName = "setup/code"

// clearSetupLock frees the door the wrong attempts of a case locked, so the
// sound setup that follows measures the signature and not the lock.
func clearSetupLock(t *testing.T, a *api) {
	t.Helper()
	if err := lock.Clear(ctx, a.DB, setupLockName); err != nil {
		t.Fatalf("clear the setup lock: %v", err)
	}
	wait, err := lock.Check(ctx, a.DB, setupLockName, testNow)
	if err != nil {
		t.Fatalf("check the setup lock: %v", err)
	}
	if wait > 0 {
		t.Fatalf("the setup lock still holds for %ds after it was cleared -- "+
			"the name %q is not the one package setup counts under", wait, setupLockName)
	}
}

// --- app packages ---

// packBody is the body of the one UI file of the test package. A case changes
// one byte of it to break the package without touching the manifest.
const packBody = "<!doctype html><title>attack</title>"

// packManifest is the manifest of the test package: one service, so the
// package has to carry an image, and a UI entry and an icon, so Walk has
// something to check its files against.
const packManifest = `{"id":"org.sorglos.attack","name":"Attack","version":"1.0.0",` +
	`"publisher":"Sorglos","icon":"logo.png",` +
	`"ui":{"entry":"index.html","width":800,"height":600},` +
	`"actions":[{"name":"doit","title":"Tun","service":"svc"}],` +
	`"services":[{"name":"svc","command":["/usr/bin/app"]}]}`

// testPackage is a package store.Walk takes. It is built here rather than
// read from testdata because a case needs to change one byte of it, and
// because a package that does not walk would make every refusal below
// meaningless -- which is why TestTruncatedDataIsAnError walks the whole one
// first.
func testPackage(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	add := func(name string, body []byte, flag byte) {
		h := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: flag}
		if flag == tar.TypeDir {
			h.Mode, h.Size = 0o755, 0
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if flag == tar.TypeReg {
			if _, err := w.Write(body); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}
	}
	add(store.ManifestFile, []byte(packManifest), tar.TypeReg)
	add("ui", nil, tar.TypeDir)
	add("ui/index.html", []byte(packBody), tar.TypeReg)
	add("ui/logo.png", []byte("PNG"), tar.TypeReg)
	add("services", nil, tar.TypeDir)
	add("services/amd64.tar", []byte("image bytes"), tar.TypeReg)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return buf.Bytes()
}

// walkPlain walks a package that is not encrypted and answers the paths it
// carried, nil when it was refused.
func walkPlain(t *testing.T, pkg []byte) []string {
	t.Helper()
	var paths []string
	_, err := store.Walk(bytes.NewReader(pkg), func(_ *store.Manifest, f store.PackFile, body io.Reader) error {
		if _, err := io.Copy(io.Discard, body); err != nil {
			return err
		}
		paths = append(paths, f.Path)
		return nil
	})
	if err != nil {
		return nil
	}
	return paths
}

// walkSealed decrypts and walks; it answers the paths, nil when either step
// refused. The two are one step on purpose: that is how a reader of a package
// sees it, and a case that decrypted first would not notice that the end of
// the stream is missing until it walked.
func walkSealed(t *testing.T, sealed, key []byte) []string {
	t.Helper()
	r, err := store.NewDecrypter(bytes.NewReader(sealed), key)
	if err != nil {
		return nil
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		return nil
	}
	return walkPlain(t, plain)
}

// sha is the checksum a catalog lists for a package.
func sha(b []byte) string {
	h := sha256.Sum256(b)
	return enc.B64.EncodeToString(h[:])
}

// hasPath reports whether paths carries name.
func hasPath(paths []string, name string) bool {
	for _, p := range paths {
		if p == name {
			return true
		}
	}
	return false
}
