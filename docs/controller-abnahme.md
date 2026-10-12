# Abnahme Controller

Dieses Blatt gehört zu Aufgabe 25 des Controller-Plans
(`docs/superpowers/plans/2026-09-23-ngc-plan-3-controller.md`, ab Zeile 2733) und ist die
letzte Aufgabe des Pakets. Vorbild ist `docs/recovery-abnahme.md` des Recovery-Pakets.

**Eine Zeile gilt erst als bestanden, wenn Ergebnis, Datum und Prüfer eingetragen sind.
Prüfer ist immer ein Mensch.** Die Zeilen mit Zahlen daneben sind gemessen, aber nicht
abgenommen: das Eintragen ist Thomas' Sache, keiner Sitzung.

## Wie dieses Blatt gelesen wird

Jede Aussage trägt eine **Stufe**. Die vier sind nicht austauschbar, und die Verwechslung ist
der Grund für dieses Blatt: wer „ausgeliefert" für „ausgeführt" nimmt, sucht an der Stelle
nicht mehr.

| Stufe | heißt | heißt **nicht** |
|---|---|---|
| **geschrieben** | der Satz steht im Repo | dass er stimmt |
| **übersetzt** | Werkzeug und Syntax nehmen es an (`go vet`, `bash -n`, `tools/check.sh`) | dass es etwas tut |
| **ausgeliefert** | der committete Stand ist bitgleich mit dem gemessenen (`git hash-object` beidseitig) | dass ein Lauf ihn berührt hat |
| **ausgeführt** | ein Lauf hat die Zeile erreicht, mit Ausstiegscode und Fallzahl daneben | dass es auf einem echten Server so läuft |

Die letzte Spalte der vierten Zeile ist die wichtigste Einschränkung dieses Pakets: **alles
unten ist auf Windows mit Attrappen gelaufen.** Kein Debian, kein podman, kein systemd, keine
Dateirechte — siehe „Was ungemessen bleibt".

## Werkzeuge und Umgebung der Messung

| | Wert |
|---|---|
| Werkzeugkette | go1.27.2, Windows 11 (amd64) |
| Arbeitsbaum | `controller-t25`, abgezweigt von `controller` (`33ac964`) |
| ulh-Lauf | Debian in WSL, ulh-Zweig `ngc-controller` ab `main` (`096a655`) |
| `-race` | **nicht gefahren** und nicht gesucht (Vorgabe dieser Maschine) |
| `jq` | auf dieser Maschine **nicht vorhanden**, weder in WSL noch unter Windows |
| `shellcheck` | auf dieser Maschine **nicht vorhanden** |

## Was diese Aufgabe geliefert hat

| Datei | Repo | Stufe |
|---|---|---|
| `apps/server-controller/classes/serve/attack_test.go` | nextgencloud | **ausgeführt** |
| `apps/server-controller/VERSION` → `1.0.0` | nextgencloud | **übersetzt** (kein Bau mit `-ldflags` gemessen) |
| `docs/controller-abnahme.md` (dieses Blatt) | nextgencloud | **geschrieben** |
| `apps/cli/assets/scripts/ngc-controller.sh` | ulh | **übersetzt** (`bash -n`), Teile **ausgeführt** |
| `apps/cli/tests/ngc-controller.test.sh` | ulh | **ausgeführt**, 90 Fälle |
| `apps/cli/assets/catalog.yaml` (Kategorie + Eintrag, 5 Sprachen) | ulh | **ausgeführt** (ulhs eigene Tests) |
| `apps/cli/tests/menu.test.sh` (16 → 17 Kategorien) | ulh | **ausgeführt** |
| `docs/scripts.md` (mit `tools/gen-scripts-doc.sh` erzeugt) | ulh | **ausgeführt** |
| `README.md`, `docs/guide.md` (96 → 97 Skripte), `CHANGELOG.md` | ulh | **geschrieben** |

## Die Angriffstests

`go test -count=1 -v -run Attack ./apps/server-controller/classes/serve/` → **Ausstiegscode 0**,
9 Fälle, davon **8 PASS** und **1 SKIP**. Stufe **ausgeführt**.

Je Angriff steht hier, **was er nicht erreicht** — das ist die Aussage, nicht das Grün:

| Fall | Angriff | Code | Was er nicht erreicht | Zwischenprüfung |
|---|---|---|---|---|
| `TestAttackForgedTask` | Aufgabe mit fremdem Schlüssel unterschrieben | `E_VERIFY` | keinen Handschlag, keinen Container; der Dienst lief genau **einmal** (die Zwischenprüfung) | dieselbe Wurzel-Aktion mit echter Aufgabe läuft durch |
| `TestAttackSwappedTask` | echte Aufgabe für eine **andere** Aktion desselben Pakets | `E_VERIFY` | dieselbe Wand; der Dienst lief einmal | die Aufgabe öffnet **ihren eigenen** Kanal |
| `TestAttackMissingConfirmation` (2 Unterfälle) | Wurzel-Aktion ohne Aufgabe; kritische Aktion mit Aufgabe, ohne Bestätigung | `E_CONFIRM` | beides vor dem Handschlag | mit Aufgabe bzw. mit Bestätigung läuft derselbe Kanal |
| `TestAttackForeignControllerKey` | Handschlag gegen den Schlüssel eines **anderen** Controllers | `E_INTERNAL`, `HandshakeFailures` +1 | keine Transportschlüssel, kein Containerprozess | gegen den eigenen Schlüssel kommt der Handschlag zustande |
| `TestAttackContainerEscape` | aus dem Container `/etc/shadow` lesen, ins Netz | — | **NICHT GEMESSEN** (kein Linux, kein podman) | im Fall enthalten: „der Container startet überhaupt", sonst `t.Fatalf` |
| `TestAttackReplayedOpen` | dieselbe unterschriebene Aufgabe zweimal | `E_VERIFY` | der zweite Kanal erreicht nichts; der Dienst lief **einmal** | der erste Kanal derselben Aufgabe läuft durch |
| `TestAttackChangedParams` | ein Byte in `params`, nachdem der Prolog darüber gebaut war | `E_INTERNAL`, `HandshakeFailures` +1 | keinen Handschlag | mit unveränderten `params` läuft derselbe Kanal |
| `TestAttackCutInput` | EOF des Transports **vor** dem `E2EEnd` der Shell | `E_PROTOCOL` | keinen Erfolg: das Programm liest die halbe Eingabe und endet trotzdem als Fehler | dieselben zwei inneren Nachrichten **mit** `E2EEnd` enden als Erfolg |
| `TestAttackWrongUIHash` | untergeschobene Oberfläche | — | **in `serve` gar nicht erreichbar**: dieser Bau führt `activate` nicht aus, die Wand wäre `E_INTERNAL`. Gemessen in `install`: `TestAcceptChecksChecksumAndUIHash` und `TestActivateRefusesWrongUIHash` | der Fall wird rot, sobald `serve` `activate` oder `stage` ausführt |

**Zur Zwischenprüfung:** Sie hat zweimal eine falsche Wand gefangen, während diese Datei
entstand — eine Aufgabe in einem OPEN, das auch `params` trägt, wird von `wire.Open.Validate`
abgewiesen, **bevor** eine Unterschrift angesehen wird; und eine Wurzel-Aufgabe eines Benutzers
ohne Wurzelrecht endet `E_FORBIDDEN`, nicht `E_CONFIRM`. Ohne die Zwischenprüfung hätten beide
Fälle grün gemeldet, dass ein Riegel hält, der nie erreicht wurde.

### Die Nulllinie

Ein Prüfsatz, der nie rot wird, ist kein Riegel. Beide Sätze sind absichtlich rot gemacht
worden, die Mutationen in **Kopien** (`go test -overlay`, bzw. eine Kopie des ulh-Baums),
nie im Arbeitsbaum:

| Mutation | Erwartung | Gemessen |
|---|---|---|
| `E_CONFIRM` → `E_VERIFY` in „root ohne Aufgabe" | muss sterben | **Code 1**, `--- FAIL`, Zeile: „the channel ended with … E_CONFIRM …, want … E_VERIFY" |
| die gefälschte Aufgabe echt unterschreiben, Erwartung `E_VERIFY` lassen | muss sterben | **Code 1**, `--- FAIL`, es kam `E_TIMEOUT` statt `E_VERIFY` |
| eine lokale Variable umbenennen (`stranger` → `strangerKey`) | muss **leben** | **Code 0**, kein Fehler |
| `RestartSec=5` → `RestartSec=1800` im ulh-Skript | muss sterben | **Code 1**, 3 Fälle rot, darunter „waits far less than the 900 seconds of the lock" |

Der erste Versuch der zweiten Mutation war ein **Bauabbruch** (`declared and not used: forged`)
und wurde verworfen und neu gebaut: ein Rot aus einem Bauabbruch beweist nichts
(`Z:\GitHub\claude-md\messen.md`). Der Mutationslauf des ulh-Skripts hat seine eigene
Vorbereitung geprüft und beim ersten Versuch **abgebrochen** statt eine Zahl zu nennen (ein
`sed` starb am eigenen Trennzeichen).

## Der ganze Prüfsatz des Controllers

`go test -count=1 -v ./apps/server-controller/...` → **Ausstiegscode 0**. Stufe **ausgeführt**.

| | Wert |
|---|---|
| `=== RUN` | **944** |
| `--- PASS` (samt Unterfällen) | **877** |
| `--- SKIP` | **67** |
| `--- FAIL` | **0** |

Die **67** übersprungenen Fälle sind die, die Linux brauchen (podman, Systembenutzer,
POSIX-Shell, `/dev/ptmx`, Dateirechte, freier Plattenplatz) — sie sagen es unter `-v` je mit
eigener Zeile. `ok` ohne `-v` wäre hier keine Aussage: ein Satz, der sich stumm selbst
abschaltet, sieht grün aus.

`tools/check.sh` (ohne Pipe) → **Ausstiegscode 0**, Schlusszeile **`check: ok`** gedruckt. Die
Null allein wäre kein Beleg; die Zeile ist es.

**`-race` ist nicht gefahren.** Nicht „grün", nicht „rot" — nicht gemessen.
**`govulncheck` ist nicht gefahren.**

## Der ulh-Lauf

`bash apps/cli/tests/run.sh` in WSL (Debian), gegen eine Kopie des Zweigs `ngc-controller`:

| | Wert |
|---|---|
| Ausstiegscode | **1** |
| bestanden / fehlgeschlagen | **143 / 1** |
| davon im Abschnitt `ngc-controller` | **90 bestanden**, 1 rot |
| die eine rote Zeile | `NOT MEASURED: jq is here, so the change of the roles really ran` |
| Sonde `(exit 7)` vor der Messung | **7**, danach `false` → **1** |
| `bash <skript>` mit unbekannter Aktion | **Code 1** mit „Unknown action" |

Der Ausstiegscode wurde **in** WSL in eine Datei geschrieben und von dort gelesen: `$?` ist im
Befehlstext von `wsl -- sh -c '…'` schon tot. Die Messung lief als **Datei**, und sie prüft
vorher, dass die Kopie ein git-Repo mit 151 Dateien ist — ohne das fällt ulhs `run.sh` auf eine
reine Kopie zurück, `yq` findet keinen Katalog, und **91 von 158 Fällen** scheitern aus einem
Grund, der mit dem geprüften Skript nichts zu tun hat (so zuerst gemessen).

Stufe **ausgeführt** für die 90 Fälle, Stufe **geschrieben** für `change_settings`.

## Die Abnahme von Hand (Schritt 5 des Plans)

Keine dieser vier Zeilen ist gemessen. Sie brauchen den Server „Dienste" mit Debian, podman
und systemd; dieser Rechner ist Windows.

| Nr. | Kriterium | Ergebnis | Datum | Prüfer |
|---|---|---|---|---|
| 1 | Eine App mit Unterschrift aktivieren | offen | | |
| 2 | Im Terminal `cp -r` eintippen und die Ausgabe sehen | offen | | |
| 3 | Server neu starten, während die API aus ist: die Apps laufen wieder | offen | | |
| 4 | Ein absichtlich kaputtes Update einspielen: es wird selbst zurückgenommen | offen | | |
| 5 | `go test -race ./apps/server-controller/...` auf einem Linux-Server | offen | | |
| 6 | `govulncheck ./apps/server-controller/...` | offen | | |
| 7 | `shellcheck apps/cli/assets/scripts/ngc-controller.sh` | offen | | |
| 8 | Ein Durchlauf von `ngc-controller.sh install` in WSL (Debian) mit `answer.yaml` | offen | | |
| 9 | Die 67 übersprungenen Go-Fälle auf einem Linux-Server grün | offen | | |

Zu 8 ausdrücklich: **ein echter Durchlauf des Skripts ist nicht gemessen.** Was gemessen ist,
sind seine Funktionen einzeln (90 Fälle) und dass es mit einer unbekannten Aktion mit Code 1
antwortet. `install` braucht root, dpkg, systemd und ein Release von NGC, das es noch nicht
gibt.

## Die offenen Punkte, die beim Controller bleiben

Diese Liste ist der Teil dieses Blattes, nach dem der Nächste entscheidet, wo er **nicht**
mehr sucht. Jeder Punkt kommt aus dem Bericht der Aufgabe, die ihn gemessen hat.

1. **`serve.Deps.Updater` bleibt auf dem echten Server `nil`.** Die Verteilung
   (`update.Manager`) ist gebaut und gemessen, **ihr Aufrufer fehlt**: `node/run.go:531` setzt
   ausdrücklich `Updater: nil`. Jeder Install-Kanal endet damit `E_INTERNAL`. (Aufgaben 19/20
   und 24.) **Gemessen am 12.10.2026, und es ist schärfer als „ein Feld ist nicht gesetzt":**
   `grep -rn 'update\.New\|install\.New\|install\.Deps' apps/ --include=*.go | grep -v _test.go`
   → **0**. Kein Programm baut je einen `update.Manager` oder `install.Manager` — es gibt also
   nichts, womit man das Feld setzen könnte. Begründet ist die Naht in `node/run.go:521-531`:
   `update.Deps` nimmt die Vertrauensliste als **Wert**, während `chain.Keeper` sie unter dem
   Programm austauscht.
2. **`ngc-api migrate` fehlt als Aufruf** (Punkt 28 der Verträge). Der Controller startet die
   Migrationen als Programm — nur gibt es den Unterbefehl noch nicht. `openState` migriert
   allein das **eigene** Schema. Ein `migrate.go` im Controller gibt es bewusst nicht: ein
   Gerüst sähe aus wie eine Zusage. **Dazu gemessen am 12.10.2026:**
   `config.Controller.Migrate` liest **kein Programm** —
   `grep -rn 'Controller\.Migrate\|Migrate ' shared/config/config.go` trifft nur
   `config.go:293,296` (Erklärung und Feld) und `:676,679-680,748-749`, also `Validate` und
   `LogValue`. Ein **totes Vertragsfeld**, das auf einem db-Knoten etwas tun soll.
3. **Die Migrationssperre gilt nur für den Augenblick der Migration.** Zwei Controller, die
   beide hochkamen, schreiben danach **gleichzeitig** in dieselbe Datei. **Nicht gemessen.** Die
   Antwort des Datenbankpakets darauf ist `busyTimeout` 10 s plus Wiederholung. Wer zwei
   Controller auf eine Datei lässt, muss das zuerst messen. (Punkt 32.)
4. **Die Abbild-Referenz fehlt in `dbapi.App`** (Punkt 24). Verliert eine App beim Update ihre
   Dienste, bleibt ihr Abbild liegen — im **Datenverzeichnis** der App, und `DataSize` zählt es
   als Benutzerdaten mit. Ein Verwalter liest vor dem Löschen eine Zahl, die etwas anderes zählt,
   als ihr Name sagt. Die Lösung ist eine Änderung im eingefrorenen `shared/`.
5. **Die Lizenz hält keinen App-Kanal auf.** `license.Allowed` ist gebaut und gemessen und
   hängt im Startweg (`node.run` → `Licensed`, `node.Restore`) — aber **nicht** in
   `serve/app.go`. Die Stelle ist im Code benannt („here the licence of the app belongs"), und
   **kein `E_LICENSE` verlässt dieses Paket**: ein App-Kanal läuft heute auch für eine App,
   deren Lizenz abgelaufen ist. **Nachgemessen am 12.10.2026:**
   `grep -rn 'license.Allowed' apps/ --include=*.go | grep -v _test.go` → **ein** Aufruf im
   Produktionscode, `node/run.go:506`; die Stelle in `serve/app.go:217-221` ist ein
   **Kommentar**. Und `grep -rn 'CodeLicense\|E_LICENSE' apps/server-controller/` → **7**
   Treffer, **alle** in Kommentaren oder in `CLAUDE.md`, **keiner** im Code. `serve.Deps` um Liste und Installation zu erweitern ist der
   Eingriff, der dafür fehlt. (Aufgabe 18, Abweichung 5.)
6. **Das Einrichten einer App-Aktualisierung IST gebaut und verdrahtet — und von keinem
   Programm erreichbar.** Hier stand bis zum 12.10.2026 „ist nicht gebaut … die Betriebsfassung
   von `AppInstaller` (`Needs` + `Update`) fehlt". Das ist **überholt**; nachgemessen am
   12.10.2026 im Zweig `controller` (`8bf977b`):

   | gemessen | Befehl | Stelle |
   |---|---|---|
   | `Needs` existiert | `grep -n 'func (m \*Manager) \(Needs\|Update\)' apps/server-controller/classes/install/update.go` | `install/update.go:78` |
   | `Update` existiert | derselbe Befehl | `install/update.go:140` |
   | beide werden **gerufen** | `grep -n '\.Needs(\|\.Update(' apps/server-controller/classes/update/install.go` | `update/install.go:459` und `:471` |

   Der Riegel davor ist derselbe, den Vertragspunkt 31 beschreibt: `install.Update` glaubt der
   Lesung des Aufrufers **nicht**, sondern vergleicht sie gegen das Manifest (`sameFile`,
   `sameSet`), sonst `ErrVerify`. **Eine Quelle, das Manifest.**

   **Was offen bleibt, ist etwas anderes:** diese Arbeit ist von keinem Programm erreichbar,
   weil niemand einen `update.Manager` oder `install.Manager` baut (Punkt 1). Stufe:
   **von Fällen ausgeführt**, nie **in Betrieb**.
7. **`core.Unattended` kann den ERSTEN Einbau eines Teils nie erlauben** (Aufgabe 20,
   BLOCKED 1): `store.Newer("1.2.0", "")` ist **false**, der Kommentar von `core.Unattended`
   sagt das Gegenteil. Die Zeile liegt in eingefrorenem `shared/`.
8. **Was eine `ngc-web`-Release-Datei ist, steht nirgends** (Aufgabe 20, BLOCKED 2):
   `shared/testdata/release/ngc-web-all.tar` ist kein tar, sondern 18 Byte Text.
9. **65 bis 78 Fälle überspringen sich laut**, weil sie Linux brauchen — hier **67** gezählt
   (podman, Systembenutzer, POSIX-Shell, `/dev/ptmx`, Rechte, Plattenplatz). Sie sind kein Grün.
10. **Der Balancer-Block** (`/etc/ngc/apache-apis.conf`) steht seit 12.10.2026 in
   `shared/ulh.md` und wird von `TestBalancerTemplateMatchesTheContract` byteweise gehalten —
   **gemessen gegen die Vorlage, nicht gegen ein laufendes Apache.** `apachectl configtest` ist
   nie gelaufen. **Hier stand bis zum 12.10.2026 „die Apache-Vorlage"**, und das kann jemanden
   glauben lassen, der virtuelle Host sei hier geprüft. Gemessen:
   `grep -c 'VirtualHost\|apache-vhost' shared/ulh.md` → **0**,
   `grep -c 'apache-apis' shared/ulh.md` → **2**. Die Vorlage des Auslieferungsweges
   (`apps/web/templates/apache-vhost.conf`, Zweig `web`) ist etwas **anderes** und von diesem
   Paket nicht berührt.
11. **Das Terminal-Protokoll** (Punkt 29: 1 Byte Art, 2 Byte Länge, Nutzlast) ist eine
   **Abweichung vom Plan** und mit der Sitzung Webinterface **noch abzustimmen**. Wie viele
   Terminals gleichzeitig laufen dürfen, begrenzt seit Aufgabe 24 `serve.DefaultMaxApps = 128`;
   die Zahl ist **gerechnet, nicht gemessen** (etwa 50 MiB und 128 Prozesse).
12. **Der Einmal-Code wird vor der MAC-Prüfung verbraucht** (Punkt 26), an **zwei** Stellen
   (`join.Accept` und `controllers.Pair`). Wer es ändert, ändert beide.
13. **Punkt 31:** Nichts hält `store.Package.Needs` des Katalogeintrags gegen
    `store.Manifest.Needs`. Heute keine Lücke, weil niemand die Katalogkopie als Grundlage einer
    Rechteentscheidung nimmt. Wer das tun will, muss es zuerst dort lesen.
14. **Punkt 33 — die Rollenwand im Einbau — ist behoben; bis zum 12.10.2026 fehlte dieser Punkt
    in dieser Liste ganz.** Was gefehlt hat: `update/install.go` kannte das Wort `Roles` nicht,
    `knownParts` führte `AutoWeb`, `AutoAPI`, `AutoController` ohne Unterschied, und die
    Wandliste im Kopf von `Install` zählte **acht** Schritte, keiner davon eine Rollenwand. Ein
    `install ngc-api` auf einem db-Knoten wäre dort nach `/usr/lib/ngc` kopiert und
    `systemctl restart ngc-api` gefahren worden; die einzige Wand lag in `rollout.go` auf der
    **API**-Seite, also auf der falschen Seite der Leitung.

    **Gemessen am 12.10.2026 im Zweig `controller` (`8bf977b`, „Refuse an NGC part that does not
    belong on this server"):** `grep -rn 'Roles' apps/server-controller/classes/update/ | grep -v _test.go`
    → **5** Treffer (die Abschlussprüfung hatte auf `52c2346` **0** gemessen), und die Wandliste
    im Kopf von `Install` trägt jetzt Schritt **6b**: „an NGC part … the roles of THIS server
    have to run that part (runs), so that nobody can tell an API server to become a web server".
    Entschieden wird mit `tunnel.Components`, derselben Funktion, mit der die API ihre Knoten
    wählt — die zwei Seiten können nicht auseinanderlaufen.

    **Noch nicht abgenommen:** auf einem echten Server ist die Wand nie gelaufen, denn dieses
    Teilsystem ist nie in Betrieb (Punkt 1).
15. **`ngc-controller image` steht in der Hilfe und antwortet immer „noch nicht gebaut".**
    Gemessen am 12.10.2026: `writeUsage` (`node/main.go:178`) führt
    „`image -arch <amd64|arm64> -out <Datei>`" als Kommando auf; `buildImage` (`:361-368`) prüft
    die Flags und antwortet dann `notReady` → „Dieser Teil ist noch nicht gebaut." mit
    **Code 1**. Der Kommentar daneben sagt, warum („no later task fills this command: which of
    the two ways stays is the coordinator's call") — **die Hilfe sagt es nicht.**

    **Die Folge ist größer als der Befehl:** `grep -rn '"image"' apps/ --include=*.go | grep -v _test.go`
    findet außer `install/activate.go:412` (Rücknahme eines geladenen Abbilds) nur Stellen in
    `node/main.go`. **Damit hat das Abbild der Terminal-App keinen Erzeuger**, und das Terminal
    aus Aufgabe 23 ist auf einem echten Server nicht in Betrieb zu nehmen, solange niemand
    entscheidet, wer das Abbild baut. (Die Folge ist **geschlossen** aus der Messung, nicht
    gefahren.)
16. **Zu entscheiden (Sicherheitsfrage, nicht Befund): ist ein Kopplungscode 24 Stunden lang
    gültig richtig?** Gemessen am 12.10.2026: `tunnel.PairLifetime = 86400`
    (`shared/tunnel/tunnel.go:35`; die Feldkommentare `:86`/`:107` sagen „one use, valid for
    PairLifetime") — **24 Stunden**. Die einzigen 30 Sekunden in der Nähe sind
    `pairing.Timeout = 30 * time.Second` (`pairing/pairing.go:33`), und die begrenzen die
    **HTTP-Anfrage**, nicht die Lebenszeit des Codes.

    Bis zum 12.10.2026 begründeten `node/ask.go:17-22` und `node/main.go:206-207` das Lesen des
    Codes von **stdin** statt aus `argv` mit „den **dreißig Sekunden** einer Kopplung" — das
    Fenster ist **2880-mal** so groß, wie die Begründung sagte. Die Entscheidung (stdin statt
    `argv`) bleibt dadurch richtig und wird nur **wichtiger**; die zwei Kommentare sind
    berichtigt. **Die Zahl selbst liegt in eingefrorenem `shared/tunnel` und ist nicht
    geändert.** Wer über `hidepid` oder über die Dauer des Codes entscheidet, entscheidet hier.

## Was ungemessen bleibt

Abschließend, damit niemand es für geprüft hält:

- **Kein echtes Debian.** Alles lief auf Windows mit Attrappen, der ulh-Satz in WSL.
- **Kein podman.** Kein Container ist gestartet, kein Ausbruch versucht, kein Abbild geladen.
- **Kein systemd.** Die Dienstdatei ist byteweise geprüft und **nie geladen**; `Restart=on-failure`
  mit 5 Sekunden ist die Antwort auf Punkt 32 und **nicht in Betrieb gesehen**.
- **Keine Dateirechte.** Weder die 0600 von `controller.json` noch die 0700 der Verzeichnisse
  noch Eigentümer `root` sind auf einem Dateisystem geprüft, das sie kennt.
- **`-race` nicht gefahren**, `govulncheck` nicht gefahren, `shellcheck` nicht vorhanden.
- **Die Fuzz-Ziele (Schritt 4 des Plans) sind in dieser Aufgabe nicht gelaufen.** Die Zahlen
  früherer Aufgaben stehen in ihren Berichten (etwa `FuzzParseFrame`, 13 365 806 Läufe ohne
  Fund); hier ist keine neu gemessen.
- **`change_settings` des ulh-Skripts** (die jq-Bearbeitung, die `dbKey` und die Kopplung
  bewahrt) ist **nicht gemessen**: diese Maschine hat kein `jq`. Der Prüfsatz sagt das als
  roter Fall, nicht als stilles Überspringen.
- **Der Durchlauf des Skripts selbst** ist nicht gemessen (siehe Tabelle oben, Nr. 8).
- **Die Versionsnummer 1.0.0** ist gesetzt, aber kein Bau mit
  `-ldflags "-X main.Version=1.0.0"` ist gemacht und kein Programm hat sie ausgegeben.
