#!/usr/bin/env bash
# Checks that the recovery test environment has everything it needs. Every
# other script starts with this, so a missing package fails early and says so.
set -euo pipefail
cd "$(dirname "$0")/.."
work="$(pwd)/dist/recovery"

# Every tool the package actually calls, including the ones that only appear
# deep inside recovery-policy.sh - a missing tpm2_policynv must not surface as
# a failing policy branch three tasks later. No "go" here: recovery-setup.sh
# never installs it ("No Go here: the repository and tools/check.sh stay on
# the machine that commits"), and this script itself runs only on the VM.
# Measured on ngc-recovery: /usr/bin/go there is go1.24.4 from an unrelated
# apt package, not from this package's install list - it happens to be
# present today only because of other work sharing the VM, and requiring it
# here would fail this check, and therefore every later script, on any clean
# VM that only ever ran tools/recovery-setup.sh install.
need="qemu-system-x86_64 swtpm swtpm_setup softhsm2-util pkcs11-tool openssl xxd socat shred
	pgrep tpm2 tpm2_startauthsession tpm2_sessionconfig tpm2_flushcontext tpm2_createprimary
	tpm2_evictcontrol tpm2_getpolicydigest tpm2_create tpm2_load tpm2_loadexternal
	tpm2_unseal tpm2_pcrread tpm2_pcrextend tpm2_policypcr tpm2_policysigned
	tpm2_policyauthvalue tpm2_policynv tpm2_policyor tpm2_nvdefine tpm2_nvincrement
	tpm2_nvread tpm2_nvreadpublic tpm2_getcap tpm2_getrandom tpm2_verifysignature
	tpm2_dictionarylockout"
missing=""
for tool in $need; do
	if ! command -v "$tool" >/dev/null; then missing="$missing $tool"; fi
done
# Firmware and the PKCS#11 module are data, not programs: check the files. The
# secure-boot build is the one work package 8 will sign against.
for file in /usr/share/OVMF/OVMF_CODE_4M.secboot.fd /usr/share/OVMF/OVMF_VARS_4M.ms.fd \
	/usr/lib/softhsm/libsofthsm2.so; do
	if [ ! -f "$file" ]; then missing="$missing $(basename "$file")"; fi
done

if [ -n "$missing" ]; then
	printf 'Es fehlt:%s\n' "$missing" >&2
	printf 'Einrichten mit: tools/recovery-setup.sh install\n' >&2
	exit 1
fi

# More than bare existence: task 1's own EXIT/HUP/INT/TERM traps now delete a
# truncated werkzeuge.txt, but nothing can trap SIGKILL or a lost VM, so a
# stale, half-written file could still be sitting here. "Geprüft und in
# Ordnung:" is the heading recovery-tools.sh's success block writes right
# before the checked lines and the final KVM line - cheap to ask for, and
# exactly the boundary a truncated run stopped short of (measured in task 1:
# 7 of 24 lines, missing everything from this heading on).
if [ ! -s "$work/werkzeuge.txt" ] || ! grep -q '^Geprüft und in Ordnung:$' "$work/werkzeuge.txt"; then
	printf 'Die Werkzeuge sind noch nicht geprüft: %s fehlt oder ist unvollständig.\n' "$work/werkzeuge.txt" >&2
	printf 'Prüfen mit: tools/recovery-tools.sh\n' >&2
	exit 1
fi

pcr_bank() {
	if [ -z "${TPM2TOOLS_TCTI:-}" ]; then
		printf 'TPM2TOOLS_TCTI ist nicht gesetzt.\n' >&2
		printf 'Setzen mit: export TPM2TOOLS_TCTI="$(tools/recovery-swtpm.sh tcti main)"\n' >&2
		exit 1
	fi
	# Declared and assigned on separate lines, and tested as the if's own
	# condition rather than piped into grep: capturing first sidesteps two
	# things measured on this package - a call inside a tested condition is
	# exempt from errexit by design (so this alone would not need splitting),
	# but a producer feeding "| grep -q" under pipefail can still turn a real
	# match into a nonzero pipeline status if grep exits at the first line
	# before the producer is done writing (the same class left open at
	# recovery-tools.sh:85/114). Splitting also gives a TPM that is simply
	# unreachable its own message instead of folding it into "no bank".
	local caps
	if ! caps="$(tpm2_getcap pcrs)"; then
		printf 'Das TPM ist über TPM2TOOLS_TCTI=%s nicht erreichbar.\n' "$TPM2TOOLS_TCTI" >&2
		printf 'Prüfen: läuft der Simulator? tools/recovery-swtpm.sh reset main\n' >&2
		exit 1
	fi
	# tpm2_getcap pcrs always lists all four bank names - sha1, sha256, sha384
	# and sha512 - whether or not they are actually allocated, an empty one
	# printed as "sha384: [ ]" same as a populated "sha384: [ 0, 1, ... ]".
	# Measured: a simulator set up with --pcr-banks sha256 only still answers
	# with a literal "sha384" in its output, so "grep -q sha384" (the form in
	# this package's own brief, and the one already committed at
	# recovery-tools.sh:114) is true whether or not the bank exists - it only
	# reads correctly there because that probe always requests sha256,sha384
	# together first. This subcommand runs against whatever TPM is already up,
	# so the empty case is exactly the one it exists to catch: only a digit
	# right after the opening bracket on the sha384 line proves the bank is
	# really allocated, not just known by name.
	case "$caps" in
	*'sha384: [ '[0-9]*) printf 'PCR-Bank sha384: vorhanden\n' >&2 ;;
	*)
		printf 'Dieses TPM hat keine SHA-384-PCR-Bank.\n' >&2
		printf 'Neu einrichten mit: tools/recovery-swtpm.sh reset main\n' >&2
		exit 1
		;;
	esac
}

case "${1:-}" in
"") printf 'Umgebung: vollständig\n' >&2 ;;
pcr-bank) pcr_bank ;;
*) printf 'Aufruf: tools/recovery-env.sh [pcr-bank]\n' >&2; exit 1 ;;
esac
