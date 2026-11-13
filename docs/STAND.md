# Stand: NGC (NextGenCloud)

**Zuletzt aktualisiert:** 10.11.2026 · gemessen an `origin/main` **`681d4880`** (1488 Commits)

> **Die vorige Fassung war einen Monat alt** (10.10.2026) und nannte „fünf offene Pull Requests
> #26–#30" als nächsten Schritt. Die sind längst zusammengeführt, wir sind bei **#192**. Wer
> diese Datei liest, um sich auszurichten, lief damit in die Irre. Darum steht ab hier bei jeder
> Zahl, **woran** sie gemessen ist.

## Eine Falle gleich vorweg

**Der geteilte Checkout `Z:\GitHub\nextgencloud` steht nicht auf `main`.** Am 10.11.2026 stand er
auf `feldzug-echter-server`, **912 Commits hinter main**. Jede Datei, die dort geöffnet wird, kann
einen Monat alt sein — auch `CLAUDE.md` und diese Datei.

Vor jeder Messung:

    git -C Z:\GitHub\nextgencloud rev-parse --abbrev-ref HEAD
    git -C Z:\GitHub\nextgencloud rev-list --count HEAD..origin/main

Steht da nicht `main` und `0`, dann entweder mit `git show origin/main:<datei>` lesen oder einen
eigenen Arbeitsbaum anlegen — unter `D:\claude\working\ngc-<name>`, **nie auf `Z:`** (NAS: keine
Symlinks, lange Pfade).

## Wo wir stehen

**NGC läuft.** Das ist der Unterschied zur Fassung vom 10.10., die noch sagte „nichts davon ist je
gelaufen". Eine Anlage aus **sieben Maschinen** (api-1, api-2, db-1, svc-1, svc-2, store, ein)
trägt Installation, Anmeldung, Schreibtisch, Kopplung und zwei Apps. Drei Fassungen sind
veröffentlicht, die letzte **0.2.2** vom 23.10.2026.

| Was | Stand, gemessen am 10.11.2026 |
|---|---|
| `main` | `681d4880`, **kein offener Pull Request** (`gh pr list` = 0) |
| Entscheidungsbuch | bis **Punkt 102** (`docs/vertraege-v1.2.md`, Reihe B = Punkte 19–102) |
| Veröffentlicht | 0.2.0, 0.2.1, **0.2.2** (`gh release list`) |
| Web-Prüfsatz in CI | **ja, seit #192** — chromium 574, firefox 572, webkit 567, je **0 rot, 0 flatternd** |
| Playwright | **1.65.0** (1.55.1 lieferte Browser mit kaputter WebCrypto aus und übersprang ~290 Fälle je Engine still) |

### Die Teile

| Teil | Ordner | Fassung | Einstiegspunkt |
|---|---|---|---|
| Webinterface | `apps/web/` | 0.2.1 | PHP, kein `main.go` |
| API | `apps/server-api/` | 0.2.0 | `main.go`, 311 Zeilen |
| Controller | `apps/server-controller/` | 1.0.0 | `main.go`, 14 Zeilen |
| Datenbank | `apps/server-db/` | 0.1.0 | nur `classes/`, kein eigener Dienst |
| Werkzeug `ngc-store` | `apps/cli/` | 0.2.0 | `main.go`, 25 Zeilen |
| Uhr (zweite App) | `apps/uhr/` | 1.0.1 | `main.go` |

**Überholt aus der alten Fassung:** `Updater: nil` ist weg — `run.go:667` verdrahtet den Updater.
Frage 1 aus `docs/thomas-entscheidet.md` ist damit erledigt.

## Schon entschieden — nicht neu aufmachen

**Warum Go und nicht Rust?** Beantwortet und gemessen: **Buchpunkt 67**
(`docs/vertraege-v1.2.md`), 20.10.2026. Der Versuch liegt im Repo unter `versuch-rust/` — als
Beleg, **nicht** als Bestandteil; `tools/check.sh` kennt den Ordner nicht.

`shared/sign` (136 Zeilen Go, Ed25519) ein zweites Mal in Rust, gegen dieselbe Vektordatei:
**32 OK, 0 FAIL**, die Vektordatei aus Rust neu gebaut ist Byte für Byte die aus Go.
**Rust kann den Vertrag also treffen — und genau das ist das Argument dagegen:**

| für diesen **einen** Vertrag | Kisten/Module | Fremdzeilen |
|---|---|---|
| Rust (`cargo vendor`) | **41** | **577 163** |
| Go, ganzes NGC heute | 12 | — |

Go bringt Ed25519, SHA-256, base64url und strenges JSON in der Standardbibliothek mit; für
`shared/sign` ist die Zahl **null**. Die Hausregel „Fremdcode wird ins Repo kopiert und selbst
gepflegt" ist der Engpass, nicht die Bitgleichheit. **Wer Rust in NGC will, muss zuerst diese
Regel abschaffen.**

Zwei Befunde aus dem Versuch, die schwerer wiegen als die Sprachfrage und für **jede** künftige
Fremdsprache gelten:

- **`serde_json` nimmt ein doppeltes JSON-Mitglied eine Ebene tiefer an**, das Go ablehnt
  (`{"inner":{"a":1,"a":2}}` → Rust: `a=2`, Go: Fehler). `core`, `store` und `wire` ruhen auf
  dieser Zusage.
- **`wire.ValidText` ist an Gos Unicode-Tabellen gebunden** (`shared/wire/wire.go:282`, „Unicode
  17.0 with Go 1.27"). Eine Fremdsprache bringt eine dritte Tabellenfassung mit, die
  gleichzuhalten ist.

**Rust ist trotzdem nicht aus dem Projekt:** der Recovery-Modus ist bewusst in Rust geplant
(`docs/recovery-modus.md`, `lib/rust/`, `#![forbid(unsafe_code)]`) — ein Stück, das nur Schlüssel
anfasst und keinen der eingefrorenen Verträge nachbauen muss. Das ist **Punkt 68**, nicht 67.

## Nächster Schritt

Die Landkarte ist **`docs/was-fehlt.md`** (30.10.2026): 64 Einträge, davon **15 sperren**, 32
halten auf, 18 stören nur uns. Die Sperren in der Reihenfolge, in der ein Fremder sie trifft:

| | Worum | Was es braucht |
|---|---|---|
| **S1** | Es gibt keinen Laden — NGC liefert keine Apps, jede Installation müsste ihren eigenen nennen | Entscheidung, dann bauen |
| **S2** | Zwei Apps, und beide sind Beispiele | bauen |
| **S3/S4** | Der Release-Schlüssel ist eine Datei auf einer Testmaschine; der Stick kann nicht unterschreiben, weil Punkt 19 nicht gebaut ist | bleibt bei Thomas |
| **S5** | Ein Fremder kann die Kette nicht mit eigenem Schlüssel prüfen | bauen |
| **S6** | „Mit Passkey anmelden" kann nicht gehen | bauen |
| **S7/S8** | Ein `ngc-web`-Update kann nicht gehen; die Selbstaktualisierung endet am ersten Knoten | bauen |
| **S9** | Benutzerdaten werden nicht gesichert | bauen |
| **S10** | Für `.lan` stellt keine Zertifizierungsstelle aus | Messung |
| **S11/S12** | Die Oberfläche liegt auf einer Maschine und ist deutsch | bauen |
| **S13** | Eine Lizenz mit Ausstellungszeit in der Zukunft sperrt jede spätere aus | bauen |
| **S14** | Die erste Anmeldung sagt bei jedem „Anmeldung fehlgeschlagen" | bauen |
| **S15** | `ngc-ein` hat keine Vertrauensliste | bauen |

**Diese Liste ist vom 30.10.2026.** Am 9.11.2026 waren von 56 scheinbar offenen Zweigen 17
inhaltlich leer — eine Sperre kann also längst zu sein. **Erst nachsehen, dann anfangen.**

## Zweige: was wirklich offen ist

Am 9.11.2026 alle Zweige gemessen, die Commits tragen, die nicht auf `main` sind:

- **17 sind inhaltlich leer.** Wir führen per Squash zusammen, darum bleibt ein Zweig „Commits
  voraus", obwohl sein Inhalt oben steht. `web-pruefung-in-ci` stand 24 Commits vorne und war null.
- **38 tragen echte Arbeit**, keiner hat einen Pull Request.
- Die drei dringendsten: **`ngc-join-keys`** (schreibt keinen privaten Schlüssel mehr nach `/run`
  — Sicherheit), **`fristen-hintergrund`** (größter offener Block im API-Server, enthält ein unter
  `-race` gemessenes Datenrennen), **`ngc-rootrechte`** (Rechtemodell, 30 Dateien über drei
  Server, höchstes Konfliktrisiko, wenn es liegen bleibt).
- Nur **ein** Zweig fasst `shared/` an (`vertraege-v1-1`), und dem fehlen 18 Zeilen.

**Der Test, der dafür zählt:** `git diff origin/main...$b` mit **drei** Punkten taugt nicht, der
vergleicht gegen den Verzweigungspunkt und wird nie leer. Richtig ist:

    git diff origin/main $b -- <pfade des zweigs>   # Einfuegungen = 0  -> nichts fehlt main
    git cherry origin/main $b                       # new = 0           -> jeder Patch ist oben

## Im ulh-Repo (öffentlich — dort schiebt nur Thomas, nie ein Agent)

Am 9.11.2026 gemessen, zwei echte Löcher:

1. **Die `/run/ngc`-Reparatur ist halb drin.** `RuntimeDirectoryPreserve=yes` steht nur in den
   **Controller**-Units, in den **API**-Units fehlt sie. Genau dieser Mechanismus hat am
   3.11.2026 die Waisen auf `svc-1` verursacht: beide Dienste teilen `/run/ngc`, und eine Unit
   ohne die Zeile löscht es unter der anderen weg. Der Nachtrag liegt unveröffentlicht auf
   `archiv/ulh/api-runtimedir-preserve`. Die Zeile muss **vor** `[Install]` stehen, systemd
   verwirft sie dahinter still.
2. **`Delegate=yes` kommt in ulh-`main` nirgends vor.** Ohne das greifen `--pids-limit`,
   `--memory` und `--cpus` an App-Containern nicht — der Deckel ist geteilt statt pro App.

Von 18 Zweigen unter `archiv/ulh/` sind 15 inhaltlich erledigt. **Alle drei echten liegen auf dem
alten Pfad `apps/cli/`, main auf `apps/cli-ulh/`** — sie sind nicht mergebar wie sie sind, der
Inhalt muss umgesetzt werden.

## Fallen dieser Umgebung

Vollständig: `docs/arbeitsweise.md` (NGC-eigene Lehren) und `Z:\GitHub\claude\docs\` (gilt für
jedes Projekt). Die, die am meisten gekostet haben:

- **Der Checkout auf `Z:` steht nicht auf `main`** — siehe ganz oben.
- **Arbeitsbäume werden während der Arbeit hohl.** `git diff --name-only --diff-filter=D HEAD |
  wc -l` muss **0** sein, und zwar **vor jeder gemeldeten Zahl im selben Aufruf**. Fehlt
  Fremdcode, scheitert der Bau sichtbar — **fehlt eine Testdatei, läuft der Satz mit weniger
  Fällen und sieht grün aus.** Wiederherstellen nur die gelöschten Pfade (`git restore
  --source=HEAD -- <pfade>`), nie `git restore .`.
- **Der git-Index ist zwischen Sitzungen geteilt.** Darum `git commit -- <pfade>`, nie `git add
  -A`. Und **nie `git config` in einem Arbeitsbaum** — das landet in der gemeinsamen
  Konfiguration aller Sitzungen.
- **`push` und `pull_request` werfen zwei Läufe Sekunden auseinander**, und die zweigweise Sperre
  bricht einen ab — **welcher überlebt, ist ein Rennen**. `gh pr checks` schreibt für einen
  **abgebrochenen** Lauf „fail": die Schlussfolgerung je Lauf holen, `cancelled` ist kein
  `failure`.
- **Flatternd zählt rot**, bis null, ohne Obergrenze. Grün mit genannter Flatterzahl ist
  Verstecken.
- **Einen Flattrer am Protokoll zuordnen:** eine **gefallene Zusicherung** heißt Code — im
  Quelltext suchen. Ein **Verbindungsfehler** oder wandernde Übersprungene heißen Umgebung. Das
  ersetzt den Versuchsaufbau. Am 9.11.2026 wurde die Erklärung „Portvorrat" für
  `desktop-keyboard.spec.js:118` damit sauber verworfen: der Lauf lag auf einer frischen
  Wegwerf-Maschine, dort gibt es keine angehäuften Anschlüsse.
- **Die Anlage schaltet nachts gegen 03:45 UTC ab** und kommt neu hoch. Ein Fehler, der sich
  anhäuft, ist morgens weggewischt — wer ihn sehen will, muss abends messen.
- **`unzip` und `Expand-Archive` hängen** an großen Archiven (Bitdefender prüft jede Datei).
  Python `zipfile` nehmen.

## Wer macht was

Siehe `CLAUDE.md`. Kurz: `shared/` ändert **nur der Koordinator** — wer dort eine Änderung
braucht, meldet sie **BLOCKIERT mit der genauen Zeile** und baut sie nicht; eine
`shared/`-Änderung ist eine Vertragsänderung und braucht einen Buchpunkt mit einer Zeile in der
Reihe-B-Tabelle, sonst wird CI rot.
