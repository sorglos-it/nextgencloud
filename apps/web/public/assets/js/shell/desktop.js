// The desktop: wallpaper, icons, taskbar, start menu and the windows on it.
//
// Every name, every title and every message that comes from outside goes on
// screen through dom.js, which writes text nodes and nothing else. The
// prototype in docs/prototype.html builds the same surface with innerHTML;
// look and handling are taken from it, that habit is not.
import { el, button, text, clear } from "./dom.js";
import * as core from "../contracts/core.js";
import { Win, minSize } from "./window.js";
import { calcApp } from "../apps/calc.js";
import { UICache, openAppUI } from "./appframe.js";
import {
  ACCENTS, WALLPAPERS, applyAccent, applyTheme, applyWallpaper, currentTheme, toggleTheme,
} from "./theme.js";

// The apps whose face the web interface itself builds: app id to the function
// that fills the window's body. Every other app runs in the fenced frame of
// appframe.js, against its signed user interface.
//
// STILL HERE AFTER TASK 22, AND WHY. The frame exists now and a real app runs
// in it, so the frame is no longer tested only by apps nobody wrote. The
// calculator cannot follow it yet, and the reason is the contract and not the
// work: core.AppParams refuses a core app ("ngc.") for both ngc.desktop/ui
// and ngc.desktop/ui-proof, so "ngc.calc" can never be asked for a user
// interface, let alone a proof of one. Moving it means first deciding what it
// IS - an app of the store like org.sorglos.calculator, which is what
// shared/testdata calls it, or a surface NGC ships and vouches for itself -
// and that decision belongs with Task 25, which builds the surfaces of the
// apps that ship with NGC.
//
// What it costs while it stands here, named so that nobody has to find it
// again: the calculator runs in the shell's own origin, with the whole DOM of
// the shell and localStorage in reach - exactly what the frame takes away
// from every other app.
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

    // The user interfaces that have already been proven, one cache for the
    // whole desktop: an app that is closed and opened again is not fetched
    // and not checked a second time. Task 20 empties it at every new version
    // of the trust list, which is what ties a proof to the list it was
    // checked against.
    this.uiCache = new UICache();

    this.windows = new Map();
    this.focused = null;
    this.zTop = 10;
    this.cascade = 0;
    this.uid = 0;
    this.appList = [];
    this.noticeTimer = 0;
    // Which tile of the start menu carries the one tabindex of the list.
    this.active = 0;

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
      // Type, then press down: that is how somebody without a mouse gets
      // from the search into the list. Without it the search is a dead end.
      if (e.key === "ArrowDown") {
        e.preventDefault();
        this.moveActive("first");
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
    // A role="menu" is walked with the arrow keys and is ONE stop on the way
    // through with Tab. Round 1 found the shell saying "menu" and doing
    // neither: the arrows did nothing, and every tile was a tab stop of its
    // own - a thousand of them with a thousand apps. Either the word goes or
    // the behaviour comes; the behaviour is what the people this is built
    // for need, so here it is.
    //
    // Real focus moves from tile to tile, so there is no aria-activedescendant:
    // where the focus is IS where the focus is, and one answer beats two.
    this.list.addEventListener("keydown", (e) => this.listKey(e));

    menu.append(this.search, head, this.list, this.buildLook(), this.buildFoot());
    // Tab comes round inside the open menu instead of walking off into the
    // desktop behind it - into controls the menu is covering, which is where
    // round 1 found it going.
    menu.addEventListener("keydown", (e) => this.trapTab(e));
    root.append(menu);
    this.menu = menu;
  }

  // The things in the menu that Tab stops at, in the order it meets them.
  // The list contributes exactly one: whichever tile carries the roving
  // tabindex at this moment.
  tabStops() {
    return [...this.menu.querySelectorAll('input, button, [role="menuitem"][tabindex="0"]')];
  }

  trapTab(e) {
    if (e.key !== "Tab") return;
    const stops = this.tabStops();
    const i = stops.indexOf(document.activeElement);
    if (i === -1) return;
    const next = e.shiftKey ? i - 1 : i + 1;
    if (next >= 0 && next < stops.length) return;
    e.preventDefault();
    stops[e.shiftKey ? stops.length - 1 : 0].focus();
  }

  // The keys a menu promises: down and right go on, up and left go back,
  // Home and End go to the ends, and it comes round at both.
  listKey(e) {
    const keys = {
      __proto__: null,
      ArrowDown: 1, ArrowRight: 1, ArrowUp: -1, ArrowLeft: -1, Home: "first", End: "last",
    };
    const how = keys[e.key];
    if (how === undefined) return;
    e.preventDefault();
    this.moveActive(how);
  }

  items() {
    return [...this.list.querySelectorAll(".smitem")];
  }

  // markActive keeps exactly one tile reachable with Tab. Anything else is a
  // list of tab stops as long as the person has apps.
  markActive(i) {
    const items = this.items();
    if (!items.length) return;
    this.active = Math.max(0, Math.min(items.length - 1, i));
    items.forEach((it, n) => { it.tabIndex = n === this.active ? 0 : -1; });
  }

  moveActive(how) {
    const items = this.items();
    if (!items.length) return;
    const i = how === "first" ? 0
      : how === "last" ? items.length - 1
        : (this.active + how + items.length) % items.length;
    this.markActive(i);
    items[this.active].focus();
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
    // The focus has to leave before the menu does. A menu that closes over
    // the element holding the focus leaves it on <body>, and from there the
    // next Tab starts again at the top of the page - which is where round 1
    // found it after Escape and after every app start.
    const hadFocus = !o && this.menu.contains(document.activeElement);
    this.menu.classList.toggle("open", o);
    this.startBtn.classList.toggle("open", o);
    this.startBtn.setAttribute("aria-expanded", o ? "true" : "false");
    if (o) {
      this.search.value = "";
      this.renderMenu("");
      this.markLook();
      this.search.focus();
    } else if (hadFocus) {
      this.startBtn.focus();
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
    // The tiles are built anew on every keystroke in the search, so the one
    // tab stop has to be set anew with them.
    this.markActive(0);
  }

  menuItem(a) {
    const it = el("div", "smitem");
    it.setAttribute("role", "menuitem");
    // -1 for now; markActive hands the one 0 out once the list is complete.
    it.tabIndex = -1;
    it.append(this.glyph(a), el("span", "n", a.name));
    if (a.state !== "ok") it.classList.add("off");
    // Clicking a tile focuses it, and then it is the one the arrows carry on
    // from: the mouse and the keyboard must not hold two different places.
    it.addEventListener("focus", () => this.markActive(this.items().indexOf(it)));
    const open = () => {
      // Closing first, opening second, on purpose. Closing puts the focus
      // back on the Start button, which is where it belongs if no window
      // opens; if one does, the next line moves it on into that window. The
      // other order would close over the focus that was just placed.
      this.toggleMenu(false);
      const win = this.openApp(a.id);
      if (win) win.el.focus();
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
      // ONE BAD APP THROWS THE WHOLE LIST AWAY, and that is meant. The list
      // arrives as one answer from one API; if any part of it broke the
      // contract, there is no way to tell which of the rest was tampered
      // with too. Showing "the good ones" would be showing a list somebody
      // else chose. So none of them is shown.
      //
      // Why the answer was no good is nothing the person can act on, and
      // saying more would describe the inside of the server.
      this.notice("error", "Die Liste der Apps konnte nicht geladen werden.");
      return;
    }
    this.appList = list;
    this.renderIcons();
    this.renderMenu("");
  }

  // The icons fill columns from the left and stop at the desktop's edge. Past
  // roughly a hundred apps the last of them are behind that edge and cannot
  // be clicked - which is all right: the way to an app among a hundred is the
  // search in the start menu, not a wall of icons to read through.
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
      // Every other app: its signed user interface, in the fenced frame.
      // openAppUI takes the window over from here - it fills it, or it says
      // why it will not - and it owns win.onClose from its first line, so a
      // window closed while the interface is still on its way takes its
      // listeners with it.
      openAppUI(win, { app: a, link: this.link, trust: this.trust, cache: this.uiCache });
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
  // tile lays the open windows out side by side.
  //
  // It asks the stylesheet how small a window may be drawn (minSize) before
  // it decides how many fit across. Round 1 found what happens otherwise: on
  // a 375 px phone it made three 187 px columns, .win{min-width} drew 240,
  // and one window stood 52 px off the edge where nothing can reach it. A
  // column narrower than a window is not a column.
  tile() {
    const list = [...this.windows.values()].filter((w) => !w.min);
    if (!list.length) return;
    const a = this.area();
    const min = minSize();
    // As square a grid as the windows allow, but never more columns than fit
    // at the smallest a window may be - and never fewer than one, because a
    // desktop narrower than one window still has to show it.
    const fitting = Math.max(1, Math.floor(a.w / min.w));
    const cols = Math.max(1, Math.min(Math.ceil(Math.sqrt(list.length)), fitting));
    const rows = Math.ceil(list.length / cols);
    const cw = Math.max(min.w, Math.floor(a.w / cols));
    const ch = Math.max(min.h, Math.floor(a.h / rows));
    list.forEach((w, i) => {
      const c = i % cols, r = Math.floor(i / cols);
      // The last clamp is for the desktop too small to hold the grid at all:
      // then the tiles overlap rather than walk off the edge, because a
      // window half outside is a window that cannot be closed.
      const left = Math.max(0, Math.min(c * cw, a.w - cw));
      const top = Math.max(0, Math.min(r * ch, a.h - ch));
      w.max = false;
      w.el.classList.remove("max");
      w.el.classList.add("anim");
      setTimeout(() => w.el.classList.remove("anim"), 200);
      Object.assign(w.el.style, { left: left + "px", top: top + "px", width: cw + "px", height: ch + "px" });
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
