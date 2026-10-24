# Wie weit eine Vorlage in eine App darf

Das ist die Frage, die Punkt 8 von `docs/gestaltung.md` ausdrücklich offen
gelassen hat: eine App läuft in einem Rahmen mit **eigenem Ursprung**, damit
nichts von der Oberfläche in sie hineinreicht (`shell/appframe.js`, Schritt 7).
Eine Vorlage, die das Aussehen einer App ersetzt, reicht genau dort hinein.

**Die Antwort: es geht, und der Riegel bleibt zu.** Was hineingeht, ist CSS —
Text, kein Dokument, kein Programm. Nichts an Schritt 7 wird gelockert, und
keine der beiden Sicherheitsregeln (`Page::CSP`, `Page::FRAME_CSP`) wird
angefasst.

## Was der Riegel heute ist

Nachgelesen, nicht vermutet:

1. `/frame.html` kommt mit `sandbox allow-scripts` in der **Kopfzeile**. Der
   Browser gibt dem Dokument damit einen eigenen, undurchsichtigen Ursprung,
   egal wie es geladen wird (`classes/Page.php`, `FRAME_CSP`).
2. `frame-boot.js` schreibt gar nichts, wenn `window.origin` nicht `"null"`
   ist. Der Rahmen fällt zu, nicht auf.
3. Der Rahmen hat `connect-src 'none'`, `img-src data:`, `font-src data:`,
   `media-src data:`, `style-src data: 'unsafe-inline'`, `script-src data:`.
   Er holt nichts, von nirgendwo.
4. Die Bytes der App sind bewiesen: `appframe.js` holt den ui-tar, rechnet den
   Hash, vergleicht ihn mit dem unterschriebenen UI-Beweis und liest **vorher
   nichts** aus dem Archiv. Zwischen Hash und Lauf steht kein `await`.

## Was eine Vorlage darf

**Genau eins: ein weiteres Stylesheet.** Liegt in der Vorlage eine Datei für
eine App (`vorlagen/<name>/apps/<app-id>.css`), dann

- holt **die Oberfläche** diese Datei — vom eigenen Webserver, gleiche Herkunft.
  Der Rahmen holt nichts; `connect-src 'none'` bleibt, wie es ist.
- putzt die Oberfläche den Text: jedes `url(...)`, das kein `data:` ist, fliegt
  raus. Der Rahmen würde es ohnehin nicht laden — aber eine Regel, auf die man
  sich verlässt, steht besser zweimal da als einmal.
- hängt die Oberfläche ihn als `data:text/css` **ans Ende des Kopfes** des
  Dokuments, das sie für die App baut. Genau die Form, die `FRAME_CSP` schon
  erlaubt (`style-src data:`). Am Ende, damit er das mitgelieferte Aussehen der
  App überschreibt — das ist der Sinn.

Das passiert **vor** `buildDocument`, also außerhalb der Strecke zwischen Hash
und Lauf. Die Reihenfolge der sieben Schritte bleibt Zeile für Zeile stehen.

## Was eine Vorlage nicht darf, und warum das trägt

- **Kein HTML und kein JavaScript.** Nur `.css` wird gelesen; der Text wird nie
  als Markup in ein Dokument geschrieben und nie als Programm ausgeführt.
  `frame-boot.js` schreibt weiterhin genau **ein** Dokument, genau einmal.
- **Nichts kommt zurück.** Der Weg ist einbahnig: Bytes hinein, nichts heraus.
  CSS hat im Rahmen keinen Rückkanal — jeder Weg nach draußen wäre ein
  `url(...)`, und das ist zweimal zu: durch die Regel des Rahmens und durch das
  Putzen.
- **Die Vorlage sieht die App nicht.** Sie beschreibt, wie etwas aussieht; sie
  liest nichts, was in der App steht, und bekommt keine Antwort darauf, ob eine
  ihrer Regeln überhaupt gegriffen hat.
- **Der Beweis der App bleibt unberührt.** Hash, Vergleich und Lauf über den
  ui-tar ändern sich um keine Zeile. Was geprüft wurde, ist weiter das, was
  läuft — das Stylesheet kommt sichtbar daneben, nicht hinein.

## Was eine Vorlage sehr wohl kann, und das ist entschieden

Mit CSS lassen sich Knöpfe verschieben, verkleinern, überdecken oder ganz
ausblenden — auch ein „Löschen" und auch eine Warnung. Das ist **kein**
Nebeneffekt, den man wegbauen müsste: es ist genau der Grund, warum eine
Vorlage nur ein Verwalter anfassen darf. Die Vorlage ist kein Beiwerk. So steht
es in Punkt 8, und so ist es hier gebaut: wer hochladen darf, ist ein Recht,
und wer es nicht hat, sieht den Weg nicht und kommt ihn nicht.

Der Riegel des Rahmens schützt die **Sitzung** vor der App — Schlüssel,
Speicher, Herkunft. Er war nie dazu da, den Benutzer vor seinem eigenen
Verwalter zu schützen; dafür ist das Recht da.

## Was dem Benutzer bleibt

Eine Vorlage hebt seine eigenen Einstellungen nicht auf:

- **Hell und dunkel** (Punkt 7) hängt an `html[data-theme]`. Eine Vorlage muss
  beide Fälle bedienen; sie kann die Wahl nicht an sich ziehen, weil die
  Oberfläche das Merkmal setzt, nicht das Stylesheet.
- **Die Farbe** steht als `--accent` direkt am `<html>`-Element. Eine Angabe am
  Element schlägt jede Regel eines Stylesheets gleichen Ranges — die Vorlage
  liefert die Voreinstellung, die Wahl des Benutzers bleibt obenauf.
- **Die Dichte** (Punkt 5) ist noch nicht gebaut. Wenn sie kommt, gehört sie
  nach derselben Regel ans Element und nicht ins Stylesheet.

---

# Das Hochladen: was dafür fehlt, und wo

Gebaut und gemessen ist alles, was die **Oberfläche** allein tragen kann: der
Ordner, zwei Vorlagen, die Auswahl im Startmenü unter „Aussehen", die Wahl, die
das Neuladen überlebt, und das Ersetzen des App-Aussehens im Rahmen. Das
Hochladen ist **nicht** gebaut, und zwar nicht aus Zeitmangel, sondern weil
seine schreibende Hälfte in `apps/web/` nicht liegt. Nachgesehen, nicht
vermutet:

1. **Der Webserver weiß nichts von Benutzern.** `public/index.php` nimmt nur
   GET und ist der einzige Einstiegspunkt; `classes/Config.php` liest
   `web.json` (die API-Server und die RP-Angaben) und `trust-anchor.json`.
   Kein Benutzer, keine Rolle, kein Recht. PHP kann also keinen Verwalter von
   irgendwem unterscheiden — ein Recht, das dort geprüft würde, wäre ein
   zweiter Türsteher neben dem von NGC, und der erste wüsste nichts von ihm.
2. **Die Sitzung läuft an PHP vorbei.** Die Oberfläche kennt den Benutzer und
   seine Rechte, aber sie redet über einen Noise-Tunnel mit der API, nicht mit
   dem Webserver. Es gibt nichts, was PHP nachsehen könnte.
3. **Ein Weg über die API braucht eine Zeile im eingefrorenen Vertrag.**
   `contracts/core.js` ACTIONS ist die Abschrift von `shared/core` und sagt
   selbst, dass sie eingefroren ist, Zeile für Zeile. Eine Aktion „Vorlage
   hochladen" ist dort keine, und `core.validRight` lässt ohnehin nur Rechte
   von Apps **aus einem Store** zu (`<app>/<aktion>`, Kern-Apps
   ausgeschlossen) — ein Recht `ngc.users/vorlage-hochladen` lässt sich in
   diesem Vertrag gar nicht schreiben.
4. **`shared/` und `apps/server-api/` gehören dieser Sitzung nicht**
   (`CLAUDE.md` im Stamm: „Fremde Ordner sind tabu", `shared/` ändert nur der
   Koordinator).

**Was es dafür braucht, in einem Satz:** eine Zeile in `shared/core` — eine
Aktion `ngc.users/api/look-upload`, Dienst `api`, `admin: true`,
`changes: true`, Fluss `in` mit einer Obergrenze — und ein Recht derselben
Herkunft. Erst dann hat das Hochladen eine Stelle, an der ein Verwalter von
einem Nicht-Verwalter unterschieden wird, und erst dann ist „ein Benutzer ohne
das Recht kann nichts hochladen" etwas, das man **messen** kann statt es nur im
Browser auszublenden.

Bis dahin ist das Aussehen hier nicht halb gebaut, sondern ganz: wer eine
Vorlage in den Ordner legt, hat sie — und der Weg, auf dem sie dort hinkommt,
ist das, was aussteht.

**Angefordert ist es.** Der Satz steht als **P16** in
`docs/vertraege-v1-3-vorschlag.md` (Nachtrag 24.10.2026), in der Form, die dieses
Dokument für jeden Punkt verlangt: die Zeile, die nachgemessenen Stellen, der Preis und
was ohne sie gilt. Gemessen ist dort auch, warum P16 **nicht** an P10 hängt: eine alte
API weist eine unbekannte Aktion mit `E_NOTFOUND` ab und nicht als kaputten Rahmen.
