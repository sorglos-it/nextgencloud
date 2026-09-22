# Werkzeug ngc-store

Gehört zur API (Sitzung API 1). Verträge: V5, V6 (`shared/README.md`).

Aufbau: `main.go` ist der Einstiegspunkt von `ngc-store`, die Logik liegt in `classes/` (Regeln:
`CLAUDE.md` im Stamm, Abschnitt „Aufbau“).

## Aufgabe

Apps packen und verschlüsseln, Kataloge und Test-Lizenzen signieren, Release-Dateien packen und
prüfen, damit jeder einen eigenen Store betreiben kann. Releases signiert es nur zum Testen, mit
einer Schlüsseldatei. Der echte Release-Schlüssel von NGC liegt auf einem Hardware-Stick, nie auf
einem Server (Entwurf 4.8); wie der Stick signiert, wird vor dem ersten Release festgelegt.

## Fertig, wenn

`ngc-store` erzeugt einen Test-Store, aus dem sich eine Kauf-App freischalten lässt.
