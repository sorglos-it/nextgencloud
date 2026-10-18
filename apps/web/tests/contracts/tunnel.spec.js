import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("tunnel.json");

// Does the Web Crypto API of this engine know Ed25519? Only one test below
// needs it - the one that verifies the chain of trust list versions the codes
// are held against - and on an engine without it trust.parseList already
// refuses the very first version, because it cannot read the list's own keys:
// measured on the WebKit the Playwright of this tree brings, "trust: invalid
// list" out of the first version, so every one of the ten cases would come
// back false and the test would pass or fail for a reason that has nothing to
// do with codes. The neighbouring specs gate the same way (trust.spec.js,
// session.spec.js), each with its own probe, and this is that probe: a real key
// out of this file's own chain, not a made-up 32 bytes, so an engine that has
// Ed25519 but refuses a value that is no point on the curve is not skipped for
// the wrong reason.
async function hasEd25519(shell, chain) {
  return shell.evaluate(async (c) => {
    const { b64decode, fromUtf8 } = await import("/assets/js/contracts/b64.js");
    const payload = JSON.parse(fromUtf8(b64decode(c[0].payload)));
    let pub = null;
    for (const u of payload.users) {
      for (const k of u.keys) {
        if (k.kind === "password") pub = k.pub;
      }
    }
    try {
      await crypto.subtle.importKey("raw", b64decode(pub), { name: "Ed25519" }, false, ["verify"]);
      return true;
    } catch {
      return false;
    }
  }, chain);
}

test("the shell computes both fingerprints itself", async ({ shell }) => {
  const got = await shell.evaluate(async (d) => {
    const t = await import("/assets/js/contracts/tunnel.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    return {
      controller: await t.fingerprint(b64decode(d.controllerKey)),
      join: await t.joinFingerprint(d.join.node, b64decode(d.join.noise), b64decode(d.join.sign),
        b64decode(d.join.tls), d.join.url),
    };
  }, v);
  expect(got.controller).toBe(v.fingerprint);
  expect(got.join).toBe(v.join.fingerprint);
});

test("a pairing code is shown only when it fits the verified version", async ({ shell }) => {
  test.setTimeout(120_000);
  test.skip(!(await hasEd25519(shell, v.pairCheck.chain)),
    "no Ed25519 in this engine's Web Crypto: it verifies no version of this chain");
  const got = await shell.evaluate(async (d) => {
    const t = await import("/assets/js/contracts/tunnel.js");
    const trust = await import("/assets/js/contracts/trust.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const chain = d.pairCheck.chain.map((s) => ({
      payload: b64decode(s.payload),
      proof: { kind: s.proof.kind, kid: s.proof.kid, sig: b64decode(s.proof.sig),
        authData: s.proof.authData ? b64decode(s.proof.authData) : new Uint8Array(0),
        clientData: s.proof.clientData ? b64decode(s.proof.clientData) : new Uint8Array(0) },
    }));
    const anchor = { seq: 1, hash: await trust.hash(chain[0]) };
    const version = await trust.open(anchor, chain);
    return d.pairCheck.cases.map((c) => {
      try {
        return t.checkCode(t.parsePairCode(c.code), version);
      } catch {
        return false;
      }
    });
  }, v);
  const wrong = v.pairCheck.cases.filter((c, i) => c.valid !== got[i]).map((c) => c.name);
  expect(wrong).toEqual([]);
});

// A fingerprint is the one thing an admin compares by eye, and it names bytes
// that are used after the comparison: the Noise key a tunnel is dialled with,
// the three keys a joining API is listed with. So what is hashed has to be what
// applies afterwards - hashing the first half of a key and then dialling with
// all of it is the shape this contract tree keeps finding.
//
// b64.viewOf believes byteLength, and that is enough to open this: an object
// that reports half its size gets half of it hashed, and the fingerprint names
// bytes nobody will ever use. b64.bytesOf reads no property at all - the copy
// comes out of the internal slots, where no getter can be installed - so the
// two tests below hand both fingerprints four shapes that lie about their bytes
// and hold that the answer stays the honest one. The four are built inside the
// page because a class cannot cross page.evaluate; the names are the order.
const LIARS = [
  // Half the size, and then the whole key is what gets dialled.
  "byteLength answers half",
  // Twice the size: viewOf throws on a view of 64 over 32 real bytes, so this
  // one turns a fingerprint into an exception instead of a name.
  "byteLength answers double",
  // The first reading is honest and the second is not - the shape a place that
  // checks one reading and works on another walks into.
  "byteLength lies from the second reading",
  // Nothing about the size; the buffer is simply somewhere else.
  "buffer answers another buffer",
];

test("fingerprint names the bytes an object really holds", async ({ shell }) => {
  const got = await shell.evaluate(async ({ d, names }) => {
    const t = await import("/assets/js/contracts/tunnel.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const key = b64decode(d.controllerKey);
    const half = class extends Uint8Array { get byteLength() { return 16; } };
    const double = class extends Uint8Array { get byteLength() { return 64; } };
    const second = class extends Uint8Array {
      #n = 0;
      get byteLength() { return this.#n++ === 0 ? super.byteLength : 16; }
    };
    const elsewhere = class extends Uint8Array { get buffer() { return new ArrayBuffer(64); } };
    const out = {};
    const shapes = [half, double, second, elsewhere];
    for (let i = 0; i < shapes.length; i++) {
      const lying = new shapes[i](key.length);
      lying.set(key);
      try {
        out[names[i]] = await t.fingerprint(lying);
      } catch (e) {
        out[names[i]] = "threw: " + e.message;
      }
    }
    return out;
  }, { d: v, names: LIARS });
  for (const name of LIARS) expect(`${name}: ${got[name]}`).toBe(`${name}: ${v.fingerprint}`);
});

test("joinFingerprint names the bytes the three keys really hold", async ({ shell }) => {
  const got = await shell.evaluate(async ({ d, names }) => {
    const t = await import("/assets/js/contracts/tunnel.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const j = d.join;
    const raw = [b64decode(j.noise), b64decode(j.sign), b64decode(j.tls)];
    const half = class extends Uint8Array { get byteLength() { return 16; } };
    const double = class extends Uint8Array { get byteLength() { return 64; } };
    const second = class extends Uint8Array {
      #n = 0;
      get byteLength() { return this.#n++ === 0 ? super.byteLength : 16; }
    };
    const elsewhere = class extends Uint8Array { get buffer() { return new ArrayBuffer(64); } };
    const shapes = [half, double, second, elsewhere];
    const out = {};
    for (let i = 0; i < shapes.length; i++) {
      const lie = (b) => { const a = new shapes[i](b.length); a.set(b); return a; };
      // Each of the three positions in turn, so none of them is honest only
      // because its neighbour happened to be copied.
      for (let pos = 0; pos < 3; pos++) {
        const args = raw.map((b, k) => (k === pos ? lie(b) : b));
        try {
          out[`${names[i]}#${pos}`] = await t.joinFingerprint(j.node, args[0], args[1], args[2], j.url);
        } catch (e) {
          out[`${names[i]}#${pos}`] = "threw: " + e.message;
        }
      }
    }
    return out;
  }, { d: v, names: LIARS });
  for (const name of LIARS) {
    for (let pos = 0; pos < 3; pos++) {
      const key = `${name}#${pos}`;
      expect(`${key}: ${got[key]}`).toBe(`${key}: ${v.join.fingerprint}`);
    }
  }
});

// The codes themselves: one grammar, two prefixes and the limits of Go's
// tunnel.parseCode. A code is pasted in by an admin, so the space around it is
// trimmed - with Go's set of space characters and not the engine's, because a
// code the controller takes and the shell refuses is a support call nobody can
// answer.
test("a code is read only under its own prefix and within its limits", async ({ shell }) => {
  const got = await shell.evaluate(async (d) => {
    const t = await import("/assets/js/contracts/tunnel.js");
    const { b64encode, b64decode, utf8, fromUtf8 } = await import("/assets/js/contracts/b64.js");
    const pair = d.pairCheck.cases[0].code;
    const body = pair.slice(t.PAIR_PREFIX.length);
    const took = (fn, s) => { try { fn(s); return true; } catch { return false; } };
    const fields = () => JSON.parse(fromUtf8(b64decode(body)));
    const wrap = (c) => t.PAIR_PREFIX + b64encode(utf8(JSON.stringify(c)));
    // A well formed code of exactly MAX_PAIR_CODE characters, and one of one
    // more: the JSON is padded with the trailing space strict JSON allows, so
    // the only thing wrong with the longer one is its size. 761 and 762 bytes
    // are what b64.encodedLen turns into 1015 and 1016 characters, which with
    // the 9 of the prefix are 1024 and 1025.
    const sized = (n) => {
      const doc = JSON.stringify(fields());
      return t.PAIR_PREFIX + b64encode(utf8(doc + " ".repeat(n - doc.length)));
    };
    const atMax = sized(761), overMax = sized(762);
    return {
      // The two prefixes tell the two codes apart, and neither reads the other.
      pairUnderPair: took(t.parsePairCode, pair),
      pairUnderJoin: took(t.parseJoinCode, pair),
      joinUnderJoin: took(t.parseJoinCode, d.join.code),
      joinUnderPair: took(t.parsePairCode, d.join.code),
      // What may stand around the pasted text. Go trims U+0085, the engine's
      // own trim does not; the engine trims U+FEFF, Go does not.
      spaces: took(t.parsePairCode, " \t\r\n" + pair + "\n "),
      nel: took(t.parsePairCode, "\u0085" + pair + "\u0085"),
      zwnbsp: took(t.parsePairCode, "﻿" + pair),
      nbsp: took(t.parsePairCode, " " + pair + " "),
      // Nothing inside it, though: a code is one word.
      inner: took(t.parsePairCode, t.PAIR_PREFIX + " " + body),
      // The size, said out loud so that "refused" below is about the size.
      sizes: [atMax.length, overMax.length],
      atMax: took(t.parsePairCode, atMax),
      overMax: took(t.parsePairCode, overMax),
      // Padding, an empty body, no prefix at all, and bytes instead of text.
      padded: took(t.parsePairCode, t.PAIR_PREFIX + body + "="),
      empty: took(t.parsePairCode, t.PAIR_PREFIX),
      bare: took(t.parsePairCode, body),
      notText: took(t.parsePairCode, utf8(pair)),
      // And the five fields the grammar decides on, one wrong at a time.
      url: took(t.parsePairCode, wrap({ ...fields(), url: "https://api-1.test:7443/tunnel" })),
      key: took(t.parsePairCode, wrap({ ...fields(), key: fields().key.slice(0, 4) })),
      tls: took(t.parsePairCode, wrap({ ...fields(), tls: fields().tls.slice(0, 4) })),
      secret: took(t.parsePairCode, wrap({ ...fields(), secret: fields().secret.slice(0, 4) })),
      anchor: took(t.parsePairCode, wrap({ ...fields(), anchor: { seq: 0, hash: fields().anchor.hash } })),
      // What a taken code carries: the fields of the vector, decoded.
      taken: (() => {
        const c = t.parsePairCode(pair);
        return { url: c.url, key: b64encode(c.key), tls: b64encode(c.tls),
          secret: b64encode(c.secret), seq: c.anchor.seq, hash: b64encode(c.anchor.hash) };
      })(),
      want: fields(),
    };
  }, v);
  expect(got.pairUnderPair).toBe(true);
  expect(got.pairUnderJoin).toBe(false);
  expect(got.joinUnderJoin).toBe(true);
  expect(got.joinUnderPair).toBe(false);
  expect(got.spaces).toBe(true);
  // The two characters the engine and Go disagree about, decided Go's way.
  expect(got.nel).toBe(true);
  expect(got.zwnbsp).toBe(false);
  expect(got.nbsp).toBe(true);
  expect(got.inner).toBe(false);
  expect(got.sizes).toEqual([1024, 1025]);
  expect(got.atMax).toBe(true);
  expect(got.overMax).toBe(false);
  expect(got.padded).toBe(false);
  expect(got.empty).toBe(false);
  expect(got.bare).toBe(false);
  expect(got.notText).toBe(false);
  expect([got.url, got.key, got.tls, got.secret, got.anchor]).toEqual([false, false, false, false, false]);
  // The fields come out as the code holds them, and the bytes as bytes.
  expect(got.taken).toEqual({
    url: got.want.url, key: got.want.key, tls: got.want.tls,
    secret: got.want.secret, seq: got.want.anchor.seq, hash: got.want.anchor.hash,
  });
});

// components decides what a server installs, so a role nobody asked for would
// put software on a machine that was never meant to run it (Go:
// tunnel.Components).
test("the roles of a server name the software it runs", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/tunnel.js");
    const trust = await import("/assets/js/contracts/trust.js");
    return {
      names: [t.ROLE_WEB, t.ROLE_API, t.ROLE_DB, t.ROLE_SERVICES],
      auto: [trust.AUTO_CONTROLLER, trust.AUTO_API, trust.AUTO_WEB],
      none: t.components([]),
      web: t.components([t.ROLE_WEB]),
      api: t.components([t.ROLE_API]),
      // db and services run no NGC software of their own: the db server takes
      // its migrations out of the ngc-api release without installing it, and
      // services runs app containers.
      db: t.components([t.ROLE_DB]),
      services: t.components([t.ROLE_SERVICES]),
      all: t.components([t.ROLE_WEB, t.ROLE_API, t.ROLE_DB, t.ROLE_SERVICES]),
      // Sorted, whatever order the roles arrive in.
      reversed: t.components([t.ROLE_API, t.ROLE_WEB]),
      // No list of roles at all: the controller alone, never an exception out
      // of a function that names software to install.
      notAList: t.components(undefined),
      nonsense: t.components(["WEB", "api "]),
      contract: t.CONTRACT,
      secret: t.SECRET_SIZE,
      maxCode: t.MAX_PAIR_CODE,
      prefixes: [t.PAIR_PREFIX, t.JOIN_PREFIX],
    };
  });
  expect(got.names).toEqual(["web", "api", "db", "services"]);
  expect(got.auto).toEqual(["ngc-controller", "ngc-api", "ngc-web"]);
  expect(got.none).toEqual(["ngc-controller"]);
  expect(got.web).toEqual(["ngc-controller", "ngc-web"]);
  expect(got.api).toEqual(["ngc-api", "ngc-controller"]);
  expect(got.db).toEqual(["ngc-controller"]);
  expect(got.services).toEqual(["ngc-controller"]);
  expect(got.all).toEqual(["ngc-api", "ngc-controller", "ngc-web"]);
  expect(got.reversed).toEqual(["ngc-api", "ngc-controller", "ngc-web"]);
  expect(got.notAList).toEqual(["ngc-controller"]);
  expect(got.nonsense).toEqual(["ngc-controller"]);
  expect(got.contract).toBe(1);
  expect(got.secret).toBe(16);
  expect(got.maxCode).toBe(1024);
  expect(got.prefixes).toEqual(["ngc-pair:", "ngc-join:"]);
});

// checkCode answers and never throws: it is asked about a code an admin pasted
// in, beside a version the shell verified, and an exception out of it would be
// an unhandled rejection in the page instead of a refused code.
test("checkCode refuses everything that is not a verified version", async ({ shell }) => {
  const got = await shell.evaluate(async (d) => {
    const t = await import("/assets/js/contracts/tunnel.js");
    const code = t.parsePairCode(d.pairCheck.cases[0].code);
    const ask = (c, version) => {
      try {
        return t.checkCode(c, version);
      } catch (e) {
        return "threw: " + e.message;
      }
    };
    const hash = new Uint8Array(32);
    return [
      ask(code, undefined), ask(code, null), ask(code, {}), ask(code, { list: null }),
      ask(code, { list: {}, hash }),
      ask(code, { list: { seq: 2, nodes: null }, hash }),
      ask(code, { list: { seq: 2, nodes: [null, 1, "x"] }, hash }),
      ask(undefined, { list: { seq: 2, nodes: [] }, hash }),
      ask(null, { list: { seq: 2, nodes: [] }, hash }),
      ask({}, { list: { seq: 2, nodes: [] }, hash }),
      ask({ anchor: {} }, { list: { seq: 2, nodes: [] }, hash }),
    ];
  }, v);
  expect(got).toEqual(new Array(11).fill(false));
});
