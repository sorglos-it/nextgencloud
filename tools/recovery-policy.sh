#!/usr/bin/env bash
# Builds the sealing policy and opens the sealed secret again. The policy gets
# the shape the specification demands: one branch for the normal boot, six for
# the pairs of four sticks.
#
# The rule this file lives by: the digest of a branch and the act of opening it
# come out of the same function, policy_branch, in the same order. Two separate
# command sequences drift apart, and the digest computed while opening could
# never meet the sealed one.
set -euo pipefail
# Without it a command that fails inside a $( ) is swallowed whole. Six of the
# seven scripts of this package carry it; recovery-setup.sh is the one that
# does not.
shopt -s inherit_errexit
cd "$(dirname "$0")/.."
tools/recovery-env.sh
say() { printf '%s\n' "$*" >&2; }

work="$(pwd)/dist/recovery"
w="$work/work"
mkdir -p "$w"

# This script speaks to the simulator of instance main and to nothing else, and
# that is settled BEFORE anything here can clean up. Measured on the test
# machine, which carries a real TPM of its own: with TPM2TOOLS_TCTI empty the
# tpm2 tools reach for /dev/tpmrm0 and /dev/tpm0 - and with the trap set ahead
# of this check, the way it used to be, three tpm2_flushcontext went out with
# that empty variable. Not reasoned out: a stub on the PATH wrote down what the
# tidy-up really addressed, after the mkdir above was made to fail. A value
# handed in from outside is no safer: pointed at a second simulator, this
# script extended ITS PCR 11 and made ITS 0x81000001 persistent, both measured.
# So the variable is checked, never trusted, and an environment aimed anywhere
# else ends the run right here - still before the trap exists, so nothing gets
# flushed on the way out either.
tcti_main="$(tools/recovery-swtpm.sh tcti main)"
if [ -n "${TPM2TOOLS_TCTI:-}" ] && [ "$TPM2TOOLS_TCTI" != "$tcti_main" ]; then
	say "TPM2TOOLS_TCTI zeigt nicht auf den Simulator der Instanz main."
	say "  gesetzt:  $TPM2TOOLS_TCTI"
	say "  erwartet: $tcti_main"
	say "Dieses Skript arbeitet nur mit dem Simulator. Abbruch."
	exit 1
fi
TPM2TOOLS_TCTI="$tcti_main"
export TPM2TOOLS_TCTI

# Cleanup only: a branch that failed on purpose leaves its session behind, and
# -t alone would not take that one with it. Aimed at our own simulator and no
# other - the kind of guard recovery-test.sh carries, for the same measured
# reason. Here the variable can no longer be empty when the trap exists, and
# the test is asked anyway: a guard that relies on the lines above it staying
# in that order is no guard.
trap 'if [ "${TPM2TOOLS_TCTI:-}" = "$tcti_main" ]; then
	for h in -t -l -s; do tpm2_flushcontext "$h" >/dev/null 2>&1 || :; done
fi' EXIT

# The branch of the specification covers PCR 0-7 and 11, and that has to go in
# as TWO assertions. Not a matter of taste: measured with tpm2-tools 5.7, one
# tpm2_policypcr -f over nine PCRs dies with "Could not hash pcr values", while
# eight go through - a value file carries at most TPM2_MAX_NUM_DIGESTS = 8
# digests. The same call without -f works at any count, because then the TPM
# reads its own PCRs instead, and that is exactly what must not happen here:
# sealing has to name the state of a branch, not take the one that happens to
# be there. Two assertions bind the same nine PCRs; the TPM checks each against
# its own values. What counts is that sealing and opening walk them in the same
# order, and policy_branch is what guarantees that.
pcrs_boot="sha384:0,1,2,3,4,5,6,7"
pcrs_uki="sha384:11"
handle=0x81000001
# This simulator holds three transient objects - measured, not guessed:
# tpm2_getcap says TPM2_PT_HR_TRANSIENT_AVAIL 0x3 and TPM2_PT_HR_TRANSIENT_MIN
# 0x3, and an earlier comment here claimed two. Every tpm2 call loads its
# context anew without giving the slot back, so three are gone in no time.
# Without this before each loading command the run dies at tpm2_create with
# "out of memory for object contexts" (0x902) - tried by turning flush into a
# no-op.
flush() { tpm2_flushcontext -t >/dev/null; }

# Two boot states, as section 6 of the specification prescribes. Nothing boots
# on the simulator, so both are TEST digests whose names say so.
uki_prod="$(printf 'TEST-UKI-PRODUKTIV' | openssl dgst -sha384 -binary | xxd -p -c 96 | tr -d '\n')"
uki_recovery="$(printf 'TEST-UKI-RECOVERY' | openssl dgst -sha384 -binary | xxd -p -c 96 | tr -d '\n')"

# PCR 11 after a single extend: SHA-384 over the zero start value followed by
# the image digest - exactly what the TPM computes on TPM2_PCR_Extend.
pcr11_after() {          # $1 = image digest, hex
	{ head -c 48 /dev/zero | xxd -p -c 96; printf '%s' "$1"; } \
		| tr -d '\n' | xxd -r -p | openssl dgst -sha384 -binary | xxd -p -c 96 | tr -d '\n'
}

# Writes the two value files a branch is measured against: the eight boot PCRs
# as the TPM has them now, and PCR 11 as it stands once the image of that
# branch has been measured into it.
pcr_files() {            # $1 = normal|recovery
	local digest
	case "$1" in
	normal) digest="$uki_prod" ;;
	recovery) digest="$uki_recovery" ;;
	*) say "Unbekannter PCR-Zustand: $1 (erlaubt: normal, recovery)"; exit 1 ;;
	esac
	tpm2_pcrread -o "$w/pcr-boot.bin" "$pcrs_boot" >/dev/null
	pcr11_after "$digest" | xxd -r -p > "$w/pcr-uki.bin"
	# Both files go into the TPM as a list of digests, and a wrong length there
	# builds a different policy instead of saying anything. wc, not stat:
	# recovery-env.sh names wc among the coreutils it counts on, stat nowhere.
	local n
	n="$(wc -c < "$w/pcr-boot.bin")"
	[ "$n" -eq 384 ] || { say "pcr-boot.bin hat $n statt 8x48 Byte"; exit 1; }
	n="$(wc -c < "$w/pcr-uki.bin")"
	[ "$n" -eq 48 ] || { say "pcr-uki.bin hat $n statt 48 Byte"; exit 1; }
}

# Brings the simulator into one of the two states. A PCR can only be extended,
# never set - tpm2_pcrreset 11 is answered with "bad locality" (0x907), tried
# on this very simulator - so the caller resets the simulator first.
set_pcr_state() {        # $1 = normal|recovery
	case "$1" in
	normal) tpm2_pcrextend "11:sha384=$uki_prod" ;;
	recovery) tpm2_pcrextend "11:sha384=$uki_recovery" ;;
	*) say "Unbekannter PCR-Zustand: $1 (erlaubt: normal, recovery)"; exit 1 ;;
	esac
	say "PCR 11 steht auf: $1"
}

# The primary key is made persistent once, and stays the same across calls:
# after two runs of seal, tpm2_getcap handles-persistent still lists 0x81000001
# exactly once. Handing it around as a context file instead would occupy one of
# the three transient slots in every single call.
primary() {
	if tpm2_getcap handles-persistent | grep -q "$handle"; then return 0; fi
	flush
	tpm2_createprimary -C o -g sha384 -G ecc384 -c "$w/primary.ctx" >/dev/null
	tpm2_evictcontrol -C o -c "$w/primary.ctx" "$handle" >/dev/null
	rm -f "$w/primary.ctx"
	flush
}

# The policy session - the one that carries the unsealed secret home - is
# salted against the primary key and uses parameter encryption, as section 6 of
# the specification demands, so the pattern moves to work package 3 unchanged.
# Proven, not assumed: tpm2_sessionconfig reports it as
# "continuesession|decrypt|encrypt".
#
# The trial session gets NEITHER, and that is not a choice. Measured with
# tpm2-tools 5.7: --tpmkey-context on a session without --policy-session is
# refused with "Trial sessions cannot be additionally used as encrypt/decrypt
# session", EXIT=2 - the tool turns the salt into encrypt/decrypt attributes,
# and a trial session may not carry those. It has nothing to protect either: it
# transports no secret, it only computes a digest that is public anyway.
new_session() {          # $1 = context file, $2 = trial|policy
	primary
	flush
	case "$2" in
	policy)
		tpm2_startauthsession --policy-session -S "$1" -g sha384 --tpmkey-context "$handle" >/dev/null
		tpm2_sessionconfig --enable-encrypt --enable-decrypt "$1" >/dev/null
		;;
	trial)
		tpm2_startauthsession -S "$1" -g sha384 >/dev/null
		;;
	# A typo must not quietly become a trial session: that one builds a digest
	# nothing can ever open.
	*) say "Unbekannte Sitzungsart: $2 (erlaubt: trial, policy)"; exit 1 ;;
	esac
}

# One branch, one command sequence, for both jobs: mode "trial" builds the
# digest, mode "real" opens. In this branch the mode makes no difference at all
# yet - what separates the two jobs is the session, not the commands - and it
# is named anyway because the sticks, the PIN and the counter join this function
# in tasks 7 and 8 and will need it. The closing PolicyOR stays outside it.
#
# That one function does both is what the whole file hangs on, and it is not a
# tidiness argument: tried on the simulator, with the two assertions below in
# the other order while opening, the PCR values all correct, tpm2_unseal
# returns nothing and the test says "Normalstart-Zweig öffnet nicht". A policy
# digest carries the order of its assertions.
policy_branch() {        # $1 = trial|real, $2 = branch, $3 = session context
	local branch="$2" session="$3"
	if [ "$branch" = pcr ]; then
		pcr_files normal
		tpm2_policypcr -S "$session" -l "$pcrs_boot" -f "$w/pcr-boot.bin" >/dev/null
		tpm2_policypcr -S "$session" -l "$pcrs_uki" -f "$w/pcr-uki.bin" >/dev/null
		return 0
	fi
	say "Unbekannter Zweig: $branch"
	exit 1
}

branch_digest() {        # $1 = branch
	new_session "$w/trial.ctx" trial
	policy_branch trial "$1" "$w/trial.ctx"
	tpm2_getpolicydigest -S "$w/trial.ctx" -o "$w/branch-$1.digest" >/dev/null
	tpm2_flushcontext "$w/trial.ctx" >/dev/null
}

seal() {                 # $1 = branch
	primary
	branch_digest "$1"
	printf 'TEST-DEK' > "$w/secret.bin"
	flush
	# noda is deliberately absent. It would switch off the dictionary attack
	# protection, and from task 7 on this object is what the platform PIN hangs
	# on - without that protection the PIN could be guessed without limit. There
	# is no PIN in this branch yet, so nothing here suffers from it either way.
	# userwithauth is absent too: only the policy may open this object, never a
	# plain password. sensitiveDataOrigin stays CLEAR because the data comes
	# from outside (-i). All three read back from the sealed object:
	# tpm2_readpublic says attributes fixedtpm|fixedparent|adminwithpolicy, raw
	# 0x92.
	tpm2_create -C "$handle" -g sha384 -u "$w/seal.pub" -r "$w/seal.priv" \
		-i "$w/secret.bin" -L "$w/branch-$1.digest" \
		-a "fixedtpm|fixedparent|adminwithpolicy" >/dev/null
	shred -u "$w/secret.bin"
	say "Versiegelt gegen Zweig $1"
}

open_branch() {          # $1 = branch
	primary
	flush
	tpm2_load -C "$handle" -u "$w/seal.pub" -r "$w/seal.priv" -c "$w/seal.ctx" >/dev/null
	new_session "$w/sess.ctx" policy
	policy_branch real "$1" "$w/sess.ctx"
	flush
	# The secret goes to stdout because the test compares it. The real version
	# in work package 6 uses tpm2_unseal -o and never lets it near a terminal.
	tpm2_unseal -c "$w/seal.ctx" -p "session:$w/sess.ctx"
	tpm2_flushcontext "$w/sess.ctx" >/dev/null
}

case "${1:-}" in
state) set_pcr_state "${2:-}" ;;
seal) seal "${2:-}" ;;
open)
	case "${2:-}" in
	pcr) open_branch pcr ;;
	*) say "Aufruf: tools/recovery-policy.sh open pcr"; exit 1 ;;
	esac ;;
*) say "Aufruf: tools/recovery-policy.sh state|seal|open"; exit 1 ;;
esac
