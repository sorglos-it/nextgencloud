package runtime

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
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

// 6. --pull=always. The base is pinned by digest
// (TestTheContainerfilePinsItsBaseByDigest), so this no longer decides WHICH
// image is used - the digest does. It is still held because a digest that is
// not in the cache of the build machine has to be fetchable at all, and
// because the default would let a build reach for whatever a local tag of that
// name points at.
//
// So the NAME of this case is now wider than what it proves: it holds the
// flag, not a pin. The pin is the case above, and it is the one that would go
// red if somebody went back to a tag.
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

// 16. The base of the image is pinned to a DIGEST, and this case reads the
// one Containerfile that stands in this repo off the disk to say so.
//
// Why a digest and not the tag it replaced: a tag is a NAME. Whatever lies
// under docker.io/library/debian:13-slim is whatever the registry says the day
// somebody builds, so two builds of the same commit were two different images,
// and whoever gained control of that name gained control of what runs in the
// container of the terminal app - the app whose manifest says needs: ["root"].
// A digest is the bytes themselves.
//
// The digest in the file is MEASURED: registry-1.docker.io answers it for that
// tag, and sha256 over the 8973 bytes of the returned manifest index equals it
// (measured 15.10.2026; tools/pin-base.sh repeats exactly that). It is not a
// number copied out of a document.
//
// THIS CASE CANNOT BE MUTATED BY AN OVERLAY, and that is measured, not
// assumed: -overlay replacing this Containerfile with a pinned one left the
// unpinned line in front of the case that stood here before (1 case ran, PASS
// with the unpinned FROM logged). go test -overlay lies to the COMPILER, and
// this reads the disk. A mutant against this line is therefore a real swap in
// a copy of the tree - which is how it was killed.
func TestTheContainerfilePinsItsBaseByDigest(t *testing.T) {
	b := readContainerfile(t)
	from := ""
	for _, line := range strings.Split(b, "\n") {
		if l := strings.TrimLeft(line, " \t"); strings.HasPrefix(l, "FROM") {
			from = strings.TrimRight(l, " \t\r")
			break
		}
	}
	if from == "" {
		t.Fatalf("the Containerfile has no FROM line; it reads\n%s", b)
	}
	if !basePinned.MatchString(from) {
		t.Fatalf("the base is NOT pinned to a digest: %q. A tag is a name, and a "+
			"name does not say which bytes run in the container. Pin it with "+
			"tools/pin-base.sh, never by hand", from)
	}
	// EVERY FROM line, not the first one: a second stage added later without a
	// digest would be the base that got in, and the loop above stops at the
	// first.
	//
	// The wall itself (CheckBase) is not called with this path on purpose, and
	// the reason is a measurement of it: CheckBase runs the path through
	// validBuildArg, which refuses "..", and the path of this file from inside
	// its own package is "../../templates/...". That refusal is right for a
	// build argument - a build stands in the root of the repo - so the case
	// holds the same grammar here instead of bending the wall for a test.
	for _, line := range strings.Split(b, "\n") {
		l := strings.TrimRight(strings.TrimLeft(line, " \t"), " \t\r")
		if strings.HasPrefix(l, "FROM") && !basePinned.MatchString(l) {
			t.Fatalf("a FROM line of the Containerfile is not pinned to a digest: %q", l)
		}
	}
}

// 17. CheckBase is the wall itself, and these are its refusals. The input is a
// PATH, so every case writes its own file in a temporary directory - that is
// what makes the wall measurable on a machine with no podman, and it is also
// why these are not the case above: that one is about the file this repo
// ships, these are about what the wall accepts from anybody.
func TestCheckBaseRefusesABaseThatIsNotPinned(t *testing.T) {
	const hex64 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, c := range []struct {
		name string
		body string
		want error
	}{
		{"a bare tag is refused", "FROM docker.io/library/debian:13-slim\nRUN true\n", ErrBase},
		{"a bare name is refused", "FROM debian\n", ErrBase},
		{"no FROM at all is refused", "RUN true\nCMD [\"/bin/sh\"]\n", ErrBase},
		{"another digest algorithm is refused", "FROM debian@sha512:" + hex64 + "\n", ErrBase},
		{"a short digest is refused", "FROM debian@sha256:" + hex64[:63] + "\n", ErrBase},
		{"a long digest is refused", "FROM debian@sha256:" + hex64 + "a\n", ErrBase},
		{"upper case hex is refused", "FROM debian@sha256:" + strings.ToUpper(hex64) + "\n", ErrBase},
		{"a word behind the digest is refused", "FROM debian@sha256:" + hex64 + " AS base\n", ErrBase},
		// The one that matters for a file somebody extends later: the first
		// base is nailed down and the second is not, and a check that stopped
		// at the first FROM would call this file fine.
		{"a second unpinned stage is refused", "FROM debian@sha256:" + hex64 +
			"\nFROM alpine:3\n", ErrBase},
		{"a pinned base passes", "FROM debian@sha256:" + hex64 + "\nRUN true\n", nil},
		{"an indented pinned base passes", "\tFROM debian@sha256:" + hex64 + "\n", nil},
		{"CRLF line ends pass", "FROM debian@sha256:" + hex64 + "\r\nRUN true\r\n", nil},
		{"two pinned stages pass", "FROM debian@sha256:" + hex64 +
			"\nFROM debian@sha256:" + strings.Repeat("b", 64) + "\n", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := filepath.ToSlash(filepath.Join(t.TempDir(), "Containerfile"))
			if err := os.WriteFile(f, []byte(c.body), 0o600); err != nil {
				t.Fatalf("the case could not write its own Containerfile: %v", err)
			}
			// The preparation is held before the result is read: a case that
			// measured an empty or unwritten file would report a refusal that
			// came from nothing being there.
			if got, err := os.ReadFile(f); err != nil || string(got) != c.body {
				t.Fatalf("the case did not get its own file onto the disk: %v", err)
			}
			err := CheckBase(f)
			if c.want == nil && err != nil {
				t.Fatalf("CheckBase = %v, want nil", err)
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("CheckBase = %v, want %v", err, c.want)
			}
		})
	}
}

// 18. A file that cannot be read is a refusal and not a pass. It is its own
// case because it is the shape that makes a wall hollow: "nothing to read"
// read as "nothing wrong" would let a build run with no base checked at all.
func TestCheckBaseRefusesAFileItCannotRead(t *testing.T) {
	missing := filepath.ToSlash(filepath.Join(t.TempDir(), "Containerfile"))
	if err := CheckBase(missing); !errors.Is(err, ErrBase) {
		t.Fatalf("CheckBase on a missing file = %v, want ErrBase", err)
	}
	// And a path the builder would never be given either: the same grammar
	// every other path of a build goes through, so this wall cannot be used to
	// read something outside the tree.
	if err := CheckBase("../../../etc/passwd"); !errors.Is(err, ErrPath) {
		t.Fatalf("CheckBase on a path with .. = %v, want ErrPath", err)
	}
}

// 19. Build refuses before it ever starts podman when the base is not pinned.
// Without this the wall would only be the two cases above, and nothing would
// say that the BUILD uses it - a mutant that takes the call out of Build again
// would leave every case green.
//
// bin is a name no machine has, so a Build that got past the checks would fail
// with an exec error instead of ErrBase - which is exactly how the two are
// told apart here.
func TestBuildRefusesAnUnpinnedBaseBeforeItStartsTheBuilder(t *testing.T) {
	dir := t.TempDir()
	s := goodSpec()
	s.Context = filepath.ToSlash(dir)
	s.Out = filepath.ToSlash(filepath.Join(dir, "out.tar"))
	s.File = filepath.ToSlash(filepath.Join(dir, "Containerfile"))
	if err := os.WriteFile(s.File, []byte("FROM debian:13-slim\n"), 0o600); err != nil {
		t.Fatalf("the case could not write its own Containerfile: %v", err)
	}
	err := Build(context.Background(), "ngc-no-such-builder", s, io.Discard, io.Discard)
	if !errors.Is(err, ErrBase) {
		t.Fatalf("Build = %v, want ErrBase", err)
	}
	// The intermediate check that keeps this from measuring an earlier wall:
	// with the base pinned the same call gets PAST CheckBase and dies at the
	// next one (no program in the context), so the refusal above came from the
	// base and from nothing in front of it.
	pinned := "FROM debian@sha256:" + strings.Repeat("a", 64) + "\n"
	if err := os.WriteFile(s.File, []byte(pinned), 0o600); err != nil {
		t.Fatalf("the case could not rewrite its own Containerfile: %v", err)
	}
	if err := Build(context.Background(), "ngc-no-such-builder", s, io.Discard, io.Discard); !errors.Is(err, ErrProgram) {
		t.Fatalf("with the base pinned Build = %v, want ErrProgram (the wall behind this one)", err)
	}
}

// containerfilePath is the Containerfile of the terminal app, relative to this
// package - which is where a test runs. One constant because two cases read
// the same file and a second spelling of the path is a second thing to keep
// right.
const containerfilePath = "../../templates/terminal/Containerfile"

// readContainerfile reads the Containerfile of the terminal app off the disk.
func readContainerfile(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(containerfilePath)
	if err != nil {
		t.Fatalf("the Containerfile of the terminal app is not readable at %s: %v", containerfilePath, err)
	}
	return string(b)
}
