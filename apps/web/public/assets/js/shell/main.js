// Boot of the shell: the look, then the connection, then the browser, then
// the configuration, then the desktop.
import { startDesktop } from "./desktop.js";
import { initTheme } from "./theme.js";
import { loadConfig } from "../net/config.js";
import { anchorFor } from "./trustlist.js";
import { loginView } from "../auth/login.js";
import { setupView } from "../auth/setup.js";
import { el } from "./dom.js";
// A STAND-IN, and the only line of the shell that knows about it. Task 17
// built net/link.js and Task 20 shell/trustlist.js; what is still missing is
// the login (Task 18), which is what gives the session cookie that
// GET /api/v1/trust needs. With it, the two lines that use the stand-in below
// change, fake-link.js is deleted, and nothing else moves.
import { fakeWorld } from "./fake-link.js";

const NEEDED = ["Ed25519", "X25519"];

// The standing advice for somebody who got in with a password alone.
const HINT = "Umstellen auf Passkeys empfohlen";

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
  // anything against, and nothing may be shown at all, not even the mask:
  // a login against an installation we cannot name is a login into the dark.
  //
  // This gate stands BEFORE the login on purpose. The login checks the trust
  // list too, and that is belt and braces, not a duplicate: this one refuses
  // to draw anything, the other one refuses to believe an answer.
  let anchor;
  try {
    anchor = await anchorFor(config);
  } catch {
    anchor = null;
  }
  // config is handed on rather than parked on window: both masks need rp -
  // the login to know whether this installation has passkeys at all, the setup
  // to write the relying party into version 1. A global would be one more
  // thing anything else on the page could write between the check above and
  // that read.
  const root = document.getElementById("root");
  if (anchor === null) {
    // NO ANCHOR ANYWHERE, AND THIS USED TO BE WHERE THE SHELL GAVE UP. There
    // are two ways to get here and only one of them is a fault: an
    // installation that nobody has set up yet, and one that runs while the
    // controller of this web server has not been paired. The first one is what
    // the setup is for, and it is offered right here; the second one says so
    // itself, because GET /api/v1/setup answers E_NOTFOUND as soon as there is
    // an admin and auth/setup.js turns that into the sentence this spot used
    // to show unconditionally.
    //
    // onDone gets the same handover loginView's onSession gets - the setup
    // signs the new admin in itself - so the desktop behind it cannot tell
    // which of the two ways somebody came in.
    setupView(root, { config, onDone: (world) => showDesktop(root, config, world) });
    return;
  }
  // The login. Nothing of the desktop is built before somebody is in.
  loginView(root, { config, onSession: (world) => showDesktop(root, config, world) });
}

// showDesktop puts the desktop up once the login is through.
//
// BOTH HALVES NOW EXIST, AND THE SEAM BETWEEN THEM DOES NOT. The login above
// is real: a real certificate, a real proof, the cookie of the API, the trust
// list checked and the Noise session opened. shell/trustlist.js can follow
// that chain and keep it current. What the DESKTOP eats is still the stand-in,
// because the mock API answers five core actions where fake-link.js answers
// two dozen. Handing the desktop the real link today would take roughly a
// hundred cases in twelve files down with it, and a tree that does not build
// is worse than a stand-in that is named.
//
// WHAT IS MISSING IS ONE PIECE THAT NO TASK OF THE PLAN OWNS: the production
// adapter that offers link.call(app, service, action, params, opts) and
// link.open(...) over net/link.js and net/channel.js, the way fake-link.js
// offers them today. With it, this function loses its first line, the four
// values below come out of `world`, and
//
//   const trust = await startTrust({ anchor, installation, link });
//
// goes in place of the stand-in's trust. Until then the real session is closed
// again right here rather than left open: a socket nothing reads holds a
// channel budget and a session on the API for nothing, and closing it is the
// honest ending.
async function showDesktop(root, config, world) {
  world.link.close();
  const { link, trust, session, releaseKey } = await fakeWorld();
  startDesktop(root, { link, session: session ?? null, trust, config, releaseKey });
  // Somebody who got in with a password alone is told that passkeys are the
  // better way. It belongs on the desktop and not on the mask, because the
  // mask is gone by the time the API says so.
  //
  // A STRIP OF ITS OWN and not the desktop's notice bar, for two reasons that
  // were both measured. The bar clears itself after eight seconds for
  // everything but an error, and this hint is meant to STAY - it is advice,
  // not news. And it is the bar every hostile case reads to prove the desktop
  // said nothing (desktop-hostile.spec.js: `expect(.status).toHaveText("")`),
  // so a standing hint in it would turn two of those cases red for a reason
  // that has nothing to do with what they are about. When task 21 gives the
  // desktop a place for standing advice, this moves there.
  //
  // A PLAIN PARAGRAPH and no live region. dom.note would give it role=status,
  // and the desktop already has exactly one of those - the notice bar - which
  // two cases pin by that very count (desktop-keyboard.spec.js:
  // `getByRole("status")).toHaveCount(1)`, store.spec.js reads the one there
  // is). A live region is for news anyway; this is advice that stands there.
  if (world.passwordHint) document.body.append(el("p", "passkeyhint", HINT));
}

start().catch(() => {
  // A broken connection or a damaged answer must never leave a white page:
  // that is exactly the moment when the person needs to be told what to do.
  message("NGC konnte nicht geladen werden. Bitte die Seite neu laden. Bleibt es dabei, "
    + "läuft NGC gerade nicht.");
});
