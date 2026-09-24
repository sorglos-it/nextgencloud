// NGC always uses Noise_KK_25519_AESGCM_SHA256, for three uses told apart by
// the prologue: session (browser to API), tunnel (controller to API) and end
// to end (shell to controller). Messages 1 and 2 carry an empty payload, so
// each is exactly HANDSHAKE_SIZE bytes (Go: noisep).
//
// The cryptography itself comes from the Web Crypto API; only the order of
// the steps is written out here, straight from the Noise specification.
import { b64decode, concat, utf8 } from "./b64.js";

export const PROLOGUE_SESSION = "ngc/session/v1";
export const PROLOGUE_TUNNEL = "ngc/tunnel/v1";
export const PROLOGUE_E2E = "ngc/e2e/v1";
export const HANDSHAKE_SIZE = 48;
export const TAG_SIZE = 16;
export const MAX_MESSAGE = 65535;
export const MAX_PLAINTEXT = MAX_MESSAGE - TAG_SIZE;

const PROTOCOL = "Noise_KK_25519_AESGCM_SHA256";
const X25519_PKCS8 = new Uint8Array([0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b,
  0x65, 0x6e, 0x04, 0x22, 0x04, 0x20]);
const X25519_SPKI = new Uint8Array([0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x6e, 0x03, 0x21, 0x00]);

export async function e2ePrologue(controllerID, app, service, action, params, task) {
  const p = new Uint8Array(await crypto.subtle.digest("SHA-256", params));
  const t = new Uint8Array(await crypto.subtle.digest("SHA-256", task));
  let b = utf8(PROLOGUE_E2E);
  for (const s of [controllerID, app, service, action]) {
    b = concat(b, new Uint8Array([0]), utf8(s));
  }
  return concat(b, new Uint8Array([0]), p, t);
}

// newKeypair makes a session key pair whose private part cannot be read
// again, not even by the shell.
export async function newKeypair() {
  const pair = await crypto.subtle.generateKey({ name: "X25519" }, false, ["deriveBits"]);
  const spki = new Uint8Array(await crypto.subtle.exportKey("raw", pair.publicKey));
  return { privateKey: pair.privateKey, publicRaw: spki };
}

async function importPrivate(raw) {
  return crypto.subtle.importKey("pkcs8", concat(X25519_PKCS8, raw), { name: "X25519" }, false, ["deriveBits"]);
}

async function importPublic(raw) {
  try {
    return await crypto.subtle.importKey("raw", raw, { name: "X25519" }, false, []);
  } catch (err) {
    // Engines that do not take "raw" for X25519 read the SubjectPublicKeyInfo;
    // any other failure is real and must reach the caller, not be replaced.
    if (err.name !== "NotSupportedError" && err.name !== "DataError") throw err;
    return crypto.subtle.importKey("spki", concat(X25519_SPKI, raw), { name: "X25519" }, false, []);
  }
}

async function dh(priv, pubRaw) {
  const pub = await importPublic(pubRaw);
  return new Uint8Array(await crypto.subtle.deriveBits({ name: "X25519", public: pub }, priv, 256));
}

async function sha256(...parts) {
  return new Uint8Array(await crypto.subtle.digest("SHA-256", concat(...parts)));
}

async function hmac(key, data) {
  const k = await crypto.subtle.importKey("raw", key, { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  return new Uint8Array(await crypto.subtle.sign("HMAC", k, data));
}

// hkdf is the two- or three-output form the Noise specification defines.
async function hkdf(chainingKey, ikm, outputs) {
  const temp = await hmac(chainingKey, ikm);
  const o1 = await hmac(temp, new Uint8Array([1]));
  const o2 = await hmac(temp, concat(o1, new Uint8Array([2])));
  if (outputs === 2) return [o1, o2];
  return [o1, o2, await hmac(temp, concat(o2, new Uint8Array([3])))];
}

function nonce(n) {
  // 32 bits of zeros, then the counter as 64 bits big endian.
  const b = new Uint8Array(12);
  new DataView(b.buffer).setBigUint64(4, BigInt(n));
  return b;
}

// MAX_NONCE guards Number precision, not the Noise limit: past 2**53 a
// double can no longer represent every integer, so n++ can silently stop
// moving and two messages would end up sharing one key/IV pair. Go does not
// need this (n is a uint64 there); here n is a plain Number.
const MAX_NONCE = Number.MAX_SAFE_INTEGER - 1;

class CipherState {
  constructor(key) {
    this.keyRaw = key;
    this.key = null;
    this.n = 0;
  }

  async aes() {
    if (!this.key) {
      this.key = await crypto.subtle.importKey("raw", this.keyRaw, { name: "AES-GCM" }, false, ["encrypt", "decrypt"]);
    }
    return this.key;
  }

  async seal(plain, ad = new Uint8Array(0)) {
    if (!(plain instanceof Uint8Array)) throw new Error("noise: not bytes");
    if (plain.length > MAX_PLAINTEXT) throw new Error("noise: message too large");
    if (this.n > MAX_NONCE) throw new Error("noise: nonce counter exhausted");
    const out = new Uint8Array(await crypto.subtle.encrypt(
      { name: "AES-GCM", iv: nonce(this.n), additionalData: ad, tagLength: 128 }, await this.aes(), plain));
    this.n++;
    return out;
  }

  async open(cipher, ad = new Uint8Array(0)) {
    if (!(cipher instanceof Uint8Array)) throw new Error("noise: not bytes");
    if (cipher.length > MAX_MESSAGE) throw new Error("noise: message too large");
    if (this.n > MAX_NONCE) throw new Error("noise: nonce counter exhausted");
    const out = new Uint8Array(await crypto.subtle.decrypt(
      { name: "AES-GCM", iv: nonce(this.n), additionalData: ad, tagLength: 128 }, await this.aes(), cipher));
    this.n++;
    return out;
  }
}

export class Transport {
  constructor(send, recv) {
    this.sendState = send;
    this.recvState = recv;
  }

  seal(plain) {
    return this.sendState.seal(plain);
  }

  open(cipher) {
    return this.recvState.open(cipher);
  }
}

export class HandshakeKK {
  // Use of() outside tests. forTest() also takes the static key as raw bytes
  // instead of a CryptoKey and, because publicOf() has to read the public
  // part back out, imports it as extractable - the opposite of what
  // newKeypair() promises. Fine to reproduce noise.json, never for a real key.
  static async of(initiator, prologue, staticPriv, staticPub, peerPub) {
    return HandshakeKK.build(initiator, prologue, staticPriv, staticPub, peerPub, null);
  }

  static async forTest(initiator, prologue, staticPrivRaw, peerPub, ephemeralPrivRaw) {
    const staticPriv = await importPrivate(staticPrivRaw);
    const staticPub = await publicOf(staticPrivRaw);
    return HandshakeKK.build(initiator, prologue, staticPriv, staticPub, peerPub, ephemeralPrivRaw);
  }

  static async build(initiator, prologue, staticPriv, staticPub, peerPub, ephemeralPrivRaw) {
    const h = new HandshakeKK();
    h.initiator = initiator;
    h.s = { priv: staticPriv, pub: staticPub };
    h.rs = peerPub;
    h.fixedEphemeral = ephemeralPrivRaw;
    h.splitCalled = false;
    const name = utf8(PROTOCOL);
    // The protocol name is 28 bytes, so it is padded with zeros to 32.
    h.hash = concat(name, new Uint8Array(32 - name.length));
    h.ck = h.hash;
    h.cs = null;
    await h.mixHash(prologue);
    // KK pre-messages: first the initiator's static key, then the responder's.
    await h.mixHash(initiator ? staticPub : peerPub);
    await h.mixHash(initiator ? peerPub : staticPub);
    return h;
  }

  async mixHash(data) {
    this.hash = await sha256(this.hash, data);
  }

  async mixKey(input) {
    const [ck, k] = await hkdf(this.ck, input, 2);
    this.ck = ck;
    this.cs = new CipherState(k);
  }

  async encryptAndHash(plain) {
    const out = await this.cs.seal(plain, this.hash);
    await this.mixHash(out);
    return out;
  }

  async decryptAndHash(cipher) {
    const out = await this.cs.open(cipher, this.hash);
    await this.mixHash(cipher);
    return out;
  }

  async ephemeral() {
    if (this.fixedEphemeral) {
      return { priv: await importPrivate(this.fixedEphemeral), pub: await publicOf(this.fixedEphemeral) };
    }
    const pair = await newKeypair();
    return { priv: pair.privateKey, pub: pair.publicRaw };
  }

  // -> e, es, ss
  async writeMessage1() {
    this.e = await this.ephemeral();
    await this.mixHash(this.e.pub);
    await this.mixKey(await dh(this.e.priv, this.rs));
    await this.mixKey(await dh(this.s.priv, this.rs));
    return concat(this.e.pub, await this.encryptAndHash(new Uint8Array(0)));
  }

  async readMessage1(msg) {
    if (msg.length !== HANDSHAKE_SIZE) throw new Error("noise: message 1");
    this.re = msg.subarray(0, 32);
    await this.mixHash(this.re);
    await this.mixKey(await dh(this.s.priv, this.re));
    await this.mixKey(await dh(this.s.priv, this.rs));
    const payload = await this.decryptAndHash(msg.subarray(32));
    // Messages 1 and 2 never carry a payload in NGC.
    if (payload.length !== 0) throw new Error("noise: message 1 payload");
  }

  // <- e, ee, se
  async writeMessage2() {
    this.e = await this.ephemeral();
    await this.mixHash(this.e.pub);
    await this.mixKey(await dh(this.e.priv, this.re));
    await this.mixKey(await dh(this.e.priv, this.rs));
    return concat(this.e.pub, await this.encryptAndHash(new Uint8Array(0)));
  }

  async readMessage2(msg) {
    if (msg.length !== HANDSHAKE_SIZE) throw new Error("noise: message 2");
    this.re = msg.subarray(0, 32);
    await this.mixHash(this.re);
    await this.mixKey(await dh(this.e.priv, this.re));
    await this.mixKey(await dh(this.s.priv, this.re));
    const payload = await this.decryptAndHash(msg.subarray(32));
    if (payload.length !== 0) throw new Error("noise: message 2 payload");
  }

  // split ends the handshake: c1 carries initiator to responder, c2 the other
  // way, and each cipher state starts its counter at 0. ck never changes
  // here, so a second call would derive the same k1/k2 again and hand out a
  // second CipherState that reuses every nonce the first one will ever use -
  // the same key and IV encrypting two different messages. split() runs once.
  async split() {
    if (this.splitCalled) throw new Error("noise: split already called");
    this.splitCalled = true;
    const [k1, k2] = await hkdf(this.ck, new Uint8Array(0), 2);
    return this.initiator ? new Transport(new CipherState(k1), new CipherState(k2))
      : new Transport(new CipherState(k2), new CipherState(k1));
  }
}

async function publicOf(privRaw) {
  const key = await crypto.subtle.importKey("pkcs8", concat(X25519_PKCS8, privRaw),
    { name: "X25519" }, true, ["deriveBits"]);
  const jwk = await crypto.subtle.exportKey("jwk", key);
  return b64decode(jwk.x);
}
