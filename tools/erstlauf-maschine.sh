#!/usr/bin/env bash
# Phase B of the Erstlauf: the stations that only a MACHINE can answer.
#
# Runs ON a throwaway machine, as root, on Debian or Ubuntu with systemd. It
# installs NGC the way a user does - with the ulh script, from a signed release -
# and then asks the stations. tools/erstlauf.sh is phase A and decides nothing
# here; docs/erstlauf.md holds the inventory and why the two phases are split.
#
# Usage, as root on the throwaway machine:
#   erstlauf-maschine.sh --staged <dir> --ulh <dir> [--test-release-key <pem>]
#                        [--hostname <name>] [--name <node>]
#
#   --staged  a directory with release.json, release-key.pem and the tar of
#             each part (ngc-api-<arch>.tar, ngc-controller-<arch>.tar). It is
#             copied to /var/lib/ngc/staged, the way shared/ulh.md describes for
#             an installation without internet.
#   --ulh     a checkout of github.com/sorglos-it/ulh that CARRIES the NGC
#             scripts. They are not on ulh's main (measured 23.10.2026); they
#             live on the branches ngc-kette / pruefsatz-ngc-kette.
#   --test-release-key  see THE ONE BOLT THIS RUN REPLACES below.
#
# ============================================================
# THE ONE BOLT THIS RUN REPLACES, AND WHAT THAT COSTS
# ============================================================
# ngc-all.sh carries the public release key of NGC in its text, and that key is
# the whole trust in a release. Nobody but NGC can sign a release it accepts, so
# a run against a release built here has two choices: not run at all, or name a
# test root. This script takes the second and says so at every turn - the station
# message carries "test root", not "ok" alone.
#
# What still runs: the signature check itself, byte for byte, including the
# checksums and the size (verify_release of the script). A tampered file fails
# here exactly as it would in the field. What is NOT measured: that NGC's own
# key is in the script and correct. That stays a job for a real release.
# The rule behind this is messen.md: a fixture that replaces a security bolt is
# named at the line and in every report that carries a number from that path.
set -euo pipefail

if [ -z "${BASH_VERSION:-}" ]; then
	echo "erstlauf-maschine: run with bash, not sh" >&2
	exit 1
fi
[ "$(id -u)" = 0 ] || {
	echo "erstlauf-maschine: has to run as root - it installs services" >&2
	exit 1
}

# The leak test from messen.md, in band. A shell that swallows exit codes writes
# FAIL and 0 into the same run, and the caller reads the 0.
probe=$( (exit 7) >/dev/null 2>&1 || echo "$?")
[ "$probe" = 7 ] || {
	echo "erstlauf-maschine: this shell loses exit codes (the (exit 7) probe answered '$probe')" >&2
	exit 1
}

staged="" ulhdir="" testkey="" host="" node="erstlauf" storeurl="" storekey=""
while [ $# -gt 0 ]; do
	case $1 in
	--staged)
		staged=${2:-}
		shift 2
		;;
	--ulh)
		ulhdir=${2:-}
		shift 2
		;;
	--test-release-key)
		testkey=${2:-}
		shift 2
		;;
	--hostname)
		host=${2:-}
		shift 2
		;;
	--store-url)
		storeurl=${2:-}
		shift 2
		;;
	--store-key)
		storekey=${2:-}
		shift 2
		;;
	--name)
		node=${2:-}
		shift 2
		;;
	-h | --help)
		sed -n '2,33p' "${BASH_SOURCE[0]}"
		exit 0
		;;
	*)
		echo "erstlauf-maschine: unknown argument $1" >&2
		exit 1
		;;
	esac
done

[ -n "$staged" ] && [ -d "$staged" ] || {
	echo "erstlauf-maschine: --staged <dir> is needed and has to be a directory" >&2
	exit 1
}
[ -n "$ulhdir" ] && [ -d "$ulhdir" ] || {
	echo "erstlauf-maschine: --ulh <dir> is needed and has to be a directory" >&2
	exit 1
}
script=$ulhdir/apps/cli/assets/scripts/ngc-all.sh
[ -f "$script" ] || {
	echo "erstlauf-maschine: $script is missing - the NGC scripts are not on ulh's main, use a branch that carries them (ngc-kette)" >&2
	exit 1
}
# The address of the interface the default route uses, not simply the first one
# in the list: on WSL the first global address is 10.255.255.254/32, a host-side
# stub, and an installation that writes it into api.json points at nothing.
if [ -z "$host" ]; then
	dev=$(ip -4 route show default 2>/dev/null | awk 'NR==1{print $5}')
	[ -n "$dev" ] && host=$(ip -4 -o addr show dev "$dev" scope global 2>/dev/null | awk 'NR==1{sub(/\/.*/,"",$4); print $4}')
fi
[ -n "$host" ] || {
	echo "erstlauf-maschine: no IPv4 address found; pass --hostname" >&2
	exit 1
}

fails=0
unmeasured=0
unmeasured_list=""
pass() { printf 'station %s %s: ok -- %s\n' "$1" "$2" "$3"; }
fail() {
	printf 'station %s %s: FAIL -- %s\n' "$1" "$2" "$3"
	fails=$((fails + 1))
}
broken() {
	printf 'erstlauf-maschine: the run cannot measure station %s (%s): %s\n' "$1" "$2" "$3" >&2
	exit 1
}
unmeasurable() {
	printf 'station %s %s: UNMEASURABLE -- %s\n' "$1" "$2" "$3"
	unmeasured=$((unmeasured + 1))
	unmeasured_list="${unmeasured_list}    station $1 $2: $3
"
}

work=$(mktemp -d)
chmod 0700 -- "$work"
trap 'rm -rf -- "$work"' EXIT

echo "erstlauf-maschine: $(sed -n 's/^PRETTY_NAME="\(.*\)"$/\1/p' /etc/os-release), kernel $(uname -r), host $host"

# ---------------------------------------------------------------------------
# Before any station: the machine has to be able to answer at all. A run that
# steps aside on a missing tool measures nothing and looks like a result, so
# every prerequisite is a hard stop with its own sentence.
# ---------------------------------------------------------------------------
[ "$(ps -p 1 -o comm=)" = systemd ] ||
	broken 0 "voraussetzungen" "pid 1 is '$(ps -p 1 -o comm=)', not systemd - the ulh script installs services and this run asks systemd about them"
# Only what NOBODY installs for this run. openssl, jq, curl, ca-certificates
# and podman are NGC_PACKAGES of ngc-all.sh, and ensure_packages installs every
# one of them that is missing - so demanding them here refused to measure the
# one machine this whole run is about: a fresh clone of the Proxmox template,
# which carries no jq (measured 29.10.2026, VMID 310). A precondition the
# script under test fulfils itself is not a precondition of the test.
for t in tar sha256sum systemctl dpkg-query; do
	command -v "$t" >/dev/null 2>&1 ||
		broken 0 "voraussetzungen" "$t is missing, and no step of the installation brings it - this run cannot measure anything on this machine"
done

# ---------------------------------------------------------------------------
# The installation, the way a user runs it: the staged release into
# /var/lib/ngc/staged, then the ulh script with the answers in its environment.
# ---------------------------------------------------------------------------
mkdir -p /var/lib/ngc/staged
cp -f -- "$staged"/* /var/lib/ngc/staged/
[ -f /var/lib/ngc/staged/release.json ] ||
	broken 0 "installieren" "$staged carries no release.json, so fetch_release would go to the internet and this run would measure a download"

run=$script
# mit_testschluessel writes a copy of an ulh script whose NGC_RELEASE_KEY is
# the test root, and refuses to hand back a copy the replacement missed.
#
# IT TAKES A SCRIPT NAME, because the run needs TWO of them. Until 29.10.2026
# only ngc-all.sh was swapped, and ngc-web.sh - which carries the very same
# key and checks the very same release - was left with NGC's own. So
# "ngc-web install" refused every release this run can build ("release wrong:
# signature does not verify"), the web interface was never installed, and
# station 3 said the interface "has to be installed first" on every run there
# has ever been. Six stations behind it were therefore unreachable not because
# they are hard, but because the run could not get past a bolt it had already
# decided to replace. Measured on a fresh clone of the Proxmox template.
mit_testschluessel() {
	local src=$1 out=$2 want
	# The copy has to lie NEXT TO the original: the ulh scripts source
	# ../../classes/bootstrap.sh relative to their own place, and a copy in a
	# mktemp directory dies at line 105 with "No such file or directory" - which
	# looks like a fault of the script and is a fault of the run. Measured
	# 23.10.2026 on the first machine run.
	#
	# The replacement is done with awk and not with sed, because the key is a
	# multi-line value and a sed over line boundaries is where a measurement
	# dies at its own separator (messen.md, 10.10.2026).
	awk -v keyfile="$testkey" '
		/^NGC_RELEASE_KEY="-----BEGIN PUBLIC KEY-----$/ {
			printf "NGC_RELEASE_KEY=\"";
			while ((getline line < keyfile) > 0) {
				if (n++) printf "\n";
				printf "%s", line;
			}
			printf "\"\n";
			skip = 1; next
		}
		skip && /^-----END PUBLIC KEY-----"$/ { skip = 0; next }
		skip { next }
		{ print }
	' "$src" >"$out"
	# The preparation is checked before anything is concluded from it: a
	# replacement that did not take leaves the run measuring the ORIGINAL key,
	# and it would then fail for a reason nobody could see.
	want=$(sed -n '2p' "$testkey")
	grep -q -- "$want" "$out" ||
		broken 0 "installieren" "the test key did not get into the copy of $(basename "$src") - the run would measure the original key"
	bash -n "$out" ||
		broken 0 "installieren" "the copy of $(basename "$src") with the test key does not parse any more"
}

webscript=$ulhdir/apps/cli/assets/scripts/ngc-web.sh
webrun=$webscript
if [ -n "$testkey" ]; then
	[ -f "$testkey" ] ||
		broken 0 "installieren" "--test-release-key names no readable file: $testkey"
	grep -q 'BEGIN PUBLIC KEY' "$testkey" ||
		broken 0 "installieren" "$testkey is no PEM public key"
	run=$(dirname "$script")/ngc-all.erstlauf.sh
	webrun=$(dirname "$script")/ngc-web.erstlauf.sh
	trap 'rm -rf -- "$work"; rm -f -- "'"$run"'" "'"$webrun"'"' EXIT
	mit_testschluessel "$script" "$run"
	[ -f "$webscript" ] && mit_testschluessel "$webscript" "$webrun"
	echo "erstlauf-maschine: TEST ROOT - the release key of ngc-all.sh and of ngc-web.sh was replaced by $testkey."
	echo "erstlauf-maschine: the signature check itself runs unchanged; that NGC's OWN key is right is NOT measured here."
fi

# A STORE HAS TO BE NAMED, AND THAT IS A FINDING OF THIS RUN, not a setting.
# Measured 23.10.2026: with both store answers empty, ngc-all.sh install stops
# with "NGC has no official app store yet, so this installation needs a store of
# its own [...] Leaving them empty would write a placeholder into
# /etc/ngc/api.json, and the API refuses that file." So an installation from a
# published release cannot be completed today unless the admin invents a store.
# The run names one so that it gets PAST station 1 - the store is not running,
# which is why station 6 stays unmeasurable and says so.
if [ -z "$storeurl" ]; then
	storeurl="https://store.erstlauf.invalid"
	echo "erstlauf-maschine: NAMED STORE - the install refuses to run with the store answers empty, so this run names $storeurl."
	echo "erstlauf-maschine: nothing answers there; station 6 therefore stays unmeasurable, and no number here says a catalog arrived."
fi
if [ -z "$storekey" ]; then
	storekey=$(openssl rand 32 | base64 -w0 | tr '+/' '-_' | tr -d '=')
	[ ${#storekey} -eq 43 ] ||
		broken 0 "installieren" "the store key made here is ${#storekey} characters, not 43 - openssl or base64 did something else than expected"
fi

echo "erstlauf-maschine: installing with the ulh script ..."
set +e
env NGC_NAME="$node" NGC_HOSTNAME="$host" NGC_STORE_URL="$storeurl" NGC_STORE_KEY="$storekey" \
	bash "$run" install >"$work/install.log" 2>&1
installrc=$?
set -e
echo "erstlauf-maschine: the install ended with code $installrc; the last lines:"
tail -n 15 "$work/install.log" | sed 's/^/    /'

# ---------------------------------------------------------------------------
# station 1  installieren
#
# Obstacle 1 of October: ngc-api.service carried ProtectSystem=strict and
# ReadWritePaths without /etc/ngc, so the API could never move the pin in
# /etc/ngc/trust-anchor.json up. pinned=1 while the database stood at 2, and
# nothing in the system named the cause.
#
# PHASE A ASKS THE TEXT OF THE SCRIPT. THIS ASKS THE SANDBOX. The difference is
# the whole point of a machine run: systemd is asked what it really grants, and
# then a write into that very sandbox is tried with systemd-run. A unit that
# looks right in the file and still forbids the write fails here.
# ---------------------------------------------------------------------------
s1="installieren"
if [ "$installrc" != 0 ]; then
	fail 1 "$s1" "the ulh script ended with code $installrc, so nothing is installed: $(grep -iE 'error|fehler' "$work/install.log" | tail -3 | tr '\n' ' ')"
else
	missing=""
	for p in /usr/lib/ngc/ngc-api /usr/lib/ngc/ngc-store /usr/lib/ngc/ngc-controller; do
		[ -x "$p" ] || missing="$missing$p "
	done
	[ -f /etc/systemd/system/ngc-api.service ] || missing="$missing/etc/systemd/system/ngc-api.service "
	if [ -n "$missing" ]; then
		fail 1 "$s1" "the install said 0, but these are not there: $missing"
	else
		# What systemd really grants, not what the file says. systemctl show
		# answers for the unit as loaded, including a drop-in nobody looked at.
		rwp=$(systemctl show -p ReadWritePaths --value ngc-api.service 2>/dev/null || true)
		prot=$(systemctl show -p ProtectSystem --value ngc-api.service 2>/dev/null || true)
		anchor=/etc/ngc/trust-anchor.json
		if [ "$prot" = "" ] || [ "$prot" = no ]; then
			# No sandbox at all: then this station cannot fail for the October
			# reason, and saying "ok" would hide that the bolt is gone.
			fail 1 "$s1" "ngc-api.service has ProtectSystem='$prot', so the confinement obstacle 1 lived in is not there at all - the unit lost its sandbox"
		else
			# The real question, put to systemd: can a process under THIS
			# unit's sandbox write the anchor? systemd-run is given the same
			# two settings, so the answer is about the paths and nothing else.
			probe_out=$work/probe.txt
			# --pipe, or the reason is lost: without it systemd-run keeps the
			# probe's stderr in the journal and the FAIL below promises a reason
			# it cannot give. With it the line reads "touch: cannot touch
			# '/etc/ngc/trust-anchor.json.erstlauf': Read-only file system",
			# which is the whole finding in one sentence (measured 23.10.2026).
			if systemd-run --quiet --wait --collect --pipe \
				-p ProtectSystem="$prot" -p ReadWritePaths="$rwp" \
				/bin/sh -c "touch $anchor.erstlauf && rm -f $anchor.erstlauf" >"$probe_out" 2>&1; then
				pass 1 "$s1" "the install came off and the sandbox of ngc-api.service (ProtectSystem=$prot, ReadWritePaths=$rwp) really lets $anchor be written${testkey:+ -- measured with a TEST release root}"
			else
				fail 1 "$s1" "ngc-api.service grants ProtectSystem=$prot with ReadWritePaths='$rwp', and a process in that sandbox CANNOT write $anchor: $(tr '\n' ' ' <"$probe_out") -- the pin outside the database can never be moved up, and the installation gets stuck without naming a cause"
			fi
		fi
	fi
fi

# ---------------------------------------------------------------------------
# station 2  API und Controller laufen
#
# Obstacle 2 of October: the programs of the release reported 0.0.0-dev, and the
# interface threw away the whole machine list for it. Here the installed binary
# is asked, not one built for the occasion - that is the difference to phase A.
#
# AND THE SERVICE HAS TO STAY UP. NRestarts is no restart counter: measured
# 18.10.2026, ngc-api died four times, the journal counted to 3 and
# "systemctl show -p NRestarts" stood at 0 in every sample, because systemd
# resets it as soon as the unit runs clean again. The proof is the MainPID over
# time.
# ---------------------------------------------------------------------------
s2="API und Controller laufen"
if [ ! -x /usr/lib/ngc/ngc-api ]; then
	unmeasurable 2 "$s2" "there is no /usr/lib/ngc/ngc-api, so station 1 has to come off first"
else
	ver=$(/usr/lib/ngc/ngc-api -version 2>&1 | head -1 | awk '{print $2}')
	# The same pattern the interface uses, and nothing laxer: three numbers
	# without a leading zero (contracts/text.js, store.ValidVersion).
	vre='^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})$'
	bad=0
	grep -Eq -- "$vre" <<<"$ver" || {
		fail 2 "$s2" "the INSTALLED /usr/lib/ngc/ngc-api reports version '$ver', which the interface's validVersion refuses - it throws the whole machine list away for it"
		bad=1
	}
	if ! systemctl is-active --quiet ngc-api.service; then
		fail 2 "$s2" "ngc-api.service is not active ($(systemctl is-active ngc-api.service 2>&1)): $(journalctl -u ngc-api.service -n 5 --no-pager 2>/dev/null | tr '\n' ' ')"
		bad=1
	else
		pid1=$(systemctl show -p MainPID --value ngc-api.service)
		sleep 10
		pid2=$(systemctl show -p MainPID --value ngc-api.service)
		if [ "$pid1" != "$pid2" ]; then
			fail 2 "$s2" "ngc-api.service restarted during this run (MainPID $pid1 -> $pid2), so it is crash-looping; NRestarts says $(systemctl show -p NRestarts --value ngc-api.service) and that number is not a restart counter"
			bad=1
		fi
	fi
	[ "$bad" = 0 ] && pass 2 "$s2" "the installed ngc-api reports $ver, ngc-api.service is active and kept one MainPID ($pid1) over 10 s"
fi

# ---------------------------------------------------------------------------
# station 2w  die Oberflaeche installieren
#
# WHY THIS STATION IS NEW AND WHY IT IS NUMBERED 2w. It is step 4 of
# docs/installation-kleines-system.md, it sits between station 2 and station 3
# in the ORDER (the pairing code of station 3 comes out of the browser, and
# without this there is no browser page to get it from), and the numbers of the
# ten stations are quoted in docs/erstlauf.md - so it gets a letter instead of
# pushing eight numbers along.
#
# It was measured by hand on 29.10.2026 and nothing in this run asked it: the
# page answers 200 with NGC's own <title>, and /api/v1/setup answers 200
# THROUGH APACHE, which is the only thing that proves the whole stretch
# browser -> apache -> balancer -> api. A run that installs the API and stops
# has measured half a machine.
#
# The pin is computed here and not typed: ngc-web.sh refuses a pin that does
# not belong to the API's certificate, and a pin pasted from somewhere else
# would make this station a measurement of the paste.
# ---------------------------------------------------------------------------
s2w="Oberflaeche installieren"
if [ ! -f "$webscript" ]; then
	unmeasurable 2w "$s2w" "$ulhdir carries no apps/cli/assets/scripts/ngc-web.sh - the NGC scripts are on their own branches of ulh (ngc-kette)"
elif [ ! -f /etc/ngc/tls/api.crt ]; then
	unmeasurable 2w "$s2w" "there is no /etc/ngc/tls/api.crt, so station 1 has to come off first"
else
	pin=$(openssl x509 -in /etc/ngc/tls/api.crt -noout -pubkey 2>/dev/null |
		openssl pkey -pubin -outform der 2>/dev/null |
		openssl dgst -sha256 -binary 2>/dev/null | base64 -w0 | tr '+/' '-_' | tr -d '=')
	[ ${#pin} -eq 43 ] ||
		broken 2w "$s2w" "the pin of /etc/ngc/tls/api.crt came out ${#pin} characters, not 43 - openssl did something else than expected"
	set +e
	env NGC_HOSTNAME="$host" NGC_CERT_MODE=selfsigned \
		NGC_APIS="https://$host:7443" NGC_API_PINS="$pin" \
		bash "$webrun" install >"$work/web.log" 2>&1
	webrc=$?
	set -e
	if [ "$webrc" != 0 ]; then
		fail 2w "$s2w" "ngc-web.sh install ended with code $webrc: $(grep -iE 'error|wrong|fehler|✗' "$work/web.log" | tail -3 | tr '\n' ' ')"
	else
		code=$(curl -sk -o "$work/index.html" -w '%{http_code}' "https://$host/" 2>/dev/null)
		setup=$(curl -sk -o /dev/null -w '%{http_code}' "https://$host/api/v1/setup" 2>/dev/null)
		rp=$(curl -sk "https://$host/config.json" 2>/dev/null | jq -r '.rp.id // "none"' 2>/dev/null)
		wrong=""
		systemctl is-active --quiet apache2 || wrong="apache2 is not active; "
		[ "$code" = 200 ] || wrong="${wrong}GET / answered $code, not 200; "
		grep -q '<title>NGC</title>' "$work/index.html" ||
			wrong="${wrong}the page at / is not NGC's own (no <title>NGC</title>); "
		[ "$setup" = 200 ] || [ "$setup" = 404 ] ||
			wrong="${wrong}/api/v1/setup answered $setup through apache, so the stretch browser -> apache -> balancer -> api does not carry; "
		[ "$rp" = "$host" ] ||
			wrong="${wrong}/config.json names rp.id '$rp', not '$host' - the browser would bind passkeys to another name; "
		if [ -n "$wrong" ]; then
			fail 2w "$s2w" "${wrong%; }"
		else
			pass 2w "$s2w" "apache2 is active, / answers 200 with NGC's own page, /api/v1/setup answers $setup THROUGH apache, and /config.json names rp.id $host"
		fi
	fi
fi

# ---------------------------------------------------------------------------
# station 3  Controller koppeln       station 4  aufnehmen
# station 5  Systemrechte setzen      station 6  Katalog da
# station 7  App einrichten           station 8  App erscheint im Menue
#
# What each of these still needs, named so the next person sees a gap in the
# search and does not have to believe a sentence (messen.md: an absence is shown
# with places, not with searchers).
# ---------------------------------------------------------------------------
unmeasurable 3 "Controller koppeln" "the pairing code comes out of the BROWSER: a person signs in on the page station 2w put up, makes the code there, and only then does ngc-all.sh take it as its answer 'pair_code'. Nothing on this machine can sign in, so this is the station where the run ends and a human carries on"
unmeasurable 4 "aufnehmen" "needs station 3, and then a click through the interface with a signed-in admin"
unmeasurable 5 "Systemrechte setzen" "needs station 4"
unmeasurable 6 "Katalog da" "needs a store with a signed catalog in the trust list; ngc-all.sh takes store_url and store_key, and NGC's own store key is still a placeholder in the script (NGC_STORE_KEY_DEFAULT=REPLACE-THIS-LINE-...)"
# Three sources, because the first two answer on no Debian at all and the run
# then told a machine that CAN encrypt that no app could ever be set up on it
# (measured 29.10.2026 on a fresh clone of the Proxmox template, VMID 310):
# /proc/config.gz does not exist on Debian - the config ships as
# /boot/config-$(uname -r) - and /sys/fs/ext4/features/encryption is a FILE,
# so the "-d" below was false on every machine that has it. A probe that is
# wrong in the safe-looking direction is the worse half: it hands out a red
# verdict nobody checks.
if zgrep -q '^CONFIG_FS_ENCRYPTION=y' /proc/config.gz 2>/dev/null ||
	grep -q '^CONFIG_FS_ENCRYPTION=y' "/boot/config-$(uname -r)" 2>/dev/null ||
	[ -e /sys/fs/ext4/features/encryption ]; then
	unmeasurable 7 "App einrichten" "this kernel can encrypt, so the obstacle is not here - what is missing is station 6 and an app image"
else
	# The repository says this itself, at the one place that answers for it:
	# serve/activate.go, "On a kernel without CONFIG_FS_ENCRYPTION every
	# activation ends here (measured on 6.18.33.2-microsoft-standard-WSL2)".
	unmeasurable 7 "App einrichten" "this kernel has no CONFIG_FS_ENCRYPTION ($(uname -r)), and runtime.ErrNoFscrypt ends EVERY activation on such a kernel (apps/server-controller/classes/serve/activate.go says so at the line) - no app can be set up here, whatever else is right"
fi
unmeasurable 8 "App erscheint im Menue" "needs station 7, and a second user to show that the rights page offers an app one cannot see yet"
# ---------------------------------------------------------------------------
# station 9  Recht "benutzen" vergeben
# station 10 Die App ist wirklich bedienbar
#
# The two obstacles that stood BEHIND the last station on 23.10.2026. Phase A
# measures the half that lives in the code of this repository, and it measures
# it by RUNNING that code (tools/erstlauf-probe); what is missing HERE is named
# with its place, so the next person sees a gap in the search rather than a
# sentence to believe.
# ---------------------------------------------------------------------------
unmeasurable 9 "Recht benutzen vergeben" "needs station 7 and a SECOND USER - and a user is a new version of the trust list, which is signed in the BROWSER with that person own key; nothing on this machine holds that key, so no phase of this run can create the user the right would be given to"
unmeasurable 10 "App ist wirklich bedienbar" "needs station 7: an app that really runs. Phase A measures where the two streams part in serve.RunService with a probe engine; what is measured NOWHERE is that podman own engine (runtime.Podman.Exec) hands them over the same way, and that needs a container on this machine"

# ---------------------------------------------------------------------------
echo "erstlauf-maschine: $fails red, $unmeasured unmeasurable"
if [ "$unmeasured" != 0 ]; then
	printf 'erstlauf-maschine: these stations did NOT run -- the run says nothing about them:\n%s' "$unmeasured_list"
fi
[ "$fails" = 0 ] || exit 1
[ "$unmeasured" = 0 ] || exit 2
echo "erstlauf-maschine: ok"
