# Beleg, nicht Bestandteil

Dieser Ordner gehört **nicht** zu NGC. Er ist der Beweisversuch zu
`docs/vertraege-v1.2.md`, Punkt 67 („Rust für NGC"): **ein** kleiner Vertrag aus `shared/`,
ein zweites Mal in Rust geschrieben, um an einer gemessenen Zahl zu zeigen, was eine
zweite Umsetzung kostet und ob sie bitgleich herauskommt.

Nichts hier wird gebaut, geprüft oder ausgeliefert. `tools/check.sh` kennt den Ordner nicht,
und kein Teil von NGC bindet ihn ein.

## Was drinsteht

`sign/` ist `shared/sign` (136 Zeilen Go) als Rust-Kiste:

- `src/lib.rs` – der Vertrag: Envelope, Validate, ParseEnvelope, Message, KeyID, Sign, Verify.
- `src/main.rs` – der Beweis: liest `shared/testdata/sign.json` – dieselbe Datei, aus der
  heute Go und JavaScript prüfen – und rechnet 32 Fälle dagegen: Schlüssel aus dem Seed,
  Kennung, Nachrichtenbytes, Unterschrift, die ganze Vektordatei neu gebaut und Byte für
  Byte verglichen, dazu die komplette Ablehnungstabelle aus `shared/sign/sign_test.go`.
- `src/tiefe.rs` – eine einzelne Probe zu der Frage, die `sign` allein nicht beantwortet:
  nimmt `serde_json` ein doppeltes Mitglied **eine Ebene tiefer** an, das Go ablehnt?

## Ergebnis

```
32 OK, 0 FAIL
```

Gemessen am 20.10.2026 in Debian 13 unter WSL, rustc 1.99.1. Die Zahlen und was sie bedeuten:
Punkt 67.

## Nachstellen

Rust liegt auf diesem Rechner nicht; für den Versuch wurde eine Werkzeugkette nach
`/opt/rust` in der Debian-WSL gelegt (11,4 s, 613 MB; mit zwei Zielen 1,0 GB).

```sh
export RUSTUP_HOME=/opt/rust/rustup CARGO_HOME=/opt/rust/cargo PATH=/opt/rust/cargo/bin:$PATH
cd versuch-rust/sign
cargo build --release
./target/release/ngc-sign-versuch ../../shared/testdata/sign.json
```
