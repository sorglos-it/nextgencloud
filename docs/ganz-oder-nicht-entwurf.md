# Entwurf: soll NGC alles mitnehmen, wenn ein Teil wegbricht?

Grundlage sind die Messungen in `docs/ganz-oder-nicht.md` (Unit-Dateien der
Anlage, 30.10.2026; M5 am Code) sowie `docs/ausfall-je-maschine.md` (24.10.)
und `docs/ausfall-nachtrag.md` (25.10.). Die Rangfolge für jede Abwägung hier
ist **Sicherheit > Stabilität > Geschwindigkeit**.

Dieses Blatt baut nichts. Es legt die Wege nebeneinander, mit Nutzen, Kosten
und Schaden, und nennt am Ende eine Empfehlung mit Begründung.

## Das Problem in einem Satz

Zwischen den NGC-Diensten besteht **keine** Kopplung (gemessen: kein
`BindsTo=`, kein `PartOf=`, kein `Requires=` auf einen Nachbarn in acht Units
auf sechs Knoten), und es gibt **keine** Stelle, die die Anlage als Ganzes
beurteilt (gemessen: null Treffer für `healthz`/`readyz`/`degraded` über den
ganzen Baum, Grundmenge geprüft). Darum kann die Anlage „heil" antworten,
während ein Teil fehlt — und zweimal hat sie genau das getan:

- **svc-1, Controller aus:** alle App-Container weg, **40/40** Anmeldungen gut,
  Oberfläche `200`.
- **api-2, API aus:** 6 von 40 Anmeldungen enden in einer Apache-Seite, die
  nicht von NGC ist, und **kein** Protokoll schreibt eine Zeile.

In drei von fünf gemessenen Fällen war „sieht heil aus" dagegen die **richtige**
Auskunft: es fehlte wirklich nichts. Das ist der Grund, warum die naheliegende
Lösung die falsche sein könnte.

## Der Satz, der vor jeder Wegwahl kommt

**Diese Entscheidung steht nirgends aufgeschrieben — sie steht in der
Abwesenheit von vier Zeilen.**

CLAUDE.md dieses Repos sagt: was nicht im Repo steht, gilt nicht. Also ist der
heutige Zustand **keine Entscheidung, sondern ein Zufall, der sich wie eine
benimmt**: niemand hat je geschrieben „die NGC-Dienste sind absichtlich
entkoppelt", und niemand hat je geschrieben, dass sie gekoppelt sein sollen.
Die Entkopplung ist das, was systemd ohne Zutun tut.

**Darum gilt, unabhängig davon, welcher der drei Wege gewählt wird: die
Kopplung — oder ihr Fehlen — muss als Satz in `docs/` stehen.** Fällt die Wahl
auf „nichts koppeln", ist das ein Satz, der geschrieben werden muss, und nicht
der Zustand, in dem man schon ist. Das ist die einzige Zeile dieses Blatts, die
in jedem Fall fällig ist, auch wenn an der Anlage nichts geändert wird.

## Weg A — alles mitnehmen (die Herkunfts-Idee)

Jeder Knoten bekommt eine Kopplung, die den ganzen Knoten abräumt, sobald ein
Teil fehlt. In systemd sind das wenige Zeilen, zum Beispiel auf api-1
`BindsTo=mariadb.service` an `ngc-api`, oder umgekehrt `PartOf=` plus eine
Einheit, die alle drei zusammenfasst.

**Nutzen.** Der halb laufende Zustand verschwindet, und zwar vollständig. Ein
Benutzer erlebt entweder eine Anlage, die alles kann, oder eine, die nicht
antwortet — und beim Nichtantworten liegt die Deutung beim Browser, der sie
richtig macht. Die beiden Befunde oben wären damit beide weg. Kein neuer Code,
keine neue Schnittstelle, kein neuer Angriffspunkt: Konfiguration, die systemd
seit Jahren trägt.

**Kosten.** Jeder kleine Fehler wird zu einem ganzen Ausfall. Der gemessene
Fall B3 ist das Gegenbeispiel: `mariadb` auf db-1 aus, zwei Knoten hielten das
Quorum, **40/40** Anmeldungen gut, keine Delle — die Anlage hat den Ausfall
nicht einmal gemerkt. Unter Weg A hätte derselbe Stopp den Controller auf db-1
mitgerissen, und für nichts. Ebenso B2: `ngc-api` auf api-1 aus, api-2 trug,
40/40 — unter Weg A wäre der einzige Apache mitgegangen und die ganze Anlage
unten gewesen. **Weg A macht aus zwei gemessenen Nicht-Ausfällen zwei echte.**

**Schaden.** Zwei, und der zweite ist der schwerere.

1. **Die Neustart-Schleife.** Aus den gemessenen Zahlen:
   `RestartSec=5` setzt die Starts auf 0, 5, 10, 15 … s, in jedes Fenster von
   `StartLimitIntervalUSec=10s` fallen höchstens 3, und `StartLimitBurst=5`
   wird nie erreicht. Der Riegel gegen die Schleife greift heute also **nicht**
   (gerechnet, nicht gemessen — das ist M2). Koppelt man die Dienste, zieht ein
   einzelner Dienst, der sich nicht mehr starten lässt, den ganzen Knoten im
   Fünf-Sekunden-Takt mit. Das ist schlimmer als der heutige Zustand, nicht
   besser: aus „die Hälfte fehlt still" wird „alles flackert".
2. **Der Eingang ist einer.** `ngc.mein.lan` löst nur auf api-1 auf, und nur
   api-1 hat einen Apache (gemessen 24.10.). Eine Kopplung auf api-1 ist damit
   keine Kopplung eines Knotens, sondern eine Kopplung **der Anlage**. Weg A
   ist auf fünf von sechs Knoten harmlos und auf dem sechsten ein einzelner
   Punkt, dessen Ausfall alles mitnimmt — genau die Stelle, die der Auftrag als
   schon bekannt nennt.

**Und das ist die Stelle, an der eine Idee aus einem Durchsatz-Wettbewerb an
einer echten Anlage scheitert.** Dort läuft ein Programm mit mehreren Prozessen
auf einer Maschine, und „stirbt einer, bringt der Elternprozess die anderen um"
ist richtig, weil die Prozesse ohne einander nichts können. Hier sind es sechs
Maschinen mit verschiedenen Aufgaben, von denen drei einen Ausfall des Nachbarn
gemessen **überlebt** haben. Die Regel ist nicht falsch — sie gilt für einen
Aufbau, der dieser nicht ist.

## Weg B — den halben Zustand sichtbar machen, nicht verbieten

Nichts wird abgeräumt. Stattdessen bekommt die Anlage, was ihr fehlt: eine
Stelle, die sagt, dass ein Teil weg ist. Drei Stücke, von innen nach außen:

1. **Eine Vollständigkeits-Auskunft in `ngc-api`**, und zwar mit genau zwei
   möglichen Antworten: **„vollständig"** oder **„ein Teil fehlt"**. Welcher
   Teil, wie viele, welche Fassung — nichts davon, und nur für den
   **angemeldeten Verwalter**. Das ist eine Vorgabe des Koordinators und keine
   Anregung: nach Sicherheit > Stabilität heißt „sichtbar machen" nicht
   „ausplaudern". Eine Auskunft, die „die Datenbank ist weg" sagt, nennt dem
   Unangemeldeten den Moment, in dem Anmeldeversuche nicht mitgeschrieben
   werden.
2. **Eine Lebensprobe im Lastverteiler**, damit ein totes API-Mitglied draußen
   bleibt. Das behebt gemessen die 6 von 40 `500`-Antworten: `retry=5` holt das
   tote Mitglied heute alle fünf Sekunden zurück, und `failontimeout=On` greift
   bei einer **abgelehnten** Verbindung nicht.
3. **Ein Satz in der Oberfläche**, wenn die Auskunft „unvollständig" sagt — an
   derselben Stelle, an der `login.js:449` heute schon am Feld `code` und nicht
   am Ausgang entscheidet. Diese Stelle ist gemessen ehrlich; sie bekommt nur
   einen zweiten Fall.

**Nutzen.** Jeder der fünf gemessenen Fälle wird besser, keiner schlechter. B3
und B2 bleiben die Nicht-Ausfälle, die sie sind. svc-1 wird zum ersten Mal
sichtbar. api-2 verliert die Apache-Seite.

**Kosten.** Deutlich mehr als Weg A: neuer Code in drei Teilen (`ngc-api`,
Apache-Konfiguration im ulh-Repo, `apps/web`), je mit Tests, Fuzz dort, wo ein
Parser entsteht, und eine Vertragsfrage, sobald die Auskunft über die Leitung
geht — und `shared/` ändert nur der Koordinator.

**Schaden.** Drei, ehrlich benannt:

1. **Eine Auskunft über den Zustand der Anlage ist eine Auskunft für Angreifer.**
   „Die Datenbank ist weg" nennt dem Unangemeldeten den Moment, in dem
   Anmeldeversuche nicht mitgeschrieben werden. Darum die Einschränkung in
   Stück 1 — aber sie ist eine Einschränkung, keine Lösung, und sie muss am
   Wortlaut der Antwort gemessen werden, nicht am Entwurf.
2. **Eine Lebensprobe ist eine Last und ein Weg nach innen.** Sie fragt im
   Takt, sie braucht einen Pfad ohne Anmeldung, und ein solcher Pfad ist das,
   was man an einem Eingang am wenigsten will.
3. **Der halbe Zustand bleibt.** Weg B macht ihn sichtbar; er hört nicht auf zu
   existieren. Wer die Oberfläche nicht liest, erlebt weiter eine Anlage, die
   die Hälfte tut. Weg B verlässt sich darauf, dass jemand hinsieht.

## Weg C — der kleine Weg, nur die gemessenen Lücken

Nicht als Kompromiss gedacht, sondern als die Teilmenge, für die eine Messung
**schon** vorliegt. Drei Posten, jeder einzeln entscheidbar:

1. **`apache2` auf api-1 bekommt `Restart=always`** (heute `on-abort`,
   Debian-Vorgabe). Der einzige Eingang der Anlage hat heute die schwächste der
   drei Neustart-Regeln. Eine Zeile.
2. **`nginx` auf store bekommt `Restart=on-failure`** (heute `no`). Der Laden
   kommt nach einem Absturz gemessen gar nicht zurück.
3. **Die Lebensprobe im Lastverteiler** (Stück 2 aus Weg B). Das ist der einzige
   der fünf gemessenen Fälle mit einer Antwort, die nicht von NGC stammt und
   die **nirgends** mitgeschrieben wird.

**Nutzen.** Jeder Posten behebt eine gemessene Lücke, keiner ändert das
Verhalten im gesunden Betrieb, und keiner braucht neuen Code in `shared/`.

**Kosten.** Gering, aber nicht null: die Units liegen im ulh-Repo
(`scripts/ngc-*.sh`), und jede ulh-Installation holt beim Start `main` — also
Pull Request dort, nicht hier, und nur geprüfte Skripte.

**Schaden.** Posten 1 und 2 **schalten den Riegel gegen die Schleife weiter
ab**: `Restart=always` auf einem Dienst, der sich nicht starten lässt, läuft
genau in das Flackern, das unter Weg A als Schaden steht. Das ist der Grund,
warum M2 vor diesem Posten gemessen werden muss und nicht nach ihm. Und:
Weg C lässt den Kernbefund **stehen** — svc-1 bleibt der Fall, in dem alle
Container weg sind und die Anlage 40/40 meldet.

## Empfehlung

**Weg C zuerst, dann Weg B. Weg A nicht.**

Begründung nach der Rangfolge:

- **Sicherheit.** Weg A fügt keinen Angriffspunkt hinzu und wäre damit der
  sauberste — aber er koppelt den einzigen Eingang der Anlage an ihre
  Nachbarn, und ein einzelner Punkt, dessen Ausfall alles mitnimmt, ist
  derselbe Schaden, den der Auftrag als bekannt bezeichnet. Weg B fügt einen
  Pfad ohne Anmeldung hinzu und ist darin der teuerste; die Einschränkung
  („vollständig / unvollständig", keine Teile­liste) ist der Preis, zu dem er
  tragbar wird, und sie ist am Wortlaut zu messen.
- **Stabilität.** Hier entscheidet eine gemessene Zahl: in **drei von fünf**
  Dienst-Stopps fehlte nichts. Weg A hätte in diesen drei Fällen aus einem
  Nicht-Ausfall einen Ausfall gemacht, zweimal davon den der ganzen Anlage.
  Das ist nicht eine Abwägung, das ist eine Verschlechterung an gemessenen
  Fällen.
- **Geschwindigkeit** spielt in keinem der drei Wege eine Rolle und steht hier
  nur, damit die Rangfolge vollständig ist.

**Reihenfolge, und warum sie so ist:** erst M2 messen (greift der Riegel gegen
die Schleife wirklich nicht?), denn Weg C Posten 1 und 2 verschärfen genau
diesen Punkt. Ergibt M2, dass die Schleife ungebremst läuft, gehört
`StartLimitBurst`/`StartLimitIntervalUSec` **vor** `Restart=always` berichtigt
— sonst behebt Posten 1 einen Ausfall und baut ein Flackern.

**Was offen bleibt und die Empfehlung ändern könnte:** M3 (Waisen nach einem
Absturz des Controllers). Laufen die Container als Waisen weiter, ist der
schwerste Fall nicht die fehlende Kopplung, sondern **zwei Verwalter für einen
Container** — und dann steht hier ein anderer Entwurf, in dem der Controller
seine Container beim Start wiedererkennen oder abräumen muss. Darum ist M3 die
Messung, die der Zustimmung am meisten wert ist.

---

# Nachtrag vom 30.10.2026: M3 verschiebt die Rangfolge

Die drei Wege oben standen, bevor M3 gefahren war. Gemessen ist jetzt
(`docs/ganz-oder-nicht.md`, M3): ein `kill -9` auf den Controller lässt die
App-Container als Waisen laufen, **und** der nach fünf Sekunden
zurückkehrende Controller legt eigene daneben. Vier lebende `conmon`, zwei je
App, beide Paare in derselben cgroup, und nach außen war **nichts** zu sehen:
Oberfläche und Anmeldung durchgehend `200`.

**Das ist kein Stabilitätsposten mehr.** Ein Container, den kein Verwalter
kennt, läuft mit den Rechten seines App-Benutzers weiter, und die Oberfläche
kann ihn nicht zeigen. Nach der Rangfolge Sicherheit > Stabilität >
Geschwindigkeit steht er damit **vor** allem, was oben steht.

## Was sich an der Empfehlung ändert

**Neuer Posten 0, vor Weg C:** der Controller muss beim Start die Container
seiner Apps **wiedererkennen oder abräumen**, bevor er neue anlegt. Welches von
beiden, ist eine Entscheidung und keine Technik: wiedererkennen hält die App
durchlaufend und ist das Freundlichere, abräumen ist das Einfachere und kostet
den Benutzer einen Neustart seiner App. Gemessen ist nur, dass heute **keines
von beiden** geschieht.

**Weg C Posten 1 und 2 werden dadurch gefährlicher, nicht harmloser.**
`Restart=always` auf einem Dienst bedeutet: jeder Absturz kommt zurück, und
nach M3 heißt „kommt zurück" am Controller **„legt noch einen Satz Container
an"**. Ob jeder weitere Absturz noch einmal verdoppelt, ist nicht gemessen
(dort angehalten) — aber solange das offen ist, ist eine schnellere Rückkehr
keine Verbesserung. **Posten 0 kommt vor Posten 1.**

**Weg A sieht in diesem einen Licht besser aus, und bleibt doch abgelehnt.**
Hätte der Knoten eine Kopplung, die beim Tod des Controllers alles auf svc-1
abräumt, gäbe es die Waisen nicht. Das ist das stärkste Argument für die
Herkunfts-Idee, das diese Messreihe erbracht hat, und es gehört hierhin. Es
ändert die Empfehlung trotzdem nicht: die drei gemessenen Nicht-Ausfälle (B2,
B3, B5) bleiben, und Weg A macht aus ihnen Ausfälle. Posten 0 behebt die Waisen
an der Ursache — ein Verwalter, der seine Container nicht wiedererkennt — statt
sie durch einen größeren Ausfall zu verdecken. **Eine Kopplung, die ein
Aufräumproblem löst, löst es, indem sie mehr kaputt macht.**

**Weg B wird durch M3 wichtiger.** „Zwei Instanzen je App laufen, die
Oberfläche zeigt eine" ist genau der halbe Zustand, den Weg B sichtbar machen
soll — und er ist jetzt ein gemessener Fall und kein gedachter. Die Vorgabe des
Koordinators bleibt: nach außen nur „vollständig" oder „ein Teil fehlt", und
nur für den angemeldeten Verwalter.

## Reihenfolge, neu

1. **Posten 0** — der Controller erkennt seine Container wieder oder räumt sie
   ab. Sicherheit, und damit vor allem anderen.
2. **M2 messen** — greift der Riegel gegen die Neustart-Schleife? Offen,
   angehalten, und Voraussetzung für Posten 1 und 2.
3. **Weg C Posten 3** (Lebensprobe im Lastverteiler) — unabhängig von beidem
   und durch M1 bestätigt: die erste Anmeldung nach einem Absturz bekommt eine
   nackte Apache-Seite, in der kein Feld `code` steht.
4. **Weg C Posten 1 und 2** (`Restart=always`, `on-failure`) — erst nach 1 und 2.
5. **Weg B** — wenn Posten 0 steht.
6. **Weg A** — nicht.

Und der Satz aus „Der Satz, der vor jeder Wegwahl kommt" gilt unverändert und
wird durch M3 nur dringender: die Kopplung oder ihr Fehlen muss als Satz in
`docs/` stehen. Heute steht dort nichts, und ein Zufall, der sich wie eine
Entscheidung benimmt, hat gerade zwei Container je App erzeugt.

## Nachtrag 2: M2 ist gefahren, und M4 wird zur Abnahmemessung

**M2 ist gemessen** (`docs/ganz-oder-nicht.md`), und damit ist die Vorbedingung
erfüllt, die oben für Weg C Posten 1 und 2 gesetzt war. Das Ergebnis bestätigt
die Rechnung: **zehn Neustarts in 60 Sekunden, Zähler bis 11, kein Aufgeben.**
Der Riegel gegen die Neustart-Schleife greift heute nicht.

**Die Folge für Posten 1 und 2 ist damit nicht „geht jetzt", sondern ein
zusätzlicher Posten davor:** `StartLimitIntervalUSec` muss an `RestartSec`
angepasst werden, sonst fügt `Restart=always` der Anlage eine **unbegrenzte**
Schleife hinzu. Am Controller wäre das besonders teuer — nach M3b legt jeder
Controller-Start einen Prozesssatz dazu, und elf Neustarts in einer Minute
wären elf zusätzliche Sätze.

**M4 wandert aus der Messrunde in den Bauplan.** Entschieden vom Koordinator:
heute würde M4 den einzigen Eingang umlegen, um eine Unit-Zeile zu bestätigen,
die niemand bestreitet. Nach Posten 1 belegt dieselbe Störung etwas Neues —
dass die Behebung greift und der Apache von selbst zurückkommt. **M4 ist damit
die Abnahmemessung von Posten 1**, mit eigenem, vorher angesagtem Fenster und
mit `kill -TERM` vor `kill -9`, damit der geordnete und der harte Fall
auseinander bleiben.

Das ist eine Lehre über die Reihenfolge und nicht nur über diesen Fall: **eine
Störung, die nur bestätigt, was man schon weiß, ist teuer; dieselbe Störung
nach der Behebung beweist, dass sie wirkt.** Wer messen will, wartet mit der
teuren Messung, bis sie etwas Neues sagen kann.

## Reihenfolge, endgültig

1. **Posten 0** — der Controller erkennt seine Container wieder oder räumt sie
   ab. Sicherheit. Baut der Strang „Die Härtung wirklich durchsetzen"; die
   Fassung 0.3.0 gehört ihm.
2. **Posten 0b** — `StartLimitIntervalUSec` an `RestartSec` anpassen, damit es
   überhaupt einen Riegel gegen die Schleife gibt. Durch M2 belegt.
3. **Weg C Posten 3** — Lebensprobe im Lastverteiler. Durch M1 und M2 belegt:
   die scheiternde Anmeldung bekommt eine nackte Apache-Seite ohne Feld `code`.
4. **Weg C Posten 1 und 2** — `Restart=always` / `on-failure`, erst nach 1 und 2.
   Abnahme: **M4**.
5. **Weg B** — wenn Posten 0 steht.
6. **Weg A** — nicht. Nach M3b ist er schlechter als beim Schreiben: eine
   Kopplung, die bei jedem Fehler alles neu startet, treibt den Zähler aus M3b
   schneller hoch.
