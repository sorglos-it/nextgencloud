#!/usr/bin/env bash
# Starts, stops and resets the TPM simulator, and names the socket the tpm2
# tools are to talk to. There are two instances: "main" carries the policy
# tests, "qemu" belongs to the virtual machine - a guest moves the PCRs, and
# one shared simulator would change the very PCR state the policy is sealed
# against.
set -euo pipefail
shopt -s inherit_errexit
cd "$(dirname "$0")/.."
# Absolute, and that is not cosmetic. Measured: handed a relative state
# directory, swtpm starts, reports success and even lays down its socket, and
# the TPM behind it answers no command at all.
work="$(pwd)/dist/recovery"

usage() {
	printf 'Aufruf: tools/recovery-swtpm.sh start|stop|reset|resume|tcti [main|qemu] [mitschnitt]\n' >&2
	printf 'mitschnitt gibt es nur für start und reset der Instanz main - siehe Kommentar im Skript.\n' >&2
	exit 1
}

# Set by resume alone, and read by start: it decides whether this start gets a
# data channel. Empty everywhere else, so nothing but resume can switch one on.
datenkanal=""

# Set by the third argument alone, and read by start: it decides whether this
# simulator writes the wire down. DANGEROUS, and the danger is the point of the
# thing: --log level=20 puts every byte that travels between the tpm2 tools and
# the TPM into a file, the sealed secret included - measured, TEST-DEK stands
# twice in the clear in a run of seal, both times inside TPM2_CC_Create. That is
# bearable here (a throw-away VM, a secret that is the string TEST-DEK) and it
# would be a catastrophe in the real installation, where work package 6 hangs
# the real DEK on the same commands. So it never switches itself on: nothing but
# this one argument sets it, it is refused for the instance the virtual machine
# hangs on, and the caller that asks for it is the one that takes the recording
# away again (recovery-test.sh shreds it and then checks it is gone). Whoever
# needs it while a simulator is already up has to go through reset: start
# returns early on a live instance and would leave it running without the log.
mitschnitt=""

if [ "$#" -lt 1 ] || [ "$#" -gt 3 ]; then usage; fi
name="${2:-main}"
case "$name" in
main|qemu) ;;
*) printf 'Unbekannte Instanz: %s (erlaubt: main, qemu)\n' "$name" >&2; exit 1 ;;
esac
if [ "$#" -eq 3 ]; then
	if [ "$3" != mitschnitt ]; then usage; fi
	# Only where the recording is wanted AND can be tidied up again: the policy
	# instance, and only when this call really starts a simulator.
	case "$1" in
	start|reset) ;;
	*) printf 'Den Mitschnitt gibt es nur bei start und reset.\n' >&2; exit 1 ;;
	esac
	if [ "$name" != main ]; then
		printf 'Den Mitschnitt gibt es nur für die Instanz main.\n' >&2
		printf 'Die Instanz qemu behält ihr work-Verzeichnis über reset hinweg, dort bliebe er liegen.\n' >&2
		exit 1
	fi
	mitschnitt=ja
fi
dir="$work/tpm/$name"
sock="$dir/sock"
pidfile="$dir/swtpm.pid"

# True only for a live swtpm that carries THIS instance's state directory on
# its own command line. A number on its own proves nothing: it is handed out
# again, and any process may merely mention the directory. -ww is not
# decoration: without it ps cuts the line to the width in COLUMNS, and
# measured, an exported COLUMNS=80 cut it to 80 characters and made a running
# simulator invisible here.
is_this_swtpm() {        # $1 = PID
	local args
	[ -n "${1:-}" ] || return 1
	args="$(ps -ww -p "$1" -o args= 2>/dev/null)" || return 1
	case "$args " in
	"swtpm "*" --tpmstate dir=$dir "*) return 0 ;;
	esac
	return 1
}

# Writes the PID of THIS instance's simulator, or nothing at all. Two sources,
# because either one can be missing on its own: the pid file swtpm writes, and
# the process list. Both are put to the same question, because neither is
# evidence by itself - a pid file outlives its daemon, and pgrep only says the
# text turns up somewhere on a command line. Only ever returns, never exits: it
# runs inside a $( ), where an exit would end just the subshell.
swtpm_pid() {
	local candidate="" found=""
	if [ -f "$pidfile" ]; then
		candidate="$(cat "$pidfile" 2>/dev/null || :)"
		if is_this_swtpm "$candidate"; then
			printf '%s\n' "$candidate"
			return 0
		fi
	fi
	# A missing or stale pid file does not mean no daemon: one can still sit on
	# this state directory, and reset would pull it out from under it. Asked by
	# program name, not by path: pgrep -f reads its pattern as an expression,
	# and measured, a single square bracket in the path made the search come
	# back empty while the simulator was running. Every hit then has to answer
	# the same question as the pid file did.
	if found="$(pgrep -x swtpm)"; then
		while IFS= read -r candidate; do
			if is_this_swtpm "$candidate"; then
				printf '%s\n' "$candidate"
				return 0
			fi
		done <<< "$found"
	fi
	return 0
}

# Asks the simulator itself, which is the only real proof that it is there.
# Only main can be asked: QEMU starts its own instance over the control
# channel, so a qemu simulator answers nothing until the virtual machine has.
ask_tpm() {
	if [ "$name" = main ]; then
		# recovery-env.sh owns the SHA-384 check; nothing rebuilds it.
		TPM2TOOLS_TCTI="swtpm:path=$sock" tools/recovery-env.sh pcr-bank
	fi
}

start() {
	local pid flags=() channel=() protokoll=() sichtbar
	mkdir -p "$dir" "$work/work"
	# In work/, deliberately: that is the one directory recovery-test.sh empties,
	# searches for the plaintext and has reset throw away, so the recording falls
	# under the same rules as the secret.bin of recovery-policy.sh.
	if [ -n "$mitschnitt" ]; then
		protokoll=(--log "file=$work/work/wire-$name.log,level=20")
	fi
	# The data channel is what the tpm2 tools talk to, and main always has it.
	# The instance qemu must NOT have one while the virtual machine hangs on
	# it: QEMU hands its OWN data channel in over the control channel with
	# CMD_SET_DATAFD, and swtpm rejects that - and ends itself right after -
	# when one is already bound. Measured: with --server the attach dies with
	# "Failed to send CMD_SET_DATAFD", without it QEMU comes up. Only resume
	# adds the channel afterwards, once the machine is gone, so the PCRs can be
	# read out. Do not put --server back for qemu.
	if [ "$name" = main ] || [ -n "$datenkanal" ]; then
		channel=(--server "type=unixio,path=$sock")
		sichtbar="$sock"
	else
		# Without a data channel no $sock is ever laid down, so the control
		# socket is the only thing that can prove this simulator is up.
		sichtbar="$sock.ctrl"
	fi
	pid="$(swtpm_pid)"
	if [ -n "$pid" ] && [ -S "$sichtbar" ]; then
		# Already up - and that is not taken on trust: measured, a stranger on
		# the process list plus a socket file left lying around got a
		# "started" out of this with no simulator running anywhere.
		ask_tpm
		return 0
	fi
	if [ -n "$pid" ]; then
		# Running without its socket is a half state and has to end first: a
		# second simulator on the same state directory cannot start, libtpms
		# holds the directory locked.
		stop
	fi
	# swtpm_setup writes its state file as it goes, so a run that died halfway
	# leaves one behind that a plain existence test takes for finished -
	# measured, an empty one gets the simulator no further than "Could not
	# initialize libtpms". The stamp goes down only after swtpm_setup has
	# returned, and by rename, so it is there wholly or not at all. A state
	# directory from before this stamp existed carries none, so it gets
	# manufactured once more - for main that is what reset does anyway, for
	# qemu it means the instance starts over exactly once.
	if [ ! -s "$dir/einrichtung.txt" ]; then
		# --createek is enough here. The EK certificate needs root and
		# /var/lib/swtpm-localca, and checking it against the manufacturer CA
		# belongs to work package 3. The SHA-384 bank has to be asked for.
		swtpm_setup --tpm2 --tpm-state "$dir" --createek --pcr-banks sha256,sha384 --overwrite
		printf 'swtpm_setup: EK, PCR-Bänke sha256,sha384\n' > "$dir/einrichtung.neu"
		mv "$dir/einrichtung.neu" "$dir/einrichtung.txt"
	fi
	if [ "$name" = main ]; then
		# The policy instance has to answer straight away. The qemu instance
		# gets no such flags: QEMU initialises that TPM itself over the
		# control channel, so it must not be started up in advance.
		flags=(--flags not-need-init,startup-clear)
	fi
	if ! swtpm socket --tpm2 --tpmstate "dir=$dir" "${channel[@]}" "${protokoll[@]}" \
		--ctrl "type=unixio,path=$sock.ctrl" "${flags[@]}" --daemon --pid "file=$pidfile"; then
		printf 'Der Simulator %s lässt sich nicht starten. Die Zeilen darüber sagen, woran es lag.\n' "$name" >&2
		printf 'Hilft das nicht weiter: Zustand wegwerfen mit tools/recovery-swtpm.sh reset %s\n' "$name" >&2
		if [ "$name" = main ]; then
			printf 'Achtung: reset main leert dabei auch %s/work.\n' "$work" >&2
		fi
		exit 1
	fi
	# Guards against a machine slower than this one: measured, this swtpm has
	# its socket bound before it returns - 0 extra passes in every run, 2 ms
	# idle and 12 ms with sixteen busy processes, against a budget of 50
	# passes. It stays a pass count and not a clock: no message here names a
	# time, so nothing can drift apart, and a budget that stretches with the
	# load gives a slow machine more room instead of less. What decides is the
	# check below.
	for _ in $(seq 50); do
		if [ -S "$sichtbar" ]; then break; fi
		sleep 0.1
	done
	if [ ! -S "$sichtbar" ]; then
		printf 'Der Simulator %s ist nicht gestartet: %s\n' "$name" "$dir" >&2
		printf 'Prüfen: swtpm --version, Rechte auf dem Verzeichnis, alter Socket im Weg\n' >&2
		exit 1
	fi
	ask_tpm
}

# Brings the data channel to the instance qemu AFTER the virtual machine is
# gone, so its PCRs can be read. The volatile state is what carries them: it
# has to be written out while this simulator still lives, because stop takes it
# with it. Measured: a simulator restarted without this step answers PCR 0 with
# all zero, and the proof that the firmware measured would be lost.
resume() {
	local pid rc
	if [ "$name" != qemu ]; then
		printf 'resume gibt es nur für die Instanz qemu.\n' >&2
		exit 1
	fi
	pid="$(swtpm_pid)"
	if [ -z "$pid" ]; then
		printf 'Die Instanz qemu läuft nicht, es gibt nichts zu übernehmen.\n' >&2
		printf 'Prüfen: hat die virtuelle Maschine den Simulator mitgenommen? Ein geordnetes\n' >&2
		printf 'Ende von QEMU (QMP quit oder SIGTERM) tut genau das - es muss SIGKILL sein.\n' >&2
		exit 1
	fi
	# Needs the control channel to itself. QEMU holds it as long as it runs, and
	# a unixio control channel takes one client at a time - and on that this
	# call does not fail fast, it waits. Measured three times by hand against a
	# running machine: it comes back only after 20.0 s, and then with
	# swtpm_ioctl's own "Connection timed out" and EXIT=1. That limit belongs to
	# a foreign tool, nothing here promises it, and twenty silent seconds are a
	# poor answer in any case. So the wait gets a limit of our own, 10 s, and a
	# message that says what to do. It sits wide of the normal case, and that is
	# all it does: inside tools/recovery-qemu.sh the machine is provably gone
	# before this runs, and the call then costs 2-64 ms (19 runs, idle up to
	# sixty-four busy processes), so 10 s is a thousandfold clear of it. It is
	# no proof that the machine is the culprit, though - measured, a simulator
	# halted with SIGSTOP right after the marker gives EXIT=124 just the same,
	# with not one QEMU left on the process list. That is why the message below
	# names both causes instead of picking one; the same misreading was written
	# out for qemu_swtpm_alive in recovery-qemu.sh and belongs here too.
	# timeout is coreutils, the same package as the seq, cut, head
	# and shred this package already uses, and it stands on the tool list for
	# itself. EXIT=124 gets its own message: "still held" and "broken" need
	# different answers. Nothing of swtpm_ioctl's own is lost to the limit:
	# timeout leaves the child's stderr alone - measured against a socket that
	# is not there, three runs each way, byte for byte the same 42-byte message
	# with the limit and without - and at EXIT=124 swtpm_ioctl has said nothing
	# yet, because its own word on the matter would only come at its 20 s.
	rc=0
	timeout 10 swtpm_ioctl -v --unix "$sock.ctrl" || rc=$?
	if [ "$rc" -eq 124 ]; then
		printf 'Der flüchtige Zustand der Instanz qemu lässt sich nicht sichern (EXIT=124):\n' >&2
		printf 'der Steuerkanal antwortet nach 10 s nicht. Zwei Ursachen kommen in Frage.\n' >&2
		printf '1. Die virtuelle Maschine läuft noch und hält den Kanal.\n' >&2
		printf '   Nachsehen: pgrep -f qemu-system-x86_64, beenden: tools/recovery-qemu.sh stop\n' >&2
		printf '2. Die Maschine ist weg, der Simulator antwortet trotzdem nicht.\n' >&2
		printf '   Nachsehen: ps -o stat= -p "$(cat %s)" - ein T heißt angehalten.\n' "$pidfile" >&2
		exit 1
	fi
	if [ "$rc" -ne 0 ]; then
		printf 'Der flüchtige Zustand der Instanz qemu lässt sich nicht sichern (EXIT=%s).\n' "$rc" >&2
		printf 'Die Zeilen darüber sagen, woran es lag.\n' >&2
		exit 1
	fi
	if [ ! -s "$dir/tpm2-00.volatilestate" ]; then
		printf 'swtpm hat keinen flüchtigen Zustand hinterlegt: %s\n' "$dir/tpm2-00.volatilestate" >&2
		exit 1
	fi
	stop
	datenkanal=ja
	start
	# Without this the simulator sits there uninitialised: it was started with
	# no --flags on purpose, so the state just saved gets loaded here instead of
	# a fresh startup clearing the very PCRs this is about.
	# Same limit for the same reason: this control channel is fresh, but nothing
	# in the code guarantees that nobody else got to it first, and a wait whose
	# end belongs to a foreign tool is not one to leave standing.
	rc=0
	timeout 10 swtpm_ioctl -i --unix "$sock.ctrl" || rc=$?
	if [ "$rc" -eq 124 ]; then
		printf 'Der gesicherte Zustand lässt sich nicht wieder einspielen (EXIT=124):\n' >&2
		printf 'der Steuerkanal antwortet nach 10 s nicht. Zwei Ursachen kommen in Frage.\n' >&2
		printf '1. Es hält noch jemand anders den Kanal dieses eben gestarteten Simulators.\n' >&2
		printf '2. Der Simulator selbst antwortet nicht.\n' >&2
		printf '   Nachsehen: ps -o stat= -p "$(cat %s)" - ein T heißt angehalten.\n' "$pidfile" >&2
		exit 1
	fi
	if [ "$rc" -ne 0 ]; then
		printf 'Der gesicherte Zustand lässt sich nicht wieder einspielen (EXIT=%s).\n' "$rc" >&2
		printf 'Die Zeilen darüber sagen, woran es lag.\n' >&2
		exit 1
	fi
}

stop() {
	local pid
	pid="$(swtpm_pid)"
	if [ -z "$pid" ]; then
		rm -f "$pidfile"
		return 0
	fi
	# kill's own reason stays visible on purpose: "not permitted" and "already
	# gone" are two different answers, and swallowing stderr made them one.
	if ! kill "$pid"; then
		if is_this_swtpm "$pid"; then
			printf 'Der Simulator %s (PID %s) lässt sich nicht beenden. Die Zeile darüber sagt, warum.\n' \
				"$name" "$pid" >&2
			exit 1
		fi
		rm -f "$pidfile"
		return 0
	fi
	# Waited out, not fired and forgotten: kill only asks. When stop returns,
	# the simulator has to be gone - reset deletes the state directory in the
	# very next line, and start puts a new one on it. Measured, same shape as
	# the other pass budgets in this package: 0 extra passes in every run, 9-12
	# ms idle and 26-37 ms with sixteen busy processes, against 100 passes.
	for _ in $(seq 100); do
		if ! is_this_swtpm "$pid"; then break; fi
		sleep 0.1
	done
	if is_this_swtpm "$pid"; then
		printf 'Der Simulator %s endet nicht (PID %s). Von Hand beenden: kill -9 %s\n' \
			"$name" "$pid" "$pid" >&2
		exit 1
	fi
	rm -f "$pidfile"
}

reset() {
	stop
	rm -rf "$dir"
	if [ "$name" = main ]; then
		# Whoever switches the recording on takes it away again, and rm is not
		# how a file with a secret in it goes away. It normally has been shredded
		# by its caller long before this line; what is caught here is the run
		# that died in between, or a machine that was restarted under it.
		shred -u "$work/work/wire-$name.log" 2>/dev/null || :
		# A second run has to start from nothing. Careful: work/ is NOT per
		# instance the way tpm/<instanz>/ above is - it is the shared working
		# surface, and this empties it for everyone. Nothing may be parked there.
		rm -rf "$work/work"
		mkdir -p "$work/work"
	fi
	start
}

case "$1" in
start) start ;;
stop) stop ;;
reset) reset ;;
resume) resume ;;
tcti) printf 'swtpm:path=%s\n' "$sock" ;;
*) usage ;;
esac
