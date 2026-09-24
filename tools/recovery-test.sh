#!/usr/bin/env bash
# The test run for work package 0: every case the package has so far, and it
# grows with each task. The cases that must fail will run here too.
# It does not set a machine up - tools/recovery-setup.sh install and
# tools/recovery-tools.sh have to have been through first.
# Careful: it empties dist/recovery/work. Nothing may be parked there.
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
