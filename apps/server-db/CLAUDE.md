# Datenbank

Dein Ordner: `apps/server-db/`, dazu `scripts/ngc-db.sh` im ulh-Repo (`shared/ulh.md`).
Verträge: V4, V6.

Aufbau: kein eigenes Programm. `classes/` enthält die Umsetzungen von `dbapi`, je ein Go-Paket
als Unterordner (`classes/mariadb/`, `classes/sqlite/`), damit jedes Programm nur den Treiber
bekommt, den es braucht. `sql/` enthält die nummerierten Migrationen.

`classes/migrate/` kennt SQL und keinen Treiber; `classes/migraterun/` ist der Läufer darüber,
den `ngc-api migrate` ruft (`Run`, `Needed`). Der Läufer ist ein eigenes Paket, weil
`classes/sqlite/` seinerseits `migrate` importiert — ein Import von `sqlite` innerhalb von
`migrate` ist ein Ring, gemessen: „import cycle not allowed".

## Aufgabe

- `dbapi.Store` für MariaDB und für SQLite umsetzen; die SQLite-Umsetzung dient zugleich dem
  Controller als `dbapi.Local`. Beide müssen `dbtest.Run` bestehen, und zwar mit denselben Tests.
- MariaDB-Galera-Cluster mit mindestens drei Knoten einrichten: TLS zwischen den Knoten,
  eingebaute Verschlüsselung (AES-256), Konto `ngc` nur mit Lesen und Schreiben, Konto
  `ngc_migrate` für das Schema.
- Knotenadressen sind `<host>:<port>` nach `trust.ValidHostPort` (`shared/README.md`, Regel
  „Adressen“), in `db.nodes` und `migrate.nodes` ebenso wie in `nodes` von `ngc-db`; die Länge
  begrenzt `config.MaxField`. Die Skripte prüfen den Host mit `host.sh`
  (`shared/testdata/ulh/host.sh`: DNS-Name in Kleinbuchstaben oder IPv4, kein IPv6).
- TLS zu MariaDB: eine CA je Installation. Das Zertifikat eines Knotens nennt genau diesen Host
  (bei IPv4 als IP-Adresse); MariaDB verlangt TLS 1.3. Das Zertifikat der CA (PEM,
  `config.DB.CA`, höchstens `config.MaxCA`) kommt nach `db_ca` (`ngc-api`) und `migrate_ca`
  (`ngc-controller`); API und Controller prüfen damit jeden Knoten (`config.DB.TLSConfig`), nie eine
  Verbindung ohne Prüfung. Auch die Replikation zwischen den Galera-Knoten prüft gegen dieselbe CA
  der Installation. Der private Schlüssel der CA kommt nie in answer.yaml, eine Konfiguration oder
  das Repo. Wie er verwahrt wird und wie weitere Knoten ihre Zertifikate bekommen, legt dein Plan
  fest; der Koordinator trägt es in `shared/ulh.md` ein.
- `dbapi` hat neue Arten bekommen (`dbapi.BlobApproval`, `dbapi.BlobJoin`, `dbapi.OnceJoin`), und
  `dbapi.SettingUpdate` hält je Ziel nur `core.UpdateSetting`: alles Blobs, Einmalwerte und
  Einstellungen, also keine Änderung am Schema.
- SQLite verschlüsselt (AES-256-XTS, Kandidat `github.com/ncruces/go-sqlite3` mit `vfs/xts`) für den
  Controller und für „alles auf einem Pi".
- Nummerierte Migrationen, die auch mit der vorigen API-Version laufen. Sie gehören zur
  API-Version, stecken in der Release-Datei `ngc-api` und werden vor der neuen API eingespielt, von
  genau einem Controller mit Rolle `db` mit dem Zugang `migrate` aus seiner `controller.json`.
- Lastverteilung über die Knoten, Ausweichen bei Ausfall, Wiederholung, wenn Galera eine
  Transaktion wegen eines gleichzeitigen Schreibzugriffs abweist.
- Portables SQL: nur was MariaDB, SQLite, MySQL, MSSQL und Oracle verstehen. IDs erzeugt die
  Anwendung, Zeitstempel sind Zahlen, keine datenbankeigenen Funktionen.
- Protokoll (`LogAdd`, `LogQuery`): neueste zuerst, nach Zeit und ID absteigend, IDs Byte für Byte
  verglichen. Eine Seite enthält nur Einträge mit (Zeit, ID) < (`BeforeTime`, `BeforeID`), ohne
  `BeforeID` die mit Zeit < `BeforeTime`, ohne `BeforeTime` ab dem neuesten. Die ID setzt die API:
  32 Zeichen Hex aus Kleinbuchstaben (`core.ValidLogID`), damit jede Sortierung der Datenbank sie
  wie Go ordnet. Fehlt sie oder ist sie ungültig, gibt `LogAdd` `ErrInvalid`, dieselbe ID zweimal
  `ErrUsed`.
- `OnceCreate` und `LockTake` sind atomar, auch wenn mehrere APIs gleichzeitig auf verschiedenen
  Galera-Knoten schreiben: von gleichzeitigen Versuchen gelingt genau einer.

## Wo die zwei Speicher verschieden antworten

`dbapi` hat zwei Umsetzungen: `memstore` in `shared/` (die Referenz, gegen die die Tests der
anderen Teile laufen) und `sqlstore` hier. Beide bestehen `dbtest.Run`, und doch antworten sie an
den folgenden Stellen verschieden. Diese Liste ist die, auf die der Kommentar an
`TestAnExpiredAndSpentValueIsNotFound` in `classes/sqlstore/once_test.go` zeigt; sie stand dort als
Verweis, bevor es sie gab.

Keine dieser Stellen hält `dbtest.Run`: keine Gruppe stellt eine überlange Zeichenkette, keine ein
`Limit` über 500, und keine fragt, ob eine leere Seite `nil` ist. Wer eine davon schließen will,
schließt sie im Vertrag (`dbtest`), nicht je Speicher — sonst wandert dieselbe Regel in zwei
Fassungen.

Gemessen am 01.10.2026 mit einer Wegwerf-Sonde, die beide Speicher in einem Lauf fragt, je Fall
eine eigene Kennung:

| Frage | `sqlstore` | `memstore` |
|---|---|---|
| `LogAdd` mit `User` über `MaxKey` | `ErrInvalid` | `nil` — Zeile geschrieben |
| `LogAdd` mit `Kind` über `MaxKey` | `ErrInvalid` | `nil` — Zeile geschrieben |
| `LogAdd` mit `App` über `MaxKey` | `ErrInvalid` | `nil` — Zeile geschrieben |
| `LogAdd` mit `Action` über `MaxKey` | `ErrInvalid` | `nil` — Zeile geschrieben |
| `LogAdd` mit `Node` über `MaxKey` | `ErrInvalid` | `nil` — Zeile geschrieben |
| `LogAdd` mit `Result` über `MaxKey` | `ErrInvalid` | `nil` — Zeile geschrieben |
| `LogAdd` mit `Detail` über `MaxText` | `ErrInvalid` | `nil` — Zeile geschrieben |
| `LogQuery` mit `User` über `MaxKey` | `ErrInvalid` | `nil` |
| `LogQuery` mit `Limit` über 500 | `ErrInvalid` | `nil` |
| `LogQuery` auf leerer Tabelle | `nil`-Scheibe, Länge 0 | nicht-`nil`, Länge 0 |

Von den sieben `LogAdd`-Zeilen hat `memstore` alle sieben wirklich geschrieben (nachgezählt: sieben
Zeilen danach, in `sqlstore` keine).

Warum die ersten neun so sind: `memstore` hat keine Spalten und deshalb keine Längen; `sqlstore`
muss jede Länge in Go prüfen, weil MariaDB kürzt oder abbricht und SQLite alles nimmt. Die Grenze
bei 500 ist eine Entscheidung des Koordinators aus Fixrunde 1 von Aufgabe 7: abweisen, nicht kappen.

Die zehnte ist der Art nach älter und breiter: `sqlstore` baut seine Listen mit `var out []T`,
`memstore` mit `make([]T, 0, …)`. Das betrifft auch `UserList`, `AppList` und `PendingList`; über
`json.Marshal` wird daraus `null` gegen `[]` in der Antwort. `dbtest` prüft überall nur `len(…)`,
nie `== nil`.

Dazu die eine Stelle, an der der Vertrag selbst offenlässt, was gilt, und `sqlstore` sich nach
`memstore` richtet: ein Einmalwert, der verbraucht **und** abgelaufen ist, kann `ErrNotFound` oder
`ErrUsed` geben. Die Reihenfolge ist aus `memstore` übernommen, und zwar durch Fahren, nicht durch
Lesen — festgehalten in `TestAnExpiredAndSpentValueIsNotFound`.

## Tests

- `dbtest.Run` gegen MariaDB (Cluster aus drei Knoten) und gegen SQLite. Darin sind Wettläufe:
  256 Runden mit je 16 gleichzeitigen `OnceCreate` bzw. `LockTake`, jede auf einem neuen Wert oder
  einer neuen Sperre. Sie müssen auf Linux auch mit `go test -race` bestehen (SQLite braucht dafür
  eine Behandlung für „busy“). Verlässlich sind sie nur mit `-race`: Ohne kann eine Umsetzung, die
  Prüfen und Schreiben in zwei Schritten macht, sie bestehen.
- Migrationen vorwärts und mit der vorigen API-Version.
- Ausfall: ein Knoten wird im laufenden Betrieb abgeschaltet, die Tests laufen weiter.
- Kein Test darf ein Schema von Hand anlegen: immer über die Migrationen.

## Fertig, wenn

Beide Umsetzungen bestehen dieselben Tests, der Cluster verkraftet den Ausfall eines Knotens, und
eine Migration läuft, während noch die vorige API-Version arbeitet.
