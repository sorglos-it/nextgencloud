# Stand: NGC (NextGenCloud)

**Zuletzt aktualisiert:** 01.10.2026

## Ziel der nächsten Sitzung

Die Vertragsänderung V1.1 abschließen und mit `vertraege-v1.1` etikettieren, den überarbeiteten
Plan für Recovery-Paket 0 abnehmen und dann die fünf Sitzungen starten: Web, API, Controller,
Datenbank und Recovery.

## Stand

| | |
|---|---|
| Erledigt | Plan 0 (Verträge V1–V7, Testdaten, Anweisungen, Grundgerüst), drei Abschlussprüfungen mit je einer Fix-Welle (T1–T4, A–F, dann A–C), Umbau nach dem Projektstruktur-Standard. Endabnahme grün: `tools/check.sh`, Linux mit `-race` (WSL), 39 Fuzz-Ziele je 3 Minuten ohne Fund, `govulncheck` ohne erreichbare Lücke, alle 277 Go-Namen der Dokumente vorhanden. Dazu die Pläne der vier Teile (Web 27, API 30, Controller 25, Datenbank 12 Aufgaben) und der Plan für Recovery-Paket 0 |
| In Arbeit | Vertragsänderung V1.1 (Passkey-Anmeldung, von Thomas freigegeben) und die Überarbeitung des Plans für Recovery-Paket 0 |
| Plan 1 (Web) | umgesetzt: `apps/web` 0.2.0. Verträge in JavaScript, Anmeldung in beiden Stufen, Ersteinrichtung, Sitzung mit Noise und Ende zu Ende, Desktop, abgeschottete App-Fenster, vier Oberflächen (App-Store, Benutzer & Rechte, Rechner, Terminal), Browser-Tests gegen Chrome, Firefox und Safari. Bei Abschluss von Aufgabe 27 liefen noch vier Aufgaben: Adapter Desktop ↔ Sitzung (17b), Oberfläche der Ersteinrichtung (19), Rechner im Rahmen (25), Installationsskript (26) |
| Blockiert | nichts |
| Code | `shared/` (die Verträge), `apps/web` (fertig, siehe oben) und die Programm-Grundgerüste der übrigen Teile unter `apps/` |

## Nächste Schritte (in dieser Reihenfolge)

1. Vertragsänderung V1.1 zu Ende bringen (`session.LoginChallenge` nennt je Passkey `credId` und
   `kid`), Testdaten und die Pläne 1 und 2 nachziehen, Etikett `vertraege-v1.1` setzen.
2. Den überarbeiteten Plan für Recovery-Paket 0 abnehmen; die VM `ngc-recovery` steht bereit.
3. Die fünf Sitzungen starten: Web, API, Controller, Datenbank, Recovery.
4. Recovery-Modus nach `docs/recovery-modus.md`: `AdminVerify` kommt nach Go, weil die Anmeldung
   in der API liegt (`apps/web` ist nur Einstieg und Oberfläche); `server-recovery` wäre ein
   neues Teilprogramm in Rust; `lib/` als zentrale Bibliothek gibt es hier noch nicht; und die
   signierten Admin-Datensätze müssen zur Vertrauensliste passen (Rollen nur mit Passkey,
   Wechsel des Admin-Schlüssels bei hochgeladenen Apps).

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
- **Aus Aufgabe 27 (Web), je mit Messung im Aufgabenbericht:**
  - **Zurückgedrehte Vertrauensliste:** Liefert die API auf `from=N+1` eine ältere, gültig
    unterschriebene Fassung, wirft die Shell heute ihre geprüfte Liste weg und schlägt Alarm.
    Entwurf 4.6 will das Gegenteil: ablehnen, ohne Alarm, bei der neueren bleiben. Sonst kann eine
    übernommene API jede Sitzung abmelden. Entscheiden, dann `shell/trustlist.js` nachziehen.
  - **`wire.MaxCallResult` auf Ende-zu-Ende-Kanälen:** `net/channel.js` setzt die Grenze nur, wenn
    der Aufrufer sie nennt. `channel.call` nennt sie, `channel.openE2E` nicht – richtig für eine
    Aktion mit `stream`, offen für eine ohne. Festlegen, wer sie dort setzt, bevor die erste
    solche App kommt.
  - ~~**Sicherheitskopfzeilen für `assets/`**~~ – erledigt mit Aufgabe 29 (Web):
    `apps/web/templates/apache-vhost.conf` ist die Vorlage für den virtuellen Host und setzt für
    `assets/` `nosniff`, `Cross-Origin-Resource-Policy`, HSTS und eine **eigene** Richtlinie
    (`default-src 'none'; script-src 'self'` – nicht die der Seite, siehe Begründung in der Datei).
    `tests/shell/csp.spec.js` prüft es an einer `.js`, einer `.css`, einer Datei ohne Endung und
    einer, die es nicht gibt; die Mock-API liest die Werte aus der Vorlage, damit es keine zweite
    Fassung gibt. `scripts/ngc-web.sh` im ulh-Repo übernimmt die Vorlage noch nicht – das ist der
    offene Rest.
  - **`HEAD` auf `/`** antwortet 405 statt wie `GET`, und ein 405 trägt keine `Allow`-Kopfzeile.
    Kein Loch, aber nicht nach HTTP. Lassen oder ändern.

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
