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
#   4. the data directories of the apps. On the machine they are encrypted
#      with a key derived from dbKey (measured: the key identifier the kernel
#      holds for every app directory on svc-1 is the one HKDF over that dbKey
#      gives) - but the service is running while this reads them, so the
#      kernel hands them over in the CLEAR and this tar is plaintext. It is
#      the one part of the backup that needs no key, and therefore the one
#      that has to be locked away hardest.
#   5. a manifest with a sha256 of every file.
#
# What it does NOT do is play anything back, and it needs no program for most
# of that: a controller opens whatever lies at the path in its
# controller.json. The one step the product itself does not have is
# tools/sicherung-zurueck (the system user of an app and its encrypted data
# directory, which only an activation makes). The order of the steps, and
# what came back and what did not when they were walked on a fresh machine,
# is in the document named above.
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
#
# The tar is written ON the machine first and fetched afterwards, and not
# streamed straight out. It costs the room of one more copy under /var/tmp for
# as long as the fetch takes, and it buys the only thing that makes a transfer
# checkable at all: a file on both ends with a sha256 of its own. A stream has
# no sum, and tarring a live directory twice to get one would give two
# different tars.
her() {
	local ziel=$1 pfad=$2 aus=$3 name
	name=$(basename -- "$aus")
	fern "$ziel" "tar -C / --numeric-owner -cf $PROBE/$name '${pfad#/}' 2>&1 | grep -v 'socket ignored' || true"
	fern "$ziel" cat "$PROBE/$name" >"$aus"
	gleich "$ziel" "$PROBE/$name" "$aus" || return 1
	fern "$ziel" rm -f "$PROBE/$name"
}

# gleich holds the file that just arrived against the machine it came from, and
# it is not decoration.
#
# EVERYTHING here comes over ssh's stdout as binary, and that is a path with a
# known way of going wrong: a session of 24.10.2026 took a logical dump of the
# MariaDB the same way and got a 47 KB file that looked like a backup and held
# no table and no user - the bytes were bent on the way into a file on Windows.
# It only showed because somebody OPENED the dump instead of looking at its
# size.
#
# So this script does not trust its own transfer. The sha256 is taken ON the
# machine, before the bytes travel, and held against the file that arrived. A
# mismatch is not a warning: the file is deleted, because a backup that is
# quietly wrong is worse than one that is missing, and the run ends with a
# gap it names.
gleich() {
	local ziel=$1 pfad=$2 aus=$3 dort hier
	dort=$(fern "$ziel" sha256sum "$pfad" | cut -d' ' -f1)
	hier=$(sha256sum "$aus" | cut -d' ' -f1)
	if [ "$dort" != "$hier" ]; then
		rm -f "$aus"
		echo "sicherung: $(basename -- "$aus") kam verbogen an und wurde geloescht" >&2
		echo "sicherung:   auf der Maschine $dort, hier $hier" >&2
		return 1
	fi
	return 0
}

# fehlt records a part that did not make it, in the manifest and in the exit
# code. Nothing here stops the run: the other parts of this machine and the
# other machines are worth having, and a backup with one named gap is a
# different thing from a backup that is silently short.
fehlt() {
	notiz "$1  FEHLT - kam verbogen an, siehe die Zeilen auf dem Schirm."
	fehler=1
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
	# The one directory this makes on the machine, before anything is read:
	# every part is written there first so it has a sha256 on both ends
	# (gleich). It is removed again at the end of this machine.
	fern "$ziel" mkdir -p "$PROBE"
	notiz "Sicherung von $name ($ziel), $STEMPEL"
	notiz ""

	# 1. /etc/ngc. First, because without it every other part of this folder
	#    is bytes nobody opens again - and because a machine that has none is
	#    no NGC machine and the rest would be a quiet empty folder.
	if fern "$ziel" test -d /etc/ngc; then
		her "$ziel" /etc/ngc "$ordner/etc-ngc.tar" || fehlt "etc-ngc.tar"
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
		fern "$ziel" "/usr/lib/ngc/ngc-controller backup $PROBE/state.db >/dev/null"
		fern "$ziel" cat "$PROBE/state.db" >"$ordner/state.db"
		gleich "$ziel" "$PROBE/state.db" "$ordner/state.db" || fehlt "state.db"
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
		her "$ziel" "$PROBE/mariadb" "$ordner/mariadb.tar" || fehlt "mariadb.tar"
		fern "$ziel" cat "$PROBE/mariabackup.log" >"$ordner/mariabackup.log"
		notiz "mariadb.tar          die Benutzerdaten der Anlage (ngc_user, ngc_blob, ngc_session, ngc_log)."
		notiz "                     Die Tabellen sind verschluesselt; der Schluessel liegt in etc-ngc.tar"
		notiz "                     unter db-keys und NICHT hier drin (gemessen: der Benutzername steht in"
		notiz "                     keiner .ibd dieser Sicherung)."
		notiz "                     Noch nicht zurueckgespielt, auf keiner Maschine: siehe das Dokument."
	else
		notiz "mariadb.tar          keine laufende MariaDB auf diesem Rechner."
	fi

	# 4. the data directories of the apps. THIS PART IS PLAINTEXT, and the
	#    first version of this script said the opposite - measured and
	#    corrected: the service is running while this is read, so the key of
	#    every app is in the kernel's keyring and root reads the directories
	#    THROUGH it. tar therefore carries readable names AND readable
	#    contents. Counted in the tar of svc-1: 1419 ELF headers and the
	#    dpkg lists of the container, plain to read.
	#
	#    So this one file is the most sensitive thing in the whole backup: it
	#    is the only part that opens without any key at all. The manifest says
	#    it in those words, because the opposite belief is the dangerous one.
	#
	#    The other side of the same fact is the good news of the restore: the
	#    app data are NOT lost with the dbKey as long as this tar exists, and
	#    putting them back needs no key either - measured, both apps' data came
	#    back onto a fresh machine out of this tar.
	if fern "$ziel" test -d /var/lib/ngc/apps; then
		her "$ziel" /var/lib/ngc/apps "$ordner/app-daten.tar" || fehlt "app-daten.tar"
		notiz "app-daten.tar        die Datenordner der Apps - UNVERSCHLUESSELT, im Klartext."
		notiz "                     Auf der Maschine sind sie verschluesselt (fscrypt, Schluessel aus"
		notiz "                     dbKey), aber der Dienst lief beim Sichern, also hat der Kern sie"
		notiz "                     entschluesselt vorgelegt. Diese Datei oeffnet JEDER, ohne Schluessel."
		notiz "                     Sie ist das Empfindlichste in dieser Sicherung - und zugleich der"
		notiz "                     Grund, dass die App-Daten einen Verlust des dbKey ueberleben."
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
in diesem Ordner. Ohne ihn oeffnet weder state.db noch die MariaDB, von
niemandem, auch nicht von NextGenCloud.

DIE EINE AUSNAHME, UND SIE IST WICHTIG: app-daten.tar ist KEIN Geheimtext. Auf
der Maschine sind die Datenordner der Apps verschluesselt, aber beim Sichern
lief der Dienst, also hat der Kern sie entschluesselt vorgelegt. Diese Datei
oeffnet jeder, ohne Schluessel. Sie gehoert damit genauso weggeschlossen wie
etc-ngc.tar - und sie ist der Grund, dass die Daten der Apps einen Verlust des
dbKey ueberleben.

DARAUS FOLGT FUER DIE AUFBEWAHRUNG: etc-ngc.tar gehoert an einen anderen Ort
als der Rest - wer beides zusammen hat, hat die Anlage. Und jeder Rechner hat
seinen EIGENEN dbKey (vier verschiedene auf dieser Anlage, gemessen): es gibt
keinen einen Schluessel der Anlage, den man einmal wegschliesst.

Der Weg zurueck ist in docs/sicherung-und-weg-zurueck.md beschrieben, Schritt
fuer Schritt, und er ist einmal gegangen: auf einer frischen Maschine, die
dieser Anlage nie gehoert hat, lief eine App aus dieser Sicherung wieder.
Dort steht auch, was dabei NICHT zurueckkam.
EOF

if [ "$fehler" -ne 0 ]; then
	echo "sicherung: mit Luecken - siehe die Zeilen oben." >&2
	exit 1
fi
echo "sicherung: ok"
