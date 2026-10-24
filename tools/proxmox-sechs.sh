#!/usr/bin/env bash
# proxmox-sechs - the six Debian VMs of a DISTRIBUTED NGC installation, on a
# Proxmox host, as commands anyone can read before they run them.
#
# WHY SIX AND NOT ONE. tools/erstlauf-maschine.sh installs NGC with ngc-all.sh:
# everything on one machine, SQLite, the store on the same host. That run can
# never reach four of the questions the contracts make promises about, and two
# findings of the week of 19.10.2026 hid behind exactly that:
#   web + api   the interface and ONE api, so the balancer in Apache has a
#               second member and web.json holds a real pin
#   api (2nd)   the only way to reach "a joining API" (join_code) and the bolt
#               "this is the only API of this installation" at all
#   db          only a database on ANOTHER machine shows whether the API lives
#               without MariaDB on its own host (api.json db_nodes over the net)
#   services    carries the apps, podman and the encrypted app directories
#   services 2  the interface lets one PICK a machine when setting an app up and
#               the update round walks several nodes; with one node both are
#               only claimed
#   store       THE STORE DOES NOT BELONG TO THE INSTALLATION. Today it sits on
#               the same machine, and that covered two findings: only from
#               outside does it show whether the API really fetches over the
#               network, whether it checks the certificate, what happens when
#               the store is gone, and whether the rollback guard bites on an
#               older catalog.
#
# WHAT IS MEASURED AND WHAT IS A VALUE TO CHECK. Everything in VALUES below was
# read off the Proxmox host and off the running NGC VM on 23.10.2026; the line
# over each one says where it came from. Nothing here was invented. Read the
# block, change what is yours, then run the script.
#
# Usage, as root ON THE PROXMOX HOST:
#   proxmox-sechs.sh pruefen            check every value against this host
#   proxmox-sechs.sh vorlage            fetch the Debian image, build the template
#   proxmox-sechs.sh anlegen            clone and start the six
#   proxmox-sechs.sh namen [--setzen]   print the hosts block, or put it on the six
#   proxmox-sechs.sh abbauen [--vorlage]  remove the six again
#
# Any action may be prefixed with --trocken: it then only prints the commands.
set -euo pipefail

if [ -z "${BASH_VERSION:-}" ]; then
	echo "proxmox-sechs: run with bash, not sh" >&2
	exit 1
fi

# The leak test of messen.md, in band: a shell that swallows exit codes writes
# FAIL and 0 into the same run and the caller reads the 0.
probe=$( (exit 7) >/dev/null 2>&1 || echo "$?")
[ "$probe" = 7 ] || {
	echo "proxmox-sechs: this shell loses exit codes (the (exit 7) probe answered '$probe')" >&2
	exit 1
}

# ============================================================
# VALUES - READ THIS BLOCK BEFORE YOU RUN ANYTHING
# ============================================================

# Measured: "hostname" on 10.2.3.2 answers "pve". The ssh entry is called
# pve-2/proxmox, the NODE is called pve - and qm wants the node's name.
NODE=${NGC_PVE_NODE:-pve}

# Measured: "pvesm status" lists hdd_20TB_1, hdd_20TB_2, local, local-lvm,
# ssd_1TB, ssd_2TB. The disk of the existing NGC VM (208) lies on ssd_2TB,
# which had 1.03 TiB free - so the six go to the same place.
STORAGE=${NGC_PVE_STORAGE:-ssd_2TB}

# Measured: "qm config 208" has net0 on bridge vmbr0 with firewall=1, and
# "ip -br link show type bridge" lists vmbr0 as the only bridge that is UP with
# a carrier. A bridge called "intern" exists but has NO-CARRIER.
BRIDGE=${NGC_PVE_BRIDGE:-vmbr0}
FIREWALL=${NGC_PVE_FIREWALL:-1}

# Measured: /etc/pve/.vmlist holds 100-116 and 200-208, so 300 and up is free -
# and that is the range Thomas named.
VORLAGE_ID=${NGC_PVE_TEMPLATE_ID:-300}

# Measured: vmbr0 carries 10.2.3.2/8 and the default route goes via 10.0.0.1.
# The /8 is unusual and it is what this network really is - a /24 here would
# cut the VMs off from their gateway.
CIDR=${NGC_PVE_CIDR:-8}
GATEWAY=${NGC_PVE_GATEWAY:-10.0.0.1}

# Measured: /etc/resolv.conf on the host names 10.2.3.102 (the adguard
# container), then 10.2.3.204 and 10.0.0.1, search domain mein.lan.
DNS=${NGC_PVE_DNS:-10.2.3.102}
SEARCH=${NGC_PVE_SEARCH:-mein.lan}

# The name of the installation. The interface answers under its NAME: the
# certificate and the passkeys hang on it, a browser warns over an IP and
# passkeys do not work over one at all. docs/proxmox-sechs.md says what has to
# resolve where.
ZONE=${NGC_ZONE:-ngc.mein.lan}
WEBNAME=${NGC_WEBNAME:-$ZONE}

# vmid | node name (wire.ValidName) | DNS name | IP | MiB | cores | disk GB |
#   extra disk GB | roles
#
# Measured: 10.2.3.210 to 10.2.3.220 answered no ping on 23.10.2026. They are
# NOT reserved anywhere this script can see - if a DHCP range covers them, this
# is the value to change. The host's own convention is "last octet = VMID"
# (208 = 10.2.3.208) and a VMID of 300 has no octet, so the addresses are
# written out instead of computed.
#
# The sizes are argued in docs/proxmox-sechs.md, not guessed. In short: the
# host had 21 GiB of RAM free of 62 (measured "free -g"), so the six fit with
# room to spare; the services nodes get the second disk because they carry
# podman images (the terminal image alone is 108 MB, its unpacked data
# directory 110 MB) and because an activation spools a package of up to
# store.MaxPackage = 4 GiB.
MASCHINEN=(
	"301|web-api-1|api-1.$ZONE|10.2.3.210|2048|2|16|0|web api"
	"302|api-2|api-2.$ZONE|10.2.3.211|2048|2|16|0|api"
	"303|db-1|db-1.$ZONE|10.2.3.212|2560|2|24|0|db"
	"304|svc-1|svc-1.$ZONE|10.2.3.213|3072|2|24|16|services"
	"305|svc-2|svc-2.$ZONE|10.2.3.214|3072|2|24|16|services"
	"306|store|store.$ZONE|10.2.3.215|1024|1|16|0|-"
)

# The store VM carries NO NGC and no controller. That is the whole point of it
# (see the head of this file), and "-" in the roles column says so.

# Measured: cloud.debian.org serves trixie as a genericcloud qcow2, and the
# SHA512SUMS next to it confirmed the downloaded file on 23.10.2026.
ABBILD_URL=${NGC_IMAGE_URL:-https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2}
ABBILD_DIR=${NGC_IMAGE_DIR:-/var/lib/vz/template/qcow}
ABBILD=$ABBILD_DIR/$(basename -- "$ABBILD_URL")

# The account cloud-init makes, and the keys that may sign in as it. BOTH keys
# go in: the Proxmox host's own, because the setup runs from there, and the one
# Thomas's PC already uses for the existing NGC VM, so he can reach the six
# without making a new key. Measured: /root/.ssh/id_rsa.pub exists on the host.
CIUSER=${NGC_CIUSER:-root}
PVE_KEY=${NGC_PVE_KEY:-/root/.ssh/id_rsa.pub}
# The PUBLIC key of ~/Documents/ssh/keys/id_ed25519_ngc on Thomas's PC, read
# there on 23.10.2026. A public key is not a secret; the private one never
# leaves his machine.
PC_KEY=${NGC_PC_KEY:-"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIIXQkob6Q3SI8B9cabPvn+PZInCKJN+yRis+JFHheG5J User@PC01_ngc"}

# Measured: "qm config 208" uses cpu x86-64-v2-AES. The AES part is not
# decoration here - MariaDB with encryption at rest and the TLS of five
# machines run on it.
CPUTYPE=${NGC_PVE_CPU:-x86-64-v2-AES}

# ============================================================

trocken=0
if [ "${1:-}" = --trocken ]; then
	trocken=1
	shift
fi
aktion=${1:-}
zusatz=${2:-}

lauf() {
	if [ "$trocken" = 1 ]; then
		printf '  %s\n' "$*"
		return 0
	fi
	printf '+ %s\n' "$*" >&2
	"$@"
}

fehler() {
	echo "proxmox-sechs: $*" >&2
	exit 1
}

# feld reads one column out of a MASCHINEN entry, so a reader of the calls
# below does not have to count fields.
feld() { printf '%s' "$1" | cut -d'|' -f"$2"; }

# hosts_block builds the lines that make the names resolve.
hosts_block() {
	local e ip dns
	for e in "${MASCHINEN[@]}"; do
		ip=$(feld "$e" 4)
		dns=$(feld "$e" 3)
		printf '%s %s %s\n' "$ip" "$dns" "${dns%%.*}"
	done
	printf '%s %s\n' "$(feld "${MASCHINEN[0]}" 4)" "$WEBNAME"
}

# ------------------------------------------------------------------ pruefen
# Nothing is created here. Every value of the block above is held against this
# host, because a value that is only written down is a value nobody checked.
pruefen() {
	local ok=0 bad=0 e id ip name
	sag() {
		if [ "$1" = ok ]; then
			printf 'ok    %s\n' "$2"
			ok=$((ok + 1))
		else
			printf 'FEHLT %s\n' "$2"
			bad=$((bad + 1))
		fi
	}
	command -v qm >/dev/null 2>&1 || fehler "no qm on this machine - run this on the Proxmox host"
	if [ "$(hostname)" = "$NODE" ]; then
		sag ok "node $NODE"
	else
		sag no "node: this host calls itself $(hostname), the block says $NODE"
	fi
	if pvesm status 2>/dev/null | awk 'NR>1{print $1}' | grep -qx -- "$STORAGE"; then
		sag ok "storage $STORAGE"
	else
		sag no "storage $STORAGE is not in pvesm status"
	fi
	if ip -br link show type bridge 2>/dev/null | awk '{print $1}' | grep -qx -- "$BRIDGE"; then
		sag ok "bridge $BRIDGE"
	else
		sag no "bridge $BRIDGE does not exist on this host"
	fi
	if ip -4 route show default | grep -q -- "$GATEWAY"; then
		sag ok "gateway $GATEWAY"
	else
		sag no "gateway $GATEWAY is not the default route ($(ip -4 route show default))"
	fi
	if [ -f "$PVE_KEY" ]; then
		sag ok "key $PVE_KEY"
	else
		sag no "no public key at $PVE_KEY"
	fi
	for e in "${MASCHINEN[@]}" "$VORLAGE_ID|vorlage|-|-|-|-|-|-|-"; do
		id=$(feld "$e" 1)
		if qm status "$id" >/dev/null 2>&1; then
			sag no "VMID $id is taken: $(qm config "$id" 2>/dev/null | sed -n 's/^name: //p')"
		else
			sag ok "VMID $id free"
		fi
	done
	for e in "${MASCHINEN[@]}"; do
		ip=$(feld "$e" 4)
		name=$(feld "$e" 2)
		if ping -c1 -W1 "$ip" >/dev/null 2>&1; then
			sag no "$ip ($name) answers a ping - something is already there"
		else
			sag ok "$ip ($name) free"
		fi
	done
	printf 'pruefen: %d ok, %d missing\n' "$ok" "$bad"
	[ "$bad" = 0 ] || exit 1
	echo "pruefen: ok"
}

# ------------------------------------------------------------------ vorlage
# The image is fetched ONCE and its checksum is held against Debian's own
# SHA512SUMS before a VM is made from it: a template built from a half download
# is a fault that shows up six machines later.
vorlage() {
	local keys
	lauf mkdir -p -- "$ABBILD_DIR"
	if [ ! -s "$ABBILD" ]; then
		lauf wget -q -O "$ABBILD" -- "$ABBILD_URL"
	fi
	if [ "$trocken" = 0 ]; then
		(
			cd -- "$ABBILD_DIR" &&
				wget -q -O SHA512SUMS -- "$(dirname -- "$ABBILD_URL")/SHA512SUMS" &&
				grep -- "$(basename -- "$ABBILD")" SHA512SUMS | sha512sum -c -
		) || fehler "the checksum of $ABBILD does not match Debian's SHA512SUMS"
	fi
	if qm status "$VORLAGE_ID" >/dev/null 2>&1; then
		echo "proxmox-sechs: VMID $VORLAGE_ID is already there, leaving it alone"
		return 0
	fi
	# serial0 as the console: a Debian cloud image writes its boot to the
	# serial line, and without it "qm terminal" shows nothing at all when
	# something goes wrong early.
	lauf qm create "$VORLAGE_ID" --name ngc-vorlage --machine q35 --ostype l26 \
		--cpu "$CPUTYPE" --cores 2 --memory 2048 --numa 0 \
		--scsihw virtio-scsi-single --agent 1 \
		--serial0 socket --vga serial0 \
		--net0 "virtio,bridge=$BRIDGE,firewall=$FIREWALL"
	lauf qm disk import "$VORLAGE_ID" "$ABBILD" "$STORAGE" --format qcow2
	lauf qm set "$VORLAGE_ID" --scsi0 "$STORAGE:$VORLAGE_ID/vm-$VORLAGE_ID-disk-0.qcow2,iothread=1"
	lauf qm set "$VORLAGE_ID" --ide2 "$STORAGE:cloudinit" --boot order=scsi0
	# Both keys in one file: cloud-init takes a file, not two arguments.
	if [ "$trocken" = 0 ]; then
		keys=$(mktemp)
		cat -- "$PVE_KEY" >"$keys"
		printf '%s\n' "$PC_KEY" >>"$keys"
		qm set "$VORLAGE_ID" --ciuser "$CIUSER" --sshkeys "$keys"
		rm -f -- "$keys"
	else
		printf '  qm set %s --ciuser %s --sshkeys <%s plus the PC key>\n' "$VORLAGE_ID" "$CIUSER" "$PVE_KEY"
	fi
	lauf qm set "$VORLAGE_ID" --nameserver "$DNS" --searchdomain "$SEARCH"
	lauf qm template "$VORLAGE_ID"
	echo "vorlage: ok - template $VORLAGE_ID is there"
}

# ------------------------------------------------------------------ anlegen
anlegen() {
	local e id name dns ip mem cores disk extra
	qm status "$VORLAGE_ID" >/dev/null 2>&1 ||
		fehler "there is no template $VORLAGE_ID - run 'vorlage' first"
	for e in "${MASCHINEN[@]}"; do
		id=$(feld "$e" 1)
		name=$(feld "$e" 2)
		dns=$(feld "$e" 3)
		ip=$(feld "$e" 4)
		mem=$(feld "$e" 5)
		cores=$(feld "$e" 6)
		disk=$(feld "$e" 7)
		extra=$(feld "$e" 8)
		if qm status "$id" >/dev/null 2>&1; then
			echo "proxmox-sechs: VMID $id is already there, leaving it alone"
			continue
		fi
		lauf qm clone "$VORLAGE_ID" "$id" --name "ngc-$name" --full 1 --storage "$STORAGE"
		lauf qm set "$id" --memory "$mem" --cores "$cores" --onboot 1 \
			--ipconfig0 "ip=$ip/$CIDR,gw=$GATEWAY"
		lauf qm disk resize "$id" scsi0 "${disk}G"
		if [ "$extra" != 0 ]; then
			# The second disk is what makes fscrypt work without touching the
			# MOUNTED root filesystem: it is made with "mkfs.ext4 -O encrypt"
			# (mkfs.ext4 does NOT set that feature by itself - measured with
			# e2fsprogs 1.47.2, the version Debian 13 ships) and mounted at
			# /var/lib/ngc/apps. docs/proxmox-sechs.md has the whole reason.
			lauf qm set "$id" --scsi1 "$STORAGE:${extra},iothread=1"
		fi
		lauf qm start "$id"
		printf 'angelegt: %s (VMID %s) %s -> %s\n' "$name" "$id" "$dns" "$ip"
	done
	echo "anlegen: ok - give them a minute, then: proxmox-sechs.sh namen --setzen"
}

# ------------------------------------------------------------------ namen
# The names have to resolve, and that is not a detail: the interface answers
# under its NAME, the certificate and the passkeys hang on it, a browser warns
# over an IP and passkeys do not work over one at all. Without --setzen this
# only prints the block - that is the half that belongs on Thomas's PC.
namen() {
	local e ip dns block
	block=$(hosts_block)
	printf '%s\n' "$block"
	[ "$zusatz" = --setzen ] || return 0
	for e in "${MASCHINEN[@]}"; do
		ip=$(feld "$e" 4)
		dns=$(feld "$e" 3)
		printf '%s\n' "$block" |
			ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=10 "$CIUSER@$ip" \
				"sed -i '/^# ngc-sechs/,\$d' /etc/hosts && printf '# ngc-sechs\n' >>/etc/hosts && cat >>/etc/hosts && hostnamectl set-hostname $dns" ||
			fehler "could not write the hosts block on $ip"
		printf 'namen: %s has the block and is called %s\n' "$ip" "$dns"
	done
	echo "namen: ok"
}

# ------------------------------------------------------------------ abbauen
abbauen() {
	local e id
	for e in "${MASCHINEN[@]}"; do
		id=$(feld "$e" 1)
		qm status "$id" >/dev/null 2>&1 || continue
		lauf qm stop "$id" --skiplock 1 || true
		lauf qm destroy "$id" --purge 1 --destroy-unreferenced-disks 1
	done
	if [ "$zusatz" = --vorlage ] && qm status "$VORLAGE_ID" >/dev/null 2>&1; then
		lauf qm destroy "$VORLAGE_ID" --purge 1 --destroy-unreferenced-disks 1
	fi
	echo "abbauen: ok"
}

case $aktion in
pruefen) pruefen ;;
vorlage) vorlage ;;
anlegen) anlegen ;;
namen) namen ;;
abbauen) abbauen ;;
*)
	sed -n '2,38p' "${BASH_SOURCE[0]}"
	exit 2
	;;
esac
