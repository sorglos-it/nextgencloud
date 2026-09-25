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
      // A purpose that is not text. Whoever writes PURPOSE_CATALOGUE for
      // PURPOSE_CATALOG hands over undefined, and the key id of these cases
      // is the right one, so nothing stops them before the message is built.
      // The answer has to be false like everything else that cannot be
      // checked: an exception through `if (await verify(...))` does not close
      // the gate, it walks past it.
      ["a purpose that is missing", env.payload, env.kid, env.sig, pub, undefined],
      ["a purpose that is a number", env.payload, env.kid, env.sig, pub, 7],
      ["a purpose that is a list", env.payload, env.kid, env.sig, pub, [d.purpose]],
      ["a purpose with a toString", env.payload, env.kid, env.sig, pub, { toString: () => d.purpose }],
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
    // The shape of a contract is a constant of that contract. Whoever can
    // change envelopeShape decides what parseEnvelope reads: one
    // envelopeShape.m.extra = str and a member the contract does not know
    // walks through every envelope from outside. The lock sits where the
    // shape is built (json.js: S), so no contract has to set it for itself.
    const opened = [];
    for (const [what, change] of [
      ["a member added", (sh) => { sh.m.extra = { k: "str" }; }],
      ["a member replaced", (sh) => { sh.m.payload = { k: "str" }; }],
    ]) {
      const before = JSON.stringify(s.envelopeShape);
      try {
        change(s.envelopeShape);
      } catch {
        // A module is strict and a frozen shape throws there; the code of
        // this call is no module, and there the change is dropped without a
        // word. What counts is that the shape is the same afterwards.
      }
      if (JSON.stringify(s.envelopeShape) !== before) opened.push(what);
    }
    let afterShape;
    try {
      s.parseEnvelope(doc({ ...whole, extra: 1 }), 4194304);
      afterShape = "read an envelope with a member the contract does not know";
    } catch (e) {
      afterShape = e.constructor.name;
    }
    return { wrong, sizes, budgetRefused, opened, afterShape };
  }, v);
  expect(out.wrong).toEqual([]);
  expect(out.budgetRefused).toEqual([]);
  expect(out.opened).toEqual([]);
  expect(out.afterShape).toBe("BadEnvelope");
  // Go: enc.B64.EncodedLen(maxPayload) + 1 KiB, with maxPayload capped at 4 MiB.
  expect(out.sizes).toEqual([Math.floor((4194304 * 8 + 5) / 6) + 1024, Math.floor((15 * 8 + 5) / 6) + 1024]);
});

// Everything an envelope is made of comes from an object the caller owns, and
// such an object can fight back: a field behind a getter that throws, or a
// proxy that throws on every read. Go is handed an Envelope of three typed
// fields and can meet none of this. The three checks here promise an answer
// and never an exception - a caller who had to catch one around a signature
// is a caller who lets one through - so the reads themselves stand inside the
// guard, not in front of it.
test("a check of a signature answers false for an envelope that fights back", async ({ shell }) => {
  const out = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/sign.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const pub = b64decode(d.public);
    const payload = b64decode(d.envelope.payload);
    const env = { payload, kid: d.envelope.kid, sig: b64decode(d.envelope.sig) };
    const boom = () => { throw new TypeError("this field fights back"); };
    // Two kinds of byte string that are no byte string: one that is bytes to
    // instanceof and throws when its length is read, and one that throws
    // before instanceof can even answer.
    const trap = (u8) => new Proxy(u8, { get: (t, k) => (k === "length" ? boom() : Reflect.get(t, k)) });
    const hidden = (u8) => new Proxy(u8, { getPrototypeOf: boom });
    const envelopes = [
      ["a payload behind a getter that throws", { get payload() { return boom(); }, kid: env.kid, sig: env.sig }],
      ["a key id behind a getter that throws", { payload, get kid() { return boom(); }, sig: env.sig }],
      ["a signature behind a getter that throws", { payload, kid: env.kid, get sig() { return boom(); } }],
      ["an envelope behind a proxy that throws", new Proxy({ ...env }, { get: boom })],
      ["a payload whose length throws", { payload: trap(payload), kid: env.kid, sig: env.sig }],
      ["a payload no instanceof can look at", { payload: hidden(payload), kid: env.kid, sig: env.sig }],
      ["a signature whose length throws", { payload, kid: env.kid, sig: trap(env.sig) }],
      ["a signature no instanceof can look at", { payload, kid: env.kid, sig: hidden(env.sig) }],
    ];
    const wrong = [];
    for (const [name, e] of envelopes) {
      const calls = [
        ["validateEnvelope", () => s.validateEnvelope(e, 4194304)],
        ["verify", () => s.verify(e, pub, d.purpose)],
      ];
      for (const [fn, call] of calls) {
        try {
          const got = await call();
          if (got !== false) wrong.push(`${fn} with ${name}: answered ${JSON.stringify(got)}`);
        } catch (err) {
          wrong.push(`${fn} with ${name}: threw ${err.constructor.name}: ${err.message}`);
        }
      }
    }
    // verifyRaw is handed its three byte strings one by one, so each one
    // fights back on its own.
    const msg = s.message(d.purpose, payload);
    const raws = [
      ["a key whose length throws", trap(pub), env.sig, msg],
      ["a key no instanceof can look at", hidden(pub), env.sig, msg],
      ["a signature whose length throws", pub, trap(env.sig), msg],
      ["a signature no instanceof can look at", pub, hidden(env.sig), msg],
      ["a message no instanceof can look at", pub, env.sig, hidden(msg)],
    ];
    for (const [name, key, sig, m] of raws) {
      try {
        const got = await s.verifyRaw(key, sig, m);
        if (got !== false) wrong.push(`verifyRaw with ${name}: answered ${JSON.stringify(got)}`);
      } catch (err) {
        wrong.push(`verifyRaw with ${name}: threw ${err.constructor.name}: ${err.message}`);
      }
    }
    return wrong;
  }, v);
  expect(out).toEqual([]);
});

// What the guard next door cannot see, and what this round was found by.
//
// tests/contracts/bytes.spec.js reads WHICH properties a place touched and in
// what order, and it holds the three positions of verifyRaw to reading length
// after the view. That is a rule about the code. This is the same property
// measured on the outcome: two objects that fight back in opposite ways, and
// both have to be answered false.
//
// The two are opposites, and that is why neither half of verifyRaw is enough
// on its own. Measured in chromium, each of the three stands built as the only
// change in a copy:
//
//                                            flipping   throwing
//   length first, then the view (f24686c)    true       false
//   the view only, no length read            false      true
//   the view first, then the length          false      false
//
// The flipping object holds 33 real bytes, 32 of key and one of rubbish. Its
// length getter sets a flag and its byteLength answers 33 before that flag and
// 32 after it, so READING the length changes what the next view will be. An
// honest reading sees 33 bytes and refuses; a reading that happens before the
// view is built hands the engine the 32 real bytes of a key nobody offered as
// a key, together with a signature that is genuinely over them.
//
// The throwing object is the proxy of the test above, whose length throws. It
// needs the length to be read at all, or nothing throws and a real signature
// behind it is answered true.
//
// The table is the key and the signature. The message behaves differently and
// it is measured too, because a case nobody has measured is a case nobody
// knows: a message whose length moves the view was answered false on every one
// of the three stands, though not for the same reason - the signature is over
// the honest bytes, so a view of one byte more fails to verify whatever else
// happens. A message whose length THROWS did follow the table: true on the
// first two stands and false here. That third position is new to this round;
// before it, only the key and the signature were asked for a length at all.
//
// Reach, and it belongs in the same breath: only from JavaScript of this same
// origin. Nothing from the network can hand a contract an object with a getter
// on it.
// Seven shapes of one mistake, at all three arguments, and the reason this
// test is built as a sweep and not as a list of the shapes somebody thought
// of. Five rounds stood here. Each fenced the shapes it had measured by
// putting the readings in a new order; each time the next measurement found a
// shape the new order did not cover, because a getter is code and runs
// wherever the object is touched. The shape that ended the argument needs no
// getter at all:
//
//   const plain = new Uint8Array(64).fill(0x5a);   // an ordinary array
//   queueMicrotask(() => plain.set(realSignature));
//   await verifyRaw(pub, plain, msg);              // on 8b6fa87: true
//
// Every synchronous check saw rubbish; the engine got the real signature,
// because the rewrite lands during the awaits. No arrangement of readings can
// see that, and no guard that watches WHICH PROPERTIES WERE TOUCHED can see it
// either - an ordinary array touches nothing unusual. So the fence is no
// longer an order. verifyRaw copies the caller's bytes out of reach before it
// decides anything, and what the caller cannot reach it cannot steer.
//
// Every object below hands over RUBBISH and reveals the bytes that would
// verify only through its trick. So every answer here must be false, and a
// true is not a strictness wobble but a forgery: the engine would have
// verified bytes that no check ever saw.
test("verifyRaw decides on a copy no getter can reach", async ({ shell }) => {
  test.skip(!(await hasEd25519(shell, v.public)),
    "no Ed25519 in this engine's Web Crypto: it answers false for everything, hostile or not");
  const got = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/sign.js");
    const { b64decode, bytesOf, viewOf } = await import("/assets/js/contracts/b64.js");
    const pub = b64decode(d.public);
    const sig = b64decode(d.envelope.sig);
    const msg = s.message(d.purpose, b64decode(d.envelope.payload));
    const out = {};

    // A buffer of n (or n+1) rubbish bytes, and `reveal()` writes the bytes
    // that would verify into it. Nothing here starts out honest.
    const bed = (real, extra) => {
      const ab = new ArrayBuffer(real.length + (extra || 0));
      const raw = new Uint8Array(ab);
      raw.fill(0x5a);
      return { ab, raw, n: real.length, reveal: () => raw.set(real) };
    };
    const SHAPES = {
      // reading length reveals the bytes and shrinks the reported byteLength
      umlegend: (real) => { const b = bed(real, 1); return new (class extends Uint8Array {
        get length() { b.reveal(); this.f = 1; return b.n; }
        get byteLength() { return this.f ? b.n : b.n + 1; } })(b.ab, 0, b.n + 1); },
      // an honest number that is never given: the getter fights back
      werfend: (real) => { const b = bed(real, 0); return new (class extends Uint8Array {
        get length() { b.reveal(); throw new TypeError("this field fights back"); } })(b.ab); },
      // the honest number, and the buffer rewritten while it is read
      umschreibend: (real) => { const b = bed(real, 0); return new (class extends Uint8Array {
        get length() { b.reveal(); return b.n; } })(b.ab); },
      // the mirror of umlegend: the reported byteLength grows instead
      wachsend: (real) => { const b = bed(real, 1); return new (class extends Uint8Array {
        get length() { b.reveal(); this.g = 1; return b.n; }
        get byteLength() { return this.g ? b.n + 1 : b.n; } })(b.ab, 0, b.n + 1); },
      // every reading honest, and the rewrite handed in afterwards
      nachreichend: (real) => { const b = bed(real, 0); return new (class extends Uint8Array {
        get length() { queueMicrotask(b.reveal); return b.n; } })(b.ab); },
      // the buffer taken away underneath, from either getter
      entziehendL: (real) => { const b = bed(real, 0); return new (class extends Uint8Array {
        get length() { b.reveal(); try { structuredClone(b.ab, { transfer: [b.ab] }); } catch { /* gone already */ } return b.n; } })(b.ab); },
      entziehendB: (real) => { const b = bed(real, 0); return new (class extends Uint8Array {
        get byteLength() { b.reveal(); try { structuredClone(b.ab, { transfer: [b.ab] }); } catch { /* gone already */ } return b.n; } })(b.ab); },
      // a Proxy that answers the second reading of every index differently
      wechselnd: (real) => { const t = new Uint8Array(real.length).fill(0x5a); const c = new Map();
        return new Proxy(t, { get(tt, p, r) {
          const k = (c.get(p) ?? 0) + 1; c.set(p, k);
          if (p === "length" || p === "byteLength") return real.length;
          if (typeof p === "string" && p >= "0" && p <= "999999" && String(Number(p)) === p) {
            return k === 1 ? 0x5a : real[Number(p)];
          }
          return Reflect.get(tt, p, r);
        } }); },
    };

    // That the shapes really are hostile, said out loud: a later round that
    // makes these objects harmless must not leave this test green.
    const witness = SHAPES.umlegend(pub);
    out.flipBefore = witness.byteLength;
    void witness.length;
    out.flipAfter = witness.byteLength;
    const rewriter = SHAPES.umschreibend(pub);
    const beforeRead = Array.from(new Uint8Array(rewriter.buffer).slice(0, 4));
    void rewriter.length;
    out.rewrote = beforeRead.join() !== Array.from(new Uint8Array(rewriter.buffer).slice(0, 4)).join();

    // The honest answer first, so a run in which nothing verifies at all
    // cannot look like a run in which everything was refused on purpose.
    out.honest = await s.verifyRaw(pub, sig, msg);

    const wrong = [];
    for (const [name, make] of Object.entries(SHAPES)) {
      for (const [where, real] of [["key", pub], ["sig", sig], ["msg", msg]]) {
        const a = [pub, sig, msg];
        a[where === "key" ? 0 : where === "sig" ? 1 : 2] = make(real);
        try {
          const r = await s.verifyRaw(a[0], a[1], a[2]);
          if (r !== false) wrong.push(`${name} at ${where}: answered ${JSON.stringify(r)}`);
        } catch (e) {
          wrong.push(`${name} at ${where}: threw ${e.constructor.name}: ${e.message}`);
        }
      }
    }
    // And the shape that needs no getter at all: an ordinary array whose
    // rewrite is simply scheduled. This is the one no ordering could reach.
    for (const [where, real] of [["key", pub], ["sig", sig], ["msg", msg]]) {
      const a = [pub, sig, msg];
      const plain = new Uint8Array(real.length).fill(0x5a);
      a[where === "key" ? 0 : where === "sig" ? 1 : 2] = plain;
      queueMicrotask(() => plain.set(real));
      const r = await s.verifyRaw(a[0], a[1], a[2]);
      if (r !== false) wrong.push(`a plain array rewritten in a microtask at ${where}: answered ${JSON.stringify(r)}`);
    }
    out.wrong = wrong;

    // THE COPY READS NOTHING AT THE OBJECT. Every accessor an object has for
    // its bytes throws; if the copying consulted any of them it could not come
    // back with the bytes at all. viewOf on the same object is the contrast:
    // it must read three of them, so it refuses.
    class AllThrow extends Uint8Array {
      get length() { throw new Error("length"); }
      get byteLength() { throw new Error("byteLength"); }
      get buffer() { throw new Error("buffer"); }
      get byteOffset() { throw new Error("byteOffset"); }
    }
    const src = new Uint8Array([1, 2, 3, 4]);
    const sealed = new AllThrow(src.buffer, 0, 4);
    try { out.copiedThroughThrowingAccessors = Array.from(bytesOf(sealed, "x")); }
    catch (e) { out.copiedThroughThrowingAccessors = "threw " + e.constructor.name; }
    try { viewOf(sealed, "x"); out.viewOfSameObject = "answered"; }
    catch (e) { out.viewOfSameObject = "threw " + e.constructor.name; }

    // And the whole ordered log of the byte properties verifyRaw touches on a
    // caller's object: two readings, both AFTER the copy, and never buffer or
    // byteOffset - because nothing is ever built on the caller's memory.
    let seq = "";
    class Probe extends Uint8Array {
      get length() { seq += "L"; return super.length; }
      get byteLength() { seq += "B"; return super.byteLength; }
      get buffer() { seq += "F"; return super.buffer; }
      get byteOffset() { seq += "O"; return super.byteOffset; }
    }
    const copyOf = (a) => new Probe(a.buffer.slice(a.byteOffset, a.byteOffset + a.length));
    out.touched = {};
    for (const [where, real] of [["key", pub], ["sig", sig], ["msg", msg]]) {
      const a = [pub, sig, msg];
      a[where === "key" ? 0 : where === "sig" ? 1 : 2] = copyOf(real);
      seq = "";
      out.touched[where] = { order: seq, answer: await s.verifyRaw(a[0], a[1], a[2]) };
      out.touched[where].order = seq;
    }
    return out;
  }, v);

  // The honest signature still verifies, so the refusals below are refusals
  // and not a broken vector.
  expect(got.honest).toBe(true);
  // The shapes are hostile: one reading of length moves what a view built
  // afterwards would hold, and one of them rewrites the buffer outright.
  expect([got.flipBefore, got.flipAfter]).toEqual([33, 32]);
  expect(got.rewrote).toBe(true);
  // Seven shapes at three arguments, and the plain array at three more: every
  // one of them refused. A name here is a forgery, not a wobble.
  expect(got.wrong).toEqual([]);
  // The copying consulted no accessor of the caller's: all four throw and the
  // bytes still come back, where viewOf - which must read three of them -
  // refuses.
  expect(got.copiedThroughThrowingAccessors).toEqual([1, 2, 3, 4]);
  expect(got.viewOfSameObject).toBe("threw BadInput");
  // And the whole reading verifyRaw does at a caller's object is length and
  // byteLength, in that order, after the copy. No buffer, no byteOffset:
  // nothing is built on the caller's memory, so nothing of the caller's can
  // move what the engine is given.
  for (const where of ["key", "sig", "msg"]) {
    expect(got.touched[where]).toEqual({ order: "LB", answer: true });
  }
});

// One reading of the key, for both things that are done with it.
//
// verify does two separate things with the key it is handed: it works out the
// id and compares it against the one the envelope names, and it checks the
// signature. Two readings of one object are two byte strings whenever the
// object is not honest - and that is not untidiness, it is a forged envelope:
// the id is worked out over a key the caller trusts, so the envelope passes
// for that key, and the signature is then checked against a key the attacker
// owns and has signed with. Both halves answer truthfully about the bytes they
// were given. The envelope is still accepted for the wrong key.
//
// The object below needs no getter at all. It is an ordinary Uint8Array whose
// bytes are swapped in a microtask, which lands while the id is being awaited -
// between the two readings, and out of reach of any rule about order.
//
// This test was written because a mutant survived without it: `verify` reading
// pubRaw twice instead of its one copy left every other test in this file
// green.
test("verify reads the key once, so the id and the signature are one key's", async ({ shell }) => {
  test.skip(!(await hasEd25519(shell, v.public)),
    "no Ed25519 in this engine's Web Crypto: it answers false for everything, hostile or not");
  const got = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/sign.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const trusted = b64decode(d.public);
    const payload = b64decode(d.envelope.payload);
    const msg = s.message(d.purpose, payload);

    // A key of the attacker's own, and a signature that is genuinely his.
    const mine = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]);
    const minePub = new Uint8Array(await crypto.subtle.exportKey("raw", mine.publicKey));
    const mineSig = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, mine.privateKey, msg));
    const out = {};
    // The signature really is his, so a false below is a refusal of the swap
    // and not a signature that never verified anywhere.
    out.signatureIsGenuine = await s.verifyRaw(minePub, mineSig, msg);
    out.signatureIsNotTheTrustedKeys = await s.verifyRaw(trusted, mineSig, msg);

    // The envelope names the TRUSTED key and carries the ATTACKER's signature.
    const env = { payload, kid: await s.keyID(trusted), sig: mineSig };
    // An ordinary array: the trusted key now, the attacker's key one microtask
    // later - that is, between working out the id and checking the signature.
    const twoFaced = new Uint8Array(trusted.length);
    twoFaced.set(trusted);
    out.plainArray = Object.getPrototypeOf(twoFaced) === Uint8Array.prototype;
    queueMicrotask(() => twoFaced.set(minePub));
    out.accepted = await s.verify(env, twoFaced, d.purpose);
    out.swapHappened = Array.from(twoFaced).join() === Array.from(minePub).join();
    return out;
  }, v);
  // The attacker's signature verifies under his own key and not under the
  // trusted one, so the setup is a real forgery attempt.
  expect(got.signatureIsGenuine).toBe(true);
  expect(got.signatureIsNotTheTrustedKeys).toBe(false);
  // Nothing exotic was needed: an ordinary array, and the swap did happen.
  expect(got.plainArray).toBe(true);
  expect(got.swapHappened).toBe(true);
  // And the envelope is refused: the id and the signature were decided on one
  // and the same reading of the key.
  expect(got.accepted).toBe(false);
});
