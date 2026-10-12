# Aufgabe 27: `ngc-store encrypt` und `ngc-store license`

Zweig `api-t27` (aus `api-t26`, `1bc5454`). Arbeitsbaum außerhalb von `Z:` (Pfadlänge),
`git` nur mit `-c safe.directory=<Baum>` (sonst Code 128 „dubious ownership"; eine Pipe
daneben hätte die 0 gemeldet). Alle Git-Zahlen hier sind so gemessen.

## Was gebaut wurde

| Datei | git hash-object |
|---|---|
| `apps/cli/classes/crypt.go` (neu) | siehe Commit |
| `apps/cli/classes/license.go` (neu) | siehe Commit |
| `apps/cli/classes/crypt_test.go` (neu) | siehe Commit |
| `apps/cli/classes/license_test.go` (neu) | siehe Commit |
| `apps/cli/classes/run.go` (geändert, Versand) | siehe Commit |
| `apps/cli/classes/run_test.go` (geändert) | siehe Commit |

- `NewContentKey(path)` – 32 Zufallsbytes, base64url, `O_EXCL|0600` in **einem** Schritt;
  ein vorhandener Schlüssel wird **nie** überschrieben (`ErrKeyExists`), sonst wäre jede
  schon verschlüsselte Kauf-App unlizenzierbar.
- `Encrypt(in, out, keyFile)` – `store.Encrypt` mit dem Schlüssel aus der Datei;
  Abbruch entfernt die Ausgabe wieder (Muster von `Pack`), `in == out` wird abgewiesen.
- `License(app, installation, keyFile, contentKeyFile, installKey, LicenseOpts)` –
  baut `store.License`, packt den Inhaltsschlüssel mit `store.WrapKey` hinein, prüft mit
  `License.Validate` und unterschreibt mit `sign.PurposeLicense`.
- `NewStoreKey(path)` – Ed25519-Saat, gleiche Sorgfalt wie `NewContentKey`.
- Versand in `run.go`: `encrypt` und `license` arbeiten, `catalog`/`release`/`check`
  melden weiter „noch nicht gebaut". `shared/` ist **nicht** angefasst.

**Die Uhr steht nur in `Run`.** `License` liest keine Zeit; `LicenseOpts.Issued` muss
gesetzt sein (`Validate` verlangt > 0). Damit ist die Bibliothek wiederholbar und die
Ausstellungszeit eine Angabe, keine Nebenwirkung.

**Entscheidung Wiederholbarkeit, im Code begründet:** Aufgabe 26 hält die Zeit im tar auf
**0**, damit eine Prüfsumme nicht vom Packzeitpunkt abhängt. Das **Verschlüsseln** hält
dieselbe Entscheidung **nicht** – und darf es nicht: `store.Encrypt` zieht je Lauf eine
frische Datei-Kennung, aus der die Chunk-Nonces kommen. Zweimal dieselbe Kennung unter
demselben Schlüssel wäre Nonce-Wiederverwendung in AES-GCM. Gemessen in
`TestEncryptIsNotReproducibleOnPurpose`: gleiche Länge, gleiches Magic, **verschiedene**
Datei-Kennung, verschiedene Bytes. Wiederholbar ist das **Klartextpaket** (Aufgabe 26);
der Katalog nennt die SHA-256 der Datei, die er ausliefert.

## Testergebnis

```
go test -count=1 ./apps/cli/...  -> ok, exit 0 (9,7 s)
bash tools/check.sh (ohne Pipe)  -> "check: ok", exit 0
git diff --name-only --diff-filter=D HEAD | wc -l -> 0 (im selben Aufruf, mit safe.directory)
```

## Je Abnahmekriterium (Plan, Aufgabe 27)

| Kriterium | Test | belegt |
|---|---|---|
| `NewContentKey` legt 32 Byte an, Modus 0600 | `TestEncryptRoundTrip` | teils: 0600 ist auf Windows **nicht** prüfbar, siehe Ungemessenes |
| `Encrypt` + `store.NewDecrypter` über 3 Chunks + 7 Byte, Klartext kommt gleich zurück | `TestEncryptRoundTrip` | ja |
| Vom Paket zeigt nichts durch (64 Byte Klartext im Chiffrat gesucht) | `TestEncryptRoundTrip` | ja |
| Ein anderer Schlüssel öffnet das Paket nicht | `TestDecryptWithAnotherKeyFails` (mit Zwischenprüfung) | ja |
| Lizenz wird von `store.VerifyLicense` gegen Store-Schlüssel, Installation und App angenommen | `TestLicenseVerifies` | ja |
| Lizenz für eine andere Installation/App wird abgelehnt | `TestALicenceForAnotherInstallationIsRefused`, `…ForAnotherAppIsRefused` | ja |
| `store.UnwrapKey` öffnet den Inhaltsschlüssel nur mit dem privaten Schlüssel der Installation | `TestLicenseCarriesTheContentKeyForThisInstallationOnly` | ja |
| Versand kennt alle sechs Namen, 2 bei falscher Form | `TestRunKnowsItsSubcommands`, `TestRunPrintsTheUsageForAWrongCall` | ja |

Zusätzlich gemessen: Abo-Grenzen von **beiden** Seiten (`TestLicensePerUserAndSubscription`:
`Expires-1` gültig, `Expires` Nachfrist, `Expires+Grace-1` Nachfrist, `Expires+Grace`
abgelaufen), frische Kennung und frische Umhüllung je Lauf, und dass die Unterschrift
einer Lizenz **nicht** als Katalog verifiziert.

## Was eine fremde oder manipulierte Lizenz nicht erreicht

Je Angriff eine **Zwischenprüfung** vorweg: genau diese Hülle wird mit den richtigen
Angaben angenommen (`verifyOK`). Damit misst kein Fall eine frühere Wand.

| Angriff | Ergebnis |
|---|---|
| andere Installation (API-Weg und Controller-Weg) | `store.ErrLicense` |
| andere App | `store.ErrLicense` |
| anderer Installationsschlüssel bei richtiger Installation | `store.ErrLicense` |
| fremder Store-Schlüssel gegen dieselbe Hülle | abgelehnt; die fremde Lizenz ist **in sich** gültig (gegengeprüft), die Ablehnung gilt also dem Schlüssel |
| Lizenz nennt einen anderen Store, unterschrieben vom richtigen Schlüssel | `store.ErrLicense` |
| ein Bit im `payload` gedreht | `sign.ErrBadSignature` |
| ein Bit in der `sig` gedreht | `sign.ErrBadSignature` |
| umhüllten Schlüssel gegen den einer anderen Installation getauscht | `sign.ErrBadSignature` (der Schlüssel steckt im unterschriebenen Teil); der Tausch wird im Test gegengeprüft, sonst hätte er stillschweigend nichts geändert |
| `UnwrapKey` mit fremdem privaten Schlüssel | `store.ErrEncrypted` |
| `UnwrapKey` unter einem anderen App-Namen | `store.ErrEncrypted` (die App ist AEAD-Zusatzdaten) |
| Form, die der Vertrag ablehnt: kein Store, keine Ausstellungszeit, unbekanntes `per`, `site` mit Nutzerzahl, `user` ohne, zu viele Nutzer, `expires` vor `issued`, Nachfrist zu lang/negativ, unerlaubte Kennung, keine App, unerlaubte App-Kennung, unerlaubte Installationskennung | alle `store.ErrLicense`, nichts wird unterschrieben |
| Installationsschlüssel leer, kein base64url, 31 oder 33 Byte | `ErrInstallKey` |
| Installationsschlüssel 32 Nullbytes | abgelehnt (X25519 selbst) |
| Schlüsseldatei fehlt / leer / kein base64url / zu kurz | `ErrKeyMissing` bzw. `ErrSignKey`, auch für die Inhaltsschlüsseldatei |

## Wie belegt ist, dass kein Geheimnis in Protokoll, Fehlermeldung oder Prozessliste gerät

**Drei Gestalten, und eine Kontrolle, die leben muss.** Gesucht wird in vier Gestalten:
Rohbytes, base64url, Hex klein, Hex groß (`keyShapes`). Bevor irgendetwas grün gemeldet
wird, baut `TestNoSecretReachesAMessage` **vier verbotene Meldungen** (`+ key`,
`+ base64url`, `%x`, `%X`) und verlangt, dass die Suche jede einzelne **findet**. Ohne
diese Kontrolle würde ein grüner Lauf nur beweisen, dass die Suche nicht funktioniert.

**Meldungen.** Jeder Weg durch `Run` für `encrypt` und `license` (Erfolg, fehlende
Schlüsseldatei, falsche Form, fehlendes Paket) und zusätzlich die Fehler der Funktionen
selbst werden gegen alle vier Gestalten geprüft — auch der Aufruf, der sich **selbst**
eine Schlüsseldatei anlegt: der Schlüssel wird danach aus der Datei **zurückgelesen** und
gegen die Ausgabe gehalten, damit die Prüfung die Bytes kennt, die wirklich entstanden
sind. Die Fehler selbst sind durchweg Sentinels (`ErrKeyFile`, `ErrKeyMissing`,
`ErrKeyExists`, `ErrSignKey`, `ErrInstallKey`), in die nichts hineinpasst; die Fehler von
`enc.B64` und von `os.ReadFile` werden **nicht** weitergegeben, weil schon die Position
des ersten schlechten Bytes etwas über den Inhalt sagt. Der Mutant M4 (Fehler des
Decoders mit dem Dateiinhalt weitergeben) **stirbt**, M15 und M16 (den neuen Schlüssel
bzw. die Saat drucken) **sterben** ebenfalls.

**Prozessliste — ohne Rennen.** Die bezahlte Falle ist die Stichprobe mit `ps` gegen
einen kurzlebigen Prozess. Hier ist es kein Rennen, weil das Kind **wartet**:
`TestNoSecretReachesTheProcessList` startet das Testprogramm als Kind
(`TestChildEncryptsWhenTheParentSaysSo`), das Kind blockiert auf einem Byte von der
Standardeingabe und ist dadurch für jeden Leseversuch am Leben. Der Vater liest die
Kommandozeile über das Betriebssystem (Windows: `Get-CimInstance Win32_Process`;
Linux: `/proc/<pid>/cmdline`) und schickt erst danach das Byte.

Drei Riegel in diesem einen Fall:
1. **Positive Kontrolle:** die gelesene Zeile muss die Marke `ngc-t27-marker` enthalten —
   sonst ist nicht belegt, dass die Messung Argumente überhaupt sieht, und der Fall sagt
   `LOUD SKIP, NO RESULT` statt grün zu werden.
2. **Der echte Aufruf** (`encrypt <Paket> <Schlüsseldatei>`) darf in keiner der vier
   Gestalten in der Zeile stehen.
3. **Der verbotene Aufruf** — derselbe Start, aber mit dem Schlüssel als zusätzlichem
   Argument — **muss gefunden werden**. Das ist die Kontrolle, die leben muss: ohne sie
   wäre Punkt 2 eine Aussage über eine Suche, die vielleicht nie etwas findet.

Gemessen am 12.10.2026 auf Windows 11, Lauf 1,6 s, kein Skip.

**Bauart statt Zusage:** Geheimnisse kommen nur aus Dateien. `encrypt` nimmt Paket und
Schlüsseldatei, `license` nimmt App, Installation, zwei **Dateipfade** und den
**öffentlichen** X25519-Schlüssel der Installation (darf im Aufruf stehen, und der
Hilfetext sagt genau das). Es gibt keinen Passphrase-Schalter: eine Passphrase auf der
Kommandozeile stünde in der Prozessliste, und eine, die nur über die Standardeingabe
käme, wäre ein zweiter Weg zum selben Schlüssel — die sicherere Variante ist eine Datei
mit 0600, die ngc-store selbst anlegt.

**Nicht gemessen:** die **Umgebung** des Prozesses (`/proc/<pid>/environ`) — auf Windows
nicht ohne Weiteres lesbar. Keine der beiden Dateien liest eine Umgebungsvariable
(`os.Getenv` kommt in `crypt.go` und `license.go` nicht vor); das ist gelesen, nicht
gemessen.

## Mutanten

Treiber: `scratchpad/t27/mutants.py`, eigener Unterordner dieses Strangs, `-overlay` mit
Windows-Pfaden (`C:/…`), je Mutant `go test -count=1`. Der Treiber prüft erst seine eigene
Vorbereitung — der Text muss **genau einmal** vorkommen, der Mutant muss `go vet`
überstehen — und meldet sonst `BROKEN`, nie „lebt". Dichtheitsprüfung vorweg: unveränderte
Quelle grün.

**17 von 17 wie gewollt, 0 Probleme** (16 gewollt tot, 1 Kontrolle gewollt lebendig).

| Mutant | gewollt | gemessen |
|---|---|---|
| M1 Schlüsseldatei darf überschrieben werden (`O_EXCL` → `O_TRUNC`) | tot | tot |
| M2 Schlüssellänge nicht geprüft | tot | tot |
| M3 keine Grenze für die Schlüsseldatei (`maxKeyFile` weg) | tot | tot |
| M4 Fehler des Decoders mit Dateiinhalt weitergegeben | tot | tot |
| M5 Ausgabe darf die Eingabe sein | tot | tot |
| M6 abgebrochener Lauf lässt die Datei liegen | tot | tot |
| M7 Puffer wird nie geleert | tot | tot |
| M8 Lizenz wird nicht validiert | tot | tot |
| M9 mit `PurposeCatalog` unterschrieben | tot | tot |
| M10 Umhüllung nicht an die App gebunden | tot | tot |
| M11 Länge des Installationsschlüssels nicht geprüft | tot | tot |
| M12 Lizenzkennung fest statt zufällig | tot | tot |
| M13 leerer Store wird der globale | tot | tot |
| M14 Länge der Saat nicht geprüft | tot | tot |
| M15 `encrypt` druckt den neuen Schlüssel | tot | tot |
| M16 `license` druckt die Saat | tot | tot |
| KONTROLLE lokale Variable umbenannt | lebt | lebt |

Zwei Mutanten haben in der ersten Runde einen Fehler der **Messung** gezeigt, nicht des
Codes, und beide sind behoben:

- **M3 lebte**, weil eine Riesendatei ohnehin kein gültiger Schlüssel ist. Erst der Fall
  „ein echter Schlüssel hinter einem Megabyte Leerzeichen" trennt die Fälle: mit Grenze
  abgewiesen, ohne Grenze würde `TrimSpace` daraus einen Schlüssel machen.
- **M15 lebte**, weil kein Fall den Zweig traf, der die Schlüsseldatei **selbst** anlegt.
  Jetzt gibt es ihn (Schlüssel danach aus der Datei zurückgelesen).

Zwei Mutanten sind **umgebaut** worden, weil sie am Übersetzen scheiterten und ein
Bauabbruch nichts beweist: M4 brauchte `fmt` (jetzt `errors.New`), M5 hätte `filepath`
unbenutzt gelassen (jetzt ein nie wahrer Vergleich statt einer entfernten Prüfung).

## Rot vorher

Plan-Schritt 2 (`undefined: Encrypt`) ist ein **Bauabbruch** und belegt nur, dass die
Namen neu sind; er ist nicht als Beleg geführt. Das echte Rot je Riegel sind die 16
Mutanten oben.

## Was mit Aufgabe 28 abgestimmt ist

Der Strang zu Aufgabe 28 (Zweig `api-t28`, aus derselben Basis) hat in `catalog.go`
`readSigningKey`, `CatalogFile`, `EncSuffix`, `PriceSuffix`, `ErrSeq` angelegt. Geprüft mit
`git show api-t28:apps/cli/classes/catalog.go | grep -nE "^(func|var|const|type)"`:

- **Keine Namenskollision** mit dieser Aufgabe. Meine Namen:
  `ErrKeyFile`, `ErrKeyMissing`, `ErrKeyExists`, `ErrSamePath`, `ErrSignKey`,
  `ErrInstallKey`, `maxKeyFile`, `NewContentKey`, `readContentKey`, `Encrypt`,
  `LicenseOpts`, `License`, `licenseIDSize`, `readStoreKey`, `NewStoreKey`,
  `runEncrypt`, `runLicense`, `writeEnvelope`; in den Tests `keyShapes`, `leaks`,
  `childEnv`, `commandLineOf`, `testID`, `licenseFixture`, `newLicenseFixture`,
  `parseLicencePayload`, `env2bytes`.
- **`encrypt` schreibt unter `<Paket>.ngc.enc`** — also `path + EncSuffix`, genau der
  Name, den `entryOf` von Aufgabe 28 sucht. Namentlich festgenagelt in
  `TestRunEncryptsAndCreatesItsKeyFile` (die Zeichenkette `app.ngc.enc` steht im Test, nicht
  nur eine Variable). **Achtung:** der Plan schreibt in Aufgabe 29
  `strings.TrimSuffix(paid, ".ngc")+".enc"`, also `app.enc` — das ist der **andere** Name.
  `Encrypt` nimmt den Ausgabepfad als Angabe, der Unterschied liegt nur in der Vorgabe des
  Versands; Aufgabe 29 sollte auf `in + ".enc"` umgestellt werden.
- **`readStoreKey` ist bewusst nicht `readSigningKey`** (zwei gleiche Namen in einem Paket
  übersetzen nicht, solange `api-t28` nicht zusammengeführt ist). Meine Fassung ist die
  strengere: Grenze `maxKeyFile`, Sentinel statt Fehler von `os.ReadFile`/`enc.B64`,
  gelesener Text wird überschrieben. **Beim Zusammenführen sollte eine der beiden
  bleiben, und zwar diese**; der Code sagt das an der Funktion selbst.
- **Konflikt in `run.go` und `run_test.go` ist zu erwarten** (beide Stränge fassen den
  Versand und die Liste der Unterbefehle an). Meine Änderung dort: der Zweig
  `case "encrypt"`/`case "license"`, zwei Funktionen (`runEncrypt`, `runLicense`), ein
  Helfer (`writeEnvelope`), der Hilfetext und die Importe. Die Liste „noch nicht gebaut"
  heißt bei mir `catalog`, `release`, `check`; bei Aufgabe 28 `encrypt`, `license`,
  `release`, `check` — nach dem Zusammenführen bleiben **`release`, `check`**.

## Ungemessenes

- **Modus 0600 ist auf diesem Rechner nicht prüfbar.** Gemessen am 12.10.2026 auf
  Windows 11: Go bildet einen Dateimodus nur auf das Nur-Lesen-Attribut ab, `os.Stat`
  meldet `-rw-rw-rw-`. Der Fall sagt es laut (`NOT MEASURED on windows`) statt still grün
  zu sein. `NewContentKey` und `NewStoreKey` verlangen `O_EXCL|0600` in einem Schritt;
  **dass die Bits auf einem Unix-Dateisystem ankommen, ist hier nicht gemessen** — auf
  einem Linux-Server nachziehen. Der Riegel „überschreibt keinen Schlüssel" ist gemessen
  (M1 tot).
- **`go test -race` ist nicht gelaufen:** `-race` braucht cgo, cgo braucht einen
  C-Übersetzer. Suche: `gcc`, `clang`, `cc`, `x86_64-w64-mingw32-gcc`, `tcc` — alle
  `MISSING`; WSL hat `gcc` (`/usr/bin/gcc`), aber **kein** `go` (`command -v go` leer).
  `Encrypt` und `License` starten keine Goroutine, gemessen ist das nicht.
- **Kein Fuzz-Ziel:** beide Funktionen lesen Dateien mit festem Format; die Parser dahinter
  (`store.NewDecrypter`, `store.UnwrapKey`, `enc.Budget`) haben ihre Ziele in `shared/`.
- **Die Umgebung des Prozesses** ist nicht gemessen (siehe oben).
- `apps/cli/VERSION` ist nicht angefasst (der Plan verlangt es hier nicht).
- Ein Nebenfund dieser Runde, behoben: `TestRunPrintsTheUsageForAWrongCall` hat mit
  relativen Pfaden gerufen und dadurch eine **Schlüsseldatei namens `c` im Arbeitsbaum**
  hinterlassen (mit einer Ed25519-Saat darin). Jetzt liegen alle Pfade dieses Falls in
  `t.TempDir()`. Die Datei ist gelöscht und nicht eingecheckt.

## BLOCKED

Keines. `shared/` ist nicht angefasst; alles Nötige war vorhanden (`store.Encrypt`,
`store.NewDecrypter`, `store.WrapKey`, `store.UnwrapKey`, `store.License`,
`store.VerifyLicense`, `store.VerifyLicenseFor`, `sign.Sign`, `sign.PurposeLicense`,
`trust.ValidID` über `License.Validate`).
