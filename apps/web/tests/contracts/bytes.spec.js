import { test, expect } from "../fixtures/page.js";

// The ninth case of one mistake, caught by a rule instead of by hand.
//
// Go is handed a []byte, whose length is the slice's own: two looks give one
// number, and the compiler answers for it. Here every byte string comes out of
// an object the caller owns, and a subclass of Uint8Array may define length.
// A function that asks twice - once to decide, once to work - then decides
// about something it never worked on. Eight times that was found one at a
// time and fixed one at a time, the eighth in task.js the round before this
// one. This test is the ninth, and it is not a case: it walks every export of
// every contract, hands each of them a byte array that counts how often it is
// asked for its size, and holds every one of them at one asking.
//
// One asking, not none: the answer comes from b64.viewOf, which asks
// byteLength once and builds the view every later reader sees. A function
// that never asks at all is as tight - b64.fromUtf8 hands its bytes to a
// TextDecoder, which takes the size out of the internal slot, and so asks
// nothing and is not in the list below at all - so the rule is "at most once",
// and what the list holds is the ones that do ask.
//
// length and byteLength count together. They are the same question, and after
// viewOf it is byteLength that carries it; counting only one of the two would
// call an entry tight that asks the other.
//
// What this holds and what it does not: the arguments of a function, and not
// the members of an object it is handed. session.encodeClientCert takes a
// certificate whose nonce is the caller's bytes, and that one is held by
// validateClientCert's own rule - every member read once into a local - which
// each of those functions writes out where it stands. A probe cannot be
// handed to all seven members at once without counting their askings
// together, so this test names the property it can decide and leaves the
// other one where its reason is.
//
// Measured against the tree as it stood before this round: of these entries,
// 20 asked more than once. Eleven through json.decode, which measured
// the budget against the first asking and then let the parser ask again at
// every step - a document of 70027 bytes came out of sign.parseEnvelope
// through a budget of 2390, and 5 MB, 40 MB and 70027 bytes walked past the
// bounds of session.parseLoginResult, session.parseTrustChain and
// session.parseTrustHint. Six through b64.concat, which sized the output
// from one asking and stepped the cursor by another: measured, concat(a, b)
// over a 32-byte a whose second answer was 0 wrote b over a's first byte, and
// sign.message signed that. One through b64.b64encode, whose loop bound was
// asked once per three bytes: measured, 32 real bytes encoded to the empty
// string. Two through b64.equal, whose loop bound was a second asking of the
// first argument: measured, true for two 32-byte arrays that differ in every
// byte. Four places now ask through b64.viewOf instead, and that closed all
// twenty.

// The modules this walks. The list is pinned, and the crawl below holds it
// down: whatever a contract imports has to stand here, so a module that is
// wired into the tree and forgotten here turns this red instead of walking
// through unseen. A module nobody imports yet has no caller either.
const MODULES = ["b64", "json", "noise", "proof", "session", "sign", "task", "text", "trust", "url", "wire"];

// Every entry: name#argument, of the ones the probe reaches. A new one, or
// one that disappears, is a thing to look at, so the list and its length are
// both part of what is asserted. Whoever adds an entry adds a line here, and
// whoever reads this sees what the tree does with caller bytes in one place.
//
// Six more are entries in an engine whose crypto.subtle.digest asks its
// argument for the size instead of taking it out of the internal slot -
// noise.e2ePrologue#4 and #5, session.certHash#0, sign.keyID#0,
// sign.keyIDAsync#0 and trust.tlsPin#0, measured under node 24. Chromium and
// webkit ask none of the six, so there they are tighter still, and the rule
// holds either way: measured under node, all six are at one asking. They are
// not in the list because the list says what the engine running the test
// reached, and both browsers reach the same 38.
const ENTRIES = [
  "b64.b64encode#0", "b64.concat#0", "b64.equal#0", "b64.equal#1", "b64.viewOf#0",
  "json.decode#0",
  "proof.challenge#1", "proof.privateFromSeed#0", "proof.publicFromSeed#0",
  "session.offerMAC#1", "session.parseLoginChallenge#0",
  "session.parseLoginResult#0", "session.parseSetupOffer#0", "session.parseTrustChain#0",
  "session.parseTrustHint#0", "session.setupMAC#1", "session.verifyAPI#2",
  "session.verifyApproval#0",
  "sign.equal#0", "sign.equal#1", "sign.message#1",
  "sign.parseEnvelope#0", "sign.verifyRaw#0", "sign.verifyRaw#1",
  "task.parseTask#0", "task.signTask#1",
  "trust.open#1", "trust.parseList#0", "trust.parseSigned#0",
  "wire.decodeFrame#0", "wire.e2e#1", "wire.grant#1", "wire.parseClose#0",
  "wire.parseControl#0", "wire.parseE2E#0", "wire.parseGrant#0", "wire.parseOpen#0",
  "wire.validParams#0",
];

// How many functions the eleven modules export. The probe reaches a function
// only if the arguments beside the bytes let it get that far, so a new
// function it cannot reach would be an entry nobody sees. This number needs
// no reaching: it comes from the module namespaces themselves, so a new
// export turns this red until somebody has said whether it takes caller
// bytes. Whoever adds one changes one number here.
const EXPORTS = 125;

test("every entry that takes caller bytes asks for their size once", async ({ shell }) => {
  const got = await shell.evaluate(async (names) => {
    const base = "/assets/js/contracts/";
    const mods = {};
    for (const n of names) mods[n] = await import(base + n + ".js");

    // What the contracts import of each other has to stand in the list above.
    const known = new Set(names);
    const unlisted = new Set();
    for (const n of names) {
      const src = await (await fetch(base + n + ".js")).text();
      // Every "./x.js" in the source, not only the ones after `from`: a
      // dynamic import counts too. A mention inside a comment would count as
      // well, and that is the safe direction - it turns this red and somebody
      // looks.
      for (const m of src.matchAll(/"\.\/([A-Za-z0-9_-]+)\.js"/g)) {
        if (!known.has(m[1])) unlisted.add(m[1]);
      }
    }

    // The probe: bytes that count every asking and answer the truth every
    // time. A lie is not needed here - what is measured is how often the
    // question is put, and a function that puts it once can be lied to only
    // once, which is the whole property.
    const probe = () => {
      let c = 0;
      class Probe extends Uint8Array {
        get length() { c++; return super.length; }
        get byteLength() { c++; return super.byteLength; }
      }
      const a = new Probe(2);
      // "{}" - a document every parser gets far enough into to ask its size.
      new Uint8Array(a.buffer).set([0x7b, 0x7d]);
      return { a, reads: () => c };
    };

    // The other arguments. A function that throws before it ever looks at the
    // bytes is no entry for that call, so the bytes have to be reached: two
    // ways, and what either one reaches counts. WIDE puts one value in every
    // other place at once and covers the functions whose neighbours are all
    // of a kind; NARROW is run as every combination and covers the ones that
    // want two different kinds at once - json.decode wants a Budget and a
    // shape, and no single value is both.
    const BUDGET = new mods.json.Budget({ size: 1 << 20 });
    const WIDE = [undefined, null, true, 0, 1, 1024, 16384, "", "x", {}, [],
      new Uint8Array(0), new Uint8Array(2), new Uint8Array(32), BUDGET, mods.json.raw];
    const NARROW = [undefined, {}, 1024, new Uint8Array(2), BUDGET, mods.json.raw];

    const found = [];
    let calls = 0, exports = 0;
    for (const n of names) {
      for (const [name, f] of Object.entries(mods[n])) {
        if (typeof f !== "function") continue;
        exports++;
        // A function that declares no argument may still take one: concat is
        // written with a rest argument and f.length is 0.
        const arity = Math.max(f.length, 1);
        for (let pos = 0; pos < arity; pos++) {
          let reads = 0;
          const attempt = async (args, p) => {
            calls++;
            try {
              const r = f(...args);
              // Everything that fails, fails silently: what is measured is
              // the asking, not the answer.
              if (r && typeof r.then === "function") { try { await r; } catch { /* no answer wanted */ } }
            } catch { /* no answer wanted */ }
            reads = Math.max(reads, p.reads());
          };
          for (const fill of WIDE) {
            const p = probe();
            const args = new Array(arity).fill(fill);
            args[pos] = p.a;
            await attempt(args, p);
          }
          // The combinations grow with the arity, so they stop at four
          // arguments: beyond that WIDE alone decides, and the two functions
          // it leaves - noise.e2ePrologue and proof.verify - reach their bytes
          // through it. Measured on webkit: 885 ms with the stop, 17 s without
          // it, and the same 38 entries either way.
          const total = arity <= 4 ? Math.pow(NARROW.length, arity - 1) : 0;
          for (let k = 0; k < total; k++) {
            const p = probe();
            const args = [];
            let q = k;
            for (let j = 0; j < arity; j++) {
              if (j === pos) { args.push(p.a); continue; }
              args.push(NARROW[q % NARROW.length]);
              q = Math.floor(q / NARROW.length);
            }
            await attempt(args, p);
          }
          if (reads > 0) found.push([`${n}.${name}#${pos}`, reads]);
        }
      }
    }
    found.sort((a, b) => a[0].localeCompare(b[0]));
    return {
      unlisted: [...unlisted].sort(),
      entries: found.map(([w]) => w),
      // Only what breaks the rule is named, so a failure reads as the list of
      // places to look at and not as 38 numbers.
      overOne: found.filter(([, n]) => n > 1).map(([w, n]) => `${w}: ${n}`),
      exports,
      calls,
    };
  }, MODULES);

  // No contract imports a module this test does not walk.
  expect(got.unlisted).toEqual([]);
  // The rule itself: not one entry asks twice.
  expect(got.overOne).toEqual([]);
  // The entries, and how many there are. A new one is red until somebody has
  // looked at it, which is the point of counting them.
  expect(got.entries).toEqual(ENTRIES);
  expect(got.entries.length).toBe(38);
  // And every exported function has been walked, reached or not.
  expect(got.exports).toBe(EXPORTS);
});
