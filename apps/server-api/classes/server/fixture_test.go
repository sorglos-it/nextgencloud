package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbopen"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

var ctx = context.Background()

// newConfig writes the key files into a directory of this test and returns a
// configuration that Build accepts. Nothing of it is shared between cases.
func newConfig(t *testing.T) *config.API {
	t.Helper()
	dir := t.TempDir()
	noiseKey, signKey := trusttest.NodeKeys(0xA1)
	return &config.API{
		Node: "api-1", URL: "wss://api-1.test:7443/tunnel", Listen: "127.0.0.1:0",
		Cert: filepath.Join(dir, "api.crt"), CertKey: filepath.Join(dir, "api.key"),
		NoiseKey:        config.NewSecret(noiseKey.Private),
		SignKey:         config.NewSecret(signKey.Seed()),
		SecretKey:       config.NewSecret(bytes.Repeat([]byte{3}, 32)),
		ChallengeKey:    config.NewSecret(bytes.Repeat([]byte{7}, 32)),
		SessionLifetime: 43200, IdleTimeout: 1800,
	}
}

// withStore registers an opener that hands out one memory store, and takes it
// back when the case ends: dbopen holds exactly one, and a case that left its
// own standing would decide what the next one opens.
func withStore(t *testing.T, db dbapi.Store) {
	t.Helper()
	dbopen.Register(func(context.Context, config.DB, string) (dbapi.Store, error) { return db, nil })
	t.Cleanup(func() { dbopen.Register(nil) })
}

// withDirs moves the setup code and the spool into this case's own directory.
func withDirs(t *testing.T) (code, spool string) {
	t.Helper()
	dir := t.TempDir()
	code, spool = filepath.Join(dir, "setup-code"), filepath.Join(dir, "spool")
	oldCode, oldSpool, oldAnchor := SetupCodeFile, SpoolDir, AnchorFile
	SetupCodeFile, SpoolDir = code, spool
	// The anchor belongs in this directory too: without it Build reads the
	// file of the machine (config.AnchorFile).
	AnchorFile = filepath.Join(dir, "trust-anchor.json")
	t.Cleanup(func() {
		SetupCodeFile, SpoolDir, AnchorFile = oldCode, oldSpool, oldAnchor
	})
	return code, spool
}

// newServer builds a server over an empty database, the state a first start
// is really in: no trust list, so the setup is open.
func newServer(t *testing.T) (*Server, dbapi.Store, string) {
	t.Helper()
	db := memstore.New()
	withStore(t, db)
	_, spool := withDirs(t)
	srv, err := Build(ctx, newConfig(t), "9.9.9")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return srv, db, spool
}

// signedIn puts a genesis trust list into db, so a case can measure a server
// whose setup is closed.
func signedIn(t *testing.T, db dbapi.Store) {
	t.Helper()
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	signed := admin.Sign(t, l)
	body, err := json.Marshal(signed)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting,
		ID: dbapi.SettingInstallation, Data: []byte(l.Installation), Updated: 1}); err != nil {
		t.Fatalf("installation: %v", err)
	}
	if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobTrust, ID: "1",
		Data: body, Updated: 1}); err != nil {
		t.Fatalf("trust: %v", err)
	}
}

// codeOf reads the wire code out of an answer of httpapi.Fail.
func codeOf(t *testing.T, rec *httptest.ResponseRecorder) wire.Code {
	t.Helper()
	var e struct {
		Code wire.Code `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("answer %q is not an error of the contract: %v", rec.Body.String(), err)
	}
	return e.Code
}

// filesIn names what lies in dir, "" for a directory that is not there.
func filesIn(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	names := make([]string, 0, len(es))
	for _, e := range es {
		names = append(names, e.Name())
	}
	return names
}
