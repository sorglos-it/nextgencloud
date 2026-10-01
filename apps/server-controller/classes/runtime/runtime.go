// Package runtime runs the containers the apps of this server live in: it
// loads their images, creates, starts, stops and removes the containers and
// runs an action or a service inside one.
//
// Everything goes through Engine, so that LXC can sit beside Podman later
// without a caller changing: store.Manifest.Runtime names store.RuntimePodman
// today and keeps "lxc" reserved, and no other package of the controller names
// a container program at all.
//
// Nothing here decides whether something may run - access and install do that
// - and nothing here writes a log line: what a service was given is secret
// (NGC_PARAMS, content keys, licenses), so this package hands its output to
// the writers of the caller and keeps nothing.
package runtime

import (
	"context"
	"errors"
	"io"
)

// Engine runs the containers of the apps.
//
// The name of a container is the name of the system user of its app, and after
// Create it is all the calls below carry: podman runs rootless, so it finds a
// container only under the user that made it, and that user is what the name
// answers.
type Engine interface {
	// LoadImage puts the image of tarPath into the image store and answers
	// with the reference it now has there, read out of the tar itself
	// (ImageTag), never out of an output of the engine.
	LoadImage(ctx context.Context, tarPath string) (string, error)
	// RemoveImage takes the image ref out of the store again.
	RemoveImage(ctx context.Context, ref string) error
	// Create makes the container c without starting it.
	Create(ctx context.Context, c Container) error
	// Start starts a container that was created.
	Start(ctx context.Context, name string) error
	// Stop ends the container, giving the service ten seconds first.
	Stop(ctx context.Context, name string) error
	// Remove takes a container that is not running away. A container that is
	// not there is an error: no output of the engine is read, so the two
	// cannot be told apart here.
	Remove(ctx context.Context, name string) error
	// Running reports whether the container is running.
	Running(ctx context.Context, name string) (bool, error)
	// Exec runs a command in a running container and answers with the number
	// the command ended with. An error means the engine could not be run at
	// all; what the command did is the number.
	Exec(ctx context.Context, e Exec) (int, error)
}

// Container is one container of one app.
//
// Name, DataDir and Hold land in the argument list of the engine, so each of
// them is checked before anything starts: a value beginning with a dash would
// become an option, and a colon in a path would cut the volume specification
// in another place and so mount another directory.
type Container struct {
	// Name is the name of the container and the name of the system user of
	// the app.
	Name string
	// Image is the reference ImageTag read out of the image tar.
	Image string
	// UID and GID are the system user of the app. The engine runs under them,
	// so whoever breaks out of the container lands on a user without rights of
	// its own. The caller gives the app's own user here and no other: where
	// these are the ones this process already has, nothing is switched, so a 0
	// put here by mistake would run podman as this controller. Nothing in this
	// package can tell that mistake from a test, so nothing here refuses it.
	UID, GID uint32
	// DataDir is the encrypted data directory of the app
	// (config.AppDataDir + "/" + app). It becomes /data in the container and
	// is the home directory of the app's user, which is where the engine
	// keeps the image store of that user.
	DataDir string
	// Hold is the program that keeps the container alive: ngc-controller
	// itself, mounted read-only as /ngc/ngc-hold and started with the
	// subcommand hold. An app image therefore needs to bring nothing of its
	// own - that would be a change to store.Manifest.
	Hold string
	// Network lets the app reach the network (store.NeedNetwork). Without it
	// the container has none at all.
	Network bool
	// Root gives the app root inside the container (store.NeedRoot): the
	// capabilities come back. Never root on this server - the container runs
	// in the user namespace of the app's own user either way.
	Root bool
	// CPU is thousandths of a core, Memory is MiB, both as the manifest has
	// them. 0 means the default of the contract (store.DefaultCPU,
	// store.DefaultMemory), never "no limit".
	CPU, Memory int
}

// Exec is one call into a running container: an action of an app or one of its
// services.
type Exec struct {
	// Name is the container, as in Container.Name.
	Name string
	// Command is the program and its arguments, as the manifest has them.
	// The engine passes them as a list; no shell sees them, so a space or a
	// quotation mark in an argument stays inside that argument.
	Command []string
	// EnvFile holds the environment of the call, one NAME=value per line.
	// The engine passes the file instead of the values and removes it when
	// the call is over, so NGC_PARAMS stands in no command line, in no
	// /proc/*/cmdline and in no log. The file belongs to the app's user and
	// lies on the tmpfs under config.RunDir; the caller writes it with mode
	// 0600, because it is the only copy on the machine.
	EnvFile string
	// Stdin, Stdout and Stderr are the ends of the call. Without Stdin the
	// command gets no open standard input in the container.
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Root asks for root inside the container for this call. It names that
	// user out loud and changes nothing else: the container already runs as
	// 0:0, and whether the capabilities are there was decided once, at Create.
	Root bool
}

// The reasons an engine refuses before it starts anything. Every one of them
// is a value this controller built itself, so none of their texts carries
// anything from outside and none of them belongs to a user.
var (
	// ErrName reports a container name that is no name.
	ErrName = errors.New("runtime: container name is not a name")
	// ErrImage reports an image reference that is no reference.
	ErrImage = errors.New("runtime: image reference is not a reference")
	// ErrPath reports a path that is not absolute, not clean, or carries a
	// colon.
	ErrPath = errors.New("runtime: path is not a clean absolute path without a colon")
	// ErrHome reports a system user whose home directory is not the data
	// directory of its app. Create refuses it, because every call after Create
	// takes the home out of the user database and would then look for the
	// container in another store of podman than the one it was made in.
	ErrHome = errors.New("runtime: the home of the app's user is not its data directory")
	// ErrLimit reports a negative CPU or memory limit.
	ErrLimit = errors.New("runtime: cpu or memory limit is negative")
	// ErrCommand reports a call without a command.
	ErrCommand = errors.New("runtime: no command to run")
	// ErrNotRoot reports that this process cannot become the app's user.
	ErrNotRoot = errors.New("runtime: switching to the app's user needs root")
	// ErrUser reports that the system user of the app is not in the user
	// database.
	ErrUser = errors.New("runtime: no system user for this container")
	// ErrManifest reports an image tar that does not name exactly one tag.
	ErrManifest = errors.New("runtime: image tar does not name exactly one tag")
)
