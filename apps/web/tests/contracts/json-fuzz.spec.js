import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// The fuzz target for the JSON parser of the shell (CLAUDE.md asks every
// parser for one). It has no second implementation to compare against — a Go
// parser translated into JavaScript would be a second truth beside shared/enc
// — so it holds the parser against what must be true for every input at all:
//
//   - decode answers with a value or throws BadJSON, never anything else. A
//     RangeError or a TypeError from the engine is no answer a contract can
//     give, and it is how the engines differ from one another.
//   - a value it answers with fits the shape it was asked for, down to every
//     element: nothing from outside may reach a caller in a form the contract
//     does not describe.
//   - and enough of the inputs are accepted at all. Both rules above hold
//     for a parser that refuses everything, so without a floor under the
//     accepted ones a step backwards stays green.
//
// Seed and rounds come from the environment, so a failure can be run again:
// NGC_FUZZ_SEED=123 NGC_FUZZ_ROUNDS=50000 npm test -- contracts/json-fuzz
const v = vectors("json.json");
const seed = Number(process.env.NGC_FUZZ_SEED || 20260923) >>> 0;
const rounds = Number(process.env.NGC_FUZZ_ROUNDS || 6000);

test(`fuzz: value or BadJSON, ${rounds} inputs from seed ${seed}`, async ({ shell }) => {
  const { found, accepted } = await shell.evaluate(async ([items, totals, size, docs, seed, rounds]) => {
    const { decode, Budget, BadJSON, S, P, A, M, str, bool, int, uint32, bytes, raw,
      MAX_SAFE_INTEGER } = await import("/assets/js/contracts/json.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const subMembers = { x: int, list: A(int) };
    const shape = S({
      n: int, u: uint32, s: str, t: bool, b: bytes,
      list: A(int), map: M(int),
      sub: P(subMembers),
      subs: A(S(subMembers)),
      raw: raw,
    });
    const budget = new Budget({ size, items, totals });

    // fits says whether a value is one the shape describes. It is the second
    // half of the criterion and deliberately spelled out here, not taken from
    // the module under test.
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
        case "map":
          return x !== null && typeof x === "object" &&
            // A map takes its names from the document, so it may never carry
            // a prototype a document could reach.
            Object.getPrototypeOf(x) === null &&
            Object.values(x).every((e) => fits(e, s.of));
        case "struct": {
          if (x === null) return s.ptr === true;
          if (typeof x !== "object") return false;
          const names = Object.keys(x);
          return names.length === Object.keys(s.m).length &&
            names.every((n) => Object.hasOwn(s.m, n) && fits(x[n], s.m[n]));
        }
      }
      return false;
    };

    // A small generator: names and scalars that sit on the edges of the
    // rules, put together into documents, plus byte mutations of the 54 cases
    // of json.json, which are the documents that get furthest into the parser.
    let state = seed || 1;
    const next = () => {
      state ^= state << 13; state >>>= 0;
      state ^= state >> 17;
      state ^= state << 5; state >>>= 0;
      return state / 4294967296;
    };
    const upto = (n) => Math.floor(next() * n);
    const pick = (a) => a[upto(a.length)];
    // Member names as they stand in the document, escapes and all: "n"
    // and "n" are the same name, and a name may hold anything a string may.
    const names = ['"n"', '"u"', '"s"', '"t"', '"b"', '"list"', '"map"', '"sub"',
      '"subs"', '"raw"', '"x"', '"a"', '"*"', '"a/b"', '""', '"N"', '"Raw"',
      '"__proto__"', '"constructor"', '"\\u006e"', '"\\u0041"', '"ä"',
      '"' + "x".repeat(64) + '"', '"' + "x".repeat(65) + '"'];
    const scalars = ["0", "-0", "1", "-5", "7", "9007199254740991", "9007199254740992",
      "-9007199254740991", "1e3", "1E+3", "0.5", "9007199254740991.0", "9007199254740991.4",
      '"ä"', '"AQID"', '"AQI="', '""', '"AQ\\nID"', "true", "false", "null", "-", "01",
      "1.", "1e", '"\\u0041"', '"\\ud800"', '"\\ud83d\\ude00"', '"\\x"', "NaN", "+1", ".5"];
    const value = (d) => {
      if (d <= 0 || next() < 0.55) return pick(scalars);
      if (next() < 0.5) {
        const n = upto(5);
        const parts = [];
        for (let i = 0; i < n; i++) parts.push(value(d - 1));
        return "[" + parts.join(",") + "]";
      }
      const n = upto(5);
      const parts = [];
      for (let i = 0; i < n; i++) parts.push(pick(names) + ":" + value(d - 1));
      return "{" + parts.join(",") + "}";
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

    const corpus = docs.map((d) => b64decode(d));
    const found = [];
    let accepted = 0;
    const show = (b) => {
      let s = "";
      for (const byte of b.subarray(0, 120)) {
        s += byte >= 0x20 && byte < 0x7f ? String.fromCharCode(byte) : "\\x" + byte.toString(16).padStart(2, "0");
      }
      return s + (b.length > 120 ? "…" : "");
    };
    for (let i = 0; i < rounds && found.length < 8; i++) {
      let input;
      if (i % 2 === 0) input = mutate(pick(corpus));
      else input = encoder.encode(value(1 + upto(4)));
      try {
        const out = decode(input, budget, shape);
        accepted++;
        if (!fits(out, shape)) found.push({ input: show(input), why: "the value does not fit the shape" });
      } catch (e) {
        if (!(e instanceof BadJSON)) found.push({ input: show(input), why: "threw " + e.constructor.name + ": " + e.message });
      }
    }
    return { found, accepted };
  }, [v.items, v.totals, v.size, v.cases.map((c) => c.doc), seed, rounds]);

  expect(found).toEqual([]);
  // 220 of the 6000 inputs of this seed reach a value; the floor is well
  // under that, and only a parser that has stopped accepting falls through it.
  expect(accepted).toBeGreaterThan(50);
});
