#!/usr/bin/env bash
# Proves that the installed tools are the ones this package was written
# against. Every command sequence here comes from a run on this machine
# (~/smoke/run.sh and ~/smoke/stick.sh); a different release may spell an
# option differently, and a wrong guess would weaken the policy without anyone
# noticing. So the assumptions get proven, not trusted.
set -euo pipefail
cd "$(dirname "$0")/.."
# Cleanup only, never a check: this is the one line in the package allowed to
# swallow an error. -t alone would leave sessions behind, so -l and -s go with
# it - a probe that died mid-way must not cost the next run its session slots.
trap 'for h in -t -l -s; do tpm2_flushcontext "$h" >/dev/null 2>&1 || :; done' EXIT

work="$(pwd)/dist/recovery"
probe="$work/work/probe"
state="$work/tpm/probe"
txt="$work/werkzeuge.txt"
handle=0x81000002        # the probe uses its own handle, not the policy's

say() { printf '%s\n' "$*" >&2; }
stop_and_ask() {
	say ""
	say "STOPP: $1"
	say "Ein Ersatzweg würde ein Sicherheitsmerkmal aus docs/recovery-modus.md schwächen."
	say "Bitte den Koordinator fragen und erst danach weitermachen."
	exit 1
}
# swtpm holds only two transient objects and every tpm2 call loads its context
# afresh without giving the slot back.
flush() { tpm2_flushcontext -t >/dev/null; }

rm -rf "$probe" "$state"
mkdir -p "$probe" "$state" "$work/softhsm/tokens" "$work/keys" "$work/pin"
chmod 700 "$work/pin"
cat > "$work/softhsm/softhsm2.conf" <<CONF
directories.tokendir = $work/softhsm/tokens
objectstore.backend = file
log.level = ERROR
CONF

# TEST PINs in files with mode 0600: a PIN on the command line is readable in
# /proc/<pid>/cmdline by every user on the machine. Stage "test" of the
# specification prescribes "PIN source: file" anyway.
for entry in stick:12345678 so:87654321 platform:1234; do
	f="$work/pin/${entry%%:*}.txt"
	if [ ! -f "$f" ]; then
		printf '%s' "${entry##*:}" > "$f"
		chmod 600 "$f"
	fi
done
pin_platform="$work/pin/platform.txt"

# --- 1. versions ----------------------------------------------------------
version="$(tpm2 --version | sed -n 's/.*version="\([^"]*\)".*/\1/p')"
if [ -z "$version" ]; then
	stop_and_ask "Die Fassung der tpm2-tools ist nicht feststellbar."
fi
major="${version%%.*}"
rest="${version#*.}"
minor="${rest%%.*}"
if [ "$major" -lt 5 ] || { [ "$major" -eq 5 ] && [ "$minor" -lt 7 ]; }; then
	stop_and_ask "tpm2-tools $version ist älter als 5.7, gegen die dieser Plan geschrieben ist."
fi

# --- 2. commands and options ----------------------------------------------

for cmd in tpm2_getpolicydigest tpm2_evictcontrol tpm2_policysigned tpm2_policynv \
	tpm2_policypcr tpm2_policyauthvalue tpm2_policyor tpm2_sessionconfig \
	tpm2_startauthsession tpm2_dictionarylockout tpm2_loadexternal tpm2_verifysignature; do
	if ! command -v "$cmd" >/dev/null; then
		stop_and_ask "Der Befehl $cmd fehlt. Ohne ihn ist die Policy nicht baubar."
	fi
done

# --help of some tools exits non-zero; the grep below is the check that counts.
help_of() {              # $1 = command
	if "$1" --help > "$probe/help.txt" 2>&1; then :; fi
	cat "$probe/help.txt"
}
need_opt() {             # $1 = command, rest = long options that must exist
	local cmd="$1" opt page
	shift
	page="$(help_of "$cmd")"
	for opt in "$@"; do
		if ! printf '%s\n' "$page" | grep -q -- "$opt"; then
			stop_and_ask "$cmd kennt $opt nicht."
		fi
	done
}
need_opt tpm2_policysigned --nonce-tpm --expiration --raw-data
need_opt tpm2_startauthsession --policy-session --tpmkey-context
need_opt tpm2_sessionconfig --enable-encrypt --enable-decrypt
need_opt tpm2_policypcr --pcr-list --pcr
need_opt tpm2_policynv --input
need_opt tpm2_create --sealing-input --policy
need_opt tpm2_dictionarylockout --max-tries --recovery-time --lockout-recovery-time

# --- 3. simulator with a SHA-384 PCR bank ---------------------------------
# The probe brings its own instance: tools/recovery-swtpm.sh only exists from
# task 4 on, and the probe must not depend on what it measures.
swtpm_setup --tpm2 --tpm-state "$state" --createek --pcr-banks sha256,sha384 --overwrite
swtpm socket --tpm2 --tpmstate "dir=$state" --server "type=unixio,path=$state/sock" \
	--ctrl "type=unixio,path=$state/sock.ctrl" --flags not-need-init,startup-clear \
	--daemon --pid "file=$state/swtpm.pid"
for _ in $(seq 50); do
	if [ -S "$state/sock" ]; then break; fi
	sleep 0.1
done
if [ ! -S "$state/sock" ]; then
	say "swtpm ist nicht gestartet. Prüfen: swtpm --version und die Rechte auf $state"
	exit 1
fi
export TPM2TOOLS_TCTI="swtpm:path=$state/sock"
if ! tpm2_getcap pcrs | grep -q sha384; then
	stop_and_ask "Der Simulator hat keine SHA-384-PCR-Bank trotz --pcr-banks sha256,sha384."
fi

# --- 4. the one unmeasured point: does the PIN come out of a file? --------
flush
tpm2_createprimary -C o -g sha384 -G ecc384 -c "$probe/primary.ctx" >/dev/null
tpm2_evictcontrol -C o -c "$probe/primary.ctx" "$handle" >/dev/null
flush
# Trial session stays unsalted: tpm2-tools 5.7 refuses --tpmkey-context here
# ("Trial sessions cannot be additionally used as encrypt/decrypt session",
# measured twice on this VM). Nothing protective is lost - a trial session
# only computes a digest that is public anyway and never carries a secret
# over the bus. Decision Thomas, 2026-09-24; the real session below still
# salts and encrypts, because that one does carry the platform PIN.
tpm2_startauthsession -S "$probe/trial.ctx" -g sha384 >/dev/null
tpm2_policyauthvalue -S "$probe/trial.ctx" >/dev/null
tpm2_getpolicydigest -S "$probe/trial.ctx" -o "$probe/authvalue.digest" >/dev/null
tpm2_flushcontext "$probe/trial.ctx" >/dev/null
printf 'TEST-PROBE-SECRET' > "$probe/secret.bin"
flush
# sensitiveDataOrigin stays CLEAR because the data comes from outside (-i).
tpm2_create -C "$handle" -g sha384 -u "$probe/p.pub" -r "$probe/p.priv" \
	-i "$probe/secret.bin" -L "$probe/authvalue.digest" -p "file:$pin_platform" \
	-a "fixedtpm|fixedparent|adminwithpolicy" >/dev/null
shred -u "$probe/secret.bin"
flush
tpm2_load -C "$handle" -u "$probe/p.pub" -r "$probe/p.priv" -c "$probe/p.ctx" >/dev/null
tpm2_startauthsession --policy-session -S "$probe/sess.ctx" -g sha384 \
	--tpmkey-context "$handle" >/dev/null
tpm2_sessionconfig --enable-encrypt --enable-decrypt "$probe/sess.ctx" >/dev/null
tpm2_policyauthvalue -S "$probe/sess.ctx" >/dev/null
flush
got="$(tpm2_unseal -c "$probe/p.ctx" -p "session:$probe/sess.ctx+file:$pin_platform")"
tpm2_flushcontext "$probe/sess.ctx" >/dev/null
if [ "$got" != TEST-PROBE-SECRET ]; then
	stop_and_ask "tpm2-tools nimmt die PIN nicht aus einer Datei (session:…+file:…). Die PIN kommt nicht auf die Kommandozeile."
fi
tpm2_evictcontrol -C o -c "$handle" >/dev/null

if [ -c /dev/kvm ]; then
	kvm=vorhanden
else
	kvm="nicht vorhanden, QEMU laeuft emuliert"
fi
{
	printf 'Werkzeuge der Recovery-Testumgebung\n'
	printf 'Geprüft am %s auf %s\n\n' "$(date -I)" "$(uname -srm)"
	printf 'tpm2-tools:  %s\n' "$version"
	printf 'swtpm:       %s\n' "$(swtpm --version | head -1)"
	printf 'SoftHSM2:    %s\n' "$(softhsm2-util --version)"
	printf 'QEMU:        %s\n' "$(qemu-system-x86_64 --version | head -1)"
	printf 'OpenSSL:     %s\n' "$(openssl version)"
	printf '\nGeprüft und in Ordnung:\n'
	printf '  tpm2_policysigned kennt --nonce-tpm, --expiration, --raw-data\n'
	printf '  tpm2_startauthsession kennt --policy-session und --tpmkey-context\n'
	printf '  tpm2_sessionconfig kennt --enable-encrypt und --enable-decrypt\n'
	printf '  tpm2_policypcr kennt --pcr-list und --pcr\n'
	printf '  tpm2_policynv kennt --input\n'
	printf '  tpm2_dictionarylockout kennt --max-tries und die beiden Sperrzeiten\n'
	printf '  PCR-Bank sha384 vorhanden\n'
	printf '  Plattform-PIN aus Datei (session:…+file:…) funktioniert\n'
	printf '\nKVM: %s\n' "$kvm"
} > "$txt"

kill "$(cat "$state/swtpm.pid")"
say "Werkzeuge geprüft: $txt"
