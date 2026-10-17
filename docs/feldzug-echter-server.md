# Feldzug: NGC auf einen echten Server

**Stand 12.10.2026.** Dieses Blatt liegt beim Einrichten neben der Tastatur.

> **Durchgesehen am 17.10.2026 gegen `origin/main` und die Vertragspunkte 56–62.**
> Zwischen dem 16. und dem 17.10.2026 ist NGC zum ersten Mal auf echtem Debian
> gelaufen — eine Anlage entsteht heute allein aus den Skripten (Punkt 62).
> Mehrere Punkte dieses Blattes beschreiben darum eine Lage, die es nicht mehr
> gibt. Jeder betroffene Punkt trägt unten einen Kasten „Überholt". **Ein
> Prüfplan mit Erledigtem wird nicht abgearbeitet** — wer hier weiterarbeitet,
> liest die Kästen zuerst. Die Zahl „44 offene Punkte" ist damit **zu hoch** und
> nicht neu gezählt.

Es führt die beiden Abschlussprüfungen vom 12.10.2026 zusammen (api: 23 Punkte,
controller: 22) und ordnet sie nach dem, **was voneinander abhängt**. Jeder Punkt
ist am Code auf `main` (`592e4da`) nachgesehen. Was inzwischen erledigt ist,
steht gestrichen unten im eigenen Abschnitt — ein Prüfplan mit Erledigtem wird
nicht abgearbeitet.

## Die Zahl, die zählt

**44 Punkte bleiben offen.**

| | Punkte |
|---|---|
| **Kein Gerät nötig** (Schreibtisch, hier) | **7** |
| **Eine Maschine** (Debian 13, root) | **33** |
| **Mehr als eine Maschine** | **4** |

Also: **40 von 44 brauchen höchstens einen Server.** Nur vier brauchen zwei oder
drei Maschinen — und alle vier gehen um dasselbe: zwei API-Server auf einer
Datenbank.

## Das fehlende KVM schließt nichts davon aus

Gemessen: der Proxmox-Host reicht keine Virtualisierung durch (`/dev/kvm` fehlt
auf der VM `ngc-recovery`, heute nachgesehen).

**Für diesen Feldzug ist das ohne Belang.** podman sind Container, keine
virtuellen Maschinen. MariaDB, Galera, systemd, fscrypt, Apache — nichts davon
braucht einen Hypervisor. Betroffen ist allein die Recovery-Arbeit (TPM, swtpm),
und die hat ihren gemessenen Umweg schon.

## Was heute gemessen wurde (ohne Gerät)

Alles in diesem Abschnitt ist heute entstanden, mit Sonde `(exit 7)` davor und
ohne Pipe vor einer Messung.

**1. Alle drei Programme bauen für Linux — und sie sind das erste Mal gelaufen.**

`CGO_ENABLED=0`, sechs Binärdateien, alle Code 0:

```
server-api, server-controller, cli  ×  linux/amd64, linux/arm64   = 6 × Code 0
```

Die amd64-Fassungen in WSL ausgeführt:

| Befehl | Code | Ausgabe |
|---|---|---|
| `ngc-api -version` | 0 | `ngc-api 1.0.0 (contract 1)` |
| `ngc-controller -version` | 0 | `ngc-controller 1.0.0 (contract 1)` |
| `ngc-store -version` | 0 | `ngc-store 1.0.0` |

Damit ist **Controller-Punkt 21 geschlossen**: mit `-ldflags` gebaut, die Nummer
ausgegeben, auf Linux. Stufe **ausgeführt**.

**2. Die beiden Dienstdateien sind gegen echtes systemd geprüft.**

WSL Debian 13.7 fährt **systemd 257 als PID 1** (`/proc/1/comm` = `systemd`,
`systemctl is-system-running` = `running`). Das war unbekannt und ist viel wert.

`systemd-analyze verify` nimmt `ngc-api.service` und `ngc-controller.service`
**ohne eine einzige Beschwerdezeile** an — jede Direktive erkannt.

**Vorsicht, gemessene Falle:** `systemd-analyze verify` endet auch bei einer
**erfundenen** Direktive mit Code **0** und schreibt nur eine Warnzeile
(`Unknown key 'GibtsNichtDirektive'`). **Der Beleg ist der leere Text, nicht die
Null.** Wer hier den Ausstiegscode liest, prüft nichts.

**3. Die vier ulh-Skripte sind syntaktisch heil.**

`bash -n` = 0 für `ngc-api.sh`, `ngc-controller.sh`, `ngc-db.sh`, `ngc-web.sh`
(je ~1250 Zeilen). Gegenprobe ohne Pipe: ein kaputtes Skript gibt Code **2**.

`dash -n` meldet bei allen vier Zeile 75 rot — **das ist kein Befund.** Die
Skripte haben `#!/bin/bash` und benutzen Arrays. Falsches Werkzeug, nicht
falscher Code.

**4. `shellcheck` gibt es hier nicht — und so wurde gesucht.**

Nicht in `PATH`; nicht unter `/usr/bin`, `/usr/local/bin`, `/bin`, `/snap/bin`,
`~/.local/bin`, `/opt/shellcheck`; rekursiv durch `/usr`, `/opt`, `/snap`, `$HOME`
bis Tiefe 4 nur eine bash-Vervollständigung, nicht das Programm.

Ebenfalls nicht da: `jq`, `podman`, `go` (in WSL), `fscrypt`, `apachectl`,
`mariadbd`. Da sind: `gcc`, `dpkg`, `apt-get`, `useradd`, `userdel`, `openssl`,
`curl`, `python3`, `systemd-analyze`.

**5. 69 Stellen im Prüfsatz überspringen sich.**

Heute statisch gezählt:

| Bereich | `t.Skip`-Stellen |
|---|---|
| `apps/server-controller` | **45** |
| `apps/server-db` | 9 |
| `apps/cli` | 6 |
| `apps/server-api` | 5 |
| `shared` | 4 |
| **gesamt** | **69** |

Die Gründe nennen sich selbst: „the system users of an app are Linux", „this
kernel or this filesystem cannot encrypt a directory", „who owns a directory is a
number of Linux". **Diese Zahl ist der beste Einzelmaßstab des ganzen Feldzugs:**
auf Debian als root muss sie einbrechen. Tut sie es nicht, läuft der Satz dort
genauso blind wie hier.

**6. Die VM `ngc-recovery` ist erreichbar — und ihr Dateisystem kann kein fscrypt.**

Heute per `ssh ngc-recovery` gemessen: Debian 13.7, Kernel 6.12.107, 4 Kerne,
4 GB RAM, 61 GB frei auf ext4, `sudo` ohne Passwort, systemd (`degraded`).

**Der Fund, der einen Servertag gerettet hätte:**

```
Filesystem features: has_journal ext_attr resize_inode dir_index orphan_file
filetype needs_recovery extent 64bit flex_bg metadata_csum_seed sparse_super
large_file huge_file dir_nlink extra_isize metadata_csum orphan_present
```

**`encrypt` fehlt.** Ohne dieses Merkmal kann fscrypt auf `/dev/sda2` nicht
arbeiten — der Controller endet dort mit `ErrNoFscrypt`, und Punkt E13 wäre vor
Ort nicht messbar. Entweder `tune2fs -O encrypt` (ungemountet) oder eine eigene
Partition oder eine Loop-Datei. **Vor der Reise entscheiden.**

Dazu: `go` ist dort **1.24.4**, `go.mod` verlangt **1.27**. Also wird **nicht auf
dem Server gebaut** — die Binärdateien kommen von hier (siehe Punkt 1).

Und eine Berichtigung nach `messen.md`: `useradd` meldete sich zuerst als
„FEHLT". Es liegt unter `/usr/sbin/useradd` und ist da; `/usr/sbin` steht nur
nicht im Pfad eines Nicht-root. **Ein Werkzeug, das „nicht gefunden" sagt,
antwortet über seinen Pfad, nicht über die Maschine.**

---

# Stufe 0 — Schreibtisch, kein Gerät

**Diese sieben kommen vor jeder Reise.** Ohne S1 startet nichts, ohne S2 und S3
gibt es keine Einrichtung.

### S1 — `ngc-api` kann gar nicht starten (Vertragspunkt 34)

> **Überholt, gemessen am 17.10.2026.** `apps/server-db/classes/register/register.go`
> meldet in `init` Opener, Migrator und Backuper an (Zeilen 44–46), und
> `apps/server-api/main.go:39` bindet das Paket als leeren Import ein.
> `ngc-api` läuft als Dienst auf Debian 13, `active` mit `NRestarts=0`
> (Punkte 56, 57, 62). **S1 ist zu, und damit der Riegel vor Stufe 1.**

Heute nachgesehen auf `main`: `dbopen.Register` hat **0** Produktionsaufrufer (nur
Testvorrichtungen), `dbopen.RegisterMigrator` ebenso **0**. `apps/server-db` hat
fünf Pakete mit Code, aber **keine `main.go` und keine Anmeldung**.

Im Lauf heute bestätigt: `ngc-api -config <datei>` endet mit Code 1 schon an der
Konfiguration, kommt also nie bis `dbopen.Open`.

**Zu messen:** `apps/server-db` ruft `dbopen.Register` und `RegisterMigrator`
einmal; `ngc-api` mit vollständiger `api.json` erreicht die Datenbank.
**Widerlegt wird es durch:** die Fehlerzeile „die Datenbank ist nicht erreichbar"
bei einer laufenden MariaDB.

**Alles ab Stufe 1 hängt daran.** Nichts anderes zuerst.

### S2 — Der Release-Packer liegt nicht auf `main`

> **Überholt, gemessen am 17.10.2026.** Unterschrieben wird heute mit
> `ngc-store release` aus `apps/cli` — `pack-release.sh` und `sign-release.sh`
> braucht niemand mehr. Es gibt Releases: dreimal `release ok` für `ngc-web all`,
> `ngc-api amd64` und `ngc-controller amd64` (Punkte 56, 57, 62). `tools/` auf
> `main` hat heute `check.sh`, `modules.go`, `pin-base.sh` und `tunnelprobe`;
> `local.env` ist weg.
> **Was an diesem Punkt bleibt:** `apps/web` liegt **nicht** auf `main`
> (`git ls-tree -r --name-only origin/main -- apps/web` = 2 Dateien,
> `origin/web` = 128) — wer nur `main` klont, kann das Release nicht bauen
> (Punkt 62, Befund 1).

Gemessen: `tools/` auf `main` hat `check.sh`, `local.env`, `modules.go` — und
sonst nichts. `pack-release.sh`, `sign-release.sh` und `tools/release/` liegen auf
dem Zweig `release-packer` (`merge-base --is-ancestor` → **nein**).

**Warum das ein Riegel ist:** `ngc-api.sh` holt sich ein **Release**
(`fetch_release`, `verify_release`). Ohne Release gibt es keine Einrichtung.

### S3 — Die vier Einrichtungsskripte liegen auf vier getrennten Zweigen

> **Teilweise überholt.** Punkt 62 hat eine ganze Anlage aus **einem** ulh-Zweig
> gebaut (`ngc-kette`, `223baac`), dazu `ngc-all.sh` als Zusammenfassung — die
> Skripte stehen sich also nicht im Weg. **Nicht überholt:** ob sie auf ulh-`main`
> liegen, ist nicht nachgezählt, und ein Push dorthin erreicht per Auto-Update
> sofort alle Nutzer. Das entscheidet Thomas.

Heute im ulh-Repo nachgesehen. Auf `main`: **kein** `ngc-*`-Skript.

| Zweig | Kopf | Skript |
|---|---|---|
| `ngc-api` | `045c380` | `ngc-api.sh` |
| `ngc-controller` | `9480f4e` | `ngc-controller.sh` |
| `ngc-db` | `33dce46` | `ngc-db.sh` |
| `ngc-web-kopfzeilen` | `75c63f4` | `ngc-web.sh` |

**Die vier haben noch nie zusammen in einem Stand gelegen.** Ein Server braucht
aber alle vier. Dass sie einzeln `bash -n`-heil sind (oben gemessen), sagt nichts
darüber, ob sie sich im selben `ulh` nicht gegenseitig im Weg stehen — gemeinsame
Namen, gemeinsames `/var/lib/ngc`, gemeinsame Pakete.

**Zu messen:** die vier in einen Zweig, dann ulhs eigener Prüfsatz und das Menü.

### S4 — Release bauen und unterschreiben

Offene Entscheidung aus `STAND.md`: mit welchem Werkzeug der Hardware-Stick
unterschreibt. `ngc-store` unterschreibt nur Test-Releases. **Das ist eine
Entscheidung, keine Messung** — sie muss vor S2/S3 fallen.

### S5 — Das Einbau- und Aktualisierungs-Teilsystem wird von niemandem gebaut

Am 17.10.2026 nachgemessen, **unverändert gültig**:
`grep -rn 'update\.New\|install\.New\|install\.Deps' apps/ --include=*.go | grep -v _test`
→ **0**. `run.go` setzt `Updater: nil` (am 17.10.2026 Zeile 587, am 12.10.2026
Zeile 531).

Folge: weder `install`, noch `stage`, noch die Rücknahme, noch `Resume` nach
einem Neustart ist je durch ein Programm gelaufen. **Das ist Bauen, nicht
Prüfen** — es gehört in den Plan, nicht in diesen Feldzug.

### S6 — Drei gebaute Prüfungen ohne Aufrufer

Heute gemessen, je 0 Produktionsaufrufer:

| Funktion | Was dadurch offen bleibt |
|---|---|
| `Stores.UsersAllowed` | der Sitzplatz einer Lizenz wird nie gefragt |
| `Stores.ContentKey` | ein Controller bekommt den Schlüssel nach einem Neustart nicht |
| `config.LoadAnchor` | **Vertragspunkt 33b**: wer die Datenbank schreiben kann, nimmt jede Rücknahme zurück |

`LoadAnchor` ist der schwerste der drei und kein Schönheitsfehler.

### S7 — Werkzeuge und Sprachen

- `govulncheck` nie gefahren. Geht hier, kein Gerät nötig.
- `shellcheck` fehlt (Suchorte oben). Beschaffen oder ersetzen.
- Die fünf Katalog-Übersetzungen des ulh-Eintrags hat kein Muttersprachler
  gelesen. Kein Gerät, aber ein Mensch.

---

# Stufe 1 — eine Maschine, Debian 13, root

**Was gebraucht wird:** ein Debian 13 (bookworm-Nachfolger, wie die VM:
Kernel 6.12), **root**, mindestens 4 Kerne, **8 GB RAM oder mehr** (E29 lädt
4 GiB in den Speicher), 60 GB Platte, **ein Dateisystem mit `encrypt`** (E13),
Netz nach außen (Release holen). Kein KVM.

Die Reihenfolge ist Pflicht: jede Gruppe macht die nächste erst messbar.

## 1.1 Der Boden: Dateisystem, Benutzer, Pakete (E10–E13)

**E10 — `apt-get` echt.** Bisher nur gegen eine selbst gebaute Go-Attrappe.
*Messen:* `update.System` spielt ein Paket ein; dann ein Abbruch **mitten** im
Einspielen (`dpkg` im halben Zustand).
*Widerlegt durch:* ein `ErrSystem`, das nicht kommt, obwohl `dpkg` gesperrt ist.

**E11 — Systembenutzer.** `useradd` mit `nologin` und Zuhause = Datenverzeichnis;
`userdel` **ohne `-r`**; kein Container läuft je als `root` des Servers.
*Widerlegt durch:* ein `userdel`, das Daten mitnimmt.

**E12 — subuid/subgid.** Ein Bereich je App, 65536 breit, ab 524288, nie
überlappend, ganz oder nicht geschrieben. **12 Fälle warten.**
*Messen:* `/etc/subuid` nach drei Apps und nach dem Löschen der mittleren.

**E13 — fscrypt.** Schlüssel geht in den Keyring, Verzeichnis ist ohne ihn
unlesbar. **Ein Fall, und der überspringt sich.**
*Vorbedingung:* das Dateisystem muss `encrypt` können — **auf `ngc-recovery`
heute gemessen: kann es nicht.**
*Widerlegt durch:* lesbarer Klartext nach `fscrypt`-Schlüsselentzug.

## 1.2 Die Datenbank (E1–E3)

**E1 — `ngc-api` erreicht die Datenbank.** S1 im Lauf.
*Messen:* `ngc-api` startet, `systemctl is-active` sagt `active`, das Protokoll
zeigt eine geöffnete Verbindung.
*Widerlegt durch:* „die Datenbank ist nicht erreichbar".

**Falle, heute gemessen:** `ngc-api` wird **vor** der Datenbank an der
Konfiguration abgewiesen. Erst eine vollständige `api.json` (das macht
`ngc-api.sh` mit `render_api_json`), sonst misst man die frühere Wand.

**E2 — `ngc-api migrate`.** Am 12.10.2026 antwortete es „dieses Programm wurde
ohne die Datenbank-Migrationen gebaut". **Überholt:** der Unterbefehl ist
verdrahtet (`apps/server-api/main.go:81`, `:175`, `:180`), und der Migrator ist
angemeldet (`register.go:45`). Gelaufen ist er nicht.
*Messen:* `ngc-api migrate` legt das Schema an; ein zweiter Lauf ändert nichts
(idempotent).

**E3 — Eine echte MariaDB.** Jede Fehlernummer, jede Wiederholung, jede
Transaktion war bisher gegen `memstore` gemessen.
*Messen:* `TestThePoolRetiresAConnectionAtItsLifetime` und die
Konformitätsfälle gegen die echte Datenbank.
*Widerlegt durch:* eine Fehlernummer, die der Code nicht kennt.

## 1.3 Dienste und Einrichtung (E4–E9)

**E4 — Dateirechte und Eigentümer.** 0700 der App-Verzeichnisse, 0600 von
`controller.json`, `api.json` und der bereitgelegten Datei, 0755 des eingebauten
Programms, Eigentümer `root`. **Mit Mutant M3 als ungedeckt gemessen.**
*Messen:* `stat -c '%a %U:%G'` über alle angelegten Pfade.

**E5 — systemd lädt die Dienstdateien.** Heute ist die Syntax gegen systemd 257
geprüft (oben) — **geladen ist keine.**
*Messen:* `systemctl daemon-reload`, `enable --now`, `is-active`; dann das
Programm von außen töten und `Restart=on-failure` mit `RestartSec=5` in Betrieb
sehen (Vertragspunkt 32).
*Widerlegt durch:* ein Dienst, der nach dem Töten nicht wiederkommt, oder einer,
der sofort zurückkommt und die 5 Sekunden nicht wartet.

**E6 — `ngc-api.sh` ganz durch.** Mit root, `dpkg`, `systemctl`, echtem `jq`.
Bisher nur Byte-Vergleiche und Funktionen einzeln.
*Messen:* ein Durchlauf, dann ein **zweiter** — der darf nichts ändern, was
richtig ist (`shared/ulh.md`).

**E7 — `ngc-db.sh` ganz durch**, samt `backup`/`restore`.

**E8 — `ngc-web.sh` ganz durch und Apache nimmt die Vorlage an.**
*Messen:* `apachectl configtest` und `graceful`; dann die neun Befehle aus
`apps/web/README.md`. **Befehl D zählt am meisten:** eine fehlende Datei unter
`/assets/` muss von Apache beantwortet werden, nicht von PHP — sonst stehen dort
zwei `Cross-Origin-Resource-Policy` und die Wand wirkt nicht.
*Widerlegt durch:* zwei gleichnamige Kopfzeilen in der Antwort.

**E9 — `ngc-controller.sh install` und `change_settings`.** Braucht `jq`.
Bisher 90 Fälle, Funktionen einzeln.

**Dazu, aus derselben Gruppe:**

**E27 — Die Strecke Apache ↔ API und der Chiffren-Riegel.** W-3 der API-Prüfung:
der Riegel hält nicht, was sein Satz sagt.
*Messen:* `openssl s_client -cipher` mit einer verbotenen Chiffre — die
Verbindung muss **scheitern**.
*Widerlegt durch:* eine Verbindung, die zustande kommt.

**E33 — Der ganze Prüfsatz auf Debian als root.**
*Messen:* `go test -v -count=1 ./...`, die Zahl der `--- SKIP` zählen.
*Erwartung:* deutlich unter den **69** Stellen von heute.
*Widerlegt durch:* eine Zahl in derselben Höhe — dann prüft der Satz dort
genauso wenig wie hier.
**Nicht `ok` lesen, sondern `=== RUN` zählen.**

## 1.4 Container (E14–E18)

Erst jetzt, weil podman Benutzer, subuid und verschlüsselte Verzeichnisse
braucht (1.1).

**E14 — podman überhaupt.** Kein Container ist je gestartet: nicht `load`, nicht
`create`, `start`, `exec`, `stop`, `rm`. **Die Argumentliste ist von keinem Fall
gehalten** (W-1).
*Widerlegt durch:* ein `create`, das podman mit „unknown flag" abweist.

**E15 — Die Härtung.** `--cap-drop ALL`, `--security-opt no-new-privileges`,
`--read-only`, `--network none`, `--user 0:0`. Geschrieben, von nichts gehalten,
nie gelaufen. **`TestAttackContainerEscape` überspringt sich.**
*Messen:* der Fall läuft und bleibt grün: `/etc/shadow` ist im Container nicht
lesbar, ins Netz kommt nichts.
*Widerlegt durch:* eine gelesene Zeile aus `/etc/shadow` oder ein geglücktes
`curl`.

**E16 — Rootless podman als App-Benutzer.** `podman.go:219-230` sagt selbst, das
müsse „die Installation herausfinden": Bildspeicher im Datenverzeichnis, Netz für
diesen Benutzer.

**E17 — Das Terminal-Abbild.** **Berichtigt am 17.10.2026:** hier stand,
`ngc-controller image` antworte „Dieser Teil ist noch nicht gebaut."
Das war schon am 12.10.2026 überholt und steht so auch in
`docs/controller-abnahme.md`, Punkt 15: `grep -rn "notReady" apps/server-controller/`
→ **0**, der Befehl ist ein Paket (`classes/node/image.go`,
`classes/runtime/build.go`) und läuft bis zu der Zeile, die podman startet.
Der `Containerfile` liegt da
(`apps/server-controller/templates/terminal/Containerfile`) und nagelt seine
Basis seitdem mit einem Digest fest: `debian:13-slim@sha256:a29215f6…`,
gehalten von `build.go:98`.
*Messen:* `podman build` auf einer Baumaschine mit dem gebauten
`ngc-controller` im Kontext; das Abbild als Datei; `CMD` ist
`ngc-controller hold`.
*Entscheidung offen:* bleibt `ngc-controller image` leer oder wird es gefüllt.

**E18 — Das PTY.** `/dev/ptmx`, `/bin/sh -i`, die Fenstergröße, die 30 Minuten
Leerlaufgrenze. Zwei Fälle warten; es läuft als `root` **im** Container.

## 1.5 Tunnel, Store, Apps (E19–E26, E28)

**E19 — Echter Tunnel API ↔ Controller.** Alle 15 Aktionen von `ngc.store` sind
bis an die Paketgrenze gemessen, keine über einen echten Tunnel. Geht auf
**einer** Maschine: zwei Dienste, zwei Ports.
*Messen:* der Controller wählt die API, der Handschlag kommt zustande,
`controllers.Tunnel` steht.

**E20 — Eine hochgeladene App aktivieren.** Vertragspunkt 30: niemand hält das
Paket zwischen Hochladen und Aktivieren, eine Aktivierung aus `core.LocalStore`
endet `E_UNREACHABLE`. **Festgenagelt, nicht gelöst.**
*Widerlegt durch:* eine Aktivierung, die durchgeht.

**E21 — Eine Update-Runde, die wirklich installiert.** `stage`, `install`, die
Rücknahme, `Resume` nach einem Neustart. Setzt S5 voraus.

**E22 — Die gebaute App-Aktualisierung.** `install.Needs`/`install.Update`:
gebaut, verdrahtet, von keinem Programm erreichbar.

**E23 — Ein echter Store.** Echte Zertifikatskette, echte Katalog- und
Release-Dateien unter den **angenommenen** Pfaden (eine Festlegung, keine
geprüfte Tatsache) und ein Server, der langsam tröpfelt.
*Messen:* die 60-s-Frist gegen einen Server, der ein Byte je Sekunde schickt.
*Widerlegt durch:* eine Verbindung, die nach 60 s noch offen ist.

**E24 — Lizenz.** Eine echte Lizenz eines echten Shops, eine Lizenz, die ein
**Controller** beurteilt (`VerifyLicenseFor`) — und: **heute verlässt kein
`E_LICENSE` das Controller-Paket.** Ein App-Kanal läuft auch für eine abgelaufene
Lizenz.
*Widerlegt durch:* ein offener Kanal trotz abgelaufener Lizenz.

**E25 — Der UI-Beweis auf dem lebenden Weg.** Die API prüft ihn **jetzt**
(`uiProof` → `Stores.Proof`, heute nachgesehen), die Shell prüft ihn auch — **nie
zusammen gelaufen.** Drei Schichten, eine Messung.
*Messen:* ein Schlüssel wird nach dem Einbau zurückgenommen; die Shell muss die
Oberfläche verweigern.

**E26 — Der Meldeweg Controller → Shell.** Verdrahtet (`server.go:202`,
`ctl.OnNotice = srv.notice`), **nie gelaufen.**
*Messen:* ein Controller schickt eine Meldung, eine offene Shell sieht sie; mit
**keiner** offenen Shell steht die Warnzeile im Protokoll.

**E28 — `controller.json`: zwei Felder ohne Leser.** **Nachgezählt am 17.10.2026**
(`grep -rn '\.<Feld>' apps/ shared/ --include=*.go` ohne Tests und ohne
`shared/config/`): acht der zehn haben heute Leser — `Podman` 5, `WebConfig` 3,
`AppDataDir` 10, `StageDir` 21, `BalancerFile` 8, `APITrustFile` 3, `WebRoot` 15,
`BinDir` 11. **Ohne Leser sind noch zwei:** `Migrate` und `APIPort`.
Am 12.10.2026 stand hier „zehn Felder".
**Ein Admin, der sie setzt, sieht die Prüfung durchgehen und bekommt kein
Verhalten.** Das ist die bösartigste Sorte Fehler in diesem Haufen.
*Messen:* je Feld einen Wert setzen, der sichtbar wirken müsste.

## 1.6 Grenzen und Last (E29–E32)

**E29 — Ein 4-GiB-Paket wirklich hochladen.** K-1: `ngc.store/api/upload` legt
das ganze Paket in den Arbeitsspeicher.
**Braucht mehr als 8 GB RAM; die VM hat 4 GB und wird sterben** — was genau die
Messung ist.
*Widerlegt durch:* ein Hochladen, das mit beschränktem Speicher durchgeht.

**E30 — Die Stillzeit eines weitergeleiteten Kanals.** Vertragspunkt 35: keine
Grenze. Was 2 048 Goroutinen und 256 MiB je Tunnel zeitlich **unbegrenzt** tun,
ist nicht gemessen.

**E31 — Gleichzeitige Sitzungen je Benutzer.** Unbegrenzt, nie gefahren.

**E32 — Zwei Controller auf einer Datei.** Die Sperre gilt nur für den Augenblick
der Migration (Vertragspunkt 32). Was danach passiert, wenn **beide** hochkamen:
nicht gemessen. Geht auf einer Maschine, zwei Prozesse.

---

# Stufe 2 — mehr als eine Maschine

**Alle vier drehen sich um dasselbe: zwei API-Server auf einer Datenbank.**
Vorher lohnt keine zweite Maschine.

**M1 — Zwei API-Server gegen eine Datenbank.** Davon hängen ab: dass genau einer
die Update-Runde führt, die Anmeldesperre (Vertragspunkt 7, **heute
prozesslokal**), die wartende TOTP-Geheimzahl (im Speicher **eines** Prozesses)
und das Zwischenlager einer hochgeladenen Datei.
*Messen:* derselbe Benutzer meldet sich an API 1 falsch an und versucht es an
API 2 — die Sperre muss greifen.
*Widerlegt durch:* eine Anmeldung, die an API 2 durchgeht.
**Braucht:** 2 Maschinen.

**M2 — Die Pacht der Update-Runde.** W-2: die Pacht ist **kürzer als die Runde**
— gerechnet, nie gefahren.
*Messen:* zwei APIs, eine Runde über 45 Minuten; nur einer darf führen.
*Widerlegt durch:* zwei Führende gleichzeitig.
**Braucht:** 2 Maschinen.

**M3 — Der Beitritt eines zweiten API-Servers.** `join.Run`: die Ein-Stunden-Frist
und `KeysRetry` = 5 s sind **gesetzt, nicht gemessen** (die Fälle setzen 20 ms).
**Braucht:** 2 Maschinen.

**M4 — Ein echter Galera-Verbund.** Einzel-MariaDB ist E3; der Verbund ist etwas
anderes.
**Braucht:** 3 Knoten.

---

# Gestrichen: was inzwischen erledigt ist

Heute am Code auf `main` nachgesehen. Wer diese Punkte noch in den alten
Berichten liest, sucht umsonst.

| Punkt | Warum gestrichen |
|---|---|
| api-10 / W-5: „Meldeweg Controller → Shell ist nirgends verdrahtet" | **ist verdrahtet**: `server.go:202` setzt `ctl.OnNotice = srv.notice`, `notice()` ab Zeile 528. Bleibt als **E26** (nie gelaufen) |
| api-14 / W-1: „Die API prüft den UI-Beweis nicht" | **prüft ihn**: `desktop.go:287` ruft `Stores.Proof`. Bleibt als **E25** (nie zusammen gelaufen) |
| api + ctrl-19: „`-race` nicht messbar" | **war ein Irrtum.** Gemessen: 30 Pakete, 0 `DATA RACE`. `shared/trust` wird rot, weil sein Fall Speicher-Obergrenzen misst |
| ctrl-21: „Version 1.0.0 nie von einem Bau mit `-ldflags` ausgegeben" | **heute geschlossen**: gebaut und auf Linux gelaufen, `ngc-controller 1.0.0 (contract 1)`, Code 0 |
| ctrl-8 (halb): „Die Dienstdatei ist nie geprüft worden" | **Syntax heute gegen systemd 257 geprüft**, ohne Beschwerde. Geladen ist sie nicht — bleibt als **E5** |
| „Die Programme bauen für Linux nur in der Theorie" | **sechs Binärdateien, Code 0**, amd64 und arm64, `CGO_ENABLED=0` |
| ctrl-12 (halb): „Das Terminal-Abbild hat keinen Erzeuger" | der `Containerfile` ist da; nur `ngc-controller image` ist leer. Bleibt als **E17** |

**Nicht auf `main`:** `docs/recovery-abnahme.md` liegt auf den Zweigen `recovery`
und `recovery-integration`, nicht auf `main` — am 17.10.2026 mit
`git ls-tree -r --name-only <zweig>` je Zweig nachgezählt. Hier stand „gibt es
nicht"; das war ein Zähler über den falschen Baum. Die Recovery-Arbeit ist ein
eigener Strang (Rust, `docs/recovery-modus.md`) und in diesem Feldzug nicht
enthalten.

---

# Vier Stufen, je Punkt

So steht es oben, und so ist es gemeint (`messen.md`):

| Stufe | heißt | heißt **nicht** |
|---|---|---|
| **geschrieben** | der Satz steht im Repo | dass er stimmt |
| **übersetzt** | Werkzeug und Syntax nehmen es an | dass es etwas tut |
| **ausgeliefert** | der committete Stand ist bitgleich mit dem gemessenen | dass ein Lauf ihn berührt hat |
| **ausgeführt** | ein Lauf hat die Zeile erreicht, mit Code und Fallzahl daneben | dass es auf einem echten Server so läuft |

Stand heute über den ganzen Feldzug:

**Stand 12.10.2026; überholt seit dem 17.10.2026** — eine ganze Anlage steht
(Punkte 56–62), die Tabelle ist nicht neu gezählt.

| | Punkte |
|---|---|
| **ausgeführt** | die drei `-version`-Läufe, sonst nichts von den 44 |
| **übersetzt** | alles Übrige, plus die zwei Dienstdateien und die vier Skripte |
| **geschrieben** | S4 (Entscheidung), S7 (Übersetzungen) |

---

# Regeln für jede Messung dieses Feldzugs

Alle fünf sind in diesem Projekt bezahlt worden.

1. **Nie `| tail` oder `| head` vor eine Messung.** Der Ausstiegscode gehört dem
   letzten Glied der Pipe. Heute wieder passiert: eine Gegenprobe zeigte Code 0,
   während das Programm rot war.
2. **`systemd-analyze verify`: den Text lesen, nicht den Code.** Code 0 auch bei
   erfundener Direktive — heute gemessen.
3. **Nicht `ok`, sondern `=== RUN` zählen.** Ein Satz, der sich stumm selbst
   abschaltet, sieht grün aus. `t.Skipf` schreibt seine Zeile nur unter `-v`.
4. **`$?` ist in `wsl -- sh -c '…'` schon tot.** Messskript als **Datei**, Sonde
   `(exit 7)` davor. Und `MSYS_NO_PATHCONV=1` setzen, sonst verbiegt Git Bash den
   Pfad zu `C:/Program Files/Git/mnt/c/…` — heute wieder passiert.
5. **Ein Werkzeug, das „nicht gefunden" meldet, antwortet über seinen Pfad.** Eine
   Abwesenheit wird mit der **Liste der Orte** belegt, nicht mit der Zahl der
   Sucher. `useradd` war heute genau dieser Fall.
6. **Vor jeder gemeldeten Zahl im selben Aufruf:**
   `git diff --name-only --diff-filter=D HEAD | wc -l` muss **0** sein.

---

# Die kürzeste Fassung

1. ~~**S1 zuerst** — ohne `dbopen.Register` startet `ngc-api` nicht.~~
   **Erledigt am 17.10.2026** (siehe S1).
2. ~~**S2 und S3** — ohne Release und ohne die vier Skripte in einem Stand gibt
   es keine Einrichtung.~~ **Erledigt** (siehe S2, S3); offen bleibt nur, dass
   `apps/web` nicht auf `main` liegt.
3. **Dann eine Maschine.** 33 Punkte, in der Reihenfolge 1.1 → 1.6.
4. **Vor der Reise:** ein Dateisystem, das `encrypt` kann, und 8 GB RAM.
5. **Zwei Maschinen erst zum Schluss**, für die vier Punkte der Stufe 2.
6. **Der Maßstab ist die 69.** Fällt die Zahl der Skips auf Debian nicht
   deutlich, hat der Feldzug nichts gemessen.
