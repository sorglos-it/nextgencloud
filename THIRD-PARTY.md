# Fremdcode in NGC

Übernommen per `go mod vendor` (Ordner `vendor/`) beziehungsweise, für JavaScript, als Kopie unter
`apps/web/public/assets/vendor/`. Jede Lizenz ist mit der AGPL-3.0 vereinbar. Sicherheitsupdates ziehen wir selbst
nach: `go get <modul>@<version>`, `go mod vendor`, `govulncheck -tags modules ./...`, Eintrag hier anpassen.

**Der Schalter `-tags modules` gehört dazu, nicht als Beiwerk:** ohne ihn sieht `govulncheck` 12
der 22 Module. Am 18.10.2026 gemessen, derselbe Stand, zweimal mit `-show verbose` – „scanned the
following **12** modules" gegen „**22** modules". Die zehn, die fehlen, sind der Passkey-Strang:
kein Teil bindet ihn ein, also erreicht ihn kein Bau, also prüft ihn das Werkzeug nicht. Zahlen und
Herleitung: `docs/fremdcode.md`.

Was noch kein Teil benutzt, hält `tools/modules.go` fest, sonst würde `go mod tidy` es wieder
entfernen. Zusammen rund 20 MB, davon 8 MB `golang.org/x/sys` und 4,4 MB die SQLite-Übersetzung.
Genau gemessen am 18.10.2026: 19,6 MB in 976 Dateien, 21 Module.

## Selbst ausgewählt

| Modul | Version | Lizenz | Wofür | Übernommen |
|---|---|---|---|---|
| golang.org/x/crypto | v0.57.0 | BSD-3-Clause | Argon2id für den Passwortschlüssel; dazu curve25519, chacha20poly1305, blake2b und blake2s, die github.com/flynn/noise braucht; `xts` verschlüsselt die SQLite-Datei, `ocsp` kommt über go-webauthn/x herein | 2026-09-19 |
| github.com/flynn/noise | v1.1.0 | BSD-3-Clause (GitHub meldet NOASSERTION wegen der Markenzeile) | Noise_KK für Sitzung, Tunnel und Ende zu Ende | 2026-09-19 |
| github.com/ncruces/go-sqlite3 | v0.35.5 | MIT | SQLite ohne cgo für kleine Installationen; `vfs/xts` verschlüsselt die Datei mit AES-256-XTS | 2026-09-22 |
| github.com/go-sql-driver/mysql | v1.10.1 | MPL-2.0 | MariaDB | 2026-09-22 |
| github.com/coder/websocket | v1.8.15 | ISC | WebSocket für Sitzung und Tunnel | 2026-09-22 |
| github.com/go-webauthn/webauthn | v0.18.2 | BSD-3-Clause | Passkeys (WebAuthn, FIDO2) | 2026-09-22 |
| github.com/pquerna/otp | v1.5.0 | Apache-2.0 | TOTP: Einmalkennwörter nach RFC 6238 | 2026-09-22 |

Zur MPL-2.0 des MariaDB-Treibers: Keine seiner Dateien trägt den Vermerk „Incompatible With
Secondary Licenses“, deshalb darf er nach Abschnitt 3.3 zusammen mit AGPL-3.0-Code verbreitet
werden.

## Mitgebracht

Diese Module haben wir nicht ausgesucht; die oberen brauchen sie.

| Modul | Version | Lizenz | Wofür | Gebraucht von |
|---|---|---|---|---|
| golang.org/x/sys | v0.48.0 | BSD-3-Clause | Systemaufrufe von Linux: Dateisperren für SQLite, CPU-Erkennung für blake2b | go-sqlite3, x/crypto |
| github.com/ncruces/go-sqlite3-wasm/v6 | v6.2.35304 | MIT No Attribution | SQLite selbst, übersetzt nach WebAssembly; der Treiber führt es aus | go-sqlite3 |
| github.com/ncruces/julianday | v1.0.0 | MIT | Datumsrechnung von SQLite | go-sqlite3 |
| filippo.io/edwards25519 | v1.2.0 | BSD-3-Clause | Anmeldeverfahren ed25519 von MariaDB | go-sql-driver/mysql |
| github.com/go-webauthn/x | v0.3.1 | BSD-3-Clause (Teil secp256k1: ISC) | Zertifikatsprüfung und Metadaten für Passkeys | go-webauthn/webauthn |
| github.com/fxamacker/cbor/v2 | v2.9.4 | MIT | CBOR, das Format der Passkey-Antworten | go-webauthn/webauthn |
| github.com/x448/float16 | v0.8.4 | MIT | 16-Bit-Gleitkommazahlen in CBOR | fxamacker/cbor |
| github.com/google/go-tpm | v0.9.8 | Apache-2.0 | Nachweise von Sicherheitschips (TPM) bei Passkeys | go-webauthn/webauthn |
| github.com/google/uuid | v1.6.0 | BSD-3-Clause | Kennungen in den Passkey-Metadaten | go-webauthn/webauthn |
| github.com/golang-jwt/jwt/v5 | v5.3.1 | MIT | signierte Metadaten der FIDO-Allianz lesen | go-webauthn/x |
| github.com/go-viper/mapstructure/v2 | v2.5.0 | MIT | Metadaten in Go-Strukturen umfüllen | go-webauthn/x |
| github.com/tinylib/msgp | v1.6.4 | MIT | MessagePack im Zwischenspeicher der Metadaten | go-webauthn/x |
| github.com/philhofer/fwd | v1.2.0 | MIT | gepufferte Ein- und Ausgabe für msgp | tinylib/msgp |
| github.com/boombuler/barcode | v1.0.1-0.20190219062509 | MIT | QR-Code für die Einrichtung von TOTP | pquerna/otp |

Eigene Änderungen an übernommenem Code: keine. **Gemessen am 18.10.2026**, nicht behauptet: in
einer Kopie `vendor/` gelöscht und mit `go mod vendor` neu geholt (das prüft gegen `go.sum` und die
Prüfsummen-Datenbank), dann `diff -rq` gegen das Repo – **0 Unterschiede**. Dazu `go mod verify`:
`all modules verified`.

## JavaScript, von Hand kopiert

Nicht über einen Paketmanager: die Dateien liegen im Repo und werden von uns gepflegt. NGC
installiert zum Bauen und Ausliefern **nichts** mit npm.

| Stück | Version | Lizenz | Wofür | Übernommen |
|---|---|---|---|---|
| @noble/hashes | 2.4.0 | MIT | Argon2id für den Passwortschlüssel im Browser (die Web Crypto API kennt kein Argon2id) und ein SHA-256 mit `update`/`digest` fürs Hochladen | 2026-09-24, `sha2.js` am 2026-09-25 |

Liegt unter `apps/web/public/assets/vendor/noble-hashes/` (Zweig `web`, auf `main` gibt es
`apps/web` noch nicht). Die vollständige Herkunft steht dort in `SOURCE.md`: Quelle, Registry-Hash,
SHA-256 je Datei, warum 2.4.0 und nicht 1.8.0, und der Weg, es nachzuprüfen. Am 18.10.2026 geprüft:
2.4.0 ist die **neueste** Fassung auf npm (veröffentlicht 27.08.2026).

## Nur zum Testen, und nicht im Repo

| Stück | Version | Wofür |
|---|---|---|
| @playwright/test | 1.55.1 | der Browser-Prüfsatz unter `apps/web/tests/` |

**Benannt, weil es die eine Ausnahme ist.** Es kommt per npm (`apps/web/tests/package.json`) und
lädt zusätzlich Browser nach, statt im Repo zu liegen. Begründung: es wird nicht ausgeliefert, kein
Byte davon läuft auf einer Anlage, und ein Prüfwerkzeug im Repo zu pflegen wäre teurer als der
Gewinn. Am 18.10.2026 ist draußen **1.64.1** – wir sind neun Minor-Fassungen hinter, bewusst und
ohne Eile, weil nichts davon ausgeliefert wird.
