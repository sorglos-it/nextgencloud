//go:build linux

package runtime

import (
	"encoding/binary"
	"errors"
	"syscall"
	"unsafe"
)

// The four calls of fscrypt, the directory encryption of the Linux kernel.
// Everything about them is fixed by the kernel's own header (linux/fscrypt.h),
// so every number here is written out with where it comes from: a wrong one
// would not fail to build, it would either do nothing or encrypt with something
// else.
//
// The structures are built as plain byte buffers instead of Go structs. An
// ioctl reads a layout the kernel fixed down to the byte, and a buffer with the
// offsets written next to each field is the one form in which that layout can
// be read against the header. The sizes below are part of the ioctl numbers, so
// a buffer of the wrong length is caught by the kernel, not by us.
const (
	// _IOWR('f', 23, struct fscrypt_add_key_arg), the structure being 80 bytes
	// before its key material.
	iocAddKey = 0xc0506617
	// _IOWR('f', 24, struct fscrypt_remove_key_arg), 64 bytes.
	iocRemoveKey = 0xc0406618
	// _IOR('f', 19, struct fscrypt_policy), 12 bytes - the number carries the
	// size of the first version of the structure, and the kernel reads the
	// version byte first and then as much as that version has. So this one
	// number sets a policy of version 2 as well.
	iocSetPolicy = 0x800c6613
	// _IOWR('f', 22, __u8[9]), the argument being
	// struct fscrypt_get_policy_ex_arg.
	iocGetPolicyEx = 0xc0096616

	// FSCRYPT_KEY_SPEC_TYPE_IDENTIFIER: the key is named by the identifier the
	// kernel derives from it, which is what a version 2 policy carries. The
	// older way (a descriptor chosen by whoever adds the key) lets anyone claim
	// a name and is not used here.
	keySpecIdentifier = 2
	// FSCRYPT_POLICY_V2.
	policyV2 = 2
	// FSCRYPT_MODE_AES_256_XTS for the contents, FSCRYPT_MODE_AES_256_CTS for
	// the file names.
	modeAES256XTS = 1
	modeAES256CTS = 4
	// FSCRYPT_POLICY_FLAGS_PAD_32: a file name is padded to a multiple of 32
	// bytes, so its encrypted length says less about its real one. The widest
	// padding the kernel offers, and the default of the kernel's own tooling.
	policyFlagsPad32 = 0x3

	// The sizes the ioctl numbers above carry.
	addKeyArgSize    = 80
	removeKeyArgSize = 64
	policyV2Size     = 24
	getPolicyExSize  = 8 + policyV2Size
	// keyIdentifierSize is FSCRYPT_KEY_IDENTIFIER_SIZE.
	keyIdentifierSize = 16
)

// openForIoctl opens a directory for an ioctl and nothing else: read only, no
// following of a link at the end, and closed on exec so no child of this
// process ever sees it.
func openForIoctl(dir string) (int, error) {
	return syscall.Open(dir,
		syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
}

// ioctl is the raw call. The buffer is the kernel's to read and to write, and
// it stays alive across the call because it is used after it.
func ioctl(fd int, req uintptr, buf []byte) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req,
		uintptr(unsafe.Pointer(&buf[0])))
	if e == 0 {
		return nil
	}
	return e
}

// cannotEncrypt maps the kernel's way of saying "not here" onto one error of
// ours. ENOTTY is a filesystem that has no such ioctl at all (tmpfs, a network
// mount), EOPNOTSUPP one that has it but was made without the feature (an ext4
// without encrypt), ENOPKG one whose kernel lacks the cipher.
func cannotEncrypt(err error) bool {
	var e syscall.Errno
	if !errors.As(err, &e) {
		return false
	}
	return e == syscall.ENOTTY || e == syscall.EOPNOTSUPP || e == syscall.ENOPKG
}

// addKey puts the key into the keyring of the filesystem that mount lies on and
// answers with the identifier the kernel derived from it. The identifier is
// what the policy of a directory names, and it is the kernel's to compute: the
// field is handed over empty and read back.
func addKey(mount string, key []byte) ([keyIdentifierSize]byte, error) {
	var id [keyIdentifierSize]byte
	if len(key) != keyBytes {
		return id, ErrKey
	}
	fd, err := openForIoctl(mount)
	if err != nil {
		return id, err
	}
	defer syscall.Close(fd)

	buf := make([]byte, addKeyArgSize+len(key))
	// key_spec.type at 0; key_spec.__reserved at 4 and key_spec.u at 8 stay
	// zero, which is how the kernel is told to fill the identifier in.
	binary.NativeEndian.PutUint32(buf[0:], keySpecIdentifier)
	// raw_size at 40, key_id at 44 (zero: the key is handed over directly and
	// not taken out of a keyring of this process), __reserved at 48.
	binary.NativeEndian.PutUint32(buf[40:], uint32(len(key)))
	copy(buf[addKeyArgSize:], key)
	if err := ioctl(fd, iocAddKey, buf); err != nil {
		// The key is in this buffer, so it goes out of it whatever happens.
		clear(buf)
		if cannotEncrypt(err) {
			return id, errors.Join(ErrNoFscrypt, err)
		}
		return id, err
	}
	copy(id[:], buf[8:8+keyIdentifierSize])
	clear(buf)
	return id, nil
}

// removeKey takes the key with this identifier out of the keyring of the
// filesystem again.
//
// The kernel may keep it and say so when files of it are still open; that is
// not a failure of this call, and the status it reports is not read here for
// exactly that reason - there is nothing this package would do differently.
func removeKey(mount string, id [keyIdentifierSize]byte) error {
	fd, err := openForIoctl(mount)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)

	buf := make([]byte, removeKeyArgSize)
	binary.NativeEndian.PutUint32(buf[0:], keySpecIdentifier)
	copy(buf[8:], id[:])
	if err := ioctl(fd, iocRemoveKey, buf); err != nil {
		if cannotEncrypt(err) || errors.Is(err, syscall.ENOKEY) {
			return errors.Join(ErrNoFscrypt, err)
		}
		return err
	}
	return nil
}

// setPolicy binds the directory to the key with this identifier: AES-256-XTS
// for the contents, AES-256-CTS for the names, names padded to 32 bytes.
//
// The directory has to be empty - the kernel refuses otherwise, and that is
// what makes the order in EnsureData the whole of it. A directory that already
// carries exactly this policy is accepted by the kernel without a change, which
// is what makes EnsureData repeatable; one that carries another policy answers
// EEXIST, and that is ErrPolicy and not something to overwrite.
func setPolicy(dir string, id [keyIdentifierSize]byte) error {
	fd, err := openForIoctl(dir)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)

	buf := make([]byte, policyV2Size)
	buf[0] = policyV2
	buf[1] = modeAES256XTS
	buf[2] = modeAES256CTS
	buf[3] = policyFlagsPad32
	// buf[4] is log2_data_unit_size: 0 leaves it to the filesystem's own block
	// size, which is what every other tool does. buf[5] to buf[7] are reserved.
	copy(buf[8:], id[:])
	if err := ioctl(fd, iocSetPolicy, buf); err != nil {
		if cannotEncrypt(err) {
			return errors.Join(ErrNoFscrypt, err)
		}
		if errors.Is(err, syscall.EEXIST) {
			return errors.Join(ErrPolicy, err)
		}
		return err
	}
	return nil
}

// policyOf is the identifier the policy of this directory names, or an error
// where there is no policy. Used by DropData, which has no key of its own and
// must work for a directory whose policy names something else.
func policyOf(dir string) ([keyIdentifierSize]byte, error) {
	var id [keyIdentifierSize]byte
	fd, err := openForIoctl(dir)
	if err != nil {
		return id, err
	}
	defer syscall.Close(fd)

	buf := make([]byte, getPolicyExSize)
	// policy_size at 0 says how much room the kernel has; it writes back how
	// much it used.
	binary.NativeEndian.PutUint64(buf[0:], policyV2Size)
	if err := ioctl(fd, iocGetPolicyEx, buf); err != nil {
		if cannotEncrypt(err) {
			return id, errors.Join(ErrNoFscrypt, err)
		}
		return id, err
	}
	// A version 1 policy carries a descriptor of 8 bytes and no identifier, so
	// it is not one of ours and its key cannot be named this way.
	if buf[8] != policyV2 {
		return id, errors.Join(ErrPolicy,
			errors.New("the policy of "+dir+" is not version 2"))
	}
	copy(id[:], buf[8+8:8+8+keyIdentifierSize])
	return id, nil
}
