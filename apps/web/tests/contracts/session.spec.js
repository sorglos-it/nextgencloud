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
