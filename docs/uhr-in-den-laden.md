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

> **Nachtrag vom selben Tag, etwa 12:30 (Uhr der Anlage): die Uhr ist
> eingerichtet und sie antwortet mit einer Uhrzeit.** Das ist nach dieser
> Tabelle passiert und steht in **Abschnitt 12 bis 14**. Die Tabelle oben
> bleibt stehen, wie sie gemessen wurde — und eine Zeile fehlt dort immer
> noch: **wer** eingerichtet hat, ist nicht gemessen. Solange das offen ist,
> steht hier **nicht**, dass „der Weg geht".

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

1116 Byte, Folge **7**, **beide** Apps, dieselbe Schlüsselkennung. Zwischen
„App im Laden" (11:33) und „Katalog in der Anlage" (etwa 11:58) lagen etwa
25 Minuten.

**Dass es die stündliche Runde war, ist nachgemessen und nicht angenommen:**
in den fünf Stunden vor 12:07 wurde auf keinem der beiden API-Server
`ngc-api` neu gestartet (die ersten Einträge sind api-2 um 12:07:36 und api-1
um 12:20:03). Ein Neustart hätte die Runde nämlich vorgezogen — `FirstCatalog
= time.Minute` in `apps/server-api/classes/server/server.go` lässt die **erste**
Runde eine Minute nach dem Start laufen und erst danach stündlich. Es gab
keinen, also war es der Stundenschlag.

Für die Praxis heißt das zweierlei, und das zweite ist der nützlichere Teil:
**ohne Zutun dauert es bis zu einer Stunde**, bis eine App im Laden in der
Anlage sichtbar wird — aber **wer nicht warten will, startet `ngc-api` neu und
hat die Runde nach einer Minute**. (Den Fund mit `FirstCatalog` hat die
Erstlauf-Sitzung gemacht; hier steht er, weil er die 25 Minuten oben erst
einordnet.)

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

Was von diesem Abschnitt eine halbe Stunde später nicht mehr stimmte, steht in
Abschnitt 12. Er bleibt stehen, damit man sieht, was wann gewusst war.

---

## 12. Nachtrag: die Uhr ist eingerichtet — und sie antwortet

Zwischen der Messung oben und dieser hier ist die Uhr auf der Anlage
eingerichtet worden. Gemessen, nicht erzählt:

```
ngc_app:  org.sorglos.uhr  1.0.0  store global  node svc-1  active 1  grants (leer)
svc-1:    ngc-uhr-ad8c00e2 | localhost/ngc/uhr:1.0.0 | Up
```

Der Ablauf aus dem Protokoll von `svc-1`:

```
12:20/12:21  api-1 getauscht -> 0.2.2   (nicht von dieser Sitzung)
12:25:37     useradd ngc-uhr-ad8c00e2
12:25:44     Aktion zeit   -> 510 Byte auf stderr
12:25:59     Aktion zeit   -> 510 Byte auf stderr
12:26:00     Aktion zeit   -> 510 Byte auf stderr
12:28:03     container died   (Behälter 3800792d…)
12:28:04     cleanup, remove, image pull, create, init, start  (Behälter 741d600b…)
```

Die Uhr liegt auf **svc-1**, also auf der anderen Maschine als das Terminal
(`svc-2`). Damit liegen zum ersten Mal zwei Apps auf zwei Maschinen. `grants`
ist leer — die Uhr fordert keine Rechte, und sie hat auch keine bekommen.

**Was nicht gemessen ist und was deshalb hier nicht steht:** wer diese
Einrichtung ausgelöst hat. Eine Einrichtung ist eine Aufgabe mit vollen
Rechten und soll ohne einen Menschen nicht gehen (Abschnitt 9). Ob Thomas sie
ausgelöst hat oder eine andere Sitzung, klärt die Koordinationssitzung. **Bis
das geklärt ist, ist dies kein Beleg, dass der Weg geht** — es ist ein Beleg,
dass er gegangen worden ist.

## 13. Beide Ströme der Aktion `zeit` — gemessen, getrennt

Ein einzelner Aufruf im eigenen Behälter der Uhr auf `svc-1`, die beiden
Ströme getrennt aufgefangen (`podman exec … /usr/lib/ngc/ngc-uhr` mit
`NGC_ACTION=zeit`):

```
Rückgabewert: 0

STDOUT  (21 Byte):  2026-10-24T12:31:52Z
STDERR  (33 Byte):  ngc-uhr 1.0.0 beantwortet "zeit"
```

**Beide Ströme kommen an, und zwar jeder dort, wo er hingehört.** Auf stdout
steht eine echte Uhrzeit — das ist die Zeile, die das Fenster zeigt. Auf
stderr steht die Meldung, die nur das Protokoll des Controllers je sieht. Das
ist genau die Messung, für die diese App gebaut wurde, und beim Terminal war
sie einmal schiefgegangen.

**Die App tut also, was sie soll.** Was damit **nicht** gemessen ist: dass das
Fenster die Zeile auch anzeigt. Zwischen stdout des Dienstes und dem Text im
Fenster liegen der Controller, der Tunnel, die API und die Schale. Dafür muss
ein Mensch einmal hinsehen.

## 14. Die 510 Byte — ein Befund, der sich nicht mehr aufklären lässt

Dreimal hat die Aktion `zeit` **genau 510 Byte** auf stderr geschrieben. Das
passt zu **keiner** Meldung, die der Dienst überhaupt schreiben kann. Die
Längen sind nachgerechnet, nicht geschätzt:

| was der Dienst schreiben kann | Byte |
|---|---|
| Erfolgsmeldung `ngc-uhr 1.0.0 beantwortet "zeit"` | **33** |
| Hilfe (`usage`) | 61 |
| `NGC_ACTION ist leer …` | 86 |
| **beobachtet** | **510** |

Und im heutigen Behälter sind es gemessen **33 Byte**, nicht 510.

Der Unterschied: die 510 Byte kamen aus Behälter `3800792d…`, der um 12:28:03
**gestorben** und um 12:28:04 entfernt und neu gebaut wurde. Er existiert
nicht mehr:

```
podman ps -a  ->  741d600b37b5 | ngc-uhr-ad8c00e2 | Up   (nur dieser eine)
```

**Damit ist der Inhalt dieser 510 Byte endgültig verloren**, und das ist der
eigentliche Befund:

- Der Controller schreibt ins Protokoll **nur die Anzahl** der Byte
  („a service wrote to stderr … bytes=510"), nie den Text.
- Den Text gibt es nur, wenn `serve.CallInput.LogStderr` an ist, und das ist
  von Haus aus aus.
- In `/etc/ngc/controller.json` gibt es **keinen Schalter** dafür. Die Schlüssel
  dort sind `anchor`, `apis`, `db`, `dbKey`, `name`, `noiseKey`, `podman`,
  `releaseKey`, `roles` — und sonst keiner.

Der einzige Lauf, bei dem etwas schieflief, hat also eine Zahl hinterlassen und
keinen Satz. Genau in dem Fall, für den ein Protokoll da ist, steht nichts
drin.

**Gemessen:** 510 Byte, dreimal, aus einem Behälter, der nicht mehr da ist;
im laufenden Behälter 33 Byte und eine Uhrzeit auf stdout; keine Möglichkeit,
den Text nachzulesen.

> **Berichtigung, eine halbe Stunde später gemessen — und sie dreht diesen
> Abschnitt um.** Hier stand, die drei Läufe seien wahrscheinlich
> **fehlgeschlagen**, weil 510 zu keiner vorgesehenen Meldung passt und der
> Behälter danach starb. **Das ist widerlegt.** Es gibt eine **vierte** Zeile,
> um `12:29:55`, und das ist genau der Lauf, bei dem im Fenster die Uhrzeit
> stand (Abschnitt 15) — auch er mit `bytes=510`. Ein erfolgreicher Lauf
> schreibt also dieselben 510 Byte. **510 ist kein Anzeichen für einen
> Fehlschlag**, und der gestorbene Behälter war nicht die Erklärung. Was
> wirklich dahintersteckt, steht in Abschnitt 16.

**Die saubere Gegenprobe kostet einen Knopfdruck:** einmal „Noch einmal
lesen" im Fenster der Uhr. Steht danach im Protokoll von `svc-1`
`bytes=33`, dann war 510 eine Eigenheit des gestorbenen Behälters und ist
erledigt. Steht dort wieder `bytes=510`, ist es ein laufender Fehler und
wiegt schwerer als alles andere an dieser App.

---

## 15. Aufgelöst: das Fenster zeigt die Zeit, und die Einrichtung hatte eine Anmeldung

Die zwei offenen Punkte aus Abschnitt 12 und 13 sind beantwortet. Die
Abschnitte oben bleiben stehen, wie sie gemessen wurden; hier steht, was sie
auflöst und **wer** es gemessen hat.

### Das Fenster zeigt die Zeit — gemessen von der App-Weg-Sitzung, im Browser

Nicht von mir gemessen, sondern von der Sitzung, die `docs/app-weg.md`
schreibt (Commit `201de73`). Sie hat den Weg im Browser zu Ende gegangen und
**im Rahmen der App** gelesen, nicht auf der Seite des Schreibtischs:

```
12:27:49  "Installieren" an der Uhr-Karte
12:27:55  Dialog: nur "Rechner | svc-1 | svc-2 | Aktivieren" — keine Rechte
12:27:56  Rechner gewählt: svc-1
12:28:08  "Die App ist eingerichtet."
12:29:53  Fenster geöffnet
12:29:55  im Rahmen der App:  "2026-10-24T12:29:55Z"
                              "Die Zeit kommt vom Server."
```

Und der Punkt, der diese App überhaupt rechtfertigt: **die Zeile kam über
`stdout` an, die `stderr`-Zeile stand nicht im Fenster.** Die beiden
Richtungen sind damit nicht nur im Behälter auseinandergehalten (Abschnitt
13), sondern über den ganzen Weg.

Das passt auf die Sekunde zu meiner eigenen Messung: `12:28:04 container
create` auf `svc-1` gegen `12:28:08 "Die App ist eingerichtet."`, und ein
Zeitstempel im Fenster, dessen Form (`RFC 3339`) die ist, die der Dienst
schreibt.

**Damit ist die Lücke M1 zu.** NGC hat zwei echte Apps, und der Weg der
zweiten ist gegangen und aufgeschrieben. Das ist der eine Satz, den ich in
Abschnitt 11 noch nicht schreiben durfte.

### Die Einrichtung hatte eine Anmeldung — kein Sicherheitsbefund

Die Frage war ernst: eine Einrichtung ist eine Aufgabe mit vollen Rechten, und
wenn eine App **ohne** Anmeldung eines Menschen eingerichtet werden kann, wäre
das schwerer als alles andere an diesem Tag. Sie ist beantwortet, und zwar
nein. Von mir an der Datenbank nachgemessen:

```
Sitzungen von weiricth:  12:19:23  12:19:42  12:21:37  12:25:18  12:25:22
                         12:26:58  12:29:16  12:29:36
ngc_app org.sorglos.uhr  updated   12:28:04
ngc_user weiricth        failures 0   locked 0   last_login 12:29:36
```

Die Einrichtung um **12:28:04** liegt zwischen den Anmeldungen um **12:26:58**
und **12:29:16**. Es gab also eine gültige Verwalter-Sitzung, und `failures 0`
heißt: keine fehlgeschlagenen Versuche, nichts Erzwungenes. Der Vertrag ist
eingehalten — der Auftrag war mit dem Sitzungsschlüssel einer echten Anmeldung
unterschrieben, so wie Abschnitt 9 es verlangt.

**Genau unterschieden, weil das zwei Dinge sind:** gemessen ist, dass eine
Anmeldung mit dem Passwort des Verwalters vorlag und der Weg darüber ging.
**Nicht** gemessen ist, ob Thomas das Passwort selbst getippt hat oder ob eine
Sitzung es aus einer Datei gelesen hat — die App-Weg-Sitzung beschreibt
Anmeldeläufe mit Argon2id im Browser, was beides sein kann. Für den Vertrag
macht es keinen Unterschied; für die Frage „wer hat hier gehandelt" macht es
einen, und die kann nur Thomas beantworten.

### Es waren zwei Einrichtungen, und die erste war niemand von uns beiden

Die Einrichtung um **12:25:37** ist eine andere als die um **12:28:04**, und
wer die erste ausgelöst hat, ist bis jetzt **nicht gemessen**. Ausgeschlossen
sind zwei:

- **Ich nicht.** Ich habe keine Anmeldung — kein Passwort, keine Sitzung,
  keinen Token; den Sitzungsschlüssel kann ich nicht haben (Abschnitt 9). Mein
  einziger ausführender Aufruf war der `podman exec` um 12:31:52, und der
  läuft nicht über den Controller und steht darum in dessen Protokoll auch
  nicht.
- **Die App-Weg-Sitzung nicht** (Commit `04627d2`). Sie hat „Aktivieren"
  genau zweimal gedrückt, um `12:14:34` (der Lauf, der am Tunnel riss) und um
  `12:27:57` (der durchlief); ihr Lauf um `12:22:29` kam nicht so weit, weil
  der Dialog „Kein Rechner für Dienste da" sagte. Zwischen 12:22:35 und
  12:26:58 lief bei ihr nichts, und ihre Skripte rufen `zeit` nie von selbst
  auf. Von den acht Anmeldungen ordnet sie sich sechs zu — die beiden um
  12:25:18 und 12:25:22 **nicht**.

Bleibt: **eine dritte Sitzung oder ein Mensch am Browser**, mit einer gültigen
Verwalter-Anmeldung. Das ist offen und wird hier nicht zugeraten.

**Was es für den Weg bedeutet, und das ist der stärkere Satz:** es sind
**zwei voneinander unabhängige Einrichtungen** gegen API 0.2.2 gelungen, auf
derselben Maschine, von verschiedenen Auslösern. Ein einzelner glücklicher
Lauf wäre schwach gewesen; zwei sind es nicht.

### Was von den 510 Byte bleibt

Mit den Zeiten der App-Weg-Sitzung wird der Befund aus Abschnitt 14 schärfer,
nicht schwächer. Es gab **zwei** Einrichtungen:

```
12:25:37  useradd, Behälter 3800792d…   <- erster Versuch
12:25:44 / 12:25:59 / 12:26:00          <- "zeit", je 510 Byte auf stderr
12:28:03  3800792d… stirbt
12:28:04  741d600b… neu gebaut          <- zweiter Versuch, der getragen hat
12:29:55  Zeit im Fenster
```

Der Knopfdruck-Gegenprobe oben braucht es nicht mehr: der laufende Behälter
ist gemessen, und zwar auf beiden Strömen. Die 510 Byte aber gehören **nicht**
zum gescheiterten Versuch — siehe den nächsten Abschnitt. Das war mein
Trugschluss, und er hat genau so lange gehalten, bis ich alle Zeilen gezählt
habe statt der drei, die ich erwartet hatte.

---

## 16. Die 510 Byte, richtig gemessen: der Dienst schreibt 33, das Protokoll sagt 510

Das ist der Befund, der von Abschnitt 14 übrig bleibt, nachdem meine erste
Erklärung widerlegt war. Er ist schärfer als die erste und betrifft **jede**
App, nicht die Uhr.

**Alle Zeilen, nicht nur die ersten drei:**

```
12:25:44  action=zeit  bytes=510     erster Versuch
12:25:59  action=zeit  bytes=510     erster Versuch
12:26:00  action=zeit  bytes=510     erster Versuch
12:29:55  action=zeit  bytes=510     <- DER LAUF, DER IM FENSTER DIE ZEIT ZEIGTE
```

Der vierte Lauf ist der erfolgreiche. Also schreibt ein **gelungener** Aufruf
dieselben 510 Byte wie die drei davor. Damit ist 510 kein Fehlerzeichen, und
der gestorbene Behälter erklärt nichts.

**Dagegen, im selben Behälter direkt gemessen** (`podman exec`, beide Ströme
getrennt):

```
Rückgabewert 0
STDOUT 21 Byte:  2026-10-24T12:31:52Z
STDERR 33 Byte:  ngc-uhr 1.0.0 beantwortet "zeit"
```

**Der Dienst schreibt 33 Byte. Das Protokoll des Controllers sagt 510.** Die
Differenz sind **477 Byte, die der Dienst nicht geschrieben hat** und die ihm
trotzdem zugeschrieben werden.

Drei Dinge, die ich ausgeschlossen habe, damit das nicht als Vermutung
dasteht:

- **Es ist keine Obergrenze.** `service.MaxStderr` ist `64 << 10`, also
  64 KiB. 510 ist kein Anschlag.
- **Es ist kein falscher Zähler.** Der Ring hängt mit `append` an und gibt
  `len(r.b)` zurück (`apps/server-controller/classes/serve/call.go`, `ring`
  und `logStderr`). Die Zahl ist eine echte Byte-Zahl, kein Puffermaß.
- **Es ist nicht die `sudo`-Warnung meines eigenen Aufrufs.** Dieselbe
  Ausführung ohne `cd /tmp` schreibt `cannot chdir to /root: Permission
  denied` — **41** Byte, und der Aufruf scheitert dann ganz (Rückgabewert 1,
  nichts auf stdout). Das ist eine Eigenheit meines Messwegs, nicht der 477.

**Nicht gemessen: was die 477 Byte sind.** Sie sind bei allen vier Aufrufen
gleich groß, also deterministisch und keine zufällige Störung. Woher sie im
Aufrufweg des Controllers kommen, ist offen — und genau das lässt sich **nicht
nachlesen**, weil der Text nur mit `serve.CallInput.LogStderr` ins Protokoll
kommt und `/etc/ngc/controller.json` dafür keinen Schlüssel hat (`anchor`,
`apis`, `db`, `dbKey`, `name`, `noiseKey`, `podman`, `releaseKey`, `roles` —
und keinen weiteren).

**Warum das zählt, auch wenn die Uhr tadellos läuft:** eine App, die ihre
Fehlermeldung auf stderr schreibt, bekommt sie im Protokoll mit 477 Byte
Fremdtext vermischt, und ein Betreiber, der den Schalter anwirft, liest
Fremdtext als Aussage der App. Für eine Uhr ist das Kosmetik. Für eine App,
die erklären soll, warum sie nicht konnte, ist es der Unterschied zwischen
einem Protokoll und einem Rätsel. Es steht als **Punkt 76** in
`docs/vertraege-v1.2.md`.
