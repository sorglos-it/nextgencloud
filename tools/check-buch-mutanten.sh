#!/usr/bin/env bash
# Proves that tools/check-buch.sh measures what it claims to measure.
#
# Per assertion a mutant that must DIE (the check goes red) AND a control
# mutation that must LIVE: a change of the same shape, in the same line, that is
# NOT the error. A check that is red for everything is as useless as one that is
# green for everything, and the control is what tells the two apart
# (Z:\GitHub\claude-md\messen.md).
#
# It mutates a COPY and never the working tree, and it stops if a replacement
# does not hit exactly once - a mutant that was never planted "dies" for free.
#
# Run:  bash tools/check-buch-mutanten.sh . <scratch dir outside the repo>
# Measured 24.10.2026: 15 of 15 - 7 mutants dead, 7 controls alive, healthy tree
# green. One case was WRONG the first time and is kept as a warning: the mutant
# for "chapter without a row in the table" only renamed the row to "72x", which
# the parser still read as row 72, so it lived. The parser was tightened and the
# mutant now deletes the row.
set -uo pipefail
src=$1
work=$2
rm -rf "$work"; mkdir -p "$work"
pass=0; fail=0

# The base is built ONCE: only the markdown files and the script are needed,
# and "git ls-files" has to work inside the copy, so it is a git tree.
base="$work/_base"
mkdir -p "$base/tools"
( cd "$src" && git -c safe.directory="$src" ls-files '*.md' ) | while read -r f; do
	mkdir -p "$base/$(dirname "$f")"; cp "$src/$f" "$base/$f"
done
cp "$src/tools/check-buch.sh" "$base/tools/check-buch.sh"
( cd "$base" && git init -q . && git add -A >/dev/null && git -c user.email=m@m -c user.name=m commit -qm x ) >/dev/null 2>&1

prep() { # $1 = name -> prints the copy dir
	local d="$work/$1"
	rm -rf "$d"
	cp -r "$base" "$d"
	echo "$d"
}

sub() { # $1 = file  $2 = awk-free literal old  $3 = new  -- must hit exactly once
	local f=$1 old=$2 new=$3
	local n
	n=$(grep -cF -- "$old" "$f")
	if [ "$n" != 1 ]; then echo "ABBRUCH: '$old' kommt ${n}x in $f vor, nicht genau einmal" >&2; exit 2; fi
	python -c "
import sys,io
p,o,nw=sys.argv[1],sys.argv[2],sys.argv[3]
s=io.open(p,encoding='utf-8').read()
assert s.count(o)==1
io.open(p,'w',encoding='utf-8',newline='').write(s.replace(o,nw))
" "$f" "$old" "$new"
}

run() { # $1 = dir ; prints rot/gruen
	if ( cd "$1" && bash tools/check-buch.sh >/tmp/_bo 2>&1 ); then echo gruen; else echo rot; fi
}

judge() { # $1 = label  $2 = erwartet  $3 = ist
	if [ "$2" = "$3" ]; then echo "  OK   $1: $3 (erwartet $2)"; pass=$((pass+1));
	else echo "  FEHL $1: $3 (erwartet $2)"; fail=$((fail+1)); fi
}

echo "== A0: der gesunde Baum"
d=$(prep gesund); judge "unveraendert" gruen "$(run "$d")"

echo "== A1: doppelte Nummer"
d=$(prep dup)
sub "$d/docs/vertraege-v1.2.md" "## Punkt 73: Ein zweiter Mensch" "## Punkt 72: Ein zweiter Mensch"
judge "Mutant (73 noch einmal als 72)" rot "$(run "$d")"
d=$(prep dup_kontrolle)
sub "$d/docs/vertraege-v1.2.md" "## Punkt 73: Ein zweiter Mensch" "## Punkt 73: Ein zweiter Nutzer"
judge "Kontrolle (dieselbe Zeile, nur der Text)" gruen "$(run "$d")"

echo "== A2: toter Verweis"
d=$(prep tot)
sub "$d/docs/vertraege-v1.2.md" "Punkt 36 hat die Hälfte offen gelassen" "Punkt 86 hat die Hälfte offen gelassen"
judge "Mutant (Verweis auf 86)" rot "$(run "$d")"
d=$(prep tot_kontrolle)
sub "$d/docs/vertraege-v1.2.md" "Punkt 36 hat die Hälfte offen gelassen" "Punkt 51 hat die Hälfte offen gelassen"
judge "Kontrolle (Verweis auf 51, gibt es)" gruen "$(run "$d")"

echo "== A2b: toter Verweis in einer ANDEREN Datei"
d=$(prep tot_fremd)
sub "$d/CHANGELOG.md" "sechs Wände aus Punkt 71" "sechs Wände aus Punkt 171"
judge "Mutant (CHANGELOG zeigt auf 171)" rot "$(run "$d")"
d=$(prep tot_fremd_kontrolle)
sub "$d/CHANGELOG.md" "sechs Wände aus Punkt 71" "sechs Wände aus Punkt 64"
judge "Kontrolle (CHANGELOG zeigt auf 64)" gruen "$(run "$d")"

echo "== A2c: der Verweis am Ende einer Aufzaehlung (die Stelle aus PR 120)"
d=$(prep tot_liste)
sub "$d/docs/vertraege-v1.2.md" "**behoben durch Punkt 52 und 53**" "**behoben durch Punkt 52 und 530**"
judge "Mutant (zweite Zahl der Liste tot)" rot "$(run "$d")"
d=$(prep tot_liste_kontrolle)
sub "$d/docs/vertraege-v1.2.md" "**behoben durch Punkt 52 und 53**" "**behoben durch Punkt 52 und 54**"
judge "Kontrolle (zweite Zahl lebt)" gruen "$(run "$d")"

echo "== A3: Luecke in der Reihe"
d=$(prep luecke)
sub "$d/docs/vertraege-v1.2.md" "## Punkt 70: Ein Paket über eine Fenstergröße" "## Punkt 170: Ein Paket über eine Fenstergröße"
judge "Mutant (70 fehlt)" rot "$(run "$d")"
d=$(prep luecke_kontrolle)
sub "$d/docs/vertraege-v1.2.md" "## Punkt 70: Ein Paket über eine Fenstergröße" "## Punkt 70: Ein Pakete über eine Fenstergröße"
judge "Kontrolle (Nummer bleibt)" gruen "$(run "$d")"

echo "== A4: Kapitel ohne Zeile im Inhaltsverzeichnis"
d=$(prep toc)
python -c "
import sys,io
p=sys.argv[1]
s=io.open(p,encoding='utf-8').read().split('\n')
k=[i for i,l in enumerate(s) if l.startswith('| 72 | Härtung des App-Containers')]
assert len(k)==1, k
del s[k[0]]
io.open(p,'w',encoding='utf-8',newline='').write('\n'.join(s))
" "$d/docs/vertraege-v1.2.md"
judge "Mutant (Zeile 72 ganz geloescht)" rot "$(run "$d")"
d=$(prep toc_kontrolle)
sub "$d/docs/vertraege-v1.2.md" "| 72 | Härtung des App-Containers" "| 72 | Haertung des App-Containers"
judge "Kontrolle (Zeile bleibt, Text anders)" gruen "$(run "$d")"

echo "== A5: Nachtrag auf einen Punkt, den es nicht gibt"
d=$(prep nachtrag)
sub "$d/docs/vertraege-v1.2.md" "## Punkt 29, Nachtrag: die Zahl" "## Punkt 129, Nachtrag: die Zahl"
judge "Mutant (Nachtrag zu 129)" rot "$(run "$d")"
d=$(prep nachtrag_kontrolle)
sub "$d/docs/vertraege-v1.2.md" "## Punkt 29, Nachtrag: die Zahl" "## Punkt 29, Nachtrag: die Anzahl"
judge "Kontrolle (Nachtrag bleibt bei 29)" gruen "$(run "$d")"

echo
echo "bestanden: $pass   durchgefallen: $fail"
# An explicit exit: the first version of this driver ended on a test command and
# still reported 0 while one case had failed - a green light nobody earned.
if [ "$fail" = 0 ]; then exit 0; else exit 1; fi
