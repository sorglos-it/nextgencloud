#!/bin/sh
# build-release.sh - builds EVERY program a release ships and STAMPS the version
# into each one. It holds no key and signs nothing.
#
# Usage: build-release.sh <version> [-ref <git ref>] [outdir]
#   <version>  the NGC version of this release, MAJOR.MINOR.PATCH.
#   -ref       the git ref the WHOLE release is built from, binaries and web
#              tree alike; default HEAD. For a PUBLISHED release this must be a
#              TAG - a name moves.
#   [outdir]   where to write; default dist/release/<version>/ (dist/ is ignored)
#
# EVERYTHING COMES OUT OF <ref>, NOT OUT OF THE WORK TREE. The first version of
# this script handed <ref> to pack-release.sh for apps/web and then ran go build
# in the work tree - so the web archive came from the tag and the binaries came
# from whatever was lying around, uncommitted edits included. A release whose
# parts come from two different sources is not a release of anything. The ref is
# therefore unpacked into a temporary directory with git archive and the
# binaries are built THERE; vendor/ is in the repository, so that build needs no
# network and no work tree.
#
# It writes the five archives a release lists and nothing else:
#   ngc-web-all.tar          through tools/pack-release.sh
#   ngc-api-amd64.tar        exactly ngc-api and ngc-store, no folders
#   ngc-api-arm64.tar        the same two
#   ngc-controller-amd64.tar exactly ngc-controller
#   ngc-controller-arm64.tar the same one
# The shape is the contract's (docs/vertraege-v1.2.md, Punkt 60): two plain
# files for ngc-api, one for ngc-controller, and NO "./" entry - ngc-store
# contents refuses one, measured on 18.10.2026 when a hand-built web archive
# carried it.
#
# ===========================================================================
# WHY THIS SCRIPT EXISTS. The published release v0.2.0 was built by hand, and
# the hand forgot one flag. Measured on 19.10.2026 on the Debian VM, from the
# binaries of that very release:
#     ngc-api 0.0.0-dev (contract 1)
#     ngc-controller 0.0.0-dev (contract 1)
# "0.0.0-dev" is the default of main.Version (apps/server-api/main.go:44), and
# it is what a build without -ldflags "-X main.Version=..." leaves in place.
#
# That is not cosmetic. The controller sends its version in the hello, the API
# hands it on in ngc.users/nodes, and the web interface checks it with
# validVersion (apps/web/public/assets/js/contracts/text.js:84). A single
# invalid field throws the WHOLE answer away, so the manager reads "Die
# Rechner konnten nicht geladen werden." and nobody can install an app from a
# published release - ever. Rebuilt WITH the flag, both answered 0.2.0 and the
# path went on.
#
# So the one job of this script is to make an unstamped release IMPOSSIBLE,
# not merely unlikely. Two gates, and both run for every single binary:
#   1. go version -m <file>  must show GOOS=linux, GOARCH=<arch> and
#      CGO_ENABLED=0. This reads the build record, so it holds for the foreign
#      architecture without running anything - but it says NOTHING about the
#      stamp, see below.
#   2. <file> -version  must ANSWER with the version, and the answer must pass
#      the rule of the WEB interface, not a laxer one. This is the gate that
#      carries the whole claim, and it is the one that would have caught
#      v0.2.0. "Gebaut ist nicht gelaufen."
#
# WHY THE BUILD RECORD CANNOT PROVE THE STAMP, measured on 23.10.2026 on the
# Debian VM, go1.27.2: a binary built with -ldflags "-X main.Version=0.2.0" has
# a build record of -buildmode=exe, -compiler=gc, -trimpath=true, CGO_ENABLED=0,
# GOARCH=amd64, GOOS=linux - and NO -ldflags line at all, while the same binary
# answers "ngc-api 0.2.0 (contract 1)". Go does not record -ldflags. The first
# version of this script asked the record for it and went red on a correctly
# stamped binary. So there is no cheap paper check for the stamp: the only proof
# is the program's own answer, for every architecture, which is why qemu-user is
# a hard requirement here and not a nicety.
# ===========================================================================
#
# WHY THE RULE HERE IS THE WEB'S AND NOT store.ValidVersion. The two differ,
# measured: store.ValidVersion (shared/store/manifest.go:224) allows an
# OPTIONAL FOURTH place, the web's VERSION (text.js:18) does not. A version
# like 0.2.0.1 would therefore be packed, signed, published - and then thrown
# away at the manager with the same sentence as 0.0.0-dev. The stricter of the
# two is the only safe gate, because the stricter one is the one the user runs.
# This is a finding about the contract, not a decision of this script.
#
# WHY THERE IS NO ESCAPE HATCH. A special build like "0.2.0-confirmfix" fails
# the gate here, and that is on purpose: this is the path that produces files
# for signing and publishing, and the hole it had was exactly a build that
# skipped the stamp. An intermediate build gets a PATCH bump (0.2.1), not a
# suffix - anything else is invisible to the manager, which is the bug this
# script exists for. Whoever wants an unpublishable throwaway runs go build by
# hand; it is never signed, so it never reaches a user.
#
# WHY IT NEEDS LINUX. Gate 2 executes a linux binary. On Windows nothing of
# the sort runs, and a gate that switches itself off when the environment is
# short of something is the worst kind of measurement
# (Z:\GitHub\claude-md\messen.md). So the script refuses to run anywhere else
# instead of skipping the gate, and it refuses when the runner for the foreign
# architecture is missing rather than shipping that architecture unchecked.
set -eu
LC_ALL=C
TZ=UTC
export LC_ALL TZ

self=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=$(CDPATH= cd -- "$self/.." && pwd)

die() {
	echo "build-release: $1" >&2
	exit 1
}

missing() {
	echo "build-release: MISSING TOOL $1 - $2" >&2
	exit 1
}

# THE VERSION GATE, written out field by field so a plain sh can do it. It is
# text.js:18 and nothing laxer: three places, each 0 or 1 to 6 digits without a
# leading zero, and no suffix of any kind.
valid_part() {
	case $1 in
	0) return 0 ;;
	[1-9]) return 0 ;;
	[1-9][0-9] | [1-9][0-9][0-9] | [1-9][0-9][0-9][0-9]) return 0 ;;
	[1-9][0-9][0-9][0-9][0-9] | [1-9][0-9][0-9][0-9][0-9][0-9]) return 0 ;;
	esac
	return 1
}
valid_version() {
	case $1 in
	*[!0-9.]* | *..* | .* | *.) return 1 ;;
	esac
	# Exactly two dots. A fourth place is refused HERE, where the web refuses
	# it, and not left to whoever reads the number later.
	case $1 in
	*.*.*.*) return 1 ;;
	*.*.*) ;;
	*) return 1 ;;
	esac
	v_major=${1%%.*}
	v_rest=${1#*.}
	v_minor=${v_rest%%.*}
	v_patch=${v_rest#*.}
	valid_part "$v_major" && valid_part "$v_minor" && valid_part "$v_patch"
}

ref=HEAD
version=
out=
while [ $# -gt 0 ]; do
	case $1 in
	-ref)
		[ $# -ge 2 ] || die "-ref wants a git ref"
		ref=$2
		shift 2
		;;
	-*) die "unknown option: $1" ;;
	*)
		if [ -z "$version" ]; then
			version=$1
		elif [ -z "$out" ]; then
			out=$1
		else
			die "usage: build-release.sh <version> [-ref <git ref>] [outdir]"
		fi
		shift
		;;
	esac
done
[ -n "$version" ] || die "usage: build-release.sh <version> [-ref <git ref>] [outdir]"
valid_version "$version" || die "the version \"$version\" is not MAJOR.MINOR.PATCH without leading zeros - that is the rule of the web interface (validVersion, apps/web/public/assets/js/contracts/text.js:84), and a version it refuses makes the manager read \"Die Rechner konnten nicht geladen werden.\" An intermediate build gets a patch bump, not a suffix."

[ "$(uname -s)" = Linux ] || die "this needs Linux: the version of every built binary is checked by RUNNING it with -version, and a linux binary does not run here ($(uname -s)). Build the release on the Linux machine; nothing is skipped to make it work elsewhere."

host_arch=
case $(uname -m) in
x86_64 | amd64) host_arch=amd64 ;;
aarch64 | arm64) host_arch=arm64 ;;
esac
[ -n "$host_arch" ] || die "this machine is $(uname -m), and a release is built for amd64 and arm64 only"

command -v go >/dev/null 2>&1 || missing go "no Go toolchain, so nothing can be built"
command -v tar >/dev/null 2>&1 || missing tar "the archives cannot be packed"

# The runner for each architecture: nothing for the host's own, qemu-user for
# the other. Resolved BEFORE anything is built, so a missing runner stops the
# run at the start and not after ten minutes of compiling.
runner_for() {
	case $1 in
	"$host_arch") printf '' ;;
	amd64) command -v qemu-x86_64-static || command -v qemu-x86_64 || return 1 ;;
	arm64) command -v qemu-aarch64-static || command -v qemu-aarch64 || return 1 ;;
	*) return 1 ;;
	esac
}
for arch in amd64 arm64; do
	runner_for "$arch" >/dev/null || missing "qemu-user for $arch" "the binaries for $arch cannot be asked for their version, and an architecture that cannot be checked is not shipped. On Debian: sudo apt-get install qemu-user-static"
done

cd -- "$root"

# The programs of a release, and which package each comes from. The three
# main.Version variables they stamp sit in apps/server-api/main.go:44,
# apps/cli/main.go:14 and apps/server-controller/main.go:12.
#
# The grouping is the archive shape of the contract: ngc-api travels WITH
# ngc-store, because ngc-all.sh needs the tool beside the server.
api_progs="ngc-api ngc-store"
controller_progs="ngc-controller"
pkg_for() {
	case $1 in
	ngc-api) printf './apps/server-api' ;;
	ngc-store) printf './apps/cli' ;;
	ngc-controller) printf './apps/server-controller' ;;
	*) return 1 ;;
	esac
}

if [ -z "$out" ]; then out=dist/release/$version; fi
mkdir -p -- "$out" || die "cannot create $out"
out=$(CDPATH= cd -- "$out" && pwd)
work=$(mktemp -d) || die "mktemp failed"
trap 'rm -rf -- "$work"' EXIT
trap 'exit 1' HUP INT TERM

# THE STAMP. One string, built once, used for every build and for every check,
# so the flag the linker gets and the value the gates expect cannot drift
# apart. There is deliberately no way for a caller to add to it.
ldflags="-X main.Version=$version"

# check_target asks the BUILD RECORD what the binary is FOR. It proves the
# target and the static link, not the stamp - Go records no -ldflags line
# (measured, see the header). It runs anyway, because a release that ships a
# dynamically linked binary or the wrong architecture fails at the user in a way
# nobody can read either.
check_target() {
	cs_file=$1
	cs_arch=$2
	go version -m -- "$cs_file" >"$work/buildinfo" 2>&1 || die "the build record of $cs_file cannot be read:
$(cat "$work/buildinfo")"
	grep -q -- 'GOOS=linux' "$work/buildinfo" || die "$cs_file is not built for linux"
	grep -q -- "GOARCH=$cs_arch" "$work/buildinfo" || die "$cs_file is not built for $cs_arch"
	grep -q -- 'CGO_ENABLED=0' "$work/buildinfo" || die "$cs_file is not built statically (CGO_ENABLED=0 is missing from its build record)"
}

# check_version asks the PROGRAM. This is the gate that carries the claim and
# the one that would have caught v0.2.0: one call of -version says 0.0.0-dev out
# loud, and no amount of reading the build record would have.
#
# The answer is "<name> <version>" and, for the two that carry the tunnel
# contract, " (contract N)" behind it. Both fields are checked: a binary that
# answers with another program's name is in the wrong archive.
check_version() {
	cv_file=$1
	cv_prog=$2
	cv_runner=$3
	cv_rc=0
	if [ -n "$cv_runner" ]; then
		"$cv_runner" "$cv_file" -version >"$work/ver.out" 2>&1 || cv_rc=$?
	else
		"$cv_file" -version >"$work/ver.out" 2>&1 || cv_rc=$?
	fi
	[ "$cv_rc" = 0 ] || die "$cv_file -version failed (exit $cv_rc):
$(cat "$work/ver.out")"
	cv_line=$(sed -n '1p' "$work/ver.out")
	cv_name=$(printf '%s' "$cv_line" | cut -d' ' -f1)
	cv_said=$(printf '%s' "$cv_line" | cut -d' ' -f2)
	[ "$cv_name" = "$cv_prog" ] || die "$cv_file answers as \"$cv_name\", not as $cv_prog: \"$cv_line\""
	valid_version "$cv_said" || die "$cv_prog ANSWERS \"$cv_said\", which validVersion refuses (apps/web/public/assets/js/contracts/text.js:84). The whole line was:
  $cv_line
A release of this binary makes the manager read \"Die Rechner konnten nicht geladen werden.\" and nobody can install an app from it. This is exactly what v0.2.0 shipped."
	[ "$cv_said" = "$version" ] || die "$cv_prog answers \"$cv_said\" but this release is $version"
}

# pack writes one archive from the files that are already in $p_dir, by NAME and
# in the given order. Names and no path is what keeps "./" out: tar adds that
# entry when it is handed a directory, and ngc-store contents refuses it.
pack() {
	p_tar=$1
	p_dir=$2
	shift 2
	(cd -- "$p_dir" && tar --format=ustar --numeric-owner --owner=0 --group=0 \
		--mtime="@$stamp" --mode=755 -cf "$p_tar" -- "$@") || die "$p_tar could not be packed"

	# The shape, read back from the archive that was just written - not from
	# the list that went in. The contract names it: plain files only, no
	# folders, no links, exactly these entries in this order.
	tar -tvf "$p_tar" >"$work/entries.v" || die "$p_tar cannot be read"
	if awk '{print $1}' <"$work/entries.v" | grep -qv '^-'; then
		die "$p_tar holds something that is not a plain file (a folder, a link or a device):
$(cat "$work/entries.v")"
	fi
	tar -tf "$p_tar" | tr -d '\r' >"$work/entries"
	printf '%s\n' "$@" >"$work/entries.want"
	cmp -s "$work/entries" "$work/entries.want" || die "$p_tar does not carry exactly $* - it carries:
$(sed 's/^/  /' "$work/entries")
A release archive of any other shape is refused by ngc-store contents, and an entry \"./\" is the one that got a release as far as being built on 18.10.2026."
}

# The date of the commit apps/web is archived from, so one source always gives
# one archive - the same reasoning as in pack-release.sh, and the same number,
# so the binaries and the web tree do not disagree about when this release is.
if ! git rev-parse --git-dir >/dev/null 2>&1; then
	die "git refuses to work here. A worktree outside the repository needs the guard, for this call only:
  export GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=safe.directory GIT_CONFIG_VALUE_0=\"\$PWD\""
fi
git rev-parse --verify -q "$ref^{commit}" >/dev/null || die "no such commit: $ref"
commit=$(git rev-parse --verify "$ref^{commit}")
stamp=$(git show -s --format=%ct "$commit")
case $stamp in
'' | *[!0-9]*) die "the commit has no usable date: $ref" ;;
esac

# The source of this release, unpacked from the ref. Nothing of the work tree
# reaches the binaries from here on.
src=$work/src
mkdir -p -- "$src"
git -c core.autocrlf=false archive --format=tar "$commit" | tar -xf - -C "$src" ||
	die "the source of $ref could not be unpacked"
[ -f "$src/go.mod" ] || die "$ref carries no go.mod"
[ -d "$src/vendor" ] || die "$ref carries no vendor/, so the binaries cannot be built without the network"

echo "== programs, stamped $version, from $commit"
for arch in amd64 arm64; do
	runner=$(runner_for "$arch") || die "no runner for $arch"
	bindir=$work/$arch
	mkdir -p -- "$bindir"
	for prog in $api_progs $controller_progs; do
		pkg=$(pkg_for "$prog") || die "no package for $prog"
		(cd -- "$src" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
			go build -trimpath -ldflags "$ldflags" -o "$bindir/$prog" "$pkg") || die "$prog for $arch did not build"
		check_target "$bindir/$prog" "$arch"
		check_version "$bindir/$prog" "$prog" "$runner"
		echo "  $arch/$prog   $(sed -n '1p' "$work/ver.out")"
	done
	# shellcheck disable=SC2086
	pack "$out/ngc-api-$arch.tar" "$bindir" $api_progs
	# shellcheck disable=SC2086
	pack "$out/ngc-controller-$arch.tar" "$bindir" $controller_progs
	echo "  $out/ngc-api-$arch.tar"
	echo "  $out/ngc-controller-$arch.tar"
done

echo "== web"
# pack-release.sh writes release.payload.json for the web part ALONE. A release
# lists five files, and ngc-store release builds that payload over the whole
# directory, so the single-part payload is removed again: a stale one beside
# five archives is a trap.
sh "$self/pack-release.sh" "$ref" 1 "$out" || die "the web archive could not be packed"
rm -f -- "$out/release.payload.json"
# AND THEREFORE ITS CLOSING HINT DOES NOT APPLY HERE, and that has to be said in
# the same breath or the two lines contradict each other. pack-release.sh ends
# with "tools/sign-release.sh <dir> <key.pem>", which is right when pack is run
# on its own - it leaves the payload that script needs. Run from here the payload
# is gone one line later, so the call dies with "release.payload.json is missing
# in <dir>" (measured 29.10.2026, Punkt 96). The two ways also take different
# key files: sign-release.sh wants an openssl Ed25519 PEM, ngc-store wants its
# own key file and writes one when the path is empty.
echo "  IGNORE the sign-release.sh line above: it belongs to pack-release.sh run"
echo "  on its own. From here the payload of the WHOLE release is built by"
echo "  ngc-store release, named at the end of this run."

echo "== shape of all five, through the tool the user runs"
"$work/$host_arch/ngc-store" contents "$out" || die "ngc-store contents refuses this release"

echo
echo "built $version into $out"
echo "Nothing is signed. Sign it on the machine that holds the private release key:"
echo "  ngc-store release -version $version -seq <n> $out <key.pem>"
