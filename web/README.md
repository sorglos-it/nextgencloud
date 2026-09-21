# Webinterface

HTML, CSS, JavaScript und PHP, ohne Framework und ohne Build-Schritt. Anweisungen: `web/CLAUDE.md`.
Vorlage für den Desktop: `prototype/index.html`. Verträge: `contracts/README.md`,
Testdaten: `contracts/testdata/`.

Geplanter Aufbau:

```
web/index.php          liefert die Seite aus, liest /etc/ngc/web.json und trust-anchor.json
web/shell/             Desktop: Fenster, Taskleiste, Startmenü, Anmeldung
web/shell/ngc/         Verträge in JavaScript: Noise, Signaturen, Vertrauensliste, Frames
web/sdk/ngc.js         Bibliothek für Apps (MIT)
web/apps/              Oberflächen von App-Store, Benutzer & Rechte, Rechner, Terminal
```
