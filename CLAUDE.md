# NGC – Anweisungen für alle Sitzungen

NGC (NextGenCloud) ist eine offene Alternative zu Synologys DSM: ein Arbeitsplatz im Browser mit
App-Store. Entwurf: `docs/superpowers/specs/2026-09-19-ngc-kern-design.md`. Stand und nächster
Schritt: `docs/STAND.md`. Verträge: `contracts/README.md`.

## Wer macht was

| Teil | Ordner | Sitzung auf Server |
|---|---|---|
| Webinterface | `web/`, `ulh/web/` | Web |
| API | `api/`, `ulh/api/` | API 1 |
| Controller | `controller/`, `ulh/controller/` | Dienste |
| Datenbank | `db/`, `ulh/db/` | DB 1 |
| Verträge, `contracts/`, Zusammenführen | – | Koordinator (Thomas' Rechner) |

Fremde Ordner sind tabu. `contracts/` ändert nur der Koordinator: Wer eine Änderung braucht, fragt
ihn und wartet auf die neue Fassung. Fragen an einen Nachbarn gehen direkt an dessen Sitzung.

## Regeln für sicheres Programmieren (Entwurf 4.6, verbindlich)

- SQL nur mit Platzhaltern, Tabellen- und Spaltennamen nur aus festen Listen.
- Programme ohne Shell starten, immer mit Argumentliste, nie `sh -c`.
- Jede Eingabe von außen prüfen: Typ, Länge, erlaubte Werte; unbekannte JSON-Felder ablehnen. Immer
  über die `Parse…`- oder `Verify…`-Funktion des Vertrags.
- Nichts, was von außen kommt, als HTML einfügen (`textContent`, nie `innerHTML`).
- Pakete sicher entpacken: keine absoluten Pfade, kein `..`, keine Links, feste Größen (`store.Walk`).
- Überall Obergrenzen: Nachrichtengröße, Kanäle je Sitzung, Anmeldeversuche.
- Kryptografie nur aus der Standardbibliothek oder aus `vendor/`, nichts selbst gebaut.
- Geheimnisse nie ins Log, auch nicht gekürzt.

## Technik

- Go 1.27 (`go.mod`: `go 1.27`, `toolchain go1.27.1`), Bau für linux/amd64 und linux/arm64 mit
  `CGO_ENABLED=0`. Ziel sind Debian und Ubuntu. Windows wird nicht umgesetzt.
- Keine Laufzeitabhängigkeiten: Fremdcode nur per `go mod vendor`, mit Zeile in `THIRD-PARTY.md`,
  Lizenz vereinbar mit der AGPL-3.0. Neue Module vorher beim Koordinator anfragen.
- Webinterface ohne Framework und ohne Build-Schritt; npm nur in `tests/browser/`.
- Sprache: Code, Bezeichner, Kommentare und Commit-Nachrichten Englisch. Dokumente, Oberfläche und
  Meldungen für Benutzer Deutsch.
- Kommentare erklären, warum etwas so ist, nicht was die Zeile tut.

## Tests

- `go test ./...` muss grün sein, `scripts/check.sh` vor jedem Commit.
- Auf den Linux-Servern zusätzlich `go test -race ./...` laufen lassen (Thomas' Windows-Rechner hat
  kein cgo, der Data-Race-Detektor lief dort noch nicht); die beiden Tests, die sich unter Windows
  selbst überspringen (`TestEchoService` in `contracts/service`, die Dateirechte-Prüfung in
  `contracts/config`), müssen dort grün sein.
- Jeder Parser bekommt einen Fuzz-Test (`go test -fuzz`), jede Kryptografie einen Test gegen die
  Vektoren in `contracts/testdata/`.
- In `contracts/trust/trusttest` enthält `Genesis` zwei nach ID sortierte Admins: `Users[0]` ist der
  Passkey-Admin, `Users[1]` der Passwort-Admin aus `NewSigner(1)`.
- Fremdcode prüfen: `govulncheck ./...`.
- Keine Tests löschen oder überspringen, um grün zu werden.

## Git

- Ein Branch je Teil (`web`, `api`, `controller`, `db`), Pull Request nach `main`. Der Koordinator
  prüft jeden Pull Request auf Vertrag und Sicherheit und führt ihn zusammen.
- Kleine Commits, englische Nachricht in der Befehlsform, Trailer
  `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`.
- Nie mit Gewalt schieben, nie fremde Branches ändern.

## Wenn etwas unklar ist

Lieber fragen als raten. Was nicht im Repo steht, gilt nicht: Jede Entscheidung kommt in das
Entscheidungsprotokoll des Entwurfs.
