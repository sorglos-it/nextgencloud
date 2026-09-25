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
# and that is not a hope: this file searches the whole directory for it three
# times over - after the sealing, after the first opening and as its LAST case -
# in both shapes it can take: the characters themselves and the hex dump of a
# swtpm recording. Measured why each of the three is there: with shred -u turned
# into a no-op the run stayed green nine times over while work/secret.bin lay
# there with the secret in it; with the search only in the middle of the run, as
# it used to be, a step that parked the secret under another name afterwards
# stayed green as well - what tidied up was a later reset, not the check; and
# with the search only at the foot, that same step stays green too, because the
# last reset has taken the file away before anybody looks.
# In work package 6 that file is the real DEK.
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
	# The wire case further down has swtpm write the plaintext into a file and
	# takes that file away again itself. What is caught here is the run that
	# dies before it gets there: the file would otherwise lie around until the
	# next reset.
	shred -u dist/recovery/work/wire-main.log 2>/dev/null || :
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

geheim="TEST-DEK"
# The same secret as the TPM sees it on the wire. swtpm writes its recording as
# a hex dump, sixteen bytes to a line, so nothing that looks for the characters
# TEST-DEK will ever find it there - measured, grep -F comes back empty on a
# recording that provably carries it twice.
geheim_hex="$(printf '%s' "$geheim" | xxd -p -u | tr -d '\n')"

# Counts how often the secret crosses the wire in a swtpm recording. Every run
# of hex lines is one message; the bytes of a message are put back together
# first, because the eight bytes of the secret can sit astride a line break.
# Plain bash on purpose: awk is mawk on this machine, a package of its own, and
# the tool list in recovery-env.sh would have to grow for a handful of lines.
# Only ever returns a number, never fails the run: the call site decides what a
# number means, and this runs inside a $( ) where fail would end the subshell.
leitungstreffer() {      # $1 = Mitschnitt
	local zeile="" puffer="" treffer=0
	local hexzeile='^ ([0-9A-F][0-9A-F] )+$'
	while IFS= read -r zeile; do
		if [[ "$zeile" =~ $hexzeile ]]; then
			puffer="$puffer${zeile// /}"
			continue
		fi
		case "$puffer" in *"$geheim_hex"*) treffer=$((treffer + 1)) ;; esac
		puffer=""
	done < "$1"
	# A file that ends without a newline leaves its last line in zeile, and the
	# loop never saw it. A file that ends with one leaves zeile empty, and then
	# the line below does nothing.
	if [[ "$zeile" =~ $hexzeile ]]; then puffer="$puffer${zeile// /}"; fi
	case "$puffer" in *"$geheim_hex"*) treffer=$((treffer + 1)) ;; esac
	printf '%s\n' "$treffer"
}

# Searches the whole working directory for the secret, in both shapes it can
# take: the characters themselves, and the hex dump of a swtpm recording.
# The promise in the head of this file is about the DIRECTORY, not about one
# file name - a step that parks the secret under another name falls under it
# too. grep -l prints names, never content, so nothing of the secret reaches
# this run's output even when the case goes red.
klartext_suche() {       # $1 = Zeitpunkt, für die Meldung
	local rc=0 traeger datei
	traeger="$(grep -rlF "$geheim" dist/recovery/work)" || rc=$?
	case "$rc" in
	0) fail "Das Geheimnis steht $1 im Klartext in: $traeger" ;;
	1) ;;
	*) fail "Die Suche nach dem Klartext in dist/recovery/work ist fehlgeschlagen (grep sagt $rc)" ;;
	esac
	for datei in dist/recovery/work/*; do
		[ -f "$datei" ] || continue
		[ "$(leitungstreffer "$datei")" -eq 0 ] \
			|| fail "Das Geheimnis steht $1 als Hexdump der Leitung in: $datei"
	done
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
#
# All three subcommands, not state alone. The guard sits at the top of
# recovery-policy.sh and so covers every one of them today - but a check that
# only ever asks state would pass a guard that holds for state and is walked
# around by open. None of the three gets past the guard, so all three are cheap.
#
# The || fail on the four lines around the loop is no decoration either:
# measured, sha384:11 turned into sha999:11 ended the run at EXIT=1 after four
# ok and NOT ONE "FEHLGESCHLAGEN" line - the very hole that pub= further down
# had its || fail put on for one round earlier.
tpm2_pcrread -o dist/recovery/work/pcr11-vorher.bin sha384:11 >/dev/null \
	|| fail "PCR 11 lässt sich vor dem Wächterfall nicht lesen"
vorher="$(xxd -p -c 96 < dist/recovery/work/pcr11-vorher.bin | tr -d '\n')" \
	|| fail "PCR 11 lässt sich vor dem Wächterfall nicht in Hex wandeln"
fremd="swtpm:path=$PWD/dist/recovery/work/kein-simulator.sock"
for unterbefehl in state seal open; do
	case "$unterbefehl" in
	state) zusatz=normal ;;
	*) zusatz=pcr ;;
	esac
	if wache="$(TPM2TOOLS_TCTI="$fremd" tools/recovery-policy.sh "$unterbefehl" "$zusatz" 2>&1)"; then
		fail "recovery-policy.sh nimmt bei '$unterbefehl $zusatz' ein fremdes TPM2TOOLS_TCTI an: $wache"
	fi
done
tpm2_pcrread -o dist/recovery/work/pcr11-nachher.bin sha384:11 >/dev/null \
	|| fail "PCR 11 lässt sich nach dem Wächterfall nicht lesen"
nachher="$(xxd -p -c 96 < dist/recovery/work/pcr11-nachher.bin | tr -d '\n')" \
	|| fail "PCR 11 lässt sich nach dem Wächterfall nicht in Hex wandeln"
[ "$vorher" = "$nachher" ] \
	|| fail "Der abgewiesene Aufruf hat PCR 11 trotzdem erweitert - er hat also am TPM gearbeitet."
ok "ein fremdes TPM2TOOLS_TCTI hält recovery-policy.sh an"

tools/recovery-policy.sh state normal
tools/recovery-policy.sh seal pcr

# What the sealing has to leave behind, and above all what it must not. The
# secret is handed to the TPM through a file, and shred -u is what takes that
# file away again - measured, with shred -u replaced by a no-op the run stayed
# green nine times over and dist/recovery/work/secret.bin lay there with the
# secret in it. In work package 6 this is the real DEK.
# Asked at three points, and each one catches something the others let through:
# here, because this is where the sealing is and where a broken shred shows up
# at once; right after the first opening, because that is where a secret parked
# under another name appears; and at the foot of this file, because the promise
# in its head is about what a FINISHED run leaves behind, and ten more sealings
# follow this line.
klartext_suche "nach dem Siegeln"
ok "das Siegeln lässt das Geheimnis nirgends im Klartext liegen"

# Removed first so a leftover from an earlier run cannot answer for this one.
rm -f dist/recovery/work/sess.ctx
[ "$(tools/recovery-policy.sh open pcr)" = "$geheim" ] || fail "Normalstart-Zweig öffnet nicht"
ok "Zweig 1: Normalstart über PCR 11 = Produktiv-UKI"

# The same search again, right behind the first opening. It stands here because
# the other two points let through exactly what the opening parks: one runs
# before it, the other only after the last "reset main", and that reset takes
# the file away before anybody looks. Measured - with
# "tpm2_unseal ... | tee work/dek.bin" in open_branch both versions stayed
# green, the old one eleven times ok and the new one thirteen, while
# work/dek.bin lay
# there with the secret in it in between. In work package 6 that is the real
# DEK.
klartext_suche "nach dem Öffnen"
ok "auch das Öffnen legt das Geheimnis nirgends ab"

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

# Immediately above the attributes, in the very same output, and nobody read it:
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
# What this line really asks, said plainly, because it used to claim more: it
# reads the attributes out of a tpm2-tools CONTEXT FILE. The TPM only has to
# still hold that context for the file to load; decrypt and encrypt are flags
# per command, and the TPM keeps no state about them. So this is not the
# property itself, it is a statement recovery-policy.sh makes about itself -
# measured, one tpm2_sessionconfig --enable-encrypt --enable-decrypt on the same
# file AFTER the tpm2_unseal keeps the run green while the secret came out in
# the clear. The property itself is asked at the foot of this file, of the wire.
# What this catches is the plain forgetting of the switch, and that is worth the
# two lines.
# Nor does it say WHAT KIND of session this is: tpm2_sessionconfig prints
# handle, attributes and digest, and nothing about session type or hash
# algorithm. The case below therefore claims nothing about either.
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

# The same question for the eight other PCRs of the branch, and it needed cases
# of its own: they hang on the FIRST of the two tpm2_policypcr calls in
# recovery-policy.sh, and measured, that one line can be deleted without any of
# the cases that stood above it going red - nine times ok, EXIT=0 - while the
# seal then opens with PCR 7 extended, which is exactly what it must not do.
# Nothing above this line is changed for it: the three calls below are the ones
# case 1 opened with, so the only difference is the extend between sealing and
# opening.
#
# Every one of the eight, one at a time, and not PCR 7 standing in for the rest.
# Measured why: with pcrs_boot in recovery-policy.sh turned from
# sha384:0,...,7 into sha384:7,8,9,10,12,13,14,15 the run stayed green from end
# to end, eleven times ok and EXIT=0 - eight digests, so the length check is
# satisfied, PCR 7 still in the selection, and PCR 0 to 6 bound to nothing at
# all with nobody noticing. A case per register is what makes the selection
# itself visible. Measured what that costs: the whole run grows from 7 to 14
# seconds.
for n in 0 1 2 3 4 5 6 7; do
	tools/recovery-swtpm.sh reset main
	use_main
	tools/recovery-policy.sh state normal
	tools/recovery-policy.sh seal pcr
	tpm2_pcrextend "$n:sha384=$anders" >/dev/null \
		|| fail "PCR $n lässt sich nicht erweitern - der Fall hat gar nicht stattgefunden"
	if tools/recovery-policy.sh open pcr >/dev/null; then
		fail "Normalzweig öffnet trotz geänderter PCR $n - an dieses Register ist er nicht gebunden"
	fi
done
ok "jede der acht Boot-PCR 0 bis 7 sperrt den Normalzweig für sich allein"

# The unsealed secret must not travel in the open, and up to here the only word
# on that was the tested script's own: the case above reads the attributes of
# the handed-over session, and those sit in a tpm2-tools context file, not in
# the TPM. Measured what that is worth: ONE extra tpm2_sessionconfig
# --enable-encrypt --enable-decrypt on that same context file AFTER the
# tpm2_unseal - and the run stays green, eleven times ok, while the secret came
# out unencrypted. The three-command way (unseal naked, flush, hand over a
# second encrypted session) does exactly the same.
#
# So the wire itself is looked at. swtpm writes it down with --log level=20; the
# recording carries the plaintext and is therefore handled like secret.bin.
# What comes out of it, measured on this simulator:
#
#   honest run:  2 hits, both in the TPM2_CC_Create of the sealing (twice
#                because the TPM answers the first one with 0x922 TPM_RC_RETRY
#                and ESAPI sends it again), NONE in the opening
#   either attack above: 3 hits, the third being the unseal answer in the clear
#
# What is checked is therefore not a fixed number but that the opening does NOT
# raise it. The hits of the sealing are the counter-proof that makes that worth
# anything: a search that finds nothing where the plaintext provably is would
# make the line below green for the wrong reason. That the recording grew at all
# is asked as well - a dead recording has no hits either.
#
# That plaintext flows during the SEALING is not a fault of this case but the
# open point from the last round: tpm2_create -i runs without any session at
# all. In work package 0 that is TEST-DEK; before work package 6 it has to be
# decided.
mit="dist/recovery/work/wire-main.log"
# The counter-case first: without the test asking for one, no recording
# comes into being at all.
tools/recovery-swtpm.sh reset main
use_main
tools/recovery-policy.sh state normal
[ ! -e "$mit" ] \
	|| fail "recovery-swtpm.sh schreibt einen Mitschnitt, ohne dass der Test ihn verlangt hat: $mit"

tools/recovery-swtpm.sh reset main mitschnitt
use_main
[ -f "$mit" ] || fail "Der angeforderte Mitschnitt ist nicht entstanden: $mit"
tools/recovery-policy.sh state normal
tools/recovery-policy.sh seal pcr
treffer_vor="$(leitungstreffer "$mit")"
[ "$treffer_vor" -ge 1 ] \
	|| fail "Die Suche findet den Klartext nicht einmal dort, wo das Siegeln ihn nachweislich hinschreibt - sie taugt dann auch als Beleg fürs Öffnen nichts."
laenge_vor="$(wc -c < "$mit")"
rm -f dist/recovery/work/sess.ctx
[ "$(tools/recovery-policy.sh open pcr)" = "$geheim" ] || fail "Normalstart-Zweig öffnet im Leitungsfall nicht"
[ "$(wc -c < "$mit")" -gt "$laenge_vor" ] \
	|| fail "Der Mitschnitt ist beim Öffnen nicht gewachsen - er wird nicht geschrieben, und der Vergleich darunter sagt dann nichts."
treffer_nach="$(leitungstreffer "$mit")"
[ "$treffer_nach" -eq "$treffer_vor" ] \
	|| fail "Das entsiegelte Geheimnis fährt offen über die Leitung: $treffer_nach statt $treffer_vor Treffer im Mitschnitt. Die Parameterverschlüsselung der Policy-Sitzung greift nicht."
# Away with it, and looked at afterwards to see that it is away. The same
# handling as secret.bin gets: the recording carries the plaintext, and in work
# package 6 that would be the real DEK. The next "reset main" clears it too, and
# so does cleanup, but both only later.
shred -u "$mit" || fail "Der Mitschnitt lässt sich nicht sicher löschen: $mit"
[ ! -e "$mit" ] || fail "Der Mitschnitt liegt nach shred -u immer noch da: $mit"
ok "das entsiegelte Geheimnis fährt nicht offen über die Leitung"

tools/recovery-swtpm.sh reset main
use_main
tools/recovery-policy.sh state recovery
tools/recovery-policy.sh seal pcr
if tools/recovery-policy.sh open pcr >/dev/null; then fail "Normalzweig öffnet im Recovery-Zustand"; fi
ok "im Recovery-Zustand bleibt der Normalzweig zu"

# And the same search once more, right at the end. The head of this file says
# what a FINISHED run leaves standing and concludes from it that the secret is
# not among it - and until now nothing asked that question at the end. The
# search sat in the middle of the run, and what saw to it that nothing was left
# afterwards was a later "reset main", not a check. This is the state the head
# speaks about: the files of the last case, and the recording of the wire must
# not be among them.
klartext_suche "nach dem vollen Lauf"
ok "auch nach dem vollen Lauf liegt das Geheimnis nirgends in dist/recovery/work"
