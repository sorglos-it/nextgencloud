# Prüfauftrag: Vier Stellen, die der Engine fremden Speicher reichen

**Wozu und mit welcher Erlaubnis.** Dies ist eine Sicherheitsmessung an eigenem
Code, im privaten Repository seines Eigentümers, mit dessen Auftrag. Es geht
darum, eine bereits geschlossene Fehlerklasse an **vier weiteren Stellen** zu
messen, an denen sie vermutet, aber nie nachgewiesen wurde. Du änderst nichts am
geprüften Code. Du schreibst nur einen Bericht.

**Zweig:** `web`, Stand `2e242e9`
**Wo:** `apps/web/public/assets/js/contracts/`
**Tests:** `apps/web/tests/`, Playwright. `npm ci` in `apps/web/tests/`, dann
`npx playwright test`. **Nimm einen eigenen Port** (`NGC_WEB_TEST_PORT`, etwas
ab 9500) und prüf vor- und nachher, dass er frei ist. Port 8101 gehört einer
fremden Sitzung und ist tabu.

## Die Klasse, um die es geht

Ein Vertrag bekommt vom Aufrufer einen Byte-Gegenstand. Er liest ihn **zweimal**
— einmal, um etwas zu prüfen, und einmal, um damit zu arbeiten. Dazwischen liegt
ein `await`. Ein Aufrufer aus **demselben Ursprung** kann den Gegenstand in
diesem Moment austauschen oder überschreiben: geprüft wird A, benutzt wird B.

Im Zweig `web` ist diese Klasse an fünf Stellen geschlossen worden
(`sign.verify`, `proof.verify`, `task.verifyApproval`, `session.verifyAPI`,
`trust.advance`/`open`). Die Reparatur war immer dieselbe: **eine Kopie ziehen
und nur noch mit der arbeiten.**

Vier Stellen sind übrig. Für sie ist die Gestalt **gelesen, nicht gemessen**:

| Stelle | Was dort passiert |
| --- | --- |
| `noise.js` — `Transport.seal` und `Transport.open` | bauen eine Sicht auf fremdem Speicher und reichen sie WebCrypto |
| `session.js` — `certHash` | dieselbe Bauweise |
| `trust.js` — `tlsPin` | dieselbe Bauweise |
| `proof.js` — `verify` | kopiert `sig`, `authData` und `clientData` **nicht** |

## Was du messen sollst

Für **jede** der vier Stellen, einzeln:

1. **Zähl die Lesungen.** Wie oft fragt die Funktion den übergebenen Gegenstand
   nach `length`, `byteLength`, `buffer`, `byteOffset` oder einem Index? Bau dir
   dafür ein Objekt, das jede dieser Fragen zählt, und lass die echte Funktion
   darauf laufen. Eine Lesung ist unbedenklich. Zwei sind der Anfang.
2. **Bau den Tausch.** Wenn es zwei Lesungen gibt: liegt ein `await` dazwischen?
   Dann tausch dort. Das Werkzeug, das sich hier bewährt hat: **ein gemeinsamer
   Zähler über alle `crypto.subtle`-Aufrufe, getauscht nach dem k-ten
   aufgelösten Versprechen** — damit ist „Moment k" aufzählbar statt geraten.
   Fahr **alle** k durch und sag, wie viele Momente es überhaupt gibt.
3. **Sag, was dabei herauskommt.** Ein Unterschied ist nur dann ein Fund, wenn
   er etwas Falsches bewirkt: eine Prüfung sagt `true` für etwas, das sie
   ablehnen müsste; ein Hash deckt andere Bytes als die benutzten; ein
   Chiffrat entsteht über andere Bytes als die geprüften. Ein Wurf oder eine
   Ablehnung ist **kein** Fund.
4. **Miss in node und in chromium.** Beide, oder sag welches nicht ging.

Für `proof.verify` zusätzlich: der Kommentar bei `proof.js` sagt sinngemäß
„alles darunter liest die Kopien". Stimmt das für `sig`, `authData` und
`clientData`? Miss es, statt es zu lesen.

## Was ausdrücklich KEIN Fund ist

- **Die Reichweite.** Alle diese Gegenstände bekommen heute schlichte
  `Uint8Array` aus dem Dekoder. Das lügende Objekt muss von JavaScript
  **desselben Ursprungs** hereingegeben werden, nicht aus dem Netz. Das ist ein
  echter Vertragsdefekt, aber keine heute aus der Ferne erreichbare Umgehung.
  Schreib das bei jedem Fund dazu — es ist mehrfach vergessen worden.
- `Transport` ist eine **Klasse**. Ein Wächter, der `f(...)` ohne `new` ruft,
  wirft dort sofort. Das ist bekannt und kein Fund.

## Die Falle, in die hier jeder getappt ist

Eine Zahl, die du **weitergibst**, ist keine Zahl, die du **gemessen** hast. In
diesem Projekt sind so schon Zeilennummern, Testnamen und Größen falsch in den
Quelltext gewandert. Wenn du in diesem Auftrag eine Zahl liest, die du selbst
nicht nachgerechnet hast, schreib dazu, dass sie von hier kommt.

Nenn Tests **beim Namen**, nie bei der Zeilennummer.

## Wo dein Ergebnis hingehört

Schreib deine Antwort **zusätzlich** in eine Datei und schieb sie hoch — sonst
erreicht sie den Koordinator nicht, denn er kann deine Sitzung nicht sehen.

```bash
git fetch origin pruefergebnisse || true
git checkout -B pruefergebnisse origin/pruefergebnisse 2>/dev/null || git checkout -B pruefergebnisse origin/main
mkdir -p docs/pruefergebnisse
# Antwort nach docs/pruefergebnisse/web-fremder-speicher.md schreiben
git add docs/pruefergebnisse/web-fremder-speicher.md
git commit -m "Ergebnis: web-fremder-speicher" -- docs/pruefergebnisse/web-fremder-speicher.md
git push origin pruefergebnisse
```

**Das ist die einzige Ausnahme vom „nichts committen, nichts pushen".** Sie gilt
nur für diese eine Ergebnisdatei, nur auf dem Zweig `pruefergebnisse`.

Schreib dazu, was du **nicht** geschafft hast und warum. Ein ehrliches „nicht
geprüft" ist brauchbar; eine Vermutung, die wie eine Messung aussieht, ist es
nicht.
