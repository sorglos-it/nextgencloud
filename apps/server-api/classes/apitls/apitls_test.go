package apitls

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "api-1.ngc.test" {
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

	// A file over the bound is none of ours; it is not pulled into memory
	// whole, and for the key it is a reason to stop, never to make a new one.
	if err := os.WriteFile(keyFile, bytes.Repeat([]byte{'x'}, maxFile+1), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := Ensure(certFile, keyFile, "api-1.ngc.test", when); !errors.Is(err, ErrTLS) {
		t.Fatalf("oversized key gave %v", err)
	}

	// The certificate is the other way round: it carries no trust of its own,
	// so whatever stands in its file, the API comes up with the same pin.
	dir = t.TempDir()
	certFile, keyFile = filepath.Join(dir, "api.crt"), filepath.Join(dir, "api.key")
	_, pin, err := Ensure(certFile, keyFile, "api-1.ngc.test", when)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	for _, junk := range [][]byte{[]byte("not a certificate"), bytes.Repeat([]byte{'x'}, maxFile+1)} {
		if err := os.WriteFile(certFile, junk, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, again, err := Ensure(certFile, keyFile, "api-1.ngc.test", when); err != nil || !bytes.Equal(pin, again) {
			t.Fatalf("certificate of %d bytes: %v", len(junk), err)
		}
	}
}

// TestEnsureRejectsAForeignCertificate covers the check that the certificate
// belongs to the key, the whole reason use exists. If a certificate for
// another key came through, Ensure would hand out the pin of a key this
// server does not hold, and that pin goes into session.SetupOffer, into the
// pairing and join codes and into the trust list.
func TestEnsureRejectsAForeignCertificate(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "api.crt"), filepath.Join(dir, "api.key")
	_, pin, err := Ensure(certFile, keyFile, "api-1.ngc.test", when)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	key, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}

	// A second API: its certificate is valid, carries the same name and the
	// same time, and differs only in the key it belongs to.
	other := t.TempDir()
	foreign, foreignPin, err := Ensure(filepath.Join(other, "api.crt"), filepath.Join(other, "api.key"), "api-1.ngc.test", when)
	if err != nil {
		t.Fatalf("second key: %v", err)
	}
	if bytes.Equal(pin, foreignPin) {
		t.Fatal("both runs made the same key")
	}
	block := &pem.Block{Type: "CERTIFICATE", Bytes: foreign.Certificate[0]}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(block), 0o644); err != nil {
		t.Fatalf("write certificate: %v", err)
	}

	cert, again, err := Ensure(certFile, keyFile, "api-1.ngc.test", when)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if bytes.Equal(again, foreignPin) {
		t.Fatal("Ensure handed out the pin of the foreign key")
	}
	if !bytes.Equal(again, pin) {
		t.Fatal("the pin of this API changed")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !bytes.Equal(again, trust.TLSPin(leaf.RawSubjectPublicKeyInfo)) {
		t.Fatal("pin does not belong to the returned certificate")
	}
	if now, err := os.ReadFile(keyFile); err != nil || !bytes.Equal(now, key) {
		t.Fatalf("the key was replaced: %v", err)
	}
}

// TestEnsureRenewsForANewNameAndAfterTheEnd: Apache turns away a certificate
// with the wrong name (SSLProxyCheckPeerName) and an expired one
// (SSLProxyCheckPeerExpire on), so neither may be kept. Both are renewed with
// the same key, so the pin stands.
func TestEnsureRenewsForANewNameAndAfterTheEnd(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "api.crt"), filepath.Join(dir, "api.key")
	_, pin, err := Ensure(certFile, keyFile, "api-1.ngc.test", when)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	cert, again, err := Ensure(certFile, keyFile, "api-2.ngc.test", when)
	if err != nil {
		t.Fatalf("new name: %v", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "api-2.ngc.test" {
		t.Fatalf("the certificate kept the old name %v", leaf.DNSNames)
	}
	if !bytes.Equal(pin, again) {
		t.Fatal("the new name brought a new pin")
	}

	// Long past the end of the certificate on disk.
	late := when.AddDate(101, 0, 0)
	cert, again, err = Ensure(certFile, keyFile, "api-2.ngc.test", late)
	if err != nil {
		t.Fatalf("after the end: %v", err)
	}
	leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !late.Before(leaf.NotAfter) {
		t.Fatalf("the expired certificate was kept, it ends %v", leaf.NotAfter)
	}
	if !bytes.Equal(pin, again) {
		t.Fatal("the renewal brought a new pin")
	}
}

// TestEnsureNamesAnAddressAsAnAddress: shared/ulh.md allows an IPv4 address
// as the host of url, and trust.ValidHost allows IPv6 as well. A checker
// matches an address only against the IP entries of a certificate, never
// against a DNS name, so a name entry would let no one through.
func TestEnsureNamesAnAddressAsAnAddress(t *testing.T) {
	for _, u := range []string{"wss://192.0.2.7:7443/tunnel", "wss://[2001:db8::1]:7443/tunnel"} {
		host, err := Host(u)
		if err != nil {
			t.Fatalf("host of %q: %v", u, err)
		}
		dir := t.TempDir()
		cert, _, err := Ensure(filepath.Join(dir, "api.crt"), filepath.Join(dir, "api.key"), host, when)
		if err != nil {
			t.Fatalf("ensure for %q: %v", host, err)
		}
		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if len(leaf.IPAddresses) != 1 || len(leaf.DNSNames) != 0 {
			t.Fatalf("%q: addresses %v, names %v", host, leaf.IPAddresses, leaf.DNSNames)
		}
		// The point of it all: a checker takes the certificate for the host of
		// the tunnel address, brackets and all.
		if err := leaf.VerifyHostname(host); err != nil {
			t.Fatalf("%q: %v", host, err)
		}
	}
}

// TestFilesWrittenWholeOrNotAtAll: both files are written through a temp file
// next to them, so a crash or a full disk never leaves half of one. None of
// those temp files may stay, least of all one holding the key, and the key
// itself is never written over.
func TestFilesWrittenWholeOrNotAtAll(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "api.crt"), filepath.Join(dir, "api.key")
	if _, _, err := Ensure(certFile, keyFile, "api-1.ngc.test", when); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	// A renewal, so the replacing path runs too.
	if _, _, err := Ensure(certFile, keyFile, "api-2.ngc.test", when); err != nil {
		t.Fatalf("renew: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"api.crt", "api.key"}) {
		t.Fatalf("files left behind: %v", names)
	}

	// The key of a second API starting at the same moment never lands on top
	// of the first one's: the pin in the trust list would go stale at once.
	before, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	if err := writeNew(keyFile, blockKey, []byte{1, 2, 3}, 0o600); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("writeNew answered %v instead of fs.ErrExist", err)
	}
	if after, err := os.ReadFile(keyFile); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("the key changed: %v", err)
	}
}
