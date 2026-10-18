package release

import (
	"archive/tar"
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

// 1. The probe Parts uses has to PASS on a pair that is known good,
// otherwise Parts would answer "no parts at all" and that would look like a
// measurement. This is the leak test of the measuring device: without it, a
// typo in probe (a zero seq, a short checksum) makes every candidate fail
// and Parts returns an empty list in silence.
func TestTheProbeItselfWouldPass(t *testing.T) {
	good := Part{Name: store.ReleaseWeb, Arch: "all"}
	if err := probe(good).Validate(); err != nil {
		t.Fatalf("the probe is broken, not the contract: a release of %s/%s alone must be valid, got %v",
			good.Name, good.Arch, err)
	}
}

// 2. Which parts a release has, asked of the contract and counted. Five is
// not a wish: it is what store.Release.Validate accepts out of nine offered
// pairs. The list is written out here so that a change in the contract has
// to be read and confirmed, not merely absorbed.
func TestTheContractNamesFiveParts(t *testing.T) {
	want := []string{
		"ngc-api-amd64.tar",
		"ngc-api-arm64.tar",
		"ngc-controller-amd64.tar",
		"ngc-controller-arm64.tar",
		"ngc-web-all.tar",
	}
	got := Parts()
	if len(got) != len(want) {
		t.Fatalf("the contract accepts %d of %d offered pairs, the release path expects %d: %v",
			len(got), len(candidates.names)*len(candidates.archs), len(want), got)
	}
	for i, p := range got {
		if p.File() != want[i] {
			t.Fatalf("part %d is %q, the release path expects %q", i, p.File(), want[i])
		}
	}
	t.Logf("the contract accepts %d of %d offered pairs", len(got), len(candidates.names)*len(candidates.archs))
}

// 3. There is NO ngc-db part, and that is not an oversight: the comment of
// the contract says the API release "includes ngc-store and the database
// migrations" (shared/store/catalog.go, ReleaseAPI). The case holds it,
// because a release path that quietly grows a fourth component would ship
// the migrations twice.
func TestThereIsNoDatabasePart(t *testing.T) {
	for _, p := range Parts() {
		if strings.Contains(p.Name, "db") {
			t.Fatalf("a release names a database part (%s): the migrations ride in %s, "+
				"so this is either a contract change to write down or a mistake", p.File(), store.ReleaseAPI)
		}
	}
}

// 4. The web-archive collision has its own file. It lived here first and
// was WRONG: it built the archive with Go archive/tar, and store.WalkUI
// refuses that dialect for reasons that have nothing to do with the
// collision - so the case was green while measuring the wrong thing, and its
// own counter-probe caught it. webtar_test.go builds the blocks by hand,
// proves the base case passes first, and then names one cause at a time.

// 5. What the controller installs for ngc-api is the release FILE ITSELF:
// installPart copies its bytes into BinDir, chmods 0755 and renames it onto
// the name of the program (install.go, copyInto and partFile - nothing on
// that path unpacks anything). The release names that file
// "ngc-api-amd64.tar".
//
// So if the file is a real tar, the service is handed a tar to execute.
// That half is measured on Linux and written down in docs/release-weg.md
// (exit 126, "Exec format error"). What runs HERE, on every platform, is
// the half that needs no exec: a tar does not begin with an ELF header, and
// a Linux program does.
//
// The case does not claim the installer is wrong. It claims the two
// readings differ, and it is the reminder until one of them is written
// down.
func TestATarIsNotAProgram(t *testing.T) {
	const elf = "\x7fELF"
	prog := []byte(elf + "\x02\x01\x01the rest of a linux program")
	ar := tarOf(t, map[string]string{"ngc-api": string(prog)})
	if bytes.HasPrefix(ar, []byte(elf)) {
		t.Fatalf("a tar begins with an ELF header - then this case says nothing")
	}
	if !bytes.HasPrefix(prog, []byte(elf)) {
		t.Fatalf("the probe program is no ELF file - the case would be green for the wrong reason")
	}
	if !bytes.Contains(ar, []byte(elf)) {
		t.Fatalf("the program is not inside the archive at all: tarOf is broken")
	}
}

// 6. The written list in apps/cli/classes/release.go against the contract.
// It is read off the disk as TEXT, so this is a linter and not a test of
// behaviour - it cannot run that file loop (it reads text, it does not execute it). It earns its place anyway: the
// list is the one thing in the release path that is written twice, and a
// drift there is invisible until a release is short a file.
func TestTheToolAndTheContractNameTheSameParts(t *testing.T) {
	const p = "../../apps/cli/classes/release.go"
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("apps/cli/classes/release.go is not readable at %s: %v", p, err)
	}
	src := string(b)
	i := strings.Index(src, "var releaseParts")
	if i < 0 {
		t.Fatalf("apps/cli/classes/release.go holds no \"var releaseParts\" any more - " +
			"if the tool asks the contract now, this case has done its job and comes out")
	}
	j := strings.Index(src[i:], "\n}")
	if j < 0 {
		t.Fatalf("the releaseParts block does not end: the shape of that file changed")
	}
	block := src[i : i+j]
	for _, part := range Parts() {
		// The architecture is matched WITH its quotes: the bare word would
		// also match a comment beside the block.
		if !strings.Contains(block, `"`+part.Arch+`"`) {
			t.Fatalf("releaseParts names no architecture %q, but the contract accepts %s",
				part.Arch, part.File())
		}
	}
	if n := strings.Count(block, "{store.Release"); n != len(Parts()) {
		t.Fatalf("releaseParts holds %d entries, the contract accepts %d parts", n, len(Parts()))
	}
}

// tarOf builds a tar of the given files. The names are walked sorted and the
// time is left at 0, so two calls give the same bytes - the same reason the
// packages of this project are byte-identical at all.
//
// It checks its own work: an archive that does not read back is a failure
// here and not in the case that uses it.
func tarOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sortNames(names)
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, n := range names {
		body := files[n]
		if err := w.WriteHeader(&tar.Header{
			Name:     n,
			Mode:     0o644,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("tarOf: header of %s: %v", n, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("tarOf: body of %s: %v", n, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("tarOf: close: %v", err)
	}
	out := buf.Bytes()
	r := tar.NewReader(bytes.NewReader(out))
	for _, n := range names {
		h, err := r.Next()
		if err != nil || h.Name != n {
			t.Fatalf("tarOf built an archive that does not read back at %s: %v", n, err)
		}
	}
	return out
}

// sortNames sorts in place.
func sortNames(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
