# Sechs Debian-VMs unter Proxmox — die verteilte Anlage

Stand 24.10.2026. Gebaut und gemessen auf dem Wirt `pve` (10.2.3.2,
pve-manager 9.2.3), VMIDs 300–306. Skripte:

```bash
bash tools/proxmox-sechs.sh pruefen          # jeden Wert gegen den Wirt halten
bash tools/proxmox-sechs.sh vorlage          # Debian-Abbild holen, Vorlage 300
bash tools/proxmox-sechs.sh anlegen          # die sechs klonen und starten
bash tools/proxmox-sechs.sh namen --setzen   # Namen auflösen lassen
bash tools/proxmox-sechs-grundlage.sh pakete
bash tools/proxmox-sechs-grundlage.sh platte # die verschlüsselbare Apps-Platte
bash tools/proxmox-sechs-ngc.sh laden        # das Release auf die fünf
bash tools/proxmox-sechs-ngc.sh ulh
bash tools/proxmox-sechs-ngc.sh store        # der Laden auf eigener Maschine
bash tools/proxmox-sechs-ngc.sh db
bash tools/proxmox-sechs-ngc.sh api1
bash tools/proxmox-sechs-ngc.sh web
bash tools/erstlauf-verteilt.sh              # der Erstlauf über die verteilte Anlage
bash tools/proxmox-sechs.sh abbauen --vorlage  # alles wieder wegräumen
```

`tools/erstlauf.sh`, `tools/build-release.sh`, `tools/ci-count.sh` und
`tools/check.sh` sind **nicht angefasst**; `tools/erstlauf-verteilt.sh` ist eine
eigene Datei, Phase C neben deren Phase A und B.

---

## Warum sechs

| # | VM | Rollen | Wofür es diese Maschine braucht |
|---|---|---|---|
| 1 | `ngc-web-api-1` (301) | `web api` | Oberfläche und eine API. Erst damit hat Apaches Lastverteilung überhaupt ein Mitglied und `web.json` einen echten Fingerabdruck. |
| 2 | `ngc-api-2` (302) | `api` | Nur so lässt sich der Weg „beitretende API" (`join_code`) und der Riegel „das ist der einzige API-Server dieser Anlage" treffen. |
| 3 | `ngc-db-1` (303) | `db` | Erst getrennt zeigt sich, ob die Anlage ohne Datenbank auf derselben Maschine lebt. |
| 4 | `ngc-svc-1` (304) | `services` | Trägt die Apps, podman und die verschlüsselten App-Ordner. |
| 5 | `ngc-svc-2` (305) | `services` | Die Oberfläche lässt beim Einrichten einen Rechner **auswählen**, und die Update-Runde geht über mehrere Knoten. Mit einem einzigen ist beides nur behauptet. |
| 6 | `ngc-store` (306) | — | **Der Laden gehört nicht zur Anlage.** Diese VM trägt kein NGC und keinen Controller: nur nginx und signierte Dateien. |

---

## Die Werte — was gemessen ist und was du prüfen musst

Alles davon steht im Kopf von `tools/proxmox-sechs.sh`, jede Zeile mit der
Herkunft darüber. Nichts ist erfunden.

| Wert | genommen | woher |
|---|---|---|
| Knoten | `pve` | `hostname` auf 10.2.3.2 antwortet `pve` (der ssh-Eintrag heißt `pve-2`/`proxmox`, `qm` will den Knotennamen) |
| Speicher | `ssd_2TB` | `pvesm status`; dort liegt auch die Platte der bestehenden NGC-VM (208), 1,03 TiB frei |
| Brücke | `vmbr0`, `firewall=1` | `qm config 208`; `vmbr0` ist die einzige Brücke mit Carrier (`intern` hat NO-CARRIER) |
| VMIDs | 300 Vorlage, 301–306 | `/etc/pve/.vmlist` hält 100–116 und 200–208; ab 300 ist frei (so von Thomas benannt) |
| Netz | `10.2.3.210–215/8`, GW `10.0.0.1` | `vmbr0` trägt `10.2.3.2/**8**`, Standardroute über 10.0.0.1. Das `/8` ist ungewöhnlich und ist dieses Netz — ein `/24` schneidet die VMs vom Gateway ab. |
| DNS | `10.2.3.102`, Suchdomäne `mein.lan` | `/etc/resolv.conf` des Wirts |
| Adressen frei | ja | 10.2.3.210–220 antworteten am 23.10.2026 auf keinen Ping |
| CPU-Typ | `x86-64-v2-AES` | `qm config 208`. Das AES ist hier keine Zierde: MariaDB mit Verschlüsselung und das TLS von fünf Maschinen laufen darauf. |
| Abbild | `debian-13-genericcloud-amd64.qcow2` | cloud.debian.org, mit `SHA512SUMS` daneben geprüft (OK) |
| Schlüssel | `/root/.ssh/id_rsa.pub` des Wirts **und** der öffentliche Teil von `~/Documents/ssh/keys/id_ed25519_ngc` | damit du von deinem PC ohne neuen Schlüssel hineinkommst |

**Was du prüfen musst, weil ich es nicht wissen kann:**

1. **Die sechs Adressen.** Sie antworten auf keinen Ping — das heißt nicht, dass
   kein DHCP-Bereich sie abdeckt. Deckt er sie ab, ändere die Tabelle
   `MASCHINEN` im Skriptkopf.
2. **Die Zone `ngc.mein.lan`.** Frei gewählt. Soll die Anlage anders heißen,
   ist `NGC_ZONE` der eine Ort dafür.
3. **Der Name im DNS.** Ich habe die Namen über `/etc/hosts` auf den sechs
   gesetzt (siehe unten). Auf deinem PC fehlt der Eintrag noch.

---

## Die Größen, und worauf sie beruhen

Der Wirt hatte **21 GiB von 62 GiB RAM frei** (`free -g`) und 20 Kerne. Die
sechs nehmen zusammen **13,5 GiB** — mit Luft, und ohne der bestehenden NGC-VM
(8 GiB) etwas zu nehmen.

| VM | RAM | Kerne | Platte | Grund |
|---|---|---|---|---|
| web-api-1 | 2048 MiB | 2 | 16 GB | Apache, php-fpm, `ngc-api` und `ngc-store`. Gemessen nach der Installation: 1,9 GB belegt. |
| api-2 | 2048 MiB | 2 | 16 GB | wie oben ohne Apache |
| db-1 | 2560 MiB | 2 | 24 GB | MariaDB 11.8 mit Galera; `innodb_buffer_pool_size` steht auf 128 MiB, der Rest ist Datei-Cache und Platz für `mariabackup` |
| svc-1 | 3072 MiB | 2 | 24 GB **+ 16 GB** | Trägt podman-Abbilder und je App einen verschlüsselten Ordner. Maßstäbe: das Terminal-Abbild ist 108 MB, sein entpackter Datenordner 110 MB — und eine Aktivierung legt ein Paket von bis zu `store.MaxPackage` = **4 GiB** in den Spool. Die zweite Platte trägt `/var/lib/ngc/apps`. |
| svc-2 | 3072 MiB | 2 | 24 GB **+ 16 GB** | dito |
| store | 1024 MiB | 1 | 16 GB | nginx und signierte Dateien. Ein Katalog ist Kilobytes; Pakete bis 4 GiB liegen hier, wenn du welche anbietest. |

Platte zusammen: 160 GB auf `ssd_2TB` (1,03 TiB frei).

---

## Fallstrick 1 — fscrypt. Ein frisches Debian bringt es NICHT mit

Eine App von NGC lebt in einem verschlüsselten Ordner unter
`config.AppDataDir` (`/var/lib/ngc/apps`). Der Controller macht das mit den
ioctls des Kernels selbst (`FS_IOC_ADD_ENCRYPTION_KEY`,
`FS_IOC_SET_ENCRYPTION_POLICY` in
`apps/server-controller/classes/runtime/data_linux.go`). Auf einem
Dateisystem, das nicht verschlüsseln kann, antworten sie `ENOTTY`/`EOPNOTSUPP`,
daraus wird `runtime.ErrNoFscrypt`, und **jede** Aktivierung endet dort
(`classes/serve/activate.go` sagt es an der Zeile).

Drei Dinge, getrennt gemessen am 23./24.10.2026:

- **Der Kernel kann es.** Debian 13 trixie, `6.12.113+deb13-cloud-amd64`:
  `CONFIG_FS_ENCRYPTION=y` in `/boot/config-$(uname -r)`.
- **Das Dateisystem kann es nicht.** `/etc/mke2fs.conf` von Debian 13 listet
  für ext4 `has_journal,extent,huge_file,flex_bg,metadata_csum,metadata_csum_seed,64bit,dir_nlink,extra_isize,orphan_file`
  — **kein `encrypt`**. Zweimal gemessen: auf einem Loop-Abbild mit e2fsprogs
  1.47.2 (der Fassung von Debian 13) ließ `mkfs.ext4` das Merkmal weg und
  `tune2fs -O encrypt` setzte es; und die laufende NGC-VM trägt `encrypt` auf
  `/`, weil es dort einmal von Hand gesetzt wurde.
- **Das Paket `fscrypt` braucht NGC nicht.** Auf der laufenden VM ist es
  installiert, NGC ruft es nie auf — der Controller macht die ioctls selbst.
  Nötig ist das Merkmal des Dateisystems, sonst nichts.

**Darum eine zweite Platte und kein `tune2fs` auf der Wurzel.**
`tune2fs -O encrypt` auf der **eingehängten** Wurzel einer laufenden Maschine
ist das Einzige in diesem Aufbau, das ich **nicht** gemessen habe — und das
Dateisystem, von dem die Maschine läuft, ist ein schlechter Ort dafür. Eine
zweite Platte mit `mkfs.ext4 -O encrypt`, eingehängt auf `/var/lib/ngc/apps`,
beantwortet dieselbe Frage mit nichts im Risiko; die ioctls gehen je
Dateisystem, also sitzt der Ordner genau dort, wo der Controller ihn braucht.

Willst du es doch auf der Wurzel: in der Rettungs-Shell des Installers, mit
der Wurzel **nicht** eingehängt, `tune2fs -O encrypt /dev/sda1`, danach mit
`dumpe2fs -h` nachsehen. Die Skripte hier tun es nicht.

Gemessen nach `platte`: auf svc-1 und svc-2 liegt `/var/lib/ngc/apps` auf
`/dev/sdb`, ext4 **mit** `encrypt`, Modus `0711` (derselbe, den
`runtime/data.go` selbst setzt — Hürde 5 vom Oktober), podman 5.4.2.

**Nebenbefund zu `tools/erstlauf-maschine.sh`** (nicht geändert, andere
Sitzung): dessen fscrypt-Probe in Station 7 ist auf Debian in **beiden**
Hälften falsch. `zgrep CONFIG_FS_ENCRYPTION=y /proc/config.gz` — diese Datei
gibt es auf Debian nicht; und `[ -d /sys/fs/ext4/features/encryption ]` — der
Eintrag ist eine **Datei** (`-r--r--r-- 1 root root 4096`), kein Ordner, also
ist `-d` falsch auf einem Kernel, der es kann. Die Station meldet darum auf
jeder Debian-Maschine „dieser Kernel hat kein CONFIG_FS_ENCRYPTION", und das
ist das Gegenteil der Wahrheit. `tools/erstlauf-verteilt.sh` fragt die
Kernel-Konfiguration unter `/boot` und den sysfs-Eintrag mit `-e`.

---

## Fallstrick 2 — die Namen müssen auflösen

Die Anlage antwortet unter ihrem **Namen**. Zertifikat und Passkey hängen am
Namen: über eine IP warnt der Browser, und Passkeys gehen über eine IP gar
nicht.

**Auf den sechs** ist es gesetzt (`proxmox-sechs.sh namen --setzen`): jede
Maschine trägt den Block unten in `/etc/hosts` und heißt mit
`hostnamectl set-hostname` nach ihrem DNS-Namen.

**Auf deinem PC fehlt er noch.** Nach
`C:\Windows\System32\drivers\etc\hosts`, als Administrator:

```
10.2.3.210 api-1.ngc.mein.lan api-1
10.2.3.211 api-2.ngc.mein.lan api-2
10.2.3.212 db-1.ngc.mein.lan db-1
10.2.3.213 svc-1.ngc.mein.lan svc-1
10.2.3.214 svc-2.ngc.mein.lan svc-2
10.2.3.215 store.ngc.mein.lan store
10.2.3.210 ngc.mein.lan
```

Die Alternative ohne hosts-Einträge: dieselben sieben Namen als Rewrites in
AdGuard Home auf 10.2.3.102 — dann findet sie jedes Gerät im Netz. Ich habe
AdGuard **nicht** angefasst; das ist dein Dienst und eine Änderung dort trifft
alles im Haus.

---

## Fallstrick 3 — der Abbau

```bash
bash tools/proxmox-sechs.sh abbauen            # die sechs weg, Vorlage bleibt
bash tools/proxmox-sechs.sh abbauen --vorlage  # auch die Vorlage 300
```

Räumt `qm stop` + `qm destroy --purge --destroy-unreferenced-disks` über
301–306 (und 300). Die bestehende NGC-VM **208** steht in keiner Tabelle
dieser Skripte und wird von keinem Aufruf berührt.

---

## Was der verteilte Lauf gefunden hat

Fünf Dinge standen zwischen einem frischen Debian und einer laufenden Anlage.
Vier davon liegen in ulh (`origin/ngc-kette` bei `afb2ea4`), nicht in diesem
Repo — darum sind sie hier **gemeldet** und nicht behoben.

**1. `ngc-db install` stirbt auf einem frischen Debian am ersten Schritt.**
```
▶ NextGenCloud: MariaDB with Galera for db-1.ngc.mein.lan
chown: invalid group: 'root:mysql'
```
`prepare()` ruft `ensure_dir "$NGC_TLS" root:mysql 0710`, und
`install_ngc_db()` ruft `prepare` **vor** `ensure_packages` — die Gruppe
`mysql` gibt es da noch nicht. Drei Anweisungen weiter steht „MariaDB has to be
there before the keys: they belong to its user": die Reihenfolge war für die
Schlüssel gesehen und für die Ordner übergangen.

**2. Die Verschlüsselung im Ruhezustand lässt MariaDB nicht starten.**
```
[ERROR] mariadbd: Cannot decrypt /etc/ngc/db-keys/keys.enc. Wrong key?
[ERROR] Plugin 'file_key_management' init function returned error.
```
`ensure_encryption_keys()` verschlüsselt mit
`openssl enc -aes-256-cbc -pbkdf2 -md sha256 -salt`. MariaDBs
`file_key_management` kann das nicht aufmachen; es rechnet mit der alten
Ableitung (`EVP_BytesToKey`, SHA-1). **Gegenprobe im selben Lauf**, mariadbd
direkt gestartet und ohne Galera im Weg, dieselbe Nutzlast und dieselbe
Passphrase:

| Variante | Ableitung | Antwort des Plugins |
|---|---|---|
| a | `-pbkdf2 -md sha256 -salt` (so schreibt es ngc-db) | `Cannot decrypt … Wrong key?` |
| b | `-md sha1` | keine Entschlüsselungszeile — angenommen |
| c | `-md sha1`, kurze Passphrase | angenommen |

Es ist also die Ableitung und nicht die Länge der Passphrase.

**3. `socket.ssl_cipher=TLS_AES_256_GCM_SHA384` lässt Galera nicht laden.**
```
[ERROR] WSREP: Error setting SSL cipher list to 'TLS_AES_256_GCM_SHA384':
        'error:0A0000B9:SSL routines::no cipher match'
[ERROR] WSREP: Failed to load provider
[ERROR] Aborting
```
Das ist ein TLS-1.3-**Chiffrensatz**; Galeras `socket.ssl_cipher` nimmt eine
OpenSSL-**Chiffrenliste** (Namen bis TLS 1.2). **Gegenprobe:** dieselbe
Konfiguration ohne genau diese eine Option — MariaDB läuft, und
`wsrep_local_state_comment` meldet **Synced**. Mit galera-4 26.4.23 und
OpenSSL 3.5.7 kommt der erste Datenbankknoten sonst gar nicht hoch.

**4. `start_cluster` nimmt auf einem frischen Debian nicht den
Bootstrap-Weg.** Das Paket lässt MariaDB nach der Installation **laufen**,
also ist `mariadb_running` wahr und der erste Zweig greift
(`systemctl restart mariadb`) statt `galera_new_cluster`. Der erste Start des
Clusters findet so nie über den dafür gebauten Weg statt.

**5. Niemand spielt die Migrationen ein — und der Weg dahin ist ein Kreis.**
Nach `ngc-api install` dreht die API in einer Startschleife:
```
ERROR die API kann nicht starten error="die Vertrauensliste dieser Anlage lässt
sich nicht lesen; zuerst /etc/ngc/trust-anchor.json prüfen, dann die Datenbank"
```
`ngc-api migrate` mit dem Konto aus `api.json` wird abgelehnt:
```
Error 1142 (42000): CREATE command denied to user 'ngc'@'api-1.ngc.mein.lan'
for table `ngc`.`ngc_schema`
```
Der einzige dokumentierte Einspieler ist der **Controller mit der Rolle `db`**
(`docs/migrationen-wer-spielt-sie-ein.md`, Weg (a)) — und der kann nicht
gekoppelt werden, bevor die API läuft, weil der Kopplungscode aus der
Oberfläche kommt. Ich habe den Kreis von Hand aufgeschnitten: `ngc-api migrate`
mit dem Konto `ngc_migrate`, Passwort über die Standardeingabe, nie in einem
Aufruf. Danach: **„1 Migration eingespielt."**, API `active` und horchend.

Zwei benannte Vorarbeiten tragen darum jede Zahl dieses Laufs: `mariadb-server`
war vorinstalliert (gegen 1), das Schlüsselpaar wurde mit `-md sha1` neu
geschrieben und `wsrep_provider_options` über ein Drop-in
`99-erstlauf-galera.cnf` gesetzt (gegen 2 und 3), und die Migration lief von
Hand (gegen 5). Auf einer Maschine, auf der das niemand tut, **startet die
Installation nicht**.

---

## Was danach wirklich steht

Gemessen am 24.10.2026 auf der verteilten Anlage:

- **Das echte Release, mit NGCs echtem Schlüssel.** `ngc-api.sh` meldet „The
  signature of the program files is good." gegen das veröffentlichte `v0.2.1`;
  dessen `release-key.pem` ist Byte für Byte der `NGC_RELEASE_KEY` der
  ulh-Skripte. **Nichts in diesem Lauf ersetzt einen Sicherheitsriegel** —
  `tools/erstlauf-maschine.sh` muss dafür eine Test-Wurzel einsetzen.
- **Die Fassungen stimmen.** `ngc-api 0.2.1 (contract 1)`, `ngc-store 0.2.1` —
  nicht `0.0.0-dev`. Hürde 2 vom Oktober ist im Release weg.
- **Hürde 1 ist weg.** `ngc-api.service` hat `ProtectSystem=strict` **und**
  `ReadWritePaths=/var/lib/ngc /etc/ngc` — `/etc/ngc` ist dabei.
- **Die API lebt ohne Datenbank auf ihrer Maschine.** Auf api-1 ist
  `mariadb-server` nicht installiert, `api.json` nennt `db-1.ngc.mein.lan:3306`,
  und es steht eine Verbindung `10.2.3.210 → 10.2.3.212:3306`.
- **Der Laden wird über Netz geholt, und das Zertifikat wird geprüft.** api-1
  holt `https://store.ngc.mein.lan/catalog.json` mit `http=200 verify=0`; eine
  Maschine, die die CA des Ladens nie bekommen hat (db-1), wird mit `http=000`
  abgewiesen. Ohne diese Gegenprobe sagt die erste Zahl nichts.
- **Der Durchstich steht.** `https://ngc.mein.lan/api/v1/setup` antwortet durch
  Apache, die API und MariaDB mit dem Einrichtungsangebot: Anlagenkennung,
  Knoten `api-1` mit `wss://api-1.ngc.mein.lan:7443/tunnel` und seinem
  Fingerabdruck, der echte `releaseKey`, und als Store
  `https://store.ngc.mein.lan` mit unserem Schlüssel.

## Nach der Ersteinrichtung — was dabei noch aufgefallen ist

Thomas hat am 24.10.2026 die Ersteinrichtung im Browser gemacht, svc-1
gekoppelt und api-2 beitreten lassen. Damit sind Station 3 und D3 gemessen
(der Lauf geht von 9 auf 6 unmessbar). Dabei kamen vier weitere Dinge heraus.

**6. Der Anker außerhalb der Datenbank liegt auf den API-Servern gar nicht.**
`advanceAnchor` in `apps/server-api/classes/state/trust.go` schreibt
ausdrücklich nur **über eine Datei, die schon da ist** („Only over a file that
is already there"), und auf einem API-Knoten legt sie niemand an — `ngc-api.sh`
tut es nicht. Gemessen: auf api-1 und api-2 gibt es `/etc/ngc/trust-anchor.json`
nicht, während die Vertrauensliste schon auf Fassung 3 steht. Das Versprechen
„der Anker liegt außerhalb der Datenbank" hält auf einer frischen Anlage also
für die API niemand. Der Kommentar an der Stelle beschreibt genau diese Lage
selbst: ein Anker, den nur eine Kopplung setzt, „protects nobody".

**7. Der Anker des Controllers bleibt stehen.** svc-1 hat die Datei — mit
`"seq": 1`, während die Liste auf 3 steht. Gesetzt wurde sie bei der Kopplung
und seither nicht bewegt.

**8. Ein selbst betriebener Laden muss die Kennung `global` tragen.**
`ngc-api.sh` nimmt `store_url` und `store_key` und trägt den Laden unter der
Kennung **`global`** ein (Zeile `store.global` in `ngc_blob`). `ngc-store
catalog` nimmt die Kennung dagegen als Angabe. Wer dort etwas anderes schreibt
— wir zuerst `ngc-sechs` —, bekommt den Katalog abgelehnt, und die Anlage sagt
nur: „Der Katalog ist fehlerhaft oder gehört zu einem anderen Store."
(`store.VerifyCatalog`, `c.Store != st.ID`). Welche der beiden Kennungen nicht
passt, steht nirgends.

**9. Umbenennen lässt `ngc-store catalog` mit einer falschen Meldung stehen.**
Nach dem Wechsel der Kennung antwortet es „the catalog needs a higher sequence
number than the last one" — bei **jeder** Nummer. `lastSeq` prüft den Katalog,
der schon im Ordner liegt, mit der **neuen** Kennung; das schlägt fehl, und der
Fehler kommt als `ErrSeq` heraus. Abhilfe: die alte `catalog.json` löschen,
dann neu unterschreiben.

Außerdem gemessen, ohne Wertung: der Tunnel des Controllers endet alle rund
420 Sekunden mit `link: the API cannot be reached` und steht sofort wieder —
und die Katalogrunde schlägt **erst eine Stunde nach dem Start** zum ersten Mal
(`server.go`: „The first beat is an hour after the start and not at the start",
mit Begründung).

## Was der Erstlauf verteilt NICHT durchläuft, und warum

**Erster Lauf, vor der Ersteinrichtung:** 0 rot, **9 unmessbar**, Ausstieg 2.
Durchgelaufen waren Station 1, Station 2 (API), D1, D2 und Station 7 für den
Datenträger.

**Stand nach der Ersteinrichtung** (Thomas hat den Einmal-Code eingegeben,
fünf Controller gekoppelt und alle aufgenommen): **0 rot, 4 unmessbar,
Ausstieg 2**, keine `ok`-Zeile — **elf Stationen melden ok**:

| Station | was sie misst |
|---|---|
| 1 | das veröffentlichte Release unter NGCs eigenem Schlüssel |
| 2 | ngc-api 0.2.1 aktiv mit stehendem MainPID |
| 2 (Controller) | ngc-controller 0.2.1 auf svc-1 |
| 3 | svc-1 gekoppelt: `controller.json`, Marke, Dienst hält |
| 4 | Vertrauensliste **Fassung 6 mit sieben Knoten** |
| 6 | **Katalog da** — 202 Bytes gespeichert, `{"checked":…}` |
| D1 | Datenbank auf einer anderen Maschine, Verbindung steht |
| D2 | Laden auf eigener Maschine, Zertifikat geprüft, mit Gegenprobe |
| D3 | zweiter API-Server aktiv |
| 7 (×2) | Datenträger auf svc-1 und svc-2 kann verschlüsseln |

Vier bleiben, und keine davon ist eine Lücke im Aufbau:

- **Station 5 Systemrechte** — ein Klick eines angemeldeten Admins auf der
  Rechteseite; auf keiner Maschine liegt eine Datei, die ihn bezeugt.
- **Station 7 (die App selbst)** und **Station 8** — der Katalog ist da, aber
  **leer**: `ngc-store catalog` über einem leeren Ordner unterschreibt eine
  leere Liste. Es fehlt ein gepacktes App-Paket, nicht ein Stück Anlage.
- **D2 (Laden weg) und der Rückfall-Schutz** — jetzt endlich messbar, kosten
  aber je eine Stunde Wanduhr, weil die Ladenrunde stündlich schlägt. Das ist
  ein eigener Lauf, keine Zeile in diesem.

Vor der Ersteinrichtung hing alles Übrige an **einem** Punkt, und der ist
keine Lücke im Aufbau, sondern ein Mensch: die **Ersteinrichtung geschieht im
Browser**. `GET`/`POST`
auf `/api/v1/setup`, beide Seiten weisen mit einem MAC nach, dass sie den
Einmal-Code aus `/var/lib/ngc/setup-code` haben
(`apps/server-api/classes/setup/setup.go`). Vorher gibt es keinen Admin und
keine Vertrauensliste — die API sagt es selbst: `catalogs could not be fetched
error="state: no trust list yet"`. Daran hängen:

- **Station 3/4/5** Koppeln, Aufnehmen, Systemrechte — jeder Schritt ein Klick
  eines angemeldeten Admins.
- **Station 6** Katalog — braucht einen aufgenommenen Controller.
- **Station 7 (die App selbst)** und **Station 8** — brauchen 6 und einen
  zweiten Benutzer.
- **D3, der zweite API-Server** — `join_code` wird von einem angemeldeten Admin
  ausgestellt.
- **D2 (Laden weg)** — ob ein unerreichbarer Laden den letzten guten Katalog
  stehen lässt, sieht man erst, wenn je ein Katalog ankam.

Ein Skript kann dieser Admin nicht sein, und ein Lauf, der es vorgäbe, würde
seine eigene Vorrichtung messen.

**Der nächste Schritt gehört darum dir:** den hosts-Block oben setzen,
`https://ngc.mein.lan` öffnen, den Code aus
`/var/lib/ngc/setup-code` auf api-1 (`0600`, nur für root) eingeben und den
ersten Admin anlegen. Danach laufen `api2`, `web` erneut und die Stationen 3
bis 8.
