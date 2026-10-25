# Was passiert, wenn ein Rechner ausfällt

Sechs Maschinen, damit der Ausfall einer einzelnen die Anlage nicht mitnimmt.
Gemessen hat das bis zum 24.10.2026 niemand: gemessen war nur, dass alles
läuft, wenn alles läuft. Dieses Blatt schaltet jede Maschine einmal aus und
schreibt auf, **was der Benutzer dabei sieht**.

**Die Erwartungen in diesem Blatt standen hier, bevor die erste Maschine aus
war** — festgeschrieben mit dem Stand, der dieses Blatt anlegt; die Messspalten
kamen danach, in eigenen Ständen. Die Überraschungen stehen genau zwischen den
beiden Spalten.

## Die Anlage, am laufenden Dienst gemessen (24.10.2026)

Die Namen stimmen hier — aber erst die Messung sagt das. `systemctl` auf jedem
der sechs Knoten, `qm config` und `qm guest cmd <id> get-host-name` auf dem
Proxmox-Wirt 10.2.3.2:

| Adresse | Name | VMID | Dienste, die wirklich laufen |
|---|---|---|---|
| 10.2.3.210 | api-1 | 301 | `ngc-api`, `ngc-controller`, **`apache2` (:80, :443)**, `php8.4-fpm` |
| 10.2.3.211 | api-2 | 302 | `ngc-api` (nur :7443) |
| 10.2.3.212 | db-1 | 303 | `ngc-controller`, **`mariadb` (:3306, Galera :4567)** |
| 10.2.3.213 | svc-1 | 304 | `ngc-controller`, `podman` — trägt **Uhr** |
| 10.2.3.214 | svc-2 | 305 | `ngc-controller`, `podman` — trägt **Terminal** |
| 10.2.3.215 | store | 306 | nur `nginx` (:443), kein NGC |

Drei Dinge, die der Auftrag so nicht gesagt hat und die jede Erwartung unten
tragen — alle drei gemessen, nicht angenommen:

1. **Die VMIDs sind 301–306, nicht 210–215.** Das Anlegeskript
   `tools/proxmox-sechs.sh` nennt als Hauskonvention des Wirts „letztes Oktett
   = VMID"; die Anlage hält sie nicht. Wer nach dieser Konvention `qm stop 210`
   tippt, trifft eine VMID, die es nicht gibt — und `qm stop 200` wäre das NAS
   dieses Hauses. Die Zuordnung oben ist darum nicht gerechnet, sondern bei
   jedem Gast über den Gast-Agenten erfragt.
2. **Nur api-1 hat einen Apache.** api-2 lauscht ausschließlich auf :7443.
   `ngc.mein.lan` löst auf 10.2.3.210 auf, und auf *nichts* sonst. Der Eingang
   der Anlage ist einer, nicht zwei.
3. **Die Datenbank ist ein einzelner Galera-Knoten.**
   `wsrep_cluster_size` = 1, `wsrep_cluster_status` = Primary. Beide APIs
   tragen in `/etc/ngc/api.json` genau einen DB-Knoten: `db-1…:3306`.

Dazu die Lastverteilung, aus `/etc/ngc/apache-apis.conf` auf api-1:

```
<Proxy "balancer://ngc-api">
    BalancerMember "https://api-1.ngc.mein.lan:7443" retry=5 timeout=65 …
    BalancerMember "https://api-2.ngc.mein.lan:7443" retry=5 timeout=65 …
    ProxySet lbmethod=byrequests failontimeout=On
</Proxy>
```

Und der Stand der Apps zur Messzeit, aus `ngc_app` auf db-1: `org.sorglos.uhr`
auf **svc-1**, `org.sorglos.terminal` auf **svc-2**, beide aktiv. Jeder
Dienstknoten trägt genau eine App — das macht die beiden svc-Fälle
unterscheidbar.

## Was ich erwarte (geschrieben vor dem ersten Abschalten)

Fünf Fragen je Fall: **(a)** kommt man auf die Oberfläche, **(b)** kann man
sich anmelden, **(c)** sieht man seine Apps, **(d)** lässt sich eine App
öffnen, **(e)** kann man eine neue einrichten. Dazu die wichtigste:
**sagt die Anlage ehrlich, dass etwas fehlt?**

### store aus — erwartet

Der Laden gehört nicht zur Anlage; fällt er aus, darf die Anlage nur das nicht
können, was neu aus dem Laden kommt.

- (a)–(d) **unverändert.** Der Laden steht in keinem Pfad der Oberfläche, der
  Anmeldung oder einer laufenden App; die laufenden Apps sind Container auf den
  svc-Knoten und brauchen ihn nicht.
- (e) **fällt aus.** Eine neue App zu holen heißt, das Paket über
  `storeUrl` = `https://store.ngc.mein.lan` zu laden.
- Ehrlichkeit: **hier erwarte ich den Fehler.** Der Katalog liegt in der
  Datenbank zwischengespeichert; eine Oberfläche, die den Katalog aus dem
  Zwischenspeicher zeigt und erst beim Einrichten scheitert, verspricht etwas,
  das sie nicht halten kann. Mein Verdacht für den leeren Zustand, der wie ein
  Angriff aussieht: eine **leere Ladenliste ohne Grund** — nicht „der Laden ist
  nicht erreichbar", sondern „keine Apps", oder eine Signaturmeldung, die wie
  ein Fälschungsversuch klingt, obwohl nur niemand antwortet.
- Selbstfang: **ja**, der Katalog wird beim nächsten Nachsehen neu geholt.

### api-1 aus — erwartet

- (a) **nein, und zwar vollständig.** Der Apache läuft nur dort, und der Name
  zeigt nur dorthin. Was der Benutzer sieht, ist keine NGC-Seite, sondern der
  Verbindungsfehler des Browsers.
- (b)–(e) damit ebenfalls nein.
- Die Frage des Auftrags — *kommt man über api-2 noch hinein?* — erwarte ich
  mit **nein** beantwortet: api-2 ist Mitglied im Lastverteiler, aber kein
  Eingang. Solange api-1 den einzigen Apache und den einzigen Namen hält, ist
  der zweite API-Server für die Erreichbarkeit **Zierde**. Was er trägt, trägt
  er nur, solange api-1 lebt.
- Ehrlichkeit: ein Browser-Verbindungsfehler ist nicht unehrlich, aber er sagt
  dem Benutzer auch nicht, dass seine Daten noch da sind.
- Selbstfang: **ja**, Apache und Dienste starten beim Hochfahren.

### api-2 aus — erwartet

- (a)–(e) **im Ergebnis unverändert**, aber nicht sauber: `lbmethod=byrequests`
  verteilt abwechselnd, und bis der Apache das Mitglied als tot führt, läuft
  jede zweite Anfrage in `timeout=65`. Ich erwarte darum **jede zweite Anfrage
  langsam oder mit 503**, für bis zu eine Minute, und danach Ruhe.
- Das ist meine Erklärung für das „erst scheitert die Anmeldung, dann geht sie"
  aus dieser Woche, und hier wird sie prüfbar.
- Ehrlichkeit: ein 503 mitten in der Anmeldung sieht für den Benutzer wie sein
  eigener Fehler aus.
- Selbstfang: **ja**.

### db-1 aus — erwartet

Der schwerste erwartete Befund.

- (a) **ja** — die Oberfläche ist Apache und PHP auf api-1 und kommt.
- (b) **nein.** Beide APIs haben genau diesen einen DB-Knoten.
- (c)–(e) **nein**: Benutzer, Sitzungen und App-Liste stehen in dieser
  Datenbank.
- Ehrlichkeit: **hier erwarte ich den zweiten leeren Zustand.** Eine Anmeldung,
  die ohne Datenbank scheitert, hat denselben Ausgang wie eine Anmeldung mit
  falschem Kennwort. Wenn die Anlage dasselbe sagt, schiebt sie dem Benutzer
  einen Fehler zu, den er nicht gemacht hat. Das messe ich gezielt: dieselbe
  falsche Anmeldung einmal bei laufender und einmal bei toter Datenbank, und
  dann die beiden Antworten nebeneinander.
- Selbstfang: **nein, erwartet.** Ein einzelner Galera-Knoten kommt nach dem
  Hochfahren nicht von selbst zurück — er findet kein Quorum und braucht
  `galera_new_cluster` von Hand. Wenn das so ist, ist nicht nur der Ausfall ein
  Befund, sondern auch die Rückkehr.

### svc-1 aus — erwartet

- (a)–(c) **unverändert.**
- (d) **die Uhr nicht, das Terminal ja.** Die Uhr läuft als Container auf
  svc-1.
- (e) **ja, auf svc-2** — die Oberfläche lässt den Dienstknoten wählen.
- Ehrlichkeit: offen. Eine App, deren Knoten fehlt, kann als „geht nicht",
  als Zeitüberschreitung oder als leere Seite erscheinen. Der dritte Fall wäre
  der Befund.
- Selbstfang: **ja** für den Knoten; ob der Container von selbst wieder läuft,
  ist die eigentliche Frage.

### svc-2 aus — erwartet

Spiegelbild: das Terminal fällt, die Uhr bleibt. Mit einer Sorge: diese Woche
ist ein App-Container nach einer Neuinstallation **auf dem alten Knoten stehen
geblieben** (Punkt 74 der Verträge). Wenn ein Rest auf dem jeweils anderen
Knoten liegt, kann der Ausfall harmloser aussehen, als er ist.

### Nachtrag zu den beiden svc-Fällen, geschrieben vor dem Schalten

Diese vier Zeilen sind am 24.10.2026 gemessen, nachdem die Erwartungen oben
schon standen, und sie schärfen sie, ohne sie umzuschreiben:

| | svc-1 | svc-2 |
|---|---|---|
| App-Benutzer | `ngc-terminal-439982dd` **und** `ngc-uhr-ad8c00e2` | nur `ngc-terminal-439982dd` |
| `ngc-controller` | 0.2.2 | 0.2.1 |

**Punkt 74 ist von der Benutzerseite bestätigt**, bevor ich schalte: svc-1 trägt
den Benutzer *und* den Containerspeicher des Terminals, obwohl `ngc_app` das
Terminal auf svc-2 führt. Wenn also nach dem Abschalten von svc-2 ein Terminal
antwortet, ist das **dieser Befund und kein Ausfallschutz**. Genau darum steht
es hier vorher.

Und die **Fassungen stehen gemischt** (0.2.2 gegen 0.2.1, dazu db-1 auf 0.2.1).
Ein Ausfall gegen gemischte Fassungen misst zwei Dinge auf einmal; kommt einer
der beiden Knoten anders zurück als der andere, ist das zuerst eine Frage an die
Fassung und erst danach an den Ausfall.

Dazu eine **Erwartung einer Nachbarsitzung**, die ich übernehme, weil sie die
richtige Stelle trifft — als Erwartung, nicht als Behauptung, und von dort,
nicht von mir: nach einem harten Abschalten ist die interessante Frage nicht, ob
der Dienst wieder läuft, sondern **ob die Containerspeicher heil sind.** Was ich
dazu gemessen habe:

- `/var/lib/ngc/apps` ist auf beiden Knoten ein **eigenes Dateisystem**,
  `/dev/sdb`, ext4, `rw,relatime` — mit gesetztem `encrypt`-Merkmal, also
  fscrypt für die App-Verzeichnisse.
- Darin läuft `podman` **rootless mit `overlay`**, und seine Datenbank ist
  `db.sql` (SQLite) unter
  `/var/lib/ngc/apps/<app-id>/.local/share/containers/storage/`.

Ein Stromausfall mitten in einem Schreibvorgang trifft damit eine SQLite-Datei
und einen overlay-Baum auf einem eigenen ext4. Das ist die Stelle, an der ich
beim Wiederanlauf zuerst hinsehe — und die Frage ist nicht „läuft der
Controller", sondern „kann der App-Benutzer sein Abbild noch lesen".

Eine Zahl, die das Ausmessen abkürzt, **von der Lastsitzung gemessen**: im
Leerlauf steht `ngc-controller.service` bei `TasksCurrent=16` von
`TasksMax=3466`, ein leerer App-Container kostet rund 6 Aufgaben, eine offene
Terminal-Sitzung rund 9,4. Hängt nach dem Hochfahren etwas und `TasksCurrent`
steht weit unter 100, ist es **nicht** der Deckel.

## Wie hier gemessen wird — und warum der naheliegende Weg lügt

Das steht vor den Ergebnissen, weil es sie alle trägt. Zwei Fallen, beide am
24.10.2026 gemessen, beide hätten diesen ganzen Auftrag entwertet.

### Der DNS liefert für jeden Namen ein AAAA `::`

`nslookup <name> 10.2.3.102` von diesem PC, für alle sechs Namen einzeln: neben
dem richtigen A-Satz steht immer ein AAAA-Satz `::`. Alles, was über NSS geht —
`getent`, `curl`, `ping`, jedes Werkzeug, zu dem ein Mensch greift — nimmt das
`::` zuerst. Auf api-1 gemessen:

```
getent ahosts store.ngc.mein.lan   ->  10.2.3.215 STREAM …  und  :: STREAM
curl … https://store.ngc.mein.lan/catalog.json            ->  code=404 von=::
curl --resolve store.ngc.mein.lan:443:10.2.3.215 …        ->  code=200 von=10.2.3.215
```

Das `::` landet auf der eigenen Maschine, und dort antwortet auf 443 der Apache
von api-1. **Die naive Frage nach dem Laden wird also von api-1 selbst
beantwortet.** Wie teuer das ist, zeigt der Fall unten: dieselbe naive Zeile
sagt `404 von=::`, *egal ob der Laden läuft oder abgeschaltet ist*. Ein Mensch,
der so nachsieht, kann einen toten Laden nicht von einem lebenden
unterscheiden.

Von diesem PC aus beißt die Falle **nicht**: Windows-curl überspringt das `::`
und landet auf 10.2.3.210. Das ist der Grund, warum sie so lange unentdeckt
blieb — und der Grund, warum in diesem Blatt zu jeder Zahl steht, **von wo**
gemessen wurde. Dass NGC selbst läuft, liegt daran, dass Go die Adressen der
Reihe nach durchgeht und am `::` vorbeifällt. Die Anlage steht damit auf einem
Bein: die Software überlebt den kaputten Satz, der Mensch mit dem Werkzeug
nicht. (Den Hinweis auf diese Falle hat die Leitsitzung gegeben; die Zahlen
oben sind nachgemessen.)

### Der Laden ist von Windows aus nicht erreichbar

`store.crt` ist mit **ED25519** unterschrieben, Schlüssel ebenfalls ED25519.
Beide `curl` unter Windows (`/mingw64/bin/curl` und
`C:\WINDOWS\system32\curl`) brechen den Handshake ab:
`schannel: next InitializeSecurityContext failed: SEC_E_ILLEGAL_MESSAGE`. `-k`
hilft nicht, weil nicht die Prüfung scheitert, sondern der Handshake. Der Laden
wird in diesem Blatt darum **von api-1 aus** gefragt, immer mit `--resolve`.

Mein erster Lauf des Ladenfalls ist genau daran gestorben und war **kein
Ergebnis**: die Ladensonde sagte `code=000` schon *vor* dem Abschalten. Dass
das auffiel, liegt nur an der Vorher-Runde — eine Sonde, die erst im Ausfall
zum ersten Mal gefragt wird, hätte „der Laden ist weg" gemeldet und damit das
Instrument gemessen, nicht die Anlage.

### Was ein „aus" hier heißt

Geschaltet wird auf dem Proxmox-Wirt mit `qm stop <VMID>` / `qm start <VMID>` —
also Strom weg, nicht Dienst gestoppt und nicht Route geschwärzt. Das Messskript
glaubt das „aus" nicht, sondern prüft es: erst wenn `qm status` *stopped* sagt
**und** die Adresse stumm ist, wird gemessen; sonst bricht es ab und fährt die
Maschine wieder an. Und es trägt eine Liste der sechs erlaubten VMIDs, weil auf
demselben Wirt das NAS dieses Hauses (VMID 200), zwei Kameras und loxberry
laufen. Der Riegel ist geprüft: `fall.sh 200 …` bricht mit Code 2 ab, bevor
irgendetwas passiert.

## Was gemessen wurde

### store aus — gemessen am 24.10.2026, 14:42–14:43

| Frage | erwartet | gemessen |
|---|---|---|
| (a) Oberfläche | unverändert | **unverändert.** `code=200 von=10.2.3.210`, 10 von 10 Anfragen in Folge 200, Zeiten wie vorher (19–28 ms) |
| (b) anmelden | unverändert | **nicht gemessen** — ohne Kennwort für `weiricth` nicht zu messen |
| (c) Apps sehen | unverändert | **nicht gemessen** (dito) |
| (d) App öffnen | unverändert | **nicht gemessen** (dito) |
| (e) neu einrichten | fällt aus | **nicht gemessen** (dito) |
| Laden selbst | — | `200` → `000` → `200`; mit `--resolve` sauber unterscheidbar |
| sagt die Anlage es? | leere Ladenliste befürchtet | **sie sagt gar nichts, nirgends** — siehe unten |
| Selbstfang | ja | **ja.** 40 s nach `qm start` antwortet die Adresse, `nginx` läuft von selbst wieder, einziger Dienst auf der Maschine |

**Die Anlage fragt stündlich nach dem Laden — und schweigt darüber.**

Hier stand zuerst der Satz „sie merkt es nicht, weil sie nie fragt", gestützt
auf `systemctl list-timers` auf api-1, wo kein einziger NGC-Timer steht. Der
Satz war falsch, und meine Messung dazu war wertlos. Beides gehört
aufgeschrieben, weil der richtige Satz schlimmer ist als der falsche.

Der Takt ist kein systemd-Timer, sondern steckt im Programm.
`apps/server-api/classes/server/server.go`, `Server.rounds`:

```go
hour  := time.NewTicker(Hour)          // Hour = time.Hour
first := time.NewTimer(FirstCatalog)   // FirstCatalog = time.Minute
…
case <-first.C: s.once(func() { s.hour(ctx) })
case <-hour.C:  s.once(func() { s.hour(ctx) })
```

Eine Minute nach dem Start, danach jede Stunde. Ein fehlender systemd-Timer ist
also der **erwartete** Zustand, kein Befund. Und mein Journalbefund ist damit
keiner: mein Ausfallfenster war rund **40 Sekunden** lang, in denen überhaupt
kein Takt fällig war. `-- No entries --` beweist in diesem Fenster nichts. Der
richtige Beleg steht nicht im Journal, sondern im Code.

Dort steht er dafür doppelt. `Server.hour` *hat* eine Meldung:

```go
func (s *Server) hour(ctx context.Context) {
	if err := s.st.Refresh(ctx); err != nil {
		slog.Warn("catalogs could not be fetched", "error", err)
	}
}
```

Aber `Stores.Refresh` (`fetch.go:175`) gibt einen Fehler **nur** zurück, wenn
die Vertrauensliste nicht zu lesen war. Der Fehler des einzelnen Ladens geht in
`_ = st.note(ctx, s.ID, err)` und wird verworfen; am Ende steht `return nil`.
Ein unerreichbarer Laden liefert also `nil`, und die Warnzeile darüber ist für
genau diesen Fall **nicht erreichbar**. Der Grund landet still in der Zeile des
Ladens, sichtbar nur in `ngc.store/sources` — für den, der sich anmeldet und
dorthin klickt.

Dazu bleibt richtig: über die Befehlszeile gibt es kein Nachsehen.
`ngc-store --help` kennt nur Packen, Unterschreiben und Prüfen; `ngc-api --help`
und `ngc-controller --help` kennen keinen Katalogbefehl.

Der Befund ist damit: **die Anlage fragt jede Stunde, bekommt keine Antwort,
und sagt es niemandem.** Ein Laden, der eine Woche weg ist, hat 168 Mal
erfolglos geklopft, ohne eine Zeile zu hinterlassen, die ein Mensch findet,
ohne sie zu suchen. Das ist nicht gemessen worden, sondern am Code gelesen —
die Messung, die es zeigen würde, braucht ein Fenster von über einer Stunde und
steht unten unter „was offen ist".

Damit ist der schwere Befund, den der Auftrag befürchtet hat, **nicht**
eingetreten: die Anlage arbeitet ohne ihren Laden weiter, die Oberfläche steht,
der Laden steht in keinem Pfad, der ohne ihn gebraucht wird. Der Befund ist ein
anderer und leiser: **ein Laden, der eine Woche weg ist, fällt niemandem auf.**
Keine Zeile im Journal, kein Timer, der stolpert, keine Meldung. Es merkt nur,
wer sich anmeldet und hinsieht — und was der dann sieht, ist die offene Hälfte
dieses Falls.

Was die Erstlauf-Sitzung mit einer geschwärzten Route gemessen hat (die Anlage
behält den letzten guten Katalog, schreibt den Fehler in die Zeile des Ladens,
aber nichts ins Journal), deckt sich damit und erklärt dieselbe Stelle von der
anderen Seite. Zwei Dinge daran sind **abgewogen, nicht gemessen**: dass ein
Zeitablauf nirgends hängt, habe ich nur an den Antwortzeiten der Oberfläche
gesehen (keine Verlängerung, 19–28 ms) — nicht an dem Pfad, der wirklich zum
Laden greift.

### Nachtrag: der erste Ladenfall bewies die eigentliche Frage nicht

Nach dem Lauf habe ich an der Datenbank nachgesehen, was der Ausfall gekostet
hat — und damit meinen eigenen Befund entwertet. Die Zeile des Ladens stand
nachher auf `{"checked":1792845105}`, also **12:31:45 UTC**, elf Minuten *vor*
meinem Ausfall (12:42:11–12:43:00), **ohne Fehlerfeld**. In das Fenster von 40
Sekunden fiel überhaupt keine Katalogrunde. Katalog unverändert Folge 7
(1116 Byte), Vertrauensliste unverändert Folge 6, beide Apps aktiv.

Die Anlage hat also nichts verloren — aber nur, weil sie in diesem Fenster
nichts versucht hat. „Der Laden darf ausfallen" war damit noch nicht gemessen,
sondern nur „40 Sekunden ohne Laden kosten nichts". Das ist ein deutlich
schwächerer Satz.

### Also noch einmal: eine Runde, die wirklich auf den toten Laden trifft

`FirstCatalog = time.Minute` erlaubt, die Runde zu erzwingen, statt eine Stunde
zu warten: Laden aus, `ngc-api` neu starten, Runde abwarten. Gemessen am
24.10.2026, 13:01–13:05 UTC, auf **api-2** — nicht auf api-1, weil api-1 der
einzige Eingang ist. **Die Runde ist erzwungen, nicht von allein gekommen**; das
gehört zu jeder Zahl hier dazu.

| | vorher | Runde gegen den toten Laden | nach dem Einschalten |
|---|---|---|---|
| Zeile des Ladens | `{"checked":1792845105}` | `{"checked":1792845105,`<br>`"error":"Der Store hat nicht oder nicht brauchbar geantwortet."}` | `{"checked":1792847065}` |
| Katalog | Folge 7, 1116 B | **Folge 7, 1116 B** | Folge 7, 1116 B |
| Vertrauensliste | Folge 6 | **Folge 6** | Folge 6 |
| Apps | beide aktiv | **beide aktiv** | beide aktiv |
| Journal auf api-2 | — | **0 Zeilen** zu Katalog, Store oder Laden | — |

Das Skript hat sich dabei an drei Stellen selbst geprüft und hätte sonst
abgebrochen: der Laden muss nachweisbar aus sein, `ngc-api` muss eine **neue
PID** haben (2829 → 3351 → 3464), und die Zeile des Ladens muss sich bewegt
haben, bevor sie gelesen wird (nach 70 s bzw. 60 s).

Damit ist die Frage beantwortet, und zwar gut für die Anlage:

- **Der schwere Befund ist nicht eingetreten.** Eine Runde, die auf einen toten
  Laden trifft, kostet genau diese Runde. Der Katalog bleibt Byte für Byte
  stehen, der Laden verliert sein Vertrauen nicht, keine App verschwindet.
- **Die Anlage fängt sich von selbst.** Laden wieder an, nächste Runde, und der
  Fehlertext ist weg und `checked` neu — ohne eine Hand. Dass der Fehlertext
  stehenbleibt, bis jemand hinsieht, war die Sorge; sie trifft nicht zu.
- **Im Journal steht weiter nichts**, jetzt nicht mehr nur aus dem Code
  geschlossen, sondern an einer Runde gemessen, die wirklich gescheitert ist:
  null Zeilen, die Katalog, Store oder Laden erwähnen.

Und ein Fund, der mir selbst erst in der Tabelle auffiel: **`checked` rückt beim
Fehlschlag nicht vor.** Der Wert bleibt auf dem letzten *erfolgreichen* Blick
stehen, der Fehler wird nur angehängt. Das ist nicht nur schlecht — es heißt,
dass die Zeile das **Alter des letzten guten Blicks** trägt, und wer hinsieht,
kann daran erkennen, dass der Laden seit einer Woche nicht mehr geantwortet hat.
Was fehlt, ist nicht die Auskunft, sondern der Anlass, sie zu holen: kein
Journal, kein Befehl, kein Hinweis in der Oberfläche außerhalb von
`ngc.store/sources`.

Der Laden ist damit wirklich kein Teil der Anlage, und sie behandelt ihn auch
nicht wie einen. **Dieser Fall ist abgeschlossen** — bis auf die Spalte „was
der Benutzer dabei sieht", für die ein Zugang fehlt.

Ein Hinweis einer Nachbarsitzung, der die Wahl von api-2 im Nachhinein stützt
und hier als **deren** Messung steht: `ngc-api` und `ngc-controller` teilen auf
einem Knoten dasselbe Laufzeitverzeichnis unter `/run/ngc` mit `Preserve=no`,
und ein Neustart der API nimmt dort die Laufzeitverzeichnisse der Apps mit. Auf
api-1, wo beide Dienste laufen, wäre derselbe Neustart darum nicht harmlos
gewesen; api-2 trägt nur die API.

### api-1 aus — gemessen am 24.10.2026, 13:32–13:34 UTC

Vor dem Schalten gemessen, und schon das ist die halbe Antwort: **alle drei
Controller nennen in `/etc/ngc/controller.json` genau einen Tunnel,
`wss://api-1.ngc.mein.lan:7443/tunnel`.** db-1, svc-1 und svc-2 kennen api-2
nicht. Nur `web.json` auf api-1 nennt beide APIs.

| Frage | erwartet | gemessen |
|---|---|---|
| (a) Oberfläche | nein, vollständig | **nein.** `ngc.mein.lan` und api-1:443 antworten gar nicht; api-2:443 antwortet nicht, vorher wie nachher — dort liegt kein Web |
| (b)–(e) | nein | **nein**, es gibt keine Seite, die sie anbieten könnte |
| API-Pfad auf api-2 | — | **lebt, unverändert.** `POST /api/v1/login/start` mit `{}` gibt 400, genau wie im vollen Betrieb und genau wie api-1 sonst |
| Datenbank | — | `mariadb` auf db-1 die ganze Zeit `active` |
| Controller | — | **alle drei verlieren den Tunnel und versuchen es ewig bei api-1.** `0` Treffer auf api-2 in allen drei Journalen |
| laufende Apps | — | **laufen weiter.** `ngc-terminal-439982dd` und `ngc-uhr-ad8c00e2` auf svc-1, `ngc-terminal-439982dd` auf svc-2 — unverändert über den ganzen Ausfall |
| Ehrlichkeit | Browserfehler, mehr nicht | **nach innen ehrlich, nach außen stumm** — siehe unten |
| Selbstfang | ja | **ja, und gut.** Oberfläche wieder 200 **vier Sekunden** nachdem die Adresse antwortete; alle drei Controller hängen sich von selbst wieder ein (13:34:02, 13:34:03, 13:34:04). Keine Hand nötig |

**Die Antwort auf die Frage des Auftrags.** „Ist der zweite API-Server etwas
wert oder nur Zierde?" — Zierde ist zu hart und zu ungenau. Gemessen ist:
api-2 **arbeitet durchgehend**, er beantwortet die ganze Zeit dieselben
Anfragen wie api-1 im Normalbetrieb. Nur kommt niemand zu ihm:

- der Apache, der ihn aufrufen würde, lief auf api-1,
- der Name `ngc.mein.lan` zeigt nur auf api-1,
- und **kein einziger Controller hat ihn in seiner Liste.**

api-2 ist also kein Zierstück, sondern ein **Ersatzteil, an das nichts
angeschlossen ist**. Der Unterschied ist wichtig, weil er sagt, was zu tun
wäre: nicht „api-2 reparieren", sondern drei Verdrahtungen nachziehen — Web
auch auf api-2, ein Name, der auf beide zeigt, und beide APIs in jeder
`controller.json`. Dass `web.json` beide schon kennt, zeigt, dass die Bauart
darauf angelegt ist; nur die Anlage ist nicht so verdrahtet.

**Zur Ehrlichkeit, und hier ist sie zweigeteilt.** Nach innen sagt die Anlage
die Wahrheit, wörtlich und ohne Beschönigung:

```
WARN tunnel failed api=wss://api-1.ngc.mein.lan:7443/tunnel err="link: the API cannot be reached"
INFO status stayed on this server apps=2 err="node: this controller has no tunnel to an API"
```

Das ist genau richtig: es nennt die Ursache, es nennt die Folge („apps=2
bleiben hier"), und es schiebt nichts auf den Benutzer. Nach außen aber sieht
der Benutzer **nur den Verbindungsfehler seines Browsers**. Dass seine Daten
unangetastet sind, dass die Datenbank läuft, dass seine beiden Apps die ganze
Zeit weiterlaufen — davon erfährt er nichts, weil die Stelle, die es ihm sagen
könnte, dieselbe Maschine ist, die fehlt. Das ist kein leerer Zustand, der wie
ein Angriff aussieht; es ist ein Totalausfall, der wie ein Netzproblem
aussieht, und immerhin ist er das auch.

Der beruhigendste Einzelbefund dieses Falls: **die Apps laufen weiter, während
niemand sie sehen kann.** Wer ein Terminal offen hatte, verliert die Seite,
aber nicht den Behälter.

Eine Messnotiz zum Lesen der Tabelle: meine Sonde schreibt bei „keine Antwort"
`000000` (die Null von `curl` und die des Rückfalls hintereinander). Und die
Container-Sonde musste ich korrigieren: über `su` gefragt meldete rootloses
`podman` „nichts", weil es als `su`-Kind sein Laufzeitverzeichnis nicht findet
(`mkdir /run/user/0/libpod: permission denied`) — die Container liefen die ganze
Zeit. Gemessen wird darum von root aus an den `conmon`-Prozessen, die den Namen
des Behälters hinter `-n` tragen. Eine Sonde, die „nichts" sagt, weil sie selbst
nicht darf, ist die gefährlichste Sorte.

### db-1 aus — gemessen am 24.10.2026, 13:46–13:51 UTC

Der Fall, bei dem ich mit dem schwersten Befund gerechnet habe. **Zwei von drei
Erwartungen waren falsch, und beide in die gute Richtung.**

| Frage | erwartet | gemessen |
|---|---|---|
| (a) Oberfläche | ja | **ja**, `200`, unverändert — Apache und PHP liegen auf api-1 |
| (b) anmelden | nein | **nein** — und zwar so: |
| | | `{"status":"error","code":"E_UNREACHABLE","message":"Dienst nicht erreichbar."}` mit **HTTP 503**, gleichlautend über den Lastverteiler *und* direkt auf api-2:7443 |
| (c)–(e) | nein | **nein**, alles hängt an dieser Datenbank |
| laufende Apps | — | **laufen weiter**, beide Container auf svc-1, einer auf svc-2, unverändert |
| sagt die Anlage es ehrlich? | **nein erwartet** | **ja, sie sagt es ehrlich** |
| Selbstfang | **nein erwartet** | **ja, ganz allein** |

**Die erste falsche Erwartung: die Anlage schiebt dem Benutzer nichts unter.**
Ich hatte erwartet, dass eine Anmeldung ohne Datenbank denselben Ausgang nimmt
wie eine mit falschem Kennwort — der leere Zustand, der wie ein eigener Fehler
aussieht. Das ist nicht so, und es ist nicht Zufall, sondern gebaut. Im Code
ist der erste Zugriff in `login/start` ein `lock.Check` gegen die Datenbank,
und sein Fehler wird ausdrücklich als `E_UNREACHABLE` beantwortet. Die
Oberfläche hat dafür einen **eigenen Satz**, getrennt vom Satz für eine
gescheiterte Anmeldung:

| Fall | Text, den der Benutzer sieht |
|---|---|
| Datenbank weg | `err.unreachable` — „Service unavailable. Please try again later." |
| falsches Kennwort | `login.failed` — „Login failed." |

Die API liefert dazu „Dienst nicht erreichbar." auf Deutsch. Der Benutzer
erfährt also, dass es nicht an ihm liegt. Das ist genau das Gegenteil des
Befunds, den der Auftrag gesucht hat, und gehört aufgeschrieben, weil die drei
leeren Zustände dieser Woche den Verdacht in die andere Richtung gelegt hatten.

Dazu kommt ein **Gegensatz zum Ladenfall, der ins Auge springt.** Die
unerreichbare Datenbank bekommt eine Journalzeile, mit Zähler:

```
api-2  WARN the database could not be reached reason=db-unreachable count=1 since=1
```

Der unerreichbare Laden bekommt **keine**. Dieselbe Anlage, zwei fehlende
Nachbarn, zweimal eine andere Lautstärke. Das ist der Beleg dafür, dass das
Schweigen im Ladenfall kein Hausstil ist, sondern eine Lücke an einer Stelle.

**Die zweite falsche Erwartung: db-1 kommt von selbst zurück.** Ich hatte
erwartet, dass ein einzelner Galera-Knoten kein Quorum findet und
`galera_new_cluster` von Hand braucht — heute früh lag die Datenbank aus genau
diesem Grund von 02:45 bis 09:30 tot. **Ich habe nicht von Hand geholfen, und
es war nicht nötig.** Gemessen im halbminütigen Takt über drei Minuten: schon
beim ersten Blick `active`, `wsrep_cluster_size 1`, `wsrep_cluster_status
Primary`. Aus dem Journal:

```
13:47:16  Starting mariadb.service - MariaDB 11.8.6 database server...
13:47:20  WSREP: Recovered cluster id dc0a900c-cf30-11f1-8342-b316e8ae0db8
13:47:20  WSREP: Server db-1 synced with group
13:47:20  WSREP: Synchronized with group, ready for connections
```

**Vier Sekunden** vom Start des Dienstes bis „ready for connections". Der Grund
steht in `grastate.dat`: `safe_to_bootstrap: 1`. Ein Knoten, der weiß, dass er
der letzte war, darf sich selbst hochziehen.

**Und das war ein harter Schnitt, kein sauberes Herunterfahren.** `qm stop` ist
in Proxmox der Stecker, nicht das ACPI-Signal, und zwei Spuren belegen, dass es
auch so ankam: im vorigen Start steht **keine Herunterfahr-Folge** zur Uhrzeit
des Abschaltens, und die Journalspitze fehlt — der letzte Eintrag ist 13:37:00,
obwohl die Maschine um 13:45:51 nachweislich geantwortet hat. Alles dazwischen
war noch nicht auf die Platte geschrieben und ist mit dem Strom weg.

**Was damit NICHT gemessen ist, und das ist die wichtige Einschränkung.** Dass
dieser harte Schnitt heil überstanden wurde, heißt nicht, dass der Fall von
heute früh nicht existiert. `grastate.dat` trug hier `safe_to_bootstrap: 1`;
der bekannte Stillstand tritt ein, wenn dort `0` steht, weil der Knoten nicht
weiß, ob er der letzte war. Die Startliste von db-1 zeigt, dass der
Vorfall von heute früh an einem anderen Neustart hing (Start endet 02:44:50,
nächster beginnt 02:45:03). **Was den Unterschied macht, ist offen** — und das
ist der nächste Fall, der gemessen werden müsste, nicht der, den ich gefahren
habe. Ein Satz wie „db-1 fängt sich immer selbst" wäre aus dieser Messung
nicht gedeckt.

Vor dem Lauf lag eine **logische Sicherung** der Datenbank bereit
(`mariadb-dump --single-transaction`, 155 KB, 9 Tabellen, Abschlusszeile
geprüft), außerhalb von db-1. Sie war nicht nötig. Zwei Dinge daran sind
trotzdem festzuhalten: der Dump braucht die Schlüsseldateien unter
`/etc/ngc/db-keys` **nicht**, weil er entschlüsselt ausliest — und mein erster
Versuch erzeugte eine 47-KB-Datei, die wie eine Sicherung aussah und keine war
(0 Tabellen, kein Benutzer): Binärdaten über `ssh`-stdout in eine Windows-Datei
werden verbogen, mit `scp` geht es. Wer einen Rückweg baut, **macht die
Sicherung auf**, statt ihre Größe anzusehen.

### api-2 aus — gemessen am 24.10.2026, 13:57–14:07 UTC

Der Fall, bei dem ich die Zahl erst nach drei Anläufen hatte, und das ist der
lehrreiche Teil.

**Erster Anlauf:** zwanzig Anfragen auf die Startseite, vor und während des
Ausfalls, **20 von 20 mal 200**. Kein Unterschied. Meine Erwartung („jede zweite
Anfrage hängt oder gibt 503") sah widerlegt aus.

**Dann fiel mir auf**, dass beim Wiederanfahren eine Anmeldeanfrage eine
Apache-Fehlerseite zurückbrachte (`<!DOCTYPE HTML PUBLIC …`). Der Grund für den
scheinbar sauberen ersten Anlauf: **`GET /` geht gar nicht durch den
Lastverteiler.** Die Seite kommt von PHP auf api-1; nur `/api/` ist
weitergeleitet. Ich hatte die falsche Anfrage gezählt.

**Dritter Anlauf**, und diesmal getrennt gezählt, je vierzig Anfragen:

| | `POST /api/v1/login/start` (über den Verteiler) | `GET /` (nicht über den Verteiler) |
|---|---|---|
| beide APIs oben | **40/40 gut** | 40/40 gut |
| api-2 nachweisbar aus | **31/40 gut, 9 schlecht** — 6× HTTP 500, 3× keine Antwort | **40/40 gut** |

Damit ist es gemessen: **api-2 fehlt unsichtbar auf der Oberfläche und
schmerzhaft auf jedem API-Weg.** Ungefähr jede vierte Anmeldung scheitert,
solange er weg ist. Für den Benutzer heißt das genau das, was diese Woche
berichtet wurde: *erst scheitert die Anmeldung, dann geht sie.* Hier ist die
Erklärung dafür, und sie ist kein Zufall und keine Laune.

Zur Ehrlichkeit ist das der **schlechteste Fall der sechs**. Der Benutzer
bekommt **HTTP 500** — „internal server error", also die Meldung, die nach einem
Fehler im Programm klingt, nicht nach einer fehlenden Maschine. Und in
`/var/log/apache2/error.log` steht dazu **keine Zeile**: `0` Treffer auf
`proxy` oder `balancer` in der ganzen Datei. Nur das Zugriffsprotokoll trägt die
500er und 502er, und die liegen minutengenau in meinen Fenstern (13:59–14:01 und
14:05–14:07, dazu 09:25–09:26 mit 503ern von heute früh, die nicht von mir sind).

**Und hier beißt die DNS-Falle aus Punkt 80 nicht das Messwerkzeug, sondern die
Anlage selbst.** Auf api-1 gemessen:

```
getent ahosts api-2.ngc.mein.lan   ->  10.2.3.211 STREAM …   und   :: STREAM
```

Der Lastverteiler trägt das Mitglied als Namen (`BalancerMember
"https://api-2.ngc.mein.lan:7443"`). Ist die echte Adresse tot, bleibt die
zweite — `::` — und die führt auf die **eigene** Maschine. Im Journal von
`ngc-api` auf api-1 steht dann:

```
http: TLS handshake error from [::1]:34332: EOF
```

Apache hat sich zu sich selbst verbunden und ist am Namensabgleich gescheitert
(`SSLProxyCheckPeerName on`). **Was ich dazu gemessen habe und was nicht:** diese
Zeile kam **einmal** in den neun Fehlschlägen vor. Dass das `::` der Weg *aller*
neun ist, ist damit **nicht** gemessen — aber dass es im Anfrageweg der Anlage
liegt und nicht nur in meiner Sonde, ist belegt. Ohne den `::`-Satz hätte Apache
nur ein totes Mitglied zu überspringen; mit ihm hat es ein zweites, das
antwortet und dann den Abgleich verweigert.

Selbstfang: **ja.** Im Sekundentakt über 90 Sekunden nach dem Einschalten
gemessen: **1 schlechte Antwort von 90**, und die gleich am Anfang. `ngc-api`
auf api-2 war 14:01:21 bereit.

### svc-1 aus und svc-2 aus — gemessen am 24.10.2026, 14:08–14:15 UTC

Die beiden ruhigsten Fälle, und der Wiederanlauf ist der beste der ganzen Reihe.

| Frage | svc-1 aus | svc-2 aus |
|---|---|---|
| (a) Oberfläche | 20/20 mal 200 | 20/20 mal 200 |
| (b) Anmeldung beginnt | geht, unverändert | geht, unverändert |
| Container auf dem anderen Knoten | `ngc-terminal-439982dd` läuft weiter | beide auf svc-1 laufen weiter |
| Controller des anderen Knotens | `active` | `active` |
| api-1 sagt | nichts im Fenster | `INFO controller tunnel ended node=svc-2 seconds=2370` |
| App-Speicher nach dem harten Schnitt | `terminal=ok uhr=ok` | `terminal=ok` |
| Container von selbst zurück | **ja, nach 1 s** | **ja, nach 1 s** |
| `TasksCurrent/Max` nachher | 24 / 3466 | 16 / 3466 |

Der Wiederanlauf ist in den Journalen Schritt für Schritt zu lesen, und er
braucht eine Sekunde:

```
14:09:58  Started ngc-controller.service
14:09:59  system refresh
14:09:59  container init / container start   ngc-terminal-439982dd
14:09:59  container init / container start   ngc-uhr-ad8c00e2
14:09:59  INFO tunnel up api=wss://api-1...
```

**Die Sorge der Nachbarsitzung trifft nicht zu:** rootloses `podman` mit
`overlay` und einer SQLite-`db.sql` auf einem eigenen ext4 übersteht den harten
Schnitt. `pragma integrity_check` sagt vorher und nachher `ok`, auf beiden
Knoten, für jede App. Und der Deckel ist weit weg: 24 von 3466 Aufgaben.

Zwei Dinge, die erst beim Hinsehen harmlos wurden:

- **Der Gerätename der App-Platte wechselte** über den Neustart, auf svc-1 von
  `/dev/sdb` nach `/dev/sda`. Das sieht nach einem Problem aus und ist keines:
  `/etc/fstab` hängt sie per `LABEL=ngc-apps` ein, die Wurzel per `PARTUUID`.
  svc-1 und svc-2 haben ihre Platten gerade spiegelbildlich benannt, und beide
  hängen richtig. Hier ist etwas **richtig gebaut**, und der Neustart hat es
  bewiesen.
- **Punkt 74 ist von der Benutzerseite bestätigt**: svc-1 trägt
  `ngc-terminal-439982dd` *und* `ngc-uhr-ad8c00e2`, obwohl `ngc_app` das
  Terminal auf svc-2 führt — und der Controller wirft den Fremdling beim
  Wiederanlauf **von selbst wieder an** (`container start … ngc-terminal` auf
  svc-1, 14:09:59). Dass nach dem Abschalten von svc-2 ein Terminal antwortet,
  ist also dieser Befund und kein Ausfallschutz. Der Ausfall überlebt die
  Doppelung nicht nur, er festigt sie.

## Alle sechs auf einem Blatt

Jede Maschine einmal mit `qm stop` ausgeschaltet und wieder angefahren,
24.10.2026 zwischen 12:42 und 14:15 UTC, von diesem PC aus gemessen.

| aus | Oberfläche | Anmeldung beginnt | laufende Apps | sagt es die Anlage? | fängt sich selbst |
|---|---|---|---|---|---|
| **store** | unberührt | unberührt | laufen | **nein, nirgends** | ja, 40 s |
| **api-1** | **ganz weg** | ganz weg | **laufen weiter** | innen ja, außen stumm | ja, 4 s nach der Adresse |
| **api-2** | unberührt | **jede vierte scheitert** | laufen | **nein: HTTP 500** | ja, 1 von 90 |
| **db-1** | unberührt | **nein, aber ehrlich** | laufen weiter | **ja, mit Zähler** | **ja, allein, 4 s** |
| **svc-1** | unberührt | unberührt | die des anderen laufen | — | ja, Container nach 1 s |
| **svc-2** | unberührt | unberührt | die des anderen laufen | — | ja, Container nach 1 s |

**Der Sinn der sechs Maschinen ist erfüllt — mit einer Ausnahme.** Fünf von
sechs Ausfällen nimmt die Anlage nicht mit, und alle sechs fängt sie ohne eine
Hand von Hand. Die Ausnahme ist **api-1**: solange der einzige Apache, der
einzige Name und der einzige Tunnel aller Controller dort liegen, ist die
Anlage für ihre Benutzer eine Ein-Maschinen-Anlage mit fünf Gehilfen.

**Die Reihenfolge der Ehrlichkeit, von gut nach schlecht**, und sie folgt nicht
der Schwere des Ausfalls:

1. **db-1** — eigener Fehlercode, eigener Satz in der Oberfläche, Journalzeile
   mit Zähler. Vorbildlich, und der schwerste Ausfall der sechs.
2. **api-1** — nach innen wörtlich und schuldfrei („this controller has no
   tunnel to an API"), nach außen nur der Verbindungsfehler des Browsers. Mehr
   ist von einer fehlenden Maschine auch nicht zu erwarten.
3. **store** — die Auskunft existiert in der Zeile des Ladens, aber nichts
   weist darauf hin. Stille.
4. **api-2** — **HTTP 500 und keine Zeile im Fehlerprotokoll.** Der Benutzer
   bekommt die Meldung, die nach einem Fehler im Programm klingt, für eine
   fehlende Maschine. Der schlechteste der sechs, obwohl der harmloseste
   Ausfall.

## Nachtrag vom selben Tag: die Anlage hat sich unter dem Blatt verändert

Noch am 24.10.2026, nach Abschluss der sechs Fälle, hat Thomas drei
Datenbankknoten in Auftrag gegeben. **Alles oben beschreibt damit die Anlage mit
einem einzelnen Datenbankknoten**, und besonders der db-1-Fall ist so zu lesen:
er ist richtig für die Anlage, an der er gemessen wurde, und er ist keine
Eigenschaft von NGC.

Stand zum Zeitpunkt dieses Nachtrags, von mir gemessen:

```
qm list        308 ngc-db-2 running,  309 ngc-db-3 running
db-1           wsrep_cluster_size 1,  wsrep_incoming_addresses 10.2.3.212:0
db-2, db-3     mariadb INACTIVE  (ein beitretender Knoten wartet auf den Verbund)
api-1, api-2   /etc/ngc/api.json  ->  db.nodes = ['db-1.ngc.mein.lan:3306']
```

**Und hier steht eine Erwartung, bevor jemand den Umbau für fertig erklärt:**
`db.nodes` ist eine Liste, die Bauart ist auf mehrere angelegt — aber beide APIs
tragen darin genau einen Eintrag. Solange das so bleibt, ändern drei Knoten am
Ausfall von db-1 **nichts**: beide APIs greifen auf denselben Namen, und der
zeigt auf die Maschine, die fehlt. Der Verbund hält dann die Daten, und niemand
fragt die anderen zwei.

Das ist dieselbe Gestalt wie api-2 in diesem Blatt — ein Ersatzteil, an das
nichts angeschlossen ist. Endet der Umbau hier, hat die Anlage zwei davon, und
das Ergebnis wäre **schlechter** als der gemessene Zustand von 13:46: derselbe
Vollausfall, aber mit zwei laufenden Reservemaschinen daneben, die wie
Redundanz aussehen.

Zwei Dinge, die für die nächste Messung gelten und die ich von der
Koordinationssitzung habe, **nicht gemessen**: die Firewall-Rolle „Datenbank"
lässt 3306 nur von den beiden APIs zu, wer also von einem svc-Knoten aus gegen
die Datenbank misst, misst die Firewall und nicht den Ausfall — und ein
beitretender Knoten wartet, db-1 muss zuerst stehen. Damit sind es **zwei**
Fälle und nicht einer: „der Erste fällt" und „ein Beitretender fällt".

### Der Umbau ist fertig, und die Warnung hat sich erledigt

Wenig später gemessen, und beides hat sich geändert:

```
db-1   wsrep_cluster_size=3  wsrep_cluster_status=Primary
       incoming = 10.2.3.212:0, db-3.ngc.mein.lan:0, db-2.ngc.mein.lan:0
db-2, db-3   mariadb active
api-1 und api-2   db.nodes = ['db-1...:3306', 'db-2...:3306', 'db-3...:3306']
```

**`db.nodes` nennt jetzt auf beiden APIs alle drei.** Die Gestalt „Ersatzteil,
an das nichts angeschlossen ist" ist hier damit nicht eingetreten — anders als
bei api-2, wo sie steht. Erst damit ist der Fall überhaupt messbar: vorher
wusste ich das Ergebnis, jetzt nicht.

### Was ich für die zwei neuen Fälle erwarte (geschrieben vor dem Abschalten)

Thomas hat 308 und 309 auf die Liste genommen; der Riegel ist neu geprüft (200
und 307 brechen weiter mit Code 2 ab, ohne Pipe gemessen).

**„Ein Beitretender fällt" — db-2 oder db-3 aus.** Erwartung: **kein sichtbarer
Unterschied.** Zwei von drei Knoten halten das Quorum (2 > 3/2), beide APIs
haben zwei gesunde Einträge in `db.nodes`, und nichts zeigt allein auf den
fehlenden. Oberfläche 200, Anmeldung geht, `wsrep_cluster_size` fällt auf 2.
Die Sorge liegt nicht beim Ausfall, sondern bei der Rückkehr: ein beitretender
Knoten **wartet** auf den Verbund, und der Verbund ist dann ja da — also
erwarte ich, dass er von selbst zurückkommt, schneller als db-1 heute.

**„Der Erste fällt" — db-1 aus, mit drei Knoten.** Das ist der Fall, für den der
Umbau gemacht wurde, und meine Erwartung ist **jetzt die umgekehrte von heute
Mittag**: die Anmeldung soll **weiterlaufen**. Zwei Knoten halten das Quorum,
und beide APIs kennen sie. Wenn stattdessen wieder
`E_UNREACHABLE` kommt, liegt es nicht mehr am Quorum, sondern daran, wie ein API
seine Knotenliste abarbeitet — und das wäre der schwerere Befund, weil dann drei
Knoten und eine richtige Liste nicht reichen.

Zwei Dinge, die ich dabei gezielt ansehe, weil sie heute Mittag nicht zu sehen
waren: **ob die erste Anfrage nach dem Ausfall anders aussieht als die zehnte**
(ein Verbindungsaufbau, der in einen Zeitablauf läuft, statt sofort auf den
nächsten Knoten zu gehen — dasselbe Muster wie bei api-2, wo jede vierte
Anmeldung 500 bekam), und **ob db-1 als der Knoten, der den Verbund gebootet
hat, beim Zurückkommen anders behandelt wird** als ein Beitretender.

### db-3 aus — gemessen am 24.10.2026, 18:21–18:31 UTC

Der Ausfall selbst geht glatt. Die Rückkehr nicht.

| Frage | erwartet | gemessen |
|---|---|---|
| (a) Oberfläche | unverändert | **200**, unverändert |
| (b) Anmeldung, 40 in Folge | unverändert | **40/40 gut** — wie vorher |
| (b) **die erste Anfrage** | — | **`503` nach 5,03 s.** Zweite und dritte: `200` in 0,03 s |
| Verbund | fällt auf 2, bleibt Primary | **genau so**: `wsrep_cluster_size=2`, `Primary` |
| laufende Apps | laufen | laufen, unverändert |
| sagt die Anlage es ehrlich? | — | **ja, vorbildlich** (siehe unten) |
| Selbstfang | **ja, schneller als db-1** | **nein. 71 Versuche, keiner gelingt** |

**Die erste Anfrage ist der Befund, den die vierzig verdecken.** Genau dafür war
sie getrennt gemessen, und das api-2-Muster hat sich bestätigt: der erste
Aufrufer nach dem Wegfall läuft in einen Zeitablauf von fünf Sekunden und
bekommt `503`; ab dem zweiten ist der Knoten als tot geführt und alles ist
wieder bei 0,03 s. Hätte ich nur die vierzig gezählt, stünde hier „kein
Unterschied" — und das wäre falsch gewesen.

Die Anlage sagt es dabei **vorbildlich**, besser als in jedem der sechs Fälle
vorher:

```
api-1  WARN the database did not come free in time, the caller is told to come back
       reason=db-busy count=1 since=1 retry-after=5
api-1  WARN callers were turned away because the database was busy or could not be
       reached busy=1 unreachable=0 new-busy=1 new-unreachable=0
```

Eigener Grund, eigener Zähler, `retry-after` — und `503` statt der `500`, die
api-2 geliefert hat. Für den Benutzer heißt das „kommen Sie in fünf Sekunden
wieder" und nicht „interner Fehler". Das ist die Messlatte, an der der
api-2-Fall dieses Blattes zu messen ist: **dieselbe Anlage kann es.**

**Und jetzt der schwere Teil: db-3 kommt nicht zurück.** Nach dem Einschalten
antwortet die Adresse, aber `mariadb` hängt in `activating` und versucht es
wieder und wieder — `NRestarts=71`, als ich die Schleife angehalten habe. Jeder
Versuch startet einen vollen Abgleich und endet so:

```
[ERROR] WSREP: gcs_group_handle_join_msg(): Will never receive state. Need to abort.
```

`grastate.dat` auf db-3 danach: `uuid: 00000000-0000-...`, `seqno: -1`,
`safe_to_bootstrap: 0` — der Knoten hat keinen Stand mehr, von dem aus er
anknüpfen könnte, und braucht einen vollen Abgleich, der nicht zustande kommt.

**Die Ursache habe ich nicht.** Was ich habe, ist eine widerlegte Vermutung: ich
hatte die Firewall im Verdacht, weil der Abgleich über Port 4444 läuft. Die
Regel auf db-1 sagt das Gegenteil, und damit ist der Verdacht erledigt:

```
ip saddr { 10.2.3.212, 10.2.3.217, 10.2.3.218 } tcp dport { 4444, 4567, 4568 } accept
```

Mein Portklopfen von db-3 aus (`4444 ZU`) beweist ebenfalls nichts: 4444 lauscht
nur, **während** ein Abgleich läuft, und db-1 ist dabei nicht der Lauschende.
Eine Sonde, die den Normalzustand für einen Befund nimmt, misst sich selbst.

### Die Ursache ist gefunden, und meine Vermutung war falsch

Hier stand: „ein kurzer Ausfall knüpft vermutlich inkrementell an, ein langer
braucht den vollen Abgleich — und der ist das, was scheitert", gestützt darauf,
dass eine Nachbarsitzung db-3 48 Sekunden lang gestoppt hatte und er nach fünf
Sekunden wieder im Verbund war, während mein Fenster sechs Minuten lang war.
**Das war falsch, und der richtige Satz ist schärfer.** Gefunden hat ihn die
Release-Sitzung; hier steht er als deren Messung.

Es war **nie ein voller Abgleich**. Der Spender sagt wörtlich
`IST failed … Connection refused` — das ist der *inkrementelle* Weg, und
„refused" heißt, dass dort niemand lauschte. (Das ist zugleich das zweite
Argument gegen die Firewall: eine mit `policy drop` lässt ins Leere laufen, sie
weist nicht ab. Mein erstes war die Regel selbst — damit ist sie doppelt
ausgeschlossen.)

Die Ursache: **db-3 öffnete seinen Empfänger auf einer link-lokalen
IPv6-Adresse und nannte dem Spender den Namen.** db-1 löste den Namen auf
10.2.3.218 auf, und dort lauschte nichts. Bei dreißig Sekunden Ausfall wäre das
genauso gescheitert — die Dauer war nie der Unterschied.

**Der Unterschied war Dienst-Stopp gegen VM-Stopp.** Beim VM-Stopp liegt ein
Neustart dazwischen, und `cloud-init` schreibt beim Start `/etc/hosts` neu — und
hat dabei die Zeile verworfen, die den eigenen Namen auf die echte Adresse legte.

**Und das wirft eine Einschränkung auf diese ganze Reihe zurück.** Alle sechs
Fälle oben sind mit `qm stop` gemessen, also sechsmal der **VM**-Fall. Wo eine
Maschine etwas zur Laufzeit gesetzt bekommen hat — in `/etc/hosts` oder sonstwo
—, habe ich den Dienst-Fall nie gesehen. „Ein Dienst-Stopp und ein VM-Stopp sind
zwei verschiedene Fälle" ist damit keine Feinheit, sondern eine Lücke, die
**alle sechs Maschinen** betrifft und in keinem Fall oben gemessen ist.

Damit ist auch mein Satz „die 48-Sekunden-Messung konnte diesen Fehler nicht
finden" nur zufällig richtig gewesen: sie konnte ihn nicht finden, aber nicht
wegen der Dauer, sondern weil sie den Dienst stoppte und nicht die Maschine.

**db-3 ist inzwischen wieder im Verbund** (`size=3`, `Primary`, `Synced`), und
das war nicht mein Werk — ich hatte nur die Schleife angehalten.

Der Fall „db-1 fällt bei drei Knoten" war danach noch offen, aus einem anderen
Grund: db-1 und db-2 trugen denselben Fehler, der db-3 herausgeworfen hat, und
db-1 trägt die CA und den Datenbestand. **Entschieden** (die Wahl lag bei mir):
erst wird `wsrep_node_address` auf beiden festgenagelt, dann wird gemessen. Den
bekannten Mechanismus am Knoten mit der CA noch einmal vorzuführen, kauft keine
Erkenntnis, sondern nur Risiko. Die Release-Sitzung hat es gemacht; alle drei
Knoten tragen jetzt ihre Adresse statt ihres Namens, und der Verbund ist dabei
nie unter drei gefallen.

### db-1 aus, mit drei Knoten — gemessen am 24.10.2026, 19:28–19:31 UTC

**Der Fall, für den der Umbau gemacht wurde, und er geht auf.** Hier war meine
Erwartung einmal richtig.

| Frage | gemessen |
|---|---|
| (a) Oberfläche | **200** |
| (b) **erste** Anmeldung nach dem Wegfall | **200 in 0,027 s** — kein 503, kein Zeitablauf |
| (b) 40 in Folge | **40/40 gut** |
| Verbund | fällt auf 2, bleibt `Primary` |
| laufende Apps | laufen, unverändert |
| Selbstfang | **ja, in unter 30 s** |

Der Vergleich mit dem Mittag ist der ganze Punkt: **dieselbe Maschine, am Mittag
ein Vollausfall mit `E_UNREACHABLE`, jetzt nicht einmal eine langsame Anfrage.**
Drei Knoten und eine Knotenliste, die alle drei nennt, tragen den Ausfall des
Knotens, der vorher die ganze Anlage war.

Der Wiederanlauf braucht keine Hand:

```
19:30:30  WSREP: 1.0 (db-1): State transfer from 2.0 (db-3) complete.
19:30:30  WSREP: Shifting JOINER -> JOINED -> SYNCED
19:30:30  WSREP: Synchronized with group, ready for connections
```

Und eine Einzelheit, die den db-1-Befund vom Mittag ausdrücklich **überholt**:
`grastate.dat` auf db-1 trägt jetzt `safe_to_bootstrap: 0`. Am Mittag war es
`1`, und genau daran hing, dass er sich allein hochziehen konnte. Mit dreien ist
er nicht mehr der Ausgezeichnete — er holt sich den Stand von db-3. **Die
Eigenschaft, die ich am Mittag gemessen habe, hat die Anlage heute Abend nicht
mehr.**

### Was die beiden DB-Fälle zusammen über die fünf Sekunden sagen

Das ist der Befund, den erst der Vergleich hergibt:

| | erste Anmeldung nach dem Wegfall |
|---|---|
| **db-3** fällt | `503` nach **5,03 s** |
| **db-1** fällt | `200` nach **0,027 s** |

Dieselbe Anlage, dieselbe Art Ausfall, zweimal ein anderes Ergebnis für den
ersten Benutzer. Die Strafe von fünf Sekunden ist also **nicht die Knotenliste**
und nicht der Verbund, sondern ein Zeitablauf beim Verbindungsaufbau, der nur
anfällt, **wenn der wegfallende Knoten gerade der benutzte war** (so von der
Release-Sitzung im api-Code eingeordnet; von mir gemessen sind die beiden Zahlen
oben). Für den Benutzer heißt das: es ist ein Münzwurf, ob ihn ein Knotenausfall
fünf Sekunden kostet oder nichts.

Und dieselbe Ursache hat in dieser Reihe **zwei Gesichter beim Benutzer**:
`503` mit `retry-after` und einer Journalzeile mit Zähler (DB-Fall,
vorbildlich) und `500` ohne eine Zeile im Fehlerprotokoll (api-2-Fall, Punkt
90). Dass beides aus einem Zeitablauf beim Verbindungsaufbau kommt, macht den
Unterschied in der Meldung nicht kleiner — es erklärt ihn: niemand hat ihn
entschieden.

**Was ich deshalb NICHT gemessen habe, und das ist eine Entscheidung.** Der Fall
„db-1 fällt bei drei Knoten" ist nicht gefahren. Mit db-3 draußen sind es zwei
von drei; nehme ich db-1 dazu, bleibt **einer**, das Quorum ist weg, und die
Anlage liegt — und zwar in einem Zustand, von dem ich gerade gemessen habe, dass
ein Knoten nicht von selbst zurückfindet. Nach der Rangfolge Sicherheit =
Stabilität vor Leistung ist das kein Fall, den man „noch schnell" fährt. Er
bleibt offen, bis db-3 wieder im Verbund ist.

**Zustand der Anlage am Ende dieses Falls**, gemessen: Verbund 2 von 3,
`Primary`, Oberfläche 200, drei Anmeldungen hintereinander 200 bei 0,03 s. Für
den Benutzer ist nichts kaputt; kaputt ist die **Reserve**. Die Schleife auf
db-3 habe ich angehalten (`systemctl stop mariadb`), weil 71 erfolglose
Versuche keine Selbstheilung sind, sondern Last — der Knoten war zu dem
Zeitpunkt ohnehin nicht im Verbund, das Anhalten ändert an der Anlage nichts und
ist mit einem `start` rückgängig.

## Der Vorfall vom 24.10.2026, 02:45 bis 09:30 — was ich erwarte

Geschrieben am 25.10.2026, **bevor ich ein einziges Protokoll dieses Fensters
geöffnet habe**. Was ich zu diesem Zeitpunkt über den Vorfall weiß, stammt
ausschließlich aus Nebenbemerkungen anderer Sitzungen und aus zwei Zahlen, die
mir bei anderen Messungen untergekommen sind:

- „die Datenbank lag von 02:45 bis 09:30" (mehrfach berichtet, nie belegt)
- `journalctl --list-boots` auf db-1 zeigte mir gestern: ein Start endete
  **02:44:50**, der nächste begann **02:45:03**
- der Sicherungsstand `qemu-303-2026_10_24-04_44_45` — der Lauf hielt db-1
  mitten im Fenster an
- damals war db-1 der **einzige** Datenbankknoten, `wsrep_cluster_size 1`

### Was ich erwarte

**1. Auslöser: ein Neustart von db-1 um 02:45, nicht ein Absturz.** Die dreizehn
Sekunden zwischen 02:44:50 und 02:45:03 sehen nach einem geordneten Neustart aus
und nicht nach einem Stromverlust — bei einem harten Schnitt fehlt die
Journalspitze, wie ich es am 24.10. bei meinem eigenen `qm stop` gemessen habe.
Wer ihn ausgelöst hat, weiß ich nicht; ich erwarte eine Paketaktualisierung oder
einen Neustart von Hand.

**2. Die eigentliche Ursache: der einzelne Galera-Knoten durfte sich nicht
hochziehen.** Das ist meine Haupterwartung. Bei einem **sauberen**
Herunterfahren schreibt Galera den Stand in `grastate.dat` — und dann
entscheidet, was dort steht. Steht `safe_to_bootstrap: 0`, wartet der Knoten
auf einen Verbund, den es nicht gibt, und `mariadb` bleibt in `activating`
hängen. Genau dieses Bild habe ich am 24.10. abends bei db-3 gesehen.

**3. Die Anlage sah dabei aus wie am Mittag bei meinem db-1-Fall:** Oberfläche
`200`, Anmeldung `E_UNREACHABLE` / „Dienst nicht erreichbar.", Apps liefen
weiter. Also sechs Stunden lang eine Anlage, die sich meldet und nichts kann.

**4. Das Ende um 09:30 war eine Hand, kein Selbstfang.** `galera_new_cluster`
oder ein gesetztes `safe_to_bootstrap: 1`. Ich erwarte **nicht**, dass er sich
nach Stunden von selbst gefangen hat.

**5. Der Sicherungslauf um 04:44:45 hat nichts geändert.** Er hielt eine
Maschine an, die ohnehin nicht arbeitete, und fuhr sie wieder an — und der
Knoten lief danach in dieselbe Wand. Erwartung: ein weiterer gescheiterter
Startversuch im Protokoll, mehr nicht.

### Woran meine Erwartung scheitern kann

- **Die Protokolle sind weg.** db-1 ist seitdem mehrfach neu gestartet
  (mein `qm stop`, der Cipher-Neustart). Ob `journalctl` den 24.10. noch hat,
  weiß ich nicht. Wenn nicht, ist der Fall nicht aufklärbar, und das ist dann
  das Ergebnis.
- **Es war gar kein Galera-Problem**, sondern etwas davor — ein Dateisystem,
  das nicht einhängt, ein Netz, das fehlt. Dann liegt die Ursache früher.
- **Der Neustart um 02:45 war nicht geordnet.** Dann ist Punkt 1 falsch und
  mit ihm die Begründung in Punkt 2.

### Was dieser Fall NICHT beantworten wird

Ob die Anlage **heute** denselben Vorfall noch hätte. Sie hat seit dem 24.10.
drei Datenbankknoten, und ich habe gemessen, dass der Verlust eines einzelnen
nicht einmal eine langsame Anmeldung kostet. Dieser Fall ist Forensik an einer
Anlage, die es so nicht mehr gibt — sein Wert liegt darin, ob der Mechanismus
noch da ist, nicht ob das Ereignis sich wiederholt.

## Was offen bleibt

**Der Ausfall aller drei Datenbankknoten gleichzeitig ist ungemessen — und was
die Anlage dafür bereithält, ist gemessen.** Auf allen drei Knoten steht am
24.10.2026 abends in `/var/lib/mysql/grastate.dat`:

```
db-1, db-2, db-3:   seqno: -1,  safe_to_bootstrap: 0
```

(Von mir auf allen drei einzeln gelesen, bei laufendem Verbund `size=3`.)

Was daraus **folgt** — Galeras eigene Regel, also eine Überlegung und keine
Messung: fallen alle drei **unsauber** zugleich weg (Stromausfall, Neustart des
Wirts, oder dreimal `qm stop`), darf **kein** Knoten den Verbund von selbst
hochziehen. Keiner weiß, ob er den neuesten Stand hat, also wartet jeder — und
die Anlage braucht `galera_new_cluster` von Hand. `seqno: -1` heißt genau das:
keiner hat beim Anhalten seinen Stand hinterlassen.

Damit hat der Umbau auf drei Knoten **auch etwas weggenommen**, und das ist das
Gegenstück zum db-1-Fall oben. Am Mittag trug der einzelne Knoten
`safe_to_bootstrap: 1` und fing sich nach einem harten Schnitt in vier Sekunden
allein. Jetzt übersteht die Anlage den Verlust **eines** Knotens, ohne dass ein
Benutzer es merkt — und sie steht, bis jemand kommt, wenn **alle** zugleich
weg sind. Der eine Fall ist häufig und ist jetzt gelöst; der andere ist selten
und war vorher gelöst.

Das ist kein Vorwurf an den Umbau — ein seltener Fall mit einem Handgriff ist
besser als ein häufiger ohne. Aber es ist genau die Sorte Veränderung, die man
nicht merkt, bis sie eintritt, und es ist der Fall, der heute früh von 02:45 bis
09:30 gedauert hat. Wer ihn messen will, braucht ein Fenster, in dem die Anlage
liegen darf, und sollte vorher wissen, wo `galera_new_cluster` steht.

**Der Ausfall von zwei Datenbankknoten gleichzeitig ist ungemessen.** Keine der
beteiligten Sitzungen hat ihn gefahren, und aus zwei verschiedenen Gründen, die
beide dastehen sollen. Bei mir ist es Methode: ich hatte die Erwartung dafür
nicht aufgeschrieben, und ohne die fange ich nicht an. Bei der Release-Sitzung
ist es der Auftrag — zwei von drei Knoten anzuhalten heißt, die Mehrheit der
laufenden Anlage wegzunehmen und jedes Schreiben zu verweigern, und das ist
Thomas' Entscheidung, nicht ihre. Die Erwartung dazu ist **ihre und unbelegt**:
lesen geht noch, jedes Schreiben wird verweigert, und die Anlage sollte `503`
mit `db-busy` liefern und nicht `500`. Nach den beiden gemessenen DB-Fällen ist
das plausibel — aber plausibel ist nicht gemessen, und die Reihe oben lebt
davon, dass der Unterschied benannt wird.

**Eins, und es ist dasselbe in allen sechs Fällen: (c) Apps sehen, (d) eine App
öffnen und (e) eine neue einrichten sind nicht gemessen.** Dafür braucht es eine
Anmeldung, und in `ngc_user` steht genau ein Benutzer: `weiricth`, `admin`, kein
TOTP. Ohne dessen Kennwort kommt man über den ersten Schritt nicht hinaus.
Gemessen ist darum überall der Anfang der Anmeldung (`login/start`) und nie ihr
Ende. Was die Oberfläche in den Fällen *zeigt*, ist für db-1 am Code belegt
(`err.unreachable` gegen `login.failed`) und sonst offen.

Der Sperrzähler stand der Messung nicht im Weg und ist nachgelesen:
`lock.go` hat `freeCount = 2`, danach verdoppelnde Wartezeit bis
`maxWait = 300` s, keine dauerhafte Sperre — und `login/start` zählt überhaupt
keinen Fehlversuch, nur `login/finish` tut das. Die Sonden dieses Blattes haben
also niemanden ausgesperrt.

Drei weitere Löcher, ausdrücklich benannt:

1. **Die stündliche Katalogrunde über eine lange Zeit.** Dass 168 erfolglose
   Versuche keine Zeile hinterlassen, ist am Code belegt und an einer
   erzwungenen Runde gemessen — nicht an einem Ausfall von einer Woche.
2. **`safe_to_bootstrap: 0`.** Dass db-1 sich selbst fängt, ist für den Fall
   gemessen, in dem die Datei `1` trägt. Der bekannte Stillstand von heute früh
   braucht `0`, und **diesen Fall hat niemand erzeugt.** Er steht hier nicht
   unter „abgewogen", sondern unter „offen".
3. **Ob das `::` der Weg aller neun api-2-Fehlschläge ist.** Belegt ist eine
   `[::1]`-Zeile von neun, und dass das `::` im Anfrageweg liegt.

Eine **Berichtigung**, die ich von der Sicherungs-Sitzung übernehme und die
meinen eigenen Text betrifft: der logische Dump, den ich vor dem db-1-Fall
gezogen habe, war mein Rückweg für diese Messung und ist **nicht** der Rückweg
dieser Anlage. Ein zurückgespielter Dump kann Code ausführen, und `register.go`
macht aus genau diesem Grund keinen. Wer einen Rückweg baut, nimmt
`mariabackup` mit den Schlüsseln unter `/etc/ngc/db-keys` — dort gilt der
Hinweis auf die Schlüssel, den ich für den Dump zurückgewiesen hatte, in voller
Länge. Mein Einwand war für das logische Werkzeug richtig und für das
physische falsch.

### Berichtigung am 25.10.2026: es gibt sehr wohl eine Sicherung

Hier stand, am 24.10.2026 habe von dieser Anlage **keine Sicherung** existiert.
Das war falsch, und zwar weil ich nur auf das NGC-eigene Werkzeug gesehen habe.
Die Maschinen liegen täglich in der Sicherung des Proxmox-Wirts. Gemessen auf
10.2.3.2, nicht geglaubt:

```
/etc/pve/jobs.cfg:  vzdump  all 1  exclude 200,203  schedule 03:00
                    mode stop   compress zstd   storage hdd_20TB_2
                    prune-backups keep-last=2
pvesm list:         15 Sicherungen der VMs 301-309, die neuesten vom
                    25.10.2026, 04:41-04:43 Uhr
```

Die beiden neuen Datenbankknoten 308 und 309 sind darin enthalten. Was fehlte,
war also nie *eine* Sicherung, sondern die **NGC-eigene** — und die beantwortet
eine andere Frage: eine VM-Sicherung holt Maschinen zurück, das Werkzeug der
Sicherungs-Sitzung holt eine *Anlage* auf andere Maschinen zurück. Beides ist
nötig, keines ersetzt das andere.

### Und daraus fällt ein Befund in dieses Blatt

`mode stop`. Der Sicherungslauf **hält jede Maschine an** und fährt sie wieder
an — jede Nacht, alle neun, nacheinander.

**Berichtigung an mir selbst:** hier stand „jede Nacht ab 03:00", und meine
eigenen Zahlen hatten das schon widerlegt — die Zeitstempel, die ich zitiert
habe, lauten 04:41 bis 04:43. Der *Auftrag* beginnt um 03:00, aber er arbeitet
sich erst durch die Container des Wirts; bei den NGC-Maschinen ist er **gegen
04:39 bis 04:45**. Selbst nachgezählt: die Stände vom 25.10. beginnen um
04:39:10. Wer das Fenster eines nächtlichen Ausfalls sucht, sucht also an der
falschen Stelle, wenn er bei drei Uhr nachsieht.

Und eine zweite Zahl, die ich nicht hatte und selbst nachgezählt habe:

| VM | Stände auf `hdd_20TB_2` |
|---|---|
| 300, 301–306 | **2** (24. und 25.10.) |
| 307, 308, 309 | **1** (nur 25.10.) |

Die drei mit nur einem Stand sind die, die es am 24.10. noch nicht gab. Das
schärft `keep-last=2` um eine Stufe: nicht nur „zwei Stände", sondern **eine
Maschine, die am Tag entsteht, hat bis zur nächsten Nacht gar keinen**. Am
Abend des 24.10., als ich db-3 abgeschaltet und nicht zurückbekommen habe,
existierte von db-2 und db-3 in keiner Sicherung des Wirts ein Stand — und in
der NGC-eigenen auch nicht. (Die Beobachtung stammt von der Sicherungs-Sitzung,
die Zahlen habe ich nachgezählt.)

Das heißt:

**Der Fall „alle Datenbankknoten gleichzeitig weg", den ich oben als ungemessen
und offen aufgeschrieben habe, läuft in Wahrheit jede Nacht** — in seiner
*sauberen* Form, und die Anlage kommt offensichtlich zurück, denn sie lief den
ganzen 25.10. Ein sauberes Anhalten schreibt den Stand in `grastate.dat`, und
damit greift die Sperre `safe_to_bootstrap: 0` nicht, die den unsauberen Fall
blockieren würde. Der offene Fall ist also schmaler als gedacht: offen ist nur
noch der **unsaubere** gleichzeitige Verlust aller drei — Stromausfall, nicht
Sicherungslauf.

Das ist zugleich die genaueste Fassung der Unterscheidung, die dieses Blatt an
mehreren Stellen macht: **ein Dienst-Stopp, ein sauberer Maschinen-Stopp und
ein harter Schnitt sind drei Fälle, nicht zwei.** Meine sechs Fälle oben sind
alle der dritte (`qm stop`), der nächtliche Lauf ist der zweite, und der erste
ist in dieser Reihe nie gemessen worden.

**Eine Frage, die ich ausdrücklich offen lasse, statt sie zu beantworten** — und
sie hat jetzt ein Datum. Am Morgen des 24.10. lag die Datenbank von 02:45 bis
09:30. Der Sicherungsstand von **VM 303 (db-1) trägt 04:44:45**, selbst
nachgesehen (`qemu-303-2026_10_24-04_44_45`): der Lauf hat db-1 **mitten im
Ausfallfenster angehalten und wieder angefahren**.

Was damit entschieden ist: der Lauf kann den Ausfall **nicht ausgelöst** haben.
Er begann um 02:45, also fast zwei Stunden vorher, und der Neustart von db-1 um
02:45:03 liegt lange davor.

Was damit **nicht** entschieden ist: ob der Lauf den Zustand **verlängert oder
verändert** hat. Ein Anhalten und Anfahren mitten in einem Galera-Stillstand ist
kein neutraler Vorgang, und gemessen hat das niemand. Das Datum steht hier,
damit wer den Vorfall aufklärt den Lauf aus- oder einschließen kann, statt ihn
zu übersehen — nicht als Erklärung. (Das Datum hat die Sicherungs-Sitzung
gefunden, ich habe es nachgesehen.)

### Was zu `d:\ngc_backup` gehört (Stand 24.10.2026)

Gefehlt hat dem NGC-eigenen Werkzeug nur ein Ort, und Thomas hat ihn am selben
Tag genannt: **`d:\ngc_backup`**. Beide Ordner hat er inzwischen selbst wieder
abgeräumt; die Schlüssel liegen in Vaultwarden. Der folgende Abschnitt
beschreibt den Stand von damals und ist als solcher zu lesen.

Der **aktuelle** Stand ist ein anderer: am 25.10.2026 um 12:15 UTC hat die
Sicherungs-Sitzung einen neuen Lauf über sieben Maschinen gefahren, getrennt
abgelegt unter `D:\claude\working\ngc-sicherung\` — 456 MB Daten gegen 197 KB
Schlüssel, alle Prüfsummen je Hälfte in ihrem eigenen Ordner gegengerechnet.
Das ist ihre Messung, nicht meine.

Dort liegt seit 17:05 die logische Sicherung, die ich vor dem db-1-Fall gezogen
habe, mit einer Datei `WAS-HIER-LIEGT.txt` daneben, die sagt, was sie ist und
was nicht. Sie ist **kein** Rückweg dieser Anlage — nur die Datenbank, ein Stand
von 13:36 UTC, und ein zurückgespielter Dump kann Code ausführen, weshalb die
Anlage selbst keinen macht. Sie liegt dort, weil sie zu diesem Zeitpunkt die
einzige Sicherung der Datenbank war, die überhaupt existierte, und sonst mit
meinem Sitzungsordner verschwunden wäre. Dass sie den Kennwort-Abdruck des
einzigen Benutzers enthält, steht in der Datei daneben; ins Repository gehört
sie nicht. Der richtige Rückweg bleibt `mariabackup` mit den Schlüsseln unter
`/etc/ngc/db-keys`, und das Werkzeug dafür gehört der Sicherungs-Sitzung.

**Entschieden am 24.10.2026 nach der Rangfolge Sicherheit = Stabilität vor
Leistung** (von Thomas delegiert), weil die Sicherungs-Sitzung zwei Orte
vorschlagen wollte statt einem:

**Nicht zwei Ordner auf derselben Platte.** Das ist die Form, die wie Trennung
aussieht und keine ist — dieselbe Maschine, dasselbe Konto, dieselben Rechte.
Wer den einen Ordner lesen kann, kann auch den anderen. Die Zahlen sagen, wo
die Trennung wirklich liegt, und sie sind gemessen:

| | Größe | gehört |
|---|---|---|
| die Daten (`/var/lib/mysql`, App-Daten, Dumps) | **285 MB** | nach `d:\ngc_backup`, wie benannt |
| die Schlüssel (`/etc/ngc/db-keys`: `keyfile.key`, `keys.enc`, `sst.pass`) | **224 Byte** | in **keinen Ordner** |

224 Byte schließen 285 MB auf. Was sie gefährlich macht, ist nicht ihr Ort,
sondern dass sie alles aufschließen und in eine Textnachricht passen — also
gehören sie dorthin, wo dieses Haus Geheimnisse hält (Vaultwarden, der
Passwortmanager auf dem Stick), nicht in ein Verzeichnis daneben. Ein
Sicherungsordner ohne die Schlüssel ist Geheimtext; mit ihnen ist er die
Anlage.

**Nachtrag, und er macht aus der Entscheidung eine Messung** — von der
Sicherungs-Sitzung gebaut und gemessen, **nicht von mir**: `tools/sicherung.sh`
nimmt jetzt zwei Ziele und fährt ohne das zweite nicht los (drei Verweigerungen
je mit Code 2, auch wenn der Schlüsselordner *im* Datenordner liegt — die Pfade
werden vorher mit `pwd -P` aufgelöst). Der Lauf gegen alle fünf Maschinen
trennt sich in **348 MB Daten** und **75 KB Schlüssel**. Damit ist das Argument
nicht nur richtig, sondern billig: 75 KB für fünf Rechner passen als Anhang in
einen Passwortmanager, und der Datenordner darf liegen, wo Platz ist.

Und sie hat die Trennung **an den Werten geprüft, nicht an den Wörtern**: ein
`grep` nach Feldnamen findet im Datenordner sieben Treffer, die alle aus
Zeichenketten in den Programmen des App-Abbilds stammen; die Suche nach den
achtzehn **echten** Geheimnissen des Schlüsselordners ergibt **0 von 18** im
Datenordner. Eine Trennung, die nur an Dateinamen geprüft wird, ist nicht
geprüft.

**Eine Ausnahme bleibt offen und ist keine Nachlässigkeit:** `app-daten.tar` ist
Klartext, 110–201 MB, zu groß für einen Passwortmanager, und es geht ohne jeden
Schlüssel auf. Es liegt darum bei den Daten. Nach derselben Rangfolge gehören
die App-Daten **vor** dem Wegschreiben verschlüsselt — mit einem Schlüssel, der
in den Schlüssel-Ordner geht und nicht mitfährt.

Die Bedingung dazu war, dass der Weg zurück mit getrenntem Schlüssel **einmal
gegangen** sein muss, bevor verschlüsselt wird — sonst tauscht man ein
Leseproblem gegen ein Totalverlustproblem und hat einen grünen Lauf als Beleg.
Dafür fehlte die Maschine, und was dabei half, kam aus der Vermessung dieses
Wirts: die **Vorlage VMID 300 (`ngc-vorlage`) stand noch**, 308 und 309 waren
frei, `ssd_2TB` hatte 1 TB von 1,9 TB frei (`qm list` und `pvesm status` auf
10.2.3.2, 24.10.2026). Eine Wegwerf-Maschine war also ein Klon und keine
Neuinstallation.

**Beides ist inzwischen eingelöst — von der Sicherungs-Sitzung gebaut und
gemessen, nicht von mir.** Auf einem Klon von 300 (VM 311) ist der Weg zurück
gegangen: `app-daten.tar.enc` lässt sich ohne Schlüssel nicht öffnen, auch nicht
mit einem leeren und nicht mit einem falschen 32-Byte-Schlüssel („This does not
look like a tar archive"), mit dem richtigen 6720 Dateien — und danach laufen
**beide** Apps wieder, Terminal und Uhr. Der Schlüssel ist einer je Maschine und
Lauf, er kommt über `ssh`-stdin in eine 0600-Datei statt als Argument (ein
Argument steht in `ps` für jeden Benutzer), und der Klartext wird nirgends eine
Datei. Kosten: 473 s statt 313 s, Schlüssel-Ordner 106 statt 75 KB.

Eine Einschränkung, die sie selbst nennt und die hier mitsteht, weil sie leicht
untergeht: **AES-256-CTR gibt Vertraulichkeit, keine Beglaubigung.** Ein
verändertes Byte fängt die `sha256`-Liste im Verzeichnis — und die fängt einen
**Unfall**, nicht jemanden, der in den Ordner schreiben darf, denn der kann die
Liste mitändern. Nach derselben Rechnung wie bei den 224 Byte gehört die
Prüfsummenliste darum **zu den Schlüsseln**, nicht neben die Daten.

Das war mein Vorschlag, und er ist **gebaut und gemessen** — von der
Sicherungs-Sitzung, nicht von mir. Mit einem angehängten Byte in
`app-daten.tar.enc`: der Datenordner allein enthält **null Prüfsummen**, es ist
also nichts zu vergleichen; mit `summen.txt` aus dem Schlüssel-Ordner meldet
`sha256sum -c` `app-daten.tar.enc: FAILED` und `state.db: OK`. Damit gilt für
beide Hälften derselbe Satz: wer nur die Daten hat, kann sie weder lesen noch
unbemerkt ändern. Laufzeit kostet es nicht.

**Und zu meiner eigenen Datei, nach derselben Rangfolge:** sie bleibt liegen.
Sie trägt einen argon2id-Abdruck, keinen Klartext, auf dem Rechner ihres
Besitzers in einem Ordner, den er selbst benannt hat — und sie ist zur Stunde
die einzige Sicherung der Datenbank, die existiert. Sicherheit und Stabilität
stehen hier gleich, und bei Gleichstand wiegt „es gibt überhaupt eine
Sicherung" mehr als „ein Abdruck liegt auf der eigenen Platte". Sobald das
richtige Werkzeug einmal dorthin geschrieben hat, kann sie weg.

Zwei Zahlen aus den Nachbarsitzungen, die beim Wiederanlauf zählen und hier
stehen, damit sie nicht in Nachrichten verschwinden — **von dort gemessen,
nicht von mir**: kein App-Container hat eine eigene cgroup, es greift
`TasksMax=3466` des Controllers (gemessen nachher: 24 bzw. 16 Aufgaben, der
Deckel ist weit weg); und der Systembenutzer einer App wird nur von
`install/activate.go` angelegt, nie von `node.Restore` — nach einer
*Wiederherstellung* bleibt eine App darum aus, auch wenn sie nach einem
*Ausfall* von selbst zurückkommt. Beides sind verschiedene Wege, und dieses
Blatt hat nur den zweiten gemessen.

**svc-1 trägt `ngc-controller` 0.2.2, svc-2 trägt 0.2.1, db-1 ebenfalls 0.2.1.**
Die Knoten sind nicht gleich. Dass beide svc-Knoten sich gleich verhalten
haben, ist gemessen — dass sie es wegen der Fassungen müssten, nicht.
