#!/usr/bin/env bash
# Runs every case of work package 0, including the ones that must fail.
set -euo pipefail
cd "$(dirname "$0")/.."
# Cleanup only: a negative case that failed on purpose leaves its session and
# its objects in the TPM - the simulator holds only a handful of either.
trap 'for h in -t -l -s; do tpm2_flushcontext "$h" >/dev/null 2>&1 || :; done' EXIT

fail() { printf 'FEHLGESCHLAGEN: %s\n' "$1" >&2; exit 1; }
ok() { printf 'ok: %s\n' "$1" >&2; }
use_main() {
	TPM2TOOLS_TCTI="$(tools/recovery-swtpm.sh tcti main)"
	export TPM2TOOLS_TCTI
}

# A syntax error in any script must show up here, not three cases later.
for s in tools/recovery-*.sh; do bash -n "$s"; done

tools/recovery-env.sh
tools/recovery-setup.sh prepare
tools/recovery-swtpm.sh reset main
use_main
tools/recovery-env.sh pcr-bank
tpm2_getrandom 8 -o dist/recovery/work/rnd.bin || fail "TPM antwortet nicht"
ok "TPM antwortet, SHA-384-Bank vorhanden"
