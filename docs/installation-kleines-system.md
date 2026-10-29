# Ein kleines NGC-System aufsetzen

Ein Rechner. Alles drauf: API, Controller, Weboberfläche, SQLite.
Für jemanden, der NGC noch nie gesehen hat.

**Woher das hier kommt:** jeder Schritt steht in den Vertragspunkten 47, 51, 56, 57, 58, 59,
61, 71 oder im Code. Was dort nicht steht, steht unten unter „Nicht gemessen".

**Gemessen auf:** Debian 13 (trixie), Apache 2.4.69, PHP 8.4, podman 5.4.2, systemd läuft.
Der erste Teil dieser Anleitung entstand auf einer WSL2-Instanz, also einem echten
Debian-Userland (Punkt 56). Die Abschnitte, die seit dem 19.10.2026 dazugekommen sind, sind
an der laufenden Anlage nachgesehen (Debian 13.7, `ngc-api`/`ngc-controller` **0.2.2**,
Oberfläche **0.2.0**, Stand 24.10.2026).

> **Die Maschine für den kleinen Fall ist seit dem 24.10.2026 eine neue.** Die alte Einzel-VM
> (`ssh ngc`, 10.2.3.208) wurde an jenem Tag um 12:37 vom Proxmox-Wirt aus angehalten und ist
> aus. **Die Messungen in dieser Anleitung bleiben gültig** — sie wurden gemacht, als jene
> Maschine lief, und ein ausgeschalteter Rechner macht eine richtige Messung nicht falsch.
> Nachgemessen wird jetzt auf **`ngc-ein`** (VMID 307, 10.2.3.216, Name wieder `ngc.test`),
> angelegt mit `tools/einzelmaschine.sh` aus der Vorlage und dem Release v0.2.2 — also
> **nachbaubar**, was die alte nie war. Der Weg bis „die Oberfläche antwortet" ist dort am
> 24.10.2026 noch einmal durchgelaufen und bestätigt: `https://ngc.test/` gibt `http=200` mit
> 451 Byte, genau wie am 16.10.2026. Alles ab der Ersteinrichtung im Browser ist dort **noch
> nicht** gegangen; was fehlt, steht in `docs/alte-vm-verweise.md`, Abschnitt 6.
>
> Die laufende Anlage mit sechs Maschinen ist **nicht** der kleine Fall und kann ihn nicht
> ersetzen (andere Datenbank, anderes Skript, mehrere Namen).

**Es gibt zurzeit zwei Anlagen, und diese Anleitung beschreibt die erste.**

| | **eine Maschine** — der Fall dieser Anleitung | **sechs Maschinen** — der andere Fall |
|---|---|---|
| Name der Anlage | `ngc.test` (Beispiel, siehe Schritt 0) | `ngc.mein.lan` |
| Maschine heute | `ngc-ein`, 10.2.3.216 (seit 24.10.2026) | api-1 … store |
| Maschinen | eine, alle vier Rollen | sechs, siehe Schritt 0 |
| Datenbank | SQLite, eine Datei je Programm | eigene Maschine |
| Skript | `ngc-all` | `ngc-api`, `ngc-db`, `ngc-controller`, `ngc-web` je Maschine |

Was beim verteilten Fall anders ist, steht an jeder Stelle, an der es anders ist: in Schritt 0
(mehr als ein Name im Browser), in Schritt 2 (vier Skripte statt einem) und unter
„Nicht gemessen". **Der durchgehende Weg unten ist der kleine Fall.** Eine eigene Anleitung
für sechs Maschinen gibt es nicht, und diese hier ist keine.

> **Was sich seit dem 19.10.2026 geändert hat, und warum diese Anleitung länger geworden ist.**
> Zwischen dem 19. und 21.10.2026 ist der Weg vom Release bis zur laufenden App zum ersten Mal
> von **einer** Person von Anfang bis Ende gegangen worden. Er ist **kein einziges Mal**
> durchgelaufen — an sechs kleinen Stellen hintereinander nicht (Punkt 71, „Sechs Wände auf
> einem Weg"). Die alte Fassung dieser Anleitung endete bei „der Tunnel steht". Hinter diesem
> Punkt lagen aber noch **drei** Stationen, die hier gefehlt haben und ohne die niemand je eine
> App zum Laufen bringt: der **Name**, unter dem die Anlage antwortet (Schritt 0), das
> **Aufnehmen** des gekoppelten Controllers samt **Systemrechten** (Schritt 9) und das Recht
> **„benutzen"** für eine eingerichtete App (Schritt 12).
> Alle drei sind auf der echten Maschine passiert, keine war vorher aufgeschrieben.

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
  `NGC_STORE_KEY_DEFAULT=REPLACE-THIS-LINE-WITH-THE-NGC-STORE-KEY` steht am 24.10.2026
  unverändert in `ngc-all.sh` (Zeile 177, Zweig `ngc-kette`).
- **Also muss jede Installation ihren eigenen Store nennen**: `store_url` und `store_key`
  in `answer.yaml`, beide zusammen oder keins (`shared/ulh.md`).
- Seit Punkt 57 bricht das Skript ab, wenn beide leer sind: „NGC has no official app store
  yet …", Ausstiegscode 1, **bevor** etwas geschrieben wird.
- **Das ist nicht kosmetisch, das ist eine Sperre.** Eine Installation aus einem
  veröffentlichten Release ist heute **nur** möglich, wenn du dir einen Store ausdenkst. Der
  Prüflauf `tools/erstlauf.sh` nennt dafür `https://store.erstlauf.invalid` und sagt es laut;
  dort antwortet dann nichts, und damit bleibt der Katalog leer (Schritt 11).

---

## 2. Was du brauchst

- Debian 13, systemd läuft, root.
- [ulh](https://github.com/sorglos-it/ulh) — die Skripte liegen dort unter
  `apps/cli/assets/scripts/`. **Auf `main` liegen sie nicht:** am 24.10.2026 hat
  `origin/main` **null** `ngc-*`-Skripte, alle fünf liegen auf dem Zweig `ngc-kette`
  (`ngc-all.sh`, `ngc-api.sh`, `ngc-controller.sh`, `ngc-db.sh`, `ngc-web.sh`).
- **Ein unterschriebenes Release.** Ohne Release geht nichts (siehe Stolperstein 4).
- Eine `answer.yaml` für `ngc-all` und `ngc-web`. `ngc-all` fragt fünf Dinge, in dieser
  Reihenfolge (`ngc-all.sh`, Zeilen 17–21):

  | # | Schlüssel | Was |
  |---|---|---|
  | 1 | `name` | Name dieser Maschine und ihrer API (`wire.ValidName`) |
  | 2 | `hostname` | Name oder IPv4-Adresse, unter der der Browser sie erreicht |
  | 3 | `store_url` | der App-Store — **nicht leer lassen**, siehe Abschnitt 1 |
  | 4 | `store_key` | sein öffentlicher Schlüssel |
  | 5 | `pair_code` | nur bei `configure`: der Kopplungscode aus dem Browser (Schritt 8) |

- Einen Hostnamen. DNS-Name in Kleinbuchstaben oder IPv4 — **kein IPv6**
  (`shared/ulh.md`, Regel „Hosts und Adressen").

---

## 3. Die Reihenfolge, die funktioniert

Jeder Schritt: **was du tust → was du siehst, wenn es geht → was du siehst, wenn es nicht geht.**

### Schritt 0 — Den Namen auf **deinem eigenen** Rechner eintragen

**Das ist der Schritt, der in dieser Anleitung gefehlt hat, und er kommt vor allen anderen.**

Die Anlage antwortet unter **ihrem Namen**, nicht unter ihrer IP-Adresse. Was du in Schritt 2
als `hostname` angibst, steht danach überall: in `/etc/ngc/web.json` als `apis[].url` und als
`rp.id`, in `/etc/ngc/apache-apis.conf` am `BalancerMember`, im TLS-Zertifikat als `CN` und
`subjectAltName`.

**`ngc.test` ist in dieser Anleitung ein Beispielname, kein Name, den du übernehmen musst.**
Du setzt dort **deinen eigenen** ein. `.test` ist für genau das reserviert (RFC 2606): dieser
Name kann nie einer echten Domain in die Quere kommen, und dieselbe Schreibweise steht in allen
Prüfdaten des Projekts. Darum bleibt er hier stehen, statt durch einen echten Namen ersetzt zu
werden — ein echter Name wäre Thomas' Heimnetz und hilft keinem Leser. Wo die Anleitung von
einer **Messung** spricht, ist `ngc.test` dagegen kein Beispiel, sondern der Name, unter dem
damals wirklich gemessen wurde.

Auf der Einzel-VM hieß der Name `ngc.test`, und `/config.json` gab
ihn genau so an den Browser weiter (gemessen, solange die Maschine lief):

```
{"version":"0.2.0","anchor":{…},"rp":{"id":"ngc.test","origins":["https://ngc.test"]},
 "apis":["https://ngc.test:7443"]}
```

**Was du auf deinem Rechner einträgst** — in der Datei `hosts`, eine Zeile, IP-Adresse der
Anlage und der Name aus Schritt 2:

```
<IP-Adresse deiner Anlage>   <dein Name>
```

Nicht abtippen, sondern einsetzen. So sieht die Zeile für die heutige Maschine des kleinen
Falls aus (`ngc-ein`, seit 24.10.2026):

```
10.2.3.216   ngc.test
```

Auf der alten Einzel-VM hieß dieselbe Zeile `10.2.3.208   ngc.test`. **Diese Adresse
antwortet seit dem 24.10.2026 nicht mehr**; in älteren Messungen steht sie noch.

- Windows: `C:\Windows\System32\drivers\etc\hosts`, Editor als Administrator.
- Linux/macOS: `/etc/hosts`, als root.
- Hast du einen echten DNS-Server, gehört der Name dorthin; dann entfällt dieser Schritt —
  **aber lies erst die Falle weiter unten.**

**Warum es ohne nicht geht, und zwar an zwei verschiedenen Stellen:**

1. **Der Browser warnt, und er hat recht.** Das Zertifikat lautet auf den **Namen** und auf
   nichts sonst. Nachgesehen am 24.10.2026 an der verteilten Anlage:

   ```
   subject=CN=ngc.mein.lan
   X509v3 Subject Alternative Name:
       DNS:ngc.mein.lan
   ```

   **Keine einzige IP-Adresse steht darin.** Rufst du die Anlage über ihre Adresse auf, kann
   das Zertifikat nicht passen — nicht weil etwas falsch eingestellt ist, sondern weil dort
   nichts steht, was zu einer Adresse passen könnte. Gemessen wurde bisher mit einem **selbst
   unterschriebenen** Zertifikat und einem Browser, der es für den Lauf annimmt
   (`ignoreHTTPSErrors`); ein echter Browser nimmt es nicht ohne Weiteres an (Punkt 59).
2. **Passschlüssel gehen über eine IP-Adresse gar nicht.** Ein Passschlüssel hängt an der
   `rp.id`, und die ist ein **Name** — `ngc.test` bei der einen Maschine, `ngc.mein.lan` bei
   der verteilten Anlage. Eine IP-Adresse kann keine `rp.id` sein, und `origins` nennt genau
   die eine Adresse mit dem Namen. Über die IP lässt sich also kein Passschlüssel anlegen und
   keiner benutzen, egal wie man die Zertifikatswarnung wegklickt. Die Oberfläche empfiehlt
   Passkeys selbst (Schritt 6) — über die IP ist diese Empfehlung nicht befolgbar.

#### Beim verteilten Fall sind es mehr Namen, nicht einer

Diese sieben Zeilen hat Thomas am 24.10.2026 für die Anlage aus sechs Maschinen genannt:

```
10.2.3.210 api-1.ngc.mein.lan api-1
10.2.3.211 api-2.ngc.mein.lan api-2
10.2.3.212 db-1.ngc.mein.lan db-1
10.2.3.213 svc-1.ngc.mein.lan svc-1
10.2.3.214 svc-2.ngc.mein.lan svc-2
10.2.3.215 store.ngc.mein.lan store
10.2.3.210 ngc.mein.lan
```

**Der Name der Anlage ist `ngc.mein.lan`, und er zeigt auf dieselbe Adresse wie `api-1`.**

**Und du brauchst nicht nur ihn.** Nachgesehen an der laufenden verteilten Anlage, mit dem
Namen erzwungen (`curl --resolve ngc.mein.lan:443:10.2.3.210`):

```json
{"version":"0.2.1","anchor":{…},"rp":{"id":"ngc.mein.lan","origins":["https://ngc.mein.lan"]},
 "apis":["https://api-1.ngc.mein.lan:7443","https://api-2.ngc.mein.lan:7443"]}
```

`apis` nennt **Namen**, und der Browser verbindet sich zu ihnen **selbst** — nicht über die
Oberfläche. Jeder davon trägt sein **eigenes** Zertifikat: auf `10.2.3.211:7443` steht
`CN=api-2.ngc.mein.lan`, `SAN: DNS:api-2.ngc.mein.lan`. Fehlt `api-2.ngc.mein.lan` in deiner
`hosts`-Datei, kommt die Oberfläche hoch und die zweite API ist für deinen Browser nicht da.
**Also alle sieben Zeilen eintragen, nicht die letzte allein.**

**Die Namen sind keine Rollenverteilung.** Es gab einen Stand, auf dem `db-1` einen Controller
trug und `store` gar kein NGC. Welche Maschine was tut, steht in ihrer `controller.json` und in
der Vertrauensliste — nicht in ihrem Namen. Gemessen am 24.10.2026: alle sechs antworten auf
`ping` und auf Port 22; eine API auf Port 7443 haben **nur** `10.2.3.210` und `10.2.3.211`. Die
anderen vier haben dort nichts, und das ist richtig so — ein Controller horcht nicht, er wählt
durch den Tunnel hinaus.

#### Die Falle, wenn dein Netz jeden Namen beantwortet

**Gemessen hier am 24.10.2026, und sie ist bösartig:** in diesem Netz lösen **alle sieben
Namen** auf `10.2.3.102` auf — eine Maschine, auf der **kein NGC** läuft (`https://…/` gibt
`302`, `/config.json` gibt `401`). Der DNS-Server beantwortet also jeden Namen unter
`ngc.mein.lan`, auch die, die er nicht kennt.

Das heißt: **ohne die Zeilen in deiner `hosts`-Datei landest du auf einer fremden Maschine**,
und zwar ohne Fehlermeldung, die das sagt. Was du siehst, ist eine Anmeldung, die nicht NGCs
Anmeldung ist, oder eine Zertifikatswarnung, die wie ein Fehler der Anlage aussieht. Darum
reicht „wir haben DNS" hier **nicht** — ein DNS, der nie „kenne ich nicht" sagt, ist schlimmer
als keiner.

**Gegenprobe, bevor du weitergehst** — nicht `ping`, denn `ping` beantwortet auch die falsche
Maschine. Frage die Anlage, wie sie selbst heißt:

```bash
curl -sk https://<name>/config.json
```

Darin muss `rp.id` **genau der Name** stehen, den du aufgerufen hast. Steht dort ein anderer,
oder kommt `401`, `302` oder HTML statt JSON, redest du mit der falschen Maschine. Erst wenn
das stimmt, lohnt Schritt 5.

### Schritt 1 — Release bauen und unterschreiben

`ngc-store release` über drei Teile, jeden mit `ngc-store check` prüfen.

- **Geht:** dreimal `release ok` — für `ngc-web all`, `ngc-api amd64`,
  `ngc-controller amd64` (Punkte 56, 57).
- **Geht nicht:** `release wrong: <Grund>`, Ausstiegscode 1. Bei falschem Aufruf Code 2
  (`shared/ulh.md`).

**Die Programme müssen mit der Versionsnummer im Bauch gebaut sein.** Das ist Wand 2 von
sechs: im Oktober wurde ohne `-ldflags "-X main.Version=…"` gebaut, jedes Programm meldete den
Rückfallwert `0.0.0-dev` aus `apps/server-api/main.go:43` — und die Oberfläche hält jede
Rechnerzeile gegen `validVersion` (`contracts/text.js:84`, genau MAJOR.MINOR.PATCH) und
verwarf deshalb die **ganze** Antwort. Auf dem Schirm stand „Die Rechner konnten nicht geladen
werden. Bitte das Fenster neu öffnen." — und das Fenster neu öffnen half nicht, weil die
Version dieselbe blieb.

Dafür gibt es seit dem 23.10.2026 ein Skript, und du nimmst es, statt den Befehl abzutippen:

```bash
bash tools/build-servers.sh <ausgabeverzeichnis> amd64
```

Es stempelt die Nummer aus den `VERSION`-Dateien ein. **Jedes Programm trägt seine eigene
Nummer**, und das ist kein Fehler: `apps/server-api/VERSION` ist `0.2.0`,
`apps/server-controller/VERSION` ist `1.0.0`, `apps/web/VERSION` ist `0.2.1` (24.10.2026).
Welche davon die Nummer **des Release** ist, steht nirgends — siehe „Nicht gemessen".

Die Form der Archive ist festgelegt und wird geprüft (Punkt 56):

| Archiv | Inhalt |
|---|---|
| `ngc-web-all.tar` | im Wurzelverzeichnis `public/index.php` und `templates/apache-vhost.conf`, 83 Einträge, keine Verknüpfungen |
| `ngc-api-amd64.tar` | genau zwei gewöhnliche Dateien: `ngc-api` und `ngc-store`, ohne Ordner |
| `ngc-controller-amd64.tar` | genau eine Datei |

Jede andere Form weist das Skript ab.

### Schritt 2 — `ngc-all install`

Richtet API, Schlüssel, Zertifikat und Dienst ein und startet die API.

> **Beim verteilten Fall ist das nicht ein Skript, sondern vier.** `ngc-all` ist genau für
> „alles auf einer Maschine" da und nimmt deshalb SQLite. Sechs Maschinen brauchen `ngc-api`,
> `ngc-db`, `ngc-controller` und `ngc-web` einzeln, jedes auf seiner Maschine und mit eigener
> `answer.yaml`; `ngc-api.sh` weist jeden Treiber außer MariaDB ab und sagt in seiner eigenen
> Meldung, dass eine Maschine auf SQLite die Sache von `ngc-all` ist. **Die Reihenfolge dieser
> vier untereinander steht in keiner Anleitung** und ist hier nicht gemessen — siehe
> „Nicht gemessen".

- **Geht:** Dienst `ngc-api` **`active`** mit **`NRestarts=0`**, Port 7443 belegt,
  Zertifikat da. Auf dem Schirm: **Einmal-Code**, **Kennung der Anlage** und
  **TLS-Fingerabdruck**. Gegenprobe: `grep -c REPLACE-THIS-LINE /etc/ngc/api.json` = **0**
  (Punkt 57).
- **Geht nicht:** siehe Stolperstein 1. Der Dienst liegt in einer Neustartschleife, und
  das Skript meldet trotzdem Erfolg — das war der Fall vor Punkt 57.

**Den Einmal-Code aufschreiben.** Er liest nur root, er kommt in kein Log, und er ist
einmal brauchbar (`shared/ulh.md`). Auch die Kennung und den Fingerabdruck: die vergleichst
du in Schritt 6.

**Was die Versionsnummer angeht, fragst du die installierten Programme selbst:**

```bash
/usr/lib/ngc/ngc-api -version
/usr/lib/ngc/ngc-controller -version
```

Auf der laufenden Anlage antworten sie `ngc-api 0.2.2 (contract 1)` und
`ngc-controller 0.2.2 (contract 1)`. Steht dort `0.0.0-dev`, ist Schritt 1 ohne das Flag
gelaufen — dann **jetzt** zurück und neu bauen, nicht erst in Schritt 10, wo es als „Die
Rechner konnten nicht geladen werden" wieder auftaucht.

### Schritt 3 — Dem Dienst `/etc/ngc` zum Schreiben erlauben

> **Berichtigung vom 29.10.2026: Wand 1 ist weg, und du musst hier nichts tun.** Auf dem Zweig
> `ngc-kette` lautet die Zeile `ReadWritePaths=${NGC_DATA} ${NGC_ETC}` — also `/var/lib/ngc` und
> **`/etc/ngc`** (`ngc-all.sh:1431`, dieselbe Zeile in `ngc-api.sh:1073`). Auf einer frisch aus
> der Vorlage geklonten Maschine nachgemessen, nicht im Text nachgelesen: `systemctl show
> ngc-api.service -p ReadWritePaths` nennt `/var/lib/ngc /etc/ngc`, und ein Schreibversuch in
> der **echten** Sandbox der Unit kommt durch (Punkt 96). **Der Drop-in unten ist überholt** —
> wer ihn anlegt, legt eine Zeile an, die schon im Skript steht. Der Rest dieses Schritts bleibt
> stehen, weil die Gegenprobe mit `systemd-run` richtig und nützlich bleibt und weil der Grund
> erklärt, warum diese eine Zeile so viel entscheidet.

**Das war Wand 1 von sechs** (Stand bis 24.10.2026, hier als Begründung aufbewahrt).

`ngc-api.service` wird mit `ProtectSystem=strict` geschrieben, und die schreibbaren Pfade waren
`ReadWritePaths=${NGC_DATA} ${NGC_TLS}` — also `/var/lib/ngc` und `/etc/ngc/tls`. **`/etc/ngc`
selbst stand nicht dabei.** Der Vertrauensanker liegt aber dort, und er wird **atomar**
geschrieben: `config.SaveAnchor` (`shared/config/config.go:451`) legt erst eine Datei
`.trust-anchor.json.tmp-*` **im selben Ordner** an. Ein Ordner, in dem keine Datei entstehen
darf, lässt kein atomares Ersetzen zu — auch dann nicht, wenn die Zieldatei selbst `0644`
trägt.

Das ist die einzige der sechs Wände, die den Grund **selbst nennt** (`state/trust.go:249`):

> the trust anchor outside the database could not be moved up; this API still pins the older
> version — `pinned=1 stored=2`

Die Folge steht in derselben Zeile: **die Anlage bleibt auf der alten Vertrauensliste
festgenagelt.** Das merkst du nicht in Schritt 2 — dort läuft alles mit Code 0 durch, die API
läuft, der Einmal-Code steht auf dem Schirm. Du merkst es erst, wenn später nichts mehr
nachkommt.

**Was du tust** — einen Drop-in anlegen, statt die Unit zu ändern (die Unit schreibt das
Skript bei jedem `configure` neu, ein Drop-in bleibt):

```bash
mkdir -p /etc/systemd/system/ngc-api.service.d
printf '[Service]\nReadWritePaths=/var/lib/ngc /etc/ngc\n' \
  > /etc/systemd/system/ngc-api.service.d/anker.conf
systemctl daemon-reload
systemctl restart ngc-api
```

- **Geht:** `systemctl show ngc-api.service -p ReadWritePaths` nennt `/etc/ngc`.
  Auf der laufenden Anlage steht dort `/var/lib/ngc /etc/ngc /etc/ngc/tls`.
- **Gegenprobe, die nicht lügt** — in der **echten** Sandbox der Unit schreiben, nicht in der
  Datei nachlesen:

  ```bash
  systemd-run --pipe -p ProtectSystem=strict \
    -p 'ReadWritePaths=/var/lib/ngc /etc/ngc' \
    touch /etc/ngc/.probe && rm -f /etc/ngc/.probe
  ```

  Ohne den Drop-in antwortet dasselbe mit
  `touch: cannot touch '…': Read-only file system`. Diese zwei Läufe gegeneinander sind der
  Beweis; `systemctl show` allein rechnet Drop-ins ein, die niemand angesehen hat.

### Schritt 4 — `ngc-web install`

Apache, PHP, Oberfläche nach `/var/www/ngc/`, Zertifikat, `web.json`, Lastverteilung.

- **Geht:** Apache **`active`**, `curl -sk https://<host>/` gibt **`http=200`**, 451 Byte,
  und es ist NGCs eigene Seite (`<title>NGC</title>`).
  `GET /api/v1/setup` **durch Apache** gibt **`http=200`** — dieselbe Antwort wie direkt an
  Port 7443. Damit trägt die Strecke Browser → Apache → Balancer → API (Punkte 56, 57).
- **Geht nicht:** siehe Stolperstein 3 (Apache startet die Seite nicht).
- **Nebenbei, laut und richtig:** `⚠ NGC brings 5 protective settings for the files of the
  interface, and this version of ngc-web knows 4 of them.` Das Skript macht weiter. Welche
  die fünfte ist, ist **nicht gemessen** (Punkt 56, Befund 3).

### Schritt 5 — Die Einrichtungsseite im Browser

`https://<host>/` aufrufen — mit dem **Namen**, nicht der IP (Schritt 0).

- **Geht:** „**NGC einrichten** — Diese Anlage ist neu. Auf dem Bildschirm des Servers steht
  ein Einmal-Code. Bitte tippen Sie ihn hier ein." Dazu ein Feld und `Weiter` (Punkt 58).
- **Geht nicht, über `http://`:** „NGC braucht eine sichere Verbindung. Bitte die Seite über
  https:// aufrufen." Dann kommt nichts weiter (Punkt 58).
- **Geht nicht, Name löst nicht auf:** nach vier Sekunden **„Dienst nicht erreichbar. Bitte
  später erneut versuchen."** Dahinter steckt `fetch(...)` → `TypeError: Failed to fetch`
  (Punkt 58).
  **Achtung:** diesen Satz gibt es auch, wenn der Dienst weg ist **oder** die Datenbank
  gerade besetzt ist. Der Benutzer kann die drei nicht unterscheiden (Punkt 58, P9).
  Also erst `curl` von der Maschine aus probieren, bevor du NGC verdächtigst — und auf
  **deinem** Rechner den Eintrag aus Schritt 0 prüfen.

### Schritt 6 — Ersten Verwalter anlegen

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
die Kür — und sie brauchen den Namen aus Schritt 0.

### Schritt 7 — Kopplungscode holen (nur hier, nirgends sonst)

**Jetzt nicht den Browser schließen und nicht neu laden.** Bis der Controller gekoppelt ist,
kommt nur dieser eine Browser hinein (`shared/ulh.md`, „Reihenfolge nach der
Ersteinrichtung"; Punkt 60).

- In der angemeldeten Oberfläche: **Benutzer & Rechte** → Reiter **Server** → Abschnitt
  **Kopplungscode** → Knopf **„Kopplungscode zeigen"**.
  Belegt im Code: `apps/web/public/assets/js/apps/users.js:1140` (`fillPairCode`), Reiter
  `["servers", "Server"]` in Zeile 107. Die vier Reiter heißen **Benutzer**,
  **Rollen und Rechte**, **Server**, **Protokoll** (Zeilen 104–109).
- Der Reiter **Server** hat sechs Abschnitte, in dieser Reihenfolge: **Wartende Server**,
  **Wartende API-Server**, **Server dieser Anlage**, **Eingetragene Server**,
  **Kopplungscode**, **Beitrittscode** (`users.js:734–744`). Du brauchst in Schritt 7 den
  fünften und in Schritt 9 den ersten.
- **Geht:** der Code steht auf dem Schirm, dazu „Gültig bis …". Er fängt mit `ngc-pair:` an.
- **Geht nicht:** kein Code, sondern „Dieser Code passt nicht zu dieser Anlage." Die
  Oberfläche zeigt ihn **nur**, wenn er zu ihrer neuesten geprüften Fassung der
  Vertrauensliste passt (`tunnel.checkCode`) — „ein Code auf dem Schirm ist ein Code, den
  jemand einfügt" (Kommentar in `users.js`).

### Schritt 8 — `ngc-all configure` mit dem Kopplungscode

Den Code als `pair_code` in die `answer.yaml`, dann `configure`.

- Der Code geht **nie** über die Kommandozeile. Das Skript reicht ihn mit
  `printf '%s\n' "$pair_code" | ngc-controller pair` durch, damit er nicht in
  `/proc/<pid>/cmdline` landet (`shared/ulh.md`).
- **Geht:** Kopplung `RC=0`, `/etc/ngc/trust-anchor.json` **entsteht**,
  `/etc/ngc/controller.json` entsteht, `ngc-controller.service` läuft, und der Tunnel steht
  (Punkt 61).
- **Geht:** `ngc-controller` druckt seinen **Fingerabdruck** und den **Anker aus dem
  Kopplungscode** (Nummer und Fingerabdruck des Hashes). **Beides mit der Anzeige der
  Oberfläche vergleichen** (`shared/ulh.md`).
- **Geht nicht:** die eine feste Zeile „Ist der Kopplungscode noch gültig und die API
  erreichbar?" Sie nennt zwei Möglichkeiten und schließt keine aus — vor Punkt 61 war die
  Ursache eine dritte (siehe Stolperstein 5).
- **Geht nicht, nach gelungener Kopplung antwortet die Oberfläche 500:** siehe
  Stolperstein 2.

**Auf einer Maschine bekommt der Controller alle vier Rollen** — `ROLES=(web api db services)`
(`ngc-all.sh:150`). Die Rolle `services` ist die, die später Apps ausführt; ohne sie bietet der
Store den Rechner nicht an (Schritt 10). Du musst dafür nichts tun: `controller.json` der
laufenden Anlage trägt `"roles": ["web","api","db","services"]`.

### Schritt 9 — Den Controller **aufnehmen** und dabei die **Systemrechte** setzen

**Hier endete die alte Fassung dieser Anleitung, und hier strandet jeder, der sie befolgt.**
Gekoppelt ist nicht aufgenommen. Der Tunnel steht, der Controller meldet sich — aber er steht
noch **nicht in der Vertrauensliste**, und solange er dort nicht steht, darf er nichts.

- In der angemeldeten Oberfläche: **Benutzer & Rechte** → Reiter **Server** → Abschnitt
  **Wartende Server**. Dort steht er mit seinem **Fingerabdruck**, darüber der Satz
  „Vergleichen Sie diesen Fingerabdruck mit dem, den das Skript auf dem Server anzeigt."
- **Den Fingerabdruck aus Schritt 8 vergleichen.** Erst dann aufnehmen.
- Es öffnet sich **„Server »…« aufnehmen"**. Darin steht, unter welchem Namen er in der Liste
  heißt, der Fingerabdruck, und — das ist der Teil, der gefehlt hat — ein Kasten
  **„Aufgaben mit Systemrechten"** mit dem Hinweis:

  > Wer hier einen Haken hat, darf auf diesem Server Apps einrichten, entfernen, Daten
  > löschen und das Terminal öffnen.

  Darunter **„Einzelne Personen"** und darunter die Rollen.
- **Dein eigener Haken ist schon gesetzt.** Darunter steht: „Ihr Haken ist gesetzt. Nehmen Sie
  ihn weg, wenn Sie auf diesem Server selbst nichts einrichten sollen — dann kann es dort
  zunächst niemand. Das lässt sich später unter »Eingetragene Server« ändern."
- `Server aufnehmen`.
- **Geht:** „Der Server »…« ist aufgenommen. Aufgaben mit Systemrechten dürfen dort: …" mit
  den Namen, die du angekreuzt hast.
- **Geht auch, und das ist die Sackgasse:** nimmst du **alle** Haken weg, steht dort „Auf
  diesem Server darf noch niemand Apps einrichten." Das ist kein Fehler, das ist die Wahrheit
  — und der Weg heraus führt über **Eingetragene Server**, wo sich beide Listen jederzeit
  ändern lassen.

**Warum das eine eigene Station ist, und warum sie so neu ist.** Bis zum 23.10.2026 trug die
Oberfläche einen frisch aufgenommenen Controller mit `rootUsers: []` und `rootRoles: []` ein,
und **keine Seite konnte diese beiden Listen danach ändern**. `trust.MayRoot`
(`shared/trust/trust.go:419`) fragt genau sie: der Benutzer steht in `RootUsers`, oder eine
seiner Rollen in `RootRoles` — sonst `false`. Geprüft wird das an **jeder** Root-Aufgabe
(`shared/task/task.go:199`, `shared/core/uiproof.go:132`), und „App einrichten" ist eine.
Zwei leere Listen, die niemand füllen kann, hießen: **auf diesem Controller kann nie eine App
eingerichtet werden.** Das ist Wand 4 von sechs.

Gebaut ist die fehlende Oberfläche in PR #87 (`rootPicker`, `users.js:310`), und PR #105 hat
entschieden, dass der Haken **der unterschreibenden Person vorgekreuzt** ist — abwählbar, nur
`rootUsers`, nur dieser Controller. **Nicht** über eine Rolle: eine Rolle gibt das Recht an
alle weiter, die sie heute oder morgen tragen, und das ist eine andere Entscheidung als „ich
für diesen Rechner" (`users.js:290–309` trägt die Begründung).

**Für einen API-Server gibt es diesen Kasten nicht**, und das ist richtig: `MayRoot` gibt für
alles, was kein Controller ist, ohnehin `false` (`askJoinAPI`, `users.js:1285`).

### Schritt 10 — Gegenprobe

- Ein **zweiter** Browser muss jetzt **„Bei NGC anmelden"** zeigen, nicht mehr die
  Einrichtungsseite (Punkt 61).
- **Geht nicht:** zeigt er weiter die Einrichtungsseite, fehlt `/etc/ngc/trust-anchor.json`.
  Die Entscheidung fällt serverseitig in `index.php`/`Config.php` aus dieser Datei
  (`index.php:27`, `$anchorFile = '/etc/ngc/trust-anchor.json'`; `shared/config/config.go:32`,
  `AnchorFile`). Ohne Anker hält die Oberfläche **jede** Anlage für neu — und auch eine
  gültige, gespeicherte Sitzung hilft dann nicht (Punkt 60).
- **Beide Seiten messen, nicht eine.** API und Oberfläche können verschiedener Meinung sein:
  `curl -sk -o /dev/null -w '%{http_code}\n' https://<host>/api/v1/setup` muss nach der
  Einrichtung **404** sagen, und die Seite im Browser muss **die Anmeldung** zeigen. Auf der
  laufenden Anlage: Seite `200`, `setup` `404`. Sagt die API 404 und der Browser zeigt die
  Einrichtung, fehlt der Anker; zeigt der Browser die Anmeldung und die API sagt 200, ist die
  Kette unlesbar.
- **Im Store muss der Rechner stehen:** App-Store → der Rechner aus Schritt 9 steht in der
  Auswahl **Rechner**. Steht dort „Die Rechner konnten nicht geladen werden. Bitte das Fenster
  neu öffnen.", ist es Wand 2 — zurück zu Schritt 1. Steht dort „Eine Angabe dieses Servers
  ist unlesbar (`version`)." — dasselbe, nur sagt die Oberfläche seit PR #104 **welches Feld**.
  Steht dort „Kein Rechner für Dienste da. Bitte zuerst einen Rechner hinzufügen.", fehlt die
  Rolle `services` oder der Controller ist nicht online (`store.js:614`).

### Schritt 11 — Auf den Katalog warten (bis zu eine Stunde)

> **Berichtigung vom 29.10.2026, zwei Sätze, und der zweite ist der nützliche.**
>
> 1. **Es gibt eine Handauslösung: `systemctl restart ngc-api`.** `FirstCatalog = time.Minute`
>    lässt die **erste** Runde eine Minute nach dem Start laufen und erst danach stündlich. Am
>    29.10.2026 gemessen: Start 19:49:22, erste Katalogrunde 19:50:22. Wer nicht warten will,
>    startet neu und hat die Runde nach einer Minute.
> 2. **Vor der Vertrauensliste wird ohnehin nichts geholt.** Dieselbe Runde endete mit
>    `WARN catalogs could not be fetched error="state: no trust list yet"`. Die Stunde ist also
>    nicht der erste Hinderer — **Schritt 8 und 9 sind es.** Wer hier auf einen Katalog wartet,
>    ohne den Controller gekoppelt und aufgenommen zu haben, wartet auf etwas, das gar nicht
>    kommen kann (Punkt 96).

**Eine frische Anlage hat bis zu eine Stunde lang keinen Laden.** `server.go:386` legt einen
**Stundenticker** an, `Server.hour` holt die Kataloge. Bis der erste Tick kommt, steht im Store:

> Der Katalog dieses Stores ist nicht echt.

**Das ist nicht wahr, und zwar nur in der Formulierung.** Gemeint ist „noch nicht geholt". Wer
den Satz liest, sucht einen Schlüsselfehler, und das ist die falsche Stelle. Das ist Wand 3 von
sechs (Punkte 65, 66; behoben in PR #86).

- **Was du tust:** erst prüfen, dass Schritt 8 und 9 wirklich durch sind — sonst wartest du
  umsonst. Dann entweder warten (bis zu 60 Minuten) oder `systemctl restart ngc-api` und eine
  Minute warten.
- **Hast du in Abschnitt 1 einen erfundenen Store eingetragen** (weil es keinen offiziellen
  gibt), antwortet dort auch nach einer Stunde nichts. Dann führt der Weg über **Hochladen**:
  App-Store → **Hochladen** („Ein Paket von der Festplatte einrichten", `store.js:503`).

### Schritt 12 — App einrichten und das Recht **„benutzen"** vergeben

**Eine eingerichtete App bleibt unsichtbar, solange niemand das Recht „benutzen" hat.** Das
ist die dritte Station, die hier gefehlt hat.

**Einrichten:** App-Store → die App → alle Rechte erlauben, die ihr Manifest verlangt → Auswahl
**Rechner** → `Aktivieren`.

- **Geht:** „Die App ist eingerichtet." Und sie steht **sofort** im Startmenü — ohne
  Neuladen; die Oberfläche sagt sich das selbst (siehe Schritt 13).
- **Geht nicht, es fehlen Haken:** „Bitte alle Rechte erlauben. Ohne sie kann die App nicht
  arbeiten." Eine halb erlaubte App gibt es nicht (`store.js:630`).
- **Geht nicht, du darfst dort nichts:** dann hat Schritt 9 keinen Haken für dich gesetzt.
  Zurück zu **Eingetragene Server**.

**Das Recht vergeben:** Benutzer & Rechte → Reiter **Rollen und Rechte**. Dort steht je Rolle
ein Kasten, darüber „Ein Haken heißt: alle mit dieser Rolle dürfen das." Für jede App gibt es
die Zeile `<Name der App> · benutzen` (`users.js:1592`) und dazu jede Aktion, die ihr
Manifest nennt. Haken setzen, speichern.

- **Die Rolle `admin` hat keine Haken, und das ist Absicht.** Dort steht: „Diese Rolle darf
  jede eingerichtete App benutzen. Das ist fest und lässt sich nicht abwählen." Die Rechte
  stehen benannt da, ohne Kasten und ohne Speicherknopf.
- **Ist noch keine App da**, steht „Es ist noch keine App eingerichtet."

**Warum es diese Station braucht, und warum sie fast nicht zu lösen war.** Der Schreibtisch
zeigt eine App nur dem, der das Recht `<app>/use` hält: `apps()` überspringt jede Zeile ohne
`rq.Rights[a.ID+"/"+core.RightUse]` (`apps/server-api/classes/coreapp/desktop.go:162`). Und die
Seite „Rollen und Rechte" baut ihre Auswahl aus **genau dieser gefilterten Liste**
(`users.js:621`, `this.appsNow()`). Eine neu eingerichtete App, die noch niemand benutzen
darf, stand also in keinem Schreibtisch und damit in keiner Rechteliste: **der Haken, der sie
sichtbar machen würde, war nur dort zu setzen, wo sie schon sichtbar ist.** Das ist Wand 6 von
sechs.

Behoben am 21.10.2026 mit `c0f0022`: `rights.Of` ruft für einen Admin `adminApps`
(`apps/server-api/classes/rights/rights.go:56–83`), und das setzt `<app>/use` **und** jede
Aktion jeder eingerichteten App — **ohne Zeile in der Datenbank**. Damit ist die Liste für
einen Verwalter vollständig, die App steht im Menü und also auch in der Rechteliste.
Dass das Recht in **keiner** Zeile liegt, ist dabei der Kern: ein Recht, das in einer Zeile
läge, könnte man dem Verwalter wegnehmen, indem man die Seite ohne den Haken speichert.

**Was davon bleibt, klein, aber echt:** `adminApps` und das Menü lassen eine
**abgeschaltete** App beide aus (`!a.Active`). Die Rechte einer App lassen sich also nicht
vorbereiten, solange sie aus ist. Das ist so gewollt und hier nur vermerkt.

### Schritt 13 — Die Oberfläche zieht sich selbst nach

**Du musst nicht mehr Strg+F5 drücken, und du sollst es auch nicht.** Am 21.10.2026 musste an
jeder Station von Hand neu geladen werden, und mehrfach sah ein veralteter Schirm wie ein
Fehlschlag aus. Das ist gebaut:

- Wer in **diesem Fenster** etwas ändert, sagt es weiter: eine App einrichten, ein Recht
  vergeben, einen Server aufnehmen. Die vier Zustände heißen `apps`, `rights`, `nodes`,
  `users` (`shell/changed.js:31–34`); die Seiten hängen sich daran
  (`users.js:486–490`, `store.js:149–150`) und bauen sich neu.
- **Kein Zeitgeber, mit Absicht.** Eine Liste von Apps ändert sich Minuten bis Monate
  auseinander; eine Frage je Sekunde wäre eine Frage je Sekunde für nichts, auf einem Tunnel,
  der jede andere Sitzung mitträgt (`changed.js:24–26`).

**Und was es ehrlich nicht kann:** eine Änderung, die **jemand anderes** gemacht hat. Ein
zweiter Verwalter richtet eine App ein, während dein Fenster offen steht — davon sagt keine
Nachricht der Sitzungsverbindung etwas. Die Verbindung trägt genau zwei Nachrichten von der
API zur Oberfläche: `trust` („es gibt eine neuere Fassung der Vertrauensliste") und `notify`
(ein Satz für den Menschen). Mehr gibt es nicht, und mehr zu erfinden wäre eine Änderung an
`shared/wire` — und das ist eingefroren (`changed.js:1–26`).

Deshalb sagt jede so gebaute Ansicht, **seit wann** sie steht, und bietet **einen Knopf**, der
neu fragt; und sie fragt **von selbst** neu, wenn du das Fenster wieder ansiehst
(`shell/stale.js`). Nicht nach einem Zeitgeber, sondern weil Zurückkommen genau der Moment ist,
in dem ein Mensch erwartet, zu sehen, was jetzt da ist. Ein Schirm, der alt sein kann, darf
nicht frisch aussehen.

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
- **Behoben in beiden Skripten** (ulh, `ngc-kette`, `223baac`). Auf der laufenden Anlage
  trägt `/etc/ngc/trust-anchor.json` `-rw-r--r--`. Siehst du trotzdem 500: Modus der Datei
  prüfen.
- **Nicht verwechseln mit Wand 1** (Schritt 3): dort darf die Datei nicht **entstehen**, hier
  darf sie nicht **gelesen** werden. Zwei verschiedene Fehler am selben Pfad.

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
  und in `ngc-web.sh`. Auf der laufenden Anlage steht `upgrade=websocket` am
  `BalancerMember`, und `ProxyPass "/api/" "balancer://ngc-api/api/"` ist nackt.
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
- **Also:** Schritt 7 **im selben Seitenaufbau** wie Schritt 6. Code holen, dann koppeln,
  dann aufnehmen, dann darf der Browser zu.
- **Punkt 60 sagt „den Knopf dafür gibt es nicht". Das ist falsch** (Berichtigung in
  Punkt 61). Der Knopf heißt „Kopplungscode zeigen". Der Zähler „null Treffer in
  `apps/web`" kam aus einem Arbeitsbaum auf `main`, wo `apps/web` damals nur zwei Dateien
  hatte. **Ein Zähler über den falschen Baum ist kein Befund.** Heute liegt der
  Oberflächencode auf `main` (`apps/web/public/assets/js/`).

---

## 5. Umgebungsfallen, die als Programmfehler aussahen

Die ersten drei nur in WSL gemessen (Punkt 56).

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
  Oberfläche, der wie ein Fehler von NGC aussieht. **Dasselbe gilt für Schritt 0 auf deinem
  eigenen Rechner:** fehlt der Eintrag dort, sieht es aus wie ein Fehler der Anlage.
- **In WSL lässt sich keine App einrichten.** Der Kern kann nicht verschlüsseln:
  `# CONFIG_FS_ENCRYPTION is not set` auf `6.18.33.2-microsoft-standard-WSL2`, gemessen am
  23.10.2026. `apps/server-controller/classes/serve/activate.go` sagt es an der Zeile selbst:
  auf so einem Kern endet **jede** Aktivierung in `runtime.ErrNoFscrypt`. **Alle**
  WSL-Instanzen teilen diesen Kern. Für Schritt 12 brauchst du eine echte Maschine.

---

## 6. Nicht gemessen

Das steht hier, damit niemand es für geprüft hält.

- **Diese Anleitung selbst ist nicht in einem Zug nachgefahren.** Die Schritte 0 bis 10 sind
  aus Messprotokollen und aus der laufenden Anlage zusammengestellt, die Schritte 11 bis 13
  aus dem Code und aus Punkt 71. **Niemand ist sie an einer frischen Maschine von Schritt 0
  bis Schritt 13 durchgegangen.** Genau das war die Ursache aller sechs Wände: jede
  Teilstrecke war geprüft, die Lücken lagen zwischen zwei geprüften Teilstrecken.
  `tools/erstlauf.sh` baut den Weg als Prüflauf nach — **lies, was er heute wirklich
  meldet**, bevor du dich darauf verlässt: am 23.10.2026 Phase A allein **1 rot**
  (Station 1, Wand 1) und **7 unmeasurable**, Ausstiegscode 1 (`docs/erstlauf.md`).
- **Wand 1 ist nicht behoben, nur umgangen.** Schritt 3 legt einen Drop-in an. Die Zeile
  `ReadWritePaths=${NGC_DATA} ${NGC_TLS}` steht am 24.10.2026 unverändert in `ngc-all.sh:1322`
  und `ngc-api.sh:1065` auf dem ulh-Zweig `ngc-kette`. Wer ohne Schritt 3 installiert, steht
  wieder vor der Wand vom Oktober.
- **Welche Nummer die Nummer des Release ist.** Jedes Programm trägt seine eigene
  (`0.2.0`, `1.0.0`, `0.2.1` in den `VERSION`-Dateien), ein Release hat **ein** Feld
  `Version` in `store.Release`, und welche der drei das sein soll, steht nirgends. Auf der
  laufenden Anlage melden beide Programme `0.2.2` und die Oberfläche `0.2.0` — sie sind also
  auch in Betrieb verschieden.
- **Ein echtes Zertifikat.** Gemessen wurde mit **selbst unterschriebenem** Zertifikat und
  einem Browser, der es für diesen Lauf annimmt (`ignoreHTTPSErrors`, `--host-resolver-rules`).
  Was ein Nutzer mit `letsencrypt` erlebt, ist **nicht** gemessen (Punkt 59). Was ein
  **echter** Browser mit dem selbst unterschriebenen Zertifikat macht, auch nicht.
- **Passkeys.** Der erste Verwalter läuft mit Passwort, und das ist vertragsgemäß. Die
  Oberfläche empfiehlt Passkeys selbst; der Weg dorthin ist nicht gemessen (Punkt 59). Dass
  sie über eine IP-Adresse **nicht** gehen, folgt aus `rp.id` — gemessen ist es nicht.
- **Ein zweiter Benutzer.** Ein Benutzer entsteht nur als **neue Fassung der
  Vertrauensliste**, und die wird **im Browser mit dem Schlüssel der Person unterschrieben**;
  die API prüft eine Liste, die man ihr reicht, und stellt keine aus. Keine Phase des
  Prüflaufs hält diesen Schlüssel, und darum ist Schritt 12 **nur mit dem ersten Verwalter**
  gegangen: dass das Recht „benutzen" bei einem **zweiten** Menschen wirklich ankommt, ist in
  keiner Phase gemessen (`docs/erstlauf.md`, Abschnitt 6).
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
- **Dass eine eingerichtete App wirklich antwortet, auf einer Maschine.** Am 23.10.2026 ging
  eine App auf und antwortete nicht: `ls` kam als **nichts** zurück, dasselbe `ls` im
  Container lieferte eine Liste — die Ausgabe ging **auf dem Weg** verloren, und ein offenes
  Fenster bewies gar nichts. Behoben mit dem Stand von `main` (PR 95/100), und die Trennung
  der beiden Ströme ist in `serve.RunService` gemessen. Dass **podmans** eigene Engine sie
  genauso übergibt (`runtime.Podman.Exec`), ist nirgends gemessen und braucht einen Container.
- **Mehrere Maschinen.** Diese Anleitung ist der kleine Fall: **eine** Maschine, SQLite
  (`config.DriverSQLite`), eine Datei je Programm — `/var/lib/ngc/api/ngc.db` und
  `/var/lib/ngc/controller/state.db`. MariaDB mit Galera ist ein anderer Fall und steht hier
  nicht (Punkt 47).
- **Der verteilte Fall ist hier nicht nachgefahren, nur nachgesehen.** Von der Anlage aus sechs
  Maschinen ist gemessen, was ein Browser von außen sehen kann: die sieben Namen und wohin sie
  wirklich auflösen, `config.json` mit `rp.id` und den beiden `apis`, die Zertifikate auf
  `:443` und auf `api-2:7443`, und welche der sechs auf Port 22 und 7443 antworten (alles
  24.10.2026). **Auf keiner der sechs war ich angemeldet**, und kein Schritt dieser Anleitung
  ist dort durchgegangen. Die Zuordnung Adresse → Name stammt von Thomas; dass sie stimmt, ist
  für `api-1`, `api-2` und `ngc.mein.lan` aus den Zertifikaten und aus `config.json` bestätigt,
  für `db-1`, `svc-1`, `svc-2` und `store` **nicht** — dort antwortet von außen nichts, was
  seinen Namen nennt.
- **Welches Skript beim verteilten Fall was macht.** Der kleine Fall nimmt `ngc-all`; sechs
  Maschinen brauchen `ngc-api`, `ngc-db`, `ngc-controller` und `ngc-web` einzeln, jedes mit
  eigener `answer.yaml`. Die Reihenfolge dieser vier untereinander, und wo dabei gekoppelt und
  aufgenommen wird, steht in **keiner** Anleitung und ist hier nicht gemessen.
- **Dass die fünf Skripte je auf ulh-`main` kommen.** Nachgezählt am 24.10.2026: `origin/main`
  hat weiter **null** `ngc-*`-Skripte. Ein Push nach ulh-`main` erreicht per Auto-Update
  sofort alle Nutzer, deshalb entscheidet das Thomas (Punkt 47).

---

## 7. Wo diese Anleitung etwas versprochen hat, das nicht trägt

Nicht weggelassen, sondern hingeschrieben.

- **„Der Tunnel steht" war nicht das Ende.** Die alte Fassung endete mit Schritt 8 und einer
  Gegenprobe. Wer nur das tat, hatte eine Anlage, in der sich **keine App einrichten lässt** —
  der Controller stand nicht in der Vertrauensliste, und niemand hatte Systemrechte. Drei
  Stationen fehlten: Schritt 9, 11 und 12.
- **„Einen Hostnamen" war zu wenig.** Der Satz stand in Abschnitt 2 als eine Zeile unter
  Werkzeugen. Dass der Name auf **dem Rechner des Lesers** eingetragen sein muss und dass es
  über eine IP-Adresse keine Passschlüssel gibt, stand nirgends. Jetzt ist es Schritt 0.
- **Die Anleitung sagte nicht, welche Anlage sie beschreibt.** Sie sagte „ein Rechner" und
  danach „`<host>`" — und es gibt inzwischen **zwei** Anlagen mit zwei verschiedenen Namen
  (`ngc.test` und `ngc.mein.lan`). Wer die verteilte vor sich hatte und diese Anleitung las,
  trug den falschen Namen ein und bekam keinen Satz, der das sagt. Jetzt steht oben eine
  Tabelle, und der verteilte Fall ist an jeder abweichenden Stelle benannt.
- **Die Reihenfolge der alten Schritte 4 bis 7 hielt nicht, was sie andeutete.** „Nicht neu
  laden" stand dort — aber das Aufnehmen des Controllers fehlte ganz, und genau das ist der
  Schritt, der **nach** dem Koppeln noch im selben angemeldeten Fenster geschehen muss.
- **„Dass Apps laufen" stand unter „Nicht gemessen" und klang wie eine Kleinigkeit.** Es war
  die Hälfte des Wegs, und auf ihr standen drei der sechs Wände.
- **Der Satz „Der Katalog dieses Stores ist nicht echt" ist weiter falsch formuliert**,
  solange PR #86 nicht in `main` ist. Er sagt „gefälscht", wo „noch nicht geholt" gemeint ist.
  Das ist dieselbe Klasse wie die Anmeldelüge aus Punkt 50: ein **leerer Zustand** wird als
  **Angriff** dargestellt. Wer das liest, sucht lange an der falschen Stelle.
- **Diese Anleitung wird wieder veralten**, und zwar an derselben Stelle wie zuletzt: zwischen
  zwei geprüften Teilstrecken. Der Schluss aus Punkt 71 gilt auch für sie — ein Durchlauf von
  Anfang bis Ende gehört in die Prüfung, nicht in die Woche eines Menschen. Solange er von
  Hand läuft, läuft er selten, und was nur selten läuft, bricht zwischen zwei Läufen an der
  nächsten kleinen Stelle.

---

## 8. Noch zu entscheiden (Thomas)

Außer den zwei Punkten aus Abschnitt 1:

- **Wand 1 richtig zumachen.** `/etc/ngc` gehört in `ReadWritePaths` von `ngc-api.service`,
  in `ngc-all.sh:1322` und `ngc-api.sh:1065`. Beide liegen im **ulh**-Repo auf Zweigen, an
  denen andere Sitzungen arbeiten. Solange das offen ist, braucht jede Installation den
  Drop-in aus Schritt 3 — eine Anleitung, die einen Handgriff gegen einen bekannten Fehler
  verlangt, ist die zweitbeste Lösung.
- **`noexec` auf `/data`.** Punkt 36 nennt es nicht, und es ist eine Verhaltensänderung für
  jede App, die unter `/data` Skripte hält (Punkt 51).
- **Die fünf ulh-Skripte nach `main` zusammenführen.** Ein Push dorthin erreicht sofort alle
  Nutzer (Punkt 47).
- **Wo gemessen wird:** entweder weiter mit selbst unterschriebenem Zertifikat, oder auf
  einer Maschine mit echtem Namen und `letsencrypt` — das ist der Fall, den ein Nutzer
  erlebt (Punkt 58), und es ist auch der einzige, in dem Passschlüssel überhaupt prüfbar sind.
- **Welche Nummer die Nummer eines Release ist.** Drei Programme, drei Nummern, ein Feld.
- **Eine Anleitung für die Anlage aus sechs Maschinen.** Es gibt sie nicht. Diese hier nennt
  den verteilten Fall an den Stellen, an denen er anders ist, und ist ausdrücklich keine
  Anleitung dafür. Wer sie schreibt, muss die Reihenfolge der vier Skripte messen — sie steht
  nirgends.
- **Der DNS, der jeden Namen beantwortet.** In diesem Netz lösen alle sieben Namen unter
  `ngc.mein.lan` auf `10.2.3.102` auf, wo kein NGC läuft (Schritt 0). Solange das so ist,
  braucht **jeder** Leser die Zeilen in seiner `hosts`-Datei, auch der mit DNS — und jeder
  Fehler sieht zuerst wie ein Fehler von NGC aus. Das ist keine NGC-Sache, aber es trifft
  jeden, der NGC hier aufsetzt.
