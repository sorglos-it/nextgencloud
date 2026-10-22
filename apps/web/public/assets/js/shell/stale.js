// Two things for every screen that is built from an answer of the API: when it
// may be old, and how it stops being old without the person pressing Ctrl+F5.
//
// WHY THIS EXISTS AT ALL. shell/changed.js tells every surface about what was
// changed IN THIS WINDOW. What it cannot know is a change somebody else made -
// a second admin who installs an app, a controller that goes offline - because
// no message of the session link says so (the reason is written down there). So
// such a screen CAN be old, and the rule is the one point 60 of the design
// record was written for: a screen that can be old must not look fresh. It says
// since when it has been standing, and it offers the one button that asks
// again, instead of leaving the person to reload the whole desktop.
//
// AND IT ASKS BY ITSELF when the window is looked at again. That is an event
// and not a timer: coming back to a window is exactly the moment a person
// expects to see what is there now, and it costs one question per return
// instead of one per second.
import { button, el, text } from "./dom.js";

// How long two returns to the window have to be apart before the second one
// asks again. Switching tabs with the keyboard raises "visibilitychange" and
// "focus" within the same handful of milliseconds, and one return is one
// question.
const FLOOR_MS = 1500;

// How often the line is rewritten. The text names minutes, so a second would
// be 60 writes for one change of the words.
const TICK_MS = 15000;

// freshness builds the line and the button that belong to one screen.
//
// ask is called with no arguments and builds the screen again. It is called
// when the button is pressed and when the window is looked at again, never
// twice at once: a second call while the first is still running is dropped,
// because two answers painting over each other is the bug shell/store.js's
// turn counter was written against.
//
// The caller gets back {el, done, stop}: the element to put in the panel, the
// word that one build has finished (which restarts the clock), and the way to
// take the listeners down when the window closes.
export function freshness(ask) {
  const line = el("p", "stand");
  // A polite region: this changes while somebody reads something else, and a
  // screen reader that announced every tick of a clock would be unusable.
  line.setAttribute("role", "status");
  const when = el("span", "standtxt", "");
  const again = button("standgo", "Jetzt nachsehen", "Diesen Stand jetzt neu holen");
  line.append(when, again);

  let at = 0;              // when the screen last finished being built
  let busy = false;
  let last = 0;            // when a return to the window last asked
  let timer = 0;

  const draw = () => {
    if (at === 0) {
      text(when, "Stand: wird geholt …");
      return;
    }
    text(when, "Stand: " + ago(Date.now() - at));
  };

  const run = () => {
    if (busy || typeof ask !== "function") return;
    busy = true;
    try {
      const r = ask();
      if (r && typeof r.finally === "function") r.finally(() => { busy = false; });
      else busy = false;
    } catch {
      busy = false;
    }
  };

  const back = () => {
    // "hidden" fires on the way out too, and leaving a window is no reason to
    // ask anything.
    if (document.visibilityState === "hidden") return;
    const now = Date.now();
    if (now - last < FLOOR_MS) return;
    last = now;
    run();
  };

  again.addEventListener("click", run);
  document.addEventListener("visibilitychange", back);
  window.addEventListener("focus", back);
  timer = setInterval(draw, TICK_MS);
  draw();

  return {
    el: line,
    done() {
      at = Date.now();
      busy = false;
      draw();
    },
    stop() {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", back);
      window.removeEventListener("focus", back);
    },
  };
}

// ago says the age in words a person reads, not in numbers they have to
// convert. The step from "gerade eben" to a minute is at 45 seconds and not at
// 60, because a screen that says "gerade eben" for a whole minute is saying
// something it cannot vouch for towards the end of it.
export function ago(ms) {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 45) return "gerade eben";
  const m = Math.round(s / 60);
  if (m < 60) return "vor " + m + (m === 1 ? " Minute" : " Minuten");
  const h = Math.round(m / 60);
  if (h < 24) return "vor " + h + (h === 1 ? " Stunde" : " Stunden");
  const d = Math.round(h / 24);
  return "vor " + d + (d === 1 ? " Tag" : " Tagen");
}
