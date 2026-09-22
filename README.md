# NextGenCloud

Short name: **NGC**. An open alternative to Synology's DSM: a desktop in the browser that starts
with nothing but a login and an app store, and grows through apps — from a small cloud or a
firewall on a Raspberry Pi to distributed setups where web interface, API, database and server
controller run on separate machines.

| Folder | Purpose | Language | Start | Build |
| --- | --- | --- | --- | --- |
| `apps/web` | Web interface: the desktop in the browser | HTML, CSS, JavaScript, PHP | served by Apache from `apps/web/public` | none, no build step |
| `apps/server-api` | API `ngc-api`: sessions, rights, forwarding | Go | `ngc-api -config /etc/ngc/api.json` | `go build -o ngc-api ./apps/server-api` |
| `apps/server-controller` | Controller `ngc-controller` on every server: tunnel, services, updates | Go | `ngc-controller -config /etc/ngc/controller.json` | `go build -o ngc-controller ./apps/server-controller` |
| `apps/server-db` | Database: MariaDB and SQLite behind `dbapi`, migrations | Go, SQL | — (library, used by API and controller) | — |
| `apps/cli` | Tool `ngc-store`: packs apps, signs catalogs and licenses | Go | `ngc-store pack <directory> <package.ngc>` | `go build -o ngc-store ./apps/cli` |

`shared/` holds the contracts between the parts (Go packages and test vectors), `tools/` the
scripts, `docs/` the design and the current state.

## Start in 3 steps

Nothing runs yet: the programs are skeletons until the contracts are approved.

1. Install Go 1.27 or newer.
2. Check everything: `tools/check.sh` (format, vet, tests, builds for linux/amd64 and arm64).
3. Open [`docs/prototype.html`](docs/prototype.html) in a browser to see the desktop.

## Status

Design and contracts. The parts are built after the contracts are approved.

- Design of the core, in German: [`docs/superpowers/specs/2026-09-19-ngc-kern-design.md`](docs/superpowers/specs/2026-09-19-ngc-kern-design.md)
- Current state and next steps: [`docs/STAND.md`](docs/STAND.md)
- Contracts: [`shared/README.md`](shared/README.md)

## Prototype: NGC Desktop 1.0

[`docs/prototype.html`](docs/prototype.html) is a desktop user interface that runs in the browser.
It is a single self-contained HTML file — no framework, no build step, no external dependencies.
Open it in a browser and it runs.

- **Windows** — move, resize from every edge, maximise, minimise, close. Double-clicking the title bar
  maximises; dragging a window to the top edge maximises it, to the left or right edge snaps it to that
  half of the screen.
- **Taskbar at the top** — start button, one button per open window, light/dark switch, clock.
- **Start menu** — searchable list of all apps, plus "Fenster anordnen" (tile all windows) and
  "Alle schließen" (close all).
- **Eight apps** — Explorer, Notizblock, Rechner, Terminal, Malen, Uhr, Einstellungen, Über NGC.
- **Settings** — six wallpapers, six accent colours, light and dark theme.
- **Persistence** — theme, wallpaper, accent colour and the notepad text live in the browser's
  `localStorage`, keys prefixed with `ngc.`.
- **Mouse, pen and touch** — dragging and resizing use Pointer Events.

The interface language is German. Keyboard: `Esc` closes the start menu, `Alt`+`F4` closes the focused
window, the Windows key or `Ctrl`+`Esc` opens and closes the start menu.

Still a mock-up: the Explorer shows placeholder files and cannot open them, and the terminal
("NGC Shell") only knows its ten built-in commands (`help`, `apps`, `open <app>`, `echo`, `date`, `dir`,
`ver`, `theme`, `cls`, `exit`) — it runs nothing on the host.

The file is committed unchanged from `W:\ngc\index.html` (5 August 2026).

## License

NGC is licensed under the AGPL-3.0 ([`LICENSE`](LICENSE)). The contracts in `shared/`
([`shared/LICENSE`](shared/LICENSE)) and the app library `ngc.js` are licensed under MIT, so closed
and paid apps are possible. Third-party code: [`THIRD-PARTY.md`](THIRD-PARTY.md).
