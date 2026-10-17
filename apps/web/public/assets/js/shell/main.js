// Boot of the shell: the look, then the connection, then the browser, then
// the configuration, then the desktop.
import { startDesktop } from "./desktop.js";
import { initTheme } from "./theme.js";
import { loadConfig } from "../net/config.js";
import { anchorFor } from "./trustlist.js";
import { loginView } from "../auth/login.js";
import { setupView } from "../auth/setup.js";
import { el } from "./dom.js";
import { realWorld } from "../net/world.js";
// A STAND-IN, and the only line of the shell that still knows about it. What
// it is for and when it goes stands at standIn() below.
import { fakeWorld } from "./fake-link.js";
import { t } from "../lang.js";

const NEEDED = ["Ed25519", "X25519"];

// The standing advice for somebody who got in with a password alone.
const HINT = t("shell.passkeyhint");

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
    message(t("shell.insecure"));
    return;
  }
  if (!(await browserFits())) {
    message(t("shell.oldbrowser"));
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
    message(t("page.notsetup"));
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
    // which of the two ways somebody came in. It gets ONE thing more: the
    // anchor. `anchor` above is null here by definition - that is why this
    // branch was taken - and the one that exists afterwards is the one the
    // setup just wrote. Task 17b gave showDesktop that parameter and updated
    // the login's call below; this one still handed it three arguments, so
    // `anchor` took the handover and `world` was undefined. MEASURED on the
    // merged tree: "Cannot read properties of undefined (reading 'session')",
    // an unhandled rejection, and a WHITE PAGE right after an installation
    // was set up - the exact dead end this whole branch exists to remove.
    //
    // The same .catch as the login below, and for the same reason: a trust
    // list that does not open has put its own alarm up, anything else broke on
    // the way to the desktop, and a white page is the one answer that helps
    // nobody.
    setupView(root, {
      config,
      onDone: (world, setupAnchor) => showDesktop(root, config, setupAnchor, world).catch(() => {
        message(t("shell.loadfailed"));
      }),
    });
    return;
  }
  // The login. Nothing of the desktop is built before somebody is in. root and
  // config come from above - the setup mask needs them too, so they are won
  // once for both ways through this function.
  loginView(root, {
    config,
    onSession: (world) => showDesktop(root, config, anchor, world).catch(() => {
      // A trust list that does not open has already put its own alarm on the
      // screen and ended the session; anything else broke on the way to the
      // desktop, and a white page is the one answer that helps nobody.
      message(t("shell.loadfailed"));
    }),
  });
}

// standIn reports whether this page is to eat shell/fake-link.js instead of
// the real session.
//
// IT IS A TEST HOOK AND IT LEAVES WITH THAT FILE. The stand-in answers two
// dozen actions; the mock API of apps/web/tests answers five, and none of them
// with the signed user interface the app frame needs. So roughly a hundred
// cases in twelve files still measure the shell against the stand-in, and
// signIn() in apps/web/tests/fixtures/page.js sets this for them - in ONE
// place, named, so "which cases still hang on the stand-in" is a line in a
// file and not a guess. A case that is ready asks for the real way instead
// (tests/shell/world.spec.js does).
//
// The DEFAULT is the real session, which is the whole point: in operation
// nothing sets this, the link that was just opened stays open, and the desktop
// eats what the API really sent.
//
// HOW TO SEE THAT THIS MAY GO, so nobody has to judge it: when no case asks for
// the stand-in any more. That is one command -
//
//   grep -rn standInOn apps/web/tests --include=*.js
//
// and it is over when the only hits left are the definition of standInOn in
// apps/web/tests/fixtures/page.js and the comments around it. Then this
// function, the `standIn` argument of signIn(), standInOn() itself and
// shell/fake-link.js all go in one commit - a lever that lays the workplace
// onto made-up data has no business in a shipped shell.
const standIn = () => window.__ngcStandIn === true;

// showDesktop puts the desktop up once the login is through.
//
// THE SEAM IS HERE, and until this round there was none: the login is real - a
// real certificate, a real proof, the cookie of the API, the trust list
// checked and the Noise session opened - and what the desktop ate was the
// stand-in, so the real session was closed again right here. net/world.js is
// what stands in that gap now: it offers the surfaces' own
// call(app, service, name, params, opts) and open(...) over net/link.js and
// net/channel.js, and it opens the trust list against the anchor on the
// session that was just opened.
//
// WHERE THE CHAIN OF TASK 28 RUNS TOGETHER: world.sessionDH is the key pair
// auth/login.js built for this session, and this line is the only one that
// carries it. It goes into net/world.js and from there into the adapter, which
// needs it for the end-to-end channel of an app's own service; it is NOT put
// into what startDesktop gets, because no surface of the desktop has anything to
// do with a key.
async function showDesktop(root, config, anchor, world) {
  let parts;
  if (standIn()) {
    // The real session is closed again rather than left open: a socket nothing
    // reads holds a channel budget and a session on the API for nothing.
    world.link.close();
    parts = await fakeWorld();
  } else {
    parts = await realWorld({
      config, anchor, session: world.session, link: world.link, sessionDH: world.sessionDH,
    });
  }
  const { link, trust, session, releaseKey } = parts;
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
  message(t("shell.loadfailed"));
});
