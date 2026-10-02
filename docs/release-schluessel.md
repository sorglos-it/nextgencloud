# Der Release-Schlüssel

Mit diesem Schlüssel wird jedes NGC-Release unterschrieben. Wer ein Release
installiert, prüft es dagegen. **Der private Teil liegt auf einem YubiKey und
hat ihn nie verlassen** — er kann auch dann nicht gestohlen werden, wenn der
Rechner des Entwicklers übernommen wird.

## Woran man ein echtes Release erkennt

| | |
| --- | --- |
| Fingerabdruck | `2531 3B63 3392 D909 4B07  C4E4 71C3 9385 3F8F 4DBB` |
| Schlüsselkennung in der Hülle | `YWNJ9R-LilweZWb4Q4a1Vg` |
| Öffentlicher Schlüssel, 32 Byte | `c9e382058071d20a074f941b192e84d2e0e95b754d30534f75026f16a7ed5c42` |
| Verfahren | Ed25519 (Curve 25519) |
| Erzeugt | 02.10.2026 |
| Inhaber | Thomas Weirich &lt;info@sorglos.org&gt; |

Derselbe Schlüssel in der Form, die `gpg` liest, liegt daneben:
[`ngc-release-key.asc`](ngc-release-key.asc).

Die Schlüsselkennung ist `sign.KeyID`: die ersten 16 Byte von
`sha256(<32 rohe Byte>)`, in `base64url` **ohne** Füllzeichen — 22 Zeichen.
Nachgerechnet gegen `shared/sign/sign.go` und `shared/enc/enc.go`, nicht
geschätzt.

## Was die Karte kann und was nicht

Am 02./03.10.2026 gemessen, YubiKey 5.4.3, OpenPGP 3.4, Seriennummer
18241912:

- **Die Karte signiert nur Daten, deren Länge einem bekannten Hash
  entspricht** — 32 und 64 Byte gehen, 30 Byte lehnt sie mit
  `ERR Invalid value` ab. `sign.Message(purpose, payload)` ist beliebig lang
  (für ein Release 193 Byte) und geht damit **nie**.
- Eine Signatur über `gpg` deckt den **OpenPGP-Digest**, nicht die Nachricht:
  `ed25519.Verify(pub, nachricht, sig)` = false,
  `ed25519.Verify(pub, digest, sig)` = true.

**Folge, und sie steht als Punkt 19 im Register der Vertragsfassung 1.2:** die
Signatur deckt künftig `sha512(Message(purpose, payload))`. Die Hülle bleibt
gleich lang, die Sicherheit bleibt dieselbe — Ed25519 hasht intern ohnehin —,
und der Preis ist ein Satz mehr Vertrag. Der Satz muss dastehen, damit niemand
später die Nachricht einsetzt und sich wundert.

## Wie der Stick gehärtet ist

Alle drei Zeilen aus `gpg --card-status` gemessen, nicht angenommen:

- `UIF setting ......: Sign=on Decrypt=on Auth=on` — **jede** Signatur braucht
  einen Finger auf dem Stick. Ein Angreifer auf dem Rechner kann nichts
  unterschreiben, auch mit der PIN nicht.
- `Signature PIN ....: forced` — **jede** Signatur fragt die PIN, nicht nur die
  erste je Sitzung.
- `Key attributes ...: ed25519 cv25519 ed25519`.

`uif … permanent` wurde **nicht** gesetzt: das wäre unwiderruflich und nur per
Werksreset zu lösen.

## Was dabei schiefgegangen ist, damit es niemand wiederholt

- `gpg` liegt nicht im PATH, sondern unter
  `C:/Program Files/Git/usr/bin/gpg.exe`. In PowerShell braucht ein Befehl in
  Anführungszeichen den Aufruf-Operator `&`.
- In `~/.gnupg/common.conf` stand `use-keyboxd`, und Gits GnuPG findet den
  Dienst dafür nicht — jeder Schlüsselzugriff scheiterte mit
  „No Keybox daemon running". Datei beiseitelegen, dann legt `gpg` sich
  `pubring.kbx` an und braucht keinen Dienst.
- `admin` und `key-attr` in **einer** Zeile: `gpg` nimmt nur `admin`. Jede
  Zeile einzeln.
- Die **Admin-PIN braucht mindestens 8 Zeichen**. Eine kürzere weist `gpg` ab,
  **ohne** sie zur Karte zu schicken — die Meldung heißt trotzdem „Bad PIN",
  und der Fehlversuchszähler bleibt stehen. Das sieht aus wie eine falsche PIN
  und ist eine zu kurze.
- Die Frage „Make off-card backup of encryption key?" steht auf **Ja**. Wer
  sie durchklickt, hat eine Kopie eines privaten Schlüssels auf der Platte.
  Antwort ist `n`.
- **Eine Mindestlänge in ZIFFERN treibt in Geburtsdaten** (acht Stellen sind
  genau `ddmmjjjj`). Die Admin-PIN erlaubt bis zu 127 Zeichen — eine
  Passphrase, kein Datum.
