# Sicherung und der Weg zurück

Gemessen am 24.10.2026 an der verteilten Anlage `ngc.mein.lan` (10.2.3.210–.215) und am
selben Tag auf einer eigenen Wegwerf-Maschine (VM 310, `10.2.3.220`, aus der Proxmox-Vorlage
300 geklont), Stand `17b9895` (origin/main). Messregeln: `Z:\GitHub\claude-md\messen.md`.

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

### Eine Berichtigung, und sie geht in die andere Richtung (gemessen)

Dieses Blatt hat zuerst behauptet, das `tar` eines App-Datenordners trage lesbare **Namen** und
verschlüsselte **Inhalte**. Das ist falsch, und die Wiederherstellung hat es gezeigt: beim
Sichern **läuft der Dienst**, also liegt der Schlüssel jeder App im Schlüsselbund des Kerns, und
root liest die Ordner durch ihn hindurch. Gezählt im `tar` von svc-1: 1419 ELF-Köpfe und die
dpkg-Listen des Containers, im Klartext zu lesen.

Zwei Folgen, die beide zählen:

- **Sicherheit:** `app-daten.tar` ist der empfindlichste Teil der ganzen Sicherung, weil er der
  einzige ist, der **ohne jeden Schlüssel** aufgeht. Er gehört genauso weggeschlossen wie
  `etc-ngc.tar` — und das ist der Teil, den man beim Wort „verschlüsselte Datenordner" gerade
  nicht erwartet. `tools/sicherung.sh` sagt es jetzt in diesen Worten in jedem `inhalt.txt`.
- **Stabilität:** dieselbe Tatsache ist die Rettung. Die Daten der Apps überleben einen Verlust
  des dbKey, solange dieses `tar` existiert, und sie lassen sich ohne Schlüssel zurücklegen —
  gemessen, beide Apps kamen damit auf eine frische Maschine.

### Die MariaDB hat dieselbe Falle, ein drittes Mal (gemessen)

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
Anlage gegengehalten. Die Schritte 1, 4 und 5 sind in Abschnitt 6 **gefahren**, die Schritte 2
und 3 nicht — die brauchen einen angemeldeten Admin:

1. **`controller.json` muss schon da sein, vollständig.** `pair` lädt sie als Erstes und
   bricht ohne sie ab. Name, Rollen, Datenbankpfad, `dbKey`, `releaseKey` stehen darin und
   kommen **nicht** aus der Kopplung. Geschrieben wird sie von `ngc-controller.sh` aus *ulh*,
   also außerhalb dieses Repos (`systemctl cat ngc-controller.service` nennt die Datei).
2. **Ein Kopplungscode**, gültig 24 h und für genau eine Kopplung. Den zeigt die Oberfläche
   einem angemeldeten Admin.
3. **Die Kopplung erzeugt einen neuen Noise-Schlüssel und setzt `node` auf leer** — der neue
   Rechner gehört zunächst zu niemandem. Erst wenn ein Admin seinen öffentlichen Schlüssel in
   die Vertrauensliste schreibt, ist er ein Knoten (`msgListAgain` sagt das dem Admin).
4. **Die Apps kommen fast von allein wieder** — und an dem „fast" hängt der ganze Abschnitt 6:
   die Zeilen stehen in `state.db`, aber der **Systembenutzer** jeder App und ihr
   verschlüsselter Datenordner werden nur von der Einrichtung angelegt, nicht von
   `node.Restore`. Ohne diesen Schritt bleibt jede App aus. Gemessen, nicht geschlossen.
5. **Die Daten der Apps kommen nur mit dem alten `dbKey` wieder.** Mit einem neuen Schlüssel
   sind die mitgenommenen Ordner Geheimtext ohne Schlüssel.

Der entscheidende Satz daraus: **der Weg zurück braucht fast kein neues Programm.** Ein
Controller öffnet, was an dem Pfad in seiner `controller.json` liegt — und dass eine *Kopie*
sich öffnen lässt, ist in Abschnitt 4 auf einer fremden Maschine gemessen. Gefehlt haben drei
Dinge, und nur das letzte ist ein Programm: **das Einsammeln** (`tools/sicherung.sh`), **eine
festgenagelte Reihenfolge** (Abschnitt 6) und **der Systembenutzer samt Datenordner jeder App**
(`tools/sicherung-zurueck`).

Eine Warnung, die in jede Reihenfolge gehört und aus der Messung fällt: `state.Open` **spielt
Migrationen in die Datei, die es öffnet**. Eine Sicherung, die man „nur mal ansieht", ist
danach nicht mehr der Stand, der sie war. Immer auf einer Kopie der Kopie arbeiten —
`tools/sicherung-probe` sagt das in seinem Kopf und tut es nicht selbst.

---

## 6. Der Weg zurück, wirklich gegangen

Thomas hat eine Maschine freigegeben, also ist der Weg nicht beschrieben, sondern gefahren.
**VM 310 `ngc-sicherung-probe`, 10.2.3.220**, Vollklon der Proxmox-Vorlage 300, zweite Platte mit
`mkfs.ext4 -O encrypt` auf `/var/lib/ngc/apps`, podman 5.4.2 — dieselbe Fassung wie auf svc-1.
Diese Maschine hat der Anlage nie gehört.

**Eine Vorsichtsmaßnahme, und sie ist keine Nebensache:** in der zurückgespielten
`controller.json` habe ich `apis` auf `wss://127.0.0.1:7443/tunnel` gesetzt. Schlüssel und
TLS-Abdruck blieben unverändert aus der Sicherung. Grund: die Datei trägt den `noiseKey` von
svc-1, und eine zweite Maschine, die sich damit bei der echten API meldet, wäre svc-1 ein zweites
Mal. Das ist die erste Regel jeder Wiederherstellung, und sie steht in keiner Anleitung:
**erst die API-Adresse totlegen, dann starten.**

### Was zurückgespielt wurde, und was dabei herauskam

| Schritt | Ergebnis |
|---|---|
| `etc-ngc.tar` ausgepackt, Modi erhalten | `controller.json` 0600 root, `trust-anchor.json` 0644 — wie auf svc-1 |
| `state.db` nach `/var/lib/ngc/controller/` | Prüfsummen der drei Dateien gegen `inhalt.txt`: alle **OK** |
| Controller mit `apis: []` gestartet | **abgewiesen**: „Die Konfiguration dieses Servers fehlt oder ist unvollständig." Ein Controller lässt sich nicht ohne API-Verweis starten — man kann eine Wiederherstellung also nicht erst einmal offline prüfen |
| Controller mit totem API-Ziel gestartet | **die Datenbank öffnete.** Beide App-Zeilen gelesen, beide Apps angefasst |

Und dann stand er genau an einer Wand:

```
WARN the runtime directory of an app could not be made, it stays off
     app=org.sorglos.terminal
     err="runtime: no system user for this container
          user: unknown user ngc-terminal-439982dd"
```

**Der Systembenutzer der App fehlt.** Er steht in keiner Sicherung, und beim Zurückspielen legt
ihn nichts an: `runtime.EnsureUser` und `runtime.EnsureData` werden von der **Einrichtung**
gerufen (`install/activate.go`) und von sonst nichts. `node.Restore` sucht den Benutzer und legt
keinen an. Ohne diesen Schritt bleibt nach einer Wiederherstellung **jede** App für immer aus,
und das Einzige im Produkt, das es richtigstellt, ist ein Admin, der die App neu aus dem Laden
einrichtet — eine Runde über das Netz, mit Lizenz und Paket, für zwei Zeilen, die in der
Sicherung schon dastanden.

### Das Stück, das die Wand wegnimmt

`tools/sicherung-zurueck` ruft für jede App-Kennung die beiden Funktionen des Produkts selbst —
nicht nachgebaut, sondern dieselben, damit die Antwort auf „was ist der Benutzer einer App"
nicht an zwei Stellen steht. Gelaufen auf 310:

```
org.sorglos.terminal     Benutzer ngc-terminal-439982dd (uid 999, gid 989)
org.sorglos.terminal     Datenordner /var/lib/ngc/apps/org.sorglos.terminal, verschluesselt, leer
org.sorglos.uhr          Benutzer ngc-uhr-ad8c00e2 (uid 995, gid 987)
org.sorglos.uhr          Datenordner /var/lib/ngc/apps/org.sorglos.uhr, verschluesselt, leer
sicherung-zurueck: ok                                        Ausstiegscode 0
```

**Und die Schlüsselkennungen stimmen.** Der Kern von 310 hält jetzt für die beiden Ordner
genau die Kennungen, die der Kern von svc-1 hält:

```
310:    060244942f4faf1dd8453e6ab359558e   a26b3623dc61f8e1a3693dc0667aaa32
svc-1:  060244942f4faf1dd8453e6ab359558e   a26b3623dc61f8e1a3693dc0667aaa32
```

Bitgleich, auf einer anderen Maschine, allein aus dem dbKey der Sicherung. Damit ist der Kreis
geschlossen: der Schlüssel der App-Daten reist in `controller.json` mit und nirgends sonst.

### Eine App lief wieder

Daten aus `app-daten.tar` in die vorbereiteten Ordner (110 MB und 89 MB, je unter einer
Sekunde), dann der Controller:

```
ngc-terminal-439982dd   localhost/ngc-terminal:1.0.0   Up About a minute
```

**Die Terminal-App läuft auf einer Maschine, die diese Anlage nie gesehen hat** — ohne API, ohne
Laden, ohne Admin, allein aus der Sicherung. Das ist der Weg zurück, und er geht.

### Was nicht zurückkam

Die Uhr nicht. Ihr Abbild ist da (`localhost/ngc/uhr:1.0.0`, 85,6 MB), ihre Daten sind da, ihr
Benutzer ist da — aber der Container startet nicht:

```
crun: mount `devpts` to `dev/pts`: Invalid argument
```

Die Container-Konfigurationen der beiden Apps sind an dieser Stelle **gleich** (beide mounten
`devpts` mit `newinstance`, beide ohne `terminal`), und auf svc-1 laufen beide. Es ist also keine
Eigenschaft der App. Was die beiden unterscheidet, ist ihr Alter: der Terminal-Container lief
beim Sichern seit drei Stunden, der Uhr-Container seit sieben Minuten — er war am 24.10. um 12:25
angelegt worden, meine Sicherung entstand 12:32. Der zurückgespielte Zustand trägt auch eine
kaputte Zeit (`Exited (0) 292 years ago`). Gegenprobe gefahren: mit angehaltenem
Terminal-Container startet die Uhr genauso nicht, es ist also kein Gedränge um `devpts`.

**Was daraus gemessen folgt:** der Datenordner einer App ist nicht nur ihre Daten, er ist auch
podmans Container-Lager. Ein `tar` davon im laufenden Betrieb ist für die **Daten** eine
Sicherung und für das **Container-Objekt** eine Momentaufnahme, die schiefgehen kann. Abbild und
Daten überlebten, das Container-Objekt der jüngeren App nicht.

**Was daraus geschlossen, nicht gemessen ist:** der Ausweg ist, den Container wegzuwerfen und
neu anlegen zu lassen. Das kann heute nur die Einrichtung (`install/activate.go`) — `node.Restore`
startet vorhandene Container und legt keine an. Also: Daten ja, Container womöglich nein, und
dann braucht es den Admin. Ein `podman rm` und ein Neuanlegen aus dem vorhandenen Abbild habe ich
**nicht** gefahren.

### Was weiter offen ist, und warum

**Eine neue Maschine in die Vertrauensliste bringen.** `pair` braucht einen Kopplungscode, den
die Oberfläche einem angemeldeten Admin zeigt; danach muss ein Admin den öffentlichen Schlüssel
listen. Ein Skript kann dieser Admin nicht sein, und wer es nachstellt, misst seine eigene
Vorrichtung — dieselbe Grenze, die `tools/erstlauf-verteilt.sh` in ihrem Kopf nennt. Die fünf
Schritte in Abschnitt 5 bleiben damit gelesen und nicht gefahren. Mit einer alten
`controller.json` habe ich bewusst **nicht** gekoppelt: das wäre svc-1 ein zweites Mal an
derselben API.

**Die MariaDB zurückspielen.** `mariabackup --prepare` und das Wiedereinspielen auf einem leeren
Knoten sind nicht gefahren. Der Grund ist nicht Vorsicht, sondern Reihenfolge: eine Sitzung hat
heute Nachmittag db-1 absichtlich abgeschaltet, und wie dieser Knoten von Hand zurückkommt
wird dort gemessen. Zweimal denselben Ausfall zu erzeugen wäre Verschwendung — ihre Messung
steht in Abschnitt 8, und sie fällt anders aus, als die Frage erwartet hat.

---

## 7. Das kleinste Stück, das den Weg schließt — und es ist gelaufen

`tools/sicherung.sh` sammelt von jeder Maschine die fünf Dinge ein, die niemand neu erzeugen
kann, je Rechner einen Ordner, mit `inhalt.txt` darin, das zu jeder Datei sagt, *was* sie ist
und *ohne was* sie wertlos ist.

Es baut die Wiederherstellung **nicht** — die braucht fast kein Programm, nur eine Reihenfolge
(Abschnitt 6). Es schließt die Lücke, die in Abschnitt 1 steht: dass heute der eine Teil
gesichert wird, den die Anlage ohnehin wiederbekäme, und kein einziger, den sie nicht
wiederbekäme. Das zweite Stück, `tools/sicherung-zurueck`, ist das eine Programm, das wirklich
fehlte — warum, steht in Abschnitt 6, und es ist aus einer Messung entstanden und nicht aus
einer Vermutung.

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

Und noch einmal um 14:05, nachdem das Skript seinen eigenen Transport zu prüfen gelernt hat
(Abschnitt 8): `sicherung: ok`, Ausstiegscode 0, **180 s** statt 25 — das ist der Preis der
Prüfung und er ist genannt, nicht versteckt.

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

## 8. Eine Sicherung, die niemand aufmacht, ist keine (gemessen)

Dieser Abschnitt steht hier, weil eine andere Sitzung ihn mir geschenkt hat, und er trifft
dieses Skript selbst.

**Der Befund ist ihrer.** Die Sitzung „Was passiert, wenn ein Rechner ausfällt" hat am
24.10.2026 einen logischen Dump der MariaDB gezogen und eine **47-KB-Datei** bekommen, die
wie eine Sicherung aussah und keine war: null Tabellen, kein Benutzer. Binärdaten über
ssh-stdout in eine Datei auf Windows werden verbogen; mit `scp` ging es. Es fiel nur auf, weil
jemand die Sicherung **aufgemacht** hat statt ihre Größe anzusehen.

**Und das ist genau der Weg, den `tools/sicherung.sh` benutzt**: jeder Teil kam über
`ssh … cat` als Binärstrom auf eine Windows-Platte. Dass im Lauf von 12:32 nichts verbogen
war, ist belegt — ich hatte die `sha256` von Hand verglichen und die Wiederherstellung in
Abschnitt 6 lief aus genau diesen Dateien. Aber „ich habe nachgesehen" ist keine Eigenschaft
eines Werkzeugs.

Also prüft das Skript sich jetzt selbst. Jeder Teil wird **auf** der Maschine geschrieben, dort
mit `sha256sum` gemessen, geholt und hier gegengerechnet. Stimmt es nicht, wird die Datei
**gelöscht** und die Lücke in `inhalt.txt` und im Ausstiegscode benannt — eine Sicherung, die
still falsch ist, ist schlimmer als eine, die fehlt.

Gemessen, in beide Richtungen, gegen eine 1-MB-Datei auf der Probemaschine:

| Fall | Antwort |
|---|---|
| unverändert geholt | `gleich` |
| ein einzelnes Byte angehängt | **greift**, Datei gelöscht, danach nicht mehr da |

Und der ganze Lauf noch einmal gegen alle fünf Maschinen, mit der Prüfung drin:

```
sicherung: ok          Ausstiegscode 0, Dauer 180 s
```

**Was es kostet:** 25 s vorher, 180 s jetzt. Der Preis ist, dass jeder Teil zweimal geschrieben
wird — einmal auf der Maschine, einmal hier — und dass ein `tar` nicht mehr durchgestreamt
wird. Ein Strom hat keine Prüfsumme, und ein laufendes Verzeichnis zweimal zu tarren gibt zwei
verschiedene Tars. 155 Sekunden für die Gewissheit, dass die Sicherung die Sicherung ist, sind
nach Thomas' Rangfolge keine Frage.

### Der Weg zurück von db-1, gemessen — von einer anderen Sitzung

Die Frage aus Abschnitt 6 („wie kommt ein einzelner Galera-Knoten von Hand zurück") hat eine
Antwort, die ich nicht erwartet habe: **es gibt keine Schrittfolge, er kam allein zurück.**

Gemessen von derselben Sitzung, db-1 aus von 13:46 bis 13:51 UTC, **nicht von mir**:

- Der Weg zurück war das Einschalten der Maschine und vier Sekunden warten: 13:47:16
  „Starting mariadb.service" bis 13:47:20 „ready for connections". Danach
  `wsrep_cluster_size 1`, Primary, über drei Minuten im Halbminutentakt nachgeprüft. **Kein
  einziger Befehl von Hand, kein `galera_new_cluster`.**
- Und es war ein harter Schnitt: die Journalspitze fehlt (letzter Eintrag 13:37:00, obwohl die
  Maschine 13:45:51 nachweislich noch antwortete).
- Die **Bedingung** ist kein Befehl, sondern eine Datei: `grastate.dat` mit
  `safe_to_bootstrap: 1`.

**Was daraus ausdrücklich NICHT folgt:** dass db-1 sich immer fängt. Der Stillstand von heute
früh (02:45 bis 09:30) hing an `safe_to_bootstrap: 0`, und was den Unterschied macht, ist
**offen**. Diesen Fall hat niemand erzeugt, und ich übernehme für ihn keine Zeilen — sonst
stände hier ein Weg, den niemand gegangen ist. Wer ihn braucht, stellt ihn selbst her.

### Eine Berichtigung an meinem eigenen Hinweis

Ich hatte der Sitzung geschrieben, eine MariaDB-Sicherung brauche `/etc/ngc/db-keys`. Das gilt
für `mariabackup`, das physische Werkzeug — es kopiert die verschlüsselten Tabellen über den
Dateisystemweg, und ohne die Schlüsseldateien ist das Geheimtext (Abschnitt 4). Für einen
**logischen** Dump gilt es nicht: der liest über die Datenbank entschlüsselt aus und braucht
die Dateien nicht. Dafür ist er die Form, die diese Anlage bewusst nicht schreibt, weil ein
zurückgespielter Dump Code ausführen kann (`register.go` sagt es an der Stelle, die keinen
macht).

---

## Was gemessen und was abgewogen ist

**Gemessen** (Lauf, Zahl, Ausstiegscode):

- Abschnitt 0 ganz — Rollen, Dienste und Ankerstände je Maschine am laufenden Dienst.
- Abschnitt 1 die Spalte „wo er liegt" und die Abwesenheit von `ngc-db`.
- Abschnitt 2 ganz — Zeilenzahlen, `mariabackup` 1 s / 49 MB, Datenbank danach weiter aktiv.
- Abschnitt 3 die Kommandoliste des Controllers.
- Abschnitt 4 ganz — vier Öffnungsversuche, vier dbKey-Fingerabdrücke, zwei fscrypt-Kennungen
  nachgerechnet, `grep` über neun `.ibd`, und die Berichtigung zum Klartext im `app-daten.tar`
  (1419 ELF-Köpfe gezählt).
- Abschnitt 6 fast ganz — die zurückgespielte Konfiguration und Datenbank, die Abweisung bei
  `apis: []`, die Wand am fehlenden Systembenutzer, der Lauf von `tools/sicherung-zurueck`, die
  bitgleichen fscrypt-Kennungen auf einer fremden Maschine, die **laufende Terminal-App**, und
  das Nichtanlaufen der Uhr samt Gegenprobe mit angehaltenem Nachbarn.
- Abschnitt 7 ganz, einschließlich des zweiten Laufs mit der Prüfung (180 s) und der
  Gegenprobe, dass sie bei einem einzigen Byte greift.
- Abschnitt 8 ganz — die beiden Fälle der Transportprüfung sind meine Messung; der
  47-KB-Dump und die Wiederkehr von db-1 in vier Sekunden sind die Messung der Sitzung „Was
  passiert, wenn ein Rechner ausfällt" und stehen hier mit ihr als Herkunft.

**Abgewogen** (aus Gemessenem geschlossen, nicht ausgeführt):

- Abschnitt 1 die Spalte „in der Sicherung?" für die Teile, die nachweislich nirgends
  eingesammelt werden — dass sie *fehlen* ist gemessen, dass sie beim Zurückspielen *gebraucht*
  werden ist für `/etc/ngc` und `state.db` inzwischen gemessen (Abschnitt 6) und für die
  MariaDB weiterhin geschlossen.
- Abschnitt 4 der Unterabschnitt zur Aufbewahrung.
- Abschnitt 5 die Schritte 2 und 3 — Kopplungscode und Eintrag in die Vertrauensliste. Die
  brauchen einen angemeldeten Admin in der Oberfläche; Schritt 1, 4 und 5 sind gefahren.
- Abschnitt 8 der Unterschied zwischen `safe_to_bootstrap: 1` und `0` — **nicht einmal
  abgewogen, sondern offen**: den zweiten Fall hat niemand erzeugt, und es stehen bewusst keine
  Zeilen dafür hier.
- Abschnitt 6 die Ursache des Uhr-Fehlers und der Ausweg daraus. Dass Abbild und Daten
  zurückkamen und der Container nicht, ist gemessen; **warum** gerade der jüngere Container
  nicht anläuft, ist die naheliegende Erklärung und kein Beweis, und ein `podman rm` mit
  Neuanlegen ist nicht gefahren.

**Nicht gemessen und nicht abgewogen, sondern eine Entscheidung für Thomas:**

1. Ob die dbKeys bei einer Neuinstallation **vorgegeben** werden dürfen. Ohne das kann
   „dieselbe Maschine wiederherstellen" nicht funktionieren — und mit `tools/sicherung-zurueck`
   ist jetzt gezeigt, dass alles andere daran hängt und sonst nichts.
2. Ob `tools/sicherung-zurueck` ein eigenes Werkzeug bleiben soll oder ob `node.Restore` den
   Benutzer und den Datenordner selbst anlegen soll. Das Zweite wäre die bessere Anlage und die
   größere Änderung: ein Controller, der beim Start Systembenutzer anlegt, tut beim Hochfahren
   mehr als heute, und das ist eine Entscheidung über die Form und nicht über eine Zahl.
3. Ob die beiden Code-Stellen auf `ngc-db backup` zeigen dürfen, solange es das nicht gibt.
4. Ob die Wegwerf-Maschine (VM 310 auf dem Proxmox, 10.2.3.220) stehen bleiben soll. Sie trägt
   im Moment eine Kopie der App-Daten von svc-1 im Klartext und die `controller.json` von svc-1
   — solange sie steht, ist sie so empfindlich wie svc-1 selbst.
