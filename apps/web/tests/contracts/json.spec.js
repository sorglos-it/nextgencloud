import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// json.json holds one document with every kind of field (schema), its budget
// (size, items, totals) and the cases Go accepts and refuses. The shell must
// decide every one of them exactly as Go does.
const v = vectors("json.json");

test("every case of json.json is decided as Go decides it", async ({ shell }) => {
  const results = await shell.evaluate(async ([items, totals, size, cases]) => {
    const { decode, Budget, S, P, A, M, str, bool, int, uint32, bytes, raw } =
      await import("/assets/js/contracts/json.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    // The shape mirrors the Go struct json.json was made from, field for
    // field: u is a uint32, sub is a pointer (missing gives null, not an
    // empty object), and the elements of subs are that same struct by value.
    const subMembers = { x: int, list: A(int) };
    const shape = S({
      n: int, u: uint32, s: str, t: bool, b: bytes,
      list: A(int), map: M(int),
      sub: P(subMembers),
      subs: A(S(subMembers)),
      raw: raw,
    });
    const budget = new Budget({ size, items, totals });
    return cases.map((c) => {
      try {
        decode(b64decode(c.doc), budget, shape);
        return true;
      } catch {
        return false;
      }
    });
  }, [v.items, v.totals, v.size, v.cases]);

  const wrong = v.cases.filter((c, i) => c.valid !== results[i]).map((c) => c.name);
  expect(wrong).toEqual([]);
});

// Deciding accepted or refused leaves half the interface unproven. This one
// holds the decoded value against the value Go decodes, field for field: the
// zero of every shape, a pointer struct that stays null when its member is
// missing, the bytes of a base64url field, the members of a map and the raw
// slice byte for byte.
test("every field decodes to the value Go decodes", async ({ shell }) => {
  const doc = v.cases.find((c) => c.name === "every field").doc;
  const out = await shell.evaluate(async ([items, totals, size, full]) => {
    const { decode, Budget, S, P, A, M, str, bool, int, uint32, bytes, raw } =
      await import("/assets/js/contracts/json.js");
    const { b64decode, utf8, fromUtf8 } = await import("/assets/js/contracts/b64.js");
    const subMembers = { x: int, list: A(int) };
    const shape = S({
      n: int, u: uint32, s: str, t: bool, b: bytes,
      list: A(int), map: M(int),
      sub: P(subMembers),
      subs: A(S(subMembers)),
      raw: raw,
    });
    const budget = new Budget({ size, items, totals });
    // A map is shown by its members, not as itself: it has no prototype, and
    // that is part of what is checked here.
    const shown = (d) => ({
      n: d.n, u: d.u, s: d.s, t: d.t,
      b: Array.from(d.b),
      list: d.list,
      mapKeys: Object.keys(d.map),
      mapValues: Object.values(d.map),
      mapHasNoPrototype: Object.getPrototypeOf(d.map) === null,
      sub: d.sub,
      subs: d.subs,
      raw: d.raw === null ? null : fromUtf8(d.raw),
    });
    return {
      full: shown(decode(b64decode(full), budget, shape)),
      empty: shown(decode(utf8("{}"), budget, shape)),
    };
  }, [v.items, v.totals, v.size, doc]);

  expect(out.full).toEqual({
    n: -5, u: 7, s: "ä", t: true,
    b: [1, 2, 3],
    list: [1, 2, 3, 4],
    mapKeys: ["a", "b"],
    mapValues: [1, 2],
    mapHasNoPrototype: true,
    sub: { x: 1, list: [1] },
    // x is missing in every element, so it is the zero of its shape.
    subs: [{ x: 0, list: [1, 2] }, { x: 0, list: [3, 4] }, { x: 0, list: [5, 6] }],
    raw: '{"k":[1,{"z":"y"}],"K":2}',
  });
  expect(out.empty).toEqual({
    n: 0, u: 0, s: "", t: false,
    b: [],
    list: [],
    mapKeys: [],
    mapValues: [],
    mapHasNoPrototype: true,
    // A pointer struct that is missing is null, a raw value that is missing
    // is null; every other shape has a zero value.
    sub: null,
    subs: [],
    raw: null,
  });
});

// A raw value must survive the buffer it was read from: E2EPrologue hashes
// these bytes and signatures cover them, so a view into the input would let
// whoever reuses the buffer change a value that was already checked.
test("a raw value keeps its own bytes", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { decode, Budget, S, raw } = await import("/assets/js/contracts/json.js");
    const { utf8, fromUtf8 } = await import("/assets/js/contracts/b64.js");
    const input = utf8('{"raw":{"a":1}}');
    const d = decode(input, new Budget({ size: 1024, items: { raw: -1 } }), S({ raw: raw }));
    const before = fromUtf8(d.raw);
    input.fill(0x20); // the caller reuses its buffer
    return { before, after: fromUtf8(d.raw), sharesBuffer: d.raw.buffer === input.buffer };
  });
  expect(out).toEqual({ before: '{"a":1}', after: '{"a":1}', sharesBuffer: false });
});

// A budget that is wrong in itself is a mistake in the contract, not in the
// document: Go checks it before anything else and then refuses every
// document, whatever it holds (enc.go, errLimit and maxTotals). A missing
// size closes the gate too — Go's Budget{} has Size 0 and takes nothing.
test("a budget Go refuses refuses every document", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { decode, Budget, BadJSON, S, A, M, int, raw } =
      await import("/assets/js/contracts/json.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    const shape = S({ list: A(int), map: M(int), raw: raw });
    // Everything is inside the try: a budget may already be refused where it
    // is built, and that counts as refused just the same.
    const run = (budget, text) => {
      try {
        decode(utf8(text), new Budget(budget), shape);
        return "accepted";
      } catch (e) {
        return e instanceof BadJSON ? "refused" : "threw " + e.constructor.name;
      }
    };
    const totals = (n) => {
      const items = {}, t = {};
      for (let i = 0; i < n; i++) { items["p" + i] = 4; t["p" + i] = 4; }
      return { size: 1024, items, totals: t };
    };
    return {
      itemsNegative: run({ size: 1024, items: { list: -5 } }, "{}"),
      itemsNegativeElsewhere: run({ size: 1024, items: { list: 4, map: -5 } }, "{}"),
      itemsNotAnInteger: run({ size: 1024, items: { list: 2.5 } }, "{}"),
      totalsNegative: run({ size: 1024, items: { list: 4 }, totals: { list: -1 } }, "{}"),
      totalsUnlisted: run({ size: 1024, items: { list: 4 }, totals: { map: 2 } }, "{}"),
      totalsOnRaw: run({ size: 1024, items: { raw: -1 }, totals: { raw: 2 } }, "{}"),
      nineTotals: run(totals(9), "{}"),
      eightTotals: run(totals(8), "{}"),
      sizeMissing: run({ items: {} }, "{}"),
      sizeNegative: run({ size: -1, items: {} }, "{}"),
      sizeNotAnInteger: run({ size: 1.5, items: {} }, "{}"),
      sizeZero: run({ size: 0, items: {} }, "{}"),
      sizeZeroEmptyInput: run({ size: 0, items: {} }, ""),
      raw: run({ size: 1024, items: { raw: -1 } }, '{"raw":{"a":1}}'),
    };
  });
  expect(out).toEqual({
    itemsNegative: "refused",
    itemsNegativeElsewhere: "refused",
    itemsNotAnInteger: "refused",
    totalsNegative: "refused",
    totalsUnlisted: "refused",
    totalsOnRaw: "refused",
    nineTotals: "refused",
    // Eight is the most Go allows, and Raw is the one negative limit it takes.
    eightTotals: "accepted",
    sizeMissing: "refused",
    sizeNegative: "refused",
    sizeNotAnInteger: "refused",
    sizeZero: "refused",
    sizeZeroEmptyInput: "refused",
    raw: "accepted",
  });
});

// In Go the budget and the contract never meet: the first pass knows only the
// path, the second only the Go type. Where the two disagree the shell has to
// answer as both passes together do. Every answer below was measured against
// shared/enc, not argued.
test("the budget goes by the path, the contract by the shape", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { decode, Budget, BadJSON, S, P, A, M, int, raw, MAX_MEMBERS } =
      await import("/assets/js/contracts/json.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    const run = (items, shape, text, size = 4096) => {
      try {
        decode(utf8(text), new Budget({ size, items }), shape);
        return "accepted";
      } catch (e) {
        return e instanceof BadJSON ? "refused" : "threw " + e.constructor.name;
      }
    };
    const sub = { x: int, list: A(int) };
    const doc = () => S({ n: int, map: M(int), list: A(int), sub: P(sub), raw: raw });
    const members = (n) => {
      const out = [];
      for (let i = 0; i < n; i++) out.push('"m' + i + '":' + i);
      return out.join(",");
    };
    return {
      // A struct path the budget lists is counted like a map, and its members
      // are decoded by the shape all the same.
      structPathListed: run({ "": 5 }, doc(), "{}"),
      structPathListedOneMember: run({ "": 5 }, doc(), '{"n":1}'),
      structPathListedOverLimit: run({ "": 1 }, doc(), '{"n":1,"map":{}}'),
      structPathListedUnknownMember: run({ "": 5 }, doc(), '{"nope":1}'),
      // Because it is counted like a map, its members' path is "*".
      listedSubInnerAtStar: run({ sub: 5, "sub/*": 4 }, doc(), '{"sub":{"list":[1,2]}}'),
      listedSubInnerAtName: run({ sub: 5, "sub/list": 4 }, doc(), '{"sub":{"list":[1,2]}}'),
      // A map whose path is missing is a struct object: at most MAX_MEMBERS
      // members, and their names are checked.
      mapPathMissing: run({}, doc(), '{"map":{"a":1}}'),
      mapPathMissingSlash: run({}, doc(), '{"map":{"a/b":1}}'),
      mapPathMissingStar: run({}, doc(), '{"map":{"*":1}}'),
      mapPathMissingTooMany: run({}, doc(), '{"map":{' + members(MAX_MEMBERS + 1) + "}}"),
      mapPathMissingAtLimit: run({}, doc(), '{"map":{' + members(MAX_MEMBERS) + "}}"),
      mapListedSlash: run({ map: 4 }, doc(), '{"map":{"a/b":1}}'),
      // Raw is about counting, not about the shape: inside a raw path nothing
      // is counted and no name is checked, and the shape still decides what
      // the value becomes.
      rawListed: run({ raw: -1 }, doc(), '{"raw":{"a":[1,2,3]}}'),
      rawShapeUnlisted: run({}, doc(), '{"raw":{"a":[1,2,3]}}'),
      rawShapeUnlistedNoArray: run({}, doc(), '{"raw":{"a":1}}'),
      arrayShapeAtRawPath: run({ list: -1 }, doc(), '{"list":[1,2,3]}'),
      structShapeAtRawPath: run({ sub: -1 }, doc(), '{"sub":{"x":1,"list":[1,2]}}'),
      structShapeAtRawPathUnknown: run({ sub: -1 }, doc(), '{"sub":{"nope":1}}'),
      scalarAtRawPath: run({ n: -1 }, doc(), '{"n":5}'),
      mapShapeAtRawPathTooMany: run({ map: -1 }, doc(), '{"map":{' + members(MAX_MEMBERS + 1) + "}}"),
    };
  });
  expect(out).toEqual({
    structPathListed: "accepted",
    structPathListedOneMember: "accepted",
    structPathListedOverLimit: "refused",
    structPathListedUnknownMember: "refused",
    listedSubInnerAtStar: "accepted",
    listedSubInnerAtName: "refused",
    mapPathMissing: "accepted",
    mapPathMissingSlash: "refused",
    mapPathMissingStar: "refused",
    mapPathMissingTooMany: "refused",
    mapPathMissingAtLimit: "accepted",
    mapListedSlash: "accepted",
    rawListed: "accepted",
    rawShapeUnlisted: "refused",
    rawShapeUnlistedNoArray: "accepted",
    arrayShapeAtRawPath: "accepted",
    structShapeAtRawPath: "accepted",
    structShapeAtRawPathUnknown: "refused",
    scalarAtRawPath: "accepted",
    mapShapeAtRawPathTooMany: "accepted",
  });
});

// A map takes its member names from the document. On an ordinary object
// "__proto__" is not a member but the prototype: the value would vanish and
// the prototype of the result would be whatever the document says.
test("__proto__ is a map member like any other", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { decode, Budget, S, M, int } = await import("/assets/js/contracts/json.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    const numbers = decode(
      utf8('{"map":{"__proto__":5,"a":1}}'),
      new Budget({ size: 1024, items: { map: 2 } }),
      S({ map: M(int) }),
    );
    const nested = decode(
      utf8('{"map":{"__proto__":{"x":7}}}'),
      new Budget({ size: 1024, items: { map: 2 } }),
      S({ map: M(S({ x: int })) }),
    );
    return {
      keys: Object.keys(numbers.map),
      value: numbers.map["__proto__"],
      nestedKeys: Object.keys(nested.map),
      nestedValue: nested.map["__proto__"],
      prototypeLeft: Object.getPrototypeOf(nested.map) === null,
    };
  });
  expect(out).toEqual({
    keys: ["__proto__", "a"],
    value: 5,
    nestedKeys: ["__proto__"],
    nestedValue: { x: 7 },
    prototypeLeft: true,
  });
});

// The number is read byte by byte, never by spreading the bytes into the
// arguments of a call: how many arguments a call takes is the engine's
// business, differs between V8, JavaScriptCore and SpiderMonkey, and a
// RangeError from there is no answer a contract can give. Today the size of
// every budget keeps such a number away; the next contract with a larger one
// would meet it.
test("a number of 300000 digits is refused, not a RangeError", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { decode, Budget, BadJSON, S, int } = await import("/assets/js/contracts/json.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    const budget = new Budget({ size: 400000, items: {} });
    const answer = (text) => {
      try {
        decode(utf8(text), budget, S({ n: int }));
        return "accepted";
      } catch (e) {
        return e instanceof BadJSON ? "refused" : "threw " + e.constructor.name;
      }
    };
    return {
      long: answer('{"n":' + "9".repeat(300000) + "}"),
      // The same length as a fraction, where the digits are read as well.
      longFraction: answer('{"n":0.' + "0".repeat(300000) + "1}"),
      short: answer('{"n":7}'),
    };
  });
  expect(out).toEqual({ long: "refused", longFraction: "refused", short: "accepted" });
});

test("base64url is strict", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { b64encode, b64decode, encodedLen } = await import("/assets/js/contracts/b64.js");
    const ok = (s) => { try { b64decode(s); return true; } catch { return false; } };
    return {
      roundTrip: b64encode(new Uint8Array([1, 2, 3])) === "AQID" && b64decode("AQID").length === 3,
      len: encodedLen(16) === 22 && encodedLen(32) === 43,
      padding: ok("AQI="), lineFeed: ok("AQ\nID"), standard: ok("+/=="),
      trailingBits: ok("AQJ"), lonely: ok("A"), empty: b64decode("").length === 0,
    };
  });
  expect(out).toEqual({
    roundTrip: true, len: true, padding: false, lineFeed: false, standard: false,
    trailingBits: false, lonely: false, empty: true,
  });
});

// The other four functions of b64.js are part of the interface later tasks
// build on. equal compares signatures and message codes, so an equal that
// says true too easily is a hole nobody sees: it is checked on values that
// differ in one byte at the same length, at a different length, and on
// arguments that are no bytes at all.
test("the byte helpers of b64.js hold", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { utf8, fromUtf8, concat, equal, BadInput } = await import("/assets/js/contracts/b64.js");
    const threw = (f) => { try { f(); return "no"; } catch (e) { return e instanceof BadInput ? "BadInput" : e.constructor.name; } };
    const u = (...n) => new Uint8Array(n);
    return {
      utf8Ascii: Array.from(utf8("ab")),
      utf8Wide: Array.from(utf8("ä€")),
      utf8Empty: Array.from(utf8("")),
      utf8NotAString: threw(() => utf8(5)),
      fromUtf8RoundTrip: fromUtf8(utf8("ä€ 1")) === "ä€ 1",
      fromUtf8Broken: threw(() => fromUtf8(u(0xff, 0xfe))),
      fromUtf8NotBytes: threw(() => fromUtf8("ab")),
      concatNone: Array.from(concat()),
      concatOne: Array.from(concat(u(1, 2))),
      concatThree: Array.from(concat(u(1), u(), u(2, 3))),
      concatNotBytes: threw(() => concat(u(1), "ab")),
      equalSame: equal(u(1, 2, 3), u(1, 2, 3)),
      equalEmpty: equal(u(), u()),
      equalOneByteDifferent: equal(u(1, 2, 3), u(1, 2, 4)),
      equalFirstByteDifferent: equal(u(1, 2, 3), u(9, 2, 3)),
      equalShorter: equal(u(1, 2, 3), u(1, 2)),
      equalLonger: equal(u(1, 2), u(1, 2, 3)),
      equalMissing: equal(u(1), undefined),
      // Two strings have a length and index like bytes, but "a" ^ "b" is 0:
      // a comparison that took them would answer true for anything.
      equalStrings: equal("abc", "abd"),
      equalArrays: equal([1, 2], [1, 3]),
    };
  });
  expect(out).toEqual({
    utf8Ascii: [97, 98],
    utf8Wide: [0xc3, 0xa4, 0xe2, 0x82, 0xac],
    utf8Empty: [],
    utf8NotAString: "BadInput",
    fromUtf8RoundTrip: true,
    fromUtf8Broken: "BadInput",
    fromUtf8NotBytes: "BadInput",
    concatNone: [],
    concatOne: [1, 2],
    concatThree: [1, 2, 3],
    concatNotBytes: "BadInput",
    equalSame: true,
    equalEmpty: true,
    equalOneByteDifferent: false,
    equalFirstByteDifferent: false,
    equalShorter: false,
    equalLonger: false,
    equalMissing: false,
    equalStrings: false,
    equalArrays: false,
  });
});

// json.json has no case for this, and a differential run against the compiled
// shared/enc found the shell one level stricter than Go: MAX_DEPTH counts the
// arrays and objects nested in each other, and a number or a string inside
// the innermost one is not a level of its own. The top-level object is level
// 1 and the raw value level 2, so 31 containers below it still fit.
test("depth counts containers, not the values inside them", async ({ shell }) => {
  const docs = [];
  for (let d = 30; d <= 33; d++) {
    docs.push(["arrays, empty leaf, " + d, '{"raw":' + "[".repeat(d) + "]".repeat(d) + "}", d <= 31]);
    docs.push(["arrays, number leaf, " + d, '{"raw":' + "[".repeat(d) + "1" + "]".repeat(d) + "}", d <= 31]);
    docs.push(["objects, number leaf, " + d, '{"raw":' + '{"a":'.repeat(d) + "1" + "}".repeat(d) + "}", d <= 31]);
    docs.push(["objects, string leaf, " + d, '{"raw":' + '{"a":'.repeat(d) + '"x"' + "}".repeat(d) + "}", d <= 31]);
  }
  const results = await shell.evaluate(async (texts) => {
    const { decode, Budget, S, raw } = await import("/assets/js/contracts/json.js");
    const budget = new Budget({ size: 1024, items: { raw: -1 }, totals: {} });
    const shape = S({ raw: raw });
    return texts.map((t) => {
      try {
        decode(new TextEncoder().encode(t), budget, shape);
        return true;
      } catch {
        return false;
      }
    });
  }, docs.map(([, text]) => text));

  const wrong = docs.filter(([, , want], i) => want !== results[i]).map(([name]) => name);
  expect(wrong).toEqual([]);
});

// A shape is a constant of the contract that holds it, and every contract
// keeps its shapes in module variables that anyone on the page can reach.
// Whoever can change one decides what a parser reads: sign.envelopeShape with
// one member more and parseEnvelope takes envelopes the contract does not
// describe. The lock sits here, where a shape is built, so no contract has to
// set it for itself.
test("a shape cannot be changed from outside", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const j = await import("/assets/js/contracts/json.js");
    const shapes = [
      ["str", j.str], ["bool", j.bool], ["int", j.int], ["uint", j.uint],
      ["uint32", j.uint32], ["uint64", j.uint64], ["bytes", j.bytes], ["raw", j.raw],
      ["S", j.S({ a: j.str })], ["P", j.P({ a: j.str })],
      ["A", j.A(j.str)], ["M", j.M(j.str)],
    ];
    const opened = [];
    for (const [name, shape] of shapes) {
      for (const [what, change] of [
        ["the kind", (x) => { x.k = "map"; }],
        ["a member of its own", (x) => { x.extra = 1; }],
        ["a member of the struct", (x) => { if (x.m) x.m.extra = j.str; }],
        ["what an array or map holds", (x) => { if (x.of) x.of = j.raw; }],
      ]) {
        const before = JSON.stringify(shape);
        try {
          change(shape);
        } catch {
          // Every module is strict, so a frozen shape throws.
          continue;
        }
        if (JSON.stringify(shape) !== before) opened.push(name + ": " + what);
      }
    }
    // The members a caller hands over stay his own: the shape keeps a copy,
    // so a change to that object afterwards cannot reach the shape either.
    const members = { a: j.str };
    const shape = j.S(members);
    members.b = j.str;
    return { opened, leaked: Object.hasOwn(shape.m, "b") };
  });
  expect(out.opened).toEqual([]);
  expect(out.leaked).toBe(false);
});
