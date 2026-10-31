#!/usr/bin/env bash
# Checks docs/vertraege-v1.2.md against ITSELF: no number twice, no gap, no
# reference to a point that does not exist.
#
# WHY THIS EXISTS. The book grows by sessions running in parallel appending at
# the end, and twice two sessions handed out the SAME number - the last time two
# different points both carried 72, found by a human while merging and fixed by
# hand. PR 120 then corrected two references that pointed at 72 instead of 73.
# Nothing said a word in between: a duplicate number and a dead reference both
# render perfectly, and a reader sent to the wrong point does not notice either.
# So the file is read by a machine before every commit, not by whoever merges.
#
# WHAT IT DOES NOT DO. Whether a reference points at the RIGHT point in content
# ("corrected by point 54" - does 54 correct anything?) cannot be read from the
# numbers. That stays a human job; this holds the half that is mechanical.
#
# TWO NUMBER SERIES, on purpose (the book says so in its own table of contents):
#   Reihe A - the collection list at the top, 1-35 plus 33b, by contract package.
#   Reihe B - the "## Punkt N" chapters below, 19 upwards, by date.
# They overlap in 19-35, so a bare "Punkt 25" is ambiguous BY DESIGN and the
# book's rule is to quote the heading. A number is therefore counted as alive
# when EITHER series has it - which still catches the error that actually
# happens: a number above the last chapter, which is what a duplicate at the end
# of the file turns into as soon as someone renumbers.
set -euo pipefail
cd "$(dirname "$0")/.."

buch=docs/vertraege-v1.2.md
# A floor, for the same reason check.sh has one: a parse that suddenly finds
# three chapters has failed, and "no duplicates among three" is a green light
# nobody earned. The real count was 55 chapters on 24.10.2026 and only grows.
floor=40
rc=0

die() {
	echo "buch: $*" >&2
	rc=1
}

[ -f "$buch" ] || { echo "buch: $buch is missing" >&2; exit 1; }

# The chapter headings. "## Punkt 29, Nachtrag:" is deliberately NOT one of
# these - an addendum carries the number of the point it continues, and counting
# it as a chapter would report every addendum as a duplicate.
nums=$(sed -n 's/^## Punkt \([0-9][0-9]*\):.*/\1/p' "$buch")
n=$(printf '%s\n' "$nums" | grep -c . || true)
[ "$n" -ge "$floor" ] || { echo "buch: only $n chapters found in $buch, fewer than the floor of $floor - that is a failed read, not a short book" >&2; exit 1; }

# 1. Every number exactly once. THE error this file was written for.
dup=$(printf '%s\n' "$nums" | sort -n | uniq -d | tr '\n' ' ')
[ -z "$dup" ] || die "these point numbers are handed out TWICE: ${dup%% }- two sessions appended at the same time; give the later one the next free number and fix the references to it"

first=$(printf '%s\n' "$nums" | sort -n | head -1)
last=$(printf '%s\n' "$nums" | sort -n | tail -1)

# 2. No gap. A missing number is either a point that was deleted (the book is a
# log; nothing is deleted) or a number someone skipped, and the next session
# will reuse it.
missing=
for i in $(seq "$first" "$last"); do
	printf '%s\n' "$nums" | grep -qx "$i" || missing="$missing $i"
done
[ -z "$missing" ] || die "no chapter carries these numbers, although they lie between $first and $last:$missing - a log has no holes; if one is meant, say so in the chapter that explains it"

# 3. An addendum continues a point that exists.
for a in $(sed -n 's/^## Punkt \([0-9][0-9]*\), Nachtrag.*/\1/p' "$buch"); do
	printf '%s\n' "$nums" | grep -qx "$a" || die "the addendum '## Punkt $a, Nachtrag' continues a point $a that no chapter carries"
done

# 4. The table of contents lists every chapter, and no chapter it does not have.
# This is the early warning: a session that appends a chapter without a row is
# the exact situation in which two sessions pick the same next number, because
# the table is where the next number is read off.
# Only "N" and the addendum form "Nn" are rows. The suffix was "[a-z]\{0,1\}"
# until it was measured on 24.10.2026: a row written "| 72x |" was then still
# read as row 72, so a mutant that renamed the row lived. An unknown suffix is
# not a row for point N.
toc=$(sed -n '/^### Reihe B /,/^### /p' "$buch" | sed -n 's/^| \([0-9][0-9]*\)n\{0,1\} |.*/\1/p' | sort -nu)
nosort=$(printf '%s\n' "$nums" | sort -nu)
only_text=$(comm -23 <(printf '%s\n' "$nosort") <(printf '%s\n' "$toc") | tr '\n' ' ')
only_toc=$(comm -13 <(printf '%s\n' "$nosort") <(printf '%s\n' "$toc") | tr '\n' ' ')
[ -z "$only_text" ] || die "these points have a chapter but no row in the Reihe-B table of contents: ${only_text%% }- the table is where the next free number is read off, so a missing row is how the next collision starts"
[ -z "$only_toc" ] || die "the Reihe-B table of contents lists points with no chapter in the file: ${only_toc%% }"

# 5. Dead references, in EVERY tracked markdown file of the repo, not only in the
# book: thomas-entscheidet.md, messungen-m1-m3.md and CHANGELOG.md all point into
# it, and a reader sent from there to a point that does not exist is just as lost.
# Alive = Reihe A (1-35, and 33b is written out so it is never read as 33) or
# Reihe B (19..last). So the test is: 1 <= number <= last.
#
# What is read as a reference: "Punkt N" / "Punkte N", and the numbers that hang
# off it in a list - "Punkt 52 und 53", "Punkt 57/59/61", "Punkte 63-71". Those
# trailing numbers are where the 72/73 error sat twice, so they are not skipped.
mdfiles=$(git -c safe.directory="$PWD" ls-files '*.md')
nmd=$(printf '%s\n' "$mdfiles" | grep -c . || true)
# Same floor thought as above: an empty file list is a failed listing, and it
# would report "no dead reference" about nothing at all.
[ "$nmd" -ge 10 ] || { echo "buch: git ls-files named only $nmd markdown files - that is a failed listing" >&2; exit 1; }

dead=$(mktemp)
trap 'rm -f "$dead"' EXIT
# shellcheck disable=SC2086
awk -v last="$last" '
	{
		line = $0
		while (match(line, /Punkte? [0-9]+([ ]*(,|und|bis|\/|-|\xe2\x80\x93)[ ]*[0-9]+)*/)) {
			s = substr(line, RSTART, RLENGTH)
			line = substr(line, RSTART + RLENGTH)
			n = split(s, parts, /[^0-9]+/)
			for (i = 1; i <= n; i++) {
				# Four digits is a year, a byte count or a line number that
				# happened to follow a comma - never a point number.
				if (parts[i] == "" || length(parts[i]) > 3) continue
				v = parts[i] + 0
				if (v < 1 || v > last)
					printf "%s:%d: points at point %d, and the book ends at %d -- %s\n", FILENAME, FNR, v, last, substr($0, 1, 110)
			}
		}
	}
' $mdfiles >"$dead"
if [ -s "$dead" ]; then
	echo "buch: dead references - they send the reader to a point that does not exist:" >&2
	sed 's/^/    /' "$dead" >&2
	rc=1
fi

[ "$rc" = 0 ] || { echo "buch: NOT ok" >&2; exit 1; }
# The printed line is the proof, not the exit code - the rule tools/check.sh
# holds itself to.
echo "buch: ok ($n points, $first-$last, no duplicate, no gap, no dead reference)"
