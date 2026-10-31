# Aussehen nach LordOS „Lord Flow"

Thomas' Auftrag: die NGC-Oberfläche soll aussehen und sich anfühlen wie macOS, und das Design
kommt aus **LordOS**, das er lizenziert hat.

Dieses Blatt ist die **Beschreibung**, nicht der Bau. Es nennt jede Zahl, jede Farbe und jede
Bewegung einmal und sagt bei jeder, **woher sie kommt**. Erst danach wird umgebaut, in kleinen
Schritten, je Schritt ein eigener PR.

Was Thomas am 24.10.2026 über die Oberfläche entschieden hat, steht in `docs/gestaltung.md` und
gilt weiter. Dieses Blatt widerspricht ihm nicht; wo es daran rührt, steht es unter
„Was aus den Entscheidungen folgt".

---

## 1. Die Quelle und ihre Lizenz

| | |
|---|---|
| Projekt | LordOS |
| Ort | `https://github.com/lordbasex/LordOS`, Zweig `main` |
| Was es ist | eine GNU/Linux-Verteilung auf Debian mit eigener Arbeitsumgebung: Anzeigeserver und Wayland-Compositor in C, Oberfläche in React auf QuickJS |
| Das Design | der Satz `themes/default/`, Name **„Lord Flow"**: Menüband oben, Dock mit Lupe, App-Starter, Kontrollzentrum |
| Lizenz des Codes | **MIT**, Datei `LICENSE`: „Copyright (c) 2026 iPERFEX and the LordOS contributors" |
| Marke und Webseite | **nicht** frei. `lordos.org` behält sich Name, Marke und Inhalte vor |
| Gemessen am | 30.10.2026, Stand `pushed_at` 2026-10-30 |

**MIT ist mit der AGPL-3.0 vereinbar.** MIT stellt nur zwei Bedingungen: der Lizenztext und der
Urheberrechtsvermerk müssen bei jeder Kopie dabei sein. Beides trägt die AGPL mit; MIT-Code darf in
ein AGPL-Werk aufgenommen und als Ganzes unter AGPL weitergegeben werden.

### Was wir übernehmen dürfen und was nicht

| | |
|---|---|
| Code aus `themes/default/` | **ja**, MIT. Er ist React auf QuickJS, kein Browser-Code — wir übernehmen daraus die **Zahlen und die Anordnung**, nicht die Dateien |
| Die Zahlen dieses Blatts | **ja**. Maße und Farbwerte sind Tatsachen, kein geschütztes Werk; sie stehen hier trotzdem mit Herkunft, damit nachprüfbar ist, was von wo kommt |
| Schriften Inter und JetBrains Mono | **ja**, SIL OFL 1.1 (LordOS `THIRD_PARTY.md`). OFL 1.1 ist mit der AGPL vereinbar |
| Schrift Plus Jakarta Sans | **nein**. LordOS benutzt sie für das **eigene Wortzeichen**. Die Schrift wäre frei, der Gebrauch ist Marke |
| Die elf Tapeten `assets/wallpapers/*.jpg` | **nein.** `assets/wallpapers/README.md`, Zeile 3: „Created by iPERFEX with an AI image generator; **part of the LordOS brand assets**." Damit sind sie Marke, nicht Code — der MIT-Vermerk deckt sie nicht |
| Die vier Verläufe `themes/default/assets/*.png` | **ja**, aber wir brauchen sie nicht als Datei. Sie werden von `tools/gen-wallpaper.py` **erzeugt** — ein Python-Skript im Repo, nur Standardbibliothek, die vier Farbsätze stehen als Zahlen darin. Eigenes Werk, MIT. Ein Verlauf braucht im Browser ohnehin keine Datei, sondern `linear-gradient` |
| Name „LordOS", „Lord Flow", „Lumo", das Maskottchen, die Krone | **nein**. Marke |
| Schriften und Marken von Apple | **nein**, und zwar gar nicht. Keine San Francisco, kein Apfel, keine Namen wie „Finder" oder „Launchpad" |

**Bevor die erste Datei aus LordOS in NGC liegt**, bekommt sie eine Zeile in `THIRD-PARTY.md`
(Modul, Fassung, Lizenz, wofür, Übernahmedatum) — so wie jeder andere Fremdcode dort. Solange nur
Zahlen aus diesem Blatt umgesetzt werden, ist keine Zeile fällig: es liegt keine fremde Datei im
Baum.

### Was noch offen ist

Die Leitsitzung hat gemeldet, dass auf Thomas' Proxmox eine VM 208 `lordosVPS` steht (angehalten).
**Sie wird nicht angefasst.** Ob das eine andere, weitergehende Lizenz ist als die öffentliche MIT,
fragt die Leitsitzung bei Thomas. Bis zu seiner Antwort gilt für dieses Blatt allein die öffentliche
MIT-Lizenz — und die reicht für alles, was hier steht.

---

## 2. Eine Zahl steht an einer Stelle

Alle Zahlen dieses Blatts werden **CSS-Marken** in `apps/web/public/assets/css/shell.css`, im Block
`:root` ganz oben.

### Und genau das stimmt heute nicht — gemessen am 30.10.2026

Hier stand zuerst „das ist heute schon so und bleibt so: keine Regel und keine App bekommt eine
eigene Zahl". Das war falsch, und es ist beim ersten Umbauschritt aufgefallen.

**Solange jede App ihre eigene Kopie mitbringt, ist „eine Zahl nur an einer Stelle" in diesem Repo
nicht wahr.**

Belegt am Akzent, der sichtbarsten Farbe. Dieselbe Farbe steht an **sechs** Orten:

| # | Ort | Was dort steht |
|---|---|---|
| 1 | `shell.css`, `--accent` | die Vorgabe |
| 2 | `shell/theme.js`, `ACCENTS` | die **sechs**, aus denen der Benutzer aussucht |
| 3 | `shell/desktop.js`, `ACCENT_NAMES` | ihre Namen, fest verdrahtet auf Deutsch |
| 4 | `public/assets/ngc.js` | ein Rückfallwert |
| 5 | `tests/shell/desktop-look.spec.js` | die Erwartung `#2f7de1` |
| 6 | `assets/apps/calculator/app.css`, `terminal/app.css` | **je eine ganze Kopie** von `--win-bg`, `--win-fg`, `--win-border`, `--title-bg`, `--field-bg`, `--muted`, `--accent` |

Gemessen: das LordOS-Blau allein in `shell.css` machte einen Prüffall rot und ließ die „="-Taste
des Rechners im alten Blau stehen.

**Nummer 6 ist der schwerwiegende, und er ist kein Versehen.** Eine App läuft in einem Rahmen mit
**eigenem Ursprung** (`shell/appframe.js`, Schritt 7). Sie kann `shell.css` nicht lesen, und sie
soll es auch nicht können — es ist eine Sicherheitsschranke. Nach der Rangfolge steht Sicherheit
über allem, also wird dieser Riegel **nicht** gelockert, damit Farben hineinkommen. Was gehen kann,
ist das Gegenteil: ein festes, geprüftes Häufchen Werte in den Rahmen **geben** — keine Vorlage und
kein Stylesheet aus der Oberfläche. Das ist ein eigener Entwurf mit eigener Messung und fällt unter
`docs/gestaltung.md` Punkt 8, wo die Frage „wie weit eine Vorlage in eine App darf" ausdrücklich
als offen steht.

Für dieses Blatt heißt das: Wo unten „wird eine Marke in `shell.css`" steht, gilt das für die
**Oberfläche** — Schreibtisch, Leiste, Menü, Fensterrahmen. Die **Inhalte** der Apps erreicht es
nicht. Ein Fenster in LordOS-Farben, dessen Inhalt in den alten Farben steht, ist kein fertiges
Aussehen.

Zweite Doppelung, kleiner, aber derselbe Fehler: die **hellen** Werte stehen zweimal in
`shell.css` — einmal unter `html[data-theme="light"]` und einmal im Block
`@media (prefers-color-scheme: light)`. CSS lässt sich nicht so schreiben, dass beide Wähler einen
Block teilen, also müssen die Werte vorerst doppelt gepflegt werden. Eigener Schritt, eigene
Messung.

Die Marken heißen weiter wie heute, wo es sie schon gibt (`--bar-h`, `--radius`, `--accent`,
`--win-bg`, `--fs`, `--pad`, `--win-min-w`, `--win-min-h`, `--alarm` …). Neue Marken kommen dazu;
**keine wird umbenannt**, denn `window.js`, `theme.js` und jede App lesen die heutigen Namen.

Hell und dunkel: dunkel sind die Werte in `:root`, hell überschreibt
`html[data-theme="light"]` — genau der Aufbau, der heute im Blatt steht. Dazu der Block
`@media (prefers-color-scheme: light)` für den Fall ohne JavaScript. Dichte (eng / normal / luftig)
rührt nur an `--fs` und `--pad`, wie heute.

---

## 3. Farben

Spalte **Quelle**: `LordOS` = aus `themes/default/tokens.js`, Zeile für Zeile abgelesen.
`gewählt` = von mir gewählt, weil LordOS an dieser Stelle keinen Wert hat.

### Die Akzentfarben, die der Benutzer wählen darf

**Entschieden von Thomas am 30.10.2026, wörtlich:**

> FARBEN: bleiben, wie sie sind. Die sechs NGC-Farben — Blau, Türkis, Pink, Orange, Violett, Grün —
> werden NICHT durch die LordOS-Palette ersetzt. Werte unverändert. Damit ist der Akzent-Schritt vom
> Tisch: nimm aus LordOS nur Form, Flächen, Ränder, Abstände, Schatten, Ecken — keine Farbwerte.

Es bleiben also die sechs, die NGC hat, mit den Werten, die NGC hat:

| Name | Wert | Quelle |
|---|---|---|
| Blau (Vorgabe) | `#2f7de1` | NGC, `shell/theme.js` |
| Türkis | `#16d1c3` | NGC |
| Pink | `#e14f8a` | NGC |
| Orange | `#f0a020` | NGC |
| Violett | `#8b5cf6` | NGC |
| Grün | `#22c55e` | NGC |

Die LordOS-Palette (`accents.azul` `#0a84ff`, `violeta` `#bf5af2`, `rosa` `#ff375f`, `naranja`
`#ff9f0a`, `verde` `#30d158`, `grafito` `#8e8e93`) steht hier nur noch als Quellenangabe. **Sie wird
nicht eingebaut.** Was aus LordOS kommt, ist Form, Fläche, Rand, Abstand, Schatten, Ecke.

Die sechs Namen ziehen aus `shell/desktop.js` in die fünf Sprachdateien um — **Namen unverändert,
Werte unverändert**. Das ist ein Umzug, keine Änderung.

Damit entfällt der Akzent-Schritt, der in Abschnitt 2 als eigener Schritt für sechs Stellen stand.
Die sechs Stellen bleiben ein Befund über das Repo; eine Farbe, die durch alle sechs muss, gibt es
nicht mehr.

### Flächen und Schrift

| Marke | dunkel | hell | Quelle |
|---|---|---|---|
| `--win-fg` Schrift | `#f5f5f7` | `#1d1d1f` | LordOS `text` |
| `--muted` zweite Schrift | `rgba(235,235,245,.55)` | `rgba(60,60,67,.6)` | LordOS `textDim` |
| `--win-bg` Fensterfläche | `#1f1f23` | `#ffffff` | LordOS `windowBg` |
| `--side-bg` Seitenspalte | `#26262b` | `#f3f3f6` | LordOS `sidebarBg` |
| `--line` Haarlinie | `rgba(255,255,255,.08)` | `rgba(0,0,0,.08)` | LordOS `separator` |
| `--hover` unter dem Zeiger | `rgba(255,255,255,.07)` | `rgba(0,0,0,.05)` | LordOS `hover` |
| `--control` Knopffläche | `#3a3a40` | `#e3e3e8` | LordOS `control` |
| `--field-bg` Eingabefeld | `#2a2a2f` | `#ffffff` | LordOS `inputBg` |
| `--bar-bg` Menüband | `rgba(24,24,30,.42)` | `rgba(250,250,252,.55)` | LordOS `menuBg` |
| `--menu-bg` aufgeklapptes Menü | `rgba(36,36,42,.72)` | `rgba(246,246,248,.78)` | LordOS `panelBg` (mit Effekten) |
| `--menu-bg-flat` ohne Effekte | `#2a2a30` | `#f2f2f5` | LordOS `panelBg` (ohne Effekte) |
| `--menu-hover` Menüzeile | `rgba(255,255,255,.16)` | `rgba(0,0,0,.08)` | LordOS `menuHover` |
| `--win-border` Fensterrand | `rgba(255,255,255,.12)` | `rgba(0,0,0,.14)` | LordOS `windowBorder` |
| `--title-bg` Titelzeile | `#2b2b30` | `#ececef` | LordOS `titleBg` |
| `--dock-bg` Dock | `rgba(46,46,56,.42)` | `rgba(255,255,255,.38)` | LordOS `dockBg` |
| `--dock-border` Dock-Rand | `rgba(255,255,255,.18)` | `rgba(255,255,255,.55)` | LordOS `dockBorder` |
| `--off` Schalter aus | `#4a4a50` | `#c8c8cc` | LordOS `lightOff` |
| `--scroll` Rollbalken | `rgba(255,255,255,.32)` | `rgba(0,0,0,.3)` | LordOS `scrollThumb` |
| `--scroll-hover` | `rgba(255,255,255,.5)` | `rgba(0,0,0,.45)` | LordOS `scrollThumbHover` |
| `--page-bg` Schreibtisch unter der Tapete | `#0d1017` | `#e7ecf4` | heute in NGC, bleibt |
| `--alarm` | `#ff7b72` | `#b3261e` | heute in NGC, bleibt |
| `--alarm-veil` | `rgba(6,9,16,.86)` | `rgba(22,32,46,.62)` | heute in NGC, bleibt |

`--title-bg-active` hat LordOS nicht: dort trägt der Unterschied zwischen vorn und hinten der
**Schatten**, nicht die Farbe der Titelzeile (siehe Punkt 6). Die Marke bleibt vorerst stehen und
wird gleich `--title-bg` gesetzt; sie fällt weg, wenn nachgesehen ist, wo sie noch gelesen wird.

### Die drei Knöpfe der Titelzeile

**Erst eine Korrektur dieses Blattes, gemessen am 30.10.2026.** Hier stand, NGC habe die drei runden
Lichter von LordOS, 13 × 13 Pixel. **Das hatte NGC nie.** Die drei Knöpfe sind `.wbtn` in
`shell.css`, sie tragen Text und kein Licht, und sie waren **44 breit und 36 hoch**. Ein Blatt, das
eine Vorlage beschreibt, die im eigenen Code nicht vorkommt, hätte den Umbau auf eine Annahme
gestellt.

**Entschieden von Thomas am 30.10.2026, wörtlich:**

> FENSTERKNÖPFE: 40 px. Nicht 13 px. Das ist eine Entscheidung gegen die LordOS-Vorlage, also als
> Abweichung mit Begründung (Finger) ins Blatt.

**Abweichung von LordOS, mit Begründung.** LordOS zeichnet drei runde Lichter von 13 × 13 Pixeln;
das ist eine Maus-Größe. NGC wird mit dem Finger bedient, und ein Finger braucht ungefähr 40. Also
40 × 40.

Die 40 steht **einmal**: in der Breite von `.wbtn`. Die Höhe kommt aus der Titelzeile, und die
Titelzeile steht darum auf genau 40. Zwei Zahlen 40 hinzuschreiben wäre dieselbe Entscheidung an
zwei Stellen.

| | Wert | Quelle |
|---|---|---|
| Größe | **40 × 40 px** | Thomas, 30.10.2026 — Abweichung von LordOS (13 × 13) |
| Form | Text, kein rundes Licht | NGC, wie gehabt |
| Schließen / Kleiner / Größer in Farbe | **nicht übernommen** | LordOS `Window.jsx`: `#ff5f57`, `#febc2e`, `#28c840` — Farbwerte, und Farbwerte kommen nach Thomas' Entscheidung 3 nicht aus LordOS |

---

## 4. Abstände: ein Raster von 8 Pixeln

Aus LordOS `docs/DESIGN.md`, unverändert übernommen.

| Marke | px | Wofür |
|---|---|---|
| `--sp-xs` | 4 | zwischen einem Zeichen und seiner Aufschrift, zwischen Knöpfen einer Gruppe |
| `--sp-s` | 8 | zwischen Bedienteilen einer Zeile, zwischen Zeilen einer Liste |
| `--sp-m` | 12 | seitlich in der Werkzeugzeile, innen in einem gerahmten Kasten |
| `--sp-l` | 16 | Polster von Feldern und Dialogen **in** einem Fenster |
| `--sp-xl` | 20 | **Innenabstand des Fensterinhalts** — nichts berührt den Fensterrand |
| `--sp-xxl` | 24 | zwischen Gruppen und Abschnitten |

Regeln, ebenfalls aus LordOS:

- Der Inhalt eines Fensters beginnt 20 Pixel vom Rand. Listen in einer Seitenspalte 10 Pixel, mit
  8–10 Pixeln Polster je Zeile.
- Gruppen trennen 20–24 Pixel, Bedienteile 8–12. **Nie zwei Bedienteile, die sich berühren.**
- Trennen heißt **Haarlinie**, 1 Pixel, `--line`: zwischen Seitenspalte und Inhalt, unter der
  Werkzeugzeile, über der Statuszeile. Keine dicken Ränder.

Eine Haarlinie, die zu einem Kasten gehört, ist ein **Rand dieses Kastens**
(`border-bottom:1px solid var(--line)`), kein eigenes Element. Ein Rand liegt innen: wer eine feste
Höhe hat, rechnet 1 Pixel dazu.

---

## 5. Schrift

LordOS benutzt **Inter** in 400 / 500 / 600 (700 nur für sehr kurze Betonung) und **JetBrains Mono**
für Terminal und Code. Beide SIL OFL 1.1, beide dürfen wir mitbringen.

Heute steht in NGC `font: var(--fs)/1.45 "Segoe UI", system-ui, -apple-system, Roboto, sans-serif`.
Das wird `Inter`, mit derselben Kette dahinter, falls die Datei fehlt. **Keine Apple-Schrift**:
`-apple-system` fällt aus der Kette.

Die Größen aus LordOS `docs/DESIGN.md`:

| Marke | px | Gewicht | Wofür |
|---|---|---|---|
| `--fs-caption` | 11 | 400–600 | zweite Aufschriften, Abschnittsköpfe in Seitenspalten, Statuszeilen, Zahlen |
| `--fs-callout` | 12 | 400 | Hinweise, zweiter Text in einer Zeile |
| `--fs` | 13 | 400–500 | **Grundschrift**: Text, Listenzeilen, Knöpfe, Menüs |
| `--fs-headline` | 15 | 600 | Titel eines Dialogs, Kopf eines leeren Zustands |
| `--fs-title` | 17 | 600 | Titel eines Felds, wenn der Platz knapp ist |
| `--fs-large` | 22 | 600 | Titel einer Übersicht |

**Hier weiche ich von LordOS ab, und es ist eine Wahl, keine Messung:** `--fs` steht heute in NGC
auf 14 Pixel, LordOS hat 13. Ich lasse **14** stehen. Grund: `--fs` ist in NGC die Marke, an der die
Dichte hängt (`docs/gestaltung.md` Punkt 5), und 14 Pixel sind das, was gemessen und geprüft wurde.
Die Stufen bleiben `12,5` eng und `15,5` luftig. Wer das auf 13 ziehen will, zieht eine Zahl an
einer Stelle — aber dann sind alle Messungen der Oberfläche neu zu machen.

Zweiter Text nimmt `--muted`. **Keine Großbuchstaben** in Abschnittsköpfen: „Inhalt", nicht
„INHALT".

---

## 6. Ein Fenster

Alle Zahlen aus LordOS `themes/default/components/Window.jsx`.

| | Wert | Quelle |
|---|---|---|
| Ecken | 12 px (`--radius`) | LordOS. Heute steht in NGC 10 |
| Ecken, wenn das Fenster den Schirm füllt | 0 | LordOS |
| Höhe der Titelzeile | 40 px (`--title-h`) | LordOS `TITLE_H` |
| Titel | mittig, Zeilenhöhe = Titelhöhe | LordOS |
| Die drei Knöpfe | 13 × 13 px, Ecke 7 px, links, 14 px vom Rand | LordOS |
| Reihenfolge | Schließen, Kleiner, Größer — **von links** | LordOS |
| Haarlinie unter der Titelzeile | 1 px, `--line` | LordOS |
| Rand | 1 px, `--win-border` | LordOS |
| Schatten **vorn** | `0 24px 56px rgba(0,0,0,.55)` | LordOS |
| Schatten **hinten** | `0 10px 28px rgba(0,0,0,.32)` | LordOS |
| Kleinste Größe | 240 × 150 px (`--win-min-w`, `--win-min-h`) | heute in NGC, **bleibt** |

**Vorn und hinten unterscheidet der Schatten**, nicht die Farbe. Das Fenster vorn liegt höher: ein
doppelt so weicher und doppelt so weit geworfener Schatten.

Heute hat NGC `--shadow: 0 18px 50px rgba(0,0,0,.55), 0 2px 6px rgba(0,0,0,.4)` — eine Marke für
beide Fälle. Daraus werden zwei: `--shadow` (vorn) und `--shadow-back` (hinten).

Für hell rechne ich die beiden Schatten um, denn ein Schwarz mit 55 Prozent über einer hellen Seite
ist ein Fleck, kein Schatten. **Gewählt**, LordOS unterscheidet hier nicht:
`--shadow: 0 24px 56px rgba(30,45,70,.22)`, `--shadow-back: 0 10px 28px rgba(30,45,70,.13)`.

---

## 7. Das Menüband oben

Aus LordOS `themes/default/index.jsx` und `components/MenuBar.jsx`.

| | Wert | Quelle |
|---|---|---|
| Höhe | **28 px** (`--bar-h`) | LordOS `MENU_H` |
| Fläche | `--bar-bg`, durchscheinend | LordOS |
| Unschärfe dahinter | 24 px (`--blur`), 0 wenn Effekte aus | LordOS `blur` |
| Polster seitlich | 8 px | LordOS |
| Ein Eintrag | 22 px hoch, 9 px seitlich, Ecke 5 px | LordOS |
| Schrift im Band | 13 px, Gewicht 500; der Name der App vorn 700 | LordOS |
| Aufgeklapptes Menü | 260 px breit, 2 px unter dem Band | LordOS |

**Korrektur, am Bild und am Quelltext nachgesehen (30.10.2026):** Hier stand, NGC habe die Leiste
**unten**. Das ist falsch. `#taskbar` ist `position:fixed; top:0` mit `backdrop-filter:blur(18px)
saturate(150%)` — die Leiste sitzt **schon oben und ist schon durchscheinend mit Unschärfe
dahinter**. NGC ist damit näher an Lord Flow, als dieses Blatt zuerst behauptet hat.

Was an der Leiste wirklich zu tun bleibt, ist deshalb weniger:

- die Höhe von `--bar-h: 44px` auf **28**,
- aus dem einen „Start"-Knopf werden die Menüs der App, die vorn steht,
- und ein **Dock unten** kommt dazu, das es heute gar nicht gibt.

Das Dock ist damit der einzige wirkliche Bruch mit dem heutigen Aufbau — und es ist der Teil, der
an Thomas' Antwort zum Handy hängt.

---

## 8. Das Dock

Aus LordOS `docs/DOCK.md` (`DOCK_DEFAULTS`) und `components/Dock.jsx`.

### Die Größe: entschieden, dann gerechnet

**Entschieden von Thomas am 30.10.2026, wörtlich:**

> DOCK: kleiner als LordOS. Die Zahl ist nicht gesetzt — nimm die größte, bei der auf 360 px Breite
> genug Zeichen Platz haben, und schreib die Rechnung daneben. Keine Zahl ohne Messung.

Gerechnet, mit dem Raster aus Abschnitt 4: 8 Pixel zwischen zwei Zeichen, 8 Pixel Polster an jeder
Seite. Für `n` Zeichen der Größe `S`:

    Breite = 16 + n · S + 8 · (n − 1)

| Größe `S` | passt auf 360 px | gerechnet |
|---|---|---|
| 54 (LordOS) | 5 Zeichen | 16 + 270 + 32 = 318 — bei 6 wären es 380 |
| 48 | 6 Zeichen | 16 + 288 + 40 = 344 — bei 7 wären es 400 |
| **40** | **7 Zeichen** | 16 + 280 + 48 = 344 — bei 8 wären es 392 |

**Die Zahl ist 40 px**, und sie ist die Untergrenze und das Ergebnis zugleich:

- Sie ist die **größte**, die sieben Zeichen auf ein Handy von 360 Pixeln bringt.
- Sie ist **nicht gewählt**: 40 ist dieselbe Fingergröße, die Thomas am selben Tag für die
  Fensterknöpfe entschieden hat. Kleiner als 40 geht nicht, ohne diese Entscheidung zu brechen.
- Sie ist **kleiner als LordOS** (54), wie verlangt.

Damit steht die 40 in diesem Blatt für zweierlei dasselbe: Fensterknopf und Dock-Zeichen. Es ist
eine Zahl, nicht zwei — die Größe einer Fingerspitze.

Die Lupe behält das Verhältnis von LordOS statt einer zweiten eigenen Zahl: 86 ÷ 54 = 1,59, also
**64 px** unter dem Zeiger (40 × 1,6).

**Was hier noch nicht entschieden ist:** was „genug" über sieben hinaus heißt, wenn NGC mehr Apps
bekommt, als ins Dock passen. Das Dock ist dann eine Auswahl und keine Liste — die Frage gehört zum
Dock-Schritt, nicht hierher. Heute hat NGC zwei Apps.

### Was LordOS vorgibt

| | Vorgabe | Bereich | Quelle |
|---|---|---|---|
| Lage | unten | unten, links, rechts | LordOS |
| Größe je Zeichen | ~~54 px~~ **40 px** (`--dock-size`) | 36–80 | Thomas, 30.10.2026 (gerechnet, s. o.) |
| Lupe | ein | ein / aus | LordOS |
| Größtes Zeichen unter dem Zeiger | ~~86 px~~ **64 px** (`--dock-magnify`) | bis 128 | 40 × 1,6, Verhältnis aus LordOS |
| Von selbst verstecken | aus | ein / aus | LordOS |
| Die letzten benutzten Apps | aus (dann 3, nach einer Trennlinie) | | LordOS |
| Fläche, Rand | `--dock-bg`, 1 px `--dock-border` | | LordOS |
| Punkt unter einer offenen App | ja | | LordOS |

So arbeitet es, aus LordOS abgelesen:

- **Lupe:** Das Zeichen unter dem Zeiger wächst von 40 auf 64 Pixel, die Nachbarn weniger (in
  LordOS 54 auf 86; das Verhältnis ist dasselbe). Bei
  „Bewegung verringern" bleibt sie aus.
- **Ziehen:** Ein Zug beginnt erst nach **6 Pixeln** — ein Klick verschiebt also nie etwas. Die
  Nachbarn weichen in **150 ms** aus. Die Lupe hält an, solange gezogen wird.
- **Entfernen:** Mehr als **etwa 60 Pixel** vom Dock weg ziehen und einen Augenblick halten; dann
  verschwindet es mit einer kleinen Wolke (**320 ms**).
- **Von selbst verstecken:** Es kommt nach **120 ms** am Rand und geht **500 ms** nachdem der Zeiger
  es verlassen hat (mit 8 Pixeln Spielraum). Das Hereinfahren dauert **180 ms**.
- Menü, Kontrollzentrum und App-Starter liegen **immer über** dem Dock, auf welcher Seite es auch
  steht.

---

## 9. Ecken und Formen der Bedienteile

Aus LordOS `docs/DESIGN.md`, Spalte „Lord Flow".

| Marke | px | Wofür |
|---|---|---|
| `--radius` | 12 | Fenster |
| `--r-control` | 7 | Knopf, Eingabefeld, Suchfeld, Schalterreihe, Zeile einer Seitenspalte |
| `--r-control-s` | 6 | dasselbe in klein, Knopf der Werkzeugzeile |
| `--r-group` | 10 | gerahmter Kasten, Abschnitt in den Einstellungen; ein Dialog 12 |
| `--r-menu` | 8 | Menü, Auswahlliste; ein Kurzhinweis 6 |
| `--r-menu-item` | 5 | eine Zeile in einem Menü |

Und die Höhen, ebenfalls aus LordOS:

| Bedienteil | Höhe | Ecke |
|---|---|---|
| Knopf | 28, klein 22 | 7 / 6 |
| Eingabefeld, Suchfeld | 28 | 7 |
| Schalterreihe | 28, klein 22 | 8 |
| Knopf der Werkzeugzeile | 28, klein 22 | 6 |
| Gruppe solcher Knöpfe | 32 | 8 |
| Werkzeugzeile | 44 | — |
| Zeile einer Seitenspalte | 28 | 6 |
| Schalter | 24, klein 36 × 20 | — |

Dazu vier Formentscheidungen, die Lord Flow von „Lord Classic" unterscheiden und die wir
mitnehmen:

- Die Zeile im Menü unter dem Zeiger wird **mit der Akzentfarbe gefüllt**, Schrift weiß.
- Die gewählte Zeile einer Seitenspalte wird **gefüllt**, nicht mit einem Balken markiert.
- Ein Eingabefeld im Zugriff bekommt einen **Rand** in der Akzentfarbe, keine Linie darunter.
- Ein schlichter Knopf hat **keinen Rand**.

**Je Feld eine Haupthandlung in der Akzentfarbe.** Alles andere schlicht.

---

## 10. Rollbalken

Aus LordOS `docs/DESIGN.md`, Abschnitt „Scroll bars".

- **Über dem Inhalt**, am Rand: ein Balken von **6 px**, **2 px** vom Rand, unter dem Zeiger auf
  **10 px** verbreitert und dunkler. Er nimmt **keinen** Streifen weg — der Inhalt behält sein
  normales Polster, und der Balken liegt darin.
- **Versteckt, bis er gebraucht wird:** Er erscheint beim Rollen und, in der Vorgabe, wenn der
  Zeiger **16 px** an den Rand kommt. Er geht eine Sekunde nach dem letzten Rollen über **200 ms**
  aus. Bei „Bewegung verringern" ohne Übergang.
- **Greifen:** Solange er zu sehen ist, nimmt ein **14 px** breiter Streifen am Rand den Zeiger;
  solange nicht, geht jeder Klick durch (`pointer-events: none`).
- Form: eine Pille (`--r-scroll: 999px`), Farben `--scroll` und `--scroll-hover`.
- **Mindestens 12 px Polster** am Rand, damit der verbreiterte Balken nicht über der Schrift steht.

---

## 11. Bewegungen

Jede Zahl aus LordOS. Alle hängen an **einer** Marke für die Dauer und einer für die Kurve, damit
„Bewegung verringern" an einer Stelle greift.

| Was | Dauer | Quelle |
|---|---|---|
| Dock fährt herein | 180 ms | LordOS `SLIDE_MS` |
| Nachbarn weichen aus | 150 ms | LordOS `REORDER_MS` |
| Zeichen verschwindet | 320 ms | LordOS `POOF_MS` |
| Rollbalken geht aus | 200 ms | LordOS |
| Kurzhinweis erscheint | 600 ms Wartezeit | LordOS (Lord Classic) |
| Dock kommt am Rand | 120 ms | LordOS |
| Dock geht wieder | 500 ms | LordOS |
| Knopf unter dem Finger | Maßstab 0,96 | LordOS |

`--motion: 180ms` als Grundwert, `--ease: cubic-bezier(.25,.1,.25,1)` **gewählt** — LordOS zeichnet
seine Bewegungen in C und nennt keine Kurve.

Bei `@media (prefers-reduced-motion: reduce)` wird `--motion: 0ms`, und die Lupe im Dock bleibt aus.

---

## 12. Was aus Thomas' Entscheidungen folgt

`docs/gestaltung.md` gilt weiter. Vier Punkte treffen dieses Design:

1. **Punkt 1, das Handy muss gehen.** Ein Dock mit 54 Pixeln je Zeichen und ein Band mit 28 Pixeln
   sind für die Maus gebaut. Auf 360 Pixeln Breite passen etwa fünf Zeichen ins Dock. **Das ist zu
   klären, nicht hier entschieden:** entweder das Dock wird auf dem Handy kleiner, oder es rollt
   seitlich, oder der App-Starter übernimmt. Der Weg zu den Apps muss unter 640 Pixeln bleiben —
   das steht in Punkt 1 und wird von diesem Umbau nicht erledigt.
2. **Punkt 2, Fenster bleiben Fenster.** Die kleinste Größe bleibt 240 × 150. Die Titelzeile wird
   von 44 auf **40** Pixel niedriger, die drei Knöpfe von heute auf **13 × 13**. Ein 13 Pixel
   großer Knopf ist mit dem Finger schwer zu treffen; die Mindestgröße ist gegen die drei Knöpfe
   gemessen und wird **nicht** angetastet. **Zu klären:** ob die drei Knöpfe auf einem Zeigegerät
   ohne Maus größer werden.
   **Hinweis für diese Entscheidung, kein Beschluss:** Die Erfahrung aus den Touch-Displays sagt,
   dass unter etwa **40 Pixeln** mit dem Finger nichts verlässlich getroffen wird. 13 Pixel sind
   also nicht knapp, sondern ein Drittel davon.
3. **Punkt 5, eng oder luftig.** Bleibt wie es ist: zwei Marken, `--fs` und `--pad`. Die neuen
   Schriftgrößen aus Punkt 5 dieses Blatts sind **feste Stufen** und hängen nicht an der Dichte —
   sonst wäre eine Zahl an zwei Stellen.
4. **Punkt 8, Vorlagen wie bei Joomla und PrestaShop.** Dieses Design ist **die erste Vorlage**, nicht
   eine Verdrahtung. Deshalb steht jede Zahl als Marke: eine Vorlage, die „Lord Flow" ersetzt,
   überschreibt den `:root`-Block und sonst nichts.

---

## 13. Was dieses Blatt nicht entscheidet

- **Die Zeichen der Apps.** Eigener Auftrag, eigene Sitzung, eigener Satz SVGs. Dieses Blatt sagt
  nur, wie groß sie im Dock stehen (40, unter dem Zeiger 64 — Abschnitt 8).
- **Die Tapeten.** Die elf aus `assets/wallpapers/` sind Marke und fallen weg (Punkt 1). Die vier
  Verläufe sind frei, brauchen aber keine Datei: ob NGC sie als `linear-gradient` nachbaut oder
  eigene bekommt, ist eine Frage für den Bau-Auftrag.
- **Das Kontrollzentrum und der App-Starter.** Lord Flow hat beide; was NGC darin zeigt, ist eine
  eigene Frage.
- **Der Weg aufs Handy** (Punkt 12.1).
- **Ob die VM 208 eine weitergehende Lizenz trägt.** Liegt bei Thomas.

---

## 14. Belege

| Behauptung | Wie nachgesehen |
|---|---|
| LordOS ist MIT | `gh api repos/lordbasex/LordOS/license` → `spdx_id: MIT`; Datei `LICENSE`, Zeile 3: „Copyright (c) 2026 iPERFEX and the LordOS contributors" |
| „Lord Flow" ist der macOS-artige Satz | `themes/default/theme.json`: „Menu bar, magnifying dock, app launcher and Control Center" |
| Die Farben | `themes/default/tokens.js`, Funktion `makeTheme` |
| Fenster und Titelzeile | `themes/default/components/Window.jsx`, `TITLE_H = 40`, Zeile 61 und 64 |
| Menüband | `themes/default/index.jsx`, `MENU_H = 28`; `components/MenuBar.jsx` Zeile 15 und 23 |
| Dock | `docs/DOCK.md`, Tabelle `DOCK_DEFAULTS`; `components/Dock.jsx` Zeile 18–21, 192 |
| Abstände, Schrift, Bedienteile, Rollbalken | `docs/DESIGN.md` |
| Die Schriftlizenzen | LordOS `THIRD_PARTY.md`, Abschnitt „Data files" |
| Die elf Tapeten sind Marke | `assets/wallpapers/README.md`, Zeile 3: „part of the LordOS brand assets" |
| Die vier Verläufe sind eigenes Werk | `tools/gen-wallpaper.py`, Kopf: „PNG, standard library only"; die vier Farbsätze stehen als Zahlen in `PALETTES` |
| NGC-Ist-Stand | `apps/web/public/assets/css/shell.css` auf `origin/main`, 530 Zeilen, Marken in `:root` Zeile 3–45 |

Alles am 30.10.2026 nachgesehen, LordOS-Stand `pushed_at` 2026-10-30.

**Nichts davon ist am Bild geprüft.** Dieses Blatt ist eine Beschreibung aus Quelltext und
Beschreibung der Quelle. Was die Oberfläche von NGC nach dem Umbau wirklich zeigt, wird je Schritt
im Browser gemessen und mit einem Bild belegt — nicht mit diesem Blatt.
