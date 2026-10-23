#!/usr/bin/env bash
# proxmox-sechs-ngc - NGC on the six, distributed, with the ulh scripts and a
# REAL published release.
#
# It runs from the Proxmox host (or anywhere that reaches the six by ssh) and
# drives each machine over ssh, in the order shared/ulh.md gives:
#
#   store  the store FIRST, because ngc-api wants its address and its public
#          key as answers and an installation cannot be completed without a
#          store (measured 23.10.2026: with both answers empty ngc-all.sh
#          stops). The store is NOT part of the installation - it is a plain
#          web server with signed files on it, on its own machine, so that
#          "does the API really fetch over the network, does it check the
#          certificate, what happens when the store is gone" can be asked at
#          all.
#   db     ngc-db with bootstrap=yes; it makes the CA of the installation and
#          leaves its certificate in /etc/ngc/tls/ca.crt, which api and the
#          controller of the db role need.
#   api1   ngc-api with join_code empty: the first API. It prints the setup
#          code of the first admin and the pin of its TLS key.
#   api2   ngc-api with the join code of api1: the only way to reach the
#          joining path and the bolt "this is the only API of this
#          installation" at all.
#   web    ngc-web with BOTH apis and BOTH pins, so Apache's balancer has a
#          second member and web.json holds real pins.
#
# THE RELEASE IS THE REAL ONE AND THAT IS NEW. tools/erstlauf-maschine.sh has
# to replace the release key of ngc-all.sh with a test root, because it builds
# the release it measures. Here the published v0.2.1 is used, and its
# release-key.pem is byte for byte the NGC_RELEASE_KEY the ulh scripts carry
# (compared 24.10.2026). So nothing in this run replaces a security bolt: the
# signature check runs against NGC's own key, as it would in the field.
#
# SECRETS. The database passwords are made ON the machine that needs them and
# stay in a 0600 file there (/root/ngc-antworten.env). They never stand in an
# argument of a command, because /proc/<pid>/cmdline is readable by every local
# user without hidepid, and they are never printed by this script.
#
# Usage, as root ON THE PROXMOX HOST:
#   proxmox-sechs-ngc.sh laden     fetch release v0.2.1 and stage it on the five
#   proxmox-sechs-ngc.sh ulh       put the ulh checkout with the NGC scripts there
#   proxmox-sechs-ngc.sh store     the store on its own machine, with its own CA
#   proxmox-sechs-ngc.sh db        ngc-db on db-1
#   proxmox-sechs-ngc.sh api1      ngc-api on web-api-1 (first API)
#   proxmox-sechs-ngc.sh api2      ngc-api on api-2 (joining API)
#   proxmox-sechs-ngc.sh web       ngc-web on web-api-1
#   proxmox-sechs-ngc.sh messen    the distributed questions, changes nothing
set -euo pipefail

if [ -z "${BASH_VERSION:-}" ]; then
	echo "sechs-ngc: run with bash, not sh" >&2
	exit 1
fi
probe=$( (exit 7) >/dev/null 2>&1 || echo "$?")
[ "$probe" = 7 ] || {
	echo "sechs-ngc: this shell loses exit codes (the (exit 7) probe answered '$probe')" >&2
	exit 1
}

ZONE=${NGC_ZONE:-ngc.mein.lan}
WEB_IP=${NGC_WEB_IP:-10.2.3.210}
API1_IP=${NGC_API1_IP:-10.2.3.210}
API2_IP=${NGC_API2_IP:-10.2.3.211}
DB_IP=${NGC_DB_IP:-10.2.3.212}
SVC1_IP=${NGC_SVC1_IP:-10.2.3.213}
SVC2_IP=${NGC_SVC2_IP:-10.2.3.214}
STORE_IP=${NGC_STORE_IP:-10.2.3.215}
NUTZER=${NGC_CIUSER:-root}

# Measured 24.10.2026: this is the newest published release, it carries all
# five tars, and its release-key.pem equals the key in ulh's scripts.
RELEASE=${NGC_RELEASE:-v0.2.1}
REPO=${NGC_REPO:-sorglos-it/nextgencloud}
# Measured 24.10.2026: ngc-kette is on ulh's origin at afb2ea4. The local
# checkout on the developer machine is AHEAD of it with work that is not pushed,
# and a run must not depend on that - so the public branch is used and named.
ULH_URL=${NGC_ULH_URL:-https://github.com/sorglos-it/ulh}
ULH_REF=${NGC_ULH_REF:-ngc-kette}

STAGED=/var/lib/ngc/staged
ANTWORTEN=/root/ngc-antworten.env
ARBEIT=${NGC_WORK:-/root/ngc-sechs}

fehler() {
	echo "sechs-ngc: $*" >&2
	exit 1
}

auf() {
	local ip=$1
	shift
	ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=20 \
		"$NUTZER@$ip" "LC_ALL=C; export LC_ALL; $*"
}

# datei_hin pipes stdin into a file on a machine, with a mode, without the
# content ever being an argument.
datei_hin() {
	local ip=$1 pfad=$2 modus=$3
	# The directory first: a target under /etc/ngc exists only after a script
	# of ulh has run there, and "install: cannot create regular file" is a
	# confusing way to learn that.
	auf "$ip" "mkdir -p \"\$(dirname $pfad)\" && install -m $modus /dev/null $pfad && cat > $pfad"
}

NGC_MASCHINEN="$API1_IP $API2_IP $DB_IP $SVC1_IP $SVC2_IP"

# ------------------------------------------------------------------ laden
laden() {
	local ip
	local teil
	mkdir -p -- "$ARBEIT/release"
	# curl and not gh: a Proxmox host has no gh, and it needs none - the
	# release is public (measured 24.10.2026: an unauthenticated HEAD on
	# release.json answers 200), so no token is involved in this run at all.
	for teil in release.json release-key.pem ngc-api-amd64.tar \
		ngc-controller-amd64.tar ngc-web-all.tar; do
		[ -s "$ARBEIT/release/$teil" ] && continue
		curl -fsSL -o "$ARBEIT/release/$teil" \
			"https://github.com/$REPO/releases/download/$RELEASE/$teil" ||
			fehler "could not fetch $teil of $RELEASE"
	done
	# The check before anything is staged: the ulh scripts refuse a release
	# whose signature does not hold, and finding that out on five machines is
	# five times the work of finding it out once.
	[ -s "$ARBEIT/release/release-key.pem" ] || fehler "release-key.pem is missing"
	printf 'laden: %s with %s assets\n' "$RELEASE" "$(find "$ARBEIT/release" -type f | wc -l)"
	for ip in $NGC_MASCHINEN; do
		auf "$ip" "mkdir -p $STAGED"
		# Only the amd64 parts and the common ones: an arm64 tar on an amd64
		# machine is bytes nobody reads, and 55 MB per machine over ssh is
		# time nobody gets back.
		scp -q -o StrictHostKeyChecking=accept-new \
			"$ARBEIT/release/release.json" "$ARBEIT/release/release-key.pem" \
			"$ARBEIT/release/ngc-api-amd64.tar" "$ARBEIT/release/ngc-controller-amd64.tar" \
			"$ARBEIT/release/ngc-web-all.tar" "$NUTZER@$ip:$STAGED/" ||
			fehler "could not stage the release on $ip"
		printf 'laden: %s has the release in %s\n' "$ip" "$STAGED"
	done
	echo "laden: ok"
}

# ------------------------------------------------------------------ ulh
ulh() {
	local ip
	for ip in $NGC_MASCHINEN; do
		auf "$ip" "command -v git >/dev/null || (DEBIAN_FRONTEND=noninteractive apt-get install -y -qq git >/dev/null)"
		auf "$ip" "[ -d /root/ulh/.git ] || git clone -q --branch $ULH_REF --single-branch $ULH_URL /root/ulh"
		auf "$ip" "cd /root/ulh && git fetch -q origin $ULH_REF && git checkout -q FETCH_HEAD"
		auf "$ip" "test -f /root/ulh/apps/cli/assets/scripts/ngc-api.sh" ||
			fehler "$ip: the NGC scripts are not in the checkout - is $ULH_REF right?"
		printf 'ulh: %s has %s at %s\n' "$ip" "$ULH_REF" "$(auf "$ip" "cd /root/ulh && git rev-parse --short HEAD")"
	done
	echo "ulh: ok"
}

# ------------------------------------------------------------------ store
# The store on its own machine: a CA of its own, a certificate for its NAME, a
# signed catalog, nginx over https - and the CA into the system trust of the
# two API machines, because the API checks the certificate with the system's
# pool and nothing else (appstore.New has no TLSClientConfig at all).
store() {
	local pub
	auf "$STORE_IP" "mkdir -p /root/store /var/www/store"
	# ngc-store comes out of the release's api tar: the tar carries exactly
	# ngc-api and ngc-store (the usage text of apps/cli says so).
	scp -q "$ARBEIT/release/ngc-api-amd64.tar" "$NUTZER@$STORE_IP:/root/store/" ||
		fehler "could not copy the api tar to the store machine"
	auf "$STORE_IP" "cd /root/store && tar xf ngc-api-amd64.tar && install -m 0755 ngc-store /usr/local/bin/ngc-store" ||
		fehler "ngc-store is not in the api tar"
	# The key of the store. ngc-store makes the file itself with 0600 and
	# prints only its path, never its content. The sequence number counts up on
	# every call, which is what the rollback guard of the API is about
	# (store.VerifyRelease / ErrRollback): a catalog with a LOWER seq than the
	# one already seen has to be refused, and "messen" asks exactly that.
	auf "$STORE_IP" "cd /root/store && /usr/local/bin/ngc-store catalog /var/www/store /root/store/store.key ngc-sechs \$(( \$(cat /root/store/seq 2>/dev/null || echo 0) + 1 )) https://store.$ZONE > /root/store/catalog.log 2>&1 && echo \$(( \$(cat /root/store/seq 2>/dev/null || echo 0) + 1 )) > /root/store/seq" ||
		fehler "the catalog could not be written: $(auf "$STORE_IP" "tail -3 /root/store/catalog.log")"
	auf "$STORE_IP" "test -s /var/www/store/catalog.json" ||
		fehler "no catalog.json on the store machine: $(auf "$STORE_IP" "tail -5 /root/store/catalog.log")"
	# A CA of its own and a certificate for the NAME. Not for the IP: the API
	# fetches https://store.<zone>/catalog.json, and a certificate for an
	# address would not match that name.
	auf "$STORE_IP" "[ -s /root/store/ca.crt ] || (
		openssl genpkey -algorithm ed25519 -out /root/store/ca.key &&
		chmod 0400 /root/store/ca.key &&
		openssl req -new -x509 -key /root/store/ca.key -out /root/store/ca.crt -days 3650 -subj '/CN=ngc-sechs store CA' &&
		openssl genpkey -algorithm ed25519 -out /root/store/store.tls.key &&
		chmod 0400 /root/store/store.tls.key &&
		openssl req -new -key /root/store/store.tls.key -out /root/store/store.csr -subj '/CN=store.$ZONE' &&
		printf 'subjectAltName=DNS:store.%s\nbasicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=serverAuth\n' '$ZONE' > /root/store/ext.cnf &&
		openssl x509 -req -in /root/store/store.csr -CA /root/store/ca.crt -CAkey /root/store/ca.key \
			-CAcreateserial -out /root/store/store.crt -days 3650 -extfile /root/store/ext.cnf
	)" || fehler "the store certificate could not be made"
	# nginx: https only, the signed files and nothing else.
	printf 'server {\n  listen 443 ssl;\n  server_name store.%s;\n  ssl_certificate /root/store/store.crt;\n  ssl_certificate_key /root/store/store.tls.key;\n  root /var/www/store;\n  autoindex off;\n}\n' "$ZONE" |
		datei_hin "$STORE_IP" /etc/nginx/sites-available/store 0644
	auf "$STORE_IP" "ln -sf /etc/nginx/sites-available/store /etc/nginx/sites-enabled/store && rm -f /etc/nginx/sites-enabled/default && nginx -t >/dev/null 2>&1 && systemctl reload nginx" ||
		fehler "nginx does not take the store site"
	# The CA into the system trust of the two APIs. THIS IS THE OPERATOR'S
	# STEP and it is named as one: NGC checks the certificate against the
	# system pool, so a store with a certificate nobody trusts is refused -
	# which is the right answer and the reason this line exists.
	auf "$STORE_IP" "cat /root/store/ca.crt" >"$ARBEIT/store-ca.crt"
	local ip
	for ip in "$API1_IP" "$API2_IP"; do
		datei_hin "$ip" /usr/local/share/ca-certificates/ngc-sechs-store.crt 0644 <"$ARBEIT/store-ca.crt"
		auf "$ip" "update-ca-certificates >/dev/null 2>&1" || fehler "$ip: update-ca-certificates failed"
		printf 'store: %s trusts the store CA\n' "$ip"
	done
	# The public key of the store, base64url, as the answer store_key wants it.
	# It is DERIVED from the published store-key.pem and not scraped out of a
	# log line: ngc-store prints the key only on the run that CREATES the key
	# file (measured 24.10.2026 - the second run prints one summary line), so a
	# scrape would come back empty on every later run and leave the answer
	# silently blank. The PRIVATE key never leaves the store machine.
	pub=$(auf "$STORE_IP" "openssl pkey -pubin -in /var/www/store/store-key.pem -outform DER | tail -c 32 | base64 | tr '+/' '-_' | tr -d '='")
	[ ${#pub} -eq 43 ] ||
		fehler "the store key came back as ${#pub} characters, not 43 - it is not an ed25519 key in base64url"
	printf '%s' "$pub" >"$ARBEIT/store-key.txt"
	printf 'store: catalog served at https://store.%s/catalog.json (seq %s)\n' "$ZONE" "$(auf "$STORE_IP" "cat /root/store/seq")"
	printf 'store: public key for the store_key answer: %s\n' "$pub"
	echo "store: ok"
}

# ------------------------------------------------------------------ db
db() {
	# The two passwords are made HERE, on the machine that needs them, out of
	# the kernel's own randomness, and they go into a 0600 file. No comma: ulh
	# joins the answers with commas.
	auf "$DB_IP" "[ -s $ANTWORTEN ] || (
		install -m 0600 /dev/null $ANTWORTEN &&
		{
			printf 'export NGC_HOSTNAME=db-1.%s\n' '$ZONE'
			printf 'export NGC_NODES=db-1.%s:3306\n' '$ZONE'
			printf 'export NGC_BOOTSTRAP=yes\n'
			printf 'export NGC_PASSWORD_NGC=%s\n' \"\$(openssl rand 24 | base64 | tr -d '=+/,')\"
			printf 'export NGC_PASSWORD_MIGRATE=%s\n' \"\$(openssl rand 24 | base64 | tr -d '=+/,')\"
			printf 'export NGC_CA_DIR=/etc/ngc/db-ca\n'
		} >> $ANTWORTEN
	)" || fehler "could not write the answers on db-1"
	# A NAMED PIECE OF GROUNDWORK, AND IT IS A FINDING OF THIS RUN.
	#
	# ngc-db.sh install dies on a FRESH Debian at its very first step:
	#   "NextGenCloud: MariaDB with Galera for db-1.ngc.mein.lan"
	#   "chown: invalid group: 'root:mysql'"
	# prepare() runs "ensure_dir $NGC_TLS root:mysql 0710" and
	# "ensure_dir $NGC_KEYS root:mysql 0710", and install_ngc_db() calls
	# prepare BEFORE ensure_packages - so the group mysql does not exist yet.
	# (Measured 24.10.2026 on 10.2.3.212 against ulh origin/ngc-kette afb2ea4.
	# The line three statements further down, "MariaDB has to be there before
	# the keys: they belong to its user", shows the ordering was seen for the
	# keys and missed for the directories.)
	#
	# So the package goes on first, by hand, and every number that comes out of
	# this path carries that sentence with it: on a machine where nobody did
	# this, the installation does not start at all.
	auf "$DB_IP" "dpkg-query -W -f='\${Status}' mariadb-server 2>/dev/null | grep -q 'install ok installed' ||
		DEBIAN_FRONTEND=noninteractive apt-get install -y -qq mariadb-server galera-4 mariadb-backup >/dev/null" ||
		fehler "db-1: mariadb-server could not be installed"
	auf "$DB_IP" "set -a; . $ANTWORTEN; set +a; bash /root/ulh/apps/cli/assets/scripts/ngc-db.sh install" ||
		fehler "ngc-db install failed on db-1 - look at the output above"
	# /etc/ngc/db-tls and NOT /etc/ngc/tls: ngc-db.sh keeps its own directory
	# (NGC_TLS="/etc/ngc/db-tls" in the script), while ngc-api and ngc-web use
	# /etc/ngc/tls for theirs. Looking in the wrong one is a run that reports
	# "the database has no CA" about a database that just printed it.
	auf "$DB_IP" "test -s /etc/ngc/db-tls/ca.crt" || fehler "db-1 has no /etc/ngc/db-tls/ca.crt"
	auf "$DB_IP" "cat /etc/ngc/db-tls/ca.crt" >"$ARBEIT/db-ca.crt"
	printf 'db: ok - the CA of the installation is in %s\n' "$ARBEIT/db-ca.crt"
}

# ------------------------------------------------------------------ api1
# The first API: join_code empty. It makes the shared keys of the installation
# (secretKey and challengeKey), its own Noise keys and a self-signed TLS
# certificate, and it prints the setup code of the first admin and the pin of
# its TLS key - the two things web and api2 need afterwards.
api() {
	local welche=$1 ip node antwort
	case $welche in
	1)
		ip=$API1_IP
		node=api-1
		;;
	2)
		ip=$API2_IP
		node=api-2
		;;
	*) fehler "api takes 1 or 2" ;;
	esac
	[ -s "$ARBEIT/db-ca.crt" ] || fehler "no $ARBEIT/db-ca.crt - run 'db' first"
	[ -s "$ARBEIT/store-key.txt" ] || fehler "no store key - run 'store' first"
	# The CA of the database as a FILE on the machine, which is what the
	# answer db_ca names.
	datei_hin "$ip" /etc/ngc/db-ca.crt 0644 <"$ARBEIT/db-ca.crt"
	antwort=$ANTWORTEN
	# THE PASSWORD. It is read out of db-1's 0600 answers file and piped into
	# this machine's 0600 answers file. It never stands in an argument (every
	# local user can read /proc/<pid>/cmdline without hidepid) and this script
	# never prints it.
	{
		printf 'export NGC_NODE=%s\n' "$node"
		printf 'export NGC_HOSTNAME=%s.%s\n' "$node" "$ZONE"
		printf 'export NGC_DB_DRIVER=mariadb\n'
		printf 'export NGC_DB_NODES=db-1.%s:3306\n' "$ZONE"
		printf 'export NGC_DB_NAME=ngc\n'
		printf 'export NGC_DB_USER=ngc\n'
		auf "$DB_IP" "grep '^export NGC_PASSWORD_NGC=' $ANTWORTEN" |
			sed 's/NGC_PASSWORD_NGC=/NGC_DB_PASSWORD=/'
		printf 'export NGC_DB_CA=/etc/ngc/db-ca.crt\n'
		printf 'export NGC_STORE_URL=https://store.%s\n' "$ZONE"
		printf 'export NGC_STORE_KEY=%s\n' "$(cat "$ARBEIT/store-key.txt")"
		if [ "$welche" = 2 ]; then
			[ -s "$ARBEIT/join-code.txt" ] ||
				fehler "no join code - it comes out of the first API (api1) and has to be in $ARBEIT/join-code.txt"
			printf 'export NGC_JOIN_CODE=%s\n' "$(cat "$ARBEIT/join-code.txt")"
		else
			printf 'export NGC_JOIN_CODE=\n'
		fi
	} | datei_hin "$ip" "$antwort" 0600
	auf "$ip" "set -a; . $antwort; set +a; bash /root/ulh/apps/cli/assets/scripts/ngc-api.sh install" ||
		fehler "ngc-api install failed on $node - look at the output above"
	printf 'api%s: ok\n' "$welche"
}

# ------------------------------------------------------------------ web
# BOTH apis and BOTH pins: that is what makes the balancer in Apache a real
# balancer and web.json hold real pins. With one API neither is measured.
web() {
	local pin1 pin2 adressen pins
	# The pin is the SHA-256 of the public key of the certificate, base64url -
	# the same number shared/testdata/tls/pin.sh computes and ngc-api prints at
	# the end of its install. There is no pin.sh in the ulh checkout (measured
	# 24.10.2026 on origin/ngc-kette), so it is computed here from the
	# certificate the API made itself.
	pin1=$(auf "$API1_IP" "openssl x509 -in /etc/ngc/tls/api.crt -pubkey -noout 2>/dev/null | openssl pkey -pubin -outform DER 2>/dev/null | openssl dgst -sha256 -binary | base64 | tr '+/' '-_' | tr -d '='")
	[ ${#pin1} -eq 43 ] || fehler "the pin of api-1 came back as '${pin1:-}' - that is no SHA-256 in base64url"
	adressen="https://api-1.$ZONE:7443"
	pins=$pin1
	# api-2 only if it is really installed. A pin typed in for a machine that
	# is not there would make Apache's balancer point at nothing, and web.json
	# would carry a number nobody can check.
	if auf "$API2_IP" "test -s /etc/ngc/tls/api.crt" 2>/dev/null; then
		pin2=$(auf "$API2_IP" "openssl x509 -in /etc/ngc/tls/api.crt -pubkey -noout 2>/dev/null | openssl pkey -pubin -outform DER 2>/dev/null | openssl dgst -sha256 -binary | base64 | tr '+/' '-_' | tr -d '='")
		[ ${#pin2} -eq 43 ] || fehler "the pin of api-2 came back as '${pin2:-}'"
		adressen="$adressen https://api-2.$ZONE:7443"
		pins="$pins $pin2"
	else
		echo "web: api-2 carries no certificate yet, so the balancer gets ONE member - the second API joins with a join code, and that comes from a signed-in admin in the browser"
	fi
	{
		printf 'export NGC_HOSTNAME=%s\n' "$ZONE"
		printf 'export NGC_CERT_MODE=selfsigned\n'
		printf 'export NGC_CERT_FILE=\n'
		printf 'export NGC_KEY_FILE=\n'
		# QUOTED, and that is not cosmetic: both answers are LISTS separated by
		# spaces, and an unquoted "export NGC_APIS=a b" makes the shell read
		# "b" as a second assignment - "export: `https://api-2...': not a
		# valid identifier". Measured 24.10.2026: the second API fell out of
		# the balancer that way while api-trust.pem already held both
		# certificates, so nothing looked wrong except one BalancerMember.
		printf 'export NGC_APIS="%s"\n' "$adressen"
		printf 'export NGC_API_PINS="%s"\n' "$pins"
	} | datei_hin "$WEB_IP" /root/ngc-web.env 0600
	auf "$WEB_IP" "set -a; . /root/ngc-web.env; set +a; bash /root/ulh/apps/cli/assets/scripts/ngc-web.sh install" ||
		fehler "ngc-web install failed - look at the output above"
	echo "web: ok"
}

# ------------------------------------------------------------------ controller
# ngc-controller.sh pairs WHILE it installs, and the pairing code comes out of
# the browser: "Benutzer und Server" -> Server -> Kopplungscode. A code is
# good ONCE (dbapi.OncePair) and for 24 hours (tunnel.PairLifetime = 86400),
# so every server needs its own.
#
# The code goes in through the STANDARD INPUT of this script and from there
# into a 0600 file on the machine. It never stands in an argument:
# /proc/<pid>/cmdline is readable by every local user without hidepid, and
# the script of ulh keeps the same promise where it hands the code to
# "ngc-controller pair".
#
# Usage:  printf '%s' 'ngc-pair:...' | proxmox-sechs-ngc.sh controller svc-1
controller() {
	local name=$1 ip rollen code
	case $name in
	web-api-1)
		ip=$API1_IP
		rollen="web api"
		;;
	api-2)
		ip=$API2_IP
		rollen="api"
		;;
	db-1)
		ip=$DB_IP
		rollen="db"
		;;
	svc-1)
		ip=$SVC1_IP
		rollen="services"
		;;
	svc-2)
		ip=$SVC2_IP
		rollen="services"
		;;
	*) fehler "controller takes one of: web-api-1 api-2 db-1 svc-1 svc-2" ;;
	esac
	code=$(cat)
	case $code in
	ngc-pair:*) ;;
	*) fehler "that is no pairing code - it begins with 'ngc-pair:' (a join code, 'ngc-join:', belongs to ngc-api and not here)" ;;
	esac
	{
		printf 'export NGC_NAME=%s\n' "$name"
		printf 'export NGC_PAIR_CODE=%s\n' "$code"
		printf 'export NGC_ROLES=%s\n' "$rollen"
		if [ "$name" = db-1 ]; then
			# Role db carries the migration in controller.json, and that is
			# the account which may change the schema - a different one from
			# the db_user of api.json, which may not (measured: "CREATE
			# command denied to user 'ngc'").
			printf 'export NGC_MIGRATE_NODES=db-1.%s:3306\n' "$ZONE"
			printf 'export NGC_MIGRATE_NAME=ngc\n'
			printf 'export NGC_MIGRATE_USER=ngc_migrate\n'
			auf "$DB_IP" "grep '^export NGC_PASSWORD_MIGRATE=' $ANTWORTEN" |
				sed 's/NGC_PASSWORD_MIGRATE=/NGC_MIGRATE_PASSWORD=/'
			printf 'export NGC_MIGRATE_CA=/etc/ngc/db-ca.crt\n'
		fi
	} | datei_hin "$ip" /root/ngc-controller.env 0600
	if [ "$name" = db-1 ] || [ "$name" = web-api-1 ] || [ "$name" = api-2 ]; then
		[ -s "$ARBEIT/db-ca.crt" ] || fehler "no $ARBEIT/db-ca.crt - run 'db' first"
		datei_hin "$ip" /etc/ngc/db-ca.crt 0644 <"$ARBEIT/db-ca.crt"
	fi
	auf "$ip" "set -a; . /root/ngc-controller.env; set +a; bash /root/ulh/apps/cli/assets/scripts/ngc-controller.sh install; rc=\$?; shred -u /root/ngc-controller.env 2>/dev/null || rm -f /root/ngc-controller.env; exit \$rc" ||
		fehler "ngc-controller install failed on $name - look at the output above"
	printf 'controller: %s is paired (roles: %s)\n' "$name" "$rollen"
}

case ${1:-} in
laden) laden ;;
controller) controller "${2:-}" ;;
ulh) ulh ;;
store) store ;;
db) db ;;
api1) api 1 ;;
api2) api 2 ;;
web) web ;;
*)
	sed -n '2,50p' "${BASH_SOURCE[0]}"
	exit 2
	;;
esac
