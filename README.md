# NextGenCloud

Short name: **NGC**.

## Status

New project. What exists so far is one prototype, the NGC Desktop.

## Prototype: NGC Desktop 1.0

[`prototype/index.html`](prototype/index.html) is a desktop user interface that runs in the browser.
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
