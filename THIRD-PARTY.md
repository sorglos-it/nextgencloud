# Fremdcode in NGC

Übernommen per `go mod vendor` (Ordner `vendor/`) beziehungsweise, für JavaScript, als Kopie unter
`web/vendor/`. Jede Lizenz ist mit der AGPL-3.0 vereinbar. Sicherheitsupdates ziehen wir selbst
nach: `go get <modul>@<version>`, `go mod vendor`, `govulncheck ./...`, Eintrag hier anpassen.

| Modul | Version | Lizenz | Wofür | Übernommen |
|---|---|---|---|---|
| golang.org/x/crypto | v0.57.0 | BSD-3-Clause | Argon2id für den Passwortschlüssel; dazu curve25519, chacha20poly1305, blake2b und blake2s, die github.com/flynn/noise braucht | 2026-09-19 |
| golang.org/x/sys | v0.48.0 | BSD-3-Clause | indirekt über golang.org/x/crypto (CPU-Erkennung für blake2b) | 2026-09-19 |
| github.com/flynn/noise | v1.1.0 | BSD-3-Clause (GitHub meldet NOASSERTION wegen der Markenzeile) | Noise_KK für Sitzung, Tunnel und Ende zu Ende | 2026-09-19 |

Eigene Änderungen an übernommenem Code: keine.
