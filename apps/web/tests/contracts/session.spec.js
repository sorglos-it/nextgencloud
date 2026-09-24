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
        return true;
      } catch {
        return false;
      }
    };
    return { good: await check(d.api), rejects: await Promise.all(d.apiRejects.map((r) => check(r.api))) };
  }, v);
  expect(got.good).toBe(true);
  const wrong = v.apiRejects.filter((r, i) => got.rejects[i]).map((r) => r.name);
  expect(wrong).toEqual([]);
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
  // the key stays the passkey it claimed to be - and a password proof is no
  // proof by a passkey - and the owner stays carol, whose role asks for the
  // password too.
  expect(got).toEqual({
    plain: "session: proof is not by a key of this user",
    kindGetter: "session: bad signature",
    idGetter: "session: proof is not by a key of this user",
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
