#!/usr/bin/env bash
# sicherung - collect everything of an installation that NOTHING can make
# again, one directory per machine, from a workstation that reaches the
# machines by ssh.
#
# WHY THIS FILE EXISTS AND WHAT IT IS NOT. "ngc-controller backup <Datei>"
# copies one thing: the controller's own SQLite, encrypted, with the key
# nowhere in it. That file alone is worth nothing - measured, not reasoned:
# docs/sicherung-und-weg-zurueck.md opens the same copy on a foreign machine
# with the key and with one bit of the key changed, and the second answer is
# "sqlite3: file is not a database". And the user data of a distributed
# installation are not in that file at all: they lie in the MariaDB of the
# database machines, and the two places in this repository that send an admin
# to "ngc-db backup" for them name a program that does not exist (there is no
# apps/server-db/main.go, and no ngc-db on any of the six machines).
#
# So this collects five kinds of thing, and the manifest says of every one of
# them which it is:
#   1. /etc/ngc of the machine - controller.json (dbKey, noiseKey, anchor),
#      api.json (signKey, storeKey, secretKey, the database password),
#      db-keys (the key of the encrypted MariaDB tablespaces), tls.
#      THIS IS THE PART NOTHING CAN MAKE AGAIN. Everything else in a backup
#      is only readable with it.
#   2. the controller's own database, through the command that already exists,
#      while the controller keeps running.
#   3. the MariaDB, with mariabackup, where one runs.
#   4. the encrypted data directories of the apps, as the ciphertext they are
#      on the disk. Their key is derived from dbKey of the same machine
#      (measured: the key identifier the kernel holds for every app directory
#      on svc-1 is the one HKDF over that dbKey gives), so part 1 of THIS
#      machine is what opens them, and no part 1 of any other.
#   5. a manifest with a sha256 of every file.
#
# What it does NOT do is play anything back. The way back needs no new
# program - a controller opens whatever lies at the path in its
# controller.json, and that was measured too - but it needs an order of steps
# and a machine to be measured on, and both are in the document named above.
#
# Usage:
#   sicherung.sh <ziel-ordner> <benutzer@rechner> [<benutzer@rechner> ...]
#
# The proof of a complete run is the printed line "sicherung: ok", never the
# exit code alone (tools/erstlauf.sh is held to the same rule and says why).
set -euo pipefail

if [ -z "${BASH_VERSION:-}" ]; then
	echo "sicherung: run with bash, not sh" >&2
	exit 2
fi

SSH_OPTS=${NGC_SSH_OPTS:--o StrictHostKeyChecking=accept-new -o ConnectTimeout=10}
SSH_KEY=${NGC_SSH_KEY:-}

usage() {
	cat >&2 <<'EOF'
Aufruf: sicherung.sh <ziel-ordner> <benutzer@rechner> [<benutzer@rechner> ...]

Der Ziel-Ordner wird angelegt und darf noch nicht da sein: eine Sicherung, die
in eine vorhandene hineinschreibt, ist zwei halbe Staende.

Umgebung:
  NGC_SSH_KEY    Schluesseldatei fuer ssh
  NGC_SSH_OPTS   weitere ssh-Angaben
EOF
}

# fern runs one command on a machine. The argument list is passed to ssh as a
# list and never built into a string: a name of this installation must not be
# able to become a second command.
fern() {
	local ziel=$1
	shift
	if [ -n "$SSH_KEY" ]; then
		# shellcheck disable=SC2086
		ssh -i "$SSH_KEY" $SSH_OPTS "$ziel" "$@"
	else
		# shellcheck disable=SC2086
		ssh $SSH_OPTS "$ziel" "$@"
	fi
}

# her fetches a path as a tar stream. tar and not scp, because what is fetched
# are directories whose modes and owners are part of what they are: db-keys is
# 0710 root:mysql and keyfile.key is 0400 mysql, and a copy that loses that is
# a copy a restore cannot put back.
her() {
	local ziel=$1 pfad=$2 aus=$3
	fern "$ziel" tar -C / --numeric-owner -cf - "${pfad#/}" >"$aus"
}

# notiz appends one line to the manifest of a machine.
notiz() {
	printf '%s\n' "$1" >>"$MANIFEST"
}

if [ $# -lt 2 ]; then
	usage
	exit 2
fi

ZIEL=$1
shift
if [ -e "$ZIEL" ]; then
	echo "sicherung: $ZIEL ist schon da - bitte einen neuen Ordner nennen." >&2
	exit 2
fi
mkdir -p "$ZIEL"
STEMPEL=$(date -u +%Y-%m-%dT%H:%M:%SZ)

# The one path this writes on the machines themselves, and it is removed again
# at the end of every machine: ngc-controller backup writes a file, and it has
# to write it where the machine can reach it.
PROBE=/var/tmp/ngc-sicherung-lauf

fehler=0
for ziel in "$@"; do
	name=$(fern "$ziel" hostname -s)
	echo "sicherung: $ziel ($name)"
	ordner="$ZIEL/$name"
	mkdir -p "$ordner"
	MANIFEST="$ordner/inhalt.txt"
	notiz "Sicherung von $name ($ziel), $STEMPEL"
	notiz ""

	# 1. /etc/ngc. First, because without it every other part of this folder
	#    is bytes nobody opens again - and because a machine that has none is
	#    no NGC machine and the rest would be a quiet empty folder.
	if fern "$ziel" test -d /etc/ngc; then
		her "$ziel" /etc/ngc "$ordner/etc-ngc.tar"
		notiz "etc-ngc.tar          DIE SCHLUESSEL. Ohne diese Datei ist alles andere hier wertlos."
		notiz "                     controller.json: dbKey (oeffnet state.db UND die App-Datenordner),"
		notiz "                     noiseKey (die Kennung dieses Rechners), anchor (Stand der Vertrauensliste)."
		notiz "                     api.json: signKey, storeKey, secretKey, das Datenbank-Kennwort."
		notiz "                     db-keys: der Schluessel der verschluesselten MariaDB-Tabellen."
	else
		echo "sicherung: $name hat kein /etc/ngc - uebersprungen." >&2
		notiz "etc-ngc.tar          FEHLT: dieser Rechner hat kein /etc/ngc."
		fehler=1
		continue
	fi

	# 2. the controller's own database, through the command that exists. The
	#    controller keeps running; that is the one sentence its help gives and
	#    it was measured again on all three controllers of the installation.
	if fern "$ziel" test -x /usr/lib/ngc/ngc-controller; then
		fern "$ziel" "mkdir -p $PROBE && /usr/lib/ngc/ngc-controller backup $PROBE/state.db >/dev/null"
		fern "$ziel" cat "$PROBE/state.db" >"$ordner/state.db"
		notiz "state.db             die eigene Datenbank des Controllers: Vertrauensliste, die Zeilen der"
		notiz "                     Apps mit ihren Manifesten, die Lizenzen, gelegte Aktualisierungen."
		notiz "                     Verschluesselt mit dbKey aus controller.json - nur damit zu oeffnen."
	else
		notiz "state.db             kein Controller auf diesem Rechner."
	fi

	# 3. the MariaDB. mariabackup and no dump: a dump played back may run
	#    code, and this installation deliberately writes none (register.go
	#    says so at the place that refuses to make one).
	if fern "$ziel" 'command -v mariabackup >/dev/null && systemctl is-active --quiet mariadb'; then
		fern "$ziel" "rm -rf $PROBE/mariadb && mkdir -p $PROBE/mariadb && mariabackup --backup --target-dir=$PROBE/mariadb >$PROBE/mariabackup.log 2>&1"
		her "$ziel" "$PROBE/mariadb" "$ordner/mariadb.tar"
		fern "$ziel" cat "$PROBE/mariabackup.log" >"$ordner/mariabackup.log"
		notiz "mariadb.tar          die Benutzerdaten der Anlage (ngc_user, ngc_blob, ngc_session, ngc_log)."
		notiz "                     Die Tabellen sind verschluesselt; der Schluessel liegt in etc-ngc.tar"
		notiz "                     unter db-keys und NICHT hier drin (gemessen: der Benutzername steht in"
		notiz "                     keiner .ibd dieser Sicherung)."
		notiz "                     Noch nicht zurueckgespielt, auf keiner Maschine: siehe das Dokument."
	else
		notiz "mariadb.tar          keine laufende MariaDB auf diesem Rechner."
	fi

	# 4. the data directories of the apps, as ciphertext. The service is
	#    running while this is read, so root sees the plaintext NAMES through
	#    the kernel's keyring - tar therefore carries the readable names and
	#    the encrypted contents. That is the honest limit of this part and the
	#    manifest says it: it is a copy that only this machine's dbKey opens,
	#    and whether an fscrypt directory can be put back from a tar at all is
	#    the open question of the document, not a claim of this script.
	if fern "$ziel" test -d /var/lib/ngc/apps; then
		her "$ziel" /var/lib/ngc/apps "$ordner/app-daten.tar"
		notiz "app-daten.tar        die Datenordner der Apps. Ihr Schluessel wird aus dbKey dieses"
		notiz "                     Rechners abgeleitet (HKDF, App-Kennung als Salz) - der dbKey eines"
		notiz "                     anderen Rechners oeffnet sie nicht."
	else
		notiz "app-daten.tar        keine App-Datenordner auf diesem Rechner."
	fi

	fern "$ziel" rm -rf "$PROBE"

	# 5. the sha256 of everything, so a later run can be held against this
	#    one and a half-written file is visible as one.
	notiz ""
	notiz "sha256:"
	(cd "$ordner" && for f in *; do
		[ "$f" = inhalt.txt ] && continue
		printf '  %s\n' "$(sha256sum "$f")"
	done) >>"$MANIFEST"
done

cat >"$ZIEL/LIESMICH.txt" <<EOF
Sicherung einer NextGenCloud-Anlage, $STEMPEL

Je Rechner ein Ordner, darin inhalt.txt mit der Liste und den Pruefsummen.

DAS WICHTIGSTE IN EINEM SATZ: etc-ngc.tar ist der Schluessel zu allem anderen
in diesem Ordner. Ohne ihn oeffnet weder state.db noch die MariaDB noch ein
App-Datenordner, von niemandem, auch nicht von NextGenCloud.

DARAUS FOLGT FUER DIE AUFBEWAHRUNG: etc-ngc.tar gehoert an einen anderen Ort
als der Rest - wer beides zusammen hat, hat die Anlage. Und jeder Rechner hat
seinen EIGENEN dbKey (vier verschiedene auf dieser Anlage, gemessen): es gibt
keinen einen Schluessel der Anlage, den man einmal wegschliesst.

Der Weg zurueck ist in docs/sicherung-und-weg-zurueck.md beschrieben und dort
steht auch, welcher Schritt davon noch auf keiner Maschine gelaufen ist.
EOF

if [ "$fehler" -ne 0 ]; then
	echo "sicherung: mit Luecken - siehe die Zeilen oben." >&2
	exit 1
fi
echo "sicherung: ok"
