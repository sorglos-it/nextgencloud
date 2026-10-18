# Bis zur Veröffentlichung — was fehlt, bis ein Fremder NGC benutzt

**Geschrieben am 18.10.2026.** Diese Datei ändert nichts: kein Code, kein `shared/`,
und `docs/vertraege-v1.2.md`, `docs/STAND.md`, `docs/vertraege-v1-3-vorschlag.md`
bleiben unberührt. Sie gehören dem Koordinator.

**Die Frage war:** seit dem 17.10.2026 läuft NGC nachweislich — Installation,
Anmeldung, Schreibtisch, Kopplung (Punkte 56–62). **Trotzdem könnte heute kein
Fremder es installieren.** Was genau fehlt?

**Wie gemessen wurde, damit jede Zahl einen Baum hat:**

- Arbeitsbaum `D:\claude\ngc-veroeffentlichung`, Zweig `veroeffentlichungsliste`,
  von `origin/main` **`d955f52`** („Point 63: the web interface is on main").
  Lokale Platte, nicht Z:.
- Baum vollständig: `git diff --name-only --diff-filter=D HEAD` = **0** Zeilen.
- Zweites Repo lesend: `Z:\GitHub\ulh`, `origin/main` **`096a655`**, nach `git fetch`.
- **Keine Zeile ohne Beleg.** Was ich nicht belegen konnte, steht in Abschnitt 5.

**Was sich seit der letzten Zählung geändert hat — zwei Berichtigungen gleich vorweg:**

1. **`apps/web` liegt jetzt auf `main`: 136 Dateien**, nicht 2. Punkt 62, Befund 1
   („wer nur `main` klont, kann kein Release bauen") ist **erledigt**, und
   `origin/web-alles` ist `ahead=0` — alles davon ist auf `main`.
2. **Die vierstellige Hausnummerierung ist kein Riegel mehr.** `shared/store/manifest.go:231`
   nimmt die vierte Stelle (`(\.(0|[1-9][0-9]{0,5}))?$`), `parseVersion` gibt `[4]int`
   (`:272`). Die Tabelle „Offene Entscheidungen" in `vertraege-v1.2.md` nennt das noch als
   blockierend — **das ist seit `d43a29d` überholt.** Bitte dort streichen. (Die fünf
   `apps/*/VERSION` sind weiter dreistellig; das ist kein Riegel, sondern Absicht.)

---

## 1. Die harten Sperren — ohne die geht es nachweislich nicht

Reihenfolge: was ein Fremder zuerst trifft.

### H1 — Das Repo ist privat. Niemand kann NGC überhaupt holen.

- **Was fehlt:** `gh repo view sorglos-it/nextgencloud --json visibility` gibt
  **`"PRIVATE"`**. Die `README.md` nennt NGC „an open alternative to Synology's DSM" und
  AGPL-3.0, und niemand außer Thomas kann es klonen.
- **Warum es zählt:** das ist die erste Sperre von allen. Alles andere in dieser Liste ist
  erst danach ein Problem.
- **Größe:** **offene Entscheidung von Thomas** (ein Klick, sobald entschieden).
- **Fundstelle:** `gh repo view`, 18.10.2026; `README.md`, Zeile 5 und Abschnitt „License".

### H2 — Es gibt kein einziges veröffentlichtes Release, und die Skripte holen von genau dort.

- **Was fehlt:** `gh release list --repo sorglos-it/nextgencloud` gibt **null Zeilen**. Die
  Skripte holen aus
  `NGC_RELEASE_BASE="${NGC_RELEASE_BASE:-https://github.com/sorglos-it/nextgencloud/releases/latest/download}"`
  (`ngc-api.sh:81` auf ulh-`ngc-kette`). Diese Adresse hat nichts.
- **Warum es zählt:** `fetch_release` holt dort `release.json` und die Programmdateien. Ohne
  Release bleibt nur „von Hand nach `/var/lib/ngc/staged` legen" — und dieser Ordner wird von
  keinem Skript angelegt (Punkt 62, Befund 3). Ein Fremder müsste NGC also selbst bauen und
  selbst unterschreiben; genau das verlangt `docs/installation-kleines-system.md` heute als
  **Schritt 1**.
- **Größe:** 1 Tag (das Release-Werkzeug steht; D1–D4 aus `docs/release-weg.md` sind vorher
  zu entscheiden), **plus** die Entscheidung über den Schlüssel (H4).
- **Fundstelle:** `gh release list`, 18.10.2026; ulh `ngc-api.sh:81`;
  `docs/installation-kleines-system.md:58`; `vertraege-v1.2.md`, Punkt 62 Befund 3.

### H3 — Auf ulh-`main` liegt kein einziges `ngc-*`-Skript.

- **Was fehlt:** `git ls-tree -r --name-only origin/main apps/cli/assets/scripts/ | grep -i ngc`
  gibt **null Treffer** auf ulh-`main`, und `catalog.yaml` enthält **kein** „NextGenCloud".
  Alle fünf Skripte liegen auf Zweigen:

  | ulh-Zweig | `ngc-*`-Skripte | Commits vor `main` |
  |---|---:|---:|
  | `ngc-kette` | 5 (all, api, controller, db, web) | 8 |
  | `ngc-db-mariadb` | 5 | 13 |
  | `ngc-zusammenfuehrung` | 5 | 2 |
  | `ngc-all` | 3 | 4 |
  | `ngc-skripte` | 2 | 3 |
  | `ngc-api` | 1 | 1 |

- **Warum es zählt:** NGC wird ausschließlich mit ulh installiert (`shared/ulh.md`). Jede
  ulh-Installation holt beim Start `main`. Was dort nicht liegt, kann niemand aufrufen.
- **Größe:** **offene Entscheidung von Thomas** — 🚨 ein Push nach ulh-`main` erreicht per
  Auto-Update **sofort alle ulh-Nutzer**. Das ist ausdrücklich keine Agentenaufgabe. Davor
  ist zu klären, welcher der sechs Zweige der richtige ist (vier tragen alle fünf Skripte).
- **Fundstelle:** `git ls-tree`/`git rev-list --count` auf `Z:\GitHub\ulh`, 18.10.2026;
  `shared/ulh.md`, Abschnitt „Installation über ulh"; `vertraege-v1.2.md`, Tabelle
  „Blockierend".

### H4 — Der geheime Release-Schlüssel liegt als Datei auf der Messmaschine — und die Skripte tragen genau diesen Schlüssel.

- **Was fehlt:** der Hardware-Stick. Nachgerechnet, nicht angenommen: der öffentliche
  Schlüssel in `ngc-api.sh:88–90` ist
  `MCowBQYDK2VwAyEAG0sJt5p/tSyJB9yXaDT5Y5VWchsBAMDErZPY1h9sDAo=`; seine 32 rohen Byte
  ergeben die Kennung **`pN6Z6a9NR0aG6aEJIJ5EHg`** — das ist nach
  `docs/release-schluessel.md` der **dateibasierte** Schlüssel, `/root/ngc-keys/release.key`
  auf der Messmaschine. Der YubiKey-Schlüssel (`YWNJ9R-LilweZWb4Q4a1Vg`) steht in **keinem**
  Skript.
- **Warum es zählt:** wer auf dieser einen Maschine root ist, unterschreibt NGC-Releases für
  alle. Der Ausweg ist Punkt 19 (Unterschrift über `sha512(Message(...))`), und **Punkt 19
  ist nicht gebaut** — `shared/sign/sign.go` unterschreibt weiter die nackte Nachricht,
  darum kann die Karte überhaupt nicht unterschreiben.
- **Größe:** **offene Entscheidung von Thomas** (welcher der zwei Schlüssel der echte wird);
  wird es der Stick, kommt Punkt 19 dazu — Vertragsänderung in `shared/`, also nur im
  v1.3-Schnitt.
- **Fundstelle:** `docs/release-schluessel.md`, Berichtigungskasten vom 17.10.2026; ulh
  `ngc-api.sh:88`; eigene Nachrechnung der `sign.KeyID` am 18.10.2026.

### H4a — Nebenbefund dazu: der Kommentar über dem Schlüssel sagt das Gegenteil des Codes.

- **Was fehlt:** eine Berichtigung dieser drei Zeilen. `ngc-api.sh:85–87` schreibt: „The
  placeholder below is not a key, so every install stops at the check - which is the safe way
  round until the real key is in here." **Darunter steht ein echter, gültiger Schlüssel**
  (eben nachgerechnet). Es stoppt also **nichts**.
- **Warum es zählt:** wer das liest, hält die Lage für sicher, und sie ist es nicht: eine
  fremde Installation prüft ihre Releases heute gegen einen Schlüssel, dessen geheime Hälfte
  in `/root` auf Thomas' Testmaschine liegt. Es ist dieselbe Form, die in diesem Projekt am
  häufigsten auffällt — der Text behauptet das Gegenteil des Codes.
- **Größe:** Minuten (drei Zeilen Kommentar im ulh-Repo), aber sie hängt an H3.
- **Fundstelle:** ulh `origin/ngc-kette:apps/cli/assets/scripts/ngc-api.sh:85–90`, gelesen am
  18.10.2026; derselbe Block in `ngc-all.sh:165–170`, `ngc-web.sh`, `ngc-controller.sh`.

### H5 — Es gibt keinen offiziellen App-Store; der Platzhalter steht noch drin.

- **Was fehlt:** Adresse und öffentlicher Schlüssel eines NGC-Stores.
  `NGC_STORE_KEY_DEFAULT="REPLACE-THIS-LINE-WITH-THE-NGC-STORE-KEY"` (`ngc-api.sh:97`,
  ebenso `ngc-all.sh:177`).
- **Warum es zählt:** jede Installation muss `store_url` **und** `store_key` selbst nennen,
  sonst bricht das Skript ab (richtig so, Punkt 57). **Es gibt also keine Apps von NGC** —
  und NGC ist „ein Schreibtisch, auf dem Apps in Containern laufen".
- **Größe:** **offene Entscheidung von Thomas**; danach ein Store aufsetzen (mehrere Tage:
  Adresse, Schlüssel, Katalog).
- **Fundstelle:** ulh `ngc-api.sh:97`; `vertraege-v1.2.md`, Tabelle „Blockierend";
  `docs/installation-kleines-system.md:31`.

### H6 — Kein Fremder kann die Kette mit einem eigenen Schlüssel prüfen.

- **Was fehlt:** ein Weg, den Release-Schlüssel von außen zu setzen.
  `NGC_RELEASE_KEY="-----BEGIN PUBLIC KEY-----…"` ist eine **nackte Zuweisung** — anders als
  jede andere Antwort daneben, die `${NGC_…:-}` liest (`ngc-api.sh:103–113`).
- **Warum es zählt:** wer einen eigenen Schlüssel braucht, muss `sed` in vier Skripten
  laufen lassen — **und wer die Skripte verbiegt, prüft nicht mehr die Skripte.**
- **Größe:** 2–4 Stunden (`NGC_RELEASE_KEY_FILE`, nur gelesen wenn gesetzt und root gehörend;
  oder ein ausdrückliches `ngc-all.sh test-key <Datei>`).
- **Fundstelle:** `vertraege-v1.2.md`, Punkt 62 Befund 2 (**blockierend**); ulh
  `ngc-api.sh:88` und `:103–113`, am 18.10.2026 nachgesehen.

### H7 — Kein Fremder kann eine App installieren. `Updater` ist `nil`.

- **Was fehlt:** ein Updater im Controller. `apps/server-controller/classes/node/run.go:650`
  steht wörtlich `Updater: nil`, und der Kommentar darüber sagt selbst: „until then an install
  channel ends E_INTERNAL". `grep` auf `update.New|install.New|install.Deps` in `apps/` ohne
  Tests = **0 Treffer**.
- **Warum es zählt:** das ist nicht „eine Lücke", das ist der Zweck des Produkts. Ein Fremder
  installiert NGC, steht auf dem Schreibtisch — und **jede** App-Installation endet mit
  `E_INTERNAL`.
- **Größe:** 1–3 Tage, **und davor eine Entscheidung:** `update.Deps.List` nimmt die
  Vertrauensliste als **Wert**, während der Controller sie Fassung für Fassung austauscht
  (`chain.Keeper.Current`). Welche der zwei Seiten nachgibt, ist eine Änderung in
  `package update`.
- **Fundstelle:** `apps/server-controller/classes/node/run.go:640–651`, gelesen am
  18.10.2026; `docs/controller-abnahme.md`, am 17.10. nachgemessen und gültig.

### H8 — Eine Installation hat genau einen Benutzer. Für immer.

- **Was fehlt:** eine Aktion, die einen Benutzer anlegt. Der eingefrorene Vertrag kennt in
  `ngc.users` zehn Aktionen (`shared/core/core.go:44–53`): `list`, `rights`, `set-rights`,
  `trust`, `pair-code`, `join-code`, `pending`, `pending-api`, `nodes`, `log`. **Keine legt
  einen Benutzer an.** Der Weg über die Vertrauensliste gäbe es her — `ngc.users/trust` nimmt
  eine unterschriebene neue Fassung an (`coreapp/users.go:224`) —, aber die Oberfläche nutzt
  ihn nur für Server: `buildNextVersion` wird an **zwei** Stellen gerufen (`users.js:686`,
  `:719`), **beide** schreiben in `next.nodes`. **Nichts schreibt je in `next.users`.**
- **Warum es zählt:** NGC ist ein System für einen Haushalt oder ein Büro. Der erste
  Verwalter ist der einzige Mensch, den es je geben kann. Für einen Fremden ist das kein
  Mangel, sondern das Ende.
- **Größe:** 1–2 Tage Oberfläche. **Kein** Vertragsschnitt nötig, weil `ngc.users/trust` schon
  alles annimmt — das ist der gute Teil der Nachricht.
- **Fundstelle:** `shared/core/core.go:44–53`;
  `apps/web/public/assets/js/apps/users.js:686`, `:719`, `:752`;
  `apps/server-api/classes/coreapp/users.go:224`; alles am 18.10.2026 gelesen.

### H9 — Vier Sicherheitsbehebungen liegen auf Zweigen, die **nichts** enthält — auch `web-alles` nicht.

- **Was fehlt:** drei Zweige zusammenführen. `docs/befunde-17-18-10-2026.md` nennt diese
  Behebungen „behoben nur auf `web-alles`", und Punkt 63 nennt den Umzug von `web-alles` nach
  `main` erledigt. **Beides stimmt zusammen nicht.** Gemessen mit
  `git merge-base --is-ancestor <commit> HEAD` und `git branch -r --contains <commit>`:

  | Commit | Was | auf `main`? | liegt allein auf |
  |---|---|---|---|
  | `f4131cb` | „The admin who lists a server may run root tasks on it" | **NEIN** | `ngc-knotenfassung` |
  | `d9e7794` | „A node's version is read with the hello's rule, so the server list arrives" | **NEIN** | `ngc-knotenfassung` |
  | `2b64ecd` | „A package can be uploaded: the upload is signed and confirmed" | **NEIN** | `ngc-upload-bestaetigung` |
  | `2cd0e31` | „Let an admin issue a join code, and name a taken node name" | **NEIN** | `ngc-zweitemaschine-web` |

  Gegenprobe, die den Widerspruch auflöst:
  `git rev-list --left-right --count HEAD...origin/web-alles` = **104 0**. `web-alles` ist
  `ahead=0` — alles davon **ist** auf `main`. Die vier Commits waren also **nie in
  `web-alles`**; sie liegen auf drei Zweigen, die kein anderer Zweig enthält. Zur Gegenprobe
  am Code: `users.js:690` und `:722` schreiben auf `main` weiter
  `rootUsers: [], rootRoles: []` — genau das, was `f4131cb` behebt.
- **Warum es zählt:** Folge auf **jeder** Anlage: ein frisch gekoppelter Controller kann
  nichts, was Rootrechte braucht — aktivieren, installieren, deinstallieren, Daten löschen,
  Terminal (`E_FORBIDDEN`). **Der Server steht in der Liste und ist nutzlos.** Dazu: die
  Serverliste bleibt leer, weil `0.0.0-dev` die ganze Antwort verwirft; kein Paket ist
  hochladbar; eine zweite Maschine ist überhaupt nicht aufzusetzen.
- **Größe:** 2–4 Stunden. **Das ist die billigste Zeile dieser Liste und eine der
  schwersten.**
- **Fundstelle:** eigene Messung am 18.10.2026 im Baum `d955f52`;
  `docs/befunde-17-18-10-2026.md`, Abschnitte 0–2; `vertraege-v1.2.md`, Punkt 63.

> **ERLEDIGT am 18.10.2026, auf diesem Zweig.** Alle drei Zweige sind zusammengeführt;
> `git merge-base --is-ancestor origin/<zweig> HEAD` sagt für alle drei **ja**. Gegenprobe am
> Code: `users.js:943` schreibt jetzt `rootUsers: [this.session.user]`, `:982` lässt den
> API-Knoten bei `[]` (so will es `trust.ValidAPINode`), und `channel.js:552` nimmt
> `confirmBytes` an.
>
> **Die drei Kommentarbehauptungen sind gegen den Go-Code gelesen, nicht geglaubt** — und alle
> drei halten: `trust.ValidAPINode` verlangt `len(RootUsers)+len(RootRoles) == 0`
> (`trust.go:176`), `tunnel.versionPattern` ist `^[0-9a-z.+-]{1,32}$` (`tunnel.go:426`), und
> `MayRoot` gibt für jeden Knoten, der kein Controller ist, `false` (`trust.go:422`).
>
> **Ein Konflikt, und er war der lehrreichen Sorte:** `users.spec.js` — beide Seiten hängen
> **verschiedene neue Fälle an dieselbe Stelle**. Das ist kein Entweder-oder; **beide Blöcke
> stehen jetzt**, alle acht Fälle beider Seiten sind einzeln nachgewiesen, `node --check`
> grün.
>
> **Zwei Dinge, die der Auftrag nicht vorhersah:**
> 1. **`ngc-zweitemaschine-web` ist eine lebende Baustelle.** Beim ersten Messen war sie
>    1 Commit vor `main`, beim Zusammenführen 3: `0156cd8` („Let an admin change a listed
>    server's address or retire it", 15:20 Uhr) und `7be1f5e` (15:29 Uhr) sind **während
>    dieser Sitzung** entstanden. Mitgekommen ist damit **mehr als die vier Behebungen** —
>    nämlich ein neuer Befund samt Behebung: die Adresse eines eingetragenen Servers ließ sich
>    nie ändern, gemessen am 18.10.2026 an einer Zwei-Maschinen-Anlage, deren Server neue
>    Adressen bekommen hatten (Apache lieferte weiter an die alten und antwortete 500).
>    **Das steht in keiner Befundliste** und gehört vom Koordinator eingetragen.
> 2. **`main` ist ebenfalls weitergelaufen** (`d955f52` → `8c98bdc`) und hat Controller-Arbeit
>    am Aktivierungsweg gebracht (`serve/activate.go`, `serve/stream.go`, +950 Zeilen).
>    Deshalb ist `origin/main` hier mit zusammengeführt, damit der Prüfsatz nicht gegen einen
>    veralteten Stand gemessen wird. **`Updater: nil` steht trotzdem noch**
>    (`node/run.go:653`) — H7 ist davon **nicht** berührt.
>
> **Gemessen, beide Sätze, auf demselben Baum:**
>
> | | Go `tools/check.sh` | Web-Prüfsatz |
> |---|---|---|
> | vorher (`940b445`) | `check: ok`, 0 rot | **834 grün, 414 übersprungen, 0 rot** (1248, 22,5 min) |
> | nachher (`267a9cb` + `main`) | `check: ok`, 0 rot | **873 grün, 432 übersprungen, 0 rot** (1305, 23,7 min) |
>
> **+39 grün, +18 übersprungen, nichts rot, nichts verloren.** Und weil ein neuer Fall, der
> *überspringt*, nichts beweist (die Lehre von E15): die vier Sicherheitsfälle sind
> **einzeln** nachgefahren — `12 grün, 0 übersprungen` in 18,3 s.
>
> **Was dabei an der Messung selbst auffiel, und es ist ein eigener Befund.** Die
> 432 Übersprünge liegen fast alle **nicht** in `chromium`. Je Browser nachgezählt, derselbe
> Baum, 435 Fälle je Browser:
>
> | Browser | grün | übersprungen | Anteil übersprungen |
> |---|---:|---:|---:|
> | `chromium` | 433 | **1** | 0,2 % |
> | `firefox` | 247 | **188** | **43 %** |
> | `webkit` | 192 | **243** | **56 %** |
>
> Die Browser sind installiert (`chromium`, `firefox`, `webkit` liegen alle unter
> `ms-playwright/`) — es sind **Übersprünge im Fall selbst**, aus der Fähigkeitsprüfung
> (`browserFits()`). **Auf `webkit` prüft der Prüfsatz also mehr nicht als doch**, und genau
> in dieser Ecke saß der Firefox-Befund vom 17.10. (`95a9547`: die Maske nahm das Passwort an
> und antwortete mit dem Satz für ein falsches). Das ist kein Mangel dieser Zusammenführung,
> aber es ist der Grund, warum „der Web-Prüfsatz ist grün" für `firefox` und `webkit` **viel
> weniger heißt** als für `chromium`. Gehört als eigener Posten eingetragen.
>
> **Eine Zahl geht dabei um eins nicht auf, und das sei gesagt statt geglättet:** die drei
> Einzelläufe geben 872 grün, der Lauf über alle drei Browser 873. Die Übersprünge stimmen
> genau (1 + 188 + 243 = 432). Woher der eine Fall kommt, ist **nicht** nachgemessen.
>
> **Noch nicht geprüft:** dass die Zusammenführung an einer **laufenden** Anlage trägt. Grün
> ist hier der Prüfsatz, nicht das Gerät — und genau diese vier Befunde sind am 17.10. **am
> laufenden System** gefunden worden, nicht im Prüfsatz. Nach `main` geführt ist **nichts**:
> das liegt beim Koordinator.
>
> **Nachtrag vom 18.10.2026, 17:10: die zweite Hälfte ist auch drin.** `origin/web-reif`
> hatte **dieselben drei Zweige** zusammengeführt, ohne dass die beiden Sitzungen voneinander
> wussten — und keiner der zwei Zweige trug alles. Beide sind jetzt **einer**:
>
> - **Ein Konflikt**, in `users.js`, und wieder derselbe Schnitt: `web-reif` hatte seine
>   deutschen Sätze durch `t()` ersetzt, `0156cd8` hatte neue deutsche Sätze dazugelegt.
>   Keine Seite durfte gewinnen — der Abschnitt „Eingetragene Server" steht jetzt in der
>   `t()`-Form, weil `fillListed()` unter dem Konflikt schon darauf zeigte. Wer hier
>   `web-reif` allein genommen hätte, hätte eine Oberfläche gebaut, die auf eine
>   Zeichenkette zeigt, die es nicht gibt.
> - **29 Sätze in allen fünf Sprachen nachgetragen** (de, en, fr, it, es — die Projektregel
>   ist alle fünf, nie nur de/en): Adresse ändern, Stilllegen, der letzte API-Server.
>   Nachgemessen: je **350 Schlüssel**, in allen fünf **dieselbe Menge**, **dieselbe
>   Reihenfolge** und **dieselben Platzhalter**; kein leerer Satz; kein im Code benutzter
>   Schlüssel ohne Übersetzung; `users.js` ohne eine lebende deutsche Zeichenkette.
> - **`tools/check.sh`: `check: ok`** (325 Fälle, 0 rot, 84 übersprungen aus der Umgebung).
>   Die Sprachprüfsätze einzeln: **21 grün**.
>
> **Die drei Stände nebeneinander, derselbe Prüfsatz, derselbe Rechner:**
>
> | Stand | grün | übersprungen | rot | Fälle | Dauer |
> |---|---:|---:|---:|---:|---:|
> | `main` `940b445` (Ausgang) | 834 | 414 | **0** | 1248 | 22,5 min |
> | nach den drei Zweigen | 873 | 432 | **0** | 1305 | 23,7 min |
> | **vereint mit `web-reif`** | **933** | 432 | **0** | 1365 | 27,5 min |
>
> **+99 grün gegenüber dem Ausgang, und die Übersprünge sind seit dem zweiten Stand
> unverändert 432** — die Sprachschicht bringt 60 grüne Fälle und **keinen** neuen
> Übersprung. Die sechs Fälle, die mit `0156cd8` kamen, sind **einzeln** nachgefahren und
> laufen wirklich: **6 grün, 0 übersprungen** in 11,4 s (`users.spec.js:1623`, `:1695`,
> `:1722`, `:1743`, `:1784`, `:1809`).
>
> Auch hier gilt der Satz von oben unverändert: **am Gerät ist nichts davon nachgemessen**,
> und nach `main` geführt ist nichts.

### H10 — Ein `ngc-web`-Update über den Controller kann heute nicht gehen.

- **Was fehlt:** eine Entscheidung, **P3**: zieht das Release-Werkzeug auf `ui/` nach, oder
  packt der Controller ein Wurzel-tar aus? Die Skripte bauen Namen im Wurzelverzeichnis, der
  Controller packt nur Namen unter `ui/` aus, `store.WalkUI` weist das Wurzel-tar mit
  `ErrPackage` ab. Dazu: `installPart` entpackt gar nichts (Byte-Kopie, `chmod 0755`,
  umbenennen) — auf Linux gefahren **Ausstiegscode 126, „Exec format error"**.
- **Warum es zählt:** Punkt 56 hat „P3 ist beantwortet" geschrieben; das ist seit dem
  18.10.2026 **als falsch gemessen**. Ein Fremder kann NGC installieren und danach nie
  aktualisieren.
- **Größe:** **offene Entscheidung** (P3) + 1 Tag Arbeit danach; `installPart` separat
  ~4 Stunden.
- **Fundstelle:** `vertraege-v1.2.md`, P3-Zeile mit ⚠; `docs/release-weg.md`, D2 und D3;
  `docs/befunde-17-18-10-2026.md`, Abschnitt 2.

---

## 2. Die Risiken — was einem Fremden zuerst um die Ohren fliegt

Sortiert nach **Schwere**, nicht nach Aufwand. Schwere wie in
`docs/befunde-17-18-10-2026.md`: **S1** = jemand kommt hinein oder etwas Geheimes kommt
heraus · **S2** = ein Riegel wirkt nicht · **S3** = Riegel ohne jede Messung.

### R1 (S1, BLOCKED) — Eine Lizenz mit Ausstellungszeit in der Zukunft sperrt jede spätere aus.

- **Was fehlt:** die Zeile liegt in eingefrorenem `shared/store/catalog.go:321`. Der
  Verwalter liest dabei weiter Zustand „gültig".
- **Warum es zählt:** mit dem **eigenen** Schlüssel des Stores unterschrieben, also kein
  Fälschungsfall — eine Uhr, die einmal falsch stand, sperrt die Anlage dauerhaft aus.
- **Größe:** **offene Entscheidung** — nur im v1.3-Schnitt, und nur **nach** P10.
- **Fundstelle:** `docs/befunde-17-18-10-2026.md`, Abschnitt 1 (Commit `5bfaa0c`);
  `vertraege-v1.2.md`, Punkt 63 („weiter BLOCKED").

### R2 (S1) — „Mit Passkey anmelden" kann auf keiner Anlage funktionieren.

- **Was fehlt:** ein Weg, überhaupt einen Passkey anzulegen. `credentials.create` hat auf
  `main` **einen** Treffer, und der ist ein Kommentar in einem Fall
  (`tests/shell/login.spec.js:295`); `credentials.get` wird benutzt (`auth/keys.js`,
  `auth/login.js`). `login()` bricht darum stets bei `challenge.passkeys.length === 0` ab
  (`auth/login.js:128`).
- **Warum es zählt:** die Oberfläche **empfiehlt** Passkeys selbst („Bitte mit dem Passkey
  bestätigen.", `shell/confirm.js:101`), und der ganze Passschlüssel-Zweig von `session` —
  **22 Vertragsposten** — lebt ausschließlich in Prüfständen. Die gefährliche Fassung („der
  Knopf meldete mit dem Passwort an") ist auf `main` behoben (`6263f48`, nachgeprüft: **ist**
  Vorfahr von `HEAD`); geblieben ist, dass der Knopf nie geht.
- **Größe:** 1–2 Tage (ein `change()`, das einen Benutzerschlüssel hinzufügt — hängt an H8).
- **Fundstelle:** eigene Zählung am 18.10.2026; `docs/befunde-17-18-10-2026.md`,
  Abschnitt 3.1; `docs/vertragsdeckung.md`, Abschnitt 3.

### R3 (S3) — Elf von siebzehn ungemessenen Vertragsposten sind Sicherheitsriegel.

- **Was fehlt:** Grenzfälle. Das Schwerste ist **ein einziges `if`**,
  `shared/trust/trust.go:489–491`, das sechs Größen trägt: `MaxUsers`, `MaxRoles`,
  `MaxOrigins` **haben** einen Fall, `MaxNodes`, `MaxStores`, `MaxAuto` **keinen**. Ebenso
  ohne Fall: `MaxKeysPerUser` (`:578`), `MaxRoot` (`:526`), `MaxEntries` (`:633`).
- **Warum es zählt:** `MaxKeysPerUser` hängt an `session.go:455`,
  `passkeys [trust.MaxKeysPerUser + 1]int` — ein **Feld fester Länge**, indiziert aus einer
  Zahl, die aus der **unterschriebenen, verteilten** Vertrauensliste kommt. Fällt
  `trust.go:578`, schreibt `session` über das Feld hinaus. Dass die zwei Zeilen zusammen
  gehören, steht in keinem Fall. Und die echte Anlage hat **einen** Knoten, **null** Stores,
  **einen** Benutzer mit **einem** Schlüssel: drei Größenordnungen von jeder Grenze entfernt.
- **Größe:** eine knappe Stunde für die sechs `trust`-Grenzfälle, dazu je ~20 Minuten für
  `enc.MaxDepth` (trägt `stack [MaxDepth]frame`, `enc.go:271`) und
  `session.ChallengeKeyLabel`.
- **Fundstelle:** `docs/vertragsdeckung.md`, Abschnitte 4 und 5.1–5.3.

### R4 (S2) — Die zwei Wege zurück dieser Anlage teilen nichts.

- **Was fehlt:** ein gemeinsamer Rückdreh-Riegel. Der der Vertrauensliste hängt an einer
  **Datei** (Anker-Pin), der des Katalogs an einer **Zeile in der Datenbank**.
- **Warum es zählt:** eine zurückgespielte Datenbank bringt **jede Store-Nummer zurück**,
  während der Pin weg ist — zwei getrennt entschiedene Wege zurück in einer Anlage. Gemessen,
  **nicht** behoben.
- **Größe:** **offene Entscheidung** (Punkt 43: Antwort A, B oder der dritte Weg) — und
  Antwort B ist gemessen keine Antwort.
- **Fundstelle:** `docs/befunde-17-18-10-2026.md`, Abschnitt 1 (Commit `7c4804e`);
  `vertraege-v1.2.md`, Zeile „Vertrauensanker nach einem Totalverlust".

### R5 (S2) — `E_VERIFY` beim Aktivieren ist nicht erklärt, und niemand schreibt eine Zeile darüber.

- **Was fehlt:** die Ursache. Die Datei wird gefunden und gelesen, es sind **keine** der
  beiden Prüfsummen (eine Sonde rechnete dieselbe Paketsumme und denselben ui-Hash wie die
  Oberfläche). **Weder die API noch der Controller schreibt eine einzige Zeile über die
  Abweisung.**
- **Warum es zählt:** das ist die Form, die an diesem Projekt am meisten Zeit kostet — *die
  Seite, die es weiß, sagt nichts.* Ein Fremder sieht nur, dass es nicht geht.
- **Größe:** halber Tag Suche (eine Protokollzeile ist Minuten, die Ursache nicht).
- **Fundstelle:** `docs/befunde-17-18-10-2026.md`, Abschnitt 3.2.

### R6 (S2) — Ein App-Kanal läuft heute auch für eine App mit abgelaufener Lizenz.

- **Was fehlt:** `E_LICENSE` verlässt das Controller-Paket nicht. Gegenprobe am 18.10.2026:
  `CodeLicense` kommt in `apps/server-controller/` außerhalb der Tests **genau einmal** vor,
  und das ist ein Kommentar (`install/activate.go:47`: „A caller maps it to
  wire.CodeLicense") — der Anrufer, der das täte, existiert nicht.
- **Warum es zählt:** die Lizenzprüfung ist gebaut und wirkt nicht. Für ein System, das
  bezahlte Apps vorsieht (MIT-`ngc.js`, damit geschlossene Apps möglich sind), ist das der
  Riegel selbst.
- **Größe:** halber Tag.
- **Fundstelle:** `docs/controller-abnahme.md`, am 17.10. nachgemessen; eigene
  `grep`-Gegenprobe am 18.10.2026.

### R7 (S2) — Prüfstände, die nichts beweisen, und zwei, die der Maschine schaden.

- **Was fehlt:** vier Fälle in Ordnung bringen. `TestHostIsTheSystemOfOperation`
  (`install/activate_test.go:1076`) ist rot, seine Annahme ist falsch, **und er lässt einen
  Systembenutzer und einen subuid-Bereich auf der Maschine zurück**.
  `TestMainWithoutACommandRunsTheController` (`node/run_test.go:421`) findet auf einem
  eingerichteten Server das echte `/etc/ngc/controller.json`, **startet den echten
  Controller** und hängt **10 Minuten**. E18 ist rot (2 von 2), E15 misst das **falsche
  Dateisystem** (las die 9 Einträge des busybox-Abbilds, nicht die 29 des Wirts).
- **Warum es zählt:** E15 soll messen, dass der Wirt aus dem Container nicht erreichbar ist —
  **genau das ist weiter nicht gemessen**, und der Fall sah durch seinen Übersprung harmlos
  aus. Ein grüner Prüfsatz, der weniger prüft, als er vorgibt, ist die gefährlichste Stelle
  im Repo.
- **Größe:** 1 Tag; bei E18 steckt eine **Entscheidung** darin (Vorrichtung oder Code).
- **Fundstelle:** `docs/befunde-17-18-10-2026.md`, Abschnitt 3.4.

### R8 (S2) — Zwei NGC-Anlagen können auf einem WSL-Rechner nicht nebeneinander laufen.

- **Was fehlt:** nichts am Produkt — aber es gehört in die Anleitung. Alle WSL-Instanzen
  teilen einen Netzwerk-Namensraum; `ngc-api` starb 15-mal mit
  `bind: address already in use`, obwohl in der Instanz **kein** `ngc-api` lief, und
  `ngc-web` maß den Fingerabdruck der **fremden** API.
- **Warum es zählt:** wer NGC zum ersten Mal ausprobiert, tut das in WSL und sucht den Fehler
  dann Stunden im Produkt. Der Port ist der Vertrag (`config.APIPort`).
- **Größe:** 1 Stunde (ein Absatz in `installation-kleines-system.md`).
- **Fundstelle:** `vertraege-v1.2.md`, Punkt 62, Abschnitt „Warum ein Container".

### R9 (S2) — `ReadWritePaths` nennt `/etc/ngc` nicht, und `/var/lib/ngc/staged` legt kein Skript an.

- **Was fehlt:** zwei Zeilen in den Skripten. `ngc-all.sh:1322` und `ngc-api.sh:1065`
  schreiben `ReadWritePaths=${NGC_DATA} ${NGC_TLS}` — **nicht** `/etc/ngc`, wo die API den
  Anker hochziehen muss.
- **Warum es zählt:** eine Schwestersitzung hat am 17.10. genau dort „the trust anchor outside
  the database could not be moved up" gemessen; an der frischen Anlage entsteht der Mangel
  ebenso. Die Vertrauensliste kann also nicht wachsen.
- **Größe:** 1 Stunde, hängt an H3.
- **Fundstelle:** `vertraege-v1.2.md`, Punkt 62, Befunde 3 und 4.

### R10 (mittel) — Was ein Nutzer mit `letsencrypt` erlebt, ist nicht gemessen.

- **Was fehlt:** ein Lauf auf einer Maschine mit echtem Namen. Alles seit Punkt 58 ist mit
  selbst unterschriebenem Zertifikat gemessen, in einem Browser, der es für den Lauf annimmt
  (`ignoreHTTPSErrors`, `--host-resolver-rules`).
- **Warum es zählt:** die Setup-Seite liest ihren Ort aus `/config.json` und **weigert sich
  über `http://`**. Ob die Kette mit einem echten Zertifikat durchgeht, weiß niemand — und es
  ist der erste Schirm, den ein Fremder sieht.
- **Größe:** **offene Entscheidung** (Messort), danach 1 Tag.
- **Fundstelle:** `README.md`, Abschnitt „What does not work yet"; `vertraege-v1.2.md`,
  Tabelle „Messorte".

### R11 (mittel) — Zwei Fälle sind geschrieben und nie gelaufen.

- **Was fehlt:** sie laufen lassen. Zwei neue Fälle in `store.spec.js` sind geschrieben und
  **nicht gelaufen**, dazu drei Sätze, die durch eine Wegwerf-Konfiguration liefen, die
  **nicht** im Commit ist.
- **Warum es zählt:** „geschrieben" ist nicht „gelaufen". Dieselben Commits sind die, die in
  H9 auf unverbundenen Zweigen liegen — die Fälle würden mit ihnen zusammen ankommen.
- **Größe:** 2 Stunden, gehört zu H9.
- **Fundstelle:** `docs/befunde-17-18-10-2026.md`, Abschnitt 2.

### R12 (mittel) — Die Release-Prüfung ist nie ganz gelaufen.

- **Was fehlt:** `tools/release-gate.sh` Schritte **4–6** sind nicht ausgeführt (kein
  shellcheck, kein `jq`, kein Release-Verzeichnis); `govulncheck` ist auf dem echten Debian
  **nicht** gefahren; `TestReleaseScript` (31 Fälle) lief **nie mit echtem `jq`**.
- **Warum es zählt:** das ist das Tor, das vor dem ersten Release stehen soll, und es ist
  selbst ungemessen.
- **Größe:** halber Tag.
- **Fundstelle:** `docs/befunde-17-18-10-2026.md`, Abschnitt 3.3; `docs/STAND.md`.

### R13 (mittel) — Der Weg der Migrationen ist weiter nicht gelaufen.

- **Was fehlt:** ein Lauf. Der Unterbefehl ist verdrahtet (`apps/server-api/main.go:81`), der
  Migrator angemeldet — **gelaufen ist er nicht**. Dazu: `ngc-api migrate` verlangt eine
  vollständige `api.json`, die ein reiner db-Knoten nicht hat (= **P7**).
- **Warum es zählt:** die erste Aktualisierung einer fremden Anlage läuft darüber.
- **Größe:** **offene Entscheidung** (P7), danach halber Tag.
- **Fundstelle:** `apps/server-api/main.go:81`, am 18.10. gelesen;
  `docs/migrationen-wer-spielt-sie-ein.md`; `docs/befunde-17-18-10-2026.md`, Abschnitt 3.2.

### R14 (mittel) — Was ein Verwalter sieht, wenn die Datenbank des Controllers weg ist: nichts.

- **Was fehlt:** eine Meldung. Der Dienst kommt `active` hoch, ohne Fehlerzeile, und hat
  einfach keine Apps. **Bewusst so gelassen** (`63aa08a`).
- **Warum es zählt:** derselbe Schnitt wie R5 — wer die Apps sucht, findet keinen Hinweis. Es
  ist eine bewusste Entscheidung, aber eine, die ein Fremder nicht kennt.
- **Größe:** 2 Stunden.
- **Fundstelle:** `docs/befunde-17-18-10-2026.md`, Abschnitt 3.2.

### R15 (mittel) — Der Mehrmaschinenfall ist nie gelaufen.

- **Was fehlt:** ein Lauf mit MariaDB/Galera. Alles Gemessene ist der kleine Fall: eine
  Maschine, SQLite (Punkt 47). Die `galera.cnf`-Behebung (`44a03e1`) ist auf `main` und
  **vier Handschläge gemessen** — das ist mehr als nichts, aber keine laufende Anlage.
- **Warum es zählt:** die `README.md` nennt den Fall ausdrücklich als nicht gelaufen. Wer NGC
  für mehr als einen Rechner nimmt, ist der erste, der ihn fährt.
- **Größe:** 2–3 Tage.
- **Fundstelle:** `README.md`, Abschnitt „What does not work yet";
  `docs/befunde-17-18-10-2026.md`, Abschnitt 1.

### R16 — Die offenen Posten der v1.3-Reihe, die auf P10 warten.

- **Was fehlt:** **P10** zuerst — darf ein Vertrag von `shared/` überhaupt etwas
  dazubekommen? **P8** (eigener Schließgrund für die Leerlaufgrenze) und **P9** (eigener Code
  `E_BUSY`) gehen nur **danach**.
- **Warum es zählt:** P10 ist das Tor. Ohne es bleibt **jede** künftige Ergänzung für immer
  ein Bruch, und P8/P9 kosten das Dreifache. Für einen Fremden heißt heute fehlendes P9:
  „Dienst weg", „Name löst nicht auf" und „Datenbank besetzt" sind **nicht zu
  unterscheiden** — er liest immer „Dienst nicht erreichbar."
- **Größe:** **offene Entscheidung** je Punkt (JA/NEIN), Arbeit steht in der Vorlage.
- **Fundstelle:** `docs/vertraege-v1-3-vorschlag.md:48` (P10); `vertraege-v1.2.md`, Tabelle
  „Die v1.3-Reihe".

### R17 — Offene Sicherheitsentscheidungen, die ein Fremder mitbringt.

- **Was fehlt:** vier Entscheidungen, jede mit einer Folge, die der Fremde trägt: **`noexec`
  auf `/data`** (eine App darf dort abgelegte Skripte ausführen); **Digest statt Marke für
  das Terminal-Abbild** — und **wer hebt ihn an**; **Terminal: Rohtasten oder Zeilenfeld**
  (beim Zeilenfeld tippt ein Admin irgendwann sein `sudo`-Passwort in ein Feld, das es
  **anzeigt**); **Einmal-Code vor oder nach der MAC-Prüfung verbrauchen** (wer die Adresse
  kennt, verbrennt mit einer falschen MAC jeden wartenden Beitrittscode).
- **Warum es zählt:** jede dieser vier ist eine Verhaltensänderung für fremde Anlagen — darum
  sind sie bewusst keine Agentenarbeit.
- **Größe:** **offene Entscheidungen von Thomas**.
- **Fundstelle:** `vertraege-v1.2.md`, Tabelle „Sicherheit und Oberfläche".

---

## 3. Bedienbarkeit — was ein Nutzer erwartet und nicht findet

### B1 — Die Oberfläche ist deutsch. Vier der fünf Sprachen gibt es nur für 17 Sätze.

- **Was fehlt:** Übersetzungen. Gemessen: `public/assets/lang/{de,en,fr,it,es}.json` haben
  **je 17 Schlüssel**, alle aus Anmeldung und Einrichtung (`err.*`, `login.*`, `setup.*`).
  Alles andere steht als **deutscher Text im Code**: `apps/store.js` **91**,
  `apps/users.js` **78**, `shell/desktop.js` 18, `shell/trustlist.js` 10,
  `shell/appframe.js` 9, `shell/confirm.js` 7, `shell/main.js` 7, `shell/window.js` 5, die
  beiden Apps 8 — **rund 233 Zeichenketten**. Stichprobe: „Benutzer und Rechte", „Rechte
  speichern", „Neue Fassung da", „Bitte mit dem Passkey bestätigen."
- **Warum es zählt:** `lang.js` ist gebaut, nennt fünf Sprachen (`LANGS`, `:57`) und
  `FALLBACK = "en"` (`:61`) — und ein englischer Nutzer bekommt trotzdem eine deutsche
  Oberfläche. Für ein Projekt, dessen README englisch ist, ist das der erste Bruch, den ein
  Fremder sieht.
- **Größe:** 2–3 Tage (233 Stellen auf `t()` umstellen, dann 5 Sprachen füllen).
- **Fundstelle:** eigene Zählung am 18.10.2026 im Baum `d955f52`;
  `apps/web/public/assets/js/shell/lang.js:57`, `:61`, `:179`.

> **Berichtigung vom 18.10.2026, 16:30: das ist zum großen Teil schon gebaut — auf einem
> Zweig, der nirgends liegt.** `origin/web-reif` (`b436b8e`, 14 Commits vor `main`, gepusht
> um 16:24) trägt die Sprachschicht: die fünf Sprachen liegen dort in `apps/web/lang/` mit je
> **321 Sätzen** statt 17 Schlüsseln, dazu `apps/web/classes/Lang.php` und zwei eigene
> Prüfsätze (`tests/lang/files.spec.js`, `tests/lang/lookup.spec.js`). Die deutschen
> Zeichenketten im Code gehen von rund **233 auf 97** zurück — `users.js` von 78 auf **6**,
> `store.js` von 91 auf **2**.
>
> **Also nicht 2–3 Tage, sondern eine Zusammenführung** — und eine Entscheidung dazu, denn
> `web-reif` hat **dieselben drei Zweige** zusammengeführt wie `veroeffentlichungsliste`,
> ohne voneinander zu wissen. Die zwei Zweige sind nicht deckungsgleich: `web-reif` fehlt
> `0156cd8` (die Serveradresse) und das neuere `main`, `veroeffentlichungsliste` fehlt die
> Sprachschicht. **Wer nur einen von beiden nach `main` nimmt, verliert, was im anderen
> steckt.** Siehe `docs/befunde-17-18-10-2026.md`, Abschnitt 0, Berichtigung Punkt 3.
>
> **Zweite Berichtigung, 18.10.2026, 17:10: zusammengeführt, und jetzt ist auch
> aufgeschlüsselt, was übrig ist.** `origin/web-reif` ist in diesen Zweig hineingeführt; die
> 29 Sätze, die `0156cd8` mitbrachte (Adresse ändern, Stilllegen), sind in **allen fünf
> Sprachen** nachgetragen — je **350 Schlüssel**, gleiche Menge, gleiche Reihenfolge,
> gleiche Platzhalter in allen fünf (nachgemessen), und `users.js` hat **keine** lebende
> deutsche Zeichenkette mehr.
>
> **Was von B1 wirklich übrig ist, Zeile für Zeile nachgesehen:** von 105 deutschen
> Zeichenketten im Oberflächen-Code stehen **68 nicht in Kommentaren**, und davon sind die
> meisten **keine Oberflächentexte**: `contracts/wire.js` (14) sind die **Vertragstexte** der
> vierzehn Fehlercodes — sie reisen im CLOSE-Rahmen, `shared/wire/wire.go` hält dieselben
> Wörter, und `lang.js` sagt selbst, dass das absichtlich nicht dasselbe ist wie
> Oberflächentext; `shell/fake-link.js` (17) ist die Attrappe des Prüfsatzes; der Rest sind
> Kennungen und Namen (`ApiError`, `NotTrusted`, `Content-Type`, `Retry-After`, `ArrowDown`,
> `Backspace`, Schriftnamen wie `Cascadia Mono`).
>
> **Übrig bleiben rund 13 Sätze, die ein Nutzer wirklich liest:** `shell/desktop.js` vier
> („Alle Apps", „Aussehen", „Hintergrund", „Keine Treffer."), `shell/theme.js` drei
> Themennamen („Weltraum", „Polarlicht", „Abendrot"), `assets/apps/terminal/app.js` fünf
> Sätze und `assets/apps/calculator/app.js` einen („Vorzeichen"). **Größe damit: ein halber
> Tag, nicht 2–3 Tage** — und der Schreibtischrahmen ist die wichtigere Hälfte davon, die
> zwei Beispiel-Apps die unwichtigere.

### B2 — Eine Sicherung geht nur über die Befehlszeile. Eine Rücksicherung gar nicht.

- **Was fehlt:** ein Knopf. `ngc-api backup` und `ngc-controller backup` sind Unterbefehle
  (`apps/server-api/main.go:83`, `server-controller/classes/node/main.go:156`); in der
  Oberfläche kommt „backup"/„Sicherung" **nirgends** vor (`grep -ril` über
  `apps/web/public/` = **0 Treffer**).
- **Warum es zählt:** das ist die eine Sache, die ein DSM-Nutzer als Erstes sucht. Und der
  Satz, der die Kopie überhaupt brauchbar macht — **der Schlüssel liegt in `api.json`, nicht
  in der Datei** — stand bis zum 18.10. nur in einem Go-Kommentar.
- **Größe:** 1–2 Tage (Knopf, Fortschritt, Herunterladen), die Rücksicherung separat.
- **Fundstelle:** eigene Messung am 18.10.2026; `docs/befunde-17-18-10-2026.md`, Abschnitt 2
  (Commits `9648c12`, `63aa08a`).

### B3 — Der Schreibtisch hat zwei Apps, und beide sind Beispiele.

- **Was fehlt:** Apps. `apps/web/assets/apps/` enthält `calculator` und `terminal`. Weil es
  keinen Store gibt (H5) und keine App installierbar ist (H7), ist das alles, was ein Fremder
  je sieht.
- **Warum es zählt:** NGC verspricht „a desktop in the browser, where apps run in
  containers". Heute: ein Schreibtisch mit einem Rechner darauf.
- **Größe:** hängt ganz an H5 und H7.
- **Fundstelle:** `ls apps/web/assets/apps/` am 18.10.2026; `README.md`, Zeile 5.

### B4 — Ein Fremder muss NGC selbst bauen und selbst unterschreiben.

- **Was fehlt:** der erste Schritt einer Installation ist heute „Release bauen und
  unterschreiben" (`docs/installation-kleines-system.md:58`) — mit Go-Werkzeugkette,
  `ngc-store` und einem eigenen Schlüssel, den man nur mit `sed` in vier Skripten einsetzen
  kann (H6).
- **Warum es zählt:** eine Anleitung, die mit „bau es erst selbst" anfängt, ist keine
  Installationsanleitung. Mit H1, H2 und H3 zusammen ist das die ganze Antwort auf die Frage
  dieser Datei.
- **Größe:** fällt mit H1–H3 weg, kostet allein nichts.
- **Fundstelle:** `docs/installation-kleines-system.md:54–76`.

### B5 — Der Kopplungscode ist nur an einer Stelle zu holen, und das stand erst nicht da.

- **Was fehlt:** nichts am Code — der Knopf heißt **„Kopplungscode zeigen"** und liegt in
  *Benutzer & Rechte* → Reiter **Server** (Punkt 62). Was fehlte, war die Zeile in der
  Anleitung; Punkt 60 hat deswegen einen Befund gemeldet, den Punkt 61 widerrufen musste.
- **Warum es zählt:** als Risiko gering, als Lehre nicht: ein Fremder, der ihn nicht findet,
  hält NGC für kaputt. `installation-kleines-system.md:140` sagt es jetzt („nur hier,
  nirgends sonst").
- **Größe:** erledigt.
- **Fundstelle:** `vertraege-v1.2.md`, Punkt 60 (ÜBERHOLT) und Punkt 61;
  `docs/installation-kleines-system.md:140`.

### B6 — Nach dem Aufnehmen eines Servers zeigt die Liste ihn weiter.

- **Was fehlt:** eine Auffrischung — oder die zweite Hälfte eines Vorgangs. Knopf „Aufnehmen"
  geklickt, die Minutenwarnung der API hörte auf, aber „Wartende Server" zeigte den Eintrag
  weiter und der Anker blieb bei `seq: 1`.
- **Warum es zählt:** der Nutzer weiß nicht, ob es geklappt hat, und klickt noch einmal.
  **Ob ausbleibende Auffrischung oder halber Vorgang: nicht gemessen** (Punkt 62).
- **Größe:** halber Tag Messen, dann 2 Stunden.
- **Fundstelle:** `vertraege-v1.2.md`, Punkt 62, „Was ungemessen bleibt".

### B7 — Eine migrierte, aber nicht eingerichtete Anlage lügt bei jeder Anmeldung.

- **Was fehlt:** ein dritter Grund. `state.ErrNoTrust` erreicht die zweite Zeile: die Anlage
  sagt bei **jedem** `login/start` „die Datenbank konnte nicht erreicht werden" — über eine
  Datenbank, die einwandfrei antwortet. **12 288 solcher Zeilen in 3,4 s** gemessen.
- **Warum es zählt:** genau der Zustand, in dem ein Fremder nach dem ersten Aufsetzen steckt,
  und die Meldung schickt ihn an die falsche Stelle.
- **Größe:** **offene Entscheidung** (welcher Anrufer einen dritten Grund geben darf), hängt
  an **P9**; danach 2 Stunden.
- **Fundstelle:** `docs/befunde-17-18-10-2026.md`, Abschnitt 2 (Commit `e160c82`,
  ausdrücklich **nicht** geändert).

### B8 — Die fünfte Schutzeinstellung der Oberfläche ist nicht gemessen.

- **Was fehlt:** eine Messung. Punkt 56, Befund 3: „NGC brings 5 … and this version of
  ngc-web knows 4 of them."
- **Warum es zählt:** die Oberfläche sagt über sich selbst, dass sie eine Einstellung nicht
  kennt. Welche, und was sie schützt, steht nicht da.
- **Größe:** 2 Stunden.
- **Fundstelle:** `vertraege-v1.2.md`, Punkt 56, Befund 3.

---

## 4. Die fünf Dinge, die zuerst passieren müssen

In dieser Reihenfolge, und die Reihenfolge ist begründet: jede Zeile macht die darunter erst
messbar.

| # | Was | Wer | Größe |
|---|---|---|---|
| **1** | ~~Die drei Zweige zusammenführen~~ (H9) — **am 18.10.2026 erledigt**, auf Zweig `veroeffentlichungsliste`, beide Prüfsätze grün (siehe Kasten unter H9). **Was bleibt: der Koordinator führt diesen Zweig nach `main`** und misst die vier Befunde an einer laufenden Anlage nach — gefunden wurden sie dort, nicht im Prüfsatz. | **Thomas / Koordinator** | 1 h |
| **2** | **Den Updater verdrahten** (H7). `Updater: nil` heißt: auf **keiner** Anlage kann eine App installiert werden. Das ist der Zweck des Produkts, und solange er fehlt, messen alle anderen Punkte eine Hülle. | **Thomas entscheidet** (Vertrauensliste als Wert oder als Kette), **Agent baut** | 1–3 Tage |
| **3** | **Repo öffnen, Release veröffentlichen, Skripte nach ulh-`main`** (H1, H2, H3). Diese drei sind **eine** Entscheidung, weil keine ohne die anderen etwas nützt — und ein Push nach ulh-`main` erreicht per Auto-Update **sofort alle Nutzer**. | **nur Thomas** | Entscheidung + 1 Tag |
| **4** | **Den Release-Schlüssel klären** (H4), dazu den falschen Kommentar (H4a) und den Weg für einen Testschlüssel (H6). Bis dahin prüft jede fremde Installation gegen einen Schlüssel, dessen geheime Hälfte in `/root` auf einer Testmaschine liegt. | **nur Thomas** (Stick oder Datei; der Stick zieht Punkt 19 nach) | Entscheidung + 2–4 h |
| **5** | **Einen zweiten Menschen anlegen können** (H8), und damit auch einen Passkey (R2). Heute hat eine Anlage genau einen Benutzer, für immer. `ngc.users/trust` nimmt alles schon an — es fehlt nur die Oberfläche, **kein** Vertragsschnitt. | **Agent** | 1–2 Tage |

### Was von diesen fünf nur Thomas entscheiden kann

- **#3 ganz** — Repo öffnen, Release veröffentlichen, Push nach ulh-`main`. Letzteres
  erreicht sofort alle ulh-Nutzer.
- **#4 ganz** — welcher der zwei Schlüssel der echte wird. Wird es der YubiKey, kommt Punkt 19
  dazu, und das ist eine Änderung in `shared/`, also nur im v1.3-Schnitt.
- **Die Vorfrage von #2** — welche Seite bei `update.Deps.List` nachgibt.
- **Nicht in den fünf, aber davor nötig, sobald `shared/` angefasst wird:** **P10** (darf ein
  Vertrag überhaupt etwas dazubekommen). Ohne P10 bleiben P8, P9 und R1 für immer ein Bruch,
  und R1 ist ein S1.
- **Dazu der offizielle Store** (H5). Er steht nicht in den fünf, weil ohne #2 ohnehin keine
  App läuft — aber ohne ihn hat NGC dauerhaft keine Apps.

---

## 5. Vermutet, nicht geprüft

Alles darüber hat eine Fundstelle. Das hier nicht — es steht getrennt, damit niemand es
weiterträgt.

- **Die Größen in Stunden und Tagen sind Schätzungen.** Nur H9 (2–4 h) ist aus dem gemessenen
  Umfang abgeleitet (drei Zweige, vier Commits). Alle anderen sind geschätzt und niemand hat
  sie nachgerechnet.
- **Die 233 deutschen Zeichenketten in B1 sind eine Musterzählung**, kein Abgleich. Das
  Muster `"[A-ZÄÖÜ][a-zäöüß]…"` trifft auch englische Sätze; die Stichproben waren alle
  deutsch, aber ich habe nicht jede der 233 gelesen. Die 17 Schlüssel je Sprachdatei sind
  dagegen gezählt und sicher.
- **Ob die drei Zweige aus H9 sich sauber zusammenführen lassen, ist nicht geprüft.** Gemessen
  ist nur, dass ihre Commits nicht auf `main` liegen. Punkt 63 nennt für den letzten Umzug
  genau **einen** Konflikt (`tools/check.sh`); dass es hier ebenso wenige sind, ist eine
  Vermutung.
- **Welcher der sechs ulh-Zweige der richtige ist, habe ich nicht beurteilt.** Gezählt sind
  nur Dateien und Abstand zu `main`. Vier Zweige tragen alle fünf Skripte; ob sie denselben
  Inhalt haben, ist nicht verglichen.
- **Dass H1 (privates Repo) mit einem Klick erledigt ist, ist eine Annahme.** Ob vorher etwas
  aus der Geschichte muss (Schlüssel, Messdaten, Pfade), ist **nicht geprüft** — und bei einem
  Repo, in dem Releases mit einem Dateischlüssel unterschrieben wurden, ist das keine müßige
  Frage.
- **Ob `ngc-db.sh` ohne Release-Schlüssel wirklich auskommt**, habe ich nicht am Code geprüft;
  `grep` fand in diesem Skript keinen, und `docs/installation-kleines-system.md:24` sagt es
  so. Beides ist kein Beweis, dass es nirgends eine Programmdatei holt.
- **Die Schwere-Einstufungen in Abschnitt 2 sind übernommen**, nicht neu vergeben — aus
  `docs/befunde-17-18-10-2026.md`. Wo ich eine selbst gesetzt habe (R6, „S2"), ist es ein
  Urteil, keine Messung.
