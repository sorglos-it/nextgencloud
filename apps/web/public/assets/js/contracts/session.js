// Login, the session certificates and the HTTP exchange before the WebSocket
// (Go: session).
//
// The imports differ from the task brief's own block in nine names: five it
// does not have, four it has that nothing here uses. Added: validOrigin, for
// parseLoginChallenge - url.js stands in the brief's "Consumes" line but in
// no import; PURPOSE_CLIENT_CERT, what verifyApproval checks a user's proof
// with, where the brief names only PURPOSE_API_CERT; validateEnvelope, what
// parseLoginResult holds the API's envelope against; keyID and verify, which
// verifyAPI needs to find the node that signed and to check its signature.
// Left out: b64decode, envelopeBudget, validText and validTOTP, none of which
// this file uses - the same reading task 7 gave trust.js's own import line.
import { b64encode, concat, utf8, equal } from "./b64.js";
import { decode, Budget, S, P, A, str, bool, bytes, uint, int } from "./json.js";
import { validID } from "./text.js";
import { validOrigin } from "./url.js";
import * as trust from "./trust.js";
import * as proof from "./proof.js";
import {
  PURPOSE_API_CERT, PURPOSE_CLIENT_CERT, MAX_PAYLOAD,
  envelopeShape, validateEnvelope, keyID, verify as verifyEnvelope,
} from "./sign.js";

export const PATH_SETUP = "/api/v1/setup";
export const PATH_LOGIN_START = "/api/v1/login/start";
export const PATH_LOGIN_FINISH = "/api/v1/login/finish";
export const PATH_LOGOUT = "/api/v1/logout";
export const PATH_TRUST = "/api/v1/trust";
export const PATH_SOCKET = "/api/v1/ws";
export const SUBPROTOCOL = "ngc.v1";
export const COOKIE_NAME = "ngc_session";
export const TOKEN_SIZE = 32, NONCE_SIZE = 32;
export const MAX_LIFETIME = 86400, MAX_SKEW = 300, CHALLENGE_LIFETIME = 120;
export const MAX_CERT = 4096, MAX_REQUEST = 64 << 10;
export const MAX_SETUP_OFFER = 128 << 10, MAX_SETUP_NODES = 16;
export const MAX_CHAIN_VERSIONS = 64, MAX_CHAIN = 32 << 20;
export const TOTP_DIGITS = 6, TOTP_PERIOD = 30;
// A user holds at most this many keys in the trust list, so no honest answer
// names more passkeys, and the shell allocates no more either.
export const MAX_PASSKEYS = trust.MAX_KEYS_PER_USER;

// The bound of a TrustHint body, which is all hintBudget counts; {"seq":N}
// needs at most 24 of those bytes (Go: maxTrustHint).
const MAX_TRUST_HINT = 64;
// Two names for one number, so that every check below says which of the two
// it means: HASH_SIZE is Go's sha256.Size, the length of a setup MAC, and
// KEY_SIZE the 32 bytes of an Ed25519 public key (Go: ed25519.PublicKeySize)
// and of an X25519 key alike.
const HASH_SIZE = 32, KEY_SIZE = 32;

const certBudget = new Budget({ size: MAX_CERT });
const requestBudget = new Budget({ size: MAX_REQUEST });
const offerBudget = new Budget({ size: MAX_SETUP_OFFER });
const challengeBudget = new Budget({ size: MAX_REQUEST,
  items: { passkeys: MAX_PASSKEYS, "rp/origins": trust.MAX_ORIGINS } });
const infoBudget = new Budget({ size: MAX_REQUEST, items: { nodes: MAX_SETUP_NODES } });
const hintBudget = new Budget({ size: MAX_TRUST_HINT });
const chainBudget = new Budget({ size: MAX_CHAIN, items: { versions: MAX_CHAIN_VERSIONS } });

// Go folds every decoder error of this package into ErrMessage, because its
// decoder quotes parts of the input and an input here may hold a setup code
// or a proof. json.js gives that away nowhere: its fail() names the rule that
// broke and never what stood there. So a decoder error travels out of the
// parsers below as what it is, the way trust.parseList leaves it too, and
// what they decide themselves is said in Go's own words - "malformed
// message", "malformed certificate", "proof is not by a key of this user",
// "certificate not valid at this time", "setup does not match the code or
// the offer". Two errors here have no Go counterpart, because Go hands on an
// error where a Web Crypto call answers only true or false, or writes what
// JSON.stringify may refuse: each says so where it is thrown.

// The node shape of a setup offer is trust's own: SetupInfo.Nodes is a
// []trust.Node in Go, and the shell puts the offered nodes into version 1 of
// the trust list unchanged, where trust.encodeList writes them. trust.js
// keeps that shape private and exports the list shape that holds it, so it is
// read from there rather than written out a second time: two places with the
// same shape are two places that can drift apart, and a drift would show up
// as a version 1 the API refuses.
const nodeShape = trust.listShape.m.nodes.of;
// Go's SetupInfo.Store is a *trust.Store, so an offer without one decodes to
// null; a trust list's own Stores are values. P() over the members of trust's
// store shape is that pointer, of the same members as the list's.
const storeShape = P(trust.listShape.m.stores.of.m);

export const passkeyRefShape = S({ credId: bytes, kid: str });
export const loginChallengeShape = S({
  nonce: bytes, now: int, maxLifetime: int, installation: str, user: str,
  kdf: proof.kdfShape, passkeys: A(passkeyRefShape), totp: bool, passwordToo: bool,
  rp: proof.rpShape,
});
export const loginResultShape = S({ api: envelopeShape, passwordHint: bool });
export const clientCertShape = S({ inst: str, user: str, nonce: bytes, sig: bytes, dh: bytes,
  iat: int, exp: int });
export const apiCertShape = S({ inst: str, node: str, client: bytes, dh: bytes, iat: int, exp: int });
const setupOfferShape = S({ info: bytes, mac: bytes });
const setupInfoShape = S({ installation: str, nodes: A(nodeShape), now: int,
  releaseKey: bytes, store: storeShape });
const trustChainShape = S({ versions: A(trust.signedShape), more: bool });
const trustHintShape = S({ seq: uint });

// encodeClientCert writes the certificate in the field order of Go's
// session.ClientCert. These exact bytes are what the user signs (purpose
// clientcert/v1) and what certHash hashes, so they are built once and kept.
//
// Go's json.Marshal writes all seven fields every time - the struct carries
// no omitempty - while JSON.stringify leaves out a member whose value is
// undefined. A certificate with a field nobody filled in would therefore go
// unnoticed into the very bytes the user signs with a passkey, and only the
// API would say no: a gesture spent for nothing. So the fields are read once
// into locals, those are held against the check the API applies too
// (validateClientCert, Go: ClientCert.Validate), and only then written
// (Fixrunde 1, W2).
//
// There is one way out of here for every failure: a field that is no byte
// string or no ID, a getter or a proxy that throws, and a value
// JSON.stringify refuses - a BigInt passes every check a number passes and is
// no JSON number - all end at the one error below, as trust.encodeList
// answers one for a list it cannot write.
export function encodeClientCert(c) {
  try {
    const { inst, user, nonce, sig, dh, iat, exp } = c ?? {};
    if (validateClientCert({ inst, user, nonce, sig, dh, iat, exp })) {
      return utf8(JSON.stringify({
        inst, user, nonce: b64encode(nonce),
        sig: b64encode(sig), dh: b64encode(dh), iat, exp,
      }));
    }
  } catch {
    // Falls through to the one error, like a check that answered false.
  }
  throw new Error("session: cannot encode this certificate");
}

// validateClientCert checks the fields of a client certificate (Go:
// ClientCert.Validate). It answers true or false and never throws: the guard
// in front refuses what is no object at all, and the try behind it catches a
// field of a real object that throws on being read - a getter or a proxy
// trap, which no guard in front can see (sign.js's validateEnvelope writes
// the same pair, and trust.validAPINode and trust.validStore follow it). The
// seven fields are read once into locals and only those are compared, so no
// two checks here can see two different values of one field.
export function validateClientCert(c) {
  if (c === null || typeof c !== "object") return false;
  try {
    const { inst, user, nonce, sig, dh, iat, exp } = c;
    return validID(inst) && validID(user)
      && nonce instanceof Uint8Array && nonce.length === NONCE_SIZE
      && sig instanceof Uint8Array && sig.length === KEY_SIZE
      && dh instanceof Uint8Array && dh.length === KEY_SIZE
      && iat > 0 && exp > iat && exp - iat <= MAX_LIFETIME;
  } catch {
    return false;
  }
}

// certHash is the SHA-256 of a client certificate payload (Go: CertHash).
export async function certHash(payload) {
  return new Uint8Array(await crypto.subtle.digest("SHA-256", payload));
}

// apiNodes picks the API nodes of a list that carry a signing key, in the
// order the list has them, so the search below finds the same first match Go
// finds. A list whose nodes are no array, and an entry that is null or no
// object, leave it empty or drop that entry: the answer is then the same
// "no node signed this" the caller gets for a list without a match, and
// Array.isArray says nothing about what lies inside an array. There is no try
// here: a kind, an id or a sign that throws on being read carries its
// exception out of verifyAPI, which refuses this envelope either way.
//
// Fixrunde 1, after K1: the list is the caller's, so nodes and the three
// fields of each node are read once and what goes back is this module's own
// object. verifyAPI reads the id twice - for "is this a node at all" and for
// "is this the node the certificate names" - and the key twice, to find the
// node and to check the signature; out of one read each, those pairs cannot
// disagree. Go walks a []trust.Node of typed fields and has nothing to
// disagree with.
const apiNodes = (l) => {
  const ns = l?.nodes;
  if (!Array.isArray(ns)) return [];
  const out = [];
  for (const n of ns) {
    if (n === null || typeof n !== "object") continue;
    const { kind, id, sign } = n;
    if (kind === trust.NODE_API && sign instanceof Uint8Array) out.push({ id, sign });
  }
  return out;
};

// verifyAPI checks an API certificate: signed by an API node of the trust
// list, bound to the client certificate client and never valid longer than it,
// so a session cannot outlive the user's approval (Go: session.VerifyAPI).
// Every deviation throws, a now that is no whole number included.
export async function verifyAPI(env, l, client, now) {
  // The envelope belongs to the caller, so each of its fields may be a getter
  // or a proxy trap that answers two reads with two values. The three are read
  // once here and only these copies are used below: the signature is checked
  // over the same bytes the certificate is then read from, never over a first
  // read while a second one is decoded - that second read would hand back a
  // certificate no API node ever signed. Go is given a sign.Envelope of three
  // typed fields, which answer every read alike; sign.js and proof.js read
  // their own fields once for this reason, and validateClientCert above says
  // it too (Fixrunde 1, K1).
  if (env === null || typeof env !== "object") throw new Error("session: malformed certificate");
  const { payload, kid, sig } = env;
  const cc = decode(client, certBudget, clientCertShape);
  // A key id is the first 16 bytes of the SHA-256 of the key, so finding the
  // node that signed this envelope costs one hash per API node of the list.
  // They are computed as one Promise.all instead of one await at a time, the
  // way trust.parseList batches a user's keys. Measured over a list of 256 API
  // nodes whose signer is the last one, in the WebKit Playwright brings:
  // 3894 ms one await at a time, 30 ms as one Promise.all - a round trip to
  // the Web Crypto API costs about 15 ms there and the hash itself almost
  // nothing (Chromium: 10 ms and 3 ms).
  //
  // The group is as large as the list has API nodes: trust.MAX_NODES = 256
  // for a list that came through trust.parseList, whose budget counts them,
  // and whatever a hand-built one holds otherwise. trust.parseList bounds its
  // own groups at MAX_KEYS_PER_USER = 16 because it makes one per user and
  // would otherwise start MAX_USERS x 16 of them at once; here there is one
  // group in all, and its size is the number the measurement above is of
  // (Fixrunde 1: the earlier wording said "up to trust.MAX_NODES of them",
  // which holds for a parsed list and for no other).
  const nodes = apiNodes(l);
  const ids = await Promise.all(nodes.map((n) => keyID(n.sign)));
  const node = nodes[ids.indexOf(kid)] ?? null;
  // Go marks "none found" with the zero trust.Node and asks `if node.ID == ""`
  // afterwards, so a node whose ID is empty is no node there either - refused
  // even when its key made the signature. A list that came through
  // trust.validateList holds none (validName refuses ""), but this takes the
  // list its caller hands it, and the two answer that list alike.
  if (node === null || node.id === "") throw new Error("session: malformed certificate");
  // Go hands sign.Verify's own error on; here a signature that cannot be
  // checked answers false and this one error says so.
  if (!(await verifyEnvelope({ payload, kid, sig }, node.sign, PURPOSE_API_CERT))) {
    throw new Error("session: bad signature");
  }
  const c = decode(payload, certBudget, apiCertShape);
  if (c.inst !== l.installation || c.node !== node.id
    || !equal(c.client, await certHash(client))
    || c.dh.length !== KEY_SIZE || c.iat <= 0 || c.exp <= c.iat || c.exp - c.iat > MAX_LIFETIME
    || c.exp > cc.exp) {
    throw new Error("session: malformed certificate");
  }
  // Go takes an int64 for now and can be handed nothing else. Here both
  // comparisons below are false against undefined, NaN, an object or text -
  // and a caller who forgets the fourth argument hands over undefined - so a
  // certificate that expired long ago would come through. A now that is no
  // whole number is no point in time, and a certificate whose validity cannot
  // be established at it is refused with the same error the check itself
  // gives. json.js says the same of a missing budget size: everything a
  // caller leaves out closes the gate (Fixrunde 1, K2).
  if (!Number.isSafeInteger(now) || now < c.iat - MAX_SKEW || now >= c.exp) {
    throw new Error("session: certificate not valid at this time");
  }
  return c;
}

// verifyApproval checks a client certificate and the user's proof over it
// against the trust list, but not the time: it is evidence of what a user
// approved once, which is what core.verifyUIProof needs for an uploaded app
// (Go: session.VerifyApproval). If a role of the user asks for the password
// too, the proof must be by a passkey - a password key alone never approves
// anything. The shell needs no verifyClient of its own: it builds its
// certificate itself and never checks another's.
export async function verifyApproval(payload, p, l) {
  const c = decode(payload, certBudget, clientCertShape);
  if (!validateClientCert(c) || c.inst !== l.installation) {
    throw new Error("session: malformed certificate");
  }
  // The proof, the key and the user all belong to the caller, so each field
  // is read once here and only these copies travel on - the same reason K1
  // gives for the envelope, and the reason Go has nothing of the kind: it is
  // handed a proof.Proof by value, and KeyOwner returns a copy of the user
  // and of the key. Both fields below decide the one rule this function has
  // beyond a signature check, so a second read is not a detail: a kind that
  // reads "passkey" here and "password" inside proof.verify skips the rule
  // and then matches the proof, and an id that reads as the certificate's
  // user here and as another user in trust.passwordToo asks the question
  // about the wrong user. proof.verify reads the key's id, kind, alg and pub
  // and nothing else, so those four are what the copy carries.
  const { kind: proofKind, kid, sig, authData, clientData } = p ?? {};
  const found = trust.keyOwner(l, kid);
  if (found === null) throw new Error("session: proof is not by a key of this user");
  const ownerID = found.user.id;
  const { id, kind, alg, pub } = found.key;
  const key = { id, kind, alg, pub };
  if (ownerID !== c.user || (kind !== proof.KIND_PASSKEY && trust.passwordToo(l, ownerID))) {
    throw new Error("session: proof is not by a key of this user");
  }
  // Go returns proof.Verify's own error here; proof.verify answers true or
  // false, and this is the error that says false.
  const copy = { kind: proofKind, kid, sig, authData, clientData };
  if (!(await proof.verify(copy, key, l.rp, PURPOSE_CLIENT_CERT, payload))) {
    throw new Error("session: bad signature");
  }
  return c;
}

// validatePasskeyRef checks one passkey the user may log in with: the
// credential ID for allowCredentials, of 1 to proof.MAX_CRED_ID bytes as a
// passkey's own key has it, and the key ID that goes into proof.Proof.kid,
// by the rule proofs are held to (Go: PasskeyRef.Validate). Without the key
// ID a browser that has never seen the trust list could not build the proof
// at all, so it travels with the answer and the shell never derives it
// itself. Guard, try and one read per field for the reasons
// validateClientCert gives above.
export function validatePasskeyRef(r) {
  if (r === null || typeof r !== "object") return false;
  try {
    const { credId, kid } = r;
    return credId instanceof Uint8Array && credId.length >= 1 && credId.length <= proof.MAX_CRED_ID
      && proof.validKeyID(kid);
  } catch {
    return false;
  }
}

// parseLoginChallenge reads an answer of PATH_LOGIN_START (Go:
// session.ParseLoginChallenge, the only yardstick): strict JSON within
// MAX_REQUEST with at most MAX_PASSKEYS passkeys and trust.MAX_ORIGINS
// origins, a nonce of NONCE_SIZE bytes, now above 0, maxLifetime 1 to
// MAX_LIFETIME, installation and user each an ID, a KDF that validates if one
// is there, an rp.id by proof.validRPID if one is there - the shell hands it
// to navigator.credentials.get, so it is a DNS name and never free text - and
// origins by url.validOrigin. The passkey list may be empty (a user without
// one), but an answer that names passkeys names the relying party too, each
// entry passes validatePasskeyRef, and no credential ID and no key ID stands
// twice: two entries of one key ID would leave the shell guessing which key
// made a proof. The made-up answer the API gives for a name it does not know
// keeps all of this, so the shell may be neither milder nor stricter at any
// point - its reaction would otherwise tell whether the name exists.
export function parseLoginChallenge(b) {
  const c = decode(b, challengeBudget, loginChallengeShape);
  if (c.nonce.length !== NONCE_SIZE || c.now <= 0
    || c.maxLifetime < 1 || c.maxLifetime > MAX_LIFETIME
    || !validID(c.installation) || !validID(c.user)
    || (c.kdf !== null && !proof.validateKDF(c.kdf))
    || c.passkeys.length > MAX_PASSKEYS || c.rp.origins.length > trust.MAX_ORIGINS
    || (c.rp.id !== "" && !proof.validRPID(c.rp.id))
    || (c.passkeys.length > 0 && (c.rp.id === "" || c.rp.origins.length === 0))) {
    throw new Error("session: malformed message");
  }
  for (const o of c.rp.origins) {
    if (!validOrigin(o)) throw new Error("session: malformed message");
  }
  const keys = new Set(), creds = new Set();
  for (const p of c.passkeys) {
    // base64url is one spelling per byte string, so two credential IDs are
    // the same set entry exactly when they are the same bytes - what Go
    // compares as string(p.CredID).
    const cred = b64encode(p.credId);
    if (!validatePasskeyRef(p) || keys.has(p.kid) || creds.has(cred)) {
      throw new Error("session: malformed message");
    }
    keys.add(p.kid);
    creds.add(cred);
  }
  return c;
}

// parseLoginResult reads the answer of PATH_LOGIN_FINISH: the API's signed
// certificate and whether the user logged in with a password. Go has no
// parser for it - the API writes this one and never reads it - so the bounds
// are the ones the rest of the contract sets: the size of a body before the
// WebSocket, and a payload verifyAPI can still decode, which is MAX_CERT.
export function parseLoginResult(b) {
  const r = decode(b, requestBudget, loginResultShape);
  if (!validateEnvelope(r.api, MAX_CERT)) throw new Error("session: malformed message");
  return r;
}

// parseTrustChain reads an answer of PATH_TRUST: 1 to MAX_CHAIN_VERSIONS
// versions within MAX_CHAIN bytes, each with a payload of 1 to
// sign.MAX_PAYLOAD bytes and a proof of the right form. trust.open and
// trust.advance check the versions themselves (Go: ParseTrustChain).
export function parseTrustChain(b) {
  const c = decode(b, chainBudget, trustChainShape);
  if (c.versions.length === 0 || c.versions.length > MAX_CHAIN_VERSIONS) {
    throw new Error("session: malformed message");
  }
  for (const v of c.versions) {
    if (v.payload.length === 0 || v.payload.length > MAX_PAYLOAD || !proof.validateProof(v.proof)) {
      throw new Error("session: malformed message");
    }
  }
  return c;
}

// parseTrustHint reads the body of the control message "trust" within
// MAX_TRUST_HINT bytes: version seq exists, and seq is at least 1 (Go:
// ParseTrustHint).
export function parseTrustHint(b) {
  const h = decode(b, hintBudget, trustHintShape);
  if (h.seq < 1) throw new Error("session: malformed message");
  return h;
}

// httpStatus is the status of an error answer before the WebSocket; its body
// is the CLOSE payload for the same code (Go: HTTPStatus). The codes stand
// here as the strings Go's wire.Code is - "E_PROTOCOL" and the rest - because
// the shell has no wire module yet; E_INTERNAL and every other code answer
// 500, as Go's default does.
export function httpStatus(code) {
  switch (code) {
    case "E_PROTOCOL": return 400;
    case "E_LOGIN": return 401;
    case "E_FORBIDDEN": case "E_VERIFY": return 403;
    case "E_NOTFOUND": return 404;
    case "E_TOOLARGE": return 413;
    case "E_LOCKED": return 429;
    case "E_UNREACHABLE": return 503;
  }
  return 500;
}

const CODE = /^[A-HJ-NP-Z2-9]{5}(-[A-HJ-NP-Z2-9]{5}){3}$/;
export const validSetupCode = (s) => typeof s === "string" && CODE.test(s);

// normalizeSetupCode turns what someone typed into the canonical code: spaces,
// tabs and '-' are dropped, a-z becomes upper case, then exactly 20 characters
// of the alphabet without I, O, 0 and 1, in groups of five. null where that is
// not possible, including for anything that is no string at all.
//
// It walks UTF-16 units and folds a-z alone, which is Go's byte loop
// character for character: every unit of a character outside ASCII is 0x80 or
// above and is refused here, as every one of its UTF-8 bytes is there. The
// task brief wrote this loop over code points with toUpperCase() instead, and
// that one differs in exactly one place - measured over every code point
// outside ASCII, surrogates apart: U+017F, the long s 'ſ', upper-cases to a
// single "S", so the brief's loop makes "ſBCDE-FGHJK-MNPQR-STUVW" the code
// "SBCDE-FGHJK-MNPQR-STUVW" while Go's NormalizeSetupCode, run on the same
// input, answers false. A shell that takes what the API's own normalizer
// would not is a second rule, so this one stays Go's.
export function normalizeSetupCode(s) {
  if (typeof s !== "string") return null;
  const c = [];
  for (let i = 0; i < s.length; i++) {
    let u = s[i];
    if (u === " " || u === "\t" || u === "-") continue;
    if (u >= "a" && u <= "z") u = String.fromCharCode(s.charCodeAt(i) - 32);
    if (c.length === 20 || !/^[A-HJ-NP-Z2-9]$/.test(u)) return null;
    c.push(u);
  }
  if (c.length !== 20) return null;
  const g = (i) => c.slice(i, i + 5).join("");
  return [g(0), g(5), g(10), g(15)].join("-");
}

async function setupHMAC(code, label, b) {
  const key = await crypto.subtle.importKey("raw", utf8(code), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  return new Uint8Array(await crypto.subtle.sign("HMAC", key, concat(utf8(label), new Uint8Array([0]), b)));
}

// The two labels keep the MACs apart, so one made for one direction never
// passes for the other, whatever bytes it covers.
export const offerMAC = (code, info) => setupHMAC(code, "ngc/setup-offer/v1", info);
export const setupMAC = (code, payload) => setupHMAC(code, "ngc/setup/v1", payload);

// parseSetupOffer reads the answer of GET PATH_SETUP with the canonical code
// (Go: session.ParseSetupOffer, step for step): strict JSON within
// MAX_SETUP_OFFER; info of 1 to MAX_REQUEST bytes and a 32-byte MAC; then the
// code and the MAC over exactly those info bytes - whoever does not hold the
// code cannot make that MAC, so nothing anyone wrote into info is read before
// it matches, and a code that is not canonical holds for no MAC; then info
// strictly within MAX_REQUEST with at most MAX_SETUP_NODES nodes: an
// installation ID, 1 to MAX_SETUP_NODES nodes each trust.validAPINode and
// sorted by ID without two of one ID, now above 0, a 32-byte release key, and
// a store that is either absent or the global one and passes
// trust.validStore.
export async function parseSetupOffer(b, code) {
  const o = decode(b, offerBudget, setupOfferShape);
  if (o.info.length === 0 || o.info.length > MAX_REQUEST || o.mac.length !== HASH_SIZE) {
    throw new Error("session: malformed message");
  }
  if (!validSetupCode(code) || !equal(o.mac, await offerMAC(code, o.info))) {
    throw new Error("session: setup does not match the code or the offer");
  }
  const info = decode(o.info, infoBudget, setupInfoShape);
  if (!validID(info.installation) || info.nodes.length === 0 || info.nodes.length > MAX_SETUP_NODES
    || info.now <= 0 || info.releaseKey.length !== KEY_SIZE
    || (info.store !== null && (info.store.id !== trust.STORE_GLOBAL || !trust.validStore(info.store)))) {
    throw new Error("session: malformed message");
  }
  // Version 1 lists the nodes as they were offered, and a trust list keeps
  // its nodes sorted by ID.
  for (let i = 0; i < info.nodes.length; i++) {
    if (!trust.validAPINode(info.nodes[i]) || (i > 0 && info.nodes[i - 1].id >= info.nodes[i].id)) {
      throw new Error("session: malformed message");
    }
  }
  return info;
}

// encodeSetupInfoNodes hands the offer's nodes on as version 1 of the trust
// list carries them: the same nodes, in the same order, because the API
// compares its own offer with what the admin signed as JSON (Go: VerifySetup,
// sameNodes). They need no conversion - the shape they were read with above
// is trust's own node shape - so this returns info.nodes itself, not a copy,
// and whoever builds version 1 from it leaves those nodes as they are. Go has
// no such function: there the API compares and the shell builds, and this is
// the one place in the shell that knows the rule.
//
// What it takes is what parseSetupOffer gives out, 1 to MAX_SETUP_NODES
// nodes, and nothing else: it is exported, and it answered null with a raw
// TypeError and an object without nodes with undefined - which whoever builds
// version 1 from it would have written as no nodes at all, and the API would
// have refused that version (Fixrunde 1). info.nodes is read once, and one
// error stands for every way this can fail, encodeClientCert's shape above.
export function encodeSetupInfoNodes(info) {
  try {
    const nodes = info?.nodes;
    if (Array.isArray(nodes) && nodes.length > 0 && nodes.length <= MAX_SETUP_NODES) return nodes;
  } catch {
    // Falls through to the one error, like a length that did not fit.
  }
  throw new Error("session: malformed message");
}
