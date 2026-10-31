# Erste Anmeldung sagt "Anmeldung fehlgeschlagen."

Erscheinung (Thomas, wörtlich): "der fehler is immer noch drin das er das erste
mal anmelden sagt passwort falsch". Zweiter Versuch mit demselben Passwort geht
durch.

Gelesene Lesart: der erste Versuch JEDER Sitzung (nach jedem Neuladen), nicht
der allererste nach der Ersteinrichtung. Grund: "immer noch drin" heißt, Thomas
erlebt es wiederholt; der allererste Versuch nach einer Ersteinrichtung
passiert nur ein einziges Mal. Frage 2 liegt bei Thomas; bis zur Antwort wird
diese Lesart gemessen.

## Erwartungen, aufgeschrieben VOR der Messung (30.10.2026)

- E1: Das von der Anlage AUSGELIEFERTE shell/main.js ruft loginView ohne
  openTrust auf, so wie main es tut (main.js:268). Dann läuft im Betrieb die
  Attrappe openTrustFromChain, die chain.versions[0] nimmt -- Version 1 der
  Vertrauensliste statt der aktuellen.
  Falsifiziert, wenn im ausgelieferten main.js ein openTrust übergeben wird.
- E2: GET /api/v1/trust?from=1 OHNE Sitzung antwortet E_LOGIN, nicht 204.
  Falsifiziert durch 204 oder 200.
- E3: Die KDF-Parameter der Anlage sind m=65536, t=3. Dann ist die Frist von
  proof.js 108 s und die Argon2-Frist kann die Erscheinung nicht erklären.
  Falsifiziert durch deutlich größere Parameter.
- E4: Die Vertrauensliste der Anlage steht über Version 1. Dann holt die
  Attrappe eine ANDERE Liste als die aktuelle, und session.verifyAPI prüft das
  API-Zertifikat gegen veraltete Schlüssel.
  Falsifiziert, wenn die Liste auf Version 1 steht.

## Was ohne Thomas nicht messbar ist

Ein echter Anmeldeversuch braucht sein Passwort. Ich gebe an einer Anlage kein
Passwort ein (das ist keine Entwicklungsumgebung auf diesem Rechner), also
tippt Thomas, und mitgeschnitten wird Netzwerk und Konsole.

## Messung

Gemessen am Code (30.10.2026). Jede Zeile nennt die Datei, die sie belegt oder
widerlegen würde.

- M1: `apps/server-api/classes/login/trust.go` antwortet 204 nur für die Nummer
  EINE HINTER der neuesten; `from=1` liefert bei einer Liste ab Version 1 immer
  eine Version. **Verdacht 1 ist widerlegt** -- daraus kann kein Fehler
  entstehen, der nur beim ersten Versuch auftritt.
- M2: `finish.go`, `deny()` schreibt KEINEN Grund ins Protokoll (nur eine
  gedrosselte Zeile, wenn die Zählung nicht geschrieben werden konnte). Das
  API-Protokoll nennt den echten Grund also nicht; er ist nur im Browser zu
  holen.
- M3: `apps/web/public/assets/js/shell/main.js:268` ruft `loginView` OHNE
  `openTrust` auf. Im Betrieb läuft damit die als Platzhalter gekennzeichnete
  `openTrustFromChain` (`auth/login.js`), die `chain.versions[0]` nimmt --
  **Version 1** der Vertrauensliste statt der aktuellen.
- M4: `contracts/session.js:320-328`, `verifyAPI` sucht den signierenden Knoten
  per Schlüsselkennung IN DER ÜBERGEBENEN LISTE. Hält Version 1 den heutigen
  Signierschlüssel des API-Knotens nicht, ist `node === null` und es fliegt
  `Error("session: malformed certificate")` -- ein gewöhnlicher Fehler, also
  derselbe eine Satz, und das ERST NACHDEM `loginFinish` durchgelaufen ist:
  Cookie gesetzt, Nonce verbraucht, Sitzungszeile geschrieben.
- M5: `shared/session/session.go:87`, `Path=/api/; Secure; HttpOnly;
  SameSite=Strict`, und `net/http.js` ruft mit `credentials: "same-origin"`.
  Das Cookie erreicht `/api/v1/trust`. „Das Cookie kam nicht an" ist kein
  Pfad- oder Attributproblem.
- M6: `httpapi.Cookie` verlangt GENAU EIN Cookie dieses Namens
  (`httpapi.go:173`). Ein zweites `ngc_session` macht jede Anfrage sitzungslos.
  Das erzeugt die umgekehrte Asymmetrie (erst geht es, später nicht) und ist
  nicht Thomas' Erscheinung -- hier notiert, damit es niemand zweimal sucht.
- M7: Frist für Argon2id in `contracts/proof.js:189-202`: 10 s + 0,5 ms je
  KiB-Durchgang, bei m=65536 t=3 also 108 s. Ein kalter Start erklärt die
  Erscheinung nicht.
- M8: Die Browser-Messung an der Anlage steckt: `https://10.2.3.216/` lädt hier
  nicht, ERR_CERT_AUTHORITY_INVALID. Die Zertifikatsprüfung wird nicht
  abgeschaltet und es wird nicht drumherum gegangen.
- M9: **Das ungeprüfte Stück und das verdächtige Stück sind dasselbe Stück.**
  `tests/shell/login.spec.js` ersetzt `openTrust`/`verifyAPI` per `standInOn`
  (Begründung dort: die Kette der Attrappe passt nicht zum Anker des
  Testservers), und `login.spec.js:402` macht als ERSTE Handlung eine richtige
  Anmeldung und erwartet die Taskleiste. Der erste Versuch ist gegen die
  Attrappe also schon abgedeckt -- die Attrappe kann die Erscheinung darum
  nicht nachstellen.

## Was noch nicht gemessen ist

Die Asymmetrie selbst. Jeder Mechanismus, den ich im Quelltext finde,
scheitert IMMER oder NIE; „erster Versuch tot, zweiter geht" steckt in Zustand,
der zwischen den beiden Versuchen wechselt, und den sehe ich im Code nicht. M3
und M4 sind am Code gemessen; **dass sie Thomas' Erscheinung erzeugen, ist
nicht gemessen** und wird hier nicht behauptet.

Offen, in dieser Reihenfolge: E1, E4 und E3 liest die Leitsitzung über SSH; den
sterbenden ersten Versuch muss Thomas selbst tippen, mit Mitschnitt von Netz
und Konsole. Ein Test, der die erste Anmeldung festhält, kommt danach -- einer
vorher hielte nichts fest.

## An der Anlage gemessen (30.10.2026, über die Leitsitzung per SSH)

- **E1 bestätigt.** `ngc-ein` (10.2.3.216), Webroot `/var/www/ngc`, `apps/web`
  VERSION 0.2.1: die ausgelieferte `shell/main.js` ruft `loginView` mit drei
  Feldern und `onSession` auf, **ohne** `openTrust`. Im Betrieb läuft die
  Attrappe.
- **E4 gestützt, nicht widerlegt.** `ngc-db-1` (10.2.3.212),
  `select kind, id, seq, length(data) from ngc_blob` -- nur Schlüssel und
  Längen: `trust` 1 bis 8, lückenlos. Die Anlage läuft auf **Fassung 8**,
  `chain.versions[0]` holt **Fassung 1**. Sieben Fassungen Abstand.
- **E3 widerlegt.** `ngc-api-1`, `GET /api/v1/login/start` mit einem
  Phantasienamen, 200: `argon2id`, m=65536, t=3, p=1. Damit ist die Frist 108 s
  und Argon2 als Ursache erledigt. (Die Leitsitzung hat dafür auf der Maschine
  selbst `curl -sk` gegen 127.0.0.1 benutzt, also die Zertifikatsprüfung für
  diesen einen lokalen Aufruf übergangen; das ist hier festgehalten, weil eine
  Messung sagen muss, wie sie zustande kam.) Nebenbefund: für einen unbekannten
  Namen `passwordToo` = false, `totp` = false.
- Nebenbei erledigt: 8 Fassungen sprengen keine Grenze.
  `MAX_CHAIN_VERSIONS` = 64, `MAX_CHAIN` = 32 MiB.

### Was E4 ist, und was es nicht ist

E4 ist ein **Sicherheitsmangel für sich**: die Anmeldung prüft das
API-Zertifikat gegen Fassung 1, während die Anlage auf 8 läuft. Ein Knoten, der
in den Fassungen 2 bis 8 aus der Liste GENOMMEN wurde, wird von der Anmeldung
weiter angenommen. Nach der Rangfolge Sicherheit > Stabilität > Geschwindigkeit
gehört das behoben, auch wenn es Thomas' Erscheinung nicht erzeugt.

E4 erzeugt die Erscheinung **wahrscheinlich nicht**, und das ist der Grund:
hält Fassung 1 den heutigen Signierschlüssel des API-Knotens nicht, scheitert
`verifyAPI` bei JEDEM Versuch, auch beim zweiten -- dann käme Thomas nie
hinein. Hält sie ihn, erklärt E4 den ersten Versuch nicht. Die Nachmessung, die
das entscheidet (hält Fassung 1 denselben Signierschlüssel wie Fassung 8?),
liegt bei der Leitsitzung.

Die Asymmetrie selbst bleibt damit ungemessen. Sie misst Thomas am Browser,
nach der Liste, die über die Leitsitzung bei ihm liegt; die eine Frage daran
ist, ob in Durchlauf 1 hinter `login/finish` eine 200 steht.

## Berichtigung, und der Mechanismus (30.10.2026)

**Zwei Dinge oben sind falsch und werden hier richtiggestellt.**

1. **Fassung 8 gehört NICHT zu 10.2.3.216.** Sie ist auf `ngc-db-1` gemessen,
   also an der **Sechs-VM-Anlage**. `ngc-ein` (10.2.3.216) ist eine **eigene,
   getrennte Anlage**: Knotenname `ein`, eine einzige API, keine MariaDB, eine
   örtliche Datei als Datenbank. Deren Fassung ist **unbekannt** -- die Datei
   ist verschlüsselt (`sqlite3`: „file is not a database", Absicht, kein
   Schaden). Für ngc-ein ist E4 unvermessen. E1 dagegen steht: die
   ausgelieferte `main.js` dort übergibt kein `openTrust`.
2. **Mein Satz „ein in 2 bis 8 entfernter Knoten wird weiter angenommen"
   trifft nicht zu.** An der Sechs-VM-Anlage wurde nie ein Knoten entfernt,
   nur hinzugefügt. Der Mangel ist der **umgekehrte**: später hinzugekommene
   Knoten werden **nicht** angenommen.

### Der Mechanismus, am Code gemessen

Kurzmerkmale der Signierschlüssel, Sechs-VM-Anlage, alle acht Fassungen (die
ersten 8 Zeichen eines SHA-256 über den Schlüsseleintrag, keine Schlüsselbytes):

    Fassung 1: api-1 4cdd09a8
    Fassung 2: api-1 4cdd09a8 ; api-2 2d33f2b1
    Fassung 3-8: dieselben beiden, dazu Controller (ohne sign)

- `finish.go:353-357` (`newSession`): das API-Zertifikat wird mit
  `h.S.Cfg.SignKey` signiert, also dem **eigenen** Schlüssel des Knotens, der
  geantwortet hat, und trägt `Node: h.S.Cfg.Node`.
- `contracts/session.js:261` (`apiNodes`) und `:320-328`: `verifyAPI` sucht den
  signierenden Knoten in der **übergebenen** Liste. Fassung 1 hält nur api-1.
- Also: antwortet **api-2**, ist `node === null` und es fliegt
  `Error("session: malformed certificate")` -- derselbe eine Satz, und das
  **nachdem** `login/finish` mit 200 durchgelaufen ist. Antwortet api-1, geht
  es durch.

api-1 hat seinen Signierschlüssel nie gewechselt. Gegen Fassung 1 geht
`verifyAPI` für api-1 also durch -- womit der ursprüngliche E4-Zweifel bestätigt
ist: die alte Fassung allein erklärt nichts. **Was erklärt, ist die
Verteilung auf zwei APIs.**

### Vorhersage, aufgeschrieben VOR Thomas' Messung

Trifft dieser Mechanismus zu, dann:

- **V1: Der Fehlschlag WECHSELT ab**, etwa jeder zweite Versuch, und ist nicht
  streng der erste. „Erster Versuch tot, zweiter geht" ist genau, wie
  Reihum-Verteilung über zwei Knoten aussieht, wenn man sofort nochmal tippt.
- **V2: Im gescheiterten Durchlauf antwortet `login/finish` mit 200**, und
  `trust?from=1` auch. Kein HTTP-Status zeigt den Fehler; er steht nur in der
  Konsole.
- **V3: Die Konsolenzeile lautet `session: malformed certificate`.** Diese eine
  Zeichenfolge trennt die Erklärungen voneinander:
  - `session: malformed certificate` -> der antwortende Knoten steht nicht in
    Fassung 1 (dieser Mechanismus), oder der Knotenname passt nicht;
  - `session: bad signature` -> Schlüsselkennung gefunden, Signatur falsch;
  - `login: no trust list` -> leere Kette oder 204;
  - `proof: Argon2id did not answer` -> die Frist, die E3 ausschließt.

Falsifiziert ist der Mechanismus, wenn der Fehlschlag streng nur beim ersten
Versuch auftritt und danach nie wieder (V1), wenn `login/finish` im
gescheiterten Durchlauf KEINE 200 hat (V2), oder wenn die Konsole etwas anderes
sagt als `session: malformed certificate` (V3).

### Offene Frage, die alles entscheidet

Welche Anlage benutzt Thomas, wenn er sich anmeldet? Auf der Sechs-VM-Anlage
mit zwei APIs hat dieser Mechanismus eine echte Chance; auf ngc-ein mit einer
einzigen API kann dort nichts wechseln -- dort hat die Leitsitzung drei Läufe
gegen `https://ngc.test:7443/api/v1/setup` gemessen, alle 200, 6 ms, kein
langsamer erster Lauf. Die Frage liegt bei Thomas.

## V1 ist gefallen (30.10.2026)

Thomas' `hosts`-Datei hat als einzige NGC-Zeile `10.2.3.216 ngc.test
store.ngc.test`. Sein Browser landet auf **ngc-ein**, der Einzelmaschine: ein
API-Knoten (`node: ein`, ein Dienst, ein Balancer-Mitglied). **Es gibt dort kein
api-2, auf das verteilt werden könnte.**

- **V1 widerlegt**, und damit der api-2-Mechanismus für Thomas' Erscheinung.
  Grund: diese `hosts`-Zeile.
- An der **Sechs-VM-Anlage** bleibt der Mechanismus richtig und bleibt ein
  echter Mangel. Er erklärt nur nicht, was Thomas erlebt.
- Einschränkung: das ist seine Namenszuordnung, nicht sein Klick. Tippt er eine
  IP direkt, kann er woanders landen. Die Frage bleibt offen, aber die
  Beweislast hat sich gedreht.
- V2 und V3 bleiben als Trennschnitt gültig.

Damit war die Vorsorge das Wertvolle, nicht die Vermutung: weil die drei
Vorhersagen vor der Messung eingecheckt waren, ist jetzt zu sehen, welche
gefallen ist, statt dass sie nachträglich umgedeutet wird.

## Was auf einer Einzelmaschine überhaupt noch asymmetrisch sein kann

Eingegrenzt über den Satz, den Thomas sieht. „Anmeldung fehlgeschlagen." ist
NICHT jeder Fehler: ein `E_UNREACHABLE` zeichnet „Dienst nicht erreichbar"
(`auth/login.js`, `unreachableText`), ein `E_LOCKED` den Zähler. Damit fällt
eine ganze Klasse weg:

- **Ein kalter Zwischenspeicher der Vertrauensliste im Dienst ist
  ausgeschlossen.** `finish.go:104-115`: scheitert `h.S.Trust(ctx)`, antwortet
  der Pfad `E_UNREACHABLE`, und die Maske sagt „Dienst nicht erreichbar" --
  nicht den Satz, den Thomas sieht.

Was bleibt, mit je einem Trennschnitt, den der Mitschnitt zieht:

1. **Die Nonce** (`finish.go:254`, `OnceUse`; `live.go:454`,
   `OncePurge(now - MaxSkew)`). Auf einer Einzelmaschine fällt der
   Uhrenvergleich zwischen Knoten weg, nicht aber ein **Sprung** der Uhr
   zwischen `login/start` und `login/finish`. Trennschnitt: dann antwortet
   `login/finish` mit **E_LOGIN, nicht mit 200**.
2. **Der WebSocket.** `Link.connect(socketURL(), ...)` läuft in `login()` NACH
   `verifyAPI`, und ein gewöhnlicher Fehler dort ergibt denselben einen Satz.
   Dieser Schritt war bisher in keinem Verdacht. Trennschnitt: `login/start`,
   `login/finish` und `trust?from=1` sind alle **200**, und die `ws`-Zeile ist
   die, die stirbt.
3. **Die Vertrauensliste von ngc-ein.** Ihre Fassung ist nicht lesbar
   (verschlüsselte Datei, Absicht). Ein einziger Knoten kann in Fassung 1 nicht
   fehlen -- sonst käme niemand je hinein --, aber sein Signierschlüssel kann
   **gewechselt** worden sein. Trennschnitt: das scheitert bei JEDEM Versuch,
   nicht bei jedem zweiten, und die Konsole sagt dann
   `session: malformed certificate`.
4. **Etwas im Browser, das der erste Versuch aufwärmt.** E3 schließt die
   Argon2-Frist aus; bleiben der Modulabruf und IndexedDB. Trennschnitt: dann
   stirbt es VOR `login/finish`, und dessen Zeile fehlt im Mitschnitt ganz.

**Die ganze Messung reduziert sich damit auf eine Frage:** welche der vier
Zeilen `login/start`, `login/finish`, `trust?from=1`, `ws` ist die letzte, die
gelungen ist? Die Liste für Thomas gilt für ngc-ein unverändert.

## Der api-2-Verdächtige ist tot (30.10.2026)

Der Strang „Kein halb funktionierender Server" hat an der Sechs-VM-Anlage
gemessen: `kill -9` auf `ngc-api` (api-2, PID 623) um 16:58:48 UTC, zurück
16:58:53 als PID 1374. Die erste Anmeldung danach scheiterte, die vier
folgenden nicht:

    1.      HTTP 500 in 0,0247 s, Rumpf `<html>…500 Proxy Error…`
    2.–5.   HTTP 200 in 0,027–0,029 s

Das Muster „erster Versuch scheitert, dann geht es" ist damit mit einem
fehlenden API-Knoten erzeugbar. Die Frage war, welchen Satz der Browser dabei
zeichnet.

Entscheidende Zeile, selbst gelesen, nicht übernommen:
`apps/web/public/assets/js/net/http.js`, in `call()` nach dem `fetch`:

    if (res.ok) return raw;
    let close;
    try {
      close = wire.parseClose(raw);
    } catch {
      throw new ApiError(wire.CODES.UNREACHABLE);
    }

Eine Apache-Fehlerseite ist kein `CLOSE`-JSON. `parseClose` wirft, und der
Fang daneben macht daraus `E_UNREACHABLE`. Der Kommentar im Code sagt es
ausdrücklich: „Answers Apache gives itself - 502 and 503 when no API replies
or its certificate check fails - carry no CLOSE JSON at all." Die 500 aus der
Messung fällt in dieselbe Klasse.

`E_UNREACHABLE` zeichnet die Maske als „Dienst nicht erreichbar", über
`unreachableText` — nicht als „Anmeldung fehlgeschlagen." Thomas sieht den
zweiten Satz. Also:

**Befund: ein fehlender, abgeschossener oder neu gestarteter API-Knoten ist
als Ursache von Thomas' Erscheinung aus.** Nicht weil es bei ihm nur eine API
gibt (das war schon V1), sondern weil dieser Weg einen anderen Satz zeichnet.
Beide Gründe treffen unabhängig voneinander zu.

Der Browser zeigt übrigens auch nicht die Apache-Seite: `call` liest den Rumpf
und wirft, bevor irgendetwas davon angezeigt werden könnte. Die Vermutung der
Leitsitzung, es könne bei fehlgeschlagenem Lesen doch auf den einen Satz
zurückfallen, trifft nicht zu — der Fang nennt `UNREACHABLE` namentlich.

Damit bleiben von den vier Kandidaten drei: die Nonce, der WebSocket, und
etwas, das der Browser aufwärmt. Der gewechselte Signierschlüssel bleibt
daneben stehen, scheitert aber jedes Mal und nicht nur beim ersten Versuch.

Entscheidend bleibt dieselbe Frage: welche der vier Zeilen `login/start`,
`login/finish`, `trust?from=1`, `ws` ist die letzte, die gelungen ist?

## Zwei Kandidaten sind einer, und meine frühere Entlastung war falsch (30.10.2026)

Vor dem Nachstellen habe ich die Datei gesucht, die den Nonce-Verdacht
widerlegen würde. Sie widerlegt die eine Hälfte und legt die andere frei.

**Was den Nonce NICHT tötet.** `apps/server-api/classes/login/live.go:448` ist
die einzige echte Rufstelle von `OncePurge` — `Purge`, die Aufräumrunde. Sie
gibt `now - session.MaxSkew` weiter und entfernt nur Werte, die schon über
ihre Zeit sind. Ein Nonce, der vor Sekunden mit `now+120` angelegt wurde, ist
für sie unerreichbar. Der Kommentar darüber sagt zusätzlich: „Nothing in this
package wires it up, and nothing hangs on the cadence. Nothing hangs on it
running at all." Die Aufräumrunde als Ursache ist damit aus — und mit ihr der
Auftrag „vergleiche die Uhren der sechs Maschinen", den die Vorarbeit
aufgeschrieben hatte.

**Was dabei herauskommt.** Die Reihenfolge in `auth/login.js`:

    113:  const challenge = await http.loginStart(name);   // Nonce entsteht, +120 s
    165:  proofs.proof = await proveWithPassword(...);     // Argon2id läuft
    175:  const result = await http.loginFinish(...);      // Nonce wird verbraucht

Die ganze Argon2-Herleitung liegt **innerhalb** der 120 Sekunden des Nonce.
Und in `contracts/proof.js:288-302` wird der Wecker (`setTimeout`) erst nach
`new Worker(...)` gestellt, der Modul-Abruf und das WASM-Übersetzen zählen
also mit hinein: `min(10000 + m·t·0,5, 120000)` = **108 s** bei m=65536, t=3.

Daraus folgt eine Gabel, und beide Zinken zeichnen denselben Satz:

- Herleitung über 108 s → `BadProof("proof: Argon2id did not answer")`,
  gefangen in `attempt()`, gezeichnet als „Anmeldung fehlgeschlagen."
- Herleitung knapp unter 108 s, aber Start + Herleitung + Unterschrift +
  `loginFinish` über 120 s → `OnceUse` gibt ErrNotFound, `deny()`,
  gezeichnet als „Anmeldung fehlgeschlagen."

**Korrektur meiner eigenen Aussage.** Oben steht bei E3, der Argon2-Wecker sei
widerlegt, weil das Budget 108 s beträgt. Dieser Schluss hat eine
unausgesprochene Annahme: dass ein kalter erster Lauf deutlich unter 108 s
bleibt. Diese Zahl habe ich nie gemessen. Ich habe ein Budget gegen eine
Vermutung gehalten und das Ergebnis Entlastung genannt. E3 gilt nicht mehr.

**Damit sind Kandidat 1 (Nonce) und Kandidat 4 (etwas, das der Browser
aufwärmt) derselbe Kandidat**, mit einer einzigen messbaren Größe:

> Wie lange braucht die erste Argon2id-Herleitung in einem kalten Browser
> (64 MiB, 3 Durchgänge, 1 Spur), und wie lange die zweite?

Erwartung, vor der Messung aufgeschrieben:

- **E5** Der zweite Lauf ist schneller als der erste. Fällt er nicht um
  mindestens ein Fünftel, trägt „aufgewärmt" nichts, und der Kandidat fällt.
- **E6** Bleibt der erste Lauf unter 60 s, erklärt er den Satz nicht: dann
  liegt der ganze Ablauf mit Abstand unter beiden Grenzen, und ich brauche
  eine andere Ursache.
- **E7** Liegt der erste Lauf über 108 s, scheitert er über den Wecker; liegt
  er zwischen 100 und 108 s, kann er über den Nonce scheitern. Welcher der
  beiden es ist, entscheidet nicht der Satz, sondern die Zeile der Konsole
  und ob `login/finish` eine Antwort bekam.

Diese Messung braucht Thomas nicht und die laufende Anlage nicht — nur einen
Browser und die Datei aus diesem Baum.

## E5 ist gefallen: es gibt kein Aufwärmen (30.10.2026, gemessen)

Zuerst der Fund, der die Messung billig machte: **es gibt kein WASM.** Argon2id
läuft als reines JavaScript aus `apps/web/public/assets/vendor/noble-hashes/argon2.js`.
Damit entfällt der Modul-Abruf und das WASM-Übersetzen, die ich als Kosten vor
dem Wecker vermutet hatte, und die Messung braucht keinen Browser: node
benutzt dieselbe Maschine (V8) wie Chrome.

Gemessen, fünf Läufe in einem Prozess, dieselben Parameter wie an der Anlage
(m=65536, t=3, p=1, dkLen=32):

    Lauf 1: 0,94 s
    Lauf 2: 0,90 s
    Lauf 3: 1,09 s
    Lauf 4: 1,26 s
    Lauf 5: 1,19 s

**E5 ist widerlegt, und zwar in die andere Richtung.** Der erste Lauf ist nicht
langsamer als die folgenden, er ist der schnellste von allen. Eine
Aufwärmwirkung gibt es nicht; die Streuung geht nach oben, nicht nach unten.
Der Kommentar in `auth/argon2-worker.js` („about a second at m=65536") trifft.

**Warum das den Kandidaten tötet, unabhängig von der absoluten Geschwindigkeit.**
Seine ganze Kraft war die Asymmetrie zwischen kalt und warm — nur daraus kann
„der erste Versuch scheitert, der zweite nicht" entstehen. Gemessen gibt es
diese Asymmetrie nicht. Ein langsames Gerät wäre also bei **jedem** Versuch
langsam, und das ist nicht Thomas' Erscheinung. Der Kandidat erklärt sie nicht
mehr, egal wie langsam seine Maschine ist.

Die Zahl dazu, nach der Vorgabe „ab welcher Bremse reißt es": von 0,94 s auf
108 s sind es **115-fach**. Die Prozessorbremse der Browser-Werkzeuge reicht
bis 20-fach. Es gibt keine Einstellung und kein Gerät dieser Klasse, das diese
Strecke zurücklegt.

E6 bestätigt sich damit auch: der erste Lauf liegt unter 60 s, also erklärt er
den Satz nicht.

**Grenzen dieser Messung, ausdrücklich.** Gemessen in node auf V8, nicht in
einem Browser-Worker, und auf einer Maschine. Was ich dadurch nicht gemessen
habe, ist ein fester Zuschlag für das Starten des Workers im Browser. Dass der
100 Sekunden beträgt, ist nicht glaubhaft — aber gemessen habe ich ihn nicht,
und für die Lücke unten gilt das auch nicht als Entlastung.

**Was als Lücke stehen bleibt, unabhängig von diesen Zahlen.** Der Wecker wird
in `proof.js` nach `new Worker(...)` gestellt und zählt damit das Laden des
Workers mit. Er ist als Frist auf die Herleitung gedacht, misst aber Laden plus
Herleitung. Das ist ein Mangel für sich, auch wenn er heute nicht reißt, und
die Behebung ist nicht „Budget hochsetzen", sondern den Wecker hinter das Laden
stellen. Kein Auftrag, nur notiert.

**Stand der Kandidaten.** Nonce und „aufgewärmt" sind zusammen gefallen. Übrig
bleibt der WebSocket — der Kandidat, den ich ohne Thomas' Zeile am
schlechtesten prüfen kann, und jetzt der einzige, der die Erscheinung noch
trägt. Der gewechselte Signierschlüssel steht daneben, scheitert aber jedes
Mal.

## V4: der WebSocket über den Umkehr-Stellvertreter (30.10.2026, vor der Messung)

`apps/web/public/assets/js/net/link.js:67-82`, der Anfang von `connect`:

    const ws = new WebSocket(url, session.SUBPROTOCOL);
    await new Promise((ok, no) => {
      ws.onopen = ok;
      ws.onerror = () => no(new Error("link: no connection"));
      ws.onclose = () => no(new Error("link: no connection"));
    });

Das ist ein gewöhnlicher `Error`, kein `ApiError` mit `code`. In `attempt()`
fällt er damit auf `FAILED()` — **genau den Satz, den Thomas sieht.** Kein
`E_UNREACHABLE`, weil hier kein `wire.CODES` im Spiel ist.

Der Mechanismus, der daraus eine Asymmetrie zwischen erstem und zweitem
Versuch macht, liegt nicht in NGC, sondern im Umkehr-Stellvertreter: ein
`Upgrade` ist für Apache eine andere Sache als ein gewöhnlicher Aufruf. Hat er
für den Dienst keine warme Verbindung, beantwortet er den ersten `Upgrade`
selbst — und der Browser sieht davon keinen Rumpf, nur ein `onerror`. Beim
zweiten Mal steht die Verbindung.

Das deckt sich mit der Messung des anderen Strangs: dort war die erste Antwort
nach dem Neustart eine 500 des Stellvertreters. Nur traf sie dort einen
gewöhnlichen Aufruf, wo `http.js` sie zu `E_UNREACHABLE` macht und die Maske
„Dienst nicht erreichbar" zeichnet. Trifft dasselbe den `Upgrade`, gibt es
keinen Rumpf zum Lesen, keinen Code zum Unterscheiden — und den anderen Satz.

Warum das zu Thomas passt, ohne Hilfskonstruktion:

- Es braucht keine zweite API und keine Asymmetrie in der Anlage. Eine
  Einzelmaschine genügt.
- Es trifft nur den ersten Versuch und nur nach einer Pause, weil es an einer
  Verbindung des Stellvertreters hängt, nicht an NGC.
- Die drei HTTP-Schritte gelingen dabei alle, weil sie gewöhnliche Aufrufe
  sind. Nur der `Upgrade` stirbt.

**V4, vor der Messung:** Thomas' Mitschnitt zeigt `login/start` 200,
`login/finish` 200, `trust?from=1` 200 oder 204 — und die `ws`-Zeile scheitert.
In der Konsole steht `link: no connection`, nicht `session: malformed
certificate` und nicht `login: no trust list`.

Fällt V4, bleibt von meinen vier Kandidaten keiner, und ich fange die Suche
ohne Verdacht neu an — bei der Zeile, die der Mitschnitt als letzte gelungene
zeigt.

Nebenbefund, nicht die Erscheinung: das `await new Promise` oben hat **keine
Frist**. Öffnet sich die Verbindung nie und kommt auch kein Fehler, hängt die
Anmeldung für immer. Das ist kein Kandidat für Thomas' Satz — ein Hänger
zeichnet gar keinen Satz —, aber es ist ein Mangel. Notiert, nicht gebaut.

## Befund 3 ist entschieden, und zwar am Code: wir messen am falschen Rechner (30.10.2026)

Die Leitsitzung hat an ngc-ein (.216) gelesen: 302 Warnungen in sieben Tagen,
`state: no trust list yet`, und **keine einzige** `/api/`-Zeile im
Zugriffsprotokoll von Apache außer einem `curl` vom 24.10. Sie bat mich, zuerst
zu prüfen, ob wir überhaupt am richtigen Rechner messen. Das lässt sich ohne
die Maschine entscheiden.

`apps/server-api/classes/server/server.go:520-524` sagt, was diese Warnung
bedeutet — gemessen von jemand anderem, am 18.10. an der kleinen Anlage:

> „the expected state of an installation that is set up and not yet paired"

ngc-ein ist eingerichtet und **nie gepaart**. Es hat keine Vertrauensliste, und
`State.Refresh` ist die Vertrauensliste der API selbst, nicht die eines anderen
Dienstes.

Was daraus für eine Anmeldung folgt, steht in
`apps/server-api/classes/login/finish.go:101-113`:

    // Before the setup has run there is no list and no login. E_UNREACHABLE
    v, err := h.S.Trust(ctx)
    if err != nil {
        httpapi.FailCoded(w, wire.CodeUnreachable, err)
        return
    }

**Auf ngc-ein kann sich überhaupt niemand anmelden** — nicht beim ersten
Versuch, nicht beim zweiten, nie. Und der Satz, den die Maske dabei zeichnet,
ist „Dienst nicht erreichbar", nicht „Anmeldung fehlgeschlagen."

Thomas sieht den anderen Satz, und er kommt beim zweiten Versuch durch. Beides
ist auf dieser Maschine unmöglich.

**Befund: Thomas meldet sich nicht an ngc-ein an.** Das leere Zugriffsprotokoll
ist kein Rätsel, sondern die Bestätigung: dort hat sich nie jemand angemeldet,
weil es dort nicht geht. Die Deutung der Leitsitzung zu Befund 1 fällt damit
mit — die fehlende Liste ist ein echter Mangel dieser Maschine, aber nicht
Thomas' Erscheinung. Ihre eigene Frage („schreibt die Liste sich beim ersten
Gebrauch nach?") braucht keine Antwort mehr: welcher Weg sie auch nachschriebe,
der Satz wäre der falsche.

**Und das dreht V1 um.** V1 hatte den api-2-Mechanismus getötet, allein auf die
`hosts`-Zeile `10.2.3.216 ngc.test store.ngc.test` gestützt. Ich hatte damals
selbst dazugeschrieben, das sei seine Namenszuordnung und nicht sein Klick.
Genau diese Lücke trägt jetzt: die Namenszuordnung zeigt auf eine Maschine, an
der die Erscheinung nicht entstehen kann. **V1 ist hinfällig, und der
api-2-Mechanismus lebt wieder.**

Er ist der einzige Kandidat, der alles trägt, was heute gemessen wurde:

- Alle drei HTTP-Schritte antworten 200 — die APIs laufen, die Liste steht.
- `verifyAPI` scheitert, weil die Maske durch den Stellvertreter
  `openTrustFromChain` auf Fassung 1 festgenagelt ist und api-2 erst ab
  Fassung 2 in der Liste steht.
- Der Fehler ist `session: malformed certificate`, ein gewöhnlicher `Error`
  ohne Feld `code` → `FAILED()` → **Thomas' Satz.**
- Die Asymmetrie zwischen Versuchen kommt vom Lastverteiler: der eine Knoten
  gelingt, der andere nicht. „Erster Versuch scheitert, zweiter geht" ist
  genau das, was ein Wechsel zwischen zwei Knoten erzeugt.

Was dagegen noch offen ist: nicht widerlegt, aber auch nicht gemessen ist, ob
Fassung 1 der Sechs-VM-Liste denselben Signierschlüssel von api-1 trägt wie
Fassung 8. Die Kurzmarken sprechen dafür (api-1 hat nie gewechselt).

**Die eine Frage an Thomas ist jetzt eine andere, und sie ist billiger:** nicht
mehr „welche Zeile gelingt zuletzt", sondern **welche Adresse steht in der
Adresszeile, wenn er sich anmeldet.** Seine `hosts`-Datei hat uns heute
stundenlang am falschen Rechner messen lassen. Der Mitschnitt bleibt wertvoll,
aber diese eine Zeile ordnet alles davor neu ein.

## Die 42 Zeilen: was sie beweisen und was nicht (30.10.2026)

Die Leitsitzung hat im Zugriffsprotokoll von api-1 42 `login/finish` eines
echten Browsers gefunden, 23.10. bis 30.10., fast alle als **Paare** im Abstand
von 3 bis 12 Sekunden, und **beide mit 200**. Dazu eine einzige 401.

**Was bewiesen ist, und es ist viel.** `shared/session/session.go:739-744`
bildet `wire.CodeLogin` auf **401** ab, und `deny()` in `finish.go` ruft
`httpapi.Fail(w, wire.CodeLogin)`. Ein echtes Abweisen durch die API — falsches
Passwort, unbekannter Name, verbrauchter Nonce, schlechter Beweis — ist also
eine 401 und niemals eine 200.

Beide Hälften jedes Paares sind 200. **Damit ist gemessen, dass die API den
gescheiterten Versuch nicht abgewiesen hat.** Er scheitert nach der 200, im
Browser. Der Nonce ist damit auch von dieser Seite tot, und der Auftrag „lies
das API-Protokoll" ist endgültig erledigt: das Protokoll konnte den Grund nie
nennen, weil es keinen gab.

**Die 401 ist eingeordnet, nicht weggelassen.** Sie steht am 24.10. 10:55:55
mitten in einem Dreier 200 / 401 / 200. Gelesen als: erster Versuch scheitert
im Browser (200), dann vertippt sich Thomas (401), dann geht es (200). Es ist
die einzige echte Abweisung in einer Woche. Sie ist kein zweiter Fehler mit
demselben Satz, sondern der Beleg dafür, dass ein echter Fehlschlag anders
aussieht — und dass alle anderen 41 Zeilen keiner waren.

**Was NICHT bewiesen ist, und es spricht gegen meinen eigenen Kandidaten.**

Erstens: ein Zugriffsprotokoll auf api-1 sagt nicht, **welcher Knoten
geantwortet hat.** Liegt dort der Umkehr-Stellvertreter für beide, schreibt es
seine Sicht, nicht die des Hintermanns. Mein Mechanismus verlangt, dass den
gescheiterten Versuch **api-2** beantwortet hat. Diese Zeile steht nicht in der
Messung. Offen: trägt die `LogFormat` den Namen des Lastverteiler-Arbeiters,
und hat api-2 ein eigenes Protokoll mit denselben Sekunden?

Zweitens, und schwerer: **die Paare sind zu regelmäßig.** Ein Lastverteiler
über zwei Knoten ist ein Münzwurf oder ein Wechsel; über sechs Tage und
zwanzig Anmeldungen müsste er gemischt scheitern, nicht fast immer beim ersten
und fast nie beim zweiten. Mein api-2-Mechanismus erklärt den Satz, aber die
**Gleichmäßigkeit** erklärt er nicht. Etwas Bestimmtes scheitert beim ersten
Versuch, nicht etwas Zufälliges.

Gegen die naheliegende Rettung („der Browser merkt sich etwas") spricht der
Code: `shell/trustlist.js` hat zwar einen Speicher (`indexedDB`, Zeile 97),
aber `main.js:268` übergibt kein `openTrust`, also läuft der Stellvertreter und
`trustlist.js` wird beim Anmelden **nicht angefasst**. Im scheiternden Weg gibt
es nichts, was sich merkt.

**Stand, ehrlich:** gemessen ist der Ort des Fehlers (im Browser, nach der 200)
und dass es kein Abweisen der API ist. Nicht gemessen ist, welcher Knoten
antwortete, und ungeklärt bleibt die Gleichmäßigkeit. Mein Kandidat ist der
beste, den ich habe, und er trägt die eine Hälfte nicht. Ich nenne ihn deshalb
nicht belegt.

Nicht zu verwechseln, auf Bitte der Leitsitzung ausdrücklich getrennt: der
andere Weg über api-2 — ein **fehlender** Knoten — erzeugt eine nackte
Apache-500 und über `http.js` den Satz „Dienst nicht erreichbar". Das ist ein
anderer Weg mit einem anderen Satz als der hier gemeinte.

## Die Rechnung rettet api-2 nicht, sie erledigt ihn (30.10.2026)

Die Leitsitzung hat die Einstellung gelesen: `lbmethod=byrequests` über zwei
`BalancerMember`, also ein **Zähler** und kein Münzwurf. Ihr Vorschlag: zähl
die Anfragen je Anmeldung; ist die Zahl gerade, landet jede Anmeldung wieder
auf demselben Knoten, und „immer der erste Versuch" wird erwartbar.

Gezählt, aus `net/http.js` und `auth/login.js`:

    gescheiterter Versuch:   login/start, login/finish, trust?from=1   = 3
    gelungener Versuch:      dieselben drei + der ws-Aufstieg          = 4

Beide Zahlen helfen nicht, und zwar aus einem Grund, der von der Parität
unabhängig ist: **der Zähler ist global.** Er läuft über alles, was durch
diesen Lastverteiler geht — Seitenaufrufe, `getSetupOffer`, Abmeldungen, jede
Neuladung, jeder andere Nutzer, jeder andere Strang, der heute gegen die Anlage
gemessen hat. Über sechs Tage und zwanzig Anmeldungen wird jede Parität
verrührt. Eine Erscheinung, die an **jedem** der sechs Tage beim ersten Versuch
auftritt, kann nicht an der Restklasse eines Zählers hängen, den jede
dazwischenliegende Anfrage um eins verschiebt.

Damit ist die Rechnung durch, und sie geht gegen mich: **meine eigene Lücke 2
bleibt offen, und die Arithmetik schließt sie nicht.** Der api-2-Mechanismus
erklärt den Satz und den Ort, aber nicht die Gleichmäßigkeit — und er hat jetzt
keinen Weg mehr, sie zu erklären.

## V4 geht wieder voran, und die Leitsitzung hatte den besseren Grund

Dieselbe Datei trägt `keepalive=On` mit `timeout=65`: der Stellvertreter hält
eine ruhende Verbindung zum Knoten 65 Sekunden. Thomas' Paare liegen **3 bis 12
Sekunden** auseinander, also innerhalb dieses Fensters. Der erste Versuch läuft
über eine kalte Verbindung, der zweite über die warme.

Das ist genau die Bestimmtheit, die ich gesucht und bei api-2 nicht gefunden
habe: nicht Zufall und keine Restklasse, sondern „das erste Mal nach einer
Pause". Es erklärt ohne eine einzige Annahme über die Vertrauensliste, warum es
fast immer der erste Versuch ist und fast nie der zweite.

Ich hatte V4 liegen gelassen mit der Begründung, api-2 erkläre es „ohne
Hilfskonstruktion". Nach meiner eigenen Lücke 2 ist es umgekehrt. **V4 ist
wieder der Hauptverdächtige**, und die Umkehrung kam nicht von meiner Messung,
sondern von einem Einwand, dem ich zustimme.

## Der Trennschnitt, der nichts kostet

Beide Kandidaten lassen die drei HTTP-Schritte mit 200 gelingen. Sie
unterscheiden sich in **einer Zeile, die im Protokoll schon steht oder fehlt**:

- **api-2:** der Versuch stirbt in `verifyAPI`, also **vor** `Link.connect`.
  Zum gescheiterten Versuch gehört **kein** ws-Aufstieg. Drei Zeilen, dann
  nichts.
- **V4:** der Versuch stirbt **im** Aufstieg. Zum gescheiterten Versuch gehört
  eine ws-Zeile, und sie trägt nicht die 101 eines gelungenen Aufstiegs.

Die Frage an das Protokoll, das die Leitsitzung ohnehin offen hat: **steht
neben dem ersten `login/finish` eines Paares eine Zeile zum ws-Pfad, und mit
welchem Status?** Steht sie da, ist es V4. Fehlt sie und steht nur beim zweiten
eine, ist es api-2. Kein Eingriff, kein Thomas, kein Nachstellen.

Dieselbe Unterscheidung noch einmal in der Konsole, falls Thomas' Mitschnitt
doch kommt: `link: no connection` ist V4, `session: malformed certificate` ist
api-2.

Zu Lücke 1: sie ist aus dem Protokoll endgültig nicht schließbar — die
`LogFormat` sind die Debian-Vorgaben und api-2 hat gar keinen Apache. Ich lasse
sie und schließe sie beim Nachstellen gegen jeden Knoten einzeln, falls es
dazu kommt. Fällt V4 am ws-Trennschnitt nicht, brauche ich das Nachstellen
nicht.

## Lücke 2 ist zu, und die Gegenprobe lebt (30.10.2026)

Die Leitsitzung hat die Vertrauensliste geholt (`ngc_blob` kind `trust` auf
db-1, acht Fassungen, Signierschlüssel nur als Kurzmarke):

    Fassung 1   1 Knoten    api api-1  adb184f0
    Fassung 2   2 Knoten    + api api-2  935894a4
    Fassung 3-6             + vier controller
    Fassung 7-8 6 Knoten    (neueste: 8)

**api-2 fehlt in Fassung 1.** Es kommt erst mit Fassung 2. Und api-1 trägt in
allen acht Fassungen dieselbe Marke — ein gewechselter Schlüssel ist damit
ausgeschlossen, mein dritter Rest-Verdacht fällt.

### Die Rechnung, am Code nachgezählt

Die Leitsitzung hat die Parität aus ihrem Protokoll gerechnet und mich gebeten,
am Code nachzuzählen, weil eine einzige unbemerkte `/api/`-Anfrage sie bricht.
Nachgezählt:

- Ein gewöhnlicher Seitenaufruf macht **keine** `/api/`-Anfrage. `getSetupOffer`
  (`PATH_SETUP`) steht nur in `auth/setup.js:119`, also im Erstlauf, nicht im
  Laden der Maske. Die rund 45 Dateien gehen an den Webteil und drehen den
  Zähler nicht.
- Es gibt **kein Abmelden für den Nutzer.** `http.logout()` wird an genau einer
  Stelle gerufen, `shell/trustlist.js:523`, auf einem Alarmweg. `PATH_LOGOUT`
  kommt sonst nirgends vor.

Damit sind die einzigen `/api/`-Anfrägen im gewöhnlichen Betrieb die der
Anmeldung, und die Parität der Leitsitzung hält:

    Fehlschlag  start, finish, trust                = 3
    Erfolg      start, finish, trust, trust, ws     = 5
    ein Paar                                        = 8, gerade

Bei api-1 angesetzt:

    1. Versuch  start→api-1  finish→api-2 FEHLSCHLAG  trust→api-1
    2. Versuch  start→api-2  finish→api-1 ERFOLG      trust→api-2  trust→api-1  ws→api-2

Nach dem Paar steht der Zähler wieder bei api-1. Die Reihe trägt sich selbst,
und „immer der erste, nie der zweite" ist kein Zufall, sondern ein Zähler mit
gerader Schrittzahl.

### Die Gegenprobe, die lebt — und sie ist stärker als die Rechnung

Eine Rechnung, die nie bricht, erklärt zu viel: bei vollkommen festem Zähler
wäre **jede** Anmeldung ein Paar, für immer. In den Zeilen der Leitsitzung
stehen aber **drei Einzelgänger** — Anmeldungen ohne Paar, bei denen der erste
Versuch gelang: 14:28:33, 16:23:57, 16:27:30.

Die Rechnung sagt, wann das passiert: wenn das Paar bei **api-2** ansetzt, geht
`finish` an api-1 und der erste Versuch gelingt. Dazu muss irgendetwas den
Zähler um eine ungerade Zahl verschoben haben — fremder `/api/`-Verkehr oder
ein Neustart von Apache.

**Alle drei Einzelgänger liegen auf dem 30.10.** Und der 30.10. ist genau der
Tag, an dem drei Stränge gleichzeitig an der Anlage gemessen haben, mit `curl`
gegen die APIs und mit `kill -9` auf Dienste. Vom 23. bis 29.10., ohne fremden
Verkehr, ist **jede** Anmeldung ein Paar.

Das ist eine Vorhersage, die ich nicht gestellt habe, um sie zu bestätigen: die
Einzelgänger sind kein Rauschen, sondern die andere Parität, und sie treten nur
an dem einen Tag auf, an dem der Mechanismus sie vorhersagt.

Auch der Dreier vom 24.10. rechnet sich glatt: 3 (Fehlschlag) + 2 (die 401
bricht nach `finish` ab) + 5 (Erfolg) = 10, gerade. Die Reihe bleibt also
ausgerichtet — und tatsächlich ist die nächste Anmeldung um 11:27 wieder ein
Paar.

### Befund

**Satz, Ort und Gleichmäßigkeit sind jetzt alle drei erklärt, und von derselben
Ursache:** die Maske baut die Vertrauensliste über den Stellvertreter
`openTrustFromChain` aus **Fassung 1**, in der nur api-1 steht. Antwortet
api-2 auf `login/finish`, findet `verifyAPI` für dessen Kennung keinen
Aussteller und wirft `session: malformed certificate` — ein gewöhnlicher
`Error` ohne Feld `code`, also „Anmeldung fehlgeschlagen." Der Lastverteiler
mit `lbmethod=byrequests` und gerader Schrittzahl je Paar sorgt dafür, dass es
immer der erste Versuch ist.

Was weiterhin **nicht** gemessen ist: dass api-2 den gescheiterten Versuch
beantwortet hat. Es gibt dafür kein Protokoll (api-2 hat keinen Apache, die
`LogFormat` trägt den Arbeiter nicht). Die Rechnung und die Einzelgänger
sprechen dafür; ein Nachstellen gegen jeden Knoten einzeln würde es messen.
Ich nenne die Ursache daher **belegt bis auf diese eine Zeile** — und nenne die
Zeile.

### Zeilen fürs Blatt, nicht verfolgt

- Kein wirksamer Zwischenspeicher für die Rechendateien: der Browser holt
  `argon2.js` 55-mal und `_md.js` 83-mal.
- `web-api-1` steht in der Vertrauensliste als `controller`, nicht als `web`.
- Der Argon2-Wecker steht nach `new Worker(...)` und zählt das Laden mit.
- Das `await new Promise` in `Link.connect` hat keine Frist.

## Der Platzhalter-Anker: der zweite Befund dieses Auftrags (30.10.2026)

`apps/web/tests/trust-anchor.json` trägt `{ "seq": 1, "hash": "q6urq6ur..." }` —
lauter 0xAB. Kein Hash einer Fassung, ein Füllmuster.

Das ist mehr als ein Hindernis im Prüfsatz. Es heißt, dass **die Ankerprüfung
der ganzen Weboberfläche nie geprüft worden ist.** `standInOn` ist nicht die
Ursache, sondern die Narbe darüber: eine Prüfung, die den Anker umgeht, kann
nicht sagen, ob die Prüfung gegen den Anker überhaupt greift. Ein Riegel ohne
eine einzige Prüfung ist ein unbekannter Riegel.

Auflagen für die Behebung, von der Leitsitzung und angenommen: der Anker wird
von einem Werkzeug **erzeugt**, nicht hineingeschrieben, und der Fall, der ihn
gegen Abdriften sichert, erzeugt ihn **neu und vergleicht** — nicht eine
Konstante gegen eine Konstante. Sonst steht der Platzhalter nur unter einer
anderen festen Zahl, und der nächste Umbau der Nachbild-Welt lässt sie stehen.

## Warum Weg B der richtige war, mit der besseren Begründung

Gesucht wurden alle Aufrufstellen. Es sind vier, und zwei kannte vorher
niemand:

    shell/main.js:284    die bekannte, hatte keines
    auth/setup.js:539    login() im Erstlauf, ohne openTrust
    auth/setup.js:544    loginView() als Rückfall, ohne openTrust
    auth/login.js:393    die Durchleitung aus loginView

Stellen 2 und 3 sind die beste Begründung für Weg B, die bisher auf dem Tisch
lag, und sie ist stärker als „eine Pflichtangabe bricht an einer vergessenen
Stelle": **der Erstlauf meldet den ersten Verwalter selbst an.** Bei Weg A wäre
genau er am Stellvertreter geblieben. Und bei einer frischen Anlage mit einem
Knoten fällt das **nicht auf**, weil Fassung 1 dort die neueste ist. Der Fehler
wäre unsichtbar geblieben, bis der zweite Rechner dazukommt — also genau bis zu
dem Tag, an dem er am schwersten zu finden ist.

## Eine stille Globale, von mir gebaut und von mir gefunden

Im ersten Wurf von `openerFor` war `lastSeq` nicht deklariert, also eine stille
Globale. Das hätte die Grenze „genau einmal nachholen" über **mehrere**
Anmeldeversuche hinweg aufgehoben — und damit genau die Auflage ausgehöhlt, der
sie dienen sollte. Jetzt eine Laufvariable des Öffners, je Maske.

Es gehört ins Blatt und nicht nur in die Commit-Meldung, weil es die Art Fehler
ist, die eine Behebung unbemerkt in ihr Gegenteil dreht.

## Kann sich eine frische Anlage noch anmelden? (am Code beantwortet)

Die Frage der Leitsitzung: der Erstlauf ruft die Anmeldung mit einem Anker, den
er gerade geschrieben hat, und die Kette hat dort genau eine Fassung. Gibt es
einen Lauf, in dem der Öffner startet, bevor Fassung 1 lesbar ist? Das wäre
schlimmer als Thomas' Fehler.

**Nein, und es folgt aus der Reihenfolge.** Zwei Zeilen tragen es:

- `auth/login.js` ruft `openTrust()` **nach** `http.loginFinish(...)`.
- `login/finish.go:104` ruft `h.S.Trust(ctx)` und antwortet bei einem Fehler
  `E_UNREACHABLE`. Ohne lesbare Vertrauensliste gibt es also **keine 200 und
  keinen Keks**.

Der Öffner läuft damit nur in einem Lauf, in dem die Liste schon lesbar war.
Dazu kommt, dass der Kettenpfad selbst (`login/trust.go:34`) über `h.Live(...)`
geht und die Liste **für diese Anfrage** aktuell holt (`live.Trust`), also kein
alter Zwischenspeicher antwortet. Und `from=1` auf einer Liste bei Fassung 1
gibt Fassung 1 zurück, nicht 204 — die 204 gilt nur für die Nummer nach der
neuesten.

Ein Lauf, in dem der Öffner vor Fassung 1 startet, kann es also nicht geben.
Der Fall gehört trotzdem in den Prüfsatz: er ist jetzt durch eine Reihenfolge
gesichert, und Reihenfolgen verschiebt der nächste Umbau.

## Nebenfund: die Liste ist erst nach der Anmeldung zu holen

`apps/server-api/classes/login/trust.go:34` holt über `h.Live(ctx, r)` zuerst
die Sitzung; ohne sie bleibt es bei `E_LOGIN`. Der Kettenpfad **verlangt also
eine Sitzung**, und damit ist die Vertrauensliste ohne Anmeldung nicht zu
holen. Der Browser prüft die Gegenstelle erst, nachdem er seinen Beweis
geschickt hat.

**Richtigstellung zu der Formulierung, in der das zuerst festgehalten wurde:**
der Browser zeigt der Gegenstelle **nicht sein Passwort.** Das Passwort
verlässt den Browser nie — `contracts/proof.js` leitet in einem Worker einen
Schlüssel daraus ab und schickt eine **Unterschrift** über die Bytes der
Client-Bescheinigung, an Nonce und Zweck gebunden. Was eine unverifizierte
Gegenstelle zu sehen bekommt, ist dieser Beweis, nicht das Geheimnis. Der
Unterschied ist nicht kosmetisch: ein abgefangener Beweis gilt für diese eine
Bescheinigung und diesen einen Nonce, ein abgefangenes Passwort für alles.

Was dabei wirklich von einer noch ungeprüften Gegenstelle kommt, ist
`challenge.kdf` — die Argon2-Parameter. Auch das ist eingefasst:
`proof.validateKDF` hält jeden Wert gegen den Vertrag, bevor abgeleitet wird,
also kann eine Gegenstelle die Ableitung nicht beliebig schwach machen.

Ob die Reihenfolge „erst anmelden, dann prüfen, wem" so gewollt ist, ist eine
Frage an den Entwurf der Anmeldung und nicht an diesen Auftrag. Notiert, nicht
verfolgt; die Leitsitzung hängt sie an „Was fehlt bis zur ersten Version".

## Dieselbe Figur, zweimal an einem Tag

Zwei eigene Fehlschlüsse dieses Auftrags sind derselbe:

- **E3:** ein Argon2-Budget von 108 s gegen eine nie gemessene Dauer gehalten
  und das Ergebnis Entlastung genannt.
- **Der leere Prüfbericht:** 20 Minuten ohne Ausgabe mit der eigenen Vorhersage
  erklärt („die Anmelde-Fälle laufen in ihre Zeitgrenzen") statt gemessen. Die
  Stille kam von `| tail`, das die ganze Ausgabe bis zum Ende der Leitung
  zurückhält. Das Gegenargument, das ich mir selbst hätte machen müssen: **eine
  Zeitgrenze, die reißt, schreibt eine Zeile.**

Beide Male: eine Beobachtung mit der Erwartung erklärt, statt sie zu messen.
Das Muster ist teurer als jeder der beiden Einzelfälle, weil es sich wie eine
Messung anfühlt.

Fortschritt wird ab jetzt ungefiltert in eine Datei geschrieben und die Datei
gelesen, während sie wächst — nicht erschlossen.

## Dreimal dieselbe Figur, an einem Tag

Alle drei sind eigene Fehlschlüsse dieses Auftrags, und alle drei sind dieselbe
Bewegung: eine Beobachtung mit der Erwartung erklären, statt sie zu messen.

1. **Budget gegen Vermutung (E3).** Ein Argon2-Budget von 108 s gegen eine nie
   gemessene Dauer gehalten und das Ergebnis Entlastung genannt. Gemessen waren
   es 0,94 s, und der erste Lauf war der schnellste.
2. **Leere Datei gegen Vorhersage.** 20 Minuten ohne Ausgabe als „die
   Anmelde-Fälle laufen in ihre Zeitgrenzen" gedeutet. Die Stille kam von
   `| tail`, das die ganze Ausgabe bis zum Ende zurückhält. Eine Zeitgrenze,
   die reißt, schreibt eine Zeile.
3. **Ausstiegscode gegen Mantel.** `npx playwright test ... ; echo "AUSSTIEG=$?"`
   — das `; echo` macht den Mantel immer erfolgreich. Gemeldet wurde „exit
   code 0" für einen Lauf, der mit `ERR_MODULE_NOT_FOUND` keinen einzigen Fall
   gefahren hat.

Die Verallgemeinerung, die bei 2 und 3 gefehlt hat: **der Ausstiegscode des
Laufs muss die letzte Anweisung sein oder ausdrücklich durchgereicht werden**,
und das gilt für jede Hülle, nicht nur für eine Pipe. Aufgeschrieben war
bisher nur der Pipe-Fall — die halbe Lehre, und die halbe Lehre greift nicht.

Gefunden wurde Nummer 3 nicht durch Klugheit, sondern weil Nummer 2 die Regel
hinterlassen hatte, die Berichtsdatei zu lesen statt der Erfolgsmeldung zu
glauben. So soll eine Regel wirken.

Wer das liest, lernt mehr als aus der Behebung, um die es im PR geht.

## Erwartung an den Webprüfsatz, vor dem Lauf (30.10.2026)

Vergleichspunkt: Lauf vom 02.10.2026, chromium + webkit in einem Zug,
**580 bestanden + 216 übersprungen = 796**, Ausstiegscode 0, 10,1 Minuten.

**Rot ist hier das erwartete Ergebnis und nicht die schlechte Nachricht.** Ein
Prüfsatz, der nach einem Umbau grün bleibt, den er abdecken müsste, ist das
schlechtere Ergebnis — die unveränderten 323 zu 0 von `tools/check.sh` haben
heute genau das gezeigt.

Aber „rot" allein entscheidet nichts. Nach diesem Bau gibt es **drei Sorten
Rot**, und nur eine ist das Ergebnis:

- **`login: no way to open the trust list`** — eine Aufrufstelle, die ich nicht
  verdrahtet habe. Das wäre ein Fehler in meinem Bau, und ich baue nach.
- **`session: malformed certificate` oder `session: bad signature`** — die
  Kette geht gegen den Platzhalter-Anker nicht auf. **Das ist das erwartete
  Rot**, und es ist zugleich der Beweis, dass die Fälle meinen Weg wirklich
  anfassen.
- **eine gerissene Zeitgrenze ohne eine dieser Meldungen** — dann haben die
  Fälle etwas anderes getroffen, und das Rot sagt über meinen Weg nichts. Der
  Lauf wäre dann so wenig eine Messung wie die 323 zu 0.

Weicht die Zahl der **übersprungenen** Fälle von 216 ab, ist das zuerst ein
Verdacht auf die Umgebung und kein Befund: ein ausgefallener Hilfsserver macht
aus einem Überspringen einen laufenden Fall. Die beiden Fälle unterscheiden
sich an den **Namen** der neu laufenden Fälle, nicht an der Summe — also werden
die Namen mitgeschrieben.

Fassung geprüft: `@playwright/test` **1.55.1**, gelesen aus
`node_modules/@playwright/test/package.json` und nicht aus der Sperrdatei. Es
zählt, was daliegt, nicht was eine Datei darüber behauptet — dasselbe
Vorgehen, mit dem heute entschieden wurde, welche Datei an der Anlage wirklich
ausgeliefert wird.
