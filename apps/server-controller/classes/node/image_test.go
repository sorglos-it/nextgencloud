package node

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ctrlrt "github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/shared/store"
)

// No case here starts a builder, and that is deliberate twice over: there is
// no podman on this machine, so a case that did would skip itself - and on a
// machine that HAS podman a case that did would really build an image out of a
// test. So what is read is imageSpec and imageMessage, which is the command up
// to the moment podman starts.
//
// Unmeasured here and nowhere else in this repo, said out loud: that podman
// takes these arguments, that an image comes out, and that it runs.

// atRepoRoot puts the case in the directory the three documented build lines
// are run from. It is three levels up from this package; a case that cannot
// get there fails instead of skipping, because a set that switches itself off
// looks green.
func atRepoRoot(t *testing.T) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("the root of the repo is not reachable from this package: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("%s is not the root of the repo (no go.mod): %v", root, err)
	}
	t.Chdir(root)
}

// 1. The spec the command builds, whole, from the real files of the repo. The
// tag is the version out of the manifest beside the Containerfile - not a
// constant of this test and not the version of the controller - so this case
// also says what the manifest says today.
func TestImageSpecIsBuiltFromTheFilesOfTheApp(t *testing.T) {
	atRepoRoot(t)
	b, err := os.ReadFile("apps/server-controller/templates/terminal/manifest.json")
	if err != nil {
		t.Fatalf("the manifest of the terminal app: %v", err)
	}
	m, err := store.ParseManifest(b)
	if err != nil {
		t.Fatalf("store.ParseManifest on the manifest of the terminal app: %v", err)
	}

	spec, err := imageSpec(Options{Arch: "arm64"}, "services/arm64.tar")
	if err != nil {
		t.Fatalf("imageSpec: %v", err)
	}
	want := ctrlrt.BuildSpec{
		Arch:    "arm64",
		Ref:     "ngc/terminal:" + m.Version,
		File:    "apps/server-controller/templates/terminal/Containerfile",
		Context: "build",
		Out:     "services/arm64.tar",
	}
	if spec != want {
		t.Fatalf("imageSpec =\n%+v\nwant\n%+v", spec, want)
	}
	// And the two files it names are really there: a spec that points at
	// nothing would build nothing, and the error would come from podman.
	for _, p := range []string{spec.File,
		"apps/server-controller/templates/terminal/manifest.json"} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("the spec names %s, which is not there: %v", p, err)
		}
	}
}

// 2. The tag of the image is the version of the manifest and nothing else. The
// case changes the manifest in a copy of the two files and reads the tag
// again: a spec that carried a tag of its own would stay the same here, and a
// package whose tar is named otherwise than its manifest installs nowhere.
func TestImageSpecTakesItsTagFromTheManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, terminalDir), 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "templates", "terminal", "manifest.json"))
	if err != nil {
		t.Fatalf("the manifest of the terminal app: %v", err)
	}
	bent := bytes.Replace(src, []byte(`"version": "1.0.0"`), []byte(`"version": "4.5.6"`), 1)
	if bytes.Equal(bent, src) {
		t.Fatalf(`the manifest no longer carries "version": "1.0.0", so this case bent nothing`)
	}
	if err := os.WriteFile(filepath.Join(dir, terminalDir, "manifest.json"), bent, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	spec, err := imageSpec(Options{Arch: "amd64"}, "services/amd64.tar")
	if err != nil {
		t.Fatalf("imageSpec: %v", err)
	}
	if spec.Ref != "ngc/terminal:4.5.6" {
		t.Fatalf("Ref = %q, want ngc/terminal:4.5.6 - the tag does not follow the manifest", spec.Ref)
	}
}

// 3. The spec goes through the grammar of the runtime package, which is where
// it becomes arguments of podman. Read here and not only there: the paths and
// the name are assembled in THIS package, and a spec that runtime refuses is a
// command that fails on a build machine and nowhere else.
func TestImageSpecPassesWhatRuntimeDemands(t *testing.T) {
	atRepoRoot(t)
	for _, arch := range ctrlrt.Arches {
		spec, err := imageSpec(Options{Arch: arch}, "services/"+arch+".tar")
		if err != nil {
			t.Fatalf("imageSpec for %s: %v", arch, err)
		}
		// Build would start podman; what is read is the spec, with the one
		// input of the build taken out of the way on purpose: checkProgram
		// wants a cross compiled program in build/, which no test makes.
		if err := ctrlrt.CheckBuildSpec(spec); err != nil {
			t.Fatalf("runtime refuses the spec for %s: %v (%+v)", arch, err, spec)
		}
	}
}

// 4. A manifest that is not there or is no manifest ends the command with the
// German line about the templates, not with something from podman: whoever
// called this in the wrong directory has to be told that, and no build must
// start on a manifest nothing could pack.
func TestImageSpecRefusesAManifestItCannotPack(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if _, err := imageSpec(Options{Arch: "amd64"}, "out.tar"); err == nil {
		t.Fatal("imageSpec in an empty directory gave no error")
	}
	if err := os.MkdirAll(terminalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"{}", "not json", `{"id":"org.sorglos.terminal"}`} {
		if err := os.WriteFile(filepath.Join(terminalDir, "manifest.json"), []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := imageSpec(Options{Arch: "amd64"}, "out.tar"); err == nil {
			t.Fatalf("imageSpec took %q for a manifest", bad)
		}
	}
}

// 5. The three causes with a different next step are told apart, and the rest
// is the build itself. A single line for all of them would send an admin whose
// program is missing to read the output of a podman that never ran.
func TestImageMessageNamesTheNextStep(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{ctrlrt.ErrProgram, msgNoProgram},
		{errors.Join(ctrlrt.ErrProgram, errors.New("no such file")), msgNoProgram},
		{ctrlrt.ErrArch, msgWrongArch},
		{errors.New("exit status 125"), msgNoImage},
	} {
		if got := imageMessage(c.err); got != c.want {
			t.Fatalf("imageMessage(%v) = %q, want %q", c.err, got, c.want)
		}
	}
	// And the three lines are three lines: one of them repeated would send two
	// different mistakes to the same place.
	seen := map[string]bool{}
	for _, m := range []string{msgNoProgram, msgWrongArch, msgNoImage, msgNoBuilder, msgNoApp} {
		if m == "" || seen[m] {
			t.Fatalf("the messages of image are not five different lines: %q", m)
		}
		seen[m] = true
	}
}

// 6. "image" needs nothing of the server, which is the other half of the
// decision that the admin runs it on a build machine: a wrong call is answered
// before any configuration is looked at, so a controller.json that is not
// there (and on a build machine it is not) changes nothing about it.
func TestImageNeedsNothingOfTheServer(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-controller.json")
	var out, errOut bytes.Buffer
	code := Main("1.2.3", []string{"-config", missing, "image", "-arch", "x86", "-out", "t.tar"},
		noInput(), &out, &errOut)
	if code != 2 {
		t.Fatalf("Main = %d, want 2 (wrong use, answered without reading a configuration)", code)
	}
	if !strings.Contains(errOut.String(), usageImage) {
		t.Fatalf("stderr = %q, want %q", errOut.String(), usageImage)
	}
	if strings.Contains(errOut.String(), msgNoConfig) {
		t.Fatalf("stderr = %q - the command read the configuration of a server", errOut.String())
	}
}

// 7. The help says where the command runs. The line of the plan said only what
// it takes, and the measurement of 12.10.2026 that found this command empty
// found that too: the comment in the code knew it was not built, the help did
// not. Whoever reads the help on a server has to see that this one belongs
// somewhere else.
func TestTheHelpSaysWhereTheImageIsBuilt(t *testing.T) {
	var out bytes.Buffer
	writeUsage(&out)
	got := out.String()
	for _, want := range []string{"image -arch", "podman", "Baurechner", "Zielserver"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the help does not say %q:\n%s", want, got)
		}
	}
}
