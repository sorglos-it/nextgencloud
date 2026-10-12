# Stand: NGC (NextGenCloud)

**Zuletzt aktualisiert:** 10.10.2026

## Ziel der nächsten Sitzung

Die fünf großen Zweige sind weit gebaut, aber **nichts davon ist je gelaufen**: kein `main.go`
bindet die Pakete ein. Das nächste große Ziel ist deshalb **api-Aufgabe 24** („`main.go`
verdrahten, Dienst und Module") — erst dort zeigt sich, ob die Teile zusammenpassen. Davor die
offenen Aufgaben der vier Teile weiterbauen und die fünf Pull Requests zusammenführen.

## Stand je Zweig (gemessen am 10.10.2026)

| Zweig | Kopf | Gebaut und geprüft |
|---|---|---|
| `main` | `424a198` | Verträge V1.1 (Etikett `vertraege-v1.1`), Arbeitsweise, `messen.md`, V1.2-Punkte 19–24 |
| `web` | `e5f1fcc` | Aufgaben 1–35 plus Apache-Vorlage gehärtet (36) und auf die gemessene Maschine gebracht (37) |
| `api` | `33688a3` | Aufgaben 1–15, dazu das Protokoll (14), die Rechte je Kanal (13) und die Übergabefläche für die Weiterleitung |
| `api-t17` | `6000616` | Aufgabe 17 (Weiterleitung an die Controller) — **fertig, eigener Zweig, noch nicht in `api`** |
| `controller` | `dce4bb8` | Aufgaben 1–15 (Paket annehmen und prüfen, mit `ErrDisk`) |
| `controller-t16` | `863c89b` | Aufgabe 16 (App aktivieren) — **fertig, eigener Zweig, noch nicht in `controller`** |
| `db` | `a48d8a0` | Plan vollständig (1–12), dazu `ADD COLUMN`, Register nach Verzeichnis, Sicherung in Stücken |
| `recovery` | `3b5b1e8` | Aufgaben 1–9. **Abnahmekriterium von Paket 0 erreicht:** PolicyOR aus sieben Zweigen auf swtpm, 30 Fälle grün, dreimal |
| `release-packer` | `61e21f0` | Packer gebaut und geprüft, **noch nicht zusammengeführt** |

Im ulh-Repo (öffentlich, **Thomas schiebt dort, nie ein Agent**): `ngc-web-kopfzeilen` (`75c63f4`,
der vhost kommt aus dem Release) und `ngc-db` (`33dce46`, `backup`/`restore` ohne den Augenblick,
in dem die Daten weg sind). Beide warten.

Offene Pull Requests nach `main`: **#26 web, #27 api, #28 controller, #29 db, #30 recovery**.

## Was beim Abbruch noch lief

Sechs Stränge waren am 10.10.2026 unterwegs, als Thomas „auslaufen lassen" sagte. Ihre Ergebnisse
stehen in den Ledgern, **falls sie fertig geworden sind** — wer hier weitermacht, liest dort zuerst
nach, ob die Aufgabe noch offen ist:

| Strang | Aufgabe | Zweig |
|---|---|---|
| api | 16 — `ngc.users` | `api` |
| api | 19 — Stores und Kataloge | `api-t19` |
| controller | 17 — Deaktivieren, Deinstallieren, Daten löschen | `controller` |
| recovery | 10 — die Negativfälle (ein Stick allein, Replay) | `recovery` |
| packer | Messung: trägt das Archiv die neue Vorlage bitgleich? | `release-packer` |
| db | Abschlussprüfung des ganzen Zweigs | `db` |

## Nächste Schritte (in dieser Reihenfolge)

1. **Die Ledger lesen, nicht den Kopf.** Je Plan `.superpowers/sdd/<plan>/progress.md`. **Jeder
   Ledger hat sein eigenes Wort für „fertig"** — der Recovery-Ledger schreibt `ABGESCHLOSSEN`, die
   anderen `COMPLETE`. Nach dem Wort des Ledgers suchen, nicht nach dem eigenen.
2. Die fertigen Nebenzweige zusammenführen: `api-t17` → `api`, `controller-t16` → `controller`.
3. Die fünf Pull Requests nach `main` zusammenführen, wenn kein Strang mehr in den Zweigen
   arbeitet. Alle fünf führten am 10.10. sauber zusammen.
4. **api-Aufgabe 24** (`main.go` und `classes/server/`): bis dahin gilt für alles „gebaut ist nicht
   gelaufen".
5. Die Auslieferungskette unten abarbeiten — Vorlage, Packer, unterschriebenes Release, dann das
   ulh-Skript.

## Fallen dieser Umgebung (am 10.10.2026 gemessen, alle bezahlt)

Vollständig mit Messungen: `Z:\GitHub\claude-md\messen.md` (gilt für jedes Projekt) und
`docs/arbeitsweise.md` (NGC-eigene Lehren). Die sechs, die am meisten gekostet haben:

- **Arbeitsbäume werden während der Arbeit hohl.** Mehrmals gemessen: ein Baum stand nach einer
  Wiederherstellung bei 0 fehlenden Dateien und kurz darauf wieder bei **629**; parallel 145, 123,
  159 und einmal **alle 1097**. Ursache **nicht gemessen**. Darum: `git diff --name-only
  --diff-filter=D HEAD | wc -l` muss **0** sein, und zwar **vor jeder gemeldeten Zahl im selben
  Aufruf**. Fehlt Fremdcode, scheitert der Bau sichtbar — **fehlt eine Testdatei, läuft der Satz
  mit weniger Fällen und sieht grün aus.** Wiederherstellen nur die **gelöschten** Pfade
  (`git restore --source=HEAD -- <pfade>`), nie `git restore .`.
- **`tools/check.sh` war im Arbeitsbaum kaputt** und ist repariert: `git ls-files` starb an
  „dubious ownership" (Code **128**), und der go-Werkzeugkasten an derselben Frage beim
  VCS-Stempel. **Der Ausstiegscode einer Pipe gehört dem letzten Glied** — mit `| tail` kam **0**,
  während das Skript tot war. **Der Beleg ist die gedruckte Zeile `check: ok`, nicht die Null.**
- **`$?` ist in `wsl -- sh -c '…'` schon tot**, auch in einfachen Anführungszeichen (Sonde:
  im Befehlstext **0**, dieselben Zeilen als **Datei** 7 und 1). Messskript als Datei, Code **in**
  WSL schreiben, Sonde `(exit 7)` vor der ersten echten Messung.
- **`go test` über alle Controller-Pakete stirbt in 3 von 5 Läufen**, und zwar nicht an
  einem Testfall: `fatal error: unknown caller pc`, immer in `scanstack`/`markroot` des
  Müllsammlers der Go-Laufzeit (`go1.27.1`), immer im Paket `classes/install`. **Allein
  ist dasselbe Paket grün**, auch unter künstlicher Last. Am 12.10.2026 gemessen, 3/5 —
  und ebenso 3/5 auf dem Stand **davor** (`01f8a6e`), also nicht von einer Änderung
  verursacht. Daneben gemessen: `go test -race ./apps/server-api/...` scheitert
  reproduzierbar mit `open …httpapi.test.exe: Zugriff verweigert`, was auf den
  Virenschutz (Bitdefender) deutet; die Ursache des Absturzes ist **nicht gemessen**.
  **Daraus folgt für jede Meldung: ein einzelnes grünes `go test ./...` ist hier keine
  Aussage.** Das eigene Paket einzeln messen, und wenn der Gesamtsatz genannt wird, ihn
  mehrfach fahren und die Zahl der roten Läufe mitnennen.
- **`git` stirbt in der Bash mit „Out of memory", während 98 GB frei sind.** Gemessen am
  10.10.2026 bei vielen gleichzeitigen Strängen: `git commit` brach mit „malloc failed (tried to
  allocate 1048576 bytes)" ab, `git push` mit „calloc failed", und derselbe Befehl lief
  **unverändert** über PowerShell (`git -C Z:\GitHub\nextgencloud push`) sauber durch. Es ist nicht
  der Arbeitsspeicher, sondern die Fork-Schicht von Git-für-Windows (dieselbe Messung zeigte
  `post-commit: fork: Resource temporarily unavailable`). **Also: bricht git in der Bash mit
  „Out of memory" ab, erst den Arbeitsspeicher nachsehen, dann denselben Befehl über PowerShell
  wiederholen — und vorher prüfen, ob der Commit schon liegt (`git log --oneline -1`), sonst
  entsteht er zweimal.**
- **Der Scratchpad ist zwischen den Strängen geteilt**, nicht nur `/tmp`: eine Messdatei wurde
  mitten im Lauf von einem anderen Strang überschrieben. Je Strang ein eigener Unterordner, und
  eine Zahl aus einer überschriebenen Datei wird **neu gemessen**, nicht geborgen.

## Wie hier gearbeitet wird (Head-Officer)

- **Jeder Auftrag beginnt mit „sieh nach, ob es diese Aufgabe noch gibt."** Am 10.10. sind zwei
  Stränge auf fertige Arbeit geschickt worden, weil der Koordinator einen Ledger falsch gelesen
  hat; nur weil der Strang nachsah, blieb ein 540-Zeilen-Bericht unversehrt.
- **Greift der Koordinator in einen laufenden Arbeitsbaum, sagt er es dem Strang im selben
  Moment** — mit Datei, Zeit und Grund. Zwei Stränge haben sonst eine Geistersuche gemacht.
- **Eine Planberichtigung hat drei Orte:** Plan, Hauptcheckout, neu erzeugter Auftrag — und
  **innerhalb** des Aufgabenstücks noch Kopf, Rumpf und **Codeblöcke**. Dieselbe Berichtigung
  brauchte am 03.10. drei Anläufe.
- **Dokumente wandern mit dem Zweig.** Ein Strangzweig kann Dutzende Commits hinter `main` liegen;
  ein Verweis auf „Punkt 20" ist dort etwas anderes. **Die Überschrift zitieren, nicht die Nummer**,
  und vor einer Prüfung, die Dokumente heranzieht, `origin/main` in den Zweig ziehen.

## Vor dem Bauen zu prüfen

- Sobald die Server per Remote Control verbunden sind: ob sich die Sitzungen gegenseitig
  Nachrichten schicken können (Entwurf 4.7 setzt das voraus).
- Was Thomas bereitstellt: siehe Entwurf 4.7, „Voraussetzungen“ (sieben Server, ein Pi, je ein
  YubiKey 5 NFC und Nitrokey 3 NFC).
- Auf dem ersten Linux-Server mit echtem `jq`: `TestReleaseScript` (31 Fälle, auch der mit einem
  Null-Byte) muss laufen und grün sein; bisher lief es nur mit einem Ersatz für `jq`.

## Auslieferungskette ulh ↔ Release (zwingende Reihenfolge, Stand 03.10.2026)

Vier Stücke hängen aneinander, und **in der falschen Reihenfolge macht jedes
einzelne die Installation kaputt**. Gemessen, nicht geschätzt:

1. **`apps/web/templates/apache-vhost.conf`** (Zweig `web`, `c5c3591`) — die
   Vorlage, die der Server ausliefert. Sie trägt jetzt die host-weite HSTS, die
   verengte Cipher-Liste, eigene Protokolle und `FallbackResource disabled` im
   assets-Block.
2. **Der Release-Packer** (Zweig `release-packer`, `61e21f0`) — er nimmt diese
   Vorlage namentlich auf (`tools/release/extra-web.txt:30`), sie landet beim
   Nutzer unter `/var/www/ngc/templates/apache-vhost.conf`.
3. **Ein gebautes und unterschriebenes Release**, das dieses Stück enthält.
4. **Der ulh-Zweig `ngc-web-kopfzeilen`** (`75c63f4`) — `ngc-web.sh` **liest**
   die Vorlage aus dem Release, statt sich einen zweiten virtuellen Host zu
   schreiben.

**Warum die Reihenfolge Pflicht ist:** ohne 1-3 findet Schritt 4 die Datei
nicht. Das Skript bricht dann ausdrücklich ab („Update NGC to a release that
carries the file"), ändert nichts — **und damit kann niemand mehr einrichten.**
Dieser ulh-Zweig allein ist also **schlechter** als der Stand davor
(`ad5ad0f`), obwohl er ein Sicherheitsloch schließt.

**Also: 1 und 2 nach `main`, dann ein Release, erst dann darf 4 zusammengeführt
werden.** Das ulh-Repo ist öffentlich und jeder Push nach `main` erreicht per
Auto-Update sofort alle Nutzer — Schritt 4 macht **Thomas**, nicht der Agent.

**Was auf der ersten Maschine mit einem echten Apache noch zu messen ist**,
steht als Befehlsliste in `apps/web/README.md` (neun Zeilen) und als A-D im
Kopf der Vorlage selbst. **Befehl D zählt am meisten:** er prüft, dass eine
fehlende Datei unter `/assets/` von Apache und nicht von PHP beantwortet wird —
sonst stehen dort zwei `Cross-Origin-Resource-Policy`, und die Wand wirkt nicht.

### Nachtrag 10.10.2026: ein Kommentar des Packers ist überholt — vor dem Release berichtigen

Der Packer nimmt `templates/apache-vhost.conf` mit, und der Kommentar daneben
(`tools/release/extra-web.txt`) sagt: *„ngc-web.sh does NOT read it — it writes its own virtual
host, lines 978-1028 — and the task that makes it read this one cannot start before the file is on
the server."*

**Das war am 02.10. richtig und ist es nicht mehr.** Gemessen am 10.10.2026:

| Zweig | Treffer auf `ensure_vhost_template` / `templates/apache-vhost.conf` |
|---|---|
| `ngc-web` (`ad5ad0f`, der alte Stand) | **0** |
| `ngc-web-kopfzeilen` (`75c63f4`, der neue) | **12** |

Das Skript **liest** die Vorlage jetzt, bricht sogar ab, wenn sie fehlt. Der Satz „die Aufgabe, die
es lesen lässt, kann nicht beginnen" beschreibt eine Aufgabe, **die inzwischen erledigt ist**.

**Also, in dieser Reihenfolge:** wenn `ngc-web-kopfzeilen` zusammengeführt wird, im selben Zug den
Kommentar in `extra-web.txt` berichtigen. Er ist kein Beiwerk: er ist der Grund, warum die Datei
überhaupt auf der Liste steht, und der Nächste liest ihn als Begründung.

**Was dagegen gemessen stimmt** (Packer-Nachmessung, 10.10.2026): die **gepackte** Vorlage ist
**bitgleich** zur neuen (`git hash-object` des aus dem Archiv gezogenen Files gleich
`origin/web:apps/web/templates/apache-vhost.conf`, 40 374 Bytes), zwei Packläufe geben dieselbe
Prüfsumme, **66 Dateien, 0 unter `tests/`**, und alles, was die Vorlage nennt und beim Nutzer
liegen muss, liegt im Archiv. Offen bleiben zwei Kleinigkeiten: die mitgelieferte `README.md` und
die Vorlage verweisen auf `docs/STAND.md`, **das nicht im Archiv liegt** (für einen Nutzer ohne
Repo ein Verweis ins Leere), und eine Zusicherung der Dichtheitsprüfung **überspringt weiter laut**,
weil `jq` auf diesem Rechner fehlt.

## Offene Entscheidungen

- **Vertragsfassung 1.2:** Was der nächste Schnitt an `shared/` tragen muss,
  steht gesammelt in [`vertraege-v1.2.md`](vertraege-v1.2.md) — 18 Punkte aus
  allen vier Strängen, jeder mit Herkunft und Messung. Mehrere Module verweisen
  im Code darauf; die Datei ist die Liste, die dort gemeint ist.

- **Release-Stick:** Mit welchem Werkzeug der Hardware-Stick das Release signiert, wird vor dem
  ersten Release entschieden. `ngc-store` signiert nur Test-Releases.
- **Für den Sicherheitsdurchgang (Plan 7) vorgemerkt:** eine feste Bindung der Oberfläche einer
  Store-App an ihren Store, die einen Wechsel des Admin-Schlüssels übersteht; `enc.Budget` soll
  Mitglieder in Rohwerten zählen; eine gemeinsame Prüfung der Form von URLs für alle Verträge.

Alle weiteren Entscheidungen stehen im Entwurf, Kapitel 6.

## Hinweise

- Aufbau des Repos: `CLAUDE.md` im Stamm, Abschnitt „Aufbau“. `vendor/` bleibt im Stamm, weil Go es
  neben `go.mod` verlangt.
- Fremdcode: alle Module aus Entwurf, Kapitel 7 liegen seit dem 22.09.2026 in `vendor/`, aufgezählt
  in `THIRD-PARTY.md`. Was noch kein Teil einbindet, hält `tools/modules.go` fest; wer ein Modul
  zuerst benutzt, nimmt seine Zeile dort heraus.
- Linux-Tests mit `-race` gehen auf Thomas' Rechner nur in WSL (Debian, mit `gcc`); eine
  Go-Toolchain für Linux muss dort vorhanden sein.
- Ein Go-Update mit neuen Unicode-Tabellen ist eine Vertragsänderung (`TestValidText` hält es an).
- Der claude-mem-Dienst (`bun … worker-service.cjs`) leckt Speicher. Brechen Builds oder
  Fuzz-Läufe ab, zuerst den freien Speicher prüfen.

## Skills

- `superpowers:brainstorming` und `superpowers:writing-plans`: Wiederherstellung, dann die Pläne 1–4
- `superpowers:subagent-driven-development`: die Pläne ausführen

## Artefakte

- Pläne der vier Teile (23.09.2026): [Webinterface](superpowers/plans/2026-09-23-ngc-plan-1-web.md),
  [API](superpowers/plans/2026-09-23-ngc-plan-2-api.md),
  [Controller](superpowers/plans/2026-09-23-ngc-plan-3-controller.md),
  [Datenbank](superpowers/plans/2026-09-23-ngc-plan-4-db.md); Recovery:
  [Paket 0](superpowers/plans/2026-09-23-recovery-paket-0-testumgebung.md)
- Testumgebung des Recovery-Strangs: VM `ngc-recovery` (Debian 13, `ssh ngc-recovery`); Pakete
  eingerichtet, `sudo` ohne Passwort, **kein** `/dev/kvm` (Proxmox reicht keine Virtualisierung
  durch), QEMU läuft dort emuliert
- Entwurf (freigegeben, mit Entscheidungsprotokoll): [`docs/superpowers/specs/2026-09-19-ngc-kern-design.md`](superpowers/specs/2026-09-19-ngc-kern-design.md)
- Plan 0: [`docs/superpowers/plans/2026-09-19-ngc-plan-0-vertraege-grundgeruest.md`](superpowers/plans/2026-09-19-ngc-plan-0-vertraege-grundgeruest.md)
  (historisch; maßgeblich sind `shared/` und `shared/README.md`)
- Fixpläne, Berichte, Prüfungen, Protokoll: `.superpowers/sdd/2026-09-19-ngc-plan-0-vertraege-grundgeruest/`
  (nicht im Repo)
- Verträge: [`shared/README.md`](../shared/README.md), [`shared/ulh.md`](../shared/ulh.md)
- Prototyp: [`docs/prototype.html`](prototype.html), Original unter `W:\ngc\index.html`
- Repo: <https://github.com/sorglos-it/nextgencloud> (privat)
- Facebook-Post: zurückgestellt, solange das Repo privat ist; Vermerk in
  `Z:\GitHub\facebook\README.md`, Abschnitt „Bestand“
