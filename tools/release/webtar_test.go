package release

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

// WHAT THIS FILE MEASURES, and why it is the heaviest finding of the release
// path.
//
// Two halves of that path exist today and they cannot meet:
//
//   - tools/pack-release.sh (branch release-packer) builds the ngc-web file
//     with "git archive --format=tar --mtime=@<commit date>" over the tree
//     apps/web, with NO name prefix: it greps its own archive for the exact
//     entry "public/index.php".
//   - apps/server-controller/classes/update/install.go, unpack, reads an
//     ngc-web release file with store.WalkUI. Its BLOCKED paragraph says
//     plainly that nothing in shared/ says what such a file is, and that this
//     build therefore takes it to be a UI tar rooted at "ui/".
//
// store.WalkUI knows exactly ONE tar dialect (the rules at the top of
// shared/store/ui.go), and a real git archive breaks it in three independent
// places. All three are MEASURED, on this machine, on 12.10.2026, by reading
// the header block of the archive git actually wrote
// ("git archive --format=tar --mtime=@1760000000 HEAD:docs", bytes 100-147):
//
//	mode   git writes "0000664", WalkUI requires "0000644" (a directory
//	       "0000755")
//	mtime  git writes the pinned date, WalkUI requires "00000000000".
//	       "--mtime=@0" does NOT help: the field then read 015263246526,
//	       also measured.
//	name   no "ui/" prefix at all
//
// So the decision P3 of docs/vertraege-v1-3-vorschlag.md asks for cannot be
// answered with "the packer adds a prefix". Either shared/ grows a safe
// unpacker for a plain tar, or the packer stops using git archive and writes
// a store.UIWriter archive instead - and then its reproducibility argument
// has to be made again, because it rests on git archive today.
//
// The cases below take each cause ONE AT A TIME, starting from bytes
// store.WalkUI accepts. Without the base case the others would all be red
// for whatever reason happened to be first.

// uiBlocks builds a one-file UI tar block by block, so that each field can
// be set on its own. mode and mtime are the fields git archive spells
// differently; name is the whole name including any prefix.
//
// The checksum is computed the way the contract spells it (shared/store/ui.go:
// six octal digits, NUL and a space, over the 512 bytes with these eight
// counted as spaces), so a patched header is a VALID header of whatever it
// now says - the refusals below are about the field, never about a checksum
// gone stale.
func uiBlocks(name, mode, mtime string, body []byte) []byte {
	var out []byte
	hdr := func(flag byte, nm, md, mt string, size int64) []byte {
		b := make([]byte, 512)
		copy(b[0:100], nm)
		copy(b[100:108], md)
		copy(b[108:124], "0000000\x000000000\x00")
		copy(b[124:136], octal(size, 12))
		copy(b[136:148], mt)
		b[156] = flag
		copy(b[257:265], "ustar\x0000")
		// The checksum last, over the block with its own field as spaces.
		for i := 148; i < 156; i++ {
			b[i] = ' '
		}
		sum := 0
		for _, c := range b {
			sum += int(c)
		}
		copy(b[148:156], octal(int64(sum), 7)+" ")
		return b
	}
	// A directory entry, then the file. The entry is NOT required - measured
	// by switching this block off, and the base case stayed green, because
	// WalkUI implies a parent from the path ("ui/a/b" implies "ui" and
	// "ui/a"). It is written because a real archive carries one, so the bytes
	// below are the shape of a real archive and not the thinnest one that
	// passes.
	if i := strings.LastIndexByte(name, '/'); i > 0 {
		out = append(out, hdr('5', name[:i+1], "0000755\x00", "00000000000\x00", 0)...)
	}
	out = append(out, hdr('0', name, mode, mtime, int64(len(body)))...)
	out = append(out, body...)
	if pad := (512 - len(body)%512) % 512; pad > 0 {
		out = append(out, make([]byte, pad)...)
	}
	return append(out, make([]byte, 1024)...) // the end marker
}

// octal formats v as octal digits and a NUL, width bytes in all.
func octal(v int64, width int) string {
	s := strconv.FormatInt(v, 8)
	return strings.Repeat("0", width-1-len(s)) + s + "\x00"
}

const (
	uiMode    = "0000644\x00"
	uiTime    = "00000000000\x00"
	gitMode   = "0000664\x00"     // measured: what git archive writes
	gitTime   = "15071674000\x00" // MEASURED: the field git archive wrote
	uiName    = "ui/public/index.php"
	plainName = "public/index.php" // measured: what pack-release.sh writes
)

// walk hands ar to store.WalkUI and says how many files came through.
func walk(ar []byte) (int, error) {
	n := 0
	err := store.WalkUI(bytes.NewReader(ar), func(f store.PackFile, body io.Reader) error {
		n++
		_, err := io.Copy(io.Discard, body)
		return err
	})
	return n, err
}

// 1. THE BASE CASE, and nothing below means anything without it: bytes this
// file builds, with every field as the contract spells it, go through
// store.WalkUI. If this is red, uiBlocks is broken and the four refusals
// below measure that instead of the thing they name.
func TestTheBuiltArchiveIsOneWalkUIAccepts(t *testing.T) {
	n, err := walk(uiBlocks(uiName, uiMode, uiTime, []byte("<?php\n")))
	if err != nil {
		t.Fatalf("uiBlocks is broken, not the contract: a correct UI tar must pass, got %v", err)
	}
	if n != 1 {
		t.Fatalf("the archive delivered %d files, expected 1", n)
	}
}

// 2. The mode git archive writes. Measured 0000664; the contract wants
// 0000644.
func TestTheModeGitArchiveWritesIsRefused(t *testing.T) {
	refused(t, uiBlocks(uiName, gitMode, uiTime, []byte("<?php\n")),
		"the mode of a git archive entry (0000664)")
}

// 3. The date the packer pins. The contract wants the field all zeros, and
// the packer pins it to the commit date ON PURPOSE - that is how it makes
// two builds of one commit give one checksum. The two requirements are
// opposed, and that is the heart of the decision.
func TestThePinnedDateIsRefused(t *testing.T) {
	refused(t, uiBlocks(uiName, uiMode, gitTime, []byte("<?php\n")),
		"a pinned modification time")
}

// 4. The missing prefix, which is the one cause the BLOCKED paragraph in the
// controller already names.
func TestTheMissingPrefixIsRefused(t *testing.T) {
	refused(t, uiBlocks(plainName, uiMode, uiTime, []byte("<?php\n")),
		"an entry name without the \"ui/\" prefix")
}

// 5. All three together, which is what a real release built today would be.
// It is not a fourth cause; it is the shape that would actually arrive on a
// server, and it is here so that nobody has to assemble it mentally.
func TestTheArchiveThePackerWritesTodayIsRefused(t *testing.T) {
	refused(t, uiBlocks(plainName, gitMode, gitTime, []byte("<?php\n")),
		"the shape tools/pack-release.sh writes today")
}

// refused checks that store.WalkUI says no, and says no as a package error.
func refused(t *testing.T, ar []byte, what string) {
	t.Helper()
	n, err := walk(ar)
	if err == nil {
		t.Fatalf("store.WalkUI accepted %s (%d files). If that is now the agreed form, "+
			"docs/release-weg.md has to say so and this case has to be rewritten", what, n)
	}
	if !errors.Is(err, store.ErrPackage) {
		t.Fatalf("%s was refused, but not as a package error: %v", what, err)
	}
	t.Logf("refused as expected: %s", what)
}
