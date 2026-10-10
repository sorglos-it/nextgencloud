#!/usr/bin/env bash
# Runs the checks every commit must pass.
set -euo pipefail
cd "$(dirname "$0")/.."
# The go tool stamps the build with VCS data and runs git for it, which fails the
# same way in a worktree outside the repo ("error obtaining VCS status"). The check
# produces no artefact, so the stamp is not needed here. Measured 10.10.2026.
export GOFLAGS="${GOFLAGS:+$GOFLAGS }-buildvcs=false"
# safe.directory for THIS call only: in a worktree outside the repo git refuses
# with "dubious ownership" and exit 128, and a caller that pipes the script reads
# the pipe's 0 instead. Measured 10.10.2026. Never set this globally.
files=$(git -c safe.directory="$PWD" ls-files '*.go' ':!:vendor/*')
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
	# -tags modules builds tools/modules.go too, so the vendored modules that no
	# part imports yet are proven to build for both targets.
	CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -tags modules ./...
done
echo "check: ok"
