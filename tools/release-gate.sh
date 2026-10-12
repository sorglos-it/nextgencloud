#!/usr/bin/env bash
# release-gate.sh - the checks that have to pass BEFORE a release is signed.
#
# Usage: tools/release-gate.sh [<release directory>]
#
# It is the counterpart of tools/check.sh. check.sh is the gate of a COMMIT
# and runs everywhere; this one is the gate of a RELEASE and most of it can
# only run on Linux. It signs nothing, publishes nothing and sets no tag.
#
# ============================================================================
# NOT EXECUTED. This script has never run end to end, and that is not a
# formality: on the machine it was written on (Windows 11, 12.10.2026) there
# is no podman, no docker, no systemd, no jq and no shellcheck, and the WSL
# beside it (Debian 13.7) has gcc but NO Go. Each step below says which of
# the four stages it has reached - written / compiled / delivered / RUN - and
# the steps marked RUN are the ones that were measured on this machine, in a
# worktree on the LOCAL disk C:, at commit 0bc2659.
#
# What that means for whoever runs this first: expect the script itself to be
# wrong somewhere. Read the step that fails before believing it.
# ============================================================================
set -euo pipefail
cd "$(dirname "$0")/.."

dir=${1:-}
fail=0

say() { printf '%s\n' "$*"; }
step() { printf '\n== %s\n' "$*"; }
bad() {
	printf 'release-gate: %s\n' "$1" >&2
	fail=1
}
# missing is for a tool this machine does not have. It is LOUD and it counts
# as a failure, because the worst measurement is the one that switches itself
# off when the environment is short of something (claude-md/messen.md).
missing() {
	printf 'release-gate: MISSING TOOL %s - %s\n' "$1" "$2" >&2
	fail=1
}

step "0. the worktree is whole"
# RUN (measured 12.10.2026, in the worktree on C:, answer 0). A hollow
# worktree is the trap of this project: missing vendor code breaks the build
# visibly, a missing TEST FILE makes the suite run with fewer cases and look
# green.
#
# The guard is set for THIS process only and nothing global is touched - the
# same thing tools/check.sh does with "-c safe.directory". Without it git
# answers 128 in a worktree outside the repository, and the next step would
# then report a wrong cause. Measured here on the first run of this script.
export GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=safe.directory GIT_CONFIG_VALUE_0="$PWD"
if ! git rev-parse --git-dir >/dev/null 2>&1; then
	bad "git refuses to work here even with the guard: $(git rev-parse --git-dir 2>&1 || true)"
else
	gone=$(git diff --name-only --diff-filter=D HEAD | wc -l | tr -d ' ')
	say "deleted files against HEAD: $gone"
	[ "$gone" = 0 ] || bad "the worktree is short $gone files. Restore the DELETED paths only: git restore --source=HEAD -- <paths>"
fi

step "1. the commit gate"
# RUN (measured: printed "check: ok", exit 0, no pipe). The proof is the
# printed line, not the zero - the exit code of a pipe belongs to its last
# member, and this project has been bitten by that.
out=$(bash tools/check.sh 2>&1) || true
printf '%s\n' "$out" | tail -3
printf '%s' "$out" | grep -qx 'check: ok' || bad "tools/check.sh did not print \"check: ok\""

step "2. the race detector"
# RUN on Windows: this gate own run on 12.10.2026 found nothing outside
# shared/trust. NOT run on Linux, where the programs actually run.
#
# And the zero is BACKED: a throwaway mutation with two writers on one
# variable, in a module of its own, produced exactly one "WARNING: DATA RACE"
# and exit 1 - so the detector was armed in that run and the zero is a
# measurement, not a habit. shared/trust is named separately on purpose: its case measures
# memory ceilings and the instrumentation itself takes more, so it cannot
# hold under -race. That is not a data race and it is not a reason to skip
# the rest.
#
# shared/trust is therefore run SEPARATELY and its result is reported, not
# counted. Leaving it in ./... would make this gate red on every release and
# a gate that is always red gets switched off (docs/arbeitsweise.md, "a
# watchman that goes red on good code gets switched off"). Leaving it out
# silently would hide the day it goes red for a REAL reason, so it is run and
# printed.
if command -v gcc >/dev/null 2>&1; then
	pkgs=$(go list -buildvcs=false ./... | grep -v '/shared/trust$')
	# shellcheck disable=SC2086
	CGO_ENABLED=1 go test -count=1 -buildvcs=false -race $pkgs || bad "the race detector found something outside shared/trust"
	say "-- shared/trust separately; it CANNOT hold under -race (its case measures memory ceilings and the instrumentation takes more). Reported, not counted:"
	CGO_ENABLED=1 go test -count=1 -buildvcs=false -race ./shared/trust/ || say "   shared/trust red, as expected. Red for another reason is a finding: read the output."
else
	missing "gcc" "without a C compiler there is no -race. On Windows it lies at C:\\esp\\tools\\llvm-mingw\\bin\\gcc.exe"
fi

step "3. known vulnerabilities"
# RUN once (12.10.2026, 0 reachable vulnerabilities) - and the CHANGELOG had
# claimed it before that, which is why it is a step here and not a habit.
if command -v govulncheck >/dev/null 2>&1; then
	govulncheck ./... || bad "govulncheck reports reachable vulnerabilities"
else
	missing "govulncheck" "go install golang.org/x/vuln/cmd/govulncheck@latest"
fi

step "4. the shell scripts"
# NOT RUN: no shellcheck on this machine. Named rather than left out, because
# the release path is partly shell and an unchecked script is where an
# unquoted variable waits.
if command -v shellcheck >/dev/null 2>&1; then
	shellcheck tools/*.sh shared/testdata/release/verify.sh || bad "shellcheck found something"
else
	missing "shellcheck" "the scripts of the release path are unchecked without it"
fi

step "5. the release directory"
# NOT RUN: no release directory exists anywhere yet, which is the whole point
# of docs/release-weg.md. Written and compiled only.
if [ -z "$dir" ]; then
	say "no directory given - steps 5 and 6 skipped. This is NOT a pass."
	say "A release directory holds, for every part of tools/release:"
	say "  ngc-web-all.tar  ngc-api-{amd64,arm64}.tar  ngc-controller-{amd64,arm64}.tar"
	say "  release.json (the signed sign.Envelope)  release-key.pem"
else
	[ -d "$dir" ] || bad "no such directory: $dir"
	for f in release.json release-key.pem; do
		[ -f "$dir/$f" ] || bad "the release directory holds no $f"
	done
	# ngc-store check judges one part at a time, the same way
	# shared/testdata/release/verify.sh judges it on the server.
	go build -buildvcs=false -o "${TMPDIR:-/tmp}/ngc-store" ./apps/cli || bad "ngc-store does not build"
	for pa in "ngc-web all" "ngc-api amd64" "ngc-api arm64" "ngc-controller amd64" "ngc-controller arm64"; do
		set -- $pa
		if [ -f "$dir/$1-$2.tar" ]; then
			"${TMPDIR:-/tmp}/ngc-store" check "$dir" "$1" "$2" || bad "ngc-store check refuses $1 $2"
		else
			say "  $1-$2.tar is not in this release"
		fi
	done
fi

step "6. the shell checker of the server says the same"
# NOT RUN, and it has never run with a real jq: shared/testdata/release/
# verify.sh needs jq, and this machine has none. docs/STAND.md carries the
# same open point ("On the first Linux server with a real jq:
# TestReleaseScript, 31 cases").
if [ -z "$dir" ]; then
	say "no directory given - skipped. This is NOT a pass."
elif command -v jq >/dev/null 2>&1; then
	sh shared/testdata/release/verify.sh "$dir" || bad "verify.sh refuses this release"
else
	missing "jq" "verify.sh is what the SERVER runs; until it has run with a real jq, the server side of this gate is unmeasured"
fi

step "result"
if [ "$fail" = 0 ]; then
	# The printed line is the proof, as in check.sh.
	say "release-gate: ok"
else
	say "release-gate: NOT ok - read the lines above"
	exit 1
fi
