import { test, expect, skipWithoutSignatures } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("store.json");

// What this case needs from Web Crypto: every catalog of store.json is signed
// with Ed25519, so on an engine that cannot check one, verifyCatalog answers
// "bad signature" for the good catalog as well as for the sixteen bad ones -
// and a run that asserts the sixteen refusals while the good one is refused too
// asserts nothing. It reads no seed, so the signature step is the whole
// question. Measured per action in fixtures/page.js (sessionKeyGaps).
//
// MEASURED, AND IT IS WHY THIS STANDS HERE. The WebKit that Playwright brings
// answers NotSupportedError already at generateKey and reads this key for no
// catalog; chromium and the Firefox that Playwright brings (build firefox-1490)
// both read it, so both run this case and must keep running it. The guard that
// stood here imported one raw public key for "verify" - which that Firefox also
// manages, so for THIS case it happened to answer right; it answered wrong for
// the cases that build a key out of a seed, and that is why all four copies of
// it are gone.

test("a catalog is only taken from its own store, and never an older one", async ({ shell }) => {
  test.setTimeout(120_000);
  await skipWithoutSignatures(shell);
  const got = await shell.evaluate(async (d) => {
    const c = await import("/assets/js/contracts/catalog.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const store = { id: d.store, name: "NGC Store", url: "https://store.ngc.example", key: b64decode(d.storeKey) };
    const env = (e) => ({ payload: b64decode(e.payload), kid: e.kid, sig: b64decode(e.sig) });
    const ok = async (e, lastSeq) => {
      try { await c.verifyCatalog(env(e), store, lastSeq); return true; } catch { return false; }
    };
    return {
      good: await ok(d.catalog, 0),
      rollback: await ok(d.catalog, d.catalogLastSeq + 1),
      // Each reject carries the lastSeq it has to be refused at, and it is
      // that number the case is run with. Fifteen of the sixteen carry 0,
      // which refuses no seq at all; "older than the last one seen" carries
      // one past its own seq, and it is the ONLY thing wrong with that
      // catalog - its bytes are the good one's, signed by the right key for
      // the right purpose. Run at 0 it is a catalog that must be taken, so a
      // run that hands every case 0 asserts the opposite of what its name
      // says and goes green on a verifier with no rollback check at all.
      rejects: await Promise.all(d.catalogRejects.map((r) => ok(r.catalog, r.lastSeq))),
      otherStore: await ok(d.catalog, 0) && !(await (async () => {
        try {
          await c.verifyCatalog(env(d.catalog), { ...store, id: "other" }, 0);
          return true;
        } catch { return false; }
      })()),
    };
  }, v);
  expect(got.good).toBe(true);
  expect(got.rollback).toBe(false);
  expect(got.otherStore).toBe(true);
  const wrong = v.catalogRejects.filter((r, i) => got.rejects[i]).map((r) => r.name);
  expect(wrong).toEqual([]);
});

test("the grant rule of an update holds", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const c = await import("/assets/js/contracts/catalog.js");
    return {
      same: c.granted(["root"], ["root"]),
      fewer: c.granted([], ["root"]),
      more: c.granted(["root", "network"], ["root"]),
      target: c.validTarget("ngc-web") && c.validTarget("system") && c.validTarget("org.sorglos.terminal")
        && !c.validTarget("ngc.desktop") && !c.validTarget("terminal"),
    };
  });
  expect(out).toEqual({ same: true, fewer: true, more: false, target: true });
});
