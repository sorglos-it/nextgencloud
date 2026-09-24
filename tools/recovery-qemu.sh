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

# True only for a live simulator of the instance qemu: the number out of its pid
# file, put to the same question recovery-swtpm.sh puts it to - the state
# directory has to stand on the command line. The pid file alone, without the
# process list behind it, because reset wrote it moments ago in this same run.
# A look at that file and one ps, and -ww for the same reason as above -
# measured, that line is 233 characters long and COLUMNS=80 cuts it to 80.
qemu_swtpm_alive() {
	local pid args
	pid="$(cat "$work/tpm/qemu/swtpm.pid" 2>/dev/null || :)"
	[ -n "$pid" ] || return 1
	args="$(ps -ww -p "$pid" -o args= 2>/dev/null)" || return 1
	case "$args " in
	"swtpm "*" --tpmstate dir=$work/tpm/qemu "*) return 0 ;;
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
		# No waiting time behind this, and that is measured: the loop above only
		# ends once the process is gone from the list, and a process that is
		# gone has had its end of the control channel closed by the kernel. So
		# resume can have it at once - measured on this VM in 9 runs, idle and
		# with all four cores under load, swtpm_ioctl answers EXIT=0 in 2-7 ms
		# right afterwards. A fixed second here claimed a need that is not
		# there.
	fi
	rm -f "$pidfile"
}

check() {
	local sock answer model grenze fertig
	# Only check needs an environment; stop calls nothing that this check looks
	# for. Asked here and not at the top of the file for a measured reason: with
	# werkzeuge.txt missing, stop used to die on this line, EXIT=1, and the
	# running machine stayed exactly where it was. Tidying up must not depend
	# on a complete environment.
	tools/recovery-env.sh
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
	# -pidfile is what is_our_qemu knows the VM by; where it stands does not
	# matter, but without it stop no longer finds the machine.
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

	# The question to QMP waits for nothing: QEMU serves the monitor from the
	# moment it is up, and the TPM back end comes with the machine. There used
	# to be a sleep 30 behind the two commands, and it was paid EVERY time -
	# socat only sees EOF when that sleep ends, quick machine or not. It is gone:
	# QEMU closes the line after our EOF, and socat then ends without sitting
	# out its half second (its own manual says so, and measured, 9 runs, the
	# whole exchange takes 5-16 ms). An answer cut short would leave model
	# empty, and that is caught below.
	answer="$(printf '%s\n' '{"execute":"qmp_capabilities"}' '{"execute":"query-tpm"}' \
		| socat - "UNIX-CONNECT:$qmp")"

	# The firmware is what has to be waited for, and on what it says, not on the
	# clock: BdsDxe is the boot phase, and by the time it speaks PCR 0 is
	# finished. Measured, not reasoned: on this VM (4 emulated cores, QEMU
	# 10.0.13, swtpm 0.7.1, no KVM) the line comes after 4.3-4.5 s idle and 6.1 s
	# with all four cores under load, and in 9 runs PCR 0 read out right at it
	# was bit for bit the value the old 30 s wait produced. The limit below is
	# no waiting time - whoever gets there sooner goes on at once - it is the
	# stop for a firmware that never reports at all, and about ten times the
	# slowest run measured.
	grenze=60                # Sekunden
	fertig=""
	for _ in $(seq "$((grenze * 10))"); do
		if grep -q 'BdsDxe:' "$serial" 2>/dev/null; then fertig=ja; break; fi
		sleep 0.1
	done
	if [ -z "$fertig" ]; then
		say "Die Firmware meldet auch nach $grenze s nicht das Ende ihrer Startphase."
		say "Prüfen: was steht in $serial?"
		stop_qemu
		exit 1
	fi
	stop_qemu
	# The whole proof hangs on the simulator outliving the machine - that is
	# what the SIGKILL above is for. Asked right here, where the requirement is,
	# so that a stop_qemu turned into an orderly end says so at this line
	# instead of failing two steps further on. Costs one ps.
	if ! qemu_swtpm_alive; then
		say "Der Simulator der Instanz qemu hat die Maschine nicht überlebt."
		say "Das Ende der Maschine muss SIGKILL sein: bei einem geordneten Ende schickt"
		say "QEMU CMD_SHUTDOWN über den Steuerkanal, und swtpm beendet sich mitsamt den"
		say "gemessenen PCRs. Prüfen: steht in stop_qemu noch kill -9?"
		exit 1
	fi

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
		say "An der Zeit liegt es nicht: die Firmware hat das Ende ihrer Startphase"
		say "gemeldet. Sagt $serial sonst etwas?"
		exit 1
	fi
	say "Firmware hat gemessen: PCR 0 ist nicht mehr leer"
}

case "${1:-}" in
check) check ;;
stop) stop_qemu ;;
*)
	printf 'Aufruf: tools/recovery-qemu.sh check|stop\n' >&2
	printf 'stop beendet nur die Maschine; den Simulator der Instanz qemu beendet tools/recovery-swtpm.sh stop qemu.\n' >&2
	exit 1
	;;
esac
