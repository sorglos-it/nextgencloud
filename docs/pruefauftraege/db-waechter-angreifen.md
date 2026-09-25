# Prüfauftrag: Den fünften Weg am Datenbank-Wächter finden

Du bist ein unabhängiger Angreifer. Du kennst die Vorgeschichte nicht — das ist
Absicht. Du änderst nichts dauerhaft, committest nichts und pushst nichts.

**Zweig:** `db`, Commit `aa718c6`
**Paket:** `apps/server-db/classes/sqlstore`
**Toolchain:** Go 1.27, `CGO_ENABLED=0`, `GOFLAGS=-mod=vendor`, alle
Abhängigkeiten liegen in `vendor/`. Es gibt `tools/check.sh`.

## Die Aufgabe in einem Satz

Finde einen **weiteren** Weg, die Namensprüfung in `UserPut` wirkungslos zu
machen, während der Wächter `TestTheNameCheckSitsInTheTransaction` in
`sqlcheck_test.go` **und das ganze Paket grün bleiben**.

## Was der Wächter halten soll

Die Abfrage, die prüft, ob der Name schon einer anderen ID gehört, muss
**innerhalb derselben Datenbank-Transaktion** laufen wie der Schreibsatz — damit
zwischen Prüfung und Schreiben kein zweiter Schreiber passt.

## Vier Wege sind schon bekannt. Such NICHT nach diesen

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

- Bau jede Idee als **echte Änderung am Quelltext**. Übersetze sie
  (`go vet ./...`) und **lass sie laufen** (`go test ./apps/server-db/...`).
- **Eine Änderung, die nicht übersetzt, ist kein Fund.** Und prüf immer, ob der
  Test überhaupt ausgeführt wurde — ein `[build failed]` sieht aus wie FAIL und
  ist keins. An dieser Falle ist in diesem Projekt schon mehrfach jemand
  hängengeblieben.
- Ein Fund zählt nur, wenn **alles** grün bleibt: das Paket, der Wächter und das
  ganze Modul (`go test ./...`).
- **Weis nach, dass die Prüfung wirklich tot ist** — zum Beispiel mit einem
  `panic` an der Stelle, die dann nie erreicht wird, oder mit einem Zähler. Nicht
  behaupten: zeigen.
- Nimm jede Änderung danach zurück und weis mit `git diff` nach, dass der Baum
  wieder sauber ist.

## Was du nicht tust

- Nichts committen, nichts pushen, keinen Pull Request öffnen.
- `shared/` nicht ändern — eingefroren.
- Keine neuen Abhängigkeiten.

## Antworte auf Deutsch, kurz

- **Gefunden: ja/nein.** Bei ja: der Quelltext der Änderung, die Ausgabe, die
  zeigt, dass alles grün bleibt, und der Nachweis, dass die Prüfung tot ist.
- Bei nein: **was du versucht hast, als Liste**, und woran es jeweils gescheitert
  ist. Das ist genauso wertvoll — es sagt, was der Wächter wirklich hält.
- **Deine Einschätzung in zwei Sätzen:** Kann ein Prüfer über den Syntaxbaum
  diese Eigenschaft überhaupt halten, oder braucht es einen Test am Verhalten?

Behaupte nichts, was du nicht ausgeführt hast.
