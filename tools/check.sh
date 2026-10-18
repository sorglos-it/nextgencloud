#!/usr/bin/env bash
# Runs the checks every commit must pass.
#
# THE ORDER IS A DECISION: everything that can say "this run would measure less
# than it claims" comes FIRST, before go vet, go test and go build. Two reasons,
# both measured 17.10.2026 against this very script: the structural checks cost
# seconds while the three builds cost three minutes, and a hole in them makes
# those three minutes worthless. A check tool that silently checks less than it
# says is the most dangerous file in the repo, so this one is held to the rule it
# enforces: the proof is the printed line "check: ok", never the exit code alone.
set -euo pipefail
cd "$(dirname "$0")/.."
# The go tool stamps the build with VCS data and runs git for it, which fails the
# same way in a worktree outside the repo ("error obtaining VCS status"). The check
# produces no artefact, so the stamp is not needed here. Measured 10.10.2026.
export GOFLAGS="${GOFLAGS:+$GOFLAGS }-buildvcs=false"

mod=github.com/sorglos-it/nextgencloud
# A list that comes back nearly empty is a failed list, not a small repo: go list
# answers "packages=0" in a worktree it cannot stat (measured 12.10.2026). The
# floor is far below the real count (69 packages, 393 files on main) so it never
# needs raising for ordinary growth, and far above zero.
floor=20

die() {
	echo "check: $*" >&2
	exit 1
}

# safe.directory for THESE calls only: in a worktree outside the repo git refuses
# with "dubious ownership" and exit 128, and a caller that pipes the script reads
# the pipe's 0 instead. Measured 10.10.2026. Never set this globally.
git() { command git -c safe.directory="$PWD" "$@"; }

# A tree that lost tracked files measures less than it says, and the dangerous
# half is silent: missing vendor code breaks the build loudly, a missing TEST
# file just runs fewer cases and still prints "check: ok". Worktrees here have
# gone hollow during work, repeatedly (629, 145, 123, 159 and once all 1097
# files; cause unmeasured, see messen.md). So this is asked before anything is
# measured, not once a day.
gone=$(git diff --name-only --diff-filter=D HEAD | grep -c . || true)
[ "$gone" = 0 ] || die "$gone tracked files are missing from the tree - restore them ('git restore --source=HEAD -- <paths>', never 'git restore .') before trusting any number from this run"

files=$(git ls-files '*.go' ':!:vendor/*')
nfiles=$(printf '%s\n' "$files" | grep -c . || true)
# An empty list is not "nothing to format", it is a failed listing. git's own
# non-zero exit ends the script through set -e; this catches the other half.
[ "$nfiles" -ge "$floor" ] || die "git ls-files named only $nfiles .go files, fewer than the floor of $floor - that is a failed listing, not a clean tree"

# gofmt -l prints NOTHING for a file it cannot parse: it writes the parse error
# to stderr and exits 2. Captured inside a condition, that reads exactly like
# "everything is formatted". Measured 17.10.2026 on an unparsable file: exit 2,
# no name on stdout. So the exit code is read, not only the output.
fmterr=$(mktemp)
covered=$(mktemp)
trap 'rm -f "$fmterr" "$covered"' EXIT
set +e
unformatted=$(gofmt -l $files 2>"$fmterr")
fmtrc=$?
set -e
[ "$fmtrc" = 0 ] || die "gofmt itself failed (exit $fmtrc): $(tr '\n' ' ' <"$fmterr")"
if [ -n "$unformatted" ]; then
	echo "$unformatted"
	echo "gofmt needed"
	exit 1
fi

listed() { # $1 = -f template, $2 = build tags (may be empty), rest = environment
	local tpl=$1 tags=$2
	shift 2
	env "$@" go list ${tags:+-tags="$tags"} -f "$tpl" ./...
}

# Every list below is taken WITHOUT "|| true" on the go call, and that is the
# point. Until 17.10.2026 the trusttest check read
#   bad=$(go list ... | grep ... | grep -v ... || true)
# so a go list that FAILED left bad empty, and the check passed by never
# running. Measured with a go that fails only on "list": the supply-chain guard
# was skipped for both targets and the script still printed "check: ok" with
# exit 0. Now the assignment ends the script (set -e with pipefail), and the
# floor catches the other shape of the same hole - a list that succeeds with
# nothing in it.

# Every tracked .go file has to be compiled by at least ONE configuration this
# script checks, or nothing ever type-checks it. Measured 17.10.2026: a file
# with "//go:build darwin" and an undefined call in it passed the whole script
# ("check: ok", exit 0), because gofmt only judges layout and no target here
# builds darwin. A file that no target builds is named, not carried along.
tpl_files='{{$p := .ImportPath}}{{range .GoFiles}}{{$p}}|{{.}}
{{end}}{{range .CgoFiles}}{{$p}}|{{.}}
{{end}}{{range .TestGoFiles}}{{$p}}|{{.}}
{{end}}{{range .XTestGoFiles}}{{$p}}|{{.}}
{{end}}'
#
# Two kinds of file are exempt, and both are named here rather than silently
# dropped. Files under testdata/ are FIXTURES: the go tool ignores such
# directories on purpose, and the analyzer cases in
# apps/server-db/classes/sqlstore read them as text (28 files on main). And the
# "race" tag is listed as a tag although only -race really sets it, with a
# matching "go vet -tags=race" below, so apps/server-api/classes/login's
# race-only case is type-checked here and not only on a machine with a C
# compiler.
# The host is listed twice, with and without the race tag: the login package
# has one case for each side (runs_race_test.go is "//go:build race",
# runs_test.go is "//go:build !race"), so one list alone always leaves one of
# them uncovered. It is the union of the configurations that has to cover the
# tree, not any single one.
for cfg in \
	"modules GOOS=windows GOARCH=amd64" \
	"modules,race GOOS=windows GOARCH=amd64" \
	"modules CGO_ENABLED=0 GOOS=linux GOARCH=amd64" \
	"modules CGO_ENABLED=0 GOOS=linux GOARCH=arm64"; do
	# shellcheck disable=SC2086
	out=$(listed "$tpl_files" $cfg)
	n=$(printf '%s\n' "$out" | grep -c . || true)
	[ "$n" -ge "$floor" ] || die "go list ($cfg) named only $n files, fewer than the floor of $floor - the list failed, it did not find a small repo"
	printf '%s\n' "$out" | sed -e "s|^$mod/\{0,1\}||" -e 's/|/\//' -e 's|^/||' >>"$covered"
done
sort -u "$covered" -o "$covered"
uncovered=$(printf '%s\n' "$files" | grep -v '/testdata/' | grep -vxF -f "$covered" || true)
[ -z "$uncovered" ] || die "no checked configuration compiles these files, so nothing type-checks them: $(printf '%s' "$uncovered" | tr '\n' ' ')"

# tools/modules.go names the vendored modules that NO part imports yet, and a
# line for a module that IS imported is the one thing that file must not hold -
# its own words. Nothing checked it. Test imports count too: the line is stale
# as soon as any code in this repo names the module.
allimports=$(listed '{{.ImportPath}}: {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}} ' "" GOOS=windows GOARCH=amd64)
n=$(printf '%s\n' "$allimports" | grep -c . || true)
[ "$n" -ge "$floor" ] || die "go list named only $n packages, fewer than the floor of $floor"
for m in $(sed -n 's/^[[:space:]]*_ "\([^"]*\)".*/\1/p' tools/modules.go); do
	# Only the package tools ITSELF is exempt, written with the colon that ends
	# the import path in the listing: "^$mod/tools" without it also exempted
	# every package BELOW tools/, and a measurement on 17.10.2026 went green
	# because the probe import sat in tools/zprobe.
	hit=$(printf '%s\n' "$allimports" | grep -v "^$mod/tools: " | grep -F " $m " || true)
	[ -z "$hit" ] || die "tools/modules.go still names $m, but it is imported now - take that line out: $(printf '%s' "$hit" | tr '\n' ' ')"
done


# The PHP side of the web interface checks itself against shared/testdata/ and
# needs nothing but PHP. Skipping it quietly would mean the drift it is written
# to catch never shows in a normal run, and a path to one machine's php has no
# place in a shared script, so php is looked up in four steps:
#   1. NGC_PHP, 2. tools/local.env (not in git, one NGC_PHP=<path> line),
#   3. php from the PATH, 4. stop and say so.
# apps/web/tests/playwright.config.js looks it up in the same four steps for
# the test server; the path itself is written down only in tools/local.env.
named="${NGC_PHP:-}"
where="NGC_PHP"
if [ -z "$named" ] && [ -f tools/local.env ]; then
	. ./tools/local.env
	named="${NGC_PHP:-}"
	where="tools/local.env"
fi
if [ -n "$named" ]; then
	if [ ! -x "$named" ]; then
		echo "$where names a php that cannot be run: $named"
		exit 1
	fi
	php="$named"
elif command -v php >/dev/null 2>&1; then
	php=php
else
	echo "no php: set NGC_PHP to its path, or write NGC_PHP=<path> into tools/local.env"
	exit 1
fi
"$php" apps/web/tests/php/config-test.php
for arch in amd64 arm64; do
	# trusttest holds fixed test keys: only test code may import it. The list
	# is taken as each target is built (cgo off too), so files built only
	# there count as well. Only .Imports here: a TEST importing it is exactly
	# what is allowed.
	imports=$(listed '{{.ImportPath}}: {{join .Imports " "}}' "" CGO_ENABLED=0 GOOS=linux GOARCH="$arch")
	n=$(printf '%s\n' "$imports" | grep -c . || true)
	[ "$n" -ge "$floor" ] || die "go list (linux/$arch) named only $n packages, fewer than the floor of $floor"
	bad=$(printf '%s\n' "$imports" | grep 'shared/trust/trusttest' | grep -v "^$mod/shared/trust/trusttest:" || true)
	if [ -n "$bad" ]; then
		echo "trusttest imported outside tests (linux/$arch):"
		echo "$bad"
		exit 1
	fi
done

go vet ./...
# The race-only test code is counted as covered above, so something has to
# really compile it: -race itself needs cgo and a C compiler (here only in the
# ESP toolchain), vet with the tag does not. Measured 17.10.2026.
go vet -tags=race ./...
go test ./...
for arch in amd64 arm64; do
	# go build compiles no test file, so the tests that only exist on the
	# target (//go:build linux) would never be seen on a machine that builds
	# elsewhere: a typo in them would still say "check: ok". go vet compiles
	# them as the target and is the only thing here that does.
	CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go vet ./...
	# -tags modules builds tools/modules.go too, so the vendored modules that no
	# part imports yet are proven to build for both targets.
	CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -tags modules ./...
done
echo "check: ok"
