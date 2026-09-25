#!/usr/bin/env bash
# The test run for work package 0: every case the package has so far, and it
# grows with each task. The cases that must fail will run here too.
# It does not set a machine up - tools/recovery-setup.sh install and
# tools/recovery-tools.sh have to have been through first.
# Careful: it empties dist/recovery/work. Nothing may be parked there.
# What a finished run leaves standing, so nobody has to guess: the simulator of
# instance main keeps running, 0x81000001 stays persistent inside it, and
# dist/recovery/work still holds the files of the last case - the sealed object,
# the PCR value files and the contexts. The unsealed secret is NOT among them,
# and that is not a hope: the case right before the first branch searches the
# whole directory for it. Measured why that matters - with shred -u turned
# into a no-op the run stayed green nine times over while work/secret.bin lay
# there with the secret in it, and the promise in this very paragraph is what
# made it lie there for good. In work package 6 that file is the real DEK.
# Only the virtual machine and the simulator of instance qemu are ended, by
# cleanup below. Ending main by hand: tools/recovery-swtpm.sh stop main.
set -euo pipefail
shopt -s inherit_errexit
cd "$(dirname "$0")/.."

if [ "$#" -ne 0 ]; then
	printf 'Aufruf: tools/recovery-test.sh (ohne weitere Angaben)\n' >&2
	exit 1
fi

# Whatever the caller had set is not ours and is dropped here, before the trap
# exists: the run addresses the simulator through use_main and nothing else.
unset TPM2TOOLS_TCTI

fail() { printf 'FEHLGESCHLAGEN: %s\n' "$1" >&2; exit 1; }
ok() { printf 'ok: %s\n' "$1" >&2; }
use_main() {
	TPM2TOOLS_TCTI="$(tools/recovery-swtpm.sh tcti main)"
	export TPM2TOOLS_TCTI
	# Kept so cleanup can tell our simulator from anything else that might be
	# in this variable by then.
	tcti_main="$TPM2TOOLS_TCTI"
}

cleanup() {
	# Only ever against OUR simulator, and "is it set" is not that question -
	# measured, a caller who had exported TPM2TOOLS_TCTI=device:/dev/tpmrm0 got
	# three tpm2_flushcontext out of this, aimed at that device, and the
	# redirection below hid them completely.
	if [ -n "${tcti_main:-}" ] && [ "${TPM2TOOLS_TCTI:-}" = "$tcti_main" ]; then
		# A case that failed on purpose leaves its session and its objects
		# behind, and the simulator holds only a handful of either. There is
		# nothing to clean yet - the cases that must fail come later.
		for h in -t -l -s; do tpm2_flushcontext "$h" >/dev/null 2>&1 || :; done
	fi
	# The qemu simulator and the virtual machine are both started in the middle
	# of the run; a case failing after that must not leave either standing.
	# Asked unconditionally, NOT only when a flag says so: the flag says what
	# this run meant to start, and what is actually running is a different
	# question - a run that ended between the two would answer it wrongly. Both
	# calls are harmless when there is nothing to end. Their reason and their
	# outcome stay visible: a tidy-up that fails in silence leaves a process
	# behind and nobody learns of it.
	if ! tools/recovery-qemu.sh stop; then
		printf 'Achtung: die virtuelle Maschine läuft noch. Beenden mit: tools/recovery-qemu.sh stop\n' >&2
	fi
	if ! tools/recovery-swtpm.sh stop qemu; then
		printf 'Achtung: der Simulator der Instanz qemu läuft noch. Beenden mit: tools/recovery-swtpm.sh stop qemu\n' >&2
	fi
}
trap cleanup EXIT

# Answers whether the TPM holds that NV index: 0 yes, 1 no. A TPM that says
# nothing at all is a third answer and ends the run right here - measured,
# tpm2_nvreadpublic answers a TPM it cannot reach with exit 1 and an empty
# list, and reading that as "the index is gone" is how a reset test passes
# without a reset ever happening. Never call this from inside a $( ): the exit
# in fail would end only the subshell.
nv_has() {               # $1 = NV-Index, z. B. 0x1500016
	local list
	if ! list="$(tpm2_nvreadpublic)"; then
		fail "Das TPM antwortet nicht auf die Frage nach seinen NV-Indizes"
	fi
	case "$list" in
	*"$1"*) return 0 ;;
	esac
	return 1
}

# Fishes one field out of a tpm2_readpublic output: the "raw:" line that comes
# right after the field's own name. The line on its own is not enough - the
# same output holds a "raw:" for name-alg, attributes, type, curve-id and more,
# all spelled the same way. Says nothing itself and returns 1 when the field is
# not there at all, so the call site decides what that means. Safe inside a
# $( ): it ends in return, never in fail.
rohwert() {              # $1 = Feldname, $2 = Ausgabe von tpm2_readpublic
	local zeile treffer=""
	while IFS= read -r zeile; do
		case "$zeile" in
		"$1:") treffer=ja ;;
		"  raw: "*)
			if [ "$treffer" = ja ]; then printf '%s' "${zeile#  raw: }"; return 0; fi
			;;
		esac
	done <<< "$2"
	return 1
}

# A syntax error in any script must show up here, not three cases later.
for s in tools/recovery-*.sh; do bash -n "$s"; done

tools/recovery-env.sh
tools/recovery-setup.sh prepare
tools/recovery-swtpm.sh reset main
use_main
tools/recovery-env.sh pcr-bank
# Names no cause: measured, a working directory that was never recreated dies
# on this very line, and the TPM is fine then - tpm2_getrandom says why above.
tpm2_getrandom 8 -o dist/recovery/work/rnd.bin \
	|| fail "dist/recovery/work/rnd.bin ist nicht entstanden. Die Zeilen darüber sagen, woran es lag."
ok "TPM antwortet, SHA-384-Bank vorhanden"

# reset has to really throw the state away, otherwise the second run of this
# test starts on the leftovers of the first. The NV counter is the proof.
tpm2_nvdefine -C o 0x1500016 -a "nt=counter|ownerwrite|ownerread|authread" >/dev/null \
	|| fail "Der Test kann keinen NV-Zähler anlegen"
tpm2_nvincrement -C o 0x1500016 >/dev/null || fail "Der Test kann den NV-Zähler nicht hochzählen"
# Asked before the reset as well: if the counter never arrived, its absence
# afterwards would prove nothing.
nv_has 0x1500016 || fail "Der NV-Zähler ist gar nicht angekommen"
tools/recovery-swtpm.sh reset main
use_main
if nv_has 0x1500016; then fail "reset wirft den TPM-Zustand nicht weg"; fi
# ls -A is asked for its exit status as well, not only for its output:
# measured, on a directory that is GONE it prints nothing and fails, and an
# emptiness test on its own reads that silence as "tidy".
if ! rest="$(ls -A dist/recovery/work)"; then
	fail "dist/recovery/work fehlt oder lässt sich nicht lesen"
fi
[ -z "$rest" ] || fail "reset räumt dist/recovery/work nicht"
ok "reset stellt ein jungfräuliches TPM und ein leeres Arbeitsverzeichnis her"

# One instance at a time: from work package 5 on a second simulator runs on
# the same machine, and a reset reaching across would throw away the state the
# virtual machine sits on. Asked of a real second simulator, not read off the
# code.
# No flag tells cleanup about this start any more, and that is deliberate:
# start forks the daemon and can still fail behind it - measured, a start that
# gave up after the fork left the simulator standing because cleanup had never
# been told about it. cleanup now asks the machine itself, every time.
tools/recovery-swtpm.sh start qemu
if ! qemu_pid="$(cat dist/recovery/tpm/qemu/swtpm.pid)" || [ -z "$qemu_pid" ]; then
	fail "Die Instanz qemu hinterlässt keine brauchbare PID-Datei"
fi
tools/recovery-swtpm.sh reset main
use_main
# Neither the number alone nor the program name alone: numbers are handed out
# again, and "swtpm" would also fit the other instance. The command line
# carries the state directory, and that is what settles it. -ww because ps
# otherwise cuts the line to the width in COLUMNS.
qemu_args="$(ps -ww -p "$qemu_pid" -o args= || :)"
case "$qemu_args " in
*" --tpmstate dir=$PWD/dist/recovery/tpm/qemu "*) ;;
*) fail "reset main beendet den Simulator der Instanz qemu" ;;
esac
[ -s dist/recovery/tpm/qemu/tpm2-00.permall ] || fail "reset main wirft den Zustand der Instanz qemu weg"
tools/recovery-swtpm.sh stop qemu
ok "reset main lässt die Instanz qemu in Ruhe"

tools/recovery-qemu.sh check
ok "QEMU mit Secure-Boot-OVMF, eigener TPM-Instanz und Messung der Firmware"
use_main

tools/recovery-swtpm.sh reset main
use_main

# The guard in recovery-policy.sh is the most important repair of this task, and
# nothing here could notice it going away: measured, before this case existed
# the guard could be taken out and the run stayed green, because every other
# call below sets TPM2TOOLS_TCTI to our own simulator first and so can never
# reach the guard. This one call can. The value points at a socket that does
# not exist - deliberately NOT at /dev/tpmrm0, the machine's own TPM: a case
# that is only harmless while the code it tests is correct is not a test.
# Measured, so that this holds even if every guard were gone:
# swtpm:path=<no such file> ends at "Could not load tcti" and reaches for
# nothing else, while an EMPTY variable is the one that goes for /dev/tpmrm0
# and /dev/tpm0.
#
# Two things are asked, and the guard's wording is not one of them. It used to
# be, for a reason that was the wrong way round: with the guard cut out the run
# does not end at all, it runs through - so the exit status IS the difference,
# measured. What the wording cannot catch is a guard in the WRONG PLACE, and
# that one was built: the check moved to the foot of the script says its piece
# and ends at exit 1, exactly as it should, and has extended PCR 11 on the way
# there. So PCR 11 is read before and after and has to be the same. That holds
# through any rewording of the message, and it is what caught the case above.
tpm2_pcrread -o dist/recovery/work/pcr11-vorher.bin sha384:11 >/dev/null
vorher="$(xxd -p -c 96 < dist/recovery/work/pcr11-vorher.bin | tr -d '\n')"
fremd="swtpm:path=$PWD/dist/recovery/work/kein-simulator.sock"
if wache="$(TPM2TOOLS_TCTI="$fremd" tools/recovery-policy.sh state normal 2>&1)"; then
	fail "recovery-policy.sh nimmt ein fremdes TPM2TOOLS_TCTI an: $wache"
fi
tpm2_pcrread -o dist/recovery/work/pcr11-nachher.bin sha384:11 >/dev/null
nachher="$(xxd -p -c 96 < dist/recovery/work/pcr11-nachher.bin | tr -d '\n')"
[ "$vorher" = "$nachher" ] \
	|| fail "Der abgewiesene Aufruf hat PCR 11 trotzdem erweitert - er hat also am TPM gearbeitet."
ok "ein fremdes TPM2TOOLS_TCTI hält recovery-policy.sh an"

geheim="TEST-DEK"
tools/recovery-policy.sh state normal
tools/recovery-policy.sh seal pcr

# What the sealing has to leave behind, and above all what it must not. The
# secret is handed to the TPM through a file, and shred -u is what takes that
# file away again - measured, with shred -u replaced by a no-op the run stayed
# green nine times over and dist/recovery/work/secret.bin lay there with the
# secret in it. Asked of the whole directory and not of that one name, because
# that is the promise the head of this file makes: work/ stays standing, so
# nothing in it may carry the secret - a later step parking it under another
# name falls under this too. grep -l prints names, never content, so nothing of
# the secret reaches this run's output even when the case goes red. In work
# package 6 this is the real DEK.
rc=0
traeger="$(grep -rlF "$geheim" dist/recovery/work)" || rc=$?
case "$rc" in
0) fail "Das Geheimnis steht nach dem Siegeln im Klartext in: $traeger" ;;
1) ;;
*) fail "Die Suche nach dem Klartext in dist/recovery/work ist fehlgeschlagen (grep sagt $rc)" ;;
esac
ok "das Siegeln lässt das Geheimnis nirgends im Klartext liegen"

# Removed first so a leftover from an earlier run cannot answer for this one.
rm -f dist/recovery/work/sess.ctx
[ "$(tools/recovery-policy.sh open pcr)" = "$geheim" ] || fail "Normalstart-Zweig öffnet nicht"
ok "Zweig 1: Normalstart über PCR 11 = Produktiv-UKI"

# Three promises of recovery-policy.sh sit in the attributes of the sealed
# object, and not one of them can be read off the script that sets them:
# measured, before this case existed userwithauth in that line left the whole
# run green - and with it set, "tpm2_unseal -c <ctx>" hands out the secret with
# no policy session at all, PCR 11 deliberately standing on the other branch.
# With this case the same manipulation ends at 0xd2, and noda at 0x492, both
# tried. In work package 6 the real DEK hangs on this object. So the
# attributes are read back off the object. 0x92 is fixedtpm|fixedparent|
# adminwithpolicy and nothing besides: userwithauth (0x40) would make the policy
# optional, noda (0x400) would switch off the dictionary lock the platform PIN
# needs from task 7 on, sensitiveDataOrigin (0x20) would mean the TPM had made
# the secret up itself.
#
# The handle is written out here instead of asked of the script under test: "the
# primary key sits at 0x81000001" is a promise of the interface, and measured,
# before this line existed the script moved to 0x81000042 and the run stayed
# green - with the key then sitting there for good.
seal_handle=0x81000001
if ! handles="$(tpm2_getcap handles-persistent)"; then
	fail "Das TPM antwortet nicht auf die Frage nach seinen persistenten Handles"
fi
case "$handles" in
*"$seal_handle"*) ;;
*) fail "Der Primärschlüssel steht nicht unter $seal_handle" ;;
esac
tpm2_flushcontext -t >/dev/null 2>&1 || :
tpm2_load -C "$seal_handle" -u dist/recovery/work/seal.pub -r dist/recovery/work/seal.priv \
	-c dist/recovery/work/pruefung.ctx >/dev/null \
	|| fail "Das versiegelte Objekt lässt sich nicht laden"
pub="$(tpm2_readpublic -c dist/recovery/work/pruefung.ctx)" \
	|| fail "Das versiegelte Objekt lässt sich nicht auslesen"
tpm2_flushcontext -t >/dev/null 2>&1 || :
attrs="$(rohwert attributes "$pub")" || fail "Das Siegel nennt keine Eigenschaften"
# One sentence for both ways this can go wrong, because it is true of both:
# with 0xd2 the policy stops being the only way in, with 0x492 the dictionary
# lock is off. Naming the raw value is what lets the reader look it up.
[ "$attrs" = 0x92 ] \
	|| fail "Das Siegel trägt $attrs statt 0x92. Erlaubt ist allein fixedtpm|fixedparent|adminwithpolicy; jede weitere Eigenschaft schwächt den Schutz des versiegelten Inhalts."

# Two fields above the attributes, in the very same output, and nobody read it:
# the name algorithm of the sealed object. 0xc is sha384. Measured, sha384
# turned into sha1 all through recovery-policy.sh seals and opens exactly as
# before, the run stays green and the attributes still read 0x92 - and the
# policy digest that guards the secret is then a SHA-1 digest.
namealg="$(rohwert name-alg "$pub")" || fail "Das Siegel nennt keinen Namensalgorithmus"
[ "$namealg" = 0xc ] \
	|| fail "Das Siegel rechnet mit Namensalgorithmus $namealg statt 0xc (SHA-384) - die Policy, die es schützt, ist dann ein Digest dieses Verfahrens."

# The primary key wraps the private part of the seal, and up to here only its
# handle was ever looked at, never the key itself. Measured, ecc384 turned into
# ecc256 runs through without a word - and that weaker key is then what the
# sealed blob hangs on. Read off the TPM, where the key really is: 0x23 is ecc,
# curve-id 0x4 is NIST p384, 0xc is sha384.
ppub="$(tpm2_readpublic -c "$seal_handle")" \
	|| fail "Der Primärschlüssel unter $seal_handle lässt sich nicht auslesen"
ptyp="$(rohwert type "$ppub")" || fail "Der Primärschlüssel nennt keine Art"
pkurve="$(rohwert curve-id "$ppub")" || fail "Der Primärschlüssel nennt keine Kurve"
pnamealg="$(rohwert name-alg "$ppub")" || fail "Der Primärschlüssel nennt keinen Namensalgorithmus"
if [ "$ptyp" != 0x23 ] || [ "$pkurve" != 0x4 ] || [ "$pnamealg" != 0xc ]; then
	fail "Der Primärschlüssel ist nicht ECC NIST p384 mit SHA-384 (gelesen: type=$ptyp, curve-id=$pkurve, name-alg=$pnamealg) - er umschließt den privaten Teil des Siegels."
fi

# The other half of the same promise. Dropping the salt alone is already caught
# by case 1 - measured, a session with parameter encryption but without salt has
# no session key and tpm2_unseal answers 0x996 "unsupported symmetric algorithm
# or key size". Dropping the parameter encryption alone is what this catches.
#
# Asked of the TPM, like the attributes above, and no longer of a file:
# open_branch hands its session on instead of flushing it, and
# tpm2_sessionconfig loads that context back out of the TPM. The old way was no
# check - measured, with the encryption taken out and one printf of this very
# sentence put in its place the run stayed green nine times over. What stays is
# named in the report: this script still cannot tell that the session it is
# handed is the one that unsealed.
if ! sitzung="$(tpm2_sessionconfig dist/recovery/work/sess.ctx 2>&1)"; then
	fail "Die Policy-Sitzung steht nach dem Öffnen nicht mehr im TPM - recovery-policy.sh hat sie nicht übergeben"
fi
# Taken away again right here: the handover ends with this run, not with the
# next reset. Its digest is empty by now (the TPM clears it once the session
# has authorised something), so it opens nothing any more either way.
tpm2_flushcontext dist/recovery/work/sess.ctx >/dev/null 2>&1 || :
case "$sitzung" in
*"continuesession|decrypt|encrypt"*) ;;
*) fail "Die Policy-Sitzung verschlüsselt ihre Parameter nicht - das entsiegelte Geheimnis fährt dann offen über die Sitzung. Gelesen: $sitzung" ;;
esac
ok "Siegel, Sitzung und Primärschlüssel tragen genau die zugesagten Eigenschaften"

# Criterion 4 in miniature: one more extend stands for an image changed by a
# single byte. The branch must stay shut.
anders="$(printf 'TEST-EIN-BYTE-ANDERS' | openssl dgst -sha384 -binary | xxd -p -c 96 | tr -d '\n')"
tpm2_pcrextend "11:sha384=$anders" >/dev/null
if tools/recovery-policy.sh open pcr >/dev/null; then fail "Normalzweig öffnet trotz geänderter PCR"; fi
ok "geänderte PCR 11 sperrt den Normalzweig"

# The same question for the eight other PCRs of the branch, and it needed a case
# of its own: they hang on the FIRST of the two tpm2_policypcr calls in
# recovery-policy.sh, and measured, that one line can be deleted without any of
# the cases that stood here before going red - nine times ok, EXIT=0 - while the
# seal then opens with PCR 7 extended, which is exactly what it must not do.
# Nothing above this line is changed for it: the three calls below are the ones
# case 1 opened with, so the only difference is the extend of PCR 7 between
# sealing and opening.
tools/recovery-swtpm.sh reset main
use_main
tools/recovery-policy.sh state normal
tools/recovery-policy.sh seal pcr
tpm2_pcrextend "7:sha384=$anders" >/dev/null
if tools/recovery-policy.sh open pcr >/dev/null; then
	fail "Normalzweig öffnet trotz geänderter PCR 7 - an PCR 0 bis 7 ist er dann gar nicht gebunden"
fi
ok "geänderte PCR 7 sperrt den Normalzweig"

tools/recovery-swtpm.sh reset main
use_main
tools/recovery-policy.sh state recovery
tools/recovery-policy.sh seal pcr
if tools/recovery-policy.sh open pcr >/dev/null; then fail "Normalzweig öffnet im Recovery-Zustand"; fi
ok "im Recovery-Zustand bleibt der Normalzweig zu"
