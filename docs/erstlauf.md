# Erstlauf — der Weg vom Release bis zur App im Menü als Prüflauf

Stand 23.10.2026. Lauf: `bash tools/erstlauf.sh`.

Zwischen dem 19. und 21.10.2026 wurde zum ersten Mal eine NGC-Anlage aus einem veröffentlichten
Release auf einer echten Maschine installiert. Dabei standen **sechs** Hürden im Weg — jede
einzeln klein, zusammen unüberwindbar. **Keine davon hat ein Prüfsatz bemerkt.** Dieser Lauf baut
den Weg nach, damit eine siebte nicht wieder erst von Hand gefunden wird.

## 0. Der Weg war nach der letzten Station nicht zu Ende

Am 23.10.2026 wurde der Weg zum ersten Mal ganz durchgegangen — und **hinter** Station 8 standen
noch **zwei** Hürden. Der Lauf von diesem Tag hätte keine davon bemerkt:

1. **Der geschlossene Kreis beim Freigeben.** Der Schreibtisch zeigt eine App nur dem, der das
   Recht `<app>/use` hält; die Rechte-Seite baute ihre Liste der Apps **aus diesem Schreibtisch**.
   Das erste Recht einer frisch eingerichteten App war damit für niemanden zu vergeben. Behoben
   (`rights.adminApps`) — **Station 8 wäre damit weiter grün**, denn Station 8 fragt nach dem
   **Menü**, und der Kreis liegt in der **Seite, die das Menü füllt**.
2. **Die App ging auf und antwortete nicht.** `muh` kam zurück als
   `/bin/sh: 2: muh: not found`, `ls` kam zurück als **nichts** — und dasselbe `ls` im Container
   lieferte eine Liste. Die Ausgabe ging also **auf dem Weg** verloren, und ein offenes Fenster
   bewies gar nichts. Behoben mit dem Stand von `main` (PR 95/100).

Daraus sind **Station 9 und Station 10** geworden. Beide werden gemessen, indem der Code
**ausgeführt** wird (`tools/erstlauf-probe`), nicht indem ein Wort gesucht wird — beide Hürden
waren in jeder Textsuche grün, während die Anlage stand.

**Und beide Ströme getrennt.** Bei Hürde 2 kam **einer** der beiden Ströme an und der andere
nicht. Eine Station, die „es kam etwas zurück" liest, wäre genau auf diesen Fehler grün gewesen.
Station 10 fragt darum zwei Dinge auf zwei Zeilen: kommt **stdout** auf dem Kanal an, den der
Mensch ansieht, und bleibt **stderr** davon weg und landet im Journal.

## 1. Was es heute schon gibt — und wo es aufhört

| Prüfsatz | Was er umfasst | Wo er aufhört |
|---|---|---|
| `tools/check.sh` | gofmt, `go vet` (4 Konfigurationen), `go test ./...`, `go build` für linux/amd64 und arm64, der PHP-Selbsttest, die Modultyp-Sonde | **Beim Übersetzen und bei Unit-Tests.** Er installiert nichts, startet nichts und kennt keine Datei außerhalb dieses Repos. Er ist bemerkenswert gründlich gegen *hohle Prüfungen* (leere `go list`, `gofmt`-Exit 2, fehlende Dateien im Baum) — aber jede dieser Prüfungen fragt nach dem Inhalt dieses Repos. |
| `apps/web/tests/` (Playwright) | Die Browser-Verträge und die Oberfläche gegen einen Attrappen-Server | **Am Attrappen-Server.** Kein echter API-Knoten, keine Vertrauensliste aus einer Datenbank, kein Controller. |
| `apps/server-controller/classes/e2e` | zwei Dateien, Go-Ebene | Im Prozess. Keine Maschine, kein systemd, kein podman. |
| ulh: `apps/cli/tests/ngc-*.test.sh` (auf den Zweigen `ngc-kette` / `pruefsatz-ngc-kette`, **nicht** auf `main`) | Was die Installationsskripte schreiben, ohne Server: `api.json` und die Unit **Byte für Byte**, und welche Namen, Adressen und Codes sie ablehnen | **Am Text.** Der Fall vergleicht die Unit mit einer wörtlichen Kopie ihrer selbst — einschließlich der Zeile `ReadWritePaths=/var/lib/ngc /etc/ngc/tls`. Damit ist **Hürde 1 in einem Prüffall eingefroren**: der Fall beweist, dass die Datei sich selbst gleicht. |

### Drei der sechs Hürden lagen gar nicht in einer `.go`-Datei

Das ist der Kern des Befundes, nicht eine Randbemerkung:

1. **Hürde 1 — die systemd-Unit liegt in einem anderen Repo.** `ngc-api.service` wird von
   `apps/cli/assets/scripts/ngc-api.sh` im **ulh**-Repo geschrieben. In `nextgencloud` gibt es
   keine einzige Datei mit `ProtectSystem` oder `ReadWritePaths` (gemessen: Treffer nur in drei
   Dokumenten unter `docs/`). `tools/check.sh` kann diese Hürde prinzipiell nicht sehen.
2. **Hürde 2 — die Version kommt aus einem Bau-Flag, das nur ein Dokument verlangt.**
   `-ldflags "-X main.Version=…"` steht in `docs/release-weg.md`, `CLAUDE.md` und in alten Plänen.
   **Kein Skript im Repo baut die Server.** Der Rückfall im Code ist `0.0.0-dev`, und die
   Oberfläche verwirft auf eine Version, die `validVersion` ablehnt, die **ganze** Antwort.
3. **Hürde 5 — ein Dateimodus.** `apps/server-controller/classes/runtime/data.go` legte
   `config.AppDataDir` mit `0700` an (seit PR 92: `0711`). Kein Unit-Test kann das als Fehler
   erkennen, weil es im Prozess als root richtig aussieht; falsch wird es erst, wenn ein
   *anderer* Benutzer hindurchmuss.

Die anderen drei (3 Katalogfrist, 4 leere `rootUsers`, 6 geschlossener Rechtekreis) sind je für
sich korrekter Code. Sie werden erst beim Zusammensetzen zu einer Sackgasse.

## 2. Wie weit der Lauf gehen kann — und warum nicht weiter

**Zwei Phasen, beide gebaut, beide gelaufen.**

```powershell
bash tools/erstlauf.sh --machine erstlauf-vm --machine-staged /root/release/dist --machine-ulh /root/release/ulh --machine-release-key /root/release/dist/release-key.pem
```

- **Phase A — `tools/erstlauf.sh`, statisch, läuft überall.** Die Stationen 1, 2 und 7 haben eine
  Hälfte, die aus den Quellen dieses Repos **und des ulh-Repos zusammen** entscheidbar ist. Läuft
  in Sekunden, braucht keine Maschine.
- **Phase B — `tools/erstlauf-maschine.sh`, läuft ALS ROOT AUF der Wegwerf-Maschine.** Sie
  installiert NGC so, wie ein Nutzer es tut: Testrelease nach `/var/lib/ngc/staged`, dann das
  ulh-Skript `ngc-all.sh`, dann die Stationen. `--machine <ziel>` in Phase A legt das Skript per
  `scp` hinüber und fährt es per `ssh`; zusätzliche ssh-Optionen kommen aus `NGC_SSH_OPTS`, damit
  nichts in die ssh-Konfiguration des Nutzers geschrieben werden muss.

### Der Unterschied der beiden Phasen ist Station 1

Phase A fragt den **Text** des ulh-Skripts. Phase B fragt die **Sandbox**: `systemctl show` sagt,
was systemd wirklich gewährt (Drop-ins eingerechnet, die niemand angesehen hat), und
`systemd-run` mit genau diesen zwei Einstellungen versucht den Schreibvorgang. Eine Unit, die in
der Datei richtig aussieht und trotzdem verbietet, fällt nur hier.

### Wo Phase B endet, und warum (Zeilen, nicht Vermutung)

| Station | Was fehlt |
|---|---|
| 3 koppeln | Der Kopplungscode kommt **aus dem Browser** (`pair_code`). Dafür braucht es das Webinterface: ulhs `ngc-web.sh` mit Apache und PHP — `ngc-all.sh` installiert das nicht (`NGC_PACKAGES=(openssl jq curl ca-certificates podman)`). |
| 4, 5 | Brauchen Station 3. |
| 6 Katalog | Braucht einen Store mit signiertem Katalog in der Vertrauensliste. NGCs eigener Store-Schlüssel ist im Skript noch ein Platzhalter (`NGC_STORE_KEY_DEFAULT=REPLACE-THIS-LINE-…`). |
| 7 App einrichten | **Der Kern kann nicht verschlüsseln.** `# CONFIG_FS_ENCRYPTION is not set` auf `6.18.33.2-microsoft-standard-WSL2`, gemessen am 23.10.2026. `apps/server-controller/classes/serve/activate.go` sagt es an der Zeile selbst: auf so einem Kern endet **jede** Aktivierung in `runtime.ErrNoFscrypt`. **Alle** WSL-Instanzen teilen diesen Kern. |
| 8 | Braucht Station 7 und einen zweiten Benutzer. |

### Warum eine WSL-Instanz und keine echte VM

Gemessen am 23.10.2026, mit den Orten:

- **Kein QEMU:** nicht in `C:\Program Files\qemu`, nicht in `PATH` auf Windows, nicht in der
  Debian-WSL-Instanz (`command -v qemu-system-x86_64`).
- **Keine Virtualisierung in der Instanz:** `/dev/kvm` fehlt, und `/proc/cpuinfo` nennt
  **0**-mal `vmx` und **0**-mal `svm`. Ohne KVM bliebe nur TCG-Emulation — für eine
  Debian-Installation samt podman unbrauchbar.
- **Die eine eingerichtete Maschine darf nicht kaputtgehen.** Auf `ssh ngc` (10.2.3.208) steht die
  Anlage, in der andere Sitzungen arbeiten; ihr Zustand ist mehrfach von Hand verändert. Sie wurde
  **nicht angefasst**.

Die Wegwerf-Instanz heißt `ngc-erstlauf` und liegt in `D:\claude\ngc-erstlauf-vm`; das
Testrelease in `D:\claude\ngc-erstlauf-release`. Beides ist meins und darf weg.

**Lieber ein ehrlicher Lauf über die halbe Strecke als ein grüner, der nichts beweist.**

### Der eine Riegel, den Phase B ersetzt

`ngc-all.sh` trägt den öffentlichen Release-Schlüssel von NGC im Text, und niemand außer NGC kann
ein Release signieren, das er annimmt. Phase B nennt darum mit `--test-release-key` eine
**Test-Wurzel** und sagt es in jeder Ausgabe. Was weiter läuft: die Signaturprüfung selbst, samt
Prüfsummen und Größe — eine manipulierte Datei fällt hier wie im Feld. Was **nicht** gemessen ist:
dass NGCs eigener Schlüssel im Skript steht und stimmt.

## 3. Die Stationen

| # | Station | Aussage, die rot wird | Hürde | heute |
|---|---|---|---|---|
| 1 | installieren | **A:** Jeder Pfad, den `ngc-api` **schreibt**, muss von `ReadWritePaths=` der Unit gedeckt sein — beide Hälften aus den beiden Repos **gelesen**, nicht abgetippt. **B:** die Installation kommt mit Code 0 durch, und ein Prozess in der **echten** Sandbox der Unit kann `/etc/ngc/trust-anchor.json` schreiben. | 1 | **ROT**, in beiden Phasen |
| 2 | API und Controller laufen | **A:** `tools/build-servers.sh` muss es geben, muss laufen, und die Programme müssen auf `-version` eine Nummer melden, die das **Muster der Oberfläche** annimmt (aus `contracts/text.js` gelesen); Gegenprobe im selben Lauf: ein Bau **ohne** `-ldflags` muss abgelehnt werden. **B:** das **installierte** `/usr/lib/ngc/ngc-api` meldet so eine Nummer, `ngc-api.service` ist aktiv und behält **eine** MainPID über 10 s. | 2 | grün |
| 3 | Controller koppeln | Kopplung gelingt und der Controller meldet sich. | — | UNMEASURABLE |
| 4 | aufnehmen | Ein frisch aufgenommener Controller ist über die Oberfläche **änderbar** — nicht nur `rootUsers: []`. | 4 | UNMEASURABLE |
| 5 | Systemrechte setzen | Systemrechte sind über die Oberfläche setzbar. | 4 | UNMEASURABLE |
| 6 | Katalog da | Der Katalog ist **Minuten** nach der Installation da, nicht eine Stunde. | 3 | UNMEASURABLE |
| 7 | App einrichten | Der Modus von `config.AppDataDir` muss einen App-Benutzer **hindurchlassen** (x-Bit für andere) — die Begründung ist die Aussage, nicht die Zahl. | 5 | grün (seit PR 92) |
| 8 | App erscheint im Menü | Die Rechte-Seite bietet Rechte auch für Apps an, die man noch **nicht** sieht. | 6 | UNMEASURABLE |
| 9 | Recht „benutzen" vergeben | **A:** Eine frisch eingerichtete App ohne jede Rechte-Zeile steht in den eigenen Rechten eines Admins (`rights.Of` — also in seinem Menü, also in der Liste der Rechte-Seite), der Vertrag nimmt das Recht an (`core.ValidRight`), und nach `rights.Set` **hält** ein Benutzer der Rolle es wirklich. Gemessen durch **Ausführen**, nicht durch `grep`. | 9 | grün |
| 10 | App ist wirklich bedienbar | **A:** Was ein Dienst auf **stdout** schreibt, kommt auf dem Kanal des Menschen an; was er auf **stderr** schreibt, kommt dort **nicht** an, sondern in der Journal-Zeile des Aufrufs. Zwei Zeilen, zwei Ströme. | 10 | grün |
| 9+ | Benutzer anlegen (Unterschrift) | — | — | **NICHT GEPRÜFT**, in keiner Phase — siehe Abschnitt 6 |
| 10+ | App antwortet auf der Maschine | podmans eigene Engine verdrahtet die beiden Ströme genauso. | — | UNMEASURABLE |

Warum 3, 4, 5, 6 und 8 keine statische Hälfte haben, steht als Kommentar im Lauf selbst — mit
Grund je Station, damit der Nächste eine Lücke in der Suche sehen kann statt einem Satz zu glauben
(`messen.md`: eine Abwesenheit belegt man mit Orten).

## 4. Was der Lauf gefunden hat

Gemessen am 23.10.2026, `main` eingemischt, beide Phasen in einem Zug: **2 rot, 6 unmeasurable**,
Ausstiegscode 1.

### Hürde 1 ist nicht behoben, und Phase B zeigt sie am laufenden System

```
station 1 installieren: FAIL -- ngc-api.service grants ProtectSystem=strict with
  ReadWritePaths='/var/lib/ngc /etc/ngc/tls', and a process in that sandbox CANNOT write
  /etc/ngc/trust-anchor.json: touch: cannot touch '…': Read-only file system
```

Die Installation lief dabei mit **Code 0** durch, die API läuft, der Einmal-Code wurde gezeigt —
und der Vertrauensanker ist trotzdem unschreibbar. Genau die Lage vom Oktober.

**Beweis, dass die Aussage der Behebung folgt** (am laufenden System, Drop-in, `systemctl show`
sieht ihn):

| Lage | `ReadWritePaths` | Schreiben |
|---|---|---|
| wie installiert | `/var/lib/ngc /etc/ngc/tls` | **geht nicht** |
| Drop-in mit `/etc/ngc` | `/var/lib/ngc /etc/ngc` | **geht** |
| Kontrolle: `RestartSec=7` dazu | `/var/lib/ngc /etc/ngc` | geht (unverändert) |
| Drop-in weg | `/var/lib/ngc /etc/ngc/tls` | **geht nicht** |

### Hürde 2 ist behoben — von diesem Zug

`tools/build-servers.sh` baut die drei Programme mit `-ldflags "-s -w -X main.Version=…"` aus den
`VERSION`-Dateien. Station 2 **führt es aus** und fragt die Programme; die Gegenprobe im selben
Lauf verlangt, dass ein Bau ohne das Flag von demselben Muster **abgelehnt** wird. Auf der
Maschine meldet das installierte `/usr/lib/ngc/ngc-api` **0.2.0**, der Dienst ist aktiv und
behielt eine MainPID über 10 s.

### Neu gefunden: ohne eigenen Store kommt keine Installation zustande

Nicht gesucht, vom Lauf gefunden. Mit beiden Store-Antworten leer bricht `ngc-all.sh install` ab:

> NGC has no official app store yet, so this installation needs a store of its own: give its
> address and its key in the two questions about the store. Leaving them empty would write a
> placeholder into `/etc/ngc/api.json`, and the API refuses that file.

Eine Installation aus einem veröffentlichten Release ist heute also **nur** möglich, wenn der
Admin sich einen Store ausdenkt. Phase B nennt darum einen (`https://store.erstlauf.invalid`),
sagt das laut, und Station 6 bleibt deshalb unmeasurable — dort antwortet nichts.

### Hürde 5 war die erste echte Probe des Laufs

Bei der ersten Messung (Basis `b782635`) war Station 7 rot, weil `data.go` dort noch `0o700`
schrieb. PR 92 hat daraus `0o711` gemacht; derselbe Lauf, unverändert, meldet seitdem `ok`. Die
Station folgt also der Behebung und nicht einer abgetippten Zahl.

Hürde 1 wird hier nicht behoben: die Datei liegt im **ulh**-Repo auf Zweigen, an denen andere
Sitzungen arbeiten.

## 5. Der Lauf prüft erst sich selbst

Nach `messen.md`, und jede dieser Prüfungen hat eine gemessene Vorgeschichte:

- **Schale:** ohne `BASH_VERSION` bricht er ab (ein berechneter Pfad landete mit `sh` schon auf `/`).
- **Ort:** der berechnete Wurzelpfad wird gegen etwas gehalten, das nur das richtige Verzeichnis
  hat (`AnchorFile` in `shared/config/config.go`), **bevor** irgendetwas gelesen wird.
- **Dichtheit:** die Sonde `(exit 7)` läuft im Band. Eine Schale, die Ausstiegscodes verschluckt,
  schreibt sonst `FAIL` und Code 0 in denselben Lauf.
- **Baum:** `git diff --diff-filter=D` muss 0 sein — eine fehlende Testdatei macht einen Lauf
  still kleiner.
- **Vorbereitung je Station:** `broken()` trennt einen **Fehler dieses Skripts** von einem Befund
  über NGC und bricht ab, statt eine Zahl zu nennen. Fällt `render_unit` weg, wird `ProtectSystem`
  nicht mehr gesetzt, verschwindet eine Konstante aus `config.go` oder nennt der benannte
  Schreiber sie nicht mehr, dann sagt der Lauf das — er meldet nicht grün.
- **Muster-Übertragung:** das Versionsmuster wird aus `contracts/text.js` gelesen und
  **in beide Richtungen** geprüft: es muss `1.2.3` annehmen und `0.0.0-dev` ablehnen. Sonst war
  die Übertragung falsch und nichts darunter bedeutet etwas.
- **Was der Lauf NICHT tut:** er sucht nicht per `grep` nach `main.Version=` unter `tools/`. Ein
  Kommentar hätte das erfüllt, und die Station wäre grün geworden, während der Release weiter
  `0.0.0-dev` trägt — genau das Loch, gegen das sie geschrieben ist. Darum wird der Erbauer
  **ausgeführt** und die Programme werden **gefragt**.

## 6. Station 9 und 10: was gemessen ist, und was ehrlich nicht

### Was der Lauf nicht klicken kann, und darum nicht meldet

Ein zweiter Mensch braucht einen **Benutzer**, und ein Benutzer entsteht nur als **neue Fassung
der Vertrauensliste** — die wird **im Browser mit dem Schlüssel der Person unterschrieben**; die
API prüft eine Liste, die man ihr reicht, und stellt keine aus. Keine Phase dieses Laufs hält
diesen Schlüssel. Die Sonde baut ihre Liste darum **im Speicher** und sagt das.

Das ist **keine Station, die auf eine Maschine wartet**: `--machine` hilft hier nicht. Sie wird
deshalb als eigene Zeile `station 9 Benutzer anlegen (Unterschrift): UNMEASURABLE` gemeldet und
zählt in die Liste „diese Stationen liefen NICHT" — nicht übersprungen, nicht grün.

Dasselbe für die zweite Hälfte von Station 10: **wo** die beiden Ströme sich trennen, ist gemessen
(`serve.RunService`); dass **podmans** eigene Engine sie genauso übergibt (`runtime.Podman.Exec`),
ist nirgends gemessen und braucht einen Container.

### Die Sonde prüft erst sich selbst

- **Eigene Vorbereitung:** `rights.All` muss für die zwei Rollen der Liste antworten und für beide
  **leere** Rechte melden — eine Vorrichtung, in der schon Rechte stehen, ist nicht der frische
  Fall. Die beiden Benutzer-IDs werden gegen `trust.ValidID` gehalten, bevor etwas daraus
  geschlossen wird.
- **Ein Fehler der Sonde ist kein Befund:** schreibt die Engine der Sonde nicht, oder endet
  `serve.RunService` nicht mit `ok`, dann sagt die Sonde `broken:` und der Lauf bricht ab, statt
  eine Zahl zu nennen.
- **Der Lauf zählt Zeilen, nicht Ausstiegscodes:** `tools/erstlauf.sh` verlangt **jede** der fünf
  Prüf-Kennungen (`recht-angeboten`, `recht-gueltig`, `recht-vergeben`, `ausgabe-stdout`,
  `ausgabe-stderr`). Fällt eine weg, meldet der Lauf „cannot measure" — eine Sonde, die weniger
  druckt, als sie schuldet, kann den Lauf nicht still kleiner machen.
- **Die Vorbereitung, die die Sonde absichtlich NICHT nachbildet:** der Kreis läuft durch zwei Orte
  außerhalb von `package rights` — den Filter des Menüs (`coreapp/desktop.go`,
  `rq.Rights[a.ID+"/"+core.RightUse]`) und die Liste der Rechte-Seite (`apps/users.js`,
  `this.appsNow()` samt `core.RIGHT_USE`). Eine Sonde, die diese Bedingung selbst abschreibt,
  beweist ihre eigene Kopie. Der Lauf prüft stattdessen, dass beide Orte noch dort lesen, und sagt
  „cannot measure", wenn einer umgezogen ist.
- **Und was die Vorbereitung bei Station 10 bewusst NICHT festnagelt:** welcher der beiden Ströme
  welchen Platz bekommt. Würde das hier festgenagelt, dann käme ein **Tausch** der beiden — genau
  der Fehler vom 23.10.2026 — als „cannot measure" zurück statt als rote Station.

### Beweis, dass die beiden Stationen den Fehler FINDEN

`messen.md`: eine Prüfung, die den Fehler nicht nachweislich findet, misst nichts. Alle Läufe am
23.10.2026 im Arbeitsbaum `D:\claude\nextgencloud-erstlauf-weiter`, Basis `0bf7201`, jeweils
`bash tools/erstlauf.sh`:

| Änderung | Station 9 | Station 10 |
|---|---|---|
| unverändert (`main`) | **ok** | **ok** |
| `rights.adminApps`: die Zeile `out[a.ID+"/"+core.RightUse] = true` entfernt | **FAIL** — „der Admin hält `de.example.frisch/use` NICHT … das erste Recht einer frisch eingerichteten App kann dann niemandem gegeben werden" | ok (unberührt) |
| `serve.RunService`: `Stdout` und `Stderr` **getauscht** | ok (unberührt) | **FAIL** — „der Kanal trug das STDERR des Dienstes und NICHT sein stdout — das ist genau der Fehler vom 23.10.2026", **und** eine zweite Zeile für den anderen Strom |
| `serve.RunService`: `Stdout` auf `io.Discard`, `in.Out` bleibt genannt | ok (unberührt) | **FAIL** — „nichts von dem, was der Dienst auf STDOUT schrieb, kam auf dem Kanal an (er trug 0 Bytes)" |
| **Kontrolle:** `errs` → `errRing` in `call.go`, Laufvariable `a` → `row` in `adminApps` | **ok** | **ok** |

Die Kontrolle ist mit Absicht eine **Umbenennung in genau den beiden Zeilen, um die es geht**: sie
beweist, dass die Stationen dem **Verhalten** folgen und nicht einer Schreibweise. Ein weiterer
Lauf, bei dem `in.Out` ganz aus `call.go` verschwand, meldete
`erstlauf: the run cannot measure station 10` und Code 1 — also auch dort kein grüner Lauf,
sondern eine Vorbereitung, die abbricht.

### Was der Lauf heute meldet

Phase A allein, `main` (`0bf7201`), gemessen 23.10.2026: **1 rot** (Station 1 — Hürde 1 ist weiter
offen und liegt im ulh-Repo), **7 unmeasurable**, Ausstiegscode 1. Station 9 und 10 melden `ok`.

## 11. Die verteilte Anlage — was eine einzelne Maschine verdeckt

Stand 24.10.2026. Lauf: `bash tools/erstlauf-verteilt.sh`, gemessen gegen sechs Maschinen
(`tools/proxmox-sechs.sh`). Maschinen nicht in `~/.ssh/config`:
`NGC_SSH_OPTS="-i ~/Documents/ssh/keys/id_ed25519_ngc -o IdentitiesOnly=yes"`.

Phase A liest die Quellen, Phase B (`--machine`) eine Maschine, auf der **alles** liegt.
**Phase C** ist diese Datei: dieselben Stationsnummern, gefragt dort, wo die Teile auseinander
stehen. Wer nur Phase A und B fährt, erfährt von dem hier gar nichts — nicht weil die Stationen
grün sind, sondern weil die Fragen auf einer Maschine keinen Inhalt haben.

### Die Rollen lügen, also werden sie gemessen

Die Namen der Maschinen sagen nicht, was auf ihnen läuft. Gemessen am laufenden Dienst,
24.10.2026:

| Maschine | Name | läuft wirklich |
|---|---|---|
| 10.2.3.210 | api-1 | `apache2`, `ngc-api`, `ngc-controller` |
| 10.2.3.211 | api-2 | `ngc-api` |
| 10.2.3.212 | db-1 | `mariadb` **und** `ngc-controller` |
| 10.2.3.213 | svc-1 | `ngc-controller` + podman |
| 10.2.3.214 | svc-2 | `ngc-controller` + podman |
| 10.2.3.215 | store | `nginx`, kein NGC |

`db-1` trägt **beides** — die Datenbank und einen Controller. Wer die Rolle aus dem Namen
abliest, hält `db-1` für reine Datenbank und `api-1` für reine API; beide stimmen nicht.

### Drei Abstürze, die den Lauf stumm beendet haben

Der Lauf hat gegen diese Anlage nach Station D1 **aufgehört** — Ausstiegscode 60, keine weitere
Zeile, keine Summe. Nichts in der Ausgabe sagte, dass sieben Stationen nie gefragt wurden.
Dreimal dieselbe Form, und `messen.md` nennt sie: eine Ersetzung, die scheitert, beendet unter
`set -e` den ganzen Lauf.

| Stelle | was scheitert | Folge |
|---|---|---|
| Station D2 | `curl` meldet 60 für ein Zertifikat, das es nicht prüfen kann | Lauf endet nach D1 |
| Station D4 | `sed` über eine Datei, die es nicht gibt — unter `set -o pipefail` | Lauf endet nach Station 7 |
| Station 7 (Datenträger) | `findmnt` findet nichts, `[ -n "" ]` ist falsch, der Fernbefehl endet mit 1 | Lauf endet **vor** der Station, die genau das melden wollte |

Dazu: `NGC_SSH_OPTS` fehlte in dieser Datei. `tools/erstlauf.sh` trägt es seit PR 99; hier
antwortete jeder Aufruf „Permission denied (publickey)" und der Lauf schrieb es den **Maschinen**
zu („the six have to be up") statt sich selbst.

### Station D0 — die Namen zeigen dorthin, wo die Teile stehen

**Die neue Station, und sie steht zuerst, weil jede Station hinter ihr als NGC-Fehler zu lesen
ist, solange sie rot ist.** Auf einer Maschine hat die Frage keinen Inhalt: dort ist alles
`localhost`, und ein Name, der auf die falsche Maschine zeigt, landet trotzdem auf der richtigen.

Gemessen: der DNS dieses Netzes (10.2.3.102) antwortet auf **jeden** `*.ngc.mein.lan`-Namen mit
einem AAAA-Satz `::` **neben** dem richtigen A-Satz. `getent`, `curl` und alles andere über NSS
nehmen das `::` zuerst, verbinden sich zur **eigenen** Maschine und treffen dort, was auf 443
lauscht — auf api-1 ist das `apache`. Darum antwortete „holt api-1 den Katalog vom Laden?" mit
`self-signed certificate, 404`, während der Laden gesund fünf Meter daneben stand.

**Go geht die Adressen der Reihe nach durch und fällt an der falschen vorbei, darum läuft NGC
selbst.** Jedes Werkzeug, zu dem ein Mensch greift, läuft nicht. Fünf von sechs Namen sind rot;
`api-1` ist grün, weil es auf seine **eigenen** echten IPv6-Adressen auflöst.

**Was die Station nicht fängt**, und das steht in ihrem Kopf: ein Name, der die echte
IPv6-Adresse der **falschen** Maschine liefert, kommt durch. Der Lauf kennt die sechs über ihre
IPv4-Adressen und hält den A-Satz dagegen; eine IPv6-Adresse hat er von keiner.

### Station D2b — der Laden ist AUS

**Der Satz, der hier stand, war gegen die Quelle falsch**, und das ist mehr wert als die Station:
er sagte, das sei messbar, „but it costs an hour of wall clock: the store round first beats an
hour after the start and then hourly (server.go says why)". `server.go` sagt das Gegenteil —
`FirstCatalog = time.Minute`, mit sechs eigenen Zeilen darüber, warum eine Minute und nicht eine
Stunde (V1.2 Punkt 66). Die Station kostet **einen Neustart und zwei Minuten** und war für eine
Stunde als nicht messbar abgelegt, die es nicht gibt.

Gemessen auf **api-2** (nicht auf api-1, und **ohne** den Laden abzuschalten — der Laden ist eine
Maschine, auf der andere arbeiten). Der Hebel ist eine Blackhole-Route auf der zweiten API:

| Mutation | Zeile des Ladens | Katalog | Journal |
|---|---|---|---|
| `ip route add blackhole 10.2.3.215/32` auf api-2, Neustart | `{"checked":1792844524,"error":"Der Store hat nicht oder nicht brauchbar geantwortet."}` | 1116 Bytes, **unverändert** | **0 Zeilen** |
| **Kontrolle:** Route weg, Neustart | `{"checked":1792845105}` — Fehler **weg** | 1116 Bytes | 0 Zeilen |

Zwei Zusagen halten: die Anlage **schreibt den Fehler dorthin, wo ein Admin ihn liest**, und sie
**behält den letzten guten Katalog** (`appstore.Refresh`: „a store that fails leaves the last good
catalog in place"). Eine Maschine, die neu startet, kostet die Anlage also nicht ihr Menü.

**Und ein Befund, der in der Stationszeile steht statt verschluckt zu werden: das Journal schreibt
keine einzige Zeile.** `Server.hour` meldet nur, wenn `Stores.Refresh` einen Fehler liefert — und
`Refresh` liefert nie einen: es notiert den Grund je Laden und wirft den Fehler der Notiz weg. Ein
Laden, der eine Woche weg ist, ist nur für den sichtbar, der sich anmeldet und die Zeile anschaut.

Die Station läuft **nicht** im normalen Lauf. Sie braucht `--laden-weg`, weil sie die Anlage für
vier Minuten verändert, und sie weigert sich, wenn `--api1` und `--api2` dieselbe Maschine sind.

### Station D3b — eine Anmeldung, die auf api-2 landet

Die alte Station D3 fragte, ob api-2 installiert ist und sein Dienst läuft. Das ist eine andere
Frage als die, die ein Mensch hat: der Browser nimmt für **jeden** Aufruf **eine** API aus
`/etc/ngc/web.json`, also landet die Hälfte der Anmeldungen auf api-2. Eine zweite API, die oben
ist und Anmeldungen anders beantwortet, sperrt die Hälfte der Leute aus, während jede Statuszeile
grün bleibt.

Gemessen: `web.json` nennt beide APIs mit einem TLS-Pin, und der Pin für api-2 **ist** api-2s
eigener Schlüssel (`t2isQ…`, und api-1s `j7gMS…` wird davon abgelehnt). `POST
/api/v1/login/start` antwortet auf api-2 über Netz **genau wie** auf api-1
(`{"status":"error","code":"E_PROTOCOL","message":"Ungültige Nachricht."}` auf denselben
absichtlich leeren Rumpf — eine Form, kein Ausweis, darum braucht es dafür keinen Schlüssel).

### Station D4 — eine App landet auf dem Rechner, auf den man sie schickt

Mit einer Maschine hat die Frage keinen Inhalt: eine App, die überhaupt eingerichtet wurde, wurde
an der einzigen Stelle eingerichtet, die es gibt. Mit zwei Dienstrechnern hat sie Inhalt, und es
ist die Frage, die ein Admin wirklich hat — er hat eine Maschine ausgewählt und will diese.

Vier Dinge je Knoten, und alle vier zusammen, weil jedes einzelne von einer halb fertigen
Einrichtung erfüllt wird: eigener **Benutzer**, dessen **Heim** die App-Verzeichnis unter
`config.AppDataDir` ist, das **Bild im Bildspeicher genau dieses Benutzers**, und ein **laufender
Container** dieses Benutzers.

**Der dritte Punkt ist eine Falle, in die diese Station beim Schreiben selbst getreten ist:**
`podman images` als `root` antwortete für svc-1 (dort hielt `root` das Bild zufällig auch) und
nicht für svc-2 — und die erste Lesung dieser Maschinen sagte, die App sei nicht auf svc-2
gelandet, obwohl sie dort lief. Das Journal (`useradd`, `image loadfromarchive`, `container
start`) hat es richtiggestellt.

**Die Gegenprobe sind die Maschinen, an die niemand etwas geschickt hat:** api-1 und db-1 müssen
von allen vier Dingen keines zeigen. Eine Prüfung, die überall eine App findet, würde „es ist dort
gelandet, wo man es hingeschickt hat" zu „irgendwo ist irgendwas" machen.

### Beweis, dass die neuen Stationen den Fehler FINDEN

`messen.md`: eine Prüfung, die den Fehler nicht nachweislich findet, misst nichts. Alle Läufe
24.10.2026 im Arbeitsbaum `D:\claude\nextgencloud-erstlauf-verteilt` gegen dieselbe Anlage.

| Mutation | Station | Ergebnis |
|---|---|---|
| `--store 10.2.3.214` (Laden auf die falsche Maschine gezeigt) | D0 (store) | **FAIL** — und mit der **anderen** Begründung: „the A record … is '10.2.3.215' and this run was told the machine is 10.2.3.214" statt der `::`-Zeile |
| Der Pin, den `web.json` für api-2 trägt, durch api-1s Schlüssel ersetzt | D3b | **FAIL** — „web.json pins api-2 at 'j7gMS…' and api-2 really serves 't2isQ…'" |
| `appdir=/var/lib` (ein Pfad, den **jede** Maschine hat) | D4 (svc-1, svc-2, Gegenprobe) | **3× FAIL** — 24 bzw. 25 „Apps", keine davon vollständig, und die Gegenprobe meldet api-1 und db-1 als belegt |
| `--svc1 10.2.3.210 --svc2 10.2.3.212` (Dienstrechner auf api-1/db-1 gezeigt) | 7 (Datenträger), 7 (die App selbst) | **3× FAIL**, D4 wird ehrlich UNMEASURABLE statt grün |
| Blackhole-Route zum Laden auf api-2 | D2b | **Fehler in der Zeile des Ladens**, Katalog unverändert |
| **Kontrolle:** unverändert | alle davon | **ok** |
| **Kontrolle:** Route weg, derselbe Neustart | D2b | Fehler **weg**, `checked` weitergerückt |

Die beiden Kontrollen sind der Punkt: ohne die zweite Zeile zu D2b wäre eine Zeile, die aus einem
anderen Grund „error" sagt, als Beweis durchgegangen.

### Was der Lauf heute meldet

`main` + dieser Zweig, gemessen 24.10.2026, ohne `--laden-weg`: **28 Stationen, 6 rot,
6 unmeasurable**, Ausstiegscode 1, kein `erstlauf-verteilt: ok`. Mit `--laden-weg`: 6 rot,
5 unmeasurable.

Die sechs roten sind **fünf Namen** (Station D0) und **Station D2**, die an denselben fünf Namen
hängt. Es ist **ein** Fehler, und er liegt im DNS dieses Netzes, nicht in NGC.

### Was nur ein Mensch drücken kann

Weder Phase A noch B noch C kann das, und keine Station überspringt es — jede sagt es als
UNMEASURABLE mit ihrem Namen:

1. **Die erste Einrichtung** einer Anlage. `GET/POST /api/v1/setup`, mit einem MAC über den
   Einmalcode aus `/var/lib/ngc/setup-code`. Alles dahinter hängt an einem angemeldeten Admin.
   *(Auf dieser Anlage schon getan — `setup-code` ist weg.)*
2. **Eine Anmeldung auf api-2.** `https://ngc.mein.lan` öffnen, anmelden, und das **zweimal mit
   einem Neuladen dazwischen**, weil `web.json` api-1 und api-2 abwechselnd herausgibt.
3. **Eine App auf einen bestimmten Rechner schicken.** In der Oberfläche eine App einrichten,
   dabei **svc-2 mit Absicht** auswählen — und hinterher sagen, welche Maschine gewählt wurde.
   Welcher Knoten gewählt wurde, hält keine Datei auf keiner Maschine fest; der Vergleich ist
   eine Sache, die nur der machen kann, der geklickt hat.
4. **Systemrechte vergeben** (Station 5) und **das Recht „benutzen"** (Station 8). Station 8 ist
   dabei nicht ungemessen, sondern **unerreichbar**: v0.2.1 kann keinen zweiten Benutzer anlegen.

Und zwei Stationen, die ein Mensch nicht drücken muss, aber ein Lauf von sich aus nicht anfassen
darf:

5. **Der Rückdreh** (`store.ErrRollback`). Dafür müsste eine **niedrigere** Folge vom Laden
   ausgeliefert werden, also auf die Laden-Maschine geschrieben — abzustimmen mit dem, der dort
   Apps veröffentlicht.
6. **`--laden-weg`** selbst: vorher ansagen, mit Maschine und Zeitfenster. An dieser Anlage
   messen mehrere Sitzungen gleichzeitig.
