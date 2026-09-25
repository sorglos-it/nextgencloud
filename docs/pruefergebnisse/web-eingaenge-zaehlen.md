# Ergebnis: Eingänge mit lügender Länge (web, cb9fbde)

Gemessen auf dem Stand **vor** dem Wächter (`cb9fbde`), in einem temporären
Worktree, danach entfernt. Nichts committet, nichts gepusht, nichts repariert.

**Kein Code der Seite kann einen dieser Eingänge heute erreichen, nur Tests** —
gemessen: die Seite lädt `shell.css`, `shell/main.js` und `config.json`, und
`main.js` importiert nichts. Die Vertragsmodule rufen einander aber selbst auf:
`trust.open` und `trust.advance` geben ein Payload-Objekt des Aufrufers
unverändert an `parseList` weiter, `session.verifyAPI` gibt es an `sign.verify`
und dann an `decode`.

## Wie gemessen wurde

Ein echtes `Uint8Array` bekam ein eigenes `length` als Getter, der zählt und bei
Bedarf lügt. Indexzugriff, `TextDecoder` und `crypto.subtle` sehen dabei weiter
die echten Bytes. Jede Messung lief in **Node 22 und in Chromium** über
Playwright. Je Eingang drei Prüfungen: die gültige Eingabe wird angenommen, der
Getter zählt wirklich mit, die große Eingabe wird **ohne** Lüge abgelehnt.

Danach **146 Lügen-Muster**: eine einzelne Lesung (1–40), ein Anfang (1–40),
Paare bis 12, und alle Lesungen. Wo eine Unterschrift nötig war, mit eigenen
Schlüsseln, damit die große Eingabe **gültig unterschrieben** ist.

Die Zahl der Lesungen gilt für eine gültige Eingabe. Bei allem, was durch
`decode` läuft, wächst sie mit der Größe — im Angriff etwa so viele Lesungen wie
Bytes.

## Eingänge, die Bytes direkt als Parameter nehmen und eine Schranke haben (27)

| Eingang | Schranke | Lesungen | kommt durch |
| --- | --- | --- | --- |
| `json.decode` | `budget.size` (16 B) | 20 | ja, 70 027 B (Lüge bei Lesung 1) |
| `sign.parseEnvelope(b,1024)` | Budget **2 390 B** | 178 | ja, 70 027 B |
| `session.parseLoginChallenge` | 64 KiB | 565 | ja, 70 027 B |
| `session.parseLoginResult` | 64 KiB | 442 | ja, 5 000 000 B |
| `session.parseTrustChain` | 32 MiB | 3 142 | ja, 40 000 000 B |
| `session.parseTrustHint` | 64 B | 20 | ja, 70 027 B |
| `session.parseSetupOffer` | 128 KiB | 737 | ja, 140 000 B, **MAC stimmt** |
| `session.verifyAPI` – `client` | MAX_CERT 4 096 | 292 | ja, 5 000 B, **Zertifikat gültig unterschrieben** |
| `session.verifyApproval` – `payload` | MAX_CERT 4 096 | 294 | ja, 5 000 B, **Proof gültig** |
| `trust.parseList` | MAX_PAYLOAD 4 MiB | 2 624 | ja, 5 000 000 B |
| `trust.parseSigned` | Budget 5 658 346 B | 3 107 | ja, 6 000 000 B |
| **`noise.Transport.seal`** | MAX_PLAINTEXT 65 519 | **1** | **ja, 70 000 B werden zu 70 016 B Chiffrat** |
| **`noise.Transport.open`** | MAX_MESSAGE 65 535 | **1** | **ja, 70 016 B werden zu 70 000 B Klartext** |
| `proof.privateFromSeed` / `publicFromSeed` | == 32 B | 3 / 3 | Chromium nein (WebCrypto lehnt ab), Node ja (33 B) |
| `sign.verifyRaw` (`pubRaw`, `sig`) | == 32 / 64 B | 1 / 1 | nein |
| `noise.readMessage1` / `readMessage2` | == 48 B | 1 / 1 | nein |
| `task.parseTask` | 16 KiB | 0 | nein |
| `wire.decodeFrame`, `parseGrant`, `parseOpen`, `parseClose`, `parseE2E`, `e2e`, `parseControl`, `validParams` | je eigene | je 0 | nein |

**Offen 13, dicht 14** (in Chromium).

**Die Ursache ist nicht nur `json.decode`:** `noise.Transport.seal` und `open`
lesen die Länge **einmal** und geben die Bytes dann an WebCrypto weiter, das die
**echte** Größe nimmt.

## Bytes in Feldern von Aufrufer-Objekten: 40 gemessen, nicht vollständig

**Durchlässig: 27.**

Mit gültiger Unterschrift:

- `sign.verify` `env.payload`: 4 195 304 B über 4 MiB, **Unterschrift wahr**
- `session.verifyAPI` `env.payload`: 5 000 B über 4 096 B (Lüge bei Lesung 4;
  1–3 liest `sign.verify`)
- `trust.open` `chain[0].payload`: 5 MB
- `trust.advance` `chain[i].payload`: 5 MB
- `proof.verify` `p.clientData`: 70 027 B über 4 096 B
- `proof.verify` `p.authData`: 70 027 B über 4 096 B

Prüfer, die die Länge **zweimal** lesen (je 70 027 B bei Lüge auf Lesung 2):
`sign.validateEnvelope` `payload`; `proof.validateProof` `sig`, `authData`,
`clientData`; `session.validatePasskeyRef` `credId`;
`proof.validateUserKeySync` `credId`, `proof.validateUserKey` `credId`.

Prüfer mit **einer** Lesung (je 70 027 B bei Lüge auf Lesung 1):
`sign.validateEnvelope` `sig`; `session.validateClientCert` `nonce`, `sig`, `dh`;
`trust.validateList` `prev`, `trust.validAPINode` `sign`, `trust.validStore`
`key`; `proof.validateUserKeySync` `pub`, `proof.validateKDF` `salt`.

Schreibt oder rechnet Übergroßes:

- `session.encodeClientCert`: eine Nonce von 70 027 B ergibt ein Zertifikat von
  **93 583 B**
- `proof.deriveSeed` (Chromium): ein Salt von 17 B ergibt einen Seed

Nur gekürzt: `task.validateTask` `id` und `cert`, `task.encodeTask` `id` — nur
mit einer Lüge auf **allen** Lesungen täuschbar; dann wird der Anfang der Bytes
geprüft und geschrieben, nicht die übergroßen.

**Dicht (8):** `task.verifyApproval` `env.payload`, `wire.encodeFrame`,
`encodeControl`, `validateOpen` (`params`, `e2e`), `encodeOpen` `params`,
`task.validateTask` `params`, `task.buildTask` `params`.

## Die Gesamtzahl

**47** exportierte Funktionen oder Methoden nehmen Aufrufer-Bytes **direkt als
Parameter**. Davon haben **27** eine Schranke: **13 durchlässig, 14 dicht**. Die
übrigen 20 haben keine Schranke. Ihre Lesungen bei gültiger Eingabe:

- **0:** `keyID`, `certHash`, `tlsPin`, `e2ePrologue`, `fromUtf8`, `viewOf`,
  `grant`, `task.verifyApproval` (`client`)
- **1:** `sign.verify` (`pubRaw`)
- **2:** `message`, `offerMAC`, `setupMAC`, `signTask`, `HandshakeKK.of`,
  `challenge`, `proof.verify` (`payload`), `concat`
- **3:** `b64encode`
- **4:** `forTest`
- **6:** `equal`

Die Byte-**Felder** sind nicht vollständig gezählt und taugen deshalb **nicht**
als Maßstab für einen Wächter.

## Nicht geprüft

- **WebKit und Firefox:** nur Chromium 1194 installiert (Playwright 1.55.1
  erwartet 1193, per Link untergeschoben). Dass das Ergebnis vom Browser abhängen
  kann, zeigen die Seed-Funktionen: Node und Chromium antworten verschieden.
- Weitere Byte-Felder: `validateList` (Schlüssel, `noise`, `tls`, `auto.key`);
  `verifyAPI` `l.nodes[].sign`; `task.verifyApproval` `cert.sig`; `validateOpen`
  `task` und `confirm`; `encodeOpen` `e2e`, `taskBytes`, `confirmBytes`;
  `encodeClientCert` `sig` und `dh`; bei `e2ePrologue`, `HandshakeKK.of` und
  `forTest` die übrigen Byte-Parameter.
- `proof.verify` `p.sig` und `key.pub`: gezählt (je 1 Lesung), **nicht**
  angegriffen — dafür bräuchte es eine RSA-Unterschrift über 1 024 B.
- Andere Eigenschaften: gelogen hat nur `length`. `byteLength`, `buffer` und
  `byteOffset` blieben ehrlich (`viewOf` liest sie je einmal).

**„Dicht" ist kein Beweis** — es heißt nur, dass keines der 146 Muster durchkam.
