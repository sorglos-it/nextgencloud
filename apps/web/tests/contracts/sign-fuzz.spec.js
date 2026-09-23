import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// The fuzz target for the envelopes (CLAUDE.md asks every parser for one).
// sign.json holds a single envelope and no case that has to be refused, so
// there is nothing to compare an answer against; the target holds the module
// against what has to be true for every input at all:
//
//   - parseEnvelope answers with an envelope or throws BadEnvelope, and
//     nothing else. A TypeError or a RangeError from the engine is no answer
//     a contract can give, and it is where the engines differ.
//   - every envelope it answers with is one the contract describes: a payload
//     of 1 to its limit bytes, a key id of 22 base64url characters over 16
//     bytes, a signature of 64 bytes. The rules stand here in full and are
//     not taken from the module under test.
//   - validateEnvelope answers true or false, whatever it is handed.
//   - the message of an envelope carries the purpose and then exactly the
//     payload: a signature covers those bytes and no others.
//   - verify answers false for a key that signed nothing, and never throws.
//   - and enough of the inputs get through at all. Every rule above holds for
//     a module that refuses everything, so without a floor under the accepted
//     ones a step backwards stays green.
//
// Seed and rounds come from the environment, so a failure can be run again:
// NGC_FUZZ_SEED=123 NGC_FUZZ_ROUNDS=50000 npm test -- contracts/sign-fuzz
const v = vectors("sign.json");
const seed = Number(process.env.NGC_FUZZ_SEED || 20260923) >>> 0;
const rounds = Number(process.env.NGC_FUZZ_ROUNDS || 4000);

test(`fuzz: an envelope or BadEnvelope, ${rounds} inputs from seed ${seed}`, async ({ shell }) => {
  const out = await shell.evaluate(async ([d, seed, rounds]) => {
    const s = await import("/assets/js/contracts/sign.js");
    const { b64encode, b64decode, utf8 } = await import("/assets/js/contracts/b64.js");

    let state = seed || 1;
    const next = () => {
      state ^= state << 13; state >>>= 0;
      state ^= state >> 17;
      state ^= state << 5; state >>>= 0;
      return state / 4294967296;
    };
    const upto = (n) => Math.floor(next() * n);
    const pick = (a) => a[upto(a.length)];

    // The pieces an envelope is built from. Of each there are the ones that
    // keep the rule and the ones that sit just outside it: key ids of 21, 22
    // and 23 characters, signatures of 63, 64 and 65 bytes, payloads from
    // none to over a small limit, and text that is no base64url at all. A
    // piece is drawn from the edge cases every third time, so that whole
    // envelopes come out often enough for the checks below to have something
    // to hold.
    const kid = d.envelope.kid, sig = d.envelope.sig;
    const goodKids = [kid, "AAAAAAAAAAAAAAAAAAAAAA", "_".repeat(21) + "w", "0123456789abcdefghijkQ"];
    const oddKids = [kid.slice(0, 21), kid + "A", "+" + kid.slice(1), kid.slice(0, 21) + "B",
      "", "A", "-".repeat(22)];
    const goodSigs = [sig, "A".repeat(86), sig.slice(0, 85) + "A", "_".repeat(85) + "A"];
    const oddSigs = [sig.slice(0, 84), sig + "A", "", "A".repeat(85)];
    const goodPayloads = [d.envelope.payload, "AQ", "AQID", "AQIDBAUGBwgJ", "_-_-",
      b64encode(new Uint8Array(64))];
    const oddPayloads = ["A", "", "a b", "AQI=", "AQ==", String.fromCharCode(228),
      b64encode(new Uint8Array(200)), b64encode(new Uint8Array(2048))];
    const piece = (good, odd) => (upto(3) === 0 ? pick(odd) : pick(good));
    const payloads = [...goodPayloads, ...oddPayloads];
    const names = ["payload", "kid", "sig", "extra", "Payload", "__proto__", "constructor", ""];
    const junk = ["", " ", "\n", "{", "}", "[", "]", ",", ":", "\"", "null", "0", "-", "\\"];

    const member = (name, value) => JSON.stringify(name) + ":" + JSON.stringify(value);
    const document = () => {
      switch (upto(10)) {
        case 0: case 1: return JSON.stringify({ payload: d.envelope.payload, kid, sig });
        case 2: case 3: case 4: case 5: {
          // A whole envelope of pieces, its members in the order the round
          // draws them.
          const parts = [member("payload", piece(goodPayloads, oddPayloads)),
            member("kid", piece(goodKids, oddKids)), member("sig", piece(goodSigs, oddSigs))];
          for (let i = parts.length - 1; i > 0; i--) {
            const j = upto(i + 1);
            [parts[i], parts[j]] = [parts[j], parts[i]];
          }
          if (upto(6) === 0) parts.push(member(pick(names), pick(payloads)));
          if (upto(6) === 0) parts.pop();
          return "{" + parts.join(",") + "}";
        }
        case 6: {
          const parts = [];
          const n = 1 + upto(4);
          for (let i = 0; i < n; i++) parts.push(member(pick(names), pick([...payloads, ...goodKids, ...goodSigs])));
          return "{" + parts.join(",") + "}";
        }
        default: {
          // A whole envelope with one piece of junk pushed into it.
          const base = JSON.stringify({ payload: pick(payloads), kid: pick([...goodKids, ...oddKids]), sig: pick([...goodSigs, ...oddSigs]) });
          const i = upto(base.length + 1);
          return base.slice(0, i) + pick(junk) + base.slice(i);
        }
      }
    };
    const mutate = (b) => {
      const out = Array.from(b);
      const steps = 1 + upto(3);
      for (let r = 0; r < steps && out.length > 0; r++) {
        const i = upto(out.length);
        switch (upto(5)) {
          case 0: out[i] ^= 1 << upto(8); break;
          case 1: out.splice(i, 1); break;
          case 2: out.splice(i, 0, upto(256)); break;
          case 3: out.splice(i, 0, ...out.slice(0, 1 + upto(8))); break;
          default: out.length = i; break;
        }
      }
      return new Uint8Array(out);
    };

    // Limits as a caller may hand them over: whole numbers, and every fourth
    // round one of the values that are no limit at all.
    const goodLimits = [4194304, 4194305, 9007199254740991, 2048, 100, 20];
    const oddLimits = [15, 1, 0, -1, undefined, NaN, Infinity, 1.5, "100"];
    const limits = [...goodLimits, ...oddLimits];
    const purposes = [s.PURPOSE_CLIENT_CERT, s.PURPOSE_API_CERT, s.PURPOSE_TRUST_LIST, s.PURPOSE_TASK,
      s.PURPOSE_CONFIRM, s.PURPOSE_CATALOG, s.PURPOSE_LICENSE, s.PURPOSE_RELEASE];
    // A key that signed nothing: no envelope of this run may ever verify.
    const stranger = b64decode(d.public);
    stranger[0] ^= 1;

    const show = (b) => {
      let out = "";
      for (const byte of b.subarray(0, 120)) {
        out += byte >= 0x20 && byte < 0x7f ? String.fromCharCode(byte) : "\\x" + byte.toString(16).padStart(2, "0");
      }
      return out + (b.length > 120 ? "..." : "");
    };
    const found = [];
    const say = (b, why) => { if (found.length < 8) found.push({ input: show(b), why }); };
    const count = { parsed: 0, valid: 0, refused: 0 };
    const encoder = new TextEncoder();

    for (let i = 0; i < rounds && found.length < 8; i++) {
      const b = i % 4 === 0 ? mutate(encoder.encode(document())) : encoder.encode(document());
      const max = upto(4) === 0 ? pick(oddLimits) : pick(goodLimits);
      const limit = Number.isSafeInteger(max) && max >= 1 ? Math.min(max, 4194304) : 0;
      let env = null;
      try {
        env = s.parseEnvelope(b, max);
      } catch (e) {
        if (e.constructor.name !== "BadEnvelope") say(b, `parseEnvelope threw ${e.constructor.name}: ${e.message}`);
        count.refused++;
        continue;
      }
      count.parsed++;
      // The shape of what came back, spelled out and not asked of the module.
      if (!(env instanceof Object) || env === null) say(b, "no envelope came back");
      else if (!(env.payload instanceof Uint8Array)) say(b, "a payload that is no bytes");
      else if (env.payload.length < 1 || env.payload.length > limit) say(b, `a payload of ${env.payload.length} bytes for a limit of ${limit}`);
      else if (typeof env.kid !== "string" || env.kid.length !== 22) say(b, "a key id that is no 22 characters");
      else if (!(env.sig instanceof Uint8Array) || env.sig.length !== 64) say(b, "a signature that is no 64 bytes");
      else {
        try {
          if (b64decode(env.kid).length !== 16) say(b, "a key id that is no 16 bytes");
        } catch {
          say(b, "a key id that is no base64url");
        }
      }
      // What the module says about its own answer has to agree.
      let ok;
      try {
        ok = s.validateEnvelope(env, max);
      } catch (e) {
        say(b, `validateEnvelope threw ${e.constructor.name}`);
      }
      if (ok !== true) say(b, `validateEnvelope answered ${JSON.stringify(ok)} for an envelope it read`);
      else count.valid++;
      // The message covers the purpose and then exactly the payload.
      const purpose = pick(purposes);
      const m = s.message(purpose, env.payload);
      const head = utf8("ngc/" + purpose);
      if (m.length !== head.length + 1 + env.payload.length) say(b, "a message of the wrong length");
      else {
        let same = m[head.length] === 0;
        for (let k = 0; same && k < head.length; k++) same = m[k] === head[k];
        for (let k = 0; same && k < env.payload.length; k++) same = m[head.length + 1 + k] === env.payload[k];
        if (!same) say(b, "a message that is not the purpose and the payload");
      }
      // Nothing this run builds was ever signed by the stranger.
      try {
        if (await s.verify(env, stranger, purpose) !== false) say(b, "an envelope verified against a key that signed nothing");
      } catch (e) {
        say(b, `verify threw ${e.constructor.name}: ${e.message}`);
      }
    }

    // validateEnvelope is handed what no parser would give it, and still
    // answers true or false.
    const shapes = [null, undefined, 0, "", [], {}, { payload: null, kid: null, sig: null },
      { payload: new Uint8Array(1), kid, sig: new Uint8Array(64) },
      Object.create(null), new Uint8Array(3)];
    for (const shape of shapes) {
      for (const max of limits) {
        let got;
        try {
          got = s.validateEnvelope(shape, max);
        } catch (e) {
          if (found.length < 8) found.push({ input: "shape", why: `validateEnvelope threw ${e.constructor.name}` });
          continue;
        }
        if (typeof got !== "boolean") {
          if (found.length < 8) found.push({ input: "shape", why: `validateEnvelope answered ${typeof got}` });
        }
      }
    }
    return { found, count };
  }, [v, seed, rounds]);

  expect(out.found).toEqual([]);
  // 716 of the 4000 inputs of this seed are read as an envelope, in both
  // engines; the floor is at about a third of that. The number belongs to the
  // engine as much as to the seed - JSON.stringify and the text encoder decide
  // what the generator writes, and whether a signature can be checked at all
  // is the engine's answer too - so it is a floor, not a count to match.
  // Without it a module that had stopped reading anything would keep every
  // rule above and stay green.
  expect(out.count.parsed).toBeGreaterThan(240);
  expect(out.count.valid).toBe(out.count.parsed);
});
