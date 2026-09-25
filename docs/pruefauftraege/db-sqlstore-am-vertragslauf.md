# Prüfauftrag: Was passiert, wenn `sqlstore` den Vertragslauf wirklich fährt?

**Wozu und mit welcher Erlaubnis.** Dies ist eine Messung an eigenem Code, im
privaten Repository seines Eigentümers, mit dessen Auftrag. Zwei Sätze im
Vertrag behaupten eine Prüfung, die es nicht gibt. Dieser Auftrag stellt fest,
was die Prüfung **ergeben würde**. Du änderst nichts am Vertrag und schiebst
nur deine Ergebnisdatei hoch.

**Zweig:** `db`, Stand `ca27ee7`
**Toolchain:** Go 1.27, `CGO_ENABLED=0`, `GOFLAGS=-mod=vendor`, Abhängigkeiten
in `vendor/`. `bash tools/check.sh` muss `check: ok` sagen.

## Der Befund, von dem du ausgehst

`shared/dbapi/dbtest/dbtest.go` sagt sinngemäß: **jede** Umsetzung unter
`apps/server-db/` muss `Run` bestehen, gegen MariaDB und gegen SQLite. Und
`shared/dbapi/dbapi.go` sagt etwas Ähnliches.

Gemessen ist dagegen: `dbtest.Run` hat **genau eine** Aufrufstelle im ganzen
Baum — `shared/dbapi/memstore/memstore_test.go`, gegen `memstore.New()`. Nur
diese eine Datei importiert `dbtest` überhaupt. Keine Build-Tags, keine
Umgebungsschalter. **`sqlstore` ist an den Vertragslauf nicht angeschlossen.**

Prüf das zuerst selbst nach. Wenn es nicht stimmt, ist das dein Befund und du
bist fertig.

## Was du messen sollst

Schließ `sqlstore` in einer **Wegwerfkopie** an den Vertragslauf an und sag,
was dabei herauskommt.

1. **Kopie, nicht Arbeitsbaum.** `git archive ca27ee7 | tar -x` in ein eigenes
   Verzeichnis, oder ein eigener Klon. Nicht im geprüften Baum arbeiten.
2. **Eine Testdatei** neben `sqlstore`, die `dbtest.Run` gegen einen frisch
   angelegten `sqlstore` auf SQLite fährt — so wie `memstore_test.go` es für
   `memstore` tut. Mehr nicht: du baust keinen neuen Vertrag, du schließt den
   vorhandenen an.
3. **Fahr ihn.** Und dann beantworte genau das:
   - Wie viele Fälle fährt `Run`? Wie viele sind grün, wie viele rot?
   - **Jeder rote Fall einzeln:** welche Zusicherung, welche Erwartung, welche
     tatsächliche Antwort. Wörtlich, nicht zusammengefasst.
   - Ist der rote Fall ein **Defekt in `sqlstore`** oder eine **Erwartung, die
     nur für `memstore` gilt**? Das ist die eigentliche Frage. Begründe jede
     Einordnung mit dem, was du gesehen hast.
4. **MariaDB**, falls du eine zum Laufen bringst: dasselbe. Wenn nicht, sag
   klar, dass es nicht geprüft ist — das ist brauchbar, eine Vermutung nicht.

## Dazu, wenn du dabei bist

`apps/server-controller/CLAUDE.md` enthält eine Aussage, die auf derselben
Annahme ruhen könnte (dass der Vertragslauf jede Umsetzung fährt). Sieh nach,
ob sie trägt, und sag es.

## Die zwei Fallen, in die hier jeder getappt ist

1. **Ein `[build failed]` sieht aus wie ein FAIL und ist keines.** Wenn ein Fall
   rot ist, weis erst nach, dass er **übersetzt und gelaufen** ist — etwa, indem
   du die Gesamtzahl der gelaufenen Fälle nennst. Erst dann ist rot ein Befund.
2. **Eine Zahl, die du weitergibst, ist keine Zahl, die du gemessen hast.** Auch
   die Zahlen in diesem Auftrag nicht. Rechne nach, was du behauptest.

Nenn Tests **beim Namen**, nie bei der Zeilennummer.

## Wo dein Ergebnis hingehört

```bash
git fetch origin pruefergebnisse || true
git checkout -B pruefergebnisse origin/pruefergebnisse 2>/dev/null || git checkout -B pruefergebnisse origin/main
mkdir -p docs/pruefergebnisse
# Antwort nach docs/pruefergebnisse/db-sqlstore-am-vertragslauf.md schreiben
git add docs/pruefergebnisse/db-sqlstore-am-vertragslauf.md
git commit -m "Ergebnis: db-sqlstore-am-vertragslauf" -- docs/pruefergebnisse/db-sqlstore-am-vertragslauf.md
git push origin pruefergebnisse
```

**Das ist die einzige Ausnahme vom „nichts committen, nichts pushen".** Deine
Testdatei aus Schritt 2 gehört **nicht** dazu — häng sie als Text in den Bericht,
damit sie nachvollziehbar ist, aber committe sie nicht.

Schreib dazu, was du **nicht** geschafft hast und warum.
