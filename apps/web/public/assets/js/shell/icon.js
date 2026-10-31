// The icons the shell draws for an app: the shapes themselves, and the one
// rule about where a picture on this desktop may come from.
//
// EVERY SHAPE HERE IS THE SHELL'S OWN. None of it comes out of an app's
// package. The manifest's "icon" names a file inside a foreign ui-tar, and a
// picture whose proof has not been checked is a picture somebody else chose;
// checking it would mean fetching a whole ui-tar (up to 64 MiB, uitar.js)
// before the start menu can be drawn. See docs/app-weg.md, finding 6.
//
// WHY THE SHAPES STAND IN THIS FILE and not in .svg files beside the other
// assets: an .svg under this origin is a DOCUMENT of this origin, with scripts
// of its own, and a person can be sent to it directly - which is the rule
// shell/csp.spec.js holds the web root to ("nothing a browser would run as a
// document of this origin"). The way in that the rule leaves open is an inline
// <svg> built with the DOM, and that is what fillGlyph does: element by
// element, attribute by attribute, never innerHTML and never a URL.
//
// The same shapes lie in docs/icons/ as .svg files - for app builders to copy
// into their own ui/, and for docs/icons/uebersicht.html. That is a second
// copy, so it is measured and not trusted: shell/desktop-icons.spec.js reads
// both and fails when they come apart.

// One canvas for all of them, and the line they are drawn with. Anything
// closer than 2.5 units to a neighbour disappears at 24 px - measured
// 24.10.2026 on docs/icons/uebersicht.html.
export const BOX = "0 0 24 24";
export const LINE = { fill: "none", stroke: "currentColor", "stroke-width": "1.75", "stroke-linecap": "round", "stroke-linejoin": "round" };
export const SOLID = { fill: "currentColor", stroke: "none" };

// The shapes. Each is a list of [tag, attributes]; nothing else may stand in
// here, and nothing in here is ever read as text by the browser.
export const SHAPES = {
  __proto__: null,
  terminal: [
    ["rect", { x: 2.5, y: 3.5, width: 19, height: 17, rx: 2.5 }],
    ["path", { d: "M6.5 9.75 9.25 12.5 6.5 15.25" }],
    ["path", { d: "M12.25 15.25h5.25" }],
  ],
  uhr: [
    ["circle", { cx: 12, cy: 12, r: 9 }],
    ["path", { d: "M12 6.75V12l3.5 2.5" }],
  ],
  laden: [
    ["path", { d: "M3.5 8.5h17v10a2.5 2.5 0 0 1-2.5 2.5H6a2.5 2.5 0 0 1-2.5-2.5z" }],
    ["path", { d: "M8.5 8.5V6.75a3.5 3.5 0 0 1 7 0V8.5" }],
  ],
  benutzer: [
    ["circle", { cx: 12, cy: 8, r: 3.75 }],
    ["path", { d: "M4.75 20.75a7.25 7.25 0 0 1 14.5 0" }],
  ],
  rechte: [
    ["path", { d: "M12 2.75 20 5.5v6.25c0 4.5-3.25 7.75-8 9.5-4.75-1.75-8-5-8-9.5V5.5z" }],
    ["path", { d: "M8.5 11.75 11 14.25l4.5-4.5" }],
  ],
  platzhalter: [
    ["rect", { x: 3.5, y: 3.5, width: 17, height: 17, rx: 3.5 }],
    ["circle", { cx: 12, cy: 12, r: 3.25 }],
  ],
  einstellungen: [
    ["path", { d: "M3.5 8.5h4.25M12.25 8.5h8.25" }],
    ["path", { d: "M3.5 15.5h8.25M16.25 15.5h4.25" }],
    ["circle", { cx: 10, cy: 8.5, r: 2.5 }],
    ["circle", { cx: 14, cy: 15.5, r: 2.5 }],
  ],
  dateien: [
    ["path", { d: "M6.5 2.75h7.25L19.5 8.5v12.75H6.5z" }],
    ["path", { d: "M13.75 2.75V8.5h5.75" }],
  ],
  knoten: [
    ["circle", { cx: 12, cy: 5.25, r: 2.75 }],
    ["circle", { cx: 5.75, cy: 18.25, r: 2.75 }],
    ["circle", { cx: 18.25, cy: 18.25, r: 2.75 }],
    ["path", { d: "M10.5 7.75 7.25 15.75M13.5 7.75 16.75 15.75M8.5 18.25h7" }],
  ],
  rechner: [
    ["rect", { x: 3.5, y: 2.5, width: 17, height: 19, rx: 2.5 }],
    ["path", { d: "M7 7h10" }],
    ["circle", { cx: 8.75, cy: 13, r: 1.35, solid: true }],
    ["circle", { cx: 15.25, cy: 13, r: 1.35, solid: true }],
    ["circle", { cx: 8.75, cy: 17.5, r: 1.35, solid: true }],
    ["circle", { cx: 15.25, cy: 17.5, r: 1.35, solid: true }],
  ],
  "nicht-erreichbar": [
    ["circle", { cx: 12, cy: 12, r: 9 }],
    ["path", { d: "M6.25 17.75 17.75 6.25" }],
  ],
  "keine-lizenz": [
    ["path", { d: "M5.75 2.75h12.5v18.5H5.75z" }],
    ["path", { d: "M9 8.75 15 14.75M15 8.75 9 14.75" }],
  ],
};

// The name a person reads. The shell never shows it - the app's own name
// stands beside every badge - but docs/icons/*.svg carry it as <title>, and
// those files are written out of this table (apps/web/tests/icons-build.mjs).
export const TITLES = {
  __proto__: null,
  terminal: "Terminal", uhr: "Uhr", laden: "App-Store", benutzer: "Benutzer",
  rechte: "Rechte", platzhalter: "App", einstellungen: "Einstellungen",
  dateien: "Dateien", knoten: "Knoten", rechner: "Rechner",
  "nicht-erreichbar": "Nicht erreichbar", "keine-lizenz": "Keine Lizenz",
};

export const PLACEHOLDER = "platzhalter";

// The apps the shell knows by name. An id that is not in here gets the
// placeholder, never a picture from the package.
//
// __proto__: null, because an id comes from the API: without it an app that
// called itself "constructor" would read a function out of Object's prototype.
const BY_APP = {
  __proto__: null,
  "ngc.store": "laden",
  "ngc.users": "benutzer",
  "org.sorglos.terminal": "terminal",
  "org.sorglos.uhr": "uhr",
  "org.sorglos.calculator": "rechner",
};

// The two states the desktop already knows (core.AppUnreachable,
// core.AppUnlicensed). They are drawn instead of the app's icon: an app that
// cannot be reached is not the app that was clicked, and the dimmed tile alone
// does not say which of the two it is.
const BY_STATE = {
  __proto__: null,
  unreachable: "nicht-erreichbar",
  unlicensed: "keine-lizenz",
};

// iconName answers the name of the icon for one entry of the app list. Never
// null and never a name SHAPES has no entry for: the name under the tile is
// what tells two placeholders apart.
export function iconName(app) {
  return BY_STATE[app?.state] ?? BY_APP[app?.id] ?? PLACEHOLDER;
}

export function shapesOf(name) {
  return SHAPES[name] ?? null;
}

export function names() {
  return Object.keys(SHAPES);
}

const NS = "http://www.w3.org/2000/svg";

// fillGlyph builds the icon into a badge that is already in the page. The
// badge keeps its own size and the icon fills it, so one call serves the 38 px
// tile on the desktop, the 34 px tile in the menu and the 20 px one in the
// title bar.
//
// A name SHAPES does not know draws the placeholder rather than nothing: an
// empty badge is a hole in the row, and a hole says less than a picture that
// admits it knows nothing.
export function fillGlyph(badge, name) {
  const shapes = SHAPES[name] ?? SHAPES[PLACEHOLDER];
  const svg = document.createElementNS(NS, "svg");
  svg.setAttribute("viewBox", BOX);
  svg.setAttribute("class", "ico");
  // The title beside it says everything a screen reader needs; the badge
  // itself is hidden where it is built (desktop.js, window.js), and this says
  // the same thing a second time for an icon that ends up anywhere else.
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");
  for (const [tag, attr] of shapes) {
    const e = document.createElementNS(NS, tag);
    for (const [k, v] of Object.entries(attr)) {
      if (k !== "solid") e.setAttribute(k, String(v));
    }
    for (const [k, v] of Object.entries(attr.solid ? SOLID : LINE)) e.setAttribute(k, v);
    svg.append(e);
  }
  badge.append(svg);
  return svg;
}
