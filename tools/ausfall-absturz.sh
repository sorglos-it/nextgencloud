#!/usr/bin/env bash
# Der ABSTURZ, nicht der Stopp. M1, M2, M3 aus docs/ganz-oder-nicht.md.
#
# docs/ausfall-nachtrag.md hat den DIENST-Stopp gemessen (systemctl stop) und
# docs/ausfall-je-maschine.md den MASCHINEN-Stopp (qm stop). Beides ist ein
# GEORDNETES Ende: systemd weiss, dass es beenden soll, und raeumt nach seiner
# KillMode-Regel auf. Der Absturz ist der dritte Fall und nie gefahren worden:
#
#   Dienst-Stopp              | Absturz (kill -9 auf den Hauptprozess)
#   --------------------------|------------------------------------------
#   systemd BEENDET die Unit  | systemd sieht den Hauptprozess wegfallen
#   KillMode greift           | KillMode greift NICHT - es wird nicht beendet
#   Unit bleibt inactive      | Restart=on-failure wirft sie wieder an
#   Dauer waehlt der Messende | Dauer ist RestartSec=5 (gemessen)
#
# WARUM DIE PID-REIHE DER BELEG IST UND NICHT NRestarts. Gemessen am
# 18.10.2026 (Z:\GitHub\claude-md\messen.md): systemd setzt NRestarts auf 0
# zurueck, sobald die Unit wieder sauber laeuft - eine Spalte, die
# NRestarts=0 liest, ist KEIN Beleg, dass nichts neu gestartet ist. Der Beleg
# ist die MainPID je Abtastung: wechselt sie, war ein Neustart.
#
# WAS DIESES SKRIPT AN SICH SELBST PRUEFT, bevor es eine Zahl nennt:
#   - der Dienst lief vorher (sonst gibt es nichts abzustuerzen),
#   - die MainPID vor dem Schlag ist eine Zahl > 1,
#   - nach dem Schlag ist diese PID WEG (sonst hat kill nichts getroffen und
#     jede Zahl danach misst den gesunden Betrieb),
#   - die Maschine antwortet weiter (sonst war es ein Maschinen-Stopp),
#   - am Ende laeuft der Dienst wieder.
# Bei jedem dieser Punkte bricht es ab, statt eine Zahl zu nennen.
#
# ANGESAGT: Fenster 17:10-17:40 UTC am 30.10.2026, der Leitsitzung genannt,
# bevor der erste Schlag fiel. M4 (apache2 auf api-1) ist NICHT freigegeben
# und steht darum nicht in diesem Skript.
set -uo pipefail

# KEIN Schluesselpfad und KEIN abgeschaltetes StrictHostKeyChecking in diesem
# Skript, und beides mit Grund:
#
#   - Ein Pfad in jemandes Dokumentenordner gehoert nicht in ein Repo, und er
#     faellt erst beim Fahren auf, wenn er nicht stimmt. Die Maschinen stehen
#     in der ssh-Konfiguration des Benutzers (`ssh -G ngc-api-1` nennt Adresse,
#     Benutzer und Schluessel), also sucht ssh den Schluessel selbst.
#   - `StrictHostKeyChecking=no` schaltet die Pruefung der Gegenstelle ab. In
#     einem Repo, dessen eigene Regel "TLS zwischen NGC-Teilen nie ungeprueft"
#     lautet, darf kein eingechecktes Werkzeug das Gegenteil vormachen - das
#     naechste Werkzeug wird von diesem abgeschrieben. Wer das Skript zum
#     ersten Mal fahrt, bestaetigt den Fingerabdruck einmal; die Konfiguration
#     dieser Anlage steht ohnehin auf `accept-new`.
#
# Die ssh-NAMEN kommen aus der Konfiguration, die ADRESSEN stehen daneben: sie
# sind fuer curl (--resolve) und ping noetig, die keine ssh-Konfiguration lesen.
SSH="ssh -o ConnectTimeout=8 -o BatchMode=yes"
A1=10.2.3.210
A2=10.2.3.211
SVC1=10.2.3.213
SVC2=10.2.3.214
H_SVC1=ngc-svc-1
H_SVC2=ngc-svc-2
H_A1=ngc-api-1

case "${1:-}" in
  M1) ADDR=$A2;   HOST=ngc-api-2; DIENST=ngc-api;        KNOTEN=api-2; ART=einmal;    ;;
  M2) ADDR=$A2;   HOST=ngc-api-2; DIENST=ngc-api;        KNOTEN=api-2; ART=schleife;  ;;
  M3) ADDR=$SVC1; HOST=$H_SVC1;   DIENST=ngc-controller; KNOTEN=svc-1; ART=einmal;    ;;
  *) echo "Aufruf: $0 M1|M2|M3   (M4 ist nicht freigegeben)"; exit 2;;
esac
# Die Gegenprobe, dass die Konfiguration den Namen wirklich kennt - und sie
# prueft die WIRKUNG, nicht die Form.
#
# Der erste Versuch hier war `ssh -G "$HOST" | grep -q '^hostname '`, und der
# war wertlos: `ssh -G` druckt auch fuer einen erfundenen Namen eine Zeile
# `hostname <derselbe Name>`. Gemessen am 30.10.2026:
#
#   ssh -G ngc-api-2        ->  hostname=10.2.3.211
#   ssh -G gibtsnicht-xyz   ->  hostname=gibtsnicht-xyz
#
# Eine Pruefung, die alles durchlaesst, prueft nichts. Geprueft wird darum
# gegen die Adresse, die dieses Skript ohnehin fuer curl und ping braucht -
# eine Zahl aus einer ZWEITEN Quelle. Das faengt beide Faelle: Name unbekannt,
# und Name zeigt woandershin als die Messung.
AUFGELOEST=$(ssh -G "$HOST" 2>/dev/null | awk '/^hostname /{print $2}')
[ "$AUFGELOEST" = "$ADDR" ] || {
  echo "ABBRUCH: ssh loest '$HOST' auf '${AUFGELOEST:-nichts}' auf, die Messung erwartet '$ADDR'."
  echo "         Entweder fehlt der Host-Eintrag in der ssh-Konfiguration, oder er"
  echo "         zeigt auf eine andere Maschine. Nichts gemessen."
  exit 3
}
echo "    geprueft: ssh '$HOST' -> $AUFGELOEST, und das ist die Adresse der Messung"
SCHLEIFE=${2:-60}

aktiv()  { $SSH -o ConnectTimeout=6 "$1" "systemctl is-active $2" 2>&1 | tr -d '\r'; }
hauptpid() { $SSH -o ConnectTimeout=6 "$1" "systemctl show $2 -p MainPID --value" 2>&1 | tr -d '\r '; }
# Der Grund, den systemd dem Ende gibt. Beim Absturz MUSS hier "signal"
# stehen und nicht "exit-code": das trennt den Schlag vom Austritt.
ergebnis() { $SSH -o ConnectTimeout=6 "$1" "systemctl show $2 -p Result -p NRestarts -p ExecMainStatus -p ExecMainCode --value" 2>&1 | tr -d '\r' | tr '\n' ' '; }

seite() {
  curl.exe -sk -o NUL -w "%{http_code} %{time_total}s" --max-time 20 \
    --resolve "ngc.mein.lan:443:$A1" https://ngc.mein.lan/ 2>/dev/null || echo "000 -"
}
# Dieselbe Anfrage wie in tools/ausfall-mehrheit.sh und tools/ausfall-dienst.sh.
# Sie ist bewusst nicht "verbessert": nur so sind diese Zahlen mit B1 und mit
# dem Maschinen-Stopp vom 24.10. vergleichbar.
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
# Code UND Rumpf in einer Zeile, fuer die Reihen, in denen nur der Code
# gezaehlt wird. Der Anmelde-Strang jagt einen Fehler, bei dem die erste
# Anmeldung "Passwort falsch" meldet, und einer seiner Verdaechtigen ist ein
# API-Knoten, der aus dem Takt ist: welcher der zwei Saetze hier erscheint -
# "Anmeldung fehlgeschlagen." oder "Dienst nicht erreichbar." - ist fuer ihn
# der Befund. Darum wird der Rumpf jeder SCHLECHTEN Antwort mitgeschrieben
# und nicht weggeworfen.
anm_paar() {
  local out code body
  out=$(curl.exe -sk -w '\n%{http_code}' --max-time 25 -X POST \
    -H "content-type: application/json" -d '{"name":"weiricth"}' \
    --resolve "ngc.mein.lan:443:$A1" https://ngc.mein.lan/api/v1/login/start 2>/dev/null) \
    || out=$'\n000'
  code=$(printf '%s' "$out" | tail -1 | tr -d ' \r')
  body=$(printf '%s' "$out" | sed '$d' | tr -d '\r\n' | cut -c1-160)
  printf '%s\t%s' "$code" "${body:-(leer)}"
}
direkt() {
  curl.exe -sk -o NUL -w "%{http_code} %{time_total}s" --max-time 12 \
    "https://$1:7443/api/v1/login/start" 2>/dev/null || echo "000 -"
}
container() {
  local n
  n=$($SSH "$1" 'pgrep -af "^/usr/bin/conmon" 2>/dev/null | grep -oE " -n [a-z0-9-]+" | sed "s/ -n //" | sort | tr "\n" " "' 2>&1)
  echo "${n:-KEINE}"
}
# Die Waisenfrage braucht mehr als die Namen: ein Container, der weiterlaeuft,
# nachdem sein Verwalter starb, hat einen conmon, dessen Elternteil 1 ist.
waisen() {
  $SSH "$1" 'for p in $(pgrep -f "^/usr/bin/conmon" 2>/dev/null); do
      ppid=$(awk "{print \$4}" /proc/$p/stat 2>/dev/null)
      name=$(tr "\0" "\n" < /proc/$p/cmdline 2>/dev/null | grep -A1 "^-n$" | tail -1)
      echo "    conmon pid=$p ppid=$ppid name=${name:-?}"
    done; echo "    (keine Zeile = kein conmon)"' 2>&1 | tr -d '\r'
}

runde() {
  echo "--- $1 ($(date -u '+%H:%M:%S') UTC) ---"
  echo "    $DIENST auf $KNOTEN : $(aktiv "$HOST" "$DIENST")  MainPID=$(hauptpid "$HOST" "$DIENST")"
  echo "    systemd sagt        : $(ergebnis "$HOST" "$DIENST")"
  echo "(a) Oberflaeche         : $(seite)"
  echo "(b) Anmeldung, einzeln  : $(anm_voll)"
  echo "    api-1:7443 direkt   : $(direkt $A1)"
  echo "    api-2:7443 direkt   : $(direkt $A2)"
  if [ "$KNOTEN" = svc-1 ]; then
    echo "    Container svc-1     : $(container $H_SVC1)"
    echo "    Container svc-2     : $(container $H_SVC2)"
    echo "    Elternteil je conmon auf svc-1:"
    waisen $H_SVC1
  fi
}

echo "===== Fall $1: ABSTURZ - kill -9 auf $DIENST ($KNOTEN, $ADDR), Art: $ART ====="
echo "Gemessen von diesem PC aus, $(date -u '+%Y-%m-%d %H:%M:%S') UTC."
echo "Die Maschine bleibt AN. Der Dienst wird NICHT gestoppt, sondern getoetet."
echo "Zum Vergleich: der Dienst-Stopp derselben Rolle in docs/ausfall-nachtrag.md."
echo

# --- Vorrichtungspruefung, vor jeder Zahl ---------------------------------
VORHER=$(aktiv "$HOST" "$DIENST")
[ "$VORHER" = active ] || { echo "ABBRUCH: $DIENST auf $KNOTEN ist schon '$VORHER'. Nichts gemessen."; exit 3; }
PID0=$(hauptpid "$HOST" "$DIENST")
case "$PID0" in ''|0|1|*[!0-9]*) echo "ABBRUCH: MainPID ist '$PID0', keine brauchbare Zahl. Nichts gemessen."; exit 3;; esac
echo "    geprueft: $DIENST laeuft, MainPID=$PID0"
echo
runde "VORHER"
VOR=$(date -u '+%Y-%m-%d %H:%M:%S')

if [ "$ART" = einmal ]; then
  echo
  # Die Uhrzeit auf die Sekunde: ein Mensch kann im selben Fenster selbst im
  # Browser nachsehen, und dann muessen zwei Beobachtungen auseinanderzuhalten
  # sein.
  echo ">>> $(date -u '+%H:%M:%S') UTC  kill -9 $PID0 auf $KNOTEN"
  $SSH "$HOST" "kill -9 $PID0" 2>&1 | tr -d '\r'
  # Die Pruefung, ohne die jede Zahl danach den gesunden Betrieb misst.
  WEG=nein
  for i in $(seq 1 10); do
    if ! $SSH "$HOST" "kill -0 $PID0 2>/dev/null" 2>/dev/null; then WEG=ja; break; fi
    sleep 1
  done
  [ "$WEG" = ja ] || { echo "ABBRUCH: PID $PID0 lebt noch. kill hat nichts getroffen, nichts gemessen."; exit 3; }
  echo "    geprueft: PID $PID0 ist weg"
  echo "    ping $ADDR: $(ping -n 1 -w 1500 "$ADDR" >/dev/null 2>&1 && echo "antwortet (Maschine lebt)" || echo "STUMM - das waere ein Maschinen-Stopp")"
  echo
  echo "    Die ersten fuenf Anmeldungen, einzeln, ab sofort:"
  for i in 1 2 3 4 5; do echo "      $i. $(anm_voll)"; done
  echo
  echo "    MainPID im Sekundentakt - das ist der Beleg fuer den Neustart,"
  echo "    nicht NRestarts (das setzt systemd zurueck):"
  for i in $(seq 1 20); do
    printf "      +%2ds  is-active=%-10s MainPID=%s\n" "$i" "$(aktiv "$HOST" "$DIENST")" "$(hauptpid "$HOST" "$DIENST")"
    sleep 1
  done
  echo
  runde "NACH DEM ABSTURZ"
else
  # --- M2: Dauerschleife -------------------------------------------------
  # Der Schlag faellt auf dem Knoten selbst, aus einer DATEI, mit harter
  # Frist: eine Schleife, die ich von hier aus im Befehlstext zusammensetze,
  # haengt an der Anfuehrungszeichen-Aufloesung zweier Schalen (messen.md).
  # Die Frist steht IM Skript und nicht nur im Aufruf, damit ein verlorener
  # ssh-Klient die Schleife nicht erbt.
  TMP=$(mktemp)
  cat > "$TMP" <<'SKRIPT'
#!/bin/sh
# Toetet ngc-api alle 2 s, hoechstens DAUER Sekunden, und schreibt jede
# Abtastung sofort auf die Platte - eine Langzeitmessung, die ihre Reihe erst
# am Ende schreibt, verliert sie beim Abbruch.
DAUER=$1
ENDE=$(( $(date +%s) + DAUER ))
: > /run/ngc-absturz-pids.txt
while [ "$(date +%s)" -lt "$ENDE" ]; do
  P=$(systemctl show ngc-api -p MainPID --value)
  echo "$(date -u +%H:%M:%S) pid=$P" >> /run/ngc-absturz-pids.txt
  case "$P" in ''|0) : ;; *) kill -9 "$P" 2>/dev/null ;; esac
  sleep 2
done
echo "$(date -u +%H:%M:%S) ENDE" >> /run/ngc-absturz-pids.txt
SKRIPT
  scp -q "$TMP" "$HOST:/run/ngc-absturz.sh"
  rm -f "$TMP"
  $SSH "$HOST" "chmod 0700 /run/ngc-absturz.sh" 2>&1 | tr -d '\r'
  # Die Gegenprobe, dass die Datei wirklich angekommen ist: ohne sie messe
  # ich eine Schleife, die nie lief, und bekomme "Riegel greift" geschenkt.
  Z=$($SSH "$HOST" "wc -l < /run/ngc-absturz.sh" 2>&1 | tr -d '\r ')
  case "$Z" in ''|0|*[!0-9]*) echo "ABBRUCH: /run/ngc-absturz.sh ist nicht angekommen ('$Z' Zeilen)."; exit 3;; esac
  echo "    geprueft: Schlagskript liegt auf $KNOTEN, $Z Zeilen"
  echo
  echo ">>> $(date -u '+%H:%M:%S') UTC  Dauerschleife: kill -9 auf ngc-api alle 2 s, ${SCHLEIFE}s lang"
  # BERICHTIGT am 30.10.2026, nachdem der erste Lauf NICHTS gemessen hat.
  # Vorher stand hier "setsid timeout … &" innerhalb des ssh-Befehls. Der
  # ssh-Klient endete mit dem Befehl, und die Schleife ging mit ihm - genau
  # die Falle "ein Halteprozess aus dem Bash-Werkzeug haelt die Gegenseite
  # nicht offen" aus messen.md, nur mit ssh statt wsl. Der Lauf meldete
  # dabei 40/40 gute Anmeldungen und sah wie ein Ergebnis aus ("der Riegel
  # greift nicht, nichts passiert"), war aber eine Messung des GESUNDEN
  # Betriebs. Jetzt bleibt der Klient fuer die ganze Dauer am Leben, weil er
  # im Vordergrund derselben Schale laeuft, die auch die Anmeldungen macht.
  $SSH "$HOST" "timeout $((SCHLEIFE+30)) sh /run/ngc-absturz.sh $SCHLEIFE" >/dev/null 2>&1 &
  SCHLAEGER=$!
  sleep 3
  # Und die Pruefung, die beim ersten Lauf gefehlt hat: nicht, dass die DATEI
  # angekommen ist, sondern dass die SCHLEIFE LAEUFT. Eine angekommene Datei
  # ist keine laufende Schleife.
  L=$($SSH "$HOST" "wc -l < /run/ngc-absturz-pids.txt 2>/dev/null" 2>&1 | tr -d '\r ')
  case "$L" in ''|0|*[!0-9]*) kill "$SCHLAEGER" 2>/dev/null; echo "ABBRUCH: die Schleife schreibt nicht ('$L'). Sie laeuft nicht, nichts gemessen."; exit 3;; esac
  echo "    geprueft: die Schleife laeuft und schreibt ($L Zeilen nach 3 s)"
  echo
  echo "    Anmeldungen im Takt, solange die Schleife laeuft."
  echo "    Gezaehlt wird, WAS EIN NUTZER DABEI SIEHT - nicht nur, ob der"
  echo "    Riegel greift. Die Zahl gehoert ins Blatt."
  OK=0; BAD=0; CODES=""; i=0
  SAETZE=$(mktemp)
  ENDE=$(( $(date +%s) + SCHLEIFE ))
  while [ "$(date +%s)" -lt "$ENDE" ]; do
    i=$((i+1))
    p=$(anm_paar)
    c=${p%%	*}
    if [ "$c" = 200 ]; then
      OK=$((OK+1))
    else
      BAD=$((BAD+1)); CODES="$CODES $c"
      printf '%s  %s  %s\n' "$(date -u '+%H:%M:%S')" "$c" "${p#*	}" >> "$SAETZE"
    fi
  done
  echo "      $OK gut von $i, $BAD schlecht:${CODES:- -}"
  echo
  echo "    JEDE schlechte Antwort mit Uhrzeit und Wortlaut. Welcher der zwei"
  echo "    Saetze hier steht, ist der Befund fuer den Anmelde-Strang:"
  if [ -s "$SAETZE" ]; then sed 's/^/      /' "$SAETZE"; else echo "      (keine einzige schlechte Antwort)"; fi
  echo "    Wortlaute, gezaehlt:"
  if [ -s "$SAETZE" ]; then
    sed 's/^[0-9:]*  //' "$SAETZE" | sort | uniq -c | sort -rn | sed 's/^/      /'
  else echo "      -"; fi
  rm -f "$SAETZE"
  echo
  echo "    Ein voller Rumpf aus der Schleife, damit der Wortlaut im Blatt steht:"
  echo "      $(anm_voll)"
  echo
  wait "$SCHLAEGER" 2>/dev/null
  echo "    Die PID-Reihe des Knotens (so viele Neustarts hat es wirklich gegeben):"
  REIHE=$($SSH "$HOST" "cat /run/ngc-absturz-pids.txt" 2>&1 | tr -d '\r')
  printf '%s\n' "$REIHE" | sed 's/^/      /'
  # Die Gegenprobe gegen den eigenen Lauf: hat sich die PID ueberhaupt
  # bewegt? Eine Reihe aus einer einzigen PID heisst, dass kein Schlag
  # getroffen hat, und dann ist jede Zahl oben eine Zahl ueber den gesunden
  # Betrieb - so ist der erste M2-Lauf gescheitert.
  VERSCH=$(printf '%s\n' "$REIHE" | grep -o 'pid=[0-9]*' | sort -u | grep -vc 'pid=0$')
  echo "    verschiedene MainPIDs in der Reihe (ohne 0): $VERSCH"
  [ "${VERSCH:-0}" -ge 2 ] || echo "    ACHTUNG: weniger als zwei verschiedene PIDs - die Schleife hat nicht getroffen, die Zahlen oben gelten NICHT."
  echo
  echo "    Hat der Riegel gegriffen? Das sagt diese Zeile, nicht meine Rechnung:"
  $SSH "$HOST" "systemctl show ngc-api -p Result -p NRestarts -p ActiveState -p StartLimitBurst -p StartLimitIntervalUSec" 2>&1 | tr -d '\r' | sed 's/^/      /'
  echo "    Und das Journal dazu:"
  $SSH "$HOST" "journalctl -u ngc-api --since '$VOR' --no-pager 2>/dev/null | grep -iE 'start request repeated|rate limit|failed|killed|signal|Scheduled restart' | tail -12 | sed 's/^/      /'" 2>&1
  $SSH "$HOST" "rm -f /run/ngc-absturz.sh" 2>&1 | tr -d '\r'
fi

# --- Wieder anwerfen, und nachweisen, dass es gelungen ist ----------------
echo
echo ">>> aufraeumen: $DIENST auf $KNOTEN muss wieder laufen"
for i in $(seq 1 30); do
  [ "$(aktiv "$HOST" "$DIENST")" = active ] && break
  $SSH "$HOST" "systemctl reset-failed $DIENST 2>/dev/null; systemctl start $DIENST" 2>&1 | tr -d '\r'
  sleep 3
done
ENDSTAND=$(aktiv "$HOST" "$DIENST")
echo "    $DIENST sagt jetzt: $ENDSTAND  MainPID=$(hauptpid "$HOST" "$DIENST")"
[ "$ENDSTAND" = active ] || echo "    ACHTUNG: $DIENST laeuft NICHT. Das ist von Hand nachzuholen."

echo
echo "    Was die beiden APIs im Journal sagen:"
for a in $H_A1 ngc-api-2; do
  echo "      --- $a:"
  $SSH "$a" "journalctl -u ngc-api --since '$VOR' --no-pager 2>/dev/null | tail -8 | sed 's/^/        /'" 2>&1
done
if [ "$KNOTEN" = svc-1 ]; then
  echo "      --- Controller svc-1:"
  $SSH "$H_SVC1" "journalctl -u ngc-controller --since '$VOR' --no-pager 2>/dev/null | tail -12 | sed 's/^/        /'" 2>&1
fi
echo
sleep 15
runde "NACHHER"
echo "===== Ende Fall $1 ====="
