#!/usr/bin/env bash
# Starts, stops and resets the TPM simulator, and names the socket the tpm2
# tools are to talk to. There are two instances: "main" carries the policy
# tests, "qemu" belongs to the virtual machine - a guest moves the PCRs, and
# one shared simulator would change the very PCR state the policy is sealed
# against.
set -euo pipefail
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

# Writes the PID of THIS instance's simulator, or nothing at all. Two sources,
# because either one can be missing on its own: the pid file swtpm writes, and
# the process list. Measured: a pid file outlives its daemon, and a number that
# has been handed out again lets any other process pass for the simulator - so
# the program name is asked for as well. Only ever returns, never exits: it
# runs inside a $( ), where an exit would end just the subshell.
swtpm_pid() {
	local candidate="" procname="" found=""
	if [ -f "$pidfile" ]; then
		candidate="$(cat "$pidfile" 2>/dev/null || :)"
		if [ -n "$candidate" ] && procname="$(ps -p "$candidate" -o comm= 2>/dev/null)" \
			&& [ "$procname" = swtpm ]; then
			printf '%s\n' "$candidate"
			return 0
		fi
	fi
	# No usable pid file does not mean no daemon: one can still sit on this
	# state directory, and reset would pull the directory out from under it.
	# The pattern carries the directory, which is what tells main and qemu
	# apart; measured, each pattern finds only its own instance. Trimmed to one
	# line because a second simulator on the same directory cannot exist -
	# libtpms locks it - so a further line could only be a foreign process.
	if found="$(pgrep -f "tpmstate dir=$dir")"; then
		printf '%s\n' "${found%%$'\n'*}"
	fi
	return 0
}

start() {
	local pid flags=()
	mkdir -p "$dir" "$work/work"
	if [ ! -f "$dir/tpm2-00.permall" ]; then
		# --createek is enough here. The EK certificate needs root and
		# /var/lib/swtpm-localca, and checking it against the manufacturer CA
		# belongs to work package 3. The SHA-384 bank has to be asked for.
		swtpm_setup --tpm2 --tpm-state "$dir" --createek --pcr-banks sha256,sha384 --overwrite
	fi
	# Running WITH its socket is the only state that counts as started.
	# Running without one is a half state, and it has to be ended first,
	# because a second simulator on the same directory refuses to start.
	pid="$(swtpm_pid)"
	if [ -n "$pid" ]; then
		if [ -S "$sock" ]; then return 0; fi
		stop
	fi
	if [ "$name" = main ]; then
		# The policy instance has to answer straight away. The qemu instance
		# gets no such flags: QEMU initialises that TPM itself over the
		# control channel, so it must not be started up in advance.
		flags=(--flags not-need-init,startup-clear)
	fi
	if ! swtpm socket --tpm2 --tpmstate "dir=$dir" --server "type=unixio,path=$sock" \
		--ctrl "type=unixio,path=$sock.ctrl" "${flags[@]}" --daemon --pid "file=$pidfile"; then
		printf 'Der Simulator %s lässt sich nicht starten. Die Zeile darüber sagt, woran es lag.\n' "$name" >&2
		printf 'Häufigster Grund: auf %s läuft noch ein alter. Beenden mit: tools/recovery-swtpm.sh stop %s\n' \
			"$dir" "$name" >&2
		exit 1
	fi
	# A guard, not a fix: measured, this swtpm hands its socket over before it
	# returns. The check after the loop is what decides.
	for _ in $(seq 50); do
		if [ -S "$sock" ]; then break; fi
		sleep 0.1
	done
	if [ ! -S "$sock" ]; then
		printf 'Der Simulator %s ist nicht gestartet: %s\n' "$name" "$dir" >&2
		printf 'Prüfen: swtpm --version, Rechte auf dem Verzeichnis, alter Socket im Weg\n' >&2
		exit 1
	fi
	if [ "$name" = main ]; then
		# The socket on its own proves nothing - see the note on absolute
		# paths above. This asks the TPM itself, and it asks through
		# recovery-env.sh, which owns the SHA-384 check; nothing rebuilds it.
		TPM2TOOLS_TCTI="swtpm:path=$sock" tools/recovery-env.sh pcr-bank
	fi
}

stop() {
	local pid
	pid="$(swtpm_pid)"
	if [ -z "$pid" ]; then
		rm -f "$pidfile"
		return 0
	fi
	if ! kill "$pid" 2>/dev/null; then
		# Gone between looking it up and ending it. Nothing left to do.
		rm -f "$pidfile"
		return 0
	fi
	# Waited out, not fired and forgotten: kill only asks. When stop returns,
	# the simulator has to be gone - reset deletes the state directory in the
	# very next line, and start puts a new one on it.
	for _ in $(seq 100); do
		if ! ps -p "$pid" >/dev/null; then break; fi
		sleep 0.1
	done
	if ps -p "$pid" >/dev/null; then
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
		# A second run has to start from nothing: sessions, contexts, branch
		# digests and the sealed object all belong to the TPM just thrown
		# away. Only for main, and only this instance's state directory goes
		# above - a reset of one instance never reaches into the other.
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
