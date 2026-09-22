# Verträge

Hier stehen die Schnittstellen zwischen Webinterface, API, Controller und Datenbank. Sie gehören
dem Koordinator. Wer einen Vertrag ändern will, fragt ihn; alle vier Teile erfahren jede Änderung
sofort. Dieses Verzeichnis steht unter MIT (siehe `LICENSE`), NGC selbst unter AGPL-3.0.

Noch kein Vertrag ist die Wiederherstellung (Codes, Zurücksetzen durch einen Admin, Notfallbefehl):
Sie kommt als Vertragsänderung nach der Freigabe der Verträge und vor den Plänen 1 bis 4.

| Vertrag | Inhalt | Pakete | Testdaten |
|---|---|---|---|
| Grundlagen | Kodierung und signierte Umschläge, für alle Verträge | `enc`, `sign` | `sign.json`, `json.json` |
| V1 | Webinterface ↔ API | `session`, `wire`, `noisep`, `proof`, `task`, `core` | `session.json`, `wire.json`, `noise.json`, `proof.json`, `proof_kdf.json`, `task.json`, `core.json`, `uiproof.json` |
| V2 | API ↔ Controller | `tunnel`, `session`, `wire`, `noisep`, `task`, `core` | `tunnel.json`, `wire.json`, `noise.json`, `task.json`, `core.json` |
| V3 | Controller ↔ Dienst im Container | `service` | `service/echo.sh` |
| V4 | API und Controller ↔ Datenbank | `dbapi`, `dbapi/memstore`, `dbapi/dbtest` | – (Konformitätstests) |
| V5 | App-Paket, Katalog, Lizenz, Updates | `store` | `store.json`, `pack.json`, `ui.json`, `packui.json`, `release/` |
| V6 | Konfiguration, Pfade, ulh | `config`, `ulh.md` | `config/*.json`, `release/`, `tls/` |
| V7 | Vertrauensliste, Ende zu Ende | `trust`, `trust/trusttest`, `noisep`, `proof` | `trust.json`, `noise.json` |

## Regeln

- **Signaturen decken immer die genauen Bytes** eines Dokuments ab (`sign.Envelope`, `trust.Signed`).
  Nie neu erzeugtes JSON prüfen, sondern die empfangene Nutzlast. Erst die Unterschrift prüfen,
  dann die Nutzlast lesen; bei der Vertrauensliste erst den Hash des Ankers bzw. den Beweis der
  neuen Fassung (`trust.Open`, `trust.Advance`).
- **Umschläge von außen** liest `sign.ParseEnvelope` (Lizenzen, Kataloge, Releases), einzelne
  Fassungen der Vertrauensliste `trust.ParseSigned` und ganze Ketten `session.ParseTrustChain`.
  `store.VerifyCatalog`, `store.VerifyLicense`, `store.VerifyRelease`, `trust.Open` und
  `trust.Advance` erwarten so gelesene Werte; steckt ein Umschlag in einem größeren Dokument (im
  Kopf einer Übertragung, `core.ReadTransfer`, oder in einem UI-Beweis, `core.ParseUIProof`),
  prüft dessen Parser seine Form (`sign.Envelope.Validate`).
- **Jede Signatur hat einen Zweck** (`sign.Purpose…`). Eine Unterschrift für eine Sache gilt nie für
  eine andere.
- **Jede Eingabe von außen** läuft durch die `Parse…`- oder `Verify…`-Funktion des Vertrags, nie
  direkt durch `json.Unmarshal` (im Browser nie direkt durch `JSON.parse`). JSON ist dabei immer
  strikt, siehe „Striktes JSON“.
- **Grenzen gelten immer**, auch wenn die Gegenstelle zu NGC gehört. Jede Grenze aus den Go-Paketen
  (`Max…`, `Min…`, die Budgets und die Regeln in `Validate`) setzt das JavaScript des Webinterface
  genauso durch, auch die, für die es keine eigenen Testdaten gibt (etwa `trust.MaxAuto`,
  `trust.MaxOrigins`, `trust.MaxRoot`, die Grenzen für Passkey-Beweise `proof.MaxPublic`,
  `proof.MaxSig`, `proof.MaxRSABits`, `proof.MaxAuthData`, `proof.MaxClientData`). Dazu gehören
  die Regeln der Vertrauensliste (`trust.List.Validate`): Keine Rolle ist zugleich `passkeyOnly` und
  `passwordToo`; wer in einer Rolle mit `passkeyOnly` ist, hat keinen Passwortschlüssel, und wer in
  einer Rolle mit `passwordToo` ist, hat einen Passkey und einen Passwortschlüssel. Einzeilige
  Texte für Menschen (Namen, Titel, Meldungen, auch `message` eines CLOSE) prüft `wire.ValidText`
  (`store.ValidText` ist dieselbe Regel): 1 Byte bis zur Grenze des Feldes, druckbares UTF-8, ohne
  Steuerzeichen und Zeilenumbrüche. Beschreibungen und Hinweise (`description`, `notes`) sind
  gültiges UTF-8 bis zu ihrer Grenze; die Shell zeigt alles nur als Text.
- **Druckbar** heißt, was die Unicode-Tabellen von Go sagen (Unicode 17.0 mit Go 1.27):
  Buchstaben, kombinierende Zeichen, Zahlzeichen, Satzzeichen, Symbole und das Leerzeichen U+0020.
  In JavaScript muss jeder Codepunkt (nicht jede UTF-16-Einheit) auf
  `/^[\p{L}\p{M}\p{N}\p{P}\p{S} ]$/u` passen, und die Länge zählt UTF-8-Bytes. Einen Codepunkt,
  den die Tabellen des Prüfers nicht kennen, zählt er als nicht druckbar; U+FDD0 ist nie druckbar.
  Ein Prüfer mit älteren Tabellen lehnt also manchen Text ab, den Go annimmt: Wer Texte schreibt,
  die andere prüfen, nimmt nur lange zugewiesene Zeichen. Neue Unicode-Tabellen in Go sind eine
  Vertragsänderung (`TestValidText` in `shared/wire` hält sie an).
- **Selbst rechnen:** Was die Shell unterschreibt oder den Admin vergleichen lässt, rechnet sie
  selbst aus, nie aus Antworten der API: Prüfsumme, Manifest und UI-Prüfsumme (`store.PackageUI`)
  eines Pakets, das sie hochlädt (`core.UploadResult` dient nur dem Abgleich), die Fingerabdrücke
  wartender Controller (`tunnel.Fingerprint` ihres Schlüssels) und API-Server
  (`tunnel.JoinFingerprint`). Ein Paket liest sie dafür wie `store.Walk`, also in jeder Form, die
  Go `archive/tar` liest, auch mit PAX-Köpfen und langen GNU-Namen; `packui.json` zeigt nur die
  USTAR-Pakete von `ngc-store pack`. Was sie nicht lesen kann, lehnt sie ab. Rechnet sie anders als
  Go, scheitert nur die Aktivierung: Eine UI-Prüfsumme, die nicht zum Paket passt, lehnt der
  Controller ab (`E_VERIFY`).
- **TLS intern:** Jede TLS-Strecke zwischen NGC-Teilen prüft ihre Gegenseite; eine Prüfung
  abzuschalten ist verboten. API-Server weisen sich mit dem Fingerabdruck ihres TLS-Schlüssels aus
  (`trust.Node.TLS`, `trust.TLSPin`; in Go `tunnel.PinnedTLS`, im Skript `testdata/tls/pin.sh`),
  MariaDB-Knoten mit einem Zertifikat der CA der Installation (`config.DB.CA`,
  `config.DB.TLSConfig`). Auf dem Tunnel und dem WebSocket der Sitzung prüft Noise beide Enden; das
  TLS darum trägt sie nur durch Firewalls und Proxys und wird trotzdem geprüft. Abschalten heißt:
  eine Verbindung ohne Prüfung. Einen Fingerabdruck prüft Go nur mit `InsecureSkipVerify` zusammen
  mit `VerifyConnection`; beides setzt `tunnel.PinnedTLS`, sonst steht `InsecureSkipVerify`
  nirgends.
- **Signierte Dokumente, die bleiben:** Release, Katalog und Lizenz bekommen ein neues Feld nur mit
  einem neuen Zweck (`release/v2` usw.) an einem eigenen Ort; in der Übergangszeit signiert der
  Herausgeber beide Fassungen, jeder Leser nimmt die neueste, die er kennt. Ein neues Feld im
  Manifest braucht `minNgc` (im Manifest und im Katalog) mindestens bei der NGC-Version, die es
  liest: Eine Version, deren `minNgc` neuer ist als die NGC-Version des Knotens, bietet die API
  nicht an, und der Controller installiert sie nicht. Die Vertrauensliste ist eine einzige Kette:
  ein neues Format nur mit einer neuen Vertragsnummer, sobald alle Controller sie in `hello` nennen;
  ein Controller, der dabei fehlte, spielt erst sein Update ein und liest dann weiter. Das ui-tar
  ändert sich nie; eine andere Form wäre eine neue Aktion.
- **Geheimnisse** in Konfigurationen sind `config.Secret` (Schlüssel) und `config.SecretText`
  (Passwörter). Den Wert geben nur `Bytes()` bzw. `Text()`; fmt, `log/slog` und `json.Marshal`
  zeigen `[secret]`. Eine Konfigurationsdatei schreibt Go nur mit `config.SaveController`,
  `config.SaveAnchor` oder `config.SaveWeb`, nie mit `json.Marshal`. Ein Passwort, das wörtlich
  `[secret]` lautet, lehnt `config` ab.
- **Rollen und Softwareteile:** Ein Server hat Rollen (`tunnel.RoleWeb`, `tunnel.RoleAPI`,
  `tunnel.RoleDB`, `tunnel.RoleServices`: `web`, `api`, `db`, `services`). Softwareteile sind
  `ngc-web`, `ngc-api` und `ngc-controller` (`trust.AutoWeb`, `trust.AutoAPI`,
  `trust.AutoController`). Release-Dateien und `tunnel.Status.Components` nennen Softwareteile,
  Update-Ziele ebenso (dazu `system` und Apps); welche Softwareteile ein Server mit seinen Rollen
  braucht, sagt `tunnel.Components`.
- **Testdaten**: `shared/testdata/` ist die gemeinsame Wahrheit (Tabelle unten). Go-Tests
  schreiben sie mit dem Schalter `-update`, gezielt mit `-run` auf den Test, der die Datei erzeugt.
  `proof.json` enthält zufällige Schlüssel und entsteht nur bei einer Vertragsänderung neu. Das
  Webinterface prüft sich in `apps/web/tests/` dagegen. Eine geänderte Datei ist eine
  Vertragsänderung.
- **Vertragsfassungen:** Zwischen Browser und API steht die Nummer der Fassung im Pfad, heute
  überall `/api/v1/…` (`session.Path…`, auch die WebSocket-Leitung `session.PathSocket`). Neue
  oder geänderte Felder kommen nur mit einer neuen Nummer, denn striktes JSON lehnt unbekannte
  Felder ab. Die API bedient die aktuelle und die vorige Nummer nebeneinander, jede unter ihren
  eigenen Pfaden; eine alte Shell spricht weiter `/api/v1/…`, bis sie aktualisiert ist. Das passt
  zur Reihenfolge der Updates (Datenbank → API → Controller → Webinterface, Entwurf 4.8): Das
  Webinterface ist nie neuer als die API. Im Tunnel nennt `tunnel.Hello` die Fassung (`contract`,
  heute `tunnel.Contract` = 1); die neuere Seite spricht die Fassung der älteren, und beide liegen
  höchstens eine Fassung auseinander.
- **Nachrichten vor der Einigung** bleiben über alle Nummern gleich: `hello` (`tunnel.Hello`),
  `tunnel.PairRequest`, `tunnel.JoinRequest`, `tunnel.PairCode` und `tunnel.JoinCode`. Mit ihnen
  erfahren die Seiten erst, welche Fassung die andere spricht. Eine Änderung daran bekommt einen
  neuen Nachrichtentyp bzw. ein neues Präfix (etwa `ngc-pair2:`).
- **Sprache**: Code und Kommentare Englisch, Meldungen für Benutzer Deutsch (`wire.Code.Message`).

## Testdaten

| Datei | Inhalt | Erzeugt von |
|---|---|---|
| `sign.json` | ein signierter Umschlag | `sign`: `TestVectors` |
| `json.json` | Prüfdokument mit jeder Feldart (`schema`), sein Budget (`size`, `items`, `totals`) und gültige und ungültige Fälle (`cases`, das Dokument als base64url) | `enc`: `TestJSONVectors` |
| `proof.json`, `proof_kdf.json` | Beweise mit Passkey und Passwortschlüssel, gültige und ungültige; Argon2id, auch für ein Passwort, das in NFD getippt wurde (`typed`; `password` ist dessen NFC) | `proof`: `TestProofVectors`, `TestKDFVectors` |
| `trust.json` | Ketten von Fassungen ab einem Anker, gültige und ungültige | `trust`: `TestVectors` |
| `session.json` | Client- und API-Zertifikat, dazu abgelehnte (`apiRejects`, `clientRejects`); `setup`: Einmal-Code, getippte Formen (`typed`), Angebote (`offers`) und Anfragen (`requests`) der Ersteinrichtung, gültige und ungültige | `session`: `TestVectors` |
| `task.json` | Root-Aufgabe (`deactivate`) und erneute Bestätigung | `task`: `TestVectors` |
| `wire.json` | `frames`, `badFrames`, `grants`, `opens`, `closes`, `e2e` (innere Nachrichten), `controls` (`wire.ControlAllowed`) | `wire`: `TestVectors` |
| `noise.json` | ein Ende-zu-Ende-Handschlag mit festen Schlüsseln und Transportnachrichten; `prologues`: je ein OPEN mit seinen `params`- und `task`-Bytes (base64url, leer, wenn das Feld fehlt), Prolog und Nachricht 1 | `noisep`: `TestVectors` |
| `tunnel.json` | Kopplungscode (mit `tls`) mit MAC und Fingerabdruck, `pairCheck` (`tunnel.PairCode.Check`), `join` (Beitrittscode und Anfrage mit `node`, `tls`, `url`, MAC nach `tunnel.JoinMAC` und `fingerprint` nach `tunnel.JoinFingerprint`), `tlsPin` (Fingerabdruck von `tls/api.pem`) | `tunnel`: `TestVectors` |
| `tls/` | `api.pem`: TLS-Zertifikat von `api-1` aus `trust.json`; `pin.sh` (von Hand) rechnet den Fingerabdruck wie `trust.TLSPin`, geprüft von `TestTLSPinScript` | `tunnel`: `TestVectors` |
| `core.json` | ein Beispiel-Ergebnis je Kern-Aktion mit Ergebnis (auch `ngc.desktop/ui-proof` und `ngc.users/pending-api`; Kopplungs- und Beitrittscode sind echte Codes zur Fassung „genesis“ aus `trust.json`, `ngc.store/catalog` ein echter Katalog des globalen Stores), dazu `notice`; unter `empty` die neun Ergebnisse, die Listen sind, leer, wie `core.EncodeResult` sie schickt (`[]`) | `core`: `TestExamples` |
| `uiproof.json` | UI-Beweise gegen eine Vertrauensliste (`trust`): je Fall App, Version, Beweis, die UI-Prüfsumme, die er belegt, und ob er gilt | `core`: `TestUIProofVectors` |
| `store.json`, `pack.json` | Katalog (mit `ui` je Version), Lizenz und Release, je mit abgelehnten (`catalogRejects`, `licenseRejects`, `releaseRejects`) und den Werten zum Prüfen (`store`, `installation`, `installKey`, `catalogLastSeq`, `releaseLastSeq`); Verschlüsselung der Kauf-Apps | `store`: `TestVectors`, `TestPackVectors` |
| `packui.json` | Pakete, wie `ngc-store pack` sie schreibt, mit dem ui-tar von `store.PackageUI` und dessen SHA-256 | `store`: `TestPackageUIVectors` |
| `ui.json` | ui-tars, gültige und ungültige (Regeln im Kommentar von `store/ui.go`); bei gültigen unter `files` die Dateien, die `store.WalkUI` liefert. Fälle mit Daten bis `store.MaxUIFile`, `store.MaxUITotal` oder mit `store.MaxEntries` Einträgen stehen nicht darin | `store`: `TestUIVectors` |
| `release/` | signiertes Release mit Release-Schlüssel als PEM und einer Datei; `verify.sh` (von Hand) prüft wie die ulh-Skripte die Datei eines Teils (`verify.sh <Verzeichnis> <Teil> <Arch>`), `TestReleaseScript` in 31 Fällen | `store`: `TestReleaseScriptVector` |
| `config/*.json` | Beispiel-Konfigurationen | von Hand |
| `service/echo.sh` | Beispiel-Dienst | von Hand |

## Striktes JSON

Go liest JSON von außen mit `enc.DecodeStrict`, immer über ein `enc.Budget`. Das Webinterface lehnt
genau dieselben Dokumente ab; `JSON.parse` allein reicht dafür nicht. Die Testdaten dafür stehen in
`json.json`. Einzige Ausnahme ist `clientData` eines Passkey-Beweises: Das schreibt der Browser,
und WebAuthn erlaubt darin weitere Felder; `proof.Verify` liest davon nur `type`, `challenge`,
`origin` und `crossOrigin`.

1. Genau ein JSON-Wert. Leerraum davor und danach ist erlaubt (Leerzeichen, Tab, CR, LF), sonst
   nichts, auch kein BOM.
2. Höchstens die Bytegrenze des Dokuments (Tabelle unten).
3. Feldnamen genau wie im Vertrag, auch in Groß- und Kleinschreibung; unbekannte Felder werden
   abgelehnt.
4. Kein Feldname doppelt, in keiner Tiefe, auch nicht in rohen Werten. Verglichen wird nach dem
   Auflösen der Escapes: `"a"` und `"\u0061"` sind derselbe Name.
5. Nur gültiges UTF-8, auch kein einzelnes Surrogat als Escape (`"\ud800"`).
6. Rohe Werte (`json.RawMessage`, etwa `params`) bleiben Byte für Byte erhalten.
7. Alle Zahlenfelder der Verträge sind ganze Zahlen und stehen nur als Ziffern da: `1.0`, `0.0`,
   `1e2` und `10E0` werden abgelehnt. Vorzeichenlose Felder (`uint…`) lehnen jedes Minus ab, auch
   `-0`. Es gilt der Wertebereich des Go-Typs. Nie eine Zahl, wo Text oder `true`/`false` steht,
   und umgekehrt.
8. Bytes (`enc.Bytes`) sind base64url ohne `=`: nur `A-Z a-z 0-9 - _`, also auch kein CR, LF,
   Leerzeichen oder Tab; die unbenutzten Bits am Ende sind 0. Ebenso die IDs, die `trust.ValidID`
   prüft (16 Byte: Benutzer, Installation), und Schlüssel-IDs (16 Byte, `sign.KeyID`; geprüft in
   `proof.Proof.Validate` und `sign.ParseEnvelope`). Andere IDs haben eigene Regeln: Knoten und
   Stores `wire.ValidName`, Apps `wire.ValidAppID`, Einträge im Protokoll `core.ValidLogID` (Hex aus
   Kleinbuchstaben).

Vorher prüft das Budget in einem Durchgang, der nichts aufbaut:

9. Listen nur an den Pfaden, die das Budget nennt, je mit höchstens so vielen Elementen; ebenso
   Objekte, die Go als Map liest (Zahl der Einträge). Ein Pfad verbindet die Feldnamen von außen
   nach innen mit `/`, `*` steht für jedes Element: `users/*/keys`. `0` erlaubt nur eine leere
   Liste. Dazu kann ein Budget einen dieser Pfade über das ganze Dokument begrenzen
   (`enc.Budget.Totals`): alle Listen (Maps) an diesem Pfad zusammen, etwa die Versionen aller Apps
   eines Katalogs (Tabelle, „insgesamt“).
10. Jedes andere Objekt ist eine Struktur: höchstens 64 Felder (`enc.MaxMembers`), jeder Name
    höchstens 64 Byte nach dem Auflösen der Escapes (`enc.MaxName`), ohne `/` und nicht `*`.
11. In rohen Werten zählt nichts, und die Namen sind frei (doppelt dürfen sie nicht sein, Regel 4);
    12 bis 14 gelten auch dort. Ein roher Wert hat immer eine eigene kleine Größengrenze.
12. Höchstens 32 Ebenen (`enc.MaxDepth`); der äußerste Wert ist Ebene 1.
13. Keine Zahl, deren genauer Wert dem Betrag nach größer ist als 2^53−1 (`enc.MaxSafeInteger`):
    `9007199254740991` und `9007199254740991.0` gehen, `9007199254740992`, `9007199254740991.4`
    und `1e16` nicht. Es zählt der Wert der Ziffern: Ein `Number` in JavaScript rundet
    `9007199254740991.4` auf 2^53−1 und darf deshalb nicht der Prüfer sein.
14. Nirgends `null`. Wer JSON schreibt, lässt ein leeres Feld weg. Ergebnisse der API kodiert
    `core.EncodeResult`: Eine leere Liste als Ergebnis ist `[]`, nie `null` (Beispiele unter
    `empty` in `core.json`), und optionale Felder mit Nullwert (`""`, 0, `false`) fehlen.

| Dokument | Go | Bytes | Listen und Maps (Pfad: höchstens) |
|---|---|---|---|
| Fassung der Vertrauensliste (Nutzlast) | `trust.Parse` | `sign.MaxPayload` (4 MiB) | `users` 10000, `users/*/roles` 256, `users/*/keys` 16, `roles` 256, `roles/*/black` 256, `roles/*/white` 256, `nodes` 256, `nodes/*/rootUsers` 256, `nodes/*/rootRoles` 256, `stores` 64, `auto` 64, `rp/origins` 8 |
| Fassung samt Beweis (`trust.Signed`), etwa an `ngc.users/api/trust` | `trust.ParseSigned` | base64url von `sign.MaxPayload` Byte + 64 KiB | keine |
| Anmeldung, Ersteinrichtung | `session.ParseLoginStart`, `session.ParseLoginFinish`, `session.ParseSetup` | `session.MaxRequest` (64 KiB) | keine |
| Angebot der Ersteinrichtung | `session.ParseSetupOffer` | `session.MaxSetupOffer` (128 KiB), darin `info` höchstens `session.MaxRequest` | `info`: `nodes` 16 (`session.MaxSetupNodes`) |
| Client- und API-Zertifikat | `session.VerifyClient`, `session.VerifyAPI` | `session.MaxCert` (4096) | keine |
| Antwort von `GET /api/v1/trust` | `session.ParseTrustChain` | `session.MaxChain` (32 MiB) | `versions` 64 |
| Steuernachricht `trust` | `session.ParseTrustHint` | 64 | keine |
| Aufgabe | `task.Verify` | `task.MaxTask` (16 KiB) | `params` roh |
| OPEN des Browsers | `wire.ParseOpen` | `wire.MaxBrowserOpen` (16 KiB) | `params` roh, höchstens `wire.MaxParams` (8 KiB) |
| CLOSE, auch im Ende des Controllers | `wire.ParseClose`, `wire.ParseE2E` | `wire.MaxClosePayload` (4096) | keine |
| Steuernachricht | `wire.ParseControl` | `wire.MaxPayload` (65514) | `b` roh |
| Steuernachricht `hello` | `tunnel.ParseHello` | `tunnel.MaxHello` (4096) | `roles` 4 |
| Steuernachricht `status` | `tunnel.ParseStatus` | `wire.MaxPayload` | `apps` 256 (`tunnel.MaxStatusApps`), `components` 3 (Map) |
| Steuernachricht `notify` | `core.ParseNotice` | `core.MaxNotice` (1024) | keine |
| Steuernachricht `update` | `store.ParseUpdateReport` | 4096 | keine |
| OPEN im Tunnel | `tunnel.ParseOpen` | `wire.MaxOpenPayload` (40 KiB) | keine |
| Kopplungs- und Beitrittscode | `tunnel.ParsePairCode`, `tunnel.ParseJoinCode` | `tunnel.MaxPairCode` (1024, der ganze Code) | keine |
| Kopplungs- und Beitrittsanfrage | `tunnel.ParsePairRequest`, `tunnel.ParseJoinRequest` | `tunnel.MaxPairRequest` (4096) | keine |
| Parameter einer Kern-Aktion | `core.ParseParams` | `wire.MaxParams` (8 KiB) | `rights` 1000, `grants` 2 |
| Kopf einer Übertragung | `core.ReadTransfer` | `core.MaxTransferHeader` (8 MiB) | keine |
| UI-Beweis | `core.ParseUIProof` | `wire.MaxCallResult` (8 MiB) | keine |
| Umschlag (`sign.Envelope`) um Lizenz, Katalog oder Release | `sign.ParseEnvelope` | base64url von `maxPayload` Byte + 1 KiB (`maxPayload` höchstens `sign.MaxPayload`; für Lizenzen `store.MaxLicense`) | keine |
| Katalog | `store.VerifyCatalog` | `sign.MaxPayload` | `apps` 10000, `apps/*/versions` 100, `apps/*/versions/*/needs` 2; insgesamt `apps/*/versions` 30000 (`store.MaxCatalogVersions`) |
| Lizenz | `store.VerifyLicense` | `store.MaxLicense` (8 KiB) | keine |
| Release | `store.VerifyRelease` | `sign.MaxPayload` | `files` 16 |
| Manifest | `store.ParseManifest` | `store.MaxManifest` (64 KiB) | `actions` 64, `services` 16, `services/*/command` 32, `needs` 2 |
| `web.json` | `config.LoadWeb` | `config.MaxConfig` (1 MiB) | `apis` 16 (Objekte mit `url` und `tls`), `rp/origins` 8 |
| `api.json` | `config.LoadAPI` | `config.MaxConfig` | `db/nodes` 16 |
| `controller.json` | `config.LoadController` | `config.MaxConfig` | `apis` 16, `roles` 4, `migrate/nodes` 16 |
| `trust-anchor.json` | `config.LoadAnchor` | `config.MaxConfig` | keine |

Antworten der API, für die Go keinen Parser hat (etwa `session.LoginChallenge`,
`session.LoginResult` und die Ergebnisse in `core.json` außer dem UI-Beweis), liest das
Webinterface nach den Regeln 1, 3 bis 8 und 12 bis 14; ein Ergebnis hat höchstens
`wire.MaxCallResult` Byte.

## Vor dem WebSocket

Bis zum WebSocket spricht die Shell HTTPS mit JSON (`Content-Type: application/json`, sonst
`E_PROTOCOL`); jede Antwort trägt `Cache-Control: no-store`. Fehler haben den Status
`session.HTTPStatus(code)` und als Inhalt das CLOSE-JSON `wire.Error(code)`.

| Pfad | Methode | Inhalt | Antwort | Fehler |
|---|---|---|---|---|
| `/api/v1/setup` | GET | – | 200 `session.SetupOffer` | 404 `E_NOTFOUND`, sobald es einen Admin gibt |
| `/api/v1/setup` | POST | `session.Setup` | 204 | 403 `E_VERIFY`, 404 `E_NOTFOUND`, 429 `E_LOCKED` |
| `/api/v1/login/start` | POST | `session.LoginStart` | 200 `session.LoginChallenge`, auch für unbekannte Namen | 429 `E_LOCKED` |
| `/api/v1/login/finish` | POST | `session.LoginFinish` | 200 `session.LoginResult` und das Cookie | 401 `E_LOGIN`, 429 `E_LOCKED` |
| `/api/v1/logout` | POST | – | 204, Cookie gelöscht | 401 `E_LOGIN` |
| `/api/v1/trust?from=N` | GET | – | 200 `session.TrustChain` ab N; 204, wenn N eins nach der neuesten ist | 401 `E_LOGIN`, 404 `E_NOTFOUND` (N größer) |
| `/api/v1/ws` | GET (Upgrade) | – | 101, Unterprotokoll `ngc.v1` | 401 `E_LOGIN` |

Überall zusätzlich 400 `E_PROTOCOL` (kaputte Anfrage, `from` nicht nach `session.ParseFrom`), 413
`E_TOOLARGE` (mehr als `session.MaxRequest`), 500 `E_INTERNAL`, 503 `E_UNREACHABLE` (Datenbank nicht
erreichbar).

- `E_LOGIN` steht für jede fehlgeschlagene Anmeldung gleich (unbekannter Name, falscher Beweis oder
  TOTP-Code, fehlender Passwortbeweis, verbrauchte oder abgelaufene Nonce) und für eine fehlende
  oder abgelaufene Sitzung. `E_LOCKED` kommt mit `Retry-After` in Sekunden.
- Cookie `ngc_session` (`session.CookieName`): base64url von 32 Zufallsbytes, dazu
  `Path=/api/; Secure; HttpOnly; SameSite=Strict` (`session.CookieAttributes`), ohne `Domain` und
  ohne `Max-Age`; die Datenbank kennt nur `session.TokenHash` der 32 Bytes. Abmelden löscht es mit
  `Max-Age=0`. Die API beendet eine Sitzung nach `IdleTimeout` ohne Nutzung oder nach
  `SessionLifetime` (`config.API`).
- Jede API bedient jede Sitzung: Der private Schlüssel zu `dh` des API-Zertifikats liegt
  verschlüsselt in der Datenbank (`dbapi.Session`). Nach einem Ausfall verbindet sich die Shell mit
  denselben Schlüsseln neu.
- **Ersteinrichtung:** Die Shell fragt zuerst den Einmal-Code ab (`session.NormalizeSetupCode`:
  Leerzeichen, Tabs und `-` egal, Kleinbuchstaben erlaubt) und prüft damit das Angebot
  (`session.ParseSetupOffer`: erst der MAC `session.OfferMAC` über die genauen Bytes von `info`,
  dann `info` nach den Regeln der Vertrauensliste: Knoten nach `trust.ValidAPINode`, nach ID
  sortiert und ohne doppelte ID, der Store nach `trust.ValidStore`). Daraus baut sie Fassung 1: der
  Admin, die API-Knoten aus `info.nodes`, wahlweise der globale Store aus `info.store`,
  Dauerfreigaben nur für `ngc-web`, `ngc-api`, `ngc-controller` mit `info.releaseKey` und für
  `system`. Sie schickt sie mit `session.SetupMAC` über die genauen Bytes der Nutzlast; die API
  prüft sie mit `session.VerifySetup`. Der Code selbst geht nie über die Leitung.

## Kanäle

Die genauen Regeln stehen in den Paketbeschreibungen von `wire`, `noisep` und `tunnel`; hier die
Kurzfassung.

### Leitungen und Frames

- **Leitungen:** Sitzung (Browser ↔ API: `/api/v1/ws`, Unterprotokoll `ngc.v1`, Prolog
  `ngc/session/v1`) und Tunnel (Controller ↔ API: `/tunnel?key=` mit dem Fingerabdruck des
  Controller-Schlüssels, `ngc.tunnel.v1`, Prolog `ngc/tunnel/v1`), beide mit
  Noise_KK_25519_AESGCM_SHA256 (`noisep`). Die erste binäre WebSocket-Nachricht ist
  Noise-Nachricht 1 von Browser bzw. Controller, die zweite Nachricht 2 der API; beide ohne
  Nutzlast, genau 48 Byte (`wire.HandshakeSize`). Danach ist jede binäre Nachricht genau eine
  Noise-Transportnachricht mit genau einem Frame. Eine Textnachricht, ein kaputter Frame oder ein
  Entschlüsselungsfehler beenden die Leitung. Ausgeführt wird erst etwas, wenn die erste
  Transportnachricht des Initiators entschlüsselt ist. Beide laufen in TLS, das nur durch Firewalls
  und Proxys trägt; die Echtheit prüft Noise. Geprüft wird das TLS trotzdem (Regel „TLS intern“):
  Der Browser prüft das Zertifikat des Webservers, Apache und der Controller das der API gegen
  ihren Fingerabdruck.
- **Frames:** 1 Byte Typ, 4 Byte Kanal (Big Endian), dann die Nutzlast; zusammen höchstens
  `wire.MaxFrame` (65519) Byte. OPEN (1): JSON, 1 Byte bis `wire.MaxOpenPayload`. DATA (2): Daten.
  EOF (3): leer. CLOSE (4): `wire.Close` als JSON, 1 Byte bis `wire.MaxClosePayload`. GRANT (5):
  genau 4 oder 52 Byte, erst die Freigabe (uint32, Big Endian, 1 bis `wire.MaxWindow`), dann
  nichts oder Noise-Nachricht 2. Auf Kanal 0 gibt es nur DATA.

### Steuerkanal 0

Kanal 0 ist immer offen und trägt Steuernachrichten (`wire.Control`: Typ `t`, Inhalt `b`). Wer was
schicken darf, sagt `wire.ControlAllowed`; alles andere beendet die Leitung.

| Leitung | Absender | erlaubt |
|---|---|---|
| Sitzung | API | `trust` (`session.TrustHint`), `notify` (`core.Notice`), `bye` |
| Sitzung | Browser | `bye` |
| Tunnel | API | `hello` (`tunnel.Hello`), `bye` |
| Tunnel | Controller | `hello`, `status` (`tunnel.Status`), `update` (`store.UpdateReport`), `notify`, `bye` |

Im Tunnel schicken beide zuerst `hello`; das `hello` des Controllers nennt in `node` den Knoten,
dessen Noise-Schlüssel den Tunnel aufgebaut hat (leer, solange der Controller nicht in der
Vertrauensliste steht). Nennt er einen anderen, beendet die API den Tunnel. Nach `bye` schließt der
Absender die Leitung. `trust` ist nur ein Hinweis: Die Fassungen holt die Shell mit
`GET /api/v1/trust?from=N` (`session.TrustChain`, seitenweise mit `more`); ein Controller bekommt
sie über den Kanal `ngc.users/controller/trust`.

### Öffnen, Schließen, Freigabe

- **Öffnen:** nur der Browser (Sitzung) bzw. die API (Tunnel), mit einer freien Nummer ab 1.
  Ablauf: OPEN (sein `window` ist die erste Freigabe des Öffners) → erste GRANT der Gegenseite
  (angenommen; Freigabe mindestens `wire.MinWindow`, 4 KiB) oder CLOSE mit Fehler → DATA in beide
  Richtungen → EOF von jeder Seite, die nichts mehr schickt → CLOSE von der Seite, die den Kanal
  angenommen hat. Jede Seite darf jederzeit mit einem Fehler schließen; nach ihrem CLOSE schickt sie
  nichts mehr auf dem Kanal.
- **Schließen:** Jedes CLOSE wird mit CLOSE beantwortet, außer man hat selbst schon eines geschickt.
  Eine Nummer ist erst wieder frei, wenn ein CLOSE für sie gesendet und eines empfangen ist. Was
  nach dem eigenen CLOSE noch für den Kanal ankommt, wird ohne Antwort verworfen; ein CLOSE der
  Gegenseite darunter zählt trotzdem als empfangen. Protokollfehler sind: ein Frame für eine
  Nummer, die weder offen ist noch schließt, ein OPEN für eine offene Nummer und ein OPEN von der
  Seite, die keine Kanäle öffnet. Die Antwort ist CLOSE `E_PROTOCOL` für diese Nummer.
- **Grenzen:** Eine Nummer zählt, bis sie wieder frei ist, auch beim Schließen: höchstens
  `wire.MaxSessionChannels` (64) je Sitzung und `wire.MaxTunnelChannels` (1024) je Tunnel. Ein
  Frame, der darüber führen würde (ein OPEN oder ein Frame, dessen CLOSE `E_PROTOCOL` eine Nummer zu
  viel ergäbe), beendet die Leitung. Der Öffner zählt deshalb selbst mit und öffnet nie über die
  Grenze. Ist der Tunnel zum Ziel-Controller voll, antwortet die API dem Browser mit CLOSE
  `E_LIMIT`, statt weiterzuleiten.
- **Freigabe:** je Strecke und Richtung, gezählt in Bytes der DATA-Nutzlast (auf
  Ende-zu-Ende-Kanälen samt 16 Byte Noise-Prüfwert). `window` im OPEN (`wire.MinWindow` bis
  `wire.MaxWindow`) und jede GRANT kommen hinzu; offen sein darf höchstens `wire.MaxWindow` (1 MiB).
  Mehr DATA als freigegeben oder eine GRANT, die darüber führte, schließen den Kanal mit CLOSE
  `E_PROTOCOL`. EOF, CLOSE, GRANT und Kanal 0 zählen nicht.
- **API als Vermittler:** Sie legt das OPEN des Browsers unverändert in das Feld `open` von
  `tunnel.Open`, dazu ihre eigene Freigabe (`window`) und das Client-Zertifikat mit dem Beweis des
  Benutzers (`client`, `proof`). DATA und EOF gibt sie unverändert und eins zu eins weiter, CLOSE
  mit demselben Code. Die Freigabe führt sie je Strecke selbst und gibt dem Controller nie mehr
  frei, als sie halten kann, bis der Browser es abnimmt. Ihre erste GRANT an den Browser schickt
  sie erst nach der ersten GRANT des Controllers (oder sie gibt dessen CLOSE weiter); auf
  Ende-zu-Ende-Kanälen kopiert sie Noise-Nachricht 2 aus dessen GRANT hinein. Ausnahme:
  `transfer` mit Aufgabe (unten, „Kern-Apps und Aufgaben“).
- **Beim Controller:** `tunnel.VerifyOpen` prüft das OPEN samt Client-Zertifikat
  (`session.VerifyClient`). Der Benutzer eines Kanals ist nur `Opened.Session.Cert().User`; das OPEN
  selbst binden nur der Prolog (Ende zu Ende) und die Unterschrift der Aufgabe. Eine Rolle mit
  `passwordToo` setzt die API bei der Anmeldung durch; an Controller geht kein Passwortbeweis, sie
  verlassen sich auf den Passkey-Beweis, den `session.VerifyClient` für solche Benutzer verlangt.
  Das Client-Zertifikat beweist den Benutzer nur zusammen mit dem Ende-zu-Ende-Handschlag
  (Schlüssel `dh`) oder einer Aufgabe (Schlüssel `sig`): Die API hält Zertifikat und Beweis jeder
  Sitzung und kann jedes anhängen. Auf anderen Kanälen (etwa `stage`) berechtigt und protokolliert
  der Controller nicht nach ihm.

### Ende zu Ende

Alle Apps außer den Kern-Apps (`wire.CoreApp`) sprechen Ende zu Ende: Noise_KK zwischen Shell und
Controller, mit dem Schlüssel `dh` des Client-Zertifikats und dem Noise-Schlüssel des Controllers
aus der Vertrauensliste. Die Shell findet den Controller in `core.DesktopApp` (Feld `node`).

- **Prolog** (`noisep.E2EPrologue`):
  `ngc/e2e/v1` 0x00 Controller-ID 0x00 App 0x00 Dienst 0x00 Aktion 0x00 SHA-256(params)
  SHA-256(task), beide Hashes als 32 rohe Byte. params und task sind die Werte der Felder `params`
  und `task` genau so, wie sie im OPEN stehen, jeweils von `{` bis `}`; fehlt ein Feld, wird der
  leere Text gehasht. Der Controller nimmt sie aus `Opened.Open.Params` und
  `Opened.Open.TaskBytes()`. Ein OPEN mit Aufgabe hat keine `params`; dann binden die Bytes der
  Aufgabe deren Parameter (`service.Call`).
- **Handschlag:** Nachricht 1 steht im OPEN (`e2e`), Nachricht 2 in der ersten GRANT der Seite, die
  den Kanal angenommen hat (diese GRANT hat dann 52 Byte). Fehlt der Handschlag dort oder steht
  einer in einer anderen GRANT, ist das `E_PROTOCOL`.
- **Innere Nachrichten:** Jede DATA-Nutzlast ist eine Noise-Transportnachricht; ihr Klartext beginnt
  mit einem Byte (`wire.E2E`, `wire.ParseE2E`): 1 = Daten (`wire.E2EData`, höchstens
  `wire.MaxE2EPlaintext` Byte samt diesem), 2 = Ende (`wire.E2EEnd`; von der Shell leer, vom
  Controller mit dem Ergebnis als CLOSE-JSON).
- **Ablauf:** Sofort nach Nachricht 2 schickt die Shell ihre erste innere Nachricht: Daten, leer,
  wenn noch keine Eingabe da ist, oder Eingabe und Ende. Der Controller startet den Dienst erst,
  wenn diese Nachricht entschlüsselt ist, und wartet darauf höchstens `wire.E2EFirstTimeout` (30)
  Sekunden. Ende von der Shell heißt „keine Eingabe mehr“; die Ausgabe des Controllers läuft danach
  weiter. Nach ihrem eigenen Ende schickt eine Seite keine innere Nachricht mehr (sonst
  `E_PROTOCOL`).
- **Abgeschnitten:** Kommt EOF oder CLOSE des Kanals vor dem Ende der Gegenseite, sind die Daten
  abgeschnitten: Fehler. EOF und CLOSE beenden nur den Transport.

### Kern-Apps und Aufgaben

- **Kern-Apps** (`ngc.desktop`, `ngc.users`, `ngc.store`) haben keinen eigenen
  Ende-zu-Ende-Austausch; ihre DATA laufen ohne zweite Verschlüsselung in der Noise-Sitzung der
  Leitung. Wie Daten fließen, sagt `core.Action` (`Flow`, `MaxIn`):
  - `call` (`core.FlowCall`): Parameter im OPEN oder in der Aufgabe; der Öffner schickt sofort EOF.
    Das Ergebnis sind alle DATA bis zum EOF der Gegenseite. Aktionen ohne Ergebnis antworten `{}`.
    Beispiele: `core.json`.
  - `in` (`core.FlowIn`): Der Öffner schickt höchstens `MaxIn` Byte, dann EOF; Ergebnis wie bei
    `call`.
  - `out` (`core.FlowOut`): Die API schickt Daten statt eines Ergebnisses; bei `ui` ein ui-tar in
    der einen Form, die der Kommentar in `store/ui.go` festlegt: `store.UIWriter` schreibt sie,
    `store.WalkUI` liest sie, alles andere wird abgelehnt.
  - `transfer` (`core.FlowTransfer`): API → Controller, ein Kopf `core.Transfer`
    (`core.WriteTransfer`, `core.ReadTransfer`), danach genau `size` Byte; Ergebnis wie bei `call`.
- **Wer öffnet:** Die Shell öffnet alle Aktionen des Dienstes `api` und die Root-Aufgaben des
  Dienstes `controller` (`activate`, `deactivate`, `uninstall`, `delete-data`, `install`), jeweils
  mit Aufgabe (`core.ShellOpens`); `stage` und `ngc.users/controller/trust` öffnet nur die API,
  ohne Benutzer, und lehnt ein OPEN der Shell dafür mit `E_FORBIDDEN` ab.
- **`transfer` mit Aufgabe** (`activate`): Die Shell schickt nach der ersten GRANT sofort EOF und
  keine DATA (sonst `E_PROTOCOL`). Die API schreibt auf der Tunnel-Strecke selbst den Kopf
  `core.Transfer` und die Datei, dann EOF; dort gilt `MaxIn`. Ergebnis und CLOSE des Controllers
  gehen wie sonst zurück.
- **App-Kanäle:** Das Ergebnis ist die Ausgabe des Controllers bis zu seinem Ende (`wire.E2EEnd`).
- **Ergebnis nur mit Erfolg:** Ein Ergebnis gilt nur, wenn der Kanal mit CLOSE `ok` endet, auf
  Ende-zu-Ende-Kanälen mit einem Ende des Controllers mit `ok`. Sonst verwirft der Empfänger, was
  kam.
- **Grenze für Ergebnisse:** höchstens `wire.MaxCallResult` (8 MiB) für jedes Ergebnis nach Art von
  `call`, bei Kern-Apps wie bei Apps:
  - gemeint sind `call`, `in` und `transfer` der Kern-Aktionen und jede App-Aktion, die kein
    Datenstrom ist (ohne `stream` im Manifest);
  - wer mehr zu schicken hätte, beendet mit `E_TOOLARGE` (auf Ende-zu-Ende-Kanälen im Ende des
    Controllers);
  - durchsetzen muss es der Empfänger, der bei mehr den Kanal mit `E_TOOLARGE` schließt: die API
    für das Ergebnis eines Controllers auf einem Kern-App-Kanal, bevor sie es weitergibt; die Shell
    für die Ergebnisse der API und auf jedem Ende-zu-Ende-Kanal, dessen Klartext die API nicht
    sieht.
- **Grenze für Eingaben:** Was der Öffner bei `in` und was die API bei `transfer` schickt, begrenzt
  `MaxIn` (`core.Action`); der Empfänger schließt bei mehr ebenso mit `E_TOOLARGE`:
  - die API bei `ngc.store/api/upload` (bis `store.MaxPackage`, 4 GiB), `ngc.users/api/trust` und
    `ngc.store/api/license`;
  - der Controller bei `ngc.store/controller/activate` und `ngc.store/controller/stage`
    (`transfer`) und bei `ngc.users/controller/trust`.
- **Aufgaben:** Ein OPEN mit Aufgabe (`task`) hat keine eigenen Parameter; es gelten die der
  Aufgabe. Ob ein Kanal eine Aufgabe braucht, root ist oder kritisch, entscheidet `task.Check` mit
  einer Regel aus geprüften Daten, nie aus dem OPEN: `core.RuleFor` für Kern-Aktionen (ohne
  Parameter ist `activate` oder `uninstall` ein Fehler, denn ihre Parameter entscheiden),
  `store.RuleFor` aus dem Manifest einer App (root, wenn das Manifest `root` verlangt). Die API
  prüft eine Aufgabe, bevor sie sie an einen Controller weiterleitet, mit dem Ziel-Controller als
  `task.Input.Node`. Wer prüft, merkt sich die ID der Aufgabe bis zu ihrem Ablauf
  (`dbapi.OnceTask`).
- **Vertrauensliste an Controller:** Kanal `ngc.users/controller/trust` (`in`). Die API schickt eine
  `session.TrustChain`; der Controller prüft sie (`session.ParseTrustChain`, `trust.Advance`) und
  antwortet mit `core.TrustResult` (seine neueste geprüfte Fassung) oder schließt mit `E_VERIFY`.
- **Ohne Benutzer** (`tunnel.Open` ohne `client` und `proof`, mit einem OPEN der API) öffnet die
  API nur, was `core.WithoutUser` nennt: `ngc.users/controller/trust`,
  `ngc.store/controller/stage` und `ngc.store/controller/install`. `install` ohne Benutzer führt
  der Controller nur aus, wenn `core.Unattended` zustimmt: Es gibt eine Dauerfreigabe für das Ziel
  und den Schlüssel, der die Datei signiert hat, und die Version ist neuer als die installierte; ist
  nichts installiert, nie. Bei `system` gibt es keinen Versionsvergleich. Es zählen nur die
  signierten Dateien: Version und SHA-256 nimmt der Controller aus dem geprüften Release oder
  Katalogeintrag der Datei (dessen `seq` er geprüft hat) und lehnt Parameter ab, die davon
  abweichen, denn die Parameter eines Kanals ohne Benutzer sind nicht unterschrieben.
- **Oberflächen:** Eine App-Oberfläche führt die Shell erst aus, wenn ihr ui-tar zu einer
  signierten Prüfsumme passt. Die Prüfsumme ist SHA-256 des kanonischen ui-tar (`store.PackageUI`:
  die Dateien unter `ui/` in der Reihenfolge des Pakets, mit `store.UIWriter` geschrieben, ohne
  Ordner). Sie steht im Katalog je Version (`ui`, vom Store signiert) und in `activate` (`ui`, vom
  Admin signiert); der Controller vergleicht sie beim Aktivieren und Installieren. Den Beweis holt
  die Shell mit `ngc.desktop/ui-proof` (`core.UIProof`) und prüft ihn wie `core.VerifyUIProof`;
  dann lädt sie `ngc.desktop/ui` ganz, vergleicht SHA-256 und liest es erst danach nach den Regeln
  von `store.WalkUI`. Geprüfte Prüfsummen merkt sie sich je App und Version, Kataloge je Store und
  `seq`. Nach jeder neuen Fassung der Vertrauensliste prüft sie die Beweise neu, vor allem die
  hochgeladener Apps: Ein Store oder der Schlüssel eines Admins kann darin fehlen.
  - Für eine Store-App bürgt jeder Store der Vertrauensliste, dessen geprüfter Katalog App, Version
    und Prüfsumme führt. Grenze: Eine übernommene API kann zusammen mit einem feindlichen Store, den
    der Admin eingetragen hat, einer App eines anderen Stores eine fremde Oberfläche unterschieben.
    Stores trägt man deshalb nur ein, wenn man ihnen so weit traut wie der Software von dort.
  - Welche signierte Version einer Oberfläche die Shell ausführt, entscheidet die API: etwa eine
    ältere, die ein Store oder Admin einmal signiert hat, oder die Version, zu der ein Controller
    nach einem gescheiterten Update zurückgekehrt ist. Eine Oberfläche, die niemand für diese App
    signiert hat, kann sie der Shell nicht unterschieben.
  - Der Beweis einer hochgeladenen App gilt ohne Zeitprüfung, aber nur, solange Schlüssel und
    Root-Recht des Admins, der sie aktiviert hat, in der Vertrauensliste stehen. Nach einem Wechsel
    dieses Schlüssels muss die App neu aktiviert werden.
- **Updates per Klick, Dauerfreigaben:** `ngc.store/updates` hat eine Zeile je Ziel und Controller
  (`core.UpdateInfo`). Ein Update per Klick ist je Controller eine Root-Aufgabe `install` mit
  `target`, `version` = `available` und `sha256` der Zeile (gesetzt, sobald `state` `downloaded`
  ist; bei `system` nur `target`). Eine Dauerfreigabe für einen NGC-Teil nennt den Release-Schlüssel
  (bei der Ersteinrichtung aus `session.SetupInfo`, später `key` der Zeile), für eine App den
  Schlüssel ihres Stores aus der Vertrauensliste. Controller prüfen Releases nur mit ihrem eigenen
  Release-Schlüssel. Gespeichert ist je Ziel nur die Einstellung (Modus und Zeitfenster,
  `core.UpdateSetting` unter `dbapi.SettingUpdate`); die Zeilen rechnet die API aus.

## Prüfen

```bash
go test ./shared/...
tools/check.sh
```
