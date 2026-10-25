#!/usr/bin/env bash
# Ein Datenbankknoten fällt, bei dreien im Verbund.
#
# Zwei Fälle: db-1 (303) hat den Verbund gebootet, db-2 (308) und db-3 (309)
# sind beigetreten. Erwartung für beide: die Anlage läuft weiter, weil zwei von
# drei das Quorum halten UND beide APIs alle drei in db.nodes tragen.
#
# Gemessen wird von diesem PC aus, immer mit --resolve oder gegen die nackte
# Adresse: der DNS dieses Netzes liefert für jeden *.ngc.mein.lan ein AAAA ::.
# Der Verbund wird von einem Knoten erfragt, der STEHT - nicht von dem, der
# gerade aus ist, sonst misst man die ssh-Zeitüberschreitung.
#
# Zwei Zahlen getrennt, weil der api-2-Fall gezeigt hat, dass sie auseinander
# liegen: die ERSTE Anfrage nach dem Abschalten, und dann vierzig in Folge.
set -uo pipefail

PVE=root@10.2.3.2
K=$HOME/Documents/ssh/keys/id_ed25519_ngc
SSH="ssh -i $K -o StrictHostKeyChecking=no -o ConnectTimeout=8 -o BatchMode=yes"
A1=10.2.3.210
A2=10.2.3.211
# alle drei DB-Knoten: VMID:Adresse:Name
DBS="303:10.2.3.212:db-1 308:10.2.3.217:db-2 309:10.2.3.218:db-3"

erlaubt() { case "$1" in 301|302|303|304|305|306|308|309) return 0;; *) return 1;; esac; }

VMID=${1:?VMID fehlt}
erlaubt "$VMID" || { echo "ABBRUCH: VMID $VMID steht nicht auf der Liste"; exit 2; }
ADDR=""; NAME=""
for e in $DBS; do
  [ "${e%%:*}" = "$VMID" ] && { ADDR=$(echo "$e" | cut -d: -f2); NAME=$(echo "$e" | cut -d: -f3); }
done
[ -n "$ADDR" ] || { echo "ABBRUCH: VMID $VMID ist kein Datenbankknoten"; exit 2; }
# Ein Knoten, der stehen bleibt - von dort wird der Verbund erfragt.
ZEUGE=""
for e in $DBS; do
  a=$(echo "$e" | cut -d: -f2); [ "$a" != "$ADDR" ] && { ZEUGE=$a; break; }
done

q() { $SSH "$PVE" "qm $*"; }
st() { q status "$VMID" | sed 's/^status: //'; }
lebt() { ping -n 1 -w 1500 "$ADDR" >/dev/null 2>&1; }

seite() {
  curl.exe -sk -o NUL -w "%{http_code}" --max-time 15 --resolve "ngc.mein.lan:443:$A1" \
    https://ngc.mein.lan/ 2>/dev/null || echo 000
}
# Eine Anmeldeanfrage, mit Zeit. login/start zählt laut Code keinen Fehlversuch.
anm() {
  curl.exe -sk -o NUL -w "%{http_code} %{time_total}s" --max-time 20 -X POST \
    -H "content-type: application/json" -d '{"name":"weiricth"}' \
    --resolve "ngc.mein.lan:443:$A1" https://ngc.mein.lan/api/v1/login/start 2>/dev/null \
    || echo "000 -"
}
anm_code() {
  curl.exe -sk -o NUL -w "%{http_code}" --max-time 20 -X POST \
    -H "content-type: application/json" -d '{"name":"weiricth"}' \
    --resolve "ngc.mein.lan:443:$A1" https://ngc.mein.lan/api/v1/login/start 2>/dev/null || echo 000
}
vierzig() {
  local ok=0 bad=0 codes="" c
  for i in $(seq 1 40); do
    c=$(anm_code); [ "$c" = 200 ] && ok=$((ok+1)) || { bad=$((bad+1)); codes="$codes $c"; }
  done
  echo "$ok/40 gut, $bad schlecht:${codes:- -}"
}
verbund() { # von welchem Knoten
  $SSH "root@$1" 'mysql -N -e "show status like \"wsrep_cluster_size\"; show status like \"wsrep_cluster_status\"; show status like \"wsrep_incoming_addresses\";" 2>&1 | tr "\t" "=" | tr "\n" " "' 2>&1 | tail -1
}
dbs_aktiv() {
  local out=""
  for e in $DBS; do
    a=$(echo "$e" | cut -d: -f2); n=$(echo "$e" | cut -d: -f3)
    s=$($SSH -o ConnectTimeout=5 "root@$a" 'systemctl is-active mariadb' 2>/dev/null || echo "nicht erreichbar")
    out="$out $n=$s"
  done
  echo "$out"
}
container() {
  local n
  n=$($SSH "root@$1" 'pgrep -af "^/usr/bin/conmon" 2>/dev/null | grep -oE " -n [a-z0-9-]+" | sed "s/ -n //" | sort | tr "\n" " "' 2>&1)
  echo "${n:-KEINE}"
}

runde() {
  echo "--- $1 ($(date -u '+%H:%M:%S') UTC) ---"
  echo "    qm status $NAME  : $(st)"
  echo "(a) Oberflaeche     : $(seite)"
  echo "(b) Anmeldung, 40x  : $(vierzig)"
  echo "    Verbund (von $ZEUGE): $(verbund $ZEUGE)"
  echo "    mariadb je Knoten:$(dbs_aktiv)"
  echo "    Container svc-1/2: $(container 10.2.3.213) / $(container 10.2.3.214)"
}

echo "=========== Fall: $NAME aus (VMID $VMID, $ADDR), drei Knoten im Verbund ==========="
echo "Gemessen von diesem PC aus, $(date -u '+%Y-%m-%d %H:%M:%S') UTC."
echo "Zeuge fuer den Verbund: $ZEUGE (bleibt stehen)."
echo "db.nodes der APIs:"
for a in $A1 $A2; do
  printf "    %s: " "$a"
  $SSH "root@$a" 'python3 -c "import json;print(json.load(open(\"/etc/ngc/api.json\"))[\"db\"][\"nodes\"])"' 2>&1
done
echo
runde "VORHER"
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
  echo "ABBRUCH: $NAME ist nicht nachweisbar aus (qm sagt '$(st)'). Nichts gemessen."
  q "start $VMID"; exit 3
fi
echo "    geprueft: qm sagt stopped UND $ADDR ist stumm."
echo
# Die ERSTE Anfrage, sofort und einzeln - der api-2-Fall hat gezeigt, dass sie
# anders aussieht als die vierzigste.
echo "    ERSTE Anmeldung nach dem Abschalten: $(anm)"
echo "    zweite: $(anm)    dritte: $(anm)"
echo
runde "WAEHREND $NAME aus ist"
echo
echo "    Was die APIs sagen:"
for a in $A1 $A2; do
  echo "      --- $a:"
  $SSH "root@$a" "journalctl -u ngc-api --since '$VOR' --no-pager 2>/dev/null | tail -6 | sed 's/^/        /'" 2>&1
done

echo
echo ">>> qm start $VMID   (kommt ein $([ "$NAME" = db-1 ] && echo Bootender || echo Beitretender) von selbst zurueck?)"
AUS=$(date -u '+%Y-%m-%d %H:%M:%S')
q "start $VMID"
for i in $(seq 1 60); do lebt && break; sleep 2; done
lebt && echo "    $ADDR antwortet wieder" || echo "    $ADDR antwortet NICHT"
echo "    Verbund im Halbminutentakt, bis er wieder 3 ist (hoechstens 4 min):"
for i in 1 2 3 4 5 6 7 8; do
  sleep 30
  v=$(verbund "$ZEUGE")
  printf "      +%3ds  %s\n" $((i*30)) "$v"
  echo "$v" | grep -q "wsrep_cluster_size=3" && break
done
echo "    mariadb auf $NAME: $($SSH -o ConnectTimeout=6 root@$ADDR 'systemctl is-active mariadb' 2>&1)"
echo "    Journal von mariadb auf $NAME seit dem Hochfahren:"
$SSH "root@$ADDR" "journalctl -u mariadb --since '$AUS' --no-pager 2>/dev/null | grep -iE 'wsrep|sst|joiner|synced|bootstrap|ERROR' | tail -12 | sed 's/^/      /'" 2>&1
echo "    grastate.dat auf $NAME:"
$SSH "root@$ADDR" "sed -n '1,6p' /var/lib/mysql/grastate.dat 2>&1 | sed 's/^/      /'" 2>&1
echo
runde "NACHHER, von selbst"
echo "=========== Ende $NAME ==========="
