#!/usr/bin/env bash
# Fall B: der DIENST-Stopp (systemctl stop), nicht der Maschinen-Stopp.
#
# Die Reihe vom 24.10.2026 bestand aus sechs "qm stop". Das ist der STRENGERE
# Fall: der Knoten verschwindet, Pakete an ihn verfallen stumm, Nachbardienste
# fallen mit, und beim Hochfahren raeumt cloud-init weg, was zur Laufzeit
# gesetzt war. Der Dienst-Stopp ist der mildere und nie gemessen worden:
#
#   Maschinen-Stopp         | Dienst-Stopp
#   ------------------------|---------------------------------------
#   Pakete verfallen stumm  | Kernel antwortet RST -> sofort refused
#   Nachbardienste fallen   | Nachbardienste laufen weiter
#   cloud-init raeumt auf   | nichts wird weggeraeumt
#   Galera-Knoten verschw.  | mariadb tritt GEORDNET aus dem Verbund aus
#
# Gefahren werden nur die Rollen, bei denen sich die beiden UNTERSCHEIDEN
# KOENNEN - nicht alle sechs noch einmal. Jeder Aufruf nimmt einen Fall.
#
# WARUM DIE ERSTEN FUENF EINZELN STEHEN. Gestern bekam die erste Anfrage nach
# dem Wegfall 503 nach 5,03 s und die zweite 200 in 0,03 s. Genau an dieser
# Stelle trennen sich Maschinen- und Dienst-Stopp, wenn die Erwartung stimmt:
# ein RST kommt in Millisekunden, ein stummer Knoten erst nach dem Zeitablauf.
# Wer nur vierzig zaehlt, sieht "kein Unterschied" und irrt sich.
#
# NICHTS WIRD NEU GESTARTET, WAS NICHT VORHER LIEF: der Dienst wird vor dem
# Stopp als aktiv nachgewiesen und nachher wieder angeworfen und nachgewiesen.
set -uo pipefail

K=$HOME/Documents/ssh/keys/id_ed25519_ngc
SSH="ssh -i $K -o StrictHostKeyChecking=no -o ConnectTimeout=8 -o BatchMode=yes"
A1=10.2.3.210
A2=10.2.3.211
DB1=10.2.3.212

# Fall -> Adresse, Dienst, Name des Knotens, Nachbardienst, der weiterlaufen MUSS
case "${1:-}" in
  B1) ADDR=$A2;          DIENST=ngc-api;        KNOTEN=api-2; NACHBAR="";          ;;
  B2) ADDR=$A1;          DIENST=ngc-api;        KNOTEN=api-1; NACHBAR=apache2;     ;;
  B3) ADDR=$DB1;         DIENST=mariadb;        KNOTEN=db-1;  NACHBAR=ngc-controller; ;;
  B4) ADDR=10.2.3.213;   DIENST=ngc-controller; KNOTEN=svc-1; NACHBAR="";          ;;
  B5) ADDR=10.2.3.215;   DIENST=nginx;          KNOTEN=store; NACHBAR="";          ;;
  *) echo "Aufruf: $0 B1|B2|B3|B4|B5"; exit 2;;
esac
DAUER=${2:-180}

seite() {
  curl.exe -sk -o NUL -w "%{http_code} %{time_total}s" --max-time 20 \
    --resolve "ngc.mein.lan:443:$A1" https://ngc.mein.lan/ 2>/dev/null || echo "000 -"
}
anm_voll() {
  local out code t body
  out=$(curl.exe -sk -w '\n%{http_code} %{time_total}' --max-time 25 -X POST \
    -H "content-type: application/json" -d '{"name":"weiricth"}' \
    --resolve "ngc.mein.lan:443:$A1" https://ngc.mein.lan/api/v1/login/start 2>/dev/null) \
    || out=$'\n000 -'
  code=$(printf '%s' "$out" | tail -1 | cut -d' ' -f1)
  t=$(printf '%s' "$out" | tail -1 | cut -d' ' -f2)
  body=$(printf '%s' "$out" | sed '$d' | tr -d '\r\n' | cut -c1-200)
  printf '%s in %ss  Rumpf: %s' "$code" "$t" "${body:-(leer)}"
}
anm_code() {
  curl.exe -sk -o NUL -w "%{http_code}" --max-time 25 -X POST \
    -H "content-type: application/json" -d '{"name":"weiricth"}' \
    --resolve "ngc.mein.lan:443:$A1" https://ngc.mein.lan/api/v1/login/start 2>/dev/null || echo 000
}
vierzig() {
  local ok=0 bad=0 codes="" c langsam=0 t
  for i in $(seq 1 40); do
    c=$(anm_code); [ "$c" = 200 ] && ok=$((ok+1)) || { bad=$((bad+1)); codes="$codes $c"; }
  done
  echo "$ok/40 gut, $bad schlecht:${codes:- -}"
}
# Direkt auf die beiden APIs, am Lastverteiler vorbei. Im vollen Betrieb
# antwortet login/start dort mit 400 (leerer Rumpf) - das ist "lebt".
direkt() {
  curl.exe -sk -o NUL -w "%{http_code} %{time_total}s" --max-time 12 \
    "https://$1:7443/api/v1/login/start" 2>/dev/null || echo "000 -"
}
# Sagt der Kernel RST oder verfallen die Pakete? Das ist der Kern von Fall B.
tcp() {
  curl.exe -sk -o NUL -w "%{http_code} %{time_total}s" --max-time 12 \
    "https://$1:$2/" 2>&1 | tail -1 || echo "000 -"
}
sql() { $SSH "root@$DB1" "mysql -N -B -e \"$1\" 2>&1 | tr '\t' '=' | tr '\n' ' '" 2>&1 | tail -1; }
verbund_von() { $SSH "root@$1" "mysql -N -B -e \"show status like 'wsrep_cluster_size'; show status like 'wsrep_cluster_status';\" 2>&1 | tr '\t' '=' | tr '\n' ' '" 2>&1 | tail -1; }
katalog() { $SSH "root@$DB1" 'mysql ngc -N -e "select seq,length(data) from ngc_blob where kind=\"catalog\";" 2>&1' 2>&1 | tr -d '\r' | tr '\n' ' '; }
container() {
  local n
  n=$($SSH "root@$1" 'pgrep -af "^/usr/bin/conmon" 2>/dev/null | grep -oE " -n [a-z0-9-]+" | sed "s/ -n //" | sort | tr "\n" " "' 2>&1)
  echo "${n:-KEINE}"
}
aktiv() { $SSH -o ConnectTimeout=6 "root@$1" "systemctl is-active $2" 2>&1 | tr -d '\r'; }

runde() {
  echo "--- $1 ($(date -u '+%H:%M:%S') UTC) ---"
  echo "    $DIENST auf $KNOTEN     : $(aktiv "$ADDR" "$DIENST")"
  [ -n "$NACHBAR" ] && echo "    $NACHBAR auf $KNOTEN (muss laufen): $(aktiv "$ADDR" "$NACHBAR")"
  echo "(a) Oberflaeche          : $(seite)"
  echo "(b) Anmeldung, einzeln   : $(anm_voll)"
  echo "(b) Anmeldung, 40x       : $(vierzig)"
  echo "    api-1:7443 direkt    : $(direkt $A1)"
  echo "    api-2:7443 direkt    : $(direkt $A2)"
  case "$KNOTEN" in
    db-1)  echo "    Verbund von db-2     : $(verbund_von 10.2.3.217)"
           echo "    Verbund von db-1     : $(verbund_von $DB1)" ;;
    store) echo "    Laden direkt :443    : $(tcp 10.2.3.215 443)"
           echo "    Katalog in der DB    : $(katalog)" ;;
    svc-1) echo "    Container svc-1      : $(container 10.2.3.213)"
           echo "    Container svc-2      : $(container 10.2.3.214)" ;;
  esac
}

echo "===== Fall $1: DIENST-Stopp - systemctl stop $DIENST auf $KNOTEN ($ADDR) ====="
echo "Gemessen von diesem PC aus, $(date -u '+%Y-%m-%d %H:%M:%S') UTC. Dauer aus: ${DAUER}s."
echo "Die Maschine bleibt AN. Zum Vergleich steht der Maschinen-Stopp derselben"
echo "Rolle in docs/ausfall-je-maschine.md."
echo

VORHER=$(aktiv "$ADDR" "$DIENST")
[ "$VORHER" = active ] || { echo "ABBRUCH: $DIENST auf $KNOTEN ist schon '$VORHER'. Nichts gemessen."; exit 3; }
runde "VORHER"
VOR=$(date -u '+%Y-%m-%d %H:%M:%S')

echo
echo ">>> systemctl stop $DIENST auf $KNOTEN"
$SSH "root@$ADDR" "systemctl stop $DIENST" 2>&1 | tr -d '\r'
for i in $(seq 1 15); do
  [ "$(aktiv "$ADDR" "$DIENST")" != active ] && break
  sleep 2
done
NACH=$(aktiv "$ADDR" "$DIENST")
[ "$NACH" = active ] && { echo "ABBRUCH: $DIENST laeuft noch ('$NACH'). Nichts gemessen."; exit 3; }
echo "    geprueft: $DIENST sagt '$NACH', die Maschine antwortet weiter"
echo "    ping $ADDR: $(ping -n 1 -w 1500 "$ADDR" >/dev/null 2>&1 && echo "antwortet (Maschine lebt)" || echo "STUMM - das waere ein Maschinen-Stopp")"
echo

echo "    Die ersten fuenf Anmeldungen, einzeln - hier trennt sich RST vom Zeitablauf:"
for i in 1 2 3 4 5; do echo "      $i. $(anm_voll)"; done
echo
runde "WAEHREND $DIENST auf $KNOTEN aus ist"
echo
echo "    Was die APIs sagen:"
for a in $A1 $A2; do
  echo "      --- $a:"
  $SSH "root@$a" "journalctl -u ngc-api --since '$VOR' --no-pager 2>/dev/null | tail -8 | sed 's/^/        /'" 2>&1
done
if [ "$KNOTEN" = api-1 ]; then
  echo "    Was der Apache sagt:"
  $SSH "root@$A1" "journalctl -u apache2 --since '$VOR' --no-pager 2>/dev/null | tail -6 | sed 's/^/      /'; tail -6 /var/log/apache2/error.log 2>/dev/null | sed 's/^/      /'" 2>&1
fi
if [ "$KNOTEN" = db-1 ]; then
  echo "    Was mariadb auf db-2 zum Austritt sagt:"
  $SSH "root@10.2.3.217" "journalctl -u mariadb --since '$VOR' --no-pager 2>/dev/null | grep -iE 'wsrep|view|member|left' | tail -8 | sed 's/^/      /'" 2>&1
fi

echo
echo "    (bleibt ${DAUER}s aus)"
sleep "$DAUER"
echo
echo ">>> systemctl start $DIENST auf $KNOTEN"
AUS=$(date -u '+%Y-%m-%d %H:%M:%S')
$SSH "root@$ADDR" "systemctl start $DIENST" 2>&1 | tr -d '\r'
for i in $(seq 1 45); do
  [ "$(aktiv "$ADDR" "$DIENST")" = active ] && break
  sleep 2
done
echo "    $DIENST sagt jetzt: $(aktiv "$ADDR" "$DIENST")"
if [ "$KNOTEN" = db-1 ]; then
  echo "    Beitritt: IST oder SST? (das ist der Unterschied zum Maschinen-Stopp von gestern)"
  $SSH "root@$ADDR" "journalctl -u mariadb --since '$AUS' --no-pager 2>/dev/null | grep -iE 'IST |SST |receiv|joiner|synced|state transfer' | tail -12 | sed 's/^/      /'" 2>&1
  echo "    NRestarts: $($SSH "root@$ADDR" 'systemctl show mariadb -p NRestarts --value' 2>&1 | tr -d '\r')"
  echo "    Verbund im Halbminutentakt, bis er wieder 3 ist:"
  for i in 1 2 3 4 5 6; do
    sleep 30
    v=$(verbund_von 10.2.3.217)
    printf "      +%3ds  %s\n" $((i*30)) "$v"
    echo "$v" | grep -q "wsrep_cluster_size=3" && break
  done
else
  sleep 20
fi
echo
runde "NACHHER"
echo "===== Ende Fall $1 ====="
