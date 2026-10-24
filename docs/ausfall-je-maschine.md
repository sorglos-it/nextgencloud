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

Der schwere Befund, den der Auftrag befürchtet hat, ist damit **nicht**
eingetreten: die Anlage arbeitet ohne ihren Laden weiter. Der Laden ist kein
Teil der Anlage, und sie behandelt ihn auch nicht wie einen.

## Was offen ist, und warum

Vier der sechs Fälle sind nicht gefahren, und die Spalte „was der Benutzer
dabei sieht" ist in dem einen gefahrenen Fall leer. Beides hat Gründe, die
nicht an der Messung liegen:

1. **Es fehlt ein Zugang.** In `ngc_user` steht genau ein Benutzer: `weiricth`,
   `admin`, kein TOTP. Ohne dessen Kennwort sind (b) anmelden, (c) Apps sehen,
   (d) App öffnen und (e) neu einrichten in **keinem** Fall zu messen — und das
   ist genau die Spalte, um die es im Auftrag geht. Die Leitsitzung gibt das
   Kennwort nicht weiter (richtig so, es ist nicht ihres). Der Sperrzähler ist
   nachgelesen und harmlos genug für eine Messung:
   `lock.go` hat `freeCount = 2`, danach verdoppelnde Wartezeit bis
   `maxWait = 300` s, keine dauerhafte Sperre des Kontos.
2. **api-1 und db-1 sind gesperrt.** Beide nehmen die ganze Anlage mit; an ihr
   hängen gerade mehrere Sitzungen. Bei db-1 erwarte ich, dass sie ohne
   `galera_new_cluster` von Hand nicht zurückkommt — heute früh lag die
   Datenbank aus genau diesem Grund von 02:45 bis 09:30 tot. Ein Fenster dafür
   liegt bei Thomas.
3. **api-2, svc-1 und svc-2 sind freigegeben, aber das Abschalten wurde
   abgelehnt.** Der erste `qm stop 302` wurde von der Prüfung dieser Sitzung mit
   „Interfere With Workloads" zurückgewiesen. Das ist nicht umgangen worden.
   Der Ladenfall lief vorher noch durch; ab api-2 braucht es eine Erlaubnis.
4. **Die stündliche Katalogrunde ist nicht gemessen, nur gelesen.** Der Beleg
   dafür, dass 168 erfolglose Versuche keine Zeile hinterlassen, braucht ein
   Ausfallfenster von über einer Stunde. Vierzig Sekunden zeigen es nicht.

Zwei Dinge aus den Nachbarsitzungen, die beim Wiederanlauf der Dienstknoten
zählen und hier festgehalten sind, damit sie nicht in Nachrichten verschwinden
— **von dort gemessen, nicht von mir**:

- Kein App-Container hat auf dieser Anlage eine eigene cgroup;
  `CgroupPath` zeigt auf `/system.slice/ngc-controller.service`. Was greift, ist
  `TasksMax=3466` des Controllers, geteilt mit allen Apps des Knotens. Im
  Leerlauf steht er bei `TasksCurrent=16`; ein leerer App-Container kostet rund
  6 Aufgaben, eine offene Terminal-Sitzung rund 9,4. Für den Wiederanlauf heißt
  das: die Container allein bringen den Deckel nicht in Gefahr. Hängt dort
  etwas und `TasksCurrent` steht weit unter 100, ist es **nicht** der Deckel.
- **svc-1 trägt `ngc-controller` 0.2.2, svc-2 trägt 0.2.1.** Die beiden Knoten
  sind nicht gleich. Kommt einer anders zurück als der andere, muss das nicht am
  Ausfall liegen.
