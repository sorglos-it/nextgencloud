#!/usr/bin/env bash
# Checks that the recovery test environment has everything it needs.
set -euo pipefail
shopt -s inherit_errexit
cd "$(dirname "$0")/.."
work="$(pwd)/dist/recovery"

# Every tool the package calls, including ones only used deep inside
# recovery-policy.sh. No "go": recovery-setup.sh never installs it, and
# this script runs only on the VM (measured: report has the details).
# ps is called by four of the scripts and is still not on the list: it comes out
# of the same package as pgrep - measured, dpkg -S names procps for both - so
# the pgrep entry already answers for it. grep and sed get no such stand-in and
# so are named below: dpkg -S puts each of them in a package of its own, called
# grep and sed, and the paragraph on coreutils does not cover them. Both are
# really called - grep further down in this script and in recovery-qemu.sh,
# recovery-setup.sh and recovery-tools.sh, sed in the latter two. Counted, not
# guessed: recovery-policy.sh used to call grep as well and no longer does.
# Tried: with one of them missing from the PATH the loop below names it and ends
# at EXIT=1, grep included - the loop runs before this script greps anything.
# Coreutils goes the other way round:
# shred and the timeout that bounds swtpm_ioctl in recovery-swtpm.sh are named
# below, and dpkg -S puts them in the same package as the twenty more this
# package relies on without naming them - basename, cat, chmod, cp, cut, date,
# dirname, head, ls, mkdir, mv, rm, seq, sha256sum, sleep, sort, tr, uname, wc,
# and the env that stands in all six shebangs. Tried, not assumed: with
# coreutils out of the PATH both named ones stand among the missing and the run
# ends at EXIT=1. Not before everything else, though - the cd in line 5 loses
# its dirname first and says so, and the script carries on from /. That stays
# harmless only because this check comes before the first use of $work; move it
# and the wrong working directory is suddenly real.
need="qemu-system-x86_64 swtpm swtpm_setup swtpm_ioctl softhsm2-util pkcs11-tool openssl xxd socat shred
	timeout pgrep grep sed tpm2 tpm2_startauthsession tpm2_sessionconfig tpm2_flushcontext tpm2_createprimary
	tpm2_evictcontrol tpm2_getpolicydigest tpm2_create tpm2_load tpm2_loadexternal tpm2_readpublic
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
	if [ ! -s "$file" ]; then missing="$missing $(basename "$file")"; fi
done

if [ -n "$missing" ]; then
	printf 'Es fehlt:%s\n' "$missing" >&2
	printf 'Einrichten mit: tools/recovery-setup.sh install\n' >&2
	exit 1
fi

# More than bare existence: nothing can trap SIGKILL or a lost VM, so a
# half-written file could still be sitting here. The trailing KVM line is
# the last thing a finished run writes.
if [ ! -s "$work/werkzeuge.txt" ] || ! grep -q '^KVM: ' "$work/werkzeuge.txt"; then
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
	# tpm2_getcap lists sha384 by name whether or not it is allocated - only a
	# populated list proves the bank exists. Captured first (not piped into
	# grep) so an unreachable TPM gets its own message instead of "no bank".
	local caps
	if ! caps="$(tpm2_getcap pcrs)"; then
		printf 'Das TPM ist über TPM2TOOLS_TCTI=%s nicht erreichbar.\n' "$TPM2TOOLS_TCTI" >&2
		printf 'Prüfen: läuft der Simulator? tools/recovery-swtpm.sh reset main\n' >&2
		exit 1
	fi
	# Isolated first because case's * crosses newlines (measured: without
	# this, a later bank's "23" can satisfy the pattern). Against the shapes
	# swtpm actually produces - all 24 PCRs or none - requiring 23 here is enough.
	local bank_line=""
	while IFS= read -r pcr_line; do
		case "$pcr_line" in *'sha384: ['*) bank_line="$pcr_line" ;; esac
	done <<< "$caps"
	case "$bank_line" in
	*'sha384: [ '[0-9]*' 23 ]'*) printf 'PCR-Bank sha384: vorhanden\n' >&2 ;;
	*)
		printf 'Dieses TPM hat keine SHA-384-PCR-Bank (oder eine andere tpm2-tools-Fassung schreibt das Format anders).\n' >&2
		printf 'Neu einrichten mit: tools/recovery-swtpm.sh reset main. Hilft das nicht: tpm2_getcap pcrs von Hand ansehen.\n' >&2
		exit 1
		;;
	esac
}

if [ "$#" -gt 1 ]; then
	printf 'Aufruf: tools/recovery-env.sh [pcr-bank]\n' >&2
	exit 1
fi
case "${1:-}" in
"") printf 'Umgebung: vollständig\n' >&2 ;;
pcr-bank) pcr_bank ;;
*) printf 'Aufruf: tools/recovery-env.sh [pcr-bank]\n' >&2; exit 1 ;;
esac
