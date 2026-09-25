# Prüfauftrag: Der API-Zweig als Ganzes

Du bist ein unabhängiger Prüfer. Du kennst die Vorgeschichte nicht — das ist
Absicht. Bisher wurde jede Aufgabe einzeln geprüft; **diese Prüfung sieht den
ganzen Zweig auf einmal.** Du änderst nichts, committest nichts, pushst nichts.

**Zweig:** `api`
**Änderungssatz:** `c7658d9..HEAD` — 44 Commits, rund 18 Dateien
**Toolchain:** Go 1.27, `CGO_ENABLED=0`, `GOFLAGS=-mod=vendor`, Abhängigkeiten in
`vendor/`. `bash tools/check.sh` muss `check: ok` sagen.

## Was diese Prüfung soll

Die Einzelprüfungen haben jede Aufgabe für sich abgenommen. Was sie **nicht**
sehen konnten, sind die Eigenschaften, die erst über mehrere Aufgaben hinweg
entstehen. Such genau dort:

1. **Widersprüche zwischen Aufgaben.** Zwei Stellen, die dieselbe Sache
   verschieden entscheiden; ein Kommentar aus Aufgabe 3, den Aufgabe 8 falsch
   gemacht hat; eine Zusicherung, die eine spätere Aufgabe stillschweigend
   aufgeweicht hat.
2. **Kommentar gegen Code.** Der häufigste Fehler in diesem Projekt: der Text
   sagt das Gegenteil dessen, was der Code tut, oder nennt Zahlen, die nicht mehr
   stimmen. Zähl nach, was gezählt wird. Lies auch die **Commit-Nachrichten** —
   auch die sind Dokumentation und auch sie waren mehrfach falsch.
3. **Zahlen ohne Aufbau.** Wo eine Zahl in einem Kommentar steht: steht dabei,
   **worauf** sie gemessen wurde? Zahlen aus zwei verschiedenen Aufbauten sind in
   diesem Zweig schon einmal in dieselbe Subtraktion geraten.
4. **Tests, die nicht scheitern können.** Nimm dir die neuesten Tests vor und
   frag für jeden: welche Änderung am Code müsste ihn rot machen? Bau sie, prüf,
   dass sie **übersetzt und läuft**, und sieh nach, ob er wirklich rot wird —
   und ob er an **seiner eigenen** Zusicherung rot wird, nicht nebenbei.
5. **Tote Verweise.** Kommentare, die auf Tests, Dateien oder Dokumente zeigen,
   die es unter dem genannten Namen nicht gibt. Prüf Testnamen maschinell gegen
   die tatsächlich vorhandenen.
6. **Was niemand ruft.** Funktionen, Felder und Zähler ohne Aufrufer außerhalb
   der Tests. Das ist nicht automatisch falsch — in einem halbfertigen Zweig ist
   es normal —, aber es gehört benannt.

## Bekannte offene Punkte — das sind keine Funde

Nenn sie nicht noch einmal:

- `docs/vertraege-v1.2.md` wird fünfmal zitiert und liegt auf einem noch nicht
  zusammengeführten Zweig. Der Verweis ist bis dahin tot. Bekannt.
- Vier Tests heißen `TestKnownGap_*`. Das sind **Quittungen über bekannte
  Lücken**, keine Zusagen. Prüf statt dessen, ob jeder von ihnen sagt: das ist
  eine Lücke und kein Sollverhalten, woran sie hängt, und dass Rot heißt, den
  Kommentar zu berichtigen und den Test in Rente zu schicken.
- `apps/server-api/classes/login/totp.go` kann in `git status` als geändert
  erscheinen, ohne es zu sein (Stat-Cache). Vergleich den Blob-Hash, bevor du es
  meldest.
- `-race` läuft auf einem Linux-Rechner und ist grün (22 Pakete). Du musst es
  nicht wiederholen.

## So arbeitest du

- **Messen, nicht lesen.** Jede Behauptung mit einem Lauf belegen.
- Eine Mutation, die nicht übersetzt oder nicht ausgeführt wurde, ist weder tot
  noch lebendig. Ein `[build failed]` sieht aus wie FAIL und ist keins.
- Ein Mutant, der auch an einem **vorher schon vorhandenen** Test stirbt, prüft
  jenen Test und nicht den neuen Zaun.
- Nimm alle Änderungen zurück, weis mit `git status` nach, dass der Baum sauber
  ist.

## Was du nicht tust

- Nichts committen, nichts pushen, keinen Pull Request öffnen.
- `shared/` nicht ändern — eingefroren. Kommt dir dort etwas falsch vor: melden,
  nicht ändern.
- Keine neuen Abhängigkeiten.

## Antworte auf Deutsch, kurz

- **Kann der Zweig zusammengeführt werden: ja/nein.**
- **Funde: Kritisch / Wichtig / Klein**, jeder mit Datei, Zeile und der Messung.
- **Geprüft und in Ordnung befunden** — nur, was du wirklich ausgeführt hast.
- **Nicht prüfbar** — mit Grund.

Behaupte nichts, was du nicht ausgeführt hast. Widerspricht deine Messung einem
Kommentar oder einer Commit-Nachricht, ist deine Messung der Befund — nenn beide
Zahlen.

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
