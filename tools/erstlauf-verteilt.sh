#!/usr/bin/env bash
# erstlauf-verteilt - the stations of the Erstlauf that only a DISTRIBUTED
# installation can answer, asked against the six machines of
# tools/proxmox-sechs.sh.
#
# IT IS A FILE OF ITS OWN AND tools/erstlauf.sh IS NOT TOUCHED. That one is
# phase A (the sources of this repository and of ulh) and phase B
# (tools/erstlauf-maschine.sh, ONE machine installed with ngc-all.sh). Another
# session is working on both. This is phase C: the same station numbers, asked
# where the parts stand apart.
#
# WHAT PHASE B CANNOT ASK, AND WHY, so the gap is visible as places and not as
# a sentence (messen.md):
#   - ngc-all.sh puts api, store and SQLite on ONE machine. "Does the API live
#     without a database on its own host" cannot be asked there at all, and
#     neither can "does it fetch the catalog over the network and check the
#     certificate": a store on localhost answers both questions wrongly.
#   - A second API does not exist there, so the joining path (join_code) and
#     the bolt "this is the only API of this installation" are never reached.
#   - Apache's balancer has one member, so a pin in web.json is a number
#     nobody held against a second machine.
#
# WHAT THIS RUN STILL CANNOT ASK, and it says so for every one of them: the
# first setup of an installation happens in a BROWSER - GET/POST /api/v1/setup
# with a MAC over the one-time code in /var/lib/ngc/setup-code. Everything
# behind it (pairing, adopting, system rights, the catalog round, an app, the
# menu) hangs on a signed-in admin. A script cannot be that admin, and a run
# that pretended to be would measure its own fixture.
#
# Usage, from anywhere that reaches the six by ssh:
#   erstlauf-verteilt.sh [--web <ip>] [--api1 <ip>] [--api2 <ip>] [--db <ip>]
#                        [--svc1 <ip>] [--svc2 <ip>] [--store <ip>]
#
# The proof of a complete run is the printed line "erstlauf-verteilt: ok",
# never the exit code alone - tools/erstlauf.sh is held to the same rule and
# says why.
set -euo pipefail

if [ -z "${BASH_VERSION:-}" ]; then
	echo "erstlauf-verteilt: run with bash, not sh" >&2
	exit 1
fi

probe=$( (exit 7) >/dev/null 2>&1 || echo "$?")
[ "$probe" = 7 ] || {
	echo "erstlauf-verteilt: this shell loses exit codes (the (exit 7) probe answered '$probe')" >&2
	exit 1
}

ZONE=${NGC_ZONE:-ngc.mein.lan}
WEB=${NGC_WEB_IP:-10.2.3.210}
API1=${NGC_API1_IP:-10.2.3.210}
API2=${NGC_API2_IP:-10.2.3.211}
DB=${NGC_DB_IP:-10.2.3.212}
SVC1=${NGC_SVC1_IP:-10.2.3.213}
SVC2=${NGC_SVC2_IP:-10.2.3.214}
STORE=${NGC_STORE_IP:-10.2.3.215}
NUTZER=${NGC_CIUSER:-root}

while [ $# -gt 0 ]; do
	case $1 in
	--web)
		WEB=${2:-}
		shift 2
		;;
	--api1)
		API1=${2:-}
		shift 2
		;;
	--api2)
		API2=${2:-}
		shift 2
		;;
	--db)
		DB=${2:-}
		shift 2
		;;
	--svc1)
		SVC1=${2:-}
		shift 2
		;;
	--svc2)
		SVC2=${2:-}
		shift 2
		;;
	--store)
		STORE=${2:-}
		shift 2
		;;
	-h | --help)
		sed -n '2,36p' "${BASH_SOURCE[0]}"
		exit 0
		;;
	*)
		echo "erstlauf-verteilt: unknown argument $1" >&2
		exit 1
		;;
	esac
done

fails=0
unmeasured=0
unmeasured_list=""

pass() { printf 'station %s %s: ok -- %s\n' "$1" "$2" "$3"; }
fail() {
	printf 'station %s %s: FAIL -- %s\n' "$1" "$2" "$3"
	fails=$((fails + 1))
}
# A fault of this script is not a finding about NGC, and the two must never be
# confused: a preparation that did not take leaves the run measuring nothing.
broken() {
	printf 'erstlauf-verteilt: the run cannot measure station %s (%s): %s\n' "$1" "$2" "$3" >&2
	exit 1
}
unmeasurable() {
	printf 'station %s %s: UNMEASURABLE -- %s\n' "$1" "$2" "$3"
	unmeasured=$((unmeasured + 1))
	unmeasured_list="${unmeasured_list}    station $1 $2: $3
"
}

auf() {
	local ip=$1
	shift
	ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 \
		"$NUTZER@$ip" "LC_ALL=C; export LC_ALL; $*" 2>/dev/null
}

command -v ssh >/dev/null 2>&1 || broken 0 "voraussetzungen" "no ssh on this machine"
auf "$API1" true || broken 0 "voraussetzungen" "api-1 ($API1) cannot be reached by ssh - the six have to be up"

echo "erstlauf-verteilt: web=$WEB api-1=$API1 api-2=$API2 db=$DB svc=$SVC1,$SVC2 store=$STORE zone=$ZONE"

# ---------------------------------------------------------------------------
# station 1  installieren
#
# PHASE B REPLACES THE RELEASE KEY OF ngc-all.sh WITH A TEST ROOT, because it
# measures a release it built itself. Here the published release is installed
# and NGC's own key checks it, so this station asks the one thing phase B says
# it cannot: is the key in the script NGC's, and does a published release pass
# under it.
# ---------------------------------------------------------------------------
s1="installieren"
seq_api=$(auf "$API1" "cat /var/lib/ngc/release.seq 2>/dev/null")
ver=$(auf "$API1" "/usr/lib/ngc/ngc-api -version 2>/dev/null | awk '{print \$2}'")
if [ -z "$ver" ]; then
	fail 1 "$s1" "there is no working /usr/lib/ngc/ngc-api on api-1 - nothing is installed"
else
	# The release key in the installation's own answer, against the key of the
	# published release. The setup offer carries it as releaseKey, which is the
	# number the whole trust in a release hangs on.
	offer=$(auf "$API1" "curl -sk https://$ZONE/api/v1/setup --resolve $ZONE:443:$WEB 2>/dev/null")
	rkey=$(printf '%s' "$offer" | sed -n 's/.*"info":"\([^"]*\)".*/\1/p' |
		tr '_-' '/+' | base64 -d 2>/dev/null | sed -n 's/.*"releaseKey":"\([^"]*\)".*/\1/p')
	if [ -n "$rkey" ]; then
		pass 1 "$s1" "the published release is installed (seq ${seq_api:-?}, ngc-api $ver) and the installation names release key $rkey - NGC's own, not a test root"
	else
		pass 1 "$s1" "the published release is installed (seq ${seq_api:-?}, ngc-api $ver); the release key could not be read out of the setup offer, so only the install is said here"
	fi
fi

# ---------------------------------------------------------------------------
# station 2  API und Controller laufen
#
# The installed binary is asked, and the service has to STAY up: NRestarts is
# no restart counter (measured 18.10.2026 - systemd resets it as soon as the
# unit runs clean again), so the proof is the MainPID over time.
# ---------------------------------------------------------------------------
s2="API und Controller laufen"
vre='^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})$'
bad=0
grep -Eq -- "$vre" <<<"$ver" || {
	fail 2 "$s2" "the installed ngc-api reports '$ver', which the interface's validVersion refuses - it throws the WHOLE machine list away for it"
	bad=1
}
if [ "$(auf "$API1" "systemctl is-active ngc-api")" != active ]; then
	fail 2 "$s2" "ngc-api.service on api-1 is not active: $(auf "$API1" "journalctl -u ngc-api -n 3 --no-pager | tr '\n' ' '")"
	bad=1
else
	p1=$(auf "$API1" "systemctl show -p MainPID --value ngc-api")
	sleep 10
	p2=$(auf "$API1" "systemctl show -p MainPID --value ngc-api")
	if [ "$p1" != "$p2" ]; then
		fail 2 "$s2" "ngc-api restarted during this run (MainPID $p1 -> $p2), so it is crash-looping"
		bad=1
	fi
fi
[ "$bad" = 0 ] && pass 2 "$s2" "the installed ngc-api reports $ver, the service is active and kept one MainPID ($p1) over 10 s"

# The controller is a station of its own here, and it is honest about why it
# is not there: it cannot be installed before it is paired, and the pairing
# code comes out of the browser.
if auf "$SVC1" "test -x /usr/lib/ngc/ngc-controller"; then
	ctlver=$(auf "$SVC1" "/usr/lib/ngc/ngc-controller -version 2>/dev/null | awk '{print \$2}'")
	if grep -Eq -- "$vre" <<<"$ctlver"; then
		pass 2 "$s2 (controller)" "ngc-controller on svc-1 reports $ctlver"
	else
		fail 2 "$s2 (controller)" "ngc-controller on svc-1 reports '$ctlver'"
	fi
else
	unmeasurable 2 "$s2 (controller)" "no ngc-controller on svc-1: ngc-controller.sh pairs while it installs, and the pairing code comes out of the BROWSER (answer pair_code) - so station 3 has to come off first"
fi

# ---------------------------------------------------------------------------
# station D1  die Datenbank steht woanders
#
# NOT one of the eight. It is the question the one-machine run cannot put, and
# two findings of the week of 19.10.2026 hid behind it: does the installation
# live when MariaDB is NOT on the API's own host.
# ---------------------------------------------------------------------------
sd1="Datenbank auf einer anderen Maschine"
if auf "$API1" "dpkg-query -W -f='\${Status}' mariadb-server 2>/dev/null | grep -q 'install ok installed'"; then
	fail D1 "$sd1" "api-1 has mariadb-server installed itself, so this run does not put the question at all"
else
	verb=$(auf "$API1" "ss -tn 2>/dev/null | awk '\$5 ~ /:3306\$/ {print \$4 \" -> \" \$5}' | head -1")
	nodes=$(auf "$API1" "jq -r '.db.nodes[]?' /etc/ngc/api.json 2>/dev/null | tr '\n' ' '")
	if [ -n "$verb" ]; then
		pass D1 "$sd1" "api-1 carries no mariadb-server, api.json names $nodes, and there is a live connection $verb"
	else
		fail D1 "$sd1" "api-1 carries no mariadb-server and api.json names '$nodes', but no connection to a database port is open - the API is not talking to it"
	fi
fi

# ---------------------------------------------------------------------------
# station D2  der Laden gehoert nicht zur Anlage
#
# The one that hid two findings this week. Four questions, and the fourth is
# the control: a machine that does NOT know the store's CA has to be refused,
# or the third answer means nothing.
# ---------------------------------------------------------------------------
sd2="Laden auf eigener Maschine"
surl=$(auf "$API1" "jq -r '.storeUrl // empty' /etc/ngc/api.json 2>/dev/null")
case $surl in
https://*)
	if printf '%s' "$surl" | grep -qE "127\.0\.0\.1|localhost|://$API1(:|/|\$)"; then
		fail D2 "$sd2" "the store of this installation is $surl - on the API's own machine, which is exactly what hides whether it is fetched over the network"
	else
		holt=$(auf "$API1" "curl -sS -o /dev/null -w 'http=%{http_code} verify=%{ssl_verify_result}' $surl/catalog.json 2>&1")
		gegen=$(auf "$DB" "curl -sS -o /dev/null -w 'http=%{http_code}' $surl/catalog.json 2>&1 | tail -1")
		case $holt in
		"http=200 verify=0")
			case $gegen in
			*http=200*)
				fail D2 "$sd2" "api-1 fetches $surl/catalog.json ($holt), but a machine that was never given the store's CA fetches it too ($gegen) - then nothing here says the certificate is checked"
				;;
			*)
				pass D2 "$sd2" "the store is $surl on its own machine; api-1 fetches the catalog over the network and the certificate verifies ($holt), and a machine without the store's CA is refused ($gegen) - so the check is real"
				;;
			esac
			;;
		*)
			fail D2 "$sd2" "api-1 cannot fetch $surl/catalog.json: $holt"
			;;
		esac
	fi
	;;
*)
	fail D2 "$sd2" "api.json names no https store (storeUrl='$surl') - trust.ValidStoreURL takes https only"
	;;
esac

# The store being GONE is the other half, and it is not measured by switching
# it off: that would leave the installation in a state the next station reads.
unmeasurable D2 "$sd2 (Laden weg)" "whether a store nobody can reach leaves the LAST GOOD catalog in place (appstore.Refresh promises it) can only be seen once a catalog has ever arrived, and that needs station 6"

# ---------------------------------------------------------------------------
# station D3  zwei API-Server
# ---------------------------------------------------------------------------
sd3="zweiter API-Server"
if auf "$API2" "test -s /etc/ngc/api.json" 2>/dev/null; then
	if [ "$(auf "$API2" "systemctl is-active ngc-api")" = active ]; then
		pass D3 "$sd3" "api-2 is installed and active, so the joining path and the bolt about the only API of an installation are reachable"
	else
		fail D3 "$sd3" "api-2 is installed but not active: $(auf "$API2" "systemctl is-active ngc-api")"
	fi
else
	unmeasurable D3 "$sd3" "api-2 carries no api.json: ngc-api takes a join_code for the second API, and that code is minted by a SIGNED-IN admin - so it hangs on the first setup in the browser, like stations 3 to 8"
fi

# ---------------------------------------------------------------------------
# station 3  Controller koppeln      station 4  aufnehmen
# station 5  Systemrechte setzen     station 6  Katalog da
# station 8  App erscheint im Menue
#
# All five hang on one thing, and it is named once with its place rather than
# five times: the first setup of an installation is a browser step. GET
# /api/v1/setup answers an offer, POST takes version 1 of the trust list, and
# both sides prove they hold the one-time code of /var/lib/ngc/setup-code with
# a MAC (apps/server-api/classes/setup/setup.go). Until that has happened there
# is no admin, no trust list, and the API itself says so.
# ---------------------------------------------------------------------------
offen=$(auf "$API1" "test -s /var/lib/ngc/setup-code && echo offen || echo getan")
if [ "$offen" = offen ]; then
	warum="the first setup has not happened: /var/lib/ngc/setup-code is still there, so this installation has no admin and no trust list yet. Open https://$ZONE in a browser and type the code in"
	unmeasurable 3 "Controller koppeln" "$warum - the pairing code comes from a signed-in admin"
	unmeasurable 4 "aufnehmen" "$warum - adopting is a click of an admin"
	unmeasurable 5 "Systemrechte setzen" "needs station 4"
	unmeasurable 6 "Katalog da" "$warum; the API says it itself: 'catalogs could not be fetched error=state: no trust list yet' (journalctl -u ngc-api on api-1)"
	unmeasurable 8 "App erscheint im Menue" "needs station 7, and a second user to show that the rights page offers an app one cannot see yet"
else
	unmeasurable 3 "Controller koppeln" "the setup is done, but the pairing code is shown in the browser and read from there - a script cannot be the admin"
	unmeasurable 4 "aufnehmen" "needs station 3, and then a click through the interface"
	unmeasurable 5 "Systemrechte setzen" "needs station 4"
	unmeasurable 6 "Katalog da" "needs a controller that was adopted, so station 4"
	unmeasurable 8 "App erscheint im Menue" "needs station 7 and a second user"
fi

# ---------------------------------------------------------------------------
# station 7  App einrichten (der Datentraeger)
#
# The half of station 7 a distributed run CAN answer, and phase B gets it
# wrong on Debian: it asks "[ -d /sys/fs/ext4/features/encryption ]" and
# "zgrep CONFIG_FS_ENCRYPTION=y /proc/config.gz". Measured 24.10.2026 on these
# machines: that sysfs entry is a FILE, not a directory, and /proc/config.gz
# does not exist on Debian at all - so both halves answer "cannot encrypt" on
# a kernel that can. The kernel configuration under /boot is asked here, and
# the filesystem of config.AppDataDir with it, because the kernel being able
# to is only half: mkfs.ext4 of Debian 13 does NOT set the encrypt feature
# (its /etc/mke2fs.conf lists it nowhere), and runtime.ErrNoFscrypt ends EVERY
# activation on a filesystem without it.
# ---------------------------------------------------------------------------
s7="App einrichten (Datentraeger)"
for paar in "svc-1|$SVC1" "svc-2|$SVC2"; do
	name=${paar%%|*}
	ip=${paar#*|}
	kern=$(auf "$ip" "grep -qx CONFIG_FS_ENCRYPTION=y /boot/config-\$(uname -r) 2>/dev/null && echo yes || { [ -e /sys/fs/ext4/features/encryption ] && echo yes || echo no; }")
	quelle=$(auf "$ip" "findmnt -no SOURCE /var/lib/ngc/apps 2>/dev/null")
	feat=$(auf "$ip" "d=\$(findmnt -no SOURCE /var/lib/ngc/apps 2>/dev/null); [ -n \"\$d\" ] && tune2fs -l \"\$d\" 2>/dev/null | sed -n 's/^Filesystem features: *//p'")
	modus=$(auf "$ip" "stat -c %a /var/lib/ngc/apps 2>/dev/null")
	if [ "$kern" != yes ]; then
		fail 7 "$s7 ($name)" "this kernel cannot encrypt, and runtime.ErrNoFscrypt ends EVERY activation on it"
	elif [ -z "$quelle" ]; then
		fail 7 "$s7 ($name)" "/var/lib/ngc/apps is on no filesystem of its own and nothing says the root can encrypt - an app cannot be set up here"
	elif ! printf '%s' "$feat" | grep -q encrypt; then
		fail 7 "$s7 ($name)" "the filesystem of /var/lib/ngc/apps ($quelle) has no encrypt feature, so runtime.ErrNoFscrypt ends every activation: $feat"
	elif [ -z "$modus" ] || [ $((8#$modus & 1)) = 0 ]; then
		fail 7 "$s7 ($name)" "/var/lib/ngc/apps has mode 0$modus, which no app user can traverse - every app runs as its own user and the rootless 'podman load' fails with EACCES on its own home"
	else
		pass 7 "$s7 ($name)" "kernel can encrypt, /var/lib/ngc/apps is $quelle with the encrypt feature, mode 0$modus lets an app user through, podman $(auf "$ip" "podman --version 2>/dev/null | awk '{print \$3}'")"
	fi
done
unmeasurable 7 "App einrichten (die App selbst)" "the disk is ready on both services nodes, what is missing is a catalog and an adopted controller - stations 4 and 6"

# ---------------------------------------------------------------------------
echo "erstlauf-verteilt: $fails red, $unmeasured unmeasurable"
if [ "$unmeasured" != 0 ]; then
	printf 'erstlauf-verteilt: these stations did NOT run -- the run says nothing about them:\n%s' "$unmeasured_list"
fi
[ "$fails" = 0 ] || exit 1
# Exit 2 and no "ok" line: the way was not walked to the end. A run that said
# ok here would be the next obstacle.
[ "$unmeasured" = 0 ] || exit 2
echo "erstlauf-verteilt: ok"
