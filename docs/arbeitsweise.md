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

## 13. Was ehrlich offen bleibt, bleibt ehrlich offen

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
