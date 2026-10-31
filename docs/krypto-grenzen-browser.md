# Krypto-Grenzen der Browser – was NGC in welcher Engine kann

Stand 31.10.2026. Grundstand `dab25ef7` (origin/main), Zweig `web-krypto-grenzen`.
Arbeitsbaum `D:\claude\working\ngc-krypto-grenzen` (lokale Platte D:, nicht das NAS Z:).

Dieses Blatt trennt **zwei verschiedene Grenzen** mit **zwei verschiedenen Behebungen**.
Wer sie in einen Satz packt, sucht später an der falschen Stelle.

## 0. Die erste Lehre, und sie ist keine über Browser

**Der Befund stand schon im Repo – getrennt, gemessen, mit Datum und Befehl.** In
`apps/web/tests/fixtures/page.js:124-138` und `:322-346` steht seit dem 18.10.2026,
dass webkit 26.0 bei `generateKey` mit `NotSupportedError` fällt und firefox 141.0 bei
`exportKey("jwk")` mit `OperationError` – zwei verschiedene Grenzen, jede benannt, mit
den drei Zeilen zum Nachfahren daneben. Gefunden hat es niemand, weil keiner dort
nachgesehen hat. Stattdessen wurde dreimal eine Zahl weitergegeben, die der Absender
nicht selbst gelesen hatte: erst „WebKit kann gar kein Ed25519" (Vermutung), dann „alle
Übersprünge hängen an einem Haken `hasEd25519`" (falsch), dann „es sind vier Haken"
(auch falsch – es sind zehn über sechs Lücken, und daneben ein zweites System aus elf
lockeren Kopien).

**Und es stand nicht nur im Repo.** Dieselbe Messung liegt seit dem 25.09./01.10.2026
auch im Gedächtnis dieses Rechners, Wort für Wort: firefox kann `sign`, `verify`,
`deriveBits`, `generateKey` und `importKey` für beide Kurven und genau eines nicht,
`exportKey("jwk")` eines per `importKey("pkcs8")` eingeführten Schlüssels; webkit kennt
die Kurven gar nicht. Zwei unabhängige Quellen, beide ungelesen.

Die Notiz dort hält zusätzlich fest, **wie teuer dieses Muster schon einmal war**:
sechsundzwanzig Aufträge lang wurde „firefox startet auf dieser Maschine nicht" als
Umgebungstatsache weitergegeben, niemand hat firefox gemessen, und dann lief ein Satz
versehentlich ohne Engine-Filter – **157 rote Fälle**. Am 31.10.2026 wiederholt sich
dieselbe Figur, nur andersherum: ein Satzlauf über **zwei** Engines, und die am
stärksten betroffene fehlt (Abschnitt 8, Punkt 5).

**Daraus folgt der Satz, der das nächste Mal verhindert.** Dass ein Lauf alle drei
Engines fährt, ist keine Verbesserung am Rand und kein Gründlichkeitsluxus, den man
weglassen darf, weil es schneller geht – es ist die Behebung eines Fehlers, der hier
schon einmal **157 Fälle** gekostet hat. Wer die dritte Engine weglässt, misst nicht
weniger genau, sondern **in der Engine gar nicht**, und ein Grün aus zwei Engines sagt
über die dritte nichts. Ein übersprungener Fall heißt nicht „der Browser kann es nicht",
sondern „wir wissen nicht, ob er es kann" – und eine fehlende Engine heißt nicht einmal
das.

Dazu die billige Gegenmaßnahme, die am 31.10.2026 einen Platz gerettet hat und ab jetzt
vor jedem Lauf gilt: **die Fahrbereitschaftsprüfung.** `npx playwright test --list` auf
die gemeinte Datei startet keinen Browser, bindet keinen Port und braucht keine Sekunde
eines Platzes – und sie nennt die Fallzahl **je Engine** und einen Ausstiegscode. Hier:
6 Fälle, 3 Engines, Ausstieg 0. Wer ohne sie auf einen freien Platz losfährt, erfährt
erst im Platz, dass die Vorrichtung fehlt (Abschnitt 4, Tabelle).

**Die Lehre: vor der Messung die Datei lesen, die die Antwort schon enthalten könnte.**
Das ist dieselbe Figur, die `messen.md` unter „eine Abwesenheit belegt man mit Orten,
nicht mit Suchern" führt – dort war eine frühere, gelungene Messung für einen Irrtum
gehalten worden, weil die Mehrheit nichts fand. Hier war sie nicht einmal gesucht.

Was **nicht** im Repo stand und darum den eigentlichen Auftrag ausmacht: was die
Engines jenseits der ersten gefallenen Stufe können, und ob sich dort ein Mensch
anmelden kann.

---

## 1. Erwartung, aufgeschrieben VOR der Messung

Festgehalten am 31.10.2026, bevor ein Browser gelaufen ist. Rein aus dem Quelltext
gelesen, damit später zu sehen ist, wo das Lesen danebenlag.

| Nr. | Erwartung | Wird wie geprüft |
|---|---|---|
| E1 | Es gibt **kein** einzelnes `hasEd25519`, an dem alle Übersprünge hängen. Zwei getrennte Systeme: zehn `skipWithout…` über sechs gemessene Lücken, und daneben elf lokale Kopien eines lockereren `hasEd25519`. | Dateizeilen, Abschnitt 2 |
| E2 | Die webkit-Grenze ist **die Kurve selbst** (`generateKey` fällt), die firefox-Grenze ist **ein einzelner Ausgang** (`exportKey("jwk")` eines eingeführten Schlüssels). Verschiedene Stufen, verschiedene Behebungen. | Fähigkeitstafel, Abschnitt 4 |
| E3 | In Playwrights webkit kommt **die Maske nie** – `browserFits()` fällt vor allem anderen, der Mensch liest `boot.tooold`. | Anmeldemessung, Abschnitt 5 |
| E4 | In firefox kommt eine Anmeldung **sehr wohl durch**, aber nur über **Passkey**: Passwortfeld aus, Passkey-Knopf an. | Anmeldemessung, Abschnitt 5 |
| E5 | Die webkit-Grenze ist eine Grenze **des Prüfgerüsts**, nicht von Safari. Safari ab 17 hat Ed25519. Dann ist „in diesem Browser kann sich niemand anmelden" für echtes Safari **falsch**. | Abschnitt 6, und sie ist mit diesem Prüfsatz **nicht** zu messen |
| E6 | Die elf lokalen `hasEd25519`-Kopien sind in firefox **zu locker**: sie fragen nur `importKey("raw")`, das kann firefox – also lassen sie Fälle laufen, die danach an `exportKey("jwk")` sterben. | Abschnitt 4 gegen Abschnitt 2 |

Was diese Erwartung **widerlegen** würde, und das wäre das beste Ergebnis von allen:
eine Anmeldung, die in firefox **und** in webkit durchkommt. E3 und E4 sind genau darauf
gerichtet.

---

## 2. Woran der Übersprung hängt – zwei Systeme, nicht eins

### System A: zehn Haken über sechs gemessene Lücken

Alle in `apps/web/tests/fixtures/page.js`. Sie hängen an **einer** Messfunktion,
`sessionKeyGaps` (Zeile 139), die je Engine sechs Lücken misst. Jede Lücke antwortet
mit dem leeren Text, wenn die Engine den Vorgang kann, sonst mit dem Namen der ersten
Stufe, die gefallen ist – und genau dieser Text steht dann in der Übersprung-Begründung.

| Lücke | Was sie wirklich tut | Zeile |
|---|---|---|
| `curves` | die zwei `generateKey`, die der Auslieferungscode in `browserFits()` macht – und nichts weiter | 193 |
| `ed25519` | Schlüssel, Signatur, Prüfung, öffentlicher Teil wieder eingeführt **und benutzt** | 206 |
| `ed25519FromSeed` | öffentlicher Teil aus einer PKCS#8-Saat, und er muss die richtige Hälfte sein | 234 |
| `x25519` | Schlüsselpaar, rohe Bytes, Gegenschlüssel, gemeinsames Geheimnis | 260 |
| `x25519FromDocument` | öffentlicher Teil eines FESTEN Schlüssels aus einem PKCS#8-Dokument | 293 |
| `p256FromDocument` | P-256 aus einem PKCS#8-Dokument, Signatur, Prüfung – was der Passkey-Weg braucht | 347 |

| Haken | Zeile | Verlangt |
|---|---|---|
| `skipWithoutShell` | 465 | `curves` |
| `skipWithoutLogin` | 470 | `ed25519` + `x25519` + `ed25519FromSeed` |
| `skipWithoutSetup` | 478 | dasselbe wie `skipWithoutLogin` |
| `skipWithoutSeedKeys` | 413 | `ed25519` + `ed25519FromSeed` |
| `skipWithoutPasskey` | 434 | `ed25519` + `x25519` + `p256FromDocument` |
| `skipWithoutSignatures` | 524 | `ed25519` |
| `skipWithoutHandshake` | 511 | `x25519` |
| `skipWithoutSessionKeys` | 530 | `x25519` |
| `skipWithoutFixedKeys` | 516 | `x25519` + `x25519FromDocument` |
| `skipWithoutCDPAuthenticator` | 506 | nichts davon – die WebAuthn-Domäne von CDP |

Dieses System ist **sorgfältig gebaut und trägt**. Die Datei schreibt selbst auf, was
zwei frühere Fassungen gekostet haben (Zeile 84–101): ein Haken, der nur `generateKey`
fragte, ließ 157 Fälle in firefox rot laufen; die Reparatur danach war zu streng und
schaltete zehn grüne firefox-Fälle still ab, darunter eine Sicherheitsprüfung. Daraus
die Regel in der Datei: **eine Lücke je Vorgang, und ein Fall fragt nach dem, was er
wirklich tut.** Diese Regel gilt weiter und wird hier nicht angetastet.

### System B: elf lokale Kopien eines lockereren Hakens

Daneben stehen elf eigene, voneinander unabhängige Kopien einer Funktion namens
`hasEd25519`, die **nicht** über `fixtures/page.js` gehen:

| Datei | Zeile |
|---|---|
| `apps/web/tests/contracts/catalog.spec.js` | 25 |
| `apps/web/tests/contracts/core.spec.js` | 146 |
| `apps/web/tests/contracts/proof.spec.js` | 23 |
| `apps/web/tests/contracts/session.spec.js` | 22 |
| `apps/web/tests/contracts/sign.spec.js` | 16 |
| `apps/web/tests/contracts/task.spec.js` | 19 |
| `apps/web/tests/contracts/trust.spec.js` | 18 |
| `apps/web/tests/contracts/tunnel.spec.js` | 18 |
| `apps/web/tests/shell/store.spec.js` | 106 |
| `apps/web/tests/shell/trustlist.spec.js` | 42 |
| `apps/web/tests/shell/users.spec.js` | 411 |

Jede fragt dasselbe und nur dieses:

```js
await crypto.subtle.importKey("raw", b64decode(pubB64), { name: "Ed25519" }, false, ["verify"]);
```

**Das ist genau der Haken, den `fixtures/page.js` als unzureichend abgelöst hat** – er
prüft eine Vorbereitung, nicht die Wirkung (`messen.md`, „geprüft wurde die Form, nicht
die Wirkung"). In webkit sagt er richtig Nein, weil dort die Kurve fehlt. In firefox
sagt er Ja, weil firefox `importKey("raw")` kann – und lässt damit jeden Fall laufen,
der danach an `exportKey("jwk")` stirbt.

**Also: die Übersprünge in `contracts/` kommen aus System B, die in `shell/` aus
System A.** Das ist die Trennung, die der Auftrag verlangt, und sie liegt eine Ebene
tiefer als vermutet: nicht zwei Engines mit einem Haken, sondern zwei Haken-Systeme
verschiedener Güte am selben Satz.

---

## 3. Die drei Tore im Auslieferungscode

Entscheidend für die Frage „kann sich dort ein Mensch anmelden", und sie steht nicht im
Prüfsatz, sondern in `apps/web/public/assets/js/shell/main.js`:

| Tor | Zeile | Fragt | Was ein Nein bedeutet |
|---|---|---|---|
| `browserFits()` | 22 | `generateKey` für Ed25519 und X25519 | **Keine Maske.** Der Mensch liest `boot.tooold` und kommt nicht weiter |
| `passwordLoginFits()` | 66 | öffentlicher Teil einer Passwort-Saat, und er muss die Signatur prüfen | Passwortfeld aus, Passkey bleibt |
| `passkeyFits()` | 119 | `navigator.credentials.get` ist eine Funktion | Passkey-Knopf aus |

Der Code hat also **drei** Stufen, nicht eine. Was die Datei selbst dazu aufschreibt
(Zeile 50–57) ist der Grund, warum das Blatt überhaupt nötig ist: vor dem zweiten Tor
kam in firefox die Maske, das Passwort wurde genommen, Argon2id lief anderthalb
Sekunden, und die Antwort war „Anmeldung fehlgeschlagen." – **der Satz für ein falsches
Passwort**, obwohl kein Passwort der Welt hineingekommen wäre. Das ist dieselbe Falle,
die `messen.md` als „ein verschluckter Fehler wird zur Falschauskunft" führt, und sie
ist hier behoben.

---

## 4. Fähigkeitstafel je Engine

> **Noch nicht gemessen.** Ein Lauf braucht einen Platz von der Leitsitzung, und am
> 31.10.2026 stand `(Get-NetTCPConnection -State TimeWait).Count` bei **19 528** –
> über der Obergrenze von 10 000 aus dem Auftrag. Die Sonde liegt fahrbereit in
> `apps/web/tests/shell/krypto-tafel.spec.js`.

Die Sonde ist ein **Messgerät und kein Prüfschritt**: sie behauptet über keine Engine
etwas, kann darum an keiner rot werden, und eine Engine, die sich morgen ändert, ändert
nur ihre Ausgabe. Das ist der Grund, warum sie dauerhaft im Repo stehen darf.

Was ein Lauf auf diesem Rechner braucht, gemessen am 31.10.2026 **vor** dem Lauf, damit
kein Platz fürs Einrichten draufgeht:

| Posten | Stand |
|---|---|
| `apps/web/tests/node_modules` | **Je Worktree eigen.** Ein frischer Worktree hat keines; `npm install` dort, 3 Pakete in 4 s aus dem Zwischenspeicher. Weder global noch im Hauptcheckout liegt Playwright |
| Browser | **Geteilt** in `C:\Users\User\playwright-browsers` über die Benutzervariable `PLAYWRIGHT_BROWSERS_PATH`. Alle drei liegen: `chromium-1193`, `firefox-1490`, `webkit-2203`, passend zu Playwright 1.55.1 – kein Download |
| `D:\claude\package.json` | **Gibt es nicht mehr**, und `D:\claude\node_modules` auch nicht. Die alte ESM-Falle („`type: commonjs` fängt die Suche ab") ist damit aus zwei unabhängigen Gründen weg: diese Datei ist fort, und `apps/web/package.json` trägt `"type": "module"`. Sonde grün, Ausstieg 0 |
| Port | `NGC_WEB_TEST_PORT=8201`. Auf der Vorgabe 8099 findet ein zweiter Lauf einen schon antwortenden Server und prüft dann **den anderen Arbeitsbaum, ohne es zu sagen** (`playwright.config.js:10-16`) |

Warum eine eigene Sonde und nicht `sessionKeyGaps`: die Lücken dort sind **Ketten**
und brechen bei der ersten gefallenen Stufe ab. Sie sagen darum die **erste** Grenze,
nicht die **ganze** Fähigkeit – und für „was kann die Engine wirklich" braucht es jede
Zelle einzeln. Was bisher aus der Kette bekannt ist, steht in `fixtures/page.js:124`
und ist dort mit Datum und Befehl belegt (18.10.2026, playwright 1.55.1):

| Engine | `curves` | `ed25519` | `ed25519FromSeed` | `x25519` | `x25519FromDocument` | `p256FromDocument` |
|---|---|---|---|---|---|---|
| chromium 140 | – | – | – | – | – | – |
| firefox 141 | – | – | `OperationError` | – | `OperationError` | – |
| webkit 26 | `NotSupportedError` bei `generateKey` | (verdeckt) | `NotSupportedError` | (verdeckt) | (verdeckt) | – |

„–" heißt: kann es. „(verdeckt)" heißt: die Kette ist vorher abgebrochen, **es ist
nicht gemessen**. Genau diese vier Zellen füllt die Sonde.

---

## 5. Kommt eine Anmeldung durch?

> **Noch nicht gemessen**, siehe Abschnitt 4. Die Sonde fährt gegen die örtliche Anlage
> des Prüfsatzes (`mock/api.js` plus der PHP-Server davor), nicht gegen die
> sechs-VM-Anlage und nicht gegen ngc-ein.

Was aus dem Quelltext zu erwarten ist, steht als E3 und E4 in Abschnitt 1.

---

## 6. Die Grenze des Prüfgerüsts ist nicht die Grenze der Welt

Playwrights webkit ist **nicht** Safari. Es ist ein Bau von WebKit, den Playwright
mitbringt, und Safari 17 und neuer hat Ed25519 im Web-Crypto. Die vorhandene Messung
sagt also:

- **gemessen:** in Playwrights webkit 26 fehlen die CFRG-Kurven.
- **nicht gemessen und mit diesem Prüfsatz nicht messbar:** ob sie in einem echten
  Safari fehlen.

Nach `messen.md` („eine Abwesenheit belegt man mit Orten, nicht mit Suchern") ist das
der Unterschied zwischen einem Befund und einer Gewohnheit. Ein Satz wie „in Safari
kann sich niemand bei NGC anmelden" ist durch diesen Prüfsatz **nicht** belegt. Was ihn
belegen oder widerlegen würde, ist ein echtes Safari auf einem Apple-Gerät – auf diesem
Rechner nicht vorhanden, und darum hier als offener Punkt und nicht als Ergebnis.

---

## 7. Wege je fehlender Fähigkeit

Nach Rangfolge **Sicherheit > Stabilität > Geschwindigkeit**.

### Vorab und verbindlich: eine Krypto-Lücke wird nicht mit eigenem Code umgangen

`CLAUDE.md` dieses Repos: „Kryptografie nur aus der Standardbibliothek oder aus
`vendor/`, nichts selbst gebaut." Das heißt für dieses Blatt:

- Eine Ed25519-Umsetzung in JavaScript selbst schreiben, weil webkit die Kurve nicht
  hat: **verboten.** Nicht „ungünstig", nicht „abzuwägen" – verboten, und dieser Absatz
  steht hier, damit niemand es für ein Versehen hält.
- Der zulässige Weg für dieselbe Lücke ist eine **geprüfte Bibliothek in `vendor/`**,
  mit Zeile in `THIRD-PARTY.md` und AGPL-verträglicher Lizenz, und neue Module werden
  vorher beim Koordinator angefragt. Im Baum liegt bereits `noble-hashes` (für
  Argon2id), also ist der Weg beschritten und nicht neu.
- Für den Austausch von Schlüsseln und Signaturen ist eine Umsetzung ohne
  Seitenkanalschutz zusätzlich ein **Sicherheitsrückschritt**, nicht nur ein
  Regelverstoß: der private Schlüssel eines Menschen liegt dann in einer Sprache mit
  Zwischenspeicher-Zeitverhalten, das niemand hier messen kann.

### Lücke 1 – webkit: die CFRG-Kurven fehlen (`generateKey` → `NotSupportedError`)

| Weg | Kosten | Schaden |
|---|---|---|
| **1a. Nichts bauen, nur richtig sagen.** Das Tor `browserFits()` bleibt, der Satz `boot.tooold` wird so geschrieben, dass er den Browser und die fehlende Fähigkeit benennt und einen Browser empfiehlt, der sie hat – in allen fünf Sprachen. | Ein Sprachschlüssel je Sprache, ein Fall | **Kein Schaden.** Ein Mensch in webkit kommt nicht hinein, erfährt aber richtig, warum, und kann handeln. Sicherheit unverändert. Das ist der Stand heute, nur mit einem besseren Satz |
| **1b. Ed25519 und X25519 aus `vendor/`** nachladen, wenn die Engine sie nicht hat, und die Krypto über diese Bibliothek führen. | Ein neues Modul (Anfrage beim Koordinator), `THIRD-PARTY.md`, Vektorentests gegen `shared/testdata/`, Bau- und Ladeweg, Fälle für den Rückfallpfad | **Zwei Wege im Programm**, und nach `messen.md` braucht der schnelle Weg eine Gegenprobe gegen den langsamen **im Prüfsatz**. Dazu: Schlüssel im JavaScript-Heap statt in der Engine, also ein Sicherheitsrückschritt gegenüber Web-Crypto. Teuer und riskant |
| **1c. Den Passkey-Weg auch dort öffnen.** `p256FromDocument` ist in webkit LEER – P-256 ist vorhanden. Ein Passkey-Login braucht auf dieser Seite keine CFRG-Kurve. | Zu messen: hängt die Sitzung nach der Anmeldung doch an X25519? Nach `fixtures/page.js:429` verlangt `passkeyGap` auch `ed25519` und `x25519` – dann trägt der Weg nicht | **Hängt an der Messung.** Wenn die Sitzungsschlüssel X25519 brauchen, hilft P-256 nur bis zur Anmeldung und nicht weiter. **Erst messen, dann bewerten** – das ist der Grund, warum Abschnitt 4 vor Abschnitt 7 kommt |

**Empfehlung: 1a jetzt, 1c messen, 1b nicht.** 1b verstößt gegen keine Regel, wenn das
Modul aus `vendor/` kommt – aber es verdoppelt den Krypto-Weg für eine Engine, deren
Grenze (Abschnitt 6) nicht einmal belegt eine Grenze echter Nutzer ist.

### Lücke 2 – firefox: `exportKey("jwk")` eines eingeführten PKCS#8-Schlüssels

Das ist **eine einzige Stelle**: Web-Crypto gibt keinen öffentlichen Teil eines privaten
Schlüssels heraus, und firefox rechnet ihn aus einer Ed25519-Saat nicht aus, während
chromium und webkit es tun. Betroffen sind `proof.publicFromSeed` (daran hängt die
Passwort-Anmeldung) und `noise.publicOf`.

| Weg | Kosten | Schaden |
|---|---|---|
| **2a. Passkey statt Passwort**, und die Maske sagt es. Ist im Code **schon gebaut** (`main.js:66` und `:119`, `auth/login.js` `passwordKeys`). | Nichts zu bauen; zu messen, ob es wirklich greift (E4) | **Kein Schaden, und es ist der sicherere Weg sowieso** – Passkeys statt Passwörter ist die Richtung, in die NGC ohnehin zeigt (`HINT` in `main.js:19`). Preis: eine Installation ohne Relying Party hat in firefox keinen Weg hinein |
| **2b. Den öffentlichen Teil aus `vendor/` rechnen** – nur die eine Stelle, nur die Punktmultiplikation auf der Kurve, der private Schlüssel bleibt sonst in Web-Crypto. | Ein Modul aus `vendor/`, Vektorentests, eine Gegenprobe gegen chromium Byte für Byte | **Verboten als Eigenbau, zulässig aus `vendor/`.** Sicherheitlich milder als 1b, weil nur der ÖFFENTLICHE Teil berechnet wird – eine falsche Zahl fällt sofort auf, denn sie prüft keine Signatur. Trotzdem: der Saatwert geht durch JavaScript-Code |
| **2c. Den öffentlichen Teil vom Server holen.** | Wenig Code | **Nicht zulässig.** Der Server erfährt dann etwas, was aus dem Passwort folgt, und die Passwort-Anmeldung stützt sich darauf, dass er das nicht tut. Hier aufgeschrieben, damit es niemand als Abkürzung vorschlägt |
| **2d. Nichts tun.** | Nichts | Ein Mensch in firefox ohne Passkey liest heute, dass sein Passwort nicht geht – richtig, aber ohne Weg. **Nur tragbar, solange 2a trägt** |

**Empfehlung: 2a bestätigen (messen), 2b als Rückfall vorbereiten, 2c nie, 2d nicht.**

### Lücke 3 – die elf lockeren Haken aus System B (Abschnitt 2)

Kein Krypto-Problem, ein Messproblem – und nach `messen.md` das gefährlichere der
beiden, weil ein übersprungener Fall wie ein bestandener aussieht.

| Weg | Kosten | Schaden |
|---|---|---|
| **3a. Die elf Kopien auf die Haken aus `fixtures/page.js` umstellen**, je Datei auf den, der dem entspricht, was die Datei wirklich tut. | Elf Dateien, je zwei Zeilen; je Datei eine Entscheidung, welcher Haken der richtige ist | **Der Haken kann zu streng werden** – genau der Fehler, den `fixtures/page.js:94` schon einmal bezahlt hat (zehn grüne firefox-Fälle still abgeschaltet, eine Sicherheitsprüfung darunter). Darum **je Datei einzeln** und mit einer Zahl vorher und nachher, nicht in einem Zug |
| **3b. So lassen.** | Nichts | In firefox laufen Fälle an, die an `exportKey` sterben, und ihr Rot sieht nach einem Fehler im Code aus. Das ist der Zustand, aus dem der Auftrag kommt |

**Empfehlung: 3a, aber datei‑ und schrittweise, mit der Fallzahl je Engine vor und nach
jedem Schritt.** Nicht als ein PR.

---

## 8. Offene Punkte

1. Die vier verdeckten Zellen der webkit-Zeile in Abschnitt 4 – braucht einen Lauf.
2. Kommt eine Anmeldung in firefox über Passkey durch (E4)? Braucht einen Lauf.
3. Trägt 1c, oder brauchen die Sitzungsschlüssel in webkit doch X25519? Braucht einen Lauf.
4. Hat ein echtes Safari die Kurven? **Auf diesem Rechner nicht messbar** (Abschnitt 6).
5. Der gemeldete Satzlauf vom 31.10.2026 (chromium 302/137/2, webkit 147/0/134) hat
   **firefox nicht dabei**, obwohl `apps/web/tests/playwright.config.js` alle drei
   Projekte führt. Die am engsten betroffene Engine fehlt in der Zahl.
