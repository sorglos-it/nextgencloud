// Boot of the shell: the look, then the connection, then the browser, then
// the configuration, then the desktop.
import { startDesktop } from "./desktop.js";
import { initTheme } from "./theme.js";
import { loadConfig } from "../net/config.js";
import { anchorFor, NOT_SET_UP } from "./trustlist.js";
// A STAND-IN, and the only line of the shell that knows about it. Task 17
// built net/link.js and Task 20 shell/trustlist.js; what is still missing is
// the login (Task 18), which is what gives the session cookie that
// GET /api/v1/trust needs. With it, the two lines that use the stand-in below
// change, fake-link.js is deleted, and nothing else moves.
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
  // THE ROOT OF ALL OF IT: which keys this workplace believes. The anchor
  // comes from the page's own server, or - while the web controller is not
  // paired and /config.json therefore carries none - out of what the initial
  // setup left in this browser. Without it there is no trust list to check
  // anything against, and nothing may be shown at all; the sentence names
  // what is missing instead of leaving the person with a page that does
  // nothing. Task 19 puts the initial setup in the place of that message.
  let anchor;
  try {
    anchor = await anchorFor(config);
  } catch {
    anchor = null;
  }
  if (anchor === null) {
    message(NOT_SET_UP);
    return;
  }
  // PULLED FORWARD AND UNFINISHED. There is no login yet (Task 18), so there
  // is no session cookie, and GET /api/v1/trust answers 401 without one: the
  // desktop starts straight away and gets the stand-in instead of an opened
  // trust list. THE ONE LINE THAT IS STILL MISSING, and where it goes - right
  // here, after the login has the cookie and before startDesktop:
  //
  //   const trust = await startTrust({ anchor, installation, link });
  //
  // startTrust (shell/trustlist.js) opens the chain against this anchor,
  // follows it to the newest version, keeps it up to date from the session's
  // trust hints and raises the alarm when it breaks. Its answer goes to
  // startDesktop in place of the stand-in's `trust`.
  //
  // config is handed on rather than parked on window: nothing reads it yet,
  // and the moment something does - the login needs rp, the session needs
  // apis - it takes it from here. A global would be one more thing anything
  // else on the page could write between the check above and that read.
  // The session is null while there is no login (Tasks 19 and 20). It is
  // taken from the stand-in rather than written as null here, so that the one
  // line which will hand over the real session is already the line that runs:
  // "Benutzer und Rechte" asks the session to sign a new trust list and for
  // the API's clock, and says plainly that it cannot change anything while
  // there is none.
  const { link, trust, session, releaseKey } = await fakeWorld();
  startDesktop(document.getElementById("root"),
    { link, session: session ?? null, trust, config, releaseKey });
}

start().catch(() => {
  // A broken connection or a damaged answer must never leave a white page:
  // that is exactly the moment when the person needs to be told what to do.
  message("NGC konnte nicht geladen werden. Bitte die Seite neu laden. Bleibt es dabei, "
    + "läuft NGC gerade nicht.");
});
