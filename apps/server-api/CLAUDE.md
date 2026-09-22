# API

Deine Ordner: `apps/server-api/` und `apps/cli/` (Werkzeug `ngc-store`, Anweisungen dort), dazu
`scripts/ngc-api.sh` im ulh-Repo (`shared/ulh.md`). Verträge: V1, V2, V4, V5, V6, V7.

Aufbau: `main.go` ist der Einstiegspunkt von `ngc-api`, die Logik liegt in `classes/` (Regeln:
`CLAUDE.md` im Stamm, Abschnitt „Aufbau“).

## Aufgabe

Der Vermittler und die Firewall zwischen Oberfläche und Servern. Führt nie App-Code aus.

- Anmeldung nach `session`: Anfragen nur über `session.ParseLoginStart`, `session.ParseLoginFinish`
  und `session.ParseSetup`; Passwortschlüssel oder Passkey prüfen (`session.VerifyClient`). Die
  Nonce aus `session.LoginChallenge` prüft `session.VerifyClient` nicht, das tust du: beim
  Ausgeben mit `dbapi.OnceNonce` und dem Benutzer merken (höchstens `session.ChallengeLifetime`),
  bei der Anmeldung genau einmal verbrauchen (`dbapi.Local.OnceUse`) und mit dem Benutzer des
  Zertifikats vergleichen. Hat eine Rolle des Benutzers `passwordToo` (`trust.List.PasswordToo`,
  dann `session.LoginChallenge.PasswordToo`), kommt der Beweis von einem Passkey und dazu der
  Passwortbeweis (`password`), den du mit `session.VerifyPassword` prüfst; fehlt `password`, lehnst
  du die Anmeldung ab (`session.ParseLoginFinish` lässt das Feld offen). Das setzt nur die API
  durch; an Controller geht kein Passwortbeweis. TOTP, Sperre nach Fehlversuchen, Sitzung und
  Zertifikate, Cookie mit `session.TokenHash`. Ersteinrichtung: `session.SetupInfo` mit dem
  globalen Store aus `api.json` (`storeUrl`, `storeKey`), Fassung 1 mit `session.VerifySetup`
  prüfen, den Einmal-Code in konstanter Zeit vergleichen.
- WebSocket nach `wire` und `noisep` (Ablauf: `shared/README.md`, Abschnitt „Kanäle“):
  Kanalnummern höchstens `wire.MaxSessionChannels` je Sitzung und `wire.MaxTunnelChannels` je
  Tunnel, gezählt, bis sie wieder frei sind (CLOSE gesendet und empfangen); ein Frame darüber
  beendet die Leitung. Ist der Tunnel zum Ziel-Controller voll, antwortest du dem Browser mit CLOSE
  `E_LIMIT`. Freigabe je Strecke bis `wire.MaxWindow`, Steuernachrichten nach
  `wire.ControlAllowed`.
- Kanäle weiterleiten: das OPEN des Browsers Byte für Byte in `tunnel.Open` (Feld `open`), mit
  eigener Freigabe; DATA und EOF unverändert und eins zu eins, CLOSE mit demselben Code;
  Noise-Nachricht 2 aus der ersten GRANT des Controllers in die erste GRANT an den Browser
  kopieren. Ende-zu-Ende-Daten nie auspacken.
- Ergebnisse nach Art von `call` auf Kern-App-Kanälen höchstens `wire.MaxCallResult`: Das
  Ergebnis eines Controllers prüfst du, bevor du es weitergibst, und schließt bei mehr mit
  `E_TOOLARGE`; deine eigenen Ergebnisse hältst du darunter (als Empfängerin prüft sie die Shell).
  Auf Ende-zu-Ende-Kanälen prüft das die Shell, du siehst dort keinen Klartext.
- Ergebnisse kodierst du mit `core.EncodeResult`, nie mit `json.Marshal`: Das schreibt eine
  nil-Slice als `null`, und das lehnt die Shell ab (`shared/README.md`, „Striktes JSON“, Regel
  14). `core.EncodeResult` schreibt leere Listen als `[]` und leere Maps als `{}`, lässt optionale
  Felder mit Nullwert weg und gibt `core.ErrResult` für alles, was die Shell ablehnen würde (ein
  übrig gebliebenes `null`, ungültiges UTF-8, mehr als `wire.MaxCallResult` Byte). Das betrifft vor
  allem die acht Ergebnisse, die als Ganzes eine Liste sind (`ngc.desktop/apps`, `ngc.users/list`,
  `ngc.users/rights`, `ngc.users/pending`, `ngc.users/log`, `ngc.store/sources`,
  `ngc.store/installed`, `ngc.store/updates`); leer stehen sie unter `empty` in `core.json`.
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
  `tunnel.JoinCode`; Kern-Aktionen `pair-code` und `join-code`). Neue Fassungen der
  Vertrauensliste an Controller über den Kanal `ngc.users/controller/trust`, an Browser per
  `GET /api/v1/trust` (`session.TrustChain`, seitenweise) und Hinweis `trust`. Ohne Benutzer
  öffnest du nur, was `core.WithoutUser` nennt.
- Apps und Stores: Kataloge prüfen (`store.VerifyCatalog`), Pakete entpacken (`store.Walk`,
  höchstens `store.MaxDirs` Verzeichnisse), die Oberfläche einer App mit `store.NewUIWriter`
  (`store.UIWriter`) als ui-tar in der einen Form aus `store/ui.go` schreiben, in `dbapi.BlobUI`
  ablegen und bei `ngc.desktop/ui` unverändert ausliefern (die Shell liest es wie `store.WalkUI`),
  Lizenzen prüfen, Schlüssel der Kauf-Apps entpacken (`store.UnwrapKey`) und nur an eigene
  Controller geben. Der globale Store heißt `global` (`store.GlobalID`), `local` steht für
  hochgeladene Apps (`core.LocalStore`); einen zweiten Store `global` lehnst du ab.
- Updates: Reihenfolge Datenbank → APIs → Controller → Webinterface, gesteuert von genau einer API
  über `dbapi.LockUpdate`. Die Migrationen spielt genau ein Controller mit Rolle `db` ein: Die API
  mit der Sperre wählt unter den verbundenen Controllern mit Rolle `db` den mit der kleinsten
  Node-ID; scheitert er, den nächsten. Er bekommt dafür die Datei `ngc-api` seiner Architektur.
- Protokoll: Anmeldungen, Rechte, Aufgaben mit Unterschrift, Kanäle, Stores, Lizenzen, Updates;
  jede Zeile mit einer ID aus 16 Zufallsbytes als Hex (`core.LogEntry`, `core.ValidLogID`).
- Mehrere APIs teilen sich Schlüssel und Sitzungen über die Datenbank; jede kann jede ersetzen. Ein
  neuer API-Server kommt mit einem Beitrittscode (`tunnel.JoinCode`, `tunnel.JoinRequest`); wie er
  nach der Unterschrift des Admins die gemeinsamen Schlüssel bekommt, legst du selbst fest.
- Werkzeug `ngc-store` in `apps/cli/`: Aufgabe in `apps/cli/CLAUDE.md`.
- Konfiguration nur über `config.LoadAPI`. Schlüssel und Passwörter darin sind `config.Secret` bzw.
  `config.SecretText`; den Wert geben nur `Bytes()` bzw. `Text()`. Ins Log nur über die
  `LogValue`-Methoden, nie Schlüssel oder Passwörter.

## Tests

- Gegen `dbapi/memstore`, ohne Datenbank.
- Gegen die Vektoren in `shared/testdata/`.
- Fuzzing für alles, was von außen kommt.
- Angriffe: eingeschleustes SQL, gefälschte Unterschriften, wieder eingespielte Nachrichten,
  fremde Lizenz, geänderte Parameter, abgeschnittene Daten, Kanäle über der Grenze.

## Fertig, wenn

Ein Browser kann sich anmelden, der Terminal-Kanal läuft bis zum Controller, Rechte und Lizenzen
greifen, ein Ausfall einer API unterbricht keine Sitzung, und `ngc-store` erzeugt einen Test-Store,
aus dem sich eine Kauf-App freischalten lässt.
