//go:build linux

package runtime

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// ioc is the kernel's own _IOC macro (asm-generic/ioctl.h): the direction in
// the top two bits, the size of the argument structure, the type letter and the
// number.
func ioc(dir, size uint32, letter byte, nr uint32) uint32 {
	return dir<<30 | size<<16 | uint32(letter)<<8 | nr
}

const (
	iocNone  = 0
	iocWrite = 1
	iocRead  = 2
)

// The four numbers are fixed by linux/fscrypt.h, and a wrong one would not fail
// to build: it would reach another ioctl of another subsystem, or none. So each
// is built here out of the macro the kernel uses and the size of the structure
// it names, and compared.
//
// This is the one part of fscrypt that can be measured on a machine without it,
// and it is measured because the rest cannot be: a kernel without
// CONFIG_FS_ENCRYPTION answers every one of these the same way.
func TestTheIoctlNumbersAreTheOnesOfTheKernel(t *testing.T) {
	for _, c := range []struct {
		name string
		got  uint32
		want uint32
	}{
		// _IOWR('f', 23, struct fscrypt_add_key_arg): key_spec 40 + raw_size 4
		// + key_id 4 + __reserved[8] 32 = 80.
		{"FS_IOC_ADD_ENCRYPTION_KEY", iocAddKey, ioc(iocRead|iocWrite, 40+4+4+32, 'f', 23)},
		// _IOWR('f', 24, struct fscrypt_remove_key_arg): key_spec 40 +
		// removal_status_flags 4 + __reserved[5] 20 = 64.
		{"FS_IOC_REMOVE_ENCRYPTION_KEY", iocRemoveKey, ioc(iocRead|iocWrite, 40+4+20, 'f', 24)},
		// _IOR('f', 19, struct fscrypt_policy): the version 1 structure, which
		// is 4 bytes of modes and flags plus a descriptor of 8.
		{"FS_IOC_SET_ENCRYPTION_POLICY", iocSetPolicy, ioc(iocRead, 4+8, 'f', 19)},
		// _IOWR('f', 22, __u8[9]).
		{"FS_IOC_GET_ENCRYPTION_POLICY_EX", iocGetPolicyEx, ioc(iocRead|iocWrite, 9, 'f', 22)},
	} {
		if c.got != c.want {
			t.Errorf("%s = %#x, the macro gives %#x", c.name, c.got, c.want)
		}
	}
	// And the sizes the buffers are built to, against the fields of the header.
	for _, c := range []struct {
		name      string
		got, want int
	}{
		{"fscrypt_add_key_arg", addKeyArgSize, 40 + 4 + 4 + 32},
		{"fscrypt_remove_key_arg", removeKeyArgSize, 40 + 4 + 20},
		// version, two modes, flags, log2_data_unit_size, __reserved[3],
		// master_key_identifier[16].
		{"fscrypt_policy_v2", policyV2Size, 1 + 1 + 1 + 1 + 1 + 3 + 16},
		// __u64 policy_size, then the policy itself.
		{"fscrypt_get_policy_ex_arg", getPolicyExSize, 8 + 24},
		{"FSCRYPT_KEY_IDENTIFIER_SIZE", keyIdentifierSize, 16},
	} {
		if c.got != c.want {
			t.Errorf("%s = %d bytes, its fields are %d", c.name, c.got, c.want)
		}
	}
	// The modes and the padding, which decide what the encryption actually is.
	if modeAES256XTS != 1 || modeAES256CTS != 4 || policyV2 != 2 ||
		keySpecIdentifier != 2 || policyFlagsPad32 != 0x3 {
		t.Errorf("contents %d, names %d, version %d, key spec %d, flags %#x",
			modeAES256XTS, modeAES256CTS, policyV2, keySpecIdentifier, policyFlagsPad32)
	}
}

// Every one of the four calls goes through a directory it opens itself, and it
// opens it without following a link at the end: a link in place of the data
// directory of an app would put the policy, or the key, on something else.
// Measured on a link that points at a directory which does exist.
func TestTheFscryptCallsFollowNoLink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	// Either of the two says the link was not followed, and which one it is
	// depends on the order Linux checks the two flags in: O_DIRECTORY on a
	// symlink answers ENOTDIR, O_NOFOLLOW alone answers ELOOP. Measured here:
	// ENOTDIR. What matters is that neither call reaches the directory behind
	// the link, which is what takes both flags away to see.
	isLoop := func(err error) bool {
		return errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR)
	}
	if _, err := addKey(link, bytes.Repeat([]byte{1}, 64)); !isLoop(err) {
		t.Errorf("addKey = %v, want ELOOP", err)
	}
	if err := setPolicy(link, [keyIdentifierSize]byte{}); !isLoop(err) {
		t.Errorf("setPolicy = %v, want ELOOP", err)
	}
	if _, err := policyOf(link); !isLoop(err) {
		t.Errorf("policyOf = %v, want ELOOP", err)
	}
	if err := removeKey(link, [keyIdentifierSize]byte{}); !isLoop(err) {
		t.Errorf("removeKey = %v, want ELOOP", err)
	}
}

// And none of the four takes anything but a directory. A regular file in place
// of the data directory of an app would be opened and the ioctl sent to it;
// what it answers is the filesystem's business, and this must not get that far.
func TestTheFscryptCallsTakeOnlyADirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	notDir := func(err error) bool { return errors.Is(err, syscall.ENOTDIR) }
	if _, err := addKey(file, bytes.Repeat([]byte{1}, 64)); !notDir(err) {
		t.Errorf("addKey = %v, want ENOTDIR", err)
	}
	if err := setPolicy(file, [keyIdentifierSize]byte{}); !notDir(err) {
		t.Errorf("setPolicy = %v, want ENOTDIR", err)
	}
	if _, err := policyOf(file); !notDir(err) {
		t.Errorf("policyOf = %v, want ENOTDIR", err)
	}
	if err := removeKey(file, [keyIdentifierSize]byte{}); !notDir(err) {
		t.Errorf("removeKey = %v, want ENOTDIR", err)
	}
}

// Only three answers of the kernel mean "this filesystem cannot encrypt a
// directory", and the difference matters in both directions: one more and a
// real failure would be reported to a user as a disk that cannot do it; one
// fewer and a disk that cannot would come out as an internal error, with no
// sentence telling anyone what to use instead.
func TestOnlyTheKernelsThreeWaysOfSayingNotHereCount(t *testing.T) {
	for _, e := range []syscall.Errno{syscall.ENOTTY, syscall.EOPNOTSUPP, syscall.ENOPKG} {
		if !cannotEncrypt(e) {
			t.Errorf("%v (%d) is not read as a filesystem that cannot", e, int(e))
		}
	}
	for _, e := range []syscall.Errno{syscall.ENOENT, syscall.EACCES, syscall.EEXIST,
		syscall.ENOTDIR, syscall.ENOTEMPTY, syscall.EINVAL, syscall.ENOKEY, syscall.ELOOP} {
		if cannotEncrypt(e) {
			t.Errorf("%v (%d) is read as a filesystem that cannot", e, int(e))
		}
	}
	if cannotEncrypt(nil) || cannotEncrypt(errors.New("not an errno")) {
		t.Error("something that is no errno is read as a filesystem that cannot")
	}
}

// A key of the wrong length never reaches the kernel: the ioctl would take the
// length from the field beside it, so a short key would be read as 64 bytes out
// of a buffer that does not hold them.
func TestAddKeyRefusesAKeyOfTheWrongLength(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []int{0, 32, 63, 65, 128} {
		if _, err := addKey(dir, bytes.Repeat([]byte{1}, n)); !errors.Is(err, ErrKey) {
			t.Errorf("addKey with %d bytes = %v, want ErrKey", n, err)
		}
	}
}
