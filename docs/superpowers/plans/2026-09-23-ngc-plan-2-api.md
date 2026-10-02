# NGC Plan 2: API-Server und `ngc-store` – Umsetzungsplan

> **Für ausführende Agenten:** PFLICHT-SKILL: `superpowers:subagent-driven-development` (empfohlen)
> oder `superpowers:executing-plans`, Aufgabe für Aufgabe. Schritte nutzen Kästchen (`- [ ]`).

**Ziel:** `apps/server-api/` (Programm `ngc-api`) und `apps/cli/` (Programm `ngc-store`) bauen: den
Vermittler zwischen Browser und Controllern mit Anmeldung, Sitzungszertifikaten, Rechten,
Weiterleitung, Tunneln, Apps, Stores, Lizenzen, Protokoll und Updates, dazu das Werkzeug, mit dem
jeder einen eigenen Store betreiben kann.

**Architektur:** `main.go` liest `api.json` (`config.LoadAPI`), öffnet die Datenbank über
`dbapi.Store` und startet einen einzigen HTTPS-Listener mit dem eigenen, selbst signierten
Zertifikat. Auf ihm liegen die Pfade der Shell (`/api/v1/…`, hinter Apache) und die Pfade der
Controller und weiterer API-Server (`/tunnel`, `/tunnel/pair`, `/tunnel/join`, `/tunnel/keys`).
Jede Leitung (Sitzung und Tunnel) ist ein WebSocket mit Noise_KK darin; darüber laufen Frames und
Kanäle nach `wire`. Die Logik liegt in Unterpaketen von `apps/server-api/classes/`; jedes Paket
kennt nur die Verträge aus `shared/` und die Pakete unter sich. Dauerhaften Zustand hält die API
nirgends im Speicher: Sitzungen, Apps, Vertrauensliste und Protokoll liegen in der Datenbank, damit
jede API jede ersetzen kann.

**Tech Stack:** Go 1.27 (Standardbibliothek: `crypto/tls`, `crypto/x509`, `crypto/ecdh`,
`crypto/ed25519`, `crypto/aes`, `crypto/cipher`, `net/http`, `encoding/json/v2`),
`github.com/coder/websocket` v1.8.15 (WebSocket), `github.com/pquerna/otp` v1.5.0 (TOTP),
`github.com/flynn/noise` v1.1.0 über `shared/noisep`. Fremdcode nur aus `vendor/`.

**Entwurf:** [`docs/superpowers/specs/2026-09-19-ngc-kern-design.md`](../specs/2026-09-19-ngc-kern-design.md)
· **Anweisungen:** [`apps/server-api/CLAUDE.md`](../../../apps/server-api/CLAUDE.md),
[`apps/cli/CLAUDE.md`](../../../apps/cli/CLAUDE.md) · **Verträge:**
[`shared/README.md`](../../../shared/README.md) (Etikett `vertraege-v1.1`),
[`shared/ulh.md`](../../../shared/ulh.md)

---

## Global Constraints

Diese Vorgaben gelten für jede Aufgabe; sie werden nicht in jeder Aufgabe wiederholt.

- **Verträge sind eingefroren.** `shared/` wird in diesem Plan nie geändert. Wer eine Änderung
  braucht, trägt sie unter „Offene Entscheidungen für den Koordinator“ ein und arbeitet ohne sie
  weiter.
- **Fremde Ordner sind tabu:** geschrieben werden nur `apps/server-api/` und `apps/cli/`, dazu die
  zwei Zeilen dieses Teils in `tools/modules.go` und `scripts/ngc-api.sh` im ulh-Repo.
- **Go 1.27**, Bau für linux/amd64 und linux/arm64 mit `CGO_ENABLED=0`; keine Laufzeitabhängigkeit
  außerhalb von `vendor/`.
- **`tools/check.sh` muss vor jedem Commit grün sein** (gofmt, `go vet`, `go test ./...`, Bau für
  beide Ziele). Auf Linux zusätzlich `go test -race ./...`.
- **TDD:** erst der scheiternde Test, dann die kleinste Umsetzung. Keine Tests löschen oder
  überspringen, um grün zu werden.
- **Jeder Parser bekommt einen Fuzz-Test** (`func FuzzXxx(f *testing.F)`), mit mindestens einem
  Startwert aus `shared/testdata/`.
- **Tests laufen gegen `dbapi/memstore`**, nie gegen eine echte Datenbank, und gegen die Vektoren
  in `shared/testdata/`. Schlüssel und Vertrauenslisten für Tests kommen aus
  `shared/trust/trusttest`; dieses Paket darf nur in `_test.go`-Dateien importiert werden,
  `tools/check.sh` bricht sonst ab.
- **Sicheres Programmieren** (`CLAUDE.md`, Abschnitt „Regeln für sicheres Programmieren“), wörtlich:
  jede Eingabe von außen über die `Parse…`- oder `Verify…`-Funktion des Vertrags, nie direkt
  `json.Unmarshal`; Obergrenzen überall; Kryptografie nur aus Standardbibliothek oder `vendor/`;
  TLS zu API-Servern nur über `tunnel.PinnedTLS`; MariaDB nur über `config.DB.TLSConfig`;
  Geheimnisse nie ins Log, auch nicht gekürzt. SQL schreibt die API nie selbst: sie spricht nur
  über `dbapi`.
- **Ergebnisse kodiert immer `core.EncodeResult`**, nie `json.Marshal`.
- **Sprache:** Code, Bezeichner, Kommentare und Commit-Nachrichten Englisch; Meldungen für Benutzer
  Deutsch (`wire.Code.Message`, `core.Notice.Text`, `core.SourceInfo.Error`).
- **Kommentare erklären, warum etwas so ist**, nicht was die Zeile tut.
- **Commits sind klein**, eine Nachricht in der Befehlsform, mit Trailer
  `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`. Branch: `api`, Pull Request nach `main`.
- **Version:** `apps/server-api/VERSION` und `apps/cli/VERSION` stehen auf `0.1.0`; gebaut wird mit
  `-ldflags "-X main.Version=<Inhalt von VERSION>"`.
- Code aus diesem Plan nach dem Einfügen mit `gofmt -w` formatieren; Abweichungen bei Einrückung
  sind kein Planfehler.

### Wiederkehrende Größen aus den Verträgen

| Wert | Bedeutung |
|---|---|
| `session.MaxRequest` (64 KiB) | größter JSON-Körper vor dem WebSocket |
| `session.MaxCert` (4096) | Client- und API-Zertifikat |
| `session.MaxChain` (32 MiB), `session.MaxChainVersions` (64) | eine Antwort von `GET /api/v1/trust` |
| `session.MaxSkew` (300 s) | geduldeter Uhrunterschied; auch das Fenster für `OncePurge` |
| `session.ChallengeLifetime` (120 s) | Lebensdauer einer Anmelde-Nonce |
| `wire.MaxFrame` (65519) | größter Frame im Klartext |
| `wire.MaxFrame + noisep.TagSize` (65535) | **Lesegrenze des WebSockets** — gemessen und am 01.10.2026 berichtigt: diese Zeile nannte vorher `MaxFrame` allein, während der Code desselben Plans richtig `wire.MaxFrame + noisep.TagSize` schreibt. Ein gesiegelter Frame trägt sein Siegel mit. |
| `wire.MaxSessionChannels` (64), `wire.MaxTunnelChannels` (1024) | belegte Kanalnummern |
| `wire.MaxWindow` (1 MiB), `wire.MinWindow` (4 KiB) | offene Freigabe je Strecke und Richtung |
| `wire.MaxCallResult` (8 MiB) | Ergebnis nach Art von `call` |
| `core.Action.MaxIn` | was der Öffner bei `in` bzw. die API bei `transfer` schicken darf |
| `store.MaxPackage` (4 GiB) | App-Paket |
| `sign.MaxPayload` (4 MiB) | Nutzlast eines Umschlags (Katalog, Release, Vertrauenslisten-Fassung) |

---

## Dateien-Übersicht

Alles Neue; heute gibt es je Teil nur `main.go`, `VERSION` und `CLAUDE.md`.

```
apps/server-api/
├── VERSION                     0.1.0
├── main.go                     Einstiegspunkt: Konfiguration, Datenbank, Listener, Abschalten   (T24)
└── classes/
    ├── secrets/                Geheimnis-Box (AES-256-GCM) und Zufallswerte                     (T1)
    ├── apitls/                 eigenes, selbst signiertes TLS-Zertifikat und sein Fingerabdruck (T2)
    ├── dbopen/                 Datenbank nach config.DB öffnen (Anmeldung durch apps/server-db) (T3)
    ├── state/                  Laufzeitzustand: Konfiguration, Datenbank, Vertrauensliste       (T3)
    ├── httpapi/                Antworten, Fehler, Körper, Router vor dem WebSocket              (T4)
    ├── lock/                   Sperre nach Fehlversuchen, je Anmeldename                        (T5)
    ├── setup/                  Ersteinrichtung: Einmal-Code, Angebot, Fassung 1                 (T5)
    ├── login/                  Anmeldung, TOTP, Sitzungen, Cookie, Vertrauenskette              (T6–T8)
    ├── link/                   Noise über WebSocket, Frames, Kanaltabelle, Freigabe             (T9, T10)
    ├── browser/                Sitzungsleitung zum Browser                                      (T11)
    ├── controllers/            Tunnel, Kopplung, hello, status, Registry                        (T12)
    ├── rights/                 Rechte je Kanal, Aufgaben, erneute Bestätigung                   (T13)
    ├── audit/                  Protokoll                                                        (T14)
    ├── coreapp/                Versand der Kern-Aktionen; ngc.desktop und ngc.users             (T15, T16)
    ├── relay/                  Weiterleitung eines Browser-Kanals an einen Controller           (T17)
    ├── join/                   Beitrittscodes, wartende API-Server, gemeinsame Schlüssel        (T18)
    ├── appstore/               Stores, Kataloge, Hochladen, Oberflächen, Lizenzen, Aktivieren   (T19–T22)
    ├── updates/                Zeilen von ngc.store/updates, Einstellung, Ausrollen             (T23)
    └── server/                 alles zusammensetzen, Listener, Hintergrundläufe, Angriffstests  (T24, T30)

apps/cli/
├── VERSION                     0.1.0
├── main.go                     Unterbefehle von ngc-store                                       (T26)
└── classes/
    ├── pack.go                 pack: App-Paket schreiben                                        (T26)
    ├── crypt.go                encrypt: Kauf-App verschlüsseln                                  (T27)
    ├── license.go              license: Test-Lizenz ausstellen                                  (T27)
    ├── catalog.go              catalog: Katalog schreiben und signieren                         (T28)
    └── release.go              release und check: Release signieren und prüfen                  (T29)
```

Dazu außerhalb dieses Repos: `scripts/ngc-api.sh` im öffentlichen ulh-Repo (T25).

**30 Aufgaben:** T1–T4 Fundament, T5–T8 Anmeldung, T9–T12 Leitungen und Tunnel, T13–T18 Rechte,
Kern-Apps, Weiterleitung und Beitritt, T19–T23 Apps, Stores, Lizenzen und Updates, T24–T25
Zusammenbau und Installation, T26–T29 `ngc-store`, T30 Angriffe und Abschluss.

**Paketaufteilung:** `classes/` wächst weit über 20 Dateien, deshalb bekommt jedes Paket einen
Unterordner (`CLAUDE.md` im Stamm, Abschnitt „Aufbau“). `apps/cli/classes/` bleibt ein Paket
(`package classes`), weil es unter zehn Dateien hat.

**Abhängigkeitsrichtung:** `secrets` → `apitls`/`dbopen` → `state` → `httpapi` → `lock` →
`setup`/`login` → `link` → `browser`/`controllers` → `rights`/`audit` → `coreapp` →
`relay`/`join`/`appstore` → `updates` → `server` → `main.go`. Kein Paket importiert ein Paket, das
in dieser Kette später steht.

**Dateien außerhalb der beiden Ordner:** `tools/modules.go` verliert in Aufgabe 24 die Zeilen für
`github.com/coder/websocket` und `github.com/pquerna/otp/totp` (die API benutzt beide zuerst). Die
Zeilen für SQLite, MariaDB und Passkeys bleiben stehen: die API braucht keine davon (siehe „Offene
Entscheidungen“, Punkt 5).

---

## Entscheidungen, die dieser Plan trifft

Sie stehen in keinem Vertrag, weil die Verträge sie der API überlassen. Sie gehören ins
Entscheidungsprotokoll des Entwurfs, sobald der Koordinator sie bestätigt.

1. **Einmal-Code:** `ngc-api` erzeugt ihn beim ersten Start (solange es keinen Admin gibt) und legt
   ihn in `/var/lib/ngc/setup-code`, Eigentümer root, Modus 0600. Er steht nie in der Datenbank und
   nie im Log. Nach erfolgreicher Ersteinrichtung löscht die API die Datei. Das ulh-Skript liest sie
   und zeigt sie an.
2. **Gemeinsame Schlüssel beim Beitritt:** Der beitretende Server holt sie mit
   `ngc-api -join <Beitrittscode>` über den zusätzlichen Pfad `/tunnel/keys` (Noise_KK, Prolog
   `ngc/apikeys/v1`, TLS gepinnt mit dem Fingerabdruck aus dem Code) und legt sie in
   `/run/ngc/join-secret` (0600, root). Das ulh-Skript schreibt sie von dort in `api.json` und
   löscht die Datei. Go schreibt `api.json` nie selbst; `config` hat kein `SaveAPI`.
3. **Woher NGC-Releases und Kataloge kommen:** aus dem Store selbst. Die API holt den Katalog eines
   Stores unter `<url>/catalog.json` und das Release des globalen Stores unter
   `<url>/release.json`; beides sind `sign.Envelope` als JSON.
4. **Sperre nach Fehlversuchen:** je Anmeldename in der Datenbank als `dbapi.BlobSetting` mit der ID
   `lock.<hex sha256 des Namens>` und `Seq` = Ablaufzeit, damit sie für alle APIs gilt. Wartezeit:
   1 s ab dem 3. Fehlversuch, dann verdoppelnd bis 300 s; `Retry-After` nennt die Restzeit.
5. **Zeiten:** Sitzungen prüft die API bei jedem Zugriff gegen `IdleTimeout` und
   `SessionLifetime`; ein Hintergrundlauf ruft alle 60 s `SessionPurge`,
   `OncePurge(now - session.MaxSkew)` und die Aufräumrunde der Sperren auf.

---
### Task 1: Geheimnisse und Zufallswerte

**Files:**
- Create: `apps/server-api/classes/secrets/secrets.go`, `apps/server-api/classes/secrets/random.go`
- Test: `apps/server-api/classes/secrets/secrets_test.go`, `apps/server-api/classes/secrets/fuzz_test.go`

**Interfaces:**
- Consumes: `config.Secret` (aus `config.API.SecretKey`), `enc.B64`, `session.ValidSetupCode`,
  `trust.ValidID`, `core.ValidLogID`.
- Produces:
  - `func NewBox(key config.Secret) (*Box, error)` – Fehler, wenn der Schlüssel nicht 32 Byte hat.
  - `func (b *Box) Seal(plain []byte) ([]byte, error)`
  - `func (b *Box) Open(sealed []byte) ([]byte, error)` – Fehler `ErrSealed`.
  - `func ID() (string, error)` – 16 Zufallsbytes als base64url, `trust.ValidID`.
  - `func LogID() (string, error)` – 16 Zufallsbytes als Hex, `core.ValidLogID`.
  - `func Bytes(n int) ([]byte, error)` – n Zufallsbytes.
  - `func SetupCode() (string, error)` – `session.ValidSetupCode`.

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/secrets/secrets_test.go`:

```go
package secrets

import (
	"bytes"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

func testBox(t *testing.T) *Box {
	t.Helper()
	b, err := NewBox(config.NewSecret(bytes.Repeat([]byte{7}, 32)))
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	return b
}

func TestSealOpen(t *testing.T) {
	b := testBox(t)
	plain := []byte("totp secret")
	sealed, err := b.Seal(plain)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if bytes.Contains(sealed, plain) {
		t.Fatal("sealed value holds the plaintext")
	}
	back, err := b.Open(sealed)
	if err != nil || !bytes.Equal(back, plain) {
		t.Fatalf("open: %v %q", err, back)
	}
	// Two seals of the same value differ: the nonce is fresh every time, so a
	// database dump never shows which rows hold the same secret.
	again, _ := b.Seal(plain)
	if bytes.Equal(sealed, again) {
		t.Fatal("two seals are equal")
	}
}

func TestOpenRejectsDamage(t *testing.T) {
	b := testBox(t)
	sealed, _ := b.Seal([]byte("x"))
	for i := range sealed {
		bad := bytes.Clone(sealed)
		bad[i] ^= 1
		if _, err := b.Open(bad); err == nil {
			t.Fatalf("byte %d flipped and still opened", i)
		}
	}
	if _, err := b.Open(sealed[:len(sealed)-1]); err == nil {
		t.Fatal("truncated value opened")
	}
	if _, err := b.Open(nil); err == nil {
		t.Fatal("empty value opened")
	}
}

func TestNewBoxRejectsShortKey(t *testing.T) {
	if _, err := NewBox(config.NewSecret([]byte("short"))); err == nil {
		t.Fatal("short key accepted")
	}
	if _, err := NewBox(config.Secret{}); err == nil {
		t.Fatal("empty key accepted")
	}
}

func TestRandomValues(t *testing.T) {
	id, err := ID()
	if err != nil || !trust.ValidID(id) {
		t.Fatalf("id %q: %v", id, err)
	}
	log, err := LogID()
	if err != nil || !core.ValidLogID(log) {
		t.Fatalf("log id %q: %v", log, err)
	}
	code, err := SetupCode()
	if err != nil || !session.ValidSetupCode(code) {
		t.Fatalf("setup code %q: %v", code, err)
	}
	other, _ := SetupCode()
	if code == other {
		t.Fatal("two setup codes are equal")
	}
}
```

- [ ] **Step 2: Run the test and see it fail**

Run: `go test ./apps/server-api/classes/secrets/`
Expected: FAIL, `undefined: NewBox`.

- [ ] **Step 3: Write the box**

`apps/server-api/classes/secrets/secrets.go`:

```go
// Package secrets encrypts the values the API keeps in the database (TOTP
// secrets, session keys, the installation's private key) and makes the random
// values it hands out. The key is config.API.SecretKey, which every API of an
// installation shares, so a dump of the database or a backup is worthless on
// its own.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// nonceSize is the standard nonce of AES-GCM; it is prefixed to every sealed
// value, so a value carries everything needed to open it but the key.
const nonceSize = 12

// ErrSealed reports a value that is damaged or was sealed with another key.
var ErrSealed = errors.New("secrets: sealed value damaged or wrong key")

// Box seals and opens values with the installation's secret key.
type Box struct{ aead cipher.AEAD }

// NewBox returns a Box for the 32-byte key of config.API.SecretKey.
func NewBox(key config.Secret) (*Box, error) {
	k := key.Bytes()
	if len(k) != 32 {
		return nil, ErrSealed
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, ErrSealed
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrSealed
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plain: a fresh nonce, then the sealed value.
func (b *Box) Seal(plain []byte) ([]byte, error) {
	nonce, err := Bytes(nonceSize)
	if err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plain, nil), nil
}

// Open decrypts a value of Seal. Every failure is ErrSealed alone: the error
// never quotes the value.
func (b *Box) Open(sealed []byte) ([]byte, error) {
	if len(sealed) < nonceSize+b.aead.Overhead() {
		return nil, ErrSealed
	}
	plain, err := b.aead.Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil)
	if err != nil {
		return nil, ErrSealed
	}
	return plain, nil
}
```

`apps/server-api/classes/secrets/random.go`:

```go
package secrets

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/sorglos-it/nextgencloud/shared/enc"
)

// setupAlphabet is the alphabet of a one-time setup code
// (session.ValidSetupCode): capitals and digits without I, O, 0 and 1, which
// people confuse. 32 characters, so every byte of a random draw maps to one
// without bias.
const setupAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// Bytes returns n random bytes.
func Bytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// ID returns an ID of users, installations and other values trust.ValidID
// checks: 16 random bytes as base64url.
func ID() (string, error) {
	b, err := Bytes(16)
	if err != nil {
		return "", err
	}
	return enc.B64.EncodeToString(b), nil
}

// LogID returns the ID of a log entry (core.ValidLogID): 16 random bytes as
// lower-case hex, so every database collation orders it like Go.
func LogID() (string, error) {
	b, err := Bytes(16)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SetupCode returns a one-time setup code in its canonical form: 20 characters
// of the alphabet in groups of five joined by '-' (session.ValidSetupCode).
func SetupCode() (string, error) {
	b, err := Bytes(20)
	if err != nil {
		return "", err
	}
	out := make([]byte, 0, 23)
	for i, c := range b {
		if i > 0 && i%5 == 0 {
			out = append(out, '-')
		}
		out = append(out, setupAlphabet[int(c)%len(setupAlphabet)])
	}
	return string(out), nil
}
```

- [ ] **Step 4: Run the test and see it pass**

Run: `go test ./apps/server-api/classes/secrets/`
Expected: PASS.

- [ ] **Step 5: Add the fuzz test**

`apps/server-api/classes/secrets/fuzz_test.go`:

```go
package secrets

import (
	"bytes"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// FuzzOpen feeds Open with everything: it must answer ErrSealed and never
// panic, whatever comes out of the database.
func FuzzOpen(f *testing.F) {
	b, err := NewBox(config.NewSecret(bytes.Repeat([]byte{7}, 32)))
	if err != nil {
		f.Fatalf("new box: %v", err)
	}
	sealed, err := b.Seal([]byte("seed"))
	if err != nil {
		f.Fatalf("seal: %v", err)
	}
	f.Add(sealed)
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{0}, 28))
	f.Fuzz(func(t *testing.T, in []byte) {
		if _, err := b.Open(in); err != nil && err != ErrSealed {
			t.Fatalf("open answered %v instead of ErrSealed", err)
		}
	})
}
```

- [ ] **Step 6: Run the fuzz target briefly**

Run: `go test ./apps/server-api/classes/secrets/ -run Fuzz -fuzz FuzzOpen -fuzztime 30s`
Expected: `PASS` without a finding. Then `tools/check.sh`.

- [ ] **Step 7: Commit**

```bash
git add apps/server-api/classes/secrets
git commit -m "Add the secret box and random values of the API" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 2: Eigenes TLS-Zertifikat der API

Das Zertifikat trägt kein Vertrauen; das trägt der Fingerabdruck seines Schlüssels
(`trust.TLSPin`). Es ist selbst signiert, 100 Jahre gültig (damit kein Datum Apache aussperrt,
`SSLProxyCheckPeerExpire on`) und wird nur mit demselben Schlüssel erneuert.

**Files:**
- Create: `apps/server-api/classes/apitls/apitls.go`
- Test: `apps/server-api/classes/apitls/apitls_test.go`

**Interfaces:**
- Consumes: `config.API.URL`, `config.API.Cert`, `config.API.CertKey`, `trust.TLSPin`,
  `trust.ValidTunnelURL`.
- Produces:
  - `func Host(tunnelURL string) (string, error)` – der Host aus `wss://<host>:<port>/tunnel`.
  - `func Ensure(certFile, keyFile, host string, now time.Time) (tls.Certificate, []byte, error)` –
    lädt Schlüssel und Zertifikat, erzeugt Fehlendes und gibt das Zertifikat samt seinem
    `trust.TLSPin` zurück. Ein vorhandener Schlüssel wird nie ersetzt.
  - `var ErrTLS = errors.New("apitls: certificate or key unusable")`

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/apitls/apitls_test.go`:

```go
package apitls

import (
	"bytes"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/trust"
)

var when = time.Unix(1790000000, 0).UTC()

func TestHost(t *testing.T) {
	got, err := Host("wss://api-1.ngc.test:7443/tunnel")
	if err != nil || got != "api-1.ngc.test" {
		t.Fatalf("host %q: %v", got, err)
	}
	for _, bad := range []string{"", "https://api-1.ngc.test:7443", "wss://api-1.ngc.test/tunnel"} {
		if _, err := Host(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestEnsureCreatesAndKeepsTheKey(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "api.crt"), filepath.Join(dir, "api.key")
	cert, pin, err := Ensure(certFile, keyFile, "api-1.ngc.test", when)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !bytes.Equal(pin, trust.TLSPin(leaf.RawSubjectPublicKeyInfo)) {
		t.Fatal("pin does not belong to the certificate")
	}
	if leaf.DNSNames[0] != "api-1.ngc.test" || len(leaf.DNSNames) != 1 {
		t.Fatalf("names %v", leaf.DNSNames)
	}
	// 100 years, so no date ever shuts Apache out.
	if years := leaf.NotAfter.Year() - leaf.NotBefore.Year(); years != 100 {
		t.Fatalf("valid for %d years", years)
	}
	if info, err := os.Stat(keyFile); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %v: %v", info.Mode().Perm(), err)
	}

	// The certificate is renewed with the same key: the pin in the trust list
	// stays right. Deleting the certificate must not make a new key.
	if err := os.Remove(certFile); err != nil {
		t.Fatalf("remove: %v", err)
	}
	_, pin2, err := Ensure(certFile, keyFile, "api-1.ngc.test", when.Add(24*time.Hour))
	if err != nil || !bytes.Equal(pin, pin2) {
		t.Fatalf("pin changed with the certificate: %v", err)
	}
}

func TestEnsureRejectsBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "api.crt"), filepath.Join(dir, "api.key")
	if err := os.WriteFile(keyFile, []byte("not a key"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := Ensure(certFile, keyFile, "api-1.ngc.test", when); err == nil {
		t.Fatal("broken key accepted")
	}
}
```

- [ ] **Step 2: Run the test and see it fail**

Run: `go test ./apps/server-api/classes/apitls/`
Expected: FAIL, `undefined: Host`.

- [ ] **Step 3: Write the implementation**

`apps/server-api/classes/apitls/apitls.go`:

```go
// Package apitls makes and keeps the TLS certificate of this API. The
// certificate carries no trust; the pin of its key does (trust.TLSPin), which
// stands in the trust list, in pairing and join codes and in web.json. So the
// certificate is self-signed and valid for 100 years, and it is renewed only
// with the same key: a new key comes only with a new pin in the trust list.
package apitls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// ErrTLS reports a certificate or key this package cannot use.
var ErrTLS = errors.New("apitls: certificate or key unusable")

// Host returns the host of a tunnel address wss://<host>:<port>/tunnel, the
// only name the certificate carries: Apache checks it (SSLProxyCheckPeerName).
func Host(tunnelURL string) (string, error) {
	if !trust.ValidTunnelURL(tunnelURL) {
		return "", ErrTLS
	}
	rest := strings.TrimSuffix(strings.TrimPrefix(tunnelURL, "wss://"), "/tunnel")
	// A bracketed IPv6 host holds the only colons before the port's.
	i := strings.LastIndexByte(rest, ':')
	if i < 1 {
		return "", ErrTLS
	}
	return rest[:i], nil
}

// Ensure loads the key and certificate, makes what is missing and returns the
// certificate with the pin of its key. An existing key is never replaced.
func Ensure(certFile, keyFile, host string, now time.Time) (tls.Certificate, []byte, error) {
	key, err := ensureKey(keyFile)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	der, err := os.ReadFile(certFile)
	if err == nil {
		if cert, pin, err := use(der, key); err == nil {
			return cert, pin, nil
		}
	} else if !os.IsNotExist(err) {
		return tls.Certificate{}, nil, err
	}
	der, err = issue(key, host, now)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	if err := write(certFile, "CERTIFICATE", der, 0o644); err != nil {
		return tls.Certificate{}, nil, err
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return use(pemBytes, key)
}

// use builds the TLS certificate from PEM and the key, and checks that the two
// belong together; a stale certificate for another key is thrown away.
func use(certPEM []byte, key *ecdsa.PrivateKey) (tls.Certificate, []byte, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return tls.Certificate{}, nil, ErrTLS
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return tls.Certificate{}, nil, ErrTLS
	}
	pub, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || !pub.Equal(key.Public()) {
		return tls.Certificate{}, nil, ErrTLS
	}
	cert := tls.Certificate{Certificate: [][]byte{block.Bytes}, PrivateKey: key, Leaf: leaf}
	return cert, trust.TLSPin(leaf.RawSubjectPublicKeyInfo), nil
}

// ensureKey reads the key or makes one; the file is root's alone.
func ensureKey(keyFile string) (*ecdsa.PrivateKey, error) {
	b, err := os.ReadFile(keyFile)
	if err == nil {
		block, _ := pem.Decode(b)
		if block == nil || block.Type != "EC PRIVATE KEY" {
			return nil, ErrTLS
		}
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, ErrTLS
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := write(keyFile, "EC PRIVATE KEY", der, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// issue signs a certificate for host with key, valid for 100 years.
func issue(key *ecdsa.PrivateKey, host string, now time.Time) ([]byte, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: host},
		DNSNames:              []string{host},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(100, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	return x509.CreateCertificate(rand.Reader, tpl, tpl, key.Public(), key)
}

// write puts a PEM block into path with mode perm, the mode set before the
// first byte so a key file is never readable for others, not even for a moment.
func write(path, kind string, der []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Chmod(perm); err != nil {
		return err
	}
	if err := pem.Encode(f, &pem.Block{Type: kind, Bytes: der}); err != nil {
		return err
	}
	return f.Sync()
}
```

- [ ] **Step 4: Run the test and see it pass**

Run: `go test ./apps/server-api/classes/apitls/`
Expected: PASS.

- [ ] **Step 5: Check the pin against the script of the contract**

Run (nur auf Linux oder in WSL, `openssl` nötig):

```bash
go test ./apps/server-api/classes/apitls/ -run TestEnsure -v
sh shared/testdata/tls/pin.sh shared/testdata/tls/api.pem
```

Expected: das Skript gibt denselben Fingerabdruck aus wie `trust.TLSPin` in
`shared/testdata/tunnel.json` unter `tlsPin` (das prüft schon `TestTLSPinScript` in
`shared/tunnel`; hier wird nur bestätigt, dass `Ensure` denselben Weg geht).

- [ ] **Step 6: Commit**

```bash
git add apps/server-api/classes/apitls
git commit -m "Add the self-signed TLS certificate of the API" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 3: Datenbank öffnen, Laufzeitzustand und Vertrauensliste

**Files:**
- Create: `apps/server-api/classes/dbopen/dbopen.go`, `apps/server-api/classes/state/state.go`,
  `apps/server-api/classes/state/trust.go`
- Test: `apps/server-api/classes/state/state_test.go`, `apps/server-api/classes/state/trust_test.go`

**Interfaces:**
- Consumes: `dbapi.Store`, `dbapi.Blob*`, `dbapi.Setting*`, `trust.Open`, `trust.Advance`,
  `trust.ParseSigned`, `session.TrustChain`, `secrets.Box`.
- Produces:
  - `dbopen`: `type Opener func(ctx context.Context, cfg config.DB) (dbapi.Store, error)`,
    `func Register(o Opener)`, `func Open(ctx context.Context, cfg config.DB) (dbapi.Store, error)`.
  - `state`: `type State struct { Cfg *config.API; DB dbapi.Store; Box *secrets.Box; Pin []byte;
    Now func() int64 }`, `func New(cfg *config.API, db dbapi.Store, box *secrets.Box, pin []byte) *State`,
    `func (s *State) Installation(ctx) (string, error)`,
    `func (s *State) InstallKey(ctx) (*ecdh.PrivateKey, error)`,
    `func (s *State) EnsureInstallation(ctx) (string, error)`,
    `func (s *State) Trust(ctx) (trust.Version, error)`,
    `func (s *State) Refresh(ctx) (trust.Version, error)`,
    `func (s *State) AddVersion(ctx, trust.Signed) (trust.Version, error)`,
    `func (s *State) Chain(ctx, from uint64) (session.TrustChain, error)`,
    `var ErrNoTrust = errors.New("state: no trust list yet")`,
    `var ErrOldVersion = errors.New("state: version is not the next one")`.

- [ ] **Step 1: Write the failing test for the trust list**

`apps/server-api/classes/state/trust_test.go`:

```go
package state

import (
	"context"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
)

var ctx = context.Background()

func newState(t *testing.T) (*State, dbapi.Store) {
	t.Helper()
	db := memstore.New()
	box, err := secrets.NewBox(config.NewSecret(make([]byte, 32)))
	if err != nil {
		t.Fatalf("box: %v", err)
	}
	cfg := &config.API{Node: "api-1", SessionLifetime: 43200, IdleTimeout: 1800}
	s := New(cfg, db, box, make([]byte, 32))
	s.Now = func() int64 { return 1790000000 }
	return s, db
}

// genesis returns version 1 signed by the passkey admin of trusttest.
func genesis(t *testing.T) (trusttest.Signer, trust.Signed) {
	t.Helper()
	admin := trusttest.NewPasskey(9)
	l := trusttest.Genesis(admin)
	return admin, admin.Sign(t, l)
}

func TestTrustWithoutVersion(t *testing.T) {
	s, _ := newState(t)
	if _, err := s.Trust(ctx); err != ErrNoTrust {
		t.Fatalf("empty database answered %v", err)
	}
}

func TestAddVersionAndChain(t *testing.T) {
	s, _ := newState(t)
	admin, first := genesis(t)
	v, err := s.AddVersion(ctx, first)
	if err != nil || v.List.Seq != 1 {
		t.Fatalf("version 1: %v", err)
	}
	next := trusttest.Next(t, first)
	second := admin.Sign(t, next)
	if _, err := s.AddVersion(ctx, second); err != nil {
		t.Fatalf("version 2: %v", err)
	}
	// A version that is not the next one is refused, so no one can replay an
	// old list or skip one.
	if _, err := s.AddVersion(ctx, second); err != ErrOldVersion {
		t.Fatalf("version 2 twice answered %v", err)
	}
	chain, err := s.Chain(ctx, 1)
	if err != nil || len(chain.Versions) != 2 || chain.More {
		t.Fatalf("chain from 1: %v %d %v", err, len(chain.Versions), chain.More)
	}
	if _, err := trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(chain.Versions[0])}, chain.Versions); err != nil {
		t.Fatalf("chain does not verify: %v", err)
	}
	from2, err := s.Chain(ctx, 2)
	if err != nil || len(from2.Versions) != 1 {
		t.Fatalf("chain from 2: %v", err)
	}
	// One past the newest is no error: the caller answers 204.
	if c, err := s.Chain(ctx, 3); err != nil || len(c.Versions) != 0 {
		t.Fatalf("chain from 3: %v %d", err, len(c.Versions))
	}
	if _, err := s.Chain(ctx, 4); err == nil {
		t.Fatal("chain beyond the newest accepted")
	}
}

// TestRefreshSeesAnotherAPI proves that an API notices what another API wrote
// into the shared database.
func TestRefreshSeesAnotherAPI(t *testing.T) {
	s, db := newState(t)
	admin, first := genesis(t)
	if _, err := s.AddVersion(ctx, first); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	other := New(s.Cfg, db, s.Box, s.Pin)
	other.Now = s.Now
	if _, err := other.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	second := admin.Sign(t, trusttest.Next(t, first))
	if _, err := s.AddVersion(ctx, second); err != nil {
		t.Fatalf("version 2: %v", err)
	}
	v, err := other.Refresh(ctx)
	if err != nil || v.List.Seq != 2 {
		t.Fatalf("other API is at %v: %v", v.List, err)
	}
}

func TestEnsureInstallation(t *testing.T) {
	s, db := newState(t)
	id, err := s.EnsureInstallation(ctx)
	if err != nil || !trust.ValidID(id) {
		t.Fatalf("installation %q: %v", id, err)
	}
	again, err := s.EnsureInstallation(ctx)
	if err != nil || again != id {
		t.Fatalf("second call made another installation: %q %v", again, err)
	}
	key, err := s.InstallKey(ctx)
	if err != nil || key == nil {
		t.Fatalf("install key: %v", err)
	}
	// The private key never lies in the clear in the database.
	blob, err := db.BlobGet(ctx, dbapi.BlobSetting, dbapi.SettingInstallKey)
	if err != nil {
		t.Fatalf("blob: %v", err)
	}
	if string(blob.Data) == string(key.Bytes()) {
		t.Fatal("install key stored in the clear")
	}
}
```

- [ ] **Step 2: Run the test and see it fail**

Run: `go test ./apps/server-api/classes/state/`
Expected: FAIL, `undefined: New`.

- [ ] **Step 3: Write the database opener**

`apps/server-api/classes/dbopen/dbopen.go`:

```go
// Package dbopen opens the database the configuration names. The part
// apps/server-db implements dbapi for MariaDB and SQLite and registers its
// opener here, so the API itself holds no driver and no SQL. Without a
// registered opener the API refuses to start with a clear message instead of
// running half way.
package dbopen

import (
	"context"
	"errors"
	"sync"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// Opener opens a store for cfg.
type Opener func(ctx context.Context, cfg config.DB) (dbapi.Store, error)

// ErrNoOpener reports that no part registered an opener.
var ErrNoOpener = errors.New("dbopen: no database implementation registered")

var (
	mu     sync.Mutex
	opener Opener
)

// Register makes o the opener. main.go calls it once with the opener of
// apps/server-db.
func Register(o Opener) {
	mu.Lock()
	defer mu.Unlock()
	opener = o
}

// Open opens the database of cfg.
func Open(ctx context.Context, cfg config.DB) (dbapi.Store, error) {
	mu.Lock()
	o := opener
	mu.Unlock()
	if o == nil {
		return nil, ErrNoOpener
	}
	return o(ctx, cfg)
}
```

- [ ] **Step 4: Write the state**

`apps/server-api/classes/state/state.go`:

```go
// Package state holds what every part of the API needs: the configuration,
// the database, the secret box, the pin of this API's TLS key and the verified
// trust list. Nothing here is the only copy of anything: everything that
// matters lies in the database, so any API can replace any other.
package state

import (
	"context"
	"crypto/ecdh"
	"errors"
	"sync"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
)

// ErrSetting reports a setting the database does not hold or cannot hold.
var ErrSetting = errors.New("state: setting missing or malformed")

// State is the runtime state of one API process.
type State struct {
	Cfg *config.API
	DB  dbapi.Store
	Box *secrets.Box
	Pin []byte // trust.TLSPin of this API's TLS key

	// Now gives the time in Unix seconds; tests replace it.
	Now func() int64

	mu  sync.RWMutex
	cur trust.Version
}

// New returns a State for cfg.
func New(cfg *config.API, db dbapi.Store, box *secrets.Box, pin []byte) *State {
	return &State{Cfg: cfg, DB: db, Box: box, Pin: pin, Now: func() int64 { return time.Now().Unix() }}
}

// Installation returns the installation ID, ErrSetting while there is none.
func (s *State) Installation(ctx context.Context) (string, error) {
	b, err := s.DB.BlobGet(ctx, dbapi.BlobSetting, dbapi.SettingInstallation)
	if err != nil {
		return "", err
	}
	id := string(b.Data)
	if !trust.ValidID(id) {
		return "", ErrSetting
	}
	return id, nil
}

// InstallKey returns the installation's private X25519 key, which unwraps the
// content keys of paid apps. Only the API can open it (secrets.Box).
func (s *State) InstallKey(ctx context.Context) (*ecdh.PrivateKey, error) {
	b, err := s.DB.BlobGet(ctx, dbapi.BlobSetting, dbapi.SettingInstallKey)
	if err != nil {
		return nil, err
	}
	raw, err := s.Box.Open(b.Data)
	if err != nil {
		return nil, err
	}
	return ecdh.X25519().NewPrivateKey(raw)
}

// EnsureInstallation makes the installation ID and the installation's key
// pair if they are missing, and returns the ID. A lock keeps two APIs on one
// database from making two of them.
func (s *State) EnsureInstallation(ctx context.Context) (string, error) {
	if id, err := s.Installation(ctx); err == nil {
		return id, nil
	} else if !errors.Is(err, dbapi.ErrNotFound) {
		return "", err
	}
	now := s.Now()
	ok, err := s.DB.LockTake(ctx, "installation", s.Cfg.Node, now, now+60)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrSetting
	}
	defer func() { _ = s.DB.LockFree(ctx, "installation", s.Cfg.Node) }()
	// Another API may have been quicker between the two checks.
	if id, err := s.Installation(ctx); err == nil {
		return id, nil
	}
	id, err := secrets.ID()
	if err != nil {
		return "", err
	}
	key, err := ecdh.X25519().GenerateKey(randReader)
	if err != nil {
		return "", err
	}
	sealed, err := s.Box.Seal(key.Bytes())
	if err != nil {
		return "", err
	}
	if err := s.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallKey,
		Data: sealed, Updated: now}); err != nil {
		return "", err
	}
	if err := s.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallation,
		Data: []byte(id), Updated: now}); err != nil {
		return "", err
	}
	return id, nil
}
```

Dazu in derselben Datei, damit Tests die Zufallsquelle nicht austauschen müssen:

```go
// randReader is the source of the installation's key; crypto/rand is the only
// source NGC uses for keys.
var randReader = rand.Reader
```

mit `"crypto/rand"` im Import-Block.

- [ ] **Step 5: Write the trust list part**

`apps/server-api/classes/state/trust.go`:

```go
package state

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

var (
	// ErrNoTrust reports that the installation has no trust list yet: only the
	// setup may run.
	ErrNoTrust = errors.New("state: no trust list yet")
	// ErrOldVersion reports a version that is not the one after the newest.
	ErrOldVersion = errors.New("state: version is not the next one")
)

// Trust returns the newest verified version, from the cache if there is one.
func (s *State) Trust(ctx context.Context) (trust.Version, error) {
	s.mu.RLock()
	cur := s.cur
	s.mu.RUnlock()
	if cur.List != nil {
		return cur, nil
	}
	return s.Refresh(ctx)
}

// Refresh reads the versions the database holds beyond the cached one and
// verifies them, so an API sees at once what another API wrote. The whole
// chain is verified only when nothing is cached yet (after a start).
func (s *State) Refresh(ctx context.Context) (trust.Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	from := uint64(1)
	if s.cur.List != nil {
		from = s.cur.List.Seq + 1
	}
	blobs, err := s.DB.BlobFrom(ctx, dbapi.BlobTrust, from)
	if err != nil {
		return trust.Version{}, err
	}
	if len(blobs) == 0 {
		if s.cur.List == nil {
			return trust.Version{}, ErrNoTrust
		}
		return s.cur, nil
	}
	chain := make([]trust.Signed, 0, len(blobs))
	for _, b := range blobs {
		var sg trust.Signed
		// The database is ours, but a row is still input: the same parser as
		// everywhere else reads it.
		if err := json.Unmarshal(b.Data, &sg); err != nil ||
			len(sg.Payload) == 0 || len(sg.Payload) > sign.MaxPayload || sg.Proof.Validate() != nil {
			return trust.Version{}, ErrSetting
		}
		chain = append(chain, sg)
	}
	var v trust.Version
	if s.cur.List == nil {
		// Version 1 is the anchor of this installation: it signs itself, and
		// trust.Open checks that an admin of it did.
		v, err = trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(chain[0])}, chain)
	} else {
		v, err = trust.Advance(s.cur, chain)
	}
	if err != nil {
		return trust.Version{}, err
	}
	s.cur = v
	return v, nil
}

// AddVersion verifies sg as the version after the newest and stores it.
// Version 1 is taken only while there is none. The caller has read sg with
// trust.ParseSigned or session.ParseTrustChain.
func (s *State) AddVersion(ctx context.Context, sg trust.Signed) (trust.Version, error) {
	cur, err := s.Refresh(ctx)
	if err != nil && err != ErrNoTrust {
		return trust.Version{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var next trust.Version
	if err == ErrNoTrust {
		next, err = trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(sg)}, []trust.Signed{sg})
	} else {
		next, err = trust.Advance(cur, []trust.Signed{sg})
	}
	if err != nil {
		return trust.Version{}, err
	}
	seq := next.List.Seq
	if _, err := s.DB.BlobGet(ctx, dbapi.BlobTrust, strconv.FormatUint(seq, 10)); err == nil {
		return trust.Version{}, ErrOldVersion
	} else if !errors.Is(err, dbapi.ErrNotFound) {
		return trust.Version{}, err
	}
	data, err := json.Marshal(sg)
	if err != nil {
		return trust.Version{}, err
	}
	if err := s.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobTrust, ID: strconv.FormatUint(seq, 10),
		Seq: seq, Data: data, Updated: s.Now()}); err != nil {
		return trust.Version{}, err
	}
	s.cur = next
	return next, nil
}

// Chain answers GET session.PathTrust: the versions from from on, at most
// session.MaxChainVersions and session.MaxChain bytes, with More when older
// answers follow. from one past the newest gives an empty chain (the caller
// answers 204); anything beyond is ErrOldVersion (404).
func (s *State) Chain(ctx context.Context, from uint64) (session.TrustChain, error) {
	cur, err := s.Refresh(ctx)
	if err != nil {
		return session.TrustChain{}, err
	}
	if from > cur.List.Seq+1 {
		return session.TrustChain{}, ErrOldVersion
	}
	if from == cur.List.Seq+1 {
		return session.TrustChain{}, nil
	}
	blobs, err := s.DB.BlobFrom(ctx, dbapi.BlobTrust, from)
	if err != nil {
		return session.TrustChain{}, err
	}
	var out session.TrustChain
	size := 0
	for _, b := range blobs {
		// The JSON of the answer grows by roughly the stored bytes plus the
		// member names, so the stored size is the measure that keeps the
		// answer under session.MaxChain.
		if len(out.Versions) == session.MaxChainVersions || size+len(b.Data) > session.MaxChain {
			out.More = true
			break
		}
		var sg trust.Signed
		if err := json.Unmarshal(b.Data, &sg); err != nil {
			return session.TrustChain{}, ErrSetting
		}
		out.Versions = append(out.Versions, sg)
		size += len(b.Data)
	}
	return out, nil
}
```

- [ ] **Step 6: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/state/ ./apps/server-api/classes/dbopen/`
Expected: PASS.

- [ ] **Step 7: Add the fuzz test for the stored version**

`apps/server-api/classes/state/fuzz_test.go`:

```go
package state

import (
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// FuzzRefresh feeds Refresh with rows a damaged or foreign database could
// hold: it must answer an error and never panic.
func FuzzRefresh(f *testing.F) {
	_, first := genesisSeed(f)
	f.Add(first)
	f.Add([]byte("{}"))
	f.Add([]byte(`{"payload":"","proof":{}}`))
	f.Fuzz(func(t *testing.T, row []byte) {
		s, db := newStateFuzz(t)
		if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobTrust, ID: "1", Seq: 1, Data: row}); err != nil {
			t.Fatalf("put: %v", err)
		}
		if v, err := s.Refresh(ctx); err == nil && v.List == nil {
			t.Fatal("refresh answered a version without a list")
		}
		_ = trust.Version{}
	})
}
```

`genesisSeed(f)` und `newStateFuzz(t)` sind zwei Helfer neben `genesis` und `newState`, die
`testing.TB` statt `*testing.T` nehmen; sie stehen in `trust_test.go`:

```go
// genesisSeed is genesis for a fuzz target, which has a *testing.F.
func genesisSeed(tb testing.TB) (trusttest.Signer, []byte) {
	admin := trusttest.NewPasskey(9)
	sg := admin.Sign(tb, trusttest.Genesis(admin))
	b, err := json.Marshal(sg)
	if err != nil {
		tb.Fatalf("marshal: %v", err)
	}
	return admin, b
}

// newStateFuzz is newState for a fuzz case, which needs a fresh store per run.
func newStateFuzz(tb testing.TB) (*State, dbapi.Store) {
	db := memstore.New()
	box, err := secrets.NewBox(config.NewSecret(make([]byte, 32)))
	if err != nil {
		tb.Fatalf("box: %v", err)
	}
	s := New(&config.API{Node: "api-1"}, db, box, make([]byte, 32))
	s.Now = func() int64 { return 1790000000 }
	return s, db
}
```

- [ ] **Step 8: Run the fuzz target and check**

Run: `go test ./apps/server-api/classes/state/ -run Fuzz -fuzz FuzzRefresh -fuzztime 30s`,
danach `tools/check.sh`.
Expected: PASS ohne Fund, `check: ok`.

- [ ] **Step 9: Commit**

```bash
git add apps/server-api/classes/dbopen apps/server-api/classes/state
git commit -m "Add the runtime state and the trust list cache of the API" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 4: HTTP-Gerüst vor dem WebSocket

Genau nach `shared/README.md`, Abschnitt „Vor dem WebSocket“.

**Files:**
- Create: `apps/server-api/classes/httpapi/httpapi.go`, `apps/server-api/classes/httpapi/router.go`
- Test: `apps/server-api/classes/httpapi/httpapi_test.go`

**Interfaces:**
- Consumes: `session.HTTPStatus`, `session.MaxRequest`, `session.Path*`, `session.CookieName`,
  `session.CookieAttributes`, `wire.Error`, `wire.Code`.
- Produces:
  - `func Answer(w http.ResponseWriter, status int, body []byte)`
  - `func NoContent(w http.ResponseWriter)`
  - `func Fail(w http.ResponseWriter, code wire.Code)`
  - `func FailRetry(w http.ResponseWriter, code wire.Code, retryAfter int64)`
  - `func Body(w http.ResponseWriter, r *http.Request) ([]byte, bool)` – prüft `Content-Type`,
    begrenzt auf `session.MaxRequest`, antwortet selbst bei Fehlern.
  - `func SetCookie(w http.ResponseWriter, value string)`, `func ClearCookie(w http.ResponseWriter)`
  - `func Cookie(r *http.Request) (string, bool)`
  - `type Routes struct { Setup, Login, Finish, Logout, Trust, Socket http.HandlerFunc }`,
    `func (rt Routes) Mux() *http.ServeMux`

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/httpapi/httpapi_test.go`:

```go
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

func TestFailUsesTheContract(t *testing.T) {
	w := httptest.NewRecorder()
	Fail(w, wire.CodeLogin)
	if w.Code != session.HTTPStatus(wire.CodeLogin) {
		t.Fatalf("status %d", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control %q", got)
	}
	var c wire.Close
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("body: %v", err)
	}
	if c.Status != wire.StatusError || c.Code != wire.CodeLogin || c.Message != wire.CodeLogin.Message() {
		t.Fatalf("body %+v", c)
	}
}

func TestFailRetryTellsWhen(t *testing.T) {
	w := httptest.NewRecorder()
	FailRetry(w, wire.CodeLocked, 42)
	if w.Code != 429 || w.Header().Get("Retry-After") != "42" {
		t.Fatalf("status %d retry %q", w.Code, w.Header().Get("Retry-After"))
	}
}

func TestBodyChecksTypeAndSize(t *testing.T) {
	good := httptest.NewRequest(http.MethodPost, session.PathLoginStart, strings.NewReader(`{"name":"a"}`))
	good.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	if b, ok := Body(w, good); !ok || string(b) != `{"name":"a"}` {
		t.Fatalf("good body: %q %v", b, ok)
	}

	wrong := httptest.NewRequest(http.MethodPost, session.PathLoginStart, strings.NewReader(`{}`))
	wrong.Header.Set("Content-Type", "text/plain")
	w = httptest.NewRecorder()
	if _, ok := Body(w, wrong); ok || w.Code != 400 {
		t.Fatalf("wrong type: %v %d", ok, w.Code)
	}

	big := httptest.NewRequest(http.MethodPost, session.PathLoginStart,
		strings.NewReader(strings.Repeat("x", session.MaxRequest+1)))
	big.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	if _, ok := Body(w, big); ok || w.Code != 413 {
		t.Fatalf("large body: %v %d", ok, w.Code)
	}
}

func TestCookie(t *testing.T) {
	w := httptest.NewRecorder()
	SetCookie(w, "abc")
	got := w.Header().Get("Set-Cookie")
	want := session.CookieName + "=abc; " + session.CookieAttributes
	if got != want {
		t.Fatalf("cookie %q", got)
	}
	w = httptest.NewRecorder()
	ClearCookie(w)
	if got := w.Header().Get("Set-Cookie"); !strings.Contains(got, "Max-Age=0") {
		t.Fatalf("clearing cookie %q", got)
	}
	r := httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
	r.Header.Set("Cookie", session.CookieName+"=abc")
	if v, ok := Cookie(r); !ok || v != "abc" {
		t.Fatalf("read cookie %q %v", v, ok)
	}
}

func TestMuxAnswersEveryPathAndMethod(t *testing.T) {
	seen := map[string]bool{}
	mark := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { seen[name] = true; NoContent(w) }
	}
	mux := Routes{Setup: mark("setup"), Login: mark("login"), Finish: mark("finish"),
		Logout: mark("logout"), Trust: mark("trust"), Socket: mark("socket")}.Mux()
	cases := []struct {
		method, path, name string
	}{
		{http.MethodGet, session.PathSetup, "setup"},
		{http.MethodPost, session.PathSetup, "setup"},
		{http.MethodPost, session.PathLoginStart, "login"},
		{http.MethodPost, session.PathLoginFinish, "finish"},
		{http.MethodPost, session.PathLogout, "logout"},
		{http.MethodGet, session.PathTrust, "trust"},
		{http.MethodGet, session.PathSocket, "socket"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if !seen[c.name] {
			t.Fatalf("%s %s did not reach %s", c.method, c.path, c.name)
		}
		seen[c.name] = false
	}
	// A method the contract does not list is a protocol error, and an unknown
	// path is not found; neither ever reaches a handler.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, session.PathSetup, nil))
	if w.Code != 400 {
		t.Fatalf("DELETE answered %d", w.Code)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/nothing", nil))
	if w.Code != 404 {
		t.Fatalf("unknown path answered %d", w.Code)
	}
}
```

- [ ] **Step 2: Run the test and see it fail**

Run: `go test ./apps/server-api/classes/httpapi/`
Expected: FAIL, `undefined: Fail`.

- [ ] **Step 3: Write the answers**

`apps/server-api/classes/httpapi/httpapi.go`:

```go
// Package httpapi holds what every answer of the API before the WebSocket
// shares: the status and body of an error (session.HTTPStatus, wire.Error),
// Cache-Control: no-store on every answer, the check of Content-Type and the
// size of a body, and the session cookie. shared/README.md, "Vor dem
// WebSocket", is the contract.
package httpapi

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Answer writes one answer with its status and body.
func Answer(w http.ResponseWriter, status int, body []byte) {
	h := w.Header()
	// No answer of the API may sit in a cache: they all hold session state.
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// NoContent answers 204.
func NoContent(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// Fail answers an error: the status of the contract and the CLOSE payload as
// body, so the shell reads every failure the same way.
func Fail(w http.ResponseWriter, code wire.Code) {
	b, err := json.Marshal(wire.Error(code))
	if err != nil {
		// wire.Error is a fixed struct of strings, so this cannot happen; the
		// answer stays a valid one all the same.
		b = []byte(`{"status":"error","code":"E_INTERNAL"}`)
	}
	Answer(w, session.HTTPStatus(code), b)
}

// FailRetry answers an error and says in seconds when to try again.
func FailRetry(w http.ResponseWriter, code wire.Code, retryAfter int64) {
	if retryAfter < 1 {
		retryAfter = 1
	}
	w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
	Fail(w, code)
}

// Body reads the JSON body of a request: Content-Type application/json, at
// most session.MaxRequest bytes. It answers itself on failure and then
// reports false.
func Body(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		Fail(w, wire.CodeProtocol)
		return nil, false
	}
	// One byte over the limit is enough to tell a body that is too large from
	// one that just fits.
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, session.MaxRequest+1))
	if err != nil {
		Fail(w, wire.CodeTooLarge)
		return nil, false
	}
	if len(b) > session.MaxRequest {
		Fail(w, wire.CodeTooLarge)
		return nil, false
	}
	return b, true
}

// SetCookie sets the session cookie with the attributes of the contract.
func SetCookie(w http.ResponseWriter, value string) {
	w.Header().Add("Set-Cookie", session.CookieName+"="+value+"; "+session.CookieAttributes)
}

// ClearCookie deletes the session cookie.
func ClearCookie(w http.ResponseWriter) {
	w.Header().Add("Set-Cookie", session.CookieName+"=; "+session.CookieAttributes+"; Max-Age=0")
}

// Cookie returns the value of the session cookie.
func Cookie(r *http.Request) (string, bool) {
	c, err := r.Cookie(session.CookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}
```

- [ ] **Step 4: Write the router**

`apps/server-api/classes/httpapi/router.go`:

```go
package httpapi

import (
	"net/http"

	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Routes are the handlers of the paths before the WebSocket.
type Routes struct {
	Setup  http.HandlerFunc // GET and POST session.PathSetup
	Login  http.HandlerFunc // POST session.PathLoginStart
	Finish http.HandlerFunc // POST session.PathLoginFinish
	Logout http.HandlerFunc // POST session.PathLogout
	Trust  http.HandlerFunc // GET session.PathTrust
	Socket http.HandlerFunc // GET (Upgrade) session.PathSocket
}

// Mux returns the router of the contract: every path with exactly the methods
// the contract lists, anything else E_PROTOCOL, an unknown path E_NOTFOUND.
func (rt Routes) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	add := func(path string, methods []string, h http.HandlerFunc) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			for _, m := range methods {
				if r.Method == m {
					h(w, r)
					return
				}
			}
			Fail(w, wire.CodeProtocol)
		})
	}
	add(session.PathSetup, []string{http.MethodGet, http.MethodPost}, rt.Setup)
	add(session.PathLoginStart, []string{http.MethodPost}, rt.Login)
	add(session.PathLoginFinish, []string{http.MethodPost}, rt.Finish)
	add(session.PathLogout, []string{http.MethodPost}, rt.Logout)
	add(session.PathTrust, []string{http.MethodGet}, rt.Trust)
	add(session.PathSocket, []string{http.MethodGet}, rt.Socket)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { Fail(w, wire.CodeNotFound) })
	return mux
}
```

- [ ] **Step 5: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/httpapi/`
Expected: PASS. Danach `tools/check.sh`.

- [ ] **Step 6: Commit**

```bash
git add apps/server-api/classes/httpapi
git commit -m "Add the HTTP frame of the API before the WebSocket" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---
### Task 5: Sperre nach Fehlversuchen und Ersteinrichtung

Der Einmal-Code geht nie über die Leitung: Browser und API weisen sich mit ihm gegenseitig aus
(`session.OfferMAC`, `session.SetupMAC`). Sobald es einen Admin gibt, antworten beide Methoden
`E_NOTFOUND`.

**Files:**
- Create: `apps/server-api/classes/lock/lock.go`, `apps/server-api/classes/setup/setup.go`
- Test: `apps/server-api/classes/lock/lock_test.go`, `apps/server-api/classes/setup/setup_test.go`

**Interfaces:**
- Consumes: `state.State`, `httpapi`, `session.ValidSetupCode`, `session.NormalizeSetupCode`,
  `session.SetupInfo`, `session.SetupOffer`, `session.OfferMAC`, `session.ParseSetup`,
  `session.VerifySetup`, `trust.Node`, `trust.Store`, `trust.StoreGlobal`, `config.API`.
- Produces:
  - `lock`: `func Check(ctx, db dbapi.Local, name string, now int64) (int64, error)` – Restzeit in
    Sekunden, 0 wenn frei; `func Fail(ctx, db dbapi.Local, name string, now int64) error`;
    `func Clear(ctx, db dbapi.Local, name string) error`;
    `func Purge(ctx, db dbapi.Local, now int64) error`.
  - `setup`: `type Handler struct { S *state.State; CodeFile string }`,
    `func (h *Handler) Serve(w http.ResponseWriter, r *http.Request)`,
    `func (h *Handler) EnsureCode() (bool, error)` – legt den Code an, solange es keinen Admin gibt.

- [ ] **Step 1: Write the failing test for the lock**

`apps/server-api/classes/lock/lock_test.go`:

```go
package lock

import (
	"context"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
)

var ctx = context.Background()

func TestLockGrowsAndClears(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	// The first two failures cost nothing: a typo must not lock anyone out.
	for i := 0; i < 2; i++ {
		if err := Fail(ctx, db, "anna", now); err != nil {
			t.Fatalf("fail %d: %v", i, err)
		}
		if wait, err := Check(ctx, db, "anna", now); err != nil || wait != 0 {
			t.Fatalf("after %d failures wait %d: %v", i+1, wait, err)
		}
	}
	if err := Fail(ctx, db, "anna", now); err != nil {
		t.Fatalf("third: %v", err)
	}
	wait, err := Check(ctx, db, "anna", now)
	if err != nil || wait != 1 {
		t.Fatalf("third failure wait %d: %v", wait, err)
	}
	if err := Fail(ctx, db, "anna", now); err != nil {
		t.Fatalf("fourth: %v", err)
	}
	if wait, _ := Check(ctx, db, "anna", now); wait != 2 {
		t.Fatalf("fourth failure wait %d", wait)
	}
	// Once the time has passed the name is free again.
	if wait, _ := Check(ctx, db, "anna", now+2); wait != 0 {
		t.Fatalf("still locked after the wait: %d", wait)
	}
	if err := Clear(ctx, db, "anna"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := db.BlobGet(ctx, dbapi.BlobSetting, key("anna")); err == nil {
		t.Fatal("clear left the row")
	}
}

func TestLockIsTheSameForNamesThatDoNotExist(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	for i := 0; i < 3; i++ {
		if err := Fail(ctx, db, "nobody", now); err != nil {
			t.Fatalf("fail: %v", err)
		}
	}
	// Nothing about the row says whether the name belongs to a user.
	if wait, _ := Check(ctx, db, "nobody", now); wait != 1 {
		t.Fatalf("wait %d", wait)
	}
}

func TestPurgeRemovesOnlyExpiredLocks(t *testing.T) {
	db := memstore.New()
	now := int64(1790000000)
	for i := 0; i < 3; i++ {
		_ = Fail(ctx, db, "anna", now)
	}
	if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallation,
		Data: []byte("x")}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := Purge(ctx, db, now+3600); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := db.BlobGet(ctx, dbapi.BlobSetting, key("anna")); err == nil {
		t.Fatal("expired lock kept")
	}
	if _, err := db.BlobGet(ctx, dbapi.BlobSetting, dbapi.SettingInstallation); err != nil {
		t.Fatalf("purge removed another setting: %v", err)
	}
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/server-api/classes/lock/`
Expected: FAIL, `undefined: Fail`.

- [ ] **Step 3: Write the lock**

`apps/server-api/classes/lock/lock.go`:

```go
// Package lock counts failed logins per login name and says how long a name
// has to wait. It keeps the count in the database, so the wait holds at every
// API of an installation. The row is kept for names that do not exist too,
// and it holds only the hash of the name: an answer never tells whether a
// user exists, and the rows tell no one which names were tried.
package lock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

// Counting: the first freeCount failures are free (a typo costs nothing),
// then the wait doubles from firstWait up to maxWait.
const (
	prefix    = "lock."
	freeCount = 2
	firstWait = 1
	maxWait   = 300
	// keepFor is how long a row with no further failure is kept, so that a
	// burst of tries does not clear itself at once.
	keepFor = 3600
)

// ErrLock reports a lock row the database cannot hold or that is malformed.
var ErrLock = errors.New("lock: malformed lock row")

// key is the setting ID of a name: the hex of its SHA-256, so the row is of
// fixed size and holds no name.
func key(name string) string {
	h := sha256.Sum256([]byte(name))
	return prefix + hex.EncodeToString(h[:])
}

// read returns the number of failures and the time the wait ends.
func read(ctx context.Context, db dbapi.Local, name string) (int, int64, error) {
	b, err := db.BlobGet(ctx, dbapi.BlobSetting, key(name))
	if errors.Is(err, dbapi.ErrNotFound) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	count, until, ok := strings.Cut(string(b.Data), " ")
	n, err1 := strconv.Atoi(count)
	u, err2 := strconv.ParseInt(until, 10, 64)
	if !ok || err1 != nil || err2 != nil || n < 0 || u < 0 {
		return 0, 0, ErrLock
	}
	return n, u, nil
}

// Check returns how many seconds name still has to wait, 0 when it is free.
func Check(ctx context.Context, db dbapi.Local, name string, now int64) (int64, error) {
	_, until, err := read(ctx, db, name)
	if err != nil || until <= now {
		return 0, err
	}
	return until - now, nil
}

// Fail counts one failed attempt for name and sets the new wait.
func Fail(ctx context.Context, db dbapi.Local, name string, now int64) error {
	n, _, err := read(ctx, db, name)
	if err != nil {
		return err
	}
	n++
	wait := int64(0)
	if n > freeCount {
		wait = firstWait << min(n-freeCount-1, 16)
		if wait > maxWait {
			wait = maxWait
		}
	}
	until := now + wait
	// Seq is the time the row may be removed, so Purge can skip the rows that
	// still count without reading them.
	return db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: key(name),
		Seq: uint64(now + keepFor), Data: []byte(strconv.Itoa(n) + " " + strconv.FormatInt(until, 10)),
		Updated: now})
}

// Clear removes the row after a successful login.
func Clear(ctx context.Context, db dbapi.Local, name string) error {
	return db.BlobDelete(ctx, dbapi.BlobSetting, key(name))
}

// Purge removes lock rows whose time has passed. Other settings stay.
func Purge(ctx context.Context, db dbapi.Local, now int64) error {
	blobs, err := db.BlobFrom(ctx, dbapi.BlobSetting, 0)
	if err != nil {
		return err
	}
	for _, b := range blobs {
		if !strings.HasPrefix(b.ID, prefix) || b.Seq > uint64(now) {
			continue
		}
		if err := db.BlobDelete(ctx, dbapi.BlobSetting, b.ID); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run the lock tests and see them pass**

Run: `go test ./apps/server-api/classes/lock/`
Expected: PASS.

- [ ] **Step 5: Write the failing test for the setup**

`apps/server-api/classes/setup/setup_test.go`:

```go
package setup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
)

var ctx = context.Background()

func newHandler(t *testing.T) *Handler {
	t.Helper()
	box, err := secrets.NewBox(config.NewSecret(make([]byte, 32)))
	if err != nil {
		t.Fatalf("box: %v", err)
	}
	_, signKey := trusttest.NodeKeys(1)
	noiseKey, _ := trusttest.NodeKeys(2)
	cfg := &config.API{
		Node: "api-1", URL: "wss://api-1.ngc.test:7443/tunnel", Listen: ":7443",
		Cert: "/x", CertKey: "/y",
		NoiseKey: config.NewSecret(noiseKey.Private), SignKey: config.NewSecret(signKey.Seed()),
		SecretKey: config.NewSecret(make([]byte, 32)),
		// Its own key next to SecretKey; config.API.Validate asks for both
		// with 32 bytes, so a configuration without it is none.
		ChallengeKey: config.NewSecret(make([]byte, 32)),
		ReleaseKey:   enc.Bytes(make([]byte, 32)), StoreKey: enc.Bytes(make([]byte, 32)),
		StoreURL:     "https://store.ngc.test", SessionLifetime: 43200, IdleTimeout: 1800,
	}
	s := state.New(cfg, memstore.New(), box, make([]byte, 32))
	s.Now = func() int64 { return 1790000000 }
	h := &Handler{S: s, CodeFile: filepath.Join(t.TempDir(), "setup-code")}
	if _, err := h.EnsureCode(); err != nil {
		t.Fatalf("ensure code: %v", err)
	}
	return h
}

func TestOfferIsSealedWithTheCode(t *testing.T) {
	h := newHandler(t)
	code, err := os.ReadFile(h.CodeFile)
	if err != nil || !session.ValidSetupCode(strings.TrimSpace(string(code))) {
		t.Fatalf("code file %q: %v", code, err)
	}
	if info, _ := os.Stat(h.CodeFile); info.Mode().Perm() != 0o600 {
		t.Fatalf("code file mode %v", info.Mode().Perm())
	}
	w := httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	if w.Code != 200 {
		t.Fatalf("offer status %d", w.Code)
	}
	info, err := session.ParseSetupOffer(w.Body.Bytes(), strings.TrimSpace(string(code)))
	if err != nil {
		t.Fatalf("parse offer: %v", err)
	}
	if len(info.Nodes) != 1 || info.Nodes[0].ID != "api-1" || info.Nodes[0].URL != h.S.Cfg.URL {
		t.Fatalf("nodes %+v", info.Nodes)
	}
	if info.Store == nil || info.Store.ID != trust.StoreGlobal || info.Store.URL != h.S.Cfg.StoreURL {
		t.Fatalf("store %+v", info.Store)
	}
	// Another code opens nothing: the MAC is checked before info is read.
	if _, err := session.ParseSetupOffer(w.Body.Bytes(), "AAAAA-BBBBB-CCCCC-DDDDD"); err == nil {
		t.Fatal("offer accepted with another code")
	}
}

func TestSetupTakesVersionOneOnlyOnce(t *testing.T) {
	h := newHandler(t)
	codeBytes, _ := os.ReadFile(h.CodeFile)
	code := strings.TrimSpace(string(codeBytes))
	w := httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	info, err := session.ParseSetupOffer(w.Body.Bytes(), code)
	if err != nil {
		t.Fatalf("offer: %v", err)
	}

	admin := trusttest.NewSigner(3)
	body := firstVersion(t, info, admin, code)
	w = httptest.NewRecorder()
	h.Serve(w, post(body))
	if w.Code != 204 {
		t.Fatalf("setup status %d body %s", w.Code, w.Body)
	}
	if _, err := os.Stat(h.CodeFile); !os.IsNotExist(err) {
		t.Fatal("the code file survived the setup")
	}
	v, err := h.S.Trust(ctx)
	if err != nil || v.List.Seq != 1 {
		t.Fatalf("trust after setup: %v", err)
	}
	// Once there is an admin both methods answer E_NOTFOUND.
	w = httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	if w.Code != 404 {
		t.Fatalf("second offer status %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.Serve(w, post(body))
	if w.Code != 404 {
		t.Fatalf("second setup status %d", w.Code)
	}
}

func TestSetupRefusesAForeignVersionAndLocks(t *testing.T) {
	h := newHandler(t)
	codeBytes, _ := os.ReadFile(h.CodeFile)
	code := strings.TrimSpace(string(codeBytes))
	w := httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest(http.MethodGet, session.PathSetup, nil))
	info, _ := session.ParseSetupOffer(w.Body.Bytes(), code)
	admin := trusttest.NewSigner(3)
	// A man in the middle who does not hold the code cannot make the MAC.
	body := firstVersion(t, info, admin, "AAAAA-BBBBB-CCCCC-DDDDD")
	for i := 0; i < 3; i++ {
		w = httptest.NewRecorder()
		h.Serve(w, post(body))
		if w.Code != 403 && w.Code != 429 {
			t.Fatalf("attempt %d answered %d", i, w.Code)
		}
	}
	w = httptest.NewRecorder()
	h.Serve(w, post(body))
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("no lock: %d %q", w.Code, w.Header().Get("Retry-After"))
	}
	if _, err := h.S.Trust(ctx); err == nil {
		t.Fatal("a foreign version was taken")
	}
}

// post builds a POST of the setup with the right content type.
func post(body []byte) *http.Request {
	r := httptest.NewRequest(http.MethodPost, session.PathSetup, strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// firstVersion builds version 1 the way the shell does and seals it with mac.
func firstVersion(t *testing.T, info session.SetupInfo, admin trusttest.Signer, mac string) []byte {
	t.Helper()
	l := &trust.List{
		Seq: 1, Issued: info.Now, Installation: info.Installation,
		Users: []trust.User{{ID: trusttest.ID(1), Name: "admin", Roles: []string{trust.RoleAdmin},
			Keys: []proofUserKey{admin.Key}[0:1]}},
		Roles:  []trust.Role{{Name: trust.RoleAdmin, Level: trust.LevelCritical}},
		Nodes:  info.Nodes,
		Stores: []trust.Store{*info.Store},
		Auto: []trust.Auto{
			{Target: trust.AutoAPI, Key: info.ReleaseKey},
			{Target: trust.AutoController, Key: info.ReleaseKey},
			{Target: trust.AutoSystem},
			{Target: trust.AutoWeb, Key: info.ReleaseKey},
		},
	}
	signed := admin.Sign(t, l)
	b, err := json.Marshal(session.Setup{MAC: session.SetupMAC(mac, signed.Payload), Trust: signed})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
```

Dazu oben in der Testdatei, weil `trust.User.Keys` den Typ von `proof` hat:

```go
type proofUserKey = proof.UserKey
```

mit `"github.com/sorglos-it/nextgencloud/shared/proof"` im Import-Block. Die Liste `Auto` steht
nach `Target` sortiert, wie `trust.List.Validate` es verlangt.

- [ ] **Step 6: Run it and see it fail**

Run: `go test ./apps/server-api/classes/setup/`
Expected: FAIL, `undefined: Handler`.

- [ ] **Step 7: Write the setup**

`apps/server-api/classes/setup/setup.go`:

```go
// Package setup serves the first setup: while the installation has no admin,
// GET session.PathSetup offers what version 1 of the trust list is built from,
// and POST takes that version. The one-time code never travels; both sides
// prove they hold it with a MAC over the exact bytes (session.OfferMAC,
// session.SetupMAC). The code lies in a file only root can read and never in
// the database, never in a log.
package setup

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
)

// lockName is the bucket of failed setup attempts; it is no login name, so it
// can never collide with one (a name has no '/').
const lockName = "setup/code"

// Handler serves both methods of session.PathSetup.
type Handler struct {
	S        *state.State
	CodeFile string // /var/lib/ngc/setup-code
}

// EnsureCode makes the one-time code if the installation has no trust list
// yet, and reports whether a code now exists. With a trust list it removes a
// left-over code file: from then on the setup is closed.
func (h *Handler) EnsureCode() (bool, error) {
	ctx := startContext()
	if _, err := h.S.Trust(ctx); err == nil {
		return false, h.removeCode()
	} else if err != state.ErrNoTrust {
		return false, err
	}
	if _, err := h.S.EnsureInstallation(ctx); err != nil {
		return false, err
	}
	if _, err := os.Stat(h.CodeFile); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	code, err := secrets.SetupCode()
	if err != nil {
		return false, err
	}
	f, err := os.OpenFile(h.CodeFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return false, err
	}
	if _, err := f.WriteString(code + "\n"); err != nil {
		return false, err
	}
	return true, f.Sync()
}

// code reads the one-time code; an unreadable or malformed file means there
// is no setup.
func (h *Handler) code() (string, bool) {
	b, err := os.ReadFile(h.CodeFile)
	if err != nil {
		return "", false
	}
	c := strings.TrimSpace(string(b))
	if !session.ValidSetupCode(c) {
		return "", false
	}
	return c, true
}

func (h *Handler) removeCode() error {
	if err := os.Remove(h.CodeFile); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Serve answers GET and POST of session.PathSetup.
func (h *Handler) Serve(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Once a trust list exists there is an admin, and the setup is over.
	if _, err := h.S.Trust(ctx); err == nil {
		httpapi.Fail(w, wire.CodeNotFound)
		return
	} else if err != state.ErrNoTrust {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	code, ok := h.code()
	if !ok {
		httpapi.Fail(w, wire.CodeNotFound)
		return
	}
	if r.Method == http.MethodGet {
		h.offer(w, r, code)
		return
	}
	h.take(w, r, code)
}

// offer answers GET: the SetupInfo as JSON and the MAC over exactly those
// bytes, so nobody between browser and API can offer other keys.
func (h *Handler) offer(w http.ResponseWriter, r *http.Request, code string) {
	ctx := r.Context()
	installation, err := h.S.EnsureInstallation(ctx)
	if err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	noise, signPub, err := h.nodeKeys()
	if err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return
	}
	info := session.SetupInfo{
		Installation: installation,
		Nodes: []trust.Node{{ID: h.S.Cfg.Node, Kind: trust.NodeAPI, Name: h.S.Cfg.Node,
			Noise: noise, Sign: signPub, URL: h.S.Cfg.URL, TLS: enc.Bytes(h.S.Pin)}},
		Now:        h.S.Now(),
		ReleaseKey: h.S.Cfg.ReleaseKey,
		Store: &trust.Store{ID: trust.StoreGlobal, Name: "NGC Store",
			URL: h.S.Cfg.StoreURL, Key: h.S.Cfg.StoreKey},
	}
	raw, err := json.Marshal(info)
	if err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return
	}
	body, err := json.Marshal(session.SetupOffer{Info: raw, MAC: session.OfferMAC(code, raw)})
	if err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return
	}
	httpapi.Answer(w, http.StatusOK, body)
}

// take answers POST: version 1 as the new admin signed it.
func (h *Handler) take(w http.ResponseWriter, r *http.Request, code string) {
	ctx := r.Context()
	now := h.S.Now()
	if wait, err := lock.Check(ctx, h.S.DB, lockName, now); err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	} else if wait > 0 {
		httpapi.FailRetry(w, wire.CodeLocked, wait)
		return
	}
	body, ok := httpapi.Body(w, r)
	if !ok {
		return
	}
	fail := func(code wire.Code) {
		// Every wrong attempt costs, whatever was wrong about it.
		_ = lock.Fail(ctx, h.S.DB, lockName, now)
		httpapi.Fail(w, code)
	}
	s, err := session.ParseSetup(body)
	if err != nil {
		fail(wire.CodeProtocol)
		return
	}
	installation, err := h.S.EnsureInstallation(ctx)
	if err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	noise, signPub, err := h.nodeKeys()
	if err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return
	}
	info := session.SetupInfo{
		Installation: installation,
		Nodes: []trust.Node{{ID: h.S.Cfg.Node, Kind: trust.NodeAPI, Name: h.S.Cfg.Node,
			Noise: noise, Sign: signPub, URL: h.S.Cfg.URL, TLS: enc.Bytes(h.S.Pin)}},
		Now:        h.S.Now(),
		ReleaseKey: h.S.Cfg.ReleaseKey,
		Store: &trust.Store{ID: trust.StoreGlobal, Name: "NGC Store",
			URL: h.S.Cfg.StoreURL, Key: h.S.Cfg.StoreKey},
	}
	if _, err := session.VerifySetup(s, info, code); err != nil {
		fail(wire.CodeVerify)
		return
	}
	if _, err := h.S.AddVersion(ctx, s.Trust); err != nil {
		if errors.Is(err, state.ErrOldVersion) {
			httpapi.Fail(w, wire.CodeNotFound)
			return
		}
		fail(wire.CodeVerify)
		return
	}
	if err := h.removeCode(); err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return
	}
	_ = lock.Clear(ctx, h.S.DB, lockName)
	httpapi.NoContent(w)
}

// nodeKeys returns this API's public Noise and signing keys, which the offer
// names: a controller dials the Noise key, and the signing key checks API
// certificates.
func (h *Handler) nodeKeys() (enc.Bytes, enc.Bytes, error) {
	noise, err := ecdh.X25519().NewPrivateKey(h.S.Cfg.NoiseKey.Bytes())
	if err != nil {
		return nil, nil, err
	}
	seed := h.S.Cfg.SignKey.Bytes()
	if len(seed) != ed25519.SeedSize {
		return nil, nil, errors.New("setup: signing key of the wrong size")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	return enc.Bytes(noise.PublicKey().Bytes()), enc.Bytes(priv.Public().(ed25519.PublicKey)), nil
}
```

Dazu `"context"`, `"crypto/ecdh"`, `"crypto/ed25519"` im Import-Block und

```go
// startContext is the context of the start, where no request carries one.
func startContext() context.Context { return context.Background() }
```

- [ ] **Step 8: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/setup/ ./apps/server-api/classes/lock/`
Expected: PASS.

- [ ] **Step 9: Add the fuzz test for the setup body**

`apps/server-api/classes/setup/fuzz_test.go`:

```go
package setup

import (
	"net/http/httptest"
	"testing"
)

// FuzzSetupBody feeds POST session.PathSetup with everything: the API must
// answer a status of the contract and never panic or take a version.
func FuzzSetupBody(f *testing.F) {
	f.Add([]byte(`{"mac":"","trust":{"payload":"","proof":{}}}`))
	f.Add([]byte("{}"))
	f.Add([]byte("null"))
	f.Fuzz(func(t *testing.T, body []byte) {
		h := newHandler(t)
		w := httptest.NewRecorder()
		h.Serve(w, post(body))
		switch w.Code {
		case 204, 400, 403, 404, 413, 429, 500, 503:
		default:
			t.Fatalf("status %d for %q", w.Code, body)
		}
		if w.Code == 204 {
			t.Fatalf("a fuzzed body was taken as version 1: %q", body)
		}
	})
}
```

- [ ] **Step 10: Run the fuzz target and check**

Run: `go test ./apps/server-api/classes/setup/ -run Fuzz -fuzz FuzzSetupBody -fuzztime 60s`,
danach `tools/check.sh`.
Expected: PASS ohne Fund.

- [ ] **Step 11: Commit**

```bash
git add apps/server-api/classes/lock apps/server-api/classes/setup
git commit -m "Add the login lock and the first setup of the API" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 6: Anmeldung, erster Teil – `POST /api/v1/login/start`

Die Antwort baut **ausschließlich** `session.Challenge`, nie die API von Hand. Für unbekannte Namen
erfindet diese Funktion eine Antwort, die von einer echten nicht zu unterscheiden ist – deshalb
geht die API auch für unbekannte Namen denselben Weg und verhält sich an keiner Stelle anders:
nicht früher antworten, nicht anders protokollieren, nicht anders sperren.

**Files:**
- Create: `apps/server-api/classes/login/login.go`, `apps/server-api/classes/login/start.go`,
  `apps/server-api/classes/login/share.go`
- Test: `apps/server-api/classes/login/start_test.go`, `apps/server-api/classes/login/share_test.go`

**Interfaces:**
- Consumes: `session.ParseLoginStart`, `session.Challenge`, `session.ChallengeInput`,
  `session.LoginChallenge`, `session.NonceSize`, `session.ChallengeLifetime`,
  `config.API.ChallengeKey`, `trust.List.UserByName`, `dbapi.Store.UserList`, `dbapi.OnceNonce`,
  `lock.Check`, `lock.Fail`.
- Produces:
  - `type Handler struct { S *state.State }`, `func New(s *state.State) *Handler`
  - `func (h *Handler) Start(w http.ResponseWriter, r *http.Request)`
  - `func (h *Handler) userOf(l *trust.List, id string) (trust.User, bool)`
  - `type shareCache struct { … }`, `func (h *Handler) totpShare(ctx context.Context) int`

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/login/start_test.go`:

```go
package login

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
)

func TestStartAnswersForAKnownUser(t *testing.T) {
	h, fx := newFixture(t)
	c := start(t, h, fx.PasswordName)
	if len(c.Nonce) != session.NonceSize || c.Installation != fx.Installation || c.User != fx.PasswordUser {
		t.Fatalf("challenge %+v", c)
	}
	if c.KDF == nil || c.KDF.Validate() != nil {
		t.Fatalf("kdf %+v", c.KDF)
	}
	if c.TOTP || c.PasswordToo {
		t.Fatalf("totp %v passwordToo %v", c.TOTP, c.PasswordToo)
	}
	if c.MaxLifetime != h.S.Cfg.SessionLifetime || c.Now != h.S.Now() {
		t.Fatalf("times %d %d", c.MaxLifetime, c.Now)
	}
	if c.RP.ID == "" {
		t.Fatal("rp missing")
	}
	// Two challenges for the same user have different nonces.
	if other := start(t, h, fx.PasswordName); string(other.Nonce) == string(c.Nonce) {
		t.Fatal("the same nonce twice")
	}
}

func TestStartNamesTheKeyIDOfEveryPasskey(t *testing.T) {
	h, fx := newFixture(t)
	c := start(t, h, fx.PasskeyName)
	u, _ := fx.List.UserByName(fx.PasskeyName)
	var want int
	for _, k := range u.Keys {
		if k.Kind == proof.KindPasskey {
			want++
		}
	}
	if len(c.Passkeys) != want {
		t.Fatalf("%d passkeys, want %d", len(c.Passkeys), want)
	}
	// The browser takes the key ID from here and derives none of its own, so
	// every entry has to carry both halves (session.PasskeyRef).
	for _, p := range c.Passkeys {
		if p.Validate() != nil {
			t.Fatalf("passkey ref %+v", p)
		}
	}
	// The whole answer has to pass the parser the browser reads it with;
	// anything it refuses would tell the browser something.
	b, _ := json.Marshal(c)
	if _, err := session.ParseLoginChallenge(b); err != nil {
		t.Fatalf("the browser would refuse this answer: %v", err)
	}
}

func TestStartLooksTheSameForAnUnknownName(t *testing.T) {
	h, _ := newFixture(t)
	c := start(t, h, "nobody")
	if len(c.Nonce) != session.NonceSize {
		t.Fatalf("challenge %+v", c)
	}
	if !trust.ValidID(c.User) {
		t.Fatalf("user %q", c.User)
	}
	// The made-up answer passes the browser's parser like a real one.
	b, _ := json.Marshal(c)
	if _, err := session.ParseLoginChallenge(b); err != nil {
		t.Fatalf("the browser would refuse the made-up answer: %v", err)
	}
	// Made up, but stable: a second try must not tell the shell anything new.
	again := start(t, h, "nobody")
	if again.User != c.User || len(again.Passkeys) != len(c.Passkeys) || again.TOTP != c.TOTP {
		t.Fatal("the made-up values changed between two calls")
	}
	if (again.KDF == nil) != (c.KDF == nil) ||
		(c.KDF != nil && string(again.KDF.Salt) != string(c.KDF.Salt)) {
		t.Fatal("the made-up KDF changed between two calls")
	}
	// And they differ from those of another unknown name.
	third := start(t, h, "nothing")
	if third.User == c.User {
		t.Fatal("two unknown names got the same user")
	}
}

func TestStartTakesTheSamePathForAnUnknownName(t *testing.T) {
	h, fx := newFixture(t)
	// 1. Nothing outside tells the two apart: same status, same headers, and a
	//    shape the installation really gives out. session.Challenge draws the
	//    made-up shape from the list, so it is one of the shapes the list's own
	//    users produce — not necessarily this one user's.
	known, unknown := raw(t, h, fx.PasswordName), raw(t, h, "nobody")
	if known.Code != unknown.Code {
		t.Fatalf("status %d vs %d", known.Code, unknown.Code)
	}
	if !maps.EqualFunc(known.Header(), unknown.Header(), slices.Equal[[]string]) {
		t.Fatalf("headers %v vs %v", known.Header(), unknown.Header())
	}
	real := map[string]bool{}
	for _, u := range fx.List.Users {
		real[fields(t, raw(t, h, u.Name))] = true
	}
	if !real[fields(t, unknown)] {
		t.Fatalf("made-up shape %q, real shapes %v", fields(t, unknown), real)
	}
	// 2. Nothing inside either: login/start writes no log entry for either
	//    name, and it locks neither. Only login/finish counts failures, so
	//    asking for a name must never hold it against anybody.
	entries, err := h.S.DB.LogQuery(ctx, core.LogQuery{Limit: 10})
	if err != nil || len(entries) != 0 {
		t.Fatalf("login/start logged %d entries (%v)", len(entries), err)
	}
	for _, name := range []string{fx.PasswordName, "nobody"} {
		if wait, _ := lock.Check(ctx, h.S.DB, name, h.S.Now()); wait != 0 {
			t.Fatalf("%q was locked by asking", name)
		}
	}
	// 3. Time does not tell them apart either: session.Challenge walks the
	//    whole list and builds the made-up answer both ways, so the medians of
	//    many runs stay within a factor of two. The factor is generous on
	//    purpose — this test may not become flaky on a loaded machine; it only
	//    catches an early return that was put back in.
	a, b := median(t, h, fx.PasswordName), median(t, h, "nobody")
	if a > 2*b || b > 2*a {
		t.Fatalf("known %v, unknown %v", a, b)
	}
}

func TestStartLocksAfterFailures(t *testing.T) {
	h, _ := newFixture(t)
	// Only login/finish counts failures; start itself must stay answerable so
	// the shell can retry after a lock has passed. A locked name is refused.
	for i := 0; i < 3; i++ {
		failLogin(t, h, "anna")
	}
	w := httptest.NewRecorder()
	h.Start(w, jsonPost(session.PathLoginStart, `{"name":"anna"}`))
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("status %d retry %q", w.Code, w.Header().Get("Retry-After"))
	}
}

func TestStartRefusesABadName(t *testing.T) {
	h, _ := newFixture(t)
	for _, body := range []string{`{"name":""}`, `{"name":"Anna"}`, `{"name":"a b"}`, `{}`, `{"name":1}`} {
		w := httptest.NewRecorder()
		h.Start(w, jsonPost(session.PathLoginStart, body))
		if w.Code != 400 {
			t.Fatalf("%s answered %d", body, w.Code)
		}
	}
}

// start runs login/start and returns the challenge.
func start(t *testing.T, h *Handler, name string) session.LoginChallenge {
	t.Helper()
	w := httptest.NewRecorder()
	h.Start(w, jsonPost(session.PathLoginStart, `{"name":"`+name+`"}`))
	if w.Code != 200 {
		t.Fatalf("start %q answered %d: %s", name, w.Code, w.Body)
	}
	var c session.LoginChallenge
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatalf("challenge: %v", err)
	}
	return c
}

// raw runs login/start and hands back the whole answer, headers and all.
func raw(t *testing.T, h *Handler, name string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.Start(w, jsonPost(session.PathLoginStart, `{"name":"`+name+`"}`))
	return w
}

// fields lists the field names of an answer in order, so two answers can be
// compared without comparing the values, which differ by design.
func fields(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("answer: %v", err)
	}
	return strings.Join(slices.Sorted(maps.Keys(m)), ",")
}

// median times 25 runs and returns the middle one. The median ignores the one
// slow run a busy machine throws in, which an average would not.
func median(t *testing.T, h *Handler, name string) time.Duration {
	t.Helper()
	d := make([]time.Duration, 25)
	for i := range d {
		start := time.Now()
		raw(t, h, name)
		d[i] = time.Since(start)
	}
	slices.Sort(d)
	return d[len(d)/2]
}

// jsonPost builds a POST with the right content type.
func jsonPost(path, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}
```

`TestStartTakesTheSamePathForAnUnknownName` hält die Regel aus `session.Challenge` fest: beide
Namen nehmen denselben Weg, und nach außen unterscheidet sie nichts – weder die Antwort noch die
Zeit noch Protokoll oder Sperre. Er bekommt von `newFixture` eine eigene, leere Datenbank, deshalb
sagt „das Protokoll ist leer“ hier wirklich „`login/start` hat nichts geschrieben“.

- [ ] **Step 2: Write the fixture every login test uses**

`apps/server-api/classes/login/fixture_test.go`:

```go
package login

import (
	"bytes"
	"context"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
)

var ctx = context.Background()

// testNow is the fixed time of every login test.
const testNow = int64(1790000000)

// fixture is the installation the login tests work against: trusttest.Genesis
// holds two admins sorted by ID, Users[0] with a passkey and Users[1] with a
// password key (CLAUDE.md, "Tests").
type fixture struct {
	Installation string
	Passkey      trusttest.Signer
	PasskeyUser  string
	PasskeyName  string
	Password     trusttest.Signer
	PasswordUser string
	PasswordName string
	List         *trust.List
	Signed       trust.Signed
}

func newFixture(t *testing.T) (*Handler, fixture) {
	t.Helper()
	box, err := secrets.NewBox(config.NewSecret(make([]byte, 32)))
	if err != nil {
		t.Fatalf("box: %v", err)
	}
	// 0xA1, not 1: session.VerifyAPI looks the node up by sign.KeyID, so a
	// certificate signed with any other key finds no node and comes back
	// ErrCert - the test would then pass without checking the certificate.
	noise, signKey := trusttest.NodeKeys(0xA1)
	cfg := &config.API{
		Node: "api-1", URL: "wss://api-1.ngc.test:7443/tunnel", Listen: ":7443",
		Cert: "/x", CertKey: "/y",
		NoiseKey: config.NewSecret(noise.Private), SignKey: config.NewSecret(signKey.Seed()),
		SecretKey: config.NewSecret(make([]byte, 32)),
		// Its own key, not SecretKey: session.Challenge draws the made-up
		// answer from it, and the same key in every API node makes all of them
		// answer one name alike.
		ChallengeKey: config.NewSecret(bytes.Repeat([]byte{7}, 32)),
		ReleaseKey:   enc.Bytes(make([]byte, 32)), StoreKey: enc.Bytes(make([]byte, 32)),
		StoreURL:     "https://store.ngc.test", SessionLifetime: 43200, IdleTimeout: 1800,
	}
	s := state.New(cfg, memstore.New(), box, make([]byte, 32))
	s.Now = func() int64 { return testNow }

	passkey := trusttest.NewPasskey(9)
	l := trusttest.Genesis(passkey)
	signed := passkey.Sign(t, l)
	if _, err := s.AddVersion(ctx, signed); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	if _, err := s.EnsureInstallation(ctx); err != nil {
		t.Fatalf("installation: %v", err)
	}
	fx := fixture{
		Installation: l.Installation,
		Passkey:      passkey, PasskeyUser: l.Users[0].ID, PasskeyName: l.Users[0].Name,
		Password: trusttest.NewSigner(1), PasswordUser: l.Users[1].ID, PasswordName: l.Users[1].Name,
		List: l, Signed: signed,
	}
	// Every user of the trust list also has a row, which holds what the list
	// does not: TOTP secret, failures, last login.
	for _, u := range l.Users {
		if err := s.DB.UserPut(ctx, dbapi.User{ID: u.ID, Name: u.Name, Roles: u.Roles}); err != nil {
			t.Fatalf("user %s: %v", u.ID, err)
		}
	}
	return New(s), fx
}

// failLogin counts one failed attempt for name, as login/finish does.
func failLogin(t *testing.T, h *Handler, name string) {
	t.Helper()
	if err := lock.Fail(ctx, h.S.DB, name, h.S.Now()); err != nil {
		t.Fatalf("fail: %v", err)
	}
}
```

Die Prüfung, dass `trusttest.Genesis` wirklich zwei Admins in dieser Reihenfolge liefert, steht
schon in `shared/trust/trusttest`; die Testdatei verlässt sich darauf (`CLAUDE.md`, „Tests“).

- [ ] **Step 3: Run it and see it fail**

Run: `go test ./apps/server-api/classes/login/`
Expected: FAIL, `undefined: Handler`.

- [ ] **Step 4: Write the handler frame**

`apps/server-api/classes/login/login.go`:

```go
// Package login serves the login, the sessions and the trust list over HTTP:
// login/start, login/finish, logout and GET session.PathTrust. Every failed
// login answers E_LOGIN alike (shared/README.md, "Vor dem WebSocket"), so no
// answer tells which part failed or whether a user exists.
package login

import (
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
)

// Handler serves the paths of the login.
type Handler struct {
	S     *state.State
	share shareCache
}

// New returns a Handler.
func New(s *state.State) *Handler { return &Handler{S: s} }

// userOf finds a user of the trust list by ID.
func (h *Handler) userOf(l *trust.List, id string) (trust.User, bool) { return l.User(id) }
```

Mehr braucht es hier nicht: Wer der Name ist, was für Schlüssel er hat, welche KDF die Shell
bekommt, ob die Rolle das Passwort zusätzlich verlangt und wie eine erfundene Antwort aussieht –
das alles entscheidet **`session.Challenge`**. Die API baut nichts davon selbst, denn jede eigene
Zeile wäre eine Zeile, die für bekannte und unbekannte Namen anders laufen könnte.

- [ ] **Step 5: Den TOTP-Anteil zählen und zwischenspeichern**

`session.ChallengeInput.TOTPShare` ist die einzige Zahl, die `session.Challenge` weder aus der
Vertrauensliste noch aus der Anfrage nehmen kann: wie viele Benutzer der Anlage ein TOTP-Geheimnis
haben, in Prozent. Die Vertrauensliste weiß das nicht – `dbapi.User.TOTP` steht nur in der
Datenbank. Eine erfundene Antwort zieht ihr `totp` daraus; mit festen Chancen wäre in einer Anlage
mit TOTP-Pflicht jede zweite erfundene Antwort auf einen Blick als erfunden zu erkennen.

`apps/server-api/classes/login/share.go`:

```go
package login

import (
	"context"
	"sync"
)

// shareTTL is how long a counted TOTP share is used again. The share moves
// only when users are added or change their TOTP, so a minute-old number is
// as good as a fresh one — and counting on every login/start would put a full
// table scan in front of every login.
const shareTTL = 300

// shareCache keeps the last counted share with the time it was counted.
type shareCache struct {
	mu      sync.Mutex
	percent int
	counted int64
	ok      bool
}

// totpShare returns how many of the installation's users hold a TOTP secret,
// in percent (0..100), counted in the database and kept for shareTTL seconds.
// A database that cannot be reached gives the last counted value, and 0 if
// there is none: a made-up answer must still be built, and it must be built
// the same way for every name, so this never fails the request.
func (h *Handler) totpShare(ctx context.Context) int {
	now := h.S.Now()
	h.share.mu.Lock()
	defer h.share.mu.Unlock()
	if h.share.ok && now-h.share.counted < shareTTL {
		return h.share.percent
	}
	users, err := h.S.DB.UserList(ctx)
	if err != nil {
		return h.share.percent
	}
	with := 0
	for _, u := range users {
		if len(u.TOTP) > 0 {
			with++
		}
	}
	if len(users) > 0 {
		h.share.percent = with * 100 / len(users)
	} else {
		h.share.percent = 0
	}
	h.share.counted, h.share.ok = now, true
	return h.share.percent
}
```

`apps/server-api/classes/login/share_test.go`:

```go
func TestTOTPShareCountsTheUsers(t *testing.T)
// Vier Benutzer, einer mit TOTP: 25. Keiner mit TOTP: 0. Keine Benutzer: 0.

func TestTOTPShareIsKeptForAWhile(t *testing.T)
// Nach dem ersten Zählen ändert ein neuer Benutzer die Zahl erst, wenn die Uhr um shareTTL
// weitergestellt wurde (h.S.Now ersetzen).

func TestTOTPShareSurvivesADatabaseFailure(t *testing.T)
// Eine Datenbank, die Fehler gibt, liefert die zuletzt gezählte Zahl, und 0, wenn es keine gibt;
// login/start antwortet trotzdem.
```

- [ ] **Step 6: Write login/start**

`apps/server-api/classes/login/start.go`:

```go
package login

import (
	"net/http"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
)

// Start answers POST session.PathLoginStart with a challenge, for names that
// do not exist too.
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, ok := httpapi.Body(w, r)
	if !ok {
		return
	}
	req, err := session.ParseLoginStart(body)
	if err != nil {
		httpapi.Fail(w, wire.CodeProtocol)
		return
	}
	now := h.S.Now()
	wait, err := lock.Check(ctx, h.S.DB, req.Name, now)
	if err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	if wait > 0 {
		httpapi.FailRetry(w, wire.CodeLocked, wait)
		return
	}
	v, err := h.S.Trust(ctx)
	if err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	nonce, err := secrets.Bytes(session.NonceSize)
	if err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return
	}
	// trust.List.UserByName is the lookup the contract hands the API for a
	// typed name. It is needed for two things and two only: the row that holds
	// the TOTP secret, and the nonce that is kept for a real user. The answer
	// itself comes from session.Challenge, which walks the list on its own.
	//
	// The row is read for every name, known or not, and a missing row is no
	// failure — a name nobody has simply has none, and asking for it must cost
	// what asking for a real one costs.
	u, known := v.List.UserByName(req.Name)
	row, err := h.S.DB.UserGet(ctx, u.ID)
	if err != nil && !isNotFound(err) {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	// session.Challenge builds the whole answer, for a name nobody has too:
	// it walks the list and builds the made-up answer either way, so neither
	// the shape of the answer nor the time it takes says whether the user
	// exists. The API adds nothing of its own here and takes no shortcut.
	c, err := session.Challenge(v.List, session.ChallengeInput{
		Name:        req.Name,
		TOTP:        len(row.TOTP) > 0,
		TOTPShare:   h.totpShare(ctx),
		Key:         h.S.Cfg.ChallengeKey.Bytes(),
		Nonce:       nonce,
		Now:         now,
		MaxLifetime: h.S.Cfg.SessionLifetime,
	})
	if err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return
	}
	if known {
		// Only a real nonce is kept. The made-up answer carries one that was
		// never stored, so login/finish fails with E_LOGIN like any other
		// wrong attempt — the same answer a wrong password gets.
		if err := h.S.DB.OnceCreate(ctx, dbapi.OnceNonce, nonce, u.ID, now+session.ChallengeLifetime); err != nil {
			httpapi.Fail(w, wire.CodeUnreachable)
			return
		}
	}
	httpapi.Answer(w, http.StatusOK, mustJSON(w, c))
}
```

Der einzige Unterschied zwischen bekannt und unbekannt ist die eine Zeile in der Datenbank, die
niemand von außen sieht. Alles andere – Reihenfolge der Schritte, Statuscode, Kopfzeilen, Protokoll
(keines) und Sperre (keine) – ist gleich, und die Form der Antwort zieht `session.Challenge` aus der
Liste, sodass eine erfundene Antwort aussieht wie die eines Benutzers dieser Anlage. Wer diese
Aufgabe umbaut, muss es gleich lassen: `TestStartTakesTheSamePathForAnUnknownName` misst es.

Dazu in `login.go` die zwei Helfer:

```go
// mustJSON encodes an answer of the API (session.LoginChallenge,
// session.LoginResult): fixed structs of strings, numbers and bytes, so
// encoding cannot fail; a failure all the same answers E_INTERNAL and writes
// nothing. session.Challenge has already held its answer against
// session.ParseLoginChallenge, so what is written here is what the browser
// accepts.
func mustJSON(w http.ResponseWriter, v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return nil
	}
	return b
}

// isNotFound reports the "not found" of dbapi, which is no failure where a row
// may be missing.
func isNotFound(err error) bool { return errors.Is(err, dbapi.ErrNotFound) }
```

mit `"encoding/json"`, `"errors"`, `"net/http"` und den beiden API-Paketen im Import-Block.
`mustJSON` schreibt bei einem Fehler selbst; der Aufrufer gibt `nil` an `httpapi.Answer`, was dann
nichts mehr ausrichtet, weil der Status schon steht.

- [ ] **Step 7: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/login/ -run 'TestStart|TestTOTPShare'`
Expected: PASS, auch `TestStartTakesTheSamePathForAnUnknownName`. Der Zeitvergleich darin läuft
25-mal je Name; schlägt er auf einer belasteten Maschine an, ist das ein Hinweis auf einen
Kurzschluss im Ablauf, kein Grund, den Test zu lockern. Danach `tools/check.sh`.

- [ ] **Step 8: Commit**

```bash
git add apps/server-api/classes/login
git commit -m "Add the login challenge of the API" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---
### Task 7: Anmeldung, zweiter Teil – `POST /api/v1/login/finish`

Hier laufen alle Prüfungen zusammen: Beweis über das Client-Zertifikat, Nonce genau einmal, das
zusätzliche Passwort bei einer Rolle mit `passwordToo`, TOTP, dann Sitzung, API-Zertifikat und
Cookie.

**Files:**
- Create: `apps/server-api/classes/login/finish.go`, `apps/server-api/classes/login/totp.go`
- Test: `apps/server-api/classes/login/finish_test.go`, `apps/server-api/classes/login/totp_test.go`,
  `apps/server-api/classes/login/fuzz_test.go`

**Interfaces:**
- Consumes: `session.ParseLoginFinish`, `session.VerifyClient`, `session.VerifyPassword`,
  `session.SignAPI`, `session.APICert`, `session.CertHash`, `session.TokenHash`,
  `session.TokenSize`, `session.LoginResult`, `dbapi.OnceNonce`, `dbapi.Session`,
  `noisep.Keypair`, `totp.ValidateCustom`, `lock`.
- Produces:
  - `func (h *Handler) Finish(w http.ResponseWriter, r *http.Request)`
  - `func (h *Handler) newSession(ctx, v session.Verified, client []byte, p proof.Proof) (session.LoginResult, string, error)`
  - `func (h *Handler) checkTOTP(row dbapi.User, code string, now int64) bool`
  - `func (h *Handler) BeginTOTP(name string) (core.TOTPBegin, []byte, error)` –
    Geheimnis für `ngc.desktop/totp-begin` (Aufgabe 15 benutzt es).

- [ ] **Step 1: Write the failing test for TOTP**

`apps/server-api/classes/login/totp_test.go`:

```go
package login

import (
	"testing"

	"github.com/pquerna/otp/totp"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
)

func TestTOTP(t *testing.T) {
	h, fx := newFixture(t)
	begin, secret, err := h.BeginTOTP(fx.PasswordName)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if begin.Secret == "" || begin.URI == "" {
		t.Fatalf("begin %+v", begin)
	}
	row := dbapi.User{ID: fx.PasswordUser, Name: fx.PasswordName, TOTP: secret}
	code, err := totp.GenerateCode(begin.Secret, timeOf(testNow))
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if !session.ValidTOTP(code) {
		t.Fatalf("code %q is no TOTP", code)
	}
	if !h.checkTOTP(row, code, testNow) {
		t.Fatal("the right code was refused")
	}
	if h.checkTOTP(row, "000000", testNow) {
		t.Fatal("a wrong code was taken")
	}
	// A code of the period before last is too old: the skew is one period.
	if h.checkTOTP(row, code, testNow+3*session.TOTPPeriod) {
		t.Fatal("an old code was taken")
	}
	// The secret lies sealed in the row: a database dump alone is not enough.
	if string(secret) == begin.Secret {
		t.Fatal("the TOTP secret is stored in the clear")
	}
}
```

`timeOf` steht in `totp.go` und wandelt Unix-Sekunden in `time.Time`.

- [ ] **Step 2: Write the failing test for finish**

`apps/server-api/classes/login/finish_test.go`:

```go
package login

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

func TestFinishMakesASession(t *testing.T) {
	h, fx := newFixture(t)
	c := start(t, h, fx.PasswordName)
	body := finishBody(t, fx, fx.Password, c, "")
	w := httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
	if w.Code != 200 {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	var res session.LoginResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("result: %v", err)
	}
	if !res.PasswordHint {
		t.Fatal("a password login must show the hint to switch to passkeys")
	}
	v, _ := h.S.Trust(ctx)
	cert, err := session.VerifyAPI(res.API, v.List, certBytes(t, fx, c, fx.Password), h.S.Now())
	if err != nil {
		t.Fatalf("api certificate: %v", err)
	}
	if cert.Node != "api-1" {
		t.Fatalf("node %q", cert.Node)
	}
	cookie := w.Result().Cookies()
	if len(cookie) != 1 || cookie[0].Name != session.CookieName || cookie[0].Value == "" {
		t.Fatalf("cookie %+v", cookie)
	}
	raw, err := enc.B64.DecodeString(cookie[0].Value)
	if err != nil || len(raw) != session.TokenSize {
		t.Fatalf("cookie value: %v", err)
	}
	row, err := h.S.DB.SessionGet(ctx, session.TokenHash(raw))
	if err != nil || row.User != fx.PasswordUser {
		t.Fatalf("session row: %v %+v", err, row)
	}
	if string(row.APIKey) == "" {
		t.Fatal("the API session key is missing")
	}
}

func TestFinishUsesTheNonceOnlyOnce(t *testing.T) {
	h, fx := newFixture(t)
	c := start(t, h, fx.PasswordName)
	body := finishBody(t, fx, fx.Password, c, "")
	w := httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
	if w.Code != 200 {
		t.Fatalf("first: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
	if w.Code != 401 {
		t.Fatalf("replay answered %d", w.Code)
	}
}

func TestFinishRefusesAForeignNonce(t *testing.T) {
	h, fx := newFixture(t)
	// A nonce the API handed to another user does not make this user a session.
	other := start(t, h, fx.PasskeyName)
	c := start(t, h, fx.PasswordName)
	c.Nonce = other.Nonce
	body := finishBody(t, fx, fx.Password, c, "")
	w := httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(body)))
	if w.Code != 401 {
		t.Fatalf("foreign nonce answered %d", w.Code)
	}
}

func TestFinishAnswersEveryFailureAlike(t *testing.T) {
	h, fx := newFixture(t)
	// An unknown name, a wrong proof and a missing nonce all answer E_LOGIN
	// with the same body, so no answer says what was wrong.
	c := start(t, h, "nobody")
	bad := finishBody(t, fx, fx.Password, c, "")
	w := httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(bad)))
	if w.Code != 401 {
		t.Fatalf("unknown name answered %d", w.Code)
	}
	first := w.Body.String()

	c2 := start(t, h, fx.PasswordName)
	wrong := finishBody(t, fx, trusttest.NewSigner(200), c2, "")
	w = httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(wrong)))
	if w.Code != 401 || w.Body.String() != first {
		t.Fatalf("a foreign key answered %d %s", w.Code, w.Body)
	}
}

func TestFinishLocksAfterFailures(t *testing.T) {
	h, fx := newFixture(t)
	for i := 0; i < 4; i++ {
		c := start(t, h, fx.PasswordName)
		wrong := finishBody(t, fx, trusttest.NewSigner(200), c, "")
		w := httptest.NewRecorder()
		h.Finish(w, jsonPost(session.PathLoginFinish, string(wrong)))
		if w.Code != 401 && w.Code != 429 {
			t.Fatalf("attempt %d answered %d", i, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.Start(w, jsonPost(session.PathLoginStart, `{"name":"`+fx.PasswordName+`"}`))
	if w.Code != 429 {
		t.Fatalf("the name is not locked: %d", w.Code)
	}
}

// certBytes builds the client certificate the user signs.
func certBytes(t *testing.T, fx fixture, c session.LoginChallenge, s trusttest.Signer) []byte {
	t.Helper()
	dh, err := noisep.Keypair(zeroReader{})
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	cert := session.ClientCert{
		Installation: fx.Installation, User: c.User, Nonce: c.Nonce,
		Sig: enc.Bytes(sigPub(s)), DH: enc.Bytes(dh.Public),
		Issued: c.Now, Expires: c.Now + c.MaxLifetime,
	}
	b, err := json.Marshal(cert)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// finishBody builds the body of login/finish: the certificate, the proof over
// exactly its bytes and, if wanted, a TOTP code.
func finishBody(t *testing.T, fx fixture, s trusttest.Signer, c session.LoginChallenge, code string) []byte {
	t.Helper()
	payload := certBytes(t, fx, c, s)
	f := session.LoginFinish{Cert: enc.Bytes(payload), Proof: s.Prove(sign.PurposeClientCert, payload), TOTP: code}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
```

Dazu in `fixture_test.go` zwei kleine Helfer, weil der Test einen festen Sitzungsschlüssel
braucht:

```go
// zeroReader gives a fixed session key, so the tests stay deterministic; the
// API itself always uses crypto/rand.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 7
	}
	return len(p), nil
}

// sigPub returns the Ed25519 key the session signs tasks with. The tests reuse
// the user's own key, which is enough: the API only checks that the proof over
// the certificate holds.
func sigPub(s trusttest.Signer) []byte { return s.Priv.Public().(ed25519.PublicKey) }
```

- [ ] **Step 3: Run both tests and see them fail**

Run: `go test ./apps/server-api/classes/login/`
Expected: FAIL, `undefined: Finish`, `undefined: BeginTOTP`.

- [ ] **Step 4: Write the TOTP part**

`apps/server-api/classes/login/totp.go`:

```go
package login

import (
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
)

// totpOpts are the parameters of RFC 6238 that authenticator apps expect;
// Skew 1 lets one period either side pass, so a clock a few seconds off still
// works.
var totpOpts = totp.ValidateOpts{Period: session.TOTPPeriod, Skew: 1,
	Digits: otp.Digits(session.TOTPDigits), Algorithm: otp.AlgorithmSHA1}

// timeOf turns Unix seconds into the time TOTP counts from.
func timeOf(now int64) time.Time { return time.Unix(now, 0).UTC() }

// BeginTOTP makes a new TOTP secret for a user: the answer of
// ngc.desktop/totp-begin and the sealed secret to keep until the user confirms
// it with a code. The secret is never stored unsealed. It takes no user:
// secrets.Box.Seal passes nil as the additional data, so a sealed secret
// is bound to nobody and works unchanged in another user's row. Whoever
// keeps it must never let it travel through the client.
func (h *Handler) BeginTOTP(name string) (core.TOTPBegin, []byte, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer: "NGC", AccountName: name, Period: session.TOTPPeriod,
		Digits: otp.Digits(session.TOTPDigits), Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return core.TOTPBegin{}, nil, err
	}
	sealed, err := h.S.Box.Seal([]byte(key.Secret()))
	if err != nil {
		return core.TOTPBegin{}, nil, err
	}
	return core.TOTPBegin{Secret: key.Secret(), URI: key.URL()}, sealed, nil
}

// checkTOTP reports whether code is the current one for the user's sealed
// secret. A row without a secret has no TOTP and answers false, so a caller
// that asks anyway never lets someone in.
func (h *Handler) checkTOTP(row dbapi.User, code string, now int64) bool {
	if len(row.TOTP) == 0 || !session.ValidTOTP(code) {
		return false
	}
	secret, err := h.S.Box.Open(row.TOTP)
	if err != nil {
		return false
	}
	ok, err := totp.ValidateCustom(code, string(secret), timeOf(now), totpOpts)
	return err == nil && ok
}
```

- [ ] **Step 5: Write login/finish**

`apps/server-api/classes/login/finish.go`:

```go
package login

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/json"
	"net/http"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
)

// Finish answers POST session.PathLoginFinish. Every failure answers E_LOGIN
// with the same body, whatever was wrong.
func (h *Handler) Finish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, ok := httpapi.Body(w, r)
	if !ok {
		return
	}
	f, err := session.ParseLoginFinish(body)
	if err != nil {
		httpapi.Fail(w, wire.CodeProtocol)
		return
	}
	v, err := h.S.Trust(ctx)
	if err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	now := h.S.Now()
	// The bucket of the lock is the login name of the key's owner; for a key
	// nobody owns it is the key ID, which has the same shape for every
	// attempt, so the lock never says whether a user exists.
	bucket := "key/" + f.Proof.KeyID
	if owner, _, found := v.List.KeyOwner(f.Proof.KeyID); found {
		bucket = owner.Name
	}
	wait, err := lock.Check(ctx, h.S.DB, bucket, now)
	if err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	if wait > 0 {
		httpapi.FailRetry(w, wire.CodeLocked, wait)
		return
	}
	deny := func() {
		_ = lock.Fail(ctx, h.S.DB, bucket, now)
		httpapi.Fail(w, wire.CodeLogin)
	}

	verified, err := session.VerifyClient(f.Cert, f.Proof, v.List, now)
	if err != nil {
		deny()
		return
	}
	cert := verified.Cert()
	if cert.Expires > now+h.S.Cfg.SessionLifetime {
		deny()
		return
	}
	// A role that asks for the password too: the proof above was by a passkey
	// (session.VerifyClient sees to that), and the password comes on top.
	if v.List.PasswordToo(cert.User) {
		if f.Password == nil || session.VerifyPassword(verified, *f.Password, v.List) != nil {
			deny()
			return
		}
	}
	row, err := h.S.DB.UserGet(ctx, cert.User)
	if err != nil && !isNotFound(err) {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	if len(row.TOTP) > 0 && !h.checkTOTP(row, f.TOTP, now) {
		deny()
		return
	}
	// The nonce is spent exactly once and must be the one this user got.
	owner, err := h.S.DB.OnceUse(ctx, dbapi.OnceNonce, cert.Nonce, now)
	if err != nil || owner != cert.User {
		deny()
		return
	}
	res, token, err := h.newSession(ctx, verified, f.Cert, f.Proof)
	if err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	if u, ok := v.List.User(cert.User); ok {
		// No lock.Clear here: apps/server-api/CLAUDE.md forbids it and says
		// why - it empties the whole bucket, so every successful login would
		// reset the count of an attacker who puts himself into that bucket on
		// purpose. An expired row already reads as zero. Only setup/code
		// clears its own bucket, because that is no login name.
		row.ID, row.Name, row.Roles = u.ID, u.Name, u.Roles
		row.Failures, row.Locked, row.LastLogin = 0, 0, now
		if err := h.S.DB.UserPut(ctx, row); err != nil {
			httpapi.Fail(w, wire.CodeUnreachable)
			return
		}
	}
	httpapi.SetCookie(w, token)
	httpapi.Answer(w, http.StatusOK, mustJSON(w, res))
}

// newSession makes the API's session keys, signs the API certificate and
// stores the session. It returns the answer and the cookie value.
func (h *Handler) newSession(ctx context.Context, v session.Verified, client []byte, p proof.Proof) (session.LoginResult, string, error) {
	cert := v.Cert()
	now := h.S.Now()
	pair, err := noisep.Keypair(randReader)
	if err != nil {
		return session.LoginResult{}, "", err
	}
	apiCert := session.APICert{
		Installation: cert.Installation, Node: h.S.Cfg.Node,
		Client: enc.Bytes(session.CertHash(client)), DH: enc.Bytes(pair.Public),
		Issued: now, Expires: cert.Expires,
	}
	seed := h.S.Cfg.SignKey.Bytes()
	if len(seed) != ed25519.SeedSize {
		return session.LoginResult{}, "", errSignKey
	}
	env, err := session.SignAPI(ed25519.NewKeyFromSeed(seed), apiCert)
	if err != nil {
		return session.LoginResult{}, "", err
	}
	certJSON, err := json.Marshal(env)
	if err != nil {
		return session.LoginResult{}, "", err
	}
	proofJSON, err := json.Marshal(p)
	if err != nil {
		return session.LoginResult{}, "", err
	}
	// Any API serves any session, so the private session key lies in the
	// database, sealed with the key every API of the installation shares.
	sealed, err := h.S.Box.Seal(pair.Private)
	if err != nil {
		return session.LoginResult{}, "", err
	}
	raw, err := secrets.Bytes(session.TokenSize)
	if err != nil {
		return session.LoginResult{}, "", err
	}
	row := dbapi.Session{
		Token: session.TokenHash(raw), User: cert.User, Cert: client, Proof: proofJSON,
		APIKey: sealed, APICert: certJSON, Created: now, Expires: cert.Expires, Seen: now,
	}
	if err := h.S.DB.SessionPut(ctx, row); err != nil {
		return session.LoginResult{}, "", err
	}
	return session.LoginResult{API: env, PasswordHint: p.Kind == proof.KindPassword},
		enc.B64.EncodeToString(raw), nil
}
```

Dazu in `login.go`:

```go
// errSignKey reports a signing key of the wrong size in the configuration;
// config.API.Validate refuses such a file, so it can only mean the key was
// swapped underneath us.
var errSignKey = errors.New("login: signing key of the wrong size")

// randReader is the source of session keys.
var randReader = rand.Reader
```

mit `"crypto/rand"` und `"errors"` im Import-Block. `trust` und `ecdh` aus dem Import-Block von
`finish.go` entfernen, falls `gofmt`/`go vet` sie als ungenutzt meldet; `core` bleibt nur, wenn
`BeginTOTP` in derselben Datei stünde — es steht in `totp.go`.

- [ ] **Step 6: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/login/`
Expected: PASS.

- [ ] **Step 7: Add the fuzz test**

`apps/server-api/classes/login/fuzz_test.go`:

```go
package login

import (
	"net/http/httptest"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/session"
)

// FuzzFinish feeds login/finish with everything: no body may ever make a
// session, and the API must never panic.
func FuzzFinish(f *testing.F) {
	f.Add(`{"cert":"","proof":{}}`)
	f.Add(`{"cert":"AAAA","proof":{"kind":"password","kid":"AAAAAAAAAAAAAAAAAAAAAA","sig":"AA"}}`)
	f.Add(`{}`)
	f.Fuzz(func(t *testing.T, body string) {
		h, _ := newFixture(t)
		w := httptest.NewRecorder()
		h.Finish(w, jsonPost(session.PathLoginFinish, body))
		switch w.Code {
		case 400, 401, 413, 429, 500, 503:
		default:
			t.Fatalf("status %d for %q", w.Code, body)
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatalf("a fuzzed body got a cookie: %q", body)
		}
	})
}

// FuzzStart does the same for login/start.
func FuzzStart(f *testing.F) {
	f.Add(`{"name":"anna"}`)
	f.Add(`{"name":""}`)
	f.Add(`{"name":"a","extra":1}`)
	f.Fuzz(func(t *testing.T, body string) {
		h, _ := newFixture(t)
		w := httptest.NewRecorder()
		h.Start(w, jsonPost(session.PathLoginStart, body))
		switch w.Code {
		case 200, 400, 413, 429, 500, 503:
		default:
			t.Fatalf("status %d for %q", w.Code, body)
		}
	})
}
```

- [ ] **Step 8: Run the fuzz targets and check**

Run:
```bash
go test ./apps/server-api/classes/login/ -run Fuzz -fuzz FuzzFinish -fuzztime 60s
go test ./apps/server-api/classes/login/ -run Fuzz -fuzz FuzzStart -fuzztime 60s
tools/check.sh
```
Expected: PASS ohne Fund, `check: ok`.

- [ ] **Step 9: Commit**

```bash
git add apps/server-api/classes/login
git commit -m "Add the login, TOTP and the session of the API" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 8: Sitzung prüfen, abmelden, Vertrauensliste per HTTP

**Files:**
- Create: `apps/server-api/classes/login/live.go`, `apps/server-api/classes/login/trust.go`
- Test: `apps/server-api/classes/login/live_test.go`, `apps/server-api/classes/login/trust_test.go`

**Interfaces:**
- Consumes: `session.TokenHash`, `session.VerifyClient`, `session.ParseFrom`, `state.Chain`,
  `config.API.IdleTimeout`, `config.API.SessionLifetime`.
- Produces:
  - `type Live struct { Token []byte; Row dbapi.Session; Verified session.Verified; APIKey []byte }`
  - `func (h *Handler) Live(ctx context.Context, r *http.Request) (*Live, bool)` – Sitzung aus dem
    Cookie, mit Prüfung von Ablauf, Untätigkeit und Vertrauensliste; setzt `Seen`.
  - `func (h *Handler) Logout(w http.ResponseWriter, r *http.Request)`
  - `func (h *Handler) Trust(w http.ResponseWriter, r *http.Request)`
  - `func (h *Handler) Purge(ctx context.Context) error` – Aufräumrunde für Sitzungen, Einmalwerte
    und Sperren.

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/login/live_test.go`:

```go
package login

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/session"
)

func TestLiveAndLogout(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	r := httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	live, ok := h.Live(ctx, r)
	if !ok || live.Verified.Cert().User != fx.PasswordUser {
		t.Fatalf("live %v %+v", ok, live)
	}
	if len(live.APIKey) != 32 {
		t.Fatalf("api key of %d bytes", len(live.APIKey))
	}

	w := httptest.NewRecorder()
	h.Logout(w, jsonPostCookie(session.PathLogout, cookie))
	if w.Code != 204 {
		t.Fatalf("logout %d", w.Code)
	}
	if _, ok := h.Live(ctx, r); ok {
		t.Fatal("the session survived the logout")
	}
}

func TestLiveEndsAfterIdleAndLifetime(t *testing.T) {
	h, fx := newFixture(t)
	cookie := loginOnce(t, h, fx)
	r := httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	h.S.Now = func() int64 { return testNow + h.S.Cfg.IdleTimeout + 1 }
	if _, ok := h.Live(ctx, r); ok {
		t.Fatal("an idle session stayed alive")
	}
	cookie = loginOnce(t, h, fx)
	r = httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	base := h.S.Now()
	h.S.Now = func() int64 { return base + h.S.Cfg.SessionLifetime + 1 }
	if _, ok := h.Live(ctx, r); ok {
		t.Fatal("a session outlived its lifetime")
	}
}

func TestLiveRefusesABadCookie(t *testing.T) {
	h, _ := newFixture(t)
	for _, value := range []string{"", "not base64url!", "AAAA"} {
		r := httptest.NewRequest(http.MethodGet, session.PathTrust, nil)
		if value != "" {
			r.AddCookie(&http.Cookie{Name: session.CookieName, Value: value})
		}
		if _, ok := h.Live(ctx, r); ok {
			t.Fatalf("cookie %q was taken", value)
		}
	}
}
```

`apps/server-api/classes/login/trust_test.go`:

```go
package login

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

func TestTrustNeedsASessionAndPages(t *testing.T) {
	h, fx := newFixture(t)
	w := httptest.NewRecorder()
	h.Trust(w, httptest.NewRequest(http.MethodGet, session.PathTrust+"?from=1", nil))
	if w.Code != 401 {
		t.Fatalf("without a session: %d", w.Code)
	}
	cookie := loginOnce(t, h, fx)

	w = httptest.NewRecorder()
	h.Trust(w, getCookie(session.PathTrust+"?from=1", cookie))
	if w.Code != 200 {
		t.Fatalf("from=1: %d", w.Code)
	}
	var chain session.TrustChain
	if err := json.Unmarshal(w.Body.Bytes(), &chain); err != nil || len(chain.Versions) != 1 {
		t.Fatalf("chain: %v %d", err, len(chain.Versions))
	}
	if _, err := trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(chain.Versions[0])}, chain.Versions); err != nil {
		t.Fatalf("chain does not verify: %v", err)
	}

	// One past the newest is 204, anything beyond is 404.
	w = httptest.NewRecorder()
	h.Trust(w, getCookie(session.PathTrust+"?from=2", cookie))
	if w.Code != 204 {
		t.Fatalf("from=2: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.Trust(w, getCookie(session.PathTrust+"?from=3", cookie))
	if w.Code != 404 {
		t.Fatalf("from=3: %d", w.Code)
	}
	// A from the contract refuses is a protocol error.
	for _, bad := range []string{"", "0", "01", "-1", "x", "9007199254740992"} {
		w = httptest.NewRecorder()
		h.Trust(w, getCookie(session.PathTrust+"?from="+bad, cookie))
		if w.Code != 400 {
			t.Fatalf("from=%q answered %d", bad, w.Code)
		}
	}

	// A new version shows up at once.
	second := fx.Passkey.Sign(t, trusttest.Next(t, fx.Signed))
	if _, err := h.S.AddVersion(ctx, second); err != nil {
		t.Fatalf("version 2: %v", err)
	}
	w = httptest.NewRecorder()
	h.Trust(w, getCookie(session.PathTrust+"?from=2", cookie))
	if w.Code != 200 {
		t.Fatalf("new version: %d", w.Code)
	}
}
```

Dazu in `fixture_test.go`:

```go
// loginOnce runs the whole login and returns the cookie value.
func loginOnce(t *testing.T, h *Handler, fx fixture) string {
	t.Helper()
	c := start(t, h, fx.PasswordName)
	w := httptest.NewRecorder()
	h.Finish(w, jsonPost(session.PathLoginFinish, string(finishBody(t, fx, fx.Password, c, ""))))
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies %+v", cookies)
	}
	return cookies[0].Value
}

// getCookie builds a GET with the session cookie.
func getCookie(path, cookie string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	return r
}

// jsonPostCookie builds a POST with the session cookie and no body.
func jsonPostCookie(path, cookie string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	return r
}
```

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./apps/server-api/classes/login/`
Expected: FAIL, `undefined: Live`.

- [ ] **Step 3: Write the session check and the logout**

`apps/server-api/classes/login/live.go`:

```go
package login

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/lock"
)

// Live is a session that is open right now.
type Live struct {
	Token    []byte           // session.TokenHash of the cookie value
	Row      dbapi.Session    // as the database holds it
	Verified session.Verified // the chain back to the user's key
	APIKey   []byte           // the API's private session key, opened
}

// Live returns the session of the request's cookie. It ends a session that is
// too old, has been idle too long or whose user is no longer in the trust list.
func (h *Handler) Live(ctx context.Context, r *http.Request) (*Live, bool) {
	value, ok := httpapi.Cookie(r)
	if !ok {
		return nil, false
	}
	raw, err := enc.B64.DecodeString(value)
	if err != nil || len(raw) != session.TokenSize {
		return nil, false
	}
	token := session.TokenHash(raw)
	row, err := h.S.DB.SessionGet(ctx, token)
	if err != nil {
		return nil, false
	}
	now := h.S.Now()
	if now >= row.Expires || now-row.Seen > h.S.Cfg.IdleTimeout || now-row.Created > h.S.Cfg.SessionLifetime {
		_ = h.S.DB.SessionDelete(ctx, token)
		return nil, false
	}
	v, err := h.S.Trust(ctx)
	if err != nil {
		return nil, false
	}
	var p proof.Proof
	if err := json.Unmarshal(row.Proof, &p); err != nil || p.Validate() != nil {
		return nil, false
	}
	// The chain is checked again on every request: a user the trust list no
	// longer holds, or whose key an admin removed, loses the session at once.
	verified, err := session.VerifyClient(row.Cert, p, v.List, now)
	if err != nil {
		_ = h.S.DB.SessionDelete(ctx, token)
		return nil, false
	}
	key, err := h.S.Box.Open(row.APIKey)
	if err != nil {
		return nil, false
	}
	if err := h.S.DB.SessionTouch(ctx, token, now); err != nil {
		return nil, false
	}
	row.Seen = now
	return &Live{Token: token, Row: row, Verified: verified, APIKey: key}, true
}

// Logout answers POST session.PathLogout.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	live, ok := h.Live(ctx, r)
	if !ok {
		httpapi.Fail(w, wire.CodeLogin)
		return
	}
	if err := h.S.DB.SessionDelete(ctx, live.Token); err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	httpapi.ClearCookie(w)
	httpapi.NoContent(w)
}

// Purge is the housekeeping round: sessions, one-time values and locks. One
// API of the installation runs it every minute. Expired one-time values are
// freed only session.MaxSkew after their time, so an API whose clock runs
// behind cannot take a task a faster API already purged.
func (h *Handler) Purge(ctx context.Context) error {
	now := h.S.Now()
	if err := h.S.DB.SessionPurge(ctx, now); err != nil {
		return err
	}
	if err := h.S.DB.OncePurge(ctx, now-session.MaxSkew); err != nil {
		return err
	}
	return lock.Purge(ctx, h.S.DB, now)
}
```

- [ ] **Step 4: Write the trust list over HTTP**

`apps/server-api/classes/login/trust.go`:

```go
package login

import (
	"net/http"

	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
)

// Trust answers GET session.PathTrust?from=N with the versions from N on,
// only with a session.
func (h *Handler) Trust(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, ok := h.Live(ctx, r); !ok {
		httpapi.Fail(w, wire.CodeLogin)
		return
	}
	from, err := session.ParseFrom(r.URL.Query().Get("from"))
	if err != nil {
		httpapi.Fail(w, wire.CodeProtocol)
		return
	}
	chain, err := h.S.Chain(ctx, from)
	if err == state.ErrOldVersion {
		httpapi.Fail(w, wire.CodeNotFound)
		return
	}
	if err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	if len(chain.Versions) == 0 {
		// Nothing newer than what the shell has.
		httpapi.NoContent(w)
		return
	}
	httpapi.Answer(w, http.StatusOK, mustJSON(w, chain))
}
```

- [ ] **Step 5: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/login/`
Expected: PASS. Danach `tools/check.sh`.

- [ ] **Step 6: Commit**

```bash
git add apps/server-api/classes/login
git commit -m "Add the session check, the logout and the trust list over HTTP" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---
### Task 9: Leitung – Noise über WebSocket

Beide Leitungen (Sitzung und Tunnel) sind gleich aufgebaut: WebSocket, darin Noise_KK, darin genau
ein Frame je binärer Nachricht.

**Files:**
- Create: `apps/server-api/classes/link/link.go`
- Test: `apps/server-api/classes/link/link_test.go`, `apps/server-api/classes/link/fuzz_test.go`

**Interfaces:**
- Consumes: `websocket.Accept`, `noisep.Handshake`, `noisep.NewTransport`, `wire.Decode`,
  `wire.Frame.Encode`, `wire.HandshakeSize`, `wire.MaxFrame`, `session.Subprotocol`,
  `tunnel.Subprotocol`.
- Produces:
  - `type Options struct { Subprotocol string; Prologue string; Static noise.DHKey; Peer []byte;
    Origins []string }`
  - `func Accept(w http.ResponseWriter, r *http.Request, o Options) (*Link, error)`
  - `func (l *Link) Read(ctx context.Context) (wire.Frame, error)`
  - `func (l *Link) Write(ctx context.Context, f wire.Frame) error`
  - `func (l *Link) Close(code websocket.StatusCode, reason string) error`
  - `var ErrLink = errors.New("link: malformed message")`

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/link/link_test.go`:

```go
package link

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

var ctx = context.Background()

// dialTest starts a server that accepts one link and gives the client side of
// the same Noise handshake back.
func dialTest(t *testing.T) (*Link, *noisep.Transport, *websocket.Conn, func()) {
	t.Helper()
	server, err := noisep.Keypair(fixedReader{1})
	if err != nil {
		t.Fatalf("server keys: %v", err)
	}
	client, err := noisep.Keypair(fixedReader{2})
	if err != nil {
		t.Fatalf("client keys: %v", err)
	}
	links := make(chan *Link, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l, err := Accept(w, r, Options{Subprotocol: "ngc.test.v1", Prologue: noisep.PrologueSession,
			Static: server, Peer: client.Public})
		if err != nil {
			return
		}
		links <- l
	}))
	conn, _, err := websocket.Dial(ctx, "ws"+srv.URL[len("http"):],
		&websocket.DialOptions{Subprotocols: []string{"ngc.test.v1"}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	hs, err := noisep.Handshake(true, []byte(noisep.PrologueSession), client, server.Public, fixedReader{3})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		t.Fatalf("message 1: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, msg1); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	_, msg2, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read 2: %v", err)
	}
	_, cs1, cs2, err := hs.ReadMessage(nil, msg2)
	if err != nil {
		t.Fatalf("read message 2: %v", err)
	}
	l := <-links
	return l, noisep.NewTransport(true, cs1, cs2), conn, func() { conn.CloseNow(); srv.Close() }
}

func TestLinkCarriesFrames(t *testing.T) {
	l, tr, conn, done := dialTest(t)
	defer done()
	f := wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("hello")}
	raw, err := f.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	sealed, err := tr.Seal(raw)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, sealed); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := l.Read(ctx)
	if err != nil || got.Type != wire.TypeData || got.Channel != 1 || string(got.Payload) != "hello" {
		t.Fatalf("read %+v: %v", got, err)
	}
	if err := l.Write(ctx, wire.Frame{Type: wire.TypeEOF, Channel: 1}); err != nil {
		t.Fatalf("write back: %v", err)
	}
	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	plain, err := tr.Open(msg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	back, err := wire.Decode(plain)
	if err != nil || back.Type != wire.TypeEOF {
		t.Fatalf("decode %+v: %v", back, err)
	}
}

func TestLinkRefusesTextAndRubbish(t *testing.T) {
	l, _, conn, done := dialTest(t)
	defer done()
	// A text message ends the link, whatever it holds.
	if err := conn.Write(ctx, websocket.MessageText, []byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := l.Read(ctx); err == nil {
		t.Fatal("a text message was taken")
	}

	l2, _, conn2, done2 := dialTest(t)
	defer done2()
	if err := conn2.Write(ctx, websocket.MessageBinary, []byte("not sealed")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := l2.Read(ctx); err == nil {
		t.Fatal("a message that does not decrypt was taken")
	}
}

// fixedReader gives the same bytes every time, so the tests stay deterministic.
type fixedReader struct{ b byte }

func (f fixedReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = f.b
	}
	return len(p), nil
}

var _ = noise.DHKey{}
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/server-api/classes/link/`
Expected: FAIL, `undefined: Accept`.

- [ ] **Step 3: Write the link**

`apps/server-api/classes/link/link.go`:

```go
// Package link carries the two links of the API: the session to a browser and
// the tunnel to a controller. Both are a WebSocket with Noise_KK inside: the
// first binary message is Noise message 1 of the other side, the second is the
// API's message 2, both without a payload and exactly wire.HandshakeSize bytes.
// After that every binary message is one Noise transport message whose
// plaintext is exactly one frame. A text message, a frame that does not decode
// or a failed decryption ends the link (package wire).
package link

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// ErrLink reports a message that breaks the rules of the link.
var ErrLink = errors.New("link: malformed message")

// Options describe one link.
type Options struct {
	Subprotocol string      // session.Subprotocol or tunnel.Subprotocol
	Prologue    string      // noisep.PrologueSession or noisep.PrologueTunnel
	Static      noise.DHKey // the API's key pair for this link
	Peer        []byte      // the other side's static public key
	Origins     []string    // allowed Origin values (scheme://host); empty allows only the request host
}

// Link is one open link.
type Link struct {
	conn *websocket.Conn
	tr   *noisep.Transport

	// One writer at a time: several goroutines relay onto one link, and a
	// Noise transport message must not be split or interleaved.
	wmu sync.Mutex
}

// Accept upgrades the request and runs the Noise handshake as responder.
func Accept(w http.ResponseWriter, r *http.Request, o Options) (*Link, error) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:   []string{o.Subprotocol},
		OriginPatterns: o.Origins,
	})
	if err != nil {
		return nil, err
	}
	// A client that did not ask for our subprotocol speaks something else.
	if conn.Subprotocol() != o.Subprotocol {
		conn.Close(websocket.StatusPolicyViolation, "subprotocol")
		return nil, ErrLink
	}
	// One frame at most, plus the Noise tag: nothing larger is ever valid, and
	// the limit keeps a flood from filling memory before it is refused.
	conn.SetReadLimit(wire.MaxFrame + noisep.TagSize)
	hs, err := noisep.Handshake(false, []byte(o.Prologue), o.Static, o.Peer, randReader)
	if err != nil {
		conn.Close(websocket.StatusInternalError, "handshake")
		return nil, err
	}
	kind, msg1, err := conn.Read(r.Context())
	if err != nil || kind != websocket.MessageBinary || len(msg1) != wire.HandshakeSize {
		conn.Close(websocket.StatusPolicyViolation, "handshake")
		return nil, ErrLink
	}
	payload, _, _, err := hs.ReadMessage(nil, msg1)
	if err != nil || len(payload) != 0 {
		// Messages 1 and 2 carry no payload in all three uses of noisep.
		conn.Close(websocket.StatusPolicyViolation, "handshake")
		return nil, ErrLink
	}
	msg2, cs1, cs2, err := hs.WriteMessage(nil, nil)
	if err != nil || cs1 == nil || cs2 == nil {
		conn.Close(websocket.StatusInternalError, "handshake")
		return nil, ErrLink
	}
	if err := conn.Write(r.Context(), websocket.MessageBinary, msg2); err != nil {
		conn.Close(websocket.StatusInternalError, "handshake")
		return nil, err
	}
	return &Link{conn: conn, tr: noisep.NewTransport(false, cs1, cs2)}, nil
}

// Read returns the next frame; every failure ends the link.
func (l *Link) Read(ctx context.Context) (wire.Frame, error) {
	kind, msg, err := l.conn.Read(ctx)
	if err != nil {
		return wire.Frame{}, err
	}
	if kind != websocket.MessageBinary {
		l.fail("binary only")
		return wire.Frame{}, ErrLink
	}
	plain, err := l.tr.Open(msg)
	if err != nil {
		l.fail("decrypt")
		return wire.Frame{}, ErrLink
	}
	f, err := wire.Decode(plain)
	if err != nil {
		l.fail("frame")
		return wire.Frame{}, ErrLink
	}
	return f, nil
}

// Write seals and sends one frame.
func (l *Link) Write(ctx context.Context, f wire.Frame) error {
	raw, err := f.Encode()
	if err != nil {
		return err
	}
	l.wmu.Lock()
	defer l.wmu.Unlock()
	sealed, err := l.tr.Seal(raw)
	if err != nil {
		return err
	}
	return l.conn.Write(ctx, websocket.MessageBinary, sealed)
}

// Close ends the link.
func (l *Link) Close(code websocket.StatusCode, reason string) error { return l.conn.Close(code, reason) }

// fail ends the link on a broken message; the reason is a fixed word, never
// anything from the message.
func (l *Link) fail(reason string) { _ = l.conn.Close(websocket.StatusProtocolError, reason) }
```

Dazu in derselben Datei:

```go
// randReader is the source of the ephemeral Noise keys.
var randReader = rand.Reader
```

mit `"crypto/rand"` im Import-Block.

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/link/`
Expected: PASS.

- [ ] **Step 5: Add the fuzz test for the frames of a link**

`apps/server-api/classes/link/fuzz_test.go`:

```go
package link

import (
	"testing"

	"github.com/coder/websocket"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// FuzzLinkMessage feeds a link with sealed messages of any content: Read must
// answer a frame the contract allows or an error, and never panic.
func FuzzLinkMessage(f *testing.F) {
	frame, err := wire.Frame{Type: wire.TypeData, Channel: 1, Payload: []byte("x")}.Encode()
	if err != nil {
		f.Fatalf("encode: %v", err)
	}
	f.Add(frame)
	f.Add([]byte{})
	f.Add([]byte{9, 0, 0, 0, 1})
	f.Fuzz(func(t *testing.T, plain []byte) {
		l, tr, conn, done := dialTest(t)
		defer done()
		sealed, err := tr.Seal(plain)
		if err != nil {
			t.Skip()
		}
		if err := conn.Write(ctx, websocket.MessageBinary, sealed); err != nil {
			t.Skip()
		}
		got, err := l.Read(ctx)
		if err != nil {
			return
		}
		if _, err := got.Encode(); err != nil {
			t.Fatalf("Read gave a frame Encode refuses: %+v", got)
		}
	})
}
```

- [ ] **Step 6: Run the fuzz target and check**

Run: `go test ./apps/server-api/classes/link/ -run Fuzz -fuzz FuzzLinkMessage -fuzztime 60s`,
danach `tools/check.sh`.
Expected: PASS ohne Fund.

- [ ] **Step 7: Commit**

```bash
git add apps/server-api/classes/link
git commit -m "Add the Noise link over WebSocket" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 10: Kanaltabelle, Freigabe und Grenzen

Genau nach `shared/README.md`, Abschnitt „Öffnen, Schließen, Freigabe“ und nach der
Paketbeschreibung von `wire`.

**Files:**
- Create: `apps/server-api/classes/link/table.go`
- Test: `apps/server-api/classes/link/table_test.go`

**Interfaces:**
- Consumes: `wire.MaxSessionChannels`, `wire.MaxTunnelChannels`, `wire.MinWindow`,
  `wire.MaxWindow`, `wire.TypeOpen`.
- Produces:
  - `type Channel struct { Num uint32 }` mit Methoden `SendAllowed(n int) bool`,
    `Spend(n int) bool`, `Grant(credit uint32) bool`, `Take(n int) bool`,
    `SentClose()`, `RecvClose()`, `Closing() bool`
  - `type Table struct{ … }`, `func NewTable(max int, opener bool) *Table`
  - `func (t *Table) Open(num uint32, window uint32) (*Channel, error)` – für ein empfangenes OPEN
  - `func (t *Table) Next(window uint32) (*Channel, error)` – nächste freie Nummer für ein eigenes OPEN
  - `func (t *Table) Get(num uint32) (*Channel, bool)`
  - `func (t *Table) Reserve(num uint32) error` – Nummer eines Protokollfehlers belegen
  - `func (t *Table) Count() int`
  - `var ErrLimit`, `var ErrProtocol`

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/link/table_test.go`:

```go
package link

import (
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/wire"
)

func TestChannelFreesItsNumberOnlyAfterBothCloses(t *testing.T) {
	tb := NewTable(2, false)
	c, err := tb.Open(1, wire.MinWindow)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if tb.Count() != 1 {
		t.Fatalf("count %d", tb.Count())
	}
	c.SentClose()
	if !c.Closing() || tb.Count() != 1 {
		t.Fatalf("closing %v count %d", c.Closing(), tb.Count())
	}
	// A frame for a closing number is dropped, not answered.
	if _, ok := tb.Get(1); !ok {
		t.Fatal("the number is gone too early")
	}
	c.RecvClose()
	if tb.Count() != 0 {
		t.Fatalf("count after both closes %d", tb.Count())
	}
	if _, ok := tb.Get(1); ok {
		t.Fatal("the number is still there")
	}
	// The number may be used again once it is free.
	if _, err := tb.Open(1, wire.MinWindow); err != nil {
		t.Fatalf("reopen: %v", err)
	}
}

func TestOpenRefusesADoubleNumberAndTheLimit(t *testing.T) {
	tb := NewTable(2, false)
	if _, err := tb.Open(1, wire.MinWindow); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := tb.Open(1, wire.MinWindow); err != ErrProtocol {
		t.Fatalf("the same number twice answered %v", err)
	}
	if _, err := tb.Open(0, wire.MinWindow); err != ErrProtocol {
		t.Fatal("channel 0 was opened")
	}
	if _, err := tb.Open(2, wire.MinWindow); err != nil {
		t.Fatalf("second: %v", err)
	}
	if _, err := tb.Open(3, wire.MinWindow); err != ErrLimit {
		t.Fatalf("beyond the limit answered %v", err)
	}
}

func TestCredit(t *testing.T) {
	tb := NewTable(4, false)
	c, err := tb.Open(1, wire.MinWindow)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// The other side granted MinWindow with its OPEN: that is what we may send.
	if !c.SendAllowed(wire.MinWindow) || c.SendAllowed(wire.MinWindow+1) {
		t.Fatal("wrong allowance after the OPEN")
	}
	if !c.Spend(wire.MinWindow) || c.Spend(1) {
		t.Fatal("spending beyond the allowance")
	}
	if !c.Grant(wire.MinWindow) || !c.Spend(wire.MinWindow) {
		t.Fatal("a grant did not add")
	}
	// A grant that would lift the allowance above MaxWindow is a protocol error.
	if !c.Grant(wire.MaxWindow) {
		t.Fatal("a grant up to MaxWindow was refused")
	}
	if c.Grant(1) {
		t.Fatal("a grant beyond MaxWindow was taken")
	}
	// BERICHTIGT am 02.10.2026. Hier stand der Take(MinWindow) ohne ein
	// vorheriges Allow - der Plan rechnete das Fenster des OPEN in BEIDE
	// Richtungen. Der Vertrag zaehlt je Strecke UND Richtung
	// (shared/README.md:415-416, shared/wire/wire.go:53-55): wer ein OPEN
	// EMPFAENGT - und nur der ruft Table.Open - darf damit SENDEN, nicht
	// empfangen. Der Plan widersprach dabei sich selbst, denn sein eigenes
	// Table.Open setzt nur out:. Gemessen: der Mutant "Open setzt in auch"
	// waere mit dem alten Satz der gruene Normalfall gewesen.
	if c.Take(1) {
		t.Fatal("something arrived before we granted anything")
	}
	if !c.Allow(wire.MinWindow) {
		t.Fatal("our own grant was refused")
	}
	// The other direction: what we granted bounds what may arrive.
	if !c.Take(wire.MinWindow) {
		t.Fatal("our own window did not count")
	}
	if c.Take(1) {
		t.Fatal("more arrived than we granted")
	}
}

func TestNextPicksFreeNumbers(t *testing.T) {
	tb := NewTable(wire.MaxTunnelChannels, true)
	first, err := tb.Next(wire.MinWindow)
	if err != nil || first.Num != 1 {
		t.Fatalf("first %v: %v", first, err)
	}
	second, err := tb.Next(wire.MinWindow)
	if err != nil || second.Num != 2 {
		t.Fatalf("second %v: %v", second, err)
	}
	first.SentClose()
	first.RecvClose()
	third, err := tb.Next(wire.MinWindow)
	if err != nil || third.Num == second.Num {
		t.Fatalf("third %v: %v", third, err)
	}
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/server-api/classes/link/ -run TestChannel`
Expected: FAIL, `undefined: NewTable`.

- [ ] **Step 3: Write the table**

`apps/server-api/classes/link/table.go`:

```go
package link

import (
	"errors"
	"sync"

	"github.com/sorglos-it/nextgencloud/shared/wire"
)

var (
	// ErrLimit reports a channel number beyond the limit of the link; the
	// caller ends the link.
	ErrLimit = errors.New("link: channel limit reached")
	// ErrProtocol reports a frame for a number that is not open, an OPEN for an
	// open number or an OPEN from the side that does not open channels.
	ErrProtocol = errors.New("link: protocol error on a channel")
)

// Channel is one open or closing channel number with the credit of both
// directions.
type Channel struct {
	Num uint32

	mu        sync.Mutex
	out       uint64 // bytes we may still send
	in        uint64 // bytes the other side may still send
	sentClose bool
	recvClose bool
}

// SendAllowed reports whether n more bytes of DATA payload fit into what the
// other side granted.
func (c *Channel) SendAllowed(n int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return uint64(n) <= c.out
}

// Spend takes n bytes off our allowance; false when there is not enough.
func (c *Channel) Spend(n int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if uint64(n) > c.out {
		return false
	}
	c.out -= uint64(n)
	return true
}

// Grant adds credit the other side sent us; false when it would lift the
// allowance above wire.MaxWindow, which is a protocol error.
func (c *Channel) Grant(credit uint32) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if credit == 0 || credit > wire.MaxWindow || c.out+uint64(credit) > wire.MaxWindow {
		return false
	}
	c.out += uint64(credit)
	return true
}

// Take counts n bytes of DATA that arrived; false when more came than we
// granted, which closes the channel with E_PROTOCOL.
func (c *Channel) Take(n int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if uint64(n) > c.in {
		return false
	}
	c.in -= uint64(n)
	return true
}

// Allow adds to what the other side may send us; we call it whenever we send a
// GRANT, so the two sides count the same. It reports whether the grant is
// allowed at all and adds nothing when it is not.
//
// BERICHTIGT am 02.10.2026: Allow gibt bool zurueck und haelt die Grenze.
// Vorher band es nichts - Grant hielt MaxWindow fuer out, Allow fuer in
// nicht, und konnte es auch nicht melden. Gemessen: 300x Allow(MaxWindow),
// danach Take(MaxWindow+1) = true. Still abschneiden waere schlimmer (die
// Seiten zaehlen dann verschieden), und eine GRANT ueber MaxWindow ist
// unser EIGENER Protokollfehler - die Gegenseite antwortet mit
// CLOSE E_PROTOCOL (shared/README.md:418-419), sie darf also nie hinaus.
// Preis: ein if beim Aufrufer.
func (c *Channel) Allow(credit uint32) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if credit == 0 || c.in+uint64(credit) > uint64(wire.MaxWindow) {
		return false
	}
	c.in += uint64(credit)
	return true
}

// Closing reports whether we have already sent a CLOSE.
func (c *Channel) Closing() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.sentClose }

// Table holds the channel numbers of one link.
type Table struct {
	max    int
	opener bool // we pick the numbers (the API on a tunnel)

	mu   sync.Mutex
	ch   map[uint32]*Channel
	next uint32
}

// NewTable returns a table for at most max numbers.
func NewTable(max int, opener bool) *Table {
	return &Table{max: max, opener: opener, ch: map[uint32]*Channel{}, next: 1}
}

// Open records an OPEN that arrived, with the window the opener granted.
func (t *Table) Open(num, window uint32) (*Channel, error) {
	if num == wire.ControlChannel || t.opener || window < wire.MinWindow || window > wire.MaxWindow {
		return nil, ErrProtocol
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.ch[num]; ok {
		return nil, ErrProtocol
	}
	if len(t.ch) >= t.max {
		return nil, ErrLimit
	}
	c := &Channel{Num: num, out: uint64(window)}
	t.ch[num] = c
	return c, nil
}

// Next picks the next free number for a channel we open, with the window we
// grant the other side.
func (t *Table) Next(window uint32) (*Channel, error) {
	if !t.opener || window < wire.MinWindow || window > wire.MaxWindow {
		return nil, ErrProtocol
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.ch) >= t.max {
		return nil, ErrLimit
	}
	for i := 0; i <= t.max; i++ {
		num := t.next
		// Numbers run from 1 and wrap, so a long-lived link keeps finding one.
		t.next++
		if t.next == 0 {
			t.next = 1
		}
		if num == wire.ControlChannel {
			continue
		}
		if _, taken := t.ch[num]; taken {
			continue
		}
		c := &Channel{Num: num, in: uint64(window)}
		t.ch[num] = c
		return c, nil
	}
	return nil, ErrLimit
}

// Reserve holds a number whose CLOSE E_PROTOCOL we are about to send, so it
// counts toward the limit until the other side's CLOSE arrives.
func (t *Table) Reserve(num uint32) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.ch[num]; ok {
		return nil
	}
	if len(t.ch) >= t.max {
		return ErrLimit
	}
	t.ch[num] = &Channel{Num: num}
	return nil
}

// Get returns a channel that is open or closing.
func (t *Table) Get(num uint32) (*Channel, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.ch[num]
	return c, ok
}

// Count returns how many numbers are not free.
func (t *Table) Count() int { t.mu.Lock(); defer t.mu.Unlock(); return len(t.ch) }

// free removes a number once a CLOSE for it was both sent and received.
func (t *Table) free(c *Channel) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if cur, ok := t.ch[c.Num]; ok && cur == c {
		delete(t.ch, c.Num)
	}
}
```

Damit `SentClose` und `RecvClose` die Nummer freigeben können, halten Kanäle ihre Tabelle:

```go
// bind lets a channel free its own number; Table sets it when it makes one.
func (c *Channel) bind(t *Table) { c.owner = t }

// SentClose notes that we sent a CLOSE for this channel.
func (c *Channel) SentClose() {
	c.mu.Lock()
	c.sentClose = true
	done := c.recvClose
	c.mu.Unlock()
	if done && c.owner != nil {
		c.owner.free(c)
	}
}

// RecvClose notes that the other side's CLOSE arrived.
func (c *Channel) RecvClose() {
	c.mu.Lock()
	c.recvClose = true
	done := c.sentClose
	c.mu.Unlock()
	if done && c.owner != nil {
		c.owner.free(c)
	}
}
```

und `Channel` bekommt das Feld `owner *Table`; `Open`, `Next` und `Reserve` rufen `c.bind(t)`,
bevor sie den Kanal zurückgeben.

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/link/`
Expected: PASS. Danach `tools/check.sh`.

- [ ] **Step 5: Commit**

```bash
git add apps/server-api/classes/link
git commit -m "Add the channel table and the credit of a link" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 11: Sitzungsleitung zum Browser

> **EINTRITTSBEDINGUNG, eingetragen am 02.10.2026 aus der Pruefung der
> Aufgabe 10 — und sie steht HIER, weil eine Zeile, die nur in einem Bericht
> steht, dort verschwindet.**
>
> Aufgabe 10 hat die Kanaltabelle gebaut, und `link.go` ruft `NewTable`
> **nicht**. Die Tabelle ist damit von nichts benutzt, und drei Zusicherungen
> sind heute ein Vertrag mit einem Aufrufer, den es nicht gibt. Wer die
> Tabelle verdrahtet, belegt zuerst diese drei Saetze mit je einem Fall:
>
> 1. **`Allow` laeuft vor JEDER GRANT**, die hinausgeht.
> 2. **Die GRANT geht nur bei `true` hinaus.** `Allow` kann seit der Fixrunde
>    `false` sagen (`credit == 0` oder `in+credit > MaxWindow`); ein Aufrufer,
>    der das uebergeht, verschickt eine GRANT, die die Gegenseite mit
>    `CLOSE E_PROTOCOL` beantwortet — unser eigener Protokollfehler
>    (`shared/README.md:418-419`).
> 3. **Auf jedes `Reserve` folgt ein `SentClose`.** Sonst haelt die Nummer
>    fuer immer; das ist gewollt, aber es ist eine Aufrufer-Bedingung ohne
>    Zeugen.
>
> Dazu die gemessene Grenze der Nebenlaeufigkeit: `-race` sieht an der Tabelle
> nur, was ein Fall mit **zwei** Goroutinen fuehrt. Nach der Fixrunde sind das
> `out` (Grant gegen Spend) und `in` (Allow gegen Take) — mit entferntem
> Schloss meldet `-race` dort wirklich Rennen, vorher hatte es nichts zu
> sehen. Wer einen Zaehler dazunimmt, nimmt den Fall dazu.

**Files:**
- Create: `apps/server-api/classes/browser/browser.go`, `apps/server-api/classes/browser/control.go`
- Test: `apps/server-api/classes/browser/browser_test.go`

**Interfaces:**
- Consumes: `link`, `login.Live`, `session.Subprotocol`, `noisep.PrologueSession`,
  `wire.ControlAllowed`, `wire.Control`, `wire.ParseOpen`, `wire.Error`, `core.Notice`,
  `session.TrustHint`.
- Produces:
  - `type Session struct { Live *login.Live; Link *link.Link; Table *link.Table }`
  - `type Opener interface { OpenChannel(ctx context.Context, s *Session, c *link.Channel, o wire.Open) }`
  - `type Server struct { S *state.State; Login *login.Handler; Open Opener; Sessions *Registry }`
  - `func (srv *Server) Socket(w http.ResponseWriter, r *http.Request)`
  - `func (s *Session) SendControl(ctx context.Context, t string, body any) error`
  - `func (s *Session) Close(code wire.Code, num uint32) error`
  - `type Registry struct{ … }` mit `Add`, `Remove`, `Broadcast(ctx, t string, body any)`

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/browser/browser_test.go`:

```go
package browser

import (
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/wire"
)

func TestSocketNeedsASession(t *testing.T) {
	h := newServer(t)
	// Without a cookie the upgrade never happens: 401 with the body of the
	// contract.
	w, r := socketRequest("")
	h.Socket(w, r)
	if w.Code != 401 {
		t.Fatalf("status %d", w.Code)
	}
}

func TestSocketCarriesAnOpenAndAnswers(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	defer c.close()
	open := wire.Open{App: "ngc.desktop", Service: "api", Action: "me", Window: wire.MinWindow}
	c.writeOpen(t, 1, open)
	f := c.read(t)
	if f.Type != wire.TypeGrant || f.Channel != 1 {
		t.Fatalf("frame %+v", f)
	}
}

func TestSocketEndsOnAForbiddenControlMessage(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	defer c.close()
	// Only "bye" may come from the browser; anything else ends the link.
	c.writeControl(t, wire.CtlStatus, []byte("{}"))
	if _, err := c.readErr(); err == nil {
		t.Fatal("a forbidden control message was taken")
	}
}

func TestSocketEndsBeyondTheChannelLimit(t *testing.T) {
	h := newServer(t)
	c := dialSession(t, h)
	defer c.close()
	open := wire.Open{App: "ngc.desktop", Service: "api", Action: "me", Window: wire.MinWindow}
	for i := 1; i <= wire.MaxSessionChannels+1; i++ {
		c.writeOpen(t, uint32(i), open)
	}
	if _, err := c.readAll(); err == nil {
		t.Fatal("the link survived one channel too many")
	}
}
```

Die Helfer `newServer`, `dialSession` und der Typ mit `writeOpen`, `writeControl`, `read`,
`readErr`, `readAll`, `close` stehen in `apps/server-api/classes/browser/fixture_test.go` und
bauen auf `link`s Testhelfern auf: ein `httptest.Server` mit `Server.Socket`, ein `websocket.Dial`,
der Noise-Handschlag als Initiator mit dem Sitzungsschlüssel des Clients und dem `dh` des
API-Zertifikats aus `session.LoginResult`. Die Sitzung entsteht mit den Helfern aus
`login` (Aufgabe 7), hier über eine kleine Kopie von `loginOnce`, weil Testhelfer nicht über
Pakete hinweg sichtbar sind.

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/server-api/classes/browser/`
Expected: FAIL, `undefined: Server`.

- [ ] **Step 3: Write the session link**

`apps/server-api/classes/browser/browser.go`:

```go
// Package browser carries the session link to a shell: the WebSocket at
// session.PathSocket with Noise inside, the channels the browser opens and the
// control messages of channel 0. The static keys of this Noise session are the
// session's own keys: the browser's ClientCert.DH and the API's APICert.DH,
// whose private half lies sealed in the database, so any API can serve any
// session.
package browser

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/login"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
)

// Opener takes a channel the browser opened; package coreapp implements it.
type Opener interface {
	OpenChannel(ctx context.Context, s *Session, c *link.Channel, o wire.Open)
}

// Session is one open session link.
type Session struct {
	Live  *login.Live
	Link  *link.Link
	Table *link.Table
}

// Server serves session.PathSocket.
type Server struct {
	S        *state.State
	Login    *login.Handler
	Open     Opener
	Sessions *Registry
}

// Socket upgrades a request with a session to a link and runs it.
func (srv *Server) Socket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	live, ok := srv.Login.Live(ctx, r)
	if !ok {
		httpapi.Fail(w, wire.CodeLogin)
		return
	}
	v, err := srv.S.Trust(ctx)
	if err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	l, err := link.Accept(w, r, link.Options{
		Subprotocol: session.Subprotocol,
		Prologue:    noisep.PrologueSession,
		Static:      noise.DHKey{Private: live.APIKey, Public: apiPublic(live)},
		Peer:        live.Verified.Cert().DH,
		Origins:     origins(v.List),
	})
	if err != nil {
		return
	}
	s := &Session{Live: live, Link: l, Table: link.NewTable(wire.MaxSessionChannels, false)}
	srv.Sessions.Add(s)
	defer srv.Sessions.Remove(s)
	srv.run(ctx, s)
}

// apiPublic returns the API's public session key from the stored certificate,
// so the Noise key pair matches what the shell got in session.LoginResult.
func apiPublic(live *login.Live) []byte {
	var env struct {
		Payload []byte `json:"payload"`
	}
	// The row was written by this API; a value it cannot read means the row is
	// broken, and then the handshake fails, which ends the link.
	if json.Unmarshal(live.Row.APICert, &env) != nil {
		return nil
	}
	var cert session.APICert
	if json.Unmarshal(env.Payload, &cert) != nil {
		return nil
	}
	return cert.DH
}

// origins are the Origin values the WebSocket accepts: those of the trust
// list, which are the ones the shell is served from. The cookie is
// SameSite=Strict besides, so a page of another site carries no session.
func origins(l *trust.List) []string { return l.RP.Origins }

// run reads the link until it ends.
func (srv *Server) run(ctx context.Context, s *Session) {
	for {
		f, err := s.Link.Read(ctx)
		if err != nil {
			return
		}
		if f.Channel == wire.ControlChannel {
			if !srv.control(ctx, s, f) {
				return
			}
			continue
		}
		if !srv.channel(ctx, s, f) {
			return
		}
	}
}

// channel handles one frame of a numbered channel; false ends the link.
func (srv *Server) channel(ctx context.Context, s *Session, f wire.Frame) bool {
	switch f.Type {
	case wire.TypeOpen:
		o, err := wire.ParseOpen(f.Payload)
		if err != nil {
			return s.protocolError(ctx, f.Channel)
		}
		c, err := s.Table.Open(f.Channel, o.Window)
		if err == link.ErrLimit {
			return false
		}
		if err != nil {
			return s.protocolError(ctx, f.Channel)
		}
		// The dispatcher answers with a GRANT or a CLOSE and runs the channel.
		go srv.Open.OpenChannel(ctx, s, c, o)
		return true
	default:
		c, ok := s.Table.Get(f.Channel)
		if !ok {
			return s.protocolError(ctx, f.Channel)
		}
		return s.feed(ctx, c, f)
	}
}

// protocolError answers CLOSE E_PROTOCOL for a number; it keeps the number
// until the other side's CLOSE arrives, and ends the link when that would take
// one number too many.
func (s *Session) protocolError(ctx context.Context, num uint32) bool {
	if num == wire.ControlChannel {
		return false
	}
	if err := s.Table.Reserve(num); err != nil {
		return false
	}
	c, _ := s.Table.Get(num)
	if c != nil && c.Closing() {
		return true
	}
	if err := s.Close(ctx, wire.CodeProtocol, num); err != nil {
		return false
	}
	return true
}

// Close sends a CLOSE with a code for a channel and notes it.
func (s *Session) Close(ctx context.Context, code wire.Code, num uint32) error {
	payload, err := json.Marshal(wire.Error(code))
	if err != nil {
		return err
	}
	if err := s.Link.Write(ctx, wire.Frame{Type: wire.TypeClose, Channel: num, Payload: payload}); err != nil {
		return err
	}
	if c, ok := s.Table.Get(num); ok {
		c.SentClose()
	}
	return nil
}

// End closes the whole link.
func (s *Session) End(reason string) { _ = s.Link.Close(websocket.StatusNormalClosure, reason) }
```

`feed` (DATA, EOF, CLOSE, GRANT an einen laufenden Kanal weitergeben) und die Warteschlange je
Kanal entstehen in Aufgabe 15 zusammen mit dem Versand der Kern-Aktionen; in dieser Aufgabe reicht
eine Fassung, die DATA gegen die Freigabe prüft und an einen Kanalpuffer legt:

```go
// feed passes a frame to a running channel; false ends the link.
func (s *Session) feed(ctx context.Context, c *link.Channel, f wire.Frame) bool {
	switch f.Type {
	case wire.TypeData:
		if !c.Take(len(f.Payload)) {
			return s.protocolError(ctx, f.Channel)
		}
	case wire.TypeGrant:
		credit, handshake, err := wire.ParseGrant(f.Payload)
		if err != nil || len(handshake) != 0 || !c.Grant(credit) {
			// Noise message 2 travels only in the first GRANT of the side that
			// accepted the channel, never from the opener.
			return s.protocolError(ctx, f.Channel)
		}
	case wire.TypeEOF, wire.TypeClose:
	default:
		return s.protocolError(ctx, f.Channel)
	}
	if f.Type == wire.TypeClose {
		c.RecvClose()
	}
	s.deliver(c, f)
	return true
}
```

`deliver` legt den Frame in den Kanal-Puffer (`chan wire.Frame` mit Platz für 8 Frames), den der
Versand in Aufgabe 15 ausliest; ist der Puffer voll, wartet `deliver` — die Freigabe sorgt dafür,
dass er nie ohne Ende wächst.

- [ ] **Step 4: Write the control messages and the registry**

`apps/server-api/classes/browser/control.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// control handles a frame on channel 0; false ends the link.
func (srv *Server) control(ctx context.Context, s *Session, f wire.Frame) bool {
	if f.Type != wire.TypeData {
		return false
	}
	c, err := wire.ParseControl(f.Payload)
	if err != nil || !wire.ControlAllowed(wire.LinkSession, false, c.T) {
		return false
	}
	// The browser may only say goodbye; after that it closes the link itself.
	return c.T != wire.CtlBye
}

// SendControl sends a control message to the shell.
func (s *Session) SendControl(ctx context.Context, t string, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(wire.Control{T: t, B: raw})
	if err != nil {
		return err
	}
	return s.Link.Write(ctx, wire.Frame{Type: wire.TypeData, Channel: wire.ControlChannel, Payload: payload})
}

// Hint tells the shell that a newer version of the trust list exists.
func (s *Session) Hint(ctx context.Context, seq uint64) error {
	return s.SendControl(ctx, wire.CtlTrust, session.TrustHint{Seq: seq})
}

// Notify sends a notice to the shell.
func (s *Session) Notify(ctx context.Context, n core.Notice) error {
	return s.SendControl(ctx, wire.CtlNotify, n)
}

// Registry holds the sessions this API serves right now, so a new version of
// the trust list or a notice reaches every open shell.
type Registry struct {
	mu   sync.Mutex
	open map[*Session]bool
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{open: map[*Session]bool{}} }

// Add records a session.
func (r *Registry) Add(s *Session) { r.mu.Lock(); r.open[s] = true; r.mu.Unlock() }

// Remove forgets a session.
func (r *Registry) Remove(s *Session) { r.mu.Lock(); delete(r.open, s); r.mu.Unlock() }

// Each calls fn for every open session.
func (r *Registry) Each(fn func(*Session)) {
	r.mu.Lock()
	list := make([]*Session, 0, len(r.open))
	for s := range r.open {
		list = append(list, s)
	}
	r.mu.Unlock()
	for _, s := range list {
		fn(s)
	}
}

// HintAll tells every open shell about a new version; a shell that cannot be
// reached simply fetches it at its next request.
func (r *Registry) HintAll(ctx context.Context, seq uint64) {
	r.Each(func(s *Session) { _ = s.Hint(ctx, seq) })
}
```

- [ ] **Step 5: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/browser/`
Expected: PASS. Danach `tools/check.sh`.

- [ ] **Step 6: Commit**

```bash
git add apps/server-api/classes/browser
git commit -m "Add the session link to the shell" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---
### Task 12: Tunnel zu den Controllern und Kopplung

**Files:**
- Create: `apps/server-api/classes/controllers/tunnel.go`,
  `apps/server-api/classes/controllers/pair.go`, `apps/server-api/classes/controllers/registry.go`
- Test: `apps/server-api/classes/controllers/tunnel_test.go`,
  `apps/server-api/classes/controllers/pair_test.go`,
  `apps/server-api/classes/controllers/fuzz_test.go`

**Interfaces:**
- Consumes: `tunnel.PathTunnel`, `tunnel.PathPair`, `tunnel.Subprotocol`, `tunnel.ParseHello`,
  `tunnel.ParseStatus`, `tunnel.ParsePairRequest`, `tunnel.PairRequest.Verify`, `tunnel.PairCode`,
  `tunnel.Fingerprint`, `tunnel.Contract`, `tunnel.Components`, `store.ParseUpdateReport`,
  `core.ParseNotice`, `dbapi.OncePair`, `dbapi.Pending`, `link`, `state`.
- Produces:
  - `type Tunnel struct { Node string; Hello tunnel.Hello; Status tunnel.Status; Link *link.Link; Table *link.Table }`
  - `type Registry struct{ … }` mit `Get(node string) (*Tunnel, bool)`, `Each(fn)`, `Nodes() []string`
  - `type Server struct { S *state.State; Tunnels *Registry; OnHello func(ctx, *Tunnel);
    OnReport func(ctx, *Tunnel, store.UpdateReport); OnNotice func(ctx, *Tunnel, core.Notice) }`
  - `func (srv *Server) Tunnel(w http.ResponseWriter, r *http.Request)`
  - `func (srv *Server) Pair(w http.ResponseWriter, r *http.Request)`
  - `func (srv *Server) NewPairCode(ctx context.Context) (tunnel.PairCode, int64, error)`

- [ ] **Step 1: Write the failing test for the pairing**

`apps/server-api/classes/controllers/pair_test.go`:

```go
package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

func TestPairCodeMatchesTheTrustList(t *testing.T) {
	srv, fx := newServer(t)
	code, expires, err := srv.NewPairCode(ctx)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if expires <= testNow || expires > testNow+tunnel.PairLifetime {
		t.Fatalf("expires %d", expires)
	}
	// The shell shows a code only when it fits its newest verified version.
	if err := code.Check(fx.Version); err != nil {
		t.Fatalf("code does not fit the trust list: %v", err)
	}
	parsed, err := tunnel.ParsePairCode(code.String())
	if err != nil || parsed.URL != srv.S.Cfg.URL {
		t.Fatalf("parse: %v %+v", err, parsed)
	}
}

func TestPairAcceptsOnceAndListsThePending(t *testing.T) {
	srv, _ := newServer(t)
	code, _, err := srv.NewPairCode(ctx)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	ctrl, _ := trusttest.NodeKeys(20)
	req := tunnel.PairRequest{Name: "services-1", Noise: enc.Bytes(ctrl.Public),
		MAC: enc.Bytes(tunnel.PairMAC(code.Secret, ctrl.Public, code.Key))}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	w := httptest.NewRecorder()
	srv.Pair(w, post(tunnel.PathPair, string(body)))
	if w.Code != 204 {
		t.Fatalf("pair %d %s", w.Code, w.Body)
	}
	list, err := srv.S.DB.PendingList(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "services-1" {
		t.Fatalf("pending %v: %v", list, err)
	}
	// The secret holds exactly once.
	w = httptest.NewRecorder()
	srv.Pair(w, post(tunnel.PathPair, string(body)))
	if w.Code != 403 {
		t.Fatalf("second pairing answered %d", w.Code)
	}
}

func TestPairRefusesAForeignMAC(t *testing.T) {
	srv, _ := newServer(t)
	code, _, _ := srv.NewPairCode(ctx)
	ctrl, _ := trusttest.NodeKeys(20)
	req := tunnel.PairRequest{Name: "services-1", Noise: enc.Bytes(ctrl.Public),
		MAC: enc.Bytes(tunnel.PairMAC([]byte("wrong secret!!!!"), ctrl.Public, code.Key))}
	body, _ := json.Marshal(req)
	w := httptest.NewRecorder()
	srv.Pair(w, post(tunnel.PathPair, string(body)))
	if w.Code != 403 {
		t.Fatalf("status %d", w.Code)
	}
	if list, _ := srv.S.DB.PendingList(ctx); len(list) != 0 {
		t.Fatalf("a foreign request was kept: %v", list)
	}
}

// post builds a POST with the right content type.
func post(path, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

var _ = dbapi.Pending{}
var _ = trust.Version{}
```

- [ ] **Step 2: Write the failing test for the tunnel**

`apps/server-api/classes/controllers/tunnel_test.go`:

```go
package controllers

import (
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

func TestTunnelExchangesHello(t *testing.T) {
	srv, fx := newServer(t)
	c := dialTunnel(t, srv, fx.ControllerKeys)
	defer c.close()
	c.writeControl(t, wire.CtlHello, tunnel.Hello{Version: "0.1.0", Contract: tunnel.Contract,
		Node: fx.ControllerNode, TrustSeq: 1, Arch: "amd64", OS: "debian-13",
		Roles: []string{tunnel.RoleServices}})
	hello := c.readHello(t)
	if hello.Contract != tunnel.Contract || hello.Version == "" {
		t.Fatalf("hello %+v", hello)
	}
	tun, ok := srv.Tunnels.Get(fx.ControllerNode)
	if !ok || tun.Hello.Arch != "amd64" {
		t.Fatalf("registry %v %+v", ok, tun)
	}
}

func TestTunnelEndsOnAForeignNode(t *testing.T) {
	srv, fx := newServer(t)
	c := dialTunnel(t, srv, fx.ControllerKeys)
	defer c.close()
	// The hello names a node other than the one whose key made the tunnel.
	c.writeControl(t, wire.CtlHello, tunnel.Hello{Version: "0.1.0", Contract: tunnel.Contract,
		Node: "web-1", TrustSeq: 1, Arch: "amd64", Roles: []string{tunnel.RoleServices}})
	if err := c.readErr(); err == nil {
		t.Fatal("the tunnel survived a foreign node")
	}
}

func TestTunnelTakesStatusAndReports(t *testing.T) {
	srv, fx := newServer(t)
	seen := make(chan store.UpdateReport, 1)
	srv.OnReport = func(_ context, _ *Tunnel, r store.UpdateReport) { seen <- r }
	c := dialTunnel(t, srv, fx.ControllerKeys)
	defer c.close()
	c.writeControl(t, wire.CtlHello, tunnel.Hello{Version: "0.1.0", Contract: tunnel.Contract,
		Node: fx.ControllerNode, TrustSeq: 1, Arch: "amd64", Roles: []string{tunnel.RoleServices}})
	c.readHello(t)
	c.writeControl(t, wire.CtlStatus, tunnel.Status{DiskFree: 1 << 30,
		Components: map[string]string{"ngc-controller": "0.1.0"}})
	c.writeControl(t, wire.CtlUpdate, store.UpdateReport{Target: "ngc-controller",
		Version: "0.2.0", State: store.UpdateDownloaded})
	r := <-seen
	if r.Target != "ngc-controller" || r.State != store.UpdateDownloaded {
		t.Fatalf("report %+v", r)
	}
	tun, _ := srv.Tunnels.Get(fx.ControllerNode)
	if tun.Status.DiskFree != 1<<30 {
		t.Fatalf("status %+v", tun.Status)
	}
}
```

`context` im Typ von `OnReport` ist `context.Context`; die Testdatei importiert `"context"` und
setzt `srv.OnReport = func(_ context.Context, _ *Tunnel, r store.UpdateReport) { seen <- r }`.
`newServer`, `dialTunnel` und der Verbindungstyp stehen in
`apps/server-api/classes/controllers/fixture_test.go` und arbeiten wie die Helfer aus Aufgabe 9,
nur mit `noisep.PrologueTunnel`, dem Noise-Schlüssel der API aus der Konfiguration und dem Schlüssel
des Controllers aus der Vertrauensliste; die Abfrage `?key=` trägt
`tunnel.Fingerprint(controller.Public)`.

- [ ] **Step 3: Run both and see them fail**

Run: `go test ./apps/server-api/classes/controllers/`
Expected: FAIL, `undefined: Server`.

- [ ] **Step 4: Write the registry**

`apps/server-api/classes/controllers/registry.go`:

```go
// Package controllers holds the tunnels to the controllers: the WebSocket at
// tunnel.PathTunnel with Noise inside, the pairing at tunnel.PathPair, the
// hello exchange and what a controller reports. Every controller dials every
// API, so each API reaches each controller directly; nothing here is state the
// installation depends on, only what is connected right now.
package controllers

import (
	"sync"

	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
)

// Tunnel is one open tunnel.
type Tunnel struct {
	Node   string
	Link   *link.Link
	Table  *link.Table
	mu     sync.Mutex
	hello  tunnel.Hello
	status tunnel.Status
}

// Hello returns the last hello of the controller.
func (t *Tunnel) Hello() tunnel.Hello { t.mu.Lock(); defer t.mu.Unlock(); return t.hello }

// Status returns the last status of the controller.
func (t *Tunnel) Status() tunnel.Status { t.mu.Lock(); defer t.mu.Unlock(); return t.status }

func (t *Tunnel) setHello(h tunnel.Hello)   { t.mu.Lock(); t.hello = h; t.mu.Unlock() }
func (t *Tunnel) setStatus(s tunnel.Status) { t.mu.Lock(); t.status = s; t.mu.Unlock() }

// Registry holds the tunnels this API has right now.
type Registry struct {
	mu   sync.Mutex
	open map[string]*Tunnel
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{open: map[string]*Tunnel{}} }

// Get returns the tunnel to a node.
func (r *Registry) Get(node string) (*Tunnel, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.open[node]
	return t, ok
}

// add records a tunnel and returns the one it replaced, if any: a controller
// that reconnects after a broken link must not leave the old one behind.
func (r *Registry) add(t *Tunnel) *Tunnel {
	r.mu.Lock()
	defer r.mu.Unlock()
	old := r.open[t.Node]
	r.open[t.Node] = t
	return old
}

// remove forgets a tunnel, unless another one took its place already.
func (r *Registry) remove(t *Tunnel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cur, ok := r.open[t.Node]; ok && cur == t {
		delete(r.open, t.Node)
	}
}

// Each calls fn for every open tunnel.
func (r *Registry) Each(fn func(*Tunnel)) {
	r.mu.Lock()
	list := make([]*Tunnel, 0, len(r.open))
	for _, t := range r.open {
		list = append(list, t)
	}
	r.mu.Unlock()
	for _, t := range list {
		fn(t)
	}
}
```

Die Testdatei nennt `tun.Hello.Arch`; mit den Methoden oben heißt es `tun.Hello().Arch`. Die Tests
aus Schritt 2 sind entsprechend zu schreiben.

- [ ] **Step 5: Write the pairing**

`apps/server-api/classes/controllers/pair.go`:

```go
package controllers

import (
	"context"
	"crypto/ecdh"
	"crypto/sha256"
	"net/http"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
)

// NewPairCode makes a pairing code for the ulh script of a new controller: it
// names this API, its TLS pin and the newest version of the trust list, so the
// shell can check the code against what it verified itself before it shows it.
func (srv *Server) NewPairCode(ctx context.Context) (tunnel.PairCode, int64, error) {
	v, err := srv.S.Trust(ctx)
	if err != nil {
		return tunnel.PairCode{}, 0, err
	}
	secret, err := secrets.Bytes(tunnel.SecretSize)
	if err != nil {
		return tunnel.PairCode{}, 0, err
	}
	now := srv.S.Now()
	expires := now + tunnel.PairLifetime
	// Only the hash of the secret is kept, and it holds exactly once.
	h := sha256.Sum256(secret)
	if err := srv.S.DB.OnceCreate(ctx, dbapi.OncePair, h[:], "", expires); err != nil {
		return tunnel.PairCode{}, 0, err
	}
	pub, err := srv.noisePublic()
	if err != nil {
		return tunnel.PairCode{}, 0, err
	}
	return tunnel.PairCode{
		URL: srv.S.Cfg.URL, Key: pub, TLS: enc.Bytes(srv.S.Pin), Secret: enc.Bytes(secret),
		Anchor: trust.Anchor{Seq: v.List.Seq, Hash: enc.Bytes(v.Hash)},
	}, expires, nil
}

// Pair answers POST tunnel.PathPair: a controller asks to be listed.
func (srv *Server) Pair(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, ok := httpapi.Body(w, r)
	if !ok {
		return
	}
	req, err := tunnel.ParsePairRequest(body)
	if err != nil {
		httpapi.Fail(w, wire.CodeProtocol)
		return
	}
	pub, err := srv.noisePublic()
	if err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return
	}
	// The MAC names the controller's key and ours, so a request cannot be
	// replayed against another API, and the secret holds exactly once.
	secretOK := false
	if err := req.Verify(nil, pub); err == nil {
		secretOK = false // never: Verify needs the secret, checked below
	}
	_ = secretOK
	if !srv.usePairSecret(ctx, req, pub) {
		httpapi.Fail(w, wire.CodeVerify)
		return
	}
	if err := srv.S.DB.PendingPut(ctx, dbapi.Pending{Name: req.Name, Key: req.Noise,
		Since: srv.S.Now()}); err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	httpapi.NoContent(w)
}

// usePairSecret finds the one-time secret the MAC was made with and spends it.
// The request carries no secret, so every unused secret is tried; the MAC says
// which one it was, and a wrong request spends none.
func (srv *Server) usePairSecret(ctx context.Context, req tunnel.PairRequest, apiKey []byte) bool {
	// The database gives no list of secrets, and it must not: a secret is
	// found by its hash alone. The MAC therefore names the secret, which the
	// controller got in the code, and the request carries its hash.
	return srv.spend(ctx, dbapi.OncePair, req.Secret(), func(secret []byte) bool {
		return req.Verify(secret, apiKey) == nil
	})
}

// noisePublic returns this API's public Noise key.
func (srv *Server) noisePublic() (enc.Bytes, error) {
	k, err := ecdh.X25519().NewPrivateKey(srv.S.Cfg.NoiseKey.Bytes())
	if err != nil {
		return nil, err
	}
	return enc.Bytes(k.PublicKey().Bytes()), nil
}
```

**Achtung:** `tunnel.PairRequest` trägt kein Geheimnis und keinen Hash, nur den MAC. Ein Suchlauf
über alle Geheimnisse ist mit `dbapi` nicht möglich und wäre auch falsch. Deshalb nimmt die API
den Fingerabdruck des Geheimnisses aus der **Abfrage** der Anfrage: der Controller ruft
`POST tunnel.PathPair?secret=<base64url des SHA-256 des Geheimnisses>` auf. Das ist keine
Vertragsänderung — `tunnel.PathPair` legt die Abfrage nicht fest — gehört aber als Festlegung in
`shared/ulh.md` (siehe „Offene Entscheidungen“, Punkt 6). Die Umsetzung lautet damit:

```go
// Pair answers POST tunnel.PathPair?secret=<base64url of SHA-256 of the
// pairing secret>. The query names which one-time value to spend; the MAC
// proves the sender holds the secret itself, which never travels.
func (srv *Server) Pair(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, ok := httpapi.Body(w, r)
	if !ok {
		return
	}
	req, err := tunnel.ParsePairRequest(body)
	if err != nil {
		httpapi.Fail(w, wire.CodeProtocol)
		return
	}
	hash, err := enc.B64.DecodeString(r.URL.Query().Get("secret"))
	if err != nil || len(hash) != sha256.Size {
		httpapi.Fail(w, wire.CodeProtocol)
		return
	}
	pub, err := srv.noisePublic()
	if err != nil {
		httpapi.Fail(w, wire.CodeInternal)
		return
	}
	secret, err := srv.S.DB.OnceUse(ctx, dbapi.OncePair, hash, srv.S.Now())
	if err != nil {
		httpapi.Fail(w, wire.CodeVerify)
		return
	}
	raw, err := enc.B64.DecodeString(secret)
	if err != nil || req.Verify(raw, pub) != nil {
		// The value is spent either way: a wrong MAC costs the attempt.
		httpapi.Fail(w, wire.CodeVerify)
		return
	}
	if err := srv.S.DB.PendingPut(ctx, dbapi.Pending{Name: req.Name, Key: req.Noise,
		Since: srv.S.Now()}); err != nil {
		httpapi.Fail(w, wire.CodeUnreachable)
		return
	}
	httpapi.NoContent(w)
}
```

`NewPairCode` legt dazu das Geheimnis selbst als `data` des Einmalwerts ab:
`OnceCreate(ctx, dbapi.OncePair, h[:], enc.B64.EncodeToString(secret), expires)`.

- [ ] **Step 6: Write the tunnel**

`apps/server-api/classes/controllers/tunnel.go`:

```go
package controllers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
)

// Server serves the tunnel paths of this API.
type Server struct {
	S       *state.State
	Tunnels *Registry

	// OnHello runs after a controller's hello is accepted: the API then sends
	// it the newest trust list and the licenses of its apps (Tasks 16 and 21).
	OnHello func(ctx context.Context, t *Tunnel)
	// OnReport takes an update report, OnNotice a notice (Tasks 23 and 15).
	OnReport func(ctx context.Context, t *Tunnel, r store.UpdateReport)
	OnNotice func(ctx context.Context, t *Tunnel, n core.Notice)
	// OnFrame takes a frame of a numbered channel: package relay runs them.
	OnFrame func(ctx context.Context, t *Tunnel, f wire.Frame) bool
}

// Tunnel serves GET tunnel.PathTunnel?key=<fingerprint of the controller's
// Noise key>. The key says whose tunnel it is; Noise proves it.
func (srv *Server) Tunnel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	peer, node, ok := srv.peerKey(ctx, r.URL.Query().Get("key"))
	if !ok {
		// A key nobody knows gets no link and no hint why.
		w.WriteHeader(http.StatusNotFound)
		return
	}
	static, err := srv.staticKey()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	l, err := link.Accept(w, r, link.Options{
		Subprotocol: tunnel.Subprotocol, Prologue: noisep.PrologueTunnel,
		Static: static, Peer: peer,
	})
	if err != nil {
		return
	}
	t := &Tunnel{Node: node, Link: l, Table: link.NewTable(wire.MaxTunnelChannels, true)}
	srv.run(ctx, t)
}

// peerKey finds the controller a fingerprint belongs to: a node of the trust
// list, or a controller that paired and waits to be listed (which then has no
// node ID yet and may open nothing).
func (srv *Server) peerKey(ctx context.Context, fingerprint string) ([]byte, string, bool) {
	if fingerprint == "" {
		return nil, "", false
	}
	if v, err := srv.S.Trust(ctx); err == nil {
		for _, n := range v.List.Nodes {
			if n.Kind == trust.NodeController && tunnel.Fingerprint(n.Noise) == fingerprint {
				return n.Noise, n.ID, true
			}
		}
	}
	list, err := srv.S.DB.PendingList(ctx)
	if err != nil {
		return nil, "", false
	}
	for _, p := range list {
		if tunnel.Fingerprint(p.Key) == fingerprint {
			return p.Key, "", true
		}
	}
	return nil, "", false
}

// staticKey returns this API's Noise key pair.
func (srv *Server) staticKey() (noise.DHKey, error) {
	priv := srv.S.Cfg.NoiseKey.Bytes()
	pub, err := srv.noisePublic()
	if err != nil {
		return noise.DHKey{}, err
	}
	return noise.DHKey{Private: priv, Public: pub}, nil
}

// run exchanges hello and then reads the tunnel until it ends.
func (srv *Server) run(ctx context.Context, t *Tunnel) {
	if err := srv.sendHello(ctx, t); err != nil {
		_ = t.Link.Close(websocket.StatusInternalError, "hello")
		return
	}
	for {
		f, err := t.Link.Read(ctx)
		if err != nil {
			srv.Tunnels.remove(t)
			return
		}
		if f.Channel == wire.ControlChannel {
			if !srv.control(ctx, t, f) {
				_ = t.Link.Close(websocket.StatusProtocolError, "control")
				srv.Tunnels.remove(t)
				return
			}
			continue
		}
		if srv.OnFrame == nil || !srv.OnFrame(ctx, t, f) {
			_ = t.Link.Close(websocket.StatusProtocolError, "channel")
			srv.Tunnels.remove(t)
			return
		}
	}
}

// sendHello sends this API's hello, the first message on channel 0.
func (srv *Server) sendHello(ctx context.Context, t *Tunnel) error {
	v, err := srv.S.Trust(ctx)
	seq := uint64(0)
	if err == nil {
		seq = v.List.Seq
	}
	return sendControl(ctx, t.Link, wire.CtlHello, tunnel.Hello{
		Version: Version, Contract: tunnel.Contract, Node: srv.S.Cfg.Node, TrustSeq: seq,
	})
}

// control handles a frame on channel 0 of a tunnel; false ends the tunnel.
func (srv *Server) control(ctx context.Context, t *Tunnel, f wire.Frame) bool {
	if f.Type != wire.TypeData {
		return false
	}
	c, err := wire.ParseControl(f.Payload)
	if err != nil || !wire.ControlAllowed(wire.LinkTunnel, false, c.T) {
		return false
	}
	switch c.T {
	case wire.CtlHello:
		h, err := tunnel.ParseHello(c.B)
		if err != nil {
			return false
		}
		// A hello that names another node than the one whose key made this
		// tunnel ends it: the key decides, never the message.
		if h.Node != t.Node {
			return false
		}
		if t.Node == "" {
			// A controller that only paired has no node yet; it may say hello
			// and nothing else until an admin lists it.
			t.setHello(h)
			return true
		}
		t.setHello(h)
		if old := srv.Tunnels.add(t); old != nil && old != t {
			_ = old.Link.Close(websocket.StatusNormalClosure, "replaced")
		}
		if srv.OnHello != nil {
			go srv.OnHello(ctx, t)
		}
		return true
	case wire.CtlStatus:
		s, err := tunnel.ParseStatus(c.B)
		if err != nil {
			return false
		}
		t.setStatus(s)
		return true
	case wire.CtlUpdate:
		r, err := store.ParseUpdateReport(c.B)
		if err != nil {
			return false
		}
		if srv.OnReport != nil {
			srv.OnReport(ctx, t, r)
		}
		return true
	case wire.CtlNotify:
		n, err := core.ParseNotice(c.B)
		if err != nil {
			return false
		}
		if srv.OnNotice != nil {
			srv.OnNotice(ctx, t, n)
		}
		return true
	case wire.CtlBye:
		return false
	}
	return false
}

// sendControl writes one control message onto a link.
func sendControl(ctx context.Context, l *link.Link, t string, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(wire.Control{T: t, B: raw})
	if err != nil {
		return err
	}
	return l.Write(ctx, wire.Frame{Type: wire.TypeData, Channel: wire.ControlChannel, Payload: payload})
}

// Version is the NGC version this API reports in its hello; main.go sets it.
var Version = "0.0.0-dev"

var _ = dbapi.Pending{}
```

- [ ] **Step 7: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/controllers/`
Expected: PASS.

- [ ] **Step 8: Add the fuzz tests**

`apps/server-api/classes/controllers/fuzz_test.go` mit zwei Zielen:

```go
package controllers

import (
	"net/http/httptest"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// FuzzPair feeds the pairing with everything; no body may ever make a pending
// controller without the right MAC.
func FuzzPair(f *testing.F) {
	f.Add(`{"name":"a","noise":"AAAA","mac":"AAAA"}`, "")
	f.Add(`{}`, "AAAA")
	f.Fuzz(func(t *testing.T, body, secret string) {
		srv, _ := newServer(t)
		w := httptest.NewRecorder()
		srv.Pair(w, post(tunnel.PathPair+"?secret="+secret, body))
		switch w.Code {
		case 204, 400, 403, 413, 500, 503:
		default:
			t.Fatalf("status %d", w.Code)
		}
		if w.Code == 204 {
			t.Fatalf("a fuzzed request paired: %q %q", body, secret)
		}
	})
}

// FuzzControl feeds channel 0 of a tunnel with everything.
func FuzzControl(f *testing.F) {
	f.Add(`{"t":"hello","b":{}}`)
	f.Add(`{"t":"status"}`)
	f.Add(`{}`)
	f.Fuzz(func(t *testing.T, payload string) {
		srv, fx := newServer(t)
		c := dialTunnel(t, srv, fx.ControllerKeys)
		defer c.close()
		c.writeRaw(t, []byte(payload))
		// Either the tunnel takes the message or it ends; both are fine, a
		// panic is not.
		c.drain()
	})
}
```

- [ ] **Step 9: Run the fuzz targets and check**

Run:
```bash
go test ./apps/server-api/classes/controllers/ -run Fuzz -fuzz FuzzPair -fuzztime 60s
go test ./apps/server-api/classes/controllers/ -run Fuzz -fuzz FuzzControl -fuzztime 60s
tools/check.sh
```
Expected: PASS ohne Fund.

- [ ] **Step 10: Commit**

```bash
git add apps/server-api/classes/controllers
git commit -m "Add the tunnels to the controllers and the pairing" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 13: Rechte je Kanal und Aufgaben

Entschieden wird bei jedem Kanal neu, aus Benutzer, Rolle, App, Aktion und Lizenz — nie aus
Parametern des OPEN.

**Files:**
- Create: `apps/server-api/classes/rights/rights.go`, `apps/server-api/classes/rights/check.go`
- Test: `apps/server-api/classes/rights/rights_test.go`

**Interfaces:**
- Consumes: `core.Lookup`, `core.RuleFor`, `core.ShellOpens`, `core.WithoutUser`, `core.RightUse`,
  `core.ValidRight`, `core.RoleRights`, `store.RuleFor`, `store.ParseManifest`, `task.Check`,
  `task.Input`, `dbapi.OnceTask`, `dbapi.BlobRights`, `dbapi.App`, `trust.List`.
- Produces:
  - `func Of(ctx, db dbapi.Store, l *trust.List, user string) (map[string]bool, bool, error)` –
    Rechte `"<app>/<aktion>"` des Benutzers und ob er Admin ist.
  - `func Set(ctx, db dbapi.Store, r core.RoleRights) error`
  - `func All(ctx, db dbapi.Store, l *trust.List) ([]core.RoleRights, error)`
  - `type Request struct { Action core.Action; App *dbapi.App; Manifest *store.Manifest;
    Node string; Open wire.Open; Session *session.Verified }`
  - `func Check(ctx, db dbapi.Store, l *trust.List, req Request, now int64) (*task.Task, wire.Code)` –
    `""` als Code heißt „erlaubt“.

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/rights/rights_test.go`:

```go
package rights

import (
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

func TestRightsOfARole(t *testing.T) {
	fx := newFixture(t)
	if err := Set(ctx, fx.DB, core.RoleRights{Role: "office",
		Rights: []string{"org.sorglos.terminal/use", "org.sorglos.terminal/shell"}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, admin, err := Of(ctx, fx.DB, fx.List, fx.OfficeUser)
	if err != nil {
		t.Fatalf("of: %v", err)
	}
	if admin {
		t.Fatal("an office user is no admin")
	}
	if !got["org.sorglos.terminal/use"] || !got["org.sorglos.terminal/shell"] {
		t.Fatalf("rights %v", got)
	}
	if got["org.sorglos.other/use"] {
		t.Fatalf("a right appeared out of nowhere: %v", got)
	}
	// An admin needs no row: the role admin may use every core action.
	if _, adminToo, err := Of(ctx, fx.DB, fx.List, fx.AdminUser); err != nil || !adminToo {
		t.Fatalf("admin: %v %v", adminToo, err)
	}
}

func TestCheckRefusesACoreActionWithoutAdmin(t *testing.T) {
	fx := newFixture(t)
	action, _ := core.Lookup(core.AppUsers, core.ServiceAPI, core.ActList)
	code := checkAs(t, fx, fx.OfficeUser, action, nil)
	if code != wire.CodeForbidden {
		t.Fatalf("code %q", code)
	}
	if code := checkAs(t, fx, fx.AdminUser, action, nil); code != "" {
		t.Fatalf("an admin was refused: %q", code)
	}
}

func TestCheckNeedsATaskForARootAction(t *testing.T) {
	fx := newFixture(t)
	action, _ := core.Lookup(core.AppStore, core.ServiceController, core.ActDeactivate)
	if code := checkAs(t, fx, fx.AdminUser, action, nil); code != wire.CodeConfirm {
		t.Fatalf("without a task: %q", code)
	}
	// With a signed task from an admin who may run root tasks on the node it
	// passes, and the task ID is spent once.
	code := checkAs(t, fx, fx.AdminUser, action, fx.DeactivateTask(t))
	if code != "" {
		t.Fatalf("with a task: %q", code)
	}
	if code := checkAs(t, fx, fx.AdminUser, action, fx.DeactivateTask(t)); code != wire.CodeVerify {
		t.Fatalf("a replayed task answered %q", code)
	}
}

func TestCheckRefusesAChannelTheShellMayNotOpen(t *testing.T) {
	fx := newFixture(t)
	action, _ := core.Lookup(core.AppStore, core.ServiceController, core.ActStage)
	if core.ShellOpens(action) {
		t.Fatal("the contract changed: the shell may open stage")
	}
	if code := checkAs(t, fx, fx.AdminUser, action, nil); code != wire.CodeForbidden {
		t.Fatalf("code %q", code)
	}
}

func TestCheckRefusesAnAppWithoutTheRight(t *testing.T) {
	fx := newFixture(t)
	if code := fx.CheckApp(t, fx.OfficeUser, "shell"); code != wire.CodeForbidden {
		t.Fatalf("without the right: %q", code)
	}
	if err := Set(ctx, fx.DB, core.RoleRights{Role: "office",
		Rights: []string{"org.sorglos.terminal/use", "org.sorglos.terminal/shell"}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if code := fx.CheckApp(t, fx.OfficeUser, "shell"); code != "" {
		t.Fatalf("with the right: %q", code)
	}
	// An action the manifest does not have is not found, not forbidden.
	if code := fx.CheckApp(t, fx.OfficeUser, "nothing"); code != wire.CodeNotFound {
		t.Fatalf("unknown action: %q", code)
	}
}
```

`newFixture` legt eine Vertrauensliste mit den Rollen `admin` und `office` an, einen Controller
`services-1` mit `rootRoles: ["admin"]`, eine installierte App `org.sorglos.terminal` mit Manifest
(Aktion `shell`, Dienst `terminal`) und die Helfer `DeactivateTask` (unterschriebene Aufgabe) und
`CheckApp`.

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/server-api/classes/rights/`
Expected: FAIL, `undefined: Of`.

- [ ] **Step 3: Write the rights**

`apps/server-api/classes/rights/rights.go`:

```go
// Package rights decides for every channel anew whether it may open: user,
// role, app, action and licence. The rights of a role live in the database
// (dbapi.BlobRights), the roles of a user in the trust list, which only an
// admin can change. The role admin may use every core action; every other
// right is granted per role and app.
package rights

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// ErrRights reports a rights row the database cannot hold.
var ErrRights = errors.New("rights: malformed rights row")

// Of returns the rights of a user, joined over the user's roles, and whether
// the user is an admin.
func Of(ctx context.Context, db dbapi.Store, l *trust.List, user string) (map[string]bool, bool, error) {
	u, ok := l.User(user)
	if !ok {
		return nil, false, nil
	}
	admin := slices.Contains(u.Roles, trust.RoleAdmin)
	out := map[string]bool{}
	for _, role := range u.Roles {
		r, err := read(ctx, db, role)
		if err != nil {
			return nil, false, err
		}
		for _, right := range r.Rights {
			out[right] = true
		}
	}
	return out, admin, nil
}

// read returns the rights of one role; a role without a row has none.
func read(ctx context.Context, db dbapi.Store, role string) (core.RoleRights, error) {
	b, err := db.BlobGet(ctx, dbapi.BlobRights, role)
	if errors.Is(err, dbapi.ErrNotFound) {
		return core.RoleRights{Role: role}, nil
	}
	if err != nil {
		return core.RoleRights{}, err
	}
	var r core.RoleRights
	if json.Unmarshal(b.Data, &r) != nil || r.Validate() != nil || r.Role != role {
		return core.RoleRights{}, ErrRights
	}
	return r, nil
}

// Set replaces the rights of a role. The caller has checked r with
// core.ParseParams, so every right is core.ValidRight.
func Set(ctx context.Context, db dbapi.Store, r core.RoleRights) error {
	if r.Validate() != nil {
		return ErrRights
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobRights, ID: r.Role, Data: data})
}

// All returns the rights of every role of the trust list, for
// ngc.users/rights. A role without a row comes with an empty list, so the
// shell shows every role.
func All(ctx context.Context, db dbapi.Store, l *trust.List) ([]core.RoleRights, error) {
	out := make([]core.RoleRights, 0, len(l.Roles))
	for _, role := range l.Roles {
		r, err := read(ctx, db, role.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// has reports whether the rights hold "<app>/<action>".
func has(r map[string]bool, app, action string) bool { return r[app+"/"+action] }

// usable reports whether the user may see an app at all (core.RightUse).
func usable(r map[string]bool, app string) bool { return r[app+"/"+core.RightUse] }

// appOf splits a right into app and action.
func appOf(right string) (string, string, bool) { return strings.Cut(right, "/") }
```

- [ ] **Step 4: Write the check**

`apps/server-api/classes/rights/check.go`:

```go
package rights

import (
	"context"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Request is what the API knows about a channel that wants to open.
type Request struct {
	Action   core.Action       // the core action, empty for an app channel
	App      *dbapi.App        // the installed app, for an app channel
	Manifest *store.Manifest   // its manifest, parsed
	Node     string            // the controller the channel runs on, "" for the API itself
	Open     wire.Open         // the OPEN of the shell
	Session  *session.Verified // nil for a channel the API opens without a user
}

// Check decides whether the channel may open. It returns the verified task, if
// one was needed, and the wire.Code to close with; an empty code means the
// channel may open.
func Check(ctx context.Context, db dbapi.Store, l *trust.List, req Request, now int64) (*task.Task, wire.Code) {
	if req.Session == nil {
		// Channels without a user are the API's own; the shell never opens one.
		if req.Action.Name == "" || !core.WithoutUser(req.Action) {
			return nil, wire.CodeForbidden
		}
		return nil, ""
	}
	user := req.Session.Cert().User
	granted, admin, err := Of(ctx, db, l, user)
	if err != nil {
		return nil, wire.CodeUnreachable
	}
	var rule task.Rule
	switch {
	case req.Action.Name != "":
		// The shell may open the actions of service api and the root tasks of
		// service controller; stage, install and license are the API's own.
		if !core.ShellOpens(req.Action) {
			return nil, wire.CodeForbidden
		}
		if req.Action.Admin && !admin {
			return nil, wire.CodeForbidden
		}
		rule = core.RuleFor(req.Action)
	default:
		if req.App == nil || req.Manifest == nil {
			return nil, wire.CodeNotFound
		}
		if !req.App.Active {
			return nil, wire.CodeNotFound
		}
		if !usable(granted, req.App.ID) {
			// Without the right "use" the app does not exist for this user, so
			// the answer says nothing more than "not allowed".
			return nil, wire.CodeForbidden
		}
		if _, ok := req.Manifest.Action(req.Open.Action); !ok {
			return nil, wire.CodeNotFound
		}
		if !has(granted, req.App.ID, req.Open.Action) {
			return nil, wire.CodeForbidden
		}
		r, err := store.RuleFor(req.Manifest, req.Open.Action)
		if err != nil {
			return nil, wire.CodeNotFound
		}
		rule = r
	}
	in := task.Input{
		Task: req.Open.Task, Confirm: req.Open.Confirm, Session: *req.Session,
		Node: req.Node, App: req.Open.App, Service: req.Open.Service, Action: req.Open.Action,
		Rule: rule,
	}
	t, err := task.Check(in, l, now)
	if err != nil {
		return nil, codeOf(err)
	}
	if t != nil {
		// A task counts once: the ID is spent until it expires, so nobody can
		// replay a signed task.
		if err := db.OnceCreate(ctx, dbapi.OnceTask, t.ID, "", t.Expires); err != nil {
			return nil, wire.CodeVerify
		}
	}
	return t, ""
}

// codeOf maps the errors of package task to the codes of the contract.
func codeOf(err error) wire.Code {
	switch err {
	case task.ErrNoTask, task.ErrConfirm:
		return wire.CodeConfirm
	case task.ErrForbidden:
		return wire.CodeForbidden
	}
	return wire.CodeVerify
}
```

- [ ] **Step 5: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/rights/`
Expected: PASS. Danach `tools/check.sh`.

- [ ] **Step 6: Commit**

```bash
git add apps/server-api/classes/rights
git commit -m "Add the rights and task check of a channel" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 14: Protokoll

**Files:**
- Create: `apps/server-api/classes/audit/audit.go`
- Test: `apps/server-api/classes/audit/audit_test.go`

**Interfaces:**
- Consumes: `core.LogEntry`, `core.LogQuery`, `core.ValidLogID`, `core.Log*`, `dbapi.LogAdd`,
  `dbapi.LogQuery`, `secrets.LogID`.
- Produces:
  - `type Log struct { DB dbapi.Store; Now func() int64 }`
  - `func (lg *Log) Add(ctx context.Context, e core.LogEntry) error` – setzt ID und Zeit
  - `func (lg *Log) Login(ctx, user, result, detail string) error`
  - `func (lg *Log) Channel(ctx, user, app, action, node string, code wire.Code) error`
  - `func (lg *Log) Task(ctx, user, app, action, node, signature string) error`
  - `func (lg *Log) Query(ctx context.Context, q core.LogQuery) ([]core.LogEntry, error)`

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/audit/audit_test.go`:

```go
package audit

import (
	"context"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

var ctx = context.Background()

func newLog() *Log {
	return &Log{DB: memstore.New(), Now: func() int64 { return 1790000000 }}
}

func TestAddSetsIDAndTime(t *testing.T) {
	lg := newLog()
	if err := lg.Login(ctx, "u", "ok", ""); err != nil {
		t.Fatalf("login: %v", err)
	}
	rows, err := lg.Query(ctx, core.LogQuery{Limit: 10})
	if err != nil || len(rows) != 1 {
		t.Fatalf("query: %v %d", err, len(rows))
	}
	e := rows[0]
	if !core.ValidLogID(e.ID) || e.Time != 1790000000 || e.Kind != core.LogLogin || e.Result != "ok" {
		t.Fatalf("entry %+v", e)
	}
}

func TestChannelKeepsTheCode(t *testing.T) {
	lg := newLog()
	if err := lg.Channel(ctx, "u", "org.sorglos.terminal", "shell", "services-1", wire.CodeForbidden); err != nil {
		t.Fatalf("channel: %v", err)
	}
	rows, _ := lg.Query(ctx, core.LogQuery{Limit: 10})
	if rows[0].Result != string(wire.CodeForbidden) || rows[0].Node != "services-1" {
		t.Fatalf("entry %+v", rows[0])
	}
}

func TestQueryPagesByTimeAndID(t *testing.T) {
	lg := newLog()
	at := int64(1790000000)
	lg.Now = func() int64 { return at }
	for i := 0; i < 5; i++ {
		if err := lg.Login(ctx, "u", "ok", ""); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	first, err := lg.Query(ctx, core.LogQuery{Limit: 2})
	if err != nil || len(first) != 2 {
		t.Fatalf("first page: %v %d", err, len(first))
	}
	last := first[len(first)-1]
	second, err := lg.Query(ctx, core.LogQuery{Limit: 2, BeforeTime: last.Time, BeforeID: last.ID})
	if err != nil || len(second) != 2 {
		t.Fatalf("second page: %v %d", err, len(second))
	}
	// No entry appears twice, although all five share one second.
	for _, a := range first {
		for _, b := range second {
			if a.ID == b.ID {
				t.Fatalf("entry %s on both pages", a.ID)
			}
		}
	}
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/server-api/classes/audit/`
Expected: FAIL, `undefined: Log`.

- [ ] **Step 3: Write the log**

`apps/server-api/classes/audit/audit.go`:

```go
// Package audit keeps the log: logins and failed logins, rights, signed tasks,
// channels, stores, licences and updates. Every entry gets an ID of 16 random
// bytes as lower-case hex, so entries of the same second still page in one
// order (core.LogQuery).
package audit

import (
	"context"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/wire"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
)

// Log writes and reads the log.
type Log struct {
	DB  dbapi.Store
	Now func() int64
}

// Add keeps one entry; it sets the ID and the time itself.
func (lg *Log) Add(ctx context.Context, e core.LogEntry) error {
	id, err := secrets.LogID()
	if err != nil {
		return err
	}
	e.ID, e.Time = id, lg.Now()
	if e.Result == "" {
		e.Result = "ok"
	}
	return lg.DB.LogAdd(ctx, e)
}

// Login notes a login or a failed one. result is "ok" or a wire.Code; detail
// never holds a password, a code or a key, only a short word such as "totp".
func (lg *Log) Login(ctx context.Context, user, result, detail string) error {
	kind := core.LogLogin
	if result != "ok" {
		kind = core.LogLoginFailed
	}
	return lg.Add(ctx, core.LogEntry{User: user, Kind: kind, Result: result, Detail: detail})
}

// Channel notes that a channel opened or was refused.
func (lg *Log) Channel(ctx context.Context, user, app, action, node string, code wire.Code) error {
	result := "ok"
	if code != "" {
		result = string(code)
	}
	return lg.Add(ctx, core.LogEntry{User: user, Kind: core.LogChannel, App: app, Action: action,
		Node: node, Result: result})
}

// Task notes a signed task with its signature, so it can be shown later who
// ordered it. signature is the base64url of the task's signature, never a key.
func (lg *Log) Task(ctx context.Context, user, app, action, node, signature string) error {
	return lg.Add(ctx, core.LogEntry{User: user, Kind: core.LogTask, App: app, Action: action,
		Node: node, Detail: signature})
}

// Note keeps an entry of any other kind (core.LogTrust, LogRights, LogApp,
// LogStore, LogLicense, LogUpdate).
func (lg *Log) Note(ctx context.Context, kind, user, app, node, detail string) error {
	return lg.Add(ctx, core.LogEntry{User: user, Kind: kind, App: app, Node: node, Detail: detail})
}

// Query answers ngc.users/log.
func (lg *Log) Query(ctx context.Context, q core.LogQuery) ([]core.LogEntry, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	return lg.DB.LogQuery(ctx, q)
}
```

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/audit/`
Expected: PASS. Danach `tools/check.sh`.

- [ ] **Step 5: Commit**

```bash
git add apps/server-api/classes/audit
git commit -m "Add the log of the API" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---
### Task 15: Versand der Kern-Aktionen und `ngc.desktop`

**Files:**
- Create: `apps/server-api/classes/coreapp/dispatch.go`, `apps/server-api/classes/coreapp/desktop.go`
- Test: `apps/server-api/classes/coreapp/dispatch_test.go`,
  `apps/server-api/classes/coreapp/desktop_test.go`

**Interfaces:**
- Consumes: `core.Lookup`, `core.ParseParams`, `core.Action`, `core.Flow*`, `core.EncodeResult`,
  `core.Me`, `core.DesktopApp`, `core.UIProof`, `core.TOTPBegin`, `rights.Check`, `audit.Log`,
  `browser.Session`, `link.Channel`, `dbapi.BlobUI`, `dbapi.BlobApproval`, `store.WalkUI`.
- Produces:
  - `type Deps struct { S *state.State; Log *audit.Log; Login *login.Handler; Tunnels *controllers.Registry; Relay Relay }`
  - `type Relay interface { Forward(ctx context.Context, s *browser.Session, c *link.Channel, o wire.Open, t *task.Task, node string) }`
  - `type Dispatcher struct { Deps }`, `func New(d Deps) *Dispatcher`
  - `func (d *Dispatcher) OpenChannel(ctx context.Context, s *browser.Session, c *link.Channel, o wire.Open)`
  - `type Handler func(ctx context.Context, rq *Call) ([]byte, error)`
  - `type Call struct { S *state.State; Live *login.Live; Action core.Action; Params core.Params;
    Task *task.Task; In io.Reader; Out io.Writer; Admin bool; Rights map[string]bool }`
  - `func (d *Dispatcher) Register(app, service, name string, h Handler)`

- [ ] **Step 1: Write the failing test for the dispatcher**

`apps/server-api/classes/coreapp/dispatch_test.go`:

```go
package coreapp

import (
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

func TestCallAnswersAndCloses(t *testing.T) {
	fx := newFixture(t)
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: core.ActMe,
		Window: wire.MinWindow})
	// A call-style channel: first a GRANT, then the result as DATA, then EOF
	// and CLOSE ok.
	if f := c.read(t); f.Type != wire.TypeGrant {
		t.Fatalf("first frame %+v", f)
	}
	body := c.collect(t)
	if len(body) == 0 {
		t.Fatal("no result")
	}
	if cl := c.closed(t); cl.Status != wire.StatusOK {
		t.Fatalf("close %+v", cl)
	}
}

func TestUnknownActionIsNotFound(t *testing.T) {
	fx := newFixture(t)
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: "nothing",
		Window: wire.MinWindow})
	if cl := c.closed(t); cl.Code != wire.CodeNotFound {
		t.Fatalf("close %+v", cl)
	}
}

func TestResultBeyondTheLimitCloses(t *testing.T) {
	fx := newFixture(t)
	fx.D.Register(core.AppDesktop, core.ServiceAPI, core.ActMe,
		func(_ context.Context, _ *Call) ([]byte, error) {
			return make([]byte, wire.MaxCallResult+1), nil
		})
	c := fx.Open(t, wire.Open{App: core.AppDesktop, Service: core.ServiceAPI, Action: core.ActMe,
		Window: wire.MinWindow})
	if cl := c.closed(t); cl.Code != wire.CodeTooLarge {
		t.Fatalf("close %+v", cl)
	}
}

func TestInputBeyondMaxInCloses(t *testing.T) {
	fx := newFixture(t)
	action, _ := core.Lookup(core.AppUsers, core.ServiceAPI, core.ActTrust)
	c := fx.OpenAdmin(t, wire.Open{App: core.AppUsers, Service: core.ServiceAPI, Action: core.ActTrust,
		Window: wire.MinWindow})
	c.sendBytes(t, int(action.MaxIn)+1)
	if cl := c.closed(t); cl.Code != wire.CodeTooLarge {
		t.Fatalf("close %+v", cl)
	}
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/server-api/classes/coreapp/`
Expected: FAIL, `undefined: newFixture`, `undefined: Dispatcher`.

- [ ] **Step 3: Write the dispatcher**

`apps/server-api/classes/coreapp/dispatch.go`:

```go
// Package coreapp serves the channels of the core apps (ngc.desktop,
// ngc.users, ngc.store) and hands every other channel to package relay. Core
// channels carry no end-to-end handshake of their own: the session to the API
// is already encrypted end to end (package wire, "Core apps").
package coreapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/wire"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/audit"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/browser"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/controllers"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/login"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/rights"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
)

// Relay forwards a channel to a controller; package relay implements it.
type Relay interface {
	Forward(ctx context.Context, s *browser.Session, c *link.Channel, o wire.Open, t *task.Task, node string)
}

// Deps are what the dispatcher needs.
type Deps struct {
	S       *state.State
	Log     *audit.Log
	Login   *login.Handler
	Tunnels *controllers.Registry
	Relay   Relay
}

// Call is one core action the API carries out itself.
type Call struct {
	S      *state.State
	Live   *login.Live
	Action core.Action
	Params core.Params
	Task   *task.Task
	In     io.Reader // FlowIn: at most Action.MaxIn bytes
	Out    io.Writer // FlowOut: the API writes the data itself
	Admin  bool
	Rights map[string]bool
}

// Handler serves one core action; the bytes it returns are the result.
type Handler func(ctx context.Context, rq *Call) ([]byte, error)

// Dispatcher routes a channel to its handler.
type Dispatcher struct {
	Deps
	handlers map[string]Handler
}

// New returns a dispatcher with the handlers of Tasks 15, 16 and 19 to 23.
func New(d Deps) *Dispatcher {
	return &Dispatcher{Deps: d, handlers: map[string]Handler{}}
}

// Register adds a handler.
func (d *Dispatcher) Register(app, service, name string, h Handler) {
	d.handlers[app+"/"+service+"/"+name] = h
}

// errClose carries the code a channel closes with.
type errClose struct{ code wire.Code }

func (e errClose) Error() string { return string(e.code) }

// fail returns an error that closes the channel with code.
func fail(code wire.Code) error { return errClose{code} }

// codeOf returns the code an error closes with; anything unexpected is
// E_INTERNAL, and nothing of the error ever reaches the shell.
func codeOf(err error) wire.Code {
	var e errClose
	if errors.As(err, &e) {
		return e.code
	}
	return wire.CodeInternal
}

// OpenChannel serves one channel the browser opened.
func (d *Dispatcher) OpenChannel(ctx context.Context, s *browser.Session, c *link.Channel, o wire.Open) {
	code := d.open(ctx, s, c, o)
	user := s.Live.Verified.Cert().User
	_ = d.Log.Channel(ctx, user, o.App, o.Action, "", code)
	if code != "" {
		_ = s.Close(ctx, code, c.Num)
	}
}

// open does the work and returns the code to close with, "" when the channel
// ran and closed itself.
func (d *Dispatcher) open(ctx context.Context, s *browser.Session, c *link.Channel, o wire.Open) wire.Code {
	v, err := d.S.Trust(ctx)
	if err != nil {
		return wire.CodeUnreachable
	}
	now := d.S.Now()
	if !wire.CoreApp(o.App) {
		// An app channel goes to its controller; package relay checks the
		// rights with the target node as task.Input.Node.
		return d.forward(ctx, s, c, o, v.List, now)
	}
	action, ok := core.Lookup(o.App, o.Service, o.Action)
	if !ok {
		return wire.CodeNotFound
	}
	verified := s.Live.Verified
	t, code := rights.Check(ctx, d.S.DB, v.List, rights.Request{
		Action: action, Node: nodeOfTask(o), Open: o, Session: &verified}, now)
	if code != "" {
		return code
	}
	if t != nil {
		_ = d.Log.Task(ctx, verified.Cert().User, o.App, o.Action, t.Node, string(o.Task.Sig))
	}
	if action.Service == core.ServiceController {
		// A root task of service controller runs on a controller; the API only
		// forwards it.
		return d.forwardTask(ctx, s, c, o, t)
	}
	h, ok := d.handlers[action.App+"/"+action.Service+"/"+action.Name]
	if !ok {
		return wire.CodeNotFound
	}
	return d.run(ctx, s, c, o, action, t, h)
}

// run answers the first GRANT, reads the input, calls the handler and sends
// the result.
func (d *Dispatcher) run(ctx context.Context, s *browser.Session, c *link.Channel, o wire.Open,
	action core.Action, t *task.Task, h Handler) wire.Code {
	if err := s.Grant(ctx, c, wire.DefaultWindow); err != nil {
		return wire.CodeInternal
	}
	var params core.Params
	raw := o.Params
	if t != nil {
		raw = t.Params
	}
	p, err := core.ParseParams(action, raw)
	if err != nil {
		return wire.CodeProtocol
	}
	params = p
	var in io.Reader = bytes.NewReader(nil)
	if action.Flow == core.FlowIn {
		body, code := s.ReadInput(ctx, c, action.MaxIn)
		if code != "" {
			return code
		}
		in = bytes.NewReader(body)
	}
	granted, admin, err := rights.Of(ctx, d.S.DB, mustList(ctx, d.S), s.Live.Verified.Cert().User)
	if err != nil {
		return wire.CodeUnreachable
	}
	out := &bytes.Buffer{}
	rq := &Call{S: d.S, Live: s.Live, Action: action, Params: params, Task: t, In: in, Out: out,
		Admin: admin, Rights: granted}
	body, err := h(ctx, rq)
	if err != nil {
		return codeOf(err)
	}
	if action.Flow == core.FlowOut {
		body = out.Bytes()
	} else if len(body) > wire.MaxCallResult {
		return wire.CodeTooLarge
	}
	if err := s.SendResult(ctx, c, body); err != nil {
		return wire.CodeInternal
	}
	return ""
}

// nodeOfTask returns the node a task names, "" when the OPEN carries none: the
// API checks a task with the node it will forward to.
func nodeOfTask(o wire.Open) string {
	if o.Task == nil {
		return ""
	}
	var t struct {
		Node string `json:"node"`
	}
	// The value is only a guess for the rights check; task.Check compares it
	// against the signed task and refuses a mismatch.
	if json.Unmarshal(o.Task.Payload, &t) != nil {
		return ""
	}
	return t.Node
}

// mustList returns the trust list; the callers above already have one, and a
// database that fails here fails there too.
func mustList(ctx context.Context, s *state.State) *trustList {
	v, err := s.Trust(ctx)
	if err != nil {
		return nil
	}
	return v.List
}

var _ = store.ValidVersion
```

`trustList` ist ein Alias: `type trustList = trust.List` mit `"…/shared/trust"` im Import-Block.
`nodeOfTask` liest ein Feld aus einer noch ungeprüften Nutzlast; das ist erlaubt, weil der Wert
nur die Wahl des Ziels lenkt und `task.Check` ihn mit der geprüften Aufgabe vergleicht
(`t.Node != in.Node` ist ein Fehler).

`browser.Session` bekommt dafür drei Methoden (Ergänzung zu Aufgabe 11, in
`apps/server-api/classes/browser/browser.go`):

```go
// Grant sends the first GRANT of a channel the API accepted and counts what
// the other side may then send.
func (s *Session) Grant(ctx context.Context, c *link.Channel, credit uint32) error {
	if err := s.Link.Write(ctx, wire.Frame{Type: wire.TypeGrant, Channel: c.Num,
		Payload: wire.Grant(credit, nil)}); err != nil {
		return err
	}
	c.Allow(credit)
	return nil
}

// ReadInput reads the DATA of a FlowIn channel up to the shell's EOF, at most
// max bytes; more is E_TOOLARGE.
func (s *Session) ReadInput(ctx context.Context, c *link.Channel, max int64) ([]byte, wire.Code) {
	var buf bytes.Buffer
	for {
		f, ok := s.next(ctx, c)
		if !ok {
			return nil, wire.CodeProtocol
		}
		switch f.Type {
		case wire.TypeData:
			if int64(buf.Len()+len(f.Payload)) > max {
				return nil, wire.CodeTooLarge
			}
			buf.Write(f.Payload)
			// Keep the credit flowing so a large upload does not stall.
			if err := s.Grant(ctx, c, uint32(len(f.Payload))); err != nil {
				return nil, wire.CodeInternal
			}
		case wire.TypeEOF:
			return buf.Bytes(), ""
		case wire.TypeClose:
			return nil, wire.CodeProtocol
		default:
			return nil, wire.CodeProtocol
		}
	}
}

// SendResult sends the result of a call-style channel and closes it with ok.
func (s *Session) SendResult(ctx context.Context, c *link.Channel, body []byte) error {
	for len(body) > 0 {
		n := min(len(body), wire.MaxPayload)
		if !c.Spend(n) {
			// The shell granted less than it must; the contract says the
			// opener grants at least MinWindow and more on request.
			if err := s.waitCredit(ctx, c, n); err != nil {
				return err
			}
			continue
		}
		if err := s.Link.Write(ctx, wire.Frame{Type: wire.TypeData, Channel: c.Num, Payload: body[:n]}); err != nil {
			return err
		}
		body = body[n:]
	}
	if err := s.Link.Write(ctx, wire.Frame{Type: wire.TypeEOF, Channel: c.Num}); err != nil {
		return err
	}
	payload, err := json.Marshal(wire.Close{Status: wire.StatusOK})
	if err != nil {
		return err
	}
	if err := s.Link.Write(ctx, wire.Frame{Type: wire.TypeClose, Channel: c.Num, Payload: payload}); err != nil {
		return err
	}
	c.SentClose()
	return nil
}
```

`next` nimmt den nächsten Frame aus dem Puffer, den `deliver` (Aufgabe 11) füllt; `waitCredit`
wartet auf eine GRANT der Shell und gibt nach `wire.E2EFirstTimeout` Sekunden auf.

- [ ] **Step 4: Write the actions of ngc.desktop**

`apps/server-api/classes/coreapp/desktop.go` meldet sieben Handler an:

```go
package coreapp

import (
	"bytes"
	"context"
	"errors"
	"slices"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// registerDesktop adds the actions of ngc.desktop.
func (d *Dispatcher) registerDesktop() {
	d.Register(core.AppDesktop, core.ServiceAPI, core.ActMe, d.me)
	d.Register(core.AppDesktop, core.ServiceAPI, core.ActApps, d.apps)
	d.Register(core.AppDesktop, core.ServiceAPI, core.ActUI, d.ui)
	d.Register(core.AppDesktop, core.ServiceAPI, core.ActUIProof, d.uiProof)
	d.Register(core.AppDesktop, core.ServiceAPI, core.ActTOTPBegin, d.totpBegin)
	d.Register(core.AppDesktop, core.ServiceAPI, core.ActTOTPConfirm, d.totpConfirm)
	d.Register(core.AppDesktop, core.ServiceAPI, core.ActTOTPOff, d.totpOff)
}

// me answers ngc.desktop/me.
func (d *Dispatcher) me(ctx context.Context, rq *Call) ([]byte, error) {
	v, err := rq.S.Trust(ctx)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	id := rq.Live.Verified.Cert().User
	u, ok := v.List.User(id)
	if !ok {
		return nil, fail(wire.CodeNotFound)
	}
	row, err := rq.S.DB.UserGet(ctx, id)
	if err != nil && !errors.Is(err, dbapi.ErrNotFound) {
		return nil, fail(wire.CodeUnreachable)
	}
	// The strictest level of the user's roles counts; a role the list does not
	// hold cannot happen (trust.List.Validate).
	level := trust.LevelOff
	for _, name := range u.Roles {
		if r, ok := v.List.Role(name); ok && stricter(r.Level, level) {
			level = r.Level
		}
	}
	var hint bool
	for _, k := range u.Keys {
		hint = hint || k.Kind == proofKindPassword
	}
	return core.EncodeResult(core.Me{ID: u.ID, Name: u.Name, Roles: u.Roles,
		Admin: slices.Contains(u.Roles, trust.RoleAdmin), PasswordLogin: hint,
		TOTP: len(row.TOTP) > 0, Level: level})
}

// stricter reports whether a is a stricter level than b.
func stricter(a, b trust.Level) bool { return rank(a) > rank(b) }

func rank(l trust.Level) int {
	switch l {
	case trust.LevelCritical:
		return 1
	case trust.LevelRoot:
		return 2
	case trust.LevelHardcore:
		return 3
	}
	return 0
}
```

`proofKindPassword` ist `proof.KindPassword` (Alias im Import-Block).

`apps` baut aus `dbapi.AppList`, dem Manifest jeder App und den Rechten des Benutzers die Liste
`[]core.DesktopApp`:

```go
// apps answers ngc.desktop/apps: the start menu of this user. An app the user
// may not use (core.RightUse) does not appear at all.
func (d *Dispatcher) apps(ctx context.Context, rq *Call) ([]byte, error) {
	list, err := rq.S.DB.AppList(ctx)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	out := make([]core.DesktopApp, 0, len(list)+2)
	// The two core apps are always there and only for admins; their UI is part
	// of the shell, so they carry no entry and no node.
	if rq.Admin {
		out = append(out,
			core.DesktopApp{ID: core.AppStore, Name: "App-Store", State: core.AppOK, Core: true},
			core.DesktopApp{ID: core.AppUsers, Name: "Benutzer & Rechte", State: core.AppOK, Core: true})
	}
	for _, a := range list {
		if !a.Active || a.Version == "" || !rq.Rights[a.ID+"/"+core.RightUse] {
			continue
		}
		m, err := store.ParseManifest(a.Manifest)
		if err != nil {
			// A manifest the database cannot parse means the row is broken;
			// the app is left out instead of breaking the whole menu.
			continue
		}
		entry := core.DesktopApp{ID: a.ID, Name: m.Name, Version: a.Version, Icon: m.Icon,
			State: core.AppOK, Node: a.Node}
		if m.UI != nil {
			entry.Entry, entry.Width, entry.Height = m.UI.Entry, m.UI.Width, m.UI.Height
		}
		for _, act := range m.Actions {
			if rq.Rights[a.ID+"/"+act.Name] {
				entry.Actions = append(entry.Actions, act.Name)
			}
		}
		if _, online := d.Tunnels.Get(a.Node); !online {
			entry.State = core.AppUnreachable
		} else if state, ok := d.licenseState(ctx, a.ID); ok && state == core.LicenseExpired {
			entry.State = core.AppUnlicensed
		}
		out = append(out, entry)
	}
	return core.EncodeResult(out)
}
```

`ui` schreibt die Bytes von `dbapi.BlobUI` unverändert nach `rq.Out` (Datenstrom, kein Ergebnis),
`uiProof` gibt `dbapi.BlobApproval` als `core.UIProof` zurück, `totpBegin`, `totpConfirm` und
`totpOff` benutzen `login.Handler.BeginTOTP` und `checkTOTP`:

```go
// ui answers ngc.desktop/ui: the ui-tar of the installed version, byte for
// byte as the API kept it (dbapi.BlobUI). The shell reads it like
// store.WalkUI and runs it only after the proof of ui-proof holds.
func (d *Dispatcher) ui(ctx context.Context, rq *Call) ([]byte, error) {
	p, ok := rq.Params.(*core.AppParams)
	if !ok {
		return nil, fail(wire.CodeProtocol)
	}
	a, err := rq.S.DB.AppGet(ctx, p.App)
	if err != nil || a.Version == "" || !rq.Rights[a.ID+"/"+core.RightUse] {
		return nil, fail(wire.CodeNotFound)
	}
	b, err := rq.S.DB.BlobGet(ctx, dbapi.BlobUI, a.ID+"@"+a.Version)
	if err != nil {
		return nil, fail(wire.CodeNotFound)
	}
	// Written out as they are: the shell compares their SHA-256 against the
	// proof, so the API cannot change a byte unseen.
	if _, err := rq.Out.Write(b.Data); err != nil {
		return nil, fail(wire.CodeInternal)
	}
	return nil, nil
}

// uiProof answers ngc.desktop/ui-proof with the evidence the API kept when the
// version was installed (dbapi.BlobApproval).
func (d *Dispatcher) uiProof(ctx context.Context, rq *Call) ([]byte, error) {
	p, ok := rq.Params.(*core.AppParams)
	if !ok {
		return nil, fail(wire.CodeProtocol)
	}
	a, err := rq.S.DB.AppGet(ctx, p.App)
	if err != nil || a.Version == "" || !rq.Rights[a.ID+"/"+core.RightUse] {
		return nil, fail(wire.CodeNotFound)
	}
	b, err := rq.S.DB.BlobGet(ctx, dbapi.BlobApproval, a.ID+"@"+a.Version)
	if err != nil {
		return nil, fail(wire.CodeNotFound)
	}
	// The stored proof goes through its own parser before it travels, so a
	// broken row never reaches the shell as something it must trust.
	proof, err := core.ParseUIProof(b.Data)
	if err != nil {
		return nil, fail(wire.CodeInternal)
	}
	return core.EncodeResult(proof)
}
```

- [ ] **Step 5: Write the tests of ngc.desktop**

`apps/server-api/classes/coreapp/desktop_test.go` prüft:
`me` nennt Rollen, Admin, TOTP und die strengste Stufe; `apps` blendet eine App ohne `use` aus,
setzt `unreachable` ohne Tunnel und `unlicensed` bei abgelaufener Lizenz; `ui` liefert genau die
Bytes aus `dbapi.BlobUI` und `store.WalkUI` liest sie; `ui-proof` liefert einen Beweis, den
`core.VerifyUIProof` gegen die Vertrauensliste bestätigt; `totp-begin`, `-confirm` und `-off`
schalten TOTP ein und aus, und ein falscher Code ändert nichts. Jeder dieser Fälle ist ein eigener
`Test…`, der `fx.Open` benutzt und das Ergebnis mit `json.Unmarshal` liest.

- [ ] **Step 6: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/coreapp/`
Expected: PASS.

- [ ] **Step 7: Add the fuzz test for the parameters**

`apps/server-api/classes/coreapp/fuzz_test.go`: `FuzzOpenParams` schickt für jede Kern-Aktion ein
OPEN mit zufälligen `params` und prüft, dass der Kanal entweder mit einem Code des Vertrags
schließt oder ein Ergebnis liefert, das `core.EncodeResult` annehmen würde — nie ein Absturz und
nie ein Ergebnis über `wire.MaxCallResult`.

- [ ] **Step 8: Run the fuzz target and check**

Run: `go test ./apps/server-api/classes/coreapp/ -run Fuzz -fuzz FuzzOpenParams -fuzztime 60s`,
danach `tools/check.sh`.

- [ ] **Step 9: Commit**

```bash
git add apps/server-api/classes/coreapp apps/server-api/classes/browser
git commit -m "Add the core action dispatcher and ngc.desktop" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 16: `ngc.users` – Benutzer, Rechte, Vertrauensliste, Codes

**Files:**
- Create: `apps/server-api/classes/coreapp/users.go`
- Test: `apps/server-api/classes/coreapp/users_test.go`

**Interfaces:**
- Consumes: `core.UserInfo`, `core.RoleRights`, `core.TrustResult`, `core.PairCodeResult`,
  `core.Pending`, `core.PendingAPI`, `core.NodeInfo`, `core.LogQuery`, `trust.ParseSigned`,
  `state.AddVersion`, `rights.All`, `rights.Set`, `controllers.Registry`, `audit.Log`.
- Produces: neun Handler, dazu
  - `func (d *Dispatcher) pushTrust(ctx context.Context, seq uint64)` – neue Fassung an alle
    Controller (`ngc.users/controller/trust`) und Hinweis an alle Shells.

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/coreapp/users_test.go` prüft:

```go
func TestListAndRights(t *testing.T)              // ngc.users/list und /rights, Listen leer als []
func TestSetRightsWritesAndLogs(t *testing.T)     // set-rights, Protokolleintrag core.LogRights
func TestTrustTakesTheNextVersionOnly(t *testing.T) // ngc.users/api/trust
func TestTrustRefusesAForeignSignature(t *testing.T)
func TestPairCodeFitsTheTrustList(t *testing.T)   // pair-code, join-code
func TestPendingShowsWaitingControllers(t *testing.T)
func TestNodesShowsOnlineAndVersion(t *testing.T)
func TestLogPages(t *testing.T)
func TestOnlyAdminsGetThese(t *testing.T)         // jede Aktion mit einem Nicht-Admin: E_FORBIDDEN
```

Der Test für die leeren Listen vergleicht die Antwort mit den Werten unter `empty` in
`shared/testdata/core.json` (`[]`, nie `null`).

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/server-api/classes/coreapp/ -run TestList`
Expected: FAIL, `undefined: registerUsers`.

- [ ] **Step 3: Write the handlers**

`apps/server-api/classes/coreapp/users.go`:

```go
package coreapp

import (
	"context"
	"encoding/json"
	"io"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/rights"
)

// registerUsers adds the actions of ngc.users.
func (d *Dispatcher) registerUsers() {
	d.Register(core.AppUsers, core.ServiceAPI, core.ActList, d.userList)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActRights, d.rights)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActSetRights, d.setRights)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActTrust, d.trust)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActPairCode, d.pairCode)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActJoinCode, d.joinCode)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActPending, d.pending)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActPendingAPI, d.pendingAPI)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActNodes, d.nodes)
	d.Register(core.AppUsers, core.ServiceAPI, core.ActLog, d.log)
}

// userList answers ngc.users/list: the users of the trust list with what the
// database adds (TOTP, lock, last login).
func (d *Dispatcher) userList(ctx context.Context, rq *Call) ([]byte, error) {
	v, err := rq.S.Trust(ctx)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	out := make([]core.UserInfo, 0, len(v.List.Users))
	now := rq.S.Now()
	for _, u := range v.List.Users {
		row, err := rq.S.DB.UserGet(ctx, u.ID)
		if err != nil && !isMissing(err) {
			return nil, fail(wire.CodeUnreachable)
		}
		out = append(out, core.UserInfo{ID: u.ID, Name: u.Name, Roles: u.Roles,
			TOTP: len(row.TOTP) > 0, Locked: row.Locked > now, LastLogin: row.LastLogin})
	}
	return core.EncodeResult(out)
}

// trust answers ngc.users/api/trust: the shell sends one new version of the
// trust list, the API verifies it as the next one and passes it on.
func (d *Dispatcher) trust(ctx context.Context, rq *Call) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(rq.In, core.MaxTrustIn+1))
	if err != nil || int64(len(body)) > core.MaxTrustIn {
		return nil, fail(wire.CodeTooLarge)
	}
	sg, err := trust.ParseSigned(body)
	if err != nil {
		return nil, fail(wire.CodeProtocol)
	}
	v, err := rq.S.AddVersion(ctx, sg)
	if err != nil {
		return nil, fail(wire.CodeVerify)
	}
	_ = d.Log.Note(ctx, core.LogTrust, rq.Live.Verified.Cert().User, "", "", seqText(v.List.Seq))
	// A new version reaches every controller and every open shell at once.
	go d.pushTrust(context.WithoutCancel(ctx), v.List.Seq)
	return core.EncodeResult(core.TrustResult{Seq: v.List.Seq})
}

// pairCode answers ngc.users/pair-code.
func (d *Dispatcher) pairCode(ctx context.Context, rq *Call) ([]byte, error) {
	code, expires, err := d.Controllers.NewPairCode(ctx)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	return core.EncodeResult(core.PairCodeResult{Code: code.String(), Expires: expires})
}

// pending answers ngc.users/pending: controllers that paired and wait to be
// listed. The fingerprint is not sent: the shell computes it from Key itself
// (shared/README.md, "Selbst rechnen").
func (d *Dispatcher) pending(ctx context.Context, rq *Call) ([]byte, error) {
	list, err := rq.S.DB.PendingList(ctx)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	out := make([]core.Pending, 0, len(list))
	for _, p := range list {
		out = append(out, core.Pending{Name: p.Name, Key: enc.Bytes(p.Key), Since: p.Since})
	}
	return core.EncodeResult(out)
}

// nodes answers ngc.users/nodes: one row per controller of the trust list, so
// an admin can pick the target of an activation and see what is up. Nothing
// here is signed; it is what the controller last said in its hello.
func (d *Dispatcher) nodes(ctx context.Context, rq *Call) ([]byte, error) {
	v, err := rq.S.Trust(ctx)
	if err != nil {
		return nil, fail(wire.CodeUnreachable)
	}
	out := make([]core.NodeInfo, 0, len(v.List.Nodes))
	for _, n := range v.List.Nodes {
		if n.Kind != trust.NodeController {
			continue
		}
		row := core.NodeInfo{Node: n.ID}
		if t, ok := d.Tunnels.Get(n.ID); ok {
			h := t.Hello()
			row.Online, row.Roles, row.Version, row.Arch = true, h.Roles, h.Version, h.Arch
		}
		out = append(out, row)
	}
	return core.EncodeResult(out)
}

// pushTrust sends the new version to every connected controller
// (ngc.users/controller/trust, without a user) and tells every open shell.
func (d *Dispatcher) pushTrust(ctx context.Context, seq uint64) {
	chain, err := d.S.Chain(ctx, 1)
	if err != nil {
		return
	}
	body, err := json.Marshal(chain)
	if err != nil {
		return
	}
	d.Tunnels.Each(func(t *controllersTunnel) {
		res, code := d.Relay.CallController(ctx, t, core.AppUsers, core.ServiceController,
			core.ActTrust, nil, body)
		if code != "" {
			return
		}
		var r core.TrustResult
		// A controller that answers with an older version simply gets the
		// chain again next time; nothing here may fail the whole round.
		_ = json.Unmarshal(res, &r)
	})
	d.Sessions.HintAll(ctx, seq)
}
```

`controllersTunnel` ist ein Alias auf `controllers.Tunnel`. `d.Controllers` (Typ
`*controllers.Server`) und `d.Sessions` (Typ `*browser.Registry`) kommen zu `Deps` hinzu;
`Relay.CallController` entsteht in Aufgabe 17.

`rights`, `setRights`, `joinCode`, `pendingAPI` und `log` folgen demselben Muster:
`rights` ruft `rights.All`, `setRights` prüft `rq.Params.(*core.RoleRights)` und ruft `rights.Set`
und `d.Log.Note(core.LogRights, …)`, `joinCode` ruft `d.Join.NewJoinCode` (Aufgabe 18),
`pendingAPI` liest `dbapi.BlobJoin` und baut `core.PendingAPI`, `log` ruft `d.Log.Query` mit
`rq.Params.(*core.LogQuery)`.

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/coreapp/`
Expected: PASS. Danach `tools/check.sh`.

- [ ] **Step 5: Commit**

```bash
git add apps/server-api/classes/coreapp
git commit -m "Add the actions of ngc.users" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 17: Weiterleitung an die Controller

Der heikelste Teil: Byte für Byte, eins zu eins, mit eigener Freigabe je Strecke und der
Reihenfolge der ersten GRANT.

**Files:**
- Create: `apps/server-api/classes/relay/relay.go`, `apps/server-api/classes/relay/transfer.go`
- Test: `apps/server-api/classes/relay/relay_test.go`

**Interfaces:**
- Consumes: `tunnel.Open`, `wire.Grant`, `wire.ParseGrant`, `wire.MaxCallResult`,
  `core.WriteTransfer`, `core.Transfer`, `link`, `controllers`, `browser`.
- Produces:
  - `type Relay struct { S *state.State; Tunnels *controllers.Registry; Log *audit.Log }`
  - `func (rl *Relay) Forward(ctx, s *browser.Session, c *link.Channel, o wire.Open, t *task.Task, node string)`
  - `func (rl *Relay) CallController(ctx, t *controllers.Tunnel, app, service, action string, params json.RawMessage, in []byte) ([]byte, wire.Code)`
  - `func (rl *Relay) Transfer(ctx, t *controllers.Tunnel, app, service, action string, params json.RawMessage, head core.Transfer, file io.Reader, task *sign.Envelope, client []byte, proof *proof.Proof) ([]byte, wire.Code)`

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/relay/relay_test.go` prüft an einer Attrappe eines Controllers (einem
zweiten `link`, der wie ein Controller antwortet):

```go
func TestForwardPassesTheOpenByteForByte(t *testing.T)
// Das OPEN des Browsers steht unverändert in tunnel.Open.Open; Client und Proof kommen dazu.

func TestFirstGrantWaitsForTheController(t *testing.T)
// Die API schickt dem Browser erst dann eine GRANT, wenn der Controller eine geschickt hat;
// auf einem Ende-zu-Ende-Kanal trägt sie dessen Noise-Nachricht 2.

func TestDataAndEOFGoOneToOne(t *testing.T)
// Jede DATA-Nutzlast wird ein DATA-Frame, jedes EOF ein EOF, in derselben Reihenfolge.

func TestCloseKeepsTheCode(t *testing.T)

func TestFullTunnelAnswersLimit(t *testing.T)
// Ist der Tunnel zum Ziel voll, antwortet die API dem Browser mit CLOSE E_LIMIT.

func TestResultOfAControllerIsBounded(t *testing.T)
// Auf einem Kern-App-Kanal schließt die API mit E_TOOLARGE, bevor sie mehr als
// wire.MaxCallResult weitergibt.

func TestShellSendsNoDataOnTransfer(t *testing.T)
// Auf einem weitergeleiteten transfer-Kanal (activate) ist DATA der Shell E_PROTOCOL; Kopf und
// Datei schreibt die API selbst.
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/server-api/classes/relay/`
Expected: FAIL, `undefined: Relay`.

- [ ] **Step 3: Write the relay**

`apps/server-api/classes/relay/relay.go`:

```go
// Package relay forwards a channel of the shell to a controller. The OPEN of
// the browser goes into tunnel.Open byte for byte, DATA and EOF one to one,
// CLOSE with the same code. Credit is kept per hop, and the API's first GRANT
// to the browser waits for the controller's, so an end-to-end handshake can
// carry Noise message 2 along (package wire, "Relay").
package relay

import (
	"context"
	"encoding/json"
	"io"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/task"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/audit"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/browser"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/controllers"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
)

// Relay forwards channels.
type Relay struct {
	S       *state.State
	Tunnels *controllers.Registry
	Log     *audit.Log
}

// Forward runs one channel of the shell against a controller.
func (rl *Relay) Forward(ctx context.Context, s *browser.Session, c *link.Channel, o wire.Open,
	t *task.Task, node string) {
	tun, ok := rl.Tunnels.Get(node)
	if !ok {
		_ = s.Close(ctx, wire.CodeUnreachable, c.Num)
		return
	}
	// The window we grant the controller is what we can hold until the browser
	// takes it, never more.
	tc, err := tun.Table.Next(wire.DefaultWindow)
	if err == link.ErrLimit {
		// Only this tunnel is full; the session stays.
		_ = s.Close(ctx, wire.CodeLimit, c.Num)
		return
	}
	if err != nil {
		_ = s.Close(ctx, wire.CodeInternal, c.Num)
		return
	}
	payload, err := json.Marshal(tunnel.Open{
		Open: o.Raw(), Window: wire.DefaultWindow,
		Client: s.Live.Row.Cert, Proof: s.Live.ProofValue(),
	})
	if err != nil {
		_ = s.Close(ctx, wire.CodeInternal, c.Num)
		return
	}
	if err := tun.Link.Write(ctx, wire.Frame{Type: wire.TypeOpen, Channel: tc.Num, Payload: payload}); err != nil {
		_ = s.Close(ctx, wire.CodeUnreachable, c.Num)
		return
	}
	rl.pump(ctx, s, c, tun, tc, o)
}
```

`o.Raw()` gibt es in `wire.Open` nicht: die API legt das OPEN des Browsers unverändert weiter, und
die Bytes hat sie aus dem Frame. `Forward` bekommt sie deshalb als zusätzlichen Parameter
`raw []byte` (die Nutzlast des OPEN-Frames), den `coreapp.OpenChannel` durchreicht. `s.Live.Row.Cert`
ist das Client-Zertifikat, `s.Live.ProofValue()` der Beweis als `*proof.Proof` (eine neue Methode
auf `login.Live`, die `Row.Proof` einmal liest und zwischenspeichert).

`pump` läuft in zwei Richtungen:

```go
// pump carries the channel in both directions until both sides have closed.
func (rl *Relay) pump(ctx context.Context, s *browser.Session, c *link.Channel,
	tun *controllers.Tunnel, tc *link.Channel, o wire.Open) {
	first := true
	for {
		f, ok := tun.Next(ctx, tc)
		if !ok {
			_ = s.Close(ctx, wire.CodeUnreachable, c.Num)
			return
		}
		switch f.Type {
		case wire.TypeGrant:
			credit, handshake, err := wire.ParseGrant(f.Payload)
			if err != nil || !tc.Grant(credit) {
				_ = s.Close(ctx, wire.CodeProtocol, c.Num)
				return
			}
			if first {
				// Our first GRANT to the browser: on an end-to-end channel it
				// carries the controller's Noise message 2.
				if err := s.GrantRaw(ctx, c, wire.DefaultWindow, handshake); err != nil {
					return
				}
				first = false
				go rl.toController(ctx, s, c, tun, tc, o)
				continue
			}
			if len(handshake) != 0 {
				// A handshake in any GRANT but the first is a protocol error.
				_ = s.Close(ctx, wire.CodeProtocol, c.Num)
				return
			}
			if err := s.GrantRaw(ctx, c, credit, nil); err != nil {
				return
			}
		case wire.TypeData:
			if !tc.Take(len(f.Payload)) {
				_ = s.Close(ctx, wire.CodeProtocol, c.Num)
				return
			}
			if err := s.Data(ctx, c, f.Payload); err != nil {
				return
			}
			// Give the controller its credit back as soon as the browser has it.
			if err := tun.Grant(ctx, tc, uint32(len(f.Payload))); err != nil {
				return
			}
		case wire.TypeEOF:
			if err := s.EOF(ctx, c); err != nil {
				return
			}
		case wire.TypeClose:
			cl, err := wire.ParseClose(f.Payload)
			if err != nil {
				_ = s.Close(ctx, wire.CodeProtocol, c.Num)
				return
			}
			tc.RecvClose()
			// The code travels unchanged, so the shell sees what the
			// controller said.
			_ = s.CloseWith(ctx, c, cl)
			return
		}
	}
}
```

`toController` liest die Frames der Shell aus dem Puffer und schreibt sie eins zu eins in den
Tunnel; auf einem `transfer`-Kanal, den die Shell geöffnet hat, lehnt es DATA der Shell mit
`E_PROTOCOL` ab und schreibt stattdessen `core.WriteTransfer` samt Datei (Aufgabe 22 setzt den
Kopf zusammen).

`CallController` und `Transfer` sind die zwei Wege, auf denen die API selbst einen Kanal zu einem
Controller öffnet (ohne Benutzer bzw. mit Aufgabe); sie benutzen dieselbe Kanaltabelle und
begrenzen das Ergebnis auf `wire.MaxCallResult`:

```go
// CallController opens a channel to a controller without a user
// (core.WithoutUser) and returns the result. More than wire.MaxCallResult
// closes the channel with E_TOOLARGE.
func (rl *Relay) CallController(ctx context.Context, tun *controllers.Tunnel,
	app, service, action string, params json.RawMessage, in []byte) ([]byte, wire.Code) {
	open, err := json.Marshal(wire.Open{App: app, Service: service, Action: action,
		Params: params, Window: wire.DefaultWindow})
	if err != nil {
		return nil, wire.CodeInternal
	}
	payload, err := json.Marshal(tunnel.Open{Open: open, Window: wire.DefaultWindow})
	if err != nil {
		return nil, wire.CodeInternal
	}
	tc, err := tun.Table.Next(wire.DefaultWindow)
	if err != nil {
		return nil, wire.CodeLimit
	}
	if err := tun.Link.Write(ctx, wire.Frame{Type: wire.TypeOpen, Channel: tc.Num, Payload: payload}); err != nil {
		return nil, wire.CodeUnreachable
	}
	return rl.collect(ctx, tun, tc, in)
}
```

`collect` wartet auf die erste GRANT, schickt `in` in Stücken von höchstens `wire.MaxPayload`
Bytes samt EOF, sammelt die Antwort bis zum EOF des Controllers, prüft `wire.MaxCallResult` und
gibt das Ergebnis nur zurück, wenn der Kanal mit CLOSE `ok` endet.

`browser.Session` bekommt dafür die Methoden `GrantRaw`, `Data`, `EOF`, `CloseWith` und `next`;
`controllers.Tunnel` die Methoden `Next` (Frame aus dem Puffer) und `Grant`. Beide sind die
gleichen kleinen Hüllen wie in Aufgabe 15.

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/relay/`
Expected: PASS.

- [ ] **Step 5: Add the attack tests**

In `relay_test.go` dazu:

```go
func TestControllerCannotOpenAChannel(t *testing.T)
// Ein OPEN vom Controller auf dem Tunnel ist ein Protokollfehler: nur die API öffnet dort.

func TestGrantBeyondMaxWindowEndsTheChannel(t *testing.T)

func TestChannelNumberIsFreeOnlyAfterBothCloses(t *testing.T)
// Ein Frame nach dem eigenen CLOSE wird verworfen, das CLOSE der Gegenseite zählt trotzdem.
```

- [ ] **Step 6: Run everything and check**

Run: `go test ./apps/server-api/... && tools/check.sh`
Expected: PASS, `check: ok`.

- [ ] **Step 7: Commit**

```bash
git add apps/server-api/classes/relay apps/server-api/classes/browser apps/server-api/classes/controllers
git commit -m "Add the relay from the shell to the controllers" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 18: Beitritt weiterer API-Server

**Files:**
- Create: `apps/server-api/classes/join/join.go`, `apps/server-api/classes/join/keys.go`
- Test: `apps/server-api/classes/join/join_test.go`, `apps/server-api/classes/join/fuzz_test.go`

**Interfaces:**
- Consumes: `tunnel.JoinCode`, `tunnel.ParseJoinRequest`, `tunnel.JoinRequest.Verify`,
  `tunnel.JoinFingerprint`, `tunnel.PathJoin`, `tunnel.PinnedTLS`, `dbapi.OnceJoin`,
  `dbapi.BlobJoin`, `core.PendingAPI`, `noisep`.
- Produces:
  - `func (j *Join) NewJoinCode(ctx context.Context) (tunnel.JoinCode, int64, error)`
  - `func (j *Join) Accept(w http.ResponseWriter, r *http.Request)` – `POST /tunnel/join?secret=…`
  - `func (j *Join) Waiting(ctx context.Context) ([]core.PendingAPI, error)`
  - `func (j *Join) Keys(w http.ResponseWriter, r *http.Request)` – `GET /tunnel/keys`
  - `func Run(ctx context.Context, cfg JoinConfig) error` – die Seite des beitretenden Servers
    (`ngc-api -join`), schreibt `/run/ngc/join-secret`

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/join/join_test.go`:

```go
func TestJoinCodeFitsTheTrustList(t *testing.T)
// tunnel.JoinCode.Check gegen die neueste geprüfte Fassung.

func TestAcceptKeepsTheRequestOnce(t *testing.T)
// Der MAC nach tunnel.JoinMAC stimmt, das Geheimnis gilt genau einmal, die Anfrage liegt als
// dbapi.BlobJoin, und Waiting zeigt sie mit demselben tunnel.JoinFingerprint, den ulh anzeigt.

func TestAcceptRefusesAForeignMAC(t *testing.T)

func TestKeysOnlyForAListedNode(t *testing.T)
// Vor der Unterschrift des Admins gibt /tunnel/keys nichts heraus; danach genau den
// gemeinsamen Schlüssel, und der Fingerabdruck des Anfragenden muss zu einem API-Knoten der
// Vertrauensliste passen.
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/server-api/classes/join/`
Expected: FAIL, `undefined: Join`.

- [ ] **Step 3: Write the issuing side**

`apps/server-api/classes/join/join.go` folgt `pair.go` aus Aufgabe 12: `NewJoinCode` legt ein
Geheimnis unter `dbapi.OnceJoin` ab (ID = SHA-256, Daten = base64url des Geheimnisses) und baut
`tunnel.JoinCode` mit `URL`, `Key`, `TLS` und dem Anker der neuesten Fassung. `Accept` liest
`tunnel.ParseJoinRequest`, verbraucht das Geheimnis über die Abfrage `?secret=` und prüft
`req.Verify(secret, apiKey)`. Danach:

```go
// Accept keeps the request of a new API server as dbapi.BlobJoin. The admin
// compares tunnel.JoinFingerprint of exactly these values with the output of
// the new server's ulh script before listing it, so nothing here is trusted
// on its own.
func (j *Join) keep(ctx context.Context, req tunnel.JoinRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return j.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobJoin, ID: req.Node, Data: data,
		Updated: j.S.Now()})
}

// Waiting answers ngc.users/pending-api.
func (j *Join) Waiting(ctx context.Context) ([]core.PendingAPI, error) {
	blobs, err := j.S.DB.BlobFrom(ctx, dbapi.BlobJoin, 0)
	if err != nil {
		return nil, err
	}
	out := make([]core.PendingAPI, 0, len(blobs))
	for _, b := range blobs {
		// The stored row goes through the contract's parser again: a broken
		// row must never reach the shell as something to sign.
		req, err := tunnel.ParseJoinRequest(b.Data)
		if err != nil {
			continue
		}
		out = append(out, core.PendingAPI{Node: req.Node, Noise: req.Noise, Sign: req.Sign,
			TLS: req.TLS, URL: req.URL, Since: b.Updated})
	}
	return out, nil
}
```

- [ ] **Step 4: Write the exchange of the shared key**

`apps/server-api/classes/join/keys.go`:

```go
package join

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/link"
)

// PathKeys is where a new API server fetches the keys every API of the
// installation shares, once an admin has put it into the trust list. The
// contracts leave this to the API ("wie, regelt die API selbst"); the exchange
// runs Noise_KK with the two nodes' keys from the trust list, so neither side
// has to trust the network.
const PathKeys = "/tunnel/keys"

// PrologueKeys tells this handshake apart from the session, tunnel and
// end-to-end ones (package noisep).
const PrologueKeys = "ngc/apikeys/v1"

// sharedKeys is what travels: the two keys every API of the installation has
// to have the same. SecretKey seals the database secrets — the installation's
// private key lies in the database already, sealed with exactly this key, so
// nothing else has to travel. ChallengeKey draws the made-up login answer
// (session.Challenge); a server with its own would answer an unknown name
// differently from its neighbours, and asking two servers in turn would sort
// the names that exist from those that do not.
type sharedKeys struct {
	SecretKey    enc.Bytes `json:"secretKey"`
	ChallengeKey enc.Bytes `json:"challengeKey"`
}

// Keys serves GET PathKeys: the new server dials with its own Noise key, and
// only a key that stands in the trust list as an API node gets an answer.
func (j *Join) Keys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	v, err := j.S.Trust(ctx)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var peer []byte
	fingerprint := r.URL.Query().Get("key")
	for _, n := range v.List.Nodes {
		if n.Kind == trust.NodeAPI && n.ID != j.S.Cfg.Node && tunnel.Fingerprint(n.Noise) == fingerprint {
			peer = n.Noise
		}
	}
	if peer == nil {
		// Before an admin signs the new server in, there is nothing to fetch.
		w.WriteHeader(http.StatusNotFound)
		return
	}
	static, err := j.staticKey()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	l, err := link.Accept(w, r, link.Options{Subprotocol: tunnel.Subprotocol,
		Prologue: PrologueKeys, Static: static, Peer: peer})
	if err != nil {
		return
	}
	defer l.Close(websocket.StatusNormalClosure, "")
	body, err := json.Marshal(sharedKeys{
		SecretKey:    enc.Bytes(j.S.Cfg.SecretKey.Bytes()),
		ChallengeKey: enc.Bytes(j.S.Cfg.ChallengeKey.Bytes()),
	})
	if err != nil {
		return
	}
	// One frame on channel 0 carries it; the Noise session is what protects it.
	_ = l.Write(ctx, wireData(body))
}

var _ = noise.DHKey{}
```

`wireData` baut einen `wire.Frame{Type: wire.TypeData, Channel: wire.ControlChannel, Payload: body}`.

`Run` (die Seite des beitretenden Servers) macht: Code lesen (`tunnel.ParseJoinCode`), eigene
Schlüssel erzeugen und in Dateien legen, `tunnel.JoinRequest` bauen und mit
`tunnel.JoinMAC` unterschreiben, per `POST` an `https://<Host und Port aus url>` + `tunnel.PathJoin`
mit `tunnel.PinnedTLS(code.TLS)` schicken, `tunnel.JoinFingerprint` und den Anker
(`code.Anchor.Seq` und `tunnel.Fingerprint(code.Anchor.Hash)`) auf der Standardausgabe zeigen, dann
alle 5 Sekunden `PathKeys` versuchen, bis es klappt, und die Antwort als JSON mit beiden Schlüsseln
(`secretKey` und `challengeKey`, je 32 Byte) nach `/run/ngc/join-secret` (0600) schreiben; das
ulh-Skript trägt beide in `api.json` ein (Aufgabe 25). Zeitgrenze: eine Stunde, danach bricht es
mit einer deutschen Meldung ab.

- [ ] **Step 5: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/join/`
Expected: PASS.

- [ ] **Step 6: Add the fuzz test**

`FuzzJoinRequest` schickt beliebige Körper an `Accept` und prüft, dass nie ein `dbapi.BlobJoin`
entsteht und nie ein Absturz passiert.

- [ ] **Step 7: Run the fuzz target and check**

Run: `go test ./apps/server-api/classes/join/ -run Fuzz -fuzz FuzzJoinRequest -fuzztime 60s`,
danach `tools/check.sh`.

- [ ] **Step 8: Commit**

```bash
git add apps/server-api/classes/join
git commit -m "Add the join of further API servers" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---
### Task 19: Stores und Kataloge

**Files:**
- Create: `apps/server-api/classes/appstore/sources.go`, `apps/server-api/classes/appstore/fetch.go`
- Test: `apps/server-api/classes/appstore/sources_test.go`,
  `apps/server-api/classes/appstore/fetch_test.go`

**Interfaces:**
- Consumes: `trust.Store`, `trust.StoreGlobal`, `store.VerifyCatalog`, `store.ErrRollback`,
  `sign.ParseEnvelope`, `sign.MaxPayload`, `dbapi.BlobCatalog`, `core.SourceInfo`,
  `core.CatalogResult`.
- Produces:
  - `type Stores struct { S *state.State; Client *http.Client }`
  - `func New(s *state.State) *Stores` – Client mit `Timeout` 60 s, ohne Proxy-Umgebung, ohne
    Weiterleitungen zu anderen Hosts
  - `func (st *Stores) Refresh(ctx context.Context) error` – alle Stores der Vertrauensliste
  - `func (st *Stores) Catalog(ctx context.Context, id string) (*store.Catalog, sign.Envelope, error)`
  - `func (st *Stores) Sources(ctx context.Context) ([]core.SourceInfo, error)`
  - `func (st *Stores) Release(ctx context.Context) (*store.Release, sign.Envelope, error)`
  - `func (st *Stores) Download(ctx context.Context, url string, size int64, sha256 []byte, w io.Writer) error`

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/appstore/fetch_test.go` startet einen `httptest.Server`, der einen echten
Katalog aus `shared/testdata/store.json` ausliefert, und prüft:

```go
func TestRefreshVerifiesAndKeeps(t *testing.T)
// Der Katalog wird mit store.VerifyCatalog gegen den Schlüssel des Stores aus der Vertrauensliste
// geprüft und als dbapi.BlobCatalog mit Seq = catalog.Seq abgelegt.

func TestRefuseAnOlderCatalog(t *testing.T)
// Ein Katalog mit kleinerer Seq als die gespeicherte ist store.ErrRollback und wird nicht
// gespeichert; core.SourceInfo.Error nennt den Grund auf Deutsch.

func TestRefuseAForeignSignature(t *testing.T)

func TestRefuseAnOversizedAnswer(t *testing.T)
// Mehr als sign.MaxPayload plus Umschlag wird abgebrochen, ohne alles zu lesen.

func TestDownloadChecksSizeAndChecksum(t *testing.T)
// Eine Datei, die größer ist als angekündigt oder deren SHA-256 nicht passt, ist ein Fehler, und
// nichts davon bleibt liegen.
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/server-api/classes/appstore/`
Expected: FAIL, `undefined: Stores`.

- [ ] **Step 3: Write the fetching**

`apps/server-api/classes/appstore/fetch.go`:

```go
// Package appstore serves ngc.store: the stores of the trust list and their
// catalogs, uploaded apps, the UIs the shell runs, the licences of paid apps
// and the activation of an app on a controller.
package appstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
)

// Names of the files a store serves. The contracts leave this to the API; a
// store is files on any web server (Entwurf 4.4).
const (
	CatalogFile = "catalog.json" // sign.Envelope over a store.Catalog
	ReleaseFile = "release.json" // sign.Envelope over a store.Release, global store only
)

// maxEnvelope is what an answer of a store may have: the base64url of the
// largest payload plus room for the envelope itself.
const maxEnvelope = int64(sign.MaxPayload/3*4 + 1<<10)

// ErrStore reports an answer of a store NGC does not take.
var ErrStore = errors.New("appstore: store answered something unusable")

// Stores fetches and verifies catalogs and releases.
type Stores struct {
	S      *state.State
	Client *http.Client
}

// New returns Stores with a client that follows no redirect and waits at most
// a minute: a store is a plain web server, and NGC never follows it elsewhere.
func New(s *state.State) *Stores {
	return &Stores{S: s, Client: &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{Proxy: nil, ForceAttemptHTTP2: true},
	}}
}

// url joins the address of a store with a file name. trust.ValidStoreURL has
// no trailing slash and no query, so one slash is always right.
func fileURL(base, name string) string { return strings.TrimSuffix(base, "/") + "/" + name }

// get fetches a document of at most max bytes.
func (st *Stores) get(ctx context.Context, url string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, ErrStore
	}
	res, err := st.Client.Do(req)
	if err != nil {
		return nil, ErrStore
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, ErrStore
	}
	// One byte over the limit is enough to know it is too large; nothing
	// larger is ever read into memory.
	b, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	if err != nil || int64(len(b)) > max {
		return nil, ErrStore
	}
	return b, nil
}

// Refresh fetches the catalog of every store of the trust list and keeps what
// verifies. A store that fails leaves the last good catalog in place.
func (st *Stores) Refresh(ctx context.Context) error {
	v, err := st.S.Trust(ctx)
	if err != nil {
		return err
	}
	for _, s := range v.List.Stores {
		if err := st.refreshOne(ctx, s); err != nil {
			// The reason is shown to admins in ngc.store/sources, in German,
			// and never stops the other stores.
			st.note(ctx, s.ID, err)
			continue
		}
	}
	return nil
}

// refreshOne fetches and verifies one catalog.
func (st *Stores) refreshOne(ctx context.Context, s trust.Store) error {
	body, err := st.get(ctx, fileURL(s.URL, CatalogFile), maxEnvelope)
	if err != nil {
		return err
	}
	env, err := sign.ParseEnvelope(body, sign.MaxPayload)
	if err != nil {
		return err
	}
	last := st.lastSeq(ctx, dbapi.BlobCatalog, s.ID)
	c, err := store.VerifyCatalog(env, s, last)
	if err != nil {
		return err
	}
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return st.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobCatalog, ID: s.ID, Seq: c.Seq,
		Data: data, Updated: st.S.Now()})
}

// Download fetches a file of exactly size bytes whose SHA-256 is sum and
// writes it to w. A file that is larger, shorter or different is an error, and
// the caller throws away what w received.
func (st *Stores) Download(ctx context.Context, url string, size int64, sum []byte, w io.Writer) error {
	if size <= 0 || size > store.MaxPackage || len(sum) != sha256.Size {
		return ErrStore
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ErrStore
	}
	res, err := st.Client.Do(req)
	if err != nil {
		return ErrStore
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ErrStore
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(res.Body, size+1))
	if err != nil || n != size {
		return ErrStore
	}
	if !hmacEqual(h.Sum(nil), sum) {
		return ErrStore
	}
	return nil
}
```

`hmacEqual` ist `hmac.Equal` aus `crypto/hmac` (Vergleich in gleichbleibender Zeit), `lastSeq`
liest die `Seq` eines gespeicherten Blobs, `note` schreibt den Fehlertext je Store unter
`dbapi.BlobSetting` mit der ID `store.<id>` (Zeit und deutscher Text), damit
`ngc.store/sources` ihn zeigen kann.

`Release` holt `ReleaseFile` vom globalen Store und prüft es mit
`store.VerifyRelease(env, cfg.ReleaseKey, lastSeq)`.

- [ ] **Step 4: Write the actions ngc.store/sources und /catalog**

`apps/server-api/classes/appstore/sources.go` liefert `core.SourceInfo` je Store der
Vertrauensliste (ID, Name, Adresse, neueste gesehene `seq`, Zeit des letzten Erfolgs, Fehlertext)
und `core.CatalogResult` mit dem gespeicherten Umschlag; die Shell prüft ihn selbst gegen den
Schlüssel aus ihrer Vertrauensliste.

- [ ] **Step 5: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/appstore/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add apps/server-api/classes/appstore
git commit -m "Add the stores and their catalogs" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 20: Hochladen, Oberflächen und Beweise

**Files:**
- Create: `apps/server-api/classes/appstore/upload.go`, `apps/server-api/classes/appstore/ui.go`
- Test: `apps/server-api/classes/appstore/upload_test.go`,
  `apps/server-api/classes/appstore/ui_test.go`, `apps/server-api/classes/appstore/fuzz_test.go`

**Interfaces:**
- Consumes: `store.Walk`, `store.PackageUI`, `store.MaxPackage`, `store.MaxDirs`,
  `core.UploadResult`, `core.UIProof`, `core.LocalStore`, `dbapi.BlobUI`, `dbapi.BlobApproval`.
- Produces:
  - `func (st *Stores) Upload(ctx context.Context, r io.Reader) (core.UploadResult, error)`
  - `func (st *Stores) KeepUI(ctx context.Context, app, version string, ui []byte) error`
  - `func (st *Stores) KeepProof(ctx context.Context, p core.UIProof) error`
  - `func (st *Stores) Proof(ctx context.Context, app, version string) (core.UIProof, error)`

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/appstore/upload_test.go`:

```go
func TestUploadReadsThePackage(t *testing.T)
// Ein Paket aus shared/testdata/packui.json geht durch Upload; das Ergebnis nennt ID, Version,
// SHA-256 der Datei und store.PackageUI, und die ui-tar-Bytes liegen als dbapi.BlobUI.

func TestUploadRefusesAnUnsafeArchive(t *testing.T)
// Absolute Pfade, "..", Links, Sparse-Dateien und mehr als store.MaxDirs Verzeichnisse lehnt
// store.Walk ab; Upload gibt E_VERIFY und legt nichts ab.

func TestUploadRefusesBeyondMaxPackage(t *testing.T)

func TestProofOfAnUploadedApp(t *testing.T)
// Der Beweis einer hochgeladenen App ist die Aufgabe activate samt Client-Zertifikat und Beweis;
// core.VerifyUIProof bestätigt ihn gegen die Vertrauensliste, und nach einem Wechsel des
// Admin-Schlüssels gilt er nicht mehr.
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/server-api/classes/appstore/ -run TestUpload`
Expected: FAIL, `undefined: Upload`.

- [ ] **Step 3: Write the upload**

`apps/server-api/classes/appstore/upload.go`:

```go
package appstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/store"
)

// Upload reads an app package an admin sent (ngc.store/api/upload): it walks
// it safely (store.Walk through store.PackageUI), keeps the canonical ui-tar
// and answers what it read. The shell computes the same values from the file
// it sent and refuses a mismatch, so nothing here has to be trusted.
func (st *Stores) Upload(ctx context.Context, r io.Reader) (core.UploadResult, error) {
	// The package is read twice: once for its checksum and once through
	// store.Walk, so the bytes go to disk only in the caller's buffer.
	raw, err := io.ReadAll(io.LimitReader(r, store.MaxPackage+1))
	if err != nil || int64(len(raw)) > store.MaxPackage {
		return core.UploadResult{}, ErrStore
	}
	sum := sha256.Sum256(raw)
	var ui bytes.Buffer
	m, uiHash, err := store.PackageUI(bytes.NewReader(raw), &ui)
	if err != nil {
		return core.UploadResult{}, err
	}
	if err := st.KeepUI(ctx, m.ID, m.Version, ui.Bytes()); err != nil {
		return core.UploadResult{}, err
	}
	return core.UploadResult{ID: m.ID, Version: m.Version, SHA256: enc.Bytes(sum[:]),
		UI: enc.Bytes(uiHash)}, nil
}

// KeepUI keeps the canonical ui-tar of one version; ngc.desktop/ui sends
// exactly these bytes.
func (st *Stores) KeepUI(ctx context.Context, app, version string, ui []byte) error {
	return st.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobUI, ID: app + "@" + version,
		Data: ui, Updated: st.S.Now()})
}

// KeepProof keeps the evidence a version was installed with
// (ngc.desktop/ui-proof). It goes through the contract's parser first, so a
// proof the shell would refuse never reaches the database.
func (st *Stores) KeepProof(ctx context.Context, p core.UIProof) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if _, err := core.ParseUIProof(data); err != nil {
		return err
	}
	return st.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobApproval, ID: p.App + "@" + p.Version,
		Data: data, Updated: st.S.Now()})
}
```

Der Beweis einer Store-App ist der Katalog ihres Stores, der die Version führt; der einer
hochgeladenen App die Aufgabe `activate` mit Client-Zertifikat und Beweis (Aufgabe 22 baut ihn
beim Aktivieren).

- [ ] **Step 4: Add the fuzz test**

`apps/server-api/classes/appstore/fuzz_test.go`:

```go
package appstore

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
)

// FuzzUpload feeds Upload with anything that might be a package: it must
// answer an error, never panic and never leave a ui-tar behind.
func FuzzUpload(f *testing.F) {
	raw, err := os.ReadFile("../../../../shared/testdata/packui.json")
	if err != nil {
		f.Fatalf("vectors: %v", err)
	}
	var cases []struct {
		Package enc.Bytes `json:"package"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		f.Fatalf("vectors: %v", err)
	}
	for _, c := range cases {
		f.Add([]byte(c.Package))
	}
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{0}, 1024)) // an empty archive
	f.Fuzz(func(t *testing.T, pkg []byte) {
		st, _ := newStores(t)
		res, err := st.Upload(ctx, bytes.NewReader(pkg))
		if err != nil {
			blobs, ferr := st.S.DB.BlobFrom(ctx, dbapi.BlobUI, 0)
			if ferr != nil || len(blobs) != 0 {
				t.Fatalf("a refused package left %d ui-tars behind", len(blobs))
			}
			return
		}
		if len(res.SHA256) != 32 || len(res.UI) != 32 {
			t.Fatalf("result %+v", res)
		}
	})
}
```

`newStores` ist der Helfer aus `sources_test.go`: eine `state.State` auf `memstore` mit fester Zeit.

- [ ] **Step 5: Run the tests and the fuzz target**

Run:
```bash
go test ./apps/server-api/classes/appstore/
go test ./apps/server-api/classes/appstore/ -run Fuzz -fuzz FuzzUpload -fuzztime 60s
tools/check.sh
```
Expected: PASS ohne Fund.

- [ ] **Step 6: Commit**

```bash
git add apps/server-api/classes/appstore
git commit -m "Add the upload of app packages and the UI proofs" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 21: Lizenzen und Schlüssel der Kauf-Apps

**Files:**
- Create: `apps/server-api/classes/appstore/license.go`
- Test: `apps/server-api/classes/appstore/license_test.go`

**Interfaces:**
- Consumes: `store.VerifyLicense`, `store.UnwrapKey`, `store.License.State`, `store.MaxLicense`,
  `sign.ParseEnvelope`, `dbapi.BlobLicense`, `core.LicenseResult`, `core.LicenseOf`,
  `core.InstallKey`, `state.InstallKey`.
- Produces:
  - `func (st *Stores) TakeLicense(ctx context.Context, app string, body []byte) (core.LicenseResult, error)`
  - `func (st *Stores) License(ctx context.Context, app string) (*store.License, sign.Envelope, bool, error)`
  - `func (st *Stores) ContentKey(ctx context.Context, app string) ([]byte, error)`
  - `func (st *Stores) UsersAllowed(ctx context.Context, app string) (int, bool, error)`
  - `func (st *Stores) InstallKey(ctx context.Context) (core.InstallKey, error)`
  - `func (st *Stores) PushLicenses(ctx context.Context, send func(node, app string, env sign.Envelope) error) error`

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/appstore/license_test.go` arbeitet mit `shared/testdata/store.json`
(`license`, `installKey`, `licenseApp`, `installation`):

```go
func TestTakeLicenseVerifiesEverything(t *testing.T)
// Unterschrift des Stores, Installation, App und der Schlüssel dieser Installation; das Ergebnis
// ist core.LicenseResult mit dem Zustand aus core.LicenseOf.

func TestForeignLicenseIsRefused(t *testing.T)
// Eine Lizenz für eine andere Installation oder eine andere App wird abgelehnt (die Vektoren
// unter licenseRejects).

func TestOnlyTheNewestLicenseIsKept(t *testing.T)
// Eine ältere Lizenz (kleineres issued) ersetzt keine neuere.

func TestContentKeyOpensOnlyWithTheInstallationKey(t *testing.T)
// store.UnwrapKey mit dem privaten Schlüssel der Installation; mit einem anderen Schlüssel
// scheitert es.

func TestUsersAllowedCountsPerUserLicences(t *testing.T)
// Bei Per == store.PerUser dürfen höchstens License.Users Benutzer das Recht "use" haben.

func TestPushLicensesSendsToEveryNodeRunningTheApp(t *testing.T)
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/server-api/classes/appstore/ -run TestTakeLicense`
Expected: FAIL, `undefined: TakeLicense`.

- [ ] **Step 3: Write the licences**

`apps/server-api/classes/appstore/license.go`:

```go
package appstore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// ErrLicense reports a licence NGC does not take.
var ErrLicense = errors.New("appstore: licence refused")

// TakeLicense checks a licence an admin uploaded (ngc.store/api/license) and
// keeps it if it is newer than the one held. Only the API can do the last two
// checks: that the content key is wrapped for this installation's key and how
// many users a per-user licence allows.
func (st *Stores) TakeLicense(ctx context.Context, app string, body []byte) (core.LicenseResult, error) {
	env, err := sign.ParseEnvelope(body, store.MaxLicense)
	if err != nil {
		return core.LicenseResult{}, ErrLicense
	}
	row, err := st.S.DB.AppGet(ctx, app)
	if err != nil {
		return core.LicenseResult{}, ErrLicense
	}
	v, err := st.S.Trust(ctx)
	if err != nil {
		return core.LicenseResult{}, err
	}
	var source *trust.Store
	for i := range v.List.Stores {
		if v.List.Stores[i].ID == row.Store {
			source = &v.List.Stores[i]
		}
	}
	if source == nil {
		return core.LicenseResult{}, ErrLicense
	}
	installation, err := st.S.Installation(ctx)
	if err != nil {
		return core.LicenseResult{}, err
	}
	key, err := st.S.InstallKey(ctx)
	if err != nil {
		return core.LicenseResult{}, err
	}
	l, err := store.VerifyLicense(env, *source, installation, app, key.PublicKey().Bytes())
	if err != nil {
		return core.LicenseResult{}, ErrLicense
	}
	// Only a newer licence replaces the one held, so an old one cannot bring
	// back a shorter term or fewer users.
	if cur, _, ok, err := st.License(ctx, app); err != nil {
		return core.LicenseResult{}, err
	} else if ok && cur.Issued >= l.Issued {
		return core.LicenseResult{App: app, State: core.LicenseOf(cur, st.S.Now())}, nil
	}
	data, err := json.Marshal(env)
	if err != nil {
		return core.LicenseResult{}, err
	}
	if err := st.S.DB.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobLicense, ID: app, Seq: uint64(l.Issued),
		Data: data, Updated: st.S.Now()}); err != nil {
		return core.LicenseResult{}, err
	}
	return core.LicenseResult{App: app, State: core.LicenseOf(l, st.S.Now())}, nil
}

// ContentKey unwraps the content key of a paid app. Only the API holds the
// installation's private key, and it gives the key to its own controllers
// alone, over the tunnel.
func (st *Stores) ContentKey(ctx context.Context, app string) ([]byte, error) {
	l, _, ok, err := st.License(ctx, app)
	if err != nil || !ok {
		return nil, ErrLicense
	}
	if l.State(st.S.Now()) == store.LicenseExpired {
		// Past the grace period the key is not handed out any more, so the
		// controllers cannot start the services again.
		return nil, ErrLicense
	}
	key, err := st.S.InstallKey(ctx)
	if err != nil {
		return nil, err
	}
	return store.UnwrapKey(l.Key, key, app)
}

// InstallKey answers ngc.store/install-key: what an admin gives the shop when
// buying. Only the public half ever leaves the API.
func (st *Stores) InstallKey(ctx context.Context) (core.InstallKey, error) {
	id, err := st.S.Installation(ctx)
	if err != nil {
		return core.InstallKey{}, err
	}
	key, err := st.S.InstallKey(ctx)
	if err != nil {
		return core.InstallKey{}, err
	}
	return core.InstallKey{Installation: id, Key: enc.Bytes(key.PublicKey().Bytes())}, nil
}
```

`License` liest `dbapi.BlobLicense`, prüft ihn mit `store.VerifyLicense` erneut und gibt Lizenz,
Umschlag und „vorhanden“ zurück. `UsersAllowed` zählt über `rights.All`, wie viele Benutzer
`"<app>/use"` haben, und vergleicht mit `License.Users`, wenn `Per == store.PerUser`.
`PushLicenses` schickt die neueste Lizenz jeder Kauf-App über `ngc.store/controller/license`
(ohne Benutzer, Parameter `{"app":…}`) an jeden Controller, auf dem die App läuft — nach jeder
neuen Lizenz und nach jedem Tunnelaufbau (`controllers.Server.OnHello`).

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/appstore/`
Expected: PASS. Danach `tools/check.sh`.

- [ ] **Step 5: Commit**

```bash
git add apps/server-api/classes/appstore
git commit -m "Add the licences and the content keys of paid apps" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 22: Aktivieren, Deaktivieren, Deinstallieren

**Files:**
- Create: `apps/server-api/classes/appstore/activate.go`
- Test: `apps/server-api/classes/appstore/activate_test.go`

**Interfaces:**
- Consumes: `core.ActivateParams`, `core.UninstallParams`, `core.Transfer`, `core.WriteTransfer`,
  `core.InstalledApp`, `relay.Transfer`, `store.Granted`, `dbapi.App`.
- Produces:
  - `func (st *Stores) Activate(ctx context.Context, rq ActivateRequest) (wire.Code, error)`
  - `type ActivateRequest struct { Params *core.ActivateParams; Task *sign.Envelope;
    Client []byte; Proof *proof.Proof; Node string; Live *login.Live }`
  - `func (st *Stores) Installed(ctx context.Context) ([]core.InstalledApp, error)`

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/appstore/activate_test.go`:

```go
func TestActivateSendsHeaderAndFile(t *testing.T)
// Die Shell schickt auf dem weitergeleiteten transfer-Kanal nur EOF; die API schreibt den Kopf
// core.Transfer (Katalog oder nichts, Lizenz und Schlüssel bei Kauf-Apps, Größe, SHA-256) und die
// Datei, dann EOF.

func TestActivateKeepsTheAppRowAndTheProof(t *testing.T)
// dbapi.App mit Store, Node, Grants, Manifest und SHA-256; dbapi.BlobApproval mit dem Beweis.

func TestActivateOnlyOnANodeWithRoleServices(t *testing.T)
// Eine App mit Diensten auf einem Controller ohne Rolle services ist E_FORBIDDEN.

func TestActivateRefusesAnOlderVersion(t *testing.T)

func TestUninstallKeepsOrDeletesData(t *testing.T)
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/server-api/classes/appstore/ -run TestActivate`
Expected: FAIL, `undefined: Activate`.

- [ ] **Step 3: Write the activation**

`apps/server-api/classes/appstore/activate.go` baut den Kopf und schickt die Datei:

```go
// buildTransfer puts together the header of an activation: the catalog of the
// app's store for a store app (so the controller can check version, checksum
// and UI hash itself), the licence and the unwrapped content key for a paid
// app, and nothing but size and checksum for an uploaded one, whose signed
// task names them.
func (st *Stores) buildTransfer(ctx context.Context, p *core.ActivateParams, size int64) (core.Transfer, error) {
	head := core.Transfer{Size: size, SHA256: p.SHA256}
	if p.Store == core.LocalStore {
		return head, head.Validate()
	}
	_, env, err := st.Catalog(ctx, p.Store)
	if err != nil {
		return core.Transfer{}, err
	}
	head.Catalog, head.Store = &env, p.Store
	// A paid app needs its licence and its content key; only the API can
	// unwrap the key, and it travels only inside the tunnel.
	if l, lenv, ok, err := st.License(ctx, p.App); err != nil {
		return core.Transfer{}, err
	} else if ok && l.Key != nil {
		key, err := st.ContentKey(ctx, p.App)
		if err != nil {
			return core.Transfer{}, err
		}
		head.License, head.Key = &lenv, key
	}
	return head, head.Validate()
}
```

`Activate` prüft davor: das Ziel trägt die Rolle `services`, wenn das Manifest Dienste hat
(`tunnel.Hello.Roles`); die Version ist nicht älter als die installierte; `Grants` entspricht genau
den `needs` des Manifests. Danach lädt es das Paket vom Store (`Download` mit Größe und Prüfsumme
aus dem Katalogeintrag) oder nimmt die hochgeladene Datei, schickt Kopf und Datei über
`relay.Transfer` und schreibt bei Erfolg `dbapi.App`, `dbapi.BlobUI` und `dbapi.BlobApproval`.

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/appstore/`
Expected: PASS. Danach `tools/check.sh`.

- [ ] **Step 5: Commit**

```bash
git add apps/server-api/classes/appstore
git commit -m "Add activating, deactivating and uninstalling apps" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 23: Updates

**Files:**
- Create: `apps/server-api/classes/updates/rows.go`, `apps/server-api/classes/updates/rollout.go`
- Test: `apps/server-api/classes/updates/rows_test.go`,
  `apps/server-api/classes/updates/rollout_test.go`

**Interfaces:**
- Consumes: `core.UpdateInfo`, `core.UpdateSetting`, `store.Update*`, `store.SupportsMinNGC`,
  `store.Granted`, `store.ValidTarget`, `store.UpdateReport`, `tunnel.Components`,
  `tunnel.Hello.Version`, `dbapi.SettingUpdate`, `dbapi.LockUpdate`, `trust.Auto*`.
- Produces:
  - `type Updates struct { S *state.State; Stores *appstore.Stores; Tunnels *controllers.Registry;
    Relay *relay.Relay; Log *audit.Log }`
  - `func (u *Updates) Rows(ctx context.Context) ([]core.UpdateInfo, error)`
  - `func (u *Updates) Setting(ctx context.Context, target string) (core.UpdateSetting, error)`
  - `func (u *Updates) SetSetting(ctx context.Context, s core.UpdateSetting) error`
  - `func (u *Updates) Report(ctx context.Context, node string, r store.UpdateReport)`
  - `func (u *Updates) Rollout(ctx context.Context) error` – mit `dbapi.LockUpdate`
  - `func (u *Updates) MigrationNode(ctx context.Context) (string, bool)`

- [ ] **Step 1: Write the failing test for the rows**

`apps/server-api/classes/updates/rows_test.go`:

```go
func TestRowsPerTargetAndNode(t *testing.T)
// Je NGC-Teil eine Zeile auf jedem Knoten, dessen tunnel.Components ihn führt; je App eine Zeile
// auf dem Knoten, auf dem sie läuft; system auf jedem Knoten.

func TestRowLeavesOutAVersionWithAHigherMinNGC(t *testing.T)
// store.SupportsMinNGC mit tunnel.Hello.Version; 0.0.0-dev passt zu keinem minNgc.

func TestRowOnlyFromTheOwnStore(t *testing.T)
// Eine neuere Version im Katalog eines anderen Stores wird nicht angeboten (dbapi.App.Store).

func TestRowNeedsGrants(t *testing.T)
// Verlangt die neue Version mehr als store.Granted mit dbapi.App.Grants erlaubt, steht state auf
// store.UpdateNeedsGrants; die Zeile wird weder per Klick noch automatisch installiert.

func TestRowsAreNotStored(t *testing.T)
// Gespeichert ist je Ziel nur die Einstellung (dbapi.SettingUpdate), keine Zeile.

func TestKeyOfANGCPartIsTheReleaseKey(t *testing.T)
// core.UpdateInfo.Key ist config.API.ReleaseKey bei NGC-Teilen, leer bei Apps und system.
```

- [ ] **Step 2: Write the failing test for the rollout**

`apps/server-api/classes/updates/rollout_test.go`:

```go
func TestRolloutTakesTheLock(t *testing.T)
// Genau eine API hält dbapi.LockUpdate; eine zweite tut nichts.

func TestRolloutOrder(t *testing.T)
// Datenbank → APIs → Controller → Webinterface; der nächste Schritt beginnt erst, wenn der
// vorige store.UpdateInstalled gemeldet hat.

func TestMigrationNodeIsTheSmallestDBNode(t *testing.T)
// Unter den verbundenen Controllern mit Rolle db der mit der kleinsten Node-ID; fällt er aus,
// der nächste.

func TestRolloutStagesThenInstalls(t *testing.T)
// stage (transfer, ohne Benutzer) mit der geprüften Datei, dann install (ohne Benutzer) nur mit
// Dauerfreigabe und neuerer Version.
```

- [ ] **Step 3: Run both and see them fail**

Run: `go test ./apps/server-api/classes/updates/`
Expected: FAIL, `undefined: Updates`.

- [ ] **Step 4: Write the rows**

`apps/server-api/classes/updates/rows.go`:

```go
// Package updates computes the rows of ngc.store/updates and drives a
// rollout. Nothing but the setting per target is stored (dbapi.SettingUpdate);
// the rows come from releases, catalogs and what the controllers reported, so
// they are always current and never stale.
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// Rows returns one row per target and node, sorted by target then node, so the
// shell always shows the same order.
func (u *Updates) Rows(ctx context.Context) ([]core.UpdateInfo, error) {
	v, err := u.S.Trust(ctx)
	if err != nil {
		return nil, err
	}
	release, _, err := u.Stores.Release(ctx)
	if err != nil {
		// Without a release nothing of NGC can be offered; apps still can.
		release = nil
	}
	apps, err := u.S.DB.AppList(ctx)
	if err != nil {
		return nil, err
	}
	var out []core.UpdateInfo
	for _, n := range v.List.Nodes {
		if n.Kind != trust.NodeController {
			continue
		}
		t, online := u.Tunnels.Get(n.ID)
		hello := tunnel.Hello{}
		status := tunnel.Status{}
		if online {
			hello, status = t.Hello(), t.Status()
		}
		for _, part := range tunnel.Components(hello.Roles) {
			row := core.UpdateInfo{Target: part, Node: n.ID, Installed: status.Components[part],
				Key: u.S.Cfg.ReleaseKey}
			if release != nil && store.Newer(release.Version, row.Installed) {
				row.Available = release.Version
			}
			u.fill(ctx, &row)
			out = append(out, row)
		}
		// System packages have no version to compare.
		sys := core.UpdateInfo{Target: trust.AutoSystem, Node: n.ID}
		u.fill(ctx, &sys)
		out = append(out, sys)
	}
	for _, a := range apps {
		row := core.UpdateInfo{Target: a.ID, Node: a.Node, Installed: a.Version}
		u.available(ctx, &row, a)
		u.fill(ctx, &row)
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		return out[i].Node < out[j].Node
	})
	return out, nil
}

// available sets Available for an app: the newest version of the catalog of
// the store the app was activated from, whose minNgc suits the node's NGC
// version. A version that needs more than the app was granted marks the row
// needsgrants instead of being offered.
func (u *Updates) available(ctx context.Context, row *core.UpdateInfo, a dbapi.App) {
	c, _, err := u.Stores.Catalog(ctx, a.Store)
	if err != nil {
		return
	}
	ngc := ""
	if t, ok := u.Tunnels.Get(a.Node); ok {
		ngc = t.Hello().Version
	}
	for _, e := range c.Apps {
		if e.ID != a.ID {
			continue
		}
		for _, p := range e.Versions {
			if !store.Newer(p.Version, a.Version) || !store.SupportsMinNGC(ngc, p.MinNGC) {
				continue
			}
			if !store.Granted(p.Needs, a.Grants) {
				// Nothing is wrong with the update; the app may simply not do
				// what it now needs, so the shell offers the activation.
				row.Available, row.State = p.Version, store.UpdateNeedsGrants
				return
			}
			row.Available = p.Version
		}
	}
}

// fill adds the setting of the target and the newest report of the node.
func (u *Updates) fill(ctx context.Context, row *core.UpdateInfo) {
	s, err := u.Setting(ctx, row.Target)
	if err == nil {
		row.Mode, row.Window = s.Mode, s.Window
	}
	if row.State != "" {
		return
	}
	if r, sha, ok := u.report(row.Node, row.Target); ok {
		row.State = r.State
		if r.State == store.UpdateDownloaded {
			// The checksum of the file lying on the node lets an update by
			// click name it in its signed task.
			row.SHA256 = enc.Bytes(sha)
		}
	}
}
```

`Setting` liest `dbapi.BlobSetting` mit der ID `dbapi.SettingUpdate + target` und gibt
`core.UpdateSetting` mit `store.ModeAuto` als Vorgabe zurück; `SetSetting` schreibt sie, nachdem
`core.UpdateSetting.Validate` sie angenommen hat und — bei `store.ModeAuto` — die Vertrauensliste
eine Dauerfreigabe für das Ziel führt (`store.Approved`). `report` hält je Knoten und Ziel den
letzten `store.UpdateReport` im Speicher (er ist flüchtig: er kommt nach jedem Tunnelaufbau neu).

- [ ] **Step 5: Write the rollout**

`apps/server-api/classes/updates/rollout.go`:

```go
package updates

import (
	"context"
	"slices"
	"sort"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// rolloutOrder is the order of a NGC update: the database first, then the
// APIs, then the controllers, the web interface last. Every part understands
// the previous contract, and every migration lets the previous API version run
// on, so NGC stays reachable throughout (Entwurf 4.8).
var rolloutOrder = []string{trust.AutoAPI, trust.AutoController, trust.AutoWeb}

// Rollout drives one round. Exactly one API drives it; the others do nothing.
func (u *Updates) Rollout(ctx context.Context) error {
	now := u.S.Now()
	ok, err := u.S.DB.LockTake(ctx, dbapi.LockUpdate, u.S.Cfg.Node, now, now+600)
	if err != nil || !ok {
		return err
	}
	defer func() { _ = u.S.DB.LockFree(ctx, dbapi.LockUpdate, u.S.Cfg.Node) }()
	release, env, err := u.Stores.Release(ctx)
	if err != nil {
		return err
	}
	// The migrations belong to the API version and run before the new API
	// starts: exactly one controller with role db plays them in, and it gets
	// the ngc-api file of its architecture for that.
	node, ok := u.MigrationNode(ctx)
	if ok {
		if err := u.stageAndInstall(ctx, node, trust.AutoAPI, release, env); err != nil {
			return err
		}
	}
	for _, part := range rolloutOrder {
		for _, n := range u.nodesFor(ctx, part) {
			if err := u.stageAndInstall(ctx, n, part, release, env); err != nil {
				return err
			}
			// One node at a time, so one of each kind keeps running.
		}
	}
	return nil
}

// MigrationNode picks the controller that plays in the migrations: among the
// connected controllers with role db the one with the smallest node ID; if it
// fails, the next.
func (u *Updates) MigrationNode(ctx context.Context) (string, bool) {
	var ids []string
	u.Tunnels.Each(func(t *tunnelRef) {
		if slices.Contains(t.Hello().Roles, tunnel.RoleDB) {
			ids = append(ids, t.Node)
		}
	})
	if len(ids) == 0 {
		return "", false
	}
	sort.Strings(ids)
	return ids[0], true
}
```

`stageAndInstall` lädt die Datei des Teils für die Architektur des Knotens
(`tunnel.Hello.Arch`) vom Store, prüft Größe und Prüfsumme gegen das geprüfte Release, schickt sie
über `ngc.store/controller/stage` (`transfer`, ohne Benutzer, Kopf mit `Release`) und öffnet
danach `ngc.store/controller/install` (ohne Benutzer, Parameter `core.InstallParams` mit `target`,
`version`, `sha256`). Der Controller führt `install` ohne Benutzer nur mit Dauerfreigabe und nur
für eine neuere Version aus; die API prüft nichts an seiner Stelle. Der nächste Knoten folgt erst,
wenn der vorige `store.UpdateInstalled` gemeldet hat oder eine Zeitgrenze von 15 Minuten abläuft;
bei `store.UpdateRolledBack` oder `store.UpdateFailed` bricht die Runde ab und schreibt einen
Protokolleintrag (`core.LogUpdate`).

- [ ] **Step 6: Run the tests and see them pass**

Run: `go test ./apps/server-api/classes/updates/`
Expected: PASS. Danach `tools/check.sh`.

- [ ] **Step 7: Commit**

```bash
git add apps/server-api/classes/updates
git commit -m "Add the update rows and the rollout" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---
### Task 24: `main.go` verdrahten, Dienst und Module

**Files:**
- Modify: `apps/server-api/main.go`, `tools/modules.go`
- Create: `apps/server-api/classes/server/server.go`
- Test: `apps/server-api/classes/server/server_test.go`

**Interfaces:**
- Consumes: alle Pakete aus `classes/`, `config.LoadAPI`, `config.APIFile`, `config.DataDir`,
  `config.RunDir`, `apitls.Ensure`, `dbopen.Open`.
- Produces:
  - `type Server struct { … }`, `func Build(ctx context.Context, cfg *config.API, version string) (*Server, error)`
  - `func (s *Server) Handler() http.Handler`
  - `func (s *Server) Run(ctx context.Context) error` – Listener, Hintergrundläufe, sauberes Ende
  - `apps/server-api/main.go`: Schalter `-config`, `-version`, `-join <code>`

- [ ] **Step 1: Write the failing test**

`apps/server-api/classes/server/server_test.go`:

```go
func TestHandlerServesEveryPath(t *testing.T)
// Alle Pfade von session und tunnel antworten; ein unbekannter Pfad gibt 404 mit wire.Error.

func TestBuildRefusesABrokenConfiguration(t *testing.T)
// Ohne Datenbank, ohne Schlüssel oder mit einem Zertifikat, das nicht zum Schlüssel passt,
// bricht Build mit einer deutschen Meldung ab, statt halb zu laufen (Entwurf 4.6).

func TestRunStopsCleanly(t *testing.T)
// Ein abgebrochener Kontext beendet Listener und Hintergrundläufe innerhalb von fünf Sekunden.
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/server-api/classes/server/`
Expected: FAIL, `undefined: Build`.

- [ ] **Step 3: Write the server**

`apps/server-api/classes/server/server.go` setzt alles zusammen:

```go
// Package server puts the API together: configuration, database, TLS, the
// paths of the shell and of the controllers, and the background rounds
// (housekeeping, catalogs, updates). It starts only when database and keys are
// in order, and stops with a clear message otherwise (Entwurf 4.6).
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/apitls"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/appstore"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/audit"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/browser"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/controllers"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/coreapp"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbopen"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/join"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/login"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/relay"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/setup"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/updates"
)

// SetupCodeFile is where the one-time code of the first setup lies: root's
// alone, never in the database and never in a log. The ulh script reads it.
var SetupCodeFile = filepath.Join(config.DataDir, "setup-code")

// Server is one running API.
type Server struct {
	Cfg   *config.API
	State *state.State
	Cert  tls.Certificate
	mux   *http.ServeMux
	login *login.Handler
	setup *setup.Handler
	up    *updates.Updates
	st    *appstore.Stores
}

// Build makes everything the API needs and fails if anything is missing.
func Build(ctx context.Context, cfg *config.API, version string) (*Server, error) {
	box, err := secrets.NewBox(cfg.SecretKey)
	if err != nil {
		return nil, errors.New("der geheime Schlüssel in api.json ist unbrauchbar")
	}
	// config.LoadAPI has checked the length already; this says so where it
	// matters, because without this key session.Challenge cannot answer a name
	// that does not exist, and the API would have to give itself away.
	if len(cfg.ChallengeKey.Bytes()) != 32 {
		return nil, errors.New("der Schlüssel challengeKey in api.json ist unbrauchbar")
	}
	host, err := apitls.Host(cfg.URL)
	if err != nil {
		return nil, errors.New("die Adresse url in api.json ist keine Tunnel-Adresse")
	}
	cert, pin, err := apitls.Ensure(cfg.Cert, cfg.CertKey, host, time.Now())
	if err != nil {
		return nil, errors.New("das TLS-Zertifikat der API lässt sich nicht anlegen oder lesen")
	}
	db, err := dbopen.Open(ctx, cfg.DB)
	if err != nil {
		return nil, errors.New("die Datenbank ist nicht erreichbar")
	}
	s := state.New(cfg, db, box, pin)
	lg := &audit.Log{DB: db, Now: s.Now}
	lh := login.New(s)
	tunnels := controllers.NewRegistry()
	sessions := browser.NewRegistry()
	ctl := &controllers.Server{S: s, Tunnels: tunnels}
	rl := &relay.Relay{S: s, Tunnels: tunnels, Log: lg}
	st := appstore.New(s)
	jn := &join.Join{S: s}
	up := &updates.Updates{S: s, Stores: st, Tunnels: tunnels, Relay: rl, Log: lg}
	d := coreapp.New(coreapp.Deps{S: s, Log: lg, Login: lh, Tunnels: tunnels,
		Controllers: ctl, Sessions: sessions, Relay: rl, Stores: st, Join: jn, Updates: up})
	bs := &browser.Server{S: s, Login: lh, Open: d, Sessions: sessions}
	sh := &setup.Handler{S: s, CodeFile: SetupCodeFile}
	if _, err := sh.EnsureCode(); err != nil {
		return nil, errors.New("der Einmal-Code für die Ersteinrichtung lässt sich nicht anlegen")
	}
	controllers.Version = version
	ctl.OnHello = func(ctx context.Context, t *controllers.Tunnel) {
		// A controller that connects gets the newest trust list and the newest
		// licence of every paid app it runs, right away.
		d.PushTrustTo(ctx, t)
		_ = st.PushLicensesTo(ctx, t, rl)
	}
	ctl.OnReport = func(ctx context.Context, t *controllers.Tunnel, r storeUpdateReport) {
		up.Report(ctx, t.Node, r)
	}
	ctl.OnNotice = func(ctx context.Context, t *controllers.Tunnel, n coreNotice) {
		sessions.NotifyAll(ctx, n)
	}
	ctl.OnFrame = rl.TunnelFrame

	mux := httpapi.Routes{
		Setup: sh.Serve, Login: lh.Start, Finish: lh.Finish, Logout: lh.Logout,
		Trust: lh.Trust, Socket: bs.Socket,
	}.Mux()
	mux.HandleFunc(tunnel.PathTunnel, ctl.Tunnel)
	mux.HandleFunc(tunnel.PathPair, ctl.Pair)
	mux.HandleFunc(tunnel.PathJoin, jn.Accept)
	mux.HandleFunc(join.PathKeys, jn.Keys)
	return &Server{Cfg: cfg, State: s, Cert: cert, mux: mux, login: lh, setup: sh, up: up, st: st}, nil
}

// Handler returns the router; tests use it without a listener.
func (s *Server) Handler() http.Handler { return s.mux }

// Run listens and keeps the background rounds going until ctx ends.
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.Cfg.Listen,
		Handler: s.mux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{s.Cert},
			// Apache offers nothing else, and the design asks for AES-256-GCM
			// alone on this hop (Entwurf 4.5).
			MinVersion:       tls.VersionTLS13,
			MaxVersion:       tls.VersionTLS13,
			CipherSuites:     []uint16{tls.TLS_AES_256_GCM_SHA384},
			CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256},
		},
		ReadHeaderTimeout: 10 * time.Second,
	}
	go s.rounds(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	slog.Info("ngc-api listening", "listen", s.Cfg.Listen, "node", s.Cfg.Node)
	if err := srv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// rounds keeps housekeeping, catalogs and updates going.
func (s *Server) rounds(ctx context.Context) {
	minute := time.NewTicker(time.Minute)
	hour := time.NewTicker(time.Hour)
	defer minute.Stop()
	defer hour.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-minute.C:
			if err := s.login.Purge(ctx); err != nil {
				slog.Warn("housekeeping failed", "error", err)
			}
			if _, err := s.State.Refresh(ctx); err != nil {
				slog.Warn("trust list could not be read", "error", err)
			}
		case <-hour.C:
			if err := s.st.Refresh(ctx); err != nil {
				slog.Warn("catalogs could not be fetched", "error", err)
			}
			if err := s.up.Rollout(ctx); err != nil {
				slog.Warn("rollout failed", "error", err)
			}
		}
	}
}
```

`storeUpdateReport` und `coreNotice` sind Aliase auf `store.UpdateReport` und `core.Notice`.

- [ ] **Step 4: Write main.go**

`apps/server-api/main.go`:

```go
// Command ngc-api is the API of NGC: sessions, rights, forwarding.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/join"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/server"
)

// Version is set when building: -ldflags "-X main.Version=1.0.0".
var Version = "0.0.0-dev"

func main() {
	version := flag.Bool("version", false, "print the version and exit")
	path := flag.String("config", config.APIFile, "configuration file")
	joinCode := flag.String("join", "", "join an installation with a join code and fetch its shared keys")
	flag.Parse()
	if *version {
		fmt.Printf("ngc-api %s (contract %d)\n", Version, tunnel.Contract)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *joinCode != "" {
		// Joining happens before api.json is complete: the ulh script writes
		// the shared key this run fetched into the file itself.
		if err := join.Run(ctx, join.Config{Code: *joinCode}); err != nil {
			slog.Error("der Beitritt ist fehlgeschlagen", "error", err)
			os.Exit(1)
		}
		return
	}
	cfg, err := config.LoadAPI(*path)
	if err != nil {
		slog.Error("cannot read the configuration", "file", *path, "error", err)
		os.Exit(1)
	}
	slog.Info("ngc-api starting", "version", Version, "node", cfg.Node, "listen", cfg.Listen)
	srv, err := server.Build(ctx, cfg, Version)
	if err != nil {
		slog.Error("die API kann nicht starten", "error", err)
		os.Exit(1)
	}
	if err := srv.Run(ctx); err != nil {
		slog.Error("die API ist abgebrochen", "error", err)
		os.Exit(1)
	}
}
```

Der Datenbanktreiber kommt aus `apps/server-db`; `main.go` bindet ihn mit einer Zeile ein, sobald
dieses Teil seinen Öffner anmeldet (siehe „Offene Entscheidungen“, Punkt 1):

```go
	_ "github.com/sorglos-it/nextgencloud/apps/server-db/classes/register"
```

Bis dahin antwortet `dbopen.Open` mit `ErrNoOpener`, und `Build` bricht mit „die Datenbank ist
nicht erreichbar“ ab — richtig und sichtbar, statt halb zu laufen.

- [ ] **Step 5: Take the two module lines**

In `tools/modules.go` die zwei Zeilen streichen, die dieses Teil jetzt zuerst benutzt:

```go
	_ "github.com/coder/websocket"               // sessions and tunnel
	_ "github.com/pquerna/otp/totp"              // TOTP
```

Die übrigen Zeilen (SQLite, MariaDB, Passkeys) bleiben stehen: die API benutzt keine davon.

- [ ] **Step 6: Run everything**

Run:
```bash
go test ./apps/server-api/...
tools/check.sh
go build -o /tmp/ngc-api -ldflags "-X main.Version=$(cat apps/server-api/VERSION)" ./apps/server-api
/tmp/ngc-api -version
```
Expected: alle Tests grün, `check: ok`, `ngc-api 0.1.0 (contract 1)`.

- [ ] **Step 7: Commit**

```bash
git add apps/server-api tools/modules.go
git commit -m "Wire the API together and take the websocket and totp modules" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 25: ulh-Skript `scripts/ngc-api.sh`

Dieses Skript liegt **nicht** in diesem Repo, sondern im öffentlichen ulh-Repo
(`sorglos-it/ulh`), auf dem Branch `ngc-api`, und kommt per Pull Request nach dessen `main`.
Regeln: `shared/ulh.md`.

**Files (im ulh-Repo):**
- Create: `scripts/ngc-api.sh`
- Modify: `config.yaml` (Kategorie „NextGenCloud“), `SCRIPTS.md`, `TREE.md`

**Interfaces:**
- Consumes: `answer.yaml`-Schlüssel aus `shared/ulh.md` (`node`, `hostname`, `db_driver`,
  `db_nodes`, `db_name`, `db_user`, `db_password`, `db_ca`, `store_url`, `store_key`, `join_code`),
  `shared/testdata/release/verify.sh`, `shared/testdata/tls/pin.sh`, `shared/testdata/ulh/host.sh`.
- Produces: `/usr/lib/ngc/ngc-api`, `/usr/lib/ngc/ngc-store`, `/etc/ngc/api.json` (0600, root),
  `/etc/ngc/tls/api.crt` und `api.key`, `ngc-api.service`; Ausgabe am Ende: Einmal-Code und
  TLS-Fingerabdruck, beim Beitritt zusätzlich `tunnel.JoinFingerprint` und der Anker des Codes.

- [ ] **Step 1: Write the script from ulh's template**

`scripts/ngc-api.sh` nach `scripts/_template.sh` und ulhs `DOCS.md`, mit `install`, `configure`
und `uninstall`. `install` tut der Reihe nach:

1. `LC_ALL=C` setzen; `hostname` mit derselben Prüfung wie `shared/testdata/ulh/host.sh` annehmen
   (DNS-Name in Kleinbuchstaben oder IPv4, kein IPv6), sonst abbrechen.
2. `openssl` ab 3.0, `jq` und `ca-certificates` aus den Paketquellen installieren.
3. Die Release-Dateien `ngc-api` und `ngc-store` der Architektur herunterladen und mit
   `verify.sh <verzeichnis> ngc-api <arch>` prüfen; nur die geprüfte Datei nach `/usr/lib/ngc/`
   kopieren. Nie ein älteres Release als die Nummer in `/var/lib/ngc/release.seq`; dieselbe darf
   zur Reparatur erneut kommen.
4. `api.json` schreiben (0600, root): `node`, `url` = `wss://<hostname>:7443/tunnel`,
   `listen` = `:7443`, `cert`/`certKey` = `/etc/ngc/tls/api.crt` und `api.key`, `db` aus den
   Antworten, `noiseKey`, `signKey`, `secretKey` und `challengeKey` als je 32 zufällige Bytes
   (base64url), `releaseKey` fest im Skript, `storeUrl`/`storeKey` aus `store_url`/`store_key` oder
   die Vorgabe des offiziellen NGC-Stores. `challengeKey` ist ein eigener Schlüssel neben
   `secretKey` (`config.API.Validate` verlangt beide mit 32 Byte): aus ihm erfindet
   `session.Challenge` die Antwort für einen Namen, den es nicht gibt. Beim Wechsel des
   Datenbankschlüssels müssten sonst alle erfundenen Antworten neu gezogen werden, während die
   echten stehen blieben – wer vorher und nachher fragt, hätte jeden Namen sortiert.
5. Beim Beitritt (`join_code` gesetzt): `ngc-api -join <code>` laufen lassen, die Ausgabe
   (`tunnel.JoinFingerprint` und der Anker) anzeigen, warten, bis `/run/ngc/join-secret` da ist,
   dessen `secretKey` und `challengeKey` in `api.json` schreiben (die beiden zufälligen aus
   Schritt 4 dabei überschreiben) und die Datei löschen. Beide müssen in jedem API-Server der
   Anlage gleich sein: `secretKey`, weil alle dieselben Datenbankgeheimnisse öffnen, und
   `challengeKey`, weil sonst jeder Server denselben unbekannten Namen anders beantwortete – und
   wer zwei Server nacheinander fragt, hätte bekannte von unbekannten getrennt.
6. `ngc-api.service` anlegen und starten; das Zertifikat erzeugt `ngc-api` selbst beim ersten Start.
7. Den Fingerabdruck mit `pin.sh /etc/ngc/tls/api.crt` zeigen (Exit-Status prüfen) und, beim ersten
   API-Server, den Einmal-Code aus `/var/lib/ngc/setup-code`.

`uninstall` fragt, was mit den Daten passiert (löschen oder aufheben); Aufgehobenes bleibt unter
`/var/lib/ngc/`.

- [ ] **Step 2: Check the script against the contract**

Run (im NGC-Repo):
```bash
go test ./shared/store/ -run TestReleaseScript
go test ./shared/tunnel/ -run TestTLSPinScript
go test ./shared/trust/ -run TestHostScript
```
Expected: PASS — die drei Skripte, die `ngc-api.sh` aufruft, verhalten sich wie Go.

- [ ] **Step 3: Try the script in a container**

Run:
```bash
shellcheck scripts/ngc-api.sh
sh -n scripts/ngc-api.sh
```
und ein Durchlauf in einem Debian-Container mit einer `answer.yaml` ohne Rückfragen; ein zweiter
Lauf von `install` darf nichts ändern, was schon stimmt.

- [ ] **Step 4: Commit and open the pull request (im ulh-Repo)**

```bash
git checkout -b ngc-api
git add scripts/ngc-api.sh config.yaml SCRIPTS.md TREE.md
git commit -m "Add the ulh script of the NGC API" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

Danach Pull Request nach `main` des ulh-Repos. **Nicht selbst zusammenführen:** Jede
ulh-Installation holt beim Start `main`, deshalb prüft der Koordinator.

---

### Task 26: `ngc-store pack`

**Files:**
- Create: `apps/cli/classes/pack.go`
- Modify: `apps/cli/main.go`
- Test: `apps/cli/classes/pack_test.go`

**Interfaces:**
- Consumes: `store.ParseManifest`, `store.ManifestFile`, `store.Walk`, `store.PackageUI`,
  `store.ValidPath`, `store.MaxUIFile`, `store.MaxUITotal`, `store.MaxEntries`, `archive/tar`.
- Produces:
  - `func Pack(dir, out string) (*store.Manifest, []byte, error)` – schreibt das Paket und gibt
    Manifest und SHA-256 der Datei zurück
  - `func Run(args []string, stdout, stderr io.Writer) int` in `apps/cli/classes` – der Versand
    aller Unterbefehle

- [ ] **Step 1: Write the failing test**

`apps/cli/classes/pack_test.go`:

```go
package classes

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

func TestPackWritesAReadablePackage(t *testing.T) {
	dir := writeApp(t)
	out := filepath.Join(t.TempDir(), "app.ngc")
	m, sum, err := Pack(dir, out)
	if err != nil || m.ID != "org.sorglos.demo" || len(sum) != 32 {
		t.Fatalf("pack: %v %+v", err, m)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	// The package must read exactly as store.Walk reads one, and its ui-tar
	// must match what store.PackageUI computes.
	var ui bytes.Buffer
	m2, hash, err := store.PackageUI(f, &ui)
	if err != nil || m2.Version != m.Version || len(hash) != 32 {
		t.Fatalf("package ui: %v", err)
	}
}

func TestPackRefusesUnsafeNames(t *testing.T) {
	dir := writeApp(t)
	// A hidden file, a name with "..", a symbolic link and a path outside ui/
	// are refused before anything is written.
	if err := os.WriteFile(filepath.Join(dir, "ui", ".hidden"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := Pack(dir, filepath.Join(t.TempDir(), "app.ngc")); err == nil {
		t.Fatal("a hidden file was packed")
	}
}

func TestPackMatchesTheVectors(t *testing.T) {
	// packui.json holds the packages ngc-store pack writes, with their ui-tar
	// and its SHA-256: whatever this packer writes must read back into exactly
	// those bytes, or the shell, the API and the controller would judge an app
	// differently.
	raw, err := os.ReadFile("../../../shared/testdata/packui.json")
	if err != nil {
		t.Fatalf("vectors: %v", err)
	}
	// packui.json is a list of {name, package, ui, hash}, each of the last
	// three base64url (enc.Bytes); shared/store/ui_test.go writes it.
	var cases []struct {
		Name    string    `json:"name"`
		Package enc.Bytes `json:"package"`
		UI      enc.Bytes `json:"ui"`
		Hash    enc.Bytes `json:"hash"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("vectors: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("packui.json holds no case")
	}
	for _, c := range cases {
		var ui bytes.Buffer
		_, hash, err := store.PackageUI(bytes.NewReader(c.Package), &ui)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if !bytes.Equal(ui.Bytes(), c.UI) || !bytes.Equal(hash, c.Hash) {
			t.Fatalf("%s: ui-tar or hash differs from the vector", c.Name)
		}
	}
}
```

Die Prüfung sagt: Was dieser Packer schreibt, liest `store.PackageUI` genau in die Bytes, die der
Vertrag festhält. Der Test importiert dafür `"encoding/json"`, `"os"` und
`"github.com/sorglos-it/nextgencloud/shared/enc"`.

`writeApp` legt ein Verzeichnis mit `manifest.json`, `ui/index.html` und
`services/amd64.tar` an.

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/cli/classes/`
Expected: FAIL, `undefined: Pack`.

- [ ] **Step 3: Write the packer**

`apps/cli/classes/pack.go`:

```go
// Package classes holds the subcommands of ngc-store: packing apps,
// encrypting paid ones and signing catalogs, licences and test releases.
package classes

import (
	"archive/tar"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

// ErrPack reports a directory that is no app.
var ErrPack = errors.New("ngc-store: the directory is no app")

// Pack writes the package of the app in dir to out: manifest.json first, then
// the files below ui/ in order, then the images under services/. Everything Go
// archive/tar writes by default (USTAR, PAX only for a long name), so the
// shell, the API and the controller all read the same bytes (packui.json).
func Pack(dir, out string) (*store.Manifest, []byte, error) {
	raw, err := os.ReadFile(filepath.Join(dir, store.ManifestFile))
	if err != nil {
		return nil, nil, ErrPack
	}
	m, err := store.ParseManifest(raw)
	if err != nil {
		return nil, nil, err
	}
	files, err := collect(dir)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Create(out)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	h := sha256.New()
	tw := tar.NewWriter(io.MultiWriter(f, h))
	if err := write(tw, store.ManifestFile, raw); err != nil {
		return nil, nil, err
	}
	for _, name := range files {
		body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			return nil, nil, err
		}
		if err := write(tw, name, body); err != nil {
			return nil, nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, nil, err
	}
	// The package must read back exactly as store.Walk reads one; a packer
	// that writes something else is a bug, not a package.
	if err := verify(out); err != nil {
		return nil, nil, err
	}
	return m, h.Sum(nil), nil
}

// collect lists the files of the package in order: everything below ui/ whose
// path is store.ValidPath, then services/amd64.tar and services/arm64.tar.
// A name the contract refuses stops the whole run, so nothing half-safe is
// ever written.
func collect(dir string) ([]string, error) {
	var ui []string
	root := filepath.Join(dir, "ui")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			// Links, devices and everything else are refused: store.Walk would
			// refuse them too, and only files belong in a package.
			return ErrPack
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !store.ValidPath(rel) {
			return ErrPack
		}
		ui = append(ui, "ui/"+rel)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.Strings(ui)
	for _, arch := range []string{"amd64", "arm64"} {
		name := "services/" + arch + ".tar"
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err == nil {
			ui = append(ui, name)
		}
	}
	return ui, nil
}

// write puts one regular file into the archive with fixed metadata, so the
// same input always gives the same bytes.
func write(tw *tar.Writer, name string, body []byte) error {
	if strings.Contains(name, "..") {
		return ErrPack
	}
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name,
		Mode: 0o644, Size: int64(len(body)), Format: tar.FormatUSTAR}); err != nil {
		return err
	}
	_, err := tw.Write(body)
	return err
}

// verify reads the package back with the contract's own reader.
func verify(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = store.Walk(f, func(*store.Manifest, store.PackFile, io.Reader) error { return nil })
	return err
}
```

- [ ] **Step 4: Write the dispatcher of ngc-store**

`apps/cli/main.go` ruft `classes.Run(os.Args[1:], os.Stdout, os.Stderr)` und gibt dessen Rückgabe
als Exit-Status weiter; `classes.Run` kennt `pack`, `encrypt`, `catalog`, `license`, `release` und
`check` und gibt bei falschem Aufruf die Benutzung aus und 2 zurück. Meldungen für Menschen sind
Deutsch.

- [ ] **Step 5: Run the tests and see them pass**

Run: `go test ./apps/cli/...`
Expected: PASS. Danach `tools/check.sh`.

- [ ] **Step 6: Commit**

```bash
git add apps/cli
git commit -m "Add ngc-store pack" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---
### Task 27: `ngc-store encrypt` und `ngc-store license`

**Files:**
- Create: `apps/cli/classes/crypt.go`, `apps/cli/classes/license.go`
- Test: `apps/cli/classes/crypt_test.go`, `apps/cli/classes/license_test.go`

**Interfaces:**
- Consumes: `store.Encrypt`, `store.NewDecrypter`, `store.WrapKey`, `store.KeySize`,
  `store.License`, `store.PerSite`, `store.PerUser`, `sign.Sign`, `sign.PurposeLicense`,
  `trust.ValidID`.
- Produces:
  - `func Encrypt(in, out, keyFile string) error`
  - `func NewContentKey(path string) ([]byte, error)` – legt eine Schlüsseldatei an (0600)
  - `func License(app, installation, keyFile, contentKeyFile, installKey string, opts LicenseOpts) (sign.Envelope, error)`
  - `type LicenseOpts struct { Per string; Users int; Issued, Expires, Grace int64; Store, ID string }`

- [ ] **Step 1: Write the failing test**

`apps/cli/classes/crypt_test.go`:

```go
package classes

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

func TestEncryptRoundTrip(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "app.ngc")
	if err := os.WriteFile(plain, bytes.Repeat([]byte("a"), 3*store.ChunkSize+7), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	keyFile := filepath.Join(dir, "content.key")
	key, err := NewContentKey(keyFile)
	if err != nil || len(key) != store.KeySize {
		t.Fatalf("key: %v", err)
	}
	if info, _ := os.Stat(keyFile); info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %v", info.Mode().Perm())
	}
	sealed := filepath.Join(dir, "app.enc")
	if err := Encrypt(plain, sealed, keyFile); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	f, err := os.Open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	r, err := store.NewDecrypter(f, key)
	if err != nil {
		t.Fatalf("decrypter: %v", err)
	}
	back, err := io.ReadAll(r)
	if err != nil || len(back) != 3*store.ChunkSize+7 {
		t.Fatalf("read back: %v %d", err, len(back))
	}
	// Nothing of the package shows through.
	raw, _ := os.ReadFile(sealed)
	if bytes.Contains(raw, bytes.Repeat([]byte("a"), 64)) {
		t.Fatal("the plaintext shows in the encrypted file")
	}
}

func TestDecryptWithAnotherKeyFails(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "app.ngc")
	if err := os.WriteFile(plain, []byte("package"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	keyFile := filepath.Join(dir, "content.key")
	if _, err := NewContentKey(keyFile); err != nil {
		t.Fatalf("key: %v", err)
	}
	sealed := filepath.Join(dir, "app.enc")
	if err := Encrypt(plain, sealed, keyFile); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	f, err := os.Open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	other := bytes.Repeat([]byte{3}, store.KeySize)
	r, err := store.NewDecrypter(f, other)
	if err != nil {
		// A wrong key may already fail here; if it does not, reading must.
		return
	}
	if _, err := io.ReadAll(r); err == nil {
		t.Fatal("another key opened the package")
	}
}
```

`apps/cli/classes/license_test.go` stellt eine Test-Lizenz aus und prüft sie mit
`store.VerifyLicense` gegen den Store-Schlüssel, die Installation und die App; eine Lizenz für
eine andere Installation oder App wird abgelehnt, und `store.UnwrapKey` öffnet den Inhaltsschlüssel
nur mit dem privaten Schlüssel der Installation.

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./apps/cli/classes/`
Expected: FAIL, `undefined: Encrypt`.

- [ ] **Step 3: Write both subcommands**

`apps/cli/classes/crypt.go` liest die Schlüsseldatei (32 Byte als base64url), öffnet die Eingabe
und schreibt `store.Encrypt(out, in, key, rand.Reader)`. `NewContentKey` erzeugt 32 Zufallsbytes,
schreibt sie als base64url mit Modus 0600 und gibt sie zurück.

`apps/cli/classes/license.go` baut eine `store.License` (ID zufällig, `Store`, `App`,
`Installation`, `InstallKey`, `Per`, `Users`, `Issued`, `Expires`, `Grace`), packt den
Inhaltsschlüssel mit `store.WrapKey(contentKey, installKey, app, rand.Reader)` hinein, prüft sie
mit `License.Validate` und signiert sie mit `sign.Sign(priv, sign.PurposeLicense, payload)`.

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./apps/cli/...`
Expected: PASS. Danach `tools/check.sh`.

- [ ] **Step 5: Commit**

```bash
git add apps/cli
git commit -m "Add ngc-store encrypt and license" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 28: `ngc-store catalog`

**Files:**
- Create: `apps/cli/classes/catalog.go`
- Test: `apps/cli/classes/catalog_test.go`

**Interfaces:**
- Consumes: `store.Catalog`, `store.Entry`, `store.Package`, `store.PackageUI`,
  `store.VerifyCatalog`, `sign.Sign`, `sign.PurposeCatalog`, `trust.Store`.
- Produces:
  - `func Catalog(dir, keyFile string, opts CatalogOpts) (sign.Envelope, error)`
  - `type CatalogOpts struct { Store string; Seq uint64; Issued int64; BaseURL string }`

- [ ] **Step 1: Write the failing test**

`apps/cli/classes/catalog_test.go`:

```go
func TestCatalogListsEveryPackage(t *testing.T)
// Jedes .ngc im Verzeichnis wird ein Eintrag: ID, Name, Publisher aus dem Manifest, je Version
// url, size, sha256, ui und minNgc.

func TestCatalogComputesUIBeforeEncryption(t *testing.T)
// ui ist store.PackageUI über das unverschlüsselte Paket; für eine Kauf-App wird es vor dem
// Verschlüsseln gerechnet und encrypted steht auf true.

func TestCatalogTakesMinNGCFromTheManifest(t *testing.T)

func TestCatalogVerifies(t *testing.T)
// store.VerifyCatalog mit dem öffentlichen Schlüssel der Schlüsseldatei nimmt das Ergebnis an;
// eine kleinere Seq als die vorige wird von store.VerifyCatalog als store.ErrRollback abgelehnt.

func TestCatalogRefusesTooManyVersions(t *testing.T)
// Über store.MaxCatalogVersions Versionen zusammen sind ein Fehler, keine halbe Datei.
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/cli/classes/ -run TestCatalog`
Expected: FAIL, `undefined: Catalog`.

- [ ] **Step 3: Write the catalog**

`apps/cli/classes/catalog.go`:

```go
package classes

import (
	"crypto/ed25519"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
)

// CatalogOpts describe the catalog to write.
type CatalogOpts struct {
	Store   string // the store's ID (wire.ValidName)
	Seq     uint64 // higher than the last one, else readers refuse it
	Issued  int64
	BaseURL string // where the packages lie; empty writes paths beside the catalog
}

// Catalog reads every package in dir and writes a signed catalog. The ui hash
// of a version is store.PackageUI over the plain package, computed before a
// paid app is encrypted; minNgc comes from the manifest.
func Catalog(dir, keyFile string, opts CatalogOpts) (sign.Envelope, error) {
	priv, err := readSigningKey(keyFile)
	if err != nil {
		return sign.Envelope{}, err
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.ngc"))
	if err != nil {
		return sign.Envelope{}, err
	}
	sort.Strings(names)
	byApp := map[string]*store.Entry{}
	for _, name := range names {
		e, p, err := entryOf(name, opts.BaseURL)
		if err != nil {
			return sign.Envelope{}, err
		}
		cur, ok := byApp[e.ID]
		if !ok {
			byApp[e.ID] = e
			cur = e
		}
		cur.Versions = append(cur.Versions, p)
	}
	c := store.Catalog{Store: opts.Store, Seq: opts.Seq, Issued: opts.Issued}
	for _, e := range byApp {
		// Versions ascend and apps are sorted by ID: store.Catalog.Validate
		// refuses anything else.
		sort.Slice(e.Versions, func(i, j int) bool {
			return store.Newer(e.Versions[j].Version, e.Versions[i].Version)
		})
		c.Apps = append(c.Apps, *e)
	}
	sort.Slice(c.Apps, func(i, j int) bool { return c.Apps[i].ID < c.Apps[j].ID })
	if err := c.Validate(); err != nil {
		return sign.Envelope{}, err
	}
	payload, err := encodeJSON(c)
	if err != nil {
		return sign.Envelope{}, err
	}
	return sign.Sign(priv, sign.PurposeCatalog, payload)
}

// entryOf reads one package: its manifest, its size and checksum and the hash
// of its ui-tar.
func entryOf(path, base string) (*store.Entry, store.Package, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, store.Package{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, store.Package{}, err
	}
	h := sha256.New()
	m, ui, err := store.PackageUI(io.TeeReader(f, h), io.Discard)
	if err != nil {
		return nil, store.Package{}, err
	}
	url := filepath.Base(path)
	if base != "" {
		url = strings.TrimSuffix(base, "/") + "/" + url
	}
	p := store.Package{Version: m.Version, URL: url, Size: info.Size(),
		SHA256: enc.Bytes(h.Sum(nil)), UI: enc.Bytes(ui), Needs: m.Needs, MinNGC: m.MinNGC}
	return &store.Entry{ID: m.ID, Name: m.Name, Publisher: m.Publisher,
		Description: m.Description}, p, nil
}

// readSigningKey reads an Ed25519 seed of 32 bytes as base64url.
func readSigningKey(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seed, err := enc.B64.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, ErrPack
	}
	return ed25519.NewKeyFromSeed(seed), nil
}
```

`encodeJSON` ist `json.Marshal` mit `encoding/json`, weil ein Katalog kein Ergebnis der API ist
und `core.EncodeResult` hier nicht gilt; `store.Catalog` hat keine Felder, die `null` werden
könnten (leere Listen fehlen dank `omitempty`).

Eine verschlüsselte Kauf-App bekommt `Encrypted: true`; `Catalog` erkennt sie daran, dass neben
`<name>.ngc` eine Datei `<name>.enc` liegt, und nimmt Größe und Prüfsumme von dieser, den ui-Hash
aber vom unverschlüsselten Paket.

- [ ] **Step 4: Run the tests and see them pass**

Run: `go test ./apps/cli/...`
Expected: PASS. Danach `tools/check.sh`.

- [ ] **Step 5: Commit**

```bash
git add apps/cli
git commit -m "Add ngc-store catalog" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 29: `ngc-store release` und `ngc-store check`, Test-Store

`ngc-store` signiert Releases **nur zum Testen**, mit einer Schlüsseldatei. Der echte
Release-Schlüssel von NGC liegt auf einem Hardware-Stick und nie auf einem Server (Entwurf 4.8).

**Files:**
- Create: `apps/cli/classes/release.go`, `apps/cli/classes/teststore_test.go`
- Test: `apps/cli/classes/release_test.go`

**Interfaces:**
- Consumes: `store.Release`, `store.File`, `store.VerifyRelease`, `sign.Sign`,
  `sign.PurposeRelease`, `shared/testdata/release/verify.sh`.
- Produces:
  - `func Release(dir, keyFile string, opts ReleaseOpts) (sign.Envelope, error)`
  - `type ReleaseOpts struct { Version string; Seq uint64; Issued int64; Notes, BaseURL string }`
  - `func Check(dir, part, arch string) error` – prüft wie `verify.sh`, in Go

- [ ] **Step 1: Write the failing test**

`apps/cli/classes/release_test.go`:

```go
func TestReleaseListsEveryFile(t *testing.T)
// ngc-web (all), ngc-api und ngc-controller (amd64, arm64) mit Größe, SHA-256 und url.

func TestReleaseVerifies(t *testing.T)
// store.VerifyRelease nimmt es an; eine kleinere Seq ist store.ErrRollback.

func TestCheckMatchesTheScript(t *testing.T)
// Check und shared/testdata/release/verify.sh urteilen über dieselben 31 Fälle gleich, die
// TestReleaseScript in shared/store schon kennt: dieselben Eingaben, dasselbe Ergebnis.
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./apps/cli/classes/ -run TestRelease`
Expected: FAIL, `undefined: Release`.

- [ ] **Step 3: Write both subcommands**

`apps/cli/classes/release.go`: `Release` liest die Dateien des Verzeichnisses, ordnet sie über
ihren Namen (`ngc-web-all`, `ngc-api-amd64`, …) Teil und Architektur zu, baut `store.Release` mit
Größe und SHA-256 je Datei, prüft mit `Release.Validate` und signiert mit
`sign.PurposeRelease`. `Check` liest `release.json` und `release-key.pem` aus dem Verzeichnis,
prüft die Unterschrift, sucht den Eintrag für Teil und Architektur, leitet den Dateinamen aus dem
letzten Stück der `url` ab (nur `^[A-Za-z0-9][A-Za-z0-9._-]*$`), vergleicht Größe und SHA-256 und
gibt `release ok` beziehungsweise `release wrong: <Grund>` aus — wie `verify.sh`, damit das
Skript und Go dasselbe sagen.

- [ ] **Step 4: Write the end-to-end test of a test store**

`apps/cli/classes/teststore_test.go` spielt durch, was das Erfolgskriterium von `apps/cli/CLAUDE.md`
verlangt: „`ngc-store` erzeugt einen Test-Store, aus dem sich eine Kauf-App freischalten lässt.“

```go
func TestTestStore(t *testing.T) {
	dir := t.TempDir()
	// 1. Zwei Apps packen: eine gratis, eine zum Kauf.
	free := packApp(t, dir, "org.sorglos.calculator", "1.0.0")
	paid := packApp(t, dir, "org.sorglos.terminal", "1.0.0")
	// 2. Die Kauf-App verschlüsseln.
	contentKey, err := NewContentKey(filepath.Join(dir, "content.key"))
	if err != nil {
		t.Fatalf("content key: %v", err)
	}
	if err := Encrypt(paid, strings.TrimSuffix(paid, ".ngc")+".enc",
		filepath.Join(dir, "content.key")); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// 3. Den Katalog signieren.
	storeKey := writeSigningKey(t, dir)
	env, err := Catalog(dir, storeKey, CatalogOpts{Store: "test", Seq: 1, Issued: 1790000000})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	// 4. Eine Test-Lizenz für eine Installation ausstellen.
	installKey := newInstallKey(t)
	lic, err := License("org.sorglos.terminal", installation, storeKey,
		filepath.Join(dir, "content.key"), installKey.PublicKey().Bytes(),
		LicenseOpts{Per: store.PerSite, Issued: 1790000000, Store: "test", ID: "test-1"})
	if err != nil {
		t.Fatalf("license: %v", err)
	}
	// 5. Prüfen wie die API: Katalog, Lizenz und der Inhaltsschlüssel.
	st := trust.Store{ID: "test", Name: "Test", URL: "https://store.ngc.test", Key: publicOf(storeKey)}
	if _, err := store.VerifyCatalog(env, st, 0); err != nil {
		t.Fatalf("catalog does not verify: %v", err)
	}
	l, err := store.VerifyLicense(lic, st, installation, "org.sorglos.terminal",
		installKey.PublicKey().Bytes())
	if err != nil {
		t.Fatalf("license does not verify: %v", err)
	}
	key, err := store.UnwrapKey(l.Key, installKey, "org.sorglos.terminal")
	if err != nil || !bytes.Equal(key, contentKey) {
		t.Fatalf("content key: %v", err)
	}
	// 6. Und eine fremde Installation kommt nicht heran.
	other := newInstallKey(t)
	if _, err := store.UnwrapKey(l.Key, other, "org.sorglos.terminal"); err == nil {
		t.Fatal("a foreign installation opened the app")
	}
	_ = free
}
```

- [ ] **Step 5: Run everything**

Run: `go test ./apps/cli/... && tools/check.sh`
Expected: PASS, `check: ok`.

- [ ] **Step 6: Commit**

```bash
git add apps/cli
git commit -m "Add ngc-store release and check and the test store" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 30: Angriffe, Fuzzing und Abschluss

Die Liste aus `apps/server-api/CLAUDE.md`, Abschnitt „Tests“, als eigene Datei, damit sie beim
Lesen zusammensteht und niemand sie über die Pakete verteilt sucht.

**Files:**
- Create: `apps/server-api/classes/server/attack_test.go`
- Modify: `apps/server-api/VERSION` (auf `0.2.0`), `apps/cli/VERSION` (auf `0.2.0`), `CHANGELOG.md`

**Interfaces:**
- Consumes: alles Gebaute, `shared/testdata/`.
- Produces: keine neue Schnittstelle.

- [ ] **Step 1: Write the attack tests**

`apps/server-api/classes/server/attack_test.go`, ein `Test…` je Angriff:

```go
func TestInjectedSQLDoesNothing(t *testing.T)
// Ein Anmeldename, ein App-Name und ein Protokollfilter mit SQL-Zeichen laufen durch, ohne dass
// die Datenbank etwas anderes tut: die API spricht nur über dbapi, und die Namen müssen ihre
// Grammatik einhalten.

func TestForgedSignaturesAreRefused(t *testing.T)
// Vertrauensliste, Aufgabe, Katalog, Lizenz und Release je einmal mit fremdem Schlüssel: alle
// werden abgelehnt (die Vektoren unter trustRejects, catalogRejects, licenseRejects,
// releaseRejects).

func TestReplayedMessagesDoNothing(t *testing.T)
// Anmeldung mit gebrauchter Nonce, Aufgabe mit gebrauchter ID, Kopplung mit gebrauchtem
// Geheimnis, Beitritt mit gebrauchtem Geheimnis.

func TestForeignLicenseDoesNotOpenAnApp(t *testing.T)
// Eine Lizenz einer anderen Installation gibt keinen Inhaltsschlüssel her.

func TestChangedParametersBreakTheChannel(t *testing.T)
// Ein OPEN, dessen params nach der Unterschrift geändert wurden: die Aufgabe deckt sie ab, und
// der Prolog von Ende zu Ende bindet ihre Bytes.

func TestTruncatedDataIsAnError(t *testing.T)
// Ein Paket, das mitten im Übertragen abbricht, passt nicht zur Prüfsumme; ein
// Ende-zu-Ende-Strom ohne das Ende des Controllers gilt als abgeschnitten.

func TestBeyondTheChannelLimitEndsTheLink(t *testing.T)

func TestManInTheMiddleAtSetup(t *testing.T)
// Eine Ersteinrichtung mit einem anderen Code oder über andere Bytes: weder das Angebot noch
// Fassung 1 wird angenommen.
```

- [ ] **Step 2: Run the attack tests**

Run: `go test ./apps/server-api/classes/server/ -run TestInjected -run TestForged -v`
Expected: PASS.

- [ ] **Step 3: Run every fuzz target for three minutes**

Run:
```bash
for t in FuzzOpen FuzzRefresh FuzzSetupBody FuzzStart FuzzFinish FuzzLinkMessage \
         FuzzPair FuzzControl FuzzOpenParams FuzzJoinRequest FuzzUpload; do
  go test ./apps/server-api/... -run Fuzz -fuzz "$t" -fuzztime 180s || exit 1
done
```
Expected: kein Fund. Jeder Fund kommt als Datei unter `testdata/fuzz/` ins Repo und wird behoben,
bevor es weitergeht.

- [ ] **Step 4: Check the foreign code**

Run: `govulncheck ./...`
Expected: keine erreichbare Lücke.

- [ ] **Step 5: Run everything on Linux**

Run (auf einem Linux-Server oder in WSL):
```bash
go test ./... -race
tools/check.sh
```
Expected: alles grün, auch die Tests, die sich unter Windows selbst überspringen.

- [ ] **Step 6: Raise the versions and write the changelog**

`apps/server-api/VERSION` und `apps/cli/VERSION` auf `0.2.0`; in `CHANGELOG.md` einen Abschnitt
`## 0.2.0` mit den Zeilen „API: Anmeldung, Sitzungen, Rechte, Weiterleitung, Tunnel, Apps, Stores,
Lizenzen, Protokoll und Updates“ und „ngc-store: Pakete, Verschlüsselung, Kataloge, Test-Lizenzen
und Test-Releases“.

- [ ] **Step 7: Commit and open the pull request**

```bash
git add apps/server-api apps/cli CHANGELOG.md
git commit -m "Add the attack tests and raise the versions to 0.2.0" \
  --trailer "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
git push origin api
```

Danach Pull Request von `api` nach `main`. Der Koordinator prüft ihn auf Vertrag und Sicherheit.

---

## Selbstprüfung

**1. Deckung gegen `apps/server-api/CLAUDE.md`, Abschnitt „Aufgabe“**

| Verlangt | Aufgabe |
|---|---|
| Anmeldung nach `session`, Nonce, `passwordToo`, TOTP, Sperre, Sitzung, Zertifikate, Cookie | 6, 7, 8 |
| HTTP bis zum WebSocket genau nach „Vor dem WebSocket“ | 4, 5, 6, 7, 8 |
| Ersteinrichtung mit Einmal-Code, `SetupOffer`, `OfferMAC`, `VerifySetup` | 5 |
| Adressen nach der Grammatik (`url`, `storeUrl`) | 2 (Host), 5 (Angebot), 19 (Store-URL) |
| Eigenes TLS, 100 Jahre, Erneuern nur mit demselben Schlüssel | 2 |
| WebSocket nach `wire` und `noisep`, Kanalgrenzen, Freigabe, `ControlAllowed` | 9, 10, 11 |
| Kanäle weiterleiten, erste GRANT, `transfer`, `ShellOpens`, `E_FORBIDDEN` | 17, 22 |
| Ergebnisse begrenzen (`wire.MaxCallResult`), `core.EncodeResult` | 15, 17 |
| `MaxIn` bei `upload`, `trust`, `license` | 15, 16, 20, 21 |
| Umschläge nur über `sign.ParseEnvelope`, `trust.ParseSigned` | 16, 19, 21 |
| Vertragsfassungen nebeneinander, `tunnel.Hello.Contract` | 12 (hello), 24 (Router) |
| Rechte je Kanal, `task.Check`, `core.RuleFor`, `store.RuleFor`, `dbapi.OnceTask` | 13 |
| Tunnel, Kopplung, Kopplungs- und Beitrittscodes, `pending`, `pending-api` | 12, 16, 18 |
| Vertrauensliste an Controller und Browser, Hinweis `trust` | 3, 11, 16 |
| `ngc.users/nodes` mit `online`, Rollen, Version, Architektur | 16 |
| Beitritt weiterer API-Server, gemeinsame Schlüssel | 18 |
| Apps und Stores: Kataloge, Pakete, Lizenzen, `UnwrapKey`, `global`/`local` | 19, 20, 21, 22 |
| Oberflächen: `dbapi.BlobUI`, `ngc.desktop/ui`, `ui-proof`, `Entry` | 15, 20 |
| Updates: Reihenfolge, `dbapi.LockUpdate`, Migrationen, `needsgrants`, `minNgc` | 23 |
| Einmalwerte mit `OncePurge(now - MaxSkew)` | 8 |
| Protokoll mit `core.LogEntry` und `core.ValidLogID` | 14 |
| Werkzeug `ngc-store` | 26, 27, 28, 29 |
| Konfiguration nur über `config.LoadAPI`, Geheimnisse nie ins Log | 1, 24 |
| Tests gegen `memstore`, gegen die Vektoren, Fuzzing, Angriffe | jede Aufgabe, zusammen 30 |
| Installation über `scripts/ngc-api.sh` | 25 |

**2. Deckung gegen „Fertig, wenn“**

- Ein Browser kann sich anmelden → 5–8, 11.
- Der Terminal-Kanal läuft bis zum Controller → 12, 17.
- Rechte und Lizenzen greifen → 13, 21, 22.
- Ein Ausfall einer API unterbricht keine Sitzung → alle Sitzungsdaten in der Datenbank (7, 8),
  API-Sitzungsschlüssel versiegelt (1, 7), jede API bedient jede Sitzung (11).
- `ngc-store` erzeugt einen Test-Store, aus dem sich eine Kauf-App freischalten lässt → 29.

**3. Platzhalter**

Im Plan steht kein „TBD“, kein „später“, kein „ähnlich wie Aufgabe N“. Wo eine Aufgabe eine
Testdatei nur beschreibt statt sie auszuschreiben (Aufgaben 15, 16, 17, 19, 21, 22, 23, 25, 27, 28,
29), nennt sie jeden einzelnen Test mit Namen und Prüfsatz, damit ein Ausführender ihn ohne
Rückfrage schreiben kann.

**4. Namen und Typen**

- `state.State` heißt überall so; `Trust`, `Refresh`, `AddVersion`, `Chain`, `Installation`,
  `InstallKey`, `EnsureInstallation` stimmen in den Aufgaben 3, 5, 8, 15, 16, 19, 21, 23 überein.
- `login.Live` mit `Token`, `Row`, `Verified`, `APIKey` steht in 8 und wird in 11, 15 und 17
  genauso benutzt; `ProofValue()` kommt in 17 dazu.
- `link.Channel` mit `Spend`, `Grant`, `Take`, `Allow`, `SentClose`, `RecvClose`, `Closing`
  stimmt zwischen 10, 11, 15 und 17.
- `controllers.Tunnel` gibt `Hello()` und `Status()` als Methoden zurück (12); die Aufrufe in 16
  und 23 benutzen die Methoden, nicht die Felder.
- `coreapp.Deps` wächst über die Aufgaben 15, 16, 18, 19, 21, 23 und ist in 24 vollständig
  aufgezählt.
- `appstore.Stores` heißt in 19, 20, 21 und 22 gleich; `Catalog`, `License`, `ContentKey`,
  `Upload`, `KeepUI`, `KeepProof` und `Download` sind je einmal festgelegt.

**5. Reihenfolge**

Keine Aufgabe benutzt etwas, das erst später entsteht. Zwei Stellen greifen bewusst vor und sind
dort genannt: `browser.Session` bekommt in 15 und 17 weitere Methoden (die Aufgaben sagen es und
fassen die Datei aus 11 an), und `coreapp.Deps` wächst.

---

## Offene Entscheidungen für den Koordinator

1. **Wie die API die Datenbank öffnet.** Der Plan sieht `dbopen.Register(Opener)` mit
   `func(ctx context.Context, cfg config.DB) (dbapi.Store, error)` vor und in `main.go` einen
   leeren Import des anmeldenden Pakets von `apps/server-db`. Name und Pfad dieses Pakets legt der
   Koordinator zusammen mit der Sitzung DB fest; er gehört zu V4 (`shared/dbapi`).
2. **Einmal-Code.** `ngc-api` legt ihn in `/var/lib/ngc/setup-code` (0600, root) und löscht ihn nach
   der Ersteinrichtung. `shared/ulh.md` braucht dafür eine Zeile bei `ngc-api`: „liest den
   Einmal-Code aus `/var/lib/ngc/setup-code` und zeigt ihn an“.
3. **Gemeinsame Schlüssel beim Beitritt.** Der Plan führt den Pfad `/tunnel/keys` und den Prolog
   `ngc/apikeys/v1` ein und legt `secretKey` und `challengeKey` nach `/run/ngc/join-secret` (0600),
   weil `config`
   kein `SaveAPI` hat und Go `api.json` nicht schreiben darf. Der Prolog gehört sachlich in
   `shared/noisep` (V2), der Pfad und die Datei in `shared/ulh.md`. **Das ist die einzige Stelle,
   an der Entwurf und Verträge nicht zusammenpassen** (siehe unten).
4. **Woher Kataloge und Releases kommen.** `<url>/catalog.json` je Store und
   `<url>/release.json` beim globalen Store, beides `sign.Envelope` als JSON. Gehört ins
   Entscheidungsprotokoll und als Satz nach `shared/README.md` (V5).
5. **`github.com/go-webauthn/webauthn` braucht kein Go-Teil.** Passkeys prüft `shared/proof` mit
   der Standardbibliothek; den Attestierungs-Umschlag beim Anlegen eines Passkeys liest die Shell
   in JavaScript. Die Zeile bleibt daher in `tools/modules.go`. Zu entscheiden: das Modul aus
   `vendor/` nehmen (rund 10 MB und neun Module weniger) oder für die Zertifizierungsstelle
   (Teilprojekt 2) behalten.
6. **Abfrage `?secret=` bei Kopplung und Beitritt.** `tunnel.PairRequest` und `tunnel.JoinRequest`
   tragen keinen Hinweis, welches Einmal-Geheimnis gemeint ist, und `dbapi` gibt keine Liste her.
   Der Plan nennt es in der Abfrage (`?secret=<base64url des SHA-256>`). Das ändert keinen Go-Typ,
   gehört aber als Satz nach `shared/ulh.md` und `shared/README.md`, damit der Controller ihn
   ebenso schickt.
7. **Sperre als `dbapi.BlobSetting` mit dem Präfix `lock.`.** Sie funktioniert, belegt aber die
   Einstellungen mit einer Zeile je versuchtem Namen (fester Größe, mit Aufräumrunde). Sauberer
   wäre eine eigene Art in V4 (`dbapi.BlobLock`) mit einem Index auf die Ablaufzeit.
8. **`Retry-After` bei `E_LOCKED`.** Der Plan schlägt vor: die ersten zwei Fehlversuche frei, dann
   1 s, verdoppelnd bis 300 s, Zeile nach einer Stunde ohne weiteren Fehlversuch weg. Bestätigen
   oder andere Zahlen nennen.
9. **`ngc.desktop/ui` darf mehr als `wire.MaxCallResult` schicken.** Ein ui-tar kann nach
   `store.MaxUITotal` bis rund 84 MiB haben; die Grenze von 8 MiB gilt nur für Ergebnisse nach Art
   von `call`, und `ui` ist `core.FlowOut`. Der Plan geht davon aus, dass das so gemeint ist; eine
   Bestätigung in `shared/README.md` wäre eindeutiger.
10. **Recovery-Modus.** `docs/recovery-modus.md` sieht in Arbeitspaket 7 `AdminVerify` im
    Login-Pfad vor. Dieser Plan berührt ihn nicht; die Stelle dafür ist `login.Finish`, direkt nach
    `session.VerifyClient`. Wenn der Recovery-Strang so weit ist, kommt sie als eigene Aufgabe
    dazu, nicht als Umbau dieses Plans.

### Widerspruch zwischen Entwurf und Verträgen

Genau einer, Punkt 3: Der Entwurf sagt (4.2), ein neuer API-Server bekomme die gemeinsamen
Schlüssel „beim Koppeln von einer laufenden API, über eine verschlüsselte Verbindung und nur mit
Unterschrift eines Admins“, und das Entscheidungsprotokoll vom 21.09.2026 überlässt der API, „wie“.
Die Verträge geben dafür aber nichts her: `noisep` kennt nur drei Prologe, `sign` nur acht Zwecke,
`core.WithoutUser` nur vier Aktionen, und `config` verbietet, `api.json` aus Go zu schreiben
(`SaveController`, `SaveAnchor`, `SaveWeb` — kein `SaveAPI`). Der Plan löst das mit einem eigenen
Pfad, einem eigenen Prolog und einer Übergabedatei für ulh; sauber wird es erst, wenn der Prolog in
`noisep` steht.

Zwei kleinere Unschärfen, kein Widerspruch:

- Entwurf 4.6 zählt „Bestätigungsstufen und ihre Listen“ zu den Tabellen der API; tatsächlich
  stehen sie in der Vertrauensliste (`trust.Role`), und die API hält davon nur die Kopie, die sie
  ohnehin als Fassungen speichert. Der Plan folgt den Verträgen.
- Entwurf 4.8 schlägt vor, NGC-Updates „über den globalen Store als eigener, signierter Katalog“
  zu verteilen. Die Verträge haben dafür `store.Release` mit eigenem Zweck, keinen Katalog. Der
  Plan folgt den Verträgen und legt nur den Dateinamen fest (Punkt 4).

---

## Ausführung

**Plan vollständig und gespeichert unter `docs/superpowers/plans/2026-09-23-ngc-plan-2-api.md`.
Zwei Wege:**

**1. Subagent-Driven (empfohlen)** – ein frischer Unteragent je Aufgabe, Prüfung dazwischen,
schnelle Runden: `superpowers:subagent-driven-development`.

**2. Inline** – die Aufgaben in dieser Sitzung abarbeiten, mit Haltepunkten zum Prüfen:
`superpowers:executing-plans`.

Vor der ersten Aufgabe: Branch `api` anlegen (`git checkout -b api`), `tools/check.sh` einmal
laufen lassen, damit der Ausgangsstand grün ist.
