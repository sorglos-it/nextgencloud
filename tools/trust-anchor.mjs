// Derives the trust anchor of the mock world and prints it.
//
// WHY A TOOL AND NOT A NUMBER IN A FILE. apps/web/tests/trust-anchor.json used
// to hold a filler pattern (0xAB repeated), because mock/world.js built random
// keys on every run and no fixed number could ever match. The anchor is now
// DERIVED from the seeds in mock/seeded.js, so there is exactly one place that
// decides it -- the seeds -- and the file beside them is a copy that this tool
// writes.
//
// The case that guards it (apps/web/tests/contracts/anchor.spec.js) derives the
// anchor again from the seeds and compares it with the file. It does NOT read
// the file twice: two constants confirming each other are the placeholder in
// better clothes.
//
//   node tools/trust-anchor.mjs            prints seq and hash
//   node tools/trust-anchor.mjs --write    writes apps/web/tests/trust-anchor.json
//
// It needs a WebCrypto with Ed25519, which node 22 and later have. Where it is
// missing the tool says so and exits 2 rather than printing a hash it could not
// compute.
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath, pathToFileURL } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const repo = join(here, "..");
const anchorFile = join(repo, "apps", "web", "tests", "trust-anchor.json");

try {
  await crypto.subtle.generateKey({ name: "Ed25519" }, false, ["sign", "verify"]);
} catch {
  console.error("trust-anchor: this node has no Ed25519 in WebCrypto");
  process.exit(2);
}

// pathToFileURL and not a string with slashes swapped: a Windows path handed
// to import() as text is not a URL, and patching it by hand is how the first
// draft of this tool broke.
const trust = await import(
  pathToFileURL(join(repo, "apps/web/public/assets/js/contracts/trust.js")).href);
const world = await import(
  pathToFileURL(join(repo, "apps/web/tests/mock/world.js")).href);

const w = await world.buildWorld();
// The anchor binds the EXACT bytes of one version, so it is taken from the
// chain the mock really hands out and not from the list object: a re-encode
// could differ by a byte and then the anchor would be of something nobody
// serves.
const chain = JSON.parse(new TextDecoder().decode(w.chainBytes));
const signed = chain.versions[0];
const hash = await trust.hash({
  payload: b64(signed.payload),
  proof: signed.proof,
});

const out = { seq: 1, hash: b64url(hash) };
const text = JSON.stringify(out) + "\n";

if (process.argv.includes("--write")) {
  writeFileSync(anchorFile, text);
  console.log(`trust-anchor: wrote ${anchorFile}`);
} else {
  process.stdout.write(text);
  try {
    const have = JSON.parse(readFileSync(anchorFile, "utf8"));
    const same = have.seq === out.seq && have.hash === out.hash;
    console.error(same ? "trust-anchor: the file agrees"
      : `trust-anchor: the file DISAGREES (file ${have.hash}, derived ${out.hash})`);
  } catch {
    console.error("trust-anchor: no file to compare with");
  }
}

function b64(s) {
  const b = Buffer.from(s, "base64");
  return new Uint8Array(b);
}
function b64url(bytes) {
  return Buffer.from(bytes).toString("base64")
    .replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
