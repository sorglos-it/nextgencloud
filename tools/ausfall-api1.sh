#!/usr/bin/env bash
# api-1 aus. Die Frage des Auftrags: ist der zweite API-Server etwas wert oder
# nur Zierde?
#
# Lebensprobe ist POST /api/v1/login/start mit {} - das gibt bei voller
# Gesundheit auf BEIDEN APIs 400 und durch den Lastverteiler ebenfalls 400,
# gemessen vorher. Ein leerer Rumpf wird abgewiesen, bevor ein Name geprueft
# wird, es wird also kein Anmeldeversuch gezaehlt (freeCount=2 bleibt unberuehrt).
#
# Gemessen wird von diesem PC aus, immer gegen die nackte IP oder mit
# --resolve: der DNS dieses Netzes liefert fuer jeden Namen ein AAAA ::.
set -uo pipefail

PVE=root@10.2.3.2
K=$HOME/Documents/ssh/keys/id_ed25519_ngc
SSH="ssh -i $K -o StrictHostKeyChecking=no -o ConnectTimeout=8 -o BatchMode=yes"
VMID=301
A1=10.2.3.210
A2=10.2.3.211
DB=10.2.3.212

erlaubt() { case "$1" in 301|302|303|304|305|306|308|309) return 0;; *) return 1;; esac; }
erlaubt "$VMID" || { echo "ABBRUCH: VMID $VMID nicht auf der Liste"; exit 2; }

q() { $SSH "$PVE" "qm $*"; }
st() { q status "$VMID" | sed 's/^status: //'; }
lebt() { ping -n 1 -w 1500 "$A1" >/dev/null 2>&1; }

probe() { # host port pfad
  curl.exe -sk -o NUL -w "%{http_code}" --max-time 12 -X POST \
    -H "content-type: application/json" -d '{}' "https://$1:$2$3" 2>/dev/null || echo 000
}
seite() { # host
  curl.exe -sk -o NUL -w "%{http_code}" --max-time 12 "https://$1:443/" 2>/dev/null || echo 000
}
ueber_namen() {
  curl.exe -sk -o NUL -w "%{http_code}" --max-time 12 --resolve "ngc.mein.lan:443:$A1" \
    https://ngc.mein.lan/ 2>/dev/null || echo 000
}
# Laufen die Apps weiter, waehrend niemand sie sehen kann?
# Von root aus gemessen, nicht per su: rootloses podman findet als su-Kind sein
# Laufzeitverzeichnis nicht ("mkdir /run/user/0/libpod: permission denied") und
# meldete darum "nichts", obwohl die Container liefen. Die conmon-Prozesse sind
# von root sichtbar und tragen den Namen des Behaelters hinter -n.
apps_laufen() { # knoten
  local n
  n=$($SSH "root@$1" 'pgrep -af "^/usr/bin/conmon" 2>/dev/null | grep -oE " -n [a-z0-9-]+" | sed "s/ -n //" | sort | tr "\n" " "' 2>&1)
  echo "${n:-KEINE}"
}
tunnel() { # knoten
  $SSH "root@$1" 'systemctl is-active ngc-controller 2>/dev/null | tr "\n" " "; journalctl -u ngc-controller --no-pager -n 3 2>/dev/null | tail -3 | sed "s/^/        /"' 2>&1
}

runde() {
  echo "--- $1 ($(date -u '+%H:%M:%S') UTC) ---"
  echo "    qm status api-1          : $(st)"
  echo "(a) Oberflaeche ngc.mein.lan : $(ueber_namen)   (voller Betrieb: 200)"
  echo "    api-1:443 direkt         : $(seite $A1)   (voller Betrieb: 200)"
  echo "    api-2:443 direkt         : $(seite $A2)   (voller Betrieb: 000, dort liegt kein Web)"
  echo "    api-1:7443 login/start   : $(probe $A1 7443 /api/v1/login/start)   (voller Betrieb: 400)"
  echo "    api-2:7443 login/start   : $(probe $A2 7443 /api/v1/login/start)   (voller Betrieb: 400)"
  echo "    mariadb auf db-1         : $($SSH root@$DB 'systemctl is-active mariadb' 2>&1)"
  for n in $DB 10.2.3.213 10.2.3.214; do
    echo "    Controller $n:"; tunnel "$n"
  done
}

echo "=========== Fall: api-1 aus (VMID 301, $A1) ==========="
echo "Gemessen von diesem PC aus, $(date -u '+%Y-%m-%d %H:%M:%S') UTC."
echo
echo "Vor dem Schalten gemessen, aus /etc/ngc/controller.json:"
echo "  db-1, svc-1 und svc-2 nennen ALLE NUR wss://api-1...:7443/tunnel."
echo "  api-2 steht in keiner Controller-Konfiguration. Nur web.json auf api-1"
echo "  nennt beide APIs."
echo
runde "VORHER"
echo "    Apps auf svc-1: $(apps_laufen 10.2.3.213)"
echo "    Apps auf svc-2: $(apps_laufen 10.2.3.214)"
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
  echo "ABBRUCH: api-1 ist nicht nachweisbar aus (qm sagt '$(st)'). Nichts gemessen."
  q "start $VMID"; exit 3
fi
echo "    geprueft: qm sagt stopped UND $A1 ist stumm."
echo

sleep 20
runde "WAEHREND api-1 aus ist"
echo "    Apps auf svc-1: $(apps_laufen 10.2.3.213)"
echo "    Apps auf svc-2: $(apps_laufen 10.2.3.214)"
echo
echo "    Versuchen die Controller api-2? (Journal seit dem Abschalten)"
for n in $DB 10.2.3.213 10.2.3.214; do
  printf "      %s: " "$n"
  $SSH "root@$n" "journalctl -u ngc-controller --since '$VOR' --no-pager 2>/dev/null | grep -ciE 'api-2|10\.2\.3\.211' || echo 0" 2>&1 | tr -d '\n'
  echo " Treffer auf api-2"
done
echo "    Was die Controller stattdessen sagen:"
$SSH "root@10.2.3.213" "journalctl -u ngc-controller --since '$VOR' --no-pager 2>/dev/null | tail -6 | sed 's/^/      /'" 2>&1

echo
echo ">>> qm start $VMID   (faengt sich die Anlage von selbst?)"
AUS=$(date -u '+%Y-%m-%d %H:%M:%S')
q "start $VMID"
for i in $(seq 1 60); do lebt && break; sleep 2; done
lebt && echo "    $A1 antwortet nach dem Hochfahren" || echo "    $A1 antwortet NICHT (120 s lang nicht)"
# Warten, bis die Oberflaeche wieder 200 sagt - und zaehlen, wie lange.
T0=$(date +%s)
for i in $(seq 1 60); do
  [ "$(ueber_namen)" = "200" ] && break
  sleep 3
done
T1=$(date +%s)
echo "    Oberflaeche wieder 200 nach $((T1-T0)) s ab Antwort der Adresse"
sleep 15
runde "NACHHER, von selbst"
echo "    Apps auf svc-1: $(apps_laufen 10.2.3.213)"
echo "    Apps auf svc-2: $(apps_laufen 10.2.3.214)"
echo "=========== Ende api-1 ==========="
