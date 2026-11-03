// Deterministic keys for the mock world.
//
// WHY THIS FILE EXISTS. mock/world.js used to build every key with
// crypto.subtle.generateKey, so version 1 of its trust list carried different
// bytes on every run and its hash with them. A fixed trust anchor therefore
// COULD NOT match -- and that, not carelessness, is why
// apps/web/tests/trust-anchor.json held a filler pattern (0xAB repeated) and
// why the cases had to bypass the anchor check with standInOn. One cause,
// three consequences; the bypass was the scar, not the wound.
//
// The consequence worth naming: the anchor check of the whole web shell had
// never once been exercised. A bolt no case has ever touched is an unknown
// bolt.
//
// So the keys come from seeds that stand in this file, in the repository, and
// the anchor is DERIVED from them by tools/trust-anchor.mjs rather than
// written down beside them. The case that guards it derives the anchor again
// from these seeds and compares -- it does not read the anchor file and hold
// it against the anchor file. Two constants confirming each other are the
// placeholder in better clothes.
//
// THE SEEDS ARE TEST SEEDS AND NOTHING ELSE. They are in a public repository
// and they sign a list no installation believes. Nothing here may be reused
// for a plant; a key that stands in a repository is a key everybody has.

// Ed25519 and X25519 private keys go into WebCrypto as PKCS#8. For a 32-byte
// seed that wrapper is a fixed prefix, so it is written out rather than pulled
// in as a dependency: 302e020100 300506032b65 7004220420 <32 bytes>, where
// 2b6570 is id-Ed25519 and 2b656e is id-X25519.
const PKCS8_ED25519 = "302e020100300506032b657004220420";
const PKCS8_X25519 = "302e020100300506032b656e04220420";

const unhex = (s) => {
  const out = new Uint8Array(s.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16);
  return out;
};

// seedBytes makes a 32-byte seed out of a name, so a reader can see which key
// is which instead of counting hex. The name is padded, never hashed: a hash
// would make the seeds depend on a digest this file would then have to pin.
function seedBytes(name) {
  const out = new Uint8Array(32);
  const text = new TextEncoder().encode(`ngc-test-seed/${name}`);
  if (text.length > out.length) throw new Error("seeded: name too long for one seed");
  out.set(text);
  // The tail is not left at zero: a seed of mostly zeroes is a seed somebody
  // will later mistake for an unset value.
  for (let i = text.length; i < out.length; i++) out[i] = (i * 7 + 13) & 0xff;
  return out;
}

export const SEED = Object.freeze({
  // The key that signs version 1 of the list, and with it the anchor.
  listV1: "list-v1",
  // The key that signs version 2. It has to be a key version 1 already
  // carries, because trust.open holds every later version against the keys of
  // the one before it.
  listV2: "list-v1",
  apiSign1: "api-1-sign",
  apiSign2: "api-2-sign",
  apiNoise1: "api-1-noise",
  apiNoise2: "api-2-noise",
  ctlNoise: "ctl-1-noise",
  // Not a key but a pin: 32 bytes that stand for api-2's TLS certificate.
  apiTls2: "api-2-tls",
});

// ed25519FromBytes is the same thing for a seed that already exists as bytes:
// the password key of an admin of the mock world, which proof.deriveSeed makes
// out of the test password. Version 1 of a list SIGNS ITSELF and only an admin
// of it may have signed (contracts/trust.js, open), so the mock has to sign
// with a key the list really carries -- and that is this one.
export async function ed25519FromBytes(seed) {
  if (!(seed instanceof Uint8Array) || seed.length !== 32) {
    throw new Error("seeded: a seed is 32 bytes");
  }
  const hex = [...seed].map((b) => b.toString(16).padStart(2, "0")).join("");
  const priv = await crypto.subtle.importKey("pkcs8", unhex(PKCS8_ED25519 + hex),
    { name: "Ed25519" }, false, ["sign"]);
  const k = await crypto.subtle.importKey("pkcs8", unhex(PKCS8_ED25519 + hex),
    { name: "Ed25519" }, true, ["sign"]);
  const jwk = await crypto.subtle.exportKey("jwk", k);
  return { priv, pub: b64urlBytes(jwk.x) };
}

export async function ed25519FromSeed(name) {
  const priv = await crypto.subtle.importKey(
    "pkcs8", unhex(PKCS8_ED25519 + [...seedBytes(name)].map(
      (b) => b.toString(16).padStart(2, "0")).join("")),
    { name: "Ed25519" }, false, ["sign"]);
  // The public key cannot be exported out of a non-extractable private one, so
  // it is derived by signing nothing and is instead taken from a second import
  // of the same seed as a public key -- WebCrypto has no "public of private",
  // which is the one awkward part of doing this without a library.
  const pub = await publicOfEd25519(name);
  return { priv, pub };
}

// publicOfEd25519 is the one place that needs the seed's public half. It goes
// through an EXTRACTABLE import so the raw bytes can come back out; the
// extractable copy is used for nothing else and is not returned.
async function publicOfEd25519(name) {
  const seed = seedBytes(name);
  const hex = [...seed].map((b) => b.toString(16).padStart(2, "0")).join("");
  const k = await crypto.subtle.importKey("pkcs8", unhex(PKCS8_ED25519 + hex),
    { name: "Ed25519" }, true, ["sign"]);
  const jwk = await crypto.subtle.exportKey("jwk", k);
  return b64urlBytes(jwk.x);
}

export async function x25519FromSeed(name) {
  const hex = [...seedBytes(name)].map((b) => b.toString(16).padStart(2, "0")).join("");
  const priv = await crypto.subtle.importKey("pkcs8", unhex(PKCS8_X25519 + hex),
    { name: "X25519" }, false, ["deriveBits"]);
  const k = await crypto.subtle.importKey("pkcs8", unhex(PKCS8_X25519 + hex),
    { name: "X25519" }, true, ["deriveBits"]);
  const jwk = await crypto.subtle.exportKey("jwk", k);
  return { priv, pub: b64urlBytes(jwk.x) };
}

function b64urlBytes(s) {
  const b = atob(s.replace(/-/g, "+").replace(/_/g, "/"));
  const out = new Uint8Array(b.length);
  for (let i = 0; i < b.length; i++) out[i] = b.charCodeAt(i);
  return out;
}
