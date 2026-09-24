#!/usr/bin/env bash
# Proves that QEMU boots the secure-boot OVMF with the simulator attached as a
# TPM and that the firmware really measures into it. The signed images come
# with work package 8; here only the plumbing is checked.
set -euo pipefail
# Same reason as in the sibling scripts: without it a x=$(f) for a
# multi-statement f only weighs f's LAST command for errexit. Declaring and
# assigning in one local statement stays exempt regardless, so every local
# below is declared on its own line.
shopt -s inherit_errexit
cd "$(dirname "$0")/.."
tools/recovery-env.sh
work="$(pwd)/dist/recovery"
vars="$work/ovmf/OVMF_VARS_4M.fd"
qmp="$work/work/qmp.sock"
pidfile="$work/work/qemu.pid"
serial="$work/work/qemu-serial.log"

say() { printf '%s\n' "$*" >&2; }

# True only for a live QEMU that carries THIS script's pid file on its own
# command line. The number out of the file proves nothing by itself: a pid
# file outlives its process, numbers are handed out again, and stop would then
# kill a stranger. -ww is not decoration: without it ps cuts the line to the
# width in COLUMNS, and measured in the sibling script an exported COLUMNS=80
# cut a running process out of sight - stop then reported success over a VM
# that was still running.
is_our_qemu() {          # $1 = PID
	local args
	[ -n "${1:-}" ] || return 1
	args="$(ps -ww -p "$1" -o args= 2>/dev/null)" || return 1
	case "$args " in
	*"qemu-system-x86_64 "*" -pidfile $pidfile "*) return 0 ;;
	esac
	return 1
}

# SIGKILL, and that is the whole point, not impatience. A QEMU that gets to end
# in an orderly way sends CMD_SHUTDOWN down the control channel, and swtpm obeys
# and exits - taking the measured PCRs with it. Measured, all three ways: QMP
# quit and SIGTERM both kill the simulator, only SIGKILL leaves it standing, and
# only then can resume read the PCRs out. The machine has nothing to lose here:
# the firmware volume is read-only and the variable file is written straight
# through.
stop_qemu() {
	local pid
	if [ ! -f "$pidfile" ]; then return 0; fi
	pid="$(cat "$pidfile" 2>/dev/null || :)"
	if is_our_qemu "$pid"; then
		kill -9 "$pid"
		for _ in $(seq 100); do
			if ! is_our_qemu "$pid"; then break; fi
			sleep 0.1
		done
		if is_our_qemu "$pid"; then
			say "QEMU endet nicht (PID $pid). Von Hand beenden: kill -9 $pid"
			exit 1
		fi
		# Not cosmetic: resume needs the control channel of the simulator to
		# itself, and a QEMU already reaped still has its end of it for a
		# moment. Without this wait the next step hangs.
		sleep 1
	fi
	rm -f "$pidfile"
}

check() {
	local sock answer model
	mkdir -p "$work/ovmf" "$work/work"
	stop_qemu
	# A guest moves the PCRs it is measured into. Sharing the simulator with the
	# policy tests would change exactly the PCR state they seal against.
	tools/recovery-swtpm.sh reset qemu
	sock="$work/tpm/qemu/sock"
	if [ ! -f "$vars" ]; then cp /usr/share/OVMF/OVMF_VARS_4M.ms.fd "$vars"; fi
	rm -f "$qmp" "$serial"

	# -tpmdev emulator speaks swtpm's control protocol, so the chardev points at
	# the control socket, not at the data socket. -nographic and -daemonize do
	# not work together; -display none with the serial line in a file does.
	# No accel=kvm: this host passes no virtualisation through, QEMU emulates.
	# -pidfile stays last: is_our_qemu recognises the VM by exactly this tail.
	qemu-system-x86_64 -machine q35,smm=on -m 1024 -no-reboot -display none \
		-global driver=cfi.pflash01,property=secure,value=on \
		-drive "if=pflash,format=raw,unit=0,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.secboot.fd" \
		-drive "if=pflash,format=raw,unit=1,file=$vars" \
		-chardev "socket,id=chrtpm,path=$sock.ctrl" \
		-tpmdev emulator,id=tpm0,chardev=chrtpm \
		-device tpm-tis,tpmdev=tpm0 \
		-serial "file:$serial" \
		-qmp "unix:$qmp,server,nowait" -daemonize -pidfile "$pidfile"

	for _ in $(seq 100); do
		if [ -S "$qmp" ]; then break; fi
		sleep 0.1
	done
	if [ ! -S "$qmp" ]; then
		say "QEMU hat keinen QMP-Socket angelegt: $qmp"
		stop_qemu
		exit 1
	fi

	# Emulated firmware needs its time to come up and measure; 30 seconds are
	# plenty on this machine and cost nothing when it is quicker.
	answer="$({ printf '%s\n' '{"execute":"qmp_capabilities"}' '{"execute":"query-tpm"}'; sleep 30; } \
		| socat - "UNIX-CONNECT:$qmp")"
	stop_qemu

	model="$(printf '%s' "$answer" | grep -o '"model": *"[^"]*"' | head -1 | cut -d'"' -f4)"
	if [ -z "$model" ]; then
		say "QEMU meldet kein TPM."
		say "Prüfen: läuft swtpm (tools/recovery-swtpm.sh start qemu)?"
		say "Zeigt der chardev auf $sock.ctrl und nicht auf $sock?"
		exit 1
	fi
	say "TPM-Gerät an QEMU: $model"

	# The stronger proof: the firmware has extended PCR 0. An untouched TPM has
	# it at all zero, so anything else can only come from the guest. The
	# simulator carried the machine without a data channel of its own, so it
	# only gets one now - resume writes the volatile state out, restarts with
	# the channel and plays the state back in. Without that step the PCRs are
	# gone with the machine.
	tools/recovery-swtpm.sh resume qemu
	# Assigned and exported on separate lines, and only ever the qemu instance:
	# this machine has a TPM of its own, and an unset TCTI would quietly ask
	# that one.
	TPM2TOOLS_TCTI="$(tools/recovery-swtpm.sh tcti qemu)"
	export TPM2TOOLS_TCTI
	tpm2_pcrread -o "$work/work/qemu-pcr0.bin" sha384:0 >/dev/null
	if [ -z "$(xxd -p -c 96 "$work/work/qemu-pcr0.bin" | tr -d '0\n')" ]; then
		say "Die Firmware hat nicht ins TPM gemessen: PCR 0 ist noch leer."
		say "Prüfen: zeigt der chardev auf das Steuerprotokoll ($sock.ctrl)?"
		say "Sagt $serial etwas? Hat die emulierte Firmware genug Zeit bekommen?"
		exit 1
	fi
	say "Firmware hat gemessen: PCR 0 ist nicht mehr leer"
}

case "${1:-}" in
check) check ;;
stop) stop_qemu ;;
*) printf 'Aufruf: tools/recovery-qemu.sh check|stop\n' >&2; exit 1 ;;
esac
