package config

import (
	"bytes"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

func example(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "testdata", "config", name)
}

// pin returns a TLS pin of the examples: 32 bytes of fill (api-1 0xE1, api-2
// 0xE2).
func pin(fill byte) []byte { return bytes.Repeat([]byte{fill}, 32) }

// zeroReader reads only zero bytes, so certificates come out the same on
// every run.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// testCert makes a certificate from template with an Ed25519 key from 32
// bytes of fill, valid from 2026-01-01 to 2036-01-01 UTC: signed by parent's
// key parentKey, or by itself when parent is nil. It returns the DER and the
// key.
func testCert(t testing.TB, template *x509.Certificate, fill byte, parent *x509.Certificate,
	parentKey ed25519.PrivateKey) ([]byte, ed25519.PrivateKey) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{fill}, ed25519.SeedSize))
	template.NotBefore = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	template.NotAfter = time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC)
	if parent == nil {
		parent, parentKey = template, key
	}
	der, err := x509.CreateCertificate(zeroReader{}, template, parent, key.Public(), parentKey)
	if err != nil {
		t.Fatal(err)
	}
	return der, key
}

// certPEM returns der as a PEM block of type CERTIFICATE.
func certPEM(der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// testCA makes a database CA from the Ed25519 seed of 32 bytes of fill:
// serial 1, CN "NGC DB CA (Test)", a CA for certificates and CRLs. It returns
// the certificate, its key and its PEM.
func testCA(t testing.TB, fill byte) (*x509.Certificate, ed25519.PrivateKey, string) {
	t.Helper()
	der, key := testCert(t, &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "NGC DB CA (Test)"},
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}, fill, nil, nil)
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key, certPEM(der)
}

// exampleCA returns the PEM of the database CA of the examples (db.ca in
// api.json, migrate.ca in controller-db.json): testCA with fill 0xCA.
func exampleCA(t testing.TB) string {
	t.Helper()
	_, _, p := testCA(t, 0xCA)
	return p
}

// nodeCert returns the certificate of database node host, signed by the
// example CA, with its key from fill 0xDB.
func nodeCert(t testing.TB, host string) ([]byte, ed25519.PrivateKey) {
	t.Helper()
	ca, caKey, _ := testCA(t, 0xCA)
	return testCert(t, &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: host},
		DNSNames:              []string{host},
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, 0xDB, ca, caKey)
}

func TestExamples(t *testing.T) {
	// The examples hold the CA as a JSON string: go test -v prints it to paste.
	ca := exampleCA(t)
	quoted, err := json.Marshal(ca)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("exampleCA as a JSON string: %s", quoted)

	w, err := LoadWeb(example(t, "web.json"))
	if err != nil || len(w.APIs) != 2 || w.RP.ID != "ngc.example.org" ||
		w.APIs[0].URL != "https://api-1.ngc.test:7443" || !bytes.Equal(w.APIs[0].TLS, pin(0xE1)) ||
		w.APIs[1].URL != "https://api-2.ngc.test:7443" || !bytes.Equal(w.APIs[1].TLS, pin(0xE2)) {
		t.Fatalf("web: %+v, %v", w, err)
	}
	a, err := LoadAPI(example(t, "api.json"))
	if err != nil || a.Node != "api-1" || a.URL != "wss://api-1.ngc.test:7443/tunnel" || a.DB.Driver != DriverMariaDB ||
		len(a.DB.Nodes) != 3 {
		t.Fatalf("api: %+v, %v", a, err)
	}
	if a.SessionLifetime != DefaultSessionLifetime || a.IdleTimeout != DefaultIdleTimeout {
		t.Errorf("defaults: %+v", a)
	}
	c, err := LoadController(example(t, "controller.json"))
	if err != nil || c.Name != "dienste" || len(c.APIs) != 1 || c.Anchor.Seq != 1 || c.Podman != DefaultPodman ||
		len(c.Roles) != 1 || c.Roles[0] != tunnel.RoleServices || c.Migrate != nil {
		t.Fatalf("controller: %+v, %v", c, err)
	}
	d, err := LoadController(example(t, "controller-db.json"))
	if err != nil || d.Name != "datenbank" || len(d.Roles) != 2 || d.Roles[0] != tunnel.RoleDB || d.Migrate == nil ||
		d.Migrate.Driver != DriverMariaDB || d.Migrate.User != "ngc_migrate" || len(d.Migrate.Nodes) != 3 {
		t.Fatalf("controller with the db role: %+v, %v", d, err)
	}
	// Both controllers dial api-1 of web.json, pinned to its TLS key.
	for _, ctl := range []*Controller{c, d} {
		for _, api := range ctl.APIs {
			if api.URL != "wss://api-1.ngc.test:7443/tunnel" || !bytes.Equal(api.TLS, pin(0xE1)) {
				t.Errorf("%s: api %+v", ctl.Name, api)
			}
		}
	}
	if a.DB.CA != ca || d.Migrate.CA != ca {
		t.Errorf("db.ca of api.json or migrate.ca of controller-db.json is not exampleCA")
	}
}

// dirEntries returns the names in dir.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestSaveController(t *testing.T) {
	c, err := LoadController(example(t, "controller-db.json"))
	if err != nil {
		t.Fatal(err)
	}
	c.Node = "ctl-1"
	dir := t.TempDir()
	path := filepath.Join(dir, "controller.json")
	// An older file that others may read: saving must take that away.
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveController(path, c); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// On Windows, os.Stat reports permissions derived only from the
	// read-only attribute, so it cannot reflect the 0600 mode SaveController
	// requested; run go test ./shared/config/ on Linux to check the modes.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v: the file holds keys", info.Mode().Perm())
	}
	if names := dirEntries(t, dir); len(names) != 1 || names[0] != "controller.json" {
		t.Errorf("files left: %v", names)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("null")) || bytes.Contains(b, []byte("[secret]")) {
		t.Errorf("saved file holds null or a redacted secret:\n%s", b)
	}
	// The file is the one place that holds the secrets themselves.
	for _, s := range []string{enc.B64.EncodeToString(c.NoiseKey.Bytes()), enc.B64.EncodeToString(c.DBKey.Bytes()),
		`"password": "` + c.Migrate.Password.Text() + `"`} {
		if !bytes.Contains(b, []byte(s)) {
			t.Errorf("saved file lacks %q:\n%s", s, b)
		}
	}
	again, err := LoadController(path)
	if err != nil || again.Node != "ctl-1" || again.String() != c.String() ||
		!bytes.Equal(again.NoiseKey.Bytes(), c.NoiseKey.Bytes()) || !bytes.Equal(again.DBKey.Bytes(), c.DBKey.Bytes()) ||
		again.Migrate.Password.Text() != c.Migrate.Password.Text() {
		t.Fatalf("round trip: %+v, %v", again, err)
	}
}

// TestSaveControllerInvalid checks that SaveController refuses to write a
// controller that fails Validate, and leaves no file behind when it refuses.
func TestSaveControllerInvalid(t *testing.T) {
	c, err := LoadController(example(t, "controller.json"))
	if err != nil {
		t.Fatal(err)
	}
	c.Name = "" // now fails wire.ValidName
	dir := t.TempDir()
	path := filepath.Join(dir, "controller.json")
	if err := SaveController(path, c); err != ErrConfig {
		t.Fatalf("got %v, want ErrConfig", err)
	}
	if names := dirEntries(t, dir); len(names) != 0 {
		t.Errorf("files after a refused save: %v", names)
	}
}

// TestSaveWeb: SaveWeb writes web.json so that LoadWeb reads the same back,
// readable for the PHP of the web interface, with no temporary file left
// over, and refuses a configuration that fails Validate without writing.
func TestSaveWeb(t *testing.T) {
	w, err := LoadWeb(example(t, "web.json"))
	if err != nil {
		t.Fatal(err)
	}
	// As the controller with role web does when a third API server joins.
	w.APIs = append(w.APIs, WebAPI{URL: "https://api-3.ngc.test:7443", TLS: pin(0xE3)})
	dir := t.TempDir()
	path := filepath.Join(dir, "web.json")
	// An older file only root may read: saving must make it readable again.
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveWeb(path, w); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// See TestSaveController for Windows.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
		t.Errorf("mode %v: the PHP of the web interface reads the file", info.Mode().Perm())
	}
	if names := dirEntries(t, dir); len(names) != 1 || names[0] != "web.json" {
		t.Errorf("files left: %v", names)
	}
	if again, err := LoadWeb(path); err != nil || !reflect.DeepEqual(again, w) {
		t.Fatalf("round trip: %+v, %v", again, err)
	}
	// Without passkeys the file keeps an empty rp, never a null.
	w.RP = proof.RP{}
	if err := SaveWeb(path, w); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || bytes.Contains(b, []byte("null")) {
		t.Errorf("saved file:\n%s\n%v", b, err)
	}
	if again, err := LoadWeb(path); err != nil || !reflect.DeepEqual(again, w) {
		t.Fatalf("round trip without passkeys: %+v, %v", again, err)
	}

	w.APIs[2].TLS = nil
	refused := filepath.Join(t.TempDir(), "web.json")
	if err := SaveWeb(refused, w); err != ErrConfig {
		t.Fatalf("API without a pin: got %v, want ErrConfig", err)
	}
	if names := dirEntries(t, filepath.Dir(refused)); len(names) != 0 {
		t.Errorf("files after a refused save: %v", names)
	}
}

func TestAnchor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust-anchor.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := trust.Anchor{Seq: 7, Hash: make([]byte, 32)}
	if err := SaveAnchor(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAnchor(path)
	if err != nil || got.Seq != 7 || len(got.Hash) != 32 {
		t.Fatalf("got %+v, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// The web server's PHP reads the anchor; see TestSaveController for Windows.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
		t.Errorf("mode %v", info.Mode().Perm())
	}
	if names := dirEntries(t, dir); len(names) != 1 {
		t.Errorf("files left: %v", names)
	}
	if _, err := LoadAnchor(filepath.Join(t.TempDir(), "gone.json")); err == nil {
		t.Error("missing file")
	}
	for _, s := range []string{`{"seq":0,"hash":"` + enc.B64.EncodeToString(make([]byte, 32)) + `"}`, `{"seq":1,"hash":null}`,
		`{"seq":1,"hash":"AAAA"}`, `{"seq":1,"hash":[1]}`} {
		if _, err := parseAnchor([]byte(s)); err != ErrConfig {
			t.Errorf("%s: %v", s, err)
		}
	}
}

// TestSaveAnchorInvalid checks that SaveAnchor refuses a Seq of 0 and a
// too-short hash, and leaves no file behind either time.
func TestSaveAnchorInvalid(t *testing.T) {
	cases := map[string]trust.Anchor{
		"seq zero":   {Seq: 0, Hash: make([]byte, 32)},
		"short hash": {Seq: 7, Hash: make([]byte, 16)},
	}
	for name, a := range cases {
		path := filepath.Join(t.TempDir(), "trust-anchor.json")
		if err := SaveAnchor(path, a); err != ErrConfig {
			t.Errorf("%s: got %v, want ErrConfig", name, err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s: file exists after a refused save: %v", name, err)
		}
	}
}

func origins(n int) []string {
	o := make([]string, n)
	for i := range o {
		o[i] = fmt.Sprintf("https://ngc-%d.example.org", i)
	}
	return o
}

// webAPIs returns n valid APIs for web.json, each with its own address and pin.
func webAPIs(n int) []WebAPI {
	apis := make([]WebAPI, n)
	for i := range apis {
		apis[i] = WebAPI{URL: fmt.Sprintf("https://api-%d.ngc.test:7443", i+1), TLS: pin(byte(0xE1 + i))}
	}
	return apis
}

// TestValidate loads a fresh, known-valid Web/API/Controller from the
// example files for every case (rather than mutating and restoring one
// shared value), so a case can never leak a change into a later one: each
// case changes exactly the field(s) its closure touches.
func TestValidate(t *testing.T) {
	webCases := map[string]struct {
		mutate func(*Web)
		valid  bool
	}{
		"without passkeys":        {func(w *Web) { w.RP.ID, w.RP.Origins = "", nil }, true},
		"eight origins":           {func(w *Web) { w.RP.Origins = origins(trust.MaxOrigins) }, true},
		"RP ID of 253 bytes":      {func(w *Web) { w.RP.ID = strings.Repeat(strings.Repeat("a", 62)+".", 4) + "a" }, true},
		"sixteen apis":            {func(w *Web) { w.APIs = webAPIs(maxList) }, true},
		"api url of 256 bytes":    {func(w *Web) { w.APIs[1].URL = "https://" + trusttest.Host(243) + ":7443" }, true},
		"origin of 256 bytes":     {func(w *Web) { w.RP.Origins = []string{"https://" + trusttest.Host(248)} }, true},
		"origin with a port":      {func(w *Web) { w.RP.Origins = []string{"https://ngc.example.org:8443"} }, true},
		"without apis":            {func(w *Web) { w.APIs = nil }, false},
		"seventeen apis":          {func(w *Web) { w.APIs = webAPIs(maxList + 1) }, false},
		"plain http":              {func(w *Web) { w.APIs[0].URL = "http://api-1.ngc.test:7443" }, false},
		"api url of 257 bytes":    {func(w *Web) { w.APIs[1].URL = "https://" + trusttest.Host(244) + ":7443" }, false},
		"api url with a path":     {func(w *Web) { w.APIs[1].URL += "/api" }, false},
		"api url without a port":  {func(w *Web) { w.APIs[1].URL = "https://api-2.ngc.test" }, false},
		"api without pin":         {func(w *Web) { w.APIs[1].TLS = nil }, false},
		"api pin of 31 bytes":     {func(w *Web) { w.APIs[1].TLS = w.APIs[1].TLS[:31] }, false},
		"api url twice":           {func(w *Web) { w.APIs[1].URL = w.APIs[0].URL }, false},
		"origin not https":        {func(w *Web) { w.RP.Origins = []string{"http://ngc.example.org"} }, false},
		"http origin":             {func(w *Web) { w.RP.Origins = append(w.RP.Origins, "http://ngc.example.org") }, false},
		"nine origins":            {func(w *Web) { w.RP.Origins = origins(trust.MaxOrigins + 1) }, false},
		"origin of 257 bytes":     {func(w *Web) { w.RP.Origins = []string{"https://" + trusttest.Host(249)} }, false},
		"origin on port 443":      {func(w *Web) { w.RP.Origins = []string{"https://ngc.example.org:443"} }, false},
		"origin with a path":      {func(w *Web) { w.RP.Origins = []string{"https://ngc.example.org/x"} }, false},
		"origin with a query":     {func(w *Web) { w.RP.Origins = []string{"https://ngc.example.org?y=1"} }, false},
		"origin with user info":   {func(w *Web) { w.RP.Origins = []string{"https://user@ngc.example.org"} }, false},
		"origin in upper case":    {func(w *Web) { w.RP.Origins = []string{"https://NGC.example.org"} }, false},
		"origin with a line feed": {func(w *Web) { w.RP.Origins = []string{"https://ngc.example.org\x0a"} }, false},
		"RP ID with a space":      {func(w *Web) { w.RP.ID = "ngc example.org" }, false},
		"RP ID in upper case":     {func(w *Web) { w.RP.ID = "NGC.example.org" }, false},
		"RP ID with a port":       {func(w *Web) { w.RP.ID = "ngc.example.org:443" }, false},
		"RP ID of 254 bytes":      {func(w *Web) { w.RP.ID = strings.Repeat(strings.Repeat("a", 62)+".", 4) + "ab" }, false},
		"RP ID without origins":   {func(w *Web) { w.RP.Origins = nil }, false},
		"origins without RP ID":   {func(w *Web) { w.RP.ID = "" }, false},
	}
	for name, c := range webCases {
		w, err := LoadWeb(example(t, "web.json"))
		if err != nil {
			t.Fatal(err)
		}
		c.mutate(w)
		if err := w.Validate(); c.valid && err != nil || !c.valid && err != ErrConfig {
			t.Errorf("web %s: %v", name, err)
		}
	}

	caCert, _, ca := testCA(t, 0xCA)
	leaf, _ := nodeCert(t, "db-1.ngc.test")
	// A self-signed certificate without basic constraints: no CA.
	unconstrained, _ := testCert(t, &x509.Certificate{SerialNumber: big.NewInt(3),
		Subject: pkix.Name{CommonName: "NGC DB CA (Test)"}, KeyUsage: x509.KeyUsageCertSign}, 0xCA, nil, nil)
	caBlock := func(b pem.Block) string { return string(pem.EncodeToMemory(&b)) }
	apiCases := map[string]struct {
		mutate func(*API)
		valid  bool
	}{
		"session lifetime of 86400":         {func(a *API) { a.SessionLifetime = session.MaxLifetime }, true},
		"url of 256 bytes":                  {func(a *API) { a.URL = "wss://" + trusttest.Host(238) + ":7443/tunnel" }, true},
		"mariadb node with an IPv6 address": {func(a *API) { a.DB.Nodes = []string{"[2001:db8::5]:3306"} }, true},
		// trust.ValidHostPort takes up to 259 bytes, a host of 253 and a port of
		// 5; the bound of 256 is this package's, so both sides of it need a case.
		"mariadb node of 256 bytes":   {func(a *API) { a.DB.Nodes = []string{trusttest.Host(250) + ":65535"} }, true},
		"mariadb node of 257 bytes":   {func(a *API) { a.DB.Nodes = []string{trusttest.Host(251) + ":65535"} }, false},
		"four CA certificates":        {func(a *API) { a.DB.CA = strings.Repeat(ca, maxCACerts) }, true},
		"CA with white space around":  {func(a *API) { a.DB.CA = " \t\r\n" + ca + "\r\n\t " + ca + " \n" }, true},
		"CA with CRLF line ends":      {func(a *API) { a.DB.CA = strings.ReplaceAll(ca, "\n", "\r\n") }, true},
		"CA of MaxCA bytes":           {func(a *API) { a.DB.CA = ca + strings.Repeat(" ", MaxCA-len(ca)) }, true},
		"SQLite":                      {func(a *API) { a.DB = validSQLite() }, true},
		"without node":                {func(a *API) { a.Node = "" }, false},
		"without url":                 {func(a *API) { a.URL = "" }, false},
		"https url":                   {func(a *API) { a.URL = "https://api-1.ngc.test:7443/tunnel" }, false},
		"url of 257 bytes":            {func(a *API) { a.URL = "wss://" + trusttest.Host(239) + ":7443/tunnel" }, false},
		"url without the tunnel path": {func(a *API) { a.URL = "wss://api-1.ngc.test:7443" }, false},
		"short key":                   {func(a *API) { a.NoiseKey = NewSecret([]byte{1}) }, false},
		"mariadb node no colon":       {func(a *API) { a.DB.Nodes = []string{"db-1"} }, false},
		"mariadb node with DSN parameters": {func(a *API) {
			a.DB.Nodes = []string{"db-1.ngc.test:3306)/ngc?allowAllFiles=true"}
		}, false},
		"mariadb node in upper case":   {func(a *API) { a.DB.Nodes = []string{"DB-1.ngc.test:3306"} }, false},
		"db password too long":         {func(a *API) { a.DB.Password = NewSecretText(strings.Repeat("x", MaxField+1)) }, false},
		"store url too long":           {func(a *API) { a.StoreURL = "https://" + trusttest.Host(249) }, false},
		"store url with a query":       {func(a *API) { a.StoreURL += "?x=1" }, false},
		"session lifetime above 86400": {func(a *API) { a.SessionLifetime = session.MaxLifetime + 1 }, false},
		"idle longer than the session": {func(a *API) { a.IdleTimeout = a.SessionLifetime + 1 }, false},
		"MariaDB without CA":           {func(a *API) { a.DB.CA = "" }, false},
		"CA not PEM":                   {func(a *API) { a.DB.CA = "NGC DB CA (Test)" }, false},
		"CA that is a leaf":            {func(a *API) { a.DB.CA = certPEM(leaf) }, false},
		"CA without basic constraints": {func(a *API) { a.DB.CA = certPEM(unconstrained) }, false},
		"five CA certificates":         {func(a *API) { a.DB.CA = strings.Repeat(ca, maxCACerts+1) }, false},
		"CA over MaxCA":                {func(a *API) { a.DB.CA = ca + strings.Repeat(" ", MaxCA-len(ca)+1) }, false},
		"text before the CA":           {func(a *API) { a.DB.CA = "CA:\n" + ca }, false},
		"text after the CA":            {func(a *API) { a.DB.CA = ca + "#\n" }, false},
		"text between CA certificates": {func(a *API) { a.DB.CA = ca + "#\n" + ca }, false},
		// pem.Decode alone would skip the broken block and return the CA.
		"CA after a broken block": {func(a *API) {
			a.DB.CA = "-----BEGIN CERTIFICATE-----\nAAAA!\n-----END CERTIFICATE-----\n" + ca
		}, false},
		"CA with a PEM header": {func(a *API) {
			a.DB.CA = caBlock(pem.Block{Type: "CERTIFICATE", Headers: map[string]string{"Comment": "NGC"}, Bytes: caCert.Raw})
		}, false},
		"CA of another PEM type": {func(a *API) { a.DB.CA = caBlock(pem.Block{Type: "TRUSTED CERTIFICATE", Bytes: caCert.Raw}) }, false},
		"CA that does not parse": {func(a *API) { a.DB.CA = caBlock(pem.Block{Type: "CERTIFICATE", Bytes: []byte("NGC")}) }, false},
		"SQLite with a CA":       {func(a *API) { a.DB = validSQLite(); a.DB.CA = ca }, false},
	}
	for name, c := range apiCases {
		a, err := LoadAPI(example(t, "api.json"))
		if err != nil {
			t.Fatal(err)
		}
		c.mutate(a)
		if err := a.Validate(); c.valid && err != nil || !c.valid && err != ErrConfig {
			t.Errorf("api %s: %v", name, err)
		}
	}

	sqlite := validSQLite()
	controllerCases := map[string]struct {
		mutate func(*Controller)
		valid  bool
	}{
		"all four roles": {func(c *Controller) {
			c.Roles, c.WebConfig = []string{tunnel.RoleWeb, tunnel.RoleAPI, tunnel.RoleDB, tunnel.RoleServices}, "/etc/ngc/web.json"
		}, true},
		"migrate to SQLite":         {func(c *Controller) { c.Migrate = &sqlite }, true},
		"name":                      {func(c *Controller) { c.Name = "Server 1" }, false},
		"api url":                   {func(c *Controller) { c.APIs[0].URL = "https://api-1.ngc.test:7443/tunnel" }, false},
		"api url with a line feed":  {func(c *Controller) { c.APIs[0].URL += "\x0a" }, false},
		"api pin of 31 bytes":       {func(c *Controller) { c.APIs[0].TLS = c.APIs[0].TLS[:31] }, false},
		"api without pin":           {func(c *Controller) { c.APIs[0].TLS = nil }, false},
		"without apis":              {func(c *Controller) { c.APIs = nil }, false},
		"unknown role":              {func(c *Controller) { c.Roles = []string{"mail"} }, false},
		"old part name":             {func(c *Controller) { c.Roles = []string{"ngc-controller"} }, false},
		"no roles":                  {func(c *Controller) { c.Roles, c.Migrate = nil, nil }, false},
		"duplicate role":            {func(c *Controller) { c.Roles = []string{tunnel.RoleDB, tunnel.RoleDB} }, false},
		"five roles":                {func(c *Controller) { c.Roles = append(c.Roles, tunnel.RoleWeb, tunnel.RoleAPI, tunnel.RoleWeb) }, false},
		"db key":                    {func(c *Controller) { c.DBKey = NewSecret(make([]byte, 32)) }, false},
		"web without webconf":       {func(c *Controller) { c.Roles = append(c.Roles, tunnel.RoleWeb) }, false},
		"db role without migrate":   {func(c *Controller) { c.Migrate = nil }, false},
		"migrate without db role":   {func(c *Controller) { c.Roles = []string{tunnel.RoleServices} }, false},
		"migrate with empty driver": {func(c *Controller) { c.Migrate.Driver = "" }, false},
		"migrate without password":  {func(c *Controller) { c.Migrate.Password = SecretText{} }, false},
		"migrate without CA":        {func(c *Controller) { c.Migrate.CA = "" }, false},
	}
	for name, c := range controllerCases {
		ctl, err := LoadController(example(t, "controller-db.json"))
		if err != nil {
			t.Fatal(err)
		}
		c.mutate(ctl)
		if err := ctl.Validate(); c.valid && err != nil || !c.valid && err != ErrConfig {
			t.Errorf("controller %s: %v", name, err)
		}
	}
}

// validSQLite is a DB configuration that passes Validate(): SQLite, an
// absolute path, a 64-byte key, no Galera nodes and no MariaDB leftovers.
func validSQLite() DB {
	return DB{Driver: DriverSQLite, Path: "/var/lib/ngc/api/api.db", Key: NewSecret(make([]byte, 64))}
}

// TestValidateSQLite pins one SQLite rule per case: each starts from a fresh
// validSQLite() and changes exactly one field away from valid.
func TestValidateSQLite(t *testing.T) {
	base := validSQLite()
	if err := base.Validate(); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	cases := map[string]func(*DB){
		"relative path":     func(d *DB) { d.Path = "var/lib/ngc/api/api.db" },
		"wrong key length":  func(d *DB) { d.Key = NewSecret(make([]byte, 32)) },
		"nodes present":     func(d *DB) { d.Nodes = []string{"db-1:3306"} },
		"leftover password": func(d *DB) { d.Password = NewSecretText("leftover") },
		"path too long":     func(d *DB) { d.Path = "/" + strings.Repeat("x", MaxField) },
		"leftover CA":       func(d *DB) { d.CA = exampleCA(t) },
	}
	for name, mutate := range cases {
		d := validSQLite()
		mutate(&d)
		if err := d.Validate(); err != ErrConfig {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// handshake connects a client with config client to a local tls.Listen with
// config server and returns the client's handshake error.
func handshake(t *testing.T, server, client *tls.Config) error {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", server)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	served := make(chan struct{})
	go func() {
		defer close(served)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(10 * time.Second))
		// The client decides; the server's result only mirrors it.
		_ = c.(*tls.Conn).Handshake()
	}()
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", ln.Addr().String(), client)
	if err == nil {
		conn.Close()
	}
	<-served
	return err
}

// TestDBTLSConfig connects to a database node behind a local TLS listener
// with DB.TLSConfig: only a certificate from the CA of DB.CA that names the
// node counts, and only over TLS 1.3. Without a CA there is no configuration.
func TestDBTLSConfig(t *testing.T) {
	_, _, other := testCA(t, 0xCB) // the same name as the example CA, another key
	der, key := nodeCert(t, "db-1.ngc.test")
	node := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	cases := []struct {
		name       string
		ca         string
		serverName string
		serverMax  uint16 // highest TLS version of the node; 0 is Go's default
		fails      func(error) bool
	}{
		{"node of the CA", exampleCA(t), "db-1.ngc.test", 0, nil},
		{"another CA", other, "db-1.ngc.test", 0,
			func(err error) bool { return errors.As(err, new(x509.UnknownAuthorityError)) }},
		{"another server name", exampleCA(t), "db-2.ngc.test", 0,
			func(err error) bool { return errors.As(err, new(x509.HostnameError)) }},
		{"node limited to TLS 1.2", exampleCA(t), "db-1.ngc.test", tls.VersionTLS12,
			func(err error) bool { return err != nil && strings.Contains(err.Error(), "protocol version") }},
	}
	for _, c := range cases {
		cfg, err := DB{Driver: DriverMariaDB, CA: c.ca}.TLSConfig(c.serverName)
		if err != nil || cfg.InsecureSkipVerify || cfg.MinVersion != tls.VersionTLS13 || cfg.ServerName != c.serverName {
			t.Fatalf("%s: %+v, %v", c.name, cfg, err)
		}
		// The certificates are valid from 2026 to 2036; a fixed clock keeps
		// the test from depending on the day it runs.
		cfg.Time = func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
		err = handshake(t, &tls.Config{Certificates: []tls.Certificate{node}, MaxVersion: c.serverMax}, cfg)
		if c.fails == nil && err != nil || c.fails != nil && !c.fails(err) {
			t.Errorf("%s: %v", c.name, err)
		}
	}

	for name, d := range map[string]DB{
		"SQLite":               validSQLite(),
		"MariaDB without CA":   {Driver: DriverMariaDB},
		"five CA certificates": {Driver: DriverMariaDB, CA: strings.Repeat(exampleCA(t), maxCACerts+1)},
	} {
		if cfg, err := d.TLSConfig("db-1.ngc.test"); cfg != nil || err != ErrConfig {
			t.Errorf("%s: %v, %v", name, cfg, err)
		}
	}
	// The name goes into the TLS handshake and names the node whose
	// certificate must match: only a host of the grammar is one.
	for name, server := range map[string]string{
		"without a server name": "",
		"in upper case":         "DB-1.ngc.test",
		"with a line feed":      "db-1.ngc.test\x0a",
	} {
		if cfg, err := (DB{Driver: DriverMariaDB, CA: exampleCA(t)}).TLSConfig(server); cfg != nil || err != ErrConfig {
			t.Errorf("server name %s: %v, %v", name, cfg, err)
		}
	}
}

// secretForms returns how a key or password could show when printed: as
// base64url, standard base64, decimal and Go byte lists, and hex. A key of
// the examples repeats one byte, so the first 8 bytes stand for all of it.
func secretForms(keys [][]byte, passwords []string) []string {
	var forms []string
	for _, k := range keys {
		head := k[:8]
		forms = append(forms, enc.B64.EncodeToString(k), base64.StdEncoding.EncodeToString(k),
			strings.Trim(fmt.Sprint(head), "[]"), strings.Trim(fmt.Sprintf("%#v", head), "[]byte{}"),
			fmt.Sprintf("%x", head), fmt.Sprintf("%X", head))
	}
	for _, p := range passwords {
		forms = append(forms, p, fmt.Sprintf("%x", p), fmt.Sprintf("%X", p))
	}
	return forms
}

// TestRedaction prints and logs each configuration in every way at hand:
// every verb of package fmt, the configuration itself, by pointer, in a
// slice, in a map and in an exported and an unexported field of another
// struct, through both handlers of log/slog and through encoding/json. No
// secret of the examples may show; the node or name does. Only the file
// holds the secrets (TestSaveController).
func TestRedaction(t *testing.T) {
	api, err := LoadAPI(example(t, "api.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctl, err := LoadController(example(t, "controller-db.json"))
	if err != nil {
		t.Fatal(err)
	}
	sqlite := validSQLite()
	sqlite.Key = NewSecret(bytes.Repeat([]byte{0x5C}, 64))
	withSQLite, err := LoadController(example(t, "controller.json"))
	if err != nil {
		t.Fatal(err)
	}
	withSQLite.Roles, withSQLite.Migrate = []string{tunnel.RoleDB}, &sqlite

	type exported struct{ Cfg any }
	type unexported struct{ cfg any }
	verbs := []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%b", "%o", "%O", "%c", "%U", "%e", "%E",
		"%f", "%F", "%g", "%G", "%t", "%T", "%p", "% x", "%#x", "%+q", "%#q", "%-300s", "%.10v", "%300v", "%w"}
	cases := []struct {
		name    string
		value   any // the configuration itself
		pointer any
		shown   string
		secrets []string
	}{
		{"api", *api, api, "api-1",
			secretForms([][]byte{api.NoiseKey.Bytes(), api.SignKey.Bytes(), api.SecretKey.Bytes(), api.ChallengeKey.Bytes()},
				[]string{api.DB.Password.Text()})},
		{"controller", *ctl, ctl, "datenbank",
			secretForms([][]byte{ctl.NoiseKey.Bytes(), ctl.DBKey.Bytes()}, []string{ctl.Migrate.Password.Text()})},
		{"controller with SQLite", *withSQLite, withSQLite, "dienste",
			secretForms([][]byte{withSQLite.NoiseKey.Bytes(), withSQLite.DBKey.Bytes(), sqlite.Key.Bytes()}, nil)},
		{"db", api.DB, &api.DB, "ngc.test", secretForms(nil, []string{api.DB.Password.Text()})},
	}
	for _, c := range cases {
		var outs []string
		for _, arg := range []any{c.value, c.pointer, []any{c.value}, map[string]any{"cfg": c.value},
			exported{c.value}, unexported{c.value}} {
			for _, verb := range verbs {
				outs = append(outs, fmt.Sprintf(verb, arg))
			}
			var buf bytes.Buffer
			slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "cfg", arg)
			slog.New(slog.NewTextHandler(&buf, nil)).Info("x", "cfg", arg)
			b, err := json.Marshal(arg)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			outs = append(outs, buf.String(), string(b))
		}
		for _, out := range outs {
			for _, s := range c.secrets {
				if strings.Contains(out, s) {
					t.Errorf("%s: %.40q shows a secret:\n%.400s", c.name, s, out)
				}
			}
		}
		var logged bytes.Buffer
		slog.New(slog.NewJSONHandler(&logged, nil)).Info("x", "cfg", c.pointer)
		encoded, _ := json.Marshal(c.value)
		for _, out := range []string{fmt.Sprintf("%v", c.value), fmt.Sprintf("%+v", c.pointer), logged.String(), string(encoded)} {
			if !strings.Contains(out, c.shown) {
				t.Errorf("%s: %q missing:\n%s", c.name, c.shown, out)
			}
		}
		if s := fmt.Sprint(c.value); !strings.Contains(s, "[secret]") {
			t.Errorf("%s: [secret] missing: %s", c.name, s)
		}
	}
	empty := DB{Driver: DriverMariaDB}
	if s := empty.String(); strings.Contains(s, "[secret]") {
		t.Errorf("unset secrets shown as set: %s", s)
	}
}

// TestLogValues: the logs show an API's tunnel address, each API of a
// controller as address, Noise key and TLS pin, and of a database CA only
// whether it is set.
func TestLogValues(t *testing.T) {
	api, err := LoadAPI(example(t, "api.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctl, err := LoadController(example(t, "controller-db.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s := api.String(); !strings.Contains(s, "url=wss://api-1.ngc.test:7443/tunnel ") || !strings.Contains(s, " ca=true]") ||
		strings.Contains(s, "CERTIFICATE") {
		t.Errorf("api: %s", s)
	}
	dials := "wss://api-1.ngc.test:7443/tunnel " + enc.B64.EncodeToString(ctl.APIs[0].Key) + " " + enc.B64.EncodeToString(pin(0xE1))
	if s := ctl.String(); !strings.Contains(s, "apis=["+dials+"]") || !strings.Contains(s, " ca=true]") ||
		strings.Contains(s, "CERTIFICATE") {
		t.Errorf("controller: %s", s)
	}
	if s := validSQLite().String(); !strings.Contains(s, " ca=false]") {
		t.Errorf("SQLite: %s", s)
	}
}

// TestSecret: a secret keeps a copy of its value, shows it only through
// Bytes and Text, and an unset one is left out of a file.
func TestSecret(t *testing.T) {
	b := []byte{1, 2, 3}
	s := NewSecret(b)
	b[0] = 9
	if !bytes.Equal(s.Bytes(), []byte{1, 2, 3}) || s.String() != "[secret]" || fmt.Sprintf("%#v", s) != `"[secret]"` {
		t.Errorf("secret %v, %s", s.Bytes(), s)
	}
	if NewSecret(nil).Bytes() != nil || !NewSecret(nil).IsZero() || NewSecretText("").Text() != "" || (SecretText{}).String() != "" {
		t.Error("unset secrets")
	}
	var back struct {
		Key  Secret     `json:"key"`
		Word SecretText `json:"word"`
	}
	if err := decode(anchorBudget, []byte(`{"key":"AQID","word":"geheim"}`), &back); err != nil ||
		!bytes.Equal(back.Key.Bytes(), []byte{1, 2, 3}) || back.Word.Text() != "geheim" {
		t.Errorf("decode: %v, %v", back, err)
	}
	for _, in := range []string{`{"key":"AQI="}`, `{"key":"AQ\nID"}`, `{"key":1}`, `{"word":1}`, "{\"word\":\"\xff\"}"} {
		if err := decode(anchorBudget, []byte(in), &back); err != ErrConfig {
			t.Errorf("%q: got %v, want %v", in, err, ErrConfig)
		}
	}
	file, err := marshalFile(DB{Driver: DriverSQLite, Path: "/x.db", Key: NewSecret([]byte{1, 2, 3})})
	if err != nil || string(file) != "{\n  \"driver\": \"sqlite\",\n  \"path\": \"/x.db\",\n  \"key\": \"AQID\"\n}\n" {
		t.Errorf("file %q, %v", file, err)
	}
}

// TestSecretBytesIsACopy: whoever changes what Bytes returned changes neither
// the Secret nor a copy of it, which shares its key.
func TestSecretBytesIsACopy(t *testing.T) {
	s := NewSecret([]byte{1, 2, 3})
	shared := s
	got := s.Bytes()
	got[0] = 9
	if !bytes.Equal(s.Bytes(), []byte{1, 2, 3}) || !bytes.Equal(shared.Bytes(), []byte{1, 2, 3}) {
		t.Errorf("secret changed through Bytes: %v, %v", s.Bytes(), shared.Bytes())
	}
	if &s.Bytes()[0] == &s.Bytes()[0] {
		t.Error("Bytes hands out the same array twice")
	}
}

// TestRedactedFileRefused: encoding/json writes the stand-in "[secret]" for
// every secret. A file written that way must fail to load, never store the
// stand-in as a key or password; SaveController writes the real file.
func TestRedactedFileRefused(t *testing.T) {
	api, err := LoadAPI(example(t, "api.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctl, err := LoadController(example(t, "controller-db.json"))
	if err != nil {
		t.Fatal(err)
	}
	for name, parse := range map[string]func() error{
		"api.json": func() error { b, _ := json.Marshal(api); _, err := parseAPI(b); return err },
		"controller.json": func() error {
			b, _ := json.Marshal(ctl)
			_, err := parseController(b)
			return err
		},
		// The password alone: everything else of the database is valid.
		"database of api.json": func() error {
			b, _ := json.Marshal(api.DB)
			var d DB
			return decode(apiBudget, b, &d)
		},
		// The real api.json with only its password replaced by the stand-in.
		"api.json, password only": func() error {
			b, err := os.ReadFile(example(t, "api.json"))
			if err != nil {
				t.Fatal(err)
			}
			b = bytes.Replace(b, []byte(`"password": "changeme"`), []byte(`"password": "[secret]"`), 1)
			if !bytes.Contains(b, []byte(`"password": "[secret]"`)) {
				t.Fatal("password not replaced")
			}
			_, err = parseAPI(b)
			return err
		},
	} {
		if err := parse(); err != ErrConfig {
			t.Errorf("%s: got %v, want %v", name, err, ErrConfig)
		}
	}
}

// allocated returns how many bytes f allocates.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// hostile returns prefix, then as many empty objects as fit, then suffix:
// exactly size bytes.
func hostile(prefix, suffix string, size int) []byte {
	var b bytes.Buffer
	b.Grow(size)
	b.WriteString(prefix + "{}")
	for b.Len()+len(",{}")+len(suffix) <= size {
		b.WriteString(",{}")
	}
	for b.Len()+len(suffix) < size {
		b.WriteByte(' ')
	}
	b.WriteString(suffix)
	return b.Bytes()
}

// TestParsersBoundAllocation feeds each parser a file of MaxConfig bytes with
// more list elements than its contract allows, or a list where it allows
// none: refusing it must allocate next to nothing.
func TestParsersBoundAllocation(t *testing.T) {
	cases := []struct {
		name  string
		in    []byte
		parse func([]byte) error
	}{
		{"parseWeb", hostile(`{"apis":[`, `]}`, MaxConfig), func(b []byte) error { _, err := parseWeb(b); return err }},
		{"parseWeb, origins", hostile(`{"rp":{"origins":[`, `]}}`, MaxConfig), func(b []byte) error { _, err := parseWeb(b); return err }},
		{"parseAPI", hostile(`{"db":{"nodes":[`, `]}}`, MaxConfig), func(b []byte) error { _, err := parseAPI(b); return err }},
		{"parseController", hostile(`{"roles":[`, `]}`, MaxConfig), func(b []byte) error { _, err := parseController(b); return err }},
		{"parseController, migrate", hostile(`{"migrate":{"nodes":[`, `]}}`, MaxConfig), func(b []byte) error { _, err := parseController(b); return err }},
		{"parseAnchor", hostile(`{"hash":[`, `]}`, MaxConfig), func(b []byte) error { _, err := parseAnchor(b); return err }},
	}
	for _, c := range cases {
		var err error
		n := allocated(func() { err = c.parse(c.in) })
		t.Logf("%s: %d bytes allocated", c.name, n)
		if n > 64<<10 {
			t.Errorf("%s: %d bytes allocated", c.name, n)
		}
		if err != ErrConfig {
			t.Errorf("%s: got %v, want %v", c.name, err, ErrConfig)
		}
	}
}

// FuzzLoad fuzzes the parse functions directly, in memory: no file is
// written per iteration, only JSON bytes are decoded and validated.
func FuzzLoad(f *testing.F) {
	for _, name := range []string{"web.json", "api.json", "controller.json", "controller-db.json"} {
		b, err := os.ReadFile(filepath.Join("..", "testdata", "config", name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Add([]byte(`{"seq":1,"hash":"` + enc.B64.EncodeToString(make([]byte, 32)) + `"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = parseWeb(b)
		_, _ = parseAPI(b)
		_, _ = parseController(b)
		_, _ = parseAnchor(b)
	})
}

// FuzzParseCA feeds the parser of DB.CA directly, where FuzzLoad reaches it
// only through a JSON string. What it accepts is 1 to maxCACerts CA
// certificates, one per PEM block, with nothing but white space outside the
// blocks, and TLSConfig takes it too.
func FuzzParseCA(f *testing.F) {
	ca := exampleCA(f)
	leaf, _ := nodeCert(f, "db-1.ngc.test")
	for _, s := range []string{ca, " \t\r\n" + ca + "\r\n" + ca, strings.ReplaceAll(ca, "\n", "\r\n"), strings.Repeat(ca, maxCACerts+1),
		certPEM(leaf), "-----BEGIN CERTIFICATE-----\nAAAA!\n-----END CERTIFICATE-----\n" + ca, "CA:\n" + ca, ca + "#", ""} {
		f.Add(s)
	}
	const end = "-----END CERTIFICATE-----"
	f.Fuzz(func(t *testing.T, s string) {
		certs, err := parseCA(s)
		if err != nil {
			if certs != nil || err != ErrConfig {
				t.Fatalf("refused with %v, %d certificates", err, len(certs))
			}
			return
		}
		if len(s) > MaxCA || len(certs) == 0 || len(certs) > maxCACerts ||
			strings.Count(s, "-----BEGIN") != len(certs) || strings.Count(s, end) != len(certs) {
			t.Fatalf("accepted %q as %d certificates", s, len(certs))
		}
		rest := s
		for _, c := range certs {
			if !c.BasicConstraintsValid || !c.IsCA {
				t.Fatalf("accepted a certificate that is no CA: %q", s)
			}
			if strings.Trim(rest[:strings.Index(rest, "-----BEGIN")], pemSpace) != "" {
				t.Fatalf("accepted text outside the blocks: %q", s)
			}
			rest = rest[strings.Index(rest, end)+len(end):]
		}
		if strings.Trim(rest, pemSpace) != "" {
			t.Fatalf("accepted text after the blocks: %q", s)
		}
		if _, err := (DB{Driver: DriverMariaDB, CA: s}).TLSConfig("db-1.ngc.test"); err != nil {
			t.Fatalf("TLSConfig: %v", err)
		}
	})
}
