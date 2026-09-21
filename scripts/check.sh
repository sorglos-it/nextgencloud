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
# trusttest holds fixed test keys: only test code may import it.
bad=$(go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./... | grep 'contracts/trust/trusttest' | grep -v '^github.com/sorglos-it/nextgencloud/contracts/trust/trusttest:' || true)
if [ -n "$bad" ]; then
	echo "trusttest imported outside tests:"
	echo "$bad"
	exit 1
fi
go test ./...
for arch in amd64 arm64; do
	CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build ./...
done
echo "check: ok"
