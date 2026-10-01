# NGC Plan 3: Server-Controller – Umsetzungsplan

> **Für ausführende Agenten:** PFLICHT-SKILL: `superpowers:subagent-driven-development` (empfohlen)
> oder `superpowers:executing-plans`, Aufgabe für Aufgabe. Schritte nutzen Kästchen (`- [ ]`).

**Ziel:** `ngc-controller` bauen – den Dienst, der auf jedem der sieben Server läuft, einen Tunnel
zu jeder API hält, Root-Aufgaben nur mit geprüfter Unterschriftenkette ausführt, die Dienste der
Apps in Podman-Containern startet, Updates einspielt und zurücknimmt und den Server auch ohne API
weiterlaufen lässt.

**Architektur:** Ein Programm (`main.go`, dünn) über dreizehn Go-Paketen unter `classes/`. Nach
außen gibt es genau zwei Wege hinein: den Tunnel zur API (WebSocket + Noise_KK, der Controller
baut ihn auf) und die Kopplung per Code (einmalig, HTTPS mit gepinntem TLS). Alles, was von dort
kommt, läuft durch die `Parse…`- und `Verify…`-Funktionen der Verträge, bevor es irgendetwas
auslöst. Der Controller läuft als root; deshalb ist jede Wirkung nach außen in wenige, eng
geprüfte Stellen gekapselt: `classes/runtime` startet Programme ausschließlich mit Argumentliste,
`classes/apache` schreibt genau eine Vorlage, `classes/update` fasst nur Dateien an, die eine
geprüfte Signatur deckt. Der lokale Zustand liegt in einer verschlüsselten SQLite (`dbapi.Local`),
damit der Server nach einem Neustart ohne API weiterläuft.

**Tech Stack:** Go 1.27 (Standardbibliothek: `crypto/*`, `archive/tar`, `os/exec`, `syscall`,
`log/slog`), `github.com/coder/websocket` v1.8.15 (Tunnel), `github.com/flynn/noise` v1.1.0 über
`shared/noisep`, die Verträge in `shared/` (V1–V7, Etikett `vertraege-v1`). Zur Laufzeit auf dem
Server: Podman und – nur mit Rolle `web` – Apache, beide aus den Paketquellen.

**Entwurf:** [`docs/superpowers/specs/2026-09-19-ngc-kern-design.md`](../specs/2026-09-19-ngc-kern-design.md)
(4.2, 4.3, 4.5, 4.6, 4.8). **Anweisungen:** [`apps/server-controller/CLAUDE.md`](../../../apps/server-controller/CLAUDE.md).
**Verträge:** [`shared/README.md`](../../../shared/README.md), [`shared/ulh.md`](../../../shared/ulh.md).

---

## Global Constraints

Diese Regeln gelten für jede Aufgabe; sie werden nicht wiederholt.

- **Verträge sind eingefroren** (Etikett `vertraege-v1`, 22.09.2026). `shared/` wird nie geändert.
  Braucht eine Aufgabe eine Änderung, kommt sie in „Offene Entscheidungen für den Koordinator“ und
  geht an den Koordinator; bis zur neuen Fassung wird nicht weitergebaut.
- **Fremde Ordner sind tabu:** geschrieben wird nur in `apps/server-controller/` (Ausnahme:
  `tools/modules.go`, siehe Aufgabe 5). `apps/server-db/`, `apps/server-api/`, `apps/web/`,
  `shared/`, `docs/`, `tools/check.sh` gehören anderen.
- **Go 1.27**, Bau für linux/amd64 und linux/arm64 mit `CGO_ENABLED=0`. Ziel: Debian und Ubuntu.
  Windows wird nicht umgesetzt; Tests, die Linux brauchen, überspringen sich dort selbst
  (`if runtime.GOOS != "linux" { t.Skip(…) }`).
- **Keine Laufzeitabhängigkeiten außerhalb von `vendor/`.** Neue Module gibt es nicht; wer ein
  schon vorhandenes zuerst benutzt, streicht seine Zeile in `tools/modules.go`.
- **`tools/check.sh` muss vor jedem Commit grün sein** (gofmt, `go vet`, `go test ./...`, Bau für
  beide Architekturen). Auf Linux zusätzlich `go test -race ./apps/server-controller/...`.
- **Erst Tests, dann Code (TDD).** Jeder Schritt „Test schreiben“ wird von einem Schritt „Test
  ausführen, er muss fehlschlagen“ gefolgt. Keine Tests löschen oder überspringen, um grün zu
  werden.
- **Jeder Parser bekommt einen Fuzz-Test** (`go test -fuzz`). Die Liste steht in Aufgabe 25.
- **Programme immer mit Argumentliste starten**, nie `sh -c`, nie eine aus Eingaben gebaute
  Zeichenkette. `exec.Command` bekommt den absoluten Pfad und eine Liste.
- **Jede Eingabe von außen** läuft durch die `Parse…`- oder `Verify…`-Funktion ihres Vertrags,
  nie durch `json.Unmarshal`.
- **Geheimnisse nie ins Log**, auch nicht gekürzt: `config.Secret`, `config.SecretText`,
  `NGC_PARAMS`, Inhaltsschlüssel aus `core.Transfer`, Lizenzen, Kopplungs- und Beitrittscodes.
  Logs entstehen nur über `log/slog` mit benannten Feldern, nie mit `%v` über eine ganze
  Konfiguration.
- **TLS nie ungeprüft:** zur API immer `tunnel.PinnedTLS`, zu MariaDB immer
  `config.DB.TLSConfig`. `InsecureSkipVerify` steht nirgends außer in `tunnel.PinnedTLS`.
- **Adressen nur nach der Grammatik in `trust`** (`ValidHost`, `ValidHostPort`, `ValidTunnelURL`,
  `ValidWebURL`, `ValidStoreURL`, `ValidOrigin`). Nichts Ungeprüftes landet in einer fremden
  Konfiguration (Apache, Datenbanktreiber).
- **Grenzen gelten immer**, auch gegenüber der eigenen API: `wire.MaxTunnelChannels`,
  `wire.MaxWindow`, `core.Action.MaxIn`, `wire.MaxCallResult`, `store.MaxDirs`, `store.MaxEntries`.
- **Sprache:** Plan und Dokumente Deutsch; Code, Bezeichner, Kommentare und Commit-Nachrichten
  Englisch; Meldungen für Benutzer Deutsch (`wire.Code.Message`, `core.Notice.Text`).
- **Commits:** klein, englische Befehlsform, Trailer
  `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`. Branch `controller`, Pull Request nach
  `main`. Nie mit Gewalt schieben.
- **Kommentare erklären, warum etwas so ist**, nicht was die Zeile tut.
- Code aus diesem Plan nach dem Einfügen mit `gofmt -w` formatieren; Abweichungen bei Einrückung
  sind kein Planfehler.

---

## Voraussetzungen und Abhängigkeiten von anderen Teilen

| Was | Von wem | Gebraucht ab | Bis dahin |
|---|---|---|---|
| `dbapi.Store` für SQLite (`apps/server-db/classes/sqlite`) | Sitzung Datenbank | Aufgabe 24 | `memstore` in allen Tests |
| Vorlage der Apache-Lastverteilung in `shared/ulh.md` | Plan des Webinterface, eingetragen vom Koordinator | Aufgabe 22 | die Form aus diesem Plan, danach abgleichen |
| Oberfläche der Terminal-App (`ui/`) | Sitzung Webinterface | Aufgabe 23 (Paket) | Dienst und Image allein |
| `ngc-store pack` zum Packen der Terminal-App | Sitzung API | Aufgabe 23 | Paket von Hand mit `tar` in einem Test |
| Datenprotokoll zwischen Terminal-Oberfläche und Terminal-Dienst | mit der Sitzung Webinterface abstimmen | Aufgabe 23 | die Festlegung aus Aufgabe 23 |

Fragen an einen Nachbarn gehen direkt an dessen Sitzung, Fragen zu einem Vertrag an den
Koordinator. Eine Aufgabe, deren Voraussetzung fehlt, wird zurückgestellt, nicht geraten.

---

## Dateien-Übersicht

```
apps/server-controller/
├── CLAUDE.md                     Anweisungen (Aufgabe 23 ergänzt den Abschnitt „Terminal-App bauen“)
├── VERSION                       0.1.0, in Aufgabe 25 auf 1.0.0
├── main.go                       nur: Version einsetzen, node.Main aufrufen, Exit-Code
├── templates/
│   ├── apache-apis.conf.tmpl     Vorlage der Lastverteilung (Form aus shared/ulh.md)
│   └── terminal/
│       ├── Containerfile         Image der Terminal-App
│       └── manifest.json         Manifest der Terminal-App
└── classes/
    ├── node/                     Kommandos, Verdrahtung, Wiederanlauf ohne API, Statusmeldungen
    ├── state/                    lokaler Zustand über dbapi.Local (Apps, Fassungen, Lizenzen, Aufgaben)
    ├── chain/                    Vertrauensliste: aus dem Anker öffnen, fortschreiben, halten
    ├── pairing/                  Kopplung mit dem Kopplungscode
    ├── link/                     Tunnel: WebSocket, Noise, Frames, Kanäle, Freigabe, Steuerkanal
    ├── access/                   OPEN prüfen: Kette bis zum Benutzer, Aufgabe, Regel, Wiedereinspielung
    ├── e2e/                      Ende zu Ende je Kanal
    ├── runtime/                  Container-Schnittstelle, Podman, Systembenutzer, Datenordner
    ├── serve/                    Kanäle verteilen: Kern-Aktionen und App-Kanäle
    ├── install/                  Paket prüfen, aktivieren, deaktivieren, deinstallieren, Daten löschen
    ├── license/                  Lizenzen prüfen, behalten, Zustand durchsetzen
    ├── update/                   bereitlegen, einspielen, zurücknehmen, apt, Migrationen
    ├── apache/                   Rolle web: api-trust.pem, web.json, Lastverteilung, Neuladen
    └── terminal/                 Dienst „virtuelles Terminal“ und sein Image
```

Jedes Paket ist ein eigener Ordner unter `classes/`, wie `CLAUDE.md` im Stamm es für Teile mit mehr
als etwa 20 Dateien verlangt. Tests liegen neben dem Code.

---

## Etappe 1 – Grundlage (Aufgaben 1 bis 4)

### Task 1: Programmgerüst und Kommandos

**Files:**
- Modify: `apps/server-controller/main.go`
- Create: `apps/server-controller/classes/node/main.go`, `apps/server-controller/classes/node/main_test.go`

**Interfaces:**
- Produces: `node.Main(version string, args []string, stdout, stderr io.Writer) int` – der ganze
  Einstieg; `node.Options{Config string, Pair string, Arch string}`. Kommandos: ohne Argument
  laufen (später Aufgabe 18), `-version`, `pair -code <Kopplungscode>`, `terminal`, `hold`,
  `image -arch <arch> -out <datei>`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/server-controller/classes/node/main_test.go`:

```go
package node

import (
	"bytes"
	"strings"
	"testing"
)

func TestMainVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Main("1.2.3", []string{"-version"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := out.String(); !strings.HasPrefix(got, "ngc-controller 1.2.3 (contract ") {
		t.Fatalf("output = %q", got)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", errOut.String())
	}
}

func TestMainUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Main("1.2.3", []string{"frobnicate"}, &out, &errOut); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "frobnicate") {
		t.Fatalf("stderr = %q, want the unknown command named", errOut.String())
	}
}

func TestMainPairNeedsCode(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Main("1.2.3", []string{"pair"}, &out, &errOut); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/node/`
Erwartet: FAIL, „undefined: Main“.

- [ ] **Step 3: `node.Main` schreiben**

`apps/server-controller/classes/node/main.go`:

```go
// Package node wires the controller together: commands, the links to the
// APIs, and what the server keeps doing without an API.
package node

import (
	"flag"
	"fmt"
	"io"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// Main runs one command and returns the exit code: 0 success, 1 failure,
// 2 wrong use. It never calls os.Exit, so tests can drive it.
func Main(version string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ngc-controller", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := fs.String("config", config.ControllerFile, "configuration file")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "ngc-controller %s (contract %d)\n", version, tunnel.Contract)
		return 0
	}
	switch cmd := fs.Arg(0); cmd {
	case "":
		return run(version, *cfgPath, stdout, stderr)
	case "pair":
		return pair(version, *cfgPath, fs.Args()[1:], stdout, stderr)
	case "terminal":
		return runTerminal(fs.Args()[1:], stdout, stderr)
	case "hold":
		return hold()
	case "image":
		return buildImage(version, fs.Args()[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", cmd)
		return 2
	}
}
```

Für diese Aufgabe geben `run`, `pair`, `runTerminal`, `hold` und `buildImage` in derselben Datei
zunächst `notReady(stderr, "<name>")` zurück (Exit 1 mit der deutschen Meldung „Dieser Teil ist
noch nicht gebaut.“); `pair` prüft vorher sein eigenes Flag `-code` und gibt ohne Code 2 zurück.
Die folgenden Aufgaben ersetzen je eine dieser Funktionen.

- [ ] **Step 4: Test ausführen, er muss bestehen**

Ausführen: `go test ./apps/server-controller/classes/node/`
Erwartet: PASS.

- [ ] **Step 5: `main.go` dünn machen**

`apps/server-controller/main.go`:

```go
// Command ngc-controller runs on every server: tunnel to the APIs, services
// in containers, updates.
package main

import (
	"os"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/node"
)

// Version is set when building.
var Version = "0.0.0-dev"

func main() { os.Exit(node.Main(Version, os.Args[1:], os.Stdout, os.Stderr)) }
```

- [ ] **Step 6: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/main.go apps/server-controller/classes/node/
git commit -m "Add the controller command skeleton"
```

---

### Task 2: Lokaler Zustand

**Files:**
- Create: `apps/server-controller/classes/state/state.go`,
  `apps/server-controller/classes/state/state_test.go`

**Interfaces:**
- Consumes: `dbapi.Local` (Aufgabe 24 liefert die SQLite dahinter; hier `memstore.New()`).
- Produces: `state.Store` mit `New(db dbapi.Local) *Store`, `Close() error`,
  `PutVersion(ctx, v trust.Signed, seq uint64) error`, `Versions(ctx, from uint64) ([]trust.Signed, error)`,
  `NewestSeq(ctx) (uint64, error)`, `PutApp(ctx, dbapi.App) error`, `App(ctx, id string) (dbapi.App, error)`,
  `Apps(ctx) ([]dbapi.App, error)`, `DeleteApp(ctx, id string) error`,
  `PutLicense(ctx, app string, env sign.Envelope, issued int64) error`,
  `License(ctx, app string) (sign.Envelope, int64, error)`,
  `PutStaged(ctx, Staged) error`, `Staged(ctx, target string) (Staged, error)`,
  `DeleteStaged(ctx, target string) error`, `PutComponents(ctx, map[string]string) error`,
  `Components(ctx) (map[string]string, error)`, `UseTask(ctx, id []byte, expires, now int64) error`,
  `Purge(ctx, now int64) error`; Typ `state.Staged`.

**Hinweis zu den Arten:** `dbapi` kennt feste Blob-Arten. Fassungen der Vertrauensliste liegen als
`dbapi.BlobTrust` (ID = Nummer als Text, `Seq` = Nummer), Lizenzen als `dbapi.BlobLicense`
(ID = App). Alles Übrige des Controllers – bereitgelegte Updates und die installierten
Softwareteile – liegt als `dbapi.BlobSetting` unter den Schlüsseln `staged.<ziel>` und
`components`. So braucht der Controller **keine Vertragsänderung**.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/server-controller/classes/state/state_test.go`:

```go
package state

import (
	"context"
	"errors"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s := New(memstore.New())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStagedRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	want := Staged{Target: "ngc-controller", Version: "1.2.0", SHA256: make([]byte, 32),
		Size: 17, Seq: 4, File: "/var/lib/ngc/staged/ngc-controller", State: "downloaded", Time: 100}
	if err := s.PutStaged(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Staged(ctx, "ngc-controller")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != want.Version || got.Seq != want.Seq || got.File != want.File {
		t.Fatalf("staged = %+v, want %+v", got, want)
	}
	if err := s.DeleteStaged(ctx, "ngc-controller"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Staged(ctx, "ngc-controller"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("after delete: err = %v, want ErrNotFound", err)
	}
}

func TestUseTaskRefusesReplay(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	id := []byte("0123456789abcdef")
	if err := s.UseTask(ctx, id, 200, 100); err != nil {
		t.Fatal(err)
	}
	if err := s.UseTask(ctx, id, 200, 100); !errors.Is(err, dbapi.ErrUsed) {
		t.Fatalf("second use: err = %v, want ErrUsed", err)
	}
}

func TestVersionsFrom(t *testing.T) {
	// Fassungen 1 bis 3 ablegen, ab 2 lesen: genau zwei, aufsteigend.
	// (Testdaten aus trusttest, siehe Aufgabe 3; hier reichen Rohbytes.)
}
```

Den dritten Test erst in Aufgabe 3 ausfüllen; hier bleibt er leer und grün.

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/state/`
Erwartet: FAIL, „undefined: New“.

- [ ] **Step 3: `state.Store` schreiben**

`apps/server-controller/classes/state/state.go` mit Paketkommentar „Package state keeps what the
server needs to carry on without an API“. Kern:

```go
type Staged struct {
	Target  string    `json:"target"`
	Version string    `json:"version,omitempty"`
	SHA256  enc.Bytes `json:"sha256"`
	Size    int64     `json:"size"`
	Signer  enc.Bytes `json:"signer,omitempty"` // the key whose signature was verified; empty for system
	Seq     uint64    `json:"seq,omitempty"`
	File    string    `json:"file"`
	State   string    `json:"state"`
	Time    int64     `json:"time"`
}

type Store struct{ db dbapi.Local }

func New(db dbapi.Local) *Store { return &Store{db: db} }

// putJSON writes one document; readJSON reads it back through
// enc.DecodeStrict, so a damaged row is refused like any input from outside.
func (s *Store) putJSON(ctx context.Context, kind, id string, seq uint64, v any) error
func (s *Store) getJSON(ctx context.Context, kind, id string, max int, v any) error
```

`PutStaged` schreibt `dbapi.BlobSetting` mit ID `"staged."+target`, `Staged` liest sie mit
`enc.DecodeStrict(data, 4096, &st)`. `PutComponents`/`Components` genauso unter `components`
(Budget: `enc.Budget{Size: 4096, Items: map[string]int{"": 3}}`, weil eine Map gezählt wird).
`UseTask` ruft `dbapi.OnceCreate(ctx, dbapi.OnceTask, id, "", expires)` und gibt dessen Fehler
unverändert zurück; `Purge` ruft `OncePurge(ctx, now)` – der Controller hat seine eigene Datenbank
und zieht deshalb kein `session.MaxSkew` ab.

- [ ] **Step 4: Test ausführen, er muss bestehen**

Ausführen: `go test ./apps/server-controller/classes/state/`
Erwartet: PASS.

- [ ] **Step 5: Fuzz-Test für die gelesenen Dokumente**

```go
func FuzzStaged(f *testing.F) {
	f.Add([]byte(`{"target":"system","sha256":"","size":0,"file":"/x","state":"downloaded","time":1}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		var st Staged
		_ = enc.DecodeStrict(b, 4096, &st) // must not panic
	})
}
```

Ausführen: `go test -run Fuzz -fuzz FuzzStaged -fuzztime 30s ./apps/server-controller/classes/state/`
Erwartet: kein Fund.

- [ ] **Step 6: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/state/
git commit -m "Add the controller's local state over dbapi.Local"
```

---

### Task 3: Vertrauensliste halten

**Files:**
- Create: `apps/server-controller/classes/chain/chain.go`,
  `apps/server-controller/classes/chain/chain_test.go`
- Modify: `apps/server-controller/classes/state/state_test.go` (dritter Test aus Aufgabe 2)

**Interfaces:**
- Consumes: `state.Store`, `trust.Anchor` aus `config.Controller.Anchor`.
- Produces: `chain.Keeper` mit `New(st *state.Store, a trust.Anchor) *Keeper`,
  `Load(ctx) error`, `Current() (trust.Version, bool)`, `List() *trust.List`, `Seq() uint64`,
  `Apply(ctx context.Context, b []byte) (uint64, error)` (nimmt die Bytes einer
  `session.TrustChain`, gibt die neueste geprüfte Nummer zurück).

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/server-controller/classes/chain/chain_test.go`:

```go
package chain

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

func TestApplyAdvancesAndPersists(t *testing.T) {
	ctx := context.Background()
	admin := trusttest.NewSigner(1)
	v1 := admin.Sign(t, trusttest.Genesis(admin))
	st := state.New(memstore.New())
	if err := st.PutVersion(ctx, v1, 1); err != nil {
		t.Fatal(err)
	}
	k := New(st, trust.Anchor{Seq: 1, Hash: trust.Hash(v1)})
	if err := k.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if k.Seq() != 1 {
		t.Fatalf("seq = %d, want 1", k.Seq())
	}
	v2 := admin.Sign(t, trusttest.Next(t, v1))
	b, err := json.Marshal(session.TrustChain{Versions: []trust.Signed{v2}})
	if err != nil {
		t.Fatal(err)
	}
	seq, err := k.Apply(ctx, b)
	if err != nil || seq != 2 {
		t.Fatalf("Apply = %d, %v, want 2, nil", seq, err)
	}
	// Nach einem Neustart muss Fassung 2 aus dem Zustand kommen.
	k2 := New(st, trust.Anchor{Seq: 1, Hash: trust.Hash(v1)})
	if err := k2.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if k2.Seq() != 2 {
		t.Fatalf("after reload seq = %d, want 2", k2.Seq())
	}
}

func TestApplyRefusesForgedVersion(t *testing.T) {
	// Fassung 2 von einem fremden Schlüssel unterschrieben: Apply muss scheitern
	// und die gehaltene Fassung darf 1 bleiben.
	ctx := context.Background()
	admin, other := trusttest.NewSigner(1), trusttest.NewSigner(9)
	v1 := admin.Sign(t, trusttest.Genesis(admin))
	st := state.New(memstore.New())
	if err := st.PutVersion(ctx, v1, 1); err != nil {
		t.Fatal(err)
	}
	k := New(st, trust.Anchor{Seq: 1, Hash: trust.Hash(v1)})
	if err := k.Load(ctx); err != nil {
		t.Fatal(err)
	}
	bad := other.Sign(t, trusttest.Next(t, v1))
	b, _ := json.Marshal(session.TrustChain{Versions: []trust.Signed{bad}})
	if _, err := k.Apply(ctx, b); err == nil {
		t.Fatal("Apply took a version signed by an unknown key")
	}
	if k.Seq() != 1 {
		t.Fatalf("seq = %d after a refused version, want 1", k.Seq())
	}
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/chain/`
Erwartet: FAIL, „undefined: New“.

- [ ] **Step 3: `chain.Keeper` schreiben**

`Load`: `st.Versions(ctx, anchor.Seq)` → `trust.Open(anchor, chain)`; ist nichts gespeichert,
bleibt `Current()` leer und `Seq()` 0 (der Controller wartet dann auf die erste Fassung).
`Apply`: `session.ParseTrustChain(b)` → ohne gehaltene Fassung `trust.Open(anchor, ch.Versions)`,
sonst `trust.Advance(cur, ch.Versions)` → jede neue Fassung mit `st.PutVersion` speichern → neue
Fassung erst **nach** erfolgreichem Speichern in `Keeper` setzen (sonst verliert ein Neustart die
Kette). Fehler kommen unverändert zurück (`trust.ErrChain`, `trust.ErrSigner`, …); der Aufrufer
bildet sie auf `E_VERIFY` ab. `Keeper` schützt seinen Zustand mit einem `sync.RWMutex`, weil
mehrere Tunnel gleichzeitig lesen.

- [ ] **Step 4: Test ausführen, er muss bestehen**

Ausführen: `go test ./apps/server-controller/classes/chain/`
Erwartet: PASS.

- [ ] **Step 5: Gegen die Testdaten des Vertrags prüfen**

Einen Test `TestAgainstVectors` ergänzen, der `shared/testdata/trust.json` liest und für jede
gültige Kette `Apply` erfolgreich, für jede ungültige erfolglos sein lässt. Die Datei wird mit
`os.ReadFile("../../../../shared/testdata/trust.json")` gelesen und mit `enc.DecodeStrict` in eine
lokale Struktur mit den Feldern gelesen, die der Test braucht.

- [ ] **Step 6: Den offenen Test aus Aufgabe 2 ausfüllen**

`TestVersionsFrom` in `state_test.go` mit `trusttest`-Fassungen füllen: drei Fassungen ablegen,
`Versions(ctx, 2)` gibt genau zwei zurück, aufsteigend nach `Seq`.

- [ ] **Step 7: Fuzz-Test**

```go
func FuzzApply(f *testing.F) {
	f.Add([]byte(`{"versions":[]}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		k := New(state.New(memstore.New()), trust.Anchor{Seq: 1, Hash: make([]byte, 32)})
		_, _ = k.Apply(context.Background(), b) // must not panic
	})
}
```

Ausführen: `go test -run Fuzz -fuzz FuzzApply -fuzztime 60s ./apps/server-controller/classes/chain/`

- [ ] **Step 8: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/chain/ apps/server-controller/classes/state/
git commit -m "Keep and advance the trust list from the pinned anchor"
```

---

### Task 4: Kopplung mit dem Kopplungscode

**Files:**
- Create: `apps/server-controller/classes/pairing/pairing.go`,
  `apps/server-controller/classes/pairing/pairing_test.go`
- Modify: `apps/server-controller/classes/node/main.go` (Funktion `pair`)

**Interfaces:**
- Consumes: `tunnel.ParsePairCode`, `tunnel.PairMAC`, `tunnel.PinnedTLS`, `tunnel.PathPair`,
  `config.SaveController`, `config.SaveAnchor`, `noisep.Keypair`.
- Produces: `pairing.Do(ctx context.Context, in pairing.Input) (pairing.Result, error)`;
  `pairing.Input{Code string, Name string, Roles []string, Noise noise.DHKey, Client *http.Client}`;
  `pairing.Result{APIs []config.APIRef, Anchor trust.Anchor}`;
  `pairing.Show(code tunnel.PairCode, noisePub []byte) string` – die zwei Zeilen, die ulh und die
  Shell vergleichen.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`apps/server-controller/classes/pairing/pairing_test.go` baut mit `httptest.NewUnstartedServer`
einen Server, dessen TLS-Zertifikat aus `trusttest.APITLS(0xE1, "api-1.test")` kommt, und prüft:

```go
func TestDoPairs(t *testing.T) {
	apiNoise, _ := trusttest.NodeKeys(0xA1)
	cert, pin := trusttest.APITLS(0xE1, "api-1.test")
	secret := bytes.Repeat([]byte{7}, tunnel.SecretSize)

	var got tunnel.PairRequest
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != tunnel.PathPair || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(io.LimitReader(r.Body, tunnel.MaxPairRequest+1))
		var err error
		if got, err = tunnel.ParsePairRequest(b); err != nil {
			t.Errorf("ParsePairRequest: %v", err)
		}
		if err := got.Verify(secret, apiNoise.Public); err != nil {
			t.Errorf("Verify: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	defer srv.Close()
	// … Kopplungscode mit url = wss://<host:port des Servers>/tunnel, key = apiNoise.Public,
	// tls = pin, secret, anchor bauen, pairing.Do aufrufen und prüfen, dass Result.APIs
	// genau einen Eintrag mit dieser URL, diesem Schlüssel und diesem Pin trägt.
}

func TestDoRefusesWrongPin(t *testing.T) {
	// Derselbe Server, aber der Code trägt einen fremden Pin: Do muss scheitern,
	// und der Server darf keine Anfrage gesehen haben.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/pairing/`
Erwartet: FAIL, „undefined: Do“.

- [ ] **Step 3: `pairing.Do` schreiben**

Ablauf, genau in dieser Reihenfolge:

1. `code, err := tunnel.ParsePairCode(in.Code)` – vorher wird nichts mit dem Text gemacht.
2. Aus `code.URL` Host und Port holen (`strings.TrimPrefix` auf `wss://`, `strings.TrimSuffix` auf
   `/tunnel`, dann `net.SplitHostPort`); die Adresse ist durch `ParsePairCode` bereits
   `trust.ValidTunnelURL`.
3. HTTP-Client mit `Transport{TLSClientConfig: tunnel.PinnedTLS(code.TLS)}` und
   `tlsCfg.ServerName = host`; Zeitlimit 30 s.
4. `req := tunnel.PairRequest{Name: in.Name, Noise: in.Noise.Public,
   MAC: tunnel.PairMAC(code.Secret, in.Noise.Public, code.Key)}`, als JSON POSTen an
   `"https://" + net.JoinHostPort(host, port) + tunnel.PathPair`, `Content-Type: application/json`.
5. Nur 204 gilt als Erfolg; jede andere Antwort ist `tunnel.ErrPair`. Der Antwortkörper wird auf
   `tunnel.MaxPairRequest` begrenzt gelesen und nie ins Log geschrieben.
6. Ergebnis: `config.APIRef{URL: code.URL, Key: code.Key, TLS: code.TLS}` und `code.Anchor`.

Der Kopplungscode, `code.Secret` und der MAC kommen nie ins Log.

- [ ] **Step 4: Test ausführen, er muss bestehen**

Ausführen: `go test ./apps/server-controller/classes/pairing/`
Erwartet: PASS.

- [ ] **Step 5: Das Kommando `pair` verdrahten**

`node.pair` liest `controller.json` (oder legt sie an, wenn sie fehlt), erzeugt mit
`noisep.Keypair(rand.Reader)` den Schlüssel, ruft `pairing.Do`, schreibt danach
`config.SaveController` (Name, Rollen, `noiseKey`, `apis`, `anchor`, `db`, `dbKey`, `releaseKey`)
und `config.SaveAnchor(config.AnchorFile, res.Anchor)` und gibt auf stdout aus:

```
Fingerabdruck dieses Controllers: <tunnel.Fingerprint(noise.Public)>
Anker: Fassung <seq>, <tunnel.Fingerprint(anchor.Hash)>
```

Beides vergleicht der Admin mit der Anzeige der Shell (`shared/ulh.md`, Regel zu den Codes).

- [ ] **Step 6: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/pairing/ apps/server-controller/classes/node/
git commit -m "Pair the controller with an API using a pairing code"
```

---

## Etappe 2 – Tunnel (Aufgaben 5 bis 7)

### Task 5: Tunnel-Transport mit Noise

**Files:**
- Create: `apps/server-controller/classes/link/link.go`,
  `apps/server-controller/classes/link/link_test.go`
- Modify: `tools/modules.go` (Zeile `github.com/coder/websocket` streichen)

**Interfaces:**
- Consumes: `noisep.Handshake`, `noisep.PrologueTunnel`, `noisep.NewTransport`,
  `tunnel.PinnedTLS`, `tunnel.PathTunnel`, `tunnel.Subprotocol`, `tunnel.Fingerprint`,
  `wire.HandshakeSize`.
- Produces: `link.Config{API config.APIRef, Noise noise.DHKey, Hello func() tunnel.Hello,
  Handler Handler, Log *slog.Logger, Now func() int64}`;
  `link.Dial(ctx, Config) (*Tunnel, error)`; `(*Tunnel).Run(ctx) error`;
  `(*Tunnel).Control(t string, body any) error`; `link.Keep(ctx, Config)` – Wiederaufbau mit
  wachsenden Pausen.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

Der Test baut mit `websocket.Accept` eine Gegenstelle, die die API spielt: sie nimmt Noise-Nachricht 1
entgegen, antwortet mit Nachricht 2 und schickt dann ihr `hello`.

```go
func TestDialCompletesHandshakeAndHello(t *testing.T) {
	apiKey, _ := trusttest.NodeKeys(0xA1)
	ctlKey, _ := trusttest.NodeKeys(0xC1)
	cert, pin := trusttest.APITLS(0xE1, "api-1.test")

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("key"); got != tunnel.Fingerprint(ctlKey.Public) {
			t.Errorf("key = %q", got)
		}
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{tunnel.Subprotocol}})
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		// Noise als Antwortende; dann hello als erste Transportnachricht.
		…
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	defer srv.Close()

	tn, err := Dial(context.Background(), Config{
		API:   config.APIRef{URL: wssOf(srv), Key: apiKey.Public, TLS: pin},
		Noise: ctlKey,
		Hello: func() tunnel.Hello { return tunnel.Hello{Version: "1.0.0", Contract: tunnel.Contract} },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tn.Close()
	if got := tn.Peer(); got.Contract != tunnel.Contract {
		t.Fatalf("peer hello = %+v", got)
	}
}

func TestDialRefusesWrongTLSPin(t *testing.T) { /* fremder Pin: Dial scheitert */ }
func TestDialRefusesTextMessage(t *testing.T) { /* Textnachricht der API beendet die Leitung */ }
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/link/`
Erwartet: FAIL, „undefined: Dial“.

- [ ] **Step 3: `link.Dial` und den Rahmenlauf schreiben**

- Adresse: `cfg.API.URL + "?key=" + url.QueryEscape(tunnel.Fingerprint(cfg.Noise.Public))`.
  `websocket.Dial` mit `HTTPClient{Transport: &http.Transport{TLSClientConfig: tlsCfg}}`,
  `tlsCfg := tunnel.PinnedTLS(cfg.API.TLS)`, `tlsCfg.ServerName = host`, Unterprotokoll
  `tunnel.Subprotocol`; `c.SetReadLimit(wire.MaxFrame + noisep.TagSize)`.
- Noise: `hs, _ := noisep.Handshake(true, []byte(noisep.PrologueTunnel), cfg.Noise, cfg.API.Key, rand.Reader)`;
  Nachricht 1 ohne Nutzlast schreiben (binär, genau `wire.HandshakeSize` Byte), Nachricht 2 lesen
  und auf genau `wire.HandshakeSize` Byte prüfen, dann `noisep.NewTransport(true, cs1, cs2)`.
- Danach ist jede binäre Nachricht genau eine Transportnachricht mit genau einem Frame:
  `t.Open(msg)` → `wire.Decode`. Eine Textnachricht, ein kaputter Frame oder ein
  Entschlüsselungsfehler beenden die Leitung sofort (`c.Close`), ohne Antwort.
- `hello` wird als erste eigene Transportnachricht geschickt (Steuerkanal 0, `wire.Control{T: wire.CtlHello}`),
  das `hello` der API wird gelesen, bevor irgendein Kanal angenommen wird.
- Schreiben ist mit einem `sync.Mutex` serialisiert, weil Status, Antworten und Kanäle
  gleichzeitig schreiben; der Noise-Zähler verträgt keine Nebenläufigkeit.
- `PingInterval` (30 s) als `c.Ping(ctx)`; bleibt der Pong aus, gilt die Leitung als tot.

- [ ] **Step 4: Test ausführen, er muss bestehen**

Ausführen: `go test ./apps/server-controller/classes/link/`
Erwartet: PASS.

- [ ] **Step 5: `link.Keep` mit wachsenden Pausen**

```go
// Keep dials again and again until ctx ends. The pause doubles from
// tunnel.ReconnectMin to tunnel.ReconnectMax and carries up to 20 % jitter,
// so that seven controllers do not all return in the same second.
func Keep(ctx context.Context, cfg Config)
```

Test: eine Gegenstelle, die die ersten zwei Versuche ablehnt und den dritten annimmt; der Test
setzt `cfg.Sleep` (Feld für Tests, sonst `time.Sleep`) auf eine Zählfunktion und prüft die
Pausenfolge 1 s, 2 s.

- [ ] **Step 6: `tools/modules.go` anpassen**

Die Zeile `_ "github.com/coder/websocket"` streichen (der Controller benutzt das Modul jetzt zuerst).
Vorher der Sitzung API kurz Bescheid geben, damit die Zeile nicht zweimal verschwindet.

- [ ] **Step 7: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/link/ tools/modules.go
git commit -m "Dial the tunnel to an API over TLS and Noise_KK"
```

---

### Task 6: Kanäle, Freigabe und Grenzen

**Files:**
- Create: `apps/server-controller/classes/link/channel.go`,
  `apps/server-controller/classes/link/channel_test.go`

**Interfaces:**
- Produces: `link.Handler` (`Open(ctx context.Context, ch *link.Channel, o tunnel.Opened)`);
  `link.Channel` mit `Num() uint32`, `Recv(ctx) (link.Inbound, error)`, `Send(b []byte) error`,
  `EOF() error`, `Close(cl wire.Close) error`, `Grant(credit uint32, handshake []byte) error`,
  `Window() uint32`; `link.Inbound{Kind wire.Type, Data []byte}`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

`channel_test.go` treibt einen `*Tunnel` über eine Schleife im Speicher (Hilfsfunktion `pipePair`,
die zwei `net.Conn` verbindet und beide Seiten mit Noise ausstattet) und prüft:

```go
func TestChannelNumberFreeOnlyAfterBothCloses(t *testing.T) {
	// OPEN 1 → Handler antwortet → CLOSE des Controllers → ein OPEN für 1 der API
	// vor deren CLOSE ist ein Protokollfehler (CLOSE E_PROTOCOL), nach deren CLOSE
	// nicht mehr.
}

func TestTooManyChannelsEndsTheLink(t *testing.T) {
	// wire.MaxTunnelChannels offene Nummern, dann ein OPEN mehr: die Leitung endet.
}

func TestMoreDataThanGrantedClosesTheChannel(t *testing.T) {
	// window aus dem OPEN ist wire.MinWindow; die API schickt ein Byte mehr:
	// CLOSE E_PROTOCOL für diesen Kanal, die Leitung bleibt.
}

func TestGrantAboveMaxWindowClosesTheChannel(t *testing.T) {
	// GRANT, die die offene Freigabe über wire.MaxWindow höbe: CLOSE E_PROTOCOL.
}

func TestFramesAfterOwnCloseAreDropped(t *testing.T) {
	// Nach dem eigenen CLOSE kommt DATA: keine Antwort; das CLOSE der API darunter
	// zählt trotzdem als empfangen und gibt die Nummer frei.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/link/ -run Channel`
Erwartet: FAIL.

- [ ] **Step 3: Die Kanalverwaltung schreiben**

Zustand je Nummer: `open`, `closing` (eigenes CLOSE gesendet), `peerClosed` (CLOSE empfangen);
frei erst, wenn gesendet **und** empfangen. Regeln genau nach `shared/README.md`, Abschnitt
„Öffnen, Schließen, Freigabe“:

- Nur die API öffnet; ein OPEN des Controllers gibt es nicht.
- Ein Frame für eine Nummer, die weder offen ist noch schließt, ein OPEN für eine offene Nummer:
  Antwort `CLOSE E_PROTOCOL` für diese Nummer.
- Führte ein Frame (OPEN oder das nötige `CLOSE E_PROTOCOL`) über `wire.MaxTunnelChannels`
  hinaus, endet die **Leitung**, nicht nur der Kanal.
- Freigabe je Richtung in Bytes der DATA-Nutzlast; `Opened.Window` ist die erste Freigabe der API
  an den Controller (**nicht** `window` im OPEN des Browsers – das gilt nur zwischen Browser und
  API). Mehr DATA als freigegeben oder eine GRANT über `wire.MaxWindow`: `CLOSE E_PROTOCOL`.
- Der Controller gibt selbst frei: erste GRANT mindestens `wire.MinWindow`, danach nach dem
  Abfluss, nie mehr als `wire.MaxWindow` offen.
- Jedes CLOSE wird mit CLOSE beantwortet, außer man hat selbst schon eines geschickt.
- Jeder Kanal läuft in seiner eigenen Goroutine; ihr Ende schreibt genau ein CLOSE.

- [ ] **Step 4: Test ausführen, er muss bestehen**

Ausführen: `go test ./apps/server-controller/classes/link/`
Erwartet: PASS.

- [ ] **Step 5: Fuzz-Test über den Zustandsautomaten**

```go
func FuzzChannelFrames(f *testing.F) {
	f.Add([]byte{1, 0, 0, 0, 1})
	f.Fuzz(func(t *testing.T, b []byte) {
		// Frames stückweise in den Automaten geben; er darf nie in Panik geraten
		// und nie eine Nummer freigeben, für die kein CLOSE empfangen wurde.
	})
}
```

Ausführen: `go test -run Fuzz -fuzz FuzzChannelFrames -fuzztime 60s ./apps/server-controller/classes/link/`

- [ ] **Step 6: Prüfen und committen**

```bash
tools/check.sh
go test -race ./apps/server-controller/classes/link/   # auf Linux
git add apps/server-controller/classes/link/
git commit -m "Track tunnel channels, credit and their limits"
```

---

### Task 7: Steuerkanal – hello, status, update, notify

**Files:**
- Create: `apps/server-controller/classes/link/control.go`,
  `apps/server-controller/classes/link/control_test.go`,
  `apps/server-controller/classes/node/report.go`,
  `apps/server-controller/classes/node/report_test.go`

**Interfaces:**
- Consumes: `wire.ControlAllowed`, `tunnel.ParseHello`, `tunnel.Status`, `tunnel.Components`,
  `store.UpdateReport`, `core.Notice`.
- Produces: `(*Tunnel).Control(t string, body any) error`; `(*Tunnel).Peer() tunnel.Hello`;
  `node.Reporter` mit `Status(ctx) (tunnel.Status, error)`, `Notify(level, text, app string)`,
  `Update(r store.UpdateReport)`; `node.OSRelease(path string) (string, error)` (z. B.
  `debian-13`).

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

```go
func TestControlRefusesWhatTheAPIMayNotSend(t *testing.T) {
	// Die API schickt "status": wire.ControlAllowed(wire.LinkTunnel, true, "status")
	// ist falsch, also endet die Leitung.
}

func TestHelloNamesNodeRolesAndContract(t *testing.T) {
	// Der Controller schickt hello mit Node aus der Konfiguration, Contract =
	// tunnel.Contract, Roles, Arch, OS und TrustSeq der gehaltenen Fassung.
}

func TestStatusFitsTheLimits(t *testing.T) {
	// 300 Apps im Zustand: Status meldet höchstens tunnel.MaxStatusApps, und
	// tunnel.ParseStatus nimmt die Nachricht an.
}

func TestOSRelease(t *testing.T) {
	// Datei mit ID=debian und VERSION_ID="13" → "debian-13";
	// fehlende Felder, Anführungszeichen, Zeilen ohne "=" und eine Datei mit
	// 10 MB Müll dürfen nichts umwerfen.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/link/ ./apps/server-controller/classes/node/`
Erwartet: FAIL.

- [ ] **Step 3: Steuerkanal schreiben**

- Empfang: `wire.ParseControl` → `wire.ControlAllowed(wire.LinkTunnel, true, c.T)`; alles andere
  beendet die Leitung. Erlaubt sind von der API nur `hello` und `bye`.
- Senden: `hello`, `status`, `update`, `notify`, `bye` – jeweils der Körper durch den Parser des
  Vertrags geschickt, bevor er hinausgeht (`tunnel.ParseHello`, `tunnel.ParseStatus`,
  `store.ParseUpdateReport`, `core.ParseNotice`), damit der Controller nie etwas schickt, was die
  Gegenseite ablehnen müsste.
- Fassungen: Die neuere Seite spricht die Fassung der älteren.
  `t.speaks = min(tunnel.Contract, peer.Contract)`; liegen beide mehr als eine Fassung
  auseinander (`abs(diff) > 1`), endet die Leitung mit einer deutschen Meldung im Log.
- `hello` der API mit einem anderen `Node`, als der Controller trägt, ist **kein** Fehler des
  Controllers; entscheidend ist nur, dass der Controller seinen eigenen Knoten nennt (leer,
  solange er nicht in der Vertrauensliste steht).

- [ ] **Step 4: `node.Reporter` schreiben**

`Status` sammelt: `tunnel.AppStatus` je Zeile aus `state.Apps` (ID, Version, Zustand aus
`runtime.Running` bzw. `tunnel.StateUnlicensed`), höchstens `tunnel.MaxStatusApps` (die übrigen
bleiben weg, die API sieht die Liste nur zur Anzeige); `Components` aus `state.Components`,
gefiltert auf `tunnel.Components(cfg.Roles)`; `DiskFree` aus `syscall.Statfs(config.AppDataDir)`
(`Bavail * Bsize`). Auf Nicht-Linux liefert `DiskFree` 0 und der Test überspringt sich.

`OSRelease` liest `/etc/os-release` mit einem `bufio.Scanner` und `LimitReader` auf 64 KiB, nimmt
nur `ID` und `VERSION_ID`, entfernt genau ein Paar Anführungszeichen und prüft das Ergebnis mit
`wire.ValidName`; passt es nicht, bleibt das Feld leer.

- [ ] **Step 5: Tests ausführen, sie müssen bestehen**

Ausführen: `go test ./apps/server-controller/classes/link/ ./apps/server-controller/classes/node/`
Erwartet: PASS.

- [ ] **Step 6: Fuzz-Test für `/etc/os-release`**

```go
func FuzzOSRelease(f *testing.F) {
	f.Add([]byte("ID=debian\nVERSION_ID=\"13\"\n"))
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = parseOSRelease(b) })
}
```

- [ ] **Step 7: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/link/ apps/server-controller/classes/node/
git commit -m "Speak the tunnel control channel: hello, status, update, notify"
```

---

## Etappe 3 – Zugang und Ende zu Ende (Aufgaben 8 bis 10)

### Task 8: Zugang prüfen

**Files:**
- Create: `apps/server-controller/classes/access/access.go`,
  `apps/server-controller/classes/access/access_test.go`

**Interfaces:**
- Consumes: `tunnel.VerifyOpen`, `task.Check`, `task.Input`, `core.RuleFor`, `store.RuleFor`,
  `core.Lookup`, `core.WithoutUser`, `core.ShellOpens`, `state.Store.UseTask`.
- Produces:
  `access.Check(ctx context.Context, r access.Request) (access.Decision, *wire.Close)`;
  `access.Request{Payload []byte, List *trust.List, Node string, Now int64, Apps AppLookup, Tasks TaskOnce}`;
  `access.Decision{Opened tunnel.Opened, Action core.Action, Core bool, Task *task.Task,
  User string, UserName string, App dbapi.App, Manifest *store.Manifest}`;
  `access.AppLookup` (`App(ctx, id string) (dbapi.App, error)`), `access.TaskOnce`
  (`UseTask(ctx, id []byte, expires, now int64) error`).

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

```go
func TestCheckAcceptsSignedRootTask(t *testing.T) {
	// activate mit gültiger Aufgabe des Admins aus trusttest.Genesis auf ctl-1:
	// Decision.Task != nil, Decision.User = ID des Admins, kein CLOSE.
}

func TestCheckRefusesForgedTask(t *testing.T) {
	// Dieselbe Aufgabe, aber mit dem Schlüssel eines Fremden unterschrieben:
	// CLOSE E_VERIFY.
}

func TestCheckRefusesSwappedTask(t *testing.T) {
	// Gültige Aufgabe für "deactivate", im OPEN steht "uninstall": CLOSE E_VERIFY.
}

func TestCheckRefusesReplayedTask(t *testing.T) {
	// Dieselbe Aufgabe zweimal: beim zweiten Mal CLOSE E_VERIFY (dbapi.ErrUsed).
}

func TestCheckRefusesRootTaskWithoutRootPermission(t *testing.T) {
	// Benutzer ohne Root-Recht auf diesem Knoten: CLOSE E_FORBIDDEN.
}

func TestCheckAsksForConfirmation(t *testing.T) {
	// Rolle mit Level "root", Aufgabe ohne Bestätigung: CLOSE E_CONFIRM.
}

func TestCheckWithoutUserOnlyForListedActions(t *testing.T) {
	// Ohne Client-Zertifikat: ngc.users/controller/trust geht, activate nicht
	// (CLOSE E_FORBIDDEN).
}

func TestCheckRefusesShellOpenOfStage(t *testing.T) {
	// OPEN mit Client-Zertifikat für ngc.store/controller/stage: CLOSE E_FORBIDDEN,
	// denn core.ShellOpens(stage) ist falsch.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/access/`
Erwartet: FAIL, „undefined: Check“.

- [ ] **Step 3: `access.Check` schreiben**

Reihenfolge, jede Stufe bricht mit dem genannten Code ab:

1. `o, err := tunnel.VerifyOpen(r.Payload, r.List, r.Now)` → Fehler: `E_VERIFY`
   (fehlt die Vertrauensliste ganz, `E_UNREACHABLE`, denn ohne sie kann nichts geprüft werden).
2. `a, ok := core.Lookup(o.Open.App, o.Open.Service, o.Open.Action)`.
   - Kern-Aktion (`ok`): Dienst muss `core.ServiceController` sein, sonst `E_NOTFOUND`.
     Mit Client-Zertifikat muss `core.ShellOpens(a)` gelten, sonst `E_FORBIDDEN`;
     ohne Client-Zertifikat muss `core.WithoutUser(a)` gelten, sonst `E_FORBIDDEN`.
     Regel: `core.RuleFor(a)`.
   - Sonst App-Kanal: `app, err := r.Apps.App(ctx, o.Open.App)` → `E_NOTFOUND`;
     `m, err := store.ParseManifest(app.Manifest)` → `E_INTERNAL`;
     `rule, err := store.RuleFor(m, o.Open.Action)` → `E_NOTFOUND`.
     Die Regel kommt **nur** aus dem Manifest der eigenen Datenbank, nie aus dem OPEN.
3. `tk, err := task.Check(task.Input{Task: o.Open.Task, Confirm: o.Open.Confirm,
   Session: *o.Session, Node: r.Node, App: …, Service: …, Action: …, Rule: rule}, r.List, r.Now)`.
   Abbildung der Fehler: `task.ErrNoTask` und `task.ErrConfirm` → `E_CONFIRM`,
   `task.ErrForbidden` → `E_FORBIDDEN`, alles andere → `E_VERIFY`.
   Ohne `o.Session` (Kanal ohne Benutzer) wird `task.Check` nicht aufgerufen; dann muss die
   Aktion in `core.WithoutUser` stehen und trägt weder Benutzer noch Aufgabe.
4. Wiedereinspielung: bei `tk != nil` `r.Tasks.UseTask(ctx, tk.ID, tk.Expires, r.Now)`;
   `dbapi.ErrUsed` → `E_VERIFY`.
5. `Decision.User` ist **nur** `o.Session.Cert().User`, und nur dann gesetzt, wenn ein
   Ende-zu-Ende-Handschlag folgt oder eine Aufgabe geprüft wurde. Auf einem Kanal mit Zertifikat,
   aber ohne beides, bleibt `User` leer: dort wird nach ihm weder berechtigt noch protokolliert.

- [ ] **Step 4: Test ausführen, er muss bestehen**

Ausführen: `go test ./apps/server-controller/classes/access/`
Erwartet: PASS.

- [ ] **Step 5: Fuzz-Test über das OPEN**

```go
func FuzzCheck(f *testing.F) {
	f.Add([]byte(`{"open":"","window":4096}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = Check(context.Background(), Request{Payload: b, List: testList, Node: "ctl-1", Now: 1_800_000_000, …})
	})
}
```

Ausführen: `go test -run Fuzz -fuzz FuzzCheck -fuzztime 60s ./apps/server-controller/classes/access/`

- [ ] **Step 6: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/access/
git commit -m "Check every OPEN against the trust list and its signed task"
```

---

### Task 9: Ende-zu-Ende-Verschlüsselung je Kanal

**Files:**
- Create: `apps/server-controller/classes/e2e/e2e.go`,
  `apps/server-controller/classes/e2e/e2e_test.go`

**Interfaces:**
- Consumes: `noisep.E2EPrologue`, `noisep.Handshake`, `noisep.NewTransport`, `wire.ParseE2E`,
  `wire.E2E`, `wire.HandshakeSize`, `wire.E2EFirstTimeout`, `wire.MaxE2EPlaintext`,
  `wire.MaxCallResult`.
- Produces: `e2e.Accept(o tunnel.Opened, nodeID string, static noise.DHKey) (*e2e.Session, []byte, error)`
  (zweiter Rückgabewert: Noise-Nachricht 2 für die erste GRANT);
  `(*Session).Open(ciphertext []byte) (wire.E2EMessage, error)`;
  `(*Session).Data(b []byte) ([]byte, error)`; `(*Session).End(cl wire.Close) ([]byte, error)`;
  `(*Session).Sent() int64`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

```go
func TestAcceptMatchesTheVectors(t *testing.T) {
	// shared/testdata/noise.json: für jeden Fall unter "prologues" muss
	// noisep.E2EPrologue mit Opened.Open.Params und Opened.Open.TaskBytes()
	// genau den Prolog der Datei ergeben, und Nachricht 1 der Datei muss sich
	// mit dem Schlüssel des Falls öffnen lassen.
}

func TestPrologueBindsParams(t *testing.T) {
	// Dasselbe OPEN mit einem geänderten Byte in params: der Handschlag scheitert.
}

func TestPrologueBindsTask(t *testing.T) {
	// Eine andere, für sich gültige Aufgabe desselben Benutzers untergeschoben:
	// der Handschlag scheitert.
}

func TestEndCarriesTheOutcome(t *testing.T) {
	// End(wire.Close{Status: wire.StatusOK}) ergibt eine innere Nachricht, die
	// wire.ParseE2E(…, true) als E2EEnd mit Status ok liest.
}

func TestNothingAfterOwnEnd(t *testing.T) {
	// Nach End gibt Data einen Fehler zurück.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/e2e/`
Erwartet: FAIL.

- [ ] **Step 3: `e2e.Session` schreiben**

- Prolog: `noisep.E2EPrologue(nodeID, o.Open.App, o.Open.Service, o.Open.Action,
  o.Open.Params, o.Open.TaskBytes())` – die Bytes kommen unverändert aus `tunnel.VerifyOpen`.
- Peer-Schlüssel ist `o.Session.Cert().DH`; ohne Sitzung gibt es keinen
  Ende-zu-Ende-Kanal (App-Kanäle haben immer eine).
- `noisep.Handshake(false, prologue, static, peer, rand.Reader)`; Nachricht 1 ist `o.Open.E2E`
  (genau `wire.HandshakeSize` Byte, sonst Fehler), Nachricht 2 ohne Nutzlast zurück.
- `Open` entschlüsselt eine DATA-Nutzlast und liest sie mit `wire.ParseE2E(plain, false)`.
- `Data`/`End` verschlüsseln `wire.E2E(wire.E2EData, b)` bzw. `wire.E2E(wire.E2EEnd, closeJSON)`;
  `Data` bricht ab, wenn der Klartext `wire.MaxE2EPlaintext` überschreitet oder schon ein `End`
  ging.
- `Sent()` zählt die Nutzbytes, damit der Aufrufer `wire.MaxCallResult` bei Aktionen ohne
  `stream` durchsetzen und sonst mit `E_TOOLARGE` im Ende abbrechen kann.

- [ ] **Step 4: Test ausführen, er muss bestehen**

Ausführen: `go test ./apps/server-controller/classes/e2e/`
Erwartet: PASS.

- [ ] **Step 5: Fuzz-Test für die inneren Nachrichten**

```go
func FuzzInner(f *testing.F) {
	f.Add([]byte{1})
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = wire.ParseE2E(b, false) })
}
```

Zusätzlich ein Fuzz-Ziel `FuzzOpenCiphertext`, das beliebige Bytes in `(*Session).Open` gibt.

- [ ] **Step 6: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/e2e/
git commit -m "Run the end-to-end handshake and inner messages per channel"
```

---

### Task 10: Kanäle verteilen

**Files:**
- Create: `apps/server-controller/classes/serve/serve.go`,
  `apps/server-controller/classes/serve/serve_test.go`,
  `apps/server-controller/classes/serve/trust.go`

**Interfaces:**
- Consumes: `link.Handler`, `access.Check`, `e2e.Accept`, `chain.Keeper`, `core.Action`,
  `core.EncodeResult`, `core.TrustResult`.
- Produces: `serve.Server` mit `New(d serve.Deps) *Server` und der Methode
  `Open(ctx context.Context, ch *link.Channel, o tunnel.Opened) error` (erfüllt `link.Handler`);
  `serve.Deps{Node string, Chain *chain.Keeper, State *state.Store, Install Installer,
  Update Updater, License Licenser, Runtime runtime.Engine, Now func() int64, Log *slog.Logger}`.
  Die Schnittstellen `Installer`, `Updater` und `Licenser` werden in den Aufgaben 15 bis 21
  gefüllt; hier stehen sie mit genau den Methoden, die diese Aufgaben liefern.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

```go
func TestTrustChannelAnswersWithNewestSeq(t *testing.T) {
	// ngc.users/controller/trust ohne Benutzer: die API schickt eine TrustChain,
	// der Controller antwortet mit core.TrustResult{Seq: 2} und CLOSE ok.
}

func TestTrustChannelRefusesBrokenChain(t *testing.T) {
	// Kaputte Kette: CLOSE E_VERIFY, und die gehaltene Fassung bleibt.
}

func TestInputAboveMaxInClosesWithTooLarge(t *testing.T) {
	// Mehr als core.Action.MaxIn auf dem trust-Kanal: CLOSE E_TOOLARGE.
}

func TestCallResultAboveLimitClosesWithTooLarge(t *testing.T) {
	// Ein Ergebnis über wire.MaxCallResult wird nicht geschickt, sondern der Kanal
	// endet mit E_TOOLARGE.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/serve/`
Erwartet: FAIL.

- [ ] **Step 3: `serve.Server.Open` schreiben**

1. `access.Check` – bei einem `*wire.Close` sofort schließen.
2. Kern-Aktion: erste GRANT (`wire.MinWindow` oder mehr, ohne Handschlag) → Eingabe je nach
   `Flow` lesen, dabei `MaxIn` mitzählen (mehr → `E_TOOLARGE`) → Handler aufrufen → Ergebnis mit
   `core.EncodeResult` kodieren, gegen `wire.MaxCallResult` prüfen, als DATA schicken, EOF,
   CLOSE `ok`.
3. App-Kanal: `e2e.Accept` → erste GRANT mit Noise-Nachricht 2 → auf die erste innere Nachricht
   warten, höchstens `wire.E2EFirstTimeout` Sekunden; erst danach wird der Dienst gestartet
   (Aufgabe 14). Kommt EOF oder CLOSE vor dem Ende der Shell, ist die Eingabe abgeschnitten:
   Dienst beenden, Fehler melden.
4. Jeder Kanal bekommt seinen eigenen `context.WithCancel`, der beim CLOSE abgebrochen wird.

`serve/trust.go` bedient `ngc.users/controller/trust`: Bytes sammeln (bis `MaxIn`),
`chain.Apply`, Antwort `core.TrustResult{Seq: neu}`; bei Fehler CLOSE `E_VERIFY`.

- [ ] **Step 4: Test ausführen, er muss bestehen**

Ausführen: `go test ./apps/server-controller/classes/serve/`
Erwartet: PASS.

- [ ] **Step 5: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/serve/
git commit -m "Dispatch tunnel channels to core actions and app services"
```

---

## Etappe 4 – Container (Aufgaben 11 bis 14)

### Task 11: Container-Schnittstelle und Podman

**Files:**
- Create: `apps/server-controller/classes/runtime/runtime.go`,
  `apps/server-controller/classes/runtime/podman.go`,
  `apps/server-controller/classes/runtime/podman_test.go`,
  `apps/server-controller/classes/runtime/image.go`,
  `apps/server-controller/classes/runtime/image_test.go`

**Interfaces:**
- Produces: `runtime.Engine` (Schnittstelle, damit LXC später danebenpasst) mit
  `LoadImage(ctx, tarPath string) (string, error)`, `RemoveImage(ctx, ref string) error`,
  `Create(ctx, c Container) error`, `Start(ctx, name string) error`, `Stop(ctx, name string) error`,
  `Remove(ctx, name string) error`, `Running(ctx, name string) (bool, error)`,
  `Exec(ctx, e Exec) (int, error)`;
  `runtime.Container{Name, Image string, UID, GID uint32, DataDir, Hold string, Network, Root bool,
  CPU, Memory int}`; `runtime.Exec{Name string, Command []string, EnvFile string, Stdin io.Reader,
  Stdout, Stderr io.Writer, Root bool}`; `runtime.NewPodman(bin string) *Podman`;
  `runtime.ImageTag(tarPath string) (string, error)`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

Der Test ersetzt `podman` durch ein Skript, das seine Argumente zeilenweise in eine Datei schreibt
(nur Linux; sonst `t.Skip`).

```go
func TestCreateBuildsTheArgumentList(t *testing.T) {
	bin := fakePodman(t) // schreibt os.Args[1:] nach $RECORD
	p := NewPodman(bin)
	err := p.Create(context.Background(), Container{
		Name: "ngc-terminal-1a2b3c4d", Image: "ngc/terminal:1.0.0",
		UID: 900, GID: 900, DataDir: "/var/lib/ngc/apps/org.sorglos.terminal",
		Hold: "/usr/lib/ngc/ngc-controller", CPU: 1000, Memory: 256,
	})
	if err != nil {
		t.Fatal(err)
	}
	args := recorded(t)
	want := []string{"create", "--name", "ngc-terminal-1a2b3c4d", "--network", "none",
		"--cpus", "1.000", "--memory", "256m", "--read-only", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--volume", "/var/lib/ngc/apps/org.sorglos.terminal:/data:rw",
		"--volume", "/usr/lib/ngc/ngc-controller:/ngc/ngc-hold:ro",
		"--user", "0:0", "ngc/terminal:1.0.0", "/ngc/ngc-hold", "hold"}
	if !slices.Equal(args, want) {
		t.Fatalf("args =\n%q\nwant\n%q", args, want)
	}
}

func TestCreateWithNetworkAndRoot(t *testing.T) {
	// Network: statt "--network none" steht "--network slirp4netns"; Root: ohne
	// "--cap-drop ALL" wäre es falsch – der Container läuft trotzdem im eigenen
	// Benutzernamensraum des App-Benutzers.
}

func TestExecNeverUsesAShell(t *testing.T) {
	// Ein Befehl mit Leerzeichen und Anführungszeichen landet als einzelnes
	// Argument; nirgends steht "sh" oder "-c".
}

func TestImageTagRefusesMoreThanOneRepoTag(t *testing.T) {
	// Ein Image-Tar mit zwei RepoTags oder ohne RepoTag wird abgelehnt.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/runtime/`
Erwartet: FAIL.

- [ ] **Step 3: `runtime.Podman` schreiben**

- Jeder Aufruf ist `exec.CommandContext(ctx, p.bin, args...)` mit einer Liste; nirgends `sh -c`,
  nirgends eine gebaute Zeichenkette.
- Der Prozess läuft unter dem Systembenutzer der App:
  `cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: c.UID, Gid: c.GID}}`,
  Umgebung nur `HOME=/var/lib/ngc/apps/<app>`, `XDG_RUNTIME_DIR=/run/ngc/<app>`,
  `PATH=/usr/bin:/bin`, `LANG=C.UTF-8`. So läuft Podman rootless – wer aus dem Container
  ausbricht, landet bei einem Benutzer ohne Rechte.
- Standardmäßig kein Netz (`--network none`), `--read-only`, `--cap-drop ALL`,
  `--security-opt no-new-privileges`; `store.NeedNetwork` schaltet das Netz frei,
  `store.NeedRoot` erlaubt root **im Container** (`--user 0:0` bleibt, die Fähigkeiten kommen
  zurück), nie auf dem Server.
- Grenzen aus dem Manifest: `--cpus` (Tausendstel Kerne als `%d.%03d`), `--memory <n>m`;
  Voreinstellung `store.DefaultCPU` und `store.DefaultMemory`.
- `Exec` bekommt die Umgebung über `--env-file <datei>`: die Datei liegt in
  `config.RunDir + "/env"` (tmpfs), Modus 0600, wird nach dem Start gelöscht. So steht
  `NGC_PARAMS` **nie** in `/proc/*/cmdline` und nie im Log.
- `Stop` nutzt `--time 10`, danach `kill`.

> **Berichtigt am 01.10.2026.** Von diesen zwei Sätzen — der Env-Datei, die
> „nach dem Start gelöscht" wird, und `Stop` mit „danach `kill`" — weicht der
> Code der Aufgabe 11 ab, **mit Begründung an der Stelle selbst**
> (`apps/server-controller/classes/runtime/`). Der Strang hat beides gemessen
> und als Planfrage gemeldet; die Prüfung hat angemerkt, dass es in keiner
> Abweichungsliste stand.
>
> **Wer hier baut, liest die Begründung im Code, nicht diese zwei Sätze.** Ich
> schreibe den richtigen Wortlaut hier bewusst **nicht** hinein, weil ich ihn
> nicht gemessen habe — und ein Plansatz, den der Koordinator sich ausdenkt,
> wird vom Nächsten nicht mehr nachgeprüft.

- [ ] **Step 4: `runtime.ImageTag` schreiben**

Liest das Image-Tar mit `archive/tar`, sucht `manifest.json`, liest es mit
`enc.DecodeStrict(b, 1<<20, &m)` in `[]struct{ RepoTags []string }` und gibt genau dann einen Tag
zurück, wenn es genau einen Eintrag mit genau einem `RepoTags`-Wert gibt, der auf
`^[a-z0-9][a-z0-9._/-]{0,127}:[A-Za-z0-9][A-Za-z0-9._-]{0,63}$` passt. Alles andere ist ein
Fehler. Damit muss nie eine Ausgabe von Podman geparst werden.

- [ ] **Step 5: Tests ausführen, sie müssen bestehen**

Ausführen: `go test ./apps/server-controller/classes/runtime/`
Erwartet: PASS (auf Windows überspringen sich die Podman-Tests).

- [ ] **Step 6: Fuzz-Test**

```go
func FuzzImageTag(f *testing.F) {
	f.Add([]byte("")) // kein gültiges tar
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = imageTagFrom(bytes.NewReader(b)) })
}
```

- [ ] **Step 7: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/runtime/
git commit -m "Run app containers through one engine interface on Podman"
```

---

### Task 12: Systembenutzer und verschlüsselter Datenordner

**Files:**
- Create: `apps/server-controller/classes/runtime/user.go`,
  `apps/server-controller/classes/runtime/user_test.go`,
  `apps/server-controller/classes/runtime/data.go`,
  `apps/server-controller/classes/runtime/data_test.go`

**Interfaces:**
- Produces: `runtime.UserName(app string) string`;
  `runtime.EnsureUser(ctx context.Context, app string) (runtime.User, error)`;
  `runtime.User{Name string, UID, GID uint32}`;
  `runtime.DropUser(ctx context.Context, app string) error`;
  `runtime.EnsureData(ctx context.Context, app string, u User, key []byte) (string, error)`;
  `runtime.UnlockData(ctx context.Context, app string, key []byte) error`;
  `runtime.DropData(ctx context.Context, app string) error`;
  `runtime.DataSize(app string) (int64, error)`.

**Entscheidung (Entwurf 4.6 überlässt sie dem Plan):** Der Datenordner wird mit **fscrypt**
verschlüsselt, der Ordnerverschlüsselung des Linux-Kernels (AES-256-XTS), über die Ioctls
`FS_IOC_ADD_ENCRYPTION_KEY` und `FS_IOC_SET_ENCRYPTION_POLICY` direkt aus `syscall`; kein
zusätzliches Modul, kein Fremdprogramm. Der Schlüssel je App entsteht aus dem
`dbKey` des Controllers und der App-ID über HKDF-SHA256 (`crypto/hkdf` der Standardbibliothek),
liegt also nie auf der Platte. Kann das Dateisystem kein fscrypt, aktiviert der Controller keine
App mit Daten und meldet auf Deutsch: „Der Datenträger kann keine verschlüsselten Ordner
(fscrypt). Bitte ext4 oder f2fs mit aktivierter Verschlüsselung verwenden.“ Der zweite Weg aus dem
Entwurf (ein verschlüsselter Container je App) steht in den offenen Entscheidungen.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

```go
func TestUserNameIsStableAndShort(t *testing.T) {
	long := "org.example." + strings.Repeat("a", 80)
	n := UserName(long)
	if len(n) > 32 || !strings.HasPrefix(n, "ngc-") {
		t.Fatalf("name = %q (%d bytes)", n, len(n))
	}
	if n != UserName(long) {
		t.Fatal("UserName is not stable")
	}
	if UserName("org.example.a") == UserName("org.example.b") {
		t.Fatal("two apps share one user")
	}
	for _, r := range n {
		if !(r == '-' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			t.Fatalf("name = %q has %q", n, r)
		}
	}
}

func TestEnsureUserBuildsTheArgumentList(t *testing.T) {
	// useradd --system --no-create-home --shell /usr/sbin/nologin <name>,
	// danach die subuid/subgid-Bereiche; nirgends eine Shell.
}

func TestEnsureDataSetsAPolicy(t *testing.T) {
	// Nur Linux mit fscrypt (sonst t.Skip): der Ordner bekommt eine Richtlinie,
	// ein zweiter Aufruf ändert nichts, und DataSize zählt die Bytes.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/runtime/ -run User`
Erwartet: FAIL.

- [ ] **Step 3: `UserName` und `EnsureUser` schreiben**

`UserName`: `"ngc-" + label + "-" + hex(SHA-256(app))[:8]`, wobei `label` das letzte Stück der
App-ID ist, auf `a-z0-9` gekürzt und auf 18 Zeichen beschnitten. Ergebnis höchstens 31 Zeichen,
eindeutig durch den Hash.

`EnsureUser`: gibt es den Benutzer schon (`user.Lookup`), wird er genommen; sonst
`/usr/sbin/useradd` mit Argumentliste. Danach je eine Zeile in `/etc/subuid` und `/etc/subgid`
(65536 IDs je App, beginnend bei 524288 + 65536·n), atomar über eine temporäre Datei mit
`os.Rename`, damit ein Absturz keine halbe Zeile hinterlässt.

- [ ] **Step 4: `EnsureData` schreiben**

`/var/lib/ngc/apps/<app>` anlegen (0700, dem App-Benutzer gehörend), Schlüssel aus
`hkdf.Key(sha256.New, dbKey, []byte(app), "ngc/appdata/v1", 64)`, mit
`FS_IOC_ADD_ENCRYPTION_KEY` in den Schlüsselbund des Dateisystems legen und mit
`FS_IOC_SET_ENCRYPTION_POLICY` (v2, AES-256-XTS für Inhalte, AES-256-CTS für Namen) an den leeren
Ordner binden. Ist die Richtlinie schon gesetzt, wird nur der Schlüssel hinzugefügt
(`UnlockData`, nach jedem Neustart). `DropData` löscht den Ordner samt Inhalt und entfernt den
Schlüssel (`FS_IOC_REMOVE_ENCRYPTION_KEY`).

Alle Ioctl-Strukturen stehen in `data_linux.go`; `data_other.go` (Bau-Markierung `!linux`) gibt
einen Fehler „fscrypt gibt es nur unter Linux“ zurück, damit das Paket auch unter Windows baut.

- [ ] **Step 5: Tests ausführen, sie müssen bestehen**

Ausführen: `go test ./apps/server-controller/classes/runtime/`
Erwartet: PASS.

- [ ] **Step 6: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/runtime/
git commit -m "Give every app its own system user and encrypted data directory"
```

---

### Task 13: Dienst starten (Vertrag V3)

**Files:**
- Create: `apps/server-controller/classes/serve/call.go`,
  `apps/server-controller/classes/serve/call_test.go`

**Interfaces:**
- Consumes: `service.Call`, `service.Call.Env`, `service.Result`, `service.DefaultTimeout`,
  `service.MaxTimeout`, `service.MaxStderr`, `runtime.Engine.Exec`, `e2e.Session`.
- Produces: `serve.RunService(ctx context.Context, in serve.CallInput) wire.Close`;
  `serve.CallInput{Engine runtime.Engine, Container string, Command []string, Call service.Call,
  Timeout time.Duration, Stream bool, In io.Reader, Out io.Writer, Log *slog.Logger}`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

Der Test benutzt den Beispiel-Dienst `shared/testdata/service/echo.sh` und eine
`runtime.Engine`-Attrappe, die statt Podman das Skript lokal startet (nur Linux).

```go
func TestRunServicePassesTheEnvironmentAndEchoes(t *testing.T) {
	// echo.sh gibt stdin auf stdout zurück und meldet App, Dienst, Aktion und
	// Benutzer auf stderr; Ergebnis: wire.Close{Status: ok}.
}

func TestRunServiceReportsTheExitCode(t *testing.T) {
	// NGC_PARAMS {"exit":2}: wire.Close mit Code E_EXIT und ExitCode 2,
	// Meldung "Dienst mit Code 2 beendet."
}

func TestRunServiceTimesOut(t *testing.T) {
	// Ein Dienst, der nicht endet: nach dem Zeitlimit E_TIMEOUT, und der
	// Prozess läuft nicht weiter.
}

func TestParamsNeverReachTheLog(t *testing.T) {
	// Ein slog.Handler, der alles sammelt: NGC_PARAMS und sein Wert dürfen
	// darin nicht vorkommen, auch nicht in der Fehlermeldung.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/serve/ -run RunService`
Erwartet: FAIL.

- [ ] **Step 3: `serve.RunService` schreiben**

1. `in.Call.Validate()` – ohne das wird nichts gestartet (`service.ErrCall` → `E_VERIFY`).
2. Umgebung aus `in.Call.Env()` in die `--env-file`-Datei schreiben (Aufgabe 11), niemals als
   Argument.
3. Zeitlimit: `store.Action.Timeout`, sonst `service.DefaultTimeout`, höchstens
   `service.MaxTimeout`; bei `store.Action.Stream` kein Zeitlimit.
4. stderr in einen Ringpuffer von `service.MaxStderr` Byte, danach als eine Zeile ins Log
   (`slog.Info`, Feld `stderr`), nie mit Parametern.
5. Ende: `service.Result(exitCode, timedOut, startErr)` – das ist das CLOSE bzw. bei
   Ende-zu-Ende-Kanälen der Inhalt des `wire.E2EEnd`.
6. Läuft die Eingabe vorzeitig aus (EOF oder CLOSE des Kanals vor dem Ende der Shell), wird der
   Kontext abgebrochen, der Dienst beendet und ein Fehler gemeldet – abgeschnittene Eingabe gilt
   nie als Erfolg.

- [ ] **Step 4: Tests ausführen, sie müssen bestehen**

Ausführen: `go test ./apps/server-controller/classes/serve/`
Erwartet: PASS.

- [ ] **Step 5: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/serve/
git commit -m "Run a service program in its container per contract V3"
```

---

### Task 14: App-Kanäle von Anfang bis Ende

**Files:**
- Modify: `apps/server-controller/classes/serve/serve.go`
- Create: `apps/server-controller/classes/serve/app.go`,
  `apps/server-controller/classes/serve/app_test.go`

**Interfaces:**
- Produces: `(*Server).openApp(ctx, ch *link.Channel, d access.Decision) error` (intern),
  vollständig verdrahtet über `Open`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

```go
func TestAppChannelRunsOnlyAfterTheFirstInnerMessage(t *testing.T) {
	// Der Dienst darf erst starten, wenn die erste innere Nachricht der Shell
	// entschlüsselt ist: eine Attrappe zählt die Starts.
}

func TestReplayedOpenStartsNothing(t *testing.T) {
	// Dasselbe OPEN ein zweites Mal, ohne dass die Shell eine innere Nachricht
	// schickt: nach wire.E2EFirstTimeout endet der Kanal, der Dienst lief nie.
}

func TestCutInputIsAnError(t *testing.T) {
	// EOF des Kanals vor dem E2EEnd der Shell: Ende des Controllers mit Fehler.
}

func TestResultAboveMaxCallResultEndsWithTooLarge(t *testing.T) {
	// Aktion ohne stream, Dienst schreibt mehr als wire.MaxCallResult:
	// E2EEnd mit E_TOOLARGE, und nichts darüber hinaus geht hinaus.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/serve/ -run App`
Erwartet: FAIL.

- [ ] **Step 3: `openApp` schreiben**

Ablauf: `e2e.Accept` → erste GRANT mit Nachricht 2 → auf die erste innere Nachricht warten
(`wire.E2EFirstTimeout`) → Lizenz prüfen (Aufgabe 17; abgelaufen: Ende mit `E_LICENSE`) →
Container sicherstellen (läuft er nicht, starten) → `serve.RunService` mit
`service.Call{User: d.User, UserName: d.UserName, App: …, Service: …, Action: …,
Version: d.App.Version, Params: d.Params(), Start: false}` → Ausgabe als `wire.E2EData`,
Ergebnis als `wire.E2EEnd` mit `service.Result` → danach nichts mehr → CLOSE.

`d.Params()` ist bei einer Aufgabe deren `Params`, sonst `o.Open.Params` – genau so, wie der
Prolog es gebunden hat.

- [ ] **Step 4: Tests ausführen, sie müssen bestehen**

Ausführen: `go test ./apps/server-controller/classes/serve/`
Erwartet: PASS.

- [ ] **Step 5: Prüfen und committen**

```bash
tools/check.sh
go test -race ./apps/server-controller/...   # auf Linux
git add apps/server-controller/classes/serve/
git commit -m "Serve app channels end to end and start the service"
```

---

## Etappe 5 – Apps (Aufgaben 15 bis 18)

### Task 15: Paket annehmen und prüfen

**Files:**
- Create: `apps/server-controller/classes/install/package.go`,
  `apps/server-controller/classes/install/package_test.go`

**Interfaces:**
- Consumes: `core.ReadTransfer`, `core.Transfer`, `store.VerifyCatalog`, `store.VerifyRelease`,
  `store.NewDecrypter`, `store.Walk`, `store.PackageUI`, `store.SupportsMinNGC`,
  `store.MaxDirs`, `store.Granted`.
- Produces: `install.Accept(ctx context.Context, in install.AcceptInput) (install.Package, error)`;
  `install.AcceptInput{Reader io.Reader, Max int64, List *trust.List, Store string, App string,
  Version string, SHA256 []byte, UI []byte, NGC string, Dir string, LastSeq func(store string) uint64}`;
  `install.Package{Dir string, Manifest *store.Manifest, UI []byte, Transfer core.Transfer,
  Signer []byte, Seq uint64}`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

```go
func TestAcceptChecksChecksumAndUIHash(t *testing.T) {
	// Paket aus shared/testdata/packui.json: Accept liefert Manifest und UI-Hash;
	// ein Paket, dessen ui-tar einen anderen Hash ergibt, wird abgelehnt.
}

func TestAcceptRefusesUnsafePaths(t *testing.T) {
	// tar mit "../x", "/etc/passwd", einem Symlink und einer Sparse-Datei:
	// jedes Mal store.ErrPackage, und im Zielordner liegt nichts.
}

func TestAcceptRefusesTooManyDirectories(t *testing.T) {
	// store.MaxDirs + 1 Verzeichnisse: abgelehnt.
}

func TestAcceptDecryptsAPaidApp(t *testing.T) {
	// Verschlüsseltes Paket mit dem Inhaltsschlüssel aus core.Transfer.Key:
	// Accept entschlüsselt; mit einem falschen Schlüssel store.ErrEncrypted.
}

func TestAcceptRefusesTooNewMinNGC(t *testing.T) {
	// minNgc 2.0.0 gegen NGC 1.0.0: abgelehnt; 0.0.0-dev gilt als älter als
	// jedes minNgc.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/install/`
Erwartet: FAIL.

- [ ] **Step 3: `install.Accept` schreiben**

Reihenfolge – nichts wird geschrieben, bevor das darüber stimmt:

1. `core.ReadTransfer(r)` – der Kopf kommt immer von der API, auch bei `activate`.
2. Genau eine Quelle: `Catalog` mit `Store`, `Release`, oder keins (hochgeladene App).
   - Katalog: `store.VerifyCatalog(env, st, lastSeq)` mit dem Store aus der **eigenen**
     Vertrauensliste; die App und die Version müssen darin stehen. `Signer` ist der Schlüssel des
     Stores, `Seq` die Katalognummer.
   - Release: `store.VerifyRelease(env, cfg.ReleaseKey, lastSeq)` – nur mit dem **eigenen**
     Release-Schlüssel.
3. `store.SupportsMinNGC(ngc, pkg.MinNGC)`.
4. Die Datei in einen frischen Ordner unter `config.StageDir` schreiben, dabei SHA-256 und Größe
   mitrechnen und mit `Transfer.SHA256`/`Size` und den Werten aus dem geprüften Katalog- bzw.
   Release-Eintrag vergleichen. Stimmt etwas nicht, wird der Ordner gelöscht.
5. Kauf-App: `store.NewDecrypter(f, tr.Key)` – der Inhaltsschlüssel kommt nie ins Log.
6. `store.PackageUI(plain, &buf)` über das **unverschlüsselte** Paket: der SHA-256 muss zu
   `in.UI` passen (aus `core.ActivateParams.UI`) und, bei Store-Apps, zu `ui` des
   Katalogeintrags; sonst Fehler, den der Aufrufer auf `E_VERIFY` abbildet.
7. `store.Walk` schreibt die Dateien in den Zielordner: nur `ui/…` und `services/<arch>.tar`,
   keine absoluten Pfade, kein `..`, keine Links, keine Sparse-Dateien, höchstens
   `store.MaxDirs` Verzeichnisse – das prüft `store.Walk` selbst; der Controller legt nur an, was
   `Walk` durchlässt, und löscht bei jedem Fehler alles Geschriebene wieder (Walk prüft das Paket
   erst nach der letzten Datei).

- [ ] **Step 4: Tests ausführen, sie müssen bestehen**

Ausführen: `go test ./apps/server-controller/classes/install/`
Erwartet: PASS.

- [ ] **Step 5: Fuzz-Test**

```go
func FuzzAcceptHeader(f *testing.F) {
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = core.ReadTransfer(bytes.NewReader(b)) })
}

func FuzzAcceptPackage(f *testing.F) {
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = store.Walk(bytes.NewReader(b), func(*store.Manifest, store.PackFile, io.Reader) error { return nil })
	})
}
```

- [ ] **Step 6: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/install/
git commit -m "Accept and verify an app package before anything is unpacked"
```

---

### Task 16: App aktivieren

**Files:**
- Create: `apps/server-controller/classes/install/activate.go`,
  `apps/server-controller/classes/install/activate_test.go`

**Interfaces:**
- Consumes: `install.Accept`, `runtime.EnsureUser`, `runtime.EnsureData`, `runtime.Engine`,
  `core.ActivateParams`, `store.Granted`, `dbapi.App`.
- Produces: `(*install.Manager).Activate(ctx context.Context, d access.Decision, r io.Reader) ([]byte, error)`
  (Ergebnis: `{}` als JSON); `install.New(deps install.Deps) *Manager`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

```go
func TestActivateGrantsMustEqualNeeds(t *testing.T) {
	// Manifest mit needs ["network"], grants ["network"] → gut;
	// grants [] oder ["network","root"] → E_FORBIDDEN.
}

func TestActivateRefusesServicesWithoutRoleServices(t *testing.T) {
	// Manifest mit Diensten auf einem Server ohne Rolle services: E_FORBIDDEN.
}

func TestActivateRefusesOlderVersion(t *testing.T) {
	// Installiert 1.2.0, activate mit 1.1.0: E_FORBIDDEN; 1.2.0 und 1.3.0 gehen
	// und behalten die Daten.
}

func TestActivateRefusesWrongUIHash(t *testing.T) {
	// ActivateParams.UI passt nicht zum Paket: E_VERIFY, nichts wird eingerichtet.
}

func TestActivateWritesTheRowWithItsGrants(t *testing.T) {
	// dbapi.App bekommt Store, Node, Manifest, SHA256 und Grants aus der Aufgabe.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/install/ -run Activate`
Erwartet: FAIL.

- [ ] **Step 3: `Activate` schreiben**

1. Parameter aus der **geprüften Aufgabe** (`core.ParseParams` über `d.Task.Params`), nie aus dem
   OPEN.
2. `install.Accept` mit `App`, `Version`, `SHA256`, `UI` und dem Store aus den Parametern.
3. `grants` muss als Menge genau `manifest.Needs` sein, sonst `E_FORBIDDEN`.
4. Dienste nur mit Rolle `services` (`slices.Contains(cfg.Roles, tunnel.RoleServices)`).
5. Ist die App installiert: dieselbe oder eine neuere Version ersetzt sie samt Rechten und behält
   die Daten (`store.Newer`), eine ältere gibt `E_FORBIDDEN`.
6. Einrichten in dieser Reihenfolge, jeder Schritt umkehrbar: Systembenutzer → Datenordner →
   Image laden (`runtime.ImageTag`, `runtime.Engine.LoadImage` der Datei
   `services/<GOARCH>.tar`) → alten Container entfernen → neuen anlegen und starten →
   `dbapi.App` schreiben → Startaktion ausführen (Aufgabe 18). Scheitert ein Schritt, werden die
   vorigen zurückgenommen und die alte Zeile bleibt.
7. Kauf-App: Lizenz aus `core.Transfer.License` mit `store.VerifyLicenseFor` prüfen und behalten
   (Aufgabe 17), bevor der Container startet; eine abgelaufene Lizenz gibt `E_LICENSE`.

- [ ] **Step 4: Tests ausführen, sie müssen bestehen**

Ausführen: `go test ./apps/server-controller/classes/install/`
Erwartet: PASS.

- [ ] **Step 5: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/install/
git commit -m "Activate an app: user, data, image, container, row"
```

---

### Task 17: Deaktivieren, Deinstallieren, Daten löschen

**Files:**
- Create: `apps/server-controller/classes/install/remove.go`,
  `apps/server-controller/classes/install/remove_test.go`

**Interfaces:**
- Produces: `(*Manager).Deactivate(ctx, d access.Decision) ([]byte, error)`;
  `(*Manager).Uninstall(ctx, d access.Decision) ([]byte, error)`;
  `(*Manager).DeleteData(ctx, d access.Decision) ([]byte, error)`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

```go
func TestDeactivateStopsButKeepsEverything(t *testing.T) {
	// Container gestoppt, dbapi.App.Active = false, Daten und Image bleiben,
	// offene Kanäle zu der App werden geschlossen.
}

func TestUninstallKeepData(t *testing.T) {
	// Container, Image und alle Versionen weg; der Datenordner bleibt,
	// dbapi.App.Version ist leer und KeptData nennt die Größe.
}

func TestUninstallDeleteData(t *testing.T) {
	// Mit KeepData = false ist auch der Datenordner weg und die Zeile gelöscht.
}

func TestDeleteDataRemovesKeptData(t *testing.T) {
	// Aufgehobene Daten einer deinstallierten App werden gelöscht.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/install/ -run Remove`
Erwartet: FAIL.

- [ ] **Step 3: Schreiben**

`Deactivate`: Container stoppen, `Active = false`, offene Kanäle der App mit
`wire.Error(wire.CodeForbidden)` schließen. Gelöscht wird nichts.

`Uninstall`: Container stoppen und entfernen, Image entfernen, Paketordner löschen; mit
`KeepData` bleibt der Datenordner, die Zeile behält `KeptData` (`runtime.DataSize`) und eine leere
`Version`; ohne `KeepData` werden Datenordner (`runtime.DropData`), Systembenutzer
(`runtime.DropUser`) und Zeile entfernt. Die Lizenz bleibt in jedem Fall erhalten.

`DeleteData`: nur für eine App, die nicht mehr läuft; löscht die aufgehobenen Daten und die Zeile.

- [ ] **Step 4: Tests ausführen, sie müssen bestehen**

Ausführen: `go test ./apps/server-controller/classes/install/`
Erwartet: PASS.

- [ ] **Step 5: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/install/
git commit -m "Deactivate, uninstall and delete the data of an app"
```

---

### Task 18: Lizenzen, Startaktionen und Wiederanlauf ohne API

**Files:**
- Create: `apps/server-controller/classes/license/license.go`,
  `apps/server-controller/classes/license/license_test.go`,
  `apps/server-controller/classes/node/restore.go`,
  `apps/server-controller/classes/node/restore_test.go`

**Interfaces:**
- Consumes: `store.VerifyLicenseFor`, `store.License.State`, `core.LicenseResult`,
  `core.LicenseOf`, `tunnel.StateUnlicensed`, `store.Manifest.Start`.
- Produces: `license.Keep(ctx context.Context, st *state.Store, l *trust.List, installation, app string,
  storeID string, env sign.Envelope, now int64) (core.LicenseResult, error)`;
  `license.Allowed(ctx context.Context, st *state.Store, app dbapi.App, now int64) (bool, error)`;
  `node.Restore(ctx context.Context) error`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

```go
func TestKeepTakesOnlyTheNewestLicense(t *testing.T) {
	// Zwei Lizenzen: die mit dem größeren issued wird behalten; eine ältere oder
	// gleich alte ändert nichts, und die Antwort nennt den Zustand der behaltenen.
}

func TestKeepRefusesALicenseForAnotherInstallation(t *testing.T) {
	// store.VerifyLicenseFor schlägt fehl → Fehler, den der Aufrufer auf
	// E_VERIFY abbildet; die alte Lizenz bleibt.
}

func TestAllowedFollowsTheState(t *testing.T) {
	// valid → true; grace → true; expired → false.
}

func TestRestoreStartsContainersAndRunsStartActions(t *testing.T) {
	// Zwei aktive Apps im Zustand, eine davon ohne gültige Lizenz: die erste
	// startet samt Startaktion, die zweite nicht, und ihr Zustand ist
	// tunnel.StateUnlicensed.
}

func TestRestoreNeedsNoAPI(t *testing.T) {
	// Kein Tunnel, keine Vertrauensliste in Reichweite: Restore läuft trotzdem.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/license/ ./apps/server-controller/classes/node/`
Erwartet: FAIL.

- [ ] **Step 3: `license.Keep` und `license.Allowed` schreiben**

`Keep`: `store.VerifyLicenseFor(env, st, installation, app)` mit dem Store aus `dbapi.App.Store`,
wie ihn die eigene Vertrauensliste führt (ein anderer Store: Fehler). Nur eine Lizenz mit größerem
`Issued` ersetzt die gespeicherte. Antwort: `core.LicenseResult{App: app,
State: core.LicenseOf(kept, now)}` – immer der Zustand der **behaltenen** Lizenz.

`Allowed`: keine Lizenz nötig (Gratis-App) → wahr; sonst `store.License.State(now)`:
`LicenseExpired` → falsch. Der Aufrufer stoppt dann die Dienste, startet keine mehr, lehnt Kanäle
mit `E_LICENSE` ab und meldet `tunnel.StateUnlicensed`.

- [ ] **Step 4: `node.Restore` schreiben**

Beim Start, noch bevor ein Tunnel steht: Zustand öffnen → `UnlockData` je App →
`state.Apps` lesen → je aktive App mit gültiger Lizenz Container starten (wenn er nicht läuft) und
die Startaktion aus `Manifest.Start` mit `service.Call{Start: true, User: ""}` ausführen →
Ergebnis nur ins Log, nie an einen Benutzer gebunden. Apps ohne gültige Lizenz bleiben aus.
Fehler eines einzelnen Containers halten die übrigen nicht auf.

- [ ] **Step 5: Tests ausführen, sie müssen bestehen**

Ausführen: `go test ./apps/server-controller/classes/license/ ./apps/server-controller/classes/node/`
Erwartet: PASS.

- [ ] **Step 6: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/license/ apps/server-controller/classes/node/
git commit -m "Keep licenses and bring the apps back up without an API"
```

---

## Etappe 6 – Updates (Aufgaben 19 bis 21)

### Task 19: Updates bereitlegen

**Files:**
- Create: `apps/server-controller/classes/update/stage.go`,
  `apps/server-controller/classes/update/stage_test.go`

**Interfaces:**
- Consumes: `core.StageParams`, `core.ReadTransfer`, `store.VerifyRelease`, `store.VerifyCatalog`,
  `store.UpdateReport`, `config.StageDir`, `state.Staged`.
- Produces: `(*update.Manager).Stage(ctx context.Context, d access.Decision, r io.Reader) ([]byte, error)`;
  `update.New(deps update.Deps) *Manager`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

```go
func TestStageVerifiesTheReleaseAndRemembersWhatItSaw(t *testing.T) {
	// Release aus shared/testdata/release/: Stage prüft mit dem eigenen
	// Release-Schlüssel, legt die Datei nach config.StageDir und merkt sich
	// Version, SHA-256, Signierer und seq.
}

func TestStageRefusesAnOlderSeq(t *testing.T) {
	// seq kleiner als der zuletzt gesehene: store.ErrRollback, nichts liegt da.
}

func TestStageRefusesAWrongSignature(t *testing.T) {
	// Mit einem fremden Schlüssel signiert: abgelehnt, und der Report meldet
	// store.UpdateRejected.
}

func TestStageRefusesAWrongChecksum(t *testing.T) {
	// Datei passt nicht zum sha256 des Release-Eintrags: abgelehnt und gelöscht.
}

func TestStageIsWithoutUser(t *testing.T) {
	// Der Kanal kommt ohne Benutzer (core.WithoutUser); ein OPEN der Shell
	// dafür ist schon in access.Check E_FORBIDDEN.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/update/`
Erwartet: FAIL.

- [ ] **Step 3: `Stage` schreiben**

1. `core.ReadTransfer` – Kopf und Datei kommen von der API; `MaxIn` gilt.
2. Ziel aus `core.StageParams`; `store.ValidTarget`.
3. NGC-Teil (`trust.AutoWeb`, `AutoAPI`, `AutoController`): `store.VerifyRelease` mit
   `config.Controller.ReleaseKey` und der zuletzt gesehenen Nummer aus `state.Staged`; der Eintrag
   für Teil und Architektur liefert Version, Größe und SHA-256.
   App: `store.VerifyCatalog` mit dem Store aus `dbapi.App.Store` und der zuletzt gesehenen
   Katalognummer; der Eintrag der Version liefert Größe und SHA-256.
   `system`: keine Signatur, keine Version – hier wird nichts bereitgelegt, `apt` holt selbst
   (Aufgabe 21).
4. Datei nach `config.StageDir + "/" + target` schreiben (0600, root), dabei Größe und SHA-256
   mitrechnen und gegen den geprüften Eintrag halten; bei jedem Fehler löschen.
5. `state.PutStaged` mit `Signer` (dem Schlüssel, dessen Signatur geprüft wurde) und `Seq`.
6. Meldung `store.UpdateReport{Target, Version, State: store.UpdateDownloaded}` über den
   Steuerkanal; bei Fehlern `store.UpdateRejected` mit deutschem `Message`.

- [ ] **Step 4: Tests ausführen, sie müssen bestehen**

Ausführen: `go test ./apps/server-controller/classes/update/`
Erwartet: PASS.

- [ ] **Step 5: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/update/
git commit -m "Stage an update file only after its signature checks out"
```

---

### Task 20: Updates einspielen und zurücknehmen

**Files:**
- Create: `apps/server-controller/classes/update/install.go`,
  `apps/server-controller/classes/update/install_test.go`

**Interfaces:**
- Consumes: `core.InstallParams`, `core.Unattended`, `store.Approved`, `store.Granted`,
  `store.Newer`, `store.UpdateReport`, `config.BinDir`, `config.ServiceController`,
  `config.ServiceAPI`.
- Produces: `(*Manager).Install(ctx context.Context, d access.Decision) ([]byte, error)`;
  `(*Manager).Rollback(ctx context.Context, target string) error`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

```go
func TestInstallWithoutUserNeedsAStandingApproval(t *testing.T) {
	// Ohne Benutzer und ohne trust.Auto für Ziel und Schlüssel: E_FORBIDDEN.
	// Mit Dauerfreigabe und neuerer Version: eingespielt.
}

func TestInstallWithoutUserRefusesParamsThatDiffer(t *testing.T) {
	// version/sha256 im OPEN weichen vom bereitgelegten, geprüften Eintrag ab:
	// E_VERIFY – die Parameter eines Kanals ohne Benutzer sind nicht unterschrieben.
}

func TestInstallNeverGoesBackwards(t *testing.T) {
	// Installiert 1.3.0, bereitgelegt 1.2.0: abgelehnt, auch mit Unterschrift.
}

func TestInstallAppOnlyFromItsOwnStoreAndWithinItsGrants(t *testing.T) {
	// Anderer Store: E_VERIFY. Mehr needs als grants: E_FORBIDDEN, ohne Benutzer
	// Meldung store.UpdateNeedsGrants.
}

func TestRollbackAfterAFailedStart(t *testing.T) {
	// Das neue Programm startet nicht sauber: die vorige Datei kommt zurück,
	// der Dienst läuft wieder, Meldung store.UpdateRolledBack.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/update/ -run Install`
Erwartet: FAIL.

- [ ] **Step 3: `Install` schreiben**

1. Parameter: mit Aufgabe aus der geprüften Aufgabe; ohne Benutzer aus dem OPEN, aber nur, wenn
   sie mit dem geprüften `state.Staged` **genau** übereinstimmen (Version und SHA-256), sonst
   `E_VERIFY`.
2. Ohne Benutzer zusätzlich `core.Unattended(p, list, signer, installed)`; sein Ja heißt nur
   Dauerfreigabe und neuere Version. Store und Rechte einer App prüft der Controller daneben
   selbst: `dbapi.App.Store` (anderer Store → `E_VERIFY`) und `store.Granted(needs, app.Grants)`
   mit den Rechten aus der **eigenen** Datenbank (nicht erfüllt → `E_FORBIDDEN`, ohne Benutzer
   Meldung `store.UpdateNeedsGrants`).
3. Nie eine ältere Version als die installierte (`store.Newer`), auch nicht mit Unterschrift.
4. NGC-Teil: die alte Datei nach `<datei>.prev` verschieben, die neue an ihren Platz
   (`os.Rename`, gleiches Dateisystem, 0755, root), Dienst neu starten
   (`systemctl restart <config.ServiceController|ServiceAPI>` mit Argumentliste), danach
   Gesundheitsprüfung: `<datei> -version` muss die neue Version nennen und der Dienst nach
   30 Sekunden noch laufen. Scheitert das, `Rollback`: `.prev` zurück, Dienst neu starten,
   `store.UpdateRolledBack` melden.
   Für `ngc-controller` startet sich der Controller selbst neu; er meldet vorher
   `store.UpdateInstalling` und nach dem Start `store.UpdateInstalled`. Ein Wiederanlauf, der die
   neue Version nicht bestätigt, nimmt sie beim nächsten Start zurück (Merker in `state.Staged`).
   Für `ngc-web` wird der Inhalt nach `config.WebRoot` entpackt, der vorige Stand bleibt als
   `<WebRoot>.prev`.
5. App: `install.Accept` über die bereitgelegte Datei, dann wie beim Aktivieren einrichten, aber
   mit den Rechten auf die neuen `needs` zusammengestrichen; die Daten bleiben.
6. Jede Stufe meldet `store.UpdateReport`.

- [ ] **Step 4: Tests ausführen, sie müssen bestehen**

Ausführen: `go test ./apps/server-controller/classes/update/`
Erwartet: PASS.

- [ ] **Step 5: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/update/
git commit -m "Install staged updates and roll a failed one back"
```

---

### Task 21: Systempakete und Migrationen

**Files:**
- Create: `apps/server-controller/classes/update/system.go`,
  `apps/server-controller/classes/update/system_test.go`,
  `apps/server-controller/classes/update/migrate.go`,
  `apps/server-controller/classes/update/migrate_test.go`

**Interfaces:**
- Consumes: `trust.AutoSystem`, `store.Approved`, `config.Controller.Migrate`,
  `config.DB.TLSConfig`, `tunnel.RoleDB`.
- Produces: `(*Manager).System(ctx context.Context, d access.Decision) ([]byte, error)`;
  `(*Manager).Migrate(ctx context.Context, file string) error`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

```go
func TestSystemRunsAptWithAnArgumentList(t *testing.T) {
	// Attrappe für apt-get: die Argumente sind
	// ["-o","Dpkg::Options::=--force-confold","-y","--no-install-recommends","upgrade"]
	// und die Umgebung enthält DEBIAN_FRONTEND=noninteractive; nirgends eine Shell.
}

func TestSystemNeedsAStandingApprovalWithoutUser(t *testing.T) {
	// Ohne Benutzer und ohne trust.Auto für "system": E_FORBIDDEN.
	// Bei system gibt es keinen Versionsvergleich.
}

func TestMigrateOnlyWithRoleDB(t *testing.T) {
	// Ohne Rolle db oder ohne migrate in controller.json: Fehler, nichts läuft.
}

func TestMigrateUsesTLSToMariaDB(t *testing.T) {
	// config.DB.TLSConfig mit der CA aus migrate.ca; ohne CA ein Fehler,
	// nie eine Verbindung ohne Prüfung.
}

func TestInstallOfNgcApiOnADBNodeOnlyMigrates(t *testing.T) {
	// Rolle db ohne Rolle api: install mit target ngc-api spielt die Migrationen
	// ein und installiert ngc-api nicht (tunnel.Components sagt es nicht).
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/update/ -run "System|Migrate"`
Erwartet: FAIL.

- [ ] **Step 3: `System` schreiben**

`/usr/bin/apt-get` mit Argumentliste, `DEBIAN_FRONTEND=noninteractive` in der Umgebung, zuerst
`update`, dann `upgrade`. Ohne Benutzer nur mit Dauerfreigabe für `trust.AutoSystem`
(`store.Approved(list, trust.AutoSystem, nil)`); ein Versionsvergleich entfällt. Ausgabe auf
`service.MaxStderr` begrenzt ins Log, Ergebnis als `store.UpdateReport`.

- [ ] **Step 4: `Migrate` schreiben**

Nur mit `tunnel.RoleDB` in den Rollen und `config.Controller.Migrate != nil`. Die Migrationen
stecken in der Release-Datei `ngc-api`; der Controller entpackt sie in einen temporären Ordner
(`store`-Regeln: keine absoluten Pfade, kein `..`), verbindet sich mit dem Zugang aus `migrate`
(MariaDB über `config.DB.TLSConfig(host)` mit der CA aus `migrate.ca`, SQLite über die Datei und
den Schlüssel) und spielt sie in Nummernfolge ein. Auf einem Server mit Rolle `db`, aber ohne
Rolle `api`, wird `ngc-api` **nicht** installiert – `tunnel.Components(roles)` nennt es dort
nicht.

> Die Migrationen selbst schreibt die Sitzung Datenbank (`apps/server-db/sql/`). Der Controller
> führt sie nur aus und kennt ihr Format: nummerierte Dateien, aufsteigend, jede in einer
> Transaktion. Weicht das Format ab, ist das eine Frage an die Sitzung Datenbank.

- [ ] **Step 5: Tests ausführen, sie müssen bestehen**

Ausführen: `go test ./apps/server-controller/classes/update/`
Erwartet: PASS.

- [ ] **Step 6: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/update/
git commit -m "Update system packages and run migrations on a db node"
```

---

## Etappe 7 – Rolle web, Terminal, Installation (Aufgaben 22 bis 25)

### Task 22: Rolle `web` – Lastverteilung in Apache

**Files:**
- Create: `apps/server-controller/classes/apache/apache.go`,
  `apps/server-controller/classes/apache/apache_test.go`,
  `apps/server-controller/templates/apache-apis.conf.tmpl`,
  `apps/server-controller/classes/apache/testdata/apache-apis.conf`

**Interfaces:**
- Consumes: `trust.Node`, `trust.WebURL`, `trust.ValidWebURL`, `trust.TLSPin`,
  `tunnel.PinnedTLS`, `config.SaveWeb`, `config.APITrustFile`, `config.BalancerFile`.
- Produces: `apache.Writer` mit
  `New(w apache.Options) *Writer`,
  `Sync(ctx context.Context, nodes []trust.Node) (changed bool, err error)`,
  `Compare(ctx context.Context, node trust.Node) (changed bool, err error)`,
  `apache.Render(apis []config.WebAPI) []byte`;
  `apache.Options{WebConfig, TrustFile, BalancerFile string, Fetch func(ctx, node trust.Node) ([]byte, error), Reload func(ctx) error}`.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

```go
func TestRenderMatchesTheTemplate(t *testing.T) {
	got := Render([]config.WebAPI{
		{URL: "https://api-1.ngc.test:7443", TLS: pin1},
		{URL: "https://api-2.ngc.test:7443", TLS: pin2},
	})
	want, err := os.ReadFile("testdata/apache-apis.conf")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("balancer file differs:\n%s", got)
	}
}

func TestRenderRefusesAnAddressOutsideTheGrammar(t *testing.T) {
	// "https://api-1.ngc.test:7443/x" oder eine Adresse mit Zeilenumbruch:
	// Render gibt nil zurück, es wird nichts geschrieben.
}

func TestSyncWritesOnlyCertificatesThatMatchTheirPin(t *testing.T) {
	// Ein Zertifikat mit falschem Pin kommt nicht in api-trust.pem, und Apache
	// wird nicht neu geladen.
}

func TestSyncReloadsOnlyAfterConfigtest(t *testing.T) {
	// Schlägt apachectl configtest fehl, bleibt die vorige Datei stehen.
}

func TestCompareRewritesARenewedCertificate(t *testing.T) {
	// Gleicher Schlüssel, neues Zertifikat: die Datei wird neu geschrieben und
	// Apache neu geladen; ein anderer Schlüssel scheitert schon am Pin.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/apache/`
Erwartet: FAIL.

- [ ] **Step 3: Vorlage und `Render` schreiben**

`apps/server-controller/templates/apache-apis.conf.tmpl` (als `//go:embed` eingebunden), Form nach
`shared/ulh.md`, Regel „Lastverteilung in Apache“:

```apache
# Written by ngc-controller. Do not edit; changes are overwritten.
SSLProxyEngine on
SSLProxyVerify require
SSLProxyCACertificateFile /etc/ngc/tls/api-trust.pem
SSLProxyCheckPeerName on
SSLProxyCheckPeerExpire on
SSLProxyProtocol TLSv1.3
SSLProxyCipherSuite TLSv1.3 TLS_AES_256_GCM_SHA384
<Proxy balancer://ngc-api>
{{range .APIs}}    BalancerMember {{.URL}}
{{end}}    ProxySet lbmethod=byrequests
</Proxy>
ProxyPass /api/ balancer://ngc-api/api/
ProxyPassReverse /api/ balancer://ngc-api/api/
RewriteEngine on
RewriteCond %{HTTP:Upgrade} =websocket [NC]
RewriteRule ^/api/(.*)$ balancer://ngc-api/api/$1 [P,L]
```

`Render` setzt **nur** Adressen ein, die `trust.ValidWebURL` bestehen; alles andere gibt `nil`.
So kann nichts Ungeprüftes in Apaches Konfiguration geraten.

> Die endgültige Form legt der Plan des Webinterface fest und trägt sie in `shared/ulh.md` ein.
> Steht sie dort, wird diese Vorlage Zeile für Zeile daran angeglichen und `testdata/apache-apis.conf`
> neu erzeugt (letzter Schritt dieser Aufgabe).

- [ ] **Step 4: `Sync` und `Compare` schreiben**

`Sync` läuft, sobald sich die API-Server der Vertrauensliste ändern: je Knoten der Art `api`
`trust.WebURL(node.URL)` bilden, das Zertifikat über eine TLS-Verbindung mit
`tunnel.PinnedTLS(node.TLS)` holen (`Fetch`), alle passenden Zertifikate atomar nach
`config.APITrustFile` schreiben (0644), `web.json` mit `config.SaveWeb` (`apis` mit `url` und
`tls`), daraus `config.BalancerFile` mit `Render`, dann `apachectl configtest` und erst danach
`apachectl graceful`. Schlägt der Test fehl, wird die vorige Datei wiederhergestellt.

`Compare` läuft bei jedem Aufbau eines Tunnels und einmal am Tag: weicht das Zertifikat, das
`tunnel.PinnedTLS` bekommen hat, von dem in der Datei ab (derselbe Schlüssel – das beweist der
Pin), wird die Datei neu geschrieben und Apache neu geladen.

- [ ] **Step 5: Tests ausführen, sie müssen bestehen**

Ausführen: `go test ./apps/server-controller/classes/apache/`
Erwartet: PASS.

- [ ] **Step 6: Mit `shared/ulh.md` abgleichen**

Steht die Vorlage dort, die Datei angleichen, `testdata/apache-apis.conf` neu schreiben und die
Tests erneut laufen lassen. Fehlt sie noch, den Koordinator daran erinnern und die Aufgabe als
„vorläufige Form“ im Pull Request vermerken.

- [ ] **Step 7: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/apache/ apps/server-controller/templates/
git commit -m "Keep Apache's API balancer in step with the trust list"
```

---

### Task 23: Der Dienst „virtuelles Terminal“

**Files:**
- Create: `apps/server-controller/classes/terminal/terminal.go`,
  `apps/server-controller/classes/terminal/terminal_test.go`,
  `apps/server-controller/classes/terminal/pty_linux.go`,
  `apps/server-controller/classes/terminal/pty_other.go`,
  `apps/server-controller/templates/terminal/Containerfile`,
  `apps/server-controller/templates/terminal/manifest.json`
- Modify: `apps/server-controller/classes/node/main.go` (Kommandos `terminal`, `hold`),
  `apps/server-controller/CLAUDE.md` (Abschnitt „Terminal-App bauen“)

**Interfaces:**
- Produces: `terminal.Run(ctx context.Context, in io.Reader, out io.Writer) int`;
  `terminal.ParseFrame(b []byte) (terminal.Frame, error)`;
  `terminal.Frame{Kind byte, Data []byte, Cols, Rows uint16}`;
  `terminal.EncodeOutput(b []byte) []byte`.

**Datenprotokoll (App-Ebene, mit der Sitzung Webinterface abzustimmen):** Auf dem Kanal laufen
Rahmen: 1 Byte Art, dann die Nutzlast. `1` = Tastendrücke (Bytes an die Shell), `2` = Fenstergröße
(vier Byte: Spalten und Zeilen, je uint16, Big Endian). Der Dienst antwortet mit rohen Bytes der
Ausgabe, ohne Rahmen. Mehr braucht ein Terminal nicht, und alles Weitere wäre eine neue Aktion.

- [ ] **Step 1: Den fehlschlagenden Test schreiben**

```go
func TestRunEchoesThroughAPTY(t *testing.T) {
	// Nur Linux. "echo hallo\n" als Rahmen der Art 1 hinein: die Ausgabe enthält
	// "hallo"; exit 0 nach "exit\n".
}

func TestResizeIsAppliedNotEchoed(t *testing.T) {
	// Rahmen der Art 2 mit 80x24: die Shell sieht COLUMNS=80 (über "stty size"),
	// und die vier Bytes landen nicht in der Ausgabe.
}

func TestParseFrameRefusesGarbage(t *testing.T) {
	// Leerer Rahmen, unbekannte Art, Art 2 mit drei Bytes: Fehler.
}

func TestRunRefusesWithoutNGCData(t *testing.T) {
	// Ohne service.EnvData bricht Run mit einem Exit-Code ungleich 0 ab.
}
```

- [ ] **Step 2: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/terminal/`
Erwartet: FAIL.

- [ ] **Step 3: `terminal.Run` schreiben**

- Ein Pseudoterminal über `/dev/ptmx` (`syscall`: `TIOCGPTN`, `TIOCSPTLCK`, `TIOCSWINSZ`),
  `pty_linux.go`; `pty_other.go` (`!linux`) gibt einen Fehler zurück, damit das Paket überall baut.
- Startet `/bin/sh -i` **im Container**, mit Argumentliste, als der Benutzer, unter dem der
  Container läuft, mit Arbeitsverzeichnis `service.DataDir`.
- Umgebung: nur `TERM=xterm-256color`, `HOME`, `PATH` aus `service.Path`, `LANG=C.UTF-8`.
  `NGC_PARAMS` wird nicht weitergereicht.
- Eingabe: Rahmen lesen, Art 1 an das PTY, Art 2 als `TIOCSWINSZ`.
- Ausgabe: roh aus dem PTY, in Stücken bis `wire.MaxE2EPlaintext - 1` Byte.
- Ende: Exit-Code der Shell; `service.Result` bildet ihn ab.

- [ ] **Step 4: Manifest und Containerfile schreiben**

`templates/terminal/manifest.json`:

```json
{
  "id": "org.sorglos.terminal",
  "name": "Terminal",
  "version": "1.0.0",
  "publisher": "Sorglos Thomas Weirich",
  "description": "Befehle auf dem Server ausführen.",
  "ui": { "entry": "index.html", "width": 720, "height": 420 },
  "actions": [
    { "name": "shell", "title": "Befehle auf dem Server ausführen", "service": "terminal", "stream": true, "critical": true }
  ],
  "services": [
    { "name": "terminal", "command": ["/usr/lib/ngc/ngc-controller", "terminal"] }
  ],
  "runtime": "podman",
  "needs": ["root"],
  "cpu": 1000,
  "memory": 256
}
```

Ein Test prüft die Datei mit `store.ParseManifest`, damit sie nie zum Vertrag aus der Reihe fällt.

`templates/terminal/Containerfile` setzt auf `docker.io/library/debian:13-slim`, legt
`/usr/lib/ngc/` an und kopiert das statisch gebaute `ngc-controller` hinein. Der Bau läuft auf
einem Baurechner, nicht auf dem Zielserver.

- [ ] **Step 5: Den Bau in `CLAUDE.md` festhalten**

Abschnitt „Terminal-App bauen“ in `apps/server-controller/CLAUDE.md`, drei Zeilen:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-X main.Version=$(cat apps/server-controller/VERSION)" -o build/ngc-controller ./apps/server-controller
podman build --arch amd64 -f apps/server-controller/templates/terminal/Containerfile -t ngc/terminal:1.0.0 build/
podman save --format docker-archive -o services/amd64.tar ngc/terminal:1.0.0
```

Dasselbe für `arm64`. Das Paket baut danach die Sitzung API mit `ngc-store pack` aus Manifest,
`ui/` (Sitzung Webinterface) und den beiden Image-Tars.

- [ ] **Step 6: Fuzz-Test und Ausführen**

```go
func FuzzParseFrame(f *testing.F) {
	f.Add([]byte{1, 'a'})
	f.Add([]byte{2, 0, 80, 0, 24})
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = ParseFrame(b) })
}
```

Ausführen: `go test ./apps/server-controller/classes/terminal/` und
`go test -run Fuzz -fuzz FuzzParseFrame -fuzztime 30s ./apps/server-controller/classes/terminal/`

- [ ] **Step 7: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/terminal/ apps/server-controller/templates/terminal/ apps/server-controller/CLAUDE.md apps/server-controller/classes/node/
git commit -m "Add the virtual terminal service and its app package"
```

---

### Task 24: Verdrahtung, lokale SQLite und der Dauerbetrieb

**Files:**
- Create: `apps/server-controller/classes/state/sqlite.go`,
  `apps/server-controller/classes/state/sqlite_test.go`,
  `apps/server-controller/classes/node/run.go`,
  `apps/server-controller/classes/node/run_test.go`

**Interfaces:**
- Consumes: `apps/server-db/classes/sqlite` (Sitzung Datenbank), `dbapi/dbtest`.
- Produces: `state.Open(path string, key []byte) (*state.Store, error)`;
  `node.run(...)` aus Aufgabe 1 vollständig: Konfiguration laden, Zustand öffnen,
  `node.Restore`, je API ein `link.Keep`, Statusmeldungen alle 60 s, `Compare` der Rolle `web`
  täglich, `state.Purge` stündlich, sauberes Ende bei SIGTERM.

- [ ] **Step 1: Voraussetzung prüfen**

```bash
ls apps/server-db/classes/sqlite
```

Fehlt der Ordner, geht eine Nachricht an die Sitzung Datenbank („Der Controller braucht
`sqlite.Open(path string, key []byte) (dbapi.Store, error)`; passt die Signatur?“), und diese
Aufgabe wird zurückgestellt. Alle übrigen Aufgaben laufen weiter mit `memstore`.

- [ ] **Step 2: Den fehlschlagenden Test schreiben**

```go
func TestSQLitePassesTheContract(t *testing.T) {
	dbtest.Run(t, func(t *testing.T) dbapi.Store {
		dir := t.TempDir()
		st, err := sqlite.Open(filepath.Join(dir, "state.db"), bytes.Repeat([]byte{3}, 64))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		return st
	})
}

func TestOpenRefusesAShortKey(t *testing.T) {
	// AES-256-XTS braucht 64 Byte: alles andere ist ein Fehler.
}

func TestStateSurvivesAReopen(t *testing.T) {
	// App schreiben, schließen, wieder öffnen: die Zeile ist da.
}
```

- [ ] **Step 3: Test ausführen, er muss fehlschlagen**

Ausführen: `go test ./apps/server-controller/classes/state/`
Erwartet: FAIL.

- [ ] **Step 4: `state.Open` schreiben**

Genau eine Datei bindet die Umsetzung ein, damit ein Wechsel nur hier stattfindet:

```go
// Open opens the controller's own encrypted SQLite. It is the only place
// that names an implementation; everything else works on dbapi.Local, so a
// different store needs one change here.
func Open(path string, key []byte) (*Store, error) {
	if len(key) != 64 { // AES-256-XTS
		return nil, fmt.Errorf("state: the database key needs 64 bytes")
	}
	db, err := sqlite.Open(path, key)
	if err != nil {
		return nil, err
	}
	return New(db), nil
}
```

- [ ] **Step 5: `node.run` schreiben**

`config.LoadController` → `state.Open(cfg.DB, cfg.DBKey.Bytes())` → `chain.New(st, cfg.Anchor)` +
`Load` → `node.Restore` → je Eintrag in `cfg.APIs` (später: je Knoten der Art `api` der
Vertrauensliste) `go link.Keep(ctx, cfg)` → Schleife mit `time.Ticker`: Status alle 60 s,
`state.Purge` stündlich, mit Rolle `web` `apache.Compare` täglich und nach jedem Tunnelaufbau.
SIGTERM und SIGINT beenden den Kontext; laufende Kanäle bekommen 10 Sekunden, dann wird
geschlossen. Container laufen weiter – der Server soll ohne API und ohne Controller-Neustart
weiterarbeiten.

- [ ] **Step 6: Tests ausführen, sie müssen bestehen**

Ausführen: `go test ./apps/server-controller/...`
Erwartet: PASS. Auf Linux zusätzlich `go test -race ./apps/server-controller/...`.

- [ ] **Step 7: Prüfen und committen**

```bash
tools/check.sh
git add apps/server-controller/classes/state/ apps/server-controller/classes/node/
git commit -m "Open the encrypted local SQLite and run the controller for good"
```

---

### Task 25: ulh-Skript, Angriffstests und Abnahme

**Files:**
- Create (im **ulh-Repo**, Branch `ngc-controller`): `scripts/ngc-controller.sh`, Eintrag in
  `config.yaml`, Zeilen in `SCRIPTS.md` und `TREE.md`
- Create: `apps/server-controller/classes/serve/attack_test.go`
- Modify: `apps/server-controller/VERSION`

- [ ] **Step 1: Die Angriffstests schreiben**

`attack_test.go` fasst die Liste aus `apps/server-controller/CLAUDE.md` zusammen; jeder Fall ist
ein eigener Test und muss **scheitern lassen**, nicht durchlassen:

```go
func TestAttackForgedTask(t *testing.T)        // fremder Schlüssel: E_VERIFY
func TestAttackSwappedTask(t *testing.T)       // gültige Aufgabe für eine andere Aktion: E_VERIFY
func TestAttackMissingConfirmation(t *testing.T) // Stufe root ohne Bestätigung: E_CONFIRM
func TestAttackForeignControllerKey(t *testing.T) // Tunnel mit fremdem Noise-Schlüssel: kein Handschlag
func TestAttackContainerEscape(t *testing.T)   // Nur Linux: aus dem Container ist weder /etc/shadow
                                               // lesbar noch das Netz erreichbar
func TestAttackReplayedOpen(t *testing.T)      // dasselbe OPEN zweimal: nichts läuft zweimal
func TestAttackChangedParams(t *testing.T)     // ein Byte in params geändert: Handschlag scheitert
func TestAttackCutInput(t *testing.T)          // EOF vor dem Ende der Shell: Fehler, kein Erfolg
func TestAttackWrongUIHash(t *testing.T)       // untergeschobene Oberfläche: E_VERIFY
```

- [ ] **Step 2: Tests ausführen, sie müssen fehlschlagen, dann bestehen**

Ausführen: `go test ./apps/server-controller/classes/serve/ -run Attack`
Erwartet: erst FAIL (die Tests sind neu), nach dem Ausfüllen PASS – ohne eine Zeile im
Produktivcode zu ändern. Muss doch etwas geändert werden, ist das eine Lücke; sie wird geschlossen
und im Commit genannt.

- [ ] **Step 3: Das ulh-Skript schreiben**

Im ulh-Repo, Branch `ngc-controller`, nach `scripts/_template.sh` und ulhs `DOCS.md`.
`install`, `configure`, `uninstall`; Schlüssel in `answer.yaml`: `name`, `pair_code`, `roles`, mit
Rolle `db` zusätzlich `migrate_nodes`, `migrate_name`, `migrate_user`, `migrate_password`,
`migrate_ca`.

Das Skript muss:
- `LC_ALL=C` setzen und Hosts wie `shared/testdata/ulh/host.sh` prüfen;
- Programmdateien nur nach `verify.sh <Verzeichnis> ngc-controller <arch>` übernehmen und nie ein
  älteres Release als das in `/var/lib/ngc/release.seq` installieren;
- `ngc-controller` nach `/usr/lib/ngc/` legen, den Schlüssel vom Programm erzeugen lassen
  (`ngc-controller pair`), `controller.json` mit Modus 0600 und Eigentümer root schreiben;
- `ngc-controller.service` einrichten und starten;
- Podman aus den Paketquellen installieren, wenn der Server Dienste trägt (Rolle `services`);
- am Ende Fingerabdruck und Anker anzeigen, den Kopplungscode in kein Log schreiben;
- bei `uninstall` fragen, ob die Daten gelöscht oder unter `/var/lib/ngc/` aufgehoben werden.

Prüfen mit `shellcheck scripts/ngc-controller.sh` und einem Durchlauf in WSL (Debian) mit einer
`answer.yaml`. Pull Request nach `main` des ulh-Repos; nur Geprüftes, denn jede Installation holt
beim Start `main`.

- [ ] **Step 4: Alle Fuzz-Ziele laufen lassen**

```bash
for t in FuzzStaged FuzzApply FuzzChannelFrames FuzzOSRelease FuzzCheck FuzzInner \
         FuzzOpenCiphertext FuzzImageTag FuzzAcceptHeader FuzzAcceptPackage FuzzParseFrame; do
  go test -run "^$" -fuzz "$t" -fuzztime 3m ./apps/server-controller/... || exit 1
done
```

Erwartet: kein Fund. Funde kommen als `testdata/fuzz/` ins Repo und werden behoben.

- [ ] **Step 5: Abnahme**

```bash
tools/check.sh
go test -race ./apps/server-controller/...      # auf einem Linux-Server
govulncheck ./apps/server-controller/...
```

Dazu von Hand auf dem Server „Dienste“:
1. Eine App mit Unterschrift aktivieren.
2. Im Terminal `cp -r` eintippen und die Ausgabe sehen.
3. Den Server neu starten, während die API aus ist: die Apps laufen wieder.
4. Ein absichtlich kaputtes Update einspielen: es wird selbst zurückgenommen.

- [ ] **Step 6: Version setzen und committen**

`apps/server-controller/VERSION` auf `1.0.0`.

```bash
tools/check.sh
git add apps/server-controller/VERSION apps/server-controller/classes/serve/attack_test.go
git commit -m "Harden the controller against the attacks of the overall test"
```

Dann Pull Request des Branch `controller` nach `main`; der Koordinator prüft auf Vertrag und
Sicherheit. `CHANGELOG.md` im Stamm schreibt der Koordinator.

---

## Selbstprüfung

**1. Abdeckung gegen `apps/server-controller/CLAUDE.md`**

| Verlangt | Aufgabe |
|---|---|
| Tunnel zu jeder API, Wiederaufbau mit wachsenden Pausen | 5 |
| Kopplung mit Code, Anker, `trust-anchor.json` | 4 |
| Steuerkanal: hello, status, update, notify | 7 |
| Vertrauensliste selbst prüfen, Fassungen in SQLite, Kanal `ngc.users/controller/trust` | 3, 10 |
| Kanäle: `tunnel.VerifyOpen`, Benutzer nur aus dem Zertifikat, `core.WithoutUser` | 8 |
| Kanalzahl, Freigabe, `MaxIn`, `MaxCallResult` | 6, 10 |
| Ende zu Ende mit Prolog, erste Nachricht vor dem Start | 9, 14 |
| Dienste in Podman-Containern, eigener Benutzer, kein Netz, Grenzen, `/data` | 11, 12, 13 |
| Container nur über eine eigene Schnittstelle | 11 |
| Aktivieren, Deaktivieren, Deinstallieren, Daten löschen, UI-Prüfsumme, `grants` = `needs` | 15, 16, 17 |
| Lizenzen prüfen, behalten, durchsetzen | 18 |
| Updates: nur aus dem Katalog des eigenen Stores, `store.Granted` aus der eigenen Datenbank | 19, 20 |
| Dauerfreigaben, `core.Unattended`, nie eine ältere Version, Release-Schlüssel | 19, 20 |
| Migrationen mit Rolle `db` über `config.DB.TLSConfig` | 21 |
| `controller.json` nur über `config.Load…`/`Save…`, Geheimnisse nie ins Log | 4, 24, Global Constraints |
| Rolle `web`: `api-trust.pem`, `web.json`, Lastverteilung, täglicher Vergleich | 22 |
| Dienst „virtuelles Terminal“ | 23 |
| Startaktionen, Neustart ohne API | 18 |
| Installation über `ngc-controller.sh` | 25 |
| Tests gegen `shared/testdata/`, `echo.sh`, `dbtest` | 3, 9, 13, 24 |
| Angriffe: gefälschte/ausgetauschte Aufgabe, fehlende Bestätigung, fremder Schlüssel, Ausbruch, wiederholtes OPEN, geänderte Parameter, abgeschnittene Eingabe, untergeschobene Oberfläche | 25 |

**2. Platzhalter:** Keine Schritte mit „TBD“, „später“ oder „ähnlich wie Aufgabe N“. Wo eine
Entscheidung offen ist (SQLite-Signatur, Apache-Vorlage, Terminal-Protokoll), nennt der Plan die
Fassung, mit der gebaut wird, wer sie bestätigt und welche eine Datei sich dann ändert.

**3. Bezeichner:** `state.Store`, `state.Staged`, `chain.Keeper`, `pairing.Do`, `link.Config`,
`link.Channel`, `link.Handler`, `access.Check`, `access.Decision`, `e2e.Session`, `runtime.Engine`,
`runtime.Container`, `runtime.Exec`, `serve.Server`, `serve.RunService`, `install.Manager`,
`install.Package`, `license.Keep`, `update.Manager`, `apache.Writer`, `terminal.Run`,
`node.Main`, `node.Restore` – jeder Name wird in der Aufgabe erzeugt, die ihn einführt, und in
späteren Aufgaben unverändert benutzt.

**4. Widerspruch zwischen Entwurf und Verträgen:** einer gefunden, siehe unten (Container nach
einem Neustart).

---

## Offene Entscheidungen für den Koordinator

1. **Umsetzung der lokalen SQLite.** `apps/server-db/CLAUDE.md` sagt: „die SQLite-Umsetzung dient
   zugleich dem Controller als `dbapi.Local`“. Der Plan baut auf
   `sqlite.Open(path string, key []byte) (dbapi.Store, error)` in
   `apps/server-db/classes/sqlite`. Bitte die Signatur mit der Sitzung Datenbank festhalten; bis
   dahin arbeitet der Controller gegen `memstore` (Aufgabe 24 ist die einzige betroffene Datei).
2. **Vorlage der Apache-Lastverteilung.** `shared/ulh.md` verweist auf den Plan des Webinterface,
   die Vorlage fehlt dort noch. Der Controller schreibt vorläufig die Form aus Aufgabe 22; sobald
   sie in `shared/ulh.md` steht, wird sie Zeile für Zeile übernommen.
3. **Container nach einem Neustart – Widerspruch.** Entwurf 4.3 sagt „Nach einem Neustart des
   Servers startet systemd die Container selbst“; `apps/server-controller/CLAUDE.md` verlangt
   „Startaktion nach jedem Start, auch ohne API“ und die Verträge kennen keine systemd-Einheiten
   je App. Der Plan lässt den Controller die Container selbst starten (Aufgabe 18): ein Besitzer,
   keine erzeugten Unit-Dateien, weniger Code als root. Bitte bestätigen oder auf systemd
   umstellen.
4. **Verschlüsselter Datenordner.** Der Entwurf überlässt dem Plan die Wahl. Gewählt: fscrypt
   (Kernel, AES-256-XTS) über Ioctls aus `syscall`, Schlüssel je App aus dem `dbKey` per HKDF.
   Kann das Dateisystem es nicht, wird keine App mit Daten aktiviert. Alternative wäre ein
   LUKS2-Container je App – mehr Code im root-Programm. Bitte bestätigen.
5. **Datenprotokoll des Terminals.** Der Vertrag lässt es offen („regeln App und Dienst unter
   sich“). Der Plan legt fest: 1 Byte Art (1 = Eingabe, 2 = Fenstergröße), Ausgabe roh. Die
   Sitzung Webinterface muss dieselbe Form umsetzen; bitte im Entscheidungsprotokoll festhalten.
6. **Das Programm im Container am Leben halten.** Der Controller hängt `ngc-controller` schreibgeschützt
   als `/ngc/ngc-hold` in den Container und startet es mit dem Unterbefehl `hold`. Alternative:
   jedes App-Image muss selbst ein Programm mitbringen, das wartet – das wäre eine Anforderung an
   alle App-Entwickler und damit eine Vertragsänderung an `store.Manifest`.
7. **Version von `ngc-web` für `tunnel.Status.Components`.** Der Controller braucht sie auf dem
   Webserver. Der Plan liest sie aus `<config.WebRoot>/VERSION`. Bitte mit der Sitzung Webinterface
   abstimmen, dass ngc-web diese Datei ablegt.
8. **`tools/modules.go`.** Der Controller streicht in Aufgabe 5 die Zeile
   `github.com/coder/websocket`. Benutzt die Sitzung API das Modul zuerst, streicht sie es – bitte
   einmal klarstellen, wer zuerst liefert, damit die Zeile nicht zweimal verschwindet.
9. **Ordner für den Terminal-Dienst.** Der Plan macht ihn zum Unterbefehl von `ngc-controller`
   (`terminal`), weil der Projektstruktur-Standard je Teilprogramm nur eine Einstiegsdatei
   vorsieht. Alternative wäre ein eigenes Teilprogramm `apps/server-terminal/`.
