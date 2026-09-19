# Stand: NGC (NextGenCloud)

**Zuletzt aktualisiert:** 19.09.2026

## Ziel der nächsten Sitzung

Plan 0 abarbeiten: die sieben Verträge als Go-Pakete mit Testvektoren, je eine `CLAUDE.md` für die
vier Teile und das Grundgerüst des Repos. Danach schreibt der Koordinator die Pläne 1 bis 4 für die
vier Teile, die parallel auf den Servern gebaut werden.

## Stand

| | |
|---|---|
| Erledigt | Repo angelegt (privat), Entwurf für den Kern von Thomas am 19.09.2026 freigegeben, Plan 0 geschrieben |
| In Arbeit | Plan 0 ausführen (22 Tasks, Branch `plan-0-vertraege`) |
| Blockiert | Go ist auf Thomas' Rechner nicht installiert (Task 1 fragt nach `winget install --id GoLang.Go -e`) |
| Code | noch keiner, nur der Prototyp |

## Vor dem Bauen zu prüfen

- Sobald die Server per Remote Control verbunden sind: ob sich die Sitzungen gegenseitig
  Nachrichten schicken können (Entwurf 4.7 setzt das voraus).
- Was Thomas bereitstellt: siehe Entwurf 4.7, „Voraussetzungen" (sieben Server, ein Pi, je ein
  YubiKey 5 NFC und Nitrokey 3 NFC).

## Offene Entscheidungen

- **ulh und Unterordner:** Eigene ulh-Quellen brauchen `config.yaml` im Wurzelverzeichnis des Repos
  und einen Ordner `scripts/`. NGC hätte seine Skripte gern unter `ulh/`. Zwei Wege: ulh um eine
  Option `subdir` erweitern (einmal Arbeit in ulh, alles bleibt in einem Repo) oder ein zweites,
  privates Repo `sorglos-it/nextgencloud-ulh`, das nur die Skripte enthält. Empfehlung: `subdir`
  in ulh, weil Skripte und Code sonst auseinanderlaufen. Entscheidung fällt in Task 22 von Plan 0.
- **Wiederherstellung** (Codes, Zurücksetzen durch einen Admin, Notfallbefehl auf dem Server): noch
  kein Vertrag. Kommt als Vertragsänderung vor dem Plan für das Webinterface.

Für den Kern sonst keine. Offene Punkte für spätere Teilprojekte stehen im Entwurf, Kapitel 5.

## Skills

- `superpowers:writing-plans`: Plan 0 aus dem freigegebenen Entwurf (erledigt)
- `superpowers:subagent-driven-development` oder `superpowers:executing-plans`: Plan 0 ausführen

## Artefakte

- Entwurf (freigegeben): [`docs/superpowers/specs/2026-09-19-ngc-kern-design.md`](superpowers/specs/2026-09-19-ngc-kern-design.md)
- Plan 0: [`docs/superpowers/plans/2026-09-19-ngc-plan-0-vertraege-grundgeruest.md`](superpowers/plans/2026-09-19-ngc-plan-0-vertraege-grundgeruest.md)
- Prototyp: [`prototype/index.html`](../prototype/index.html), Original unter `W:\ngc\index.html`
- Repo: <https://github.com/sorglos-it/nextgencloud> (privat)
- Facebook-Post: zurückgestellt, solange das Repo privat ist; Vermerk in
  `Z:\GitHub\facebook\README.md`, Abschnitt „Bestand"
