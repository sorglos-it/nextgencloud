#!/usr/bin/env bash
# Sets up the recovery test environment. "install" needs sudo and the network
# and runs once per machine; everything else has to work offline, so the parts
# are separate subcommands - a test run must never call apt-get.
set -euo pipefail
cd "$(dirname "$0")/.."

# Set here rather than inside prepare: stop reads work and key_id reads
# module, and a variable that only exists while prepare runs would make those
# two die with an English bash message if anything ever called them first.
work="$(pwd)/dist/recovery"
module=/usr/lib/softhsm/libsofthsm2.so

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

# Every abort that is cured by throwing the tokens away ends here, because
# they all have the same way out: a token and its key file only mean anything
# together, so both go together. The two aborts that this does NOT cure - a
# SoftHSM2 that will not answer, and files recovery-tools.sh never wrote -
# carry their own hint and do not come through here. Never call this from
# inside a $( ): an exit there ends only the subshell (measured).
stop() {                 # $1 = what went wrong, in German
	printf '%s\n' "$1" >&2
	printf 'Wegwerfen und neu anlegen: rm -rf %s/softhsm/tokens/* %s/keys/stick*\n' "$work" "$work" >&2
	printf 'Danach noch einmal: tools/recovery-setup.sh prepare\n' >&2
	exit 1
}

# The label is taken out of the Label field instead of being searched for in
# the whole output. At five sticks that changes no answer by itself, and it is
# not what carries this form: TEST-STICK-1 cannot turn up inside a
# TEST-STICK-10 here, and a hit in an object's uri field would be the same
# object anyway. What carries it is the line below - the output is captured
# first, so a softhsm2-util that fails is answered in German right here
# instead of being read as "token is missing" and dying in English one step
# later.
# The grep has no -q on purpose. With pipefail a -q ends the pipeline at the
# first hit and the upstream sed's SIGPIPE becomes the answer: measured on
# GNU grep 3.11, a hit reported as "not found" 200 of 200 times once the input
# outgrew the pipe buffer, 0 of 2000 times at the size --show-slots has today,
# and 0 with >/dev/null in either case. So this is not a repair, it keeps a
# trap out of a list that grows with every token.
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

# Writes the number of the object named $2, nothing when the token has no
# object of that name. Returns non-zero only when the listing itself failed,
# so the caller can tell "not there" from "could not ask" and answer in German
# either way. It must not call stop itself: it runs inside a $( ), where an
# exit ends only the subshell (measured).
key_id() {               # $1 = token label, $2 = object name, $3 = pubkey|privkey, $4 = user PIN
	local objects login=()
	# The private half is shown only to someone who logs in. Measured: without
	# a login the private listing comes back EMPTY with exit 0, which would
	# read as "no key" for every stick. The price is the test PIN in the
	# process list once more per stick on a repeat run, twice more on the run
	# that creates the key; decided that way in round 1, because a stick that
	# cannot sign would otherwise surface only in task 10. $4 itself travels as
	# a function argument, which no other process can read.
	if [ "$3" = privkey ]; then login=(--login --pin "$4"); fi
	# --label filters, and it filters exactly - measured: stick1 finds stick1
	# and not stick12, stick finds nothing. So the listing holds that one
	# object and the ID line below is the one that was asked for.
	objects="$(pkcs11-tool --module "$module" --token-label "$1" "${login[@]}" \
		--list-objects --type "$3" --label "$2")" || return 1
	printf '%s\n' "$objects" | sed -n 's/^[[:space:]]*ID:[[:space:]]*//p' | sed 's/[[:space:]]*$//'
}

prepare() {
	local file pin so_pin n label priv_id pub_id text fingerprint fingerprints distinct
	tools/recovery-env.sh
	export SOFTHSM2_CONF="$work/softhsm/softhsm2.conf"
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
	fingerprints=""
	for n in 1 2 3 4 5; do
		label="TEST-STICK-$n"
		if ! has_token "$label"; then
			if ! softhsm2-util --init-token --free --label "$label" \
				--so-pin "$so_pin" --pin "$pin"; then
				stop "Das Token $label lässt sich nicht anlegen."
			fi
		fi
		# The PRIVATE half is what decides here. Only it can sign, and it is
		# the half nobody sees without logging in - asking the public one, as
		# this did before, calls a token ready that can never sign.
		if ! priv_id="$(key_id "$label" "stick$n" privkey "$pin")"; then
			stop "Das Token $label lässt sich nicht öffnen. Stimmt die PIN in $work/pin/stick.txt noch?"
		fi
		if [ -z "$priv_id" ]; then
			# Generated only when the name is free on BOTH halves. Measured:
			# --keypairgen on a name that is already taken succeeds and leaves
			# TWO objects of that name behind. So a half-filled token gets
			# thrown away, not topped up.
			if ! pub_id="$(key_id "$label" "stick$n" pubkey "$pin")"; then
				stop "Das Token $label lässt sich nicht lesen."
			fi
			if [ -n "$pub_id" ]; then
				stop "In $label liegt nur die öffentliche Hälfte von stick$n. Unterschreiben kann dieser Stick damit nicht."
			fi
			if ! pkcs11-tool --module "$module" --token-label "$label" --login --pin "$pin" \
				--keypairgen --key-type EC:secp384r1 --label "stick$n" --id "0$n"; then
				stop "Für $label lässt sich kein Schlüsselpaar erzeugen."
			fi
			if ! priv_id="$(key_id "$label" "stick$n" privkey "$pin")"; then
				stop "Das Token $label lässt sich nach dem Erzeugen nicht öffnen."
			fi
		fi
		# Number and curve are only demanded where the key is generated, and
		# that step is skipped for a key that is already there - so both get
		# asked here, every stick, every run. The private half's number is
		# read out; the public half's is asked by --read-object below, which
		# looks for name AND number and answers "object not found" when the
		# number differs (measured).
		if [ "$priv_id" != "0$n" ]; then
			stop "Der private Schlüssel stick$n in $label trägt die Nummer $priv_id statt 0$n."
		fi
		# Read out on every run, not only when the file is missing. Tokens and
		# key files can be thrown away separately, and measured: a kept keys/
		# beside an emptied tokens/ otherwise leaves all five files holding
		# keys that no token has any more, and the run still reports success.
		if ! pkcs11-tool --module "$module" --token-label "$label" --read-object \
			--type pubkey --label "stick$n" --id "0$n" \
			--output-file "$work/keys/stick$n.der"; then
			stop "In $label ist kein öffentlicher Schlüssel stick$n mit der Nummer 0$n zu finden."
		fi
		if ! openssl ec -pubin -inform DER -in "$work/keys/stick$n.der" -pubout \
			-out "$work/keys/stick$n.pem"; then
			stop "Der Schlüssel von $label lässt sich nicht nach $work/keys/stick$n.pem umschreiben."
		fi
		# Captured first: reading the status of openssl out of a case or a
		# local would lose it.
		if ! text="$(openssl ec -pubin -in "$work/keys/stick$n.pem" -noout -text 2>/dev/null)"; then
			stop "Die abgelegte Datei $work/keys/stick$n.pem ist kein lesbarer Schlüssel."
		fi
		case "$text" in
		*'NIST CURVE: P-384'*) ;;
		*) stop "$label hat keinen secp384r1-Schlüssel, den die Vorgabe verlangt." ;;
		esac
		# One line per stick, so two equal keys really do collapse into one.
		# Read from standard input on purpose: sha256sum puts the file name in
		# its line, and five names always differ, so the count below would
		# reach five whatever the keys are.
		if ! fingerprint="$(sha256sum < "$work/keys/stick$n.pem")"; then
			stop "Die Datei $work/keys/stick$n.pem lässt sich nicht noch einmal lesen."
		fi
		fingerprints="$fingerprints$fingerprint"$'\n'
	done

	# Five sticks, five different keys: if two matched, every "any pair opens"
	# test would pass for the wrong reason. Counted over the five lines above.
	# Counting the lines of openssl's text dump instead would count how varied
	# the text is, not how many keys there are - measured: five IDENTICAL
	# P-384 keys already give ten different lines, so a "fewer than five"
	# threshold can never be undercut.
	distinct="$(printf '%s' "$fingerprints" | sort -u | wc -l)"
	if [ "$distinct" -ne 5 ]; then
		stop "Zwei Test-Sticks haben denselben Schlüssel. Das darf nicht sein."
	fi
	printf 'Fünf Test-Sticks bereit (1-4 Quorum, 5 Reserve): %s/keys\n' "$work" >&2
}

case "${1:-}" in
install) install ;;
prepare) prepare ;;
*) printf 'Aufruf: tools/recovery-setup.sh install|prepare\n' >&2; exit 1 ;;
esac
