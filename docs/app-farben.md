# Wie die Farben der Oberfläche in die Apps kommen

Stand 30.10.2026. Entwurf, nicht entschieden. Gemessen am Zweig `app-farben`
(aus `origin/main`, 25b8f24e).

Die Frage ist die, die `docs/gestaltung.md` Punkt 8 offen gelassen hat
(Zeile 142–149): „Wie weit eine Vorlage in eine App darf, ohne diesen Riegel
zu lösen."

---

## 1. Wie der Riegel heute wirklich steht

Gemessen, jede Zeile nachlesbar.

**Der Ursprung.** Ein App-Fenster ist ein `<iframe>` auf `/frame.html`, ein
Dokument dieses Servers (`appframe.js:34`, `appframe.js:251`). Kein `srcdoc`
und kein `data:`-Dokument — die erben die Regeln der Seite, die sie einrahmt;
das ist gemessen und steht als Begründung in `classes/Page.php:16–27`.

**Die Sandkiste.** `sandbox="allow-scripts"` steht zweimal: als Merkmal am
Rahmen (`appframe.js:244`) und im Kopf der Antwort
(`classes/Page.php:52`, `FRAME_CSP`). `allow-scripts` **ohne**
`allow-same-origin` heißt: eigener Ursprung, kein Speicher, keine Cookies.
Dazu `referrerpolicy="no-referrer"` (`appframe.js:245`) und `allow=""`
(`appframe.js:246`), also keine Browser-Befugnisse.

**Die Regeln im Rahmen.** Zweimal dasselbe, Kopf und Dokument:
`classes/Page.php:52–55` und `appframe.js:840–842`:

    default-src 'none'; script-src data:; style-src data: 'unsafe-inline';
    img-src data:; font-src data:; media-src data:; connect-src 'none';
    form-action 'none'; base-uri 'none'

`connect-src 'none'` — die App kann **nichts** aus dem Netz holen. Jede
Adresse im App-Dokument wird vorher durch die Bytes dahinter ersetzt
(`appframe.js:769–832`), `<base>` fliegt raus (`:779`), jedes `http-equiv`
der App fliegt raus (`:793`).

**Was die App ausführt, ist das, was unterschrieben wurde.** Prüfschritt 1–5
in `appframe.js:6–12`: Beweis gegen die Vertrauensliste, Prüfsumme über das
ui-tar, und zwischen Prüfsumme und Auspacken steht **nichts** — kein `await`,
kein Rückruf (`appframe.js:719–736`). Die Startseite muss eine Datei dieses
Archivs sein (`:751–756`).

**Was heute schon hineingeht.** Drei Dinge, und nur drei:

| Was | Wo | Geprüft? |
|---|---|---|
| `ngc.js`, die Bücherei | `appframe.js:38`, `:847–850` | eigene Datei der Oberfläche |
| `data-theme` + `data-accent` auf `<html>` | `appframe.js:856–857` | **nein** |
| Nachricht `op:"theme"` mit `theme` und `accent` | `appframe.js:527–536` | **nein** |

Die `theme`-Nachricht ist laut Kommentar (`appframe.js:478–480`) „das einzige,
was die Oberfläche einer App unaufgefordert zuschiebt".

**Der Befund, der zählt:** Der Farbwert wird in `appframe.js:944–946`
unbesehen aus dem Dokument gelesen (`getComputedStyle(...)` auf
`--accent`) und ungeprüft hineingegeben. Geprüft
wird er **in der App**: `assets/apps/calculator/app.js:188` und
`assets/apps/terminal/app.js:384` halten je eine eigene Kopie des Musters
für eine Rautenfarbe mit drei bis acht Stellen, bevor sie
`setProperty("--accent", …)` rufen.

Das ist die Prüfung an der falschen Stelle. Eine App aus dem Laden muss nicht
prüfen, und sie wird es nicht. Und das Muster ist zu weit: eine Raute mit
sieben Stellen ist keine Farbe, kommt aber durch. Dass heute kein Schaden
daraus folgt, liegt allein daran, dass `setProperty` den Wert selbst noch
einmal durch den CSS-Werteleser schickt und `connect-src 'none'` eine Adresse
darin ins Leere laufen lässt. Das ist Glück, keine Schranke.

**Was gegenläufig geht.** Nachrichten der App werden hart gegen den Rahmen
geprüft: Absender muss das eigene Fenster sein (`appframe.js:452`), das Wort
muss stimmen (`:464`), die App-Kennung kommt **nie** aus der Nachricht
(`:540–543`), und die Aktion muss im eigenen Handbuch der App stehen
(`:555–558`). In dieser Richtung ist der Riegel dicht.

---

## 2. Drei Wege

### Weg A — ein Stylesheet oder eine Vorlage aus der Oberfläche in den Rahmen

Die Oberfläche gibt der App CSS: entweder `shell.css` selbst, oder den
App-Ordner einer Vorlage (`gestaltung.md` Punkt 8: „Liegt in der Vorlage ein
Ordner für eine App […], wird **sein** Aussehen geladen").

**Was er kann:** alles. Eine Vorlage kann eine App vollständig neu kleiden,
genau wie bei Joomla und PrestaShop.

**Was er kostet:** Die geprüfte Kette bricht. Heute gilt „was geprüft ist, ist
was läuft" (`appframe.js:16`) — das Dokument wird aus den Dateien des Archivs
**und aus nichts anderem** gebaut (`:12`). Ein Stylesheet von außen ist
„etwas anderes". Entweder muss es mit in den Prüfweg (eigene Unterschrift,
eigene Prüfsumme, eigene Vertrauensliste — ein zweiter Beweisweg neben dem
ui-tar), oder der Satz in Zeile 12 ist nicht mehr wahr.

**Was ein böswilliger App-Anbieter damit anfangen könnte:** Hier ist der
Angreifer nicht der App-Anbieter, sondern **die Vorlage** — und der
App-Anbieter ist das Opfer. CSS kann:

- **Knöpfe verschieben und verdecken.** `gestaltung.md:147–149` sagt es selbst.
  Ein „Abbrechen", das über „Löschen" liegt, ist ein Klick, den der Mensch
  nicht gemeint hat. Eine Oberfläche, die das einer App antun kann, ist keine
  Schranke mehr.
- **Inhalt lesen und hinausschreiben.** Ein Wähler, der auf den Anfang eines
  Eingabefeldes passt und dazu ein Hintergrundbild lädt, buchstabiert
  Eingaben Zeichen für Zeichen aus. Dass `connect-src 'none'` und
  `img-src data:` das heute aufhalten, ist der **einzige** Grund, warum dieser
  Weg nicht sofort ein Leck ist — und wer ein Stylesheet hineingibt, wird als
  nächstes Schriftarten oder Bilder von außen wollen, und dann ist die Regel
  offen.
- **Vortäuschen.** Eine Vorlage kann eine App so anmalen, dass sie wie eine
  andere aussieht.

Und umgekehrt: je mehr die Oberfläche in den Rahmen schreibt, desto mehr
Fläche hat die App, um aus dem Verhalten der Oberfläche etwas über die Anlage
zu lernen.

**Darum ist dies der naheliegendste und der gefährlichste Weg.** Er ist der
einzige, der freien, von Menschen geschriebenen Text aus der Oberfläche in ein
Dokument trägt, das die Oberfläche sonst Byte für Byte selbst gebaut hat.

### Weg B — ein festes, geprüftes Häufchen von Werten

Die Oberfläche gibt **keinen Text**, sondern eine Abbildung von Namen auf
Werte. Die Namen stehen in einer festen Liste im Quelltext. Jeder Wert wird
gegen ein strenges Muster geprüft, bevor er den Rahmen sieht. Aus dem Häufchen
baut die Oberfläche selbst — in `appframe.js` — ein Stylesheet, dessen ganzer
Text die Form `:root{--name:wert;…}` hat, und hängt es als erstes Stylesheet
in das Dokument. Die Apps benutzen dann `var(--accent)` mit eigenem
Rückfallwert und brauchen keinen eigenen Code.

**Das ist nicht Weg A.** In A geht ein Stylesheet hinein, das ein Mensch
geschrieben hat. In B geht ein Stylesheet hinein, das `appframe.js` erzeugt
hat; über die Grenze geht kein Satzzeichen, nur Name und Wert aus einer
geschlossenen Menge.

**Was er kann:** Die Farbe der Oberfläche ist in der App die Farbe der
Oberfläche, sofort und ohne Zutun der App. Die sieben Kopien in
`assets/apps/*/app.css` werden zu Rückfallwerten statt zu Zweitbeständen. Der
Umschalt-Weg ist gebaut und gemessen: die `theme`-Nachricht läuft schon
(`appframe.js:527–536`).

**Was er kostet:** Eine Liste erlaubter Namen und ein Muster je Wert müssen
gepflegt werden. Was nicht in der Liste steht, kommt nicht hinein — eine
Vorlage, die eine App umbauen will, kann es nicht. Die Wirkung bleibt auf
Farben beschränkt; Maße, Ecken, Schatten nur, wenn sie ausdrücklich
aufgenommen werden, jedes mit eigenem Muster.

**Was ein böswilliger App-Anbieter damit anfangen könnte:** Nichts, was er
nicht schon kann. Er bekommt sechs bis zehn Farbangaben, die jeder Mensch auf
dem Bildschirm sieht. Keine Namen, keine Kennungen, keine Zeiten, keine Zahl,
die über die Anlage etwas sagt. Die Richtung ist einseitig — die App kann den
Satz nicht zurückschreiben, denn es gibt keine Nachricht dafür, und
`onMessage` (`appframe.js:447–474`) kennt nur `ready`, `call`, `open`, `send`,
`end`, `close`.

Der eine Rest, und er ist zu nennen: ein Farbwert kann eine Anlage
**wiedererkennbar machen**. Wählt ein Mensch eine Farbe, die selten ist, dann
ist diese Farbe ein Merkmal, das jede App dieser Anlage sieht. Deshalb: nur
die Farben, die tatsächlich auf dem Schirm stehen, und keine Werte, die der
Mensch frei eintippen durfte, ohne dass die Oberfläche sie auf eine Auswahl
abbildet.

### Weg C — die App bringt ihre Farben mit, die Oberfläche gibt nur den Namen des Satzes

Heute schon fast so: `data-theme` geht hinein (`appframe.js:856`), die Werte
stehen in `app.css`. Der Weg wäre, es dabei zu lassen und die Bündelung über
das Bauen zu lösen: ein Werkzeug, das `shell.css` liest und die App-CSS beim
Paketieren daraus erzeugt, so dass die Kopie keine Handarbeit mehr ist.

**Was er kann:** Der Riegel wird nicht einmal berührt. Über die Grenze geht
nur ein Wort aus zwei möglichen.

**Was er kostet:** Eine Farbwahl, die der Mensch **zur Laufzeit** trifft,
erreicht die App nie — die App müsste neu gepackt und neu unterschrieben
werden. Für eine Anlage, in der die Farbe wählbar ist (`shell/theme.js`,
`ACCENTS`), heißt das: die App ist dauerhaft in der falschen Farbe. Und für
Apps aus einem fremden Laden geht es überhaupt nicht, weil NGC deren Paket
nicht baut.

**Was ein böswilliger App-Anbieter damit anfangen könnte:** Nichts Neues. Er
kann seine App falsch anmalen — das kann er heute auch.

---

## 3. Empfehlung

**Weg B.** Nach der Rangfolge Sicherheit > Stabilität > Geschwindigkeit, und
die Rangfolge entscheidet schon auf der ersten Stufe.

**Sicherheit.** B lässt die Schranke, wo sie ist. Der Satz „das Dokument wird
aus den Dateien des Archivs und aus nichts anderem gebaut" bleibt wahr, bis
auf eine Ausnahme, die vollständig aufgeschrieben ist: ein von `appframe.js`
erzeugter Satz von Farbvariablen. A ist auf dieser Stufe schon aus — es trägt
fremden, von Menschen geschriebenen Text in das App-Dokument und gibt der
Oberfläche Macht über die Knöpfe einer App. C ist auf dieser Stufe das
sicherste, fällt aber auf der nächsten.

**Stabilität.** C kann die Aufgabe nicht erfüllen: eine zur Laufzeit gewählte
Farbe erreicht eine unterschriebene App nicht. Damit bleibt B.

**Geschwindigkeit** entscheidet nichts: ein Stylesheet von ein paar hundert
Byte, einmal je Fenster, neben einem ui-tar, das bis 84 MiB wiegen darf
(`appframe.js:46`).

**Was ich an Thomas' Vorschlag ändere** — und das ist der Grund, warum er
stützt und nicht nur bestätigt wird: der Vorschlag sagt „geht in den Rahmen und
wird **dort** zu CSS-Variablen". „Dort" heißt heute: in der App, mit einer
Prüfung, die jede App selbst mitbringen muss
(`calculator/app.js:188`, `terminal/app.js:384`). Das ist die Prüfung in der
Hand dessen, dem man nicht glauben muss. Besser: **die Oberfläche prüft und die
Oberfläche setzt** — `appframe.js` erzeugt das Stylesheet, die App tut gar
nichts. Dann gilt die Prüfung auch für eine App aus dem Laden, die nie davon
gehört hat.

**Was ich nicht entscheide:** welche Farben, welche Namen, wie viele. Das ist
Thomas' Sache. Dieser Entwurf sagt nur, wie sie hineinkommen.

---

## 4. Wenn zugestimmt wird: die Schritte, je ein PR

1. **Prüfen und setzen in `appframe.js`.** Feste Namensliste, Muster je Wert,
   erzeugtes `:root{…}`-Stylesheet als erstes im Kopf; die ungeprüfte Lesung in
   `appframe.js:944–946` bekommt die Prüfung davor. Ein Wert, der das Muster
   nicht erfüllt, kommt nicht hinein, und die App sieht ihren Rückfallwert.
2. **Dasselbe für die laufende Nachricht** (`appframe.js:527–536`), damit
   Umschalten und erster Anstrich durch dieselbe Prüfung gehen.
3. **`assets/apps/calculator/app.css` und `assets/apps/terminal/app.css`**: die
   Kopien werden Rückfallwerte. Die Prüfungen in den beiden `app.js` fallen
   weg, weil sie dann in der Oberfläche stehen.
4. **Belegen:** im Browser, vorher neu laden, Bild je Satz.

## 5. Offene Punkte für die Leitsitzung

- **Überschneidung:** `public/assets/ngc.js` gehört dem Strang „Design von
  NGC". Weg B in der hier empfohlenen Form braucht `ngc.js` **nicht** — genau
  darum ist sie so gewählt. Wird stattdessen über `ngc.js` gesetzt, muss der
  andere Strang bauen.
- **Welche Namen und welche Muster** gehören in die Liste? Vorschlag zur
  Vorlage, nicht zum Beschluss: die sieben, die heute in beiden `app.css`
  stehen (`--accent`, `--win-bg`, `--win-fg`, `--win-border`, `--title-bg`,
  `--field-bg`, `--muted`), plus `--alarm` aus dem Terminal.
- **`shared/` ist eingefroren** und wird für Weg B nicht gebraucht. Nichts
  blockiert.
