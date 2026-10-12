package apache

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

// apiNode builds an API node of the trust list with its TLS certificate, as a
// paired API stands in the list: fill picks the keys, host the address.
func apiNode(id, host string, fill byte) (trust.Node, []byte, ed25519.PrivateKey) {
	dh, sign := trusttest.NodeKeys(fill)
	cert, pin := trusttest.APITLS(fill, host)
	n := trust.Node{
		ID:    id,
		Kind:  trust.NodeAPI,
		Name:  id,
		Noise: enc.Bytes(dh.Public),
		Sign:  enc.Bytes(sign.Public().(ed25519.PublicKey)),
		URL:   "wss://" + host + ":7443/tunnel",
		TLS:   enc.Bytes(pin),
	}
	if !trust.ValidAPINode(n) {
		panic("test node does not pass ValidAPINode")
	}
	return n, cert.Certificate[0], cert.PrivateKey.(ed25519.PrivateKey)
}

// renewed signs a second certificate for the same key: what an API serves
// after it has renewed its certificate, which the pin cannot tell from the
// first one because the key did not change.
func renewed(host string, key ed25519.PrivateKey) []byte {
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2037, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(zeroReader{}, tmpl, tmpl, key.Public(), key)
	if err != nil {
		panic(err)
	}
	return der
}

// zeroReader keeps the test certificates fixed; Ed25519 signs without chance.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// stand is one Writer with its files in a directory of its own and counters
// for what it did to Apache.
type stand struct {
	w            *Writer
	certs        map[string][]byte // tunnel address -> certificate that server sends
	testFails    bool
	tests        int
	reloads      int
	webConfig    string
	trustFile    string
	balancerFile string
}

func newStand(t *testing.T) *stand {
	t.Helper()
	dir := t.TempDir()
	s := &stand{
		certs:        map[string][]byte{},
		webConfig:    filepath.Join(dir, "web.json"),
		trustFile:    filepath.Join(dir, "api-trust.pem"),
		balancerFile: filepath.Join(dir, "apache-apis.conf"),
	}
	s.w = New(Options{
		WebConfig:    s.webConfig,
		TrustFile:    s.trustFile,
		BalancerFile: s.balancerFile,
		Fetch: func(_ context.Context, n trust.Node) ([]byte, error) {
			der, ok := s.certs[n.URL]
			if !ok {
				return nil, errors.New("no server at this address")
			}
			return der, nil
		},
		ConfigTest: func(context.Context) error {
			s.tests++
			if s.testFails {
				return errors.New("Syntax error on line 1")
			}
			return nil
		},
		Reload: func(context.Context) error { s.reloads++; return nil },
	})
	return s
}

// read gives the contents of file, and "" if it is not there.
func (s *stand) read(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	return string(b)
}

func TestRenderMatchesTheTemplate(t *testing.T) {
	got := Render([]config.WebAPI{
		{URL: "https://api-1.ngc.test:7443", TLS: bytes.Repeat([]byte{0xE1}, 32)},
		{URL: "https://api-2.ngc.test:7443", TLS: bytes.Repeat([]byte{0xE2}, 32)},
	})
	want, err := os.ReadFile("testdata/apache-apis.conf")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("balancer file differs:\n%s", got)
	}
}

// TestRenderNamesTheFilesOfTheContract keeps the template from carrying a
// path of its own: both come from package config, so one spelling cannot
// drift away from the other.
func TestRenderNamesTheFilesOfTheContract(t *testing.T) {
	got := string(Render([]config.WebAPI{{URL: "https://api-1.ngc.test:7443", TLS: bytes.Repeat([]byte{1}, 32)}}))
	for _, want := range []string{
		"SSLProxyCACertificateFile " + config.APITrustFile + "\n",
		"# " + config.BalancerFile + "\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered file does not contain %q:\n%s", want, got)
		}
	}
}

func TestRenderRefusesAnAddressOutsideTheGrammar(t *testing.T) {
	pin := bytes.Repeat([]byte{0xE1}, 32)
	good := config.WebAPI{URL: "https://api-1.ngc.test:7443", TLS: pin}
	// The way to the grammar must be open, or every case below would pass
	// for the wrong reason.
	if Render([]config.WebAPI{good}) == nil {
		t.Fatal("the plain address renders nothing, so this test measures an earlier wall")
	}
	cases := map[string]config.WebAPI{
		"a path":                {URL: "https://api-1.ngc.test:7443/x", TLS: pin},
		"a second line":         {URL: "https://api-1.ngc.test:7443\nRequire all granted", TLS: pin},
		"a closing quote":       {URL: "https://api-1.ngc.test:7443\" retry=0 \"", TLS: pin},
		"a space and an option": {URL: "https://api-1.ngc.test:7443 keepalive=Off", TLS: pin},
		"a carriage return":     {URL: "https://api-1.ngc.test:7443\rProxySet lbmethod=bytraffic", TLS: pin},
		"an upper case host":    {URL: "https://API-1.ngc.test:7443", TLS: pin},
		"no port":               {URL: "https://api-1.ngc.test", TLS: pin},
		"plain http":            {URL: "http://api-1.ngc.test:7443", TLS: pin},
		"a short pin":           {URL: "https://api-2.ngc.test:7443", TLS: pin[:31]},
	}
	for name, bad := range cases {
		// The good entry comes first, so something would be rendered if the
		// bad one were merely left out instead of refusing the whole file.
		if got := Render([]config.WebAPI{good, bad}); got != nil {
			t.Errorf("%s: rendered anyway:\n%s", name, got)
		}
	}
	if got := Render(nil); got != nil {
		t.Errorf("no API at all: rendered anyway:\n%s", got)
	}
}

func TestSyncWritesOnlyCertificatesThatMatchTheirPin(t *testing.T) {
	n1, der1, _ := apiNode("api-1", "api-1.ngc.test", 0xA1)
	n2, der2, _ := apiNode("api-2", "api-2.ngc.test", 0xA2)
	// Intermediate check: the same two nodes with their own certificates go
	// through, so a refusal below comes from the pin and not from the way in.
	ok := newStand(t)
	ok.certs[n1.URL] = der1
	ok.certs[n2.URL] = der2
	changed, err := ok.w.Sync(context.Background(), []trust.Node{n1, n2})
	if err != nil || !changed {
		t.Fatalf("matching certificates: changed=%v err=%v", changed, err)
	}
	if ok.tests != 1 || ok.reloads != 1 {
		t.Fatalf("matching certificates: tests=%d reloads=%d", ok.tests, ok.reloads)
	}
	if n := strings.Count(ok.read(t, ok.trustFile), "BEGIN CERTIFICATE"); n != 2 {
		t.Fatalf("matching certificates: %d certificates in the trust file", n)
	}

	// The second server answers with the certificate of the first: another
	// key, so its pin does not match.
	bad := newStand(t)
	bad.certs[n1.URL] = der1
	bad.certs[n2.URL] = der1
	changed, err = bad.w.Sync(context.Background(), []trust.Node{n1, n2})
	if !errors.Is(err, ErrPin) {
		t.Fatalf("wrong pin: err=%v, want ErrPin", err)
	}
	if changed {
		t.Error("wrong pin: reported a change")
	}
	if got := bad.read(t, bad.trustFile); got != "" {
		t.Errorf("wrong pin: trust file written:\n%s", got)
	}
	if got := bad.read(t, bad.balancerFile); got != "" {
		t.Errorf("wrong pin: balancer file written:\n%s", got)
	}
	if bad.tests != 0 || bad.reloads != 0 {
		t.Errorf("wrong pin: tests=%d reloads=%d, want 0 and 0", bad.tests, bad.reloads)
	}
}

func TestSyncReloadsOnlyAfterConfigtest(t *testing.T) {
	n1, der1, _ := apiNode("api-1", "api-1.ngc.test", 0xB1)
	n2, der2, _ := apiNode("api-2", "api-2.ngc.test", 0xB2)
	s := newStand(t)
	s.certs[n1.URL] = der1
	s.certs[n2.URL] = der2
	if _, err := s.w.Sync(context.Background(), []trust.Node{n1}); err != nil {
		t.Fatal(err)
	}
	before := s.read(t, s.balancerFile)
	beforeTrust := s.read(t, s.trustFile)
	if !strings.Contains(before, "api-1.ngc.test") || strings.Contains(before, "api-2.ngc.test") {
		t.Fatalf("the first sync did not write the first API alone:\n%s", before)
	}

	s.testFails = true
	changed, err := s.w.Sync(context.Background(), []trust.Node{n1, n2})
	if !errors.Is(err, ErrApache) {
		t.Fatalf("failing configtest: err=%v, want ErrApache", err)
	}
	if changed {
		t.Error("failing configtest: reported a change")
	}
	if s.reloads != 1 {
		t.Errorf("failing configtest: %d reloads in all, want the one of the first sync", s.reloads)
	}
	if got := s.read(t, s.balancerFile); got != before {
		t.Errorf("failing configtest: the previous balancer file is gone:\n%s", got)
	}
	if got := s.read(t, s.trustFile); got != beforeTrust {
		t.Errorf("failing configtest: the previous trust file is gone:\n%s", got)
	}
}

// TestSyncKeepsThePasskeySettings guards the other half of web.json: the
// controller owns the API list there, not the settings of the passkeys.
func TestSyncKeepsThePasskeySettings(t *testing.T) {
	n1, der1, _ := apiNode("api-1", "api-1.ngc.test", 0xC1)
	s := newStand(t)
	s.certs[n1.URL] = der1
	rp := proof.RP{ID: "ngc.test", Origins: []string{"https://ngc.test"}}
	if err := config.SaveWeb(s.webConfig, &config.Web{
		APIs: []config.WebAPI{{URL: "https://old.ngc.test:7443", TLS: bytes.Repeat([]byte{9}, 32)}},
		RP:   rp,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Sync(context.Background(), []trust.Node{n1}); err != nil {
		t.Fatal(err)
	}
	w, err := config.LoadWeb(s.webConfig)
	if err != nil {
		t.Fatal(err)
	}
	if w.RP.ID != rp.ID || len(w.RP.Origins) != 1 || w.RP.Origins[0] != rp.Origins[0] {
		t.Errorf("passkey settings lost: %+v", w.RP)
	}
	if len(w.APIs) != 1 || w.APIs[0].URL != "https://api-1.ngc.test:7443" {
		t.Errorf("API list not taken over: %+v", w.APIs)
	}
}

// TestSyncDoesNotReloadWithoutAChange keeps the daily run from restarting
// Apache's workers for nothing.
func TestSyncDoesNotReloadWithoutAChange(t *testing.T) {
	n1, der1, _ := apiNode("api-1", "api-1.ngc.test", 0xD1)
	s := newStand(t)
	s.certs[n1.URL] = der1
	if _, err := s.w.Sync(context.Background(), []trust.Node{n1}); err != nil {
		t.Fatal(err)
	}
	changed, err := s.w.Sync(context.Background(), []trust.Node{n1})
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("the second sync reported a change")
	}
	if s.tests != 1 || s.reloads != 1 {
		t.Errorf("tests=%d reloads=%d, want 1 and 1", s.tests, s.reloads)
	}
}

func TestCompareRewritesARenewedCertificate(t *testing.T) {
	n1, der1, key1 := apiNode("api-1", "api-1.ngc.test", 0xE1)
	_, der2, _ := apiNode("api-2", "api-2.ngc.test", 0xE2)
	s := newStand(t)
	s.certs[n1.URL] = der1
	if _, err := s.w.Sync(context.Background(), []trust.Node{n1}); err != nil {
		t.Fatal(err)
	}
	// Intermediate check: nothing to do while the certificate stands.
	changed, err := s.w.Compare(context.Background(), n1)
	if err != nil || changed {
		t.Fatalf("unchanged certificate: changed=%v err=%v", changed, err)
	}
	if s.reloads != 1 {
		t.Fatalf("unchanged certificate: %d reloads", s.reloads)
	}

	fresh := renewed("api-1.ngc.test", key1)
	if bytes.Equal(fresh, der1) {
		t.Fatal("the renewed certificate is the old one, so this test measures nothing")
	}
	s.certs[n1.URL] = fresh
	changed, err = s.w.Compare(context.Background(), n1)
	if err != nil || !changed {
		t.Fatalf("renewed certificate: changed=%v err=%v", changed, err)
	}
	if s.tests != 2 || s.reloads != 2 {
		t.Errorf("renewed certificate: tests=%d reloads=%d, want 2 and 2", s.tests, s.reloads)
	}
	block, _ := pem.Decode([]byte(s.read(t, s.trustFile)))
	if block == nil || !bytes.Equal(block.Bytes, fresh) {
		t.Error("the trust file does not hold the renewed certificate")
	}

	// Another key fails at the pin, whatever it sends.
	s.certs[n1.URL] = der2
	changed, err = s.w.Compare(context.Background(), n1)
	if !errors.Is(err, ErrPin) {
		t.Fatalf("another key: err=%v, want ErrPin", err)
	}
	if changed {
		t.Error("another key: reported a change")
	}
	block, _ = pem.Decode([]byte(s.read(t, s.trustFile)))
	if block == nil || !bytes.Equal(block.Bytes, fresh) {
		t.Error("another key: the trust file was touched")
	}
	if s.reloads != 2 {
		t.Errorf("another key: %d reloads, want 2", s.reloads)
	}
}

// TestCompareLeavesAnUnknownNodeToSync says who owns the list: Compare only
// refreshes a certificate that is in the file already.
func TestCompareLeavesAnUnknownNodeToSync(t *testing.T) {
	n1, der1, _ := apiNode("api-1", "api-1.ngc.test", 0xF1)
	n2, der2, _ := apiNode("api-2", "api-2.ngc.test", 0xF2)
	s := newStand(t)
	s.certs[n1.URL] = der1
	s.certs[n2.URL] = der2
	if _, err := s.w.Sync(context.Background(), []trust.Node{n1}); err != nil {
		t.Fatal(err)
	}
	changed, err := s.w.Compare(context.Background(), n2)
	if err != nil || changed {
		t.Fatalf("unknown node: changed=%v err=%v", changed, err)
	}
	if n := strings.Count(s.read(t, s.trustFile), "BEGIN CERTIFICATE"); n != 1 {
		t.Errorf("%d certificates in the trust file, want 1", n)
	}
	if s.reloads != 1 {
		t.Errorf("%d reloads, want 1", s.reloads)
	}
}

// TestSyncRefusesANodeOutsideTheGrammar measures the way in: a node whose
// address is no tunnel address never reaches Apache's files.
func TestSyncRefusesANodeOutsideTheGrammar(t *testing.T) {
	n1, der1, _ := apiNode("api-1", "api-1.ngc.test", 0x11)
	// Intermediate check: this node goes through, so every refusal below
	// comes from what was changed in it and not from the way in.
	ok := newStand(t)
	ok.certs[n1.URL] = der1
	if changed, err := ok.w.Sync(context.Background(), []trust.Node{n1}); err != nil || !changed {
		t.Fatalf("the plain node does not go through: changed=%v err=%v", changed, err)
	}

	// Each of these is caught by one check alone: the address by the grammar,
	// the other two only by trust.ValidAPINode. The certificate they serve is
	// the right one, so nothing else would stop them.
	cases := map[string]func(n *trust.Node){
		"a second line in the address": func(n *trust.Node) {
			n.URL = "wss://api-1.ngc.test:7443/tunnel\nRequire all granted"
		},
		"no noise key":          func(n *trust.Node) { n.Noise = nil },
		"a name with a newline": func(n *trust.Node) { n.Name = "api\n1" },
		"a root list on an API": func(n *trust.Node) { n.RootRoles = []string{"admin"} },
	}
	for name, spoil := range cases {
		s := newStand(t)
		s.certs[n1.URL] = der1
		bad := n1
		spoil(&bad)
		if _, err := s.w.Sync(context.Background(), []trust.Node{bad}); !errors.Is(err, ErrForm) {
			t.Errorf("%s: err=%v, want ErrForm", name, err)
		}
		if got := s.read(t, s.balancerFile); got != "" {
			t.Errorf("%s: balancer file written:\n%s", name, got)
		}
		if got := s.read(t, s.trustFile); got != "" {
			t.Errorf("%s: trust file written:\n%s", name, got)
		}
		if s.tests != 0 || s.reloads != 0 {
			t.Errorf("%s: tests=%d reloads=%d, want 0 and 0", name, s.tests, s.reloads)
		}
	}
}
