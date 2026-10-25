# Die zwei Fälle, die am 24.10. offen geblieben sind

Am 24.10.2026 wurde jede der sechs Maschinen einmal abgeschaltet und gemessen
(`docs/ausfall-je-maschine.md`). Zwei Fälle blieben ausdrücklich offen, und
dieses Blatt holt sie nach:

- **Fall A: zwei Datenbankknoten gleichzeitig.** Die Anlage trägt seit dem
  24.10. drei Knoten. Fällt einer, halten zwei das Quorum — das ist gemessen.
  Fallen zwei, ist die Mehrheit weg, und das ist nicht gemessen.
- **Fall B: der Dienst-Stopp.** Die ganze Reihe von gestern bestand aus
  **Maschinen**-Stopps (`qm stop`). Der Dienst-Stopp (`systemctl stop`) ist nie
  gefahren worden.

**Die Erwartungen in diesem Blatt standen hier, bevor der erste Dienst und die
erste Maschine aus war** — festgeschrieben in dem Stand, der dieses Blatt
anlegt; die Messspalten kamen danach, in eigenen Ständen. Wer hinterher
aufschreibt, was er erwartet hat, hat nichts gemessen.

## Die Anlage vorher, gemessen (25.10.2026)

Nicht gerechnet, sondern an jedem Gast erfragt. Die beiden neuen DB-Knoten
haben **keinen laufenden Gast-Agenten** — `qm guest cmd 308 get-host-name`
antwortet „QEMU guest agent is not running". Ihre VMID ist darum über die
MAC-Adresse gebunden: `ip -o link` im Gast gegen `qm config <id>` auf dem Wirt.
Das ist der einzige Weg, der hier ohne Rechnen auskommt, und die Hausregel
„letztes Oktett = VMID" hält in dieser Anlage nicht (gestern belegt: die sechs
sind 301–306, nicht 210–215).

| Adresse | Name (im Gast) | MAC (im Gast) | VMID (Wirt, gleiche MAC) | Dienste, die laufen |
|---|---|---|---|---|
| 10.2.3.210 | api-1 | über Gast-Agent | 301 | `apache2`, `ngc-api`, `ngc-controller`, `php8.4-fpm` |
| 10.2.3.211 | api-2 | über Gast-Agent | 302 | `ngc-api` |
| 10.2.3.212 | db-1 | `bc:24:11:d6:6c:5e` | 303 | `mariadb`, `ngc-controller` |
| 10.2.3.213 | svc-1 | über Gast-Agent | 304 | `ngc-controller` (trägt die Uhr) |
| 10.2.3.214 | svc-2 | über Gast-Agent | 305 | `ngc-controller` (trägt das Terminal) |
| 10.2.3.215 | store | über Gast-Agent | 306 | `nginx` |
| 10.2.3.217 | db-2 | `bc:24:11:8e:01:55` | **308** | `mariadb` |
| 10.2.3.218 | db-3 | `bc:24:11:c7:32:5b` | **309** | `mariadb` |

Auf dem Wirt laufen daneben 200 (`nas`, das NAS dieses Hauses) und 307
(`ngc-ein`, 10.2.3.216) — beide gehören nicht zu dieser Messung und werden
nicht angefasst. Erlaubt sind 301–306, 308, 309.

Der Verbund, von db-1 erfragt: `wsrep_cluster_size` = **3**,
`wsrep_cluster_status` = **Primary**, `wsrep_local_state_comment` = **Synced**,
`wsrep_incoming_addresses` = `10.2.3.217:0,10.2.3.218:0,10.2.3.212:0`. Alle drei
mit `wsrep_node_address` auf die Adresse festgenagelt. Beide APIs tragen in
`/etc/ngc/api.json` alle drei Knoten in dieser Reihenfolge: `db-1`, `db-2`,
`db-3`.

**Eine Einstellung entscheidet den ganzen Fall A, und sie ist vorher
nachgesehen: `wsrep_dirty_reads = OFF`** (dazu `wsrep_on = ON`,
`wsrep_sync_wait = 0`). Sie steht hier, weil die Erwartung unten auf ihr ruht
und niemand sie nachher als „das wusste er ja" lesen können soll.

## Fall A: zwei Datenbankknoten gleichzeitig

Geschaltet werden **db-2 (308) und db-3 (309)**, so dicht hintereinander wie
zwei `qm stop` es zulassen. db-1 bleibt stehen: der Knoten, der den Verbund
gebootet hat und in beiden APIs an erster Stelle steht. Einer von drei hält
kein Quorum.

### Was eine andere Sitzung erwartet — und was ich erwarte

Die Erwartung, die ich nachmessen soll, lautet: **lesen geht noch, jedes
Schreiben wird verweigert.** Sie ist ausdrücklich als Erwartung notiert und
nicht gemessen.

**Ich erwarte, dass diese Erwartung falsch ist**, und zwar aus einer Einstellung
dieser Anlage und nicht aus allgemeinen Gründen: `wsrep_dirty_reads` steht auf
`OFF`. Ein Galera-Knoten ohne Quorum ist nicht nur schreibgesperrt — er lehnt
mit dieser Einstellung **auch jedes SELECT** mit Fehler 1047 ab
(`ER_UNKNOWN_COM_ERROR`, „WSREP has not yet prepared node for application
use"). Erst `wsrep_dirty_reads = ON` macht aus dem Verlust der Mehrheit den
Lesebetrieb, den die Erwartung beschreibt. Die Trennung „lesen ja, schreiben
nein" ist also keine Eigenschaft des Quorumverlusts, sondern eine Einstellung,
die hier nicht gesetzt ist.

Das messe ich getrennt und zuerst auf der Datenbank selbst — `SELECT` und
`INSERT` auf db-1, während db-2 und db-3 aus sind —, bevor ich die Anlage
frage. Sonst steht am Ende „die Anmeldung geht nicht" und niemand weiß, ob das
Lesen oder das Schreiben daran gescheitert ist.

### Die fünf Fragen, und die wichtigste dazu

- **(a) Oberfläche: ja.** Oberfläche ist `apache2` und `php8.4-fpm` auf api-1
  und hängt an keiner Datenbank. Gestern war das in jedem der drei DB-Fälle so.
- **(b) Anmeldung: nein.** Benutzer und Sitzungen stehen in dieser Datenbank,
  und nach dem Absatz oben ist sie auch für Lesezugriffe zu.
- **(c)–(e): nein.** Liste der Apps, Öffnen, Einrichten — alles über dieselbe
  Datenbank. (c) und (d) hängen zudem hinter einer Anmeldung, die nach (b)
  nicht zustande kommt; sie sind damit nicht unabhängig prüfbar, und das sage
  ich im Befund so, statt eine Zahl hinzuschreiben, die ich nicht gemessen
  habe.
- **Sagt die Anlage ehrlich, dass etwas fehlt? Ja — und das ist eine Erwartung
  aus dem Code, nicht ein Wunsch.** Drei Stellen tragen sie:
  `httpapi.Fault` beantwortet alles, was nicht aus der eigenen Fehlerliste von
  `state`/`dbapi` stammt, im letzten Arm mit `E_UNREACHABLE` — und ein
  MariaDB-1047 ist ein Treiberfehler, der dort landet. `httpapi/busy.go` macht
  daraus `503` mit `reason=db-busy`, Zähler und `retry-after`.
  `apps/web/public/assets/js/auth/login.js:449` entscheidet dann am Code und
  nicht am Ausgang: bei `E_UNREACHABLE` den Satz über den nicht erreichbaren
  Dienst, sonst „Anmeldung fehlgeschlagen."
  **Das ist genau die Stelle, an der der schwerere Befund läge.** Wenn der
  Quorum-Fehler auf irgendeinem Weg doch als Anmeldefehler ankommt, schiebt die
  Anlage dem Benutzer einen Fehler zu, den er nicht gemacht hat — und sie hat
  gestern dreimal bewiesen, dass sie es besser kann. Ich erwarte den ehrlichen
  Weg, aber ich erwarte ihn **gemessen**, am Wortlaut der Antwort und nicht am
  HTTP-Code allein.
- **Erste Anfrage getrennt von den folgenden.** Gestern: erste `503` nach
  **5,03 s**, zweite `200` in **0,03 s**. Hier erwarte ich den Zeitablauf
  **mehrfach nacheinander** — db-2 und db-3 sind zwei abwesende Knoten, und wenn
  der Treiber sie der Reihe nach anläuft, stehen zwei Zeitabläufe vor der
  Antwort. Eine erste Anfrage um 10 s wäre also kein Widerspruch, sondern die
  Bestätigung desselben Musters mit zwei Knoten statt einem. Danach erwarte ich
  **keine Erholung** auf 0,03 s, denn anders als gestern bleibt am Ende kein
  gesunder Knoten übrig, auf den umgeschaltet werden kann.
- **Selbstfang: offen, und das ist die unangenehme Frage.** Gestern kam db-3
  nach einem Maschinen-Stopp **nicht** von selbst zurück (`NRestarts=71`).
  Hier kommen zwei gleichzeitig zurück zu einem Knoten, der selbst kein Quorum
  hat. Ich erwarte, dass das **von Hand** nachgeholt werden muss, und schreibe
  auf, was genau nötig war.

## Fall B: der Dienst-Stopp

Gestern waren alle sechs Fälle `qm stop`. Beide Reihen sind einseitig, in
verschiedene Richtungen, und der Unterschied ist nicht klein:

| | Maschinen-Stopp (`qm stop`) | Dienst-Stopp (`systemctl stop`) |
|---|---|---|
| TCP | Pakete verschwinden; der Anrufer läuft in seinen Zeitablauf | Kernel antwortet `RST`; der Anrufer bekommt **sofort** „connection refused" |
| Nachbardienste auf dem Knoten | fallen mit | **laufen weiter** |
| beim Wiederanlauf | `cloud-init` räumt weg, was zur Laufzeit gesetzt wurde | nichts wird weggeräumt |
| Galera | der Knoten verschwindet; die anderen müssen ihn austragen | `mariadb` tritt **geordnet aus** dem Verbund aus |

Der Maschinen-Stopp ist damit der strengere Fall, und der Dienst-Stopp der
mildere. **Beide Spalten sind Erwartung**; die Tabelle steht hier, weil jede
Zahl unten an ihr hängt.

Gefahren werden die Rollen, bei denen sich der Dienst-Stopp vom
Maschinen-Stopp **unterscheiden kann** — nicht alle sechs noch einmal:

### B1: `ngc-api` auf api-2 (.211) aus, Maschine läuft

Der wichtigste der Dienst-Fälle, weil er einen gemessenen Befund von gestern
auseinandernimmt. Gestern mit der Maschine aus: die erste Anmeldung `503` nach
**5,03 s**, und im api-2-Fall `HTTP 500` bei 31 von 40 Anmeldungen (Punkt 90).

Erwartung: **der Zeitablauf fällt weg.** Ein `RST` kommt in Millisekunden, der
Apache führt das Mitglied sofort als tot, und die erste Anfrage sollte so
schnell sein wie die vierzigste. Wenn das so ist, dann liegen die 5,03 s
**nicht** am Umschalten, sondern daran, dass ein stummer Knoten nicht von einem
langsamen zu unterscheiden ist — und das ist eine andere Behebung als die, die
man einem Umschalter verschreiben würde. (An der Behebung arbeitet eine eigene
Sitzung; ich melde die Zahlen und baue nichts.)

### B2: `ngc-api` auf api-1 (.210) aus, `apache2` läuft weiter

Diesen Fall kann der Maschinen-Stopp nicht zeigen. Gestern fiel mit api-1 der
einzige Apache und der einzige Name — der Benutzer sah einen
Browser-Verbindungsfehler, und api-2 war für die Erreichbarkeit Zierde.

Erwartung: **hier wird api-2 zum ersten Mal zu etwas.** Der Apache auf api-1
steht, der Name löst weiter dorthin auf, und im Lastverteiler bleibt ein
lebendes Mitglied übrig. Ich erwarte (a)–(e) **unverändert** — und damit den
ersten Beleg dafür, dass der zweite API-Server wirklich trägt, wenn der Eingang
lebt. Beim ersten Versuch erwarte ich dieselbe Delle wie in B1, nur andersherum.

### B3: `mariadb` auf db-1 (.212) aus, Maschine läuft

Erwartung: **die Anlage merkt es kaum.** Zwei Knoten halten das Quorum, und
anders als gestern tritt db-1 geordnet aus dem Verbund aus statt zu
verschwinden. Die erste Anfrage sollte nicht in 5 s laufen, sondern sofort
umschalten.

Und die interessantere Hälfte: **die Rückkehr.** Gestern kam db-3 nach dem
Maschinen-Stopp nicht von selbst (71 Versuche, jedesmal ein voller Abgleich).
Nach einem geordneten Austritt erwarte ich einen **Beitritt über IST statt SST**
und damit eine Rückkehr von selbst. Wenn das so ist, dann ist die schlechte
Rückkehr von gestern eine Eigenschaft des harten Stopps und nicht der Anlage —
und das ändert, was man dagegen tut.

### B4: `ngc-controller` auf svc-1 (.213) aus, `podman` läuft weiter

Gestern nahm der Maschinen-Stopp den Container der Uhr mit. Erwartung hier:
**der Container läuft weiter, die Uhr tut es also auch** — aber niemand kann
sie mehr verwalten. Ein Befund wäre, wenn die Oberfläche eine App, deren
Verwalter fehlt, als „läuft" zeigt, obwohl kein Kommando sie mehr erreicht; der
gefährlichere Fall ist der umgekehrte: „läuft nicht", obwohl sie läuft.

### B5: `nginx` auf store (.215) aus, Maschine läuft

Gestern war der Verdacht, dass der leere Laden wie „keine Apps" aussieht und
nicht wie „der Laden antwortet nicht". Erwartung: **derselbe Ausgang, anderer
Weg dorthin** — statt eines Zeitablaufs ein sofortiges „refused". Wenn der
Wortlaut sich dadurch ändert, ist das der Befund.

## Was dieses Blatt nicht messen kann

- **(c) und (d) in Fall A.** Sie hängen hinter einer Anmeldung, die in Fall A
  nach Erwartung nicht zustande kommt, und die Anmeldung dieser Anlage braucht
  einen Passkey auf einem Gerät. Ich messe sie über die Schicht, die ich
  erreichen kann, und sage bei jeder Zahl, welche Schicht das war.
- **Die Behebung der ersten Anfrage.** Daran arbeitet eine eigene Sitzung. Hier
  stehen nur Zahlen.

---

# Gemessen

## Fall A — gemessen am 25.10.2026, 10:52–10:55 UTC

**Maschinen-Stopp** (`qm stop`) von db-2 (308) und db-3 (309) gleichzeitig,
db-1 (303) blieb stehen. Werkzeug: `tools/ausfall-mehrheit.sh`.

| Frage | erwartet | gemessen |
|---|---|---|
| (a) Oberfläche | ja, unverändert | **200 in 0,016 s** — unverändert |
| (b) Anmeldung, 40 in Folge | nein | **0/40 gut, 40× `503`** |
| (b) erste Anfragen, einzeln | Zeitablauf mehrfach, evtl. 10 s | **5,019 · 5,019 · 0,693 · 5,020 · 0,544 s** — alle `503` |
| lesen auf db-1 | **nein** (gegen die andere Erwartung) | **nein: `ERROR 1047`**, wortgleich zum INSERT |
| schreiben auf db-1 | nein | **nein: `ERROR 1047`** |
| Verbund | fällt auf 1, verliert Primary | **genau so**: `size=1`, `non-Primary`, `Initialized` |
| laufende Apps | laufen | **laufen**, unverändert |
| sagt die Anlage es ehrlich? | ja, aus dem Code | **ja, am Wortlaut belegt** |
| Selbstfang | **nein, von Hand** | **ja — in unter 30 s, ohne Zutun** |

### 1. Die Erwartung „lesen geht noch" ist falsch, und zwar wortgleich falsch

Das war der Auftrag: nachmessen statt übernehmen. Auf db-1, während die
Mehrheit weg war:

```
SELECT 1 (ohne Tabelle) : 1
SELECT count(*) FROM ngc.ngc_app : ERROR 1047 (08S01): WSREP has not yet prepared node for application use
INSERT INTO messung.nachtrag …   : ERROR 1047 (08S01): WSREP has not yet prepared node for application use
```

Lesen und Schreiben scheitern **mit demselben Fehler und demselben Wortlaut**.
Es gibt in dieser Anlage keinen Lesebetrieb bei verlorener Mehrheit, und das
liegt nicht am Quorumverlust als solchem, sondern an `wsrep_dirty_reads = OFF`
— einer Einstellung, die vor dem Schalten nachgesehen und oben
festgeschrieben ist. Wer die Trennung „lesen ja, schreiben nein" haben will,
muss sie **einschalten**; sie ist kein Naturgesetz des Verbunds. Ob sie
eingeschaltet werden *soll*, ist eine Abwägung und steht unten, nicht hier.

### 2. Der Befund, den niemand bestellt hat: `SELECT 1` geht weiter

`SELECT 1` antwortet auch ohne Quorum mit `1`, weil es keine Tabelle anfasst.
Das ist die übliche Lebensprobe. **Eine Überwachung, die mit `SELECT 1` fragt,
meldet diesen Knoten als gesund, während er keine einzige Zeile hergeben
kann.** Das ist kein Fehler dieser Anlage — es ist eine Falle für jede
Überwachung, die noch gebaut wird, und darum steht sie hier.

### 3. Die Anlage ist ehrlich — am Wortlaut, nicht nur am Code

Der schwerere Befund, auf den der Auftrag gezielt hat, ist **nicht**
eingetreten. Jede der 45 Anfragen bekam:

```
HTTP 503  {"status":"error","code":"E_UNREACHABLE","message":"Dienst nicht erreichbar."}
```

Kein „Anmeldung fehlgeschlagen", kein `500`, kein leerer Zustand. Und das
Journal trennt die beiden Gründe, die man leicht in einen Topf wirft:

```
api-1  WARN the database did not come free in time … reason=db-busy count=1 since=1 retry-after=5
api-1  WARN the database could not be reached     reason=db-unreachable count=1 since=1
api-1  WARN callers were turned away … busy=1 unreachable=2 new-busy=1 new-unreachable=2
```

**Eine Einschränkung, damit das nicht zu gut klingt.** `login/start` kann
diesen Fehler nicht mit einem Anmeldefehler verwechseln, weil es **gar keinen
hat**: derselbe Aufruf mit einem Namen, den es nicht gibt, antwortet im
gesunden Betrieb ebenfalls mit `200` und einem Nonce — absichtlich, damit die
Antwort nicht verrät, ob ein Name existiert. Die Stelle, an der sich „Datenbank
weg" und „falsches Kennwort" wirklich treffen könnten, ist `login/finish`, und
die braucht einen Passkey auf einem Gerät. **Diese Messung reicht bis
`login/start`, und für den Rest kann ich nur den Code lesen**
(`login.js:449` entscheidet am Feld `code`, nicht am Ausgang). Das ist Stufe
„am Wortlaut der API belegt", nicht „im Browser gesehen".

### 4. Die erste Anfrage: das Muster von gestern wiederholt sich nicht so, wie ich dachte

Gestern: erste `503` nach 5,03 s, zweite `200` in 0,03 s. Heute, fünf einzeln:

```
1. 5,019 s    2. 5,019 s    3. 0,693 s    4. 5,020 s    5. 0,544 s
```

**Meine Erwartung war in einem Punkt falsch:** ich hatte bei zwei abwesenden
Knoten rund 10 s erwartet, weil der Treiber sie der Reihe nach anlaufen müsste.
Das tut er nicht — die Obergrenze bleibt **ein** Zeitablauf von 5,02 s. Richtig
war, dass die Delle nicht nach der ersten Anfrage vorbei ist: sie kommt
wieder, unregelmäßig, zwischen 0,54 s und 5,02 s. Wer nur die vierzig zählt,
sieht „40× 503" und verliert genau diese Streuung — und sie ist der Unterschied
zwischen „fünf Sekunden einmal" und „fünf Sekunden immer wieder".

Die Zahl für die Sitzung, die daran arbeitet: **5,019 / 5,019 / 0,693 / 5,020 /
0,544 s**, alle mit `503` und `E_UNREACHABLE`, bei **zwei** abwesenden Knoten
und **keinem** gesunden. Gebaut habe ich daran nichts.

### 5. Der Selbstfang: meine Erwartung war falsch, und das ist die gute Nachricht

Ich hatte Handarbeit erwartet, weil db-3 gestern nach einem Maschinen-Stopp
`NRestarts=71` brauchte und nicht zurückkam. Gemessen:

```
+30s  wsrep_cluster_size=3  wsrep_cluster_status=Primary  wsrep_local_state_comment=Synced
db-2: NRestarts=0   „Server db-2 synced with group"   Min available from gcache: 1949
db-3: NRestarts=0   „Server db-3 synced with group"   Min available from gcache: 1404
```

Beide Knoten kamen **beim ersten Versuch** zurück, in unter 30 Sekunden, über
den gcache (also ohne vollen Abgleich), und der Verbund war wieder vollständig
und Primary. `safe_to_bootstrap: 0` und `seqno: -1` auf beiden — niemand musste
bootstrappen.

Das stellt den schweren Befund von gestern in ein anderes Licht, und zwar ohne
ihn zu widerlegen: **zwei gleichzeitig abgeschaltete Knoten, die zu einem
non-Primary-Überlebenden zurückkommen, fangen sich von selbst.** Der Fall, der
gestern nicht zurückkam, war ein *einzelner* Knoten zu einem *gesunden*
Verbund. Was die beiden unterscheidet, ist mit dieser einen Messung **nicht**
geklärt — es kann der gcache sein, der hier noch reichte, es kann die Länge des
Ausfalls sein (hier gut 90 Sekunden, gestern länger). Das ist eine offene
Frage, keine Erklärung, und ich schreibe sie als offene Frage hin.

### 6. Zwei Zeilen im Journal, die nicht zur Anmeldung gehören

```
api-1  WARN housekeeping failed error="Error 1047 … WSREP has not yet prepared node"
api-1  WARN trust list could not be read error="Error 1205 (HY000): Lock wait timeout exceeded"
```

Die Hintergrundarbeit scheitert mit — erwartbar. Interessant ist die
**zweite**: `1205`, ein ganz anderer Fehler als `1047`. Bei verlorener Mehrheit
laufen also nicht alle Wege in denselben Fehler, und `httpapi.Fault` fängt
`1205` nur deshalb richtig, weil sein letzter Arm **alles** Unbenannte zu
`E_UNREACHABLE` macht. Das ist hier ein Glücksfall und kein Entwurf: eine
Liste benannter Fehler hätte `1205` nicht enthalten. Befund, nicht Schaden.

Nebenbei, ohne Deutung: api-2 zählte `unreachable=17`, api-1 `unreachable=2` —
die beiden APIs sahen denselben Ausfall unterschiedlich oft.

### 7. Was Fall A nicht beantwortet

(c), (d) und (e) sind **nicht gemessen**. Sie hängen hinter einer Anmeldung,
die mit `503` endet — es gibt keine Sitzung, mit der man eine App-Liste holen
oder eine App öffnen könnte. Dass sie „nein" sind, folgt aus (b) und aus der
Tatsache, dass dieselbe Datenbank sie trägt; es ist **abgeleitet, nicht
gemessen**, und steht darum in keiner Zahlenspalte.

Was dagegen gemessen ist: **die laufenden Apps liefen durch.** Die Container
auf svc-1 und svc-2 waren vorher, während und nachher dieselben. Eine App, die
läuft, braucht die Datenbank nicht — wer drin ist, bleibt drin.

Dabei ist mir etwas aufgefallen, das nicht zu diesem Auftrag gehört und das ich
nicht weiterverfolgt habe: **svc-1 trägt `ngc-uhr-ad8c00e2` *und*
`ngc-terminal-439982dd`, svc-2 trägt `ngc-terminal-439982dd`** — derselbe
Terminal-Container erscheint auf beiden Knoten. Das sieht nach dem Rest aus,
den Punkt 74 beschreibt (ein Container bleibt nach einer Neuinstallation auf
dem alten Knoten stehen). Gemessen ist nur die Liste der Namen, nichts weiter.
