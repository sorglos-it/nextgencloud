#!/usr/bin/env bash
# Builds the server programs of a release, with the version stamped in.
#
# Usage: build-servers.sh <outdir> [arch...]        default arch: amd64
#
# NGC_BUILD_OS overrides the target system (default linux, which is what a
# release is). It exists for ONE caller: tools/erstlauf.sh station 2 has to RUN
# the programs to ask them their version, and a linux binary does not run on the
# machine a developer sits at. The stamping is the same work for every target,
# so asking a host build is a true answer about the flag - and the station says
# in its own message which target it asked.
#
# WHY THIS FILE EXISTS. Until 23.10.2026 the build command of the servers stood
# only in docs/release-weg.md and in apps/server-controller/CLAUDE.md, for a
# person to type. In October the release was built without it, every program
# reported the fallback 0.0.0-dev - and the interface, which holds a machine's
# version against validVersion (MAJOR.MINOR.PATCH), threw away the WHOLE answer
# and said "Die Rechner konnten nicht geladen werden." That is obstacle 2 of the
# six, and a flag that only a document asks for is a flag that gets forgotten.
# tools/erstlauf.sh station 2 runs THIS script and asks the programs it leaves
# behind for their version, so the hole cannot come back unnoticed.
#
# The command is the one docs/release-weg.md writes down, unchanged:
#   CGO_ENABLED=0 GOOS=linux GOARCH=<arch> go build -buildvcs=false -trimpath \
#     -ldflags "-s -w -X main.Version=$(cat apps/<app>/VERSION)" ...
#
# EVERY PROGRAM CARRIES ITS OWN NUMBER, and that is not a mistake here: ngc-api
# and ngc-store are 0.2.0 while ngc-controller is 1.0.0 (measured 23.10.2026 in
# the three VERSION files). A release has one Version field in store.Release;
# WHICH of the three it is, is written down nowhere, and this script does not
# decide it - it only makes sure that what a program reports is its own VERSION
# file and not the fallback. Whoever packs the release picks the number.
set -euo pipefail
cd "$(dirname "$0")/.."

die() {
	echo "build-servers: $*" >&2
	exit 1
}

[ $# -ge 1 ] || die "usage: build-servers.sh <outdir> [arch...]"
out=$1
shift
arches=("$@")
[ ${#arches[@]} -gt 0 ] || arches=(amd64)
goos=${NGC_BUILD_OS:-linux}

mkdir -p -- "$out" || die "cannot write into $out"
# A computed output path is held against nothing - it is the caller's - but the
# SOURCE directory is, before anything is built from it.
[ -f shared/config/config.go ] || die "$PWD is not the NGC repository"

# app -> name of the program. apps/cli builds ngc-store (its main.go says so),
# which is why the name cannot be derived from the directory.
parts=(
	"server-api|ngc-api"
	"cli|ngc-store"
	"server-controller|ngc-controller"
)

built=0
for arch in "${arches[@]}"; do
	case $arch in
	amd64 | arm64) ;;
	*) die "NGC builds for amd64 and arm64, not for $arch" ;;
	esac
	for p in "${parts[@]}"; do
		app=${p%%|*}
		name=${p#*|}
		[ -f "apps/$app/VERSION" ] || die "apps/$app/VERSION is missing, so there is no number to stamp in"
		ver=$(tr -d ' \t\r\n' <"apps/$app/VERSION")
		# The number is checked here and not only by whoever reads it later: an
		# empty or broken VERSION file would otherwise be stamped in and the
		# program would report something the contract refuses - which is the
		# very failure this script is written against.
		case $ver in
		'' | *[!0-9.]* | *..* | .* | *.) die "apps/$app/VERSION holds \"$ver\", which is no version" ;;
		esac
		if [ ${#arches[@]} -gt 1 ]; then
			target="$out/$name-$arch"
		else
			target="$out/$name"
		fi
		# Windows refuses to start a file without this ending, and a caller that
		# wants to RUN what was built would read "wrote nothing" instead.
		[ "$goos" = windows ] && target="$target.exe"
		CGO_ENABLED=0 GOOS="$goos" GOARCH="$arch" \
			go build -buildvcs=false -trimpath \
			-ldflags "-s -w -X main.Version=$ver" \
			-o "$target" "./apps/$app" ||
			die "./apps/$app did not build for $arch"
		[ -s "$target" ] || die "go build wrote no $target"
		built=$((built + 1))
		echo "build-servers: $name $ver ($goos/$arch) -> $target"
	done
done

# The printed line is the proof, never the exit code alone - the rule
# tools/check.sh and tools/erstlauf.sh are held to as well.
echo "build-servers: ok, $built program(s)"
