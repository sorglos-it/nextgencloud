# Datenbank

Deine Ordner: `db/`, dazu `scripts/ngc-db.sh` im ulh-Repo (`contracts/ulh.md`). Verträge: V4, V6.

## Aufgabe

- `dbapi.Store` für MariaDB und `dbapi.Local` für SQLite umsetzen. Beide müssen `dbtest.Run`
  bestehen, und zwar mit denselben Tests.
- MariaDB-Galera-Cluster mit mindestens drei Knoten einrichten: TLS zwischen den Knoten,
  eingebaute Verschlüsselung (AES-256), Konto `ngc` nur mit Lesen und Schreiben, Konto
  `ngc_migrate` für das Schema.
- SQLite verschlüsselt (AES-256-XTS, Kandidat `github.com/ncruces/go-sqlite3` mit `vfs/xts`) für den
  Controller und für „alles auf einem Pi".
- Nummerierte Migrationen, die auch mit der vorigen API-Version laufen. Sie gehören zur
  API-Version und werden vom Controller vor der neuen API eingespielt.
- Lastverteilung über die Knoten, Ausweichen bei Ausfall, Wiederholung, wenn Galera eine
  Transaktion wegen eines gleichzeitigen Schreibzugriffs abweist.
- Portables SQL: nur was MariaDB, SQLite, MySQL, MSSQL und Oracle verstehen. IDs erzeugt die
  Anwendung, Zeitstempel sind Zahlen, keine datenbankeigenen Funktionen.

## Tests

- `dbtest.Run` gegen MariaDB (Cluster aus drei Knoten) und gegen SQLite.
- Migrationen vorwärts und mit der vorigen API-Version.
- Ausfall: ein Knoten wird im laufenden Betrieb abgeschaltet, die Tests laufen weiter.
- Kein Test darf ein Schema von Hand anlegen: immer über die Migrationen.

## Fertig, wenn

Beide Umsetzungen bestehen dieselben Tests, der Cluster verkraftet den Ausfall eines Knotens, und
eine Migration läuft, während noch die vorige API-Version arbeitet.
