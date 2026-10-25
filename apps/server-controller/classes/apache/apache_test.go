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
	// key, so its pin does not match. It is left OUT, and the one that is
	// good is written.
	//
	// THIS CASE USED TO ASSERT THAT NOTHING WAS WRITTEN AT ALL, and that
	// decision was changed on 18.10.2026 for a measured reason, not for
	// convenience. All-or-nothing means one server froze what Apache knew for
	// the whole installation, and it froze it at the worst moment: the list had
	// just changed because a server moved or joined. Measured at a two-machine
	// installation, with the controller's own line: `WARN Apache could not be
	// brought in line with the trust list err="dial tcp …:7443: connect: no
	// route to host"`, twice, while the balancer went on naming a server that
	// was not there.
	//
	// What is unchanged, and is what this case's name is about: a certificate
	// whose pin does not match NEVER reaches the trust file, and the member it
	// belongs to never reaches the balancer. Leaving such a member in is the
	// expensive mistake, also measured: Apache then answers the client 500
	// (AH00898 "Error during SSL Handshake with remote server"), 1 in 24 and
	// 1 in 40 requests, and does not mark that worker as failed. A member it
	// simply cannot reach costs an answer from nobody: 30 of 30 requests were
	// answered right over the other member.
	bad := newStand(t)
	bad.certs[n1.URL] = der1
	bad.certs[n2.URL] = der1
	changed, err = bad.w.Sync(context.Background(), []trust.Node{n1, n2})
	if !errors.Is(err, ErrPin) {
		t.Fatalf("wrong pin: err=%v, want ErrPin", err)
	}
	// Written AND an error: the good member is in, the bad one is named.
	if !changed {
		t.Error("wrong pin: the good server was not written")
	}
	if n := strings.Count(bad.read(t, bad.trustFile), "BEGIN CERTIFICATE"); n != 1 {
		t.Errorf("wrong pin: %d certificates in the trust file, want the good one alone", n)
	}
	conf := bad.read(t, bad.balancerFile)
	if !strings.Contains(conf, "api-1.ngc.test:7443") {
		t.Errorf("wrong pin: the good server is not in the balancer:\n%s", conf)
	}
	if strings.Contains(conf, "api-2.ngc.test:7443") {
		t.Errorf("wrong pin: the server with the wrong pin reached the balancer:\n%s", conf)
	}
	if bad.tests != 1 || bad.reloads != 1 {
		t.Errorf("wrong pin: tests=%d reloads=%d, want 1 and 1", bad.tests, bad.reloads)
	}

	// And the floor of the decision: if NOT ONE server can be written, nothing
	// is. A balancer without a member makes Apache answer every /api/ with
	// 503, so what it has keeps working.
	none := newStand(t)
	none.certs[n1.URL] = der2
	none.certs[n2.URL] = der1
	changed, err = none.w.Sync(context.Background(), []trust.Node{n1, n2})
	if !errors.Is(err, ErrPin) || !errors.Is(err, ErrForm) {
		t.Fatalf("no server at all: err=%v, want both ErrPin and ErrForm", err)
	}
	if changed {
		t.Error("no server at all: reported a change")
	}
	if got := none.read(t, none.balancerFile); got != "" {
		t.Errorf("no server at all: balancer written:\n%s", got)
	}
	if none.tests != 0 || none.reloads != 0 {
		t.Errorf("no server at all: tests=%d reloads=%d, want 0 and 0", none.tests, none.reloads)
	}
}

// A server that is away does not hold the others up. This is the finding it
// was built for: one API that could not be reached used to make Sync write
// nothing at all, so the balancer went on naming a server that had moved.
//
// The node that was away comes back by itself: node.watchWeb runs Sync again
// on every change of the trust list and once a day.
func TestSyncWritesTheServersItCouldReach(t *testing.T) {
	n1, der1, _ := apiNode("api-1", "api-1.ngc.test", 0xD1)
	n2, der2, _ := apiNode("api-2", "api-2.ngc.test", 0xD2)

	// Intermediate check, so a refusal below comes from the fetch and not
	// from the way in: both reachable, both written.
	ok := newStand(t)
	ok.certs[n1.URL] = der1
	ok.certs[n2.URL] = der2
	if changed, err := ok.w.Sync(context.Background(), []trust.Node{n1, n2}); err != nil || !changed {
		t.Fatalf("both reachable: changed=%v err=%v", changed, err)
	}
	if n := strings.Count(ok.read(t, ok.trustFile), "BEGIN CERTIFICATE"); n != 2 {
		t.Fatalf("both reachable: %d certificates", n)
	}

	// Now the second one is away: no certificate at its address.
	away := newStand(t)
	away.certs[n1.URL] = der1
	changed, err := away.w.Sync(context.Background(), []trust.Node{n1, n2})
	if err == nil {
		t.Fatal("a server that was away was not named")
	}
	if !strings.Contains(err.Error(), "api-2") {
		t.Errorf("the error does not name the server that was away: %v", err)
	}
	if !changed {
		t.Error("the server that could be reached was not written")
	}
	conf := away.read(t, away.balancerFile)
	if !strings.Contains(conf, "api-1.ngc.test:7443") {
		t.Errorf("the reachable server is not in the balancer:\n%s", conf)
	}
	if strings.Contains(conf, "api-2.ngc.test:7443") {
		t.Errorf("the server that was away reached the balancer:\n%s", conf)
	}
	if n := strings.Count(away.read(t, away.trustFile), "BEGIN CERTIFICATE"); n != 1 {
		t.Errorf("%d certificates in the trust file, want the reachable one alone", n)
	}
	if away.tests != 1 || away.reloads != 1 {
		t.Errorf("tests=%d reloads=%d, want 1 and 1", away.tests, away.reloads)
	}

	// Every one away: nothing is written, and the web.json of the moment
	// before stays as it is.
	none := newStand(t)
	changed, err = none.w.Sync(context.Background(), []trust.Node{n1, n2})
	if !errors.Is(err, ErrForm) {
		t.Fatalf("every server away: err=%v, want ErrForm among it", err)
	}
	if changed {
		t.Error("every server away: reported a change")
	}
	if got := none.read(t, none.balancerFile); got != "" {
		t.Errorf("every server away: balancer written:\n%s", got)
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

// ulhContract is the contract that carries the balancer template,
// shared/ulh.md. It is read off the disk on purpose: the point of the case
// below is that the document and this program cannot drift apart, so the
// document that ships is the one it has to be measured against.
const ulhContract = "../../../../shared/ulh.md"

// balancerFromContract cuts the single ```apache block out of shared/ulh.md
// and undoes the four spaces the list item indents it by. It fails loudly
// rather than returning an empty string: a block that moved, got a second
// twin or lost its fence would otherwise turn this case green for the wrong
// reason.
func balancerFromContract(t *testing.T) []byte {
	t.Helper()
	md, err := os.ReadFile(ulhContract)
	if err != nil {
		t.Fatal(err)
	}
	const (
		open  = "    ```apache\n"
		close = "    ```\n"
	)
	if n := strings.Count(string(md), "```apache"); n != 1 {
		t.Fatalf("%s holds %d apache blocks, so there is no single form to compare against", ulhContract, n)
	}
	_, after, ok := strings.Cut(string(md), open)
	if !ok {
		t.Fatalf("%s has no apache block indented into its list item", ulhContract)
	}
	body, _, ok := strings.Cut(after, close)
	if !ok {
		t.Fatalf("the apache block in %s is never closed", ulhContract)
	}
	var out bytes.Buffer
	for _, line := range strings.SplitAfter(body, "\n") {
		if line == "" {
			continue
		}
		if line != "\n" && !strings.HasPrefix(line, "    ") {
			t.Fatalf("line %q of the template is not indented into the list item", line)
		}
		out.WriteString(strings.TrimPrefix(line, "    "))
	}
	if out.Len() == 0 {
		t.Fatalf("the apache block in %s is empty", ulhContract)
	}
	return out.Bytes()
}

// TestBalancerTemplateMatchesTheContract is the bolt the rule asked for:
// shared/ulh.md says the controller writes exactly the form printed there, so
// that form is compared byte for byte with what Render produces for the two
// APIs the template names. Whichever side moves alone, this case goes red -
// a reader is not the only thing standing between the two spellings any more.
//
// What this does not measure: nothing here starts Apache. There is none on
// the machine this was written on, so that the directives are also accepted
// by Apache is unmeasured, here and in every other case of this package.
func TestBalancerTemplateMatchesTheContract(t *testing.T) {
	want := balancerFromContract(t)
	got := Render([]config.WebAPI{
		{URL: "https://api-1.ngc.test:7443", TLS: bytes.Repeat([]byte{0xE1}, 32)},
		{URL: "https://api-2.ngc.test:7443", TLS: bytes.Repeat([]byte{0xE2}, 32)},
	})
	if got == nil {
		t.Fatal("Render wrote nothing for two plain addresses, so this case measures an earlier wall")
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the controller and %s write two different files.\ncontroller:\n%s\ncontract:\n%s", ulhContract, got, want)
	}
}

// TestWorkerParametersStayOnTheBalancerMember is the case the byte-wise
// comparison above cannot be: testdata/apache-apis.conf is whatever we wrote
// into it, so a line Apache refuses matches itself just fine. Measured on a
// real Debian 13 with Apache 2.4.69 on 16.10.2026: "upgrade=websocket" on the
// ProxyPass of a balancer:// target is
//
//	AH00526: Syntax error ... ProxyPass unknown Balancer parameter
//
// and the setup of ngc-web stops there. The parameter is a WORKER parameter,
// so it belongs on the BalancerMember; with it moved, the same apachectl
// configtest says "Syntax OK". The version note of the plan was right and the
// place was wrong -- upgrade= needs Apache 2.4.47, but never on a ProxyPass
// that points at a balancer.
//
// Checked here, and not left to a configtest, because this machine has no
// Apache: what the case holds is the RULE, so the next renderer cannot put a
// worker parameter back on that line.
func TestWorkerParametersStayOnTheBalancerMember(t *testing.T) {
	got := string(Render([]config.WebAPI{
		{URL: "https://api-1.ngc.test:7443", TLS: bytes.Repeat([]byte{0xE1}, 32)},
		{URL: "https://api-2.ngc.test:7443", TLS: bytes.Repeat([]byte{0xE2}, 32)},
	}))
	members := 0
	for _, line := range strings.Split(got, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "BalancerMember "):
			members++
			if !strings.Contains(trimmed, "upgrade=websocket") {
				t.Errorf("a BalancerMember carries no upgrade=websocket, so the websocket of the browser cannot pass: %q", trimmed)
			}
		case strings.HasPrefix(trimmed, "ProxyPass ") && strings.Contains(trimmed, "balancer://"):
			// Every key=value on this line is read as a BALANCER parameter.
			for _, word := range strings.Fields(trimmed)[1:] {
				if !strings.Contains(word, "=") || strings.HasPrefix(word, "\"") {
					continue
				}
				name := word[:strings.Index(word, "=")]
				switch name {
				case "stickysession", "nofailover", "timeout", "maxattempts",
					"lbmethod", "failonstatus", "failontimeout", "growth",
					"forcerecovery", "scolonpathdelim", "hcheck":
					// A balancer parameter; Apache takes these here.
				default:
					t.Errorf("ProxyPass to a balancer carries %q, which Apache reads as a balancer parameter and refuses (AH00526, measured on Apache 2.4.69); a worker parameter belongs on the BalancerMember: %q", name, trimmed)
				}
			}
		}
	}
	if members != 2 {
		t.Fatalf("two APIs went in, %d BalancerMember lines came out", members)
	}
}

// TestTheBalancerAnswersAMissingMemberHonestly holds the three lines book
// point 91 put into the template as a RULE, which the byte-wise comparison
// above cannot: testdata/apache-apis.conf is whatever we wrote into it, so a
// line dropped from BOTH sides matches itself just fine.
//
// Measured on 24.10.2026 with api-2 gone (point 90): nine of forty logins
// failed, six of them with a naked HTTP 500 and about 1600 byte of Apache
// error page, and /var/log/apache2/error.log held not one line about a
// member - grep -c on proxy|balancer was 0. The three lines answer the two
// halves of that: a dead member is given up on in two seconds instead of
// sixty-five, the 503 Apache answers when nothing is left carries the same
// Retry-After a busy database gives (point 48), and the failure is written
// down where someone looking for the cause will read it.
//
// What this does not measure: no Apache runs on the machine this was written
// on, so that the directives are also ACCEPTED is unmeasured here, as in
// every other case of this package. headers_module and proxy_balancer_module
// were read as loaded on api-1 on 25.10.2026.
func TestTheBalancerAnswersAMissingMemberHonestly(t *testing.T) {
	got := string(Render([]config.WebAPI{
		{URL: "https://api-1.ngc.test:7443", TLS: bytes.Repeat([]byte{0xE1}, 32)},
		{URL: "https://api-2.ngc.test:7443", TLS: bytes.Repeat([]byte{0xE2}, 32)},
	}))
	if got == "" {
		t.Fatal("Render wrote nothing, so this case measures an earlier wall")
	}
	members := 0
	for _, line := range strings.Split(got, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "BalancerMember ") {
			continue
		}
		members++
		// The DIAL has to be bounded on its own. Without connectiontimeout it
		// inherits timeout, and then a member whose machine is gone is waited
		// on for over a minute before the balancer looks at the next one.
		if !strings.Contains(trimmed, "connectiontimeout=2") {
			t.Errorf("a BalancerMember bounds no dial of its own, so a dead member is waited out at timeout=: %q", trimmed)
		}
		// And it has to be SHORTER than the answer's own timeout, or it would
		// change nothing - the same relation connectDeadline has to the
		// driver's ReadTimeout in apps/server-db.
		if !strings.Contains(trimmed, "timeout=65") {
			t.Errorf("a BalancerMember no longer gives the ANSWER its 65 s, so cutting the dial would cut the answer with it: %q", trimmed)
		}
	}
	if members != 2 {
		t.Fatalf("two APIs went in, %d BalancerMember lines came out", members)
	}
	if !strings.Contains(got, `Header always set Retry-After "5" "expr=%{REQUEST_STATUS} == 503"`) {
		t.Error("a 503 of the balancer's own goes out bare, while a busy database answers 503 with Retry-After: 5 (point 48)")
	}
	if !strings.Contains(got, "LogLevel proxy:info") {
		t.Error("a member that cannot be reached is not written to the error log, which is the second half of point 90")
	}
	// The bolt against the line that looks like the fix and is not: this API
	// answers its OWN faults with 500 (E_INTERNAL, httpapi/fault.go), so
	// failonstatus=500 would pull a HEALTHY api out of the balancer over a
	// damaged row of its own and put an error page in place of its honest
	// body. Decided against in point 91, and held here so the next hand does
	// not add it back as an obvious improvement.
	if strings.Contains(got, "failonstatus=500") {
		t.Error("failonstatus=500 takes a healthy API out of the balancer over its own E_INTERNAL and replaces its honest body with an error page (point 91)")
	}
}
