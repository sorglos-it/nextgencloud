package apitls

import (
	"bytes"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// FuzzHost holds Host to the grammar of the contract instead of only to not
// crashing: it takes exactly the addresses trust.ValidTunnelURL takes, and
// what it hands back is a host of that grammar which, with the port, gives
// the address back unchanged. So the name in the certificate is always the
// whole name Apache reads out of the address (SSLProxyCheckPeerName), never a
// piece of one: a host cut at the wrong colon would still look like a name.
func FuzzHost(f *testing.F) {
	f.Add("wss://api-1.ngc.test:7443/tunnel")
	f.Add("wss://[2001:db8::1]:7443/tunnel")
	f.Add("wss://192.0.2.7:7443/tunnel")
	f.Add("wss://api-1.ngc.test/tunnel")
	f.Add("https://api-1.ngc.test:7443")
	f.Add("wss://:7443/tunnel")
	f.Add("wss://api-1.ngc.test:7443/tunnel/tunnel")
	f.Add("")
	f.Fuzz(func(t *testing.T, u string) {
		got, err := Host(u)
		// The defensive guard in Host must never turn an address the
		// contract takes into an error, and never the other way round.
		if ok := trust.ValidTunnelURL(u); ok != (err == nil) {
			t.Fatalf("Host(%q) = %q, %v, but ValidTunnelURL says %v", u, got, err, ok)
		}
		if err != nil {
			if !errors.Is(err, ErrTLS) {
				t.Fatalf("Host(%q) answered %v instead of ErrTLS", u, err)
			}
			return
		}
		if !trust.ValidHost(got) {
			t.Fatalf("Host(%q) = %q, no host of the grammar", u, got)
		}
		port, ok := strings.CutPrefix(u, "wss://"+got+":")
		if !ok {
			t.Fatalf("Host(%q) = %q, which is not what stands behind wss://", u, got)
		}
		port, ok = strings.CutSuffix(port, "/tunnel")
		if !ok {
			t.Fatalf("Host(%q) = %q leaves %q, not a port", u, got, port)
		}
		// A colon left in the port would mean Host cut before the end of a
		// bracketed IPv6 host and handed out a piece of it.
		if strings.Contains(port, ":") {
			t.Fatalf("Host(%q) = %q leaves the port %q", u, got, port)
		}
	})
}

// FuzzEnsureKeepsTheKey feeds the certificate file with anything a broken
// disk, a half-written update or a wrong hand could leave there. Whatever
// stands in it, Ensure answers with a certificate for the key that is already
// on disk: the pin never changes behind the admin's back, because a new pin
// would need a new version of the trust list first, and the key file itself
// is never rewritten.
func FuzzEnsureKeepsTheKey(f *testing.F) {
	dir := f.TempDir()
	certFile, keyFile := filepath.Join(dir, "api.crt"), filepath.Join(dir, "api.key")
	_, want, err := Ensure(certFile, keyFile, "api-1.ngc.test", when)
	if err != nil {
		f.Fatalf("ensure: %v", err)
	}
	good, err := os.ReadFile(certFile)
	if err != nil {
		f.Fatalf("read certificate: %v", err)
	}
	key, err := os.ReadFile(keyFile)
	if err != nil {
		f.Fatalf("read key: %v", err)
	}
	f.Add(good)
	f.Add([]byte{})
	f.Add([]byte("-----BEGIN CERTIFICATE-----\n-----END CERTIFICATE-----\n"))
	f.Add(bytes.Repeat([]byte{0}, 64))
	f.Fuzz(func(t *testing.T, in []byte) {
		if err := os.WriteFile(certFile, in, 0o644); err != nil {
			t.Fatalf("write certificate: %v", err)
		}
		cert, pin, err := Ensure(certFile, keyFile, "api-1.ngc.test", when)
		if err != nil {
			t.Fatalf("ensure: %v", err)
		}
		if !bytes.Equal(pin, want) {
			t.Fatal("the pin changed with the certificate file")
		}
		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if !bytes.Equal(pin, trust.TLSPin(leaf.RawSubjectPublicKeyInfo)) {
			t.Fatal("pin does not belong to the certificate")
		}
		if again, err := os.ReadFile(keyFile); err != nil || !bytes.Equal(again, key) {
			t.Fatalf("the key file was rewritten: %v", err)
		}
	})
}
