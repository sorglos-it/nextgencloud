import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// sign.json holds one signed envelope: the key, the purpose, the bytes the
// signature covers and the envelope itself. It is the only vector there is
// for this contract, and it says nothing about what has to be refused, so the
// cases below spell that out from the Go source (shared/sign/sign.go).
const v = vectors("sign.json");

// Does the Web Crypto API of this engine know Ed25519? Chrome, Safari 17 and
// Firefox 130 do; the WebKit that Playwright brings answers NotSupportedError
// to importKey, for X25519 as well. An engine without it can check no
// signature at all, and the shell then refuses every one of them. The one
// test that needs a signature to come out right says out loud why it did not
// run there, instead of quietly passing.
async function hasEd25519(shell, pub) {
  return shell.evaluate(async (pubB64) => {
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    try {
      await crypto.subtle.importKey("raw", b64decode(pubB64), { name: "Ed25519" }, false, ["verify"]);
      return true;
    } catch {
      return false;
    }
  }, pub);
}

test("an envelope is checked over the exact bytes it carries", async ({ shell }) => {
  test.skip(!(await hasEd25519(shell, v.public)),
    "no Ed25519 in this engine's Web Crypto: it cannot tell a right signature from a wrong one");
  const out = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/sign.js");
    const { b64decode, b64encode, equal } = await import("/assets/js/contracts/b64.js");
    const pub = b64decode(d.public);
    const payload = b64decode(d.envelope.payload);
    const env = { payload, kid: d.envelope.kid, sig: b64decode(d.envelope.sig) };
    const flipped = { ...env, sig: Uint8Array.from(env.sig) };
    flipped.sig[0] ^= 1;
    const other = { ...env, payload: Uint8Array.from(payload) };
    other.payload[0] ^= 1;
    return {
      message: b64encode(s.message(d.purpose, payload)) === d.message,
      keyID: (await s.keyID(pub)) === d.envelope.kid,
      good: await s.verify(env, pub, d.purpose),
      flipped: await s.verify(flipped, pub, d.purpose),
      otherPayload: await s.verify(other, pub, d.purpose),
      otherPurpose: await s.verify(env, pub, "task/v1"),
      // A key that is not the one the envelope names is refused before any
      // signature is checked.
      otherKey: await s.verify({ ...env, kid: "AAAAAAAAAAAAAAAAAAAAAA" }, pub, d.purpose),
      shape: s.validateEnvelope(env, 4194304) && !s.validateEnvelope({ ...env, sig: new Uint8Array(63) }, 4194304)
        && !s.validateEnvelope({ ...env, payload: new Uint8Array(0) }, 4194304)
        && !s.validateEnvelope(env, payload.length - 1),
      roundTrip: equal(s.parseEnvelope(new TextEncoder().encode(JSON.stringify({
        payload: d.envelope.payload, kid: d.envelope.kid, sig: d.envelope.sig,
      })), 4194304).payload, payload),
    };
  }, v);
  expect(out).toEqual({
    message: true, keyID: true, good: true, flipped: false, otherPayload: false,
    otherPurpose: false, otherKey: false, shape: true, roundTrip: true,
  });
});

// What the test above proves only where the engine can check a signature, and
// what it does not reach at all: verify has to answer false, and it may never
// throw. A caller that had to catch an exception around a signature is a
// caller that lets one through as soon as the engine surprises it.
test("verify answers false and never throws for anything but the right signature", async ({ shell }) => {
  const out = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/sign.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const pub = b64decode(d.public);
    const payload = b64decode(d.envelope.payload);
    const env = { payload, kid: d.envelope.kid, sig: b64decode(d.envelope.sig) };
    const flipped = { ...env, sig: Uint8Array.from(env.sig) };
    flipped.sig[0] ^= 1;
    const cases = [
      ["a flipped signature", env.payload, env.kid, flipped.sig, pub, d.purpose],
      ["a payload of no bytes", new Uint8Array(0), env.kid, env.sig, pub, d.purpose],
      ["a payload one byte longer", new Uint8Array(payload.length + 1), env.kid, env.sig, pub, d.purpose],
      ["another purpose", env.payload, env.kid, env.sig, pub, "task/v1"],
      ["a purpose nobody signs", env.payload, env.kid, env.sig, pub, "clientcert/v1"],
      ["another key id", env.payload, "AAAAAAAAAAAAAAAAAAAAAA", env.sig, pub, d.purpose],
      ["a key id that is no text", env.payload, 22, env.sig, pub, d.purpose],
      ["a key that is not this one", env.payload, env.kid, env.sig, new Uint8Array(32), d.purpose],
      ["a key of 31 bytes", env.payload, env.kid, env.sig, pub.subarray(0, 31), d.purpose],
      ["a key that is no bytes", env.payload, env.kid, env.sig, d.public, d.purpose],
      // A field a caller left out: Go has the types of its struct, here the
      // value is undefined and the answer may still only be false.
      ["a signature that is missing", env.payload, env.kid, undefined, pub, d.purpose],
      ["a signature of 63 bytes", env.payload, env.kid, env.sig.subarray(0, 63), pub, d.purpose],
      ["a payload that is missing", undefined, env.kid, env.sig, pub, d.purpose],
      ["a payload that is text", d.envelope.payload, env.kid, env.sig, pub, d.purpose],
    ];
    const wrong = [];
    for (const [name, payloadIn, kid, sig, key, purpose] of cases) {
      try {
        const got = await s.verify({ payload: payloadIn, kid, sig }, key, purpose);
        if (got !== false) wrong.push(`${name}: answered ${JSON.stringify(got)}`);
      } catch (e) {
        wrong.push(`${name}: threw ${e.constructor.name}: ${e.message}`);
      }
    }
    for (const [name, env2] of [["no envelope", null], ["an envelope that is text", "x"], ["an envelope that is a number", 7]]) {
      try {
        const got = await s.verify(env2, pub, d.purpose);
        if (got !== false) wrong.push(`${name}: answered ${JSON.stringify(got)}`);
      } catch (e) {
        wrong.push(`${name}: threw ${e.constructor.name}: ${e.message}`);
      }
    }
    // verifyRaw is the same answer one layer down, where the message is not
    // built from a purpose but handed over.
    const msg = s.message(d.purpose, payload);
    const raw = {
      good: await s.verifyRaw(pub, env.sig, msg),
      flipped: await s.verifyRaw(pub, flipped.sig, msg),
      shortKey: await s.verifyRaw(pub.subarray(0, 31), env.sig, msg),
      shortSig: await s.verifyRaw(pub, env.sig.subarray(0, 63), msg),
      noKey: await s.verifyRaw(d.public, env.sig, msg),
      noSig: await s.verifyRaw(pub, undefined, msg),
      noMessage: await s.verifyRaw(pub, env.sig, undefined),
    };
    // A page that is no secure context has no crypto.subtle at all, and the
    // hash throws where it would otherwise answer. The check has to come back
    // false there as well, and never with an exception past the caller. The
    // digest is taken away for one call to show it.
    let noDigest, afterwards;
    try {
      Object.defineProperty(crypto.subtle, "digest", {
        value: () => { throw new TypeError("no digest here"); }, configurable: true, writable: true,
      });
      noDigest = await s.verify(env, pub, d.purpose);
    } catch (e) {
      noDigest = `threw ${e.constructor.name}: ${e.message}`;
    } finally {
      delete crypto.subtle.digest;
    }
    afterwards = (await s.keyID(pub)) === d.envelope.kid;
    return {
      wrong, raw, noDigest, afterwards,
      sameKeyID: (await s.keyID(pub)) === (await s.keyIDAsync(pub)),
      oneEqual: s.equal === (await import("/assets/js/contracts/b64.js")).equal,
    };
  }, v);
  expect(out.wrong).toEqual([]);
  expect(out.noDigest).toBe(false);
  // The engine is whole again, so the case above proves the answer and not a
  // broken page.
  expect(out.afterwards).toBe(true);
  // equal is handed on from b64 and is that one function, not a copy.
  expect(out.oneEqual).toBe(true);
  // keyID and keyIDAsync are one function under two names, so no caller ever
  // holds two key ids for one key.
  expect(out.sameKeyID).toBe(true);
  // Where the engine can check a signature the right one comes out true; where
  // it cannot, every answer is false. Nothing but the right signature is ever
  // accepted, in either engine.
  const ed = await hasEd25519(shell, v.public);
  expect(out.raw).toEqual({
    good: ed, flipped: false, shortKey: false, shortSig: false,
    noKey: false, noSig: false, noMessage: false,
  });
});

// The message a signature covers, for every purpose of the contract: the
// prefix, the zero byte that ends it and then the payload, byte for byte.
test("the message of every purpose carries its purpose and nothing else", async ({ shell }) => {
  const out = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/sign.js");
    const { b64encode, utf8 } = await import("/assets/js/contracts/b64.js");
    const purposes = [s.PURPOSE_CLIENT_CERT, s.PURPOSE_API_CERT, s.PURPOSE_TRUST_LIST, s.PURPOSE_TASK,
      s.PURPOSE_CONFIRM, s.PURPOSE_CATALOG, s.PURPOSE_LICENSE, s.PURPOSE_RELEASE];
    const wrong = [];
    const payload = utf8("payload");
    for (const p of purposes) {
      const m = s.message(p, payload);
      const want = new Uint8Array([...utf8("ngc/" + p), 0, ...payload]);
      if (b64encode(m) !== b64encode(want)) wrong.push(p);
    }
    // A purpose that is not text is refused instead of turned into one: a
    // misspelled import would otherwise sign for the purpose "ngc/undefined".
    const refused = [];
    for (const bad of [undefined, null, 7, ["task/v1"], { toString: () => "task/v1" }]) {
      try {
        s.message(bad, payload);
        refused.push(JSON.stringify(bad) ?? "undefined");
      } catch (e) {
        if (e.constructor.name !== "BadInput") refused.push(`threw ${e.constructor.name}`);
      }
    }
    // A payload that is not bytes likewise.
    for (const bad of [undefined, null, "payload", 7]) {
      try {
        s.message("task/v1", bad);
        refused.push(`payload ${JSON.stringify(bad) ?? "undefined"}`);
      } catch (e) {
        if (e.constructor.name !== "BadInput") refused.push(`payload threw ${e.constructor.name}`);
      }
    }
    return {
      wrong, refused,
      purposes,
      empty: b64encode(s.message("task/v1", new Uint8Array(0))) === b64encode(utf8("ngc/task/v1\0")),
      maxPayload: s.MAX_PAYLOAD,
    };
  }, v);
  expect(out.wrong).toEqual([]);
  expect(out.refused).toEqual([]);
  expect(out.empty).toBe(true);
  // The constants, word for word as the Go source has them.
  expect(out.purposes).toEqual(["clientcert/v1", "apicert/v1", "trustlist/v1", "task/v1",
    "confirm/v1", "catalog/v1", "license/v1", "release/v1"]);
  expect(out.maxPayload).toBe(4194304);
});

// Envelope.Validate of Go, case by case. There are no vectors for what has to
// be refused, so every rule of the Go source stands here as its own case: a
// payload of 1 to maxPayload bytes, a key id of 22 base64url characters over
// 16 bytes, a signature of 64 bytes - and a limit that is a whole number of at
// least 1, because a limit that is none would take the bound away.
test("the shape of an envelope is decided as Go decides it", async ({ shell }) => {
  const out = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/sign.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const payload = b64decode(d.envelope.payload);
    const env = { payload, kid: d.envelope.kid, sig: b64decode(d.envelope.sig) };
    const MAX = 4194304;
    const cases = [
      ["the envelope of sign.json", env, MAX, true],
      ["a payload of one byte", { ...env, payload: new Uint8Array(1) }, MAX, true],
      ["a payload of no bytes", { ...env, payload: new Uint8Array(0) }, MAX, false],
      ["a payload exactly at the limit", env, payload.length, true],
      ["a payload one byte over the limit", env, payload.length - 1, false],
      ["a payload over the largest payload", { ...env, payload: new Uint8Array(MAX + 1) }, MAX + 1, false],
      ["a payload that is no bytes", { ...env, payload: d.envelope.payload }, MAX, false],
      ["a payload that is an array of numbers", { ...env, payload: [1, 2, 3] }, MAX, false],
      ["a payload that is missing", { kid: env.kid, sig: env.sig }, MAX, false],
      ["a key id of 21 characters", { ...env, kid: env.kid.slice(0, 21) }, MAX, false],
      ["a key id of 23 characters", { ...env, kid: env.kid + "A" }, MAX, false],
      // Both are refused by b64decode, which the check leans on: a character
      // of no alphabet, and bits at the end that belong to no byte.
      ["a key id outside the alphabet", { ...env, kid: "+" + env.kid.slice(1) }, MAX, false],
      ["a key id with bits left over", { ...env, kid: env.kid.slice(0, 21) + "B" }, MAX, false],
      ["a key id that is no text", { ...env, kid: 22 }, MAX, false],
      ["a key id that is missing", { payload: env.payload, sig: env.sig }, MAX, false],
      ["a signature of 63 bytes", { ...env, sig: new Uint8Array(63) }, MAX, false],
      ["a signature of 65 bytes", { ...env, sig: new Uint8Array(65) }, MAX, false],
      ["a signature that is no bytes", { ...env, sig: d.envelope.sig }, MAX, false],
      ["a signature that is missing", { payload: env.payload, kid: env.kid }, MAX, false],
      ["no envelope at all", null, MAX, false],
      ["an envelope that is text", "envelope", MAX, false],
      ["an envelope that is a number", 7, MAX, false],
      ["a limit of 1", { ...env, payload: new Uint8Array(1) }, 1, true],
      ["a limit of 0", env, 0, false],
      ["a limit below 0", env, -1, false],
      ["a limit that is no whole number", env, 15.5, false],
      ["a limit that is missing", env, undefined, false],
      ["a limit that is not a number", env, "4194304", false],
      ["a limit of NaN", env, NaN, false],
      ["a limit of Infinity", env, Infinity, false],
      ["a limit past the whole numbers", env, 1e300, false],
      // Math.min of a big integer and a number throws; the limit is looked at
      // before anything is done with it, so the answer stays an answer.
      ["a limit that is a big integer", env, BigInt(15), false],
      ["a limit above the largest payload", env, 9007199254740991, true],
    ];
    const wrong = [];
    for (const [name, e, max, want] of cases) {
      let got;
      try {
        got = s.validateEnvelope(e, max);
      } catch (err) {
        wrong.push(`${name}: threw ${err.constructor.name}: ${err.message}`);
        continue;
      }
      if (got !== want) wrong.push(`${name}: answered ${JSON.stringify(got)}, ${want} wanted`);
    }
    return wrong;
  }, v);
  expect(out).toEqual([]);
});

// ParseEnvelope of Go: strict JSON within a budget of the base64url of
// maxPayload bytes plus 1 KiB, then Validate. Every failure is the one error,
// so a caller never has to tell them apart - and never sees an error of the
// engine instead.
test("an envelope from outside is read within its budget or refused", async ({ shell }) => {
  const out = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/sign.js");
    const { b64encode, b64decode, utf8, equal } = await import("/assets/js/contracts/b64.js");
    const kid = d.envelope.kid, sig = d.envelope.sig;
    const payload = b64decode(d.envelope.payload);
    const doc = (o) => utf8(JSON.stringify(o));
    const whole = { payload: d.envelope.payload, kid, sig };
    // A payload that fills a small limit to the brim, and one byte more. The
    // budget is the base64url of the limit plus 1 KiB, so the second document
    // is over its budget as well as over its limit.
    const big = b64encode(new Uint8Array(2048));
    // The six characters of a JSON escape for the letter k, built here so that
    // nothing but ASCII stands in this file.
    const esc = String.fromCharCode(92) + "u006b";
    const cases = [
      ["the envelope of sign.json", doc(whole), 4194304, true],
      ["a payload exactly at the limit", doc(whole), payload.length, true],
      ["a payload one byte over the limit", doc(whole), payload.length - 1, false],
      ["a document over its budget", doc({ payload: big, kid, sig }), 15, false],
      ["a member the contract does not know", doc({ ...whole, extra: 1 }), 4194304, false],
      ["a member twice", utf8(`{"payload":"${d.envelope.payload}","kid":"${kid}","sig":"${sig}","sig":"${sig}"}`), 4194304, false],
      ["a member that is missing", doc({ payload: d.envelope.payload, kid }), 4194304, false],
      ["a member that is null", doc({ ...whole, sig: null }), 4194304, false],
      ["a payload that is no base64url", doc({ ...whole, payload: "a b c" }), 4194304, false],
      ["a payload of no bytes", doc({ ...whole, payload: "" }), 4194304, false],
      ["a key id of 21 characters", doc({ ...whole, kid: kid.slice(0, 21) }), 4194304, false],
      ["a signature of 63 bytes", doc({ ...whole, sig: sig.slice(0, 84) }), 4194304, false],
      // A member name written as an escape is that name and no other, so it
      // neither slips past the three the contract knows nor counts twice.
      ["a member name written as an escape", utf8(`{"payload":"${d.envelope.payload}","${esc}id":"${kid}","sig":"${sig}"}`), 4194304, true],
      ["a member twice, once as an escape", utf8(`{"payload":"${d.envelope.payload}","kid":"${kid}","${esc}id":"${kid}","sig":"${sig}"}`), 4194304, false],
      ["two envelopes", utf8(JSON.stringify(whole) + JSON.stringify(whole)), 4194304, false],
      ["no JSON at all", utf8("envelope"), 4194304, false],
      ["nothing", new Uint8Array(0), 4194304, false],
      ["an array", utf8("[]"), 4194304, false],
      ["a limit of 0", doc(whole), 0, false],
      ["a limit below 0", doc(whole), -1, false],
      ["a limit that is missing", doc(whole), undefined, false],
      ["a limit of Infinity", doc(whole), Infinity, false],
      ["a limit that is a big integer", doc(whole), BigInt(4194304), false],
      ["a document that is no bytes", JSON.stringify(whole), 4194304, false],
      ["a document that is missing", undefined, 4194304, false],
    ];
    const wrong = [];
    for (const [name, b, max, want] of cases) {
      try {
        const env = s.parseEnvelope(b, max);
        if (!want) { wrong.push(`${name}: was read`); continue; }
        if (!equal(env.payload, payload) || env.kid !== kid || b64encode(env.sig) !== sig) {
          wrong.push(`${name}: came back changed`);
        }
      } catch (e) {
        if (want) { wrong.push(`${name}: threw ${e.constructor.name}: ${e.message}`); continue; }
        // One error for every failure, as in Go, and never one of the engine.
        if (e.constructor.name !== "BadEnvelope") wrong.push(`${name}: threw ${e.constructor.name}: ${e.message}`);
      }
    }
    // The budget itself: the base64url of the limit plus 1 KiB, and no limit
    // that is none.
    const budget = s.envelopeBudget(4194304);
    const small = s.envelopeBudget(15);
    const sizes = [budget.size, small.size];
    const budgetRefused = [];
    for (const bad of [0, -1, undefined, NaN, Infinity, 15.5, "15"]) {
      try {
        s.envelopeBudget(bad);
        budgetRefused.push(String(bad));
      } catch (e) {
        if (e.constructor.name !== "BadEnvelope") budgetRefused.push(`${bad}: threw ${e.constructor.name}`);
      }
    }
    return { wrong, sizes, budgetRefused };
  }, v);
  expect(out.wrong).toEqual([]);
  expect(out.budgetRefused).toEqual([]);
  // Go: enc.B64.EncodedLen(maxPayload) + 1 KiB, with maxPayload capped at 4 MiB.
  expect(out.sizes).toEqual([Math.floor((4194304 * 8 + 5) / 6) + 1024, Math.floor((15 * 8 + 5) / 6) + 1024]);
});
