# Aufgabe 26: `ngc-store pack`

Zweig `api-t26` (aus `api`, `ecdceb6`). Arbeitsbaum außerhalb von `Z:` (Pfadlänge).

## Was gebaut wurde

| Datei | git hash-object |
|---|---|
| `apps/cli/classes/pack.go` (neu) | `3c378a39b747c927c99e44df6aea83027d0d2f1a` |
| `apps/cli/classes/run.go` (neu, Versand) | `bba6532cf4504925449d190777a50629f235aad6` |
| `apps/cli/classes/pack_test.go` (neu) | `85aaf0aad3b713029e2791df68d439906cfcb547` |
| `apps/cli/classes/run_test.go` (neu) | `09cf17241ee978bf6ba86be06ce588d467369045` |
| `apps/cli/main.go` (geändert) | `005c98c46f1e74bb0e1d8777c46e7a62c7f1a57e` |

`Pack(dir, out)` schreibt `manifest.json`, dann die Dateien unter `ui/` nach Pfad sortiert, dann
`services/amd64.tar` und `services/arm64.tar`, und gibt Manifest und SHA-256 der Datei zurück.
`Run(args, stdout, stderr)` ist der Versand: `pack` arbeitet, die fünf Namen der Aufgaben 27–29
melden „noch nicht gebaut" (1), ein falscher Aufruf gibt die Benutzung aus (2). Meldungen Deutsch.

Dateien der Aufgaben 27–29 (`crypt.go`, `license.go`, `catalog.go`, `release.go`) sind nicht
angefasst; der Versand liegt in `run.go`, nicht in einer ihrer Dateien.

## Testergebnis

```
go test -count=1 ./apps/cli/...   -> ok, exit 0 (7,6 s)
bash tools/check.sh (ohne Pipe)   -> "check: ok", exit 0
git diff --name-only --diff-filter=D HEAD | wc -l -> 0 (im selben Aufruf)
```

Achtung, bezahlte Falle: `git` sieht diesen Arbeitsbaum nur mit
`-c safe.directory=<Baum>` (sonst „dubious ownership", Code 128 — und eine Pipe daneben hätte
die 0 gemeldet). Alle gemeldeten Git-Zahlen sind so gemessen.

## Je Abnahmekriterium

| Kriterium (Plan) | Test | belegt |
|---|---|---|
| Paket ist mit `store.Walk`/`store.PackageUI` lesbar, ID und Version stimmen | `TestPackWritesAReadablePackage` | ja |
| unsichere Namen abgewiesen, bevor etwas geschrieben wird | `TestPackRefusesUnsafeNames`, `TestPackRefusesWhatAHostileDirectoryTries` | ja |
| ui-tar und SHA-256 wie in `shared/testdata/packui.json` | `TestPackMatchesTheVectors` (3 Fälle) | ja |
| Versand `classes.Run` kennt alle sechs Unterbefehle, 2 bei falschem Aufruf | `TestRunPacks`, `TestRunKnowsItsSubcommands`, `TestRunPrintsTheUsageForAWrongCall` | ja |
| Köpfe wie die Vektoren (USTAR, 0644, Eigner 0, Zeit 0), Reihenfolge | `TestPackWritesTheHeadersOfTheVectors`, `TestPackWritesALongNameAsUSTAR` | ja |

## Bitgleich (gemessen, nicht angenommen)

Zweimal dasselbe Verzeichnis gepackt, zwischen den Läufen die Zeitstempel geändert:

```
ngc-store pack app one.ngc   -> 6ea7559723fd889f3e483139cd2f5dff0b718e0961de6c03d434ae82581d45e5
(touch ui/index.html manifest.json; Platte: 1791798329 gegen 1791798327)
ngc-store pack app two.ngc   -> 6ea7559723fd889f3e483139cd2f5dff0b718e0961de6c03d434ae82581d45e5
git hash-object one.ngc two.ngc -> 57327449bd9b19617a35cf39abb88ccad05b7ec5 (beide)
```

`git hash-object`, nicht `sha256`: damit jede gemessene Datei gegen den Stand im Commit gehalten
werden kann. Die Zwischenprüfung (geänderter Zeitstempel auf der Platte) steht auch im Test
`TestPackIsReproducible`, sonst wären zwei gleiche Pakete nur zwei Läufe über ein unverändertes
Verzeichnis.

**Entscheidung Zeitstempel, im Code begründet:** Änderungszeit **0** (1970-01-01), Rechte 0644,
Eigner 0 — nichts von der Platte, nicht die Uhr, nicht das 1980 anderer Packer. Sonst hinge die
SHA-256, die ein Katalog nennt, daran, *wann* jemand gepackt hat. Die Leser lesen die Zeit ohnehin
nicht: `store.WalkUI` nimmt im ui-tar nur die feste `00000000000`.

**Form USTAR:** gemessen mit einem Namen von 150 Byte — mit `tar.FormatPAX` kommen **dieselben
Bytes** heraus (`git hash-object` beide `5bb690f3671cc0abb5335b2790c5aa439916273b`). `FormatUSTAR`
steht also für das, was es **abweist**, nicht für das, was es schreibt: ein Name, der auch im
prefix-Feld nicht Platz hat (`store.ValidPath` erlaubt 200 Byte), wird abgewiesen statt als
PAX-Eintrag geschrieben, denn `packui.json` hält die Pakete von ngc-store durchgehend als USTAR.

## Was ein böswilliges Eingabeverzeichnis nicht erreicht

Je Angriff **zwei Zwischenprüfungen** am unveränderten Verzeichnis: dieselbe App packt, und mit
einem Ausgabepfad, der nicht angelegt werden kann, kommt der Lauf bis zum `os.Create` und
scheitert mit dessen Fehler (nie `ErrPack`). Dann wird der Angriff gegen **denselben**
unschreibbaren Pfad gefahren: kommt `ErrPack`, war er abgewiesen, **bevor** die Ausgabedatei
überhaupt angefasst wurde. Genau diese Schärfung hat fünf lebende Mutanten getötet.

| Angriff | Ergebnis |
|---|---|
| versteckte Datei `ui/.env` | abgewiesen (`store.ValidPath`), vor dem ersten Byte |
| Name, den `store.ValidPath` ablehnt (`ui/a b.css`) | abgewiesen, vor dem ersten Byte |
| Verweis aus der App heraus (Verzeichnis) | abgewiesen (nicht-reguläre Eintragsart), vor dem ersten Byte |
| Datei über `store.MaxUIFile` (16 MiB + 1) | abgewiesen, ohne sie zu lesen |
| Dateien über `store.MaxUITotal` (5 × 16 MiB) | abgewiesen, ohne sie zu lesen |
| mehr Einträge als `store.MaxEntries` (10 000 + 1) | abgewiesen |
| „Abbild", das ein Verzeichnis ist | abgewiesen |
| Name, den kein USTAR-Kopf hält (120 Byte in einem Teil) | abgewiesen, keine Datei bleibt liegen |
| `..`, absoluter Pfad, Steuerzeichen, `ui/`, `ui`, `etc/passwd`, `manifest.json/x`, `services/x86.tar` | `validEntry` lehnt alle 17 ab (`TestValidEntryRefusesNamesNoDirectoryCanHold`) |
| Manifest verspricht ein `ui`-Eintrag, das es nicht gibt | `store.ErrPackage` aus dem Zurücklesen |
| Datei verschwindet **während** des Laufs | Abbruch; nur ein ganz fehlendes `ui/` ist erlaubt (Dienst-App) |

`..`, absolute Pfade und Steuerzeichen kann **kein Verzeichnis auf diesem Rechner tragen** — sie
sind an `validEntry` gemessen, der Stelle, die jeder Name auf dem Weg der Bytes zweimal passiert
(in `collect` und direkt vor dem Schreiben). Die Regel selbst ist `store.ValidPath` aus `shared/`;
es gibt keine zweite eigene Fassung davon.

Nach jedem Abbruch bleibt **keine** Datei liegen (in jedem Fall geprüft).

## Mutanten

Treiber: eigener Unterordner der Sitzung, `-overlay` mit Windows-Pfaden (`C:/…`), je Mutant
`go test -count=1`. Der Treiber prüft erst seine eigene Vorbereitung (Text genau einmal vorhanden,
Mutant übersetzt) und meldet sonst `BROKEN`, nie „lebt". Dichtheitsprüfung vorweg: unveränderte
Quelle grün.

**13 von 13 gewollt tot, 1 Kontrolle gewollt lebendig, 0 Probleme.**

| Mutant | gewollt | gemessen |
|---|---|---|
| M1 Verweise/Geräte angenommen | tot | tot |
| M2 Namensprüfung in `collect` weg | tot | tot |
| M3 beide Namensprüfungen weg | tot | tot |
| M4 `validEntry` nimmt alles | tot | tot |
| M5 Größengrenzen weg | tot | tot |
| M6 Grenze der Eintragszahl weg | tot | tot |
| M7 halb geschriebene Datei bleibt liegen | tot | tot |
| M8 Änderungszeit von der Platte | tot | tot |
| M9 Reihenfolge der ui-Dateien umgedreht | tot | tot |
| M10 Zurücklesen mit `store.Walk` übersprungen | tot | tot |
| M11 Form PAX statt USTAR | tot | tot |
| M12 Abbild, das keine reguläre Datei ist, wird genommen | tot | tot |
| M13 Dienst-App verliert ihre Abbilder | tot | tot |
| KONTROLLE lokale Variable umbenannt | lebt | lebt |

M2–M6 und M11 lebten in der **ersten** Runde: `store.Walk` im Zurücklesen fing alles ab, die
frühen Prüfungen waren nur Vorhut. Erst die Zwischenprüfung „abgewiesen, bevor die Ausgabedatei
angefasst wird" (und bei M11 der Fall mit dem 120-Byte-Namen) macht sie messbar. Ein lebender
Mutant war hier zuerst eine Frage an die Messung — zu Recht.

## Rot vorher

Plan-Schritt 2: `pack.go` und `run.go` per Überlagerung auf leere Dateien gelegt →
`undefined: Pack`, `undefined: ErrPack`, `build failed`. Das ist ein **Bauabbruch** und belegt nur,
dass die Namen neu sind. Das echte Rot je Riegel sind die 13 Mutanten oben.

## Ungemessenes

- **`go test -race` ist auf diesem Rechner nicht gelaufen:** `-race` braucht cgo, cgo braucht gcc,
  gcc fehlt (`cgo: C compiler "gcc" not found`), und WSL hat kein Go (`command -v go` leer). Der
  Packer startet keine Goroutine, aber **gemessen ist das nicht** — auf einem Linux-Server
  nachziehen.
- **Ein Verweis auf eine *Datei* ist hier nicht messbar:** `os.Symlink` scheitert mit „Dem Clienten
  fehlt ein erforderliches Recht". Der Fall sagt das laut (`LOUD SKIP, NO RESULT`) und überspringt
  sich; auf Linux läuft er. Der Verweis auf ein **Verzeichnis** ist gemessen: Windows erlaubt eine
  Verzeichnis-Verbindung ohne Recht (`mklink /J`), Go meldet sie als nicht-regulären Eintrag
  (Modus `?rw-rw-rw-`), und `Pack` weist sie ab. Dafür steht ein Rückfall im Test, nur für Windows.
- Kein Fuzz-Ziel: `Pack` liest ein Verzeichnis, keinen Datenstrom; die Parser dahinter
  (`store.ParseManifest`, `store.Walk`) haben ihre Fuzz-Ziele in `shared/store`.
- `apps/cli/VERSION` ist nicht angefasst (der Plan verlangt es in dieser Aufgabe nicht).

## BLOCKED

Keines. `shared/` ist nicht angefasst; alles Nötige war vorhanden
(`store.ValidPath`, `store.Walk`, `store.PackageUI`, die Grenzen und `packui.json`).
