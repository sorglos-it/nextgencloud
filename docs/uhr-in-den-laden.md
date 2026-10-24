# Die Uhr in den Laden — was gemessen ist und was noch ein Mensch tun muss

Stand 24.10.2026, nachmittags. Anlage: die verteilte, sechs Maschinen,
`https://ngc.mein.lan`. Diese Datei gehört zur Lücke **M1** aus
`docs/messungen-m1-m3.md`: „es gibt nur eine echte App". Die Lücke ist nach
dieser Runde **nicht geschlossen**, aber sie ist kleiner, und ihr Rest hat
genau einen Grund, der hier gemessen steht.

`docs/app-weg.md` ist mit Absicht nicht angefasst — daran arbeitet eine andere
Sitzung.

---

## Kurz: wo der Weg steht

| Station | Stand |
|---|---|
| Paket der Uhr gebaut | **gemessen, in Ordnung** |
| Katalog Folge 7 unterschrieben, beide Apps | **gemessen, in Ordnung** |
| Alter Katalog gesichert | **gemessen** |
| Katalog kommt über das Netz an | **gemessen** |
| Paket kommt über das Netz an | **gemessen** |
| Katalog liegt in der Anlage (Datenbank) | **gemessen** |
| Uhr im Laden sichtbar, einrichten, Recht, Fenster, Zeit | **offen — braucht eine Anmeldung im Browser** |

Die letzte Zeile ist **nicht** ein Rest, den man noch schnell abarbeitet: sie
ist gesperrt, und der Riegel ist in Abschnitt 9 gemessen.

---

## 1. Die Sicherung — wohin der alte Katalog gelegt ist

```
store (10.2.3.215):  /root/store/catalog-vor-uhr-20261024.json
```

690 Byte, Laden `global`, **Folge 6**, eine App (`org.sorglos.terminal`),
Schlüsselkennung `RPmRtyPJs5WQECAkiPeBsg`. Das ist der Katalog, der bis 11:33
ausgeliefert wurde.

Zurückdrehen heißt: diese Datei nach `/var/www/store/catalog.json` kopieren
und `/root/store/seq` auf `6` setzen.

## 2. Der Schlüssel — was mit ihm getan und was nicht getan wurde

Der geheime Ladenschlüssel liegt auf `store` unter `/root/store/store.key`,
Rechte 0600, 44 Byte. Er ist **nicht** gewechselt worden, und das war die
wichtigste Bedingung: ein anderer Schlüssel oder eine andere Ladenkennung
hätte das eingerichtete Terminal ausgesperrt.

`ngc-store` nimmt den Schlüssel **als Datei**, nie als Aufrufglied — das
Programm sagt das in seiner eigenen Hilfe, und so ist es benutzt worden. Der
Schlüssel steht in keinem Protokoll, in keiner Fehlermeldung und in keiner
Kommandozeile dieser Runde.

## 3. Der Katalog Folge 7 — gemessen

```
/var/www/store/catalog.json   1116 Byte, 24.10.2026 11:33
  kid    RPmRtyPJs5WQECAkiPeBsg
  store  global
  seq    7
  apps   org.sorglos.terminal, org.sorglos.uhr
/root/store/seq               7
```

Beide Apps sind drin, die Ladenkennung ist `global` geblieben, die
Schlüsselkennung ist dieselbe wie vorher.

**Der Laden prüft sich selbst:**

```
store# ./ngc-store check -store global /var/www/store
store ok
RC=0
```

Der Rückgabewert ist hier wirklich der von `ngc-store`. Beim ersten Versuch
stand `EXIT=0` hinter einem `| tail` — das hätte alles gesagt, nur nichts
über `ngc-store`.

## 4. Die Unterschrift gegen die Vertrauensliste — der eigentliche Beweis

`store ok` prüft gegen die Schlüsseldatei des Ladens. Das beweist noch nicht,
dass **die Anlage** den Katalog annimmt. Darum von der anderen Seite her
gegengeprüft.

Die Vertrauensliste der Anlage (`db-1`, Datenbank `ngc`, Tabelle `ngc_blob`,
Art `trust`, Folge 6) führt genau einen Laden:

```
id   global
name NGC Store
url  https://store.ngc.mein.lan
key  BTVQ0arqRXk6dQ13mLatXNgUtkU4iuARkmgQisePPYk
```

Aus diesem **öffentlichen** Schlüssel die Kennung gerechnet, wie es
`sign.keyID` tut (die ersten 16 Byte des SHA-256, base64url):

```
BTVQ0arqRXk6dQ13mLatXNgUtkU4iuARkmgQisePPYk  ->  RPmRtyPJs5WQECAkiPeBsg
Kennung im Katalog:                               RPmRtyPJs5WQECAkiPeBsg
gleich: ja
```

Und dann die Unterschrift selbst, mit **diesem** öffentlichen Schlüssel aus
der Vertrauensliste, über `"ngc/catalog/v1" || 0x00 || payload`:

```
SIGNATURE OK  (trust-list key, decoded payload)
store: global  seq: 7
```

**Daraus folgt der wichtigste Satz dieser Datei:** Thomas muss **keine neue
Fassung der Vertrauensliste unterschreiben**. Der Laden steht schon drin, mit
genau dem Schlüssel, der Folge 7 unterschrieben hat. Die einzige menschliche
Handlung, die noch fehlt, ist eine **Anmeldung**.

## 5. Beide Ströme, getrennt gemessen

Beim Terminal war einmal `stderr` angekommen und `stdout` nicht. Darum jede
Abholung mit getrennten Strömen, gemessen von `api-1` aus, über das echte
Netz:

```
Katalog:  curl rc=0   http=200  size=1116       stdout 1116 Byte   stderr 0 Byte
Paket:    curl rc=0   http=200  size=85623296   stdout 85623296    stderr 0 Byte
          0,58 s
```

Das Paket gegen den Katalog gerechnet:

```
Größe   85623296  erwartet 85623296                  stimmt
sha256  21pfUXt0xiCaEJzG3NEnewhTDVQWapwkvJnaAinL_OA  stimmt
```

Beide Ströme sind da, wo sie hingehören, und `stderr` ist in beiden Fällen
leer. Kein stiller Fehlschlag.

## 6. Das Paket — was wirklich drin liegt

```
manifest.json
ui/index.html, ui/app.js, ui/app.css
services/amd64.tar
```

Und in einer Schicht des Abbilds:

```
usr/lib/ngc/ngc-uhr
```

Das ist nachgesehen und nicht geglaubt. Gebaut ist nicht gelaufen — aber ein
Paket, in dem das Programm fehlt, wäre gar nicht erst gelaufen. Es fehlt
nicht.

## 7. Der Katalog ist in der Anlage angekommen

Auf `db-1`, Datenbank `ngc`, Tabelle `ngc_blob`:

```
kind     id      Länge   updated
catalog  global  1116    1792842288   (24.10.2026, etwa 11:58)
```

1116 Byte, Folge **7**, **beide** Apps, dieselbe Schlüsselkennung. Die
stündliche Abholung hat also stattgefunden. Zwischen „App im Laden" (11:33)
und „Katalog in der Anlage" (etwa 11:58) lagen etwa 25 Minuten.

**Und gleichzeitig:**

```
ngc_app:  org.sorglos.terminal  1.0.0  store global  node svc-2  active 1
          (eine Zeile, das ist alles)
```

Die Uhr ist **nicht eingerichtet**. Der Katalog liegt in der Anlage, die App
nicht. Genau da endet, was ohne einen Menschen geht.

---

## 8. Auf welcher Maschine eine App landet — gemessen, nicht geraten

Die Namen lügen, und sie lügen in beide Richtungen. Darum an den laufenden
Diensten abgelesen, nicht am Namen der Maschine:

| Maschine | läuft dort wirklich | Rolle (`controller.json`) | podman |
|---|---|---|---|
| api-1 `.210` | `ngc-api` **und** `ngc-controller` | `web` | fehlt |
| api-2 `.211` | nur `ngc-api` | — | — |
| db-1 `.212` | nur `ngc-controller` | `db` | fehlt |
| svc-1 `.213` | `ngc-controller` + `ngc-hold` | `services` | `/usr/bin/podman` |
| svc-2 `.214` | `ngc-controller` + `ngc-hold` | `services` | `/usr/bin/podman` |
| store `.215` | **kein NGC** | — | — |

**Wer die Maschine wählt, ist der Mensch, nicht die Anlage.** Im Laden steht
ein Auswahlfeld „Rechner", und es führt nur Controller, die oben sind und die
Rolle `services` tragen — `apps/web/public/assets/js/apps/store.js`, die
Zeile mit `n.online && n.roles.includes(ROLE_SERVICES)`. Gemessen heißt das:
das Feld wird **svc-1 und svc-2** anbieten und sonst nichts. Es gibt keine
Verteilung, die sich etwas aussucht.

### Ein Befund, der nicht bestellt war

Das Terminal läuft auf **beiden** Dienstmaschinen, während die Datenbank nur
`svc-2` nennt. An den App-eigenen Containerspeichern abgelesen:

```
svc-1:  ngc-terminal-439982dd | localhost/ngc-terminal:1.0.0 | Up About an hour
svc-2:  ngc-terminal-439982dd | localhost/ngc-terminal:1.0.0 | Up 39 minutes
ngc_app.node: svc-2
```

Aus den Protokollen:

```
svc-1  10:56:53  container create / init / start   (erste Einrichtung)
svc-1  11:05:40  Controller neu gestartet -> container init / start
svc-2  11:28:10  neuer Benutzer ngc-terminal-439982dd
svc-2  11:28:14  container create / init / start   (zweite Einrichtung)
ngc_app.updated  etwa 11:28
```

**Gemessen:** das Terminal wurde zuerst auf `svc-1` eingerichtet, später auf
`svc-2`. Der Container auf `svc-1` ist nie entfernt worden, und der Controller
hat ihn nach seinem Neustart um 11:05 **selbst wieder angeworfen**. Die
Datenbank trägt nur die neueste Maschine; von `svc-1` weiß die Oberfläche
nichts.

**Abgewogen:** das sieht nach einem echten Fehler aus — eine Einrichtung auf
einer anderen Maschine sollte die alte abräumen, oder die Anlage sollte
wenigstens wissen, dass dort noch etwas läuft. Dass es so **gemeint** ist
(eine App absichtlich auf mehreren Maschinen), halte ich für unwahrscheinlich,
aber ich habe es nicht im Vertrag nachgelesen und nenne es darum nicht
gemessen. Es steht als Punkt 74 in `docs/vertraege-v1.2.md`.

---

## 9. Warum es ohne Thomas nicht weitergeht — gemessen, nicht vermutet

Ich habe drei Wege gesucht, das Einrichten ohne Browser zu tun. Alle drei sind
zu, und zwar aus einem Grund.

**Erstens: einen vorhandenen Zugang wiederverwenden geht nicht.** In der
Datenbank stehen zwölf gültige Sitzungen des Verwalters `weiricth` (alle noch
etwa 11 Stunden gültig, `locked 0`, `failures 0`). Trotzdem ist mit ihnen von
außen nichts zu machen: der Sitzungsschlüssel wird im Browser erzeugt und ist
**nicht auslesbar** —

```
apps/web/public/assets/js/auth/keys.js:31
crypto.subtle.generateKey({ name: "Ed25519" }, false, ["sign", "verify"])
                                               ^ extractable = false
```

— und das Passwort wird danach weggeworfen. Der geheime Teil hat den Browser
nie verlassen und kann ihn nicht verlassen.

**Zweitens: der Server nimmt nichts an, was der Browser nicht unterschrieben
hat.** Jeder Auftrag wird gegen den Sitzungsschlüssel des Client-Zertifikats
geprüft:

```
shared/task/task.go:108
sign.Verify(env, ed25519.PublicKey(cert.Sig), sign.PurposeTask)
```

Nur die Schale unterschreibt Aufträge. Das ist keine Hürde, die man umgeht —
das ist der Zweck der Bauart.

**Drittens: es gibt kein Kommando dafür.** `ngc-store` packt und
unterschreibt, mehr nicht. Und `ngc-controller` kennt:

```
(ohne Kommando) | pair | backup | terminal | hold | image
```

Kein `install`, kein `activate`. Nichts, was eine App einrichtet.

**Also:** die Anmeldung ist nicht die bequemste Art weiterzukommen, sie ist
die einzige. Ein Passwort gebe ich nicht ein.

---

## 10. Was Thomas drücken soll

Alles ist vorbereitet. Es sind sechs Schritte, und keiner davon braucht eine
Unterschrift unter eine neue Vertrauensliste (Abschnitt 4).

1. **`https://ngc.mein.lan` öffnen und als `weiricth` anmelden.**
   Das ist der eine Schritt, den nur ein Mensch tun kann.

2. **Den „Laden" öffnen, Reiter „Apps".**
   Dort sollen jetzt **zwei** Apps stehen: „Terminal" und **„Uhr"** („Die
   Uhrzeit des Servers lesen."). Steht die Uhr nicht da: einmal das Fenster
   neu öffnen — der Katalog ist nachweislich in der Anlage, das wäre dann die
   Oberfläche und kein Lieferproblem.

3. **Bei „Uhr" auf „Aktivieren".**
   Im Feld **„Rechner" bitte `svc-1` wählen** — nicht `svc-2`. Das Terminal
   liegt auf `svc-2`; mit der Uhr auf `svc-1` ist zum ersten Mal gemessen,
   dass eine App dort landet, wo sie gewählt wurde, und dass zwei Apps auf
   zwei Maschinen laufen. Angeboten werden nur `svc-1` und `svc-2`.
   Die Uhr fordert **keine** besonderen Rechte, es sind also keine
   Rechte-Haken abzunicken.

4. **Warten, bis „Die App ist eingerichtet." dasteht.**

5. **Das Recht „benutzen" vergeben**, damit die App im Startmenü auftaucht.

6. **Die Uhr öffnen** und sagen, **ob eine Uhrzeit im Fenster steht** — und
   wenn nicht, was stattdessen dort steht. Der Knopf heißt „Noch einmal
   lesen".

Danach messe ich ohne weitere Anmeldung nach, und zwar an den laufenden
Diensten: `ngc_app` (Zeile, `node`, `active`), der Container im App-eigenen
Speicher auf der gewählten Maschine, und beide Ströme der Aktion `zeit`.

## 11. Was offen bleibt, damit es niemand für geprüft hält

- **Nicht gemessen: dass die Uhr im Laden erscheint.** Der Katalog liegt in
  der Anlage (Abschnitt 7) — dass die Oberfläche ihn auch anzeigt, ist damit
  wahrscheinlich, aber nicht dasselbe.
- **Nicht gemessen: Einrichten, Recht, Fenster, Zeit.** Das ist Schritt 3 bis
  6 oben.
- **Nicht gemessen: dass die Uhr auf `svc-1` läuft.** Die Maschine ist erst
  gewählt, wenn ein Mensch sie wählt.
- **Nicht gemessen: ob das Feld „Rechner" wirklich nur `svc-1` und `svc-2`
  zeigt.** Aus Rollen, `podman` und dem Code der Oberfläche gelesen, nicht am
  Bildschirm gesehen.
- **Die Lücke M1 bleibt offen.** Sie schließt, wenn Schritt 6 eine Uhrzeit
  zeigt — nicht vorher.
