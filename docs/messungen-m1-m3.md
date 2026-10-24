# Die drei fehlenden Messungen: M1, M2, M3

Auf `docs/thomas-entscheidet.md` stehen drei Messungen, die fehlen. Ohne sie
bleibt Frage 20 offen, könnte Frage 21 noch kippen, und in Frage 23 fehlt eine
Zahl. Hier sind sie gemessen.

Gemessen am 24.10.2026. Gemessen heißt: an einem laufenden System. Was nur aus
dem Quelltext feststeht, steht hier ausdrücklich als solches da. Was nicht
gemessen werden konnte, steht als Lücke da und nicht als Schätzung.

**Diese Datei ändert `docs/thomas-entscheidet.md` nicht.** Das Blatt liegt in
PR 114. Was hier entschieden wird, ist dort nachzutragen, sobald PR 114
zusammengeführt ist.

## Wo gemessen wurde

| | |
|---|---|
| M1 | Debian 13, podman 5.4.2, 128 Kerne, 64 GiB. Die Terminal-App im Container, gebaut nach der Anleitung in `apps/server-controller/CLAUDE.md`. |
| M2 | Zwei Orte. Die Shell ist der Browser: dort gemessen. Dazu die Go-Seite (Controller und API) mit dem echten Code aus `shared/store`. |
| M3 | Die echte Anlage aus sechs VMs, aufgesetzt am 23.10.2026. Ihr Protokoll und ihre Datenbank. |

Für M1 und M2 wurden Grenzen in einer Arbeitskopie außerhalb des Repos
angehoben. Im Repo ist nichts davon geändert.

---

## M1 — Wie viele Prozesse braucht eine echte App?

### Was gemessen wurde

Drei Dinge:

1. Wie hoch der Deckel wirklich ist.
2. Wie viele Prozesse eine offene Terminal-Sitzung kostet.
3. Ob der Deckel geteilt ist — ob also eine App eine andere mitreißen kann.

### Wie

Die Terminal-App wurde gebaut und als Container gestartet, mit genau den
Aufrufzeilen, die `createArgs` in
`apps/server-controller/classes/runtime/podman.go` zusammensetzt: eigener
Benutzer, kein Netz, 1 Kern, 256 MiB, nur lesbar.

Dann wurden echte Terminal-Sitzungen geöffnet. Jede startet den echten Dienst
`ngc-controller terminal`, und der startet eine echte Shell. Gezählt wurde im
cgroup des Containers. Drei Läufe je Stufe.

### Die Zahlen

**Der Deckel ist 2048, nicht 512.**

Abgelesen am cgroup des laufenden Containers: `pids.max = 2048`. Und
nachgefahren: ein Container, der so viele Kindprozesse startet, wie er kann,
bleibt bei `pids.current = 2047` stehen, meldet „Cannot fork", und der Zähler
`pids.events` zeigt genau eine Bremsung. Der Deckel ist also echt und er greift.

**Eine offene Terminal-Sitzung kostet 2 Prozesse und rund 9 Aufgaben.**

Der Deckel zählt nicht Prozesse, sondern Aufgaben — also auch die Fäden, die
das Go-Programm von sich aus aufmacht. Das ist der Grund für den Unterschied.

| Sitzungen | Prozesse | Aufgaben (3 Läufe) |
|---|---|---|
| 1 | 3 | 15–16 |
| 2 | 5 | 24–25 |
| 4 | 9 | 42–43 |
| 8 | 17 | 78–79 |
| 16 | 33 | 150–151 |
| 32 | 65 | 297–301 |
| 64 | 129 | 586–590 |

Leerlauf, ohne jede Sitzung: 6 Aufgaben, 1 Prozess.

Die Werte sind ruhig: über drei Läufe liegen sie nie mehr als 4 Aufgaben
auseinander. Das Gesetz dahinter: Prozesse = 2 × Sitzungen + 1, Aufgaben rund
9,1 × Sitzungen + 6.

64 gleichzeitige Sitzungen sind kein erfundener Fall. Das ist
`MaxSessionChannels` — so viele Kanäle darf **ein einziger Browser** offen
haben. Dabei stehen 586 bis 590 Aufgaben an. Wäre der Deckel 512, würde ein
einzelner Benutzer ihn allein reißen.

**Aus der gemessenen Steigung gerechnet** (nicht selbst gemessen): der Deckel
von 2048 wäre bei rund 224 Sitzungen erreicht.

**Der Deckel ist nicht geteilt. Er gilt je App.**

Zwei Container gleichzeitig. Der eine wurde an den Anschlag gefahren:

| | App A | App B |
|---|---|---|
| vorher | 6 Aufgaben | 6 Aufgaben |
| A am Anschlag | 2047, eine Bremsung | 6, keine Bremsung |

App B lief weiter und konnte weiter Prozesse starten. Beide liefen dabei sogar
unter **demselben** Benutzer — das ist der schwierigere Fall. In NGC bekommt
jede App ihren eigenen. Wenn es schon im schwierigeren Fall hält, hält es erst
recht im leichteren.

### Was nicht gemessen ist

**Eine zweite echte App gibt es nicht.** Nachgesehen im Repo und im echten
Katalog der laufenden Anlage: dort steht genau eine App, das Terminal. Das
Abbild `ngc/portversuch` auf der Messmaschine startet `/bin/sleep infinity` —
eine Attrappe, kein Maßstab. Die zweite Messung oben ist deshalb ein zweiter
**Container derselben App**. Sie beantwortet die Frage nach dem geteilten
Deckel, aber nicht die Frage, was eine *andere* App braucht.

**Über 64 Sitzungen hinaus fehlt die Messung.** Ab etwa 150 gleichzeitigen
Sitzungen ist nicht der Container stehengeblieben, sondern die Messvorrichtung:
jede Sitzung braucht außerhalb des Containers ein eigenes `podman exec`. Der
Deckel selbst wurde deshalb von innen nachgefahren (siehe oben), die Strecke
dorthin über Terminal-Sitzungen nicht.

**Nur aus dem Quelltext**, nicht gemessen: NGC setzt `--pids-limit` nirgends.
Die 2048 sind die Voreinstellung von podman auf dieser Maschine. Auf einem
Server, dessen `containers.conf` etwas anderes sagt, ist der Deckel ein anderer.

### Was das für Frage 20 heißt

**Frage 20 ist jetzt entscheidbar. Entschieden: kein Prozessdeckel je App in
v1.3.**

**STABILITÄT entscheidet.** Die Frage stand offen, weil ein geteilter Deckel
reißen könnte — eine App zieht die anderen mit. Gemessen: den geteilten Deckel
gibt es nicht. Jeder Container hat seinen eigenen. Eine App am Anschlag lässt
die Nachbarin unberührt, sogar unter demselben Benutzer. Damit fällt der Grund
weg, aus dem ein Feld je App die Stabilität tragen sollte: es gäbe nichts zu
trennen, was nicht schon getrennt ist. Ein Feld je App könnte den eigenen
Deckel nur **senken** — es schützt niemanden vor der App, sondern höchstens die
App vor sich selbst.

Sicherheit und Leistung werden nicht gebraucht.

**Was offen bleibt, und das ehrlich:** ob eine andere App mit 2048 auskommt,
ist nicht gemessen, weil es keine zweite gibt. Der Punkt kommt von selbst
zurück, sobald eine App an ihren eigenen Deckel stößt — und dann mit einer
Zahl. Das ist dieselbe Antwort wie vorher, aber jetzt aus einem anderen Grund:
nicht weil die Messung fehlt, sondern weil die gemessene Lage kein Feld
verlangt.

**Nachzutragen in das Blatt**, sobald PR 114 zusammengeführt ist: Frage 20 ist
entschieden. Und der Satz „Jede App teilt sich heute den einen Deckel von 512
Prozessen" stimmt in beiden Hälften nicht — es sind 2048, und geteilt ist
nichts.

### Aufträge, hier nicht gebaut

- `--pids-limit` im Controller selbst setzen, statt podmans Voreinstellung zu
  erben. Sonst hängt der Deckel an der Maschine und nicht an NGC. Ein Wert für
  alle Apps, kein Feld im Manifest.
- Eine zweite echte App bauen, damit der Bedarf einer *anderen* App überhaupt
  messbar wird.

---

## M2 — Was kostet eine größere Oberflächengrenze im Speicher?

### Was gemessen wurde

Der Preis dafür, `MaxUIFile` anzuheben: wie viel Speicher die Shell bei acht
offenen Apps hält, und wie lange das Hashen in einem Zug dauert.

### Wie

Die Shell ist der Browser. Dort wurde gemessen: acht Oberflächen gleichzeitig
gehalten und mit SHA-256 gehasht, so wie die Shell es tut. Fünf Läufe je Größe.

Dazu die Go-Seite mit dem echten Code aus `shared/store`: `UIWriter`, `WalkUI`
und der Hash über den ui-tar. Neun Läufe je Größe. `MaxUIFile` und `MaxUITotal`
wurden dafür in der Arbeitskopie angehoben — `UIWriter` lässt sonst gar nicht
erst zu, was gemessen werden soll.

### Die Zahlen — der Browser, also die Shell

| je App | acht Apps | Hashen in einem Zug | Speicher im Browser |
|---|---|---|---|
| **16 MiB (heute)** | 128 MiB | 128–134 ms | **209 MiB** |
| 32 MiB | 256 MiB | 262–287 ms | 545 MiB |
| **64 MiB** | 512 MiB | 499–554 ms | **1089 MiB** |
| 128 MiB | 1024 MiB | 1018–1078 ms | 1153 MiB (siehe unten) |

Bei 128 MiB je App zeigt der Browser seinen eigenen Höchstwert an. Die Zahl ist
dort eine Untergrenze, nicht der echte Bedarf.

Die Spannen sind eng — über fünf Läufe liegen die Zeiten nie mehr als 6 Prozent
auseinander.

**Der Preis, die Grenze von 16 auf 64 MiB zu vervierfachen: 880 MiB mehr
Speicher im Browser und viermal so lange Wartezeit.**

### Die Zahlen — die Go-Seite

| je App | Hashen 1 Stück | Hashen 8 Stück |
|---|---|---|
| 16 MiB | 30–77 ms | 376 ms |
| 32 MiB | — | 729 ms |
| 64 MiB | — | 1629 ms |
| 128 MiB | — | 3207 ms |

Das Lesen und Prüfen selbst (`WalkUI`) ist billig: 1,0 ms bei 16 MiB, 7,1 ms
bei 128 MiB. Teuer ist nur das Hashen.

### Was das für Frage 21 heißt

**Frage 21 kommt nicht zurück. Die Entscheidung „Grenzen bleiben" steht — jetzt
mit einer Zahl.**

**STABILITÄT entscheidet.** Das Blatt sagte, der Preis sei nicht gemessen und
eine Grenze ohne bekannten Preis dürfe nicht steigen. Jetzt ist er gemessen,
und er ist hoch: acht offene Apps mit 64 MiB je Oberfläche kosten den Browser
**über ein Gigabyte**. Das ist kein Randfall — acht offene Fenster sind ein
normaler Arbeitstag. Auf einem Gerät mit 4 GB wirft der Browser so einen Tab
weg, und der Benutzer verliert seine Arbeit, ohne zu verstehen warum. Die
heutigen 209 MiB sind das, was ein Arbeitsplatz im Browser tragen kann.

Sicherheit und Leistung werden nicht gebraucht.

**Nur aus dem Quelltext**, nicht gemessen, aber wichtig: `MaxUITotal` ist nicht
bloß eine Grenze der Shell. Dieselbe Zahl begrenzt den Datenblock in der
Datenbank (`sqlstore.MaxData`), die Einstellung des MariaDB-Servers
(`apps/server-db/classes/mariadb/mariadb.go:23`) und die Nachrichtengröße in
`coreapp/dispatch.go:460`. Die Grenze anzuheben ist deshalb keine Änderung an
der Shell allein, sondern eine an vier Stellen gleichzeitig, eine davon die
Datenbank. Das ist ein zweiter Grund, und er zeigt in dieselbe Richtung.

Der Satz, der laut Blatt in den Vertrag soll, steht damit auf einer Zahl:
NGC-Oberflächen bleiben klein und handgeschrieben, und 16 MiB je Datei sind
Absicht.

---

## M3 — Wie oft wird ein Kopplungscode neu ausgegeben?

### Was gemessen wurde

Zwei Dinge:

1. Wie oft beim normalen Aufsetzen ein Code neu ausgegeben wird.
2. Wie viele MAC-Fehlversuche dabei wirklich vorkommen.

### Wie

Nachgesehen in der echten Anlage aus sechs VMs, die am 23.10.2026 aufgesetzt
wurde. Jede Ausgabe eines Codes steht im Protokoll der API (`pair-code`,
`join-code` — beide sind im Vertrag als ändernd geführt und werden deshalb
protokolliert). Dazu die Tabelle der Einmal-Werte: dort steht, welcher Code
verbraucht wurde und welcher nicht.

Für die Fehlversuche zusätzlich ein Versuch am laufenden System: 20
Kopplungsversuche mit falschen Angaben gegen die laufende API, und davor und
danach die Zeilen im Protokoll gezählt.

### Die Zahlen

Das Aufsetzen lief in 25 Minuten ab, von 23:15 bis 23:40.

| | ausgegeben | verbraucht | übrig | je Server |
|---|---|---|---|---|
| Controller koppeln (`pair`) | 7 | 4 | 3 | **1,75** |
| API beitreten (`join`) | 3 | 1 | 2 | **3,0** |

Vier Controller wurden gekoppelt, eine zweite API ist beigetreten. Dafür wurden
zehn Codes ausgegeben. **Ein Server braucht also zwischen 1,75 und 3 Codes, bis
er drin ist** — anders gesagt: 0,75 bis 2 Neuausgaben je Server.

**MAC-Fehlversuche: keine.** Im ganzen Protokoll der Anlage stehen 331 Zeilen
`ok` und 7 Zeilen `E_NOTFOUND`. Kein einziger Prüffehler.

**Aber diese Null ist nicht zu glauben, und das ist der eigentliche Befund.**
Gemessen am laufenden System: 20 fehlgeschlagene Kopplungsversuche gegen die
echte API ließen das Protokoll bei genau 338 Zeilen stehen — vorher 338,
nachher 338. **Ein Fehlversuch hinterlässt nirgends eine Spur.** Weder
`join.Accept` noch `controllers.Pair` schreiben ins Protokoll.

Die Null heißt also „nichts gesehen", nicht „nichts passiert". Wer heute die
Adresse kennt und Codes verbrennt, tut das unsichtbar.

### Was das für Frage 23 heißt

**Die Entscheidung in 23 steht und hing nie an dieser Messung. Die Schwelle ist
jetzt begründbar.**

Zuerst das Wichtige: die Neuausgaben und der Zähler sind zwei verschiedene
Dinge. Wenn ein Admin es noch einmal versucht, holt er sich einen **neuen**
Code — das belastet den Zähler des alten nicht. Der Zähler zählt falsche MACs
**gegen denselben Code**. Und genau das tut ein ehrlicher Admin nie: das
Geheimnis steckt im Code, ulh trägt es unverändert weiter. Gemessen sind dafür
null Fälle.

**Vorschlag: der Zähler sperrt nach 3 Fehlversuchen je Code.**

- **STABILITÄT:** Ein ehrlicher Admin erzeugt gemessen **null** falsche MACs.
  Seine Wiederholungen — gemessen bis zu 2 je Server — kosten ihn neue Codes,
  nicht Zählerstände. Es gibt also nichts, was 3 ihm wegnehmen würde. Die 3
  statt 1 ist allein der Platz für eine Wiederholung, die unterwegs verloren
  ging.
- **SICHERHEIT setzt die Grenze nach oben:** jeder zusätzliche Versuch ist ein
  weiterer Rateversuch. Weil Stabilität nichts Höheres verlangt, bleibt die Zahl
  klein.
- Leistung wird nicht gebraucht.

**Was die Zahl noch nicht trägt:** 3 steht auf null gemessenen ehrlichen
Fehlversuchen — und null, weil nichts aufgezeichnet wird. Die Zahl ist zu
prüfen, sobald Fehlversuche überhaupt sichtbar sind. Das ist zuerst zu bauen,
nicht der Zähler.

### Aufträge, hier nicht gebaut

- Fehlgeschlagene Kopplungsversuche protokollieren. Ohne das zählt der Zähler
  im Dunkeln, und kein Admin sieht je, dass jemand seine Codes verbrennt.
- Erst danach die Schwelle von 3 an echten Zahlen nachprüfen.

---

## Kurz

| | Ergebnis | Frage |
|---|---|---|
| **M1** | Deckel 2048, nicht 512, und **je App statt geteilt**. Eine Terminal-Sitzung: 2 Prozesse, 9 Aufgaben. 64 Sitzungen: 588. | **20 ist entschieden:** kein Feld je App. |
| **M2** | Acht Apps mit 64 MiB kosten den Browser **1089 MiB** statt 209. | **21 bleibt, wie es war** — jetzt mit Zahl. |
| **M3** | 1,75 bis 3 Codes je Server. Fehlversuche: **null gesehen, aber nichts wird aufgezeichnet.** | **23 bleibt.** Schwelle: 3, unter Vorbehalt. |

Offene Lücken: eine zweite echte App fehlt (M1). Die Strecke über 64 Sitzungen
hinaus fehlt (M1). Fehlversuche werden nirgends aufgezeichnet (M3).
