# Arbeitsweise: wie hier gebaut wird

Für jede Claude-Sitzung, die in diesem oder einem anderen Projekt Code schreibt
oder prüft.

Alles hier ist **gemessen**, nicht ausgedacht. Jede Regel steht, weil sie in NGC
Geld gekostet hat — Stand 24.09.2026: fünf Stränge, über dreißig Prüfrunden.
Die Fälle daneben sind echte Funde, keine Beispiele.

**Was hier nicht drinsteht:** wie eine NGC-App aufgebaut sein muss. Der
App-Baustein ist noch nicht gebaut; dazu gäbe es nur Vermutungen.

---

## 1. Was du zusagst, muss gemessen sein

Die mit Abstand häufigste Fehlerklasse. **Neun Fälle**, in denen ein Kommentar
das Gegenteil dessen behauptete, was der Code tut — **jedes Mal vom Prüfer
gefunden, nie von einem Test**. Tests prüfen Verhalten, nicht Prosa.

Zweimal drehte so ein Satz die Sicherheitsrichtung um („under-accepts", wo der
Code über-akzeptiert). Einmal nannte er eine Ursache, die den Fall gar nicht
erzeugt.

Das Tückische: **es passiert in genau dem Commit, der die Klasse reparieren
soll.** Vier Runden hintereinander haben je einen neuen Fall eingeschleppt,
während sie den alten behoben.

- Schreib in einen Kommentar nur, was du hast laufen lassen.
- Alles andere kennzeichne ausdrücklich als „noch zu prüfen".
- **Lies am Ende jeder Runde jeden Satz, den du angefasst hast, gegen den Code.**
  Ein Umsetzer hat so zwei eigene Fehler gefunden, bevor sie eingecheckt waren.
- Zahlen altern: „18 Mikrosekunden" gilt für **einen** Rechner. Schreib
  Verhältnisse, oder sag dazu, worauf gemessen wurde.
- **Und der tückischste Satz ist der über die eigene Arbeit.** Ein Umsetzer
  meldete, seine Runde sei „netto kleiner" geworden — nachgezählt war sie um
  34 Zeilen gewachsen. Er hat den Grund selbst benannt: er hatte die drei
  entfernten Namen gezählt und die 33 neuen nicht, und die größte neue Funktion
  als „kleine Sache aus vorhandenem Werkzeug" verbucht. **Das war ein Satz über
  seine Absicht, nicht über den Code** — und ich hatte ihn ungeprüft
  weitergegeben. Wer zusagt, etwas sei kleiner, schneller oder einfacher
  geworden, legt die zwei Zahlen daneben.

**Die Durchsicht ist ein eigener Arbeitsschritt, kein Nebenbei.** Ein Umsetzer
hat sie zum Schluss getrennt gemacht und dabei **sechs** weitere falsche Sätze
gefunden — zweimal dieselbe Klasse, im selben Commit, der sie reparieren
sollte. Wer sie nebenbei macht, findet sie nicht.

**Und wenn dieselbe Sache dreimal in Folge eine Runde braucht, ist meistens
nicht die Behebung falsch, sondern eine Zahl geraten.** Ein Leck wurde über drei
Runden geschlossen; jedes Mal stimmte die Behebung, und jedes Mal war die
**Anzahl der noch offenen Fälle** eine Schätzung, die der nächste Prüfer
widerlegte. Gelöst hat es erst, sie zu einer Konstante mit eigener Zusicherung
zu machen (`const openShapes = 3`), gegengeprüft in **beide** Richtungen: eine
Form mehr und eine weniger müssen den Test rot machen. Eine Zahl in Prosa ist
eine Behauptung; eine Zahl, an der ein Test hängt, ist eine Zusage.

**Und wenn du zählst, zähl nach der Eigenschaft, nicht nach dem Ort.** Dieselbe
Zahl wuchs von eins auf drei auf fünf auf acht. Der Ausweg war, die Fälle
**aufzuzählen** statt zu schätzen — aber die erste Aufzählung war **auch** falsch,
weil sie nach der Stelle im Code gebaut war („die Zweige **vor** der
Signaturprüfung") und die gesuchte Eigenschaft die **Kosten** sind. Eine Prüfung
kann mitten in der Signaturprüfung liegen und trotzdem nichts kosten: Go gibt
bei einer Signatur falscher Länge sofort `false` zurück, ohne die Nachricht
anzusehen. Nach Kosten aufgezählt waren es sieben statt fünf.

Was die Sache endgültig geschlossen hat, sind **zwei statische Zusicherungen**,
die **vor** jeder Messung laufen und deshalb nie flattern: jede gemessene Form
nennt einen Zweig der Liste, und **jeder Zweig der Liste hat eine Form**. Damit
ist die Zahl keine Zahl mehr, sondern eine Zuordnung — und ein Zweig, den
niemand aufgeschrieben hat, fällt auf, statt still zu bleiben.

**Zähl auch nach, ob dein Messgerät alle Fälle erreicht hat.** In derselben
Aufgabe beendete der erste Fehlschlag den Lauf — die letzten drei Formen wurden
deshalb **nie gemessen**, und die Tabelle im Bericht sah trotzdem vollständig
aus.

## 2. Grüne Tests beweisen nichts, wenn sie nicht rot werden können

Der teuerste Befund dieses Projekts: ein Prüfer hat einen absichtlich
**falschen** Baustein gebaut — einen, der erst liest und dann schreibt, wo es
eine einzige Anweisung sein muss. **Kein einziger Test des Repos hat es
bemerkt**: nicht der gemeinsame Vertragstest, nicht ein eigens geschriebener
Wettlauf mit 256 × 16 gleichzeitigen Zugriffen.

Zwei Gründe, und beide sind lehrreich:

- Der Vertragstest ließ beim Wettlauf das **Anlegen** konkurrieren, nie das
  **Verbrauchen**. Die Eigenschaft, um die es ging, war überhaupt nicht geprüft.
- Und die Testdatenbank **rettete** den falschen Baustein zufällig: der zweite
  Zugriff prallte ab, die Transaktion wurde wiederholt, und beim zweiten Anlauf
  stimmte das Ergebnis. Auf der Produktionsdatenbank wäre derselbe Code falsch.

**Bau den falschen Baustein und sieh nach, ob dein Test ihn fängt.** Eine
Mutation, die grün bleibt, ist der Beweis, dass die Prüfung fehlt. Dasselbe hat
an anderer Stelle gezeigt, dass eine Sperre fremde Sperren löschen könnte, ohne
dass ein Test rot wird.

## 3. Ein Test, dessen Voraussetzung nicht hergestellt ist, beweist nichts

Ein Pflichttest **hoffte** auf einen blockierten Schreibvorgang, statt ihn
herzustellen. Auf Windows entstand er, auf Linux schluckte ihn der Puffer nach
250 ms — zweimal rot, einmal grün, am selben Commit.

Danach: ein vorgeschalteter Weiterleiter sperrt die Richtung, der Fall entsteht
sicher. Fünf Läufe grün, Streuung 440 ms.

**Stell die Bedingung her. Und lass den Test laut scheitern, wenn sie doch nicht
entsteht** — sonst rutscht er still durch.

## 4. Eine Grenze ist erst belegt, wenn ein Fall beide Seiten festnagelt

Genau auf der Grenze muss es durchgehen, eins darüber abgelehnt werden. Ein
Test, der nur die Überschreitung prüft, bliebe auch grün, wenn die Grenze bei
null stünde.

Gefunden: ein Schutz gegen Zurückstellen der Uhr brach ab **genau zwei
Stunden** — der Test maß bei einer Stunde und einer Sekunde, also knapp
darunter. Neun Abstände später war es dicht.

### Eine weitergereichte Zahl ist keine gemessene Zahl

Eine Aufzählung wanderte durch fünf Hände: **drei** Stellen sagte der erste
Bericht. Der Koordinator schrieb „die drei Stellen" in seine Fundliste. Der
Umsetzer berichtigte zwei und fand die dritte nicht, weil sie nicht auf der Liste
stand. Der Prüfer fand eine **vierte** in einer Datei, die im Diff gar nicht
vorkam. Und als endlich jemand **zählte** statt zu übernehmen, waren es **sechs**
— zwei davon in Produktionscode, nicht in Tests.

Der unangenehmste war der sechste: er stand in genau dem Kommentar, den die
Runde davor bearbeitet hatte. Wer einen Text anfasst, liest ihn nicht.

Und der Zähler fand beim Zählen einen Fehler in seiner **eigenen** Arbeit: sein
Zusatz begann mit „beide Zählungen **oben**", während die dritte Behauptung
desselben Kommentars **unter** seinem Absatz stand. Er hatte mitten
hineingeschrieben und nur nach oben gesehen.

**Eine Zahl, die du nicht selbst gezählt hast, ist eine Behauptung wie jede
andere** — und sie hat die unangenehme Eigenschaft, unterwegs bestätigt zu
klingen: je öfter sie weitergereicht wird, desto sicherer wirkt sie, obwohl
niemand seither nachgesehen hat.

Deshalb: **wer eine Zahl weitergibt, sagt dazu, ob er sie gezählt oder übernommen
hat.** Und wer nach ihr handelt, zählt sie einmal selbst — es kostet eine
Minute und war hier sechsmal nötig.

Dazu gehört die Kehrseite, die derselbe Umsetzer geliefert hat: **sag, wo du die
Grenze gezogen hast.** Er hat acht Stellen ausdrücklich *nicht* mitgezählt und
sie einzeln benannt. Damit ist seine Sechs nachprüfbar statt geglaubt — und wer
die Grenze anders zieht, sieht sofort, worüber er streitet.

## 5. Miss, ob dein Messgerät überhaupt misst

Ein Zeittest verglich zwei Codewege und war „fünfzig Läufe ohne Flattern" grün.
Nachgemessen: **23 von 25 Einzelmessungen waren glatt null.** Die Uhr des
Rechners löst eine Millisekunde auf, gemessen wurden 30 Mikrosekunden.
`0 > 2*0` ist falsch — der Test konnte **nie** fehlschlagen.

Bei Zeitmessungen: Stapel von mindestens 2000 Läufen, verschränkte Arme, und
eine **Null-Kontrolle** (derselbe Arm zweimal). Oder ganz ohne Uhr bauen — das
ist fast immer möglich und immer besser.

**Und rechne die Nachweisgrenze aus, bevor du eine Kampagne fährst.** Ich habe
ein Paket 25-mal laufen lassen, um einen Fehlschlag auszuschließen, und „0 von
25" gemeldet. Die Rate war 1 zu 2000 — die Wahrscheinlichkeit, dass 25 Läufe
nichts finden, lag bei 99 %. Die Kampagne hat nichts entlastet und drei Viertel
einer Stunde gekostet; der richtige Griff waren 20 000 Läufe **eines** Tests in
zehn Sekunden.

Dieselbe Frage gilt für jeden nebenläufigen Test, der eingecheckt wird: **ab
welcher Maschine misst er noch?** Eine Wettlaufprobe fiel auf einer 64-Kern-Kiste
in jedem Lauf — und auf zwei Kernen in **keinem einzigen von zehn**. Auf dem CI-
Läufer und auf dem Einplatinenrechner, für die der Code gebaut wird, hätte sie
nie etwas gefangen, und ihr Kommentar behauptete das Gegenteil. Miss die
Untergrenze und **schreib sie hin**, statt sie stillschweigend zu unterschreiten.

**Eine Grenze, die unter Last nachgibt, ist keine.** Eine Warteschleife war als
„60 Sekunden" dokumentiert und zählte in Wahrheit **Durchläufe**: 600 mal
prüfen, dazwischen ein Zehntel Sekunde schlafen. Im Leerlauf kam das hin. Unter
Last wuchs sie mit — der Lauf, der rot hätte werden müssen, kam **grün nach 103
Sekunden** durch. Genau dann, wenn die Grenze gebraucht wird, war sie weg.

**Aber der naheliegende Schluss ist falsch.** „Also immer die Uhr" stimmt nicht.
Am selben Skript hingen vier weitere Durchlaufkonten, und dort ist das
Durchlaufkonto die **sichere** Form — gemessen und begründet:

- Keine dieser Meldungen nennt eine Zeit. Es kann also nichts auseinanderlaufen
  zwischen dem, was dasteht, und dem, was gilt.
- Ein Durchlaufkonto **dehnt** sich unter Last, eine Sekundengrenze **kappt**.
  Wo das Warten nur eine Anlaufverzögerung überbrückt, ist Dehnen richtig.

Die Regel ist also nicht „Uhr statt Durchläufe", sondern: **ein Durchlaufkonto
ist falsch, sobald etwas daneben eine Zeit behauptet.** Sagt niemand eine Zahl
zu, zählt ruhig Durchläufe — und schreib trotzdem hin, was du gemessen hast
(bei diesen vier: null Zusatzdurchläufe, 2–12 ms, Deckel tausendfach großzügig).

**Und beim Mutieren: eine Mutation, die nicht übersetzt, ist keine überlebende
Mutation.** Ein Prüfer baute eine Mutation, sah „kein Test rot" und hätte sie
als überlebend gemeldet — bis er nachschaute: sie hatte gar nicht gebaut,
`go test` war am Übersetzungsfehler gescheitert, und „kein Test rot" hieß „kein
Test gelaufen". Die kompilierende Fassung starb dann an zwölf Tests.

Dieses Messgerät irrt also **in die gefährliche Richtung**: es meldet eine Lücke
im Schutz, wo in Wahrheit nichts gemessen wurde — und eine gemeldete Lücke
kostet eine Runde, in der jemand einen Test für etwas baut, das längst geprüft
ist. Prüf bei jeder Mutation zuerst, dass sie **baut**, und zähl die Tests, die
gelaufen sind, nicht nur die, die rot wurden.

**Und `go test -overlay` misst nicht alles.** Ein Wächter, der den Quelltext
selbst liest — er parst sein Verzeichnis, statt den übersetzten Code zu prüfen —
sieht die Overlay-Datei gar nicht: er liest die Datei auf der Platte. Eine
Mutation gegen so einen Wächter meldet **grün**, und zwar zuverlässig, egal was
man ändert. Das ist derselbe Irrtum in dieselbe Richtung wie oben, nur leiser:
nichts bricht, nichts warnt, die Zahl stimmt scheinbar.

Wenn du einen Wächter misst, der Dateien liest, dann miss auf einer **Kopie**
(`git archive`), nicht über ein Overlay. Und prüf es einmal ausdrücklich nach:
mutier den Wächter so, dass er **sicher** anschlagen müsste. Bleibt er grün,
misst du die falsche Datei.

Auch die Werkzeuge daneben lügen. Zwei Fälle aus einer einzigen Runde:

- `git status` meldete eine geänderte Datei, die **nicht** geändert war. Der
  Umsetzer hat nicht geglaubt und nicht ignoriert, sondern den **Blob-Hash**
  verglichen: identisch mit `HEAD`, null Diffzeilen — ein Stat-Cache-Fehlalarm.
  Wer so etwas ignoriert, übersieht irgendwann eine echte Änderung; wer es
  glaubt, jagt ein Gespenst oder committet fremde Arbeit. Nachsehen ist billig.
- `go test -list` gab in einer Schleife **117** Tests, wo es 124 sind. Er hat
  gegengezählt und die abweichende Zahl gemeldet, obwohl sie folgenlos war.

Und wenn zwei Messungen sich widersprechen, ist die Frage nie „wer hat recht",
sondern **„was zählt welches Instrument"**. Zwei Prüfer maßen dieselben Kosten
und kamen auf 1 → 2 und auf 2 → 3. Beide waren sich über den **Aufschlag** einig
(+1, unabhängig von der Kettenlänge) und uneinig über den Ausgangswert — sie
zählten verschiedene Dinge als „Lesung". Wer eine Zahl in einen Kommentar
schreibt, schreibt dazu, **was** sie zählt; sonst ist der nächste Widerspruch
schon eingebaut.

Und Zahlen aus **zwei Aufbauten** gehören nicht in dieselbe Differenz. Der
Koordinator stellte in einer Fundliste drei Kosten nebeneinander — 10, 11 und 3 —
und schloss „sieben weniger". Nachgemessen auf **einer** Installation waren es 10,
11 und **1**: die 3 stammte von einer Installation mit drei Fassungen, die 10 und
11 von einer mit zwölf. Der Schluss stimmte zufällig in der Richtung und war in
der Zahl falsch, und aus der Fundliste wanderte er wörtlich in einen
Code-Kommentar.

Wer Zahlen vergleicht, sagt dazu, **worauf** gemessen wurde — und vergleicht nur,
was auf demselben Aufbau gemessen ist.

### Die gefährlichste Messung ist die richtige auf die falsche Frage

Ein Prüfer maß, dass ein bestimmtes Systemregister **deterministisch** ist: zwei
Firmware-Bauten, zwei Läufe, stabile und verschiedene Werte. Die Messung war
sauber, und sie widerlegte ein „geht nicht", zu Recht.

Der Koordinator zog daraus, dass dieses Register die beiden **Betriebszweige**
trennen könne — und lag damit falsch. Die Tabelle, die er später bestellte, zeigte
es: von acht Registern trennt genau **eines** die Zweige, und es ist nicht dieses.
Das gemessene folgt dem **Zertifikat**; beide Zweige sind mit demselben Schlüssel
signiert, also ist es in beiden gleich. Die erste Messung hatte
**Firmware-Bauten** unterschieden, nicht Zweige.

Beide Zahlen waren richtig. Zwischen ihnen lag eine Frage, die niemand
ausgesprochen hatte.

**Deshalb: schreib neben jede Zahl die Frage, die sie beantwortet** — nicht nur,
wie gemessen wurde (Regel 1), sondern **worauf sie eine Antwort ist.** Und bevor
du eine Messung für eine Entscheidung benutzt, für die sie nicht gemacht wurde,
bestell die Tabelle. Eine halbe Stunde Messen ist billiger als ein Entwurf, der
auf dem Simulator funktioniert und auf echter Hardware nicht aufgeht.

### Die Nachbarzusage misst mit — und deckt die deine zu

Der Koordinator verlangte eine Verhaltensprobe: zwei Schreiber gegeneinander,
und keiner darf denselben Namen bekommen. Die Form hatte in einem anderen Strang
sofort und stabil gefangen.

Hier fing sie **nie**. 32 Schreiber, 400 Runden, drei Läufe je Fassung — grün
gegen den richtigen Code **und grün gegen beide kaputten**. Der Grund: ein
eindeutiger Datenbankindex hält dieselbe Zusage allein. Nimmt man die Prüfung
heraus, läuft der Verlierer in den Index, die Wiederholung findet keine Zeile mit
seiner Kennung und antwortet mit **demselben Fehlerwort** — gleicher Zustand,
gleiche Antwort, nichts zu sehen.

**Eine Probe misst, was am Ende herauskommt, nicht wer es bewirkt hat.** Liegt
neben deiner Prüfung eine zweite Zusage, die dasselbe garantiert, misst du sie.

Der Umsetzer hat es so gelöst: in der Vorrichtung den **Index weggenommen** —
mit eigenem SQL, und zuerst gemessen, dass er wirklich weg ist. Danach braucht es
gar keinen Wettlauf mehr: ein **sequenzieller** zweiter Schreiber nimmt den
belegten Namen, 5 von 5 rot in 0,04 Sekunden. Die Prüfung steht dann allein da
und muss selbst tragen.

Also: **frag vor jeder Verhaltensprobe, was die Eigenschaft sonst noch halten
könnte, und nimm es für die Dauer der Probe weg.** Bleibt sie dann grün, hast du
die Antwort.

### Ein Test, der in die Uhr läuft, ist kein Zaun

Eine der Mutationen antwortete nicht falsch — sie hungerte den Verbindungspool
aus (vier Verbindungen gegen zweiunddreißig Wettläufer) und endete in
`panic: test timed out`. Das sieht aus wie ein Fang und ist keiner: ein Lauf, der
in die Zeitschranke rennt, sagt nichts über die Eigenschaft, er sagt nur, dass
etwas stehenblieb. Der Umsetzer hat es so benannt statt es als Fang zu verkaufen
— und den Fall dem statischen Prüfer überlassen, der ihn umsonst mitnimmt.

### Ein Punkt sagt nicht, ob sich etwas ansammelt

Eine Aufgabe sollte belegen, dass ein Paket von 4 GiB nie ganz im Speicher
liegt. Die erste Runde maß **einen** Punkt: 4 GiB hinein, 32 MiB gehalten. Die
Zahl war richtig und sagte trotzdem nicht, was sie sagen sollte — aus einem
einzelnen Punkt folgt nicht, dass nichts mitwächst.

Die zweite Runde maß eine **Reihe**: 256 MiB, 1 GiB, 4 GiB — gehalten
**68,7 / 68,7 / 67,7 MiB**. Sechzehnfache Eingabe, und die Zahl bewegt sich
nicht. **Das Flache ist die Aussage**, nicht die Höhe.

Wo du behauptest, dass etwas **nicht** mitwächst, miss mindestens drei Punkte,
die weit genug auseinanderliegen, dass ein Mitwachsen auffallen müsste. Ein
Punkt beweist eine Höhe. Nur eine Reihe beweist eine Steigung.

### Ein sauber zusammengefuegter Merge kann ein Argument fallen lassen

Eine Aufgabe gab einer Funktion einen vierten Parameter und zog **eine** ihrer
zwei Aufrufstellen nach. Beide stehen in derselben Funktion, zwanzig Zeilen
auseinander. Git hatte keinen Konflikt, JavaScript keinen Fehler: das dritte
Argument rutschte in den vierten Platz, der vierte fehlte.

    setupView(root, { config, onDone: (world) => showDesktop(root, config, world) });
    //                                                       ^^^^^ landet in `anchor`,
    //                                                             `world` ist undefined

Die Folge war **eine weiße Seite, eine Zeile nachdem eine Anlage eingerichtet
und ihr Einmal-Code verbraucht ist** — der Code ist einmalig, der Mensch sitzt
vor einem installierten, unerreichbaren System. Und weil dieser Zweig keine
Auffangklausel hatte, kam nicht einmal die Meldung.

Dazu zwei Dinge, die diesen Fall lehrreich machen:

**Der Koordinator hat die Ursache falsch geraten.** Er sah den roten Test
(„wartet auf die Taskleiste"), kannte eine passende Erklärung aus einem anderen
Bericht (die Kette der Gegenstelle passt keinem festen Anker) und schrieb sie in
den Auftrag — samt der Anweisung, den Fall auf die Attrappe zu legen. Das wäre
ein grüner Test gewesen **mit der weißen Seite dahinter**. Der Umsetzer hat
gemessen statt die Diagnose zu übernehmen, die Ursache gefunden und die
Abweichung vom Auftrag offen genannt.

**Eine plausible Erklärung in der Hand ist gefährlicher als keine.** Wer ein
rotes Ergebnis mit einem Satz erklären kann, hört auf zu suchen — und der Satz
stammte hier aus einem richtigen Bericht über einen anderen Fall.

**Also:** Wächst eine Signatur, such **jede** Aufrufstelle, auch die in
derselben Datei und derselben Funktion — in JavaScript hilft kein Übersetzer.
Und wenn du einem Umsetzer eine Ursache nennst, nenn sie als **Vermutung** und
verlang die Messung.

### Ein Strang auf der älteren Basis bringt einen überholten Weg mit

Ein Strang hat einen Fall gemeldet, der bei ihm rot war, und hat die Ursache
richtig gemessen: die Attrappe liefert seit seiner Aufgabe mehrere Apps aus und
liest die App aus den Parametern, also lehnt **sie** ab, wenn der Test sich
Parameter ausdenkt. Sein Weg dahin: die Kennung der App in den Test schreiben.

Beides war wahr. Nur sass er auf der Basis von vorgestern. Ein anderer Strang
hatte denselben Fall einen Tag später anders geschlossen — die App kommt aus
der **eigenen Liste der Attrappe**, damit im Test keine Kennung steht, die
verrotten kann. Hätte ich seinen Weg genommen, wäre eine Kennung
hereingekommen, die der andere gerade entfernt hatte.

**Die Regel:** Bevor du den Weg eines Strangs uebernimmst, schau, auf welcher
Basis er gemessen hat. Seine **Ursache** gilt dann noch, sein **Weg** vielleicht
nicht mehr. Und wenn zwei Stränge denselben Fund unabhaengig finden, ist das
kein Streit, der entschieden werden muss, sondern eine Bestätigung: die Ursache
ist echt. Nimm den späteren Weg und schreib in das Verzeichnis, dass beide sie
gefunden haben — sonst sucht der dritte sie noch einmal.

### Ein Fähigkeitstest, der das Anlegen prüft, prüft nicht das Benutzen

Die Shell braucht Ed25519 und X25519 im Browser. Wo eine Engine sie nicht hat,
sollen die Fälle übersprungen werden — **gemessen**, nie am Namen der Engine
abgelesen. Genau so steht es da, mit einem guten Kommentar daneben. Der
Wächter legt einen Schlüssel an (`generateKey`), der zweite führt einen
Schlüssel ein (`importKey`), und wenn das geht, antwortet er **ja**.

Firefox legt den Schlüssel an. Firefox führt ihn ein. Und scheitert dann an
`deriveBits` und an `sign` mit *"The operation failed for an
operation-specific reason"*. Der Wächter sagte ja, die Arbeit sagte nein:
**157 rote Fälle, die alle Überspringen hätten sein müssen** — und weil die
Anmeldung daran scheitert, fällt danach alles, auch der Taschenrechner und die
Tastaturbedienung, die von Krypto nichts wollen.

**Die Regel:** Ein Fähigkeitstest muss **die Handlung tun**, auf die es ankommt,
nicht ihre Vorbereitung. Anlegen ist nicht Unterschreiben, Einführen ist nicht
Ableiten. Frag dich bei jedem solchen Wächter: *welchen Schritt mache ich hier,
und welchen Schritt machen die Fälle dahinter?* Sind es zwei verschiedene, hast
du keinen Wächter, sondern eine Vermutung mit `try`/`catch` drumherum.

### Und das Drittel, das nie gelaufen ist, weil in meiner Notiz stand, es laufe nicht

Ich habe jedem Strang mitgegeben: *"Firefox startet auf dieser Maschine nicht
(`spawn UNKNOWN`) — das ist die Umgebung, kein Fund."* Das war einmal wahr und
hatte eine Ursache, die längst behoben war: die Browser lagen unter `AppData`,
wo Firefox an `mozglue` scheitert, und seit `PLAYWRIGHT_BROWSERS_PATH` woanders
hinzeigt, **startet Firefox**.

Sechsundzwanzig Aufgaben lang hat niemand ihn laufen lassen, weil in meinem Satz
stand, es sei zwecklos. Ein Drittel der Prüfmatrix war nie gemessen. Gefunden
habe ich es nur, weil ich einmal **ohne** Einschränkung auf die Engines gelaufen
bin — aus Versehen.

**Die Regel:** Jeder Satz der Form *"X geht hier nicht"*, den du weitergibst,
braucht ein Datum und die **Ursache**. Ohne Ursache kann niemand merken, dass sie
weg ist; mit Ursache merkt es der Nächste beim Lesen. Und läuft etwas einmal
ohne deine üblichen Schalter — ein Lauf ohne Filter, ein Build ohne Cache — dann
**lies das Ergebnis**, auch wenn es nicht das war, was du messen wolltest. Dort
liegt, was deine Schalter sonst zudecken.

### Eine Regel, die zu streng ist, stirbt nur auf einer Engine

Ein Strang sollte die Schutz-Richtlinie für die ausgelieferten Dateien
festlegen. Seine erste Annahme: die Richtlinie der Seite ist dort
**überflüssig** — eine `.js`-Datei ist kein Dokument. Er hat sie trotzdem
gemessen, auf beiden Engines, und das Ergebnis war nicht überflüssig, sondern
**schädlich**: auf der Worker-Datei lebt die Richtlinie, und ohne
`script-src 'self'` stirbt der Worker **auf WebKit** — das heißt, keine
Passwort-Anmeldung in Safari. **Chromium merkt davon nichts.**

Sein Satz dazu: *"Hätte ich nur Chromium gemessen, wäre der falsche Wert
ausgeliefert worden."*

Das ist die Gegenrichtung zu allem, was man über Engines lernt. Dass eine
Fähigkeit **fehlt**, merkt man: etwas geht nicht. Dass eine **Einschränkung zu
eng** ist, merkt man nur dort, wo sie greift — und sie greift nicht überall
gleich.

**Die Regel:** Jede Einschränkung, die du setzt — eine Richtlinie, eine
Obergrenze, ein erlaubter Zeichensatz, ein Zeitfenster — wird auf **jeder**
Engine gemessen, in die du ausliefern willst, und nicht nur auf der, auf der du
entwickelst. Und lass die **Begründung** daneben stehen, auch für das, was du
weggelassen hast: er hat `connect-src` bewusst nicht gesetzt und auf beiden
Engines gemessen, dass es keinen Weg aus dem Worker gibt. Ein weggelassener Wert
ohne Messung daneben ist nicht dasselbe wie ein geprüfter.

### Ein Prüfgriff, der die Form prüft, prüft nicht die Wirkung

Ein Strang hat eine Serverkonfiguration gebaut und einen Fall dazu, der
festhält, dass die Schutz-Kopfzeilen für die ausgelieferten Dateien dastehen.
Der Prüfer hat gemessen, was der Fall wirklich prüft:

```apache
<Directory "/srv/woanders/public/assets">
```

Ein Pfad **ohne jeden Bezug** zum Webroot — **15 passed, EXIT=0**. Der Griff
prüfte, dass der Pfad auf `/public/assets` **endet**. Nicht, ob der Block je in
Kraft ist.

Dasselbe hatte am selben Tag der Fähigkeits-Wächter: er prüfte, dass ein
Schlüssel sich **anlegen** lässt, nicht dass man mit ihm **arbeiten** kann. Und
dasselbe hat `browserFits()` in der Shell: es fragt `generateKey` und lässt
darum einen Browser zur Anmeldemaske, in der die Anmeldung immer scheitert.

**Die Regel:** Schreib bei jedem Prüfgriff zwei Sätze untereinander: *was ich
hier anfasse* und *was der Code später wirklich tut*. Stehen dort zwei
verschiedene Dinge — eine Zeichenkette gegen eine Wirkung, ein Anlegen gegen ein
Benutzen, ein Vorhandensein gegen ein Greifen — dann prüfst du die Form und
nennst es die Sache. Die Gegenprobe ist immer dieselbe: **bau den Fall, der die
Form behaelt und die Wirkung verliert**, und sieh zu, dass er rot wird.

### Die gefährlichste Zusicherung, die nicht fallen kann, steht im Plan

Den ganzen Tag haben Stränge Zusicherungen gefunden, die für ihr eigenes Thema
nicht fallen konnten. Als ich am Abend nachgesehen habe, **woher** sie kamen, war
die Antwort unangenehm: die meisten standen **in meinen Plänen**, wörtlich, als
fertiger Code.

- Ein Fuzz-Fall meines Plans konnte nicht rot werden, weil `Encode` dieselbe
  Prüfung macht wie `Decode`: *"eine Leitung, die jede Nutzlast wegwirft, lässt
  ihn grün"*.
- Neun Längenprüfungen eines anderen Plans ließen sich **alle** löschen, ohne
  dass ein Test fiel — und eine Funktion wurde von der Vertragsgruppe gar nicht
  erreicht. In der Prüfung kamen vier weitere dazu.
- Und der schlimmste: mein Fall gegen ungesiegelte Fremdbytes konnte für **keine**
  seiner zwei Regeln fallen. Man durfte die Entschlüsselungsfehler durchlassen —
  **ungesiegelte Bytes als Frames** — und alles blieb grün.

**Warum gerade der Plan:** eine Zusicherung aus dem Plan wird **nicht nachgeprüft**.
Sie kommt von oben, sie sieht fertig aus, und der Implementierer schreibt sie ab,
weil der Auftrag es verlangt. Dieselbe Mechanik wie bei einer Ursache im Auftrag,
nur schlimmer: eine falsche Ursache fällt spätestens beim Messen auf, eine
Zusicherung, die nicht fallen kann, fällt **nie** auf. Sie ist grün.

**Die Regel für den, der Pläne schreibt:** Zu jeder Zusicherung, die du in einen
Plan schreibst, gehört der Satz, **welche Mutation sie rot machen muss**. Kannst
du die Mutation nicht benennen, hast du keine Zusicherung aufgeschrieben, sondern
eine Hoffnung. Und schreib dazu, dass der Implementierer sie **prüfen** soll —
nicht übernehmen.

Dazu ein Sonderfall, der mir dreimal begegnet ist: **ein Fuzzer, der nur gültige
Eingaben baut, erreicht den ungültigen Zweig nie.** Einer siegelte jede Eingabe,
bevor er sie hineingab — damit war der ganze Entschlüsselungszweig toter Code
für ihn, und die 40 000 Läufe in seinem Protokoll sagten darüber nichts. Zähl bei
jedem Fuzzer nach, **welche Zweige seine Eingaben überhaupt erreichen können**.

### Null Funde sind erst eine Aussage, wenn das Werkzeug einen finden kann

`go test -race` meldete über drei Pläne hinweg **0 DATA RACE**. Das klingt nach
einem Ergebnis und war noch keins: ein Renn-Sucher findet nur, was die Tests
tatsächlich **gleichzeitig** anfassen. Läuft in einem Paket nie etwas parallel,
meldet er null — so wie ein Metalldetektor an einem leeren Strand.

Zwei Stränge haben daraus unabhängig dieselbe Lehre gezogen und sie **gemessen**:

- Der eine hat sein Schreibschloss entfernt und bekam `WARNING: DATA RACE` in
  `noisep.(*Transport).Seal()`, zwei Goroutinen im selben `Seal`. Sein Satz dazu
  ist der Kern: *ohne den Fall, der absichtlich zwei Schreiber ansetzt, hätte
  `-race` an dieser Stelle **nichts zu sehen**.* Die Ersatzmessung, die er gebaut
  hatte, als `-race` noch unmöglich schien, ist das, was dem Original die Augen
  gibt.
- Der andere hat eine Wegwerf-Datei mit einem echten Rennen gebaut:
  `DATA RACE count: 2`, Test rot, Ausstiegscode 1. Erst danach hat er sein
  „0 DATA RACE, 202 von 202" aufgeschrieben.

**Die Regel:** Bevor du eine Null berichtest, zeig, dass dasselbe Werkzeug an
derselben Stelle eine Eins hätte melden können. Das gilt für jeden Sucher, der
nach etwas Abwesendem fragt — Renn-Sucher, Fuzzer, Prozentzahlen über
abgedeckte Zeilen, ein `grep`, das nichts findet. **Und es gilt doppelt, wenn die
Null das Ergebnis ist, das du dir gewünscht hast.**

### Sechs Messungen gleichzeitig messen die Maschine, nicht den Code

Ich hatte jedem Strang eingeschärft, sich einen **eigenen Port** zu nehmen — gut,
denn sonst misst der zweite Lauf stillschweigend den Arbeitsbaum des ersten. Nur
verhindert ein eigener Port nicht, dass der Maschine die Sockets ausgehen.

Als sechs Prüfsätze gleichzeitig liefen, hat ein Prüfer gemessen:
**24 266 Sockets in TIME_WAIT auf 16 384 mögliche Ports.** Zwei seiner
vollständigen Läufe waren rot — 9 und 15 Fälle, an **verschiedenen** Stellen —
und **keine einzige gefallene Zusicherung** darunter: nur
„Could not connect to server" und „Importing a module script failed".

Das Gemeinste daran hat er gleich mitgemessen: **ein Serverausfall macht aus
einem Überspringen einen laufenden Fall.** Damit wandert die Zahl der
übersprungenen Fälle, und zwei Läufe sind nicht mehr vergleichbar — ohne dass
irgendwo das Wort „Fehler" steht. Wer dann Zahlen nebeneinanderlegt, vergleicht
die Auslastung der Maschine.

**Die Regel:** Vor einem vollen Lauf fragst du, ob gerade einer läuft, und
verteilst nicht mehr als zwei gleichzeitig. Und was für Zahlen gilt, gilt erst
recht für ihre Erklärung: „flatterhaft" ist keine Ursache, solange niemand die
Maschine gemessen hat.

Dazu der kleinere Bruder, aus demselben Tag: mein eigener Nachmesslauf hat
**keinen einzigen Test gefahren** — in einem frischen Arbeitsbaum fehlte die
nicht eingecheckte Zeile mit dem Pfad zu php, und die Konfiguration brach ab.
Ausstiegscode 1, also formal richtig. **Lies trotzdem immer, wie viele Fälle
gelaufen sind.** Null bestandene Fälle sind kein Ergebnis, auch nicht ein
schlechtes.

### Eine Pipe verschluckt das Urteil

Der Koordinator hat einen zusammengeführten Stand geprüft, das Ergebnis
gelesen und **„394 bestanden, 0 durchgefallen"** gemeldet. Zwölf Tests waren
rot. Zwei Prüfer fanden sie unabhängig voneinander, einen Tag später.

Der Befehl war `npx playwright test … 2>&1 | tail -5`. Daran ist **beides**
falsch:

- **Der Ausstiegscode gehört dem letzten Glied der Pipe**, also `tail`.
  Nachgemessen, derselbe Lauf: playwright antwortet **1**, die Pipe **0**.
- `tail -5` schneidet den Block mit den Fehlern ab. Übrig bleiben die zwei
  Zeilen, die immer gut aussehen: „N übersprungen, M bestanden".

Zusammen ergibt das ein Grün, das dreimal hintereinander gehalten hat.

**Also: lass das Werkzeug selbst antworten.** Kein `| tail` um einen Lauf,
dessen Ausgang du wissen willst; Ausgabe in eine Datei und danach den
**Ausstiegscode** lesen, oder den Lauf ohne Pipe fahren und erst danach kürzen.

Und die allgemeine Form, die teurer ist als der Handgriff: **ein Grün, das du
nicht vom Werkzeug selbst gehört hast, ist kein Grün.** Dieselbe Regel gilt für
die Zahl der **übersprungenen** Tests. Sie stieg in diesem Lauf von 45 auf 122,
und niemand fragte warum — ein Test, der sich still überspringt, sieht in der
Zusammenfassung aus wie einer, der nichts zu meckern hat.

### Dein Prüfgriff darf nicht an der Sprache des Systems hängen

Zwei Tage lang hat der Koordinator vor jedem Testserver geprüft, ob der Port
frei ist — mit

    netstat -ano | grep -c ":$p .*LISTENING"

Auf diesem Rechner antwortet `netstat` deutsch: dort steht **`ABHÖREN`**. Die
Zählung war also **immer 0**, für jeden Port, auch für belegte. Aufgefallen ist
es erst, als ein Server auf einem „freien" Port startete und der Browser die
Seite einer **fremden Sitzung** zeigte: Windows ließ beide lauschen, und die
andere gewann.

Zwei Dinge daran:

- **Ein Prüfgriff, der eine Sprache voraussetzt, prüft die Sprache.** Nimm eine
  Schnittstelle, die Daten liefert statt Text — hier
  `Get-NetTCPConnection -State Listen`, deren Zustand ein Aufzählungswert ist
  und nicht übersetzt wird.
- **Ein Prüfgriff, der nie „belegt" sagt, ist nicht beruhigend, sondern
  verdächtig.** Wenn eine Prüfung über zwei Tage ausnahmslos dasselbe antwortet,
  ist das ein Grund nachzusehen — nicht ein Grund, sich zu freuen.

Und der Nachsatz, der wehtut: derselbe Koordinator hat jeder Sitzung
eingeschärft, Port 8101 gehöre einer fremden Sitzung — und ist dann selbst
daraufgelaufen, weil sein eigenes Messgerät ihn freigemeldet hat.

### Schneid die Funktion aus der ausgelieferten Datei und lass sie laufen

Ein Installationsskript in einer fremden Sprache, 1166 Zeilen Shell, kein Server
zum Ausprobieren: der nächstliegende Weg ist Lesen, und Lesen ist kein Prüfen.
Die Sitzung hat etwas Besseres gebaut.

Sie hat die einzelnen Funktionen **aus der eingecheckten Datei
herausgeschnitten** — nicht aus einer Kopie, nicht nachgeschrieben — und sie in
Subshells gegen die Umsetzung der **anderen Sprache** gefahren:

- die Adressprüfung des Skripts gegen alle 32 Host-Vektoren der Testdaten,
  Antwort für Antwort **genau wie** die Go-Seite;
- den Fingerabdruck gegen zwei andere Umsetzungen desselben Werts;
- die erzeugte Konfiguration **byteweise** gegen die Ausgabe von Gos eigenem
  Schreiber.

Dreiundzwanzig Prüfungen, null Abweichungen. Und weil die Funktionen aus der
ausgelieferten Datei kommen, prüft das **die Datei**, die wirklich ausgeliefert
wird — nicht eine Nachbildung, die daneben verrottet.

**Das ist das Muster, wo ein Ding keine Umgebung zum Laufen hat:** nicht die
Logik nachbauen, sondern das Stück herausschneiden und es gegen die Seite
antworten lassen, die es nachbilden soll. Was dabei nicht geht, bleibt ehrlich
ungeprüft — in diesem Fall alles, was Apache, das Netz oder einen Paketmanager
gebraucht hätte, und das stand so im Bericht.

### Ein Zusammenbruch verdeckt den einen echten Roten

Ein Lauf einer Aufgabe brach in einer Maschine mit **127 roten Fällen** ein. Die
Erklärung war richtig und binnen Minuten belegt: die Maschine startete auf diesem
Rechner überhaupt nicht, dieselben 127 fielen auch in einem Baum ohne eine Zeile
dieser Aufgabe. Umgebungsbedingt, kein Fund — und so stand es im Bericht.

**Einer der 127 war kein Rauschen.** Der neue Test dieser Aufgabe fehlte die
Weiche für Maschinen ohne Ed25519, die fünfunddreißig Geschwistertests seit jeher
tragen. Er fällt dort **deterministisch**, und schlimmer: ohne Ed25519 lehnt die
geprüfte Funktion auch den **guten** Fall ab, der Test sichert dann gar nichts
mehr zu. Gefunden hat ihn erst der Lauf nach dem Zusammenführen, in einer
Maschine, die wieder startete.

Eine Erklärung, die 127 Rote deckt, deckt 126 und **versteckt den einen**. Wer
einen Massenausfall erklärt hat, ist damit fertig, ihn zu *verstehen* — nicht
damit, ihn zu *lesen*. Einmal Stück für Stück durchsehen, ob eine Zeile aus der
Reihe fällt: eine andere Fehlermeldung, eine andere Datei, ein anderer Zeitpunkt.

Dieselbe Regel gilt für den unerklärten Roten, den man **nicht** wegerklären
kann. Ein einzelner roter `-race`-Lauf dieser Sitzung blieb ungeklärt; fünfzehn
Wiederholungen waren grün. Er ist als **ungeklärt** vermerkt, nicht als erledigt.
Beide Fälle sind dieselbe Versuchung, einmal von oben und einmal von unten.

### Frag das Remote, nicht den Koordinator

Der Koordinator hatte eine Regel richtig — „eine Nachricht, die auf dem Remote
liegt, wird nicht mehr berichtigt" — und sie auf sechs Commits angewandt, von
denen **keiner** dort lag. Er hatte den Zweig einmal hochgeschoben und angenommen,
das Remote sei mit den späteren Commits mitgewandert.

Der Prüfer hat nicht geglaubt, sondern gefragt:

    git ls-remote origin refs/heads/api   →   af2c186

Sechs Commits darüber, keiner oben. Die Auskunft, auf der eine Entscheidung
ruhte, war schlicht falsch.

**Eine Anweisung des Koordinators steht unter derselben Regel wie alles andere**
(Regel 9) — und der billigste Weg, das zu prüfen, ist oft **ein Befehl**. Wo eine
Anweisung eine Tatsache über den Zustand der Welt behauptet („liegt auf origin",
„hat keinen Aufrufer", „ist eingefroren"), frag den Zustand selbst. Es kostet eine
Zeile und hat in dieser Sitzung mehrfach eine Entscheidung gedreht.

Und die Kehrseite, die derselbe Prüfer mitgeliefert hat: **daraus folgt nicht
automatisch die andere Handlung.** Die Nachricht ganz unten blieb trotzdem, wie
sie war — nicht, weil es nicht ginge, sondern weil fünf Commits darüber neu
gebaut werden müssten. Ein widerlegter Grund macht die Entscheidung nicht
verkehrt; er verlangt einen **richtigen** Grund.

### Ein Getter ist Code — lesen heißt ausführen

Der teuerste Irrtum dieser Sitzung sah aus wie ein Sicherheitsgewinn. Über zwei
Zeilen, die die Länge eines fremden Gegenstands prüfen, stand:

> „Sie können nur **abweisen**. Was an ihnen vorbeikommt, wird unten ein zweites
> Mal entschieden."

Der Koordinator hat darauf eine Entscheidung gegründet — die Zeilen bleiben,
weil ihr Wegräumen *lockerer* wäre. Ein Prüfer hat es gemessen, mit einem
Gegenstand, dessen Länge beim **ersten** Lesen eine Marke umlegt, sodass die
Sicht darunter andere Bytes sieht:

| Stand | Antwort auf die Fälschung |
| --- | --- |
| mit den zwei Zeilen | **ja** |
| ohne sie | **nein** |

**Die Prüfung war das Werkzeug der Fälschung.** Ein Getter ist Code; ihn zu lesen
heißt, ihn laufen zu lassen, und was er dabei tut, ändert alles Folgende. Eine
Prüfung, die den Gegenstand **anfasst**, weist nicht nur ab — sie entscheidet mit.

Und die zweite Hälfte, ohne die man nur die Seite wechselt: dieselbe Messreihe
kannte einen **zweiten** feindlichen Gegenstand, dessen Länge nicht umlegt,
sondern **wirft** — und der bekam ohne die zwei Zeilen ein Ja. **Keine der
beiden Fassungen war gegen beide sicher.**

Die Frage war also nie „Zeile rein oder raus". Sie lautet: **die Entscheidung auf
etwas heben, das der Gegenstand nicht mehr beeinflussen kann** — einmal eine
Sicht bilden und ab da nur noch die Sicht befragen.

Zwei Regeln fallen daraus:

- **Fass den fremden Gegenstand genau einmal an, so früh wie möglich, und
  entscheide danach nur noch auf dem, was du dabei gewonnen hast.**
- **Wenn zwei Angriffe in entgegengesetzte Richtungen zeigen, ist keine der
  beiden Seiten die Antwort.** Dass eine Änderung den einen Fall repariert, sagt
  nichts, solange der andere ungemessen ist — such den Gegenfall zum Gegenfall.

## 6. Ein Fuzz-Ziel, das den Annahme-Anteil kumuliert, taugt nicht

Der Fuzzer optimiert auf Code-Abdeckung. Weil die **Ablehnungswege** die
Abdeckung tragen, **drückt er den Annahme-Anteil aktiv herunter** — gemessen von
45 % auf unter 5 % in einer Minute. Eine Prüfung „mindestens ein Drittel wird
angenommen" hört damit von selbst auf zu greifen.

**Nimm eine Prüfung bei jeder Eingabe, die der Fuzzer nicht umsteuern kann**,
und miss den Anteil über die **festen Saaten**.

Und prüf die Prüfung: in einem Fall erhöhte der Kanarienvogel **beide** Zähler,
sodass die Anteilsschranke rechnerisch nie anschlagen konnte.

## 5a. Prüf die Eigenschaft am Gegenstand, nicht am Weg dorthin

Ein Testlauf legte ein verschlüsseltes Objekt an und prüfte, dass jeder Schritt
gelang. Er prüfte **nie**, welche Eigenschaften das Objekt am Ende trug. Setzte
jemand ein einziges Attribut dazu, blieb der Lauf grün — und das Objekt ließ sich
ohne jede Prüfung öffnen.

Das wurde repariert, richtig: der Test liest die Attribute jetzt **am Objekt
selbst** zurück. Dann hat ein Prüfer dieselbe Frage an alles andere gestellt, und
sieben Antworten waren dieselbe. Jede dieser Änderungen ließ den Lauf grün:

- die eine Zeile löschen, die den Systemzustand an das Siegel bindet — danach
  öffnet es in jedem Zustand
- das Löschen des Geheimnisses durch nichts ersetzen — danach liegt es im Klartext
- den Hash von SHA-384 auf SHA-1 stellen — dasselbe Objekt, schwächere Kette
- den Schlüssel von 384 auf 256 Bit — dasselbe Objekt, schwächerer Schutz
- die gesicherte Firmware gegen die ungesicherte tauschen

**Alle sieben haben dieselbe Form:** geprüft wurde, was das Skript **tut**, nicht
was am Ende **dasteht**. Ein Schritt, der gelingt, sagt nichts darüber, ob er das
Richtige bewirkt hat.

Dieselbe Sorte in einem anderen Strang: eine Datenbankzeile wurde geschrieben und
der Erfolg geprüft — dass die Spalten die richtigen Werte trugen, prüfte nichts.
Vertauschte man zwei davon, blieb alles grün.

**Deshalb: nach jedem Schritt, der etwas Bleibendes anlegt, frag nicht „ging es
gut?", sondern „was steht jetzt da?"** — und lies es von dort zurück, wo es liegt,
nicht aus der Variablen, aus der du es geschrieben hast. Wo das nicht geht, sag im
Bericht, dass die Eigenschaft ungeprüft ist; sie fällt sonst beim ersten Umbau weg,
ohne dass etwas rot wird.

### Und der Wächter selbst ist ein Gegenstand

Der schärfste Fall kam später, weil er die Regel auf sich selbst anwendet. Ein
Strang baute einen **Wächter** — einen Test, der über den Syntaxbaum prüft, dass
eine bestimmte Abfrage *innerhalb der Transaktion* steht. Er war gemessen: drei
Umbauten, die die Absicherung wegnehmen, machten ihn rot. Er sah gut aus.

Er prüfte `pos > lit.Pos() && pos < lit.End()` — also **wo der Aufruf im
Quelltext steht**. Zwei Umstellungen ließen ihn grün:

- die Abfrage auf dem Verbindungspool statt auf der Transaktion laufen lassen —
  eine einzige Zeile. Sie nimmt dann eine zweite Verbindung und liest den zuletzt
  festgeschriebenen Stand, sieht also nicht einmal die eigene Transaktion. Genau
  die Eigenschaft, die der Wächter im Namen trägt, war offen.
- dieselben Zeilen in eine Closure stecken und per `defer` aufrufen — sie
  **stehen** davor und **laufen** danach.

Der Wächter maß die Textstelle und behauptete, den Ablauf zu messen. Ein
Prüfwerkzeug ist selbst ein Gegenstand: **frag bei jedem Wächter, welche
Eigenschaft er wirklich liest, und bau die zwei Umstellungen, die diese
Eigenschaft unberührt lassen und die gemeinte zerstören.** Bleibt er dabei grün,
prüft er den Weg, nicht den Gegenstand.

Die Reparatur hat dieselbe Form wie überall in dieser Regel: nicht "steht es an
der richtigen Stelle", sondern "ist es **dieses** Ding" — der Aufruf muss auf
demselben Transaktionsobjekt erfolgen (über die Identität der Variablen, nicht
über ihren Namen) und eine direkte Anweisung des Rumpfes sein.

## 6a. Eine zweite Verteidigungslinie entwaffnet die erste

Eine Funktion prüfte vor dem Schreiben, ob der Name schon einem anderen gehört.
Diese Zeile war gemessen: nahm man sie heraus, wurden ein Test **und** eine
Gruppe des Vertrags rot. Dann kam — zu Recht — eine zweite Absicherung dazu, die
denselben Fall am Datenbankindex abfängt.

Danach ließ sich die erste Prüfung **samt Abfrage und Konstante** löschen, und
nichts wurde rot. Sie ist nicht falsch geworden; sie ist nur nicht mehr
unterscheidbar. Beim nächsten Aufräumen fliegt sie raus, weil kein Test sie
hält — und der Kommentar, der sich um sie dreht, steht dann über nichts.

**Deshalb: wenn du eine zweite Linie einziehst, sieh nach, was die erste noch
misst.** Die Mutation, die sie früher gekippt hat, überlebt jetzt. Entweder die
erste Linie bekommt eine Zusicherung über das, was **nur sie** leistet — hier:
in derselben Transaktion zu sitzen, nicht dasselbe Ergebnis zu liefern —, oder
sie geht mit der Begründung weg, dass die zweite sie ersetzt. Beides ist in
Ordnung. Sie unbewacht stehen zu lassen ist es nicht.

Das ist die Spiegelseite von Regel 7: dort wird ein Wächter abgeschaltet, weil
er zu oft irrt; hier verschwindet eine Prüfung, weil niemand mehr merkt, dass es
sie gibt.

## 7. Ein Wächter, der auf gutem Code rot wird, wird abgeschaltet

Ein Quelltext-Wächter über SQL brauchte fünf Runden. Zwei Erkenntnisse:

- **Erkennung über Namen ist umgehbar.** Eine lokale Variable, die eine
  Konstante überdeckt; ein Methodenwert (`e := s.db.ExecContext`); `reflect`.
  Lös auf (`go/types`), statt Namen zu vergleichen — und vergleich Typen mit
  `types.Unalias`, sonst rutscht ein Deckname durch.
- **Zu streng ist genauso schlimm wie zu lasch.** Derselbe Wächter wurde rot auf
  vorbereiteten Anweisungen und auf `vals := []any{…}` — beides normaler,
  richtiger Code. Das ist der Zustand, in dem Leute solche Prüfungen entfernen.

**Miss jede neue Strenge gegen die Formen, die die nächsten Aufgaben brauchen.**
Sechsmal gemessen, dann war klar, dass der Wächter überlebt.

**Und das ist die Reihenfolge, in der ein Wächter gebaut wird: erst das falsche
Rot messen, dann die Regel schreiben.** Ein Umsetzer hat zehn Nachbauten
angelegt, bevor er eine Zeile Wächter schrieb — neun zulässige Formen und drei
kaputte. **Vier der neun zulässigen waren in seiner ersten Fassung rot** und
haben die Regel je einmal geändert: eine Helfermethode, eine Closure vor dem
Schreiben, eine gewöhnliche Zeichenkette, die mit „select" anfing, und ein
`WITH … UPDATE`-CTE. Ohne diese Messung wäre der Wächter mit vier eingebauten
Fehlalarmen in den Baum gegangen — und beim ersten davon abgeschaltet worden.

**Und trotzdem reicht das nicht: der Prüfer muss eigene Formen bauen.** Derselbe
Wächter, nach zehn eigenen Nachbauten für dicht erklärt, wurde vom Prüfer mit
**neun anderen** noch einmal gefragt — **fünf davon rot**. Die schlimmste: ein
gewöhnlicher Satz in einer Fehlermeldung, in einer Datei, die mit der geprüften
Funktion nichts zu tun hat, schaltete den Wächter rot und behauptete dabei, diese
Zeile sei die erste Anweisung an die Datenbank. Sie ist keine Anweisung und geht
an keine Datenbank.

Wer eine Regel schreibt, kennt seine eigenen blinden Flecken nicht — deshalb ist
„Ich habe zehn Formen gemessen" eine Aussage über zehn Formen und keine über die
Regel. **Frag bei jedem Wächter: welche Gestalt gewöhnlichen Codes habe ich nicht
gebaut, weil ich sie mir nicht vorstellen konnte?** Und lass die Frage von
jemandem noch einmal stellen, der die Regel nicht geschrieben hat.

Und wenn eine Regel eine Eigenschaft festnageln würde, die **niemand braucht**:
lass sie weg. Dieselbe Regel wäre auf einer Nachbarfunktion grün gewesen; sie
dort auch anzuwenden hätte nur den Tag vorbereitet, an dem eine zulässige
Änderung an ihr scheitert und jemand den **ganzen** Wächter herausnimmt.

Und: **jede Runde neue Mechanik bringt neue Löcher.** Ab einem Punkt heißt die
Anweisung „schließen und Zusagen engen, **kein neuer Erkennungsmechanismus**" —
Verbote sind erlaubt, Erkennung nicht.

## 8. Ein Fehler im Auftrag ist ein Fund, kein Hindernis

**Acht** Auftragsfehler haben Umsetzer in NGC gefunden. Jedes Mal war die
Meldung wertvoller als der Code. Darunter:

- ein Testhelfer rief eine `async`-Funktion **ohne `await`** — der `catch` war
  unerreichbar, die Zusicherung hätte **dauerhaft rot** sein müssen;
- ein Prüfvektor gab dem „Passwort-Admin" einen Passkey — der Haupttest hätte
  nie grün werden können;
- eine Größengrenze war um 64 KB zu klein gerechnet.

**Folge dem eingefrorenen Vertrag, nicht dem Auftrag** — das wurde dreimal so
entschieden. Und melde es, statt es still zu umgehen.

## 9. Die Anweisung des Koordinators steht unter derselben Regel

**Dreimal** hat ein Umsetzer eine Vorgabe von mir berichtigt, weil ich eine
Nebenwirkung nicht durchgerechnet hatte. Jedes Mal hatte er recht.

Einmal entstand eine **Kette aus drei ungeprüften Behauptungen**: ein Prüfer
vermutete eine Shell-Einstellung als Lösung „der ganzen Klasse", der Koordinator
gab es weiter, der Umsetzer schrieb es ungeprüft in den Code. Der nächste
Prüfer maß nach: sie schließt einen Teil der Fälle — **genau den nicht**, um den
es ging.

**Kommt dir eine Anweisung nicht stimmig vor: frag, bevor du sie ausführst.**

### Der teuerste Satz ist der, der die Suche beendet

Der Koordinator hat einen Weg bewusst offengelassen und den Grund dazugegeben —
das ist richtig, denn eine benannte Lücke ist besser als eine verschwiegene.
Sein Grund lautete: *ein Beweis an dieser Stelle würde ohnehin von einem
Aufräumer geschlagen, der danach angemeldet wird. Kein Wettlauf, den jemand
gewinnt; der Nächste braucht es nicht noch einmal zu versuchen.*

Der Prüfer hat es gemessen: **Aufräumer laufen nach dem Rumpf.** Ein Beweis
**im** Rumpf ist für sie unerreichbar. Der Beweis fängt den Weg, kostet eine
Dreiviertelsekunde, und ist damit genau das, wovon der Satz abgeraten hat.

Die erste Hälfte des Grundes stimmte sogar — ein dritter Beweis *als Aufräumer*
wird geschlagen. Die zweite nicht. Und weil der Satz im Code stand, wäre er die
nächsten Jahre die Antwort auf jede Nachfrage gewesen.

**Eine benannte Lücke trägt ihren Grund mit, und der Grund wird geprüft wie eine
Zusicherung.** Wer schreibt „das geht nicht, deshalb lassen wir es", hat eine
Behauptung über die Welt aufgestellt — dieselbe Sorte, die Regel 1 misst. Der
Unterschied zu jeder anderen falschen Zusage ist nur, dass diese hier **niemanden
rot werden lässt**: sie beendet die Suche, und danach sieht niemand mehr hin.

Zwei Dinge folgen:

- **Miss den Grund, bevor du ihn hinschreibst** — besonders, wenn er „geht
  nicht" oder „bringt nichts" sagt. Ein Satz, der zum Aufhören rät, verdient mehr
  Prüfung als einer, der zur Arbeit rät.
- **Und schreib ihn so, dass er widerlegbar ist.** „Ein Aufräumer schlägt jeden
  Beweis" lässt sich prüfen und war falsch. „Das lohnt sich nicht" lässt sich
  nicht prüfen und wäre nie aufgefallen.

## 10. Im Zweifel die sichere Seite

Steht Sicherheit gegen Bequemlichkeit, gegen Geschwindigkeit oder gegen Aufwand:
**sichere Seite, Preis in einem Satz nennen, weitermachen.** Nur fragen, wenn
die sichere Seite etwas kostet, das der Auftraggeber nicht absehen kann.

Ein Sicherheitsmerkmal wird **nie** abgeschwächt, damit etwas durchläuft. Geht
etwas mit den vorhandenen Werkzeugen nicht: **stoppen und fragen.**

Aber: **rechne den Handel durch, bevor du ihn machst.** Ein „Schutz", der zwei
Drittel eines Zeitunterschieds wegnimmt und dafür eine **unbegrenzte,
unauthentifizierte Schreiberlaubnis** auf eine Tabelle ohne Aufräumen eintauscht,
ist keiner.

## 11. Zwei Commits: erst der rote Test, dann der Code

Sonst ist „rot vor grün" nicht belegbar. Ein Prüfer konnte es nicht bestätigen,
weil beides in einem Commit lag.

**Ausnahme, und sie ist keine Aufweichung:** Wenn du einen Fehler in **bereits
angenommenem** Code behebst, gibt es keinen sinnvollen roten Commit — der rote
Zustand ist der Stand, der schon im Zweig liegt. Dann gilt: **erst messen, dann
beheben**, und die Rotmessung wörtlich in Bericht und Commit-Nachricht. Ein
Umsetzer hat das gegen meine Anweisung so gemacht und Bescheid gesagt. Er hatte
recht. Was zählt, ist der Beleg, dass es vorher fiel — nicht die Zahl der
Commits.

## 12. Der Prüfer greift an, statt zu lesen

Was in NGC funktioniert hat:

- **Am echten Paket angreifen, nicht an Testdaten.** Zwei Löcher kamen genau
  daran vorbei.
- **Eigene Angriffe erfinden.** Ein Prüfer hat neun Wege zu einer Methode
  durchgespielt, ein anderer acht erfundene Optionen — beide fanden, was der
  Umsetzer nicht gesehen hatte.
- **Die Zusage prüfen, nicht nur den Fall.** „Jede Funktion mit dem Merker wird
  gemessen" war noch falsch, nachdem der konkrete Weg zu war.
- **Zählen statt lesen.** „Keine Datei bindet X ein" wurde ausgezählt: neun
  geprüft, null Treffer, eine absichtliche Ausnahme in den Testdaten.
- **Mutieren statt glauben.** Der wirksamste Griff überhaupt: bau die Behebung
  gedanklich zurück und sieh nach, ob der neue Test dann wirklich fällt. Ein
  Prüfer hat 28 Mutationen gefahren, 24 wurden gefangen — die vier
  Überlebenden waren vier echte Lücken, von denen keine im Bericht stand.
- **Gegen alten Code messen, ohne den Baum anzufassen: `go test -overlay`.**
  Wo mehrere Sitzungen denselben Arbeitsbaum teilen, sind `git stash`,
  `git checkout` und Hin-und-Her-Editieren verboten — sie reißen anderen den
  Boden weg. Mit `-overlay` misst man den alten Stand, ohne eine Datei zu
  berühren. Ein Prüfer hat damit 200 von 200 Läufen gegen den Stand vor dem Fix
  belegt, während vier andere Stränge weiterarbeiteten.

### Ein aufzählbarer Zeitpunkt schlägt einen geratenen

Ein Angriff hing an einem Zeitpunkt: die Bytes werden getauscht, **während** die
Engine arbeitet. Zwei Umsetzer haben dafür Werkzeuge gebaut und dabei gemeldet,
welche **nicht** gehen — verkettete Microtasks landen alle in einem Durchlauf,
verkettetes `setTimeout(0)` überschießt die Millisekundenklemme. Das war ehrlich
und half. Sie hängten den Tausch dann an die Zusage des Engine-Aufrufs selbst und
maßen **dreizehn** Momente.

Der Prüfer hat etwas anderes gebaut: **einen gemeinsamen Zähler über alle
Engine-Aufrufe**, und getauscht wird nach dem **k-ten** aufgelösten Versprechen.
Damit ist der Zeitpunkt kein Kunstgriff mehr, sondern eine **Zahl** — k läuft von
eins bis zur Zahl der Aufrufe des ehrlichen Laufs, und die Stichprobe ist
vollständig für diese Stelle statt „was mir eingefallen ist".

Ergebnis: **zweiundzwanzig** Momente statt dreizehn, davon zehn gefälscht auf dem
alten Stand und null auf dem neuen, in zwei Umgebungen zeichengleich.

**Wo dein Angriff einen Zeitpunkt braucht, mach den Zeitpunkt zählbar.** Ein
Werkzeug, das „irgendwann dazwischen" trifft, misst, was dir eingefallen ist. Ein
Werkzeug, das „nach dem k-ten Schritt" trifft, misst die Stelle — und sagt dir
nebenbei, wie viele Schritte es überhaupt gibt.

Und was daraus **nicht** folgt: eine Stichprobe über alle Momente **eines**
Laufs ist noch kein Beweis. Der Prüfer hat es selbst dazugeschrieben.

## 13. Was ehrlich offen bleibt, bleibt ehrlich offen

### Zäun die Lücke ein, die du nicht schließt

Ein Umsetzer hatte vier Angriffe gebaut, die eine Lücke belegen, die er nicht
schließen durfte — die Abhilfe lag hinter einem eingefrorenen Vertrag. Er hat
die Lücke im Kommentar benannt und die Angriffe in seinem Kratzordner gelassen,
mit der Begründung: ein Test, der sie festhielte, schriebe das kaputte Verhalten
fest.

Der Einwand klingt richtig und ist es nicht. **Ein Test über eine bekannte Lücke
ist keine Zustimmung zu ihr, sondern ihre Quittung.** Wird sie eines Tages
geschlossen, geht er **rot** — und jemand muss hinsehen und den Kommentar
nachziehen, der bis dahin ehrlich war und in dem Moment still falsch wird. Ohne
den Zaun merkt es niemand.

Ein anderer Strang war nach zehn Runden über dieselbe Frage schon dort: zwei
Lücken und ein lebender Fehlalarm stehen dort als eingecheckte Pakete. Die Zeile
sagt, was der Code **tut**; der Kommentar daneben, ob das **richtig** ist. Und
der Satz, der es zusammenfasst, stammt von dort: **ein Kommentar verrottet, ein
Paket nicht.**

**Der Zaun muss empfindlich sein, sonst ist er Dekoration.** Fahr jeden Test
gegen einen Mutanten, der die **Reparatur** nachbildet, und verlang, dass er an
**seiner eigenen** Zusicherung mit **seiner eigenen** Meldung rot wird. Wird er
aus einem anderen Grund rot, zählt es nicht — ein Umsetzer hat genau das bemerkt
(sein Mutant sperrte schon einen früheren Schritt, der Test fiel an der falschen
Stelle) und ihn neu gebaut, statt das Rot mitzunehmen.

Und benenne die Tests so, dass niemand sie für eine Zusage hält:
`TestKnownGap_TruncatingTheChainRollsItBack`, nicht `TestGapIsCaught`. In den
Rumpf gehören drei Sätze: **das ist eine Lücke, kein Sollverhalten**; woran sie
hängt; und dass Rot heißt, den Kommentar zu berichtigen und den Test in Rente zu
schicken.

„Ungeklärt" ist erlaubt — **unbegründet weglassen** nicht.

Ein Umsetzer meldete drei Minuten ohne Ergebnis auf einer Engine als ungeklärt.
Eingegrenzt stellte sich heraus: eine Schleife brauchte dort **5,4 Minuten** für
die größte zulässige Liste. Auf einem iPhone wäre die Seite unbenutzbar gewesen.
Nach dem Umbau: 20 Sekunden im Extremfall, **769 Millisekunden** im Normalfall.

Und die 769 Millisekunden sind selbst ein Fall für Regel 1. Ich hatte sie aus
0,97 ms je Schlüssel **hochgerechnet** und „unter einer Zehntelsekunde" gesagt —
gemessen war es das **Achtfache**, weil die Bündelung je **Nutzer** läuft und
nicht je Schlüssel. Die Regel lautet: rund 15 ms mal Zahl der Nutzer.
Hochrechnen ist kein Messen, auch dann nicht, wenn der Ausgangswert gemessen war.

Wer eine Zahl nennt, sagt dazu, **wie** gemessen wurde. Wer eine nicht hat, sagt
das — und der Koordinator entscheidet, ob sie gebraucht wird.

### Eine Gegenprobe, die einen Arm kaputt macht, sichert einen Arm

Eine Prüfung hatte zwei Hälften: sie zählte Treffer in **Anfragen** und Treffer
in **Antworten**. Der Umsetzer hat die Gegenprobe gebaut, die diese Regel
verlangt — Filter trifft nichts, Filter trifft alles, Filter vertauscht — und
alle drei wurden rot. Er schrieb in die Commit-Nachricht, alle drei Wege seien
gebaut und gingen rot.

Der Prüfer hat die Arme **einzeln** kaputt gemacht:

| kaputt | Lauf |
| --- | --- |
| Anfrage-Arm trifft nichts | rot |
| **Antwort-Arm trifft nichts** | **grün, alles ok** |

Der Arm, der die Eigenschaft trug, war durch nichts abgesichert. Eine
Fremdänderung, die nur die eine Kopfzeile umbenennt, hätte den Lauf **grün**
gelassen — also genau das Gegenteil dessen, was im Bericht als Bedenken stand.

**Hat deine Prüfung mehrere Arme, mach jeden einzeln kaputt und verlange für
jeden Rot.** Eine Gegenprobe, die den Prüfer als Ganzes abschaltet, sagt nur,
dass er überhaupt etwas tut.

Dieselbe Runde hat die Schwesterform geliefert: Gesucht wurde ein Geheimnis in
zwei Schreibweisen — roh und als Mitschnitt-Hexdump. Es lag als schlichter
`xxd -p`-Dump **mitten im durchsuchten Baum** und wurde von beiden Hälften nicht
gesehen. **Wer nach etwas sucht, sucht nach jeder Form, in der es dastehen kann**
— nicht nach den zwei, die bisher aufgefallen sind.

### Vorrichtungen aus lauter Nullwerten prüfen lauter Nullwerte

Acht Umgehungen an einer einzigen Eigenschaft, alle von frischen Prüfern
gefunden, keine von dem, der die Prüfung gebaut hat. Die achte war die
lehrreichste, weil sie nichts mit der Prüfung zu tun hatte, sondern mit ihren
**Daten**.

Beide Verhaltenszäune schrieben Datensätze, bei denen jedes Feld außer Kennung
und Name der Nullwert war. Also ließ sich die Ablehnung an ein beliebiges
anderes Feld hängen —

    case ... && len(u.Roles) == 0:

— und alles blieb grün, obwohl damit praktisch **kein echter Datensatz mehr
geprüft** war: die Vertragsprüfung selbst schreibt Rollen. Dieselbe Lücke gab es
auf der SQL-Seite, ohne den Programmcode überhaupt anzufassen: die Frage um
`AND locked = 0` verengt, fertig.

**Eine Vorrichtung aus Nullwerten pinnt die Eigenschaft nur für Nullwerte.**
Jedes Feld, das dein Test nicht besetzt, ist ein Haken, an den jemand eine
Bedingung hängen kann.

Und die Hälfte, die der Umsetzer hinzugefügt hat und die die Anweisung nicht
hatte: **besetze nicht alle Seiten gleich.** Wären beide Datensätze voll
gewesen, hätte die **umgekehrte** Bedingung (`len(u.Roles) != 0`) überlebt. Die
zwei Ablehnungen müssen sich **über Kreuz** unterscheiden — auf der einen Seite
fragt der volle und hält der leere, auf der anderen andersherum. Erst dann gibt
es kein Feld mehr, das in beiden Fällen dasselbe sagt.

### Strenger machen ist erlaubt — und muss dastehen

„Schwäch nie einen abgenommenen Test ab" hat eine Gegenrichtung, die zweimal
gebraucht wurde und beide Male fast falsch gelaufen wäre.

Ein Umsetzer fand, dass er vier abgenommene Tests rot machen würde, wenn er einen
Baustein **abweisen** ließe statt eine Lüge zu ignorieren. Er hat es **nicht**
getan und es vorgelegt: „Ich halte abweisen für die sicherere Variante und sage
es, statt es durchzudrücken." Richtig — und die Antwort war trotzdem ja.

Der Unterschied ist die **Richtung**:

- **Lockerer machen** ist nie erlaubt. Ein Test, der nach der Änderung etwas
  durchlässt, was er vorher abgewiesen hat, ist abgeschwächt — egal wie
  aufgeräumt die Änderung sonst ist. Im selben Bericht stand dafür das Beispiel:
  eine saubere Umstellung hätte einen Prüfer dazu gebracht, eine gefälschte
  Eingabe mit **ja** zu beantworten. Die Umstellung wurde nicht gemacht.
- **Strenger machen** ist erlaubt, wenn die alte Erwartung eine **Nachsicht**
  festhielt und keine Anforderung. Dann gehört in jeden geänderten Test der Satz,
  **dass** sie eine Nachsicht war — sonst liest der Nächste eine gekippte Zusage
  und stellt sie zurück.

Und eine Schranke darüber: Steht die Nachsicht in einem **eingefrorenen Vertrag**,
gewinnt der Vertrag. Dann wird gemeldet und eingezäunt, nicht verschärft — zwei
Seiten, die auseinanderlaufen, sind schlimmer als eine bekannte Lücke. Das ist
eine Messung, keine Auslegung: nachsehen, was die andere Seite wirklich tut.

### Eine Suche beweist keine Abwesenheit

Der Recovery-Strang hat drei Runden lang eine Zusage verschärft: das entsiegelte
Geheimnis fährt nicht offen über die Leitung, und nach dem Lauf liegt es nirgends
herum. Jede Runde fand einen neuen Weg, jede Runde wurde die Suche breiter — von
einer Differenz zu einer Richtung, von einer Richtung zu einer Buchführung, von
zwei Schreibweisen zu sechs, von einem Unterordner auf den ganzen Baum.

Dann hat ein Prüfer das Geheimnis in **zwei Anfragen zu je vier Byte** geschickt.
Keine Nachricht trug alle acht. **Ohne jede Kodierung.** Alles grün. Und das
Werkzeug hatte das Stückeln **von selbst** gewählt — Zerteilen ist keine Kunst,
das macht der Protokollstapel.

Daneben: dieselbe Datei mit zurückgestelltem Zeitstempel (was `cp -p` und
`tar -xp` ohnehin tun), eine Ablage **über** dem Projektstamm, das Geheimnis in
**Dezimal** statt Hex, und das Geheimnis **im Dateinamen**.

**Eine Nadelsuche kann nie beweisen, dass etwas nicht entwichen ist.** Jede
Erweiterung findet den nächsten Fall und schließt keinen einzigen künftigen.

Daraus folgt **nicht**, dass man aufhört zu suchen — die Suche fängt echte
Fehler, und jede billige, gemessene Erweiterung ist es wert. Es folgt etwas
anderes:

> **Der Anspruch muss der Prüfung entsprechen, nicht der Absicht.**

Wo `ok "das Geheimnis fährt nur an der einen bekannten Stelle"` steht, muss
stehen, **was der Lauf wirklich gesehen hat** — welche Schreibweisen, welcher
Baum, welches Zeitfenster, und dass alles andere ungeprüft ist. Ein Satz, der
mehr verspricht, als gemessen wurde, ist gefährlicher als gar keiner: der
Nächste hört auf zu suchen.

Der Prüfer hat es so zusammengefasst, und daran erkennt man den Unterschied:
**die Dateisuche hatte sechs Schreibweisen gelernt, die Leitungsprüfung kannte
weiter genau eine.**

**Und sie beweist nichts außerhalb ihres Pfades.** Ich habe `find apps/web -name
'*.conf'` laufen lassen, nichts gefunden und einem Strang geschrieben: *"Es gibt
in diesem Repo keine Serverkonfiguration."* Der Satz war wahr und die
Schlussfolgerung falsch — die Konfiguration lag in **einem anderen Repo**, das
Installationsskript schreibt sie selbst, und sie setzt genau die Kopfzeilen
nicht, um die es ging. Wo du `-name` und einen Pfad angibst, schreib das Ergebnis
**mit dem Pfad** hin: nicht "es gibt keine", sondern "unter `apps/web` liegt
keine". Der Unterschied ist der ganze Fund.

### Ein Test, dessen Name mehr verspricht, als er zählt

Eine Unterschriftsprüfung las denselben Gegenstand zweimal — einmal für die
Schlüsselkennung, einmal für die Prüfung — mit einem `await` dazwischen. Tauscht
jemand die Bytes in diesem Fenster, gilt die Kennung für den vertrauten und die
Unterschrift für den fremden Schlüssel: **eine Fälschung wird angenommen.**

Zwei Dateien weiter stand derselbe Fehler noch einmal. Dort gab es sogar einen
Test dafür, und er hieß:

> „a proof, a key and the relying party are **read once**"

Er zählte die Lesungen der **Feldnamen am Behälter** — nicht die Lesungen des
**Byte-Gegenstands**, um den es ging. Der Name sagte die Eigenschaft, der Code
zählte die Umgebung. Deshalb war die Stelle sechs Runden lang unsichtbar,
während eine Tür weiter mit großem Aufwand dieselbe Klasse geschlossen wurde.

**Ein Testname ist eine Behauptung.** Lies ihn und frag: zählt der Rumpf wirklich
das, was der Name sagt — am selben Gegenstand, auf derselben Ebene? Ein Name, der
mehr verspricht, ist schlimmer als gar kein Test: er beendet die Suche.

Und die Gegenprobe dazu ist billig: **nimm die Eigenschaft weg, die der Name
behauptet, und sieh nach, ob er rot wird.** Wird er es nicht, zählt er etwas
anderes.

### Ein Mutant, der grün bleibt, kann auch Tiefe heißen

Die übliche Lesart stimmt meistens: bleibt ein Mutant grün, fehlt ein Zaun. Sie
stimmt nicht immer. Eine Mutation entfernte die Auffangregel für `..` im Pfad —
und der Lauf blieb grün, weil ein **zweites** Modul denselben Pfad noch einmal
mit seiner eigenen Regel ablehnt. Dasselbe galt für eine Tar-Gestalt, die an
zwei Stellen stirbt.

Das ist kein fehlender Zaun, das ist ein Zaun zu viel — Tiefe. Und es ist genau
der Grund, warum eine **dritte** Gestalt nötig war, die nur an **einer** Stelle
abgelehnt wird: erst die fängt die Mutation.

Also: bleibt ein Mutant grün, hast du zwei Möglichkeiten und musst zwischen
ihnen **messen**, nicht raten. Such die zweite Stelle. Findest du sie, schreib
sie hin — und bau die Gestalt, die nur durch die erste läuft.

### Ein Zaun an der falschen Zeile ist kein Zaun

Zweimal am selben Tag stand ein ehrlicher Hinweis am falschen Ort.

Im einen Fall hatte der Koordinator verlangt, an einer Antwort müsse stehen, dass
sie **wissentlich** über eine möglicherweise verlorene Liste ergeht. Der Satz
stand danach in derselben Datei — aber drei Schritte von der Zeile entfernt, die
die Antwort schreibt, und in der Datei, die den Zweig tatsächlich enthält, stand
**gar nichts**. Der Umsetzer hat nachgesehen, statt es abzuhaken, und ihn dorthin
gesetzt, wo ihn trifft, wer den Zweig liest.

Im anderen war eine bekannte Lücke benannt — **170 Zeilen unter dem Schalter**,
um den es ging, während der Kommentar direkt am Schalter vier andere Dinge
erklärte und zu den beiden Sicherheitsschaltern schwieg.

**Der Prüfstein ist nicht „steht es in der Datei", sondern „trifft es den, der
die Stelle liest".** Wer einen Zaun aus Worten baut, setzt ihn an die Zeile, die
jemand ändern wird — und fragt sich: wenn ich in einem Jahr nur diese zehn Zeilen
sehe, weiß ich dann, was ich nicht annehmen darf?

### Ein Mutant, der nicht übersetzt, zählt als tot — und hat nichts gemessen

Ein Strang hat in einer Fixrunde eine Hilfsfunktion ersatzlos entfernt. Danach
hatte die Datei nur noch **eine** Verwendung des Pakets `os`. Seine Mutation
„die Env-Datei bleibt liegen" löscht genau diese Zeile — und damit wurde der
Import unbenutzt, die Mutation **übersetzte nicht mehr**, und der Lauf meldete
sie als **tot**.

Tot ist in jeder Zählung dasselbe Wort, ob der Test sie erschlagen hat oder der
Übersetzer. Die Zahl „46 gebaut, 46 tot" hätte gestimmt und **eine** davon hätte
nichts geprüft. Er hat die Mutation als `_ = os.Remove` neu gebaut: sie
übersetzt, die Datei bleibt liegen, und sie stirbt am richtigen Fall.

**Und dieselbe Form ein zweites Mal, aus derselben Aufgabe:** eine Mutation
überlebte, **weil der Fall, der sie töten sollte, sich im gewöhnlichen Lauf
überspringt** — er braucht root. Sie stand in der Zählung als tot und hatte nichts
gemessen. Der Strang hat sie in die root-Liste verschoben, mit dem Satz:
*"sonst hätte sie wieder eine Zahl ohne Beobachtung geliefert."*

**Die Regel:** Eine Mutation, die nicht übersetzt oder deren Fall übersprungen
wird, ist **kein Ergebnis** — trenn die Fälle in der Zählung, oder sorg dafür,
dass jede Mutation übersetzt **und** dass der Fall, der sie töten soll, in dem Lauf
überhaupt läuft. Drei Wege zu derselben falschen Null: der Übersetzer, das
Überspringen, und ein Lauf, der keinen einzigen Test gefahren hat.
Und sei besonders wachsam **nach dem Löschen** von Code: wer die letzte
Verwendung eines Imports entfernt, macht aus jeder Mutation an dieser Zeile einen
Übersetzungsfehler. Dieselbe Form wie der grüne Lauf ohne einen einzigen
gefahrenen Test — ein Werkzeug, das „kein Problem" sagt, weil es gar nicht
gearbeitet hat.

### Der Mutant muss die alten Tests überleben

Die Empfindlichkeitsprüfung hat eine Falle, in die der Koordinator dieses
Projekts selbst gelaufen ist. Er verlangte für einen neuen Zaun die Mutation
„der Melder reicht **0** durch statt der Zahl, die er halten soll". Der Umsetzer
baute sie, sie starb — und meldete zurück, dass das **nichts beweist**: sie starb
auch an einem Test, der **vorher schon dastand**.

Er baute den richtigen dazu: nicht 0, sondern eine **feste Zahl, die zufällig die
des alten Tests ist**. Die überlebt jeden alten Test und stirbt nur am neuen
Zaun. Erst das zeigt, dass der Zaun etwas hält, was ohne ihn offen wäre.

**Also: ein Mutant, der auch an einem vorher vorhandenen Test stirbt, prüft
deinen neuen Zaun nicht.** Er prüft den alten Test. Bau ihn so, dass er an allem
Alten vorbeikommt, und weis das ausdrücklich nach — sonst hast du einen Zaun, von
dem du nur weißt, dass er neben einem funktionierenden Test steht.

Das ist die schärfere Fassung von „ein Test, der aus dem falschen Grund rot
wird, zählt nicht": hier wird der **richtige** Test rot, nur eben nicht der, um
den es ging.

Und die Regel hat einen Ausgang, den du nicht nehmen darfst. Als derselbe
Koordinator gleich darauf einen zweiten Zaun bestellte, prüfte der Umsetzer ihn
an der neuen Regel — und sie ging nicht auf: die naheliegende Mutation starb auch
an einem Test aus der Runde davor. Der bequeme Weg wäre gewesen, dem **alten**
Test seine Zusicherung wegzunehmen, damit der neue einzigartig dasteht. Er hat es
nicht getan und dazugeschrieben, warum:

> „Ich schwäche keinen abgenommenen Test, damit ein neuer einzigartig aussieht."

Statt dessen hat er eine **schärfere** Mutation gesucht und gefunden: nicht „eine
Zeile je Kanal", sondern „still beim ersten Kanal, eine Zeile bei jedem weiteren"
— genau die Vervielfachung, die ein Riegel für die erste Zeile offen lässt. Die
kommt an allem Alten vorbei und kippt nur den neuen Zaun.

**Wenn dein Mutant an einem alten Test hängenbleibt, ist das eine Aufgabe für den
Mutanten, nie für den alten Test.** Deckung wird dazugebaut, nicht umverteilt.

## 14. Der Satz daneben ist der häufigste Fehler, den dieses Projekt macht

Elfmal hat ein Kommentar das Gegenteil dessen behauptet, was der Code tut.
**Siebenmal davon im selben Commit, der genau diese Klasse reparieren sollte.**
Es ist mit Abstand die häufigste Fundart hier — häufiger als jeder Denkfehler
im Code.

Beispiele, alle gemessen:

- „Wirft nie" an einer Funktion, die bei einem `null` in einer echten Liste warf.
- Ein Kommentar schrieb dem **Vertrag** zu, was bloß eine der Umsetzungen tut —
  in dem Commit, der diese Verwechslung abstellen sollte.
- Ein Satz behauptete, nichts sonst im Paket rufe eine Funktion auf; vier Zeilen
  weiter tat es derselbe Commit.
- Ein Satz versprach „überspringt nur diesen einen Nutzer" — die Änderung darüber
  hatte das Überspringen gerade abgeschafft.

**Deshalb:** Gegenlesen ist ein **eigener Schritt am Ende**, nicht etwas nebenbei.
Nimm jeden Satz, den du geschrieben oder stehen gelassen hast, und such die
Codezeile, die ihn wahr macht. Findest du sie nicht, ist der Satz falsch.

**Und schreib nicht die Messung in den Quelltext.** In einem Fall waren rund 40
von 102 Zeilen Mess-Erzählung; die darin genannten fremden Zeilennummern zeigten
schon sechs Revisionen später auf ganz anderen Code. Der Messbericht gehört in
den Bericht. In den Code gehören zwei Zeilen Begründung — und statt einer
Zeilennummer der **Name** der Stelle, denn Namen wandern mit.

### Die Commit-Nachricht gehört dazu — und sie ist die teuerste

Ein Kommentar lässt sich immer berichtigen. Eine Commit-Nachricht nur, solange
niemand den Commit hat: kein Remote, kein anderer Baum, nichts darüber. Danach
steht sie für immer da.

Dreimal an einem Tag stand in einer Nachricht etwas, das die Messung nicht
trägt. Einmal behauptete sie ungemessen, ein Kanal ende an einer bestimmten
Prüfung — gemessen endete er eine Prüfung später. Einmal sagte sie „fünf Fälle
kamen durch Paket und Vertrag", wo es vier waren — **in dem Commit, der falsche
Behauptungen reparierte**, gefunden vom Umsetzer beim Gegenlesen der eigenen,
gerade erst berichtigten Nachricht.

Also: **lies deine Commit-Nachricht gegen dieselbe Messung wie den Code.** Und
wenn du sie berichtigen musst, prüf zuerst, ob du darfst — `git log`, das
Remote, die anderen Arbeitsbäume. Die Regel „Historie nie umschreiben" schützt
**geteilte** Historie; wo nichts geteilt ist, ist ein Amend kein Umschreiben.
Frag im Zweifel, statt eine Ausnahme auf einen zweiten Zweck auszudehnen.

### Prüf, was du committet hast, nicht was du daliegen hast

Ein Skript lief im Arbeitsbaum grün und war im Commit als `100644` abgelegt —
aus dem Commit heraus brach derselbe Lauf mit `EXIT=126` ab. Aufgefallen ist es
nur, weil der Umsetzer zum Schluss aus `git archive HEAD` gelaufen ist statt aus
seinem Verzeichnis. Dieselbe Sache steckt schon einmal in der Historie dieses
Projekts.

Das Ausführbar-Bit ist nur der häufigste Fall. Alles, was im Arbeitsbaum anders
ist als im Commit — eine nicht vorgemerkte Datei, ein `.gitignore`, ein
Dateimodus, eine Zeilenendung —, macht aus „bei mir läuft es" eine Aussage über
etwas, das niemand sonst bekommt. **Der letzte Lauf einer Aufgabe geht aus dem
Commit**, nicht aus dem Baum.

### Den Gegenfall baust du, du denkst ihn nicht

Die zählebigste Sorte ist der Absolutsatz: „nur", „immer", „nie", „das einzige",
„kann nicht", „es gibt keinen anderen Weg". Er klingt nach Sorgfalt und ist in
Wahrheit die Stelle, an der jemand aufgehört hat zu prüfen.

Ein Umsetzer hat es nach der dritten Runde in Folge selbst so gesagt, und er hat
recht: **den Gegenfall zu bauen statt ihn zu überlegen gehört als fester Schritt
ins Gegenlesen** — nicht in den Auftrag der jeweiligen Runde.

Wie oft das trägt, ist gemessen. In einer einzigen Datei fielen drei solcher
Sätze nacheinander, jeder in dem Commit, der den vorigen reparieren sollte:

- „nur eine Aufgabe, die direkt an `encodeTask` geht, kann so ein Array tragen" —
  ein zweiter Weg stand zwanzig Zeilen weiter in derselben Datei.
- „und beim zweiten Aufruf dasselbe. Nie zwei." — ein `Proxy` über ein
  `Uint8Array` besteht `instanceof` und liefert zwei verschiedene Antworten.
- „das Zurücklesen ist **der einzige** Weg, das zuzusagen" — die Alternative
  bestand alle acht Tests und alle Angriffsfälle.

Der dritte Satz ist der lehrreiche: die Alternative war **nicht besser**, sie war
nur **möglich**. Der Satz hätte gestimmt, wenn er den Grund genannt hätte statt
der Einzigkeit. Genau so schreibst du ihn: **warum du diesen Weg wählst**, nicht
dass es keinen anderen gäbe. Das ist auch die ehrlichere Aussage, denn sie bleibt
wahr, wenn jemand später einen zweiten Weg findet.

Und wenn du den Gegenfall gebaut hast und er fällt: schreib in den Bericht, was
er kostet. „An zwölf Fällen gemessen, drei unterscheiden sich" ist eine Aussage.
„Es gibt keinen anderen Weg" ist keine.

**Ein widerlegtes „geht nicht" kippt die Entscheidung nicht — es verbessert
ihren Grund.** Ein Umsetzer schrieb, keine Prüfung in seinem Paket könne einem
Fehler ansehen, auf wessen Seite er entstand. Der Prüfer hat zwei gebaut, die
alle vier eigenen Fälle fangen. Die Entscheidung, sie dort **nicht** einzubauen,
blieb trotzdem richtig — aus einem Grund, den vorher keiner der beiden hatte:
die vier Fälle hängen allein an der Konfiguration, und die ist für den ganzen
Prozess konstant. Ein Kanal kann daran nicht scheitern, ohne dass alle daran
scheitern. Der Platz für die Prüfung ist der Programmstart, nicht der
Verbindungsaufbau — dort kostete sie einen Rechenschritt an der teuersten
Stelle, je Verbindung.

Deshalb ist „geht nicht" auch als Begründung schlecht, selbst wenn sie zufällig
stimmt: sie sagt nichts über das Problem. „Gehört woanders hin, weil …" hält,
und hält auch dann noch, wenn jemand den Weg findet.

---

### Und das fehlende Fremdsystem baust du auch

Der stärkste Fall dieser Regel kam beim Datenbankteil. Eine Funktion schloss aus
einer Zeilenzahl auf einen Sachverhalt, und diese Zahl zählt je nach Treiber
Getroffenes oder Geändertes. Auf der hier verfügbaren Datenbank fällt das nie
auf; die andere gibt es in diesem Projekt nirgends.

Der Koordinator hat deshalb ausdrücklich **kein Rezept** vorgeschrieben, sondern
ein Kriterium — „die Antwort darf nicht davon abhängen, wie gezählt wird" — und
dazugesagt: *geht das hier nicht zu fahren, sag mir warum, mit der Messung, und
zäun die Lücke statt dessen ein.*

Der Umsetzer hat den Ausweg nicht genommen. Er hat die fehlende Zählweise
**nachgebaut**: ein Trigger auf der vorhandenen Datenbank, der einen Schreibsatz,
der nichts ändert, still verwirft — danach zählt sie genau wie die andere.
Gemessen: unveränderter Schreibsatz 0 statt 1, echter 1. Damit war der Fehler
zuerst **vorgeführt**, dann behoben, und der Test fährt dasselbe Skript nun
**zweimal, je Zählweise**, und hält, dass beide dasselbe antworten.

Zwei Dinge daran sind die Regel:

- **Ein fehlendes Fremdsystem ist selten ein Grund, nicht zu messen.** Oft
  braucht es nur die eine Eigenschaft, um die es geht — und die lässt sich auf
  dem vorhandenen System herstellen. Such danach, bevor du „nicht prüfbar"
  schreibst.
- **Die Vorrichtung ist selbst ein Messgerät** (Regel 5). Er hat sie mitgemessen:
  Trigger weg, und die Prüfung der Vorrichtung wird rot. Ohne das hättest du eine
  Zusicherung, die auf einer Behauptung über deinen eigenen Prüfstand steht.

„Nicht prüfbar" bleibt eine ehrliche Antwort. Sie ist nur die **zweite**.

**Dritter Fall am selben Tag, und der teuerste von den drei:** Ich habe einem
Strang geschrieben, Firefox könne `deriveBits` und `sign` nicht — als Ursache,
nicht als Vermutung. Er hat gemessen: Firefox kann `sign`, `verify`,
`deriveBits`, `generateKey` und `importKey` für beide Kurven. Es kann genau
**eins** nicht: `exportKey("jwk")` eines Schlüssels, der per `importKey("pkcs8")`
hereinkam. Mein Satz, wörtlich befolgt — „unterschreiben" und „ableiten" statt
anlegen und einführen — hätte **beide** Wächter auf Firefox weiter ja sagen
lassen und **alle 157 Fälle rot gelassen**. Die Aufgabe ist nur gelungen, weil er
der Messung gefolgt ist und nicht mir.

Daraus die schärfere Fassung derselben Regel: **wer eine Aufgabe ausgibt, gibt
die Erscheinung aus, nicht die Ursache.** Schreib hin, was zu sehen ist — welche
Fälle rot sind, mit welcher Meldung — und sag ausdrücklich, dass die Ursache zu
messen ist. Eine Ursache im Auftrag ist eine Abkürzung, die der Empfänger nicht
mehr prüft, weil sie von oben kommt.

### Ein Satz kann im Moment seiner Berichtigung falsch werden

Der schnellste Fall dieser Regel dauerte einen Commit. Eine Doku sagte, welche
Pakete ein bestimmtes Ding überhaupt bauen. Sie wurde in einer Fixrunde
berichtigt — und **dieselbe Fixrunde** legte in einem dritten Paket einen neuen
Aufrufer an. Der berichtigte Satz war falsch, bevor jemand ihn zum zweiten Mal
gelesen hatte.

Daraus folgt nicht „gründlicher berichtigen", sondern etwas Einfacheres:
**Aufzählungen, wer etwas benutzt, verrotten bei jedem neuen Aufrufer.** Wenn du
eine schreibst, frag dich, ob der Satz auch ohne sie trägt. Meistens ja — und
dann lass sie weg.

### Eine Zeilennummer ist keine Quellenangabe

Der Koordinator hat eine Berichtigung angeordnet — und in die Anweisung selbst
eine Zeilennummer geschrieben, die er von einem Prüfer übernommen und nie
nachgeschlagen hatte. Zwei Dinge waren falsch, und die Nummer ist nur das eine:

- In diesem Baum steht der gemeinte Test auf **415**, nicht auf 526.
- Zeile 526 ist nicht leer. Dort steht ein Kommentar **in einem auskommentierten
  Block**. Wer die Angabe nachschlägt, findet etwas, das wie Code aussieht — und
  hält eine falsche Quelle für bestätigt.

Der Umsetzer hat den Test stattdessen **beim Namen** genannt. Das ist richtig und
es hält: ein Name übersteht jede eingefügte Zeile darüber, und wenn er falsch
ist, ist er sofort falsch statt glaubhaft richtig.

**Verweise auf Namen, nicht auf Zeilen.** Eine Zeilennummer im Fließtext sagt
nichts darüber, worauf sie zeigt, sie altert mit dem nächsten Commit, und ihre
Fehler sehen aus wie Treffer.

### Ein Verzeichnis, das die Lücke breiter aufschreibt als sie ist

In mein Verzeichnis der offenen Punkte hatte ich geschrieben: "der
Sitzungsschlüssel gibt es nach der Anmeldung nirgends im Browser". Gemessen ist
es enger: die Anmeldung **baut** das Schlüsselpaar und **öffnet** damit sogar
die Leitung — sie gibt es nur nicht weiter. Ein Satz, drei Wörter zu breit.

Hätte ich damit einen Implementierer losgeschickt, hätte er angefangen zu
bauen, was schon da ist — und wäre beim Suchen auf die fertige Stelle gestoßen
und hätte sich gefragt, wem er glauben soll.

**Die Regel:** Dasselbe Gegenlesen, das du dem Kommentar neben dem Code
schuldest, schuldest du deiner eigenen Notiz. Eine Lücke, die du aufschreibst,
schreibst du mit der **Stelle** auf, an der sie aufhört: welche Datei, welche
Zeile, was dort schon steht. Wer das nicht nachlesen kann, muss dir glauben — und
ein Verzeichnis, dem man glauben muss, ist ein Verzeichnis, das altert.

### Fünf Stränge bringen fünf Kopien derselben Konstante mit

Fünf Sitzungen bauten gleichzeitig je einen Vertrag, jede in ihrem eigenen Baum,
jede sauber. Erst das Zusammenführen zeigte, was keine von ihnen sehen konnte:
**`MAX_PACKAGE` und `MAX_MANIFEST` standen danach zweimal da, `MAX_ENTRIES`
dreimal.** Auf der Go-Seite ist jede davon **eine** Konstante.

Niemand hat etwas falsch gemacht. Jeder brauchte die Zahl, keiner konnte den
Baum der anderen sehen, und ein Brief nennt nun einmal die Werte, die seine
Aufgabe braucht. Die Doppelung entsteht **durch die Parallelität selbst**.

Daraus folgt zweierlei. Beim Aufteilen: sag jedem Strang, welches Modul eine
geteilte Zahl **besitzt**, auch wenn dieses Modul noch gar nicht existiert.
Und beim Zusammenführen: such ausdrücklich nach Konstanten, die jetzt mehrfach
dastehen — der Lauf bleibt grün, solange die Kopien gleich sind, und genau
deshalb findet ihn kein Test.

### Der Vertrag kann vollständiger sein als der Plan, nicht nur strenger

Eine Sitzung meldete einen Fund: die Oberfläche bitte den Menschen, einen
Fingerabdruck mit dem Serverbildschirm zu vergleichen — aber der Installer zeige
diesen Fingerabdruck gar nicht, die Bitte sei also nicht erfüllbar. Sauber
begründet, mit Quellenangabe.

Nachgelesen im **eingefrorenen Vertrag**: dort steht, dass der Installer am Ende
den Einmal-Code **und den TLS-Fingerabdruck** zeigt. Es gibt etwas zu
vergleichen. Der Fund war falsch.

Ihre eigene Lehre daraus ist die Regel:

> „Ich habe den **Plan** gegen die Oberfläche gelesen statt den **Vertrag** — und
> die Fehlerrichtung war die ungewohnte: der Vertrag ist vollständiger als der
> Plan."

Man geht zum Vertrag, wenn man vermutet, dass er **strenger** ist. Dass er
**mehr enthält** als der Plan, erwartet niemand — und deshalb sucht niemand
dort, wenn etwas zu fehlen scheint. Vier Aufgaben dieses Strangs haben Zahlen im
Plan gegen `shared/` berichtigt, alle in dieselbe Richtung; dieser eine Fall ging
andersherum, und genau darum fiel er durch.

**Also: bevor du etwas als fehlend meldest, sieh in `shared/` nach — auch dann,
wenn du dort nichts Strengeres erwartest.** Ein `CLAUDE.md`, das auf eine
Vertragsdatei zeigt, meint sie auch.

### Auf der anderen Seite heißt dieselbe Sache anders

Der Koordinator hatte eine Verschärfung bewilligt — **unter der Bedingung**, dass
der eingefrorene Vertrag auf der anderen Sprachseite dasselbe tut. Er konnte
nicht sagen, wie man das dort überhaupt misst: die Fehlerform war „ein Gegenstand
lügt über seine Länge", und diese Sprache hat gar keinen Längen-Getter, über den
sich lügen ließe.

Der Umsetzer hat das Gegenstück gefunden, statt „nicht prüfbar" zu schreiben: ein
Feld, das **mehr echte Bytes trägt, als es meldet**. Damit ließ sich dieselbe
Frage stellen — und die Antwort war eindeutig, aus einem Lauf statt aus einer
Auslegung:

| Aufruf | gemeldet / echt | Ergebnis |
| --- | --- | --- |
| Budget 7 | 7 gemeldet, 100 echt | **genommen** |
| Budget 6 | 7 gemeldet, 100 echt | abgewiesen |
| Verschlüsseln | an der Grenze, mehr dahinter | **genommen** |

**Der Vertrag ignoriert, was jenseits der gemeldeten Länge liegt, und entscheidet
auf der gemeldeten Zahl allein.** Damit war die Verschärfung blockiert — nicht
weil sie schlecht wäre, sondern weil die zwei Seiten sonst auseinandergelaufen
wären. Eingezäunt statt zugemacht, mit der Messung im Kommentar.

**Bevor du „nicht prüfbar" schreibst, such das Gegenstück.** Eine Eigenschaft hat
auf der anderen Seite fast immer eine Entsprechung — sie heißt nur anders. Und
ein Vertrag, der eine Nachsicht festhält, hält sie für beide Seiten: ihn auf
einer Seite zu verschärfen heißt, ihn zu brechen.

### Wer den Zweig hochschiebt, gibt die Nachricht aus der Hand

Der Koordinator hat fünf Arbeitszweige aufs Remote geschoben, damit fremde
Sitzungen sie prüfen können. Gute Entscheidung — mit einem Preis, den er nicht
mitgedacht hat: **ab diesem Moment lässt sich keine Commit-Nachricht darin mehr
berichtigen.** Ein `commit-tree` samt `update-ref` bräuchte danach einen
erzwungenen Push, und der ist verboten.

Ein Umsetzer bekam kurz darauf den Auftrag, genau so eine Nachricht zu
berichtigen. Er hat es **nicht** getan, sondern nachgesehen, worauf das Remote
steht, und die Berichtigung statt dessen in den Code und in die **nächste**
Nachricht geschrieben.

Daraus zwei Dinge:

- **Berichtige eine Nachricht, solange sie dir gehört** — kein Remote, nichts
  darüber, kein zweiter Arbeitsbaum. Danach ist sie Geschichte.
- **Schieb einen Zweig erst hoch, wenn du seine Nachrichten nicht mehr anfassen
  willst.** Der Zugewinn (fremde Augen) ist es oft wert; nur muss man wissen,
  was man dafür hergibt.

### Und dieselbe Form nach innen: dein eigener Zähler

Alle Fälle oben handeln vom Gegenstand eines **Aufrufers**, der sich zwischen
zwei Lesungen ändert. Die gefährlichste Fassung dieser Form kam aus der eigenen
Tasche.

Der Entwurf für die Sitzungsleitung baute die Nonce so:

    const iv = ivFrom(this.n);        // lesen
    const out = await crypto.subtle.encrypt(…, iv, …);   // warten
    this.n++;                         // erhöhen

Zwei Aufrufe, die sich überlappen, lesen **dieselbe** Zahl. Für einen Zähler
wäre das ein Zählfehler; für eine **Nonce** ist es der Totalschaden: eine Nonce,
zwei Klartexte, und die Vertraulichkeit eines AEAD ist dahin. Kein Aufrufer muss
dafür lügen, keine Lücke muss offenstehen — es genügt, dass zweimal gesendet
wird, bevor das erste `await` zurückkommt.

Gefunden wurde es beim Bauen, nicht von einem Test: ein Test hätte zwei
gleichzeitige Sendungen anstoßen müssen, und der naheliegende Test schickt eine.
Repariert mit einer **Sendekette** und einer **Empfangsschleife** — die Zahl wird
genommen und erhöht, bevor irgendetwas wartet.

**Also: wo du einen Wert nimmst, der sich pro Benutzung ändern muss, nimm ihn
und schreib ihn fort, bevor du wartest.** Zwischen `lesen` und `erhöhen` darf
kein `await` stehen. Und wenn dir das wie eine Kleinigkeit vorkommt: dieselbe
Form, nach außen gerichtet, hat diesen Strang zehn Runden gekostet.

## 15. Beim dritten Mal baust du den Wächter, nicht die Reparatur

Ein Strang hatte eine Fehlerform, die immer gleich aussieht: ein Gegenstand, der
seine eigene Länge angibt, gibt bei der **zweiten** Lesung eine andere Zahl an
als bei der ersten. Wer die Länge zweimal liest — einmal zum Prüfen, einmal zum
Verarbeiten —, prüft etwas anderes, als er verarbeitet.

Acht Fälle, über mehrere Runden, jeder einzeln gefunden und einzeln repariert.
Der achte war der teuerste: eine Funktion las die Länge in einem einzigen Lauf
**303-mal**; mit einer Lüge auf der ersten und der fünften Lesung ging ein
Auftrag von 70 027 Byte durch eine Grenze von 16 384 — mit gültiger Unterschrift,
richtiger Form und richtiger Bindung. Und im selben Bericht stand der Satz, der
diese Regel ausgelöst hat:

> „Ein Wächter hätte alle vier auf einmal gefunden."

Das Signal ist nicht der dritte **Fehler**. Es ist der dritte Fehler **derselben
Form**. Ab da ist jede weitere Einzelreparatur die teurere Wahl: Sie kostet eine
Runde, findet genau einen Fall, und der nächste liegt schon daneben.

**Wenn du dieselbe Form zum dritten Mal reparierst, halt an und bau statt dessen
den Wächter.** Vier Dinge gehören dazu:

- **Er gehört dorthin, wo die Sache herkommt**, nicht dorthin, wo der letzte Fall
  aufgetreten ist. Der Helfer, der die Bytes einmal festhält, gehört zur
  Byte-Ebene — dann findet der Wächter dort alle Aufrufer statt nur die zwei,
  die gerade wehgetan haben.
- **Er zählt die Eingänge, und die Zahl ist Teil der Zusicherung.** Ein Wächter,
  der nur sagt „die, die ich kenne, sind sauber", übersieht den neunten, den
  jemand nächste Woche danebenlegt.
- **Er wird empfindlichkeitsgeprüft wie jeder Zaun** (Regel 13): ein Mutant, der
  einen Eingang wieder zweimal lesen lässt, muss ihn rot machen — an **seiner
  eigenen** Zusicherung.
- **Er darf auf gutem Code nicht rot werden** (Regel 7), sonst schaltet ihn der
  Nächste ab. Also benennt er genau die Eigenschaft und nicht ihre Umgebung.

Und der ehrliche Teil: Solange der Wächter nicht steht, gehört jeder bekannte,
nicht reparierte Fall in den Bericht — mit der Messung, nicht als Vermutung. Der
Umsetzer hat den fünften Fall genannt, obwohl er ihn nicht nachgeprüft hatte, und
**das ausdrücklich dazugeschrieben**. Genau so.

### Wenn fünf Runden fünf neue Gestalten finden, war die Reparatur die falsche Ebene

Regel 15 sagt: beim dritten Fehler derselben Form baust du den Wächter. Der
Web-Strang hat das getan, und der Wächter war gut — er fand beim ersten Lauf
fünfzehn Fälle, die acht Runden Handsuche übersehen hatten.

Danach ging es trotzdem weiter. Fünf Runden, fünf Prüfungen, jedes Mal eine
**neue Gestalt** desselben Fehlers: ein Gegenstand, dessen Länge beim Lesen
umlegt · einer, der wirft · einer, der den Puffer **umschreibt** und die ehrliche
Zahl zurückgibt · einer, bei dem das Lesen die Länge **wachsen** lässt. Jede
Runde schloss die gemessenen Fälle sauber. Die nächste Prüfung fand die nächste
Gestalt.

**Das ist ein anderes Signal als der dritte Fehler derselben Form.** Der dritte
Fehler sagt: bau den Wächter. Die fünfte neue **Gestalt** sagt: die Reparatur
sitzt auf der falschen Ebene.

Hier war die Ursache ein Satz, der für jede Gestalt gilt: **ein Getter ist Code
und läuft an jeder Stelle, an der jemand den Gegenstand anfasst.** Solange die
Prüfung den fremden Gegenstand befragt und die Engine danach auf denselben
Speicher sieht, gibt es immer eine weitere Gestalt.

Der Ausweg hat deshalb nichts mehr mit Lesen zu tun: **der Engine wird eine Kopie
gegeben, nicht die Sicht auf den fremden Puffer.** Was der Aufrufer nicht mehr
anfassen kann, kann er nicht mehr steuern — gleich welcher Bauart.

**Das Muster, an dem du es erkennst:** Wenn du dieselbe Zusicherung zum dritten
Mal *verschärfst* und beim vierten Mal wieder eine neue Gestalt kommt, hör auf,
die Prüfung zu verbessern. Frag statt dessen, welche **Annahme** allen Gestalten
gemeinsam ist — und ob man sie wegnehmen kann, statt sie besser zu prüfen.

Und der Preis gehört gemessen, nicht geschätzt: eine Kopie ist bei zweiunddreißig
Byte nichts und bei einer großen Nachricht vielleicht etwas. Wer die sichere
Ebene wählt, sagt dazu, was sie kostet.

### Und die Gestalt, die die Entscheidung bewies

Der Nachtrag zu dieser Regel ist der Fall, der sie endgültig belegt hat — er kam,
**nachdem** entschieden war, die Ebene zu wechseln, und er hätte auf der alten
Ebene nie gefunden werden können.

Sieben Bauarten hatten alle eines gemeinsam: irgendwo lief ein Getter des
Aufrufers, und die Verteidigung bestand darin, ihn seltener, später oder in
anderer Reihenfolge laufen zu lassen. Die achte lief **ohne jeden Getter**. Ein
ganz gewöhnliches Byte-Feld, und die Umschreibung nur **vorgemerkt**:

| | |
| --- | --- |
| Was jede synchrone Prüfung sah | Müll |
| Was die Engine bekam | die echte Unterschrift |
| Antwort | **ja** |

Es wurde nichts gelesen. Keine Zählung, keine Reihenfolgeprüfung, kein
Lesungsprotokoll kann das sehen — die Lücke sitzt zwischen der synchronen Prüfung
und dem späteren Aufruf, und in dieses Fenster passt jede Änderung am fremden
Speicher.

**Daran erkennt man den Ebenenfehler im Nachhinein am klarsten:** die sieben
Gestalten sahen wie Gegner aus, die man einzeln schlägt; die achte zeigte, dass
sie alle nur Erscheinungen **einer** Annahme waren — *die Bytes, die ich geprüft
habe, sind die Bytes, die verarbeitet werden*. Solange die Engine auf fremden
Speicher sieht, ist diese Annahme falsch, unabhängig davon, wer wann was liest.

Der Preis der richtigen Ebene wurde gemessen und ist klein: bei vier Megabyte
**1,3 Millisekunden** und acht Megabyte Spitze, bei zweiunddreißig Byte gar
nicht messbar. Wer die Ebene wechselt, nennt die Zahl — dann ist es eine
Entscheidung und keine Hoffnung.

### Gewürfelt sind die Werte, nicht die Gestalt

Zwölf Umgehungen an einer Eigenschaft, und die zwölfte ist die lehrreichste.

Eine Vorrichtung bewies zu Beginn und am Ende, dass ein Datenbankindex wirklich
weg war. Der elfte Weg hatte den **festen Namen** ausgenutzt, mit dem der Beweis
fragte: ein Teilindex, der genau diesen Namen ausspart, lässt beide Beweisläufe
ehrlich bestehen und bewacht alles andere. Die Reparatur war naheliegend und
richtig — Name und Kennungen werden jetzt **je Aufruf frisch gewürfelt**.

Dann kam der zwölfte:

    WHERE length(name) <> 26
    WHERE length(id)   <> 30
    WHERE substr(id,1,4) NOT IN (…)

Der gewürfelte Name ist **immer 26 Zeichen** lang. Die Kennung wird aus einer
festen Vorsilbe gebaut. **Gewürfelt waren die Werte, nicht die Gestalt** — und
die Gestalt genügt vollkommen, um genau die zwei Beweiszeilen auszusparen.

Daran ist nichts zu reparieren, und das ist der Punkt: **die Beweiszeilen müssen
unterscheidbar sein, sonst fände der Beweis sie nicht — und jede Eigenschaft, die
sie unterscheidbar macht, ist ein Griff.** Auch ein Name zufälliger Länge hat
eine Länge. Auch eine Zeile ohne jedes Merkmal ist „die Zeile, die es zur
Beweiszeit gab".

**Wenn du zum zwölften Mal dieselbe Zusicherung verteidigst, verteidigst du keine
Eigenschaft mehr, sondern dich selbst gegen den Nächsten, der die Datei
bearbeitet.** Das kann kein Test. Schreib den letzten Weg als Beleg dafür hin,
dass die Reihe endet, und hör auf — nicht, weil es genug wäre, sondern weil der
nächste Zaun dieselbe Lücke unter anderem Namen hätte.

## Kurzfassung für den Anfang einer Aufgabe

1. Auftrag lesen. Widersprüche **melden**, nicht umgehen.
2. Erst der Test, der fehlschlägt — eigener Commit.
3. Dann der Code. Grenzen beidseitig. Voraussetzungen herstellen.
4. **Bau den falschen Baustein** und sieh nach, ob dein Test ihn fängt.
5. Jeden Satz, den du schreibst, gegen den Code lesen — als **eigenen Schritt**
   am Ende, nicht nebenbei.
6. Zahlen messen, Herkunft dazuschreiben.
7. Im Zweifel die sichere Seite, Preis nennen.
8. Was nicht messbar war, offen melden.
