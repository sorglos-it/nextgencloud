#!/bin/sh
# Test service for contract V3: echoes stdin to stdout, reports who asked on
# stderr and exits with the code given in NGC_PARAMS, e.g. {"exit":2}.
set -eu
printf '%s %s %s %s\n' "$NGC_APP" "$NGC_SERVICE" "$NGC_ACTION" "$NGC_USER" >&2
cat
code=$(printf '%s' "${NGC_PARAMS:-}" | sed -n 's/.*"exit":\([0-9][0-9]*\).*/\1/p')
exit "${code:-0}"
