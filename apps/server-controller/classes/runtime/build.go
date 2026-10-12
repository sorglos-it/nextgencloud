package runtime

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"
)

// This file builds the image of an app out of a Containerfile. It is the other
// half of the engine: podman.go runs containers on the server, this runs the
// builder on a BUILD MACHINE.
//
// Who calls it, decided here and named out loud: the admin, by hand, with
// "ngc-controller image". NOT the controller when a terminal session first
// opens. Three reasons, in this order:
//
//  1. The target server has no builder and must not need one. That is not a
//     preference of this file - it is what the Containerfile of the terminal
//     app says of itself, and what the install path of this controller is
//     built for: install/activate.go LOADS a tar out of the app package
//     (LoadImage), it never builds one.
//  2. A build at the first session is wait time and disk in the one moment a
//     person is waiting for a shell - a base image to fetch over the network,
//     layers to write - and it would make the network and a registry a
//     condition for a terminal on a server that is meant to run without
//     either.
//  3. A builder on the server is a second program that writes images into the
//     store the apps run out of. The whole separation of podman.go is that
//     nothing but NGC puts something there.
//
// What is measurable here without podman is the argument list and the inputs,
// and that is what this file is cut for (buildArgs, saveArgs, checkProgram).
// Build itself starts podman and is therefore WRITTEN AND COMPILED, NOT RUN:
// there is no podman and no docker on the machine this was written on, so no
// image has ever come out of this code. The stage "ausgefuehrt" is missing.

// Arches are the architectures NGC is built for. The list stands here, where
// the value becomes an argument of podman, and node refers to it: two lists
// that have to agree are the shape of a wall in front of an open door.
var Arches = []string{"amd64", "arm64"}

const (
	// ContextProgram is the name the cross compiled controller has inside the
	// build context. The Containerfile copies exactly this name, so this and
	// that COPY line are one decision in two places.
	ContextProgram = "ngc-controller"
	// SaveFormat is the archive podman has to write. docker-archive and not
	// oci-archive, and that is not a taste: ImageTag reads the member
	// "manifest.json" with its single RepoTags out of the tar, and an OCI
	// archive has no such member at all. An image saved in the other format
	// loads on no server of this project.
	SaveFormat = "docker-archive"
	// maxBuildPath is where a Linux path ends; every path of a build stays
	// below it.
	maxBuildPath = 4096
	// elfHeader is as much of the program as is read to tell its architecture,
	// and elfMachineAt is where that number stands in it.
	elfHeader    = 20
	elfMachineAt = 18
	// The two numbers ELF gives the two architectures NGC is built for.
	elfAMD64 = 0x3e
	elfARM64 = 0xb7
)

var (
	// ErrArch reports an architecture that is not one of Arches, or a program
	// that is built for another one than the image is for.
	ErrArch = errors.New("runtime: not an architecture NGC is built for")
	// ErrProgram reports the program in the build context: missing, or no
	// 64 bit ELF program at all.
	ErrProgram = errors.New("runtime: the program in the build context is missing or is no ELF program")
)

// BuildSpec is one image to build. Every field becomes an argument of podman,
// so every field is held against something below.
type BuildSpec struct {
	Arch    string // one of Arches
	Ref     string // name of the image, repository:tag (validRef)
	File    string // the Containerfile
	Context string // the directory podman builds in; holds ContextProgram
	Out     string // the tar to write
}

// validBuildArg holds every path that becomes an argument of the builder.
//
// It is not validPath of podman.go, and the difference is the whole reason it
// exists: a path of a build is relative to the directory the admin stands in
// (the root of the repo), while every path of podman.go is one of a running
// server and absolute. What both refuse is the same: nothing empty, nothing
// endless, no NUL and no line break, and no "..". A leading dash is refused on
// top of that, because here no absolute slash keeps a path from being read as
// an option of podman - and a "--" in front of the positional argument is not
// used instead, because no line of it could be run on this machine to see
// whether podman takes it.
func validBuildArg(s string) error {
	if s == "" || len(s) > maxBuildPath || strings.ContainsAny(s, "\x00\n") ||
		strings.HasPrefix(s, "-") ||
		slices.Contains(strings.FieldsFunc(s, isBuildSeparator), "..") {
		return ErrPath
	}
	return nil
}

// isBuildSeparator reports a path separator. Both count: the image is built on
// Linux, and the cases also run on Windows.
func isBuildSeparator(r rune) bool { return r == '/' || r == '\\' }

// check holds the whole spec and is what both argument lists start from, so
// neither of them can be built out of something the other refused.
func (s BuildSpec) check() error {
	if !slices.Contains(Arches, s.Arch) {
		return ErrArch
	}
	// The same grammar every other image reference of this package goes
	// through: a name with a space in it would be two arguments, and one with
	// a leading dash an option of podman.
	if !validRef.MatchString(s.Ref) {
		return ErrImage
	}
	for _, p := range []string{s.File, s.Context, s.Out} {
		if err := validBuildArg(p); err != nil {
			return err
		}
	}
	return nil
}

// CheckBuildSpec holds a spec against everything the builder demands of it,
// without building anything. It is exported for the caller that assembles the
// spec (node.imageSpec): the paths and the name of the image are put together
// there, and a spec this package would refuse is a command that fails on a
// build machine and nowhere else.
func CheckBuildSpec(s BuildSpec) error { return s.check() }

// buildArgs is the argument list of "podman build" and every refusal before
// it. It stands apart from Build for the reason createArgs stands apart from
// Create: what the list carries is the whole build, and a list that only
// existed inside a call of podman could be measured on no machine without
// podman (build_test.go).
func buildArgs(s BuildSpec) ([]string, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	return []string{
		"build",
		// The image is for the server, not for the build machine. Without this
		// the architecture of whoever builds decides, and the image would die
		// on the server with "exec format error".
		"--arch", s.Arch,
		"--file", s.File,
		"--tag", s.Ref,
		// The base image is named by a TAG (see the Containerfile), so what
		// that tag means is whatever the registry says today. Pulling it every
		// time at least makes the result the one of the registry instead of
		// the one of a cache this machine filled months ago. It does NOT pin
		// anything - see the note at Build.
		"--pull=always",
		s.Context,
	}, nil
}

// saveArgs is the argument list of "podman save": it writes the image that
// buildArgs made into the tar an app package carries.
func saveArgs(s BuildSpec) ([]string, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	return []string{"save", "--format", SaveFormat, "--output", s.Out, s.Ref}, nil
}

// checkProgram holds the one file the build context has to carry against the
// architecture the image is for.
//
// Why this is worth reading a file header for: the three lines that make an
// image name the architecture THREE times (go build, podman build, the name of
// the tar), and nothing but this connects them. Whoever forgets GOARCH on the
// first line gets an image that builds, saves, loads and installs, and fails
// on the server with a message about a format - after the app is in the
// catalog. Here it costs one read of 20 bytes.
func checkProgram(s BuildSpec) error {
	if err := s.check(); err != nil {
		return err
	}
	f, err := os.Open(path.Join(path.Clean(s.Context), ContextProgram))
	if err != nil {
		return errors.Join(ErrProgram, err)
	}
	defer f.Close()
	head := make([]byte, elfHeader)
	if _, err := io.ReadFull(f, head); err != nil {
		return errors.Join(ErrProgram, err)
	}
	// Magic, 64 bit class and little endian data. Both architectures NGC is
	// built for are 64 bit and little endian, so anything else is not the
	// program this builds with - and the two bytes read below mean nothing
	// without them.
	if string(head[:4]) != "\x7fELF" || head[4] != 2 || head[5] != 1 {
		return ErrProgram
	}
	switch binary.LittleEndian.Uint16(head[elfMachineAt:]) {
	case elfAMD64:
		if s.Arch == "amd64" {
			return nil
		}
	case elfARM64:
		if s.Arch == "arm64" {
			return nil
		}
	}
	return ErrArch
}

// Build makes the image and writes it to s.Out. bin is the builder (podman);
// out and errOut are where its own output goes, because an admin who has to
// fix a build reads what podman wrote, not what this says about it.
//
// WRITTEN AND COMPILED, NOT RUN: no podman and no docker exists on the machine
// this was written on, so neither line below has ever started. What IS
// measured is everything above it (build_test.go).
//
// And one thing this cannot fix, named because it belongs to the build: the
// Containerfile of the terminal app names its base by a tag
// (docker.io/library/debian:13-slim), not by a digest. Two builds of the same
// commit are therefore not the same image, and whoever controls that tag
// controls what runs in the container. Pinning it is a decision about how the
// base gets its security fixes afterwards, and it is not this file's to make.
func Build(ctx context.Context, bin string, s BuildSpec, out, errOut io.Writer) error {
	build, err := buildArgs(s)
	if err != nil {
		return err
	}
	save, err := saveArgs(s)
	if err != nil {
		return err
	}
	// The program comes before the build: a build that takes minutes and ends
	// in an image for the wrong machine is worse than a refusal now.
	if err := checkProgram(s); err != nil {
		return err
	}
	for _, args := range [][]string{build, save} {
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Stdout, cmd.Stderr = out, errOut
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}
