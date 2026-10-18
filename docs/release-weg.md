# Der Release-Weg

**Stand 12.10.2026.** Alles hier ist **gemessen**, im Arbeitsbaum auf der **lokalen Platte C:**,
auf `main` = `0bc2659`. Je Aussage steht die Stufe dabei:

| Stufe | heißt |
|---|---|
| **geschrieben** | der Text steht da, nichts davon ist durch einen Übersetzer gegangen |
| **übersetzt** | `go build`/`go vet` ist zufrieden |
| **ausgeliefert** | die Datei liegt dort, wo sie gebraucht wird |
| **AUSGEFÜHRT** | es ist gelaufen, und die Ausgabe steht im Bericht |

> **Der Kern in einem Satz:** aus `main` lässt sich heute **kein einziges** der fünf
> Release-Stücke bauen, und zwei der drei Formate, die dafür gebraucht werden, sind
> **nicht entschieden** — eines davon so, dass ein Release den Dienst beim ersten Start tötet.

---

## 1. Was ein Release ist

**AUSGEFÜHRT** (`go test -v ./tools/release/`, 10 Fälle, 0 rot): der Vertrag nimmt **5 von 9**
angebotenen Paaren an. Nicht behauptet, sondern `store.Release.Validate` gefragt.

| Datei | Teil | Architektur |
|---|---|---|
| `ngc-web-all.tar` | `ngc-web` | `all` |
| `ngc-api-amd64.tar` | `ngc-api` | `amd64` |
| `ngc-api-arm64.tar` | `ngc-api` | `arm64` |
| `ngc-controller-amd64.tar` | `ngc-controller` | `amd64` |
| `ngc-controller-arm64.tar` | `ngc-controller` | `arm64` |

Dazu im selben Verzeichnis: `release.json` (die unterschriebene `sign.Envelope`) und
`release-key.pem` (der öffentliche Schlüssel) — die Namen stehen als Konstanten in
`apps/cli/classes/release.go`, damit `ngc-store check` und
`shared/testdata/release/verify.sh` in dasselbe Verzeichnis sehen.

**Es gibt kein `ngc-db`.** Der Vertrag sagt den Grund selbst
(`shared/store/catalog.go`, `ReleaseAPI`): das API-Release **enthält** `ngc-store` und die
Datenbank-Migrationen. `apps/server-db/` hat darum auch keine `main.go` — gemessen. Ein Fall
hält das fest (`TestThereIsNoDatabasePart`), damit nicht still ein viertes Stück wächst und
die Migrationen zweimal mitfahren.

**Ein Release darf unvollständig sein.** `Release()` überspringt eine fehlende Datei
(`continue`) und liefert eine gültige Hülle. Nur `0` Dateien sind ein Fehler
(`ErrNoReleaseFile`). Wer also nur `ngc-web-all.tar` hinlegt, bekommt ein gültiges, einteiliges
Release — das ist absichtlich so, und es heißt: **Vollständigkeit prüft niemand.**

---

## 2. Was von `main` heute fehlt

**AUSGEFÜHRT** (`git ls-tree`, `git log --all --diff-filter=A`):

| Stück | wo es liegt | auf `main` |
|---|---|---|
| das Webinterface | Zweig `web`: **128** Dateien unter `apps/web/` | **2** Dateien (nur `CLAUDE.md`, `VERSION`) |
| `tools/pack-release.sh` | Zweig `release-packer` (`9e95445`) | **fehlt** |
| `tools/sign-release.sh` | Zweig `release-packer` (`58c0914`) | **fehlt** |
| `tools/release/closure.go` (Vollständigkeitsgraph) | Zweig `release-packer` | **fehlt** |

Weder `web` noch `release-packer` sind in `main` zusammengeführt (`git branch --merged main`).

**Daraus folgt der erste Satz dieses Dokuments:** `pack-release.sh` baut ohnehin nur
`ngc-web-all.tar` — und das Verzeichnis, aus dem es packt, gibt es auf `main` nicht. Für
`ngc-api` und `ngc-controller` gibt es **überhaupt kein Packwerkzeug**, auf keinem Zweig.

Was auf `main` **doch** geht, **AUSGEFÜHRT**:

```
CGO_ENABLED=0 GOOS=linux GOARCH=<arch> go build -buildvcs=false -trimpath \
  -ldflags "-s -w -X main.Version=$(cat apps/<app>/VERSION)" -o build/<name>-<arch> ./apps/<app>
```

Sechs Programme gebaut, alle Code 0: `ngc-api` (0.2.0), `ngc-controller` (1.0.0),
`ngc-store` (0.2.0), je `amd64` und `arm64`.

---

## 3. Wer unterschreibt, und womit

Der Schlüssel steht in [`release-schluessel.md`](release-schluessel.md), der öffentliche Teil
liegt als [`ngc-release-key.asc`](ngc-release-key.asc) daneben. Fingerabdruck
`2531 3B63 … 3F8F 4DBB`, Ed25519, privater Teil auf einem YubiKey, der ihn nie verlassen hat.

**Und genau dieser Schlüssel kann heute kein Release unterschreiben.** Das ist gemessen und
steht als **Punkt 19** in [`vertraege-v1.2.md`](vertraege-v1.2.md): `sign.Sign` signiert die
**rohe Nachricht** (für ein Release 193 Byte), und die Karte nimmt nur Längen an, die einem
bekannten Hash entsprechen — 32 und 64 Byte ja, 30 Byte `ERR Invalid value`.

**Also ist die Reihenfolge zwingend:**

1. V1.2-Punkt 19 in `shared/sign` einbauen: signiert wird `sha512(Message(purpose, payload))`.
2. Erst dann kann `tools/sign-release.sh` (Zweig `release-packer`) gegen die Karte arbeiten.

Bis dahin unterschreibt **nur** `ngc-store release` mit einem Schlüssel **aus einer Datei** —
und das ist per Entscheidung in `STAND.md` ausdrücklich nur für **Test**-Releases gedacht.

`shared/` ist eingefroren. Punkt 19 ist eine `shared/`-Änderung und braucht die Vertragsfassung;
diese Sitzung hat `shared/` **nicht angefasst**.

---

## 4. Wie die Fassung hineinkommt

**Gemessen, und es ist nicht, was in anderen Repos dieses Nutzers steht:**

- Es gibt **kein** `tools/set_version.py` in diesem Repo (`find` über den ganzen Baum).
- **Keine Go-Datei liest eine `apps/*/VERSION`.** Die Nummer kommt allein über
  `-ldflags "-X main.Version=…"` hinein; der Rückfall im Code ist `0.0.0-dev`.
- Dokumentiert ist die Baubefehlszeile **nur** in `apps/server-controller/CLAUDE.md`.
  `ngc-api` und `ngc-store` haben die Variable, aber keine aufgeschriebene Zeile.
- **AUSGEFÜHRT, Linux:** `./ngc-api --version` → `ngc-api 0.2.0 (contract 1)`. Die Zeile
  greift also.

Drei Dinge stimmen dabei nicht zusammen, und keines davon entscheide ich:

**a) Der Rückfall ist keine gültige Fassung.** `store.ValidVersion("0.0.0-dev")` ist `false`
(das Muster erlaubt nur drei Zahlen). Ein ohne `-ldflags` gebautes Programm meldet also eine
Fassung, die der Vertrag ablehnt. *Nicht gemessen*, was der Controller damit tut — er vergleicht
Fassungen mit `store.Newer`, und das antwortet auf eine ungültige Fassung `false`.

**b) Jedes Teilprogramm hat seine eigene Nummer, ein Release hat EINE.**
Gemessen: `cli` 0.2.0, `server-api` 0.2.0, `server-controller` **1.0.0**, `server-db` 0.1.0,
`web` 0.1.0. `store.Release` trägt **ein** Feld `Version`. **Welche Nummer ist die des
Releases?** Steht nirgends.

**c) Die Nummern sind dreistellig, der Hausstandard ist vierstellig.**
`Z:\GitHub\claude-md\version.md` schreibt `v.x.y.z` vor. `store.ValidVersion` nimmt **genau
drei** Zahlen an — eine vierstellige Nummer lehnt der Vertrag ab. Hier gewinnt der Vertrag,
weil er in eingefrorenem `shared/` steht; der Standard sieht eine Abweichung, die benannt
gehört statt still zu bleiben.

---

## 5. Was reproduzierbar ist und was nicht

| Stück | reproduzierbar | Beleg |
|---|---|---|
| die Go-Programme | **ja** | **AUSGEFÜHRT:** zweimal gebaut, `git hash-object` gleich — `ngc-api-amd64` `d117e5f9…`, `ngc-store-amd64` `73042cff…` (mit `-trimpath`, gleiche Werkzeugkette go1.27.2, gleiche Maschine) |
| die App-Pakete (`ngc-store pack`) | **ja** | Zeit im tar ist `0`, eigens gemessen (Vorarbeit, 02.10.2026) |
| das Web-Archiv (`pack-release.sh`) | **ja** | zwei Packläufe, dieselbe Prüfsumme (Packer-Nachmessung, 10.10.2026) — und zwar **weil** `--mtime` auf das Commit-Datum genagelt ist |
| `release.json` | **nein, und das muss so sein** | es trägt `seq` und `issued` |
| **das Abbild der Terminal-App** | **nein** | `FROM docker.io/library/debian:13-slim` — ein Tag, kein Digest (Punkt 39) |

**Der Satz, auf den es ankommt:** ein Release, von dem die Hälfte reproduzierbar ist, ist nur so
gut wie der Satz, der sagt welche. Hier ist er: **alles außer `release.json` und dem
Container-Abbild der Terminal-App ist bitgleich wiederherstellbar.** Das Abbild fährt nicht im
Release mit — es baut der Admin von Hand auf einem Baurechner (Entscheidung in Punkt 39, mit
drei Gründen am Code) —, aber es ist der Teil der Lieferkette, der **als root** läuft.

---

## 6. Wer prüft, bevor es rausgeht

`tools/release-gate.sh` ist dieser Weg. **Es ist nie im Ganzen gelaufen** — das steht als
Kopfblock in der Datei selbst, nicht nur hier. Was in einem Lauf vom 12.10.2026 wirklich
passierte:

| Schritt | Stufe | Ergebnis |
|---|---|---|
| 0 Arbeitsbaum vollständig | **AUSGEFÜHRT** | `0` gelöschte Dateien. Beim **ersten** Lauf rot, weil das Skript den `safe.directory`-Riegel nicht selbst setzte — behoben, der Riegel steht jetzt im Skript und gilt nur für den Lauf |
| 1 `tools/check.sh` | **AUSGEFÜHRT** | `check: ok`, ohne Pipe. **Der Beleg ist die gedruckte Zeile, nicht die Null** |
| 2 `-race` | **AUSGEFÜHRT** | 0 DATA RACE über den Baum **ohne** `shared/trust`. `shared/trust` getrennt gefahren und rot, wie dokumentiert (`Open: 65736 bytes allocated`): sein Fall misst Speicher-Obergrenzen, die Instrumentierung belegt selbst mehr. **Kein Datenrennen** — darum wird es gemeldet und nicht gezählt |
| 2a die Null belegt | **AUSGEFÜHRT** | Wegwerf-Mutation mit zwei Schreibern in einem eigenen Modul: **1×** `WARNING: DATA RACE`, Code 1. Der Melder war in diesem Lauf scharf |
| 3 `govulncheck ./...` | **AUSGEFÜHRT** | **0** erreichbare Lücken; 0 in importierten Paketen, **1** in einem verlangten Modul, das kein Code aufruft |
| 4 `shellcheck` | **nicht ausgeführt** | nicht auf dieser Maschine. Laut gemeldet und als Fehler gezählt |
| 5 `ngc-store check` je Teil | **übersetzt** | es gibt kein Release-Verzeichnis, aus Abschnitt 2 |
| 6 `verify.sh` (was der Server fährt) | **nicht ausgeführt** | braucht `jq`, das hier fehlt. **Die Server-Seite dieses Tores ist ungemessen** — derselbe offene Punkt steht in `STAND.md` |

Zwei Werkzeuge müssen **auf Linux** laufen, nicht hier: `verify.sh` (Schritt 6), weil es das
Skript des Servers ist, und `-race` zusätzlich auf Linux, weil die Programme dort laufen.

Was `ngc-store check` kann, **AUSGEFÜHRT** (`-v`, Fälle gezählt): **16** Fälle in
`TestCheckStoreNamesTheReason`, jeder mit eigenem Grund (Katalog weg, Datei getauscht, falscher
Schlüssel, Paket nach dem Unterschreiben gepackt, …). *Der Strangbericht nannte 18; gemessen
sind 16 in diesem Satz — die übrigen liegen in anderen Sätzen, nicht nachgezählt.*

---

## 7. Der Weg, Schritt für Schritt

Was **heute** schon geht, ist mit ✅ markiert; der Rest wartet auf eine Entscheidung oder auf
einen Server.

1. ⛔ **Entscheidung D2 und D3** unten — ohne sie ist jedes Release ein Blindflug.
2. ⛔ **V1.2-Punkt 19** in `shared/sign`, sonst kann der YubiKey nicht unterschreiben.
3. ⛔ `web` und `release-packer` nach `main` zusammenführen. **Reihenfolge aus `STAND.md`
   beachten:** Vorlage → Packer → Release → erst dann der ulh-Zweig `ngc-web-kopfzeilen`.
   Dabei den überholten Kommentar in `tools/release/extra-web.txt` berichtigen.
4. ⛔ Ein Packwerkzeug für `ngc-api` und `ngc-controller`. **Gibt es nicht**, auf keinem Zweig.
5. ✅ Die Programme bauen (Abschnitt 2, sechs Programme, Code 0).
6. ✅ `tools/release-gate.sh` Schritte 0–3 fahren.
7. ⛔ `ngc-store release <verzeichnis> <schlüssel>` — braucht Schritt 1 und 4.
8. ⛔ `tools/sign-release.sh` gegen die Karte — braucht Schritt 2.
9. ⛔ `release-gate.sh` Schritte 5–6 auf einem Linux-Server mit echtem `jq`.
10. ⛔ Veröffentlichen. **Nicht Sache eines Agenten:** im öffentlichen Repo `sorglos-it/ulh`
    erreicht ein Push nach `main` per Auto-Update **sofort alle Nutzer**. Der Zweig
    `ngc-skripte` liegt dort fertig und wartet auf Thomas.

---

## 8. Vier Entscheidungen für Thomas

Jede nach demselben Muster: **was gemessen ist · was passiert, wenn man es lässt · der kleinste
Eingriff · wer danach nachzieht.** Nichts davon ist hier entschieden.

### D1 — Den Digest des Container-Abbilds festnageln? Und wer hebt ihn an?

**Gemessen:** `apps/server-controller/templates/terminal/Containerfile` sagt
`FROM docker.io/library/debian:13-slim` — ein Tag. Ein Fall
(`TestTheContainerfileDoesNotPinItsBase`) wird **rot, sobald jemand festnagelt**, und sagt im
Kommentar, dass er dann weggenommen wird. Der Container hat laut Manifest `needs: ["root"]`.
`--pull=always` nagelt **nichts** fest.

**Wenn man es lässt:** zwei Bauten desselben Commits sind zwei Abbilder, und wer den Tag
beherrscht, beherrscht den Container der schärfsten App im System.

**Die Frage, die zum Release-Weg gehört, und sie ist die eigentliche:** ein festgenagelter
Digest, den niemand pflegt, ist nach einem halben Jahr **schlechter** als ein Tag — dann fahren
alle ein Debian ohne Sicherheitsfixes. Also: **wer hebt ihn an?**

Drei Antworten, die es gibt, mit Preis:

| Weg | Preis |
|---|---|
| **Tag lassen** | nicht reproduzierbar, Lieferkette offen. Kostet nichts |
| **Digest festnageln, Hand** | reproduzierbar — und jemand muss Debians Sicherheitsmeldungen lesen und den Digest nachziehen. **Ohne benannten Menschen ist das die schlechteste Variante** |
| **Digest festnageln, Release hebt ihn** | der Digest wird bei **jedem** Release neu geholt und mit ihm gebaut: reproduzierbar je Release, und die Fixes kommen mit dem Release-Rhythmus. Braucht eine Zeile im Release-Weg und einen Baurechner mit Netz |

**Mein Vorschlag, und er ist eine Abwägung, nicht gemessen:** der dritte Weg. Er ist der
einzige, der beides hat, und er braucht keinen Menschen, der an etwas denkt.

**Danach nachzuziehen:** der Controller-Strang nimmt
`TestTheContainerfileDoesNotPinItsBase` heraus und sagt es im Bericht; Punkt 39 wird in
`vertraege-v1.2.md` als entschieden vermerkt.

### D2 — Was ist eine `ngc-web`-Release-Datei? (P3, und schlimmer als beschrieben)

**Gemessen, AUSGEFÜHRT** (`go test -v ./tools/release/`, Datei `webtar_test.go`, 5 Fälle):

Die zwei Hälften, die es gibt, können **nicht** zusammenkommen.

- `tools/pack-release.sh` baut mit `git archive --mtime=@<Commit-Datum>` über `apps/web`,
  **ohne** Namenspräfix.
- Der Controller liest mit `store.WalkUI` (`install.go`, `unpack`, mit BLOCKED-Absatz).

`store.WalkUI` kennt **genau einen** tar-Dialekt, und ein echtes `git archive` bricht ihn an
**drei** Stellen. Alle drei am Kopfblock des Archivs gemessen, das git wirklich geschrieben hat:

| Feld | `git archive` schreibt | `WalkUI` verlangt |
|---|---|---|
| Modus | `0000664` | `0000644` |
| Zeit | das genagelte Datum; **`--mtime=@0` hilft nicht** (Feld las `015263246526`) | `00000000000` |
| Name | `public/index.php` | `ui/public/index.php` |

Je Ursache ein Fall, und **zuerst** ein Grundfall, der beweist, dass vom Messgerät gebaute
Bytes durchgehen — ohne ihn wären die vier Abweisungen alle rot für irgendeinen Grund.

> **Nebenbei und ehrlich:** mein erster Fall dazu war **falsch**. Er baute das Archiv mit Gos
> `archive/tar`, das `WalkUI` aus Gründen ablehnt, die mit der Kollision nichts zu tun haben —
> der Fall war grün und maß das Falsche. Sein **eigener Gegentest** hat ihn gefangen. Er ist
> ersetzt, und warum, steht an seiner Stelle in `release_test.go`.

**Wenn man es lässt:** das Webinterface ist nach dem ersten Update **leer**, und es gibt heute
keine Stelle, an der die beiden Annahmen aufeinandertreffen und auffallen.

**Der kleinste Eingriff ist nicht mehr der aus dem V1.3-Vorschlag.** Dort steht „ein Satz in
`shared/`, der sagt: tar nach `store.WalkUI`, Namen unter `ui/`" — das genügt **nicht**, weil
Modus und Zeit auch brechen. Es bleiben zwei echte Wege:

| Weg | was sich ändert |
|---|---|
| **a) `shared/` bekommt einen Auspacker für ein schlichtes tar** | eine neue Funktion in `shared/store`, **ergänzt nur**. Der Packer bleibt, wie er ist, samt seiner gemessenen Reproduzierbarkeit |
| **b) Der Packer schreibt ein `store.UIWriter`-Archiv** | kein `git archive` mehr — und damit **muss die Reproduzierbarkeit neu begründet werden**, denn sie ruht heute auf `git archive` |

**Mein Vorschlag:** (a). Es ergänzt nur, es lässt die eine gemessene Reproduzierbarkeit des
Projekts in Ruhe, und ein sicherer tar-Auspacker in `shared/` ist ohnehin das, was der
BLOCKED-Absatz des Controllers verlangt. Dazu gehört die **echte** Testdatei:
`shared/testdata/release/ngc-web-all.tar` ist heute **18 Byte ASCII-Text**, kein tar
(selbst nachgemessen, Inhalt `NGC web test file`).

**Danach nachzuziehen:** `release-packer` bleibt; der Controller streicht seinen BLOCKED-Absatz;
`shared/testdata/release/` bekommt die richtige Datei; `webtar_test.go` wird umgeschrieben und
sagt, welche Form gewonnen hat.

### D3 — Was ist eine `ngc-api`- und `ngc-controller`-Release-Datei?

**Dieselbe Form wie D2, und sie steht in keinem Vorschlag — auch nicht im V1.3-Vorschlag, der
nur `ngc-web` nennt.** Sie ist die gefährlichste der vier.

**Gemessen am Code auf `main`:** `installPart` **entpackt nichts**. Es ruft `copyInto`
(reine Bytekopie), `chmod 0755` und `os.Rename` auf `BinDir/ngc-api` — es installiert die
Release-Datei **als das Programm selbst**. Dabei nennt der Vertragsweg die Datei
`ngc-api-amd64.tar` (`releaseExt = ".tar"`).

**AUSGEFÜHRT, auf Linux** (Debian 13.7 in WSL, mit Dichtheitsprüfung `(exit 7)` davor, die
`7` und `1` lieferte; Messskript als Datei, Code **in** WSL geschrieben):

```
tar-Magie = 6e 67 63 2d   ("ngc-", der Name im tar-Kopf)
bin-Magie = 7f 45 4c 46   (ELF)

genau die Handlung des Controllers (Bytes kopieren, chmod 0755, starten):
  ./ngc-api-installiert --version   ->  Code 126, "Exec format error"
Gegenprobe, das entpackte Programm:
  ./ngc-api --version              ->  Code 0, "ngc-api 0.2.0 (contract 1)"
```

**Wenn man es lässt:** baut irgendwer die `.tar`-Datei als echtes tar — und für `ngc-web` tut
der Packer genau das —, dann schiebt der Controller sie auf den Namen des API-Programms, startet
den Dienst, und der stirbt mit **126**. Das Rollback greift (`.prev` liegt), aber die
**Erstinstallation** hat kein `.prev`.

**Ich sage nicht, dass der Installierer falsch ist.** Die zweite Lesart ist gut möglich: die
Datei ist **das nackte Programm** und trägt die Endung `.tar` nur als Name. Dafür spricht, dass
auch `shared/testdata/release/ngc-web-all.tar` kein tar ist — die Testdaten behandeln diese
Dateien als **undurchsichtige Klumpen**. Dann ist `releaseExt = ".tar"` ein irreführender Name
und sonst nichts.

**Zu entscheiden ist also genau eine Frage:** ist eine `ngc-api`-Release-Datei **das Programm**
oder **ein tar mit dem Programm darin**?

| Weg | kleinster Eingriff | Preis |
|---|---|---|
| **das Programm** | ein Satz in `shared/`, und die Endung `.tar` wird zu `.bin` **oder** bleibt und der Satz erklärt sie | ändert am Code **nichts**. Ein Release kann dann keine zweite Datei je Teil tragen (keine Konfigvorlage, keine Migration daneben) |
| **ein tar** | `shared/` braucht den Auspacker aus D2 **und** der Controller eine Auspackstufe in `installPart` | mehr Code, aber `ngc-api` fährt „ngc-store und die Migrationen" laut Vertrag ohnehin mit — **und das geht in einer einzigen Datei nicht** |

**Mein Vorschlag: ein tar**, und zwar aus einem Grund am Vertrag, nicht am Geschmack: der
Vertrag sagt selbst, das API-Release **enthält** `ngc-store` und die Datenbank-Migrationen. Drei
Dinge passen nicht in eine Programmdatei. Dann ist derselbe Auspacker aus D2 für beide Seiten da,
und es gibt nur **eine** Form statt zweier.

**Danach nachzuziehen:** `installPart` bekommt die Auspackstufe; `TestATarIsNotAProgram` in
`tools/release/` wird umgeschrieben; und das Packwerkzeug aus Schritt 4 wird nach dieser
Entscheidung gebaut, nicht davor.

### D4 — Welche Nummer ist die Fassung des Releases?

**Gemessen:** fünf Teilprogramme, fünf Nummern (`controller` steht auf **1.0.0**, `web` auf
0.1.0). `store.Release` hat **ein** Feld `Version`. Dazu die Abweichung vom Hausstandard:
dreistellig hier, vierstellig in `claude-md\version.md`, und `store.ValidVersion` nimmt eine
vierstellige **nicht** an.

**Wenn man es lässt:** der Erste, der ein Release baut, wählt eine Nummer, und `ngc-web.sh`
beim Nutzer entscheidet über `seq` ohnehin getrennt. Die Nummer wird dann zur Zierde — und
`store.Newer` vergleicht sie, also entscheidet sie mit, ob ein Update überhaupt eingebaut wird.

**Kleinster Eingriff:** eine Datei `VERSION` im **Stamm** als die Nummer des Releases, und die
Teilprogramm-Nummern bleiben, was sie sind (die Fassung des Programms). Dazu eine Zeile in
diesem Dokument, welche von beiden `-ldflags` bekommt.

**Zu entscheiden gehört dazu:** ob dieses Repo bei drei Stellen bleibt (dann ist die Abweichung
vom Hausstandard festzuhalten) oder ob `store.ValidVersion` vier Stellen lernen soll (dann ist
es eine `shared/`-Änderung und gehört in V1.3).

---

## 9. Was diese Sitzung gebaut hat

| Datei | Stufe |
|---|---|
| `docs/release-weg.md` | **geschrieben** — dieses Dokument |
| `tools/release/release.go` | **AUSGEFÜHRT** — fragt den Vertrag nach den Teilen, hält keine eigene Liste |
| `tools/release/release_test.go` | **AUSGEFÜHRT** — 5 Fälle |
| `tools/release/webtar_test.go` | **AUSGEFÜHRT** — 5 Fälle, D2 je Ursache einzeln |
| `tools/release-gate.sh` | **Schritte 0–3 AUSGEFÜHRT**, 4–6 **nicht ausgeführt** (im Kopfblock der Datei benannt) |

Zusammen: `go test -v ./tools/release/` → **10 Fälle, 0 rot**, Code 0, Arbeitsbaum auf **C:**,
`main` = `0bc2659`. `bash tools/check.sh` ohne Pipe → `check: ok`.

**`shared/` ist nicht angefasst.** Alles, was dort zu ändern wäre, liegt als D1–D4 oben und als
Verweis in [`vertraege-v1-3-vorschlag.md`](vertraege-v1-3-vorschlag.md).
