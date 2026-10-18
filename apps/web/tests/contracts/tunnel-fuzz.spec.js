import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// The fuzz target for the pairing and joining codes (CLAUDE.md asks every
// parser for one). A code is pasted out of a clipboard into a field, so it
// arrives with whatever came with it: half a prefix, a doubled separator, the
// invisible space a chat program put around it. What the target holds the
// module against:
//
//   - parsePairCode and parseJoinCode answer with a code or throw BadCode, and
//     nothing else. A TypeError or a RangeError from the engine is no answer a
//     contract can give.
//   - every code they answer with is one the contract describes, and the rules
//     stand here in full rather than being asked of the module: a tunnel
//     address of at most 256 characters of its own alphabet, a Noise key and a
//     TLS pin of 32 bytes, a secret of 16, an anchor with a sequence number of
//     at least one and a hash of 32 bytes.
//   - and the answer is FROZEN, itself and its anchor. checkCode decides about
//     a code and the caller dials with it afterwards; a field that could still
//     be swapped between the two would be a check of one address and a
//     connection to another.
//   - the two prefixes never overlap: no text is read as both a pairing and a
//     joining code, because the admin pastes one of them into a script that
//     does the other job.
//   - checkCode answers true or false for anything at all, fields behind
//     throwing getters included: it decides whether a code is shown, and an
//     exception out of it is an unhandled rejection in the page.
//   - components answers a sorted list that always names the controller and
//     never anything but the three NGC parts, whatever it is handed.
//   - fingerprint answers 22 characters of base64url or throws BadInput, and
//     it hashes its own copy: an object whose length lies is named by the
//     fingerprint of the bytes it really holds.
//   - and enough of the inputs are read as codes at all. Every rule above
//     holds for a parser that refuses everything, so without a floor under the
//     accepted ones a step backwards stays green.
//
// Seed and rounds come from the environment, so a failure can be run again:
// NGC_FUZZ_SEED=123 NGC_FUZZ_ROUNDS=50000 npm test -- contracts/tunnel-fuzz
const v = vectors("tunnel.json");
const seed = Number(process.env.NGC_FUZZ_SEED || 20260923) >>> 0;
const rounds = Number(process.env.NGC_FUZZ_ROUNDS || 12000);

test(`fuzz: a code or BadCode, ${rounds} inputs from seed ${seed}`, async ({ shell }) => {
  test.setTimeout(180_000);
  const out = await shell.evaluate(async ([d, seed, rounds]) => {
    const t = await import("/assets/js/contracts/tunnel.js");
    const { b64encode, BadInput } = await import("/assets/js/contracts/b64.js");

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
      const b = new Uint8Array(n);
      for (let i = 0; i < n; i++) b[i] = upto(256);
      return b;
    };

    // The pieces a code is built from: of each the ones that keep the rule and
    // the ones that sit just outside it. A piece is drawn from the edge cases
    // every third time, so whole codes come out often enough for the rules
    // below to have something to hold.
    const goodURLs = ["wss://api-1.test:7443/tunnel", "wss://a.b:1/tunnel", "wss://localhost/tunnel",
      "wss://[2001:db8::1]:7443/tunnel", "wss://" + "a".repeat(60) + ".test/tunnel"];
    const oddURLs = ["ws://api-1.test:7443/tunnel", "wss://api-1.test:7443/", "wss://api-1.test:7443",
      "https://api-1.test/tunnel", "wss://API-1.test/tunnel", "wss://api 1.test/tunnel",
      "wss://api-1.test:99999/tunnel", "wss://" + "a".repeat(300) + "/tunnel", "", "/tunnel",
      "wss://api-1.test/tunnel\n", "wss://api-1.test/tunnel#x"];
    const goodKeys = [b64encode(randomBytes(32)), b64encode(new Uint8Array(32))];
    const oddKeys = [b64encode(randomBytes(31)), b64encode(randomBytes(33)), b64encode(randomBytes(0)),
      b64encode(randomBytes(16)), "AQID=", "a b", "ä"];
    const goodSecrets = [b64encode(randomBytes(16))];
    const oddSecrets = [b64encode(randomBytes(15)), b64encode(randomBytes(17)), "", b64encode(randomBytes(32))];
    const goodSeqs = [1, 2, 5, 4294967296, 9007199254740991];
    const oddSeqs = [0, -1, 1.5, "1"];
    const piece = (good, odd) => (upto(3) === 0 ? pick(odd) : pick(good));

    // A code document, its members in the order the round draws them, so a
    // parser that leans on the order of a document is seen.
    const names = ["url", "key", "tls", "secret", "anchor", "extra", "URL", "__proto__", ""];
    const document = () => {
      const parts = [
        ["url", piece(goodURLs, oddURLs)],
        ["key", piece(goodKeys, oddKeys)],
        ["tls", piece(goodKeys, oddKeys)],
        ["secret", piece(goodSecrets, oddSecrets)],
        ["anchor", { seq: piece(goodSeqs, oddSeqs), hash: piece(goodKeys, oddKeys) }],
      ];
      for (let i = parts.length - 1; i > 0; i--) {
        const j = upto(i + 1);
        [parts[i], parts[j]] = [parts[j], parts[i]];
      }
      if (upto(8) === 0) parts.push([pick(names), pick([...goodKeys, ...oddKeys])]);
      if (upto(8) === 0) parts.pop();
      const body = parts.map(([n, val]) => JSON.stringify(n) + ":" + JSON.stringify(val)).join(",");
      return "{" + body + "}";
    };

    // base64url of a document, with a quarter of them spoiled where the
    // alphabet ends: padding, a character that belongs to no alphabet, a
    // length that leaves bits over.
    const encoder = new TextEncoder();
    const body = () => {
      const s = b64encode(encoder.encode(document()));
      switch (upto(12)) {
        case 0: return s + "=";
        case 1: return s + "==";
        case 2: return s.slice(0, -1);
        case 3: return s + "A";
        case 4: return s.replace("A", "+").replace("B", "/");
        case 5: return s.slice(0, upto(s.length + 1));
        default: return s;
      }
    };

    // The GESTALT of a pasted code, which is where this one differs from the
    // other targets: the prefix halved, doubled, cased differently or swapped
    // for the other one, and the space a clipboard brings - including the two
    // characters where Go's TrimSpace and the engine's own trim disagree
    // (U+0085, which Go trims and the engine does not, and U+FEFF, the other
    // way round).
    const prefixes = [t.PAIR_PREFIX, t.JOIN_PREFIX, "ngc-pair", "ngc-pai", "ngc-pair::",
      "ngc-pair:ngc-pair:", "NGC-PAIR:", "ngc_pair:", "ngc-join", ":", "", "ngc-pair :"];
    const spaces = ["", " ", "\t", "\n", "\r", "\v", "\f", "\u0085", " ", " ",
      " ", " ", " ", " ", " ", " ", "　", "﻿", "​"];
    const space = () => {
      let s = "";
      const n = upto(3);
      for (let i = 0; i < n; i++) s += pick(spaces);
      return s;
    };
    const known = [d.code, d.join.code];
    const codeText = () => {
      switch (upto(10)) {
        case 0: return pick(known);
        case 1: { // a known code with one piece of junk pushed into it
          const s = pick(known);
          const i = upto(s.length + 1);
          return s.slice(0, i) + pick([...spaces, "=", "+", "/", "ä", "A"]) + s.slice(i);
        }
        case 2: return space() + pick(known) + space();
        case 3: return pick(prefixes) + body().slice(0, upto(40));
        case 4: return pick(prefixes) + "A".repeat(upto(1200)); // right at MAX_PAIR_CODE
        default: return space() + pick(prefixes) + body() + space();
      }
    };
    // What is no text at all: the module is handed a string in Go and anything
    // here, and "" + s would make a code out of an object that answers twice.
    const notText = [null, undefined, 0, 7, true, [], {}, new Uint8Array(3),
      { toString: () => d.code }, Object.create(null)];

    // The rules, spelled out here. The address alphabet is url.js's: lower
    // case, because NGC compares addresses as text, and nothing that could
    // start a second line in a configuration file.
    const URL_CHARS = /^[a-z0-9.:[\]/-]+$/;
    const okURL = (u) => typeof u === "string" && u.length <= 256 && URL_CHARS.test(u)
      && u.startsWith("wss://") && u.endsWith("/tunnel");
    const found = [];
    const say = (why, s) => { if (found.length < 8) found.push({ why, input: String(s).slice(0, 120) }); };
    const count = { pair: 0, join: 0, refused: 0, checked: 0 };

    const readCode = (name, parse, prefix, s) => {
      let c;
      try {
        c = parse(s);
      } catch (e) {
        if (!(e instanceof t.BadCode)) say(`${name} threw ${e.constructor.name}: ${e.message}`, s);
        count.refused++;
        return null;
      }
      if (c === null || typeof c !== "object") { say(`${name} answered ${typeof c}`, s); return null; }
      if (!Object.isFrozen(c) || !Object.isFrozen(c.anchor)) say(`${name} answered a code that is not frozen`, s);
      if (!okURL(c.url)) say(`${name} answered a code whose address is none: ${String(c.url).slice(0, 60)}`, s);
      if (!(c.key instanceof Uint8Array) || c.key.length !== 32) say(`${name} answered a key of ${c.key?.length} bytes`, s);
      if (!(c.tls instanceof Uint8Array) || c.tls.length !== 32) say(`${name} answered a pin of ${c.tls?.length} bytes`, s);
      if (!(c.secret instanceof Uint8Array) || c.secret.length !== t.SECRET_SIZE) {
        say(`${name} answered a secret of ${c.secret?.length} bytes`, s);
      }
      if (!Number.isSafeInteger(c.anchor?.seq) || c.anchor.seq < 1) say(`${name} answered a sequence number of ${String(c.anchor?.seq)}`, s);
      if (!(c.anchor?.hash instanceof Uint8Array) || c.anchor.hash.length !== 32) {
        say(`${name} answered an anchor hash of ${c.anchor?.hash?.length} bytes`, s);
      }
      // The text it came out of really carried this prefix and no more than a
      // code's bytes.
      if (typeof s === "string") {
        const trimmed = s.trim();
        if (!trimmed.includes(prefix)) say(`${name} read a code out of a text without its prefix`, s);
        if (trimmed.length > t.MAX_PAIR_CODE + 8) say(`${name} read a code out of ${trimmed.length} characters`, s);
      }
      return c;
    };

    for (let i = 0; i < rounds && found.length < 8; i++) {
      const s = i % 40 === 0 ? pick(notText) : codeText();
      const pair = readCode("parsePairCode", t.parsePairCode, t.PAIR_PREFIX, s);
      const join = readCode("parseJoinCode", t.parseJoinCode, t.JOIN_PREFIX, s);
      if (pair) count.pair++;
      if (join) count.join++;
      // One text, one reading: the two prefixes differ, so nothing may be both.
      if (pair && join) say("one text read as a pairing code and as a joining code", s);
    }

    // checkCode, over versions no parser would ever hand it - and over codes
    // whose fields fight back.
    const trap = () => ({ get url() { throw new Error("no"); }, key: new Uint8Array(32) });
    const versions = [null, undefined, 0, "", [], {}, { list: null }, { list: {} },
      { list: { seq: 1, nodes: null } }, { list: { seq: 1, nodes: [] }, hash: new Uint8Array(32) },
      { list: { seq: 1, nodes: [null, 0, "x", {}] }, hash: new Uint8Array(32) },
      { list: { seq: 1, nodes: [{ kind: "api", noise: new Uint8Array(32), url: "wss://a.b/tunnel", tls: new Uint8Array(32) }] }, hash: new Uint8Array(32) },
      { get list() { throw new Error("no"); } },
      { list: { seq: 1, get nodes() { throw new Error("no"); } }, hash: new Uint8Array(32) }];
    const codes = [null, undefined, 0, "", [], {}, trap(),
      { url: "wss://a.b/tunnel", key: new Uint8Array(32), tls: new Uint8Array(32), anchor: { seq: 1, hash: new Uint8Array(32) } },
      { url: 1, key: 1, tls: 1, anchor: null }];
    for (const c of codes) {
      for (const ver of versions) {
        let got;
        try {
          got = t.checkCode(c, ver);
        } catch (e) {
          say(`checkCode threw ${e.constructor.name}`, "a shape");
          continue;
        }
        if (typeof got !== "boolean") say(`checkCode answered ${typeof got}`, "a shape");
        else count.checked++;
      }
    }

    // components: whatever it is handed, the controller is named and nothing
    // that is not an NGC part.
    const parts = new Set([t.ROLE_WEB, t.ROLE_API, t.ROLE_DB, t.ROLE_SERVICES]);
    const roleLists = [null, undefined, 0, "web", {}, [t.ROLE_WEB], [t.ROLE_API],
      [t.ROLE_API, t.ROLE_WEB], [t.ROLE_WEB, t.ROLE_WEB], [t.ROLE_DB, t.ROLE_SERVICES],
      ["", null, 7, t.ROLE_API], [...parts], new Proxy([t.ROLE_API], {})];
    for (const roles of roleLists) {
      let c;
      try {
        c = t.components(roles);
      } catch (e) {
        say(`components threw ${e.constructor.name}`, JSON.stringify(String(roles)));
        continue;
      }
      if (!Array.isArray(c)) say(`components answered ${typeof c}`, String(roles));
      else if (c.length < 1 || c.length > 3) say(`components answered ${c.length} parts`, String(roles));
      else if (c.some((x, i) => i > 0 && c[i - 1] >= x)) say("components answered an unsorted list", String(roles));
    }

    // fingerprint: 22 characters of base64url, and the bytes it hashes are its
    // own copy. The class below reports half its length and is the shape the
    // copy exists for; both readings have to answer the same name.
    class Half extends Uint8Array {
      get length() { return 16; }
      get byteLength() { return 16; }
    }
    const keys = [randomBytes(32), randomBytes(0), randomBytes(1), randomBytes(1024)];
    const notKeys = [null, undefined, 0, "abc", [], {}, new ArrayBuffer(32), new Uint16Array(16)];
    let prints = 0;
    for (const k of keys) {
      const got = await t.fingerprint(k);
      if (typeof got !== "string" || !/^[A-Za-z0-9_-]{22}$/.test(got)) say(`fingerprint answered ${String(got)}`, "a key");
      else prints++;
    }
    const honest = randomBytes(32);
    const lying = new Half(honest);
    if (await t.fingerprint(lying) !== await t.fingerprint(honest)) {
      say("a key whose length lies was named by another fingerprint", "a key");
    }
    for (const k of notKeys) {
      try {
        await t.fingerprint(k);
        say("something that is no bytes was named", String(k));
      } catch (e) {
        if (!(e instanceof BadInput)) say(`fingerprint threw ${e.constructor.name} for what is no bytes`, String(k));
      }
    }

    return { found, count, prints };
  }, [v, seed, rounds]);

  // What was found comes first: the loop stops at the eighth finding, so a
  // fault early in the run leaves the counts below under their floors as well.
  expect(out.found).toEqual([]);
  if (out.found.length === 0) {
    // Measured: 1108 pairing codes and 1110 joining codes out of 12000 inputs
    // of this seed, in both engines. The floors are at 3 in 100, about a third
    // of that. They count in rounds and not in a fixed number, because the
    // rounds are a knob this file advertises itself.
    expect(out.count.pair).toBeGreaterThan(Math.floor(rounds * 0.03));
    expect(out.count.join).toBeGreaterThan(Math.floor(rounds * 0.03));
    // And most of them were refused, so the refusals above were really walked.
    expect(out.count.refused).toBeGreaterThan(rounds);
    // Every shape reached checkCode and answered.
    expect(out.count.checked).toBe(9 * 14);
    expect(out.prints).toBe(4);
  }
});
