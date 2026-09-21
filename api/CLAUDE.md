# API

Deine Ordner: `api/`, dazu `scripts/ngc-api.sh` im ulh-Repo (`contracts/ulh.md`). Verträge: V1, V2, V4, V5, V6, V7.

## Aufgabe

Der Vermittler und die Firewall zwischen Oberfläche und Servern. Führt nie App-Code aus.

- Anmeldung nach `session`: Anfragen nur über `session.ParseLoginStart`, `session.ParseLoginFinish`
  und `session.ParseSetup`; Passwortschlüssel oder Passkey prüfen (`session.VerifyClient`). Hat
  eine Rolle des Benutzers `passwordToo` (`trust.List.PasswordToo`, dann
  `session.LoginChallenge.PasswordToo`), kommt der Beweis von einem Passkey und dazu der
  Passwortbeweis (`password`), den du mit `session.VerifyPassword` prüfst. Das setzt nur die API
  durch; an Controller geht kein Passwortbeweis. TOTP, Sperre nach Fehlversuchen, Sitzung und
  Zertifikate, Cookie mit `session.TokenHash`. Ersteinrichtung: `session.SetupInfo` mit dem
  globalen Store aus `api.json` (`storeUrl`, `storeKey`), Fassung 1 mit `session.VerifySetup`
  prüfen, den Einmal-Code in konstanter Zeit vergleichen.
- WebSocket nach `wire` und `noisep` (Ablauf: `contracts/README.md`, Abschnitt „Kanäle“):
  Kanalnummern höchstens `wire.MaxSessionChannels` je Sitzung und `wire.MaxTunnelChannels` je
  Tunnel, gezählt, bis sie wieder frei sind (CLOSE gesendet und empfangen); ein Frame darüber
  beendet die Leitung. Ist der Tunnel zum Ziel-Controller voll, antwortest du dem Browser mit CLOSE
  `E_LIMIT`. Freigabe je Strecke bis `wire.MaxWindow`, Steuernachrichten nach
  `wire.ControlAllowed`.
- Kanäle weiterleiten: das OPEN des Browsers Byte für Byte in `tunnel.Open` (Feld `open`), mit
  eigener Freigabe; DATA und EOF unverändert und eins zu eins, CLOSE mit demselben Code;
  Noise-Nachricht 2 aus der ersten GRANT des Controllers in die erste GRANT an den Browser
  kopieren. Ende-zu-Ende-Daten nie auspacken.
- Rechte je Kanal: Benutzer, Rolle, App, Aktion, Lizenz. Kern-Aktionen nach `core`; Aufgaben mit
  `task.Check` und `core.RuleFor` bzw. `store.RuleFor`, nie mit Parametern aus dem OPEN. Eine
  Aufgabe, die du an einen Controller weiterleitest, prüfst du vorher mit dem Ziel-Controller als
  `task.Input.Node`; Aufgaben-IDs merkst du dir (`dbapi.OnceTask`).
- Tunnel zu jedem Controller (`tunnel`), Kopplung, Kopplungs- und Beitrittscodes (`tunnel.PairCode`,
  `tunnel.JoinCode`; Kern-Aktionen `pair-code` und `join-code`). Neue Fassungen der
  Vertrauensliste an Controller über den Kanal `ngc.users/controller/trust`, an Browser per
  `GET /api/v1/trust` (`session.TrustChain`, seitenweise) und Hinweis `trust`. Ohne Benutzer
  öffnest du nur, was `core.WithoutUser` nennt.
- Apps und Stores: Kataloge prüfen (`store.VerifyCatalog`), Pakete entpacken (`store.Walk`),
  Oberflächen als tar nach den Regeln von `store.WalkUI` ablegen und ausliefern, Lizenzen prüfen,
  Schlüssel der Kauf-Apps entpacken (`store.UnwrapKey`) und nur an eigene Controller geben. Der
  globale Store heißt `global` (`store.GlobalID`), `local` steht für hochgeladene Apps
  (`core.LocalStore`); einen zweiten Store `global` lehnst du ab.
- Updates: Reihenfolge Datenbank → APIs → Controller → Webinterface, gesteuert von genau einer API
  über `dbapi.LockUpdate`. Die Migrationen spielt genau ein Controller mit Rolle `db` ein; er
  bekommt dafür die Datei `ngc-api` seiner Architektur.
- Protokoll: Anmeldungen, Rechte, Aufgaben mit Unterschrift, Kanäle, Stores, Lizenzen, Updates;
  jede Zeile mit einer ID aus 16 Zufallsbytes als Hex (`core.LogEntry`, `core.ValidLogID`).
- Mehrere APIs teilen sich Schlüssel und Sitzungen über die Datenbank; jede kann jede ersetzen. Ein
  neuer API-Server kommt mit einem Beitrittscode (`tunnel.JoinCode`, `tunnel.JoinRequest`); wie er
  nach der Unterschrift des Admins die gemeinsamen Schlüssel bekommt, legst du selbst fest.
- Werkzeug `ngc-store`: Apps packen, verschlüsseln, Kataloge, Releases und Test-Lizenzen signieren.
- Konfiguration nur über `config.LoadAPI`. Schlüssel und Passwörter darin sind `config.Secret` bzw.
  `config.SecretText`; den Wert geben nur `Bytes()` bzw. `Text()`. Ins Log nur über die
  `LogValue`-Methoden, nie Schlüssel oder Passwörter.

## Tests

- Gegen `dbapi/memstore`, ohne Datenbank.
- Gegen die Vektoren in `contracts/testdata/`.
- Fuzzing für alles, was von außen kommt.
- Angriffe: eingeschleustes SQL, gefälschte Unterschriften, wieder eingespielte Nachrichten,
  fremde Lizenz, geänderte Parameter, abgeschnittene Daten, Kanäle über der Grenze.

## Fertig, wenn

Ein Browser kann sich anmelden, der Terminal-Kanal läuft bis zum Controller, Rechte und Lizenzen
greifen, ein Ausfall einer API unterbricht keine Sitzung, und `ngc-store` erzeugt einen Test-Store,
aus dem sich eine Kauf-App freischalten lässt.
