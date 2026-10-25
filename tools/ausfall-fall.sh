#!/usr/bin/env bash
# Ein Ausfallfall: Maschine aus, messen, Maschine ein, messen.
#
# Zwei Messregeln stecken hier drin, beide am 24.10.2026 an dieser Anlage
# gemessen und beide teuer:
#  1. Der DNS dieses Netzes liefert fuer jeden *.ngc.mein.lan neben dem A-Satz
#     ein AAAA ::. Ein Werkzeug auf einem der Knoten nimmt das :: zuerst und
#     landet auf der EIGENEN Maschine. Darum jede Sonde mit --resolve.
#  2. Der Laden hat ein Ed25519-Zertifikat. Beide curl unter Windows scheitern
#     am Handshake (schannel: SEC_E_ILLEGAL_MESSAGE), ohne dass -k hilft. Der
#     Laden wird darum von api-1 aus gefragt, nicht von diesem PC.
#
# Das Skript prueft seine eigene Vorbereitung: "aus" gilt erst, wenn qm status
# es sagt UND die Adresse stumm ist. Sonst bricht es ab, statt zu messen.
set -uo pipefail

PVE=root@10.2.3.2
K=$HOME/Documents/ssh/keys/id_ed25519_ngc
SSH="ssh -i $K -o StrictHostKeyChecking=no -o ConnectTimeout=8 -o BatchMode=yes"
API1=10.2.3.210

# Die einzigen VMIDs, die dieses Skript anfassen darf. Auf demselben Wirt
# laufen das NAS (200), PC02, die Kameras und loxberry - ein Tippfehler dort
# waere ein Schaden, kein Messfehler.
erlaubt() { case "$1" in 301|302|303|304|305|306|308|309) return 0;; *) return 1;; esac; }

VMID=${1:?VMID fehlt}
ADDR=${2:?Adresse fehlt}
NAME=${3:?Name fehlt}
erlaubt "$VMID" || { echo "ABBRUCH: VMID $VMID steht nicht auf der Liste der sechs"; exit 2; }

qmc() { $SSH "$PVE" "qm $*"; }
status() { qmc status "$VMID" | sed 's/^status: //'; }
lebt() { ping -n 1 -w 1500 "$ADDR" >/dev/null 2>&1; }

# (a) Oberflaeche: kommt die Seite, und von welcher Maschine.
oberflaeche() {
  curl.exe -sk -o /dev/null -w "code=%{http_code} von=%{remote_ip} zeit=%{time_total}" \
    --max-time 20 --resolve "ngc.mein.lan:443:$API1" https://ngc.mein.lan/ 2>&1 \
    || echo "code=000 (keine Antwort)"
}
# Der Laden, von api-1 aus, mit --resolve: die ehrliche Frage.
laden() {
  $SSH "root@$API1" 'curl -sk -o /dev/null -w "code=%{http_code} von=%{remote_ip}" --max-time 8 --resolve store.ngc.mein.lan:443:10.2.3.215 https://store.ngc.mein.lan/catalog.json' 2>&1 \
    || echo "code=000 (keine Antwort)"
}
# Dieselbe Frage OHNE --resolve - genau das, was ein Mensch tippt.
laden_naiv() {
  $SSH "root@$API1" 'curl -sk -o /dev/null -w "code=%{http_code} von=%{remote_ip}" --max-time 8 https://store.ngc.mein.lan/catalog.json' 2>&1 \
    || echo "code=000 (keine Antwort)"
}
# Die beiden API-Mitglieder des Lastverteilers, einzeln und direkt.
apis() {
  for a in 10.2.3.210 10.2.3.211; do
    printf "      %s:7443 " "$a"
    curl.exe -sk -o /dev/null -w "code=%{http_code} zeit=%{time_total}\n" \
      --max-time 20 "https://$a:7443/" 2>&1 || echo "code=000 keine Antwort"
  done
}
# Zehn Anfragen hintereinander durch den Lastverteiler: faellt jede zweite?
reihe() {
  local n=0 ok=0 schlecht=0 out=""
  for i in $(seq 1 10); do
    c=$(curl.exe -sk -o /dev/null -w "%{http_code}" --max-time 20 \
      --resolve "ngc.mein.lan:443:$API1" https://ngc.mein.lan/ 2>/dev/null)
    out="$out $c"; n=$((n+1))
    [ "$c" = "200" ] && ok=$((ok+1)) || schlecht=$((schlecht+1))
  done
  echo "$ok/$n mal 200, $schlecht anders:$out"
}
journal() {
  local host=$1 seit=$2
  $SSH "root@$host" "journalctl -u ngc-api -u ngc-controller --since '$seit' --no-pager 2>/dev/null | tail -25" \
    2>&1 || echo "      (Knoten nicht erreichbar)"
}

runde() {
  echo "--- $1 ($(date '+%H:%M:%S')) ---"
  echo "    qm status      : $(status)"
  echo "    ping $ADDR     : $(lebt && echo antwortet || echo stumm)"
  echo "(a) Oberflaeche    : $(oberflaeche)"
  echo "    zehn in Folge  : $(reihe)"
  echo "    Laden (resolve): $(laden)"
  echo "    Laden (naiv)   : $(laden_naiv)"
  echo "    APIs einzeln   :"; apis
}

echo "=========== Fall: $NAME aus (VMID $VMID, $ADDR) ==========="
echo "Gemessen von diesem PC aus, $(date '+%Y-%m-%d %H:%M:%S')."

runde "VORHER, alles laeuft"
VOR=$(date -u '+%Y-%m-%d %H:%M:%S')

echo
echo ">>> qm stop $VMID"
qmc "stop $VMID"
for i in $(seq 1 20); do
  s=$(status); lebt && p=antwortet || p=stumm
  [ "$s" = "stopped" ] && [ "$p" = stumm ] && break
  sleep 2
done
if [ "$(status)" != "stopped" ] || lebt; then
  echo "ABBRUCH: $NAME ist nicht nachweisbar aus (qm sagt '$(status)')."
  echo "Es wird nichts gemessen. Ich fahre sie wieder an."
  qmc "start $VMID"; exit 3
fi
echo "    Vorbereitung geprueft: qm sagt stopped UND $ADDR ist stumm."
echo

sleep 5
runde "WAEHREND $NAME aus ist"
echo
echo "    Journal api-1 seit dem Abschalten (UTC $VOR):"; journal 10.2.3.210 "$VOR"
echo "    Journal api-2 seit dem Abschalten:"; journal 10.2.3.211 "$VOR"

echo
echo ">>> qm start $VMID   (faengt sich die Anlage von selbst?)"
AUS=$(date -u '+%Y-%m-%d %H:%M:%S')
qmc "start $VMID"
for i in $(seq 1 45); do lebt && break; sleep 2; done
lebt && echo "    $ADDR antwortet nach dem Hochfahren" || echo "    $ADDR antwortet NICHT (90 s lang nicht)"
sleep 20
runde "NACHHER, von selbst"
echo "    Laufende Dienste auf $NAME:"
$SSH "root@$ADDR" 'systemctl list-units --type=service --state=running --no-legend --plain 2>/dev/null | grep -E "ngc-|nginx|mariadb" | awk "{print \"      \"\$1, \$3, \$4}"; true' 2>&1
echo "    Journal auf $NAME seit dem Hochfahren:"; journal "$ADDR" "$AUS"
echo "=========== Ende Fall $NAME ==========="
