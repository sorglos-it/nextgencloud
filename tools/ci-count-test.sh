#!/usr/bin/env bash
# Proves that the CI's test counting does what the CI claims it does.
#
# WHY THIS EXISTS: tools/ci-count.sh is the only thing that turns a test run
# into a number, and nothing checked IT. What that cost is measured, over all
# 182 runs of .github/workflows/linux.yml up to 23.10.2026:
#
#   82 red runs in the "check.sh + counted test run" job
#   42 of those 82 with check.sh itself GREEN - red only at the counting step,
#      on the line "test.json is empty - nothing ran"
#
# The counter was right every time; the file really was unwritten. The writer
# in tools/check.sh was written on 20.10.2026, measured, and then sat on a
# branch until it reached main on 23.10.2026 17:20. For three days the check
# that is supposed to say whether anything ran said only that it could not
# tell, and a check that is always red checks nothing.
#
# So the claims the CI makes are nailed down here, each against a stream a real
# "go test" produced, and each mutation-tested:
#
#   1. a run with red tests          -> red, and the failing test is named
#   2. a run in which nothing ran    -> red
#   3. a green run                   -> green, with the right number
#
# plus the one that actually broke: the workflow and check.sh must still agree
# on the file they pass between them, and the count must ride on the one test
# run instead of adding a second.
#
# Run: bash tools/ci-count-test.sh      (needs go; nothing else)
set -euo pipefail
cd "$(dirname "$0")/.."
repo=$PWD
count=$repo/tools/ci-count.sh

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

fails=0
ok() { printf '  ok   %s\n' "$1"; }
bad() { printf '  BAD  %s\n' "$1"; fails=$((fails + 1)); }

# ---------------------------------------------------------------- fixtures
# Real streams, not hand-written JSON: a stream typed by hand proves only that
# the script reads what the author of the test BELIEVES go test emits, which is
# the same mistake class as the drift above. These come out of go test itself,
# through the same "go tool test2json" that check.sh uses.
fixture() {
	d=$work/mod-$1
	mkdir -p "$d"
	printf 'module ccounttest/%s\n\ngo 1.21\n' "$1" >"$d/go.mod"
	echo "$d"
}

mkstream() { # <module dir> <stream out>; the run's own exit code is not asserted here
	( cd "$1" && go test -v ./... >"$work/raw.log" 2>&1 || true )
	go tool test2json -t <"$work/raw.log" >"$2"
	[ -s "$2" ] || { echo "fixture: the stream for $1 came out empty - the fixture is broken, not the code" >&2; exit 1; }
}

# green: five passing cases
g=$(fixture green)
cat >"$g/a_test.go" <<'GOEOF'
package a

import "testing"

func TestOne(t *testing.T)   {}
func TestTwo(t *testing.T)   {}
func TestThree(t *testing.T) {}
func TestFour(t *testing.T)  {}
func TestFive(t *testing.T)  {}
GOEOF
mkstream "$g" "$work/green.json"

# red: four pass, one falls with a reason of its own
r=$(fixture red)
cat >"$r/a_test.go" <<'GOEOF'
package a

import "testing"

func TestOne(t *testing.T)   {}
func TestTwo(t *testing.T)   {}
func TestThree(t *testing.T) {}
func TestFour(t *testing.T)  {}
func TestTheOneThatBreaks(t *testing.T) {
	t.Fatal("the reason this test failed")
}
GOEOF
mkstream "$r" "$work/red.json"

# nothing ran: a package without test files. This is the shape of a run that
# switched itself off, and what the floor and the "not one case" bolt exist for.
n=$(fixture none)
printf 'package a\n\nfunc Unused() int { return 1 }\n' >"$n/a.go"
mkstream "$n" "$work/none.json"

# The floor is 200 in CI because this repo has thousands of cases; the fixtures
# have five, so it is lowered for them. It is NOT switched off: claim 2 below
# proves the floor and the empty bolt still bite.
export NGC_COUNT_FLOOR=3

# ---------------------------------------------------------------- the claims
# ci-count.sh is called the way the workflow calls it, and its exit code is read
# WITHOUT a pipe: a pipe's exit code belongs to its last link, and a script in
# this repo has already once been dead behind a 0.
run_count() { # <script> <stream> -> writes $work/out, returns its exit code
	local rc=0
	bash "$1" "$2" "test" >"$work/out" 2>&1 || rc=$?
	return $rc
}

claim_green() { # a green run is green AND names the right number
	run_count "$1" "$work/green.json" || return 1
	grep -q 'ci-count: ok (5 cases ran, 0 skipped)' "$work/out" || return 1
	grep -q '5 passed, 0 FAILED, 0 SKIPPED' "$work/out" || return 1
	return 0
}

claim_red() { # a red run is red AND says which case fell
	# The name matters as much as the colour: a red run that does not say what
	# fell cannot be told from a load-sensitive wobble, and that is the one
	# question a red run has to answer.
	if run_count "$1" "$work/red.json"; then return 1; fi
	grep -q 'ci-count: red' "$work/out" || return 1
	grep -q 'TestTheOneThatBreaks' "$work/out" || return 1
	# And the REASON, not only the name. Measured 20.10.2026 in this repo: for
	# weeks a red CI run printed "--- FAIL: <name>" and not one line more, and
	# a wobble and a defect look identical that way.
	grep -q 'the reason this test failed' "$work/out" || return 1
	grep -q '4 passed, 1 FAILED' "$work/out" || return 1
	return 0
}

claim_nothing() { # a stream in which no case ran is red
	if run_count "$1" "$work/none.json"; then return 1; fi
	grep -qE 'not one case in the stream|fewer than the floor' "$work/out" || return 1
	return 0
}

claim_empty() { # an empty file is red, never a count of zero
	: >"$work/empty.json"
	if run_count "$1" "$work/empty.json"; then return 1; fi
	grep -q 'wrote no stream at all' "$work/out" || return 1
	return 0
}

claim_missing() { # a file that was never written says so, and says where to look
	rm -f "$work/gone.json"
	if run_count "$1" "$work/gone.json"; then return 1; fi
	grep -q 'does not exist' "$work/out" || return 1
	# Pointing at the step that really failed is the whole difference to the
	# message that stood for three days.
	grep -q 'check.sh' "$work/out" || return 1
	return 0
}

echo "== the claims, against the script in the tree =="
for c in claim_green claim_red claim_nothing claim_empty claim_missing; do
	if $c "$count"; then ok "$c"; else bad "$c"; sed 's/^/       | /' "$work/out"; fi
done

# ---------------------------------------------------------------- the two halves
# The drift itself: the workflow hands check.sh a path in NGC_CHECK_TEST_JSON and
# then counts that same path. Both halves have to still be there. This is the
# check that did not exist on 18.10.2026, and its absence is what let the two
# files walk apart for three days without anyone being told.
echo
echo "== the two halves still agree =="
wf=$repo/.github/workflows/linux.yml
if grep -q 'NGC_CHECK_TEST_JSON' "$wf"; then ok "the workflow sets NGC_CHECK_TEST_JSON"; else bad "the workflow no longer sets NGC_CHECK_TEST_JSON"; fi
if grep -q 'ci-count.sh' "$wf"; then ok "the workflow counts with ci-count.sh"; else bad "the workflow no longer calls ci-count.sh"; fi
if grep -q 'NGC_CHECK_TEST_JSON' "$repo/tools/check.sh"; then ok "check.sh writes the stream the workflow asks for"; else bad "check.sh does not write NGC_CHECK_TEST_JSON - this is exactly the 18.10.2026 drift"; fi
if grep -q 'test2json' "$repo/tools/check.sh"; then ok "the stream comes from the ONE run, via test2json"; else bad "check.sh no longer builds the stream from its own run"; fi
# A second test run just to count would undo the whole point, so it is counted.
runs=$(grep -cE '^[[:space:]]*(if )?go test ' "$repo/tools/check.sh" || true)
if [ "$runs" = 1 ]; then ok "check.sh runs the suite exactly once"; else bad "check.sh runs 'go test' $runs times - the count rides on the one run, it does not add another"; fi

# ---------------------------------------------------------------- mutants
# messen.md: every claim needs a mutant that MUST die and a control mutation
# that MUST live. If one of them behaves the other way round the run is
# worthless and says so instead of printing a number.
#
# Never the file in the worktree: every mutant is a copy.
echo
echo "== mutants (each must die) and one control (must live) =="

mutate() { # <name> <sed program> -> prints the path of the mutated copy
	local m=$work/mut-$1.sh
	cp "$count" "$m"
	sed -i "$2" "$m"
	# A sed that did not bite leaves the original behind, and a measurement
	# against the original goes green and looks like a result. So the copy has
	# to differ, and it has to still parse: a mutant that does not parse is
	# neither a mutant nor a control.
	if cmp -s "$count" "$m"; then
		echo "mutant $1: the edit changed nothing - the measurement would be worthless" >&2
		exit 1
	fi
	bash -n "$m" || { echo "mutant $1: does not parse" >&2; exit 1; }
	echo "$m"
}

dies() { # <name> <claim fn> <sed program>
	local m
	m=$(mutate "$1" "$3")
	if "$2" "$m"; then bad "mutant $1 SURVIVED $2 - the claim is not held by the code"; else ok "mutant $1 died on $2"; fi
}

# M1 - the rule that a red case makes the run red.
#
# Measured while writing this: the WEAKER mutant, "fail > 0 || pkgbad > 0"
# turned into "pkgbad > 0", SURVIVED - the red fixture is red through the
# failing PACKAGE as well, so two independent rules hold the colour. That is
# not a defect, it is redundancy, and a mutant that one of two rules covers
# proves nothing about either. So both are taken away at once, which is what
# the claim "a red case makes the run red" actually rests on.
dies fail-not-red claim_red 's/if (fail > 0 || pkgbad > 0)/if (0)/'
# M2 - the rule that a failing case is named and quoted, not merely counted.
dies fail-silent claim_red 's/if (fail > 0) {/if (0) {/'
# M3 - the reason alone: the name stays, the test's own output goes. This is
# exactly the state this repo was in until 20.10.2026.
dies fail-reasonless claim_red 's|if (k in failtext \&\& failtext\[k\] != "") printf "%s", failtext\[k\]|;|'
# M4 - the bolt under "nothing ran". This one must never be loosened: a counter
# that goes green on an empty stream is worse than the state it replaces.
dies nothing-passes claim_nothing 's/if (ran + skip == 0)/if (0)/; s/else if (ran < floor)/if (0)/'
# M5 - the bolt under an empty file.
dies empty-passes claim_empty 's|^\[ -s "\$log" \] .*|true|'
# M6 - the bolt under a file that was never written at all.
dies missing-passes claim_missing 's/^if \[ ! -e "\$log" \]; then/if false; then/'
# M7 - the number itself. A green run with a wrong count is the quietest
# failure of all, so the number is asserted and not just the colour.
dies wrong-number claim_green 's/{ pass++;  if (top) toppass++ }/{ pass += 2;  if (top) toppass++ }/'

# The control: a local name inside the awk program, renamed everywhere it
# occurs. It changes nothing the script decides, so it MUST live. If it dies,
# this harness is reacting to the act of editing rather than to the change, and
# every mutant above means nothing.
# The name is "pkgnone", and that it appears only as CODE is the whole reason
# it was picked. Measured while writing this: renaming "ran" instead KILLED the
# control, because "ran" also stands inside the printed line "(%d cases ran)",
# so the rename changed the OUTPUT and the claims grep for that output. A
# control that dies is first a question about the measurement and only then
# about the code - that one was the harness's fault, and the harness was what
# was wrong.
ctl=$(mutate control-rename 's/\bpkgnone\b/pkg_without_tests/g')
ctl_ok=1
for c in claim_green claim_red claim_nothing; do
	$c "$ctl" || ctl_ok=0
done
if [ "$ctl_ok" = 1 ]; then
	ok "control mutation (renamed local) lived on all three claims"
else
	bad "control mutation DIED - this run is worthless, and that is not a finding about the code"
	sed 's/^/       | /' "$work/out"
fi

echo
if [ "$fails" != 0 ]; then
	echo "ci-count-test: $fails failed - see the lines marked BAD above"
	exit 1
fi
# The proof is this printed line, never the exit code alone.
echo "ci-count-test: ok (5 claims, 5 halves, 7 mutants dead, 1 control alive)"
