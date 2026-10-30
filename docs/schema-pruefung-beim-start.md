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
  Anlage zu verbiegen ist ein Eingriff mit eigener Ansage, und es ist offen, ob die
  Messung überhaupt etwas beantwortet, das der Container nicht beantwortet. Hier ist an
  der Anlage nichts angefasst worden.
- Was ein abweichender Typ in `ngc_session`, `ngc_user` und `ngc_log` anrichtet. Gemessen
  ist `ngc_blob`, weil dort die Folge über `BlobFrom` weiterwandert; die anderen Tabellen
  sind nicht durchgefahren.
- Andere MariaDB-Fassungen als 11.4. Die Schreibweise `bigint(20)` ist eine Eigenheit der
  Fassung, nicht des Standards – MySQL 8 lässt die Breite weg. Weg A gehört gegen die
  Fassung gemessen, die die Anlage wirklich fährt.
