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
#
# AND IT PRINTS WHY A CASE FAILED, not only its name. Until 20.10.2026 it
# named the test and nothing else, and the stream it reads was the only place
# the reason existed - the -race and root jobs pipe go test -json into a file
# that no step ever shows. A red run that does not say why cannot be told
# apart from a load-sensitive wobble, and that is the one question a red run
# has to answer. The output records of a failing test are kept and printed
# under its name.
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
	# unesc turns the JSON string of an Output record back into text. Only
	# the four escapes go test really emits are handled; anything else stays
	# as it stands, which is honest about what was not decoded.
	function unesc(v) {
		gsub(/\\n/, "\n", v)
		gsub(/\\t/, "\t", v)
		gsub(/\\"/, "\"", v)
		gsub(/\\\\/, "\\", v)
		return v
	}
	function outtext() {
		i = index($0, "\"Output\":\"")
		if (i == 0) return ""
		v = substr($0, i + 10)
		# awk has no non-greedy match: /"Output":".*"/ ate the member AFTER
		# the value and printed ","OutputType":"frame into the text, measured
		# 20.10.2026. So the end is found by hand - either the next member or
		# the end of the object.
		j = index(v, "\",\"")
		if (j > 0) v = substr(v, 1, j - 1)
		else sub(/"}[[:space:]]*$/, "", v)
		return unesc(v)
	}
	{
		t = name(); p = pkg()
		top = (index(t, "/") == 0)
		# Kept for every case, because which one fails is known only at the
		# end. One run of this repo is a few MB of text; that is the price of
		# being able to say why.
		if ($0 ~ /"Action":"output"/ && t != "") said[t] = said[t] outtext()
		if ($0 ~ /"Action":"pass"/)  { if (t == "") { pkgok++ }   else { pass++;  if (top) toppass++ } }
		if ($0 ~ /"Action":"fail"/)  { if (t == "") { pkgbad++; badpkg[p] = 1 } else { fail++; k = t "  (" p ")"; failed[k] = 1; failtext[k] = said[t] } }
		if ($0 ~ /"Action":"skip"/)  { if (t == "") { pkgnone++ } else { skip++; if (top) topskip++; skipped[t "  (" p ")"] = 1 } }
	}
	END {
		ran = pass + fail
		printf "\n== %s ==\n", title
		printf "cases:    %d passed, %d FAILED, %d SKIPPED   (of these, top level: %d passed, %d skipped)\n", \
			pass, fail, skip, toppass, topskip
		printf "packages: %d ok, %d failed, %d without test files\n", pkgok, pkgbad, pkgnone

		if (fail > 0) {
			print "\nFAILED - with what the test itself said:"
			for (k in failed) {
				print "  " k
				if (k in failtext && failtext[k] != "") printf "%s", failtext[k]
			}
		}
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
