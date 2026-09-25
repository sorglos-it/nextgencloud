import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const core = vectors("core.json");
const ui = vectors("uiproof.json");

// Does the Web Crypto API of this engine know Ed25519? The UI proofs need it
// twice over: trust.parseList imports every passkey of this list as spki, and
// verifying a catalog, a certificate and a task is three Ed25519 signatures.
// The WebKit that Playwright brings answers NotSupportedError to importKey, so
// parseList refuses the list before the first proof is looked at ("trust:
// invalid list", measured) - which is the right answer for an engine that
// cannot check, and no measurement of this contract. Same probe as
// session.spec.js and sign.spec.js, with a real key out of this vector's own
// list, so an engine that has Ed25519 but turns down a made-up 32-byte value
// is not skipped for the wrong reason.
//
// The other two tests read documents and check no signature; they run
// everywhere.
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
    // Outside the try: a vector file that lost its password key must fail
    // loudly, not skip the test as "no Ed25519 here" on an engine that has it.
    if (typeof pub !== "string") throw new Error("uiproof.json's trust list holds no password key to probe with");
    try {
      await crypto.subtle.importKey("raw", b64decode(pub), { name: "Ed25519" }, false, ["verify"]);
      return true;
    } catch {
      return false;
    }
  }, signed);
}

test("every example result of core.json is read", async ({ shell }) => {
  const got = await shell.evaluate(async (d) => {
    const c = await import("/assets/js/contracts/core.js");
    const out = {};
    for (const [key, value] of Object.entries(d)) {
      if (key === "empty" || key === "notice") continue;
      const [app, name] = key.split("/");
      const action = c.lookupByKey(app, name);
      const bytes = new TextEncoder().encode(JSON.stringify(value));
      try {
        c.parseResult(action, bytes);
        out[key] = true;
      } catch (e) {
        out[key] = String(e);
      }
    }
    // Empty lists arrive as [], never as null.
    for (const [key, value] of Object.entries(d.empty)) {
      const [app, name] = key.split("/");
      const action = c.lookupByKey(app, name);
      const parsed = c.parseResult(action, new TextEncoder().encode(JSON.stringify(value)));
      out["empty:" + key] = Array.isArray(parsed) && parsed.length === 0;
    }
    out.notice = !!c.parseNotice(new TextEncoder().encode(JSON.stringify(d.notice)));
    return out;
  }, core);
  for (const [key, value] of Object.entries(got)) expect(value, key).toBe(true);
});

test("every UI proof of uiproof.json is decided as Go decides it", async ({ shell }) => {
  test.setTimeout(180_000);
  test.skip(!(await hasEd25519(shell, ui.trust)),
    "no Ed25519 in this engine's Web Crypto: it reads none of this list's passkeys and checks no signature");
  const got = await shell.evaluate(async (d) => {
    const c = await import("/assets/js/contracts/core.js");
    const t = await import("/assets/js/contracts/trust.js");
    const { b64decode, b64encode } = await import("/assets/js/contracts/b64.js");
    const list = await t.parseList(b64decode(d.trust.payload));
    const out = [];
    for (const k of d.cases) {
      try {
        const p = c.parseUIProof(b64decode(k.proof));
        const hash = await c.verifyUIProof(p, k.app, k.version, list);
        out.push({ valid: true, ui: b64encode(hash) });
      } catch {
        out.push({ valid: false, ui: "" });
      }
    }
    return out;
  }, ui);
  const wrong = ui.cases.filter((c, i) => String(c.valid) !== String(got[i].valid)
    || (String(c.valid) === "true" && c.ui !== got[i].ui)).map((c) => c.name);
  expect(wrong).toEqual([]);
});

test("a result over MaxCallResult is refused", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const c = await import("/assets/js/contracts/core.js");
    const w = await import("/assets/js/contracts/wire.js");
    const action = c.lookupByKey("ngc.desktop", "me");
    const big = new Uint8Array(w.MAX_CALL_RESULT + 1).fill(0x20);
    big[0] = 0x7b; big[big.length - 1] = 0x7d;
    try { c.parseResult(action, big); return false; } catch { return true; }
  });
  expect(out).toBe(true);
});
