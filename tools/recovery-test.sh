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
# and that is not a hope: this file searches for it three times over - after the
# sealing, after the first opening and as its LAST case.
# WHERE it searches, and the level is measured, not guessed: the whole of dist/,
# plus every file anywhere under the project that THIS RUN created or changed
# (dist/.lauf-stempel dates the start of the run). Twice the level has been too
# low. With "tpm2_unseal ... | tee $work/dek.bin" a search over work/ alone left
# the run green, fourteen ok and EXIT=0, while dist/recovery/dek.bin held the
# secret, mode 0664, FOR GOOD - nothing ever empties that level. With the search
# over dist/recovery alone a dist/dek.bin one level further up did the same. So
# the question is no longer which directory but which FILES this run touched,
# and the answer to that does not have a level.
# WHAT it searches for: every shape the same eight bytes can stand in, not the
# two that happened to turn up. The list and the reason stand at schreibweisen
# below; what it does NOT reach stands there too.
# Measured why each of the three points is there: with shred -u turned
# into a no-op the run stayed green nine times over while work/secret.bin lay
# there with the secret in it; with the search only in the middle of the run, as
# it used to be, a step that parked the secret under another name afterwards
# stayed green as well - what tidied up was a later reset, not the check; and
# with the search only at the foot, that same step stays green too, because the
# last reset has taken the file away before anybody looks.
# In work package 6 that file is the real DEK.
# Ended by cleanup below: the virtual machine, the simulator of instance qemu,
# and - only while one is recording the wire - the simulator of instance main.
# A recording simulator that outlives a failed run keeps writing into the
# unlinked file the shred left behind, where no search of this file can reach
# it. Ending main by hand: tools/recovery-swtpm.sh stop main.
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

# Dates the start of this run: whatever is newer than this file, this run made,
# and that is what the plaintext search asks instead of asking for a directory.
# Written here and nowhere later - a case that ran before the stamp existed
# would be invisible to the search. Empty, so it carries nothing itself, and
# taken away again by cleanup.
stempel="dist/.lauf-stempel"
mkdir -p dist
: > "$stempel"

# Set by fail alone and read by cleanup: whoever looks at a run that ended badly
# looks for the word FEHLGESCHLAGEN, and a case that dies on a bare command
# never printed it. Measured at the state before these two lines, and no
# hypothetical: tools/recovery-qemu.sh check ends its own failures with a
# message and exit 1, errexit took the run with it - EXIT=1, three ok, and NOT
# ONE FEHLGESCHLAGEN line. It is not the only call of its shape either:
# twenty-four statements in this file call a tool or a tpm2 command without a
# || fail of their own - twenty-one at the left margin and three inside the loop
# over the eight boot PCRs; eleven others do have one. Counted by a machine and
# not by eye, because the last count came out one too low: the continuation
# lines have to be joined first, or every statement whose || fail stands on the
# line below is counted as bare, and the one call that takes no argument at all
# (tools/recovery-env.sh) has to be counted with them. So the answer is not
# twenty-four || fail, each with a wording of its own to keep in step, but the
# one place every single one of them passes through on the way out.
fehler_gemeldet=""
# Set only while a simulator of instance main is writing the wire down, and read
# only by cleanup. Empty everywhere else, so nothing but the wire case can make
# cleanup end that simulator.
mitschnitt_laeuft=""
fail() { fehler_gemeldet=ja; printf 'FEHLGESCHLAGEN: %s\n' "$1" >&2; exit 1; }
ok() { printf 'ok: %s\n' "$1" >&2; }
use_main() {
	TPM2TOOLS_TCTI="$(tools/recovery-swtpm.sh tcti main)"
	export TPM2TOOLS_TCTI
	# Kept so cleanup can tell our simulator from anything else that might be
	# in this variable by then.
	tcti_main="$TPM2TOOLS_TCTI"
}

cleanup() {
	# The status that brought us here, taken before anything else can overwrite
	# it. A run that ends badly without a single FEHLGESCHLAGEN line leaves the
	# reader searching for a word that is not there - that is how every bare
	# command in this file would end, and this is the one place all of them pass
	# through.
	local status=$?
	if [ "$status" -ne 0 ] && [ -z "$fehler_gemeldet" ]; then
		printf 'FEHLGESCHLAGEN: der Lauf endet mit Status %s, ohne dass ein Fall es gemeldet hat. Die Zeilen darüber sagen, woran es lag.\n' \
			"$status" >&2
	fi
	# The recording simulator first, and only then the file it writes into:
	# shred takes the name away, the open descriptor survives it, and every
	# further TPM call of this run would land in a file nothing can search any
	# more. That is what stop does: it takes the PID out of the PID file, and
	# only when that is missing or stale does it ask pgrep -x for the program
	# name - every candidate, from either source, has to carry
	# "--tpmstate dir=" of THIS instance before anything is killed.
	# Only ever the instance this run put a recording on; a main without one
	# keeps running, as the head of this file promises.
	if [ -n "${mitschnitt_laeuft:-}" ]; then
		if ! tools/recovery-swtpm.sh stop main; then
			printf 'Achtung: der mitschneidende Simulator der Instanz main läuft noch. Beenden mit: tools/recovery-swtpm.sh stop main\n' >&2
		fi
	fi
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
	# The stamp has done its work; a finished run leaves exactly what the head
	# of this file lists and nothing else.
	rm -f "$stempel"
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
# schreibweisen: every shape the same eight bytes can stand in. Twice a shape
# walked past a search that only knew the shapes which had already turned up -
# first the hex dump of a recording, which grep -F cannot see at all, then the
# same dump WITHOUT the spaces, which the wire search insisted on. So this is no
# longer a list of incidents but of encodings, and two of them are covered by
# normalising the FILE instead of by adding a needle, which takes every
# separator and every spelling of upper and lower case with them at once:
#
#   raw       TEST-DEK                        needle
#   base64    VEVTVC1E U1QtREVL RVNULURF      needle, one per phase: the secret
#             can begin at any of the three positions of a base64 group, and
#             each needle is the run of whole groups made of secret bytes alone
#             - what stands before and after them depends on foreign bytes.
#   hex       544553542D44454B                the file is read a second time
#             with "0x" dropped and everything that is not a hex digit dropped,
#             and searched case-blind. That one pass takes "54 45 ...",
#             "544553...", "54:45:...", "\x54\x45..." and "0x54, 0x45, ..." at
#             once - and it is what makes a swtpm recording searchable.
#   fillers   UTF-16 in either byte order, NUL padding, "T E S T - D E K", a
#             base64 block broken into lines: the file is read once with NUL
#             bytes and white space dropped, and every needle above is looked
#             for through that as well. No needle carries white space itself, so
#             dropping it can never break one.
#
# NOT reached, fenced rather than claimed: the secret compressed, encrypted, or
# encoded in yet another alphabet (base32, base85, quoted-printable); split
# across two files; or held only in memory or in swap. A file this run never
# touched and that does not lie under dist/ is not looked at either - see
# klartext_suche for why that is the age of the file and not its place.
geheim_hex="$(printf '%s' "$geheim" | xxd -p -u | tr -d '\n')"
nadeln=(-e "$geheim")
for vor in 0 1 2; do
	ab=$(( (3 - vor) % 3 ))
	n=$(( (${#geheim} - ab) / 3 * 3 ))
	nadeln+=(-e "$(printf '%s' "${geheim:ab:n}" | openssl base64 -A)")
done

# Says whether one file carries the secret in any of those shapes, and says it
# with yes or no alone: the call site puts the NAME into the message, because a
# check that prints the secret to prove that the secret is nowhere is no check.
# grep -c and not grep -q: -q stops at the first hit, the normalising tr ahead
# of it then dies of SIGPIPE, and with pipefail the whole pipeline would report
# a failure exactly when it found something. Counting reads the input to the end
# and only ever prints a number.
# LC_ALL=C because these are bytes and not text: sed and tr in a UTF-8 locale
# stumble over sequences that are not characters.
schreibweise_drin() {    # $1 = Datei
	local n
	n="$(LC_ALL=C tr -d '\000 \011\012\015' < "$1" | grep -caF "${nadeln[@]}" || :)"
	[ "$n" -eq 0 ] || return 0
	n="$(LC_ALL=C sed 's/0[xX]//g' "$1" | LC_ALL=C tr -cd '0-9A-Fa-f' | grep -ciF "$geheim_hex" || :)"
	[ "$n" -eq 0 ] || return 0
	return 1
}

# One line for one message of a swtpm recording: which way it went, the four
# bytes that name it, and whether the secret stands in its bytes. swtpm writes a
# header line above the bytes of every message, and the header says which of
# them it is. Read off a real recording, not off the manual - state, seal and
# open together leave 372 messages on this simulator:
#   " SWTPM_IO_Read: length N"   anfrage    what the tools SENT       87
#   " SWTPM_IO_Write: length N"  antwort    what the TPM ANSWERED     87
#   " Ctrl Cmd/Rsp: length N"    steuerung  the control channel       74
#   "Data client disconnected"   abbruch    no bytes at all          124
# Those four are ALL of them, measured; anything else is "unbekannt", and that
# is not a harmless remainder but the alarm: a swtpm whose header lines are
# spelled differently would otherwise leave the accounting below adding up over
# a direction nobody reads any more. The counts themselves are a measurement,
# not a promise - nothing here asserts them.
# Its own function because both places that close a message call it, and a rule
# that stands in two places drifts apart.
nachrichtzeile() {       # $1 = Kopfzeile, $2 = Bytes der Nachricht, hex
	local art cc
	case "$1" in
	' SWTPM_IO_Read: '*) art=anfrage ;;
	' SWTPM_IO_Write: '*) art=antwort ;;
	' Ctrl Cmd: '*|' Ctrl Rsp: '*) art=steuerung ;;
	'Data client disconnected') art=abbruch ;;
	*) art=unbekannt ;;
	esac
	# Tag (2 bytes) and size (4) come first, then the four bytes that name the
	# message: the command code in a request, the response code in an answer.
	# Read off a real recording. A message too short to carry one says so
	# instead of guessing.
	cc="${2:12:8}"
	[ -n "$cc" ] || cc="-"
	case "$2" in
	*"$geheim_hex"*) printf '%s %s ja\n' "$art" "$cc" ;;
	*) printf '%s %s nein\n' "$art" "$cc" ;;
	esac
}

# Walks a swtpm recording and puts one nachrichtzeile out for every message.
# Every run of hex lines is one message; the bytes are put back together first,
# because the eight bytes of the secret can sit astride a line break.
# Plain bash on purpose: awk is mawk on this machine, a package of its own, and
# the tool list in recovery-env.sh would have to grow for a handful of lines.
# Never fails the run itself: the call site decides what the lines mean, and
# this runs inside a $( ) where fail would end the subshell alone.
leitungszeilen() {       # $1 = Mitschnitt
	local zeile="" puffer="" kopf=""
	local hexzeile='^ ([0-9A-F][0-9A-F] )+$'
	while IFS= read -r zeile; do
		if [[ "$zeile" =~ $hexzeile ]]; then
			puffer="$puffer${zeile// /}"
			continue
		fi
		# Empty only before the first header of the file, where there is no
		# message yet to put out.
		if [ -n "$kopf" ]; then nachrichtzeile "$kopf" "$puffer"; fi
		puffer=""
		# This line is the header of the message whose bytes follow.
		kopf="$zeile"
	done < "$1"
	# A file that ends without a newline leaves its last line in zeile, and the
	# loop never saw it. A file that ends with one leaves zeile empty, and then
	# the line below does nothing.
	if [[ "$zeile" =~ $hexzeile ]]; then puffer="$puffer${zeile// /}"; fi
	if [ -n "$kopf" ]; then nachrichtzeile "$kopf" "$puffer"; fi
}

# Searches every file this run can have written to, in every shape of
# schreibweisen above. TWO lists, because one alone has been wrong twice:
#  - the whole of dist/, whatever its age. A leftover of an EARLIER run carries
#    no fresh timestamp, and that is not a thought but a thing that happened:
#    with "tpm2_unseal ... | tee $work/dek.bin" the file dist/recovery/dek.bin
#    was still lying there a whole run later and made the NEXT run red. No reset
#    ever takes that level away - reset takes work/ and the state directory of
#    its own instance, and neither of them is it.
#  - every file under the project that is newer than the stamp this run wrote
#    before its first case. THAT is what makes the level stop mattering: a step
#    that parks the secret one directory further up, beside the scripts or in
#    the project root, is caught without this function knowing where to look.
#    The checked-in scripts carry the secret themselves and are older than the
#    stamp - which is exactly why the age of a file and not its place decides.
# Nothing of the file ever reaches this run's output: schreibweise_drin answers
# yes or no, and only the NAME goes into the message.
# find and not a glob: a glob walks neither subdirectories nor names that begin
# with a dot, and dist/recovery has both (tpm/main/.lock,
# softhsm/tokens/<uuid>/...). The status of each find is asked for its own sake:
# an empty answer out of a tree that cannot be read is not the same as "nothing
# in it", and only the second may pass as good news. A file that is gone between
# the listing and the look is skipped - the simulator tidies its own state up
# underneath - but one that is there and unreadable stops the run, because an
# unreadable file is exactly where an escaped secret would be comfortable.
# What it costs, measured and not guessed: 67 to 68 files lie under dist/ when a
# run is through - the count wobbles with the contexts the last case leaves -
# the 528 KiB OVMF variable file among them, and the search runs three times;
# the whole test grew from 16.7 s to 19.3 s. The walk over the project is the
# cheap half: over a tree of 8550 files, the size of a whole NGC checkout, find
# takes 71 ms.
klartext_suche() {       # $1 = Zeitpunkt, für die Meldung
	local liste neu datei
	if ! liste="$(find dist -type f)"; then
		fail "Die Dateiliste von dist lässt sich nicht erstellen - die Klartextsuche hat dann nichts abgesucht."
	fi
	if ! neu="$(find . -path ./dist -prune -o -newer "$stempel" -type f -print)"; then
		fail "Die Liste der in diesem Lauf angefassten Dateien lässt sich nicht erstellen - die Klartextsuche hat dann nichts abgesucht."
	fi
	while IFS= read -r datei; do
		[ -n "$datei" ] || continue
		[ -e "$datei" ] || continue
		[ -r "$datei" ] || fail "$datei lässt sich nicht lesen - die Klartextsuche $1 hat diese Datei dann nicht abgesucht."
		if schreibweise_drin "$datei"; then
			fail "Das Geheimnis steht $1 in: $datei"
		fi
	done <<< "$liste
$neu"
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
# measured at the state before the wire case existed, the encryption taken out
# AND one tpm2_sessionconfig --enable-encrypt --enable-decrypt put on the same
# file AFTER the tpm2_unseal kept the run green while the secret came out in the
# clear. Today the wire case at the foot of this file ends that run, eleven ok
# behind it, and this case here goes green through it just as it did then: the
# property itself is asked down there, not here.
# What this catches is the plain forgetting of the switch, and it does - measured,
# the encryption taken out on its own ends the run right here, eight ok, with a
# message that names the session instead of the wire.
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
# the TPM. Measured what that was worth, at the state before this case existed:
# the parameter encryption taken out AND one tpm2_sessionconfig
# --enable-encrypt --enable-decrypt put on that same context file AFTER the
# tpm2_unseal - the file then says what the session never was, and the run
# stayed green, eleven ok, while the secret came out unencrypted. The
# three-command way (unseal naked, flush, hand over a second encrypted session)
# did exactly the same.
#
# So the wire itself is looked at. swtpm writes it down with --log level=20; the
# recording carries the plaintext and is therefore handled like secret.bin.
#
# THE MEASURE IS A LEDGER OVER NAMED HITS, and it took four ways of getting the
# plaintext out to arrive at it. Three earlier measures each caught the ways
# that had already been seen and let the next one through:
#   a difference ("the opening must not RAISE the count") - broken by a seal()
#     that checks its own work with a bare unseal, because its plaintext answer
#     is already inside the base value; 14 ok, EXIT=0;
#   a direction ("nothing in an answer may carry it") - broken by taking the
#     recovered plaintext and sending it to the TPM as an unencrypted REQUEST
#     (tpm2_hash); measured with the counting of this very file: 3 requests, 0
#     answers, 14 ok, EXIT=0;
#   and both of them took their base value in the MIDDLE of the ceremony, where
#     everything the sealing had already put on the wire was invisible.
# What stands here instead asks nothing about directions or differences. It
# takes the WHOLE recording, both directions at once, from before the ceremony
# to after it, and demands: every single hit sits at one named, known place, and
# there are exactly as many of them as that place explains.
#
# The named place, and it is the open point of this package, not a fault of this
# case: tpm2_create -i hands the secret to the TPM in the clear, because sealing
# runs without any session at all. Twice, because the TPM answers the first
# attempt with 0x922 TPM_RC_RETRY and ESAPI sends it again. So: two hits, both
# requests, both TPM2_CC_Create - and nothing else anywhere. In work package 0
# that is TEST-DEK; before work package 6 it has to be decided. Whoever secures
# the sealing has to change befund_erwartet with it.
# One ledger catches all three: one hit more in a request (a fourth way), one
# hit more in an answer (a bare unseal), and a swtpm whose header lines are
# spelled differently - the hits then no longer land on a known direction and
# the ledger does not add up.
#
# Two counter-proofs, because a ledger over an unread recording adds up
# beautifully. Both arms of the direction reading are asked SEPARATELY: the
# recording has to hold at least one message of each of the four kinds
# (anfrage, antwort, steuerung, abbruch) and not one of the kind "unbekannt".
# Breaking either arm alone - renaming the request header or the answer header -
# makes exactly that arm empty and the case red. And the recording has to hold
# the OPENING itself: a request that names TPM2_CC_Unseal. That is sharper than
# asking whether the file grew, which the earlier version did: a recording
# without the opening in it explains nothing about the opening.
mit="dist/recovery/work/wire-main.log"
# The two commands this case names, as the four bytes they stand as in a
# request. Read off a real recording, not off the manual.
cc_create=00000153      # TPM2_CC_Create - the sealing, and the one known leak
cc_unseal=0000015E      # TPM2_CC_Unseal - the opening, proof the recording has it
# The whole ledger: exactly these hits, and no others anywhere in the recording.
befund_erwartet="anfrage $cc_create
anfrage $cc_create"
# The counter-case first: without the test asking for one, no recording
# comes into being at all.
tools/recovery-swtpm.sh reset main
use_main
tools/recovery-policy.sh state normal
[ ! -e "$mit" ] \
	|| fail "recovery-swtpm.sh schreibt einen Mitschnitt, ohne dass der Test ihn verlangt hat: $mit"

tools/recovery-swtpm.sh reset main mitschnitt
# From here on a simulator is writing the plaintext down, and cleanup has to
# know: a run that dies below leaves it writing into the file the shred has
# already unlinked.
mitschnitt_laeuft=ja
use_main
[ -f "$mit" ] || fail "Der angeforderte Mitschnitt ist nicht entstanden: $mit"
tools/recovery-policy.sh state normal
tools/recovery-policy.sh seal pcr
rm -f dist/recovery/work/sess.ctx
[ "$(tools/recovery-policy.sh open pcr)" = "$geheim" ] || fail "Normalstart-Zweig öffnet im Leitungsfall nicht"
# The whole recording at once, and no value taken anywhere in the middle of the
# ceremony: these lines cover the sealing and the opening together.
zeilen="$(leitungszeilen "$mit")"
# The instrument before the measurement. Each arm on its own, because a
# counter-proof that only breaks one arm secures only that one.
for art in anfrage antwort steuerung abbruch; do
	grep -q "^$art " <<< "$zeilen" \
		|| fail "Im Mitschnitt steht keine einzige Nachricht der Art '$art'. Die Buchführung darunter liest dann eine Richtung, die es für sie nicht gibt - vermutlich hat swtpm seine Kopfzeilen umbenannt."
done
if grep -q '^unbekannt ' <<< "$zeilen"; then
	fail "Der Mitschnitt enthält Nachrichten, deren Kopfzeile dieser Test nicht kennt. Die Buchführung darunter ordnet sie keiner Richtung zu - vermutlich hat swtpm seine Kopfzeilen umbenannt."
fi
grep -q "^anfrage $cc_unseal " <<< "$zeilen" \
	|| fail "Im Mitschnitt steht keine Anfrage TPM2_CC_Unseal ($cc_unseal). Das Öffnen ist dann gar nicht aufgezeichnet, und die Buchführung darunter sagt nichts über das Öffnen."
# The ledger. Every hit, both directions, the whole run - and each one has to
# sit at the one named place.
befund="$(sed -n 's/ ja$//p' <<< "$zeilen" | sort)"
[ "$befund" = "$befund_erwartet" ] \
	|| fail "Der Klartext steht im Mitschnitt woanders als an der einen bekannten Stelle. Erwartet waren nur die zwei Anfragen des Siegelns (TPM2_CC_Create, $cc_create), gefunden wurde: $(printf '%s' "$befund" | tr '\n' ';')"
# Away with it, and looked at afterwards to see that it is away. The same
# handling as secret.bin gets: the recording carries the plaintext, and in work
# package 6 that would be the real DEK. The simulator goes first: shred takes
# the name, the open descriptor survives it, and everything this run still does
# at the TPM would land where nothing can search it. stop takes the PID out of
# the PID file and only falls back to the program name when that is missing or
# stale; either way the candidate has to carry "--tpmstate dir=" of this
# instance before it is killed.
tools/recovery-swtpm.sh stop main || fail "Der mitschneidende Simulator lässt sich nicht beenden - er schriebe in die gleich gelöschte Datei weiter."
mitschnitt_laeuft=""
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
ok "auch nach dem vollen Lauf liegt das Geheimnis in keiner Schreibweise irgendwo, was dieser Lauf angefasst hat"
