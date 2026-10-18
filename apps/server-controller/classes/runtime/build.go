package runtime

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"path"
	"regexp"
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
	// ErrBase reports a Containerfile whose base is not nailed down: a FROM
	// that names a tag and no digest, or no FROM at all. A tag is a NAME, and
	// a name says nothing about the bytes that end up in the image.
	ErrBase = errors.New("runtime: the base of the Containerfile is not pinned to a digest")
)

// basePinned is a FROM line whose base carries a digest. The three parts that
// matter, and why each is held:
//
//   - "@sha256:" and 64 characters of LOWER CASE hex. Lower case because that
//     is the only form a registry answers and the only one podman compares
//     byte for byte; a digest typed in upper case is a different string to
//     everything that reads it.
//   - Nothing after the digest but space. A second word would be another
//     argument of FROM ("AS builder" is one), and this build has no stage to
//     name, so anything there was not meant.
//   - The repository in front of the "@" is not inspected beyond "no space
//     and no @". Which repository the base comes from is a decision of the
//     Containerfile; THAT the bytes are nailed down is the decision here.
var basePinned = regexp.MustCompile(`^FROM[ \t]+[^ \t@]+@sha256:[0-9a-f]{64}[ \t]*$`)

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
		// The base is pinned by DIGEST (see the Containerfile), so this no
		// longer decides WHICH image is used - the digest does, and a cached
		// layer under that digest is the same bytes as a fetched one. It stays
		// because a digest that is NOT in the cache has to be fetchable at
		// all: with --pull=never a build machine that never saw this base
		// would fail late, and with the default it could reach for whatever a
		// local tag of that name points at.
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

// CheckBase holds the Containerfile at path against the one thing a build
// cannot repair afterwards: that its base is nailed to a digest.
//
// Why this is a function of the PACKAGE and not only of a case: a case can
// read the one Containerfile that stands in this repo today, and it does
// (build_test.go). What a case cannot do is stop a build. Build calls this
// before podman starts, so a Containerfile whose base went back to a bare tag
// ends in a refusal and not in an image - including one that somebody puts
// beside the repo and points -file at.
//
// Measured without podman, and that is the whole reason it reads a file and
// decides nothing else: the input is a path, so a case hands it a file in a
// temporary directory, and the refusals are the measurement.
//
// EVERY FROM line has to be pinned, not the first one: a Containerfile with
// two stages has two bases, and the unpinned one would be the one that got in.
// No FROM line at all is refused too - a file that names no base is not the
// Containerfile this build is for, and treating "nothing to check" as "all
// good" is how a check turns hollow.
func CheckBase(file string) error {
	if err := validBuildArg(file); err != nil {
		return err
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return errors.Join(ErrBase, err)
	}
	found := false
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		// Leading space is allowed by the format, so it is trimmed before the
		// line is read - otherwise an indented FROM would be invisible here
		// and visible to podman.
		line = strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(line, "FROM") {
			continue
		}
		found = true
		if !basePinned.MatchString(strings.TrimRight(line, " \t")) {
			return ErrBase
		}
	}
	if !found {
		return ErrBase
	}
	return nil
}

// Build makes the image and writes it to s.Out. bin is the builder (podman);
// out and errOut are where its own output goes, because an admin who has to
// fix a build reads what podman wrote, not what this says about it.
//
// WRITTEN AND COMPILED, NOT RUN: no podman and no docker exists on the machine
// this was written on, so neither line below has ever started. What IS
// measured is everything above it (build_test.go).
//
// The base of the Containerfile is PINNED TO A DIGEST, and CheckBase above
// refuses a build whose Containerfile went back to a bare tag. What that buys:
// two builds of the same commit start from the same bytes, and whoever gains
// control of the tag gains nothing. What it does NOT buy, and it is the cost
// that comes with it: a pinned base receives no security fix by itself. Who
// raises the digest and how it is measured stands in tools/pin-base.sh.
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
	// in an image for the wrong machine is worse than a refusal now. The base
	// comes before both for the same reason and a sharper one: an image built
	// off an unpinned base is not wrong in a way anybody SEES - it installs,
	// it runs, and only its bytes are somebody else's choice.
	if err := CheckBase(s.File); err != nil {
		return err
	}
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
