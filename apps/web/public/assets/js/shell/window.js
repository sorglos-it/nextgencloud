// One window. The prototype builds its frame with innerHTML; here every node
// is created, because titles and names come from outside.
//
// Dragging, resizing, docking and what happens when the viewport shrinks come
// from docs/prototype.html, with pointer events instead of mouse events so a
// finger on a tablet or a phone does the same as a mouse. The smallest size is
// 240 x 150, as it is there.
import { el, button, text } from "./dom.js";

// The smallest a window may get. Below that the title bar's three buttons
// stop fitting and the window can no longer be closed by clicking.
const MIN_W = 240;
const MIN_H = 150;

// How close to an edge the pointer has to come before the window docks there.
const SNAP_EDGE = 12;

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
    const bar = el("div", "titlebar");
    // The glyph says nothing a screen reader needs; the title right beside it
    // says all of it. Without aria-hidden the window's name would read
    // "R Rechner" and every locator that asks for a name by its text would
    // have to know about the glyph.
    const g = el("span", "g", icon ?? "");
    g.setAttribute("aria-hidden", "true");
    bar.append(g, el("span", "t", title));
    const btns = el("div", "wbtns");
    for (const [cls, label] of [["mini", "Minimieren"], ["maxi", "Maximieren"], ["close", "Schließen"]]) {
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
    this.tab = button("task", null, "Fenster " + title);
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

  setTitle(t) {
    text(this.el.querySelector(".t"), t);
    text(this.tab.querySelector(".lbl"), t);
    this.tab.setAttribute("aria-label", "Fenster " + t);
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

  toggleMax() {
    const a = this.desktop.area();
    // The animation class comes off again by itself: a window left in
    // transition would drag with a delay for the rest of its life.
    this.el.classList.add("anim");
    setTimeout(() => this.el.classList.remove("anim"), 200);
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
    let sx = 0, sy = 0, ox = 0, oy = 0, dragging = false, snap = null;

    bar.addEventListener("dblclick", (e) => {
      if (!e.target.closest(".wbtn")) this.toggleMax();
    });

    bar.addEventListener("pointerdown", (e) => {
      if (e.target.closest(".wbtn") || e.button === 2) return;
      this.focus();
      dragging = true;
      snap = null;
      if (this.max) {
        // Tear a maximized window loose under the pointer: it keeps the spot
        // on the title bar that was grabbed, so it does not jump away.
        const rel = e.clientX / this.desktop.area().w;
        this.toggleMax();
        const w = this.el.offsetWidth;
        this.el.style.left = Math.max(0, e.clientX - w * rel) + "px";
        this.el.style.top = "0px";
      }
      sx = e.clientX; sy = e.clientY;
      ox = this.el.offsetLeft; oy = this.el.offsetTop;
      // Pointer capture keeps the moves coming even when the pointer leaves
      // the title bar - without it a fast drag loses the window.
      try { bar.setPointerCapture(e.pointerId); } catch { /* not every pointer can be captured */ }
      e.preventDefault();
    });

    bar.addEventListener("pointermove", (e) => {
      if (!dragging) return;
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
        const dx = e.clientX - sx, dy = e.clientY - sy;
        let l = ox, t = oy, w = ow, hh = oh;
        if (dir.includes("e")) w = Math.max(MIN_W, ow + dx);
        if (dir.includes("s")) hh = Math.max(MIN_H, oh + dy);
        if (dir.includes("w")) { w = Math.max(MIN_W, ow - dx); l = ox + (ow - w); }
        if (dir.includes("n")) { hh = Math.max(MIN_H, oh - dy); t = Math.max(0, oy + (oh - hh)); }
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
