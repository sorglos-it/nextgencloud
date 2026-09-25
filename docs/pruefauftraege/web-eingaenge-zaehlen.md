# Prüfauftrag: Alle Eingänge finden, die eine lügende Länge durchlassen

Du bist ein unabhängiger Prüfer. Du kennst die Vorgeschichte nicht — das ist
Absicht. Du änderst nichts dauerhaft, committest nichts und pushst nichts.

**Zweig:** `web`, Commit `cb9fbde`
**Teil:** die Browser-Seite, JavaScript, unter `apps/web/`
**Tests:** Playwright. chromium und webkit laufen, **Firefox nicht** (bekannt).

## Die Fehlerform

Ein Gegenstand, der seine eigene Länge angibt, gibt bei der **zweiten** Lesung
eine andere Zahl an als bei der ersten:

```js
let n = 0
const luegner = {
  get length() { return (++n === 1) ? 10 : 70000 },
  // … sonst wie ein Uint8Array
}
```

Wer die Länge zweimal liest — einmal zum **Prüfen**, einmal zum **Verarbeiten** —
prüft etwas anderes, als er verarbeitet. Eine Größengrenze, die so gebaut ist,
lässt beliebig große Eingaben durch.

Der teuerste bisher gefundene Fall: eine Funktion las die Länge in **einem
einzigen Lauf 303-mal**, und eine Lüge auf der ersten und der fünften Lesung
brachte einen Auftrag von **70 027 Byte durch eine Grenze von 16 384** — mit
gültiger Ed25519-Unterschrift und richtiger Form.

## Was schon bekannt ist

**Die Ursache ist `json.decode`:** das Budget misst gegen **eine** Lesung, der
Parser fragt danach weiter.

Bereits gemessene offene Eingänge:

| Eingang | Schranke | kam durch mit |
| --- | --- | --- |
| `sign.parseEnvelope(b, 1024)` | Budget ~1,4 kB | 20 000 B und 70 027 B |
| `session.parseTrustHint` | 64 B | 70 027 B |
| `session.parseLoginResult` | 64 KiB | 5 MB |
| `session.parseTrustChain` | 32 MiB | 40 MB |

`trust.parseList` hat dieselbe Form, ist aber **nicht durchgemessen**.
`wire.parseOpen`, `wire.parseClose` und `wire.parseControl` sind **dicht**
(0 Lesungen der Aufrufer-Länge) — dort ist es schon richtig gemacht.

## Deine Aufgabe

**Die vollständige Liste.** Geh systematisch jede Stelle durch, die Bytes vom
Aufrufer entgegennimmt, und miss für jede:

1. Wie oft wird die Länge des übergebenen Gegenstands gelesen? (Zähl es mit einem
   Getter, nicht durch Lesen des Quelltexts.)
2. Kommt mit einer lügenden Länge etwas durch, das durchkommen dürfte? Nenn die
   Grenze und die Zahl, die durchkam.
3. Ist die Stelle heute von echtem Code erreichbar, oder nur aus Tests?

`trust.parseList` gehört ausdrücklich dazu.

## So arbeitest du

- **Messen, nicht lesen.** Bau den lügenden Gegenstand und ruf die Funktion auf.
- Prüf jede Messung darauf, ob sie überhaupt gelaufen ist.
- Wo etwas durchkommt: nenn die **gemessene** Zahl, nicht eine geschätzte.
- Nimm alle Änderungen zurück und weis mit `git diff` nach, dass der Baum sauber
  ist.

## Was du nicht tust

- Nichts committen, nichts pushen, keinen Pull Request öffnen.
- **Nichts reparieren.** Eine andere Sitzung baut gerade die Behebung; du lieferst
  die Liste, gegen die ihr Wächter zählen wird. Reparierst du mit, kollidiert es.
- `shared/` und `vendor/` nicht ändern.

## Antworte auf Deutsch, kurz

- **Die Tabelle:** je Eingang Name, Schranke, Zahl der Lesungen, ob etwas
  durchkommt und womit, und ob die Stelle heute erreichbar ist.
- **Die Gesamtzahl der Eingänge**, die Aufrufer-Bytes nehmen. Diese Zahl ist das
  Wichtigste an deiner Antwort — an ihr wird sich ein Wächter messen lassen
  müssen.
- Was du **nicht** prüfen konntest, und warum.

Behaupte nichts, was du nicht ausgeführt hast.
