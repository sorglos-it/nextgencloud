# Changelog

## Unveröffentlicht

- Verträge V1 bis V7 zwischen Webinterface, API, Controller und Datenbank: Go-Pakete in `shared/`
  mit Testdaten in `shared/testdata/`, Regeln in `shared/README.md` und `shared/ulh.md`.
- Grundgerüst der Programme `ngc-api`, `ngc-controller` und `ngc-store`, Anweisungen je Teil.
- Aufbau nach dem Projektstruktur-Standard: Teile unter `apps/` (`web`, `server-api`,
  `server-controller`, `server-db`, `cli`), Verträge in `shared/` (vorher `contracts/`), Skripte in
  `tools/` (vorher `scripts/`), Prototyp in `docs/prototype.html` (vorher `prototype/index.html`).
  Der Go-Importpfad der Verträge ist jetzt `github.com/sorglos-it/nextgencloud/shared/…`.
