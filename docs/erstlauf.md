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
3. **Hürde 5 — ein Dateimodus.** `apps/server-controller/classes/runtime/data.go` legt
   `config.AppDataDir` mit `0700` an. Kein Unit-Test kann das als Fehler erkennen, weil es im
   Prozess als root richtig aussieht; falsch wird es erst, wenn ein *anderer* Benutzer
   hindurchmuss.

Die anderen drei (3 Katalogfrist, 4 leere `rootUsers`, 6 geschlossener Rechtekreis) sind je für
sich korrekter Code. Sie werden erst beim Zusammensetzen zu einer Sackgasse.

## 2. Wie weit der Lauf gehen kann — und warum nicht weiter

**Entscheidung: zwei Phasen, und die zweite läuft hier nicht.**

- **Phase A (statisch, läuft überall):** Die Stationen 1, 2 und 7 haben eine Hälfte, die aus den
  Quellen dieses Repos **und des ulh-Repos zusammen** entscheidbar ist. Diese Hälfte läuft in
  Sekunden und braucht keine Maschine.
- **Phase B (Maschine):** Die Stationen 3–6 und 8 brauchen eine frisch installierte Maschine.
  Begründung mit Zeilen, nicht mit Vermutung:
  - **fscrypt.** `runtime.ErrNoFscrypt` und die Verschlüsselung je App brauchen
    `CONFIG_FS_ENCRYPTION`. Gemessen am 23.10.2026 auf dem WSL-Kern dieses Rechners
    (`6.18.33.2-microsoft-standard-WSL2`): `# CONFIG_FS_ENCRYPTION is not set`. **Alle**
    WSL-Instanzen teilen diesen Kern, also hilft eine neue Instanz nicht.
  - **podman rootless in einem Container** (podman-in-podman) ist die zweite Hälfte derselben
    Station und ebenfalls nicht in einem Container zu haben.
  - **Die eine eingerichtete Maschine darf nicht kaputtgehen.** Auf `ssh ngc` steht die Anlage, in
    der andere Sitzungen arbeiten; ihr Zustand ist mehrfach von Hand verändert (zwei Notlösungen
    in `users.js`, `/var/lib/ngc/apps` auf `0711`, `ngc-api` ein Sonderbau `0.2.0-confirmfix`).
    Zwei NGC-Anlagen auf einem Netz streiten außerdem um 7443 und 443.

  Darum sind diese Stationen **UNMEASURABLE**, bis `--machine <ziel>` auf eine Wegwerf-Maschine
  zeigt. Der Lauf endet dann mit **Code 2** und nennt sie beim Namen — nie mit `erstlauf: ok`.
  Ein Lauf, der still auslässt, was er nicht kann, wäre selbst die siebte Hürde.

**Lieber ein ehrlicher Lauf über die halbe Strecke als ein grüner, der nichts beweist.** Phase B
ist in diesem Zug **nicht gebaut** — die Stationen stehen mit Namen, Begründung und Zustand im
Lauf, damit der nächste dort anfängt und nicht beim Inventar.

## 3. Die Stationen

| # | Station | Aussage, die rot wird | Hürde | heute |
|---|---|---|---|---|
| 1 | installieren | Jeder Pfad, den `ngc-api` **schreibt**, muss von `ReadWritePaths=` der Unit gedeckt sein, die das ulh-Skript für dieses Teil schreibt. Beide Hälften werden aus den beiden Repos **gelesen**, nicht abgetippt. | 1 | **ROT** |
| 2 | API und Controller laufen | `tools/build-servers.sh` muss es geben, muss laufen, und die Programme, die es hinterlässt, müssen auf `-version` eine Nummer melden, die das **Muster der Oberfläche** annimmt (aus `contracts/text.js` gelesen). Gegenprobe im selben Lauf: ein Bau **ohne** `-ldflags` muss von demselben Muster abgelehnt werden. | 2 | **ROT** |
| 3 | Controller koppeln | Kopplung gelingt und der Controller meldet sich. | — | UNMEASURABLE |
| 4 | aufnehmen | Ein frisch aufgenommener Controller ist über die Oberfläche **änderbar** — nicht nur `rootUsers: []`. | 4 | UNMEASURABLE |
| 5 | Systemrechte setzen | Systemrechte sind über die Oberfläche setzbar. | 4 | UNMEASURABLE |
| 6 | Katalog da | Der Katalog ist **Minuten** nach der Installation da, nicht eine Stunde. | 3 | UNMEASURABLE |
| 7 | App einrichten | Der Modus von `config.AppDataDir` muss einen App-Benutzer **hindurchlassen** (x-Bit für andere) — die Begründung ist die Aussage, nicht die Zahl. | 5 | **ROT** |
| 8 | App erscheint im Menü | Die Rechte-Seite bietet Rechte auch für Apps an, die man noch **nicht** sieht. | 6 | UNMEASURABLE |

Warum 3, 4, 5, 6 und 8 keine statische Hälfte haben, steht als Kommentar im Lauf selbst — mit
Grund je Station, damit der Nächste eine Lücke in der Suche sehen kann statt einem Satz zu glauben
(`messen.md`: eine Abwesenheit belegt man mit Orten).

## 4. Die drei roten Stationen sind drei lebende Befunde

Der Lauf ist heute auf `main` **dreifach rot**, und jedes Rot ist eine Hürde aus dem Oktober, die
im Code noch steht:

1. `ngc-api.service` (ulh `ngc-kette`) gewährt `/var/lib/ngc /etc/ngc/tls`; die API schreibt
   `/etc/ngc/trust-anchor.json`. **Hürde 1 ist nicht behoben**, nur auf der einen Maschine
   umgangen.
2. Es gibt kein `tools/build-servers.sh`. **Hürde 2 kann jederzeit wiederkehren**, weil der
   nächste Release wieder von Hand gebaut wird.
3. `data.go` legt `config.AppDataDir` mit `0700` an. **Hürde 5 ist nicht behoben**, nur auf der
   einen Maschine auf `0711` gesetzt.

Behoben wird das nicht hier: Hürde 1 liegt in einem anderen Repo auf Zweigen, an denen andere
Sitzungen arbeiten, und 5 ist eine Zeile in einer Datei, die zu einem anderen Auftrag gehört.
Der Auftrag dieses Zuges war der **Lauf**.

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
