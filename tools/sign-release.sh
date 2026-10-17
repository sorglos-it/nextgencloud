#!/bin/sh
# sign-release.sh - signs a packed NGC release. RUN THIS ONLY ON THE MACHINE
# THAT HOLDS THE PRIVATE RELEASE KEY.
#
# Usage: sign-release.sh <directory> <key.pem>
#   <directory>  what pack-release.sh wrote: release.payload.json and the files
#   <key.pem>    the private Ed25519 release key, PEM. NOT in the repository,
#                NOT in <directory>, and nothing of it is ever printed.
#
# It writes release.json (a sign.Envelope) and release-key.pem (the PUBLIC key)
# beside the files, then checks its own work with
# shared/testdata/release/verify.sh - the very script the installation at the
# user runs - and only then exits 0.
#
# WHY THIS IS A SEPARATE SCRIPT AND A SEPARATE STEP. The packer runs wherever
# somebody builds; this runs in one place, with a human present. Nothing joins
# them: there is no build that signs, so there is no build that can carry the
# key away. The price in one sentence: every release needs a person at that one
# machine, and a release cannot be made without them.
#
# The message a signature covers is sign.Message(sign.PurposeRelease, payload)
# = "ngc/" || "release/v1" || 0x00 || payload (shared/sign/sign.go:96, :23).
# -rawin is not optional: Ed25519 hashes inside, and openssl dgst would hash
# first and produce a signature verify.sh rejects.
set -eu
# No tracing, whatever the caller exported: a trace of this script would print
# the key path and, with a different shell, more than that.
set +x
unset PS4 2>/dev/null || true
LC_ALL=C
export LC_ALL

self=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
verify="$self/../shared/testdata/release/verify.sh"

die() {
	echo "sign-release: $1" >&2
	exit 1
}

[ $# -eq 2 ] || die "usage: sign-release.sh <directory> <key.pem>"
dir=$1
key=$2
[ -d "$dir" ] || die "no such directory: $dir"
[ -f "$dir/release.payload.json" ] || die "release.payload.json is missing in $dir"
[ -f "$key" ] || die "no such key file: $key"
[ -f "$verify" ] || die "verify.sh is missing: $verify"

dir=$(CDPATH= cd -- "$dir" && pwd)
# The key must not live in the directory that gets published.
case $(CDPATH= cd -- "$(dirname -- "$key")" && pwd) in
"$dir" | "$dir"/*) die "the key lies in the release directory; move it out first" ;;
esac

work=$(mktemp -d) || die "mktemp failed"
trap 'rm -rf -- "$work"' EXIT
trap 'exit 1' HUP INT TERM

# The public key as the PEM the ulh scripts carry, and its key ID.
openssl pkey -in "$key" -pubout -out "$work/release-key.pem" 2>/dev/null ||
	die "the key could not be read as an Ed25519 key"
openssl pkey -pubin -in "$work/release-key.pem" -outform DER -out "$work/pub.der" 2>/dev/null ||
	die "the public key could not be written"
[ "$(wc -c <"$work/pub.der" | tr -d ' ')" = 44 ] ||
	die "that is no Ed25519 key (the public key is not 44 bytes of DER)"
# The last 32 bytes are the key; the 12 in front are Ed25519's fixed header.
dd if="$work/pub.der" of="$work/pub.raw" bs=1 skip=12 status=none ||
	die "the public key could not be cut"
# sign.KeyID (shared/sign/sign.go:106): base64url of the first 16 bytes
# (sign.KeyIDSize) of the SHA-256 of the public key. As bytes, not through hex.
openssl dgst -sha256 -binary -- "$work/pub.raw" >"$work/pub.sha" ||
	die "the key ID could not be built"
dd if="$work/pub.sha" of="$work/kid.bin" bs=1 count=16 status=none ||
	die "the key ID could not be cut"
kid=$(base64 -w0 <"$work/kid.bin" | tr '+/' '-_' | tr -d '=')
[ ${#kid} -eq 22 ] || die "the key ID is not 22 characters"

# THIS IS THE LINE THAT CHANGES WITH CONTRACT V1.2, POINT 19. Today the
# signature covers sign.Message(...) itself, all 193 bytes of it. The release key
# that now exists is an Ed25519 key on hardware, and it was measured to sign only
# lengths that match a known hash - 32 and 64 bytes go through, 30 does not - so
# it CANNOT sign these 193 bytes. V1.2 point 19 therefore has the contract sign
# sha512(Message(...)) instead, which is 64 bytes.
#
# NOTHING IS CHANGED HERE YET, on purpose: shared/ is frozen and the contract cut
# comes separately. When it lands, a sha512 goes between this message and the
# pkeyutl call below - and openssl must still be given the DIGEST as raw input
# (-rawin), never "openssl dgst -sign", which would hash a second time.
{ printf 'ngc/release/v1\0'; cat "$dir/release.payload.json"; } >"$work/message.bin"
openssl pkeyutl -sign -inkey "$key" -rawin -in "$work/message.bin" \
	-out "$work/sig.bin" 2>/dev/null || die "signing failed"
[ "$(wc -c <"$work/sig.bin" | tr -d ' ')" = 64 ] ||
	die "the signature is not 64 bytes; that is no Ed25519 signature"

b64() { base64 -w0 <"$1" | tr '+/' '-_' | tr -d '='; }
printf '{"payload":"%s","kid":"%s","sig":"%s"}' \
	"$(b64 "$dir/release.payload.json")" "$kid" "$(b64 "$work/sig.bin")" \
	>"$work/release.json"

cp -f -- "$work/release.json" "$dir/release.json"
cp -f -- "$work/release-key.pem" "$dir/release-key.pem"

# Check the own work with the script the installation runs, not with a second
# idea of what it should do.
sh "$verify" "$dir" ngc-web all >"$work/verify.out" 2>&1 || {
	cat "$work/verify.out" >&2
	rm -f -- "$dir/release.json"
	die "the release just signed does not verify; release.json was removed again"
}

echo "release signed, key $kid"
sed -n '1p' "$work/verify.out"
echo "  $dir/release.json"
echo "  $dir/release-key.pem   <- this PEM goes into the ulh script, nothing else does"
