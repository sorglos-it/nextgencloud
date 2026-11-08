// The trust anchor of the mock world: derived, not written down.
//
// WHAT THIS CASE IS FOR. apps/web/tests/trust-anchor.json used to hold 0xAB
// repeated -- a filler pattern. Not out of carelessness: mock/world.js built a
// fresh random signing key on every run, so version 1 carried different bytes
// each time and no fixed number could ever match. The anchor check therefore
// had to be bypassed (standInOn), and the consequence nobody had written down
// is that the anchor check of the whole web shell was never once exercised.
//
// HOW THIS CASE IS BUILT, and it is the whole point: it DERIVES the anchor
// again from the seeds in mock/seeded.js and compares the result with the
// file. It does NOT read the file twice, and it does not hold the file against
// a constant stored beside it -- two constants confirming each other are the
// placeholder in better clothes. If somebody changes a seed, changes the
// genesis list, or changes how a version is encoded, this case goes red and
// says which side moved.
import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const anchorFile = join(here, "..", "trust-anchor.json");

test("the anchor file is what the mock world's version 1 really hashes to", async () => {
  // Node's own WebCrypto, not a browser's: this case is about bytes and not
  // about an engine. Where Ed25519 is missing it must not quietly pass -- a
  // guard that skips itself guards nothing -- so the missing capability is the
  // failure.
  let haveEd = true;
  try {
    await crypto.subtle.generateKey({ name: "Ed25519" }, false, ["sign", "verify"]);
  } catch {
    haveEd = false;
  }
  expect(haveEd, "this node has no Ed25519 in WebCrypto, so the anchor cannot be derived")
    .toBe(true);

  const trust = await import("../../public/assets/js/contracts/trust.js");
  const world = await import("../mock/world.js");

  const w = await world.buildWorld();
  // Out of the chain the mock really hands out and not out of the list object:
  // the anchor binds the EXACT bytes of one version, and a re-encode could
  // differ by a byte -- then the anchor would be of something nobody serves.
  const chain = JSON.parse(new TextDecoder().decode(w.chainBytes));
  const signed = chain.versions[0];
  const hash = await trust.hash({
    payload: Uint8Array.from(Buffer.from(signed.payload, "base64")),
    proof: signed.proof,
  });
  const derived = Buffer.from(hash).toString("base64")
    .replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");

  const file = JSON.parse(readFileSync(anchorFile, "utf8"));
  expect(file.seq, "the anchor is of version 1").toBe(1);
  expect(file.hash,
    "the anchor file and the derived anchor disagree -- run node tools/trust-anchor.mjs --write")
    .toBe(derived);
});

test("the mock world is reproducible, so the anchor can be a fixed number at all", async () => {
  const world = await import("../mock/world.js");
  const hashOf = async () => {
    const trust = await import("../../public/assets/js/contracts/trust.js");
    const w = await world.buildWorld();
    const chain = JSON.parse(new TextDecoder().decode(w.chainBytes));
    const signed = chain.versions[0];
    return Buffer.from(await trust.hash({
      payload: Uint8Array.from(Buffer.from(signed.payload, "base64")),
      proof: signed.proof,
    })).toString("hex");
  };
  // TWICE, and this is the case that would have caught the original fault:
  // with generateKey instead of the seeds these two differ, and then no anchor
  // file can ever be right.
  const a = await hashOf();
  const b = await hashOf();
  expect(b, "two builds of the mock world give different bytes, so it is not reproducible")
    .toBe(a);
});
