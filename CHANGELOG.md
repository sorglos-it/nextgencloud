# Changelog

## Unveröffentlicht

Die Verträge V1 bis V7 zwischen Webinterface, API, Controller und Datenbank stehen: Go-Pakete in
`shared/` mit Testdaten in `shared/testdata/`, die Regeln in `shared/README.md` und `shared/ulh.md`,
dazu die Anweisungen je Teil und das Grundgerüst der Programme `ngc-api`, `ngc-controller` und
`ngc-store`.

Was die Verträge festlegen:

- **Anmeldung und Sitzung:** Passwortschlüssel im Browser, TOTP, Passkeys; Sitzungszertifikate,
  Noise-Sitzung zur API, HTTP-Regeln vor dem WebSocket (Methoden, Statuscodes, Fehler als
  CLOSE-JSON, Cookie).
- **Ersteinrichtung:** Der Einmal-Code geht nie über die Leitung; Browser und API weisen sich mit
  ihm gegenseitig aus.
- **Kanäle:** Frames, Freigaben, Obergrenzen je Sitzung und Tunnel, Ende-zu-Ende-Verschlüsselung
  bis zum Dienst, Ergebnisse nur bei erfolgreichem Ende.
- **Vertrauensliste:** Kette mit Ankern, Admins, Rollen, Stores, Controllern und API-Servern;
  Kopplungs- und Beitrittscodes mit TLS-Fingerabdruck.
- **Apps und Store:** Pakete, Kataloge, Lizenzen, Releases; jede Oberfläche mit signierter
  Prüfsumme, Updates nur mit den Rechten und aus dem Store der Aktivierung.
- **Adressen:** eine Grammatik für Tunnel, Webserver, Passkey-Herkunft, Stores, Datenbankknoten und
  Katalog-URLs, auch für die Installationsskripte.
- **Datenbank:** `dbapi` für MariaDB und SQLite mit gemeinsamen Konformitätstests.
- **Striktes JSON:** feste Regeln und Budgets für alles, was von außen kommt, mit Testdaten für die
  Umsetzung im Browser.

Geprüft: `tools/check.sh` (Format, vet, Tests, Bau für linux/amd64 und arm64), unter Linux auch mit
`-race`, 39 Fuzz-Ziele je 3 Minuten ohne Fund, `govulncheck` ohne erreichbare Lücke, jeder Go-Name
der Dokumente vorhanden.

Aufbau nach dem Projektstruktur-Standard: Teile unter `apps/` (`web`, `server-api`,
`server-controller`, `server-db`, `cli`), Verträge in `shared/` (vorher `contracts/`), Skripte in
`tools/` (vorher `scripts/`), Prototyp in `docs/prototype.html` (vorher `prototype/index.html`).
Der Go-Importpfad der Verträge ist `github.com/sorglos-it/nextgencloud/shared/…`.
