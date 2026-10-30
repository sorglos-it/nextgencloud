# Ganz oder nicht: was NGC tut, wenn ein Teil wegbricht

Die Frage dieses Blatts ist eine einzige: **ein halb laufender Server ist
schlimmer als ein ganz stehender, weil niemand merkt, dass die Hälfte fehlt.**
Gilt das für NGC, und wenn ja, wo?

Die Anregung kam von einem fremden Server, dessen Regel lautet „stirbt ein
Prozess, bringt der Eltern­prozess die anderen um". Von dort ist **nichts**
übernommen — kein Code, kein Aufbau, keine Durchsatz­technik. Übernommen ist
die Frage.

## Was an diesem Blatt neu ist, und was schon gemessen war

Zwei Blätter messen den Ausfall einzelner Teile schon:

- `docs/ausfall-je-maschine.md` — 24.10.2026, sechs **Maschinen**-Stopps
  (`qm stop`), je einer.
- `docs/ausfall-nachtrag.md` — 25.10.2026, zwei Datenbankknoten gleichzeitig
  (Fall A) und fünf **Dienst**-Stopps (`systemctl stop`, Fall B).

Die Frage (a) des Auftrags — *sieht die Oberfläche heil aus und tut nur die
Hälfte?* — ist damit für den **geordneten** Stopp beantwortet, und die Antwort
steht unten in „Was die zwei Blätter schon sagen". Was **nicht** gemessen war:

1. **Die echten Unit-Dateien der Anlage** (Frage b). Kein Blatt hat sie
   gelesen. Dieses tut es, und der Befund ist der wichtigste des Blatts.
2. **Der Absturz** — nicht `systemctl stop`, sondern ein Prozess, der stirbt
   (`SIGKILL`, `SIGSEGV`, OOM, Austritt mit Code 0 nach einem Fehler). Das ist
   der Fall, den die Herkunfts­idee meint, und er ist in keinem Blatt gefahren.
3. **Die Waisen** (Frage c) nach einem **Absturz** des Controllers.
   Gemessen ist nur der geordnete Stopp.

## Gemessen: die Unit-Dateien der Anlage (30.10.2026)

Gelesen wurde auf jedem der sechs Knoten, nicht die Vorlage im Repo:
`systemctl cat <unit>` und `systemctl show <unit> -p …` über eigenen
SSH-Zugang. Reines Lesen; an der Anlage wurde dafür nichts geschaltet.

| Knoten | Unit | `Restart=` | `RestartSec=` | `KillMode=` | `Requires=` auf einen NGC-Nachbarn | `BindsTo=` | `PartOf=` |
|---|---|---|---|---|---|---|---|
| api-1 | `ngc-api` | `on-failure` | 5 s | `control-group` | **keiner** | leer | leer |
| api-1 | `ngc-controller` | `on-failure` | 5 s | `control-group` | **keiner** | leer | leer |
| api-1 | `apache2` | **`on-abort`** | – | `mixed` | **keiner** | leer | leer |
| api-2 | `ngc-api` | `on-failure` | 5 s | `control-group` | **keiner** | leer | leer |
| db-1 | `ngc-controller` | `on-failure` | 5 s | `control-group` | **keiner** | leer | leer |
| db-1 | `mariadb` | **`on-abnormal`** | 5 s | `control-group`, `SendSIGKILL=no` | **keiner** | leer | leer |
| svc-1 | `ngc-controller` | `on-failure` | 5 s | `control-group` | **keiner** | leer | leer |
| svc-2 | `ngc-controller` | `on-failure` | 5 s | `control-group` | **keiner** | leer | leer |
| store | `nginx` | **`no`** | – | `mixed` | **keiner** | leer | leer |

`Requires=` enthält bei jeder NGC-Unit genau `sysinit.target`, `system.slice`
und `-.mount` — das sind die drei, die systemd selbst hinzufügt. `After=` nennt
bei allen NGC-Units nur `network-online.target` und ebenfalls Selbst­verständliches.
Alle acht Units: `StartLimitIntervalUSec=10s`, `StartLimitBurst=5`.

### Was daraus folgt, und was nicht

**Befund 1: zwischen den NGC-Teilen besteht keine einzige Kopplung.** Kein
`BindsTo=`, kein `PartOf=`, kein `Requires=` auf einen Nachbarn, kein
`After=mariadb`. Der halb laufende Server ist in dieser Anlage nicht ein
Unfall, sondern die **Vorgabe**: jeder Dienst lebt und stirbt für sich. Das ist
genau das Gegenteil der Herkunfts­idee, und es ist eine Entscheidung, die so
nie aufgeschrieben wurde — sie steht nur in der Abwesenheit von vier Zeilen.

**Befund 2: drei Units kommen nach einem Absturz nicht zurück.**
`apache2` hat `Restart=on-abort` (nur bei einem Abbruch­signal, nicht bei einem
Austritt mit Fehlercode und nicht beim OOM-Killer), `nginx` hat `Restart=no`,
`mariadb` hat `on-abnormal`. Der Eingang der ganzen Anlage — der einzige Apache
auf api-1 — ist damit der Dienst mit der schwächsten Neustart­regel.

**Befund 3: `KillMode=control-group` beim Controller erklärte einen fremden
Befund — und gilt seit dem 30.10.2026, 14:08 UTC nur noch für die alte
Fassung.** `docs/ausfall-nachtrag.md`, Fall B4, hat gemessen, dass
`systemctl stop ngc-controller` die App-Container **mitnimmt**, und hat die
Ursache offen gelassen. Sie war: die Container lagen in der cgroup des
Controllers, und `control-group` bringt beim Stopp alles darin um.

**Berichtigung, noch am Tag des Schreibens.** Der Strang „Die Härtung wirklich
durchsetzen" hat an der Anlage gemessen, dass svc-1 und svc-2 seit heute
**14:08 UTC** auf `ngc-controller` **0.3.0** laufen (Buchpunkt 96), und dass
die App-Container dort **nicht mehr** in der cgroup der Einheit sitzen, sondern
in `/sys/fs/cgroup/ngc.apps/<app>`. Die alte Fassung liegt je Knoten als
`/usr/lib/ngc/ngc-controller.vor-haertung`.

Damit ist Fall B4 eine Messung an einer Anlage, **die es nicht mehr gibt** —
genau die Falle „welche Datei messe ich". Mein Satz oben stand eine Fassung zu
spät da, und er stand hier schon als Erklärung, bevor ich die Fassung geprüft
hatte.

**Was daran gemessen ist und was nicht:** dass die Container außerhalb der
cgroup der Einheit liegen, ist gemessen (von jenem Strang). Dass sie deshalb
einen `kill -9` **überleben**, ist **nur gefolgert** — niemand hat es
nachgeprüft. Das ist jetzt M3.

**Was NICHT folgt:** ob der Absturz sich wie der Stopp verhält. `KillMode`
beschreibt, was systemd beim **Beenden** tut; stirbt der Controller von selbst,
beendet systemd ihn nicht, sondern startet ihn neu. Ob die Container das
überleben, ist damit **nicht** gemessen, nur die Frage ist jetzt scharf.

### Gemessen: NGC hat keine Stelle, die „mir fehlt ein Teil" sagt

Gezählt über den Zweig `ausfallkopplung` = `origin/main` 25b8f24e, über alle
`*.go` und `*.js` des Baums einschließlich `vendor/`:
`healthz`, `readyz`, `"/health`, `degraded`, `Degraded` — **null Treffer**.
Die Grundmenge ist geprüft: derselbe Lauf findet `E_UNREACHABLE` in **50**
Dateien, der Baum ist also nicht leer.

Es gibt damit **keinen Punkt in NGC, der die Anlage als Ganzes beurteilt.**
Jede Antwort beurteilt den Weg, den die einzelne Anfrage genommen hat. Das ist
der Grund, warum die Blätter vom 24./25.10. für vier von fünf Dienst-Stopps
„40/40 Anmeldungen gut" messen konnten, während ein Teil weg war.

## Was die zwei Blätter schon sagen (Frage a, geordneter Stopp)

Aus `docs/ausfall-nachtrag.md`, Fall B — fünf Dienst-Stopps, je 180 s:

| Dienst gestoppt | Anmeldungen gut | sagt die Anlage, dass etwas fehlt? |
|---|---|---|
| `ngc-api` auf api-2 | 34/40, 6× `HTTP 500` | **nein** — nackte Apache-Seite „500 Proxy Error", und **keine Zeile** in `error.log` |
| `ngc-api` auf api-1 | 40/40 | nein — und es fehlt auch nichts, api-2 trägt |
| `mariadb` auf db-1 | 40/40 | nein — und es fehlt nichts, zwei Knoten halten |
| `ngc-controller` auf svc-1 | 40/40 | **nein — und es fehlte etwas: alle Container waren weg** |
| `nginx` auf store | 40/40 | nein — der Laden ist entbehrlich |

**Die Antwort auf die Kernfrage des Auftrags lautet damit: ja, der Fall
existiert, und er ist gemessen.** Zwei Zeilen tragen sie:

- **svc-1:** der Controller war aus, **alle** App-Container waren weg — und die
  Anlage meldete 40 von 40 guten Anmeldungen und eine Oberfläche mit `200`.
  Niemand hätte es gemerkt, der die Oberfläche ansieht. Ob die Oberfläche die
  Uhr in diesen drei Minuten als „läuft" anzeigte, ist dort ausdrücklich als
  **nicht gemessen** vermerkt; das ist die gefährliche Hälfte und bleibt offen.
- **api-2:** 6 von 40 Anmeldungen endeten in einer Apache-Seite, die nicht von
  NGC stammt, über drei Minuten verstreut — und **kein** Protokoll hat eine
  Zeile geschrieben. Der Benutzer sieht einen Fehler, den niemand mitschreibt.

Und die Gegenseite, die der Vollständigkeit wegen dazugehört: in drei von fünf
Fällen war „sieht heil aus" die **richtige** Auskunft, weil wirklich nichts
fehlte. Eine Regel, die in diesen drei Fällen die Anlage abgeräumt hätte, wäre
schlechter als der heutige Zustand.

## Erwartung für die noch offene Messung — geschrieben vor dem ersten Absturz

Hier steht, was ich erwarte, **bevor** der erste Prozess getötet ist. Die
Messspalten kommen in eigenen Ständen danach. Was noch zu messen ist:

### M1 — `SIGKILL` auf `ngc-api` (api-2), Maschine läuft

Erwartung: systemd startet neu (`Restart=on-failure` deckt ein Signal), nach
`RestartSec=5` s. In diesen fünf Sekunden erwarte ich **dasselbe Bild wie beim
Dienst-Stopp B1** — erste Anfrage schnell, einzelne `HTTP 500` aus dem
Lastverteiler —, danach Ruhe. Der Unterschied zum Stopp ist also nur die Dauer.

### M2 — `SIGKILL` in Dauerschleife auf `ngc-api` (api-2), 60 s

Das ist der Fall, den die Herkunfts­idee meint. Die Rechnung aus den Zahlen
oben: `RestartSec=5` setzt die Starts auf 0, 5, 10, 15 … s; in jedes Fenster
von `StartLimitIntervalUSec=10s` fallen damit höchstens **3** Starts, und
`StartLimitBurst=5` wird **nie** erreicht.

**Erwartung: der Riegel greift nicht.** Ein Dienst, der immer wieder abstürzt,
startet unbegrenzt weiter — alle fünf Sekunden —, und der Lastverteiler holt
ihn mit `retry=5` jedes Mal wieder in die Reihe. Nach außen erwarte ich eine
Anlage, die **dauerhaft** jede zweite Anfrage verliert, ohne dass eine Stelle
„dieser Dienst ist kaputt" sagt. Das ist die Rechnung; gemessen ist sie nicht,
und wenn sie falsch ist, ist das der wertvollere Befund.

### M3 — `SIGKILL` auf `ngc-controller` (svc-1), Maschine läuft

Die Frage (c) des Auftrags: **laufen die Container als Waisen weiter?**

Die Frage (c) des Auftrags: **laufen die Container als Waisen weiter?**

**Diese Erwartung ist nach der Berichtigung oben neu geschrieben, und zwar
bevor der Schlag fiel.** Die erste Fassung stand auf `KillMode=control-group`
und damit auf einer Anlage, die seit 14:08 UTC nicht mehr läuft. Zwei Ausgänge
sind möglich, und **beide sind ein Ergebnis**:

**Ausgang 1 — die Container überleben als Waisen.** Dann ist der gemessene
Befund des Härtungsstrangs bestätigt: die Container hängen in
`/sys/fs/cgroup/ngc.apps/<app>` und nicht an der Einheit, also nimmt der Tod
des Verwalters sie nicht mit. Das ist der **schwerere** der beiden, und zwar
nicht als Stabilitäts-, sondern als Sicherheitsfrage: ein Container läuft
weiter, den kein Verwalter mehr kennt. Kommt der Controller zurück, streiten
sich im schlimmsten Fall zwei Verwalter um einen Container, oder die Oberfläche
zeigt „läuft nicht", während die App läuft und erreichbar ist. Tritt dieser
Ausgang ein, **höre ich auf zu messen und melde es** — der Entwurf muss dann
neu, und die Waisen räume ich nicht selbst weg.

**Ausgang 2 — die Container gehen weg und kommen nicht zurück.** Dann habe ich
**nicht meinen Fall gemessen, sondern Buchpunkt 97**:
`ngc-controller.service` trägt `RuntimeDirectory=ngc` mit
`RuntimeDirectoryPreserve=no`, systemd löscht `/run/ngc` beim Ende der Einheit,
und darin liegt das `XDG_RUNTIME_DIR` des rootlosen podman mit
`overlay-containers/<id>/userdata` — das podman nur beim `create` anlegt. Der
Controller kommt dann zurück, aber eine App, die er **neu starten** muss, nicht
— bis die Maschine neu startet. Das ist gemessen (von jenem Strang, in WSL mit
podman 5.4.2, **nicht** an der Anlage), und `podman system migrate` hilft
dabei nicht.

**Ich sage ausdrücklich nicht, welchen der beiden ich erwarte.** Der eine folgt
aus der cgroup, der andere aus `RuntimeDirectory` — zwei gemessene Tatsachen,
die in verschiedene Richtungen zeigen, und welche gewinnt, entscheidet die
Reihenfolge, in der systemd aufräumt. Das ist genau der Fall, in dem eine
Erwartung geraten wäre.

**Drei Zeilen, die ich für andere Stränge mitschreibe**, weil sie nichts
kosten: lief der Uhr-Container nach dem Schlag weiter; stand er nach der
Rückkehr des Controllers wieder auf demselben Abbild (1.0.1), also erkennt der
Controller einen Waisen wieder oder legt er neu an; und welchen der zwei Sätze
eine scheiternde Anmeldung zeigt.

**Betriebsregeln für diesen Fall, vorher festgeschrieben.** Bleibt eine App
unten, wird sie im Controller **deaktiviert und wieder aktiviert** — das macht
ein `create` und legt den Ordner neu an. Kein `podman start`, kein
`podman restart` von Hand: das nimmt dem Container seine Decke (Buchpunkt 96).
Kein Maschinen-Neustart ohne Ansage. Die App-Daten bleiben unberührt —
`/var/lib/ngc/apps/ngc-uhr-ad8c00e2/` und der Benutzer `ngc-uhr-ad8c00e2`
(uid 995) werden nicht angefasst, kein `podman system reset`, kein Aufräumen
von App-Benutzern; dort liegen Markierungsdatei und Zähler, die der Beweis
eines anderen Strangs sind.

### M4 — `SIGKILL` auf `apache2` (api-1)

Erwartung: **die Anlage bleibt unten.** `Restart=on-abort` deckt ein `SIGKILL`
von außen nicht ab (systemd führt es als `signal`, nicht als `abort`), und der
Apache auf api-1 ist der einzige Eingang. Wenn das so ist, ist der schwerste
Einzelpunkt der Anlage nicht „api-1 fällt aus", sondern „der Apache auf api-1
stirbt und kommt nicht wieder" — und das ist eine Zeile Behebung, nicht ein
Umbau. Gegenprobe im selben Lauf: `SIGTERM` auf denselben Dienst, der müsste
nach derselben Regel ebenfalls nicht neu starten.

### M5 — Austritt mit Code 0 nach einem Fehler

`Restart=on-failure` startet einen Dienst, der mit **0** endet, nicht neu.
Erwartung: es gibt in NGC einen Weg, der nach einem erkannten Fehler mit 0
endet (etwa eine unlesbare Konfiguration beim Start). Das wird **am Code
gemessen**, nicht an der Anlage, und zwar an den `os.Exit`-Stellen der drei
Programme. Finde ich keinen, ist das ein vollwertiges Ergebnis.

### Was dieses Blatt nicht messen wird

- **(c) und (d) in der Oberfläche.** Die Anmeldung dieser Anlage braucht einen
  Passkey auf einem Gerät; jede Messung hier reicht bis `login/start`. Für die
  gefährliche Hälfte („zeigt die Oberfläche eine App als laufend, deren
  Container weg ist?") kann ich nur den Code lesen, und ich sage bei jeder
  Aussage, welche Stufe das war.
- **Die fünf Sekunden der ersten Anfrage.** Daran arbeitet eine eigene Sitzung
  (`ngc-fuenf-sekunden`). Hier stehen nur Zahlen, gebaut wird nichts.
- **`ngc-ein`** (10.2.3.216, VMID 307), die Ein-Maschinen-Linie. Sie ist der
  zweite Fall der Frage, aber eine Maschine mit allen Diensten in einer cgroup
  verhält sich anders, und sie gehört in einen eigenen Lauf.

## Offen, ausdrücklich

- Was den Unterschied macht zwischen „zwei hart gestoppte DB-Knoten fangen sich
  von selbst" (Fall A, 25.10.) und „ein hart gestoppter Knoten brauchte 71
  Versuche" (24.10.). Nicht geklärt, und nicht Gegenstand dieses Blatts.
- Warum in B1 sechs von vierzig Anfragen `500` bekamen und in B2 keine. Offene
  Frage aus `docs/ausfall-nachtrag.md`, hier nur verwiesen.

---

# Gemessen

## M5 — Austritt mit Code 0 nach einem Fehler: nicht gefunden (30.10.2026)

Gemessen am Code, nicht an der Anlage, über `ausfallkopplung` = `origin/main`
25b8f24e. Zuerst die Orte, an denen ein Austrittscode überhaupt entsteht:
`os.Exit(` kommt in `apps/` **außerhalb von Tests viermal** vor, und jedes Mal
in derselben Form — der Code ist der Rückgabewert einer Funktion, kein
Literal:

```
apps/server-api/main.go:55         os.Exit(run(os.Args[1:]))
apps/server-controller/main.go:14  os.Exit(node.Main(...))
apps/cli/main.go:24                os.Exit(classes.Run(...))
apps/uhr/main.go:14                os.Exit(classes.Main(...))
```

Damit ist die Frage auf zwei Funktionen eingeengt, und beide sind von der
ersten bis zur letzten Zeile gelesen:

| Weg | Fehlerfälle | Code |
|---|---|---|
| `ngc-api` `serve` (`main.go:155–172`) | Konfiguration unlesbar · `server.Build` scheitert · `srv.Run` bricht ab | **1** in allen drei |
| `ngc-api` `run` (`main.go:58–89`) | unbekannter Befehl · kaputte Flagge | **2** |
| `ngc-controller` `runWith` (`node/run.go:522–558`) | Konfiguration · Noise-Schlüssel · Speicher öffnet nicht · Vertrauensliste unlesbar · `Run` bricht ab | **1** in allen fünf |
| `ngc-controller` `Main` (`node/main.go:131–173`) | kaputte Flagge · unbekanntes Kommando | **2** |

`return 0` steht in beiden Programmen **nur** hinter `-version`, hinter `-h`
und hinter dem geordneten Halt (`SIGTERM` über `signal.NotifyContext`, dann
`msgStopped`).

**Befund: meine Erwartung war falsch, und das ist die gute Nachricht.** Ich
hatte einen Weg erwartet, der nach einem erkannten Fehler mit 0 endet und den
`Restart=on-failure` darum nicht aufgefangen hätte. Es gibt ihn nicht. Für die
beiden Go-Dienste ist `on-failure` die **richtige** Regel, und sie ist nicht
aus Versehen richtig — der geordnete Halt ist die einzige Stelle, die 0
zurückgibt.

Das verschiebt den Befund 2 von oben, ohne ihn aufzuheben: die Lücke bei den
Neustart­regeln liegt **nicht** bei NGCs eigenen Programmen, sondern bei
`apache2` (`on-abort`) und `nginx` (`no`) — zwei Fremddienste mit den
Debian-Vorgaben, die niemand angefasst hat. Der Eingang der Anlage hängt damit
an der schwächsten der drei Regeln.

**Was das nicht sagt:** ob ein abgestürzter Dienst wirklich zurückkommt. Hier
ist gemessen, welcher Code zurückgegeben **wird**, nicht was systemd daraus
macht. Das ist Stufe „am Code gelesen", nicht „ausgeführt" — M1 bis M4 bleiben
offen, und sie brauchen einen Eingriff an der Anlage.

## M1 — `kill -9` auf `ngc-api` (api-2), gemessen am 30.10.2026, 16:58 UTC

Werkzeug: `tools/ausfall-absturz.sh M1`, von diesem PC aus (Windows,
`curl.exe`), dieselbe Anfrage wie `tools/ausfall-dienst.sh` — sonst wären die
Zahlen mit B1 nicht vergleichbar. Die Maschine blieb an (`ping` nachgewiesen).

Vorrichtung geprüft, bevor eine Zahl fiel: Dienst `active`, `MainPID=623`, und
nach dem Schlag war PID 623 **weg** (`kill -0`). Ohne diese Prüfung hätte jede
Zahl danach den gesunden Betrieb gemessen.

| | erwartet | gemessen |
|---|---|---|
| kommt der Dienst zurück? | ja, nach `RestartSec=5` | **ja, in 5 s**: Schlag 16:58:48, zurück 16:58:53 |
| Beleg dafür | PID-Reihe | **MainPID 623 → 0 (2 s `activating`) → 1374** |
| wie sieht systemd das? | Signal, nicht Austritt | **`code=killed, status=9/KILL`, `Failed with result 'signal'`** |
| Bild von außen | wie B1: schnell, einzelne `500` | **eine einzige `500`, und zwar die erste** |

Die fünf ersten Anmeldungen, einzeln:

```
1. 500 in 0,0247 s   <html><head><title>500 Proxy Error</title>…
2. 200 in 0,0290 s
3. 200 in 0,0268 s
4. 200 in 0,0269 s
5. 200 in 0,0276 s
```

**Die Zeile, die ein Nachbarstrang braucht, und sie widerlegt seinen
Verdächtigen.** Gefragt war, welchen der zwei Sätze eine scheiternde Anmeldung
zeigt: „Anmeldung fehlgeschlagen." oder „Dienst nicht erreichbar.". Die Antwort
ist **keiner von beiden** — es kommt überhaupt keine NGC-Antwort, sondern die
nackte Apache-Seite. `login.js:449` entscheidet am Feld `code`, und das gibt es
in dieser Antwort nicht. Ein abgeschossener API-Knoten erzeugt also nicht den
falschen Satz, er erzeugt **keinen**.

**Erwartung gegen Messung:** ich hatte „dasselbe Bild wie B1" erwartet, also
eine Streuung von `500` über die ganze Lücke. Gemessen ist es schärfer: genau
**eine** Anfrage fällt, die erste. Der Unterschied zu B1 (dort 6 von 40 über
drei Minuten) ist die **Dauer** — fünf Sekunden statt drei Minuten geben dem
`retry=5` des Lastverteilers nur ein Fenster statt sechsunddreißig.

Nebenbei gemessen, gegen die Regel aus `messen.md`: `NRestarts` stand hier
**nicht** auf 0, sondern auf 1, und blieb es. Die Regel „`NRestarts` ist kein
Neustartzähler" bleibt trotzdem richtig — sie sagt, dass eine 0 kein Beleg für
„nichts passiert" ist, nicht dass die Zahl immer falsch ist. Der Beleg hier ist
und bleibt die PID-Reihe.

## M3 — `kill -9` auf `ngc-controller` (svc-1), 30.10.2026, 17:00 UTC

**Beide Ausgänge der Erwartung sind eingetreten, und genau darin liegt der
Befund.** Ich hatte geschrieben, dass cgroup und `RuntimeDirectory` in
verschiedene Richtungen zeigen und ich nicht weiß, welche gewinnt. Keine
gewinnt: die Waisen überleben **und** die App kommt zurück.

### Vorher gemessen, bevor der Schlag fiel

Selbst nachgesehen, nicht übernommen: `ngc-controller 0.3.0 (contract 1)`,
Prüfsumme `87a0f34133b76650`, Datei vom 30.10. 14:07. `/sys/fs/cgroup/ngc.apps`
trägt beide App-cgroups. Und der Punkt, der die Erwartung schon halb
entschied: **beide `conmon` hatten vor dem Schlag ppid=1** und saßen in
`/ngc.apps/<app>` — sie hingen also nie an der cgroup der Einheit.
`RuntimeDirectory=ngc`, `RuntimeDirectoryPreserve=no` ebenfalls bestätigt.

### Gemessen

Schlag auf `MainPID=655` um **17:00:42 UTC**. systemd:
`code=killed, status=9/KILL`, `Failed with result 'signal'`,
`restart counter is at 1`, zurück **17:00:47** als PID 4267 — fünf Sekunden.
api-1 sah den Abbruch: `controller tunnel ended node=svc-1 seconds=10337`.

Danach, aus `/proc` gelesen und nicht aus `pgrep` (die doppelten Namen hätten
ein Zählfehler sein können):

```
pid=745  Zustand=S ppid=1 seit 14:08:24  cgroup /ngc.apps/ngc-terminal-439982dd
pid=787  Zustand=S ppid=1 seit 14:08:24  cgroup /ngc.apps/ngc-uhr-ad8c00e2
pid=4308 Zustand=S ppid=1 seit 17:00:47  cgroup /ngc.apps/ngc-terminal-439982dd
pid=4350 Zustand=S ppid=1 seit 17:00:47  cgroup /ngc.apps/ngc-uhr-ad8c00e2
```

Vier lebende `conmon`, an jedem ein lebendes Kind `/ngc/ngc-hold hold`. Die
cgroup bestätigt es von der anderen Seite:

```
/sys/fs/cgroup/ngc.apps/ngc-uhr-ad8c00e2/cgroup.procs = 787 789 4350 4352
pids.current = 14
```

**Beide Paare liegen in derselben cgroup.** Die 14:08:24 sind die Startzeit der
Fassung 0.3.0 — das sind die Container von damals, und sie haben den Schlag
überlebt.

### Punkt 97 ist nicht eingetreten, und das berichtigt eine fremde Erwartung

`/run/ngc` wurde gelöscht **und um 17:00:47 neu angelegt** — beide
App-Unterordner tragen diesen Zeitstempel. Das Journal zeigt zweimal
`podman … system refresh`. Eine App, die neu gestartet werden muss, kommt also
doch zurück. Was **nicht** zurückkommt, ist der alte Container: er geht nicht
weg.

### Warum das ein Sicherheitsbefund ist und nicht nur eine Stabilitätsfrage

Ein Container läuft weiter, den kein Verwalter mehr kennt, mit den Rechten
seines App-Benutzers, und die Oberfläche kann ihn nicht sehen. Dazu teilen
beide Instanzen eine cgroup, deren Schranken nach dem bekannten Punkt ohnehin
nicht greifen — sie tragen jetzt die doppelte Last.

**Was ausdrücklich NICHT gemessen ist, weil hier angehalten wurde:** ob jeder
weitere Absturz noch einmal verdoppelt. Das ist die naheliegende nächste
Messung und sie ist offen.

**Und eine Einschränkung, damit der Befund nicht größer klingt, als er ist:**
die Container führen `ngc-hold hold`, einen Halteprozess, und es lauscht kein
Port (`ss -tlnp` zeigt auf svc-1 nur `systemd-resolve` und `sshd`). Gemessen
ist **„zwei Container-Instanzen je App laufen"**, nicht „zwei Uhren bedienen
Benutzer". Welches Abbild sie tragen, ist nicht gelesen: `podman ps` als
App-Benutzer scheitert an `mkdir /run/user/0/libpod: permission denied`. Stufe:
am Knoten gemessen, nicht in der Oberfläche gesehen.

### Was von außen zu sehen war: nichts

Oberfläche `200`, Anmeldung `200` in 0,021–0,031 s, durchgehend. **Die Anlage
hat diesen Vorfall mit keiner Zeile und keiner Antwort nach außen gemeldet** —
und damit ist die Kernfrage des Auftrags ein zweites Mal beantwortet, diesmal
an einem Fall, den ich selbst gefahren habe.

## M2 — angehalten, nicht gemessen

Die Leitsitzung hatte für den Fall „Waisen überleben" angeordnet, die Reihe
anzuhalten und zu melden. Das ist eingetreten, und M2 ist darum **nicht
gefahren**. Die Erwartung dazu (der Riegel gegen die Neustart-Schleife greift
nach der Rechnung nie) steht oben unverändert und ist **ungemessen**; sie trägt
keine Zahl und darf keine bekommen, bis sie gefahren ist.

## M3b — wächst es mit jedem Absturz? Erwartung, vor dem zweiten Schlag

Freigegeben als die wichtigste offene Frage aus M3: **verdoppelt jeder weitere
Absturz noch einmal?** Vier Instanzen sind ein Befund, sechs sind ein anderer —
nämlich einer, der mit der Zeit wächst.

Gemessen wird die Zahl aus `cgroup.procs` je App, **vorher und nachher**, nicht
die Namen aus `pgrep`.

**Erwartung: sechs, also +2 je Absturz.** Die Begründung ist der Weg, den M3
freigelegt hat, und sie ist eine Kette aus zwei gemessenen Stücken: die
`conmon` der zweiten Runde (4308, 4350) haben **ppid=1** und sitzen in
`/ngc.apps/<app>`, also überleben sie den nächsten Schlag genauso wie die
ersten; und `/run/ngc` wird beim Ende der Einheit gelöscht, womit podman sein
Laufzeitwissen verliert, beim Start `system refresh` die vorhandenen Container
für nicht laufend hält und neue anlegt. Beides noch einmal, also +2.

**Was die Erwartung widerlegen würde, und worauf ich darum besonders sehe:**
podman führt seine Container auch in einer Zustandsdatenbank, die **nicht** in
`/run` liegt. Erkennt `system refresh` beim zweiten Mal, dass dort schon zwei
Einträge je App stehen, könnte es bei vier bleiben — dann wäre die Verdoppelung
ein einmaliger Übergang und keine wachsende Lücke. Das ist der freundlichere
Ausgang, und er ist ebenso gut möglich wie meiner.

## M3b — gemessen: sechs. Es wächst mit jedem Absturz

Zweiter `kill -9` auf `ngc-controller` (svc-1), auf `MainPID=4267`, um
**17:05:52 UTC**. systemd: `code=killed, status=9/KILL`,
`restart counter is at 2`, zurück **17:05:58** als PID 5552 — wieder fünf
Sekunden, wieder zweimal `podman … system refresh` im Journal.

| | vorher (nach M3) | nachher (nach M3b) |
|---|---|---|
| `ngc-uhr-ad8c00e2` `cgroup.procs` | `787 789 4350 4352` | **`787 789 4350 4352 5635 5637`** |
| `ngc-terminal-439982dd` `cgroup.procs` | `745 747 4308 4310` | **`745 747 4308 4310 5593 5595`** |
| `pids.current` je App | 14 | **21** |
| `conmon` insgesamt | 4 | **6** |
| `NRestarts` | 1 | 2 |

Die Startzeiten trennen die drei Generationen sauber, und keine ist
verschwunden:

```
pid=745  ppid=1 seit 14:08:24   pid=787  ppid=1 seit 14:08:24
pid=4308 ppid=1 seit 17:00:47   pid=4350 ppid=1 seit 17:00:47
pid=5593 ppid=1 seit 17:05:58   pid=5635 ppid=1 seit 17:05:58
```

**Erwartung getroffen: +2 je Absturz, und der freundliche Ausgang ist
ausgeschlossen.** Ich hatte als Gegenmöglichkeit genannt, dass podmans
Zustandsdatenbank außerhalb von `/run` die vorhandenen Einträge wiedererkennt
und es bei vier bleibt. Das tut sie nicht: `system refresh` lief beim zweiten
Mal genauso und legte trotzdem einen dritten Satz an.

**Damit ist der Befund kein einmaliger Übergang, sondern eine Lücke, die mit
der Zeit wächst.** Jeder Absturz des Controllers legt einen Satz Container
dazu, und keiner geht je weg. `pids.current` wuchs von 14 auf 21, also um 7 je
Instanz, in einer cgroup, deren Schranken nach dem bekannten Punkt nicht
greifen. Drei Abstürze an einem Nachmittag haben aus zwei Instanzen sechs
gemacht — ohne eine einzige Zeile nach außen: die Anlage antwortete durchgehend
mit `200`.

Hier wurde wieder angehalten, wie abgesprochen. **Nicht gemessen** ist, was bei
hoher Zahl passiert (Speicher, `pids.max`), und ob ein Neustart der Maschine
alle Generationen abräumt. Die Waisen bleiben stehen: sie sind der Beweis, und
die Behebung gehört der Fassung 0.3.0 und damit einem anderen Strang.
