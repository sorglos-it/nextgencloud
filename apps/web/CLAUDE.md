# Webinterface

Dein Ordner: `apps/web/`, dazu `scripts/ngc-web.sh` im ulh-Repo (`shared/ulh.md`). Verträge: V1,
V2, V5, V6, V7 (`shared/README.md`). HTML, CSS, JavaScript und PHP, ohne Framework und ohne
Build-Schritt.

## Aufbau

```text
apps/web/
├── VERSION
├── public/          Webroot: nur das ist von außen erreichbar (Apache zeigt auf diesen Ordner)
│   ├── index.php    einziger Einstiegspunkt, liefert den Desktop aus
│   └── assets/      Desktop (Fenster, Taskleiste, Startmenü, Anmeldung), die Verträge in
│                    JavaScript, ngc.js für Apps (MIT); Fremdcode für den Browser in
│                    assets/vendor/
├── assets/          NICHT im Webroot, und das ist der Zweck: je mitgelieferte App ein
│                    Ordner (apps/calculator, apps/terminal) mit genau dem, was im ui/
│                    ihres Pakets liegt - index.html, app.css, app.js. Eine .html unter
│                    public/ wäre ein Dokument der Herkunft der Shell und damit genau
│                    der zweite Weg, den der abgeschottete Rahmen zumacht.
├── classes/         PHP: web.json und trust-anchor.json lesen, Liste der API-Server
├── templates/       Gerüst der Desktop-Seite und des App-Fensters, dazu zwei Vorlagen
│                    für Apache: apache-vhost.conf (der virtuelle Host - nur public/ ist
│                    erreichbar, und die Kopfzeilen für die Dateien, die Apache selbst
│                    ausliefert; eine eigene Richtlinie, nicht die der Seite, Begründung
│                    je Zeile in der Datei) und apache-apis.conf.example (die Form, die
│                    ngc-web für die API-Server schreibt - nie von Hand kopieren)
└── tests/           Playwright; npm nur hier
```

Wächst ein Ordner über etwa 20 Dateien, darf er Unterordner bekommen, mit Namen aus dem
Projektstruktur-Standard (`CLAUDE.md` im Stamm, Abschnitt „Aufbau“).

## Aufgabe

Der Desktop aus `docs/prototype.html`, aber sauber: Fenster, Taskleiste, Startmenü, hell und
dunkel. Dazu:

- Anmeldung: Benutzer und Passwort mit Passwortschlüssel im Browser (Argon2id nach `proof.KDF` über
  das Passwort in NFC, auch wenn es in NFD getippt wurde, siehe `proof_kdf.json`; Ed25519), TOTP,
  Passkeys und Sticks (WebAuthn, immer mit Benutzerprüfung). Das Passwort verlässt den Browser nie.
  Die Anmelde-Antwort nennt je Passkey `credId` **und** `kid`: `credId` kommt in
  `allowCredentials`, `kid` unverändert in `proof.kid` des Beweises. Du hast zu diesem Zeitpunkt
  noch keine Vertrauensliste, also rate die Kennung nie und leite sie nie ab.
  Verlangt eine Rolle das Passwort zusätzlich (`session.LoginChallenge.PasswordToo`), kommt `proof`
  von einem Passkey und `password` vom Passwortschlüssel, beide über dieselben Zertifikat-Bytes
  (Zweck `clientcert/v1`).
- HTTP bis zum WebSocket genau nach `shared/README.md`, Abschnitt „Vor dem WebSocket“. Fehler
  erkennst du am Code im CLOSE-JSON (`wire.Code`), nicht am Status; `E_LOGIN` sagt nie, was falsch
  war, bei `E_LOCKED` wartest du `Retry-After` Sekunden. Antworten, die Apache selbst gibt (502 oder
  503, wenn keine API antwortet oder ihre Zertifikatsprüfung scheitert), tragen kein CLOSE-JSON:
  Jede Fehlerantwort, die kein JSON ist, behandelst du als `E_UNREACHABLE`. Das Cookie setzt und
  löscht die API.
- Ersteinrichtung: Zuerst fragst du den Einmal-Code ab (`session.NormalizeSetupCode`), dann prüfst
  du das Angebot von `GET /api/v1/setup` damit wie `session.ParseSetupOffer`, bevor du `info` liest.
  Fassung 1: der Admin, die API-Knoten aus `info.nodes`, wahlweise der globale Store aus
  `info.store`, Dauerfreigaben nur für `ngc-web`, `ngc-api` und `ngc-controller` mit
  `info.releaseKey` und für `system`. Du schickst sie als `session.Setup` mit `session.SetupMAC`
  über die genauen Bytes der Nutzlast; was die API prüft, sagt `session.VerifySetup`. Den Code
  schickst du nie.
- Sitzung: eigene Schlüsselpaare (Ed25519 und X25519, nicht exportierbar), `session.ClientCert`
  bauen und unterschreiben lassen, `session.APICert` prüfen (`session.VerifyAPI`), Noise-Sitzung
  zur API (`noisep`, Prolog `ngc/session/v1`), Frames nach `wire`, Ablauf nach
  `shared/README.md`, Abschnitt „Kanäle“. Deine Kanalnummern zählst du selbst, bis sie wieder
  frei sind (CLOSE gesendet und empfangen), und öffnest nie mehr als `wire.MaxSessionChannels`;
  sonst beendet die API die Leitung.
- Kanäle zu Kern-Aktionen öffnest du nur, wo `core.ShellOpens` es sagt (die Aktionen des Dienstes
  `api` und die Root-Aufgaben des Dienstes `controller`, diese mit Aufgabe). Bei `activate` schickst
  du nach der ersten GRANT sofort EOF und keine DATA; die Datei schreibt die API. Ein Ergebnis gilt
  nur, wenn der Kanal mit CLOSE `ok` endet (Ende zu Ende: das Ende des Controllers mit `ok`); sonst
  verwirfst du, was kam.
- Vertrauensliste: Anker vom Webserver holen, Kette prüfen wie `trust.Open` und `trust.Advance`
  (erst Hash bzw. Unterschrift, dann die Nutzlast lesen), merken; bei einem Bruch Alarm und
  abmelden. Neue Fassungen per `GET /api/v1/trust?from=N` (`session.TrustChain`, seitenweise mit
  `more`); die Steuernachricht `trust` ist nur ein Hinweis. Aus der Liste kommen die Schlüssel der
  Controller für Ende zu Ende. Neue Fassungen, die du baust, bestehen `trust.List.Validate` und
  gehen über die Kern-Aktion `ngc.users/api/trust` an die API.
- Wartende Controller (`ngc.users/pending`) und API-Server (`ngc.users/pending-api`): Den
  Fingerabdruck, den der Admin mit der Ausgabe von ulh vergleicht, rechnest du selbst aus
  (`tunnel.Fingerprint` des Schlüssels bzw. `tunnel.JoinFingerprint` über `node`, `noise`, `sign`,
  `tls` und `url`). Einen API-Server nimmst du als `trust.Node` der Art `api` auf, mit genau den
  Werten aus `core.PendingAPI` und `name` = `node`; das ist sicher, weil der Admin
  `tunnel.JoinFingerprint` über genau diese Werte mit ulh vergleicht. Seine Adresse muss
  `trust.ValidTunnelURL` bestehen (sonst lehnt `trust.List.Validate` deine neue Fassung ab); du
  zeigst sie Zeichen für Zeichen, damit der Admin einen fremden Host erkennt.
- Ende zu Ende: pro Kanal Noise zum Controller aus `core.DesktopApp` (Feld `node`, nicht
  anzeigen). Prolog `noisep.E2EPrologue` mit SHA-256 von `params` und von `task`, jeweils genau den
  Bytes, die du ins OPEN geschrieben hast (fehlt das Feld: SHA-256 des leeren Texts);
  Noise-Nachricht 1 und 2 ohne Nutzlast. Sofort nach Nachricht 2 die erste Nachricht schicken
  (leere Daten oder gleich Eingabe und Ende), innere Nachrichten nach `wire.E2E`; nach deinem Ende
  nichts mehr. Das Ergebnis steht im Ende des Controllers; fehlt es, ist die Antwort abgeschnitten:
  Fehler zeigen.
- Ergebnisse nach Art von `call` nimmst du höchstens bis `wire.MaxCallResult` Byte an und schließt
  bei mehr mit `E_TOOLARGE`: die Ergebnisse der API bei Kern-Aktionen und auf Ende-zu-Ende-Kanälen
  die Ausgabe von Aktionen ohne `stream` im Manifest. Dort sieht die API den Klartext nicht, also
  setzt nur du die Grenze durch.
- Aufgaben: `task.Task` bauen und mit dem Sitzungsschlüssel unterschreiben, bei Bedarf erneute
  Bestätigung (Passkey oder Passwort erneut) mit Zweck `confirm/v1`. Ein OPEN mit Aufgabe hat keine
  eigenen Parameter.
- Kopplungs- und Beitrittscodes (Kern-Aktionen `pair-code` und `join-code`) zeigst du erst, wenn
  `tunnel.PairCode.Check` bzw. `tunnel.JoinCode.Check` gegen deine neueste geprüfte Fassung
  stimmt; nennt der Code eine neuere, holst und prüfst du sie zuerst. Dazu Nummer und Fingerabdruck
  des Ankers (`tunnel.Fingerprint` des Hashes).
- Apps: abgeschottete iframes, `ngc.js` (MIT) mit `ngc.call` und `ngc.open`, strenge CSP,
  Oberflächen für App-Store (Kataloge mit `store.VerifyCatalog` prüfen), Benutzer & Rechte, Rechner
  und Terminal, Anzeige und Einstellung der Updates.
- Oberflächen der Apps (`ngc.desktop/ui`) kommen als ui-tar in genau einer Form, die der Kommentar
  in `store/ui.go` Feld für Feld festlegt (`store.UIWriter` schreibt sie, `store.WalkUI` liest
  sie). Dein Leser folgt diesen Regeln Zeile für Zeile und lehnt alles andere ab; die Testdaten
  sind `ui.json`. Die Fälle „… header only“ darin lehnst du schon nach dem Kopf ab, ohne Daten zu
  lesen: Grenzen prüfst du am Kopf, bevor du weiterliest. Fälle mit echten Daten bis
  `store.MaxUIFile` je Datei, `store.MaxUITotal` insgesamt und `store.MaxEntries` Einträgen stehen
  nicht in `ui.json`; die erzeugst du in deinen Tests selbst. Für die Grenze der Verzeichnisse
  (`store.MaxDirs`) gibt es dagegen Fälle in `ui.json`.
- Bevor irgendetwas einer App-Oberfläche läuft, holst du ihren Beweis (`ngc.desktop/ui-proof`,
  `core.UIProof`) und prüfst ihn wie `core.ParseUIProof` und `core.VerifyUIProof` für App und
  Version aus `core.DesktopApp` gegen deine neueste geprüfte Fassung (Testdaten `uiproof.json`).
  Dann lädst du `ngc.desktop/ui` ganz, vergleichst sein SHA-256 mit der Prüfsumme aus dem Beweis
  und liest es erst danach nach den Regeln von `store.WalkUI`. Geprüfte Prüfsummen merkst du dir je
  App und Version, Kataloge je Store und `seq`; nach jeder neuen Fassung der Vertrauensliste prüfst
  du die Beweise neu, vor allem die hochgeladener Apps (sie gelten nur, solange Schlüssel und
  Root-Recht des freigebenden Admins darin stehen). Welche signierte Version läuft, entscheidet die
  API, auch eine ältere (etwa nach einem gescheiterten Update, das der Controller zurückgenommen
  hat); eine Oberfläche, die niemand für diese App signiert hat, führst du nie aus. Gestartet
  wird die Seite aus `core.DesktopApp.Entry` – nur, wenn sie eine Datei des geprüften ui-tar ist
  (`store.ValidPath`, und `store.WalkUI` hat sie geliefert); sonst zeigst du einen Fehler. Der
  Wert ist nicht signiert und wählt allein unter diesen Dateien.
- Hochladen (`ngc.store/upload`): SHA-256, Manifest (`store.ParseManifest`) und UI-Prüfsumme
  (`store.PackageUI`, Testdaten `packui.json`) rechnest du aus der Datei, die du schickst. Du liest
  sie wie `store.Walk`, also auch mit PAX-Köpfen und langen GNU-Namen, nicht nur die USTAR-Pakete
  aus `packui.json`; was du nicht lesen kannst, lehnst du ab. Weicht `core.UploadResult` davon ab,
  brichst du ab. Unterschrieben werden nur deine eigenen Werte: `core.ActivateParams` mit `sha256`,
  `ui` und `grants` genau gleich den `needs` des Manifests; erlaubt der Admin nicht alle, wird die
  App nicht aktiviert. Bei Store-Apps kommen `sha256` und `ui` aus dem geprüften Katalog.
- Ziel einer Aktivierung: Der Admin wählt den Knoten aus `ngc.users/nodes` (`core.NodeInfo`); für
  eine App mit Diensten nur einen Controller, der online ist und die Rolle `services` hat. Die
  Werte sind unsignierte Hinweise; was er nicht ausführen kann, lehnt der Controller ab.
- Updates (`ngc.store/updates`): eine Zeile je Ziel und Controller (`core.UpdateInfo`). Ein Update
  per Klick ist je Zeile eine Root-Aufgabe `install` mit `target`, `version` = `available` und
  `sha256` der Zeile (nur bei `state` `downloaded`; bei `system` nur `target`). `sha256` darfst du
  aus der Zeile nehmen: Der Controller installiert nur eine Datei, die zur Aufgabe und zum
  signierten Release oder Katalogeintrag passt, ein falscher Wert lässt das Update nur scheitern.
  Schlüssel für Dauerfreigaben nimmst du nie von der API: für NGC-Teile den Release-Schlüssel aus
  Fassung 1 (`info.releaseKey` des per MAC geprüften Angebots) oder aus den bestehenden
  Dauerfreigaben deiner geprüften Liste, für Apps den Schlüssel ihres Stores aus der geprüften
  Vertrauensliste; `key` der Zeile dient nur Anzeige und Abgleich. Für eine Zeile im Zustand
  `needsgrants` (`store.UpdateNeedsGrants`) gibt es nie ein Update per Klick: Die neue Version
  braucht mehr Rechte, als die App beim Aktivieren bekommen hat. Du bietest stattdessen die
  Aktivierung der neuen Version an, mit `grants` = den `needs` des geprüften Katalogeintrags.
- JSON von außen strikt wie in Go (`shared/README.md`, Abschnitt „Striktes JSON“: Regeln und
  Budgets). Jede Grenze aus den Go-Paketen gilt auch hier. Texte für Menschen prüfst du wie
  `wire.ValidText`, Codepunkt für Codepunkt (`shared/README.md`, Regeln).
- PHP: liefert die Seite aus, liest `/etc/ngc/web.json` (`apis`: Objekte mit `url` und `tls`) und
  `/etc/ngc/trust-anchor.json` (`config`), verwaltet die Liste der API-Server. Kein App-Code läuft
  im Webserver.

## Sprachen

Kein sichtbarer Text steht fest im Code. Jeder Satz kommt über einen Schlüssel
aus `apps/web/lang/<sprache>.json` – fünf Dateien, `de`, `en`, `fr`, `it`, `es`,
mit genau denselben Schlüsseln.

- **Eine neue Sprache ist eine Datei mehr und sonst nichts.** Kein Verzeichnis
  der Sprachen im Code: `classes/Lang.php` liest den Ordner und waehlt gegen
  `Accept-Language`. Dieselbe Liste beantwortet `GET /lang/index.json`, und
  `GET /lang/<kennung>.json` gibt eine einzelne Datei heraus – fuer alles,
  was nicht diese Seite ist.
- **Die Tabellen stehen in der Seite, nicht im Netz.** `templates/desktop.php`
  schreibt die gewaehlte Sprache und Englisch als JSON in
  `<script type="application/json" id="ngc-lang">` im Kopf;
  `public/assets/js/lang.js` liest sie von dort – ohne `await`, damit `t()`
  ueberall ein gewoehnlicher Aufruf ist. Warum nicht mit `await` geholt: ein
  Modul mit `top-level await` gibt WebKit nicht mehr an ein `import()` aus einer
  eingespielten Auswertung heraus, und das hat 19 vorher gruene Faelle von
  `tests/shell/users.spec.js` gekostet (gemessen; Begruendung steht in
  `lang.js`).
- **Fehlt ein Schlüssel oder eine Sprache, kommt der englische Text.** Nie
  ein leerer Platz und nie der nackte Schlüssel: die Seite trägt
  Englisch immer mit.
- **Platzhalter statt halber Sätze.** `{name}`, `{seconds}`, `{role}` – ein Satz
  ist ein Schlüssel, damit andere Sprachen ihre eigene Satzstellung haben.
- **PHP holt seine eigenen Seiten aus demselben Ordner** (`Lang::t`), weil die
  Fehlerseiten vor jedem JavaScript ausgeliefert werden. Der Ordner liegt
  **nicht** unter `public/`; nur eine geprüfte Kennung wird je ein Dateiname.
- **Die Sprache wählt PHP, gemessen wird sie über HTTP.** Auf dem
  WebKit von Playwright lässt sich `Accept-Language` von hier aus nicht
  setzen – gemessen mit einer Sonde über alle drei Maschinen: ein
  Kontext mit `locale: "is-IS"` **und** gesetztem Kopf bekommt auf diesem
  Rechner trotzdem eine deutsche Seite, Chromium und Firefox eine englische.
  Darum prüfen die Fälle zur Sprachwahl die Antwort des Servers
  direkt, und die Browser-Fälle prüfen, dass die Seite zeigt, was in
  ihrem Block steht.
- Geprüft wird das in `tests/lang/`: dieselben Schlüssel in allen fünf Dateien,
  dieselben Platzhalter, kein Schlüssel ohne Text, kein Text ohne Schlüssel –
  und ein nackter Schlüssel am Schirm ist ein Fehler.

### Die gespiegelte wire-Tabelle bleibt deutsch

Die vierzehn Sätze in `public/assets/js/contracts/wire.js` (`MESSAGES`,
`message()`) sind **Vertragsgegenstand, nicht Oberfläche**: dieser Text wandert
im CLOSE-Rahmen mit (`errorClose`), `parseClose` nimmt einen CLOSE nur für einen
Code aus genau dieser Tabelle, `shared/wire/wire.go` trägt dieselben Wörter, und
ein Test (`tests/contracts/wire.spec.js`) nagelt die Gleichheit fest. `shared/`
ist eingefroren, also bleibt die Spiegelung deutsch und der Test unverändert.

Was die Masken zeigen, ist daneben ein eigener Satz je Code aus `lang/`
(`wire.E_LOGIN` und die anderen dreizehn, dazu `wire.unknown`), geholt über
`codeText()` in `lang.js`. Zwei Dinge, wo vorher eines war: ein deutsches
Vertragswort und ein Satz je Sprache.

### Was noch deutsch im Code steht, und warum

- `assets/apps/calculator/app.js` und `assets/apps/terminal/app.js` (8 Sätze).
  Das sind **Apps**, keine Shell: ihr Dokument läuft in einer eigenen,
  undurchsichtigen Herkunft mit `connect-src 'none'` (`Page::FRAME_CSP`), kann
  `/lang/…` also nicht holen. Eine App übersetzt sich nur über eine Erweiterung
  von `public/assets/ngc.js` – der öffentlichen MIT-Schnittstelle – und das ist
  eine eigene Runde, nicht diese.
- `public/assets/js/shell/fake-link.js` (16 Sätze). Eine **Attrappe für Tests**,
  die mit der Datei verschwindet (siehe `standIn()` in `shell/main.js`). Ihre
  Sätze sind erfundene App-Namen und Beschreibungen, also Testdaten.

## Tests

- Playwright in `apps/web/tests/` gegen Chrome, Firefox und Safari; npm nur dort, feste Versionen.
- Die JavaScript-Umsetzung der Verträge prüft sich gegen `shared/testdata/` (Noise, Signaturen,
  Beweise samt dem NFD-Fall in `proof_kdf.json`, Vertrauensliste, Sitzung samt Ersteinrichtung
  `setup` in `session.json`, Aufgaben, Frames, Kern-Aktionen, Kataloge, ui-tar, Kopplungscodes,
  striktes JSON und Budget in `json.json`, UI-Prüfsummen in `packui.json`, UI-Beweise in
  `uiproof.json`, Hosts und Adressen in `url.json`: `hosts`, `tunnel`, `web`, `origins`,
  `store`). `core.json` enthält jetzt `ngc.users/nodes`, `entry` in `ngc.desktop/apps` und eine
  Zeile `needsgrants` in `ngc.store/updates`. Diese Tests laufen ohne API.
- Ein Test zeigt, dass das Passwort den Browser nie verlässt (Mitschnitt der Anfragen).
- Angriffe: eine untergeschobene Oberfläche (ui-tar, das nicht zum Beweis passt) und ein Mann in der
  Mitte bei der Ersteinrichtung (Angebot mit fremden Schlüsseln).

## Fertig, wenn

Anmeldung in beiden Stufen läuft, der Desktop Apps öffnet, das Terminal über API und Controller
Ausgaben zeigt, eine manipulierte Vertrauensliste Alarm auslöst und die Browser-Tests grün sind.
