// Look of the shell: light or dark, the accent colour and the wallpaper.
//
// All three are convenience and nothing else. They decide what the desktop
// looks like, never what it may do, so they live in localStorage where the
// person's own browser keeps them and no server ever sees them. That is also
// why a closed storage is not an error here: the shell carries on and the
// stylesheet answers instead.

const KEY_THEME = "ngc.theme";
const KEY_ACCENT = "ngc.accent";
const KEY_WALLPAPER = "ngc.wallpaper";
const KEY_DENSITY = "ngc.density";

// The two designs there are. A name outside this list never reaches the
// document, and applyTheme refuses it.
export const THEMES = ["dark", "light"];

// What a person may CHOOSE, which is one more than there are designs:
// "system" is a choice of its own since docs/gestaltung.md point 7, and it is
// the default. It is not a design - it is "ask the machine" - so it is kept
// apart from THEMES rather than added to it: applyTheme writes a design onto
// the document, and "system" writes nothing and takes the attribute off.
export const CHOICES = ["system", "dark", "light"];
export const DEFAULT_CHOICE = "system";

// The media query that answers for "system". It is asked through one object
// held here, not a fresh matchMedia per question: the listener below lives as
// long as the page, and a second object would mean a second listener.
//
// A browser without matchMedia (none this is built for, but the shell must
// not fall over on one) is read as dark, NGC's own default.
const SYSTEM_LIGHT = typeof matchMedia === "function"
  ? matchMedia("(prefers-color-scheme: light)")
  : null;

// The wallpapers of docs/prototype.html. They are gradients written here, not
// images fetched from anywhere: no request leaves the page for a background,
// and the content security policy needs no img-src for it.
export const WALLPAPERS = [
  { id: "deep", name: "Weltraum", css: "radial-gradient(120% 120% at 20% 15%, #2b4a7d 0%, #16223a 45%, #0a0e18 100%)" },
  { id: "aurora", name: "Polarlicht", css: "linear-gradient(135deg,#0f2027 0%,#203a43 45%,#2c5364 100%)" },
  { id: "sunset", name: "Abendrot", css: "linear-gradient(135deg,#ff7e5f 0%,#feb47b 50%,#8e5b8e 100%)" },
  { id: "mint", name: "Minze", css: "linear-gradient(135deg,#134e5e 0%,#71b280 100%)" },
  { id: "plum", name: "Pflaume", css: "linear-gradient(135deg,#41295a 0%,#2f0743 100%)" },
];

export const ACCENTS = ["#2f7de1", "#16d1c3", "#e14f8a", "#f0a020", "#8b5cf6", "#22c55e"];

// Eng, wie heute, luftig (docs/gestaltung.md Punkt 5). The names a person
// reads stand with them, because this is the one list of them: the start menu
// draws its three buttons out of this and nothing else.
//
// "normal" is the default and writes NO attribute: the sizes of :root are the
// density that was measured and checked, and a second place holding the same
// three numbers is a place for them to drift apart. Same shape as the theme's
// "system", and for the same reason.
export const DENSITIES = [
  { id: "eng", name: "Eng" },
  { id: "normal", name: "Wie heute" },
  { id: "luftig", name: "Luftig" },
];
export const DEFAULT_DENSITY = "normal";

const listeners = new Set();

// read and write never throw. A browser in private mode, a browser told to
// keep no site data, a quota that is full: none of that is a reason to leave
// the person without a desktop.
function read(key) {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function write(key, value) {
  try {
    localStorage.setItem(key, value);
  } catch {
    // Nothing to do and nothing to say: the choice holds for this page and is
    // forgotten on the next one.
  }
}

// storageWorks tells the two cases apart that read() cannot: "nothing stored
// yet" and "no storage at all". Only the second one hands the decision to the
// stylesheet, and it is asked once, at the start.
function storageWorks() {
  try {
    localStorage.getItem(KEY_THEME);
    return true;
  } catch {
    return false;
  }
}

// currentTheme is what the document shows, not what is stored: while the
// choice is "system" the attribute is gone and the stylesheet decides, and
// then there is no name to hand back.
export function currentTheme() {
  const t = document.documentElement.dataset.theme;
  return THEMES.includes(t) ? t : "";
}

// What the choice IS, as the start menu has to mark it. It is read off the
// document for the same reason markLook reads the document: the mark and the
// screen cannot then drift apart. No attribute means "system", because that
// is the only choice that takes it off.
export function currentChoice() {
  return currentTheme() || "system";
}

// Which of the two designs is actually on the screen - the choice resolved.
// The shell itself needs this nowhere, because @media answers in the
// stylesheet; an app in a frame needs it, because its document is written by
// hand and has no stylesheet of ours in it (appframe.js).
export function effectiveTheme() {
  const t = currentTheme();
  if (t) return t;
  return SYSTEM_LIGHT?.matches ? "light" : "dark";
}

// tell hands the design that is now on the screen to everyone listening. It
// is the resolved design and never "system": a listener paints something, and
// nothing can be painted in "ask the machine".
function tell() {
  const t = effectiveTheme();
  for (const fn of listeners) {
    try {
      fn(t);
    } catch {
      // One listener that throws must not stop the others, and a broken
      // listener is no reason to leave the desktop half-painted.
    }
  }
}

// The system changing its mind while "system" is chosen. The shell draws
// itself again without being asked - never "please reload", which the
// decision book forbids - and the stylesheet does that part on its own. This
// listener is here for the parts no stylesheet of ours reaches: the documents
// of the open app frames.
//
// It runs whatever the choice is and asks afterwards, rather than being added
// and removed with the choice: a listener that is attached and detached is a
// listener that can be missing when the moment comes.
SYSTEM_LIGHT?.addEventListener?.("change", () => {
  if (currentChoice() === "system") tell();
});

export function applyTheme(t) {
  if (!CHOICES.includes(t)) return;
  // "system" is the absence of a design on the document: the stylesheet's
  // prefers-color-scheme rule bites only on html:not([data-theme]), so taking
  // the attribute off is what hands the decision over.
  if (t === "system") delete document.documentElement.dataset.theme;
  else document.documentElement.dataset.theme = t;
  write(KEY_THEME, t);
  tell();
}

// toggleTheme is what the button in the taskbar does, and it is called
// "Design wechseln": it switches to the OTHER of the two designs, read off
// what is on the screen right now. So one press always does something
// visible, including the press that comes out of "system".
//
// It does not walk the three choices. "Wie das System" is a setting and not
// a design, and the place for settings is the start menu under "Aussehen"
// (docs/gestaltung.md point 5 names it for the density, point 7 for this) -
// a button in the bar that cycles through a setting is a button whose next
// press nobody can predict.
export function toggleTheme() {
  applyTheme(effectiveTheme() === "light" ? "dark" : "light");
}

export function onThemeChange(fn) {
  if (typeof fn === "function") listeners.add(fn);
}

export function applyAccent(c) {
  // Only the six of the list. An accent goes into a style property, and a
  // value from outside has no business there even when it looks like a colour.
  if (!ACCENTS.includes(c)) return;
  document.documentElement.style.setProperty("--accent", c);
  write(KEY_ACCENT, c);
}

// What the document shows, read off the document itself for the reason
// markLook reads it: the mark in the menu and the screen cannot then drift.
export function currentDensity() {
  const d = document.documentElement.dataset.density;
  return DENSITIES.some((x) => x.id === d) ? d : DEFAULT_DENSITY;
}

// applyDensity sets the two size tokens of shell.css, by name. It writes no
// number of its own: the numbers are in the stylesheet, where the shell's
// other sizes are, and this only says which of the three sets is in force.
export function applyDensity(id) {
  if (!DENSITIES.some((x) => x.id === id)) return;
  if (id === DEFAULT_DENSITY) delete document.documentElement.dataset.density;
  else document.documentElement.dataset.density = id;
  write(KEY_DENSITY, id);
}

export function applyWallpaper(id) {
  const w = WALLPAPERS.find((x) => x.id === id) || WALLPAPERS[0];
  const e = document.getElementById("wallpaper");
  if (e) e.style.background = w.css;
  write(KEY_WALLPAPER, w.id);
}

// initTheme runs before the desktop is built, so the page is never painted in
// one theme and then repainted in the other.
//
// Three cases, and the third is the reason the stylesheet carries a
// prefers-color-scheme rule at all:
//   - a design is stored         -> that one;
//   - storage works, nothing in  -> "system", the default of point 7: the
//                                   attribute comes off and the stylesheet
//                                   follows the machine;
//   - no storage                 -> the same, and for the same reason. The
//                                   system preference is answered in one
//                                   place instead of two that can drift
//                                   apart.
export function initTheme() {
  if (!storageWorks()) {
    delete document.documentElement.dataset.theme;
    return;
  }
  const t = read(KEY_THEME);
  // Nothing stored is "system" now, not the dark the template carries: the
  // default of point 7 is the machine's setting. The attribute comes off, so
  // the stylesheet answers - and it answers in ONE place instead of a second
  // one here that could drift away from it.
  if (THEMES.includes(t)) document.documentElement.dataset.theme = t;
  else delete document.documentElement.dataset.theme;
  const a = read(KEY_ACCENT);
  if (ACCENTS.includes(a)) document.documentElement.style.setProperty("--accent", a);
  // The density, remembered the same way and set in the same breath: before
  // the desktop is built, so nothing is drawn at one density and redrawn at
  // another.
  const d = read(KEY_DENSITY);
  if (DENSITIES.some((x) => x.id === d) && d !== DEFAULT_DENSITY) {
    document.documentElement.dataset.density = d;
  } else {
    delete document.documentElement.dataset.density;
  }
}
