// The mock world's chain, walked the way the shell walks it.
//
// WHAT THIS FILE IS FOR. mock/world.js now hands out TWO versions, so that a
// node which only exists from version 2 can be measured at all. A chain is
// not a list, though: the second version is only worth anything if somebody
// HOLDS it against the first, and until this file nothing did. The mock would
// then have had a chain and not checked it -- two versions sitting next to
// each other, and every case passing whether or not the signature over the
// second one means anything.
//
// So this file walks the mock's own chain with contracts/trust.js open(), the
// same function shell/trustlist.js walks it with, and it does so in three
// shapes:
//
//   1. the chain as the mock serves it            -> MUST open, at seq 2
//   2. version 2 re-signed with a FOREIGN key     -> MUST be refused
//   3. version 2 re-signed with a foreign key but
//      wearing an admin's key id                  -> MUST be refused
//
// Case 1 is the control, and it is not decoration. A pair of refusals proves
// nothing on its own: a chain that cannot be opened at all refuses every
// forgery just as convincingly, and then the two red-blooded assertions below
// measure a broken fixture. If case 1 ever goes red, no number in this file
// counts (Z:\GitHub\claude-md\messen.md, "eine Variante, die GELINGEN MUSS").
//
// It is also what says that cases 2 and 3 are refused at the line they mean.
// Their chain differs from case 1's in nothing but who signed version 2, so a
// green case 1 is the proof that version 1 opened and that the walk really
// reached version 2 before it refused. That is not hypothetical: in the first
// runs of this file version 1 failed its own signature, and then all three
// cases met that one earlier wall -- two of them red, and one GREEN, because
// the wall happened to say the words the case was waiting for.
//
// Cases 2 and 3 are the same forgery from two sides, and they fail on
// different lines of trust.js on purpose. Case 2 carries the key id of a key
// that stands in no version, so keyOwner finds no owner: "unknown signer".
// Case 3 carries anna's real key id over a signature her key did not make, so
// the owner is found and the signature is what has to fall: "bad signature".
// A walker that looked up the signer but never verified the bytes would pass
// case 2 and fail only case 3 -- which is why one of them would not do.
import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const anchorFile = join(here, "..", "trust-anchor.json");

// chainOf hands back the two versions as the mock really serves them, out of
// chainBytes and not out of the list objects: the anchor binds the EXACT bytes
// of version 1, and a re-encode could differ by a byte -- then this file would
// be walking a chain nobody serves.
//
// THROUGH session.parseTrustChain, which is the parser the shell uses, and not
// through JSON.parse. Measured on 03.11.2026, and it cost the first three runs
// of this file: trust.open expects a chain whose proofs have already been
// parsed -- proof.verify takes sig only as a Uint8Array and answers false, not
// throws, for the base64 text that stands in the JSON. So JSON.parse gave a
// chain whose version 1 failed its own signature, the control went red, and
// the two forgery cases went red and green for that same earlier wall rather
// than for the forgery. A case that is refused before the line it means
// measures the earlier wall; here the earlier wall was of the case's own
// making.
async function chainOf() {
  const world = await import("../mock/world.js");
  const session = await import("../../public/assets/js/contracts/session.js");
  const w = await world.buildWorld();
  return session.parseTrustChain(w.chainBytes).versions;
}

function anchor() {
  const file = JSON.parse(readFileSync(anchorFile, "utf8"));
  return {
    seq: file.seq,
    hash: Uint8Array.from(Buffer.from(
      file.hash.replace(/-/g, "+").replace(/_/g, "/"), "base64")),
  };
}

// Node's own WebCrypto, not a browser's: this file is about bytes, not about
// an engine. Where Ed25519 is missing the case must not quietly pass -- a
// guard that skips itself guards nothing -- so the missing capability is the
// failure, exactly as in contracts/anchor.spec.js.
async function needEd25519() {
  let have = true;
  try {
    await crypto.subtle.generateKey({ name: "Ed25519" }, false, ["sign", "verify"]);
  } catch {
    have = false;
  }
  expect(have, "this node has no Ed25519 in WebCrypto, so no chain can be walked").toBe(true);
}

// resign makes a version 2 that carries a signature by `signer` over the real
// payload of version 2, under the key id `kid`. Everything else -- the
// payload, the sequence number, the back-pointer -- stays exactly as the mock
// built it, so the ONLY thing these cases change is who signed. A forgery that
// also moved a field would be refused for the wrong reason, and the case would
// then measure the field and not the signature.
// sig stays BYTES here, for the reason chainOf gives: this is a parsed chain,
// and a base64 string in its place would be refused by proof.verify before any
// signature was looked at -- the forgery would then be turned away for the
// wrong reason and the case would prove nothing.
async function resign(v2, signer, kid) {
  const sign = await import("../../public/assets/js/contracts/sign.js");
  const proof = await import("../../public/assets/js/contracts/proof.js");
  const sig = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" },
    signer.priv, sign.message(sign.PURPOSE_TRUST_LIST, v2.payload)));
  return {
    payload: v2.payload,
    proof: {
      kind: proof.KIND_PASSWORD, kid, sig,
      authData: v2.proof.authData, clientData: v2.proof.clientData,
    },
  };
}

test.describe("the mock world's chain of two versions", () => {
  test("opens against the anchor and ends at version 2, which carries api-2", async () => {
    await needEd25519();
    const trust = await import("../../public/assets/js/contracts/trust.js");
    const chain = await chainOf();
    expect(chain.length, "the mock no longer serves two versions, so there is no growth to walk")
      .toBe(2);

    const cur = await trust.open(anchor(), chain);

    // seq 2, not "some version": walking a growing chain means arriving at
    // its NEWEST version, and arriving at version 1 is precisely the fault
    // this whole branch is about.
    expect(cur.list.seq, "the walk did not arrive at version 2").toBe(2);
    const ids = cur.list.nodes.filter((n) => n.kind === trust.NODE_API).map((n) => n.id);
    expect(ids, "version 2 does not carry api-2 beside api-1").toEqual(["api-1", "api-2"]);

    // And the other half of the same coin, stated rather than assumed: api-2
    // is in NO earlier version. Without this line the case above would also
    // pass on a mock whose version 1 already held api-2 -- and then the login
    // case in shell/trustgrowth.spec.js would be green for a reason that has
    // nothing to do with growth.
    const first = await trust.open(anchor(), chain.slice(0, 1));
    const early = first.list.nodes.filter((n) => n.kind === trust.NODE_API).map((n) => n.id);
    expect(early, "version 1 already carries api-2, so nothing here measures growth")
      .toEqual(["api-1"]);
  });

  test("refuses a version 2 signed with a foreign key", async () => {
    await needEd25519();
    const trust = await import("../../public/assets/js/contracts/trust.js");
    const sign = await import("../../public/assets/js/contracts/sign.js");
    const { ed25519FromSeed } = await import("../mock/seeded.js");
    const [v1, v2] = await chainOf();

    // A seed that stands in no version of the list. It is named here and not
    // in mock/seeded.js on purpose: a stranger's key does not belong in the
    // file that lists the world's own keys.
    const stranger = await ed25519FromSeed("stranger");
    const forged = await resign(v2, stranger, await sign.keyID(stranger.pub));

    await expect(trust.open(anchor(), [v1, forged]),
      "a version 2 signed by a key no version carries was accepted")
      .rejects.toThrow("unknown signer");
  });

  test("refuses a version 2 that wears an admin's key id over a foreign signature", async () => {
    await needEd25519();
    const trust = await import("../../public/assets/js/contracts/trust.js");
    const [v1, v2] = await chainOf();

    // The key id of the admin who really signed, lifted off the genuine
    // version 2 -- so the signer IS found and the signature is the only thing
    // left to fall. This is the shape a walker that trusts a key id would let
    // through.
    const stranger = await (await import("../mock/seeded.js"))
      .ed25519FromSeed("stranger");
    const forged = await resign(v2, stranger, v2.proof.kid);

    await expect(trust.open(anchor(), [v1, forged]),
      "a version 2 was accepted on a signature its named signer did not make")
      .rejects.toThrow("bad signature");
  });
});
