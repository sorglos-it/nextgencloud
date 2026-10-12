# Vertragsfassung V1.3 — Entscheidungsvorschlag

**Stand 12.10.2026.** Zusammengetragen aus `docs/vertraege-v1.2.md` (Punkte 24–30), den
Berichten unter `.superpowers/sdd/` und dem Code der Zweige `api`, `controller`, `db`,
`recovery`, `web`.

**Dieses Dokument entscheidet nichts.** Es legt Thomas die Entscheidung in einem Zug hin.
Nichts in `shared/` ist angefasst worden.

---

## Das Wichtigste zuerst

**Von den acht bekannten Punkten brauchen nur fünf eine Vertragsänderung.**
Drei gehören nicht in V1.3:

- **Punkt 6 (MariaDB-Hälfte von W-1)** ist **kein** `shared/`-Punkt. Die Schnittstelle
  `knowsItsSchema` sitzt in `apps/server-db/classes/migrate/already.go` und ist dort
  **absichtlich nicht** Teil von `sqlstore.Dialect` — der Kommentar sagt den Grund.
  `sqlstore` liegt unter `apps/`, nicht unter `shared/`.
- **Punkt 7 (Einmal-Code vor der MAC)** liegt in `apps/server-api/classes/join/join.go:107`
  und `.../controllers/pair.go:79`. Beide Male ist es eine **Reihenfolge im eigenen Code**.
  Kein Vertrag muss sich dafür ändern.
- **Punkt 8 (zwei Server, ein Name)** liegt in `join.keep` (`join.go:135`). Die
  Unterscheidung am öffentlichen Schlüssel ist dort baubar; `dbapi.Blob` reicht dafür aus.

**Und die fünf übrigen sind in Wahrheit EIN Punkt** — siehe „Die gemeinsame Wurzel" unten.

---

## Rangfolge

| Rang | Punkt | Art |
|---|---|---|
| 1 | P1 — `store.validHTTPSURL` nicht ausgeführt | **Sicherheit**, gemessen |
| 2 | P2 — `core.Unattended` verbietet den ersten Einbau | **geht gar nicht** |
| 3 | P3 — das Format einer `ngc-web`-Release-Datei steht nirgends | **geht gar nicht** |
| 4 | P5 — Referenzfeld in `dbapi.App` fehlt | **geht gar nicht**, dazu eine falsche Zahl |
| 5 | P4 — `shared/ulh.md` hat keine Balancer-Vorlage | **Bequemlichkeit**, und **sofort machbar** |
| — | P7, P8 | Entscheidungen, **kein** Vertrag |
| — | P6 | gehört nicht hierher |

---

## P1 — Die Adressprüfung ist nicht ausgeführt (Punkt 25)

**Gemessen (von mir, 12.10.2026, am Code):**
`shared/store/catalog.go:208` heißt `validHTTPSURL`, klein geschrieben. Sie prüft den
Pfad-und-Query-Teil Zeichen für Zeichen: alles außerhalb 0x21–0x7e fällt durch, dazu
`" ' < > \ #`.

**Und hier ist es schlimmer als in Punkt 25 beschrieben.** Die zweite Grammatik gibt es
schon: `apps/server-api/classes/appstore/fetch.go:335`, `downloadURL`. Sie prüft
**Länge, `#`, Schema und Rechnername — und den Pfad gar nicht.** Steuerzeichen,
Leerzeichen, `"`, `'`, `<`, `>` und `\` im Pfad gehen dort durch, in `shared/` nicht.

Die Begründung steht im Code daneben (`fetch.go:76`): „the value is repeated here and
nothing else of that grammar is". Das ist ehrlich aufgeschrieben — und genau der Zustand,
den Punkt 25 verhindern wollte.

**Was „nur behauptet" ist:** dass ein Store je so eine Adresse unterschreibt. Nicht
gemessen. Der Riegel in `VerifyCatalog` greift heute vorher.

**Wenn man es nicht ändert:** die beiden Grammatiken laufen auseinander, und zwar ab
heute, nicht irgendwann. Wer eine Adresse auf einem anderen Weg hereinbekommt als über
`VerifyCatalog` — und Punkt 30 baut gerade so einen Weg —, fragt sie mit der schwächeren
Prüfung an.

**Kleinster Eingriff:** `validHTTPSURL` → `ValidDownloadURL` umbenennen, groß.
**Ergänzt nur, bricht nicht.** Kein bestehender Aufrufer ändert sich.

**Danach nachzuziehen:** der api-Strang ersetzt `appstore.downloadURL` und
`maxDownloadURL` durch den Aufruf; die Fälle von `downloadURL` wandern mit.

---

## P2 — Ein ERSTER Einbau ohne Aufsicht geht nicht (Punkt aus Controller 20)

**Gemessen (von mir, 12.10.2026, mit einem Fall gegen `shared/store`):**

```
Newer("1.2.0","") = false
ValidVersion("")  = false
```

`store.Newer` (`shared/store/manifest.go:236`) gibt für jede ungültige Fassung `false`,
und `""` ist keine. `core.Unattended` (`shared/core/core.go:229`) endet mit
`store.Newer(p.Version, installed)` — also **false**, wenn nichts installiert ist.

**Der Kommentar sagt das Gegenteil.** `core.go:214`: „p.Version is newer than installed
(…; `""` never is older)". Gemessen falsch. **Genau der Fehler, vor dem die Regel
„Kommentar gegen Code lesen" warnt.**

**Wie weit es reicht:** `trust.AutoSystem` ist ausgenommen (`core.go:229` prüft es vorher,
`update/system.go:182` ruft deshalb mit `installed=""` auf). Betroffen sind also die
NGC-Teile `ngc-api`, `ngc-web`, `ngc-controller`.

**Der Controller hat die Lücke bereits benannt und sich lokal einen Riegel gebaut**
(`update/install.go:354`: `if installed != "" && !store.Newer(...)`), dazu zwei Wächter in
`install_test.go:918` und `:924`, die **rot werden, sobald `shared/` repariert ist**.

**Wenn man es nicht ändert:** kein unbeaufsichtigter erster Einbau von `ngc-api` oder
`ngc-web`. Jede Erstinstallation braucht einen angemeldeten Benutzer am Kanal. Die stehende
Genehmigung in der Vertrauensliste nützt genau beim ersten Mal nichts — also dann, wenn
niemand da ist.

**Kleinster Eingriff:** eine Zeile in `core.Unattended`:
`p.Target == trust.AutoSystem || installed == "" || store.Newer(p.Version, installed)`.
**Das bricht den Vertrag** — eine Funktion sagt danach bei gleicher Eingabe etwas anderes.
Neue Fassung nötig. Der Kommentar wird dabei **wahr**, nicht umgeschrieben.

**Alternative, die nicht bricht:** `store.Newer` unangetastet lassen und eine neue
Funktion danebenstellen (`store.NewerOrFirst`). Dann ändert sich `Unattended` trotzdem im
Verhalten — es führt also kein Weg an einer neuen Fassung vorbei.

**Danach nachzuziehen:** der Controller nimmt seinen lokalen Riegel **nicht** weg (er ist
laut Kommentar die einzige Wand für den Kanal **mit** Aufgabe), aber die zwei Wächter in
`install_test.go` müssen umgeschrieben werden — sie sind so gebaut, dass sie bei der
Reparatur fallen.

---

## P3 — Was eine `ngc-web`-Release-Datei ist, steht nirgends (Punkt aus Controller 20)

**Gemessen (von mir, 12.10.2026):**

- `config.WebRoot = "/var/www/ngc"` steht in `shared/config/config.go:35` und kommt in
  **keiner weiteren `.go`-Datei** von `shared/` vor.
- `shared/testdata/release/ngc-web-all.tar` ist **18 Byte ASCII-Text**, kein tar.
- Der Controller hat sich deshalb entschieden, eine `ngc-web`-Release-Datei sei ein tar
  nach `store.WalkUI` mit Präfix `ui/`, und hat das als BLOCKED hingeschrieben
  (`update/install.go:846`).

**Wenn man es nicht ändert:** der Controller entpackt nach einer **Annahme**. Trifft das
Release-Werkzeug (`release-packer`) eine andere Annahme, ist das Webinterface nach dem
ersten Update leer oder halb — und `WebRoot` existiert zwischen zwei Umbenennungen
ohnehin kurz nicht (`install.go:762`). Es gibt heute **keine Stelle**, an der die beiden
Annahmen aufeinandertreffen und auffallen.

**Kleinster Eingriff:** ein Satz in `shared/` (README oder `store`), der sagt: eine
`ngc-web`-Release-Datei ist ein tar nach `store.WalkUI`, Namen unter `ui/`. Dazu eine
**echte** Testdatei statt der 18 Byte.
**Ergänzt nur** — wenn die Entscheidung so ausfällt, wie der Controller gebaut hat.
**Bricht**, wenn es ein schlichtes tar werden soll: dann braucht `shared/` eine zweite,
sichere Auspack-Funktion.

**Danach nachzuziehen:** `release-packer` baut genau dieses Format; der Controller streicht
seinen BLOCKED-Absatz; `shared/testdata/release/` bekommt die richtige Datei.

---

## P5 — Das Referenzfeld in `dbapi.App` fehlt (Punkt 24)

**Gemessen (von mir, 12.10.2026):** `dbapi.App` (`shared/dbapi/dbapi.go:83`) hat die
Felder ID, Version, Store, Node, Active, Manifest, SHA256, Grants, KeptData, Updated.
**Kein Feld für die Abbild-Referenz.**

**Übernommen aus Punkt 24 (Controller 16, 10.10.2026), nicht von mir nachgemessen:** die
vier Zeilen zu `user.go:188`, `podman.go:438`, `user.go:258`, `data.go:227`.

**Von mir am Bericht nachgesehen:** Aufgabe 22 der API bestätigt es unabhängig
(`task-22-report.md:366`): das Löschen der UI-Zeilen beim Deinstallieren ist „genau
dagegen gebaut, dass eine Zeile ohne Referenz liegen bleibt" — die Reihenfolge
Controller → UI-Zeilen → App-Zeile ist **von Hand** richtig herum gebaut und mit einem
Mutanten (`row-before-ui-rows`) gehalten.

**Wenn man es nicht ändert:** zweierlei.
Erstens bleibt bei `KeepData` ein Abbild **für immer** im Datenverzeichnis liegen, und
keine Buchführung weiß davon. Zweitens — und das wiegt schwerer — zeigt `DataSize` dem
Verwalter die Abbild-Schichten **als eigene Daten**. Er entscheidet über das Löschen
anhand einer Zahl, die etwas anderes zählt als ihr Name sagt.

**Kleinster Eingriff:** ein Feld `Image string` in `dbapi.App`.
**Das bricht den Vertrag:** jede Umsetzung von `dbapi` (`memstore`, `sqlstore`) muss das
Feld tragen, und `dbtest` muss es prüfen — sonst steht es morgen nur in einem der beiden.

**Danach nachzuziehen:** `memstore` und `sqlstore` (db-Strang), eine Zeile in `dbtest`,
eine Migration in `apps/server-db/sql/`, und der Controller schreibt das Feld beim
Einbauen und liest es beim Entfernen. Er prüft dabei mit, ob dasselbe Feld auch dem
Deinstallieren hilft, statt sich auf den Benutzer zu verlassen.

---

## P4 — `shared/ulh.md` hat keine Balancer-Vorlage (Controller 22)

**Gemessen (von mir, 12.10.2026):** `shared/ulh.md:148–157`. Die Regel steht in Prosa:
Datei `/etc/ngc/apache-apis.conf`, wer schreiben darf, atomar, Modus 0644, `apachectl
configtest`, je API ein `BalancerMember`. Und dann, Zeile 156:

> „Die genaue Form legt der Plan des Webinterface fest und **trägt sie hier als Vorlage
> ein**; der Controller schreibt genau diese Form."

**Die Vorlage ist nicht eingetragen.** Die Regel verlangt sie und nennt sich selbst als
Ort. Darum ist Schritt 6 jener Aufgabe offen.

**Wenn man es nicht ändert:** der Controller kann „genau diese Form" nicht schreiben, weil
es keine gibt. Zwei Seiten schreiben in dieselbe Datei (`ngc-web` und der Controller mit
Rolle `web`) — und jede nach ihrer eigenen Vorstellung. Nach dem nächsten
`configure` sieht die Datei anders aus als nach dem letzten.

**Kleinster Eingriff:** die Vorlage aus dem Web-Plan nach `shared/ulh.md` kopieren.
**Ergänzt nur.**

**Und das Beste: dafür braucht es V1.3 gar nicht.** `shared/ulh.md` ist die **einzige
gewährte Ausnahme** vom Einfrieren — so steht es im db-Bericht
(`task-7-report.md:226`: „die einzige gewährte Ausnahme ist `shared/ulh.md`").
Das kann der Web-Strang heute machen.

**Danach nachzuziehen:** der Controller-Strang nimmt Schritt 6 wieder auf.

---

## Die gemeinsame Wurzel — P1, P2, P3, P4 und P5 sind derselbe Punkt

Thomas hat richtig gerochen. Alle fünf haben dieselbe Form, und zwar belegbar:

> **In `shared/` ist der Wert da oder geprüft — aber nicht dort, wo ihn der Nächste
> braucht. Also baut sich der Nachbar einen Ersatz, und der ist schwächer.**

| Punkt | Der Wert ist da als … | Der Nachbar baut … | Beleg |
|---|---|---|---|
| P1 | `validHTTPSURL`, klein | `appstore.downloadURL`, **ohne Pfad-Prüfung** | `fetch.go:335` |
| P2 | `Unattended`, das `""` nicht kennt | `installed != "" && …` im Controller | `install.go:354` |
| P3 | `config.WebRoot` ohne Format | Annahme „tar nach `WalkUI`" | `install.go:846` |
| P4 | Regel ohne Vorlage | zwei Schreiber, zwei Formen | `ulh.md:156` |
| P5 | `dbapi.App` ohne Referenz | Löschreihenfolge von Hand | `task-22-report.md:366` |

**Das heißt für die Entscheidung:** es sind nicht fünf Einzelfragen, sondern eine
Entscheidung mit fünf Stellen. Wer sie als eine behandelt, kann einen Satz in V1.3
schreiben, der die sechste Stelle verhindert:

> **Jede Prüfung, die `shared/` kennt, ist ausgeführt. Jeder Wert, den ein Nachbar
> aufschreiben muss, hat ein Feld. Jede Form, die zwei Seiten schreiben, steht als
> Vorlage im Vertrag. Wo das nicht geht, steht im Vertrag, warum.**

Vier der fünf Eingriffe sind **klein**: eine Umbenennung, eine Zeile, ein Satz plus eine
Testdatei, eine Vorlage. Nur P5 zieht die Datenbank nach.

---

## Was NICHT in V1.3 gehört, und warum

**P7 — Einmal-Code vor der MAC** (`join.go:107`, `pair.go:79`).
Kein Vertrag. Es ist eine **Entscheidung**, und beide Seiten haben einen Preis:
heute verbrennt eine falsche MAC den wartenden Code (billiges Zumachen von außen);
andersherum darf jemand beliebig oft gegen denselben Code rechnen. `pair.go:55` begründet
die heutige Wahl bereits im Kommentar („the safe half of that trade").
**Vorher zu messen, und es ist nicht gemessen:** wie lange ein Code gilt und wie oft er neu
ausgegeben wird. Wer entscheidet, entscheidet für **beide** Stellen.

**P8 — zwei Server, ein Name** (`join.go:135`).
Kein Vertrag. Der öffentliche Schlüssel liegt in der Anfrage; `join.keep` kann vor dem
`BlobPut` lesen, was schon da steht, und am Schlüssel entscheiden — gleicher Schlüssel
überschreibt, anderer wird abgewiesen und gemeldet. Das ist eine Aufgabe im api-Strang.

**P6 — MariaDB-Hälfte von W-1** (`already.go:50–68`).
Gehört gar nicht hierher. Der Kommentar sagt es selbst: `knowsItsSchema` ist
**absichtlich nicht** Teil von `sqlstore.Dialect`, weil MariaDB nichts dieser Form
antworten kann und ein Pflichtmethode dort aus Dokumentation beantwortet werden müsste.
Der Wächter (`TestTheStepOverStaysTheOldOneForADialectThatCannotSay` und
`TestExactlyOneDialectCanSayWhatItHolds`) hält die Lücke in Form. Wer einen Cluster hat,
schließt sie dort — ohne Vertragsfassung.

---

## Zwei Punkte, die beim Nachsehen weggefallen sind

- **api 17 („`shared/wire.Open` müsste die Nutzlast aufbewahren")** — erledigt.
  `Forward` nimmt auf dem Zweig `api` ein zusätzliches `raw []byte`
  (`relay/relay.go:193`), und `wire.Open` hält die Aufgaben-Bytes ohnehin schon
  (`wire.go:321`, `TaskBytes()`). Kein `shared/`-Eingriff mehr nötig.
- **Controller 10 („`shared/tunnel` oder `shared/config` für die erste Fassung der
  Vertrauensliste")** — der Strang hat Weg (a) gemessen, gewählt und gebaut; die Änderung
  in `shared/` war die verworfene Alternative.

## Offene Nebenpunkte ohne eigene Nummer

- **Punkt 29** nennt eine Zahl, die noch fehlt: wie viele Terminals gleichzeitig. Gezählt,
  nicht gemessen: rund 80 MiB bei 1024 Kanälen. Die Obergrenze gehört laut Punkt 29 in den
  Vertrag — also in V1.3, wenn sie gemessen ist.
- **Punkt 28 und Punkt 30** ändern `shared/` **nicht**. Beide hängen an api-Aufgabe 24
  („`main.go` verdrahten"). Sie gehören nicht in diese Entscheidung.
