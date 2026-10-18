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

# The sticks of task 3. The plan puts these under a "pin_platform=" line, and
# that line does not exist yet - the platform PIN arrives with task 8 - so they
# sit here, ahead of their first use.
keys="$work/keys"
# STICK_PIN_FILE is why this is a variable at all: the second half of
# criterion 9 - three wrong STICK PINs lock the stick - needs a way to hand
# in a wrong one, and like PIN_FILE it goes in as a FILE, never as a value on
# a command line. Default unchanged.
pin_stick="${STICK_PIN_FILE:-$work/pin/stick.txt}"
# The platform PIN of task 8. The plan lists PIN_FILE among the interfaces of
# this script but never wires it anywhere - it writes $pin_platform, a variable
# this file does not have. Both are here: without the PIN_FILE arm the "wrong
# platform PIN" case would hand in the RIGHT PIN, the branch would open, and the
# case would report a failure for a branch that is in order.
pin_platform="${PIN_FILE:-$work/pin/platform.txt}"
# No length requirement here, and that is a decision, not an oversight. Stage
# test takes the PIN from a file (section 4 of the specification) and the test
# PIN is four characters. What keeps a short PIN from being guessed at this
# stage is the dictionary protection below - three attempts, 24 hours - which is
# why it sits in the same task. When work package 6 takes this PIN from a HUMAN,
# the requirement has to be stated in CHARACTERS and with a passphrase
# suggested: a minimum in DIGITS drives people into their date of birth, and
# eight digits is exactly ddmmyyyy.
module=/usr/lib/softhsm/libsofthsm2.so
# Exported, not handed to each call: softhsm2 reads it from the environment and
# has no option for it. recovery-env.sh has already seen that the module file
# is there, so a missing SoftHSM2 never gets this far.
export SOFTHSM2_CONF="$work/softhsm/softhsm2.conf"

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
#
# The saved sessions, -s, are the one exception: a run that really opened its
# branch hands that one session on to its caller and says so with uebergabe.
# open_branch is where the reason stands. Everything else keeps the old
# behaviour - every failed run included, and that is the case the -s was added
# for.
uebergabe=""
trap 'if [ "${TPM2TOOLS_TCTI:-}" = "$tcti_main" ]; then
	for h in -t -l; do tpm2_flushcontext "$h" >/dev/null 2>&1 || :; done
	if [ -z "$uebergabe" ]; then tpm2_flushcontext -s >/dev/null 2>&1 || :; fi
fi' EXIT

# The branch of the specification covers PCR 0-7 and 11, and that has to go in
# as TWO assertions. Not a matter of taste: measured with tpm2-tools 5.7, one
# tpm2_policypcr -f over nine PCRs dies with "Could not hash pcr values", while
# eight go through. The limit counts DIGESTS, not PCR indices - measured on this
# simulator, nine spread over two banks (sha256:0,1,2,3 + sha384:0,1,2,3,4) die
# the same way and eight over two banks pass: a value file carries at most
# TPM2_MAX_NUM_DIGESTS = 8. Two assertions bind the same nine PCRs; the TPM
# checks each against its own values. What counts is that sealing and opening
# walk them in the same order, and policy_branch is what guarantees that.
#
# What -f does here, stated against the code and not against the intention:
# pcr_files below calls tpm2_pcrread on EVERY call, while sealing exactly as
# while opening. So only PCR 11 is named in this branch - 0 to 7 are taken as
# they happen to stand at that moment, and -f then only hands the TPM back the
# values it just gave us. Bound they are either way: extend PCR 7 after sealing
# and tpm2_unseal answers 0x99D "a policy check failed", measured.
#
# That binding hangs on one line, and nothing but a case of its own notices it
# going: measured, with the first of the two tpm2_policypcr calls below deleted
# the test run stayed green from end to end, nine times ok and EXIT=0 - and the
# seal then opened with PCR 7 extended, which is the very thing the paragraph
# above promises it will not. recovery-test.sh now does it the other way round:
# seal, extend PCR 7, the branch has to stay shut.
#
# For PCR 11 it is the other way round, and that is what -f is really for here:
# pcr11_after computes the value, so the branch can be sealed against a PCR 11
# the TPM is not standing on at all. The test leans on exactly that - it seals
# while PCR 11 holds the recovery image and the policy names the productive one,
# and the branch stays shut.
#
# The consequence belongs to tasks 7 and 8, so it is written down here: sealing
# the recovery branch while the machine sits in the normal boot binds the NORMAL
# boot's PCR 0-7, and the recovery boot will then not open it. On real hardware
# 4 and 7 differ between the two branches; on the simulator nothing boots and
# both are zero, which is why nothing here ever notices. Whoever needs 0-7 named
# the way PCR 11 already is has to fill pcr-boot.bin from somewhere other than
# tpm2_pcrread.
pcrs_boot="sha384:0,1,2,3,4,5,6,7"
pcrs_uki="sha384:11"
# The branch list, in fixed order, written down exactly once. The order goes
# into the OR digest, so the sealing path and the opening path both read it from
# here - a second, differing list would break every branch.
#
# Seven branches, and since the decision of 10.10.2026 in section 6 of
# docs/recovery-modus.md they sit in TWO sealed objects: the pcr branch is the
# WHOLE policy of A-auto, which carries no authValue, and the six pairs are the
# branches of the PolicyOR in A-recovery, which does. One object with an
# authValue would hand the normal boot rubbish at exit code 0 - measured in task
# 8, the table stands at seal below. So pair_branches, not branches, is what the
# OR is built from; branches is the list of digests that have to exist.
branches="pcr 12 13 14 23 24 34"
pair_branches="12 13 14 23 24 34"
pair_list() {
	local list="" b
	for b in $pair_branches; do list="$list,$w/branch-$b.digest"; done
	printf '%s' "${list#,}"
}
handle=0x81000001
nv=0x1500016
# COUNTER_FILE lets the test hand in an old counter state without putting it on
# a command line.
counter_file="${COUNTER_FILE:-$w/counter.bin}"
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
	# Captured, not piped into grep. Two reasons, both about the same silence:
	# a TPM that does not answer fails with an empty list, and "not in the list"
	# sends the run down the branch below. What that costs was measured, not
	# reasoned, because the reasoning was wrong: the key is NOT replaced.
	# tpm2_createprimary goes through, and the tpm2_evictcontrol behind it is
	# refused by the TPM itself with 0x14C "NV Index or persistent object
	# already defined" - the persistent handles are the same list before and
	# after. What is lost is the message: the run ends on a TPM error about a
	# handle instead of on the sentence below, which names the real cause. And
	# grep -q leaves early, so a listing longer than the pipe buffer would put
	# SIGPIPE on the left-hand side of the pipe, which under pipefail makes the
	# whole line fail - the same wrong answer, only rarer. Out of reach today
	# (three persistent handles fit in one buffer many times over), and out of
	# reach for good this way.
	local handles
	if ! handles="$(tpm2_getcap handles-persistent)"; then
		say "Das TPM antwortet nicht auf die Frage nach seinen persistenten Handles"
		exit 1
	fi
	case "$handles" in *"$handle"*) return 0 ;; esac
	flush
	tpm2_createprimary -C o -g sha384 -G ecc384 -c "$w/primary.ctx" >/dev/null
	tpm2_evictcontrol -C o -c "$w/primary.ctx" "$handle" >/dev/null
	rm -f "$w/primary.ctx"
	flush
}

# The counter lives in the TPM and disappears with a reset, so every entry
# point makes sure it is there before it uses it.
#
# Owner auth is empty here, which is a TEST simplification. The plan's text
# says it is "noted as an open point in CHANGELOG.md" - checked, and it is not:
# CHANGELOG.md has no "Offen" section and names neither the owner auth nor this
# NV index. Section 13 of docs/recovery-modus.md does require such an entry, so
# it is reported to the coordinator rather than claimed here. A comment that
# points at a note nobody wrote is worse than no comment.
#
# Captured, not piped into grep, for the reason the primary function above
# spells out: a TPM that does not answer hands back an empty list, and "not in
# the list" would then quietly define the index a second time.
counter() {
	local indices
	if ! indices="$(tpm2_nvreadpublic)"; then
		say "Das TPM antwortet nicht auf die Frage nach seinen NV-Indizes"
		exit 1
	fi
	case "$indices" in
	*"$nv"*) ;;
	*)
		tpm2_nvdefine -C o "$nv" -a "nt=counter|ownerwrite|ownerread|authread" >/dev/null
		# A fresh counter has to be written once before it can be read.
		tpm2_nvincrement -C o "$nv" >/dev/null
		;;
	esac
	if [ -z "${COUNTER_FILE:-}" ]; then
		tpm2_nvread -C o "$nv" -s 8 -o "$counter_file" >/dev/null
	elif [ ! -s "$counter_file" ]; then
		# Without this a mistyped COUNTER_FILE would make tpm2_policynv build a
		# policy against an empty file instead of saying anything.
		say "COUNTER_FILE zeigt auf keine lesbare Zählerdatei: $counter_file"
		exit 1
	fi
}

policy_nv() {            # $1 = session
	tpm2_policynv -S "$1" -C o -i "$counter_file" "$nv" eq >/dev/null
}

# Section 6 of the specification: 3 attempts, then 24 hours locked. This
# replaces the noda attribute, which would have switched the protection off.
#
# Measured, because it decides whether one case can be falsely green through
# another: --setup-parameters does NOT reset the failed-attempt counter.
# TPM2_PT_LOCKOUT_COUNTER stood at 0x3 before and after such a call and only
# --clear-lockout put it back to 0x0. So resealing never quietly clears a
# lockout a case has just provoked.
lockout_setup() {
	tpm2_dictionarylockout --setup-parameters --max-tries=3 \
		--recovery-time=86400 --lockout-recovery-time=86400 >/dev/null
}

lockout_clear() {
	tpm2_dictionarylockout --clear-lockout >/dev/null
	say "Sperre aufgehoben"
}

# The policy session - the one that carries the unsealed secret home - is
# salted against the primary key and uses parameter encryption, as section 6 of
# the specification demands, so the pattern moves to work package 3 unchanged.
# The two halves hold each other up, measured: a policy session with encryption
# switched on but WITHOUT the salt has no session key, and tpm2_unseal answers
# 0x996 "unsupported symmetric algorithm or key size". Losing the salt therefore
# already shuts the branch; losing only the encryption would not.
#
# What checks the encryption, said against the code and not against the wish:
# open_branch leaves its session standing so recovery-test.sh can load the
# CONTEXT FILE with tpm2_sessionconfig. That reads the attributes out of a
# tpm2-tools file; the TPM only has to still hold the context for the file to
# load. It is therefore a statement this script makes about itself, and a weak
# one - measured at the state before the wire case existed, the encryption taken
# out AND one tpm2_sessionconfig --enable-encrypt --enable-decrypt put on the
# same file AFTER the tpm2_unseal kept the whole run green while the secret came
# out of the TPM in the clear. An earlier version wrote the sentence into a file
# of its own; that was worse, but not different in kind. Today the same change
# ends the run at the wire case, with eleven ok behind it - this check goes
# green through it, exactly as it did then.
# The wire is looked at instead, and it is worth saying exactly how far that
# reaches: recovery-test.sh has swtpm write the traffic down (--log level=20)
# and then SEARCHES that recording for the eight bytes, over the whole run and
# in both directions. Honest run: 2 hits, both in what the tools SENT during the
# Create of the sealing, and that is the ONE named place - tpm2_create -i runs
# without any session at all. Every hit has to sit there and there may be no
# others; on top of that the set of command codes has to be the twenty-five of an
# honest run. Both are searches, and a search that finds nothing has NOT shown
# that the secret stayed in. Measured ways past it, green at 14 x ok against
# the state before ledger 2 existed: the plaintext split over two requests, or
# sent base64-encoded. They are listed at that case. So what the wire case
# gives this script is not the property but a much narrower sentence -
# whatever it does with its sessions, the recording of a run must carry no
# further hit of those eight bytes and exactly the twenty-five known commands - a
# twenty-sixth added or one of the twenty-five missing both end it the same way -
# and that is still more than anything here can say about itself.
#
# The trial session gets NEITHER, and that is not a choice. Measured with
# tpm2-tools 5.7: --tpmkey-context on a session without --policy-session is
# refused with "Trial sessions cannot be additionally used as encrypt/decrypt
# session", EXIT=2. The refusal is that tool's own check and nothing deeper -
# a trial session CAN carry those attributes: tpm2_sessionconfig
# --enable-encrypt --enable-decrypt on one succeeds (EXIT=0, read back as
# continuesession|decrypt|encrypt) and the TPM still takes tpm2_policypcr on it
# afterwards. Only salting it at the start is refused. It has nothing to protect
# either: it transports no secret, it only computes a digest that is public
# anyway.
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

load_stick() {           # $1 = stick number
	if [ ! -f "$w/stick$1.ctx" ]; then
		flush
		# The name algorithm matters: the key's Name goes into the PolicySigned
		# digest, so a SHA-256 name would silently build a different branch.
		tpm2_loadexternal -C o -G ecc -g sha384 -u "$keys/stick$1.pem" \
			-c "$w/stick$1.ctx" >/dev/null
	fi
}

sign_stick() {           # $1 = stick number, $2 = data file, $3 = signature file
	# SoftHSM2 only offers CKM_ECDSA, the raw operation, so the digest is made
	# outside - exactly how a real HSM stick works as well.
	#
	# The PIN stands in the command line, and that breaks the project rule that
	# no secret goes where the process list shows it. It stays because measured,
	# not assumed: pkcs11-tool offers --pin and nothing else. Its own help says
	# "Supply User PIN on the command line (if used in scripts: careful!)", and
	# fed on stdin instead it dies with "error: util_getpass error", EXIT=1 - it
	# reads the terminal, not stdin. This is a TEST stick with a test PIN that
	# docs/recovery-modus.md section 13 allows to lie in the repo, so nothing
	# secret is exposed here. What must not happen is that work package 2 copies
	# the pattern to a real stick: there the PIN comes from the keyboard and the
	# access runs through cryptoki, not through this tool.
	openssl dgst -sha384 -binary -out "$2.hash" "$2"
	pkcs11-tool --module "$module" --token-label "TEST-STICK-$1" --login \
		--pin "$(cat "$pin_stick")" --sign --mechanism ECDSA \
		--signature-format openssl --input-file "$2.hash" --output-file "$3" >/dev/null
}

# A trial session does not check the signature, the file only has to parse. One
# throwaway signature from a key nobody keeps serves every branch.
dummy_sig() {
	if [ ! -f "$w/dummy.sig" ]; then
		openssl ecparam -name secp384r1 -genkey -noout -out "$w/dummy.key"
		printf 'TEST-DUMMY' | openssl dgst -sha384 -sign "$w/dummy.key" -out "$w/dummy.sig"
		shred -u "$w/dummy.key"
	fi
}

# Hands out the bytes the stick has to sign: 48 byte nonce of this session plus
# 4 byte expiration. A recording of an earlier run does not fit a new session.
policysigned_raw() {     # $1 = session, $2 = key context, $3 = output file
	flush
	tpm2_policysigned -S "$1" -c "$2" -g sha384 -t 0 -x --raw-data="$3" >/dev/null
}

policysigned_real() {    # $1 = session, $2 = key context, $3 = signature file
	flush
	tpm2_policysigned -S "$1" -c "$2" -g sha384 -t 0 -x -s "$3" -f ecdsa >/dev/null
}

policysigned_trial() {   # $1 = session, $2 = key context
	dummy_sig
	flush
	# No -x here: the nonce belongs to the real session only, and the digest
	# does not depend on it.
	tpm2_policysigned -S "$1" -c "$2" -g sha384 -t 0 -s "$w/dummy.sig" -f ecdsa >/dev/null
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
	local branch="$2" session="$3" n
	if [ "$branch" = pcr ]; then
		pcr_files normal
		tpm2_policypcr -S "$session" -l "$pcrs_boot" -f "$w/pcr-boot.bin" >/dev/null
		tpm2_policypcr -S "$session" -l "$pcrs_uki" -f "$w/pcr-uki.bin" >/dev/null
		return 0
	fi
	# The plan stops at [1-5][1-5]. That alone lets "11" and "51" through, and
	# both build a branch nothing will ever open - 11 asserts the same stick
	# twice, 51 the pair in the wrong order and so a different digest than the
	# "15" the open path can ask for. Since seal does NOT run through
	# branch_of_pair, this is the only gate on that way in, so it asks the same
	# two questions: two different sticks, smaller one first.
	case "$branch" in
	[1-5][1-5]) ;;
	*) say "Unbekannter Zweig: $branch"; exit 1 ;;
	esac
	if [ "${branch%?}" -ge "${branch#?}" ]; then
		say "Zweig $branch: erst der kleinere Stick, und zwei verschiedene. Erlaubt: 12 13 14 15 23 24 25 34 35 45"
		exit 1
	fi
	# The plan writes one tpm2_policypcr over a single "pcr_file recovery". That
	# shape is gone: task 6 measured that a value file carries at most eight
	# digests, so the nine PCRs of the specification go in as TWO assertions,
	# and the function that fills them is pcr_files. Same two calls as the pcr
	# branch above, in the same order - only against the RECOVERY image, because
	# this branch is the recovery one. Then the sticks, so the order of
	# assertions in the digest is PCR, PCR, stick, stick on both ways through.
	pcr_files recovery
	tpm2_policypcr -S "$session" -l "$pcrs_boot" -f "$w/pcr-boot.bin" >/dev/null
	tpm2_policypcr -S "$session" -l "$pcrs_uki" -f "$w/pcr-uki.bin" >/dev/null
	for n in "${branch%?}" "${branch#?}"; do
		load_stick "$n"
		if [ "$1" = trial ]; then
			policysigned_trial "$session" "$w/stick$n.ctx"
		else
			policysigned_raw "$session" "$w/stick$n.ctx" "$w/tosign-$n.bin"
			sign_stick "$n" "$w/tosign-$n.bin" "$w/stick$n.sig"
			policysigned_real "$session" "$w/stick$n.ctx" "$w/stick$n.sig"
		fi
	done
	# Platform PIN and counter, in this order, on both ways through - a policy
	# digest carries the order of its assertions. The pcr branch returns above
	# and gets neither: it has to run at boot without a person.
	tpm2_policyauthvalue -S "$session" >/dev/null
	policy_nv "$session"
}

branch_digest() {        # $1 = branch
	new_session "$w/trial.ctx" trial
	policy_branch trial "$1" "$w/trial.ctx"
	tpm2_getpolicydigest -S "$w/trial.ctx" -o "$w/branch-$1.digest" >/dev/null
	tpm2_flushcontext "$w/trial.ctx" >/dev/null
}

# Builds the two policies of section 6. All seven branch digests are written,
# because the acceptance counts them; A-auto then IS the pcr digest, and
# A-recovery is the OR over the six pair digests.
policy_digest() {
	local b
	for b in $branches; do branch_digest "$b"; done
	# A-auto needs no PolicyOR: one branch, and its digest is the policy. An OR
	# over a single branch would be a different digest for no gain.
	cp "$w/branch-pcr.digest" "$w/policy-auto.digest"
	new_session "$w/trial.ctx" trial
	tpm2_policyor -S "$w/trial.ctx" -l "sha384:$(pair_list)" >/dev/null
	tpm2_getpolicydigest -S "$w/trial.ctx" -o "$w/policy-recovery.digest" >/dev/null
	tpm2_flushcontext "$w/trial.ctx" >/dev/null
	# Counted here as well, not only in the test: a branch that silently went
	# missing would make the policy smaller than the specification asks for, and
	# every case that opens a branch it still has would stay green.
	local n
	n="$(ls "$w"/branch-*.digest | wc -l)"
	[ "$n" -eq 7 ] || { say "Es liegen $n Zweig-Digests statt sieben vor dem Versiegeln"; exit 1; }
}

seal() {
	# Both objects go before either is made, and both have to be there
	# afterwards. That is the one guard section 6 of docs/recovery-modus.md asks
	# for and task 9 did not have: a reseal that renewed only ONE of them would
	# leave the other standing from an earlier seal, with an older counter or
	# older PCR values, and nobody would notice until the next boot or not
	# until a recovery. Today there is no code path that seals only one - this
	# makes sure there cannot be a quiet one later either.
	rm -f "$w/auto.pub" "$w/auto.priv" "$w/recovery.pub" "$w/recovery.priv"
	primary
	counter
	lockout_setup
	policy_digest
	printf 'TEST-DEK' > "$w/secret.bin"
	flush
	# noda is deliberately absent. It would switch off the dictionary attack
	# protection, and from task 7 on this object is what the platform PIN hangs
	# on - without that protection the PIN could be guessed without limit. There
	# is no PIN in this branch yet, so nothing here suffers from it either way.
	# userwithauth is absent too: only the policy may open this object, never a
	# plain password. sensitiveDataOrigin stays CLEAR because the data comes
	# from outside (-i) - and that third one is not this line's doing at all:
	# measured, the same call with sensitivedataorigin added is refused by the
	# TPM with 0x2C2 "inconsistent attributes". Of the three promises that
	# 0x92 covers, two can be broken here and the third the TPM keeps by itself.
	#
	# userwithauth is the one that matters, and it was measured, not reasoned:
	# with it in this line the object reads back as 0xd2 - and then
	# "tpm2_unseal -c <ctx>" hands out the secret with no policy session at all,
	# PCR 11 deliberately standing on the wrong branch. The whole policy is then
	# decoration. In work package 6 the real DEK hangs on this object. That is
	# why recovery-test.sh reads these attributes back off the sealed object and
	# insists on 0x92: this line here cannot vouch for itself.
	#
	# The -g below is read back with them, and for the same reason: it is the
	# name algorithm of the sealed object, and the policy digest that guards it
	# is a digest of that algorithm. Measured, sha384 turned into sha1 all
	# through this file seals and opens exactly as before and the run stays
	# green - with the whole branch then resting on SHA-1. tpm2_readpublic
	# prints it immediately above the attributes, in the very output the test
	# already reads.
	# -p file: and not -p <value>: the PIN must never stand in a command line,
	# where /proc/<pid>/cmdline shows it to every user on the machine. Measured
	# that the form carries: create with -p "file:..." ends at EXIT=0 and the
	# object then opens with the same file and refuses a wrong one with 0x98E.
	#
	# And the PIN goes on the object ONLY for a stick branch. The plan puts it on
	# every seal and says the normal boot still opens "because the normal boot
	# branch carries no PIN". That is true of the POLICY and wrong about the
	# SESSION, and the way it fails is the worst kind: EXIT=0 with the wrong
	# bytes. Measured, one variable at a time, same object and same policy:
	#
	#   authValue  Sitzung verschl.  PIN übergeben  Ergebnis
	#   nein       ja                nein           TEST-DEK
	#   ja         ja                nein           EXIT=0, 89f8c4cad807344d
	#   ja         nein              nein           TEST-DEK
	#   ja         ja                ja             TEST-DEK
	#   ja         nein              ja             TEST-DEK
	#
	# The session key of a salted, parameter-encrypting session is derived from
	# the authValue of the entity being authorised. Hand tpm2_unseal only
	# "session:" and it computes that key with an EMPTY authValue, decrypts the
	# answer with the wrong key and prints rubbish without any error. So an
	# authValue the pcr branch never checks would still have to be handed in -
	# or the encryption task 6 paid seven rounds for would have to go. Neither.
	# The pcr branch simply gets no authValue, which is what the plan's own
	# interface list promises.
	#
	# That is why Slot A is sealed TWICE, as section 6 of the specification now
	# prescribes: A-auto without an authValue, so the normal boot opens without a
	# person, and A-recovery with one. The same secret goes into both - it is the
	# same DEK in work package 6 - and the price is written down in that section:
	# both have to be resealed on a PCR change, A-recovery on every counter step
	# as well, and whoever forgets one notices at the next boot or not until a
	# recovery. bump below reseals both, and the test opens both afterwards.
	tpm2_create -C "$handle" -g sha384 -u "$w/auto.pub" -r "$w/auto.priv" \
		-i "$w/secret.bin" -L "$w/policy-auto.digest" \
		-a "fixedtpm|fixedparent|adminwithpolicy" >/dev/null
	flush
	tpm2_create -C "$handle" -g sha384 -u "$w/recovery.pub" -r "$w/recovery.priv" \
		-i "$w/secret.bin" -L "$w/policy-recovery.digest" -p "file:$pin_platform" \
		-a "fixedtpm|fixedparent|adminwithpolicy" >/dev/null
	shred -u "$w/secret.bin"
	# The other half of the guard above. Counted, not trusted: four files, and
	# every one of them from THIS seal, because the first thing this function
	# did was take the old ones away.
	local f
	for f in auto.pub auto.priv recovery.pub recovery.priv; do
		[ -s "$w/$f" ] || { say "Nach dem Versiegeln fehlt $f - die beiden Objekte sind auseinandergelaufen"; exit 1; }
	done
	say "Versiegelt gegen sieben Zweige in zwei Objekten"
}

open_branch() {          # $1 = branch
	primary
	counter
	flush
	# Which of the two objects a branch belongs to is decided here and nowhere
	# else: the pcr branch is A-auto, every pair branch is A-recovery.
	local objekt
	if [ "$1" = pcr ]; then objekt=auto; else objekt=recovery; fi
	if [ ! -s "$w/$objekt.pub" ]; then
		say "Das Objekt A-$objekt ist nicht versiegelt - zuerst tools/recovery-policy.sh seal"
		exit 1
	fi
	tpm2_load -C "$handle" -u "$w/$objekt.pub" -r "$w/$objekt.priv" -c "$w/seal.ctx" >/dev/null
	new_session "$w/sess.ctx" policy
	policy_branch real "$1" "$w/sess.ctx"
	# The same OR as at sealing time, from the same list - and only for
	# A-recovery. A-auto has one branch and no OR, so adding one here would
	# build a digest its object was never sealed against.
	if [ "$objekt" = recovery ]; then
		flush
		tpm2_policyor -S "$w/sess.ctx" -l "sha384:$(pair_list)" >/dev/null
	fi
	flush
	# The secret goes to stdout because the test compares it. The real version
	# in work package 6 uses tpm2_unseal -o and never lets it near a terminal.
	# The normal boot branch carries no PIN: it has to run at boot without a
	# person. Every stick branch does, through the file - never on a command
	# line, where /proc/<pid>/cmdline would show it to the whole machine.
	if [ "$1" = pcr ]; then
		tpm2_unseal -c "$w/seal.ctx" -p "session:$w/sess.ctx"
	else
		tpm2_unseal -c "$w/seal.ctx" -p "session:$w/sess.ctx+file:$pin_platform"
	fi
	# And the session stays, together with its context file. What that is worth
	# is written out at new_session above, and it is less than this comment used
	# to claim: recovery-test.sh reads the CONTEXT FILE with tpm2_sessionconfig,
	# the TPM only has to still hold the context for the file to load, and the
	# parameter encryption is NOT checked there - the wire case at the foot of
	# recovery-test.sh does that. What the handover is still good for is the
	# plain forgetting of the switch, with a clearer message than the wire gives.
	# The old line here flushed the session, and then not even that was possible.
	# It costs nothing, and that was measured too, not assumed: the TPM empties
	# a policy session's digest the moment it has authorised something. The
	# handed-over session reads back as Session-Digest: 00...00, and unsealing
	# with it a second time gets nothing out. Whoever takes it flushes it
	# (tpm2_flushcontext "$w/sess.ctx"); a reset of the simulator takes it
	# anyway, and a run that does NOT get here loses it to the trap.
	uebergabe=ja
}

bump() {
	primary
	counter
	tpm2_nvincrement -C o "$nv" >/dev/null
	tpm2_nvread -C o "$nv" -s 8 -o "$w/counter.bin" >/dev/null
	# Both objects go, and both come back: there is only one policy now, so
	# there is nothing left to remember about "which branch". A-recovery must be
	# resealed because its branches carry PolicyNV; A-auto is resealed with it so
	# the two can never drift apart, which is the price section 6 names.
	rm -f "$w/auto.pub" "$w/auto.priv" "$w/recovery.pub" "$w/recovery.priv" \
		"$w/seal.ctx" "$w"/branch-*.digest "$w"/policy-*.digest
	seal
	say "Zähler erhöht, beide Objekte neu versiegelt"
}

# Proves a stick can sign at all and that its signature verifies against its own
# public key. It runs before the foreign-stick case, which would otherwise pass
# for the wrong reason: a token whose key is simply broken never signs, the
# PolicySigned fails, nothing opens, and the case would report "the foreign
# stick does not open" about a stick that cannot sign anything.
checkstick() {           # $1 = stick number
	printf 'TEST-STICK-PRUEFUNG' > "$w/probe.bin"
	sign_stick "$1" "$w/probe.bin" "$w/probe.sig"
	# Verified outside the TPM, against the .pem the sticks were read out into,
	# because this says something about the STICK and must not depend on a
	# policy, a session or a sealed object.
	openssl dgst -sha384 -verify "$keys/stick$1.pem" -signature "$w/probe.sig" \
		"$w/probe.bin" >/dev/null
	say "Stick $1 signiert gültig"
}

# Records one honest run and plays that signature back into a FRESH session. The
# new session hands out a new nonce, so the recorded signature signs the wrong
# bytes - that is the whole replay argument of the branch, and until now only
# the criterion behind it was measured (52 byte, not all zero, a new session
# gives other bytes), never the case itself.
#
# Exit codes, and the test asks for 1 exactly, never "anything but 0":
#   1  the TPM refused - the case passed
#   2  the honest run already did not open - the case proves nothing
#   0  the TPM accepted the replay
replay() {               # $1 = branch
	local i="${1%?}" got=""
	if ! got="$(open_branch "$1")"; then
		say "Der ehrliche Vorlauf ist abgebrochen — der Replay-Fall beweist so nichts."
		return 2
	fi
	if [ "$got" != TEST-DEK ]; then
		say "Der ehrliche Vorlauf öffnet nicht — der Replay-Fall beweist so nichts."
		return 2
	fi
	# open_branch hands its session on (uebergabe) and the honest run above ran
	# in a command substitution, so that variable never reached this shell. The
	# session is taken away here by hand: the next new_session writes the same
	# context FILE, and the TPM would keep the old session in a slot nothing
	# can reach any more.
	tpm2_flushcontext -s >/dev/null 2>&1 || :
	[ -s "$w/stick$i.sig" ] \
		|| { say "Vom Stick $i liegt keine mitgeschnittene Signatur vor — nichts zum Einspielen."; return 2; }
	cp "$w/stick$i.sig" "$w/replay.sig"
	primary
	counter
	new_session "$w/sess.ctx" policy
	# The PCR assertions first, in the same two steps and the same order as
	# policy_branch - a case that is refused before the meant line measures the
	# earlier wall. Here they MUST go through, so what the PolicySigned below
	# answers is the recorded signature and nothing else.
	pcr_files recovery
	tpm2_policypcr -S "$w/sess.ctx" -l "$pcrs_boot" -f "$w/pcr-boot.bin" >/dev/null
	tpm2_policypcr -S "$w/sess.ctx" -l "$pcrs_uki" -f "$w/pcr-uki.bin" >/dev/null
	load_stick "$i"
	say "Spiele eine mitgeschnittene Signatur in eine neue Sitzung ein"
	if policysigned_real "$w/sess.ctx" "$w/stick$i.ctx" "$w/replay.sig"; then
		say "Das TPM hat die mitgeschnittene Signatur angenommen."
		tpm2_flushcontext "$w/sess.ctx" >/dev/null
		return 0
	fi
	tpm2_flushcontext "$w/sess.ctx" >/dev/null
	return 1
}

# Only the FIRST stick of the pair signs - correctly, with its real key, in this
# very session. The branch stays half built, so the closing PolicyOR finds no
# match among the six sealed digests. Same exit codes as replay.
half() {                 # $1 = branch
	local i="${1%?}" got=""
	if ! got="$(open_branch "$1")"; then
		say "Der ehrliche Vorlauf ist abgebrochen — der halbe Zweig beweist so nichts."
		return 2
	fi
	if [ "$got" != TEST-DEK ]; then
		say "Der ehrliche Vorlauf öffnet nicht — der halbe Zweig beweist so nichts."
		return 2
	fi
	tpm2_flushcontext -s >/dev/null 2>&1 || :
	# Printed before anything is decided, and the test looks for this very
	# sentence: a case whose exit code is 1 because the script does not even
	# KNOW the subcommand would otherwise pass. Measured - see the report.
	say "Baue den halben Zweig: nur Stick $i signiert"
	primary
	counter
	flush
	# A-recovery, the object the pair branches belong to - never seal.pub, which
	# is no file this script writes.
	tpm2_load -C "$handle" -u "$w/recovery.pub" -r "$w/recovery.priv" -c "$w/seal.ctx" >/dev/null
	new_session "$w/sess.ctx" policy
	pcr_files recovery
	tpm2_policypcr -S "$w/sess.ctx" -l "$pcrs_boot" -f "$w/pcr-boot.bin" >/dev/null
	tpm2_policypcr -S "$w/sess.ctx" -l "$pcrs_uki" -f "$w/pcr-uki.bin" >/dev/null
	load_stick "$i"
	# A real signature over the bytes of THIS session: the stick does everything
	# right, there is just one of them.
	policysigned_raw "$w/sess.ctx" "$w/stick$i.ctx" "$w/tosign-$i.bin"
	sign_stick "$i" "$w/tosign-$i.bin" "$w/stick$i.sig"
	policysigned_real "$w/sess.ctx" "$w/stick$i.ctx" "$w/stick$i.sig"
	tpm2_policyauthvalue -S "$w/sess.ctx" >/dev/null
	policy_nv "$w/sess.ctx"
	flush
	# The OR is asked in an if, not left to errexit: the TPM refuses a PolicyOR
	# whose current digest is in none of the branches, and that refusal IS the
	# wall this case means. Left bare it would end the script with a message
	# from tpm2_policyor that names no case, and the unseal below - the worse
	# outcome, the one that must never open - would never be tried at all.
	if ! tpm2_policyor -S "$w/sess.ctx" -l "sha384:$(pair_list)" >/dev/null; then
		say "Der abschließende PolicyOR passt auf den halben Zweig nicht — genau das soll er."
		tpm2_flushcontext "$w/sess.ctx" >/dev/null
		return 1
	fi
	say "Der PolicyOR hat den halben Zweig angenommen — jetzt entscheidet das Entsiegeln."
	flush
	if tpm2_unseal -c "$w/seal.ctx" -p "session:$w/sess.ctx+file:$pin_platform" >/dev/null; then
		say "Der halbe Zweig hat geöffnet."
		tpm2_flushcontext "$w/sess.ctx" >/dev/null
		return 0
	fi
	tpm2_flushcontext "$w/sess.ctx" >/dev/null
	return 1
}

# Sets $branch. Deliberately not a command substitution: an exit inside $( )
# would only end the subshell and the script would carry on with nothing.
branch_of_pair() {       # $1, $2 = stick numbers
	local i="${1:-}" j="${2:-}"
	case "$i" in 1|2|3|4|5) ;; *) say "Stick-Nummer 1 bis 5 erwartet, nicht: $i"; exit 1 ;; esac
	case "$j" in 1|2|3|4|5) ;; *) say "Stick-Nummer 1 bis 5 erwartet, nicht: $j"; exit 1 ;; esac
	if [ "$i" -ge "$j" ]; then say "Erst der kleinere Stick, also: open pair $j $i"; exit 1; fi
	branch="$i$j"
}

case "${1:-}" in
state) set_pcr_state "${2:-}" ;;
seal) seal ;;
bump) bump ;;
lockout-clear) lockout_clear ;;
replay) branch_of_pair "${2:-}" "${3:-}"; replay "$branch" ;;
half) branch_of_pair "${2:-}" "${3:-}"; half "$branch" ;;
checkstick)
	case "${2:-}" in
	1|2|3|4|5) checkstick "$2" ;;
	*) say "Stick-Nummer 1 bis 5 erwartet, nicht: ${2:-}"; exit 1 ;;
	esac ;;
open)
	case "${2:-}" in
	pcr) open_branch pcr ;;
	pair) branch_of_pair "${3:-}" "${4:-}"; open_branch "$branch" ;;
	*) say "Aufruf: tools/recovery-policy.sh open pcr|pair <i> <j>"; exit 1 ;;
	esac ;;
*) say "Aufruf: tools/recovery-policy.sh state|seal|open|bump|replay|half|checkstick|lockout-clear"; exit 1 ;;
esac
