// Package apitls makes and keeps the TLS certificate of this API. The
// certificate carries no trust; the pin of its key does (trust.TLSPin), which
// stands in the trust list, in pairing and join codes and in web.json. So the
// certificate is self-signed and valid for 100 years, and it is renewed only
// with the same key: a new key comes only with a new pin in the trust list.
package apitls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// ErrTLS reports a certificate or key this package cannot use.
var ErrTLS = errors.New("apitls: certificate or key unusable")

// Host returns the host of a tunnel address wss://<host>:<port>/tunnel, the
// only name the certificate carries: Apache checks it (SSLProxyCheckPeerName).
func Host(tunnelURL string) (string, error) {
	if !trust.ValidTunnelURL(tunnelURL) {
		return "", ErrTLS
	}
	rest := strings.TrimSuffix(strings.TrimPrefix(tunnelURL, "wss://"), "/tunnel")
	// A bracketed IPv6 host holds the only colons before the port's.
	i := strings.LastIndexByte(rest, ':')
	if i < 1 {
		return "", ErrTLS
	}
	return rest[:i], nil
}

// Ensure loads the key and certificate, makes what is missing and returns the
// certificate with the pin of its key. An existing key is never replaced.
func Ensure(certFile, keyFile, host string, now time.Time) (tls.Certificate, []byte, error) {
	key, err := ensureKey(keyFile)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	der, err := os.ReadFile(certFile)
	if err == nil {
		if cert, pin, err := use(der, key); err == nil {
			return cert, pin, nil
		}
	} else if !os.IsNotExist(err) {
		return tls.Certificate{}, nil, err
	}
	der, err = issue(key, host, now)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	if err := write(certFile, "CERTIFICATE", der, 0o644); err != nil {
		return tls.Certificate{}, nil, err
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return use(pemBytes, key)
}

// use builds the TLS certificate from PEM and the key, and checks that the two
// belong together; a stale certificate for another key is thrown away.
func use(certPEM []byte, key *ecdsa.PrivateKey) (tls.Certificate, []byte, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return tls.Certificate{}, nil, ErrTLS
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return tls.Certificate{}, nil, ErrTLS
	}
	pub, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || !pub.Equal(key.Public()) {
		return tls.Certificate{}, nil, ErrTLS
	}
	cert := tls.Certificate{Certificate: [][]byte{block.Bytes}, PrivateKey: key, Leaf: leaf}
	return cert, trust.TLSPin(leaf.RawSubjectPublicKeyInfo), nil
}

// ensureKey reads the key or makes one; the file is root's alone.
func ensureKey(keyFile string) (*ecdsa.PrivateKey, error) {
	b, err := os.ReadFile(keyFile)
	if err == nil {
		block, _ := pem.Decode(b)
		if block == nil || block.Type != "EC PRIVATE KEY" {
			return nil, ErrTLS
		}
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, ErrTLS
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := write(keyFile, "EC PRIVATE KEY", der, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// issue signs a certificate for host with key, valid for 100 years.
func issue(key *ecdsa.PrivateKey, host string, now time.Time) ([]byte, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: host},
		DNSNames:              []string{host},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(100, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	return x509.CreateCertificate(rand.Reader, tpl, tpl, key.Public(), key)
}

// write puts a PEM block into path with mode perm, the mode set before the
// first byte so a key file is never readable for others, not even for a moment.
func write(path, kind string, der []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Chmod(perm); err != nil {
		return err
	}
	if err := pem.Encode(f, &pem.Block{Type: kind, Bytes: der}); err != nil {
		return err
	}
	return f.Sync()
}
