# Thomas entscheidet

Jede Frage, die auf dich wartet — an einer Stelle, in der Reihenfolge, in der sie im Weg steht.
Oben, was eine Notlösung am Leben hält oder die Veröffentlichung aufhält. Unten, was warten kann.

Je Frage vier Zeilen: **Worum** · **Heute** · **Wahl** · **Mein Vorschlag**.
Nichts hier ist entschieden. Alles hier ist am Quelltext von `main` nachgesehen, Stand 23.10.2026.

---

## A · Die Veröffentlichung hängt daran

### 1. Der Updater ist nicht angeschlossen

- **Worum:** Auf keiner Anlage kann eine App eingebaut werden. Das ist der Zweck des Produkts.
- **Heute:** `Updater: nil` (`apps/server-controller/classes/node/run.go:661`). Jeder Einbau-Kanal endet mit „Interner Fehler". Der Kommentar daneben nennt die Naht: `update.Deps` nimmt die Vertrauensliste als **Wert**, der Controller tauscht seine Liste aber Fassung für Fassung darunter aus.
- **Wahl:** (a) `update` gibt nach und holt die Liste jedes Mal frisch — Änderung in einem Paket, kein Vertragsschnitt. (b) Der Controller gibt nach und friert die Liste je Kanal ein — so macht es die Einbau-Seite schon (`Installer`), also bewiesen machbar.
- **Mein Vorschlag:** **(b)**, weil die Gegenseite genau das schon tut und es trägt: ein Kanal wird gegen die Liste entschieden, mit der er auch eingebaut wird. (a) lässt einen Einbau gegen eine Liste laufen, die beim Entscheiden noch nicht galt.

### 2. Welcher Release-Schlüssel ist der echte — Stick oder Datei?

- **Worum:** Jede fremde Installation prüft NGC gegen einen Schlüssel. Es gibt zwei, und der eine kann nicht unterschreiben.
- **Heute:** In den Skripten steht `pN6Z6a9NR0aG6aEJIJ5EHg` — der **dateibasierte**. Sein geheimer Teil liegt als `/root/ngc-keys/release.key` auf der Messmaschine. Wer dort root ist, unterschreibt NGC für alle. Der YubiKey (`YWNJ9R-LilweZWb4Q4a1Vg`) hat nie etwas unterschrieben: die Karte kann die nackte Nachricht nicht unterschreiben, dafür bräuchte es Punkt 19 — und Punkt 19 ist nicht gebaut (`shared/sign/sign.go` unterschreibt weiter die Nachricht selbst).
- **Wahl:** (a) Die Datei bleibt der echte Schlüssel — nichts zu bauen, aber die Maschine bleibt für immer der Schwachpunkt. (b) Der Stick wird es — dann kommt Punkt 19 dazu, und das ist eine Änderung in `shared/`, also nur im v1.3-Schnitt.
- **Mein Vorschlag:** **(b)**, aber nicht vor dem v1.3-Schnitt, und (a) bis dahin ausdrücklich als Übergang hinschreiben. Ein Schlüssel, der eine Karte nie verlässt, ist der einzige, den ein übernommener Rechner nicht hergibt — und beim Release-Schlüssel ist genau das der ganze Zweck.

### 3. Die geheime Schlüsseldatei — auf einen Stick und beide löschen?

- **Worum:** Die geheime Hälfte des heute gültigen Schlüssels liegt als Datei auf einer Maschine. Sie soll dort nicht liegen.
- **Heute:** Die Doku nennt **eine** Stelle: `/root/ngc-keys/release.key`, `-rw-------`, in keinem Repo. Wo noch Kopien liegen, steht nirgends — der Schlüssel, den `tools/erstlauf.sh` auf der Prüfmaschine nennt, ist der **öffentliche**. **Das weißt nur du:** ob es eine zweite Kopie gibt, lässt sich aus dem Repo nicht beantworten.
- **Wahl:** (a) Auf einen Stick, beide Kopien löschen — dann ist der heutige Schlüssel aber nicht mehr zum Unterschreiben da, bis der Stick läuft (siehe 2). (b) Auf einen Stick kopieren, Dateien liegen lassen, bis der Stick unterschreibt. (c) Liegen lassen und ausdrücklich als Übergang benennen.
- **Mein Vorschlag:** **(b)**, und in einem Zug: Stick anlegen, Kopien zählen, dann erst löschen. (a) nimmt dir die Fähigkeit, ein Release zu unterschreiben, solange Punkt 19 fehlt — und dann steht alles.

### 4. Der Store-Schlüssel ist ein Platzhalter

- **Worum:** Es gibt keine Apps von NGC. Jede Installation muss ihren eigenen Laden nennen.
- **Heute:** `REPLACE-THIS-LINE-WITH-THE-NGC-STORE-KEY` steht unverändert in den Skripten. Der Riegel aus Punkt 57 bricht den Einbau sauber ab, statt den Platzhalter durchzulassen — das ist behoben, der fehlende Schlüssel nicht.
- **Wahl:** (a) Ein eigener Store-Schlüssel von NGC, mit einem Laden dahinter. (b) Kein NGC-Laden, dafür ein Satz in der Anleitung: jeder nennt seinen eigenen.
- **Mein Vorschlag:** **(b) für die erste Veröffentlichung.** Ein Laden ist ein Betrieb, kein Schlüssel — Inhalte, Pflege, Haftung. Solange der nicht steht, ist ein Schlüssel ohne Laden ein Versprechen, das keiner hält. (a) sobald es wirklich Apps gibt.

### 5. Die `ngc-*`-Skripte nach ulh-`main`

- **Worum:** Ohne diese Skripte kann niemand NGC installieren. Sie liegen fertig auf dem Zweig `ngc-skripte`.
- **Heute:** Auf `origin/main` von `ulh` liegt **kein** `ngc-*`-Skript. Ein Push dorthin erreicht per Auto-Update **sofort alle ulh-Nutzer** — darum nicht Sache eines Agenten. Dazu hängt die erste Wand aus Punkt 71 noch dort: `ngc-api.sh` schreibt `ReadWritePaths` ohne `/etc/ngc`, und damit bleibt eine Anlage auf der alten Vertrauensliste festgenagelt.
- **Wahl:** (a) Erst `/etc/ngc` in `ReadWritePaths` nachtragen, dann pushen. (b) Pushen und nachliefern.
- **Mein Vorschlag:** **(a).** Die eine fehlende Zeile ist der Unterschied zwischen „läuft" und „nimmt keine neue Vertrauensliste an" — und sie erreicht mit demselben Push sofort alle. Zwei Pushes, bei denen der erste kaputt ist, sind hier teurer als eine Zeile Warten.

### 6. Ein Weg, den Release-Schlüssel von außen zu setzen

- **Worum:** Niemand kann die Installationskette mit einem eigenen Schlüssel prüfen.
- **Heute:** Der öffentliche Schlüssel steht fest in vier Skripten. Wer prüfen will, verbiegt sie mit `sed` — und prüft damit nicht mehr die Skripte (Punkt 62, Befund 2).
- **Wahl:** (a) Eine Umgebungsvariable `NGC_RELEASE_KEY_FILE`. (b) Ein Unterbefehl `ngc-all.sh test-key <Datei>`. (c) Nichts, Prüfungen laufen weiter auf verbogenen Kopien.
- **Mein Vorschlag:** **(b).** Ein Unterbefehl ist sichtbar, steht in der Hilfe und lässt sich im Protokoll nachlesen; eine Umgebungsvariable wirkt still und kann aus einer Datei kommen, die niemand mehr liest. Bei einem Schlüsselriegel ist „man sieht, dass er umgangen wurde" der halbe Zweck.

### 7. P3 — Was ist eine `ngc-web`-Release-Datei?

- **Worum:** Ein Update der Oberfläche über den Controller kann heute nicht gehen.
- **Heute:** Gemessen, nicht angenommen: die Skripte bauen Namen im Wurzelverzeichnis des tar, der Controller packt nur Namen unter `ui/` aus, und `store.WalkUI` weist das Wurzel-tar am ersten Kopfblock ab. Punkt 56 sagt „P3 ist beantwortet" — das ist seit dem 18.10.2026 als falsch gemessen.
- **Wahl:** (a) Das Release-Werkzeug zieht nach und packt unter `ui/`. (b) Der Controller nimmt auch ein Wurzel-tar an.
- **Mein Vorschlag:** **(a).** Ein Werkzeug anzupassen ist billiger, als einen Vertrag zwei Formen annehmen zu lassen — zwei erlaubte Formen heißen für immer zwei Pfade, die beide geprüft werden müssen. Außerdem ist es **jetzt noch gratis**: das Packwerkzeug gibt es noch nicht.

### 8. P2 — Ein erster Einbau ohne Aufsicht geht gar nicht

- **Worum:** Eine Erstinstallation ohne zusehenden Menschen ist unmöglich, obwohl der eigene Kommentar das Gegenteil behauptet.
- **Heute:** `core.Unattended` (`shared/core/core.go:225`) sieht weder die Zeile der App noch den Katalog, kann für einen **ersten** Einbau also nie `true` sagen. Der Ersatz dafür ist schon zweimal von Hand gebaut (`install.go:353`, `rollout.go:253`).
- **Wahl:** (a) `Unattended` bekommt mit, dass es ein Erst-Einbau ist — eine Zeile im Vertrag, zwei Nachzieher, zwei Wächter umdrehen. (b) So lassen, und jede Stelle baut es weiter selbst nach.
- **Mein Vorschlag:** **(a).** Zweimal nachgebaut heißt: die dritte Stelle kommt von selbst, und sie wird anders aussehen als die ersten zwei. Eine Regel, die an fünf Stellen nachgebaut wird, ist keine Regel mehr.

### 9. Der Vertrauensanker nach einem Totalverlust

- **Worum:** Der dokumentierte Weg zurück nach einem Totalverlust endet in einer Wand.
- **Heute:** Die Einrichtung nimmt den Admin an, danach läuft nichts. Antwort B ist **gemessen keine Antwort**. Ein dritter Weg (Mutant D) ist gemessen und hält beides.
- **Wahl:** (a) Antwort A. (b) Der dritte Weg, Mutant D.
- **Mein Vorschlag:** **(b), Mutant D** — er ist der einzige der drei, der gemessen beides hält. A ist nicht bis zum Ende gemessen, B ist widerlegt.

### 10. PR 71 oder der andere Weg — nur ein Verweis

- **Worum:** Zwei Zweige schließen dasselbe Loch (eine SQLite-Datei, vier wartende Schreiber blockieren den ganzen Prozess) auf zwei Wegen. Nur einer soll landen.
- **Heute:** [PR 71](https://github.com/sorglos-it/nextgencloud/pull/71) teilt die Verbindungen (ein Schreibkanal, vier Leser); `origin/pool-schreibkanal` setzt den Schreibkanal in `sqlstore`. Der PR sagt es selbst: nur einer soll landen, und das ist eine Entscheidung, keine Messung.
- **Wahl:** Steht in PR 71, mit Zahlen zu beiden Seiten.
- **Mein Vorschlag:** **Hier nichts.** Dazu läuft eine eigene Sitzung an PR 71 — die Entscheidung gehört dorthin und nicht zweimal entschieden.

---

## B · Die v1.3-Reihe — je Punkt JA oder NEIN

Der fertige Änderungssatz, der Preis und was ohne ihn für immer fehlt, steht je Punkt in
[`vertraege-v1-3-vorschlag.md`](vertraege-v1-3-vorschlag.md). **P4 ist erledigt und gestrichen.**

### 11. P10 — Darf ein Vertrag von `shared/` überhaupt etwas dazubekommen? (das Tor)

- **Worum:** Ob eine Ergänzung an einem Vertrag eine Ergänzung sein darf oder für immer ein Bruch ist.
- **Heute:** Ein unbekannter Schließgrund gilt als kaputter Rahmen: `Close.Validate` wirft alles ab, was nicht in der Tabelle `messages` steht (`shared/wire/wire.go:528`). Jede künftige Ergänzung bricht damit jede alte Gegenseite.
- **Wahl:** (a) Zwei Zeilen: ein unbekannter Code wird angenommen und als unbekannt behandelt. (b) So lassen.
- **Mein Vorschlag:** **(a), und als erstes von allen.** Es ist das Tor: ohne P10 kosten P8 und P9 das Dreifache, und jede Ergänzung in zehn Jahren zahlt den Preis noch einmal.

### 12. P8 — Eigener Schließgrund für die Leerlaufgrenze · nur nach P10

- **Worum:** Ein Terminal, das wegen Leerlauf zugeht, sieht aus wie ein Absturz.
- **Heute:** Beide enden mit demselben Grund. Niemand kann sie unterscheiden.
- **Wahl:** JA nach P10 · NEIN.
- **Mein Vorschlag:** **JA**, direkt nach P10. Es ist der kleinere der beiden Nachzieher und berührt nichts außer `wire`.

### 13. P9 — Eigener Code `E_BUSY` für „besetzt" · nur nach P10

- **Worum:** Der Benutzer liest „Dienst nicht erreichbar", wenn die Datenbank nur besetzt ist.
- **Heute:** „Dienst weg", „Name löst nicht auf" und „Datenbank besetzt" sind für ihn derselbe Satz.
- **Wahl:** JA nach P10 · NEIN. Preis: zieht `shared/dbapi`, `memstore`, `sqlstore`, `dbtest` und die Oberfläche mit.
- **Mein Vorschlag:** **JA**, aber nach P8. Es ist dieselbe Klasse wie die Anmeldelüge aus Punkt 50 — ein Zustand wird als Ablehnung dargestellt, und der Mensch sucht lange an der falschen Stelle.

### 14. P1 — Die Adressprüfung ausführen

- **Worum:** Pfad und Anhang einer Download-Adresse prüft dauerhaft niemand.
- **Heute:** `validHTTPSURL` ist gebaut (`shared/store/catalog.go:208`) und wird vom Katalog benutzt — zugesagt ist es nirgends, und `ValidDownloadURL` gibt es nicht.
- **Wahl:** (a) Ein Satz im Vertrag und ein Name dafür. (b) Nichts.
- **Mein Vorschlag:** **(a)**, zusammen mit P11 und P12 in einem Zug — drei Sätze, kein Bruch, einzeln dreimal so teuer.

### 15. P11 — `ValidPath` sagt nichts über die Schreibweise

- **Worum:** Dieselbe Eingabe verhält sich auf Windows und Linux verschieden, und niemand sagt, ob das zugesagt ist.
- **Heute:** `store.ValidPath` (`shared/store/manifest.go:241`) ist POSIX-sicher, nicht Windows-sicher. Gemessen, nicht vermutet.
- **Wahl:** (a) Ein Satz: der Vertrag ist POSIX, Windows ist nicht zugesagt. (b) Windows mitabsichern.
- **Mein Vorschlag:** **(a).** NGC läuft auf Linux; Windows ist Entwicklerplatz. Ein Satz kostet nichts, eine Zusage kostet für immer.

### 16. P12 — Rechte im Katalog gegen das Manifest halten

- **Worum:** Nichts prüft, ob die Rechte im Katalogeintrag mit denen im Manifest übereinstimmen.
- **Heute:** Keine Lücke — die Katalogkopie wird nirgends als Grundlage benutzt. Wer sie je so benutzt, baut eine Wand vor eine offene Tür.
- **Wahl:** (a) Prüfen. (b) Ein Satz, der sagt: die Kopie ist nie die Grundlage.
- **Mein Vorschlag:** **(b).** Es gibt keine Lücke zu schließen, nur eine Annahme festzuhalten — und ein Satz hält sie billiger als eine Prüfung, die eine Gleichheit sichert, auf die sich niemand stützen soll.

### 17. P5 — Referenzfeld in `dbapi.App`

- **Worum:** Ein Abbild ohne Verweis bleibt für immer im Datenverzeichnis liegen, und die Größenrechnung zählt es als Benutzerdaten.
- **Heute:** `dbapi.App` hat kein Verweisfeld (`shared/dbapi`, `type App struct`). Niemand kann so ein Abbild entfernen.
- **Wahl:** JA · NEIN. Preis: der einzige Punkt mit Migration und bestehendem Nutzer.
- **Mein Vorschlag:** **JA, aber zuletzt** von allen v1.3-Punkten. Die Migration ist der Grund — sie will auf einem Stand laufen, der sonst nicht mehr wackelt.

### 18. P6 — Wie lange gilt ein Kopplungscode?

- **Worum:** Ein Code zum Aufnehmen eines Servers gilt heute unbegrenzt.
- **Heute:** Die Zahl ist nicht gesetzt. Die 24 Stunden im Code (`node/run.go:90`) sind etwas anderes — der Rhythmus der Oberflächenrunde.
- **Wahl:** (a) 24 Stunden. (b) Kürzer, z. B. eine Stunde. (c) Ohne Grenze.
- **Mein Vorschlag:** **(a), 24 Stunden** — und zusammen mit Frage 23 entschieden, das hängt aneinander. Eine Stunde reicht nicht für jemanden, der zwei Maschinen an zwei Tagen aufsetzt; ohne Grenze ist ein vergessener Code ein offener Zugang.

### 19. P7 — `ngc-api migrate` braucht eine ganze `api.json`

- **Worum:** Ein reiner Datenbankknoten kann seine Migrationen nicht starten.
- **Heute:** `migrate` liest die volle Konfiguration mit `load(path)` (`apps/server-api/main.go:176`) und scheitert, wenn Felder fehlen, die ein Datenbankknoten nicht hat.
- **Wahl:** (a) Eine eigene, kleine Lesefunktion nur für `migrate`. (b) So lassen, Datenbankknoten brauchen eine Attrappen-`api.json`.
- **Mein Vorschlag:** **(a).** Berührt nur `migrate`, hängt an nichts, und (b) heißt eine erfundene Datei auf jedem Datenbankknoten — die irgendwann jemand für echt nimmt.

### 20. P13 — Prozessdeckel je App

- **Worum:** Jede App teilt sich heute den einen Deckel von 512 Prozessen.
- **Heute:** Ein Feld im Manifest dafür gibt es nicht. Der Bedarf ist **nicht gemessen**.
- **Wahl:** JA für v1.3 · zurückstellen.
- **Mein Vorschlag:** **Zurückstellen.** Eine Zahl in einen Vertrag zu schreiben, deren Bedarf niemand gemessen hat, ist eine Zusage auf Verdacht. Der Punkt kommt von selbst zurück, sobald eine App an die 512 stößt.

### 21. P14 — Eine Oberfläche darf 16 MiB, ein Programm 4 GiB

- **Worum:** Ein großes Programm kann nie eine NGC-Oberfläche haben. Verhältnis 1:256, unbegründet.
- **Heute:** `MaxUIFile = 16 MiB`, `MaxUITotal = 64 MiB` (`shared/store/pack.go:22`) gegen `MaxPackage = 4 GiB`. Gemessen: ein portierter Rust-Bau war 44 MB, also 2,6-mal über der Dateigrenze.
- **Wahl:** (a) Grenzen anheben — dann hält die Shell mehr im Speicher und hasht mehr in einem Zug, bei acht offenen Apps achtmal. (b) Ausdrücklich nicht, mit einem Satz warum.
- **Mein Vorschlag:** **(b).** NGC-Oberflächen bleiben klein und handgeschrieben, und das ist vertretbar — es muss nur dastehen, damit niemand ein halbes Jahr in eine Portierung steckt, die an einer Zahl ohne Begründung endet.

### 22. P15 — Jede App erfindet ihre eigenen Nachrichtengrenzen

- **Worum:** Ein Strom sagt nicht, wo eine Nachricht aufhört. Das Terminal hat sich dafür einen eigenen Kopf aus drei Bytes erfunden.
- **Heute:** Der Kopf liegt in der App (`classes/terminal/terminal.go:76`), nicht im Vertrag. Jede weitere App erfindet ihn neu.
- **Wahl:** (a) Ein Rahmen mit Längenkopf in `shared/`, einmal, mit Vektoren; das Terminal zieht darauf um. (b) So lassen.
- **Mein Vorschlag:** **(a), aber nicht in v1.3.** Bei zwei Apps ist es nichts, bei zehn sind es zehn Stellen für einen fehlenden Längenvergleich — es ist aber kein Riegel und kann nach dem Schnitt kommen.

---

## C · Sicherheit und Oberfläche

### 23. Wird der Einmal-Code vor oder nach der MAC-Prüfung verbraucht?

- **Worum:** Wer die Adresse kennt, kann mit einer falschen MAC jeden wartenden Beitrittscode verbrennen.
- **Heute:** Verbraucht wird vorher. Gilt an **beiden** Stellen (`join.Accept`, `controllers.Pair`). Ungemessen: wie oft ein Code neu ausgegeben wird.
- **Wahl:** (a) Nachher verbrauchen — dann darf ein Angreifer beliebig oft rechnen. (b) Vorher lassen.
- **Mein Vorschlag:** **(a) mit Zähler:** nachher verbrauchen, aber nach einigen falschen MACs sperren. Beides allein ist ein offenes Tor in die eine oder die andere Richtung; zusammen gehört es mit Frage 18 entschieden.

### 24. Zweite Beitrittsanfrage für denselben Namen: überschreiben oder abweisen?

- **Worum:** Zwei Server mit demselben Namen verdrängen einander, und der Admin erfährt nichts.
- **Heute:** Die zweite überschreibt die erste, still. Unterscheiden ließen sie sich am öffentlichen Schlüssel.
- **Wahl:** (a) Abweisen. (b) Überschreiben und sagen, dass es passiert ist.
- **Mein Vorschlag:** **(a) abweisen.** Ein stiller Austausch ist der Fall, in dem ein fremder Server den Platz eines echten einnimmt — und ein Name, der schon wartet, ist für den Admin kein Fehler, den er nicht selbst lösen kann.

### 25. `noexec` auf `/data`

- **Worum:** Eine App darf heute Skripte ausführen, die sie selbst unter `/data` ablegt.
- **Heute:** Kein `noexec`. Punkt 51 nennt den Schalter, Punkt 36 nicht.
- **Wahl:** (a) Einschalten — **Verhaltensänderung** für jede App, die das heute tut. (b) Lassen.
- **Mein Vorschlag:** **(a), vor der Veröffentlichung.** Heute gibt es keine fremden Apps, die es brechen könnte — nach der Veröffentlichung gibt es sie, und dann ist derselbe Schalter eine Änderung, die Leute etwas kostet.

### 26. Digest statt Tag für das Terminal-Abbild — und wer hebt ihn an?

- **Worum:** Zwei Bauten desselben Stands sind heute zwei verschiedene Abbilder, und wer den fremden Tag beherrscht, beherrscht einen Container mit Systemrechten.
- **Heute:** Es hängt an einem Tag. Ein festgenagelter Digest, den niemand pflegt, ist nach einem halben Jahr aber schlechter als ein Tag.
- **Wahl:** (a) Digest festnageln und ein Mensch hebt ihn an. (b) Digest und eine Prüfung, die meldet, wenn er alt wird. (c) Tag behalten.
- **Mein Vorschlag:** **(b).** Die Frage ist nicht der Digest, sondern wer ihn anhebt — und solange das ein Mensch im Kalender ist, läuft es nach dem zweiten Monat nicht mehr. Eine Prüfung, die meckert, ist der Teil, der die Entscheidung trägt.

### 27. Terminal: Rohtasten oder Zeilenfeld?

- **Worum:** Das Terminal kann heute kein Passwort abfragen und kein `vi`.
- **Heute:** Zeilenfeld. Damit tippt ein Admin irgendwann sein `sudo`-Passwort in ein Feld, das es **anzeigt**.
- **Wahl:** (a) Rohtasten. (b) Zeilenfeld behalten.
- **Mein Vorschlag:** **(a) Rohtasten.** Das Format kann es schon, es ist keine Vertragsänderung — und (b) bedeutet, dass ein Passwort auf dem Schirm steht, früher oder später.

### 28. Bleibt `ngc-controller image` leer oder wird es gefüllt?

- **Worum:** Ob der Controller sein Terminal-Abbild selbst baut oder es mitgebracht bekommt.
- **Heute:** Leer. Die Basis ist mit einem Digest festgehalten (`build.go:98`), das Abbild selbst baut niemand.
- **Wahl:** (a) Füllen — der Controller baut es. (b) Leer lassen — es kommt als Datei aus dem Release.
- **Mein Vorschlag:** **(b) leer lassen.** Ein Bau auf der Zielmaschine braucht dort eine Baukette und Netz nach draußen; eine Datei aus dem Release ist dasselbe Abbild für alle und prüfbar gegen eine Prüfsumme. Hängt mit Frage 26 zusammen.

### 29. Ein Knopf „Jetzt nachsehen" im Laden (Punkt 66, zweite Hälfte)

- **Worum:** Ein Verwalter, der auf einen Katalog wartet, hat keine Handlung außer Warten.
- **Heute:** **Die erste Hälfte ist gebaut:** der erste Abruf läuft eine Minute nach dem Start (`FirstCatalog`, `apps/server-api/classes/server/server.go:481`), die Stundenrunde danach unverändert. Einen Knopf gibt es nicht.
- **Wahl:** (a) Knopf bauen — ein Weg mehr, den jemand von außen auslösen kann, braucht also dieselbe Ratenbegrenzung wie jeder andere Aufruf. (b) Ohne.
- **Mein Vorschlag:** **(a), aber ohne Eile.** Die Minute aus der ersten Hälfte nimmt dem Fall die Schärfe: der Knopf ist jetzt Bequemlichkeit, nicht mehr der einzige Ausweg.

---

## D · Zwei Wege nebeneinander, ein Messort, eine Nummer

### 30. Zwei Unterschreiber eines Releases

- **Worum:** Es gibt zwei Wege, ein Release zu unterschreiben. Nur einer sollte es sein.
- **Heute:** `ngc-store release` (der gemessene Weg, Punkt 57) und `tools/sign-release.sh` (mit einem eigenen Prüffall). Der Commit `cd69b30` sagt ausdrücklich, das sei nicht durch Löschen eines Falls zu klären.
- **Wahl:** (a) `ngc-store release` bleibt, das Skript geht. (b) Das Skript bleibt für die Karte, `ngc-store` für die Datei — dann sind es dauerhaft zwei.
- **Mein Vorschlag:** **(a)**, sobald Frage 2 entschieden ist. Vorher nicht: wird es der Stick, ist das Skript genau der Weg, der die Karte bedient. Die Antwort hängt an 2, nicht an sich selbst.

### 31. Welche der fünf Nummern ist die Fassung eines Releases? (D4)

- **Worum:** Ein Release trägt **eine** Fassungsnummer, aber es enthält **fünf** Programme.
- **Heute:** `store.Release` hat ein `Version`-Feld. Welche der fünf es ist, steht nirgends. **Die vierstellige Hausnummerierung ist dagegen kein Riegel mehr:** `store.ValidVersion` nimmt die vierte Stelle an (`shared/store/manifest.go:231`).
- **Wahl:** (a) Eine eigene Release-Nummer über den fünf Programmen. (b) Die Nummer eines ausgezeichneten Programms. (c) Alle fünf müssen gleich sein.
- **Mein Vorschlag:** **(a).** Ein Release ist ein eigenes Ding, das man benennt und zurückrollt; (b) macht ein Programm willkürlich zum Maß, (c) erzwingt Nummernsprünge bei Programmen, an denen sich nichts geändert hat.

### 32. Weiter auf `ngc.test`, oder auf einer Maschine mit echtem Namen?

- **Worum:** Wo gemessen wird. Alle heutigen Zahlen gelten für einen Browser, der ein selbst unterschriebenes Zertifikat für den Lauf annimmt.
- **Heute:** `ngc.test` mit eigenem Zertifikat. Was ein Nutzer mit `letsencrypt` erlebt, ist ungemessen.
- **Wahl:** (a) Weiter auf `ngc.test`. (b) Eine Maschine mit echtem Namen und `letsencrypt`.
- **Mein Vorschlag:** **(b), vor der Veröffentlichung einmal.** Nicht als neuer Messort für alles — einmal den ganzen Weg, weil Passschlüssel und Browser genau an Zertifikaten hängen und das der eine Unterschied ist, den `ngc.test` nicht zeigen kann.

---

## Nicht mehr zu entscheiden — inzwischen gebaut

Hier standen Fragen, die keine mehr sind. Am Quelltext von `main` nachgesehen, 23.10.2026:

| Frage | Heute |
|---|---|
| Bekommt die einrichtende Person Systemrechte automatisch? | **Entschieden und gebaut.** `rootPicker` kreuzt die unterschreibende Person vor, abwählbar, nur `rootUsers`, nur dieser Controller (`apps/web/public/assets/js/apps/users.js:301` und `:1236`). Eine Rolle wird nicht vorgekreuzt. Für `askJoinAPI` bleibt die Liste leer, und das ist richtig. |
| Bekommt sie das Recht „benutzen" für eine neue App automatisch? | **Ja, gebaut.** `adminApps` setzt einem Verwalter `<app>/use` und jede Aktion jeder eingerichteten App — **ohne Zeile in der Datenbank** (`apps/server-api/classes/rights/rights.go:56`). Damit steht eine neue App im Menü und darum auch in der Rechteliste. Eine **abgeschaltete** App lassen Menü und Rechte beide aus; das ist so gewollt. |
| Darf eine frische Anlage den Katalog beim Start holen? | **Ja, gebaut.** Erster Abruf eine Minute nach dem Start (`FirstCatalog`, `server.go:481`). Der Knopf „Jetzt nachsehen" fehlt weiter — siehe Frage 29. |
| „Der Katalog ist nicht echt" für „noch nicht geholt" | **Behoben.** Der leere Zustand hat seinen eigenen Satz (`apps/web/public/assets/js/apps/store.js:359`). |
| Vierstellige Nummern gegen `store.ValidVersion` | **Behoben.** Die vierte Stelle ist erlaubt (`shared/store/manifest.go:231`). |
| Verwirft ein unlesbares Anzeigefeld die ganze Antwort? | **Entschieden und gebaut** (PR #104): `version` und `arch` werden leer gemacht, harte Felder fallen weiter ganz, und der Satz nennt das Feld. |
| `0700` über dem Heimatverzeichnis der App | **Still behoben.** `main` legt `/var/lib/ngc/apps` mit `0o711` an (`apps/server-controller/classes/runtime/data.go:122`). |
| P4 — Balancer-Vorlage in `shared/ulh.md` | **Erledigt**, aus der v1.3-Reihe gestrichen. |
