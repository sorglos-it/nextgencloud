# Inter (von Hand kopiert)

Quelle: **Inter 4.1**, SIL Open Font License 1.1, <https://github.com/rsms/inter>.
Von Hand kopiert am 04.11.2026 aus der veröffentlichten Fassung `Inter-4.1.zip` der
Releases des Projekts. NGC installiert nichts mit npm: die Datei liegt im Repo und wird
von uns gepflegt. Geändert wurde nichts – die Datei steht Byte für Byte so da, wie sie
veröffentlicht wurde.

Wofür: die Schrift des Desktops (`assets/css/shell.css`, `@font-face` über `body`). Ohne
sie folgt die Oberfläche der Schrift, die die Maschine gerade hat – dasselbe Blatt war
Segoe UI unter Windows, Roboto unter Android und San Francisco auf einem Mac. Das sind drei
Bilder einer Gestaltung, und die Bemaßung von `docs/gestaltung-lordos.md` gilt nur für eines.

## Welche Dateien und warum genau diese

| Datei | Wofür | SHA-256 |
| --- | --- | --- |
| `InterVariable.woff2` | die Schrift selbst, aufrecht, Achse 100–900 | `693b77d4f32ee9b8bfc995589b5fad5e99adf2832738661f5402f9978429a8e3` |
| `LICENSE` | die OFL 1.1; sie verlangt, dass sie mitgeht | `262481e844521b326f5ecd053e59b98c8b2da78c8ee1bdbb6e8174305e54935a` |

**Nicht** kopiert ist `InterVariable-Italic.woff2` (387 976 Bytes): `shell.css` enthält kein
einziges `italic` (gemessen: `grep -c italic` ergibt 0). Auch nicht kopiert sind die
statischen Schnitte und die Druckschriften des Pakets – eine variable Datei trägt alle vier
Gewichte, die das Blatt verlangt (400, 500, 600, 700), in denselben 352 240 Bytes.

## Warum die Datei des Herstellers und nicht der Teilsatz von Google Fonts

Beide Wege waren offen und beide sind gemessen:

| | Bytes | Fassung | Prüfsumme der Quelle |
| --- | --- | --- | --- |
| Google Fonts, Teilsätze `latin` + `latin-ext` | 48 256 + 85 068 | `v20` (Googles Baunummer, nennt keine Inter-Fassung) | keine |
| `Inter-4.1.zip`, `web/InterVariable.woff2` | 352 240 | 4.1, von den Autoren benannt | keine |

Die Teilsätze sind 7-mal kleiner, und für die fünf Sprachen der Oberfläche reicht `latin`
völlig: Von den **71** verschiedenen Zeichen in `de/en/es/fr/it.json` liegt **kein einziges**
außerhalb des `latin`-Bereichs (gemessen über alle fünf Dateien).

Genommen ist trotzdem die Datei des Herstellers, und der Grund ist die **Herkunft**. Eine
Schriftdatei wird vom Schriftwerk des Browsers gelesen – das ist eine Angriffsfläche, und
wer diese Bytes geliefert hat, muss nachprüfbar sein. Googles Teilsätze sind von Google
*gebaut*: Sie nennen keine Inter-Fassung, sondern eine Baunummer, und niemand außerhalb
Googles kann sie nachrechnen. `Inter-4.1.zip` nennt die Fassung, und wer die Datei
nachprüfen will, holt dasselbe Archiv und vergleicht. Sicherheit vor Geschwindigkeit –
und die Geschwindigkeit kostet es einmal je Browser-Zwischenspeicher, weil der
`assets/`-Block kein `Cache-Control` sendet und Apache den zweiten Aufruf mit 304
beantwortet.

**Was keine der beiden Quellen liefert: eine veröffentlichte Prüfsumme.** Bei
`noble-hashes` kam sie von der Registry selbst (Feld `dist.integrity`) und wurde gerechnet,
bevor eine Datei ausgepackt wurde. Weder die GitHub-Releases von Inter noch
`fonts.gstatic.com` veröffentlichen eine; die GitHub-API gibt für dieses Release
`digest: None`. Beide Wege hängen damit allein an TLS. Das steht hier, statt es zu
verschweigen – die Prüfsummen oben sind unsere eigenen und taugen dazu, einen *späteren*
Austausch zu bemerken, nicht dazu, den ersten zu beweisen.

## Herkunft nachprüfen

```bash
curl -sL https://github.com/rsms/inter/releases/download/v4.1/Inter-4.1.zip > Inter-4.1.zip
sha256sum Inter-4.1.zip
# 9883fdd4a49d4fb66bd8177ba6625ef9a64aa45899767dde3d36aa425756b11e  (am 04.11.2026)
unzip -p Inter-4.1.zip web/InterVariable.woff2 | sha256sum   # Zeile 1 der Tabelle oben
unzip -p Inter-4.1.zip LICENSE.txt            | sha256sum   # Zeile 2 der Tabelle oben
```

Die Prüfsumme des Archivs ist **unsere** Messung, keine Zusage des Projekts (siehe oben).
Die der `LICENSE` ist dafür doppelt belegt: dieselben 4380 Bytes kamen aus dem Archiv
**und** aus `raw.githubusercontent.com/rsms/inter/master/LICENSE.txt`, zwei voneinander
unabhängige Wege mit demselben Ergebnis.

## Mitreisen im Paket

Gemessen am 04.11.2026 mit `Closure` (`tools/release/closure.go`) über `apps/web/` dieses Zweigs:
80 Dateien im Baum, 64 erreicht, **0 fehlend, 0 nackt**. `InterVariable.woff2` ist **erreicht** –
`closure.go` folgt `url(…)` in einem Stilblatt, also trägt jedes Paket die Schrift, ohne dass sie
irgendwo benannt werden muss.

`LICENSE` und `SOURCE.md` erreicht kein Verweis. Sie gehören deshalb in
`tools/release/extra-web.txt`, wie die beiden von noble-hashes – **und stehen dort noch nicht.**
`TestRealArchiveIsClosed` prüft diese Liste gegen `origin/web`, einen Zweig vom 12.10.2026, 1154
Commits hinter main; er kennt die beiden Dateien nicht, und eine Zeile für eine Datei, die dort
fehlt, geht als „stale line" rot. Die Zeilen gehören in denselben Schritt, der `origin/web`
nachzieht. **Wer das tut, nimmt sie mit** – die OFL 1.1 verlangt, dass sie mit der Schrift geht.
Bis dahin enthält kein Paket die Schrift überhaupt, weil jedes aus `origin/web` gebaut wird.

## Was die Schrift NICHT erreicht

Die Apps hinter dem abgeschotteten Rahmen. Ihre Richtlinie lässt nur
`font-src data:` zu (`classes/Page.php`, `FRAME_CSP`) – eine App kann diese Datei also
nicht holen, und das ist Absicht und keine Lücke: mit `'self'` dürfte eine App unseren
Server um jede Adresse bitten, und ein Aufruf ist ein Weg, etwas hinauszutragen. Eine App,
die Inter will, trägt sie als `data:`-Adresse in ihrem eigenen `ui-tar`. Die Richtlinie
bleibt zu.

## Aktualisieren

Dieselbe Prüfung noch einmal, Datei ersetzen, die Prüfsummen oben neu eintragen – und dann
in `apps/web/tests` die **ganze** Mappe laufen lassen, nicht nur eine Datei:

```bash
cd apps/web/tests && npx playwright test
```

Wer es abkürzen will, braucht mindestens diese zwei – die erste prüft, dass die Schrift
wirklich ankommt und die Maße des Blatts hält, die zweite, dass die Datei dieselben Wälle
bekommt wie jede andere unter `assets/`:

```bash
npx playwright test shell/desktop-look.spec.js shell/csp.spec.js
```

Und `tools/check.sh` gehört wie immer davor.
