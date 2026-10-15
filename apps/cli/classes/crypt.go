package classes

import (
	"bufio"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/store"
)

// Errors of the key file and of encrypting. None of them ever carries key
// material: a sentinel has no room for it, and that is the point. Whatever
// goes wrong with a key, the caller learns what kind of trouble it is and
// nothing of what was in the file — not even a length or a prefix, since
// both narrow a brute-force search. Which file it was the caller knows: it
// passed the path, and Run prints that path, never the content.
var (
	// ErrKeyFile reports a file that is no key of KeySize bytes as base64url.
	ErrKeyFile = errors.New("ngc-store: the file is no key")
	// ErrKeyMissing reports a key file that is not there.
	ErrKeyMissing = errors.New("ngc-store: the key file does not exist")
	// ErrKeyExists reports a key file that is already there: a new key would
	// lock out every package already encrypted with the old one, so
	// NewContentKey refuses instead of overwriting.
	ErrKeyExists = errors.New("ngc-store: the key file already exists")
	// ErrSamePath reports an output path that is the input path, compared
	// after filepath.Clean. Two different paths that lead to the same file
	// (a link, another mount) are not caught — this is the cheap guard
	// against the obvious call, not a check of file identity.
	ErrSamePath = errors.New("ngc-store: input and output are the same file")
)

// maxKeyFile bounds what is read while looking for a key: 64 bytes, the 43
// characters of base64url of KeySize bytes plus room for white space. A
// file bigger than that is no key, and it is refused without reading it all
// — a key file is named on the command line, so "read the whole file first"
// would hand anyone a way to pull a gigabyte into memory.
const maxKeyFile = 64

// NewContentKey writes a new content key to path and returns it: KeySize
// random bytes, base64url, one line.
//
// The file is created with O_EXCL and mode 0600 in one step, so where the
// file system keeps the mode it never exists readable for a moment (the
// paragraph below says where it does not), and an existing key is never
// overwritten
// (ErrKeyExists) — losing the content key of a published paid app would mean
// nobody can be licensed for it any more. The key itself is written only to
// this file: it is never returned through an error, never printed and never
// an argument of a process, because arguments stand in the process list.
//
// What is measured here is the opposite of a promise: on Windows the mode
// does NOT arrive. Go maps a file mode to the read-only attribute alone, so
// os.Stat reports 0666 (measured, 12.10.2026, Windows 11), and the test says
// that out loud instead of passing quietly; protecting the file is left to
// the directory it lies in. That 0600 arrives on a Unix file system is NOT
// measured here — no Unix machine was at hand.
func NewContentKey(path string) ([]byte, error) {
	key := make([]byte, store.KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, ErrKeyExists
		}
		return nil, err
	}
	if _, err := io.WriteString(f, enc.B64.EncodeToString(key)+"\n"); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return nil, err
	}
	return key, nil
}

// readContentKey reads a content key of KeySize bytes, written as base64url.
//
// Every failure is ErrKeyFile or ErrKeyMissing and nothing else: the error
// of enc.B64 names the position of the first bad byte, which is already a
// hint about the content of a file that is meant to be secret, and an error
// of the decoder could quote the input outright. The text read from the file
// is overwritten before returning — best effort, since Go may have copied it
// while reading, but it costs nothing and shortens the window.
func readContentKey(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrKeyMissing
		}
		return nil, ErrKeyFile
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxKeyFile+1))
	if err != nil || len(raw) > maxKeyFile {
		return nil, ErrKeyFile
	}
	defer clear(raw)
	key, err := enc.B64.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != store.KeySize {
		return nil, ErrKeyFile
	}
	return key, nil
}

// Encrypt seals the package in in with the content key in keyFile and writes
// it to out, in the form store.NewDecrypter reads (store.Encrypt: magic, a
// random file ID, then chunks of store.ChunkSize under AES-GCM).
//
// out is ordinary mode (0666 before the umask, as os.Create makes it): it is
// the file a store hands out, and without the key it says nothing. Only the
// key file is 0600.
//
// Writing is the pattern of Pack: straight to out, and on any failure out is
// removed again, so a package cut short never looks like one. Unlike a
// package, the result is deliberately *not* reproducible — store.Encrypt
// draws a fresh file ID from rand.Reader for every run, and the chunk nonces
// are derived from it. Two runs over the same input therefore differ, and
// they must: the same file ID with the same key twice would mean the same
// nonce for the same chunk number, which is the one mistake AES-GCM does not
// forgive. What a catalog pins is the SHA-256 of the file it ships, so the
// reproducibility the packer needs (modification time 0, nothing from the
// disk — see Pack) belongs to the plain package, not to this step.
func Encrypt(in, out, keyFile string) error {
	if filepath.Clean(in) == filepath.Clean(out) {
		return ErrSamePath
	}
	key, err := readContentKey(keyFile)
	if err != nil {
		return err
	}
	defer clear(key)
	src, err := os.Open(in)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(out)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(dst, store.ChunkSize)
	if err := store.Encrypt(w, src, key, rand.Reader); err != nil {
		dst.Close()
		os.Remove(out)
		return err
	}
	if err := w.Flush(); err != nil {
		dst.Close()
		os.Remove(out)
		return err
	}
	if err := dst.Close(); err != nil {
		os.Remove(out)
		return err
	}
	return nil
}
