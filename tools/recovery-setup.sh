#!/usr/bin/env bash
# Sets up the recovery test environment. "install" needs sudo and the network
# and runs once per machine; everything else has to work offline, so the parts
# are separate subcommands - a test run must never call apt-get.
set -euo pipefail
cd "$(dirname "$0")/.."

install() {
	# xxd is its own package on Debian 13, and the test environment falls over
	# without it: every PCR value passes through it.
	sudo apt-get update
	# procps brings pgrep, which the simulator control uses. No Go here: the
	# repository and tools/check.sh stay on the machine that commits.
	sudo apt-get install -y \
		qemu-system-x86 ovmf swtpm swtpm-tools softhsm2 opensc tpm2-tools \
		openssl xxd socat procps
	printf 'Pakete installiert. Weiter mit: tools/recovery-tools.sh\n' >&2
}

prepare() {
	# Stub: fails on purpose until the next commit fills it in.
	printf 'prepare ist noch nicht gebaut.\n' >&2
	exit 1
}

case "${1:-}" in
install) install ;;
prepare) prepare ;;
*) printf 'Aufruf: tools/recovery-setup.sh install|prepare\n' >&2; exit 1 ;;
esac
