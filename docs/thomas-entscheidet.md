# Thomas entscheidet — entschieden

Thomas hat das Blatt am 24.10.2026 zurückgegeben, wörtlich:
„keine ahnung entscheide selbständig nach den grundsätzen stabilität vor sicherheit vor performance".

Also ist hier entschieden. Die Rangfolge wird in dieser Reihenfolge geprüft:
**Stabilität**, dann **Sicherheit**, dann **Leistung**.
Wo die erste Stufe schon entscheidet, wird die zweite nicht mehr bemüht. Das steht je Frage dabei.

Je Frage: **Worum** · **Heute** · **Entschieden** · **Warum** · **Zu bauen**.
„Zu bauen" ist eine Zeile Auftrag. **Hier ist nichts gebaut, nur entschieden.**

Was nicht entschieden ist:

- **Bei Thomas bleiben 2, 3 und 4.** Schlüssel, und was öffentlich wird. Das gehört keinem Agenten.
- **Offen bleibt 30.** Es hängt an Frage 2, nicht an der Rangfolge.
- **20 ist nachgetragen.** Die fehlende Messung wurde am 24.10.2026 gemacht (M1). Sie hat die Annahme hinter der Frage widerlegt, nicht die Frage beantwortet. Die Zahlen stehen in `docs/messungen-m1-m3.md`.
- **Bei 5** ist die Reihenfolge entschieden, der Push selbst bleibt bei Thomas.

Alles unter **Heute** ist am Quelltext von `main` nachgesehen, Stand 24.10.2026.
Eine Ausnahme: bei Frage 20 ist es **gemessen**, und die Messung hat den Quelltext-Schluss
widerlegt. Das steht dort.

---

## A · Die Veröffentlichung hängt daran

### 1. Der Updater ist nicht angeschlossen

- **Worum:** Auf keiner Anlage kann eine App eingebaut werden. Das ist der Zweck des Produkts.
- **Heute:** `Updater: nil` (`apps/server-controller/classes/node/run.go:661`). Jeder Einbau-Kanal endet mit „Interner Fehler". `update.Deps` nimmt die Vertrauensliste als **Wert**, der Controller tauscht seine Liste aber Fassung für Fassung darunter aus.
- **Entschieden (24.10.2026):** **(b)** — der Controller friert die Liste je Kanal ein, so wie es die Einbau-Seite (`Installer`) schon tut.
- **Warum:**
  - **STABILITÄT entscheidet.** (b) läuft auf der Einbau-Seite bereits und trägt dort. Ein Kanal wird dann gegen dieselbe Liste entschieden, mit der er auch einbaut. Bei (a) kann ein Einbau gegen eine Liste laufen, die beim Entscheiden noch nicht galt — ein Einbau, dessen Ergebnis von der Laufzeit abhängt. Abwägung; gemessen ist nur, dass `Installer` es so hält.
  - Sicherheit und Leistung werden nicht gebraucht.
- **Zu bauen:** `Updater` in `run.go:661` verdrahten, Vertrauensliste je Kanal einfrieren wie im `Installer`.

### 2. Welcher Release-Schlüssel ist der echte — Stick oder Datei?

> **Bleibt bei Thomas.** Ein Release-Schlüssel ist der Schlüssel, mit dem NGC für fremde Rechner spricht. Das entscheidet kein Agent.

- **Worum:** Jede fremde Installation prüft NGC gegen einen Schlüssel. Es gibt zwei, und der eine kann nicht unterschreiben.
- **Heute:** In den Skripten steht `pN6Z6a9NR0aG6aEJIJ5EHg` — der **dateibasierte**. Sein geheimer Teil liegt als `/root/ngc-keys/release.key` auf der Messmaschine. Wer dort root ist, unterschreibt NGC für alle. Der YubiKey (`YWNJ9R-LilweZWb4Q4a1Vg`) hat nie etwas unterschrieben: die Karte kann die nackte Nachricht nicht unterschreiben, dafür bräuchte es Punkt 19 — und der ist nicht gebaut (`shared/sign/sign.go` unterschreibt weiter die Nachricht selbst).
- **Wahl:** (a) Die Datei bleibt der echte Schlüssel — nichts zu bauen, aber die Maschine bleibt für immer der Schwachpunkt. (b) Der Stick wird es — dann kommt Punkt 19 dazu, eine Änderung in `shared/`, also nur im v1.3-Schnitt.
- **Vorschlag bleibt:** **(b)**, aber nicht vor dem v1.3-Schnitt, und (a) bis dahin ausdrücklich als Übergang hinschreiben. Ein Schlüssel, der eine Karte nie verlässt, ist der einzige, den ein übernommener Rechner nicht hergibt.

### 3. Die geheime Schlüsseldatei — auf einen Stick und beide löschen?

> **Bleibt bei Thomas.** Wo die geheime Hälfte liegt und wie viele Kopien es gibt, weiß nur er. Das Repo kann es nicht beantworten.

- **Worum:** Die geheime Hälfte des heute gültigen Schlüssels liegt als Datei auf einer Maschine. Sie soll dort nicht liegen.
- **Heute:** Die Doku nennt **eine** Stelle: `/root/ngc-keys/release.key`, `-rw-------`, in keinem Repo. Wo noch Kopien liegen, steht nirgends — der Schlüssel, den `tools/erstlauf.sh` nennt, ist der **öffentliche**.
- **Wahl:** (a) Auf einen Stick, beide Kopien löschen — dann ist der heutige Schlüssel aber nicht mehr zum Unterschreiben da, bis der Stick läuft (siehe 2). (b) Auf einen Stick kopieren, Dateien liegen lassen, bis der Stick unterschreibt. (c) Liegen lassen und ausdrücklich als Übergang benennen.
- **Vorschlag bleibt:** **(b)**, und in einem Zug: Stick anlegen, Kopien zählen, dann erst löschen. (a) nimmt die Fähigkeit, ein Release zu unterschreiben, solange Punkt 19 fehlt.

### 4. Der Store-Schlüssel ist ein Platzhalter

> **Bleibt bei Thomas.** Ein Laden ist Betrieb: Inhalte, Pflege, Haftung, Geld. Und er wird öffentlich.

- **Worum:** Es gibt keine Apps von NGC. Jede Installation muss ihren eigenen Laden nennen.
- **Heute:** `REPLACE-THIS-LINE-WITH-THE-NGC-STORE-KEY` steht unverändert in den Skripten. Der Riegel aus Punkt 57 bricht den Einbau sauber ab, statt den Platzhalter durchzulassen — das ist behoben, der fehlende Schlüssel nicht.
- **Wahl:** (a) Ein eigener Store-Schlüssel von NGC, mit einem Laden dahinter. (b) Kein NGC-Laden, dafür ein Satz in der Anleitung: jeder nennt seinen eigenen.
- **Vorschlag bleibt:** **(b) für die erste Veröffentlichung.** Ein Schlüssel ohne Laden ist ein Versprechen, das keiner hält. (a) sobald es wirklich Apps gibt.

### 5. Die `ngc-*`-Skripte nach ulh-`main`

- **Worum:** Ohne diese Skripte kann niemand NGC installieren. Sie liegen fertig auf dem Zweig `ngc-skripte`.
- **Heute:** Auf `origin/main` von `ulh` liegt **kein** `ngc-*`-Skript. Ein Push dorthin erreicht per Auto-Update **sofort alle ulh-Nutzer**. Dazu hängt die erste Wand aus Punkt 71 noch dort: `ngc-api.sh` schreibt `ReadWritePaths` ohne `/etc/ngc`, und damit bleibt eine Anlage auf der alten Vertrauensliste festgenagelt.
- **Entschieden (24.10.2026):** **(a)** — erst `/etc/ngc` nachtragen, dann pushen. **Der Push selbst bleibt bei Thomas:** er erreicht sofort alle ulh-Nutzer, und das ist Veröffentlichung.
- **Warum:**
  - **STABILITÄT entscheidet.** Gemessen (Punkt 71, erste Wand): ohne die Zeile nimmt eine Anlage keine neue Vertrauensliste an. Das ist eine Anlage, die man nicht mehr erreicht, ohne an die Datei zu gehen. Zwei Pushes, bei denen der erste kaputt ist, erreichen beide sofort alle — der kaputte also auch.
  - Sicherheit und Leistung werden nicht gebraucht.
- **Zu bauen:** `/etc/ngc` in `ReadWritePaths` von `ngc-api.sh` nachtragen, auf dem Zweig `ngc-skripte`, vor dem Push.

### 6. Ein Weg, den Release-Schlüssel von außen zu setzen

- **Worum:** Niemand kann die Installationskette mit einem eigenen Schlüssel prüfen.
- **Heute:** Der öffentliche Schlüssel steht fest in vier Skripten. Wer prüfen will, verbiegt sie mit `sed` — und prüft damit nicht mehr die Skripte (Punkt 62, Befund 2).
- **Entschieden (24.10.2026):** **(b)** — ein Unterbefehl `ngc-all.sh test-key <Datei>`.
- **Warum:**
  - **STABILITÄT entscheidet nicht.** Keine der drei Wahlen ändert etwas am laufenden System.
  - **SICHERHEIT entscheidet.** Ein Unterbefehl steht in der Hilfe und im Protokoll: man sieht hinterher, dass der Schlüsselriegel umgangen wurde. Eine Umgebungsvariable wirkt still und kann aus einer Datei kommen, die niemand mehr liest. Bei einem Riegel ist die Sichtbarkeit der halbe Zweck. Abwägung, nicht gemessen.
  - Leistung wird nicht gebraucht.
- **Zu bauen:** Unterbefehl `test-key <Datei>` in `ngc-all.sh`, mit Zeile in der Hilfe und im Protokoll.

### 7. P3 — Was ist eine `ngc-web`-Release-Datei?

- **Worum:** Ein Update der Oberfläche über den Controller kann heute nicht gehen.
- **Heute:** Gemessen, nicht angenommen: die Skripte bauen Namen im Wurzelverzeichnis des tar, der Controller packt nur Namen unter `ui/` aus, und `store.WalkUI` weist das Wurzel-tar am ersten Kopfblock ab. Punkt 56 sagt „P3 ist beantwortet" — das ist seit dem 18.10.2026 als falsch gemessen.
- **Entschieden (24.10.2026):** **(a)** — das Release-Werkzeug packt unter `ui/`.
- **Warum:**
  - **STABILITÄT entscheidet.** (b) erlaubt dem Vertrag zwei Formen. Zwei erlaubte Formen heißen für immer zwei Pfade, die beide geprüft werden müssen, und der seltenere wird mit der Zeit nicht mehr geprüft. Dazu ist (a) **jetzt noch gratis**: das Packwerkzeug gibt es noch nicht, es ist nichts umzubauen. Abwägung; gemessen ist die Abweisung am ersten Kopfblock.
  - Sicherheit und Leistung werden nicht gebraucht.
- **Zu bauen:** Das Packwerkzeug legt Oberflächendateien unter `ui/` an — gleich beim ersten Bau, nicht nachträglich.

### 8. P2 — Ein erster Einbau ohne Aufsicht geht gar nicht

- **Worum:** Eine Erstinstallation ohne zusehenden Menschen ist unmöglich, obwohl der eigene Kommentar das Gegenteil behauptet.
- **Heute:** `core.Unattended` (`shared/core/core.go:225`) sieht weder die Zeile der App noch den Katalog, kann für einen **ersten** Einbau also nie `true` sagen. Der Ersatz dafür ist schon zweimal von Hand gebaut (`install.go:353`, `rollout.go:253`).
- **Entschieden (24.10.2026):** **(a)** — `Unattended` bekommt mit, dass es ein Erst-Einbau ist.
- **Warum:**
  - **STABILITÄT entscheidet.** Zweimal von Hand nachgebaut heißt: die dritte Stelle kommt, und sie wird anders aussehen als die ersten zwei. Eine Regel, die an mehreren Stellen nachgebaut wird, gilt an keiner verlässlich — welche Fassung greift, hängt dann davon ab, welchen Weg der Einbau nimmt. Abwägung; gemessen sind die zwei Nachbauten.
  - Sicherheit und Leistung werden nicht gebraucht.
- **Zu bauen:** Eine Zeile im Vertrag (Erst-Einbau), zwei Nachzieher, die Wächter in `install.go` und `rollout.go` auf `Unattended` umdrehen.

### 9. Der Vertrauensanker nach einem Totalverlust

- **Worum:** Der dokumentierte Weg zurück nach einem Totalverlust endet in einer Wand.
- **Heute:** Die Einrichtung nimmt den Admin an, danach läuft nichts. Antwort B ist **gemessen keine Antwort**. Ein dritter Weg (Mutant D) ist gemessen und hält beides.
- **Entschieden (24.10.2026):** **(b)** — der dritte Weg, Mutant D.
- **Warum:**
  - **STABILITÄT entscheidet, gemessen.** Mutant D ist der einzige der drei, der gemessen beides hält. B ist gemessen widerlegt. A ist nicht bis zum Ende gemessen — und ein Weg zurück aus dem Totalverlust, der nicht bis zum Ende gemessen ist, ist kein Weg zurück.
  - Sicherheit und Leistung werden nicht gebraucht.
- **Zu bauen:** Mutant D als den dokumentierten Weg festschreiben und den Erstlauf-Prüfstand darauf ziehen.

### 10. PR 71 oder der andere Weg

- **Worum:** Zwei Zweige schließen dasselbe Loch (eine SQLite-Datei, vier wartende Schreiber blockieren den ganzen Prozess) auf zwei Wegen. Nur einer soll landen.
- **Heute:** **Gelandet.** Der Weg aus [PR 71](https://github.com/sorglos-it/nextgencloud/pull/71) (ein Schreibkanal, vier Leser) liegt auf `main` als `9753a8c`.
- **Entschieden (24.10.2026, in `9753a8c`):** Der Weg aus **PR 71**. `origin/pool-schreibkanal` fällt damit weg.
- **Warum:**
  - **STABILITÄT entscheidet, gemessen.** Der Pool aus vier nimmt bei genau vier blockierten Schreibern jede **andere** Abfrage des Prozesses mit, Lesen eingeschlossen: 9,49 s, und der Aufrufer bekommt `E_UNREACHABLE`, das niemand von einer verschwundenen Datenbank unterscheiden kann. Mit einem Kanal je Prozess antwortet dieselbe Leseabfrage in 0,001 s. Ein feststeckender Schreiber kostet auf beiden Seiten dasselbe (80,6 s, eine Kette, kein Wachsen mit der Zahl der Schreiber) — es wird für den Gewinn nichts hergegeben.
  - Sicherheit: unverändert, `shared/` nicht angefasst.
  - Leistung: +7–8 Prozent, drei Läufe von drei. Die kleinste der drei Begründungen; sie hätte nichts entschieden.
- **Zu bauen:** Nichts. `origin/pool-schreibkanal` kann weg.

---

## B · Die v1.3-Reihe

Der fertige Änderungssatz, der Preis und was ohne ihn für immer fehlt, steht je Punkt in
[`vertraege-v1-3-vorschlag.md`](vertraege-v1-3-vorschlag.md). **P4 ist erledigt und gestrichen.**

**Die Reihenfolge steht jetzt fest:** P10 (11) zuerst, dann P8 (12), dann P9 (13),
dann P1, P11 und P12 (14–16) in einem Zug, dann P7 (19), und P5 (17) ganz zuletzt.
P13 (20) bleibt offen. P14 (21) ist ein Satz. P15 (22) kommt nach dem Schnitt.

### 11. P10 — Darf ein Vertrag von `shared/` etwas dazubekommen? (das Tor)

- **Worum:** Ob eine Ergänzung an einem Vertrag eine Ergänzung sein darf oder für immer ein Bruch ist.
- **Heute:** Ein unbekannter Schließgrund gilt als kaputter Rahmen: `Close.Validate` wirft alles ab, was nicht in der Tabelle `messages` steht (`shared/wire/wire.go:528`).
- **Entschieden (24.10.2026):** **(a)** — ein unbekannter Code wird angenommen und als unbekannt behandelt. **Als erstes von allen v1.3-Punkten.**
- **Warum:**
  - **STABILITÄT entscheidet.** Heute bricht jede künftige Ergänzung jede alte Gegenseite: eine neue Fassung auf einer Seite reißt die Verbindung ab, und der Abriss sieht aus wie ein kaputter Rahmen. Eine Anlage aus Teilen verschiedener Fassungen ist damit nicht lauffähig — und das ist der Normalfall während eines Updates. Abwägung; gemessen ist das Abwerfen in `wire.go:528`.
  - Sicherheit und Leistung werden nicht gebraucht.
- **Zu bauen:** Zwei Zeilen in `Close.Validate`: unbekannter Code wird angenommen, als unbekannt behandelt und als unbekannt protokolliert.

### 12. P8 — Eigener Schließgrund für die Leerlaufgrenze · nur nach P10

- **Worum:** Ein Terminal, das wegen Leerlauf zugeht, sieht aus wie ein Absturz.
- **Heute:** Beide enden mit demselben Grund. Niemand kann sie unterscheiden.
- **Entschieden (24.10.2026):** **JA**, direkt nach P10.
- **Warum:**
  - **STABILITÄT entscheidet.** Solange ein normales Zugehen aussieht wie ein Absturz, ist jede Absturzzahl aus dem Protokoll falsch, und jede Stabilitätsmessung am Terminal misst Leerlauf mit. Man kann die Stabilität des Terminals heute nicht beurteilen. Abwägung, nicht gemessen.
  - Sicherheit und Leistung werden nicht gebraucht.
- **Zu bauen:** Eigener Schließgrund für die Leerlaufgrenze in `wire`, Terminal setzt ihn.

### 13. P9 — Eigener Code `E_BUSY` für „besetzt" · nur nach P10

- **Worum:** Der Benutzer liest „Dienst nicht erreichbar", wenn die Datenbank nur besetzt ist.
- **Heute:** „Dienst weg", „Name löst nicht auf" und „Datenbank besetzt" sind für ihn derselbe Satz. Preis: zieht `shared/dbapi`, `memstore`, `sqlstore`, `dbtest` und die Oberfläche mit.
- **Entschieden (24.10.2026):** **JA**, nach P8.
- **Warum:**
  - **STABILITÄT entscheidet, und die Zahl liegt vor.** Gemessen in `9753a8c`: eine besetzte Datenbank meldet sich nach 9,49 s als `E_UNREACHABLE` — nicht zu unterscheiden von einer Datenbank, die weg ist. Wer das liest, startet einen Dienst neu, der in Ordnung ist. Eine falsche Reparatur an einer gesunden Anlage ist der Weg, auf dem aus einer Wartezeit ein Ausfall wird. Dieselbe Klasse wie die Anmeldelüge aus Punkt 50.
  - Sicherheit und Leistung werden nicht gebraucht.
- **Zu bauen:** `E_BUSY` in `shared/`, Nachzieher in `dbapi`, `memstore`, `sqlstore`, `dbtest`, eigener Satz in der Oberfläche.

### 14. P1 — Die Adressprüfung ausführen

- **Worum:** Pfad und Anhang einer Download-Adresse prüft dauerhaft niemand.
- **Heute:** `validHTTPSURL` ist gebaut (`shared/store/catalog.go:208`) und wird vom Katalog benutzt — zugesagt ist es nirgends, und `ValidDownloadURL` gibt es nicht.
- **Entschieden (24.10.2026):** **(a)** — ein Satz im Vertrag und ein Name dafür. Zusammen mit 15 und 16 in einem Zug.
- **Warum:**
  - **STABILITÄT entscheidet nicht.** Die Prüfung läuft heute schon; ein Satz im Vertrag ändert am Lauf nichts.
  - **SICHERHEIT entscheidet.** Was nur gebaut, aber nicht zugesagt ist, fällt beim nächsten Umbau heraus, ohne dass es jemand merkt — und dann prüft niemand mehr Pfad und Anhang einer Adresse, von der heruntergeladen wird. Ein Name macht die Prüfung zu etwas, das man beim Entfernen sieht. Abwägung, nicht gemessen.
  - Leistung wird nicht gebraucht.
- **Zu bauen:** `ValidDownloadURL` als zugesagten Namen, ein Satz im Vertrag.

### 15. P11 — `ValidPath` sagt nichts über die Schreibweise

- **Worum:** Dieselbe Eingabe verhält sich auf Windows und Linux verschieden, und niemand sagt, ob das zugesagt ist.
- **Heute:** `store.ValidPath` (`shared/store/manifest.go:241`) ist POSIX-sicher, nicht Windows-sicher. Gemessen, nicht vermutet.
- **Entschieden (24.10.2026):** **(a)** — der Vertrag ist POSIX, Windows ist nicht zugesagt.
- **Warum:**
  - **STABILITÄT entscheidet.** (b) gibt eine Zusage für eine Plattform, auf der NGC nicht läuft und auf der niemand sie dauerhaft nachmisst. Eine Zusage, die nicht gemessen wird, ist die Sorte, die irgendwann still nicht mehr gilt. NGC läuft auf Linux; Windows ist Entwicklerplatz. Abwägung; gemessen ist, dass `ValidPath` POSIX-sicher ist.
  - Sicherheit und Leistung werden nicht gebraucht.
- **Zu bauen:** Ein Satz im Vertrag: `ValidPath` ist POSIX, Windows ist nicht zugesagt.

### 16. P12 — Rechte im Katalog gegen das Manifest halten

- **Worum:** Nichts prüft, ob die Rechte im Katalogeintrag mit denen im Manifest übereinstimmen.
- **Heute:** Keine Lücke — die Katalogkopie wird nirgends als Grundlage benutzt.
- **Entschieden (24.10.2026):** **(b)** — ein Satz: die Kopie ist nie die Grundlage.
- **Warum:**
  - **STABILITÄT entscheidet.** Es gibt keine Lücke zu schließen. Eine Prüfung, die eine Gleichheit sichert, auf die sich niemand stützen soll, lädt dazu ein, sich darauf zu stützen — und dann hängt die Rechteentscheidung an einer Kopie, die der Laden schreibt. Der Satz hält die Annahme fest, ohne einen zweiten Weg zu öffnen. Abwägung, nicht gemessen.
  - Sicherheit und Leistung werden nicht gebraucht.
- **Zu bauen:** Ein Satz im Vertrag: die Rechte im Katalogeintrag sind Anzeige, nie Grundlage.

### 17. P5 — Referenzfeld in `dbapi.App`

- **Worum:** Ein Abbild ohne Verweis bleibt für immer im Datenverzeichnis liegen, und die Größenrechnung zählt es als Benutzerdaten.
- **Heute:** `dbapi.App` hat kein Verweisfeld (`shared/dbapi`, `type App struct`). Niemand kann so ein Abbild entfernen. Preis: der einzige Punkt mit Migration und bestehendem Nutzer.
- **Entschieden (24.10.2026):** **JA, und als letzter** von allen v1.3-Punkten.
- **Warum:**
  - **STABILITÄT entscheidet — hier entscheidet sie die Reihenfolge.** Es ist der einzige Punkt, der eine Migration auf einer Anlage mit Daten fährt. Eine Migration will auf einem Stand laufen, der sonst nicht mehr wackelt; läuft sie zwischen anderen Vertragsänderungen, ist nach einem Fehler nicht mehr zu sagen, welche Änderung ihn verursacht hat. Abwägung, nicht gemessen.
  - Sicherheit und Leistung werden nicht gebraucht.
- **Zu bauen:** Verweisfeld in `dbapi.App`, Migration, Aufräumweg für Abbilder ohne Verweis — nach allen anderen v1.3-Punkten.

### 18. P6 — Wie lange gilt ein Kopplungscode?

- **Worum:** Ein Code zum Aufnehmen eines Servers gilt heute unbegrenzt.
- **Heute:** Die Zahl ist nicht gesetzt. Die 24 Stunden im Code (`node/run.go:90`) sind etwas anderes — der Rhythmus der Oberflächenrunde.
- **Entschieden (24.10.2026):** **(b), eine Stunde** — mit einem sichtbaren Weg, einen neuen Code auszugeben.
- **Warum:**
  - **STABILITÄT entscheidet nicht.** Keine der drei Zahlen macht eine laufende Anlage wackelig. Ein abgelaufener Code kostet einen zweiten Anlauf beim Aufsetzen, kein Datum und keinen Dienst.
  - **SICHERHEIT entscheidet.** (c) macht aus einem vergessenen Code einen dauerhaft offenen Zugang zur Anlage. Zwischen 24 Stunden und einer Stunde ist die kürzere die sicherere, und die Bequemlichkeit dahinter ist mit einem neuen Code bezahlt — nicht mit einem offenen Tor über Nacht. Abwägung, nicht gemessen.
  - Leistung wird nicht gebraucht.
  - Hängt mit Frage 23 zusammen: der Zähler dort und diese Frist sind dieselbe Tür.
- **Zu bauen:** Frist von einer Stunde auf den Kopplungscode, und ein sichtbarer Weg, einen neuen auszugeben.

### 19. P7 — `ngc-api migrate` braucht eine ganze `api.json`

- **Worum:** Ein reiner Datenbankknoten kann seine Migrationen nicht starten.
- **Heute:** `migrate` liest die volle Konfiguration mit `load(path)` (`apps/server-api/main.go:176`) und scheitert, wenn Felder fehlen, die ein Datenbankknoten nicht hat.
- **Entschieden (24.10.2026):** **(a)** — eine eigene, kleine Lesefunktion nur für `migrate`.
- **Warum:**
  - **STABILITÄT entscheidet.** Heute kann ein Datenbankknoten seine Migrationen nicht fahren; eine Anlage, die nicht migrieren kann, bleibt auf einem alten Schema stehen. (b) heißt eine erfundene `api.json` auf jedem Datenbankknoten — eine Datei mit Attrappenwerten, die irgendwann jemand für echt nimmt und ausliest. (a) berührt nur `migrate` und hängt an nichts. Abwägung; gemessen ist das Scheitern in `main.go:176`.
  - Sicherheit und Leistung werden nicht gebraucht.
- **Zu bauen:** Eigene Lesefunktion für `migrate`, die nur die Datenbankfelder verlangt.

### 20. P13 — Prozessdeckel je App

> **Nachgetragen am 24.10.2026.** Diese Frage stand offen, weil die Messung fehlte. Die Messung ist gemacht (**M1**) — und sie hat die Annahme hinter der Frage widerlegt. Zahlen und Messaufbau: `docs/messungen-m1-m3.md`.

- **Worum:** Ob jede App ihren eigenen Prozessdeckel im Manifest bekommt.
- **Hier stand etwas Falsches:** „Jede App teilt sich heute den einen Deckel von 512 Prozessen." Der Satz stimmt **in beiden Hälften nicht**. Er war aus dem Quelltext geschlossen, nicht gemessen.
- **Heute, jetzt gemessen:** Der Deckel ist **2048, nicht 512** — abgelesen am cgroup des laufenden Containers (`pids.max`) und von innen nachgefahren: bei `pids.current = 2047` meldet der Container „Cannot fork", `pids.events` zeigt genau eine Bremsung. Und er ist **nicht geteilt**: jeder Container hat seinen eigenen. Eine App am Anschlag lässt die Nachbarin unberührt, sogar unter demselben Benutzer. Eine offene Terminal-Sitzung kostet 2 Prozesse und rund 9 Aufgaben; 64 Sitzungen kommen auf 588. Ein Feld im Manifest gibt es weiter nicht.
- **Entschieden (24.10.2026):** **Kein Prozessdeckel je App in v1.3.**
- **Warum:**
  - **STABILITÄT entscheidet.** Die Frage stand offen, weil ein geteilter Deckel reißen könnte — eine App zieht die anderen mit. Gemessen: den geteilten Deckel gibt es nicht. Damit fällt der Grund weg, aus dem ein Feld je App die Stabilität tragen sollte: es gäbe nichts zu trennen, was nicht schon getrennt ist. Ein Feld je App könnte den eigenen Deckel nur **senken** — es schützt niemanden vor der App, höchstens die App vor sich selbst. Gemessen, nicht abgewogen.
  - Sicherheit und Leistung werden nicht gebraucht.
- **Was offen bleibt, und das ehrlich:** Ob eine **andere** App mit 2048 auskommt, ist nicht gemessen — es gibt keine zweite. Im Katalog der laufenden Anlage steht genau eine App, das Terminal; das Abbild `ngc/portversuch` startet `sleep infinity` und ist eine Attrappe, kein Maßstab. Der Punkt kommt von selbst zurück, sobald eine App an ihren eigenen Deckel stößt — und dann mit einer Zahl. Dasselbe Ergebnis wie vorher, aber aus einem anderen Grund: nicht weil die Messung fehlt, sondern weil die gemessene Lage kein Feld verlangt.
- **Zu bauen:** `--pids-limit` im Controller selbst setzen, statt podmans Voreinstellung zu erben — die 2048 sind nicht von NGC gesetzt und auf einem Server mit anderer `containers.conf` ein anderer Wert.

### 21. P14 — Eine Oberfläche darf 16 MiB, ein Programm 4 GiB

- **Worum:** Ein großes Programm kann nie eine NGC-Oberfläche haben. Verhältnis 1:256, unbegründet.
- **Heute:** `MaxUIFile = 16 MiB`, `MaxUITotal = 64 MiB` (`shared/store/pack.go:22`) gegen `MaxPackage = 4 GiB`. Gemessen: ein portierter Rust-Bau war 44 MB, also 2,6-mal über der Dateigrenze.
- **Entschieden (24.10.2026):** **(b)** — die Grenzen bleiben, mit einem Satz, warum.
- **Warum:**
  - **STABILITÄT entscheidet, und jetzt mit einer Zahl.** (a) heißt: die Shell hält mehr im Speicher und hasht mehr in einem Zug, bei acht offenen Apps achtmal. Was das kostet, war zuerst nicht gemessen — **jetzt ist es gemessen** (**M2**, 24.10.2026): acht Apps mit 64 MiB kosten den Browser **1089 MiB statt 209**. Mehr als das Fünffache. Eine Grenze um diesen Preis anzuheben ist genau der Handel, den „Stabilität zuerst" verbietet. Die 44 MB auf der anderen Seite sagen, dass jemand anstoßen wird — nicht, dass der Deckel gefahrlos höher kann.
  - Sicherheit und Leistung werden nicht gebraucht.
  - **Die Messung, die diese Frage hätte umdrehen können, ist gemacht** und hat sie nicht umgedreht. Aus einem „Nein ohne Messung" ist ein **Nein mit Messung** geworden. Zahlen und Messaufbau: `docs/messungen-m1-m3.md`.
- **Zu bauen:** Ein Satz im Vertrag: NGC-Oberflächen bleiben klein und handgeschrieben, 16 MiB je Datei sind Absicht — damit niemand ein halbes Jahr in eine Portierung steckt, die an einer Zahl ohne Begründung endet.

### 22. P15 — Jede App erfindet ihre eigenen Nachrichtengrenzen

- **Worum:** Ein Strom sagt nicht, wo eine Nachricht aufhört. Das Terminal hat sich dafür einen eigenen Kopf aus drei Bytes erfunden.
- **Heute:** Der Kopf liegt in der App (`classes/terminal/terminal.go:76`), nicht im Vertrag. Jede weitere App erfindet ihn neu.
- **Entschieden (24.10.2026):** **(a)**, aber **nach dem v1.3-Schnitt**.
- **Warum:**
  - **STABILITÄT entscheidet die Richtung.** Ein fehlender Längenvergleich ist bei zehn Apps zehn Stellen, an denen ein Strom in der Mitte auseinanderfällt; einmal in `shared/` mit Vektoren ist eine Stelle, die geprüft wird. Abwägung, nicht gemessen.
  - **STABILITÄT entscheidet auch den Zeitpunkt, in die andere Richtung.** Es sind heute zwei Apps, und keine davon ist dabei kaputt. Ein Umzug des Terminals auf einen neuen Rahmen vor dem Schnitt rührt ohne Not an dem Stand, auf dem P5 später migrieren soll (siehe 17).
  - Sicherheit und Leistung werden nicht gebraucht.
- **Zu bauen:** Rahmen mit Längenkopf in `shared/`, mit Vektoren; das Terminal zieht darauf um — nach dem v1.3-Schnitt.

---

## C · Sicherheit und Oberfläche

### 23. Wird der Einmal-Code vor oder nach der MAC-Prüfung verbraucht?

- **Worum:** Wer die Adresse kennt, kann mit einer falschen MAC jeden wartenden Beitrittscode verbrennen.
- **Heute:** Verbraucht wird vorher. Gilt an **beiden** Stellen (`join.Accept`, `controllers.Pair`).
- **Entschieden (24.10.2026):** **(a) mit Zähler** — nachher verbrauchen, und nach einigen falschen MACs sperren.
- **Warum:**
  - **STABILITÄT entscheidet.** Heute kann jeder, der die Adresse kennt, jeden wartenden Beitritt von außen zerstören — ohne Schlüssel, ohne Zugang, mit einer falschen MAC. Der Weg, auf dem eine Anlage wächst, ist damit von außen abschaltbar, und der Admin sieht nur einen Code, der „schon benutzt" ist. Abwägung, nicht gemessen.
  - **SICHERHEIT setzt die Bedingung.** Nachher verbrauchen allein lässt einen Angreifer beliebig oft rechnen. Darum der Zähler: nachher verbrauchen **und** nach einigen falschen MACs sperren. Nur eins von beiden ist ein offenes Tor in die eine oder die andere Richtung.
  - Leistung wird nicht gebraucht.
  - Gehört mit Frage 18 zusammen entschieden: die Frist dort begrenzt, was ein Zähler hier offen lässt.
- **Die Schwelle (nachgetragen 24.10.2026):** **3 bleibt** — und was sie bisher unter Vorbehalt stellte, ist jetzt ein messbarer Zustand statt eines blinden Flecks.
  - **Gemessen** an der verteilten Anlage, mit einem Kopplungscode, den die Messung selbst angelegt hat: ein einziger falscher MAC ließ `used` auf **1** springen und die Anlage antwortete `E_VERIFY`. **Die Schwelle 3 kann heute kein Code je erreichen**, weil der erste Fehlversuch ihn schon verbraucht. Die Zahl 3 ist also keine Zahl über heute, sondern eine über den Zustand nach der anderen Hälfte dieser Frage (nachher verbrauchen). Vorher ist der Zähler nicht zu klein oder zu groß, sondern wirkungslos.
  - **Gemessen**, dass der Fehlversuch überhaupt sichtbar wird: dieselbe Messung ließ das Protokoll der Anlage vorher bei **412 → 412** Zeilen stehen und mit der neuen Fassung bei **415 → 416** — eine Zeile, `trust` / `pair` / `E_VERIFY` / „Der Nachweis zum Kopplungscode eines Controllers stimmt nicht." Damit ist die Zahl, auf der die Schwelle beruht, von heute an erhebbar; erhoben ist sie noch nicht.
  - **STABILITÄT entscheidet** und entscheidet für 3, unverändert aus M3: ein ehrlicher Verwalter erzeugt null falsche MACs, seine Wiederholungen kosten ihn **neue Codes** und keine Zählerstände (gemessen: bis zu 2 je Server). Es gibt nichts, was 3 ihm wegnimmt.
  - **SICHERHEIT setzt die Grenze nach oben:** jeder weitere Versuch ist ein weiterer Rateversuch. Weil Stabilität nichts Höheres verlangt, bleibt die Zahl klein. Leistung wird nicht gebraucht.
  - **Abgewogen, nicht gemessen** bleibt der Schritt von 1 auf 3: er ist der Platz für eine Wiederholung, die unterwegs verloren ging.
- **Zu bauen:** An beiden Stellen (`join.Accept`, `controllers.Pair`) erst die MAC prüfen, dann verbrauchen, plus Fehlversuchszähler mit Sperre. **Beides ist BLOCKIERT:** `shared/` ist eingefroren, und keine Methode in `shared/dbapi/dbapi.go:137` (`OnceCreate`), `:140` (`OnceUse`) und `:146` (`OncePurge`) liest einen Einmal-Wert, **ohne ihn zu verbrauchen**, oder hält einen Zählerstand dazu. Protokolliert wird der Fehlversuch seit dem 24.10.2026 (`audit.Log.Pairing`); gezählt und gesperrt wird er erst, wenn `shared/dbapi` wieder angefasst werden darf.

### 24. Zweite Beitrittsanfrage für denselben Namen: überschreiben oder abweisen?

- **Worum:** Zwei Server mit demselben Namen verdrängen einander, und der Admin erfährt nichts.
- **Heute:** Die zweite überschreibt die erste, still. Unterscheiden ließen sie sich am öffentlichen Schlüssel.
- **Entschieden (24.10.2026):** **(a)** — abweisen.
- **Warum:**
  - **STABILITÄT entscheidet.** Eine wartende, echte Anfrage verschwindet still, weil von außen eine zweite mit demselben Namen kam. Der Admin nimmt dann einen anderen Server an als den, auf den er wartet, und merkt es nicht. Ein Name, der schon wartet, ist für ihn dagegen ein Fehler, den er selbst lösen kann. Abwägung, nicht gemessen.
  - Sicherheit und Leistung werden nicht gebraucht.
- **Zu bauen:** Zweite Anfrage für einen schon wartenden Namen abweisen, mit einem Satz, der den Namen nennt.

### 25. `noexec` auf `/data`

- **Worum:** Eine App darf heute Skripte ausführen, die sie selbst unter `/data` ablegt.
- **Heute:** Kein `noexec`. Punkt 51 nennt den Schalter, Punkt 36 nicht.
- **Entschieden (24.10.2026):** **(a)** — einschalten, **vor der Veröffentlichung**.
- **Warum:**
  - **STABILITÄT entscheidet — über den Zeitpunkt, und damit über die Frage.** Heute gibt es keine fremden Apps, die der Schalter brechen könnte: die Verhaltensänderung kostet jetzt niemanden etwas. Nach der Veröffentlichung gibt es sie, und dann ist derselbe Schalter ein Bruch an fremden Installationen. Die Wahl ist also nicht „ein oder aus", sondern „jetzt gratis oder später teuer".
  - **SICHERHEIT bestätigt** die Richtung: Daten, die eine App selbst schreibt, sollen nicht ausführbar sein. Entscheiden musste sie nicht.
  - Leistung wird nicht gebraucht.
- **Zu bauen:** `noexec` auf `/data` in der Unit, und ein Satz in der Anleitung, dass Daten dort nicht ausführbar sind.

### 26. Digest statt Tag für das Terminal-Abbild — und wer hebt ihn an?

- **Worum:** Zwei Bauten desselben Stands sind heute zwei verschiedene Abbilder, und wer den fremden Tag beherrscht, beherrscht einen Container mit Systemrechten.
- **Heute:** Es hängt an einem Tag. Ein festgenagelter Digest, den niemand pflegt, ist nach einem halben Jahr aber schlechter als ein Tag.
- **Entschieden (24.10.2026):** **(b)** — Digest festnageln **und** eine Prüfung, die meldet, wenn er alt wird.
- **Warum:**
  - **STABILITÄT entscheidet.** (c) heißt: dieselbe Fassung baut zwei verschiedene Abbilder, und was auf der einen Maschine läuft, ist nicht das, was gemessen wurde. (a) tauscht das gegen ein anderes Stabilitätsproblem: ein Digest, den ein Mensch im Kalender anheben soll, steht nach dem zweiten Monat still, und dann läuft überall eine halbjahrealte Basis. Nur (b) hält beides — reproduzierbar **und** nicht veraltend.
  - Sicherheit und Leistung werden nicht gebraucht.
  - Hängt mit Frage 28 zusammen: das Abbild kommt als Datei aus dem Release, also wird der Digest dort festgenagelt.
- **Zu bauen:** Digest festnageln, plus eine Prüfung im Prüfsatz, die meckert, sobald er zu alt ist.

### 27. Terminal: Rohtasten oder Zeilenfeld?

- **Worum:** Das Terminal kann heute kein Passwort abfragen und kein `vi`.
- **Heute:** Zeilenfeld. Damit tippt ein Admin irgendwann sein `sudo`-Passwort in ein Feld, das es **anzeigt**.
- **Entschieden (24.10.2026):** **(a)** — Rohtasten.
- **Warum:**
  - **STABILITÄT entscheidet nicht.** Das Format kann Rohtasten schon, es ist keine Vertragsänderung, und es geht kein Weg verloren.
  - **SICHERHEIT entscheidet.** (b) heißt: früher oder später steht ein `sudo`-Passwort auf dem Schirm, in einem Feld, das es anzeigt, und in allem, was den Schirm mitschreibt. Abwägung, nicht gemessen.
  - Leistung wird nicht gebraucht.
- **Zu bauen:** Rohtasten im Terminal, Zeilenfeld entfällt.

### 28. Bleibt `ngc-controller image` leer oder wird es gefüllt?

- **Worum:** Ob der Controller sein Terminal-Abbild selbst baut oder es mitgebracht bekommt.
- **Heute:** Leer. Die Basis ist mit einem Digest festgehalten (`build.go:98`), das Abbild selbst baut niemand.
- **Entschieden (24.10.2026):** **(b)** — leer lassen, das Abbild kommt als Datei aus dem Release.
- **Warum:**
  - **STABILITÄT entscheidet.** (a) braucht auf jeder Zielmaschine eine Baukette und Netz nach draußen; beides fällt irgendwann aus, und dann steht die Installation an einem Bau, nicht an NGC. Eine Datei aus dem Release ist dasselbe Abbild für alle und gegen eine Prüfsumme prüfbar. Abwägung, nicht gemessen.
  - Sicherheit und Leistung werden nicht gebraucht.
  - Hängt mit Frage 26 zusammen: der Digest wird dann im Release festgenagelt.
- **Zu bauen:** `ngc-controller image` bleibt leer; das Terminal-Abbild wird Teil des Release, mit Prüfsumme.

### 29. Ein Knopf „Jetzt nachsehen" im Laden (Punkt 66, zweite Hälfte)

- **Worum:** Ein Verwalter, der auf einen Katalog wartet, hat keine Handlung außer Warten.
- **Heute:** **Die erste Hälfte ist gebaut:** der erste Abruf läuft eine Minute nach dem Start (`FirstCatalog`, `apps/server-api/classes/server/server.go:480`), die Stundenrunde danach unverändert. Einen Knopf gibt es nicht.
- **Entschieden (24.10.2026):** **(a)**, aber **nach der Veröffentlichung**, und nur mit Ratenbegrenzung.
- **Warum:**
  - **STABILITÄT entscheidet den Zeitpunkt.** Die Minute aus der ersten Hälfte nimmt dem Fall die Schärfe: der Knopf ist jetzt Bequemlichkeit, nicht mehr der einzige Ausweg. Nichts steht, wenn er fehlt — also hat er keinen Platz vor den Punkten, an denen etwas steht.
  - **SICHERHEIT setzt die Bedingung.** Ein Knopf ist ein Weg mehr, den jemand von außen auslösen kann, und braucht dieselbe Ratenbegrenzung wie jeder andere Aufruf.
  - Leistung wird nicht gebraucht.
- **Zu bauen:** Knopf „Jetzt nachsehen" im Laden, mit Ratenbegrenzung wie jeder andere Aufruf — nach der Veröffentlichung.

---

## D · Zwei Wege nebeneinander, ein Messort, eine Nummer

### 30. Zwei Unterschreiber eines Releases

> **Bleibt offen.** Nicht wegen der Rangfolge, sondern weil die Antwort an Frage 2 hängt — und 2 bleibt bei Thomas.

- **Worum:** Es gibt zwei Wege, ein Release zu unterschreiben. Nur einer sollte es sein.
- **Heute:** `ngc-store release` (der gemessene Weg, Punkt 57) und `tools/sign-release.sh` (mit einem eigenen Prüffall). Der Commit `cd69b30` sagt ausdrücklich, das sei nicht durch Löschen eines Falls zu klären.
- **Wahl:** (a) `ngc-store release` bleibt, das Skript geht. (b) Das Skript bleibt für die Karte, `ngc-store` für die Datei — dann sind es dauerhaft zwei.
- **Warum nicht entschieden:** Wird es der Stick (Frage 2b), ist `tools/sign-release.sh` genau der Weg, der die Karte bedient — dann ist (a) falsch. Bleibt es die Datei (2a), ist (a) richtig. Die Rangfolge kann das nicht drehen; nur Frage 2 kann es.
- **Vorschlag bleibt:** **(a), sobald Frage 2 entschieden ist.** Vorher nicht.

### 31. Welche der fünf Nummern ist die Fassung eines Releases? (D4)

- **Worum:** Ein Release trägt **eine** Fassungsnummer, aber es enthält **fünf** Programme.
- **Heute:** `store.Release` hat ein `Version`-Feld. Welche der fünf es ist, steht nirgends. Die vierstellige Hausnummerierung ist dagegen kein Riegel mehr: `store.ValidVersion` nimmt die vierte Stelle an (`shared/store/manifest.go:231`).
- **Entschieden (24.10.2026):** **(a)** — eine eigene Release-Nummer über den fünf Programmen.
- **Warum:**
  - **STABILITÄT entscheidet.** Ein Release ist das Ding, das man zurückrollt — und zurückrollen kann man nur, was eine eigene Nummer hat. (b) macht ein Programm willkürlich zum Maß: ändert sich nur ein anderes, trägt das Release dieselbe Nummer wie ein anderer Stand, und dann zeigen zwei verschiedene Installationen dieselbe Zahl. (c) erzwingt Nummernsprünge bei Programmen, an denen sich nichts geändert hat, und damit Updates, die nichts ändern. Abwägung, nicht gemessen.
  - Sicherheit und Leistung werden nicht gebraucht.
- **Zu bauen:** Eigene Release-Nummer in `store.Release`, ein Satz, dass sie über den fünf Programmen steht, und die fünf Programmnummern daneben.

### 32. Weiter auf `ngc.test`, oder auf einer Maschine mit echtem Namen?

- **Worum:** Wo gemessen wird. Alle heutigen Zahlen gelten für einen Browser, der ein selbst unterschriebenes Zertifikat für den Lauf annimmt.
- **Heute:** `ngc.test` mit eigenem Zertifikat. Was ein Nutzer mit `letsencrypt` erlebt, ist ungemessen.
- **Entschieden (24.10.2026):** **(b), einmal vor der Veröffentlichung** den ganzen Weg. `ngc.test` bleibt der Messort für alles andere.
- **Warum:**
  - **STABILITÄT entscheidet.** Passschlüssel und Browser hängen genau an Zertifikaten — das ist der eine Unterschied, den `ngc.test` nicht zeigen kann. Ohne diesen Lauf ist „die Anmeldung läuft" für keine echte Installation gemessen. Hier ist die fehlende Messung der Grund, den Lauf zu machen, nicht der Grund, nichts zu entscheiden.
  - Sicherheit und Leistung werden nicht gebraucht.
- **Zu bauen:** Einmal der ganze Erstlauf auf einer Maschine mit echtem Namen und `letsencrypt`, mit Passschlüssel-Anmeldung im Browser.

---

## Was fehlt — die Messungen, die eine Entscheidung tragen würden

Hier standen drei Messungen, die fehlten. **Alle drei sind am 24.10.2026 gemacht**
— an laufenden Systemen, nicht am Quelltext. Die Zahlen, der Messaufbau und die
Lücken, die dabei offen blieben, stehen in `docs/messungen-m1-m3.md`. Was unten
steht, ist die Vorgabe: was jeweils gebraucht wurde und welche Frage daran hing.

### M1 — Wie viele Prozesse braucht eine echte App? (hielt **20** offen · **gemessen**)

Gebraucht wurde: die Zahl der gleichzeitig laufenden Prozesse je App unter Last,
für das Terminal und für mindestens eine zweite App, und ob eine davon dem
Deckel nahekommt.

**Ergebnis:** Der Deckel ist 2048 und gilt je Container, nicht geteilt. Damit ist
Frage 20 entschieden — siehe dort. Zwei Lücken blieben: eine zweite echte App gibt
es nicht, und über 64 Sitzungen hinaus blieb die Messvorrichtung stehen, nicht der
Container.

### M2 — Was kostet eine größere Oberflächengrenze im Speicher? (könnte **21** umdrehen · **gemessen**)

Gebraucht wurde: der Speicherbedarf der Shell bei acht offenen Apps, einmal mit
16 MiB je Datei und einmal mit der angehobenen Grenze, samt der Zeit fürs Hashen
in einem Zug.

**Ergebnis:** Der Preis liegt **nicht** niedrig — acht Apps mit 64 MiB kosten den
Browser 1089 MiB statt 209. Frage 21 bleibt, wie sie entschieden war, jetzt mit
einer Zahl dahinter.

### M3 — Wie oft wird ein Kopplungscode neu ausgegeben? (bestimmt die Schwelle in **23** · **gemessen**)

Gebraucht wurde: wie oft im normalen Aufsetzen ein Code neu ausgegeben wird und
wie viele MAC-Fehlversuche dabei echt vorkommen. Erst daraus ergibt sich, bei
welcher Zahl der Zähler sperren darf, ohne einen ehrlichen Admin auszusperren.
Die Entscheidung in 23 hängt nicht daran — nur die Zahl im Zähler.

**Ergebnis:** 1,75 bis 3 Codes je Server. Fehlversuche: **keine gesehen — aber es
wird auch nichts aufgezeichnet.** Die Schwelle 3 steht darum unter Vorbehalt, und
„null" heißt hier „nichts gesehen", nicht „nichts passiert".

---

## Nicht mehr zu entscheiden — inzwischen gebaut

Hier standen Fragen, die keine mehr sind. Am Quelltext von `main` nachgesehen, 24.10.2026:

| Frage | Heute |
|---|---|
| Bekommt die einrichtende Person Systemrechte automatisch? | **Entschieden und gebaut.** `rootPicker` kreuzt die unterschreibende Person vor, abwählbar, nur `rootUsers`, nur dieser Controller (`apps/web/public/assets/js/apps/users.js:301` und `:1236`). Eine Rolle wird nicht vorgekreuzt. Für `askJoinAPI` bleibt die Liste leer, und das ist richtig. |
| Bekommt sie das Recht „benutzen" für eine neue App automatisch? | **Ja, gebaut.** `adminApps` setzt einem Verwalter `<app>/use` und jede Aktion jeder eingerichteten App — **ohne Zeile in der Datenbank** (`apps/server-api/classes/rights/rights.go:56`). Damit steht eine neue App im Menü und darum auch in der Rechteliste. Eine **abgeschaltete** App lassen Menü und Rechte beide aus; das ist so gewollt. |
| Darf eine frische Anlage den Katalog beim Start holen? | **Ja, gebaut.** Erster Abruf eine Minute nach dem Start (`FirstCatalog`, `server.go:480`). Der Knopf „Jetzt nachsehen" fehlt weiter — siehe Frage 29. |
| „Der Katalog ist nicht echt" für „noch nicht geholt" | **Behoben.** Der leere Zustand hat seinen eigenen Satz (`apps/web/public/assets/js/apps/store.js:359`). |
| Vierstellige Nummern gegen `store.ValidVersion` | **Behoben.** Die vierte Stelle ist erlaubt (`shared/store/manifest.go:231`). |
| Verwirft ein unlesbares Anzeigefeld die ganze Antwort? | **Entschieden und gebaut** (PR #104): `version` und `arch` werden leer gemacht, harte Felder fallen weiter ganz, und der Satz nennt das Feld. |
| `0700` über dem Heimatverzeichnis der App | **Still behoben.** `main` legt `/var/lib/ngc/apps` mit `0o711` an (`apps/server-controller/classes/runtime/data.go:122`). |
| P4 — Balancer-Vorlage in `shared/ulh.md` | **Erledigt**, aus der v1.3-Reihe gestrichen. |
| PR 71 oder der andere Weg (Frage 10) | **Entschieden und gelandet** als `9753a8c`: ein Schreibkanal, vier Leser. Stabilität, gemessen — 9,49 s gegen 0,001 s. |
