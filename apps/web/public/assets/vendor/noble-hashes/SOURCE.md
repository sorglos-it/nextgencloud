# noble-hashes (von Hand kopiert)

Quelle: `@noble/hashes` **2.4.0**, MIT, <https://github.com/paulmillr/noble-hashes>.
Von Hand kopiert am 24.09.2026 aus der veröffentlichten Fassung der Registry
`registry.npmjs.org`. NGC installiert nichts mit npm: die Dateien liegen im Repo und werden
von uns gepflegt. Geändert wurde nichts – die Dateien stehen Byte für Byte so da, wie sie
veröffentlicht wurden.

Wofür: **Argon2id** für den Passwortschlüssel (`proof.KDF`, `proof.deriveSeed`). Die Web
Crypto API des Browsers kennt kein Argon2id – sie kann PBKDF2 und sonst nichts, was ein
Passwort streckt.

## Welche Dateien und warum genau diese

Kopiert ist der Importschluss von `argon2.js` und nichts darüber hinaus: jede Datei wird von
einer anderen wirklich gebraucht.

| Datei | Wofür | SHA-256 |
| --- | --- | --- |
| `argon2.js` | Argon2id selbst (RFC 9106) | `f39753324bdf2f484150731d24b1d71236f26bf10bedd594f7d190fcdfdefdc9` |
| `blake2.js` | BLAKE2b, die Hashfunktion in Argon2 | `03f00237bcc0b4d822a80f7c0ac22e42632c48e246c3b6da584e6452186e32f0` |
| `_blake.js` | Rundenkonstanten von BLAKE2 | `cabbff61d1f373bac45594ad758df7c42e176464ddbb94b44f5533fa977a4f56` |
| `_md.js` | Grundgerüst der Hashfunktionen | `60cf3010fda89e3e4d3f0e7ff1ce249e9c467f34b4fd41b6fd6101d9f69be763` |
| `_u64.js` | 64-Bit-Rechnen in zwei 32-Bit-Hälften | `b09da8c07fe8187c07649494cdb7cd0bcf13df90b506a9473d19e4d5f8c2e102` |
| `utils.js` | Byte- und Prüfhelfer | `037ad49adb78168b6b699598fd33f85f7877456b1d28b4d032e2a1a16807947c` |
| `LICENSE` | die MIT-Lizenz; sie verlangt, dass sie mitgeht | `4f221aee6e072336700c408c68ab3b96a3fc09f6aebe6f48f1bd99e5ef13faec` |

Nicht kopiert ist alles andere des Pakets, auch `sha2.js`: Es benutzt bisher niemand.
Wer später ein SHA-256 mit `update`/`digest` braucht (Dateien bis 4 GiB beim Hochladen),
holt `sha2.js` auf demselben Weg nach – es hängt an denselben `_md.js`, `_u64.js` und
`utils.js`, die hier schon liegen.

## Warum 2.4.0 und nicht 1.8.0

Der Auftrag nannte 1.8.0. Deren veröffentlichte ES-Module lassen sich im Browser **nicht
laden**: `esm/utils.js` beginnt mit `import { crypto } from '@noble/hashes/crypto'`, einem
Namen ohne Pfad, den ein Browser ohne Bündelwerkzeug oder Import-Map nicht auflösen kann.
Die Bibliothek schreibt das selbst in ihren Kommentar („Makes the utils un-importable in
browsers without a bundler"). NGC hat keinen Bauschritt, und in einem Modul-Worker – dort
läuft Argon2id – wirkt eine Import-Map gar nicht. Die Bibliothek ändern wir nicht, also ist
die Fassung zu nehmen, die als Ganzes lädt: 2.4.0 hat nur noch relative Importe und holt sich
den Zufallszahlengeber direkt über `globalThis.crypto`.

Der Go-Teil bleibt davon unberührt: dort rechnet `golang.org/x/crypto/argon2`. Beide Seiten
sind gegen dieselben Vektoren gemessen (`shared/testdata/proof_kdf.json`), nicht gegeneinander.

## Herkunft nachprüfen

Die Prüfsumme des Archivs stammt von der Registry selbst (Feld `dist.integrity`) und wurde
gerechnet, **bevor** eine Datei ausgepackt wurde:

    sha512-X5XaVWZIBCT7HHZGm5I7ZQXDwLG+bGXuSrMQAW+7Zvl87h1kmc1ZB1VSRJcpUfoUrGQp4Fkoxm5kZ+Ms+aW+eA==

So geht dieselbe Prüfung noch einmal:

```bash
curl -s https://registry.npmjs.org/@noble/hashes/2.4.0 > meta.json      # dist.tarball, dist.integrity
curl -sL https://registry.npmjs.org/@noble/hashes/-/hashes-2.4.0.tgz > hashes.tgz
openssl dgst -sha512 -binary hashes.tgz | openssl base64 -A             # muss dist.integrity sein
tar -xzf hashes.tgz                                                     # erst danach auspacken
sha256sum package/{argon2,blake2,_blake,_md,_u64,utils}.js package/LICENSE
```

## Aktualisieren

Dieselbe Prüfung noch einmal, Datei für Datei ersetzen, die Prüfsummen oben neu eintragen,
und `apps/web/tests` laufen lassen: `npx playwright test contracts/proof.spec.js`. Die drei
Vektoren aus `shared/testdata/proof_kdf.json` müssen weiter stimmen – sie kommen von Go und
sind der Maßstab, nicht die Bibliothek.
