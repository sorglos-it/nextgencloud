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
	"io"
	"io/fs"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// ErrTLS reports a certificate or key this package cannot use.
var ErrTLS = errors.New("apitls: certificate or key unusable")

// The PEM types of the two files.
const (
	blockCert = "CERTIFICATE"
	blockKey  = "EC PRIVATE KEY"
)

// maxFile bounds what is read from either file. Each holds one PEM block of a
// few hundred bytes, so anything longer is none of ours; the bound keeps a
// file that ended up there by mistake from turning into a huge allocation.
const maxFile = 64 << 10

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
	certPEM, err := readBounded(certFile)
	switch {
	case err == nil:
		if cert, pin, err := use(certPEM, key, host, now); err == nil {
			return cert, pin, nil
		}
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, ErrTLS):
		// No certificate, or none this package could ever use. Unlike the key
		// it costs nothing to make again: it carries no trust of its own.
	default:
		return tls.Certificate{}, nil, err
	}
	der, err := issue(key, host, now)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	if err := writeAtomic(certFile, blockCert, der, 0o644); err != nil {
		return tls.Certificate{}, nil, err
	}
	// The fresh certificate goes through the same gate as one from the file,
	// so nothing is handed out that the next start would throw away again.
	return use(pem.EncodeToMemory(&pem.Block{Type: blockCert, Bytes: der}), key, host, now)
}

// use builds the TLS certificate from PEM and the key, and checks that it is
// the certificate this API may show: it belongs to the key, it carries host,
// and now lies inside its life. Anything else is thrown away and made afresh
// with the same key, so the pin in the trust list stays as it is.
func use(certPEM []byte, key *ecdsa.PrivateKey, host string, now time.Time) (tls.Certificate, []byte, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != blockCert {
		return tls.Certificate{}, nil, ErrTLS
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return tls.Certificate{}, nil, ErrTLS
	}
	// Without this the pin would be that of a key the server does not hold,
	// and that pin goes into session.SetupOffer and into the trust list.
	pub, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || !pub.Equal(key.Public()) {
		return tls.Certificate{}, nil, ErrTLS
	}
	// A new address of the API needs a certificate with the new name, or
	// Apache turns it away (SSLProxyCheckPeerName). VerifyHostname takes an
	// IP address in brackets too, just as Host hands the host over.
	if leaf.VerifyHostname(host) != nil {
		return tls.Certificate{}, nil, ErrTLS
	}
	// This package issues for 100 years, so only a file from an older or a
	// foreign hand can be out of its time; Apache would turn that away as
	// well (SSLProxyCheckPeerExpire on).
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return tls.Certificate{}, nil, ErrTLS
	}
	cert := tls.Certificate{Certificate: [][]byte{block.Bytes}, PrivateKey: key, Leaf: leaf}
	return cert, trust.TLSPin(leaf.RawSubjectPublicKeyInfo), nil
}

// ensureKey reads the key or makes one; the file is root's alone.
func ensureKey(keyFile string) (*ecdsa.PrivateKey, error) {
	key, err := readKey(keyFile)
	if !errors.Is(err, fs.ErrNotExist) {
		return key, err
	}
	fresh, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalECPrivateKey(fresh)
	if err != nil {
		return nil, err
	}
	// writeNew refuses a key that is already there. So of two APIs starting at
	// the same moment exactly one makes the key, and the other works with the
	// one on disk instead of with a key nobody else will ever see.
	if err := writeNew(keyFile, blockKey, der, 0o600); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return readKey(keyFile)
		}
		return nil, err
	}
	return fresh, nil
}

// readKey reads the key file. A file that is there but holds no key stays and
// gives ErrTLS: making a new one would change the pin of this API behind the
// admin's back, and that needs a new version of the trust list first.
func readKey(keyFile string) (*ecdsa.PrivateKey, error) {
	b, err := readBounded(keyFile)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil || block.Type != blockKey {
		return nil, ErrTLS
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, ErrTLS
	}
	return key, nil
}

// readBounded reads path, at most maxFile bytes; a longer file is ErrTLS.
func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// One byte over the bound tells a file of exactly maxFile bytes from a
	// longer one that was only cut short here.
	b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFile {
		return nil, ErrTLS
	}
	return b, nil
}

// issue signs a certificate for host with key, valid for 100 years.
func issue(key *ecdsa.PrivateKey, host string, now time.Time) ([]byte, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber: serial,
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(100, 0, 0),
		// KeyEncipherment belongs to the RSA key transport alone, which TLS
		// 1.3 does not know: with it the certificate would claim something it
		// cannot do.
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	// An address is named as an address, never as a name: a checker matches it
	// only against the IP entries (x509.VerifyHostname, Apache with
	// SSLProxyCheckPeerName), and shared/ulh.md allows IPv4 in url. The
	// brackets belong to the address's spelling, never into the certificate.
	name := strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	tpl.Subject = pkix.Name{CommonName: name}
	if ip, err := netip.ParseAddr(name); err == nil {
		tpl.IPAddresses = []net.IP{net.IP(ip.AsSlice())}
	} else {
		tpl.DNSNames = []string{name}
	}
	return x509.CreateCertificate(rand.Reader, tpl, tpl, key.Public(), key)
}

// writeAtomic replaces path with a PEM block of kind, mode perm.
func writeAtomic(path, kind string, der []byte, perm os.FileMode) error {
	return writePEM(path, kind, der, perm, os.Rename)
}

// writeNew puts a PEM block at path only while nothing is there, and answers
// fs.ErrExist otherwise: a hard link cannot land on a name that is taken, so
// the key can never be replaced, not even by two APIs starting at once.
func writeNew(path, kind string, der []byte, perm os.FileMode) error {
	return writePEM(path, kind, der, perm, os.Link)
}

// writePEM writes the block into a temp file next to path and lets place
// (os.Rename or os.Link) put it there, so path never holds half a file: a torn
// key would have to be deleted by hand, and that changes the pin and needs a
// new version of the trust list. Same mechanics as writeAtomic in
// shared/config: temp file, mode before the first byte, write, sync, place,
// then sync the directory.
func writePEM(path, kind string, der []byte, perm os.FileMode, place func(from, to string) error) (err error) {
	dir := filepath.Dir(path)
	// CreateTemp opens with O_CREATE|O_EXCL under a fresh name, so it never
	// writes through a symlink someone laid out and never takes a file that
	// is already there.
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	// The mode is set before any byte is written, so the key file is never
	// readable for others, not even for a moment.
	if err = f.Chmod(perm); err != nil {
		return err
	}
	if err = pem.Encode(f, &pem.Block{Type: kind, Bytes: der}); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = place(tmp, path); err != nil {
		return err
	}
	// A rename has consumed the temp file; a link left it, and the key must
	// not lie about under a second name.
	os.Remove(tmp)
	if err = os.Chmod(path, perm); err != nil {
		return err
	}
	// The new name lasts through a crash only once the directory is synced;
	// some systems cannot sync a directory, and the file is complete anyway.
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
