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

**Befund 3: `KillMode=control-group` beim Controller erklärt einen fremden
Befund.** `docs/ausfall-nachtrag.md`, Fall B4, hat gemessen, dass
`systemctl stop ngc-controller` die App-Container **mitnimmt** — und hat die
Ursache offen gelassen. Sie steht hier: die Container liegen in der cgroup des
Controllers, und `control-group` bringt beim Stopp alles darin um. Das passt zu
dem bekannten Punkt, dass die App-Container keine eigenen, greifenden
cgroup-Schranken haben.

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

Erwartung: **ja.** `KillMode=control-group` wirkt nur, wenn systemd die Unit
beendet; stirbt der Hauptprozess allein, bleiben die übrigen Prozesse der
cgroup laufen, bis systemd die Unit für beendet hält und aufräumt — und das tut
es bei `Type=simple`, sobald der Hauptprozess weg ist. **Ich erwarte darum
beides und weiß nicht, welches:** entweder Waisen, die weiterlaufen, oder
dasselbe Bild wie B4 (alles weg). Die beiden Ausgänge verlangen verschiedene
Behebungen, und genau darum ist dieser Fall zu messen und nicht zu überlegen.

Der schlimmere der beiden: **Waisen.** Dann läuft die Uhr, der neue Controller
weiß nichts von ihr, und zwei Verwalter streiten sich um einen Container —
oder die Oberfläche zeigt „läuft nicht", während sie läuft.

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
