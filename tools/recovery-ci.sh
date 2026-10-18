#!/usr/bin/env bash
# The CI job of work package 0: check the tools, set the test environment up,
# run every case, leave a log that says what happened. Red means no merge.
# "install" stays out: tools/recovery-setup.sh install needs sudo and the
# network and runs once per machine, and a CI run must never reach for apt-get.
# recovery-test.sh calls "prepare" itself, so tokens and keys are made there.
#
# Return values, and nothing else: 0 and GRUEN when every case passed, 1 and
# ROT otherwise, with the last 20 lines of the log on standard error. Log under
# dist/recovery/ci/<timestamp>.log and, as a copy, dist/recovery/ci/last.log.
#
# ---------------------------------------------------------------------------
# The known flutter point, and the decision taken here, written down because a
# job that goes red now and then without anything being broken is worse than no
# job at all:
#
# One number in the plaintext ledger of recovery-test.sh (the hits of the eight
# bytes in the wire recording) is three, and the third hit exists only because
# the TPM answers one tpm2_create with TPM_RC_RETRY and ESAPI sends it again.
# Task 6 measured exactly one retry, tasks 9 and 10 saw it hold over four runs.
# A run with no retry or with two makes that number 2 or 4 and the case red,
# and that is the SAFE direction: the ledger says what it found.
#
# This job does NOT repeat the run and does NOT excuse that case. Two measured
# reasons, not a preference:
#   - Nobody has ever seen the other count. The rate is unmeasured, so a repeat
#     policy would be sized by guess, and a repeat cannot be shown to help.
#   - A repeat that turns a red run green is a hole for every other case too,
#     and the cases of this package are the only gate the package has.
# What it does instead: it READS the log and says which kind of red it is. If
# the one and only failed case is that ledger line, the summary names the known
# cause and asks for one hand-started repeat, instead of letting a human hunt a
# code fault that is not there. The return value stays 1 either way - the gate
# holds, no merge on red.
# ---------------------------------------------------------------------------
set -euo pipefail
shopt -s inherit_errexit
cd "$(dirname "$0")/.."

if [ "$#" -ne 0 ]; then
	printf 'Aufruf: tools/recovery-ci.sh (ohne weitere Angaben)\n' >&2
	exit 1
fi

mkdir -p dist/recovery/ci
log="dist/recovery/ci/$(date +%Y%m%d-%H%M%S).log"

{
	printf 'Recovery-Testlauf %s\n' "$(date -Is)"
	printf 'Git: %s\n' "$(git rev-parse --short HEAD 2>/dev/null || printf 'kein Git-Baum')"
} > "$log"

# The sentence the flutter case prints when it fails. This is a COPY of that
# sentence, not a lookup: nothing here reads recovery-test.sh. It was checked
# once against the delivered file (grep, exactly one hit, 12.10.2026), and if
# that file ever words it differently the check below finds nothing and falls
# back to "a real red" - the safe direction, it never turns a red into
# something softer than red, it only stops explaining it.
flatter_satz='Die acht Byte stehen im Mitschnitt noch woanders als an der einen bekannten Stelle'

# Reads the log and answers "flatter" or "echt". Only the one case, and only
# when it is ALONE: a second failed case means something else is broken as
# well, and then nothing here is known.
einordnen() {                     # $1 = Protokoll
	local datei="$1" zahl=""
	zahl="$(grep -c '^FEHLGESCHLAGEN: ' "$datei" || :)"
	if [ "$zahl" = 1 ] && grep -qF "FEHLGESCHLAGEN: $flatter_satz" "$datei"; then
		printf 'flatter\n'
	else
		printf 'echt\n'
	fi
}

# Neither step may take the job down: the result has to reach the log and the
# summary either way.
status=0
if ! tools/recovery-tools.sh >> "$log" 2>&1; then status=1; fi
if [ "$status" -eq 0 ] && ! tools/recovery-test.sh >> "$log" 2>&1; then status=1; fi

# A green run that ran no case at all is not a result. The number is not
# compared against an expected count on purpose - it grows with every task and
# would age into a false red; zero is the only value that cannot be right.
if [ "$status" -eq 0 ]; then
	faelle="$(grep -c '^ok: ' "$log" || :)"
	if [ "$faelle" -eq 0 ]; then
		printf 'Kein einziger Fall ist gelaufen - das ist kein grüner Lauf.\n' >> "$log"
		status=1
	fi
else
	faelle="$(grep -c '^ok: ' "$log" || :)"
fi

# No symlink: the work directory has to survive being copied anywhere.
cp "$log" dist/recovery/ci/last.log

if [ "$status" -ne 0 ]; then
	printf 'ROT: der Testlauf ist fehlgeschlagen. %s Fälle bestanden.\n' "$faelle" >&2
	if [ "$(einordnen "$log")" = flatter ]; then
		printf 'Einordnung: der einzige gescheiterte Fall ist der bekannte Flatter-Punkt (die Zahl der Treffer im Mitschnitt hängt an genau einem TPM_RC_RETRY). Lauf einmal von Hand wieder an; bleibt es rot, ist es kein Flattern.\n' >&2
	fi
	printf 'Die letzten 20 Zeilen (vollständig in %s):\n' "$log" >&2
	tail -20 "$log" >&2
	exit 1
fi
printf 'GRUEN: alle %s Fälle von Paket 0 bestanden. Protokoll: %s\n' "$faelle" "$log" >&2
