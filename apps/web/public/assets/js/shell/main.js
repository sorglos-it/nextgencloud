// Boot of the shell: the look, then the connection, then the browser, then
// the configuration, then the desktop.
import { startDesktop } from "./desktop.js";
import { initTheme } from "./theme.js";
import { loadConfig } from "../net/config.js";
// A STAND-IN, and the only line of the shell that knows about it. Task 17
// builds shell/channel.js and Task 20 shell/trust.js; then this import and
// the two lines that use it below change, fake-link.js is deleted, and
// nothing else moves.
import { fakeWorld } from "./fake-link.js";

const NEEDED = ["Ed25519", "X25519"];

export async function browserFits() {
  // Both are needed for the session keys; a browser without them cannot open
  // a session at all, so the shell says so instead of failing halfway.
  for (const name of NEEDED) {
    try {
      const k = await crypto.subtle.generateKey({ name }, false, name === "Ed25519" ? ["sign", "verify"] : ["deriveBits"]);
      if (!k) return false;
    } catch {
      return false;
    }
  }
  return true;
}

function message(text) {
  const root = document.getElementById("root");
  root.replaceChildren();
  const p = document.createElement("p");
  p.className = "msg";
  p.textContent = text;
  root.append(p);
}

async function start() {
  // The theme first, before anything is drawn: the page would otherwise be
  // painted dark and repainted light one frame later.
  initTheme();
  // Without a secure connection there is no crypto.subtle at all. Checking
  // the browser first would blame the browser for it, and the person would
  // install another one for nothing.
  if (!window.isSecureContext) {
    message("NGC braucht eine sichere Verbindung. Bitte die Seite über https:// aufrufen.");
    return;
  }
  if (!(await browserFits())) {
    message("Dieser Browser ist zu alt für NGC. Bitte Chrome 137, Firefox 130 oder Safari 17.4 "
      + "oder neuer benutzen.");
    return;
  }
  // The configuration says where the shell connects, which trust list it
  // believes and which party a passkey is asked for, so it goes through its
  // contract and not through res.json(). Whatever it does not hold up to gets
  // the same sentence a missing installation gets - the person must not be
  // left guessing, and must learn nothing about the shape of the file either.
  let config;
  try {
    config = await loadConfig();
  } catch {
    message("NGC ist nicht eingerichtet.");
    return;
  }
  // PULLED FORWARD AND UNFINISHED. There is no login yet (Tasks 19 and 20) and
  // no session (Task 17), so the desktop starts straight away and gets the
  // stand-in instead of a channel and an opened trust list. When the login
  // lands, it runs between the configuration and this line and hands over the
  // session and the list it opened.
  //
  // config is handed on rather than parked on window: nothing reads it yet,
  // and the moment something does - the login needs rp, the session needs
  // apis - it takes it from here. A global would be one more thing anything
  // else on the page could write between the check above and that read.
  const { link, trust } = await fakeWorld();
  startDesktop(document.getElementById("root"), { link, session: null, trust, config });
}

start().catch(() => {
  // A broken connection or a damaged answer must never leave a white page:
  // that is exactly the moment when the person needs to be told what to do.
  message("NGC konnte nicht geladen werden. Bitte die Seite neu laden. Bleibt es dabei, "
    + "läuft NGC gerade nicht.");
});
