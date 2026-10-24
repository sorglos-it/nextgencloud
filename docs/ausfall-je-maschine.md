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

## Was gemessen wurde

*(noch nichts — dieser Abschnitt wird nach jedem Fall gefüllt, je Fall in einem
eigenen Stand)*
