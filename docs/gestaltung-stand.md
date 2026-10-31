# Gestaltung: was davon steht

Stand 25.10.2026, Zweig `ngc-oberflaeche2`. Gemessen am Quelltext und an
Playwright-Fällen, nicht angenommen.

Die Vorgabe ist `docs/gestaltung.md`. Sie ist das Ergebnis eines Gesprächs mit
Thomas am 24.10.2026 und wird hier nicht neu verhandelt — diese Datei sagt nur,
wo die Oberfläche gegenüber ihr steht.

| Punkt | Vorher (origin/main) | Jetzt | Abweichung |
|---|---|---|---|
| 1 Handy | Symbole aus unter 640 px; der Weg über das Startmenü war da, aber von keinem Fall gehalten | unverändert, und gemessen: jede App ist auf 375 px über das Startmenü erreichbar | nein |
| 2 Fenster bleiben Fenster | beim Schieben blieben 60 × 34 px stehen, der Rest durfte hinaus | das ganze Fenster bleibt drin; ein Fenster, das nicht mehr hineinpasst, wird zugeschnitten | nein |
| 3 Erster Schreibtisch nicht leer | leere Liste: gar nichts; Startmenü sagte „Keine Treffer" | ein Satz und der Store-Knopf; das Startmenü sagt denselben Satz | nein |
| 4 Fehler als Popup über ihrem Fenster | ein Streifen unten für alles | Meldung einer App als Popup über deren Fenster; der Streifen bleibt für das, was keiner App gehört | nein |
| 5 Eng oder luftig | gab es nicht | drei Stufen im Startmenü unter „Aussehen", zwei Marken in `shell.css` | **teils** — siehe unten |
| 6 Name der Anlage oben, NGC klein unten | weder noch | „läuft mit NGC &lt;Fassung&gt;" steht klein unter dem Anmeldekasten | **ja** — siehe unten |
| 7 Hell/dunkel: System gibt vor | nur zwei Werte, „dem System folgen" war keine Wahl | dritte Wahl, und sie ist die Vorgabe; zieht sich ohne Neuladen nach | nein |
| 8 Vorlagen wie bei Joomla/PrestaShop | gab es nicht | gibt es nicht | **ja** — siehe unten |

## Was offen ist, und warum es nicht halb gebaut wurde

**Punkt 6, Name und Zeichen der Anlage.** Die Vorgabe sagt im selben Absatz,
dass *nicht entschieden ist*, wo beides gespeichert wird und wer es ändern
darf. `/config.json` trägt beides nicht, und `web.json` — die einzige Datei,
die der Web-Controller liest — hat eine feste Feldliste, die ein unbekanntes
Feld zurückweist (`apps/web/classes/Config.php`). Ein Feld dort anzulegen wäre
die Entscheidung über Ablage und Recht, still getroffen in einer Änderung an
einer Anmeldemaske. Die Überschrift bleibt „Bei NGC anmelden", bis das
entschieden ist. Gebaut ist nur die Hälfte, die keine Ablage braucht: die
Zeile unten.

**Punkt 5, die Apps.** Eine App läuft in einem Rahmen mit eigenem Ursprung und
bringt ihr eigenes Aussehen mit; nichts aus `shell.css` reicht dort hinein. Die
Dichte gilt deshalb für alles, was die Oberfläche selbst zeichnet, und nicht
für das Innere der beiden mitgelieferten Apps. Das zu schließen heißt, die
Dichte in den App-Vertrag aufzunehmen (so wie heute Thema und Akzentfarbe) —
ein eigener Auftrag, kein Nebensatz hier.

**Punkt 8, Vorlagen.** Nicht angefangen, und das mit Absicht. Die Vorgabe nennt
selbst den Punkt, der *vor* dem Bauen zu klären ist: eine Vorlage darf auch das
Aussehen einer App ersetzen, und genau dort steht der Riegel, der eine App vom
Rest der Oberfläche trennt (`shell/appframe.js`). Wie weit eine Vorlage in eine
App darf, ohne diesen Riegel zu lösen, ist eine Sicherheitsfrage und gehört in
den Bau-Auftrag. Ein halb gebauter Vorlagen-Ordner wäre genau die stille
Abweichung, die nicht entstehen soll.

## Zwei Dinge, die dabei nicht aufgeweicht wurden

Beide stehen mehrfach im Entscheidungsbuch und sind teuer bezahlt:

- **Fremder Text geht als Text in die Seite.** Das Popup aus Punkt 4 ist ein
  zweiter Weg hinein, also hat es denselben Fall bekommen wie der Streifen:
  `<img src=x onerror=…>` steht als Zeichenkette da, und im Popup ist kein
  einziges Element, das daraus gebaut wurde.
- **Ein leerer Zustand sieht nicht wie ein Angriff aus.** Der Satz aus Punkt 3
  erscheint *nur*, nachdem wirklich eine leere Liste angekommen ist. Eine Liste,
  die nicht geladen werden konnte, ist etwas anderes: dann bleibt der
  Schreibtisch leer und der Streifen sagt, was los ist. Beide Fälle sind
  gemessen, einer gegen den anderen.

## Wo die Bilder liegen

`D:\claude\working\ngc-oberflaeche-bilder\uebersicht.html` — vorher und nachher
nebeneinander, fünfzehn Screens. Neu aufnehmen:

```
NGC_BILDER=<ordner> NGC_WEB_TEST_PORT=<port> npx playwright test \
  shell/bilder.spec.js --project=chromium
```

`apps/web/tests/shell/bilder.spec.js` steht still, solange `NGC_BILDER` keinen
Ordner nennt: es prüft fast nichts, und ein Lauf, der nur Bilder macht und
grün ist, sagt nichts über die Oberfläche.
