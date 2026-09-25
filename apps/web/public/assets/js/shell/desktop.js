// The desktop: wallpaper, icons, taskbar, start menu and the windows on it.
//
// Every name, every title and every message that comes from outside goes on
// screen through dom.js, which writes text nodes and nothing else. The
// prototype in docs/prototype.html builds the same surface with innerHTML;
// look and handling are taken from it, that habit is not.
import { el, button, text, clear } from "./dom.js";
import * as core from "../contracts/core.js";
import { Win } from "./window.js";
import { calcApp } from "../apps/calc.js";
import {
  ACCENTS, WALLPAPERS, applyAccent, applyTheme, applyWallpaper, currentTheme, toggleTheme,
} from "./theme.js";

// The apps whose face the web interface itself builds: app id to the function
// that fills the window's body. Everything else needs its verified user
// interface and the sandboxed frame it runs in (Task 22).
//
// TO BE MOVED WITH TASK 22. Whatever stands here mounts straight into
// win.body, because ngc.js and the sandboxed frame do not exist yet. It has
// to end up in that frame like every other app: an app that ships with NGC
// must not behave differently from an app out of the store, or the frame is
// only ever tested by the apps nobody wrote yet, and a built-in app would
// quietly keep powers - same origin, the whole DOM of the shell, localStorage
// - that a store app is refused. When appframe.js lands, this table maps the
// app id to its entry inside the frame and nothing mounts into win.body any
// more.
const BUILTIN = {
  __proto__: null,
  "ngc.calc": calcApp,
};

// Accent colours in the order of theme.js, with the name the swatch carries
// for a screen reader. Six colours, no colour picker: a wall of choices is
// what the design rules forbid, and a name is easier to hit than a hex value.
const ACCENT_NAMES = ["Blau", "Türkis", "Pink", "Orange", "Violett", "Grün"];

// How long a friendly notice stays. An error stays until it is closed.
const NOTICE_MS = 8000;

class Desktop {
  constructor(root, { link, session, trust }) {
    this.link = link;
    // Not used yet. Both stand in the interface this task was given, so the
    // call sites are already written the way they will stay; the session and
    // the verified trust list arrive with the channel and the login, and the
    // desktop asks nothing of them today.
    this.session = session ?? null;
    this.trust = trust ?? null;

    this.windows = new Map();
    this.focused = null;
    this.zTop = 10;
    this.cascade = 0;
    this.uid = 0;
    this.appList = [];
    this.noticeTimer = 0;

    clear(root);
    this.buildBar(root);
    this.buildMenu(root);
    this.buildDesktop(root);
    this.buildNotice(root);
    this.wireGlobal();
  }

  // ---- the frame of the shell ---------------------------------------------
  buildBar(root) {
    const bar = el("div", null);
    bar.id = "taskbar";

    this.startBtn = button(null, null, null);
    this.startBtn.id = "startbtn";
    const logo = el("span", "logo", "⊞");
    logo.setAttribute("aria-hidden", "true");
    this.startBtn.append(logo, el("span", null, "Start"));
    this.startBtn.setAttribute("aria-expanded", "false");
    this.startBtn.addEventListener("click", (e) => {
      e.stopPropagation();
      this.toggleMenu();
    });

    this.tasks = el("div", null);
    this.tasks.id = "tasks";

    const tray = el("div", null);
    tray.id = "tray";
    const themeBtn = button(null, "🌗", "Design wechseln");
    themeBtn.id = "themebtn";
    themeBtn.title = "Design wechseln";
    themeBtn.addEventListener("click", () => toggleTheme());
    this.clock = el("div", null);
    this.clock.id = "clock";
    tray.append(themeBtn, this.clock);

    bar.append(this.startBtn, this.tasks, tray);
    root.append(bar);
    this.tick();
    // Every 15 seconds, as in the prototype. The clock shows no seconds, so
    // this is only about how long a minute may look stale: a quarter of one,
    // at a cost of four wake-ups a minute.
    this.clockTimer = setInterval(() => this.tick(), 15000);
  }

  tick() {
    const d = new Date();
    clear(this.clock);
    this.clock.append(
      el("b", null, d.toLocaleTimeString("de-DE", { hour: "2-digit", minute: "2-digit" })),
      el("span", null, d.toLocaleDateString("de-DE", { day: "2-digit", month: "2-digit", year: "numeric" })),
    );
  }

  buildMenu(root) {
    const menu = el("div", null);
    menu.id = "startmenu";

    this.search = el("input", null);
    this.search.id = "smsearch";
    this.search.type = "text";
    this.search.placeholder = "Apps durchsuchen …";
    this.search.autocomplete = "off";
    this.search.setAttribute("aria-label", "Apps durchsuchen");
    this.search.addEventListener("input", () => this.renderMenu(this.search.value));
    this.search.addEventListener("keydown", (e) => {
      if (e.key === "Escape") this.toggleMenu(false);
      if (e.key === "Enter") {
        const first = this.list.querySelector(".smitem");
        if (first) first.click();
      }
    });

    const head = el("div", null, "Alle Apps");
    head.id = "smhead";

    this.list = el("div", null);
    this.list.id = "smlist";
    // Only the list is the menu. The search field above it is no menu item,
    // and a role="menu" holding something else is a promise to a screen
    // reader that the shell would then break.
    this.list.setAttribute("role", "menu");
    this.list.setAttribute("aria-label", "Alle Apps");

    menu.append(this.search, head, this.list, this.buildLook(), this.buildFoot());
    root.append(menu);
    this.menu = menu;
  }

  // The look of the desktop, right in the start menu: light or dark, one of
  // six accents, one of five backgrounds. Nothing here is typed in, so there
  // is nothing to get wrong.
  buildLook() {
    const box = el("div", null);
    box.id = "smlook";
    const head = el("div", null, "Aussehen");
    head.id = "smlookhead";

    const themes = el("div", "row");
    for (const [t, label] of [["light", "Hell"], ["dark", "Dunkel"]]) {
      const b = button("b", label, null);
      b.addEventListener("click", () => {
        applyTheme(t);
        this.markLook();
      });
      b.dataset.theme = t;
      themes.append(b);
    }
    this.themeRow = themes;

    const colors = el("div", "swatches");
    ACCENTS.forEach((c, i) => {
      const b = button(null, null, "Farbe " + ACCENT_NAMES[i]);
      b.style.background = c;
      b.dataset.accent = c;
      b.addEventListener("click", () => {
        applyAccent(c);
        this.markLook();
      });
      colors.append(b);
    });
    this.colorRow = colors;

    const papers = el("div", "wpgrid");
    for (const w of WALLPAPERS) {
      const b = button(null, null, "Hintergrund " + w.name);
      b.style.background = w.css;
      b.dataset.wallpaper = w.id;
      b.addEventListener("click", () => {
        applyWallpaper(w.id);
        this.markLook();
      });
      papers.append(b);
    }
    this.paperRow = papers;

    box.append(head, themes, colors, papers);
    return box;
  }

  // markLook shows which of the choices is the one in force. It reads the
  // document and the wallpaper element, not a variable of its own, so the
  // marks cannot drift away from what is on screen.
  markLook() {
    const t = currentTheme();
    for (const b of this.themeRow.children) b.classList.toggle("on", b.dataset.theme === t);
    const accent = getComputedStyle(document.documentElement).getPropertyValue("--accent").trim();
    for (const b of this.colorRow.children) b.classList.toggle("on", b.dataset.accent === accent);
    const bg = this.wallpaper.style.background;
    for (const b of this.paperRow.children) b.classList.toggle("on", b.style.background === bg);
  }

  buildFoot() {
    const foot = el("div", null);
    foot.id = "smfoot";
    const tile = button(null, "Fenster anordnen", null);
    tile.addEventListener("click", () => {
      this.tile();
      this.toggleMenu(false);
    });
    const closeAll = button(null, "Alle schließen", null);
    closeAll.addEventListener("click", () => {
      this.closeAll();
      this.toggleMenu(false);
    });
    foot.append(tile, closeAll);
    return foot;
  }

  buildDesktop(root) {
    this.layer = el("div", null);
    this.layer.id = "desktop";
    this.wallpaper = el("div", null);
    this.wallpaper.id = "wallpaper";
    this.icons = el("div", null);
    this.icons.id = "icons";
    this.snapHint = el("div", null);
    this.snapHint.id = "snaphint";
    this.layer.append(this.wallpaper, this.icons, this.snapHint);
    root.append(this.layer);
  }

  buildNotice(root) {
    const bar = el("div", "noticebar");
    // The live region is built once and stays, empty or not: a region that is
    // created only when there is something to say is announced by nothing.
    this.status = el("p", "status", "");
    this.status.setAttribute("role", "status");
    this.status.setAttribute("aria-live", "polite");
    this.noticeClose = button("noticeclose", "✕", "Meldung schließen");
    this.noticeClose.hidden = true;
    this.noticeClose.addEventListener("click", () => this.clearNotice());
    bar.append(this.status, this.noticeClose);
    root.append(bar);
  }

  wireGlobal() {
    // A click anywhere else closes the start menu - the way every desktop
    // does it, and the only way out that needs no explaining.
    document.addEventListener("pointerdown", (e) => {
      if (!this.menu.contains(e.target) && !this.startBtn.contains(e.target)) this.toggleMenu(false);
    });
    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape" && this.menu.classList.contains("open")) this.toggleMenu(false);
    });
    window.addEventListener("resize", () => {
      for (const w of this.windows.values()) w.keepInView();
    });
  }

  // ---- geometry ------------------------------------------------------------
  area() {
    return { w: this.layer.clientWidth, h: this.layer.clientHeight };
  }

  // top is where the desktop starts on the page. Pointer positions are page
  // positions, and the taskbar sits above the desktop, so docking at "the top
  // edge" has to subtract it.
  top() {
    return this.layer.getBoundingClientRect().top;
  }

  focusTopmost() {
    const rest = [...this.windows.values()].filter((w) => !w.min);
    if (!rest.length) return;
    rest.sort((a, b) => Number(a.el.style.zIndex) - Number(b.el.style.zIndex));
    rest[rest.length - 1].focus();
  }

  // ---- the start menu ------------------------------------------------------
  toggleMenu(open) {
    const o = open === undefined ? !this.menu.classList.contains("open") : open;
    this.menu.classList.toggle("open", o);
    this.startBtn.classList.toggle("open", o);
    this.startBtn.setAttribute("aria-expanded", o ? "true" : "false");
    if (o) {
      this.search.value = "";
      this.renderMenu("");
      this.markLook();
      this.search.focus();
    }
  }

  renderMenu(filter) {
    const f = String(filter ?? "").trim().toLowerCase();
    clear(this.list);
    const hits = this.appList.filter((a) => !f || a.name.toLowerCase().includes(f) || a.id.includes(f));
    if (!hits.length) {
      this.list.append(el("div", "smempty", "Keine Treffer."));
      return;
    }
    for (const a of hits) this.list.append(this.menuItem(a));
  }

  menuItem(a) {
    const it = el("div", "smitem");
    it.setAttribute("role", "menuitem");
    it.tabIndex = 0;
    it.append(this.glyph(a), el("span", "n", a.name));
    if (a.state !== "ok") it.classList.add("off");
    const open = () => {
      this.openApp(a.id);
      this.toggleMenu(false);
    };
    it.addEventListener("click", open);
    it.addEventListener("keydown", (e) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        open();
      }
    });
    return it;
  }

  // The app's own icon is a path inside its ui-tar and may only be shown once
  // its proof checks out (Task 22). Until then every app gets a badge of the
  // same kind, carrying the first character of its name - which is text from
  // outside and goes in as text. A picture that is not proven is a picture
  // somebody else chose.
  glyph(a) {
    const g = el("span", "g", [...a.name][0] ?? "?");
    g.setAttribute("aria-hidden", "true");
    return g;
  }

  // ---- apps ----------------------------------------------------------------
  apps() {
    return this.appList;
  }

  // load asks the API for the apps this user may use and puts them on the
  // desktop. The answer is read by core.parseResult - length, shape and every
  // field - before a single character of it reaches the screen.
  async load() {
    const action = { app: core.APP_DESKTOP, service: core.SERVICE_API, name: core.ACT_APPS };
    let list;
    try {
      const bytes = await this.link.call(action.app, action.service, action.name);
      list = core.parseResult(action, bytes);
    } catch {
      // Why the answer was no good is nothing the person can act on, and
      // saying more would describe the inside of the server.
      this.notice("error", "Die Liste der Apps konnte nicht geladen werden.");
      return;
    }
    this.appList = list;
    this.renderIcons();
    this.renderMenu("");
  }

  renderIcons() {
    clear(this.icons);
    for (const a of this.appList) {
      // One click opens. Double-click is the habit of an installed desktop,
      // and the people this is built for do not have it; one click is the
      // same gesture as in the start menu, so there is nothing to learn.
      const d = button("dicon", null, null);
      d.append(this.glyph(a), el("span", "n", a.name));
      if (a.state !== "ok") d.classList.add("off");
      d.addEventListener("click", () => this.openApp(a.id));
      this.icons.append(d);
    }
  }

  openApp(appID) {
    const a = this.appList.find((x) => x.id === appID);
    if (!a) return null;
    // An app the controller cannot be reached for, or whose licence has run
    // out, opens no window: an empty frame with an error in it looks like a
    // broken app, and this is not the app's fault.
    if (a.state === "unreachable") {
      this.notice("warn", a.name + " ist nicht erreichbar.");
      return null;
    }
    if (a.state === "unlicensed") {
      this.notice("warn", "Keine gültige Lizenz.");
      return null;
    }
    // One window per app: two calculators help nobody, and a second window of
    // the same app is the first thing that confuses a beginner.
    for (const w of this.windows.values()) {
      if (w.appID === appID) {
        if (w.min) w.restore();
        else w.focus();
        return w;
      }
    }

    const win = new Win(this, {
      id: "w" + ++this.uid,
      title: a.name,
      icon: [...a.name][0] ?? "?",
      width: a.width,
      height: a.height,
    });
    win.appID = appID;
    win.open();

    const mount = BUILTIN[appID];
    if (typeof mount === "function") {
      try {
        win.onClose = mount(win) || null;
      } catch {
        this.placeholder(win, "Diese App konnte nicht geöffnet werden.",
          "Bitte das Fenster schließen und es noch einmal versuchen.");
      }
    } else {
      // TO BE REPLACED WITH TASK 22 (and 23, 24 for the two core apps). Every
      // other app needs its verified user interface in a sandboxed frame; the
      // shell does not have one yet, so it says so instead of showing an
      // empty window.
      this.placeholder(win, "Diese App ist noch nicht fertig.",
        "Sie kommt mit einem der nächsten Schritte.");
    }
    return win;
  }

  // Two lines in a window that has nothing to show: what is the matter, and
  // what to do about it. An empty window says neither.
  placeholder(win, why, hint) {
    const pad = el("div", "pad app");
    pad.append(el("p", null, why), el("p", "muted", hint));
    clear(win.body);
    win.body.append(pad);
  }

  // ---- windows -------------------------------------------------------------
  tile() {
    const list = [...this.windows.values()].filter((w) => !w.min);
    if (!list.length) return;
    const a = this.area();
    const cols = Math.ceil(Math.sqrt(list.length));
    const rows = Math.ceil(list.length / cols);
    list.forEach((w, i) => {
      const c = i % cols, r = Math.floor(i / cols);
      const cw = Math.floor(a.w / cols), ch = Math.floor(a.h / rows);
      w.max = false;
      w.el.classList.remove("max");
      w.el.classList.add("anim");
      setTimeout(() => w.el.classList.remove("anim"), 200);
      Object.assign(w.el.style, { left: c * cw + "px", top: r * ch + "px", width: cw + "px", height: ch + "px" });
    });
  }

  closeAll() {
    for (const w of [...this.windows.values()]) w.close();
  }

  // ---- notices -------------------------------------------------------------
  //
  // noticeBytes takes the body of a "notify" control message exactly as it
  // arrived and reads it with core.parseNotice: level, German text within the
  // contract's bounds, and an app that is either empty or an app of a store.
  // A body that does not hold up is dropped without a word - a message the
  // shell cannot vouch for is worse than no message.
  noticeBytes(body) {
    let n;
    try {
      n = core.parseNotice(body);
    } catch {
      return;
    }
    this.notice(n.level, n.text);
  }

  notice(level, txt) {
    clearTimeout(this.noticeTimer);
    text(this.status, txt);
    this.status.className = "status " + (level === "error" || level === "warn" ? level : "info");
    this.noticeClose.hidden = false;
    // An error waits for the person; the other two go by themselves, so the
    // desktop does not fill up with strips nobody closed.
    if (level !== "error") this.noticeTimer = setTimeout(() => this.clearNotice(), NOTICE_MS);
  }

  clearNotice() {
    clearTimeout(this.noticeTimer);
    text(this.status, "");
    this.noticeClose.hidden = true;
  }
}

// startDesktop builds the desktop into root and starts loading the apps. It
// hands the Desktop back at once, before the apps are there: the taskbar, the
// start menu and the look are the shell's own and need nothing from a server.
export function startDesktop(root, { link, session, trust } = {}) {
  const d = new Desktop(root, { link, session, trust });
  applyWallpaper(localStorageWallpaper());
  if (link && typeof link.onNotice === "function") link.onNotice((b) => d.noticeBytes(b));
  d.load();
  return d;
}

// The wallpaper is read here and not in theme.js, because theme.js may not
// touch the document before the desktop has built the element it paints.
function localStorageWallpaper() {
  try {
    return localStorage.getItem("ngc.wallpaper") ?? "";
  } catch {
    return "";
  }
}
