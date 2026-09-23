import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// json.json holds one document with every kind of field (schema), its budget
// (size, items, totals) and the cases Go accepts and refuses. The shell must
// decide every one of them exactly as Go does.
const v = vectors("json.json");

test("every case of json.json is decided as Go decides it", async ({ shell }) => {
  const results = await shell.evaluate(async ([items, totals, size, cases]) => {
    const { decode, Budget, S, A, M, str, bool, int, uint32, bytes, raw } =
      await import("/assets/js/contracts/json.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    // The shape mirrors the Go struct json.json was made from, field for
    // field as its own "schema" spells it: u is a uint32, not a uint, and
    // the elements of subs are the same sub as the single one.
    const sub = S({ x: int, list: A(int) });
    const shape = S({
      n: int, u: uint32, s: str, t: bool, b: bytes,
      list: A(int), map: M(int),
      sub: sub,
      subs: A(sub),
      raw: raw,
    });
    const budget = new Budget({ size, items, totals });
    return cases.map((c) => {
      try {
        decode(b64decode(c.doc), budget, shape);
        return true;
      } catch {
        return false;
      }
    });
  }, [v.items, v.totals, v.size, v.cases]);

  const wrong = v.cases.filter((c, i) => c.valid !== results[i]).map((c) => c.name);
  expect(wrong).toEqual([]);
});

test("base64url is strict", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { b64encode, b64decode, encodedLen } = await import("/assets/js/contracts/b64.js");
    const ok = (s) => { try { b64decode(s); return true; } catch { return false; } };
    return {
      roundTrip: b64encode(new Uint8Array([1, 2, 3])) === "AQID" && b64decode("AQID").length === 3,
      len: encodedLen(16) === 22 && encodedLen(32) === 43,
      padding: ok("AQI="), lineFeed: ok("AQ\nID"), standard: ok("+/=="),
      trailingBits: ok("AQJ"), lonely: ok("A"), empty: b64decode("").length === 0,
    };
  });
  expect(out).toEqual({
    roundTrip: true, len: true, padding: false, lineFeed: false, standard: false,
    trailingBits: false, lonely: false, empty: true,
  });
});
