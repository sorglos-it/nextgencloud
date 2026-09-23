# NGC – Anweisungen für alle Sitzungen

NGC (NextGenCloud) ist eine offene Alternative zu Synologys DSM: ein Arbeitsplatz im Browser mit
App-Store. Entwurf: `docs/superpowers/specs/2026-09-19-ngc-kern-design.md`. Stand und nächster
Schritt: `docs/STAND.md`. Verträge: `shared/README.md`. Wie Kanäle laufen: dort, Abschnitt
„Kanäle“; was vor dem WebSocket läuft: Abschnitt „Vor dem WebSocket“; wie JSON von außen geprüft
wird: Abschnitt „Striktes JSON“.

Recovery-Modus: `docs/recovery-modus.md` lesen, bevor an Login, Admin-Rechten, Datenbank, Backup,
Installation oder Boot gearbeitet wird.

## Wer macht was

| Teil | Ordner | ulh-Skript | Sitzung auf Server |
|---|---|---|---|
| Webinterface | `apps/web/` | `scripts/ngc-web.sh` | Web |
| API | `apps/server-api/`, Werkzeug `ngc-store` in `apps/cli/` | `scripts/ngc-api.sh` | API 1 |
| Controller | `apps/server-controller/` | `scripts/ngc-controller.sh` | Dienste |
| Datenbank | `apps/server-db/` | `scripts/ngc-db.sh` | DB 1 |
| Recovery-Modus (`docs/recovery-modus.md`): `apps/cli-recovery/`, `apps/cli-stick/`, `apps/server-recovery/`, `lib/rust/`, Images in `tools/` | – | – | Recovery |
| Verträge (`shared/`), `tools/`, `docs/`, Zusammenführen | – | – | Koordinator (Thomas' Rechner) |

Die ulh-Skripte liegen im öffentlichen ulh-Repo (`sorglos-it/ulh`), nicht hier; Regeln dafür in
`shared/ulh.md`. `scripts/ngc-all.sh` (alles auf einem Pi) kommt mit dem Plan für den Pi.

Fremde Ordner sind tabu. `shared/` ändert nur der Koordinator: Wer eine Änderung braucht, fragt
ihn und wartet auf die neue Fassung. Fragen an einen Nachbarn gehen direkt an dessen Sitzung.

## Aufbau

Das Repo folgt dem Projektstruktur-Standard: Teilprogramme unter `apps/`, die Verträge in
`shared/`, Skripte in `tools/`, Dokumente in `docs/`. Neue Ordner nur mit Namen aus dem Standard.

- Ein Go-Teil hat direkt nur `main.go` (Einstiegspunkt), `VERSION` und `CLAUDE.md`. Die Logik
  liegt in `classes/`, ein Go-Paket; wächst es über etwa 20 Dateien, bekommt jedes Paket einen
  Unterordner (`classes/session/`). Go-Tests liegen neben dem Code, wie Go es verlangt.
- Dazu, wenn nötig: `sql/` (Migrationen), `config/` (nur `*.example`), `templates/`, `lang/`.
- Go ist ein Modul im Stamm (`go.mod`). Deshalb liegt `vendor/` im Stamm: Go verlangt es neben
  `go.mod`, als einzige Ausnahme vom Standard.
- Programme heißen nach ihrem Softwareteil: `go build -o ngc-api ./apps/server-api`, ebenso
  `ngc-controller` und `ngc-store`. Die Version steht in `VERSION` des Teils und kommt beim Bauen
  mit `-ldflags "-X main.Version=…"` ins Programm.

## Regeln für sicheres Programmieren (Entwurf 4.6, verbindlich)

- SQL nur mit Platzhaltern, Tabellen- und Spaltennamen nur aus festen Listen.
- Programme ohne Shell starten, immer mit Argumentliste, nie `sh -c`.
- Jede Eingabe von außen prüfen: Typ, Länge, erlaubte Werte; unbekannte, doppelte oder anders
  geschriebene JSON-Feldnamen ablehnen (`enc.DecodeStrict`, mit Budget `enc.Budget`). Immer über
  die `Parse…`- oder `Verify…`-Funktion des Vertrags.
- Nichts, was von außen kommt, als HTML einfügen (`textContent`, nie `innerHTML`).
- Pakete sicher entpacken: keine absoluten Pfade, kein `..`, keine Links, keine Sparse-Dateien, feste
  Größen (`store.Walk`, für Oberflächen `store.WalkUI`).
- Überall Obergrenzen: Nachrichtengröße, Kanäle je Sitzung und je Tunnel, Freigabe je Kanal,
  Anmeldeversuche.
- Kryptografie nur aus der Standardbibliothek oder aus `vendor/`, nichts selbst gebaut.
- TLS zwischen NGC-Teilen nie ungeprüft: API-Server über ihren Fingerabdruck (`tunnel.PinnedTLS`),
  MariaDB über die CA der Installation (`config.DB.TLSConfig`).
- Adressen nur nach der Grammatik in `trust` (`trust.ValidHost`, `trust.ValidTunnelURL`,
  `trust.ValidWebURL`, `trust.ValidStoreURL`, `trust.ValidHostPort`, `trust.ValidOrigin`);
  nichts davon ungeprüft in Konfigurationen anderer Programme (Apache, Datenbanktreiber)
  schreiben.
- Geheimnisse nie ins Log, auch nicht gekürzt (`config.Secret`, `config.SecretText`).

## Technik

- Go 1.27 (`go.mod`: `go 1.27`, `toolchain go1.27.1`), Bau für linux/amd64 und linux/arm64 mit
  `CGO_ENABLED=0`. Ziel sind Debian und Ubuntu. Windows wird nicht umgesetzt.
- Texte für Menschen prüft `wire.ValidText` mit den Unicode-Tabellen von Go (Unicode 17.0 mit
  Go 1.27). Ein Go-Update, das neue Unicode-Tabellen bringt, ist deshalb eine Vertragsänderung:
  `TestValidText` in `shared/wire` schlägt fehl, bis Doku und Testdaten nachgezogen sind.
- Keine Laufzeitabhängigkeiten: Fremdcode nur per `go mod vendor`, mit Zeile in `THIRD-PARTY.md`,
  Lizenz vereinbar mit der AGPL-3.0. Neue Module vorher beim Koordinator anfragen.
- Die Module für die vier Teile (SQLite, MariaDB-Treiber, WebSocket, Passkeys, TOTP) liegen schon
  in `vendor/`. Solange kein Teil sie einbindet, hält `tools/modules.go` sie fest; wer eines zuerst
  benutzt, streicht dort seine Zeile.
- Webinterface ohne Framework und ohne Build-Schritt; npm nur in `apps/web/tests/`.
- Sprache: Code, Bezeichner, Kommentare und Commit-Nachrichten Englisch. Dokumente, Oberfläche und
  Meldungen für Benutzer Deutsch.
- Kommentare erklären, warum etwas so ist, nicht was die Zeile tut.

## Tests

- `go test ./...` muss grün sein, `tools/check.sh` vor jedem Commit.
- Auf den Linux-Servern zusätzlich `go test -race ./...` laufen lassen (auf Thomas' Windows-Rechner
  geht das nur in WSL, dort am 22.09.2026 grün); die Tests, die sich unter Windows oder
  ohne ihre Werkzeuge selbst überspringen (`TestEchoService` in `shared/service`,
  `TestReleaseScript` in `shared/store` mit 31 Fällen, der `openssl`, das echte `jq` und
  `mktemp` braucht, `TestTLSPinScript` in `shared/tunnel`, der `openssl` braucht,
  `TestHostScript` in `shared/trust`, der `sh` und `grep` braucht, die
  Dateirechte-Prüfung in `shared/config`), müssen dort grün sein.
- Jeder Parser bekommt einen Fuzz-Test (`go test -fuzz`), jede Kryptografie einen Test gegen die
  Vektoren in `shared/testdata/`. Fuzz-Ziele in `shared/store` nur mit
  `-fuzzminimizetime=1s`, sonst bleibt das Verkleinern der Eingaben hängen.
- In `shared/trust/trusttest` enthält `Genesis` zwei nach ID sortierte Admins: `Users[0]` ist der
  Passkey-Admin, `Users[1]` der Passwort-Admin aus `NewSigner(1)`.
- `shared/trust/trusttest` nur in Tests importieren; `tools/check.sh` bricht sonst ab.
- Fremdcode prüfen: `govulncheck ./...`.
- Keine Tests löschen oder überspringen, um grün zu werden.

## Git

- Ein Branch je Teil (`web`, `api`, `controller`, `db`, `recovery`), Pull Request nach `main`. Der Koordinator
  prüft jeden Pull Request auf Vertrag und Sicherheit und führt ihn zusammen.
- Im ulh-Repo genauso: Branch `ngc-<teil>`, Pull Request nach dessen `main`. Nur geprüfte Skripte,
  denn jede ulh-Installation holt beim Start `main`.
- Kleine Commits, englische Nachricht in der Befehlsform, Trailer
  `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`.
- Nie mit Gewalt schieben, nie fremde Branches ändern.

## Wenn etwas unklar ist

Lieber fragen als raten. Was nicht im Repo steht, gilt nicht: Jede Entscheidung kommt in das
Entscheidungsprotokoll des Entwurfs.
