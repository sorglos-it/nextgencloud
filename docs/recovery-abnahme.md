# Abnahme Recovery-Modus

Zwei Tabellen: oben die Abnahme je Arbeitspaket aus Abschnitt 11 von `docs/recovery-modus.md`,
unten die 18 Kriterien aus Abschnitt 12. Eine Zeile gilt erst als bestanden, wenn Ergebnis, Datum
und Prüfer eingetragen sind. **Prüfer ist immer ein Mensch.** Ein einziges offenes Kriterium
blockiert die Inbetriebnahme.

## Wie dieses Blatt gelesen wird

Jede Aussage weiter unten trägt eine **Stufe**. Die vier Stufen sind nicht austauschbar, und die
Verwechslung ist der Grund für dieses Blatt: wer „ausgeliefert" für „ausgeführt" nimmt, sucht an
der Stelle nicht mehr.

| Stufe | heißt | heißt **nicht** |
|---|---|---|
| **geschrieben** | der Satz steht im Repo | dass er stimmt |
| **übersetzt** | Werkzeug und Syntax nehmen es an (`bash -n`, `tools/check.sh`) | dass es etwas tut |
| **ausgeliefert** | der committete Stand ist bitgleich mit dem gemessenen (`git hash-object` beidseitig) | dass ein Lauf ihn berührt hat |
| **ausgeführt** | ein Lauf hat die Zeile erreicht, mit Ausstiegscode und Zahl daneben | dass die **Wirkung** über die Laufzeit hinaus belegt ist |

Die letzte Spalte der vierten Zeile ist die wichtigste Einschränkung dieses Pakets: ein
zurückgelesener Parameter ist **ausgeführt** als Lesevorgang und **unbelegt** als Wirkung.

## Abnahme je Arbeitspaket

| Paket | Kriterium | Ergebnis | Datum | Prüfer | Beleg |
|---|---|---|---|---|---|
| 0 | Seal/Unseal mit PolicyOR aus 7 Zweigen auf swtpm | | | | `tools/recovery-ci.sh` |
| 1 | Testvektoren, 24 h Fuzzing ohne Absturz, Abdeckung > 90 % | offen | | | Paket 1 |
| 2 | alle Funktionen auf allen Backends, Sperre nach 3 Versuchen | offen | | | Paket 2 |
| 3 | Kriterien 1–4, 9, 10 auf swtpm und echtem TPM | offen | | | Paket 3 |
| 4 | Handbuch-Kapitel Erstellung und Ersatz laufen ohne Nachfrage durch | offen | | | Paket 4 |
| 5 | Kriterien 5, 6, 7 | offen | | | Paket 5 |
| 6 | Kriterien 1–8, 13–16 | offen | | | Paket 6 |
| 7 | Kriterien 8, 12 | offen | | | Paket 7 |
| 8 | Kriterien 4, 11, 17 | offen | | | Paket 8 |
| 9 | Kriterium 18 | offen | | | Paket 9 |
| 10 | Kriterien 14, 15 mit Zeitmessung | offen | | | Paket 10 |

So wird die Zeile für Paket 0 nach dem Lauf ausgefüllt: Ergebnis `bestanden` oder `nicht
bestanden`, Datum als `TT.MM.JJJJ`, Prüfer mit Namen, Beleg mit dem Dateinamen des Protokolls aus
`dist/recovery/ci/`. **Die Zeile füllt Thomas, keine Sitzung** — darum steht sie leer, obwohl der
Lauf unten mit Zahlen daneben steht.

## Die 18 Kriterien

| Nr. | Kriterium (Kurzform) | Ergebnis | Datum | Prüfer | Wird nachgewiesen in |
|---|---|---|---|---|---|
| 1 | Ein Stick allein oder ohne PIN gibt nicht frei | offen | | | Paket 3 |
| 2 | Jedes der 6 Paare gibt frei, ein gesperrter Stick in keinem | offen | | | Paket 3 |
| 3 | Mitschnitt mit altem Zählerstand gibt nicht frei (Replay) | offen | | | Paket 3 |
| 4 | Recovery-Image um 1 Byte geändert: keine Freigabe | offen | | | Paket 3 und 8 |
| 5 | Datenträger in anderer Maschine | offen | | | Paket 5 |
| 6 | Backup ohne Sticks und ohne Server-TPM | offen | | | Paket 5 |
| 7 | Fremdes OS auf dem Server | offen | | | Paket 5 |
| 8 | Admin-Datensatz direkt in der DB | offen | | | Paket 7 |
| 9 | 3 falsche PINs: Stick gesperrt, TPM 24 h gesperrt | offen | | | Paket 2 und 3 |
| 10 | TPM-Bus zeigt nur verschlüsselte Sessions | offen | | | Paket 3, nur echte Hardware |
| 11 | Kein Netz, keine Shell, kein Login | offen | | | Paket 8 |
| 12 | Audit-Eintrag beim Zeugen, Alarm in 60 s | offen | | | Paket 7 |
| 13 | Recovery-Konto verfällt nach 24 h | offen | | | Paket 6 |
| 14 | Recovery ohne Vorwissen in unter 20 min | offen | | | Paket 10 |
| 15 | Server neu aufsetzen in unter 60 min | offen | | | Paket 10 |
| 16 | Stick-Ersatz | offen | | | Paket 4 |
| 17 | Reproduzierbarer Build | offen | | | Paket 8 |
| 18 | Externer Penetrationstest | offen | | | Paket 9 |

Der Testlauf von Paket 0 fährt schon Fälle, die den Kriterien 1 bis 4 ähneln. Sie zählen hier
trotzdem **nicht** als bestanden: Sie laufen gegen den Simulator swtpm und SoftHSM2, nicht gegen
ein echtes TPM und echte Sticks, und die Vorgabe ordnet sie Paket 3 zu.

## Der Lauf von Paket 0

Alles in diesem Abschnitt ist auf der VM `ngc-recovery` gemessen (Debian 13,
Linux 6.12.107+deb13-amd64). Die acht Skripte in `tools/recovery-*.sh` waren dabei **bitgleich**
mit dem committeten Stand, `git hash-object` auf beiden Seiten gleich — Stufe **ausgeliefert** für
jede Zeile, die hier eine Zahl trägt.

**Der Lauf nach vollständigem Wegräumen** (`tools/recovery-swtpm.sh stop main`, `stop qemu`,
`rm -rf dist/recovery/tpm dist/recovery/work`, dann `tools/recovery-ci.sh`), 12.10.2026:

| | Wert |
|---|---|
| Ausstiegscode | **0** |
| Zusammenfassung | `GRUEN: alle 39 Fälle von Paket 0 bestanden` |
| bestandene Fälle (`^ok: `) | **39** |
| `FEHLGESCHLAGEN`-Zeilen | **0** |
| Schlusszeile `Paket 0: alle Fälle bestanden` | **einmal** im Protokoll |
| Laufzeit | **62 161 ms** |
| Protokoll | `dist/recovery/ci/20261012-121156.log`, Kopie `last.log` |

Stufe **ausgeführt**. Der Messlauf hat **vor** der ersten Zahl geprüft, dass das Wegräumen
gegriffen hat (beide Verzeichnisse weg, kein `swtpm`-Prozess mehr), und bricht sonst ab statt eine
Zahl zu nennen. **Was dieses Wegräumen nicht umfasst:** `dist/recovery/softhsm`, `keys`, `ovmf`,
`pin` und `werkzeuge.txt` standen weiter da — das Rezept wirft den TPM-Zustand und das
Arbeitsverzeichnis weg, nicht die Token. Die Token legt `prepare` ohnehin neu an; **ein Lauf auf
einer Maschine, auf der noch nie etwas lief, ist damit nicht gemessen.**

Werkzeuge beim Lauf (`dist/recovery/werkzeuge.txt`, Stufe **ausgeführt**): tpm2-tools 5.7,
swtpm 0.7.1, SoftHSM2 2.6.1, QEMU 10.0.13, OpenSSL 3.5.7. **KVM nicht vorhanden, QEMU läuft
emuliert.**

### Was der Lauf belegt

Jede Zeile Stufe **ausgeführt**, Beleg je der benannte Fall im Protokoll:

- **Versiegeln und Öffnen** eines Testgeheimnisses mit einer `PolicyOR` aus **sieben** Zweigen
  (Normalstart über PCR; sechs Stick-Paare mit `PolicySigned`, `PolicyAuthValue` und `PolicyNV`),
  in **zwei** Objekten (`A-auto`, `A-recovery`). Jeder der sieben Zweige einzeln geöffnet **und**
  einzeln aus der Policy genommen, dann rot an seinem eigenen Namen.
- **Ein Stick allein gibt nicht frei** (halber Zweig, echte Signatur dieser Sitzung, Wand ist der
  abschließende `PolicyOR`) — ähnlich Kriterium 1, zählt dort **nicht**.
- **Ein fremder (fünfter) Stick** signiert gültig und öffnet in keinem Paar — ähnlich Kriterium 2.
- **Replay**: eine mitgeschnittene Stick-Signatur in einer frischen Sitzung wird mit
  `0x5DB … the signature is not valid` abgewiesen — ähnlich Kriterium 3.
- **Alter Zählerstand** scheitert am `PolicyNV`.
- **Geänderte PCR** gibt nicht frei.
- **Drei falsche Plattform-PINs**: `inLockout` direkt beim TPM gefragt, danach wird auch die
  **richtige** PIN abgewiesen, `lockout-clear` hebt es auf.
- **Die Plattform-PIN steht auf keiner Kommandozeile des Öffnens**: 42 Kommandozeilen
  **aufgezeichnet** (Spiegel im `PATH`, nicht abgetastet), die Plattform-PIN in keiner — und
  dieselbe Aufzeichnung **enthält** die Stick-PIN, das ist die Kontrolle.
- **Die zwei Objekte laufen nicht auseinander**: `seal` nimmt beide weg, bevor es eines macht, und
  verlangt danach alle vier Dateien jünger als ein Stempel. Gegenprobe von Hand: mit
  ausgeschaltetem zweiten `tpm2_create` Rückgabe 1.
- **Der CI-Job ist ein Riegel**: vier getrennt gemessene rote Läufe (Mutant „zweite Wiederholung",
  Mutant „ein Befehlscode weniger", Werkzeugprüfung rot, Attrappe mit Rückgabe 0), je Rückgabe 1
  und `ROT`.

### Was der Lauf NICHT belegt

Diese Punkte sind **gemessen offen**, nicht vergessen. Sie stehen gleichlautend in
`CHANGELOG.md`.

1. **Kriterium 9 ist zur Hälfte belegt, und die andere Hälfte ist hier nicht nachrüstbar.**
   Die **Plattform**-PIN-Sperre ist Stufe **ausgeführt** (oben). Dass **drei falsche Stick-PINs
   den Stick sperren**, ist **nicht belegt und nicht belegbar**: SoftHSM2 2.6 sperrt eine
   Benutzer-PIN nie. Gemessen, nicht angenommen — **neun** falsche PINs hintereinander auf
   `TEST-STICK-4`, jede mit Rückgabe 1, ließen das Token jedes Mal nur auf `user PIN count low`
   stehen (nie „final try", nie `CKF_USER_PIN_LOCKED`), und die **richtige** PIN loggte danach mit
   Rückgabe **0** ein und löschte die Flagge wieder. Die Sperre ist eine Eigenschaft des **echten**
   Tokens. Gebaut ist der Teil, der hier gilt; der Fall sagt im Text, was er nicht zeigt.
   Gehört ins Abnahmeprotokoll von Paket 2.
2. **Die 24 Stunden sind als Wirkung unbelegt.** `tpm2_getcap properties-variable` gibt
   `MAX_AUTH_FAIL 0x3`, `LOCKOUT_INTERVAL` und `LOCKOUT_RECOVERY` je `0x15180` = **86 400**; die
   drei Parameter werden **zurückgelesen** (Stufe **ausgeführt** als Lesevorgang). Kein Prüflauf
   kann 86 400 s aussitzen — die **Wirkung** bleibt offen.
3. **Flatter-Punkt:** eine Zahl der Klartext-Buchführung (die Treffer der acht Byte im Mitschnitt)
   ist **drei**, und der dritte Treffer entsteht nur, weil das TPM **einen** `tpm2_create` mit
   `TPM_RC_RETRY` beantwortet und ESAPI ihn erneut sendet. Gemessen über vier Läufe: immer genau
   ein Retry; **kein Lauf mit null oder zwei ist je gesehen worden, die Rate ist ungemessen.** Der
   CI-Job wiederholt bewusst **nicht** — er **ordnet** ein Rot dieser Art ein (nur wenn es die
   **einzige** `FEHLGESCHLAGEN`-Zeile ist) und bittet um **einen** Lauf von Hand. Die Rückgabe
   bleibt in beiden Fällen 1.
4. **Dass `A-auto` und `A-recovery` dasselbe Geheimnis tragen, prüft in diesem Paket nichts.**
   Beide öffnen zu `TEST-DEK`; dass es in Paket 6 **ein** DEK ist, ist dort zu messen.
5. **Dass die PIN auch beim Versiegeln nicht in der Prozessliste steht, ist nicht aufgezeichnet.**
   Aufgezeichnet ist nur das **Öffnen**. Laut Bericht zu Aufgabe 9 **billig nachrüstbar** — und
   bis dahin nicht belegt.
6. **Dass der Fehlversuchszähler nicht sinken kann, ruht auf `nt=counter`** und ist **nicht eigens
   gemessen**.

Dazu, aus denselben Berichten: die **Klartextsuche bleibt eine Nadelsuche** — sie kann nie zeigen,
dass das Geheimnis nicht entwichen ist; belegt ist ein **Kriterium**, und sieben Wege, es zu
umgehen, sind gemessen und stehen im Code. Der **leere Owner-auth** schwächt alles am NV-Zähler.
Ein **Mitschnitt einer ganzen Sitzung** ist nicht gemessen.

### Eine Richtigstellung zum Plan, gemessen am 12.10.2026

Das Nulllinien-Rezept „`stick2.pem` weglegen" **beißt nicht**: `tools/recovery-test.sh` ruft
`prepare` selbst auf und legt den Stick neu an, derselbe Lauf wurde **GRUEN mit 39 Fällen**. Wer
sich darauf verlässt, hält einen hohlen Riegel für einen Beweis. Der Plan ist an der Stelle
berichtigt; die Rezepte, die beißen, stehen dort (Attrappe mit Rückgabe 0; ein Riegel aus der
ausgelieferten `recovery-policy.sh` herausgeschnitten).
