#!/usr/bin/env bash
# Proves that the installed tools are the ones this package was written
# against. Every command sequence here comes from a run on this machine
# (~/smoke/run.sh and ~/smoke/stick.sh); a different release may spell an
# option differently, and a wrong guess would weaken the policy without anyone
# noticing. So the assumptions get proven, not trusted.
set -euo pipefail
# Closes one specific hole: without this, x=$(f) for a multi-statement f
# only looks at f's LAST command for errexit purposes - exactly what let a
# failing --help hide behind a later "cat" (fixed separately, by making
# help_of one statement). Measured, what this does NOT close: declaring and
# assigning in the same local statement, e.g. local x=$(f), still reports
# exit 0 regardless of what f returned - local's own status, not the
# substitution's, is what set -e sees there. And a call inside a tested
# condition (if/while) is exempt from errexit on purpose, by design, not a
# bug - no shopt closes that either. Declare and assign on separate lines
# (need_opt already does) avoids the first; a checking function must never
# itself be used as a condition.
shopt -s inherit_errexit
cd "$(dirname "$0")/.."

work="$(pwd)/dist/recovery"
probe="$work/work/probe"
state="$work/tpm/probe"
txt="$work/werkzeuge.txt"
handle=0x81000002        # the probe uses its own handle, not the policy's
checked=()                # "Geprüft und in Ordnung" lines, filled as checks pass

# Cleanup only, never a check: everything below may swallow an error - it
# only tidies up and must run no matter how the script ends. What it reaches
# for, it first makes sure is its own - the two conditions inside. This is the
# only place the daemon and the two PIN-copy files get cleaned up - not a
# second safeguard next to an inline one, there is no inline shred or kill
# for THEM anywhere else. -t alone would leave sessions behind, so -l and -s
# go with it - a probe that died mid-way must not cost the next run its
# session slots. One separate, real exception this trap does NOT cover:
# secret.bin further down has its own inline shred -u, a different
# mechanism; measured, a signal landing in its narrow window (created,
# shredded a beat later - about 30ms of a roughly 680ms run) leaves it
# behind. Registered after work/probe/state above are set, not before (a
# failure in between would otherwise die on "state: unbound variable" inside
# the trap itself).
cleanup() {
	local pid="" args=""
	# Only ever against the probe's OWN simulator. The TCTI is exported far
	# below this trap, and an inherited one can name the machine's real TPM -
	# measured, a failure in the option checks left three tpm2_flushcontext
	# pointed at the default device, and the redirection here hid that.
	if [ "${TPM2TOOLS_TCTI:-}" = "swtpm:path=$state/sock" ]; then
		for h in -t -l -s; do tpm2_flushcontext "$h" >/dev/null 2>&1 || :; done
	fi
	# Not the number on its own: measured, a pid file naming a stranger got
	# that stranger killed right here. The command line has to name this state
	# directory; -ww because ps otherwise cuts it to the width in COLUMNS.
	if [ -f "$state/swtpm.pid" ]; then
		pid="$(cat "$state/swtpm.pid" 2>/dev/null || :)"
		if [ -n "$pid" ]; then args="$(ps -ww -p "$pid" -o args= 2>/dev/null || :)"; fi
		case "$args " in
		"swtpm "*" --tpmstate dir=$state "*) kill "$pid" >/dev/null 2>&1 || : ;;
		esac
	fi
	shred -u "$probe/pin-second.txt" "$probe/pin-wrong.txt" >/dev/null 2>&1 || :
}
trap cleanup EXIT
# A trap that only cleans up and returns does not end the script - bash keeps
# going past it. Measured, reliably, with a single signal (6 of 6): mid-run
# SIGTERM still lets the run finish normally, exit 0, full green
# werkzeuge.txt - whoever pressed Ctrl+C gets a report from the run they
# tried to stop. That is separate from round 2's orphaned-daemon finding,
# which needed REPEATED signals to reproduce - a single SIGTERM/SIGHUP at one
# point in time already measured 0 of 26 orphans there; cleanup was never
# the gap, not stopping was.
# Each handler below must end with its own exit, at the conventional
# 128+signal status, so a signal-terminated run reads as one downstream
# instead of as an ordinary exit. Each also removes $txt itself, not just
# via cleanup (which also backs the plain EXIT trap and must never touch
# $txt, or it would delete a just-finished successful run's own report): a
# signal landing while the final block is still writing it can leave a
# truncated werkzeuge.txt behind (measured, reproducible - 7 of 24 lines,
# missing everything from the "Geprüft und in Ordnung" heading on). Task 2
# only checks that the file exists, so a stale truncated one would pass
# where a missing one would correctly stop it.
trap 'cleanup; rm -f "$txt" >/dev/null 2>&1 || :; exit 129' HUP
trap 'cleanup; rm -f "$txt" >/dev/null 2>&1 || :; exit 130' INT
trap 'cleanup; rm -f "$txt" >/dev/null 2>&1 || :; exit 143' TERM

say() { printf '%s\n' "$*" >&2; }
stop_and_ask() {
	say ""
	say "STOPP: $1"
	say "Ein Ersatzweg würde ein Sicherheitsmerkmal aus docs/recovery-modus.md schwächen."
	say "Bitte den Koordinator fragen und erst danach weitermachen."
	exit 1
}
# swtpm holds only two transient objects and every tpm2 call loads its context
# afresh without giving the slot back.
flush() { tpm2_flushcontext -t >/dev/null; }

rm -rf "$probe" "$state"
mkdir -p "$probe" "$state" "$work/softhsm/tokens" "$work/keys" "$work/pin"
# The softhsm directories go with pin, and what that is worth was measured
# rather than feared. The report of a sister strand suspected the private test
# stick keys lay open; they do not. SoftHSM2 creates every token directory 0700
# and every object file 0600 itself - read back on this machine, the only
# other-readable thing under softhsm/ is softhsm2.conf, which holds a path and
# no secret. What mkdir -p really leaves behind under umask 0002 is softhsm/ and
# softhsm/tokens/ at 0775: every local user may LIST the token UUIDs, and the
# group may add or remove entries there. That is what these two close, and it
# costs nothing. The keys were never the exposure.
chmod 700 "$work/pin" "$work/softhsm" "$work/softhsm/tokens"
cat > "$work/softhsm/softhsm2.conf" <<CONF
directories.tokendir = $work/softhsm/tokens
objectstore.backend = file
log.level = ERROR
CONF

# TEST PINs in files with mode 0600: a PIN on the command line is readable in
# /proc/<pid>/cmdline by every user on the machine. Stage "test" of the
# specification prescribes "PIN source: file" anyway. umask closes the window
# between printf and chmod; chmod stays unconditional so a file that was
# already sitting there with looser rights gets corrected too, not only a
# freshly created one.
(
	umask 077
	for entry in stick:12345678 so:87654321 platform:1234; do
		f="$work/pin/${entry%%:*}.txt"
		if [ ! -f "$f" ]; then
			printf '%s' "${entry##*:}" > "$f"
		fi
		chmod 600 "$f"
	done
)
pin_platform="$work/pin/platform.txt"

# --- 1. versions ----------------------------------------------------------
version="$(tpm2 --version | sed -n 's/.*version="\([^"]*\)".*/\1/p')"
if [ -z "$version" ]; then
	stop_and_ask "Die Fassung der tpm2-tools ist nicht feststellbar."
fi
major="${version%%.*}"
rest="${version#*.}"
minor="${rest%%.*}"
if [ "$major" -lt 5 ] || { [ "$major" -eq 5 ] && [ "$minor" -lt 7 ]; }; then
	stop_and_ask "tpm2-tools $version ist älter als 5.7, gegen die dieser Plan geschrieben ist."
fi
checked+=("  tpm2-tools $version ist 5.7 oder neuer")

# --- 2. commands and options ----------------------------------------------

tools_needed=(tpm2 tpm2_getpolicydigest tpm2_evictcontrol tpm2_policysigned tpm2_policynv
	tpm2_policypcr tpm2_policyauthvalue tpm2_policyor tpm2_sessionconfig
	tpm2_startauthsession tpm2_dictionarylockout tpm2_loadexternal tpm2_verifysignature
	swtpm swtpm_setup softhsm2-util qemu-system-x86_64 openssl)
for cmd in "${tools_needed[@]}"; do
	if ! command -v "$cmd" >/dev/null; then
		stop_and_ask "Der Befehl $cmd fehlt. Ohne ihn ist die Policy nicht baubar."
	fi
done
checked+=("  alle ${#tools_needed[@]} benötigten Befehle sind vorhanden")

help_of() {              # $1 = command
	# A single command, so its exit status IS the function's exit status: no
	# later command can mask a failing "--help" the way the previous "cat" did
	# (page="$(help_of ...)" silently kept going past a failed --help, because
	# without "shopt -s inherit_errexit" bash only looks at the LAST command's
	# status inside a $( ) - measured root cause, not a guess). 2>&1 stays as a
	# defensive habit: measured, none of the seven commands used here write
	# anything to stderr for --help, but a future one might.
	"$1" --help 2>&1
}
need_opt() {             # $1 = command, rest = long options that must exist
	local cmd="$1" opt opt_re page
	shift
	# Routed through stop_and_ask like every other check here: our users are
	# not experienced, and a wordless exit on a broken --help would be a
	# riddle for them, unlike the German plain text every other abort gives.
	if ! page="$(help_of "$cmd")"; then
		stop_and_ask "$cmd --help ist fehlgeschlagen. Ohne die Hilfe-Ausgabe lässt sich nicht prüfen, ob $cmd die nötigen Optionen kennt."
	fi
	for opt in "$@"; do
		# Anchored: a plain substring grep also passes an option that does not
		# exist but is a prefix of a real one (--pcr-l inside --pcr-list) - the
		# one case this check exists to catch is a renamed option. Escaped:
		# none of today's option names need it, but $opt still ends up in an
		# ERE, so a future name with a metacharacter must not over-match.
		opt_re="$(printf '%s' "$opt" | sed 's/[][\.^$*+?(){}|]/\\&/g')"
		if ! printf '%s\n' "$page" | grep -qE -- "(^|[^-[:alnum:]])${opt_re}([]=, |]|$)"; then
			stop_and_ask "$cmd kennt $opt nicht."
		fi
	done
	checked+=("  $cmd kennt $*")
}
need_opt tpm2_policysigned --nonce-tpm --expiration --raw-data
need_opt tpm2_startauthsession --policy-session --tpmkey-context
need_opt tpm2_sessionconfig --enable-encrypt --enable-decrypt
need_opt tpm2_policypcr --pcr-list --pcr
need_opt tpm2_policynv --input
need_opt tpm2_create --sealing-input --policy
need_opt tpm2_dictionarylockout --max-tries --recovery-time --lockout-recovery-time

# --- 3. simulator with a SHA-384 PCR bank ---------------------------------
# The probe brings its own instance: tools/recovery-swtpm.sh only exists from
# task 4 on, and the probe must not depend on what it measures.
swtpm_setup --tpm2 --tpm-state "$state" --createek --pcr-banks sha256,sha384 --overwrite
swtpm socket --tpm2 --tpmstate "dir=$state" --server "type=unixio,path=$state/sock" \
	--ctrl "type=unixio,path=$state/sock.ctrl" --flags not-need-init,startup-clear \
	--daemon --pid "file=$state/swtpm.pid"
for _ in $(seq 50); do
	if [ -S "$state/sock" ]; then break; fi
	sleep 0.1
done
if [ ! -S "$state/sock" ]; then
	say "swtpm ist nicht gestartet. Prüfen: swtpm --version und die Rechte auf $state"
	exit 1
fi
export TPM2TOOLS_TCTI="swtpm:path=$state/sock"
if ! caps="$(tpm2_getcap pcrs)"; then
	stop_and_ask "Das TPM antwortet nicht (tpm2_getcap pcrs ist fehlgeschlagen)."
fi
# Isolated first because case's * crosses newlines (measured: without this,
# a later bank's "23" can satisfy the pattern). Against the shapes swtpm
# actually produces - all 24 PCRs or none - requiring 23 here is enough.
bank_line=""
while IFS= read -r pcr_line; do
	case "$pcr_line" in *'sha384: ['*) bank_line="$pcr_line" ;; esac
done <<< "$caps"
case "$bank_line" in
*'sha384: [ '[0-9]*' 23 ]'*) ;;
*) stop_and_ask "Der Simulator hat keine SHA-384-PCR-Bank trotz --pcr-banks sha256,sha384 (oder eine andere tpm2-tools-Fassung schreibt das Format anders)." ;;
esac
checked+=("  PCR-Bank sha384 vorhanden")

# --- 4. the one unmeasured point: does the PIN come out of a file? --------
flush
tpm2_createprimary -C o -g sha384 -G ecc384 -c "$probe/primary.ctx" >/dev/null
tpm2_evictcontrol -C o -c "$probe/primary.ctx" "$handle" >/dev/null
flush
# Trial session stays unsalted: tpm2-tools 5.7 refuses --tpmkey-context here
# ("Trial sessions cannot be additionally used as encrypt/decrypt session",
# measured twice on this VM). Nothing protective is lost - a trial session
# only computes a digest that is public anyway and never carries a secret
# over the bus. Decision Thomas, 2026-09-24; the real session below still
# salts and encrypts, because that one does carry the platform PIN.
tpm2_startauthsession -S "$probe/trial.ctx" -g sha384 >/dev/null
tpm2_policyauthvalue -S "$probe/trial.ctx" >/dev/null
tpm2_getpolicydigest -S "$probe/trial.ctx" -o "$probe/authvalue.digest" >/dev/null
tpm2_flushcontext "$probe/trial.ctx" >/dev/null
printf 'TEST-PROBE-SECRET' > "$probe/secret.bin"
flush
# sensitiveDataOrigin stays CLEAR because the data comes from outside (-i).
tpm2_create -C "$handle" -g sha384 -u "$probe/p.pub" -r "$probe/p.priv" \
	-i "$probe/secret.bin" -L "$probe/authvalue.digest" -p "file:$pin_platform" \
	-a "fixedtpm|fixedparent|adminwithpolicy" >/dev/null
shred -u "$probe/secret.bin"
flush
tpm2_load -C "$handle" -u "$probe/p.pub" -r "$probe/p.priv" -c "$probe/p.ctx" >/dev/null
tpm2_startauthsession --policy-session -S "$probe/sess.ctx" -g sha384 \
	--tpmkey-context "$handle" >/dev/null
tpm2_sessionconfig --enable-encrypt --enable-decrypt "$probe/sess.ctx" >/dev/null
tpm2_policyauthvalue -S "$probe/sess.ctx" >/dev/null
flush
if ! got="$(tpm2_unseal -c "$probe/p.ctx" -p "session:$probe/sess.ctx+file:$pin_platform")"; then
	stop_and_ask "tpm2-tools nimmt die PIN nicht aus einer Datei (session:…+file:…). Die PIN kommt nicht auf die Kommandozeile."
fi
tpm2_flushcontext "$probe/sess.ctx" >/dev/null
if [ "$got" != TEST-PROBE-SECRET ]; then
	stop_and_ask "tpm2-tools nimmt die PIN nicht aus einer Datei (session:…+file:…). Die PIN kommt nicht auf die Kommandozeile."
fi
checked+=("  Plattform-PIN aus Datei (session:…+file:…) funktioniert")

# Symmetry gap: the check above reads the same path at seal and unseal time,
# so a consistent misreading of "file:..." (e.g. taken as a literal string)
# would pass unnoticed too. Close it without ever putting the PIN on a
# command line: a second file with the SAME content, at a DIFFERENT path,
# must still open the object - that only holds if the content is read, not
# the spec string. A third file with WRONG content must fail to open it.
pin_second="$probe/pin-second.txt"
pin_wrong="$probe/pin-wrong.txt"
cp "$pin_platform" "$pin_second"
flush
tpm2_startauthsession --policy-session -S "$probe/sess2.ctx" -g sha384 \
	--tpmkey-context "$handle" >/dev/null
tpm2_sessionconfig --enable-encrypt --enable-decrypt "$probe/sess2.ctx" >/dev/null
tpm2_policyauthvalue -S "$probe/sess2.ctx" >/dev/null
flush
if ! got2="$(tpm2_unseal -c "$probe/p.ctx" -p "session:$probe/sess2.ctx+file:$pin_second")"; then
	stop_and_ask "Derselbe PIN-Inhalt aus einer anderen Datei öffnet nicht - die PIN wird nicht zuverlässig aus dem Dateiinhalt gelesen."
fi
tpm2_flushcontext "$probe/sess2.ctx" >/dev/null
if [ "$got2" != TEST-PROBE-SECRET ]; then
	stop_and_ask "Derselbe PIN-Inhalt aus einer anderen Datei öffnet nicht - die PIN wird nicht zuverlässig aus dem Dateiinhalt gelesen."
fi

# This attempt bumps the primary's DA counter (a real, measured TPM error:
# "the authorization HMAC check failed and DA counter incremented"). Harmless
# here - swtpm_setup --overwrite discards this whole instance at the top of
# the next run - but worth knowing before Paket 1 reuses this pattern against
# state that persists.
printf '%s' 'WRONG-TEST-PIN' > "$pin_wrong"
flush
tpm2_startauthsession --policy-session -S "$probe/sess3.ctx" -g sha384 \
	--tpmkey-context "$handle" >/dev/null
tpm2_sessionconfig --enable-encrypt --enable-decrypt "$probe/sess3.ctx" >/dev/null
tpm2_policyauthvalue -S "$probe/sess3.ctx" >/dev/null
flush
if got3="$(tpm2_unseal -c "$probe/p.ctx" -p "session:$probe/sess3.ctx+file:$pin_wrong" 2>"$probe/wrong-pin.err")"; then
	cat "$probe/wrong-pin.err" >&2
	stop_and_ask "tpm2-tools öffnet mit falschem PIN-Inhalt aus einer Datei - der Dateiinhalt wird nicht wirklich geprüft."
fi
tpm2_flushcontext "$probe/sess3.ctx" >/dev/null
chmod 660 "$probe/wrong-pin.err"
# Must fail for THIS reason - a wrong auth value, TPM error 0x98E - and not
# for some unrelated one (a bad context, a TCTI hiccup, ...), or a green run
# would prove nothing about the file's content actually being checked. Kept
# in a file and only shown when it is not the expected error, so a passing
# run's stderr stays readable instead of showing four raw ERROR lines that
# look like a failure. Anchored on the closing paren: "0x98E" alone also
# matches an unrelated code like 0x98EF.
if ! grep -q 'Esys_Unseal(0x98E)' "$probe/wrong-pin.err"; then
	cat "$probe/wrong-pin.err" >&2
	stop_and_ask "Das Öffnen mit falschem PIN-Inhalt ist zwar gescheitert, aber nicht mit dem erwarteten TPM-Fehler (0x98E, Auth-Wert falsch)."
fi
checked+=("  Plattform-PIN: Dateiinhalt zählt, nicht der Pfad (anderer Pfad/gleicher Inhalt gelingt, falscher Inhalt scheitert mit Auth-Fehler)")

tpm2_evictcontrol -C o -c "$handle" >/dev/null

if [ -c /dev/kvm ]; then
	kvm=vorhanden
else
	kvm="nicht vorhanden, QEMU laeuft emuliert"
fi
{
	printf 'Werkzeuge der Recovery-Testumgebung\n'
	printf 'Geprüft am %s auf %s\n\n' "$(date -I)" "$(uname -srm)"
	printf 'tpm2-tools:  %s\n' "$version"
	printf 'swtpm:       %s\n' "$(swtpm --version | head -1)"
	printf 'SoftHSM2:    %s\n' "$(softhsm2-util --version)"
	printf 'QEMU:        %s\n' "$(qemu-system-x86_64 --version | head -1)"
	printf 'OpenSSL:     %s\n' "$(openssl version)"
	printf '\nGeprüft und in Ordnung:\n'
	printf '%s\n' "${checked[@]}"
	printf '\nKVM: %s\n' "$kvm"
} > "$txt"

say "Werkzeuge geprüft: $txt"
