# Verträge

Hier stehen die Schnittstellen zwischen Webinterface, API, Controller und Datenbank. Sie gehören
dem Koordinator. Wer einen Vertrag ändern will, fragt ihn; alle vier Teile erfahren jede Änderung
sofort. Dieses Verzeichnis steht unter MIT (siehe `LICENSE`), NGC selbst unter AGPL-3.0.

| Vertrag | Inhalt | Pakete | Testdaten |
|---|---|---|---|
| Grundlagen | Kodierung und signierte Umschläge, für alle Verträge | `enc`, `sign` | `sign.json` |
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
- **Testdaten**: `contracts/testdata/` ist die gemeinsame Wahrheit. Go-Tests schreiben sie mit dem
  Schalter `-update`, und zwar gezielt mit `-run` auf den Test, der die Datei erzeugt (etwa
  `TestVectors`, `TestExamples`, `TestPackVectors`). `proof.json` wird nur einmal erzeugt und danach
  nur geprüft, weil die Passkey-Signaturen zufällig sind. Das Webinterface prüft sich in
  `tests/browser/` dagegen. Eine geänderte Datei ist eine Vertragsänderung.
- **Vorige Fassung verstehen**: Beim Update läuft kurz die vorige Version mit (`tunnel.Contract`).
  Felder kommen dazu, alte verschwinden erst eine Version später.
- **Sprache**: Code und Kommentare Englisch, Meldungen für Benutzer Deutsch (`wire.Code.Message`).

## Prüfen

```bash
go test ./contracts/...
scripts/check.sh
```
