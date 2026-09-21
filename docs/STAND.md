# Stand: NGC (NextGenCloud)

**Zuletzt aktualisiert:** 22.09.2026

## Ziel der nächsten Sitzung

Plan 0 abschließen: die letzte kleine Korrekturrunde von T3, dann alles zusammenführen, prüfen und
Thomas die Verträge zur Freigabe vorlegen. Danach Pull Request, Etikett `vertraege-v1` und die
Pläne 1 bis 4 für die vier Teile.

## Stand

| | |
|---|---|
| Erledigt | Plan 0, Tasks 1–21 (Verträge, Testdaten, Anweisungen, Grundgerüst). Abschlussprüfung des Branches, danach Fixplan T1–T4: T1 (strenges JSON, Beweise, Vertrauensliste, Sitzung, Aufgaben), T2 (Kanäle, Ende zu Ende, Tunnel, Konfiguration) und T4 (Doku) fertig und nachgeprüft |
| In Arbeit | T3 (Kern-Aktionen, Store, UI-Pakete, Release-Prüfskript, Datenbank) auf Branch `plan-0-fix-t3`: Korrekturrunde 1 fertig, Nachprüfung verlangt eine kleine Runde 2 |
| Blockiert | nichts |
| Code | nur `contracts/` und die Programm-Grundgerüste; die vier Teile bauen erst nach der Freigabe |

## Nächste Schritte (in dieser Reihenfolge)

1. T3 Runde 2: fehlende Testfälle in `ui.json` (Ordnernamen, Ordner mit Größe, `x` nach PAX, Ordner
   doppelt, gültiger Ordner nach Datei darunter), Race-Tests in `dbtest` mit Spin-Barriere und
   Hinweis „nur mit `-race`“, `verify.sh` verlangt genau eine Zeile, die vier `omitzero`-Felder per
   Test festnageln. Einzelheiten: Protokoll unter `.superpowers/sdd/…/progress.md` (letzte Zeilen).
2. Nachprüfung, dann `plan-0-fix-t3` auf `plan-0-vertraege` umsetzen und vorspulen, Worktree
   entfernen.
3. Gesamtcheck: `scripts/check.sh`, `go test ./...`, jeden Parser fuzzen (`-parallel=4`, Store-Ziele
   mit `-fuzzminimizetime=1s`), `govulncheck ./...`, Abschlussprüfung des ganzen Branches.
4. Thomas gibt die Verträge frei. Danach Pull Request nach `main`, Squash-Merge, Etikett
   `vertraege-v1`.

## Vor dem Bauen zu prüfen

- Sobald die Server per Remote Control verbunden sind: ob sich die Sitzungen gegenseitig
  Nachrichten schicken können (Entwurf 4.7 setzt das voraus).
- Was Thomas bereitstellt: siehe Entwurf 4.7, „Voraussetzungen“ (sieben Server, ein Pi, je ein
  YubiKey 5 NFC und Nitrokey 3 NFC).
- Auf dem ersten Linux-Server nachholen: `go test -race ./...`, `TestEchoService`, die
  Dateirechte-Prüfung in `contracts/config` und `TestReleaseScript` mit echtem `jq` (darf nicht
  übersprungen werden).

## Offene Entscheidungen

- **Release-Stick:** Mit welchem Werkzeug der Hardware-Stick das Release signiert, wird vor dem
  ersten Release entschieden. `ngc-store` signiert nur Test-Releases.
- **Wiederherstellung** (Codes, Zurücksetzen durch einen Admin, Notfallbefehl auf dem Server): noch
  kein Vertrag. Kommt als Vertragsänderung vor dem Plan für das Webinterface.
- Erledigt am 21.09.2026: ulh-Skripte kommen direkt ins öffentliche ulh (Entscheidungsprotokoll).

Alle weiteren Entscheidungen stehen im Entwurf, Kapitel 6.

## Hinweise

- Arbeitskopie für T3: Worktree im Scratchpad der Sitzung vom 21.09.2026 (Branch `plan-0-fix-t3`).
  Fehlt der Ordner, `git worktree prune` und neu anlegen. git dort nur mit
  `GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=safe.directory GIT_CONFIG_VALUE_0=<pfad>`.
- Der claude-mem-Dienst (`bun … worker-service.cjs`) leckt Speicher. Brechen Builds oder
  Fuzz-Läufe ab, zuerst den freien Speicher prüfen.

## Skills

- `superpowers:subagent-driven-development`: Plan 0 und Fixplan ausführen (läuft)
- `superpowers:finishing-a-development-branch`: am Ende

## Artefakte

- Entwurf (freigegeben, mit Entscheidungsprotokoll): [`docs/superpowers/specs/2026-09-19-ngc-kern-design.md`](superpowers/specs/2026-09-19-ngc-kern-design.md)
- Plan 0: [`docs/superpowers/plans/2026-09-19-ngc-plan-0-vertraege-grundgeruest.md`](superpowers/plans/2026-09-19-ngc-plan-0-vertraege-grundgeruest.md)
- Fixplan, Berichte, Protokoll: `.superpowers/sdd/2026-09-19-ngc-plan-0-vertraege-grundgeruest/`
  (nicht im Repo)
- Verträge: [`contracts/README.md`](../contracts/README.md), [`contracts/ulh.md`](../contracts/ulh.md)
- Prototyp: [`prototype/index.html`](../prototype/index.html), Original unter `W:\ngc\index.html`
- Repo: <https://github.com/sorglos-it/nextgencloud> (privat)
- Facebook-Post: zurückgestellt, solange das Repo privat ist; Vermerk in
  `Z:\GitHub\facebook\README.md`, Abschnitt „Bestand“
