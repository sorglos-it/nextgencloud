# P4 — Balancer-Vorlage in `shared/ulh.md`

Zweig `ulh-balancer-vorlage`, aus `controller` (`1338850`). Arbeitsbaum auf C:, nicht auf Z:.

## Die Erlaubnis selbst nachgemessen

    git diff --name-only vertraege-v1.1 main -- shared/   ->  shared/ulh.md   (genau eine Datei)
    git log --oneline vertraege-v1.1..main -- shared/ulh.md -> 2c76846

Stimmt. Nur diese eine Datei in `shared/` angefasst.

## Erster Befund: die drei Formen weichen NICHT ab — zwei davon sind byteidentisch

Erwartet waren „zwei Schreiber, zwei Formen". Gemessen:

| Form | Stand | Ergebnis |
|---|---|---|
| `apps/server-controller/templates/apache-apis.conf.tmpl` | `controller` 1338850 | rendert für zwei APIs byteweise die Zeile-für-Zeile-Form unten |
| `apps/web/templates/apache-apis.conf.example` | `web` **e5f1fcc** | `diff` gegen die Prüfdatei des Controllers: **0 Zeilen Unterschied** |
| Prosa in `shared/ulh.md:148–157` | `controller` | keine Vorlage, nur eine Aufzählung der Bestandteile |

    git show web:apps/web/templates/apache-apis.conf.example > w.conf
    diff w.conf apps/server-controller/classes/apache/testdata/apache-apis.conf   ->  identisch

**Maßgeblich ist diese Form** — und zwar nicht, weil ich gewählt hätte, sondern weil es nur eine
gibt: Aufgabe 22 hat sie wörtlich von `web` übernommen, der Vorlagenkopf nennt beide Schreiber
selbst (`Written by ngc-web … and after that by ngc-controller with role web`), und der vorhandene
Fall `TestRenderMatchesTheTemplate` hält sie byteweise. Was fehlte, war allein der Eintrag im
Vertrag.

**Darum ist Schritt 4 des Auftrags gegenstandslos:** `apache-apis.conf.tmpl` und
`classes/apache/testdata/apache-apis.conf` sind unverändert geblieben. Eine Angleichung hätte eine
Abweichung erfunden, die es nicht gibt.

## Was geändert wurde

1. `shared/ulh.md`: die Zeile „Die genaue Form legt der Plan des Webinterface fest und trägt sie
   hier als Vorlage ein" ist durch die **Vorlage selbst** ersetzt — ein ```apache-Block mit den
   21 Zeilen, wörtlich aus der Prüfdatei kopiert (Python, nicht abgeschrieben), plus der Regel für
   weitere APIs (genau eine weitere `BalancerMember`-Zeile) und dem Hinweis auf den Riegel.
   Nur ergänzt; keine andere Regel der Datei berührt.
2. `apps/server-controller/classes/apache/apache_test.go`: neuer Fall
   `TestBalancerTemplateMatchesTheContract` samt `balancerFromContract`.

## Der Riegel — ein Auseinanderlaufen fällt künftig GEMESSEN auf, nicht nur einem Leser

Der neue Fall schneidet den einzigen ```apache-Block aus `shared/ulh.md`, nimmt die vier
Einrückungs-Leerzeichen weg und stellt das Ergebnis byteweise gegen `Render` für genau die zwei
Adressen, die in der Vorlage stehen. Die Datei wird **von der Platte** gelesen, mit Absicht: das
Dokument, das ausgeliefert wird, ist das, das gemessen wird (eine Überlagerung würde hier nichts
zeigen — `messen.md`).

Mutiert auf einer **Kopie** des Baums, `-count=1`, nie im Arbeitsbaum:

| Mutation | Erwartung | Ergebnis |
|---|---|---|
| M1 `retry=5` → `retry=4` **in `shared/ulh.md`** | stirbt | `--- FAIL` |
| M2 `timeout=65` → `timeout=60` **in `apache-apis.conf.tmpl`** | stirbt | `--- FAIL` |
| M3 lokale Variable `out` → `plain`+Zeiger (Kontrolle) | **lebt** | `ok` |
| M4 Vorlage ganz aus `ulh.md` entfernt | stirbt **laut** | `--- FAIL: … holds 0 apache blocks` |

Beide Richtungen beißen, die Lebend-Kontrolle lebt, und M4 zeigt das Wichtigste: verschwindet die
Vorlage, wird der Fall nicht still grün, sondern nennt den Grund. Vorher-Kontrolle auf der
unmutierten Kopie: `ok`.

## Messungen

- Vor jeder Zahl, im selben Aufruf: `git diff --name-only --diff-filter=D HEAD | wc -l` = **0**.
- Paket `classes/apache` unter `-v`: **11 Fälle, 11 PASS, 0 SKIP, 0 FAIL**
  (`go test -buildvcs=false -count=1 -v ./apps/server-controller/classes/apache/`).
  Gezählt, nicht übernommen — und nicht „ok" gemeldet, weil `ok` die Fallzahl nicht nennt.
- `bash tools/check.sh` **ohne Pipe**: gedruckte Zeile **`check: ok`**, EXIT=0. Alle
  Controller-Pakete und alle `shared`-Pakete grün in diesem einen Lauf.
- Werkzeugkette go1.27.2. Kein Lauf dieser Aufgabe zeigte `unknown caller pc` o. ä.;
  es gab also nichts zu verwerfen.

## Was ungemessen bleibt

- **Es gibt auf dieser Maschine keinen Apache** (weder `apachectl` noch `apache2`, auch nicht in
  WSL). Dass Apache diese Datei wirklich annimmt — `apachectl configtest` — ist **nicht gemessen**,
  hier so wenig wie in jedem anderen Fall dieses Pakets. Der Riegel sichert zu, dass beide Seiten
  **dieselbe** Datei schreiben, nicht dass die Datei richtig ist.
- **Der Zweig `web` ist nicht zusammengeführt.** Gemessen gegen **e5f1fcc**. Wandert
  `apps/web/templates/apache-apis.conf.example` dort noch, wandert die Vorlage im Vertrag mit —
  dann muss `shared/ulh.md` (und damit der Controller) nachgezogen werden. Der neue Fall merkt das
  **nicht**: er kennt nur `ulh.md` und den Controller, nicht den Zweig `web`. Das bleibt beim
  Zusammenführen Handarbeit.
- `-race` nicht gemessen (kein C-Übersetzer auf dieser Maschine).
- `apps/web/` wurde nicht angefasst; der Web-Strang muss seine `.example` nicht ändern, solange sie
  identisch bleibt — eine Zusicherung, die auf **seiner** Seite niemand hält.
