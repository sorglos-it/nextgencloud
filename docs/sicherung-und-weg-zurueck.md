# Sicherung und der Weg zurück

Gemessen am 24.10.2026 an der verteilten Anlage `ngc.mein.lan` (10.2.3.210–.215),
Stand `17b9895` (origin/main). Messregeln: `Z:\GitHub\claude-md\messen.md`.

Dieses Blatt trennt streng: **gemessen** heißt, ein Lauf hat die Zahl geliefert und sie steht
daneben. **Abgewogen** heißt, es ist aus Gemessenem geschlossen und niemand hat es ausgeführt.
Am Ende steht je Abschnitt, was welches ist.

---

## 0. Was die Namen verschweigen (gemessen)

Die Rolle steht in `controller.json`, nicht im Rechnernamen. Am laufenden Dienst abgefragt:

| Adresse | `hostname -s` | `name` in controller.json | `roles` | laufende NGC-Dienste |
|---|---|---|---|---|
| .210 | api-1 | **web-api-1** | `["web"]` | ngc-api **und** ngc-controller |
| .211 | api-2 | — (kein controller.json) | — | nur ngc-api |
| .212 | db-1 | db-1 | `["db"]` | ngc-controller (+ mariadb) |
| .213 | svc-1 | svc-1 | `["services"]` | ngc-controller |
| .214 | svc-2 | svc-2 | `["services"]` | ngc-controller |
| .215 | store | — (kein /etc/ngc) | — | nur nginx |

Zwei Dinge daran gehören in jede Sicherungsüberlegung:

- **.210 trägt zwei Programme mit zwei Identitäten**: `ngc-api` als Knoten `api-1`
  (`/etc/ngc/api.json`) und `ngc-controller` als Knoten `web-api-1` mit der Rolle `web`
  (`/etc/ngc/controller.json`). Wer „api-1 sichern" sagt, meint zwei Dateien mit getrennten
  Schlüsselsätzen.
- **.211 hat keinen Controller.** Es gibt dort kein `controller.json`, kein
  `trust-anchor.json`, keinen `/var/lib/ngc/controller/`, sondern `/var/lib/ngc/joined`.
  Ein Werkzeug, das „den Controller jedes Knotens sichert", sichert von .211 nichts außer
  `api.json` — und `api.json` ist dort das Wertvollste der Maschine.

Der Anker steht je Maschine auf einem anderen Stand (`trust-anchor.json`): api-1 Folge 6,
db-1 Folge 4, svc-2 Folge 3, svc-1 Folge 1. Es gibt keine eine Ankerdatei der Anlage.

---

## 1. Teil für Teil: was heute in der Sicherung ist

Es gibt genau ein Sicherungskommando in der ganzen Anlage: `ngc-controller backup <Datei>`.
Es schreibt eine Kopie der **eigenen SQLite-Datei dieses Controllers**, verschlüsselt, ohne
Schlüssel darin (`apps/server-controller/classes/state/backup.go`).

| Teil der Anlage | wo er liegt | in der Sicherung? |
|---|---|---|
| Zeilen der Apps mit Manifest, Lizenzen, Vertrauenslisten-Versionen, gelegte Aktualisierungen, gesehene Aufgaben-Kennungen | `state.db` des Controllers | **ja**, das ist genau der Inhalt |
| **Benutzer, Sitzungen, Blobs, Protokoll der Anlage** (`ngc_user`, `ngc_session`, `ngc_blob`, `ngc_log`) | MariaDB auf db-1 | **nein. Von nichts.** Siehe Abschnitt 2 |
| **dbKey** (öffnet `state.db` *und* die App-Datenordner) | `/etc/ngc/controller.json` | **nein** — und ohne ihn ist die Sicherung wertlos (Abschnitt 4) |
| **noiseKey** = die Kennung dieses Rechners in der Vertrauensliste | `/etc/ngc/controller.json` | **nein** |
| **signKey, storeKey, secretKey, challengeKey** der API, Datenbank-Kennwort | `/etc/ngc/api.json` | **nein** |
| **Schlüssel der verschlüsselten MariaDB-Tabellen** | `/etc/ngc/db-keys/keyfile.key`, `keys.enc` | **nein** |
| TLS-Zertifikate und ihre Schlüssel, `db-ca` | `/etc/ngc/tls/`, `/etc/ngc/db-ca/`, `db-tls/` | **nein** |
| Anker-Stand | `/etc/ngc/trust-anchor.json` | **nein** (steht zusätzlich als Feld `anchor` in controller.json) |
| **verschlüsselte Datenordner der Apps** | `/var/lib/ngc/apps/<app>/` | **nein** |
| App-Paket (`.ngc`) und Abbild des Dienstes | kommen aus dem Laden, liegen in `staged/` | **nicht nötig** — die Installation holt sie neu aus dem Laden, Signatur geprüft |
| Container, App-Benutzer (`ngc-uhr-ad8c00e2` …), `/run/ngc` | lokal auf dem Knoten | **nicht nötig** — `node.Restore` baut sie beim Start neu auf (und *das* ist, was `restore.go` heißt: siehe Abschnitt 3) |
| Vertrauensliste selbst | von der API geholt, Kopie in `state.db` | **ja**, als Kopie — der gültige Stand kommt von der API |

**Zusammengefasst, und das ist der Befund:** gesichert wird heute genau der eine Teil, den die
Anlage auch von der API wiederbekäme. Nicht gesichert wird jeder Teil, den niemand neu
erzeugen kann.

---

## 2. Die Benutzerdaten haben überhaupt keinen Weg (gemessen)

Zwei Stellen im Code sagen einem Admin, wo die MariaDB gesichert wird:

- `apps/server-controller/classes/state/backup.go:60`: „… gesichert wird ein MariaDB-Verbund
  auf den Datenbankservern mit **\"ngc-db backup\"**"
- `apps/server-db/classes/register/register.go:127`: dieselbe Auskunft

**Dieses Programm gibt es nicht.** `apps/server-db/` hat kein `main.go` und keinen Einstieg —
es ist eine Bibliothek. Auf keiner der fünf Maschinen liegt ein `ngc-db`
(`ls /usr/lib/ngc/` auf .210/.212/.213/.214 zeigt `ngc-api`, `ngc-controller`, `ngc-store`
und ihre Vorgängerstände, nichts weiter).

Was auf db-1 wirklich in der Datenbank steht (gezählt am laufenden Dienst):

```
ngc_app 2   ngc_blob 15   ngc_log 470   ngc_once 17   ngc_session 27
ngc_user 1  ngc_schema 1  ngc_lock 0    ngc_pending 0
```

Ein Benutzerkonto, 15 Blobs, 27 Sitzungen. **Für diese Zeilen gibt es heute kein
Sicherungskommando, kein Skript und keinen Hinweis, der nicht auf ein Programm zeigt, das
nicht existiert.** Das ist der größte Posten dieses Befunds.

Das Werkzeug dafür ist auf db-1 da, nur fährt es niemand: `mariabackup` 11.8.6 liegt in
`/usr/bin`. Zur Probe gefahren, bei laufender Datenbank:

```
mariabackup --backup --target-dir=…   Ausstiegscode 0, Dauer 1 s, 49 MB
mariadb danach: active;  select count(*) from ngc.ngc_blob -> 15
```

Die Datenbank hat weitergelaufen und weiter geantwortet. Also fehlt kein Verfahren, sondern
der Aufruf — und das macht den Posten klein zu schließen und umso teurer, ihn offen zu lassen.

---

## 3. `restore.go` ist nicht der Weg zurück

`apps/server-controller/classes/node/restore.go` heißt so, tut aber etwas anderes: es fährt
nach einem Neustart die Apps dieses Rechners wieder hoch (Lizenz fragen, Datenordner
aufschließen, Laufzeitordner anlegen, Container starten, Start-Aktion des Manifests fahren).
Es liest keine Sicherung, nimmt keine Datei an und kennt kein Ziel außer „dieser Rechner,
jetzt".

**In der ganzen Anlage gibt es kein Kommando, das eine Sicherung zurückliest.** Die
Kommandoliste des Controllers ist vollständig: `pair`, `backup`, `terminal`, `hold`, `image`
(`apps/server-controller/classes/node/main.go:151`). `ngc-api` hat ein `backup` und kein
Gegenstück.

---

## 4. Die Sicherung hängt am Schlüssel des Servers — gemessen, in beide Richtungen

Die Frage des Auftrags war, ob eine Sicherung ohne `config.Controller.DBKey` wertlos ist.
Sie ist es, und zwar nachweislich und nicht ungefähr.

Gemessen mit `tools/sicherung-probe` auf einem **fremden Rechner** (Windows, nicht in der
Anlage, nie gekoppelt), gegen die Kopie, die `ngc-controller backup` auf svc-1 geschrieben
hat (`sha256` der Kopie auf svc-1 und der heruntergeholten Datei gleich:
`2c8a7f30dd03618d…`):

| Versuch | Antwort |
|---|---|
| mit dem dbKey aus demselben `controller.json` | **OFFEN**, 2 App-Zeilen: `org.sorglos.terminal@1.0.0`, `org.sorglos.uhr@1.0.0` |
| derselbe Schlüssel, **ein Bit** gekippt | `sqlite3: file is not a database` |
| ohne Schlüssel | `the database key needs 64 bytes for AES-256-XTS` |
| mit dem dbKey von **api-1** statt svc-1 | `sqlite3: file is not a database` |

Daraus drei Sätze, und alle drei sind gemessen:

1. **Die Sicherung ist nicht an die Maschine gebunden, sondern allein an den Schlüssel.**
   Sie öffnet auf einem beliebigen Rechner, sogar unter Windows. Für die Wiederherstellung
   ist das die gute Nachricht; für die Aufbewahrung ist es die ganze Gefahr.
2. **Ohne den Schlüssel ist sie Bytes.** Kein Teilverlust, keine Rettung mit Mühe: ein
   gekipptes Bit und „file is not a database".
3. **Jeder Rechner hat seinen eigenen Schlüssel.** Vier verschiedene dbKeys auf dieser Anlage
   (Fingerabdrücke der Felder, die Schlüssel selbst standen nie auf einem Bildschirm):
   web-api-1 `633cff5e…`, db-1 `4c4bd740…`, svc-1 `fe836302…`, svc-2 `3dc34543…`. Es gibt
   **keinen einen Schlüssel der Anlage**, den man einmal in den Tresor legt.

### Der Datenordner der App hängt am selben Schlüssel (gemessen)

Der Code sagt, der Schlüssel eines App-Datenordners werde aus dem dbKey abgeleitet
(`runtime/data.go`: HKDF-SHA256, App-Kennung als Salz, Info `ngc/appdata/v1`, 64 Byte für
AES-256-XTS). Das ist nicht nur gelesen, sondern am Kern von svc-1 nachgerechnet. Der Kern
nennt für jeden verschlüsselten Ordner eine Schlüsselkennung (fscrypt v2); daneben steht die
Kennung, die aus dem dbKey dieses Rechners gerechnet wird:

| App | aus dbKey gerechnet | im Kern von svc-1 |
|---|---|---|
| org.sorglos.terminal | `a26b3623dc61f8e1a3693dc0667aaa32` | `a26b3623dc61f8e1a3693dc0667aaa32` |
| org.sorglos.uhr | `060244942f4faf1dd8453e6ab359558e` | `060244942f4faf1dd8453e6ab359558e` |

Gleich, beide. **Damit ist belegt: wer `/etc/ngc/controller.json` eines Service-Knotens
verliert, verliert die Daten jeder App auf dieser Maschine endgültig** — und wer die Datei
hat, hat die Daten jeder App dieser Maschine.

### Die MariaDB hat dieselbe Falle, ein zweites Mal (gemessen)

Die Tabellen sind zwangsverschlüsselt (`innodb_encrypt_tables = FORCE`), der Schlüssel kommt
aus `/etc/ngc/db-keys/keyfile.key` + `keys.enc` (file_key_management, aes_ctr). Die
Probe-Sicherung von `mariabackup` enthält diese Dateien **nicht**, aber ihre `backup-my.cnf`
zeigt auf sie. Gegenprobe: der Benutzername in `ngc_user` ist `weiricth`; in **keiner**
`.ibd` der Sicherung kommt er vor (`grep -c` über alle neun: jedes Mal 0). Die Sicherung ist
also echtes Geheimtext — und ohne `db-keys` genauso wertlos wie `state.db` ohne dbKey.

### Was daraus für die Aufbewahrung folgt (abgewogen)

Das ist eine Sicherheitsfrage mit einer Stabilitätsfolge, und beides zeigt in
entgegengesetzte Richtungen:

- **Sicherheit:** `/etc/ngc/controller.json` und `/etc/ngc/api.json` sind die Anlage. Wer
  beides mit der Sicherung zusammen in einem Ordner hat, braucht keinen Einbruch mehr: die
  Sicherung öffnet woanders (gemessen). Also müssen sie getrennt liegen.
- **Stabilität:** getrennt liegende Schlüssel sind getrennt verlierbar. Und ein Verlust ist
  hier nicht „schwer zu reparieren", sondern **endgültig**: kein Dienstleister, kein
  Hersteller, kein Rechenaufwand holt die Daten zurück.
- Nach Thomas' Rangfolge (Stabilität vor Sicherheit) heißt das **nicht**, die Schlüssel
  dazuzulegen, sondern: **mehrere Verwahrstellen für den Schlüssel, eine Verwahrstelle für
  die Daten.** Ein Schlüssel, der an zwei Orten liegt, ist nicht halb so sicher, aber eine
  Sicherung ohne Schlüssel ist ganz wertlos.
- Daraus folgt eine Entscheidung, die Thomas gehört und die dieses Blatt nicht trifft: ob
  die vier dbKeys bei einer Neuinstallation **vorgegeben** werden dürfen, statt sie erzeugen
  zu lassen. Ohne das ist „dieselbe Maschine wiederherstellen" nicht möglich, weil der neue
  Rechner einen neuen Schlüssel bekommt und damit die alte Sicherung nicht öffnet. Wer sie
  vorgeben darf, kann sie auch falsch vorgeben.

---

## 5. Der ehrliche Fall: eine Maschine ist weg und kommt als neue zurück

Was ein neuer Rechner braucht, damit die Anlage ihn wieder annimmt — aus dem Code gelesen
(`node/main.go` `pair`, `pairing.Do`, `config.SaveController`) und am laufenden Stand der
Anlage gegengehalten, **aber nicht ausgeführt** (siehe Abschnitt 6):

1. **`controller.json` muss schon da sein, vollständig.** `pair` lädt sie als Erstes und
   bricht ohne sie ab. Name, Rollen, Datenbankpfad, `dbKey`, `releaseKey` stehen darin und
   kommen **nicht** aus der Kopplung. Geschrieben wird sie von `ngc-controller.sh` aus *ulh*,
   also außerhalb dieses Repos (`systemctl cat ngc-controller.service` nennt die Datei).
2. **Ein Kopplungscode**, gültig 24 h und für genau eine Kopplung. Den zeigt die Oberfläche
   einem angemeldeten Admin.
3. **Die Kopplung erzeugt einen neuen Noise-Schlüssel und setzt `node` auf leer** — der neue
   Rechner gehört zunächst zu niemandem. Erst wenn ein Admin seinen öffentlichen Schlüssel in
   die Vertrauensliste schreibt, ist er ein Knoten (`msgListAgain` sagt das dem Admin).
4. **Die Apps kommen von allein wieder** — wenn `state.db` und `dbKey` zusammenpassen: die
   Zeilen stehen in `state.db`, die Pakete holt die Installation aus dem Laden, Container und
   App-Benutzer baut `node.Restore` neu.
5. **Die Daten der Apps kommen nur mit dem alten `dbKey` wieder.** Mit einem neuen Schlüssel
   sind die mitgenommenen Ordner Geheimtext ohne Schlüssel.

Der entscheidende Satz daraus: **der Weg zurück braucht kein neues Programm.** Ein Controller
öffnet, was an dem Pfad in seiner `controller.json` liegt — und dass eine *Kopie* sich öffnen
lässt, ist in Abschnitt 4 auf einer fremden Maschine gemessen. Was fehlt, ist nicht ein
`restore`-Kommando, sondern **das Einsammeln** und **eine festgenagelte Reihenfolge**.

Eine Warnung, die in jede Reihenfolge gehört und aus der Messung fällt: `state.Open` **spielt
Migrationen in die Datei, die es öffnet**. Eine Sicherung, die man „nur mal ansieht", ist
danach nicht mehr der Stand, der sie war. Immer auf einer Kopie der Kopie arbeiten —
`tools/sicherung-probe` sagt das in seinem Kopf und tut es nicht selbst.

---

## 6. Was fehlt — und was dieses Blatt deshalb nicht behauptet

**Noch nie ausgeführt, auf keiner Maschine:**

1. Eine Sicherung zurückspielen. Weder `state.db` noch `mariabackup --prepare` +
   Wiedereinspielen noch ein App-Datenordner.
2. Einen neuen Rechner mit einem *alten* `controller.json` koppeln und in die Vertrauensliste
   aufnehmen. Schritt 2 und 3 aus Abschnitt 5 brauchen einen **angemeldeten Admin in der
   Oberfläche**; ein Skript kann das nicht sein, und wer es nachstellt, misst seine eigene
   Vorrichtung (dieselbe Grenze, die `tools/erstlauf-verteilt.sh` in ihrem Kopf nennt).
3. Ob ein fscrypt-Datenordner überhaupt aus einem `tar` zurückgelegt werden kann. Beim Lesen
   lief der Dienst, der Kern hatte den Schlüssel — `tar` hat deshalb lesbare **Namen** und
   verschlüsselte **Inhalte** mitgenommen. Das ist vermutlich nicht die Form, die man
   zurücklegt, und es ist nicht geprüft.

**Dafür fehlt eine eigene Maschine.** An der laufenden Anlage wird das nicht geübt: dort
läuft eine eingerichtete Terminal-App, mehrere Sitzungen arbeiten gleichzeitig daran, und
eine Wiederherstellung schreibt genau die Dateien, an denen sie arbeiten. Die alte Einzel-VM
.208 ist aus und gehört nicht dieser Sitzung.

**Die Bitte an Thomas:** eine Wegwerf-Maschine (eine VM reicht, Debian wie die anderen), auf
der die drei Punkte oben wirklich gefahren werden dürfen. Erst danach darf in diesem Repo ein
Satz stehen, der mit „wiederhergestellt" anfängt.

---

## 7. Das kleinste Stück, das den Weg schließt — und es ist gelaufen

`tools/sicherung.sh` sammelt von jeder Maschine die fünf Dinge ein, die niemand neu erzeugen
kann, je Rechner einen Ordner, mit `inhalt.txt` darin, das zu jeder Datei sagt, *was* sie ist
und *ohne was* sie wertlos ist.

Es baut die Wiederherstellung **nicht**. Es schließt die Lücke, die in Abschnitt 1 steht: dass
heute der eine Teil gesichert wird, den die Anlage ohnehin wiederbekäme, und kein einziger,
den sie nicht wiederbekäme.

Gegen die Anlage gefahren, einmal, mit allen fünf Maschinen:

```
sicherung: root@10.2.3.210 (api-1)
sicherung: root@10.2.3.211 (api-2)
sicherung: root@10.2.3.212 (db-1)
sicherung: root@10.2.3.213 (svc-1)
sicherung: root@10.2.3.214 (svc-2)
sicherung: ok
Ausstiegscode 0, Dauer 25 s, 348 MB
```

Was dabei herauskam:

| Rechner | etc-ngc.tar | state.db | mariadb.tar | app-daten.tar |
|---|---|---|---|---|
| api-1 | 20 KB | 110 KB | — | — |
| api-2 | 10 KB | — (kein Controller) | — | — |
| db-1 | 20 KB | 114 KB | 50 MB | — |
| svc-1 | 10 KB | 122 KB | — | 201 MB |
| svc-2 | 10 KB | 118 KB | — | 112 MB |

Die Controller sind während ihrer Sicherung gelaufen und liefen danach weiter
(`systemctl is-active` vor und nach jedem Lauf: `active`), die Datenbank ebenso. Auf den
Maschinen blieb nichts zurück: das Skript legt nur `/var/tmp/ngc-sicherung-lauf` an und löscht
es je Maschine selbst (nachgesehen, auf allen fünf leer).

**Und das Eingesammelte ist auch benutzbar — gemessen:** aus dem Ordner `svc-1` das
`controller.json` aus `etc-ngc.tar` geholt und mit dessen dbKey das `state.db` **aus derselben
Sicherung** geöffnet: offen, zwei App-Zeilen. Mit dem dbKey von api-1 aus derselben Sicherung:
`file is not a database`. Die Sicherung trägt also alles, was sie zum Öffnen braucht, und sie
trägt es je Maschine getrennt.

---

## Was gemessen und was abgewogen ist

**Gemessen** (Lauf, Zahl, Ausstiegscode): Abschnitt 0 ganz; Abschnitt 1 die Spalte „wo er
liegt" und die Abwesenheit von `ngc-db`; Abschnitt 2 ganz (Zeilenzahlen,
`mariabackup` 1 s / 49 MB / Datenbank weiter aktiv); Abschnitt 3 die Kommandoliste;
Abschnitt 4 ganz (vier Öffnungsversuche, vier dbKey-Fingerabdrücke, zwei fscrypt-Kennungen,
`grep` über neun `.ibd`); Abschnitt 7 ganz.

**Abgewogen** (aus Gemessenem geschlossen, nicht ausgeführt): Abschnitt 1 die Spalte „in der
Sicherung?" für die Teile, die nachweislich nirgends eingesammelt werden — dass sie *fehlen*
ist gemessen, dass sie beim Zurückspielen *gebraucht* werden ist geschlossen; Abschnitt 4 der
Unterabschnitt zur Aufbewahrung; Abschnitt 5 ganz — die fünf Schritte sind aus dem Code
gelesen und gegen den Stand der Anlage gehalten, **gefahren ist keiner**.

**Weder noch, sondern eine Entscheidung für Thomas:** ob dbKeys bei einer Neuinstallation
vorgegeben werden dürfen (Ende von Abschnitt 4), und ob eine Wegwerf-Maschine für die drei
offenen Punkte aus Abschnitt 6 bereitsteht.
