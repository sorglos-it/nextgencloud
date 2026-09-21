#!/bin/sh
# Checks an NGC release the way the ulh scripts do (contracts/ulh.md).
# Usage: verify.sh <directory with release.json, release-key.pem and the files>
set -eu
cd "$1"
b64url() {
	s=$(tr -- '-_' '+/')
	case $(( ${#s} % 4 )) in 2) s="$s==" ;; 3) s="$s=" ;; esac
	printf '%s' "$s" | base64 -d
}
jq -r .payload release.json | b64url > payload.bin
jq -r .sig release.json | b64url > sig.bin
{ printf 'ngc/release/v1\0'; cat payload.bin; } > message.bin
openssl pkeyutl -verify -pubin -inkey release-key.pem -rawin -in message.bin -sigfile sig.bin >/dev/null
jq -r '.files[] | [.url, .sha256, .size] | @tsv' payload.bin |
while IFS="$(printf '\t')" read -r url sum size; do
	want=$(printf '%s' "$sum" | b64url | od -An -tx1 | tr -d ' \n')
	got=$(sha256sum "$url" | cut -d' ' -f1)
	[ "$want" = "$got" ] || { echo "checksum wrong: $url" >&2; exit 1; }
	[ "$(wc -c < "$url" | tr -d ' ')" = "$size" ] || { echo "size wrong: $url" >&2; exit 1; }
done
echo "release ok"
