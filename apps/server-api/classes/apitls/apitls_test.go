package apitls

import (
	"bytes"
	"crypto/x509"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/trust"
)

var when = time.Unix(1790000000, 0).UTC()

func TestHost(t *testing.T) {
	got, err := Host("wss://api-1.ngc.test:7443/tunnel")
	if err != nil || got != "api-1.ngc.test" {
		t.Fatalf("host %q: %v", got, err)
	}
	for _, bad := range []string{"", "https://api-1.ngc.test:7443", "wss://api-1.ngc.test/tunnel"} {
		if _, err := Host(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestEnsureCreatesAndKeepsTheKey(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "api.crt"), filepath.Join(dir, "api.key")
	cert, pin, err := Ensure(certFile, keyFile, "api-1.ngc.test", when)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !bytes.Equal(pin, trust.TLSPin(leaf.RawSubjectPublicKeyInfo)) {
		t.Fatal("pin does not belong to the certificate")
	}
	if leaf.DNSNames[0] != "api-1.ngc.test" || len(leaf.DNSNames) != 1 {
		t.Fatalf("names %v", leaf.DNSNames)
	}
	// 100 years, so no date ever shuts Apache out.
	if years := leaf.NotAfter.Year() - leaf.NotBefore.Year(); years != 100 {
		t.Fatalf("valid for %d years", years)
	}
	info, err := os.Stat(keyFile)
	if err != nil {
		t.Fatalf("key file: %v", err)
	}
	// On Windows, os.Stat derives the permissions from the read-only
	// attribute alone, so they cannot show the 0600 the key is written with;
	// run go test ./apps/server-api/classes/apitls/ on Linux to check the
	// mode, as shared/config does for its files.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %v", info.Mode().Perm())
	}

	// The certificate is renewed with the same key: the pin in the trust list
	// stays right. Deleting the certificate must not make a new key.
	if err := os.Remove(certFile); err != nil {
		t.Fatalf("remove: %v", err)
	}
	_, pin2, err := Ensure(certFile, keyFile, "api-1.ngc.test", when.Add(24*time.Hour))
	if err != nil || !bytes.Equal(pin, pin2) {
		t.Fatalf("pin changed with the certificate: %v", err)
	}
}

func TestEnsureRejectsBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "api.crt"), filepath.Join(dir, "api.key")
	if err := os.WriteFile(keyFile, []byte("not a key"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := Ensure(certFile, keyFile, "api-1.ngc.test", when); err == nil {
		t.Fatal("broken key accepted")
	}
}
