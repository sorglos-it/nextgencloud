# Der Ablauf der zwei Schlösser: gemessen und ein Riegel dazu

## Das Ergebnis zuerst

**In keiner gemessenen Lage bekamen zwei Halter das Schloss.** Es hält — auf SQLite und auf
MariaDB 11.8.6, bei ganzer Spalte und bei verbogener, mit dem Ablauf als Zahl und als Text. Was
eine verbogene Spalte kostet, ist nicht Sicherheit, sondern **Verfügbarkeit**: auf SQLite klemmt
das Schloss dann dauerhaft, weil der Vergleich zu viel abweist statt zu wenig. Das ist die
Entlastung, und sie steht vor allem anderen.

Gehalten hat dabei die **Richtung** des SQL-Vergleichs, nicht eine Absicht. Darum ist der Riegel
in Go nachgezogen (Weg A, gebaut): er fängt den Tag, an dem jemand `until > ?` für „gehalten"
schreibt — eine Zeile, die sich natürlicher liest als die heutige und ohne die der zweite Halter
auf beiden Datenbanken durchkommt. Weg B (Spaltentyp beim Start festnageln) ist **abgelehnt**, mit
Begründung weiter unten.

## Messplatz

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

## Die Wege, und was aus ihnen geworden ist

Rangfolge: **Sicherheit > Stabilität > Geschwindigkeit.** Weg A ist gebaut, Weg B ist abgelehnt.

### Weg A — den Ablauf zusätzlich in Go prüfen (gebaut)

Nach dem `UPDATE`, das das Schloss nimmt, wird die genommene Zeile gelesen und ihr `until` in Go
gegen `now` gehalten. Der Riegel im SQL bleibt; es kommt einer **dazu**.

- Für `LockTake` **gebaut**: die Prüfung steht in derselben Transaktion, die heute schon `UPDATE`
  und `INSERT` umspannt. Wie genau, steht weiter unten unter „Der gebaute Riegel".
- Für `migrate` **noch offen**, als eigener Pull Request: `hold` tut das Ähnliche schon, fragt
  aber die **eigene** Zeile nach dem Nehmen. Gebraucht wird die **fremde** Zeile, bevor `n > 0`
  als „genommen" gilt — und dort fehlt die Transaktion, in der eine Abweisung zurückrollen
  könnte. (`UPDATE … RETURNING` kennt SQLite, MariaDB nicht; darum ein `SELECT` in derselben
  Transaktion vor dem `UPDATE`.)
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

### Was der Bau von Weg A zusätzlich gemessen hat: MariaDB will die Zeile gehalten sehen

Die erste Fassung des Riegels las die Zeile mit einem gewöhnlichen `SELECT` in derselben
Transaktion, vor dem `UPDATE`. Auf SQLite lief alles grün; auf MariaDB 11.8.6 fiel der
Nebenläufigkeitsfall des Vertrags sofort:

```
dbtest.go:489: round 255, LockTake: Error 1020 (HY000):
Record has changed since last read in table 'ngc_lock'
```

Gefangen hat das `shared/dbapi/dbtest` (`testConcurrentLock`, 256 gleichzeitige Halter je Runde)
— also ein Prüfsatz, der schon da war. Der Riegel in dieser Form hätte eine Behebung gegen zwei
Server mit einem Fehler gegen viele gleichzeitige Halter bezahlt.

Die Abhilfe ist, die Zeile **zu halten**, wenn man über sie urteilen will: `FOR UPDATE`. Das ist
nicht portierbar — SQLite kennt die Klausel nicht —, darum entscheidet der Dialekt
(`Dialect.RowLock`, `dialect.go`), und es sind zwei Konstanten und nicht eine zusammengesetzte
Zeichenkette, weil der Wächter in `sqlcheck_test.go` jede Anweisung als Konstante an der Datei
lesen will.

Danach gemessen, **dreimal hintereinander**: `TestConformance` grün auf beiden Wegen (MariaDB
11,4 s / 11,8 s / 12,0 s, SQLite 1,02 s / 1,04 s / 1,06 s). Vorher: dieselbe Runde 255 rot.

Die Lehre für den nächsten, der einen Riegel hinter einen Schreibvorgang setzt: **ein Riegel, der
über eine Zeile urteilt, muss die Zeile halten, sonst urteilt er über eine, die schon nicht mehr
da ist** — und auf welcher Datenbank das auffällt, entscheidet nicht der Entwurf, sondern der
Prüfsatz, der beide Wege fährt.

### Und noch eine Falle des Messplatzes: ein Konto mit allen Rechten

Der Container hatte zuerst **ein** Datenbankkonto mit allen Rechten auf das Schema. Damit war
`TestAccountRights` — der Fall, der hält, dass das API-Konto keine Tabelle anlegen und keine
löschen darf — kein Riegel mehr, sondern ein Freispruch: sein `DROP TABLE ngc_blob` gelang. Der
nächste volle Lauf meldete dann elf rote Fälle mit
`Error 1146: Table 'ngcmess.ngc_blob' doesn't exist`, und das sah nach einem Fehler am Code aus.

Es war der Messplatz, der großzügiger war als die Anlage. Mit zwei Konten — `ngc` nur mit
SELECT/INSERT/UPDATE/DELETE, `ngcmigrate` zusätzlich mit CREATE/DROP/ALTER/INDEX — weist die
Datenbank das Anlegen wieder ab (`ERROR 1142: CREATE command denied`, gemessen) und der Satz ist
grün.

Zwei Nebenlehren, beide an diesem Tag bezahlt:

- Eine Vorrichtung, die mehr darf als das Programm, **misst ihre eigene Großzügigkeit** — und
  kann dabei das Schema zerstören, über das die nächsten Fälle urteilen.
- Die Gegenprobe darauf hat beim ersten Versuch nichts gefunden, weil die Fehlermeldung nach
  `stderr` ging und die Prüfung nur `stdout` ansah. Eine fehlende Fehlermeldung ist kein Beleg
  für Erfolg: geprüft wurde dann der Ausstiegscode (`1`) und die Meldung selbst.

### Weg B — die Schloss-Spalte beim Start festnageln (abgelehnt)

**Abgelehnt**, und zwar aus den Zahlen dieser Messung selbst. Der Vorschlag war, den Typ von
`ngc_lock.until` beim Start zu messen und einen abweichenden abzuweisen. Zwei Gründe, beide
gemessen:

- Er fängt die gemessene Gefahr **gar nicht**. Der sterbende Mutant dieser Messung („Richtung
  gedreht") kippt bei **ganz richtiger** Spalte; eine Typprüfung sieht ihn nie.
- Sein Schaden bei einem Irrtum ist, dass der **ganze Server nicht startet** — und ein Irrtum ist
  nah: MariaDB meldet den Typ als `bigint(20)` und nicht als `BIGINT`, die Breite steht in der
  Antwort und nicht in der Migration.

Ein Riegel, der die gemessene Gefahr nicht fängt und dafür eine gesunde Anlage stilllegen kann,
ist nach der Rangfolge nicht „später", sondern nein. Entschieden von der Leitsitzung am
03.11.2026. Ein anderer Strang hat dieselbe Prüfung für die Startzeit untersucht; dort liegt sie
mit demselben Vorbehalt. Was unten als Beschreibung von Weg B steht, bleibt als Begründung
stehen — nicht als offener Punkt.

### Weg B, wie er gedacht war

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

Der Verfügbarkeitsschaden, den B früh sichtbar gemacht hätte, bleibt damit so sichtbar, wie er
ist: als klemmendes Schloss und als zwei Testfälle, die ihn festnageln
(`TestLockTakeOnABentColumnStillRefuses`, `TestLockTakeOnATextColumnOrdersByDigits`).

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

## Der gebaute Riegel, und wie er gemessen ist

Gebaut in `sqlstore.LockTake` (`once.go`): die Zeile wird in derselben Transaktion gelesen (bei
MariaDB mit `FOR UPDATE`), das `UPDATE` bleibt unverändert und entscheidet weiter allein, und
**nach** dem Treffer urteilt `freeForTake` in Go über die Zeile, die das `UPDATE` überschrieben
hat. Eine Abweisung ist ein Fehler nach innen (`errLockHeld`), damit die Transaktion das
`UPDATE` **zurückrollt**; nach außen wird daraus das `false`, das der Vertrag vorsieht. Ein
Ablauf, der keine Zahl ist, ist dagegen ein echter Fehler (`errLockBent`) und wird gemeldet.

Der Riegel kennt denselben Unterschied wie das SQL: **eigener Halter immer, fremder nur nach
Ablauf.** Ohne das bricht die Verlängerung von `rollout.renewLease` — und genau das ist unten
gemessen.

Vier Varianten, je Variante der ganze Satz auf **beiden** Datenbankwegen, alle mit
`go build/test -overlay` und ohne eine Datei im Arbeitsbaum zu verbiegen. Vorrichtungsprüfung je
Mutant: das gesuchte Muster genau einmal vorher da, nachher nicht mehr, Mutant übersetzt.

| Variante | Was sie ändert | SQLite | MariaDB |
|---|---|---|---|
| Grundstand | — | grün (3 von 3 Läufen) | grün (3 von 3 Läufen) |
| `sql` | `until <= ?` → `until > ?` | **stirbt** | **stirbt** |
| `riegel` | im Riegel `until <= now` → `until > now` | **stirbt** | **stirbt** |
| `eigen` | der Riegel kennt den eigenen Halter nicht mehr | **stirbt** | **stirbt** |
| `leben` | nur eine lokale Variable umbenannt | grün | grün |

Zwei Zeilen daraus sind die eigentliche Aussage:

- **Der Mutant `sql` lässt den fremden Halter nicht mehr durch.** Ohne den Riegel gemessen (Lage
  `ganz`, beide Wege): zweiter Halter gleichzeitig **true**. Mit dem Riegel fällt derselbe Satz
  rot — aber an den Fällen für die *Übernahme nach Ablauf*, während die Zusicherung „fremder
  Halter, Ablauf läuft noch → abgewiesen" **hält**. Der Mutant stirbt also, und das, was er
  gefährlich macht, ist weg.
- **Der Mutant `eigen` tötet die Verlängerung**, und zwar an `TestLockTakeAgainWithTheSameTime`,
  an `TestConformance/locks` auf beiden Wegen und an „the owner renews" in den neuen Fällen. Das
  ist der Beleg, dass der Riegel nicht nur streng, sondern richtig streng ist.

Dazu drei Dinge, die in den Baum gekommen sind statt in ein Messprogramm:

- `apps/server-db/classes/sqlstore/freefortake_test.go` — 14 Fälle über den Riegel allein. Er ist
  die einzige Stelle, die ihn heute beurteilen kann: mit dem `until <= ?` von heute weist eine
  verbogene Spalte immer zu viel ab, nie zu wenig, also kommt keine Zeile am SQL vorbei, über die
  der Riegel urteilen könnte. Das steht auch im Test, damit der nächste Leser nicht nach einem
  Integrationsfall sucht, den es nicht geben kann.
- `apps/server-db/classes/sqlite/lockdeadline_test.go` — die Messung dieses Blattes als Prüffälle:
  ganze Spalte (Kontrolle), BLOB mit Textwert, TEXT mit ungleicher Ziffernlänge.
- `apps/server-db/classes/mariadb/lockdeadline_test.go` — dieselben Fragen am Container. Hier
  **ist der Fehler nicht herstellbar**, weil MariaDB zum Vergleich umrechnet; der Test sagt das,
  indem er grün ist, und sagt im Kopf, warum er trotzdem da ist. Ohne ihn bliebe von „auf
  MariaDB nicht herstellbar" nichts übrig, was jemand lesen kann.

## Was noch offen ist

- **Der zweite Riegel, in `migrate.take`.** Dort gibt es heute keine Transaktion um das `UPDATE`
  und den `INSERT`, und eine Abweisung muss zurückrollen können — die Transaktion muss also
  hinein. Das ist der eigene Pull Request, der auf diesen folgt. `migrate.take` bleibt dabei
  absichtlich strenger als `LockTake`: gehalten heißt gehalten, auch für den eigenen Namen
  (`migrate.go:380`).
- **Nicht in diesem Auftrag und nicht angefasst:** `onceUse` (`once.go`) steht weiter als
  `… AND expires > ?`, also in der gefährlichen Richtung. Der Riegel dagegen ist gebaut und
  geprüft und liegt als PR 181; er hängt nur noch am Zusammenführen. Hier steht es, damit
  niemand glaubt, in `main` sei es schon erledigt.
- **Nicht gemessen:** wie sich `FOR UPDATE` auf einem echten Galera-Verbund mit drei Knoten
  verhält. Der Container ist ein einzelner Knoten. Ein Konflikt über Knoten hinweg kommt als
  Zertifizierungskonflikt zurück, und den wiederholt `sqlstore.write` schon heute — aber das ist
  eine Überlegung und keine Zahl, und so steht es hier.

## Wie die Messung zu wiederholen ist

Sie läuft jetzt von selbst: `go test ./apps/server-db/...`. Die SQLite-Fälle brauchen nichts, die
MariaDB-Fälle einen Server, dessen Zugang in `NGC_DB_NODES`, `NGC_DB_CA`, `NGC_DB_NAME`,
`NGC_DB_USER`/`NGC_DB_PASSWORD` und `NGC_DB_MIGRATE_USER`/`NGC_DB_MIGRATE_PASSWORD` steht; ohne
den überspringen sie sich und sagen das. Für diese Messung war das ein podman-Container mit genau
der Fassung der Anlage, TLS 1.3 erzwungen — und mit **zwei** Konten, siehe oben: mit einem
einzigen allmächtigen Konto messen die Fälle etwas anderes.

Das Messprogramm, mit dem der Befund gefunden wurde, lag unter `tools/mess-schloss/` und ist
**nicht** eingecheckt: was es konnte, können jetzt die Tests. Es liegt im Scratchpad der Sitzung,
die es geschrieben hat, und nichts hängt daran.
