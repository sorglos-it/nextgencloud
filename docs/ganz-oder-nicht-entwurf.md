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

## Weg B — den halben Zustand sichtbar machen, nicht verbieten

Nichts wird abgeräumt. Stattdessen bekommt die Anlage, was ihr fehlt: eine
Stelle, die sagt, dass ein Teil weg ist. Drei Stücke, von innen nach außen:

1. **Eine Vollständigkeits-Auskunft in `ngc-api`**, die nennt, welche Teile sie
   gerade erreicht (Datenbank, die Controller der Vertrauensliste, der Laden) —
   mit Namen und Zahlen, nie mit Werten, und **ohne Anmeldung nur als
   „vollständig / unvollständig"**, damit sie keine Landkarte der Anlage an
   Unangemeldete gibt.
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
