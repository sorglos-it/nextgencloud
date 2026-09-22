#!/bin/sh
# Accepts a host as NGC's ulh scripts may write it into answer files and
# configurations: a DNS name in lower case or an IPv4 address, the subset of
# trust.ValidHost without IPv6. Prints nothing; exit 0 accepted, 1 refused,
# 2 wrong call.
# Usage: host.sh <host>
set -u
# The character ranges below are the C ranges, whatever locale the admin
# runs: in another one [a-z] and grep's classes can take other letters.
LC_ALL=C
export LC_ALL
[ $# -eq 1 ] || { echo "usage: host.sh <host>" >&2; exit 2; }
h=$1
# Only these characters, so no line break, space or quote gets further.
case $h in
'' | *[!a-z0-9.-]*) exit 1 ;;
esac
[ ${#h} -le 253 ] || exit 1
label='[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?'
printf '%s\n' "$h" | grep -Eqx "($label\.)*$label" || exit 1
case ${h##*.} in
*[!0-9]*) exit 0 ;; # a DNS name: its last label is not all digits
esac
octet='(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])'
printf '%s\n' "$h" | grep -Eqx "($octet\.){3}$octet"
