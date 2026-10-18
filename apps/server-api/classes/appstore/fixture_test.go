package appstore

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

var ctx = context.Background()

const testNow = int64(1790000000)

// vectors is shared/testdata/store.json: a real catalog of the global store,
// its key, and the sixteen catalogs the contract refuses. Nothing here is
// signed by this test, so no case can pass by signing its own answer.
type vectors struct {
	Store          string      `json:"store"`
	StoreKey       enc.Bytes   `json:"storeKey"`
	Catalog        envelopeRaw `json:"catalog"`
	CatalogLastSeq uint64      `json:"catalogLastSeq"`
	CatalogRejects []struct {
		Name    string      `json:"name"`
		Catalog envelopeRaw `json:"catalog"`
		LastSeq uint64      `json:"lastSeq"`
		Valid   bool        `json:"valid"`
	} `json:"catalogRejects"`
	ReleaseKey     enc.Bytes   `json:"releaseKey"`
	Release        envelopeRaw `json:"release"`
	ReleaseLastSeq uint64      `json:"releaseLastSeq"`
}

// envelopeRaw keeps an envelope of the vectors as JSON too, because a case
// serves the bytes a store would serve, not a struct.
type envelopeRaw json.RawMessage

func (e envelopeRaw) bytes() []byte { return []byte(e) }

func (e envelopeRaw) envelope(t *testing.T) sign.Envelope {
	t.Helper()
	env, err := sign.ParseEnvelope(e.bytes(), sign.MaxPayload)
	if err != nil {
		t.Fatalf("vector envelope: %v", err)
	}
	return env
}

func (e *envelopeRaw) UnmarshalJSON(b []byte) error { *e = envelopeRaw(b); return nil }

func loadVectors(t *testing.T) vectors {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "shared", "testdata", "store.json"))
	if err != nil {
		t.Fatalf("store.json: %v", err)
	}
	var v vectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("store.json: %v", err)
	}
	return v
}

// reject returns the vector of that name, so a case names the attack it runs
// instead of an index that moves when the vectors grow.
func (v vectors) reject(t *testing.T, name string) envelopeRaw {
	t.Helper()
	for _, r := range v.CatalogRejects {
		if r.Name == name {
			if r.Valid {
				t.Fatalf("vector %q is not a reject", name)
			}
			return r.Catalog
		}
	}
	t.Fatalf("no catalog reject named %q", name)
	return nil
}

// served is what the test server answers with and what it saw. It is behind a
// pointer and guarded, because the handler runs on the server's goroutine
// while the case reads it: a plain field of the fixture would be a copy, and a
// Hits of 0 would then read as "refused before the request" for every case.
type served struct {
	mu sync.Mutex
	// Files is what the store serves, by file name; a case replaces an entry
	// to serve a forged answer.
	Files map[string][]byte
	// Status is the status the next answer carries, 200 unless a case changes
	// it.
	Status int
	// Hits counts the requests the store saw, so a case can tell a refusal
	// before the request from one after it.
	Hits int
	// Last is the whole request line of the last request, not just the name
	// the answer was picked by. A fixture that reads only the base name of the
	// path cannot tell a request that went to the store from one that went
	// into a directory of its own, carried a query or used another method --
	// the same blindness that let the pairing request travel without its
	// ?secret= (point 61).
	Last seenRequest
}

// seenRequest is a request line of the store in the three parts a store
// address is made of. Headers are not kept: get and Download send none of
// their own, so a list of them would only print what the client library adds.
type seenRequest struct {
	Method   string
	Path     string
	RawQuery string
}

func (sv *served) put(name string, b []byte) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	sv.Files[name] = b
}

func (sv *served) status(code int) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	sv.Status = code
}

func (sv *served) hits() int {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	return sv.Hits
}

func (sv *served) last() seenRequest {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	return sv.Last
}

// fixture is one API with one store in its trust list, and that store served
// by a real TLS server.
type fixture struct {
	St    *Stores
	S     *state.State
	HTTP  *httptest.Server
	Serve *served
}

// Files, Status and Hits read through the shared state, so a case speaks of
// the server and not of its own copy.
func (fx fixture) setFile(name string, b []byte) { fx.Serve.put(name, b) }
func (fx fixture) setStatus(code int)            { fx.Serve.status(code) }
func (fx fixture) hits() int                     { return fx.Serve.hits() }

// newFixture builds the API, serves the store and puts that store into
// version 2 of the trust list with the key of the vectors.
func newFixture(t *testing.T) (fixture, vectors) {
	t.Helper()
	v := loadVectors(t)
	sv := &served{Files: map[string][]byte{}, Status: http.StatusOK}
	sv.Files[CatalogFile] = v.Catalog.bytes()
	sv.Files[ReleaseFile] = v.Release.bytes()
	fx := fixture{Serve: sv}
	fx.HTTP = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		sv.mu.Lock()
		sv.Hits++
		sv.Last = seenRequest{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery}
		b, ok := sv.Files[name]
		code := sv.Status
		sv.mu.Unlock()
		// The rule, and it is kept here so that every case of this package
		// runs through it instead of one case holding it alone: a store is
		// asked with GET, for a file that lies directly under the address of
		// the store, and with nothing appended to the address.
		// trust.ValidStoreURL has no query and no trailing slash, so the whole
		// path is "/" plus the file name. Picking the answer by the base name
		// alone, as this fixture did, serves a request that went anywhere.
		if r.Method != http.MethodGet {
			t.Errorf("the store was asked with %s; it is read with GET only", r.Method)
		}
		if r.URL.Path != "/"+name {
			t.Errorf("the store was asked for %q, not for /%s: the address of a store carries no directory of its own", r.URL.Path, name)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("the store was asked with the query %q; a store address has none, so nothing may append one", r.URL.RawQuery)
		}
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// A redirect without a target is no redirect: the case that serves
		// 302 wants the client to be handed the 3xx, so a Location is set.
		if code >= 300 && code < 400 {
			w.Header().Set("Location", "https://elsewhere.example/catalog.json")
		}
		w.WriteHeader(code)
		_, _ = w.Write(b)
	}))
	t.Cleanup(fx.HTTP.Close)

	box, err := secrets.NewBox(config.NewSecret(make([]byte, 32)))
	if err != nil {
		t.Fatalf("box: %v", err)
	}
	noiseKey, signKey := trusttest.NodeKeys(0xA1)
	_, apiPin := trusttest.APITLS(0xE1, "api-1.test")
	cfg := &config.API{
		Node: "api-1", URL: "wss://api-1.test:7443/tunnel", Listen: ":7443",
		Cert: "/x", CertKey: "/y",
		NoiseKey: config.NewSecret(noiseKey.Private), SignKey: config.NewSecret(signKey.Seed()),
		SecretKey:    config.NewSecret(make([]byte, 32)),
		ChallengeKey: config.NewSecret(bytes.Repeat([]byte{7}, 32)),
		ReleaseKey:   v.ReleaseKey, StoreKey: v.StoreKey,
		StoreURL: fx.HTTP.URL, SessionLifetime: 43200, IdleTimeout: 1800,
	}
	s := state.New(cfg, memstore.New(), box, apiPin)
	s.Now = func() int64 { return testNow }
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	signed := admin.Sign(t, l)
	if err := s.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallation,
		Data: []byte(l.Installation), Updated: testNow}); err != nil {
		t.Fatalf("installation: %v", err)
	}
	if _, err := s.AddVersion(ctx, signed); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	// Version 2 points the global store at this test's server and gives it the
	// key of the vectors, so the catalogs of store.json verify against the
	// trust list and nothing else does.
	l2 := trusttest.Next(t, signed)
	l2.Stores = []trust.Store{{ID: v.Store, Name: "NGC Store", URL: fx.HTTP.URL, Key: v.StoreKey}}
	if _, err := s.AddVersion(ctx, admin.Sign(t, l2)); err != nil {
		t.Fatalf("version 2: %v", err)
	}
	fx.S = s
	fx.St = New(s)
	// Only the roots are the test server's; the timeout and the refusal to
	// follow a redirect stay as New set them (TestTheClientFollowsNothing).
	fx.St.Client.Transport = fx.HTTP.Client().Transport
	return fx, v
}

// theStore is the store of the trust list, as Refresh reads it.
func (fx fixture) theStore(t *testing.T) trust.Store {
	t.Helper()
	ver, err := fx.S.Trust(ctx)
	if err != nil {
		t.Fatalf("trust: %v", err)
	}
	if len(ver.List.Stores) != 1 {
		t.Fatalf("stores: got %d, want 1", len(ver.List.Stores))
	}
	return ver.List.Stores[0]
}

// keptSeq is the Seq of the stored catalog of store id, 0 where none is
// stored.
func (fx fixture) keptSeq(t *testing.T, id string) uint64 {
	t.Helper()
	b, err := fx.S.DB.BlobGet(ctx, dbapi.BlobCatalog, id)
	if err != nil {
		return 0
	}
	return b.Seq
}
