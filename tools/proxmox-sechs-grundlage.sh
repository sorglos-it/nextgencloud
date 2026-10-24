#!/usr/bin/env bash
# proxmox-sechs-grundlage - what the six Debian VMs need BEFORE ngc-db, ngc-api,
# ngc-web and ngc-controller are run on them.
#
# It is a second file and not a part of tools/proxmox-sechs.sh on purpose: that
# one only talks to Proxmox (qm), this one only talks to the machines (ssh).
# A run that mixes the two cannot say which half failed.
#
# Usage, as root ON THE PROXMOX HOST (or anywhere that reaches the six by ssh):
#   proxmox-sechs-grundlage.sh pakete     the packages each role needs
#   proxmox-sechs-grundlage.sh platte     the encrypted apps disk of the services nodes
#   proxmox-sechs-grundlage.sh messen     report, change nothing
#
# THE TWO THINGS THIS FILE EXISTS FOR
#
# 1 fscrypt. An app of NGC lives in an encrypted directory under
#   config.AppDataDir (/var/lib/ngc/apps), and the controller does that with the
#   kernel's own ioctls - FS_IOC_ADD_ENCRYPTION_KEY and
#   FS_IOC_SET_ENCRYPTION_POLICY in
#   apps/server-controller/classes/runtime/data_linux.go. On a filesystem that
#   cannot encrypt, every one of them answers ENOTTY/EOPNOTSUPP, the controller
#   turns that into runtime.ErrNoFscrypt and EVERY activation ends there
#   (classes/serve/activate.go says so at the line).
#
#   MEASURED, 23./24.10.2026, and this is the part a fresh Debian does NOT bring:
#     - the KERNEL can do it. Debian 13 trixie, 6.12.113+deb13-amd64:
#       /sys/fs/ext4/features/encryption exists.
#     - the FILESYSTEM does not. /etc/mke2fs.conf of Debian 13 lists for ext4
#       "has_journal,extent,huge_file,flex_bg,metadata_csum,metadata_csum_seed,
#       64bit,dir_nlink,extra_isize,orphan_file" - no "encrypt". An ext4 made
#       with mkfs.ext4 and no further flag therefore CANNOT encrypt a
#       directory. Measured twice: once on a loop image with e2fsprogs 1.47.2
#       (the version Debian 13 ships) where mkfs left the feature out and
#       "tune2fs -O encrypt" put it in, and once on the running NGC VM, whose
#       root carries "encrypt" because somebody set it there by hand.
#     - the PACKAGE fscrypt is not needed. It is installed on the running VM,
#       and NGC never calls it: the controller does the ioctls itself. What is
#       needed is the filesystem feature, nothing else.
#
#   WHY A SECOND DISK AND NOT tune2fs ON THE ROOT. "tune2fs -O encrypt" on the
#   MOUNTED root of a running machine is the one thing in this whole setup that
#   was NOT measured here, and a flag on the filesystem the machine is running
#   from is a poor place to find out. A second disk, made with
#   "mkfs.ext4 -O encrypt" and mounted at /var/lib/ngc/apps, answers the same
#   question with nothing at risk - and the ioctls are per filesystem, so the
#   directory below the mount is exactly where the controller needs it.
#   If you would rather have it on the root after all: boot the installer's
#   rescue shell, run "tune2fs -O encrypt /dev/sda2" with the root UNMOUNTED,
#   and check with "lsattr -d" afterwards. This script does not do it.
#
# 2 The packages. The ulh scripts install their own
#   (ngc-api: openssl jq curl ca-certificates; ngc-web: apache2 php-fpm and
#   those four; ngc-db: mariadb-server galera-4 mariadb-backup openssl;
#   ngc-controller: those four plus podman where the server carries services),
#   so what is put on here is only what a Debian CLOUD image lacks and the
#   scripts need from the start: a guest agent so Proxmox sees the machine, and
#   the tools the release check of shared/ulh.md names (sha256sum, od, tr, cut,
#   wc, mktemp are in coreutils; jq, openssl and curl are not always).
set -euo pipefail

if [ -z "${BASH_VERSION:-}" ]; then
	echo "grundlage: run with bash, not sh" >&2
	exit 1
fi

probe=$( (exit 7) >/dev/null 2>&1 || echo "$?")
[ "$probe" = 7 ] || {
	echo "grundlage: this shell loses exit codes (the (exit 7) probe answered '$probe')" >&2
	exit 1
}

# The same table as tools/proxmox-sechs.sh, down to the column order, so a
# change in one is visible as a difference to the other.
# name | IP | roles
MASCHINEN=(
	"web-api-1|10.2.3.210|web api"
	"api-2|10.2.3.211|api"
	"db-1|10.2.3.212|db"
	"svc-1|10.2.3.213|services"
	"svc-2|10.2.3.214|services"
	"store|10.2.3.215|-"
)

NUTZER=${NGC_CIUSER:-root}
# config.AppDataDir of shared/config/config.go. Not typed in again anywhere
# below: the one place it stands is here.
APPS=${NGC_APPS_DIR:-/var/lib/ngc/apps}
# The disk tools/proxmox-sechs.sh hangs on the services nodes as scsi1. On a
# virtio-scsi-single controller it is the second disk, so /dev/sdb - which is
# CHECKED before anything is written to it, because a wrong guess here is a
# formatted root disk.
PLATTE=${NGC_APPS_DISK:-/dev/sdb}

fehler() {
	echo "grundlage: $*" >&2
	exit 1
}

feld() { printf '%s' "$1" | cut -d'|' -f"$2"; }

# auf runs a command on one machine. LC_ALL is set because an ssh that forwards
# the client's locale makes every remote bash print a setlocale warning, and a
# warning in the middle of a measurement is a line somebody has to explain.
auf() {
	local ip=$1
	shift
	ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 \
		"$NUTZER@$ip" "LC_ALL=C; export LC_ALL; $*"
}

hat_rolle() {
	case " $1 " in
	*" $2 "*) return 0 ;;
	esac
	return 1
}

# ------------------------------------------------------------------ pakete
pakete() {
	local e name ip rollen want
	for e in "${MASCHINEN[@]}"; do
		name=$(feld "$e" 1)
		ip=$(feld "$e" 2)
		rollen=$(feld "$e" 3)
		# The guest agent and the release-check tools everywhere; podman and
		# the two rootless helpers only where apps run, because on a web, API
		# or database server a container engine is one program more than the
		# server needs (shared/ulh.md).
		want="qemu-guest-agent jq openssl curl ca-certificates"
		if hat_rolle "$rollen" services; then
			want="$want podman uidmap slirp4netns"
		fi
		if [ "$rollen" = "-" ]; then
			# The store is a plain web server with signed files on it. nginx
			# and nothing else: it is NOT part of the installation.
			want="qemu-guest-agent nginx openssl"
		fi
		printf 'pakete: %s (%s): %s\n' "$name" "$ip" "$want"
		auf "$ip" "DEBIAN_FRONTEND=noninteractive apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq $want" >/dev/null ||
			fehler "apt-get failed on $name ($ip)"
		auf "$ip" "systemctl enable --now qemu-guest-agent" >/dev/null 2>&1 || true
	done
	echo "pakete: ok"
}

# ------------------------------------------------------------------ platte
# The encrypted apps disk, only on the nodes that carry services.
platte() {
	local e name ip rollen dev
	for e in "${MASCHINEN[@]}"; do
		rollen=$(feld "$e" 3)
		hat_rolle "$rollen" services || continue
		name=$(feld "$e" 1)
		ip=$(feld "$e" 2)
		# THE CHECK BEFORE THE FORMAT. A wrong device here formats the root
		# disk of the machine. So: the device has to exist, carry no
		# filesystem and no partition table, and NOT be the one the root is
		# on - all four, on the machine itself, right before mkfs.
		dev=$(auf "$ip" "lsblk -ndo NAME,SIZE,TYPE,MOUNTPOINTS $PLATTE 2>/dev/null || true")
		[ -n "$dev" ] || fehler "$name: $PLATTE does not exist (lsblk said nothing)"
		auf "$ip" "root=\$(findmnt -no SOURCE /); case \"\$root\" in ${PLATTE}*) echo BAD;; *) echo OK;; esac" |
			grep -q '^OK$' || fehler "$name: the root filesystem is on $PLATTE - refusing to format it"
		if auf "$ip" "blkid -p $PLATTE >/dev/null 2>&1 && echo USED || echo FREE" | grep -q '^USED$'; then
			echo "platte: $name already has something on $PLATTE, leaving it alone"
		else
			# -O encrypt is the whole point of this file; see the head.
			auf "$ip" "mkfs.ext4 -q -F -L ngc-apps -O encrypt $PLATTE" ||
				fehler "$name: mkfs.ext4 on $PLATTE failed"
			printf 'platte: %s: %s made with -O encrypt\n' "$name" "$PLATTE"
		fi
		# Mounted by LABEL and not by /dev/sdb: a device name can move when a
		# disk is added, and a mount that moved would put the apps on the
		# wrong filesystem without a word.
		auf "$ip" "mkdir -p $APPS && grep -q '^LABEL=ngc-apps' /etc/fstab || printf 'LABEL=ngc-apps %s ext4 defaults 0 2\n' $APPS >>/etc/fstab" ||
			fehler "$name: could not write /etc/fstab"
		auf "$ip" "mountpoint -q $APPS || mount $APPS" || fehler "$name: $APPS could not be mounted"
		# The mode the controller itself uses: every app runs as its own user
		# and has to traverse this directory to reach its own below it
		# (obstacle 5 of October; runtime/data.go makes it 0711).
		auf "$ip" "chmod 0711 $APPS" || fehler "$name: chmod on $APPS failed"
		printf 'platte: %s: %s is mounted\n' "$name" "$APPS"
	done
	echo "platte: ok"
}

# ------------------------------------------------------------------ messen
# Reports and changes nothing. Each line is one question with its answer, so a
# wrong one is visible without reading this file again.
messen() {
	local e name ip rollen fs feat kern
	for e in "${MASCHINEN[@]}"; do
		name=$(feld "$e" 1)
		ip=$(feld "$e" 2)
		rollen=$(feld "$e" 3)
		printf '== %s (%s) roles: %s\n' "$name" "$ip" "$rollen"
		printf '   os      %s\n' "$(auf "$ip" "sed -n 's/^PRETTY_NAME=\"\\(.*\\)\"\$/\\1/p' /etc/os-release")"
		printf '   kernel  %s\n' "$(auf "$ip" "uname -r")"
		# THE PROBE, AND WHY IT IS WRITTEN LIKE THIS. The obvious two tests
		# are both wrong on Debian, measured 24.10.2026 on these very VMs:
		#   zgrep CONFIG_FS_ENCRYPTION=y /proc/config.gz
		#     - there IS no /proc/config.gz on Debian ("No such file or
		#       directory"); the configuration lies in /boot/config-$(uname -r).
		#   [ -d /sys/fs/ext4/features/encryption ]
		#     - that entry is a FILE, not a directory ("-r--r--r-- 1 root root
		#       4096"), so -d is false on a kernel that CAN encrypt.
		# A probe that is wrong in both halves reports "this kernel cannot
		# encrypt" on every Debian machine, which is the opposite of the truth.
		# So: the kernel configuration first, and the sysfs entry with -e.
		kern=$(auf "$ip" "grep -qx CONFIG_FS_ENCRYPTION=y /boot/config-\$(uname -r) 2>/dev/null && echo yes || { [ -e /sys/fs/ext4/features/encryption ] && echo yes || echo no; }")
		printf '   kernel can encrypt ext4: %s\n' "$kern"
		printf '   name    %s -> %s\n' "$(auf "$ip" "hostname")" "$(auf "$ip" "getent hosts ngc.mein.lan | awk '{print \$1}'")"
		if hat_rolle "$rollen" services; then
			fs=$(auf "$ip" "findmnt -no SOURCE,FSTYPE $APPS 2>/dev/null || echo '(not mounted)'")
			printf '   %s on %s\n' "$APPS" "$fs"
			feat=$(auf "$ip" "d=\$(findmnt -no SOURCE $APPS 2>/dev/null); [ -n \"\$d\" ] && tune2fs -l \"\$d\" 2>/dev/null | sed -n 's/^Filesystem features: *//p'")
			case $feat in
			*encrypt*) printf '   encrypt feature: YES (%s)\n' "$feat" ;;
			"") printf '   encrypt feature: cannot tell - %s is not mounted\n' "$APPS" ;;
			*) printf '   encrypt feature: NO - no app can be set up here (%s)\n' "$feat" ;;
			esac
			printf '   podman  %s\n' "$(auf "$ip" "podman --version 2>/dev/null || echo '(none)'")"
		fi
	done
}

case ${1:-} in
pakete) pakete ;;
platte) platte ;;
messen) messen ;;
*)
	sed -n '2,20p' "${BASH_SOURCE[0]}"
	exit 2
	;;
esac
