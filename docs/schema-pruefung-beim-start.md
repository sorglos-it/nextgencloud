# Spaltentypen beim Start prüfen – Befund und Entwurf

Stand 30.10.2026. Zweig `schema-pruefung`, gemessen auf `origin/main` 25b8f24e.
Alle Zahlen unten sind gemessen; der Befehl steht jeweils dabei.

## Die Frage

Beim Start prüft NGC, **dass** die Migrationen gelaufen sind. Es prüft nicht, **ob** die
Spalten in der Datenbank das sind, was der Code aus ihnen liest. Die Frage war: passiert
dann etwas Sichtbares, oder stiller Unsinn?

## Was heute beim Start geprüft wird

Reihenfolge von `ngc-api` (Beleg: `apps/server-api/main.go:180`):

1. `dbopen.Migrate` ruft den eingetragenen Läufer auf
   (`apps/server-api/classes/dbopen/migrate.go:46`). Die API hält selbst keinen Treiber
   und kein SQL; `apps/server-db` trägt sich ein.
2. `migrate.Apply` (`apps/server-db/classes/migrate/migrate.go:192`) legt die eigenen
   Tabellen an, nimmt das Schloss `ngc_lock` und geht die Migrationsdateien durch.
3. Je Datei fragt `applyOne` das Hauptbuch `ngc_schema`
   (`apps/server-db/classes/migrate/migrate.go:422`). **Steht dort `done`, kehrt die
   Funktion sofort um** – keine Anweisung wird noch einmal gefahren.
4. Bei SQLite spielt zusätzlich jeder `sqlite.Open` die Migrationen ein
   (`apps/server-db/classes/sqlite/store.go:44`).

Eine Prüfung der echten Spalten gibt es nur an **einer** Stelle, und nur im Wiederholungsfall:
`stepOver` (`apps/server-db/classes/migrate/already.go:72`) liest bei SQLite den Text des
Schemas und vergleicht ihn Stück für Stück mit der Anweisung. Diese Prüfung greift aber
nur, wenn die Anweisung noch einmal gefahren wird – also **nur solange die Migration nicht
als `done` verbucht ist**. Nach einem normalen Update ist sie verbucht, und niemand sieht
mehr hin. Für MariaDB fehlt diese Hälfte ganz und ist an ihrer Zeile als offen vermerkt
(`already.go:51`–`68`).

Grep über das Repo ohne `vendor/`: `table_info` nur in
`apps/server-db/classes/migrate/`, `information_schema` nur in Kommentaren. Thomas' Schluss
ist damit bestätigt.

## Die Beinahe-Widerlegung, und warum sie nicht hält

Es **gibt** schon eine Prüfung genau dieser Form: `unlistedInDB` in
`apps/server-db/classes/migrate/migrate_test.go:705`. Sie liest `pragma_table_info`, hält
jede Spalte gegen `sqlstore.Columns` und jeden Typ gegen die Typen, die der Dialekt machen
kann. Zwei Gründe, warum sie die Lücke nicht schließt:

- Sie läuft **im Test**, gegen eine frisch migrierte Datei. Kein Programm ruft sie.
- Ihre Typprüfung fragte nur, ob der Typ **einer** der Dialekttypen ist, nicht ob er der
  **richtige für diese Spalte** ist. Gemessen: die Typen des SQLite-Dialekts sind
  `[BLOB BLOB BLOB BLOB INTEGER]` – `seq BLOB` statt `seq INTEGER` ist darin enthalten und
  wird nicht gemeldet. Dieselbe Logik gegen eine mutierte Datei gefahren meldet
  **leere Fundliste**.

Der zweite Punkt ist am 30.10.2026 behoben (Weg D). Dieselbe mutierte Datei, dieselbe
Stelle, mit der Zuordnung statt mit der Menge gefragt:
`column ngc_blob.seq of type BLOB instead of INTEGER`. Vorher leer, nachher gefunden.
Der erste Punkt steht weiter offen: es ist eine Messung im Test, kein Riegel im Programm.

Was für eine echte Prüfung fehlt, ist also keine Technik, sondern eine **Angabe**:
`sqlstore.Columns` kennt die Namen der Spalten, aber nicht, welcher Platzhalter
(`{{key}}`, `{{int}}`, `{{doc}}`, `{{data}}`) zu welcher Spalte gehört.

**Die Zuordnung Spalte → Platzhalter stand bis zum 30.10.2026 nur im Text einer
SQL-Datei, also galt sie nicht.** Was nicht als Daten im Repo steht, kann kein Programm
fragen. Darum ist der erste Schritt keine Prüfung, sondern diese Angabe.

Seit dem 30.10.2026 steht sie als `sqlstore.ColumnType` in
`apps/server-db/classes/sqlstore/names.go` (Weg D, unten). Zwei Tests halten sie:
`TestColumnTypeCoversEveryColumn` in `sqlstore` die Form – je Spalte ein Eintrag und
sonst keiner –, und `TestColumnTypeIsWhatTheMigrationsWrite` in `migrate` den Inhalt:
es liest die Platzhalter aus den Anweisungen heraus, die die Migrationen wirklich
tragen, und vergleicht Eintrag für Eintrag. Gemessen: 55 Spalten aus 16 Anweisungen,
keine Abweichung. Kontrolle, die sterben muss: ein Eintrag von `{{int}}` auf `{{key}}`
verbogen – `ngc_blob.seq: the migration writes {{int}}, sqlstore.ColumnType says "key"`.
Kontrolle, die leben muss: eine lokale Variable umbenannt – grün.

## Was heute passiert, wenn ein Spaltentyp abweicht

Nachgestellt, nicht gefolgert. Vorgehen: echte Datenbank über `sqlite.Open` bzw.
`migrate.Apply` anlegen, einen Blob schreiben, dann `ngc_blob.seq` vom Zahlentyp auf den
Byte-Typ verbiegen, zurücklesen dass die Mutation gegriffen hat, neu starten und lesen.
Mutation und Rücklesen stehen im Messskript; ohne die Rücklesung bricht es ab.

**Beide Wege starten ohne eine Klage.** Das ist der Kern des Befunds.

### SQLite (`seq INTEGER` → `seq BLOB`)

| gemessen | Ergebnis |
|---|---|
| Start nach der Mutation | GESTARTET, keine Klage |
| `BlobGet` | `seq=7`, kein Fehler – sieht richtig aus |
| `BlobFrom(0)` bei den Folgen 1, 9, 10, 100 | `[9 10 100 1]` – **falsche Reihenfolge** |
| `BlobFrom(11)` | `[100 1]` – **enthält Folge 1** |
| `BlobFrom(100)` | `[100 1]` – **enthält Folge 1** |
| dieselbe Mutation mit Bytes, die keine Zahl sind | `Scan error … converting []uint8 to int64: invalid syntax` bei der **ersten Lesung**, nicht beim Start |

### MariaDB (`seq BIGINT` → `seq VARBINARY(255)`)

Gemessen gegen eine echte MariaDB 11.4 in einem Container (podman in WSL, Port 13306),
**nicht** gegen die Anlage. Migration und Laden sind NGC-Code
(`migrate.Apply`, `sqlstore.New`); nur die Verbindung ist von Hand und ohne TLS geöffnet –
die Verbindung ist nicht das Gemessene.

| gemessen | Ergebnis |
|---|---|
| `migrate.Apply` nach der Mutation | `gelaufen=0 err=<nil>` – kein Wort |
| `BlobGet` | `seq=7`, kein Fehler |
| `BlobFrom(0)` bei den Folgen 9, 10, 100 | `[10 100 9]` – **falsche Reihenfolge** |
| `BlobFrom(10)` / `BlobFrom(11)` / `BlobFrom(100)` | `[10 100]` / `[100]` / `[100]` – Filter **richtig** |
| Bytes, die keine Zahl sind | `Scan error … converting []uint8 to int64: invalid syntax` bei der Lesung |

### Gilt das für beide Wege gleich?

Nein, und der Unterschied ist wichtig:

- **Der Filter.** MariaDB rechnet einen Text beim Vergleich mit einer Zahl in eine Zahl um,
  der Filter bleibt richtig. SQLite tut das nicht: dort liegen nach der Mutation Zahlen
  **und** Texte in derselben Spalte, und SQLite ordnet jede Zahl vor jeden Text – deshalb
  kommt Folge 1 bei `BlobFrom(100)` mit.
- **Die Reihenfolge ist auf beiden Wegen falsch.** `ORDER BY seq` sortiert Byte für Byte.
- Die **Lücke** ist dieselbe: der Start sagt nichts, der Schaden kommt beim ersten Leser.

`BlobFrom` ist der Weg, über den Folgen nachgezogen werden. Eine falsche Reihenfolge und
ein zu großes Ergebnis sind dort kein Schönheitsfehler: sie wandern weiter, und es gibt
keinen Fehler, an dem jemand es merken könnte.

## Wo der Riegel steht, entscheidet, ob er hält

Das ist der wichtigste Befund dieser Arbeit, und er ist am 30.10.2026 an fünf Tabellen
gemessen:

> **Wo der Riegel in Go steht, hält er. Wo er in SQL steht, kippt er auf SQLite um.**

„Kippt um" und nicht „fällt aus" – der Unterschied ist genau der zwischen Stabilität und
Sicherheit. SQLite ordnet jeden Text hinter jede Zahl, also wird ein Vergleich nicht
falsch, sondern **gegenteilig**. Gemessen an einer Zeile, deren Wert 1000 ist:

| Vergleich | ungebogen (`integer`) | verbogen (`text`) | richtig |
|---|---|---|---|
| `expires > 5000` | 0 | **1** | 0 |
| `expires >= 5000` | 0 | **1** | 0 |
| `expires < 5000` | 1 | **0** | 1 |
| `expires <= 5000` | 1 | **0** | 1 |

Alle vier kippen. Damit entscheidet die **Richtung** des Vergleichs, wie schlimm es wird:

- Ein Riegel, der „erlaubt" als `> ` oder `>=` schreibt, **lässt alles durch**.
- Ein Riegel, der „frei" oder „abgelaufen" als `<` oder `<=` schreibt, **weist alles ab**
  oder räumt nichts – unangenehm, aber die sichere Richtung.

### Was das an den fünf Tabellen angerichtet hat

| Stelle | Riegel steht in | gemessen |
|---|---|---|
| `ngc_once.expires` (`onceUse`, `expires > ?`) | **SQL** | **SICHERHEITSBEFUND:** abgelaufener Einmalwert wird angenommen und gibt sein Geheimnis heraus (`data="geheim2"`, `err=nil`). MariaDB 11.8.6 weist korrekt ab. |
| `ngc_log.ts` (`LogQuery`, `ORDER BY ts DESC … LIMIT`) | SQL | **Prüflog verliert Einträge:** Blättern mit `Limit=1` liefert `[9]` nach einer Seite statt `[100 10 9]` nach drei. Beide Wege, kein Fehler. Auf SQLite geben die Folgeseiten gar nichts. |
| `ngc_blob.seq` (`blobFrom`, `seq >= ?`) | SQL | falsche Reihenfolge auf beiden Wegen, auf SQLite zusätzlich zu viele Zeilen |
| `ngc_user.locked` | **Go** (`coreapp/users.go:90`) | hält. Konto bleibt gesperrt, beide Wege |
| `ngc_session.expires` | **Go** (`login/live.go:114`) | hält. Sitzung bleibt abgelaufen, beide Wege. Nur `SessionPurge` räumt auf SQLite nicht – Stabilität |
| `ngc_lock.until` (`lockTake`, `until <= ?`) | SQL | hält – weil `<=` in die **sichere** Richtung kippt. Ein zweiter Halter bekommt das Schloss nicht (`ok=false`, beide Wege) |

`LockTake` prüft `until` in Go **gar nicht** ([once.go:130](../apps/server-db/classes/sqlstore/once.go)),
ebenso `takeLock` der Migrationen ([migrate.go:171](../apps/server-db/classes/migrate/migrate.go)).
Dass beide trotzdem halten, ist kein Verdienst des Codes, sondern die Richtung des
Vergleichs. Eine Umkehrung in die andere Richtung – etwa `until > ?` für „gehalten" – wäre
zwei Server, die gleichzeitig DDL in ein Schema schreiben.

**Breit gesucht**, wie die Leitsitzung verlangt hat: alle SQL-Anweisungen in `apps/` mit
einem Größenvergleich im `WHERE`, ohne `vendor/` und ohne Testdaten. Es sind sechs, sie
stehen alle in der Tafel oben. `ngc_once.expires` ist die einzige, die „erlaubt" als `>`
schreibt, und damit die einzige, bei der die Umkehrung einen Sicherheitsriegel öffnet.

### Was daraus behoben ist

Der Riegel in `OnceUse` ist auf dem Zweig `once-riegel` nachgezogen: nach dem UPDATE wird
der Ablauf **ein zweites Mal in Go** geprüft. Es ist ein zweiter Riegel, kein Ersatz – die
Entscheidung bleibt beim UPDATE.

**Eine Prüfung VOR dem UPDATE ist nicht möglich**, und das ist keine Bequemlichkeit:
`TestOnceUseDecidesWithItsWrite` liest den Quelltext und weist eine Leseprüfung vor dem
Schreiben ab, und `TestOnlyOneUseGetsTheValue` misst mit 32 Nebenläufern über 400 Runden,
warum – der Wert wird von dem Schreiben verbraucht, das ihn herausgibt. Ein Lesen davor
wäre ein Zeitfenster für zwei Aufrufer.

Das Prüflog ist **nicht** behoben: es gehört zu Weg A oder B, nicht zu einem einzelnen
Riegel.

## Vier Wege, und was jeder kostet

Rangfolge nach `Z:\GitHub\claude-md`: Sicherheit > Stabilität > Geschwindigkeit.

### Weg A – harter Riegel: abweichender Typ, Server startet nicht

Vor dem ersten Bedienen liest der Server die echten Spalten und Typen (`pragma_table_info`
bzw. `information_schema.COLUMNS`) und hält sie gegen eine feste Tabelle
Spalte → Platzhalter. Eine Abweichung ist ein Startfehler mit Namen der Spalte, erwartetem
und vorgefundenem Typ.

- **Fängt:** jede Typabweichung, jede fehlende und jede zusätzliche Spalte, jede fehlende
  Tabelle – vor dem ersten falschen Wert.
- **Kostet:** eine Abfrage je Tabelle beim Start (9 Tabellen), also einmalig und
  unmessbar neben dem Rest des Starts. Dazu die neue Zuordnung Spalte → Platzhalter, die
  mit jeder Migration mitgepflegt werden muss.
- **Macht kaputt:** Das ist die eigentliche Abwägung, und sie ist **nicht** ausgemacht.
  Ein Server, der bei Abweichung gar nicht startet, kann eine laufende Anlage nach einem
  Update **vollständig stilllegen** – und zwar auch dann, wenn die Datenbank in Ordnung
  ist und nur die Prüfung zu streng ist. Drei Wege dahin:
  - **Die Schreibweise.** MariaDB gibt Typen anders zurück, als man sie hingeschrieben
    hat. Das ist jetzt gemessen, und es ist kein kleiner Unterschied: von 55 Spalten
    kommen **20 anders geschrieben** zurück. Ein wortwörtlicher Vergleich des
    Dialekt-Textes mit `COLUMN_TYPE` würde eine gesunde Anlage an 20 Spalten anhalten.
  - Bei einem rollenden Update spielt ein Controller mit Rolle db die Migrationen ein, die
    APIs starten danach. Startet eine API, bevor die Migration durch ist, sieht sie ein
    altes Schema – mit Weg A verweigert sie den Dienst, statt zu warten.
  - In einer Galera-Gruppe, in der nicht alle Knoten gleich weit sind, verweigern alle
    APIs, die den hinterherhängenden Knoten erwischen.
  **Für sich genommen ist A also sicherer und gleichzeitig der einzige Weg, der eine
  Anlage ohne Datenfehler lahmlegen kann.**

#### Die Vergleichsregel für MariaDB, gemessen

Gemessen am 30.10.2026 gegen MariaDB 11.4 im Container, nach einer echten
`migrate.Apply`-Migration. Je Platzhalter, was hingeschrieben wird und was zurückkommt:

| Platzhalter | hingeschrieben | `DATA_TYPE` | `COLUMN_TYPE` |
|---|---|---|---|
| `{{key}}` | `VARBINARY(255)` | `varbinary` | `varbinary(255)` |
| `{{text}}` | `VARBINARY(1024)` | `varbinary` | `varbinary(1024)` |
| `{{doc}}` | `MEDIUMBLOB` | `mediumblob` | `mediumblob` |
| `{{data}}` | `LONGBLOB` | `longblob` | `longblob` |
| `{{int}}` | `BIGINT` | `bigint` | **`bigint(20)`** |

Daraus folgt die Regel, die Weg A brauchen würde, und sie ist nicht die naheliegende:

- **`COLUMN_TYPE` nie wortwörtlich vergleichen.** Es ist klein geschrieben und hängt bei
  `BIGINT` ein `(20)` an. Das sind die 20 Spalten von oben.
- **`DATA_TYPE` vergleichen**, klein geschrieben.
- **Die Länge nur bei `varbinary` dazunehmen.** Die BLOB-Sorten tragen eine Länge, die aus
  der Sorte selbst folgt (`mediumblob` → 16 777 215, `longblob` → 4 294 967 295) und die
  nirgends hingeschrieben steht; sie mitzuvergleichen hält weitere 9 Spalten an.

Mit dieser Regel: **55 Spalten geprüft, 0 Abweichungen.** Gegenprobe im selben Lauf:
`ngc_blob.seq` auf `VARBINARY(255)` verbogen → `erwartet bigint, gefunden varbinary`,
fällt auf. Damit ist die Messung, auf die Weg A gewartet hat, erbracht; offen bleibt an A
nur noch das rollende Update, und das ist das stärkere der beiden Argumente.

**Nachgefahren gegen die Fassung, die die Anlage wirklich fährt.** Die Tafel oben stand
zuerst auf MariaDB 11.4; die Anlage fährt `11.8.6-MariaDB-0+deb13u1 from Debian`. Am
30.10.2026 gegen genau diese Fassung nachgemessen – aus `debian:trixie` plus
`apt-get install mariadb-server`, nicht vom Hersteller, weil die Anlage das Debian-Paket
fährt; die Fassungszeichenkette stimmt wörtlich mit der von db-1 überein. **Ergebnis
unverändert:** `bigint(20)` schreibt auch 11.8.6, 20 von 55 Spalten kommen anders
geschrieben zurück, und die enge Regel hält mit 0 Abweichungen. Der Vorbehalt
„`bigint(20)` ist eine Eigenheit der Fassung" ist damit geprüft und ausgeräumt.

### Und die zweite Linie, die keine MariaDB hat

Die Tafel oben deckt nur die MariaDB-Seite. `ngc-ein` fährt keine MariaDB, sondern eine
verschlüsselte SQLite – und **auf SQLite ist der Schaden in jeder einzelnen Messung
schlimmer**: der Filter kippt dort zusätzlich zur Reihenfolge, das Prüflog gibt leere
Folgeseiten, und der Sicherheitsbefund an `ngc_once` tritt überhaupt nur dort auf. Eine
Prüfung, die nur die MariaDB-Seite deckt, lässt also die gefährlichere Seite ungeprüft –
und es ist die Maschine, auf der der Browser eines einzelnen Benutzers landet.

### Weg B – lauter Befund, Server startet trotzdem

Dieselbe Prüfung, aber eine Abweichung ist ein Eintrag im Log mit `ERROR`, ein Punkt im
Zustandsbericht („Schema weicht ab") und sonst nichts.

**Vorgabe für die Meldung:** in das Log gehen Tabellenname, Spaltenname, erwarteter und
gefundener Typ – und **keine Werte aus der Datenbank**, auch nicht gekürzt. Eine
Typabweichung fällt oft an einem Wert auf, und ein Wert aus `ngc_session` oder `ngc_user`
ist ein Geheimnis. Geheimnisse nie ins Log (Regel aus `CLAUDE.md`).

- **Fängt:** denselben Umfang wie A – aber nur für jemanden, der hinsieht.
- **Kostet:** dasselbe wie A.
- **Macht kaputt:** nichts. Genau das ist der Einwand: der falsche Wert wird weiter
  geschrieben und weitergegeben, während die Meldung im Log steht. Bei `BlobFrom` heißt
  das, dass eine falsche Reihenfolge weiterwandert.

### Weg C – schreibende Wege zu, lesende offen

Prüfung wie A. Bei Abweichung startet der Server, antwortet auf Lesen, und weist jedes
Schreiben ab, das eine betroffene Tabelle berührt.

- **Fängt:** die Ausbreitung. Falsches kommt nicht neu hinein, Vorhandenes bleibt lesbar.
- **Kostet:** dasselbe wie A, dazu eine Zuordnung Tabelle → Weg, damit ein Schreibweg
  weiß, ob er betroffen ist.
- **Macht kaputt:** weniger als A, aber nicht nichts. Eine Anlage, die nicht mehr
  schreiben kann, ist für ihre Benutzer fast so tot wie eine, die nicht startet – nur
  verständlicher. Und die Zuordnung ist neue Technik, die selbst falsch sein kann.

### Weg D – nur die vorhandene Prüfung reparieren

`unlistedInDB` bekommt die Zuordnung Spalte → Platzhalter und prüft den **richtigen** Typ
statt irgendeinen. Sie bleibt ein Test.

- **Fängt:** eine Migration, die falsche Typen schreibt – vor der Veröffentlichung.
- **Kostet:** fast nichts, kein Startcode, kein neues Verhalten im Betrieb.
- **Macht kaputt:** nichts – und fängt im Feld nichts. Der gemessene Fall (eine Datenbank,
  deren Spalte sich nachträglich unterscheidet) kommt in keinem Test vor.

## Stand und Vorschlag

**D ist gebaut** (30.10.2026): `sqlstore.ColumnType`, die zwei Tests darauf, und die
reparierte Typprüfung in `unlistedInDB`. Damit ist die Angabe da, auf der A, B und C alle
stehen, und die Lücke in der vorhandenen Prüfung ist zu.

**Der Sicherheitsriegel in `OnceUse` ist nachgezogen** (Zweig `once-riegel`), unabhängig
von A bis D: er behebt eine Folge der Lücke, nicht die Lücke. Der Grund, warum ein
Spaltentyp überhaupt abweichen kann, bleibt A oder B.

**Als nächstes B.** Er bringt den Befund ins Feld, ohne eine Anlage anhalten zu können.
Nach der Rangfolge ist das ein Kompromiss zulasten der Sicherheit, aber er ist bewusst
gewählt: eine Prüfung, die beim ersten Lauf eine gesunde Anlage stilllegt, kostet mehr
Vertrauen, als sie Sicherheit bringt, und wird dann abgeschaltet.

**A danach.** Die Messung, auf die A gewartet hat, ist erbracht (Vergleichsregel oben,
55 Spalten, 0 Abweichungen). Offen bleibt an A nur, was eine API tun soll, die vor der
Migration startet – warten statt verweigern –, und die Frage einer Sollbruchstelle, damit
eine falsche Prüfung keine gesunde Anlage dauerhaft unten hält. Beides ist nicht
entschieden.

**C bleibt in der Hinterhand**, für den Fall, dass eine Abweichung im Feld wirklich
auftritt und A noch nicht reif ist.

## Was nicht gemessen ist

- Dieselbe Messung gegen die Anlage (Galera, sechs VMs). Eine Spalte an der laufenden
  Anlage zu verbiegen ist ein Eingriff mit eigener Ansage. Die Frage, die der Container
  **nicht** beantwortet, ist eine andere als die nach den Typen: ob in einer Galera-Gruppe
  die Knoten überhaupt verschiedene Schemata tragen können, also ob ein Knoten mit altem
  Schema erreichbar bleibt. Die ist am Zustand der Knoten ablesbar, **ohne** Eingriff, und
  sie gehört zu Weg A. Hier ist an der Anlage nichts angefasst worden.
- Das Prüflog ist gemessen, aber nicht behoben – es gehört zu Weg A oder B.
- Von den vier Zeittabellen sind je eine Spalte durchgefahren (`expires`, `locked`, `ts`,
  `until`), nicht alle. `ngc_app`, `ngc_pending` und die Dokumentspalten sind offen.
- MySQL 8 lässt die Breite bei `BIGINT` weg. Für NGC ist das heute ohne Belang (die Anlage
  fährt MariaDB 11.8.6, nachgemessen), aber eine Prüfung nach Weg A, die auf `bigint`
  besteht, wäre dort nicht automatisch richtig.
