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
	// LoadImage puts the image of tarPath into the image store of the app that
	// name belongs to and answers with the reference it now has there, read out
	// of the tar itself (ImageTag), never out of an output of the engine.
	//
	// The app stands here because every app has its own image store, and that
	// is a line between the apps: one store that all of them read would let
	// every app see the image of every other, at once and for good. Letting the
	// controller load into its own store and handing that store to the apps as
	// an additional one would be exactly that store. So the image goes where
	// the container will run, and nowhere else.
	LoadImage(ctx context.Context, name, tarPath string) (string, error)
	// RemoveImage takes the image ref out of the store of that same app. The
	// app stands here for the same reason as above.
	RemoveImage(ctx context.Context, name, ref string) error
	// Create makes the container c without starting it.
	Create(ctx context.Context, c Container) error
	// Start starts a container that was created.
	Start(ctx context.Context, name string) error
	// Stop ends the container, giving the service ten seconds first.
	Stop(ctx context.Context, name string) error
	// Remove takes a container that is not running away.
	//
	// A container that is not there is an error, and that is meant: no output
	// of the engine is read here, so "it was never there" and "it could not be
	// removed" arrive as the same thing, and taking both for the harmless one
	// would hide the other. A caller that may run without a container asks
	// Running first or bears the error - it must not be made quiet here.
	Remove(ctx context.Context, name string) error
	// Running reports whether the container is running.
	Running(ctx context.Context, name string) (bool, error)
	// Exec runs a command in a running container and answers with the status the
	// command ended with.
	//
	// A status comes back only when the command really ended with one. An error
	// comes back for everything else, and that is two different things the
	// caller has to tell apart by the error, never by the number: ErrKilled
	// means no status reached the engine - a signal, or the context, which then
	// joins its own cause - and an error without ErrKilled means the engine
	// could not be run at all.
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
	// its own. They must be the ones Name stands for: Create holds them against
	// the user database and refuses with ErrIdentity if they are someone else,
	// because every call after Create takes the user out of that database and
	// would otherwise look for the container as another one. Neither may be 0 -
	// a container of an app is nobody's root, and Create refuses that too.
	UID, GID uint32
	// DataDir is the encrypted data directory of the app
	// (config.AppDataDir + "/" + app). It becomes /data in the container and
	// has to be the home directory of the app's user, which is where the engine
	// keeps the image store of that user: Create refuses anything else with
	// ErrHome, and where the user is not in the database yet it refuses a
	// directory outside config.AppDataDir.
	DataDir string
	// Hold is the program that keeps the container alive: ngc-controller
	// itself, mounted read-only as /ngc/ngc-hold and started with the
	// subcommand hold. An app image therefore needs to bring nothing of its
	// own - that would be a change to store.Manifest. It has to lie under
	// config.BinDir: what is mounted into a container decides what the
	// container can read, so only what NGC installed may be named here.
	Hold string
	// Network lets the app reach the network (store.NeedNetwork). Without it the
	// container has none at all. With it, which way out a rootless container
	// takes is left to the engine: naming one would pin this server to a program
	// that may not be installed, and then no app with network would run at all.
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
	// /proc/*/cmdline and in no log.
	//
	// It has to lie under config.RunDir, which the engine refuses otherwise:
	// removing it happens as this process, and this process is root.
	//
	// One thing about it the engine cannot see and the caller owns: its mode,
	// which is 0600, because the file is the only copy on the machine. The
	// owner is not that thing - the engine gives the file to the app's user
	// itself, because podman runs as that user and has to read it, and the
	// engine looks that user up by the container name anyway (Podman.Exec). An
	// earlier wording here said the engine could not see the owner, and that
	// sentence sent a caller looking for a uid it has no way to get.
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

// The reasons an engine refuses, and the one reason it gives afterwards
// (ErrKilled). None of them is a message for a user: the caller maps them to a
// wire.Code with a German text of its own.
//
// Some of them come back joined with a second error that names the offending
// value: a container name, a home directory or a uid read out of /etc/passwd,
// or what user.Lookup said. Nothing in that comes from outside this server and
// nothing of it is secret - but it is more than the sentences below, so a caller
// that logs an error of this package logs those names too.
var (
	// ErrName reports a container name that is no name.
	ErrName = errors.New("runtime: container name is not a name")
	// ErrImage reports an image reference that is no reference.
	ErrImage = errors.New("runtime: image reference is not a reference")
	// ErrPath reports a path this engine will not pass on: one that is not
	// absolute, not clean, or carries a colon, a NUL or a line break - and one
	// that is well formed but in the wrong place, which is a Hold outside
	// config.BinDir or an EnvFile outside config.RunDir. Both halves are about
	// the same thing: what a path here decides is what gets mounted into a
	// container, or which file this process removes as root.
	ErrPath = errors.New("runtime: path is malformed or not in the directory it has to be in")
	// ErrHome reports a data directory that is not the home of the app's user -
	// or, where that user is not in the database yet, one outside
	// config.AppDataDir. Create refuses both, because every call after Create
	// takes the home out of the user database and would then look for the
	// container in another store of podman than the one it was made in.
	ErrHome = errors.New("runtime: the data directory is not the home of the app's user")
	// ErrIdentity reports a uid or gid that is not the one of the app's user.
	ErrIdentity = errors.New("runtime: uid or gid is not the one of the app's user")
	// ErrKilled reports that no status of the command reached this engine: a
	// signal ended it, or the context did. A cancelled or timed-out context
	// always joins its own cause to this one, so a caller can tell a time limit
	// from a signal of someone else. It is deliberately not worded as "a signal
	// ended it": when the context runs out in the same moment the command ends,
	// Go reports the context instead of the command, and then there is no status
	// to be had either - which is all the caller needs to know.
	ErrKilled = errors.New("runtime: no status of the command reached the engine")
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
