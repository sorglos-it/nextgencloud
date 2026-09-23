# API

Deine Ordner: `apps/server-api/` und `apps/cli/` (Werkzeug `ngc-store`, Anweisungen dort), dazu
`scripts/ngc-api.sh` im ulh-Repo (`shared/ulh.md`). Verträge: V1, V2, V4, V5, V6, V7.

Aufbau: `main.go` ist der Einstiegspunkt von `ngc-api`, die Logik liegt in `classes/` (Regeln:
`CLAUDE.md` im Stamm, Abschnitt „Aufbau“).

## Aufgabe

Der Vermittler und die Firewall zwischen Oberfläche und Servern. Führt nie App-Code aus.

- Anmeldung nach `session`: Anfragen nur über `session.ParseLoginStart`, `session.ParseLoginFinish`
  und `session.ParseSetup`; Passwortschlüssel oder Passkey prüfen (`session.VerifyClient`). Die
  Antwort auf `login/start` baust du nie selbst, sondern mit `session.Challenge`
  (`session.ChallengeInput`: `Key` = `config.API.ChallengeKey`, `TOTP` aus `dbapi.User.TOTP`,
  `TOTPShare` = Anteil der Benutzer mit TOTP in Prozent, den du zählst und merken darfst): Sie
  erfindet für unbekannte Namen eine Antwort, die von einer echten nicht zu unterscheiden ist, und
  tut dafür auf beiden Wegen dieselbe Arbeit. Alles davor und danach muss das auch: gleich viele
  Datenbankabfragen, dieselbe Sperre nach Fehlversuchen und dieselbe Antwortzeit, sonst verrät die
  Laufzeit, was die Antwort verschweigt. Die
  Nonce aus `session.LoginChallenge` prüft `session.VerifyClient` nicht, das tust du: beim
  Ausgeben mit `dbapi.OnceNonce` und dem Benutzer merken (höchstens `session.ChallengeLifetime`),
  bei der Anmeldung genau einmal verbrauchen (`dbapi.Local.OnceUse`) und mit dem Benutzer des
  Zertifikats vergleichen. Hat eine Rolle des Benutzers `passwordToo` (`trust.List.PasswordToo`,
  dann `session.LoginChallenge.PasswordToo`), kommt der Beweis von einem Passkey und dazu der
  Passwortbeweis (`password`), den du mit `session.VerifyPassword` prüfst; fehlt `password`, lehnst
  du die Anmeldung ab (`session.ParseLoginFinish` lässt das Feld offen). Das setzt nur die API
  durch; an Controller geht kein Passwortbeweis. TOTP, Sperre nach Fehlversuchen, Sitzung und
  Zertifikate, Cookie mit `session.TokenHash`.
- HTTP bis zum WebSocket genau nach `shared/README.md`, Abschnitt „Vor dem WebSocket“: Status mit
  `session.HTTPStatus`, Inhalt `wire.Error`, jede Antwort mit `Cache-Control: no-store`, Anfragen
  nur mit `Content-Type: application/json`; das Cookie mit `session.CookieAttributes`; `from` nur
  über `session.ParseFrom`. Jede fehlgeschlagene Anmeldung ist gleich `E_LOGIN`, nach zu vielen
  Fehlversuchen kommt `E_LOCKED` mit `Retry-After`. Die Sperre führst du je Anmeldename, auch für
  Namen, die es nicht gibt, damit auch `E_LOCKED` nie verrät, ob ein Benutzer existiert.
  `GET /api/v1/trust` antwortet nur mit Sitzung, und mit 204, wenn nichts Neueres da ist.
- Ersteinrichtung: Den Einmal-Code (`session.ValidSetupCode`) kann nur root lesen (ulh zeigt ihn),
  und er kommt nie ins Log. `GET /api/v1/setup` antwortet mit `session.SetupOffer`: dein
  `session.SetupInfo` als JSON und `session.OfferMAC` über genau diese Bytes. Darin dein Knoten
  (`id` und `name` = `node`, `url` aus `api.json`, Noise- und Signaturschlüssel, Fingerabdruck
  deines TLS-Zertifikats), der globale Store (`id` `global`, `name` „NGC Store“, `url` und `key`
  aus `storeUrl` und `storeKey` in `api.json`) und `releaseKey` aus `api.json`. Eine Anfrage
  prüfst du mit `session.VerifySetup(s, info, code)`; Fehlversuche zählst du (`E_LOCKED`). Sobald
  es einen Admin gibt, antworten beide Methoden `E_NOTFOUND`.
- Adressen nach der Grammatik (`shared/README.md`, Regel „Adressen“): `url` in `api.json` ist
  eine Tunnel-Adresse (`trust.ValidTunnelURL`), `storeUrl` eine Store-Adresse
  (`trust.ValidStoreURL`), und dein Knoten im Angebot der Ersteinrichtung trägt genau dieses
  `url`. Das ulh-Skript prüft dafür `hostname` mit `host.sh` (`shared/ulh.md`).
- Eigenes TLS: Dein Zertifikat erzeugst du selbst, selbst signiert für den Host von `url` und deinen
  Schlüssel, 100 Jahre gültig, damit kein Datum Apache aussperrt (`SSLProxyCheckPeerExpire on`).
  Vertrauen trägt nur der Fingerabdruck des Schlüssels (`trust.TLSPin`, SHA-256 des SPKI), nicht
  das Zertifikat: Er steht in deinem Knoten der Vertrauensliste, in deinen Kopplungs- und
  Beitrittscodes und in `web.json`. Erneuern darfst du das Zertifikat nur mit demselben Schlüssel.
  Einen neuen Schlüssel gibt es nur zusammen mit einem neuen Fingerabdruck in der Vertrauensliste:
  erst die neue Fassung, dann der neue Schlüssel.
- WebSocket nach `wire` und `noisep` (Ablauf: `shared/README.md`, Abschnitt „Kanäle“):
  Kanalnummern höchstens `wire.MaxSessionChannels` je Sitzung und `wire.MaxTunnelChannels` je
  Tunnel, gezählt, bis sie wieder frei sind (CLOSE gesendet und empfangen); ein Frame darüber
  beendet die Leitung. Ist der Tunnel zum Ziel-Controller voll, antwortest du dem Browser mit CLOSE
  `E_LIMIT`. Freigabe je Strecke bis `wire.MaxWindow`, Steuernachrichten nach
  `wire.ControlAllowed`. Nennt `hello` eines Controllers in `node` einen anderen Knoten als den
  seines Noise-Schlüssels, beendest du den Tunnel.
- Kanäle weiterleiten: das OPEN des Browsers Byte für Byte in `tunnel.Open` (Feld `open`), mit
  eigener Freigabe; DATA und EOF unverändert und eins zu eins, CLOSE mit demselben Code. Deine
  erste GRANT an den Browser schickst du erst nach der ersten GRANT des Controllers (auf
  Ende-zu-Ende-Kanälen mit Noise-Nachricht 2 daraus), oder du gibst dessen CLOSE weiter.
  Ende-zu-Ende-Daten nie auspacken. Ein OPEN der Shell, bei dem `core.ShellOpens` nein sagt
  (`stage`, `ngc.users/controller/trust`), lehnst du mit `E_FORBIDDEN` ab. Auf einem
  weitergeleiteten `transfer`-Kanal (`activate`) lehnst du DATA der Shell ab (`E_PROTOCOL`) und
  schreibst den Kopf `core.Transfer` und die Datei selbst, dann EOF.
- Ergebnisse nach Art von `call` auf Kern-App-Kanälen höchstens `wire.MaxCallResult`: Das
  Ergebnis eines Controllers prüfst du, bevor du es weitergibst, und schließt bei mehr mit
  `E_TOOLARGE`; deine eigenen Ergebnisse hältst du darunter (als Empfängerin prüft sie die Shell).
  Auf Ende-zu-Ende-Kanälen prüft das die Shell, du siehst dort keinen Klartext. Ein Ergebnis gilt
  nur, wenn der Kanal mit CLOSE `ok` endet.
- Ergebnisse kodierst du mit `core.EncodeResult`, nie mit `json.Marshal`: Das schreibt eine
  nil-Slice als `null`, und das lehnt die Shell ab (`shared/README.md`, „Striktes JSON“, Regel
  14). `core.EncodeResult` schreibt leere Listen als `[]` und leere Maps als `{}`, lässt optionale
  Felder mit Nullwert weg und gibt `core.ErrResult` für alles, was die Shell ablehnen würde (ein
  übrig gebliebenes `null`, ungültiges UTF-8, mehr als `wire.MaxCallResult` Byte). Das betrifft vor
  allem die zehn Ergebnisse, die als Ganzes eine Liste sind (`ngc.desktop/apps`, `ngc.users/list`,
  `ngc.users/rights`, `ngc.users/pending`, `ngc.users/pending-api`, `ngc.users/nodes`,
  `ngc.users/log`, `ngc.store/sources`, `ngc.store/installed`, `ngc.store/updates`); leer stehen
  sie unter `empty` in `core.json`.
- Was der Öffner schicken darf (`core.Action.MaxIn`), setzt du bei `ngc.store/api/upload`,
  `ngc.users/api/trust` und `ngc.store/api/license` durch: bei mehr schließt du mit `E_TOOLARGE`.
  Umschläge von außen liest du nur mit `sign.ParseEnvelope` (Lizenzen mit `store.MaxLicense`,
  Kataloge und Releases mit `sign.MaxPayload`), eine Fassung der Vertrauensliste mit
  `trust.ParseSigned`.
- Vertragsfassungen (`shared/README.md`, Regeln): Du bedienst die aktuelle und die vorige Nummer
  nebeneinander, jede unter ihren eigenen Pfaden (heute `/api/v1/…`). Im Tunnel spricht die neuere
  Seite die Fassung der älteren, die ihr `hello` nennt (`tunnel.Hello`); nach der Reihenfolge der
  Updates bist das meist du.
- Rechte je Kanal: Benutzer, Rolle, App, Aktion, Lizenz. Kern-Aktionen nach `core`; Aufgaben mit
  `task.Check` und `core.RuleFor` bzw. `store.RuleFor`, nie mit Parametern aus dem OPEN. Eine
  Aufgabe, die du an einen Controller weiterleitest, prüfst du vorher mit dem Ziel-Controller als
  `task.Input.Node`; Aufgaben-IDs merkst du dir (`dbapi.OnceTask`).
- Tunnel zu jedem Controller (`tunnel`), Kopplung, Kopplungs- und Beitrittscodes (`tunnel.PairCode`,
  `tunnel.JoinCode`, beide mit deinem Fingerabdruck in `tls`; Kern-Aktionen `pair-code` und
  `join-code`). Wartende Controller zeigt `ngc.users/pending` (`core.Pending`, ohne Fingerabdruck:
  den rechnet die Shell). Neue Fassungen der Vertrauensliste an Controller über den Kanal
  `ngc.users/controller/trust`, an Browser per `GET /api/v1/trust` (`session.TrustChain`,
  seitenweise) und Hinweis `trust`. Ohne Benutzer öffnest du nur, was `core.WithoutUser` nennt.
- `ngc.users/nodes` (`core.NodeInfo`): eine Zeile je Controller deiner Vertrauensliste, damit der
  Admin das Ziel einer Aktivierung wählt. `online` heißt: Er hat gerade einen Tunnel zu dir.
  Rollen, Version und Architektur kommen aus seinem letzten `hello` an dich (`tunnel.Hello`) und
  fehlen, wenn du nie eines hattest. Nichts davon ist signiert.
- Beitritt weiterer API-Server: Die `tunnel.JoinRequest` kommt an `tunnel.PathJoin`
  (`tunnel.ParseJoinRequest`, MAC mit `tunnel.JoinRequest.Verify` und dem Geheimnis des Codes, das
  genau einmal gilt: `dbapi.OnceJoin`). Du legst sie als `dbapi.BlobJoin` ab und zeigst sie mit
  `ngc.users/pending-api` (`core.PendingAPI`); den Fingerabdruck rechnet die Shell. Trittst du
  selbst bei, schickst du deine `tunnel.JoinRequest` per POST an
  `https://<Host und Port aus url des Codes>` + `tunnel.PathJoin`, TLS mit `tunnel.PinnedTLS` und
  dem Fingerabdruck des Codes (`tunnel.JoinCode.TLS`), und holst später die gemeinsamen Schlüssel
  ebenso gepinnt. Dein ulh-Skript zeigt dabei nicht nur `tunnel.JoinFingerprint`, sondern auch
  den Anker aus dem Beitrittscode (Nummer und `tunnel.Fingerprint` des Hashes), wie
  `ngc-controller` es beim Koppeln tut; der Admin vergleicht ihn mit der Shell (`shared/ulh.md`).
  Mehrere APIs teilen sich Schlüssel und Sitzungen über die Datenbank; jede kann jede ersetzen.
  Wie ein neuer API-Server nach der Unterschrift des Admins die gemeinsamen Schlüssel bekommt,
  legst du selbst fest.
- Apps und Stores: Kataloge prüfen (`store.VerifyCatalog`), Pakete entpacken (`store.Walk`,
  höchstens `store.MaxDirs` Verzeichnisse), Lizenzen prüfen, Schlüssel der Kauf-Apps entpacken
  (`store.UnwrapKey`) und nur an eigene Controller geben. Die neueste Lizenz jeder Kauf-App, die
  du mit `store.VerifyLicense` geprüft hast, schickst du über `ngc.store/controller/license`
  (ohne Benutzer, Parameter `{app}`) an jeden Controller, auf dem die App läuft: nach jeder neuen
  Lizenz und nach jedem Tunnelaufbau. Er prüft sie selbst mit `store.VerifyLicenseFor`; nur du
  packst den Inhaltsschlüssel aus und zählst die Benutzer einer Lizenz je Benutzer. Der globale
  Store heißt `global` (`store.GlobalID`), `local` steht für hochgeladene Apps
  (`core.LocalStore`); einen zweiten Store `global` lehnst du ab. Auf `ngc.store/upload`
  antwortest du mit `core.UploadResult` (ID, Version, SHA-256 und `store.PackageUI` des Pakets);
  die Shell gleicht damit nur ab.
- Oberflächen: `dbapi.BlobUI` hält genau `store.PackageUI` des unverschlüsselten Pakets, und
  `ngc.desktop/ui` liefert diese Bytes unverändert (die Shell liest sie wie `store.WalkUI`). Für
  jede installierte Version hältst du ihren `core.UIProof` in `dbapi.BlobApproval`: bei Store-Apps
  den Katalog ihres Stores, der die Version führt, bei hochgeladenen Apps die Aufgabe `activate`,
  die sie installiert hat, mit dem Client-Zertifikat und dem Beweis ihrer Sitzung. Ihn liefert
  `ngc.desktop/ui-proof`, höchstens `wire.MaxCallResult` Byte; so verliert ein gekürzter Katalog
  den Beweis nicht. `core.DesktopApp.Entry` nimmst du aus `ui.entry` des Manifests der
  installierten Version; der Wert ist nicht signiert, deshalb führt die Shell nur eine Seite aus,
  die auch im geprüften ui-tar liegt.
- Updates: Reihenfolge Datenbank → APIs → Controller → Webinterface, gesteuert von genau einer API
  über `dbapi.LockUpdate`. Die Migrationen spielt genau ein Controller mit Rolle `db` ein: Die API
  mit der Sperre wählt unter den verbundenen Controllern mit Rolle `db` den mit der kleinsten
  Node-ID; scheitert er, den nächsten. Er bekommt dafür die Datei `ngc-api` seiner Architektur.
- `ngc.store/updates` rechnest du je Ziel und Controller aus Releases, Katalogen, den Meldungen der
  Controller und der Einstellung des Ziels (`dbapi.SettingUpdate`: `core.UpdateSetting` mit Modus
  und Zeitfenster); die Zeilen speicherst du nicht. `sha256` ist die Prüfsumme der Datei, die nach
  `stage` auf dem Controller liegt, `key` bei NGC-Teilen dein Release-Schlüssel
  (`config.API.ReleaseKey`). Eine Version, deren `minNgc` neuer ist als die NGC-Version des
  Controllers (`tunnel.Hello.Version`), bietest du nicht an; verglichen wird mit
  `store.SupportsMinNGC`. Versionen einer App kommen nur aus dem Katalog ihres eigenen Stores
  (`dbapi.App.Store`). Verlangt eine neue Version mehr als die `needs`, die beim Aktivieren
  freigegeben wurden (`store.Granted` gegen `dbapi.App.Grants`), steht die Zeile auf
  `needsgrants` (`store.UpdateNeedsGrants`): Diese Version installierst du nie, weder per Klick
  noch automatisch; der Admin aktiviert sie mit den neuen Rechten.
- Einmalwerte (`dbapi.OnceNonce`, `dbapi.OncePair`, `dbapi.OnceJoin`, `dbapi.OnceTask`) räumst du
  mit `OncePurge(now - session.MaxSkew)` weg (`dbapi.Local.OncePurge`): Sonst könnte eine API mit
  nachgehender Uhr eine Aufgabe noch einmal annehmen, deren ID eine API mit vorgehender Uhr schon
  weggeräumt hat.
- Protokoll: Anmeldungen, Rechte, Aufgaben mit Unterschrift, Kanäle, Stores, Lizenzen, Updates;
  jede Zeile mit einer ID aus 16 Zufallsbytes als Hex (`core.LogEntry`, `core.ValidLogID`).
- Werkzeug `ngc-store` in `apps/cli/`: Aufgabe in `apps/cli/CLAUDE.md`.
- Konfiguration nur über `config.LoadAPI`. Schlüssel und Passwörter darin sind `config.Secret` bzw.
  `config.SecretText`; den Wert geben nur `Bytes()` bzw. `Text()`. Ins Log nur über die
  `LogValue`-Methoden, nie Schlüssel oder Passwörter. MariaDB nur über `config.DB.TLSConfig`, mit
  dem Host des jeweiligen Knotens aus `db.nodes` als Servername.

## Tests

- Gegen `dbapi/memstore`, ohne Datenbank.
- Gegen die Vektoren in `shared/testdata/`.
- Fuzzing für alles, was von außen kommt.
- Angriffe: eingeschleustes SQL, gefälschte Unterschriften, wieder eingespielte Nachrichten,
  fremde Lizenz, geänderte Parameter, abgeschnittene Daten, Kanäle über der Grenze, ein Mann in der
  Mitte bei der Ersteinrichtung (Anfrage mit einem anderen Code oder über andere Bytes).

## Fertig, wenn

Ein Browser kann sich anmelden, der Terminal-Kanal läuft bis zum Controller, Rechte und Lizenzen
greifen, ein Ausfall einer API unterbricht keine Sitzung, und `ngc-store` erzeugt einen Test-Store,
aus dem sich eine Kauf-App freischalten lässt.
