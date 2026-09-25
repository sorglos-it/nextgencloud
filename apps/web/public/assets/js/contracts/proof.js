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
import { b64decode, b64encode, concat, utf8, fromUtf8, equal, viewOf } from "./b64.js";
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

// The longest relying party ID, the length of a DNS name. Exported since
// task 8: Go has it as proof.MaxRPID, and a later task that looks for the
// number should find it rather than write 253 a second time. The name stays
// MAX_RP_ID, the one this file already uses - a second name for one number is
// the drift this file warns about elsewhere.
export const MAX_RP_ID = 253;
const RP_ID = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$/;

// validRPID reports whether s is a relying party ID: a DNS name in lower case
// of at most MAX_RP_ID bytes, without scheme, port or path, as WebAuthn asks
// (Go: proof.ValidRPID). trust.validateList asks this one function for a
// non-empty rp.id, the same rule Go's trust.List.Validate and config.Web
// keep - url.js already has a very similar validDNSName for hosts, but it is
// not exported and is a little stricter than WebAuthn's own grammar (it also
// refuses an all-digit last label, which an RP ID grammar has no reason to),
// so it is not reused here for something that is a different rule with a
// different owner.
export const validRPID = (s) => typeof s === "string" && s !== "" && s.length <= MAX_RP_ID && RP_ID.test(s);

// saltBytes reads the salt of a KDF as it stands in a document: base64url,
// strictly, as everywhere else.
export const saltBytes = (s) => b64decode(s);

// validKeyID reports whether s names a user key: base64url of 16 bytes, as
// sign.keyID writes it (Go: proof.ValidKeyID). Exported since task 8, where
// session.validatePasskeyRef holds the key ID of a passkey to it: a passkey
// the login answer offers and the proof the shell then builds name the key
// by the same rule, as Go's PasskeyRef.Validate and Proof.Validate do.
export function validKeyID(s) {
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

// How long a derivation may take before it counts as stuck, rather than as
// slow. Argon2id's work grows with m and with t; p splits that work into
// lanes without adding to it, so the budget does not count p.
//
// Measured on the machine this was written on, in the slower of the two test
// engines: m=65536 t=3 took 1.9 to 2.2 s and m=19456 t=2 took 0.44 to 0.46 s,
// which is about 12 microseconds per KiB-pass. Allowed here are 500 of them,
// some forty times that, so a phone many times slower than this desktop still
// finishes well inside the budget; the ten seconds on top are for starting
// the worker and fetching its module. What misses this has not got slow, it
// has got stuck - and for someone who has just typed his password, a sign-in
// that neither finishes nor fails is the worst of the three endings.
const DERIVE_START_MS = 10_000;
const DERIVE_PER_KIB_PASS_MS = 0.5;
// And a ceiling, because the parameters come from a document the server
// stored and not from this page. The contract still allows m=1 GiB and t=10,
// and the budget above would give that 87 minutes - which, for the person who
// has just typed his password, is the same as no deadline at all. The number
// is not invented: 108 s is the budget of the heaviest parameters anyone
// actually uses (m=65536, t=3), so two minutes clears everything that is
// really derived and cuts off only what the contract has yet to narrow
// (V1.2). Its cost is named: on a phone many times slower than this desktop,
// the very largest parameter sets the contract still allows could now run
// into the ceiling and fail. A sign-in that fails is still a better ending
// than one that never comes back.
const DERIVE_MAX_MS = 120_000;

// wipe clears bytes that are still here. fill(0) on a buffer that has been
// handed over to a worker throws, and the array's length reads as zero then
// (measured in both engines), so a wipe asks first: where the transfer went
// through, nothing is left here to wipe, because the bytes went with it.
function wipe(u8) {
  if (u8.length > 0) u8.fill(0);
}

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
// The work runs in a worker. Argon2id is meant to be slow - measured at
// m=65536 t=3 it takes about 1.0 s in Chromium and about 2.0 s in WebKit,
// with 64 MiB of working memory - and the window has to keep answering
// meanwhile. The password's buffer is handed over and not copied, so it lives
// in one place: the worker wipes it and is thrown away right after. Where it
// never gets that far, this side wipes it instead, and a worker that says
// nothing runs into a deadline rather than into no end at all.
//
// That holds for the buffer, and only for it. The text itself is beyond this
// module in exactly the way publicFromSeed's JWK is: the string the caller
// handed in, and the one .normalize("NFC") makes of it - the same string
// again where it was already NFC, a second one where it was not - cannot be
// written to and live until the garbage collector takes them, and the caller
// holds the first of them anyway. What the worker is given can be cleared and
// is; the typed text cannot, and that is said here rather than covered by
// "one place".
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
      // Nobody took the buffer, so the password is still here - and it is
      // wiped before the caller is told, not left behind on the one way out
      // of this function that never reaches the worker.
      wipe(password);
      reject(new BadProof("proof: no worker for Argon2id"));
      return;
    }
    // Whatever comes first ends the worker: an answer, an error of the
    // worker, a message this side cannot read, or the deadline. Ending it
    // takes its memory with it, and with that whatever the password left
    // there.
    let deadline = 0;
    const stop = () => {
      clearTimeout(deadline);
      w.terminate();
    };
    const end = (fn) => (e) => {
      stop();
      fn(e);
    };
    w.onmessage = end((e) => {
      const seed = e.data;
      if (seed instanceof Uint8Array && seed.length === ED25519_SIZE) resolve(seed);
      else reject(new BadProof("proof: Argon2id gave no seed"));
    });
    w.onerror = end(() => reject(new BadProof("proof: Argon2id failed")));
    w.onmessageerror = end(() => reject(new BadProof("proof: Argon2id failed")));
    // A worker that is built, takes the password and then says nothing - one
    // the engine killed for want of memory, one whose module never arrived -
    // would leave this promise open for good. It gets a deadline instead.
    deadline = setTimeout(() => {
      stop();
      reject(new BadProof("proof: Argon2id did not answer"));
    }, Math.min(DERIVE_START_MS + k.m * k.t * DERIVE_PER_KIB_PASS_MS, DERIVE_MAX_MS));
    // The buffer is transferred, not cloned: after this line the password is
    // gone from this thread. The salt is the caller's and is copied, so his
    // KDF keeps it.
    try {
      w.postMessage({ password, salt: k.salt, t: k.t, m: k.m, p: k.p, dkLen: ED25519_SIZE },
        [password.buffer]);
    } catch {
      stop();
      wipe(password);
      reject(new BadProof("proof: Argon2id failed"));
    }
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

// validateUserKeySync checks the structure of a key of the trust list, like
// validateUserKey below, but without the two checks only the Web Crypto API
// can answer as a promise: id === keyID(pub) (a SHA-256) and, for a passkey,
// that pub actually imports as alg's kind of key. Everything that can be
// decided without a promise is decided right here, including that id is a
// string in the first place - the sync/async split is about which checks
// need crypto, not about id specifically, and skipping the type check too
// would over-accept a key whose id is not a string at all, not just one
// whose id is a string that happens to be wrong: with no type check here,
// validateUsersSync's own keys.has(k.id)/keys.add(k.id) duplicate-key-id
// check goes quiet for such a key (Round 1 finding Wichtig 3 - two objects
// such as k.id = {} are never === to each other, so a Set never calls them
// the same id, and the earlier version of this function let that reach the
// Set at all). trust.js calls this one from validateList, which is
// synchronous and runs once over a whole list; the two deferred checks run
// once more per key, afterwards, in trust.parseList (Go has no such gap:
// crypto/sha256 and x509.ParsePKIXPublicKey are synchronous, so
// proof.UserKey.Validate checks everything in one call). It answers true or
// false and never throws.
export function validateUserKeySync(k) {
  try {
    if (k === null || typeof k !== "object") return false;
    const { id, kind, alg, pub, kdf } = k;
    const cred = asBytes(k.credId);
    if (typeof id !== "string" || !(pub instanceof Uint8Array) || cred === null) return false;
    if (kind === KIND_PASSWORD) {
      // Go: KDF is a pointer, and a password key without one is no key.
      return alg === ALG_EDDSA && pub.length === ED25519_SIZE && cred.length === 0 && validateKDF(kdf);
    }
    if (kind === KIND_PASSKEY) {
      return cred.length > 0 && cred.length <= MAX_CRED_ID && pub.length <= MAX_PUBLIC
        && (kdf === undefined || kdf === null);
    }
    return false;
  } catch {
    return false;
  }
}

// validateUserKey checks the structure of a key of the trust list
// (Go: proof.UserKey.Validate): validateUserKeySync (which already confirms
// id is a string), plus id === keyID(pub) and, for a passkey, that pub
// imports as alg's kind of key - the two checks only the Web Crypto API can
// answer, which is why this one is a promise. It answers true or false and
// never throws.
export async function validateUserKey(k) {
  try {
    if (!validateUserKeySync(k)) return false;
    const { id, kind, alg, pub } = k;
    if (id !== (await keyIDAsync(pub))) return false;
    if (kind === KIND_PASSKEY) return (await importPublic(alg, pub)) !== null;
    return true;
  } catch {
    return false;
  }
}

// The PKCS#8 head of an Ed25519 private key (RFC 8410), the 32 bytes of the
// seed after it. It is the one form the Web Crypto API imports a seed in.
const ED25519_PKCS8 = new Uint8Array([0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b,
  0x65, 0x70, 0x04, 0x22, 0x04, 0x20]);

// seedBytes builds that document. concat makes a new array and copies the
// seed into it, so every call here puts a second copy of the secret on the
// heap - one that importSeed wipes again, because unlike a string it can be
// written to.
function seedBytes(seed) {
  // One view: the size that is checked and the bytes that go into the PKCS#8
  // document are the same ones. Handing the caller's object to the check and
  // then to concat read it twice, and measured under node a seed of 33 real
  // bytes whose length answered 32 was taken and imported as a key - chromium
  // refused it, so the answer depended on the engine.
  let v;
  try {
    v = viewOf(seed, "proof: a seed is 32 bytes");
  } catch {
    throw new BadProof("proof: a seed is 32 bytes");
  }
  if (v.length !== ED25519_SIZE) throw new BadProof("proof: a seed is 32 bytes");
  return concat(ED25519_PKCS8, v);
}

// importSeed is the one door to the Web Crypto API for a seed, for both
// functions below. The document is wiped as soon as the import is through,
// whichever way it went: the API copies what it is given before it returns,
// so the key that comes out owes the wiped bytes nothing (measured - the key
// still signs, and the test says so). The seed the caller handed in is his
// and is not touched; what this module made of it is this module's to clear.
async function importSeed(seed, extractable) {
  const der = seedBytes(seed);
  try {
    return await crypto.subtle.importKey("pkcs8", der, { name: "Ed25519" }, extractable, ["sign"]);
  } finally {
    wipe(der);
  }
}

// privateFromSeed makes the password key of a seed. Not extractable: once
// imported, the key cannot leave the browser again.
export async function privateFromSeed(seed) {
  return importSeed(seed, false);
}

// publicFromSeed gives the public key that belongs to a seed
// (Go: ed25519.NewKeyFromSeed(seed).Public()).
//
// The Web Crypto API hands out no public key of a private one, and it exports
// a private key neither as "spki" nor as "raw", so the one way to the public
// half is to import the seed as extractable and read the JWK. That costs
// something, and it is counted out here rather than talked around. While this
// call runs, the seed stands in four places:
//
//   1. the caller's array, which is his and is not touched here;
//   2. the PKCS#8 document importSeed builds - bytes, and wiped;
//   3. d of the exported JWK, the seed once more as base64url text - measured
//      in the engine, not assumed, the JWK being {kty, crv, alg, ext,
//      key_ops, x, d};
//   4. the extractable key inside the engine.
//
// Two of them can be cleared from here and are. The other two cannot: a
// string cannot be written to, so d is only let go of at once - nothing that
// outlives this call holds it, and the text itself lives until the garbage
// collector takes it - and the engine's key goes when the engine lets it go.
// Both are named rather than promised away.
//
// This is the one place in the module where a copy of the secret is knowingly
// left standing. The key privateFromSeed hands out - the one that signs - is
// never extractable and makes no JWK: there the only copy left is the
// engine's own, which is the key itself.
export async function publicFromSeed(seed) {
  const extractable = await importSeed(seed, true);
  const jwk = await crypto.subtle.exportKey("jwk", extractable);
  const x = jwk.x;
  jwk.d = undefined;
  if (typeof x !== "string") throw new BadProof("proof: the export carries no public key");
  return b64decode(x);
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
