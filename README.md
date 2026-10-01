# NextGenCloud

[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)
[![Contracts: MIT](https://img.shields.io/badge/contracts-MIT-green.svg)](shared/LICENSE)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8.svg)](go.mod)
[![Platforms](https://img.shields.io/badge/platforms-linux%2Famd64%20%7C%20linux%2Farm64-lightgrey.svg)](tools/check.sh)
[![Web interface](https://img.shields.io/badge/apps%2Fweb-0.2.0-brightgreen.svg)](apps/web/VERSION)

**NGC** for short. An open alternative to Synology's DSM: a desktop in the browser that starts with
nothing but a login and an app store, and grows through apps — from a small cloud or a firewall on
a Raspberry Pi to distributed installations where the web interface, the API, the database and the
server controllers each run on their own machine.

| Folder | Purpose | Language | Start | Build |
| --- | --- | --- | --- | --- |
| `apps/web` | The desktop in the browser | HTML, CSS, JS, PHP | `cd apps/web/tests && npm test` | no build step |
| `apps/server-api` | API `ngc-api`: sessions, rights, forwarding | Go | `ngc-api -config /etc/ngc/api.json` | `go build -o ngc-api ./apps/server-api` |
| `apps/server-controller` | Controller `ngc-controller` on every server: tunnel, services, updates | Go | `ngc-controller -config /etc/ngc/controller.json` | `go build -o ngc-controller ./apps/server-controller` |
| `apps/server-db` | Database: MariaDB and SQLite behind `dbapi`, migrations | Go, SQL | library for the API and the controller | – |
| `apps/cli` | Tool `ngc-store`: packs apps, signs catalogs and licences | Go | `ngc-store pack <directory> <package.ngc>` | `go build -o ngc-store ./apps/cli` |

`shared/` holds the contracts between the parts (Go packages and test data), `tools/` the scripts,
`docs/` the design and the current state.

## Start in 3 steps

1. Install Go 1.27 or newer. For the web interface also PHP 8.2 or newer and Node.js.
2. Check everything: `tools/check.sh` (format, vet, tests, build for linux/amd64 and arm64). The
   browser tests of the web interface run on their own: `cd apps/web/tests && npm install &&
   npm run install-browsers && npm test`.
3. Open [`docs/prototype.html`](docs/prototype.html) in a browser to see the desktop.

## State

The contracts (V1–V7) are settled and the web interface is built: login, session, desktop, app
windows, app store, users and rights — see [`apps/web/README.md`](apps/web/README.md). The API, the
controller and the database are being built next.

- Design of the core: [`docs/superpowers/specs/2026-09-19-ngc-kern-design.md`](docs/superpowers/specs/2026-09-19-ngc-kern-design.md)
- Current state and next steps: [`docs/STAND.md`](docs/STAND.md)
- Contracts: [`shared/README.md`](shared/README.md)

## Prototype: NGC Desktop 1.0

[`docs/prototype.html`](docs/prototype.html) is a desktop surface that runs in a browser. It is one
single self-contained HTML file — no framework, no build step, no outside dependency. Open it in a
browser and it runs. It is what `apps/web` was drawn from.

- **Windows** — move, resize at every edge, maximize, minimize, close. A double click on the title
  bar maximizes; a window dragged to the top edge is maximized, at the left or right edge it fills
  that half of the screen.
- **Taskbar at the top** — start button, one button per open window, light/dark switch, clock.
- **Start menu** — a searchable list of all apps, plus "Fenster anordnen" (tile all windows) and
  "Alle schließen".
- **Eight apps** — explorer, notepad, calculator, terminal, paint, clock, settings, about NGC.
- **Settings** — six wallpapers, six accent colours, a light and a dark theme.
- **Kept** — theme, wallpaper, accent colour and the text of the notepad live in the browser's
  `localStorage`, under keys with the prefix `ngc.`.
- **Mouse, pen and touch** — dragging and resizing go through pointer events.

The surface is German. Keyboard: `Esc` closes the start menu, `Alt`+`F4` closes the active window,
the Windows key or `Ctrl`+`Esc` opens and closes the start menu.

Still a mock-up: the explorer shows placeholder files and cannot open them, and the terminal
("NGC Shell") knows only its ten built-in commands (`help`, `apps`, `open <app>`, `echo`, `date`,
`dir`, `ver`, `theme`, `cls`, `exit`) — it runs nothing on the machine.

The file was taken unchanged from `W:\ngc\index.html` (5 August 2026).

## Author

Sorglos Thomas Weirich

## Donate via PayPal

NGC is free software and stays that way. If it saves you a licence, consider leaving something:
[Donate via PayPal](https://www.paypal.com/donate/?hosted_button_id=6CDEVZGJWTNQQ).

## License

NGC is licensed under the AGPL-3.0 ([`LICENSE`](LICENSE)). The contracts in `shared/`
([`shared/LICENSE`](shared/LICENSE)) and the app library `ngc.js` are MIT, so that closed and paid
apps are possible too. Third-party code: [`THIRD-PARTY.md`](THIRD-PARTY.md).
