#!/usr/bin/env bash
# The same flow as tools/siegel-tpm.sh, but the key lives in a PKCS#11 stick
# (SoftHSM2) instead of in a file.
#
# Why both: SoftHSM2 offers only raw ECDSA, so the hash is made outside and the
# stick signs the digest - exactly how a real HSM stick works. A policy that
# works with a key in a file and not with a key in a stick would be a policy
# nobody can use in the field.
#
# Rescued on 24.10.2026 off the first single machine, where it lived in
# ~/smoke/stick.sh and was in no repository.
#
#   bash tools/siegel-stick.sh [arbeitsverzeichnis]
#
# Run tools/siegel-tpm.sh FIRST. That is not a convention, it is a dependency:
# this script seals under the persistent primary at 0x81000001, and that handle
# is made there. The original did not say so and failed with a bare TPM error
# when it was missing; here it is checked and named.
#
#   bash tools/recovery-setup.sh install        # once per machine, needs sudo
#   bash tools/recovery-swtpm.sh start main
#   bash tools/siegel-tpm.sh
#   bash tools/siegel-stick.sh
set -eu

cd "$(dirname "$0")/.."
repo="$(pwd)"
work="${1:-$repo/dist/recovery/siegel-stick}"

# The module path is where Debian puts it. Named as a variable because another
# distribution puts it elsewhere, and a wrong path here looks like a broken
# stick rather than a missing file.
M="${SOFTHSM_MODULE:-/usr/lib/softhsm/libsofthsm2.so}"

for t in tpm2_startauthsession tpm2_policysigned tpm2_create tpm2_load tpm2_unseal \
	tpm2_loadexternal tpm2_readpublic tpm2_flushcontext softhsm2-util pkcs11-tool \
	openssl xxd; do
	command -v "$t" >/dev/null ||
		{ echo "fehlt: $t - 'bash tools/recovery-setup.sh install' legt die Werkzeuge an" >&2; exit 2; }
done
[ -f "$M" ] || { echo "kein SoftHSM-Modul unter $M - SOFTHSM_MODULE setzen" >&2; exit 2; }

TPM2TOOLS_TCTI="$(bash tools/recovery-swtpm.sh tcti main)"
export TPM2TOOLS_TCTI
sock="${TPM2TOOLS_TCTI#swtpm:path=}"
[ -S "$sock" ] ||
	{ echo "kein Simulator an $sock - 'bash tools/recovery-swtpm.sh start main'" >&2; exit 2; }

P=0x81000001
# The dependency, checked instead of assumed.
tpm2_readpublic -c "$P" >/dev/null 2>&1 ||
	{ echo "kein dauerhafter Primary an $P - erst 'bash tools/siegel-tpm.sh' laufen lassen" >&2; exit 2; }

rm -rf -- "$work"
mkdir -p -- "$work/softhsm/tokens"
cd "$work"

# Its own token directory under the work directory, so a run never touches a
# token store someone else set up. The original wrote into $HOME/smoke.
export SOFTHSM2_CONF="$work/softhsm/softhsm2.conf"
printf 'directories.tokendir = %s/softhsm/tokens\nobjectstore.backend = file\nlog.level = ERROR\n' \
	"$work" > "$SOFTHSM2_CONF"

# The PINs of a throwaway token in a throwaway directory. They are in the
# script because they protect nothing: the token is made here and removed with
# the work directory. A real stick never gets its PIN from a file in a repo.
SO_PIN="${SIEGEL_SO_PIN:-87654321}"
PIN="${SIEGEL_PIN:-12345678}"

f() { tpm2_flushcontext -t >/dev/null 2>&1 || true; }
sign() { # <eingabe> <ausgabe>
	openssl dgst -sha384 -binary -out "$1.hash" "$1"
	pkcs11-tool --module "$M" --token-label TEST-STICK-1 --login --pin "$PIN" --sign \
		--mechanism ECDSA --signature-format openssl \
		--input-file "$1.hash" --output-file "$2" >/dev/null 2>&1
}
trap 'f' EXIT

(exit 7) || probe=$?
[ "${probe:-0}" = 7 ] || { echo "ABBRUCH: die Sonde meldet ${probe:-0} statt 7" >&2; exit 1; }
echo "0 Sonde exit7 = 7"

echo "1 Token und Schluessel im Stick"
if ! softhsm2-util --show-slots 2>/dev/null | grep -q TEST-STICK-1; then
	softhsm2-util --init-token --free --label TEST-STICK-1 --so-pin "$SO_PIN" --pin "$PIN" >/dev/null
	pkcs11-tool --module "$M" --token-label TEST-STICK-1 --login --pin "$PIN" \
		--keypairgen --key-type EC:secp384r1 --label stick1 --id 01 >/dev/null
fi
pkcs11-tool --module "$M" --token-label TEST-STICK-1 --read-object --type pubkey --label stick1 \
	--output-file stick1.der >/dev/null 2>&1
openssl ec -pubin -inform DER -in stick1.der -pubout -out stick1.pem 2>/dev/null
echo "   Kurve: $(openssl ec -pubin -in stick1.pem -noout -text 2>/dev/null | grep -m1 NIST)"

echo "2 Policy-Digest"
f; tpm2_loadexternal -C o -G ecc -g sha384 -u stick1.pem -c stick1.ctx >/dev/null
head -c 64 /dev/urandom > dummy.bin
sign dummy.bin dummy.sig
# A signature the stick did not make is an empty file, and tpm2_policysigned
# then reports something that reads like a policy problem.
[ -s dummy.sig ] || { echo "   der Stick hat nicht unterschrieben (leere Signatur)" >&2; exit 1; }
f; tpm2_startauthsession -g sha384 -S trial.ctx >/dev/null
f; tpm2_policysigned -S trial.ctx -c stick1.ctx -g sha384 -s dummy.sig -f ecdsa -t 0 -L policy1.digest >/dev/null
f; tpm2_flushcontext trial.ctx >/dev/null
echo "   Digest: $(xxd -p -c 48 policy1.digest | cut -c1-32)..."

echo "3 Versiegeln"
printf 'TEST-DEK' > secret.bin
f; tpm2_create -C "$P" -g sha384 -u seal1.pub -r seal1.priv -i secret.bin -L policy1.digest \
	-a "fixedtpm|fixedparent|adminwithpolicy" >/dev/null
echo "   versiegelt"

echo "4 Oeffnen: der Stick signiert die TPM-Nonce"
f; tpm2_startauthsession --policy-session -g sha384 -S sess.ctx >/dev/null
f; tpm2_policysigned -S sess.ctx -c stick1.ctx -g sha384 -t 0 -x --raw-data=to_sign.bin >/dev/null
sign to_sign.bin stick1.sig
[ -s stick1.sig ] || { echo "   der Stick hat die Nonce nicht unterschrieben" >&2; exit 1; }
f; tpm2_policysigned -S sess.ctx -c stick1.ctx -g sha384 -t 0 -x -s stick1.sig -f ecdsa >/dev/null
f; tpm2_load -C "$P" -u seal1.pub -r seal1.priv -c seal1.ctx >/dev/null
f; out=$(tpm2_unseal -c seal1.ctx -p "session:sess.ctx")
f; tpm2_flushcontext sess.ctx >/dev/null
[ "$out" = "TEST-DEK" ] && echo "   GEOEFFNET: $out" || { echo "   FEHLER: $out"; exit 1; }

echo "5 Replay mit der Stick-Signatur"
f; tpm2_startauthsession --policy-session -g sha384 -S replay.ctx >/dev/null
f
if tpm2_policysigned -S replay.ctx -c stick1.ctx -g sha384 -t 0 -x -s stick1.sig -f ecdsa >/dev/null 2>&1; then
	echo "   LUECKE: alte Signatur angenommen"; exit 1
fi
echo "   abgelehnt, wie es sein muss"
f; tpm2_flushcontext replay.ctx >/dev/null 2>&1 || true
echo "STICK-WEG GRUEN ($work)"
