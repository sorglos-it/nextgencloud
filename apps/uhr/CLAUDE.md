# App „Uhr" (`ngc-uhr`)

Die zweite App von NGC. Verträge: V3 (Dienstaufruf), V5 (Paket, Manifest, Katalog)
(`shared/README.md`).

Aufbau: `main.go` ist der Einstiegspunkt von `ngc-uhr`, die Logik liegt in `classes/`
(Regeln: `CLAUDE.md` im Stamm, Abschnitt „Aufbau"). In `templates/` liegt die App selbst:
`manifest.json`, der `Containerfile` und `ui/`.

## Aufgabe

Den **Weg** einer App in den Laden belegen, nicht eine App bauen. Sie ist mit Absicht winzig:
eine Uhr braucht kein Netz, kein Root und keinen Speicher, also ist jedes Recht, das sie nicht
anfragt, eines, das der Weg auch nicht braucht. Der Weg steht in `docs/app-weg.md`, mit den
echten Befehlen.

Sie misst eine Sache, die sonst nichts misst: **welche Richtung der Ausgabe ankommt.**
`stdout` geht ins Fenster der App, `stderr` nur ins Protokoll des Controllers — und auch das
nur, wo ein Betreiber den Text eingeschaltet hat. Die Aktion `zeit` schreibt absichtlich in
beide Richtungen, damit das am laufenden System zu sehen ist.

## Nicht im Baum

`templates/services/` und `build/` (`.gitignore`): das Abbild und das übersetzte Programm.
Beide entstehen beim Bauen, siehe `docs/app-weg.md`.

## Fertig, wenn

Die App liegt in einem unterschriebenen Katalog, lässt sich auf einem echten Server
einrichten und öffnen, und ihr Fenster zeigt die Zeit des Servers.
