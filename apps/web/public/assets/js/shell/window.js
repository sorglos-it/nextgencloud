// One window. The prototype builds its frame with innerHTML; here every node
// is created, because titles and names come from outside.
//
// Dragging, resizing, docking and what happens when the viewport shrinks come
// from docs/prototype.html, with pointer events instead of mouse events so a
// finger on a tablet or a phone does the same as a mouse. The smallest size is
// 240 x 150, as it is there.
import { el, button, text } from "./dom.js";
import { fillGlyph } from "./icon.js";

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

// How far a window may be pushed off the desktop while it is dragged: NOT AT
// ALL (docs/gestaltung.md point 2). Thomas chose windows over full-screen
// apps, on the phone too, and this is the price he named with the choice: a
// window that can leave the screen is a window nobody brings back, and on a
// 360 px screen that decides whether the shell stays usable at all.
//
// Before this, 60 px of the title bar and 34 px of its height had to stay
// behind - enough to grab again, which is a weaker promise than the one that
// was decided. clampX and clampY below hold the whole window inside instead.
//
// A window WIDER than the desktop is the one case that cannot be held inside
// it: then it is pinned to the left and top edge, which is where its title
// bar and its three buttons are. Pushing it further would hide exactly those.
function clampX(x, w, area) {
  return area.w >= w ? Math.max(0, Math.min(area.w - w, x)) : 0;
}

function clampY(y, h, area) {
  return area.h >= h ? Math.max(0, Math.min(area.h - h, y)) : 0;
}

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
    // "Rechner Rechner" and every locator that asks for a name by its text
    // would have to know about the glyph.
    //
    // icon is the NAME of one of the shell's own icons (icon.js), not a
    // picture and not a path: window.js never learns where the file lies.
    const g = el("span", "g", null);
    g.setAttribute("aria-hidden", "true");
    if (icon) fillGlyph(g, icon);
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
    // The class is not decoration: the badge used to be a letter and took its
    // size from that text. An icon is a mask on an empty element, so without
    // a size of its own it draws nothing at all - which is exactly what the
    // taskbar did on 24.10.2026 until this line.
    const tg = el("span", "tico", null);
    tg.setAttribute("aria-hidden", "true");
    if (icon) fillGlyph(tg, icon);
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

  // say puts ONE message over this window and nothing else (docs/gestaltung.md
  // point 4): centred, covering the app's own window only, with one button
  // that takes it away. While the Store coughs, the clock beside it goes on
  // running and stays clickable.
  //
  // THE TEXT GOES IN AS TEXT. It comes from an app or from a server, and the
  // decision book has this one twice over: text(), never innerHTML, and never
  // a node built out of what arrived.
  //
  // One message at a time. A second one replaces the first rather than
  // stacking: two popups over one window hide each other's buttons, and the
  // person then has a window they cannot get back to.
  //
  // HOW LONG IT STANDS IS THE RULE THE STRIP ALREADY HAD, moved to the new
  // place and not rewritten: an error waits for the person, and warn and info
  // go by themselves. So a line of good news does not become something to
  // click away, and nothing about which level waits has changed with the
  // place it is shown in. The caller passes the milliseconds; 0 means wait.
  say(level, txt, holdMs = 0) {
    this.clearSay();
    const back = el("div", "winback");
    const box = el("div", "winmsg" + (level === "error" || level === "warn" ? " " + level : ""));
    const p = el("p", null, null);
    // role=alert and not role=status: this one took the window away from the
    // person, so it is read out when it arrives and not when they get round
    // to it.
    //
    // IT SITS ON THE PARAGRAPH and not on the box around it, exactly as
    // .status does in the strip: the role names the message, and a role on
    // the box would make the word on the button part of the message - both
    // for a screen reader reading it out and for a test asking what it says.
    p.setAttribute("role", "alert");
    text(p, txt);
    // "Meldung schließen", the name the strip's own button already carries,
    // and not plain "Schließen": the title bar of this very window has a
    // Schließen on it, and two controls with one name in one window is a
    // window where neither a person nor a screen reader can tell which is
    // which. Measured as a strict-mode violation the moment a case asked for
    // "Schließen" inside the window.
    const go = button("b", "Meldung schließen", null);
    go.addEventListener("click", () => {
      this.clearSay();
      // Back into the window, not nowhere: the focus was moved onto the
      // button of this popup, and a popup that takes the focus with it when
      // it goes leaves the keyboard at the top of the page.
      this.el.focus();
    });
    const row = el("div", "winmsgrow");
    row.append(go);
    box.append(p, row);
    back.append(box);
    this.el.append(back);
    this.sayBack = back;
    // The focus only for a message that waits. Pulling the focus out of the
    // window for a line that takes itself away again would move the keyboard
    // out from under the person's hands while they are working.
    if (!holdMs) go.focus();
    else this.sayTimer = setTimeout(() => this.clearSay(), holdMs);
  }

  clearSay() {
    clearTimeout(this.sayTimer);
    this.sayTimer = null;
    this.sayBack?.remove();
    this.sayBack = null;
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
    // The timer of a message that was still standing over this window. A
    // timeout that fires into a window that is gone is a timeout that keeps
    // the whole window alive in the closure it was made in.
    this.clearSay();
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
        this.el.style.left = clampX(sx - w * rel, w, this.desktop.area()) + "px";
        this.el.style.top = "0px";
        // The drag carries on from where the small window now stands, while
        // sx and sy stay the point that was grabbed - so the window follows
        // the pointer by exactly what it has moved since.
        ox = this.el.offsetLeft;
        oy = this.el.offsetTop;
      }
      const a = this.desktop.area();
      const W = this.el.offsetWidth, H = this.el.offsetHeight;
      // The whole window stays on the desktop - all four edges, not just
      // enough of the title bar to grab. The pointer may leave the desktop;
      // the window does not follow it out.
      this.el.style.left = clampX(ox + e.clientX - sx, W, a) + "px";
      this.el.style.top = clampY(oy + e.clientY - sy, H, a) + "px";

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
    // CUT TO FIT FIRST, then moved inside. open() already cuts a window to
    // the desktop it opens on; a desktop that shrinks afterwards - a phone
    // turned on its side, a browser window pulled narrow - leaves the same
    // window too big for it, and moving a window that does not fit can only
    // choose which of its edges to lose. Measured: the App-Store opens 760
    // wide, and on a 420 px desktop pinning it to the left put its three
    // title-bar buttons - the only way to close it - off the screen.
    //
    // Never below the smallest a window may be drawn: below that the three
    // buttons stop fitting, which is the same loss by another route.
    const min = minSize();
    const w = Math.max(min.w, Math.min(this.el.offsetWidth, a.w));
    const h = Math.max(min.h, Math.min(this.el.offsetHeight, a.h));
    Object.assign(this.el.style, {
      width: w + "px", height: h + "px",
      // The same clamp the drag uses, so a window that was inside the desktop
      // is still inside it afterwards. It used to keep 80 px of the width and
      // 40 px of the height and leave the rest off the screen.
      left: clampX(this.el.offsetLeft, w, a) + "px",
      top: clampY(this.el.offsetTop, h, a) + "px",
    });
  }
}
