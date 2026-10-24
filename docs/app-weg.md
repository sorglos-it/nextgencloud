# Der Weg einer App in den Laden

Wie aus einem Ordner eine NGC-App wird, die sich auf einem Server einrichten und öffnen lässt.
Je Schritt der echte Befehl.

Dieses Papier entstand, weil es den Weg nicht gab. NGC hatte **eine** App im Laden — das
Terminal, im Oktober 2026 von Hand gebaut, von Hand paketiert, von Hand in einen Testladen
gelegt. Keine Zeile beschrieb das. Belegt ist der Weg hier mit einer **zweiten** App
(`apps/uhr/`, `org.sorglos.uhr`), die mit Absicht winzig ist: eine Uhr. Was daran Arbeit war,
ist der Weg, nicht die App.

**Gemessen und abgewogen stehen getrennt.** Jeder Abschnitt sagt, was gelaufen ist und was
nicht. Am Ende stehen die Befunde: Stellen, an denen der Weg einen Handgriff braucht, den kein
Fremder erraten kann.

---

## Die zwei Prüfstände

Es gibt zwei NGC-Anlagen, und dieses Papier nennt sie auseinander, weil fast jeder Befund
unten an der einen hängt und nicht an der anderen:

- **Die Einzel-VM** (`ngc`, 10.2.3.208, Fassung 0.2.0): alles auf einer Maschine — API,
  Controller, Shell und der Laden daneben. Dort steht die eine App, die es bisher gab, und
  dort sind die Befunde 2 bis 4 gemessen. Sie ist über Monate von Hand gewachsen.
- **Die verteilte Anlage** (`ngc.mein.lan`, Fassung 0.2.1): sechs Maschinen.

```
10.2.3.210  ngc.mein.lan, api-1.ngc.mein.lan    Shell und erste API
10.2.3.211  api-2.ngc.mein.lan                  zweite API
10.2.3.212  db-1.ngc.mein.lan                   Datenbank
10.2.3.213  svc-1.ngc.mein.lan                  Dienstknoten - hier laufen Apps
10.2.3.214  svc-2.ngc.mein.lan                  Dienstknoten - hier laufen Apps
10.2.3.215  store.ngc.mein.lan                  der Laden, eigene Maschine
```

Gemessen wird **gegen die Namen**, nie gegen die IP: die Shell besteht auf ihrem Namen
(`config.json`, `rp.id` und `origins`), und mit einer nackten Adresse scheitert schon die
Anmeldung.

Die Namen sind **keine Rollenverteilung, auf die man sich verlassen darf.** Es gab einen Stand,
auf dem `db-1` einen Controller trug und `store` gar kein NGC. Wo eine App wirklich liegt, liest
man nach dem Einrichten nach, statt es aus einem Namen zu schließen.

Für diesen Weg sind zwei Unterschiede die wichtigen: der Laden ist dort eine **eigene**
Maschine, das Paket muss also wirklich über Netz geholt werden können — und es gibt **zwei**
Dienstknoten, womit erstmals prüfbar ist, dass eine App dort landet, wo man sie hinschickt.

---

## 0. Was eine App ist

Ein Paket (`*.ngc`) ist ein tar mit drei Dingen:

```
manifest.json          zuerst im Archiv, immer
ui/…                   die Oberfläche (eine Datei oder viele)
services/amd64.tar     das Abbild des Behälters, je Architektur
services/arm64.tar
```

Mehr darf nicht drin sein. `shared/store/pack.go` (`store.Walk`) nimmt **nur** diese Namen:
Verzeichnisse dürfen nur `ui`, `services` oder etwas unter `ui/` heißen, Dateien nur
`manifest.json`, etwas unter `ui/` oder genau `services/amd64.tar` bzw. `services/arm64.tar`.
Alles andere ist `store: invalid app package`. Links, Geräte und Sparse-Dateien fliegen raus.

Eine App braucht **nicht** beides: eine App ohne `services/` ist eine App, die nur eine
Oberfläche hat; eine ohne `ui/` ist ein reiner Dienst. Eines von beiden muss da sein
(`Manifest.Validate`).

---

## 1. Das Manifest

`manifest.json`, höchstens 64 KiB, **strenges** JSON: ein unbekanntes, doppeltes oder anders
geschriebenes Feld ist ein Fehler, kein Hinweis (`enc.Budget`, `store.ParseManifest`).

Das Manifest der Uhr, vollständig (`apps/uhr/templates/manifest.json`):

```json
{
  "id": "org.sorglos.uhr",
  "name": "Uhr",
  "version": "1.0.0",
  "publisher": "Sorglos Thomas Weirich",
  "description": "Die Uhrzeit des Servers lesen.",
  "ui": { "entry": "index.html", "width": 420, "height": 260 },
  "actions": [
    { "name": "zeit", "title": "Die Uhrzeit des Servers lesen", "service": "uhr" }
  ],
  "services": [
    { "name": "uhr", "command": ["/usr/lib/ngc/ngc-uhr"] }
  ],
  "runtime": "podman",
  "cpu": 1000,
  "memory": 256
}
```

### Die Felder

| Feld | Pflicht | Regel |
|---|---|---|
| `id` | ja | `wire.ValidAppID`; **kein** Name einer Kern-App (`ngc.desktop`, `ngc.users`, `ngc.store`) |
| `name` | ja | 1–64 Byte druckbarer Text |
| `version` | ja | `MAJOR.MINOR.PATCH` oder `MAJOR.MINOR.PATCH.BUILD`, jede Stelle ohne führende Null |
| `publisher` | ja | 1–64 Byte |
| `description` | nein | bis 1000 Byte |
| `icon` | nein | Pfad **unter `ui/`**, ohne `ui/` davor; die Datei muss im Paket liegen |
| `ui` | eines von beiden | `entry` (Pfad unter `ui/`), `width`/`height` je 0 oder 200–4000 |
| `services` | eines von beiden | bis 16; `command[0]` absoluter Pfad, bis 32 Teile, je bis 256 Byte |
| `actions` | nein | bis 64; `service` muss einen Dienst **dieses** Manifests nennen |
| `start` | nein | eine Aktion, die der Controller nach jedem Start fährt; darf nicht `stream` sein |
| `runtime` | nein | nur `podman` (`lxc` ist für später reserviert) |
| `needs` | nein | `root`, `network`, jedes höchstens einmal |
| `cpu` | nein | Tausendstel eines Kerns, 100–64000; Vorgabe 1000 |
| `memory` | nein | MiB, 16–65536; Vorgabe 256 |
| `minNgc` | nein | die älteste NGC-Fassung, die die App braucht |

Eine Aktion kann noch `stream` (bleibt offen, kein Zeitlimit — dann **muss** `timeout` 0 sein),
`timeout` (Sekunden, bis 3600, Vorgabe 30) und `critical` (braucht eine eigene Unterschrift).

### Was eine App an Rechten anfragen kann

Zwei Dinge, und sie sind verschieden:

1. **`needs`** — was der Behälter darf. Genau zwei Werte: `root` (Root **im** Behälter) und
   `network` (ans Netz). Der Verwalter erlaubt sie beim **Einrichten**, einmal, und sie stehen
   danach in der Datenbank des Controllers. Fragt eine neue Fassung mehr an, als erlaubt wurde,
   wird sie **nicht** eingespielt — sie bekommt den Zustand `needsgrants`, und die Oberfläche
   bietet das Einrichten an, statt einen Fehler zu zeigen (`store.Granted`,
   `store.UpdateNeedsGrants`).
2. **`actions`** — was ein **Nutzer** darf. Jede Aktion wird ein Recht `<app>/<aktion>`. Dazu
   kommt ein Recht, das in keinem Manifest steht: **`<app>/use`** — „benutzen". Ohne das sieht
   ein Nutzer die App überhaupt nicht.

Die Uhr fragt **nichts** an: kein `root`, kein `network`. Das ist Absicht — jedes Recht, das
sie nicht braucht, ist eines, das der Weg auch nicht braucht. Im Katalogeintrag fehlt `needs`
darum ganz (siehe Schritt 5).

---

## 2. Wie die Oberfläche ins Fenster kommt

Nicht so, wie man denkt. Die Oberfläche wird **nicht** in den Rahmen geladen — sie wird
hineingeschrieben. Der Weg (`apps/web/public/assets/js/shell/appframe.js`, die Reihenfolge ist
dort die Sicherheit):

1. Die Shell holt den **unterschriebenen UI-Beleg** und prüft ihn gegen die Vertrauensliste.
   Heraus kommt die Prüfsumme des `ui`-tars der App.
2. Sie holt das `ui`-tar, mit Obergrenze.
3. Sie hasht, was angekommen ist, und vergleicht. Ungleich: abgelehnt, und es wird **nichts**
   gelesen.
4. Erst dann läuft sie das Archiv durch.
5. Die Startseite muss eine Datei **dieses** Archivs sein.
6. Das Dokument wird aus diesen Dateien gebaut und aus keinen anderen: `<link>` und `<script>`
   werden durch die Bytes dahinter ersetzt.
7. Das Ganze kommt in einen Rahmen mit eigenem Ursprung, ohne Speicher und ohne Verbindung.

Was das für die App heißt — drei Dinge, die man einmal falsch macht:

- **Klassische Skripte, keine Module.** Der Rahmen macht **keine** Verbindung, und ein Modul
  wird geholt. `<script type="module">` läuft dort nie; die Shell sagt dann
  „Diese App braucht klassische Skripte" und zeigt die App nicht.
- **`ngc.js` kommt von der Shell.** Die Shell schreibt die Bytes als erstes Skript in das
  Dokument. Die App lädt sie nicht und darf sie nicht mitliefern.
- **Relative Adressen, flach.** `href="app.css"` und `src="app.js"` zeigen auf Dateien im
  `ui/`-Ordner. `store.ValidPath` erlaubt keine `..`, keine führenden Schrägstriche und keine
  versteckten Namen.

Die App spricht nur über `ngc.js` und nur mit ihren **eigenen** Diensten:

```js
const bytes = await ngc.call("uhr", "zeit");   // Promise<Uint8Array>
const strom = ngc.open("uhr", "shell");        // bleibt offen: write, end, onData, onClose
```

`ngc.call` liefert ein **`Uint8Array`** — die rohen Bytes von `stdout` des Dienstes,
unverändert durchgereicht. Das Umwandeln in Text ist die Sache der App
(`new TextDecoder().decode(bytes)`), und das Misstrauen auch: alles davon gehört auf
`textContent`, nie auf `innerHTML`.

Eine Aktion, die das Manifest nicht trägt, lehnt die Shell ab, bevor sie jemanden fragt
(`appframe.doCall`, `CODES.FORBIDDEN`). Ein Tippfehler im Namen ist also eine Ablehnung und
kein Aufruf.

**Nicht gemessen:** dass die Uhr-Oberfläche so im Fenster erscheint — siehe Schritt 7.

---

## 3. Das Abbild

Zwei Sätze zuerst, weil sie der Grund für alles andere in diesem Abschnitt sind:

> **Das Abbild entsteht auf einem Baurechner, nie auf dem Zielserver.** Der Zielserver hat
> keinen Bauer und darf keinen brauchen. Ein Abbild, das auf dem Server entstünde, wäre ein
> zweiter Weg, auf dem ein Abbild in den Speicher kommt, aus dem die Apps laufen — und es wäre
> ein Abbild, das niemand unterschrieben hat.

Das steht ausführlich in `apps/server-controller/classes/node/image.go` und gilt für jede App.

Der `Containerfile` der Uhr (`apps/uhr/templates/Containerfile`), ohne die Kommentare:

```Dockerfile
FROM docker.io/library/debian:13-slim@sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f
RUN mkdir -p /usr/lib/ngc /data
COPY --chmod=0755 ngc-uhr /usr/lib/ngc/ngc-uhr
CMD ["/usr/lib/ngc/ngc-uhr", "hold"]
```

Vier Dinge daran sind keine Geschmacksfrage:

- **Die Basis hängt an einem Digest, nicht an einem Namen.** Ein Name zeigt morgen auf andere
  Bytes. Welchen Digest und wie er gemessen wurde, steht in `tools/pin-base.sh`; geändert wird
  die Zeile mit diesem Werkzeug, nie von Hand.
- **`--chmod=0755`.** `COPY` behält die Rechte, die die Datei auf dem **Baurechner** hat, und
  ein Go-Bau auf Windows schreibt `0644` — ein Laufwerk, das ein Ausführungsbit gar nicht
  setzen kann. Ohne `--chmod` baut das Abbild, lässt sich sichern, laden und einrichten, und
  der erste Aufruf des Dienstes scheitert im Behälter mit „permission denied".
  **Gemessen am 24.10.2026**, beim Bau dieser App.
- **Ein `CMD`, das wartet.** Der Controller fährt den Dienst mit `exec` **in** den laufenden
  Behälter. Steht der Behälter nicht, gibt es nichts, in das man fahren könnte. Das Terminal
  nimmt dafür `ngc-controller hold`, die Uhr `ngc-uhr hold`: ein Programm, das auf `SIGTERM`
  wartet und sonst nichts tut.
- **Nichts wird nachinstalliert.** Je weniger Programme im Abbild liegen, desto weniger
  erreicht, wer den Behälter erreicht.

### Die Befehle

Erst das Programm, **für die Architektur des Zielservers** übersetzt, in einen eigenen
Bauordner — das ist der Zusammenhang des Baus, und nur daraus kopiert der `Containerfile`:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=false -trimpath \
  -ldflags "-X main.Version=1.0.0" -o apps/uhr/build/ngc-uhr ./apps/uhr
```

Dann das Abbild, auf dem Baurechner mit podman (hier: Debian unter WSL, podman 5.4.2):

```sh
cd apps/uhr
podman build --arch amd64 --file templates/Containerfile --tag ngc/uhr:1.0.0 --pull=always build
```

`--arch` ist nötig: ohne das entscheidet die Architektur dessen, der baut, und das Abbild
stirbt auf dem Server mit „exec format error". Die Architektur steht damit an **drei** Stellen
— `GOARCH`, `--arch` und der Name des tars —, und nichts verbindet die drei außer dem Menschen
davor (Befund 5).

Dann das Abbild in das tar, das das Paket trägt:

```sh
mkdir -p templates/services
rm -f templates/services/amd64.tar
podman save --format docker-archive --output templates/services/amd64.tar ngc/uhr:1.0.0
```

`--format docker-archive` ist Vorschrift, keine Wahl: der Controller liest den Namen des
Abbilds aus dem tar, und den hat nur ein Docker-Archiv (`runtime.SaveFormat`).

**Das `tag` des Abbilds ist die Fassung aus dem Manifest.** Ein Abbild, dessen Tag anders
heißt als die Fassung im Manifest daneben, ist ein Abbild, das kein Paket benennen kann.

**Gemessen am 24.10.2026** (Debian unter WSL, podman 5.4.2, amd64):

| Schritt | Ergebnis |
|---|---|
| `go build` (statisch, amd64) | 4 337 545 Byte |
| `podman build` | `localhost/ngc/uhr:1.0.0`, 4 Stufen |
| `podman save --format docker-archive` | 85 614 080 Byte |
| Dienst im Abbild gefahren (`NGC_ACTION=zeit`) | Ende 0, `stdout` die Zeit, `stderr` die andere Zeile |

Der letzte Lauf ist die Gegenprobe zum Ausführungsbit und zu den zwei Richtungen; er steht
ausgeschrieben in Stolperstein 4.

### Wie ein Dienst aufgerufen wird

Der Controller startet das Programm **ohne Shell**, mit der Argumentliste aus dem Manifest. Was
der Aufruf ist, steht in der Umgebung, und es gibt keine anderen Variablen
(`shared/service/service.go`):

| Variable | Inhalt |
|---|---|
| `NGC_ACTION` | die Aktion — **als Umgebungsvariable, nicht als Argument** |
| `NGC_PARAMS` | die Parameter als JSON-Objekt in einer Zeile, oder leer |
| `NGC_USER`, `NGC_USER_NAME` | wer fragt; bei der Start-Aktion leer |
| `NGC_APP`, `NGC_SERVICE`, `NGC_VERSION` | welche App, welcher Dienst, welche Fassung |
| `NGC_DATA` | immer `/data` |
| `NGC_START` | `1` bei der Start-Aktion, sonst `0` |

Dazu `LANG=C.UTF-8` und `PATH=/usr/local/bin:/usr/bin:/bin`.

Dass die Aktion in der Umgebung steht und nicht als Argument, ist die Stelle, an der fremde
Programme nicht passen: die wollen sie als Argument, und nichts in NGC überbrückt das. Wer ein
fremdes Programm portiert, schreibt dafür einen Adapter (Punkt 68, `versuch-port/`).

---

## 4. Das Paket

Ein Befehl. Der Ordner, der gepackt wird, ist der mit `manifest.json`, `ui/` und `services/`:

```sh
go run ./apps/cli pack apps/uhr/templates /pfad/uhr-1.0.0.ngc
```

(Gebaut heißt das Werkzeug `ngc-store`: `go build -o ngc-store ./apps/cli`.)

Es antwortet mit Kennung, Fassung und der SHA-256 der Datei:

```
org.sorglos.uhr 1.0.0
db5a5f517b74c6209a109cc6dcd1277b08530d54166a9c24bc99da0229cbfce0
```

**Gemessen am 24.10.2026:** 85 623 296 Byte.

Was `pack` dabei von allein richtig macht, und warum man es nicht mit `tar` von Hand macht:

- Die Reihenfolge: `manifest.json` zuerst, dann `ui/` nach Pfad sortiert, dann die Abbilder.
- **Keine Zeit von der Platte.** Jeder Eintrag bekommt Rechte 0644, Eigentümer 0 und
  Änderungszeit 0. Sonst hinge die SHA-256, die ein Katalog nennt, daran, **wann** jemand
  gepackt hat, und nicht daran, **was** im Paket liegt. Zwei Bauten aus derselben Quelle geben
  dieselbe Prüfsumme.
- **Keine Verzeichniseinträge.** `pack` schreibt nur Dateien — und damit auch kein `./`
  (Stolperstein 1).
- Es liest das Paket mit dem Leser des Vertrags **zurück**, bevor es fertig meldet. Ein halb
  geschriebenes Paket wird wieder gelöscht.

---

## 5. Der unterschriebene Katalog

Ein Laden ist **Dateien auf einem Webserver**: die Pakete und eine `catalog.json`, die sie
nennt und unterschrieben ist. Nichts weiter.

Die Pakete in einen Ordner legen und unterschreiben:

```sh
go run ./apps/cli catalog <ordner> <schlüsseldatei> <laden> <folge> https://store.example.com
```

Für die Uhr, gegen einen eigenen Probeladen:

```sh
go run ./apps/cli catalog /pfad/laden /pfad/laden-key.seed uhr-probe 1 https://store.ngc.test/uhr
```

Fehlt die Schlüsseldatei, legt `ngc-store` sie an (Rechte 0600) und nennt **nur** ihren Pfad,
den öffentlichen Teil und die Kennung — nie den Schlüssel selbst. Geheime Schlüssel stehen nie
in einem Aufruf: ein Aufruf steht in der Prozessliste und ist für jeden anderen lesbar.

**Gemessen am 24.10.2026**, der Eintrag, den es schrieb:

```json
{ "store": "uhr-probe", "seq": 1, "issued": 1792834980,
  "apps": [ { "id": "org.sorglos.uhr", "name": "Uhr",
    "publisher": "Sorglos Thomas Weirich",
    "description": "Die Uhrzeit des Servers lesen.",
    "versions": [ { "version": "1.0.0",
      "url": "https://store.ngc.test/uhr/uhr-1.0.0.ngc",
      "size": 85623296,
      "sha256": "21pfUXt0xiCaEJzG3NEnewhTDVQWapwkvJnaAinL_OA",
      "ui": "n0-Z4sCcKItN0qd6Kr1-DADIuquq5-infFHJfiNMLu8" } ] } ] }
```

Zwei Prüfsummen, nicht eine, und sie prüfen Verschiedenes:

- `sha256` ist die der **Datei, wie sie geladen wird**. Daran erkennt eine Anlage ein Paket,
  das unterwegs abgeschnitten wurde — ein zu kurzes tar liest sich sonst als vollständiges,
  kürzeres Archiv.
- `ui` ist die der **Oberfläche** (`store.PackageUI`), über das **unverschlüsselte** Paket. Das
  ist der Wert, gegen den die Shell in Schritt 2 prüft.

`needs` und `minNgc` übernimmt `catalog` aus dem Manifest. Bei der Uhr fehlt `needs` ganz, weil
sie nichts anfragt — der Eintrag oben ist der Beleg dafür.

Dazu legt `catalog` **`store-key.pem`** in den Ordner: den **öffentlichen** Teil des
Schlüssels. Der geheime Teil liegt nie im Ordner, aus dem der Laden ausgeliefert wird. Ohne die
Datei kann niemand den Laden prüfen, auch dieses Werkzeug nicht. (Wer `store-key.pem` für den
geheimen Teil hält, verliert ihn — siehe Befund 2.)

### Die Folgenummer

`seq` muss **höher** sein als die des Katalogs, der schon im Ordner liegt — `ngc-store` liest
ihn vorher und lehnt sonst ab. Eine Anlage lehnt einen Katalog mit niedrigerer Folge ab
(`store.ErrRollback`). Ein neuer Katalog, der eine App dazunimmt, nennt also **alle** Apps und
eine höhere Folge: ein Katalog ist das ganze Verzeichnis des Ladens, keine Ergänzung.

### Prüfen, bevor es wehtut

```sh
go run ./apps/cli check -store uhr-probe /pfad/laden
```

**Gemessen am 24.10.2026:** `store ok`.

### Den Laden ausliefern

Apache braucht dafür nichts als ein Verzeichnis (`DocumentRoot`), TLS und
`Options -Indexes -FollowSymLinks`. Auf der **Einzel-VM** (`ngc`, 10.2.3.208) steht der Laden
unter `/srv/ngc-store` an `https://store.ngc.test`, mit einer eigenen CA, deren Wurzel im
System-Vertrauensspeicher liegt — die API baut ihren Client ohne eigene Wurzeln und prüft die
Kette darum wirklich. Die beiden Skripte dafür liegen **nicht** im Repo; siehe Befund 3.

**Und genau da verdeckt dieser Prüfstand etwas.** Auf der Einzel-VM liegt der Laden auf
derselben Maschine wie die API. Damit ist nie geprüft worden, was der Weg eigentlich verlangt:
dass das Paket **über Netz** geholt werden kann. Die verteilte Anlage (`ngc.mein.lan`, Fassung
0.2.1) hat dafür eine **eigene** Maschine, `store.ngc.mein.lan` — der ehrlichere Prüfstand.
**Nicht gemessen:** ob dort heute schon etwas ausliefert; der Name löst auf (10.2.3.215), aber
ein TLS-Handschlag von außen kam nicht zustande.

### Wie der Laden vertraut wird

Hier gibt es **zwei** Wege, und das ist der Punkt, an dem man sich verläuft:

- **Der globale Laden** (`"store": "global"`) steht **nicht** in der Vertrauensliste. Sein
  Schlüssel und seine Adresse stehen in der Konfiguration der API, `/etc/ngc/api.json`, als
  `storeKey` und `storeUrl` (`config.API`). Ändern heißt: Root auf dem Server und die API neu
  starten.
- **Jeder weitere Laden** steht in der **Vertrauensliste** (`trust.Store`: Kennung, Name,
  Adresse, Schlüssel) und kommt dort über die Oberfläche hinein — ein Verwalter nimmt ihn auf
  und unterschreibt das persönlich. Die Adresse darf einen Pfad tragen
  (`https://store.example.com/uhr`), mehrere Läden können also auf einem Host liegen.

Wer einen **eigenen** Laden betreibt, nimmt also die Kennung **`global`**: `ngc-api.sh` trägt
`store_url` und `store_key` unter dieser Kennung ein. Ein Katalog, der sich anders nennt, wird
abgelehnt, und die Anlage sagt dazu nur „Der Katalog ist fehlerhaft oder gehört zu einem anderen
Store" (`store.VerifyCatalog`, `c.Store != st.ID`) — welche der beiden Kennungen nicht passt,
sagt sie nicht.

### Gemessen: eine zweite App in einen laufenden Laden

Das ist der Weg, den dieses Papier belegt, auf der verteilten Anlage am 24.10.2026. Der Laden
ist dort eine eigene Maschine (`store.ngc.mein.lan`), trug den Terminal und die Folge 6.

Der geheime Schlüssel liegt dort, wo das Aufsetz-Skript ihn hingelegt hat —
`/root/store/store.key` auf der Laden-Maschine — und `ngc-store` liegt unter
`/usr/local/bin/ngc-store`. Erst der Katalog zur Seite gelegt, dann das Paket dazu, dann neu
unterschrieben:

```sh
# auf der Laden-Maschine
cp -n /var/www/store/catalog.json /root/store/catalog-vor-uhr-20261024.json
# das Paket dazu (von einem Baurechner hergebracht)
ngc-store catalog /var/www/store /root/store/store.key global 7 https://store.ngc.mein.lan
ngc-store check -store global /var/www/store
```

Das Ergebnis, aus dem Katalog gelesen, der danach dort lag:

```
store global seq 7
  org.sorglos.terminal 1.0.0 https://store.ngc.mein.lan/terminal.ngc 108028416 needs=['root']
  org.sorglos.uhr      1.0.0 https://store.ngc.mein.lan/uhr.ngc       85623296 needs=None
kid RPmRtyPJs5WQECAkiPeBsg
```

`check -store global` sagte `store ok`. Drei Dinge daran sind der eigentliche Beleg:

- **Der Terminal ist noch drin.** Ein Katalog ist das ganze Verzeichnis des Ladens — hätte der
  Lauf nur die neue App genannt, wäre die alte aus dem Laden verschwunden. Dass beide Pakete im
  Ordner liegen, ist das, was sie beide in den Katalog bringt.
- **Die Kennung des Schlüssels ist dieselbe** (`kid RPmRtyPJs5WQECAkiPeBsg`). Mit einem anderen
  Schlüssel wäre der Katalog für diese Anlage wertlos.
- **Die Uhr trägt kein `needs`.** Sie fragt nichts an, und der Katalogeintrag sagt das von
  selbst.

Und der Abruf **über Netz**, von der API-Maschine aus, mit geprüftem Zertifikat:

```
curl -4 https://store.ngc.mein.lan/catalog.json   http=200 verify=0 bytes=1116
```

Das ist der Teil, den die Einzel-VM nie prüfen konnte, weil der Laden dort danebenlag.

---

## 6. Ohne Laden: ein Paket hochladen

Für die eigene App braucht man **keinen** Laden. Die App „App-Store" hat einen Reiter
**„Hochladen"** — „Ein Paket von der Festplatte einrichten." Dahinter steht
`ngc.store/api/upload` (`apps/server-api/classes/appstore/upload.go`): die API liest das Paket
in einem Durchgang, hasht es dabei, läuft es sicher durch und antwortet mit dem, was sie
gelesen hat. Die Shell rechnet dieselben Werte aus der Datei nach, die sie geschickt hat, und
lehnt einen Unterschied ab — nichts an dieser Antwort muss also geglaubt werden.

Solche Apps tragen als Laden `local` (`trust.StoreLocal`), und `local` ist **nie** ein Laden in
der Liste. Der Preis steht in der Oberfläche: eine App, die nicht aus einem Laden kam, bekommt
ihre nächste Fassung auch nicht von dort — „Bitte die neue Fassung unter ‚Hochladen' einrichten."

Das ist der kürzere Weg und für die zweite App der richtige: er rührt
keinen Laden an, der schon läuft, und braucht keinen geheimen Schlüssel (Befund 2).

---

## 7. Einrichten und öffnen

Beides macht ein **Mensch** in der Oberfläche, und beides wird persönlich unterschrieben
(Kennwort- oder Passschlüsselfrage). Das ist keine Lücke, das ist der Entwurf: was eine App
darf, entscheidet niemand automatisch.

1. Anmelden, „App-Store" öffnen.
2. Reiter „Apps" (aus einem Laden) oder „Hochladen" (Paket von der Platte).
3. **„Installieren"** → die Oberfläche zeigt, was die App anfragt (`needs`).
4. **„Aktivieren"** → der Verwalter erlaubt die `needs`. Hier und nur hier werden sie erlaubt.
5. Persönlich unterschreiben.
6. Rechte verteilen: in „Benutzer & Rechte" bekommt jede Rolle, die die App sehen soll,
   **`<app>/use`** — und je Aktion das Recht dazu (Stolperstein 3).
7. Fenster öffnen, App benutzen.

### Stand der Messung

**Gemessen am 24.10.2026:** die Schritte 1 bis 5 dieses Papiers — Manifest, Abbild, Dienst im
Abbild, Paket, unterschriebener Katalog, `check -store`. Dazu vier Fälle in
`apps/uhr/classes/uhr_test.go`.

**Gemessen auf der verteilten Anlage:** die Uhr liegt im laufenden Laden
(`store.ngc.mein.lan`, Kennung `global`, Folge 7), neben dem Terminal und mit derselben
Unterschrift, und der Katalog wird von der API-Maschine **über Netz** geholt, mit geprüftem
Zertifikat (`http=200 verify=0`). Siehe Schritt 5, „Gemessen: eine zweite App in einen
laufenden Laden".

**Versucht und gescheitert:** die Uhr **einrichten**. Die Oberfläche zeigt sie, der Dialog
fragt nur nach dem Rechner (sie will keine Rechte), und „Aktivieren" endet mit „Das hat der
Rechner nicht angenommen" — weil der Tunnel des Controllers in derselben Sekunde abbricht
(Befund 9). Das ist der Stand, und er ist das Ergebnis dieser Runde: der Weg trägt bis **in**
den Laden und bis **in** die Oberfläche, und reißt beim Einrichten.

**Nicht gemessen:** dass die Uhr sich einrichten und öffnen lässt und dass ihr Fenster die
Zeit zeigt. Dazu gehört die Anmeldung eines Verwalters, und die hatte diese Sitzung nicht. Bis
das gelaufen ist, sind Schritt 2 und Schritt 7 aus dem Code gelesen und nicht am Gerät geprüft —
und dieses Papier sagt es hier, statt es wie Erledigtes aussehen zu lassen.

**Und eines liegt dazwischen:** die Anlage holt den Katalog von selbst nur **stündlich**.
Zwischen „die App ist im Laden" und „die App steht in der Oberfläche" liegt also bis zu eine
Stunde, ohne dass etwas kaputt ist — wer in dieser Zeit sucht, sucht einen Fehler, den es nicht
gibt. Warten muss man aber nicht: der Reiter „Quellen" hat einen Knopf **„Jetzt nachsehen"**,
und er sagt daneben, wie alt der Stand ist („Zuletzt geprüft: …"). Gemessen am 24.10.2026: die
Folge 7 war um 13:44 von selbst geholt, und die Uhr stand danach im Reiter „Apps".

---

## 8. Die vier Stolpersteine des Terminals

Jeder davon hat eine Runde gekostet. Jeder steht hier mit der Zeile, die ihn macht.

### 1. Kein `./`-Eintrag

Ein tar, das mit `tar -cf x.tar .` entsteht, trägt einen Eintrag `./`. Der fällt durch:
`apps/cli/classes/contents.go` räumt jeden Namen mit `path.Clean` auf, `./` wird zu `.`, und
`store.ValidPath(".")` ist falsch — der erste Buchstabe eines Pfades muss aus
`[A-Za-z0-9_-]` sein. Die Meldung lautet dann:

> `… holds the name "./", which no installation unpacks`

Dasselbe gilt im Paket selbst: `store.Walk` nimmt als Verzeichnis nur `ui`, `services` und
etwas unter `ui/`. Ein `./` ist dort `store: invalid app package`.

**Darum:** nie `tar -cf … .`, sondern die Namen nennen — oder gar nicht selbst packen und
`ngc-store pack` nehmen, das keine Verzeichniseinträge schreibt.

### 2. Das Abbild entsteht auf einem Baurechner

Nie auf dem Zielserver. Die drei Gründe stehen in Schritt 3 und ausführlich in
`node/image.go`. Der kürzeste: das Abbild ist Teil des **Pakets**, und ein Paket ist
unterschrieben. Ein Abbild, das auf dem Server entsteht, ist eines, das niemand unterschrieben
hat.

### 3. Eine eingerichtete App ist unsichtbar, solange niemand „benutzen" hat

Das Recht heißt `<app>/use` und steht in **keinem** Manifest. Ohne es sieht ein Nutzer die App
nicht — nicht im Startmenü, nirgends (`rights.usable`).

Eine Feinheit, die das Suchen verkürzt: die Rolle **Verwalter** bekommt `use` und jede Aktion
jeder eingerichteten App **von allein** (`rights.adminApps`), damit das erste Recht einer neuen
App überhaupt jemandem gegeben werden kann. Es steht in keiner Zeile und kann darum auch nicht
weggespeichert werden. Wer also als Verwalter prüft, sieht die App **immer** — und hat damit
über jeden anderen Nutzer nichts gemessen. Dafür braucht es einen zweiten Benutzer, und der ist
auf der Einzel-VM bis heute nicht gemessen.

### 4. Die Ausgabe muss wirklich ankommen — beide Richtungen prüfen

Das ist der teuerste von den vieren. Beim Terminal kam `stderr` an und `stdout` nicht, und das
sah aus wie „die Ausgabe geht", weil im Protokoll Zeilen standen.

Die zwei Richtungen gehen an **verschiedene** Orte:

| Richtung | Wohin | Wer sieht es |
|---|---|---|
| `stdout` | über den Kanal ins **Fenster der App** (`serve.CallInput.Out`; `ngc.call` löst damit auf) | der Mensch vor dem Fenster |
| `stderr` | ins **Protokoll des Controllers**, höchstens 64 KiB | nur ein Betreiber — **und nur, wenn er den Text eingeschaltet hat** |

Von `stderr` schreibt der Controller **standardmäßig nur, dass es welches gab und wie viele
Byte** — nicht den Text (`serve.CallInput.LogStderr`, aus als Vorgabe). Der Grund ist gut: ein
Dienst hat `NGC_PARAMS` in seiner Umgebung, und einer, der seine Umgebung oder einen Fehler mit
den Parametern darin schreibt, legte das, was der Nutzer getippt hat, ins Protokoll des Servers.

**Also:** ein Dienst, der seine Fehler auf `stderr` meldet, hat dem Server etwas über sich
erzählt und dem Menschen vor dem Fenster **nichts**. Was der Mensch sehen soll, gehört auf
`stdout`.

Die Uhr misst genau das. Ihre Aktion `zeit` schreibt mit Absicht in **beide** Richtungen
(`apps/uhr/classes/uhr.go`), damit man sieht, welche wo ankommt:

```sh
podman run --rm --arch amd64 -e NGC_ACTION=zeit localhost/ngc/uhr:1.0.0 \
  /usr/lib/ngc/ngc-uhr > out.txt 2> err.txt
```

**Gemessen am 24.10.2026**, im Abbild:

```
exit=0
out.txt   2026-10-24T09:37:39Z
err.txt   ngc-uhr 1.0.0 beantwortet "zeit"
```

Zwei Fälle halten das auch ohne Behälter fest, und einer davon prüft das Gegenteil mit:
`TestZeitAnswersOnStdout` und `TestZeitAlsoWritesStderr` — der zweite geht rot, wenn die Zeit
je **auch** auf `stderr` landet, denn dann lassen sich die Richtungen nicht mehr
unterscheiden.

Was damit **noch nicht** gemessen ist: dass die Zeile auch durch den Kanal bis ins Fenster
kommt. Das ist dieselbe Lücke wie in Schritt 7.

---

## 9. Das Icon der App

Das Manifest kennt ein Feld `icon`: ein Pfad **unterhalb von `ui/`**, genau wie `ui.entry`
(`shared/store/manifest.go:43`). Es ist freiwillig — beide heutigen Apps, Terminal und Uhr,
lassen es weg.

### Was heute wirklich damit passiert

Der Weg des Feldes ist kurz und endet früher, als man denkt:

1. **Paket.** `pack` verlangt, dass die Datei im ui-tar wirklich liegt: ein `icon`, zu dem
   `ui/<pfad>` fehlt, lässt das Packen scheitern (`shared/store/pack.go:131`). Der Pfad muss
   `ValidPath` sein — relativ, keine versteckte Datei, kein `..`
   (`shared/store/manifest.go:137`).
2. **API.** Der Schreibtisch reicht den Wert unverändert an die Oberfläche weiter, als
   `core.DesktopApp.Icon` (`apps/server-api/classes/coreapp/desktop.go:171`).
3. **Oberfläche.** Und dort hört es auf. **Die Oberfläche liest `icon` nirgends.** Jede App
   bekommt im Startmenü und auf dem Schreibtisch dasselbe Schild mit dem **ersten Buchstaben
   ihres Namens** — `Desktop.glyph()`, `apps/web/public/assets/js/shell/desktop.js:479-483`;
   der Fenstertitel nimmt denselben Buchstaben
   (`apps/web/public/assets/js/shell/desktop.js:562`).

Für einen App-Bauer heißt das heute: **ein `icon` im Manifest ändert am Bild nichts.** Ob das
Feld leer ist oder gefüllt — gezeigt wird der Buchstabe. Das ist kein Versehen, sondern so
beschlossen, und der Kommentar an Ort und Stelle sagt warum: das Icon ist eine Datei aus dem
ui-tar und darf erst gezeigt werden, wenn dessen Beweis stimmt (Aufgabe 22) — „ein Bild, das
nicht bewiesen ist, ist ein Bild, das jemand anderes ausgesucht hat".

### Der Satz, den es gibt

`apps/web/public/assets/icons/` — zwölf SVGs, alle nach derselben Regel gezeichnet:

| Datei | wofür |
| --- | --- |
| `terminal.svg` | Terminal |
| `uhr.svg` | Uhr |
| `laden.svg` | App-Store |
| `benutzer.svg` | Benutzer und Rechte |
| `platzhalter.svg` | App ohne eigenes Icon |
| `rechte.svg` | Rechte, Freigaben, Bestätigungen |
| `einstellungen.svg` | Einstellungen |
| `dateien.svg` | Dateien |
| `knoten.svg` | Knoten einer Anlage |
| `rechner.svg` | Rechner (`fake-link.js`) |
| `nicht-erreichbar.svg` | Zustand `unreachable` |
| `keine-lizenz.svg` | Zustand `unlicensed` |

Die letzten beiden sind da, weil der Schreibtisch diese zwei Zustände schon kennt und heute
nur blass zeichnet (`.dicon.off`, `shell.css:84`); die mittleren vier, weil jede dieser Stellen
in NGC bereits existiert und sonst beim nächsten Mal wieder einzeln gezeichnet würde.

**Die Regel des Satzes** — wer ein dreizehntes zeichnet, hält sie ein:

* Zeichenfläche `viewBox="0 0 24 24"`, nichts innerhalb von 2 Einheiten am Rand.
* Strichstärke `1.75`, runde Enden und Ecken, `fill="none"`.
* `stroke="currentColor"` am Wurzel-Element, keine festen Farben.
* Zwei nebeneinander laufende Striche mindestens 2,5 Einheiten auseinander — alles
  Engere verschwindet bei 24 px.
* Keine Schrift, keine Verläufe, keine Filter.

Geprüft wurde das nicht nach Gefühl: **`apps/web/public/assets/icons/uebersicht.html`** stellt
alle zwölf nebeneinander, hell und dunkel, in 24, 32 und 48 px, dazu eine Zeile, wie sie im
Startmenü stünden. Die Datei im Browser öffnen, mehr braucht es nicht. Drei Icons
(Einstellungen, Rechner, Keine Lizenz) sind nach dem ersten Blick auf diese Seite neu
gezeichnet worden, weil sie bei 24 px zugelaufen sind.

### Hell und dunkel

Der Schreibtisch hat ein dunkles Thema (`data-theme`). Ein Icon mit fester Farbe ist auf einem
der beiden Gründe falsch. Deshalb `currentColor`: das Icon nimmt die Textfarbe der Stelle an,
an der es steht, und ist damit in beiden Themen richtig, ohne zweite Datei.

Das gilt aber **nur, wenn das SVG in der Seite steht**. Ein `<img src="icon.svg">` ist ein
eigenes Dokument und erbt nichts — `currentColor` wird dort schwarz. Wer ein Icon als `<img>`
oder als CSS-Hintergrund lädt, braucht entweder zwei Dateien oder eine feste Farbe, die auf
beiden Gründen trägt. Innerhalb der eigenen App-Oberfläche ist das die Entscheidung des
App-Bauers.

### Wie ein App-Bauer eins benutzt

Innerhalb der eigenen Oberfläche sofort und ohne Bedingung: Datei nach `ui/` kopieren und in
`index.html` einsetzen.

Als **App-Icon** im Manifest:

```
<app-ordner>/
  manifest.json      "icon": "icon.svg"
  ui/
    index.html
    icon.svg         <- hierher, der Pfad ist relativ zu ui/
```

```json
{ "id": "org.sorglos.uhr", "name": "Uhr", "icon": "icon.svg", "ui": { "entry": "index.html" } }
```

Grenzen, die heute wirklich gelten:

* **Pfad:** relativ, unterhalb `ui/`, kein führender `/`, kein `..`, keine versteckte Datei
  (`ValidPath`). Die Datei muss im Paket liegen, sonst packt `pack` nicht.
* **Größe:** für `icon` selbst steht keine Zahl im Code. Die Grenze ist die des ui-tars, und
  eine eigene Zahl erfände dieses Papier. Praktisch: die zwölf Dateien hier sind zwischen
  290 und 620 Bytes groß. **Mehr als 4 KB ist für ein Strich-Icon kein Icon mehr**, das ist
  die Hausregel dieses Satzes, keine Prüfung des Systems.
* **Inhalt:** nur Form — Pfade, Kreise, Rechtecke. **Kein `<script>`, kein `<foreignObject>`,
  kein `<image>`, kein `xlink:href` oder `href` nach außen, keine Schriftart von außen.** Ein
  SVG ist ein Dokument und kann all das. Ob jemand es prüft, steht gleich darunter.
* **Wirkung:** heute keine. Siehe Befund 11.

---

## Befunde

Stellen, an denen der Weg einen Handgriff braucht, den kein Fremder erraten kann. **Keiner
davon ist hier gebaut** — jeder steht als Befund mit Vorschlag.

### Befund 1: `ngc-controller image` baut nur das Terminal

`ngc-controller image -arch … -out …` ist der einzige sichtbare Schritt zu einem Abbild, und er
ist auf das Terminal festgenagelt: `terminalDir` ist
`apps/server-controller/templates/terminal`, der Name des Abbilds ist `ngc/terminal:<Fassung>`,
und der Zusammenhang des Baus ist `build/` im Stamm des Repos
(`apps/server-controller/classes/node/image.go`). Wer eine **eigene** App baut, kann den Befehl
nicht benutzen und muss podman von Hand aufrufen — was dieses Papier in Schritt 3 tut, und was
der Portierversuch (Punkt 68) mit demselben Satz tut: „mit podman von Hand, weil
`ngc-controller image` nur das Terminal baut".

Die Hilfe des Befehls sagt das auch („Abbild der Terminal-App schreiben"), aber sie sagt nicht,
wie man es für etwas anderes macht.

**Vorschlag:** einen Unterbefehl, der einen App-Ordner nimmt statt des Terminals — etwa
`ngc-store image -arch <arch> -dir <app-ordner>`, der `manifest.json` daneben liest, das Abbild
`<id>:<fassung>` nennt und `services/<arch>.tar` schreibt. Die Prüfungen, die es dafür schon
gibt, sind genau die, die man will: Basis hängt an einem Digest (`runtime.CheckBase`) und das
Programm im Zusammenhang passt zur Architektur (`runtime.checkProgram`). Damit wären die drei
Stellen aus Befund 10 auf eine Angabe zusammengezogen.

Nebenbei: `node/image.go` sagt von sich selbst „WRITTEN AND COMPILED, NOT RUN" — auf dem
Rechner, auf dem es geschrieben wurde, gab es kein podman. Ein Baurechner mit podman gibt es
inzwischen (Debian unter WSL, 5.4.2), und dieses Papier hat darauf gebaut — aber mit podman
direkt, nicht über diesen Befehl. **Der Befehl ist weiterhin nie gelaufen.**

### Befund 2: Wo der geheime Ladenschlüssel liegt, steht nirgends als Regel

Der Laden der Einzel-VM (`"store": "global"`, Folge 1, eine App) ist mit dem Schlüssel
unterschrieben, dessen öffentlicher Teil in `/etc/ngc/api.json` als `storeKey` steht
(Kennung `WM0yymuJtd9UxaUONGIXow`). Der **geheime** Teil liegt nicht auf der Maschine: was
dort unter `/home/berry/ngcapp/store/store-key.pem` liegt, ist der **öffentliche** Teil —
`store-key.pem` ist der Name, unter dem `ngc-store catalog` den öffentlichen Teil neben den
Katalog legt (`apps/cli/classes/check.go`, `StoreKeyFile`). Auf dem Baurechner liegt unter
`/root/ngc-keys/` nur `release.key`, kein Ladenschlüssel.

**Die Folge:** dem globalen Laden **der Einzel-VM** kann niemand mehr eine zweite App
hinzufügen. Jeder neue Katalog bräuchte Folge 2 und dieselbe Unterschrift, und die kann niemand
mehr leisten. Weiter gesucht habe ich nicht: ein Dateisystem nach geheimen Schlüsseln zu
durchsuchen ist genau das, was man nicht tun soll.

**Auf der verteilten Anlage ist es nicht so**, und der Unterschied ist der Befund. Dort liegt
der geheime Schlüssel auf der Laden-Maschine unter `/root/store/store.key`, weil
`tools/proxmox-sechs-ngc.sh` ihn dort anlegt — und darum konnte die zweite App dort wirklich in
den Laden (Schritt 5). Der Ort steht also in einem Aufsetz-Skript für **einen** Prüfstand, und
sonst nirgends: nicht als Regel, nicht in der Anleitung zum Betrieb eines Ladens, nicht neben
der Regel für den Release-Schlüssel. Wer die Einzel-VM aufsetzte, hat ihn an einer anderen
Stelle gesucht und verloren.

**Vorschlag:** zwei Dinge, und das erste ist das wichtigere.

1. **Festlegen, wo Ladenschlüssel liegen, und es aufschreiben** — als Regel, nicht als Zeile in
   einem Skript. Für Release-Schlüssel gibt es den Ort schon (`/root/ngc-keys/release.key` auf
   dem Baurechner) und die Regel, dass der echte Release-Schlüssel auf einem Hardware-Stick
   liegt (Entwurf 4.8). Für Ladenschlüssel gibt es die Regel nicht, nur zwei Prüfstände, die es
   verschieden gemacht haben. Ein Laden, dessen Schlüssel weg ist, ist ein Laden, der nie wieder
   eine App bekommt — und das merkt man erst bei der zweiten.
2. Für die Einzel-VM: entweder einen neuen Ladenschlüssel ziehen und `storeKey` in
   `/etc/ngc/api.json` tauschen (Root, API neu starten), oder den globalen Laden lassen, wie er
   ist. Das Zweite rührt nichts an, was läuft, und ist darum das, was ich vorschlage — zumal der
   Weg jetzt auf der verteilten Anlage belegt ist.

### Befund 3: Der Laden der Einzel-VM steht in einem Heimatverzeichnis

Was den Laden der Einzel-VM ausliefert, liegt in `/home/berry/ngcapp/`: ein Skript, das eine
eigene CA und ein Zertifikat für `store.ngc.test` baut, und eines, das die CA in den
System-Vertrauensspeicher legt, den Namen in `/etc/hosts` schreibt, die Dateien nach
`/srv/ngc-store` kopiert und einen Apache-vhost auf 443 anlegt. Beide sind sauber geschrieben
und beide prüfen sich selbst. **Keines von beiden ist im Repo.** Wer die Einzel-VM neu
aufsetzt, baut das noch einmal aus dem Kopf — oder gar nicht.

**Vorschlag:** die beiden Skripte nach `tools/` holen (etwa `tools/pruefladen-tls.sh` und
`tools/pruefladen-stellen.sh`), mit den Pfaden als Angaben statt festgenagelt. Sie gehören in
kein Release — sie bauen einen **Prüf**laden —, aber sie gehören dorthin, wo man sie findet.

### Befund 4: `ngc-store` liegt nicht auf dem Server

`ngc-store` soll mit der API ausgeliefert werden: das Release-Archiv `ngc-api-amd64.tar` trägt
„genau die Dateien `ngc-api` und `ngc-store`", und `ngc-store contents` prüft das. Auf der
Einzel-VM liegt in `/usr/lib/ngc/` nur `ngc-api` und `ngc-controller` — `ngc-store` ist
nicht da und steht in keinem `PATH`. Für diesen Weg habe ich es selbst übersetzt und in einen
eigenen Ordner gelegt (`~/uhr-bau/`).

Ob das Release es wirklich nicht mitbringt oder ob die Einzel-VM von Hand bestückt wurde,
ist **nicht gemessen** — die Maschine ist über Monate von Hand gewachsen. Gemessen ist nur:
heute ist es nicht da.

**Vorschlag:** einmal messen, ob ein Release nach `ngc-api.sh` wirklich beide Programme ablegt.
Wenn nicht, ist das ein Fehler der Auslieferung; wenn doch, ist es ein Beleg mehr dafür, dass
die Einzel-VM ein Handbau ist.

### Befund 5: Jeder Name der Anlage zeigt zusätzlich auf `::`

Gemessen am 24.10.2026 auf api-1: `store.ngc.mein.lan` löst auf **zwei** Adressen auf —
`10.2.3.215` und `::`, die unbestimmte Adresse. Ein Client, der IPv6 vorzieht, und das ist die
Vorgabe, landet damit nicht auf der Laden-Maschine, sondern auf der Maschine, von der er fragt:

```
curl -4 https://store.ngc.mein.lan/catalog.json   http=200 verify=0 bytes=1116
curl -6 https://store.ngc.mein.lan/catalog.json   http=000 verify=18
curl    https://store.ngc.mein.lan/catalog.json   scheitert
```

Und das Zertifikat, das dabei zurückkommt, ist das der API selbst (`CN=ngc.mein.lan`,
selbstunterschrieben) — nicht das des Ladens. Die Fehlersuche führt darum erst einmal in die
falsche Richtung: es sieht aus wie eine kaputte CA oder ein falsches Ladenzertifikat. Beides war
in Ordnung, `openssl s_client` von derselben Maschine sagte `Verify return code: 0 (ok)`.

Dass die API den Katalog heute trotzdem holt, liegt daran, dass Go eine zweite Adresse
nachprobiert, wenn die erste nicht antwortet (RFC 6555). Das ist Verlass auf einen Rückfall, kein
Zustand.

**Nicht gemessen:** woher das `::` kommt — welcher Eintrag im Namensdienst oder in `/etc/hosts`
es setzt, habe ich nicht verfolgt, und auf einer Maschine, auf der andere messen, wollte ich es
nicht ändern.

**Vorschlag:** den `::`-Eintrag für diese Namen loswerden. Solange er steht, ist jede Messung am
Laden eine Messung des Rückfalls mit, und eine Gegenprobe („was passiert, wenn der Laden weg
ist") trifft womöglich die falsche Maschine.

### Befund 6: Die erste Anmeldung scheitert immer, die zweite nicht verlässlich

Gemessen am 24.10.2026 auf `ngc.mein.lan`, in **einem** Browserprofil, auf **einer** Seite,
mit demselben Namen und demselben Kennwort:

```
Lauf A   Versuch 1: nein   Versuch 2: JA
Lauf B   Versuch 1: nein   Versuch 2: JA
Lauf C   Versuch 1: nein   Versuch 2: JA
Lauf D   Versuch 1: nein   Versuch 2: JA
Lauf E   Versuch 1: nein   Versuch 2: nein   Versuch 3: JA
```

Fünf Läufe. Der erste Versuch scheitert **immer**; der zweite geht **meistens**. Es ist also
kein „man muss es zweimal machen", sondern **unzuverlässig** — und das ist schlimmer, weil man
sich darauf auch nicht einrichten kann.

Thomas kannte das Bild und hielt es für die zwei APIs der Anlage — das ist es **nicht**: die
Shell redet nicht mit `api-1` oder `api-2` unmittelbar, sondern über `ngc.mein.lan/api/v1/ws`,
in jedem Versuch über denselben Weg.

**Nicht gemessen:** woran es liegt. Der Verdacht geht auf die Vertrauensliste — der erste
Versuch holt die Kette erst (`openTrustFromChain`, „login: no trust list") und scheitert dabei,
der zweite findet sie vor. Das ist eine Vermutung, keine Messung, und Lauf E passt nicht
vollständig dazu: wäre es nur das Holen der Kette, müsste der zweite Versuch immer gehen.

**Warum es mehr ist als ein Schönheitsfehler:** wer sich zum ersten Mal anmeldet, bekommt den
Satz für ein falsches Kennwort. Er wird sein Kennwort für falsch halten, es noch einmal tippen —
und dann geht es, ohne dass er weiß, warum. Ein Mensch lernt daraus „man muss es zweimal
machen"; ein Skript lernt es nicht und meldet eine falsche Anmeldung.

**Vorschlag:** eigener Lauf. Zuerst messen, ob der erste Versuch die Vertrauensliste holt und
daran scheitert; wenn ja, sie holen, **bevor** die Maske eine Anmeldung annimmt.

### Befund 7: Ein Name, der nicht in die Grammatik passt, bekommt den Satz für ein falsches Kennwort

`http.loginStart` prüft den Namen im **Browser**, bevor es etwas sendet:

```js
if (!validLoginName(name)) throw new ApiError(wire.CODES.PROTOCOL);
```

Erlaubt sind nur Kleinbuchstaben, Ziffern und `. _ -` (das erste Zeichen ein Buchstabe oder eine
Ziffer, `contracts/text.js`, `trust.ValidLoginName`). Ein Name mit einem Großbuchstaben, einem
`@`, einem Leerzeichen oder einem Umlaut wird also **lokal** abgelehnt, und die Maske sagt
„Anmeldung fehlgeschlagen." — der Satz für ein falsches Kennwort.

Gemessen am 24.10.2026, weil es mich selbst eine Runde gekostet hat: drei Versuche, **kein
einziger Aufruf** an eine API (im Protokoll beider APIs kein Eintrag), weil im Namen ein
Leerzeichen stand. Das Fehlerbild sieht aus wie ein Netz- oder Kennwortproblem und ist eine
Zeichenprüfung.

Der Code beklagt diese Verwechslung an zwei anderen Stellen ausdrücklich — für eine Maschine
ohne Passwortschlüssel und für einen Passschlüssel, den es nicht gibt, steht dort „the same lie
as above". An dieser dritten Stelle macht er sie.

**Vorschlag:** für einen Namen außerhalb der Grammatik einen eigenen Satz, der sagt, was ein
Name sein darf. Er verrät nichts: die Grammatik ist öffentlich und sagt nichts darüber, wer
existiert — genau das steht als Begründung über `loginStart`.

### Befund 8: „Der Katalog dieses Stores ist nicht echt" für einen Katalog, der echt ist

Gemessen am 24.10.2026: ein frisch geöffnetes Fenster zeigte im Reiter „Apps" den Satz

> Der Katalog dieses Stores ist nicht echt.

Der Katalog auf der Laden-Maschine war dabei **unberührt** — dieselbe Datei von 11:33, dieselbe
Schlüsselkennung, `ngc-store check -store global` grün —, und **sechs Minuten vorher** hatte
dasselbe Fenster beide Apps korrekt aufgelistet. Es ist also der Satz für „dieses Fenster hat
den Katalog noch nicht geholt", nicht für einen gefälschten Katalog.

Wer das liest, hält seinen Laden für kompromittiert. Das ist die teuerste Fehlmeldung von
allen, die hier stehen: sie beschuldigt.

Behoben ist es auf dem Zweig `ngc-ersteapp` („Stop calling a store a forger when no catalog was
fetched yet"); in der 0.2.1, die auf der Anlage läuft, ist der Satz noch da.

**Umgehen:** im Reiter „Quellen" erst **„Jetzt nachsehen"** drücken, dann in „Apps" gehen.

**Vorschlag:** den Zweig `ngc-ersteapp` ansehen und einspielen. Er ist klein (drei Dateien) und
behebt genau das.

### Befund 9: Beim Aktivieren bricht der Tunnel des Controllers ab

Das ist der Befund, an dem der Weg heute **endet**. Gemessen am 24.10.2026, 12:14:38, beim
Klick auf „Aktivieren" für die Uhr auf `svc-1`:

Die Oberfläche sagte:

> Das hat der Rechner nicht angenommen. Bitte es noch einmal versuchen.

Und auf `svc-1` sagte der Controller in derselben Sekunde:

```
WARN tunnel failed api=wss://api-1.ngc.mein.lan:7443/tunnel
     err="link: the API broke the contract of the tunnel"
INFO a service wrote to stderr app=org.sorglos.terminal service=terminal action=shell bytes=530
WARN Die Verbindung einer App wurde abgebrochen. app=org.sorglos.terminal code=E_PROTOCOL
INFO tunnel up api=wss://api-1.ngc.mein.lan:7443/tunnel
```

Der Tunnel kam eine Sekunde später zurück, aber die Einrichtung war gescheitert — **und es riss
eine laufende Terminal-Sitzung mit**, die nichts damit zu tun hatte.

**Was daran gesichert ist:** die API wurde in diesem Moment **nicht** neu gestartet (im
Journal zwischen 12:08 und 12:20 kein `Stopping`/`Started`), der Abbruch hängt also am
Aktivieren und nicht an einem Neustart.

**Was daran nicht gesichert ist:** es ist **eine** Messung, auf der API-Fassung, die bis 12:20
lief. Danach hat eine andere Sitzung 0.2.2 eingespielt; ob es dort noch auftritt, ist **nicht
gemessen**. Und warum die API den Vertrag bricht, ist auch nicht gemessen — nur dass der
Controller es so nennt.

**Warum es wichtig ist:** das ist die Station, die vorher niemand erreichen konnte, weil es nur
eine App gab. Die erste Einrichtung einer zweiten App trifft sie sofort.

**Vorschlag:** eigener Lauf gegen 0.2.2, mit dem Paket der Uhr, das dafür bereitliegt. Wenn es
noch auftritt: die Stelle, an der der Controller den Vertragsbruch feststellt
(`link`), sagt, welche Nachricht ihn auslöst — damit hat die Suche einen Anfang.

### Befund 10: Die Architektur steht an drei Stellen

`GOARCH` beim Übersetzen, `--arch` bei `podman build`, und der Name `services/<arch>.tar`. Wer
eine davon vergisst, bekommt ein Abbild, das baut, sich sichern, laden und einrichten lässt und
erst auf dem Server mit einer Meldung über ein Format stirbt — nach dem Katalog.
`runtime.checkProgram` fängt genau das ab, indem es den ELF-Kopf des Programms im Zusammenhang
liest; es läuft aber nur in `ngc-controller image`, also nur für das Terminal (Befund 1).

**Vorschlag:** mit Befund 1 erledigt. Ein Unterbefehl, der den App-Ordner nimmt, nennt die
Architektur einmal und schreibt den tar-Namen selbst.

### Befund 11: `icon` wird getragen und nie gezeigt

Das Manifest-Feld `icon` geht den ganzen Weg — `pack` prüft, dass die Datei im ui-tar liegt
(`shared/store/pack.go:131`), der Schreibtisch reicht es als `core.DesktopApp.Icon` an die
Oberfläche (`apps/server-api/classes/coreapp/desktop.go:171`) — und **kommt nirgends an**. In
`apps/web/public/assets/js/` liest kein einziger Zugriff dieses Feld. Gezeichnet wird in jedem
Fall ein Buchstabenschild: `Desktop.glyph()`,
`apps/web/public/assets/js/shell/desktop.js:479-483`.

Die gute Nachricht zuerst, weil sie die wichtigere ist: **es wird heute kein fremdes SVG in die
Oberfläche eingebettet.** Die Stelle, an der ein ungeprüftes Bild aus einem fremden Paket in
die Seite des Schreibtischs geraten könnte, existiert nicht. Das ist kein Zufall — der
Kommentar über `glyph()` sagt genau das und hängt das Zeigen an den Beweis des ui-tars
(Aufgabe 22).

Der Befund ist die andere Hälfte: das Feld **sieht benutzbar aus und ist es nicht.** Ein
App-Bauer, der `"icon": "icon.svg"` ins Manifest schreibt, bekommt keinen Fehler, kein
Schweigen im Protokoll, keinen Hinweis — er bekommt ein gepacktes, installiertes,
funktionierendes Paket, in dem sein Icon nie auftaucht. Dieses Papier war die einzige Stelle,
an der das stand; ohne sie muss man es messen, um es zu wissen.

**Vorschlag:** nicht jetzt bauen, sondern an Aufgabe 22 hängen. Wenn das Zeigen kommt, gehört
dazu — und zwar bevor die erste fremde Datei in die Seite geht:

* Das SVG darf **nur aus dem ui-tar** kommen, dessen Beweis schon geprüft ist, nie aus einer
  URL und nie aus dem Manifest selbst.
* Es gehört **nicht** als `innerHTML` in die Seite. Entweder in einem Rahmen (`<img>` aus einem
  `blob:`-Verweis, der weder Skript noch Verweis nach außen ausführt), oder, wenn es die Farbe
  erben soll, nur nach einer Prüfung, die alles außer Form verwirft: `<script>`,
  `<foreignObject>`, `<image>`, `<use>`, jedes `href`/`xlink:href`, jedes `on…`-Merkmal.
* Scheitert die Prüfung, gilt dieselbe Regel wie bei der App-Liste: **nicht das halbe Bild
  zeigen, sondern das Buchstabenschild** — das Verhalten von heute ist der sichere Rückfall.
* Der Platzhalter dafür ist gezeichnet: `apps/web/public/assets/icons/platzhalter.svg`.

Solange das nicht da ist, ist der Satz in `apps/web/public/assets/icons/` das, wofür er
gemacht ist: die Icons der **Oberfläche selbst** und eine Vorlage, die ein App-Bauer in seine
eigene `ui/` kopiert — innerhalb seiner App wirkt sie sofort.

---

## Nachstellen

Alles in einem Zug, aus dem Stamm des Repos. Schritt 2 braucht einen Baurechner mit podman;
auf diesem Rechner ist das die Debian-WSL.

```sh
# 1. das Programm des Dienstes, für den Zielserver
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=false -trimpath \
  -ldflags "-X main.Version=1.0.0" -o apps/uhr/build/ngc-uhr ./apps/uhr

# 2. das Abbild, auf dem Baurechner
cd apps/uhr
podman build --arch amd64 --file templates/Containerfile --tag ngc/uhr:1.0.0 --pull=always build
mkdir -p templates/services && rm -f templates/services/amd64.tar
podman save --format docker-archive --output templates/services/amd64.tar ngc/uhr:1.0.0
# Gegenprobe, beide Richtungen:
podman run --rm -e NGC_ACTION=zeit localhost/ngc/uhr:1.0.0 /usr/lib/ngc/ngc-uhr >out.txt 2>err.txt
cd ../..

# 3. das Paket
go run ./apps/cli pack apps/uhr/templates /pfad/laden/uhr-1.0.0.ngc

# 4. der unterschriebene Katalog und seine Prüfung
go run ./apps/cli catalog /pfad/laden /pfad/laden-key.seed uhr-probe 1 https://store.ngc.test/uhr
go run ./apps/cli check -store uhr-probe /pfad/laden

# 5. in einen laufenden Laden: Paket neben die anderen legen und den Katalog
#    mit EINER höheren Folge neu unterschreiben - alle Apps, nicht nur die neue.
#    Auf der Laden-Maschine:
#      cp -n /var/www/store/catalog.json /root/store/catalog-vor-<app>-<datum>.json
#      ngc-store catalog /var/www/store /root/store/store.key global <folge+1> https://store.<zone>
#      ngc-store check -store global /var/www/store
#    Gegenprobe von der API-Maschine aus, gegen den NAMEN und über IPv4:
#      curl -4 https://store.<zone>/catalog.json -o /dev/null -w "%{http_code} %{ssl_verify_result}
"

# 6. einrichten: in der App „App-Store" unter „Apps" (aus dem Laden) oder
#    „Hochladen" (Paket von der Platte), dann „Installieren", „Aktivieren",
#    persönlich unterschreiben, dann in „Benutzer & Rechte" das Recht
#    <app>/use verteilen. Der Laden wird von selbst nur stündlich geholt -
#    unter "Quellen" holt ihn "Jetzt nachsehen" sofort.
```
