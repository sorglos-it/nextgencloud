# Ein kleines NGC-System aufsetzen

Ein Rechner. Alles drauf: API, Controller, Weboberfläche, SQLite.
Für jemanden, der NGC noch nie gesehen hat.

**Woher das hier kommt:** jeder Schritt steht in den Vertragspunkten 47, 51, 56, 57, 58, 59,
61 oder im Code. Was dort nicht steht, steht unten unter „Nicht gemessen".

**Gemessen auf:** Debian 13 (trixie), Apache 2.4.69, PHP 8.4, podman 5.4.2, systemd läuft.
Es war eine WSL2-Instanz, also ein echtes Debian-Userland (Punkt 56).

---

## 1. Bevor du anfängst: zwei Entscheidungen von Thomas

Diese zwei Dinge sind **keine Programmierarbeit**. Ohne sie geht nichts.

### Der geheime Release-Schlüssel

- Ein Skript nimmt eine Programmdatei nur, wenn die Unterschrift des Release stimmt
  (`shared/ulh.md`, Regel „Programmdateien").
- Es gibt ein Schlüsselpaar. Kennung `pN6Z6a9NR0aG6aEJIJ5EHg` (Punkt 57).
- Der **öffentliche** Teil steht in `ngc-all.sh`, `ngc-api.sh`, `ngc-controller.sh`
  und `ngc-web.sh`. `ngc-db.sh` braucht keinen — es holt keine Programmdateien.
- Der **geheime** Teil lag am 17.10.2026 als `/root/ngc-keys/release.key` mit `-rw-------`
  auf der Messmaschine. **Er gehört auf einen Hardware-Stick** (Entwurf 4.8) und von dieser
  Maschine weg.
- Solange er dort liegt: jeder, der auf dieser Maschine root ist, kann ein Release
  unterschreiben (Punkt 57).

### Der Store-Schlüssel

- **Einen offiziellen NGC-Store gibt es nicht.** Der Platzhalter
  `REPLACE-THIS-LINE-WITH-THE-NGC-STORE-KEY` ist noch drin (Punkt 57).
- **Also muss jede Installation ihren eigenen Store nennen**: `store_url` und `store_key`
  in `answer.yaml`, beide zusammen oder keins (`shared/ulh.md`).
- Seit Punkt 57 bricht das Skript ab, wenn beide leer sind: „NGC has no official app store
  yet …", Ausstiegscode 1, **bevor** etwas geschrieben wird.

---

## 2. Was du brauchst

- Debian 13, systemd läuft, root.
- [ulh](https://github.com/sorglos-it/ulh) — die Skripte liegen dort unter
  `apps/cli/assets/scripts/`.
- **Ein unterschriebenes Release.** Ohne Release geht nichts (siehe Stolperstein 4).
- Eine `answer.yaml` für `ngc-all` und `ngc-web`. Welche Schlüssel: `shared/ulh.md`.
- Einen Hostnamen. DNS-Name in Kleinbuchstaben oder IPv4 — **kein IPv6**
  (`shared/ulh.md`, Regel „Hosts und Adressen").

---

## 3. Die Reihenfolge, die funktioniert

Jeder Schritt: **was du tust → was du siehst, wenn es geht → was du siehst, wenn es nicht geht.**

### Schritt 1 — Release bauen und unterschreiben

`ngc-store release` über drei Teile, jeden mit `ngc-store check` prüfen.

- **Geht:** dreimal `release ok` — für `ngc-web all`, `ngc-api amd64`,
  `ngc-controller amd64` (Punkte 56, 57).
- **Geht nicht:** `release wrong: <Grund>`, Ausstiegscode 1. Bei falschem Aufruf Code 2
  (`shared/ulh.md`).

Die Form der Archive ist festgelegt und wird geprüft (Punkt 56):

| Archiv | Inhalt |
|---|---|
| `ngc-web-all.tar` | im Wurzelverzeichnis `public/index.php` und `templates/apache-vhost.conf`, 83 Einträge, keine Verknüpfungen |
| `ngc-api-amd64.tar` | genau zwei gewöhnliche Dateien: `ngc-api` und `ngc-store`, ohne Ordner |
| `ngc-controller-amd64.tar` | genau eine Datei |

Jede andere Form weist das Skript ab.

### Schritt 2 — `ngc-all install`

Richtet API, Schlüssel, Zertifikat und Dienst ein und startet die API.

- **Geht:** Dienst `ngc-api` **`active`** mit **`NRestarts=0`**, Port 7443 belegt,
  Zertifikat da. Auf dem Schirm: **Einmal-Code**, **Kennung der Anlage** und
  **TLS-Fingerabdruck**. Gegenprobe: `grep -c REPLACE-THIS-LINE /etc/ngc/api.json` = **0**
  (Punkt 57).
- **Geht nicht:** siehe Stolperstein 1. Der Dienst liegt in einer Neustartschleife, und
  das Skript meldet trotzdem Erfolg — das war der Fall vor Punkt 57.

**Den Einmal-Code aufschreiben.** Er liest nur root, er kommt in kein Log, und er ist
einmal brauchbar (`shared/ulh.md`). Auch die Kennung und den Fingerabdruck: die vergleichst
du in Schritt 5.

### Schritt 3 — `ngc-web install`

Apache, PHP, Oberfläche nach `/var/www/ngc/`, Zertifikat, `web.json`, Lastverteilung.

- **Geht:** Apache **`active`**, `curl -sk https://<host>/` gibt **`http=200`**, 451 Byte,
  und es ist NGCs eigene Seite (`<title>NGC</title>`).
  `GET /api/v1/setup` **durch Apache** gibt **`http=200`** — dieselbe Antwort wie direkt an
  Port 7443. Damit trägt die Strecke Browser → Apache → Balancer → API (Punkte 56, 57).
- **Geht nicht:** siehe Stolperstein 3 (Apache startet die Seite nicht).
- **Nebenbei, laut und richtig:** `⚠ NGC brings 5 protective settings for the files of the
  interface, and this version of ngc-web knows 4 of them.` Das Skript macht weiter. Welche
  die fünfte ist, ist **nicht gemessen** (Punkt 56, Befund 3).

### Schritt 4 — Die Einrichtungsseite im Browser

`https://<host>/` aufrufen.

- **Geht:** „**NGC einrichten** — Diese Anlage ist neu. Auf dem Bildschirm des Servers steht
  ein Einmal-Code. Bitte tippen Sie ihn hier ein." Dazu ein Feld und `Weiter` (Punkt 58).
- **Geht nicht, über `http://`:** „NGC braucht eine sichere Verbindung. Bitte die Seite über
  https:// aufrufen." Dann kommt nichts weiter (Punkt 58).
- **Geht nicht, Name löst nicht auf:** nach vier Sekunden **„Dienst nicht erreichbar. Bitte
  später erneut versuchen."** Dahinter steckt `fetch(...)` → `TypeError: Failed to fetch`
  (Punkt 58).
  **Achtung:** diesen Satz gibt es auch, wenn der Dienst weg ist **oder** die Datenbank
  gerade besetzt ist. Der Benutzer kann die drei nicht unterscheiden (Punkt 58, P9).
  Also erst `curl` von der Maschine aus probieren, bevor du NGC verdächtigst.

### Schritt 5 — Ersten Verwalter anlegen

Einmal-Code eintippen, `Weiter`.

- **Geht:** **Schritt 2** erscheint — „Bitte vergleichen: Steht hier dasselbe wie auf dem
  Bildschirm des Servers?" mit **Kennung der Anlage**, **Name und Adresse des API-Servers**
  (`wss://<host>:7443/tunnel`) und dem **Fingerabdruck**. Genau die Werte, die `ngc-all.sh`
  gedruckt hat. Felder: ein Name, zweimal Passwort, ein Haken für den App-Store (Punkt 59).
- Name und Passwort eintragen, `Einrichten`.
- **Geht:** der **Schreibtisch**. Startleiste mit `Start`, Uhr, die Kacheln **App-Store** und
  **Benutzer & Rechte**, Hinweis „Umstellen auf Passkeys empfohlen". Die Sitzung liegt in
  **einem** Keks: `ngc_session`, `httpOnly` und `secure` (Punkt 59).
- **Geht nicht, `/api/v1/setup` gibt 404:** der Einmal-Code ist verbraucht, die Einrichtung
  ist schon gelaufen. Wer dann einen Code eintippt, bekommt: „Diese Anlage ist noch nicht
  fertig eingerichtet. Bitte den Controller des Webservers koppeln." (Punkt 60)

**Ein Passschlüssel ist nicht nötig.** Der erste Verwalter arbeitet mit **Passwort**
(`proveWithPassword`, `KIND_PASSWORD`, `ALG_EDDSA`, frischer Salt — Punkt 58). Passkeys sind
die Kür.

### Schritt 6 — Kopplungscode holen (nur hier, nirgends sonst)

**Jetzt nicht den Browser schließen und nicht neu laden.** Bis der Controller gekoppelt ist,
kommt nur dieser eine Browser hinein (`shared/ulh.md`, „Reihenfolge nach der
Ersteinrichtung"; Punkt 60).

- In der angemeldeten Oberfläche: **Benutzer & Rechte** → Reiter **Server** → Knopf
  **„Kopplungscode zeigen"**.
  Belegt im Code: `apps/web/public/assets/js/apps/users.js:600` (`fillPairCode`),
  `showPairCode` ab 613, Reiter `["servers", "Server"]` in Zeile 105 — auf dem Zweig `web`
  (Punkt 61).
- **Geht:** der Code steht auf dem Schirm. Er fängt mit `ngc-pair:` an.
- **Geht nicht:** kein Code. Die Oberfläche zeigt ihn **nur**, wenn er zu ihrer neuesten
  geprüften Fassung der Vertrauensliste passt (`tunnel.checkCode`). Passt er nicht, kommt er
  gar nicht auf den Schirm — „ein Code auf dem Schirm ist ein Code, den jemand einfügt"
  (Kommentar in `users.js`).

### Schritt 7 — `ngc-all configure` mit dem Kopplungscode

Den Code als `pair_code` in die `answer.yaml`, dann `configure`.

- Der Code geht **nie** über die Kommandozeile. Das Skript reicht ihn mit
  `printf '%s\n' "$pair_code" | ngc-controller pair` durch, damit er nicht in
  `/proc/<pid>/cmdline` landet (`shared/ulh.md`).
- **Geht:** Kopplung `RC=0`, `/etc/ngc/trust-anchor.json` **entsteht**,
  `ngc-controller.service` läuft, und der Tunnel steht (Punkt 61).
- **Geht:** `ngc-controller` druckt seinen **Fingerabdruck** und den **Anker aus dem
  Kopplungscode** (Nummer und Fingerabdruck des Hashes). **Beides mit der Anzeige der
  Oberfläche vergleichen** (`shared/ulh.md`).
- **Geht nicht:** die eine feste Zeile „Ist der Kopplungscode noch gültig und die API
  erreichbar?" Sie nennt zwei Möglichkeiten und schließt keine aus — vor Punkt 61 war die
  Ursache eine dritte (siehe Stolperstein 5).
- **Geht nicht, nach gelungener Kopplung antwortet die Oberfläche 500:** siehe
  Stolperstein 2.

### Schritt 8 — Gegenprobe

- Ein **zweiter** Browser muss jetzt **„Bei NGC anmelden"** zeigen, nicht mehr die
  Einrichtungsseite (Punkt 61).
- **Geht nicht:** zeigt er weiter die Einrichtungsseite, fehlt `/etc/ngc/trust-anchor.json`.
  Die Entscheidung fällt serverseitig in `index.php`/`Config.php` aus dieser Datei
  (`index.php:27`, `$anchorFile = '/etc/ngc/trust-anchor.json'`; `shared/config/config.go:32`,
  `AnchorFile`). Ohne Anker hält die Oberfläche **jede** Anlage für neu — und auch eine
  gültige, gespeicherte Sitzung hilft dann nicht (Punkt 60).

---

## 4. Die fünf Stolpersteine, die wirklich aufgetreten sind

### 1. Der Platzhalter für den Store-Schlüssel

- **Erscheinung:** drei Dinge gleichzeitig.
  1. Die API lehnt ihre Konfiguration ab:
     `ERROR die Konfiguration lässt sich nicht lesen file=/etc/ngc/api.json error="config: invalid configuration"`
  2. Der Dienst liegt in einer **Neustartschleife** (`restart counter is at 8`).
  3. Das Skript meldet **`RC=0`** und **„The API is running"**.
- **Ursache:** in `api.json` stand
  `"storeKey": "REPLACE-THIS-LINE-WITH-THE-NGC-STORE-KEY"`.
  Der Riegel gab es nur für den **Release**-Schlüssel, nicht für den **Store**-Schlüssel.
- **Dazu:** die Fehlermeldung der API nennt **kein Feld**. Welches es war, kostete einen
  Blick in die Datei.
- **Seit Punkt 57 mit Riegel:** leere Store-Antworten brechen ab mit „NGC has no official
  app store yet …", **Ausstiegscode 1**, bevor etwas geschrieben wird.
- **Was du prüfst:** `grep -c REPLACE-THIS-LINE /etc/ngc/api.json` muss **0** sein, und der
  Dienst `NRestarts=0`. **„Eingerichtet ist nicht gestartet."**

### 2. Der Anker mit falschen Rechten

- **Erscheinung:** nach einer **gelungenen** Kopplung antwortet die Oberfläche auf jeden
  Aufruf **500**. Im Apache-Protokoll `Permission denied` für `www-data`.
- **Ursache:** `ngc-all.sh` und `ngc-controller.sh` schrieben
  `/etc/ngc/trust-anchor.json` als **`0600 root:root`**. `index.php` liest ihn aber und gibt
  Nummer und Hash jedem Browser in `config.json` mit — und Apache läuft als `www-data`.
- **Gemessen:** `0600` → 500. **`0644` → 200** und die Seite (Punkt 61).
- Was den Anker schützt, ist die **Unterschrift der Vertrauensliste**, nicht der Dateimodus.
- **Behoben in beiden Skripten.** Siehst du trotzdem 500: Modus der Datei prüfen.

### 3. Die Balancer-Zeile

- **Erscheinung:** Apache verweigert den Start der Seite.

  ```
  AH00526: Syntax error on line 19 of /etc/ngc/apache-apis.conf:
  ProxyPass unknown Balancer parameter
  ```
- **Ursache:** `upgrade=websocket` stand am **`ProxyPass`** eines `balancer://`-Ziels. Dort
  liest Apache jedes `schlüssel=wert` als **Balancer**-Parameter. Es ist aber ein
  **Worker**-Parameter und gehört an den **`BalancerMember`**.
- Verschoben sagt `apachectl configtest` **`Syntax OK`** (Punkt 56, Befund 2).
- Die Planzeile daneben („`upgrade=websocket` braucht Apache 2.4.47; Debian 13 bringt es
  mit") ist **richtig in der Fassung und falsch im Ort**.
- **Behoben** in `shared/ulh.md`, in `apps/server-controller/templates/apache-apis.conf.tmpl`
  (die Vorlage trägt den Parameter heute in Zeile 14 am `BalancerMember`, Zeile 19 ist das
  nackte `ProxyPass`) und in `ngc-web.sh`.
- **Was du prüfst:** `apachectl configtest` **vor** dem Neuladen. Das machen die Skripte
  auch — Apache wird erst nach `configtest` neu geladen (`shared/ulh.md`).

### 4. Das fehlende Release

- **Erscheinung:** „This script has no real release key of NGC yet." Das Skript hält.
- **Ursache:** **ohne Release geht nichts.** Ein Skript übernimmt eine Programmdatei nur
  gegen eine gültige Unterschrift. Das ist kein Programmfehler, sondern ein fehlendes
  Schlüsselpaar (Punkt 56, offener Punkt 1).
- Punkt 56 wurde darum mit einem **Testschlüssel** gemessen, der nirgends eingecheckt ist.
- **Seit Punkt 57 ist dieser Riegel auf:** der öffentliche Schlüssel steht in den vier
  Skripten, `grep -l REPLACE-THIS-LINE-WITH-THE-NGC-RELEASE-KEY *.sh` = **0**.
- **Dazu die Rückdreh-Sperre:** `seq` des Release gegen die Zahl in
  `/var/lib/ngc/release.seq`. Kleiner → Abbruch. Dieselbe Nummer darf zur Reparatur noch
  einmal installiert werden (`shared/ulh.md`).

### 5. Der Kopplungscode nur aus der angemeldeten Oberfläche

- **Erscheinung:** die Kopplung gelingt nie. `ngc-controller` sagt „Ist der Kopplungscode
  noch gültig und die API erreichbar?"
- **Ursache (Punkt 61):** `pairing.go` schickte die Anfrage an `tunnel.PathPair` **ohne
  jede Abfrage**. Die API verbraucht aber den Einmalwert, den die **Abfrage** benennt:
  `?secret=<base64url des SHA-256 des Kopplungsgeheimnisses>`. Fehlt sie, weist die API
  die Anfrage ab (Protokollfehler).
  **Behoben** — heute baut `pairing.go:168–169` genau diese Abfrage.
- **Und daraus wurde der Kreis, in den Punkt 60 gelaufen ist:**
  1. Keine Kopplung → kein `/etc/ngc/trust-anchor.json`.
  2. Ohne Anker zeigt die Oberfläche **jedem** Browser die Einrichtungsseite — auch mit
     gültiger Sitzung.
  3. Die Einrichtung ist aber gelaufen: `/api/v1/setup` gibt **404**, der Einmal-Code ist
     verbraucht.
  4. Koppeln kann man nur mit einem Code aus der **angemeldeten** Oberfläche.
  **Wer nach dem Einrichten den Browser schließt oder neu lädt, kommt nicht mehr hinein.**
  Die Installation aus Punkt 59 war danach nur über ein Zurücksetzen der Testdatenbank zu
  erreichen.
- **Also:** Schritt 6 **im selben Seitenaufbau** wie Schritt 5. Code holen, dann koppeln,
  dann darf der Browser zu.
- **Punkt 60 sagt „der Knopf dafür gibt es nicht". Das ist falsch** (Berichtigung in
  Punkt 61). Der Knopf heißt „Kopplungscode zeigen". Der Zähler „null Treffer in
  `apps/web`" kam aus einem Arbeitsbaum auf `main`, wo `apps/web` nur zwei Dateien hat —
  der Oberflächencode liegt auf dem Zweig `web`. In diesem Arbeitsbaum nachgezählt:
  `apps/web/` hat `CLAUDE.md` und `VERSION`. **Ein Zähler über den falschen Baum ist kein
  Befund.**

---

## 5. Zwei Umgebungsfallen, die als Programmfehler aussahen

Beide nur in WSL gemessen (Punkt 56).

- **`/tmp` war zu 100 % voll** (32-GB-tmpfs, zwei ulh-Prüfläufe mit 13 und 19 GB).
  apt zerlegt dort seine Signaturen und meldete dann
  `Splitting up … into data and signature failed` und
  `Write error (28: No space left on device)` — also **OpenPGP-Fehler als Erscheinung einer
  vollen Platte**. `TMPDIR` auf die ext4-Platte: alles läuft.
- **`TMPDIR` auf `/mnt/d` geht nicht:** dpkg verlangt Verknüpfungen,
  `Symlinking … failed - DPkg::Go (1: Operation not permitted)`. Es muss ein
  Linux-Dateisystem sein.
- **WSL schreibt `/etc/hosts` neu.** Mitten in der Messung gab `/api/` plötzlich **500 Proxy
  Error** und `curl` kam auf `http=000`. Ursache: WSL hatte den Namen aus `/etc/hosts`
  entfernt, während Apache und `web.json` weiter darauf zeigten. Nach dem Wiedereintragen:
  Seite 200, `/api/v1/setup` 200, `NRestarts=0` (Punkt 59). Ein Proxy-Fehler der
  Oberfläche, der wie ein Fehler von NGC aussieht.

---

## 6. Nicht gemessen

Das steht hier, damit niemand es für geprüft hält.

- **Dass Apps laufen.** Ob mit gekoppeltem Controller wirklich Container und Terminal
  starten, ist der nächste Messpunkt. Belegt ist bisher nur: der Tunnel steht (Punkt 61).
- **Ein echtes Zertifikat.** Gemessen wurde mit **selbst unterschriebenem** Zertifikat und
  einem Browser, der es für diesen Lauf annimmt (`ignoreHTTPSErrors`, `--host-resolver-rules`).
  Was ein Nutzer mit `letsencrypt` erlebt, ist **nicht** gemessen (Punkt 59).
- **Passkeys.** Der erste Verwalter läuft mit Passwort, und das ist vertragsgemäß. Die
  Oberfläche empfiehlt Passkeys selbst; der Weg dorthin ist nicht gemessen (Punkt 59).
- **Die fünfte Schutzeinstellung** von `ngc-web`. Das Skript sagt laut, dass es nur vier von
  fünf kennt. Welche fehlt, ist nicht geklärt (Punkt 56, Befund 3).
- **Dass podman die Härtungsschalter einhält.** Gemessen ist, dass sie **geschickt** werden —
  alle elf, Zeile für Zeile aus `apps/server-controller/classes/runtime/podman.go`. Nicht
  gemessen: ob podman sie einhält, ob `--pids-limit 512` eine echte Fork-Bombe stoppt, ob der
  `pids`-Controller rootless überhaupt delegiert ist (ohne Delegation **warnt** podman statt
  zu scheitern), ob diese podman-Fassung `nosuid`/`nodev` in der Optionsliste annimmt, und ob
  eine suid-Datei unter `/data` danach wirkungslos ist (Punkt 51).
- **Die 23 podman-Fälle überspringen sich auf Windows laut.**
  `TestCreateBuildsTheArgumentList` muss auf Linux nachgemessen werden (Punkt 51).
- **Mehrere Maschinen.** Diese Anleitung ist der kleine Fall: **eine** Maschine, SQLite
  (`config.DriverSQLite`), eine Datei je Programm — `/var/lib/ngc/api/ngc.db` und
  `/var/lib/ngc/controller/state.db`. MariaDB mit Galera ist ein anderer Fall und steht hier
  nicht (Punkt 47).
- **Ob die fünf Skripte auf ulh-`main` liegen.** Punkt 47 zählte: `origin/main` hat **null**
  `ngc-*`-Skripte, sie liegen auf eigenen Zweigen. Ob das heute noch so ist, habe ich in
  dieser Sitzung **nicht** nachgezählt — ich habe nur im NGC-Repo gelesen. Ein Push nach
  ulh-`main` erreicht per Auto-Update sofort alle Nutzer, deshalb entscheidet das Thomas
  (Punkt 47).
- **Diese Anleitung selbst ist nicht nachgefahren.** Sie ist aus Messprotokollen
  zusammengestellt, nicht an einer frischen Maschine Schritt für Schritt geprüft. Der erste,
  der sie befolgt, misst sie.

---

## 7. Noch zu entscheiden (Thomas)

Außer den zwei Punkten aus Abschnitt 1:

- **`noexec` auf `/data`.** Punkt 36 nennt es nicht, und es ist eine Verhaltensänderung für
  jede App, die unter `/data` Skripte hält (Punkt 51).
- **Die fünf ulh-Skripte nach `main` zusammenführen.** Ein Push dorthin erreicht sofort alle
  Nutzer (Punkt 47).
- **Wo gemessen wird:** entweder weiter mit selbst unterschriebenem Zertifikat, oder auf
  einer Maschine mit echtem Namen und `letsencrypt` — das ist der Fall, den ein Nutzer
  erlebt (Punkt 58).
