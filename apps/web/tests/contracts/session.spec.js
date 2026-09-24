import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("session.json");

// Does the Web Crypto API of this engine know Ed25519? Chromium here does,
// and proof.spec.js says the same of Chrome, Safari 17 and Firefox 130; the
// WebKit that Playwright brings answers NotSupportedError to importKey -
// measured for this list's own keys, for "raw" and for "spki" alike. The
// first test needs both: trust.parseList imports every passkey of this list
// as spki (proof.importPublic), and sign.verify imports the node's signing
// key as raw. Without them parseList refuses the list before the first
// certificate is looked at, and no right signature can come out true - which
// is the right answer for an engine that cannot check, and no test of this
// contract (measured there: "trust: invalid list" from the first line, and
// "session: bad signature" from a hand-built list that gets past it). The
// four tests below it need no Ed25519 and run on every engine.
//
// Probed with a real key out of this vector's own list, as trust.spec.js and
// proof.spec.js probe: an engine that has Ed25519 but rejects a made-up
// 32-byte value would otherwise be skipped for the wrong reason.
async function hasEd25519(shell, signed) {
  return shell.evaluate(async (s) => {
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const list = JSON.parse(new TextDecoder().decode(b64decode(s.payload)));
    let pub = null;
    for (const u of list.users) {
      for (const k of u.keys) {
        if (k.kind === "password") pub = k.pub;
      }
    }
    // Outside the try on purpose: without a password key in this list,
    // b64decode(null) would land in the catch below and skip both tests as
    // "no Ed25519 here", on an engine that has it. A vector file that loses
    // its password key must fail loudly instead (Fixrunde 1).
    if (typeof pub !== "string") throw new Error("session.json's trust list holds no password key to probe with");
    try {
      await crypto.subtle.importKey("raw", b64decode(pub), { name: "Ed25519" }, false, ["verify"]);
      return true;
    } catch {
      return false;
    }
  }, signed);
}

test("the API certificate is checked against the trust list", async ({ shell }) => {
  test.setTimeout(120_000);
  test.skip(!(await hasEd25519(shell, v.trust)),
    "no Ed25519 in this engine's Web Crypto: it reads none of this list's passkeys and checks no signature");
  const got = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/session.js");
    const t = await import("/assets/js/contracts/trust.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const list = await t.parseList(b64decode(d.trust.payload));
    const client = b64decode(d.client);
    const env = (e) => ({ payload: b64decode(e.payload), kid: e.kid, sig: b64decode(e.sig) });
    const check = async (e) => {
      try {
        await s.verifyAPI(env(e), list, client, d.now);
        return "accepted";
      } catch (err) {
        return err.message;
      }
    };
    return { good: await check(d.api), rejects: await Promise.all(d.apiRejects.map((r) => check(r.api))) };
  }, v);
  expect(got.good).toBe("accepted");
  // Fixrunde 1, on the coordinator's decision against his own brief: each
  // case is held to the reason, not only to being refused - one that fails
  // earlier than it is meant to would go unnoticed otherwise. Go has three
  // here: ErrCert for a key no node of the list has, a payload that does not
  // read and every field that does not match; the signature's own error; and
  // ErrExpired for the time.
  const cert = "session: malformed certificate";
  const reason = {
    "signed by a key that is no API node": cert,
    "signed for another purpose": "session: bad signature",
    "other installation": cert,
    "other node name": cert,
    "bound to another client certificate": cert,
    "DH key of 31 bytes": cert,
    "longer than MaxLifetime": cert,
    "outlives the client certificate": cert,
    "expired at now": "session: certificate not valid at this time",
    "not yet valid at now": "session: certificate not valid at this time",
  };
  expect(got.rejects).toEqual(v.apiRejects.map((r) => reason[r.name]));
});

test("every login challenge is decided as Go decides it", async ({ shell }) => {
  test.setTimeout(120_000);
  const got = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/session.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of d.cases) {
      try {
        s.parseLoginChallenge(b64decode(c.challenge));
        out.push(true);
      } catch {
        out.push(false);
      }
    }
    // Every answer session.Challenge builds must pass, the made-up ones for
    // unknown names too: an answer the shell refuses would tell it that the
    // name does not exist.
    const built = [];
    for (const b of d.built) {
      const c = s.parseLoginChallenge(b64decode(b.challenge));
      built.push(c.passkeys.every((p) => p.credId.length > 0 && p.kid.length === 22));
    }
    return { out, built };
  }, v.login);
  const wrong = v.login.cases.filter((c, i) => c.valid !== got.out[i]).map((c) => c.name);
  expect(wrong).toEqual([]);
  expect(got.built).toEqual(v.login.built.map(() => true));
});

test("the one-time code is normalized as in Go", async ({ shell }) => {
  const got = await shell.evaluate(async (typed) => {
    const s = await import("/assets/js/contracts/session.js");
    return typed.map((c) => s.normalizeSetupCode(c.typed) ?? "");
  }, v.setup.typed);
  expect(got).toEqual(v.setup.typed.map((c) => c.code));
});

test("every setup offer is decided as Go decides it", async ({ shell }) => {
  test.setTimeout(120_000);
  const got = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/session.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of d.offers) {
      try {
        await s.parseSetupOffer(b64decode(c.offer), d.code);
        out.push(true);
      } catch {
        out.push(false);
      }
    }
    return out;
  }, v.setup);
  const wrong = v.setup.offers.filter((c, i) => c.valid !== got[i]).map((c) => c.name);
  expect(wrong).toEqual([]);
});

test("the version 1 the shell builds is the one the API accepts", async ({ shell }) => {
  test.setTimeout(120_000);
  const got = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/session.js");
    const { b64decode, b64encode, equal } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of d.requests) {
      const req = JSON.parse(new TextDecoder().decode(b64decode(c.request)));
      // The MAC covers the exact bytes of the payload the shell sends.
      const mac = await s.setupMAC(d.code, b64decode(req.trust.payload));
      out.push(equal(mac, b64decode(req.mac)));
    }
    return out;
  }, v.setup);
  // Only the two valid requests carry a MAC made with this code over these
  // bytes; the others differ by design.
  expect(got).toEqual(v.setup.requests.map((c) => c.name === "MAC with another code"
    || c.name === "MAC over other bytes" ? false : true));
});

// Go's VerifyAPI marks "no node signed this" with the zero trust.Node and asks
// afterwards `if node.ID == ""`, so an API node whose ID is empty is no node
// there either - it is refused even when its key made the signature. A list
// that passed trust.validateList holds no such node (validName refuses ""),
// but verifyAPI is exported and takes the list its caller hands it, so the
// two have to answer that list alike. No vector carries this case: the
// certificate is signed here, with a key of this page, so that the signature
// really checks out and the node is the only thing left that can refuse it.
test("an API node without an ID is no node, as in Go", async ({ shell }) => {
  test.setTimeout(120_000);
  test.skip(!(await hasEd25519(shell, v.trust)),
    "no Ed25519 in this engine's Web Crypto: it signs nothing and checks nothing");
  const got = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/session.js");
    const sign = await import("/assets/js/contracts/sign.js");
    const proof = await import("/assets/js/contracts/proof.js");
    const { b64decode, b64encode, utf8 } = await import("/assets/js/contracts/b64.js");
    const seed = new Uint8Array(32).fill(7);
    const priv = await proof.privateFromSeed(seed);
    const pub = await proof.publicFromSeed(seed);
    const client = b64decode(d.client);
    const cc = JSON.parse(new TextDecoder().decode(client));
    // Everything about this certificate is right but the node it names.
    const payload = utf8(JSON.stringify({
      inst: cc.inst, node: "", client: b64encode(await s.certHash(client)),
      dh: b64encode(new Uint8Array(32)), iat: cc.iat, exp: cc.exp,
    }));
    const sig = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, priv,
      sign.message(sign.PURPOSE_API_CERT, payload)));
    const env = { payload, kid: await sign.keyID(pub), sig };
    const node = {
      id: "", kind: "api", name: "", noise: new Uint8Array(32), sign: pub,
      url: "", tls: new Uint8Array(0), rootUsers: [], rootRoles: [],
    };
    const list = { installation: cc.inst, nodes: [node] };
    try {
      await s.verifyAPI(env, list, client, d.now);
      return "accepted";
    } catch (e) {
      return e.message;
    }
  }, v);
  expect(got).toBe("session: malformed certificate");
});

// Fixrunde 1, K1: the envelope is a caller's object, so every field of it may
// be a getter that answers two reads with two values. The signature is checked
// over one read of env.payload and the certificate decoded from another: what
// comes back then is bytes no API node ever signed. sign.js and proof.js read
// every field once for exactly this reason, and validateClientCert above says
// so in as many words. The envelope here signs a DH key of 0x01 bytes and
// hands out 0xaa bytes on the second read, so what the answer carries says
// which of the two the shell believed.
test("verifyAPI uses the payload it checked, not a second read of it", async ({ shell }) => {
  test.setTimeout(120_000);
  test.skip(!(await hasEd25519(shell, v.trust)),
    "no Ed25519 in this engine's Web Crypto: it signs nothing and checks nothing");
  const got = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/session.js");
    const sign = await import("/assets/js/contracts/sign.js");
    const proof = await import("/assets/js/contracts/proof.js");
    const { b64decode, b64encode, utf8 } = await import("/assets/js/contracts/b64.js");
    const seed = new Uint8Array(32).fill(9);
    const priv = await proof.privateFromSeed(seed);
    const pub = await proof.publicFromSeed(seed);
    const client = b64decode(d.client);
    const cc = JSON.parse(new TextDecoder().decode(client));
    const hash = b64encode(await s.certHash(client));
    const cert = (fill) => utf8(JSON.stringify({
      inst: cc.inst, node: "api-1", client: hash, dh: b64encode(new Uint8Array(32).fill(fill)),
      iat: cc.iat, exp: cc.exp,
    }));
    const signed = cert(0x01), other = cert(0xaa);
    const sig = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, priv,
      sign.message(sign.PURPOSE_API_CERT, signed)));
    let reads = 0;
    const env = {
      get payload() { reads++; return reads === 1 ? signed : other; },
      kid: await sign.keyID(pub), sig,
    };
    const node = {
      id: "api-1", kind: "api", name: "api-1", noise: new Uint8Array(32), sign: pub,
      url: "", tls: new Uint8Array(0), rootUsers: [], rootRoles: [],
    };
    const list = { installation: cc.inst, nodes: [node] };
    try {
      const c = await s.verifyAPI(env, list, client, d.now);
      return { answer: "accepted", dh: b64encode(c.dh), reads };
    } catch (e) {
      return { answer: e.message, dh: "", reads };
    }
  }, v);
  // The signature is over the 0x01 payload, so that is the one certificate
  // this envelope stands for, and env.payload is read exactly once.
  expect(got).toEqual({
    answer: "accepted",
    dh: "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE",
    reads: 1,
  });
});

// Fixrunde 2: encodeSetupInfoNodes read nodes.length twice, which my own fix
// of the round before put there. Array.isArray is true for a proxy over an
// array, so one read could answer "more than none" and another "not too
// many": the 17 below only has to clear the lower bound and the
// MAX_SETUP_NODES after it only the upper one, and 17 nodes go into version 1.
// What was missing is one value that answers both questions, which the local
// in the code now is. (Fixrunde 3: this said "neither read passes both
// bounds" - the second read, MAX_SETUP_NODES itself, plainly does.)
//
// What one read cannot do is make a lying length honest - a proxy that says 1
// where the array holds nothing is believed, before the fix and after, and so
// would a MAX_SETUP_NODES over 17 entries - so the second row pins that limit
// rather than claiming it away. Whoever hands this function a proxy is past
// every promise the file makes; what it does promise is that the length it
// decided on is the one length it saw.
test("encodeSetupInfoNodes decides on the length it read", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const s = await import("/assets/js/contracts/session.js");
    const lying = (target, first) => {
      let n = 0;
      return new Proxy(target, {
        get(t, k, r) {
          if (k === "length") {
            n++;
            return n === 1 ? first : s.MAX_SETUP_NODES;
          }
          return Reflect.get(t, k, r);
        },
      });
    };
    const out = (x) => {
      try {
        const r = s.encodeSetupInfoNodes(x);
        return Array.isArray(r) ? "nodes:" + r.length : "no array: " + String(r);
      } catch (e) {
        return e.message;
      }
    };
    return {
      // 17 first, MAX_SETUP_NODES after: neither bound refuses the pair.
      pastTheLimit: out({ nodes: lying(new Array(s.MAX_SETUP_NODES + 1).fill({ id: "api-1" }), s.MAX_SETUP_NODES + 1) }),
      // 1 first over an empty array: one read believes it, and says so.
      emptyButSaysOne: out({ nodes: lying([], 1) }),
      plainTooMany: out({ nodes: new Array(s.MAX_SETUP_NODES + 1).fill({ id: "api-1" }) }),
    };
  });
  expect(got).toEqual({
    pastTheLimit: "session: malformed message",
    emptyButSaysOne: "nodes:16",
    plainTooMany: "session: malformed message",
  });
});

// Fixrunde 2: W2 was not carried through. validateClientCert names iat and
// exp but never asks whether they are numbers, and Go's ClientCert has both
// as int64 - json.Unmarshal refuses a string, a boolean and a fraction alike.
// So the check that encodeClientCert holds a certificate against let three
// kinds of value through, and the shell would have had the user sign them
// with a passkey for the API to refuse: the very damage the fix was for.
// Number.isSafeInteger, the same line K2 uses for now. A BigInt is in the
// list because it passed the check and was stopped only by JSON.stringify -
// one step further than a check should let anything get.
test("validateClientCert takes whole numbers for iat and exp", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const s = await import("/assets/js/contracts/session.js");
    const good = {
      inst: "EREREREREREREREREREREQ", user: "ISEhISEhISEhISEhISEhIQ",
      nonce: new Uint8Array(32), sig: new Uint8Array(32), dh: new Uint8Array(32),
      iat: 1790000000, exp: 1790003600,
    };
    const out = {};
    const cases = {
      good,
      strings: { ...good, iat: "1790000000", exp: "1790086400" },
      boolean: { ...good, iat: true, exp: 2 },
      fraction: { ...good, iat: 1.5, exp: 2.5 },
      bigint: { ...good, iat: 1n, exp: 2n },
    };
    for (const [name, c] of Object.entries(cases)) {
      let valid, written;
      try {
        valid = s.validateClientCert(c);
      } catch (e) {
        valid = "threw: " + e.message;
      }
      try {
        s.encodeClientCert(c);
        written = "written";
      } catch (e) {
        written = e.message;
      }
      out[name] = { valid, written };
    }
    return out;
  });
  const no = { valid: false, written: "session: cannot encode this certificate" };
  expect(got).toEqual({
    good: { valid: true, written: "written" },
    strings: no,
    boolean: no,
    fraction: no,
    bigint: no,
  });
});

// Fixrunde 1, small: encodeSetupInfoNodes is exported and answers whatever it
// is handed. It broke the discipline of its own file - a raw TypeError for
// null, and undefined for an object without nodes, which whoever builds
// version 1 from it would write as "nodes": undefined. It takes what
// parseSetupOffer produces, so that is what it accepts: 1 to MAX_SETUP_NODES
// nodes, and one controlled error for everything else.
test("encodeSetupInfoNodes takes an offer's nodes or refuses", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const s = await import("/assets/js/contracts/session.js");
    const out = (x) => {
      try {
        const r = s.encodeSetupInfoNodes(x);
        return Array.isArray(r) ? "nodes:" + r.length : "no array: " + String(r);
      } catch (e) {
        return e.message;
      }
    };
    return {
      good: out({ nodes: [{ id: "api-1" }, { id: "api-2" }] }),
      empty: out({ nodes: [] }),
      tooMany: out({ nodes: new Array(s.MAX_SETUP_NODES + 1).fill({ id: "api-1" }) }),
      noNodes: out({}),
      notArray: out({ nodes: 5 }),
      nul: out(null),
      undef: out(undefined),
      number: out(5),
    };
  });
  const refused = "session: malformed message";
  expect(got).toEqual({
    good: "nodes:2",
    empty: refused,
    tooMany: refused,
    noNodes: refused,
    notArray: refused,
    nul: refused,
    undef: refused,
    number: refused,
  });
});

// Fixrunde 1, W2: Go's json.Marshal writes all seven fields of
// session.ClientCert every time - the struct has no omitempty anywhere.
// JSON.stringify leaves out a member whose value is undefined, so a field
// nobody filled in would be quietly missing from the very bytes the user then
// signs with a passkey, and the API would refuse what it gets: a burnt
// gesture and no error on the way. A BigInt is in the list because it passes
// every check a number passes and is no JSON number.
test("encodeClientCert writes all seven fields or refuses", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const s = await import("/assets/js/contracts/session.js");
    const { fromUtf8 } = await import("/assets/js/contracts/b64.js");
    const good = {
      inst: "EREREREREREREREREREREQ", user: "ISEhISEhISEhISEhISEhIQ",
      nonce: new Uint8Array(32), sig: new Uint8Array(32), dh: new Uint8Array(32),
      iat: 1790000000, exp: 1790003600,
    };
    const out = (c) => {
      try {
        return fromUtf8(s.encodeClientCert(c));
      } catch (e) {
        return e.message;
      }
    };
    const without = (name) => {
      const c = { ...good };
      delete c[name];
      return out(c);
    };
    return {
      good: out(good),
      iatUndefined: out({ ...good, iat: undefined }),
      expMissing: without("exp"),
      userUndefined: out({ ...good, user: undefined }),
      iatNaN: out({ ...good, iat: NaN }),
      bigint: out({ ...good, iat: 1n, exp: 2n }),
      nul: out(null),
    };
  });
  const zeros = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA";
  const refused = "session: cannot encode this certificate";
  expect(got).toEqual({
    good: `{"inst":"EREREREREREREREREREREQ","user":"ISEhISEhISEhISEhISEhIQ","nonce":"${zeros}",`
      + `"sig":"${zeros}","dh":"${zeros}","iat":1790000000,"exp":1790003600}`,
    iatUndefined: refused,
    expMissing: refused,
    userUndefined: refused,
    iatNaN: refused,
    bigint: refused,
    nul: refused,
  });
});

// Fixrunde 1, the same question asked of verifyAPI's node: its id is read
// once for "is this a node at all" and once for "is this the node the
// certificate names", and its sign once to find it and once to check the
// signature. A getter that answers "api-1" first and "api-9" second lets a
// certificate name a node that did not sign it. (The key survives such a
// getter on its own: sign.verify derives the key id again and compares it
// with the envelope's, so a second, other key is refused there. The id has no
// such second line.)
test("verifyAPI binds the certificate to the node it found", async ({ shell }) => {
  test.setTimeout(120_000);
  test.skip(!(await hasEd25519(shell, v.trust)),
    "no Ed25519 in this engine's Web Crypto: it signs nothing and checks nothing");
  const got = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/session.js");
    const sign = await import("/assets/js/contracts/sign.js");
    const proof = await import("/assets/js/contracts/proof.js");
    const { b64decode, b64encode, utf8 } = await import("/assets/js/contracts/b64.js");
    const seed = new Uint8Array(32).fill(9);
    const priv = await proof.privateFromSeed(seed);
    const pub = await proof.publicFromSeed(seed);
    const client = b64decode(d.client);
    const cc = JSON.parse(new TextDecoder().decode(client));
    // The certificate names api-9; the node that signs is api-1.
    const payload = utf8(JSON.stringify({
      inst: cc.inst, node: "api-9", client: b64encode(await s.certHash(client)),
      dh: b64encode(new Uint8Array(32)), iat: cc.iat, exp: cc.exp,
    }));
    const sig = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, priv,
      sign.message(sign.PURPOSE_API_CERT, payload)));
    const env = { payload, kid: await sign.keyID(pub), sig };
    const rest = {
      kind: "api", name: "api-1", noise: new Uint8Array(32), sign: pub,
      url: "", tls: new Uint8Array(0), rootUsers: [], rootRoles: [],
    };
    let n = 0;
    const node = { ...rest, get id() { n++; return n === 1 ? "api-1" : "api-9"; } };
    const check = async (nodes) => {
      try {
        await s.verifyAPI(env, { installation: cc.inst, nodes }, client, d.now);
        return "accepted";
      } catch (e) {
        return e.message;
      }
    };
    return {
      getter: await check([node]),
      plain: await check([{ ...rest, id: "api-1" }]),
    };
  }, v);
  // Both are the same certificate against the same key: it names api-9, the
  // node that signed is api-1, and that is no session either way.
  expect(got).toEqual({
    getter: "session: malformed certificate",
    plain: "session: malformed certificate",
  });
});

// Fixrunde 1, W1: verifyApproval had no test at all, though session.json
// carries the pair for it. client + proof is a certificate of the user admin
// signed with his password key, and his role asks for no password on top, so
// it stands. clientRejects holds the other side: carol's certificate with
// carol's password key, and her role "careful" has passwordToo - a password
// key alone approves nothing there, which is the one rule this function has
// beyond verifyClient's.
test("verifyApproval decides the certificates of session.json as Go does", async ({ shell }) => {
  test.setTimeout(120_000);
  test.skip(!(await hasEd25519(shell, v.trust)),
    "no Ed25519 in this engine's Web Crypto: it reads none of this list's passkeys and checks no proof");
  const got = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/session.js");
    const t = await import("/assets/js/contracts/trust.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const list = await t.parseList(b64decode(d.trust.payload));
    const asProof = (p) => ({
      kind: p.kind, kid: p.kid, sig: b64decode(p.sig),
      authData: p.authData === undefined ? new Uint8Array(0) : b64decode(p.authData),
      clientData: p.clientData === undefined ? new Uint8Array(0) : b64decode(p.clientData),
    });
    const check = async (client, p) => {
      try {
        const c = await s.verifyApproval(b64decode(client), asProof(p), list);
        return "approved by " + c.user;
      } catch (e) {
        return e.message;
      }
    };
    return {
      good: await check(d.client, d.proof),
      rejects: await Promise.all(d.clientRejects.map((r) => check(r.client, r.proof))),
    };
  }, v);
  // The good pair is the user admin's own certificate; every reject is
  // refused, and for the reason Go gives it.
  expect(got.good).toBe("approved by ISEhISEhISEhISEhISEhIQ");
  expect(got.rejects).toEqual(v.clientRejects.map(() => "session: proof is not by a key of this user"));
});

// Fixrunde 1, the third place K1 sent me looking for. verifyApproval reads
// two fields of the trust list twice, and both reads decide the one rule this
// function has of its own: a user whose role asks for the password too is
// approved by a passkey alone. found.key.kind is read here and again inside
// proof.verify, and found.user.id is read for the comparison with the
// certificate and again for trust.passwordToo. A getter that answers the
// first read with "passkey" and the second with "password", or the first with
// carol's id and the second with the admin's, turns the rejected vector into
// an accepted approval. Go is handed values: KeyOwner returns a copy of the
// user and of the key, so no second read of anything exists there.
test("verifyApproval decides on the key and the owner it read, not on a second read", async ({ shell }) => {
  test.setTimeout(120_000);
  test.skip(!(await hasEd25519(shell, v.trust)),
    "no Ed25519 in this engine's Web Crypto: it reads none of this list's passkeys and checks no proof");
  const got = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/session.js");
    const t = await import("/assets/js/contracts/trust.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const list = await t.parseList(b64decode(d.trust.payload));
    const reject = d.clientRejects[0];
    const client = b64decode(reject.client);
    // The user of the refused certificate (carol, role "careful"), and the
    // user of the good one (admin, whose role asks for no password on top).
    const owner = JSON.parse(new TextDecoder().decode(client)).user;
    const other = JSON.parse(new TextDecoder().decode(b64decode(d.client))).user;
    const p = {
      kind: reject.proof.kind, kid: reject.proof.kid, sig: b64decode(reject.proof.sig),
      authData: new Uint8Array(0), clientData: new Uint8Array(0),
    };
    const withKeys = (f) => ({ ...list, users: list.users.map((u) => ({ ...u, keys: u.keys.map(f) })) });
    const kindGetter = withKeys((k) => {
      if (k.id !== reject.proof.kid) return k;
      let n = 0;
      return { ...k, get kind() { n++; return n === 1 ? "passkey" : "password"; } };
    });
    const idGetter = {
      ...list,
      users: list.users.map((u) => {
        if (u.id !== owner) return u;
        let n = 0;
        return { ...u, get id() { n++; return n === 1 ? owner : n === 2 ? other : "x"; } };
      }),
    };
    const check = async (l) => {
      try {
        const c = await s.verifyApproval(client, p, l);
        return "approved by " + c.user;
      } catch (e) {
        return e.message;
      }
    };
    return { plain: await check(list), kindGetter: await check(kindGetter), idGetter: await check(idGetter) };
  }, v);
  // The plain list refuses for the reason Go gives. With one read per field,
  // the key stays the passkey it claimed to be, and a password proof is no
  // proof by a passkey.
  //
  // The id is the case I expected to close and could not, and the measurement
  // says why: session.js reads found.user.id once now, but trust.passwordToo
  // looks the user up in the list all over again, and Go's List.PasswordToo
  // does exactly the same - it calls List.User with the ID rather than using
  // the user KeyOwner returned. A list whose ids answer two reads differently
  // fools that second lookup in both languages, and a hand-built list with
  // two users of one ID fools Go with no getter at all. So this line is not a
  // divergence from Go and not session's to close; it is pinned here so that
  // a change in either direction is noticed. A list out of trust.parseList
  // holds plain objects and no getter, and validateList refuses two users of
  // one ID.
  expect(got).toEqual({
    plain: "session: proof is not by a key of this user",
    kindGetter: "session: bad signature",
    idGetter: "approved by IyMjIyMjIyMjIyMjIyMjIw",
  });
});

// Fixrunde 1, K2: Go is handed an int64 for now and can be handed nothing
// else. Here both comparisons of the time check are false against undefined,
// NaN, text or an object, so a certificate that expired long ago comes
// through - and verifyApproval leaves the time out on purpose, which makes
// this the one place where the shell asks whether a session still holds. A
// numeric string and a fraction are in the list because Go has neither: the
// string compares like a number and would pass unnoticed.
test("verifyAPI takes no now that is not a whole number", async ({ shell }) => {
  test.setTimeout(120_000);
  test.skip(!(await hasEd25519(shell, v.trust)),
    "no Ed25519 in this engine's Web Crypto: it signs nothing and checks nothing");
  const got = await shell.evaluate(async (d) => {
    const s = await import("/assets/js/contracts/session.js");
    const sign = await import("/assets/js/contracts/sign.js");
    const proof = await import("/assets/js/contracts/proof.js");
    const { b64decode, b64encode, utf8 } = await import("/assets/js/contracts/b64.js");
    const seed = new Uint8Array(32).fill(9);
    const priv = await proof.privateFromSeed(seed);
    const pub = await proof.publicFromSeed(seed);
    const client = b64decode(d.client);
    const cc = JSON.parse(new TextDecoder().decode(client));
    const payload = utf8(JSON.stringify({
      inst: cc.inst, node: "api-1", client: b64encode(await s.certHash(client)),
      dh: b64encode(new Uint8Array(32)), iat: cc.iat, exp: cc.exp,
    }));
    const sig = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, priv,
      sign.message(sign.PURPOSE_API_CERT, payload)));
    const env = { payload, kid: await sign.keyID(pub), sig };
    const node = {
      id: "api-1", kind: "api", name: "api-1", noise: new Uint8Array(32), sign: pub,
      url: "", tls: new Uint8Array(0), rootUsers: [], rootRoles: [],
    };
    const list = { installation: cc.inst, nodes: [node] };
    const check = async (...now) => {
      try {
        await s.verifyAPI(env, list, client, ...now);
        return "accepted";
      } catch (e) {
        return e.message;
      }
    };
    return {
      valid: await check(d.now),
      tenYearsAfterExp: await check(d.now + 10 * 365 * 24 * 3600),
      undef: await check(undefined),
      nan: await check(NaN),
      text: await check("later"),
      numericText: await check(String(d.now)),
      fraction: await check(d.now + 0.5),
      object: await check({}),
      missing: await check(),
    };
  }, v);
  const refused = "session: certificate not valid at this time";
  expect(got).toEqual({
    valid: "accepted",
    tenYearsAfterExp: refused,
    undef: refused,
    nan: refused,
    text: refused,
    numericText: refused,
    fraction: refused,
    object: refused,
    missing: refused,
  });
});
