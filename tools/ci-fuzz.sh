#!/usr/bin/env bash
# Runs the fuzz targets of this repo for a few seconds each - as a FALLBACK
# CHECK, not as a search.
#
# What a fuzz number is worth is measured in Z:\GitHub\claude-md\messen.md: the
# same code did between 15 and 637474 runs per 30 seconds in the same hour, so
# the number says more about the machine than about the code. That is why this
# script does not report a number of execs as a result. What it reports is: how
# many targets were fuzzed, and whether one of them fell over. A target that
# falls over writes its input under testdata/corpus, and the log says so.
#
# -fuzzminimizetime 0: a found input otherwise spends up to 60 s being minimized
# and eats the whole budget of the shard.
set -euo pipefail
cd "$(dirname "$0")/.."

fuzztime=${FUZZTIME:-5}
shard=${SHARD:-0}
shards=${SHARDS:-4}
export GOFLAGS="${GOFLAGS:+$GOFLAGS }-buildvcs=false"

git() { command git -c safe.directory="$PWD" "$@"; }

# The list of targets is read from the tracked files, so a tree that lost files
# cannot silently shrink the list.
files=$(git ls-files '*_test.go' ':!:vendor/*')
nfiles=$(printf '%s\n' "$files" | grep -c . || true)
[ "$nfiles" -ge 50 ] || { echo "ci-fuzz: git ls-files named only $nfiles test files - that is a failed listing" >&2; exit 1; }

# "<package dir> <FuzzName>", sorted so the split into shards is the same on
# every run and every shard covers the same targets each time.
targets=$(printf '%s\n' "$files" | while read -r f; do
	[ -n "$f" ] || continue
	sed -n 's/^func \(Fuzz[A-Za-z0-9_]*\)(.*/\1/p' "$f" | while read -r name; do
		echo "$(dirname "$f") $name"
	done
done | sort -u)

total=$(printf '%s\n' "$targets" | grep -c . || true)
# Measured on main 18.10.2026: about 90 targets. A list far below that is a
# broken list, not a repo that lost its fuzzing.
[ "$total" -ge 40 ] || { echo "ci-fuzz: found only $total fuzz targets - the list failed" >&2; exit 1; }

echo "ci-fuzz: $total targets, shard $shard of $shards, ${fuzztime}s each"

i=0
ran=0
bad=0
while read -r dir name; do
	[ -n "${name:-}" ] || continue
	if [ $((i % shards)) -eq "$shard" ]; then
		echo "--- $dir $name"
		if [ "$fuzztime" = "0" ]; then
			# Seeds only: the corpus under testdata/fuzz plus every f.Add.
			out=$(go test -count=1 -run "^${name}\$" "./$dir" 2>&1) || bad=$((bad + 1))
		else
			out=$(go test -count=1 -run '^$' -fuzz "^${name}\$" \
				-fuzztime "${fuzztime}s" -fuzzminimizetime 0 "./$dir" 2>&1) || bad=$((bad + 1))
		fi
		printf '%s\n' "$out" | tail -5
		# A target that finds something writes the input into the tree. Naming
		# it here is the whole point of the run.
		printf '%s\n' "$out" | grep -i 'testdata[/\\]corpus' || true
		ran=$((ran + 1))
	fi
	i=$((i + 1))
done <<EOF
$targets
EOF

echo "ci-fuzz: $ran targets fuzzed in this shard, $bad red"
# A shard that fuzzed nothing is not a green shard.
[ "$ran" -gt 0 ] || { echo "ci-fuzz: not one target ran in this shard" >&2; exit 1; }
[ "$bad" -eq 0 ] || exit 1
echo "ci-fuzz: ok"
