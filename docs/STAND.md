# Stand: NGC (NextGenCloud)

**Zuletzt aktualisiert:** 23.09.2026

## Ziel der nächsten Sitzung

Die Vertragsänderung V1.1 abschließen und mit `vertraege-v1.1` etikettieren, den überarbeiteten
Plan für Recovery-Paket 0 abnehmen und dann die fünf Sitzungen starten: Web, API, Controller,
Datenbank und Recovery.

## Stand

| | |
|---|---|
| Erledigt | Plan 0 (Verträge V1–V7, Testdaten, Anweisungen, Grundgerüst), drei Abschlussprüfungen mit je einer Fix-Welle (T1–T4, A–F, dann A–C), Umbau nach dem Projektstruktur-Standard. Endabnahme grün: `tools/check.sh`, Linux mit `-race` (WSL), 39 Fuzz-Ziele je 3 Minuten ohne Fund, `govulncheck` ohne erreichbare Lücke, alle 277 Go-Namen der Dokumente vorhanden. Dazu die Pläne der vier Teile (Web 27, API 30, Controller 25, Datenbank 12 Aufgaben) und der Plan für Recovery-Paket 0 |
| In Arbeit | Vertragsänderung V1.1 (Passkey-Anmeldung, von Thomas freigegeben) und die Überarbeitung des Plans für Recovery-Paket 0 |
| Blockiert | nichts |
| Code | nur `shared/` (die Verträge) und die Programm-Grundgerüste unter `apps/`; die vier Teile bauen erst nach der Freigabe |

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
