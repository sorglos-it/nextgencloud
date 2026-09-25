# Prüfauftrag: Mutationstest — hat dieser Test-Wächter ein falsches Grün?

## Worum es geht, zuerst

Dies ist ein **Mutationstest an einem Test**. Er findet ausschließlich in einer
Wegwerf-Arbeitskopie statt, nichts wird committet, nichts gepusht, nichts
ausgeliefert. Das Ziel ist, den Wächter **stärker** zu machen, nicht die Software
schwächer.

Das Repository gehört dem Auftraggeber (privat, `sorglos-it/nextgencloud`), die
Software läuft nirgends produktiv, und dieser Auftrag stammt vom Autor des
Wächters selbst. Er hat den Wächter dreimal umgangen bekommen und will wissen,
ob es einen vierten Weg gibt, bevor er sich auf ihn verlässt.

**Mutationstest heißt:** man ändert den Quelltext absichtlich so, dass er falsch
wird, und sieht nach, ob die Tests das merken. Merken sie es nicht, ist der Test
das Problem — nicht die Änderung. Die Änderung wird danach weggeworfen. Das ist
gängige Praxis und der einzige Weg, ein falsches Grün zu finden.

## Der Gegenstand

**Zweig:** `db`, Commit `aa718c6`
**Paket:** `apps/server-db/classes/sqlstore`
**Der Wächter:** `TestTheNameCheckSitsInTheTransaction` in `sqlcheck_test.go`
**Toolchain:** Go 1.27, `CGO_ENABLED=0`, `GOFLAGS=-mod=vendor`, Abhängigkeiten
liegen in `vendor/`. Es gibt `tools/check.sh`.

Der Wächter soll halten, dass die Abfrage, die prüft, ob ein Benutzername schon
einer anderen Kennung gehört, **innerhalb derselben Datenbank-Transaktion** läuft
wie der Schreibsatz — sonst passt zwischen Prüfung und Schreiben ein zweiter
Schreiber.

## Die Frage

**Gibt es eine Änderung an `UserPut`, die die Namensprüfung wirkungslos macht,
ohne dass der Wächter oder das Paket rot wird?**

Jede solche Änderung ist ein **Fund über den Wächter**: sie zeigt, welches
falsche Grün er zulässt.

## Vier Fälle sind schon bekannt — such nicht nach diesen

1. Die Abfrage auf dem Verbindungspool statt auf der Transaktion laufen lassen
   (`s.db` statt `tx`). Sie nimmt dann eine zweite Verbindung und liest den
   zuletzt festgeschriebenen Stand.
2. Die Zeilen in eine Closure stecken und per `defer` aufrufen — sie **stehen**
   davor und **laufen** danach.
3. Die Abfrage hinter eine Bedingung setzen, die weiter oben schon ausgeschlossen
   ist (etwa `if len(u.ID) == 0 { … }`, wo `u.ID == ""` oben bereits abgelehnt
   wird). Der Block läuft nie.
4. Die Antwort der Abfrage wegwerfen, sodass die Ablehnung nie greifen kann.

**Such den fünften.** Richtungen, an denen du dich nicht festbeißen musst:
Nebenläufigkeit, Fehlerpfade, `context`, Wiederholungsschleifen, Verschattung von
Variablen, Typumwandlungen, Schnittstellen, Testhilfen, Build-Tags, generierter
Code, `init()`.

## So arbeitest du

- Arbeite in einer **eigenen Arbeitskopie**, die du am Ende wieder entfernst.
- Bau jede Idee als echte Änderung, übersetze sie (`go vet ./...`) und **lass sie
  laufen** (`go test ./apps/server-db/...`).
- **Eine Änderung, die nicht übersetzt, ist kein Fund.** Prüf immer, ob der Test
  überhaupt ausgeführt wurde — ein `[build failed]` sieht aus wie FAIL und ist
  keins. An dieser Falle ist in diesem Projekt schon mehrfach jemand
  hängengeblieben.
- Ein Fund zählt nur, wenn **alles** grün bleibt: Paket, Wächter und ganzes Modul
  (`go test ./...`).
- **Weis nach, dass die Prüfung wirklich tot ist** — etwa mit einem `panic` an
  der Stelle, die dann nie erreicht wird, oder mit einem Zähler. Nicht behaupten:
  zeigen.
- Nimm jede Änderung zurück und weis mit `git diff` nach, dass der Baum sauber
  ist.

## Was du nicht tust

- Nichts committen, nichts pushen, keinen Pull Request öffnen, keinen Zweig
  anlegen, der bestehen bleibt.
- `shared/` nicht ändern — eingefroren.
- Keine neuen Abhängigkeiten.

## Antworte auf Deutsch, kurz

- **Falsches Grün gefunden: ja/nein.** Bei ja: der Quelltext der Änderung, die
  Ausgabe, die zeigt, dass alles grün bleibt, und der Nachweis, dass die Prüfung
  tot ist.
- Bei nein: **was du versucht hast, als Liste**, und woran es jeweils gescheitert
  ist. Das ist genauso wertvoll — es sagt, was der Wächter wirklich hält.
- **Deine Einschätzung in zwei Sätzen:** Kann ein Prüfer über den Syntaxbaum
  diese Eigenschaft überhaupt halten, oder braucht es einen Test am Verhalten?
  Wenn Letzteres: wie sähe der aus?

Behaupte nichts, was du nicht ausgeführt hast.

## Wo dein Ergebnis hingehört

Schreib deine Antwort **zusätzlich** in eine Datei und schieb sie hoch — sonst
erreicht sie den Koordinator nicht, denn er kann deine Sitzung nicht sehen.

```bash
git fetch origin pruefergebnisse || true
git checkout -B pruefergebnisse origin/pruefergebnisse 2>/dev/null || git checkout -B pruefergebnisse origin/main
mkdir -p docs/pruefergebnisse
# Antwort nach docs/pruefergebnisse/<name-dieses-auftrags>.md schreiben
git add docs/pruefergebnisse/<name-dieses-auftrags>.md
git commit -m "Ergebnis: <name-dieses-auftrags>" -- docs/pruefergebnisse/<name-dieses-auftrags>.md
git push origin pruefergebnisse
```

**Das ist die einzige Ausnahme vom „nichts committen, nichts pushen".** Sie gilt
nur für diese eine Ergebnisdatei, nur auf dem Zweig `pruefergebnisse`, und für
keine Änderung am geprüften Code. Alles andere bleibt wie oben.

Schreib dazu, was du **nicht** geschafft hast und warum. Ein ehrliches „nicht
geprüft" ist brauchbar; eine Vermutung, die wie eine Messung aussieht, ist es
nicht.
