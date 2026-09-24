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
	printf 'Aufruf: tools/recovery-swtpm.sh start|stop|reset|tcti [main|qemu]\n' >&2
	exit 1
}

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then usage; fi
name="${2:-main}"
case "$name" in
main|qemu) ;;
*) printf 'Unbekannte Instanz: %s (erlaubt: main, qemu)\n' "$name" >&2; exit 1 ;;
esac
dir="$work/tpm/$name"
sock="$dir/sock"
pidfile="$dir/swtpm.pid"

# True only for a live swtpm that carries THIS instance's state directory on
# its own command line. A number on its own proves nothing: it is handed out
# again, and any process may merely mention the directory. Measured: ps prints
# the whole line here, no truncation.
is_this_swtpm() {        # $1 = PID
	local args
	[ -n "${1:-}" ] || return 1
	args="$(ps -p "$1" -o args= 2>/dev/null)" || return 1
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
	# this state directory, and reset would pull it out from under it. Every
	# hit is asked, not just the first - pgrep hands back the oldest one first,
	# and that can be a stranger.
	if found="$(pgrep -f "tpmstate dir=$dir")"; then
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
	local pid flags=()
	mkdir -p "$dir" "$work/work"
	pid="$(swtpm_pid)"
	if [ -n "$pid" ] && [ -S "$sock" ]; then
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
	# returned, and by rename, so it is there wholly or not at all.
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
	if ! swtpm socket --tpm2 --tpmstate "dir=$dir" --server "type=unixio,path=$sock" \
		--ctrl "type=unixio,path=$sock.ctrl" "${flags[@]}" --daemon --pid "file=$pidfile"; then
		printf 'Der Simulator %s lässt sich nicht starten. Die Zeilen darüber sagen, woran es lag.\n' "$name" >&2
		printf 'Hilft das nicht weiter: Zustand wegwerfen mit tools/recovery-swtpm.sh reset %s\n' "$name" >&2
		exit 1
	fi
	# Guards against a machine slower than this one: measured, this swtpm has
	# its socket bound before it returns. What decides is the check below.
	for _ in $(seq 50); do
		if [ -S "$sock" ]; then break; fi
		sleep 0.1
	done
	if [ ! -S "$sock" ]; then
		printf 'Der Simulator %s ist nicht gestartet: %s\n' "$name" "$dir" >&2
		printf 'Prüfen: swtpm --version, Rechte auf dem Verzeichnis, alter Socket im Weg\n' >&2
		exit 1
	fi
	ask_tpm
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
	# very next line, and start puts a new one on it.
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
tcti) printf 'swtpm:path=%s\n' "$sock" ;;
*) usage ;;
esac
