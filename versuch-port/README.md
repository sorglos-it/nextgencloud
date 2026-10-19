# Beleg, nicht Bestandteil

Dieser Ordner gehört **nicht** zu NGC. Er ist der Beweisversuch zu
`docs/vertraege-v1.2.md`, Punkt 68 („Fremde Programme nach NGC portieren"): ein fremdes
Rust-Programm von 272 101 Zeilen als NGC-App verpackt, um an gemessenen Zahlen zu zeigen,
was am Portieren geht und was nicht.

Nichts hier wird gebaut, geprüft oder ausgeliefert. `tools/check.sh` erreicht den Ordner
nicht: der Adapter ist ein **eigenes Go-Modul** (`wordcraft/adapter/go.mod`), und ein
Verzeichnis mit eigener `go.mod` fällt aus `./...` des Hauptmoduls heraus.

**Die App ist nicht zu veröffentlichen.** Sie trägt fremdes Programm unter unserer
Herausgeberzeile; das war für die Messung nötig und ist für einen Store falsch.

## Was drinsteht

`wordcraft/` ist eine vollständige NGC-App um `wordcraft-cli` aus
[storytold/wordcraft](https://github.com/storytold/wordcraft) (MIT oder Apache-2.0):

- `manifest.json` — drei Aktionen (`text`, `info`, `convert`), ein Dienst.
- `Containerfile` — `debian:13-slim`, per Digest festgenagelt. Der Digest ist **nicht hier
  gemessen**: es ist der, den die Terminal-App schon trägt und den `tools/pin-base.sh` am
  15.10.2026 gemessen hat.
- `adapter/` — 120 Zeilen Go, die NGCs Aufrufvertrag auf eine Argumentliste übersetzen.
  **Das ist der Kern des Befundes:** NGC übergibt die Aktion in `NGC_ACTION`, fremde
  Programme wollen sie als Argument, und nichts in NGC überbrückt das. Mit Tests, weil die
  Pfadprüfung darin sicherheitsrelevant ist.
- `ui/` — die kleinstmögliche Oberfläche, mit Absicht. Die Oberfläche ist der Teil, den NGC
  **nicht** portieren kann; sie steht hier nur, damit das Paket vollständig ist.

Nicht eingecheckt (`.gitignore`): `build/`, `services/`, das 40-MB-Binär und das 120-MB-Paket.

## Ergebnis

| Schritt | Ergebnis | Dauer |
|---|---|---|
| `wordcraft-cli` statisch (musl, amd64) | 41 986 968 Bytes | 4 min 05 s |
| Adapter (Go, statisch) | 2 846 880 Bytes | — |
| `podman build` | Abbild gebaut | 6,9 s |
| `podman save --format docker-archive` | 126 142 464 Bytes | 7,8 s |
| `ngc-store pack` | 126 150 656 Bytes, SHA-256 `9881f5f6…` | 1,75 s |
| `store.Walk` liest es zurück | angenommen: `Art=image Arch=amd64` | 0,09 s |

Gegenprobe, dieselbe Quelle als **Oberfläche**: Wordcrafts WebAssembly-Bau ist
**44 027 637 Bytes** gegen `MaxUIFile` 16 MiB. Vier Blocker, alle in Punkt 68 belegt.

## Nachstellen

Rust liegt auf diesem Rechner nicht; die Werkzeugkette steht in der Debian-WSL unter
`/opt/rust` (Punkt 67). podman ist dort vorhanden (5.4.2).

```sh
# 1. das fremde Programm, statisch
export RUSTUP_HOME=/opt/rust/rustup CARGO_HOME=/opt/rust/cargo PATH=/opt/rust/cargo/bin:$PATH
cargo build --release --target x86_64-unknown-linux-musl -p wordcraft-cli

# 2. der Adapter
cd versuch-port/wordcraft/adapter && go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../build/adapter .

# 3. das Abbild - mit podman von Hand, weil "ngc-controller image" nur das Terminal baut
cd versuch-port/wordcraft
podman build --arch amd64 --file Containerfile --tag ngc/portversuch:0.0.1 --pull=always build
podman save --format docker-archive --output services/amd64.tar ngc/portversuch:0.0.1

# 4. das Paket
go run ./apps/cli pack versuch-port/wordcraft portversuch.ngc
```

Schritt 1 braucht zusätzlich `LICENSE-MIT` und `LICENSE-APACHE` aus dem Wordcraft-Repo in
`build/` — der `Containerfile` legt sie mit ins Abbild, weil beide Lizenzen das verlangen.
