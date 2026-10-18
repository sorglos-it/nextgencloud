# Changelog

## Unveröffentlicht

Fremdcode durchgezählt und aufgeräumt (`docs/fremdcode.md`, 18.10.2026). Der Passkey-Strang war in
`go.mod` festgehalten, obwohl kein Teil eine Zeile davon aufruft: `tools/modules.go` ist weg, und
mit ihm `go-webauthn/webauthn`, seine neun Mitgebrachten und `x/crypto/ocsp`. Gemessen:
**19,6 MB in 976 Dateien und 21 Modulen → 16,6 MB in 692 Dateien und 12 Modulen.** Die fünf
übrigen Module, die hinten waren, sind angehoben (x/crypto v0.58.0, x/sys v0.49.0, go-sqlite3
v0.35.6, go-sqlite3-wasm v6.3.35401, barcode v1.1.0 statt einer Pseudofassung von 2019) – damit ist
**kein Modul mehr hinter seinem `@latest`**, geprüft gegen `proxy.golang.org`. `check: ok`,
`govulncheck` 0 erreichbare Lücken, und die drei Programme bauen weiter bitgleich (`git
hash-object` zweimal gleich, auch mit leerem `GOCACHE`).

Dabei zwei Fehler im Verfahren behoben: `govulncheck ./...` sah **12 der 22** Module, weil die zehn
des Passkey-Strangs nur hinter einem Build-Tag standen – der Schalter `-tags modules` steht jetzt
an allen drei Stellen, die den Befehl nennen, mit den Zahlen daneben. Und `tools/release-gate.sh`
baute `ngc-store` ohne `-trimpath`, die einzige Bauzeile im Repo, die nicht reproduzierbar baute.

### Offen

- **Recovery, leerer Owner-auth:** die Testumgebung legt den NV-Zähler mit leerem
  Owner-auth an. Das ist für die Stufe `test` entschieden und **nicht** geprüft; ein
  echter Server braucht einen Owner-auth, sonst kann jeder lokale Prozess den Zähler
  anfassen. Vorgabe `docs/recovery-modus.md`, Abschnitt 13 verlangt diesen Eintrag.
- **Recovery, NV-Index:** der Index des Zählers ist festgelegt und nirgends gegen eine
  Kollision mit fremder Firmware geprüft.

Vertragsänderung V1: Die Anmelde-Antwort (`session.LoginChallenge`) nennt je Passkey nicht mehr nur
die Credential-ID, sondern auch die Schlüsselkennung (`kid`), die in den Beweis gehört. Der Browser
hat bei der Anmeldung noch keine Vertrauensliste; ohne die Kennung kam niemand mit Passkey herein.
Dazu `session.ParseLoginChallenge` mit Grenzen und Testdaten (`login` in `session.json`).

Dieselbe Stelle weiter: `session.Challenge` baut die Anmelde-Antwort aus der Vertrauensliste, ohne
Datenbank. Einen unbekannten Benutzernamen beantwortet sie erfunden, fest abgeleitet aus dem Namen
und dem neuen `config.API.ChallengeKey`, damit derselbe Name dieselbe Antwort bekommt, solange die
Vertrauensliste steht – sonst verriete der zweite Versuch, welche Benutzer es gibt. Ein eigener
Schlüssel und nicht `secretKey`: Ein Wechsel des Datenbankschlüssels würfelte jede erfundene
Antwort neu, während echte gleich blieben.

Die Form der erfundenen Antwort stammt aus der Liste selbst – Anteil der Benutzer je Form,
Häufigkeit der Passkey-Zahl, Längen echter Credential-IDs, Parameter eines echten `kdf`; für `totp`
kommt der Anteil vom Aufrufer. Feste Quoten waren in Installationen, die nicht dem Durchschnitt
entsprechen, sofort als erfunden zu erkennen. Beide Wege tun dieselbe Arbeit, damit nicht Laufzeit
und Speicher verraten, was die Antwort verschweigt. `rp.id` prüft `proof.ValidRPID` überall gleich.

Recovery-Modus als verbindliche Vorgabe aufgenommen: `docs/recovery-modus.md`, dazu die Zeile in
`CLAUDE.md`. Root-Sticks mit Quorum, TPM, verschlüsseltes Datenbank-Volume, signierte
Admin-Datensätze, Stufen enterprise, home und test. Noch kein Code.

Fremdcode für die vier Teile übernommen (`vendor/`, aufgezählt in `THIRD-PARTY.md`): SQLite ohne
cgo mit AES-256-XTS, MariaDB-Treiber, WebSocket, Passkeys und TOTP. Mit allem, was sie mitbringen,
sind es 21 Module und rund 20 MB; jede Lizenz ist mit der AGPL-3.0 vereinbar, `govulncheck` meldet
keine erreichbare Lücke.

Datenbank: `dbapi` läuft auf MariaDB mit Galera und auf einer mit AES-256-XTS verschlüsselten
SQLite-Datei, beide mit denselben Konformitätstests. Das Schema kommt aus nummerierten
Migrationen, die nur hinzufügen dürfen, damit die vorige API-Version während eines Updates
weiterläuft. MariaDB nimmt nur TLS-Verbindungen gegen die CA der Installation, verteilt die Last
über die Knoten und verkraftet den Ausfall eines Knotens.

Sichern der SQLite-Datei: `sqlite.Backup` legt eine Kopie neben die Datenbank, während niemand sie
ändern darf, und die Kopie bleibt genauso verschlüsselt wie das Original. Ohne `api.json`
beziehungsweise `controller.json` kann sie niemand öffnen – auch der Besitzer nicht.

## 0.2.0

- API: Anmeldung, Sitzungen, Rechte, Weiterleitung, Tunnel, Apps, Stores, Lizenzen, Protokoll und
  Updates.
- ngc-store: Pakete, Verschluesselung, Kataloge, Test-Lizenzen und Test-Releases.

Dazu die Angriffe als eigenes Paket (`apps/server-api/classes/attack`): SQL in jedem Namen,
gefaelschte Unterschriften, Wiedereinspielen, fremde Lizenz, nachtraeglich geaenderte Parameter,
abgeschnittene Pakete, die Kanalgrenze und ein Mann in der Mitte bei der Ersteinrichtung. Je
Angriff eine Zwischenpruefung, dass derselbe Weg ohne den Angriff durchgeht.

**Was dabei noch nicht messbar war:** die Aktionen `ngc.store/api/*` sind in `coreapp.New` bei
niemandem angemeldet und antworten `E_NOTFOUND`, was auch immer man ihnen schickt. Angriffe gegen
`sources`, `catalog`, `installed`, `upload`, `license` und `install-key` sind darum **nicht
gemessen**; `TestTheStoreChannelsHaveNoHandlerYet` haelt diesen Zustand fest und wird rot, sobald
die Handler haengen. `govulncheck` ist gelaufen: 0 erreichbare Luecken.

## vertraege-v1 (22.09.2026)

Die Verträge V1 bis V7 zwischen Webinterface, API, Controller und Datenbank stehen: Go-Pakete in
`shared/` mit Testdaten in `shared/testdata/`, die Regeln in `shared/README.md` und `shared/ulh.md`,
dazu die Anweisungen je Teil und das Grundgerüst der Programme `ngc-api`, `ngc-controller` und
`ngc-store`.

Was die Verträge festlegen:

- **Anmeldung und Sitzung:** Passwortschlüssel im Browser, TOTP, Passkeys; Sitzungszertifikate,
  Noise-Sitzung zur API, HTTP-Regeln vor dem WebSocket (Methoden, Statuscodes, Fehler als
  CLOSE-JSON, Cookie).
- **Ersteinrichtung:** Der Einmal-Code geht nie über die Leitung; Browser und API weisen sich mit
  ihm gegenseitig aus.
- **Kanäle:** Frames, Freigaben, Obergrenzen je Sitzung und Tunnel, Ende-zu-Ende-Verschlüsselung
  bis zum Dienst, Ergebnisse nur bei erfolgreichem Ende.
- **Vertrauensliste:** Kette mit Ankern, Admins, Rollen, Stores, Controllern und API-Servern;
  Kopplungs- und Beitrittscodes mit TLS-Fingerabdruck.
- **Apps und Store:** Pakete, Kataloge, Lizenzen, Releases; jede Oberfläche mit signierter
  Prüfsumme, Updates nur mit den Rechten und aus dem Store der Aktivierung.
- **Adressen:** eine Grammatik für Tunnel, Webserver, Passkey-Herkunft, Stores, Datenbankknoten und
  Katalog-URLs, auch für die Installationsskripte.
- **Datenbank:** `dbapi` für MariaDB und SQLite mit gemeinsamen Konformitätstests.
- **Striktes JSON:** feste Regeln und Budgets für alles, was von außen kommt, mit Testdaten für die
  Umsetzung im Browser.

Geprüft: `tools/check.sh` (Format, vet, Tests, Bau für linux/amd64 und arm64), unter Linux auch mit
`-race`, 39 Fuzz-Ziele je 3 Minuten ohne Fund, `govulncheck` ohne erreichbare Lücke, jeder Go-Name
der Dokumente vorhanden.

Aufbau nach dem Projektstruktur-Standard: Teile unter `apps/` (`web`, `server-api`,
`server-controller`, `server-db`, `cli`), Verträge in `shared/` (vorher `contracts/`), Skripte in
`tools/` (vorher `scripts/`), Prototyp in `docs/prototype.html` (vorher `prototype/index.html`).
Der Go-Importpfad der Verträge ist `github.com/sorglos-it/nextgencloud/shared/…`.
