# Der Ablauf der zwei Schlösser: gemessen und zwei Wege nach vorn

Gemessen am 03.11.2026, Zweig `db-schloss-until`, Grundstand `origin/main` = `0471b02d`.
Arbeitsbaum `D:\claude\working\nextgencloud-db-schloss-until` (lokale Platte), 0 fehlende Dateien.
Die Läufe selbst liefen in WSL (Debian 13 trixie, ext4), weil der SQLite-Teil von NGC unter
Windows nicht zu öffnen ist: `shared/config` verlangt für `Controller.DB` das Präfix `/`
(`config.go:656`) und `sqlite.validPath` verlangt zusätzlich einen absoluten Pfad — kein
Windows-Pfad erfüllt beides. Die Zahlen gelten also für ext4 in WSL, nicht für `C:` und nicht
für `Z:`.
MariaDB lief in einem **eigenen Container** (podman, `debian:13-slim`, Paket
`1:11.8.6-0+deb13u1`, Server meldet `11.8.6-MariaDB-0+deb13u1 from Debian`) — dieselbe Fassung
wie die Anlage, aber **nicht** die Anlage. TLS 1.3 erzwungen; Gegenprobe: eine Verbindung ohne
TLS wird mit `ERROR 3159` abgewiesen, mit TLS antwortet sie.
Last vor dem Lauf `load average: 1.56`, danach `2.21`, 128 Kerne.

## Worum es geht

Zwei Stellen nehmen ein Schloss mit einem Ablaufzeitpunkt:

| Stelle | Zeile | SQL |
|---|---|---|
| `sqlstore.LockTake` | `apps/server-db/classes/sqlstore/once.go:130`, SQL in `:21` | `UPDATE ngc_lock SET owner = ?, until = ? WHERE name = ? AND (owner = ? OR until <= ?)` |
| `migrate.take` | `apps/server-db/classes/migrate/migrate.go:389`, SQL in `:171` | `UPDATE ngc_lock SET owner = ?, until = ? WHERE name = ? AND until <= ?` |

Beide schreiben den Ablauf als **„frei" = `until <= ?`**. Go sieht den gespeicherten Wert dabei
nicht an: `LockTake` prüft nur das Verhältnis seiner **Argumente** (`until <= now` →
`ErrInvalid`, `once.go:131`), `take` prüft gar nichts. Der Riegel steht also allein im
SQL-Vergleich — genau das, was `Z:\GitHub\claude-md\code.md` seit dem 30.10.2026 verbietet.

## Was gemessen wurde

### Die Operator-Tafel

Eine Spalte, Wert 1000 bzw. 999, verglichen mit dem Platzhalterwert 5000. „wert=zahl" heißt, der
Wert ging als `int64` hinein, „wert=text" als Zeichenkette — beides durch einen Platzhalter, so
wie der Code schreibt.

**SQLite** (die Fassung aus `vendor/`, über den verschlüsselnden VFS):

| Spaltentyp | Wert | Zahl | `>5000` | `>=5000` | `<5000` | `<=5000` | richtig? |
|---|---|---|---|---|---|---|---|
| INTEGER | zahl | 1000 | 0 | 0 | 1 | 1 | ja |
| INTEGER | text | 1000 | 0 | 0 | 1 | 1 | ja |
| INTEGER | zahl | 999 | 0 | 0 | 1 | 1 | ja |
| INTEGER | text | 999 | 0 | 0 | 1 | 1 | ja |
| TEXT | zahl | 1000 | 0 | 0 | 1 | 1 | ja |
| TEXT | text | 1000 | 0 | 0 | 1 | 1 | ja |
| TEXT | zahl | **999** | **1** | **1** | **0** | **0** | **nein** |
| TEXT | text | **999** | **1** | **1** | **0** | **0** | **nein** |
| BLOB | zahl | 1000 | 0 | 0 | 1 | 1 | ja |
| BLOB | text | **1000** | **1** | **1** | **0** | **0** | **nein** |
| BLOB | zahl | 999 | 0 | 0 | 1 | 1 | ja |
| BLOB | text | **999** | **1** | **1** | **0** | **0** | **nein** |

**MariaDB 11.8.6**: alle zwölf Zeilen richtig, auch `VARCHAR(64)` und `VARBINARY(64)`, auch mit
Textwert. MariaDB rechnet zum Vergleich um.

**Das berichtigt die Tafel aus der Übergabe.** Dort stand, auf SQLite kippe der Vergleich,
„wenn der Spaltentyp abweicht". Gemessen kippt er nicht am abweichenden Typ allein, sondern an
**zwei verschiedenen Ursachen**:

- **TEXT-Spalte:** SQLite wandelt den Platzhalterwert in Text und vergleicht buchstabenweise.
  Das stimmt nur, solange beide Zahlen **gleich viele Ziffern** haben. Bei 1000 gegen 5000 geht
  es gut aus, bei 999 gegen 5000 kippt es.
- **BLOB-Spalte:** SQLite wandelt nichts um. Steht in der Zeile ein **Textwert**, ordnet SQLite
  ihn hinter jede Zahl, und der Vergleich kippt bei jeder Ziffernlänge.

Für NGC ist die zweite Ursache die nähere: in SQLite sind `name` und `owner` von `ngc_lock`
schon `BLOB` (`{{key}}` und `{{text}}` heißen dort beide `BLOB`, `dialect.go:116`). Eine
Migration, die `until {{text}}` statt `until {{int}}` schreibt, ist **ein Wort** entfernt. Die
erste Ursache ist für Zeitstempel dagegen heute harmlos: Unix-Sekunden haben bis zum Jahr 2286
alle zehn Ziffern. Das ist ein Zufall der Zahlen, kein Riegel.

### Halten die zwei Schlösser?

Gefragt wurde je Lage dasselbe: Eine Zeile liegt da, ihr Ablauf ist **noch nicht** vorbei —
bekommt ein zweiter Halter das Schloss? Und als Kontrolle: nach dem Ablauf **muss** er es
bekommen, sonst wird nicht der Ablauf gemessen, sondern irgendetwas anderes.

| Weg | Spalte | Wert | zweiter Halter gleichzeitig | nach Ablauf |
|---|---|---|---|---|
| SQLite | INTEGER | zahl / text | nein | ja |
| SQLite | TEXT | zahl / text | nein | ja |
| SQLite | BLOB | zahl | nein | ja |
| SQLite | BLOB | **text** | nein | **nein** |
| MariaDB | BIGINT / VARCHAR / VARBINARY | zahl / text | nein | ja |

Dasselbe Bild für `migrate.take` über `migrate.Apply`: in jeder Lage wurde der zweite Lauf
abgewiesen; nur in der SQLite-Lage BLOB+Textwert wurde auch das **abgelaufene** Schloss nicht
mehr übernommen.

**Antwort auf die Frage des Auftrags: Das Schloss hält. In keiner Lage bekamen es zwei.**
Der Schaden der verbogenen Spalte ist nicht Sicherheit, sondern **Verfügbarkeit**: das Schloss
klemmt dauerhaft. Auf SQLite heißt das, dass Migrationen und Rollouts nach 900 bzw. 600 Sekunden
nicht wieder anlaufen, sondern für immer „ein anderer Server migriert" melden. Unangenehm, aber
die sichere Richtung — und es ist Glück, nicht Absicht.

### Der sterbende Mutant

Mutation: in beiden Dateien `until <= ?` → `until > ?` („gehalten" statt „frei"), eingespielt mit
`go build -overlay`, keine Datei im Arbeitsbaum verbogen. Vorrichtungsprüfung im Skript: je Datei
genau ein Treffer vorher, die alte Zeile nachher nicht mehr vorhanden, Mutant übersetzt.

| Lauf | Spalte | zweiter Halter gleichzeitig | `migrate` weist ab |
|---|---|---|---|
| Grundstand, SQLite | INTEGER | nein | ja |
| **Mutant, SQLite** | **INTEGER** | **ja** | **nein** |
| Grundstand, MariaDB | BIGINT | nein | ja |
| **Mutant, MariaDB** | **BIGINT** | **ja** | **nein** |

**Der Mutant stirbt auf beiden Wegen, in der Lage `ganz` — ohne jede Spaltenmutation.** Der
Befund hängt damit an einem Ding, nicht an zwei. Mit dem Mutanten schreiben zwei Server
gleichzeitig DDL in dasselbe Schema; `migrate.Apply` eines zweiten Laufs läuft neben einem
haltenden durch.

Lebende Gegenprobe: eine umbenannte lokale Variable (`took` → `genommen` in `once.go`) übersetzt
und liefert in allen zwölf Zeilen genau die Zahlen des Grundstands. Ohne diese Kontrolle wäre
nicht belegt, dass die Überlagerung überhaupt etwas bewirkt.

### Was den Befund widerlegen könnte

Gesucht wurde nach einer Stelle, die das Schloss zusätzlich hält.

- **`PRIMARY KEY (name)` auf `ngc_lock`** (`migrate.go:157`) hält, dass es je Namen **eine Zeile**
  gibt — darum scheitert der `INSERT` des Verlierers und beide Stellen lesen das als „ein anderer
  hält es". Das hält „genau einer schreibt die Zeile", **nicht** „der Ablauf wird geachtet". Im
  Mutantenlauf hat es nichts gehalten.
- **`migrate.hold`** (`migrate.go:308`) liest `until` sehr wohl in Go und vergleicht `until <= now`
  → `errLockLost`. **Das berichtigt die Übergabe zum zweiten Mal:** es ist nicht so, dass das
  Feld in `migrate` nirgends in Go angesehen wird. Nur hilft es hier nicht: `hold` prüft die
  **eigene** Zeile nach dem Nehmen, und ein Lauf, der das Schloss zu Unrecht genommen hat, hat
  `owner` und `until` längst auf seine eigenen Werte geschrieben. Gemessen: im Mutantenlauf
  passierte `hold` anstandslos.
- **Die Transaktion** um `LockTake` (`s.write`) macht `UPDATE` und `INSERT` atomar, prüft aber
  keinen Ablauf.
- **Die Aufrufer** (`state.go:128`, `trust.go:482`, `rollout.go:104` und `:172`) prüfen keinen
  Ablauf nach. `trust.AddVersion` nimmt zusätzlich `s.mu` — das trennt zwei Aufrufer **innerhalb
  eines** API-Prozesses, nicht zwei Server.
- **`freeIn`** (`migrate.go:276`) liest `until` und misst den Wert sogar, bevor es ihn in einen
  Satz schreibt. Es entscheidet aber nichts, es formuliert nur die Meldung.

Nichts davon widerlegt den Befund. Was er berichtigt, steht oben.

### Die schon behobene Schwester steht noch in `main`

`onceUse` (`once.go:16`) lautet weiter
`UPDATE ngc_once SET used = ? WHERE kind = ? AND token = ? AND used = ? AND expires > ?` — der
Ablauf-Riegel in der **gefährlichen** Richtung. Nach derselben Tafel gibt ein Textwert in einer
BLOB-Spalte dort die Antwort „noch gültig" für einen längst abgelaufenen Einmalwert. Daran
arbeitet ein anderer Strang; hier steht es nur, damit niemand glaubt, in `main` sei es erledigt.

## Zwei Wege nach vorn

Rangfolge: **Sicherheit > Stabilität > Geschwindigkeit.**

### Weg A — den Ablauf zusätzlich in Go prüfen

Nach dem `UPDATE`, das das Schloss nimmt, wird die genommene Zeile gelesen und ihr `until` in Go
gegen `now` gehalten. Der Riegel im SQL bleibt; es kommt einer **dazu**.

- Für `migrate` ist der Weg kurz: `hold` tut das schon, es muss nur **vor** dem ersten DDL und
  **gegen die fremde** Zeile gefragt werden — also im `take`-Pfad, bevor `n > 0` als „genommen"
  gilt: die alte Zeile mitlesen und ihren Ablauf prüfen (`UPDATE … RETURNING` kennt SQLite,
  MariaDB nicht; darum ein `SELECT` in derselben Transaktion vor dem `UPDATE`).
- Für `LockTake` kommt die Prüfung in dieselbe Transaktion, die heute schon `UPDATE` und `INSERT`
  umspannt.
- **Kosten:** eine Abfrage mehr je Nehmen. Das Schloss wird selten genommen (Rollout, Vertrauen,
  Installation, Migration) — die Geschwindigkeit ist hier der unterste Rang und wird nicht
  berührt.
- **Schaden, wenn es schiefgeht:** siehe unten, der Abschnitt über den abweisenden Riegel.
- **Was es nicht kann:** es behebt den Typ nicht. Eine verbogene Spalte klemmt das Schloss weiter
  (Verfügbarkeit), nur kippt es nie zur falschen Seite.
- **Vertrag:** `shared/dbapi` beschreibt `LockTake` als „nimmt das Schloss, außer ein anderer hält
  es und seine Zeit ist bei `now` nicht vorbei" (`dbapi.go:190`). Ein Riegel, der genau das
  prüft, ändert den Vertrag **nicht** — er erfüllt ihn an einer zweiten Stelle. `shared/` bleibt
  unberührt. Das gilt nur, solange der Riegel nichts abweist, was der Vertrag erlaubt; sonst
  wäre es eine Vertragsänderung und damit Sache des Koordinators.

### Weg B — die Schloss-Spalte so festnageln, dass ein abweichender Typ nicht entstehen kann

Der Typ von `ngc_lock.until` wird beim Start gemessen und abgewiesen, wenn er nicht der ganze
ist: auf SQLite über `pragma_table_info`, auf MariaDB über `information_schema.columns`. Dazu ein
Test, der jede Migration daraufhin ansieht, dass `until` als `{{int}}` angelegt wird.

- **Kosten:** eine Abfrage einmal beim Start, und ein Test, der je Datenbankweg laufen muss.
  Der MariaDB-Weg läuft heute nur auf einer Maschine mit Verbund oder Container; ohne den
  überspringt sich der Fall, und dann ist er kein Riegel, sondern eine Zeile im Protokoll.
- **Schaden, wenn es schiefgeht:** ein Start, der verweigert, weil die Messung des Typs sich irrt
  (Schreibweise, Breite, eine Fassung, die den Typ anders nennt — gemessen: MariaDB meldet
  `bigint(20)`, nicht `BIGINT`). Das ist ein Ausfall des **ganzen** Servers, nicht einer
  Operation.
- **Was es nicht kann:** es hält nur den Typ, nicht die Richtung des Vergleichs. Gegen den
  sterbenden Mutanten aus dieser Messung hilft es **gar nichts** — der kippt bei ganz richtiger
  Spalte.

### Beide, in dieser Reihenfolge

Nach der Rangfolge fällt die Wahl nicht schwer: **A zuerst.** Der gemessene Mutant ist genau der
Fall, den A tötet und B nicht sieht. B ist danach sinnvoll, weil es den Verfügbarkeitsschaden
(klemmendes Schloss) früh und mit einer klaren Meldung sichtbar macht statt nach 900 Sekunden
Warten — aber es ist der zweite Schritt, nicht der erste, und seine Abweisung trifft mehr als A.

## Was passiert, wenn der Go-Riegel ein Schloss abweist, das die Datenbank für gehalten hält?

Das ist der unangenehme Fall, und er ist in beide Richtungen zu lesen:

- **Riegel weist ab, Datenbank hat es gegeben** (`UPDATE` hat getroffen, Go findet einen Ablauf
  in der Vergangenheit): Dann hat dieser Lauf die Zeile **schon überschrieben** — `owner` und
  `until` stehen auf ihm. Weist Go jetzt nur ab, bleibt ein Schloss liegen, das niemand hält und
  niemand freigibt: bis `until` abläuft, kommt keiner mehr dran, und bei der verbogenen Spalte
  kommt keiner mehr dran, **nie**. Darum muss die Abweisung die Zeile **zurückrollen** — bei
  `LockTake` ist die Transaktion schon da, bei `migrate.take` nicht, und sie muss dort hinein.
  Das ist der eine Punkt, an dem Weg A echte Arbeit ist und nicht eine Zeile.
- **Riegel weist ab, obwohl alles richtig war** (Fehlalarm, etwa weil der Wert nicht als Zahl
  einzulesen war): Dann melden beide Stellen „ein anderer hält es". Für `rollout` heißt das
  `ErrLostLease` und eine Runde, die stehenbleibt; für `migrate` heißt es ein Server, der nicht
  startet, mit der Meldung „ein anderer Server migriert". Beides ist ein Stillstand, und
  Stillstand ist nach der Rangfolge besser als zwei Server, die gleichzeitig DDL schreiben — aber
  es ist nicht gratis, und die Meldung muss sagen, dass der **Riegel** abgewiesen hat und nicht
  ein Nachbar. Sonst sucht der Betreiber den zweiten Server, den es nicht gibt.
- **Die Umkehrung, über die man leicht hinwegliest:** Go darf nicht abweisen, was der Vertrag
  erlaubt. `LockTake` gibt demselben Halter das Schloss **erneut**, mit neuer Zeit — darauf ruht
  `rollout.renewLease` (`rollout.go:172`), und `shared/dbapi/dbtest` misst es (`testLocks`, der
  dritte Fall). Ein Riegel, der stur „abgelaufen heißt verloren" prüft, bricht die Verlängerung
  genau dann, wenn eine Runde länger als die Pacht gebraucht hat. Der Riegel muss also denselben
  Unterschied kennen wie das SQL: **eigener Halter** darf immer, ein **fremder** nur nach Ablauf.
  `migrate.take` ist hier absichtlich strenger (`migrate.go:380`) und bleibt es.

## Was als nächstes gebaut wird

Erst nach Zustimmung der Leitsitzung, je Riegel ein kleiner Pull Request, und je Riegel ein
sterbender Mutant und eine lebende Gegenprobe auf **beiden** Datenbankwegen. Wo auf einem Weg
kein Mutant herstellbar ist, steht der Satz dazu samt Grund — nicht nur die Lücke.

Nach dieser Messung ist schon absehbar, wo dieser Satz hinkommt: ein Mutant, der die Spalte
verbiegt, ist auf **MariaDB nicht herstellbar**, weil MariaDB zum Vergleich umrechnet und in
allen zwölf gemessenen Zeilen richtig antwortet. Der Mutant „Richtung gedreht" ist dagegen auf
beiden Wegen herstellbar und stirbt auf beiden.

## Wie die Messung zu wiederholen ist

Das Messprogramm lag unter `tools/mess-schloss/` und ist **nicht** eingecheckt: es gehört nicht
ins Erzeugnis. Was es tat, steht vollständig oben — Spaltentyp umbauen, Zeile setzen, zweiten
Halter fragen, Kontrolle nach dem Ablauf. Mit Weg A kommt dieselbe Frage als richtiger Test
neben den Code, und dann ist sie wiederholbar, ohne dieses Blatt zu lesen.
