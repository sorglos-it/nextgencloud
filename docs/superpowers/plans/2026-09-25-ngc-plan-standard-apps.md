# NGC Plan Standard-Apps: Die Grundausstattung des Desktops

> **Stand:** Entwurf vom 25.09.2026, noch nicht abgestimmt. Die offenen Fragen in Abschnitt 10
> muss Thomas entscheiden. Danach kommen die Entscheidungen ins Protokoll des Entwurfs. Jede
> Welle bekommt dann einen eigenen Umsetzungsplan mit Aufgaben im Stil von Plan 1.

**Ziel:** NGC soll nach der Installation wie ein richtiger Arbeitsplatz wirken, mit den Apps,
die man von DSM, Windows oder macOS kennt: Dateien, Einstellungen, Editor, Betrachter, kleine
Werkzeuge, Verwaltung, Büro und ein paar Spiele. Dieser Plan legt fest, welche Apps es gibt, was
jede in ihrer ersten Fassung kann, was sie von der Plattform braucht und in welcher Reihenfolge
wir sie bauen.

**Entwurf:** [`docs/superpowers/specs/2026-09-19-ngc-kern-design.md`](../specs/2026-09-19-ngc-kern-design.md),
Abschnitte 3 (Teilprojekt 7 „Apps“), 4.3 (Container), 4.4 (Apps) und 4.6 (Daten) ·
**Verträge:** [`shared/README.md`](../../../shared/README.md) ·
**Kern-Apps:** [Plan 1](2026-09-23-ngc-plan-1-web.md), Aufgabe 25

---

## 1. Was der Entwurf schon festlegt

Die Apps müssen sich in den bestehenden Entwurf einfügen. Daraus folgen diese Regeln:

- **Oberfläche im iframe:** Jede Oberfläche läuft abgeschottet mit CSP. Sie spricht nur über
  `ngc.call` und `ngc.open` mit ihren eigenen Diensten und kann nichts selbst speichern (4.4).
- **Dienst im Container:** Ein Dienst läuft in einem eigenen Podman-Container unter dem
  Systembenutzer seiner App, ohne Netz und nur mit seinem verschlüsselten Datenordner.
  Netz, root und andere Sonderrechte gibt es nur über das Manifest und die Freigabe des Admins
  (4.3, 4.6).
- **Rechte aus Aktionen:** Jede App meldet ihre Aktionen im Manifest an. Daraus werden die Rechte,
  die ein Admin an Rollen vergibt. Dazu kommt das Grundrecht „benutzen“ (4.4).
- **Schon geplant:** App-Store, Benutzer & Rechte (mit dem Protokoll der API), Rechner und
  Terminal baut Plan 1. Sie kommen in diesem Plan nur als Bezug vor.

## 2. Drei Arten von Apps

| Art | Aufbau | Freigabe beim Aktivieren | Beispiele |
|---|---|---|---|
| **O: nur Oberfläche** | `manifest.json` und `ui/` | keine | Uhr, Paint, Spiele |
| **D: mit Dienst** | dazu `services/` im Container, Daten im eigenen Ordner | keine, solange kein Netz nötig ist | Notizen, Kalender, Kontakte |
| **S: mit Sonderrechten** | Dienst mit Zugriff auf den Server (Dateien, Laufwerke, Systemdienste, Netz) | ausdrücklich vom Admin, meist kritische Bestätigungsstufe | Dateimanager, Ressourcenmonitor, Sicherung |

Apps der Art S sind die heikelsten, denn sie durchbrechen die Abschottung bewusst. Für sie gilt:
Jedes Sonderrecht steht einzeln im Manifest. Der Dienst bekommt nur die Pfade und Befehle, die er
braucht, nie „root allgemein“, wenn es sich vermeiden lässt. Jede Aktion, die etwas ändert, ist
eine eigene Aktion mit eigenem Recht, zum Beispiel „Laufwerk formatieren“.

## 3. Was der Plattform noch fehlt

Viele Apps brauchen dieselben Bausteine, die es im Kern noch nicht gibt. Wir bauen sie einmal in
`ngc.js`, API und Controller und nicht in jeder App einzeln. Das sind Vertragsänderungen in
`shared/`, also Sache des Koordinators. Sie kommen vor die Apps, die sie brauchen.

| Nr. | Baustein | Wozu | Wer braucht ihn |
|---|---|---|---|
| P1 | **App-Speicher**: kleiner Schlüssel-Wert-Speicher je App und Benutzer (Vorschlag 64 KiB), verwaltet von der API | Einstellungen, Fenstergröße, Spielstände, zuletzt geöffnete Dateien, ohne dass jede App einen Dienst braucht | fast alle, vor allem Art O |
| P2 | **Gemeinsamer Dateibereich**: „Meine Dateien“ je Benutzer und freigegebene Ordner für Gruppen, verwaltet von einem Dienst mit Sonderrechten | Apps sehen dieselben Dateien, statt jede in ihrem eigenen Datenordner | Dateimanager, alle Editoren und Betrachter, Fotos, Musik, Sicherung |
| P3 | **Öffnen mit**: Apps melden im Manifest Dateitypen an; `ngc.open` übergibt der Ziel-App einen Verweis auf eine Datei, nur mit dem Recht für genau diese Datei | Doppelklick im Dateimanager öffnet den passenden Betrachter | Dateimanager und alle Betrachter und Editoren |
| P4 | **Hochladen und Herunterladen**: die Shell nimmt Dateien per Auswahl oder Ziehen an und gibt Dateien zum Speichern heraus | Der iframe darf beides nicht selbst | Dateimanager, Paint, Editoren, Sicherung |
| P5 | **Benachrichtigungen**: `ngc.notify` für die Oberfläche; Dienste können über die API eine Mitteilung an einen Benutzer schicken | Hinweise wie „Sicherung fertig“ oder „Termin in 10 Minuten“ | Kalender, Uhr, Sicherung, Aufgabenplaner, Chat |
| P6 | **Zwischenablage**: Kopieren aus dem iframe heraus über die Shell | In abgeschotteten iframes ist die Zwischenablage eingeschränkt | Editoren, Rechner, Zeichentabelle |
| P7 | **Sonderrechte-Katalog**: feste Liste der Sonderrechte, die ein Manifest verlangen darf (z. B. `host.read-stats`, `host.journal`, `files.users`, `disks.manage`, `net.outbound`) | Der Admin sieht beim Aktivieren verständlich, was eine App darf; es gibt kein freies „alles“ | alle Apps der Art S |

**Empfehlung:** Die Bausteine P1, P3, P4 und P7 kommen zuerst, weil ohne sie keine der Apps der
ersten Welle sauber funktioniert. Den gemeinsamen Dateibereich (P2) liefert die App Dateimanager
mit, er wird aber im Vertrag beschrieben, damit andere Apps ihn nutzen können.

## 4. Die Apps im Überblick

Paketnamen sind Vorschläge nach dem Muster `org.sorglos.<name>`. Die Spalte „Welle“ verweist auf
Abschnitt 8.

### 4.1 System

| App | Art | Erste Fassung | Braucht | Welle |
|---|---|---|---|---|
| **App-Store** | Kern | siehe Plan 1 | – | 0 |
| **Benutzer & Rechte** | Kern | siehe Plan 1, mit dem Protokoll der API | – | 0 |
| **Dateimanager** | S (`files.users`) | Ordner durchsuchen, Listen- und Kachelansicht, hochladen, herunterladen, umbenennen, verschieben, kopieren, löschen in den Papierkorb, Ordner an Benutzer und Gruppen freigeben, Suche nach Namen, Öffnen mit | P2, P3, P4 | 2 |
| **Einstellungen** | S (einzelne Rechte je Bereich) | Eigenes Konto (Passwort, TOTP, Passkeys, Sitzungen), Aussehen (hell, dunkel, Akzentfarbe, Hintergrund), Sprache und Zeitzone, Updates (die Anzeige aus Plan 1 zieht hierher), Servername und Zertifikat nur anzeigen | P1 | 2 |
| **Ressourcenmonitor** | S (`host.read-stats`) | CPU, RAM, Datenträger und Netz je Server als Verlauf der letzten Stunde; Liste der Container mit Verbrauch; nur lesen | P7 | 2 |
| **Systemprotokoll** | S (`host.journal`) | Das Journal der Server lesen, filtern nach Server, Dienst, Stufe und Zeit, als Text herunterladen. Das Protokoll der API bleibt in Benutzer & Rechte | P4, P7 | 3 |
| **Dienste** | S (`host.services`) | Container aller Apps mit Zustand, Neustart, Logausgabe eines Dienstes. Kein Paketzentrum, das ist der App-Store | P7 | 3 |
| **Speichermanager** | S (`disks.read`, `disks.manage`) | Laufwerke, Partitionen, Belegung und SMART-Werte anzeigen. Formatieren und Einbinden erst in der zweiten Fassung und nur als Root-Aufgabe mit Unterschrift | P7 | 4 |
| **Sicherung** | S (`files.users`, `backup.targets`) | Sicherungen des Dateibereichs und der App-Daten auf ein Laufwerk oder einen Netzordner planen, verschlüsselt mit AES-256-GCM, Versionen, einzelne Dateien und ganze Stände zurückspielen | P2, P5, P7 | 4 |
| **Hilfe** | O | Handbuch für Benutzer und Admins, Suche, Einführung beim ersten Start | – | 2 |

Benachrichtigungen sind keine eigene App. Das Mitteilungszentrum gehört in die Shell (P5).

### 4.2 Zubehör

| App | Art | Erste Fassung | Braucht | Welle |
|---|---|---|---|---|
| **Rechner** | Kern | siehe Plan 1; später wissenschaftlich und Programmierer (Hex, Bin, Bitoperationen) | – | 0 |
| **Uhr** | O | Weltzeit, Stoppuhr, Timer und Wecker, solange das Fenster offen ist | P1, später P5 | 2 |
| **Zeichentabelle** | O | Unicode-Zeichen und Emoji nach Name und Block suchen und kopieren, begrenzt auf das, was `wire.ValidText` erlaubt | P6 | 2 |
| **Einheitenumrechner** | O | Länge, Fläche, Gewicht, Temperatur, Datenmengen (auch KB/KiB), Zeit. Ohne Währung, denn die bräuchte Netz | – | 2 |
| **Paint** | O | Stift, Pinsel, Radierer, Linie, Rechteck, Ellipse, Füllen, Text, Farbwähler, Rückgängig, Größe ändern; öffnen und speichern als PNG | P3, P4 | 2 |
| **Texteditor** | O | Mehrere Tabs, Suchen und Ersetzen, Zeilennummern, Syntaxhervorhebung für die gängigen Konfigurationsformate, Zeichensatz UTF-8; öffnet und speichert Dateien aus dem Dateibereich | P2, P3, P6 | 2 |
| **Notizen** | D | Notizen mit Markdown, Ordner, Suche, an Benutzer freigeben | P6 | 3 |
| **Aufgaben** | D | Listen, Fälligkeit, Erinnerung, an Gruppen freigeben | P5 | 3 |
| **Kalender** | D | Tag, Woche, Monat, Termine mit Wiederholung, mehrere Kalender, freigeben. CalDAV für Handys erst in der zweiten Fassung, denn dafür braucht der Dienst einen eigenen Zugang von außen | P5 | 3 |
| **Kontakte** | D | Adressbuch, Gruppen, Import und Export als vCard. CardDAV wie beim Kalender später | P4 | 3 |

**Zurückgestellt:** *Bildschirmfoto* und *Zwischenablage-Verlauf*. Die Shell kann den Inhalt
fremder iframes nicht abfotografieren, und eine Bildschirmaufnahme geht nur mit Nachfrage des
Browsers. Ein Verlauf der Zwischenablage würde zwischen abgeschotteten Apps mitlesen. Beides
widerspricht der Abschottung und lohnt den Aufwand nicht.

### 4.3 Medien und Anzeige

| App | Art | Erste Fassung | Braucht | Welle |
|---|---|---|---|---|
| **Bildbetrachter** | O | PNG, JPEG, GIF, WebP, SVG (nur als Bild, nie eingebettet); Zoom, Drehen, Vollbild, Blättern im Ordner, EXIF-Daten anzeigen | P2, P3 | 2 |
| **Medienplayer** | O | Audio und Video in den Formaten, die der Browser selbst abspielt; Wiedergabeliste aus einem Ordner; Untertitel (WebVTT) | P2, P3 | 2 |
| **PDF-Betrachter** | O | Seiten anzeigen, suchen, zoomen, drucken. Der eingebaute PDF-Betrachter des Browsers läuft in abgeschotteten iframes nicht; wir brauchen pdf.js (Apache-2.0, vereinbar mit der AGPL) | P2, P3, neues Modul | 3 |
| **Archiv** | D | ZIP und TAR ansehen und entpacken, ZIP erstellen. Entpackt wird im Dienst in Go, nach denselben Regeln wie `store.Walk`: keine absoluten Pfade, kein `..`, keine Links, feste Größen und Anzahl | P2, P3 | 3 |
| **Fotos** | D mit `files.users` | Galerie über den Dateibereich, Zeitleiste nach Aufnahmedatum, Alben, Vorschaubilder im Dienst erzeugt | P2 | 4 |
| **Musik** | D mit `files.users` | Bibliothek nach Interpret und Album aus den Tags, Wiedergabelisten | P2 | 4 |

### 4.4 Verwaltung

| App | Art | Erste Fassung | Braucht | Welle |
|---|---|---|---|---|
| **Terminal** | Kern | siehe Plan 1 | – | 0 |
| **Aufgabenplaner** | S (`host.tasks`) | Zeitgesteuerte Aufgaben: Befehl mit Argumentliste (nie `sh -c`), Zeitplan, letzte Läufe mit Ausgabe, Benachrichtigung bei Fehlern. Anlegen und Ändern ist eine Root-Aufgabe | P5, P7 | 3 |
| **Netzwerkwerkzeuge** | S (`net.outbound`) | Ping, DNS-Abfrage, Traceroute, Portprüfung. Ziele nur nach `trust.ValidHost` und `trust.ValidHostPort`, Anzahl und Takt begrenzt, damit die App kein Werkzeug für Angriffe wird | P7 | 3 |
| **Passwort-Tresor** | D | Zugangsdaten für Benutzer und Gruppen. Ver- und entschlüsselt wird im Browser mit Web Crypto (AES-256-GCM), der Dienst sieht nur Chiffretext. Schlüssel je Benutzer, für Gruppen mit dem öffentlichen Schlüssel jedes Mitglieds verschlüsselt | P6 | 4 |
| **Zertifikate** | – | gehört zu Teilprojekt 2 (NGC-Zertifizierungsstelle) und bekommt dort seinen Entwurf | – | eigenes Teilprojekt |

### 4.5 Büro und Kommunikation

| App | Art | Erste Fassung | Braucht | Welle |
|---|---|---|---|---|
| **Chat** | D | Nachrichten zwischen Benutzern der Installation, Einzel- und Gruppengespräche, Dateien über den Dateibereich teilen | P2, P5 | 4 |
| **Mail** | S (`net.outbound` zu den eingetragenen Servern) | IMAP und SMTP zu einem bestehenden Postfach, so wie im Entwurf als Beispiel beschrieben: Oberfläche im Browser, Versand über den Dienst | P5 | 5 |
| **Textverarbeitung** | O | Dokumente mit Überschriften, Listen, Tabellen und Bildern, gespeichert als HTML-Teilmenge oder Markdown, Export als PDF über den Druckdialog. Kein .docx in der ersten Fassung | P2, P3, P4 | 5 |
| **Tabellenkalkulation** | O | Tabellen mit Formeln (eigener Parser, kein `eval`), CSV öffnen und speichern. Kein .xlsx in der ersten Fassung | P2, P3, P4 | 5 |

Mail, Textverarbeitung und Tabellenkalkulation sind jeweils groß genug für einen eigenen
Entwurf. Hier stehen sie nur, damit die Plattform-Bausteine sie nicht vergessen.

### 4.6 Spiele

Die Spiele sind für den Charme da, wie Solitär und Minesweeper bei Windows. Außerdem sind sie
gute erste Apps für das Paketformat, weil sie nur eine Oberfläche haben.

| App | Art | Erste Fassung | Braucht | Welle |
|---|---|---|---|---|
| **Minensucher** | O | Drei Stufen (9×9/10, 16×16/40, 30×16/99) und eine eigene Größe; erster Klick nie auf eine Mine; Fahnen; Doppelklick deckt Nachbarn auf; Zeit und Minenzähler | P1 für Bestzeiten | 2 |
| **2048** | O | 4×4-Feld; Pfeiltasten, WASD und Wischen; Punkte; einen Zug rückgängig; Weiterspielen nach 2048 | P1 | 2 |
| **Solitär** | O | Klondike mit 1 oder 3 Karten ziehen; Ziehen und Ablegen mit Maus und Finger; Doppelklick legt ab; Rückgängig; automatisches Ablegen am Ende; Karten selbst als SVG gezeichnet | P1 | 3 |
| **Sudoku** | O | Vier Stufen; jedes Rätsel hat genau eine Lösung; Notizen; Fehleranzeige schaltbar; Hinweis | P1 | 3 |
| **Schach** | O | Zwei Spieler an einem Gerät; alle Regeln einschließlich Rochade, en passant, Umwandlung, Patt, 50 Züge, dreifacher Wiederholung; Zugliste; Brett drehen | P1 | 3 |

Die Namen sind bewusst deutsch und allgemein, denn „Minesweeper“ und „Tetris“ sind Markennamen.
„2048“ ist ein freies Spiel (MIT). Wir bauen es neu und übernehmen keinen Code.

Später kommen Schach gegen den Computer (eigene Alpha-Beta-Suche in einem Web Worker, keine
fremde Engine), Mahjong-Solitär, Snake, Vier gewinnt, Dame und Spider-Solitär. Mehrspieler
zwischen Benutzern bräuchte einen Dienst und bekommt einen eigenen Plan.

## 5. Gemeinsame Regeln für alle Apps

- **Kein Framework, kein Build-Schritt:** HTML, CSS und ES-Module wie im Webinterface. Kein
  `eval`, kein `new Function`, kein `innerHTML`. Texte kommen über `textContent`, Grafik über
  Canvas oder SVG-Elemente aus `createElementNS`.
- **Fremde Inhalte sind Daten:** SVG, Markdown, HTML-Dokumente, E-Mails und Dateinamen werden nie
  als HTML eingefügt. SVG zeigt der Bildbetrachter nur über `<img>` an, wo Skripte nicht laufen.
  Markdown wird von einem eigenen Parser in DOM-Knoten übersetzt, der nur eine feste Liste von
  Elementen kennt, und bekommt einen Fuzz-Test.
- **Dienste in Go:** Dienste folgen denselben Regeln wie API und Controller: SQL mit Platzhaltern,
  jede Eingabe mit Typ, Länge und erlaubten Werten prüfen, strikte JSON-Prüfung, Obergrenzen für
  alles, Programme ohne Shell starten.
- **Jede Fenstergröße:** Jede App funktioniert von der Handybreite bis zum Vollbild und lässt sich
  mit Maus, Tastatur und Finger bedienen.
- **Hell und dunkel:** Die Farben kommen aus `ngc.js`. Wichtige Unterschiede zeigen wir nicht nur
  über Farbe, sondern auch über Form oder Text.
- **Barrierefreiheit:** Bedienelemente haben Rollen und Namen, damit Screenreader und die
  Playwright-Tests sie über `getByRole` finden.
- **Logik getrennt von der Anzeige:** Wo es Logik gibt (Formeln, Spielregeln, Markdown,
  Zeitpläne), liegt sie in einem Modul ohne DOM und wird ohne Server getestet.
- **Sprache:** Oberfläche und Hilfe auf Deutsch, Code und Kommentare auf Englisch.

## 6. Bauteile, die mehrere Apps teilen

- **Dateiauswahl und Speichern-Dialog** (von der Shell gestellt, damit keine App den Dateibereich
  selbst durchsuchen muss)
- **Liste und Kacheln mit Mehrfachauswahl** (Dateimanager, Fotos, Musik, Archiv)
- **Verlauf als Liniendiagramm** (Ressourcenmonitor, Dienste)
- **Zufall mit festem Startwert** über `crypto.getRandomValues` (Spiele, Tests)
- **Menü „Neu / Öffnen / Speichern / Rückgängig / Hilfe“** und Dialog „Ungespeicherte Änderungen“

Weil jede App ihr eigenes Paket ist und der iframe nichts nachladen darf, werden gemeinsame Module
beim Packen in jedes Paket kopiert und nicht zur Laufzeit geteilt.

## 7. Verteilung

Alle Standard-Apps außer den Kern-Apps kommen über den globalen Store, gratis und signiert. So
bleiben sie abschaltbar und bekommen ihre Updates auf demselben Weg wie jede andere App. Die
Ersteinrichtung bietet drei Pakete zur Auswahl:

- **Grundausstattung:** Dateimanager, Einstellungen, Texteditor, Bildbetrachter, Medienplayer,
  PDF-Betrachter, Hilfe, Uhr, Ressourcenmonitor
- **Büro:** Notizen, Aufgaben, Kalender, Kontakte, Chat
- **Spiele:** alle Spiele

Für Netze ohne Internet liegen die Pakete der Grundausstattung als Datei bei der Installation über
ulh, damit sie sich als eigene Apps mit Bestätigung einspielen lassen.

## 8. Reihenfolge

| Welle | Inhalt | Warum in dieser Reihenfolge |
|---|---|---|
| 0 | App-Store, Benutzer & Rechte, Rechner, Terminal | läuft in Plan 1 |
| 1 | Plattform-Bausteine P1, P3, P4, P7 und P2 als Vertrag | ohne sie kann keine App der Welle 2 Dateien öffnen oder etwas speichern |
| 2 | Dateimanager, Einstellungen, Ressourcenmonitor, Hilfe, Texteditor, Bildbetrachter, Medienplayer, Uhr, Zeichentabelle, Einheitenumrechner, Paint, Minensucher, 2048 | Die Grundausstattung; dazu kleine Apps der Art O, die den Weg vom Packen bis zum Fenster früh prüfen |
| 3 | P5 und P6, Notizen, Aufgaben, Kalender, Kontakte, PDF-Betrachter, Archiv, Systemprotokoll, Dienste, Aufgabenplaner, Netzwerkwerkzeuge, Solitär, Sudoku, Schach | Apps mit eigenem Dienst und die ersten weiteren Sonderrechte |
| 4 | Speichermanager, Sicherung, Fotos, Musik, Passwort-Tresor, Chat | größere Apps, die auf Dateibereich und Benachrichtigungen aufbauen |
| 5 | Mail, Textverarbeitung, Tabellenkalkulation | je ein eigener Entwurf |

Innerhalb einer Welle ist jede App ein eigener kleiner Pull Request. Der erste Pull Request der
Welle 2 ist der Minensucher: Er ist die kleinste App und prüft den ganzen Weg ohne Dienst.

## 9. Tests

- **Jede App:** Ein Playwright-Test startet die App aus dem Startmenü, bedient die Hauptfunktion und
  prüft hell, dunkel, ein kleines und ein großes Fenster.
- **Logik ohne Server:** Die DOM-freien Module werden wie die Verträge in `apps/web/tests/` im
  Browser-Kontext getestet, ohne dass eine API läuft.
- **Parser:** Jeder Parser bekommt einen Fuzz-Test: Markdown, Formeln der Tabellenkalkulation,
  vCard, iCalendar, Zeitpläne des Aufgabenplaners, ZIP und TAR im Archiv-Dienst, EXIF und
  Musik-Tags.
- **Kryptografie:** Der Passwort-Tresor und die Sicherung werden gegen Testvektoren in
  `shared/testdata/` geprüft.
- **Sonderrechte:** Für jede App der Art S prüft ein Test, dass der Dienst ohne das jeweilige Recht
  abgelehnt wird und mit dem Recht nur an die erlaubten Pfade kommt. Beim Dateimanager gehört
  dazu: kein Zugriff auf fremde Ordner, keine Links aus dem Dateibereich heraus, kein `..`.
- **Spiele:** Minensucher (erster Klick nie auf eine Mine), 2048 (jede Zahl verschmilzt pro Zug
  höchstens einmal), Solitär (jedes Mischen enthält alle 52 Karten genau einmal), Sudoku (jedes
  Rätsel hat genau eine Lösung), Schach (Zuggenerator gegen die bekannten Perft-Werte bis
  Tiefe 4).
- **Sicherheit im Code:** Ein Test in `tools/check.sh` prüft, dass in keiner App-Oberfläche
  `innerHTML`, `outerHTML`, `insertAdjacentHTML`, `eval`, `new Function`, `fetch` oder `WebSocket`
  vorkommt.

## 10. Offene Fragen an Thomas

1. **Wo liegt der Quellcode der Store-Apps, und wer baut sie?** Sie gehören nicht nach
   `apps/web/`. Vorschlag: ein eigenes Repo für die Apps des globalen Stores mit einer eigenen
   Sitzung „Apps“; oder ein Ordner hier nach dem Projektstruktur-Standard.
2. **Plattform-Bausteine P1 bis P7:** Sollen alle kommen, und in welcher Reihenfolge? P1 und P2
   berühren Web, API, DB und Controller gleichzeitig.
3. **Gemeinsamer Dateibereich (P2):** Wo liegen die Dateien der Benutzer? Vorschlag: ein eigener
   Ordner auf dem Server der Dienste, verschlüsselt wie die App-Daten, verwaltet vom Dienst des
   Dateimanagers. Oder gehört das schon zum Teilprojekt 7 „Fileserver“ (SMB/NFS)?
4. **Sonderrechte (P7):** Ist ein fester Katalog von Rechten wie `host.read-stats` richtig, statt
   „Netz / root / sonstiges“ wie im Entwurf bisher?
5. **Grundausstattung ab Werk:** Über den Store mit Auswahl in der Ersteinrichtung (Abschnitt 7),
   oder fest im Kern wie App-Store und Benutzer & Rechte?
6. **Neue Fremdmodule:** pdf.js für den PDF-Betrachter. Weitere wird es für Vorschaubilder
   (HEIC, RAW) und Musik-Tags geben, falls wir sie nicht selbst schreiben wollen. Soll ich sie
   einzeln anfragen, wenn die Welle kommt?
7. **Einstellungen:** Soll die Einstellung der Updates aus Plan 1 in die App Einstellungen
   umziehen, oder bleibt sie im App-Store?
