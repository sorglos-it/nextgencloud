#!/usr/bin/env bash
# Sets up the recovery test environment. "install" needs sudo and the network
# and runs once per machine; everything else has to work offline, so the parts
# are separate subcommands - a test run must never call apt-get.
set -euo pipefail
cd "$(dirname "$0")/.."

# Set here rather than inside prepare: stop reads work and key_ids reads
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

# Every abort that is cured by throwing a token away ends here. $2, when it is
# given, is the number of the ONE stick that is broken, and then only that one
# goes: the other four keep their keys, and from task 6 on the stick's own
# name is sealed into the policy digest, so a key thrown away for nothing
# takes everything sealed on it along. Measured: --delete-token removes
# exactly that one token directory, needs no PIN, and leaves the other four
# keys byte for byte as they were.
# Three aborts do NOT come through here, because throwing tokens away does not
# cure them, and they carry their own hint: a SoftHSM2 that will not answer,
# files that recovery-tools.sh never wrote, and a wrong subcommand.
# Never call this from inside a $( ): an exit there ends only the subshell
# (measured).
stop() {                 # $1 = what went wrong, in German; $2 = stick number, optional
	printf '%s\n' "$1" >&2
	if [ -n "${2:-}" ]; then
		printf 'Nur diesen Stick wegwerfen: SOFTHSM2_CONF=%s/softhsm/softhsm2.conf softhsm2-util --delete-token --token TEST-STICK-%s && rm -f %s/keys/stick%s.*\n' \
			"$work" "$2" "$work" "$2" >&2
	else
		printf 'Alle wegwerfen und neu anlegen: rm -rf %s/softhsm/tokens/* %s/keys/stick*\n' "$work" "$work" >&2
	fi
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

# Writes ONE LINE PER OBJECT named $2: its number, or "-" when that object
# carries no number at all. No line at all means the token has no object of
# that name. Both odd shapes are real, not precaution - measured: pkcs11-tool
# prints the ID line only for an object that has a CKA_ID, and a keypair made
# without --id has none; and --keypairgen onto a name that is already taken
# succeeds and leaves a SECOND object of that name behind. Reading the ID
# lines alone would turn the first into "no key" and the second into a number
# of two lines.
# Returns non-zero only when the listing itself failed, so "not there" stays
# tellable from "could not ask". It must not call stop: it runs inside a $( ),
# where an exit ends only the subshell (measured).
key_ids() {              # $1 = token label, $2 = object name, $3 = pubkey|privkey, $4 = user PIN
	local objects fields line login=() open=no id=""
	# The private half is shown only to someone who logs in. Measured: without
	# a login the private listing comes back EMPTY with exit 0, which would
	# read as "no key" for every stick. The price is the test PIN in the
	# process list once more per stick on a repeat run, twice more on the run
	# that creates the key; decided that way in round 1, because a stick that
	# cannot sign would otherwise surface only in task 10. $4 itself travels as
	# a function argument, which no other process can read.
	if [ "$3" = privkey ]; then login=(--login --pin "$4"); fi
	# --label filters, and measured it filters exactly: stick1 finds stick1 and
	# not stick12, stick finds nothing. The name is compared again below all
	# the same, so the answer does not hang on that staying true.
	objects="$(pkcs11-tool --module "$module" --token-label "$1" "${login[@]}" \
		--list-objects --type "$3" --label "$2")" || return 1
	# Boiled down to one "label <name>" or "id <number>" line each, anchored on
	# the field name: the uri line carries the same name and the same number
	# and must not be counted as a second object. pkcs11-tool prints an
	# object's label first and its ID right after it, so the next label line
	# closes the object before it.
	fields="$(printf '%s\n' "$objects" |
		sed -n -e 's/^[[:space:]]*label:[[:space:]]*/label /p' \
		       -e 's/^[[:space:]]*ID:[[:space:]]*/id /p' |
		sed 's/[[:space:]]*$//')"
	while IFS= read -r line; do
		case "$line" in
		"label $2")
			if [ "$open" = yes ]; then printf '%s\n' "${id:--}"; fi
			open=yes
			id=""
			;;
		"label "*)
			if [ "$open" = yes ]; then printf '%s\n' "${id:--}"; fi
			open=no
			id=""
			;;
		"id "*)
			if [ "$open" = yes ]; then id="${line#id }"; fi
			;;
		esac
	done <<< "$fields"
	if [ "$open" = yes ]; then printf '%s\n' "${id:--}"; fi
}

# The one question both halves have to answer: is exactly one key of that name
# there, and does it carry the number the plan gives it? Each no gets its own
# sentence, because the four of them are four different states and the user
# should not have to guess which one they are in.
check_ids() {            # $1 = key_ids output, $2 = "private"|"öffentliche", $3 = token label,
                         # $4 = key name, $5 = wanted number, $6 = stick number
	case "$1" in
	"$5") ;;
	*$'\n'*) stop "In $3 liegen zwei $2 Schlüssel mit dem Namen $4. Welcher gilt, ist nicht zu entscheiden." "$6" ;;
	"") stop "In $3 fehlt der $2 Schlüssel $4. So ist der Stick unbrauchbar." "$6" ;;
	"-") stop "Der $2 Schlüssel $4 in $3 trägt gar keine Nummer, gebraucht wird $5." "$6" ;;
	*) stop "Der $2 Schlüssel $4 in $3 trägt die Nummer $1 statt $5." "$6" ;;
	esac
}

prepare() {
	local file pin so_pin n label priv_ids pub_ids text fingerprint fingerprints distinct
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
		# Both halves get asked, every run: the private one because only it can
		# sign and only a login shows it at all, the public one because it is
		# what ends up in the .pem file.
		if ! priv_ids="$(key_ids "$label" "stick$n" privkey "$pin")"; then
			stop "Das Token $label lässt sich nicht öffnen. Stimmt die PIN in $work/pin/stick.txt noch?" "$n"
		fi
		if ! pub_ids="$(key_ids "$label" "stick$n" pubkey "$pin")"; then
			stop "Das Token $label lässt sich nicht lesen." "$n"
		fi
		if [ -z "$priv_ids" ] && [ -z "$pub_ids" ]; then
			# Generated only onto a name that is free on BOTH halves. Measured:
			# --keypairgen onto a taken name succeeds and leaves TWO objects of
			# that name behind, and then nothing says which of them counts. A
			# half-filled token is refused below, not topped up here.
			if ! pkcs11-tool --module "$module" --token-label "$label" --login --pin "$pin" \
				--keypairgen --key-type EC:secp384r1 --label "stick$n" --id "0$n"; then
				stop "Für $label lässt sich kein Schlüsselpaar erzeugen." "$n"
			fi
			if ! priv_ids="$(key_ids "$label" "stick$n" privkey "$pin")"; then
				stop "Das Token $label lässt sich nach dem Erzeugen nicht öffnen." "$n"
			fi
			if ! pub_ids="$(key_ids "$label" "stick$n" pubkey "$pin")"; then
				stop "Das Token $label lässt sich nach dem Erzeugen nicht lesen." "$n"
			fi
		fi
		# Number and curve are only demanded where the key is generated, and
		# that step is skipped for a key that is already there - so they get
		# asked here, every stick, every run: the number for both halves right
		# now, the curve further down, on the public key that ends up in the
		# file (the two halves of a pair share it).
		check_ids "$priv_ids" "private" "$label" "stick$n" "0$n" "$n"
		check_ids "$pub_ids" "öffentliche" "$label" "stick$n" "0$n" "$n"
		# Read out on every run, not only when the file is missing. Tokens and
		# key files can be thrown away separately, and measured: a kept keys/
		# beside an emptied tokens/ otherwise leaves all five files holding
		# keys that no token has any more, and the run still reports success.
		# Name and number are settled by the check above, so what is left to
		# fail here is the reading and the writing - the message says so
		# instead of naming a cause it does not know.
		if ! pkcs11-tool --module "$module" --token-label "$label" --read-object \
			--type pubkey --label "stick$n" --id "0$n" \
			--output-file "$work/keys/stick$n.der"; then
			stop "Der öffentliche Schlüssel stick$n aus $label kam nicht in $work/keys/stick$n.der an. Die Zeilen darüber sagen, woran es lag." "$n"
		fi
		if ! openssl ec -pubin -inform DER -in "$work/keys/stick$n.der" -pubout \
			-out "$work/keys/stick$n.pem"; then
			stop "Der Schlüssel von $label lässt sich nicht nach $work/keys/stick$n.pem umschreiben." "$n"
		fi
		# Captured first: reading the status of openssl out of a case or a
		# local would lose it.
		if ! text="$(openssl ec -pubin -in "$work/keys/stick$n.pem" -noout -text 2>/dev/null)"; then
			stop "Die abgelegte Datei $work/keys/stick$n.pem ist kein lesbarer Schlüssel." "$n"
		fi
		case "$text" in
		*'NIST CURVE: P-384'*) ;;
		*) stop "$label hat keinen secp384r1-Schlüssel, den die Vorgabe verlangt." "$n" ;;
		esac
		# One line per stick, so two equal keys really do collapse into one.
		# Read from standard input on purpose: sha256sum puts the file name in
		# its line, and five names always differ, so the count below would
		# reach five whatever the keys are.
		if ! fingerprint="$(sha256sum < "$work/keys/stick$n.pem")"; then
			stop "Die Datei $work/keys/stick$n.pem lässt sich nicht noch einmal lesen." "$n"
		fi
		fingerprints="$fingerprints$fingerprint"$'\n'
	done

	# Five sticks, five different keys: if two matched, every "any pair opens"
	# test would pass for the wrong reason. Counted over the five lines above.
	# Counting the lines of openssl's text dump instead would count how varied
	# the text is, not how many keys there are - measured: five IDENTICAL
	# P-384 keys already give ten different lines, so a "fewer than five"
	# threshold can never be undercut. No stick number here on purpose: which
	# two of them share the key is exactly what this does not know.
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
