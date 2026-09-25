# NGC Plan Spiele: Kleine Spiele für den Desktop

> **Stand:** Entwurf vom 25.09.2026, noch nicht abgestimmt. Die offenen Fragen am Ende muss
> Thomas entscheiden. Danach kommen die Entscheidungen ins Protokoll des Entwurfs, und aus dem
> Plan wird ein Umsetzungsplan mit Aufgaben im Stil von Plan 1.

**Ziel:** NGC bekommt eine Handvoll kleiner, klassischer Spiele. Sie sind für den Charme da,
wie Solitär und Minesweeper bei Windows. Außerdem sind sie gute erste Apps für das Paketformat,
weil sie nur eine Oberfläche haben und keinen Dienst brauchen.

**Entwurf:** [`docs/superpowers/specs/2026-09-19-ngc-kern-design.md`](../specs/2026-09-19-ngc-kern-design.md),
Abschnitt 4.4 (App-Paket, „Im Browser“) · **Verträge:** [`shared/README.md`](../../../shared/README.md)

---

## 1. Welche Spiele

Die erste Welle umfasst fünf Spiele. Alle laufen komplett im Browser, allein und ohne Netz.

| Nr. | Spiel | Paket (Vorschlag) | Umfang in der ersten Fassung | Aufwand |
|---|---|---|---|---|
| 1 | **Minensucher** | `org.sorglos.minensucher` | Drei Stufen (9×9/10, 16×16/40, 30×16/99) und eine eigene Größe; erster Klick nie auf eine Mine; Fahnen und Fragezeichen; Doppelklick deckt Nachbarn auf; Zeit und Minenzähler | klein |
| 2 | **2048** | `org.sorglos.2048` | 4×4-Feld; Pfeiltasten, WASD und Wischen; Punkte; Rückgängig für einen Zug; Weiterspielen nach 2048 | klein |
| 3 | **Solitär** | `org.sorglos.solitaer` | Klondike mit 1 oder 3 Karten ziehen; Ziehen und Ablegen mit Maus und Finger; Doppelklick legt auf den Stapel ab; Rückgängig ohne Grenze; automatisches Ablegen am Ende | mittel |
| 4 | **Sudoku** | `org.sorglos.sudoku` | Vier Stufen; jedes Rätsel hat genau eine Lösung; Notizen (Kandidaten); Fehler anzeigen ein- und ausschaltbar; Hinweis | mittel |
| 5 | **Schach** | `org.sorglos.schach` | Zwei Spieler an einem Gerät; alle Regeln (Rochade, en passant, Umwandlung, Patt, 50 Züge, dreifache Wiederholung); Zugliste in Notation; Brett drehen | mittel |

Die Namen sind bewusst deutsch und allgemein. „Minesweeper“ und „Tetris“ sind Markennamen, darum
heißt das Spiel „Minensucher“, und ein Fallende-Blöcke-Spiel kommt höchstens unter eigenem Namen.
„2048“ ist ein freies Spiel (MIT); wir bauen es neu und übernehmen keinen Code.

**Später, wenn die erste Welle steht:**

- **Schach gegen den Computer:** eine kleine eigene Suche (Alpha-Beta, wenige Halbzüge), in einem
  Web Worker, damit das Fenster nicht hängt. Keine fremde Engine, die bräuchte Vendoring und
  Lizenzprüfung.
- **Mahjong-Solitär**, **Snake**, **Vier gewinnt**, **Dame**, **Spider-Solitär** (nutzt die Karten
  aus Solitär wieder).
- **Bestenlisten und Spielstände über Sitzungen hinweg** (siehe Abschnitt 3).
- **Mehrspieler zwischen Benutzern der Installation** (Schach, Vier gewinnt). Das braucht einen
  Dienst und ist ein eigener Plan.

## 2. Wie die Spiele gebaut sind

Die Spiele halten sich an dieselben Regeln wie jede andere App.

- **Paket nur mit Oberfläche:** `manifest.json` und `ui/`, kein `services/`. Die einzige Aktion ist
  das Grundrecht „benutzen“. So kann ein Admin Spiele für einzelne Rollen ausblenden, zum
  Beispiel in einer Firma.
- **Abgeschotteter iframe:** Die Spiele laufen im iframe mit CSP wie alle Apps. Sie laden nichts
  nach, bauen keine Verbindungen auf und haben keinen Zugriff auf `localStorage`.
- **Kein Framework, kein Build-Schritt:** HTML, CSS und ES-Module wie im Webinterface. Kein
  `eval`, kein `new Function`, kein `innerHTML`. Texte kommen über `textContent`, Grafik über
  Canvas oder SVG-Elemente, die mit `createElementNS` gebaut werden.
- **Jede Fenstergröße:** Das Spielfeld skaliert mit dem Fenster und später mit dem Vollbild auf
  Handy und Tablet. Jedes Spiel lässt sich mit Maus, Tastatur und Finger bedienen.
- **Hell und dunkel:** Die Farben kommen aus `ngc.js` (Modus und Akzentfarbe). Die Farben für
  Karten, Figuren und Zahlen sind so gewählt, dass sie auch bei Farbenblindheit unterscheidbar
  sind. Beim Minensucher und bei 2048 stehen außerdem Zahlen auf den Feldern.
- **Zufall:** Der Zufall kommt aus `crypto.getRandomValues`. Das ist nicht aus
  Sicherheitsgründen nötig, aber es ist überall vorhanden und liefert gleichmäßig verteilte
  Zahlen, anders als ein naives `Math.random() * n`. Ein Spiel lässt sich zusätzlich aus einem
  festen Startwert erzeugen, damit Tests immer dieselbe Partie bekommen.
- **Spiellogik getrennt von der Anzeige:** Jedes Spiel hat ein Modul `logic.js` ohne DOM, das
  nur Zustand und Züge kennt, und ein Modul `view.js` für die Anzeige. So lässt sich die Logik
  ohne Browser testen.
- **Gemeinsame Teile:** Zeitanzeige, Menü „Neues Spiel / Rückgängig / Hilfe“, Dialog „Gewonnen“
  und der Zufallsgenerator mit Startwert kommen in ein kleines gemeinsames Modul. Weil jede App
  ihr eigenes Paket ist, wird es beim Packen in jedes Paket kopiert, nicht zur Laufzeit geteilt.
- **Sprache:** Oberfläche und Hilfe auf Deutsch, Code und Kommentare auf Englisch.

## 3. Spielstände und Bestenlisten

Laut Entwurf kann eine App Daten nur über ihren eigenen Dienst speichern. Die Spiele haben in
der ersten Fassung keinen Dienst. Deshalb gilt zunächst:

- Eine laufende Partie bleibt erhalten, solange das Fenster offen ist.
- Bestzeiten und Punkte gelten nur für diese Sitzung.

Für später gibt es drei Wege. Die Entscheidung fällt in Frage 2.

| Weg | Wie | Vorteil | Nachteil |
|---|---|---|---|
| A | Jedes Spiel bekommt einen winzigen Dienst, der Stand und Bestenliste speichert | Passt ohne Änderung in den Entwurf | Fünf Container für fünf kleine Spiele |
| B | Eine App „Spiele“ mit einem Dienst für alle Spiele | Ein Dienst, eine gemeinsame Bestenliste | Die Spiele sind dann keine einzelnen Apps mehr |
| C | `ngc.js` bekommt einen kleinen Speicher je App und Benutzer (z. B. 64 KiB), den die API verwaltet | Nützt auch allen anderen Apps (Fenstergröße, Einstellungen) | Vertragsänderung in `shared/`, betrifft Web, API und DB |

Empfehlung: **C**. Einen kleinen Einstellungsspeicher braucht fast jede App früher oder später,
und die Spiele wären der erste einfache Anwendungsfall dafür.

## 4. Reihenfolge

1. **Gemeinsames Modul** (Zufall mit Startwert, Menü, Zeit, Dialog) und **Minensucher**. Das ist
   das einfachste Spiel, und damit prüfen wir den ganzen Weg vom Packen über das Aktivieren bis
   zum Fenster.
2. **2048**, um Tastatur und Wischen zu prüfen.
3. **Solitär**, um Ziehen und Ablegen mit Maus und Finger zu prüfen. Die Karten zeichnen wir
   selbst als SVG, ohne fremde Grafiken.
4. **Sudoku** mit dem Generator (Backtracking mit Prüfung auf eindeutige Lösung, Stufe nach den
   nötigen Lösungstechniken).
5. **Schach**, zunächst für zwei Spieler.

Jedes Spiel ist ein eigener, kleiner Pull Request.

## 5. Tests

- **Logik ohne Browser:** Die `logic.js`-Module werden mit Playwright im Browser-Kontext getestet,
  wie die Verträge in `apps/web/tests/`, ohne dass eine API läuft.
- **Minensucher:** Der erste Klick trifft nie eine Mine; die Zahlen stimmen; das Aufdecken leerer
  Flächen hört an den Zahlen auf.
- **2048:** Feste Startwerte ergeben feste Folgen; ein Zug ohne Bewegung erzeugt keine neue Zahl;
  jede Zahl verschmilzt pro Zug höchstens einmal.
- **Solitär:** Nur erlaubte Züge werden angenommen; Rückgängig stellt genau den alten Zustand her;
  jedes Mischen enthält alle 52 Karten genau einmal.
- **Sudoku:** Jedes erzeugte Rätsel hat genau eine Lösung (Löser zählt bis 2); die Vorgaben
  widersprechen sich nie.
- **Schach:** Zuggenerator gegen die bekannten Perft-Werte der Standardstellungen (Startstellung,
  „Kiwipete“ und weitere) bis Tiefe 4. Die Werte kommen als Testdaten ins Spiel-Paket.
- **Oberfläche:** Je Spiel ein Playwright-Test, der eine Partie mit festem Startwert bis zum Sieg
  durchspielt, in hell und dunkel und in einer kleinen und einer großen Fenstergröße.
- **Sicherheit:** Ein Test prüft, dass im Code der Spiele kein `innerHTML`, `eval`,
  `new Function`, `fetch` und `WebSocket` vorkommt.

## 6. Offene Fragen an Thomas

1. **Wo liegt der Quellcode von Apps aus dem Store?** Die Spiele sind keine Kern-Apps wie der
   Rechner, gehören also nicht nach `apps/web/`. Vorschlag: ein eigenes Repo für die Apps des
   globalen Stores, oder ein Ordner nach dem Projektstruktur-Standard hier im Repo. Wer baut sie:
   die Web-Sitzung oder eine eigene Sitzung?
2. **Spielstände speichern:** Weg A, B oder C aus Abschnitt 3?
3. **Erlaubt das Manifest schon Apps ohne Dienst?** Wenn nicht, braucht `shared/` eine kleine
   Änderung.
4. **Sind die Spiele ab Werk installiert** oder nur im Store zu finden? Vorschlag: im Store, gratis,
   und ein Knopf „Spiele installieren“ in der Ersteinrichtung.
5. **Schach gegen den Computer:** Soll das in die erste Welle, oder genügt zunächst das Spiel zu
   zweit?
