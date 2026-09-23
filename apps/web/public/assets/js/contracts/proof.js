// Package proof: a proof that a user approved a payload - with the key the
// browser derives from the password (password key) or with a passkey
// (Go: proof).
//
// Go is handed a Proof and a UserKey whose fields have their types from the
// compiler: a []byte is bytes, a Kind is text, a uint32 is a whole number.
// Here every one of them comes from an object whoever calls owns, and can be
// missing, be of another kind, or fight back with a getter. None of it is
// guessed and none of it is coerced: "65536" is no memory size, a string of
// 40 characters is no authenticator data, and what cannot be looked at is
// refused. Whatever is left out closes the gate, it never opens it.
//
// The cryptography itself is the Web Crypto API of the browser and nothing
// else. An engine whose Web Crypto has no Ed25519 - the WebKit that
// Playwright brings is one - can tell no right signature from a wrong one, so
// it refuses every one of them.
//
// The one exception is Argon2id, which the Web Crypto API does not have - it
// stretches a password with PBKDF2 and with nothing else. It is not built
// here either: it is copied into the repository as it was published
// (apps/web/public/assets/vendor/noble-hashes/, its line in THIRD-PARTY.md,
// where it comes from and how to check that in its SOURCE.md). deriveSeed
// hands the work to a worker, so this module pulls those 90 KB only when a
// password is really derived.
import { b64decode, b64encode, concat, utf8, fromUtf8, equal } from "./b64.js";
import { S, A, P, str, bytes, uint32 } from "./json.js";
import { message, keyIDAsync, verifyRaw, MAX_PAYLOAD } from "./sign.js";

// Kind tells password keys and passkeys apart (Go: proof.Kind).
export const KIND_PASSWORD = "password";
export const KIND_PASSKEY = "passkey";

// Signature algorithms.
export const ALG_EDDSA = "EdDSA";
export const ALG_ES256 = "ES256";
export const ALG_RS256 = "RS256";

// Argon2id parameters for the password key.
export const KDF_ARGON2ID = "argon2id";
export const SALT_SIZE = 16;
export const DEFAULT_MEM_KIB = 65536;
export const DEFAULT_TIME = 3;
export const DEFAULT_PAR = 1;
export const MIN_MEM_KIB = 19456;
export const MAX_MEM_KIB = 1 << 20;

// Size limits. They are generous for all three allowed algorithms (RSA-4096
// is the largest) but bound the cost of parsing a key or a signature from
// outside before any expensive cryptography runs.
export const MAX_PUBLIC = 1024;
export const MAX_SIG = 1024;
export const MAX_RSA_BITS = 4096;
export const MAX_AUTH_DATA = 4096;
export const MAX_CLIENT_DATA = 4096;
// The longest credential ID an authenticator may hand out (WebAuthn, CTAP2).
export const MAX_CRED_ID = 1023;

// The smallest RSA key NGC takes for RS256 (Go: parsePublic).
const MIN_RSA_BITS = 2048;
// The fixed start of WebAuthn authenticator data: SHA-256 of the RP ID, one
// byte of flags and the 4-byte signature counter (Go: minAuthData).
const MIN_AUTH_DATA = 37;
// A key id is base64url of 16 bytes, and that takes 22 characters. Both
// numbers stand in sign.js as well; here they are counted out from the size,
// because two places with the same number are two places that can drift
// apart.
const KEY_ID_SIZE = 16;
const KEY_ID_CHARS = 22;
// Go: ed25519.PublicKeySize and ed25519.SeedSize.
const ED25519_SIZE = 32;
// The two flags of the authenticator: the user was there, and the user was
// verified with a PIN or with biometrics.
const FLAG_UP = 0x01;
const FLAG_UV = 0x04;
// Half of an ES256 signature: r and s are each 32 bytes on P-256.
const P256_SCALAR = 32;

const EMPTY = new Uint8Array(0);

// Everything this module refuses on its own is this one error, as it is a
// handful of errors in Go that no caller tells apart. An error of the engine
// stays as it is and shows up as the mistake it would be.
export class BadProof extends Error {}

// The shapes of the contract (Go: the struct tags of KDF, RP, UserKey and
// Proof). Go reads Par into a uint8 and this module has no shape for one, so
// p stands here as uint32; validateKDF holds it to 1..4 either way, which is
// narrower than both.
export const kdfShape = P({ alg: str, salt: bytes, m: uint32, t: uint32, p: uint32 });
export const rpShape = S({ id: str, origins: A(str) });
export const userKeyShape = S({ id: str, kind: str, alg: str, pub: bytes, credId: bytes, kdf: kdfShape });
export const proofShape = S({ kind: str, kid: str, sig: bytes, authData: bytes, clientData: bytes });

// saltBytes reads the salt of a KDF as it stands in a document: base64url,
// strictly, as everywhere else.
export const saltBytes = (s) => b64decode(s);

// validKeyID reports whether s names a user key: base64url of 16 bytes, as
// sign.keyID writes it (Go: proof.ValidKeyID).
function validKeyID(s) {
  // The length comes first, so an oversized key id is never decoded.
  if (typeof s !== "string" || s.length !== KEY_ID_CHARS) return false;
  try {
    return b64decode(s).length === KEY_ID_SIZE;
  } catch {
    return false;
  }
}

// kdfFields reads the five parameters of a KDF exactly once and hands them
// back as plain values, or null where they are not the ones the contract
// allows (Go: proof.KDF.Validate). Once, because Go is given a struct whose
// fields answer every read alike, while here each one can be a getter: a
// memory size that reads as 65536 for the check and as something else for
// Argon2id would carry the whole range away between the two reads. It never
// throws; a field that fights back is no field.
function kdfFields(k) {
  try {
    if (k === null || typeof k !== "object") return null;
    const { alg, salt, m, t, p } = k;
    if (alg !== KDF_ARGON2ID) return null;
    if (!(salt instanceof Uint8Array) || salt.length !== SALT_SIZE) return null;
    // Go has uint32 and uint8 and cannot be handed anything else. Here
    // "65536" compares to true against every bound, and undefined to false
    // against all of them: a parameter that is no whole number is no
    // parameter, and is refused before it is compared.
    if (!Number.isInteger(m) || !Number.isInteger(t) || !Number.isInteger(p)) return null;
    if (m < MIN_MEM_KIB || m > MAX_MEM_KIB || t < 1 || t > 10 || p < 1 || p > 4) return null;
    return { salt, m, t, p };
  } catch {
    return null;
  }
}

// validateKDF checks the Argon2id parameters against the allowed ranges
// (Go: proof.KDF.Validate). It answers true or false and never throws.
export function validateKDF(k) {
  return kdfFields(k) !== null;
}

// The worker that computes Argon2id, next to this module rather than at a
// path of its own, so moving assets/ moves it along.
const ARGON2_WORKER = new URL("../auth/argon2-worker.js", import.meta.url);

// deriveSeed turns the typed password into the 32-byte seed of the Ed25519
// password key (Go: proof.DeriveKey, which begins with KDF.Validate just the
// same and answers ErrKDF).
//
// The password is the text the user typed, normalized to NFC and then taken
// as UTF-8, exactly as the contract writes it down: a password typed in NFD
// gives the same key, which is what proof_kdf.json holds a case for. Bytes or
// a number are no password - what is not text has no normal form.
//
// Every parameter comes from the KDF of the document and none from a default
// of the library: a memory size, a number of passes or a parallelism that is
// missing is an error, never a value quietly filled in. The length of the
// seed is this module's, not the caller's.
//
// The work runs in a worker. Argon2id is meant to be slow - about a second at
// m=65536, with 64 MiB of working memory - and the window has to keep
// answering meanwhile. The password's buffer is handed over and not copied,
// so it lives in one place, which the worker wipes and which is thrown away
// with the worker.
export async function deriveSeed(passwordText, kdf) {
  if (typeof passwordText !== "string") throw new BadProof("proof: a password is text");
  const k = kdfFields(kdf);
  if (k === null) throw new BadProof("proof: KDF parameters out of range");
  const password = utf8(passwordText.normalize("NFC"));
  return inWorker(password, k);
}

function inWorker(password, k) {
  return new Promise((resolve, reject) => {
    let w;
    try {
      w = new Worker(ARGON2_WORKER, { type: "module" });
    } catch {
      throw new BadProof("proof: no worker for Argon2id");
    }
    // Whatever comes back first ends the worker: an answer, an error of the
    // worker, a message this side cannot read.
    const end = (fn) => (e) => {
      w.terminate();
      fn(e);
    };
    w.onmessage = end((e) => {
      const seed = e.data;
      if (seed instanceof Uint8Array && seed.length === ED25519_SIZE) resolve(seed);
      else reject(new BadProof("proof: Argon2id gave no seed"));
    });
    w.onerror = end(() => reject(new BadProof("proof: Argon2id failed")));
    w.onmessageerror = end(() => reject(new BadProof("proof: Argon2id failed")));
    // The buffer is transferred, not cloned: after this line the password is
    // gone from this thread. The salt is the caller's and is copied, so his
    // KDF keeps it.
    w.postMessage({ password, salt: k.salt, t: k.t, m: k.m, p: k.p, dkLen: ED25519_SIZE },
      [password.buffer]);
  });
}

// asBytes reads a field that Go holds as enc.Bytes: bytes, or nothing at all
// where Go would have a nil slice. Anything else is no answer - a string has
// a length just as bytes do, and a string of 40 characters would walk through
// every length check below as authenticator data.
function asBytes(v) {
  if (v === undefined || v === null) return EMPTY;
  return v instanceof Uint8Array ? v : null;
}

// validateProof checks the structure of p before any cryptography runs: the
// key id is base64url of 16 bytes; the signature has 1 to MAX_SIG bytes; a
// password proof carries no authenticator data and no client data; a passkey
// proof carries MIN_AUTH_DATA to MAX_AUTH_DATA bytes of authenticator data
// and 1 to MAX_CLIENT_DATA bytes of client data (Go: proof.Proof.Validate).
// It answers true or false and never throws.
export function validateProof(p) {
  try {
    if (p === null || typeof p !== "object") return false;
    const { kind, kid, sig } = p;
    if (!validKeyID(kid)) return false;
    if (!(sig instanceof Uint8Array) || sig.length === 0 || sig.length > MAX_SIG) return false;
    const auth = asBytes(p.authData);
    const client = asBytes(p.clientData);
    if (auth === null || client === null) return false;
    if (kind === KIND_PASSWORD) return auth.length === 0 && client.length === 0;
    if (kind === KIND_PASSKEY) {
      return auth.length >= MIN_AUTH_DATA && auth.length <= MAX_AUTH_DATA
        && client.length > 0 && client.length <= MAX_CLIENT_DATA;
    }
    return false;
  } catch {
    return false;
  }
}

// validateUserKey checks the structure of a key of the trust list
// (Go: proof.UserKey.Validate). It is a promise because the key id is a
// SHA-256, and SHA-256 in the Web Crypto API answers with one. It answers
// true or false and never throws.
export async function validateUserKey(k) {
  try {
    if (k === null || typeof k !== "object") return false;
    const { id, kind, alg, pub, kdf } = k;
    const cred = asBytes(k.credId);
    if (!(pub instanceof Uint8Array) || cred === null) return false;
    if (typeof id !== "string" || id !== (await keyIDAsync(pub))) return false;
    if (kind === KIND_PASSWORD) {
      // Go: KDF is a pointer, and a password key without one is no key.
      return alg === ALG_EDDSA && pub.length === ED25519_SIZE && cred.length === 0 && validateKDF(kdf);
    }
    if (kind === KIND_PASSKEY) {
      if (cred.length === 0 || cred.length > MAX_CRED_ID || pub.length > MAX_PUBLIC) return false;
      if (kdf !== undefined && kdf !== null) return false;
      return (await importPublic(alg, pub)) !== null;
    }
    return false;
  } catch {
    return false;
  }
}

// The PKCS#8 head of an Ed25519 private key (RFC 8410), the 32 bytes of the
// seed after it. It is the one form the Web Crypto API imports a seed in.
const ED25519_PKCS8 = new Uint8Array([0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b,
  0x65, 0x70, 0x04, 0x22, 0x04, 0x20]);

function seedBytes(seed) {
  if (!(seed instanceof Uint8Array) || seed.length !== ED25519_SIZE) {
    throw new BadProof("proof: a seed is 32 bytes");
  }
  return concat(ED25519_PKCS8, seed);
}

// privateFromSeed makes the password key of a seed. Not extractable: once
// imported, the key cannot leave the browser again.
export async function privateFromSeed(seed) {
  return crypto.subtle.importKey("pkcs8", seedBytes(seed), { name: "Ed25519" }, false, ["sign"]);
}

// publicFromSeed gives the public key that belongs to a seed
// (Go: ed25519.NewKeyFromSeed(seed).Public()). The Web Crypto API hands out
// no public key of a private one, so the seed is imported once as extractable
// and its JWK read: that key lives inside this call and nowhere else, while
// the key privateFromSeed hands out - the one that signs - is never
// extractable.
export async function publicFromSeed(seed) {
  const extractable = await crypto.subtle.importKey("pkcs8", seedBytes(seed),
    { name: "Ed25519" }, true, ["sign"]);
  const jwk = await crypto.subtle.exportKey("jwk", extractable);
  return b64decode(jwk.x);
}

// challenge returns the WebAuthn challenge for a payload: SHA-256 over the
// bytes a signature would cover (Go: proof.Challenge).
export async function challenge(purpose, payload) {
  return new Uint8Array(await crypto.subtle.digest("SHA-256", message(purpose, payload)));
}

// verify checks that p is a proof by key over payload for purpose
// (Go: proof.Verify). key has to be self-consistent: its id is the hash of
// its public key.
//
// It answers false where it cannot check, and never throws - a caller who had
// to catch an exception around a signature is a caller who lets one through
// the moment the engine surprises him. Everything stands in one try, so that
// promise holds for the whole function and not call by call.
//
// validateProof is not run here, because Go does not run it either: Verify
// checks what it needs itself, and the parser of every document that carries
// a proof has run Validate.
export async function verify(p, key, rp, purpose, payload) {
  try {
    if (p === null || typeof p !== "object" || key === null || typeof key !== "object") return false;
    // Every field is read once and only the copy is used after that. Go is
    // handed structs whose fields answer every read the same way; here a
    // field can be a getter, and one that answered the length check with 64
    // bytes and the Web Crypto API with a megabyte would carry MAX_SIG away
    // between the two reads. The same for the algorithm: read twice, it could
    // name one kind of key to importPublic and another to the check itself.
    const { kind, kid, sig } = p;
    const { id, kind: keyKind, alg, pub } = key;
    if (!(pub instanceof Uint8Array) || !(sig instanceof Uint8Array)) return false;
    // In the order Go has it: the key first - a key whose id is not the hash
    // of its public key is no key - then the key the proof names, and only
    // then anything that costs.
    if (typeof id !== "string" || id !== (await keyIDAsync(pub))) return false;
    if (kid !== id || kind !== keyKind) return false;
    if (sig.length > MAX_SIG) return false;
    if (keyKind === KIND_PASSWORD) {
      if (alg !== ALG_EDDSA || pub.length !== ED25519_SIZE) return false;
      // The one Ed25519 check of the web side, the same one sign.verify uses.
      // Go looks at neither authData nor clientData of a password proof, and
      // neither does this.
      return await verifyRaw(pub, sig, message(purpose, payload));
    }
    if (keyKind === KIND_PASSKEY) {
      const auth = asBytes(p.authData);
      const client = asBytes(p.clientData);
      if (auth === null || client === null) return false;
      return await verifyAssertion({ sig, auth, client, alg, pub }, rp, await challenge(purpose, payload));
    }
    return false;
  } catch {
    return false;
  }
}

// verifyAssertion checks a WebAuthn assertion (Go: verifyAssertion). It is
// handed the fields verify has already read, never the proof itself, so
// nothing here reads anything a second time. It runs inside the try of
// verify, so it may say no by throwing as well.
async function verifyAssertion(a, rp, ch) {
  const { sig, auth, client, alg, pub } = a;
  // The size before the parse, so a client data of any size is never read.
  if (client.length > MAX_CLIENT_DATA) return false;
  let cd;
  try {
    // WebAuthn lets the browser add members of its own, so this one document
    // is read leniently - it is the single exception to the strict JSON of
    // the contracts (shared/README.md). Only four members count.
    cd = JSON.parse(fromUtf8(client));
  } catch {
    return false;
  }
  if (cd === null || typeof cd !== "object") return false;
  if (cd.type !== "webauthn.get") return false;
  // Go reads crossOrigin into a bool and refuses a document in which it is
  // anything else. JSON.parse takes every value, so a crossOrigin that is no
  // boolean is refused here by hand instead of quietly counting as false.
  // Go leaves the field at false for a JSON null, and so does this.
  const cross = cd.crossOrigin;
  if (cross !== undefined && cross !== null && typeof cross !== "boolean") return false;
  if (cross === true) return false;
  if (cd.challenge !== b64encode(ch)) return false;
  // The origin is compared as text against the list of the relying party, in
  // full: an origin that only starts with one of them is another origin.
  if (rp === null || typeof rp !== "object") return false;
  const { id: rpID, origins } = rp;
  if (!Array.isArray(origins) || !origins.includes(cd.origin)) return false;
  if (auth.length < MIN_AUTH_DATA || auth.length > MAX_AUTH_DATA) return false;
  if (typeof rpID !== "string") return false;
  const rpHash = new Uint8Array(await crypto.subtle.digest("SHA-256", utf8(rpID)));
  if (!equal(auth.subarray(0, 32), rpHash)) return false;
  // NGC always asks for user verification: a PIN or biometrics, never a tap
  // alone.
  const flags = auth[32];
  if ((flags & FLAG_UP) === 0 || (flags & FLAG_UV) === 0) return false;
  const cdHash = new Uint8Array(await crypto.subtle.digest("SHA-256", client));
  const signed = concat(auth, cdHash);
  const pubKey = await importPublic(alg, pub);
  if (pubKey === null) return false;
  if (alg === ALG_ES256) {
    const raw = derToRaw(sig);
    if (raw === null) return false;
    return (await crypto.subtle.verify({ name: "ECDSA", hash: "SHA-256" }, pubKey, raw, signed)) === true;
  }
  if (alg === ALG_EDDSA) {
    return (await crypto.subtle.verify({ name: "Ed25519" }, pubKey, sig, signed)) === true;
  }
  if (alg === ALG_RS256) {
    return (await crypto.subtle.verify({ name: "RSASSA-PKCS1-v1_5" }, pubKey, sig, signed)) === true;
  }
  return false;
}

// importPublic reads a SubjectPublicKeyInfo and gives the key back only if it
// is the kind alg names: P-256 for ES256, Ed25519 for EdDSA, RSA of 2048 to
// MAX_RSA_BITS bits for RS256 (Go: parsePublic). Go parses the key and then
// holds it against alg; the Web Crypto API is asked for the one kind alg
// names and refuses every other, which comes to the same. Anything it cannot
// import - a key of another curve, a key that is no key, an engine without
// Ed25519 - is no key: null, never an exception.
async function importPublic(alg, pub) {
  try {
    if (!(pub instanceof Uint8Array) || pub.length > MAX_PUBLIC) return null;
    if (alg === ALG_ES256) {
      return await crypto.subtle.importKey("spki", pub, { name: "ECDSA", namedCurve: "P-256" }, false, ["verify"]);
    }
    if (alg === ALG_EDDSA) {
      return await crypto.subtle.importKey("spki", pub, { name: "Ed25519" }, false, ["verify"]);
    }
    if (alg === ALG_RS256) {
      const k = await crypto.subtle.importKey("spki", pub,
        { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" }, false, ["verify"]);
      const bits = k.algorithm.modulusLength;
      if (!Number.isInteger(bits) || bits < MIN_RSA_BITS || bits > MAX_RSA_BITS) return null;
      return k;
    }
  } catch {
    return null;
  }
  return null;
}

// derToRaw turns the ASN.1 signature a passkey returns for ES256 into the
// 64-byte form the Web Crypto API verifies, and answers null where the bytes
// are no such signature.
//
// It reads DER as strictly as Go does (crypto/ecdsa: VerifyASN1 over
// crypto/cryptobyte): one SEQUENCE that fills the signature to the last byte,
// two INTEGERs in it and nothing behind them, each in its shortest form and
// each of at most 32 bytes. A reader that took every spelling would give one
// signature many, and two proofs that are the same signature would look like
// two - which is exactly what Go refuses here.
function derToRaw(der) {
  if (!(der instanceof Uint8Array)) return null;
  let i = 0;
  const byte = () => {
    if (i >= der.length) throw new BadProof("proof: bad ECDSA signature");
    return der[i++];
  };
  // Short form only: r and s of P-256 are at most 33 bytes each, so neither
  // they nor the SEQUENCE around them ever reach 128 bytes, and DER asks for
  // the shortest length there is.
  const length = () => {
    const n = byte();
    if (n > 0x7f) throw new BadProof("proof: bad ECDSA signature");
    return n;
  };
  const scalar = () => {
    if (byte() !== 0x02) throw new BadProof("proof: bad ECDSA signature");
    const n = length();
    if (n === 0 || i + n > der.length) throw new BadProof("proof: bad ECDSA signature");
    const v = der.subarray(i, i + n);
    i += n;
    // A leading bit that is set makes an ASN.1 integer negative, and a
    // leading zero that nothing needs is a second spelling of one number.
    if ((v[0] & 0x80) !== 0) throw new BadProof("proof: bad ECDSA signature");
    if (v[0] === 0 && (n === 1 || (v[1] & 0x80) === 0)) throw new BadProof("proof: bad ECDSA signature");
    const digits = v[0] === 0 ? v.subarray(1) : v;
    if (digits.length > P256_SCALAR) throw new BadProof("proof: bad ECDSA signature");
    const out = new Uint8Array(P256_SCALAR);
    out.set(digits, P256_SCALAR - digits.length);
    return out;
  };
  try {
    if (byte() !== 0x30) return null;
    if (length() !== der.length - i) return null;
    const r = scalar();
    const s = scalar();
    if (i !== der.length) return null;
    return concat(r, s);
  } catch (e) {
    if (e instanceof BadProof) return null;
    throw e;
  }
}

// MAX_PAYLOAD belongs to sign and is handed on from here, so that code which
// builds a proof needs one import for the whole job.
export { MAX_PAYLOAD };
