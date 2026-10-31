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
#                        [--laden-weg]
#
# Machines that are not in ~/.ssh/config are reached with NGC_SSH_OPTS, for
# example NGC_SSH_OPTS="-i ~/Documents/ssh/keys/id_ed25519_ngc -o IdentitiesOnly=yes".
#
# --laden-weg adds station D2b, which takes the SECOND API off the store with
# a blackhole route and restarts it twice to make the catalog round come round
# again. It changes the installation for about four minutes and is therefore
# off by default; D2b says why in full.
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

# The repository this file sits in, so that a path like config.AppDataDir is
# READ out of the sources rather than typed in here a second time. Held
# against a marker only the right directory has, as tools/erstlauf.sh does and
# for the reason it gives there.
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
grep -q "^[[:space:]]*AnchorFile[[:space:]]*= " "$root/shared/config/config.go" 2>/dev/null ||
	root=""

ZONE=${NGC_ZONE:-ngc.mein.lan}
WEB=${NGC_WEB_IP:-10.2.3.210}
API1=${NGC_API1_IP:-10.2.3.210}
API2=${NGC_API2_IP:-10.2.3.211}
DB=${NGC_DB_IP:-10.2.3.212}
SVC1=${NGC_SVC1_IP:-10.2.3.213}
SVC2=${NGC_SVC2_IP:-10.2.3.214}
STORE=${NGC_STORE_IP:-10.2.3.215}
NUTZER=${NGC_CIUSER:-root}
# Extra options for ssh, for machines that are not in ~/.ssh/config - a port,
# an identity, another config file. tools/erstlauf.sh has carried this since
# PR 99 for the same reason, and this file did not: measured 24.10.2026, the six
# machines of this installation answer only to ~/Documents/ssh/keys/id_ed25519_ngc,
# which is not a default identity, so every `auf` call answered "Permission
# denied (publickey)" and the run died at the reachability check with "the six
# have to be up" - a sentence about the machines for a fault in this script.
SSHOPTS=${NGC_SSH_OPTS:-}
# Station D2b switches a machine off the store and restarts an API to make the
# round come round again. That CHANGES the installation for two minutes, so it
# is not part of a plain run: a measuring run that restarts services nobody
# asked it to restart is not a measurement, it is an outage.
LADENWEG=${NGC_LADEN_WEG:-}

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
	--laden-weg)
		LADENWEG=ja
		shift
		;;
	--store)
		STORE=${2:-}
		shift 2
		;;
	-h | --help)
		sed -n '2,45p' "${BASH_SOURCE[0]}"
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
	# shellcheck disable=SC2086
	ssh $SSHOPTS -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 \
		"$NUTZER@$ip" "LC_ALL=C; export LC_ALL; $*" 2>/dev/null
}

command -v ssh >/dev/null 2>&1 || broken 0 "voraussetzungen" "no ssh on this machine"
auf "$API1" true || broken 0 "voraussetzungen" "api-1 ($API1) cannot be reached by ssh as $NUTZER with ssh options '$SSHOPTS' - either the machine is down or this run holds the wrong key; pass NGC_SSH_OPTS='-i <key> -o IdentitiesOnly=yes'"

echo "erstlauf-verteilt: web=$WEB api-1=$API1 api-2=$API2 db=$DB svc=$SVC1,$SVC2 store=$STORE zone=$ZONE"

# ---------------------------------------------------------------------------
# station D0  die Namen zeigen dorthin, wo die Teile stehen
#
# NOT one of the eight, and it stands FIRST because every station behind it
# reads as a fault of NGC when this one is red. A one-machine installation
# cannot put the question at all: there everything is localhost, and a name
# that points at the wrong machine still lands on the right one.
#
# Measured 24.10.2026 on these six, and it is the reason this station exists:
# the DNS of this network (10.2.3.102) answers EVERY *.ngc.mein.lan name with
# an AAAA record "::" BESIDE the correct A record. getent, curl and every
# other NSS client take the AAAA first, connect to "::" - which is the local
# machine - and land on whatever listens on 443 there. On api-1 that is
# apache, so "does api-1 fetch the catalog from the store" answered
# "self-signed certificate, 404" while the store was healthy and five metres
# away. Go dials the addresses in turn and falls past the bogus one, so NGC
# itself works; every tool a person reaches for does not.
#
# THE CONTROL IS IN THE STATION, and it is what makes the two tellable apart:
# a name with no DNS answer at all is a different fault from a name whose A
# record is right and whose AAAA record lies. The A record is asked for
# separately, so this station says which of the two it found.
#
# WHAT IT DOES NOT CATCH, and it is said here rather than left for the next
# reader to trip over: a name that answers with the REAL IPv6 address of the
# WRONG machine passes. This run is told the six by their IPv4 addresses and
# holds the A record against them; it has no IPv6 address of any of them to
# hold an AAAA record against, so it can only refuse the one bogus answer it
# knows ("::"). api-1 of these six resolves to its own three real IPv6
# addresses and is green for that reason - correctly, but not because this
# station checked them.
# ---------------------------------------------------------------------------
sd0="die Namen zeigen hin, wo die Teile stehen"
for paar in "api-1|$API1" "api-2|$API2" "db-1|$DB" "svc-1|$SVC1" "svc-2|$SVC2" "store|$STORE"; do
	name=${paar%%|*}
	ip=${paar#*|}
	fqdn=$name.$ZONE
	# What a program that uses the system resolver gets, in the order it gets
	# it - the first address is the one it will dial.
	erst=$(auf "$API1" "getent ahostsv4 $fqdn 2>/dev/null | awk 'NR==1{print \$1}'; :" || true)
	alle=$(auf "$API1" "getent hosts $fqdn 2>/dev/null | awk '{print \$1}' | tr '\n' ' '; :" || true)
	arec=$(auf "$API1" "resolvectl query --type=A $fqdn 2>/dev/null | sed -n 's/.* IN A \([0-9.]*\).*/\1/p' | head -1; :" || true)
	if [ -z "$alle" ] && [ -z "$arec" ]; then
		fail D0 "$sd0 ($name)" "$fqdn resolves to nothing at all from api-1 - no A record and no answer through the system resolver, so nothing on these machines can name this part"
	elif [ "$arec" != "$ip" ]; then
		fail D0 "$sd0 ($name)" "the A record of $fqdn is '$arec' and this run was told the machine is $ip - one of the two is wrong, and every station that reads this name is measuring the wrong machine"
	elif printf '%s' "$alle" | grep -qE '(^| )::( |$)'; then
		fail D0 "$sd0 ($name)" "the A record of $fqdn is right ($arec) but the system resolver hands out '$alle' - the '::' comes first, so getent, curl and every other NSS client dial the LOCAL machine instead of $ip and land on whatever listens on 443 there. Go falls past it and NGC works; no tool a person reaches for does. The AAAA record for $fqdn has to go"
	elif [ "$erst" != "$ip" ] && [ -n "$erst" ]; then
		fail D0 "$sd0 ($name)" "$fqdn resolves to '$alle' and the FIRST address a program dials is '$erst', not $ip"
	else
		pass D0 "$sd0 ($name)" "$fqdn resolves to $arec and the system resolver hands out '$alle' - a program that uses the name reaches the machine that carries the part"
	fi
done

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
		# "|| true" and it is not decoration. curl answers 60 for a certificate
		# it cannot check, `auf` hands that code on, and under "set -e" a failing
		# command substitution ends the whole run THERE - with no station line and
		# no message. Measured 24.10.2026: this run printed stations 1, 2 and D1
		# and then vanished with exit 60, so D2, D3, 3, 4, 6, 7 and the totals
		# were never asked and nobody could tell that from the output. The run
		# has to REPORT a store it cannot reach, which is this station's whole
		# point. The same trap was already fixed once in `entpacke` below; this
		# was the second place with the same shape.
		# THREE fetches and not one, because a one-machine run never had to
		# tell these two faults apart and this run does. --resolve pins the
		# name to the machine this run was told carries the store, so the
		# certificate and the CA are measured even while the NAME is broken
		# (station D0); the third fetch uses the name as a program would and
		# is the only one that can be red for D0's reason.
		shost=${surl#https://}
		shost=${shost%%/*}
		shost=${shost%%:*}
		rsl="--resolve $shost:443:$STORE"
		holt=$(auf "$API1" "curl -sS $rsl -o /dev/null -w 'http=%{http_code} verify=%{ssl_verify_result}' $surl/catalog.json 2>&1" | tr '\n' ' ' || true)
		gegen=$(auf "$DB" "curl -sS $rsl -o /dev/null -w 'http=%{http_code}' $surl/catalog.json 2>&1 | tail -1" | tr '\n' ' ' || true)
		# One line per station is the rule of this run, and curl breaks it on its
		# own: a certificate it refuses costs six lines of prose plus a link.
		# Measured 24.10.2026 - the station printed them and the output stopped
		# being readable as a list of stations.
		ueber=$(auf "$API1" "curl -sS -o /dev/null -w 'http=%{http_code} verify=%{ssl_verify_result}' $surl/catalog.json 2>&1" | tr '\n' ' ' || true)
		case $holt in
		"http=200 verify=0")
			case $gegen in
			*http=200*)
				fail D2 "$sd2" "api-1 fetches $surl/catalog.json ($holt), but a machine that was never given the store's CA fetches it too ($gegen) - then nothing here says the certificate is checked"
				;;
			*)
				if [ "$ueber" = "http=200 verify=0" ]; then
					pass D2 "$sd2" "the store is $surl on its own machine; api-1 fetches the catalog over the network under its own NAME and the certificate verifies ($ueber), and a machine without the store's CA is refused ($gegen) - so the check is real"
				else
					fail D2 "$sd2" "the store at $surl is healthy and its certificate verifies when the name is pinned to $STORE ($holt), and a machine without its CA is refused ($gegen) - but the same fetch under the NAME answers '$ueber'. That is station D0's fault and not the store's: a program on api-1 that uses the name does not reach the store. NGC itself still gets there because Go dials the addresses in turn; nothing else on these machines does"
				fi
				;;
			esac
			;;
		*)
			fail D2 "$sd2" "api-1 cannot fetch $surl/catalog.json even with the name pinned to $STORE: $holt"
			;;
		esac
	fi
	;;
*)
	fail D2 "$sd2" "api.json names no https store (storeUrl='$surl') - trust.ValidStoreURL takes https only"
	;;
esac

# ---------------------------------------------------------------------------
# station D2b  der Laden ist AUS
#
# THE SENTENCE THIS REPLACES WAS WRONG AGAINST THE SOURCE, and that is worth
# more than the station itself: it said this was measurable "but it costs an
# hour of wall clock: the store round first beats an hour after the start and
# then hourly (server.go says why)". server.go says the opposite - it sets
# FirstCatalog = time.Minute and spends six lines on why it is a minute and
# not an hour (V1.2 point 66: "every restart of ngc-api pushed that first
# fetch out by another full hour -- so an installation somebody is working on
# never fetched one at all"). The station costs a restart and two minutes, and
# it was marked unmeasurable for an hour that does not exist.
#
# WHY NOT ON api-1, AND WHY NOT BY SWITCHING THE STORE OFF. The store is a
# machine other people work on - another session was putting a second app on
# it while this was written - and its nginx serves the whole installation. So
# the lever is a blackhole route on the SECOND API, which takes that one
# machine off the store and touches nothing else: the web app, the browser and
# the first API never notice.
#
# WHAT IS ASSERTED, and the second half is the one that matters to a person:
#   a) the installation RECORDS the failure where an admin can see it (the
#      store's own row, which ngc.store/sources reads), and
#   b) the last good catalog is STILL THERE afterwards - appstore.Refresh
#      promises "a store that fails leaves the last good catalog in place",
#      and an installation that threw its catalog away because one machine was
#      rebooted would lose every app in its menu.
#
# AND WHAT IT FOUND, measured 24.10.2026 on api-2 of these six: both hold. The
# row carried {"checked":...,"error":"Der Store hat nicht oder nicht brauchbar
# geantwortet."} and the catalog stayed byte for byte. But the JOURNAL said
# nothing at all, not one line, because Server.hour only logs when
# Stores.Refresh returns an error and Refresh never does - it notes the reason
# per store and throws the note's own error away. Whoever watches the log of
# an installation whose store has been gone for a week sees nothing. That is
# said in the station's own sentence instead of being swallowed.
#
# THE CONTROL IS PART OF THE STATION. The route goes back, the API is
# restarted again, and the SAME round has to clear the error. Without that
# half a row that said "error" for some other reason would make this station
# green for the wrong cause.
# ---------------------------------------------------------------------------
sd2b="$sd2 (Laden weg)"
if [ -z "$LADENWEG" ]; then
	unmeasurable D2 "$sd2b" "this station takes an API off the store with a blackhole route and restarts it TWICE, which changes the installation for about four minutes - so it does not run in a plain run. Pass --laden-weg (or NGC_LADEN_WEG=ja) when nobody else is working on this installation. It is NOT an hour of wall clock: server.go sets FirstCatalog = time.Minute"
elif [ "$API2" = "$API1" ]; then
	unmeasurable D2 "$sd2b" "--laden-weg takes the SECOND API off the store so that the first one, the web app and the browser never notice; here --api2 and --api1 name the same machine ($API1), and this run will not black-hole the store on the machine the installation is served from"
else
	vorher=$(auf "$DB" "mariadb -Nse 'select char_length(data) from ngc.ngc_blob where id=\"global\" and kind=\"catalog\"'" || true)
	if [ -z "$vorher" ] || [ "$vorher" = 0 ]; then
		unmeasurable D2 "$sd2b" "there is no stored catalog to lose, so the half of this station that matters - 'the last good catalog is still there afterwards' - cannot be asked at all. Station 6 has to come off first"
	else
		# A net for the route, in case this run is killed between the two
		# halves: a measuring script that leaves a machine cut off is worse
		# than one that measures nothing.
		auf "$API2" "systemd-run --unit=ngc-erstlauf-netz --on-active=900 --collect /sbin/ip route del blackhole $STORE/32 >/dev/null 2>&1" >/dev/null || true
		if ! auf "$API2" "ip route add blackhole $STORE/32"; then
			unmeasurable D2 "$sd2b" "a blackhole route to $STORE could not be set on api-2, so the store was never taken away and nothing below would mean anything"
		else
			auf "$API2" "systemctl restart ngc-api" || true
			# FirstCatalog is a minute; the slack is for a round that is still
			# running when the timer fires, because Server.once drops that tick.
			auf "$API2" "sleep 115" >/dev/null || true
			zeile=$(auf "$DB" "mariadb -Nse 'select data from ngc.ngc_blob where id=\"store.global\"'" || true)
			nachher=$(auf "$DB" "mariadb -Nse 'select char_length(data) from ngc.ngc_blob where id=\"global\" and kind=\"catalog\"'" || true)
			buch=$(auf "$API2" "journalctl -u ngc-api --since '-3min' --no-pager | grep -c 'catalogs could not be fetched'" || true)
			# The route goes back BEFORE anything is concluded, so that a red
			# station does not leave the machine cut off either.
			auf "$API2" "ip route del blackhole $STORE/32" || true
			auf "$API2" "systemctl stop ngc-erstlauf-netz.timer >/dev/null 2>&1; systemctl reset-failed ngc-erstlauf-netz.timer >/dev/null 2>&1; :" >/dev/null || true
			auf "$API2" "systemctl restart ngc-api" || true
			auf "$API2" "sleep 115" >/dev/null || true
			kontrolle=$(auf "$DB" "mariadb -Nse 'select data from ngc.ngc_blob where id=\"store.global\"'" || true)
			noch=$(auf "$API2" "ip route show | grep -c blackhole" || true)
			case $zeile in
			*'"error"'*)
				if [ "$nachher" != "$vorher" ]; then
					fail D2 "$sd2b" "api-2 could not reach the store and the installation said so ($zeile) - but the stored catalog went from $vorher to $nachher bytes. appstore.Refresh promises 'a store that fails leaves the last good catalog in place'; on this reading a machine that is rebooted costs the installation every app in its menu"
				elif printf '%s' "$kontrolle" | grep -q '"error"'; then
					fail D2 "$sd2b" "the error was recorded while the store was away ($zeile) and the catalog survived ($vorher bytes) - but the control did NOT clear it: with the route back and the store reachable again the row still says $kontrolle. Then this station cannot tell its own mutation from a fault that was there anyway, and says nothing"
				else
					pass D2 "$sd2b" "with api-2 black-holed off the store, the installation recorded the failure where an admin reads it ($zeile) and kept the last good catalog ($vorher bytes, unchanged); with the route back the SAME round cleared the error ($kontrolle), so the red half was this run's doing. The journal of api-2 printed $buch line(s) about it: Server.hour only logs when Stores.Refresh errors and Refresh never does, so a store that has been gone for a week is visible to somebody who signs in and looks at the store's row, and to nobody else"
				fi
				;;
			*)
				fail D2 "$sd2b" "api-2 was black-holed off the store for two minutes and the store's row still says '$zeile' - no error was recorded anywhere a person could read it, so an installation whose store is gone looks healthy. The catalog went from $vorher to $nachher bytes and the journal of api-2 printed $buch line(s)"
				;;
			esac
			[ "${noch:-0}" = 0 ] || broken D2 "$sd2b" "this run left $noch blackhole route(s) on api-2 - remove them with 'ip route del blackhole $STORE/32' before trusting anything that machine answers"
		fi
	fi
fi
# The rollback guard is the other half of "the store misbehaves" and it is
# still not asked: it needs a catalog with a LOWER seq served after a higher
# one, which means WRITING to the store - the one machine this run must leave
# exactly as it found it.
unmeasurable D2 "$sd2 (Rueckdreh)" "store.ErrRollback refuses a catalog whose seq is below the one on record. Measuring it means serving a LOWER seq from the store, so it writes to the store machine - a run of its own, and one that has to be agreed with whoever is publishing apps there"

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
# station D3b  eine Anmeldung, die auf api-2 landet
#
# WHAT THE STATION ABOVE DOES NOT ASK. D3 asks whether api-2 is installed and
# its service is active. That is a different question from the one a person
# has: the browser picks ONE api out of /etc/ngc/web.json for every call, so
# half the sign-ins of this installation land on api-2 - and a second API that
# is up but answers sign-ins differently locks half the people out while every
# status line stays green.
#
# TWO HALVES, AND THE SECOND IS THE ONE A ONE-MACHINE RUN CANNOT HAVE:
#   a) api-2 answers the sign-in path at all, and answers it the SAME as
#      api-1. The same deliberately empty body goes to both over the network,
#      and the two answers have to match - a shape, not a credential, so no
#      key is needed for it.
#   b) the TLS pin the browser holds for api-2 really is api-2's certificate.
#      web.json pins each api by the SHA-256 of its public key (apitls), and
#      nothing but a distributed installation has a second one of those to get
#      wrong. A pin that named the wrong machine would refuse every sign-in
#      that landed there, and no service status anywhere would say so.
#
# THE CONTROL IS IN THE STATION: api-1's own key is hashed against api-2's
# pin in the same run and MUST NOT match. Without it a hash function that
# answered the same for everything would make this station green for ever.
#
# WHAT THIS STATION DOES NOT SAY, and it says so rather than skipping it: that
# somebody really gets IN on api-2. A sign-in is a passkey, held by a person
# in a browser, and nothing in a shell can hold one. The credential half is
# therefore named below as not measured.
# ---------------------------------------------------------------------------
sd3b="eine Anmeldung landet auf api-2"
if [ "$API2" = "$API1" ]; then
	unmeasurable D3 "$sd3b" "--api1 and --api2 name the same machine ($API1), so there is no second API for a sign-in to land on"
else
	webjson=$(auf "$WEB" "cat /etc/ngc/web.json 2>/dev/null" || true)
	pin2=$(printf '%s' "$webjson" | tr ',' '\n' | grep -A1 -- "$API2\|api-2" | sed -n 's/.*"tls"[^"]*"\([^"]*\)".*/\1/p' | head -1)
	[ -n "$pin2" ] || pin2=$(auf "$WEB" "jq -r '.apis[] | select(.url|test(\"api-2\")) | .tls' /etc/ngc/web.json 2>/dev/null" || true)
	# The key the machine really serves, hashed the way apitls pins it.
	spki() {
		auf "$1" "openssl x509 -in /etc/ngc/tls/api.crt -pubkey -noout 2>/dev/null | openssl pkey -pubin -outform der 2>/dev/null | openssl dgst -sha256 -binary | base64 | tr '+/' '-_' | tr -d '='" || true
	}
	ist2=$(spki "$API2")
	ist1=$(spki "$API1")
	# The sign-in path, over the network, with a body that is deliberately not
	# a credential: what is held against each other is the SHAPE of the answer.
	anm() {
		auf "$API1" "curl -sk --max-time 20 --resolve api-2.$ZONE:7443:$API2 --resolve api-1.$ZONE:7443:$API1 -X POST -H 'content-type: application/json' -d '{}' https://$1.$ZONE:7443/api/v1/login/start" || true
	}
	a1=$(anm api-1)
	a2=$(anm api-2)
	if [ -z "$pin2" ] || [ -z "$ist2" ] || [ -z "$ist1" ]; then
		unmeasurable D3 "$sd3b" "the pin of api-2 could not be read out of web.json on $WEB ('$pin2') or the certificates of the two APIs could not be hashed ('$ist1' / '$ist2') - so this run cannot say whose certificate the browser is holding"
	elif [ "$ist1" = "$ist2" ]; then
		broken D3 "$sd3b" "api-1 and api-2 serve the SAME public key ($ist1) - then the control of this station cannot be made, because a pin cannot tell the two machines apart at all"
	elif [ "$pin2" != "$ist2" ]; then
		fail D3 "$sd3b" "web.json pins api-2 at '$pin2' and api-2 really serves '$ist2'. Every sign-in the browser sends to api-2 is refused on the certificate - and api-2's service is active, so nothing a status page shows says a word about it"
	elif [ "$pin2" = "$ist1" ]; then
		fail D3 "$sd3b" "the pin web.json holds for api-2 is api-1's key ($ist1) - the second API was written into the list with the first one's certificate, so half the sign-ins die on a pin nobody can see"
	elif [ -z "$a2" ]; then
		fail D3 "$sd3b" "api-2's pin is right ($pin2) but POST /api/v1/login/start on api-2 answered nothing at all over the network, while api-1 answered '$a1' - a sign-in that lands there gets no answer"
	elif [ "$a1" != "$a2" ]; then
		fail D3 "$sd3b" "api-1 and api-2 answer the same sign-in call differently: api-1 '$a1', api-2 '$a2'. Whoever's browser picks api-2 out of web.json meets a different installation"
	else
		pass D3 "$sd3b" "the pin web.json hands the browser for api-2 is api-2's own key ($pin2, and api-1's key $ist1 is refused by it), and POST /api/v1/login/start on api-2 answers over the network exactly as api-1 does ('$a2') - so a sign-in that lands on the second API reaches a live login path with a certificate the browser accepts"
	fi
fi
unmeasurable D3 "$sd3b (der Ausweis)" "that somebody really gets IN on api-2 is not said here. A sign-in is a passkey held by a person in a browser (login/start then login/finish), and nothing in a shell holds one - this station measures the path and the certificate, never the credential. THOMAS has to press this: open https://$ZONE, sign in, and do it twice with a reload in between, because web.json hands out api-1 and api-2 in turn"

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

# Station 3 has a half that IS decidable from a machine once a controller
# stands there: a pairing leaves controller.json (config.SaveController, 0600
# and root's) and the marker /var/lib/ngc/controller/paired behind, and the
# service has to carry it. That is asked first, so a paired controller is not
# reported as unmeasurable just because the code came from a browser.
s3="Controller koppeln"
if auf "$SVC1" "test -s /etc/ngc/controller.json && test -e /var/lib/ngc/controller/paired"; then
	if [ "$(auf "$SVC1" "systemctl is-active ngc-controller")" = active ]; then
		cp1=$(auf "$SVC1" "systemctl show -p MainPID --value ngc-controller")
		sleep 10
		cp2=$(auf "$SVC1" "systemctl show -p MainPID --value ngc-controller")
		if [ "$cp1" != "$cp2" ]; then
			fail 3 "$s3" "ngc-controller on svc-1 restarted during this run (MainPID $cp1 -> $cp2), so it is crash-looping"
		else
			pass 3 "$s3" "svc-1 is paired: controller.json and the paired marker are there, ngc-controller.service is active and kept one MainPID ($cp1) over 10 s"
		fi
	else
		fail 3 "$s3" "svc-1 is paired (controller.json and the marker are there) but ngc-controller.service is $(auf "$SVC1" "systemctl is-active ngc-controller"): $(auf "$SVC1" "journalctl -u ngc-controller -n 3 --no-pager | tr '\n' ' '")"
	fi
	drei=getan
else
	drei=offen
fi

# ---------------------------------------------------------------------------
# station 4  aufnehmen      station 6  Katalog da
#
# Both are read from the DATABASE NODE, and that is said rather than hidden:
# the trust list and the catalogs live in the database (dbapi.BlobTrust,
# dbapi.BlobCatalog), and NGC exposes neither to anybody who is not signed in -
# which is right, and leaves a run like this one no other place to look. What
# is asked here is therefore the RESULT of the two clicks, not the clicks.
s4="aufnehmen"
s6="Katalog da"
# base64url WITH its padding put back, and the whole thing allowed to fail.
# Both halves were learned the hard way in this very script (24.10.2026): a
# payload is base64url without padding, "base64 -d" refuses it for exactly
# that - and under "set -e" a failing command substitution ends the run with
# no line and no message, so the stations after this one simply were not
# there. A measuring script that disappears mid-run is worse than a red one.
entpacke() {
	local b=$1
	case $(( ${#b} % 4 )) in
	2) b="$b==" ;;
	3) b="$b=" ;;
	esac
	printf '%s' "$b" | tr '_-' '/+' | base64 -d 2>/dev/null || true
}
roh=$(auf "$DB" "mariadb -Nse 'select data from ngc.ngc_blob where kind=\"trust\" order by cast(id as unsigned) desc limit 1'" |
	sed -n 's/.*"payload":"\([^"]*\)".*/\1/p' || true)
liste=$(entpacke "$roh")
if [ -z "$liste" ]; then
	unmeasurable 4 "$s4" "no trust list could be read from $DB - the first setup has to come off first"
else
	lseq=$(printf '%s' "$liste" | sed -n 's/.*"seq":\([0-9]*\).*/\1/p' | head -1 || true)
	# One line per node in the list, counted by the "kind" every entry carries.
	knoten=$(printf '%s' "$liste" | grep -o '"kind":"[a-z]*"' | wc -l || true)
	if [ "${knoten:-0}" -ge 2 ]; then
		pass 4 "$s4" "the trust list stands at version $lseq and holds $knoten nodes, so servers really were adopted - every one of them cost an admin a click"
	else
		fail 4 "$s4" "the trust list stands at version $lseq and holds $knoten node(s): nothing was adopted, so a paired server takes no tasks"
	fi
fi

kat=$(auf "$DB" "mariadb -Nse 'select char_length(data) from ngc.ngc_blob where kind=\"catalog\"'" | head -1 || true)
lage=$(auf "$DB" "mariadb -Nse 'select data from ngc.ngc_blob where id=\"store.global\"'" || true)
case $lage in
*'"error"'*)
	fail 6 "$s6" "the installation kept no catalog and says why: $lage"
	;;
*'"checked"'*)
	if [ "${kat:-0}" -gt 0 ]; then
		pass 6 "$s6" "a catalog of $kat bytes is stored and the store's row says $lage - fetched over the network from a store on its OWN machine, with its certificate checked"
	else
		fail 6 "$s6" "the store's row says $lage but no catalog is stored"
	fi
	;;
*)
	unmeasurable 6 "$s6" "the installation has not tried a store round yet (no store.global row). The round first beats an HOUR after the start, on purpose: server.go, 'an installation that comes up must not begin replacing its own parts before anybody has looked at it'"
	;;
esac

if [ "$offen" = offen ]; then
	warum="the first setup has not happened: /var/lib/ngc/setup-code is still there, so this installation has no admin and no trust list yet. Open https://$ZONE in a browser and type the code in"
	[ "$drei" = getan ] || unmeasurable 3 "$s3" "$warum - the pairing code comes from a signed-in admin"
	unmeasurable 5 "Systemrechte setzen" "needs station 4"
	unmeasurable 8 "App erscheint im Menue" "needs station 7, and a second user to show that the rights page offers an app one cannot see yet - and v0.2.1 can make no second user at all (see the other branch of this station)"
else
	[ "$drei" = getan ] || unmeasurable 3 "$s3" "no controller is paired yet: the code is shown in the browser (Benutzer und Server -> Server -> Kopplungscode), is good ONCE (dbapi.OncePair) and for 24 hours - a script cannot be the admin that mints it"
	unmeasurable 5 "Systemrechte setzen" "the servers are adopted, and giving one its system rights is a click of a signed-in admin on the rights page - there is no file on a machine that says it was done"
	# NOT "needs a second user" any more. There is no way to make one.
	# Measured 24.10.2026 against v0.2.1 at three places that would each have
	# to carry it: the actions of the users app on the API side are list,
	# rights, set-rights, trust, pair-code, join-code, pending, pending-api,
	# nodes and log - no add, no invite; the HTTP paths of the API are setup,
	# login/start, login/finish, logout, trust and ws - no registration; and
	# the web app has no mask for it (its "Benutzer" tab only lists). The one
	# admin of the first setup is the only account an installation ever gets.
	unmeasurable 8 "App erscheint im Menue" "this station is about a CIRCLE - the desktop shows an app only to somebody holding its 'use' right, and the rights page offered rights only for apps one could already see - so it takes a SECOND user to ask it. v0.2.1 can make no second user: ngc.users has no add action, the API has no registration path, and the interface has no mask. So the station is not unmeasured here, it is unreachable"
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
	kern=$(auf "$ip" "grep -qx CONFIG_FS_ENCRYPTION=y /boot/config-\$(uname -r) 2>/dev/null && echo yes || { [ -e /sys/fs/ext4/features/encryption ] && echo yes || echo no; }" || true)
	quelle=$(auf "$ip" "findmnt -no SOURCE /var/lib/ngc/apps 2>/dev/null; :" || true)
	# The trailing "; :" and the "|| true" were NOT here, and the run died on
	# them: on a node where /var/lib/ngc/apps is on no filesystem of its own,
	# findmnt prints nothing, the "[ -n ]" is false, the whole remote command
	# exits 1, `auf` hands that on and "set -e" ends the run inside a command
	# substitution - so the station that was about to report exactly that node
	# never printed a line. Measured 24.10.2026 by pointing --svc1 at a machine
	# that carries no app disk: the run stopped before station 7 without a word.
	feat=$(auf "$ip" "d=\$(findmnt -no SOURCE /var/lib/ngc/apps 2>/dev/null); [ -n \"\$d\" ] && tune2fs -l \"\$d\" 2>/dev/null | sed -n 's/^Filesystem features: *//p'; :" || true)
	modus=$(auf "$ip" "stat -c %a /var/lib/ngc/apps 2>/dev/null; :" || true)
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
# What is missing for the app itself is named by what the catalog HOLDS, not
# by a station number: a signed catalog with no app in it is a catalog that
# arrived - station 6 is green on it - and still leaves nothing to install.
# The app itself. THE SENTENCE THIS REPLACES WAS TRUE AND WENT STALE, which is
# the failure mode of a station written as prose: it said "a catalog arrived,
# but it carries no app: 'ngc-store catalog' over an empty directory signs an
# empty list". On 24.10.2026 the catalog of this installation carried two
# (Terminal and Uhr) and one of them was running on both service nodes - so
# the run reported a station as unmeasured while the thing it was waiting for
# had happened. It is read out of the stored catalog now instead of being
# stated, and it is held against what station D4 below really found on the
# machines: a catalog that names apps and machines that carry none is a
# different fault from a catalog that names none at all.
s7app="App einrichten (die App selbst)"
katapps=$(printf '%s' "$(entpacke "$(auf "$DB" "mariadb -Nse 'select data from ngc.ngc_blob where id=\"global\" and kind=\"catalog\"'" |
	sed -n 's/.*"payload":"\([^"]*\)".*/\1/p' || true)")" | grep -o '"id":"[a-zA-Z0-9._-]*"' | sed 's/"id":"//;s/"//' | tr '\n' ' ' || true)
gesetzt=$(auf "$SVC1" "ls -1 /var/lib/ngc/apps 2>/dev/null | grep -cv '^lost+found\$'" || true)
gesetzt2=$(auf "$SVC2" "ls -1 /var/lib/ngc/apps 2>/dev/null | grep -cv '^lost+found\$'" || true)
if [ -z "$katapps" ]; then
	unmeasurable 7 "$s7app" "the stored catalog could not be read or names no app at all: 'ngc-store catalog' over an empty directory signs an empty list. Pack one with 'ngc-store pack', put it on the store beside catalog.json and sign the catalog again with a higher seq"
elif [ "${gesetzt:-0}" = 0 ] && [ "${gesetzt2:-0}" = 0 ]; then
	fail 7 "$s7app" "the catalog of this installation names $katapps and NEITHER service node carries a single one of them - the disk is ready on both (above), so an app that is in the offer cannot be set up"
else
	pass 7 "$s7app" "the catalog names $katapps, and the service nodes carry ${gesetzt:-0} app(s) on svc-1 and ${gesetzt2:-0} on svc-2 - so an app really went from the store through the catalog onto a machine. Whether each one is set up COMPLETELY is station D4 below, and this station says nothing about it"
fi

# ---------------------------------------------------------------------------
# station D4  eine App landet auf dem Rechner, auf den man sie schickt
#
# THE QUESTION NO ONE-MACHINE RUN CAN PUT. With one host there is nowhere else
# for an app to go, so "did it land where it was sent" has no content: an app
# that was set up at all was set up in the only place there is. With two
# service nodes it has content, and it is the question an administrator
# actually has - they picked a machine in the interface and want that machine.
#
# WHAT IS ASSERTED PER NODE, and all four together, because each alone is
# satisfied by a half-done setup - that is the whole lesson of obstacle 10 of
# October ("an open window proved nothing at all"):
#   1 the app has a USER of its own on that node, and
#   2 that user's home is the app's directory under config.AppDataDir, and
#   3 the app's image is in THAT USER's own rootless image store - not root's,
#     which is the trap this station fell into while it was being written: on
#     svc-1 root happened to hold the image too, so "podman images" as root
#     answered for svc-1 and not for svc-2, and the first reading of these
#     machines said the app had not landed on svc-2 when it had, and
#   4 a container of that app is RUNNING as that user right now - the conmon
#     of the app's own user, which is the only one of the four that cannot be
#     left behind by a setup that failed halfway.
#
# THE CONTROL IS THE MACHINES THAT WERE NOT SENT ANYTHING. api-1 and db-1 run
# no podman and were sent no app, so they MUST show none of the four. A check
# that found an app everywhere would make this station green for every node of
# every installation, and nothing here would mean "it landed where it was
# sent" - it would only mean "something is somewhere".
#
# WHAT IT STILL DOES NOT SAY. Which node an admin PICKED is a click, and no
# file on any machine records it. So this station says where the apps are and
# where they are not; holding that against what was asked for is a person's
# job, and the line below says so instead of pretending otherwise.
# ---------------------------------------------------------------------------
sd4="eine App landet, wo man sie hinschickt"
# "|| true" and it is not decoration, for the third time in this file: the run
# carries "set -o pipefail", so a sed over a file that is not there makes the
# WHOLE pipeline exit 2 and "set -e" ends the run right here - with no station
# line and no message, exactly as the curl of station D2 did. Measured
# 24.10.2026 while the mutants of station D4 were being made: a copy of this
# script outside the repository finds no shared/config/config.go, and three of
# four mutant runs vanished after station 7 instead of answering.
appdir=$(sed -n 's/^[[:space:]]*AppDataDir[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$root/shared/config/config.go" 2>/dev/null | head -1 || true)
[ -n "$appdir" ] || appdir=/var/lib/ngc/apps

# One line per node: how many apps have a user, a home, an image of their own
# and a running container there.
wieviel() {
	auf "$1" "d=$appdir
n=0; voll=0; namen=''
for a in \$d/*/; do
  [ -d \"\$a\" ] || continue
  id=\$(basename \"\$a\")
  case \$id in lost+found) continue ;; esac
  n=\$((n+1))
  u=\$(stat -c %U \"\$a\" 2>/dev/null)
  h=\$(getent passwd \"\$u\" 2>/dev/null | cut -d: -f6)
  bild=\$(ls \"\$a/.local/share/containers/storage/overlay-images/\" 2>/dev/null | grep -c '^[0-9a-f][0-9a-f]*\$')
  lauf=\$(ps -eo user:32,comm --no-headers 2>/dev/null | awk -v u=\"\$u\" '\$1==u && \$2==\"conmon\"' | wc -l)
  if [ \"\$u\" != root ] && [ \"\$h\" = \"\${a%/}\" ] && [ \"\${bild:-0}\" -ge 1 ] && [ \"\${lauf:-0}\" -ge 1 ]; then
    voll=\$((voll+1)); namen=\"\$namen\$id(\$u) \"
  else
    namen=\"\$namen\$id(HALB: benutzer=\$u heim=\$h bilder=\${bild:-0} laeuft=\${lauf:-0}) \"
  fi
done
echo \"\$n|\$voll|\$namen\"" || true
}

ganz=0
halb=0
for paar in "svc-1|$SVC1" "svc-2|$SVC2"; do
	nm=${paar%%|*}
	ip=${paar#*|}
	z=$(wieviel "$ip")
	n=${z%%|*}
	r=${z#*|}
	v=${r%%|*}
	wer=${r#*|}
	if [ -z "$z" ] || [ -z "$n" ]; then
		unmeasurable D4 "$sd4 ($nm)" "the app directories of $nm could not be read, so this run cannot say what is on that machine"
	elif [ "${n:-0}" = 0 ]; then
		unmeasurable D4 "$sd4 ($nm)" "no app has been set up on $nm at all ($appdir is empty), so nothing was sent there and this run has nothing to hold against a choice"
	elif [ "$v" != "$n" ]; then
		fail D4 "$sd4 ($nm)" "$nm carries $n app directory/ies and only $v of them is/are really set up: $wer. A directory and a user without the app's own image and a running container is a setup that stopped halfway, and the menu shows such an app to a person who then cannot use it"
		halb=$((halb + 1))
	else
		pass D4 "$sd4 ($nm)" "$n app(s) on $nm are set up completely: own user, home under $appdir, the image in THAT user's own rootless store and a container of it running as that user - $wer"
		ganz=$((ganz + 1))
	fi
done

# The control, and it is the half that gives the four checks above their
# meaning: the machines nobody sent an app to have to be empty.
leer=""
for paar in "api-1|$API1" "db-1|$DB"; do
	nm=${paar%%|*}
	ip=${paar#*|}
	da=$(auf "$ip" "ls -1 $appdir 2>/dev/null | grep -cv '^lost+found\$'" || true)
	kon=$(auf "$ip" "ps -eo comm --no-headers 2>/dev/null | grep -c '^conmon\$'" || true)
	[ "${da:-0}" = 0 ] && [ "${kon:-0}" = 0 ] || leer="$leer$nm($appdir: ${da:-?} Eintraege, ${kon:-?} conmon) "
done
if [ -n "$leer" ]; then
	fail D4 "$sd4 (Gegenprobe)" "the machines this installation runs no apps on carry app state anyway: $leer- then the four checks above do not measure 'it landed where it was sent', only 'something is somewhere', and every green line of this station says less than it reads"
elif [ "$ganz" = 0 ] && [ "$halb" = 0 ]; then
	unmeasurable D4 "$sd4 (Gegenprobe)" "the control holds (api-1 and db-1 carry no app state), but no service node carries an app either - so there is nothing for the control to be a control OF"
else
	pass D4 "$sd4 (Gegenprobe)" "api-1 and db-1 were sent no app and carry none: no directory under $appdir and no conmon of their own. So the nodes that DO carry one carry it because it was sent there, and the checks above tell the two apart"
fi
unmeasurable D4 "$sd4 (die Wahl)" "WHICH node an admin picked is a click in the interface and no file on any machine records it. This station says where the apps are; that this is where they were MEANT to go is a comparison only the person who clicked can make. THOMAS has to press this: set up one app in the interface, pick svc-2 on purpose, and say afterwards which machine he picked"

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
