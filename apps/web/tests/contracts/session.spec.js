import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("session.json");

test("the API certificate is checked against the trust list", async ({ shell }) => {
  test.setTimeout(120_000);
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
