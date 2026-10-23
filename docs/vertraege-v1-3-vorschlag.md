# Vertragsfassung V1.3 — Entscheidungsvorlage

**Stand 15.10.2026.** Jeder Punkt ist gegen den eingefrorenen Code nachgemessen und so
umgeschrieben, dass er mit JA oder NEIN zu beantworten ist.

**Dieses Dokument ändert nichts.** In `shared/` ist nichts angefasst. Jeder Änderungssatz
steht als Textblock da und muss nach dem JA nur noch abgeschrieben werden.

---

## Wie gemessen wurde

- Arbeitsbaum `D:\claude\nextgencloud-v13`, Zweig `vertraege-v13-vorschlag`, von
  `origin/main` (`48cb3aa`). Lokale Platte, nicht Z:.
- Baum vollständig: `git diff --name-only --diff-filter=D HEAD` = **0** Zeilen.
- Alle acht eingefrorenen Dateien sind **byteweise der Stand von HEAD**
  (`git hash-object` gegen `git rev-parse HEAD:<datei>`, 8 von 8 gleich). Jede Zeilennummer
  unten ist aus diesem Stand.
- Zwei Sonden liefen gegen diesen Stand (Go 1.27.2, eigenes Modul mit `replace`, nichts im
  Baum geändert). Ihre Ausgaben stehen bei P2 und P10 wörtlich.

**Die Nummern sind neu vergeben.** Die alte Reihe hatte `P6`, `P7` und `P8` **doppelt**
(einmal als Posten, einmal als „gehört nicht hierher"). Die Nicht-Posten heißen jetzt
**N1–N3**. Vier Punkte sind dazugekommen: **P10–P13**.

---

## Die Lage in sechs Zeilen

| Punkt | Stand | Art |
|---|---|---|
| **P10** — kein Vertrag darf etwas dazubekommen | **neu, gemessen** | **Tor für P8 + P9** |
| P1 — `validHTTPSURL` nicht ausgeführt | gilt | Sicherheit, Naht |
| P2 — `Unattended` verbietet den ersten Einbau | gilt, **schlimmer** | geht gar nicht |
| P3 — Format einer `ngc-web`-Release-Datei | gilt — **dringender**: die zwei Formen sind gemessen verschieden, `WalkUI` weist das Wurzel-tar ab (Nachtrag 18.10.2026) | geht gar nicht |
| **P4** — Balancer-Vorlage in `shared/ulh.md` | **ERLEDIGT** | gestrichen |
| P5 — Referenzfeld in `dbapi.App` | gilt | geht gar nicht |
| P6 — Kopplungscode 24 Stunden | gilt | Sicherheit, Zahl |
| P7 — `ngc-api migrate` braucht ganze `api.json` | gilt | geht gar nicht |
| P8 — Leerlauf nicht von Absturz zu trennen | gilt, **teurer als gedacht** | hängt an P10 |
| P9 — „besetzt" braucht `E_BUSY` | gilt, **teurer als gedacht** | hängt an P10 |
| **P11** — `ValidPath` sagt nichts zur Schreibweise | **neu** | Satz, kostenlos |
| **P12** — Rechte im Katalog nie gegen das Manifest | **neu** | Sicherheit |
| **P13** — kein Prozessdeckel je App im Manifest | **neu** — Empfehlung **NEIN für V1.3** (Bedarf nicht gemessen), unten unter „Die Reihenfolge" darum als **gestrichen** geführt | Feld |

---

## P10 — Kein Vertrag von `shared/` darf etwas dazubekommen

**Neu, und es ist die Wurzel von P8 und P9.** Die alte Reihe nennt beide „Ergänzung, kein
Bruch". **Das ist gemessen falsch.**

**Gemessen am 15.10.2026**, Sonde gegen den HEAD-Stand von `shared/wire`:

```
ParseClose(code=E_BUSY        ) -> code=""              err=wire: malformed CLOSE payload
ParseClose(code=E_IDLE        ) -> code=""              err=wire: malformed CLOSE payload
ParseClose(code=E_UNREACHABLE ) -> code="E_UNREACHABLE" err=<nil>
ParseClose(status=ok)          -> status="ok"           err=<nil>
```

**Die Zeile dahinter:** `shared/wire/wire.go:528`, in `Close.Validate`:

```go
case StatusError:
    if _, ok := messages[c.Code]; !ok {
        return ErrClose
    }
```

Ein Leser, der den neuen Code nicht kennt, **wirft den ganzen Rahmen weg**. Er sieht
**nicht** „einen Schluss ohne Grund", wie P8 behauptet — er sieht einen **kaputten
Rahmen**.

**Und es ist nicht nur diese Zeile.** `shared/README.md:199–200` macht es zur Regel für
alles:

> „Feldnamen genau wie im Vertrag, auch in Groß- und Kleinschreibung; **unbekannte Felder
> werden abgelehnt**."

**Es gibt keine Protokollfassung.** Gesucht in `shared/wire/wire.go` und
`shared/tunnel/tunnel.go`: das einzige `Version`-Feld (`tunnel.go:151`) ist die Fassung
einer **App**, nicht des Protokolls. Zwei Seiten können sich also nicht darauf
verständigen, was sie beide verstehen.

**Was das heute kostet:** API, Controller und Weboberfläche werden **einzeln** aktualisiert
(`trust.AutoAPI`, `AutoController`, `AutoWeb` sind drei Komponenten,
`shared/tunnel/tunnel.go:164`). Eine neue API mit `E_BUSY` an einen alten Controller
heißt: abgewiesener Rahmen, nicht „unbekannter Grund".

### Der Änderungssatz (zwei Zeilen)

`shared/wire/wire.go`, in `Close.Validate`, `case StatusError` — aus

```go
        if _, ok := messages[c.Code]; !ok {
            return ErrClose
        }
```

wird

```go
        // An unknown code comes from a newer peer: a code this build does not
        // know is read as CodeInternal, so a frame with a reason this side
        // cannot name is still a close with a reason and not a broken frame.
        // Without this every code added later breaks every older reader.
        if _, ok := messages[c.Code]; !ok {
            c.Code = CodeInternal
        }
```

(`Validate` braucht dafür einen Zeiger-Empfänger, oder `ParseClose` setzt es nach dem
Prüfen — je ein Wort Unterschied, derselbe Satz.)

Dazu **ein Satz in `shared/README.md`**, nach Regel 3:

> Eine Aufzählung eines Vertrags (`wire.Code`, `tunnel.AppState`, `store`-Arten) darf
> wachsen. Ein Leser, der einen Wert nicht kennt, nimmt den dafür benannten Rückfall
> (`wire.Code` → `CodeInternal`) und lehnt die Nachricht **nicht** ab. Felder bleiben
> streng: ein unbekanntes Feld wird weiter abgelehnt.

### Der Preis

- **Das bricht den Vertrag.** `ParseClose` antwortet bei derselben Eingabe anders: heute
  Fehler, danach ein Schluss mit `E_INTERNAL`. **Neue Fassung nötig.**
- Mitziehen muss: niemand. Kein Aufrufer ändert sich, kein Feld wandert.
- **Nachzuziehen:** die Fälle in `shared/wire/wire_test.go`, die heute prüfen, dass ein
  unbekannter Code abgewiesen wird — die werden rot und müssen umgedreht werden.
- Die Weboberfläche hat ihre eigene Tabelle (`contracts/wire.js`, nur Zweig `web`); sie
  braucht denselben Rückfall. Auf `main` liegen in `apps/web/` **zwei** Dateien, dort ist
  heute nichts zu ändern.
- Für einen Nutzer, der schon installiert hat: **nichts.** Ein alter Leser bleibt ein alter
  Leser; die Regel wirkt erst für den, der sie mitbringt.

### Ohne die Änderung

- Jeder neue Code ist **für immer** ein Bruch. P8 und P9 sind dann nicht „eine Zeile",
  sondern „alle drei Teile müssen gleichzeitig aktualisiert werden" — und genau das kann
  NGC heute nicht zusagen.
- Was für immer fehlt: die Möglichkeit, einen Grund **überhaupt** nachzutragen. Nicht
  „besetzt" fehlt dann, sondern jeder künftige Grund.

**Empfehlung: JA, und zuerst** — weil ohne diese zwei Zeilen jede weitere Zeile dieser
Liste doppelt so teuer ist.

---

## P1 — Die Adressprüfung ist nicht ausgeführt

**Gilt.** Nachgemessen am 15.10.2026:

- `shared/store/catalog.go:208`: `func validHTTPSURL(u string) bool` — **klein**.
- Aufrufer in `shared/`: genau einer, `catalog.go:192`, innerhalb von `validURL`.
- Die schwächere zweite Grammatik: `apps/server-api/classes/appstore/fetch.go`,
  `downloadURL` — prüft Länge, `#`, Schema, Rechnername; **den Pfad nicht**.

**Was nicht gilt:** der alte Rang. Heute kommt dort keine ungeprüfte Adresse an —
`Download` hat einen Aufrufer im Betrieb, und der nimmt den Eintrag aus einem über
`store.VerifyCatalog` geprüften Katalog. **Es ist eine Naht ohne Riegel, kein offenes
Loch.**

### Der Änderungssatz

`shared/store/catalog.go` — Funktion umbenennen und den Kommentarkopf anpassen:

```go
// ValidDownloadURL reports whether u is an address NGC fetches a package or a
// UI tar from: nothing, or "/" and a path and query of printable ASCII (0x21
// to 0x7e) without " ' < > \ and #; at most maxURL bytes.
//
// It is the one grammar of a signed download address. A fetcher outside this
// package checks the address it is about to request against this function
// rather than trusting that VerifyCatalog has already seen it.
func ValidDownloadURL(u string) bool {
```

und im Rumpf von `validURL` (`catalog.go:192`):

```go
        return ValidDownloadURL(u)
```

### Der Preis

- **Ergänzt nur, bricht nicht.** Kein bestehender Aufrufer ändert sich, kein Verhalten
  ändert sich.
- Mitziehen muss der **api-Strang**: `appstore.downloadURL` und `maxDownloadURL` fallen
  weg, ihre Fälle wandern mit. Die Begründung im Code daneben
  („the value is repeated here and nothing else of that grammar is") wird gestrichen.
- Für einen Nutzer, der schon installiert hat: **nichts.**

### Ohne die Änderung

- Es geht alles. Heute fehlt **nichts**.
- Für immer fehlt: die Zusage, dass jeder Abholer dieselbe Grammatik benutzt. Der
  **nächste** Aufrufer von `Download` bekommt keine Warnung und hält seine Prüfung für
  vollständig, obwohl sie den Pfad nicht ansieht.

**Empfehlung: JA** — Sicherheit gegen Bequemlichkeit, und sie kostet hier nichts
(Umbenennung, kein Verhalten).

---

## P2 — Ein ERSTER Einbau ohne Aufsicht geht nicht

**Gilt, und es ist schlimmer als in der alten Reihe.**

**Gemessen am 15.10.2026**, Sonde gegen den HEAD-Stand:

```
Newer("1.2.0","")      = false
ValidVersion("")        = false
Newer("1.2.0","1.1.0") = true
```

Die Zeilen:

- `shared/store/manifest.go:236` — `Newer` gibt für jede ungültige Fassung `false`, und
  `""` ist keine.
- `shared/core/core.go:225` — `func Unattended(...)`, und `:229`:
  `return p.Target == trust.AutoSystem || store.Newer(p.Version, installed)`.
- Der Kommentar darüber (`core.go:214`) sagt: **„`""` never is older"**. **Gemessen
  falsch.** Genau der Fehler, vor dem „Kommentar gegen Code lesen" warnt.

**Neu gegenüber der alten Reihe: der Ersatz steht jetzt an ZWEI Stellen**, nicht an einer.

| Stelle | Zeile |
|---|---|
| Controller | `apps/server-controller/classes/update/install.go:353` |
| **API** | `apps/server-api/classes/updates/rollout.go:253` |

Beide schreiben dasselbe von Hand: `installed != "" && !store.Newer(...)`. Die
gemeinsame Wurzel hat also schon zweimal gekeimt.

Dazu zwei Wächter, die **rot werden, sobald `shared/` repariert ist** — so gebaut,
absichtlich: `install_test.go:929` und `:934`, in
`TestInstallOfAFirstPartWithoutAUserIsRefusedByTheContract`.

### Der Änderungssatz

`shared/core/core.go:229` — aus

```go
	return p.Target == trust.AutoSystem || store.Newer(p.Version, installed)
```

wird

```go
	return p.Target == trust.AutoSystem || installed == "" ||
		store.Newer(p.Version, installed)
```

Im Kommentarkopf (`core.go:213–214`) wird der Satz **wahr**, nicht umgeschrieben:

```go
// file's release or catalog; nil for trust.AutoSystem), and p.Version is
// newer than installed, where nothing installed ("") is older than every
// version: the FIRST install of a part is an unattended one too. store.Newer
// answers false for "" because "" is no version, so that case stands here.
```

### Der Preis

- **Das bricht den Vertrag.** `Unattended` antwortet bei derselben Eingabe anders.
  **Neue Fassung nötig.** Eine Nebenfunktion (`store.NewerOrFirst`) hilft nicht: das
  Verhalten von `Unattended` ändert sich so oder so.
- Mitziehen müssen: **Controller und API**. Beide behalten ihren lokalen Riegel (er ist
  laut Kommentar die einzige Wand für den Kanal **mit** Aufgabe), aber die zwei Wächter in
  `install_test.go` müssen umgedreht werden — sie sind darauf gebaut zu fallen.
- Für einen Nutzer, der schon installiert hat: **nichts** — er hat ja installiert. Der
  Preis fällt bei dem an, der noch nichts hat.

### Ohne die Änderung

- Alles Bestehende läuft. Updates funktionieren.
- Für immer fehlt: **jede Erstinstallation von `ngc-api`, `ngc-web` oder
  `ngc-controller` ohne angemeldeten Benutzer.** Die stehende Genehmigung in der
  Vertrauensliste nützt genau beim ersten Mal nichts — also dann, wenn niemand da ist.
  `trust.AutoSystem` ist ausgenommen und bleibt es.

**Empfehlung: JA** — eine Zusage, die im eigenen Kommentar steht und gemessen nicht gilt,
ist keine Abwägung, sondern ein Fehler.

---

## P3 — Was eine `ngc-web`-Release-Datei ist, steht nirgends

**Gilt.** Nachgemessen am 15.10.2026:

- `shared/config/config.go:35`: `WebRoot = "/var/www/ngc"` — und in **keiner weiteren
  `.go`-Datei** von `shared/`.
- `shared/testdata/release/ngc-web-all.tar` ist **18 Byte**, Inhalt:
  `NGC web test file` — kein tar.
- Der Controller hat sich entschieden und es als BLOCKED hingeschrieben:
  `apps/server-controller/classes/update/install.go:873` (die alte Reihe nannte `:846`;
  die Funktion ist gewandert).

**Neu gemessen, und es macht die Entscheidung billiger:** `release-packer` **gibt es
nicht**. In `tools/` liegen `check.sh`, `modules.go`, `tunnelprobe` — kein Packer, und
keine Datei in `tools/` nennt `ngc-web`. Der zweite Schreiber ist also noch nicht gebaut:
**heute entscheiden kostet nichts, später entscheiden kostet einen Umbau.**

### Der Änderungssatz

`shared/README.md`, im Abschnitt zu den Release-Dateien, ein Absatz:

> **Release-Datei eines NGC-Teils.** Eine Release-Datei von `ngc-web` ist ein tar in
> genau der Form, die `store.WalkUI` liest: alle Namen unter `ui/`, keine absoluten
> Pfade, kein `..`, keine Links, keine Sparse-Dateien, begrenzte Größen. Der Controller
> entpackt sie mit `store.WalkUI` und ersetzt das Präfix `ui/` durch `config.WebRoot`.
> Das Release-Werkzeug baut genau diese Form. Es gibt keine zweite Auspack-Funktion.

Dazu `shared/testdata/release/ngc-web-all.tar` **durch ein echtes tar dieser Form
ersetzen** (ein `index.html` unter `ui/` reicht), und einen Fall, der es mit
`store.WalkUI` liest.

### Der Preis

- **Ergänzt nur** — wenn die Entscheidung so ausfällt, wie der Controller gebaut hat.
- **Bricht**, wenn es ein schlichtes tar ohne `ui/` werden soll: dann braucht `shared/`
  eine **zweite sichere Auspack-Funktion**, und der Controller baut seinen Teil um. Das
  ist der teure Weg, und nichts spricht heute dafür.
- Mitziehen muss: das Release-Werkzeug (gibt es noch nicht — es wird so gebaut); der
  Controller streicht seinen BLOCKED-Absatz.
- Für einen Nutzer, der schon installiert hat: **nichts** — bis zu seinem ersten
  `ngc-web`-Update, und das ist der Punkt.

### Ohne die Änderung

- Es geht alles, solange niemand ein `ngc-web`-Release baut.
- Für immer fehlt: die Stelle, an der die beiden Annahmen aufeinandertreffen. Trifft das
  Release-Werkzeug eine andere, ist das Webinterface nach dem ersten Update **leer oder
  halb** — und `WebRoot` existiert zwischen zwei Umbenennungen ohnehin kurz nicht
  (`install.go:843`). Es fällt niemandem auf, bis es einem Nutzer auffällt.

**Empfehlung: JA, in der Form, die der Controller gebaut hat** — sie benutzt die eine
sichere Auspack-Funktion, die es schon gibt, statt eine zweite zu verlangen.

### Nachtrag 18.10.2026: „P3 ist beantwortet" stimmt nicht — die zwei Annahmen sind aufeinandergetroffen

`vertraege-v1.2.md`, **Punkt 56** (16.10.2026) schreibt: „Damit ist auch **P3 beantwortet**,
gemessen am Code der Skripte". Gegengelesen ist das **falsch** — und zwar so, dass es den
Punkt nicht erledigt, sondern **zuspitzt**:

| Seite | Form, die sie annimmt | Beleg |
|---|---|---|
| die **Skripte** (`ngc-store release`, ulh) | Namen im **Wurzelverzeichnis**: `public/index.php`, `templates/apache-vhost.conf`, 83 Einträge | `vertraege-v1.2.md`, Punkt 56 |
| der **Controller** beim Update | Namen **unter `ui/`**, Präfix wird durch `config.WebRoot` ersetzt | `apps/server-controller/classes/update/install.go:869–880` („whose names have to be rooted at `ui/`"), `shared/store/ui.go:193` |

**Die beiden Formen sind nicht dieselbe.** Punkt 56 hat gemessen, was der **Einrichtungsweg**
baut und auspackt; P3 fragt, was der **Updateweg** auspackt. Dass beides heute nebeneinander
gutgeht, liegt allein daran, dass noch nie ein `ngc-web`-Update über den Controller gelaufen ist.

**Was P3 „ohne die Änderung" vorhergesagt hat, ist damit keine Vermutung mehr:** trifft das
Release-Werkzeug eine andere Annahme als der Controller, ist das Webinterface nach dem ersten
Update **leer oder halb**. Genau dieser Fall liegt vor.

**Gemessen am 18.10.2026** mit einer Sonde gegen den Stand dieses Zweigs (eigenes Modul mit
`replace`, nichts im Baum geändert), beide Läufe aus **einem** Aufruf:

```
tar gebaut: 2048 Byte
Zwischenpruefung ui/...      err=<nil>                        Dateien=[ui/public/index.php]
Wurzel (ohne ui/)            err=store: invalid app package   Dateien=[]
```

Dieselbe Vorrichtung, derselbe Inhalt, **ein** Unterschied: im zweiten Lauf steht im
Namensfeld jedes Kopfblocks `public/index.php` statt `ui/public/index.php` (Prüfsumme nach
der Regel aus `ui.go:31–33` neu gerechnet). Die **Zwischenprüfung** ist der Beleg, dass der
Weg bis zur gemeinten Regel offen ist — sonst misst die Sonde ihre eigene Bauweise.

**Also: `store.WalkUI` weist ein Wurzel-tar ab, mit `ErrPackage`.** Ein `ngc-web`-Update über
den Controller **kann** mit einem Archiv in der Form, die Punkt 56 gemessen hat, nicht gehen —
es scheitert am ersten Kopfblock, bevor eine einzige Datei entpackt ist.

**Was an dieser Zahl noch fehlt:** gefahren wurde gegen ein **nachgebautes** Wurzel-tar, nicht
gegen das echte `ngc-web-all.tar` der Skripte (das liegt im Repo `ulh`, nicht hier). Die Form
stimmt mit Punkt 56 überein; die Gegenprobe am echten Archiv ist **einen Aufruf wert**, sobald
eines vorliegt.

**Die Empfehlung bleibt JA, aber die Entscheidung ist jetzt eine andere Frage:** nicht mehr
„welche Form schreiben wir auf", sondern „**welche der zwei gebauten Formen zieht nach**" —
das Release-Werkzeug auf `ui/`, oder der Controller auf ein Wurzel-tar (dann braucht `shared/`
die zweite Auspack-Funktion, und das ist der teure Weg, gegen den P3 oben argumentiert).

---

## P4 — ERLEDIGT, gestrichen

**Die Vorlage ist eingetragen.** `shared/ulh.md:175–201` enthält sie vollständig
(`SSLProxy*`-Zeilen, `<Proxy "balancer://ngc-api">`, zwei `BalancerMember`,
`ProxySet`, `ProxyPass`/`ProxyPassReverse`), dazu den Satz:

> „Die genaue Form ist diese Vorlage; `ngc-web` und der Controller schreiben genau sie,
> Zeile für Zeile. … Es gibt keine andere erlaubte Abweichung."

**Belegt:** `git log -- shared/ulh.md` → `7c9c405` „Write the balancer template into the
ulh contract". Der Riegel dazu läuft:
`TestBalancerTemplateMatchesTheContract`,
`apps/server-controller/classes/apache/apache_test.go:490` — er vergleicht die Vorlage
**byteweise** mit dem, was der Controller schreibt.

**Begründung der Streichung:** der Posten war „die Vorlage fehlt". Sie fehlt nicht mehr,
und `shared/ulh.md` war ohnehin die einzige gewährte Ausnahme vom Einfrieren. **Kein
v1.3-Posten.** Schritt 6 der Controller-Aufgabe kann laufen.

---

## P5 — Das Referenzfeld in `dbapi.App` fehlt

**Gilt.** Nachgemessen am 15.10.2026: `shared/dbapi/dbapi.go:83`, `type App struct` — die
Felder sind ID, Version, Store, Node, Active, Manifest, SHA256, Grants, KeptData, Updated.
**Kein Feld für die Abbild-Referenz.**

Die vier Zeilen zur Folge sind aus Punkt 24 übernommen (Controller 16, 10.10.2026) und
**von mir nicht nachgemessen**: `user.go:188`, `podman.go:438`, `user.go:258`,
`data.go:227`.

### Der Änderungssatz

`shared/dbapi/dbapi.go`, in `type App struct`, hinter `SHA256`:

```go
	// Image is the reference of the container image this app was built from,
	// as the package's tar named it. The controller writes it at install and
	// reads it to remove the image: Engine.RemoveImage needs a reference, and
	// an update without services brings no tar to take one from. Empty for an
	// app with no image (only kept data remains).
	Image string
```

### Der Preis

- **Das bricht den Vertrag.** Jede Umsetzung von `dbapi` muss das Feld tragen, sonst steht
  es morgen nur in einer. **Neue Fassung nötig.**
- Mitziehen müssen: **`memstore` und `sqlstore`** (db-Strang), **eine Zeile in `dbtest`**
  (sonst prüft niemand, dass beide es tragen), **eine nummerierte Migration** in
  `apps/server-db/sql/`, und der **Controller** schreibt beim Einbauen und liest beim
  Entfernen.
- **Für einen Nutzer, der schon installiert hat: das ist der einzige Punkt mit echtem
  Preis.** Eine Migration läuft über seine Datenbank, und für Apps, die vorher eingebaut
  wurden, bleibt das Feld **leer** — deren Abbilder bleiben also weiter liegen. Die
  Migration repariert die Vergangenheit nicht, sie macht nur die Zukunft buchführbar.
  Das gehört in die Release-Notiz.
- `dbapi.App` ist eine **interne** Go-Struktur ohne JSON-Namen. P10 gilt dafür **nicht**
  — ein neues Feld hier geht keinem Leser am Draht vor die Füße.

### Ohne die Änderung

- Einbauen, Updaten und Deinstallieren gehen. Niemand verliert Daten.
- Für immer fehlt **zweierlei**:
  1. Mit `KeepData` beim Deinstallieren bleibt ein Abbild **für immer** im
     Datenverzeichnis liegen, und keine Buchführung weiß davon.
  2. **Das wiegt schwerer:** `DataSize` zeigt dem Verwalter die Abbild-Schichten **als
     eigene Daten der App**. Er entscheidet über das Löschen anhand einer Zahl, die
     etwas anderes zählt, als ihr Name sagt — bei einem Gigabyte Abbild um
     Größenordnungen daneben.

**Empfehlung: JA, aber als letzter der fünf** — er ist der einzige, der die Datenbank und
einen bestehenden Nutzer anfasst, und er eilt am wenigsten (es geht um Platz und eine
Zahl, nicht um einen Weg, der versperrt ist).

---

## P6 — Ein Kopplungscode gilt 24 Stunden

**Gilt.** Nachgemessen am 15.10.2026: `shared/tunnel/tunnel.go:35`:

```go
	PairLifetime   = 86400
```

Dazu zwei Felder, die sich darauf berufen (`tunnel.go:86` und `:107`:
„one use, valid for PairLifetime").

Die Kommentare, die das Lesen von der Tastatur mit „dreißig Sekunden" begründeten
(`node/ask.go`, `node/main.go`), sind berichtigt. Die 30 Sekunden sind `pairing.Timeout`
und begrenzen nur die **HTTP-Anfrage**. **Das Fenster ist 2880-mal so groß, wie die
Begründung sagte.**

### Der Änderungssatz

`shared/tunnel/tunnel.go:35` — aus

```go
	PairLifetime   = 86400
```

wird

```go
	// PairLifetime is how long a pair or join code stays usable, in seconds.
	// One hour: long enough to set a server up with an interruption, short
	// enough that a code left in a terminal log or a chat window is worth
	// nothing the next morning. The HTTP request itself has its own, much
	// shorter limit (pairing.Timeout); this is the life of the code.
	PairLifetime   = 3600
```

### Der Preis

- **Das bricht den Vertrag** — eine Zusage über eine Gültigkeit wird kürzer.
  **Neue Fassung nötig.**
- Mitziehen muss: niemand im Code. Die Zahl steht an einer Stelle.
- **Was es einen Nutzer kostet:** wer einen Code holt und das Einrichten länger als eine
  Stunde liegen lässt, holt einen neuen. Das ist der ganze Preis — und es ist spürbar,
  deshalb steht es hier.
- **Nachzuziehen:** jeder Text, der dem Nutzer eine Gültigkeit nennt, und die `ulh`-Skripte,
  falls sie eine Zahl nennen.

### Ohne die Änderung

- Das Koppeln geht. Niemand ist ausgesperrt.
- Für immer bleibt: 24 Stunden sind lang genug, dass ein Code in einem Terminalprotokoll,
  einem Chatfenster oder einer Notiz **liegen bleibt** und bis dahin brauchbar ist. Dazu
  N1 (der Einmal-Wert wird **vor** der MAC-Prüfung verbraucht): wer die Adresse kennt,
  kann einen wartenden Code also auch ohne ihn verbrennen — ein langes Fenster ist darum
  nicht nur eine lange Chance zum Mitlesen, sondern auch eine lange Zeit, in der jemand
  das Einrichten stören kann.
- **Gemessen ist keine der beiden Zahlen.** Es ist eine Abwägung.

**Empfehlung: JA, eine Stunde** — bei Sicherheit gegen Bequemlichkeit die sicherere
Variante, und eine Stunde deckt ein Einrichten mit Unterbrechung noch ab.

---

## P7 — `ngc-api migrate` kann einen reinen Datenbankknoten nicht bedienen

**Gilt.** Nachgemessen am 15.10.2026, der ganze Weg:

`apps/server-api/main.go:150` `migrate()` → `:121` `load()` → `config.LoadAPI` →
`parseAPI` → `(*API).Validate`. Und dort, **`shared/config/config.go:533`**:

```go
	if !wire.ValidName(a.Node) ||
		!trust.ValidTunnelURL(a.URL) ||
		a.Listen == "" || len(a.Listen) > MaxField ||
		a.Cert == "" || len(a.Cert) > MaxField ||
		a.CertKey == "" || len(a.CertKey) > MaxField ||
		len(a.NoiseKey.Bytes()) != 32 || … len(a.ChallengeKey.Bytes()) != 32 ||
		len(a.ReleaseKey) != 32 || len(a.StoreKey) != 32 ||
```

`ngc-api migrate` verlangt damit eine **vollständige** `api.json`. Ein reiner
Datenbankknoten hat die nicht — er hat `controller.json` mit einem Feld `Migrate *DB`,
und **dieses Feld hat bis heute keinen Leser**: nachgesehen, es kommt nur in
`config.go:676`, `:679`, `:680` (Validate) und `:748`, `:749` (LogValue) vor.

### Der Änderungssatz

`shared/config/config.go`, neben `LoadAPI`, eine zweite Lesefunktion:

```go
// LoadMigrate reads the database part of an API configuration for the migrate
// subcommand alone: a db node has no api.json of its own, it has the Migrate
// field of its controller.json, and nothing of the tunnel, the listen address
// or the keys exists there. It checks DB and Node and nothing else, so that
// (*API).Validate stays as strict as it is for everyone who serves.
func LoadMigrate(path string) (*DB, string, error) {
	// read the file like LoadAPI does, then:
	//   if !wire.ValidName(node) { return nil, "", ErrConfig }
	//   if err := db.Validate(); err != nil { return nil, "", err }
}
```

und in `apps/server-api/main.go` ruft `migrate()` **diese** statt `load()`.

### Der Preis

- **Ergänzt nur, bricht nicht.** `(*API).Validate` bleibt unverändert streng. Das ist der
  Grund, diesen Weg zu nehmen und **nicht** `Validate` zu lockern: jede Antwort auf „was
  reicht fürs Migrieren" lockert sie sonst **für alle**, auch für den, der bedient.
- Mitziehen müssen: `apps/server-api/main.go` (eine Zeile im `migrate`-Zweig), der
  **Controller** (er legt dem db-Knoten die Datei bereit und startet das Programm), und
  die `ulh`-Skripte für die Rolle `db`.
- **`Controller.Migrate` wird damit endlich ein gelesenes Feld.**
- Für einen Nutzer, der schon installiert hat: **nichts.** Heute läuft dieser Weg nicht,
  es kann also keiner darauf gebaut haben.

**Ausdrücklich nicht:** eine zweite Konfigurationsform. Das wäre eine zweite Wahrheit über
die Zugangsdaten der Datenbank.

### Ohne die Änderung

- `ngc-api migrate` **läuft** — gemessen durch den Strang: „1 Migration eingespielt.",
  wiederholbar „0 Migrationen eingespielt." Nur auf einem **API-Server** mit voller
  `api.json`.
- Für immer fehlt: der Weg, auf dem die Migrationen einer neuen Fassung **auf dem
  db-Knoten** eingespielt werden — also genau dort, wo der Controller die Datei
  bereitlegt und wo Punkt 28 sie haben wollte. Die Entscheidung von Punkt 28 (ein Läufer,
  als Programm gestartet) bleibt richtig; **ausführbar ist sie so nicht.**

**Empfehlung: JA, über `LoadMigrate`** — die sicherere Variante, weil sie die strenge
Prüfung für jeden, der bedient, unangetastet lässt.

---

## P8 — Die Leerlaufgrenze des Terminals ist von einem Absturz nicht zu unterscheiden

**Gilt — und der Preis ist höher als in der alten Reihe.**

Nachgemessen am 15.10.2026:

- `terminal.IdleLimit = 30 * time.Minute`,
  `apps/server-controller/classes/terminal/terminal.go:102`.
- In `shared/wire/wire.go:450–469` steht die Code-Liste. **Kein Code für Leerlauf.**
- `Close` (`wire.go:493`) hat `Status`, `Code`, `Message`, `ExitCode` — der Dienst endet
  mit Code 1, und mehr kommt oben nicht an.

**Die alte Reihe sagt „Ergänzung, kein Bruch". Das ist gemessen falsch** — siehe P10:
ein alter Leser **wirft den Rahmen weg** (`wire: malformed CLOSE payload`), er sieht
nicht „weiter einen Schluss".

### Der Änderungssatz

`shared/wire/wire.go`, in der Code-Liste (nach `CodeLocked`, `:469`):

```go
	// CodeIdle ends a stream that neither took input nor gave output for its
	// idle limit (terminal.IdleLimit). It is not an error of the service: the
	// far side says "you were away", not "something broke".
	CodeIdle Code = "E_IDLE"
```

und in `messages` (`:471` ff.):

```go
	CodeIdle:        "Wegen Leerlaufs beendet.",
```

### Der Preis

- **Das bricht den Vertrag — es sei denn, P10 ist vorher drin.** Mit P10 ist es eine
  Ergänzung: ein alter Leser liest `E_INTERNAL` und sagt wenigstens etwas. Ohne P10
  müssen Controller, API und Weboberfläche **gleichzeitig** aktualisiert werden, und das
  kann NGC nicht zusagen (drei einzeln aktualisierte Komponenten,
  `tunnel.go:164`).
- Mitziehen müssen: der **Controller** schickt ihn (eine Zeile), die **Weboberfläche** sagt
  den eigenen Satz (eine Zeile) und braucht den Code in ihrer eigenen Tabelle
  (`contracts/wire.js`, Zweig `web`). **Beide Seiten sind gebaut und warten nur auf den
  Code.**
- Für einen Nutzer, der schon installiert hat: **nichts** bei vorhandenem P10; **ein
  abgewiesener Rahmen statt eines Schlusses** ohne P10.

### Ohne die Änderung

- Das Terminal geht. Die Grenze greift. Niemand verliert Daten.
- Für immer fehlt: „Du warst zu lange weg" und „etwas ist kaputt" bleiben **dieselbe
  Auskunft**. Der Benutzer liest „Die Verbindung wurde abgeschnitten." und sucht nach
  einem Fehler, den es nicht gibt. Das trifft jeden, der einmal Kaffee holt.

**Empfehlung: JA, aber nur zusammen mit P10** — allein ist es ein Bruch für eine
Bequemlichkeit, mit P10 ist es die billigste Verbesserung der ganzen Liste.

---

## P9 — „beschäftigt" braucht einen eigenen Code

**Gilt. Die Umgehung in `apps/` ist gebaut, der Code fehlt.**

Nachgemessen am 15.10.2026:

- `shared/wire/wire.go:450–469`, Code-Liste: **kein `E_BUSY`**. Sonde:
  `Code("E_BUSY").Message() = ""`, `CodeUnreachable.Message() = "Dienst nicht
  erreichbar."`
- `shared/dbapi/dbapi.go:108–112`, die Wächter: `ErrNotFound`, `ErrUsed`, `ErrInvalid`
  — **kein vierter.**
- `apps/server-api/classes/state/state.go:32`: `ErrBusy = errors.New("state: another API
  is at this step, try again")`, und `apps/server-api/classes/httpapi/fault.go:84` bildet
  ihn weiter auf **`wire.CodeInternal`** (500) ab. Der Kommentar dort
  (`fault.go:89–94`) beruft sich darauf, dass **eine** API auf einer Datei liege — und
  Punkt 50 hat gemessen, dass **zwei** darauf liegen.

**Was in `apps/` schon da ist** (also nicht mehr zu bauen):
`apps/server-api/classes/httpapi/busy.go:73` setzt `Retry-After` und protokolliert
`reason=db-busy`, `:77` `reason=db-unreachable`.

**Die Lücke ist genau benannt, und sie ist gemessen:** das reicht einem Klienten, der den
**Header** liest. Ein Klient, der den **Code** liest, kann „besetzt" nicht von „weg"
trennen — beide heißen `E_UNREACHABLE` (503). Der Verlierer wartet bis **80,6 s**
(Punkt 48, zwei echte Prozesse) und hört dann dasselbe Wort wie bei einer verschwundenen
Datenbank.

### Der Änderungssatz, drei Teile

**1. `shared/wire/wire.go`**, in der Code-Liste:

```go
	// CodeBusy answers a request the installation could not carry out right
	// now because another program holds the database: waiting a moment and
	// asking again helps, and nothing is broken. The HTTP answer says in
	// Retry-After when to try again. E_UNREACHABLE keeps its own meaning:
	// the database is away, and asking again brings the same answer.
	CodeBusy Code = "E_BUSY"
```

und in `messages`:

```go
	CodeBusy:        "Gerade belegt. Bitte in einem Moment erneut versuchen.",
```

**2. `shared/dbapi/dbapi.go`**, im `var`-Block bei `:108`:

```go
	// ErrBusy reports that another program holds the database for this step.
	// It is its own error, never ErrInvalid: the call was right, and asking
	// again in a moment helps. A store that cannot tell busy from away
	// returns ErrNotFound or its own error, never this one.
	ErrBusy = errors.New("dbapi: database busy")
```

**3. `apps/server-api/classes/httpapi/fault.go:84`** — `state.ErrBusy` wird aus dem
`CodeInternal`-Zweig herausgenommen und bekommt seinen eigenen:

```go
	case errors.Is(err, state.ErrBusy), errors.Is(err, dbapi.ErrBusy):
		return wire.CodeBusy
```

### Der Preis

- **Das bricht den Vertrag — es sei denn, P10 ist vorher drin.** Dieselbe Rechnung wie bei
  P8, mit einem Unterschied: der **HTTP**-Weg ist weniger empfindlich (der Körper ist
  nicht `ParseClose`), der **Kanal**-Weg ist es genauso. Die Weboberfläche hat ihre eigene
  Tabelle und nimmt den `message` der Antwort **nie** (`contracts/wire.js:505`) — ohne
  einen Eintrag dort liest der Benutzer weiter „Dienst nicht erreichbar."
- Mitziehen müssen: **`memstore` und `sqlstore`** (wer gibt `ErrBusy` zurück?), **`dbtest`**
  (sonst gibt ihn morgen nur einer), die **Weboberfläche** (Tabelle + Satz),
  und `state.go` bleibt wie es ist — nur seine Abbildung ändert sich.
- Für einen Nutzer, der schon installiert hat: **nichts** bei vorhandenem P10.

### Ohne die Änderung

- Es geht alles. `Retry-After: 5` und der Protokollgrund sind gebaut und wirken heute.
- Für immer fehlt, und das ist gemessen: **ein Klient, der nur den Code liest, kann
  „besetzt" nicht von „weg" trennen.** Er darf nach `E_UNREACHABLE` nicht wiederholen —
  also wiederholt er nicht, obwohl eine Wiederholung in einem Moment durchgegangen wäre.
  Dazu bleibt `state.ErrBusy` bei **500**, wo der Kommentar selbst sagt, dass die
  Begründung (eine API je Datei) nicht mehr stimmt.

**Empfehlung: JA, aber nur zusammen mit P10, und nach P8** (P8 ist derselbe Eingriff an
derselben Liste, aber ohne Nachzug in Datenbank und Oberfläche — wer beide will, macht P8
als Probe und P9 hinterher).

---

## P11 — `store.ValidPath` sagt nichts über die Schreibweise (neu, aus Punkt 23)

**Warum neu:** Punkt 23 (10.10.2026) sagt ausdrücklich, das gehöre „in die nächste
Vertragsfassung". In der alten Reihe stand es nicht.

**Gemessen am 15.10.2026:** `shared/store/manifest.go:233`:

```go
// ValidPath reports whether p is a safe relative path: no leading slash, no
// "..", no hidden files, only letters, digits and . _ - (at most 200 bytes).
func ValidPath(p string) bool { return len(p) <= 200 && pathPattern.MatchString(p) }
```

Und in `shared/README.md` und `shared/store/*.go` kommt **kein Satz** über Groß- und
Kleinschreibung von **Pfaden** vor (gesucht; der einzige Treffer, `README.md:199`, gilt
für **Feldnamen**).

**Die Folge, aus Punkt 23 übernommen:** zwei Namen im Paket, die sich nur in der
Schreibweise unterscheiden, sind für `Walk` **zwei** Einträge. Auf einem faltenden
Dateisystem scheitert die zweite Datei an `O_EXCL` → `install.ErrDisk`; auf einem
case-sensitiven werden **beide angenommen**. Dieselbe Eingabe, zwei Ergebnisse.

### Der Änderungssatz

Ein Satz im Kommentarkopf von `ValidPath` und in `shared/README.md`:

```go
// ValidPath does NOT reject two names that differ only in case: that is a
// property of the file system, not of the path. A package whose names collide
// when case is folded is accepted here and installs on a case-sensitive file
// system; on a folding one the second file fails as a disk error. NGC targets
// Debian and Ubuntu, so this is said rather than enforced.
```

### Der Preis

- **Ergänzt nur** — es wird nichts geprüft, es wird etwas **gesagt**. Kein Verhalten
  ändert sich, kein Aufrufer, keine Umsetzung.
- Mitziehen muss: niemand.
- Für einen Nutzer: **nichts.**
- **Die Alternative wäre teuer:** `ValidPath` wirklich kollisionsfrei machen heißt, die
  Funktion kennt die anderen Namen des Pakets — sie bekommt heute **einen** Pfad. Das ist
  ein anderer Eingriff, und nichts verlangt ihn.

### Ohne die Änderung

- Es geht alles. Auf dem Ziel (Linux) ist nichts kaputt.
- Für immer fehlt: **niemand sagt es.** Wer ein Paket baut, weiß nicht, ob zwei Namen in
  verschiedener Schreibweise erlaubt sind — und es fällt erst dem auf, der auf einer
  Entwicklungsmaschine `ErrDisk` sieht und nach einem Plattenfehler sucht.

**Empfehlung: JA** — ein Satz, der nichts kostet und eine Zusage von einer
Nicht-Zusage trennt. Das ist genau die Lücke, die der Satz der „gemeinsamen Wurzel"
schließen soll.

---

## P12 — Nichts hält die Rechte des Katalogeintrags gegen das Manifest (neu, aus Punkt 31)

**Warum neu:** Punkt 31 (12.10.2026) ist in der alten Reihe **gar nicht aufgeführt** —
auch nicht unter „gehört nicht hierher". Es ist eine Sicherheitsfrage.

**Gemessen am 15.10.2026:** `store.Package.Needs` wird in `shared/` an genau einer Stelle
angefasst: `shared/store/catalog.go:260`, und dort nur durch `validNeeds` — also auf
**Form**. **Nichts** hält diesen Eintrag gegen `store.Manifest.Needs`
(`shared/store/manifest.go:49`), die Rechte im **Paket**.

**Gebaut wird der Container aus dem Manifest.** Daraus folgt der Satz, der zählt — und er
ist der Grund, aus dem ein Strang einen vorgeschlagenen Weg **verworfen** hat:

> Eine Rechteprüfung gegen die Katalogkopie wäre eine **Wand vor einer offenen Tür**.
> Verlangt das Manifest `root` und sagt der Eintrag nichts, wird `root` durchgewinkt —
> und die Prüfung davor meldet nichts, weil sie die falsche Kopie liest.

### Der Änderungssatz, zwei Möglichkeiten

**(a) Prüfen** — in `shared/store`, dort wo das Manifest zum Eintrag kommt:

```go
// NeedsMatch reports whether the manifest of a package and its catalog entry
// name the same needs. The container is built from the manifest, so a check
// against the entry alone is a wall in front of an open door: this is the one
// place that holds the two copies against each other, and install refuses a
// package whose manifest asks for more than its entry says.
func NeedsMatch(m *Manifest, p *Package) bool
```

**(b) Sagen** — ein Satz in `shared/README.md`:

> `Package.Needs` im Katalogeintrag ist eine **Anzeige für den Verwalter**, keine
> Zusicherung. Verbindlich sind die Rechte im **Manifest**: daraus wird der Container
> gebaut, und `store.Granted` prüft **sie** gegen die Genehmigung. Wer gegen den
> Katalogeintrag prüft, prüft die falsche Kopie.

### Der Preis

- **(a) bricht** — ein Paket, das heute durchgeht, geht danach nicht mehr durch (jedes,
  dessen Eintrag und Manifest auseinandergehen). **Neue Fassung nötig**, und
  mitziehen müssen `install.Accept` und das Werkzeug, das Kataloge baut.
- **(b) ergänzt nur** und kostet niemanden etwas.
- Für einen Nutzer, der schon installiert hat: bei (a) kann ein **Update** abgewiesen
  werden, das vorher gelaufen wäre — das ist ein echter Preis und gehört in die
  Release-Notiz. Bei (b): nichts.

### Ohne die Änderung

- Es geht alles, und **die Prüfung, die zählt, greift**: `store.Granted` prüft die Rechte
  des **Manifests** gegen die Genehmigung des Verwalters. Es ist **kein offenes Loch.**
- Für immer fehlt: der Satz, der den nächsten davon abhält, die **Katalogkopie** zu
  prüfen und sich sicher zu fühlen. Genau das wurde einmal vorgeschlagen und nur deshalb
  verworfen, weil ein Strang es gemessen hat.

**Empfehlung: JA zu (b), NEIN zu (a)** — die Zusage fehlt, nicht die Prüfung. (a) würde
einen Riegel an eine Kopie hängen, die nicht die Wahrheit ist; das ist die unsicherere
Variante, obwohl sie strenger aussieht.

---

## P13 — Kein Prozessdeckel je App (neu, aus Punkt 51)

**Warum neu:** Punkt 51 (13.10.2026) merkt es ausdrücklich vor
(„eine App-eigene Prozessgrenze wäre ein Manifestfeld,
`shared/store/manifest.go:50–51` — eingefroren") und nennt es **keinen Blocker**. In der
alten Reihe steht es nicht.

**Gemessen am 15.10.2026:** `shared/store/manifest.go:50–51`:

```go
	CPU         int       `json:"cpu,omitempty"`     // thousandths of a core, default DefaultCPU
	Memory      int       `json:"memory,omitempty"`  // MiB, default DefaultMemory
```

Zwei Deckel je App — **für Prozesse keiner.** Der Controller setzt stattdessen einen
festen: `--pids-limit 512` für jede App
(`apps/server-controller/classes/runtime/podman.go`, gemessene Zahl: ein Go-Programm in
der Form des hold-Prozesses hält 24 OS-Threads, Faktor 21).

### Der Änderungssatz

`shared/store/manifest.go`, hinter `Memory`:

```go
	Pids        int       `json:"pids,omitempty"`    // processes (threads, as the cgroup counts), default DefaultPids
```

dazu `DefaultPids = 512` neben `DefaultCPU`/`DefaultMemory`, und das Feld wird in
`Manifest.Validate` wie die anderen zwei auf einen Bereich geprüft.

**P10 gilt hier:** `Manifest` ist **Vertrags-JSON**, und `shared/README.md:199–200` lehnt
unbekannte Felder ab. Ein Paket mit `"pids"` wird von jedem älteren NGC **abgewiesen** —
dafür gibt es die Regel in `manifest.go:52–57`: ein Feld, das Fassung X einführt, darf
nur in Paketen stehen, deren `minNgc` mindestens X ist. **Diese Regel gibt es, sie
funktioniert, und sie ist der Grund, dass P13 anders als P8/P9 kein Problem hat.**

### Der Preis

- **Ergänzt nur** — ein Paket ohne `pids` verhält sich wie heute (`DefaultPids = 512`).
- Mitziehen müssen: der **Controller** (statt der festen Zahl das Feld lesen), das
  Werkzeug, das Manifeste baut, und die Oberfläche, wenn sie die Grenze anzeigt.
- Für einen Nutzer, der schon installiert hat: **nichts.** Jede bestehende App bekommt
  weiter 512.
- **Der eigentliche Preis ist eine Frage:** eine App, die mehr als 512 verlangt, verlangt
  etwas, das der Verwalter heute nicht sieht und nicht genehmigt. Dann gehört das Feld in
  die Genehmigungsmaske, oder es gehört begrenzt (etwa 4096).

### Ohne die Änderung

- Es geht alles. **512 ist gemessen und steht im Repo**, und eine Fork-Bombe stoppt jede
  endliche Zahl.
- Für immer fehlt: eine App, die legitim mehr Prozesse braucht (ein Build-Dienst, eine
  Datenbank mit vielen Fäden), **kann es nicht sagen** und scheitert ohne erkennbaren
  Grund an einer Zahl, die nicht in ihrem Manifest steht.
- **Ungemessen und es gehört dazu:** ob überhaupt eine App an 512 anstößt. Das ist nicht
  gemessen; es ist eine Vermutung.

**Empfehlung: NEIN für V1.3, später JA** — es ist der einzige Punkt der Liste, dessen
Bedarf **nicht gemessen** ist. Ein Feld auf Vorrat ist genau das, was `code.md` mit
YAGNI verbietet. Wer es will, misst zuerst eine App, die anstößt.

---

## Was ich geprüft und NICHT aufgenommen habe

**Die Zwei-Programm-Falle unter Windows** (`shared/config/config.go:572` +
`apps/server-db/classes/sqlite/sqlite.go:201`). Beide Zeilen stehen da, nachgemessen:

```go
// config.go:572
if !strings.HasPrefix(d.Path, "/") || … {

// sqlite.go:201
if !filepath.IsAbs(path) || len(path) > config.MaxField || path != filepath.Clean(path) {
```

Zusammen erfüllt **kein Windows-Pfad beides** — jede Zwei-Programm-Messung muss nach
WSL/ext4 (so steht es in Punkt 50).

**Entscheidung: Umgebungsfalle, kein v1.3-Posten.** Begründung:

- Beide Zeilen sind **einzeln richtig** und machen zusammen genau das, was der Vertrag
  will: der Pfad einer Datenbankdatei ist ein absoluter POSIX-Pfad. NGC läuft auf Debian
  und Ubuntu; `CLAUDE.md` sagt „Windows wird nicht umgesetzt".
- Eine Änderung würde nicht einen Mangel beheben, sondern eine **Zusage lockern**, damit
  eine Messung bequemer wird. Das ist Bequemlichkeit gegen Sicherheit, und die Regel sagt,
  welche Seite gewinnt.
- **Der Preis ist bekannt und tragbar:** zwei Zeilen in `messen.md` („wer eine Messung
  auf eine andere Plattform verlegt, nennt die Zeilen, die das erzwingen"). Das ist
  gebucht, nicht offen.
- **Was damit für immer fehlt**, und es ist echt: zwei echte `ngc-api` lassen sich auf
  C: und auf Z: nie messen. Jede Zahl dieser Klasse gilt für WSL/ext4. **Das ist eine
  Lücke in der Messbarkeit, kein Loch im Produkt** — und darum gehört es in `messen.md`,
  nicht in V1.3.

**Die Obergrenze gleichzeitiger Terminals** (Punkt 29 nennt sie als offen). **Kein
Posten:** die Zahl **steht** im Vertrag — `shared/wire/wire.go:149`,
`MaxTunnelChannels = 1024`, und die 80 MiB aus Punkt 29 sind genau für diese 1024
gezählt. Was fehlt, ist eine **Terminal**-Grenze unterhalb davon, und die gehört nach
`serve` (`apps/`), unter die bestehende Decke. Kein `shared/`-Eingriff.

---

## N1–N3: keine Verträge (nur umbenannt, Inhalt unverändert)

Diese drei standen in der alten Reihe als „P6/P7/P8" **ein zweites Mal** und haben die
Nummern doppelt belegt. Sie sind **keine Vertragsfragen** und brauchen kein JA oder NEIN
zu V1.3.

- **N1 — Einmal-Code vor der MAC** (`join.go:107`, `pair.go:79`). Eine Reihenfolge im
  eigenen Code, an **zwei** Stellen. Wer sie anfasst, fasst beide an. **Vorher zu messen
  und nicht gemessen:** wie oft ein Code neu ausgegeben wird. Hängt an **P6**.
- **N2 — zwei Server, ein Name** (`join.go:135`). Der öffentliche Schlüssel liegt in der
  Anfrage; `join.keep` kann vor dem `BlobPut` lesen und am Schlüssel entscheiden.
  `dbapi.Blob` reicht dafür. Aufgabe im api-Strang.
- **N3 — MariaDB-Hälfte von W-1** (`already.go:50–68`). `knowsItsSchema` sitzt in
  `apps/`, und der Kommentar sagt, dass es **absichtlich nicht** Teil von
  `sqlstore.Dialect` ist. Zwei Wächter halten die Lücke in Form.

---

## Die Reihenfolge

### Was zusammen entschieden werden muss

| Bündel | Punkte | Warum |
|---|---|---|
| **A** | **P10 → P8 → P9** | P10 ist das Tor. Ohne es ist jeder neue Code ein Bruch. P9 zieht `shared/dbapi`, `memstore`, `sqlstore`, `dbtest` und die Oberfläche mit; P8 nicht. |
| **B** | **P2** allein | Eine Zeile, zwei Nachzieher in `apps/`, zwei Wächter umdrehen. Hängt an nichts. |
| **C** | **P1 + P12(b) + P11** | Alle drei sind „die Zusage fehlt, nicht die Prüfung". Drei Sätze und eine Umbenennung, kein Bruch. In einem Zug billiger als einzeln. |
| **D** | **P7** allein | Eigene Lesefunktion, berührt nur `migrate`. Hängt an nichts. |
| **E** | **P6** allein | Eine Zahl. Hängt an nichts im Code — aber an **N1**: wer sie entscheidet, soll N1 danebenlegen. |
| **F** | **P5** allein | Der einzige mit Migration und bestehendem Nutzer. Zuletzt. |

**Einzeln entscheidbar:** P1, P2, P5, P6, P7, P11, P12.
**Nicht einzeln:** P8 und P9 — beide nur nach P10.
**Gestrichen:** P4 — **erledigt**, es gibt nichts mehr zu entscheiden.
**Zurückgestellt:** P13 — der Punkt steht, nur sein **Bedarf ist nicht gemessen**;
er kommt wieder, sobald eine App an den 512 anstößt.

### Die drei dringendsten

1. **P10** — weil es das Tor ist: solange `ParseClose` einen unbekannten Code als
   kaputten Rahmen behandelt (gemessen: `wire: malformed CLOSE payload`), ist **jede**
   künftige Ergänzung ein Bruch, und P8 wie P9 kosten das Dreifache.
2. **P2** — weil es der einzige Punkt ist, der einen **Weg versperrt**, den es geben
   soll: eine Erstinstallation ohne Aufsicht geht gar nicht, und der eigene Kommentar
   behauptet das Gegenteil. Der Ersatz dafür ist schon **zweimal** von Hand gebaut
   (`install.go:353`, `rollout.go:253`) — die sechste Stelle kommt sonst von selbst.
3. **P3** — weil es **jetzt noch gratis** ist: `release-packer` existiert nicht, der
   zweite Schreiber ist nicht gebaut. Nach ihm ist dieselbe Entscheidung ein Umbau, und
   bis dahin entpackt der Controller nach einer Annahme, die nirgends steht.

---

## Nachtrag 20.10.2026: P14 und P15 kommen aus Punkt 68

Beide stammen aus dem Portierversuch zu `docs/vertraege-v1.2.md`, Punkt 68 — ein fremdes
Rust-Programm als NGC-App. **Die Entscheidungstabelle oben kennt sie noch nicht.**

### P14 — Eine Oberfläche darf 16 MiB, ein Programm 4 GiB

`MaxUIFile = 16 << 20` und `MaxUITotal = 64 << 20` (`shared/store/pack.go:22-23`) gegen
`MaxPackage = 4 << 30` (`shared/store/catalog.go:25`). Das Verhältnis ist 1:256, und es ist
nicht begründet: beide Zahlen stehen ohne Kommentar zu ihrer Höhe.

**Gemessen:** Wordcrafts WebAssembly-Bau ist **44 027 637 Bytes** — 2,6-mal über der
Dateigrenze, und als `data:`-Adresse (base64, +33 %) über die Gesamtgrenze von 64 MiB. Kein
Programm dieser Größenklasse kann je eine NGC-Oberfläche haben, auch wenn jeder andere
Blocker fiele.

**Der Änderungssatz:** `MaxUIFile` und `MaxUITotal` anheben — oder ausdrücklich
nicht, mit einem Satz, der sagt warum. Betroffen ist auch die JavaScript-Seite
(`apps/web/public/assets/js/contracts/uitar.js:20-24`) und das Zwischenlager der Shell
(`appframe.js:49-50`, `CACHE_BYTES = MAX_UI_TOTAL`): eine höhere Grenze heißt ein größeres
Zwischenlager im Browser, bei acht offenen Apps achtmal.

**Der Preis:** die Shell hält das ui-tar vollständig im Speicher und hasht es in einem Zug
(`appframe.js:656-668`). 16 MiB sind dort eine bewusst kleine Zahl; 64 MiB je Datei wären
eine ganz andere Last auf einem schwachen Gerät. Das ist die eigentliche Frage, nicht die
Zahl selbst.

**Ohne die Änderung:** NGC-Oberflächen bleiben handgeschrieben und klein. Das ist ein
vertretbarer Zustand — er muss nur dastehen, damit niemand ein halbes Jahr in eine
Portierung steckt, die an einer Zahl ohne Begründung endet.

### P15 — Ein Stream hat keine Nachrichtengrenzen, und jede App erfindet ihre eigenen

`serve` gibt den Klartext eines Kanals als **Byte-Strom** an das Dienstprogramm durch
(`apps/server-controller/classes/serve/call.go:193-197`); wo eine Nachricht aufhört, sagt
nichts. Das Terminal hat sich dafür einen eigenen Kopf aus drei Bytes erfunden —
Art und Länge, `KindKeys` = 1, `KindResize` = 2 (`classes/terminal/terminal.go:76-96`).

**Der Befund:** das ist die zweite Hälfte eines Vertrags, die in einer App liegt. Jede
weitere App, die über einen Stream mehr als einen Byte-Brei schickt, erfindet denselben Kopf
neu — mit eigener Länge, eigener Bytefolge und eigenen Fehlern. Beim Portierversuch stand
genau diese Entscheidung wieder an.

**Der Änderungssatz:** ein Rahmen mit Längenkopf in `shared/`, einmal, mit Vektoren in
`shared/testdata` wie jeder andere Vertrag — und das Terminal zieht darauf um.

**Der Preis:** eine Vertragsdatei mehr und eine Umstellung im Terminal. Die JavaScript-Seite
braucht das Gegenstück, also eine zweite Umsetzung — der Preis aus Punkt 67, hier mit
Ansage.

**Ohne die Änderung:** es bleibt bei „jede App ihr eigenes Protokoll". Bei zwei Apps ist das
nichts; bei zehn ist es zehn Stellen, an denen eine Längenprüfung fehlen kann.
