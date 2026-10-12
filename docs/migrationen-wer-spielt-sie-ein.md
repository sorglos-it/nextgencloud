# Wer spielt die Migrationen einer neuen NGC-Fassung ein?

**Messauftrag, 12.10.2026. Nichts gebaut, kein Gerüst.** Jede Aussage ist mit
**GEMESSEN** oder **GESCHLOSSEN** gekennzeichnet. Gemessene Stände:
`api` 639bf84, `controller` 33ac964, `db` 98741b5, `main` 6cf0449.

---

## Die kurze Antwort

**Empfohlen: Weg (a) — der Controller mit der Rolle `db` ruft `ngc-api migrate`
aus der bereitgelegten Datei.** Der Weg ist gebaut bis auf drei Dinge, und alle
drei sind benannt statt vermutet.

**Aber Punkt 28 kann in seiner Begründung nicht so stehen bleiben.** Die
Begründung dort ist gemessen falsch, und die Entscheidung bleibt trotzdem
richtig — aus einem anderen Grund.

---

## Vier Sätze, die der Auftrag so nicht halten kann

**1. Es gibt kein „db-Programm". GEMESSEN.**
`git ls-tree -r --name-only db -- apps/server-db` nennt **kein `main.go`**.
`apps/server-db` ist eine Sammlung von Paketen. Die Migrationen stecken also
nicht in einem Programm, sondern in einer **Bibliothek** — und wandern in jedes
Programm, das sie einbindet.

**2. Auf dem Zweig `api` gibt es `apps/server-db` praktisch nicht. GEMESSEN.**
Dort liegen genau zwei Dateien: `CLAUDE.md` und `VERSION`. Kein Go-Code. Die
Naht `RegisterMigrator` kann auf diesem Zweig niemand bedienen.

**3. `ngc-controller` trägt die Migrationen schon heute. GEMESSEN.**
Die Importkette ist eine Zeile lang:
`apps/server-controller/classes/state/open.go:11` importiert
`apps/server-db/classes/sqlite`; `sqlite/store.go:34` ruft `migrate.Apply`;
`migrate.go` importiert `apps/server-db/sql` (die eingebetteten `*.sql`).
Im Controller-Programm stecken damit: der SQLite-Treiber, der XTS-VFS, das
Paket `migrate` **und jede Migrationsdatei**.

**4. `core.All()` hat keine Aktion, die migriert. GEMESSEN.**
Die Liste in `shared/core/core.go:131–170` nennt auf `ServiceController`
`activate`, `deactivate`, `uninstall`, `delete-data`, `stage`, `install`,
`license` — keine achte.

---

## Was der Reihe nach wirklich passiert

**Den Takt gibt die API. GEMESSEN.**
`apps/server-api/classes/updates/rollout.go`:

- `rolloutOrder = {ngc-api, ngc-controller, ngc-web}`.
- **Vor** der Liste wird dem Migrationsknoten die `ngc-api`-Datei
  **bereitgelegt** (`u.stage(...)`), und nur das.
- `MigrationNode()` = unter den verbundenen Controllern mit Rolle `db` der mit
  der kleinsten Knoten-ID.
- Wer den Durchgang fährt, entscheidet **`dbapi.LockUpdate`** in der
  Datenbank; die anderen APIs antworten `ErrNotDriving`. Pacht 2400 s.

**Damit ist die Frage „wer erzwingt die Reihenfolge bei mehreren API-Servern"
beantwortet, und zwar gebaut:** zwei Schlösser hintereinander —
`dbapi.LockUpdate` für den Durchgang und `ngc_lock` für die Migration selbst
(`migrate.Apply`, `take`/`hold`, 900 s).

**Und der Kommentar in `rollout.go` sagt die Lücke selbst:**
„Playing the file in is the controller's step and is not built on its branch
either; the file lying ready is what this side can do and all of it."

---

## Wann die Migrationen laufen müssen

**Vorher. GEMESSEN** aus `rolloutOrder`: die Datenbank kommt vor allen Teilen,
die neue API danach.

**Und die alte API darf dabei weiterlaufen — das ist kein Versprechen, sondern
ein Riegel. GEMESSEN** in `apps/server-db/classes/migrate/lint.go`:

- `forbidden` verbietet 13 Wörter, darunter `DROP`, `RENAME`, `TRUNCATE`
  (gezählt, nicht übernommen).
- `kinds` erlaubt nur `CREATE TABLE`, `CREATE [UNIQUE] INDEX`, `ALTER TABLE`.
- Eine später hinzugefügte Spalte **muss** nullable sein (`errAddedNullable`).
- `migrate.List()` prüft das **zur Laufzeit** bei jedem `Apply`, nicht nur im
  Test.

Eine Migration kann also nichts wegnehmen, was die alte API liest. Der
ungemessene Punkt der db-Abschlussprüfung ist damit **auf der SQL-Ebene
geschlossen**. **Nicht** geschlossen: ob eine *neue* Spalte mit Bedeutung
(etwa ein Zähler, den nur die neue API pflegt) der alten API etwas antut — das
ist keine Frage des Linters.

---

## Die Naht `dbopen.RegisterMigrator`

**Wer müsste sich anmelden: `apps/server-db`. GEMESSEN** aus dem Paketkopf von
`dbopen/migrate.go` („apps/server-db implements it … and registers it here,
exactly as it registers its Opener").

**Niemand tut es — und auch `Register` (den Opener) ruft kein Produktionscode.
GEMESSEN:** `git grep "dbopen\.\(Register\|RegisterMigrator\|Migrate\)"` findet
außer Tests nur `main.go:147`. Also:

> **`ngc-api` kann heute nicht einmal starten.** `server.go:118` ruft
> `dbopen.Open`, und ohne Anmeldung kommt `ErrNoOpener`.

**Daraus folgt der Satz, der Punkt 28 die Begründung wegnimmt. GESCHLOSSEN aus
den beiden Messungen oben:** `ngc-api` **muss** `apps/server-db` einbinden,
damit es überhaupt die API sein kann. Die Treiberzeilen wandern ohnehin aus
`tools/modules.go` heraus. `RegisterMigrator` kostet danach **keine
Abhängigkeit mehr**, sondern eine Zeile neben `Register`.

**Eine Anpassung ist nötig, keine reine Zeile. GEMESSEN:** die Formen passen
nicht. `migraterun.Run(ctx, cfg, owner string, now int64) (int, error)` gegen
`dbopen.Migrator = func(ctx, cfg, out io.Writer) error`. Es braucht einen
kleinen Umsetzer, der `owner` und `now` beschafft und die Zahl nach `out`
schreibt.

**Programmgröße: nicht gemessen.** Kein Programm dieses Repos ist je gebaut
worden (STAND.md). Als Anhaltspunkt, ausdrücklich **kein** Binärmaß:
`vendor/github.com/ncruces/go-sqlite3` 556 KiB, `go-sql-driver/mysql` 296 KiB,
`go-sqlite3-wasm/v6/sqlite3.dat` 103 092 Byte Quelle.

---

## Weg (a): der Controller ruft `ngc-api migrate`

**Dafür ist da. GEMESSEN:**

- `ngc-api migrate` ist gebaut: `main.go`, `subcommand`, `migrate()`. Der
  Unterbefehl wird sogar vor der Falle geschützt, dass `flag` beim ersten
  Nicht-Flag stehenbleibt und `-config` verliert.
- Die Datei liegt bereit: `/var/lib/ngc/staged/ngc-api`, geschrieben von
  `update/stage.go:466–516`, Name per `os.Rename`.
- Der Controller **führt schon Programme aus**: `update/install.go`
  `VersionOf` ruft `exec.CommandContext(ctx, file, "-version")`, ohne Shell,
  mit Argumentliste, Ausgabe begrenzt.
- Die Zugangsdaten sind im Vertrag: `config.Controller.Migrate *DB`, und
  `config.go:676` erzwingt, dass **genau** die Rolle `db` sie hat.

**Was fehlt — drei Dinge, alle gemessen:**

1. **Die bereitgelegte Datei ist `0600` und damit nicht ausführbar.**
   `stage.go:508–513` setzt den Modus ausdrücklich, mit Begründung;
   `install.go` `copyInto` setzt `0755` erst beim Einbauen. Ein `exec` auf die
   Stage-Datei scheitert. (Nebenbefund derselben Stelle: **beide** Modi sind
   laut ihren eigenen Kommentaren von keinem Fall gedeckt.)
2. **`ngc-api migrate` liest eine `api.json`, die es auf einem reinen
   db-Knoten nicht gibt.** `migrate()` → `load()` → `config.LoadAPI` →
   `(*API).Validate` (`config.go:532`) verlangt Knoten-ID, Tunnel-URL,
   `listen`, zwei Zertifikatspfade, **vier** Schlüssel à 32 Byte,
   `releaseKey`, `storeKey`, Store-URL und zwei Laufzeiten. Ein db-Knoten hat
   `controller.json` mit `Migrate` — und `controller.json` fällt durch
   `parseAPI`. **Es gibt keinen Weg, `ngc-api migrate` nur eine `config.DB` zu
   übergeben.**
3. **`config.Controller.Migrate` wird von keinem Programm gelesen. GEMESSEN:**
   `git grep` findet außer `Validate` und `LogValue` keinen Leser. Das Feld
   steht im Vertrag und ist toter Code.

**Was schiefgehen kann:**

- **Kein zweiter Läufer.** Weg (a) erzeugt keine zweite Stelle, die migriert —
  genau der Punkt, um den es Punkt 28 geht.
- **Aber eine zweite Stelle entsteht trotzdem woanders:** `install` im
  Controller prüft für NGC-Teile **die Rolle nicht**. GEMESSEN: in
  `update/install.go` kommt das Wort `Roles` **nicht ein Mal** vor, und
  `update.Deps` hat kein solches Feld. Ein `install ngc-api` auf einem
  db-Knoten würde das Programm nach `/usr/lib/ngc` kopieren und
  `systemctl restart ngc-api` fahren. Dass das nicht passiert, hängt allein
  daran, dass `rollout.go` diesen Knoten nicht in `nodesFor(ngc-api)` hat —
  **eine Wand auf der falschen Seite der Leitung.**
- **Die Datei ist nicht geprüft, wenn man sie ausführt.** Die SHA-256-Prüfung
  gegen die signierte Release-Zeile sitzt in `install.go` (`sameFile`), nicht
  im Bereitlegen. Wer die Stage-Datei als Programm startet, führt sie **vor**
  dieser Prüfung aus. Das ist der gefährlichste Teil von (a), und er ist
  lösbar: `sameFile` zuerst, dann kopieren, `0700`, ausführen, wieder weg.

---

## Weg (b): `ngc-api` migriert beim eigenen Start

**Dafür ist da. GEMESSEN:** auf SQLite ist das schon so. `sqlite.Open` ruft
`migrate.Apply` bei **jedem** Öffnen (`sqlite/store.go:34`), und
`state/open.go` nennt das ausdrücklich den Normalfall für SQLite.

**Was fehlt:** eine Zeile in `server.Build` — technisch wenig.

**Was schiefgeht, und das ist viel. GEMESSEN:**

1. **Die Migrationssperre wartet nicht.** `migrate.Apply` → `take` → bei
   gehaltener Sperre sofort ein Fehler („the lock is held …"). Zwei
   API-Server, die derselbe Durchgang hintereinander neu startet, sind der
   Normalfall — und bei Überlappung **startet der zweite nicht**. Punkt 32 hat
   genau diese Form gemessen: 8 gleichzeitige Öffnungen, 30 Runden, in
   **10 von 30** traf es mindestens einen.
2. **Die Rolle ist weg.** Der Vertrag gibt die `ngc_migrate`-Zugangsdaten
   **nur** der Rolle `db` (`config.go:676`). Migriert die API beim Start, muss
   **jeder** API-Server migrieren dürfen — also hätte jeder API-Server den
   Zugang. Das ist eine Vertragsänderung in `shared/config`, nicht eine Zeile.
3. **Die Reihenfolge kippt.** `rolloutOrder` setzt die Datenbank **vor** die
   neue API. Bei (b) ist die neue API ihr eigener Vorläufer: wer migriert,
   läuft schon. Die Zusicherung „die Migrationen laufen, bevor die neue API
   startet" wäre nicht mehr wahr, und `rolloutOrder` müsste umgeschrieben
   werden.
4. **Zwei Wahrheiten.** Der Durchgang legt die Datei auf dem db-Knoten bereit
   (`rollout.go`, vor der Liste) **und** die API migriert beim Start. Das
   Bereitlegen wäre dann sinnlos, bliebe aber im Code stehen. **Das ist W-1 in
   der Form, die hier schon zweimal Geld gekostet hat.**

**Weg (b) ist damit verworfen** — nicht, weil er nicht ginge, sondern weil er
drei gebaute Zusicherungen gleichzeitig umdreht.

---

## Weg (c), den die Messung selbst anbietet

**Der Controller trägt die Migrationen schon (Satz 3 oben, GEMESSEN).** Was ihm
für das NGC-Schema fehlt, ist **nicht** „ein eigener SQL-Treiber und eine eigene
Zeile in `tools/modules.go`", wie Punkt 28 sagt, sondern:

- `mariadb.OpenDB` (und damit `go-sql-driver/mysql`) — **eine** Zeile von den
  fünf in `tools/modules.go`; die drei SQLite-Zeilen wandern ohnehin schon mit
  `state/open.go` mit,
- und `migraterun.Run`, etwa zwanzig Zeilen.

**Warum ich (c) trotzdem nicht empfehle. GESCHLOSSEN:** dann migrieren zwei
Programme eine Datenbank — `ngc-api migrate` **und** der Controller. Das ist
genau W-1, und zwar diesmal wirklich: zwei Stellen, die dasselbe tun, und
`migrate.List()` ist in beiden eine andere Übersetzung desselben `go:embed`.
**Zwei Programme mit verschiedenen Migrationsständen** sind schlimmer als eine
Naht, die noch nicht bedient ist.

**Die ehrliche Zwischenform, GESCHLOSSEN, nicht gebaut:** die `0600`-Datei
ausführbar zu machen ist ein Hilfsmittel; der Controller *kennt* das Programm
nicht, er startet es nur. Das bleibt **eine** Stelle, die migriert — nämlich
die, die in der Datei steckt, die gerade ausgeliefert wird. Das ist der Kern
von (a) und der Grund, warum (a) richtig ist.

---

## Kann Punkt 28 so stehen bleiben?

**Die Entscheidung: ja.** `ngc-api migrate` als Programm, der Controller bekommt
keinen Läufer. Richtig, und von dieser Messung gestützt.

**Die Begründung: nein, zwei Sätze sind gemessen falsch.**

| Satz in Punkt 28 | Messung |
|---|---|
| „ein zweiter Läufer im Controller braucht einen eigenen SQL-Treiber" | **Falsch.** Der Controller hat SQLite + XTS + `migrate` + die eingebetteten `*.sql` schon, über `state/open.go:11`. Es fehlt nur MariaDB. |
| „eine eigene Zeile in `tools/modules.go`" | **Die Zahl stimmt, der Grund nicht.** Es ist eine Zeile (`mysql`) — aber nicht, weil ein Läufer wenig braucht, sondern weil die drei SQLite-Zeilen durch `state/open.go` **längst** im Controller stehen. |
| „Die Migrationen … liegen als `go:embed` **im Programm**" | **Ungenau.** In einer Bibliothek, und die steckt in **zwei** Programmen. |
| „`shared/` ändert sich dadurch **nicht**" | **Wahrscheinlich falsch, GESCHLOSSEN.** `ngc-api migrate` verlangt eine volle `api.json`; ein db-Knoten hat keine. Entweder `shared/config` bekommt einen Weg, nur eine `config.DB` zu laden, oder der Controller schreibt eine Wegwerf-`api.json` — und das wäre eine zweite Wahrheit über die Zugangsdaten. |

**Drei Punkte sollten dazu, als offen benannt und nicht gebaut:**

1. **Wie `ngc-api migrate` an die `config.DB` kommt.** Heute: nur über eine
   vollständige `api.json`. Das ist die eigentliche Blockade von Aufgabe 21.
2. **Dass die bereitgelegte Datei `0600` ist.** Wer (a) baut, kopiert sie,
   prüft `sameFile` **vor** dem Ausführen, setzt `0700`, führt aus, räumt weg.
3. **Dass `install` die Rolle nicht prüft.** Dass heute kein db-Knoten ein
   `install ngc-api` bekommt, hängt an `rollout.go` — nicht am Controller.

---

## Was in dieser Messung NICHT gemessen ist

- **Nichts ist gelaufen.** Kein Programm dieses Repos wurde je gebaut
  (STAND.md). Alles oben ist am Quelltext gelesen, nicht am Verhalten.
- **Programmgrößen.** Nur Quellgrößen als Anhaltspunkt.
- **Ob eine neue nullable Spalte der alten API wehtun kann.** Der Linter deckt
  die Form, nicht die Bedeutung.
- **Zwei Controller auf einer SQLite-Datei** — steht schon als offen in
  Punkt 32.
