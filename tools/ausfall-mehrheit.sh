#!/usr/bin/env bash
# Fall A: ZWEI Datenbankknoten gleichzeitig aus, bei dreien im Verbund.
#
# Am 24.10.2026 ist jeder der drei Knoten einmal einzeln gefallen: zwei halten
# das Quorum, die Anlage laeuft weiter. Dieser Fall nimmt die Mehrheit weg -
# db-2 (308) und db-3 (309) zusammen, db-1 (303) bleibt allein stehen.
#
# WARUM DIE DATENBANK VOR DER ANLAGE GEFRAGT WIRD. Die Erwartung, die hier
# nachgemessen wird, lautet "lesen geht noch, jedes Schreiben wird verweigert".
# Wer nur die Anmeldung misst, bekommt ein "geht nicht" und weiss nicht, ob das
# Lesen oder das Schreiben daran gescheitert ist. Darum erst SELECT und INSERT
# auf db-1 selbst, und dann die Anlage. Die Schreibprobe laeuft in einer
# EIGENEN Datenbank (messung.nachtrag), nie in ngc: eine Messung darf keine
# Zeile der Anlage anfassen.
#
# WARUM DER WORTLAUT UND NICHT NUR DER CODE. Der schwerere Befund waere eine
# Anlage, die beim Verlust der Mehrheit "Anmeldung fehlgeschlagen" sagt statt
# "Dienst nicht erreichbar" - also dem Benutzer einen Fehler zuschiebt, den er
# nicht gemacht hat. Ein HTTP-Code allein zeigt das nicht; login.js:449
# entscheidet am Feld "code" des Rumpfes. Also wird der Rumpf mitgeschrieben.
#
# WARUM DIE ERSTE ANFRAGE EINZELN. Gestern bekam die erste Anfrage nach dem
# Wegfall 503 nach 5,03 s und die zweite 200 in 0,03 s. Wer nur vierzig zaehlt,
# sieht "kein Unterschied" und irrt sich. Hier stehen die ersten fuenf einzeln.
set -uo pipefail

PVE=root@10.2.3.2
K=$HOME/Documents/ssh/keys/id_ed25519_ngc
SSH="ssh -i $K -o StrictHostKeyChecking=no -o ConnectTimeout=8 -o BatchMode=yes"
A1=10.2.3.210
A2=10.2.3.211
# Der Knoten, der STEHEN BLEIBT - von ihm wird der Verbund und die Datenbank
# erfragt. Nie von einem, der gerade aus ist: das misst die ssh-Zeit.
BLEIBT=10.2.3.212
BLEIBT_ID=303
# Die beiden, die fallen. VMID:Adresse:Name, ueber die MAC-Adresse gebunden
# (ip -o link im Gast gegen qm config auf dem Wirt), nicht gerechnet.
FALLEN="308:10.2.3.217:db-2 309:10.2.3.218:db-3"

erlaubt() { case "$1" in 301|302|303|304|305|306|308|309) return 0;; *) return 1;; esac; }
for e in $FALLEN; do
  erlaubt "${e%%:*}" || { echo "ABBRUCH: VMID ${e%%:*} steht nicht auf der Liste"; exit 2; }
done

q() { $SSH "$PVE" "qm $*"; }
st() { q status "$1" | sed 's/^status: //'; }
lebt() { ping -n 1 -w 1500 "$1" >/dev/null 2>&1; }

# ---- Die Anlage, von diesem PC aus. Immer mit --resolve: der DNS dieses
# ---- Netzes liefert fuer jeden *.ngc.mein.lan ein AAAA ::.
seite() {
  curl.exe -sk -o NUL -w "%{http_code} %{time_total}s" --max-time 20 \
    --resolve "ngc.mein.lan:443:$A1" https://ngc.mein.lan/ 2>/dev/null || echo "000 -"
}
# Eine Anmeldeanfrage MIT RUMPF. login/start zaehlt laut Code keinen Fehlversuch.
anm_voll() {
  local body code t out
  out=$(curl.exe -sk -w '\n%{http_code} %{time_total}' --max-time 25 -X POST \
    -H "content-type: application/json" -d '{"name":"weiricth"}' \
    --resolve "ngc.mein.lan:443:$A1" https://ngc.mein.lan/api/v1/login/start 2>/dev/null) \
    || out=$'\n000 -'
  code=$(printf '%s' "$out" | tail -1 | cut -d' ' -f1)
  t=$(printf '%s' "$out" | tail -1 | cut -d' ' -f2)
  body=$(printf '%s' "$out" | sed '$d' | tr -d '\r\n' | cut -c1-220)
  printf '%s in %ss  Rumpf: %s' "$code" "$t" "${body:-(leer)}"
}
anm_code() {
  curl.exe -sk -o NUL -w "%{http_code}" --max-time 25 -X POST \
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
# Die falsche Anmeldung daneben: derselbe Weg mit einem Namen, den es nicht
# gibt. Bei gesunder Datenbank muss das etwas ANDERES ergeben als bei toter -
# sonst sind "Datenbank weg" und "Name falsch" fuer den Benutzer dasselbe.
anm_falsch() {
  local out
  out=$(curl.exe -sk -w '\n%{http_code}' --max-time 25 -X POST \
    -H "content-type: application/json" -d '{"name":"gibtsnicht-xy7"}' \
    --resolve "ngc.mein.lan:443:$A1" https://ngc.mein.lan/api/v1/login/start 2>/dev/null) \
    || out=$'\n000'
  printf '%s  Rumpf: %s' "$(printf '%s' "$out" | tail -1)" \
    "$(printf '%s' "$out" | sed '$d' | tr -d '\r\n' | cut -c1-200)"
}

# ---- Die Datenbank selbst, auf dem Knoten, der steht.
sql() { $SSH "root@$BLEIBT" "mysql -N -B -e \"$1\" 2>&1 | tr '\t' '=' | tr '\n' ' '" 2>&1 | tail -1; }
verbund() {
  sql "show status like 'wsrep_cluster_size'; show status like 'wsrep_cluster_status'; show status like 'wsrep_local_state_comment'; show status like 'wsrep_incoming_addresses';"
}
# Die entscheidende Probe: liest der Knoten ohne Quorum, und schreibt er?
lesen_schreiben() {
  echo "      SELECT 1 (ohne Tabelle) : $(sql 'select 1;')"
  echo "      SELECT aus ngc (echt)   : $(sql 'select count(*) from ngc.ngc_app;')"
  echo "      INSERT in messung       : $(sql 'insert into messung.nachtrag (wann) values (now());')"
  echo "      SELECT aus messung      : $(sql 'select count(*) from messung.nachtrag;')"
}
dbs_aktiv() {
  local out="" a n s
  for e in "$BLEIBT_ID:$BLEIBT:db-1" $FALLEN; do
    a=$(echo "$e" | cut -d: -f2); n=$(echo "$e" | cut -d: -f3)
    s=$($SSH -o ConnectTimeout=5 "root@$a" 'systemctl is-active mariadb' 2>/dev/null || echo "nicht-erreichbar")
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
  echo "    qm status 308/309   : $(st 308) / $(st 309)"
  echo "(a) Oberflaeche         : $(seite)"
  echo "(b) Anmeldung, einzeln  : $(anm_voll)"
  echo "(b) Anmeldung, 40x      : $(vierzig)"
  echo "(b) falscher Name daneben: $(anm_falsch)"
  echo "    Verbund (von db-1)  : $(verbund)"
  echo "    Lesen/Schreiben auf db-1:"
  lesen_schreiben
  echo "    mariadb je Knoten   :$(dbs_aktiv)"
  echo "    Container svc-1/2   : $(container 10.2.3.213) / $(container 10.2.3.214)"
}

echo "===== Fall A: die Mehrheit faellt - db-2 (308) und db-3 (309) zusammen aus ====="
echo "Gemessen von diesem PC aus, $(date -u '+%Y-%m-%d %H:%M:%S') UTC."
echo "db-1 ($BLEIBT, VMID $BLEIBT_ID) bleibt stehen und ist der Zeuge."
echo "db.nodes der APIs:"
for a in $A1 $A2; do
  printf "    %s: " "$a"
  $SSH "root@$a" 'python3 -c "import json;print(json.load(open(\"/etc/ngc/api.json\"))[\"db\"][\"nodes\"])"' 2>&1
done
echo "wsrep_dirty_reads: $(sql "show variables like 'wsrep_dirty_reads';")"
echo
echo ">>> Messtabelle anlegen (eigene Datenbank, nie ngc):"
sql "create database if not exists messung; create table if not exists messung.nachtrag (id int auto_increment primary key, wann datetime);"
echo "    $(sql 'select count(*) from messung.nachtrag;')"
echo
runde "VORHER, alle drei im Verbund"

echo
echo ">>> qm stop 308 und 309, so dicht hintereinander wie moeglich"
VOR=$(date -u '+%Y-%m-%d %H:%M:%S')
q "stop 308" & q "stop 309" & wait
for i in $(seq 1 25); do
  s8=$(st 308); s9=$(st 309)
  if [ "$s8" = stopped ] && [ "$s9" = stopped ] && ! lebt 10.2.3.217 && ! lebt 10.2.3.218; then break; fi
  sleep 2
done
if [ "$(st 308)" != stopped ] || [ "$(st 309)" != stopped ] || lebt 10.2.3.217 || lebt 10.2.3.218; then
  echo "ABBRUCH: 308/309 sind nicht nachweisbar aus (qm sagt '$(st 308)' / '$(st 309)'). Nichts gemessen."
  q "start 308"; q "start 309"; exit 3
fi
echo "    geprueft: qm sagt beide stopped UND .217 und .218 sind stumm."
echo

# Die ersten fuenf Anfragen EINZELN. Hier liegt der Befund, den vierzig
# verdecken - und bei zwei abwesenden Knoten koennen zwei Zeitablaeufe
# hintereinander stehen.
echo "    Die ersten fuenf Anmeldungen, einzeln:"
for i in 1 2 3 4 5; do echo "      $i. $(anm_voll)"; done
echo
echo "    Lesen und Schreiben auf db-1, OHNE Quorum:"
lesen_schreiben
echo
runde "WAEHREND die Mehrheit weg ist"
echo
echo "    Was die APIs sagen:"
for a in $A1 $A2; do
  echo "      --- $a:"
  $SSH "root@$a" "journalctl -u ngc-api --since '$VOR' --no-pager 2>/dev/null | tail -8 | sed 's/^/        /'" 2>&1
done
echo "    Was mariadb auf db-1 sagt:"
$SSH "root@$BLEIBT" "journalctl -u mariadb --since '$VOR' --no-pager 2>/dev/null | grep -iE 'wsrep|quorum|primary|non-prim|view' | tail -10 | sed 's/^/      /'" 2>&1

echo
echo ">>> qm start 308 und 309 - faengt sich die Anlage von selbst?"
AUS=$(date -u '+%Y-%m-%d %H:%M:%S')
q "start 308"; q "start 309"
for i in $(seq 1 60); do lebt 10.2.3.217 && lebt 10.2.3.218 && break; sleep 2; done
echo "    .217 $(lebt 10.2.3.217 && echo antwortet || echo stumm), .218 $(lebt 10.2.3.218 && echo antwortet || echo stumm)"
echo "    Verbund im Halbminutentakt, bis er wieder 3 ist (hoechstens 5 min):"
for i in 1 2 3 4 5 6 7 8 9 10; do
  sleep 30
  v=$(verbund)
  printf "      +%3ds  %s\n" $((i*30)) "$v"
  echo "$v" | grep -q "wsrep_cluster_size=3" && break
done
echo "    mariadb je Knoten:$(dbs_aktiv)"
for e in $FALLEN; do
  a=$(echo "$e" | cut -d: -f2); n=$(echo "$e" | cut -d: -f3)
  echo "    --- $n ($a): NRestarts=$($SSH -o ConnectTimeout=6 "root@$a" 'systemctl show mariadb -p NRestarts --value' 2>&1 | tr -d '\r')"
  $SSH "root@$a" "journalctl -u mariadb --since '$AUS' --no-pager 2>/dev/null | grep -iE 'wsrep|sst|ist |joiner|synced|bootstrap|ERROR' | tail -8 | sed 's/^/        /'" 2>&1
  echo "        grastate.dat: $($SSH -o ConnectTimeout=6 "root@$a" "grep -E 'safe_to_bootstrap|seqno|uuid' /var/lib/mysql/grastate.dat 2>&1 | tr '\n' ' '" 2>&1 | tr -d '\r')"
done
echo
runde "NACHHER, von selbst"
echo "===== Ende Fall A ====="
