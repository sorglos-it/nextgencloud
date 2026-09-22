# Stand: NGC (NextGenCloud)

**Zuletzt aktualisiert:** 22.09.2026

## Ziel der nächsten Sitzung

Plan 0 abschließen: die letzte kleine Korrekturrunde von T3, dann alles prüfen und Thomas die
Verträge zur Freigabe vorlegen. Danach Pull Request, Etikett `vertraege-v1` und die Pläne 1 bis 4
für die vier Teile.

## Stand

| | |
|---|---|
| Erledigt | Plan 0, Tasks 1–21 (Verträge, Testdaten, Anweisungen, Grundgerüst). Abschlussprüfung des Branches, danach Fixplan T1–T4: T1, T2 und T4 fertig und nachgeprüft, T3 Runde 1 zusammengeführt. Umbau nach dem Projektstruktur-Standard (22.09.2026), vorher und nachher mit `tools/check.sh` geprüft |
| In Arbeit | T3 Runde 2 (kleine Nachträge aus der Nachprüfung), auf `plan-0-vertraege` |
| Blockiert | nichts |
| Code | nur `shared/` (die Verträge) und die Programm-Grundgerüste unter `apps/`; die vier Teile bauen erst nach der Freigabe |

## Nächste Schritte (in dieser Reihenfolge)

1. T3 Runde 2: fehlende Testfälle in `shared/testdata/ui.json` (Ordnernamen, Ordner mit Größe,
   `x` nach PAX, Ordner doppelt, gültiger Ordner nach Datei darunter), Race-Tests in
   `shared/dbapi/dbtest` mit Spin-Barriere und Hinweis „nur mit `-race`“, `verify.sh` verlangt
   genau eine Zeile, die vier `omitzero`-Felder per Test festnageln. Einzelheiten: Protokoll unter
   `.superpowers/sdd/…/progress.md` (letzte Zeilen), dort noch mit den alten Pfaden.
2. Nachprüfung von Runde 2.
3. Gesamtcheck: `tools/check.sh`, jeden Parser fuzzen (`-parallel=4`, Ziele in `shared/store` mit
   `-fuzzminimizetime=1s`), `govulncheck ./...`, Abschlussprüfung des ganzen Branches.
4. Thomas gibt die Verträge frei. Danach Pull Request nach `main`, Squash-Merge, Etikett
   `vertraege-v1`.

## Vor dem Bauen zu prüfen

- Sobald die Server per Remote Control verbunden sind: ob sich die Sitzungen gegenseitig
  Nachrichten schicken können (Entwurf 4.7 setzt das voraus).
- Was Thomas bereitstellt: siehe Entwurf 4.7, „Voraussetzungen“ (sieben Server, ein Pi, je ein
  YubiKey 5 NFC und Nitrokey 3 NFC).
- Auf dem ersten Linux-Server nachholen: `go test -race ./...`, `TestEchoService`, die
  Dateirechte-Prüfung in `shared/config` und `TestReleaseScript` mit echtem `jq` (darf nicht
  übersprungen werden).

## Offene Entscheidungen

- **Release-Stick:** Mit welchem Werkzeug der Hardware-Stick das Release signiert, wird vor dem
  ersten Release entschieden. `ngc-store` signiert nur Test-Releases.
- **Wiederherstellung** (Codes, Zurücksetzen durch einen Admin, Notfallbefehl auf dem Server): noch
  kein Vertrag. Kommt als Vertragsänderung vor dem Plan für das Webinterface.

Alle weiteren Entscheidungen stehen im Entwurf, Kapitel 6.

## Hinweise

- Aufbau des Repos: `CLAUDE.md` im Stamm, Abschnitt „Aufbau“. `vendor/` bleibt im Stamm, weil Go es
  neben `go.mod` verlangt.
- Der claude-mem-Dienst (`bun … worker-service.cjs`) leckt Speicher. Brechen Builds oder
  Fuzz-Läufe ab, zuerst den freien Speicher prüfen.

## Skills

- `superpowers:subagent-driven-development`: Plan 0 und Fixplan ausführen (läuft)
- `superpowers:finishing-a-development-branch`: am Ende

## Artefakte

- Entwurf (freigegeben, mit Entscheidungsprotokoll): [`docs/superpowers/specs/2026-09-19-ngc-kern-design.md`](superpowers/specs/2026-09-19-ngc-kern-design.md)
- Plan 0: [`docs/superpowers/plans/2026-09-19-ngc-plan-0-vertraege-grundgeruest.md`](superpowers/plans/2026-09-19-ngc-plan-0-vertraege-grundgeruest.md)
  (noch mit den alten Pfaden, Zuordnung oben im Plan)
- Fixplan, Berichte, Protokoll: `.superpowers/sdd/2026-09-19-ngc-plan-0-vertraege-grundgeruest/`
  (nicht im Repo)
- Verträge: [`shared/README.md`](../shared/README.md), [`shared/ulh.md`](../shared/ulh.md)
- Prototyp: [`docs/prototype.html`](prototype.html), Original unter `W:\ngc\index.html`
- Repo: <https://github.com/sorglos-it/nextgencloud> (privat)
- Facebook-Post: zurückgestellt, solange das Repo privat ist; Vermerk in
  `Z:\GitHub\facebook\README.md`, Abschnitt „Bestand“
