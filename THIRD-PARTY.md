# Fremdcode in NGC

Übernommen per `go mod vendor` (Ordner `vendor/`) beziehungsweise, für JavaScript, als Kopie unter
`apps/web/public/assets/vendor/`. Jede Lizenz ist mit der AGPL-3.0 vereinbar. Sicherheitsupdates ziehen wir selbst
nach: `go get <modul>@<version>`, `go mod vendor`, `govulncheck -tags modules ./...`, Eintrag hier anpassen.

**Jedes Modul hier wird von einem Teil wirklich aufgerufen.** Bis zum 18.10.2026 war das anders:
`tools/modules.go` hielt den Passkey-Strang in `go.mod` fest, obwohl kein Teil ihn einband. Die
Datei ist weg, und damit zehn Module (`go-webauthn/webauthn` samt neun Mitgebrachten) und
`x/crypto/ocsp`. Gemessen davor und danach:

| | Bytes | Dateien | Module |
|---|---|---|---|
| vorher | 19 643 720 | 976 | 21 |
| nachher | **16 616 004** | **692** | **12** |

Davon sind 8,2 MB `golang.org/x/sys` und 4,5 MB die SQLite-Übersetzung – zwei Module, die niemand
ausgesucht hat, tragen zwei Drittel.

**Wenn Passkeys wirklich gebaut werden**, kommt der Strang mit einem Befehl zurück:
`go get github.com/go-webauthn/webauthn@latest`, dann `go mod vendor`, und die Zeilen dafür gehören
in die Tabellen unten – mit Lizenz, wie bei allen anderen. Die vorige Fassung stand bei v0.18.2.

**Der Schalter `-tags modules` bleibt im Prüfbefehl stehen, auch wenn er jetzt nichts mehr tut.**
Er war nötig, weil `govulncheck ./...` nur 12 der 22 Module sah – am 18.10.2026 zweimal mit
`-show verbose` gemessen, „scanned the following **12** modules" gegen „**22**". Ein Modul, das
kein Bau erreicht, prüft das Werkzeug nicht. Ohne `tools/modules.go` passt der Tag auf keine Datei
mehr, und beide Befehle zählen dieselben 12 – nachgemessen. Der Schalter kostet nichts und greift
wieder, sobald jemand ein Modul auf Vorrat einträgt. Herleitung: `docs/fremdcode.md`.

**Keine Fassung ist hinter ihrem `@latest`** (am 18.10.2026 gegen `proxy.golang.org` geprüft,
alle 12).

## Selbst ausgewählt

| Modul | Version | Lizenz | Wofür | Übernommen |
|---|---|---|---|---|
| golang.org/x/crypto | v0.58.0 | BSD-3-Clause | Argon2id für den Passwortschlüssel; dazu curve25519, chacha20poly1305, blake2b und blake2s, die github.com/flynn/noise braucht; `xts` verschlüsselt die SQLite-Datei | 2026-09-19, auf v0.58.0 am 2026-10-18 |
| github.com/flynn/noise | v1.1.0 | BSD-3-Clause (GitHub meldet NOASSERTION wegen der Markenzeile) | Noise_KK für Sitzung, Tunnel und Ende zu Ende | 2026-09-19 |
| github.com/ncruces/go-sqlite3 | v0.35.6 | MIT | SQLite ohne cgo für kleine Installationen; `vfs/xts` verschlüsselt die Datei mit AES-256-XTS | 2026-09-22, auf v0.35.6 am 2026-10-18 |
| github.com/go-sql-driver/mysql | v1.10.1 | MPL-2.0 | MariaDB | 2026-09-22 |
| github.com/coder/websocket | v1.8.15 | ISC | WebSocket für Sitzung und Tunnel | 2026-09-22 |
| github.com/pquerna/otp | v1.5.0 | Apache-2.0 | TOTP: Einmalkennwörter nach RFC 6238 | 2026-09-22 |

Zur MPL-2.0 des MariaDB-Treibers: Keine seiner Dateien trägt den Vermerk „Incompatible With
Secondary Licenses“, deshalb darf er nach Abschnitt 3.3 zusammen mit AGPL-3.0-Code verbreitet
werden.

## JavaScript, von Hand kopiert

Kein Paketmanager, kein Bauschritt: Die Dateien liegen im Repo und werden von uns gepflegt.
Kopiert wird nur der Importschluss dessen, was wirklich benutzt wird.

| Ordner | Modul | Version | Lizenz | Wofür | Übernommen |
|---|---|---|---|---|---|
| `apps/web/public/assets/vendor/noble-hashes/` | @noble/hashes | 2.4.0 | MIT | Argon2id für den Passwortschlüssel (`proof.deriveSeed`); die Web Crypto API des Browsers hat kein Argon2id | 2026-09-24 |

Quelle ist die Registry `registry.npmjs.org`, geprüft gegen die dort veröffentlichte Prüfsumme
des Archivs, **bevor** eine Datei ausgepackt wurde:

    @noble/hashes 2.4.0, dist.integrity
    sha512-X5XaVWZIBCT7HHZGm5I7ZQXDwLG+bGXuSrMQAW+7Zvl87h1kmc1ZB1VSRJcpUfoUrGQp4Fkoxm5kZ+Ms+aW+eA==

Welche sieben Dateien kopiert sind, warum es 2.4.0 ist und nicht 1.8.0, die SHA-256 jeder
einzelnen und wie man dieselbe Prüfung wiederholt: `apps/web/public/assets/vendor/noble-hashes/SOURCE.md`.
Wer aktualisiert, macht dieselbe Prüfung und trägt die neuen Prüfsummen dort ein.

Am 18.10.2026 gegen die Registry geprüft: **2.4.0 ist dort die neueste Fassung** (veröffentlicht
27.08.2026), also ist hier nichts nachzuziehen.

## Mitgebracht

Diese Module haben wir nicht ausgesucht; die oberen brauchen sie.

| Modul | Version | Lizenz | Wofür | Gebraucht von |
|---|---|---|---|---|
| golang.org/x/sys | v0.49.0 | BSD-3-Clause | Systemaufrufe von Linux: Dateisperren für SQLite, CPU-Erkennung für blake2b | go-sqlite3, x/crypto |
| github.com/ncruces/go-sqlite3-wasm/v6 | v6.3.35401 | MIT No Attribution | SQLite selbst, übersetzt nach WebAssembly; der Treiber führt es aus | go-sqlite3 |
| github.com/ncruces/julianday | v1.0.0 | MIT | Datumsrechnung von SQLite | go-sqlite3 |
| filippo.io/edwards25519 | v1.2.0 | BSD-3-Clause | Anmeldeverfahren ed25519 von MariaDB | go-sql-driver/mysql |
| github.com/boombuler/barcode | v1.1.0 | MIT | QR-Code für die Einrichtung von TOTP | pquerna/otp |
| github.com/stretchr/testify | v1.12.1 | MIT | Zusicherungen in den **eigenen Tests** von pquerna/otp. Steht nur in `go.mod`: `go mod vendor` schreibt seinen Namen in `modules.txt` und **keine einzige Datei** | pquerna/otp (Tests) |

Eigene Änderungen an übernommenem Code: keine. **Gemessen am 18.10.2026**, nicht behauptet: in
einer Kopie `vendor/` gelöscht und mit `go mod vendor` neu geholt (das prüft gegen `go.sum` und die
Prüfsummen-Datenbank), dann `diff -rq` gegen das Repo – **0 Unterschiede**. Dazu `go mod verify`:
`all modules verified`.

## Nur zum Testen, und nicht im Repo

| Stück | Version | Wofür |
|---|---|---|
| @playwright/test | 1.55.1 | der Browser-Prüfsatz unter `apps/web/tests/` |

**Benannt, weil es die eine Ausnahme ist.** Es kommt per npm (`apps/web/tests/package.json`) und
lädt zusätzlich Browser nach, statt im Repo zu liegen. Begründung: es wird nicht ausgeliefert, kein
Byte davon läuft auf einer Anlage, und ein Prüfwerkzeug im Repo zu pflegen wäre teurer als der
Gewinn. Am 18.10.2026 ist draußen **1.64.1** – wir sind neun Minor-Fassungen hinter, bewusst und
ohne Eile, weil nichts davon ausgeliefert wird.
