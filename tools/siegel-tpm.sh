#!/usr/bin/env bash
# Seal a secret against a signed policy, open it with a signature over the
# TPM's OWN nonce, and show that replaying that signature fails.
#
# That last line is the whole point. A policy signature that can be replayed is
# a key that can be stolen once and used forever, and nothing else in the
# recovery strand measures it: recovery-test.sh checks the policy that NGC
# builds, this checks that the TPM refuses a used signature in a fresh session.
#
# Rescued on 24.10.2026 off the first single machine, where it lived in
# ~/smoke/run.sh and was in no repository.
#
#   bash tools/siegel-tpm.sh [arbeitsverzeichnis]
#
# Needs the TPM simulator. It does NOT start it, on purpose - the instance
# "main" is shared with recovery-test.sh and starting a second one behind its
# back would move the PCRs it seals against:
#
#   bash tools/recovery-setup.sh install        # once per machine, needs sudo
#   bash tools/recovery-swtpm.sh start main
#
# The original took the socket path and the work directory from $HOME/smoke and
# spoke to the simulator directly. Both now come from recovery-swtpm.sh, so
# there is one place that knows where the simulator lives.
set -eu

cd "$(dirname "$0")/.."
repo="$(pwd)"
work="${1:-$repo/dist/recovery/siegel}"

for t in tpm2_createprimary tpm2_startauthsession tpm2_policysigned tpm2_create \
	tpm2_load tpm2_unseal tpm2_loadexternal tpm2_evictcontrol tpm2_flushcontext \
	openssl xxd; do
	command -v "$t" >/dev/null ||
		{ echo "fehlt: $t - 'bash tools/recovery-setup.sh install' legt die Werkzeuge an" >&2; exit 2; }
done

# The socket has to be there BEFORE the first command. Without this check every
# tpm2 call fails with a connection error, and a reader takes that for a
# finding about the policy.
TPM2TOOLS_TCTI="$(bash tools/recovery-swtpm.sh tcti main)"
export TPM2TOOLS_TCTI
sock="${TPM2TOOLS_TCTI#swtpm:path=}"
[ -S "$sock" ] ||
	{ echo "kein Simulator an $sock - 'bash tools/recovery-swtpm.sh start main'" >&2; exit 2; }

# Its own directory, and emptied at the start. The original did "rm -f *.ctx
# *.pem ..." in a directory it shared with everything else; one wrong $HOME and
# that takes someone's keys with it.
rm -rf -- "$work"
mkdir -p -- "$work"
cd "$work"

P=0x81000001
f() { tpm2_flushcontext -t >/dev/null 2>&1 || true; }
trap 'f' EXIT

# Leak check first: a run that does not report 7 as 7 makes every number below
# it worthless. Taken from the sibling script release-mutanten.sh, where it is
# the first thing too.
(exit 7) || probe=$?
[ "${probe:-0}" = 7 ] || { echo "ABBRUCH: die Sonde meldet ${probe:-0} statt 7" >&2; exit 1; }
echo "0 Sonde exit7 = 7"

echo "1 Primary dauerhaft ablegen"
f; tpm2_evictcontrol -C o -c "$P" >/dev/null 2>&1 || true
f; tpm2_createprimary -C o -g sha384 -G ecc384 -c primary.ctx >/dev/null
f; tpm2_evictcontrol -C o -c primary.ctx "$P" >/dev/null
f; echo "   Handle $P"

echo "2 Stick-Schluessel und Policy-Digest"
openssl ecparam -name secp384r1 -genkey -noout -out stick.key
chmod 0600 stick.key
openssl ec -in stick.key -pubout -out stick.pem 2>/dev/null
f; tpm2_loadexternal -C o -G ecc -g sha384 -u stick.pem -c stick.ctx >/dev/null
head -c 64 /dev/urandom > dummy.bin
openssl dgst -sha384 -sign stick.key -out dummy.sig dummy.bin
f; tpm2_startauthsession -g sha384 -S trial.ctx >/dev/null
f; tpm2_policysigned -S trial.ctx -c stick.ctx -g sha384 -s dummy.sig -f ecdsa -t 0 -L policy.digest >/dev/null
f; tpm2_flushcontext trial.ctx >/dev/null
echo "   Digest: $(xxd -p -c 48 policy.digest | cut -c1-32)..."

echo "3 Versiegeln"
printf 'TEST-DEK' > secret.bin
f; tpm2_create -C "$P" -g sha384 -u seal.pub -r seal.priv -i secret.bin -L policy.digest \
	-a "fixedtpm|fixedparent|adminwithpolicy" >/dev/null
echo "   versiegelt"

echo "4 Oeffnen mit Signatur ueber die TPM-Nonce"
f; tpm2_startauthsession --policy-session -g sha384 -S sess.ctx >/dev/null
f; tpm2_policysigned -S sess.ctx -c stick.ctx -g sha384 -t 0 -x --raw-data=to_sign.bin >/dev/null
echo "   zu signieren: $(wc -c < to_sign.bin) Byte"
openssl dgst -sha384 -sign stick.key -out real.sig to_sign.bin
f; tpm2_policysigned -S sess.ctx -c stick.ctx -g sha384 -t 0 -x -s real.sig -f ecdsa >/dev/null
f; tpm2_load -C "$P" -u seal.pub -r seal.priv -c seal.ctx >/dev/null
f; out=$(tpm2_unseal -c seal.ctx -p "session:sess.ctx")
f; tpm2_flushcontext sess.ctx >/dev/null
[ "$out" = "TEST-DEK" ] && echo "   GEOEFFNET: $out" || { echo "   FEHLER: $out"; exit 1; }

echo "5 Replay: dieselbe Signatur in einer neuen Sitzung"
f; tpm2_startauthsession --policy-session -g sha384 -S replay.ctx >/dev/null
f
if tpm2_policysigned -S replay.ctx -c stick.ctx -g sha384 -t 0 -x -s real.sig -f ecdsa >/dev/null 2>&1; then
	echo "   LUECKE: die alte Signatur wurde angenommen"
	exit 1
fi
echo "   abgelehnt, wie es sein muss"
f; tpm2_flushcontext replay.ctx >/dev/null 2>&1 || true
echo "ALLES GRUEN ($work)"
