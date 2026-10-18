// One window. The prototype builds its frame with innerHTML; here every node
// is created, because titles and names come from outside.
//
// Dragging, resizing, docking and what happens when the viewport shrinks come
// from docs/prototype.html, with pointer events instead of mouse events so a
// finger on a tablet or a phone does the same as a mouse. The smallest size is
// 240 x 150, as it is there.
import { el, button, text } from "./dom.js";
import { t } from "../lang.js";

// The smallest a window may get. Below that the title bar's three buttons
// stop fitting and the window can no longer be closed by clicking.
//
// THE NUMBER LIVES IN THE STYLESHEET, in --win-min-w and --win-min-h, and
// .win{min-width} draws from the same two. Round 1 found why that matters:
// tile() divided the desktop into 187 px columns while the browser drew 240,
// and a window stood 52 px off the edge of a phone screen. Two numbers that
// mean the same thing will drift; one of them has to know the other, and the
// one that draws is the one to ask.
//
// The fallbacks are what the stylesheet says today. They are reached only if
// the stylesheet did not load at all, and then the window would be unstyled
// anyway - a number here is better than NaN.
const MIN_FALLBACK = { w: 240, h: 150 };
let measuredMin = null;

export function minSize() {
  if (measuredMin) return measuredMin;
  const cs = getComputedStyle(document.documentElement);
  const w = parseInt(cs.getPropertyValue("--win-min-w"), 10);
  const h = parseInt(cs.getPropertyValue("--win-min-h"), 10);
  measuredMin = {
    w: Number.isFinite(w) && w > 0 ? w : MIN_FALLBACK.w,
    h: Number.isFinite(h) && h > 0 ? h : MIN_FALLBACK.h,
  };
  return measuredMin;
}

// How close to an edge the pointer has to come before the window docks there.
const SNAP_EDGE = 12;

// How far the pointer has to travel before a press counts as a drag. It only
// decides when a maximized window is torn loose, and it has to be larger than
// the jitter of a hand double-clicking and smaller than a movement anybody
// means.
const TEAR = 4;

// How much of a window has to stay on the desktop while it is dragged: enough
// of the title bar to grab it again.
const KEEP_X = 60;
const KEEP_Y = 34;

export class Win {
  constructor(desktop, { id, title, icon, width, height }) {
    this.desktop = desktop;
    this.id = id;
    this.min = false;
    this.max = false;
    this.prev = null;
    this.onClose = null;

    this.el = el("div", "win");
    // A window can be given the focus without being a tab stop: the desktop
    // sends it there when the app is started, so the keyboard carries on
    // inside the window it just asked for instead of at the top of the page.
    this.el.tabIndex = -1;
    const bar = el("div", "titlebar");
    // The glyph says nothing a screen reader needs; the title right beside it
    // says all of it. Without aria-hidden the window's name would read
    // "R Rechner" and every locator that asks for a name by its text would
    // have to know about the glyph.
    const g = el("span", "g", icon ?? "");
    g.setAttribute("aria-hidden", "true");
    bar.append(g, el("span", "t", title));
    const btns = el("div", "wbtns");
    for (const [cls, label] of [["mini", t("window.minimize")], ["maxi", t("window.maximize")], ["close", t("window.close")]]) {
      const b = button("wbtn " + cls, cls === "mini" ? "—" : cls === "maxi" ? "▢" : "✕", label);
      b.title = label;
      btns.append(b);
    }
    bar.append(btns);
    this.body = el("div", "body");
    this.el.append(bar, this.body);
    for (const dir of ["n", "e", "s", "w", "se", "sw"]) this.el.append(el("div", "rs " + dir));

    // The window opens cascaded, at most as large as the desktop minus a
    // margin, like the prototype does.
    const a = desktop.area();
    const w = Math.min(width || 560, Math.max(280, a.w - 40));
    const h = Math.min(height || 400, Math.max(180, a.h - 40));
    const off = (desktop.cascade++ % 8) * 26;
    Object.assign(this.el.style, {
      left: Math.max(8, Math.min(a.w - w - 8, 70 + off)) + "px",
      top: Math.max(8, Math.min(a.h - h - 8, 40 + off)) + "px",
      width: w + "px", height: h + "px",
    });

    // The taskbar entry is a button: it is pressed, and a keyboard has to
    // reach it like every other control in the bar.
    this.tab = button("task", null, t("window.task", { title }));
    const tg = el("span", null, icon ?? "");
    tg.setAttribute("aria-hidden", "true");
    this.tab.append(tg, el("span", "lbl", title));
    this.tab.addEventListener("click", () => {
      if (this.min) this.restore();
      else if (this.desktop.focused === this) this.minimize();
      else this.focus();
    });

    btns.children[0].addEventListener("click", (e) => { e.stopPropagation(); this.minimize(); });
    btns.children[1].addEventListener("click", (e) => { e.stopPropagation(); this.toggleMax(); });
    btns.children[2].addEventListener("click", (e) => { e.stopPropagation(); this.close(); });
    this.el.addEventListener("pointerdown", () => this.focus(), true);
    this.makeDraggable(bar);
    this.makeResizable();
  }

  setTitle(title) {
    text(this.el.querySelector(".t"), title);
    text(this.tab.querySelector(".lbl"), title);
    this.tab.setAttribute("aria-label", t("window.task", { title }));
  }

  open() {
    this.desktop.layer.append(this.el);
    this.desktop.tasks.append(this.tab);
    this.desktop.windows.set(this.id, this);
    this.focus();
    return this;
  }

  focus() {
    const d = this.desktop;
    // THE KEYBOARD GOES INTO THE APP, and it is done before the early return
    // below: a window that already has the focus of the desktop may all the
    // same have lost the keyboard to the taskbar or the start menu, and
    // clicking it is how a person asks for it back.
    //
    // An app runs in a frame with a document of its own, so a key press
    // reaches it only while the browser has given that frame the focus - the
    // shell cannot forward keys to an app, and must not: reading them would be
    // the shell listening in on everything typed into an app. A window whose
    // body the shell built itself (the app store, a refusal, a placeholder)
    // has no frame, and then nothing happens here.
    const frame = this.body.querySelector("iframe.appframe");
    if (frame && !this.min) frame.focus();
    if (d.focused === this && !this.min) return;
    if (d.focused && d.focused !== this) {
      d.focused.el.classList.remove("focus");
      d.focused.tab.classList.remove("active");
    }
    d.focused = this;
    this.el.classList.add("focus");
    this.tab.classList.add("active");
    this.el.style.zIndex = String(++d.zTop);
  }

  minimize() {
    this.min = true;
    this.el.classList.add("min");
    this.tab.classList.add("min");
    this.el.classList.remove("focus");
    this.tab.classList.remove("active");
    if (this.desktop.focused === this) this.desktop.focused = null;
    this.desktop.focusTopmost();
  }

  restore() {
    this.min = false;
    this.el.classList.remove("min");
    this.tab.classList.remove("min");
    this.focus();
  }

  // animate is false where the caller reads the new size back straight away:
  // during the .win.anim cross-fade offsetWidth still answers the OLD size,
  // and round 1 found what that costs - a window torn loose from maximized
  // jumped to the left edge of the screen every time.
  toggleMax(animate = true) {
    const a = this.desktop.area();
    // The animation class comes off again by itself: a window left in
    // transition would drag with a delay for the rest of its life.
    if (animate) {
      this.el.classList.add("anim");
      setTimeout(() => this.el.classList.remove("anim"), 200);
    }
    if (this.max) {
      Object.assign(this.el.style, this.prev || { left: "60px", top: "40px", width: "600px", height: "420px" });
      this.max = false;
      this.el.classList.remove("max");
    } else {
      this.prev = {
        left: this.el.style.left, top: this.el.style.top,
        width: this.el.style.width, height: this.el.style.height,
      };
      Object.assign(this.el.style, { left: "0px", top: "0px", width: a.w + "px", height: a.h + "px" });
      this.max = true;
      this.el.classList.add("max");
    }
  }

  close() {
    // An app may have a timer, a listener or a channel running. It gets its
    // say before the nodes are gone, and a throw from it must not leave the
    // window standing - a window that cannot be closed is worse than an app
    // that did not tidy up.
    try {
      if (typeof this.onClose === "function") this.onClose();
    } catch {
      // Swallowed on purpose; see above.
    }
    this.el.remove();
    this.tab.remove();
    this.desktop.windows.delete(this.id);
    if (this.desktop.focused === this) this.desktop.focused = null;
    this.desktop.focusTopmost();
  }

  // ---- dragging (mouse, pen and finger, all through pointer events) --------
  makeDraggable(bar) {
    let sx = 0, sy = 0, ox = 0, oy = 0, dragging = false, torn = false, snap = null;

    bar.addEventListener("dblclick", (e) => {
      if (!e.target.closest(".wbtn")) this.toggleMax();
    });

    bar.addEventListener("pointerdown", (e) => {
      if (e.target.closest(".wbtn") || e.button === 2) return;
      this.focus();
      dragging = true;
      torn = false;
      snap = null;
      // A maximized window is NOT torn loose here. Round 1: pressing the
      // title bar restored it, and the double click that followed maximized
      // it again - four double clicks, four times maximized, and the one
      // gesture everybody tries first could not make a window small again.
      // The tearing waits for a real drag; see the pointermove below.
      sx = e.clientX; sy = e.clientY;
      ox = this.el.offsetLeft; oy = this.el.offsetTop;
      // Pointer capture keeps the moves coming even when the pointer leaves
      // the title bar - without it a fast drag loses the window.
      try { bar.setPointerCapture(e.pointerId); } catch { /* not every pointer can be captured */ }
      e.preventDefault();
    });

    bar.addEventListener("pointermove", (e) => {
      if (!dragging) return;
      if (this.max && !torn) {
        // Below the threshold this is still a click, not a drag, and a click
        // on a title bar does nothing. Above it, tear the window loose under
        // the pointer: it keeps the spot on the title bar that was grabbed,
        // so it does not jump away.
        if (Math.abs(e.clientX - sx) < TEAR && Math.abs(e.clientY - sy) < TEAR) return;
        torn = true;
        const rel = sx / this.desktop.area().w;
        // No cross-fade: offsetWidth is read on the very next line, and
        // during a transition it answers the size the window is leaving.
        this.toggleMax(false);
        const w = this.el.offsetWidth;
        this.el.style.left = Math.max(0, sx - w * rel) + "px";
        this.el.style.top = "0px";
        // The drag carries on from where the small window now stands, while
        // sx and sy stay the point that was grabbed - so the window follows
        // the pointer by exactly what it has moved since.
        ox = this.el.offsetLeft;
        oy = this.el.offsetTop;
      }
      const a = this.desktop.area(), W = this.el.offsetWidth;
      let nx = ox + e.clientX - sx;
      let ny = oy + e.clientY - sy;
      nx = Math.max(-W + KEEP_X, Math.min(a.w - KEEP_X, nx));
      ny = Math.max(0, Math.min(a.h - KEEP_Y, ny));
      this.el.style.left = nx + "px";
      this.el.style.top = ny + "px";

      // Docking preview at the edges of the desktop.
      let s = null;
      const top = this.desktop.top();
      if (e.clientY - top <= SNAP_EDGE) s = { left: 0, top: 0, width: a.w, height: a.h };
      else if (e.clientX <= SNAP_EDGE) s = { left: 0, top: 0, width: Math.round(a.w / 2), height: a.h };
      else if (e.clientX >= a.w - SNAP_EDGE) {
        s = { left: Math.round(a.w / 2), top: 0, width: Math.round(a.w / 2), height: a.h };
      }
      snap = s;
      const hint = this.desktop.snapHint;
      if (s) {
        Object.assign(hint.style, {
          display: "block", left: s.left + "px", top: s.top + "px",
          width: s.width + "px", height: s.height + "px",
        });
      } else hint.style.display = "none";
    });

    const end = (e) => {
      if (!dragging) return;
      dragging = false;
      this.desktop.snapHint.style.display = "none";
      try { bar.releasePointerCapture(e.pointerId); } catch { /* never captured */ }
      if (snap) {
        this.prev = {
          left: this.el.style.left, top: this.el.style.top,
          width: this.el.offsetWidth + "px", height: this.el.offsetHeight + "px",
        };
        this.el.classList.add("anim");
        setTimeout(() => this.el.classList.remove("anim"), 200);
        Object.assign(this.el.style, {
          left: snap.left + "px", top: snap.top + "px",
          width: snap.width + "px", height: snap.height + "px",
        });
        this.max = snap.width === this.desktop.area().w;
        this.el.classList.toggle("max", this.max);
        snap = null;
      }
    };
    bar.addEventListener("pointerup", end);
    bar.addEventListener("pointercancel", end);
  }

  // ---- resizing ------------------------------------------------------------
  makeResizable() {
    for (const h of this.el.querySelectorAll(".rs")) {
      const dir = h.classList[1];
      let sx = 0, sy = 0, ox = 0, oy = 0, ow = 0, oh = 0, on = false;
      h.addEventListener("pointerdown", (e) => {
        on = true;
        this.focus();
        sx = e.clientX; sy = e.clientY;
        ox = this.el.offsetLeft; oy = this.el.offsetTop;
        ow = this.el.offsetWidth; oh = this.el.offsetHeight;
        try { h.setPointerCapture(e.pointerId); } catch { /* not every pointer can be captured */ }
        e.preventDefault();
        e.stopPropagation();
      });
      h.addEventListener("pointermove", (e) => {
        if (!on) return;
        const min = minSize();
        const dx = e.clientX - sx, dy = e.clientY - sy;
        let l = ox, t = oy, w = ow, hh = oh;
        if (dir.includes("e")) w = Math.max(min.w, ow + dx);
        if (dir.includes("s")) hh = Math.max(min.h, oh + dy);
        // West and north drag the NEAR edge, so the far one must not move.
        // Both are written as "hold the far edge, put the near one where it
        // may go, and let the size follow" - round 1 found what the other way
        // round costs: the top was clamped at the desktop's edge and the
        // height went on growing, so the bottom edge walked 193 px down the
        // screen. Only the top is clamped, because only the top has the
        // taskbar above it that a window must not go behind.
        if (dir.includes("w")) {
          const right = ox + ow;
          l = Math.min(right - min.w, ox + dx);
          w = right - l;
        }
        if (dir.includes("n")) {
          const bottom = oy + oh;
          t = Math.min(bottom - min.h, Math.max(0, oy + dy));
          hh = bottom - t;
        }
        Object.assign(this.el.style, { left: l + "px", top: t + "px", width: w + "px", height: hh + "px" });
      });
      const end = (e) => {
        on = false;
        try { h.releasePointerCapture(e.pointerId); } catch { /* never captured */ }
      };
      h.addEventListener("pointerup", end);
      h.addEventListener("pointercancel", end);
    }
  }

  // keepInView pulls a window back when the desktop got smaller under it, so
  // nothing ends up where it cannot be reached any more.
  keepInView() {
    const a = this.desktop.area();
    if (this.max) {
      Object.assign(this.el.style, { left: "0px", top: "0px", width: a.w + "px", height: a.h + "px" });
      return;
    }
    this.el.style.left = Math.min(this.el.offsetLeft, Math.max(0, a.w - 80)) + "px";
    this.el.style.top = Math.min(this.el.offsetTop, Math.max(0, a.h - 40)) + "px";
  }
}
