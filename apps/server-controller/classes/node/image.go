package node

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	ctrlrt "github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
	"github.com/sorglos-it/nextgencloud/shared/store"
)

// This file is the command "image": it writes the image of the terminal app
// for one architecture.
//
// WHO CALLS IT, and this is a decision with consequences, so it stands in the
// code and not only in a report: the ADMIN, by hand, on a BUILD MACHINE. Not
// the controller, and above all not the controller when the first terminal
// session opens.
//
// Why not at the first session, in the order the reasons weigh:
//
//  1. The target server has no builder and must not need one. That is what the
//     Containerfile of this app says of itself, and the install path of this
//     controller is built for it: install.Activate LOADS the tar out of the
//     app package (runtime.LoadImage) and never builds one. A build on the
//     server would be a second way for an image to appear in the store the
//     apps run out of, and the separation podman.go is made of rests on there
//     being only one.
//  2. It would be wait time and disk in the one moment somebody is waiting for
//     a shell: a base image over the network, layers to write - on a server
//     that is meant to come up after a restart without an API and without a
//     registry (restore.go). The first terminal of a fresh server would be the
//     slowest, and it would fail where the network is the thing that is broken.
//  3. The image is part of the app PACKAGE, and a package is signed. An image
//     built on the server is one nobody signed.
//
// What that means for this command: it needs nothing of the server. No
// controller.json, no database, no tunnel, no root. It reads two files of the
// repo and runs the builder. What a case holds of that is the entry
// (TestImageNeedsNothingOfTheServer): a call is answered without a
// configuration ever being read. That nothing FURTHER down reads one is held
// by reading this file, not by a case - there is nothing here that could.
//
// Stage reached, said plainly: WRITTEN AND COMPILED, NOT RUN. There is no
// podman and no docker on this machine, so no image has come out of this
// command. Everything up to the moment podman starts is measured
// (runtime/build_test.go, image_test.go); the image itself and the run of it
// are not.

const (
	// buildDir is the context of the build: the directory the cross compiled
	// controller was built into. The Containerfile copies exactly one file out
	// of it, so nothing else of the repo reaches the image.
	buildDir = "build"
	// terminalDir holds the two files of the app: the Containerfile and the
	// manifest. Both paths are relative to the root of the repo, which is where
	// the three documented lines are run from
	// (apps/server-controller/CLAUDE.md, "Terminal-App bauen").
	terminalDir = "apps/server-controller/templates/terminal"
	// imageRepo is the repository half of the name every terminal image gets.
	// The tag half is the version out of the manifest, never a version of its
	// own: an image whose tag differs from the version in the manifest beside
	// it is an image no package can name.
	imageRepo = "ngc/terminal"
	// builder is what is looked for in PATH. Only the name, so a build machine
	// decides where its podman is - config.DefaultPodman is the path of a
	// SERVER and has nothing to say here.
	builder = "podman"
)

// What "image" answers. One fixed German line each, every one of them saying
// what to do next, because whoever reads it is setting up a build machine.
const (
	msgNoBuilder = "podman ist auf diesem Rechner nicht zu finden. Das Abbild entsteht auf einem Baurechner mit podman, nie auf dem Zielserver."
	msgNoApp     = "Die Vorlagen der Terminal-App sind nicht lesbar. Bitte den Befehl im Stamm des Repos aufrufen."
	msgNoProgram = "Das übersetzte Programm fehlt in build/ngc-controller. Bitte den Controller zuerst für diese Architektur dorthin übersetzen."
	msgWrongArch = "Das Programm in build/ngc-controller ist für eine andere Architektur übersetzt. Bitte mit GOARCH für diese Architektur neu übersetzen."
	msgNoImage   = "Das Abbild ließ sich nicht bauen. Die Ausgabe von podman darüber sagt, woran."
)

// buildImage writes the image of the terminal app for one architecture.
func buildImage(version string, args []string, stdout, stderr io.Writer) int {
	opts, out, err := parseImage(args)
	if err != nil {
		fmt.Fprintln(stderr, usageImage)
		return 2
	}
	spec, err := imageSpec(opts, out)
	if err != nil {
		fmt.Fprintln(stderr, msgNoApp)
		return 1
	}
	bin, err := exec.LookPath(builder)
	if err != nil {
		fmt.Fprintln(stderr, msgNoBuilder)
		return 1
	}
	// The builder writes its own output straight through: an admin who has to
	// fix a build reads what podman said, not what this says about it.
	if err := ctrlrt.Build(context.Background(), bin, spec, stdout, stderr); err != nil {
		fmt.Fprintln(stderr, imageMessage(err))
		return 1
	}
	fmt.Fprintf(stdout, "Abbild %s für %s geschrieben: %s (ngc-controller %s)\n",
		spec.Ref, spec.Arch, spec.Out, version)
	return 0
}

// imageMessage turns what the build answered into the one German line that
// says what to do about it. Only the three causes that have a different next
// step are told apart; everything else is the build itself failing, and then
// the output of podman above is the answer.
func imageMessage(err error) string {
	switch {
	case errors.Is(err, ctrlrt.ErrProgram):
		return msgNoProgram
	case errors.Is(err, ctrlrt.ErrArch):
		return msgWrongArch
	default:
		return msgNoImage
	}
}

// imageSpec is what the command builds, and it is a function of its own so
// that a case can read it without a builder on the machine: it is the one
// place the repo layout, the name of the image and the flags of the command
// come together, and starting podman to see whether they do would make it
// unmeasurable here.
//
// The name of the image comes out of the manifest beside the Containerfile and
// not out of this program: the version of the app and the tag of its image are
// one value, and a package carrying a tar whose tag is not the one of its
// manifest is a package nothing can install. The manifest goes through
// store.ParseManifest, the same door the API packs it with, so an image is
// only built for a manifest that can be packed.
func imageSpec(opts Options, out string) (ctrlrt.BuildSpec, error) {
	m, err := readManifest()
	if err != nil {
		return ctrlrt.BuildSpec{}, err
	}
	return ctrlrt.BuildSpec{
		Arch:    opts.Arch,
		Ref:     imageRepo + ":" + m.Version,
		File:    terminalDir + "/Containerfile",
		Context: buildDir,
		Out:     out,
	}, nil
}

// readManifest reads the manifest of the terminal app.
func readManifest() (*store.Manifest, error) {
	b, err := os.ReadFile(terminalDir + "/manifest.json")
	if err != nil {
		return nil, err
	}
	return store.ParseManifest(b)
}
