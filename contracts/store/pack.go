package store

import (
	"archive/tar"
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"strings"
)

// Limits and names of app packages and their encryption.
const (
	MaxEntries = 10000
	MaxUIFile  = 16 << 20
	MaxUITotal = 64 << 20
	ChunkSize  = 64 << 10
	FileIDSize = 16
	KeySize    = 32
	KindUI     = "ui"
	KindImage  = "image"
	encMagic   = "NGCENC1\n"
)

// PackFile is a checked file of an app package.
type PackFile struct {
	Path string // e.g. "ui/index.html" or "services/arm64.tar"
	Size int64
	Kind string // KindUI or KindImage
	Arch string // images only
}

var (
	ErrPackage   = errors.New("store: invalid app package")
	ErrEncrypted = errors.New("store: encrypted data damaged or wrong key")
)

// Walk reads an app package and calls fn for every file once its header has
// been checked. manifest.json must come first. Walk returns the manifest
// after checking that the files it names are present.
func Walk(r io.Reader, fn func(m *Manifest, f PackFile, body io.Reader) error) (*Manifest, error) {
	tr := tar.NewReader(r)
	h, err := tr.Next()
	if err != nil || h.Typeflag != tar.TypeReg || h.Name != ManifestFile || h.Size > MaxManifest {
		return nil, ErrPackage
	}
	b, err := io.ReadAll(tr)
	if err != nil {
		return nil, ErrPackage
	}
	m, err := ParseManifest(b)
	if err != nil {
		return nil, ErrPackage
	}
	seen := map[string]bool{ManifestFile: true}
	var uiTotal int64
	images := 0
	for n := 1; ; n++ {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil || n > MaxEntries {
			return nil, ErrPackage
		}
		name := strings.TrimSuffix(h.Name, "/")
		if seen[name] {
			return nil, ErrPackage
		}
		seen[name] = true
		switch h.Typeflag {
		case tar.TypeDir:
			if name != "ui" && name != "services" && !(strings.HasPrefix(name, "ui/") && ValidPath(name[3:])) {
				return nil, ErrPackage
			}
			continue
		case tar.TypeReg:
		default:
			return nil, ErrPackage
		}
		f := PackFile{Path: name, Size: h.Size}
		switch {
		case strings.HasPrefix(name, "ui/") && ValidPath(name[3:]):
			uiTotal += h.Size
			if h.Size > MaxUIFile || uiTotal > MaxUITotal {
				return nil, ErrPackage
			}
			f.Kind = KindUI
		case name == "services/amd64.tar" || name == "services/arm64.tar":
			if h.Size > MaxPackage {
				return nil, ErrPackage
			}
			f.Kind, f.Arch = KindImage, strings.TrimSuffix(strings.TrimPrefix(name, "services/"), ".tar")
			images++
		default:
			return nil, ErrPackage
		}
		if err := fn(m, f, tr); err != nil {
			return nil, err
		}
	}
	if (m.UI != nil && !seen["ui/"+m.UI.Entry]) || (m.Icon != "" && !seen["ui/"+m.Icon]) ||
		(len(m.Services) > 0) != (images > 0) {
		return nil, ErrPackage
	}
	return m, nil
}

func packageAEAD(key, fileID []byte) (cipher.AEAD, error) {
	k, err := hkdf.Key(sha256.New, key, fileID, "ngc/package/v1", KeySize)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func chunkNonce(i uint32, last bool) []byte {
	n := make([]byte, 12)
	binary.BigEndian.PutUint32(n[7:11], i)
	if last {
		n[11] = 1
	}
	return n
}

// Encrypt writes src encrypted with the content key of an app.
func Encrypt(dst io.Writer, src io.Reader, key []byte, rng io.Reader) error {
	if len(key) != KeySize {
		return ErrEncrypted
	}
	id := make([]byte, FileIDSize)
	if _, err := io.ReadFull(rng, id); err != nil {
		return err
	}
	aead, err := packageAEAD(key, id)
	if err != nil {
		return err
	}
	if _, err := dst.Write(append([]byte(encMagic), id...)); err != nil {
		return err
	}
	cur, err := readChunk(src)
	if err != nil {
		return err
	}
	for i := uint32(0); ; i++ {
		next, err := readChunk(src)
		if err != nil {
			return err
		}
		last := len(next) == 0
		if _, err := dst.Write(aead.Seal(nil, chunkNonce(i, last), cur, nil)); err != nil {
			return err
		}
		if last {
			return nil
		}
		cur = next
	}
}

func readChunk(r io.Reader) ([]byte, error) {
	b := make([]byte, ChunkSize)
	n, err := io.ReadFull(r, b)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return b[:n], nil
}

type decrypter struct {
	src  *bufio.Reader
	aead cipher.AEAD
	i    uint32
	buf  []byte
	done bool
	err  error
}

// NewDecrypter returns a reader for the plaintext of an encrypted package.
// Any damage, a missing or an extra block ends reading with ErrEncrypted.
func NewDecrypter(src io.Reader, key []byte) (io.Reader, error) {
	head := make([]byte, len(encMagic)+FileIDSize)
	if len(key) != KeySize {
		return nil, ErrEncrypted
	}
	if _, err := io.ReadFull(src, head); err != nil || string(head[:len(encMagic)]) != encMagic {
		return nil, ErrEncrypted
	}
	aead, err := packageAEAD(key, head[len(encMagic):])
	if err != nil {
		return nil, ErrEncrypted
	}
	return &decrypter{src: bufio.NewReaderSize(src, ChunkSize+aead.Overhead()+1), aead: aead}, nil
}

func (d *decrypter) Read(p []byte) (int, error) {
	for len(d.buf) == 0 {
		if d.err != nil {
			return 0, d.err
		}
		if d.done {
			return 0, io.EOF
		}
		d.next()
	}
	n := copy(p, d.buf)
	d.buf = d.buf[n:]
	return n, nil
}

func (d *decrypter) next() {
	sealed := make([]byte, ChunkSize+d.aead.Overhead())
	n, err := io.ReadFull(d.src, sealed)
	if err != nil && err != io.ErrUnexpectedEOF {
		d.err = ErrEncrypted
		return
	}
	_, err = d.src.Peek(1)
	if err != nil && err != io.EOF {
		d.err = ErrEncrypted
		return
	}
	last := err == io.EOF
	plain, err := d.aead.Open(nil, chunkNonce(d.i, last), sealed[:n], nil)
	if err != nil || d.i == ^uint32(0) {
		d.err = ErrEncrypted
		return
	}
	d.i++
	d.buf, d.done = plain, last
}

func wrapAEAD(shared, eph, installPub []byte, app string) (cipher.AEAD, error) {
	salt := append(append([]byte{}, eph...), installPub...)
	k, err := hkdf.Key(sha256.New, shared, salt, "ngc/license-key/v1\x00"+app, KeySize)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// WrapKey encrypts the content key of app for the X25519 key of an
// installation. The one-time key comes from rng, so vectors are reproducible.
func WrapKey(contentKey, installPub []byte, app string, rng io.Reader) (*WrappedKey, error) {
	curve := ecdh.X25519()
	peer, err := curve.NewPublicKey(installPub)
	if err != nil || len(contentKey) != KeySize {
		return nil, ErrEncrypted
	}
	seed := make([]byte, 32)
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rng, seed); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(rng, nonce); err != nil {
		return nil, err
	}
	eph, err := curve.NewPrivateKey(seed)
	if err != nil {
		return nil, err
	}
	shared, err := eph.ECDH(peer)
	if err != nil {
		return nil, ErrEncrypted
	}
	aead, err := wrapAEAD(shared, eph.PublicKey().Bytes(), installPub, app)
	if err != nil {
		return nil, err
	}
	return &WrappedKey{Ephemeral: eph.PublicKey().Bytes(), Nonce: nonce, Sealed: aead.Seal(nil, nonce, contentKey, []byte(app))}, nil
}

// UnwrapKey opens a wrapped content key with the installation's private key.
func UnwrapKey(w *WrappedKey, installPriv *ecdh.PrivateKey, app string) ([]byte, error) {
	peer, err := ecdh.X25519().NewPublicKey(w.Ephemeral)
	if err != nil || len(w.Nonce) != 12 {
		return nil, ErrEncrypted
	}
	shared, err := installPriv.ECDH(peer)
	if err != nil {
		return nil, ErrEncrypted
	}
	aead, err := wrapAEAD(shared, w.Ephemeral, installPriv.PublicKey().Bytes(), app)
	if err != nil {
		return nil, ErrEncrypted
	}
	key, err := aead.Open(nil, w.Nonce, w.Sealed, []byte(app))
	if err != nil || len(key) != KeySize {
		return nil, ErrEncrypted
	}
	return key, nil
}
