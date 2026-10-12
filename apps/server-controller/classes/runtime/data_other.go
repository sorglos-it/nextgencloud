//go:build !linux

package runtime

// keyIdentifierSize is FSCRYPT_KEY_IDENTIFIER_SIZE, named here as well so the
// signatures below are the ones of the Linux half.
const keyIdentifierSize = 16

// Directory encryption is a thing of the Linux kernel, and NGC is written on
// Windows and run on Debian and Ubuntu. These four say so instead of doing
// nothing quietly: a data directory that is not encrypted must never come out
// of a call that says it encrypted one. Every case that needs them skips itself
// outside Linux, and the file is here so the package builds where it is
// written.

func addKey(mount string, key []byte) ([keyIdentifierSize]byte, error) {
	return [keyIdentifierSize]byte{}, ErrNoFscrypt
}

func removeKey(mount string, id [keyIdentifierSize]byte) error { return ErrNoFscrypt }

func setPolicy(dir string, id [keyIdentifierSize]byte) error { return ErrNoFscrypt }

func policyOf(dir string) ([keyIdentifierSize]byte, error) {
	return [keyIdentifierSize]byte{}, ErrNoFscrypt
}
