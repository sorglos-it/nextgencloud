# Webinterface

Deine Ordner: `web/`, `tests/browser/`, dazu `scripts/ngc-web.sh` im ulh-Repo (`contracts/ulh.md`). Verträge: V1, V5, V6, V7 (`contracts/README.md`).

## Aufgabe

Der Desktop aus `prototype/index.html`, aber sauber: Fenster, Taskleiste, Startmenü, hell und
dunkel. Dazu:

- Anmeldung: Benutzer und Passwort mit Passwortschlüssel im Browser (Argon2id nach `proof.KDF`,
  Ed25519), TOTP, Passkeys und Sticks (WebAuthn, immer mit Benutzerprüfung). Das Passwort verlässt
  den Browser nie. Verlangt eine Rolle das Passwort zusätzlich
  (`session.LoginChallenge.PasswordToo`), kommt `proof` von einem Passkey und `password` vom
  Passwortschlüssel, beide über dieselben Zertifikat-Bytes (Zweck `clientcert/v1`).
- Sitzung: eigene Schlüsselpaare (Ed25519 und X25519, nicht exportierbar), `session.ClientCert`
  bauen und unterschreiben lassen, `session.APICert` prüfen (`session.VerifyAPI`), Noise-Sitzung
  zur API (`noisep`, Prolog `ngc/session/v1`), Frames nach `wire`, Ablauf nach
  `contracts/README.md`, Abschnitt „Kanäle“. Deine Kanalnummern zählst du selbst, bis sie wieder
  frei sind (CLOSE gesendet und empfangen), und öffnest nie mehr als `wire.MaxSessionChannels`;
  sonst beendet die API die Leitung.
- Vertrauensliste: Anker vom Webserver holen, Kette prüfen wie `trust.Open` und `trust.Advance`
  (erst Hash bzw. Unterschrift, dann die Nutzlast lesen), merken; bei einem Bruch Alarm und
  abmelden. Neue Fassungen per `GET /api/v1/trust?from=N` (`session.TrustChain`, seitenweise mit
  `more`); die Steuernachricht `trust` ist nur ein Hinweis. Aus der Liste kommen die Schlüssel der
  Controller für Ende zu Ende. Neue Fassungen, die du baust, bestehen `trust.List.Validate` und
  gehen über die Kern-Aktion `ngc.users/api/trust` an die API. Bei der Ersteinrichtung kommt der
  globale Store aus `session.SetupInfo` (Feld `store`) in Fassung 1; was die API daran prüft, sagt
  `session.VerifySetup`.
- Ende zu Ende: pro Kanal Noise zum Controller aus `core.DesktopApp` (Feld `node`, nicht
  anzeigen). Prolog `noisep.E2EPrologue` mit SHA-256 von `params` und von `task`, jeweils genau den
  Bytes, die du ins OPEN geschrieben hast (fehlt das Feld: SHA-256 des leeren Texts);
  Noise-Nachricht 1 und 2 ohne Nutzlast. Sofort nach Nachricht 2 die erste Nachricht schicken
  (leere Daten oder gleich Eingabe und Ende), innere Nachrichten nach `wire.E2E`; nach deinem Ende
  nichts mehr. Das Ergebnis steht im Ende des Controllers; fehlt es, ist die Antwort abgeschnitten:
  Fehler zeigen. Bei Aktionen ohne `stream` im Manifest nimmst du höchstens `wire.MaxCallResult`
  Byte Ausgabe an und schließt bei mehr mit `E_TOOLARGE`: Die API sieht den Klartext nicht, also
  setzt nur du diese Grenze durch.
- Aufgaben: `task.Task` bauen und mit dem Sitzungsschlüssel unterschreiben, bei Bedarf erneute
  Bestätigung (Passkey oder Passwort erneut) mit Zweck `confirm/v1`. Ein OPEN mit Aufgabe hat keine
  eigenen Parameter.
- Kopplungs- und Beitrittscodes (Kern-Aktionen `pair-code` und `join-code`) zeigst du erst, wenn
  `tunnel.PairCode.Check` bzw. `tunnel.JoinCode.Check` gegen deine neueste geprüfte Fassung
  stimmt; nennt der Code eine neuere, holst und prüfst du sie zuerst. Dazu Nummer und Fingerabdruck
  des Ankers (`tunnel.Fingerprint` des Hashes).
- Apps: abgeschottete iframes, `web/sdk/ngc.js` (MIT) mit `ngc.call` und `ngc.open`, strenge CSP,
  Oberflächen für App-Store (Kataloge mit `store.VerifyCatalog` prüfen), Benutzer & Rechte, Rechner
  und Terminal, Anzeige und Einstellung der Updates. Das ui-tar prüfst du nach den Regeln von
  `store.WalkUI`.
- JSON von außen strikt wie in Go (`contracts/README.md`, Abschnitt „Striktes JSON“: Regeln und
  Budgets). Jede Grenze aus den Go-Paketen gilt auch hier.
- PHP: liefert die Seite aus, liest `/etc/ngc/web.json` und `/etc/ngc/trust-anchor.json`
  (`config`), verwaltet die Liste der API-Server. Kein App-Code läuft im Webserver.

## Tests

- Playwright in `tests/browser/` gegen Chrome, Firefox und Safari; npm nur dort, feste Versionen.
- Die JavaScript-Umsetzung der Verträge prüft sich gegen `contracts/testdata/` (Noise, Signaturen,
  Beweise, Vertrauensliste, Sitzung, Aufgaben, Frames, Kern-Aktionen, Kataloge, ui-tar,
  Kopplungscodes). Diese Tests laufen ohne API.
- Ein Test zeigt, dass das Passwort den Browser nie verlässt (Mitschnitt der Anfragen).

## Fertig, wenn

Anmeldung in beiden Stufen läuft, der Desktop Apps öffnet, das Terminal über API und Controller
Ausgaben zeigt, eine manipulierte Vertrauensliste Alarm auslöst und die Browser-Tests grün sind.
