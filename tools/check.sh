#!/usr/bin/env bash
# Runs the checks every commit must pass.
set -euo pipefail
cd "$(dirname "$0")/.."
files=$(git ls-files '*.go' ':!:vendor/*')
if [ -n "$files" ] && [ -n "$(gofmt -l $files)" ]; then
	gofmt -l $files
	echo "gofmt needed"
	exit 1
fi
go vet ./...
go test ./...
# The PHP side of the web interface checks itself against shared/testdata/ and
# needs nothing but PHP. Skipping it quietly would mean the drift it is written
# to catch never shows in a normal run, and a path to one machine's php has no
# place in a shared script, so php is looked up in four steps:
#   1. NGC_PHP, 2. tools/local.env (not in git, one NGC_PHP=<path> line),
#   3. php from the PATH, 4. stop and say so.
# apps/web/tests/playwright.config.js looks it up in the same four steps for
# the test server; the path itself is written down only in tools/local.env.
named="${NGC_PHP:-}"
where="NGC_PHP"
if [ -z "$named" ] && [ -f tools/local.env ]; then
	. ./tools/local.env
	named="${NGC_PHP:-}"
	where="tools/local.env"
fi
if [ -n "$named" ]; then
	if [ ! -x "$named" ]; then
		echo "$where names a php that cannot be run: $named"
		exit 1
	fi
	php="$named"
elif command -v php >/dev/null 2>&1; then
	php=php
else
	echo "no php: set NGC_PHP to its path, or write NGC_PHP=<path> into tools/local.env"
	exit 1
fi
"$php" apps/web/tests/php/config-test.php
for arch in amd64 arm64; do
	# trusttest holds fixed test keys: only test code may import it. The list
	# is taken as each target is built (cgo off too), so files built only
	# there count as well.
	bad=$(CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./... | grep 'shared/trust/trusttest' | grep -v '^github.com/sorglos-it/nextgencloud/shared/trust/trusttest:' || true)
	if [ -n "$bad" ]; then
		echo "trusttest imported outside tests (linux/$arch):"
		echo "$bad"
		exit 1
	fi
	# -tags modules builds tools/modules.go too, so the vendored modules that no
	# part imports yet are proven to build for both targets.
	CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -tags modules ./...
done
echo "check: ok"
