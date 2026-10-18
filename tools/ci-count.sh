#!/usr/bin/env bash
# Counts what a test run really did, from the JSON stream of "go test -json".
#
# WHY THIS EXISTS: "ok" says nothing about the number of cases that ran.
# Measured 12.10.2026 in this repo: go test printed "ok" while 10 of 11 cases
# were SKIP, because a fixture did not build and t.Skipf prints its line only
# under -v. A run that quietly switches itself off looks green. So every run
# that is reported has to name three numbers - passed, FAILED, SKIPPED - and a
# skipped case is not a case that ran.
#
# The script is held to the rule it enforces: it refuses to print a number it
# cannot stand behind (empty file, no case at all, fewer cases than the floor)
# and says so instead.
set -euo pipefail

log=${1:-}
title=${2:-run}
# The floor is far below the real count (over 1000 cases on main) and far above
# zero: a stream with a handful of cases is a failed run, not a small repo.
floor=${NGC_COUNT_FLOOR:-200}

[ -n "$log" ] || { echo "ci-count: usage: ci-count.sh <go test -json file> [title]" >&2; exit 2; }
[ -s "$log" ] || { echo "ci-count: $log is empty - nothing ran, and that is not a result" >&2; exit 2; }

awk -v title="$title" -v floor="$floor" '
	function name() {
		if (match($0, /"Test":"[^"]*"/)) return substr($0, RSTART + 8, RLENGTH - 9)
		return ""
	}
	function pkg() {
		if (match($0, /"Package":"[^"]*"/)) return substr($0, RSTART + 11, RLENGTH - 12)
		return ""
	}
	{
		t = name(); p = pkg()
		top = (index(t, "/") == 0)
		if ($0 ~ /"Action":"pass"/)  { if (t == "") { pkgok++ }   else { pass++;  if (top) toppass++ } }
		if ($0 ~ /"Action":"fail"/)  { if (t == "") { pkgbad++; badpkg[p] = 1 } else { fail++; failed[t "  (" p ")"] = 1 } }
		if ($0 ~ /"Action":"skip"/)  { if (t == "") { pkgnone++ } else { skip++; if (top) topskip++; skipped[t "  (" p ")"] = 1 } }
	}
	END {
		ran = pass + fail
		printf "\n== %s ==\n", title
		printf "cases:    %d passed, %d FAILED, %d SKIPPED   (of these, top level: %d passed, %d skipped)\n", \
			pass, fail, skip, toppass, topskip
		printf "packages: %d ok, %d failed, %d without test files\n", pkgok, pkgbad, pkgnone

		if (fail > 0) { print "\nFAILED:"; for (k in failed) print "  " k }
		if (pkgbad > 0) { print "\nfailed packages (a build error lands here, with no case of its own):"; for (k in badpkg) print "  " k }
		if (skip > 0) {
			print "\nSKIPPED - a skipped case is not a case that ran:"
			for (k in skipped) print "  " k
		}

		bad = 0
		if (ran + skip == 0) { print "\nci-count: not one case in the stream - the run did not happen"; bad = 1 }
		else if (ran < floor) { printf "\nci-count: only %d cases ran, fewer than the floor of %d - that is a failed run, not a small repo\n", ran, floor; bad = 1 }
		if (fail > 0 || pkgbad > 0) { print "\nci-count: red"; bad = 1 }
		if (bad) exit 1
		# The proof is this printed line, never the exit code alone.
		printf "\nci-count: ok (%d cases ran, %d skipped)\n", ran, skip
	}
' "$log"
