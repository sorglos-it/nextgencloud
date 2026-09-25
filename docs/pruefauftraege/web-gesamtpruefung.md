# Prüfauftrag: Der Web-Zweig als Ganzes

Du bist ein unabhängiger Prüfer. Du kennst die Vorgeschichte nicht — das ist
Absicht. Bisher wurde jede Aufgabe einzeln geprüft; **diese Prüfung sieht den
ganzen Zweig auf einmal.** Du änderst nichts, committest nichts, pushst nichts —
außer deiner Ergebnisdatei ganz unten.

**Zweig:** `web`, Stand `2e242e9`
**Änderungssatz:** `main..2e242e9` — 97 Commits
**Tests:** `apps/web/tests/`, Playwright. `npm ci` dort, dann
`npx playwright test`. **Eigener Port** (`NGC_WEB_TEST_PORT`, etwas ab 9600),
vorher und nachher als frei prüfen. Port 8101 gehört einer fremden Sitzung.
`bash tools/check.sh` muss `check: ok` sagen (der prüft nur Go, nicht das Web).

## Was diese Prüfung soll

Die Einzelprüfungen haben jede Aufgabe für sich abgenommen. Was sie **nicht**
sehen konnten, sind die Eigenschaften, die erst über mehrere Aufgaben hinweg
entstehen. Such genau dort:

1. **Widersprüche zwischen Aufgaben.** Zwei Stellen, die dieselbe Sache
   verschieden entscheiden; eine Zusicherung, die eine spätere Aufgabe
   stillschweigend aufgeweicht hat.
2. **Kommentar gegen Code.** Der häufigste Fehler in diesem Projekt: der Text
   sagt das Gegenteil dessen, was der Code tut, oder nennt Zahlen, die nicht
   mehr stimmen. Zähl nach, was gezählt wird. Lies auch die
   **Commit-Nachrichten** — auch die sind Dokumentation und auch sie waren
   mehrfach falsch. Der letzte Commit dieses Zweiges ändert **nur** Kommentare;
   nimm ihn besonders genau.
3. **Zahlen ohne Aufbau.** Wo eine Zahl in einem Kommentar steht: steht dabei,
   **worauf** sie gemessen wurde? Zahlen aus zwei verschiedenen Aufbauten sind
   in diesem Projekt schon in dieselbe Subtraktion geraten.
4. **Tests, die nicht scheitern können.** Nimm dir die neuesten Tests vor und
   frag für jeden: welche Änderung am Code müsste ihn rot machen? Bau sie, prüf,
   dass sie **übersetzt und läuft**, und sieh nach, ob er wirklich rot wird —
   und ob er an **seiner eigenen** Zusicherung rot wird, nicht nebenbei.
5. **Tote Verweise.** Kommentare, die auf Tests, Dateien oder Dokumente zeigen,
   die es unter dem genannten Namen nicht gibt. Prüf Testnamen **maschinell**
   gegen die tatsächlich vorhandenen. Hier ist schon eine falsche Zeilennummer
   durchgerutscht, die auf einen auskommentierten Block zeigte und deshalb wie
   ein Treffer aussah.
6. **Was niemand ruft.** Funktionen und Felder ohne Aufrufer außerhalb der
   Tests. In einem halbfertigen Zweig ist das normal — aber es gehört benannt.
7. **firefox.** Die ganze Suite lief bisher nur in chromium und webkit. Fahr sie
   in firefox und sag, was anders ist. Wenn firefox nicht startet, sag das.

## Bekannte offene Punkte — das sind keine Funde

- `noise.Transport.seal`/`open`, `session.certHash`, `trust.tlsPin` reichen der
  Engine eine Sicht auf fremden Speicher; `proof.verify` kopiert `sig`,
  `authData` und `clientData` nicht. Alle vier sind angemeldet und haben einen
  eigenen Auftrag.
- `session.verifyAPI`s `client`-Argument hat dieselbe Gestalt. Es gibt heute
  keinen Produktions-Aufrufer dieser Funktion; der Plan baut ihn später.
- `trust.open` prüft oberhalb Fassung 1 **keine Signatur**. Das ist gewollt und
  steht auf der Go-Seite so im Vertrag: der Ankerhash bindet die genauen Bytes.
- Von 27 Aufgaben des Web-Plans sind 1–10 und 10a gebaut. Alles ab Aufgabe 11
  fehlt noch und ist kein Fund.

## Die Falle, in die hier jeder getappt ist

Eine Zahl, die du **weitergibst**, ist keine Zahl, die du **gemessen** hast —
auch keine Zahl aus diesem Auftrag. Wenn deine Messung von einem Kommentar oder
einer Commit-Nachricht abweicht, ist deine Messung der Befund; nenn beide
Zahlen. Und nenn Tests **beim Namen**, nie bei der Zeilennummer.

## Wo dein Ergebnis hingehört

```bash
git fetch origin pruefergebnisse || true
git checkout -B pruefergebnisse origin/pruefergebnisse 2>/dev/null || git checkout -B pruefergebnisse origin/main
mkdir -p docs/pruefergebnisse
# Antwort nach docs/pruefergebnisse/web-gesamtpruefung.md schreiben
git add docs/pruefergebnisse/web-gesamtpruefung.md
git commit -m "Ergebnis: web-gesamtpruefung" -- docs/pruefergebnisse/web-gesamtpruefung.md
git push origin pruefergebnisse
```

**Das ist die einzige Ausnahme vom „nichts committen, nichts pushen".**

Schreib dazu, was du **nicht** geschafft hast und warum. Ein ehrliches „nicht
geprüft" ist brauchbar; eine Vermutung, die wie eine Messung aussieht, ist es
nicht.
