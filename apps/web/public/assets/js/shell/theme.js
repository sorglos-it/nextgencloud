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

export const THEMES = ["dark", "light"];

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

// currentTheme is what the document shows, not what is stored: with no storage
// the attribute is gone and the system decides, and then there is no name to
// hand back.
export function currentTheme() {
  const t = document.documentElement.dataset.theme;
  return THEMES.includes(t) ? t : "";
}

export function applyTheme(t) {
  if (!THEMES.includes(t)) return;
  document.documentElement.dataset.theme = t;
  write(KEY_THEME, t);
  for (const fn of listeners) {
    try {
      fn(t);
    } catch {
      // One listener that throws must not stop the others, and a broken
      // listener is no reason to leave the desktop half-painted.
    }
  }
}

// toggleTheme is what the button in the taskbar does. With no theme on the
// document - no storage, the stylesheet decides - the first press picks dark,
// NGC's own default, so one press always does something visible.
export function toggleTheme() {
  applyTheme(currentTheme() === "light" ? "dark" : "light");
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
//   - a theme is stored          -> that one;
//   - storage works, nothing in  -> what the document already says, which is
//                                   NGC's default dark from the template;
//   - no storage                 -> take the attribute off and let the
//                                   stylesheet follow the system. The system
//                                   preference is then answered in one place
//                                   instead of two that can drift apart.
export function initTheme() {
  if (!storageWorks()) {
    delete document.documentElement.dataset.theme;
    return;
  }
  const t = read(KEY_THEME);
  if (THEMES.includes(t)) document.documentElement.dataset.theme = t;
  const a = read(KEY_ACCENT);
  if (ACCENTS.includes(a)) document.documentElement.style.setProperty("--accent", a);
}
