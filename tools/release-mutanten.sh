#!/bin/sh
# Mutation run for tools/build-release.sh: break one thing at a time and show
# that the build notices.
#
# Why it earns a place: build-release.sh is the script that stamps the version
# into the programs, and wall 2 of the six was exactly a build WITHOUT
# -ldflags, where every program reported 0.0.0-dev and the interface threw the
# whole answer away (point 71). A green build proves nothing unless a broken
# one goes red, and that is what this measures - with living controls beside
# the dying mutants, so a red run cannot be red for the wrong reason.
#
# Rescued on 24.10.2026 off the first single machine, where it lived in
# ~/relbuild/mutanten.sh and was in no repository.
#
#   sh tools/release-mutanten.sh [ergebnisverzeichnis] [version]
#
# Runs ON a Linux machine with go: it really builds, seven times plus five
# input cases, so it takes a while. The results directory defaults to
# dist/release-mutanten and is emptied at the start.
#
# Never bends the file in the working tree: every mutant is a copy under
# tools/mut-<n>.sh, so $self stays tools/ and the repository root stays the
# root. They are removed again - now also when the run dies in the middle,
# which the original did not do.
set -u

self_dir=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$self_dir/.." && pwd)
res=${1:-$repo/dist/release-mutanten}
version=${2:-0.2.0}

orig=$repo/tools/build-release.sh
[ -f "$orig" ] || { echo "nicht gefunden: $orig" >&2; exit 2; }
command -v go >/dev/null || { echo "kein go - dieser Lauf baut wirklich" >&2; exit 2; }

# A leftover mut-*.sh from a killed run would be built as if it were a tool of
# the repository. Refuse instead of overwriting blindly.
if ls "$repo"/tools/mut-*.sh >/dev/null 2>&1; then
	echo "ABBRUCH: es liegen noch tools/mut-*.sh herum - erst wegraeumen:" >&2
	ls "$repo"/tools/mut-*.sh >&2
	exit 2
fi
aufraeumen() { rm -f -- "$repo"/tools/mut-*.sh; }
trap 'aufraeumen' EXIT HUP INT TERM

rm -rf -- "$res"
mkdir -p -- "$res"
out_base=$res/ausgabe

# Leak check FIRST: a run that does not report 7 as 7 makes every number below
# it worthless.
(exit 7)
probe=$?
echo "SONDE exit7 = $probe" | tee "$res/00-sonde.txt"
[ "$probe" = 7 ] || { echo "ABBRUCH: die Sonde meldet $probe statt 7"; exit 1; }

rot=0

# run <name> <skript> <erwartung gruen|rot> <version...>
run() {
	r_name=$1
	r_script=$2
	r_want=$3
	shift 3
	rc=0
	(cd "$repo" && sh "$r_script" "$@" "$out_base-$r_name") >"$res/$r_name.log" 2>&1 || rc=$?
	echo "$rc" >"$res/$r_name.rc"
	# The expectation is checked here, not left to a reader of the log. The
	# original printed the exit code and said nothing about what it should be,
	# so a mutant that stopped dying looked like a normal line.
	if [ "$r_want" = gruen ] && [ "$rc" != 0 ]; then
		echo "== $r_name  AUSSTIEGSCODE=$rc  ERWARTET GRUEN - ROT"
		rot=1
	elif [ "$r_want" = rot ] && [ "$rc" = 0 ]; then
		echo "== $r_name  AUSSTIEGSCODE=0  ERWARTET ROT - der Mutant LEBT"
		rot=1
	else
		echo "== $r_name  AUSSTIEGSCODE=$rc  wie erwartet ($r_want)"
	fi
	sed -n '1,6p' "$res/$r_name.log" | sed 's/^/   /'
}

# mut <n> <sed-ausdruck> <beleg-muster>: a mutation that does not take hold
# gives a green run that means nothing, so both are checked.
mut() {
	m_n=$1
	m_sed=$2
	m_want=$3
	m_f=$repo/tools/mut-$m_n.sh
	sed "$m_sed" "$orig" >"$m_f"
	if ! grep -q -- "$m_want" "$m_f"; then
		echo "ABBRUCH: Mutation $m_n hat nicht gegriffen (Muster fehlt: $m_want)"
		exit 1
	fi
	if cmp -s "$m_f" "$orig"; then
		echo "ABBRUCH: Mutation $m_n hat die Datei nicht veraendert"
		exit 1
	fi
	echo "$m_f"
}

echo
echo "### K0  Kontrolle: der unveraenderte Stand muss GRUEN sein"
run K0 "$orig" gruen "$version"

# And if the control is red, the run stops HERE.
#
# Measured on 24.10.2026, and this bolt exists because of it: on a machine
# without qemu-user-static, build-release.sh refuses at its very first step,
# for every case alike. K0, L1 and L2 then go red and are reported as such -
# but M1 to M4 come back "rot, wie erwartet" and look like four dying mutants,
# while not one of them ever reached its own mutation. A mutant has to die of
# its own cause; red for the same reason as the control measures nothing and
# reads like proof.
if [ "$rot" != 0 ]; then
	echo
	echo "ABBRUCH: die Kontrolle K0 ist rot - der unveraenderte Stand baut hier nicht."
	echo "Jeder Mutant waere aus demselben Grund rot, und das beweist nichts."
	echo "Erst den Grund oben beheben (haeufig: sudo apt-get install qemu-user-static)."
	exit 1
fi

echo
echo "### L1  LEBENDE Kontrollmutation: eine lokale Variable umbenannt"
# cs_file -> cs_pfad in check_target, throughout. The bolt is the same, so the
# run must stay green. A control that does not parse or does not run is no
# control.
f=$(mut L1 's/cs_file/cs_pfad/g' 'cs_pfad=\$1') || exit 1
run L1 "$f" gruen "$version"

echo
echo "### M1  STERBENDER Mutant: go build OHNE -ldflags (der Fall von v0.2.0)"
f=$(mut M1 's/go build -trimpath -ldflags "\$ldflags"/go build -trimpath/' 'go build -trimpath -o') || exit 1
run M1 "$f" rot "$version"

echo
echo "### M2  STERBENDER Mutant: ohne -ldflags UND ohne check_target"
# Shows that the -version bolt kills on its own - not the buildinfo check.
f=$(mut M2 's/go build -trimpath -ldflags "\$ldflags"/go build -trimpath/; s/^\t\tcheck_target /\t\t: check_target /' ': check_target ') || exit 1
run M2 "$f" rot "$version"

echo
echo "### L2  LEBENDE Kontrolle: check_target weg, -ldflags DA"
# Counter-check to M2: without the buildinfo bolt the run stays green as long
# as it is stamped. That is what proves M2 does not die at check_target.
f=$(mut L2 's/^\t\tcheck_target /\t\t: check_target /' ': check_target ') || exit 1
run L2 "$f" gruen "$version"

echo
echo "### M3  STERBENDER Mutant: pack() bekommt zusaetzlich den Punkt"
# Makes the entry "./" a hand-built release hung on on 18.10.2026. Has to fall
# at the shape check or at ngc-store contents.
f=$(mut M3 's/pack "\$out\/ngc-controller-\$arch.tar" "\$bindir" \$controller_progs/pack "$out\/ngc-controller-$arch.tar" "$bindir" . $controller_progs/' 'bindir" \. \$controller_progs') || exit 1
run M3 "$f" rot "$version"

echo
echo "### M4  STERBENDER Mutant: kein Laeufer fuer arm64 (qemu fehlt)"
f=$(mut M4 's/^\tarm64) command -v qemu-aarch64-static.*$/\tarm64) return 1 ;;/' 'arm64) return 1 ;;') || exit 1
run M4 "$f" rot "$version"

echo
echo "### Eingabefaelle am unveraenderten Stand (Versions-Riegel)"
# Every one of these is malformed, so every one has to be refused. The
# original only printed the code; here a 0 is a finding.
for v in 0.2.0-confirmfix 0.2.0.1 01.2.0 0.2 0.2.0.; do
	rc=0
	(cd "$repo" && sh "$orig" "$v" "$out_base-v") >"$res/v-$v.log" 2>&1 || rc=$?
	echo "$rc" >"$res/v-$v.rc"
	if [ "$rc" = 0 ]; then
		echo "== version \"$v\"  AUSSTIEGSCODE=0 - angenommen, obwohl sie abgewiesen werden muss"
		rot=1
	else
		echo "== version \"$v\"  AUSSTIEGSCODE=$rc  abgewiesen, wie es sein muss"
	fi
	sed -n '1p' "$res/v-$v.log" | cut -c1-90 | sed 's/^/   /'
done

echo
[ "$rot" = 0 ] || { echo "mutanten: ROT - siehe $res"; exit 1; }
echo "mutanten: ok ($res)"
