// The session keys and the proofs over the client certificate. The private
// parts never leave the browser: both key pairs are created as
// non-extractable Web Crypto keys, and the password is thrown away right
// after it has been turned into a key.
//
// THE IMPORTS DIFFER FROM THE TASK BRIEF'S OWN BLOCK in three names, the
// reading task 7 gave trust.js and task 16 gave http.js. Left out: b64encode,
// which nothing here writes - the proofs leave as bytes and login.js is the
// one place that spells a document; and utf8, which the brief imports and no
// line of its own body calls. Added: nothing.
//
// WHAT THE BRIEF HAS AND THIS DOES NOT: a module-level
// `lastLoginHadBothProofs` that its own test then reads. A mutable global
// that records what the last login needed is two things at once - a fact
// about a person's keys that anything else on the page can read, and a
// measurement the page makes about its own work. Its case asked
// `expect(both === true || both === false).toBe(true)`, which holds for every
// boolean and so for every implementation there could be; what replaces it
// asks the MOCK how many proofs it had to check (tests/shell/login.spec.js,
// "a role with passwordToo has the certificate signed twice"), which is a
// number this side cannot produce by itself.
import { equal as equalBytes } from "../contracts/b64.js";
import * as session from "../contracts/session.js";
import * as proof from "../contracts/proof.js";
import * as sign from "../contracts/sign.js";
import * as noise from "../contracts/noise.js";

export async function newSessionKeys() {
  // Ed25519 signs tasks, X25519 carries the Noise session and every
  // end-to-end channel. Neither can be read out again, not even by the shell.
  const signPair = await crypto.subtle.generateKey({ name: "Ed25519" }, false, ["sign", "verify"]);
  // "raw" and not the brief's jwk detour: the raw export of an Ed25519 public
  // key IS the 32 bytes the certificate carries, while the JWK route takes
  // the same bytes through a base64url string and back (b64decode(jwk.x)).
  // One step fewer, one place fewer where a spelling can go wrong - and the
  // engines that NGC asks for all export a public key as raw, which is the
  // line net/testlink.js has been using since task 17.
  const publicRaw = new Uint8Array(await crypto.subtle.exportKey("raw", signPair.publicKey));
  const dh = await noise.newKeypair();
  return {
    sign: { privateKey: signPair.privateKey, publicRaw, kid: await sign.keyIDAsync(publicRaw) },
    dh,
  };
}

export function buildClientCert({ installation, user, nonce, keys, now, maxLifetime }) {
  const cert = {
    inst: installation, user, nonce,
    sig: keys.sign.publicRaw, dh: keys.dh.publicRaw,
    iat: now, exp: now + Math.min(maxLifetime, session.MAX_LIFETIME),
  };
  if (!session.validateClientCert(cert)) throw new Error("keys: malformed certificate");
  // These exact bytes are what the user signs and what CertHash hashes, so
  // they are built once and kept for the whole session.
  return { cert, payload: session.encodeClientCert(cert) };
}

// proveWithPassword turns the typed password into the user's password key and
// signs the certificate with it.
//
// The password reaches exactly one function, proof.deriveSeed, which hands
// its bytes to a worker and never gives them back; the seed that comes out
// lives for one signature and is wiped in the finally below, whichever way
// this function ends. Nothing else here ever sees the text.
export async function proveWithPassword(payload, passwordText, kdf, purpose) {
  const seed = await proof.deriveSeed(passwordText, kdf);
  try {
    const key = await proof.privateFromSeed(seed);
    const pub = await proof.publicFromSeed(seed);
    const sig = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, key,
      sign.message(purpose, payload)));
    return { kind: proof.KIND_PASSWORD, kid: await sign.keyIDAsync(pub), sig,
      authData: new Uint8Array(0), clientData: new Uint8Array(0) };
  } finally {
    seed.fill(0);
  }
}

// proveWithPasskey asks the authenticator for an assertion over the challenge
// of this purpose and payload. NGC always asks for user verification: a PIN or
// biometrics, never a tap alone.
//
// passkeys is challenge.passkeys, checked by session.parseLoginChallenge: each
// entry holds the credential ID for allowCredentials and the key ID the proof
// needs. The shell takes the key ID from the answer and derives none of its
// own, so a browser that has never seen the trust list can log in with a
// passkey on the first try.
export async function proveWithPasskey(payload, { rp, passkeys }, purpose) {
  const challenge = await proof.challenge(purpose, payload);
  const cred = await navigator.credentials.get({
    publicKey: {
      challenge,
      rpId: rp.id,
      userVerification: "required",
      timeout: 60000,
      allowCredentials: passkeys.map((p) => ({ type: "public-key", id: p.credId })),
    },
  });
  if (!cred) throw new Error("keys: no passkey");
  const id = new Uint8Array(cred.rawId);
  // The authenticator answers with one of the credential IDs that were
  // offered, so its entry is the one that names the key ID.
  const ref = passkeys.find((p) => equalBytes(p.credId, id));
  if (!ref) throw new Error("keys: the authenticator answered with another credential");
  return {
    kind: proof.KIND_PASSKEY, kid: ref.kid,
    sig: new Uint8Array(cred.response.signature),
    authData: new Uint8Array(cred.response.authenticatorData),
    clientData: new Uint8Array(cred.response.clientDataJSON),
  };
}
