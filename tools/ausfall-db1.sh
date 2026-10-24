#!/usr/bin/env bash
# db-1 aus. Die einzige Datenbank der Anlage, ein einzelner Galera-Knoten
# (wsrep_cluster_size=1), auf den BEIDE APIs als einzigen DB-Knoten zeigen.
#
# Zwei Fragen:
#  1. Was sieht der Benutzer? Die Oberflaeche sollte kommen (Apache und PHP
#     liegen auf api-1), die Anmeldung nicht. Im Code ist der erste
#     DB-Zugriff in login/start ein lock.Check, und sein Fehler wird als
#     E_UNREACHABLE beantwortet - wofuer die Oberflaeche einen eigenen Satz
#     hat ("Service unavailable"), nicht den Satz "Login failed". Wenn das
#     haelt, ist die Anlage ehrlicher als erwartet.
#  2. Kommt db-1 von SELBST zurueck? Erwartet: nein, ein einzelner
#     Galera-Knoten findet kein Quorum. HIER WIRD NICHT VON HAND GEHOLFEN,
#     bevor gemessen ist, wie weit er allein kommt.
#
# login/start zaehlt laut Code keinen Fehlversuch (nur login/finish tut das),
# die Sonde sperrt also niemanden aus. Eine logische Sicherung der Datenbank
# liegt vor dem Lauf im Sitzungsordner, geprueft auf Brauchbarkeit.
set -uo pipefail

PVE=root@10.2.3.2
K=$HOME/Documents/ssh/keys/id_ed25519_ngc
SSH="ssh -i $K -o StrictHostKeyChecking=no -o ConnectTimeout=8 -o BatchMode=yes"
VMID=303
DB=10.2.3.212
A1=10.2.3.210
A2=10.2.3.211

erlaubt() { case "$1" in 301|302|303|304|305|306|308|309) return 0;; *) return 1;; esac; }
erlaubt "$VMID" || { echo "ABBRUCH: VMID $VMID nicht auf der Liste"; exit 2; }

q() { $SSH "$PVE" "qm $*"; }
st() { q status "$VMID" | sed 's/^status: //'; }
lebt() { ping -n 1 -w 1500 "$DB" >/dev/null 2>&1; }

seite() {
  curl.exe -sk -o NUL -w "%{http_code}" --max-time 15 --resolve "ngc.mein.lan:443:$A1" \
    https://ngc.mein.lan/ 2>/dev/null || echo 000
}
# Die eigentliche Frage: was antwortet die Anmeldung?
anmelden() { # ziel (host:port oder name)
  curl.exe -sk -w "|code=%{http_code}" --max-time 15 -X POST \
    -H "content-type: application/json" -d '{"name":"weiricth"}' "$1" 2>/dev/null | head -c 220
}
galera() {
  $SSH "root@$DB" 'systemctl is-active mariadb 2>/dev/null | tr "\n" " "; mysql -N -e "show status like \"wsrep_cluster_size\"; show status like \"wsrep_cluster_status\";" 2>&1 | tr "\n" " "' 2>&1
}
apps_laufen() {
  local n
  n=$($SSH "root@$1" 'pgrep -af "^/usr/bin/conmon" 2>/dev/null | grep -oE " -n [a-z0-9-]+" | sed "s/ -n //" | sort | tr "\n" " "' 2>&1)
  echo "${n:-KEINE}"
}

runde() {
  echo "--- $1 ($(date -u '+%H:%M:%S') UTC) ---"
  echo "    qm status db-1    : $(st)"
  echo "(a) Oberflaeche       : $(seite)   (voller Betrieb: 200)"
  echo "(b) Anmeldung ueber den Namen:"
  echo "        $(anmelden https://ngc.mein.lan/api/v1/login/start 2>/dev/null)" 2>/dev/null
  echo "(b) Anmeldung direkt auf api-2:7443:"
  echo "        $(anmelden https://$A2:7443/api/v1/login/start)"
  echo "    Galera auf db-1   : $(galera)"
  echo "    Apps svc-1/svc-2  : $(apps_laufen 10.2.3.213) / $(apps_laufen 10.2.3.214)"
}

# anmelden ueber den Namen braucht --resolve; eigene Fassung dafuer
anmelden_name() {
  curl.exe -sk -w "|code=%{http_code}" --max-time 15 -X POST \
    -H "content-type: application/json" -d '{"name":"weiricth"}' \
    --resolve "ngc.mein.lan:443:$A1" https://ngc.mein.lan/api/v1/login/start 2>/dev/null | head -c 220 || echo "|code=000"
}

runde2() {
  echo "--- $1 ($(date -u '+%H:%M:%S') UTC) ---"
  echo "    qm status db-1    : $(st)"
  echo "(a) Oberflaeche       : $(seite)   (voller Betrieb: 200)"
  echo "(b) Anmeldung, ueber den Namen durch den Lastverteiler:"
  echo "        $(anmelden_name)"
  echo "(b) Anmeldung, direkt auf api-2:7443:"
  echo "        $(anmelden https://$A2:7443/api/v1/login/start)"
  echo "    Galera auf db-1   : $(galera)"
  echo "    Apps svc-1/svc-2  : $(apps_laufen 10.2.3.213) / $(apps_laufen 10.2.3.214)"
}

echo "=========== Fall: db-1 aus (VMID 303, $DB) ==========="
echo "Gemessen von diesem PC aus, $(date -u '+%Y-%m-%d %H:%M:%S') UTC."
echo
runde2 "VORHER"
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
  echo "ABBRUCH: db-1 ist nicht nachweisbar aus (qm sagt '$(st)'). Nichts gemessen."
  q "start $VMID"; exit 3
fi
echo "    geprueft: qm sagt stopped UND $DB ist stumm."
echo

sleep 15
runde2 "WAEHREND db-1 aus ist"
echo
echo "    Was die APIs dazu ins Journal schreiben:"
for a in $A1 $A2; do
  echo "      --- $a:"
  $SSH "root@$a" "journalctl -u ngc-api --since '$VOR' --no-pager 2>/dev/null | tail -8 | sed 's/^/        /'" 2>&1
done
echo "    Was die Controller sagen:"
$SSH "root@10.2.3.213" "journalctl -u ngc-controller --since '$VOR' --no-pager 2>/dev/null | tail -4 | sed 's/^/        /'" 2>&1

echo
echo ">>> qm start $VMID   -- und jetzt die eigentliche Frage: kommt er ALLEIN?"
echo "    (es wird NICHT von Hand geholfen, bevor das gemessen ist)"
AUS=$(date -u '+%Y-%m-%d %H:%M:%S')
q "start $VMID"
for i in $(seq 1 60); do lebt && break; sleep 2; done
lebt && echo "    $DB antwortet nach dem Hochfahren" || echo "    $DB antwortet NICHT"
echo "    Zustand von mariadb im Minutentakt, drei Minuten lang:"
for i in 1 2 3 4 5 6; do
  sleep 30
  printf "      +%2ds  %s\n" $((i*30)) "$(galera)"
done
echo
echo "    Warum, im Journal von mariadb:"
$SSH "root@$DB" "journalctl -u mariadb --since '$AUS' --no-pager 2>/dev/null | grep -iE 'wsrep|grastate|quorum|bootstrap|safe_to_bootstrap|abort|error' | tail -15 | sed 's/^/        /'" 2>&1
echo "    grastate.dat:"
$SSH "root@$DB" "cat /var/lib/mysql/grastate.dat 2>&1 | sed 's/^/        /'" 2>&1
echo
runde2 "NACHHER, ohne jede Hilfe von Hand"
echo "=========== Ende db-1 (Teil 1: ohne Nachhelfen) ==========="
