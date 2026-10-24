#!/usr/bin/env bash
# Was macht eine KATALOGRUNDE, die auf einen toten Laden trifft?
#
# Der erste Ladenfall beantwortet das nicht: er war 40 s lang, und in dieser
# Zeit war keine Runde faellig - store.global stand nachher unveraendert auf
# {"checked":1792845105} = 12:31:45 UTC, elf Minuten VOR dem Ausfall.
#
# Der Takt steht in server.go: FirstCatalog = time.Minute, danach stuendlich.
# Ein Neustart von ngc-api holt die erste Runde also in einer Minute. Gemessen
# wird auf api-2, weil api-1 der einzige Eingang der Anlage ist.
#
# Das Skript prueft seine eigene Vorbereitung an drei Stellen und bricht ab,
# statt eine Zahl zu nennen: der Laden muss nachweisbar aus sein, ngc-api muss
# eine NEUE PID haben, und die Ladenzeile muss sich ueberhaupt bewegt haben.
set -uo pipefail

PVE=root@10.2.3.2
K=$HOME/Documents/ssh/keys/id_ed25519_ngc
SSH="ssh -i $K -o StrictHostKeyChecking=no -o ConnectTimeout=8 -o BatchMode=yes"
API2=10.2.3.211
DB=10.2.3.212
VMID=306
ADDR=10.2.3.215

zeile() { $SSH "root@$DB" 'mysql ngc -N -e "select data from ngc_blob where kind=\"setting\" and id=\"store.global\";"' 2>&1; }
katalog() { $SSH "root@$DB" 'mysql ngc -N -e "select seq,length(data) from ngc_blob where kind=\"catalog\";"' 2>&1; }
apps() { $SSH "root@$DB" 'mysql ngc -N -e "select id,node,active from ngc_app;"' 2>&1 | tr '\n' ' '; }
vertrauen() { $SSH "root@$DB" 'mysql ngc -N -e "select max(seq) from ngc_blob where kind=\"trust\";"' 2>&1; }
pid() { $SSH "root@$API2" 'systemctl show -p MainPID --value ngc-api' 2>&1; }
lebt() { ping -n 1 -w 1500 "$ADDR" >/dev/null 2>&1; }
stand() {
  echo "    Ladenzeile  : $(zeile)"
  echo "    Katalog     : $(katalog)"
  echo "    Vertrauen   : Folge $(vertrauen)"
  echo "    Apps        : $(apps)"
}

# Eine Runde erzwingen und warten, bis die Ladenzeile sich bewegt hat.
runde_erzwingen() {
  local vorher=$1 altpid neupid
  altpid=$(pid)
  $SSH "root@$API2" 'systemctl restart ngc-api' 2>&1
  sleep 3
  neupid=$(pid)
  if [ -z "$neupid" ] || [ "$neupid" = "0" ] || [ "$neupid" = "$altpid" ]; then
    echo "    ABBRUCH: ngc-api auf api-2 hat keine neue PID (alt=$altpid neu=$neupid)."
    return 1
  fi
  echo "    ngc-api auf api-2 neu gestartet, PID $altpid -> $neupid"
  echo "    warte auf die Runde (FirstCatalog = 1 min)..."
  for i in $(seq 1 24); do
    sleep 10
    [ "$(zeile)" != "$vorher" ] && { echo "    Ladenzeile hat sich nach ~$((i*10)) s bewegt."; return 0; }
  done
  echo "    ABBRUCH: die Ladenzeile hat sich in 240 s NICHT bewegt - die Runde ist nicht nachweisbar gelaufen."
  return 2
}

echo "=========== Katalogrunde gegen einen toten Laden ==========="
echo "Gemessen $(date -u '+%Y-%m-%d %H:%M:%S') UTC, von diesem PC aus gesteuert."
echo
echo "--- VORHER ---"
stand
VOR=$(zeile)

echo
echo ">>> qm stop $VMID"
$SSH "$PVE" "qm stop $VMID"
for i in $(seq 1 20); do
  s=$($SSH "$PVE" "qm status $VMID" | sed 's/^status: //')
  lebt && p=an || p=stumm
  [ "$s" = stopped ] && [ "$p" = stumm ] && break
  sleep 2
done
s=$($SSH "$PVE" "qm status $VMID" | sed 's/^status: //')
if [ "$s" != stopped ] || lebt; then
  echo "ABBRUCH: der Laden ist nicht nachweisbar aus (qm sagt '$s'). Nichts gemessen."
  $SSH "$PVE" "qm start $VMID"; exit 3
fi
echo "    geprueft: qm sagt stopped UND $ADDR ist stumm."
UTC_AUS=$(date -u '+%Y-%m-%d %H:%M:%S')

echo
echo "--- RUNDE GEGEN DEN TOTEN LADEN ---"
if runde_erzwingen "$VOR"; then
  stand
else
  echo "    (kein Ergebnis fuer diese Runde)"
fi
echo "    Journal api-2 seit dem Abschalten:"
$SSH "root@$API2" "journalctl -u ngc-api --since '$UTC_AUS' --no-pager | tail -30" 2>&1
echo "    -- gezielt nach der Meldung gesucht, die es laut Code geben koennte:"
$SSH "root@$API2" "journalctl -u ngc-api --since '$UTC_AUS' --no-pager | grep -icE 'catalog|store|laden' || echo 0" 2>&1

echo
echo ">>> qm start $VMID"
$SSH "$PVE" "qm start $VMID"
for i in $(seq 1 45); do lebt && break; sleep 2; done
lebt && echo "    $ADDR antwortet wieder" || echo "    $ADDR antwortet NICHT"
sleep 10
echo
echo "--- FAENGT SICH DIE ZEILE VON SELBST? (zweite erzwungene Runde) ---"
NACH=$(zeile)
if runde_erzwingen "$NACH"; then
  stand
else
  echo "    (kein Ergebnis fuer diese Runde)"
fi
echo "=========== Ende ==========="
