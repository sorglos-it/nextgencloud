import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// The fuzz target for what the API sends the shell (CLAUDE.md asks every
// parser for one): the result of an action, the body of a notice, and the
// signed evidence for an app's user interface. A compromised API is exactly
// the case these parsers exist for, so what arrives here is a document an
// attacker wrote with the shell's own table in front of him.
//
// core has no error class of its own: every refusal is an Error whose message
// begins with "core: ". So that is what the target holds it to, and a
// TypeError or a RangeError from the engine - which is where the engines
// differ from one another - is the finding.
//
//   - parseResult, parseNotice and parseUIProof answer with a document or
//     throw such an Error, and nothing else.
//   - a result that comes back FITS the shape of its action, down to every
//     element. The shape walk stands here and is not taken from the module.
//   - and every string anywhere in an accepted result or notice is empty or
//     printable text: no control character, no format character, no half
//     surrogate, no spacing character but the ASCII space. Every field of
//     every row goes through a grammar or a set of fixed words in core.js, so
//     this holds for all of them at once - and it is the rule that matters,
//     because these strings are put on the page.
//   - an accepted UI proof is ONE of the two forms and never a mixture: a
//     catalog alone from a store that is not the local one, or a task, a
//     client certificate and the user's proof over it with the local store.
//     A proof that carried both would be one kind checked and the other used.
//   - verifyUIProof answers 32 bytes or throws core's Error, whatever it is
//     handed.
//   - validateParams answers true or false and never throws, and encodeParams
//     either refuses or writes at most MAX_PARAMS bytes that it writes again
//     the same way and that validateParams calls whole.
//   - and enough of the inputs get through at all. Every rule above holds for
//     a parser that refuses everything, so without a floor under the accepted
//     ones a step backwards stays green.
//
// Seed and rounds come from the environment, so a failure can be run again:
// NGC_FUZZ_SEED=123 NGC_FUZZ_ROUNDS=50000 npm test -- contracts/core-fuzz
const v = vectors("core.json");
const ui = vectors("uiproof.json");
const seed = Number(process.env.NGC_FUZZ_SEED || 20260923) >>> 0;
const rounds = Number(process.env.NGC_FUZZ_ROUNDS || 6000);

test(`fuzz: a document or core's own error, ${rounds} inputs from seed ${seed}`, async ({ shell }) => {
  test.setTimeout(240_000);
  const out = await shell.evaluate(async ([d, proofs, trust, seed, rounds]) => {
    const c = await import("/assets/js/contracts/core.js");
    const session = await import("/assets/js/contracts/session.js");
    const { MAX_SAFE_INTEGER } = await import("/assets/js/contracts/json.js");
    const { MAX_PARAMS } = await import("/assets/js/contracts/wire.js");
    const { b64decode, fromUtf8 } = await import("/assets/js/contracts/b64.js");

    let state = seed || 1;
    const next = () => {
      state ^= state << 13; state >>>= 0;
      state ^= state >> 17;
      state ^= state << 5; state >>>= 0;
      return state / 4294967296;
    };
    const upto = (n) => Math.floor(next() * n);
    const pick = (a) => a[upto(a.length)];

    // fits says whether a value is one the shape describes, element by
    // element. It is the second half of the criterion and deliberately
    // spelled out here, not taken from the module under test.
    const fits = (x, s) => {
      switch (s.k) {
        case "str": return typeof x === "string";
        case "bool": return typeof x === "boolean";
        case "int": return Number.isInteger(x) && Math.abs(x) <= MAX_SAFE_INTEGER;
        case "uint": return Number.isInteger(x) && x >= 0 && x <= MAX_SAFE_INTEGER;
        case "uint32": return Number.isInteger(x) && x >= 0 && x <= 4294967295;
        case "bytes": return x instanceof Uint8Array;
        case "raw": return x === null || x instanceof Uint8Array;
        case "array": return Array.isArray(x) && x.every((e) => fits(e, s.of));
        case "struct": {
          if (x === null) return s.ptr === true;
          if (typeof x !== "object") return false;
          const names = Object.keys(x);
          return names.length === Object.keys(s.m).length
            && names.every((n) => Object.hasOwn(s.m, n) && fits(x[n], s.m[n]));
        }
      }
      return false;
    };

    // The categories no text a person reads may hold: control, format,
    // surrogate, private use and unassigned, and every spacing character but
    // the ASCII space. Asked as the categories that are OUT, so the criterion
    // does not repeat the list of the ones that are in.
    const controlLike = /\p{C}/u, spacing = /\p{Z}/u;
    const printable = (s) => {
      for (const cp of s) {
        if (controlLike.test(cp)) return false;
        if (cp !== " " && spacing.test(cp)) return false;
      }
      return true;
    };
    // strings walks an answer and reports the first string in it that nobody
    // could put on a page.
    const unprintable = (x) => {
      if (typeof x === "string") return x === "" || printable(x) ? null : x;
      if (x === null || typeof x !== "object" || x instanceof Uint8Array) return null;
      for (const e of Array.isArray(x) ? x : Object.values(x)) {
        const bad = unprintable(e);
        if (bad !== null) return bad;
      }
      return null;
    };

    // ---- the generator ---------------------------------------------------
    // The values a member is swapped for: numbers on the edges of every limit
    // this table holds, ids and versions just inside and just outside their
    // grammars, and the shapes that are no value of that member at all.
    const edges = [0, 1, -1, 1.5, 4294967296, 9007199254740991, 9007199254740992,
      "", " ", "x", "ok", "info", "auto", "amd64", "critical", "local", "global",
      "ngc.desktop", "ngc-web", "system", "org.sorglos.terminal", "A.b", "a..b",
      "1.0.0", "01.0.0", "1.0", "admin", "Admin", "a".repeat(300), "\u0000x", "‮x",
      "\ud800", "x­", "AQID", "AQI=", "AAAAAAAAAAAAAAAAAAAAAA", "0123456789abcdef".repeat(2),
      "wss://a.b:1/tunnel", "https://a.b", "otpauth://totp/NGC:a?secret=JBSWY3DP",
      "08:00-09:00", "24:00-09:00", true, false, null, [], {}];
    // twist rewrites a document a member at a time: a value swapped for an
    // edge, a list made longer or shorter, a row repeated, a member added or
    // dropped. This is the shape of a document and not its numbers - the two
    // reach different rules, and a result table is almost all shape.
    const twist = (x, depth) => {
      if (depth > 3 || upto(6) === 0) return pick(edges);
      if (Array.isArray(x)) {
        const a = x.map((e) => (upto(3) === 0 ? twist(e, depth + 1) : e));
        switch (upto(10)) {
          case 0: return [...a, ...a];
          case 1: return a.slice(0, upto(a.length + 1));
          case 2: return [...a, pick(edges)];
          case 3: return a.length > 0 ? [...a, a[0]] : a; // one row twice
          default: return a;
        }
      }
      if (x !== null && typeof x === "object") {
        const o = {};
        for (const [k, e] of Object.entries(x)) o[k] = upto(5) === 0 ? twist(e, depth + 1) : e;
        if (upto(8) === 0) o[pick(["extra", "Id", "ID", "", "name"])] = pick(edges);
        if (upto(8) === 0) {
          const names = Object.keys(o);
          if (names.length > 0) delete o[pick(names)];
        }
        return o;
      }
      return pick(edges);
    };
    // A member no shape knows, pushed in at the text level: an object literal
    // would only move the prototype and leave the document unchanged.
    const inject = (s) => {
      if (upto(10) !== 0 || !s.startsWith("{")) return s;
      return '{"' + pick(["__proto__", "constructor", "toString"]) + '":'
        + JSON.stringify(pick(edges)) + (s === "{}" ? "" : ",") + s.slice(1);
    };
    const encoder = new TextEncoder();
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

    const found = [];
    const say = (why, what) => { if (found.length < 8) found.push({ why, input: String(what).slice(0, 200) }); };
    const ours = (e, name, what) => {
      // core's refusal is an Error and nothing else; an engine's TypeError or
      // RangeError is a state the parser did not mean to be in.
      if (e.constructor !== Error || !String(e.message).startsWith("core: ")) {
        say(`${name} threw ${e.constructor.name}: ${e.message}`, what);
      }
    };
    const count = { result: 0, notice: 0, proof: 0, refused: 0, params: 0, encoded: 0, verified: 0 };

    // ---- results ---------------------------------------------------------
    const keys = Object.keys(d).filter((k) => k !== "notice" && k !== "empty");
    for (let i = 0; i < rounds && found.length < 8; i++) {
      const key = pick(keys);
      const [app, name] = key.split("/");
      const action = c.lookupByKey(app, name);
      // A third of the rounds is the vector itself or a small twist of it;
      // the rest twist it further, and every fourth document is mutated as
      // bytes afterwards, which is where a document stops being JSON at all.
      const src = inject(JSON.stringify(upto(3) === 0 ? d[key] : twist(d[key], 0)));
      const b = i % 4 === 0 ? mutate(encoder.encode(src)) : encoder.encode(src);
      let r;
      try {
        r = c.parseResult(action, b);
      } catch (e) {
        ours(e, "parseResult", src);
        count.refused++;
        continue;
      }
      count.result++;
      const shape = c.RESULT_SHAPES[key];
      if (shape === undefined) { say("a result for a key of no shape", key); continue; }
      if (!fits(r, shape)) { say(`a ${key} that does not fit its shape`, src); continue; }
      const bad = unprintable(r);
      if (bad !== null) say(`a ${key} carrying text nobody can show: ${JSON.stringify(bad).slice(0, 60)}`, src);
    }
    // An action nobody wrote gets no result read for it at all.
    for (const a of [null, undefined, {}, { app: "x", service: "api", name: "me" },
      { app: "ngc.desktop", service: "api", name: "nosuch" }, 7, "ngc.desktop/me"]) {
      try {
        c.parseResult(a, encoder.encode("{}"));
        say("a result was read for an action of no table", JSON.stringify(a));
      } catch (e) {
        ours(e, "parseResult", JSON.stringify(a));
      }
    }

    // ---- notices ---------------------------------------------------------
    const levels = ["info", "warn", "error"];
    const oddLevels = ["", "INFO", "debug", "Info", 7];
    const texts = ["Alles gut", "x", "x".repeat(256)];
    const oddTexts = ["", "x".repeat(257), "a\u0000b", "a\nb", "‮"];
    const apps = ["", "org.sorglos.terminal", "a.b"];
    const oddApps = ["ngc.desktop", "terminal", "A.b", "a."];
    for (let i = 0; i < rounds && found.length < 8; i++) {
      // ONE thing at a time, as everywhere here: a notice written whole, then
      // one of its three members from the edges, in two documents out of three.
      const s = upto(3) === 0 ? -1 : upto(3);
      const src = inject(JSON.stringify({
        level: s === 0 ? pick(oddLevels) : pick(levels),
        text: s === 1 ? pick(oddTexts) : pick(texts),
        app: s === 2 ? pick(oddApps) : pick(apps),
      }));
      const b = i % 4 === 1 ? mutate(encoder.encode(src)) : encoder.encode(src);
      let n;
      try {
        n = c.parseNotice(b);
      } catch (e) {
        ours(e, "parseNotice", src);
        count.refused++;
        continue;
      }
      count.notice++;
      if (!levels.includes(n.level)) say(`a notice at level ${n.level}`, src);
      else if (n.text === "" || !printable(n.text) || encoder.encode(n.text).length > 256) {
        say("a notice whose text nobody can show", src);
      } else if (n.app !== "" && !(/^[a-z0-9]+(\.[a-z0-9-]+)+$/.test(n.app) && !n.app.startsWith("ngc."))) {
        say(`a notice about app ${n.app}`, src);
      }
    }

    // ---- UI proofs -------------------------------------------------------
    // The 23 documents of uiproof.json, read back as JSON and twisted the same
    // way. They are the only whole UI proofs there are: one holds two
    // envelopes, a certificate and a passkey proof, and nothing generated from
    // nothing would get past the first member.
    const proofDocs = proofs.map((p) => JSON.parse(fromUtf8(b64decode(p))));
    // The verified trust list these proofs belong to, where the engine can
    // read it: trust.parseList checks the list's own signatures, so an engine
    // without Ed25519 has no list and only the refusals below run there.
    let trustList = null;
    try {
      const t = await import("/assets/js/contracts/trust.js");
      trustList = await t.parseList(b64decode(trust));
    } catch {
      trustList = null;
    }
    for (let i = 0; i < Math.ceil(rounds / 4) && found.length < 8; i++) {
      const doc = pick(proofDocs);
      const src = inject(JSON.stringify(upto(3) === 0 ? doc : twist(doc, 0)));
      const b = i % 4 === 2 ? mutate(encoder.encode(src)) : encoder.encode(src);
      let p;
      try {
        p = c.parseUIProof(b);
      } catch (e) {
        ours(e, "parseUIProof", src.slice(0, 200));
        count.refused++;
        continue;
      }
      count.proof++;
      const app = typeof p.app === "string" && /^[a-z0-9]+(\.[a-z0-9-]+)+$/.test(p.app) && !p.app.startsWith("ngc.");
      const version = typeof p.version === "string" && /^(0|[1-9][0-9]{0,5})(\.(0|[1-9][0-9]{0,5})){2}$/.test(p.version);
      const store = typeof p.store === "string" && /^[a-z][a-z0-9-]{0,31}$/.test(p.store);
      if (!app || !version || !store) { say(`a UI proof for ${p.app} at ${p.version} of ${p.store}`, src.slice(0, 200)); continue; }
      // Exactly one of the two forms, never a mixture: a proof that carried
      // both would be one kind checked and the other one used.
      const fromStore = p.catalog !== null && p.task === null && p.client.length === 0
        && p.proof === null && p.store !== c.LOCAL_STORE;
      const uploaded = p.catalog === null && p.task !== null && p.proof !== null
        && p.client.length > 0 && p.client.length <= session.MAX_CERT && p.store === c.LOCAL_STORE;
      if (fromStore === uploaded) say("a UI proof that is both forms or neither", src.slice(0, 200));
      // Every envelope it carries is an envelope: a key id of 22 characters
      // and a signature of 64 bytes over a payload of at least one.
      for (const env of [p.catalog, p.task]) {
        if (env === null) continue;
        if (typeof env.kid !== "string" || env.kid.length !== 22 || !(env.sig instanceof Uint8Array)
          || env.sig.length !== 64 || !(env.payload instanceof Uint8Array) || env.payload.length < 1) {
          say("a UI proof carrying something that is no envelope", src.slice(0, 200));
        }
      }
      // And it is held against a trust list. Whatever comes back is a
      // SHA-256, and whatever goes wrong is core's own error - lists that are
      // no lists included.
      //
      // The real list of uiproof.json is among them, so the ANSWER side is
      // really reached and not only the refusal: without it every round here
      // would end in an exception and a verifier that never answers would keep
      // every rule. It needs Ed25519 in the engine's Web Crypto, which the
      // WebKit Playwright brings has not (measured; see core.spec.js), so
      // there the list is null and the floor below is not asked.
      if (count.proof <= 40) {
        for (const list of [null, {}, { users: [], stores: [] }, { stores: 7 }, trustList]) {
          if (list === null && trustList === null) continue;
          try {
            const hash = await c.verifyUIProof(p, p.app, p.version, list);
            if (!(hash instanceof Uint8Array) || hash.length !== 32) say("verifyUIProof answered no hash", "a proof");
            else count.verified++;
          } catch (e) {
            ours(e, "verifyUIProof", "a proof");
          }
        }
      }
    }

    // ---- parameters ------------------------------------------------------
    // Every action of the table, and rows nobody wrote, against objects built
    // out of the members the seven parameter kinds have between them.
    const values = [...edges, new Uint8Array(0), new Uint8Array(16), new Uint8Array(32),
      new Uint8Array(33), ["root"], ["root", "network"], ["root", "root"], ["x"],
      ["org.sorglos.terminal/use"], [""], 500, 501, "08:00-09:00"];
    const members = ["app", "version", "sha256", "ui", "store", "grants", "keepData", "target",
      "role", "rights", "code", "id", "limit", "kind", "user", "before", "mode", "window", "extra"];
    const rows = [...c.ACTIONS, null, undefined, {}, { app: "ngc.store", service: "controller", name: "activate" },
      { app: "x", service: "y", name: "z" }];
    for (let i = 0; i < Math.ceil(rounds / 2) && found.length < 8; i++) {
      const a = pick(rows);
      const p = {};
      const n = upto(6);
      for (let k = 0; k < n; k++) p[pick(members)] = pick(values);
      const params = upto(8) === 0 ? pick([null, undefined, 0, "", [], new Uint8Array(2)]) : p;
      let ok;
      try {
        ok = c.validateParams(a, params);
      } catch (e) {
        say(`validateParams threw ${e.constructor.name}: ${e.message}`, JSON.stringify(Object.keys(p)));
        continue;
      }
      if (typeof ok !== "boolean") { say(`validateParams answered ${typeof ok}`, String(ok)); continue; }
      if (ok) count.params++;
      let b;
      try {
        b = c.encodeParams(a, params);
      } catch (e) {
        if (e.constructor !== Error) say(`encodeParams threw ${e.constructor.name}: ${e.message}`, JSON.stringify(Object.keys(p)));
        // What it refuses to write, it must not call whole either.
        if (ok) say("encodeParams refused what validateParams called whole", JSON.stringify(Object.keys(p)));
        continue;
      }
      count.encoded++;
      if (!(b instanceof Uint8Array) || b.length === 0 || b.length > MAX_PARAMS || b[0] !== 0x7b) {
        say("encodeParams wrote something that is no parameter document", JSON.stringify(Object.keys(p)));
        continue;
      }
      if (!ok) say("encodeParams wrote what validateParams called broken", JSON.stringify(Object.keys(p)));
      // The same object twice writes the same bytes: these are the bytes a
      // session key signs, and two readings of one object may not differ.
      const again = c.encodeParams(a, params);
      if (again.length !== b.length || again.some((byte, k) => byte !== b[k])) {
        say("encodeParams wrote two different documents for one object", JSON.stringify(Object.keys(p)));
      }
    }

    return { found, count, ed25519: trustList !== null };
  }, [v, ui.cases.map((k) => k.proof), ui.trust.payload, seed, rounds]);

  // What was found comes first: the loop stops at the eighth finding, so a
  // fault early in the run leaves the counts below under their floors as well.
  expect(out.found).toEqual([]);
  if (out.found.length === 0) {
    // Measured over 6000 rounds of this seed, the same in both engines: 2694
    // results, 1354 notices, 462 UI proofs, 266 parameter documents and 8990
    // refusals, and 11 proofs that really verified on the engine that has
    // Ed25519. The floors are about a third of that, and they count in rounds
    // and not in a fixed number, because the rounds are a knob this file
    // advertises itself: a fixed count would turn every short run red without
    // anything being wrong.
    expect(out.count.result).toBeGreaterThan(Math.floor(rounds * 0.15));
    expect(out.count.notice).toBeGreaterThan(Math.floor(rounds * 0.10));
    expect(out.count.proof).toBeGreaterThan(Math.floor(rounds / 4 * 0.10));
    expect(out.count.refused).toBeGreaterThan(rounds);
    // And the parameters really were built and written, so the two rules
    // about them held against something.
    expect(out.count.params).toBeGreaterThan(80);
    expect(out.count.encoded).toBeGreaterThan(80);
    // A UI proof really came out true, and not only false: on an engine with
    // Ed25519 the real list of uiproof.json verifies the proofs it belongs to,
    // and a verifier that answered nothing would keep every rule above.
    if (out.ed25519) expect(out.count.verified).toBeGreaterThan(3);
  }
});
