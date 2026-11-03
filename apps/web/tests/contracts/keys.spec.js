// The two failures of auth/keys.js that have a sentence of their own, nailed
// down at the place they are thrown.
//
// WHY THIS FILE EXISTS. The mask draws one sentence for 22 different causes on
// purpose, so a case must not read the screen to learn which cause it was
// (docs/login-fehlerklassen.md, way 3, part 1). It asks the module instead:
// the thrown text with equality, inside page.evaluate, the way
// contracts/session.spec.js has done since task 7.
//
// NO ENGINE GUARD, and that is measured rather than saved. Neither case makes
// a key: buildClientCert reads keys.sign.publicRaw and keys.dh.publicRaw and
// never asks Web Crypto about them, and proof.challenge is a SHA-256. So both
// cases run on all three engines, which is the point - a case switched off for
// a thing it never touches is a case nobody measured.
//
// WHAT HAS NO CASE HERE, and it is written down because the sheet promised
// one. Class 10 (session keys did not come) and class 14 (the person aborts,
// or the 60-second deadline passes) have NO sentence of their own:
//
//   auth/keys.js:31,38,40  newSessionKeys lets Web Crypto's own error fly -
//                          OperationError, NotSupportedError
//   auth/keys.js:95        timeout: 60000 is handed to the authenticator, and
//                          WebAuthn answers with NotAllowedError
//
// There is nothing to pin with equality: the text belongs to the engine, and
// it differs between engines. A case could only pin that the foreign error is
// NOT swallowed into a sentence of ours - which is a property of code that
// does not exist yet (nothing catches there). So these two classes stay
// uncovered, and they stay uncovered honestly: the sheet's line that lists six
// classes to cover is corrected in the same commit.
import { test, expect } from "../fixtures/page.js";

test("a client certificate that does not hold the contract never leaves the browser",
  async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const keys = await import("/assets/js/auth/keys.js");
    const { b64encode } = await import("/assets/js/contracts/b64.js");
    const session = await import("/assets/js/contracts/session.js");
    // An ID the contract accepts: 22 characters of base64url over 16 bytes
    // (contracts/text.js validID). Built rather than written out, so a change
    // to the ID size moves this with it instead of past it.
    const id = b64encode(new Uint8Array(16).fill(0x11));
    // The public halves are bytes here and nothing else. buildClientCert
    // copies them into the certificate and asks session.validateClientCert
    // about their LENGTH; no engine is asked whether they are keys.
    const good = {
      installation: id, user: id,
      nonce: new Uint8Array(session.NONCE_SIZE).fill(0x22),
      keys: { sign: { publicRaw: new Uint8Array(32).fill(0x33) },
        dh: { publicRaw: new Uint8Array(32).fill(0x44) } },
      now: 1000, maxLifetime: 600,
    };
    const run = (over) => {
      try {
        keys.buildClientCert({ ...good, ...over });
        return "accepted";
      } catch (e) {
        return e.message;
      }
    };
    return {
      // The control that has to LIVE: the same call without a change is
      // accepted. Without it a case that refused everything - a typo in the
      // good values, say - would pass as a thorough one.
      good: run({}),
      bad: {
        "no installation": run({ installation: "" }),
        "installation of the wrong length": run({ installation: id.slice(1) }),
        "no user": run({ user: "" }),
        "nonce one byte short": run({ nonce: new Uint8Array(session.NONCE_SIZE - 1) }),
        "nonce that is not bytes": run({ nonce: [1, 2, 3] }),
        "signing key of 31 bytes": run({
          keys: { ...good.keys, sign: { publicRaw: new Uint8Array(31) } } }),
        "DH key of 33 bytes": run({
          keys: { ...good.keys, dh: { publicRaw: new Uint8Array(33) } } }),
        "issued at zero": run({ now: 0 }),
        "no keys at all": run({ keys: null }),
      },
      // What the contract CANNOT be talked into from here, and it is asked
      // rather than assumed: buildClientCert clamps the lifetime itself with
      // Math.min(maxLifetime, MAX_LIFETIME), so a caller who asks for a year
      // gets a day and a valid certificate - not a refusal.
      aYear: run({ maxLifetime: 365 * 86400 }),
    };
  });
  expect(got.good, "the unchanged call is accepted").toBe("accepted");
  const one = "keys: malformed certificate";
  expect(got.bad).toEqual({
    "no installation": one,
    "installation of the wrong length": one,
    "no user": one,
    "nonce one byte short": one,
    "nonce that is not bytes": one,
    "signing key of 31 bytes": one,
    "DH key of 33 bytes": one,
    "issued at zero": one,
    "no keys at all": one,
  });
  expect(got.aYear, "a lifetime over the maximum is clamped, not refused").toBe("accepted");
});

test("an authenticator that answers with a credential nobody offered is refused",
  async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const keys = await import("/assets/js/auth/keys.js");
    const sign = await import("/assets/js/contracts/sign.js");
    const offered = new Uint8Array([1, 2, 3, 4]);
    // THE STUB, and what it is worth. The branch under measurement reads
    // cred.rawId and nothing else about the answer (auth/keys.js:100-104), so
    // an answer that carries a rawId is a faithful stand-in FOR THAT BRANCH.
    // It is not a stand-in for an authenticator: a real one filters by
    // allowCredentials and would not answer off-list at all, which is why the
    // branch cannot be reached through Playwright's virtual authenticator and
    // why the case stubs instead of asking for one.
    const answer = (rawId) => ({
      rawId: rawId.buffer,
      response: { signature: new Uint8Array(64), authenticatorData: new Uint8Array(37),
        clientDataJSON: new Uint8Array(8) },
    });
    const had = navigator.credentials;
    const put = (get) => Object.defineProperty(navigator, "credentials",
      { value: { get }, configurable: true });
    const run = async (rawId) => {
      put(async () => (rawId === null ? null : answer(rawId)));
      try {
        const p = await keys.proveWithPasskey(new Uint8Array([9, 9]),
          { rp: { id: "localhost" }, passkeys: [{ credId: offered, kid: "the-offered-one" }] },
          sign.PURPOSE_CLIENT_CERT);
        return "kid:" + p.kid;
      } catch (e) {
        return e.message;
      }
    };
    const out = {
      // Class 13: an ID that was never offered.
      stranger: await run(new Uint8Array([5, 6, 7, 8])),
      // Class 13 again, one byte off - the comparison is over bytes, not over
      // a length or a prefix.
      almost: await run(new Uint8Array([1, 2, 3, 5])),
      // Class 12, so the two are told apart here instead of both reading as
      // "the passkey went wrong": nothing came back at all.
      nothing: await run(null),
      // The control that has to LIVE: the offered ID is accepted and the
      // proof names the key of that very entry. Without it this case would
      // pass just as well if proveWithPasskey refused every answer.
      offered: await run(offered),
    };
    if (had === undefined) delete navigator.credentials;
    else Object.defineProperty(navigator, "credentials", { value: had, configurable: true });
    return out;
  });
  const other = "keys: the authenticator answered with another credential";
  expect(got.stranger).toBe(other);
  expect(got.almost).toBe(other);
  expect(got.nothing, "nothing back is its own class, not this one").toBe("keys: no passkey");
  expect(got.offered, "the offered credential still goes through").toBe("kid:the-offered-one");
});
