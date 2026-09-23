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
# to catch never shows in a normal run, so look in the places php is really
# installed before giving up: NGC_PHP, the PATH, and the Windows development
# machine, where php is installed but not on the PATH.
php=""
if [ -n "${NGC_PHP:-}" ]; then
	if [ -x "$NGC_PHP" ]; then
		php="$NGC_PHP"
	else
		echo "NGC_PHP is set but not executable: $NGC_PHP"
		exit 1
	fi
elif command -v php >/dev/null 2>&1; then
	php=php
else
	for cand in /c/esp/php/php82/php.exe "C:/esp/php/php82/php.exe"; do
		if [ -x "$cand" ]; then
			php="$cand"
			break
		fi
	done
fi
if [ -n "$php" ]; then
	"$php" apps/web/tests/php/config-test.php
else
	echo "no php: skipping apps/web/tests/php/config-test.php (set NGC_PHP to its path)"
fi
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
