# Gestaltung der Oberfläche — entschieden

Thomas hat das im Gespräch am 24.10.2026 entschieden. **Das hier ist keine
Vorschlagsliste.** Künftige Sitzungen berufen sich darauf und stellen es nicht
neu in Frage. Was hier nicht steht, ist nicht entschieden.

Je Punkt: **Entschieden** · **Warum** (eine Zeile) · **Heute** · **Zu bauen**.
„Heute" ist am Quelltext von `main` nachgesehen, Stand 24.10.2026.
Wo Thomas die Frage zurückgegeben hat, ist nach seiner Rangfolge entschieden:
**Stabilität, dann Sicherheit, dann Leistung**. Das steht dann dabei.

Was schon im Entscheidungsbuch (`docs/vertraege-v1.2.md`) steht, gilt weiter und
wird hier nicht wiederholt, nur genannt: ein leerer Zustand sieht nicht wie ein
Angriff aus, die Oberfläche zieht sich selbst nach (nie „bitte neu laden"), und
fremder Text geht als Text in die Seite, nie als Markup.

---

## 1. Das Handy muss gehen

- **Entschieden:** NGC muss auf einem Handy benutzbar sein. Es ist nicht nur für
  den großen Schirm.
- **Warum:** Thomas hat es so entschieden.
- **Dazu gehört seine eigene Annahme, und sie steht hier, weil sie Arbeit
  erspart:** das **erste** Öffnen einer Anlage passiert am PC, nicht am Handy.
  Die Ersteinrichtung muss also nicht fürs Handy gestaltet werden.
- **Heute:** Unter 640 Pixel Breite blendet `shell.css` die Schreibtisch-Symbole
  **aus** (`#icons{display:none}`) und die Namen in der Taskleiste auch. Auf dem
  Handy fehlt damit der Weg zu den Apps.
- **Zu bauen:** Ein Weg zu den Apps, der unter 640 Pixel bleibt. Das Startmenü
  ist schon auf `min(420px, 100vw - 16px)` gestellt und taugt dafür.

## 2. Fenster bleiben Fenster — auch auf dem Handy

- **Entschieden:** Auch auf einem schmalen Schirm sind es **kleine Fenster**, die
  man schieben und ziehen kann. **Nicht** eine App, die den Schirm füllt.
- **Warum:** Thomas hat es so entschieden. Mein Rat war das Gegenteil
  (Vollbild, weil ein Fenster mit dem Finger schwer zu greifen ist); er hat die
  Fenster gewählt, und damit ist es entschieden.
- **Heute:** Ein Fenster darf nicht kleiner werden als 240 × 150 Pixel
  (`--win-min-w`, `--win-min-h`). Auf einem 360 Pixel breiten Handy passt das.
- **Zu bauen, und das ist der Preis dieser Entscheidung:** Ein Fenster darf
  **nicht über den Rand hinausgeschoben** werden können. Sonst ist es weg und
  niemand holt es zurück. Das gilt auf jedem Schirm, auf dem Handy entscheidet
  es aber darüber, ob die Oberfläche benutzbar bleibt.

## 3. Der erste Schreibtisch ist nicht leer

- **Entschieden:** Der leere Schreibtisch ist der **Ausnahmefall**, nicht der
  erste Eindruck. Der Verwalter aktiviert die Standard-Apps der Firma, bevor
  jemand sich anmeldet — wer sich anmeldet, hat Apps.
- **Warum:** Thomas hat auf den Betriebsweg hingewiesen, und der Weg stimmt:
  Apps werden für die Anlage aktiviert, nicht von jedem einzeln geholt.
- **Für den Ausnahmefall entschieden (nach Rangfolge):** ein Satz und ein Knopf
  („Hier ist Platz für Ihre Apps." · „Store öffnen"). **Nicht** ein Store, der
  sich beim Anmelden von selbst öffnet.
- **Warum:** **Stabilität entscheidet.** Ein Fenster, das beim Anmelden von
  selbst aufgeht, ist eine Sonderregel im Anmeldeweg — und Sonderregeln im
  Anmeldeweg brechen als erste. Sicherheit und Leistung werden nicht gebraucht.

## 4. Fehler stehen als Popup mittig über ihrem Fenster

- **Entschieden:** Eine Fehlermeldung ist ein **Popup, mittig**, und es deckt
  **nur das Fenster der App**, in der der Fehler entstanden ist. Mit einem Knopf,
  der es wegnimmt.
- **Warum:** Thomas hat das Popup gewählt; die Reichweite ist sein Zuschlag zu
  meinem Rat. Hustet der Store, läuft die Uhr daneben weiter und bleibt
  anklickbar. Ein Popup über allem stellt die ganze Anlage wegen einer App still.
- **Die eine Ausnahme, und sie ist gewollt:** der **Alarm der Vertrauensliste**
  deckt alles, auch eine offene Frage, und hat nichts zu drücken
  (`.alarmback`, `shell/trustlist.js`). Das ist genau sein Zweck und bleibt.
- **Heute:** Es gibt beides und gemischt — einen Streifen unten am Schirmrand
  (`.noticebar`), einen Kasten über dem Schirm (`.confirmback`) und eine Frage,
  die ihr eigenes Fenster deckt (`.usersapp .askback`). Die Frage im eigenen
  Fenster ist die Form, die jetzt gilt.
- **Zu bauen:** Was heute über den Streifen unten läuft, geht in das Fenster, zu
  dem es gehört. Der Streifen bleibt für das, was **keiner App** gehört.

## 5. Eng oder luftig stellt der Benutzer selbst ein

- **Entschieden:** Abstände und Schriftgröße sind **einstellbar**, pro Benutzer.
- **Warum:** Thomas hat es so entschieden. Am Handy und am großen Schirm ist die
  richtige Dichte nicht dieselbe, und am Schirm sitzen verschiedene Augen.
- **Vorgabe ist die heutige Dichte** (14 Pixel Grundschrift, Zeilen mit 9 bis 11
  Pixel Polster). **Warum:** **Stabilität** — was niemand umstellt, bleibt so,
  wie es gemessen und geprüft wurde.
- **Eingestellt wird im Startmenü unter „Aussehen"**, wo Farbe und Tapete schon
  stehen (`#smlook`). **Warum:** Stabilität — kein neuer Ort, keine neue Ablage.
- **Zu bauen:** Drei Stufen (eng · wie heute · luftig) als Größen in den Marken
  von `shell.css`, damit kein Fenster und keine App eine eigene Zahl bekommt.
  Die Wahl wird gemerkt wie das Thema (`shell/theme.js`).

## 6. Am ersten Blick erkennt man die eigene Anlage

- **Entschieden:** Oben auf der Anmeldung steht der **Name der Firma**, mit ihrem
  Zeichen. **NGC steht klein unten** („läuft mit NGC 0.2.1").
- **Warum:** Thomas hat es so entschieden. Wer sich anmeldet, meldet sich bei
  seiner Firma an, nicht bei einem Produkt.
- **Heute:** Die Anmeldung trägt **keinen** Namen (`.loginbox`, `.loginhead`), und
  `/config.json` liefert auch keinen: darin stehen Fassung, Anker,
  Anmelde-Gegenstelle und die APIs — **kein Name und kein Zeichen der Anlage**.
- **Zu bauen:** Ein Name und ein Zeichen für die Anlage. Beides wird in der
  Oberfläche gesetzt und geht als **Text** hinein, nie als Markup — siehe den
  Grundsatz im Entscheidungsbuch. Wo es gespeichert wird und wer es ändern darf,
  ist **hier nicht entschieden** und gehört in einen eigenen Auftrag.

## 7. Hell und dunkel: das System gibt vor, der Benutzer darf überstimmen

- **Entschieden:** Vorgabe ist die **Einstellung des Betriebssystems**. Dazu eine
  **eigene Wahl** des Benutzers, die die Vorgabe überstimmt.
- **Warum:** Thomas hat es so entschieden. Wer sein System auf hell stellt, meint
  es auch für NGC; wer es anders will, stellt es um.
- **Heute:** Die Oberfläche kennt nur **zwei** Werte (`THEMES = ["dark","light"]`
  in `shell/theme.js`). „Dem System folgen" ist **keine Wahl**, sondern nur der
  Fall, in dem noch nichts gewählt wurde oder der Browser nichts merken kann —
  dann antwortet `@media (prefers-color-scheme: light)` in `shell.css`. Wer einmal
  hell gewählt hat, bleibt hell, auch wenn sein System auf dunkel umstellt.
- **Zu bauen:** Eine **dritte** Wahl „dem System folgen", und sie ist die
  **Vorgabe**. Die beiden anderen bleiben, wie sie sind. Die Oberfläche zieht
  sich nach, wenn das System umstellt (`matchMedia`-Horcher) — nie
  „bitte neu laden", siehe den Grundsatz im Entscheidungsbuch.

## 8. Vorlagen wie bei Joomla und PrestaShop

- **Entschieden:** Es gibt einen **Vorlagen-Ordner** unter dem Webserver. Jede
  Vorlage ist ein Ordner darin und bringt das ganze Aussehen mit. Der Benutzer
  **wählt** eine aus, mehr nicht. **Hochladen darf nur ein Verwalter.**
- **Warum:** Thomas hat es so entschieden, nach dem Vorbild von Joomla und
  PrestaShop. Und sein Punkt trägt: weil nur ein Verwalter hochlädt, liegt die
  Vorlage unter dem eigenen Webserver — damit ist es **kein fremdes CSS**, und
  die Sicherheitsregel der Seite (`style-src 'self'`, `apps/web/classes/Page.php`)
  bleibt **zu**. Sie muss für Vorlagen nicht aufgebohrt werden.
- **Eine Vorlage darf auch das Aussehen einer App ersetzen.** Liegt in der
  Vorlage ein Ordner für eine App — zum Beispiel `terminal` —, wird **sein**
  Aussehen geladen statt des mitgelieferten der App.
- **Heute:** Es gibt keine Vorlagen. Was es gibt, ist die Grundlage dafür: alle
  Farben, Ecken, Schatten und die Mindestgröße eines Fensters stehen als Marken
  an **einer** Stelle (`shell.css`, oben), und Farbe und Tapete sind schon
  wählbar (`shell/theme.js`).
- **Zu bauen:** Der Ordner, die Auswahl im Startmenü unter „Aussehen", das
  Hochladen im Benutzer-und-Rechte-Fenster, und ein Recht dafür.
- **Und ein Punkt, der vor dem Bauen zu klären ist, nicht hier entschieden:**
  eine App läuft heute in einem Rahmen mit **eigenem Ursprung**, damit nichts von
  der Oberfläche in sie hineinreicht (`shell/appframe.js`, Schritt 7). Das
  Ersetzen des App-Aussehens reicht genau dort hinein. Wie weit eine Vorlage in
  eine App darf, ohne diesen Riegel zu lösen, ist eine Frage für den Bau-Auftrag.
  Dazu gehört: ein Verwalter, der eine Vorlage hochlädt, kann mit ihr Knöpfe
  verschieben oder verdecken — die Vorlage ist also **kein** Beiwerk, sondern
  etwas, das nur ein Verwalter anfassen darf. Genau so ist es entschieden.

---

## Wie geredet und geschrieben wird

- **Klartext, kein Fachwort, wo ein deutsches Wort reicht:** „speichern" und
  „laden", nicht „exportieren" und „importieren".
- **Eigennamen werden nicht übersetzt.** Der **Store** heißt Store, nicht
  „Laden". Thomas hat das ausdrücklich gesagt. Dasselbe gilt für NGC und für
  Namen von Apps. Die Klartext-Regel gilt für **Tätigkeiten und Dinge**, nicht
  für Namen.
- **Ein Fehlertext sagt, was fehlt**, nicht, dass etwas kaputt ist. Die zwei
  Worte dieser Oberfläche für einen Defekt — „nicht in Ordnung" und „antwortet
  gerade nicht" — stehen **nie** in einem leeren Zustand. Das ist im
  Entscheidungsbuch entschieden und durch einen Fall gemessen.

## Was hier nicht entschieden ist

- **Die Zeichen der Apps.** Eigener Auftrag, eigene Sitzung, eigener Satz SVGs.
- **Wo der Name der Anlage gespeichert wird** (siehe Punkt 6).
- **Was aus den Schreibtisch-Symbolen auf dem Handy wird** — dass es einen Weg
  geben muss, steht in Punkt 1; welchen, nicht.
