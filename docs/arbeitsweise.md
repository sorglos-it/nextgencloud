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

## 2. Ein Test, dessen Voraussetzung nicht hergestellt ist, beweist nichts

Ein Pflichttest **hoffte** auf einen blockierten Schreibvorgang, statt ihn
herzustellen. Auf Windows entstand er, auf Linux schluckte ihn der Puffer nach
250 ms — zweimal rot, einmal grün, am selben Commit.

Danach: ein vorgeschalteter Weiterleiter sperrt die Richtung, der Fall entsteht
sicher. Fünf Läufe grün, Streuung 440 ms.

**Stell die Bedingung her. Und lass den Test laut scheitern, wenn sie doch nicht
entsteht** — sonst rutscht er still durch.

## 3. Eine Grenze ist erst belegt, wenn ein Fall beide Seiten festnagelt

Genau auf der Grenze muss es durchgehen, eins darüber abgelehnt werden. Ein
Test, der nur die Überschreitung prüft, bliebe auch grün, wenn die Grenze bei
null stünde.

Gefunden: ein Schutz gegen Zurückstellen der Uhr brach ab **genau zwei
Stunden** — der Test maß bei einer Stunde und einer Sekunde, also knapp
darunter. Neun Abstände später war es dicht.

## 4. Miss, ob dein Messgerät überhaupt misst

Ein Zeittest verglich zwei Codewege und war „fünfzig Läufe ohne Flattern" grün.
Nachgemessen: **23 von 25 Einzelmessungen waren glatt null.** Die Uhr des
Rechners löst eine Millisekunde auf, gemessen wurden 30 Mikrosekunden.
`0 > 2*0` ist falsch — der Test konnte **nie** fehlschlagen.

Bei Zeitmessungen: Stapel von mindestens 2000 Läufen, verschränkte Arme, und
eine **Null-Kontrolle** (derselbe Arm zweimal). Oder ganz ohne Uhr bauen — das
ist fast immer möglich und immer besser.

## 5. Ein Fuzz-Ziel, das den Annahme-Anteil kumuliert, taugt nicht

Der Fuzzer optimiert auf Code-Abdeckung. Weil die **Ablehnungswege** die
Abdeckung tragen, **drückt er den Annahme-Anteil aktiv herunter** — gemessen von
45 % auf unter 5 % in einer Minute. Eine Prüfung „mindestens ein Drittel wird
angenommen" hört damit von selbst auf zu greifen.

**Nimm eine Prüfung bei jeder Eingabe, die der Fuzzer nicht umsteuern kann**,
und miss den Anteil über die **festen Saaten**.

Und prüf die Prüfung: in einem Fall erhöhte der Kanarienvogel **beide** Zähler,
sodass die Anteilsschranke rechnerisch nie anschlagen konnte.

## 6. Ein Wächter, der auf gutem Code rot wird, wird abgeschaltet

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

Und: **jede Runde neue Mechanik bringt neue Löcher.** Ab einem Punkt heißt die
Anweisung „schließen und Zusagen engen, **kein neuer Erkennungsmechanismus**" —
Verbote sind erlaubt, Erkennung nicht.

## 7. Ein Fehler im Auftrag ist ein Fund, kein Hindernis

**Acht** Auftragsfehler haben Umsetzer in NGC gefunden. Jedes Mal war die
Meldung wertvoller als der Code. Darunter:

- ein Testhelfer rief eine `async`-Funktion **ohne `await`** — der `catch` war
  unerreichbar, die Zusicherung hätte **dauerhaft rot** sein müssen;
- ein Prüfvektor gab dem „Passwort-Admin" einen Passkey — der Haupttest hätte
  nie grün werden können;
- eine Größengrenze war um 64 KB zu klein gerechnet.

**Folge dem eingefrorenen Vertrag, nicht dem Auftrag** — das wurde dreimal so
entschieden. Und melde es, statt es still zu umgehen.

## 8. Die Anweisung des Koordinators steht unter derselben Regel

**Dreimal** hat ein Umsetzer eine Vorgabe von mir berichtigt, weil ich eine
Nebenwirkung nicht durchgerechnet hatte. Jedes Mal hatte er recht.

Einmal entstand eine **Kette aus drei ungeprüften Behauptungen**: ein Prüfer
vermutete eine Shell-Einstellung als Lösung „der ganzen Klasse", der Koordinator
gab es weiter, der Umsetzer schrieb es ungeprüft in den Code. Der nächste
Prüfer maß nach: sie schließt einen Teil der Fälle — **genau den nicht**, um den
es ging.

**Kommt dir eine Anweisung nicht stimmig vor: frag, bevor du sie ausführst.**

## 9. Im Zweifel die sichere Seite

Steht Sicherheit gegen Bequemlichkeit, gegen Geschwindigkeit oder gegen Aufwand:
**sichere Seite, Preis in einem Satz nennen, weitermachen.** Nur fragen, wenn
die sichere Seite etwas kostet, das der Auftraggeber nicht absehen kann.

Ein Sicherheitsmerkmal wird **nie** abgeschwächt, damit etwas durchläuft. Geht
etwas mit den vorhandenen Werkzeugen nicht: **stoppen und fragen.**

Aber: **rechne den Handel durch, bevor du ihn machst.** Ein „Schutz", der zwei
Drittel eines Zeitunterschieds wegnimmt und dafür eine **unbegrenzte,
unauthentifizierte Schreiberlaubnis** auf eine Tabelle ohne Aufräumen eintauscht,
ist keiner.

## 10. Zwei Commits: erst der rote Test, dann der Code

Sonst ist „rot vor grün" nicht belegbar. Ein Prüfer konnte es nicht bestätigen,
weil beides in einem Commit lag.

## 11. Der Prüfer greift an, statt zu lesen

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

## 12. Was ehrlich offen bleibt, bleibt ehrlich offen

„Ungeklärt" ist erlaubt — **unbegründet weglassen** nicht.

Ein Umsetzer meldete drei Minuten ohne Ergebnis auf einer Engine als ungeklärt.
Eingegrenzt stellte sich heraus: eine Schleife brauchte dort **5,4 Minuten** für
die größte zulässige Liste. Auf einem iPhone wäre die Seite unbenutzbar gewesen.
Nach dem Umbau: 20 Sekunden im Extremfall, unter einer Zehntelsekunde im
Normalfall.

Wer eine Zahl nennt, sagt dazu, **wie** gemessen wurde. Wer eine nicht hat, sagt
das — und der Koordinator entscheidet, ob sie gebraucht wird.

---

## Kurzfassung für den Anfang einer Aufgabe

1. Auftrag lesen. Widersprüche **melden**, nicht umgehen.
2. Erst der Test, der fehlschlägt — eigener Commit.
3. Dann der Code. Grenzen beidseitig. Voraussetzungen herstellen.
4. Jeden Satz, den du schreibst, gegen den Code lesen.
5. Zahlen messen, Herkunft dazuschreiben.
6. Im Zweifel die sichere Seite, Preis nennen.
7. Was nicht messbar war, offen melden.
