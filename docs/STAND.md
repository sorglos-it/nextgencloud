# Stand: NGC (NextGenCloud)

**Zuletzt aktualisiert:** 22.09.2026

## Ziel der nächsten Sitzung

Den Vertrag für die Wiederherstellung schreiben (Codes, Zurücksetzen durch einen Admin,
Notfallbefehl auf dem Server). Danach die Pläne 1 bis 4 für die vier Teile.

## Stand

| | |
|---|---|
| Erledigt | Plan 0 (Verträge V1–V7, Testdaten, Anweisungen, Grundgerüst), drei Abschlussprüfungen mit je einer Fix-Welle (T1–T4, A–F, dann A–C), Umbau nach dem Projektstruktur-Standard. Endabnahme grün: `tools/check.sh`, Linux mit `-race` (WSL), 39 Fuzz-Ziele je 3 Minuten ohne Fund, `govulncheck` ohne erreichbare Lücke, alle 277 Go-Namen der Dokumente vorhanden |
| In Arbeit | nichts; die Verträge sind freigegeben und liegen auf `main` (Etikett `vertraege-v1`, 22.09.2026), die Module aus Kapitel 7 liegen in `vendor/` |
| Blockiert | nichts |
| Code | nur `shared/` (die Verträge) und die Programm-Grundgerüste unter `apps/`; die vier Teile bauen erst nach der Freigabe |

## Nächste Schritte (in dieser Reihenfolge)

1. Vertrag für die Wiederherstellung (Codes, Zurücksetzen durch einen Admin, Notfallbefehl auf dem
   Server), als Vertragsänderung vor den Plänen 1–4. Braucht eine eigene Schlüsselart oder eine
   Ausnahme für Rollen nur mit Passkey (die Vertrauensliste lehnt dort Passwortschlüssel ab) und
   eine Antwort für hochgeladene Apps, deren freigebender Admin-Schlüssel wechselt.
2. Pläne 1–4 schreiben (Webinterface, API, Controller, Datenbank), dann die vier Sitzungen starten.

## Vor dem Bauen zu prüfen

- Sobald die Server per Remote Control verbunden sind: ob sich die Sitzungen gegenseitig
  Nachrichten schicken können (Entwurf 4.7 setzt das voraus).
- Was Thomas bereitstellt: siehe Entwurf 4.7, „Voraussetzungen“ (sieben Server, ein Pi, je ein
  YubiKey 5 NFC und Nitrokey 3 NFC).
- Auf dem ersten Linux-Server mit echtem `jq`: `TestReleaseScript` (31 Fälle, auch der mit einem
  Null-Byte) muss laufen und grün sein; bisher lief es nur mit einem Ersatz für `jq`.

## Offene Entscheidungen

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
