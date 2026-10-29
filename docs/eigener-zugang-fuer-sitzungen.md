# Sollen Sitzungen einen eigenen Zugang bekommen?

**Vorgelegt am 29.10.2026. Nicht entschieden — das gehört Thomas.**
Dieses Blatt wägt ab. Was daran gemessen ist, steht unter „Gemessen"; alles andere
ist abgewogen und als solches gekennzeichnet.

## Worum es geht

Eine Sitzung wie diese handelt heute **als Thomas**. Sein Kennwort liegt dort, wo ein
Skript es lesen kann. Solange das so ist, steht in jeder Zeile des Prüfprotokolls
derselbe Benutzer — und hinterher ist aus dem Protokoll **nicht** zu beantworten, wer
gehandelt hat. Am 24.10.2026 hat genau diese Frage eine Stunde gekostet.

## Gemessen

- **Das Protokoll führt je Eintrag einen Benutzer.** Es hält „Anmeldungen und
  Fehlanmeldungen, Änderungen an Benutzern, Rechten, Apps, Läden und Lizenzen, jede
  Wurzelaufgabe mit ihrer Unterschrift und jeden Kanal, der geöffnet wurde"
  (`apps/server-api/classes/audit/audit.go:1`–`:5`). Das Feld ist also da; es trägt nur
  bei jedem Eintrag denselben Namen.
- **Für einen eigenen Benutzer braucht niemand Verwalterrechte.** Wer **nicht** `admin`
  ist, darf in einer neuen Fassung der Vertrauensliste **nur seine eigenen Schlüssel**
  ändern (`shared/trust/trust.go:304`, `onlyOwnKeys`). Ein Sitzungskonto kann sich also
  anmelden, seine Apps benutzen und sein eigenes Kennwort wechseln — und sonst nichts an
  der Liste.
- **Umgekehrt: wer Benutzer anlegen darf, darf alles.** Eine neue Fassung mit einem
  **fremden** Benutzer darin nimmt die Liste nur von einem `admin` an (dieselbe Zeile).
  Ein Sitzungskonto, das selbst Konten anlegen soll, ist damit zwangsläufig ein zweiter
  Verwalter. Einen Mittelweg gibt es im Vertrag nicht.
- **Es gibt keinen Einladungsweg.** Wer ein Konto anlegt, legt auch dessen erstes
  Kennwort fest und kennt es, bis der Inhaber es ändert (Punkt 73, Weg (b);
  Weg (a) ist entworfen und **nicht gebaut**).
- **Das Kennwort selbst habe ich nicht gesucht und nicht gelesen.** Dass es in einer
  Datei liegt, steht in der Aufgabe. Es nachzusehen hätte es in dieser Sitzung sichtbar
  gemacht, und das ist genau der Zustand, um den es hier geht.

## Die Wege, mit Preis

### (a) Alles bleibt, wie es ist

- **Preis:** Das Protokoll bleibt als Beweismittel unbrauchbar, wer gehandelt hat. Jeder
  Strang, jedes Skript und Thomas selbst sind im Protokoll eine Person. Ein Fehlgriff
  einer Sitzung ist von einem Fehlgriff von Thomas nicht zu unterscheiden — auch nicht
  rückwirkend, auch nicht von ihm selbst.
- **Kostet an Arbeit:** nichts.

### (b) Ein Konto für alle Sitzungen, eigene Rolle, nicht `admin`

Ein Benutzer `sitzung` mit einer eigenen Rolle, die nur die Rechte hat, die Sitzungen
wirklich brauchen.

- **Gewinn:** Im Protokoll steht „eine Sitzung" gegen „Thomas". Das ist die Trennung, die
  die Stunde am 24.10. gekostet hat. Und das Konto kann weniger als Thomas: was es nicht
  darf, kann eine Sitzung auch versehentlich nicht tun.
- **Preis:** Es trennt **nicht**, *welche* Sitzung. Und das Kennwort liegt weiter in einer
  Datei, die Skripte lesen — nur eben nicht mehr das von Thomas.
- **Kostet an Arbeit:** Thomas geht die Schritte aus `docs/zweiter-benutzer-schritte.md`
  **einmal**. Danach nichts.

### (c) Ein Konto je Sitzung

- **Gewinn:** Das Protokoll sagt, welcher Strang gehandelt hat. Das ist die vollständige
  Antwort auf die Frage.
- **Preis:** **Thomas klickt je neue Sitzung.** Jede neue Fassung der Vertrauensliste wird
  im Browser mit seinem Schlüssel unterschrieben; kein Agent darf das tippen. Bei der
  Arbeitsweise dieses Projekts — mehrere Stränge am Tag — ist das der teure Weg. Dazu:
  weil es keinen Einladungsweg gibt, kennt Thomas das Kennwort jeder Sitzung, bis sie es
  ändert; ein Eintrag unter ihrem Namen ist also kein Beweis, dass sie es war.
- **Kostet an Arbeit:** laufend, je Sitzung, und zwar von Thomas.

### (d) Sitzungen melden sich überhaupt nicht an

Sitzungen arbeiten nur über SSH als `root` und messen von außen; alles, was eine Anmeldung
braucht, tut Thomas.

- **Gewinn:** Das Protokoll von NGC bleibt sauber: darin steht nur, was Menschen getan
  haben.
- **Preis:** Was nur hinter einer Anmeldung zu sehen ist, bleibt **ungemessen** — und das
  ist genau die Hälfte dieses Auftrags. Zudem verschiebt es die Zurechnung nur: `root` per
  SSH ist mächtiger als jedes NGC-Konto, und dort führt nichts ein Benutzerfeld.
- **Kostet an Arbeit:** nichts, aber es kostet Messungen.

## Abgewogen, nicht entschieden

Nach der Rangfolge dieses Projekts — **Stabilität, dann Sicherheit, dann Leistung** —
entscheidet hier die **Sicherheit**, denn keiner der Wege wackelt: (b) ist der einzige, der
die Zurechnung verbessert, ohne laufend einen Menschen zu kosten, und er nimmt den
Sitzungen zugleich Rechte weg, die sie nicht brauchen. (c) ist strenger und scheitert an
der Arbeitsweise, nicht an der Technik — er wird tragbar, sobald der Einladungsweg aus
Punkt 73 gebaut ist, denn dann klickt Thomas nicht mehr mit.

**Die Entscheidung steht offen. Sie gehört Thomas.**
