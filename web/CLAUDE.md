# Webinterface

Deine Ordner: `web/`, `ulh/web/`, `tests/browser/`. Verträge: V1, V6, V7 (`contracts/README.md`).

## Aufgabe

Der Desktop aus `prototype/index.html`, aber sauber: Fenster, Taskleiste, Startmenü, hell und
dunkel. Dazu:

- Anmeldung: Benutzer und Passwort mit Passwortschlüssel im Browser (Argon2id nach `proof.KDF`,
  Ed25519), TOTP, Passkeys und Sticks (WebAuthn, immer mit Benutzerprüfung). Das Passwort verlässt
  den Browser nie.
- Sitzung: eigene Schlüsselpaare (Ed25519 und X25519, nicht exportierbar), `session.ClientCert`
  bauen und unterschreiben lassen, `session.APICert` prüfen, Noise-Sitzung zur API
  (`noisep`, Prolog `ngc/session/v1`), Frames nach `wire`.
- Vertrauensliste: Anker vom Webserver holen, Kette prüfen (`trust`), merken; bei einem Bruch Alarm
  und abmelden. Aus ihr kommen die Schlüssel der Controller für Ende zu Ende.
- Ende zu Ende: pro Kanal Noise zum Controller, Prolog `noisep.E2EPrologue`.
- Aufgaben: `task.Task` bauen und mit dem Sitzungsschlüssel unterschreiben, bei Bedarf erneute
  Bestätigung (Passkey oder Passwort erneut) mit Zweck `confirm/v1`.
- Apps: abgeschottete iframes, `web/sdk/ngc.js` (MIT) mit `ngc.call` und `ngc.open`, strenge CSP,
  Oberflächen für App-Store, Benutzer & Rechte, Rechner und Terminal, Anzeige und Einstellung der
  Updates.
- PHP: liefert die Seite aus, liest `/etc/ngc/web.json` und `/etc/ngc/trust-anchor.json`
  (`config`), verwaltet die Liste der API-Server. Kein App-Code läuft im Webserver.

## Tests

- Playwright in `tests/browser/` gegen Chrome, Firefox und Safari; npm nur dort, feste Versionen.
- Die JavaScript-Umsetzung der Verträge prüft sich gegen `contracts/testdata/` (Noise, Signaturen,
  Vertrauensliste, Frames, Kern-Aktionen). Diese Tests laufen ohne API.
- Ein Test zeigt, dass das Passwort den Browser nie verlässt (Mitschnitt der Anfragen).

## Fertig, wenn

Anmeldung in beiden Stufen läuft, der Desktop Apps öffnet, das Terminal über API und Controller
Ausgaben zeigt, eine manipulierte Vertrauensliste Alarm auslöst und die Browser-Tests grün sind.
