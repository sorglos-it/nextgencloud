# Warum ein gescheiterter Anmeldeversuch nichts verrät – und wie der Prüfsatz es trotzdem erfährt

Gemessen am 03.11.2026 im Arbeitsbaum `D:\claude\working\ngc-login-fehlerklassen`
(lokale Platte C:/D:, nicht das Netzlaufwerk), Zweig `web-login-fehlerklassen`,
Grundstand `origin/main` = `0471b02d`. Fehlende Dateien im Baum: 0
(`git diff --name-only --diff-filter=D HEAD | wc -l`).
Kein Browser gelaufen; alle Zahlen aus `grep`, `sed`, `node` und
`npx playwright test --list` (startet keinen Browser, bindet keinen Port).

Die Erwartung wurde **vor** der Messung aufgeschrieben; wo sie falsch war, steht es
unten bei „Was die Erwartung nicht getroffen hat".

## Die Spannung

Nach **außen** muss ein gescheiterter Anmeldeversuch immer dasselbe sagen.
`apps/server-api/classes/login/finish.go` gibt für unbekannten Namen, falschen
Beweis und verbrauchten Einmalwert denselben Code `E_LOGIN`, und die Maske
zeichnet dafür einen einzigen Satz (`apps/web/public/assets/js/auth/login.js:43`,
Schlüssel `login.failed`). Das ist Absicht und bleibt.

Nach **innen** kostet es: ein voller Webprüfsatz hatte am 31.10.2026 137 rote
Fälle, 131 davon mit `expect(locator).toBeVisible() failed` – „der Schreibtisch
kam nicht". Welcher der 25 möglichen Gründe es war, stand in keinem der 131
Berichte.

Der Auftrag ist darum **nicht** „mehr verraten", sondern: der Prüfsatz muss die
Klasse erfahren, ohne dass die Oberfläche einem Angreifer mehr sagt als heute.

## Schritt 1a – welche Fehlerklassen auf den einen Satz laufen

`login()` (login.js:112–221) wirft, `attempt()` (login.js:413–479) fängt. Drei
Wege haben eine eigene Antwort, alle übrigen laufen auf `FAILED()` in
login.js:472.

### Die drei Wege mit eigener Antwort

| Weg | Stelle | Was gezeichnet wird |
|---|---|---|
| `NeedTOTP` | login.js:134, gefangen 426 | Frage nach dem Code, kein Fehlschlag |
| `code === E_LOCKED` | login.js:435 | Sekunden, die der Mensch sieht |
| `code === E_UNREACHABLE` | login.js:472 | zwei Sätze, je nach `Retry-After` |

### Die Klassen, die alle denselben Satz bekommen

Spalte „Herkunft": **außen** = der Server hat ein Nein geschickt,
**innen (Code)** = der Browser setzt selbst einen `wire`-Code,
**innen (nackt)** = gewöhnlicher `Error` ohne Feld `code`.

| # | Klasse | Stelle | Herkunft |
|---|---|---|---|
| 1 | `E_LOGIN` – unbekannter Name, falscher Beweis, verbrauchter Einmalwert | `net/http.js:186` aus `close.code` | außen |
| 2 | jeder andere Code der API (`E_INTERNAL`, `E_NOTFOUND`, `E_TOOLARGE`, `E_CONFIRM` …) | `net/http.js:186` | außen |
| 3 | Antwort verletzt den Vertrag → `E_PROTOCOL` | `net/http.js:50–56` | innen (Code) |
| 4 | Name entspricht der Grammatik nicht → `E_PROTOCOL` | `net/http.js:227` | innen (Code) |
| 5 | leere Antwort, wo Bytes erwartet waren → `E_PROTOCOL` | `net/http.js:194` | innen (Code) |
| 6 | eigene Anfrage zu groß → `E_TOOLARGE` | `net/http.js:144` | innen (Code) |
| 7 | Socket-Adresse nicht nach der Grammatik → `E_PROTOCOL` | `net/http.js:268` | innen (Code) |
| 8 | „dieser Name hat keinen Passkey" | `auth/login.js:129` | innen (nackt) |
| 9 | „keine Vertrauensliste" | `auth/login.js:101` | innen (nackt) |
| 10 | Sitzungsschlüssel kamen nicht (`OperationError`, `NotSupportedError` von Web Crypto) | `auth/keys.js:28` | innen (nackt) |
| 11 | eigenes Zertifikat hält den Vertrag nicht | `auth/keys.js:52` | innen (nackt) |
| 12 | Authenticator gab nichts her | `auth/keys.js:99` | innen (nackt) |
| 13 | Authenticator gab einen anderen Berechtigungsnachweis | `auth/keys.js:104` | innen (nackt) |
| 14 | Mensch bricht ab oder 60-s-Frist am Authenticator (`NotAllowedError`) | `auth/keys.js:95` | innen (nackt) |
| 15 | kein Worker für Argon2id | `contracts/proof.js:259` | innen (nackt) |
| 16 | **Argon2id antwortet nicht – die gerissene Frist** | `contracts/proof.js:286–288` | innen (nackt) |
| 17 | Argon2id gab keine Saat / scheiterte | `contracts/proof.js:278`, `:281`, `:282`, `:297` | innen (nackt) |
| 18 | KDF-Werte außer Bereich | `contracts/proof.js:245` | innen (nackt) |
| 19 | Saat oder Export des Passwortschlüssels unbrauchbar | `contracts/proof.js:457`, `:459`, `:515` | innen (nackt) |
| 20 | „session: malformed certificate" (API-Zertifikat) | `contracts/session.js:287`, `:328`, `:339` | innen (nackt) |
| 21 | **„session: bad signature"** | `contracts/session.js:332` | innen (nackt) |
| 22 | API-Zertifikat zur Uhrzeit der API nicht gültig | `contracts/session.js:352` | innen (nackt) |
| 23 | „trust: invalid list" | `contracts/trust.js:238` | innen (nackt) |
| 24 | „link: no connection" (Socket zu, falsches Unterprotokoll, Text statt Rahmen) | `net/link.js:80`, `:96` | innen (nackt) |
| 25 | Noise-Handschlag scheitert (falscher Schlüssel, falscher Rahmen) | `contracts/noise.js:41`, `:276`–`:312` | innen (nackt) |

**Zwei Korrekturen am Befund des Auftrags**, beide gemessen:

- `login: no way to open the trust list` gibt es in `origin/main` **nicht**
  (`grep` über `apps/web/public/assets/js`: 0 Treffer). Entweder liegt der Satz
  auf einem anderen Zweig oder er ist aus der Erinnerung.
- `TrustBroken` (`shell/trustlist.js:68`) erreicht die Anmeldemaske **nicht**.
  `shell/main.js:268` übergibt **kein** `openTrust`, also läuft die
  Behelfslösung `openTrustFromChain` (login.js:99–104); `trustlist.js` greift
  erst **hinter** der Anmeldung, und `shell/main.js:270–276` hat dafür einen
  eigenen Satz („NGC konnte nicht geladen werden."). `TrustBroken` ist eine
  Klasse eines anderen Schirms.

**Ein Fund nebenbei, und er ist der Kern der Blindheit:**
`net/http.js:50–56` fängt jeden Fehler des Vertragslesers und ersetzt ihn durch
`E_PROTOCOL`. Die genauere Ursache (`session: malformed message`,
`contracts/session.js:446`, `:449`, `:458`, `:473`) ist damit **schon eine Schicht
unter der Maske verloren** – nicht erst an der Maske. Der Kommentar darüber
(http.js:44–49) sagt das ausdrücklich und nennt den Preis sowie den einzigen Weg
daran („einen Haltepunkt auf das `throw` setzen"). Das Verschlucken ist begründet:
die Antwort kann ein Zertifikat tragen, und die ginge mit in jedes Protokoll.

Und: ein `code` am Fehler bedeutet **nicht** „der Server hat das gesagt".
Die Klassen 3 bis 7 setzt der Browser selbst. Wer die Klasse am `code` ablesen
will, liest an fünf Stellen die eigene Vorrichtung.

## Schritt 1b – was heute abgedeckt ist, in Fällen

Fallzahlen aus `npx playwright test --list`, je Engine gezählt (chromium),
Gesamtzahl über drei Engines in Klammern:

| Ebene | Fälle | davon, die eine Klasse unterscheiden |
|---|---|---|
| Anmeldemaske (`shell/login.spec.js`) | 17 (51) | **0** |
| `contracts/session.spec.js` | 17 (51) | Zusicherungen auf 16 Zeilen |
| `contracts/proof.spec.js` | 20 (60) | Zusicherungen auf 9 Zeilen (Argon2id) |
| `contracts/trust.spec.js` | 15 (45) | Zusicherungen auf 2 Zeilen |

An der **Maske** prüfen genau **3** Fälle je Engine (9 über drei Engines) den
einen Satz, und keiner davon unterscheidet, was darunter lag:

- `shell/login.spec.js:224` „a wrong password says only that the login failed" –
  prüft zweimal (`:232`, `:235`), dass falsches Passwort und unbekannter Name
  **denselben** Satz geben. Das ist die lebende Gegenprobe, die es schon gibt.
- `shell/login.spec.js:363` „the passkey button never falls back to the password",
  zweimal parametrisiert (zwei Authenticatoren), Zusicherung auf `:393`.

Die 19 Dateien, die über `fixtures/page.js:34` `signIn()` anmelden, prüfen den
Satz nie – sie warten auf den Schreibtisch. Von dort kommen die 131 roten Fälle.

**Die Abdeckung ist also nicht „nichts".** Die Klassen 15 bis 23 sind **an der
Stelle, an der sie entstehen**, mit Gleichheit festgenagelt – tabellengetrieben,
in `page.evaluate`, mit dem Fehlertext als erwartetem Wert (zum Beispiel
`contracts/session.spec.js:74`, `:77`, `:84`, `:214`, `:325`, `:607`, `:722`,
`:785`, `:894`). Was fehlt, ist die Verbindung zwischen einem **ganzen**
gescheiterten Anmeldeversuch und seiner Klasse.

## Schritt 1c – was ein Prüffall heute beim Fehlschlag sieht

Gemessen, nicht angenommen:

- **Nur den Satz.** `say()` (login.js:338) schreibt in `said` (login.js:294),
  ein `note("", "error")` mit `role="alert"`. Kein Merkmal am Element, kein
  Ereignis, kein Zustand am Formular.
- **Kein Konsolenweg.** `grep -rn "console\." apps/web/public/assets/js` ergibt
  **0** Treffer im ganzen Webteil (Grundmenge geprüft: `grep -rn "throw "`
  derselben Bäume ergibt 399 Treffer, der Lauf zählt also nicht im Leeren).
- **Kein Haken an `window`.** Der einzige Testhaken ist `window.__ngcStandIn`
  (`fixtures/page.js:74`), und der schaltet die Sitzung um, er meldet nichts.
- **Eine Nebentür gibt es doch**, und sie ist der Gegenbeweis unten.

## Schritt 1d – der Gegenbeweis: zwei Wege, die es schon gibt

**1. Die Attrappe kann gefragt werden.** `mock/api.js:1104–1137` hat eine eigene
Prüftür `/api/v1/mock`: `ask=proofs` (welche Beweise die letzte Anmeldung trug),
`ask=finishes` (wie oft `/login/finish` angekommen ist, ein Zähler, der nur
wächst), `ask=setup`, `ask=task`. Genutzt wird sie an vier Stellen
(`shell/login.spec.js:333`, `:357`, `:386`, `:396`, dazu `setup.spec.js` und
`world.spec.js`). `shell/login.spec.js:363` beweist damit **ohne** Hilfe der
Oberfläche, dass der Versuch **vor** der API starb: `ask=finishes` steht vorher
und nachher gleich. Genau diese Trennung – „vor der API" gegen „die API sagte
nein" – fehlt den 131 roten Fällen, und das Werkzeug dafür liegt da.

Die Attrappe kann auch **angewiesen** werden: `lock=`, `fault=busy|away`,
`setup=1`, `failAfterData=1` (`mock/api.js:1109–1124`). Für die Klassen 8 bis 25
gibt es **keinen** solchen Schalter – man kann sie heute nicht herbeiführen.

**2. Der Konsolenkanal ist im Prüfstand schon verdrahtet, nur ohne Sender.**
`shell/setup.spec.js:202` sammelt `shell.on("console", …)` – und zwar als
**Geheimnisprüfung**: der Fall belegt, dass der Einmal-Code dort nicht auftaucht.
Ein künftiger Sender auf der Konsole hätte damit von Tag eins einen Wächter, der
rot wird, sobald ein Geheimnis mitgeht.

Der Befund „WARUM er nicht kam, stand nirgends" ist also zu scharf. Richtiger:
**es gibt eine Nebentür, sie trennt aber nur „vor der API" von „nach der API",
und keiner der 131 Fälle benutzt sie.**

## Was die Erwartung nicht getroffen hat

- Erwartet: ein `catch`, das alles gleich behandelt. Gemessen: **drei** Wege
  haben schon eine eigene Antwort (`NeedTOTP`, `E_LOCKED`, `E_UNREACHABLE` mit
  zwei Sätzen). Die Maske ist feiner als der Befund sagt.
- Erwartet: 6 bis 10 Klassen. Gemessen: **25**, davon 22 auf dem einen Satz.
- Erwartet: 0 bis 1 Prüffälle pro Klasse. Gemessen: die Klassen sind **am
  Entstehungsort gut abgedeckt**; blind ist nur die Maske.
- Erwartet: der `code` trennt außen von innen. Gemessen: **falsch** – fünf
  Klassen setzen den `code` im Browser.
- Erwartet: vielleicht ein ungenutztes `console.error`. Gemessen: **null**
  Konsolenaufrufe im ganzen Webteil; die Nebentür ist stattdessen die Attrappe.

## Schritt 2 – drei Wege, Rangfolge Sicherheit > Stabilität > Geschwindigkeit

### Weg 1 (der naheliegende und der gefährlichste): die Oberfläche sagt den Grund

Je Klasse ein eigener Satz, oder ein Zusatz wie „(E_PROTOCOL)" hinter dem
bestehenden.

- **Nutzen:** jeder der 131 Berichte nennt die Ursache, ohne eine Zeile im
  Prüfsatz. Billigster Bau.
- **Kosten:** eine Vertragsänderung an `shared/wire` oder eine zweite
  Satztabelle in fünf Sprachen.
- **Was ein Angreifer gewinnt: die Namensprüfung, und zwar geschenkt.** Heute
  sind unbekannter Name, falsches Passwort und verbrauchter Einmalwert
  ununterscheidbar (`finish.go`, `deny()`). Trennt die Maske sie, kann jeder
  ohne Zugangsdaten eine Namensliste abklappern und erfährt, welche Konten es
  gibt – die Vorarbeit für jeden gezielten Angriff auf einen Menschen. Dazu
  verrät schon die Klasse **vor** der API etwas: „dieser Name hat keinen
  Passkey" (Klasse 8) sagt, dass der Name existiert, und das steht sogar als
  Begründung im Code (login.js:124–127). Und „session: bad signature" gegen
  „trust: invalid list" erzählt einem Angreifer, wie weit er mit einem
  gefälschten API-Zertifikat gekommen ist – ein Orakel zum Basteln.
- **Urteil: fällt an der Rangfolge aus.** Dieser Weg verkauft Sicherheit für
  Bequemlichkeit im Prüfsatz. Er bleibt hier nur stehen, damit niemand ihn
  später für die einfache Lösung hält.

### Weg 2: ein Merkmal, das nur im Prüfbetrieb gesetzt wird

Zum Beispiel `data-why="session/bad-signature"` am Element `said`, geschrieben
nur wenn ein Schalter steht.

- **Nutzen:** der Prüffall liest die Klasse am Schirm, die Oberfläche sagt
  weiter einen Satz, und es trägt auch für die 19 Dateien, die nur anmelden.
- **Kosten:** eine Zeile in login.js, eine Verzweigung mehr.
- **Woran hängt „Prüfbetrieb", und kann ein Angreifer ihn einschalten?** Das ist
  die ganze Frage, und die Antwort ist unangenehm. Die beiden Schalter, die es
  im Repo gibt, sind **beide** vom Browser aus setzbar: `window.__ngcStandIn`
  (`fixtures/page.js:74`) setzt der Prüfstand per `addInitScript`, und ein
  Angreifer mit einer Skriptlücke setzt dieselbe Variable. Eine Herkunft (nur
  auf `localhost`) hilft nicht: die Anmeldung läuft genau dort auch echt, und
  die Passkey-Fälle verschieben die Seite ohnehin auf einen DNS-Namen
  (`fixtures/page.js:57–59`). Ein Schalter aus der Konfiguration
  (`/config.json`) wäre eine Zeile, die auf einer echten Anlage versehentlich
  stehen bleiben kann – und dann hat die Anlage dauerhaft Weg 1.
- **Was ein Angreifer gewinnt:** findet er den Schalter oder kann er ihn setzen,
  gewinnt er **genau das von Weg 1** – die Namensprüfung. Der Unterschied ist
  nur, dass es dann wie ein Unfall aussieht statt wie eine Entscheidung.
- **Urteil: zweite Wahl.** Tragfähig nur, wenn der Schalter **nicht** zur
  Laufzeit erreichbar ist – also beim Ausliefern entfernt wird, nicht zur
  Laufzeit abgefragt. Das kann dieses Projekt heute nicht: `apps/web` hat
  bewusst **keinen Bauschritt** (CLAUDE.md, „Webinterface ohne Framework und
  ohne Build-Schritt"). Ohne Bauschritt gibt es keinen Ort, an dem so eine Zeile
  verschwinden könnte. Damit scheidet Weg 2 nicht aus Geschmack aus, sondern an
  einer gemessenen Eigenschaft des Projekts.

### Weg 3 (empfohlen): die Klasse dort abfangen, wo sie entsteht – und der Prüfsatz fragt die Attrappe

Drei Teile, keiner rührt die Oberfläche an:

1. **Je Klasse ein Fall am Entstehungsort**, wie es `contracts/session.spec.js`
   schon tut: der geworfene Text mit Gleichheit festgenagelt, in
   `page.evaluate`. Für die Klassen ohne Fall (10, 11, 13, 14, 24, 25) kommen
   solche Fälle dazu. Die Oberfläche erfährt nichts Neues, weil kein Wert sie
   erreicht.
2. **Schalter an der Attrappe**, damit sich eine Klasse überhaupt herbeiführen
   lässt: `fault=badsig`, `fault=badlist`, `fault=nochain` neben dem
   bestehenden `fault=busy|away` (`mock/api.js:1116`). Die Attrappe liegt in
   `apps/web/tests` und wird **nicht ausgeliefert** – ein Angreifer an einer
   echten Anlage hat sie nicht.
3. **Der Prüfsatz fragt die Nebentür**, statt den Schirm zu deuten: ein
   Helfer in `fixtures/page.js`, der nach einem gescheiterten `signIn()`
   `ask=finishes` und `ask=proofs` liest und in die Fehlermeldung des Falls
   schreibt. Damit nennt jeder der 131 Berichte mindestens „der Versuch starb
   vor der API" oder „die API sagte nein" – und mit dem Schalter aus Teil 2
   lässt sich der Rest in einem gezielten Lauf einkreisen.

- **Nutzen:** die Diagnose kommt in den Prüfsatz, wo sie gebraucht wird, nicht
  in die Oberfläche, wo sie schadet. Teil 3 trägt sofort für alle 19 Dateien.
- **Kosten:** am meisten Arbeit der drei Wege. Teil 1 sind etwa sechs neue
  Fälle, Teil 2 etwa drei Schalter, Teil 3 ein Helfer.
- **Was ein Angreifer gewinnt: nichts.** Kein Byte dieses Wegs liegt auf einer
  ausgelieferten Anlage. `apps/web/tests` ist npm-Gebiet und geht nicht mit
  (CLAUDE.md, „npm nur in `apps/web/tests/`"); `mock/api.js` ist der Prüfserver.
  Die Oberfläche zeichnet weiter genau einen Satz, und `shell/login.spec.js:224`
  bleibt die lebende Gegenprobe darauf.
- **Was er nicht leistet:** er sagt nichts über eine **echte** Anlage. Scheitert
  dort eine Anmeldung, bleibt der Satz der Satz. Für den Betrieb wäre der
  richtige Ort das Protokoll der **API**, nicht der Browser – das ist ein
  eigener Auftrag und steht hier als offener Punkt.

### Empfehlung

**Weg 3.** Er ist der einzige der drei, der einem Angreifer nichts gibt, und er
baut auf zwei Vorrichtungen, die schon gemessen funktionieren (die Prüftür der
Attrappe und die tabellengetriebenen Vertragsfälle). Weg 2 wäre der bequemere,
scheitert aber am fehlenden Bauschritt: ein Schalter, der zur Laufzeit lebt, ist
ein Schalter, den jemand findet. Weg 1 ist abzulehnen.

## Was Schritt 3 beweisen muss

- **Je Fehlerklasse ein sterbender Mutant:** wird die Klasse nicht mehr erkannt
  (der `throw` entfernt oder der Text geändert), muss ein Fall fallen.
- **Eine lebende Gegenprobe:** `shell/login.spec.js:224` muss grün bleiben – die
  Oberfläche zeichnet nach außen weiter genau einen Satz. Stirbt dieser Fall,
  ist die Behebung zu weit gegangen und ein Sicherheitsmangel.
- Dazu nach `messen.md` je Mutantenlauf eine Kontrolle, die **sterben** muss,
  und eine, die **leben** muss.

## Ein Fund über die Messvorrichtung, der jede künftige Zahl betrifft

`npx playwright test --list shell/login.spec.js` nennt Fälle an Zeilen, **die es
in der Datei nicht gibt**. Gemessen am 03.11.2026:

```
Datei (node, fs.readFileSync): 535 Zeilen, 27337 Bytes,
                               git hash-object 113e3be8b0afc87a79efcb16080b304668aa3f98
--list meldet den letzten Fall an Zeile 597
test( steht in der Datei bei 177, 206, 224, …, 521
--list meldet         199, 227, 246, …, 597
```

Der Abstand wächst von +22 auf +76, die **Namen** stimmen alle und die Zahl
stimmt (14 `test(`, davon 3 parametrisiert ×2 = 17 Fälle je Engine). Es ist also
die richtige Datei mit falschen Zeilennummern. Ein geteilter Zwischenspeicher
ist es **nicht**: derselbe Lauf mit eigenem `TEMP` gibt dieselben Zahlen
(gegengeprüft, Ausstiegscode 0 beidemal).

**Folge für alle Berichte:** aus `--list` und aus roten Berichten die
**Fallnamen** übernehmen, nie die Zeilennummern. Eine Zeilennummer aus Playwright
in diesem Prüfsatz zeigt auf nichts. Die Ursache ist nicht gemessen und bleibt
offen.
