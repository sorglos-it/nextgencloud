# Die drei fehlenden Messungen: M1, M2, M3

Auf `docs/thomas-entscheidet.md` stehen drei Messungen, die fehlen. Ohne sie
bleibt Frage 20 offen, könnte Frage 21 noch kippen, und in Frage 23 fehlt eine
Zahl. Hier sind sie gemessen.

Gemessen am 24.10.2026. Gemessen heißt: an einem laufenden System. Was nur aus
dem Quelltext feststeht, steht hier ausdrücklich als solches da. Was nicht
gemessen werden konnte, steht als Lücke da und nicht als Schätzung.

**Diese Datei ändert `docs/thomas-entscheidet.md` nicht.** Was hier entschieden
wurde, ist dort inzwischen nachgetragen: PR 114 ist am 24.10.2026
zusammengeführt, Frage 20 ist entschieden, und die Begründung zu Frage 21 trägt
jetzt die Zahl aus M2 statt des Hinweises auf eine fehlende Messung.

## Wo gemessen wurde

| | |
|---|---|
| M1 | Debian 13, podman 5.4.2, 128 Kerne, 64 GiB. Die Terminal-App im Container, gebaut nach der Anleitung in `apps/server-controller/CLAUDE.md`. |
| M2 | Zwei Orte. Die Shell ist der Browser: dort gemessen. Dazu die Go-Seite (Controller und API) mit dem echten Code aus `shared/store`. |
| M3 | Die echte Anlage aus sechs VMs, aufgesetzt am 23.10.2026 — die **verteilte** Anlage, siehe „Die beiden Anlagen" unten. Ihr Protokoll und ihre Datenbank. |

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

---

## Nachtrag · Die beiden Anlagen und ihre Namen

Es gibt **zwei** Anlagen. Wer eine Messung wiederholt, muss sagen, welche er
meint — die Zahlen oben gelten nicht für beide.

| | Was | Wo gemessen |
|---|---|---|
| **Einzel-VM** | Eine Maschine, erreichbar über `ssh ngc`. Messort unter `ngc.test` mit selbst unterschriebenem Zertifikat. | M1 und M2 |
| **Verteilte Anlage** | Sechs VMs, aufgesetzt am 23.10.2026. Antwortet unter `ngc.mein.lan`. | M3 |

Die Namen der sechs VMs, Stand 24.10.2026:

```
10.2.3.210  api-1.ngc.mein.lan    api-1
10.2.3.211  api-2.ngc.mein.lan    api-2
10.2.3.212  db-1.ngc.mein.lan     db-1
10.2.3.213  svc-1.ngc.mein.lan    svc-1
10.2.3.214  svc-2.ngc.mein.lan    svc-2
10.2.3.215  store.ngc.mein.lan    store
10.2.3.210  ngc.mein.lan          ← hier antwortet die Anlage (dieselbe wie api-1)
```

**Der Name ist nicht Geschmackssache.** Zertifikat und Passkey hängen an
`ngc.mein.lan`. Eine Messung unter einem anderen Namen misst etwas anderes.

**Herkunft, und was daran nicht gemessen ist:** Die Namen kommen von Thomas,
weitergegeben am 24.10.2026. Ich habe sie **nicht nachgeprüft** — keinen der
Namen aufgelöst, keine der Maschinen angesprochen. Sie stehen hier, wie sie
übergeben wurden. Wer darauf messen will, prüft zuerst, dass sie stimmen.

### Was die verteilte Anlage für die drei offenen Lücken ändert

Das ist der Punkt, auf den es ankommt, und die Antwort ist: **bei einer von
drei.** Mehr Maschinen schließen keine Lücke, die nicht an Maschinen hing.

**Nachholbar — die Strecke über 64 Sitzungen (M1).** Stehengeblieben ist dort
nicht der Container, sondern die Messvorrichtung: jede Sitzung braucht von außen
ein eigenes `podman exec`. Mit `svc-1` und `svc-2` als Last-Erzeugern neben der
gemessenen Maschine ist die Strecke bis an den Deckel von 2048 zu fahren. Das
ist die einzige der drei Lücken, die die verteilte Anlage wirklich schließt.

**Nicht nachholbar — die zweite echte App (M1).** Die Lücke ist nicht, dass eine
App zu wenig Platz hat. Die Lücke ist, dass es **keine zweite App gibt**: im
Katalog der laufenden Anlage steht genau eine, das Terminal, und
`ngc/portversuch` startet `sleep infinity`. Sechs Rechner erzeugen keine App.
Diese Lücke schließt, wer eine zweite echte App baut oder portiert.

**Nicht nachholbar — die MAC-Fehlversuche (M3).** NGC zeichnet sie nirgends auf.
Auf sechs Maschinen zeichnet NGC sie sechsmal nicht auf. Diese Lücke schließt
eine Zeile Code, die den Fehlversuch protokolliert, und erst danach eine
Messung. Die Schwelle 3 in Frage 23 bleibt bis dahin unter Vorbehalt.

### Und eine Warnung zu Frage 32

Frage 32 ist entschieden: einmal vor der Veröffentlichung der ganze Erstlauf auf
einer Maschine mit echtem Namen **und** `letsencrypt`, mit Passkey-Anmeldung im
Browser.

`ngc.mein.lan` erfüllt davon die **erste Hälfte, nicht die zweite**. `mein.lan`
ist ein interner Name; `letsencrypt` stellt dafür kein Zertifikat aus, auch
nicht über DNS-01. Was ein Nutzer mit einem echten, öffentlichen Zertifikat
erlebt, bleibt auf der verteilten Anlage ungemessen.

**Frage 32 ist darum nicht abzuhaken**, solange kein öffentlicher Name im Spiel
ist — und sie ist der eine Punkt, der laut Blatt vor der Veröffentlichung
gemessen sein muss.

---

# Nachtrag · M1 nachgeholt: die Strecke über 64 Sitzungen hinaus

Gemessen am 24.10.2026 auf der **verteilten** Anlage. Dies holt die Lücke nach,
die M1 selbst benannt hat: „Über 64 Sitzungen hinaus fehlt die Messung. Ab etwa
150 gleichzeitigen Sitzungen ist nicht der Container stehengeblieben, sondern
die Messvorrichtung."

Vorweg das Wichtigste, weil es einen Befund von M1 umdreht: **auf dieser Anlage
greift der Prozessdeckel gar nicht.** Weder 2048 noch 512. Was greift, ist der
Deckel des Dienstes `ngc-controller.service`, und den teilen sich alle Apps mit
dem Controller selbst. Einzelheiten unter „Grenze des Containers".

## Wo gemessen wurde

| | |
|---|---|
| Maschine | **svc-2, 10.2.3.214** der verteilten Anlage. 2 Kerne, 2914 MiB Speicher, davon im Leerlauf rund 2500 MiB frei. Debian, podman 5.x, cgroup v2. |
| Rolle am laufenden Dienst geprüft | `ngc-controller.service` läuft, `/etc/ngc/controller.json` sagt `roles: ["services"]`, podman liegt unter `/usr/bin/podman`. Der Name lügt hier nicht. |
| Fassung | svc-2 trägt `ngc-controller 0.2.1`, svc-1 trägt 0.2.2. **Die beiden Knoten sind nicht gleich** — wer eine dieser Zahlen wiederholt, prüft das zuerst. |
| Messgegenstand | Ein **eigener** Container `m64c` aus dem **echten** Abbild der Terminal-App (`localhost/ngc-terminal:1.0.0`, aus dem Speicher des App-Benutzers ausgespielt), gestartet mit genau den Schaltern, die `createArgs` zusammensetzt: `--network none --cpus 1.000 --memory 256m --pids-limit 512 --read-only --cap-drop ALL --security-opt no-new-privileges --user 0:0`. |
| Sitzung | Die **echte** Sitzung: `/usr/lib/ngc/ngc-controller terminal` mit den echten Umgebungswerten (`NGC_DATA=/data`, `NGC_START=0`, gültige `NGC_USER`), die eine echte Shell auf einem echten Pseudoterminal startet. |
| Nicht angefasst | svc-1 (.213) mit der eingerichteten Terminal-App, der laufende Container auf svc-2, der laufende Controller auf svc-2. |

**Jede Zahl unten ist auf svc-2 entstanden, im Container `m64c`, in der cgroup
`m64load.scope`.** Keine Zahl stammt von der Einzel-VM, auf der M1 gemessen
wurde, und keine aus dem laufenden Container der Anlage.

---

## 1 · Die Vorrichtung, und woran man merkt, wer aufgegeben hat

Das war der Kern des Auftrags, darum steht es vor den Zahlen.

### Warum M1s Vorrichtung stehenblieb, und was hier anders ist

M1 öffnete jede Sitzung mit einem eigenen `podman exec` **von außen**. Jeder
dieser Aufrufe ist ein vollständiges podman-Programm auf der Maschine. Die
Vorrichtung wuchs also genauso schnell wie der Messgegenstand, und bei etwa 150
war sie zuerst am Ende.

Hier startet **ein einziger** `podman exec` einen Streuer **innerhalb** des
Containers, und der Streuer gabelt die N Sitzungen. Der Preis der Vorrichtung
ist damit von N unabhängig. Der Streuer benutzt nach dem Start nur noch
Shell-Bordmittel: kein Befehl in der Buchführung gabelt, also ist ein
fehlgeschlagenes Gabeln immer das einer Sitzung und nie eines der Vorrichtung.

### Die Zurechnung, vorher festgelegt

Jeder Lauf endet in **genau einem** Urteil, und das Urteil fällt, **bevor** eine
Zahl gedruckt wird:

| Urteil | Woran man es merkt |
|---|---|
| **MEASURAND** — der Container hat aufgegeben | Der Streuer lebt und meldet ein fehlgeschlagenes Gabeln; **oder** die pids-cgroup zählt eine Bremsung (`pids.events max` gestiegen); **oder** die memory-cgroup hat getötet (`memory.events oom_kill`); **oder** gesetzte Sitzungen sind gestorben, während der Streuer lebte. |
| **RIG** — die Vorrichtung hat aufgegeben | Der Streuer kam nicht hoch; **oder** der eine Kanal endete vor der Bestätigung; **oder** es stehen weniger Sitzungen als verlangt, **ohne** dass ein Gabelfehler aufgezeichnet wurde. |
| **UNCLEAR** | Die cgroup war nicht lesbar, **oder** die Zeugen widersprechen sich: die cgroup sieht weniger Last, als der Streuer behauptet. |

**Nur MEASURAND druckt eine Zahl.** RIG und UNCLEAR drucken „NO NUMBER" und
warum, und gehen mit einem Fehlercode.

**Drei voneinander unabhängige Zeugen**, und das ist nicht Zierde: die
pids-cgroup und die memory-cgroup, beide auf dem Wirt gelesen, ganz ohne exec —
und der Bericht des Streuers von innen.

Warum drei und nicht einer, gemessen: bei einem Deckel von 100 Aufgaben meldete
der Streuer `placed=32 alive=32`, während die cgroup **565 Bremsungen** zählte.
Das Wort des Streuers allein wäre ein falsches Grün gewesen.

### Die Vorrichtung gegen sich selbst geprüft

Zu jeder Richtung ein sterbender Mutant **und** eine lebende Kontrolle:

| Mutant | Eingriff | Urteil | Kontrolle daneben |
|---|---|---|---|
| **A** sterbend | Deckel auf 100 gesenkt, 32 Sitzungen | **MEASURAND** `pids-cgroup-throttled`, 565 Bremsungen | derselbe Lauf, Deckel 3466: **MEASURAND** `all-sessions-stood`, 0 Bremsungen |
| **C** sterbend | Streuer weggenommen | **RIG** `spawner-never-came-up`, keine Zahl | Streuer zurück: Zahl |
| **B4** sterbend | Streuer setzt nur die Hälfte und bestätigt nie | **RIG** `spawner-still-placing-after-240s`, keine Zahl — **obwohl 16 Sitzungen oben und lebendig waren** | Streuer zurück: **MEASURAND**, Zahl |

Und einer, der im Feld von selbst eintrat und darum mehr wert ist als die drei
gestellten:

| Lauf | Was geschah | Urteil |
|---|---|---|
| **352 und 384 Sitzungen**, je zweimal | Der Speicher ging aus, `oom_kill=1` — und das Opfer des Kerns war **die Vorrichtung selbst** | **RIG** `exec-channel-died-before-done`, **keine Zahl** |

Genau der Fall, den M1 nicht trennen konnte, und die Vorrichtung hat ihn selbst
festgestellt und sich geweigert, eine Zahl zu nennen. **Für 352 und 384
Sitzungen steht hier deshalb keine Zahl**, und das ist das Ergebnis, nicht eine
Lücke in ihm.

Die Vorrichtung liegt in `/var/tmp/m-last64/` auf svc-2 (`last64.sh`, der
Streuer `spawn.sh` und `sicht.sh` für das, was der Benutzer sieht); sie ist
nicht im Repo, weil sie an dieser Maschine hängt.

---

## 2 · Die Strecke über 64 hinaus

Die Anlage, wie sie heute ist: Deckel 3466 (siehe unten, das ist der echte),
Speicher auf 2400 MiB begrenzt — nicht aus Vorsicht gegen den Messgegenstand,
sondern damit der Kern bei Speichermangel **in meiner** cgroup tötet und nicht
den echten Controller des Knotens erschlägt.

| Sitzungen | Aufgaben | Speicher | frei auf der Maschine danach | Urteil |
|---|---|---|---|---|
| 1 | 21 | 22 MiB | | stehen |
| 8 | 86 | 71 MiB | | stehen |
| 32 | 309–311 | 257 MiB | | stehen |
| 48 | 459–461 | 366–374 MiB | | stehen |
| **64** | **619** | **500 MiB** | 1995 MB | stehen |
| 128 | 1213 | 976 MiB | 1492 MB | stehen |
| 192 | 1836 | 1450 MiB | 1012 MB | stehen |
| 256 | 2402 | 1914 MiB | 523 MB | stehen |
| **320** | **3034** | **2205 MiB** | **262 MB** | **stehen** |
| 352 | — | — | — | **keine Zahl** (Speicher aus, Opfer war die Vorrichtung) |
| 384 | — | — | — | **keine Zahl** (dito, zweimal nachgefahren) |

**320 gleichzeitige echte Terminal-Sitzungen standen.** Das ist die Zahl, und
sie ist das Fünffache dessen, was M1s Vorrichtung erreichen konnte.

Je Sitzung: **rund 9,4 Aufgaben und rund 7 MiB.** Gleichmäßig über die ganze
Strecke, kein Knick.

**Gegenprobe gegen M1:** M1 zählte bei 64 Sitzungen 586–590 Aufgaben, hier sind
es 619. Die rund 30 mehr sind das podman-Programm und der Streuer, die in
derselben cgroup liegen. Zwei Maschinen, zwei Fassungen, derselbe Zusammenhang —
M1s Gesetz von rund 9,1 Aufgaben je Sitzung hält.

**Was bei 352 kippte, und was nicht:** der Prozessdeckel war es nicht. Bei 320
Sitzungen standen 3034 von 3466 Aufgaben — der Deckel war nie erreicht, und
`pids.events` zählte null Bremsungen. Ausgegangen ist der **Speicher**. Die
Maschine kippt also an einer anderen Grenze als der, die M1 untersucht hat.

### Was der Benutzer in dem Moment sieht

Gemessen, indem zur stehenden Last **eine weitere echte Sitzung** in derselben
cgroup geöffnet und mitgeschrieben wurde, was sie ihrem Benutzer sagt.

| Lage | Was die neue Sitzung sagt | Was mit den offenen geschieht |
|---|---|---|
| 8 Sitzungen, Deckel 512 (Kontrolle) | die Eingabeaufforderung `#` — eine echte Shell | nichts |
| **56–60 Sitzungen, Deckel 512, 180–206 Bremsungen** | **die Eingabeaufforderung `#`** | **sie werden still ausgehungert** |
| **320 Sitzungen, Speicher aus** | **die Eingabeaufforderung `#`** | **der Kern tötet eine laufende Sitzung** |

**Das ist der unangenehmste Befund des Nachtrags: am Deckel sagt NGC niemandem
etwas.** Die neue Sitzung öffnet, der Benutzer bekommt seine Shell. Was
stattdessen passiert, steht in den Zahlen: bei 50 Sitzungen standen 472
Aufgaben, bei 60 nur noch 469 — bei 206 abgewiesenen Bremsungen. Das Wachstum
hört auf, die Sitzungen oberhalb von etwa 52 sind **unvollständig**: sie
existieren, haben aber nicht alle Fäden bekommen, die sie wollten. Keine
Meldung, keine Zeile im Protokoll, kein Satz für den Benutzer.

Erst wenn der Speicher ausgeht, merkt einer etwas — und dann ist es nicht der,
der zu viel geöffnet hat, sondern der, dessen Sitzung der Kern sich aussucht.
Welchen Satz dieser Benutzer dabei liest, ist **nicht gemessen**: in meinen
Läufen traf der Kern die Last und die Vorrichtung, nie eine Sitzung, die ich
mitgeschrieben hätte. Aus dem Quelltext ist es „Die Verbindung zum Terminal ist
abgebrochen." (`terminal.msgCut`), aber das ist gelesen und nicht gesehen.

---

## 3 · Drei Grenzen, und nur eine ist der Prozessdeckel

### Grenze des Containers — **heute gibt es keine**

M1 las 2048 ab und nannte sie podmans Voreinstellung. Inzwischen setzt NGC
selbst `--pids-limit 512` (`runtime.PIDsLimit`, Commit 76203fa). Beides ist auf
dieser Anlage **ohne Wirkung**, und das ist gemessen am **laufenden** Container
der Terminal-App:

- `podman inspect` antwortet `pids=512`, `mem=268435456`, `cpu=1000000000`. Der
  Controller fragt also richtig danach.
- Derselbe `inspect` antwortet `CgroupPath = /system.slice/ngc-controller.service`.
  **Der Container hat keine eigene cgroup.** Unter dem Dienst liegt keine
  Untergruppe.
- In dieser cgroup steht `pids.max = 3466`, `memory.max = max`, und `cpu.max`
  gibt es dort überhaupt nicht.
- Der Init-Prozess des Containers (`/ngc/ngc-hold hold`) liegt nach
  `/proc/<pid>/cgroup` in `/system.slice/ngc-controller.service`.

**Also: `--pids-limit 512`, `--memory 256m` und `--cpus 1` werden angefragt und
nicht durchgesetzt.** Die Ursache steht in podmans eigener Warnung beim Laden:
`Failed to add pause process to systemd sandbox cgroup: dbus: couldn't determine
address of session bus`. podman läuft wurzellos mit `--cgroup-manager cgroupfs`,
dem App-Benutzer ist keine cgroup übertragen, und ohne Sitzungsbus kann podman
keine anlegen. `loginctl list-users` zeigt nur `root`; der App-Benutzer hat kein
`linger`.

Dazu ein zweiter, davon unabhängiger Weg an derselben Grenze vorbei, ebenfalls
gemessen: **die Prozesse eines `podman exec` landen in der cgroup des
Aufrufers, nicht in der des Containers.** Nachgesehen an `/proc/<pid>/cgroup`:
der Init des Containers lag in `m64scope.scope`, die Sitzungsprozesse lagen in
der cgroup dessen, der den exec rief. In Betrieb ist dieser Aufrufer
`ngc-controller.service`. Eine Sitzung zählt also **nie** gegen den Container,
auch dann nicht, wenn der Container eine eigene cgroup hätte.

**Und wenn die 512 je greifen: sie reißen unter 64.** Gemessen in einer cgroup
mit `TasksMax=512`:

| Sitzungen | Aufgaben | Bremsungen | Urteil |
|---|---|---|---|
| 32 | 306 | 0 | stehen |
| 48 | 461 | 0 | stehen |
| 50 | 472 | 0 | stehen |
| **52** | **495** | **0** | **stehen** |
| **54** | 485 | **100** | **gebremst** |
| 56 | 462–478 | 180–220 | gebremst |
| 64 | 470 | 255 | gebremst |

**Die Grenze liegt bei 53 Sitzungen.** `wire.MaxSessionChannels` ist 64 — so
viele Kanäle darf **ein einziger Browser** offen haben. Sobald die cgroup
einmal funktioniert, reißt ein einzelner Benutzer den Deckel seiner eigenen App
allein, und zwar ohne eine Meldung zu bekommen.

### Grenze der Maschine — **Speicher, zwischen 320 und 352 Sitzungen**

320 Sitzungen standen bei 2205 MiB und 262 MB freiem Speicher auf einer Maschine
mit 2914 MiB. Bei 352 war er aus. Der Prozessdeckel war zu dem Zeitpunkt nie
erreicht (3034 von 3466).

Diese Grenze hängt an **dieser** Maschine: 2 Kerne, 2914 MiB. Auf der
64-GiB-Maschine von M1 liegt sie an anderer Stelle. Übertragbar ist nur der
Preis je Sitzung: **rund 7 MiB und rund 9,4 Aufgaben.**

### Grenze der Anlage — **128 Kanäle je Controller**

`serve.DefaultMaxApps = 128`: so viele App-Kanäle führt ein Controller
gleichzeitig. Der 129. wird in `openApp` **vor** dem Handschlag abgewiesen, ohne
Noise-Rechnung, ohne Pseudoterminal, ohne Prozess im Container, mit `E_LIMIT` —
und dessen deutsche Zeile ist **„Zu viele offene Verbindungen."**

**Nur aus dem Quelltext, nicht gemessen.** Was gemessen ist: in
`/etc/ngc/controller.json` auf svc-2 steht kein eigener Wert, der Standard gilt
also. Durch echte Kanäle ist die 128 hier nicht nachgefahren — dafür bräuchte es
die Oberfläche und 129 gleichzeitige Kanäle.

### Wie die drei zueinander stehen

```
   53          64             128              320        352
   |           |              |                 |          |
Container   ein Browser   Anlage, je        Maschine,   Speicher
(512, wenn  darf so       Controller        gemessen     aus
er greift —  viel         (aus dem
heute       (MaxSession-  Quelltext)
nicht)      Channels)
```

Die beruhigende Hälfte: **128 < 320.** Die Anlage bremst, bevor die Maschine
kippt. Der Controller schützt den Knoten, und zwar an der billigsten Stelle —
vor jedem Prozess.

Die unangenehme Hälfte: **53 < 64 < 128.** Zwischen der Grenze des Containers
und dem, was ein Browser darf, liegt kein Puffer, sondern ein Minus. Der Deckel
ist heute wirkungslos und fällt darum nicht auf. Wer ihn in Ordnung bringt —
cgroup übertragen, `--cgroup-manager systemd`, `linger` für den App-Benutzer —
bringt damit gleichzeitig einen Fehler in Betrieb, den es vorher nicht gab.

---

## Was das für M1 heißt

**Die Lücke ist geschlossen, und sie hat eine andere Antwort gebracht als
erwartet.**

| M1 sagte | Nachtrag |
|---|---|
| „Der Deckel ist 2048." | Auf der verteilten Anlage ist er **3466**, und er gehört nicht dem Container, sondern dem Dienst `ngc-controller.service`. |
| „Der Deckel ist nicht geteilt. Er gilt je App." | **Auf dieser Anlage ist er geteilt** — zwischen allen App-Containern **und** dem Controller selbst, weil keiner der Container eine eigene cgroup hat. |
| „NGC setzt `--pids-limit` nirgends." | NGC setzt es inzwischen auf 512 (Commit 76203fa). Es wird angefragt und **nicht durchgesetzt**. |
| „Über 64 Sitzungen hinaus fehlt die Messung." | **320 standen**, gemessen. Darüber keine Zahl, weil die Vorrichtung dort selbst das Opfer war — und das sagt sie. |

M1 ist damit nicht falsch gemessen. M1 hat auf der **Einzel-VM** gemessen, wo
podman offenbar eine cgroup anlegen konnte; dort gilt sein Befund. Die
verteilte Anlage ist anders eingerichtet, und die Zahlen gelten je Anlage. Das
ist derselbe Punkt, den M1 im Nachtrag „Die beiden Anlagen und ihre Namen"
schon macht — nur trifft er hier nicht bloß die Namen, sondern das Ergebnis.

### Was das für Frage 20 heißt

**Die Entscheidung „kein Prozessdeckel je App in v1.3" bleibt richtig, aber ihre
Begründung ist hinüber.**

M1 entschied so, weil der Deckel gemessen **je App** gilt und darum nichts zu
trennen wäre. Das stimmt auf der verteilten Anlage nicht: dort ist er geteilt,
und eine App am Anschlag kann die Nachbarin und den Controller mitnehmen. Der
Grund, aus dem M1 die Frage schloss, trägt also nicht mehr.

Die Entscheidung trägt trotzdem, aus einem anderen Grund, und der ist stärker:
**ein Feld im Manifest würde nichts bewirken.** Was NGC heute anfragt — 512 —
kommt nicht an. Eine zweite Zahl, die genauso nicht ankommt, ändert nichts.
**STABILITÄT** zeigt nicht auf ein Feld, sondern auf die cgroup: erst muss ein
Deckel überhaupt greifen, dann ist zu reden, wie hoch er ist.

### Aufträge, hier nicht gebaut

In dieser Reihenfolge, und die Reihenfolge ist der Punkt:

1. **Die cgroup in Ordnung bringen, damit die drei Schranken überhaupt
   greifen** (`--cgroup-manager systemd`, cgroup an den App-Benutzer
   übertragen, `linger`). Heute sind `--pids-limit`, `--memory` und `--cpus`
   drei Zeilen, die podman notiert und niemand durchsetzt. Eine App, die zu
   viel Speicher nimmt, nimmt ihn heute der Maschine.
2. **Erst danach `PIDsLimit` heben.** 512 reißt bei 53 Sitzungen, und ein
   einzelner Browser darf 64.
3. **Das Reißen sichtbar machen.** Gemessen: am Deckel sagt NGC niemandem
   etwas. Weder die gebremste Sitzung noch das Protokoll noch der Benutzer
   erfährt es. Das ist derselbe blinde Fleck wie bei den MAC-Fehlversuchen aus
   M3, nur an einer anderen Stelle.
4. **Die 128 durch echte Kanäle nachfahren.** Sie steht hier nur aus dem
   Quelltext, und sie ist die Grenze, die der Benutzer wirklich zuerst trifft.

### Was gemessen und was abgewogen ist

**Gemessen** (svc-2, 24.10.2026, Container `m64c`, cgroup `m64load.scope`): die
Tabelle bis 320 Sitzungen samt Preis je Sitzung; die Grenze 53 unter einem
Deckel von 512; dass der laufende Container keine eigene cgroup hat und
`--pids-limit`/`--memory`/`--cpus` darum nicht greifen; dass exec-Prozesse in
der cgroup des Aufrufers landen; dass am Deckel keine Meldung entsteht und die
neue Sitzung trotzdem öffnet; dass bei 352 und 384 der Speicher ausgeht und die
Vorrichtung dort das Opfer ist; die vier Mutanten samt ihren lebenden
Kontrollen.

**Nur aus dem Quelltext**, nicht gemessen: `DefaultMaxApps = 128` samt der
Zeile „Zu viele offene Verbindungen."; und der Satz, den ein Benutzer liest,
dessen Sitzung der Kern wegnimmt.

**Abgewogen, nicht gemessen:** dass die Entscheidung zu Frage 20 stehen bleibt;
die Reihenfolge der vier Aufträge; dass M1s Befund auf der Einzel-VM richtig
ist und nur nicht übertragbar — gemessen ist die verteilte Anlage, die
Einzel-VM ist in diesem Lauf aus und wurde nicht angesehen; und dass
128 < 320 ein Schutz ist und nicht Zufall — gemessen sind die beiden Zahlen,
nicht die Absicht dahinter.

**Nicht geschlossen:** die zweite echte App fehlt weiter. Alles oben ist an
**einer** App gemessen, der Terminal-App. Was eine andere braucht, ist nach wie
vor unbekannt.
