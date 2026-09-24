import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("trust.json");

// Does the Web Crypto API of this engine know Ed25519? Every key in
// trust.json is Ed25519 - trusttest.NewSigner signs password keys with it
// too, not only NewPasskey - so an engine without it can verify no signature
// in this file at all, and every "valid" chain would come back refused
// instead of decided (measured: exactly that, on the WebKit Playwright
// brings). Probes with a real key read out of the first vector, not an
// arbitrary buffer: an engine that has Ed25519 but rejects a 32-byte value
// that is not a point on the curve would otherwise be skipped by mistake,
// for the wrong reason (Round 1 finding K5 - an earlier version of this probe
// used new Uint8Array(32), and the first report's claim of "same probe shape"
// as sign.spec.js's real-key probe was wrong; corrected here, and named as
// wrong rather than quietly replaced).
async function hasEd25519(shell, firstCase) {
  return shell.evaluate(async (c) => {
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const payload = JSON.parse(new TextDecoder().decode(b64decode(c.chain[0].payload)));
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
  }, firstCase);
}

test("every chain of trust.json is decided as Go decides it", async ({ shell }) => {
  test.setTimeout(120_000);
  test.skip(!(await hasEd25519(shell, v[0])),
    "no Ed25519 in this engine's Web Crypto: it cannot verify any signature in trust.json");
  const got = await shell.evaluate(async (cases) => {
    const t = await import("/assets/js/contracts/trust.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of cases) {
      try {
        const chain = c.chain.map((s) => ({
          payload: b64decode(s.payload),
          proof: {
            kind: s.proof.kind, kid: s.proof.kid, sig: b64decode(s.proof.sig),
            authData: s.proof.authData ? b64decode(s.proof.authData) : new Uint8Array(0),
            clientData: s.proof.clientData ? b64decode(s.proof.clientData) : new Uint8Array(0),
          },
        }));
        const anchor = { seq: c.anchor.seq, hash: b64decode(c.anchor.hash) };
        const v = await t.open(anchor, chain);
        out.push({ valid: true, seq: v.list.seq });
      } catch {
        out.push({ valid: false, seq: 0 });
      }
    }
    return out;
  }, v);

  const wrong = v.filter((c, i) => c.valid !== got[i].valid || (c.valid && c.seq !== got[i].seq))
    .map((c) => c.name);
  expect(wrong).toEqual([]);
});

test("the limits without their own vectors hold too", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/trust.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    const base = () => ({
      seq: 1, issued: 1, installation: "EREREREREREREREREREREQ",
      rp: { id: "", origins: [] }, users: [], roles: [], nodes: [], stores: [], auto: [],
    });
    const many = (n, make) => Array.from({ length: n }, (_, i) => make(i));
    // trust.js's own interface list writes "parseList(payload) -> list", but
    // the code block that defines it is "export async function parseList",
    // which it has to be: it awaits proof.validateUserKey for the id ===
    // keyID(pub) check. parseList therefore never throws synchronously - it
    // always returns a settled or pending promise - so a decodeFails that
    // calls it without await never reaches its own catch, whatever trust.js
    // does. Measured: with the literal, non-async version of this helper the
    // three cases below came back { autoOver: false, originsOver: false,
    // rootUsersOver: false } against the working validateList from the test
    // above, i.e. always false - the given helper cannot pass as specified
    // (Step 4: "die drei Grenzen ... greifen"). Fixed here by awaiting the
    // promise it was always going to get.
    const decodeFails = async (obj) => {
      try {
        await t.parseList(utf8(JSON.stringify(obj)));
        return false;
      } catch {
        return true;
      }
    };
    return {
      // trust.MaxAuto, MaxOrigins and MaxRoot have no vectors of their own.
      autoOver: await decodeFails({ ...base(), auto: many(65, (i) => ({ target: "a" + i + ".b", key: "A".repeat(43) })) }),
      originsOver: await decodeFails({ ...base(), rp: { id: "a.b", origins: many(9, () => "https://a.b") } }),
      rootUsersOver: await decodeFails({
        ...base(),
        nodes: [{ id: "ctl-1", kind: "controller", name: "n", noise: "A".repeat(43),
          rootUsers: many(257, () => "EREREREREREREREREREREQ") }],
      }),
    };
  });
  expect(out).toEqual({ autoOver: true, originsOver: true, rootUsersOver: true });
});

// Round 1 finding Wichtig 5: the test below used to pin only MAX_AUTO,
// MAX_ORIGINS and MAX_ROOT - three of trust.js's nine MAX_* constants. The
// other six (MAX_USERS, MAX_NODES, MAX_ROLES, MAX_STORES, MAX_KEYS_PER_USER,
// MAX_ENTRIES) had neither a vector nor a case: a wrongly copied constant on
// any of them would stay green, exactly the error class Wichtig 2 found. All
// nine now go through the same rig, both sides, through validateList itself -
// no real cryptography needed, since validateList (via
// proof.validateUserKeySync) checks a key's shape only, never its signature
// or id === keyID(pub).
test("all nine MAX_* limits are pinned on both sides, not just over", async ({ shell }) => {
  test.setTimeout(60_000);
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/trust.js");
    const { b64encode } = await import("/assets/js/contracts/b64.js");
    const id = "EREREREREREREREREREREQ"; // 16 bytes, valid ID shape (also used by the vectors above)
    const keyId = "AAAAAAAAAAAAAAAAAAAAAA"; // 16 zero bytes, valid key-id shape
    const validKey = (kid) => ({
      id: kid, kind: "password", alg: "EdDSA", pub: new Uint8Array(32), credId: new Uint8Array(0),
      kdf: { alg: "argon2id", salt: new Uint8Array(16), m: 65536, t: 3, p: 1 },
    });
    const validList = (patch) => ({
      seq: 1, prev: new Uint8Array(0), issued: 1, installation: id,
      rp: { id: "", origins: [] },
      users: [{ id, name: "admin", roles: ["admin"], keys: [validKey(keyId)] }],
      roles: [{ name: "admin", level: "critical", black: [], white: [], passkeyOnly: false, passwordToo: false }],
      nodes: [], stores: [], auto: [],
      ...patch,
    });
    // Same length throughout (3-digit, zero-padded), so ascending index order
    // is ascending string order too - unpadded "a9" > "a10" would break the
    // sorted-target rule before the count limit is ever reached.
    const pad = (i, n = 3) => String(i).padStart(n, "0");
    const autoEntries = (n) => Array.from({ length: n },
      (_, i) => ({ target: "app" + pad(i) + ".b", key: new Uint8Array(32) }));
    const originEntries = (n) => Array.from({ length: n }, (_, i) => "https://a" + i + ".example");
    const rootUsers = (n) => Array.from({ length: n }, () => id); // one real user, repeated: no dedup rule to honor
    const controller = (rootUsersList) => ([{
      id: "ctl-1", kind: "controller", name: "n", noise: new Uint8Array(32),
      sign: new Uint8Array(0), url: "", tls: new Uint8Array(0), rootUsers: rootUsersList, rootRoles: [],
    }]);

    // A 16-byte ID whose base64url text does not sort the way its own byte
    // value does (base64url's alphabet order is not byte order: "0"-"9" sit
    // below "A"-"Z" in ASCII despite standing for the higher 6-bit values) -
    // so N distinct IDs are sorted as text after encoding, not built already
    // in order. marker keeps two pools (user IDs, key IDs) from ever
    // colliding with each other or with the fixed ids above.
    const sortedIds = (n, marker) => {
      const out = [];
      for (let i = 0; i < n; i++) {
        const b = new Uint8Array(16);
        b[0] = marker;
        new DataView(b.buffer).setUint32(12, i);
        out.push(b64encode(b));
      }
      return out.sort();
    };
    const manyUsers = (n) => {
      const uids = sortedIds(n, 0x10), kids = sortedIds(n, 0x20);
      return uids.map((uid, i) => ({ id: uid, name: "u" + i, roles: ["admin"], keys: [validKey(kids[i])] }));
    };
    const manyNodes = (n) => Array.from({ length: n }, (_, i) => ({
      id: "ctl" + pad(i, 4), kind: "controller", name: "n", noise: new Uint8Array(32),
      sign: new Uint8Array(0), url: "", tls: new Uint8Array(0), rootUsers: [], rootRoles: [],
    }));
    const emptyRole = (name) => ({ name, level: "off", black: [], white: [], passkeyOnly: false, passwordToo: false });
    const manyRoles = (extra) => {
      const names = Array.from({ length: extra }, (_, i) => "role" + pad(i, 4));
      names.push("admin");
      names.sort();
      return names.map(emptyRole);
    };
    const manyStores = (n) => Array.from({ length: n }, (_, i) => ({
      id: "store" + pad(i), name: "s", url: "https://s" + i + ".example", key: new Uint8Array(32),
    }));
    const manyKeys = (n) => sortedIds(n, 0x30).map(validKey);
    const manyEntries = (n) => Array.from({ length: n }, (_, i) => "node/n" + i);

    return {
      autoAtLimit: t.validateList(validList({ auto: autoEntries(64) })),
      autoOverLimit: t.validateList(validList({ auto: autoEntries(65) })),
      originsAtLimit: t.validateList(validList({ rp: { id: "", origins: originEntries(8) } })),
      originsOverLimit: t.validateList(validList({ rp: { id: "", origins: originEntries(9) } })),
      rootAtLimit: t.validateList(validList({ nodes: controller(rootUsers(256)) })),
      rootOverLimit: t.validateList(validList({ nodes: controller(rootUsers(257)) })),

      usersAtLimit: t.validateList(validList({ users: manyUsers(10000) })),
      usersOverLimit: t.validateList(validList({ users: manyUsers(10001) })),
      nodesAtLimit: t.validateList(validList({ nodes: manyNodes(256) })),
      nodesOverLimit: t.validateList(validList({ nodes: manyNodes(257) })),
      rolesAtLimit: t.validateList(validList({ roles: manyRoles(255) })),
      rolesOverLimit: t.validateList(validList({ roles: manyRoles(256) })),
      storesAtLimit: t.validateList(validList({ stores: manyStores(64) })),
      storesOverLimit: t.validateList(validList({ stores: manyStores(65) })),
      keysAtLimit: t.validateList(validList({ users: [{ id, name: "admin", roles: ["admin"], keys: manyKeys(16) }] })),
      keysOverLimit: t.validateList(validList({ users: [{ id, name: "admin", roles: ["admin"], keys: manyKeys(17) }] })),
      entriesAtLimit: t.validateList(validList({
        roles: [emptyRole("admin"), { ...emptyRole("extra"), black: manyEntries(256) }],
      })),
      entriesOverLimit: t.validateList(validList({
        roles: [emptyRole("admin"), { ...emptyRole("extra"), black: manyEntries(257) }],
      })),
    };
  });
  expect(out).toEqual({
    autoAtLimit: true, autoOverLimit: false,
    originsAtLimit: true, originsOverLimit: false,
    rootAtLimit: true, rootOverLimit: false,
    usersAtLimit: true, usersOverLimit: false,
    nodesAtLimit: true, nodesOverLimit: false,
    rolesAtLimit: true, rolesOverLimit: false,
    storesAtLimit: true, storesOverLimit: false,
    keysAtLimit: true, keysOverLimit: false,
    entriesAtLimit: true, entriesOverLimit: false,
  });
});

// Round 1 finding Wichtig 1: Go refuses a non-empty rp.id that is not a
// relying party ID - a DNS name in lower case, no scheme, no port, no path,
// at most 253 bytes (shared/proof/proof.go:212, ValidRPID; checked from
// shared/trust/trust.go:497). trust.js's validateList did not have this line
// at all. None of the 24 vectors ever sets an invalid rp.id - all 52
// payloads carry "ngc.example.org" - so this is this task's own case, built
// directly against validateList; both sides of the 253-byte limit too.
test("rp.id is checked against Go's relying party grammar, both sides", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/trust.js");
    const id = "EREREREREREREREREREREQ";
    const keyId = "AAAAAAAAAAAAAAAAAAAAAA";
    const withRPID = (rpId) => t.validateList({
      seq: 1, prev: new Uint8Array(0), issued: 1, installation: id,
      rp: { id: rpId, origins: [] },
      users: [{
        id, name: "admin", roles: ["admin"],
        keys: [{ id: keyId, kind: "password", alg: "EdDSA", pub: new Uint8Array(32), credId: new Uint8Array(0),
          kdf: { alg: "argon2id", salt: new Uint8Array(16), m: 65536, t: 3, p: 1 } }],
      }],
      roles: [{ name: "admin", level: "critical", black: [], white: [], passkeyOnly: false, passwordToo: false }],
      nodes: [], stores: [], auto: [],
    });
    return {
      empty: withRPID(""), // no passkey in this list, so this is Go's own zero value, never checked
      valid: withRPID("ngc.example.org"),
      scheme: withRPID("https://ngc.example.org"),
      upperCase: withRPID("NGC.example.org"),
      withPort: withRPID("ngc.example.org:8443"),
      atLimit: withRPID("a".repeat(253)),
      overLimit: withRPID("a".repeat(254)),
    };
  });
  expect(out).toEqual({
    empty: true, valid: true, scheme: false, upperCase: false, withPort: false,
    atLimit: true, overLimit: false,
  });
});

// Round 1 finding Wichtig 2: signedBudget borrowed sign.envelopeBudget's size,
// whose 1 KiB slack (ENVELOPE_SLACK) is sized for a plain envelope's kid+sig,
// not for a trust.Signed carrying a full proof.Proof. Go sizes it with 64 KiB
// of slack on purpose (shared/trust/trust.go:222, "plus 64 KiB for the proof
// (at most about 12 KiB)"). A maximal passkey proof alone is 12 290 base64
// characters (sig 1366 + authData 5462 + clientData 5462) - well past 1 KiB,
// well inside 64 KiB. No test called parseSigned at all before this one.
test("parseSigned leaves room for a maximal proof next to a payload at the cap", async ({ shell }) => {
  test.setTimeout(60_000);
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/trust.js");
    const { b64encode } = await import("/assets/js/contracts/b64.js");
    const MAX_PAYLOAD = 4 << 20;
    const doc = JSON.stringify({
      payload: b64encode(new Uint8Array(MAX_PAYLOAD)),
      proof: {
        kind: "passkey", kid: "AAAAAAAAAAAAAAAAAAAAAA",
        sig: b64encode(new Uint8Array(1024)),
        authData: b64encode(new Uint8Array(4096)),
        clientData: b64encode(new Uint8Array(4096)),
      },
    });
    const bytes = new TextEncoder().encode(doc);
    try {
      const s = t.parseSigned(bytes);
      return { threw: false, payloadLength: s.payload.length };
    } catch (e) {
      return { threw: true, message: String(e && e.message) };
    }
  });
  expect(out).toEqual({ threw: false, payloadLength: 4 << 20 });
});

// Round 1 findings Wichtig 3 and Wichtig 4: validateList, validAPINode and
// validStore are exported (trust.js's own interface list names the last two),
// so all three can be handed anything, not only what decode() already shaped
// through userKeyShape/nodeShape/storeShape. Three things pinned here:
//  (a) validateList never throws, not even on {} - it promises -> boolean,
//      and l.prev.length on an absent l.prev threw a TypeError before this
//      fix, regardless of every other field. This one check was still not
//      the whole promise: a null element inside an otherwise-real array
//      (isArray([null]) is true) threw further down, past every guard named
//      here, until Fixrunde 6 wrapped the whole function - see there.
//  (b) a same-length string does not stand in for Uint8Array on any of the
//      byte fields these paths check (noise, sign, tls, key) - the neighbor
//      modules (b64.js's equal, text.js's validText) check the type before
//      the length for exactly this reason, spelled out in their own
//      comments, and this task's own brief said so too.
//  (c) a key whose id is not a string (here: a bare {}) is refused on its
//      own, synchronously, in validateUserKeySync - not left to the
//      duplicate-key-id check (which would never catch it: two distinct {}
//      objects are never === to each other, so keys.has(k.id) would never
//      flag them as the same id) and not left to the id === keyID(pub) check
//      (which only runs later, asynchronously, and is unreachable through
//      parseList in the first place - userKeyShape already forces id to be a
//      string there; only the exported validateList can be handed one).
test("validateList and the exported node/store checks refuse wrong-typed bytes, and never throw", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/trust.js");
    const id = "EREREREREREREREREREREQ";
    const validKey = () => ({
      id: "AAAAAAAAAAAAAAAAAAAAAA", kind: "password", alg: "EdDSA",
      pub: new Uint8Array(32), credId: new Uint8Array(0),
      kdf: { alg: "argon2id", salt: new Uint8Array(16), m: 65536, t: 3, p: 1 },
    });
    const base = (extra) => ({
      seq: 1, prev: new Uint8Array(0), issued: 1, installation: id,
      rp: { id: "", origins: [] },
      users: [{ id, name: "admin", roles: ["admin"], keys: [validKey()] }],
      roles: [{ name: "admin", level: "critical", black: [], white: [], passkeyOnly: false, passwordToo: false }],
      nodes: [], stores: [], auto: [],
      ...extra,
    });
    const thirtyTwoCharString = "A".repeat(32); // same .length as 32 bytes, is not bytes
    const apiNode = (noise, sign, tls) => ({
      id: "api-1", kind: "api", name: "n", noise, sign, url: "wss://api-1.test:7443/tunnel", tls,
      rootUsers: [], rootRoles: [],
    });
    const store = (key) => ({ id: "store", name: "Store", url: "https://s.example", key });

    let emptyObjectThrew = false, emptyObjectResult;
    try {
      emptyObjectResult = t.validateList({});
    } catch {
      emptyObjectThrew = true;
    }

    return {
      emptyObjectThrew, emptyObjectResult,
      apiNodeRealBytes: t.validAPINode(apiNode(new Uint8Array(32), new Uint8Array(32), new Uint8Array(32))),
      apiNodeStringNoise: t.validAPINode(apiNode(thirtyTwoCharString, new Uint8Array(32), new Uint8Array(32))),
      apiNodeStringSign: t.validAPINode(apiNode(new Uint8Array(32), thirtyTwoCharString, new Uint8Array(32))),
      apiNodeStringTLS: t.validAPINode(apiNode(new Uint8Array(32), new Uint8Array(32), thirtyTwoCharString)),
      storeRealBytes: t.validStore(store(new Uint8Array(32))),
      storeStringKey: t.validStore(store(thirtyTwoCharString)),
      listStringNoiseOnController: t.validateList(base({
        nodes: [{ id: "ctl-1", kind: "controller", name: "n", noise: thirtyTwoCharString,
          sign: new Uint8Array(0), url: "", tls: new Uint8Array(0), rootUsers: [], rootRoles: [] }],
      })),
      listStringAutoKey: t.validateList(base({ auto: [{ target: "ngc-web", key: thirtyTwoCharString }] })),
      objectId: t.validateList(base({
        users: [{ id, name: "admin", roles: ["admin"], keys: [{ ...validKey(), id: {} }] }],
      })),
    };
  });
  expect(out).toEqual({
    emptyObjectThrew: false, emptyObjectResult: false,
    apiNodeRealBytes: true, apiNodeStringNoise: false, apiNodeStringSign: false, apiNodeStringTLS: false,
    storeRealBytes: true, storeStringKey: false,
    listStringNoiseOnController: false, listStringAutoKey: false,
    objectId: false,
  });
});

// Round 1 was told Wichtig 4 covers bytes fields; the coordinator's Fixrunde 1
// review widened it to every array-shaped field checked by .length without
// first confirming it is an array - rootUsers/rootRoles named directly, "und
// alles andere". A plain object with a length property and numeric keys
// (an "array-like") is not caught by a .length comparison at all, and is not
// caught downstream either wherever the real content at each index is itself
// genuinely valid - proven per field below, empirically, rather than assumed:
// some fields (nodes, stores, auto, users, roles - a JSON body can carry
// {"nodes":{"length":1,"0":{...}}} as easily as a real array) take a forged
// element and are wrongly ACCEPTED; fields only ever walked with for...of
// (rp.origins, a user's roles/keys, a controller's rootUsers/rootRoles) throw
// instead, because a plain object has no Symbol.iterator - still not `false`,
// still a violation of "-> boolean, never throws". validAPINode's own
// rootUsers/rootRoles check is length-only (n.rootUsers.length +
// n.rootRoles.length === 0, no loop), so {length: 0} alone wrongly accepts
// there. Also covers l.rp being absent entirely (not just its origins), a
// role's black/white the same way, and one step further found while tracing
// this same call graph: a black/white entry that is not a string at all
// (entries.every called .startsWith directly on each entry, with no typeof
// guard - Array.isArray(entries) alone does not stop a non-string element
// inside a real array).
test("validateList refuses wrong-typed lists on every array field, and never throws", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/trust.js");
    const id = "EREREREREREREREREREREQ";
    const validKey = () => ({
      id: "AAAAAAAAAAAAAAAAAAAAAA", kind: "password", alg: "EdDSA",
      pub: new Uint8Array(32), credId: new Uint8Array(0),
      kdf: { alg: "argon2id", salt: new Uint8Array(16), m: 65536, t: 3, p: 1 },
    });
    const validUser = () => ({ id, name: "admin", roles: ["admin"], keys: [validKey()] });
    const validRole = () => ({ name: "admin", level: "critical", black: [], white: [], passkeyOnly: false, passwordToo: false });
    const validAPI = () => ({
      id: "api-1", kind: "api", name: "n", noise: new Uint8Array(32), sign: new Uint8Array(32),
      url: "wss://api-1.test:7443/tunnel", tls: new Uint8Array(32), rootUsers: [], rootRoles: [],
    });
    const validStoreObj = () => ({ id: "store", name: "s", url: "https://s.example", key: new Uint8Array(32) });
    const validAuto = () => ({ target: "ngc-web", key: new Uint8Array(32) });
    const base = (extra) => ({
      seq: 1, prev: new Uint8Array(0), issued: 1, installation: id,
      rp: { id: "", origins: [] },
      users: [validUser()], roles: [validRole()], nodes: [], stores: [], auto: [],
      ...extra,
    });
    // An array-like: a plain object with a length property and numeric keys,
    // not an array - Array.isArray is false, [...x] throws (no iterator),
    // but x.length and x[i] both read exactly as they would on a real array.
    const arrayLike = (...items) => {
      const o = { length: items.length };
      items.forEach((v, i) => { o[i] = v; });
      return o;
    };
    // Every case below is run through this: catches a throw as its own
    // outcome (threw: true) rather than letting one throw take out every
    // other property in the same object literal, and records whether the
    // call, if it returned at all, said true or false.
    const safe = (fn) => { try { return { threw: false, value: fn() }; } catch { return { threw: true, value: null }; } };

    const withController = (rootUsers, rootRoles) => [{
      id: "ctl-1", kind: "controller", name: "n", noise: new Uint8Array(32),
      sign: new Uint8Array(0), url: "", tls: new Uint8Array(0), rootUsers, rootRoles,
    }];

    return {
      users: safe(() => t.validateList({ ...base(), users: arrayLike(validUser()) })),
      roles: safe(() => t.validateList({ ...base(), roles: arrayLike(validRole()) })),
      nodes: safe(() => t.validateList({ ...base(), nodes: arrayLike(validAPI()) })),
      stores: safe(() => t.validateList({ ...base(), stores: arrayLike(validStoreObj()) })),
      auto: safe(() => t.validateList({ ...base(), auto: arrayLike(validAuto()) })),
      origins: safe(() => t.validateList({ ...base(), rp: { id: "", origins: arrayLike("https://a.example") } })),
      rpMissing: safe(() => t.validateList({ ...base(), rp: undefined })),

      controllerRootUsers: safe(() => t.validateList(base({ nodes: withController(arrayLike(id), []) }))),
      controllerRootRoles: safe(() => t.validateList(base({ nodes: withController([], arrayLike("admin")) }))),
      apiNodeRootUsers: safe(() => t.validAPINode({ ...validAPI(), rootUsers: arrayLike(), rootRoles: [] })),
      apiNodeRootRoles: safe(() => t.validAPINode({ ...validAPI(), rootUsers: [], rootRoles: arrayLike() })),

      userRoles: safe(() => t.validateList(base({ users: [{ ...validUser(), roles: arrayLike("admin") }] }))),
      userKeys: safe(() => t.validateList(base({ users: [{ ...validUser(), keys: arrayLike(validKey()) }] }))),

      roleBlack: safe(() => t.validateList(base({ roles: [{ ...validRole(), black: arrayLike("node/x") }] }))),
      roleWhite: safe(() => t.validateList(base({ roles: [{ ...validRole(), white: arrayLike("node/x") }] }))),
      roleBlackNonStringEntry: safe(() => t.validateList(base({ roles: [{ ...validRole(), black: [42] }] }))),
    };
  });
  const flat = Object.fromEntries(Object.entries(out).map(([k, v]) => [k, v.threw ? "threw" : v.value]));
  expect(flat).toEqual({
    users: false, roles: false, nodes: false, stores: false, auto: false, origins: false, rpMissing: false,
    controllerRootUsers: false, controllerRootRoles: false,
    apiNodeRootUsers: false, apiNodeRootRoles: false,
    userRoles: false, userKeys: false,
    roleBlack: false, roleWhite: false, roleBlackNonStringEntry: false,
  });
});

// Fixrunde 3, point 2: mayRoot, matches (private, exercised through
// needsConfirm) and needsConfirm have the identical shape of gap as the
// validateList family just closed - an array-only method (.includes, .some)
// called on a list-typed argument the caller controls, with no
// Array.isArray first. Flagged in the Fixrunde 2 report rather than folded
// in unasked; now closed on the coordinator's explicit instruction. An
// array-like plain object ({length, 0, 1, ...}) throws at each of these
// (none of the three has a length-only fast path the way validAPINode's
// rootUsers/rootRoles sum did, so every case here is a throw, not a silent
// wrong accept - verified empirically below, not assumed from the Wichtig 4
// pattern). needsConfirm's safe default for a malformed u.roles is `true`
// (needs confirming), matching its own existing "unknown user always
// confirms" rule - a malformed value is exactly the kind of doubt that rule
// already exists for. matches's safe default is `false` (no match), which is
// the safe direction on both of its call sites: a malformed r.black no
// longer forces a confirm by accident, but does not skip it either (falls
// through to the level check); a malformed r.white no longer grants a
// bypass by accident either.
test("mayRoot, matches and needsConfirm refuse array-like lists instead of throwing", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/trust.js");
    const adminId = "EREREREREREREREREREREQ";
    const key = () => ({
      id: "AAAAAAAAAAAAAAAAAAAAAA", kind: "password", alg: "EdDSA",
      pub: new Uint8Array(32), credId: new Uint8Array(0),
      kdf: { alg: "argon2id", salt: new Uint8Array(16), m: 65536, t: 3, p: 1 },
    });
    const arrayLike = (...items) => {
      const o = { length: items.length };
      items.forEach((v, i) => { o[i] = v; });
      return o;
    };
    const safe = (fn) => { try { return { threw: false, value: fn() }; } catch { return { threw: true, value: null }; } };

    const list = (userRoles, roleWhite) => ({
      seq: 1, prev: new Uint8Array(0), issued: 1, installation: adminId,
      rp: { id: "", origins: [] },
      users: [{ id: adminId, name: "admin", roles: userRoles, keys: [key()] }],
      roles: [
        { name: "admin", level: "critical", black: [], white: [], passkeyOnly: false, passwordToo: false },
        { name: "white", level: "critical", black: [], white: roleWhite, passkeyOnly: false, passwordToo: false },
      ],
      nodes: [{ id: "ctl-1", kind: "controller", name: "n", noise: new Uint8Array(32), sign: new Uint8Array(0),
        url: "", tls: new Uint8Array(0), rootUsers: [adminId], rootRoles: ["admin"] }],
      stores: [], auto: [],
    });
    const withControllerRoots = (rootUsers, rootRoles) => ({
      ...list(["admin"], []),
      nodes: [{ id: "ctl-1", kind: "controller", name: "n", noise: new Uint8Array(32), sign: new Uint8Array(0),
        url: "", tls: new Uint8Array(0), rootUsers, rootRoles }],
    });
    // rootUsers deliberately does NOT list adminId here: mayRoot's first
    // check (n.rootUsers.includes(userID)) must fail so the call actually
    // reaches u.roles.some(...), the line under test - with the list()
    // default (rootUsers: [adminId]) the first check alone would return
    // true and the malformed u.roles would never be read at all.
    const userRolesCase = (userRoles) => ({
      ...list(userRoles, []),
      nodes: [{ id: "ctl-1", kind: "controller", name: "n", noise: new Uint8Array(32), sign: new Uint8Array(0),
        url: "", tls: new Uint8Array(0), rootUsers: [], rootRoles: ["admin"] }],
    });

    return {
      mayRootRootUsers: safe(() => t.mayRoot(withControllerRoots(arrayLike(adminId), []), adminId, "ctl-1")),
      mayRootRootRoles: safe(() => t.mayRoot(withControllerRoots([], arrayLike("admin")), adminId, "ctl-1")),
      mayRootUserRoles: safe(() => t.mayRoot(userRolesCase(arrayLike("admin")), adminId, "ctl-1")),
      needsConfirmUserRoles: safe(() => t.needsConfirm(list(arrayLike("admin"), []), adminId, { app: "a", action: "b" })),
      needsConfirmWhite: safe(() => t.needsConfirm(
        { ...list(["white"], arrayLike("app/a")), users: [{ id: adminId, name: "admin", roles: ["white"], keys: [key()] }] },
        adminId, { app: "a", action: "b", critical: true },
      )),
    };
  });
  const flat = Object.fromEntries(Object.entries(out).map(([k, v]) => [k, v.threw ? "threw" : v.value]));
  expect(flat).toEqual({
    mayRootRootUsers: false, mayRootRootRoles: false, mayRootUserRoles: false,
    needsConfirmUserRoles: true, // malformed roles: doubt, same direction as "unknown user"
    needsConfirmWhite: true, // malformed white-list must not excuse a critical op either
  });
});

// Fixrunde 4: the coordinator named user, role, node, keyOwner and
// passwordToo as the same gap a third time - exported, an array-only method
// (.find, for...of, .some) on a caller-supplied list, no Array.isArray
// first. Closed here, all five at once, not in slices. user/role/node
// answer null on a malformed list, matching what they already answer for
// "not found" - a malformed list is exactly that kind of not-found. keyOwner
// answers null the same way for a malformed l.users, but for one user's own
// malformed keys specifically it skips just that user (continue) rather
// than aborting the whole search: a bad entry for one user must not hide a
// real key that a different, well-formed user actually owns - proven below
// by two users in one list, only the second one findable.
// passwordToo is the one case that is NOT "false/null is the safe answer":
// true here means the login flow demands a password on top of a passkey
// (Go: session.VerifyClient already requires the passkey either way;
// PasswordToo only ever adds to that, never replaces it), so answering
// false on doubt would silently accept a weaker login than Go would have
// required - the opposite direction from mayRoot/matches/needsConfirm, where
// refusing was the safe answer. passwordToo therefore answers true (require
// the extra factor) when u.roles is not a proper array.
test("user, role, node, keyOwner and passwordToo refuse array-like lists instead of throwing", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/trust.js");
    const adminId = "EREREREREREREREREREREQ";
    const otherId = "AAAAAAAAAAAAAAAAAAAAAA";
    const key = (kid) => ({
      id: kid, kind: "password", alg: "EdDSA", pub: new Uint8Array(32), credId: new Uint8Array(0),
      kdf: { alg: "argon2id", salt: new Uint8Array(16), m: 65536, t: 3, p: 1 },
    });
    const arrayLike = (...items) => {
      const o = { length: items.length };
      items.forEach((v, i) => { o[i] = v; });
      return o;
    };
    const safe = (fn) => { try { return { threw: false, value: fn() }; } catch { return { threw: true, value: null }; } };
    const realKid = "gICAgICAgICAgICAgICAgA";

    const listWith = (extra) => ({
      seq: 1, prev: new Uint8Array(0), issued: 1, installation: adminId,
      rp: { id: "", origins: [] },
      users: [{ id: adminId, name: "admin", roles: ["admin"], keys: [key("AAAAAAAAAAAAAAAAAAAAAA")] }],
      roles: [{ name: "admin", level: "critical", black: [], white: [], passkeyOnly: false, passwordToo: false }],
      nodes: [{ id: "ctl-1", kind: "controller", name: "n", noise: new Uint8Array(32), sign: new Uint8Array(0),
        url: "", tls: new Uint8Array(0), rootUsers: [], rootRoles: [] }],
      stores: [], auto: [],
      ...extra,
    });

    return {
      userUsersArrayLike: safe(() => t.user(listWith({ users: arrayLike({ id: adminId, roles: ["admin"], keys: [] }) }), adminId)),
      roleRolesArrayLike: safe(() => t.role(listWith({ roles: arrayLike({ name: "admin", level: "critical" }) }), "admin")),
      nodeNodesArrayLike: safe(() => t.node(listWith({ nodes: arrayLike({ id: "ctl-1", kind: "controller" }) }), "ctl-1")),
      keyOwnerUsersArrayLike: safe(() => t.keyOwner(listWith({ users: arrayLike({ id: adminId, keys: [key(realKid)] }) }), realKid)),
      // one malformed user (keys as an array-like), one real user who owns
      // the searched key: keyOwner must skip the first, not abort, and
      // still find the second. Reduced to the two ids that matter, not the
      // whole {user, key} shape - Uint8Array fields do not round-trip
      // through evaluate()'s JSON boundary as Uint8Array, so comparing the
      // full objects here would test the serialization, not keyOwner.
      keyOwnerSkipsOneBadUser: safe(() => {
        const found = t.keyOwner(listWith({
          users: [
            { id: adminId, name: "admin", roles: ["admin"], keys: arrayLike(key("AAAAAAAAAAAAAAAAAAAAAA")) },
            { id: otherId, name: "other", roles: ["admin"], keys: [key(realKid)] },
          ],
        }), realKid);
        return found && { userId: found.user.id, keyId: found.key.id };
      }),
      passwordTooUserRolesArrayLike: safe(() => t.passwordToo(listWith({
        users: [{ id: adminId, name: "admin", roles: arrayLike("admin"), keys: [key("AAAAAAAAAAAAAAAAAAAAAA")] }],
      }), adminId)),
    };
  });
  const flat = Object.fromEntries(Object.entries(out).map(([k, v]) => [k, v.threw ? "threw" : v.value]));
  expect(flat).toEqual({
    userUsersArrayLike: null,
    roleRolesArrayLike: null,
    nodeNodesArrayLike: null,
    keyOwnerUsersArrayLike: null,
    keyOwnerSkipsOneBadUser: { userId: "AAAAAAAAAAAAAAAAAAAAAA", keyId: "gICAgICAgICAgICAgICAgA" },
    passwordTooUserRolesArrayLike: true,
  });
});

// Fixrunde 5: encodeList closes the same criterion once more - exported,
// takes caller data ("dass die Liste normalerweise aus parseList kommt,
// hilft nicht: exportiert heisst, jemand ruft es anders auf" - the
// coordinator's decision). It has no natural false/null to answer, though:
// its job is to produce bytes, not decide or look something up, so unlike
// every function fixed so far in this task there is no single safe fallback
// value. What it can promise instead is what parseList/parseSigned already
// promise on their own failures: one controlled, named error instead of
// whatever uncontrolled exception happens to surface from deep inside
// l.users.map/encodeUser/encodeUserKey's own field reads, at whatever depth
// the first malformed field sits at. Proven at two depths below, not just
// the top level: a top-level field (users, an array-like) and a byte field
// nested two calls down (a user's own key's credId).
test("encodeList answers one controlled error instead of an arbitrary one", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/trust.js");
    const adminId = "EREREREREREREREREREREQ";
    const key = () => ({
      id: "AAAAAAAAAAAAAAAAAAAAAA", kind: "password", alg: "EdDSA",
      pub: new Uint8Array(32), credId: new Uint8Array(0),
      kdf: { alg: "argon2id", salt: new Uint8Array(16), m: 65536, t: 3, p: 1 },
    });
    const arrayLike = (...items) => {
      const o = { length: items.length };
      items.forEach((v, i) => { o[i] = v; });
      return o;
    };
    const safe = (fn) => { try { fn(); return { threw: false, message: null }; } catch (e) { return { threw: true, message: String(e && e.message) }; } };
    const list = (extra) => ({
      seq: 1, prev: new Uint8Array(0), issued: 1, installation: adminId,
      rp: { id: "", origins: [] }, users: [{ id: adminId, name: "admin", roles: ["admin"], keys: [key()] }],
      roles: [{ name: "admin", level: "critical" }], nodes: [], stores: [], auto: [],
      ...extra,
    });

    return {
      topLevelUsers: safe(() => t.encodeList(list({ users: arrayLike(key()) }))),
      nestedCredId: safe(() => t.encodeList(list({
        users: [{ id: adminId, name: "admin", roles: ["admin"], keys: [{ ...key(), credId: arrayLike(1) }] }],
      }))),
    };
  });
  expect(out.topLevelUsers).toEqual({ threw: true, message: "trust: cannot encode this list" });
  expect(out.nestedCredId).toEqual({ threw: true, message: "trust: cannot encode this list" });
});

// Fixrunde 6: the tenth appearance of this class in this task, found by the
// closing review measuring trust.js's own "Never throws" comment on
// validateList against what the code actually does. isArray(l.users) is
// true for l.users = [null]: the array itself is real, Array.isArray only
// ever asks about the array, never its contents, so an element that is
// itself null (or any other non-object) still throws two lines later
// wherever that element is read as if it were a real user/role/node/store/
// auto entry - and the same shape of gap sits in validAPINode(null),
// validStore(null/undefined) (n/s itself is null, not a field of it) and
// keyOwner (a user's own keys containing null). For the elements inside a
// list, no isArray/isBytes guard closes this - null is exactly as much an
// object as a real one is, by Array.isArray's own accounting - so the fix
// there is the one sign.js's own validateEnvelope already uses for the
// identical reason, quoted in its own comment: "An exception is no answer,
// and a check that has none refuses." For the two arguments that are
// themselves null or missing it is the other way round, which Fixrunde 7/K1
// corrected: sign.js guards those in front of its try, and validAPINode and
// validStore now do too, so apiNodeNull/storeNull/storeUndefined below are
// answered by that one-line guard and never reach the try. The two getter
// cases are what is left for the try alone - a real object whose field
// throws on being read, which no guard in front of it can see.
//
// The title said "never throw" until Fixrunde 7 and no longer does, because
// of keyOwner: its try now sits inside the user loop, so the one read in
// front of that loop (l?.users, and iterating it) is as unguarded as the
// same read in user/role/node, which never had a try at all. Measured: a
// list whose users getter throws throws out of all four alike. What this
// test pins for keyOwner is what its name now says and what W1 was about -
// a null element answers instead of throwing - not a blanket promise.
test("validateList, validAPINode, validStore and keyOwner do not throw on a null element", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/trust.js");
    const id = "EREREREREREREREREREREQ";
    const validKey = () => ({
      id: "AAAAAAAAAAAAAAAAAAAAAA", kind: "password", alg: "EdDSA",
      pub: new Uint8Array(32), credId: new Uint8Array(0),
      kdf: { alg: "argon2id", salt: new Uint8Array(16), m: 65536, t: 3, p: 1 },
    });
    const base = (extra) => ({
      seq: 1, prev: new Uint8Array(0), issued: 1, installation: id,
      rp: { id: "", origins: [] },
      users: [{ id, name: "admin", roles: ["admin"], keys: [validKey()] }],
      roles: [{ name: "admin", level: "critical", black: [], white: [], passkeyOnly: false, passwordToo: false }],
      nodes: [], stores: [], auto: [],
      ...extra,
    });
    const safe = (fn) => { try { return { threw: false, value: fn() }; } catch { return { threw: true, value: null }; } };

    return {
      usersNull: safe(() => t.validateList(base({ users: [null] }))),
      rolesNull: safe(() => t.validateList(base({ roles: [null] }))),
      nodesNull: safe(() => t.validateList(base({ nodes: [null] }))),
      storesNull: safe(() => t.validateList(base({ stores: [null] }))),
      autoNull: safe(() => t.validateList(base({ auto: [null] }))),
      apiNodeNull: safe(() => t.validAPINode(null)),
      storeNull: safe(() => t.validStore(null)),
      storeUndefined: safe(() => t.validStore(undefined)),
      apiNodeGetterThrows: safe(() => t.validAPINode({ get kind() { throw new Error("boom"); } })),
      storeGetterThrows: safe(() => t.validStore({ get id() { throw new Error("boom"); } })),
      keyOwnerKeysNull: safe(() => t.keyOwner(base({
        users: [{ id, name: "admin", roles: ["admin"], keys: [null] }],
      }), "AAAAAAAAAAAAAAAAAAAAAA")),
    };
  });
  const flat = Object.fromEntries(Object.entries(out).map(([k, v]) => [k, v.threw ? "threw" : v.value]));
  expect(flat).toEqual({
    usersNull: false, rolesNull: false, nodesNull: false, storesNull: false, autoNull: false,
    apiNodeNull: false, storeNull: false, storeUndefined: false,
    apiNodeGetterThrows: false, storeGetterThrows: false,
    keyOwnerKeysNull: null,
  });
});

// Fixrunde 7: the eleventh appearance of this class, and the first one made
// by the commit that closed the tenth. Fixrunde 6 wrapped keyOwner's whole
// body in one try, which turned the "skip just that user, not the search"
// line inside it into its own opposite: a user whose keys hold a null now
// ended the search, so a real key a later, well-formed user owns came back
// as "not found". Fixrunde 4 had proven and pinned that skipping for the
// array-like shape (keyOwnerSkipsOneBadUser above) - but the null-element
// case that came with Fixrunde 6 has only one user, and with nothing left to
// find, aborting and skipping both answer null. Two users is the only shape
// that tells them apart: the broken one first, the owner of the searched key
// second. onlyBroken below is the one-user case next to it, answering null
// either way, so the difference between the two is on record and not just
// asserted in prose.
test("keyOwner skips a user whose keys hold a null and still finds a later user's key", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/trust.js");
    const brokenId = "EREREREREREREREREREREQ", goodId = "AAAAAAAAAAAAAAAAAAAAAA";
    const realKid = "gICAgICAgICAgICAgICAgA";
    const key = (kid) => ({
      id: kid, kind: "password", alg: "EdDSA", pub: new Uint8Array(32), credId: new Uint8Array(0),
      kdf: { alg: "argon2id", salt: new Uint8Array(16), m: 65536, t: 3, p: 1 },
    });
    const listWith = (users) => ({
      seq: 1, prev: new Uint8Array(0), issued: 1, installation: brokenId,
      rp: { id: "", origins: [] }, users,
      roles: [{ name: "admin", level: "critical", black: [], white: [], passkeyOnly: false, passwordToo: false }],
      nodes: [], stores: [], auto: [],
    });
    const safe = (fn) => { try { return { threw: false, value: fn() }; } catch { return { threw: true, value: null }; } };
    // Reduced to the two ids that matter, not the whole {user, key} shape,
    // for the same reason keyOwnerSkipsOneBadUser above is: Uint8Array
    // fields do not round-trip through evaluate()'s JSON boundary as
    // Uint8Array, so comparing the full objects would test that boundary.
    const find = (users) => safe(() => {
      const found = t.keyOwner(listWith(users), realKid);
      return found && { userId: found.user.id, keyId: found.key.id };
    });
    const broken = { id: brokenId, name: "broken", roles: ["admin"], keys: [null] };
    const good = { id: goodId, name: "good", roles: ["admin"], keys: [key(realKid)] };
    return { brokenFirst: find([broken, good]), onlyBroken: find([broken]) };
  });
  const flat = Object.fromEntries(Object.entries(out).map(([k, v]) => [k, v.threw ? "threw" : v.value]));
  expect(flat).toEqual({
    brokenFirst: { userId: "AAAAAAAAAAAAAAAAAAAAAA", keyId: "gICAgICAgICAgICAgICAgA" },
    onlyBroken: null,
  });
});

// Round 1 finding Wichtig 6: passwordToo, mayRoot, needsConfirm, keyOwner and
// tlsPin had no test calling them - the previous report described their
// doubt-case behavior (unknown user/role/node grant nothing) by reading the
// code next to Go, never by running it. Pinned here. Also K2: a white-list
// entry "node/undefined" is a well-formed entry (validName("undefined") is
// true), and matches's old `op.node !== ""` treated a caller's forgotten
// op.node (undefined) as this literal text via "node/" + undefined -
// excusing an operation that was never about any node. op.node ?? "" closes
// it, matching Go's zero value "". The role is level "critical" and the op
// is critical on purpose: at level "off" the correct and the buggy path both
// end in false (the fallthrough default and the wrongful white-list match
// agree by coincidence), which would make this case pass whether or not the
// bug is fixed. At "critical", the correct path (no white-list match, fall
// through to the level check, op.critical is true) and the buggy path (an
// accidental match excuses it) disagree - true vs. false - so this only goes
// green once matches actually stops matching on a node nobody named.
test("the decision functions grant nothing on doubt, matching Go", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/trust.js");
    const adminId = "EREREREREREREREREREREQ";
    const key = () => ({
      id: "AAAAAAAAAAAAAAAAAAAAAA", kind: "password", alg: "EdDSA",
      pub: new Uint8Array(32), credId: new Uint8Array(0),
      kdf: { alg: "argon2id", salt: new Uint8Array(16), m: 65536, t: 3, p: 1 },
    });
    const list = {
      seq: 1, prev: new Uint8Array(0), issued: 1, installation: adminId,
      rp: { id: "", origins: [] },
      users: [{ id: adminId, name: "admin", roles: ["admin"], keys: [key()] }],
      roles: [
        { name: "admin", level: "off", black: [], white: [], passkeyOnly: false, passwordToo: false },
        { name: "white", level: "critical", black: [], white: ["node/undefined"], passkeyOnly: false, passwordToo: false },
      ],
      nodes: [
        { id: "api-1", kind: "api", name: "n", noise: new Uint8Array(32), sign: new Uint8Array(32),
          url: "wss://api-1.test:7443/tunnel", tls: new Uint8Array(32), rootUsers: [], rootRoles: [] },
        { id: "ctl-1", kind: "controller", name: "n", noise: new Uint8Array(32), sign: new Uint8Array(0),
          url: "", tls: new Uint8Array(0), rootUsers: [adminId], rootRoles: [] },
      ],
      stores: [], auto: [],
    };
    const unknownUser = "ISEhISEhISEhISEhISEhIQ";
    const listWithGhostRole = { ...list,
      users: [{ id: adminId, name: "admin", roles: ["ghost"], keys: [key()] }] };
    const listWhiteUser = { ...list,
      users: [{ id: adminId, name: "admin", roles: ["white"], keys: [key()] }] };

    return {
      needsConfirmUnknownUser: t.needsConfirm(list, unknownUser, { app: "a", action: "b", node: "" }),
      needsConfirmUnknownRole: t.needsConfirm(listWithGhostRole, adminId, { app: "a", action: "b", node: "" }),
      mayRootUnknownNode: t.mayRoot(list, adminId, "ghost-node"),
      mayRootUnknownUser: t.mayRoot(list, unknownUser, "ctl-1"),
      mayRootNotController: t.mayRoot(list, adminId, "api-1"),
      mayRootRealController: t.mayRoot(list, adminId, "ctl-1"),
      passwordTooUnknownUser: t.passwordToo(list, unknownUser),
      keyOwnerEmptyList: t.keyOwner({ ...list, users: [] }, "AAAAAAAAAAAAAAAAAAAAAA"),
      // K2: node deliberately left off op, like a caller might forget it.
      needsConfirmNodelessOp: t.needsConfirm(listWhiteUser, adminId, { app: "zzz", action: "zzz", critical: true }),
    };
  });
  expect(out).toEqual({
    needsConfirmUnknownUser: true,
    needsConfirmUnknownRole: true,
    mayRootUnknownNode: false,
    mayRootUnknownUser: false,
    mayRootNotController: false,
    mayRootRealController: true,
    passwordTooUnknownUser: false,
    keyOwnerEmptyList: null,
    needsConfirmNodelessOp: true,
  });
});

// Round 1 finding Wichtig 6 (continued): tlsPin and encodeList had no test
// either. tlsPin is checked against a direct digest of the same input, twice,
// for the same answer. encodeList is checked byte for byte against Go's
// field order and omitempty (shared/trust/trust.go's struct tags): prev
// absent at seq 1, a role without white/black omitting both, a role with one
// white entry keeping only that one, an API node with every optional byte
// field present, a controller with them all empty and so omitted, one store,
// one auto entry.
test("tlsPin hashes deterministically and encodeList matches Go's field order and omitempty", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/trust.js");
    const { equal, b64encode } = await import("/assets/js/contracts/b64.js");
    const spki = new Uint8Array(20).fill(7);
    const pinA = await t.tlsPin(spki);
    const pinB = await t.tlsPin(spki);
    const direct = new Uint8Array(await crypto.subtle.digest("SHA-256", spki));

    const adminId = "EREREREREREREREREREREQ";
    const list = {
      seq: 1, prev: new Uint8Array(0), issued: 1, installation: adminId,
      rp: { id: "", origins: [] },
      users: [{
        id: adminId, name: "admin", roles: ["admin"],
        keys: [{ id: "AAAAAAAAAAAAAAAAAAAAAA", kind: "password", alg: "EdDSA",
          pub: new Uint8Array(32), credId: new Uint8Array(0),
          kdf: { alg: "argon2id", salt: new Uint8Array(16), m: 65536, t: 3, p: 1 } }],
      }],
      roles: [
        { name: "admin", level: "off", black: [], white: [], passkeyOnly: false, passwordToo: false },
        { name: "white", level: "off", black: [], white: ["node/undefined"], passkeyOnly: false, passwordToo: false },
      ],
      nodes: [
        { id: "api-1", kind: "api", name: "n", noise: new Uint8Array(32), sign: new Uint8Array(32),
          url: "wss://api-1.test:7443/tunnel", tls: new Uint8Array(32), rootUsers: [], rootRoles: [] },
        { id: "ctl-1", kind: "controller", name: "n", noise: new Uint8Array(32), sign: new Uint8Array(0),
          url: "", tls: new Uint8Array(0), rootUsers: [adminId], rootRoles: [] },
      ],
      stores: [{ id: "global", name: "NGC Store", url: "https://store.example", key: new Uint8Array(32) }],
      auto: [{ target: "system", key: new Uint8Array(0) }],
    };
    const expected = {
      seq: 1, issued: 1, installation: adminId,
      rp: { id: "" },
      users: [{
        id: adminId, name: "admin", roles: ["admin"],
        keys: [{ id: "AAAAAAAAAAAAAAAAAAAAAA", kind: "password", alg: "EdDSA",
          pub: b64encode(new Uint8Array(32)),
          kdf: { alg: "argon2id", salt: b64encode(new Uint8Array(16)), m: 65536, t: 3, p: 1 } }],
      }],
      roles: [
        { name: "admin", level: "off" },
        { name: "white", level: "off", white: ["node/undefined"] },
      ],
      nodes: [
        { id: "api-1", kind: "api", name: "n", noise: b64encode(new Uint8Array(32)),
          sign: b64encode(new Uint8Array(32)), url: "wss://api-1.test:7443/tunnel",
          tls: b64encode(new Uint8Array(32)) },
        { id: "ctl-1", kind: "controller", name: "n", noise: b64encode(new Uint8Array(32)),
          rootUsers: [adminId] },
      ],
      stores: [{ id: "global", name: "NGC Store", url: "https://store.example", key: b64encode(new Uint8Array(32)) }],
      auto: [{ target: "system" }],
    };
    const encoded = new TextDecoder().decode(t.encodeList(list));
    return {
      tlsPinRepeats: equal(pinA, pinB), tlsPinMatchesDigest: equal(pinA, direct),
      encodedMatches: encoded === JSON.stringify(expected),
      encoded, expected: JSON.stringify(expected),
    };
  });
  expect(out.tlsPinRepeats).toBe(true);
  expect(out.tlsPinMatchesDigest).toBe(true);
  expect(out.encoded).toBe(out.expected);
});
