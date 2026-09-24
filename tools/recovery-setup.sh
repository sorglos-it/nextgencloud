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

# Both checks below compare one whole field, not a substring of the whole
# output: a substring would also accept TEST-STICK-1 inside a TEST-STICK-10,
# and for the key it would match the name inside the uri field as well.
# Measured on SoftHSM2 2.6.1 with OpenSC's pkcs11-tool: a token label appears
# once, in the Label field of an initialized slot; an object's name appears
# in its own label field and again in its uri field.
# Both also end in grep WITHOUT -q. With pipefail a -q ends the pipeline at
# the first hit, and the upstream sed's SIGPIPE becomes the answer: measured,
# a hit reported as "not found" 200 of 200 times once the input outgrew the
# pipe buffer, 0 of 2000 times at the size --show-slots has today. So this
# is not a repair, it is keeping a trap out of a list that grows per token.
has_token() {            # $1 = token label
	local slots
	if ! slots="$(softhsm2-util --show-slots)"; then
		printf 'SoftHSM2 antwortet nicht. Stimmen die Einstellungen in %s?\n' "$SOFTHSM2_CONF" >&2
		printf 'Neu anlegen mit: tools/recovery-tools.sh\n' >&2
		exit 1
	fi
	printf '%s\n' "$slots" | sed -n 's/^[[:space:]]*Label:[[:space:]]*//p' |
		sed 's/[[:space:]]*$//' | grep -Fx -- "$1" >/dev/null
}
# Asked separately from has_token, not folded into one question: the token
# exists from --init-token on, the key only after the second call. Measured:
# a token without a key shows its label in --show-slots just the same, so one
# shared question would leave a run that stopped between the two calls
# without a key for good. Public objects need no login. Reads $module and
# $work, which prepare sets before the first call.
has_key() {              # $1 = token label, $2 = key label
	local objects
	if ! objects="$(pkcs11-tool --module "$module" --token-label "$1" --list-objects --type pubkey)"; then
		printf 'Das Token %s lässt sich nicht lesen.\n' "$1" >&2
		printf 'Wegwerfen und neu anlegen: rm -rf %s/softhsm/tokens/* %s/keys/stick*\n' "$work" "$work" >&2
		exit 1
	fi
	printf '%s\n' "$objects" | sed -n 's/^[[:space:]]*label:[[:space:]]*//p' |
		sed 's/[[:space:]]*$//' | grep -Fx -- "$2" >/dev/null
}

prepare() {
	tools/recovery-env.sh
	work="$(pwd)/dist/recovery"
	export SOFTHSM2_CONF="$work/softhsm/softhsm2.conf"
	module=/usr/lib/softhsm/libsofthsm2.so
	# recovery-env.sh checks the programs and recovery-tools.sh's report, not
	# the three files read here. Measured: without the configuration file
	# softhsm2-util stops with an English error that names no path at all, so
	# the three get a message of their own.
	for file in "$SOFTHSM2_CONF" "$work/pin/stick.txt" "$work/pin/so.txt"; do
		if [ ! -s "$file" ]; then
			printf 'Die Testumgebung ist unvollständig, es fehlt: %s\n' "$file" >&2
			printf 'Anlegen mit: tools/recovery-tools.sh\n' >&2
			exit 1
		fi
	done
	pin="$(cat "$work/pin/stick.txt")"
	so_pin="$(cat "$work/pin/so.txt")"
	mkdir -p "$work/keys"

	# Five tokens: four sticks for the quorum plus the spare stick the ceremony
	# in the specification demands. Each stick is its own token with its own
	# key - no clones, exactly like the real sticks.
	prints=""
	for n in 1 2 3 4 5; do
		label="TEST-STICK-$n"
		if ! has_token "$label"; then
			softhsm2-util --init-token --free --label "$label" --so-pin "$so_pin" --pin "$pin"
		fi
		if ! has_key "$label" "stick$n"; then
			pkcs11-tool --module "$module" --token-label "$label" --login --pin "$pin" \
				--keypairgen --key-type EC:secp384r1 --label "stick$n" --id "0$n"
		fi
		# Read out on every run, not only when the file is missing. Tokens and
		# key files can be thrown away separately, and measured: a kept keys/
		# beside an emptied tokens/ otherwise leaves all five files holding
		# keys that no token has any more, and the run still reports success.
		pkcs11-tool --module "$module" --token-label "$label" --read-object \
			--type pubkey --label "stick$n" --output-file "$work/keys/stick$n.der"
		openssl ec -pubin -inform DER -in "$work/keys/stick$n.der" -pubout \
			-out "$work/keys/stick$n.pem"
		# The curve is only demanded where the key is generated, and that step
		# is skipped for a key that is already there - so it gets asked here,
		# for every stick, every run. Captured first: reading the status of
		# openssl out of a case or a local would lose it.
		if ! text="$(openssl ec -pubin -in "$work/keys/stick$n.pem" -noout -text 2>/dev/null)"; then
			printf 'Der Schlüssel von %s ist nicht lesbar: %s\n' "$label" "$work/keys/stick$n.pem" >&2
			printf 'Wegwerfen und neu anlegen: rm -rf %s/softhsm/tokens/* %s/keys/stick*\n' "$work" "$work" >&2
			exit 1
		fi
		case "$text" in
		*'NIST CURVE: P-384'*) ;;
		*)
			printf '%s hat keinen secp384r1-Schlüssel, den die Vorgabe verlangt.\n' "$label" >&2
			printf 'Wegwerfen und neu anlegen: rm -rf %s/softhsm/tokens/* %s/keys/stick*\n' "$work" "$work" >&2
			exit 1
			;;
		esac
		# One line per stick, so two equal keys really do collapse into one.
		# Read from standard input on purpose: sha256sum puts the file name
		# in its line, and five names always differ, so the count below would
		# reach five whatever the keys are.
		if ! print="$(sha256sum < "$work/keys/stick$n.pem")"; then
			printf 'Der Schlüssel von %s ist nicht lesbar: %s\n' "$label" "$work/keys/stick$n.pem" >&2
			exit 1
		fi
		prints="$prints$print"$'\n'
	done

	# Five sticks, five different keys: if two matched, every "any pair opens"
	# test would pass for the wrong reason. Counted over the five lines above.
	# Counting the lines of openssl's text dump instead would count how varied
	# the text is, not how many keys there are - measured: five IDENTICAL
	# P-384 keys already give ten different lines, so a "fewer than five"
	# threshold can never be undercut.
	distinct="$(printf '%s' "$prints" | sort -u | wc -l)"
	if [ "$distinct" -ne 5 ]; then
		printf 'Zwei Test-Sticks haben denselben Schlüssel. Das darf nicht sein.\n' >&2
		printf 'Token wegwerfen und neu anlegen: rm -rf %s/softhsm/tokens/* %s/keys/stick*\n' \
			"$work" "$work" >&2
		exit 1
	fi
	printf 'Fünf Test-Sticks bereit (1-4 Quorum, 5 Reserve): %s/keys\n' "$work" >&2
}

case "${1:-}" in
install) install ;;
prepare) prepare ;;
*) printf 'Aufruf: tools/recovery-setup.sh install|prepare\n' >&2; exit 1 ;;
esac
