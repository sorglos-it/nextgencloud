import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// json.json holds one document with every kind of field (schema), its budget
// (size, items, totals) and the cases Go accepts and refuses. The shell must
// decide every one of them exactly as Go does.
const v = vectors("json.json");

test("every case of json.json is decided as Go decides it", async ({ shell }) => {
  const results = await shell.evaluate(async ([items, totals, size, cases]) => {
    const { decode, Budget, S, P, A, M, str, bool, int, uint32, bytes, raw } =
      await import("/assets/js/contracts/json.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    // The shape mirrors the Go struct json.json was made from, field for
    // field: u is a uint32, sub is a pointer (missing gives null, not an
    // empty object), and the elements of subs are that same struct by value.
    const subMembers = { x: int, list: A(int) };
    const shape = S({
      n: int, u: uint32, s: str, t: bool, b: bytes,
      list: A(int), map: M(int),
      sub: P(subMembers),
      subs: A(S(subMembers)),
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

// json.json has no case for this, and a differential run against the compiled
// shared/enc found the shell one level stricter than Go: MAX_DEPTH counts the
// arrays and objects nested in each other, and a number or a string inside
// the innermost one is not a level of its own. The top-level object is level
// 1 and the raw value level 2, so 31 containers below it still fit.
test("depth counts containers, not the values inside them", async ({ shell }) => {
  const docs = [];
  for (let d = 30; d <= 33; d++) {
    docs.push(["arrays, empty leaf, " + d, '{"raw":' + "[".repeat(d) + "]".repeat(d) + "}", d <= 31]);
    docs.push(["arrays, number leaf, " + d, '{"raw":' + "[".repeat(d) + "1" + "]".repeat(d) + "}", d <= 31]);
    docs.push(["objects, number leaf, " + d, '{"raw":' + '{"a":'.repeat(d) + "1" + "}".repeat(d) + "}", d <= 31]);
    docs.push(["objects, string leaf, " + d, '{"raw":' + '{"a":'.repeat(d) + '"x"' + "}".repeat(d) + "}", d <= 31]);
  }
  const results = await shell.evaluate(async (texts) => {
    const { decode, Budget, S, raw } = await import("/assets/js/contracts/json.js");
    const budget = new Budget({ size: 1024, items: { raw: -1 }, totals: {} });
    const shape = S({ raw: raw });
    return texts.map((t) => {
      try {
        decode(new TextEncoder().encode(t), budget, shape);
        return true;
      } catch {
        return false;
      }
    });
  }, docs.map(([, text]) => text));

  const wrong = docs.filter(([, , want], i) => want !== results[i]).map(([name]) => name);
  expect(wrong).toEqual([]);
});
