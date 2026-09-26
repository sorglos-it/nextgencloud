import { test, expect, hasSessionKeys } from "../fixtures/page.js";

// /config.json carries the addresses of the API servers, the pinned anchor of
// the trust list and the relying party for passkeys - where the shell
// connects, which trust list it believes and which party a passkey is asked
// for. It used to come in through res.json(): no strict JSON, no budget, no
// grammar, and then onto window. This file holds it to its contract.
//
// Measured, so nobody looks for them: the TLS pins are NOT in here. web.json
// on the server has apis[].tls, Config.php keeps it and hands Apache the pin;
// /config.json carries the addresses alone. The body below is byte for byte
// what the test server answers.
const GOOD = {
  version: "0.1.0",
  anchor: { seq: 1, hash: "q6urq6urq6urq6urq6urq6urq6urq6urq6urq6urq6s" },
  rp: { id: "ngc.example.org", origins: ["https://ngc.example.org"] },
  apis: ["https://api-1.ngc.test:7443", "https://api-2.ngc.test:7443"],
};

const body = (over) => JSON.stringify({ ...GOOD, ...over });

// An absent member, which is not the same as one written as null: strict
// JSON has no null at all (Go: enc.ErrNull), so "not there" is the only way
// to say "there is none".
function without(name) {
  const o = { ...GOOD };
  delete o[name];
  return JSON.stringify(o);
}

// Runs parseConfig over each body and answers with the ones it took.
async function taken(page, bodies) {
  return page.evaluate(async (list) => {
    const cfg = await import("/assets/js/net/config.js");
    const enc = new TextEncoder();
    const out = [];
    for (const [name, text] of list) {
      try {
        cfg.parseConfig(enc.encode(text));
        out.push(name);
      } catch {
        // Refused, which is what every case below is here for.
      }
    }
    return out;
  }, bodies);
}

test("the configuration the server really sends is taken, and its anchor comes out as bytes", async ({ shell }) => {
  const got = await shell.evaluate(async (text) => {
    const cfg = await import("/assets/js/net/config.js");
    const c = cfg.parseConfig(new TextEncoder().encode(text));
    return {
      version: c.version,
      seq: c.anchor.seq,
      hashBytes: c.anchor.hash instanceof Uint8Array ? c.anchor.hash.length : -1,
      rpId: c.rp.id,
      origins: c.rp.origins,
      apis: c.apis,
    };
  }, body({}));
  expect(got).toEqual({
    version: "0.1.0",
    seq: 1,
    // trust.open compares the anchor hash against a SHA-256 it computes
    // itself, so it has to arrive as 32 bytes and not as 43 characters.
    hashBytes: 32,
    rpId: "ngc.example.org",
    origins: ["https://ngc.example.org"],
    apis: ["https://api-1.ngc.test:7443", "https://api-2.ngc.test:7443"],
  });
});

test("the two absences the contract allows are taken", async ({ shell }) => {
  const got = await taken(shell, [
    // Before the controller of the web server is paired there is no anchor,
    // and index.php leaves the member out - see "a null where a member is
    // absent" below for the spelling it used to write instead.
    ["no anchor", without("anchor")],
    // "No passkeys yet" is an empty id AND no origins - Config.php writes
    // both or neither.
    ["no relying party", body({ rp: { id: "", origins: [] } })],
    // VERSION unreadable leaves the field empty rather than taking the page
    // down over a version string (index.php).
    ["no version", body({ version: "" })],
  ]);
  expect(got).toEqual(["no anchor", "no relying party", "no version"]);
});

test("every field is held to its grammar", async ({ shell }) => {
  const got = await taken(shell, [
    // Addresses: trust.ValidWebURL and nothing else. The first is a downgrade
    // of one character, and it is the whole point of checking here.
    ["http instead of https", body({ apis: ["http://api-1.ngc.test:7443"] })],
    ["an address with a path", body({ apis: ["https://api-1.ngc.test:7443/x"] })],
    ["an address in upper case", body({ apis: ["https://API-1.ngc.test:7443"] })],
    ["an address without a port", body({ apis: ["https://api-1.ngc.test"] })],
    // Apache would balance over it twice and pin two keys for one server.
    ["the same address twice", body({ apis: ["https://api-1.ngc.test:7443", "https://api-1.ngc.test:7443"] })],
    ["no address at all", body({ apis: [] })],
    // The anchor is what the whole trust list hangs from.
    ["an anchor hash of 31 bytes", body({ anchor: { seq: 1, hash: "q6urq6urq6urq6urq6urq6urq6urq6urq6urq6ur" } })],
    ["an anchor hash that is no base64url", body({ anchor: { seq: 1, hash: "!!!" } })],
    ["an anchor at sequence 0", body({ anchor: { seq: 0, hash: GOOD.anchor.hash } })],
    // rp.id goes to navigator.credentials.get, so it is a DNS name and never
    // free text.
    ["an rp id in upper case", body({ rp: { id: "NGC.example.org", origins: GOOD.rp.origins } })],
    ["an rp id with a scheme", body({ rp: { id: "https://ngc.example.org", origins: GOOD.rp.origins } })],
    ["an origin with a trailing slash", body({ rp: { id: "ngc.example.org", origins: ["https://ngc.example.org/"] } })],
    ["an id without origins", body({ rp: { id: "ngc.example.org", origins: [] } })],
    ["origins without an id", body({ rp: { id: "", origins: ["https://ngc.example.org"] } })],
    ["a version that is no version", body({ version: "0.1" })],
    // Strict JSON, which res.json() never was.
    ["a member name twice", '{"version":"0.1.0","anchor":null,"rp":{"id":"","origins":[]},'
      + '"apis":["https://api-1.ngc.test:7443"],"apis":["https://evil.example:443"]}'],
    ["a member nobody reads", body({}).replace(/}$/, ',"extra":1}')],
    ["a sequence past 2^53-1", '{"version":"","anchor":{"seq":9007199254740993,'
      + `"hash":"${GOOD.anchor.hash}"},"rp":{"id":"","origins":[]},`
      + '"apis":["https://api-1.ngc.test:7443"]}'],
    ["trailing data", body({}) + "x"],
    ["an array where the object belongs", "[]"],
    // The spelling index.php used to send for an unpaired installation.
    // Strict JSON has no null on either side, so it was a body the shell
    // could not read at all - found by holding this parser against the bytes
    // the server really sends, not against the ones this file writes.
    ["a null where a member is absent", body({ anchor: null })],
  ]);
  expect(got).toEqual([]);
});

// The one case that holds the writer and the reader together. Everything
// above plays back a body this file wrote; this takes the body index.php
// really sends. It caught the first difference between them: PHP wrote
// "anchor": null for an installation whose web controller is not paired yet,
// and strict JSON has no null on either side - Go answers enc.ErrNull and
// json.js refuses it the same way. index.php leaves the member out now.
test("what the server really sends is what the parser takes", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const cfg = await import("/assets/js/net/config.js");
    const c = await cfg.loadConfig();
    return { apis: c.apis, seq: c.anchor === null ? null : c.anchor.seq };
  });
  expect(got).toEqual({
    apis: ["https://api-1.ngc.test:7443", "https://api-2.ngc.test:7443"],
    seq: 1,
  });
});

// THIS CASE DOES NOT DISTINGUISH WHICH BOUND REFUSED, and saying so is the
// point. Measured: with the stream bound in loadConfig raised to
// Number.MAX_SAFE_INTEGER it stayed green, because json.decode's budget is
// the same MAX_CONFIG and turns the same bytes away once they are all in
// memory. The two cannot disagree on the answer - only on whether 8 KiB or
// 8 GiB was held first - and no body over MAX_CONFIG can be a valid
// configuration anyway, since the grammar caps it under 7 KiB.
//
// The stream bound itself is held by "an answer past its bound is dropped,
// not buffered" in http.spec.js, where an over-long answer gives E_TOOLARGE
// and a budget failure gives E_PROTOCOL, so the two ARE told apart there.
// What stays here is the cheap guard that the pair of them still refuses.
test("an answer larger than the bound is refused", async ({ shell }) => {
  await shell.route("**/config.json", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: "a".repeat(8 * 1024 + 1) }));
  const out = await shell.evaluate(async () => {
    const cfg = await import("/assets/js/net/config.js");
    try { await cfg.loadConfig(); return "taken"; } catch { return "refused"; }
  });
  expect(out).toBe("refused");
});

// The probe has to run on the page and not before it. Measured: on
// about:blank crypto.subtle is undefined, so hasSessionKeys answers false
// there and the three cases below skipped themselves on chromium - for the
// wrong reason, and silently.
async function ready(page) {
  await page.goto("/");
  return hasSessionKeys(page);
}

// Serves text as /config.json and loads the page again, recording every
// address it asks for from that point on - the first load above is over.
async function boot(page, text) {
  const asked = [];
  page.on("request", (r) => asked.push(r.url()));
  await page.route("**/config.json", (route) =>
    route.fulfill({ status: 200, contentType: "application/json; charset=utf-8", body: text }));
  await page.goto("/");
  return asked;
}

// The addresses out of a configuration are the only reason to check it before
// using it, so "nothing was asked of it" is the half that matters. It is
// measured over every request the page made, not assumed from the message.
const OF_THE_CONFIG = /api-\d\.ngc\.test|evil\.example|\/api\/v1\//;

test("a configuration whose address is no address is refused, and nothing was asked of it", async ({ page }) => {
  test.skip(!(await ready(page)),
    "no session keys in this engine's Web Crypto: the boot stops before the configuration is fetched");
  const asked = await boot(page, body({ apis: ["http://api-1.ngc.test:7443"] }));
  // The same sentence a missing setup gets: the person must not have to
  // guess, and must learn nothing about the shape either.
  await expect(page.locator(".msg")).toHaveText("NGC ist nicht eingerichtet.");
  await expect(page.locator("#taskbar")).toHaveCount(0);
  expect(asked.filter((u) => OF_THE_CONFIG.test(u))).toEqual([]);
});

test("a configuration with a member name twice is refused, and nothing was asked of it", async ({ page }) => {
  test.skip(!(await ready(page)),
    "no session keys in this engine's Web Crypto: the boot stops before the configuration is fetched");
  // JSON.parse keeps the LAST of two members of one name, so whoever reads
  // the file down to the first "apis" and the shell would not be looking at
  // the same addresses.
  const asked = await boot(page, '{"version":"0.1.0","anchor":null,'
    + '"rp":{"id":"ngc.example.org","origins":["https://ngc.example.org"]},'
    + '"apis":["https://api-1.ngc.test:7443"],"apis":["https://evil.example:443"]}');
  await expect(page.locator(".msg")).toHaveText("NGC ist nicht eingerichtet.");
  await expect(page.locator("#taskbar")).toHaveCount(0);
  expect(asked.filter((u) => OF_THE_CONFIG.test(u))).toEqual([]);
});

test("a good configuration brings the desktop up and leaves nothing on window", async ({ page }) => {
  test.skip(!(await ready(page)),
    "no session keys in this engine's Web Crypto: there is no desktop to look at");
  await boot(page, body({}));
  await expect(page.locator("#taskbar")).toBeVisible();
  // Nothing reads a global, so nothing puts one there: the configuration is
  // handed on instead.
  expect(await page.evaluate(() => window.__ngcConfig)).toBeUndefined();
});
