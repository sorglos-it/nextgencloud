# Changelog

## Unveröffentlicht

Eine Angabe, die nur angezeigt wird, verwirft die Antwort nicht mehr — und bleibt sie verworfen,
nennt der Satz auf dem Schirm das Feld (`docs/vertraege-v1.2.md`, Punkt 71, Vorschläge b und c).
In `contracts/core.js` trennt die Rechnerliste jetzt zwei Seiten: `version` und `arch` werden
gezeigt und sonst von nichts gelesen, also macht `softenNodes` einen unlesbaren Wert **leer** und
**behält die Zeile**; `node` und `roles` tragen eine Entscheidung und bleiben hart, die ganze
Antwort fällt wie bisher. Dazu gibt die Vertragstabelle den Namen des gefallenen Feldes nach oben
(`fault`, `core.refusedField`), und `apps/store.js` wie `apps/users.js` sagen „Eine Angabe dieses
Servers ist unlesbar (`noise`).“ statt nur „Die Rechner konnten nicht geladen werden.“ — das
Fenster neu zu öffnen half bei dem Fall, der wirklich passiert ist, nämlich
Release-Binärdateien mit `0.0.0-dev`, überhaupt nicht.

Testumgebung für den Recovery-Modus (Paket 0): Skripte in `tools/`, die auf der VM `ngc-recovery`
den TPM-Simulator swtpm, fünf SoftHSM2-Token als Ersatz für vier Root-Sticks und einen
Reservestick sowie QEMU mit Secure-Boot-OVMF einrichten und prüfen. Nachgewiesen ist das
Abnahmekriterium von Paket 0: Versiegeln und Öffnen eines Testgeheimnisses mit einer PolicyOR aus
sieben Zweigen (Normalstart über PCR, sechs Stick-Paare mit PolicySigned, Plattform-PIN und
NV-Zähler) gegen swtpm, dazu die Negativfälle Replay, halber Zweig, fremder Stick, alter
Zählerstand, falsche Plattform-PIN mit Sperre und geänderte PCR. Ein Befehl fährt alles durch:
`tools/recovery-ci.sh`. Gemessen am 12.10.2026 nach vollständigem Wegräumen von TPM-Zustand und
Arbeitsverzeichnis: Ausstiegscode **0**, **39 Fälle**, 62 161 ms. Kein Rust in diesem Paket.
Abnahmeblatt: `docs/recovery-abnahme.md` — die Zeile für Paket 0 füllt ein Mensch aus.

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

- **Recovery Paket 0, Kriterium 9 nur zur Hälfte belegt und hier nicht nachrüstbar:** die Sperre
  nach drei falschen **Plattform**-PINs ist gemessen (`inLockout` direkt beim TPM, danach wird auch
  die richtige PIN abgewiesen). Dass **drei falsche Stick-PINs den Stick sperren**, ist **nicht**
  belegbar: SoftHSM2 2.6 sperrt eine Benutzer-PIN nie — neun falsche PINs hintereinander ließen das
  Token jedes Mal nur auf `user PIN count low` stehen, danach loggte die richtige PIN mit Rückgabe 0
  ein. Gebaut ist der Teil, der hier gilt; der Nachweis gehört zu Paket 2 mit echten Sticks.
- **Recovery Paket 0, die 24 Stunden sind als Wirkung unbelegt:** `MAX_AUTH_FAIL 0x3`,
  `LOCKOUT_INTERVAL` und `LOCKOUT_RECOVERY` je `0x15180` = 86 400 werden **zurückgelesen**. Kein
  Prüflauf kann 86 400 s aussitzen.
- **Recovery Paket 0, Flatter-Punkt:** eine Zahl der Klartext-Buchführung hängt an **genau einem**
  `TPM_RC_RETRY`; ein Lauf mit null oder zwei Wiederholungen macht den Fall rot (sichere Richtung),
  und die Rate ist ungemessen. Der CI-Job wiederholt bewusst **nicht**, sondern ordnet ein Rot
  dieser Art ein und bittet um einen Lauf von Hand. Rückgabe bleibt 1.
- **Recovery Paket 0, ein DEK:** dass `A-auto` und `A-recovery` **dasselbe** Geheimnis tragen,
  prüft dieses Paket nicht. Beide öffnen zu `TEST-DEK`; dass es in Paket 6 ein DEK ist, ist dort zu
  messen.
- **Recovery Paket 0, PIN beim Versiegeln:** aufgezeichnet ist nur das **Öffnen** (42
  Kommandozeilen, Plattform-PIN in keiner). Dass die PIN auch beim **Versiegeln** nicht in der
  Prozessliste steht, ist nicht aufgezeichnet — laut Bericht billig nachrüstbar.
- **Recovery Paket 0, Fehlversuchszähler:** dass er nicht **sinken** kann, ruht auf `nt=counter`
  und ist nicht eigens gemessen.
- **Kein KVM auf `ngc-recovery`:** Der Proxmox-Host reicht keine Virtualisierung durch, QEMU läuft
  emuliert. Für Paket 0 reicht das; für die Zeitmessungen in Paket 10 nicht.
- **Auslöser des CI-Jobs:** `tools/recovery-ci.sh` läuft von Hand. Bei jedem Commit auszulösen,
  braucht einen eigenen Runner auf der VM; das lädt fremden Code und muss Thomas freigeben.
- **Prüf-Notebook ohne Netz** aus Abschnitt 11 der Vorgabe fehlt — Gerätefrage, nicht im Repo lösbar.
- **Echtes Secure Boot** mit eigener PK/KEK/db und signierten UKI kommt mit Paket 8; hier startet
  nur die Secure-Boot-Firmware.
- **EK-Zertifikat** wird nicht gegen die Hersteller-CA geprüft (`swtpm_setup --create-ek-cert`
  braucht root); gehört zu Paket 3.
- **NV-Zähler am leeren Owner-Auth:** In der Testumgebung hat die Owner-Hierarchie kein Passwort,
  der Zähler ließe sich also von jedem Prozess auf der VM erhöhen. Für den Nachweis der Policy ist
  das ohne Belang, für Paket 3 nicht.
- **Stick-PIN in der Prozessliste:** `pkcs11-tool` kann keine PIN aus einer Datei lesen. Die
  TEST-PIN steht deshalb für die Dauer des Aufrufs in `/proc/<pid>/cmdline`. Das echte Stick-Modul
  in Paket 2 benutzt `cryptoki` und darf dieses Muster **nicht** übernehmen.
- **Klartextsuche bleibt eine Nadelsuche:** sie kann nie zeigen, dass das Geheimnis nicht
  entwichen ist. Belegt ist ein **Kriterium**; sieben Wege, es zu umgehen, sind gemessen und stehen
  im Code.
- **`sensitiveDataOrigin` bleibt CLEAR.** Abschnitt 5 der Vorgabe führt das Attribut für
  TPM-Objekte auf; ein von außen zugeführtes Geheimnis (`tpm2_create -i`) kann es aber nicht
  tragen, weil das Attribut gerade besagt, dass das TPM die Daten selbst erzeugt hat. Für Paket 3
  ist zu entscheiden, ob der DEK im TPM entsteht (dann gilt das Attribut) oder weiter zugeführt
  wird (dann bleibt es CLEAR und die Vorgabe wird an dieser Stelle nachgezogen).
- **Echter HSM-Stick** (Nitrokey HSM 2 für `hsm`) wird vorerst nicht beschafft (Entscheidung
  Thomas, 23.09.2026); Paket 2 entsteht für `piv` (YubiKey 5, Nitrokey 3) und `softhsm`, die
  `hsm`-Anbindung bleibt offen.
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

## apps/web 0.2.0

Noch nicht etikettiert: dieser Abschnitt gehört zum Stand oben und beschreibt Plan 1, das
Webinterface. `apps/web/VERSION` steht auf 0.2.0.

- **Verträge in JavaScript:** V1, V2, V5, V6 und V7 als Module unter
  `public/assets/js/contracts/`, jedes gegen `shared/testdata/` geprüft – Noise, Signaturen,
  Beweise, Vertrauensliste, Sitzung, Aufgaben, Frames, Kern-Aktionen, Kataloge, ui-tar,
  Kopplungscodes, striktes JSON samt Budget, UI-Prüfsummen, UI-Beweise, Hosts und Adressen.
  Dazu ein Wächter über jeden Export jedes Vertrags, der zweimaliges Lesen derselben Bytes
  ausschließt (`tests/contracts/bytes.spec.js`), und fünf Fuzz-Ziele für die Parser.
- **Anmeldung in beiden Stufen:** Benutzer und Passwort mit Passwortschlüssel im Browser
  (Argon2id nach `proof.KDF`, Ed25519), TOTP, Passkeys und Sticks über WebAuthn, dazu die Rolle
  mit `passwordToo`, die das Zertifikat zweimal unterschreiben lässt. Das Passwort verlässt den
  Browser nie – gemessen am Mitschnitt aller Anfragen und aller Socket-Frames, bei der Anmeldung
  und bei der erneuten Bestätigung.
- **Ersteinrichtung:** der Einmal-Code bleibt im Browser; das Angebot von `GET /api/v1/setup` wird
  mit ihm geprüft, bevor irgendetwas daraus gelesen wird, und Fassung 1 der Vertrauensliste geht
  mit einem MAC über genau die gesendeten Bytes hinaus.
- **Sitzung mit Noise und Ende zu Ende:** eigene, nicht exportierbare Schlüsselpaare,
  Sitzungszertifikat, geprüfte API-Antwort, Noise-Sitzung zur API, Frames und Freigaben nach
  `wire`, eigene Kanalnummern bis `wire.MaxSessionChannels`, und je Kanal eine zweite
  Noise-Sitzung zum Controller.
- **Desktop:** Fenster verschieben, andocken, in der Größe ändern, maximieren, minimieren,
  Taskleiste, Startmenü mit Suche, hell und dunkel, sechs Akzentfarben, fünf Hintergründe,
  Tastaturbedienung und Namen für Screenreader. Hält Namen aus 10 000 Zeichen, reinen
  Steuerzeichen, einem Null-Byte, einer Bidi-Umschaltung, doppelten Kennungen und 1001 Apps
  stand; alles von außen ist Text, nie HTML.
- **App-Fenster:** jede Oberfläche läuft in einem abgeschotteten Rahmen mit eigener Herkunft
  (`/frame.html`, `sandbox allow-scripts` in der Kopfzeile), ohne Speicher und ohne eigene
  Verbindung. Vorher wird ihr Beweis gegen die Vertrauensliste geprüft, das ui-tar gehasht und
  erst danach gelesen; die Startseite muss eine Datei dieses Archivs sein.
- **Die vier mitgelieferten Oberflächen:** App-Store (Kataloge, Hochladen, Aktivieren, Updates),
  Benutzer und Rechte (wartende Controller und API-Server mit selbst gerechnetem Fingerabdruck,
  neue Fassungen der Vertrauensliste), Rechner und Terminal.
- **Browser-Tests:** die ganze Mappe gegen Chrome, Firefox und Safari über einen eingebauten
  PHP-Server und eine Mock-API; die Sicherheitskopfzeilen werden nicht nur gelesen, sondern dem
  Browser vorgehalten (`tests/shell/csp.spec.js`), und die acht Angriffe aus Entwurf 4.6 haben
  ihre eigene Mappe (`tests/shell/attacks.spec.js`).
- **Vorlage für den Webserver:** `apps/web/templates/apache-vhost.conf` – sechs Befehle der Reihe
  nach, ein Name zum Einsetzen, dazu `apache-apis.conf.example` als die Form, die `ngc-web` für die
  API-Server schreibt. Die Vorlage legt fest, dass nur `public/` von außen erreichbar ist, und setzt
  vier Kopfzeilen für die Dateien, die Apache selbst ausliefert: `nosniff`,
  `Cross-Origin-Resource-Policy`, HSTS und eine **eigene** Richtlinie, nicht die der Seite. Vorher
  hatte das Repo keine Serverkonfiguration, und jede `.js` und `.css` ging ohne Wand hinaus. Die
  Werte stehen nur in der Vorlage; die Mock-API liest sie von dort, also kann keine zweite Fassung
  abdriften, und sie prüft dabei auch, dass der Block überhaupt unter dem DocumentRoot liegt. Die
  Begründung je Kopfzeile steht in der Datei – samt der beiden, die bewusst fehlen.

Zum Zeitpunkt dieses Eintrags liefen noch vier Aufgaben des Plans: der Adapter zwischen Desktop und
Sitzung, die Oberfläche der Ersteinrichtung, der Rechner im abgeschotteten Rahmen und das
Installationsskript. Was sie bringen, kommt mit ihnen.

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
