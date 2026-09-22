# Fremdcode in NGC

Übernommen per `go mod vendor` (Ordner `vendor/`) beziehungsweise, für JavaScript, als Kopie unter
`apps/web/public/assets/vendor/`. Jede Lizenz ist mit der AGPL-3.0 vereinbar. Sicherheitsupdates ziehen wir selbst
nach: `go get <modul>@<version>`, `go mod vendor`, `govulncheck ./...`, Eintrag hier anpassen.

Was noch kein Teil benutzt, hält `tools/modules.go` fest, sonst würde `go mod tidy` es wieder
entfernen. Zusammen rund 20 MB, davon 8 MB `golang.org/x/sys` und 4,4 MB die SQLite-Übersetzung.

## Selbst ausgewählt

| Modul | Version | Lizenz | Wofür | Übernommen |
|---|---|---|---|---|
| golang.org/x/crypto | v0.57.0 | BSD-3-Clause | Argon2id für den Passwortschlüssel; dazu curve25519, chacha20poly1305, blake2b und blake2s, die github.com/flynn/noise braucht | 2026-09-19 |
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

Eigene Änderungen an übernommenem Code: keine.
