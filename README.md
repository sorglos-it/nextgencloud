# NextGenCloud

Kurz **NGC**. Eine offene Alternative zu Synologys DSM: ein Desktop im Browser, der mit nichts als
einer Anmeldung und einem App-Store beginnt und durch Apps wächst – von einer kleinen Cloud
oder einer Firewall auf einem Raspberry Pi bis zu verteilten Installationen, in denen
Webinterface, API, Datenbank und Server-Controller auf eigenen Maschinen laufen.

| Ordner | Zweck | Sprache | Start | Bauen |
| --- | --- | --- | --- | --- |
| `apps/web` | Webinterface: der Desktop im Browser | HTML, CSS, JavaScript, PHP | Apache liefert `apps/web/public` aus | – (kein Build-Schritt) |
| `apps/server-api` | API `ngc-api`: Sitzungen, Rechte, Weiterleitung | Go | `ngc-api -config /etc/ngc/api.json` | `go build -o ngc-api ./apps/server-api` |
| `apps/server-controller` | Controller `ngc-controller` auf jedem Server: Tunnel, Dienste, Updates | Go | `ngc-controller -config /etc/ngc/controller.json` | `go build -o ngc-controller ./apps/server-controller` |
| `apps/server-db` | Datenbank: MariaDB und SQLite hinter `dbapi`, Migrationen | Go, SQL | – (Bibliothek für API und Controller) | – |
| `apps/cli` | Werkzeug `ngc-store`: packt Apps, signiert Kataloge und Lizenzen | Go | `ngc-store pack <directory> <package.ngc>` | `go build -o ngc-store ./apps/cli` |

`shared/` enthält die Verträge zwischen den Teilen (Go-Pakete und Testdaten), `tools/` die
Skripte, `docs/` den Entwurf und den aktuellen Stand.

## Start in 3 Schritten

Noch läuft nichts: Die Programme sind Grundgerüste, bis die Verträge freigegeben sind.

1. Go 1.27 oder neuer installieren.
2. Alles prüfen: `tools/check.sh` (Format, vet, Tests, Bauen für linux/amd64 und arm64).
3. [`docs/prototype.html`](docs/prototype.html) im Browser öffnen, um den Desktop zu sehen.

## Stand

Entwurf und Verträge. Die Teile werden gebaut, sobald die Verträge freigegeben sind.

- Entwurf des Kerns: [`docs/superpowers/specs/2026-09-19-ngc-kern-design.md`](docs/superpowers/specs/2026-09-19-ngc-kern-design.md)
- Aktueller Stand und nächste Schritte: [`docs/STAND.md`](docs/STAND.md)
- Verträge: [`shared/README.md`](shared/README.md)

## Prototyp: NGC Desktop 1.0

[`docs/prototype.html`](docs/prototype.html) ist eine Desktop-Oberfläche, die im Browser läuft. Sie
ist eine einzige, in sich geschlossene HTML-Datei – kein Framework, kein Build-Schritt, keine
fremden Abhängigkeiten. Im Browser öffnen, und sie läuft.

- **Fenster** – verschieben, an jeder Kante in der Größe ändern, maximieren, minimieren, schließen.
  Ein Doppelklick auf die Titelleiste maximiert; ein Fenster, das an den oberen Rand gezogen wird,
  wird maximiert, am linken oder rechten Rand füllt es diese Hälfte des Bildschirms.
- **Taskleiste oben** – Startknopf, ein Knopf je offenem Fenster, Umschalter hell/dunkel, Uhr.
- **Startmenü** – durchsuchbare Liste aller Apps, dazu „Fenster anordnen“ (alle Fenster
  nebeneinander) und „Alle schließen“.
- **Acht Apps** – Explorer, Notizblock, Rechner, Terminal, Malen, Uhr, Einstellungen, Über NGC.
- **Einstellungen** – sechs Hintergründe, sechs Akzentfarben, helles und dunkles Design.
- **Gespeichert** – Design, Hintergrund, Akzentfarbe und der Text des Notizblocks liegen im
  `localStorage` des Browsers, unter Schlüsseln mit dem Präfix `ngc.`.
- **Maus, Stift und Touch** – Ziehen und Größe ändern laufen über Pointer Events.

Die Oberfläche ist deutsch. Tastatur: `Esc` schließt das Startmenü, `Alt`+`F4` schließt das aktive
Fenster, die Windows-Taste oder `Strg`+`Esc` öffnet und schließt das Startmenü.

Noch eine Attrappe: Der Explorer zeigt Platzhalter-Dateien und kann sie nicht öffnen, und das
Terminal („NGC Shell“) kennt nur seine zehn eingebauten Befehle (`help`, `apps`, `open <app>`,
`echo`, `date`, `dir`, `ver`, `theme`, `cls`, `exit`) – es führt nichts auf dem Rechner aus.

Die Datei ist unverändert aus `W:\ngc\index.html` übernommen (5. August 2026).

## Lizenz

NGC steht unter der AGPL-3.0 ([`LICENSE`](LICENSE)). Die Verträge in `shared/`
([`shared/LICENSE`](shared/LICENSE)) und die App-Bibliothek `ngc.js` stehen unter MIT, damit auch
geschlossene und kostenpflichtige Apps möglich sind. Fremdcode: [`THIRD-PARTY.md`](THIRD-PARTY.md).
