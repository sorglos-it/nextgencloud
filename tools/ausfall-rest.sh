#!/usr/bin/env bash
# Die drei verbleibenden Faelle: api-2, svc-1, svc-2. api-1 bleibt dabei oben,
# die Oberflaeche ist also messbar - anders als in den beiden harten Faellen.
#
# Messregeln wie in den anderen Blaettern: von diesem PC aus, immer gegen die
# nackte IP oder mit --resolve (AAAA :: im DNS), Container von root aus an den
# conmon-Prozessen (rootloses podman meldet als su-Kind faelschlich "nichts"),
# und "aus" gilt erst, wenn qm status es sagt UND die Adresse stumm ist.
set -uo pipefail

PVE=root@10.2.3.2
K=$HOME/Documents/ssh/keys/id_ed25519_ngc
SSH="ssh -i $K -o StrictHostKeyChecking=no -o ConnectTimeout=8 -o BatchMode=yes"
A1=10.2.3.210
S1=10.2.3.213
S2=10.2.3.214

erlaubt() { case "$1" in 301|302|303|304|305|306|308|309) return 0;; *) return 1;; esac; }

VMID=${1:?}; ADDR=${2:?}; NAME=${3:?}
erlaubt "$VMID" || { echo "ABBRUCH: VMID $VMID nicht auf der Liste"; exit 2; }

q() { $SSH "$PVE" "qm $*"; }
st() { q status "$VMID" | sed 's/^status: //'; }
lebt() { ping -n 1 -w 1500 "$ADDR" >/dev/null 2>&1; }

seite() {
  curl.exe -sk -o NUL -w "%{http_code}" --max-time 15 --resolve "ngc.mein.lan:443:$A1" \
    https://ngc.mein.lan/ 2>/dev/null || echo 000
}
# Zwanzig Anfragen in Folge durch den Lastverteiler: faellt jede zweite, bis
# Apache das totes Mitglied merkt? (lbmethod=byrequests, retry=5, timeout=65)
reihe() {
  local ok=0 schlecht=0 out=""
  for i in $(seq 1 20); do
    c=$(curl.exe -sk -o NUL -w "%{http_code}" --max-time 20 \
      --resolve "ngc.mein.lan:443:$A1" https://ngc.mein.lan/ 2>/dev/null)
    out="$out ${c:-000}"
    [ "$c" = "200" ] && ok=$((ok+1)) || schlecht=$((schlecht+1))
  done
  echo "$ok/20 mal 200, $schlecht anders:$out"
}
anmelden() {
  curl.exe -sk -w "|code=%{http_code}" --max-time 20 -X POST \
    -H "content-type: application/json" -d '{"name":"weiricth"}' \
    --resolve "ngc.mein.lan:443:$A1" https://ngc.mein.lan/api/v1/login/start 2>/dev/null \
    | head -c 110 || echo "|code=000"
}
container() { # knoten
  local n
  n=$($SSH "root@$1" 'pgrep -af "^/usr/bin/conmon" 2>/dev/null | grep -oE " -n [a-z0-9-]+" | sed "s/ -n //" | sort | tr "\n" " "' 2>&1)
  echo "${n:-KEINE/nicht erreichbar}"
}
# Sind die App-Speicher nach dem harten Schnitt heil? Die Frage einer
# Nachbarsitzung: rootloses podman mit overlay und einer SQLite db.sql auf
# einem eigenen ext4 ist die Stelle, an der ein Stromausfall etwas zerbricht.
#
# NICHT mit sqlite3: das Programm liegt auf den Knoten nicht und die Sonde
# meldete "command not found" wie ein Ergebnis. python3 ist da und bringt das
# Modul mit; geoeffnet wird nur lesend (mode=ro).
speicher() { # knoten
  $SSH "root@$1" 'findmnt -no SOURCE,FSTYPE --target /var/lib/ngc/apps 2>/dev/null | tr -d "\n"; echo -n "   "; python3 - <<PY
import sqlite3,glob,os
out=[]
for d in sorted(glob.glob("/var/lib/ngc/apps/org.sorglos.*/")):
    p=os.path.join(d,".local/share/containers/storage/db.sql")
    n=os.path.basename(d.rstrip("/"))
    if not os.path.exists(p): out.append(n+"=keine db.sql"); continue
    try:
        c=sqlite3.connect("file:%s?mode=ro"%p,uri=True)
        out.append(n+"="+c.execute("pragma integrity_check").fetchone()[0])
    except Exception as e: out.append(n+"=FEHLER "+str(e))
print(" ".join(out) or "keine App-Verzeichnisse")
PY' 2>&1 | tail -1
}
tunnel() { # knoten
  $SSH "root@$1" 'systemctl is-active ngc-controller 2>/dev/null' 2>&1 | tr -d '\n'
}

runde() {
  echo "--- $1 ($(date -u '+%H:%M:%S') UTC) ---"
  echo "    qm status $NAME     : $(st)"
  echo "(a) Oberflaeche        : $(seite)"
  echo "    zwanzig in Folge   : $(reihe)"
  echo "(b) Anmeldung beginnt  : $(anmelden)"
  echo "    Container svc-1    : $(container $S1)"
  echo "    Container svc-2    : $(container $S2)"
  echo "    Controller svc-1/svc-2: $(tunnel $S1) / $(tunnel $S2)"
}

echo "=========== Fall: $NAME aus (VMID $VMID, $ADDR) ==========="
echo "Gemessen von diesem PC aus, $(date -u '+%Y-%m-%d %H:%M:%S') UTC."
echo
runde "VORHER"
[ "$NAME" != api-2 ] && echo "    App-Speicher $NAME : $(speicher $ADDR)"
VOR=$(date -u '+%Y-%m-%d %H:%M:%S')

echo
echo ">>> qm stop $VMID"
q "stop $VMID"
for i in $(seq 1 20); do
  s=$(st); lebt && p=an || p=stumm
  [ "$s" = stopped ] && [ "$p" = stumm ] && break
  sleep 2
done
if [ "$(st)" != stopped ] || lebt; then
  echo "ABBRUCH: $NAME nicht nachweisbar aus (qm sagt '$(st)'). Nichts gemessen."
  q "start $VMID"; exit 3
fi
echo "    geprueft: qm sagt stopped UND $ADDR ist stumm."
echo
runde "WAEHREND $NAME aus ist (sofort, bevor Apache es merken kann)"
echo
echo "    Was api-1 dazu sagt:"
$SSH "root@$A1" "journalctl -u ngc-api --since '$VOR' --no-pager 2>/dev/null | tail -6 | sed 's/^/      /'; grep -ciE 'proxy|balancer' /var/log/apache2/error.log 2>/dev/null | sed 's/^/      Apache-Zeilen zu proxy\/balancer insgesamt: /'" 2>&1
$SSH "root@$A1" "tail -5 /var/log/apache2/error.log 2>/dev/null | sed 's/^/      /'" 2>&1

echo
echo ">>> qm start $VMID"
AUS=$(date -u '+%Y-%m-%d %H:%M:%S')
q "start $VMID"
for i in $(seq 1 60); do lebt && break; sleep 2; done
lebt && echo "    $ADDR antwortet wieder" || echo "    $ADDR antwortet NICHT"
echo "    warte auf den Wiederanlauf der Dienste..."
sleep 45
runde "NACHHER, von selbst"
if [ "$NAME" != api-2 ]; then
  echo "    App-Speicher $NAME : $(speicher $ADDR)"
  echo "    TasksCurrent/Max auf $NAME:"
  $SSH "root@$ADDR" 'systemctl show -p TasksCurrent -p TasksMax --value ngc-controller | tr "\n" "/"; echo' 2>&1
  echo "    Journal des Controllers seit dem Hochfahren:"
  $SSH "root@$ADDR" "journalctl -u ngc-controller --since '$AUS' --no-pager 2>/dev/null | tail -8 | sed 's/^/      /'" 2>&1
fi
echo "=========== Ende $NAME ==========="
