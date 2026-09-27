// Boot of the shell: the look, then the connection, then the browser, then
// the configuration, then the desktop.
import { startDesktop } from "./desktop.js";
import { initTheme } from "./theme.js";
import { loadConfig } from "../net/config.js";
import { loginView } from "../auth/login.js";
import { el } from "./dom.js";
// A STAND-IN, and the only line of the shell that knows about it. Task 17
// builds shell/channel.js and Task 20 shell/trust.js; then this import and
// the two lines that use it below change, fake-link.js is deleted, and
// nothing else moves.
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
  // The login. Nothing of the desktop is built before somebody is in, and
  // config is handed on rather than parked on window: the mask needs rp to
  // know whether this installation has passkeys at all. A global would be one
  // more thing anything else on the page could write between the check above
  // and that read.
  const root = document.getElementById("root");
  loginView(root, { config, onSession: (world) => showDesktop(root, config, world) });
}

// showDesktop puts the desktop up once the login is through.
//
// HALF PULLED FORWARD, AND THIS IS THE HALF THAT IS NOT DONE. The login above
// is real: a real certificate, a real proof, the cookie of the API, the trust
// list checked and the Noise session opened. What the DESKTOP eats is still
// the stand-in, because its data path is tasks 21 to 25 and the mock API
// answers five core actions where fake-link.js answers two dozen. Handing the
// desktop the real link today would take every one of those cases down with
// it, and a tree that does not build is worse than a stand-in that is named.
//
// So the real session is closed again right here rather than left open. A
// socket nothing reads is a socket that holds a channel budget and a session
// on the API for nothing, and closing it is the honest ending until the
// desktop can be handed the thing itself: one line goes then, and the four
// values below come out of `world`.
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
