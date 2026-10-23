# Erstlauf — der Weg vom Release bis zur App im Menü als Prüflauf

Stand 23.10.2026. Lauf: `bash tools/erstlauf.sh`.

Zwischen dem 19. und 21.10.2026 wurde zum ersten Mal eine NGC-Anlage aus einem veröffentlichten
Release auf einer echten Maschine installiert. Dabei standen **sechs** Hürden im Weg — jede
einzeln klein, zusammen unüberwindbar. **Keine davon hat ein Prüfsatz bemerkt.** Dieser Lauf baut
den Weg nach, damit eine siebte nicht wieder erst von Hand gefunden wird.

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

Die Wegwerf-Instanz heißt `ngc-erstlauf` und liegt in `D:\claude
gc-erstlauf-vm`; das
Testrelease in `D:\claude
gc-erstlauf-release`. Beides ist meins und darf weg.

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
