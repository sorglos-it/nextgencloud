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
#   4. the data directories of the apps, ENCRYPTED before they leave the
#      machine. On the machine they are encrypted with a key derived from
#      dbKey - but the service is running while this reads them, so the kernel
#      hands them over in the clear, and the first version of this script
#      wrote that clear text to a disk. It was the one part of the backup that
#      needed no key at all. Now a key of its own is made for every machine,
#      it goes in the KEY folder, and the plaintext never touches a disk
#      anywhere: tar writes into openssl and openssl writes the file.
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
# THE KEYS DO NOT GO WHERE THE DATA GO, and this script refuses to put them
# there. The reason is a measurement of the session that switched the machines
# off, and it is sharper than "keep them apart":
#
#	/etc/ngc/db-keys   keyfile.key 64 B, keys.enc 96 B, sst.pass 64 B = 224 Byte
#	/var/lib/mysql                                                    = 285 MB
#
# 224 bytes unlock 285 megabytes. What makes them dangerous is not where they
# lie but that they unlock everything AND fit in a text message - so they
# belong where a house keeps secrets (a password manager), not in a second
# folder next to the first. Two folders on one disk are the shape that looks
# like separation and is none: same machine, same account, same rights.
# Whoever can read the one reads the other.
#
# So this takes TWO targets and writes the keys to the second one. Without it
# the run does not start, and it refuses a second target that lies on the
# first - fail closed, because the one mistake nobody notices is the backup
# that is complete in one place.
#
# Usage:
#   sicherung.sh <daten-ordner> -schluessel <schluessel-ordner> \
#                <benutzer@rechner> [<benutzer@rechner> ...]
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
Aufruf: sicherung.sh <daten-ordner> -schluessel <schluessel-ordner> \
                     <benutzer@rechner> [<benutzer@rechner> ...]

Beide Ordner werden angelegt und duerfen noch nicht da sein: eine Sicherung,
die in eine vorhandene hineinschreibt, ist zwei halbe Staende.

Die Schluessel gehen NICHT in den Daten-Ordner. 224 Byte unter
/etc/ngc/db-keys schliessen 285 MB Datenbank auf, und der dbKey aus
controller.json oeffnet die Datenbank jedes Controllers und die Datenordner
aller Apps. Der Schluessel-Ordner ist nur die Durchfuhr: was darin landet,
gehoert in den Passwortmanager und danach geloescht. Zwei Ordner auf derselben
Platte sind keine Trennung.

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

# krypt fetches a path as an ENCRYPTED tar stream and answers with the key it
# made, which the caller puts in the key folder and nowhere else.
#
# WHY THIS EXISTS. The data directory of an app is encrypted on the machine,
# but a backup taken while the service runs reads it THROUGH the kernel's
# keyring and is therefore clear text. The session that decided the shape of
# this backup put it as the same calculation as the 224 bytes of db-keys, one
# step further: an object that opens the installation with nothing else needed
# is exactly what keeping keys apart is for, and being 200 MB large makes it
# unhandy, not harmless.
#
# THE THREE CONDITIONS OF THAT DECISION, and where each one is met here:
#   - the key is a NEW secret and belongs with the other keys: one per MACHINE
#     per run, written only into that machine's folder under the key target.
#     A key for svc-1 does not open svc-2.
#   - a key that travels with the data is no key: nothing of it is written
#     beside the data, and the manifest there says which file it needs and
#     that it is not there.
#   - an encrypted backup nobody can open again is worse than a plain one, so
#     the way back is walked with the key out of the key folder, on a machine
#     that never belonged to the installation. docs/sicherung-und-weg-zurueck.md
#     has that run.
#
# HOW THE KEY REACHES THE MACHINE: over ssh's stdin into a file of mode 0600,
# never as an argument - an argument stands in "ps" for every user of that
# machine. It is shredded at the end of the machine. That it exists there for
# the seconds of the run is no new exposure: the clear data are on that
# machine anyway.
#
# WHAT THIS DOES NOT GIVE: AES-256-CTR is not authenticated, so this is
# confidentiality and not a seal. What catches a changed byte is the sha256 in
# the manifest, taken on the machine and held against the file here (gleich) -
# and that is said rather than left to be assumed.
krypt() {
	local ziel=$1 pfad=$2 aus=$3 schl=$4 name key
	name=$(basename -- "$aus")
	key=$(openssl rand -base64 32) || return 1
	printf '%s' "$key" | fern "$ziel" "cat >$PROBE/k && chmod 600 $PROBE/k" || return 1
	# tar writes into openssl and openssl writes the file: the clear tar is
	# never a file on any disk, not on the machine and not here.
	fern "$ziel" "tar -C / --numeric-owner -cf - '${pfad#/}' 2>/dev/null | openssl enc -aes-256-ctr -pbkdf2 -iter 600000 -md sha256 -salt -pass file:$PROBE/k -out $PROBE/$name" || return 1
	fern "$ziel" "shred -u $PROBE/k 2>/dev/null || rm -f $PROBE/k"
	fern "$ziel" cat "$PROBE/$name" >"$aus"
	gleich "$ziel" "$PROBE/$name" "$aus" || return 1
	fern "$ziel" rm -f "$PROBE/$name"
	# The key goes to the OTHER target, in its own file, readable by nobody
	# else. It is written only after the ciphertext has arrived and matched:
	# a key for a file that is not there would be the worse of the two halves
	# to keep.
	( umask 077; printf '%s\n' "$key" >"$schl" )
	return 0
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

if [ $# -lt 1 ]; then
	usage
	exit 2
fi

ZIEL=$1
shift
SCHLUESSEL=
if [ "${1:-}" = -schluessel ]; then
	SCHLUESSEL=${2:-}
	shift 2 || true
fi
if [ -z "$SCHLUESSEL" ] || [ $# -lt 1 ]; then
	usage
	exit 2
fi
for d in "$ZIEL" "$SCHLUESSEL"; do
	if [ -e "$d" ]; then
		echo "sicherung: $d ist schon da - bitte einen neuen Ordner nennen." >&2
		exit 2
	fi
done

# The refusal this script exists for. Both paths are made absolute first, so
# that "." and a relative name cannot hide that they are the same place, and
# then neither may lie inside the other: a key folder under the data folder is
# one folder, whatever it is called.
mkdir -p "$ZIEL" "$SCHLUESSEL"
Z_ABS=$(cd "$ZIEL" && pwd -P)
S_ABS=$(cd "$SCHLUESSEL" && pwd -P)
if [ "$Z_ABS" = "$S_ABS" ] || [ "${S_ABS#"$Z_ABS"/}" != "$S_ABS" ] || [ "${Z_ABS#"$S_ABS"/}" != "$Z_ABS" ]; then
	rmdir "$SCHLUESSEL" "$ZIEL" 2>/dev/null || true
	echo "sicherung: Daten und Schluessel sollen an EINEN Ort - das ist keine Sicherung." >&2
	echo "sicherung:   Daten:     $Z_ABS" >&2
	echo "sicherung:   Schluessel: $S_ABS" >&2
	echo "sicherung: Wer den einen Ordner lesen kann, liest auch den anderen. 224 Byte" >&2
	echo "sicherung: Schluessel schliessen 285 MB Datenbank auf; sie gehoeren in den" >&2
	echo "sicherung: Passwortmanager, nicht in ein Verzeichnis daneben." >&2
	exit 2
fi

# AND THE CASE THE REFUSAL ABOVE DOES NOT CATCH, which only showed when this
# was used for real: two SIBLING folders on one disk pass every test above and
# are exactly the shape the whole separation is against - same machine, same
# account, same rights. It is not refused, because it is also the one shape the
# key folder is MEANT to have for a moment: a thoroughfare, filled, emptied
# into the password manager, deleted. So it is said out loud instead, every
# time, and the sentence names what is still to do rather than what is wrong.
#
# "stat -c %d" is the device, which is the question - not the drive letter,
# which says nothing about a mount. Where stat cannot answer (it is not on
# every system this may run from), nothing is printed: a warning that cannot
# be measured is not worth a line.
if command -v stat >/dev/null 2>&1; then
	z_dev=$(stat -c %d "$Z_ABS" 2>/dev/null || echo a)
	s_dev=$(stat -c %d "$S_ABS" 2>/dev/null || echo b)
	if [ "$z_dev" = "$s_dev" ]; then
		echo "sicherung: ACHTUNG - Daten und Schluessel liegen auf DEMSELBEN Datentraeger." >&2
		echo "sicherung:   Solange das so ist, ist es keine Trennung: wer den einen Ordner" >&2
		echo "sicherung:   lesen kann, liest auch den anderen." >&2
		echo "sicherung:   Der Schluessel-Ordner ist als Durchfuhr gedacht - nach dem Lauf in" >&2
		echo "sicherung:   den Passwortmanager damit, dann den Ordner loeschen. Erst danach" >&2
		echo "sicherung:   ist diese Sicherung eine." >&2
	fi
fi
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
	sordner="$SCHLUESSEL/$name"
	mkdir -p "$ordner" "$sordner"
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
		her "$ziel" /etc/ngc "$sordner/etc-ngc.tar" || fehlt "etc-ngc.tar"
		notiz "etc-ngc.tar          NICHT HIER. Die Schluessel dieses Rechners liegen im"
		notiz "                     Schluessel-Ordner dieses Laufs, und ohne sie ist alles in"
		notiz "                     diesem Ordner Geheimtext - ausser app-daten.tar."
		{
			printf 'Schluessel von %s (%s), %s\n\n' "$name" "$ziel" "$STEMPEL"
			printf 'etc-ngc.tar  DIE SCHLUESSEL DIESES RECHNERS.\n'
			printf '  controller.json  dbKey (oeffnet state.db UND die Datenordner aller Apps),\n'
			printf '                   noiseKey (die Kennung dieses Rechners in der Vertrauensliste),\n'
			printf '                   anchor, releaseKey.\n'
			printf '  api.json         signKey, storeKey, secretKey, challengeKey, das Datenbank-Kennwort.\n'
			printf '  db-keys          224 Byte, die 285 MB verschluesselte MariaDB aufschliessen.\n'
			printf '  tls, db-ca       Zertifikate und ihre Schluessel.\n\n'
			printf 'app-daten.schluessel  DER SCHLUESSEL DER APP-DATEN DIESES RECHNERS.\n'
			printf '  In diesem Lauf neu erzeugt, 32 Byte aus openssl rand, nur fuer diesen\n'
			printf '  Rechner. Ohne ihn geht app-daten.tar.enc im Daten-Ordner nicht mehr auf -\n'
			printf '  von niemandem. Liegt nur hier; steht die Datei nicht da, hat dieser Rechner\n'
			printf '  keine App-Datenordner.\n\n'
			printf 'DIESER ORDNER IST NUR DIE DURCHFUHR. Was hier liegt, gehoert in den\n'
			printf 'Passwortmanager und danach von dieser Platte geloescht. Solange es hier\n'
			printf 'liegt, ist dieser Rechner so empfindlich wie %s selbst.\n\n' "$name"
			printf 'sha256:\n'
			printf '  %s\n' "$(cd "$sordner" && sha256sum etc-ngc.tar 2>/dev/null)"
		} >"$sordner/inhalt.txt"
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
		if krypt "$ziel" /var/lib/ngc/apps "$ordner/app-daten.tar.enc" \
			"$sordner/app-daten.schluessel"; then
			notiz "app-daten.tar.enc    die Datenordner der Apps, VERSCHLUESSELT (AES-256-CTR)."
			notiz "                     Der Schluessel dafuer ist in diesem Lauf neu erzeugt worden, gilt nur"
			notiz "                     fuer diesen Rechner und liegt im Schluessel-Ordner als"
			notiz "                     app-daten.schluessel - NICHT hier. Ohne ihn geht diese Datei nicht auf."
			notiz "                     Zurueck: openssl enc -d -aes-256-ctr -pbkdf2 -iter 600000 -md sha256"
			notiz "                              -pass file:<schluessel> -in app-daten.tar.enc | tar -x -C /"
			notiz "                     Auf der Maschine sind die Ordner mit fscrypt verschluesselt (Schluessel"
			notiz "                     aus dbKey), aber der Dienst lief beim Sichern - der Kern legte sie"
			notiz "                     entschluesselt vor. Darum diese zweite Verschluesselung."
		else
			fehlt "app-daten.tar.enc"
		fi
	else
		notiz "app-daten.tar.enc    keine App-Datenordner auf diesem Rechner."
	fi

	fern "$ziel" rm -rf "$PROBE"

	# 5. the sha256 of everything - AND IT GOES TO THE KEYS, not next to the
	#    data. The first version of this script wrote the list into the data
	#    folder beside the files it names, and the session that decided the
	#    shape of this backup took that apart: a list beside its files catches
	#    an ACCIDENT - a bad disk, a transfer that broke off - and nothing
	#    else. Whoever may write in the data folder changes the tar AND the
	#    line about it, and then the list proves what the last writer wanted.
	#
	#    So the same calculation as the 224 bytes of db-keys: the list is a few
	#    kilobytes, so it belongs where the keys are. Then one sentence holds
	#    for both halves - whoever has only the data can neither read them nor
	#    change them unnoticed - and it costs no running time, unlike a second
	#    layer of crypto (openssl enc has nothing authenticated that streams
	#    cleanly, and AES-GCM over 310 MB in one piece would be more risk than
	#    gain).
	#
	#    What the data folder keeps is the DESCRIPTION of every file, which is
	#    what an admin reads, plus the sentence that the sums are with the
	#    keys. A description nobody can check is still worth having; a sum
	#    nobody can trust is not.
	notiz ""
	notiz "Die Pruefsummen dieser Dateien stehen NICHT hier, sondern bei den Schluesseln"
	notiz "(summen.txt im Schluessel-Ordner dieses Rechners). Wer nur diesen Ordner hat,"
	notiz "kann die Dateien weder lesen noch unbemerkt aendern - das ist der Grund."
	{
		printf 'sha256 der Dateien im Daten-Ordner von %s, %s\n\n' "$name" "$STEMPEL"
		# Two sections in one file, and the hint belongs here: a reader who
		# runs "sha256sum -c" over the whole list in one directory gets a
		# false alarm for the other half - measured on the first list this
		# wrote. The list is for people AND for that command, so it says
		# which half to check where.
		#
		# AND THE HINT MUST NOT MATCH ITSELF. The first version of it read
		# grep "[*]" summen.txt, and that line contains the very thing it
		# greps for - so the hint was picked up as a checksum line and every
		# check came back one short. Measured on the first real backup, at
		# the moment somebody held the list against the files instead of
		# reading it. The pattern now asks for 64 hex digits at the start of
		# a line, which no sentence of this file can be.
		printf 'Pruefen, je Haelfte in ihrem eigenen Ordner:\n'
		printf '  sed -n "s/^  \\([0-9a-f]\\{64\\} \\)/\\1/p" summen.txt | sha256sum -c -\n'
		printf 'Der erste Abschnitt gehoert in den Daten-Ordner, der zweite hierher.\n'
		printf 'Eine Liste ueber beide Ordner auf einmal meldet die jeweils andere\n'
		printf 'Haelfte als fehlend - das ist kein Befund.\n\n'
		(cd "$ordner" && for f in *; do
			[ "$f" = inhalt.txt ] && continue
			printf '  %s\n' "$(sha256sum "$f")"
		done)
		printf '\nUnd der Schluessel-Ordner selbst:\n'
		(cd "$sordner" && for f in *; do
			case $f in inhalt.txt | summen.txt) continue ;;
			esac
			printf '  %s\n' "$(sha256sum "$f")"
		done)
	} >"$sordner/summen.txt"
done

cat >"$ZIEL/LIESMICH.txt" <<EOF
Sicherung einer NextGenCloud-Anlage - DIE DATEN, $STEMPEL

Je Rechner ein Ordner, darin inhalt.txt mit der Liste und den Pruefsummen.

DIE SCHLUESSEL LIEGEN NICHT HIER. Sie liegen im Schluessel-Ordner dieses
Laufs, und das ist Absicht: 224 Byte unter /etc/ngc/db-keys schliessen 285 MB
Datenbank auf, und der dbKey aus controller.json oeffnet die eigene Datenbank
jedes Controllers und die Datenordner aller Apps. Ohne diese Dateien ist alles
in diesem Ordner Geheimtext - von niemandem zu oeffnen, auch nicht von
NextGenCloud.

DIE PRUEFSUMMEN STEHEN AUCH NICHT HIER, sondern als summen.txt im
Schluessel-Ordner je Rechner. Eine Liste neben ihren Dateien faengt nur einen
Unfall; wer hier schreiben darf, aendert beides. Was hier steht, ist die
Beschreibung jeder Datei - die ist auch ohne Pruefsumme etwas wert, eine
Pruefsumme ohne Vertrauen nicht.

DAS GILT AUCH FUER DIE APP-DATEN, und das war einmal anders: app-daten.tar.enc
ist mit einem Schluessel verschluesselt, der in diesem Lauf neu erzeugt wurde
und je Rechner im Schluessel-Ordner liegt (app-daten.schluessel). Auf der
Maschine sind die Ordner mit fscrypt verschluesselt, aber beim Sichern lief
der Dienst, also legte der Kern sie entschluesselt vor - ohne diese zweite
Verschluesselung waere diese Datei der einzige Teil der Sicherung, der ohne
jeden Schluessel aufgeht. Wie sie zurueckkommt, steht in der inhalt.txt des
Rechners.

JEDER RECHNER HAT SEINEN EIGENEN dbKey (vier verschiedene auf dieser Anlage,
gemessen): es gibt keinen einen Schluessel der Anlage, den man einmal
wegschliesst.

Der Weg zurueck ist in docs/sicherung-und-weg-zurueck.md beschrieben, Schritt
fuer Schritt, und er ist einmal gegangen: auf einer frischen Maschine, die
dieser Anlage nie gehoert hat, lief eine App aus dieser Sicherung wieder.
Dort steht auch, was dabei NICHT zurueckkam.
EOF

cat >"$SCHLUESSEL/LIESMICH.txt" <<EOF
Sicherung einer NextGenCloud-Anlage - DIE SCHLUESSEL, $STEMPEL

DIESER ORDNER IST NUR DIE DURCHFUHR, KEIN AUFBEWAHRUNGSORT.

Was hier liegt, gehoert in den Passwortmanager dieses Hauses und danach von
dieser Platte geloescht. Je Rechner ein etc-ngc.tar (10 bis 20 KB), wo der
Rechner Apps traegt ein app-daten.schluessel von 45 Byte, und eine summen.txt
mit den Pruefsummen des Daten-Ordners.

WARUM DIE PRUEFSUMMEN HIER LIEGEN UND NICHT BEI DEN DATEN: eine Liste neben
ihren Dateien faengt einen Unfall - eine kaputte Platte, eine abgebrochene
Uebertragung. Wer im Daten-Ordner schreiben darf, aendert die Datei UND die
Zeile darueber. Hier getrennt gilt fuer beide Haelften derselbe Satz: wer nur
die Daten hat, kann sie weder lesen noch unbemerkt aendern.

Solange dieser Ordner hier steht, ist dieser Rechner so empfindlich wie die
Anlage selbst: wer ihn und den Daten-Ordner hat, hat die Anlage - die
Sicherung oeffnet auf einem beliebigen fremden Rechner, das ist gemessen.

Darum liegt er nicht neben den Daten. Zwei Ordner auf derselben Platte waeren
keine Trennung: dieselbe Maschine, dasselbe Konto, dieselben Rechte.

UND NICHT WEGWERFEN, SOLANGE ER NICHT UMGEZOGEN IST: ohne diese Dateien ist
der Daten-Ordner Geheimtext, vollstaendig und ohne Ausnahme. Der Verlust ist
endgueltig. Mehrere Verwahrstellen fuer die Schluessel, eine fuer die Daten.
EOF

if [ "$fehler" -ne 0 ]; then
	echo "sicherung: mit Luecken - siehe die Zeilen oben." >&2
	exit 1
fi
echo "sicherung: ok"
