#!/usr/bin/env bash
# One Debian VM under Proxmox that carries ALL roles - the small case.
#
# Why this script exists at all: the first single machine was a hand build,
# grown over months, its test store in a home directory, binaries placed by
# hand. It measured a lot of real things, and then it was switched off and
# nobody could build it again. A measurement on a machine nobody can rebuild
# says little about what a user experiences. So the replacement is a script.
#
#   bash tools/einzelmaschine.sh pruefen    # hold every value against the host
#   bash tools/einzelmaschine.sh anlegen    # clone the template and start it
#   bash tools/einzelmaschine.sh abbauen    # remove it again
#
# Runs ON the Proxmox host, or over ssh from anywhere via NGC_PVE_SSH.
# Add --trocken to any command to print what it would do and change nothing.
#
# The values come from tools/proxmox-sechs.sh, where each one is measured and
# carries its origin above it. This script deliberately repeats none of that
# reasoning: it reads the same defaults and may be overridden the same way.
set -u -o pipefail

ZONE=${NGC_ZONE:-mein.lan}

STORAGE=${NGC_PVE_STORAGE:-ssd_2TB}
BRIDGE=${NGC_PVE_BRIDGE:-vmbr0}
FIREWALL=${NGC_PVE_FIREWALL:-1}
VORLAGE_ID=${NGC_PVE_TEMPLATE_ID:-300}
CIDR=${NGC_PVE_CIDR:-8}
GATEWAY=${NGC_PVE_GATEWAY:-10.0.0.1}
CPUTYPE=${NGC_PVE_CPU:-x86-64-v2-AES}

# VMID 307 and 10.2.3.216 were free on 24.10.2026 ("qm list", and 216-220
# answered no ping). The six of the distributed installation hold 301-306.
VMID=${NGC_EIN_VMID:-307}
IP=${NGC_EIN_IP:-10.2.3.216}
NAME=${NGC_EIN_NAME:-ein}

# It carries every role at once, so it needs more than any single one of the
# six: web+api 2048, db 2560, services 3072 there. 4096 MiB is above the
# largest and below the sum - the small case runs one API, one controller and
# SQLite, not six servers. The host had 13 GiB available of 62 when this was
# written, so this fits with room left.
MEM=${NGC_EIN_MEM:-4096}
CORES=${NGC_EIN_CORES:-2}
DISK=${NGC_EIN_DISK:-32}
# Second disk for the same reason the services nodes get one: podman images
# (the terminal image alone is 108 MB) and an activation spooling a package of
# up to store.MaxPackage = 4 GiB. It is also what lets fscrypt be measured
# without touching the mounted root filesystem.
EXTRA=${NGC_EIN_EXTRA:-16}

SSH=${NGC_PVE_SSH:-}
trocken=0

fehler() { printf 'einzelmaschine: %s\n' "$*" >&2; exit 1; }

# Every call to the host goes through here, so --trocken cannot leak a real
# change and the ssh case needs no second code path.
lauf() {
	if [ "$trocken" = 1 ]; then printf '  %s\n' "$*"; return 0; fi
	if [ -n "$SSH" ]; then ssh -o BatchMode=yes "$SSH" "$*"; else "$@"; fi
}
still() {
	if [ -n "$SSH" ]; then ssh -o BatchMode=yes "$SSH" "$*" 2>/dev/null; else "$@" 2>/dev/null; fi
}

# A command ON THE NEW VM, not on the Proxmox host. Its own function because
# "still ssh root@IP ..." looked right and was not: with NGC_PVE_SSH set, the
# arguments are joined into one string, the inner quoting falls apart and
# everything after the first ";" runs on the HOST. The readiness check then
# printed the host's kernel and its 20 cores and called the VM ready. One
# level of quoting is added here explicitly, so what comes back is measured on
# the machine it claims to be measured on.
# The command travels as base64, which is letters, digits, "+", "/" and "=" and
# therefore survives any number of shells unchanged. Quoting it by hand does
# not: "printf %q" is right for one hop and loses a level on two, which showed
# up as an empty $PRETTY_NAME while $(uname -r) still worked - half the command
# interpreted here, half there.
# The command travels as base64 ON STDIN, and the remote command line is a
# fixed string that carries none of it. That is the only shape that survives
# two hops: anything put on the command line is taken apart by every shell it
# passes, and a pipe or a semicolon in it is executed by the FIRST one. Both
# earlier attempts failed exactly there - "printf %q" lost a level (empty
# $PRETTY_NAME beside a working $(uname -r)), and a piped base64 on the
# command line ran the whole pipeline on the Proxmox host, which then
# cheerfully reported its own 20 cores as the VM's.
auf_vm() {
	local fern='base64 -d | sh'
	if [ -n "$SSH" ]; then
		printf '%s' "$1" | base64 | tr -d '\n' | ssh -o BatchMode=yes "$SSH" \
			"ssh -o ConnectTimeout=4 -o BatchMode=yes -o StrictHostKeyChecking=no root@$IP '$fern'" 2>/dev/null
	else
		printf '%s' "$1" | base64 | tr -d '\n' | \
			ssh -o ConnectTimeout=4 -o BatchMode=yes -o StrictHostKeyChecking=no "root@$IP" "$fern" 2>/dev/null
	fi
}

pruefen() {
	local fehlt=0
	echo "einzelmaschine: holding the values against the host"

	still qm status "$VORLAGE_ID" >/dev/null ||
		{ echo "  NO template $VORLAGE_ID - run 'bash tools/proxmox-sechs.sh vorlage' first"; fehlt=1; }
	still qm config "$VORLAGE_ID" | grep -q '^template: 1' ||
		{ echo "  VMID $VORLAGE_ID is not a template"; fehlt=1; }

	if still qm status "$VMID" >/dev/null; then
		echo "  VMID $VMID is ALREADY taken - set NGC_EIN_VMID to a free one"; fehlt=1
	else
		echo "  VMID $VMID free"
	fi

	# A ping that answers is the one thing that must stop this script: two
	# machines on one address break both, and the second one looks broken.
	if still ping -c1 -W1 "$IP" >/dev/null; then
		echo "  $IP ANSWERS a ping - something is already there"; fehlt=1
	else
		echo "  $IP free"
	fi

	still pvesm status | grep -q "^$STORAGE " ||
		{ echo "  no storage $STORAGE"; fehlt=1; }

	local avail
	avail=$(still free -m | awk '/^Mem:/{print $7}')
	if [ -n "$avail" ] && [ "$avail" -lt "$MEM" ]; then
		echo "  only ${avail} MiB available, the VM wants $MEM"; fehlt=1
	else
		echo "  RAM ok (${avail:-?} MiB available, $MEM wanted)"
	fi

	[ "$fehlt" = 0 ] || fehler "not all values hold - nothing was changed"
	echo "einzelmaschine: pruefen ok"
}

anlegen() {
	pruefen
	lauf qm clone "$VORLAGE_ID" "$VMID" --name "ngc-$NAME" --full 1 --storage "$STORAGE"
	lauf qm set "$VMID" --memory "$MEM" --cores "$CORES" --cpu "$CPUTYPE" --onboot 1 \
		--net0 "virtio,bridge=$BRIDGE,firewall=$FIREWALL" \
		--ipconfig0 "ip=$IP/$CIDR,gw=$GATEWAY"
	lauf qm disk resize "$VMID" scsi0 "${DISK}G"
	[ "$EXTRA" = 0 ] || lauf qm set "$VMID" --scsi1 "$STORAGE:${EXTRA},iothread=1"
	lauf qm start "$VMID"
	printf 'angelegt: ngc-%s (VMID %s) -> %s, alle Rollen\n' "$NAME" "$VMID" "$IP"
	[ "$trocken" = 1 ] && return 0

	# Started is not reachable. Without this the script reports success on a
	# machine that never came up, and the next step measures the silence.
	local i wer
	for i in $(seq 1 30); do
		wer=$(auf_vm 'hostname')
		if [ "$wer" = "ngc-$NAME" ]; then
			echo "einzelmaschine: ssh answers after ~$((i * 5))s"
			auf_vm '. /etc/os-release; echo "  $PRETTY_NAME, kernel $(uname -r), $(nproc) cores, $(free -m | awk "/^Mem:/{print \$2}") MiB"'
			return 0
		fi
		# A reachable machine with the WRONG name is worse than silence: it
		# means the probe went somewhere else, and every later measurement
		# would be about that other machine.
		[ -z "$wer" ] || fehler "ssh on $IP answers as '$wer', expected 'ngc-$NAME'"
		sleep 5
	done
	fehler "VM $VMID runs but does not answer ssh on $IP after 150s"
}

abbauen() {
	still qm status "$VMID" >/dev/null || fehler "there is no VMID $VMID"
	still qm config "$VMID" | grep -q "^name: ngc-$NAME" ||
		fehler "VMID $VMID is not ngc-$NAME - refusing to remove someone else's VM"
	lauf qm stop "$VMID" || true
	lauf qm destroy "$VMID" --purge 1
	echo "abgebaut: VMID $VMID"
}

befehl=${1:-}
shift || true
for a in "$@"; do [ "$a" = --trocken ] && trocken=1; done

case "$befehl" in
	pruefen) pruefen ;;
	anlegen) anlegen ;;
	abbauen) abbauen ;;
	*) sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
