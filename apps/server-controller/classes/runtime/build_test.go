package runtime

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Every case in this file runs on any machine, podman or not, and that is the
// whole point of it: there is no podman and no docker here, so a case that
// started a build would skip itself - and a set that switches itself off looks
// green (measured 12.10.2026 on the hardening of the app container, where
// three mutants lived behind 23 skipped cases).
//
// So nothing here builds anything. buildArgs, saveArgs and checkProgram are
// the build as far as it is decidable before podman starts, and that is what
// is read.
//
// What stays unmeasured here, said out loud: whether podman takes any of these
// switches, whether an image comes out, and whether that image runs. No case
// in this repo measures it and none could on this machine.

// goodSpec is the spec every case starts from: it passes check(), so what a
// case then reads is the build and not a refusal.
func goodSpec() BuildSpec {
	return BuildSpec{
		Arch:    "amd64",
		Ref:     "ngc/terminal:1.0.0",
		File:    "apps/server-controller/templates/terminal/Containerfile",
		Context: "build",
		Out:     "services/amd64.tar",
	}
}

func buildOf(t *testing.T, s BuildSpec) []string {
	t.Helper()
	args, err := buildArgs(s)
	if err != nil {
		t.Fatalf("buildArgs: %v", err)
	}
	return args
}

// pairIn reports whether opt is in args and is followed by value. A pair and
// not two Contains calls: "--arch" somewhere and "arm64" somewhere else is not
// the same switch.
func pairIn(args []string, opt, value string) bool {
	for i, a := range args {
		if a == opt && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
}

// 1. The whole list of the build at once. This one dies at every mutant of the
// build line and is meant to; the cases after it say which switch is which, so
// a red run here is read together with them.
func TestBuildIsTheWholeArgumentList(t *testing.T) {
	want := []string{
		"build",
		"--arch", "amd64",
		"--file", "apps/server-controller/templates/terminal/Containerfile",
		"--tag", "ngc/terminal:1.0.0",
		"--pull=always",
		"build",
	}
	if got := buildOf(t, goodSpec()); !slices.Equal(got, want) {
		t.Fatalf("buildArgs =\n%q\nwant\n%q", got, want)
	}
}

// 2. The whole list of the save at once, same reason.
func TestSaveIsTheWholeArgumentList(t *testing.T) {
	want := []string{"save", "--format", "docker-archive",
		"--output", "services/amd64.tar", "ngc/terminal:1.0.0"}
	got, err := saveArgs(goodSpec())
	if err != nil {
		t.Fatalf("saveArgs: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("saveArgs =\n%q\nwant\n%q", got, want)
	}
}

// 3. --arch, and it is the architecture of the SERVER, not of whoever builds.
// Without it the build machine decides, and the image dies on the server with
// a message about a format - after the app is in the catalog.
func TestBuildNamesTheArchitectureOfTheServer(t *testing.T) {
	for _, arch := range Arches {
		s := goodSpec()
		s.Arch = arch
		args := buildOf(t, s)
		if !pairIn(args, "--arch", arch) {
			t.Fatalf("args = %q without --arch %s", args, arch)
		}
		// And nothing else names a platform, so two switches cannot disagree.
		if slices.Contains(args, "--platform") {
			t.Fatalf("args = %q name the platform twice", args)
		}
	}
}

// 4. --file, and it is the Containerfile of the app and not whatever lies in
// the context directory. Without it podman reads "Containerfile" out of the
// context, which is the build directory - a directory this repo fills with
// compiled programs and nothing else, so the build would either fail or take
// a file somebody else put there.
func TestBuildNamesTheContainerfile(t *testing.T) {
	s := goodSpec()
	args := buildOf(t, s)
	if !pairIn(args, "--file", s.File) {
		t.Fatalf("args = %q without --file %s", args, s.File)
	}
}

// 5. --tag, and the name it gives the image is the SAME one the save asks for.
// Two names here would mean a build that works and a save that writes an
// empty-handed error, or worse, saves an older image of that name.
func TestBuildAndSaveNameOneImage(t *testing.T) {
	s := goodSpec()
	s.Ref = "ngc/terminal:2.3.4"
	build := buildOf(t, s)
	save, err := saveArgs(s)
	if err != nil {
		t.Fatalf("saveArgs: %v", err)
	}
	if !pairIn(build, "--tag", s.Ref) {
		t.Fatalf("build args = %q without --tag %s", build, s.Ref)
	}
	if len(save) == 0 || save[len(save)-1] != s.Ref {
		t.Fatalf("save args = %q do not end at %s", save, s.Ref)
	}
}

// 6. --pull=always. The base image of the Containerfile is named by a tag, so
// what it is depends on the registry; without this it depends on the cache of
// the machine that builds, which may be months old. This is not a pin - see
// TestTheContainerfileDoesNotPinItsBase.
func TestBuildFetchesTheBaseInsteadOfTrustingTheCache(t *testing.T) {
	if args := buildOf(t, goodSpec()); !slices.Contains(args, "--pull=always") {
		t.Fatalf("args = %q without --pull=always", args)
	}
}

// 7. --format docker-archive on the save, and the case says why: ImageTag
// reads the tar member "manifest.json" with its single RepoTags, and an OCI
// archive has no such member. An image in the other format loads on no server
// of this project, and nothing would notice until a server tried.
func TestSaveWritesTheArchiveImageTagCanRead(t *testing.T) {
	args, err := saveArgs(goodSpec())
	if err != nil {
		t.Fatalf("saveArgs: %v", err)
	}
	if !pairIn(args, "--format", SaveFormat) {
		t.Fatalf("args = %q without --format %s", args, SaveFormat)
	}
	if SaveFormat != "docker-archive" {
		t.Fatalf("SaveFormat = %q; ImageTag reads %q out of the tar, which only a docker archive has",
			SaveFormat, ManifestFile)
	}
}

// 8. --output, and the file is the one the admin named and nothing else.
func TestSaveWritesWhereItWasTold(t *testing.T) {
	s := goodSpec()
	s.Out = "services/arm64.tar"
	s.Arch = "arm64"
	args, err := saveArgs(s)
	if err != nil {
		t.Fatalf("saveArgs: %v", err)
	}
	if !pairIn(args, "--output", "services/arm64.tar") {
		t.Fatalf("args = %q without --output %s", args, s.Out)
	}
}

// 9. Only an architecture NGC is built for gets through, in both lists. The
// value becomes an argument of podman, and an unknown one would either build
// an image nobody can run or be read as something else entirely.
func TestBuildRefusesAnUnknownArchitecture(t *testing.T) {
	for _, arch := range []string{"", "x86", "amd64 ", "-amd64", "386", "ARM64"} {
		s := goodSpec()
		s.Arch = arch
		if _, err := buildArgs(s); !errors.Is(err, ErrArch) {
			t.Fatalf("buildArgs with arch %q = %v, want ErrArch", arch, err)
		}
		if _, err := saveArgs(s); !errors.Is(err, ErrArch) {
			t.Fatalf("saveArgs with arch %q = %v, want ErrArch", arch, err)
		}
	}
}

// 10. The name of the image goes through the same grammar as every other
// reference of this package: a space would make two arguments out of one, a
// leading dash an option of podman, a second colon would move the tag into the
// repository.
func TestBuildRefusesANameThatIsNoReference(t *testing.T) {
	for _, ref := range []string{"", "ngc/terminal", "ngc/terminal:1.0.0 --privileged",
		"-ngc/terminal:1.0.0", "ngc/terminal:1.0.0:extra", "NGC/terminal:1.0.0"} {
		s := goodSpec()
		s.Ref = ref
		if _, err := buildArgs(s); !errors.Is(err, ErrImage) {
			t.Fatalf("buildArgs with ref %q = %v, want ErrImage", ref, err)
		}
		if _, err := saveArgs(s); !errors.Is(err, ErrImage) {
			t.Fatalf("saveArgs with ref %q = %v, want ErrImage", ref, err)
		}
	}
}

// 11. Every path of the build is held, and all three of them: a path that
// begins with a dash becomes an option of podman, one with a ".." in it leaves
// the directory the admin stands in, and a line break or a NUL is in no path
// anybody meant to write. All three fields, because a check on one of them is
// no check at all.
func TestBuildRefusesEveryPathThatIsNoPath(t *testing.T) {
	bad := []string{"", "-f", "--file", "../out.tar", "a/../../etc/x",
		"with\nbreak", "with\x00nul", strings.Repeat("a", maxBuildPath+1)}
	for _, field := range []string{"File", "Context", "Out"} {
		for _, p := range bad {
			s := goodSpec()
			switch field {
			case "File":
				s.File = p
			case "Context":
				s.Context = p
			case "Out":
				s.Out = p
			}
			if _, err := buildArgs(s); !errors.Is(err, ErrPath) {
				t.Fatalf("buildArgs with %s = %q gave %v, want ErrPath", field, p, err)
			}
			if _, err := saveArgs(s); !errors.Is(err, ErrPath) {
				t.Fatalf("saveArgs with %s = %q gave %v, want ErrPath", field, p, err)
			}
		}
	}
}

// elfAt writes a file with an ELF header for machine, so a case can hand
// checkProgram a program without compiling one.
func elfAt(t *testing.T, dir string, machine uint16, class, data byte) {
	t.Helper()
	head := make([]byte, elfHeader)
	copy(head, "\x7fELF")
	head[4], head[5] = class, data
	binary.LittleEndian.PutUint16(head[elfMachineAt:], machine)
	if err := os.WriteFile(filepath.Join(dir, ContextProgram), head, 0o644); err != nil {
		t.Fatal(err)
	}
}

// 12. The program in the context has to be built for the architecture the
// image is for. This is the one mistake of the three build lines that is
// invisible: everything builds, saves, loads and installs, and the server
// fails at the first start with a message about a format.
func TestCheckProgramHoldsTheProgramAgainstTheArchitecture(t *testing.T) {
	for _, c := range []struct {
		what    string
		machine uint16
		arch    string
		want    error
	}{
		{"amd64 program for an amd64 image", elfAMD64, "amd64", nil},
		{"arm64 program for an arm64 image", elfARM64, "arm64", nil},
		{"amd64 program for an arm64 image", elfAMD64, "arm64", ErrArch},
		{"arm64 program for an amd64 image", elfARM64, "amd64", ErrArch},
		{"a program for a third machine", 0x28 /* ARM 32 bit */, "arm64", ErrArch},
	} {
		dir := t.TempDir()
		elfAt(t, dir, c.machine, 2, 1)
		s := goodSpec()
		s.Arch, s.Context = c.arch, filepath.ToSlash(dir)
		err := checkProgram(s)
		if c.want == nil && err != nil {
			t.Fatalf("%s: checkProgram = %v, want nil", c.what, err)
		}
		if c.want != nil && !errors.Is(err, c.want) {
			t.Fatalf("%s: checkProgram = %v, want %v", c.what, err, c.want)
		}
	}
}

// 13. And what is no program at all: nothing in the context, something too
// short to read, and a file whose header says 32 bit or big endian - the two
// bytes the machine number is read out of mean nothing without those.
func TestCheckProgramRefusesWhatIsNoProgram(t *testing.T) {
	s := goodSpec()
	s.Context = filepath.ToSlash(t.TempDir())
	if err := checkProgram(s); !errors.Is(err, ErrProgram) {
		t.Fatalf("checkProgram on an empty context = %v, want ErrProgram", err)
	}

	short := t.TempDir()
	if err := os.WriteFile(filepath.Join(short, ContextProgram), []byte("\x7fELF"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Context = filepath.ToSlash(short)
	if err := checkProgram(s); !errors.Is(err, ErrProgram) {
		t.Fatalf("checkProgram on four bytes = %v, want ErrProgram", err)
	}

	for _, c := range []struct {
		what        string
		class, data byte
	}{
		{"32 bit", 1, 1},
		{"big endian", 2, 2},
	} {
		dir := t.TempDir()
		elfAt(t, dir, elfAMD64, c.class, c.data)
		s.Context = filepath.ToSlash(dir)
		if err := checkProgram(s); !errors.Is(err, ErrProgram) {
			t.Fatalf("checkProgram on a %s header = %v, want ErrProgram", c.what, err)
		}
	}

	// A file that is not ELF at all. A shell script in build/ is exactly what a
	// half finished build machine leaves behind.
	plain := t.TempDir()
	if err := os.WriteFile(filepath.Join(plain, ContextProgram),
		[]byte("#!/bin/sh\necho hello\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.Context = filepath.ToSlash(plain)
	if err := checkProgram(s); !errors.Is(err, ErrProgram) {
		t.Fatalf("checkProgram on a script = %v, want ErrProgram", err)
	}
}

// 14. checkProgram refuses a broken spec before it opens anything: a bad
// architecture must not turn into "the program is missing", which is a
// different thing to go and fix.
func TestCheckProgramHoldsTheSpecFirst(t *testing.T) {
	s := goodSpec()
	s.Arch = "x86"
	s.Context = filepath.ToSlash(t.TempDir())
	if err := checkProgram(s); !errors.Is(err, ErrArch) {
		t.Fatalf("checkProgram = %v, want ErrArch", err)
	}
}

// 15. The name the Containerfile copies and the name checkProgram looks for
// are one decision, and this is the place it is read. A Containerfile that
// copies another name builds an image without the program in it, and nothing
// else in this repo would say so.
//
// The file is read off the disk on purpose: ContextProgram is a constant of
// the compiled package, the COPY line is not, and only one of the two can be
// mutated by an overlay. A missing file is a failure and not a skip - a case
// that switches itself off here is the shape that left the hardening unmeasured.
func TestTheContainerfileCopiesTheProgramUnderTheNameTheBuildExpects(t *testing.T) {
	b := readContainerfile(t)
	want := "COPY " + ContextProgram + " "
	if !strings.Contains(b, want) {
		t.Fatalf("the Containerfile has no line %q; it reads\n%s", want, b)
	}
}

// 16. The base of the image is not pinned, and that is a finding and not a
// wish: the Containerfile names docker.io/library/debian:13-slim by TAG, so
// two builds of the same commit are two different images, and whoever controls
// that tag controls what runs inside the container of every app.
//
// The case is written the way the finding stands today: it holds that the base
// is named by a tag. Whoever pins it to a digest makes this case red, reads
// this comment, and deletes it - which is the point. It is not a case that
// demands the hole stay open; it is one that keeps the hole from being
// forgotten.
func TestTheContainerfileDoesNotPinItsBase(t *testing.T) {
	b := readContainerfile(t)
	i := strings.Index(b, "FROM ")
	if i < 0 {
		t.Fatalf("the Containerfile has no FROM line; it reads\n%s", b)
	}
	from := strings.TrimSpace(b[i:])
	if j := strings.IndexByte(from, '\n'); j >= 0 {
		from = strings.TrimSpace(from[:j])
	}
	if strings.Contains(from, "@sha256:") {
		t.Fatalf("the base is pinned now (%q) - the finding in the comment above is closed, "+
			"take this case out and say so in the report", from)
	}
	t.Logf("UNPINNED BASE, this is the finding: %q names a tag, not a digest. "+
		"Two builds of this commit are two images.", from)
}

// readContainerfile reads the Containerfile of the terminal app off the disk.
// Its path is relative to this package, which is where a test runs.
func readContainerfile(t *testing.T) string {
	t.Helper()
	const p = "../../templates/terminal/Containerfile"
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("the Containerfile of the terminal app is not readable at %s: %v", p, err)
	}
	return string(b)
}
