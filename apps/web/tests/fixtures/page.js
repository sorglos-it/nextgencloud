import { test as base } from "@playwright/test";
import { USER_NAME, PASSWORD, PKCS8 } from "./logins.js";

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

// WHAT A CASE NEEDS FROM Web Crypto, one named step at a time, every step
// measured on the engine that is about to run the case.
//
// TWO LESSONS ARE BUILT INTO THIS BLOCK, and both were learnt the hard way.
//
// The first: a guard must do the WORK, not prepare for it. The original guard
// created an Ed25519 and an X25519 key and said yes if that went through.
// Firefox does that, signs with them and derives with them - and then fails,
// because the step it cannot take is reading the public half back OUT of a
// private key document (contracts/proof.js publicFromSeed imports the password
// seed as PKCS#8 and reads jwk.x). The guard said yes, the login failed, and
// 157 cases went red on the one engine nobody had ever measured.
//
// The second, and it cost a whole review: a guard that is too STRICT is worse
// than one that is too loose, because a skipped case looks like a finished one.
// The first repair asked EVERY case for the seed step, and so switched off ten
// cases that were green on Firefox - among them "a man in the middle with other
// keys is refused", a security check, silently. A case that CAN run must run.
//
// Hence: ONE GAP PER ACTION, and a case asks for the actions it really
// performs. Not one guard per file, and not one per topic.
//
//   curves              the two generateKey calls shell/main.js browserFits()
//                       makes, and nothing else - see shellGap below for why
//                       generateKey is the whole action for that ONE question.
//   ed25519             a key, a signature, a check, a key read back in.
//                       auth/keys.js newSessionKeys, contracts/sign.js verify.
//   ed25519FromSeed     the public half of a password seed, and it has to be
//                       the RIGHT half. contracts/proof.js publicFromSeed.
//                       This is the step a password login stands on.
//   x25519              a key, its public bytes, a peer key, a shared secret.
//                       contracts/noise.js newKeypair, importPublic, dh - what
//                       a running session and a FRESH handshake do.
//   x25519FromDocument  the public half of a FIXED key out of a PKCS#8
//                       document. contracts/noise.js publicOf, which only
//                       HandshakeKK.forTest reaches - no running session does
//                       it, and no case with fresh keys needs it.
//   p256FromDocument    a P-256 key out of a PKCS#8 document, a signature by
//                       it, and the signature checked. What the software
//                       authenticator of fixtures/passkey.js does, and the
//                       only signature step a PASSKEY login needs on this
//                       side - contracts/proof.js ALG_ES256.
//
// Measured answers, so nobody has to guess what a "no" means:
//
//   chromium  all five empty.
//   firefox   curves, ed25519 and x25519 empty; the two steps that read a public half
//             out of a document answer OperationError. Firefox takes the
//             document IN and then refuses the JWK export, because a PKCS#8
//             seed carries no public key and Firefox, unlike Chrome and Safari,
//             does not work it out.
//   webkit    the first five, at generateKey, with NotSupportedError: the
//             curves themselves are missing. p256FromDocument is EMPTY there -
//             WebKit has P-256, it is the CFRG curves it lacks.
//
// Nothing here is decided by the engine's name. The day Playwright's WebKit
// grows the curves, or Firefox the export, these answer yes by themselves and
// the cases run with no edit here.
export async function sessionKeyGaps(shell) {
  // clara's private half, as the one P-256 document in the repository
  // (fixtures/logins.js). It is passed IN rather than written a second time
  // here: the step below measures the engine against the very key the software
  // authenticator of fixtures/passkey.js imports, so a document that changed
  // there changes this measurement with it.
  return shell.evaluate(async (p256Base64) => {
    // PKCS#8 around a bare 32-byte CFRG seed: version 0, the OID of the curve,
    // then the seed in an OCTET STRING. Byte for byte the prefixes
    // contracts/proof.js and contracts/noise.js build their own documents from.
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
    // jwk.x is base64url and carries no padding, which atob does not take.
    const bytesOf = (s) => {
      const padded = s.length % 4 === 0 ? s : s + "=".repeat(4 - (s.length % 4));
      const text = atob(padded.replace(/-/g, "+").replace(/_/g, "/"));
      const out = new Uint8Array(text.length);
      for (let i = 0; i < text.length; i++) out[i] = text.charCodeAt(i);
      return out;
    };
    const same = (a, b) => a.length === b.length && a.every((v, i) => v === b[i]);
    // Any 32 bytes are a seed for both curves. This one is the key of nothing:
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

    // shell/main.js browserFits(): THESE TWO CALLS AND NOTHING ELSE. It is the
    // only gate between a page load and the mask, so for the one question "does
    // the shell come up" these two calls really are the whole action - the only
    // place in this file where generateKey alone is an honest answer. Every
    // other question below builds on it and asks for more.
    let signing = null, own = null, peer = null;
    const curves = await walk([
      ["Ed25519 generateKey", async () => {
        signing = await crypto.subtle.generateKey({ name: "Ed25519" }, false, ["sign", "verify"]);
        return !!(signing && signing.privateKey && signing.publicKey);
      }],
      ["X25519 generateKey", async () => {
        own = await crypto.subtle.generateKey({ name: "X25519" }, false, ["deriveBits"]);
        peer = await crypto.subtle.generateKey({ name: "X25519" }, false, ["deriveBits"]);
        return !!(own && own.privateKey && peer && peer.publicKey);
      }],
    ]);

    // auth/keys.js newSessionKeys, then the signature it makes.
    const ed25519 = curves !== "" ? curves : await walk([
      ["Ed25519 public key as raw bytes", async () =>
        (new Uint8Array(await crypto.subtle.exportKey("raw", signing.publicKey))).length === 32],
      ["Ed25519 sign and verify", async () => {
        const sig = await crypto.subtle.sign({ name: "Ed25519" }, signing.privateKey, message);
        return (await crypto.subtle.verify({ name: "Ed25519" }, signing.publicKey, sig, message)) === true;
      }],
      // The key that comes back in has to CHECK the signature, not merely exist.
      // An exportKey("raw") that hands back 32 valid bytes of another key gets a
      // "does it import" question through - measured - and contracts/sign.js
      // then verifies every envelope against the wrong key.
      ["Ed25519 a public key back in as raw", async () => {
        const raw = new Uint8Array(await crypto.subtle.exportKey("raw", signing.publicKey));
        const back = await crypto.subtle.importKey("raw", raw, { name: "Ed25519" }, false, ["verify"]);
        const sig = await crypto.subtle.sign({ name: "Ed25519" }, signing.privateKey, message);
        return (await crypto.subtle.verify({ name: "Ed25519" }, back, sig, message)) === true;
      }],
    ]);

    // contracts/proof.js publicFromSeed - the step the first guard never took.
    //
    // It does not stop at "the export gave me a string". An engine whose jwk.x
    // is there but WRONG passes that, and publicFromSeed then hands out a
    // public key that belongs to no private key - measured in the review: a
    // bent export got the length-only check through while publicFromSeed
    // returned 3 bytes instead of 32. So the half that comes out is USED: it
    // has to check the signature that the private half of the same document
    // just made. A wrong public key cannot.
    const ed25519FromSeed = await walk([
      ["Ed25519 public half out of a PKCS#8 seed", async () => {
        const key = await crypto.subtle.importKey(
          "pkcs8", wrap(PKCS8_ED25519, seed), { name: "Ed25519" }, true, ["sign"]);
        const jwk = await crypto.subtle.exportKey("jwk", key);
        if (typeof jwk.x !== "string" || jwk.x.length === 0) return false;
        const raw = bytesOf(jwk.x);
        if (raw.length !== 32) return false;
        const pub = await crypto.subtle.importKey("raw", raw, { name: "Ed25519" }, false, ["verify"]);
        const sig = await crypto.subtle.sign({ name: "Ed25519" }, key, message);
        return (await crypto.subtle.verify({ name: "Ed25519" }, pub, sig, message)) === true;
      }],
    ]);

    // contracts/noise.js importPublic, byte for byte its two doors in its order.
    const importPublicX25519 = async (raw) => {
      try {
        return await crypto.subtle.importKey("raw", raw, { name: "X25519" }, false, []);
      } catch (err) {
        if (err.name !== "NotSupportedError" && err.name !== "DataError") throw err;
        return crypto.subtle.importKey("spki", wrap(SPKI_X25519, raw), { name: "X25519" }, false, []);
      }
    };

    // contracts/noise.js newKeypair, importPublic, dh.
    let peerBack = null;
    const x25519 = curves !== "" ? curves : await walk([
      ["X25519 public key as raw bytes", async () =>
        (new Uint8Array(await crypto.subtle.exportKey("raw", own.publicKey))).length === 32],
      ["X25519 a peer public key back in", async () => {
        const raw = new Uint8Array(await crypto.subtle.exportKey("raw", peer.publicKey));
        peerBack = await importPublicX25519(raw);
        return !!peerBack;
      }],
      // Derived against the key that came BACK from the exported bytes, never
      // against the CryptoKey those bytes came from. That is what a session
      // does: newKeypair() hands the raw bytes out, auth/keys.js puts them in
      // the certificate, and the peer derives from those bytes and no others.
      // Measured: an exportKey("raw") that lies kept every gap empty while the
      // fresh handshake in the production module died with OperationError, for
      // exactly as long as this step derived from own CryptoKeys.
      ["X25519 deriveBits", async () => {
        const mine = new Uint8Array(await crypto.subtle.deriveBits(
          { name: "X25519", public: peerBack }, own.privateKey, 256));
        const ownRaw = new Uint8Array(await crypto.subtle.exportKey("raw", own.publicKey));
        const ownBack = await importPublicX25519(ownRaw);
        const theirs = new Uint8Array(await crypto.subtle.deriveBits(
          { name: "X25519", public: ownBack }, peer.privateKey, 256));
        return mine.length === 32 && same(mine, theirs);
      }],
    ]);

    // contracts/noise.js publicOf, reached from HandshakeKK.forTest alone.
    //
    // Same discipline as the seed step: the public half that comes out is USED,
    // by deriving the shared secret both ways round. A handshake lives on both
    // sides reaching the same bytes, so that is what gets measured - not the
    // mere presence of a string. It needs the four steps above to have worked,
    // because it derives with the pair they made.
    const x25519FromDocument = x25519 !== "" ? x25519 : await walk([
      ["X25519 public half out of a PKCS#8 document", async () => {
        const priv = await crypto.subtle.importKey(
          "pkcs8", wrap(PKCS8_X25519, seed), { name: "X25519" }, true, ["deriveBits"]);
        const jwk = await crypto.subtle.exportKey("jwk", priv);
        if (typeof jwk.x !== "string" || jwk.x.length === 0) return false;
        const raw = bytesOf(jwk.x);
        if (raw.length !== 32) return false;
        const pub = await crypto.subtle.importKey("raw", raw, { name: "X25519" }, false, []);
        const mine = new Uint8Array(await crypto.subtle.deriveBits(
          { name: "X25519", public: peer.publicKey }, priv, 256));
        const theirs = new Uint8Array(await crypto.subtle.deriveBits(
          { name: "X25519", public: pub }, peer.privateKey, 256));
        return same(mine, theirs);
      }],
    ]);

    // fixtures/passkey.js, the software authenticator: the P-256 private key
    // out of the PKCS#8 document fixtures/logins.js holds, a signature by it,
    // and that signature CHECKED against the public half of the same document.
    // Same discipline as the two seed steps above - the half that comes out is
    // USED, so an engine that imports the document wrongly is a "no" here
    // instead of a login that fails later for a reason nobody named.
    //
    // It needs none of the five above: a passkey login touches no CFRG curve
    // on this side of the wire. The cases ask for this one TOGETHER with
    // ed25519 and x25519 (passkeyGap below), because the session the login
    // opens needs those.
    //
    // MEASURED on 18.10.2026, playwright 1.55.1: EMPTY in chromium
    // 140.0.7339.186, firefox 141.0 AND webkit 26.0 - P-256 is the one
    // signature algorithm all three have, which is why the passkey route can
    // be driven on every engine while the password route cannot. On webkit it
    // is the ONLY empty gap of the six.
    //
    // How that was measured, three lines anybody can paste again - the whole
    // answer for all three engines at once:
    //
    //   cat > shell/gaps.spec.js <<'EOF'
    //   import { test, sessionKeyGaps } from "../fixtures/page.js";
    //   test("gaps", async ({ shell }) => {
    //     console.log("GAPS " + JSON.stringify(await sessionKeyGaps(shell))); });
    //   EOF
    //   npx playwright test shell/gaps.spec.js && rm shell/gaps.spec.js
    //
    // What it printed, one line per engine, in the order of the projects:
    //
    //   {"curves":"","ed25519":"","ed25519FromSeed":"","x25519":"",
    //    "x25519FromDocument":"","p256FromDocument":""}
    //   {... "ed25519FromSeed":"Ed25519 public half out of a PKCS#8 seed:
    //    OperationError", "x25519FromDocument":"X25519 public half out of a
    //    PKCS#8 document: OperationError", "p256FromDocument":""}
    //   {"curves":"Ed25519 generateKey: NotSupportedError", ... everything but
    //    "p256FromDocument":""}
    const p256FromDocument = await walk([
      ["P-256 sign and check out of a PKCS#8 document", async () => {
        const text = atob(p256Base64);
        const doc = new Uint8Array(text.length);
        for (let i = 0; i < text.length; i++) doc[i] = text.charCodeAt(i);
        const priv = await crypto.subtle.importKey(
          "pkcs8", doc, { name: "ECDSA", namedCurve: "P-256" }, true, ["sign"]);
        const jwk = await crypto.subtle.exportKey("jwk", priv);
        if (typeof jwk.x !== "string" || typeof jwk.y !== "string") return false;
        const pub = await crypto.subtle.importKey(
          "jwk", { kty: "EC", crv: "P-256", x: jwk.x, y: jwk.y },
          { name: "ECDSA", namedCurve: "P-256" }, false, ["verify"]);
        const sig = await crypto.subtle.sign({ name: "ECDSA", hash: "SHA-256" }, priv, message);
        return (await crypto.subtle.verify(
          { name: "ECDSA", hash: "SHA-256" }, pub, sig, message)) === true;
      }],
    ]);

    return { curves, ed25519, ed25519FromSeed, x25519, x25519FromDocument, p256FromDocument };
  }, PKCS8.clara);
}

// ---- one gap per action ---------------------------------------------------
//
// Each answers "" when the engine can do what the action needs, and otherwise
// the name of the first step that failed. That string is what the skip says, so
// a skipped case always names the thing that is really missing instead of the
// topic of its file.

// What the shell needs to come up at all and show a mask. Exactly the question
// shell/main.js browserFits() asks, and no more: asking for a signature here
// would switch off cases on an engine whose mask appears. That browserFits asks
// so little is the production bug this task found - but for THIS question it is
// the right measure, because it is the gate the shell itself stands behind.
export async function shellGap(shell) {
  return (await sessionKeyGaps(shell)).curves;
}

// What a PASSWORD LOGIN needs on top of that: the public half of the seed.
// A "no" here means this engine reaches no desktop, so a case that wants one
// has nothing to look at. A case that only looks at the mask, the setup or the
// configuration parser asks shellGap instead - asking this one there is what
// switched ten green Firefox cases off.
export async function loginGap(shell) {
  const g = await sessionKeyGaps(shell);
  return g.ed25519 || g.x25519 || g.ed25519FromSeed;
}

// THE PASSWORD SEED STEP ALONE: the public half of a PKCS#8 seed, and nothing
// else. contracts/proof.js publicFromSeed, and with it everything that stands
// on a password key - auth/keys.js proveWithPassword, auth/setup.js, and the
// cases of contracts/proof.spec.js and contracts/session.spec.js that build a
// certificate out of a seed.
//
// WHY IT IS ITS OWN GAP AND NOT loginGap: those cases need no X25519 and no
// running session, only this one step, and a case must ask for what it really
// performs. Asking loginGap there would switch a case off for a curve it never
// touches; asking "is there an Ed25519 key at all" - which is what those two
// files did until 17.10.2026 - let eight of them run into
// OperationError on Firefox 141, which has Ed25519 and refuses exactly this
// export.
export async function seedKeyGap(shell) {
  const g = await sessionKeyGaps(shell);
  return g.ed25519 || g.ed25519FromSeed;
}

export async function skipWithoutSeedKeys(shell) {
  const gap = await seedKeyGap(shell);
  test.skip(gap !== "", `this engine makes no password key out of a seed - ${gap}`);
}

// WHAT A PASSKEY LOGIN NEEDS, and it is a different set from a password login:
// the session keys (ed25519, x25519) and the P-256 step the authenticator
// signs with - never ed25519FromSeed, which is the one step a PASSWORD key
// stands on and the one Firefox 141 refuses. That is why this gap exists at
// all: on Firefox the passkey is the ONLY route the mask offers, so the gap
// that switches a passkey case off must not be the password's.
//
// Until 18.10.2026 the passkey cases were switched off by
// `test.skip(browserName !== "chromium")` - by the NAME of the engine, because
// Playwright's virtual authenticator is the CDP WebAuthn domain. The software
// authenticator of fixtures/passkey.js removed the need for that name.
export async function passkeyGap(shell) {
  const g = await sessionKeyGaps(shell);
  return g.ed25519 || g.x25519 || g.p256FromDocument;
}

export async function skipWithoutPasskey(shell) {
  const gap = await passkeyGap(shell);
  test.skip(gap !== "", `this engine cannot finish a passkey login - ${gap}`);
}

// An Ed25519 signature, made or checked - and nothing about a password seed.
export async function signatureGap(shell) {
  return (await sessionKeyGaps(shell)).ed25519;
}

// A handshake with FRESH keys - newKeypair() and HandshakeKK.of(), the path NGC
// takes at runtime. No signature, no key document: X25519 alone.
export async function handshakeGap(shell) {
  return (await sessionKeyGaps(shell)).x25519;
}

// A handshake from the FIXED keys of noise.json, through HandshakeKK.forTest.
// Everything a fresh handshake needs, plus reading a public half out of a key
// document. Ed25519 is deliberately not asked about: no message of a handshake
// carries a signature.
export async function fixedKeyGap(shell) {
  const g = await sessionKeyGaps(shell);
  return g.x25519 || g.x25519FromDocument;
}

// ---- the skips -----------------------------------------------------------
//
// One line at the call site, and the reason is always the measured gap. Hand
// written reasons are how "no Ed25519 and no X25519 in this engine's Web
// Crypto" came to stand over a Firefox that has both.

export async function skipWithoutShell(shell) {
  const gap = await shellGap(shell);
  test.skip(gap !== "", `this engine cannot run the shell at all - ${gap}`);
}

export async function skipWithoutLogin(shell) {
  const gap = await loginGap(shell);
  test.skip(gap !== "", `this engine cannot finish a password login - ${gap}`);
}

// The setup makes the first admin out of a typed password, so it stands on the
// same step a login does - one measurement, two sentences, because a case that
// was setting an installation up should not be told it could not log in.
export async function skipWithoutSetup(shell) {
  const gap = await loginGap(shell);
  test.skip(gap !== "", `this engine cannot finish the setup - ${gap}`);
}

// THE BROWSER'S OWN WebAuthn STACK, reached over the CDP WebAuthn domain. It
// is asked for by the two cases that measure Playwright's virtual
// authenticator, which is a deeper replacement than the software one: it sits
// below navigator.credentials.get, inside the browser.
//
// It is MEASURED and not read off the engine's name, for the same reason as
// everything else in this file - the sentence a skip prints names the thing
// that is really missing. Today only Chromium answers; the day Playwright
// grows a WebAuthn domain for another engine, these cases run there with no
// edit here.
export async function cdpGap(shell) {
  try {
    const cdp = await shell.context().newCDPSession(shell);
    await cdp.send("WebAuthn.enable");
    await cdp.detach();
    return "";
  } catch (err) {
    // The first line only: Playwright's own errors carry a whole page of
    // advice behind the sentence, and this string is what the skip prints.
    return "CDP WebAuthn: " + ((err && err.message) || "threw").split(/\r?\n/)[0];
  }
}

export async function skipWithoutCDPAuthenticator(shell) {
  const gap = await cdpGap(shell);
  test.skip(gap !== "", `this engine has no virtual authenticator of its own - ${gap}`);
}

export async function skipWithoutHandshake(shell) {
  const gap = await handshakeGap(shell);
  test.skip(gap !== "", `this engine cannot run a handshake with fresh keys - ${gap}`);
}

export async function skipWithoutFixedKeys(shell) {
  const gap = await fixedKeyGap(shell);
  test.skip(gap !== "", `this engine cannot run a handshake from fixed keys - ${gap}`);
}

// An Ed25519 signature, made or checked. shell/world.spec.js needs it for the
// stand-in world it builds and for the trust chains it verifies; no password
// seed is read anywhere in those, so they must not be asked for one.
export async function skipWithoutSignatures(shell) {
  const gap = await signatureGap(shell);
  test.skip(gap !== "", `this engine cannot make or check an Ed25519 signature - ${gap}`);
}

// An X25519 key pair to hand to a peer, and a secret derived with it.
export async function skipWithoutSessionKeys(shell) {
  const gap = await handshakeGap(shell);
  test.skip(gap !== "", `this engine cannot make an X25519 session key pair - ${gap}`);
}

// A boolean, for the one case that does not skip but BRANCHES on it:
// shell/boot.spec.js, which runs on every engine and pins that the boot ends in
// one of its two legal endings.
//
// IT IS THE ONLY CALLER, and that is now true - it was not when this comment
// first said so. shell/world.spec.js used the same function to SKIP with, and
// when this task changed what it means (from "the two curves are there" to "a
// password login goes through"), three cases there that are green on Firefox
// went quietly out, one of them a security check. The comment said "the one
// case"; the grep said two. Whoever adds a second caller gives it a gap of its
// own instead - the gaps above are the vocabulary for that.
export async function hasSessionKeys(shell) {
  return (await loginGap(shell)) === "";
}
