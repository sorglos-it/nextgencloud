# Fremdcode in NGC – Bestand, Preis, Vorschlag

**Gemessen am 18.10.2026** im Arbeitsbaum `D:\claude\ngc-fremdcode` (lokale Platte D:), Stand
`origin/main` = `6f1b90d`. Fehlende Dateien gegen HEAD: **0** (im selben Lauf geprüft).

Diese Datei entscheidet nichts. Sie legt die Zahlen hin, damit Thomas entscheiden kann.

## Die Kurzfassung in fünf Sätzen

- Die Hausregel **„Fremdcode ins Repo kopieren und selbst pflegen" ist in NGC schon erfüllt**:
  aller Fremdcode liegt als Quelltext im Repo (`vendor/`, 976 Dateien, 19,6 MB), nicht als
  Paketverweis.
- Der Bau holt **nichts** aus dem Netz: kein npm, kein `go get` zur Bauzeit, das Container-Abbild
  hängt an einem Digest.
- **Zwei Bauten derselben Quelle sind bitgleich** – gemessen, auch mit leerem Zwischenspeicher.
- **Keine bekannte Lücke betrifft NGC.** Eine Meldung auf Modulebene (`x/crypto/openpgp`) trifft
  Code, der **nicht einmal auf der Platte liegt**.
- Die echten Lücken sind **nicht im Code, sondern im Verfahren**: niemand und nichts merkt, wenn
  draußen eine neue Fassung erscheint, und der dokumentierte Prüfbefehl sieht nur die Hälfte.

## 1. Die vollständige Liste

### Direkt ausgesucht (7 Module, go.mod erster Block)

| Modul | Fassung | Größe | Dateien | Fundstelle (eine, ohne Test) |
|---|---|---|---|---|
| `golang.org/x/crypto` | v0.57.0 | 518 KB | 56 | [shared/proof/proof.go:19](shared/proof/proof.go) (argon2), [apps/server-controller/classes/node/run.go:47](apps/server-controller/classes/node/run.go) (curve25519) |
| `github.com/flynn/noise` | v1.1.0 | 1 935 KB | 8 | [shared/noisep/noisep.go:22](shared/noisep/noisep.go), dazu 12 weitere Nicht-Test-Stellen |
| `github.com/ncruces/go-sqlite3` | v0.35.5 | 337 KB | 93 | [apps/server-db/classes/sqlite/sqlite.go:18](apps/server-db/classes/sqlite/sqlite.go), Zeile 20 bindet `vfs/xts` ein |
| `github.com/coder/websocket` | v1.8.15 | 121 KB | 30 | [apps/server-api/classes/link/link.go:22](apps/server-api/classes/link/link.go), [apps/server-controller/classes/link/link.go:43](apps/server-controller/classes/link/link.go) |
| `github.com/go-sql-driver/mysql` | v1.10.1 | 233 KB | 26 | [apps/server-db/classes/mariadb/mariadb.go:14](apps/server-db/classes/mariadb/mariadb.go) |
| `github.com/pquerna/otp` | v1.5.0 | 35 KB | 9 | [apps/server-api/classes/login/totp.go:6](apps/server-api/classes/login/totp.go) |
| `github.com/go-webauthn/webauthn` | v0.18.2 | 724 KB | 80 | **nur** [tools/modules.go:17](tools/modules.go) – kein Programm benutzt es |

### Mitgezogen (14 Module, go.mod `// indirect`)

| Modul | Fassung | Größe | Gebraucht von |
|---|---|---|---|
| `golang.org/x/sys` | v0.48.0 | **8 172 KB** | go-sqlite3, x/crypto |
| `github.com/ncruces/go-sqlite3-wasm/v6` | v6.2.35304 | **4 539 KB** | go-sqlite3 (SQLite selbst, als WebAssembly) |
| `github.com/go-webauthn/x` | v0.3.1 | 1 088 KB | webauthn |
| `github.com/google/go-tpm` | v0.9.8 | 535 KB | webauthn |
| `github.com/fxamacker/cbor/v2` | v2.9.4 | 318 KB | webauthn |
| `github.com/tinylib/msgp` | v1.6.4 | 190 KB | go-webauthn/x |
| `filippo.io/edwards25519` | v1.2.0 | 113 KB | mysql |
| `github.com/go-viper/mapstructure/v2` | v2.5.0 | 91 KB | go-webauthn/x |
| `github.com/golang-jwt/jwt/v5` | v5.3.1 | 90 KB | go-webauthn/x |
| `github.com/boombuler/barcode` | v1.0.1-0.20190219062509 | 49 KB | otp (QR-Code) |
| `github.com/google/uuid` | v1.6.0 | 41 KB | webauthn |
| `github.com/philhofer/fwd` | v1.2.0 | 27 KB | msgp |
| `github.com/x448/float16` | v0.8.4 | 18 KB | cbor |
| `github.com/ncruces/julianday` | v1.0.0 | 5 KB | go-sqlite3 |

**Summe: 21 Module, 19,6 MB, 976 Dateien.** Die zwei größten (x/sys, SQLite-WASM) sind
**68 %** davon, und **keines** davon hat jemand ausgesucht.

### Nicht in go.mod, aber auch Fremdcode

| Stück | Fassung | Liegt | Herkunft belegt? |
|---|---|---|---|
| `@noble/hashes` (JS, Argon2id im Browser) | 2.4.0 | `apps/web/public/assets/vendor/noble-hashes/` – **nur auf dem Zweig `web`/`web-alles`**, nicht auf `main` | **ja, vorbildlich**: `SOURCE.md` nennt Quelle, Datum, Grund, SHA-256 je Datei, den Registry-Hash und den Weg, es nachzuprüfen |
| `debian:13-slim` (Abbild der Terminal-App) | Digest `sha256:a29215f6…` | [apps/server-controller/templates/terminal/Containerfile:30](apps/server-controller/templates/terminal/Containerfile) | ja, festgenagelt; `tools/pin-base.sh` ist der einzige Weg, die Zeile zu ändern |
| `@playwright/test` (nur Tests) | 1.55.1 | `apps/web/tests/package.json` – **per npm installiert, nicht im Repo** | nein → siehe Punkt 6 |

## 2. Je Modul der Preis in beide Richtungen

Die Frage „was kostet es, es **nicht** selbst zu haben" hat in NGC eine ungewöhnliche Antwort:
**wenig**, weil der Code schon im Repo liegt. Eine neue Fassung kommt nur dadurch herein, dass
jemand `go get` tippt und committet – kein Paketmanager, kein Bauschritt, kein Netz. Der Preis
liegt deshalb nicht beim Einspielen, sondern beim **Merken**.

| Modul | Was es macht | Selbst haben kostet | Nicht selbst haben kostet |
|---|---|---|---|
| `x/crypto` (argon2, curve25519, chacha20poly1305, blake2) | Passwortschlüssel und die Bausteine für Noise | Argon2id und Curve25519 selbst schreiben – **verboten** nach `code.md` („nichts selbst erfinden") | Fast nichts: liegt im Repo, Google pflegt es, 32 Meldungen in der Lücken-Datenbank sind alle entweder behoben oder betreffen Pakete, die hier nicht liegen |
| `flynn/noise` | Noise_KK für Sitzung, Tunnel, Ende zu Ende | Zustandsmaschine eines Kryptoprotokolls – hoch, und ein Fehler darin ist still | **Das Ernste**: letzte Veröffentlichung **02.02.2024**. Seit 2,7 Jahren nichts. Bei einer Lücke gibt es niemanden, der sie behebt – dann ist NGC der Pfleger, ob gewollt oder nicht |
| `ncruces/go-sqlite3` + `-wasm` + `julianday` | SQLite ohne cgo, dazu AES-256-XTS auf der Datei | SQLite nachbauen: **unmöglich**. Alternative wäre cgo, und das bricht den Bau ohne C-Übersetzer | Eine Fassung hinter (v0.35.6, 23.09.2026). SQLite-Lücken sind öffentlich und gut gepflegt |
| `coder/websocket` | WebSocket für Sitzung und Tunnel | Mittel: RFC 6455 ist überschaubar, aber Rahmenlängen und Maskierung sind genau die Stelle, an der Fehler teuer sind | Gering, aktuell (v1.8.15, 15.06.2026), keine Einträge in der Lücken-Datenbank |
| `go-sql-driver/mysql` + `edwards25519` | MariaDB | Hoch: ein Netzprotokoll mit Anmeldeverfahren | Gering, aktuell (02.09.2026) |
| `pquerna/otp` + `boombuler/barcode` | TOTP nach RFC 6238, QR-Code zur Einrichtung | **Niedrig.** TOTP ist HMAC-SHA1 plus eine Modulo-Rechnung, 30 Zeilen. QR-Code ist mehr Arbeit, aber nicht sicherheitskritisch | Gering. `barcode` ist aber eine **Pseudofassung von 2019** (`v1.0.1-0.20190219062509`) – ein Zwischenstand, kein Release, und seit 22.07.2025 gibt es v1.1.0 |
| `go-webauthn/*` + `go-tpm` + `cbor` + `float16` + `uuid` + `jwt` + `mapstructure` + `msgp` + `fwd` | Passkeys (WebAuthn/FIDO2) | Hoch: Attestierung, Zertifikatsketten, CBOR, TPM-Nachweise | **Dies ist der einzige Posten mit echtem Preis für nichts**: 10 Module, **3,1 MB**, 16 % des Fremdcodes – und **kein Programm ruft eine Zeile davon auf.** Dazu: weil kein Programm sie einbindet, sieht `govulncheck ./...` sie nicht (Punkt 4) |
| `x/sys` | Dateisperren für SQLite, CPU-Erkennung für blake2b | Hoch und sinnlos: es sind Systemaufrufe von Linux | Gering inhaltlich, aber es ist **8,2 MB, 42 % des Fremdcodes**, für ein paar Funktionen |
| `@noble/hashes` (JS) | Argon2id im Browser | Argon2id in JavaScript selbst schreiben – nicht zu verantworten | **Niedrig, und das ist belegt**: Fassung 2.4.0 ist heute die neueste auf npm (veröffentlicht 27.08.2026) |

### Wer darf eine neue Fassung einspielen?

Gemessen, nicht angenommen:

- **Für das Container-Abbild: eine Regel, und sie ist aufgeschrieben.**
  `tools/pin-base.sh` (Kopfkommentar, Entscheidung von Thomas am 15.10.2026): nur wer ein Release
  **unterschreiben** darf, vor **jedem** Release, als eigener Commit, nie automatisch.
- **Für die 21 Go-Module: keine Regel.** `THIRD-PARTY.md` nennt nur das *Wie*
  („`go get` … `go mod vendor` … `govulncheck` … Eintrag anpassen"), nicht das *Wer* und nicht das
  *Wann*. Jeder Strang, der schreiben darf, kann eine Kryptobibliothek anheben.
- **Für den JS-Teil: ein Weg, aber ebenfalls kein Wer.** `SOURCE.md` beschreibt das Aktualisieren
  genau, bis hin zu den Testfällen, die darüber entscheiden.

Das ist dieselbe Frage wie in **Punkt 39** – dort für das Abbild gestellt und beantwortet, hier
für `flynn/noise` und `x/crypto` offen. Und bei `flynn/noise` ist sie schärfer: dieses Modul
sichert den Tunnel und das Ende zu Ende.

### Wie merkt man eine Lücke überhaupt?

Die schwächste Stelle, gemessen:

- **`.github/workflows/` gibt es nicht.** Nichts läuft von selbst, nie.
- **`tools/check.sh`** (das Tor jedes Commits) ruft `govulncheck` **nicht** auf – geprüft, kein
  Treffer in der Datei.
- **`tools/release-gate.sh:100-104`** ruft es auf, und zwar richtig laut: fehlt das Werkzeug,
  zählt das als Fehler. Nur: dieses Skript ist **nie durchgelaufen** (sagt sein eigener Kopf),
  und ein Release gibt es noch nicht.
- Also: **eine Lücke merkt heute niemand.** Sie wird bemerkt, wenn ein Mensch an
  `govulncheck ./...` denkt.

## 3. Die Fassungen gegen das, was es heute gibt

**Quelle: `proxy.golang.org/<modul>/@latest`, abgefragt am 18.10.2026.** Nicht aus dem Gedächtnis.

Dreizehn Module sind aktuell. Acht sind hinter:

| Modul | Hier | Draußen | Draußen vom | Art |
|---|---|---|---|---|
| `golang.org/x/crypto` | v0.57.0 | **v0.58.0** | 09.10.2026 | Minor |
| `golang.org/x/sys` | v0.48.0 | **v0.49.0** | 06.10.2026 | Minor |
| `ncruces/go-sqlite3` | v0.35.5 | **v0.35.6** | 23.09.2026 | Patch |
| `ncruces/go-sqlite3-wasm/v6` | v6.2.35304 | **v6.3.35401** | 16.10.2026 | Minor |
| `fxamacker/cbor/v2` | v2.9.4 | **v2.9.6** | 06.10.2026 | Patch |
| `tinylib/msgp` | v1.6.4 | **v1.6.5** | 30.09.2026 | Patch |
| `philhofer/fwd` | v1.2.0 | **v1.2.1** | 16.10.2026 | Patch |
| `boombuler/barcode` | v1.0.1-0.20190219062509 | **v1.1.0** | 22.07.2025 | Pseudofassung → Release |

Nichts davon ist ein Sprung über eine Hauptfassung. Fünf der acht hängen am Passkey-Strang, den
kein Programm benutzt.

### Hat eine davon eine bekannte Lücke?

**`govulncheck -show verbose ./...`, ausgeführt am 18.10.2026, Ausstiegscode 0:**

```
Your code is affected by 0 vulnerabilities.
… 0 vulnerabilities in packages you import and 1 vulnerability in modules you
require, but your code doesn't appear to call these
```

Der eine Treffer:

- **GO-2026-5932** – `golang.org/x/crypto/openpgp` ist unpflegt und unsicher, **„Fixed in: N/A"**.
- **Er trifft NGC nicht, und das ist stärker als „wird nicht aufgerufen":** `openpgp` liegt
  **nicht in `vendor/`**. Gemessen – `vendor/golang.org/x/crypto/` enthält `argon2`, `blake2b`,
  `blake2s`, `chacha20`, `chacha20poly1305`, `curve25519`, `internal`, `ocsp`, `xts`. Das Paket
  ist nicht auf der Platte und kann in keinen Bau geraten.

Zusatzprüfung gegen `vuln.go.dev/index/modules.json` (dieselbe Datenbank, Modulebene): die drei
neuesten `x/crypto`-Meldungen (GO-2026-6303, -6354, -6355) betreffen alle `x/crypto/ssh` und sind
in v0.55.0 bzw. v0.56.0 **behoben** – also vor v0.57.0, und `ssh` liegt hier ebenfalls nicht.

**Also: keine bekannte, NGC betreffende Lücke am 18.10.2026.** Das gilt für diesen Tag und diese
Datenbank, nicht für nächste Woche.

## 4. Ist der Bau reproduzierbar?

### Sind die Fassungen festgenagelt?

- `go.sum`: **ja, vollständig.** Für jedes der 21 Module in `go.mod` steht sowohl der Hash des
  Archivs als auch der der `go.mod` darin – mit einem Skript geprüft, Ergebnis: „ohne
  vollständigen go.sum-Eintrag: keine".
- `go mod verify` → **`all modules verified`**.
- Aber der wichtigere Punkt: **weil `vendor/` existiert, baut Go aus `vendor/` und liest `go.sum`
  dabei nicht.** Was wirklich gebaut wird, sind die Bytes im Git. Das ist stärker als ein Hash,
  solange niemand `vendor/` von Hand verändert.
- **Niemand hat.** Gegenprobe: in einer Kopie `vendor/` gelöscht und mit `go mod vendor` neu
  geholt (das prüft gegen `go.sum` und die Prüfsummen-Datenbank). `diff -rq` gegen das Repo:
  **0 Unterschiede.** Der Satz „Eigene Änderungen an übernommenem Code: keine" aus
  `THIRD-PARTY.md` ist damit gemessen, nicht behauptet.

### Baut derselbe Stand zweimal dasselbe?

Drei Programme, `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 -buildvcs=false -trimpath`, `git
hash-object` über die Ergebnisse:

| Programm | Größe | Bau a | Bau b | |
|---|---|---|---|---|
| `apps/cli` | 7 067 232 B | `d5803996bce6` | `d5803996bce6` | **gleich** |
| `apps/server-api` | 24 631 353 B | `f6afd9c235ac` | `f6afd9c235ac` | **gleich** |
| `apps/server-controller` | 22 922 663 B | `ba82081b0ca1` | `ba82081b0ca1` | **gleich** |

Und weil ein zweiter Bau aus demselben Zwischenspeicher nichts beweist, ein dritter mit **leerem
`GOCACHE`**: `apps/cli` wieder `d5803996bce63c48b514cb1968c5eec404ac7ef0` – identisch.

**Gegensonde, damit die Messung Zähne hat** (eine Messung, die nicht auch rot werden kann, misst
nichts): dasselbe Programm **ohne** `-trimpath`, einmal in diesem Baum und einmal in einer Kopie
unter einem anderen Pfad:

```
ohne -trimpath, Pfad D:\claude\ngc-repro-kopie  -> fae54d3de460…
ohne -trimpath, Pfad D:\claude\ngc-fremdcode    -> 1cfba7e5509a…   VERSCHIEDEN
```

Der Pfad schlägt also durch, und `-trimpath` ist genau das, was ihn heraushält. Die Gleichheit
oben ist damit ein Ergebnis und kein Zufall.

- **`docs/release-weg.md:70` und `:139` nennen `-trimpath` und frühere Prüfsummen.** Mein Lauf
  bestätigt das unabhängig und fügt den kalten Zwischenspeicher dazu.
- **War eine Lücke, ist behoben** (18.10.2026, in diesem Zweig): `tools/release-gate.sh` baute
  `ngc-store` **ohne** `-trimpath` – die einzige Zeile im Repo, die ein Programm baut und es nicht
  reproduzierbar baut. Jetzt steht der Schalter dort, mit den Zahlen im Kommentar. Die Zeile ist
  danach einmal gelaufen, Ausstiegscode 0.
- **Gültigkeitsbereich:** eine Werkzeugkette (go1.27.2), eine Maschine, ein Laufwerk (D:).
  Reproduzierbar *zwischen* zwei Rechnern ist damit **nicht** gemessen.

## 5. Vorschlag je Modul – zu entscheiden von Thomas

Die Hausregel sagt „ins Repo kopieren und selbst pflegen". **Der erste Teil ist überall erfüllt.**
Zu entscheiden ist also nur der zweite: wo hört ein Paketverweis auf und fängt echte Pflege an.

Nach meiner Lesart ist die Grenze der Regel nicht die **Größe**, sondern die Frage: *kann NGC es
reparieren, wenn draußen niemand mehr repariert?* Danach sortiert:

| Modul | Vorschlag | Preis dieses Vorschlags |
|---|---|---|
| **`go-webauthn/*` samt 9 Mitgezogenen** | **Rausnehmen**, bis Passkeys wirklich gebaut werden. Die Zeile in `tools/modules.go` fällt weg, `go mod tidy`, `go mod vendor`. | Eine Stunde, minus 3,1 MB und 10 Module; wenn Passkeys kommen, derselbe Befehl zurück. Ohne das bleibt Fremdcode im Repo, den niemand aufruft und den der dokumentierte Prüfbefehl nicht sieht. |
| **`flynn/noise`** | **Pflegeerklärung schreiben** (nicht neu schreiben): festhalten, dass es seit 02.02.2024 still steht, dass NGC bei einer Lücke selbst behebt, und **wer** es dann darf – wie `pin-base.sh` es für das Abbild tut. | Eine halbe Seite. Ohne das ist der Tunnel-Riegel an einem Projekt aufgehängt, dessen Pflegezustand niemand in NGC schriftlich hat. |
| **`pquerna/otp` + `boombuler/barcode`** | **Kandidat zum Selbstschreiben.** TOTP sind 30 Zeilen HMAC-SHA1. Dann fällt auch die Pseudofassung von 2019 weg. | Ein Tag mit Vektoren aus RFC 6238 und Tests. Für den QR-Code braucht es dann einen Ersatz oder `barcode` bleibt – dann wenigstens auf v1.1.0 heben. |
| **`x/crypto`** | **Bleibt. Auf v0.58.0 heben.** | Ein Befehl. Argon2id selbst schreiben verstößt gegen `code.md`. |
| **`x/sys`** | **Bleibt, trotz 8,2 MB.** Hier ist die Grenze der Regel: das sind Linux-Systemaufrufe, nachschreiben ist Arbeit ohne Gewinn. Auf v0.49.0 heben. | Ein Befehl. Die 8,2 MB bleiben; sie kosten Plattenplatz, keine Pflege. |
| **`ncruces/go-sqlite3` + `-wasm`** | **Bleibt. Auf v0.35.6 / v6.3.35401 heben**, danach die Datenbank-Fälle laufen lassen. | Ein Befehl plus ein Testlauf. SQLite nachbauen geht nicht, und cgo bricht den Bau ohne C-Übersetzer. |
| **`coder/websocket`, `go-sql-driver/mysql`, `edwards25519`** | **Bleibt, aktuell, nichts zu tun.** | – |
| **`@noble/hashes` (JS)** | **Bleibt wie es ist** – und seine `SOURCE.md` ist die Vorlage, nach der die Go-Seite dokumentiert sein sollte. Zusätzlich: eine Zeile mit Fassung und Lizenz in `THIRD-PARTY.md`. | Zehn Minuten. |
| **`debian:13-slim`** | **Bleibt, erledigt.** Digest festgenagelt, `pin-base.sh` ist der Weg, die Pflicht steht geschrieben (Punkt 39). | – |
| **`@playwright/test`** | **Entscheidung nötig** – siehe Punkt 6. | – |

**Zwei Verfahrenspunkte, die mehr wert sind als jeder einzelne Modulwechsel. Der erste ist
erledigt, der zweite ist deine Entscheidung:**

1. **Erledigt am 18.10.2026** (in diesem Zweig, keine Entscheidung nötig – es war ein Fehler, keine
   Wahl): `govulncheck -tags modules ./...` statt `govulncheck ./...`, an allen drei Stellen, die
   den Befehl nennen – `tools/release-gate.sh`, `THIRD-PARTY.md`, `CLAUDE.md`. Je Stelle stehen die
   gemessenen Zahlen dabei (12 gegen 22 Module), damit niemand den Schalter für Beiwerk hält und
   wieder wegnimmt. Der Befehl ist danach wortwörtlich gelaufen: **0 erreichbare Lücken,
   Ausstiegscode 0.** Mit dem Rausnehmen des Passkey-Strangs wird der Schalter wirkungslos, aber
   nicht falsch – der Kommentar sagt das.
2. **Offen, und es ist deine Entscheidung: wer darf eine Kryptobibliothek anheben?** Nach dem
   Muster von `tools/pin-base.sh`, das für das Container-Abbild genau diese Frage beantwortet
   (Punkt 39). Heute darf es jeder, der schreiben darf – und `flynn/noise` sichert den Tunnel.

## 6. Wurde die Regel irgendwo still verletzt?

Gesucht wurde an diesen Orten – die Liste gehört zur Antwort, denn eine Abwesenheit belegt man mit
Orten, nicht mit Suchern:

1. Lizenzköpfe und `SPDX` außerhalb `vendor/` → Treffer nur in `LICENSE`, `shared/LICENSE`,
   `README.md` und zwei Plandokumenten. **Kein fremder Lizenzkopf in eigenem Code.**
2. Hinweise auf Übernahme (`adapted from`, `based on`, `taken from`, `copied from`,
   `derived from`, `ported from`, `übernommen von`) über `apps/`, `shared/`, `tools/` → **44
   Treffer, alle falsch positiv.** Jeder beschreibt eine Rechnung im eigenen Code („derived from
   the private key", „taken from the first two bytes of its SHA-256"), keiner eine Herkunft.
3. `vendor/`-Ordner und `.min.`-Dateien auf allen Zweigen → genau einer außerhalb von `vendor/`:
   `noble-hashes`, **mit `SOURCE.md`**.
4. `package.json`, `package-lock.json`, `yarn.lock` → drei Dateien, siehe unten.
5. `vendor/` gegen `go mod vendor` → **0 Unterschiede** (Punkt 4): kein Fremdcode von Hand
   geändert.

**Ergebnis: ein kopiertes Stück ohne Herkunftsangabe gibt es nicht.** Die eine Handkopie im
Projekt ist besser dokumentiert als die Module.

Drei kleinere Funde, keiner davon ein stiller Fremdcode-Einbau:

- **`@playwright/test` 1.55.1 kommt per npm, nicht aus dem Repo.** Das ist das einzige Stück
  Fremdcode in NGC, auf das die Hausregel **nicht** angewandt wurde. Es wird nicht ausgeliefert
  (nur Tests), aber es läuft auf dieser Maschine und lädt zusätzlich Browser nach
  (`playwright install --with-deps`). Draußen ist heute **1.64.1** – neun Minor-Fassungen weiter.
  *Zu entscheiden bleibt:* gilt die Regel auch für Werkzeug, das nur testet? Ich schlage vor: nein.
  **Benannt ist es jetzt** – `THIRD-PARTY.md` hat am 18.10.2026 einen Abschnitt „Nur zum Testen,
  und nicht im Repo" bekommen, mit Fassung, Begründung und dem Abstand nach draußen. Eine benannte
  Ausnahme ist keine stille mehr.
- **`THIRD-PARTY.md` nannte den JS-Teil nur im Vorwort**, ohne Zeile in einer der beiden Tabellen –
  also ohne Fassung und ohne Lizenz. **Behoben am 18.10.2026:** eigener Abschnitt „JavaScript, von
  Hand kopiert" mit `@noble/hashes` 2.4.0, MIT, Übernahmedatum und dem Verweis auf `SOURCE.md`.
- **`THIRD-PARTY.md` nannte `x/crypto/ocsp` nicht**, obwohl es in `vendor/` liegt. **Behoben am
  18.10.2026**, in derselben Zeile wie `xts`. Es kommt über `go-webauthn/x/revoke` herein und
  fällt mit dem Passkey-Strang weg.

## Was diese Messung nicht sagt

- **Lizenzen sind nicht neu geprüft.** Die Einschätzungen in `THIRD-PARTY.md` (darunter die
  MPL-2.0-Begründung zum MariaDB-Treiber) sind übernommen, nicht nachgerechnet.
- **Reproduzierbar auf einem zweiten Rechner ist nicht gemessen** – eine Maschine, eine
  Werkzeugkette, ein Laufwerk.
- **Die Lückenlage gilt für den 18.10.2026** und für die Datenbank `vuln.go.dev`. Morgen kann sie
  anders sein, und heute merkt das niemand automatisch.
- **Keine der acht neueren Fassungen wurde eingespielt oder gebaut.** Dieser Auftrag schlägt vor,
  er entscheidet nicht. Dasselbe gilt für das Rausnehmen des Passkey-Strangs: vorgeschlagen, nicht
  getan.
- **Die Schalter-Änderung in `tools/release-gate.sh` ist nicht von `shellcheck` gesehen** – das
  Werkzeug gibt es auf dieser Maschine nicht (`which shellcheck`: nichts). Geprüft ist stattdessen
  `bash -n tools/release-gate.sh` (Syntax ok) und dass **beide geänderten Zeilen wortwörtlich
  gelaufen sind**, je mit Ausstiegscode 0. Das Skript als Ganzes ist weiter nie durchgelaufen, wie
  sein eigener Kopf sagt.
- **`tools/check.sh` ist nach den Änderungen gelaufen und hat `check: ok` gedruckt** – die Zeile,
  nicht die Null.
