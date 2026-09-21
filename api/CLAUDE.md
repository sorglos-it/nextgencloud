# API

Deine Ordner: `api/`, `ulh/api/`. Verträge: V1, V2, V4, V5, V6, V7.

## Aufgabe

Der Vermittler und die Firewall zwischen Oberfläche und Servern. Führt nie App-Code aus.

- Anmeldung nach `session`: Challenge, Passwortschlüssel oder Passkey prüfen (`proof`), TOTP,
  Sperre nach Fehlversuchen, Sitzung und Zertifikate, Cookie mit `session.TokenHash`.
- WebSocket nach `wire` und `noisep`, Kanäle mit Freigaben, Grenzen je Sitzung. `wire.MaxChannels`
  ist nur eine Konstante; die Grenze für offene Kanäle je Sitzung oder Link muss die API selbst
  durchsetzen.
- Rechte je Kanal: Benutzer, Rolle, App, Aktion, Lizenz. Kern-Aktionen nach `core`.
- Tunnel zu jedem Controller (`tunnel`), Kopplung, Verteilung der Vertrauensliste, Weiterleitung der
  Kanäle mit `tunnel.Open`.
- Apps und Stores: Kataloge prüfen (`store.VerifyCatalog`), Pakete entpacken (`store.Walk`),
  Oberflächen in der Datenbank ablegen, Lizenzen prüfen, Schlüssel der Kauf-Apps entpacken
  (`store.UnwrapKey`) und nur an eigene Controller geben.
- Updates: Reihenfolge Datenbank → APIs → Controller → Webinterface, gesteuert von genau einer API
  über `dbapi.LockUpdate`.
- Protokoll: Anmeldungen, Rechte, Aufgaben mit Unterschrift, Kanäle, Stores, Lizenzen, Updates.
- Mehrere APIs teilen sich Schlüssel und Sitzungen über die Datenbank; jede kann jede ersetzen.
- Werkzeug `ngc-store`: Apps packen, verschlüsseln, Kataloge und Test-Lizenzen signieren.

## Tests

- Gegen `dbapi/memstore`, ohne Datenbank.
- Gegen die Vektoren in `contracts/testdata/`.
- Fuzzing für alles, was von außen kommt.
- Angriffe: eingeschleustes SQL, gefälschte Unterschriften, wieder eingespielte Nachrichten,
  fremde Lizenz.

## Fertig, wenn

Ein Browser kann sich anmelden, der Terminal-Kanal läuft bis zum Controller, Rechte und Lizenzen
greifen, ein Ausfall einer API unterbricht keine Sitzung, und `ngc-store` erzeugt einen Test-Store,
aus dem sich eine Kauf-App freischalten lässt.
