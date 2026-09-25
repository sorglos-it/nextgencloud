import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("store.json");

test("a catalog is only taken from its own store, and never an older one", async ({ shell }) => {
  test.setTimeout(120_000);
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
