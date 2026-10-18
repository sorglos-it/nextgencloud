import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// The fuzz target for the proofs (CLAUDE.md asks every parser for one). Two
// parsers sit inside verify: the DER of an ES256 signature and the client
// data, the one document of the contracts that is read leniently. Both take
// bytes an authenticator - or an attacker - wrote, and both run before any
// answer is given.
//
// ES256 is used throughout, because it is the one algorithm both engines here
// can really check: what comes out true has been through importKey and verify
// of the Web Crypto API, not only through the gates in front of them.
//
// What the target holds the module against:
//
//   - verify answers true or false and never throws, whatever it is handed. A
//     TypeError or a RangeError from the engine is no answer a contract can
//     give, and it is where the engines differ.
//   - it answers true for exactly those rounds that hand over the signature
//     of the vector unchanged, and false for every other one. A module that
//     said false to everything would keep that rule by half, which is what
//     the count of the true answers below is for.
//   - validateProof and validateKDF answer a boolean for every input, and
//     everything they call whole really keeps the rules. The rules stand here
//     in full and are not taken from the module under test.
//   - and enough rounds get as far as the cryptography at all. Every rule
//     above holds for a module that refuses everything before the first hash,
//     so without a floor under the deep rounds - and under the ones that have
//     to come out true - a step backwards stays green.
//
// Seed and rounds come from the environment, so a failure can be run again:
// NGC_FUZZ_SEED=123 NGC_FUZZ_ROUNDS=6000 npm test -- contracts/proof-fuzz
const v = vectors("proof.json");
const seed = Number(process.env.NGC_FUZZ_SEED || 20260923) >>> 0;
const rounds = Number(process.env.NGC_FUZZ_ROUNDS || 800);

test(`fuzz: true only for the one right proof, ${rounds} inputs from seed ${seed}`, async ({ shell }) => {
  // Every deep round goes through importKey and verify of the Web Crypto API,
  // and the WebKit that Playwright brings needs about fifty times as long for
  // that as Chromium does. The run is the same in both; only the clock is
  // set wide enough for the slower one.
  test.setTimeout(240_000);
  const out = await shell.evaluate(async ([d, seed, rounds]) => {
    const p = await import("/assets/js/contracts/proof.js");
    const { b64decode, b64encode, utf8 } = await import("/assets/js/contracts/b64.js");

    let state = seed || 1;
    const next = () => {
      state ^= state << 13; state >>>= 0;
      state ^= state >> 17;
      state ^= state << 5; state >>>= 0;
      return state / 4294967296;
    };
    const upto = (n) => Math.floor(next() * n);
    const pick = (a) => a[upto(a.length)];
    const randomBytes = (n) => {
      const out = new Uint8Array(n);
      for (let i = 0; i < n; i++) out[i] = upto(256);
      return out;
    };

    const c = d.proofs.find((x) => x.valid && x.key.alg === "ES256");
    const key = { ...c.key, pub: b64decode(c.key.pub), credId: b64decode(c.key.credId) };
    const payload = b64decode(c.payload);
    const proof = {
      kind: c.proof.kind, kid: c.proof.kid, sig: b64decode(c.proof.sig),
      authData: b64decode(c.proof.authData), clientData: b64decode(c.proof.clientData),
    };
    const der = proof.sig;
    const same = (a, b) => {
      if (!(a instanceof Uint8Array) || !(b instanceof Uint8Array) || a.length !== b.length) return false;
      for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
      return true;
    };

    // The two numbers of the signature, split here by hand so the spellings
    // below are the test's own and not the module's.
    const rLen = der[3];
    const r = Array.from(der.subarray(4, 4 + rLen));
    const sLen = der[4 + rLen + 1];
    const s = Array.from(der.subarray(4 + rLen + 2, 4 + rLen + 2 + sLen));
    const build = (rr, ss) => {
      const body = [0x02, rr.length, ...rr, 0x02, ss.length, ...ss];
      return new Uint8Array([0x30, body.length & 0xff, ...body]);
    };
    const withByte = (u8, i, value) => { const out = Uint8Array.from(u8); out[i] = value; return out; };
    // Every spelling a signature can be written in, right and wrong: the one
    // Go wrote, the same two numbers put together again, a zero in front that
    // nothing needs, a length that does not fit, bytes behind the sequence,
    // one bit turned over, and bytes that are no signature at all.
    const signature = () => {
      switch (upto(10)) {
        case 0: return der;
        case 1: return build(r, s);
        case 2: return build(upto(2) === 0 ? [0, ...r] : r, upto(2) === 0 ? [0, ...s] : s);
        case 3: return new Uint8Array([...der, upto(256)]);
        case 4: return withByte(der, upto(2) === 0 ? 1 : 3, upto(256));
        case 5: return der.subarray(0, upto(der.length));
        case 6: { const o = Uint8Array.from(der); o[upto(o.length)] ^= 1 << upto(8); return o; }
        case 7: return build(Array.from(randomBytes(1 + upto(34))), Array.from(randomBytes(1 + upto(34))));
        case 8: return randomBytes(upto(80));
        default: {
          const a = Array.from(der);
          const i = upto(a.length + 1);
          if (upto(2) === 0) a.splice(i, 0, upto(256)); else a.splice(i, 1);
          return new Uint8Array(a);
        }
      }
    };

    // Client data as a browser could write it and as nobody would: the
    // document of the vector, one with a member changed, one with members
    // nobody asked for, and text that is no JSON.
    const rightChallenge = b64encode(await p.challenge(c.purpose, payload));
    const origin = d.rp.origins[0];
    const clientData = () => {
      switch (upto(8)) {
        case 0: return utf8(JSON.stringify({ type: "webauthn.get", challenge: rightChallenge, origin, crossOrigin: false, extra: upto(100) }));
        case 1: return utf8(JSON.stringify({ type: pick(["webauthn.create", "", "WEBAUTHN.GET", "Type"]), challenge: rightChallenge, origin, crossOrigin: false }));
        case 2: return utf8(JSON.stringify({ type: "webauthn.get", challenge: pick([rightChallenge.slice(0, 42), rightChallenge + "A", "", b64encode(randomBytes(32))]), origin, crossOrigin: false }));
        case 3: return utf8(JSON.stringify({ type: "webauthn.get", challenge: rightChallenge, origin: pick([origin + ".evil.example", "https://evil.example", origin.slice(0, -1), ""]), crossOrigin: false }));
        case 4: return utf8(JSON.stringify({ type: "webauthn.get", challenge: rightChallenge, origin, crossOrigin: pick([true, "false", 0, 1, [], {}]) }));
        case 5: return utf8(pick(["null", "[]", '"webauthn.get"', "0", "", "{", "{}", '{"type":"webauthn.get"}']));
        case 6: return randomBytes(upto(40));
        default: {
          const a = Array.from(proof.clientData);
          a[upto(a.length)] ^= 1 << upto(8);
          return new Uint8Array(a);
        }
      }
    };

    const otherKid = "AAAAAAAAAAAAAAAAAAAAAA";
    const found = [];
    const say = (why, what) => { if (found.length < 8) found.push({ why, what }); };
    const count = { deep: 0, shallow: 0, yes: 0, verified: 0 };

    for (let i = 0; i < rounds && found.length < 8; i++) {
      // Two thirds of the rounds change nothing but the signature, so nothing
      // in front of the cryptography can decide and the answer really comes
      // out of importKey and verify of the Web Crypto API.
      if (upto(3) !== 0) {
        const sig = signature();
        const want = same(sig, der);
        count.deep++;
        if (want) count.yes++;
        let got;
        try {
          got = await p.verify({ ...proof, sig }, key, d.rp, c.purpose, payload);
        } catch (e) {
          say(`verify threw ${e.constructor.name}: ${e.message}`, b64encode(sig instanceof Uint8Array ? sig : new Uint8Array(0)));
          continue;
        }
        if (typeof got !== "boolean") say(`verify answered ${typeof got}`, String(got));
        else if (got !== want) say(`verify answered ${got}, ${want} wanted`, b64encode(sig));
        if (got === true) count.verified++;
        continue;
      }
      // The remaining rounds break something in front of the signature. Each
      // of them really changes what it touches, so every one of them has to
      // come out false.
      count.shallow++;
      const pr = { ...proof };
      const k = { ...key };
      let rp = d.rp;
      switch (upto(6)) {
        case 0: pr.clientData = clientData(); break;
        case 1: { const a = Array.from(proof.authData); a[upto(a.length)] ^= 1 << upto(8); pr.authData = new Uint8Array(a); break; }
        case 2: pr.authData = pick([proof.authData.subarray(0, 36), new Uint8Array(4097), new Uint8Array(0), randomBytes(37)]); break;
        case 3: pr.kid = pick([otherKid, c.proof.kid.slice(0, 21), c.proof.kid + "A", 7, undefined]); break;
        case 4: { const o = Uint8Array.from(key.pub); o[upto(o.length)] ^= 1 << upto(8); k.pub = o; break; }
        default: rp = pick([{ id: "evil.example", origins: d.rp.origins }, { id: d.rp.id, origins: ["https://evil.example"] },
          { id: d.rp.id, origins: [] }, { id: d.rp.id, origins: d.rp.origins[0] }, { id: d.rp.id }, {}, null]); break;
      }
      let got;
      try {
        got = await p.verify(pr, k, rp, c.purpose, payload);
      } catch (e) {
        say(`verify threw ${e.constructor.name}: ${e.message}`, "a shallow round");
        continue;
      }
      if (got !== false) say(`verify answered ${JSON.stringify(got)} for a proof that was broken on purpose`, "a shallow round");
    }

    // validateProof and validateKDF, over what no parser would ever hand
    // them. Whatever they call whole has to keep the rules, and the rules
    // stand here and not in the module.
    const kinds = ["password", "passkey", "token", "", 7, undefined, null];
    const kids = [c.proof.kid, otherKid, otherKid.slice(0, 21), otherKid + "A", "+" + otherKid.slice(1), "", 22, undefined];
    const blobs = [undefined, null, new Uint8Array(0), new Uint8Array(1), new Uint8Array(36), new Uint8Array(37),
      new Uint8Array(1024), new Uint8Array(1025), new Uint8Array(4096), new Uint8Array(4097), "a".repeat(40), 37];
    for (let i = 0; i < 2000 && found.length < 8; i++) {
      const pr = { kind: pick(kinds), kid: pick(kids), sig: pick(blobs), authData: pick(blobs), clientData: pick(blobs) };
      let got;
      try {
        got = p.validateProof(pr);
      } catch (e) {
        say(`validateProof threw ${e.constructor.name}: ${e.message}`, JSON.stringify(pr.kind ?? null));
        continue;
      }
      if (typeof got !== "boolean") { say(`validateProof answered ${typeof got}`, String(got)); continue; }
      if (!got) continue;
      const bytesOf = (x) => (x === undefined || x === null ? 0 : x instanceof Uint8Array ? x.length : -1);
      const auth = bytesOf(pr.authData), client = bytesOf(pr.clientData);
      const okKid = typeof pr.kid === "string" && pr.kid.length === 22;
      const okSig = pr.sig instanceof Uint8Array && pr.sig.length >= 1 && pr.sig.length <= 1024;
      const okKind = pr.kind === "password"
        ? auth === 0 && client === 0
        : pr.kind === "passkey" && auth >= 37 && auth <= 4096 && client >= 1 && client <= 4096;
      if (!okKid || !okSig || !okKind) say("validateProof called a proof whole that is not", JSON.stringify({ kind: pr.kind, auth, client }));
    }
    const algs = ["argon2id", "argon2i", "", 7, undefined];
    const salts = [new Uint8Array(16), new Uint8Array(15), new Uint8Array(17), "AQIDBAUGBwgJCgsMDQ4PEA", undefined, null];
    const numbers = [0, 1, 3, 4, 5, 10, 11, 19455, 19456, 65536, 1048576, 1048577, 65536.5, NaN, Infinity,
      -1, "65536", "3", true, undefined, null];
    for (let i = 0; i < 2000 && found.length < 8; i++) {
      const k = { alg: pick(algs), salt: pick(salts), m: pick(numbers), t: pick(numbers), p: pick(numbers) };
      let got;
      try {
        got = p.validateKDF(k);
      } catch (e) {
        say(`validateKDF threw ${e.constructor.name}: ${e.message}`, JSON.stringify(String(k.alg)));
        continue;
      }
      if (typeof got !== "boolean") { say(`validateKDF answered ${typeof got}`, String(got)); continue; }
      if (!got) continue;
      const whole = k.alg === "argon2id" && k.salt instanceof Uint8Array && k.salt.length === 16
        && Number.isInteger(k.m) && k.m >= 19456 && k.m <= 1048576
        && Number.isInteger(k.t) && k.t >= 1 && k.t <= 10
        && Number.isInteger(k.p) && k.p >= 1 && k.p <= 4;
      if (!whole) say("validateKDF called parameters whole that are not", JSON.stringify({ m: String(k.m), t: String(k.t), p: String(k.p) }));
    }
    return { found, count };
  }, [v, seed, rounds]);

  // What was found comes first, and everything below only means anything
  // when nothing was: the loop stops at the eighth finding, so a fault in
  // round five leaves deep, shallow and yes far under their floors as well.
  // Those floors are here to catch a run that did not really run, not to
  // report a fault a second, third and fourth time under another name.
  expect(out.found).toEqual([]);
  if (out.found.length === 0) {
    // Two thirds of the rounds are deep by the draw above; the floor is at 40
    // in 100, a good way under it. It counts in rounds and not in a fixed
    // number, because the rounds are a knob this file advertises itself: a
    // fixed count would turn every short run red without anything being wrong.
    expect(out.count.deep).toBeGreaterThan(Math.floor(rounds * 0.4));
    expect(out.count.shallow).toBeGreaterThan(Math.floor(rounds * 0.15));
    // Two of the ten spellings hand over the signature of the vector
    // unchanged, so about a fifth of the deep rounds have to come out true:
    // without this floor a module that answered false to everything would
    // keep every rule above. The floor is at 3 in 100 of all rounds, about a
    // third of what the draw gives.
    expect(out.count.yes).toBeGreaterThan(Math.floor(rounds * 0.03));
    // And nothing else ever came out true.
    expect(out.count.verified).toBe(out.count.yes);
  }
});
