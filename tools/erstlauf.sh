#!/usr/bin/env bash
# Erstlauf: the way from a published release to an app in the menu, as a run.
#
# WHY THIS FILE EXISTS. Between 19. and 21.10.2026 an NGC installation was built
# from a published release on a real machine for the first time. Six obstacles
# stood in the way, each small, together impassable - and tools/check.sh noticed
# none of them. It could not: three of the six do not live in a .go file at all
# (a systemd unit that ships in the ulh repository, a directory mode, a build
# flag that only a document asks for), and the other three only show when the
# parts are put together on a machine. check.sh compiles, vets and tests this
# repository. That is a different question from "can somebody install this".
#
# THE ORDER IS THE WAY, not a severity: install -> API and controller run ->
# pair the controller -> adopt it -> set system rights -> catalog is there ->
# set up an app -> the app shows in the menu -> the right "use" can be handed
# to somebody -> the app really answers. A station that cannot be reached makes
# every station behind it unmeasured, so the run stops at the first red and says
# where it stood.
#
# THE LAST TWO WERE ADDED ON 23.10.2026, and why is the point of them: on that
# day the way was walked to the end for the first time, and TWO more obstacles
# stood behind the last station. The way did not end where the run thought it
# did. Whoever walks it further and finds a third one adds station 11 - a run
# that ends one station before the person's goal is a run that measures the
# wrong way.
#
# WHAT THIS RUN CAN AND CANNOT DO, and the reason is in docs/erstlauf.md:
# stations 1, 2, 7, 9 and 10 have a half that is decidable from the sources of
# this repository and of ulh together - 9 and 10 by RUNNING the code through
# tools/erstlauf-probe - and that half runs anywhere. Everything else
# needs a machine that is installed from scratch, because podman-in-podman and
# fscrypt are not available in a container (this machine's WSL kernel says
# "# CONFIG_FS_ENCRYPTION is not set", measured 23.10.2026), and because a
# second installation on the one machine that exists would break it. Those
# stations are therefore named UNMEASURABLE unless --machine is given, and the
# run then ends with exit code 2 and the list - never with "erstlauf: ok".
# A run that quietly leaves out what it cannot do is worse than no run.
#
# THERE IS A PHASE C, and this file does not run it: tools/erstlauf-verteilt.sh
# asks the same station numbers against an installation whose parts stand on
# SEPARATE machines. It is not a nicety. Phase B installs api, store and the
# database on ONE machine, and on one machine three of the questions have no
# content at all: a store on localhost answers "is the catalog fetched over the
# network and is its certificate checked" wrongly, there is no second API for a
# sign-in to land on, and "did the app land on the machine it was sent to" has
# only one machine to land on. Whoever reads a green phase A and B and stops
# has not asked those three. docs/erstlauf.md, section 11, says what phase C
# found on six machines on 24.10.2026.
#
# The proof of a complete run is the printed line "erstlauf: ok", never the exit
# code alone (tools/check.sh is held to the same rule and says why).
set -euo pipefail

# This script uses BASH_SOURCE, arrays and [[ ]]. Called with sh, BASH_SOURCE is
# empty, dirname "" is ".", and a computed root can end up at "/" - which has
# already copied a whole filesystem into a temp directory once (messen.md,
# 16.10.2026). So the shell is asked first.
if [ -z "${BASH_VERSION:-}" ]; then
	echo "erstlauf: run with bash, not sh - this script needs BASH_SOURCE" >&2
	exit 1
fi

self=${BASH_SOURCE[0]}
root=$(cd -- "$(dirname -- "$self")/.." && pwd)

# A computed path is held against something only the right directory has, before
# anything is read from it. "Two levels up" is not a check.
marker=$root/shared/config/config.go
if ! grep -q "^[[:space:]]*AnchorFile[[:space:]]*= " "$marker" 2>/dev/null; then
	echo "erstlauf: $root is not the NGC repository (shared/config/config.go has no AnchorFile)" >&2
	exit 1
fi

# The leak test from messen.md, in band: a run whose exit codes are swallowed
# reports FAIL and exit 0 in the same breath, and the caller reads the 0. This
# is three lines and it has caught that shape before.
probe=$( (exit 7) >/dev/null 2>&1 || echo "$?")
[ "$probe" = 7 ] || {
	echo "erstlauf: this shell loses exit codes (the (exit 7) probe answered '$probe') - nothing measured here can be trusted" >&2
	exit 1
}

ulh=${NGC_ULH:-}
ulh_ref=${NGC_ULH_REF:-}
machine=""
mstaged=${NGC_MACHINE_STAGED:-/root/release/dist}
mulh=${NGC_MACHINE_ULH:-/root/release/ulh}
mkey=${NGC_MACHINE_RELEASE_KEY:-}
# Extra options for ssh and scp, for a target that is not in ~/.ssh/config -
# a port, an identity, another config file. Taken from the environment so that
# nothing here has to write into the user's ssh configuration.
sshopts=${NGC_SSH_OPTS:-}
while [ $# -gt 0 ]; do
	case $1 in
	--ulh)
		ulh=${2:-}
		shift 2
		;;
	--ulh-ref)
		ulh_ref=${2:-}
		shift 2
		;;
	--machine)
		machine=${2:-}
		shift 2
		;;
	--machine-staged)
		mstaged=${2:-}
		shift 2
		;;
	--machine-ulh)
		mulh=${2:-}
		shift 2
		;;
	--machine-release-key)
		mkey=${2:-}
		shift 2
		;;
	-h | --help)
		sed -n '2,51p' "$self"
		exit 0
		;;
	*)
		echo "erstlauf: unknown argument $1" >&2
		exit 1
		;;
	esac
done

fails=0
unmeasured=0
unmeasured_list=""

pass() { printf 'station %s %s: ok -- %s\n' "$1" "$2" "$3"; }
fail() {
	printf 'station %s %s: FAIL -- %s\n' "$1" "$2" "$3"
	fails=$((fails + 1))
}
# A fault of this script is not a finding about NGC, and the two must never be
# confused: a preparation that did not take leaves the run measuring nothing
# (messen.md, "a measuring script checks itself first"). It stops the run.
broken() {
	printf 'erstlauf: the run cannot measure station %s (%s): %s\n' "$1" "$2" "$3" >&2
	exit 1
}
unmeasurable() {
	printf 'station %s %s: UNMEASURABLE -- %s\n' "$1" "$2" "$3"
	unmeasured=$((unmeasured + 1))
	unmeasured_list="${unmeasured_list}    station $1 $2: $3
"
}

cd "$root"
git() { command git -c safe.directory="$PWD" "$@"; }

# A tree that lost tracked files measures less than it says, and a missing test
# file is the silent half. Asked before anything else, as in tools/check.sh.
gone=$(git diff --name-only --diff-filter=D HEAD | grep -c . || true)
[ "$gone" = 0 ] || {
	echo "erstlauf: $gone tracked files are missing from the tree - restore them before trusting any number from this run" >&2
	exit 1
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# ---------------------------------------------------------------------------
# station 1  installieren
#
# Obstacle 1 of October: ngc-api.service carried ProtectSystem=strict, and
# ReadWritePaths named /var/lib/ngc and /etc/ngc/tls - but not /etc/ngc. The API
# writes /etc/ngc/trust-anchor.json (config.AnchorFile), so the pin outside the
# database could never be moved up: pinned=1 while the database stood at 2, and
# the installation was stuck without one error message naming the cause.
#
# WHY A TEXT TEST DOES NOT CATCH THIS. ulh has a case for this unit, and it
# compares it byte for byte - including the line "ReadWritePaths=/var/lib/ngc
# /etc/ngc/tls". A test like that proves the file equals itself. The question is
# a different one and it spans two repositories: does the unit allow every path
# the program in its ExecStart writes? That is what is asked here, and the two
# halves are read from the two repositories rather than typed in again.
# ---------------------------------------------------------------------------
s1="installieren"
if [ -z "$ulh" ]; then
	for cand in "$root/../ulh" "/z/GitHub/ulh" "Z:/GitHub/ulh"; do
		[ -d "$cand/.git" ] && ulh=$cand && break
	done
fi
if [ -z "$ulh" ] || [ ! -d "$ulh/.git" ]; then
	unmeasurable 1 "$s1" "no ulh checkout found - the install scripts and the systemd units live in github.com/sorglos-it/ulh, not here; pass --ulh <path>"
else
	if [ -z "$ulh_ref" ]; then
		for cand in ngc-kette main; do
			command git -C "$ulh" -c safe.directory="$ulh" rev-parse --verify --quiet "$cand" >/dev/null && ulh_ref=$cand && break
		done
	fi
	[ -n "$ulh_ref" ] || broken 1 "$s1" "no usable ref in $ulh (tried ngc-kette, main); pass --ulh-ref"

	script=$work/ngc-api.sh
	command git -C "$ulh" -c safe.directory="$ulh" show "$ulh_ref:apps/cli/assets/scripts/ngc-api.sh" >"$script" 2>/dev/null ||
		unmeasurable 1 "$s1" "$ulh_ref of $ulh carries no apps/cli/assets/scripts/ngc-api.sh - the NGC scripts are on their own branches there"

	if [ -s "$script" ]; then
		# The preparation is checked before anything is concluded: if the unit
		# is no longer written by this function, or no longer confines the
		# program, this script read the wrong thing and says so instead of
		# reporting a green station.
		grep -q '^render_unit() {' "$script" || broken 1 "$s1" "ngc-api.sh of $ulh_ref has no render_unit() - the unit is written somewhere else now"
		grep -q "'ProtectSystem=strict'" "$script" || broken 1 "$s1" "ngc-api.sh of $ulh_ref no longer sets ProtectSystem=strict - this check is about what that line confines"

		# The paths the unit grants, with the script's own variables resolved
		# out of the script itself - not out of this file.
		rwp=$(sed -n 's/.*"ReadWritePaths=\(.*\)".*/\1/p' "$script")
		[ -n "$rwp" ] || broken 1 "$s1" "no ReadWritePaths= line found in ngc-api.sh of $ulh_ref"
		for var in NGC_DATA NGC_TLS NGC_STATE_DIR; do
			val=$(sed -n "s/^$var=\"\([^\"]*\)\".*/\1/p" "$script" | head -1)
			[ -n "$val" ] && rwp=${rwp//\$\{$var\}/$val}
		done
		case $rwp in
		*'${'*) broken 1 "$s1" "ReadWritePaths of ngc-api.sh still holds an unresolved variable: $rwp" ;;
		esac

		# The paths the program writes. Each one is named with the place in THIS
		# repository that writes it, and that place is checked to still name the
		# constant - a renamed writer must break this run loudly, not pass it.
		# Read-only paths (api.json) are not listed: ProtectSystem=strict leaves
		# reading alone.
		writes=(
			"AnchorFile|apps/server-api/classes/state/trust.go|the trust anchor outside the database, moved up on every new version of the list"
		)
		missing=""
		for w in "${writes[@]}"; do
			const=${w%%|*}
			restw=${w#*|}
			where=${restw%%|*}
			why=${restw#*|}
			path=$(sed -n "s/^[[:space:]]*$const[[:space:]]*=[[:space:]]*\"\([^\"]*\)\".*/\1/p" shared/config/config.go)
			[ -n "$path" ] || broken 1 "$s1" "shared/config/config.go no longer defines $const"
			grep -q "$const" "$where" || broken 1 "$s1" "$where no longer names config.$const - this run would be asking about a path nobody writes"
			dir=$(dirname "$path")
			granted=no
			for g in $rwp; do
				case $dir/ in
				"$g"/* | "$g"/) granted=yes ;;
				esac
				[ "$dir" = "$g" ] && granted=yes
			done
			[ "$granted" = yes ] || missing="$missing$path ($why, written in $where); "
		done
		if [ -n "$missing" ]; then
			fail 1 "$s1" "ngc-api.service sets ProtectSystem=strict and grants only '$rwp', so the API cannot write: $missing- the installation gets stuck with a pin below the stored version and names no cause"
		else
			pass 1 "$s1" "every path the API writes is granted by ngc-api.service of $ulh_ref ($rwp)"
		fi
	fi
fi

# ---------------------------------------------------------------------------
# station 2  API und Controller laufen
#
# Obstacle 2 of October: the binaries of the release said 0.0.0-dev. The
# interface holds the version of a machine against validVersion
# (MAJOR.MINOR.PATCH) and throws away the WHOLE answer when one machine fails
# it, so the user read "Die Rechner konnten nicht geladen werden." and no
# version number anywhere.
#
# The version only comes from -ldflags "-X main.Version=..."; the fallback in
# the code is 0.0.0-dev on purpose. docs/release-weg.md writes the command down
# for a human to type. That is the hole: nothing here builds the servers, so
# nothing checks that what gets built carries a number the interface accepts.
#
# This station asks two things and gets the pattern from the interface itself,
# so a change to validVersion cannot leave this check behind:
#   a) the pattern, read out of apps/web/.../text.js, really accepts 1.2.3 and
#      really refuses 0.0.0-dev (otherwise the extraction failed, not the code),
#   b) something in this repository builds ngc-api and ngc-controller with the
#      version stamped in, and what it builds prints a number the pattern takes.
# ---------------------------------------------------------------------------
s2="API und Controller laufen"
textjs=apps/web/public/assets/js/contracts/text.js
jsre=$(sed -n 's|^const VERSION = /\(.*\)/;$|\1|p' "$textjs")
[ -n "$jsre" ] || broken 2 "$s2" "no 'const VERSION = /.../;' in $textjs - the interface's pattern could not be read"
# JavaScript and grep -E write this pattern the same way; the control below is
# what proves the transfer, not the belief that they do.
grep -Eq -- "$jsre" <<<"1.2.3" || broken 2 "$s2" "the pattern taken from $textjs refuses 1.2.3 - it was not transferred correctly, so nothing below means anything"
grep -Eq -- "$jsre" <<<"0.0.0-dev" && broken 2 "$s2" "the pattern taken from $textjs ACCEPTS 0.0.0-dev - that is the version this station is about, so the transfer is wrong"

# WHAT IS NOT DONE HERE, and why: a grep for "main.Version=" under tools/ would
# be satisfied by a comment, and this station would then be green while the
# release still carried 0.0.0-dev - exactly the hole it is written against. So
# the builder is RUN and the binaries it leaves behind are asked for their
# version. The place is fixed by name, because a check that has to guess which
# file builds the release cannot say anything when it finds none.
builder=tools/build-servers.sh
if [ ! -x "$builder" ]; then
	fail 2 "$s2" "there is no executable $builder, so nothing in this repository builds the servers with the version stamped in; the command stands only in docs/release-weg.md for a human to type, and a release built without it reports 0.0.0-dev - on which the interface throws the WHOLE machine list away with 'Die Rechner konnten nicht geladen werden.'"
else
	out=$work/servers
	mkdir -p "$out"
	# The programs have to RUN to be asked, so they are built for THIS machine,
	# not for the release target: a linux binary answers nothing on a developer's
	# Windows. The flag that stamps the number is the same work for every target,
	# and which one was asked is said in the station's message.
	hostos=$(go env GOHOSTOS)
	hostarch=$(go env GOHOSTARCH)
	ext=""
	[ "$hostos" = windows ] && ext=".exe"
	NGC_BUILD_OS="$hostos" "$builder" "$out" "$hostarch" >"$work/build.log" 2>&1 ||
		broken 2 "$s2" "$builder failed for $hostos/$hostarch, so this station says nothing about versions: $(tr '\n' ' ' <"$work/build.log")"
	bad=0
	for name in ngc-api ngc-controller; do
		bin=$out/$name$ext
		[ -s "$bin" ] || {
			fail 2 "$s2" "$builder wrote no $name into the directory it was given, so the release has no stamped $name"
			bad=1
			continue
		}
		got=$("$bin" -version 2>&1 | head -1 | awk '{print $2}')
		grep -Eq -- "$jsre" <<<"$got" || {
			fail 2 "$s2" "the $name that $builder produced reports '$got', which the interface's validVersion refuses"
			bad=1
		}
	done
	# The control in the other direction, and it is not decoration: a pattern
	# that accepts everything would make this station green for ever. An
	# unstamped build has to be refused by the SAME pattern in the SAME run.
	GOFLAGS="-buildvcs=false" go build -o "$work/plain$ext" ./apps/server-api ||
		broken 2 "$s2" "./apps/server-api does not build, so the control of this station cannot be made"
	plain=$("$work/plain$ext" -version 2>&1 | head -1 | awk '{print $2}')
	grep -Eq -- "$jsre" <<<"$plain" && {
		fail 2 "$s2" "a build WITHOUT -ldflags reports '$plain' and the interface accepts it - then this station can no longer tell a stamped build from an unstamped one and proves nothing"
		bad=1
	}
	[ "$bad" = 0 ] && pass 2 "$s2" "$builder produced ngc-api and ngc-controller for '"$hostos/$hostarch"' with versions the interface's validVersion takes, and an unstamped build is refused by the same pattern"
fi

# ---------------------------------------------------------------------------
# station 3  Controller koppeln
# station 4  aufnehmen
# station 5  Systemrechte setzen
# station 6  Katalog da
#
# These four have no half that is decidable from the sources. Why, for each, so
# that the next person can see a gap in the search rather than take a sentence
# on faith:
#   3 pairing is a Noise handshake between two running programs; there is
#     nothing in a file that says it succeeds.
#   4 adopting writes rootUsers/rootRoles of the new node into the trust list.
#     The October obstacle was that nobody could ever change them afterwards -
#     a question about a path through the interface, not about a string in it,
#     and a grep for a name would go green on a dead button.
#   5 the same, one step on.
#   6 the catalog is fetched hourly (appstore.Stores.Refresh). Whether a hand
#     can trigger it is a question about who calls Refresh, and a call graph is
#     not something this script should guess at. The run on a machine measures
#     the only thing that matters: is the catalog there within minutes of the
#     installation, or does the interface say "Der Katalog dieses Stores ist
#     nicht echt." for an hour.
# ---------------------------------------------------------------------------
if [ -z "$machine" ]; then
	unmeasurable 3 "Controller koppeln" "needs two running programs on a machine installed from scratch; pass --machine <ssh target>"
	unmeasurable 4 "aufnehmen" "needs a controller that was really adopted, and a click through the interface; pass --machine <ssh target>"
	unmeasurable 5 "Systemrechte setzen" "needs an adopted controller; pass --machine <ssh target>"
	unmeasurable 6 "Katalog da" "needs a running API with a store in its trust list; pass --machine <ssh target>"
else
	# Phase B. The companion script goes over as a file and runs there; its
	# station lines are in the same shape as the ones above and are taken as
	# they come, because the machine is the only place that can answer them.
	# What is counted here is counted out of its OUTPUT and not out of its exit
	# code alone - a run whose lines were lost must not become a green total.
	command -v ssh >/dev/null 2>&1 || broken 3 "Maschinenlauf" "no ssh on this machine, so --machine cannot reach $machine"
	# shellcheck disable=SC2086
	scp $sshopts -q -- "$root/tools/erstlauf-maschine.sh" "$machine:/tmp/erstlauf-maschine.sh" ||
		broken 3 "Maschinenlauf" "tools/erstlauf-maschine.sh could not be copied to $machine"
	mlog=$work/machine.log
	echo "erstlauf: phase B on $machine (its station lines are indented; the ones above come out of the sources):"
	set +e
	# shellcheck disable=SC2029
	ssh $sshopts -- "$machine" "bash /tmp/erstlauf-maschine.sh --staged '$mstaged' --ulh '$mulh'${mkey:+ --test-release-key '$mkey'}" >"$mlog" 2>&1
	mrc=$?
	set -e
	sed 's/^/    /' "$mlog"
	nlines=$(grep -c '^station ' "$mlog" || true)
	# A machine run that printed no station line printed nothing this run may
	# count. That is the "a run that hits no case still reports all in order"
	# trap of messen.md, and it is caught here instead of being believed.
	[ "$nlines" -ge 1 ] ||
		broken 3 "Maschinenlauf" "the run on $machine printed no station line (exit $mrc), so it measured nothing"
	mfail=$(grep -c '^station .*: FAIL' "$mlog" || true)
	munm=$(grep -c '^station .*: UNMEASURABLE' "$mlog" || true)
	fails=$((fails + mfail))
	unmeasured=$((unmeasured + munm))
	unmeasured_list="${unmeasured_list}$(grep '^station .*: UNMEASURABLE' "$mlog" | sed 's/^/    /')
"
	echo "erstlauf: the machine run on $machine ended with code $mrc: $nlines station(s), $mfail red, $munm unmeasurable"
fi

# ---------------------------------------------------------------------------
# station 7  App einrichten
#
# Obstacle 5 of October: /var/lib/ngc/apps was made with 0700, and the rootless
# "podman load" of the app's own user got EACCES on its own home directory. The
# machine was patched to 0711 by hand; the code still says 0700.
#
# The assertion is the reason, not the number: every app runs as its own user,
# and that user has to traverse config.AppDataDir to reach its own directory
# below it. A mode without the execute bit for others makes that impossible,
# whatever the number is.
# ---------------------------------------------------------------------------
s7="App einrichten (Quelle)"
datago=apps/server-controller/classes/runtime/data.go
mode=$(sed -n 's/.*mkdirAll(paths\.appData, 0o\([0-7]*\)).*/\1/p' "$datago" | head -1)
[ -n "$mode" ] || broken 7 "$s7" "no 'mkdirAll(paths.appData, 0o...)' in $datago - the directory of the app data is made somewhere else now"
# The app user is not root and not in root's group, so only the last digit can
# let it through. $((8#...)) reads the digits as octal, which is what they are.
if [ $((8#$mode & 1)) = 0 ]; then
	fail 7 "$s7" "$datago makes config.AppDataDir with 0$mode, which no app user can traverse: every app runs as its own user and has to reach its directory below it, so the rootless 'podman load' fails with EACCES on its own home"
else
	pass 7 "$s7" "$datago makes config.AppDataDir with 0$mode, which lets an app user traverse it"
fi

# ---------------------------------------------------------------------------
# station 8  App erscheint im Menue
#
# Obstacle 6 of October, and the only one of the six that is a circle: the
# desktop shows an app only to somebody who holds the right "use" it, and the
# rights page offered rights only for apps one could already see. Nobody could
# step into that circle from outside. Whether it is open is a question about a
# path through the interface and is measured on the machine, not here.
# ---------------------------------------------------------------------------
if [ -z "$machine" ]; then
	unmeasurable 8 "App erscheint im Menue" "needs an app that really runs, and a click through the interface with a second user; pass --machine <ssh target>"
fi

# ---------------------------------------------------------------------------
# station 9  Recht "benutzen" vergeben
# station 10 Die App ist wirklich bedienbar
#
# WHY THE WAY DID NOT END AT STATION 8. On 23.10.2026 the way was walked to the
# end for the first time - and two more obstacles stood BEHIND the last station,
# and the run of that day would have noticed neither:
#
#   9  The desktop showed an app only to somebody holding the right "use", and
#      the rights page offered that right only for apps one could already see.
#      A closed circle: the first right of a freshly set up app could be given
#      to nobody. It is fixed (rights.adminApps), and station 8 above would
#      still be green with it broken again, because station 8 asks about the
#      MENU and the circle is about the PAGE THAT FILLS IT.
#  10  The app opened and did not answer. "muh" came back as
#      "/bin/sh: 2: muh: not found", "ls" came back as NOTHING, and the same
#      "ls" inside the container printed a listing - so the output went astray
#      on the way, and an open window proved nothing at all.
#
# These two are therefore stations of their own, and they are measured by
# RUNNING the code: tools/erstlauf-probe asks rights.Of, rights.Set and
# serve.RunService through their exported doors. Its head says why that is a
# program and not a grep, and not a go test.
#
# BOTH STREAMS, SEPARATELY, and that is the whole lesson of obstacle 10: one of
# the two arrived and the other did not. A station that read "something came
# back" would have been green on the very fault, so the probe asks for stdout on
# the channel and for stderr in the journal, each on its own line.
# ---------------------------------------------------------------------------
s9="Recht benutzen vergeben"
s10="App ist wirklich bedienbar"

# The preparation of station 9, and it is the half the probe deliberately does
# NOT restate: the circle runs through two places outside package rights, and a
# probe that copied their condition would be proving its own copy. If either
# moved, this run says it cannot measure instead of reporting green.
menugo=apps/server-api/classes/coreapp/desktop.go
grep -q 'rq\.Rights\[a\.ID+"/"+core\.RightUse\]' "$menugo" ||
	broken 9 "$s9" "$menugo no longer filters the desktop's app list by core.RightUse - the circle this station is about ran through that filter, and where it now runs cannot be read from here"
pagejs=apps/web/public/assets/js/apps/users.js
grep -q 'this\.apps = this\.appsNow();' "$pagejs" ||
	broken 9 "$s9" "the rights page of $pagejs no longer builds its offer from the desktop's app list (this.appsNow()) - the other half of the circle is somewhere else now"
grep -q 'core\.RIGHT_USE' "$pagejs" ||
	broken 9 "$s9" "the rights page of $pagejs no longer puts core.RIGHT_USE into the rights it offers - then the right this station is about is offered by nothing"
grep -q 'adminApps(ctx, db, out)' apps/server-api/classes/rights/rights.go ||
	broken 9 "$s9" "rights.Of no longer calls adminApps - the one function that opens the circle; what opens it now cannot be read from here"

# The preparation of station 10: the probe asks serve.RunService, so the two
# streams have to still part there. What is asked for is that both ends are
# handed over in that one call and that the channel's writer is in this file at
# all - NOT which of the two gets which. That is on purpose and it is the whole
# difference between a preparation and a measurement: a run that pinned the
# assignment here would answer "cannot measure" for a SWAP of the two streams,
# which is precisely the fault of 23.10.2026 and has to come back as a red
# station instead.
callgo=apps/server-controller/classes/serve/call.go
grep -Eq 'Stdout: [^,]+, Stderr: [^,]+,' "$callgo" ||
	broken 10 "$s10" "$callgo no longer hands the engine a Stdout and a Stderr in one call - that line is the seam where the person's channel and the journal part, and this station measures it through serve.RunService"
grep -q 'in\.Out' "$callgo" ||
	broken 10 "$s10" "$callgo never names in.Out - the output of a service no longer reaches the writer the caller handed in, and where it goes now cannot be read from here"

probelog=$work/probe.log
set +e
go run ./tools/erstlauf-probe >"$probelog" 2>&1
prc=$?
set -e
[ "$prc" != 3 ] ||
	broken 9 "$s9 / $s10" "tools/erstlauf-probe could not measure: $(tr '\n' ' ' <"$probelog")"
# Every check this probe owes, by name. A probe that printed fewer lines than
# it owes measured less than this run reports, and that is the "a run that hits
# no case still reports all in order" trap of messen.md.
for id in recht-angeboten recht-gueltig recht-vergeben ausgabe-stdout ausgabe-stderr; do
	grep -q "^check $id: " "$probelog" ||
		broken 9 "$s9 / $s10" "tools/erstlauf-probe printed no line for the check '$id' (exit $prc), so this run would count a station it never measured: $(tr '\n' ' ' <"$probelog")"
done
# The probe's own lines are the sentences, and they are carried over as they
# come: the probe is the only thing here that saw the answer.
p9=$(grep -c '^check recht-.*: FAIL' "$probelog" || true)
p9u=$(grep -c '^check recht-.*: UNMEASURABLE' "$probelog" || true)
p10=$(grep -c '^check ausgabe-.*: FAIL' "$probelog" || true)
p10u=$(grep -c '^check ausgabe-.*: UNMEASURABLE' "$probelog" || true)
if [ "$p9" != 0 ]; then
	fail 9 "$s9" "$(grep '^check recht-.*: FAIL' "$probelog" | sed 's/^check [^:]*: FAIL -- //' | tr '\n' ' ')"
elif [ "$p9u" != 0 ]; then
	unmeasurable 9 "$s9" "$(grep '^check recht-.*: UNMEASURABLE' "$probelog" | sed 's/^check [^:]*: UNMEASURABLE -- //' | tr '\n' ' ')"
else
	pass 9 "$s9" "a freshly set up app with no rights row anywhere can be freed for a second person: the right is in the offer, the contract takes it, and the user of the role holds it afterwards"
fi
if [ "$p10" != 0 ]; then
	fail 10 "$s10" "$(grep '^check ausgabe-.*: FAIL' "$probelog" | sed 's/^check [^:]*: FAIL -- //' | tr '\n' ' ')"
elif [ "$p10u" != 0 ]; then
	unmeasurable 10 "$s10" "$(grep '^check ausgabe-.*: UNMEASURABLE' "$probelog" | sed 's/^check [^:]*: UNMEASURABLE -- //' | tr '\n' ' ')"
else
	pass 10 "$s10" "a service's STDOUT reaches the channel the person looks at and its STDERR reaches the journal instead - the two streams measured apart, which is the only way the fault of 23.10.2026 shows"
fi

# ---------------------------------------------------------------------------
# WHAT NO PHASE OF THIS RUN CAN CLICK, AND IS THEREFORE NOT MEASURED.
#
# This is not a station that is waiting for a machine: --machine does not help
# here, and saying nothing about it would be the dishonest half of station 9.
# A second person needs a USER, and a user only comes into being with a new
# version of the trust list, which is signed IN THE BROWSER with the key of the
# person doing it (apps/web, the question for the person's key; the API only
# ever verifies a list it is handed). Nothing in a shell holds that key, so
# neither phase can make the user that station 9 hands the right to. The probe
# builds its own list in memory and says so - it measures the rights, never the
# signature.
# ---------------------------------------------------------------------------
unmeasurable 9 "Benutzer anlegen (Unterschrift)" "a new user is a new version of the trust list, and that version is signed in the BROWSER with the person's own key - no phase of this run holds it, so the probe of station 9 builds its list in memory and nothing here says a real user can be created"
if [ -z "$machine" ]; then
	unmeasurable 10 "App antwortet auf der Maschine" "station 10 above measures the seam where the two streams part (serve.RunService with a probe engine); that podman's own engine wires them the same way needs a running container, so pass --machine <ssh target>"
fi

# ---------------------------------------------------------------------------
echo "erstlauf: $fails red, $unmeasured unmeasurable"
if [ "$unmeasured" != 0 ]; then
	printf 'erstlauf: these stations did NOT run -- the run says nothing about them:\n%s' "$unmeasured_list"
fi
[ "$fails" = 0 ] || exit 1
# Exit 2 and no "ok" line: the way was not walked to the end. A run that says
# ok here would be the seventh obstacle.
[ "$unmeasured" = 0 ] || exit 2
echo "erstlauf: ok"
