# NGC Plan 0: Verträge, Anweisungen und Grundgerüst – Umsetzungsplan

> **Für ausführende Agenten:** PFLICHT-SKILL: superpowers:subagent-driven-development (empfohlen)
> oder superpowers:executing-plans, Aufgabe für Aufgabe. Schritte nutzen Checkboxen (`- [ ]`).

> **Pfade seit 22.09.2026:** Das Repo folgt jetzt dem Projektstruktur-Standard; dieser Plan nennt
> noch die alten Pfade. `contracts/` ist `shared/` (Importpfad `…/nextgencloud/shared/…`), `api/`
> ist `apps/server-api/` (`ngc-store`: `apps/cli/`), `controller/` ist `apps/server-controller/`,
> `db/` ist `apps/server-db/`, `web/` ist `apps/web/`, `tests/browser/` ist `apps/web/tests/`,
> `scripts/check.sh` ist `tools/check.sh`, `prototype/index.html` ist `docs/prototype.html`.
>
> **Vertragstext überholt:** Nach zwei Abschlussprüfungen und ihren Fix-Wellen weicht der Vertrag an
> vielen Stellen von diesem Plan ab (etwa `tunnel.Open` ohne `User`, Rollen statt `Parts`, der Prolog
> für Ende zu Ende, Setup mit MAC, TLS-Fingerabdrücke). Maßgeblich sind `shared/` und
> `shared/README.md`; dieser Plan ist nur noch die Geschichte.

**Ziel:** Die sieben Verträge zwischen den Teilen als Go-Pakete mit Testvektoren, je eine
`CLAUDE.md` für die vier Teile und ein Grundgerüst, das baut und testet. Danach können vier
Claude-Sitzungen auf den sieben Servern parallel anfangen, ohne aufeinander zu warten.

**Architektur:** Ein Go-Modul im Wurzelverzeichnis des Repos. `contracts/` (MIT) enthält Typen,
Kodierung, Prüffunktionen, eine Referenz-Datenbank im Speicher und Testvektoren. API, Controller
und Datenbank importieren diese Pakete. Das Webinterface setzt dieselben Verträge in JavaScript
um und prüft sich gegen die Vektoren in `contracts/testdata/`. Signiert wird immer über die
exakten Bytes eines Dokuments (Umschlag aus Nutzlast und Signatur), nie über neu erzeugtes JSON.
Jede Signatur deckt zusätzlich einen Zweck ab, damit eine Signatur für eine Sache nie für eine
andere gilt.

**Tech Stack:** Go 1.27 mit Standardbibliothek (`crypto/ed25519`, `crypto/ecdh`, `crypto/ecdsa`,
`crypto/rsa`, `crypto/hkdf`, `crypto/aes`, `crypto/cipher`, `encoding/json`, `archive/tar`,
`compress/gzip`), `github.com/flynn/noise` v1.1.0 (Noise_KK_25519_AESGCM_SHA256),
`golang.org/x/crypto/argon2`. Fremdcode per `go mod vendor` im Repo.

**Entwurf:** [`docs/superpowers/specs/2026-09-19-ngc-kern-design.md`](../specs/2026-09-19-ngc-kern-design.md),
Abschnitt 4.7, Schritt 1 des Ablaufs.

## Global Constraints

- Go 1.27: `go.mod` mit `go 1.27` und `toolchain go1.27.1`. Gebaut wird für linux/amd64 und
  linux/arm64 mit `CGO_ENABLED=0`.
- Plattformen: Debian und Ubuntu, jeweils amd64 und arm64. Windows wird nicht umgesetzt.
- Browser-Mindestversionen, wegen Ed25519 und X25519 in der Web Crypto API: Chrome/Edge 137,
  Firefox 130, Safari 17.
- Keine Abhängigkeiten zur Laufzeit: Fremdcode nur per `go mod vendor`, jede Übernahme mit
  Herkunft in `THIRD-PARTY.md`; jede Lizenz muss mit der AGPL-3.0 vereinbar sein.
- Lizenz: NGC steht unter AGPL-3.0; `contracts/` und `web/sdk/ngc.js` stehen unter MIT.
- Verschlüsselung: AES-256-GCM; Noise immer als Noise_KK_25519_AESGCM_SHA256; eigene
  Signaturschlüssel Ed25519; Passkeys ES256, EdDSA oder RS256; Passkeys immer mit
  Benutzerprüfung (PIN oder Biometrie).
- Passwortschlüssel: Argon2id mit Standard m=65536 KiB, t=3, p=1, Salz 16 Byte; Mindestwerte
  m=19456 KiB, t=1.
- Kodierung: Binärdaten in JSON immer base64url ohne Auffüllung (RFC 4648 Abschnitt 5, strikt).
- Jede Eingabe von außen wird streng geprüft: unbekannte JSON-Felder und Daten nach dem
  JSON-Wert werden abgelehnt, jede Größe hat eine Obergrenze.
- Zeitstempel in Verträgen: Unix-Sekunden (`int64`); in der Datenbank: `time.Time` (UTC).
- Sprache: Code, Bezeichner und Commit-Nachrichten Englisch; Dokumente, Oberfläche und
  Meldungen für Benutzer Deutsch.
- Commit-Trailer: `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`.
- Prüfung: `scripts/check.sh` (vet, test, Bau für beide Architekturen) muss vor jedem Commit
  grün sein.
- Code aus dem Plan nach dem Einfügen mit `gofmt -w` formatieren. Abweichungen bei Einrückung
  und Ausrichtung sind kein Planfehler.

---

## Dateistruktur nach Plan 0

```
go.mod, go.sum, vendor/            Go-Modul github.com/sorglos-it/nextgencloud, Fremdcode kopiert
LICENSE                            AGPL-3.0
THIRD-PARTY.md                     Herkunft und Lizenz jedes übernommenen Moduls
CLAUDE.md                          Regeln für alle Sitzungen
scripts/check.sh                   vet + test + Bau für linux/amd64 und linux/arm64
contracts/                         Verträge (MIT), gehört dem Koordinator
  LICENSE                          MIT
  README.md                        Übersicht, Änderungsverfahren, Vertrag ↔ Paket
  internal/golden/golden.go        Vergleich mit Dateien in testdata/
  enc/                             base64url, strenges JSON                          (alle)
  sign/                            Umschläge mit Ed25519, Zwecke                     (alle)
  proof/                           Passwortschlüssel und Passkeys                    (V1, V7)
  wire/                            Frames, Kanäle, Fehlercodes                       (V1, V2)
  noisep/                          Noise-Profil für Sitzung, Tunnel, Ende zu Ende    (V1, V2, V7)
  trust/                           Vertrauensliste                                   (V7)
  session/                         Anmeldung, Sitzungszertifikate                    (V1)
  task/                            unterschriebene Aufgaben, erneute Bestätigung     (V1, V2, V7)
  core/                            Aktionen der Kern-Apps                            (V1, V2)
  tunnel/                          Kopplung, Tunnel-Nachrichten                      (V2)
  service/                         Dienst im Container                               (V3)
  dbapi/                           Datenbank-Schnittstellen                          (V4)
  dbapi/memstore/                  Referenz im Speicher
  dbapi/dbtest/                    Konformitätstests für jede Umsetzung
  store/                           Manifest, Katalog, Lizenz, Release, Paket, Verschlüsselung (V5)
  config/                          Konfigurationsdateien, Pfade, Ports               (V6)
  ulh.md                           ulh-Konventionen                                  (V6)
  testdata/                        Testvektoren für Go und JavaScript
api/CLAUDE.md, api/cmd/ngc-api/, api/cmd/ngc-store/
controller/CLAUDE.md, controller/cmd/ngc-controller/
db/CLAUDE.md, db/doc.go
web/CLAUDE.md, web/README.md
```

Vertragsnummern wie im Entwurf 4.7: V1 Webinterface ↔ API, V2 API ↔ Controller, V3 Controller ↔
Dienst, V4 Datenbank, V5 App-Paket/Katalog/Lizenz/Updates, V6 ulh und Konfiguration,
V7 Vertrauensliste und Ende-zu-Ende-Verschlüsselung.

---

### Task 1: Werkzeug und Grundgerüst

**Files:**
- Create: `go.mod`, `LICENSE`, `contracts/LICENSE`, `contracts/doc.go`, `scripts/check.sh`,
  `contracts/internal/golden/golden.go`, `contracts/testdata/.gitkeep`
- Modify: `.gitignore`

**Interfaces:**
- Produces: Modulpfad `github.com/sorglos-it/nextgencloud`; `golden.Check(t *testing.T, update bool, name string, v any)`,
  schreibt bzw. vergleicht `contracts/testdata/<name>`.

- [ ] **Step 1: Go installieren (nur mit Freigabe von Thomas)**

Auf dem Rechner des Koordinators ist kein Go installiert. Thomas fragen, dann:

```powershell
winget install --id GoLang.Go -e
```

Danach in einer neuen Shell prüfen:

```bash
go version
```

Erwartet: `go version go1.27.1 windows/amd64` (oder neuer innerhalb von 1.27).

- [ ] **Step 2: Branch anlegen**

```bash
git -C Z:/GitHub/nextgencloud switch -c plan-0-vertraege
```

- [ ] **Step 3: Go-Modul anlegen**

`go.mod`:

```
module github.com/sorglos-it/nextgencloud

go 1.27

toolchain go1.27.1
```

- [ ] **Step 4: Lizenzen anlegen**

```bash
gh api licenses/agpl-3.0 --jq .body > LICENSE
```

`contracts/LICENSE`:

```
MIT License

Copyright (c) 2026 Sorglos Thomas Weirich

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

- [ ] **Step 5: Paketdoku und Golden-Helfer anlegen**

`contracts/doc.go`:

```go
// Package contracts holds the agreements between the parts of NGC: web
// interface, API, controller and database. The sub-packages are the source of
// truth; the web interface implements the same rules in JavaScript and checks
// itself against the vectors in contracts/testdata.
//
// Only the coordinator changes these packages. Licensed under MIT.
package contracts
```

`contracts/internal/golden/golden.go`:

```go
// Package golden compares test output with files in contracts/testdata.
package golden

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Dir is the testdata directory as seen from a package directory directly
// below contracts/ (for example contracts/sign).
var Dir = filepath.Join("..", "testdata")

// Check encodes v as indented JSON and compares it with Dir/name. With update
// set it rewrites the file instead.
func Check(t *testing.T, update bool, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join(Dir, name)
	if update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update once to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from the golden file; if intended, run go test -update and review the diff", name)
	}
}

// Load reads Dir/name into v.
func Load(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(Dir, name))
	if err != nil {
		t.Fatalf("%v (run go test -update once to create it)", err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}
```

`contracts/testdata/.gitkeep`: leere Datei.

- [ ] **Step 6: Prüfskript anlegen**

`scripts/check.sh`:

```bash
#!/usr/bin/env bash
# Runs the checks every commit must pass.
set -euo pipefail
cd "$(dirname "$0")/.."
files=$(git ls-files '*.go' ':!:vendor/*')
if [ -n "$files" ] && [ -n "$(gofmt -l $files)" ]; then
	gofmt -l $files
	echo "gofmt needed"
	exit 1
fi
go vet ./...
go test ./...
for arch in amd64 arm64; do
	CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build ./...
done
echo "check: ok"
```

```bash
git add --chmod=+x scripts/check.sh
```

- [ ] **Step 7: .gitignore ergänzen**

An `.gitignore` anhängen:

```
# build output
/bin/
/dist/

# browser tests (npm only there)
/tests/browser/node_modules/
/tests/browser/test-results/
```

- [ ] **Step 8: Prüfen**

Run: `bash scripts/check.sh`
Expected: `check: ok`

- [ ] **Step 9: Commit**

```bash
git add go.mod LICENSE contracts/LICENSE contracts/doc.go contracts/internal/golden/golden.go contracts/testdata/.gitkeep scripts/check.sh .gitignore
git commit -m "Add the Go module, licences and the check script" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 2: Fremdcode prüfen und das Verzeichnis dafür anlegen

Die Module selbst kommen mit den Aufgaben, die sie zuerst importieren (Task 5: `golang.org/x/crypto`,
Task 7: `github.com/flynn/noise`). Sonst entfernt `go mod tidy` sie sofort wieder.

**Files:**
- Create: `THIRD-PARTY.md`

**Interfaces:**
- Produces: `THIRD-PARTY.md` mit Tabelle `| Modul | Version | Lizenz | Wofür | Übernommen |`; jede
  spätere Übernahme ergänzt eine Zeile.

- [ ] **Step 1: Lizenzen und Pflege prüfen**

```bash
gh api repos/flynn/noise --jq '{license: .license.spdx_id, archived: .archived, pushed: .pushed_at}'
gh api repos/golang/crypto --jq '{license: .license.spdx_id, archived: .archived, pushed: .pushed_at}'
```

Erwartet: flynn/noise `BSD-3-Clause`, nicht archiviert; golang/crypto `BSD-3-Clause`, nicht
archiviert. Weicht etwas ab, anhalten und Thomas fragen.

- [ ] **Step 2: THIRD-PARTY.md anlegen**

```markdown
# Fremdcode in NGC

Übernommen per `go mod vendor` (Ordner `vendor/`) beziehungsweise, für JavaScript, als Kopie unter
`web/vendor/`. Jede Lizenz ist mit der AGPL-3.0 vereinbar. Sicherheitsupdates ziehen wir selbst
nach: `go get <modul>@<version>`, `go mod vendor`, `govulncheck ./...`, Eintrag hier anpassen.

| Modul | Version | Lizenz | Wofür | Übernommen |
|---|---|---|---|---|

Eigene Änderungen an übernommenem Code: keine.
```

- [ ] **Step 3: Commit**

```bash
git add THIRD-PARTY.md
git commit -m "Add the register of third-party code" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 3: Kodierung (`contracts/enc`)

**Files:**
- Create: `contracts/enc/enc.go`, `contracts/enc/enc_test.go`

**Interfaces:**
- Produces: `enc.B64` (`*base64.Encoding`, base64url ohne Auffüllung, strikt);
  `type enc.Bytes []byte` (JSON als base64url); `enc.DecodeStrict(data []byte, max int, v any) error`;
  Fehler `enc.ErrTooLarge`, `enc.ErrTrailingData`.

- [ ] **Step 1: Test schreiben**

`contracts/enc/enc_test.go`:

```go
package enc

import (
	"encoding/json"
	"testing"
)

func TestBytesRoundTrip(t *testing.T) {
	in := Bytes{0x00, 0xfb, 0xff}
	out, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"APv_"` {
		t.Fatalf("got %s", out)
	}
	var back Bytes
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if string(back) != string(in) {
		t.Fatalf("round trip: %x", back)
	}
}

func TestBytesRejectsNonCanonical(t *testing.T) {
	for _, s := range []string{`"AA=="`, `"+/8"`, `"AB"`} {
		var b Bytes
		if err := json.Unmarshal([]byte(s), &b); err == nil {
			t.Errorf("%s: accepted", s)
		}
	}
}

func TestDecodeStrict(t *testing.T) {
	type doc struct {
		A int `json:"a"`
	}
	var d doc
	if err := DecodeStrict([]byte(`{"a":1}`), 100, &d); err != nil || d.A != 1 {
		t.Fatalf("valid: %v %v", err, d)
	}
	if err := DecodeStrict([]byte(`{"a":1,"b":2}`), 100, &d); err == nil {
		t.Error("unknown field accepted")
	}
	if err := DecodeStrict([]byte(`{"a":1} {}`), 100, &d); err != ErrTrailingData {
		t.Errorf("trailing data: %v", err)
	}
	if err := DecodeStrict([]byte(`{"a":1}`), 3, &d); err != ErrTooLarge {
		t.Errorf("too large: %v", err)
	}
}

func FuzzBytes(f *testing.F) {
	f.Add(`"APv_"`)
	f.Add(`"AA=="`)
	f.Fuzz(func(t *testing.T, s string) {
		var b Bytes
		if json.Unmarshal([]byte(s), &b) != nil {
			return
		}
		out, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		var back Bytes
		if err := json.Unmarshal(out, &back); err != nil || string(back) != string(b) {
			t.Fatalf("round trip failed for %q", s)
		}
	})
}
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

Run: `go test ./contracts/enc/`
Expected: FAIL, `undefined: Bytes`

- [ ] **Step 3: Umsetzung schreiben**

`contracts/enc/enc.go`:

```go
// Package enc holds the encodings shared by all NGC contracts.
package enc

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
)

// B64 is the only binary-to-text encoding in NGC contracts: base64url without
// padding (RFC 4648, section 5). Decoding is strict: non-zero trailing bits
// are rejected, so every byte string has exactly one encoding.
var B64 = base64.RawURLEncoding.Strict()

// Bytes is a byte slice that encodes as a base64url string in JSON.
type Bytes []byte

// MarshalJSON implements json.Marshaler.
func (b Bytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(B64.EncodeToString(b))
}

// UnmarshalJSON implements json.Unmarshaler.
func (b *Bytes) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	v, err := B64.DecodeString(s)
	if err != nil {
		return err
	}
	*b = v
	return nil
}

var (
	// ErrTooLarge reports input above the allowed size.
	ErrTooLarge = errors.New("enc: input too large")
	// ErrTrailingData reports bytes after the first JSON value.
	ErrTrailingData = errors.New("enc: trailing data after JSON value")
)

// DecodeStrict decodes exactly one JSON value from data into v. It rejects
// inputs longer than max bytes, unknown object fields and trailing data.
func DecodeStrict(data []byte, max int, v any) error {
	if len(data) > max {
		return ErrTooLarge
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return ErrTrailingData
	}
	return nil
}
```

- [ ] **Step 4: Tests laufen lassen**

Run: `go test ./contracts/enc/ && go test -run=XXX -fuzz=FuzzBytes -fuzztime=20s ./contracts/enc/`
Expected: `ok`, Fuzzing ohne Fund.

- [ ] **Step 5: Commit**

```bash
git add contracts/enc
git commit -m "Add the shared encodings of the contracts" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 4: Signierte Umschläge (`contracts/sign`)

**Files:**
- Create: `contracts/sign/sign.go`, `contracts/sign/sign_test.go`, `contracts/testdata/sign.json` (per `-update`)

**Interfaces:**
- Consumes: `enc.Bytes`, `enc.B64`, `golden.Check`.
- Produces: Zwecke `sign.PurposeClientCert`, `PurposeAPICert`, `PurposeTrustList`,
  `PurposeTask`, `PurposeConfirm`, `PurposeCatalog`, `PurposeLicense`, `PurposeRelease`;
  `sign.Message(purpose string, payload []byte) []byte`; `sign.KeyID(pub []byte) string`;
  `type sign.Envelope struct{ Payload enc.Bytes; KeyID string; Sig enc.Bytes }` (JSON `payload`, `kid`, `sig`);
  `sign.Sign(priv ed25519.PrivateKey, purpose string, payload []byte) (Envelope, error)`;
  `sign.Verify(env Envelope, pub ed25519.PublicKey, purpose string) error`;
  Fehler `ErrBadSignature`, `ErrWrongKey`, `ErrTooLarge`; `sign.MaxPayload = 4 << 20`.

- [ ] **Step 1: Test schreiben**

`contracts/sign/sign_test.go`:

```go
package sign

import (
	"crypto/ed25519"
	"flag"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

func testKey(fill byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = fill + byte(i)
	}
	return ed25519.NewKeyFromSeed(seed)
}

func TestSignVerify(t *testing.T) {
	priv := testKey(0)
	pub := priv.Public().(ed25519.PublicKey)
	env, err := Sign(priv, PurposeTask, []byte(`{"v":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(env, pub, PurposeTask); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := Verify(env, pub, PurposeConfirm); err != ErrBadSignature {
		t.Errorf("other purpose: %v", err)
	}
	other := testKey(100).Public().(ed25519.PublicKey)
	if err := Verify(env, other, PurposeTask); err != ErrWrongKey {
		t.Errorf("other key: %v", err)
	}
	env.Payload = []byte(`{"v":2}`)
	if err := Verify(env, pub, PurposeTask); err != ErrBadSignature {
		t.Errorf("tampered payload: %v", err)
	}
}

func TestSignTooLarge(t *testing.T) {
	if _, err := Sign(testKey(0), PurposeTask, make([]byte, MaxPayload+1)); err != ErrTooLarge {
		t.Fatalf("got %v", err)
	}
}

type signVector struct {
	Seed     enc.Bytes `json:"seed"`
	Public   enc.Bytes `json:"public"`
	Purpose  string    `json:"purpose"`
	Message  enc.Bytes `json:"message"`
	Envelope Envelope  `json:"envelope"`
}

func TestVectors(t *testing.T) {
	priv := testKey(0)
	payload := []byte(`{"v":1,"seq":1}`)
	env, err := Sign(priv, PurposeTrustList, payload)
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, *update, "sign.json", signVector{
		Seed:     priv.Seed(),
		Public:   []byte(priv.Public().(ed25519.PublicKey)),
		Purpose:  PurposeTrustList,
		Message:  Message(PurposeTrustList, payload),
		Envelope: env,
	})
}
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

Run: `go test ./contracts/sign/`
Expected: FAIL, `undefined: Sign`

- [ ] **Step 3: Umsetzung schreiben**

`contracts/sign/sign.go`:

```go
// Package sign defines envelopes for documents signed with Ed25519 keys held
// by NGC components: API nodes, stores and the release key.
package sign

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
)

// Purposes. A signature always covers Message(purpose, payload), so a
// signature made for one purpose never verifies for another.
const (
	PurposeClientCert = "clientcert/v1"
	PurposeAPICert    = "apicert/v1"
	PurposeTrustList  = "trustlist/v1"
	PurposeTask       = "task/v1"
	PurposeConfirm    = "confirm/v1"
	PurposeCatalog    = "catalog/v1"
	PurposeLicense    = "license/v1"
	PurposeRelease    = "release/v1"
)

// MaxPayload is the largest payload an envelope may carry.
const MaxPayload = 4 << 20

// Envelope is a document signed with an Ed25519 key. Verify the signature
// before parsing Payload.
type Envelope struct {
	Payload enc.Bytes `json:"payload"`
	KeyID   string    `json:"kid"`
	Sig     enc.Bytes `json:"sig"`
}

var (
	ErrBadSignature = errors.New("sign: bad signature")
	ErrWrongKey     = errors.New("sign: envelope names a different key")
	ErrTooLarge     = errors.New("sign: payload too large")
)

// Message returns the bytes a signature covers: "ngc/" || purpose || 0x00 || payload.
func Message(purpose string, payload []byte) []byte {
	m := make([]byte, 0, len("ngc/")+len(purpose)+1+len(payload))
	m = append(m, "ngc/"...)
	m = append(m, purpose...)
	m = append(m, 0)
	return append(m, payload...)
}

// KeyID identifies a public key: base64url of the first 16 bytes of its SHA-256.
func KeyID(pub []byte) string {
	h := sha256.Sum256(pub)
	return enc.B64.EncodeToString(h[:16])
}

// Sign signs payload for purpose.
func Sign(priv ed25519.PrivateKey, purpose string, payload []byte) (Envelope, error) {
	if len(payload) > MaxPayload {
		return Envelope{}, ErrTooLarge
	}
	pub := priv.Public().(ed25519.PublicKey)
	return Envelope{
		Payload: payload,
		KeyID:   KeyID(pub),
		Sig:     ed25519.Sign(priv, Message(purpose, payload)),
	}, nil
}

// Verify checks env against pub for purpose.
func Verify(env Envelope, pub ed25519.PublicKey, purpose string) error {
	if len(env.Payload) > MaxPayload {
		return ErrTooLarge
	}
	if env.KeyID != KeyID(pub) {
		return ErrWrongKey
	}
	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, Message(purpose, env.Payload), env.Sig) {
		return ErrBadSignature
	}
	return nil
}
```

- [ ] **Step 4: Testvektor erzeugen und Tests laufen lassen**

Run: `go test ./contracts/sign/ -run TestVectors -update && go test ./contracts/sign/`
Expected: `ok`; `contracts/testdata/sign.json` ist angelegt.

- [ ] **Step 5: Commit**

```bash
git add contracts/sign contracts/testdata/sign.json
git commit -m "Add signed envelopes with purpose binding" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 5: Benutzerbeweise (`contracts/proof`)

Ein Beweis zeigt, dass ein Benutzer eine Nutzlast für einen Zweck gebilligt hat: mit dem
Passwortschlüssel (Ed25519, im Browser aus dem Passwort abgeleitet) oder mit einem Passkey
(WebAuthn). Passkeys zählen nur mit Benutzerprüfung (PIN oder Biometrie).

**Files:**
- Create: `contracts/proof/proof.go`, `contracts/proof/proof_test.go`,
  `contracts/testdata/proof.json`, `contracts/testdata/proof_kdf.json` (beide per `-update`)
- Modify: `go.mod`, `go.sum`, `vendor/`, `THIRD-PARTY.md`

**Interfaces:**
- Consumes: `enc.Bytes`, `enc.B64`, `sign.Message`, `sign.KeyID`, Zwecke aus `sign`.
- Produces:
  - `type proof.Kind string`, `proof.KindPassword = "password"`, `proof.KindPasskey = "passkey"`
  - `proof.AlgEdDSA = "EdDSA"`, `AlgES256 = "ES256"`, `AlgRS256 = "RS256"`
  - `type proof.KDF struct{ Alg string; Salt enc.Bytes; MemKiB, Time uint32; Par uint8 }` (JSON `alg`, `salt`, `m`, `t`, `p`), `(KDF) Validate() error`
  - Konstanten `KDFArgon2id`, `SaltSize = 16`, `DefaultMemKiB = 65536`, `DefaultTime = 3`, `DefaultPar = 1`, `MinMemKiB = 19456`, `MaxMemKiB = 1 << 20`
  - `proof.DeriveKey(password []byte, k KDF) (ed25519.PrivateKey, error)`
  - `type proof.UserKey struct{ ID string; Kind Kind; Alg string; Public, CredID enc.Bytes; KDF *KDF }` (JSON `id`, `kind`, `alg`, `pub`, `credId`, `kdf`), `(UserKey) Validate() error`; die ID ist immer `sign.KeyID(Public)`
  - `type proof.Proof struct{ Kind Kind; KeyID string; Sig, AuthData, ClientData enc.Bytes }` (JSON `kind`, `kid`, `sig`, `authData`, `clientData`)
  - `type proof.RP struct{ ID string; Origins []string }` (JSON `id`, `origins`)
  - `proof.Challenge(purpose string, payload []byte) []byte` = SHA-256(`sign.Message(purpose, payload)`)
  - `proof.Verify(p Proof, key UserKey, rp RP, purpose string, payload []byte) error`
  - Fehler `ErrKDF`, `ErrKey`, `ErrKeyMismatch`, `ErrSignature`, `ErrClientData`, `ErrAuthData`, `ErrNoUV`

- [ ] **Step 1: Test schreiben**

`contracts/proof/proof_test.go`:

```go
package proof

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"flag"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

var testRP = RP{ID: "ngc.example.org", Origins: []string{"https://ngc.example.org"}}

// authenticator is a software passkey for tests.
type authenticator struct {
	key    UserKey
	signFn func(digest, signed []byte) []byte
}

func newAuthenticator(t *testing.T, alg string) authenticator {
	t.Helper()
	var (
		pub    any
		signFn func(digest, signed []byte) []byte
	)
	switch alg {
	case AlgES256:
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		pub = &priv.PublicKey
		signFn = func(digest, _ []byte) []byte {
			s, err := ecdsa.SignASN1(rand.Reader, priv, digest)
			if err != nil {
				t.Fatal(err)
			}
			return s
		}
	case AlgEdDSA:
		p, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		pub = p
		signFn = func(_, signed []byte) []byte { return ed25519.Sign(priv, signed) }
	case AlgRS256:
		priv, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		pub = &priv.PublicKey
		signFn = func(digest, _ []byte) []byte {
			s, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, digest)
			if err != nil {
				t.Fatal(err)
			}
			return s
		}
	}
	spki, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return authenticator{
		key:    UserKey{ID: sign.KeyID(spki), Kind: KindPasskey, Alg: alg, Public: spki, CredID: []byte("credential-1")},
		signFn: signFn,
	}
}

func (a authenticator) prove(rp RP, purpose string, payload []byte, flags byte, origin string) Proof {
	cd, _ := json.Marshal(map[string]any{
		"type":        "webauthn.get",
		"challenge":   enc.B64.EncodeToString(Challenge(purpose, payload)),
		"origin":      origin,
		"crossOrigin": false,
	})
	rpHash := sha256.Sum256([]byte(rp.ID))
	ad := append(append([]byte{}, rpHash[:]...), flags, 0, 0, 0, 7)
	cdHash := sha256.Sum256(cd)
	signed := append(append([]byte{}, ad...), cdHash[:]...)
	digest := sha256.Sum256(signed)
	return Proof{Kind: KindPasskey, KeyID: a.key.ID, Sig: a.signFn(digest[:], signed), AuthData: ad, ClientData: cd}
}

func TestPasskeyAlgorithms(t *testing.T) {
	for _, alg := range []string{AlgES256, AlgEdDSA, AlgRS256} {
		a := newAuthenticator(t, alg)
		if err := a.key.Validate(); err != nil {
			t.Fatalf("%s: validate: %v", alg, err)
		}
		p := a.prove(testRP, sign.PurposeClientCert, []byte("statement"), flagUP|flagUV, testRP.Origins[0])
		if err := Verify(p, a.key, testRP, sign.PurposeClientCert, []byte("statement")); err != nil {
			t.Errorf("%s: %v", alg, err)
		}
	}
}

func TestPasskeyRejects(t *testing.T) {
	a := newAuthenticator(t, AlgES256)
	payload := []byte("statement")
	cases := []struct {
		name string
		p    Proof
		want error
	}{
		{"no user verification", a.prove(testRP, sign.PurposeClientCert, payload, flagUP, testRP.Origins[0]), ErrNoUV},
		{"foreign origin", a.prove(testRP, sign.PurposeClientCert, payload, flagUP|flagUV, "https://evil.example"), ErrClientData},
		{"other purpose", a.prove(testRP, sign.PurposeTask, payload, flagUP|flagUV, testRP.Origins[0]), ErrClientData},
		{"other rp", a.prove(RP{ID: "evil.example", Origins: testRP.Origins}, sign.PurposeClientCert, payload, flagUP|flagUV, testRP.Origins[0]), ErrAuthData},
	}
	for _, c := range cases {
		if err := Verify(c.p, a.key, testRP, sign.PurposeClientCert, payload); err != c.want {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
	good := a.prove(testRP, sign.PurposeClientCert, payload, flagUP|flagUV, testRP.Origins[0])
	good.Sig[len(good.Sig)-1] ^= 1
	if err := Verify(good, a.key, testRP, sign.PurposeClientCert, payload); err != ErrSignature {
		t.Errorf("tampered signature: %v", err)
	}
}

func testKDF() KDF {
	salt := make([]byte, SaltSize)
	for i := range salt {
		salt[i] = byte(i + 1)
	}
	return KDF{Alg: KDFArgon2id, Salt: salt, MemKiB: DefaultMemKiB, Time: DefaultTime, Par: DefaultPar}
}

func passwordKey(t *testing.T, password string) (ed25519.PrivateKey, UserKey) {
	t.Helper()
	k := testKDF()
	priv, err := DeriveKey([]byte(password), k)
	if err != nil {
		t.Fatal(err)
	}
	pub := []byte(priv.Public().(ed25519.PublicKey))
	return priv, UserKey{ID: sign.KeyID(pub), Kind: KindPassword, Alg: AlgEdDSA, Public: pub, KDF: &k}
}

func TestPasswordKey(t *testing.T) {
	priv, key := passwordKey(t, "Heimnetz-2026")
	if err := key.Validate(); err != nil {
		t.Fatal(err)
	}
	payload := []byte("statement")
	p := Proof{Kind: KindPassword, KeyID: key.ID, Sig: ed25519.Sign(priv, sign.Message(sign.PurposeClientCert, payload))}
	if err := Verify(p, key, testRP, sign.PurposeClientCert, payload); err != nil {
		t.Fatal(err)
	}
	if err := Verify(p, key, testRP, sign.PurposeTask, payload); err != ErrSignature {
		t.Errorf("other purpose: %v", err)
	}
	weak := testKDF()
	weak.MemKiB = 1024
	if _, err := DeriveKey([]byte("x"), weak); err != ErrKDF {
		t.Errorf("weak KDF: %v", err)
	}
}

type kdfVector struct {
	Password string    `json:"password"`
	KDF      KDF       `json:"kdf"`
	Seed     enc.Bytes `json:"seed"`
	Public   enc.Bytes `json:"public"`
}

func TestKDFVectors(t *testing.T) {
	var vs []kdfVector
	for _, pw := range []string{"Heimnetz-2026", "Käse&Brot"} {
		priv, _ := passwordKey(t, pw)
		vs = append(vs, kdfVector{Password: pw, KDF: testKDF(), Seed: priv.Seed(), Public: []byte(priv.Public().(ed25519.PublicKey))})
	}
	golden.Check(t, *update, "proof_kdf.json", vs)
}

type proofVector struct {
	Name    string    `json:"name"`
	Purpose string    `json:"purpose"`
	Payload enc.Bytes `json:"payload"`
	Key     UserKey   `json:"key"`
	Proof   Proof     `json:"proof"`
	Valid   bool      `json:"valid"`
}

type proofVectors struct {
	RP     RP            `json:"rp"`
	Proofs []proofVector `json:"proofs"`
}

// Passkey test keys are random and ECDSA signatures are randomized, so this
// golden file is written once with -update and afterwards only verified.
func TestProofVectors(t *testing.T) {
	if *update {
		writeProofVectors(t)
	}
	var vs proofVectors
	golden.Load(t, "proof.json", &vs)
	for _, v := range vs.Proofs {
		err := Verify(v.Proof, v.Key, vs.RP, v.Purpose, v.Payload)
		if (err == nil) != v.Valid {
			t.Errorf("%s: valid=%v, got %v", v.Name, v.Valid, err)
		}
	}
}

func writeProofVectors(t *testing.T) {
	payload := []byte(`{"v":1}`)
	vs := proofVectors{RP: testRP}
	for _, alg := range []string{AlgES256, AlgEdDSA, AlgRS256} {
		a := newAuthenticator(t, alg)
		vs.Proofs = append(vs.Proofs,
			proofVector{Name: "passkey " + alg, Purpose: sign.PurposeClientCert, Payload: payload, Key: a.key,
				Proof: a.prove(testRP, sign.PurposeClientCert, payload, flagUP|flagUV, testRP.Origins[0]), Valid: true},
			proofVector{Name: "passkey " + alg + " without PIN", Purpose: sign.PurposeClientCert, Payload: payload, Key: a.key,
				Proof: a.prove(testRP, sign.PurposeClientCert, payload, flagUP, testRP.Origins[0]), Valid: false})
	}
	priv, key := passwordKey(t, "Heimnetz-2026")
	vs.Proofs = append(vs.Proofs, proofVector{Name: "password key", Purpose: sign.PurposeClientCert, Payload: payload, Key: key,
		Proof: Proof{Kind: KindPassword, KeyID: key.ID, Sig: ed25519.Sign(priv, sign.Message(sign.PurposeClientCert, payload))}, Valid: true})
	golden.Check(t, true, "proof.json", vs)
}

func FuzzVerifyPasskey(f *testing.F) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		f.Fatal(err)
	}
	spki, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		f.Fatal(err)
	}
	key := UserKey{ID: sign.KeyID(spki), Kind: KindPasskey, Alg: AlgES256, Public: spki, CredID: []byte("c")}
	f.Add([]byte(`{"type":"webauthn.get"}`), make([]byte, 37), []byte("sig"))
	f.Fuzz(func(t *testing.T, clientData, authData, sig []byte) {
		_ = Verify(Proof{Kind: KindPasskey, KeyID: key.ID, Sig: sig, AuthData: authData, ClientData: clientData},
			key, testRP, sign.PurposeClientCert, []byte("p"))
	})
}
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

Run: `go test ./contracts/proof/`
Expected: FAIL, `undefined: UserKey`

- [ ] **Step 3: Argon2 übernehmen**

```bash
go get golang.org/x/crypto@latest
```

In `THIRD-PARTY.md` unter der Tabellenüberschrift eine Zeile ergänzen, die Version aus
`go list -m golang.org/x/crypto`:

```
| golang.org/x/crypto | (Ausgabe von go list -m golang.org/x/crypto) | BSD-3-Clause | Argon2id für den Passwortschlüssel | 2026-09-19 |
```

- [ ] **Step 4: Umsetzung schreiben**

`contracts/proof/proof.go`:

```go
// Package proof verifies that a user approved a payload: with the key the
// browser derives from the password (password key) or with a passkey.
package proof

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"errors"
	"slices"

	"golang.org/x/crypto/argon2"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
)

// Kind tells password keys and passkeys apart.
type Kind string

const (
	KindPassword Kind = "password"
	KindPasskey  Kind = "passkey"
)

// Signature algorithms.
const (
	AlgEdDSA = "EdDSA"
	AlgES256 = "ES256"
	AlgRS256 = "RS256"
)

// Argon2id parameters for the password key.
const (
	KDFArgon2id   = "argon2id"
	SaltSize      = 16
	DefaultMemKiB = 65536
	DefaultTime   = 3
	DefaultPar    = 1
	MinMemKiB     = 19456
	MaxMemKiB     = 1 << 20
)

// KDF describes how the browser derives the password key:
//
//	seed = Argon2id(password, Salt, Time, MemKiB, Par, 32 bytes)
//	key  = Ed25519 private key from seed
//
// The password is the UTF-8 encoding of the typed text after Unicode NFC
// normalization (JavaScript: password.normalize("NFC")).
type KDF struct {
	Alg    string    `json:"alg"`
	Salt   enc.Bytes `json:"salt"`
	MemKiB uint32    `json:"m"`
	Time   uint32    `json:"t"`
	Par    uint8     `json:"p"`
}

var (
	ErrKDF         = errors.New("proof: KDF parameters out of range")
	ErrKey         = errors.New("proof: malformed user key")
	ErrKeyMismatch = errors.New("proof: proof names another key")
	ErrSignature   = errors.New("proof: bad signature")
	ErrClientData  = errors.New("proof: client data does not match")
	ErrAuthData    = errors.New("proof: authenticator data does not match")
	ErrNoUV        = errors.New("proof: user verification (PIN or biometrics) missing")
)

// Validate checks the parameters against the allowed ranges.
func (k KDF) Validate() error {
	if k.Alg != KDFArgon2id || len(k.Salt) != SaltSize ||
		k.MemKiB < MinMemKiB || k.MemKiB > MaxMemKiB ||
		k.Time < 1 || k.Time > 10 || k.Par < 1 || k.Par > 4 {
		return ErrKDF
	}
	return nil
}

// DeriveKey derives the password key. password must already be NFC-normalized UTF-8.
func DeriveKey(password []byte, k KDF) (ed25519.PrivateKey, error) {
	if err := k.Validate(); err != nil {
		return nil, err
	}
	seed := argon2.IDKey(password, k.Salt, k.Time, k.MemKiB, k.Par, ed25519.SeedSize)
	return ed25519.NewKeyFromSeed(seed), nil
}

// UserKey is a key of a user as listed in the trust list.
type UserKey struct {
	ID     string    `json:"id"`
	Kind   Kind      `json:"kind"`
	Alg    string    `json:"alg"`
	Public enc.Bytes `json:"pub"`              // password: raw Ed25519 key; passkey: SubjectPublicKeyInfo (DER)
	CredID enc.Bytes `json:"credId,omitempty"` // passkey only
	KDF    *KDF      `json:"kdf,omitempty"`    // password only
}

// Validate checks the structure of k.
func (k UserKey) Validate() error {
	if k.ID != sign.KeyID(k.Public) {
		return ErrKey
	}
	switch k.Kind {
	case KindPassword:
		if k.Alg != AlgEdDSA || len(k.Public) != ed25519.PublicKeySize || k.KDF == nil || len(k.CredID) != 0 {
			return ErrKey
		}
		return k.KDF.Validate()
	case KindPasskey:
		if len(k.CredID) == 0 || len(k.CredID) > 1023 || k.KDF != nil {
			return ErrKey
		}
		_, err := parsePublic(k)
		return err
	}
	return ErrKey
}

// Proof shows that the holder of a user key approved a payload.
type Proof struct {
	Kind  Kind   `json:"kind"`
	KeyID string `json:"kid"`
	// Password keys: Ed25519 signature over sign.Message(purpose, payload).
	// Passkeys: the WebAuthn assertion signature.
	Sig enc.Bytes `json:"sig"`
	// Passkeys only. The challenge in ClientData is Challenge(purpose, payload).
	AuthData   enc.Bytes `json:"authData,omitempty"`
	ClientData enc.Bytes `json:"clientData,omitempty"`
}

// RP names the relying party for passkeys.
type RP struct {
	ID      string   `json:"id"`      // e.g. "ngc.example.org"
	Origins []string `json:"origins,omitempty"` // e.g. ["https://ngc.example.org"]
}

// Challenge returns the WebAuthn challenge for a payload.
func Challenge(purpose string, payload []byte) []byte {
	h := sha256.Sum256(sign.Message(purpose, payload))
	return h[:]
}

// Verify checks that p is a proof by key over payload for purpose.
func Verify(p Proof, key UserKey, rp RP, purpose string, payload []byte) error {
	if p.KeyID != key.ID || p.Kind != key.Kind {
		return ErrKeyMismatch
	}
	switch key.Kind {
	case KindPassword:
		if key.Alg != AlgEdDSA || len(key.Public) != ed25519.PublicKeySize {
			return ErrKey
		}
		if !ed25519.Verify(ed25519.PublicKey(key.Public), sign.Message(purpose, payload), p.Sig) {
			return ErrSignature
		}
		return nil
	case KindPasskey:
		return verifyAssertion(p, key, rp, Challenge(purpose, payload))
	}
	return ErrKey
}

type clientData struct {
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin"`
}

const (
	flagUP = 0x01 // user present
	flagUV = 0x04 // user verified (PIN or biometrics)
)

func verifyAssertion(p Proof, key UserKey, rp RP, challenge []byte) error {
	var cd clientData
	if len(p.ClientData) > 4096 || json.Unmarshal(p.ClientData, &cd) != nil {
		return ErrClientData
	}
	if cd.Type != "webauthn.get" || cd.CrossOrigin ||
		cd.Challenge != enc.B64.EncodeToString(challenge) || !slices.Contains(rp.Origins, cd.Origin) {
		return ErrClientData
	}
	if len(p.AuthData) < 37 || len(p.AuthData) > 4096 {
		return ErrAuthData
	}
	rpHash := sha256.Sum256([]byte(rp.ID))
	if !bytes.Equal(p.AuthData[:32], rpHash[:]) {
		return ErrAuthData
	}
	if flags := p.AuthData[32]; flags&flagUP == 0 || flags&flagUV == 0 {
		return ErrNoUV
	}
	cdHash := sha256.Sum256(p.ClientData)
	signed := make([]byte, 0, len(p.AuthData)+len(cdHash))
	signed = append(append(signed, p.AuthData...), cdHash[:]...)
	pub, err := parsePublic(key)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(signed)
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		if !ecdsa.VerifyASN1(k, digest[:], p.Sig) {
			return ErrSignature
		}
	case ed25519.PublicKey:
		if !ed25519.Verify(k, signed, p.Sig) {
			return ErrSignature
		}
	case *rsa.PublicKey:
		if rsa.VerifyPKCS1v15(k, crypto.SHA256, digest[:], p.Sig) != nil {
			return ErrSignature
		}
	default:
		return ErrKey
	}
	return nil
}

func parsePublic(k UserKey) (crypto.PublicKey, error) {
	pub, err := x509.ParsePKIXPublicKey(k.Public)
	if err != nil {
		return nil, ErrKey
	}
	switch key := pub.(type) {
	case *ecdsa.PublicKey:
		if k.Alg != AlgES256 || key.Curve != elliptic.P256() {
			return nil, ErrKey
		}
	case ed25519.PublicKey:
		if k.Alg != AlgEdDSA {
			return nil, ErrKey
		}
	case *rsa.PublicKey:
		if k.Alg != AlgRS256 || key.N.BitLen() < 2048 {
			return nil, ErrKey
		}
	default:
		return nil, ErrKey
	}
	return pub, nil
}
```

- [ ] **Step 5: Vektoren erzeugen und Tests laufen lassen**

Run:

```bash
go mod tidy && go mod vendor
go test ./contracts/proof/ -run 'TestKDFVectors|TestProofVectors' -update
go test ./contracts/proof/
go test -run=XXX -fuzz=FuzzVerifyPasskey -fuzztime=30s ./contracts/proof/
```

Expected: `ok`; beide Golden-Dateien angelegt; Fuzzing ohne Fund.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum vendor THIRD-PARTY.md contracts/proof contracts/testdata/proof.json contracts/testdata/proof_kdf.json
git commit -m "Add user proofs with password keys and passkeys" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 6: Frames und Kanäle (`contracts/wire`)

Ein Frame ist die Einheit auf jeder Leitung (Browser ↔ API, API ↔ Controller). Jeder Frame ist der
Klartext genau einer Noise-Transportnachricht. Kanal 0 ist der Steuerkanal und immer offen.
Kanäle öffnet nur eine Seite je Leitung: der Browser zur API, die API zum Controller.
Kern-Apps (App-IDs mit `ngc.`, etwa `ngc.users`) bedient die API selbst; ihre Kanäle haben keinen
eigenen Ende-zu-Ende-Schlüsselaustausch, weil die Sitzung zur API schon so verschlüsselt ist.

Ablauf eines Kanals: OPEN (mit Noise-Nachricht 1 für Ende zu Ende und der Freigabe, die der Öffner
gewährt) → erste GRANT der Gegenseite (Freigabe plus Noise-Nachricht 2; das heißt „angenommen“) oder
CLOSE mit Fehler → DATA in beide Richtungen, jeweils höchstens so viele Bytes wie freigegeben → EOF
von jeder Seite, die fertig ist → CLOSE vom Dienst-Ende.

**Files:**
- Create: `contracts/wire/wire.go`, `contracts/wire/wire_test.go`, `contracts/testdata/wire.json` (per `-update`)

**Interfaces:**
- Consumes: `enc.Bytes`, `enc.DecodeStrict`, `sign.Envelope`, `proof.Proof`, `golden.Check`.
- Produces:
  - `type wire.Type uint8`: `TypeOpen = 1`, `TypeData = 2`, `TypeEOF = 3`, `TypeClose = 4`, `TypeGrant = 5`
  - Konstanten `HeaderSize = 5`, `MaxFrame = 65519`, `MaxPayload`, `ControlChannel = 0`, `DefaultWindow = 256 << 10`,
    `MinWindow = 4 << 10`, `MaxChannels = 64`, `MaxOpenPayload = 32 << 10`, `MaxBrowserOpen = 16 << 10`,
    `MaxClosePayload = 4 << 10`, `MaxParams = 8 << 10`, `MaxMessageText = 256`, `MinHandshake = 48`, `MaxHandshake = 1024`
  - `type wire.Frame struct{ Type Type; Channel uint32; Payload []byte }`, `(Frame) Encode() ([]byte, error)`, `wire.Decode([]byte) (Frame, error)`, `wire.ErrFrame`
  - `wire.Grant(credit uint32, handshake []byte) []byte`, `wire.ParseGrant(p []byte) (uint32, []byte, error)`
  - `wire.ValidAppID(string) bool`, `wire.ValidName(string) bool`, `wire.CoreApp(string) bool` (Präfix `ngc.`),
    `wire.ValidParams(json.RawMessage) bool` (leer oder JSON-Objekt bis `MaxParams`)
  - `type wire.Open struct{ App, Service, Action string; Params json.RawMessage; Window uint32; E2E enc.Bytes; Task *sign.Envelope; Confirm *proof.Proof }`
    (JSON `app`, `service`, `action`, `params`, `window`, `e2e`, `task`, `confirm`), `(Open) Validate() error`,
    `wire.ParseOpen(p []byte) (Open, error)` (Browser-Grenze 16 KiB), `wire.ErrOpen`
  - `type wire.Status string` (`StatusOK = "ok"`, `StatusError = "error"`), `type wire.Code string` mit
    `CodeForbidden`, `CodeUnreachable`, `CodeExit`, `CodeTimeout`, `CodeLimit`, `CodeProtocol`, `CodeConfirm`,
    `CodeLicense`, `CodeNotFound`, `CodeVerify`, `CodeInternal`; `(Code) Message() string`
  - `type wire.Close struct{ Status Status; Code Code; Message string; ExitCode *int }` (JSON `status`, `code`, `message`, `exitCode`),
    `(Close) Validate() error`, `wire.ParseClose(p []byte) (Close, error)`, `wire.Error(code Code) Close`, `wire.ErrClose`
  - `type wire.Control struct{ T string; B json.RawMessage }` (JSON `t`, `b`), Typen `CtlHello`, `CtlTrust`, `CtlNotify`,
    `CtlStatus`, `CtlUpdate`, `CtlBye`; `wire.ParseControl(p []byte) (Control, error)`, `wire.ErrControl`

- [ ] **Step 1: Test schreiben**

`contracts/wire/wire_test.go`:

```go
package wire

import (
	"bytes"
	"encoding/json"
	"flag"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

func testOpen() Open {
	return Open{
		App:     "org.sorglos.terminal",
		Service: "terminal",
		Action:  "shell",
		Params:  json.RawMessage(`{"cols":80,"rows":24}`),
		Window:  DefaultWindow,
		E2E:     bytes.Repeat([]byte{7}, MinHandshake),
	}
}

func TestFrameRoundTrip(t *testing.T) {
	open, _ := json.Marshal(testOpen())
	closeOK, _ := json.Marshal(Close{Status: StatusOK})
	for _, f := range []Frame{
		{Type: TypeOpen, Channel: 1, Payload: open},
		{Type: TypeData, Channel: 1, Payload: []byte("ls -la\n")},
		{Type: TypeData, Channel: ControlChannel, Payload: []byte(`{"t":"bye"}`)},
		{Type: TypeEOF, Channel: 1},
		{Type: TypeClose, Channel: 1, Payload: closeOK},
		{Type: TypeGrant, Channel: 1, Payload: Grant(DefaultWindow, nil)},
	} {
		b, err := f.Encode()
		if err != nil {
			t.Fatalf("encode %v: %v", f.Type, err)
		}
		g, err := Decode(b)
		if err != nil {
			t.Fatalf("decode %v: %v", f.Type, err)
		}
		if g.Type != f.Type || g.Channel != f.Channel || !bytes.Equal(g.Payload, f.Payload) {
			t.Fatalf("round trip %v: %+v", f.Type, g)
		}
	}
}

func TestFrameRules(t *testing.T) {
	bad := []Frame{
		{Type: 9, Channel: 1},
		{Type: TypeOpen, Channel: ControlChannel, Payload: []byte("{}")},
		{Type: TypeOpen, Channel: 1},
		{Type: TypeEOF, Channel: 1, Payload: []byte("x")},
		{Type: TypeEOF, Channel: ControlChannel},
		{Type: TypeClose, Channel: 1},
		{Type: TypeGrant, Channel: 1, Payload: []byte{1, 2}},
		{Type: TypeData, Channel: 1, Payload: make([]byte, MaxPayload+1)},
	}
	for _, f := range bad {
		if _, err := f.Encode(); err != ErrFrame {
			t.Errorf("%+v: got %v", f.Type, err)
		}
	}
	if _, err := Decode([]byte{2, 0, 0}); err != ErrFrame {
		t.Errorf("short frame: %v", err)
	}
}

func TestGrant(t *testing.T) {
	hs := bytes.Repeat([]byte{1}, MinHandshake)
	credit, got, err := ParseGrant(Grant(4096, hs))
	if err != nil || credit != 4096 || !bytes.Equal(got, hs) {
		t.Fatalf("got %d %x %v", credit, got, err)
	}
	if _, _, err := ParseGrant(Grant(1, []byte{1, 2, 3})); err != ErrFrame {
		t.Errorf("short handshake: %v", err)
	}
}

func TestParseOpen(t *testing.T) {
	good, _ := json.Marshal(testOpen())
	if _, err := ParseOpen(good); err != nil {
		t.Fatalf("valid: %v", err)
	}
	mutations := map[string]func(*Open){
		"app id":      func(o *Open) { o.App = "Terminal" },
		"service":     func(o *Open) { o.Service = "a b" },
		"params list": func(o *Open) { o.Params = json.RawMessage(`[1]`) },
		"params null": func(o *Open) { o.Params = json.RawMessage(`null`) },
		"window":      func(o *Open) { o.Window = 10 },
		"e2e":         func(o *Open) { o.E2E = []byte{1} },
	}
	for name, mutate := range mutations {
		o := testOpen()
		mutate(&o)
		b, _ := json.Marshal(o)
		if _, err := ParseOpen(b); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := ParseOpen([]byte(`{"app":"a.b","extra":1}`)); err == nil {
		t.Error("unknown field accepted")
	}
	core := testOpen()
	core.App, core.E2E = "ngc.users", nil
	b, _ := json.Marshal(core)
	if _, err := ParseOpen(b); err != nil {
		t.Errorf("core app: %v", err)
	}
	core.E2E = bytes.Repeat([]byte{7}, MinHandshake)
	b, _ = json.Marshal(core)
	if _, err := ParseOpen(b); err == nil {
		t.Error("core app with end-to-end handshake accepted")
	}
}

func TestParseClose(t *testing.T) {
	code := 2
	for _, c := range []Close{{Status: StatusOK}, Error(CodeForbidden), {Status: StatusError, Code: CodeExit, Message: "Dienst mit Code 2 beendet.", ExitCode: &code}} {
		b, _ := json.Marshal(c)
		if _, err := ParseClose(b); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
	for _, s := range []string{`{"status":"ok","code":"E_EXIT"}`, `{"status":"error","code":"E_WHAT"}`, `{"status":"maybe"}`} {
		if _, err := ParseClose([]byte(s)); err == nil {
			t.Errorf("%s: accepted", s)
		}
	}
	if Error(CodeForbidden).Message != "Keine Berechtigung." {
		t.Error("German message missing")
	}
}

func TestParseControl(t *testing.T) {
	if _, err := ParseControl([]byte(`{"t":"hello","b":{"version":"1.0.0"}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseControl([]byte(`{"t":"shutdown"}`)); err != ErrControl {
		t.Errorf("unknown type: %v", err)
	}
}

type frameVector struct {
	Name    string    `json:"name"`
	Type    Type      `json:"type"`
	Channel uint32    `json:"channel"`
	Payload enc.Bytes `json:"payload"`
	Encoded enc.Bytes `json:"encoded"`
}

func TestVectors(t *testing.T) {
	open, _ := json.Marshal(testOpen())
	var vs []frameVector
	for _, f := range []struct {
		name string
		f    Frame
	}{
		{"open", Frame{Type: TypeOpen, Channel: 1, Payload: open}},
		{"data", Frame{Type: TypeData, Channel: 1, Payload: []byte("ls\n")}},
		{"eof", Frame{Type: TypeEOF, Channel: 1}},
		{"grant with handshake", Frame{Type: TypeGrant, Channel: 1, Payload: Grant(DefaultWindow, bytes.Repeat([]byte{9}, MinHandshake))}},
		{"control", Frame{Type: TypeData, Channel: 0, Payload: []byte(`{"t":"bye"}`)}},
	} {
		b, err := f.f.Encode()
		if err != nil {
			t.Fatal(err)
		}
		vs = append(vs, frameVector{Name: f.name, Type: f.f.Type, Channel: f.f.Channel, Payload: f.f.Payload, Encoded: b})
	}
	golden.Check(t, *update, "wire.json", vs)
}

func FuzzDecode(f *testing.F) {
	f.Add([]byte{1, 0, 0, 0, 1, '{', '}'})
	f.Fuzz(func(t *testing.T, b []byte) {
		fr, err := Decode(b)
		if err != nil {
			return
		}
		out, err := fr.Encode()
		if err != nil || !bytes.Equal(out, b) {
			t.Fatalf("re-encode differs: %v", err)
		}
		switch fr.Type {
		case TypeOpen:
			_, _ = ParseOpen(fr.Payload)
		case TypeClose:
			_, _ = ParseClose(fr.Payload)
		case TypeGrant:
			_, _, _ = ParseGrant(fr.Payload)
		}
	})
}
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

Run: `go test ./contracts/wire/`
Expected: FAIL, `undefined: Open`

- [ ] **Step 3: Umsetzung schreiben**

`contracts/wire/wire.go`:

```go
// Package wire defines frames, the unit of transfer between browser, API and
// controller. Every frame is the plaintext of one Noise transport message;
// channel data is end-to-end encrypted in addition (see package noisep).
package wire

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
)

// Type is the kind of a frame.
type Type uint8

const (
	TypeOpen  Type = 1 // opens a channel; payload: Open as JSON
	TypeData  Type = 2 // channel data; on channel 0: Control as JSON
	TypeEOF   Type = 3 // the sender will send no more data; empty payload
	TypeClose Type = 4 // closes the channel; payload: Close as JSON
	TypeGrant Type = 5 // credit (uint32, big endian); the first GRANT also carries the end-to-end handshake reply
)

// Limits.
const (
	HeaderSize      = 5
	MaxFrame        = 65519 // largest plaintext of one Noise transport message (65535 - 16)
	MaxPayload      = MaxFrame - HeaderSize
	ControlChannel  = 0
	DefaultWindow   = 256 << 10
	MinWindow       = 4 << 10
	MaxChannels     = 64 // open channels per link and session
	MaxOpenPayload  = 32 << 10
	MaxBrowserOpen  = 16 << 10
	MaxClosePayload = 4 << 10
	MaxParams       = 8 << 10
	MaxMessageText  = 256
	MinHandshake    = 48 // Noise_KK message without payload: 32-byte ephemeral key and 16-byte tag
	MaxHandshake    = 1024
)

// Frame is one unit on a link.
type Frame struct {
	Type    Type
	Channel uint32
	Payload []byte
}

var (
	ErrFrame   = errors.New("wire: malformed frame")
	ErrOpen    = errors.New("wire: malformed OPEN payload")
	ErrClose   = errors.New("wire: malformed CLOSE payload")
	ErrControl = errors.New("wire: unknown control message")
)

// Encode serializes f after checking the rules for its type.
func (f Frame) Encode() ([]byte, error) {
	if err := f.check(); err != nil {
		return nil, err
	}
	b := make([]byte, HeaderSize+len(f.Payload))
	b[0] = byte(f.Type)
	binary.BigEndian.PutUint32(b[1:HeaderSize], f.Channel)
	copy(b[HeaderSize:], f.Payload)
	return b, nil
}

// Decode parses one frame and checks the rules for its type.
func Decode(b []byte) (Frame, error) {
	if len(b) < HeaderSize || len(b) > MaxFrame {
		return Frame{}, ErrFrame
	}
	f := Frame{Type: Type(b[0]), Channel: binary.BigEndian.Uint32(b[1:HeaderSize]), Payload: b[HeaderSize:]}
	if err := f.check(); err != nil {
		return Frame{}, err
	}
	return f, nil
}

func (f Frame) check() error {
	if len(f.Payload) > MaxPayload {
		return ErrFrame
	}
	switch f.Type {
	case TypeData:
		return nil
	case TypeOpen:
		if f.Channel == ControlChannel || len(f.Payload) == 0 || len(f.Payload) > MaxOpenPayload {
			return ErrFrame
		}
	case TypeEOF:
		if f.Channel == ControlChannel || len(f.Payload) != 0 {
			return ErrFrame
		}
	case TypeClose:
		if f.Channel == ControlChannel || len(f.Payload) == 0 || len(f.Payload) > MaxClosePayload {
			return ErrFrame
		}
	case TypeGrant:
		if f.Channel == ControlChannel || len(f.Payload) < 4 {
			return ErrFrame
		}
	default:
		return ErrFrame
	}
	return nil
}

// Grant builds the payload of a GRANT frame.
func Grant(credit uint32, handshake []byte) []byte {
	b := make([]byte, 4, 4+len(handshake))
	binary.BigEndian.PutUint32(b, credit)
	return append(b, handshake...)
}

// ParseGrant splits a GRANT payload into credit and the optional handshake reply.
func ParseGrant(p []byte) (uint32, []byte, error) {
	if len(p) < 4 {
		return 0, nil, ErrFrame
	}
	hs := p[4:]
	if len(hs) != 0 && (len(hs) < MinHandshake || len(hs) > MaxHandshake) {
		return 0, nil, ErrFrame
	}
	return binary.BigEndian.Uint32(p[:4]), hs, nil
}

var (
	appIDPattern = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9-]+)+$`)
	namePattern  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
)

// ValidAppID reports whether s is a well-formed app id: reverse domain, at most 100 characters.
func ValidAppID(s string) bool { return len(s) <= 100 && appIDPattern.MatchString(s) }

// ValidName reports whether s is a well-formed service or action name.
func ValidName(s string) bool { return namePattern.MatchString(s) }

// CoreApp reports whether app belongs to NGC itself. The API serves these
// channels; they carry no end-to-end handshake of their own because the
// session to the API is already encrypted end to end.
func CoreApp(app string) bool { return strings.HasPrefix(app, "ngc.") }

// Open is the payload of an OPEN frame from the browser. The API forwards it
// to the controller with the fields of tunnel.Open added.
type Open struct {
	App     string          `json:"app"`
	Service string          `json:"service"`
	Action  string          `json:"action"`
	Params  json.RawMessage `json:"params,omitempty"` // JSON object, at most MaxParams bytes
	Window  uint32          `json:"window"`           // credit the opener grants the other end
	E2E     enc.Bytes       `json:"e2e,omitempty"`    // Noise_KK message 1 of the end-to-end handshake; empty for core apps
	Task    *sign.Envelope  `json:"task,omitempty"`   // signed task (package task), for root tasks and re-confirmation
	Confirm *proof.Proof    `json:"confirm,omitempty"`
}

// ParseOpen decodes and validates an OPEN payload from the browser.
func ParseOpen(p []byte) (Open, error) {
	var o Open
	if err := enc.DecodeStrict(p, MaxBrowserOpen, &o); err != nil {
		return Open{}, err
	}
	return o, o.Validate()
}

// Validate checks the fields of o.
func (o Open) Validate() error {
	if !ValidAppID(o.App) || !ValidName(o.Service) || !ValidName(o.Action) {
		return ErrOpen
	}
	if !ValidParams(o.Params) {
		return ErrOpen
	}
	if o.Window < MinWindow || len(o.E2E) > MaxHandshake || CoreApp(o.App) != (len(o.E2E) == 0) ||
		(len(o.E2E) != 0 && len(o.E2E) < MinHandshake) {
		return ErrOpen
	}
	return nil
}

// ValidParams reports whether raw is empty or a JSON object of at most MaxParams bytes.
func ValidParams(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	t := bytes.TrimSpace(raw)
	if len(raw) > MaxParams || len(t) == 0 || t[0] != '{' {
		return false
	}
	var m map[string]json.RawMessage
	return json.Unmarshal(t, &m) == nil
}

// Status of a closed channel.
type Status string

const (
	StatusOK    Status = "ok"
	StatusError Status = "error"
)

// Code names an error.
type Code string

const (
	CodeForbidden   Code = "E_FORBIDDEN"
	CodeUnreachable Code = "E_UNREACHABLE"
	CodeExit        Code = "E_EXIT"
	CodeTimeout     Code = "E_TIMEOUT"
	CodeLimit       Code = "E_LIMIT"
	CodeProtocol    Code = "E_PROTOCOL"
	CodeConfirm     Code = "E_CONFIRM"
	CodeLicense     Code = "E_LICENSE"
	CodeNotFound    Code = "E_NOTFOUND"
	CodeVerify      Code = "E_VERIFY"
	CodeInternal    Code = "E_INTERNAL"
)

var messages = map[Code]string{
	CodeForbidden:   "Keine Berechtigung.",
	CodeUnreachable: "Dienst nicht erreichbar.",
	CodeExit:        "Dienst mit Fehler beendet.",
	CodeTimeout:     "Zeitlimit überschritten.",
	CodeLimit:       "Zu viele offene Verbindungen.",
	CodeProtocol:    "Ungültige Nachricht.",
	CodeConfirm:     "Bitte erneut bestätigen.",
	CodeLicense:     "Keine gültige Lizenz.",
	CodeNotFound:    "Nicht gefunden.",
	CodeVerify:      "Unterschrift ungültig.",
	CodeInternal:    "Interner Fehler.",
}

// Message returns the German text shown to users.
func (c Code) Message() string { return messages[c] }

// Close is the payload of a CLOSE frame.
type Close struct {
	Status   Status `json:"status"`
	Code     Code   `json:"code,omitempty"`
	Message  string `json:"message,omitempty"`
	ExitCode *int   `json:"exitCode,omitempty"`
}

// Error returns a CLOSE payload for code with its German message.
func Error(code Code) Close {
	return Close{Status: StatusError, Code: code, Message: code.Message()}
}

// ParseClose decodes and validates a CLOSE payload.
func ParseClose(p []byte) (Close, error) {
	var c Close
	if err := enc.DecodeStrict(p, MaxClosePayload, &c); err != nil {
		return Close{}, err
	}
	return c, c.Validate()
}

// Validate checks the fields of c.
func (c Close) Validate() error {
	switch c.Status {
	case StatusOK:
		if c.Code != "" {
			return ErrClose
		}
	case StatusError:
		if _, ok := messages[c.Code]; !ok {
			return ErrClose
		}
	default:
		return ErrClose
	}
	if len(c.Message) > MaxMessageText || (c.ExitCode != nil && (*c.ExitCode < 0 || *c.ExitCode > 255)) {
		return ErrClose
	}
	return nil
}

// Control is a message on channel 0.
type Control struct {
	T string          `json:"t"`
	B json.RawMessage `json:"b,omitempty"`
}

// Control message types.
const (
	CtlHello  = "hello"  // first message on a link (package tunnel: Hello)
	CtlTrust  = "trust"  // a new trust list version (trust.Signed)
	CtlNotify = "notify" // notice for the user, e.g. an update is available
	CtlStatus = "status" // controller status report
	CtlUpdate = "update" // update instruction or progress
	CtlBye    = "bye"    // the sender closes the link
)

// ParseControl decodes a control message and checks its type.
func ParseControl(p []byte) (Control, error) {
	var c Control
	if err := enc.DecodeStrict(p, MaxPayload, &c); err != nil {
		return Control{}, err
	}
	switch c.T {
	case CtlHello, CtlTrust, CtlNotify, CtlStatus, CtlUpdate, CtlBye:
		return c, nil
	}
	return Control{}, ErrControl
}
```

- [ ] **Step 4: Vektoren erzeugen und Tests laufen lassen**

Run:

```bash
go test ./contracts/wire/ -run TestVectors -update
go test ./contracts/wire/
go test -run=XXX -fuzz=FuzzDecode -fuzztime=30s ./contracts/wire/
```

Expected: `ok`; `contracts/testdata/wire.json` angelegt; Fuzzing ohne Fund.

- [ ] **Step 5: Commit**

```bash
git add contracts/wire contracts/testdata/wire.json
git commit -m "Add frames, channels and error codes" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 7: Noise-Profil (`contracts/noisep`)

NGC nutzt Noise immer als Noise_KK_25519_AESGCM_SHA256: Beide Seiten kennen den statischen
Schlüssel der Gegenseite vorab. Drei Verwendungen, unterschieden am Prolog:

| Verwendung | Initiator | Statischer Schlüssel Initiator | Statischer Schlüssel Responder | Prolog |
|---|---|---|---|---|
| Sitzung Browser ↔ API | Browser | Sitzungsschlüssel `dh` aus der Anmeldung | `APICert.key` | `ngc/session/v1` |
| Tunnel Controller ↔ API | Controller | Controller-Schlüssel aus der Vertrauensliste | API-Schlüssel aus der Vertrauensliste | `ngc/tunnel/v1` |
| Ende zu Ende Shell ↔ Controller | Shell | Sitzungsschlüssel `dh` | Controller-Schlüssel aus der Vertrauensliste | `E2EPrologue(...)` |

Nachricht 1 (`-> e, es, ss`) geht bei Sitzung und Tunnel als erste WebSocket-Nachricht, beim
Ende-zu-Ende-Kanal im Feld `e2e` des OPEN. Nachricht 2 (`<- e, ee, se`) kommt als zweite
WebSocket-Nachricht beziehungsweise in der ersten GRANT. Danach ist jede WebSocket-Nachricht genau
eine Transportnachricht mit einem Frame als Klartext; auf dem Ende-zu-Ende-Kanal ist jede
DATA-Nutzlast genau eine Transportnachricht.

**Files:**
- Create: `contracts/noisep/noisep.go`, `contracts/noisep/noisep_test.go`, `contracts/testdata/noise.json` (per `-update`)
- Modify: `go.mod`, `go.sum`, `vendor/`, `THIRD-PARTY.md`

**Interfaces:**
- Consumes: `github.com/flynn/noise` v1.1.0.
- Produces:
  - `noisep.Suite` (`noise.CipherSuite`), Konstanten `PrologueSession`, `PrologueTunnel`, `PrologueE2E`,
    `MaxMessage = 65535`, `TagSize = 16`, `MaxPlaintext = 65519`
  - `noisep.E2EPrologue(controllerID, app, service, action string) []byte`
  - `noisep.Keypair(rng io.Reader) (noise.DHKey, error)`
  - `noisep.Handshake(initiator bool, prologue []byte, static noise.DHKey, peer []byte, rng io.Reader) (*noise.HandshakeState, error)`
  - `type noisep.Transport`, `noisep.NewTransport(initiator bool, cs1, cs2 *noise.CipherState) *Transport`,
    `(*Transport) Seal([]byte) ([]byte, error)`, `(*Transport) Open([]byte) ([]byte, error)`, `noisep.ErrTooLarge`

- [ ] **Step 1: Test schreiben**

`contracts/noisep/noisep_test.go`:

```go
package noisep

import (
	"bytes"
	"crypto/rand"
	"flag"
	"testing"

	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

func fixedKey(t *testing.T, fill byte) noise.DHKey {
	t.Helper()
	k, err := Keypair(bytes.NewReader(bytes.Repeat([]byte{fill}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// pair runs a full Noise_KK handshake and returns both transports and the two handshake messages.
func pair(t *testing.T, prologue []byte, is, ie, rs, re noise.DHKey, p1, p2 []byte) (*Transport, *Transport, []byte, []byte) {
	t.Helper()
	init, err := newHandshake(true, prologue, is, rs.Public, rand.Reader, ie)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := newHandshake(false, prologue, rs, is.Public, rand.Reader, re)
	if err != nil {
		t.Fatal(err)
	}
	msg1, _, _, err := init.WriteMessage(nil, p1)
	if err != nil {
		t.Fatal(err)
	}
	got1, _, _, err := resp.ReadMessage(nil, msg1)
	if err != nil || !bytes.Equal(got1, p1) {
		t.Fatalf("read msg1: %v", err)
	}
	msg2, rcs1, rcs2, err := resp.WriteMessage(nil, p2)
	if err != nil {
		t.Fatal(err)
	}
	got2, ics1, ics2, err := init.ReadMessage(nil, msg2)
	if err != nil || !bytes.Equal(got2, p2) {
		t.Fatalf("read msg2: %v", err)
	}
	return NewTransport(true, ics1, ics2), NewTransport(false, rcs1, rcs2), msg1, msg2
}

func TestHandshakeAndTransport(t *testing.T) {
	var ie, re noise.DHKey
	it, rt, _, _ := pair(t, []byte(PrologueTunnel), fixedKey(t, 1), ie, fixedKey(t, 2), re, nil, nil)
	for _, msg := range []string{"hello", "", "world"} {
		c, err := it.Seal([]byte(msg))
		if err != nil {
			t.Fatal(err)
		}
		p, err := rt.Open(c)
		if err != nil || string(p) != msg {
			t.Fatalf("initiator → responder %q: %v", msg, err)
		}
		c, _ = rt.Seal([]byte(msg))
		if p, err := it.Open(c); err != nil || string(p) != msg {
			t.Fatalf("responder → initiator %q: %v", msg, err)
		}
	}
	if _, err := it.Seal(make([]byte, MaxPlaintext+1)); err != ErrTooLarge {
		t.Errorf("too large: %v", err)
	}
}

func TestWrongPrologueOrKeyFails(t *testing.T) {
	is, rs, other := fixedKey(t, 1), fixedKey(t, 2), fixedKey(t, 3)
	init, _ := Handshake(true, []byte(PrologueSession), is, rs.Public, rand.Reader)
	msg1, _, _, _ := init.WriteMessage(nil, nil)
	resp, _ := Handshake(false, []byte(PrologueTunnel), rs, is.Public, rand.Reader)
	if _, _, _, err := resp.ReadMessage(nil, msg1); err == nil {
		t.Error("wrong prologue accepted")
	}
	resp, _ = Handshake(false, []byte(PrologueSession), rs, other.Public, rand.Reader)
	if _, _, _, err := resp.ReadMessage(nil, msg1); err == nil {
		t.Error("wrong initiator key accepted")
	}
}

func TestE2EPrologue(t *testing.T) {
	got := string(E2EPrologue("ctl-svc", "org.sorglos.terminal", "terminal", "shell"))
	want := "ngc/e2e/v1\x00ctl-svc\x00org.sorglos.terminal\x00terminal\x00shell"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

type keyVector struct {
	Private enc.Bytes `json:"private"`
	Public  enc.Bytes `json:"public"`
}

type transportVector struct {
	From       string    `json:"from"`
	Plaintext  enc.Bytes `json:"plaintext"`
	Ciphertext enc.Bytes `json:"ciphertext"`
}

type noiseVector struct {
	Protocol      string            `json:"protocol"`
	Prologue      enc.Bytes         `json:"prologue"`
	InitStatic    keyVector         `json:"initStatic"`
	InitEphemeral keyVector         `json:"initEphemeral"`
	RespStatic    keyVector         `json:"respStatic"`
	RespEphemeral keyVector         `json:"respEphemeral"`
	Msg1Payload   enc.Bytes         `json:"msg1Payload"`
	Msg1          enc.Bytes         `json:"msg1"`
	Msg2Payload   enc.Bytes         `json:"msg2Payload"`
	Msg2          enc.Bytes         `json:"msg2"`
	Transport     []transportVector `json:"transport"`
}

func kv(k noise.DHKey) keyVector { return keyVector{Private: k.Private, Public: k.Public} }

func TestVectors(t *testing.T) {
	is, ie, rs, re := fixedKey(t, 1), fixedKey(t, 2), fixedKey(t, 3), fixedKey(t, 4)
	prologue := E2EPrologue("ctl-svc", "org.sorglos.terminal", "terminal", "shell")
	it, rt, msg1, msg2 := pair(t, prologue, is, ie, rs, re, []byte("m1"), []byte("m2"))
	v := noiseVector{
		Protocol: "Noise_KK_25519_AESGCM_SHA256", Prologue: prologue,
		InitStatic: kv(is), InitEphemeral: kv(ie), RespStatic: kv(rs), RespEphemeral: kv(re),
		Msg1Payload: []byte("m1"), Msg1: msg1, Msg2Payload: []byte("m2"), Msg2: msg2,
	}
	for _, s := range []struct {
		from string
		tr   *Transport
		msg  string
	}{{"initiator", it, "ls -la\n"}, {"responder", rt, "total 0\n"}, {"initiator", it, ""}} {
		c, err := s.tr.Seal([]byte(s.msg))
		if err != nil {
			t.Fatal(err)
		}
		v.Transport = append(v.Transport, transportVector{From: s.from, Plaintext: []byte(s.msg), Ciphertext: c})
	}
	golden.Check(t, *update, "noise.json", v)
}
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

Run: `go test ./contracts/noisep/`
Expected: FAIL, `undefined: Keypair` (und fehlendes Modul `github.com/flynn/noise`)

- [ ] **Step 3: Noise übernehmen**

```bash
go get github.com/flynn/noise@v1.1.0
```

In `THIRD-PARTY.md` eine Zeile ergänzen:

```
| github.com/flynn/noise | v1.1.0 | BSD-3-Clause | Noise_KK für Sitzung, Tunnel und Ende zu Ende | 2026-09-19 |
```

- [ ] **Step 4: Umsetzung schreiben**

`contracts/noisep/noisep.go`:

```go
// Package noisep fixes how NGC uses the Noise protocol framework: always
// Noise_KK_25519_AESGCM_SHA256, for three uses told apart by the prologue.
package noisep

import (
	"errors"
	"io"

	"github.com/flynn/noise"
)

// Suite is X25519, AES-256-GCM and SHA-256.
var Suite = noise.NewCipherSuite(noise.DH25519, noise.CipherAESGCM, noise.HashSHA256)

// Prologues and sizes.
const (
	PrologueSession = "ngc/session/v1" // browser ↔ API, the browser initiates
	PrologueTunnel  = "ngc/tunnel/v1"  // controller ↔ API, the controller initiates
	PrologueE2E     = "ngc/e2e/v1"     // shell ↔ controller per channel, the shell initiates; see E2EPrologue
	MaxMessage      = 65535
	TagSize         = 16
	MaxPlaintext    = MaxMessage - TagSize
)

// ErrTooLarge reports a message above the Noise limit.
var ErrTooLarge = errors.New("noisep: message too large")

// E2EPrologue binds an end-to-end handshake to exactly one channel target:
// "ngc/e2e/v1" 0x00 controllerID 0x00 app 0x00 service 0x00 action.
func E2EPrologue(controllerID, app, service, action string) []byte {
	b := []byte(PrologueE2E)
	for _, s := range []string{controllerID, app, service, action} {
		b = append(b, 0)
		b = append(b, s...)
	}
	return b
}

// Keypair creates an X25519 key pair. rng is crypto/rand.Reader outside tests.
func Keypair(rng io.Reader) (noise.DHKey, error) { return Suite.GenerateKeypair(rng) }

// Handshake starts a Noise_KK handshake: static is the own key pair, peer the
// static public key of the other side.
func Handshake(initiator bool, prologue []byte, static noise.DHKey, peer []byte, rng io.Reader) (*noise.HandshakeState, error) {
	return newHandshake(initiator, prologue, static, peer, rng, noise.DHKey{})
}

// newHandshake allows a fixed ephemeral key for test vectors.
func newHandshake(initiator bool, prologue []byte, static noise.DHKey, peer []byte, rng io.Reader, eph noise.DHKey) (*noise.HandshakeState, error) {
	return noise.NewHandshakeState(noise.Config{
		CipherSuite:      Suite,
		Random:           rng,
		Pattern:          noise.HandshakeKK,
		Initiator:        initiator,
		Prologue:         prologue,
		StaticKeypair:    static,
		PeerStatic:       peer,
		EphemeralKeypair: eph,
	})
}

// Transport encrypts and decrypts messages after the handshake.
type Transport struct {
	send, recv *noise.CipherState
}

// NewTransport sorts the cipher states the handshake returned: cs1 carries
// initiator → responder, cs2 responder → initiator.
func NewTransport(initiator bool, cs1, cs2 *noise.CipherState) *Transport {
	if initiator {
		return &Transport{send: cs1, recv: cs2}
	}
	return &Transport{send: cs2, recv: cs1}
}

// Seal encrypts one message.
func (t *Transport) Seal(plaintext []byte) ([]byte, error) {
	if len(plaintext) > MaxPlaintext {
		return nil, ErrTooLarge
	}
	return t.send.Encrypt(nil, nil, plaintext)
}

// Open decrypts one message.
func (t *Transport) Open(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) > MaxMessage {
		return nil, ErrTooLarge
	}
	return t.recv.Decrypt(nil, nil, ciphertext)
}
```

- [ ] **Step 5: Vendoren, Vektor erzeugen, testen**

Run:

```bash
go mod tidy && go mod vendor
go test ./contracts/noisep/ -run TestVectors -update
go test ./contracts/noisep/
```

Expected: `ok`; `contracts/testdata/noise.json` angelegt. Den Vektor danach einmal gegenlesen: die
Nachrichten 1 und 2 müssen je 32 + 2 + 16 Byte lang sein (Ephemeral, Nutzlast `m1`/`m2`, Tag).

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum vendor THIRD-PARTY.md contracts/noisep contracts/testdata/noise.json
git commit -m "Add the Noise profile for session, tunnel and end-to-end channels" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 8: Vertrauensliste (`contracts/trust`)

Die Vertrauensliste ist der gemeinsame Anker für Browser und Controller. Jede Fassung ist ein
JSON-Dokument, unterschrieben mit einem `proof.Proof` (Passkey oder Passwortschlüssel), nicht mit
einem `sign.Envelope`, weil Admins Passkeys benutzen.

Regeln für eine neue Fassung:

- Nummer genau eins höher, `prev` ist SHA-256 der Nutzlast der vorigen Fassung, gleiche Installation.
- Unterschrieben von einem Admin der **vorigen** Fassung (Rolle `admin`) mit einem seiner Schlüssel;
  Passkey-Beweise werden gegen das `rp` der vorigen Fassung geprüft.
- Ausnahme ohne Admin: Ein Benutzer ändert nur seine eigenen Schlüssel (zweiter Stick, neues Passwort)
  und unterschreibt mit einem seiner Schlüssel aus der vorigen Fassung. Alles andere muss gleich bleiben.
- Fassung 1 unterschreibt ein Admin, der in ihr selbst steht.

Anker: Nummer und SHA-256 einer Fassung. Damit sind auch die Admin-Schlüssel festgelegt, wie im
Entwurf verlangt, nur genauer. Den Anker liefert dem Browser der Webserver (`trust-anchor.json`,
Task 18), dem Controller der Kopplungscode (Task 11). Wer eine Fassung geprüft hat, merkt sich diese
als neuen Anker und nimmt danach nur noch Nachfolger an.

Stufen der erneuten Bestätigung: Pro Rolle gilt `needs(op)` = Blacklist trifft → ja; Whitelist
trifft → nein; sonst nach Stufe (`off` nie, `critical` bei kritischen Vorgängen, `root` zusätzlich
bei Root-Aufgaben, `hardcore` zusätzlich bei jeder Änderung und jedem Kanal). Hat ein Benutzer
mehrere Rollen, reicht ein Ja. Änderungen an der Vertrauensliste selbst unterschreibt ein Admin
immer mit Passkey oder Passwortschlüssel; das ist schon eine erneute Bestätigung.

Listeneinträge: `app/<app>`, `app/<app>/<aktion>`, `node/<server>`.

**Files:**
- Create: `contracts/trust/trust.go`, `contracts/trust/trust_test.go`,
  `contracts/trust/trusttest/trusttest.go`, `contracts/testdata/trust.json` (per `-update`)

**Interfaces:**
- Consumes: `enc.Bytes`, `enc.B64`, `enc.DecodeStrict`, `sign.KeyID`, `sign.Message`, `sign.PurposeTrustList`,
  `sign.MaxPayload`, `proof.UserKey`, `proof.Proof`, `proof.RP`, `proof.Verify`, `proof.KindPassword`,
  `proof.KindPasskey`, `wire.ValidName`, `wire.ValidAppID`, `noisep.Keypair`, `golden.Check`.
- Produces:
  - Konstanten `trust.RoleAdmin = "admin"`, `MaxUsers`, `MaxNodes`, `MaxRoles`, `MaxStores`, `MaxKeysPerUser`, `MaxEntries`
  - `type trust.Level string`: `LevelOff`, `LevelCritical`, `LevelRoot`, `LevelHardcore`
  - `type trust.NodeKind string`: `NodeAPI = "api"`, `NodeController = "controller"`
  - Update-Ziele `trust.AutoWeb = "ngc-web"`, `AutoAPI = "ngc-api"`, `AutoController = "ngc-controller"`, `AutoSystem = "system"`; sonst eine App-ID
  - `type trust.User struct{ ID, Name string; Roles []string; Keys []proof.UserKey }` (JSON `id`, `name`, `roles`, `keys`)
  - `type trust.Role struct{ Name string; Level Level; Black, White []string; PasskeyOnly, PasswordToo bool }`
    (JSON `name`, `level`, `black`, `white`, `passkeyOnly`, `passwordToo`)
  - `type trust.Node struct{ ID string; Kind NodeKind; Name string; Noise, Sign enc.Bytes; URL string; RootUsers, RootRoles []string }`
    (JSON `id`, `kind`, `name`, `noise`, `sign`, `url`, `rootUsers`, `rootRoles`)
  - `type trust.Store struct{ ID, Name, URL string; Key enc.Bytes }` (JSON `id`, `name`, `url`, `key`)
  - `type trust.Auto struct{ Target string; Key enc.Bytes }` (JSON `target`, `key`) – Dauerfreigabe
  - `type trust.List struct{ Seq uint64; Prev enc.Bytes; Issued int64; Installation string; RP proof.RP; Users []User; Roles []Role; Nodes []Node; Stores []Store; Auto []Auto }`
    (JSON `seq`, `prev`, `issued`, `installation`, `rp`, `users`, `roles`, `nodes`, `stores`, `auto`)
  - `(*List) Validate() error`, `(*List) User(id string) (User, bool)`, `(*List) Role(name string) (Role, bool)`,
    `(*List) Node(id string) (Node, bool)`, `(*List) KeyOwner(keyID string) (User, proof.UserKey, bool)`,
    `(*List) MayRoot(userID, nodeID string) bool`, `(*List) NeedsConfirm(userID string, op Op) bool`
  - `type trust.Op struct{ App, Action, Node string; Root, Critical, Changes bool }`
  - `type trust.Signed struct{ Payload enc.Bytes; Proof proof.Proof }` (JSON `payload`, `proof`)
  - `type trust.Anchor struct{ Seq uint64; Hash enc.Bytes }` (JSON `seq`, `hash`)
  - `type trust.Version struct{ List *List; Hash []byte }`
  - `trust.ValidID(string) bool` (16 Byte base64url, 22 Zeichen), `trust.Hash(Signed) []byte`, `trust.Parse(payload []byte) (*List, error)`,
    `trust.Open(a Anchor, chain []Signed) (Version, error)`, `trust.Advance(cur Version, chain []Signed) (Version, error)`
  - Fehler `trust.ErrInvalid`, `ErrAnchor`, `ErrChain`, `ErrSigner` (Unterschriftsfehler kommen als `proof.Err…`)
  - `trusttest.ID(fill byte) string`, `type trusttest.Signer struct{ Priv ed25519.PrivateKey; Key proof.UserKey }`,
    `trusttest.NewSigner(fill byte) Signer`, `(Signer) Prove(purpose string, payload []byte) proof.Proof`,
    `(Signer) Sign(t testing.TB, l *trust.List) trust.Signed`, `trusttest.NodeKeys(fill byte) (noise.DHKey, ed25519.PrivateKey)`,
    `trusttest.Genesis(admin Signer) *trust.List` (Admin `admin`, API `api-1` mit Schlüsseln aus `NodeKeys(0xA1)`,
    Controller `ctl-1` mit `NodeKeys(0xC1)` und Root für die Rolle `admin`), `trusttest.Next(t testing.TB, prev trust.Signed) *trust.List`

- [ ] **Step 1: Test schreiben**

`contracts/trust/trust_test.go`:

```go
package trust_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"flag"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/trust/trusttest"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

var (
	admin   = trusttest.NewSigner(1)
	alice   = trusttest.NewSigner(2)
	mallory = trusttest.NewSigner(3)
)

func anchor(t *testing.T, s trust.Signed) trust.Anchor {
	t.Helper()
	l, err := trust.Parse(s.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return trust.Anchor{Seq: l.Seq, Hash: trust.Hash(s)}
}

// chain returns version 1 (genesis), version 2 (the admin adds alice) and
// version 3 (alice adds a second key of her own, without an admin).
func chain(t *testing.T) []trust.Signed {
	g := admin.Sign(t, trusttest.Genesis(admin))
	l2 := trusttest.Next(t, g)
	l2.Roles = append(l2.Roles, trust.Role{Name: "user", Level: trust.LevelCritical})
	l2.Users = append(l2.Users, trust.User{ID: trusttest.ID(0x22), Name: "alice", Roles: []string{"user"}, Keys: []proof.UserKey{alice.Key}})
	v2 := admin.Sign(t, l2)
	l3 := trusttest.Next(t, v2)
	l3.Users[1].Keys = append(l3.Users[1].Keys, trusttest.NewSigner(4).Key)
	return []trust.Signed{g, v2, alice.Sign(t, l3)}
}

func TestChain(t *testing.T) {
	c := chain(t)
	v, err := trust.Open(anchor(t, c[0]), c)
	if err != nil || v.List.Seq != 3 {
		t.Fatalf("open: %v", err)
	}
	v1, err := trust.Open(anchor(t, c[0]), c[:1])
	if err != nil {
		t.Fatal(err)
	}
	if v, err = trust.Advance(v1, c[1:]); err != nil || v.List.Seq != 3 {
		t.Fatalf("advance: %v", err)
	}
}

type vector struct {
	Name   string         `json:"name"`
	Anchor trust.Anchor   `json:"anchor"`
	Chain  []trust.Signed `json:"chain"`
	Valid  bool           `json:"valid"`
	Seq    uint64         `json:"seq,omitempty"`
}

func vectors(t *testing.T) []vector {
	c := chain(t)
	g, v2 := c[0], c[1]

	l, err := trust.Parse(v2.Payload)
	if err != nil {
		t.Fatal(err)
	}
	l.Users[1].Roles = []string{trust.RoleAdmin, "user"}
	b, _ := json.Marshal(l)
	tampered := trust.Signed{Payload: b, Proof: v2.Proof}

	l3 := trusttest.Next(t, v2)
	l3.Users[1].Roles = []string{trust.RoleAdmin, "user"}
	selfPromoted := alice.Sign(t, l3)

	foreign := mallory.Sign(t, trusttest.Next(t, g))
	gap := admin.Sign(t, trusttest.Next(t, v2))

	return []vector{
		{"genesis", anchor(t, g), c[:1], true, 1},
		{"admin adds a user, user adds an own key", anchor(t, g), c, true, 3},
		{"anchor in the middle", anchor(t, v2), c[1:], true, 3},
		{"wrong anchor", anchor(t, v2), c[:1], false, 0},
		{"missing version", anchor(t, g), []trust.Signed{g, gap}, false, 0},
		{"old version again", anchor(t, g), []trust.Signed{g, v2, v2}, false, 0},
		{"tampered", anchor(t, g), []trust.Signed{g, tampered}, false, 0},
		{"user makes herself admin", anchor(t, g), []trust.Signed{g, v2, selfPromoted}, false, 0},
		{"unknown signer", anchor(t, g), []trust.Signed{g, foreign}, false, 0},
	}
}

func TestVectors(t *testing.T) {
	vs := vectors(t)
	for _, v := range vs {
		got, err := trust.Open(v.Anchor, v.Chain)
		if (err == nil) != v.Valid || (v.Valid && got.List.Seq != v.Seq) {
			t.Errorf("%s: want valid=%v seq=%d, got %v", v.Name, v.Valid, v.Seq, err)
		}
	}
	golden.Check(t, *update, "trust.json", vs)
}

func passkey(t *testing.T) proof.UserKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	spki, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	return proof.UserKey{ID: sign.KeyID(spki), Kind: proof.KindPasskey, Alg: proof.AlgES256, Public: spki, CredID: []byte("stick-1")}
}

func TestValidate(t *testing.T) {
	if err := trusttest.Genesis(admin).Validate(); err != nil {
		t.Fatal(err)
	}
	pk := passkey(t)
	cases := map[string]func(l *trust.List){
		"no admin": func(l *trust.List) {
			l.Roles = append(l.Roles, trust.Role{Name: "user", Level: trust.LevelOff})
			l.Users[0].Roles = []string{"user"}
		},
		"unknown role":  func(l *trust.List) { l.Users[0].Roles = []string{trust.RoleAdmin, "ghost"} },
		"duplicate key": func(l *trust.List) { l.Users = append(l.Users, trust.User{ID: trusttest.ID(0x22), Name: "bob", Roles: []string{trust.RoleAdmin}, Keys: l.Users[0].Keys}) },
		"unsorted nodes": func(l *trust.List) { l.Nodes[0], l.Nodes[1] = l.Nodes[1], l.Nodes[0] },
		"passkey without rp": func(l *trust.List) { l.Users[0].Keys = append(l.Users[0].Keys, pk) },
		"password key in passkey-only role": func(l *trust.List) { l.Roles[0].PasskeyOnly = true },
		"bad list entry":    func(l *trust.List) { l.Roles[0].Black = []string{"everything"} },
		"api without url":   func(l *trust.List) { l.Nodes[0].URL = "" },
		"genesis with prev": func(l *trust.List) { l.Prev = make([]byte, 32) },
	}
	for name, mutate := range cases {
		l := trusttest.Genesis(admin)
		mutate(l)
		if err := l.Validate(); err != trust.ErrInvalid {
			t.Errorf("%s: got %v", name, err)
		}
	}
	l := trusttest.Genesis(admin)
	l.RP = proof.RP{ID: "ngc.example.org", Origins: []string{"https://ngc.example.org"}}
	l.Users[0].Keys = append(l.Users[0].Keys, pk)
	if err := l.Validate(); err != nil {
		t.Errorf("passkey with rp: %v", err)
	}
}

func TestPolicy(t *testing.T) {
	l := trusttest.Genesis(admin)
	l.Roles = []trust.Role{
		{Name: trust.RoleAdmin, Level: trust.LevelCritical, White: []string{"app/ngc.store/update"}},
		{Name: "ops", Level: trust.LevelHardcore},
		{Name: "user", Level: trust.LevelOff, Black: []string{"app/org.sorglos.terminal"}},
	}
	bob, carol := trusttest.ID(0x22), trusttest.ID(0x23)
	l.Users = append(l.Users,
		trust.User{ID: bob, Name: "bob", Roles: []string{"user"}, Keys: []proof.UserKey{trusttest.NewSigner(5).Key}},
		trust.User{ID: carol, Name: "carol", Roles: []string{"ops", "user"}, Keys: []proof.UserKey{trusttest.NewSigner(6).Key}})
	if err := l.Validate(); err != nil {
		t.Fatal(err)
	}
	adminID := l.Users[0].ID
	shell := trust.Op{App: "org.sorglos.terminal", Action: "shell", Node: "ctl-1", Changes: true}
	calc := trust.Op{App: "org.sorglos.calculator", Action: "save", Node: "ctl-1", Changes: true}
	cases := []struct {
		name string
		user string
		op   trust.Op
		want bool
	}{
		{"admin, normal channel", adminID, shell, false},
		{"admin, critical", adminID, trust.Op{App: "ngc.store", Action: "upload", Critical: true}, true},
		{"admin, whitelisted", adminID, trust.Op{App: "ngc.store", Action: "update", Root: true, Critical: true}, false},
		{"user, blacklisted", bob, shell, true},
		{"user, level off", bob, calc, false},
		{"strictest role wins", carol, calc, true},
		{"unknown user", trusttest.ID(0x99), calc, true},
	}
	for _, c := range cases {
		if got := l.NeedsConfirm(c.user, c.op); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
	if !l.MayRoot(adminID, "ctl-1") || l.MayRoot(bob, "ctl-1") || l.MayRoot(adminID, "api-1") {
		t.Error("root permissions wrong")
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(admin.Sign(f, trusttest.Genesis(admin)).Payload))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = trust.Parse(b)
	})
}
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

Run: `go test ./contracts/trust/`
Expected: FAIL, Paket `contracts/trust/trusttest` und `trust.List` fehlen.


- [ ] **Step 3: Umsetzung schreiben**

`contracts/trust/trust.go`:

```go
// Package trust defines the trust list: users and their keys, API servers and
// controllers, root permissions, stores, re-confirmation policy and standing
// approvals for updates. An admin signs every version with a passkey or
// password key. Browsers and controllers verify the chain of versions from a
// pinned anchor themselves, so the API can distribute the list but not forge it.
package trust

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// Limits and the admin role.
const (
	RoleAdmin      = "admin"
	MaxUsers       = 10000
	MaxNodes       = 256
	MaxRoles       = 256
	MaxStores      = 64
	MaxKeysPerUser = 16
	MaxEntries     = 256 // per black or white list
)

// Level of re-confirmation for a role.
type Level string

const (
	LevelOff      Level = "off"
	LevelCritical Level = "critical"
	LevelRoot     Level = "root"
	LevelHardcore Level = "hardcore"
)

// NodeKind tells API servers and controllers apart.
type NodeKind string

const (
	NodeAPI        NodeKind = "api"
	NodeController NodeKind = "controller"
)

// Targets of standing approvals besides app IDs.
const (
	AutoWeb        = "ngc-web"
	AutoAPI        = "ngc-api" // includes the database migrations of that API version
	AutoController = "ngc-controller"
	AutoSystem     = "system" // Debian and Ubuntu packages via apt
)

// User is a person with login keys.
type User struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"` // login name
	Roles []string        `json:"roles"`
	Keys  []proof.UserKey `json:"keys"`
}

// Role carries the re-confirmation policy. Rights on apps are kept by the API.
type Role struct {
	Name        string   `json:"name"`
	Level       Level    `json:"level"`
	Black       []string `json:"black,omitempty"`
	White       []string `json:"white,omitempty"`
	PasskeyOnly bool     `json:"passkeyOnly,omitempty"` // members have no password keys
	PasswordToo bool     `json:"passwordToo,omitempty"` // the API asks for the password in addition to a passkey
}

// Node is an API server or a controller.
type Node struct {
	ID        string    `json:"id"`
	Kind      NodeKind  `json:"kind"`
	Name      string    `json:"name"`
	Noise     enc.Bytes `json:"noise"`               // X25519 static key: tunnel, and for controllers end to end
	Sign      enc.Bytes `json:"sign,omitempty"`      // API only: Ed25519 key that signs API certificates
	URL       string    `json:"url,omitempty"`       // API only: tunnel address, wss://host:port/tunnel
	RootUsers []string  `json:"rootUsers,omitempty"` // controller only
	RootRoles []string  `json:"rootRoles,omitempty"` // controller only
}

// Store is an app source whose catalog key an admin confirmed.
type Store struct {
	ID   string    `json:"id"`
	Name string    `json:"name"`
	URL  string    `json:"url"`
	Key  enc.Bytes `json:"key"` // Ed25519 key that signs the catalog
}

// Auto is a standing approval: updates for Target signed with Key may be
// installed without a fresh signature. Key is empty for AutoSystem.
type Auto struct {
	Target string    `json:"target"`
	Key    enc.Bytes `json:"key,omitempty"`
}

// List is one version of the trust list. Users, roles, nodes, stores and
// standing approvals are sorted by ID, name or target, without duplicates.
type List struct {
	Seq          uint64    `json:"seq"`
	Prev         enc.Bytes `json:"prev,omitempty"` // SHA-256 of the previous payload; empty in version 1
	Issued       int64     `json:"issued"`
	Installation string    `json:"installation"`
	RP           proof.RP  `json:"rp"`
	Users        []User    `json:"users"`
	Roles        []Role    `json:"roles"`
	Nodes        []Node    `json:"nodes,omitempty"`
	Stores       []Store   `json:"stores,omitempty"`
	Auto         []Auto    `json:"auto,omitempty"`
}

// Signed is one version as distributed: the exact payload and the proof over it.
type Signed struct {
	Payload enc.Bytes   `json:"payload"`
	Proof   proof.Proof `json:"proof"`
}

// Anchor pins one version.
type Anchor struct {
	Seq  uint64    `json:"seq"`
	Hash enc.Bytes `json:"hash"`
}

// Version is a verified version.
type Version struct {
	List *List
	Hash []byte
}

var (
	ErrInvalid = errors.New("trust: invalid list")
	ErrAnchor  = errors.New("trust: list does not match the anchor")
	ErrChain   = errors.New("trust: broken chain of versions")
	ErrSigner  = errors.New("trust: signer may not make this change")
)

// ValidID reports whether s is an ID: 16 bytes, base64url, 22 characters.
func ValidID(s string) bool {
	b, err := enc.B64.DecodeString(s)
	return err == nil && len(b) == 16
}

// Hash returns the SHA-256 of the payload of s.
func Hash(s Signed) []byte {
	h := sha256.Sum256(s.Payload)
	return h[:]
}

// Parse decodes and validates a payload. It does not check signatures.
func Parse(payload []byte) (*List, error) {
	var l List
	if err := enc.DecodeStrict(payload, sign.MaxPayload, &l); err != nil {
		return nil, ErrInvalid
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return &l, nil
}

// Open verifies chain from a pinned anchor and returns the newest version.
// chain[0] must be the anchored version.
func Open(a Anchor, chain []Signed) (Version, error) {
	if len(chain) == 0 {
		return Version{}, ErrAnchor
	}
	l, err := Parse(chain[0].Payload)
	if err != nil {
		return Version{}, err
	}
	h := Hash(chain[0])
	if l.Seq != a.Seq || !bytes.Equal(h, a.Hash) {
		return Version{}, ErrAnchor
	}
	if l.Seq == 1 {
		if err := checkSigner(l, l, chain[0]); err != nil {
			return Version{}, err
		}
	}
	return Advance(Version{List: l, Hash: h}, chain[1:])
}

// Advance verifies the versions following cur, in order, and returns the newest.
func Advance(cur Version, chain []Signed) (Version, error) {
	for _, s := range chain {
		next, err := Parse(s.Payload)
		if err != nil {
			return Version{}, err
		}
		if next.Seq != cur.List.Seq+1 || !bytes.Equal(next.Prev, cur.Hash) || next.Installation != cur.List.Installation {
			return Version{}, ErrChain
		}
		if err := checkSigner(cur.List, next, s); err != nil {
			return Version{}, err
		}
		cur = Version{List: next, Hash: Hash(s)}
	}
	return cur, nil
}

// checkSigner verifies the proof of s with a key from prev and checks that
// its owner may make the change from prev to next.
func checkSigner(prev, next *List, s Signed) error {
	u, key, ok := prev.KeyOwner(s.Proof.KeyID)
	if !ok {
		return ErrSigner
	}
	if err := proof.Verify(s.Proof, key, prev.RP, sign.PurposeTrustList, s.Payload); err != nil {
		return err
	}
	if slices.Contains(u.Roles, RoleAdmin) {
		return nil
	}
	if prev == next || !onlyOwnKeys(prev, next, u.ID) {
		return ErrSigner
	}
	return nil
}

// onlyOwnKeys reports whether next differs from prev in nothing but the keys
// of user id (and the version fields).
func onlyOwnKeys(prev, next *List, id string) bool {
	a, b := *prev, *next
	i, j := a.userIndex(id), b.userIndex(id)
	if i < 0 || j < 0 {
		return false
	}
	a.Users, b.Users = slices.Clone(a.Users), slices.Clone(b.Users)
	a.Users[i].Keys, b.Users[j].Keys = nil, nil
	a.Seq, a.Prev, a.Issued = 0, nil, 0
	b.Seq, b.Prev, b.Issued = 0, nil, 0
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

func (l *List) userIndex(id string) int {
	i, ok := slices.BinarySearchFunc(l.Users, id, func(u User, id string) int { return strings.Compare(u.ID, id) })
	if !ok {
		return -1
	}
	return i
}

// User returns the user with id.
func (l *List) User(id string) (User, bool) {
	if i := l.userIndex(id); i >= 0 {
		return l.Users[i], true
	}
	return User{}, false
}

// Role returns the role with name.
func (l *List) Role(name string) (Role, bool) {
	i, ok := slices.BinarySearchFunc(l.Roles, name, func(r Role, n string) int { return strings.Compare(r.Name, n) })
	if !ok {
		return Role{}, false
	}
	return l.Roles[i], true
}

// Node returns the node with id.
func (l *List) Node(id string) (Node, bool) {
	i, ok := slices.BinarySearchFunc(l.Nodes, id, func(n Node, id string) int { return strings.Compare(n.ID, id) })
	if !ok {
		return Node{}, false
	}
	return l.Nodes[i], true
}

// KeyOwner finds the user a key belongs to.
func (l *List) KeyOwner(keyID string) (User, proof.UserKey, bool) {
	for _, u := range l.Users {
		for _, k := range u.Keys {
			if k.ID == keyID {
				return u, k, true
			}
		}
	}
	return User{}, proof.UserKey{}, false
}

// MayRoot reports whether a user may run root tasks on a controller.
func (l *List) MayRoot(userID, nodeID string) bool {
	n, ok := l.Node(nodeID)
	u, ok2 := l.User(userID)
	if !ok || !ok2 || n.Kind != NodeController {
		return false
	}
	if slices.Contains(n.RootUsers, userID) {
		return true
	}
	for _, r := range u.Roles {
		if slices.Contains(n.RootRoles, r) {
			return true
		}
	}
	return false
}

// Op describes an operation for the re-confirmation check.
type Op struct {
	App, Action, Node string
	Root              bool // runs as root on a controller
	Critical          bool // a critical operation (package task, or marked in the app manifest)
	Changes           bool // changes something; every channel to a service counts as a change
}

// NeedsConfirm reports whether user must confirm op again. Unknown users always must.
func (l *List) NeedsConfirm(userID string, op Op) bool {
	u, ok := l.User(userID)
	if !ok {
		return true
	}
	for _, name := range u.Roles {
		if r, ok := l.Role(name); !ok || r.needs(op) {
			return true
		}
	}
	return false
}

func (r Role) needs(op Op) bool {
	switch {
	case matches(r.Black, op):
		return true
	case matches(r.White, op):
		return false
	case r.Level == LevelCritical:
		return op.Critical
	case r.Level == LevelRoot:
		return op.Critical || op.Root
	case r.Level == LevelHardcore:
		return op.Critical || op.Root || op.Changes
	}
	return false
}

func matches(entries []string, op Op) bool {
	for _, e := range entries {
		if e == "app/"+op.App || e == "app/"+op.App+"/"+op.Action || (op.Node != "" && e == "node/"+op.Node) {
			return true
		}
	}
	return false
}

// Validate checks structure, order and references of l.
func (l *List) Validate() error {
	if l.Seq == 0 || (l.Seq == 1) != (len(l.Prev) == 0) || (l.Seq > 1 && len(l.Prev) != sha256.Size) ||
		l.Issued <= 0 || !ValidID(l.Installation) {
		return ErrInvalid
	}
	if len(l.Users) == 0 || len(l.Users) > MaxUsers || len(l.Roles) > MaxRoles ||
		len(l.Nodes) > MaxNodes || len(l.Stores) > MaxStores {
		return ErrInvalid
	}
	for i, r := range l.Roles {
		if !wire.ValidName(r.Name) || (i > 0 && l.Roles[i-1].Name >= r.Name) || !validLevel(r.Level) ||
			!validEntries(r.Black) || !validEntries(r.White) {
			return ErrInvalid
		}
	}
	if _, ok := l.Role(RoleAdmin); !ok {
		return ErrInvalid
	}
	if err := l.validateUsers(); err != nil {
		return err
	}
	for i, n := range l.Nodes {
		if !wire.ValidName(n.ID) || (i > 0 && l.Nodes[i-1].ID >= n.ID) || !validLabel(n.Name) || len(n.Noise) != 32 {
			return ErrInvalid
		}
		switch n.Kind {
		case NodeAPI:
			if len(n.Sign) != ed25519.PublicKeySize || !strings.HasPrefix(n.URL, "wss://") || len(n.URL) > 256 ||
				len(n.RootUsers)+len(n.RootRoles) > 0 {
				return ErrInvalid
			}
		case NodeController:
			if len(n.Sign) != 0 || n.URL != "" {
				return ErrInvalid
			}
			for _, id := range n.RootUsers {
				if _, ok := l.User(id); !ok {
					return ErrInvalid
				}
			}
			for _, r := range n.RootRoles {
				if _, ok := l.Role(r); !ok {
					return ErrInvalid
				}
			}
		default:
			return ErrInvalid
		}
	}
	for i, s := range l.Stores {
		if !wire.ValidName(s.ID) || (i > 0 && l.Stores[i-1].ID >= s.ID) || !validLabel(s.Name) ||
			!strings.HasPrefix(s.URL, "https://") || len(s.URL) > 256 || len(s.Key) != ed25519.PublicKeySize {
			return ErrInvalid
		}
	}
	for i, a := range l.Auto {
		if i > 0 && l.Auto[i-1].Target >= a.Target {
			return ErrInvalid
		}
		switch {
		case a.Target == AutoSystem:
			if len(a.Key) != 0 {
				return ErrInvalid
			}
		case a.Target == AutoWeb || a.Target == AutoAPI || a.Target == AutoController || wire.ValidAppID(a.Target):
			if len(a.Key) != ed25519.PublicKeySize {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	return nil
}

func (l *List) validateUsers() error {
	keys := map[string]bool{}
	names := map[string]bool{}
	admins, passkeys := 0, false
	for i, u := range l.Users {
		if !ValidID(u.ID) || (i > 0 && l.Users[i-1].ID >= u.ID) || !validLoginName(u.Name) || names[u.Name] ||
			len(u.Roles) == 0 || len(u.Keys) == 0 || len(u.Keys) > MaxKeysPerUser {
			return ErrInvalid
		}
		names[u.Name] = true
		passkeyOnly := false
		for j, name := range u.Roles {
			r, ok := l.Role(name)
			if !ok || (j > 0 && u.Roles[j-1] >= name) {
				return ErrInvalid
			}
			passkeyOnly = passkeyOnly || r.PasskeyOnly
		}
		for _, k := range u.Keys {
			if k.Validate() != nil || keys[k.ID] || (passkeyOnly && k.Kind == proof.KindPassword) {
				return ErrInvalid
			}
			keys[k.ID] = true
			passkeys = passkeys || k.Kind == proof.KindPasskey
		}
		if slices.Contains(u.Roles, RoleAdmin) {
			admins++
		}
	}
	if admins == 0 || (passkeys && (l.RP.ID == "" || len(l.RP.Origins) == 0)) {
		return ErrInvalid
	}
	for _, o := range l.RP.Origins {
		if !strings.HasPrefix(o, "https://") || len(o) > 256 {
			return ErrInvalid
		}
	}
	return nil
}

func validLevel(l Level) bool {
	return l == LevelOff || l == LevelCritical || l == LevelRoot || l == LevelHardcore
}

func validEntries(entries []string) bool {
	if len(entries) > MaxEntries {
		return false
	}
	for _, e := range entries {
		switch {
		case strings.HasPrefix(e, "node/"):
			if !wire.ValidName(e[len("node/"):]) {
				return false
			}
		case strings.HasPrefix(e, "app/"):
			app, action, found := strings.Cut(e[len("app/"):], "/")
			if !wire.ValidAppID(app) || (found && !wire.ValidName(action)) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// validLoginName allows lower-case letters, digits and . _ - (1 to 64 characters).
func validLoginName(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for i, c := range s {
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || (i > 0 && (c == '.' || c == '_' || c == '-'))
		if !ok {
			return false
		}
	}
	return true
}

// validLabel allows any printable UTF-8 text of 1 to 64 bytes.
func validLabel(s string) bool {
	if len(s) == 0 || len(s) > 64 || !utf8.ValidString(s) {
		return false
	}
	for _, c := range s {
		if !unicode.IsPrint(c) {
			return false
		}
	}
	return true
}
```

`contracts/trust/trusttest/trusttest.go`:

```go
// Package trusttest builds signed trust lists for tests in other packages.
// Its keys come from fixed seeds and must never be used outside tests.
package trusttest

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"testing"

	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/noisep"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
)

// ID returns a valid ID made of 16 bytes of fill.
func ID(fill byte) string { return enc.B64.EncodeToString(bytes.Repeat([]byte{fill}, 16)) }

// Signer holds a password key. The key comes from a fixed seed instead of
// Argon2id so that tests stay fast; the KDF fields are only metadata.
type Signer struct {
	Priv ed25519.PrivateKey
	Key  proof.UserKey
}

// NewSigner returns the password key with seed fill.
func NewSigner(fill byte) Signer {
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{fill}, ed25519.SeedSize))
	pub := priv.Public().(ed25519.PublicKey)
	return Signer{Priv: priv, Key: proof.UserKey{
		ID: sign.KeyID(pub), Kind: proof.KindPassword, Alg: proof.AlgEdDSA, Public: enc.Bytes(pub),
		KDF: &proof.KDF{Alg: proof.KDFArgon2id, Salt: bytes.Repeat([]byte{fill}, proof.SaltSize),
			MemKiB: proof.DefaultMemKiB, Time: proof.DefaultTime, Par: proof.DefaultPar},
	}}
}

// Prove signs payload for purpose, as the browser does with a password key.
func (s Signer) Prove(purpose string, payload []byte) proof.Proof {
	return proof.Proof{Kind: proof.KindPassword, KeyID: s.Key.ID, Sig: ed25519.Sign(s.Priv, sign.Message(purpose, payload))}
}

// Sign signs l as a trust list version.
func (s Signer) Sign(t testing.TB, l *trust.List) trust.Signed {
	t.Helper()
	b, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	return trust.Signed{Payload: b, Proof: s.Prove(sign.PurposeTrustList, b)}
}

// NodeKeys returns the Noise key and the signing key of a test node.
func NodeKeys(fill byte) (noise.DHKey, ed25519.PrivateKey) {
	dh, err := noisep.Keypair(bytes.NewReader(bytes.Repeat([]byte{fill}, 32)))
	if err != nil {
		panic(err)
	}
	return dh, ed25519.NewKeyFromSeed(bytes.Repeat([]byte{fill}, ed25519.SeedSize))
}

// Genesis returns version 1: user "admin" with admin's key, API "api-1"
// (NodeKeys(0xA1)) and controller "ctl-1" (NodeKeys(0xC1)), on which admins may
// run root tasks.
func Genesis(admin Signer) *trust.List {
	apiDH, apiSign := NodeKeys(0xA1)
	ctlDH, _ := NodeKeys(0xC1)
	return &trust.List{
		Seq: 1, Issued: 1790000000, Installation: ID(0x11),
		Users: []trust.User{{ID: ID(0x21), Name: "admin", Roles: []string{trust.RoleAdmin}, Keys: []proof.UserKey{admin.Key}}},
		Roles: []trust.Role{{Name: trust.RoleAdmin, Level: trust.LevelCritical}},
		Nodes: []trust.Node{
			{ID: "api-1", Kind: trust.NodeAPI, Name: "API 1", Noise: apiDH.Public,
				Sign: enc.Bytes(apiSign.Public().(ed25519.PublicKey)), URL: "wss://api-1.test:7443/tunnel"},
			{ID: "ctl-1", Kind: trust.NodeController, Name: "Dienste", Noise: ctlDH.Public, RootRoles: []string{trust.RoleAdmin}},
		},
	}
}

// Next returns the list signed in prev with Seq, Prev and Issued advanced.
func Next(t testing.TB, prev trust.Signed) *trust.List {
	t.Helper()
	l, err := trust.Parse(prev.Payload)
	if err != nil {
		t.Fatal(err)
	}
	l.Seq++
	l.Prev = trust.Hash(prev)
	l.Issued++
	return l
}
```

- [ ] **Step 4: Vektoren erzeugen und Tests laufen lassen**

Run:

```bash
go test ./contracts/trust/ -run TestVectors -update
go test ./contracts/trust/...
go test -run=XXX -fuzz=FuzzParse -fuzztime=30s ./contracts/trust/
```

Expected: `ok`; `contracts/testdata/trust.json` angelegt (neun Fälle, alle Unterschriften
deterministisch); Fuzzing ohne Fund.

- [ ] **Step 5: Commit**

```bash
git add contracts/trust contracts/testdata/trust.json
git commit -m "Add the trust list with chain verification and re-confirmation policy" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---


### Task 9: Anmeldung und Sitzungszertifikate (`contracts/session`)

Ablauf (V1, bis zum WebSocket als JSON über HTTPS):

1. `POST /api/v1/login/start` mit `LoginStart` → `LoginChallenge`. Für unbekannte Namen antwortet die
   API gleichförmig mit erfundenen, aber stabilen Werten, damit sich Namen nicht erraten lassen.
2. Der Browser erzeugt zwei Schlüsselpaare, die niemand auslesen kann (Web Crypto, nicht
   exportierbar): Ed25519 `sig` für Aufgaben und X25519 `dh` für Noise. Er baut das `ClientCert`
   (Ablauf höchstens `Now + MaxLifetime`) und unterschreibt es mit Passwortschlüssel oder Passkey,
   Zweck `clientcert/v1`.
3. `POST /api/v1/login/finish` mit `LoginFinish` → `LoginResult` und das Cookie `ngc_session`
   (`HttpOnly; Secure; SameSite=Strict; Path=/api/`). Die API prüft Nonce, Beweis, TOTP und die
   Sperre nach Fehlversuchen. In der Datenbank liegt nur `TokenHash` des Cookies.
4. `GET /api/v1/trust?from=N` (mit Cookie) → `TrustChain`. Der Browser prüft die Kette gegen seinen
   Anker (Task 8) und danach das `APICert` mit `VerifyAPI`. Schlägt etwas fehl: Alarm und abmelden.
5. `GET /api/v1/ws` (mit Cookie, Unterprotokoll `ngc.v1`): erste Nachricht Noise-Nachricht 1 des
   Browsers mit Prolog `ngc/session/v1`, statischer Schlüssel `dh`, Gegenseite `APICert.dh`; zweite
   Nachricht die Antwort der API. Danach ist jede binäre WebSocket-Nachricht genau eine
   Noise-Transportnachricht mit einem Frame (Task 6). Jede API kann jede Sitzung bedienen, weil der
   private Sitzungsschlüssel der API verschlüsselt in der Datenbank liegt.
6. `POST /api/v1/logout` verwirft die Sitzung.

Ersteinrichtung: `GET /api/v1/setup` → `SetupInfo`, solange es keinen Admin gibt; `POST /api/v1/setup`
mit `Setup` (Einmal-Code aus dem ulh-Skript und Fassung 1 der Vertrauensliste, die der neue Admin
im Browser erzeugt und unterschrieben hat). Danach meldet er sich normal an.

**Files:**
- Create: `contracts/session/session.go`, `contracts/session/session_test.go`,
  `contracts/testdata/session.json` (per `-update`)

**Interfaces:**
- Consumes: `enc.Bytes`, `enc.DecodeStrict`, `sign.Envelope`, `sign.Sign`, `sign.Verify`, `sign.KeyID`,
  `sign.PurposeClientCert`, `sign.PurposeAPICert`, `proof.Proof`, `proof.KDF`, `proof.RP`, `proof.Verify`,
  `trust.List`, `trust.Signed`, `trust.Node`, `trust.ValidID`, `trusttest.*`, `golden.Check`.
- Produces:
  - Pfade `session.PathSetup = "/api/v1/setup"`, `PathLoginStart = "/api/v1/login/start"`,
    `PathLoginFinish = "/api/v1/login/finish"`, `PathLogout = "/api/v1/logout"`, `PathTrust = "/api/v1/trust"`,
    `PathSocket = "/api/v1/ws"`, `Subprotocol = "ngc.v1"`, `CookieName = "ngc_session"`
  - Konstanten `TokenSize = 32`, `NonceSize = 32`, `MaxLifetime = 86400`, `MaxSkew = 300`,
    `ChallengeLifetime = 120`, `MaxCert = 4096`, `MaxRequest = 64 << 10`, `TOTPDigits = 6`, `TOTPPeriod = 30`
  - `type session.LoginStart struct{ Name string }` (JSON `name`)
  - `type session.LoginChallenge struct{ Nonce enc.Bytes; Now, MaxLifetime int64; Installation, User string; KDF *proof.KDF; Passkeys []enc.Bytes; TOTP, PasswordToo bool; RP proof.RP }`
    (JSON `nonce`, `now`, `maxLifetime`, `installation`, `user`, `kdf`, `passkeys`, `totp`, `passwordToo`, `rp`)
  - `type session.ClientCert struct{ Installation, User string; Nonce, Sig, DH enc.Bytes; Issued, Expires int64 }`
    (JSON `inst`, `user`, `nonce`, `sig`, `dh`, `iat`, `exp`), `(ClientCert) Validate() error`
  - `type session.LoginFinish struct{ Cert enc.Bytes; Proof proof.Proof; Password *proof.Proof; TOTP string }`
    (JSON `cert`, `proof`, `password`, `totp`)
  - `type session.APICert struct{ Installation, Node string; Client, DH enc.Bytes; Issued, Expires int64 }`
    (JSON `inst`, `node`, `client`, `dh`, `iat`, `exp`)
  - `type session.LoginResult struct{ API sign.Envelope; PasswordHint bool }` (JSON `api`, `passwordHint`)
  - `type session.TrustChain struct{ Versions []trust.Signed }` (JSON `versions`)
  - `type session.SetupInfo struct{ Installation string; Nodes []trust.Node; Now int64 }` (JSON `installation`, `nodes`, `now`)
  - `type session.Setup struct{ Code string; Trust trust.Signed }` (JSON `code`, `trust`)
  - `session.VerifyClient(payload []byte, p proof.Proof, l *trust.List, now int64) (ClientCert, error)`
  - `session.SignAPI(priv ed25519.PrivateKey, c APICert) (sign.Envelope, error)`
  - `session.VerifyAPI(env sign.Envelope, l *trust.List, client []byte, now int64) (APICert, error)`
  - `session.CertHash(client []byte) []byte` (SHA-256), `session.TokenHash(token []byte) []byte` (SHA-256),
    `session.ValidSetupCode(string) bool` (vier Gruppen à fünf Zeichen aus `A–H J–N P–Z 2–9`, getrennt durch `-`),
    `session.ValidTOTP(string) bool` (genau sechs Ziffern)
  - Fehler `session.ErrCert`, `ErrUser`, `ErrExpired`

- [ ] **Step 1: Test schreiben**

`contracts/session/session_test.go`:

```go
package session

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/trust/trusttest"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

const (
	issued = 1790000000
	now    = issued + 100
)

type fixture struct {
	list   *trust.List
	admin  trusttest.Signer
	client []byte
	proof  proof.Proof
	api    sign.Envelope
}

func newFixture(t testing.TB) fixture {
	t.Helper()
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	sessSig := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x51}, ed25519.SeedSize))
	sessDH, _ := trusttest.NodeKeys(0x52)
	client, err := json.Marshal(ClientCert{
		Installation: l.Installation, User: l.Users[0].ID, Nonce: bytes.Repeat([]byte{0x53}, NonceSize),
		Sig: enc.Bytes(sessSig.Public().(ed25519.PublicKey)), DH: sessDH.Public, Issued: issued, Expires: issued + 3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	apiDH, _ := trusttest.NodeKeys(0x54)
	_, nodeSign := trusttest.NodeKeys(0xA1) // key of "api-1" in trusttest.Genesis
	env, err := SignAPI(nodeSign, APICert{Installation: l.Installation, Node: "api-1", Client: CertHash(client),
		DH: apiDH.Public, Issued: issued, Expires: issued + 3600})
	if err != nil {
		t.Fatal(err)
	}
	return fixture{list: l, admin: admin, client: client, proof: admin.Prove(sign.PurposeClientCert, client), api: env}
}

func TestVerify(t *testing.T) {
	f := newFixture(t)
	c, err := VerifyClient(f.client, f.proof, f.list, now)
	if err != nil || c.User != f.list.Users[0].ID {
		t.Fatalf("client: %v", err)
	}
	a, err := VerifyAPI(f.api, f.list, f.client, now)
	if err != nil || a.Node != "api-1" {
		t.Fatalf("api: %v", err)
	}
}

func TestVerifyRejects(t *testing.T) {
	f := newFixture(t)
	if _, err := VerifyClient(f.client, f.proof, f.list, issued+3600); err != ErrExpired {
		t.Errorf("expired: %v", err)
	}
	if _, err := VerifyClient(f.client, f.proof, f.list, issued-MaxSkew-1); err != ErrExpired {
		t.Errorf("not yet valid: %v", err)
	}
	stranger := trusttest.NewSigner(9)
	if _, err := VerifyClient(f.client, stranger.Prove(sign.PurposeClientCert, f.client), f.list, now); err != ErrUser {
		t.Errorf("foreign key: %v", err)
	}
	if _, err := VerifyClient(f.client, f.admin.Prove(sign.PurposeTask, f.client), f.list, now); err != proof.ErrSignature {
		t.Errorf("other purpose: %v", err)
	}
	var c ClientCert
	if err := json.Unmarshal(f.client, &c); err != nil {
		t.Fatal(err)
	}
	c.Expires = c.Issued + MaxLifetime + 1
	long, _ := json.Marshal(c)
	if _, err := VerifyClient(long, f.admin.Prove(sign.PurposeClientCert, long), f.list, now); err != ErrCert {
		t.Errorf("lifetime: %v", err)
	}
	if _, err := VerifyAPI(f.api, f.list, long, now); err != ErrCert {
		t.Errorf("api cert for another client: %v", err)
	}
	var a APICert
	if err := json.Unmarshal(f.api.Payload, &a); err != nil {
		t.Fatal(err)
	}
	_, rogue := trusttest.NodeKeys(0x77)
	env, _ := SignAPI(rogue, a)
	if _, err := VerifyAPI(env, f.list, f.client, now); err != ErrCert {
		t.Errorf("rogue api: %v", err)
	}
}

func TestCodes(t *testing.T) {
	for _, s := range []string{"ABCDE-FGHJK-LMNPQ-RSTUV", "23456-789AB-CDEFG-HJKLM"} {
		if !ValidSetupCode(s) {
			t.Errorf("%s rejected", s)
		}
	}
	for _, s := range []string{"ABCDE-FGHJK-LMNPQ", "abcde-fghjk-lmnpq-rstuv", "ABCDE-FGHJK-LMNPQ-RSTU0", "ABCDE FGHJK LMNPQ RSTUV"} {
		if ValidSetupCode(s) {
			t.Errorf("%s accepted", s)
		}
	}
	if !ValidTOTP("012345") || ValidTOTP("12345") || ValidTOTP("12345a") || ValidTOTP("١٢٣٤٥٦") {
		t.Error("TOTP format")
	}
}

type vector struct {
	Trust  trust.Signed  `json:"trust"`
	Now    int64         `json:"now"`
	Client enc.Bytes     `json:"client"`
	Proof  proof.Proof   `json:"proof"`
	API    sign.Envelope `json:"api"`
}

func TestVectors(t *testing.T) {
	f := newFixture(t)
	golden.Check(t, *update, "session.json", vector{Trust: f.admin.Sign(t, f.list), Now: now, Client: f.client, Proof: f.proof, API: f.api})
}

func FuzzVerifyClient(f *testing.F) {
	fx := newFixture(f)
	f.Add(fx.client)
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = VerifyClient(b, fx.proof, fx.list, now)
	})
}
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

Run: `go test ./contracts/session/`
Expected: FAIL, `undefined: ClientCert`


- [ ] **Step 3: Umsetzung schreiben**

`contracts/session/session.go`:

```go
// Package session defines the login, the session certificates and the HTTP
// paths used before the WebSocket (contract V1).
package session

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
)

// HTTP paths on the web server; Apache passes /api/ on to the APIs.
const (
	PathSetup       = "/api/v1/setup"
	PathLoginStart  = "/api/v1/login/start"
	PathLoginFinish = "/api/v1/login/finish"
	PathLogout      = "/api/v1/logout"
	PathTrust       = "/api/v1/trust"
	PathSocket      = "/api/v1/ws"
	Subprotocol     = "ngc.v1"
	CookieName      = "ngc_session"
)

// Sizes and times in seconds.
const (
	TokenSize         = 32
	NonceSize         = 32
	MaxLifetime       = 86400 // longest session; the API may configure less
	MaxSkew           = 300   // tolerated clock difference between the servers
	ChallengeLifetime = 120
	MaxCert           = 4096
	MaxRequest        = 64 << 10 // largest JSON body before the WebSocket
	TOTPDigits        = 6
	TOTPPeriod        = 30 // RFC 6238 with HMAC-SHA1, as authenticator apps expect
)

// LoginStart asks for a login challenge.
type LoginStart struct {
	Name string `json:"name"`
}

// LoginChallenge tells the browser how the user logs in. For unknown names
// the API answers with made-up but stable values.
type LoginChallenge struct {
	Nonce        enc.Bytes   `json:"nonce"`
	Now          int64       `json:"now"`
	MaxLifetime  int64       `json:"maxLifetime"` // ClientCert.Expires <= Now + MaxLifetime
	Installation string      `json:"installation"`
	User         string      `json:"user"`
	KDF          *proof.KDF  `json:"kdf,omitempty"`      // set if the user may log in with a password
	Passkeys     []enc.Bytes `json:"passkeys,omitempty"` // credential IDs for allowCredentials
	TOTP         bool        `json:"totp"`
	PasswordToo  bool        `json:"passwordToo,omitempty"` // a role asks for the password in addition to a passkey
	RP           proof.RP    `json:"rp"`
}

// ClientCert binds the browser's session keys to a user. The browser builds
// it, the user signs it with a passkey or password key (purpose clientcert/v1).
type ClientCert struct {
	Installation string    `json:"inst"`
	User         string    `json:"user"`
	Nonce        enc.Bytes `json:"nonce"` // from LoginChallenge
	Sig          enc.Bytes `json:"sig"`   // Ed25519: signs tasks (package task)
	DH           enc.Bytes `json:"dh"`    // X25519: Noise key for the session and for end-to-end channels
	Issued       int64     `json:"iat"`
	Expires      int64     `json:"exp"`
}

// LoginFinish sends the signed client certificate.
type LoginFinish struct {
	Cert     enc.Bytes    `json:"cert"` // ClientCert as JSON, exactly the bytes the proof covers
	Proof    proof.Proof  `json:"proof"`
	Password *proof.Proof `json:"password,omitempty"` // second proof by the password key if a role asks for it
	TOTP     string       `json:"totp,omitempty"`
}

// APICert binds the API side of a session to the client certificate. An API
// node signs it with its key from the trust list (purpose apicert/v1).
type APICert struct {
	Installation string    `json:"inst"`
	Node         string    `json:"node"`
	Client       enc.Bytes `json:"client"` // CertHash of the client certificate
	DH           enc.Bytes `json:"dh"`     // X25519 key of the API for this session
	Issued       int64     `json:"iat"`
	Expires      int64     `json:"exp"`
}

// LoginResult answers LoginFinish; the session token comes as cookie.
type LoginResult struct {
	API          sign.Envelope `json:"api"`
	PasswordHint bool          `json:"passwordHint,omitempty"` // logged in with a password: show the hint to switch to passkeys
}

// TrustChain answers PathTrust?from=N with versions N to the newest.
type TrustChain struct {
	Versions []trust.Signed `json:"versions"`
}

// SetupInfo answers GET PathSetup while no admin exists.
type SetupInfo struct {
	Installation string       `json:"installation"`
	Nodes        []trust.Node `json:"nodes"` // the API servers for version 1 of the trust list
	Now          int64        `json:"now"`
}

// Setup creates the first admin with version 1 of the trust list.
type Setup struct {
	Code  string       `json:"code"` // one-time code shown by the ulh script
	Trust trust.Signed `json:"trust"`
}

var (
	ErrCert    = errors.New("session: malformed certificate")
	ErrUser    = errors.New("session: proof is not by a key of this user")
	ErrExpired = errors.New("session: certificate not valid at this time")
)

// Validate checks the fields of c.
func (c ClientCert) Validate() error {
	if !trust.ValidID(c.Installation) || !trust.ValidID(c.User) || len(c.Nonce) != NonceSize ||
		len(c.Sig) != ed25519.PublicKeySize || len(c.DH) != 32 ||
		c.Issued <= 0 || c.Expires <= c.Issued || c.Expires-c.Issued > MaxLifetime {
		return ErrCert
	}
	return nil
}

// CertHash returns the SHA-256 of a client certificate payload.
func CertHash(client []byte) []byte {
	h := sha256.Sum256(client)
	return h[:]
}

// TokenHash returns the SHA-256 of a session token; only the hash is stored.
func TokenHash(token []byte) []byte {
	h := sha256.Sum256(token)
	return h[:]
}

// VerifyClient checks a client certificate and the user's proof over it
// against the trust list. The API checks the nonce in addition.
func VerifyClient(payload []byte, p proof.Proof, l *trust.List, now int64) (ClientCert, error) {
	var c ClientCert
	if err := enc.DecodeStrict(payload, MaxCert, &c); err != nil {
		return ClientCert{}, ErrCert
	}
	if err := c.Validate(); err != nil || c.Installation != l.Installation {
		return ClientCert{}, ErrCert
	}
	owner, key, ok := l.KeyOwner(p.KeyID)
	if !ok || owner.ID != c.User {
		return ClientCert{}, ErrUser
	}
	if err := proof.Verify(p, key, l.RP, sign.PurposeClientCert, payload); err != nil {
		return ClientCert{}, err
	}
	if now < c.Issued-MaxSkew || now >= c.Expires {
		return ClientCert{}, ErrExpired
	}
	return c, nil
}

// SignAPI signs an API certificate with the key of an API node.
func SignAPI(priv ed25519.PrivateKey, c APICert) (sign.Envelope, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return sign.Envelope{}, err
	}
	return sign.Sign(priv, sign.PurposeAPICert, b)
}

// VerifyAPI checks an API certificate: signed by an API node of the trust
// list and bound to the client certificate client.
func VerifyAPI(env sign.Envelope, l *trust.List, client []byte, now int64) (APICert, error) {
	var node trust.Node
	for _, n := range l.Nodes {
		if n.Kind == trust.NodeAPI && sign.KeyID(n.Sign) == env.KeyID {
			node = n
		}
	}
	if node.ID == "" {
		return APICert{}, ErrCert
	}
	if err := sign.Verify(env, ed25519.PublicKey(node.Sign), sign.PurposeAPICert); err != nil {
		return APICert{}, err
	}
	var c APICert
	if err := enc.DecodeStrict(env.Payload, MaxCert, &c); err != nil {
		return APICert{}, ErrCert
	}
	if c.Installation != l.Installation || c.Node != node.ID || !bytes.Equal(c.Client, CertHash(client)) ||
		len(c.DH) != 32 || c.Issued <= 0 || c.Expires <= c.Issued || c.Expires-c.Issued > MaxLifetime {
		return APICert{}, ErrCert
	}
	if now < c.Issued-MaxSkew || now >= c.Expires {
		return APICert{}, ErrExpired
	}
	return c, nil
}

var setupCode = regexp.MustCompile(`^[A-HJ-NP-Z2-9]{5}(-[A-HJ-NP-Z2-9]{5}){3}$`)

// ValidSetupCode reports whether s looks like a one-time setup code.
func ValidSetupCode(s string) bool { return setupCode.MatchString(s) }

// ValidTOTP reports whether s is a TOTP code: exactly six ASCII digits.
func ValidTOTP(s string) bool {
	if len(s) != TOTPDigits {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Vektor erzeugen und Tests laufen lassen**

Run:

```bash
go test ./contracts/session/ -run TestVectors -update
go test ./contracts/session/
go test -run=XXX -fuzz=FuzzVerifyClient -fuzztime=30s ./contracts/session/
```

Expected: `ok`; `contracts/testdata/session.json` angelegt; Fuzzing ohne Fund.

- [ ] **Step 5: Commit**

```bash
git add contracts/session contracts/testdata/session.json
git commit -m "Add login and session certificates" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---


### Task 10: Unterschriebene Aufgaben und erneute Bestätigung (`contracts/task`)

Eine Aufgabe beschreibt genau einen Kanal: Server, App, Dienst, Aktion und Parameter. Die Shell
unterschreibt sie mit dem Sitzungsschlüssel `sig` (Zweck `task/v1`) und legt sie ins Feld `task`
des OPEN. Muss der Benutzer erneut bestätigen, unterschreibt er dieselben Bytes zusätzlich mit
Passkey oder Passwortschlüssel (Zweck `confirm/v1`, Feld `confirm`). Liegt eine Aufgabe bei, gelten
nur ihre Parameter, nicht die aus dem OPEN.

Wer prüft: der Controller bei allem, was auf seinem Server ankommt, die API bei Kern-Aktionen, die
sie selbst ausführt. Ob etwas als root läuft oder kritisch ist, entscheidet immer der Prüfende
selbst aus Manifest oder Kernliste (Task 16), nie der Absender. Die Aufgaben-ID merkt sich der
Prüfende bis zum Ablauf (`dbapi`, Task 17), eine zweite Aufgabe mit derselben ID lehnt er ab.

Zuordnung der Fehler zu `wire.Code`: `ErrNoTask` und `ErrConfirm` → `E_CONFIRM` (die Shell fragt den
Benutzer und versucht es erneut), `ErrForbidden` → `E_FORBIDDEN`, alle übrigen → `E_VERIFY`.

**Files:**
- Create: `contracts/task/task.go`, `contracts/task/task_test.go`, `contracts/testdata/task.json` (per `-update`)

**Interfaces:**
- Consumes: `enc.Bytes`, `enc.DecodeStrict`, `sign.Envelope`, `sign.Sign`, `sign.Verify`, `sign.PurposeTask`,
  `sign.PurposeConfirm`, `proof.Proof`, `proof.Verify`, `session.ClientCert`, `session.CertHash`, `session.MaxSkew`,
  `trust.List`, `trust.Op`, `trust.ValidID`, `wire.ValidName`, `wire.ValidAppID`, `wire.ValidParams`, `trusttest.*`, `golden.Check`.
- Produces:
  - Konstanten `task.IDSize = 16`, `MaxLifetime = 300`, `MaxTask = 16 << 10`
  - `type task.Task struct{ ID enc.Bytes; Installation string; Cert enc.Bytes; Node, App, Service, Action string; Params json.RawMessage; Issued, Expires int64 }`
    (JSON `id`, `inst`, `cert`, `node`, `app`, `service`, `action`, `params`, `iat`, `exp`), `(Task) Validate() error`
  - `task.Verify(env sign.Envelope, cert session.ClientCert, client []byte, now int64) (Task, error)`
  - `task.VerifyConfirm(p proof.Proof, env sign.Envelope, l *trust.List, user string) error`
  - `type task.Input struct{ Task *sign.Envelope; Confirm *proof.Proof; Cert session.ClientCert; Client []byte; Node, App, Service, Action string; Root, Critical, Changes bool }`
  - `task.Check(in Input, l *trust.List, now int64) (*Task, error)` – `nil, nil`, wenn keine Aufgabe nötig war und keine kam
  - Fehler `task.ErrTask`, `ErrNoTask`, `ErrExpired`, `ErrForbidden`, `ErrConfirm`

- [ ] **Step 1: Test schreiben**

`contracts/task/task_test.go`:

```go
package task

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/session"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/trust/trusttest"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

const (
	issued = 1790000000
	now    = issued + 10
)

var sessionSeed = bytes.Repeat([]byte{0x51}, ed25519.SeedSize)

type fixture struct {
	list    *trust.List
	admin   trusttest.Signer
	sessSig ed25519.PrivateKey
	cert    session.ClientCert
	client  []byte
}

func newFixture(t testing.TB) fixture {
	t.Helper()
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	sessSig := ed25519.NewKeyFromSeed(sessionSeed)
	sessDH, _ := trusttest.NodeKeys(0x52)
	cert := session.ClientCert{Installation: l.Installation, User: l.Users[0].ID, Nonce: bytes.Repeat([]byte{0x53}, session.NonceSize),
		Sig: enc.Bytes(sessSig.Public().(ed25519.PublicKey)), DH: sessDH.Public, Issued: issued, Expires: issued + 3600}
	client, err := json.Marshal(cert)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{list: l, admin: admin, sessSig: sessSig, cert: cert, client: client}
}

func (f fixture) task() Task {
	return Task{ID: bytes.Repeat([]byte{0x61}, IDSize), Installation: f.list.Installation, Cert: session.CertHash(f.client),
		Node: "ctl-1", App: "ngc.store", Service: "controller", Action: "activate",
		Params: json.RawMessage(`{"app":"org.sorglos.terminal","version":"1.0.0"}`), Issued: issued, Expires: issued + 60}
}

func signTask(t testing.TB, priv ed25519.PrivateKey, tk Task) sign.Envelope {
	t.Helper()
	b, err := json.Marshal(tk)
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.Sign(priv, sign.PurposeTask, b)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func (f fixture) input(env *sign.Envelope) Input {
	return Input{Task: env, Cert: f.cert, Client: f.client, Node: "ctl-1", App: "ngc.store", Service: "controller",
		Action: "activate", Root: true, Changes: true}
}

func TestCheck(t *testing.T) {
	f := newFixture(t)
	env := signTask(t, f.sessSig, f.task())
	got, err := Check(f.input(&env), f.list, now)
	if err != nil || got == nil || got.Action != "activate" {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestCheckRejects(t *testing.T) {
	f := newFixture(t)
	env := signTask(t, f.sessSig, f.task())
	_, stranger := trusttest.NodeKeys(0x77)
	foreign := signTask(t, stranger, f.task())
	long := f.task()
	long.Expires = long.Issued + MaxLifetime + 1
	longEnv := signTask(t, f.sessSig, long)
	cases := []struct {
		name   string
		mutate func(in *Input, now *int64)
		want   error
	}{
		{"root task without task", func(in *Input, _ *int64) { in.Task = nil }, ErrNoTask},
		{"other node", func(in *Input, _ *int64) { in.Node = "ctl-2" }, ErrTask},
		{"other action", func(in *Input, _ *int64) { in.Action = "deactivate" }, ErrTask},
		{"expired", func(_ *Input, now *int64) { *now = issued + 60 }, ErrExpired},
		{"too long", func(in *Input, _ *int64) { in.Task = &longEnv }, ErrTask},
		{"other session key", func(in *Input, _ *int64) { in.Task = &foreign }, sign.ErrWrongKey},
		{"other client certificate", func(in *Input, _ *int64) { in.Client = append([]byte(" "), f.client...) }, ErrTask},
	}
	for _, c := range cases {
		in, n := f.input(&env), int64(now)
		c.mutate(&in, &n)
		if _, err := Check(in, f.list, n); err != c.want {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
	noRoot := trusttest.Genesis(f.admin)
	noRoot.Nodes[1].RootRoles = nil
	if _, err := Check(f.input(&env), noRoot, now); err != ErrForbidden {
		t.Errorf("no root permission: %v", err)
	}
}

func TestConfirm(t *testing.T) {
	f := newFixture(t)
	f.list.Roles[0].Level = trust.LevelRoot
	env := signTask(t, f.sessSig, f.task())
	in := f.input(&env)
	if _, err := Check(in, f.list, now); err != ErrConfirm {
		t.Errorf("missing confirmation: %v", err)
	}
	good := f.admin.Prove(sign.PurposeConfirm, env.Payload)
	in.Confirm = &good
	if _, err := Check(in, f.list, now); err != nil {
		t.Errorf("confirmed: %v", err)
	}
	bad := trusttest.NewSigner(9).Prove(sign.PurposeConfirm, env.Payload)
	in.Confirm = &bad
	if _, err := Check(in, f.list, now); err != ErrConfirm {
		t.Errorf("foreign confirmation: %v", err)
	}
	wrong := f.admin.Prove(sign.PurposeTask, env.Payload)
	in.Confirm = &wrong
	if _, err := Check(in, f.list, now); err != ErrConfirm {
		t.Errorf("confirmation for another purpose: %v", err)
	}
}

func TestLevels(t *testing.T) {
	f := newFixture(t)
	shell := Input{Cert: f.cert, Client: f.client, Node: "ctl-1", App: "org.sorglos.terminal", Service: "terminal", Action: "shell", Changes: true}
	if tk, err := Check(shell, f.list, now); err != nil || tk != nil {
		t.Errorf("plain channel at level critical: %v %v", tk, err)
	}
	f.list.Roles[0].Level = trust.LevelHardcore
	if _, err := Check(shell, f.list, now); err != ErrNoTask {
		t.Errorf("plain channel at level hardcore: %v", err)
	}
}

type vector struct {
	Trust       trust.Signed  `json:"trust"`
	Now         int64         `json:"now"`
	SessionSeed enc.Bytes     `json:"sessionSeed"` // private test key, so JavaScript can reproduce the signature
	Client      enc.Bytes     `json:"client"`
	ClientProof proof.Proof   `json:"clientProof"`
	Task        sign.Envelope `json:"task"`
	Confirm     proof.Proof   `json:"confirm"`
}

func TestVectors(t *testing.T) {
	f := newFixture(t)
	env := signTask(t, f.sessSig, f.task())
	golden.Check(t, *update, "task.json", vector{
		Trust: f.admin.Sign(t, f.list), Now: now, SessionSeed: sessionSeed,
		Client: f.client, ClientProof: f.admin.Prove(sign.PurposeClientCert, f.client),
		Task: env, Confirm: f.admin.Prove(sign.PurposeConfirm, env.Payload),
	})
}

func FuzzVerify(f *testing.F) {
	fx := newFixture(f)
	f.Add([]byte(signTask(f, fx.sessSig, fx.task()).Payload))
	f.Fuzz(func(t *testing.T, payload []byte) {
		env, err := sign.Sign(fx.sessSig, sign.PurposeTask, payload)
		if err != nil {
			return
		}
		_, _ = Verify(env, fx.cert, fx.client, now)
	})
}
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

Run: `go test ./contracts/task/`
Expected: FAIL, `undefined: Task`


- [ ] **Step 3: Umsetzung schreiben**

`contracts/task/task.go`:

```go
// Package task defines signed tasks and re-confirmation. The shell signs a
// task with the session key; controllers and the API verify the chain from
// the task over the client certificate to the user's key in the trust list.
package task

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/session"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// Limits.
const (
	IDSize      = 16
	MaxLifetime = 300 // seconds from Issued to Expires
	MaxTask     = 16 << 10
)

// Task describes exactly one channel. If a task comes with an OPEN, its
// Params replace the ones in the OPEN.
type Task struct {
	ID           enc.Bytes       `json:"id"` // random; verifiers remember it until Expires
	Installation string          `json:"inst"`
	Cert         enc.Bytes       `json:"cert"` // session.CertHash of the client certificate whose key signs
	Node         string          `json:"node"` // target controller; empty for core actions the API carries out
	App          string          `json:"app"`
	Service      string          `json:"service"`
	Action       string          `json:"action"`
	Params       json.RawMessage `json:"params,omitempty"`
	Issued       int64           `json:"iat"`
	Expires      int64           `json:"exp"`
}

var (
	ErrTask      = errors.New("task: malformed task or other target")
	ErrNoTask    = errors.New("task: signed task required")
	ErrExpired   = errors.New("task: task not valid at this time")
	ErrForbidden = errors.New("task: no root permission on this server")
	ErrConfirm   = errors.New("task: re-confirmation missing or invalid")
)

// Validate checks the fields of t.
func (t Task) Validate() error {
	if len(t.ID) != IDSize || !trust.ValidID(t.Installation) || len(t.Cert) != 32 ||
		(t.Node != "" && !wire.ValidName(t.Node)) || !wire.ValidAppID(t.App) ||
		!wire.ValidName(t.Service) || !wire.ValidName(t.Action) || !wire.ValidParams(t.Params) ||
		t.Issued <= 0 || t.Expires <= t.Issued || t.Expires-t.Issued > MaxLifetime {
		return ErrTask
	}
	return nil
}

// Verify checks that env is a task signed with the session key of cert,
// bound to the client certificate payload client and valid at now.
func Verify(env sign.Envelope, cert session.ClientCert, client []byte, now int64) (Task, error) {
	if err := sign.Verify(env, ed25519.PublicKey(cert.Sig), sign.PurposeTask); err != nil {
		return Task{}, err
	}
	var t Task
	if err := enc.DecodeStrict(env.Payload, MaxTask, &t); err != nil {
		return Task{}, ErrTask
	}
	if err := t.Validate(); err != nil {
		return Task{}, err
	}
	if t.Installation != cert.Installation || !bytes.Equal(t.Cert, session.CertHash(client)) {
		return Task{}, ErrTask
	}
	if now < t.Issued-session.MaxSkew || now >= t.Expires {
		return Task{}, ErrExpired
	}
	return t, nil
}

// VerifyConfirm checks a re-confirmation of the task in env by a key of user.
func VerifyConfirm(p proof.Proof, env sign.Envelope, l *trust.List, user string) error {
	owner, key, ok := l.KeyOwner(p.KeyID)
	if !ok || owner.ID != user {
		return ErrConfirm
	}
	return proof.Verify(p, key, l.RP, sign.PurposeConfirm, env.Payload)
}

// Input is what the verifier knows about a channel. Root, Critical and
// Changes come from the manifest or the core list, never from the sender.
type Input struct {
	Task    *sign.Envelope
	Confirm *proof.Proof
	Cert    session.ClientCert // already verified with session.VerifyClient
	Client  []byte             // its payload
	Node    string             // the verifier's own node; empty for the API
	App     string
	Service string
	Action  string
	Root     bool
	Critical bool
	Changes  bool // every channel to a service counts as a change
}

// Check decides whether a channel may open. It returns the verified task,
// or nil if none was needed and none came.
func Check(in Input, l *trust.List, now int64) (*Task, error) {
	op := trust.Op{App: in.App, Action: in.Action, Node: in.Node, Root: in.Root, Critical: in.Critical, Changes: in.Changes}
	confirm := l.NeedsConfirm(in.Cert.User, op)
	if in.Task == nil {
		if in.Root || confirm {
			return nil, ErrNoTask
		}
		return nil, nil
	}
	t, err := Verify(*in.Task, in.Cert, in.Client, now)
	if err != nil {
		return nil, err
	}
	if t.Node != in.Node || t.App != in.App || t.Service != in.Service || t.Action != in.Action {
		return nil, ErrTask
	}
	if in.Root && !l.MayRoot(in.Cert.User, in.Node) {
		return nil, ErrForbidden
	}
	if confirm && (in.Confirm == nil || VerifyConfirm(*in.Confirm, *in.Task, l, in.Cert.User) != nil) {
		return nil, ErrConfirm
	}
	return &t, nil
}
```

- [ ] **Step 4: Vektor erzeugen und Tests laufen lassen**

Run:

```bash
go test ./contracts/task/ -run TestVectors -update
go test ./contracts/task/
go test -run=XXX -fuzz=FuzzVerify -fuzztime=30s ./contracts/task/
```

Expected: `ok`; `contracts/testdata/task.json` angelegt; Fuzzing ohne Fund.

- [ ] **Step 5: Commit**

```bash
git add contracts/task contracts/testdata/task.json
git commit -m "Add signed tasks and re-confirmation" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---


### Task 11: Tunnel zwischen API und Controller (`contracts/tunnel`)

Ablauf (V2):

1. **Kopplung, einmal pro Controller.** Ein Admin lässt sich in Benutzer & Rechte einen
   Kopplungscode geben (`PairCode`: Adresse und Noise-Schlüssel der API, Einmal-Geheimnis,
   Anker der Vertrauensliste). Das ulh-Skript des Controllers erzeugt dessen X25519-Schlüssel und
   schickt `POST <api>/tunnel/pair` mit `PairRequest`; das `mac` beweist das Geheimnis, ohne es zu
   verraten. Der Controller gilt danach als „wartend“ und zeigt seinen Fingerabdruck
   (`Fingerprint`). Erst wenn ein Admin ihn nach Vergleich des Fingerabdrucks in die
   Vertrauensliste einträgt, nimmt irgendwer ihn ernst. Den Anker aus dem Code merkt sich der
   Controller und prüft ab da jede Fassung selbst.
2. **Tunnel.** Der Controller verbindet sich zu jeder API aus der Vertrauensliste:
   `wss://<api>/tunnel?key=<Fingerprint des eigenen Schlüssels>`, Unterprotokoll `ngc.tunnel.v1`.
   TLS ist nur die Hülle für Firewalls und Proxys; die Echtheit prüft Noise_KK (Prolog
   `ngc/tunnel/v1`, Controller ist Initiator). Über `key` findet die API den Schlüssel des
   Controllers, bevor sie Nachricht 1 liest. Danach ist jede WebSocket-Nachricht eine
   Noise-Transportnachricht mit einem Frame.
3. **Steuerkanal 0.** Beide schicken zuerst `hello` (`Hello`). Die API schickt danach mit `trust`
   (Rumpf `session.TrustChain`) alle Fassungen nach `Hello.TrustSeq` des Controllers und später jede
   neue. Der Controller meldet mit `status` (`Status`) seine Apps. `update` beschreibt Task 14,
   `notify` Task 16. Wer die Leitung schließt, schickt `bye`.
4. **Kanäle.** Nur die API öffnet Kanäle. Nutzlast des OPEN ist `tunnel.Open`: das OPEN des
   Browsers plus Benutzer, Client-Zertifikat und dessen Beweis, damit der Controller die ganze
   Kette selbst prüft (`session.VerifyClient`, `task.Check`, Noise-Antwort mit dem
   Sitzungsschlüssel `dh`). Ohne Benutzer kommen nur Kanäle, die eine Dauerfreigabe deckt
   (automatische Updates, Task 14).
5. **Abbruch.** Beide Seiten bauen die Leitung selbst wieder auf, mit Pausen von `ReconnectMin`
   bis `ReconnectMax` Sekunden, verdoppelt je Versuch. Offene Kanäle gehen verloren.

**Files:**
- Create: `contracts/tunnel/tunnel.go`, `contracts/tunnel/tunnel_test.go`, `contracts/testdata/tunnel.json` (per `-update`)

**Interfaces:**
- Consumes: `enc.Bytes`, `enc.B64`, `enc.DecodeStrict`, `sign.KeyID`, `proof.Proof`, `session.MaxCert`,
  `trust.Anchor`, `trust.ValidID`, `wire.Open`, `wire.ErrOpen`, `wire.ValidName`, `wire.MaxOpenPayload`,
  `wire.DefaultWindow`, `wire.MinHandshake`, `trusttest.ID`, `golden.Check`.
- Produces:
  - Konstanten `tunnel.PathTunnel = "/tunnel"`, `PathPair = "/tunnel/pair"`, `Subprotocol = "ngc.tunnel.v1"`,
    `PairPrefix = "ngc-pair:"`, `SecretSize = 16`, `PairLifetime = 86400`, `MaxPairCode = 1024`, `MaxHello = 4096`,
    `PingInterval = 30`, `ReconnectMin = 1`, `ReconnectMax = 60`, `Contract = 1`
  - `type tunnel.PairCode struct{ URL string; Key, Secret enc.Bytes; Anchor trust.Anchor }` (JSON `url`, `key`, `secret`, `anchor`),
    `(PairCode) String() string`, `tunnel.ParsePairCode(string) (PairCode, error)`
  - `type tunnel.PairRequest struct{ Name string; Noise, MAC enc.Bytes }` (JSON `name`, `noise`, `mac`),
    `tunnel.ParsePairRequest([]byte) (PairRequest, error)`, `(PairRequest) Verify(secret, apiKey []byte) error`
  - `tunnel.PairMAC(secret, controllerKey, apiKey []byte) []byte` = HMAC-SHA256(secret, `"ngc/pair/v1" 0x00 controllerKey apiKey`)
  - `tunnel.Fingerprint(key []byte) string` (= `sign.KeyID`)
  - `type tunnel.Hello struct{ Version string; Contract int; Node string; TrustSeq uint64; Arch, OS string; Parts []string }`
    (JSON `version`, `contract`, `node`, `trustSeq`, `arch`, `os`, `parts`), `tunnel.ParseHello([]byte) (Hello, error)`;
    Teile `PartWeb = "web"`, `PartAPI = "api"`, `PartDB = "db"`, `PartServices = "services"`
  - `type tunnel.AppState string` (`StateRunning`, `StateStopped`, `StateFailed`, `StateUnlicensed`),
    `type tunnel.AppStatus struct{ App, Version string; State AppState }` (JSON `app`, `version`, `state`),
    `type tunnel.Status struct{ Apps []AppStatus; Parts map[string]string; DiskFree int64 }` (JSON `apps`, `parts`, `diskFree`)
  - `type tunnel.Open struct{ wire.Open; User string; Client enc.Bytes; Proof *proof.Proof }` (zusätzlich JSON `user`, `client`, `proof`),
    `tunnel.ParseOpen([]byte) (Open, error)`
  - Fehler `tunnel.ErrPairCode`, `ErrPair`, `ErrHello`

- [ ] **Step 1: Test schreiben**

`contracts/tunnel/tunnel_test.go`:

```go
package tunnel

import (
	"bytes"
	"encoding/json"
	"flag"
	"reflect"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

func testCode() PairCode {
	return PairCode{URL: "wss://api-1.test:7443/tunnel", Key: bytes.Repeat([]byte{0xA1}, 32),
		Secret: bytes.Repeat([]byte{0x5E}, SecretSize), Anchor: trust.Anchor{Seq: 3, Hash: bytes.Repeat([]byte{0xAB}, 32)}}
}

func TestPairCode(t *testing.T) {
	c := testCode()
	got, err := ParsePairCode(c.String())
	if err != nil || !reflect.DeepEqual(got, c) {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	short := c
	short.Secret = short.Secret[:8]
	for _, s := range []string{"", PairPrefix, PairPrefix + "!!", strings.TrimPrefix(c.String(), PairPrefix), short.String()} {
		if _, err := ParsePairCode(s); err != ErrPairCode {
			t.Errorf("%q: %v", s, err)
		}
	}
}

func TestPairRequest(t *testing.T) {
	c := testCode()
	ctl := bytes.Repeat([]byte{0xC1}, 32)
	r := PairRequest{Name: "dienste", Noise: ctl, MAC: PairMAC(c.Secret, ctl, c.Key)}
	b, _ := json.Marshal(r)
	got, err := ParsePairRequest(b)
	if err != nil || got.Verify(c.Secret, c.Key) != nil {
		t.Fatalf("valid request: %v", err)
	}
	swapped := r
	swapped.Noise = bytes.Repeat([]byte{0xC2}, 32)
	if swapped.Verify(c.Secret, c.Key) != ErrPair {
		t.Error("other controller key accepted")
	}
	if r.Verify(bytes.Repeat([]byte{1}, SecretSize), c.Key) != ErrPair {
		t.Error("other secret accepted")
	}
	if _, err := ParsePairRequest([]byte(`{"name":"a b","noise":"","mac":""}`)); err != ErrPair {
		t.Errorf("bad request: %v", err)
	}
}

func TestHello(t *testing.T) {
	h := Hello{Version: "1.0.0", Contract: Contract, Node: "ctl-1", TrustSeq: 3, Arch: "arm64", OS: "debian-13", Parts: []string{PartServices}}
	b, _ := json.Marshal(h)
	if _, err := ParseHello(b); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Hello){
		func(h *Hello) { h.Arch = "riscv64" },
		func(h *Hello) { h.Parts = []string{"mail"} },
		func(h *Hello) { h.Contract = 0 },
		func(h *Hello) { h.Version = "" },
		func(h *Hello) { h.Node = "Ctl 1" },
	} {
		bad := h
		mutate(&bad)
		b, _ := json.Marshal(bad)
		if _, err := ParseHello(b); err != ErrHello {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func testOpen() Open {
	return Open{
		Open: wire.Open{App: "org.sorglos.terminal", Service: "terminal", Action: "shell", Window: wire.DefaultWindow,
			E2E: bytes.Repeat([]byte{7}, wire.MinHandshake)},
		User: trusttest.ID(0x21), Client: []byte(`{"inst":"x"}`),
		Proof: &proof.Proof{Kind: proof.KindPassword, KeyID: "k", Sig: []byte{1}},
	}
}

func TestParseOpen(t *testing.T) {
	b, _ := json.Marshal(testOpen())
	if _, err := ParseOpen(b); err != nil {
		t.Fatalf("valid: %v", err)
	}
	auto := Open{Open: wire.Open{App: "ngc.store", Service: "controller", Action: "update", Window: wire.DefaultWindow}}
	b, _ = json.Marshal(auto)
	if _, err := ParseOpen(b); err != nil {
		t.Errorf("automatic update without user: %v", err)
	}
	noProof := testOpen()
	noProof.Proof = nil
	b, _ = json.Marshal(noProof)
	if _, err := ParseOpen(b); err == nil {
		t.Error("client certificate without proof accepted")
	}
	if _, err := ParseOpen([]byte(`{"app":"org.sorglos.terminal","service":"terminal","action":"shell","window":262144,"extra":1}`)); err == nil {
		t.Error("unknown field accepted")
	}
}

type vector struct {
	Code          string    `json:"code"`
	Secret        enc.Bytes `json:"secret"`
	APIKey        enc.Bytes `json:"apiKey"`
	ControllerKey enc.Bytes `json:"controllerKey"`
	MAC           enc.Bytes `json:"mac"`
	Fingerprint   string    `json:"fingerprint"`
}

func TestVectors(t *testing.T) {
	c := testCode()
	ctl := bytes.Repeat([]byte{0xC1}, 32)
	golden.Check(t, *update, "tunnel.json", vector{Code: c.String(), Secret: c.Secret, APIKey: c.Key,
		ControllerKey: ctl, MAC: PairMAC(c.Secret, ctl, c.Key), Fingerprint: Fingerprint(ctl)})
}

func FuzzParsePairCode(f *testing.F) {
	f.Add(testCode().String())
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = ParsePairCode(s)
	})
}

func FuzzParseOpen(f *testing.F) {
	b, _ := json.Marshal(testOpen())
	f.Add(b)
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParseOpen(b)
	})
}
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

Run: `go test ./contracts/tunnel/`
Expected: FAIL, `undefined: PairCode`


- [ ] **Step 3: Umsetzung schreiben**

`contracts/tunnel/tunnel.go`:

```go
// Package tunnel defines the link between API and controller (contract V2):
// pairing, the hello exchange, status reports and the OPEN payload the API
// sends to a controller.
package tunnel

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/session"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// Paths on the API listener, names and limits. Times in seconds.
const (
	PathTunnel   = "/tunnel"      // WebSocket; query key=<Fingerprint of the controller's Noise key>
	PathPair     = "/tunnel/pair" // POST PairRequest
	Subprotocol  = "ngc.tunnel.v1"
	PairPrefix   = "ngc-pair:"
	SecretSize   = 16
	PairLifetime = 86400
	MaxPairCode  = 1024
	MaxHello     = 4096
	PingInterval = 30
	ReconnectMin = 1
	ReconnectMax = 60
	Contract     = 1 // version of the contracts; every part also understands the previous one
)

// Parts of NGC a server can carry.
const (
	PartWeb      = "web"
	PartAPI      = "api"
	PartDB       = "db"
	PartServices = "services"
)

// PairCode is handed from the API to the ulh script of a new controller.
type PairCode struct {
	URL    string       `json:"url"`    // wss://host:port/tunnel of the issuing API
	Key    enc.Bytes    `json:"key"`    // its Noise key
	Secret enc.Bytes    `json:"secret"` // one use, valid for PairLifetime
	Anchor trust.Anchor `json:"anchor"` // trust list version the controller pins
}

// PairRequest is what the controller sends to PathPair.
type PairRequest struct {
	Name  string    `json:"name"`  // host name, shown to the admin
	Noise enc.Bytes `json:"noise"` // the controller's X25519 key
	MAC   enc.Bytes `json:"mac"`   // PairMAC(secret, Noise, API key)
}

// Hello is the first message on channel 0, in both directions.
type Hello struct {
	Version  string   `json:"version"` // NGC version of the sender
	Contract int      `json:"contract"`
	Node     string   `json:"node,omitempty"` // empty for a controller not yet in the trust list
	TrustSeq uint64   `json:"trustSeq"`       // newest trust list version the sender verified
	Arch     string   `json:"arch,omitempty"` // controller: amd64 or arm64
	OS       string   `json:"os,omitempty"`   // controller: e.g. debian-13, ubuntu-24.04
	Parts    []string `json:"parts,omitempty"`
}

// AppState is the state of an app on a controller.
type AppState string

const (
	StateRunning    AppState = "running"
	StateStopped    AppState = "stopped"
	StateFailed     AppState = "failed"
	StateUnlicensed AppState = "unlicensed" // subscription ran out after the grace period
)

// AppStatus reports one app.
type AppStatus struct {
	App     string   `json:"app"`
	Version string   `json:"version"`
	State   AppState `json:"state"`
}

// Status is the body of a status message from a controller.
type Status struct {
	Apps     []AppStatus       `json:"apps"`
	Parts    map[string]string `json:"parts"`    // installed NGC parts and their versions, e.g. "ngc-controller": "1.0.0"
	DiskFree int64             `json:"diskFree"` // bytes free for app data
}

// Open is the OPEN payload from the API to a controller: the browser's OPEN
// plus who asks. Without User only channels covered by a standing approval come.
type Open struct {
	wire.Open
	User   string       `json:"user,omitempty"`
	Client enc.Bytes    `json:"client,omitempty"` // client certificate payload
	Proof  *proof.Proof `json:"proof,omitempty"`  // the user's proof over it
}

var (
	ErrPairCode = errors.New("tunnel: malformed pairing code")
	ErrPair     = errors.New("tunnel: pairing request rejected")
	ErrHello    = errors.New("tunnel: malformed hello")
)

// String encodes c for copying: PairPrefix followed by base64url of the JSON.
func (c PairCode) String() string {
	b, _ := json.Marshal(c)
	return PairPrefix + enc.B64.EncodeToString(b)
}

// ParsePairCode decodes and checks a pairing code.
func ParsePairCode(s string) (PairCode, error) {
	s = strings.TrimSpace(s)
	rest, ok := strings.CutPrefix(s, PairPrefix)
	if !ok || len(s) > MaxPairCode {
		return PairCode{}, ErrPairCode
	}
	b, err := enc.B64.DecodeString(rest)
	if err != nil {
		return PairCode{}, ErrPairCode
	}
	var c PairCode
	if err := enc.DecodeStrict(b, MaxPairCode, &c); err != nil {
		return PairCode{}, ErrPairCode
	}
	if !strings.HasPrefix(c.URL, "wss://") || len(c.URL) > 256 || len(c.Key) != 32 || len(c.Secret) != SecretSize ||
		c.Anchor.Seq == 0 || len(c.Anchor.Hash) != sha256.Size {
		return PairCode{}, ErrPairCode
	}
	return c, nil
}

// PairMAC binds a controller key and an API key to the pairing secret.
func PairMAC(secret, controllerKey, apiKey []byte) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("ngc/pair/v1"))
	m.Write([]byte{0})
	m.Write(controllerKey)
	m.Write(apiKey)
	return m.Sum(nil)
}

var hostName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,62}$`)

// ParsePairRequest decodes and checks a pairing request; Verify checks the MAC.
func ParsePairRequest(b []byte) (PairRequest, error) {
	var r PairRequest
	if err := enc.DecodeStrict(b, 4096, &r); err != nil {
		return PairRequest{}, ErrPair
	}
	if !hostName.MatchString(r.Name) || len(r.Noise) != 32 || len(r.MAC) != sha256.Size {
		return PairRequest{}, ErrPair
	}
	return r, nil
}

// Verify checks the MAC with the secret the API issued and its own Noise key.
func (r PairRequest) Verify(secret, apiKey []byte) error {
	if !hmac.Equal(r.MAC, PairMAC(secret, r.Noise, apiKey)) {
		return ErrPair
	}
	return nil
}

// Fingerprint names a Noise key for people and in the tunnel URL.
func Fingerprint(key []byte) string { return sign.KeyID(key) }

var version = regexp.MustCompile(`^[0-9a-z.+-]{1,32}$`)

// ParseHello decodes and checks a hello message.
func ParseHello(b []byte) (Hello, error) {
	var h Hello
	if err := enc.DecodeStrict(b, MaxHello, &h); err != nil {
		return Hello{}, ErrHello
	}
	if !version.MatchString(h.Version) || h.Contract < 1 || (h.Node != "" && !wire.ValidName(h.Node)) ||
		(h.Arch != "" && h.Arch != "amd64" && h.Arch != "arm64") || len(h.OS) > 32 || len(h.Parts) > 4 {
		return Hello{}, ErrHello
	}
	for _, p := range h.Parts {
		if !slices.Contains([]string{PartWeb, PartAPI, PartDB, PartServices}, p) {
			return Hello{}, ErrHello
		}
	}
	return h, nil
}

// ParseOpen decodes and checks the OPEN payload on the tunnel.
func ParseOpen(p []byte) (Open, error) {
	var o Open
	if err := enc.DecodeStrict(p, wire.MaxOpenPayload, &o); err != nil {
		return Open{}, err
	}
	if err := o.Open.Validate(); err != nil {
		return Open{}, err
	}
	noUser := o.User == ""
	if noUser != (len(o.Client) == 0) || noUser != (o.Proof == nil) || len(o.Client) > session.MaxCert ||
		(!noUser && !trust.ValidID(o.User)) {
		return Open{}, wire.ErrOpen
	}
	return o, nil
}
```

- [ ] **Step 4: Vektor erzeugen und Tests laufen lassen**

Run:

```bash
go test ./contracts/tunnel/ -run TestVectors -update
go test ./contracts/tunnel/
go test -run=XXX -fuzz=FuzzParsePairCode -fuzztime=30s ./contracts/tunnel/
go test -run=XXX -fuzz=FuzzParseOpen -fuzztime=30s ./contracts/tunnel/
```

Expected: `ok`; `contracts/testdata/tunnel.json` angelegt; Fuzzing ohne Fund.

- [ ] **Step 5: Commit**

```bash
git add contracts/tunnel contracts/testdata/tunnel.json
git commit -m "Add pairing and the tunnel between API and controller" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---


### Task 12: Dienst im Container (`contracts/service`)

Vertrag V3 für jeden, der einen Dienst schreibt, egal in welcher Sprache:

- Pro Kanal startet der Controller das Programm aus dem Manifest im laufenden Container der App
  (heute `podman exec -i`), ohne Shell, mit genau den Umgebungsvariablen aus `Call.Env`.
- DATA-Frames kommen entschlüsselt auf stdin an; EOF vom Browser schließt stdin. Was das Programm
  auf stdout schreibt, geht als DATA zurück, bei seinem Ende folgen EOF und CLOSE nach `Result`.
- stderr landet im Protokoll des Controllers, höchstens `MaxStderr` Byte pro Kanal.
- Aufträge enden nach `timeout` aus dem Manifest (Standard 30 Sekunden); Aktionen mit `stream`
  (etwa das Terminal) haben kein Zeitlimit.
- Im Container: `/data` ist der verschlüsselte Datenordner der App (beschreibbar), `/tmp` liegt im
  Speicher, alles andere ist schreibgeschützt. Kein Netz, keine Capabilities, kein root, außer das
  Manifest verlangt es und der Admin hat es beim Aktivieren erlaubt.
- Startaktion: Nach jedem Start des Containers ruft der Controller die Aktion `start` aus dem Manifest
  selbst auf, ohne Benutzer und ohne API, mit `NGC_START=1`.

**Files:**
- Create: `contracts/service/service.go`, `contracts/service/service_test.go`, `contracts/testdata/service/echo.sh`

**Interfaces:**
- Consumes: `wire.Close`, `wire.Error`, `wire.StatusOK`, `wire.StatusError`, `wire.CodeExit`, `wire.CodeTimeout`,
  `wire.CodeUnreachable`, `wire.ValidAppID`, `wire.ValidName`, `wire.ValidParams`, `trust.ValidID`.
- Produces:
  - Umgebungsvariablen `service.EnvUser = "NGC_USER"`, `EnvUserName = "NGC_USER_NAME"`, `EnvApp = "NGC_APP"`,
    `EnvService = "NGC_SERVICE"`, `EnvAction = "NGC_ACTION"`, `EnvParams = "NGC_PARAMS"`, `EnvVersion = "NGC_VERSION"`,
    `EnvData = "NGC_DATA"`, `EnvStart = "NGC_START"`
  - Konstanten `service.DataDir = "/data"`, `DefaultTimeout = 30`, `MaxTimeout = 3600`, `MaxStderr = 64 << 10`,
    `Path = "/usr/local/bin:/usr/bin:/bin"`
  - `type service.Call struct{ User, UserName, App, Service, Action, Version string; Params json.RawMessage; Start bool }`,
    `(Call) Validate() error`, `(Call) Env() []string`
  - `service.Result(exitCode int, timedOut bool, startErr error) wire.Close`
  - Fehler `service.ErrCall`

- [ ] **Step 1: Test schreiben**

`contracts/testdata/service/echo.sh`:

```sh
#!/bin/sh
# Test service for contract V3: echoes stdin to stdout, reports who asked on
# stderr and exits with the code given in NGC_PARAMS, e.g. {"exit":2}.
set -eu
printf '%s %s %s %s\n' "$NGC_APP" "$NGC_SERVICE" "$NGC_ACTION" "$NGC_USER" >&2
cat
code=$(printf '%s' "${NGC_PARAMS:-}" | sed -n 's/.*"exit":\([0-9][0-9]*\).*/\1/p')
exit "${code:-0}"
```

`contracts/service/service_test.go`:

```go
package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

func testCall() Call {
	return Call{User: "ISEhISEhISEhISEhISEhIQ", UserName: "admin", App: "org.sorglos.terminal", Service: "terminal",
		Action: "shell", Version: "1.0.0", Params: json.RawMessage(`{"cols":80}`)}
}

func TestEnv(t *testing.T) {
	got := testCall().Env()
	want := []string{
		"LANG=C.UTF-8",
		"NGC_ACTION=shell",
		"NGC_APP=org.sorglos.terminal",
		"NGC_DATA=/data",
		`NGC_PARAMS={"cols":80}`,
		"NGC_SERVICE=terminal",
		"NGC_START=0",
		"NGC_USER=ISEhISEhISEhISEhISEhIQ",
		"NGC_USER_NAME=admin",
		"NGC_VERSION=1.0.0",
		"PATH=" + Path,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
	start := Call{App: "org.sorglos.firewall", Service: "rules", Action: "start", Version: "2.1.0", Start: true}
	if err := start.Validate(); err != nil || !slices.Contains(start.Env(), "NGC_START=1") || !slices.Contains(start.Env(), "NGC_USER=") {
		t.Errorf("start action: %v %q", err, start.Env())
	}
}

func TestValidate(t *testing.T) {
	for name, mutate := range map[string]func(*Call){
		"app":             func(c *Call) { c.App = "Terminal" },
		"action":          func(c *Call) { c.Action = "rm -rf" },
		"params":          func(c *Call) { c.Params = json.RawMessage(`[1]`) },
		"user":            func(c *Call) { c.User = "root" },
		"start with user": func(c *Call) { c.Start = true },
		"name with newline": func(c *Call) { c.UserName = "admin\nNGC_USER=x" },
	} {
		c := testCall()
		mutate(&c)
		if err := c.Validate(); err != ErrCall {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestResult(t *testing.T) {
	if c := Result(0, false, nil); c.Status != wire.StatusOK {
		t.Errorf("exit 0: %+v", c)
	}
	c := Result(2, false, nil)
	if c.Code != wire.CodeExit || c.ExitCode == nil || *c.ExitCode != 2 || c.Message != "Dienst mit Code 2 beendet." || c.Validate() != nil {
		t.Errorf("exit 2: %+v", c)
	}
	if c := Result(-1, true, nil); c.Code != wire.CodeTimeout {
		t.Errorf("timeout: %+v", c)
	}
	if c := Result(-1, false, errors.New("no container")); c.Code != wire.CodeUnreachable {
		t.Errorf("start error: %+v", c)
	}
	if c := Result(-1, false, nil); c.Code != wire.CodeExit || c.ExitCode != nil || c.Validate() != nil {
		t.Errorf("killed: %+v", c)
	}
}

// TestEchoService runs the reference service the way the controller does,
// only without a container.
func TestEchoService(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("reference service needs a POSIX shell")
	}
	for _, exit := range []int{0, 2} {
		c := testCall()
		c.Params = json.RawMessage(`{"exit":` + string(rune('0'+exit)) + `}`)
		cmd := exec.Command("sh", filepath.Join("..", "testdata", "service", "echo.sh"))
		cmd.Env = c.Env()
		cmd.Stdin = bytes.NewReader([]byte("cp -r a b\n"))
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			t.Fatal(err)
		}
		got := Result(cmd.ProcessState.ExitCode(), false, nil)
		if stdout.String() != "cp -r a b\n" || (exit == 0) != (got.Status == wire.StatusOK) {
			t.Errorf("exit %d: stdout %q, close %+v, stderr %q", exit, stdout.String(), got, stderr.String())
		}
	}
}
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

Run: `go test ./contracts/service/`
Expected: FAIL, `undefined: Call`

- [ ] **Step 3: Umsetzung schreiben**

`contracts/service/service.go`:

```go
// Package service defines how the controller runs the program of a service
// inside its app container (contract V3).
package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// Environment variables a service program gets; there are no others.
const (
	EnvUser     = "NGC_USER"      // user ID; empty for the start action
	EnvUserName = "NGC_USER_NAME" // login name, for display only
	EnvApp      = "NGC_APP"
	EnvService  = "NGC_SERVICE"
	EnvAction   = "NGC_ACTION"
	EnvParams   = "NGC_PARAMS" // JSON object or empty
	EnvVersion  = "NGC_VERSION"
	EnvData     = "NGC_DATA"  // always DataDir
	EnvStart    = "NGC_START" // "1" for the start action, else "0"
)

// Paths and limits. Times in seconds.
const (
	DataDir        = "/data"
	DefaultTimeout = 30
	MaxTimeout     = 3600
	MaxStderr      = 64 << 10
	Path           = "/usr/local/bin:/usr/bin:/bin"
)

// Call describes one run of a service program.
type Call struct {
	User, UserName, App, Service, Action, Version string
	Params                                        json.RawMessage
	Start                                         bool
}

// ErrCall reports a call the controller must not start.
var ErrCall = errors.New("service: invalid call")

// Validate checks c before anything is started.
func (c Call) Validate() error {
	if !wire.ValidAppID(c.App) || !wire.ValidName(c.Service) || !wire.ValidName(c.Action) ||
		!wire.ValidParams(c.Params) || !printable(c.Version) || !printable(c.UserName) {
		return ErrCall
	}
	if c.Start != (c.User == "") || (c.User != "" && !trust.ValidID(c.User)) {
		return ErrCall
	}
	return nil
}

func printable(s string) bool {
	if len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// Env returns the complete, sorted environment for the program.
func (c Call) Env() []string {
	start := "0"
	if c.Start {
		start = "1"
	}
	env := []string{
		"LANG=C.UTF-8",
		"PATH=" + Path,
		EnvUser + "=" + c.User,
		EnvUserName + "=" + c.UserName,
		EnvApp + "=" + c.App,
		EnvService + "=" + c.Service,
		EnvAction + "=" + c.Action,
		EnvParams + "=" + strings.TrimSpace(string(c.Params)),
		EnvVersion + "=" + c.Version,
		EnvData + "=" + DataDir,
		EnvStart + "=" + start,
	}
	slices.Sort(env)
	return env
}

// Result maps how the program ended to the CLOSE payload: startErr if it
// could not be started, timedOut if the controller stopped it, else the
// exit code (negative if it was killed by a signal).
func Result(exitCode int, timedOut bool, startErr error) wire.Close {
	switch {
	case startErr != nil:
		return wire.Error(wire.CodeUnreachable)
	case timedOut:
		return wire.Error(wire.CodeTimeout)
	case exitCode == 0:
		return wire.Close{Status: wire.StatusOK}
	case exitCode < 0 || exitCode > 255:
		return wire.Error(wire.CodeExit)
	}
	code := exitCode
	return wire.Close{Status: wire.StatusError, Code: wire.CodeExit, Message: fmt.Sprintf("Dienst mit Code %d beendet.", code), ExitCode: &code}
}
```

- [ ] **Step 4: Tests laufen lassen**

Run: `go test ./contracts/service/`
Expected: `ok` (auf Windows wird `TestEchoService` übersprungen, auf den Servern läuft er).

- [ ] **Step 5: Commit**

```bash
git add contracts/service contracts/testdata/service
git update-index --chmod=+x contracts/testdata/service/echo.sh
git commit -m "Add the contract between controller and service" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---


### Task 13: App-Manifest und Versionen (`contracts/store`, Teil 1)

Aufbau eines App-Pakets (unkomprimiertes tar, Task 15):

```
manifest.json          zuerst im Archiv
ui/…                   nur HTML, CSS, JS, Bilder, Schriften
services/amd64.tar     Container-Image der App (OCI-Archiv), je Architektur
services/arm64.tar
```

Abweichung vom Bild im Entwurf 4.4 (`services/terminal/`), zur Klärung: Jede App bekommt genau
einen Container (4.3), also gibt es ein Image pro App und Architektur. Die Dienste sind Programme
in diesem Image, das Manifest nennt für jeden das Kommando. Den Hauptprozess des Containers stellt
der Controller, das Image muss dafür nichts mitbringen.

Aus den Aktionen im Manifest werden die Rechte; dazu kommt immer das Grundrecht `use`
(„benutzen“, Task 16). Kritische Aktionen (`critical`) verlangen in der Stufe „Kritisch“ eine
erneute Bestätigung. Versionen sind immer `MAJOR.MINOR.PATCH` ohne Zusätze.

**Files:**
- Create: `contracts/store/manifest.go`, `contracts/store/manifest_test.go`

**Interfaces:**
- Consumes: `enc.DecodeStrict`, `wire.ValidAppID`, `wire.ValidName`, `wire.CoreApp`, `service.MaxTimeout`.
- Produces:
  - Konstanten `store.ManifestFile = "manifest.json"`, `MaxManifest = 64 << 10`, `RuntimePodman = "podman"`,
    `NeedRoot = "root"`, `NeedNetwork = "network"`, `DefaultCPU = 1000` (Tausendstel Kerne), `DefaultMemory = 256` (MiB)
  - `type store.Manifest struct{ ID, Name, Version, Publisher, Description, Icon string; UI *UI; Actions []Action; Services []Service; Start, Runtime string; Needs []string; CPU, Memory int; MinNGC string }`
    (JSON `id`, `name`, `version`, `publisher`, `description`, `icon`, `ui`, `actions`, `services`, `start`, `runtime`, `needs`, `cpu`, `memory`, `minNgc`)
  - `type store.UI struct{ Entry string; Width, Height int }` (JSON `entry`, `width`, `height`)
  - `type store.Action struct{ Name, Title, Service string; Stream bool; Timeout int; Critical bool }`
    (JSON `name`, `title`, `service`, `stream`, `timeout`, `critical`)
  - `type store.Service struct{ Name string; Command []string }` (JSON `name`, `command`)
  - `(*Manifest) Validate() error`, `(*Manifest) Action(name string) (Action, bool)`, `store.ParseManifest([]byte) (*Manifest, error)`
  - `store.ValidVersion(string) bool`, `store.Newer(a, b string) bool`, `store.ValidPath(string) bool`
  - Fehler `store.ErrManifest`

- [ ] **Step 1: Test schreiben**

`contracts/store/manifest_test.go`:

```go
package store

import (
	"encoding/json"
	"testing"
)

func terminalManifest() *Manifest {
	return &Manifest{
		ID: "org.sorglos.terminal", Name: "Terminal", Version: "1.0.0", Publisher: "Sorglos IT",
		Icon: "icon.svg", UI: &UI{Entry: "index.html", Width: 720, Height: 480},
		Actions:  []Action{{Name: "shell", Title: "Befehle auf dem Server ausführen", Service: "terminal", Stream: true}},
		Services: []Service{{Name: "terminal", Command: []string{"/usr/local/bin/ngc-terminal"}}},
		Memory:   128,
	}
}

func TestManifest(t *testing.T) {
	b, _ := json.Marshal(terminalManifest())
	m, err := ParseManifest(b)
	if err != nil {
		t.Fatal(err)
	}
	if a, ok := m.Action("shell"); !ok || !a.Stream {
		t.Error("action lookup")
	}
	calculator := &Manifest{ID: "org.sorglos.calculator", Name: "Rechner", Version: "1.0.0", Publisher: "Sorglos IT", UI: &UI{Entry: "index.html"}}
	if err := calculator.Validate(); err != nil {
		t.Errorf("UI-only app: %v", err)
	}
}

func TestManifestRejects(t *testing.T) {
	cases := map[string]func(m *Manifest){
		"core app id":        func(m *Manifest) { m.ID = "ngc.store" },
		"version":            func(m *Manifest) { m.Version = "1.0" },
		"leading zero":       func(m *Manifest) { m.Version = "1.01.0" },
		"path traversal":     func(m *Manifest) { m.UI.Entry = "../index.html" },
		"absolute icon":      func(m *Manifest) { m.Icon = "/etc/passwd" },
		"hidden file":        func(m *Manifest) { m.Icon = ".icon.svg" },
		"unknown service":    func(m *Manifest) { m.Actions[0].Service = "mail" },
		"stream with timeout": func(m *Manifest) { m.Actions[0].Timeout = 10 },
		"relative command":   func(m *Manifest) { m.Services[0].Command = []string{"ngc-terminal"} },
		"duplicate action":   func(m *Manifest) { m.Actions = append(m.Actions, m.Actions[0]) },
		"unknown need":       func(m *Manifest) { m.Needs = []string{"host"} },
		"lxc not yet":        func(m *Manifest) { m.Runtime = "lxc" },
		"start is a stream":  func(m *Manifest) { m.Start = "shell" },
		"memory":             func(m *Manifest) { m.Memory = 1 },
		"nothing to do":      func(m *Manifest) { m.UI, m.Actions, m.Services = nil, nil, nil },
	}
	for name, mutate := range cases {
		m := terminalManifest()
		mutate(m)
		if err := m.Validate(); err != ErrManifest {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ParseManifest([]byte(`{"id":"org.sorglos.terminal","script":"rm -rf /"}`)); err != ErrManifest {
		t.Errorf("unknown field: %v", err)
	}
}

func TestVersions(t *testing.T) {
	cases := []struct {
		a, b  string
		newer bool
	}{
		{"1.0.1", "1.0.0", true},
		{"1.10.0", "1.9.9", true},
		{"2.0.0", "10.0.0", false},
		{"1.0.0", "1.0.0", false},
		{"0.9.0", "1.0.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.newer {
			t.Errorf("Newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
	for _, v := range []string{"1", "1.0", "v1.0.0", "1.0.0-beta", "01.0.0", "1.0.0.0", "1234567.0.0"} {
		if ValidVersion(v) {
			t.Errorf("%s accepted", v)
		}
	}
}

func FuzzParseManifest(f *testing.F) {
	b, _ := json.Marshal(terminalManifest())
	f.Add(b)
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParseManifest(b)
	})
}
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

Run: `go test ./contracts/store/`
Expected: FAIL, `undefined: Manifest`

- [ ] **Step 3: Umsetzung schreiben**

`contracts/store/manifest.go`:

```go
// Package store defines app packages, store catalogs, licenses, NGC
// releases and the encryption of paid apps (contract V5).
package store

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/service"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// Names and limits of the manifest.
const (
	ManifestFile  = "manifest.json"
	MaxManifest   = 64 << 10
	RuntimePodman = "podman"
	NeedRoot      = "root"    // root inside the container
	NeedNetwork   = "network" // access to the network
	DefaultCPU    = 1000      // thousandths of a core
	DefaultMemory = 256       // MiB
)

// Manifest describes an app. It is manifest.json in the app package.
type Manifest struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Publisher   string    `json:"publisher"`
	Description string    `json:"description,omitempty"`
	Icon        string    `json:"icon,omitempty"` // path below ui/
	UI          *UI       `json:"ui,omitempty"`
	Actions     []Action  `json:"actions,omitempty"`
	Services    []Service `json:"services,omitempty"`
	Start       string    `json:"start,omitempty"`   // action the controller runs after every container start
	Runtime     string    `json:"runtime,omitempty"` // RuntimePodman; "lxc" is reserved for later
	Needs       []string  `json:"needs,omitempty"`   // NeedRoot, NeedNetwork; the admin must allow them
	CPU         int       `json:"cpu,omitempty"`     // thousandths of a core, default DefaultCPU
	Memory      int       `json:"memory,omitempty"`  // MiB, default DefaultMemory
	MinNGC      string    `json:"minNgc,omitempty"`
}

// UI is the browser part of the app.
type UI struct {
	Entry  string `json:"entry"` // path below ui/
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// Action is something a user may be allowed to do; it becomes a right.
type Action struct {
	Name     string `json:"name"`
	Title    string `json:"title"` // shown when an admin grants the right
	Service  string `json:"service"`
	Stream   bool   `json:"stream,omitempty"`  // stays open, no time limit
	Timeout  int    `json:"timeout,omitempty"` // seconds, default service.DefaultTimeout
	Critical bool   `json:"critical,omitempty"`
}

// Service is a program in the app's image.
type Service struct {
	Name    string   `json:"name"`
	Command []string `json:"command"` // absolute path and arguments, started without a shell
}

// ErrManifest reports an invalid manifest.
var ErrManifest = errors.New("store: invalid manifest")

// ParseManifest decodes and validates manifest.json.
func ParseManifest(b []byte) (*Manifest, error) {
	var m Manifest
	if err := enc.DecodeStrict(b, MaxManifest, &m); err != nil {
		return nil, ErrManifest
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Action returns the action with name.
func (m *Manifest) Action(name string) (Action, bool) {
	i := slices.IndexFunc(m.Actions, func(a Action) bool { return a.Name == name })
	if i < 0 {
		return Action{}, false
	}
	return m.Actions[i], true
}

// Validate checks m.
func (m *Manifest) Validate() error {
	if !wire.ValidAppID(m.ID) || wire.CoreApp(m.ID) || !label(m.Name, 64) || !label(m.Publisher, 64) ||
		!ValidVersion(m.Version) || len(m.Description) > 1000 || !utf8.ValidString(m.Description) ||
		(m.Icon != "" && !ValidPath(m.Icon)) || (m.MinNGC != "" && !ValidVersion(m.MinNGC)) {
		return ErrManifest
	}
	if m.UI == nil && len(m.Services) == 0 {
		return ErrManifest
	}
	if m.UI != nil && (!ValidPath(m.UI.Entry) || !size(m.UI.Width) || !size(m.UI.Height)) {
		return ErrManifest
	}
	services := map[string]bool{}
	for _, s := range m.Services {
		if !wire.ValidName(s.Name) || services[s.Name] || len(s.Command) == 0 || len(s.Command) > 32 ||
			!strings.HasPrefix(s.Command[0], "/") {
			return ErrManifest
		}
		for _, arg := range s.Command {
			if arg == "" || len(arg) > 256 || strings.ContainsRune(arg, 0) {
				return ErrManifest
			}
		}
		services[s.Name] = true
	}
	actions := map[string]bool{}
	for _, a := range m.Actions {
		if !wire.ValidName(a.Name) || actions[a.Name] || !label(a.Title, 100) || !services[a.Service] ||
			a.Timeout < 0 || a.Timeout > service.MaxTimeout || (a.Stream && a.Timeout != 0) {
			return ErrManifest
		}
		actions[a.Name] = true
	}
	if m.Start != "" {
		if a, ok := m.Action(m.Start); !ok || a.Stream {
			return ErrManifest
		}
	}
	if m.Runtime != "" && m.Runtime != RuntimePodman {
		return ErrManifest
	}
	if !validNeeds(m.Needs) {
		return ErrManifest
	}
	if m.CPU != 0 && (m.CPU < 100 || m.CPU > 64000) || m.Memory != 0 && (m.Memory < 16 || m.Memory > 65536) {
		return ErrManifest
	}
	return nil
}

func size(v int) bool { return v == 0 || (v >= 200 && v <= 4000) }

func validNeeds(needs []string) bool {
	for i, n := range needs {
		if (n != NeedRoot && n != NeedNetwork) || slices.Contains(needs[:i], n) {
			return false
		}
	}
	return true
}

// label allows printable UTF-8 text of 1 to max bytes.
func label(s string, max int) bool {
	if s == "" || len(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

var (
	versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})$`)
	pathPattern    = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]*(/[A-Za-z0-9_-][A-Za-z0-9._-]*)*$`)
)

// ValidVersion reports whether s is MAJOR.MINOR.PATCH without leading zeros.
func ValidVersion(s string) bool { return versionPattern.MatchString(s) }

// ValidPath reports whether p is a safe relative path: no leading slash, no
// "..", no hidden files, only letters, digits and . _ - (at most 200 bytes).
func ValidPath(p string) bool { return len(p) <= 200 && pathPattern.MatchString(p) }

// Newer reports whether version a is higher than version b. Invalid versions are never newer.
func Newer(a, b string) bool {
	x, okA := parseVersion(a)
	y, okB := parseVersion(b)
	if !okA || !okB {
		return false
	}
	return slices.Compare(x[:], y[:]) > 0
}

func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	if !ValidVersion(s) {
		return v, false
	}
	for i, part := range strings.Split(s, ".") {
		v[i], _ = strconv.Atoi(part)
	}
	return v, true
}
```

- [ ] **Step 4: Tests laufen lassen**

Run:

```bash
go test ./contracts/store/
go test -run=XXX -fuzz=FuzzParseManifest -fuzztime=30s ./contracts/store/
```

Expected: `ok`; Fuzzing ohne Fund.

- [ ] **Step 5: Commit**

```bash
git add contracts/store
git commit -m "Add the app manifest and version rules" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---


### Task 14: Katalog, Lizenz, NGC-Release und Updates (`contracts/store`, Teil 2)

- **Katalog:** Der Store signiert ihn mit seinem Schlüssel (Zweck `catalog/v1`); welcher Schlüssel
  gilt, steht in der Vertrauensliste (`trust.Store`). `seq` steigt mit jedem Katalog, ältere lehnen
  API und Controller ab. Kauf-Apps sind immer verschlüsselt, Gratis-Apps nie.
- **Lizenz:** Der Store-Server stellt sie für genau eine Installation aus (Zweck `license/v1`). Darin
  steckt der Schlüssel der App, verschlüsselt für den Installationsschlüssel (Task 15). Ein Abo
  läuft nach `expires` noch die Übergangsfrist `grace` weiter (Standard 14 Tage), danach starten die
  Controller die Dienste nicht mehr; gelöscht wird nichts.
- **NGC-Release:** Ein eigener Katalog, signiert mit dem Release-Schlüssel (Zweck `release/v1`),
  mit einer Datei je Teil und Architektur. Die Datenbank-Migrationen stecken in der Datei der API.
- **Updates:** Die Dateien kommen über Kanäle zum Controller (Kern-Aktionen `stage` und `install`,
  Task 16), weil ein Katalog nicht in eine Steuernachricht passt. Den Fortschritt meldet der
  Controller über den Steuerkanal (`update`, Rumpf `UpdateReport`). Ohne Unterschrift eines
  Benutzers installiert er nur, was eine Dauerfreigabe deckt (`Approved`) und neuer ist als das
  Installierte (`Newer`); ältere Versionen nie. Die Modi `auto`, `semi` und `notify` stellt der Admin
  pro Teil und App ein; wann installiert wird, steuert die API.

**Files:**
- Create: `contracts/store/catalog.go`, `contracts/store/catalog_test.go`, `contracts/testdata/store.json` (per `-update`)

**Interfaces:**
- Consumes: `enc.Bytes`, `enc.DecodeStrict`, `sign.Envelope`, `sign.Sign`, `sign.Verify`, `sign.PurposeCatalog`,
  `sign.PurposeLicense`, `sign.PurposeRelease`, `sign.MaxPayload`, `trust.Store`, `trust.List`, `trust.Auto`, `trust.ValidID`,
  `trust.AutoWeb`, `trust.AutoAPI`, `trust.AutoController`, `trust.AutoSystem`, `wire.ValidAppID`, `wire.ValidName`,
  `wire.CoreApp`, `ValidVersion`, `Newer`, `ValidPath`, `label`, `validNeeds` (Task 13), `trusttest.*`, `golden.Check`.
- Produces:
  - Konstanten `store.PriceBuy = "buy"`, `PriceSubscription = "subscription"`, `PerSite = "site"`, `PerUser = "user"`,
    `PeriodMonth = "month"`, `PeriodYear = "year"`, `MaxPackage = 4 << 30`, `MaxApps = 10000`, `MaxVersions = 100`,
    `MaxLicense = 8 << 10`, `GraceDefault = 14 * 86400`, `MaxGrace = 90 * 86400`,
    `ReleaseWeb = "web"`, `ReleaseAPI = "api"`, `ReleaseController = "controller"`,
    `ModeAuto = "auto"`, `ModeSemi = "semi"`, `ModeNotify = "notify"`,
    `UpdateAvailable`, `UpdateDownloaded`, `UpdateInstalling`, `UpdateInstalled`, `UpdateRolledBack`, `UpdateFailed`, `UpdateRejected`
  - `type store.Catalog struct{ Store string; Seq uint64; Issued int64; Apps []Entry }` (JSON `store`, `seq`, `issued`, `apps`)
  - `type store.Entry struct{ ID, Name, Publisher, Description string; Price *Price; Versions []Package }`
    (JSON `id`, `name`, `publisher`, `description`, `price`, `versions`)
  - `type store.Package struct{ Version, URL string; Size int64; SHA256 enc.Bytes; Encrypted bool; Needs []string; MinNGC string }`
    (JSON `version`, `url`, `size`, `sha256`, `encrypted`, `needs`, `minNgc`)
  - `type store.Price struct{ Model, Per string; Amount int64; Currency, Period, Shop string }`
    (JSON `model`, `per`, `amount`, `currency`, `period`, `shop`)
  - `type store.WrappedKey struct{ Ephemeral, Nonce, Sealed enc.Bytes }` (JSON `eph`, `nonce`, `sealed`)
  - `type store.License struct{ ID, Store, App, Installation string; InstallKey enc.Bytes; Per string; Users int; Issued, Expires, Grace int64; Key *WrappedKey }`
    (JSON `id`, `store`, `app`, `installation`, `installKey`, `per`, `users`, `issued`, `expires`, `grace`, `key`)
  - `type store.LicenseState int` (`LicenseValid`, `LicenseGrace`, `LicenseExpired`), `(*License) State(now int64) LicenseState`
  - `type store.Release struct{ Seq uint64; Issued int64; Version, Notes string; Files []File }` (JSON `seq`, `issued`, `version`, `notes`, `files`)
  - `type store.File struct{ Part, Arch, URL string; Size int64; SHA256 enc.Bytes }` (JSON `part`, `arch`, `url`, `size`, `sha256`)
  - `type store.UpdateReport struct{ Target, Version, State, Message string }` (JSON `target`, `version`, `state`, `message`),
    `store.ParseUpdateReport([]byte) (UpdateReport, error)`
  - `(*Catalog) Validate() error`, `(*License) Validate() error`, `(*Release) Validate() error`
  - `store.VerifyCatalog(env sign.Envelope, st trust.Store, lastSeq uint64) (*Catalog, error)`
  - `store.VerifyLicense(env sign.Envelope, st trust.Store, installation string, installKey []byte) (*License, error)`
  - `store.VerifyRelease(env sign.Envelope, key ed25519.PublicKey, lastSeq uint64) (*Release, error)`
  - `store.Approved(l *trust.List, target string, key []byte) bool`
  - `store.ValidTarget(string) bool` (Ziel eines Updates oder einer Dauerfreigabe)
  - Fehler `store.ErrCatalog`, `ErrLicense`, `ErrRelease`, `ErrUpdate`, `ErrRollback`

- [ ] **Step 1: Test schreiben**

`contracts/store/catalog_test.go`:

```go
package store

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/trust/trusttest"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

const issued = 1790000000

var (
	storeKey   = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, ed25519.SeedSize))
	releaseKey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x72}, ed25519.SeedSize))
	global     = trust.Store{ID: "global", Name: "NGC Store", URL: "https://store.ngc.test", Key: enc.Bytes(storeKey.Public().(ed25519.PublicKey))}
	install, _ = trusttest.NodeKeys(0x91) // X25519 key pair of the installation
)

func signJSON(t testing.TB, priv ed25519.PrivateKey, purpose string, v any) sign.Envelope {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	env, err := sign.Sign(priv, purpose, b)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func testCatalog() Catalog {
	sum := bytes.Repeat([]byte{0x01}, 32)
	return Catalog{Store: "global", Seq: 5, Issued: issued, Apps: []Entry{
		{ID: "org.sorglos.calculator", Name: "Rechner", Publisher: "Sorglos IT",
			Versions: []Package{{Version: "1.0.0", URL: "org.sorglos.calculator-1.0.0.ngc", Size: 12345, SHA256: sum}}},
		{ID: "org.sorglos.terminal", Name: "Terminal", Publisher: "Sorglos IT",
			Price: &Price{Model: PriceSubscription, Per: PerSite, Amount: 499, Currency: "EUR", Period: PeriodMonth, Shop: "https://store.ngc.test/buy/terminal"},
			Versions: []Package{
				{Version: "1.0.0", URL: "https://cdn.ngc.test/terminal-1.0.0.ngc", Size: 5000000, SHA256: sum, Encrypted: true},
				{Version: "1.1.0", URL: "https://cdn.ngc.test/terminal-1.1.0.ngc", Size: 5100000, SHA256: sum, Encrypted: true},
			}},
	}}
}

func testLicense() License {
	return License{ID: "L-1", Store: "global", App: "org.sorglos.terminal", Installation: trusttest.ID(0x11),
		InstallKey: install.Public, Per: PerUser, Users: 5, Issued: issued, Expires: issued + 30*86400,
		Key: &WrappedKey{Ephemeral: bytes.Repeat([]byte{2}, 32), Nonce: bytes.Repeat([]byte{3}, 12), Sealed: bytes.Repeat([]byte{4}, 48)}}
}

func testRelease() Release {
	sum := bytes.Repeat([]byte{0x05}, 32)
	f := func(part, arch string) File {
		return File{Part: part, Arch: arch, URL: "https://releases.ngc.test/1.1.0/" + part + "-" + arch + ".tar", Size: 1000, SHA256: sum}
	}
	return Release{Seq: 3, Issued: issued, Version: "1.1.0", Notes: "Fehler behoben.", Files: []File{
		f(ReleaseWeb, "all"), f(ReleaseAPI, "amd64"), f(ReleaseAPI, "arm64"), f(ReleaseController, "amd64"), f(ReleaseController, "arm64"),
	}}
}

func TestCatalog(t *testing.T) {
	env := signJSON(t, storeKey, sign.PurposeCatalog, testCatalog())
	if c, err := VerifyCatalog(env, global, 5); err != nil || len(c.Apps) != 2 {
		t.Fatalf("valid: %v", err)
	}
	if _, err := VerifyCatalog(env, global, 6); err != ErrRollback {
		t.Errorf("older catalog: %v", err)
	}
	evil := testCatalog()
	evil.Store = "evil"
	if _, err := VerifyCatalog(signJSON(t, storeKey, sign.PurposeCatalog, evil), global, 0); err != ErrCatalog {
		t.Errorf("other store: %v", err)
	}
	other := global
	other.Key = enc.Bytes(releaseKey.Public().(ed25519.PublicKey))
	if _, err := VerifyCatalog(env, other, 0); err != sign.ErrWrongKey {
		t.Errorf("other key: %v", err)
	}
}

func TestCatalogRejects(t *testing.T) {
	cases := map[string]func(c *Catalog){
		"unsorted":             func(c *Catalog) { c.Apps[0], c.Apps[1] = c.Apps[1], c.Apps[0] },
		"paid but open":        func(c *Catalog) { c.Apps[1].Versions[0].Encrypted = false },
		"free but encrypted":   func(c *Catalog) { c.Apps[0].Versions[0].Encrypted = true },
		"versions not rising":  func(c *Catalog) { c.Apps[1].Versions[1].Version = "0.9.0" },
		"short checksum":       func(c *Catalog) { c.Apps[0].Versions[0].SHA256 = []byte{1} },
		"subscription period":  func(c *Catalog) { c.Apps[1].Price.Period = "" },
		"url with ..":          func(c *Catalog) { c.Apps[0].Versions[0].URL = "../secret.ngc" },
		"plain http":           func(c *Catalog) { c.Apps[1].Versions[0].URL = "http://cdn.ngc.test/x.ngc" },
		"core app":             func(c *Catalog) { c.Apps[0].ID = "ngc.calculator" },
	}
	for name, mutate := range cases {
		c := testCatalog()
		mutate(&c)
		if err := c.Validate(); err != ErrCatalog {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestLicense(t *testing.T) {
	env := signJSON(t, storeKey, sign.PurposeLicense, testLicense())
	l, err := VerifyLicense(env, global, trusttest.ID(0x11), install.Public)
	if err != nil {
		t.Fatalf("valid: %v", err)
	}
	if _, err := VerifyLicense(env, global, trusttest.ID(0x12), install.Public); err != ErrLicense {
		t.Errorf("other installation: %v", err)
	}
	if _, err := VerifyLicense(env, global, trusttest.ID(0x11), bytes.Repeat([]byte{9}, 32)); err != ErrLicense {
		t.Errorf("other installation key: %v", err)
	}
	for _, c := range []struct {
		now  int64
		want LicenseState
	}{
		{issued + 86400, LicenseValid},
		{l.Expires, LicenseGrace},
		{l.Expires + GraceDefault - 1, LicenseGrace},
		{l.Expires + GraceDefault, LicenseExpired},
	} {
		if got := l.State(c.now); got != c.want {
			t.Errorf("State(%d) = %v, want %v", c.now, got, c.want)
		}
	}
	bought := testLicense()
	bought.Expires = 0
	if bought.State(issued+100*365*86400) != LicenseValid {
		t.Error("a bought license never expires")
	}
	noUsers := testLicense()
	noUsers.Users = 0
	if noUsers.Validate() != ErrLicense {
		t.Error("per-user license without user count accepted")
	}
}

func TestRelease(t *testing.T) {
	env := signJSON(t, releaseKey, sign.PurposeRelease, testRelease())
	pub := releaseKey.Public().(ed25519.PublicKey)
	if _, err := VerifyRelease(env, pub, 3); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if _, err := VerifyRelease(env, pub, 4); err != ErrRollback {
		t.Errorf("older release: %v", err)
	}
	for name, mutate := range map[string]func(r *Release){
		"duplicate file": func(r *Release) { r.Files = append(r.Files, r.Files[1]) },
		"unknown part":   func(r *Release) { r.Files[0].Part = "db" },
		"web per arch":   func(r *Release) { r.Files[0].Arch = "amd64" },
		"version":        func(r *Release) { r.Version = "1.1" },
	} {
		r := testRelease()
		mutate(&r)
		if err := r.Validate(); err != ErrRelease {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestUpdates(t *testing.T) {
	for _, target := range []string{trust.AutoWeb, trust.AutoSystem, "org.sorglos.terminal"} {
		if !ValidTarget(target) {
			t.Errorf("%s rejected", target)
		}
	}
	for _, target := range []string{"ngc-db", "ngc.store", ""} {
		if ValidTarget(target) {
			t.Errorf("%q accepted", target)
		}
	}
	r, _ := json.Marshal(UpdateReport{Target: trust.AutoAPI, Version: "1.1.0", State: UpdateRolledBack, Message: "API startete nicht, alte Version wiederhergestellt."})
	if _, err := ParseUpdateReport(r); err != nil {
		t.Errorf("report: %v", err)
	}
	if _, err := ParseUpdateReport([]byte(`{"target":"ngc-api","state":"exploded"}`)); err != ErrUpdate {
		t.Errorf("bad report: %v", err)
	}
	l := trusttest.Genesis(trusttest.NewSigner(1))
	l.Auto = []trust.Auto{{Target: trust.AutoController, Key: enc.Bytes(releaseKey.Public().(ed25519.PublicKey))}, {Target: trust.AutoSystem}}
	pub := releaseKey.Public().(ed25519.PublicKey)
	if !Approved(l, trust.AutoController, pub) || Approved(l, trust.AutoAPI, pub) ||
		Approved(l, trust.AutoController, storeKey.Public().(ed25519.PublicKey)) || !Approved(l, trust.AutoSystem, nil) {
		t.Error("standing approvals")
	}
}

type vector struct {
	StoreKey   enc.Bytes     `json:"storeKey"`
	Catalog    sign.Envelope `json:"catalog"`
	License    sign.Envelope `json:"license"`
	ReleaseKey enc.Bytes     `json:"releaseKey"`
	Release    sign.Envelope `json:"release"`
}

func TestVectors(t *testing.T) {
	golden.Check(t, *update, "store.json", vector{
		StoreKey:   global.Key,
		Catalog:    signJSON(t, storeKey, sign.PurposeCatalog, testCatalog()),
		License:    signJSON(t, storeKey, sign.PurposeLicense, testLicense()),
		ReleaseKey: enc.Bytes(releaseKey.Public().(ed25519.PublicKey)),
		Release:    signJSON(t, releaseKey, sign.PurposeRelease, testRelease()),
	})
}

func FuzzCatalog(f *testing.F) {
	b, _ := json.Marshal(testCatalog())
	f.Add(b)
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = parseCatalog(b)
		_, _ = parseLicense(b)
		_, _ = parseRelease(b)
		_, _ = ParseUpdateReport(b)
	})
}
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

Run: `go test ./contracts/store/`
Expected: FAIL, `undefined: Catalog`


- [ ] **Step 3: Umsetzung schreiben**

`contracts/store/catalog.go`:

```go
package store

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// Prices, limits and update states. Times in seconds.
const (
	PriceBuy          = "buy"
	PriceSubscription = "subscription"
	PerSite           = "site"
	PerUser           = "user"
	PeriodMonth       = "month"
	PeriodYear        = "year"
	MaxPackage        = 4 << 30
	MaxApps           = 10000
	MaxVersions       = 100
	MaxLicense        = 8 << 10
	GraceDefault      = 14 * 86400
	MaxGrace          = 90 * 86400

	ReleaseWeb        = "web"
	ReleaseAPI        = "api" // includes ngc-store and the database migrations
	ReleaseController = "controller"

	ModeAuto   = "auto"   // install in the maintenance window (default); needs a standing approval
	ModeSemi   = "semi"   // download and check in advance, install on click
	ModeNotify = "notify" // only tell; download and install on click

	UpdateAvailable  = "available"
	UpdateDownloaded = "downloaded"
	UpdateInstalling = "installing"
	UpdateInstalled  = "installed"
	UpdateRolledBack = "rolledback"
	UpdateFailed     = "failed"
	UpdateRejected   = "rejected" // signature, approval or version check failed
)

// Catalog lists the apps of a store.
type Catalog struct {
	Store  string  `json:"store"`
	Seq    uint64  `json:"seq"`
	Issued int64   `json:"issued"`
	Apps   []Entry `json:"apps"` // sorted by ID
}

// Entry is an app in a catalog.
type Entry struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Publisher   string    `json:"publisher"`
	Description string    `json:"description,omitempty"`
	Price       *Price    `json:"price,omitempty"` // nil: free
	Versions    []Package `json:"versions"`        // ascending
}

// Package is one version of an app.
type Package struct {
	Version   string    `json:"version"`
	URL       string    `json:"url"` // https URL or a path relative to the catalog
	Size      int64     `json:"size"`
	SHA256    enc.Bytes `json:"sha256"`              // of the file as downloaded
	Encrypted bool      `json:"encrypted,omitempty"` // paid apps (Task 15)
	Needs     []string  `json:"needs,omitempty"`     // as in the manifest, shown before download
	MinNGC    string    `json:"minNgc,omitempty"`
}

// Price of a paid app. Payment happens on the store's shop page, never in NGC.
type Price struct {
	Model    string `json:"model"`
	Per      string `json:"per"`
	Amount   int64  `json:"amount"`   // smallest unit of the currency, e.g. cents
	Currency string `json:"currency"` // ISO 4217
	Period   string `json:"period,omitempty"`
	Shop     string `json:"shop"`
}

// WrappedKey is the content key of a paid app, encrypted for one installation (Task 15).
type WrappedKey struct {
	Ephemeral enc.Bytes `json:"eph"`
	Nonce     enc.Bytes `json:"nonce"`
	Sealed    enc.Bytes `json:"sealed"`
}

// License allows one installation to use a paid app.
type License struct {
	ID           string      `json:"id"`
	Store        string      `json:"store"`
	App          string      `json:"app"`
	Installation string      `json:"installation"`
	InstallKey   enc.Bytes   `json:"installKey"` // X25519 key the content key is wrapped for
	Per          string      `json:"per"`
	Users        int         `json:"users,omitempty"` // per user: how many users may get the right "use"
	Issued       int64       `json:"issued"`
	Expires      int64       `json:"expires,omitempty"` // subscription; 0 for a bought app
	Grace        int64       `json:"grace,omitempty"`   // 0 means GraceDefault
	Key          *WrappedKey `json:"key"`
}

// LicenseState tells whether a paid app may run.
type LicenseState int

const (
	LicenseValid   LicenseState = iota
	LicenseGrace                // expired, but within the grace period: runs, NGC warns
	LicenseExpired              // controllers do not start the services
)

// Release is a signed NGC version.
type Release struct {
	Seq     uint64 `json:"seq"`
	Issued  int64  `json:"issued"`
	Version string `json:"version"`
	Notes   string `json:"notes,omitempty"`
	Files   []File `json:"files"`
}

// File is one part of NGC for one architecture ("all" for the web interface).
type File struct {
	Part   string    `json:"part"`
	Arch   string    `json:"arch"`
	URL    string    `json:"url"`
	Size   int64     `json:"size"`
	SHA256 enc.Bytes `json:"sha256"`
}

// UpdateReport is the body of an "update" control message from a controller.
type UpdateReport struct {
	Target  string `json:"target"`
	Version string `json:"version,omitempty"`
	State   string `json:"state"`
	Message string `json:"message,omitempty"` // German, shown to admins
}

var (
	ErrCatalog  = errors.New("store: invalid catalog")
	ErrLicense  = errors.New("store: invalid license")
	ErrRelease  = errors.New("store: invalid release")
	ErrUpdate   = errors.New("store: invalid update message")
	ErrRollback = errors.New("store: older than the last one seen")
)

var (
	currency  = regexp.MustCompile(`^[A-Z]{3}$`)
	licenseID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

func validURL(u string) bool {
	if strings.HasPrefix(u, "https://") {
		return len(u) > len("https://") && len(u) <= 512 && !strings.ContainsAny(u, " \t\r\n\"'<>\\")
	}
	return ValidPath(u)
}

// Validate checks c.
func (c *Catalog) Validate() error {
	if !wire.ValidName(c.Store) || c.Seq == 0 || c.Issued <= 0 || len(c.Apps) > MaxApps {
		return ErrCatalog
	}
	for i, e := range c.Apps {
		if (i > 0 && c.Apps[i-1].ID >= e.ID) || !e.valid() {
			return ErrCatalog
		}
	}
	return nil
}

func (e Entry) valid() bool {
	if !wire.ValidAppID(e.ID) || wire.CoreApp(e.ID) || !label(e.Name, 64) || !label(e.Publisher, 64) ||
		len(e.Description) > 1000 || !utf8.ValidString(e.Description) || len(e.Versions) == 0 || len(e.Versions) > MaxVersions ||
		(e.Price != nil && !e.Price.valid()) {
		return false
	}
	for i, p := range e.Versions {
		if !ValidVersion(p.Version) || (i > 0 && !Newer(p.Version, e.Versions[i-1].Version)) || !validURL(p.URL) ||
			p.Size <= 0 || p.Size > MaxPackage || len(p.SHA256) != 32 || p.Encrypted != (e.Price != nil) ||
			!validNeeds(p.Needs) || (p.MinNGC != "" && !ValidVersion(p.MinNGC)) {
			return false
		}
	}
	return true
}

func (p Price) valid() bool {
	sub := p.Model == PriceSubscription
	return (p.Model == PriceBuy || sub) && (p.Per == PerSite || p.Per == PerUser) && p.Amount > 0 &&
		currency.MatchString(p.Currency) && sub == (p.Period != "") &&
		(!sub || p.Period == PeriodMonth || p.Period == PeriodYear) &&
		strings.HasPrefix(p.Shop, "https://") && validURL(p.Shop)
}

func parseCatalog(b []byte) (*Catalog, error) {
	var c Catalog
	if err := enc.DecodeStrict(b, sign.MaxPayload, &c); err != nil {
		return nil, ErrCatalog
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// VerifyCatalog checks the signature of the store st and rejects catalogs
// older than lastSeq, the newest one seen from this store.
func VerifyCatalog(env sign.Envelope, st trust.Store, lastSeq uint64) (*Catalog, error) {
	if err := sign.Verify(env, ed25519.PublicKey(st.Key), sign.PurposeCatalog); err != nil {
		return nil, err
	}
	c, err := parseCatalog(env.Payload)
	if err != nil {
		return nil, err
	}
	if c.Store != st.ID {
		return nil, ErrCatalog
	}
	if c.Seq < lastSeq {
		return nil, ErrRollback
	}
	return c, nil
}

// Validate checks l.
func (l *License) Validate() error {
	if !licenseID.MatchString(l.ID) || !wire.ValidName(l.Store) || !wire.ValidAppID(l.App) ||
		!trust.ValidID(l.Installation) || len(l.InstallKey) != 32 || (l.Per != PerSite && l.Per != PerUser) ||
		(l.Per == PerUser) != (l.Users > 0) || l.Users > 100000 || l.Issued <= 0 ||
		(l.Expires != 0 && l.Expires <= l.Issued) || l.Grace < 0 || l.Grace > MaxGrace ||
		l.Key == nil || len(l.Key.Ephemeral) != 32 || len(l.Key.Nonce) != 12 || len(l.Key.Sealed) != 32+16 {
		return ErrLicense
	}
	return nil
}

// State tells whether the app may run at now.
func (l *License) State(now int64) LicenseState {
	if l.Expires == 0 || now < l.Expires {
		return LicenseValid
	}
	grace := l.Grace
	if grace == 0 {
		grace = GraceDefault
	}
	if now < l.Expires+grace {
		return LicenseGrace
	}
	return LicenseExpired
}

func parseLicense(b []byte) (*License, error) {
	var l License
	if err := enc.DecodeStrict(b, MaxLicense, &l); err != nil {
		return nil, ErrLicense
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return &l, nil
}

// VerifyLicense checks the signature of the store st and that the license
// belongs to this installation and its key.
func VerifyLicense(env sign.Envelope, st trust.Store, installation string, installKey []byte) (*License, error) {
	if err := sign.Verify(env, ed25519.PublicKey(st.Key), sign.PurposeLicense); err != nil {
		return nil, err
	}
	l, err := parseLicense(env.Payload)
	if err != nil {
		return nil, err
	}
	if l.Store != st.ID || l.Installation != installation || !bytes.Equal(l.InstallKey, installKey) {
		return nil, ErrLicense
	}
	return l, nil
}

// Validate checks r.
func (r *Release) Validate() error {
	if r.Seq == 0 || r.Issued <= 0 || !ValidVersion(r.Version) || len(r.Notes) > 10000 || !utf8.ValidString(r.Notes) ||
		len(r.Files) == 0 || len(r.Files) > 16 {
		return ErrRelease
	}
	seen := map[string]bool{}
	for _, f := range r.Files {
		arch := (f.Part == ReleaseWeb && f.Arch == "all") ||
			((f.Part == ReleaseAPI || f.Part == ReleaseController) && (f.Arch == "amd64" || f.Arch == "arm64"))
		key := f.Part + "/" + f.Arch
		if !arch || seen[key] || !validURL(f.URL) || f.Size <= 0 || f.Size > MaxPackage || len(f.SHA256) != 32 {
			return ErrRelease
		}
		seen[key] = true
	}
	return nil
}

func parseRelease(b []byte) (*Release, error) {
	var r Release
	if err := enc.DecodeStrict(b, sign.MaxPayload, &r); err != nil {
		return nil, ErrRelease
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

// VerifyRelease checks the signature of the release key and rejects releases older than lastSeq.
func VerifyRelease(env sign.Envelope, key ed25519.PublicKey, lastSeq uint64) (*Release, error) {
	if err := sign.Verify(env, key, sign.PurposeRelease); err != nil {
		return nil, err
	}
	r, err := parseRelease(env.Payload)
	if err != nil {
		return nil, err
	}
	if r.Seq < lastSeq {
		return nil, ErrRollback
	}
	return r, nil
}

// ValidTarget reports whether t names something that can be updated: an NGC
// part (trust.AutoWeb, AutoAPI, AutoController), the system packages
// (trust.AutoSystem) or an app.
func ValidTarget(t string) bool {
	return t == trust.AutoWeb || t == trust.AutoAPI || t == trust.AutoController || t == trust.AutoSystem ||
		(wire.ValidAppID(t) && !wire.CoreApp(t))
}

// ParseUpdateReport decodes and checks an update report.
func ParseUpdateReport(b []byte) (UpdateReport, error) {
	var r UpdateReport
	if err := enc.DecodeStrict(b, 4096, &r); err != nil {
		return UpdateReport{}, ErrUpdate
	}
	switch r.State {
	case UpdateAvailable, UpdateDownloaded, UpdateInstalling, UpdateInstalled, UpdateRolledBack, UpdateFailed, UpdateRejected:
	default:
		return UpdateReport{}, ErrUpdate
	}
	if !ValidTarget(r.Target) || (r.Version != "" && !ValidVersion(r.Version)) ||
		(r.Message != "" && !label(r.Message, wire.MaxMessageText)) {
		return UpdateReport{}, ErrUpdate
	}
	return r, nil
}

// Approved reports whether a standing approval in l lets updates for target,
// signed with key, install without a fresh signature of a user.
func Approved(l *trust.List, target string, key []byte) bool {
	for _, a := range l.Auto {
		if a.Target == target && (target == trust.AutoSystem || (len(key) > 0 && bytes.Equal(a.Key, key))) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Vektoren erzeugen und Tests laufen lassen**

Run:

```bash
go test ./contracts/store/ -run TestVectors -update
go test ./contracts/store/
go test -run=XXX -fuzz=FuzzCatalog -fuzztime=30s ./contracts/store/
```

Expected: `ok`; `contracts/testdata/store.json` angelegt; Fuzzing ohne Fund.

- [ ] **Step 5: Commit**

```bash
git add contracts/store contracts/testdata/store.json
git commit -m "Add catalogs, licenses, releases and update messages" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---


### Task 15: App-Paket entpacken und Kauf-Apps verschlüsseln (`contracts/store`, Teil 3)

- **Entpacken:** `Walk` liest ein Paket (tar) als Strom und prüft jeden Eintrag, bevor irgendwer ihn
  zu sehen bekommt: `manifest.json` zuerst, nur normale Dateien und Ordner, keine Links, keine
  absoluten Pfade, kein `..`, nur `ui/…` und `services/<arch>.tar`, keine doppelten Namen, feste
  Größengrenzen. Die API nimmt daraus die Oberfläche, der Controller das Image.
- **Verschlüsseln:** Der Store verschlüsselt ein Kauf-Paket mit dem Inhaltsschlüssel der App
  (32 Byte, einer pro App). Format: `NGCENC1\n`, 16 Byte Datei-ID, dann Blöcke zu 64 KiB Klartext,
  jeder mit AES-256-GCM. Schlüssel pro Datei: HKDF-SHA256(Inhaltsschlüssel, Salz Datei-ID,
  Info `ngc/package/v1`). Nonce: 7 Nullbytes, Blocknummer (4 Byte, big endian), 1 Byte „letzter
  Block“. So fallen Kürzen, Anhängen und Vertauschen von Blöcken auf.
- **Schlüssel für die Installation:** `WrapKey` verschlüsselt den Inhaltsschlüssel für den
  X25519-Schlüssel einer Installation: frischer Einmal-Schlüssel, X25519, HKDF-SHA256(Salz
  Einmal-Schlüssel ‖ Installationsschlüssel, Info `ngc/license-key/v1` 0x00 App-ID), AES-256-GCM mit
  der App-ID als zusätzlichen Daten. Nur die API der Installation kann ihn mit `UnwrapKey` öffnen.

**Files:**
- Create: `contracts/store/pack.go`, `contracts/store/pack_test.go`, `contracts/testdata/pack.json` (per `-update`)

**Interfaces:**
- Consumes: `Manifest`, `ParseManifest`, `ManifestFile`, `MaxManifest`, `ValidPath`, `MaxPackage`, `WrappedKey` (Tasks 13, 14),
  `crypto/hkdf`, `crypto/ecdh`, `golden.Check`.
- Produces:
  - Konstanten `store.MaxEntries = 10000`, `MaxUIFile = 16 << 20`, `MaxUITotal = 64 << 20`, `ChunkSize = 64 << 10`,
    `FileIDSize = 16`, `KeySize = 32`, `KindUI = "ui"`, `KindImage = "image"`
  - `type store.PackFile struct{ Path string; Size int64; Kind, Arch string }`
  - `store.Walk(r io.Reader, fn func(m *Manifest, f PackFile, body io.Reader) error) (*Manifest, error)`
  - `store.Encrypt(dst io.Writer, src io.Reader, key []byte, rng io.Reader) error`
  - `store.NewDecrypter(src io.Reader, key []byte) (io.Reader, error)`
  - `store.WrapKey(contentKey, installPub []byte, app string, rng io.Reader) (*WrappedKey, error)`
  - `store.UnwrapKey(w *WrappedKey, installPriv *ecdh.PrivateKey, app string) ([]byte, error)`
  - Fehler `store.ErrPackage`, `ErrEncrypted`

- [ ] **Step 1: Test schreiben**

`contracts/store/pack_test.go`:

```go
package store

import (
	"archive/tar"
	"bytes"
	"crypto/ecdh"
	"encoding/json"
	"io"
	"slices"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
)

type entry struct {
	name string
	body []byte
	kind byte
}

func buildTar(t testing.TB, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: e.kind}
		if e.kind == tar.TypeSymlink {
			h.Linkname, h.Size = "/etc/shadow", 0
		}
		if e.kind == tar.TypeDir {
			h.Mode, h.Size = 0o755, 0
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.kind == tar.TypeReg {
			if _, err := w.Write(e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func manifestEntry(t testing.TB) entry {
	b, err := json.Marshal(terminalManifest())
	if err != nil {
		t.Fatal(err)
	}
	return entry{ManifestFile, b, tar.TypeReg}
}

func goodEntries(t testing.TB) []entry {
	return []entry{
		manifestEntry(t),
		{"ui", nil, tar.TypeDir},
		{"ui/index.html", []byte("<!doctype html><title>Terminal</title>"), tar.TypeReg},
		{"ui/icon.svg", []byte("<svg/>"), tar.TypeReg},
		{"services/amd64.tar", []byte("image"), tar.TypeReg},
	}
}

func TestWalk(t *testing.T) {
	var got []PackFile
	m, err := Walk(bytes.NewReader(buildTar(t, goodEntries(t))), func(m *Manifest, f PackFile, body io.Reader) error {
		b, err := io.ReadAll(body)
		if err != nil || int64(len(b)) != f.Size {
			t.Errorf("%s: %v", f.Path, err)
		}
		got = append(got, f)
		return nil
	})
	if err != nil || m.ID != "org.sorglos.terminal" || len(got) != 3 {
		t.Fatalf("got %v, %v", got, err)
	}
	if got[2].Kind != KindImage || got[2].Arch != "amd64" || got[0].Kind != KindUI {
		t.Errorf("kinds: %+v", got)
	}
}

func TestWalkRejects(t *testing.T) {
	good := goodEntries(t)
	cases := map[string][]entry{
		"manifest not first": {good[2], good[0], good[3], good[4]},
		"symlink":            append(good[:1:1], entry{"ui/index.html", nil, tar.TypeSymlink}),
		"parent dir":         append(slices.Clone(good), entry{"ui/../../etc/cron.d/x", []byte("x"), tar.TypeReg}),
		"absolute":           append(slices.Clone(good), entry{"/etc/passwd", []byte("x"), tar.TypeReg}),
		"unknown top level":  append(slices.Clone(good), entry{"bin/sh", []byte("x"), tar.TypeReg}),
		"duplicate":          append(slices.Clone(good), good[2]),
		"unknown arch":       append(slices.Clone(good), entry{"services/riscv64.tar", []byte("x"), tar.TypeReg}),
		"ui entry missing":   {good[0], good[3], good[4]},
		"image missing":      {good[0], good[2], good[3]},
		"ui file too large":  append(slices.Clone(good), entry{"ui/big.js", make([]byte, MaxUIFile+1), tar.TypeReg}),
	}
	for name, entries := range cases {
		_, err := Walk(bytes.NewReader(buildTar(t, entries)), func(*Manifest, PackFile, io.Reader) error { return nil })
		if err != ErrPackage {
			t.Errorf("%s: %v", name, err)
		}
	}
}

var contentKey = bytes.Repeat([]byte{0x42}, KeySize)

func fixedRand(b byte) io.Reader { return bytes.NewReader(bytes.Repeat([]byte{b}, 1024)) }

func encrypt(t testing.TB, plain []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := Encrypt(&out, bytes.NewReader(plain), contentKey, fixedRand(0x24)); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func decrypt(sealed, key []byte) ([]byte, error) {
	r, err := NewDecrypter(bytes.NewReader(sealed), key)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func TestEncrypt(t *testing.T) {
	for _, n := range []int{0, 1, ChunkSize - 1, ChunkSize, ChunkSize + 1, 3*ChunkSize + 5} {
		plain := bytes.Repeat([]byte{byte(n)}, n)
		got, err := decrypt(encrypt(t, plain), contentKey)
		if err != nil || !bytes.Equal(got, plain) {
			t.Errorf("size %d: %v", n, err)
		}
	}
	sealed := encrypt(t, bytes.Repeat([]byte{7}, 2*ChunkSize))
	block := ChunkSize + 16
	header := len("NGCENC1\n") + FileIDSize
	flipped := bytes.Clone(sealed)
	flipped[header+5] ^= 1
	attacks := map[string][]byte{
		"flipped bit":  flipped,
		"truncated":    sealed[:header+block],
		"appended":     append(bytes.Clone(sealed), sealed[header:header+block]...),
		"swapped":      append(append(bytes.Clone(sealed[:header]), sealed[header+block:header+2*block]...), sealed[header:header+block]...),
		"bad magic":    append([]byte("NGCENC2\n"), sealed[8:]...),
		"only header":  sealed[:header],
	}
	for name, s := range attacks {
		if _, err := decrypt(s, contentKey); err != ErrEncrypted {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := decrypt(sealed, bytes.Repeat([]byte{1}, KeySize)); err != ErrEncrypted {
		t.Errorf("wrong key: %v", err)
	}
}

func TestWrapKey(t *testing.T) {
	priv, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x91}, 32))
	if err != nil {
		t.Fatal(err)
	}
	w, err := WrapKey(contentKey, priv.PublicKey().Bytes(), "org.sorglos.terminal", fixedRand(0x33))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := UnwrapKey(w, priv, "org.sorglos.terminal"); err != nil || !bytes.Equal(got, contentKey) {
		t.Fatalf("unwrap: %v", err)
	}
	other, _ := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x92}, 32))
	if _, err := UnwrapKey(w, other, "org.sorglos.terminal"); err != ErrEncrypted {
		t.Errorf("other installation: %v", err)
	}
	if _, err := UnwrapKey(w, priv, "org.sorglos.mail"); err != ErrEncrypted {
		t.Errorf("other app: %v", err)
	}
}

type packVector struct {
	ContentKey  enc.Bytes   `json:"contentKey"`
	Plain       enc.Bytes   `json:"plain"`
	Sealed      enc.Bytes   `json:"sealed"`
	InstallSeed enc.Bytes   `json:"installSeed"` // X25519 private key of the test installation
	App         string      `json:"app"`
	Wrapped     *WrappedKey `json:"wrapped"`
}

func TestPackVectors(t *testing.T) {
	priv, _ := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x91}, 32))
	w, err := WrapKey(contentKey, priv.PublicKey().Bytes(), "org.sorglos.terminal", fixedRand(0x33))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("NGC Kauf-App, Testinhalt")
	golden.Check(t, *update, "pack.json", packVector{ContentKey: contentKey, Plain: plain, Sealed: encrypt(t, plain),
		InstallSeed: bytes.Repeat([]byte{0x91}, 32), App: "org.sorglos.terminal", Wrapped: w})
}

func FuzzWalk(f *testing.F) {
	f.Add(buildTar(f, goodEntries(f)))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = Walk(bytes.NewReader(b), func(_ *Manifest, _ PackFile, body io.Reader) error {
			_, err := io.Copy(io.Discard, body)
			return err
		})
	})
}

func FuzzDecrypt(f *testing.F) {
	f.Add(encrypt(f, []byte("hello")))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = decrypt(b, contentKey)
	})
}
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

Run: `go test ./contracts/store/`
Expected: FAIL, `undefined: Walk`


- [ ] **Step 3: Umsetzung schreiben**

`contracts/store/pack.go`:

```go
package store

import (
	"archive/tar"
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"strings"
)

// Limits and names of app packages and their encryption.
const (
	MaxEntries = 10000
	MaxUIFile  = 16 << 20
	MaxUITotal = 64 << 20
	ChunkSize  = 64 << 10
	FileIDSize = 16
	KeySize    = 32
	KindUI     = "ui"
	KindImage  = "image"
	encMagic   = "NGCENC1\n"
)

// PackFile is a checked file of an app package.
type PackFile struct {
	Path string // e.g. "ui/index.html" or "services/arm64.tar"
	Size int64
	Kind string // KindUI or KindImage
	Arch string // images only
}

var (
	ErrPackage   = errors.New("store: invalid app package")
	ErrEncrypted = errors.New("store: encrypted data damaged or wrong key")
)

// Walk reads an app package and calls fn for every file once its header has
// been checked. manifest.json must come first. Walk returns the manifest
// after checking that the files it names are present.
func Walk(r io.Reader, fn func(m *Manifest, f PackFile, body io.Reader) error) (*Manifest, error) {
	tr := tar.NewReader(r)
	h, err := tr.Next()
	if err != nil || h.Typeflag != tar.TypeReg || h.Name != ManifestFile || h.Size > MaxManifest {
		return nil, ErrPackage
	}
	b, err := io.ReadAll(tr)
	if err != nil {
		return nil, ErrPackage
	}
	m, err := ParseManifest(b)
	if err != nil {
		return nil, ErrPackage
	}
	seen := map[string]bool{ManifestFile: true}
	var uiTotal int64
	images := 0
	for n := 1; ; n++ {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil || n > MaxEntries {
			return nil, ErrPackage
		}
		name := strings.TrimSuffix(h.Name, "/")
		if seen[name] {
			return nil, ErrPackage
		}
		seen[name] = true
		switch h.Typeflag {
		case tar.TypeDir:
			if name != "ui" && name != "services" && !(strings.HasPrefix(name, "ui/") && ValidPath(name[3:])) {
				return nil, ErrPackage
			}
			continue
		case tar.TypeReg:
		default:
			return nil, ErrPackage
		}
		f := PackFile{Path: name, Size: h.Size}
		switch {
		case strings.HasPrefix(name, "ui/") && ValidPath(name[3:]):
			uiTotal += h.Size
			if h.Size > MaxUIFile || uiTotal > MaxUITotal {
				return nil, ErrPackage
			}
			f.Kind = KindUI
		case name == "services/amd64.tar" || name == "services/arm64.tar":
			if h.Size > MaxPackage {
				return nil, ErrPackage
			}
			f.Kind, f.Arch = KindImage, strings.TrimSuffix(strings.TrimPrefix(name, "services/"), ".tar")
			images++
		default:
			return nil, ErrPackage
		}
		if err := fn(m, f, tr); err != nil {
			return nil, err
		}
	}
	if (m.UI != nil && !seen["ui/"+m.UI.Entry]) || (m.Icon != "" && !seen["ui/"+m.Icon]) ||
		(len(m.Services) > 0) != (images > 0) {
		return nil, ErrPackage
	}
	return m, nil
}

func packageAEAD(key, fileID []byte) (cipher.AEAD, error) {
	k, err := hkdf.Key(sha256.New, key, fileID, "ngc/package/v1", KeySize)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func chunkNonce(i uint32, last bool) []byte {
	n := make([]byte, 12)
	binary.BigEndian.PutUint32(n[7:11], i)
	if last {
		n[11] = 1
	}
	return n
}

// Encrypt writes src encrypted with the content key of an app.
func Encrypt(dst io.Writer, src io.Reader, key []byte, rng io.Reader) error {
	if len(key) != KeySize {
		return ErrEncrypted
	}
	id := make([]byte, FileIDSize)
	if _, err := io.ReadFull(rng, id); err != nil {
		return err
	}
	aead, err := packageAEAD(key, id)
	if err != nil {
		return err
	}
	if _, err := dst.Write(append([]byte(encMagic), id...)); err != nil {
		return err
	}
	cur, err := readChunk(src)
	if err != nil {
		return err
	}
	for i := uint32(0); ; i++ {
		next, err := readChunk(src)
		if err != nil {
			return err
		}
		last := len(next) == 0
		if _, err := dst.Write(aead.Seal(nil, chunkNonce(i, last), cur, nil)); err != nil {
			return err
		}
		if last {
			return nil
		}
		cur = next
	}
}

func readChunk(r io.Reader) ([]byte, error) {
	b := make([]byte, ChunkSize)
	n, err := io.ReadFull(r, b)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return b[:n], nil
}

type decrypter struct {
	src  *bufio.Reader
	aead cipher.AEAD
	i    uint32
	buf  []byte
	done bool
	err  error
}

// NewDecrypter returns a reader for the plaintext of an encrypted package.
// Any damage, a missing or an extra block ends reading with ErrEncrypted.
func NewDecrypter(src io.Reader, key []byte) (io.Reader, error) {
	head := make([]byte, len(encMagic)+FileIDSize)
	if len(key) != KeySize {
		return nil, ErrEncrypted
	}
	if _, err := io.ReadFull(src, head); err != nil || string(head[:len(encMagic)]) != encMagic {
		return nil, ErrEncrypted
	}
	aead, err := packageAEAD(key, head[len(encMagic):])
	if err != nil {
		return nil, ErrEncrypted
	}
	return &decrypter{src: bufio.NewReaderSize(src, ChunkSize+aead.Overhead()+1), aead: aead}, nil
}

func (d *decrypter) Read(p []byte) (int, error) {
	for len(d.buf) == 0 {
		if d.err != nil {
			return 0, d.err
		}
		if d.done {
			return 0, io.EOF
		}
		d.next()
	}
	n := copy(p, d.buf)
	d.buf = d.buf[n:]
	return n, nil
}

func (d *decrypter) next() {
	sealed := make([]byte, ChunkSize+d.aead.Overhead())
	n, err := io.ReadFull(d.src, sealed)
	if err != nil && err != io.ErrUnexpectedEOF {
		d.err = ErrEncrypted
		return
	}
	_, err = d.src.Peek(1)
	if err != nil && err != io.EOF {
		d.err = ErrEncrypted
		return
	}
	last := err == io.EOF
	plain, err := d.aead.Open(nil, chunkNonce(d.i, last), sealed[:n], nil)
	if err != nil || d.i == ^uint32(0) {
		d.err = ErrEncrypted
		return
	}
	d.i++
	d.buf, d.done = plain, last
}

func wrapAEAD(shared, eph, installPub []byte, app string) (cipher.AEAD, error) {
	salt := append(append([]byte{}, eph...), installPub...)
	k, err := hkdf.Key(sha256.New, shared, salt, "ngc/license-key/v1\x00"+app, KeySize)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// WrapKey encrypts the content key of app for the X25519 key of an
// installation. The one-time key comes from rng, so vectors are reproducible.
func WrapKey(contentKey, installPub []byte, app string, rng io.Reader) (*WrappedKey, error) {
	curve := ecdh.X25519()
	peer, err := curve.NewPublicKey(installPub)
	if err != nil || len(contentKey) != KeySize {
		return nil, ErrEncrypted
	}
	seed := make([]byte, 32)
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rng, seed); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(rng, nonce); err != nil {
		return nil, err
	}
	eph, err := curve.NewPrivateKey(seed)
	if err != nil {
		return nil, err
	}
	shared, err := eph.ECDH(peer)
	if err != nil {
		return nil, ErrEncrypted
	}
	aead, err := wrapAEAD(shared, eph.PublicKey().Bytes(), installPub, app)
	if err != nil {
		return nil, err
	}
	return &WrappedKey{Ephemeral: eph.PublicKey().Bytes(), Nonce: nonce, Sealed: aead.Seal(nil, nonce, contentKey, []byte(app))}, nil
}

// UnwrapKey opens a wrapped content key with the installation's private key.
func UnwrapKey(w *WrappedKey, installPriv *ecdh.PrivateKey, app string) ([]byte, error) {
	peer, err := ecdh.X25519().NewPublicKey(w.Ephemeral)
	if err != nil || len(w.Nonce) != 12 {
		return nil, ErrEncrypted
	}
	shared, err := installPriv.ECDH(peer)
	if err != nil {
		return nil, ErrEncrypted
	}
	aead, err := wrapAEAD(shared, w.Ephemeral, installPriv.PublicKey().Bytes(), app)
	if err != nil {
		return nil, ErrEncrypted
	}
	key, err := aead.Open(nil, w.Nonce, w.Sealed, []byte(app))
	if err != nil || len(key) != KeySize {
		return nil, ErrEncrypted
	}
	return key, nil
}
```

- [ ] **Step 4: Vektor erzeugen und Tests laufen lassen**

Run:

```bash
go test ./contracts/store/ -run TestPackVectors -update
go test ./contracts/store/
go test -run=XXX -fuzz=FuzzWalk -fuzztime=30s ./contracts/store/
go test -run=XXX -fuzz=FuzzDecrypt -fuzztime=30s ./contracts/store/
```

Expected: `ok`; `contracts/testdata/pack.json` angelegt; Fuzzing ohne Fund.

- [ ] **Step 5: Commit**

```bash
git add contracts/store contracts/testdata/pack.json
git commit -m "Add safe unpacking of app packages and encryption of paid apps" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---


### Task 16: Kern-Aktionen (`contracts/core`)

Was die Shell von der API und die API vom Controller will, läuft über Kanäle zu drei Kern-Apps.
Dieser Vertrag legt Namen, Parameter, Ergebnisse und Datenströme fest, damit Webinterface, API
und Controller parallel bauen können. Er gehört zu V1 (Shell ↔ API) und V2 (API ↔ Controller).

| App | Für | Dienst `api` (die API führt aus) | Dienst `controller` (Root-Aufgaben) |
|---|---|---|---|
| `ngc.desktop` | alle Angemeldeten | `me`, `apps`, `ui`, `totp-begin`, `totp-confirm`, `totp-off` | – |
| `ngc.users` | Admins | `list`, `rights`, `set-rights`, `trust`, `pair-code`, `pending`, `log` | – |
| `ngc.store` | Admins | `sources`, `catalog`, `installed`, `upload`, `license`, `install-key`, `updates`, `set-update` | `activate`, `deactivate`, `uninstall`, `delete-data`, `stage`, `install` |

Datenfluss (`Flow`):

- `call`: Parameter im OPEN, die Shell schickt sofort EOF, die API antwortet mit genau einem
  JSON-Ergebnis als DATA, dann EOF und CLOSE `ok`. Aktionen ohne Ergebnis antworten `{}`.
- `in`: wie `call`, aber die Shell schickt vor dem EOF Daten: das Paket (`upload`), eine Fassung der
  Vertrauensliste als JSON (`trust`), einen Lizenz-Umschlag als JSON (`license`).
- `out`: die API schickt statt eines JSON-Ergebnisses Daten; bei `ui` ein tar mit den Dateien unter
  `ui/`, das die Shell wie `store.Walk` prüft.
- `transfer`: API → Controller: `Transfer`-Kopf (4 Byte Länge, big endian, dann JSON), danach genau
  `size` Byte Datei. Der Controller prüft Prüfsumme, Katalog oder Release, Lizenz und Aufgabe selbst.

Kanäle zum Dienst `controller` öffnet nur die API. Bei Root-Aufgaben erstellt die Shell die
Aufgabe (`task.Task` mit `node`, `app`, `service` und `action` wie in der Tabelle) und schickt sie
im OPEN an `ngc.store`; die API prüft, lädt die Datei und öffnet mit derselben Aufgabe den Kanal
zum Controller. Ohne Benutzer öffnet die API nur `stage` sowie `install`, wenn eine Dauerfreigabe
das Ziel deckt (`store.Approved`).

Rechte: `<app>/use` erlaubt eine App überhaupt, `<app>/<aktion>` eine Aktion. Die Kern-Apps
`ngc.users` und `ngc.store` sind nur für die Rolle `admin`, `ngc.desktop` für alle.

Nicht in diesem Vertrag, sondern als spätere Vertragsänderung durch den Koordinator: Wiederherstellung
(Codes, Zurücksetzen durch einen Admin, Notfallbefehl auf dem Server).

**Files:**
- Create: `contracts/core/core.go`, `contracts/core/results.go`, `contracts/core/core_test.go`,
  `contracts/testdata/core.json` (per `-update`)

**Interfaces:**
- Consumes: `enc.Bytes`, `enc.DecodeStrict`, `sign.Envelope`, `session.ValidTOTP`, `trust.Level`, `trust.ValidID`,
  `store.ValidVersion`, `store.ValidTarget`, `store.NeedRoot`, `store.NeedNetwork`, `store.MaxPackage`, `store.ModeAuto`,
  `store.ModeSemi`, `store.ModeNotify`, `trust.AutoSystem`, `wire.ValidAppID`, `wire.ValidName`, `wire.CoreApp`,
  `wire.MaxParams`, `golden.Check`.
- Produces:
  - Konstanten `core.AppDesktop = "ngc.desktop"`, `AppUsers = "ngc.users"`, `AppStore = "ngc.store"`,
    `ServiceAPI = "api"`, `ServiceController = "controller"`, `RightUse = "use"`, `LocalStore = "local"`,
    Aktionsnamen `ActMe` … `ActInstall` (Tabelle oben), `MaxTransferHeader = 8 << 20`
  - `type core.Flow string`: `FlowCall`, `FlowIn`, `FlowOut`, `FlowTransfer`
  - `type core.Action struct{ App, Service, Name string; Admin, Root, Changes bool; Flow Flow; Params func() Params }`
  - `core.Lookup(app, service, name string) (Action, bool)`, `core.All() []Action`
  - `type core.Params interface{ Validate() error }`, `core.ParseParams(a Action, raw json.RawMessage) (Params, error)`
  - `core.Critical(a Action, p Params) bool`, `core.ValidRight(string) bool`
  - Parameter: `AppParams{App}`, `CodeParams{Code}`, `StoreParams{Store}`, `RoleRights{Role; Rights []string}`,
    `LogQuery{Before int64; Limit int; User string}`, `UpdateSetting{Target, Mode, Window string}`,
    `ActivateParams{App, Version string; SHA256 enc.Bytes; Store string; Grants []string}`,
    `UninstallParams{App string; KeepData bool}`, `StageParams{Target, Version string}`,
    `InstallParams{Target, Version string; SHA256 enc.Bytes}`
  - Ergebnisse (`results.go`): `Me`, `DesktopApp` (Zustände `AppOK`, `AppUnreachable`, `AppUnlicensed`), `TOTPBegin`,
    `UserInfo`, `PairCodeResult`, `Pending`, `LogEntry` (Arten `LogLogin` … `LogUpdate`), `SourceInfo`, `CatalogResult`,
    `InstalledApp` (Lizenzzustände `LicenseNone`, `LicenseValid`, `LicenseGrace`, `LicenseExpired`), `UploadResult`,
    `LicenseResult`, `InstallKey`, `UpdateInfo`, `TrustResult`, `Notice` (Stufen `NoticeInfo`, `NoticeWarn`, `NoticeError`)
  - `type core.Transfer struct{ Catalog *sign.Envelope; Store string; Release *sign.Envelope; License *sign.Envelope; Key enc.Bytes; Size int64; SHA256 enc.Bytes }`
    (JSON `catalog`, `store`, `release`, `license`, `key`, `size`, `sha256`), `(Transfer) Validate() error`,
    `core.WriteTransfer(w io.Writer, t Transfer) error`, `core.ReadTransfer(r io.Reader) (Transfer, error)`
  - Fehler `core.ErrParams`, `ErrTransfer`


- [ ] **Step 1: Test schreiben**

`contracts/core/core_test.go`:

```go
package core

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"io"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/store"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

func TestTable(t *testing.T) {
	seen := map[string]bool{}
	for _, a := range All() {
		key := a.App + "/" + a.Service + "/" + a.Name
		if seen[key] || !wire.CoreApp(a.App) || !wire.ValidName(a.Service) || !wire.ValidName(a.Name) {
			t.Errorf("%s: duplicate or invalid", key)
		}
		seen[key] = true
		if a.Root && a.Service != ServiceController {
			t.Errorf("%s: root tasks run on controllers", key)
		}
		if a.App != AppDesktop && !a.Admin {
			t.Errorf("%s: only admins", key)
		}
		if got, ok := Lookup(a.App, a.Service, a.Name); !ok || got.Name != a.Name {
			t.Errorf("%s: lookup", key)
		}
	}
	if _, ok := Lookup(AppStore, ServiceAPI, ActActivate); ok {
		t.Error("activate belongs to the controller service")
	}
}

func action(t *testing.T, app, service, name string) Action {
	t.Helper()
	a, ok := Lookup(app, service, name)
	if !ok {
		t.Fatalf("%s/%s/%s missing", app, service, name)
	}
	return a
}

func TestParseParams(t *testing.T) {
	sum := enc.B64.EncodeToString(bytes.Repeat([]byte{1}, 32))
	good := []struct{ app, service, name, params string }{
		{AppDesktop, ServiceAPI, ActMe, ``},
		{AppDesktop, ServiceAPI, ActUI, `{"app":"org.sorglos.terminal"}`},
		{AppDesktop, ServiceAPI, ActTOTPConfirm, `{"code":"123456"}`},
		{AppUsers, ServiceAPI, ActSetRights, `{"role":"user","rights":["org.sorglos.calculator/use","org.sorglos.terminal/shell"]}`},
		{AppUsers, ServiceAPI, ActLog, `{"limit":100}`},
		{AppStore, ServiceAPI, ActSetUpdate, `{"target":"ngc-api","mode":"auto","window":"02:00-04:00"}`},
		{AppStore, ServiceController, ActActivate, `{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","store":"global","grants":["network"]}`},
		{AppStore, ServiceController, ActUninstall, `{"app":"org.sorglos.terminal","keepData":true}`},
		{AppStore, ServiceController, ActInstall, `{"target":"system"}`},
		{AppStore, ServiceController, ActInstall, `{"target":"ngc-controller","version":"1.1.0","sha256":"` + sum + `"}`},
	}
	for _, c := range good {
		if _, err := ParseParams(action(t, c.app, c.service, c.name), json.RawMessage(c.params)); err != nil {
			t.Errorf("%s %s: %v", c.name, c.params, err)
		}
	}
	bad := []struct{ app, service, name, params string }{
		{AppDesktop, ServiceAPI, ActMe, `{"x":1}`},
		{AppDesktop, ServiceAPI, ActUI, ``},
		{AppDesktop, ServiceAPI, ActUI, `{"app":"ngc.users"}`},
		{AppDesktop, ServiceAPI, ActTOTPConfirm, `{"code":"12345"}`},
		{AppUsers, ServiceAPI, ActSetRights, `{"role":"user","rights":["everything"]}`},
		{AppUsers, ServiceAPI, ActLog, `{"limit":0}`},
		{AppStore, ServiceAPI, ActSetUpdate, `{"target":"ngc-api","mode":"always"}`},
		{AppStore, ServiceController, ActActivate, `{"app":"org.sorglos.terminal","version":"1.0.0","sha256":"` + sum + `","store":"global","grants":["host"]}`},
		{AppStore, ServiceController, ActInstall, `{"target":"system","version":"1.0.0"}`},
		{AppStore, ServiceController, ActInstall, `{"target":"ngc-controller","version":"1.1.0"}`},
	}
	for _, c := range bad {
		if _, err := ParseParams(action(t, c.app, c.service, c.name), json.RawMessage(c.params)); err == nil {
			t.Errorf("%s %s: accepted", c.name, c.params)
		}
	}
}

func TestCritical(t *testing.T) {
	activate := action(t, AppStore, ServiceController, ActActivate)
	uninstall := action(t, AppStore, ServiceController, ActUninstall)
	cases := []struct {
		name string
		a    Action
		p    Params
		want bool
	}{
		{"activate without grants", activate, &ActivateParams{}, false},
		{"activate with root", activate, &ActivateParams{Grants: []string{store.NeedRoot}}, true},
		{"uninstall, keep data", uninstall, &UninstallParams{KeepData: true}, false},
		{"uninstall, delete data", uninstall, &UninstallParams{}, true},
		{"upload", action(t, AppStore, ServiceAPI, ActUpload), nil, true},
		{"delete kept data", action(t, AppStore, ServiceController, ActDeleteData), &AppParams{}, true},
		{"list users", action(t, AppUsers, ServiceAPI, ActList), nil, false},
	}
	for _, c := range cases {
		if got := Critical(c.a, c.p); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func testTransfer() Transfer {
	cat := sign.Envelope{Payload: []byte(`{}`), KeyID: "k", Sig: bytes.Repeat([]byte{1}, 64)}
	lic := sign.Envelope{Payload: []byte(`{}`), KeyID: "k", Sig: bytes.Repeat([]byte{2}, 64)}
	return Transfer{Catalog: &cat, Store: "global", License: &lic, Key: bytes.Repeat([]byte{3}, 32), Size: 5000000, SHA256: bytes.Repeat([]byte{4}, 32)}
}

func TestTransfer(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteTransfer(&buf, testTransfer()); err != nil {
		t.Fatal(err)
	}
	buf.WriteString("file")
	got, err := ReadTransfer(&buf)
	if err != nil || got.Store != "global" || got.Size != 5000000 {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	if rest, _ := io.ReadAll(&buf); string(rest) != "file" {
		t.Errorf("reader consumed the file: %q", rest)
	}
	for name, mutate := range map[string]func(*Transfer){
		"key without license":     func(tr *Transfer) { tr.License = nil },
		"catalog and release":     func(tr *Transfer) { tr.Release = tr.Catalog },
		"catalog without store":   func(tr *Transfer) { tr.Store = "" },
		"short checksum":          func(tr *Transfer) { tr.SHA256 = []byte{1} },
		"too large":               func(tr *Transfer) { tr.Size = store.MaxPackage + 1 },
	} {
		tr := testTransfer()
		mutate(&tr)
		if tr.Validate() != ErrTransfer {
			t.Errorf("%s accepted", name)
		}
	}
	huge := make([]byte, 4)
	binary.BigEndian.PutUint32(huge, MaxTransferHeader+1)
	if _, err := ReadTransfer(bytes.NewReader(huge)); err != ErrTransfer {
		t.Errorf("huge header: %v", err)
	}
}

func TestRights(t *testing.T) {
	for _, r := range []string{"org.sorglos.terminal/use", "org.sorglos.terminal/shell"} {
		if !ValidRight(r) {
			t.Errorf("%s rejected", r)
		}
	}
	for _, r := range []string{"org.sorglos.terminal", "ngc.users/use", "org.sorglos.terminal/Shell", "/use"} {
		if ValidRight(r) {
			t.Errorf("%s accepted", r)
		}
	}
}

// TestExamples writes one example result per call action, so the web
// interface and the API test against the same JSON.
func TestExamples(t *testing.T) {
	examples := map[string]any{
		"ngc.desktop/me":        Me{ID: "ISEhISEhISEhISEhISEhIQ", Name: "admin", Roles: []string{trust.RoleAdmin}, Admin: true, PasswordLogin: true, Level: trust.LevelCritical},
		"ngc.desktop/apps":      []DesktopApp{{ID: "org.sorglos.terminal", Name: "Terminal", Version: "1.0.0", Icon: "icon.svg", Width: 720, Height: 480, Actions: []string{"shell"}, State: AppOK}},
		"ngc.desktop/totp-begin": TOTPBegin{Secret: "JBSWY3DPEHPK3PXP", URI: "otpauth://totp/NGC:admin?secret=JBSWY3DPEHPK3PXP&issuer=NGC"},
		"ngc.users/list":        []UserInfo{{ID: "ISEhISEhISEhISEhISEhIQ", Name: "admin", Roles: []string{trust.RoleAdmin}, TOTP: true, LastLogin: 1790000000}},
		"ngc.users/rights":      []RoleRights{{Role: "user", Rights: []string{"org.sorglos.calculator/use"}}},
		"ngc.users/trust":       TrustResult{Seq: 4},
		"ngc.users/pair-code":   PairCodeResult{Code: "ngc-pair:…", Expires: 1790086400},
		"ngc.users/pending":     []Pending{{Name: "dienste", Key: bytes.Repeat([]byte{0xC1}, 32), Fingerprint: "k3y…", Since: 1790000000}},
		"ngc.users/log":         []LogEntry{{Time: 1790000000, User: "ISEhISEhISEhISEhISEhIQ", Kind: LogTask, App: "ngc.store", Action: "activate", Node: "ctl-1", Result: "ok"}},
		"ngc.store/sources":     []SourceInfo{{ID: "global", Name: "NGC Store", URL: "https://store.ngc.test", Seq: 5, Checked: 1790000000}},
		"ngc.store/installed":   []InstalledApp{{ID: "org.sorglos.terminal", Version: "1.0.0", Store: "global", Node: "ctl-1", Active: true, State: AppOK, License: LicenseValid}},
		"ngc.store/upload":      UploadResult{ID: "org.example.notes", Version: "0.1.0", SHA256: bytes.Repeat([]byte{5}, 32)},
		"ngc.store/license":     LicenseResult{App: "org.sorglos.terminal", State: LicenseValid},
		"ngc.store/install-key": InstallKey{Installation: "EREREREREREREREREREREQ", Key: bytes.Repeat([]byte{6}, 32)},
		"ngc.store/updates":     []UpdateInfo{{Target: "ngc-api", Installed: "1.0.0", Available: "1.1.0", Mode: store.ModeAuto, Window: "02:00-04:00", State: store.UpdateAvailable}},
		"notice":                Notice{Level: NoticeWarn, Text: "Die Lizenz für Terminal läuft in 3 Tagen ab.", App: "org.sorglos.terminal"},
	}
	golden.Check(t, *update, "core.json", examples)
}

func FuzzParseParams(f *testing.F) {
	f.Add([]byte(`{"app":"org.sorglos.terminal","version":"1.0.0"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		for _, a := range All() {
			_, _ = ParseParams(a, b)
		}
	})
}

func FuzzReadTransfer(f *testing.F) {
	var buf bytes.Buffer
	_ = WriteTransfer(&buf, testTransfer())
	f.Add(buf.Bytes())
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ReadTransfer(bytes.NewReader(b))
	})
}
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

Run: `go test ./contracts/core/`
Expected: FAIL, `undefined: All`


- [ ] **Step 3: Aktionen und Parameter schreiben**

`contracts/core/core.go`:

```go
// Package core defines the actions of the built-in apps: what the shell
// asks the API for (contract V1) and the root tasks the API forwards to a
// controller (contract V2).
package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/session"
	"github.com/sorglos-it/nextgencloud/contracts/store"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// Core apps, services, rights.
const (
	AppDesktop = "ngc.desktop"
	AppUsers   = "ngc.users"
	AppStore   = "ngc.store"

	ServiceAPI        = "api"
	ServiceController = "controller"

	RightUse   = "use"
	LocalStore = "local" // store ID of apps an admin uploaded
)

// Action names.
const (
	ActMe          = "me"
	ActApps        = "apps"
	ActUI          = "ui"
	ActTOTPBegin   = "totp-begin"
	ActTOTPConfirm = "totp-confirm"
	ActTOTPOff     = "totp-off"

	ActList      = "list"
	ActRights    = "rights"
	ActSetRights = "set-rights"
	ActTrust     = "trust"
	ActPairCode  = "pair-code"
	ActPending   = "pending"
	ActLog       = "log"

	ActSources    = "sources"
	ActCatalog    = "catalog"
	ActInstalled  = "installed"
	ActUpload     = "upload"
	ActLicense    = "license"
	ActInstallKey = "install-key"
	ActUpdates    = "updates"
	ActSetUpdate  = "set-update"

	ActActivate   = "activate"
	ActDeactivate = "deactivate"
	ActUninstall  = "uninstall"
	ActDeleteData = "delete-data"
	ActStage      = "stage"
	ActInstall    = "install"
)

// Flow is how data moves on the channel of an action.
type Flow string

const (
	FlowCall     Flow = "call"     // params in the OPEN, one JSON result
	FlowIn       Flow = "in"       // the shell sends data, then one JSON result
	FlowOut      Flow = "out"      // the API sends data instead of a JSON result
	FlowTransfer Flow = "transfer" // API to controller: Transfer header, then the file
)

// Params are the parameters of an action.
type Params interface{ Validate() error }

// Action describes a core action.
type Action struct {
	App, Service, Name string
	Admin              bool // only for the role admin
	Root               bool // root task on a controller: signed task required
	Changes            bool
	Flow               Flow
	Params             func() Params // nil: the action takes no parameters
}

// newParams returns a constructor for the parameter type T.
func newParams[T any, PT interface {
	*T
	Params
}]() func() Params {
	return func() Params { return PT(new(T)) }
}

var actions = []Action{
	// App, Service, Name, Admin, Root, Changes, Flow, Params
	{AppDesktop, ServiceAPI, ActMe, false, false, false, FlowCall, nil},
	{AppDesktop, ServiceAPI, ActApps, false, false, false, FlowCall, nil},
	{AppDesktop, ServiceAPI, ActUI, false, false, false, FlowOut, newParams[AppParams]()},
	{AppDesktop, ServiceAPI, ActTOTPBegin, false, false, true, FlowCall, nil},
	{AppDesktop, ServiceAPI, ActTOTPConfirm, false, false, true, FlowCall, newParams[CodeParams]()},
	{AppDesktop, ServiceAPI, ActTOTPOff, false, false, true, FlowCall, newParams[CodeParams]()},

	{AppUsers, ServiceAPI, ActList, true, false, false, FlowCall, nil},
	{AppUsers, ServiceAPI, ActRights, true, false, false, FlowCall, nil},
	{AppUsers, ServiceAPI, ActSetRights, true, false, true, FlowCall, newParams[RoleRights]()},
	{AppUsers, ServiceAPI, ActTrust, true, false, true, FlowIn, nil},
	{AppUsers, ServiceAPI, ActPairCode, true, false, true, FlowCall, nil},
	{AppUsers, ServiceAPI, ActPending, true, false, false, FlowCall, nil},
	{AppUsers, ServiceAPI, ActLog, true, false, false, FlowCall, newParams[LogQuery]()},

	{AppStore, ServiceAPI, ActSources, true, false, false, FlowCall, nil},
	{AppStore, ServiceAPI, ActCatalog, true, false, false, FlowCall, newParams[StoreParams]()},
	{AppStore, ServiceAPI, ActInstalled, true, false, false, FlowCall, nil},
	{AppStore, ServiceAPI, ActUpload, true, false, true, FlowIn, nil},
	{AppStore, ServiceAPI, ActLicense, true, false, true, FlowIn, nil},
	{AppStore, ServiceAPI, ActInstallKey, true, false, false, FlowCall, nil},
	{AppStore, ServiceAPI, ActUpdates, true, false, false, FlowCall, nil},
	{AppStore, ServiceAPI, ActSetUpdate, true, false, true, FlowCall, newParams[UpdateSetting]()},

	{AppStore, ServiceController, ActActivate, true, true, true, FlowTransfer, newParams[ActivateParams]()},
	{AppStore, ServiceController, ActDeactivate, true, true, true, FlowCall, newParams[AppParams]()},
	{AppStore, ServiceController, ActUninstall, true, true, true, FlowCall, newParams[UninstallParams]()},
	{AppStore, ServiceController, ActDeleteData, true, true, true, FlowCall, newParams[AppParams]()},
	{AppStore, ServiceController, ActStage, true, false, false, FlowTransfer, newParams[StageParams]()},
	{AppStore, ServiceController, ActInstall, true, true, true, FlowCall, newParams[InstallParams]()},
}

// All returns every core action.
func All() []Action { return slices.Clone(actions) }

// Lookup finds a core action.
func Lookup(app, service, name string) (Action, bool) {
	for _, a := range actions {
		if a.App == app && a.Service == service && a.Name == name {
			return a, true
		}
	}
	return Action{}, false
}

var (
	ErrParams   = errors.New("core: invalid parameters")
	ErrTransfer = errors.New("core: invalid transfer")
)

// ParseParams decodes the parameters of an action strictly. Actions without
// parameters accept nothing but an empty value or {}.
func ParseParams(a Action, raw json.RawMessage) (Params, error) {
	if a.Params == nil {
		if t := bytes.TrimSpace(raw); len(t) != 0 && string(t) != "{}" {
			return nil, ErrParams
		}
		return nil, nil
	}
	v := a.Params()
	if err := enc.DecodeStrict(raw, wire.MaxParams, v); err != nil {
		return nil, ErrParams
	}
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return v, nil
}

// Critical reports whether an action needs re-confirmation at level
// "critical". Changes to the trust list are always signed with a user key
// and count as confirmed anyway.
func Critical(a Action, params Params) bool {
	switch v := params.(type) {
	case *ActivateParams:
		return len(v.Grants) > 0
	case *UninstallParams:
		return !v.KeepData
	}
	return a.App == AppStore && (a.Name == ActUpload || a.Name == ActDeleteData)
}

// ValidRight reports whether r is "<app>/use" or "<app>/<action>" for an app from a store.
func ValidRight(r string) bool {
	app, action, ok := strings.Cut(r, "/")
	return ok && wire.ValidAppID(app) && !wire.CoreApp(app) && (action == RightUse || wire.ValidName(action))
}

func validApp(app string) bool { return wire.ValidAppID(app) && !wire.CoreApp(app) }

// AppParams names an app.
type AppParams struct {
	App string `json:"app"`
}

func (p *AppParams) Validate() error { return check(validApp(p.App)) }

// CodeParams carries a TOTP code.
type CodeParams struct {
	Code string `json:"code"`
}

func (p *CodeParams) Validate() error { return check(session.ValidTOTP(p.Code)) }

// StoreParams names a store.
type StoreParams struct {
	Store string `json:"store"`
}

func (p *StoreParams) Validate() error { return check(wire.ValidName(p.Store)) }

// RoleRights are the rights of a role on apps (params of set-rights, result of rights).
type RoleRights struct {
	Role   string   `json:"role"`
	Rights []string `json:"rights"`
}

func (p *RoleRights) Validate() error {
	ok := wire.ValidName(p.Role) && len(p.Rights) <= 1000
	for i, r := range p.Rights {
		ok = ok && ValidRight(r) && !slices.Contains(p.Rights[:i], r)
	}
	return check(ok)
}

// LogQuery selects log entries, newest first.
type LogQuery struct {
	Before int64  `json:"before,omitempty"` // entries older than this time
	Limit  int    `json:"limit"`
	User   string `json:"user,omitempty"`
}

func (p *LogQuery) Validate() error {
	return check(p.Before >= 0 && p.Limit >= 1 && p.Limit <= 500 && (p.User == "" || trust.ValidID(p.User)))
}

var window = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]-([01][0-9]|2[0-3]):[0-5][0-9]$`)

// UpdateSetting sets the update mode of a part or an app. Switching to
// store.ModeAuto also needs a standing approval in the trust list.
type UpdateSetting struct {
	Target string `json:"target"`
	Mode   string `json:"mode"`
	Window string `json:"window,omitempty"` // maintenance window, local time "HH:MM-HH:MM"
}

func (p *UpdateSetting) Validate() error {
	mode := p.Mode == store.ModeAuto || p.Mode == store.ModeSemi || p.Mode == store.ModeNotify
	return check(store.ValidTarget(p.Target) && mode && (p.Window == "" || window.MatchString(p.Window)))
}

// ActivateParams is the root task that installs and starts an app on a controller.
type ActivateParams struct {
	App     string    `json:"app"`
	Version string    `json:"version"`
	SHA256  enc.Bytes `json:"sha256"` // of the package file as the store delivers it
	Store   string    `json:"store"`  // LocalStore for uploaded apps
	Grants  []string  `json:"grants,omitempty"` // store.NeedRoot, store.NeedNetwork the admin allows
}

func (p *ActivateParams) Validate() error {
	ok := validApp(p.App) && store.ValidVersion(p.Version) && len(p.SHA256) == 32 && wire.ValidName(p.Store)
	for i, g := range p.Grants {
		ok = ok && (g == store.NeedRoot || g == store.NeedNetwork) && !slices.Contains(p.Grants[:i], g)
	}
	return check(ok)
}

// UninstallParams removes an app; KeepData keeps its encrypted data for a reinstall.
type UninstallParams struct {
	App      string `json:"app"`
	KeepData bool   `json:"keepData,omitempty"`
}

func (p *UninstallParams) Validate() error { return check(validApp(p.App)) }

// StageParams announces an update file the API sends ahead of installing it.
type StageParams struct {
	Target  string `json:"target"`
	Version string `json:"version"`
}

func (p *StageParams) Validate() error {
	return check(store.ValidTarget(p.Target) && p.Target != trust.AutoSystem && store.ValidVersion(p.Version))
}

// InstallParams installs a staged update; for trust.AutoSystem it runs apt.
type InstallParams struct {
	Target  string    `json:"target"`
	Version string    `json:"version,omitempty"`
	SHA256  enc.Bytes `json:"sha256,omitempty"`
}

func (p *InstallParams) Validate() error {
	if p.Target == trust.AutoSystem {
		return check(p.Version == "" && len(p.SHA256) == 0)
	}
	return check(store.ValidTarget(p.Target) && store.ValidVersion(p.Version) && len(p.SHA256) == 32)
}

func check(ok bool) error {
	if !ok {
		return ErrParams
	}
	return nil
}
```


- [ ] **Step 4: Ergebnisse und Transfer schreiben**

`contracts/core/results.go`:

```go
package core

import (
	"encoding/binary"
	"encoding/json"
	"io"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/store"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// States of an app for the desktop and the app store.
const (
	AppOK          = "ok"
	AppUnreachable = "unreachable" // its controller has no tunnel to this API
	AppUnlicensed  = "unlicensed"
)

// License states in InstalledApp and LicenseResult.
const (
	LicenseNone    = "none" // free app
	LicenseValid   = "valid"
	LicenseGrace   = "grace"
	LicenseExpired = "expired"
)

// Kinds of log entries.
const (
	LogLogin       = "login"
	LogLoginFailed = "login-failed"
	LogChannel     = "channel"
	LogTask        = "task" // signed task, with its signature kept by the API
	LogTrust       = "trust"
	LogRights      = "rights"
	LogApp         = "app"
	LogStore       = "store"
	LogLicense     = "license"
	LogUpdate      = "update"
)

// Levels of a notice.
const (
	NoticeInfo  = "info"
	NoticeWarn  = "warn"
	NoticeError = "error"
)

// Me answers ngc.desktop/me.
type Me struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Roles         []string    `json:"roles"`
	Admin         bool        `json:"admin"`
	PasswordLogin bool        `json:"passwordLogin"` // show the hint to switch to passkeys
	TOTP          bool        `json:"totp"`
	Level         trust.Level `json:"level"` // strictest re-confirmation level of the user's roles
}

// DesktopApp is an entry of the start menu (ngc.desktop/apps).
type DesktopApp struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Icon    string   `json:"icon,omitempty"` // path below ui/
	Width   int      `json:"width,omitempty"`
	Height  int      `json:"height,omitempty"`
	Actions []string `json:"actions"` // actions this user may use
	State   string   `json:"state"`
	Core    bool     `json:"core,omitempty"` // app store and users & rights; their UI is part of the shell
}

// TOTPBegin answers ngc.desktop/totp-begin.
type TOTPBegin struct {
	Secret string `json:"secret"` // base32, for typing in
	URI    string `json:"uri"`    // otpauth://totp/…, for the QR code
}

// UserInfo is a row of ngc.users/list; keys and roles come from the trust list.
type UserInfo struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Roles     []string `json:"roles"`
	TOTP      bool     `json:"totp"`
	Locked    bool     `json:"locked"`
	LastLogin int64    `json:"lastLogin,omitempty"`
}

// PairCodeResult answers ngc.users/pair-code.
type PairCodeResult struct {
	Code    string `json:"code"` // tunnel.PairCode as string
	Expires int64  `json:"expires"`
}

// Pending is a controller that paired but is not in the trust list yet.
type Pending struct {
	Name        string    `json:"name"`
	Key         enc.Bytes `json:"key"`
	Fingerprint string    `json:"fingerprint"` // the admin compares it with the output of the ulh script
	Since       int64     `json:"since"`
}

// LogEntry is a row of ngc.users/log.
type LogEntry struct {
	Time   int64  `json:"time"`
	User   string `json:"user,omitempty"`
	Kind   string `json:"kind"`
	App    string `json:"app,omitempty"`
	Action string `json:"action,omitempty"`
	Node   string `json:"node,omitempty"`
	Result string `json:"result"` // "ok" or a wire.Code
	Detail string `json:"detail,omitempty"`
}

// SourceInfo is a store in ngc.store/sources.
type SourceInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Seq     uint64 `json:"seq"`               // newest catalog seen
	Checked int64  `json:"checked,omitempty"` // last successful fetch
	Error   string `json:"error,omitempty"`   // German
}

// CatalogResult answers ngc.store/catalog. The shell verifies it with the
// store key from the trust list before showing it.
type CatalogResult struct {
	Catalog sign.Envelope `json:"catalog"`
}

// InstalledApp is a row of ngc.store/installed.
type InstalledApp struct {
	ID       string `json:"id"`
	Version  string `json:"version,omitempty"` // empty if only kept data remains
	Store    string `json:"store"`
	Node     string `json:"node"`
	Active   bool   `json:"active"`
	State    string `json:"state"`
	License  string `json:"license"`
	KeptData int64  `json:"keptData,omitempty"` // bytes of data kept after uninstalling
}

// UploadResult answers ngc.store/upload.
type UploadResult struct {
	ID      string    `json:"id"`
	Version string    `json:"version"`
	SHA256  enc.Bytes `json:"sha256"` // goes into ActivateParams
}

// LicenseResult answers ngc.store/license.
type LicenseResult struct {
	App   string `json:"app"`
	State string `json:"state"`
}

// InstallKey answers ngc.store/install-key; the admin gives it to the shop when buying.
type InstallKey struct {
	Installation string    `json:"installation"`
	Key          enc.Bytes `json:"key"` // X25519
}

// UpdateInfo is a row of ngc.store/updates.
type UpdateInfo struct {
	Target    string `json:"target"`
	Installed string `json:"installed,omitempty"`
	Available string `json:"available,omitempty"`
	Mode      string `json:"mode"`
	Window    string `json:"window,omitempty"`
	State     string `json:"state,omitempty"` // store.Update… of the newest report
}

// TrustResult answers ngc.users/trust.
type TrustResult struct {
	Seq uint64 `json:"seq"`
}

// Notice is the body of a "notify" control message: the API tells the
// shell, a controller tells the API.
type Notice struct {
	Level string `json:"level"`
	Text  string `json:"text"` // German
	App   string `json:"app,omitempty"`
}

// MaxTransferHeader limits the JSON header of a transfer.
const MaxTransferHeader = 8 << 20

// Transfer is the header of a file the API sends to a controller.
// Exactly one source: Catalog with Store (app from a store), Release (NGC
// update) or neither (uploaded app, the signed task names its checksum).
type Transfer struct {
	Catalog *sign.Envelope `json:"catalog,omitempty"`
	Store   string         `json:"store,omitempty"`
	Release *sign.Envelope `json:"release,omitempty"`
	License *sign.Envelope `json:"license,omitempty"` // paid apps
	Key     enc.Bytes      `json:"key,omitempty"`     // paid apps: content key, unwrapped by the API
	Size    int64          `json:"size"`
	SHA256  enc.Bytes      `json:"sha256"`
}

// Validate checks t.
func (t Transfer) Validate() error {
	ok := t.Size > 0 && t.Size <= store.MaxPackage && len(t.SHA256) == 32 &&
		!(t.Catalog != nil && t.Release != nil) && (t.Catalog != nil) == (t.Store != "") &&
		(t.Store == "" || wire.ValidName(t.Store)) &&
		(len(t.Key) == 0) == (t.License == nil) && (len(t.Key) == 0 || (len(t.Key) == store.KeySize && t.Catalog != nil))
	if !ok {
		return ErrTransfer
	}
	return nil
}

// WriteTransfer writes the header: 4 bytes length (big endian), then the JSON.
func WriteTransfer(w io.Writer, t Transfer) error {
	if err := t.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if len(b) > MaxTransferHeader {
		return ErrTransfer
	}
	head := binary.BigEndian.AppendUint32(nil, uint32(len(b)))
	_, err = w.Write(append(head, b...))
	return err
}

// ReadTransfer reads exactly the header; the file follows in r.
func ReadTransfer(r io.Reader) (Transfer, error) {
	var n [4]byte
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return Transfer{}, ErrTransfer
	}
	size := binary.BigEndian.Uint32(n[:])
	if size == 0 || size > MaxTransferHeader {
		return Transfer{}, ErrTransfer
	}
	b := make([]byte, size)
	if _, err := io.ReadFull(r, b); err != nil {
		return Transfer{}, ErrTransfer
	}
	var t Transfer
	if err := enc.DecodeStrict(b, MaxTransferHeader, &t); err != nil {
		return Transfer{}, ErrTransfer
	}
	return t, t.Validate()
}
```

- [ ] **Step 5: Beispiele erzeugen und Tests laufen lassen**

Run:

```bash
go test ./contracts/core/ -run TestExamples -update
go test ./contracts/core/
go test -run=XXX -fuzz=FuzzParseParams -fuzztime=30s ./contracts/core/
go test -run=XXX -fuzz=FuzzReadTransfer -fuzztime=30s ./contracts/core/
```

Expected: `ok`; `contracts/testdata/core.json` angelegt; Fuzzing ohne Fund.

- [ ] **Step 6: Commit**

```bash
git add contracts/core contracts/testdata/core.json
git commit -m "Add the actions of the core apps" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---


### Task 17: Datenbank-Schnittstelle (`contracts/dbapi`)

Vertrag V4: API und Controller sprechen die Datenbank nur über diese Go-Schnittstellen an. Das Teil
`db/` setzt sie für MariaDB und SQLite um, beide müssen dieselben Tests bestehen
(`dbtest.Run`). `memstore` ist die Umsetzung im Speicher: Sie sagt, wie sich eine Umsetzung zu
verhalten hat, und API-Tests laufen ohne Datenbank dagegen.

Aufteilung:

- **Dokumente (`Blob`)**: Einstellungen, Fassungen der Vertrauensliste, Kataloge, Lizenzen, die
  Rechte je Rolle und die Oberflächen der Apps (`ui`-tar). Schlüssel sind Art und ID, `Seq` ordnet
  Fassungen.
- **Zeilen mit Abfragen**: Benutzer, Sitzungen, Apps, wartende Controller, Protokoll.
- **Einmalwerte (`Once`)**: Anmelde-Nonce, Kopplungsgeheimnis und Aufgaben-IDs gegen das
  Wiedereinspielen. Ein zweites Anlegen desselben Werts scheitert mit `ErrUsed`.
- **Sperre (`Lock`)**: damit beim Update nur eine API den Ablauf steuert (4.8).

Der Controller braucht nur `Local`, also Dokumente, Apps und Einmalwerte; `Store` setzt darauf auf.
Regeln für jede Umsetzung: nur SQL mit Platzhaltern, portables SQL (4.6), Zeitstempel als
Unix-Sekunden, IDs erzeugt der Aufrufer, jede Methode nimmt `context.Context`, alles ist
nebenläufig benutzbar.

**Files:**
- Create: `contracts/dbapi/dbapi.go`, `contracts/dbapi/memstore/memstore.go`,
  `contracts/dbapi/memstore/memstore_test.go`, `contracts/dbapi/dbtest/dbtest.go`

**Interfaces:**
- Consumes: `core.LogEntry`, `core.LogQuery`.
- Produces:
  - Blob-Arten `dbapi.BlobSetting = "setting"`, `BlobTrust = "trust"`, `BlobCatalog = "catalog"`,
    `BlobLicense = "license"`, `BlobRights = "rights"`, `BlobUI = "ui"`; Einmal-Arten `OnceNonce = "nonce"`, `OncePair = "pair"`, `OnceTask = "task"`;
    Einstellungsschlüssel `SettingInstallation = "installation"`, `SettingInstallKey = "installKey"`,
    `SettingUpdate = "update."` (Präfix je Ziel); Sperrname `LockUpdate = "update"`
  - `type dbapi.Blob struct{ Kind, ID string; Seq uint64; Data []byte; Updated int64 }`
  - `type dbapi.User struct{ ID, Name string; Roles []string; TOTP []byte; Locked int64; Failures int; LastLogin int64 }`
  - `type dbapi.Session struct{ Token []byte; User string; Cert, Proof, APIKey, APICert []byte; Created, Expires, Seen int64 }`
  - `type dbapi.App struct{ ID, Version, Store, Node string; Active bool; Manifest, SHA256 []byte; Grants []string; KeptData, Updated int64 }`
  - `type dbapi.Pending struct{ Name string; Key []byte; Since int64 }`
  - `type dbapi.Local interface{ BlobPut, BlobGet, BlobFrom, BlobDelete, AppPut, AppGet, AppList, AppDelete, OnceCreate, OnceUse, OncePurge, Close }`
  - `type dbapi.Store interface{ Local; UserPut, UserGet, UserByName, UserList, UserDelete, SessionPut, SessionGet, SessionTouch, SessionDelete, SessionPurge, PendingPut, PendingList, PendingDelete, LogAdd, LogQuery, LockTake, LockFree }`
  - Fehler `dbapi.ErrNotFound`, `ErrUsed`, `ErrInvalid`
  - `memstore.New() dbapi.Store`
  - `dbtest.Run(t *testing.T, open func(t *testing.T) dbapi.Store)`

- [ ] **Step 1: Schnittstelle schreiben**

`contracts/dbapi/dbapi.go`:

```go
// Package dbapi is the contract between API, controller and database (V4).
// The part db/ implements it for MariaDB and SQLite; memstore implements it
// in memory and dbtest checks every implementation.
package dbapi

import (
	"context"
	"errors"

	"github.com/sorglos-it/nextgencloud/contracts/core"
)

// Kinds of documents.
const (
	BlobSetting = "setting" // ID: setting key
	BlobTrust   = "trust"   // ID: version number as text, Seq: version number
	BlobCatalog = "catalog" // ID: store, Seq: catalog number
	BlobLicense = "license" // ID: app
	BlobRights  = "rights"  // ID: role, Data: core.RoleRights as JSON
	BlobUI      = "ui"      // ID: "<app>@<version>", Data: tar of the ui/ files
)

// Kinds of one-time values.
const (
	OnceNonce = "nonce" // login challenge; Data: user ID
	OncePair  = "pair"  // SHA-256 of a pairing secret
	OnceTask  = "task"  // ID of a signed task, against replay
)

// Setting keys and lock names.
const (
	SettingInstallation = "installation" // installation ID
	SettingInstallKey   = "installKey"   // X25519 private key of the installation, encrypted by the API
	SettingUpdate       = "update."      // prefix, e.g. "update.ngc-api": core.UpdateInfo as JSON
	LockUpdate          = "update"       // only one API drives a rollout
)

// Blob is a document.
type Blob struct {
	Kind    string
	ID      string
	Seq     uint64 // 0 where no order is needed
	Data    []byte
	Updated int64
}

// User is what the API keeps about a user besides the trust list.
type User struct {
	ID        string
	Name      string   // login name, unique
	Roles     []string // copy from the trust list, for queries
	TOTP      []byte   // secret, encrypted by the API; empty: no TOTP
	Locked    int64    // locked until this time
	Failures  int      // failed logins in a row
	LastLogin int64
}

// Session is a login. Token is session.TokenHash of the cookie value.
type Session struct {
	Token   []byte
	User    string
	Cert    []byte // client certificate payload
	Proof   []byte // the user's proof over it, as JSON
	APIKey  []byte // the API's private session key, encrypted by the API
	APICert []byte // the API certificate, as JSON
	Created int64
	Expires int64
	Seen    int64
}

// App is an app of this installation.
type App struct {
	ID       string
	Version  string // empty if only kept data remains
	Store    string
	Node     string // controller it runs on
	Active   bool
	Manifest []byte // manifest.json as delivered
	SHA256   []byte // checksum of the package
	Grants   []string
	KeptData int64
	Updated  int64
}

// Pending is a controller that paired but is not in the trust list yet.
type Pending struct {
	Name  string
	Key   []byte // its Noise key
	Since int64
}

var (
	ErrNotFound = errors.New("dbapi: not found")
	ErrUsed     = errors.New("dbapi: value already used")
	ErrInvalid  = errors.New("dbapi: invalid value")
)

// Local is what a controller needs; it works without an API.
type Local interface {
	// BlobPut inserts or replaces the document with this kind and ID.
	BlobPut(ctx context.Context, b Blob) error
	// BlobGet returns a document or ErrNotFound.
	BlobGet(ctx context.Context, kind, id string) (Blob, error)
	// BlobFrom returns the documents of a kind with Seq >= seq, ascending by Seq.
	BlobFrom(ctx context.Context, kind string, seq uint64) ([]Blob, error)
	// BlobDelete removes a document; removing what is not there is no error.
	BlobDelete(ctx context.Context, kind, id string) error

	AppPut(ctx context.Context, a App) error
	AppGet(ctx context.Context, id string) (App, error)
	AppList(ctx context.Context) ([]App, error)
	AppDelete(ctx context.Context, id string) error

	// OnceCreate remembers a one-time value until expires. A value that is
	// already there gives ErrUsed, even after it was used; OncePurge frees
	// expired values again.
	OnceCreate(ctx context.Context, kind string, value []byte, data string, expires int64) error
	// OnceUse marks a value as used and returns its data. It gives
	// ErrNotFound if it is unknown or expired and ErrUsed on a second use.
	OnceUse(ctx context.Context, kind string, value []byte, now int64) (string, error)
	// OncePurge removes values that expired before now.
	OncePurge(ctx context.Context, now int64) error

	Close() error
}

// Store is what the API needs.
type Store interface {
	Local

	// UserPut inserts or replaces a user. Names are unique: the same name
	// for another ID gives ErrInvalid.
	UserPut(ctx context.Context, u User) error
	UserGet(ctx context.Context, id string) (User, error)
	UserByName(ctx context.Context, name string) (User, error)
	UserList(ctx context.Context) ([]User, error)
	UserDelete(ctx context.Context, id string) error

	SessionPut(ctx context.Context, s Session) error
	SessionGet(ctx context.Context, token []byte) (Session, error)
	// SessionTouch sets Seen; it gives ErrNotFound for an unknown session.
	SessionTouch(ctx context.Context, token []byte, now int64) error
	SessionDelete(ctx context.Context, token []byte) error
	// SessionPurge removes sessions that expired before now.
	SessionPurge(ctx context.Context, now int64) error

	PendingPut(ctx context.Context, p Pending) error
	PendingList(ctx context.Context) ([]Pending, error)
	PendingDelete(ctx context.Context, key []byte) error

	LogAdd(ctx context.Context, e core.LogEntry) error
	// LogQuery returns entries older than q.Before (0: now), newest first,
	// at most q.Limit, for q.User if set.
	LogQuery(ctx context.Context, q core.LogQuery) ([]core.LogEntry, error)

	// LockTake takes the lock until until, unless someone else holds it and
	// their time has not passed at now. The same owner may take it again and
	// moves the time.
	LockTake(ctx context.Context, name, owner string, now, until int64) (bool, error)
	// LockFree gives the lock back; another owner is ignored.
	LockFree(ctx context.Context, name, owner string) error
}
```


- [ ] **Step 2: Umsetzung im Speicher schreiben**

`contracts/dbapi/memstore/memstore.go`:

```go
// Package memstore implements dbapi.Store in memory. It is the reference for
// every other implementation, and the API can test against it without a
// database. It is not meant for production.
package memstore

import (
	"bytes"
	"cmp"
	"context"
	"encoding/hex"
	"slices"
	"strings"
	"sync"

	"github.com/sorglos-it/nextgencloud/contracts/core"
	"github.com/sorglos-it/nextgencloud/contracts/dbapi"
)

type store struct {
	mu       sync.Mutex
	blobs    map[string]dbapi.Blob
	users    map[string]dbapi.User
	sessions map[string]dbapi.Session
	apps     map[string]dbapi.App
	pending  map[string]dbapi.Pending
	once     map[string]onceValue
	locks    map[string]lockValue
	log      []core.LogEntry
}

type onceValue struct {
	data    string
	expires int64
	used    bool
}

type lockValue struct {
	owner string
	until int64
}

// New returns an empty store.
func New() dbapi.Store {
	return &store{
		blobs: map[string]dbapi.Blob{}, users: map[string]dbapi.User{}, sessions: map[string]dbapi.Session{},
		apps: map[string]dbapi.App{}, pending: map[string]dbapi.Pending{}, once: map[string]onceValue{},
		locks: map[string]lockValue{},
	}
}

func key(kind, id string) string { return kind + "\x00" + id }

func (s *store) Close() error { return nil }

func (s *store) BlobPut(_ context.Context, b dbapi.Blob) error {
	if b.Kind == "" || b.ID == "" {
		return dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b.Data = bytes.Clone(b.Data)
	s.blobs[key(b.Kind, b.ID)] = b
	return nil
}

func (s *store) BlobGet(_ context.Context, kind, id string) (dbapi.Blob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.blobs[key(kind, id)]
	if !ok {
		return dbapi.Blob{}, dbapi.ErrNotFound
	}
	b.Data = bytes.Clone(b.Data)
	return b, nil
}

func (s *store) BlobFrom(_ context.Context, kind string, seq uint64) ([]dbapi.Blob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []dbapi.Blob
	for _, b := range s.blobs {
		if b.Kind == kind && b.Seq >= seq {
			b.Data = bytes.Clone(b.Data)
			out = append(out, b)
		}
	}
	slices.SortFunc(out, func(a, b dbapi.Blob) int {
		if a.Seq != b.Seq {
			return cmp.Compare(a.Seq, b.Seq)
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

func (s *store) BlobDelete(_ context.Context, kind, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.blobs, key(kind, id))
	return nil
}

func cloneApp(a dbapi.App) dbapi.App {
	a.Manifest, a.SHA256, a.Grants = bytes.Clone(a.Manifest), bytes.Clone(a.SHA256), slices.Clone(a.Grants)
	return a
}

func (s *store) AppPut(_ context.Context, a dbapi.App) error {
	if a.ID == "" {
		return dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apps[a.ID] = cloneApp(a)
	return nil
}

func (s *store) AppGet(_ context.Context, id string) (dbapi.App, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.apps[id]
	if !ok {
		return dbapi.App{}, dbapi.ErrNotFound
	}
	return cloneApp(a), nil
}

func (s *store) AppList(_ context.Context) ([]dbapi.App, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]dbapi.App, 0, len(s.apps))
	for _, a := range s.apps {
		out = append(out, cloneApp(a))
	}
	slices.SortFunc(out, func(a, b dbapi.App) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

func (s *store) AppDelete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.apps, id)
	return nil
}

func (s *store) OnceCreate(_ context.Context, kind string, value []byte, data string, expires int64) error {
	if kind == "" || len(value) == 0 {
		return dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(kind, hex.EncodeToString(value))
	if _, ok := s.once[k]; ok {
		return dbapi.ErrUsed
	}
	s.once[k] = onceValue{data: data, expires: expires}
	return nil
}

func (s *store) OnceUse(_ context.Context, kind string, value []byte, now int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(kind, hex.EncodeToString(value))
	v, ok := s.once[k]
	switch {
	case !ok || v.expires <= now:
		return "", dbapi.ErrNotFound
	case v.used:
		return "", dbapi.ErrUsed
	}
	v.used = true
	s.once[k] = v
	return v.data, nil
}

func (s *store) OncePurge(_ context.Context, now int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.once {
		if v.expires <= now {
			delete(s.once, k)
		}
	}
	return nil
}

func (s *store) UserPut(_ context.Context, u dbapi.User) error {
	if u.ID == "" || u.Name == "" {
		return dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, other := range s.users {
		if other.Name == u.Name && id != u.ID {
			return dbapi.ErrInvalid
		}
	}
	u.Roles, u.TOTP = slices.Clone(u.Roles), bytes.Clone(u.TOTP)
	s.users[u.ID] = u
	return nil
}

func (s *store) UserGet(_ context.Context, id string) (dbapi.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return dbapi.User{}, dbapi.ErrNotFound
	}
	u.Roles, u.TOTP = slices.Clone(u.Roles), bytes.Clone(u.TOTP)
	return u, nil
}

func (s *store) UserByName(ctx context.Context, name string) (dbapi.User, error) {
	s.mu.Lock()
	id := ""
	for _, u := range s.users {
		if u.Name == name {
			id = u.ID
		}
	}
	s.mu.Unlock()
	if id == "" {
		return dbapi.User{}, dbapi.ErrNotFound
	}
	return s.UserGet(ctx, id)
}

func (s *store) UserList(_ context.Context) ([]dbapi.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]dbapi.User, 0, len(s.users))
	for _, u := range s.users {
		u.Roles, u.TOTP = slices.Clone(u.Roles), bytes.Clone(u.TOTP)
		out = append(out, u)
	}
	slices.SortFunc(out, func(a, b dbapi.User) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (s *store) UserDelete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.users, id)
	return nil
}

func cloneSession(s dbapi.Session) dbapi.Session {
	s.Token, s.Cert, s.Proof = bytes.Clone(s.Token), bytes.Clone(s.Cert), bytes.Clone(s.Proof)
	s.APIKey, s.APICert = bytes.Clone(s.APIKey), bytes.Clone(s.APICert)
	return s
}

func (s *store) SessionPut(_ context.Context, v dbapi.Session) error {
	if len(v.Token) == 0 || v.User == "" {
		return dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[hex.EncodeToString(v.Token)] = cloneSession(v)
	return nil
}

func (s *store) SessionGet(_ context.Context, token []byte) (dbapi.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[hex.EncodeToString(token)]
	if !ok {
		return dbapi.Session{}, dbapi.ErrNotFound
	}
	return cloneSession(v), nil
}

func (s *store) SessionTouch(_ context.Context, token []byte, now int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := hex.EncodeToString(token)
	v, ok := s.sessions[k]
	if !ok {
		return dbapi.ErrNotFound
	}
	v.Seen = now
	s.sessions[k] = v
	return nil
}

func (s *store) SessionDelete(_ context.Context, token []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, hex.EncodeToString(token))
	return nil
}

func (s *store) SessionPurge(_ context.Context, now int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.sessions {
		if v.Expires <= now {
			delete(s.sessions, k)
		}
	}
	return nil
}

func (s *store) PendingPut(_ context.Context, p dbapi.Pending) error {
	if p.Name == "" || len(p.Key) == 0 {
		return dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p.Key = bytes.Clone(p.Key)
	s.pending[hex.EncodeToString(p.Key)] = p
	return nil
}

func (s *store) PendingList(_ context.Context) ([]dbapi.Pending, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]dbapi.Pending, 0, len(s.pending))
	for _, p := range s.pending {
		p.Key = bytes.Clone(p.Key)
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b dbapi.Pending) int {
		if a.Since != b.Since {
			return cmp.Compare(a.Since, b.Since)
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

func (s *store) PendingDelete(_ context.Context, k []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, hex.EncodeToString(k))
	return nil
}

func (s *store) LogAdd(_ context.Context, e core.LogEntry) error {
	if e.Kind == "" || e.Time <= 0 {
		return dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = append(s.log, e)
	return nil
}

func (s *store) LogQuery(_ context.Context, q core.LogQuery) ([]core.LogEntry, error) {
	if q.Limit <= 0 {
		return nil, dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.LogEntry, 0, q.Limit)
	rows := slices.Clone(s.log)
	slices.SortFunc(rows, func(a, b core.LogEntry) int { return cmp.Compare(b.Time, a.Time) })
	for _, e := range rows {
		if (q.Before != 0 && e.Time >= q.Before) || (q.User != "" && e.User != q.User) {
			continue
		}
		if out = append(out, e); len(out) == q.Limit {
			break
		}
	}
	return out, nil
}

func (s *store) LockTake(_ context.Context, name, owner string, now, until int64) (bool, error) {
	if name == "" || owner == "" || until <= now {
		return false, dbapi.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.locks[name]; ok && l.owner != owner && l.until > now {
		return false, nil
	}
	s.locks[name] = lockValue{owner: owner, until: until}
	return true, nil
}

func (s *store) LockFree(_ context.Context, name, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.locks[name]; ok && l.owner == owner {
		delete(s.locks, name)
	}
	return nil
}
```

`contracts/dbapi/memstore/memstore_test.go`:

```go
package memstore_test

import (
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/dbapi"
	"github.com/sorglos-it/nextgencloud/contracts/dbapi/dbtest"
	"github.com/sorglos-it/nextgencloud/contracts/dbapi/memstore"
)

func TestMemstore(t *testing.T) {
	dbtest.Run(t, func(t *testing.T) dbapi.Store { return memstore.New() })
}
```


- [ ] **Step 3: Konformitätstests schreiben**

`contracts/dbapi/dbtest/dbtest.go`:

```go
// Package dbtest checks an implementation of dbapi against the contract.
// Every implementation in db/ has to pass Run, against MariaDB and SQLite.
package dbtest

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/core"
	"github.com/sorglos-it/nextgencloud/contracts/dbapi"
)

var ctx = context.Background()

// Run checks a store. open returns a fresh, empty store for every subtest.
func Run(t *testing.T, open func(t *testing.T) dbapi.Store) {
	t.Helper()
	tests := map[string]func(*testing.T, dbapi.Store){
		"blobs": testBlobs, "apps": testApps, "once": testOnce, "users": testUsers,
		"sessions": testSessions, "pending": testPending, "log": testLog, "locks": testLocks,
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) {
			s := open(t)
			t.Cleanup(func() {
				if err := s.Close(); err != nil {
					t.Errorf("close: %v", err)
				}
			})
			fn(t, s)
		})
	}
}

func testBlobs(t *testing.T, s dbapi.Store) {
	if _, err := s.BlobGet(ctx, dbapi.BlobSetting, "nothing"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("missing blob: %v", err)
	}
	put := func(kind, id string, seq uint64, data string) {
		t.Helper()
		if err := s.BlobPut(ctx, dbapi.Blob{Kind: kind, ID: id, Seq: seq, Data: []byte(data), Updated: 1790000000}); err != nil {
			t.Fatalf("put %s/%s: %v", kind, id, err)
		}
	}
	put(dbapi.BlobSetting, dbapi.SettingInstallation, 0, "inst-1")
	put(dbapi.BlobTrust, "1", 1, "v1")
	put(dbapi.BlobTrust, "2", 2, "v2")
	put(dbapi.BlobTrust, "3", 3, "v3")
	b, err := s.BlobGet(ctx, dbapi.BlobSetting, dbapi.SettingInstallation)
	if err != nil || string(b.Data) != "inst-1" || b.Updated != 1790000000 {
		t.Fatalf("get: %+v, %v", b, err)
	}
	put(dbapi.BlobSetting, dbapi.SettingInstallation, 0, "inst-2")
	if b, _ := s.BlobGet(ctx, dbapi.BlobSetting, dbapi.SettingInstallation); string(b.Data) != "inst-2" {
		t.Error("put must replace")
	}
	list, err := s.BlobFrom(ctx, dbapi.BlobTrust, 2)
	if err != nil || len(list) != 2 || list[0].Seq != 2 || list[1].Seq != 3 {
		t.Fatalf("from: %+v, %v", list, err)
	}
	if list, err := s.BlobFrom(ctx, dbapi.BlobLicense, 0); err != nil || len(list) != 0 {
		t.Errorf("other kind: %+v, %v", list, err)
	}
	if err := s.BlobDelete(ctx, dbapi.BlobTrust, "2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BlobGet(ctx, dbapi.BlobTrust, "2"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
	if err := s.BlobDelete(ctx, dbapi.BlobTrust, "2"); err != nil {
		t.Errorf("deleting twice must be fine: %v", err)
	}
	if err := s.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("empty ID: %v", err)
	}
	big := bytes.Repeat([]byte{7}, 1<<20)
	if err := s.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobUI, ID: "org.sorglos.terminal@1.0.0", Data: big}); err != nil {
		t.Fatalf("blob of 1 MiB: %v", err)
	}
	if b, err := s.BlobGet(ctx, dbapi.BlobUI, "org.sorglos.terminal@1.0.0"); err != nil || !bytes.Equal(b.Data, big) {
		t.Errorf("blob of 1 MiB: %v", err)
	}
}

func testApps(t *testing.T, s dbapi.Store) {
	if _, err := s.AppGet(ctx, "org.sorglos.terminal"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("missing app: %v", err)
	}
	a := dbapi.App{ID: "org.sorglos.terminal", Version: "1.0.0", Store: "global", Node: "ctl-1", Active: true,
		Manifest: []byte(`{"id":"org.sorglos.terminal"}`), SHA256: bytes.Repeat([]byte{1}, 32),
		Grants: []string{"network"}, Updated: 1790000000}
	if err := s.AppPut(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.AppPut(ctx, dbapi.App{ID: "org.sorglos.calculator", Version: "1.0.0", Store: "global", Node: "ctl-1"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.AppGet(ctx, a.ID)
	if err != nil || got.Version != "1.0.0" || !got.Active || len(got.Grants) != 1 || !bytes.Equal(got.SHA256, a.SHA256) {
		t.Fatalf("get: %+v, %v", got, err)
	}
	a.Active, a.Version = false, "1.1.0"
	if err := s.AppPut(ctx, a); err != nil {
		t.Fatal(err)
	}
	list, err := s.AppList(ctx)
	if err != nil || len(list) != 2 || list[0].ID != "org.sorglos.calculator" || list[1].Active {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if err := s.AppDelete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.AppList(ctx); len(list) != 1 {
		t.Error("delete")
	}
	if err := s.AppPut(ctx, dbapi.App{}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("app without ID: %v", err)
	}
}

func testOnce(t *testing.T, s dbapi.Store) {
	nonce := bytes.Repeat([]byte{0x53}, 32)
	if err := s.OnceCreate(ctx, dbapi.OnceNonce, nonce, "user-1", 1790000100); err != nil {
		t.Fatal(err)
	}
	if err := s.OnceCreate(ctx, dbapi.OnceNonce, nonce, "user-2", 1790000100); !errors.Is(err, dbapi.ErrUsed) {
		t.Errorf("second create: %v", err)
	}
	if err := s.OnceCreate(ctx, dbapi.OnceTask, nonce, "", 1790000100); err != nil {
		t.Errorf("same value, other kind: %v", err)
	}
	data, err := s.OnceUse(ctx, dbapi.OnceNonce, nonce, 1790000000)
	if err != nil || data != "user-1" {
		t.Fatalf("use: %q, %v", data, err)
	}
	if _, err := s.OnceUse(ctx, dbapi.OnceNonce, nonce, 1790000000); !errors.Is(err, dbapi.ErrUsed) {
		t.Errorf("second use: %v", err)
	}
	if _, err := s.OnceUse(ctx, dbapi.OnceTask, nonce, 1790000100); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("expired: %v", err)
	}
	if _, err := s.OnceUse(ctx, dbapi.OnceNonce, []byte("other"), 1790000000); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	if err := s.OncePurge(ctx, 1790000100); err != nil {
		t.Fatal(err)
	}
	if err := s.OnceCreate(ctx, dbapi.OnceNonce, nonce, "user-3", 1790000200); err != nil {
		t.Errorf("after purge the value is free again: %v", err)
	}
}

func testUsers(t *testing.T, s dbapi.Store) {
	if _, err := s.UserGet(ctx, "nobody"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("missing user: %v", err)
	}
	admin := dbapi.User{ID: "u-admin", Name: "admin", Roles: []string{"admin"}, TOTP: []byte("secret"), LastLogin: 1790000000}
	alice := dbapi.User{ID: "u-alice", Name: "alice", Roles: []string{"user"}}
	for _, u := range []dbapi.User{admin, alice} {
		if err := s.UserPut(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.UserByName(ctx, "admin")
	if err != nil || got.ID != "u-admin" || string(got.TOTP) != "secret" {
		t.Fatalf("by name: %+v, %v", got, err)
	}
	admin.Failures, admin.Locked = 3, 1790000600
	if err := s.UserPut(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.UserGet(ctx, "u-admin"); got.Failures != 3 || got.Locked != 1790000600 {
		t.Errorf("update: %+v", got)
	}
	if err := s.UserPut(ctx, dbapi.User{ID: "u-evil", Name: "admin"}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("name taken: %v", err)
	}
	list, err := s.UserList(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "admin" {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if err := s.UserDelete(ctx, "u-alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserGet(ctx, "u-alice"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
	if err := s.UserPut(ctx, dbapi.User{ID: "u-x"}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("user without name: %v", err)
	}
}

func testSessions(t *testing.T, s dbapi.Store) {
	token := bytes.Repeat([]byte{0x11}, 32)
	v := dbapi.Session{Token: token, User: "u-admin", Cert: []byte(`{"user":"u-admin"}`), Proof: []byte(`{}`),
		APIKey: bytes.Repeat([]byte{2}, 48), APICert: []byte(`{}`), Created: 1790000000, Expires: 1790003600, Seen: 1790000000}
	if err := s.SessionPut(ctx, v); err != nil {
		t.Fatal(err)
	}
	got, err := s.SessionGet(ctx, token)
	if err != nil || got.User != "u-admin" || !bytes.Equal(got.APIKey, v.APIKey) {
		t.Fatalf("get: %+v, %v", got, err)
	}
	if err := s.SessionTouch(ctx, token, 1790000500); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SessionGet(ctx, token); got.Seen != 1790000500 {
		t.Errorf("touch: %+v", got)
	}
	if err := s.SessionTouch(ctx, []byte("nope"), 1790000500); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("touching an unknown session: %v", err)
	}
	if err := s.SessionPurge(ctx, 1790001000); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionGet(ctx, token); err != nil {
		t.Errorf("purge removed a session that is still valid: %v", err)
	}
	if err := s.SessionPurge(ctx, 1790003600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionGet(ctx, token); !errors.Is(err, dbapi.ErrNotFound) {
		t.Errorf("after purge: %v", err)
	}
	if err := s.SessionPut(ctx, dbapi.Session{Token: token}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("session without user: %v", err)
	}
	if err := s.SessionDelete(ctx, token); err != nil {
		t.Errorf("deleting what is gone: %v", err)
	}
}

func testPending(t *testing.T, s dbapi.Store) {
	k1, k2 := bytes.Repeat([]byte{0xC1}, 32), bytes.Repeat([]byte{0xC2}, 32)
	if err := s.PendingPut(ctx, dbapi.Pending{Name: "dienste", Key: k1, Since: 1790000100}); err != nil {
		t.Fatal(err)
	}
	if err := s.PendingPut(ctx, dbapi.Pending{Name: "web", Key: k2, Since: 1790000000}); err != nil {
		t.Fatal(err)
	}
	list, err := s.PendingList(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "web" {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if err := s.PendingDelete(ctx, k2); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.PendingList(ctx); len(list) != 1 || list[0].Name != "dienste" {
		t.Error("delete")
	}
	if err := s.PendingPut(ctx, dbapi.Pending{Name: "x"}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("without key: %v", err)
	}
}

func testLog(t *testing.T, s dbapi.Store) {
	entries := []core.LogEntry{
		{Time: 1790000001, User: "u-admin", Kind: core.LogLogin, Result: "ok"},
		{Time: 1790000002, User: "u-alice", Kind: core.LogChannel, App: "org.sorglos.terminal", Action: "shell", Node: "ctl-1", Result: "ok"},
		{Time: 1790000003, User: "u-admin", Kind: core.LogTask, App: "ngc.store", Action: "activate", Node: "ctl-1", Result: "ok"},
	}
	for _, e := range entries {
		if err := s.LogAdd(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.LogQuery(ctx, core.LogQuery{Limit: 10})
	if err != nil || len(got) != 3 || got[0].Time != 1790000003 {
		t.Fatalf("newest first: %+v, %v", got, err)
	}
	if got, _ := s.LogQuery(ctx, core.LogQuery{Limit: 2}); len(got) != 2 {
		t.Error("limit")
	}
	if got, _ := s.LogQuery(ctx, core.LogQuery{Limit: 10, Before: 1790000003}); len(got) != 2 || got[0].Time != 1790000002 {
		t.Error("before")
	}
	if got, _ := s.LogQuery(ctx, core.LogQuery{Limit: 10, User: "u-alice"}); len(got) != 1 || got[0].App != "org.sorglos.terminal" {
		t.Error("user")
	}
	if err := s.LogAdd(ctx, core.LogEntry{Time: 1790000004}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("entry without kind: %v", err)
	}
	if _, err := s.LogQuery(ctx, core.LogQuery{}); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("query without limit: %v", err)
	}
}

func testLocks(t *testing.T, s dbapi.Store) {
	const now = 1790000000
	ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-1", now, now+60)
	if err != nil || !ok {
		t.Fatalf("take: %v, %v", ok, err)
	}
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-2", now, now+60); err != nil || ok {
		t.Errorf("two owners: %v, %v", ok, err)
	}
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-1", now, now+120); err != nil || !ok {
		t.Errorf("same owner again: %v, %v", ok, err)
	}
	if ok, err := s.LockTake(ctx, dbapi.LockUpdate, "api-2", now+121, now+180); err != nil || !ok {
		t.Errorf("after the time ran out: %v, %v", ok, err)
	}
	if err := s.LockFree(ctx, dbapi.LockUpdate, "api-1"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.LockTake(ctx, dbapi.LockUpdate, "api-1", now+121, now+180); ok {
		t.Error("the wrong owner must not free the lock")
	}
	if err := s.LockFree(ctx, dbapi.LockUpdate, "api-2"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.LockTake(ctx, dbapi.LockUpdate, "api-1", now+121, now+180); !ok {
		t.Error("after LockFree")
	}
	if _, err := s.LockTake(ctx, dbapi.LockUpdate, "api-1", now, now); !errors.Is(err, dbapi.ErrInvalid) {
		t.Errorf("lock without time: %v", err)
	}
}
```

- [ ] **Step 4: Tests laufen lassen**

Run: `go test ./contracts/dbapi/...`
Expected: `ok` für `memstore`; alle acht Untertests laufen durch.

- [ ] **Step 5: Commit**

```bash
git add contracts/dbapi
git commit -m "Add the database interface with a reference implementation and its tests" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---


### Task 18: Konfiguration und Pfade (`contracts/config`)

Vertrag V6, erster Teil: Wo liegt was, wie heißen die Dienste, wie sehen die Konfigurationsdateien
aus. Die ulh-Skripte schreiben sie, API und Controller lesen sie, PHP liest `web.json`.

| Datei | Wer schreibt | Wer liest |
|---|---|---|
| `/etc/ngc/web.json` | ulh | PHP des Webinterface, Controller (Lastverteilung in Apache) |
| `/etc/ngc/trust-anchor.json` | Controller auf dem Webserver | PHP, das ihn an den Browser ausliefert |
| `/etc/ngc/api.json` (0600, root) | ulh, beim Koppeln weiterer APIs ergänzt | API |
| `/etc/ngc/controller.json` (0600, root) | ulh, Controller schreibt seine Knoten-ID nach | Controller |

Feste Pfade: Programme in `/usr/lib/ngc/`, Daten in `/var/lib/ngc/` (API, Controller, `apps/<app>/`
als verschlüsselter Datenordner, `staged/` für vorbereitete Updates), Webinterface in
`/var/www/ngc/`, Dienste `ngc-api.service` und `ngc-controller.service`, Port der API 7443 (TLS,
`/api/` von Apache und `/tunnel` von den Controllern).

**Files:**
- Create: `contracts/config/config.go`, `contracts/config/config_test.go`,
  `contracts/testdata/config/web.json`, `contracts/testdata/config/api.json`,
  `contracts/testdata/config/controller.json`

**Interfaces:**
- Consumes: `enc.Bytes`, `enc.DecodeStrict`, `proof.RP`, `trust.Anchor`, `trust.ValidID`, `tunnel.PartWeb` … `tunnel.PartServices`,
  `wire.ValidName`.
- Produces:
  - Pfade und Namen `config.Dir`, `WebFile`, `AnchorFile`, `APIFile`, `ControllerFile`, `WebRoot`, `BinDir`,
    `DataDir`, `AppDataDir`, `StageDir`, `RunDir`, `APIPort = 7443`, `ServiceAPI = "ngc-api.service"`,
    `ServiceController = "ngc-controller.service"`, `MaxConfig = 1 << 20`,
    Vorgaben `DefaultSessionLifetime = 43200`, `DefaultIdleTimeout = 1800`, `DefaultPodman = "/usr/bin/podman"`
  - `type config.Web struct{ APIs []string; RP proof.RP }` (JSON `apis`, `rp`)
  - `type config.DB struct{ Driver string; Nodes []string; Name, User, Password, Path string; Key enc.Bytes }`
    (JSON `driver`, `nodes`, `name`, `user`, `password`, `path`, `key`), Treiber `DriverMariaDB = "mariadb"`, `DriverSQLite = "sqlite"`
  - `type config.API struct{ Node, Listen, Cert, CertKey string; DB DB; NoiseKey, SignKey, SecretKey, ReleaseKey, StoreKey enc.Bytes; StoreURL string; SessionLifetime, IdleTimeout int64 }`
  - `type config.APIRef struct{ URL string; Key enc.Bytes }` (JSON `url`, `key`)
  - `type config.Controller struct{ Node, Name string; NoiseKey enc.Bytes; APIs []APIRef; Anchor trust.Anchor; DB string; DBKey, ReleaseKey enc.Bytes; Parts []string; Podman, WebConfig string }`
  - `config.LoadWeb(path) (*Web, error)`, `config.LoadAPI(path) (*API, error)`, `config.LoadController(path) (*Controller, error)`,
    `config.SaveController(path string, c *Controller) error` (0600), `config.LoadAnchor(path) (trust.Anchor, error)`,
    `config.SaveAnchor(path string, a trust.Anchor) error` (0644)
  - `(*Web) Validate() error`, `(*API) Validate() error`, `(*Controller) Validate() error`
  - Fehler `config.ErrConfig`

- [ ] **Step 1: Beispiele und Test schreiben**

`contracts/testdata/config/web.json`:

```json
{
  "apis": ["https://api-1.ngc.test:7443", "https://api-2.ngc.test:7443"],
  "rp": {
    "id": "ngc.example.org",
    "origins": ["https://ngc.example.org"]
  }
}
```

`contracts/testdata/config/api.json`:

```json
{
  "node": "api-1",
  "listen": ":7443",
  "cert": "/etc/ngc/tls/api.crt",
  "certKey": "/etc/ngc/tls/api.key",
  "db": {
    "driver": "mariadb",
    "nodes": ["db-1.ngc.test:3306", "db-2.ngc.test:3306", "db-3.ngc.test:3306"],
    "name": "ngc",
    "user": "ngc",
    "password": "changeme"
  },
  "noiseKey": "oaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaE",
  "signKey": "oqKioqKioqKioqKioqKioqKioqKioqKioqKioqKioqI",
  "secretKey": "o6Ojo6Ojo6Ojo6Ojo6Ojo6Ojo6Ojo6Ojo6Ojo6Ojo6M",
  "releaseKey": "pKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKQ",
  "storeKey": "paWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaU",
  "storeUrl": "https://store.nextgencloud.org"
}
```

`contracts/testdata/config/controller.json`:

```json
{
  "name": "dienste",
  "noiseKey": "wcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcE",
  "apis": [
    {
      "url": "wss://api-1.ngc.test:7443/tunnel",
      "key": "oaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaE"
    }
  ],
  "anchor": {
    "seq": 1,
    "hash": "q6urq6urq6urq6urq6urq6urq6urq6urq6urq6urq6s"
  },
  "db": "/var/lib/ngc/controller/state.db",
  "dbKey": "zMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzA",
  "releaseKey": "pKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKSkpKQ",
  "parts": ["services"]
}
```

Die Schlüssel in den Beispielen sind erfundene Testwerte aus lauter gleichen Bytes: 32 Byte
(`dbKey` und ein SQLite-Schlüssel 64 Byte), base64url ohne Auffüllung, also 43 beziehungsweise
86 Zeichen. Nachrechnen: `python -c "import base64;print(base64.urlsafe_b64encode(bytes([0xA1])*32).decode().rstrip('='))"`.

`contracts/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/trust"
)

func example(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "testdata", "config", name)
}

func TestExamples(t *testing.T) {
	w, err := LoadWeb(example(t, "web.json"))
	if err != nil || len(w.APIs) != 2 || w.RP.ID != "ngc.example.org" {
		t.Fatalf("web: %+v, %v", w, err)
	}
	a, err := LoadAPI(example(t, "api.json"))
	if err != nil || a.Node != "api-1" || a.DB.Driver != DriverMariaDB || len(a.DB.Nodes) != 3 {
		t.Fatalf("api: %+v, %v", a, err)
	}
	if a.SessionLifetime != DefaultSessionLifetime || a.IdleTimeout != DefaultIdleTimeout {
		t.Errorf("defaults: %+v", a)
	}
	c, err := LoadController(example(t, "controller.json"))
	if err != nil || c.Name != "dienste" || len(c.APIs) != 1 || c.Anchor.Seq != 1 || c.Podman != DefaultPodman {
		t.Fatalf("controller: %+v, %v", c, err)
	}
}

func TestSaveController(t *testing.T) {
	c, err := LoadController(example(t, "controller.json"))
	if err != nil {
		t.Fatal(err)
	}
	c.Node = "ctl-1"
	path := filepath.Join(t.TempDir(), "controller.json")
	if err := SaveController(path, c); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v: the file holds keys", info.Mode().Perm())
	}
	again, err := LoadController(path)
	if err != nil || again.Node != "ctl-1" {
		t.Fatalf("round trip: %+v, %v", again, err)
	}
}

func TestAnchor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust-anchor.json")
	want := trust.Anchor{Seq: 7, Hash: make([]byte, 32)}
	if err := SaveAnchor(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAnchor(path)
	if err != nil || got.Seq != 7 || len(got.Hash) != 32 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := LoadAnchor(filepath.Join(t.TempDir(), "gone.json")); err == nil {
		t.Error("missing file")
	}
}

func TestValidate(t *testing.T) {
	w, _ := LoadWeb(example(t, "web.json"))
	a, _ := LoadAPI(example(t, "api.json"))
	c, _ := LoadController(example(t, "controller.json"))
	cases := map[string]func(){
		"web without apis":     func() { w.APIs = nil },
		"web with plain http":  func() { w.APIs = []string{"http://api-1.ngc.test:7443"} },
		"api without node":     func() { a.Node = "" },
		"api with short key":   func() { a.NoiseKey = []byte{1} },
		"api with sqlite path": func() { a.DB.Driver, a.DB.Path = DriverSQLite, "" },
		"api with db nodes":    func() { a.DB.Driver, a.DB.Nodes = DriverSQLite, []string{"db-1:3306"} },
		"controller name":      func() { c.Name = "Server 1" },
		"controller api url":   func() { c.APIs[0].URL = "https://api-1.ngc.test:7443/tunnel" },
		"controller parts":     func() { c.Parts = []string{"mail"} },
		"controller db key":    func() { c.DBKey = make([]byte, 32) },
	}
	for name, mutate := range cases {
		w2, a2, c2 := *w, *a, *c
		mutate()
		var err error
		switch {
		case name[:3] == "web":
			err = w.Validate()
		case name[:3] == "api":
			err = a.Validate()
		default:
			err = c.Validate()
		}
		if err != ErrConfig {
			t.Errorf("%s: %v", name, err)
		}
		*w, *a, *c = w2, a2, c2
	}
}

func FuzzLoad(f *testing.F) {
	b, err := os.ReadFile(filepath.Join("..", "testdata", "config", "api.json"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(b)
	f.Fuzz(func(t *testing.T, b []byte) {
		path := filepath.Join(t.TempDir(), "c.json")
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Skip()
		}
		_, _ = LoadWeb(path)
		_, _ = LoadAPI(path)
		_, _ = LoadController(path)
	})
}
```

- [ ] **Step 2: Test laufen lassen, er muss scheitern**

Run: `go test ./contracts/config/`
Expected: FAIL, `undefined: LoadWeb`

- [ ] **Step 3: Umsetzung schreiben**

`contracts/config/config.go`:

```go
// Package config defines the configuration files, paths and ports of NGC
// (contract V6). The ulh scripts write them, the parts read them.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/tunnel"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

// Files, directories, services and ports.
const (
	Dir            = "/etc/ngc"
	WebFile        = "/etc/ngc/web.json"
	AnchorFile     = "/etc/ngc/trust-anchor.json"
	APIFile        = "/etc/ngc/api.json"
	ControllerFile = "/etc/ngc/controller.json"
	WebRoot        = "/var/www/ngc"
	BinDir         = "/usr/lib/ngc"
	DataDir        = "/var/lib/ngc"
	AppDataDir     = "/var/lib/ngc/apps"   // <app>/ per app, encrypted, mounted as /data
	StageDir       = "/var/lib/ngc/staged" // checked update files
	RunDir         = "/run/ngc"
	APIPort        = 7443

	ServiceAPI        = "ngc-api.service"
	ServiceController = "ngc-controller.service"

	MaxConfig              = 1 << 20
	DefaultSessionLifetime = 43200 // 12 hours
	DefaultIdleTimeout     = 1800
	DefaultPodman          = "/usr/bin/podman"
)

// Database drivers.
const (
	DriverMariaDB = "mariadb"
	DriverSQLite  = "sqlite"
)

// ErrConfig reports a configuration that does not work.
var ErrConfig = errors.New("config: invalid configuration")

// Web is /etc/ngc/web.json; the PHP of the web interface reads it too.
type Web struct {
	APIs []string `json:"apis"` // https://host:port, Apache balances over them
	RP   proof.RP `json:"rp"`   // name and origins for passkeys; empty without a name
}

// DB tells an API which database to use.
type DB struct {
	Driver   string    `json:"driver"`
	Nodes    []string  `json:"nodes,omitempty"` // MariaDB: host:port of the Galera nodes
	Name     string    `json:"name,omitempty"`
	User     string    `json:"user,omitempty"`
	Password string    `json:"password,omitempty"`
	Path     string    `json:"path,omitempty"` // SQLite: file
	Key      enc.Bytes `json:"key,omitempty"`  // SQLite: 64 bytes for AES-256-XTS
}

// API is /etc/ngc/api.json. It holds keys, so the file belongs to root with mode 0600.
type API struct {
	Node            string    `json:"node"` // node ID in the trust list
	Listen          string    `json:"listen"`
	Cert            string    `json:"cert"`
	CertKey         string    `json:"certKey"`
	DB              DB        `json:"db"`
	NoiseKey        enc.Bytes `json:"noiseKey"`   // X25519 private key for the tunnel
	SignKey         enc.Bytes `json:"signKey"`    // Ed25519 private key for API certificates
	SecretKey       enc.Bytes `json:"secretKey"`  // AES-256 key for secrets in the database, shared by all APIs
	ReleaseKey      enc.Bytes `json:"releaseKey"` // public key of NGC releases
	StoreKey        enc.Bytes `json:"storeKey"`   // public key of the global store
	StoreURL        string    `json:"storeUrl"`
	SessionLifetime int64     `json:"sessionLifetime,omitempty"`
	IdleTimeout     int64     `json:"idleTimeout,omitempty"`
}

// APIRef is an API a controller dials.
type APIRef struct {
	URL string    `json:"url"`
	Key enc.Bytes `json:"key"` // its Noise key
}

// Controller is /etc/ngc/controller.json, also root and 0600.
type Controller struct {
	Node       string       `json:"node,omitempty"` // empty until an admin put it in the trust list
	Name       string       `json:"name"`
	NoiseKey   enc.Bytes    `json:"noiseKey"`
	APIs       []APIRef     `json:"apis"` // from the pairing code; later from the trust list
	Anchor     trust.Anchor `json:"anchor"`
	DB         string       `json:"db"`
	DBKey      enc.Bytes    `json:"dbKey"` // 64 bytes for AES-256-XTS
	ReleaseKey enc.Bytes    `json:"releaseKey"`
	Parts      []string     `json:"parts"` // tunnel.PartWeb, PartAPI, PartDB, PartServices
	Podman     string       `json:"podman,omitempty"`
	WebConfig  string       `json:"webConfig,omitempty"` // web server: path of web.json for the balancer
}

func load(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := enc.DecodeStrict(b, MaxConfig, v); err != nil {
		return ErrConfig
	}
	return nil
}

// LoadWeb reads web.json.
func LoadWeb(path string) (*Web, error) {
	var w Web
	if err := load(path, &w); err != nil {
		return nil, err
	}
	return &w, w.Validate()
}

// LoadAPI reads api.json and fills in the defaults.
func LoadAPI(path string) (*API, error) {
	var a API
	if err := load(path, &a); err != nil {
		return nil, err
	}
	if a.SessionLifetime == 0 {
		a.SessionLifetime = DefaultSessionLifetime
	}
	if a.IdleTimeout == 0 {
		a.IdleTimeout = DefaultIdleTimeout
	}
	return &a, a.Validate()
}

// LoadController reads controller.json and fills in the defaults.
func LoadController(path string) (*Controller, error) {
	var c Controller
	if err := load(path, &c); err != nil {
		return nil, err
	}
	if c.Podman == "" {
		c.Podman = DefaultPodman
	}
	return &c, c.Validate()
}

// SaveController writes controller.json; the controller uses it after
// pairing to remember its node ID.
func SaveController(path string, c *Controller) error {
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

// LoadAnchor reads trust-anchor.json.
func LoadAnchor(path string) (trust.Anchor, error) {
	var a trust.Anchor
	if err := load(path, &a); err != nil {
		return trust.Anchor{}, err
	}
	if a.Seq == 0 || len(a.Hash) != 32 {
		return trust.Anchor{}, ErrConfig
	}
	return a, nil
}

// SaveAnchor writes trust-anchor.json; the PHP of the web interface sends it
// to the browser, so it is readable for everyone on that server.
func SaveAnchor(path string, a trust.Anchor) error {
	if a.Seq == 0 || len(a.Hash) != 32 {
		return ErrConfig
	}
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Validate checks the web configuration.
func (w *Web) Validate() error {
	if len(w.APIs) == 0 || len(w.APIs) > 16 {
		return ErrConfig
	}
	for _, u := range w.APIs {
		if !strings.HasPrefix(u, "https://") || len(u) > 256 {
			return ErrConfig
		}
	}
	for _, o := range w.RP.Origins {
		if !strings.HasPrefix(o, "https://") {
			return ErrConfig
		}
	}
	return nil
}

// Validate checks the API configuration.
func (a *API) Validate() error {
	if !wire.ValidName(a.Node) || a.Listen == "" || a.Cert == "" || a.CertKey == "" ||
		len(a.NoiseKey) != 32 || len(a.SignKey) != 32 || len(a.SecretKey) != 32 ||
		len(a.ReleaseKey) != 32 || len(a.StoreKey) != 32 || !strings.HasPrefix(a.StoreURL, "https://") ||
		a.SessionLifetime < 300 || a.IdleTimeout < 60 || a.IdleTimeout > a.SessionLifetime {
		return ErrConfig
	}
	return a.DB.Validate()
}

// Validate checks the database part.
func (d *DB) Validate() error {
	switch d.Driver {
	case DriverMariaDB:
		if len(d.Nodes) == 0 || len(d.Nodes) > 16 || d.Name == "" || d.User == "" || d.Path != "" || len(d.Key) != 0 {
			return ErrConfig
		}
		for _, n := range d.Nodes {
			if !strings.Contains(n, ":") || len(n) > 256 {
				return ErrConfig
			}
		}
	case DriverSQLite:
		if !strings.HasPrefix(d.Path, "/") || len(d.Key) != 64 || len(d.Nodes) != 0 {
			return ErrConfig
		}
	default:
		return ErrConfig
	}
	return nil
}

// Validate checks the controller configuration.
func (c *Controller) Validate() error {
	if (c.Node != "" && !wire.ValidName(c.Node)) || !wire.ValidName(c.Name) || len(c.NoiseKey) != 32 ||
		len(c.APIs) == 0 || len(c.APIs) > 16 || c.Anchor.Seq == 0 || len(c.Anchor.Hash) != 32 ||
		!strings.HasPrefix(c.DB, "/") || len(c.DBKey) != 64 || len(c.ReleaseKey) != 32 ||
		len(c.Parts) == 0 || !strings.HasPrefix(c.Podman, "/") {
		return ErrConfig
	}
	for _, a := range c.APIs {
		if !strings.HasPrefix(a.URL, "wss://") || len(a.URL) > 256 || len(a.Key) != 32 {
			return ErrConfig
		}
	}
	for i, p := range c.Parts {
		if !slices.Contains([]string{tunnel.PartWeb, tunnel.PartAPI, tunnel.PartDB, tunnel.PartServices}, p) ||
			slices.Contains(c.Parts[:i], p) {
			return ErrConfig
		}
	}
	if slices.Contains(c.Parts, tunnel.PartWeb) && c.WebConfig == "" {
		return ErrConfig
	}
	return nil
}
```

- [ ] **Step 4: Tests laufen lassen**

Run:

```bash
go test ./contracts/config/
go test -run=XXX -fuzz=FuzzLoad -fuzztime=30s ./contracts/config/
```

Expected: `ok`; Fuzzing ohne Fund. Schlägt `TestExamples` an den Schlüsseln fehl, die Beispieldateien
mit `enc.B64` neu erzeugen (32 beziehungsweise 64 gleiche Bytes) statt die Prüfung zu lockern.

- [ ] **Step 5: Commit**

```bash
git add contracts/config contracts/testdata/config
git commit -m "Add configuration files, paths and ports" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---


### Task 19: Vertragsübersicht und ulh-Konventionen (`contracts/README.md`, `contracts/ulh.md`)

Zwei Dokumente: eines für alle, die einen Vertrag benutzen, eines für die Installation.

**Files:**
- Create: `contracts/README.md`, `contracts/ulh.md`

**Interfaces:**
- Consumes: alle Pakete aus den Tasks 3 bis 18.
- Produces: die verbindliche Zuordnung Vertrag ↔ Paket ↔ Testdaten und die ulh-Konventionen, auf die
  sich die vier Teile beim Schreiben ihrer Skripte stützen.

- [ ] **Step 1: Vertragsübersicht schreiben**

`contracts/README.md`:

````markdown
# Verträge

Hier stehen die Schnittstellen zwischen Webinterface, API, Controller und Datenbank. Sie gehören
dem Koordinator. Wer einen Vertrag ändern will, fragt ihn; alle vier Teile erfahren jede Änderung
sofort. Dieses Verzeichnis steht unter MIT (siehe `LICENSE`), NGC selbst unter AGPL-3.0.

| Vertrag | Inhalt | Pakete | Testdaten |
|---|---|---|---|
| V1 | Webinterface ↔ API | `session`, `wire`, `noisep`, `proof`, `core` | `session.json`, `wire.json`, `noise.json`, `proof.json`, `proof_kdf.json`, `core.json` |
| V2 | API ↔ Controller | `tunnel`, `wire`, `noisep`, `task`, `core` | `tunnel.json`, `task.json` |
| V3 | Controller ↔ Dienst im Container | `service` | `service/echo.sh` |
| V4 | API und Controller ↔ Datenbank | `dbapi`, `dbapi/memstore`, `dbapi/dbtest` | – (Konformitätstests) |
| V5 | App-Paket, Katalog, Lizenz, Updates | `store` | `store.json`, `pack.json` |
| V6 | Konfiguration, Pfade, ulh | `config`, `ulh.md` | `config/*.json` |
| V7 | Vertrauensliste, Ende zu Ende | `trust`, `trust/trusttest`, `noisep`, `proof` | `trust.json` |

## Regeln

- **Signaturen decken immer die genauen Bytes** eines Dokuments ab (`sign.Envelope`, `trust.Signed`).
  Nie neu erzeugtes JSON prüfen, sondern die empfangene Nutzlast.
- **Jede Signatur hat einen Zweck** (`sign.Purpose…`). Eine Unterschrift für eine Sache gilt nie für
  eine andere.
- **Jede Eingabe von außen** läuft durch die `Parse…`- oder `Verify…`-Funktion des Vertrags, nie
  direkt durch `json.Unmarshal`.
- **Grenzen gelten immer**, auch wenn die Gegenstelle zu NGC gehört.
- **Testdaten**: `contracts/testdata/` ist die gemeinsame Wahrheit. Go-Tests erzeugen sie mit
  `go test ./contracts/<paket>/ -run TestVectors -update`; das Webinterface prüft sich in
  `tests/browser/` dagegen. Eine geänderte Datei ist eine Vertragsänderung.
- **Vorige Fassung verstehen**: Beim Update läuft kurz die vorige Version mit (`tunnel.Contract`).
  Felder kommen dazu, alte verschwinden erst eine Version später.
- **Sprache**: Code und Kommentare Englisch, Meldungen für Benutzer Deutsch (`wire.Code.Message`).

## Prüfen

```bash
go test ./contracts/...
scripts/check.sh
```
````

- [ ] **Step 2: ulh-Konventionen schreiben**

`contracts/ulh.md`:

```markdown
# Installation über ulh

NGC wird mit [ulh](https://github.com/sorglos-it/ulh) installiert, konfiguriert und deinstalliert.
Die Skripte liegen im NGC-Repo unter `ulh/`, solange NGC privat ist; ulh bindet das Repo als eigene
Quelle ein. Jedes Teil schreibt die Skripte für seinen Server, der Koordinator hält diese
Konventionen fest.

## Skripte

| Skript | Server | Was es tut |
|---|---|---|
| `ngc-db` | DB 1 bis 3 | MariaDB mit Galera und TLS einrichten, Verschlüsselung einschalten, Konten `ngc` (lesen und schreiben) und `ngc_migrate` (Schema) anlegen |
| `ngc-api` | API 1 und 2 | `ngc-api` und `ngc-store` nach `/usr/lib/ngc/`, `api.json` schreiben, Schlüssel erzeugen oder von einer laufenden API übernehmen, `ngc-api.service` starten, Einmal-Code anzeigen |
| `ngc-web` | Web | Apache und PHP einrichten, `web/` nach `/var/www/ngc/`, Zertifikat (Let's Encrypt, selbst signiert oder Import), `web.json` schreiben, Lastverteilung vorbereiten |
| `ngc-controller` | alle | `ngc-controller` nach `/usr/lib/ngc/`, Schlüssel erzeugen, mit dem Kopplungscode koppeln, `controller.json` schreiben, `ngc-controller.service` starten, Podman installieren, wenn der Server Dienste trägt |
| `ngc-all` | Pi | alles auf einer Maschine mit SQLite statt MariaDB, koppelt den Controller selbst |

Jedes Skript kann `install`, `configure` und `uninstall`. Ein zweiter Lauf von `install` ändert
nichts, was schon stimmt.

## answer.yaml

Damit die Skripte ohne Rückfragen laufen. Schlüssel je Skript:

- `ngc-db`: `nodes` (Liste der Knoten), `bootstrap` (erster Knoten: ja/nein), `password_ngc`,
  `password_migrate`
- `ngc-api`: `node` (z. B. `api-1`), `db_driver`, `db_nodes`, `db_name`, `db_user`, `db_password`,
  `store_url`, `join_code` (leer beim ersten API-Server, sonst der Code einer laufenden API)
- `ngc-web`: `hostname`, `cert_mode` (`letsencrypt`, `selfsigned`, `import`), `cert_file`,
  `key_file`, `apis` (Liste der API-Adressen)
- `ngc-controller`: `name`, `pair_code`, `parts` (`web`, `api`, `db`, `services`)
- `ngc-all`: `hostname`, `cert_mode`, `admin_name`

## Regeln

- Programmdateien lädt ein Skript nur herunter, wenn die Signatur mit dem Release-Schlüssel stimmt
  (`store.VerifyRelease`, Prüfsumme je Datei).
- Schlüssel erzeugt immer der Server selbst; nichts Privates geht über die Leitung, außer beim
  Koppeln weiterer API-Server, und dort verschlüsselt und nur mit Unterschrift eines Admins.
- Konfigurationsdateien mit Schlüsseln gehören root und haben Modus 0600 (`config`).
- `uninstall` fragt, was mit den Daten passiert: löschen oder aufheben. Aufgehobene Daten bleiben
  unter `/var/lib/ngc/` liegen, damit eine Neuinstallation sie wiederfindet.
- Am Ende zeigt `ngc-api` beziehungsweise `ngc-all` den Einmal-Code für das erste Admin-Konto an
  (`session.ValidSetupCode`), `ngc-controller` seinen Fingerabdruck (`tunnel.Fingerprint`).
- Nach der Installation spielt der Controller die Updates ein (4.8); ulh bleibt der Notweg.
```

- [ ] **Step 3: Offene Frage für Thomas notieren**

In `docs/STAND.md` unter „Offene Fragen" ergänzen (die Entscheidung fällt in Task 22):

```markdown
- **ulh und Unterordner:** Eigene ulh-Quellen brauchen `config.yaml` im Wurzelverzeichnis des Repos
  und einen Ordner `scripts/`. NGC hätte seine Skripte gern unter `ulh/`. Zwei Wege: ulh um eine
  Option `subdir` erweitern (einmal Arbeit in ulh, alles bleibt in einem Repo) oder ein zweites,
  privates Repo `sorglos-it/nextgencloud-ulh`, das nur die Skripte enthält. Empfehlung: `subdir`
  in ulh, weil Skripte und Code sonst auseinanderlaufen.
```

- [ ] **Step 4: Commit**

```bash
git add contracts/README.md contracts/ulh.md docs/STAND.md
git commit -m "Add the contract overview and the ulh conventions" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---


### Task 20: Anweisungen für die fünf Sitzungen (`CLAUDE.md`)

Jede der vier Sitzungen liest beim Start die `CLAUDE.md` im Wurzelverzeichnis und die in ihrem
Ordner. Beide zusammen sagen: Aufgabe, eigene Ordner, geltende Verträge, Sicherheitsregeln, Tests,
wann der Teil fertig ist, was tabu ist.

**Files:**
- Create: `CLAUDE.md`, `web/CLAUDE.md`, `api/CLAUDE.md`, `controller/CLAUDE.md`, `db/CLAUDE.md`

**Interfaces:**
- Consumes: Entwurf 4.6 und 4.7, `contracts/README.md`, `contracts/ulh.md`.
- Produces: die verbindlichen Anweisungen je Sitzung.

- [ ] **Step 1: Gemeinsame Anweisungen schreiben**

`CLAUDE.md`:

```markdown
# NGC – Anweisungen für alle Sitzungen

NGC (NextGenCloud) ist eine offene Alternative zu Synologys DSM: ein Arbeitsplatz im Browser mit
App-Store. Entwurf: `docs/superpowers/specs/2026-09-19-ngc-kern-design.md`. Stand und nächster
Schritt: `docs/STAND.md`. Verträge: `contracts/README.md`.

## Wer macht was

| Teil | Ordner | Sitzung auf Server |
|---|---|---|
| Webinterface | `web/`, `ulh/web/` | Web |
| API | `api/`, `ulh/api/` | API 1 |
| Controller | `controller/`, `ulh/controller/` | Dienste |
| Datenbank | `db/`, `ulh/db/` | DB 1 |
| Verträge, `contracts/`, Zusammenführen | – | Koordinator (Thomas' Rechner) |

Fremde Ordner sind tabu. `contracts/` ändert nur der Koordinator: Wer eine Änderung braucht, fragt
ihn und wartet auf die neue Fassung. Fragen an einen Nachbarn gehen direkt an dessen Sitzung.

## Regeln für sicheres Programmieren (Entwurf 4.6, verbindlich)

- SQL nur mit Platzhaltern, Tabellen- und Spaltennamen nur aus festen Listen.
- Programme ohne Shell starten, immer mit Argumentliste, nie `sh -c`.
- Jede Eingabe von außen prüfen: Typ, Länge, erlaubte Werte; unbekannte JSON-Felder ablehnen. Immer
  über die `Parse…`- oder `Verify…`-Funktion des Vertrags.
- Nichts, was von außen kommt, als HTML einfügen (`textContent`, nie `innerHTML`).
- Pakete sicher entpacken: keine absoluten Pfade, kein `..`, keine Links, feste Größen (`store.Walk`).
- Überall Obergrenzen: Nachrichtengröße, Kanäle je Sitzung, Anmeldeversuche.
- Kryptografie nur aus der Standardbibliothek oder aus `vendor/`, nichts selbst gebaut.
- Geheimnisse nie ins Log, auch nicht gekürzt.

## Technik

- Go 1.27 (`go.mod`: `go 1.27`, `toolchain go1.27.1`), Bau für linux/amd64 und linux/arm64 mit
  `CGO_ENABLED=0`. Ziel sind Debian und Ubuntu. Windows wird nicht umgesetzt.
- Keine Laufzeitabhängigkeiten: Fremdcode nur per `go mod vendor`, mit Zeile in `THIRD-PARTY.md`,
  Lizenz vereinbar mit der AGPL-3.0. Neue Module vorher beim Koordinator anfragen.
- Webinterface ohne Framework und ohne Build-Schritt; npm nur in `tests/browser/`.
- Sprache: Code, Bezeichner, Kommentare und Commit-Nachrichten Englisch. Dokumente, Oberfläche und
  Meldungen für Benutzer Deutsch.
- Kommentare erklären, warum etwas so ist, nicht was die Zeile tut.

## Tests

- `go test ./...` muss grün sein, `scripts/check.sh` vor jedem Commit.
- Jeder Parser bekommt einen Fuzz-Test (`go test -fuzz`), jede Kryptografie einen Test gegen die
  Vektoren in `contracts/testdata/`.
- Fremdcode prüfen: `govulncheck ./...`.
- Keine Tests löschen oder überspringen, um grün zu werden.

## Git

- Ein Branch je Teil (`web`, `api`, `controller`, `db`), Pull Request nach `main`. Der Koordinator
  prüft jeden Pull Request auf Vertrag und Sicherheit und führt ihn zusammen.
- Kleine Commits, englische Nachricht in der Befehlsform, Trailer
  `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`.
- Nie mit Gewalt schieben, nie fremde Branches ändern.

## Wenn etwas unklar ist

Lieber fragen als raten. Was nicht im Repo steht, gilt nicht: Jede Entscheidung kommt in das
Entscheidungsprotokoll des Entwurfs.
```

- [ ] **Step 2: Anweisungen für das Webinterface schreiben**

`web/CLAUDE.md`:

```markdown
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
```

- [ ] **Step 3: Anweisungen für die API schreiben**

`api/CLAUDE.md`:

```markdown
# API

Deine Ordner: `api/`, `ulh/api/`. Verträge: V1, V2, V4, V5, V6, V7.

## Aufgabe

Der Vermittler und die Firewall zwischen Oberfläche und Servern. Führt nie App-Code aus.

- Anmeldung nach `session`: Challenge, Passwortschlüssel oder Passkey prüfen (`proof`), TOTP,
  Sperre nach Fehlversuchen, Sitzung und Zertifikate, Cookie mit `session.TokenHash`.
- WebSocket nach `wire` und `noisep`, Kanäle mit Freigaben, Grenzen je Sitzung.
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
```

- [ ] **Step 4: Anweisungen für den Controller schreiben**

`controller/CLAUDE.md`:

```markdown
# Server-Controller

Deine Ordner: `controller/`, `ulh/controller/`. Verträge: V2, V3, V4, V5, V6, V7.

## Aufgabe

Der Dienst auf jedem Server. Er redet nur mit der API und läuft als root, deshalb bleibt sein Code
so klein wie möglich.

- Tunnel zu jeder API (`tunnel`), Kopplung mit Code, Wiederaufbau mit wachsenden Pausen.
- Vertrauensliste selbst prüfen (`trust`), Anker aus dem Kopplungscode, Fassungen in SQLite.
- Kanäle: Client-Zertifikat prüfen (`session.VerifyClient`), Ende zu Ende beantworten
  (`noisep.E2EPrologue`), Aufgaben und erneute Bestätigung prüfen (`task.Check`), Wiedereinspielen
  verhindern (`dbapi` `OnceTask`).
- Dienste in Podman-Containern starten (`service`): ein Container je App, eigener Systembenutzer,
  kein Netz, Grenzen für CPU und Speicher, verschlüsselter Datenordner als `/data`, Startaktion nach
  jedem Start, auch ohne API.
- Container nur über eine eigene Schnittstelle ansprechen, damit LXC später danebenpassen kann.
- Apps aktivieren, deaktivieren, deinstallieren (`core`, `store`): Paket prüfen, Kauf-Apps
  entschlüsseln, Image laden, Daten löschen oder aufheben.
- Updates einspielen und bei Fehlschlag zurücknehmen; ohne Unterschrift nur mit Dauerfreigabe
  (`store.Approved`) und nie eine ältere Version.
- Auf dem Webserver: Lastverteilung in Apache aus `web.json` einrichten und den Anker schreiben.
- Der Dienst „virtuelles Terminal" als Beispiel-Dienst.

## Tests

- Gegen die Vektoren in `contracts/testdata/`, den Beispiel-Dienst `service/echo.sh` und
  `dbapi/dbtest` für die lokale SQLite.
- Neustart: Container und Startaktionen kommen ohne API wieder hoch.
- Angriffe: gefälschte Aufgabe, fehlende Bestätigung, fremder Controller-Schlüssel, Ausbruch aus
  dem Container.

## Fertig, wenn

Eine App lässt sich mit Unterschrift aktivieren, das Terminal liefert Ausgaben, der Server läuft
nach einem Neustart ohne API weiter, ein kaputtes Update wird selbst zurückgenommen.
```

- [ ] **Step 5: Anweisungen für die Datenbank schreiben**

`db/CLAUDE.md`:

```markdown
# Datenbank

Deine Ordner: `db/`, `ulh/db/`. Verträge: V4, V6.

## Aufgabe

- `dbapi.Store` für MariaDB und `dbapi.Local` für SQLite umsetzen. Beide müssen `dbtest.Run`
  bestehen, und zwar mit denselben Tests.
- MariaDB-Galera-Cluster mit mindestens drei Knoten einrichten: TLS zwischen den Knoten,
  eingebaute Verschlüsselung (AES-256), Konto `ngc` nur mit Lesen und Schreiben, Konto
  `ngc_migrate` für das Schema.
- SQLite verschlüsselt (AES-256-XTS, Kandidat `github.com/ncruces/go-sqlite3` mit `vfs/xts`) für den
  Controller und für „alles auf einem Pi".
- Nummerierte Migrationen, die auch mit der vorigen API-Version laufen. Sie gehören zur
  API-Version und werden vom Controller vor der neuen API eingespielt.
- Lastverteilung über die Knoten, Ausweichen bei Ausfall, Wiederholung, wenn Galera eine
  Transaktion wegen eines gleichzeitigen Schreibzugriffs abweist.
- Portables SQL: nur was MariaDB, SQLite, MySQL, MSSQL und Oracle verstehen. IDs erzeugt die
  Anwendung, Zeitstempel sind Zahlen, keine datenbankeigenen Funktionen.

## Tests

- `dbtest.Run` gegen MariaDB (Cluster aus drei Knoten) und gegen SQLite.
- Migrationen vorwärts und mit der vorigen API-Version.
- Ausfall: ein Knoten wird im laufenden Betrieb abgeschaltet, die Tests laufen weiter.
- Kein Test darf ein Schema von Hand anlegen: immer über die Migrationen.

## Fertig, wenn

Beide Umsetzungen bestehen dieselben Tests, der Cluster verkraftet den Ausfall eines Knotens, und
eine Migration läuft, während noch die vorige API-Version arbeitet.
```

- [ ] **Step 6: Commit**

```bash
git add CLAUDE.md web/CLAUDE.md api/CLAUDE.md controller/CLAUDE.md db/CLAUDE.md
git commit -m "Add the instructions for the coordinator and the four parts" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---


### Task 21: Grundgerüst der Programme

Damit jeder Teil einen Anfang hat, der baut, läuft und `-version` beantwortet. Mehr nicht: Die
Sitzungen füllen das später.

**Files:**
- Create: `api/cmd/ngc-api/main.go`, `api/cmd/ngc-store/main.go`, `controller/cmd/ngc-controller/main.go`,
  `api/doc.go`, `controller/doc.go`, `db/doc.go`, `web/README.md`, `ulh/README.md`

**Interfaces:**
- Consumes: `config.LoadAPI`, `config.LoadController`, `config.APIFile`, `config.ControllerFile`, `tunnel.Contract`.
- Produces: die Programme `ngc-api`, `ngc-store` und `ngc-controller` mit `-version` und `-config`;
  `main.Version` wird beim Bau gesetzt (`-ldflags "-X main.Version=…"`).

- [ ] **Step 1: API-Programm schreiben**

`api/cmd/ngc-api/main.go`:

```go
// Command ngc-api is the API of NGC: sessions, rights, forwarding.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/sorglos-it/nextgencloud/contracts/config"
	"github.com/sorglos-it/nextgencloud/contracts/tunnel"
)

// Version is set when building: -ldflags "-X main.Version=1.0.0".
var Version = "0.0.0-dev"

func main() {
	version := flag.Bool("version", false, "print the version and exit")
	path := flag.String("config", config.APIFile, "configuration file")
	flag.Parse()
	if *version {
		fmt.Printf("ngc-api %s (contract %d)\n", Version, tunnel.Contract)
		return
	}
	cfg, err := config.LoadAPI(*path)
	if err != nil {
		slog.Error("cannot read the configuration", "file", *path, "error", err)
		os.Exit(1)
	}
	slog.Info("ngc-api starting", "version", Version, "node", cfg.Node, "listen", cfg.Listen)
	slog.Error("the API is not built yet, see api/CLAUDE.md")
	os.Exit(1)
}
```

`api/cmd/ngc-store/main.go`:

```go
// Command ngc-store packs apps, encrypts paid apps and signs catalogs and
// licenses. With it anyone can run their own store.
package main

import (
	"flag"
	"fmt"
	"os"
)

// Version is set when building.
var Version = "0.0.0-dev"

const usage = `ngc-store packs apps and signs stores.

Usage:
  ngc-store pack <directory> <package.ngc>       build an app package
  ngc-store encrypt <package.ngc> <key file>     encrypt a paid app
  ngc-store catalog <directory> <key file>       write and sign a catalog
  ngc-store license <app> <installation> <key>   issue a license for a test store
`

func main() {
	version := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()
	if *version {
		fmt.Printf("ngc-store %s\n", Version)
		return
	}
	fmt.Fprint(os.Stderr, usage)
	fmt.Fprintln(os.Stderr, "\nngc-store is not built yet, see api/CLAUDE.md")
	os.Exit(1)
}
```

- [ ] **Step 2: Controller-Programm schreiben**

`controller/cmd/ngc-controller/main.go`:

```go
// Command ngc-controller runs on every server: tunnel to the APIs, services
// in containers, updates.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/sorglos-it/nextgencloud/contracts/config"
	"github.com/sorglos-it/nextgencloud/contracts/tunnel"
)

// Version is set when building.
var Version = "0.0.0-dev"

func main() {
	version := flag.Bool("version", false, "print the version and exit")
	path := flag.String("config", config.ControllerFile, "configuration file")
	flag.Parse()
	if *version {
		fmt.Printf("ngc-controller %s (contract %d)\n", Version, tunnel.Contract)
		return
	}
	cfg, err := config.LoadController(*path)
	if err != nil {
		slog.Error("cannot read the configuration", "file", *path, "error", err)
		os.Exit(1)
	}
	slog.Info("ngc-controller starting", "version", Version, "name", cfg.Name, "parts", cfg.Parts)
	slog.Error("the controller is not built yet, see controller/CLAUDE.md")
	os.Exit(1)
}
```

- [ ] **Step 3: Platzhalter für die übrigen Teile**

`api/doc.go`:

```go
// Package api holds the API of NGC. The programs are in api/cmd/; see api/CLAUDE.md.
package api
```

`controller/doc.go`:

```go
// Package controller holds the server controller of NGC. The program is in
// controller/cmd/ngc-controller; see controller/CLAUDE.md.
package controller
```

`db/doc.go`:

```go
// Package db holds the database of NGC: the implementations of dbapi for
// MariaDB and SQLite, and the migrations; see db/CLAUDE.md.
package db
```

`web/README.md`:

````markdown
# Webinterface

HTML, CSS, JavaScript und PHP, ohne Framework und ohne Build-Schritt. Anweisungen: `web/CLAUDE.md`.
Vorlage für den Desktop: `prototype/index.html`. Verträge: `contracts/README.md`,
Testdaten: `contracts/testdata/`.

Geplanter Aufbau:

```
web/index.php          liefert die Seite aus, liest /etc/ngc/web.json und trust-anchor.json
web/shell/             Desktop: Fenster, Taskleiste, Startmenü, Anmeldung
web/shell/ngc/         Verträge in JavaScript: Noise, Signaturen, Vertrauensliste, Frames
web/sdk/ngc.js         Bibliothek für Apps (MIT)
web/apps/              Oberflächen von App-Store, Benutzer & Rechte, Rechner, Terminal
```
````

`ulh/README.md`:

```markdown
# Installation über ulh

Hier liegen die ulh-Skripte je Teil (`web/`, `api/`, `controller/`, `db/`) und für „alles auf einem
Pi". Verbindlich sind die Konventionen in `contracts/ulh.md`. Jedes Teil schreibt die Skripte für
seinen Server selbst.
```

- [ ] **Step 4: Bauen und prüfen**

Run:

```bash
go build ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./...
go run ./api/cmd/ngc-api -version
go run ./controller/cmd/ngc-controller -version
go run ./api/cmd/ngc-store -version
```

Expected: die Bauläufe ohne Ausgabe, die drei Aufrufe geben Name, Version und Vertragsnummer aus.

- [ ] **Step 5: Commit**

```bash
git add api controller db web ulh
git commit -m "Add the program skeletons for API, store tool and controller" -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---


### Task 22: Abnahme

Vor dem Zusammenführen: alles prüfen, die beim Schreiben entstandenen Klärungen festhalten, Thomas
die zwei offenen Punkte vorlegen.

**Files:**
- Modify: `docs/superpowers/specs/2026-09-19-ngc-kern-design.md` (Entscheidungsprotokoll),
  `docs/STAND.md`

**Interfaces:**
- Consumes: alle Tasks.
- Produces: den Stand „Verträge stehen", den Pull Request und das Etikett `vertraege-v1`.

- [ ] **Step 1: Ganze Prüfung laufen lassen**

```bash
scripts/check.sh
go test ./... -count=1
```

Expected: alles grün, Bau für linux/amd64 und linux/arm64 ohne Fehler.

- [ ] **Step 2: Jeden Parser fuzzen**

```bash
for pkg in enc proof wire trust session task tunnel store core config; do
  go test -run=XXX -fuzz=Fuzz -fuzztime=60s ./contracts/$pkg/ || echo "FEHLER in $pkg"
done
```

Expected: kein Fund. Hat ein Paket mehrere Fuzz-Tests, beschwert sich Go („will not fuzz, -fuzz
matches more than one fuzz test"); dann die Tests einzeln aufrufen, wie in den jeweiligen Tasks.
Ein Fund ist ein Fehler im Vertrag: beheben, Testfall aus `testdata/fuzz/` behalten, Commit.

- [ ] **Step 3: Fremdcode prüfen**

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

Expected: keine bekannten Lücken. Meldet es eine, das Modul aktualisieren (`go get …@…`,
`go mod vendor`, Zeile in `THIRD-PARTY.md`) und erneut prüfen.

- [ ] **Step 4: Klärungen ins Entscheidungsprotokoll**

Im Entwurf (`docs/superpowers/specs/2026-09-19-ngc-kern-design.md`), Kapitel 6, ergänzen:

```markdown
| 19.09.2026 | Beim Schreiben von Plan 0 geklärt: Der Anker der Vertrauensliste ist Nummer und SHA-256 einer Fassung (damit auch der Admin-Schlüssel), nicht der Schlüssel allein. |
| 19.09.2026 | Beim Schreiben von Plan 0 geklärt: Ein Container je App, also ein Image je App und Architektur (`services/<arch>.tar`); die Dienste sind Programme darin. |
| 19.09.2026 | Beim Schreiben von Plan 0 geklärt: Kern-Apps (`ngc.desktop`, `ngc.users`, `ngc.store`) bedient die API selbst; ihre Kanäle haben keinen eigenen Ende-zu-Ende-Schlüsselaustausch, weil die Sitzung zur API schon so verschlüsselt ist. |
| 19.09.2026 | Beim Schreiben von Plan 0 geklärt: Update-Dateien kommen über Kanäle (`stage`, `install`) zum Controller, nicht über Steuernachrichten; über den Steuerkanal läuft nur der Fortschritt. |
| 19.09.2026 | Beim Schreiben von Plan 0 geklärt: Die Wiederherstellung (Codes, Zurücksetzen durch einen Admin, Notfallbefehl) ist noch kein Vertrag; sie kommt als Vertragsänderung vor Plan 1 des Webinterface. |
```

- [ ] **Step 5: Thomas die zwei offenen Punkte vorlegen**

1. **ulh und Unterordner** (Task 19, Step 3): `subdir`-Option in ulh oder zweites Repo
   `sorglos-it/nextgencloud-ulh`? Empfehlung: `subdir` in ulh.
2. **Verträge freigeben:** Kurz zeigen, was `contracts/README.md` auflistet, und die Freigabe
   einholen. Erst danach fangen die vier Sitzungen an.

Die Antwort kommt ins Entscheidungsprotokoll.

- [ ] **Step 6: Stand fortschreiben**

`docs/STAND.md` aktualisieren: Verträge stehen, Testdaten liegen in `contracts/testdata/`, nächster
Schritt sind die vier Pläne je Teil, Voraussetzungen (sieben Server, Pi, zwei Sticks), wie die
Sitzungen starten (jede liest `CLAUDE.md` und die ihres Ordners).

- [ ] **Step 7: Pull Request und Etikett**

```bash
git push -u origin plan-0-vertraege
gh pr create --title "Verträge, Anweisungen und Grundgerüst" --body "$(cat <<'EOF'
## Was drin ist

Die sieben Verträge als Go-Pakete mit Testvektoren, je eine CLAUDE.md für die vier Teile,
das Grundgerüst des Repos und die ulh-Konventionen.

## Geprüft

- scripts/check.sh (gofmt, vet, Tests, Bau für amd64 und arm64)
- Fuzzing für jeden Parser, govulncheck ohne Fund

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Nach Thomas' Freigabe zusammenführen und das Etikett setzen:

```bash
gh pr merge --squash --delete-branch
git checkout main && git pull
git tag -a vertraege-v1 -m "Contracts V1 to V7, first version"
git push origin vertraege-v1
```

- [ ] **Step 8: Gedächtnis auffrischen**

In `C:\Users\User\.claude\projects\Z--GitHub\memory\nextgencloud-projekt.md` den Stand in „How to
apply" auf „Verträge stehen (Etikett `vertraege-v1`), als Nächstes die vier Pläne je Teil" ändern.

---

## Danach

Plan 0 ist die Grundlage. Danach in dieser Reihenfolge:

1. **Pläne 1 bis 4, je ein Teil** (`docs/superpowers/plans/`), geschrieben vom Koordinator, jeder
   gegen die Verträge und die Testdaten: Webinterface, API, Controller, Datenbank. Die vier
   Sitzungen auf Web, API 1, Dienste und DB 1 arbeiten sie parallel ab, jede auf ihrem Branch, und
   liefern per Pull Request.
2. **Plan 5, Zusammenspiel:** die vier Teile auf den sieben Servern, verteilter Gesamttest nach dem
   Erfolgskriterium (4.1), Ausfall einer API und eines Datenbankknotens, Updates mit Rücknahme.
3. **Plan 6, Pi:** alles auf einer Maschine mit SQLite, zweiter Gesamttest.
4. **Plan 7, Sicherheitsdurchgang:** Fuzzing über alles, die Angriffe aus 4.6, Review nach OWASP
   ASVS, Test mit echtem YubiKey 5 NFC und Nitrokey 3 NFC.

Danach ist der Kern fertig, und Teilprojekt 2 (NGC-Zertifizierungsstelle) beginnt.
