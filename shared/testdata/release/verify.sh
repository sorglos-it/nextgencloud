#!/bin/sh
# Checks the file of one NGC part in a release the way the ulh scripts do
# (shared/ulh.md), with the tools of a Linux server: sh, jq, openssl 3,
# base64, sha256sum, od, tr, cut, wc and mktemp.
#
# Usage: verify.sh <directory> <part> <arch>
#   <directory> holds release.json (a sign.Envelope), release-key.pem (the
#   release key as PEM "PUBLIC KEY") and the downloaded files.
#   <part> <arch> name the one file to check: ngc-web all, ngc-api amd64,
#   ngc-controller arm64 and so on (store.File). The signed release must list
#   exactly one entry for them.
#
# The entry names its file by the last segment of its url: "https://example.org
# /1.1.0/ngc-web-all.tar" and "ngc-web-all.tar" both mean the file
# ngc-web-all.tar in <directory>. That name must start with a letter or digit
# and hold only letters, digits and . _ -, or the check fails. Only this one
# file is checked, and only it may be installed afterwards: files of other
# parts and files the release does not list are ignored, whatever lies in
# <directory>.
#
# Prints "release ok" and exits with 0 only if everything holds. Work files
# go to a directory from mktemp -d that is removed on exit; nothing is written
# into <directory>.
set -eu
[ $# -eq 3 ] || { echo "usage: verify.sh <directory> <part> <arch>" >&2; exit 2; }
dir=$1 part=$2 arch=$3
LC_ALL=C
export LC_ALL
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
trap 'exit 1' HUP INT TERM
cd -- "$dir"

fail() {
	echo "release wrong: $1" >&2
	exit 1
}

# b64url decodes base64url without padding from stdin, as strictly as Go's
# enc.B64: only the letters A-Z a-z 0-9 - _, and only the one encoding of the
# bytes (the unused bits of the last letter are zero).
b64url() {
	s=$(cat)
	case $s in *[!A-Za-z0-9_-]*) return 1 ;; esac
	case $((${#s} % 4)) in
	0) pad= ;;
	2) pad='==' ;;
	3) pad='=' ;;
	*) return 1 ;;
	esac
	printf '%s' "$s$pad" | tr -- '-_' '+/' > "$work/b64.txt"
	base64 -d < "$work/b64.txt" > "$work/b64.bin" || return 1
	base64 -w0 < "$work/b64.bin" > "$work/b64.again" || return 1
	[ "$(tr -- '+/' '-_' < "$work/b64.again" | tr -d '=')" = "$s" ] || return 1
	cat "$work/b64.bin"
}

# The signature over "ngc/release/v1", a NUL byte and the payload.
jq -er '.payload | strings' release.json > "$work/payload.txt" || fail "no payload"
jq -er '.sig | strings' release.json > "$work/sig.txt" || fail "no signature"
b64url < "$work/payload.txt" > "$work/payload.json" || fail "payload not base64url"
b64url < "$work/sig.txt" > "$work/sig.bin" || fail "signature not base64url"
{ printf 'ngc/release/v1\0'; cat "$work/payload.json"; } > "$work/message.bin"
openssl pkeyutl -verify -pubin -inkey release-key.pem -rawin -in "$work/message.bin" \
	-sigfile "$work/sig.bin" > /dev/null 2>&1 || fail "signature does not verify"

# The one entry of <part> and <arch>, as url, sha256 and size separated by tabs.
jq -r --arg part "$part" --arg arch "$arch" '
	if (.files | type) != "array" then error("files is not a list") else . end
	| [.files[] | select(.part == $part and .arch == $arch)]
	| if length != 1 then error("not exactly one entry") else .[0] end
	| if (.url | type) == "string" and (.sha256 | type) == "string" and (.size | type) == "number"
	then [.url, .sha256, (.size | tostring)] | @tsv else error("entry of wrong types") end
' "$work/payload.json" > "$work/entry.tsv" || fail "no single valid entry for $part $arch"
tab=$(printf '\t')
IFS=$tab read -r url sum size < "$work/entry.tsv" || fail "entry unreadable"

name=${url##*/}
case $name in
[A-Za-z0-9]*) ;;
*) fail "file name not allowed: $name" ;;
esac
case $name in *[!A-Za-z0-9._-]*) fail "file name not allowed: $name" ;; esac
[ -f "$name" ] || fail "file missing: $name"

printf '%s' "$sum" | b64url > "$work/sum.bin" || fail "checksum not base64url"
want=$(od -An -tx1 < "$work/sum.bin" | tr -d ' \n')
[ ${#want} -eq 64 ] || fail "checksum not 32 bytes"
sha256sum -- "$name" > "$work/got.txt" || fail "file unreadable: $name"
got=$(cut -d' ' -f1 < "$work/got.txt")
[ "$want" = "$got" ] || fail "checksum wrong: $name"
[ "$(wc -c < "$name" | tr -d ' ')" = "$size" ] || fail "size wrong: $name"
echo "release ok"
