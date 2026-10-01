import { test as base } from "@playwright/test";
import { USER_NAME, PASSWORD } from "./logins.js";

// The shell page for every test. The content security policy stays on, so a
// test notices when the page starts breaking it. The contract tests import
// the modules of the page inside page.evaluate, which Playwright runs over
// the protocol, and the dynamic import of /assets/... is script-src 'self'
// and allowed - nothing here needs the policy switched off. A separate test
// checks the policy itself (Task 26).
export const test = base.extend({
  shell: async ({ page }, use) => {
    await page.goto("/");
    await use(page);
  },
});

export { expect } from "@playwright/test";

// signIn puts anna in. Since task 18 the shell shows the login mask before
// anything else, so every case that wants a desktop starts here - the four
// lines the briefs of tasks 21 to 25 always had, and the line those files
// promised to grow back "when the login lands".
//
// It is ONE function and not four lines in a dozen files, because the mask is
// going to change again: task 19 puts the initial setup in front of it, task
// 20 an alarm behind it. A caption that moves then moves in one place.
//
// It presses the button and does NOT wait for the desktop: what a caller
// waits for is its own business, and the case that has no desktop to wait for
// (shell/boot.spec.js on an engine without the session keys) never gets here.
//
// standIn: false is how a case asks for the real way instead - see standInOn
// below; tests/shell/world.spec.js is the one case that does.
export async function signIn(page, { standIn = true } = {}) {
  if (standIn) await standInOn(page);
  await page.getByLabel("Benutzername").fill(USER_NAME);
  await page.getByLabel("Passwort").fill(PASSWORD);
  // exact, because "Mit Passkey anmelden" also contains the word: playwright
  // matches an accessible name as a case-insensitive substring.
  await page.getByRole("button", { name: "Anmelden", exact: true }).click();
}

// standInOn hands this page shell/fake-link.js instead of the real session.
//
// WHICH CASES STILL EAT THE STAND-IN, in one place and by name. Since task 17b
// shell/main.js takes the REAL session: net/world.js puts the surfaces'
// call(app, service, name, …) on net/link.js and opens the trust list on it.
// The stand-in answers two dozen actions where the mock API answers five, and
// none of them with the signed user interface the app frame needs - so every
// case that wants a desktop with apps, a store, a user administration or a
// running app still needs it and asks for it through here: signIn() for every
// case that goes through signIn, and shell/login.spec.js for its own cases,
// which type into the mask themselves.
//
// BOTH WAYS OF SETTING IT, and each one is needed. addInitScript covers every
// navigation that follows - the passkey cases move the page to its DNS name,
// and a flag set on the old document would be gone there. evaluate covers the
// document that is already standing, which is where signIn finds it; the hook
// is read when the login is through, so that is in time.
//
// IT LEAVES WITH shell/fake-link.js: when the mock answers every action of
// core.ACTIONS with a signed world, this function and that file go together.
//
// HOW TO SEE THAT IT MAY GO, countable and not a matter of judgement:
//
//   grep -rn standInOn apps/web/tests --include=*.js
//
// is over when the only hits left are this definition and the comments around
// it - no signIn() without `standIn: false`, no beforeEach calling it. Then
// this function, the `standIn` argument above, standIn() in
// shell/main.js and shell/fake-link.js go in one commit.
export async function standInOn(page) {
  await page.addInitScript(() => { window.__ngcStandIn = true; });
  try {
    await page.evaluate(() => { window.__ngcStandIn = true; });
  } catch {
    // No document on this page yet; the init script above is what sets it.
  }
}

// WHAT A SESSION NEEDS FROM Web Crypto, step by step, every step measured.
//
// This used to CREATE an Ed25519 and an X25519 key and answer yes if that went
// through. The thought was right - measure the capability, never read it off
// the engine's name - and the code measured the wrong step. Firefox creates
// both keys, signs with them and derives with them; what it cannot do is read
// the public half back OUT of a private key document, and that is the step
// a login stands on (contracts/proof.js publicFromSeed imports the password
// seed as PKCS#8 and reads jwk.x out of it). So the guard said yes, the login
// failed with the one sentence every failed login gets, and 157 cases went red
// on the one engine nobody had ever measured (task 30).
//
// Every step below is a call some module really makes, and is named after it.
// The first step that fails is the gap, and its name is the answer to "what is
// missing": "Ed25519 generateKey" and "Ed25519 public half out of a PKCS#8
// document" are two completely different engines, and a guard that cannot tell
// them apart sends the next reader down the same wrong road.
//
// THREE gaps and not one, because three different questions get asked:
//
//   ed25519   what a login does: a key, a signature, and the public half of
//             the password seed (auth/keys.js, contracts/proof.js).
//   x25519    what a running session does: a key, its public bytes, a peer
//             key, a shared secret (contracts/noise.js).
//   x25519FromDocument  one step more, and only a test needs it: a FIXED key
//             read out of a PKCS#8 document, which is how
//             HandshakeKK.forTest reproduces noise.json. A real session never
//             does it, so it is kept out of the two gaps above - an engine
//             that can run a session but not this would otherwise have its
//             desktop cases switched off for a reason that is not theirs.
//
// An empty string means the engine can do it. Nothing here is decided by the
// engine's name: the day Playwright's WebKit grows the curves, or Firefox the
// export, these answer yes by themselves and the cases run with no edit here.
export async function sessionKeyGaps(shell) {
  return shell.evaluate(async () => {
    // PKCS#8 around a bare 32-byte CFRG seed: version 0, the OID of the curve,
    // then the seed in an OCTET STRING. The same prefixes contracts/noise.js
    // and contracts/proof.js build their own documents from.
    const PKCS8_ED25519 = new Uint8Array([
      0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x04, 0x22, 0x04, 0x20]);
    const PKCS8_X25519 = new Uint8Array([
      0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x6e, 0x04, 0x22, 0x04, 0x20]);
    const SPKI_X25519 = new Uint8Array([
      0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x6e, 0x03, 0x21, 0x00]);
    const wrap = (prefix, raw) => {
      const out = new Uint8Array(prefix.length + raw.length);
      out.set(prefix, 0);
      out.set(raw, prefix.length);
      return out;
    };
    // Any 32 bytes are a seed for both curves. This one is no key of anything:
    // it is made here, used here and dropped here.
    const seed = crypto.getRandomValues(new Uint8Array(32));
    const message = new TextEncoder().encode("ngc: can this engine sign");

    // The first step that throws, or answers anything but true, is the gap.
    const walk = async (steps) => {
      for (const [name, step] of steps) {
        try {
          if ((await step()) !== true) return name + ": answered no";
        } catch (err) {
          return name + ": " + ((err && err.name) || "threw");
        }
      }
      return "";
    };

    // auth/keys.js newSessionKeys, then proveWithPassword.
    let signing = null;
    const ed25519 = await walk([
      ["Ed25519 generateKey", async () => {
        signing = await crypto.subtle.generateKey({ name: "Ed25519" }, false, ["sign", "verify"]);
        return !!(signing && signing.privateKey && signing.publicKey);
      }],
      ["Ed25519 public key as raw bytes", async () =>
        (new Uint8Array(await crypto.subtle.exportKey("raw", signing.publicKey))).length === 32],
      ["Ed25519 sign and verify", async () => {
        const sig = await crypto.subtle.sign({ name: "Ed25519" }, signing.privateKey, message);
        return (await crypto.subtle.verify({ name: "Ed25519" }, signing.publicKey, sig, message)) === true;
      }],
      ["Ed25519 a public key back in as raw", async () => {
        const raw = new Uint8Array(await crypto.subtle.exportKey("raw", signing.publicKey));
        return !!(await crypto.subtle.importKey("raw", raw, { name: "Ed25519" }, false, ["verify"]));
      }],
      // contracts/proof.js publicFromSeed. THE step the old guard never took:
      // Firefox takes the document in and then refuses the export with
      // OperationError, because a PKCS#8 seed carries no public key and
      // Firefox, unlike Chrome and Safari, does not work it out.
      ["Ed25519 public half out of a PKCS#8 document", async () => {
        const key = await crypto.subtle.importKey(
          "pkcs8", wrap(PKCS8_ED25519, seed), { name: "Ed25519" }, true, ["sign"]);
        const jwk = await crypto.subtle.exportKey("jwk", key);
        return typeof jwk.x === "string" && jwk.x.length > 0;
      }],
    ]);

    // contracts/noise.js: newKeypair, importPublic, dh.
    let own = null, peer = null;
    const x25519 = await walk([
      ["X25519 generateKey", async () => {
        own = await crypto.subtle.generateKey({ name: "X25519" }, false, ["deriveBits"]);
        peer = await crypto.subtle.generateKey({ name: "X25519" }, false, ["deriveBits"]);
        return !!(own && own.privateKey && peer && peer.publicKey);
      }],
      ["X25519 public key as raw bytes", async () =>
        (new Uint8Array(await crypto.subtle.exportKey("raw", own.publicKey))).length === 32],
      // importPublic takes "raw" and falls back to "spki", so either door is
      // enough here - and it falls back on exactly the two errors noise.js
      // falls back on. Catching more than that would be a guard that says yes
      // where importPublic lets the error through and the handshake stops.
      ["X25519 a peer public key back in", async () => {
        const raw = new Uint8Array(await crypto.subtle.exportKey("raw", peer.publicKey));
        try {
          return !!(await crypto.subtle.importKey("raw", raw, { name: "X25519" }, false, []));
        } catch (err) {
          if (err.name !== "NotSupportedError" && err.name !== "DataError") throw err;
          return !!(await crypto.subtle.importKey(
            "spki", wrap(SPKI_X25519, raw), { name: "X25519" }, false, []));
        }
      }],
      ["X25519 deriveBits", async () =>
        (new Uint8Array(await crypto.subtle.deriveBits(
          { name: "X25519", public: peer.publicKey }, own.privateKey, 256))).length === 32],
    ]);

    // contracts/noise.js publicOf, which only HandshakeKK.forTest reaches.
    const x25519FromDocument = await walk([
      ["X25519 public half out of a PKCS#8 document", async () => {
        const key = await crypto.subtle.importKey(
          "pkcs8", wrap(PKCS8_X25519, seed), { name: "X25519" }, true, ["deriveBits"]);
        const jwk = await crypto.subtle.exportKey("jwk", key);
        return typeof jwk.x === "string" && jwk.x.length > 0;
      }],
    ]);

    return { ed25519, x25519, x25519FromDocument };
  });
}

// Can this engine open a session? A "no" means this engine reaches no desktop,
// so a test of the desktop has nothing to look at on it. shell/main.js
// browserFits() asks a shorter question and lets a browser past that cannot
// finish the login - see the task 30 report; that is production code and is
// not changed from here.
export async function hasSessionKeys(shell) {
  const gaps = await sessionKeyGaps(shell);
  return gaps.ed25519 === "" && gaps.x25519 === "";
}

// Can this engine run a Noise_KK handshake from the fixed keys of noise.json?
// Everything the X25519 side of a session needs, plus the one step more a
// fixed key costs. Ed25519 is deliberately NOT asked about: no message of a
// handshake carries a signature, and a missing one must not switch these cases
// off. That is also why this is not folded into hasSessionKeys.
export async function hasFixedKeyHandshake(shell) {
  const gaps = await sessionKeyGaps(shell);
  return gaps.x25519 === "" && gaps.x25519FromDocument === "";
}
