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
   (`update.Manager`) ist gebaut und gemessen, **ihr Aufrufer fehlt**: `node.run` setzt das Feld
   nicht. Jeder Install-Kanal endet damit `E_INTERNAL`. (Aufgaben 19/20 und 24.)
2. **`ngc-api migrate` fehlt als Aufruf** (Punkt 28 der Verträge). Der Controller startet die
   Migrationen als Programm — nur gibt es den Unterbefehl noch nicht. `openState` migriert
   allein das **eigene** Schema. Ein `migrate.go` im Controller gibt es bewusst nicht: ein
   Gerüst sähe aus wie eine Zusage.
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
   deren Lizenz abgelaufen ist. `serve.Deps` um Liste und Installation zu erweitern ist der
   Eingriff, der dafür fehlt. (Aufgabe 18, Abweichung 5.)
6. **Das Einrichten einer App-Aktualisierung ist nicht gebaut** (Aufgabe 20, „nicht gebaut,
   benannt"): der Riegel davor ist voll gemessen, die Betriebsfassung von `AppInstaller`
   (`Needs` + `Update`) fehlt, weil `install.Accept` auf eine **bereitgelegte** Datei nicht
   anwendbar ist (`state.Staged` trägt weder `UI` noch `Needs` noch den Katalog-Umschlag) und
   `install.stage` unexportiert ist. Ein zweiter Entpacker wäre gegen `CLAUDE.md`.
7. **`core.Unattended` kann den ERSTEN Einbau eines Teils nie erlauben** (Aufgabe 20,
   BLOCKED 1): `store.Newer("1.2.0", "")` ist **false**, der Kommentar von `core.Unattended`
   sagt das Gegenteil. Die Zeile liegt in eingefrorenem `shared/`.
8. **Was eine `ngc-web`-Release-Datei ist, steht nirgends** (Aufgabe 20, BLOCKED 2):
   `shared/testdata/release/ngc-web-all.tar` ist kein tar, sondern 18 Byte Text.
9. **65 bis 78 Fälle überspringen sich laut**, weil sie Linux brauchen — hier **67** gezählt
   (podman, Systembenutzer, POSIX-Shell, `/dev/ptmx`, Rechte, Plattenplatz). Sie sind kein Grün.
10. **Die Apache-Vorlage** steht seit 12.10.2026 in `shared/ulh.md` und wird von
   `TestBalancerTemplateMatchesTheContract` byteweise gehalten — **gemessen gegen die Vorlage,
   nicht gegen ein laufendes Apache.** `apachectl configtest` ist nie gelaufen.
11. **Das Terminal-Protokoll** (Punkt 29: 1 Byte Art, 2 Byte Länge, Nutzlast) ist eine
   **Abweichung vom Plan** und mit der Sitzung Webinterface **noch abzustimmen**. Wie viele
   Terminals gleichzeitig laufen dürfen, begrenzt seit Aufgabe 24 `serve.DefaultMaxApps = 128`;
   die Zahl ist **gerechnet, nicht gemessen** (etwa 50 MiB und 128 Prozesse).
12. **Der Einmal-Code wird vor der MAC-Prüfung verbraucht** (Punkt 26), an **zwei** Stellen
   (`join.Accept` und `controllers.Pair`). Wer es ändert, ändert beide.
13. **Punkt 31:** Nichts hält `store.Package.Needs` des Katalogeintrags gegen
    `store.Manifest.Needs`. Heute keine Lücke, weil niemand die Katalogkopie als Grundlage einer
    Rechteentscheidung nimmt. Wer das tun will, muss es zuerst dort lesen.

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
