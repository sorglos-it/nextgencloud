# Datenbank

Deine Ordner: `db/`, dazu `scripts/ngc-db.sh` im ulh-Repo (`contracts/ulh.md`). Verträge: V4, V6.

## Aufgabe

- `dbapi.Store` für MariaDB und für SQLite umsetzen; die SQLite-Umsetzung dient zugleich dem
  Controller als `dbapi.Local`. Beide müssen `dbtest.Run` bestehen, und zwar mit denselben Tests.
- MariaDB-Galera-Cluster mit mindestens drei Knoten einrichten: TLS zwischen den Knoten,
  eingebaute Verschlüsselung (AES-256), Konto `ngc` nur mit Lesen und Schreiben, Konto
  `ngc_migrate` für das Schema.
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

## Tests

- `dbtest.Run` gegen MariaDB (Cluster aus drei Knoten) und gegen SQLite.
- Migrationen vorwärts und mit der vorigen API-Version.
- Ausfall: ein Knoten wird im laufenden Betrieb abgeschaltet, die Tests laufen weiter.
- Kein Test darf ein Schema von Hand anlegen: immer über die Migrationen.

## Fertig, wenn

Beide Umsetzungen bestehen dieselben Tests, der Cluster verkraftet den Ausfall eines Knotens, und
eine Migration läuft, während noch die vorige API-Version arbeitet.
