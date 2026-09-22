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
	CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build ./...
done
echo "check: ok"
