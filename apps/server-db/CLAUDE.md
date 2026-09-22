# Datenbank

Dein Ordner: `apps/server-db/`, dazu `scripts/ngc-db.sh` im ulh-Repo (`shared/ulh.md`).
Verträge: V4, V6.

Aufbau: kein eigenes Programm. `classes/` enthält die Umsetzungen von `dbapi`, je ein Go-Paket
als Unterordner (`classes/mariadb/`, `classes/sqlite/`), damit jedes Programm nur den Treiber
bekommt, den es braucht. `sql/` enthält die nummerierten Migrationen.

## Aufgabe

- `dbapi.Store` für MariaDB und für SQLite umsetzen; die SQLite-Umsetzung dient zugleich dem
  Controller als `dbapi.Local`. Beide müssen `dbtest.Run` bestehen, und zwar mit denselben Tests.
- MariaDB-Galera-Cluster mit mindestens drei Knoten einrichten: TLS zwischen den Knoten,
  eingebaute Verschlüsselung (AES-256), Konto `ngc` nur mit Lesen und Schreiben, Konto
  `ngc_migrate` für das Schema.
- TLS zu MariaDB: eine CA je Installation. Das Zertifikat eines Knotens nennt den Host seines
  Eintrags in `db.nodes` (`host:port`); MariaDB verlangt TLS 1.3. Das Zertifikat der CA (PEM,
  `config.DB.CA`, höchstens `config.MaxCA`) kommt nach `db_ca` (`ngc-api`) und `migrate_ca`
  (`ngc-controller`); API und Controller prüfen damit jeden Knoten (`config.DB.TLSConfig`), nie eine
  Verbindung ohne Prüfung.
  Der private Schlüssel der CA kommt nie in answer.yaml, eine Konfiguration oder das Repo. Wie er
  verwahrt wird und wie weitere Knoten ihre Zertifikate bekommen, legt dein Plan fest; der
  Koordinator trägt es in `shared/ulh.md` ein.
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
