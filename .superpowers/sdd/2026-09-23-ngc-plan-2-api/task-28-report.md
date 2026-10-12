# Aufgabe 28: `ngc-store catalog`

Stand: 12.10.2026. Zweig `api-t28` (aus `api-t26`, `1bc5454`). Arbeitsbaum
`C:/Users/User/AppData/Local/Temp/claude/wt-a28` — außerhalb von `Z:`, weil Worktrees dort an der
Pfadlänge scheitern. Nichts geschoben. `shared/` nicht angefasst.

## Was gebaut wurde

| Datei | |
|---|---|
| `apps/cli/classes/catalog.go` | neu |
| `apps/cli/classes/catalog_test.go` | neu |
| `apps/cli/classes/run.go` | geändert (Versand des Unterbefehls `catalog`) |
| `apps/cli/classes/run_test.go` | geändert (`catalog` ist nicht mehr „noch nicht gebaut") |

`crypt.go`, `license.go` (Aufgabe 27) und `release.go` (Aufgabe 29) sind **nicht** angelegt.

`Catalog(dir, keyFile, CatalogOpts) (sign.Envelope, error)` liest jedes `*.ngc` des Verzeichnisses,
nimmt Manifest und `store.PackageUI`-Hash aus dem **unverschlüsselten** Paket, Größe und SHA-256
aus der Datei, die eine Installation herunterlädt, und unterschreibt mit `sign.PurposeCatalog`.
`Catalog` schreibt **keine** Datei; der Unterbefehl in `run.go` schreibt den Umschlag danach
atomar (Nebendatei, dann umbenennen) nach `<Verzeichnis>/catalog.json`.

## Testergebnis

Ein Satz, ein Lauf: `go test -count=1 ./apps/cli/...` → **ok, Ausstiegscode 0**; `bash tools/check.sh`
(ohne Pipe) → gedruckte Zeile **`check: ok`**, `EXIT=0`. 29 Fälle im Paket (`-list '.*'` gezählt,
selbst gezählt, nicht übernommen). Vor jeder Zahl im selben Aufruf:
`git -c safe.directory=<Baum> diff --name-only --diff-filter=D HEAD | wc -l` = **0**.

Ohne `-c safe.directory=<Baum>` antwortet git in diesem Baum mit Code 128 („dubious ownership");
die go-Werkzeuge an derselben Frage mit „error obtaining VCS status", darum lief alles mit
`GOFLAGS=-buildvcs=false`. `tools/check.sh` braucht das nicht, es regelt es selbst.

## Geht mein Katalog durch `store.VerifyCatalog`? Ja — und nur darüber wird gemessen

Jeder Fall liest sein Ergebnis über `store.VerifyCatalog` zurück (Hilfsfunktion `readBack`), nie
über einen eigenen Leser. Gemessen in `TestCatalogVerifies`, mit genau den Prüfsteinen, an denen
der Server der Aufgabe 19 abweist:

| Prüfung | Ergebnis |
|---|---|
| richtiger Schlüssel, richtige Store-Kennung | angenommen |
| `lastSeq` = die eigene Folge (6) | angenommen |
| `lastSeq` = 7 (eine höhere) | `store.ErrRollback` |
| fremder öffentlicher Schlüssel | `sign.ErrWrongKey` |
| verbogene Unterschrift (ein Bit) | `sign.ErrBadSignature` |
| dieselbe Nutzlast, mit `sign.PurposeLicense` unterschrieben | `sign.ErrBadSignature` |
| derselbe Katalog als Katalog eines **anderen** Stores gelesen | `store.ErrCatalog` |
| JSON → `sign.ParseEnvelope` → `store.VerifyCatalog` (der Weg, den er wirklich nimmt) | angenommen |

## Die Grammatik der Download-Adresse (Punkt 25 der Verträge)

`store.validHTTPSURL` ist klein geschrieben und von außen nicht aufrufbar. Es gibt **keine** zweite
Zeichengrammatik in diesem Werkzeug: `Catalog` setzt die Adresse zusammen und lässt
`store.Catalog.Validate` urteilen — dieselbe Regel, die ein Leser über `store.VerifyCatalog`
anwendet. Belegt in `TestCatalogRefusesAnAddressOutsideTheGrammar`: eine erlaubte Basis auf
demselben Verzeichnis geht durch (Zwischenprüfung, damit das Rot nicht eine frühere Wand ist),
acht Basen werden mit `store.ErrCatalog` abgewiesen (`http://`, Fragment, Leerzeichen, `<`,
Benutzerteil, Port 0, `ftp://`, länger als die 512 Byte von store).

**Eine eigene Regel gibt es, und sie steht offen im Code:** eine Basis, die **schon eine
Abfrage** (`?`) trägt, bekommt keinen Dateinamen angehängt (`ErrPack`). Das ist keine
Zeichengrammatik, sondern eine Aussage über die Form der Basis — `…?token=x/app.ngc` bleibt
**innerhalb** der Grammatik von store, der Vertrag kann es also nicht fangen, und auf keinem
Server benennt es eine Datei.

## Was gegen eine zurückgedrehte Folgenummer greift

Nicht nur eine Zusicherung im Vertrag, sondern ein Riegel **vor** dem ersten gelesenen Paket:

- `Catalog` liest das `catalog.json`, das im Verzeichnis liegt, **so wie eine Installation es
  liest** (`sign.ParseEnvelope`, dann `store.VerifyCatalog` gegen den öffentlichen Schlüssel der
  Schlüsseldatei und die Store-Kennung) und verlangt `opts.Seq > gespeicherte Seq`.
- **Strenger als der Vertrag, mit Absicht:** `store.VerifyCatalog` nimmt eine **gleiche** Folge an
  (`c.Seq < lastSeq` ist der Rückdreh), dieses Werkzeug nicht. Eine zweite Unterschrift unter
  derselben Nummer sind zwei verschiedene Dokumente mit derselben Folge.
- Ein `catalog.json`, das mit **diesem** Schlüssel nicht lesbar ist (kaputt, fremder Schlüssel,
  anderer Store), ist `ErrSeq` und **keine 0**: es sagt nichts darüber, welche Nummern die Leser
  schon gesehen haben. Die sicherere Variante.
- Gemessen in `TestCatalogRefusesASequenceThatDoesNotRise`, **beide Seiten der Grenze**: nach
  einem geschriebenen Katalog mit Folge 7 werden 6 und 7 mit `ErrSeq` abgewiesen, 8 geht durch;
  Folge 0 wird abgewiesen; ein kaputtes und ein fremd unterschriebenes `catalog.json` sind
  `ErrSeq`. Nach jeder Abweisung steht noch der Katalog mit Folge 7 im Verzeichnis.

## Je Abnahmekriterium

| Kriterium (Plan, Zeile 9903 ff.) | Fall | belegt |
|---|---|---|
| jedes `.ngc` wird ein Eintrag: ID, Name, Publisher aus dem Manifest, je Version `url`, `size`, `sha256`, `ui`, `minNgc` | `TestCatalogListsEveryPackage` | ja |
| `ui` ist `store.PackageUI` über das **unverschlüsselte** Paket, bei einer Kauf-App vor dem Verschlüsseln gerechnet, `encrypted` = true | `TestCatalogComputesUIBeforeEncryption` | ja (mit Platzhalter für die `.enc`, siehe Ungemessenes) |
| `minNgc` aus dem Manifest | `TestCatalogTakesMinNGCFromTheManifest` (mit Gegen-App ohne `minNgc`) | ja |
| `store.VerifyCatalog` nimmt das Ergebnis an; kleinere Seq ist `store.ErrRollback` | `TestCatalogVerifies` | ja |
| über `store.MaxCatalogVersions` Versionen sind ein Fehler, keine halbe Datei | `TestCatalogRefusesTooManyVersions`, `TestAssembleRefusesTooManyVersionsInTotal` | teils, siehe unten |
| Versand kennt `catalog` | `TestRunWritesTheCatalog`, `TestRunKnowsItsSubcommands`, `TestRunPrintsTheUsageForAWrongCall` | ja |

**Die Versionsgrenze, mit der Grenze der Messung:** `store.MaxVersions` (100) je App ist **durch
`Catalog` hindurch** gemessen, beide Seiten — 100 Versionen werden unterschrieben, 101 sind
`store.ErrCatalog`, und danach liegt **kein** `catalog.json` im Verzeichnis.
`store.MaxCatalogVersions` (30 000 insgesamt) ist **nicht** durch `Catalog` gemessen: sie braucht
mindestens 301 Apps mit je 100 Versionen, also über 30 000 echte Pakete, und die schreibt kein
Test. Sie ist eine Ebene tiefer gemessen, an `assemble` (die Funktion, die den Katalog baut und
prüft), beide Seiten der Grenze. Was `Catalog` darüber hinaus tut — das Verzeichnis lesen —
hängt am Fall mit den 101 Versionen.

## Kein Schlüssel, keine Passphrase in Protokoll, Fehler oder Prozessliste

`ngc-store` nimmt eine **Schlüsseldatei**, nie eine Passphrase und nie einen Schlüssel als
Argument. Gemessen (`TestCatalogTellsNoSecret`): vier Läufe — einer, der arbeitet, und drei, die
abgewiesen werden (Folge steigt nicht, keine Zahl, Verzeichnis fehlt) — und in **stdout**,
**stderr** und der **Argumentliste** kommt der Samen in keiner von vier Gestalten vor (Klartext,
base64url, hex klein, hex groß). Dazu eine Kontrolle im Fall selbst: jeder Lauf druckt überhaupt
etwas, der Fall ist also nicht grün, weil nichts ausgegeben wurde. `TestCatalogRefusesABrokenKeyFile`
hält zusätzlich fest, dass die Fehlermeldung den Inhalt der Datei nicht zitiert (`ErrPack`).

## Mutanten

Treiber: `…/scratchpad/a28/mutate.py`, eigener Unterordner dieser Sitzung, `-overlay` gegen Kopien
mit Windows-Pfaden (`C:/…`), je Mutant `go test -count=1`. Der Treiber prüft **zuerst seine eigene
Vorbereitung**: der ersetzte Text muss genau einmal vorkommen und der Mutant muss **bauen** —
sonst `BROKEN`, nie „lebt". Dichtheitsprüfung vorweg: unveränderte Quelle Ausstiegscode 0.

**14 von 14 gewollt tot, 1 Kontrolle gewollt lebendig, 0 `BROKEN`.** Protokoll:
`…/scratchpad/a28/final.txt`.

| Mutant | was weggenommen wurde | gewollt | gemessen (rote Fälle) |
|---|---|---|---|
| M1 | der Riegel `opts.Seq <= last` | tot | tot (2) |
| M2 | ein unlesbares `catalog.json` gilt als Folge 0 statt `ErrSeq` | tot | tot (1) |
| M3 | Größe und SHA-256 vom **unverschlüsselten** Paket | tot | tot (1) |
| M4 | `encrypted` immer false | tot | tot (1) |
| M5 | der Preis wird nicht gelesen | tot | tot (1) |
| M6 | die Metadaten der ältesten Version gewinnen | tot | tot (1) |
| M7 | `c.Validate()` in `assemble` | tot | tot (3) |
| M8 | die Sortierung der Versionen | tot | tot (2) |
| M9 | `minNgc` wird nicht übernommen | tot | tot (1) |
| M10 | `needs` wird nicht übernommen | tot | tot (1) |
| M11 | die Abweisung einer Basis mit `?` | tot | tot (1) |
| M12 | `BaseURL` wird ignoriert | tot | tot (2) |
| M13 | der Schlüsselfehler zitiert den Dateiinhalt | tot | tot (1) |
| M14 | die Sortierung der Apps nach ID | tot | tot (1) |
| KONTROLLE | eine lokale Variable umbenannt | lebt | lebt (0) |

M8 war in der ersten Runde `BROKEN` („baut nicht"), weil der Ersatz für die Sortierung fehlte —
genau der Fall, den `messen.md` beschreibt: ein Mutant, der nicht übersetzt, ist kein lebender
Mutant. Erst die bauende Fassung starb an zwei Fällen.

## Ungemessenes

- **`go test -race` ist nicht gelaufen.** `-race` braucht cgo, cgo einen C-Übersetzer. Die Suche:
  `command -v gcc clang cc x86_64-w64-mingw32-gcc` → alle vier leer; `C:/Program Files/LLVM/bin/clang.exe`
  und `C:/mingw64/bin/gcc.exe` gibt es nicht; `find` über `C:/Users/User` nach `*mingw*`, `gcc.exe`,
  `clang.exe` → nichts; WSL hat `gcc` (`/usr/bin/gcc`), aber **kein** `go`. `Catalog` startet keine
  Goroutine — das ist gelesen, nicht gemessen; auf einem Linux-Server nachziehen.
- **Die `.enc`-Datei ist ein Platzhalter.** `ngc-store encrypt` (Aufgabe 27) gibt es noch nicht.
  Gemessen ist, dass Größe und SHA-256 **dieser Datei** in den Eintrag kommen und der ui-Hash vom
  unverschlüsselten Paket. **Nicht** gemessen: dass Aufgabe 27 wirklich unter `<name>.ngc.enc`
  schreibt. Das muss beim Zusammenführen abgeglichen werden.
- **Die Preisdatei ist eine Festlegung dieses Werkzeugs, keine Vertragstatsache.** Der Vertrag
  verlangt zu jeder verschlüsselten Version einen Preis im Eintrag (`store.Entry.valid` bindet
  `Encrypted` an `Price`), das Manifest kennt keinen Preis, und `CatalogOpts` des Plans auch nicht.
  Darum liest `Catalog` `<name>.price.json` neben dem Paket. Im Plan steht das nicht.
- **Kein Fuzz-Ziel.** `Catalog` liest ein Verzeichnis, keinen Datenstrom. Die Parser dahinter
  (`store.ParseManifest`, `store.Walk`, `enc.Budget`) haben ihre Fuzz-Ziele in `shared/`. Die
  Preisdatei geht über `enc.Budget.Decode`, also durch einen gefuzzten Dekoder.
- **Das atomare Schreiben in `run.go` ist nicht mutiert.** Gemessen ist nur, dass nach einem Lauf
  keine `catalog.json.new` liegen bleibt; ein Mutant, der direkt in die Zieldatei schreibt, bliebe
  grün. Ein Riegel dagegen bräuchte einen Abbruch mitten im Schreiben.
- `apps/cli/VERSION` ist nicht angefasst (der Plan verlangt es hier nicht).

## Abweichungen vom Plan

1. **Signatur von `entryOf`.** Der Plan gibt `(*store.Entry, store.Package, error)` zurück; hier
   ist es ein `appVersion` (Eintrag + Version), damit `assemble` getrennt messbar ist (die Grenze
   der 30 000 Versionen).
2. **Metadaten der neuesten Version.** Der Plan nimmt Name, Publisher und Beschreibung aus dem
   **erst gelesenen** Paket. Hier gewinnt die **neueste** Version: eine Umbenennung oder ein neuer
   Preis kommt mit ihr. Gemessen in `TestCatalogTakesTheMetadataOfTheNewestVersion`, gegen die
   Reihenfolge auf der Platte.
3. **`ErrSeq` und das Lesen des vorigen Katalogs** stehen nicht im Plan (siehe oben).
4. **Die Preisdatei** steht nicht im Plan (siehe Ungemessenes).
5. **`run_test.go` geändert:** `catalog` ist aus der Liste „noch nicht gebaut" heraus, und die
   Prüfung „jeder Unterbefehl des Hilfetexts wird verstanden" ruft jetzt jeden mit **seiner
   eigenen Argumentzahl** auf. Eine Form für alle hätte die Argumentzahl gemessen, nicht den
   Versand: `catalog x y` ist ein falscher Aufruf (2), auch wenn der Unterbefehl existiert.
6. **`Catalog` schreibt nicht**, `Run` schreibt. Der Plan sagt dazu nichts; so bleibt die Funktion
   prüfbar, ohne eine Datei anzulegen.

## Was unterwegs schiefging (gemessen)

1. **Ein Kommentar sagte das Gegenteil des Codes — in meiner eigenen ersten Fassung.** Über
   `newest` stand „`store.Newer` nennt die leere Zeichenkette älter als jede gültige Version".
   Falsch: `store.Newer` ist **false**, sobald eine Seite keine Version ist, die leere
   eingeschlossen. Darum hat die erste Fassung die Metadaten **nie** ersetzt, und
   `TestCatalogTakesTheMetadataOfTheNewestVersion` war rot. Behoben mit einem ausdrücklichen Test
   auf `""`; der Kommentar sagt das jetzt.
2. **Zwei Mutanten waren erst nach einem neuen Fall tötbar.** Ohne
   `TestCatalogSortsByTheContractNotByFileName` lebten „keine Versionssortierung" und „keine
   Sortierung nach ID": in allen anderen Fällen stand die Reihenfolge der Dateinamen **zufällig
   richtig**. Der Fall dreht die Dateinamen gegen die Reihenfolge des Vertrags.
3. **Eine Basis mit Abfrage war ein stiller Fehler.** Die erste Fassung hängte den Dateinamen an
   `https://…/pkg?token=abc` an. `store.VerifyCatalog` nimmt das an — die Zeichen sind erlaubt —
   und der Fall war grün gegen eine Adresse, die kein Server auflöst. Jetzt abgewiesen, mit Fall.
4. **Der Heredoc frisst Escapes** (steht so im Gedächtnis): `\n` im Python-Text einer Heredoc
   wurde ein echter Zeilenumbruch und damit ein Go-Übersetzungsfehler. Abhilfe: `chr(92)`.

## BLOCKED

Keines. `shared/` ist nicht angefasst und war nicht zu ändern: `store.validHTTPSURL` bleibt
unaufrufbar, und dieses Werkzeug braucht sie nicht — es belegt seine Adressen über
`store.VerifyCatalog`. Der offene Punkt 25 der Verträge (ein ausgeführtes
`store.ValidDownloadURL`) bleibt damit, wie Aufgabe 19 ihn gemeldet hat.

## Hinweis an die Nachbarn

`readSigningKey` liegt in `catalog.go` (so steht es im Plan). Aufgabe 27 (`license.go`) und
Aufgabe 29 (`release.go`) brauchen dieselbe Funktion — **nicht zweimal anlegen**, sonst gibt es
beim Zusammenführen zwei gleich benannte Funktionen in einem Paket. Ebenso `CatalogFile`,
`EncSuffix`, `PriceSuffix` und `ErrSeq`.
