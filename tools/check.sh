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
# needs nothing but PHP. Without it the one drift it is written to catch would
# only show when somebody runs it by hand. Where there is no php, say so and
# carry on, like the Go tests that skip themselves without their tools.
if command -v php >/dev/null 2>&1; then
	php apps/web/tests/php/config-test.php
else
	echo "no php: skipping apps/web/tests/php/config-test.php"
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
