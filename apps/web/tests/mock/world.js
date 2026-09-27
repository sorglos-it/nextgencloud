// The little world the mock API answers out of: a trust list, the user the
// tests log in as, the keys of one API node and one controller, and the
// results of the core actions.
//
// It is built with the SAME modules the shell uses - the contracts under
// public/assets/js - so an answer the mock writes is one the shell's own
// parser reads, and a rule that changes in a contract changes on both sides
// at once. Nothing here re-states a contract; where a document is built it is
// built by that contract's encoder and read back by its parser.
//
// The list starts from shared/testdata/trust.json, case "genesis", so the
// shape is the frozen one. Three things are swapped in, because a test needs
// keys it HOLDS: the signing key of api-1 (it signs the API certificate), the
// Noise key of ctl-1 (it plays the controller of an end-to-end channel), and
// a user "anna" with a password key whose password this file knows.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { b64decode, b64encode, utf8 } from "../../public/assets/js/contracts/b64.js";
import * as trust from "../../public/assets/js/contracts/trust.js";
import * as sign from "../../public/assets/js/contracts/sign.js";
import * as proof from "../../public/assets/js/contracts/proof.js";
import * as core from "../../public/assets/js/contracts/core.js";
import { argon2id } from "../../public/assets/vendor/noble-hashes/argon2.js";
import {
  USER_NAME, PASSWORD, TOTP_NAME, PASSKEY_NAME, BOTH_NAME, RP_ID, CRED_ID, PKCS8,
} from "../fixtures/logins.js";

// The fixtures of the repository, read as the files they are. This is the one
// JSON.parse in the mock and it stands on our own testdata, not on anything
// that came over a wire; every document that DID come over one goes through
// its contract's parser.
const testdata = (name) =>
  JSON.parse(readFileSync(fileURLToPath(new URL("../../../../shared/testdata/" + name, import.meta.url)), "utf8"));

// Who may log in stands in fixtures/logins.js, as plain values with no
// imports behind them: the cases need the same password, the same code and
// the same private keys, and importing this file from a case pulls the
// vendored Argon2 in behind it, which playwright's loader does not get
// through (measured; the reason is written down there). They are re-exported
// here so mock/api.js keeps reading one file.
export {
  USER_NAME, PASSWORD, TOTP_NAME, PASSKEY_NAME, BOTH_NAME, TOTP_CODE, RP_ID, RP_ORIGINS,
  PASSKEY_USERS,
} from "../fixtures/logins.js";

export const USER_ID = b64encode(new Uint8Array(16).fill(0x22));

const BERND_ID = b64encode(new Uint8Array(16).fill(0x23));
const CLARA_ID = b64encode(new Uint8Array(16).fill(0x24));
const DORA_ID = b64encode(new Uint8Array(16).fill(0x25));
// The two roles the login cases need. A role cannot be both, which
// trust.validateList refuses outright, so they are two.
const ROLE_PASSKEY_ONLY = "nurpasskey";
const ROLE_PASSWORD_TOO = "beides";

// The KDF of that password key. m and t are the values Thomas decided on
// (the contract minimum, a quarter of the work of the old defaults): a test
// that spent a second per login inside the browser would be a test nobody
// runs twice.
export const KDF = Object.freeze({
  alg: proof.KDF_ARGON2ID,
  salt: b64encode(new Uint8Array(16).fill(0x05)),
  m: proof.MIN_MEM_KIB, t: 2, p: 1,
});

const SEED_SIZE = 32;

// seedOf is what proof.deriveSeed computes in the browser, computed here with
// the very same copied library. There is no worker in node, and none is
// needed: nothing is kept here, this is the mock's own side of a test key.
function seedOf(passwordText, kdf) {
  return argon2id(utf8(passwordText.normalize("NFC")), b64decode(kdf.salt),
    { t: kdf.t, m: kdf.m, p: kdf.p, dkLen: SEED_SIZE });
}

// es256Public rebuilds the public half of a fixed P-256 private key. The Web
// Crypto API hands out no public key of a private one, so the private key is
// imported as extractable, read as a JWK and its x and y put back together
// as a public key - the same road proof.publicFromSeed walks for Ed25519.
// This is a mock and the key is in the repository, so nothing is given away.
async function es256Public(pkcs8Base64) {
  const der = Uint8Array.from(Buffer.from(pkcs8Base64, "base64"));
  const alg = { name: "ECDSA", namedCurve: "P-256" };
  const priv = await crypto.subtle.importKey("pkcs8", der, alg, true, ["sign"]);
  const jwk = await crypto.subtle.exportKey("jwk", priv);
  const pub = await crypto.subtle.importKey("jwk", { kty: "EC", crv: "P-256", x: jwk.x, y: jwk.y },
    alg, true, ["verify"]);
  return new Uint8Array(await crypto.subtle.exportKey("spki", pub));
}

// passkeyKey is one entry of a user's key list in the trust list's own shape:
// the public half, the credential ID the challenge offers and the key ID the
// proof names. proof.verify checks all three against each other.
async function passkeyKey(which) {
  const pub = await es256Public(PKCS8[which]);
  return {
    id: await sign.keyID(pub), kind: proof.KIND_PASSKEY, alg: proof.ALG_ES256,
    pub, credId: CRED_ID[which], kdf: null,
  };
}

// passwordKey is the password key of a password, with the salt this world
// gives that user. Two users never share a salt, so two people with the same
// password have different keys - which is what a salt is for.
async function passwordKey(passwordText, salt) {
  const kdf = { ...KDF, salt: b64encode(salt) };
  const pub = await proof.publicFromSeed(seedOf(passwordText, kdf));
  return {
    key: {
      id: await sign.keyID(pub), kind: proof.KIND_PASSWORD, alg: proof.ALG_EDDSA,
      pub, credId: new Uint8Array(0),
      kdf: { alg: kdf.alg, salt: b64decode(kdf.salt), m: kdf.m, t: kdf.t, p: kdf.p },
    },
    kdf,
  };
}

async function ed25519() {
  const pair = await crypto.subtle.generateKey({ name: "Ed25519" }, false, ["sign", "verify"]);
  return { priv: pair.privateKey, pub: new Uint8Array(await crypto.subtle.exportKey("raw", pair.publicKey)) };
}

async function x25519() {
  const pair = await crypto.subtle.generateKey({ name: "X25519" }, false, ["deriveBits"]);
  return { priv: pair.privateKey, pub: new Uint8Array(await crypto.subtle.exportKey("raw", pair.publicKey)) };
}

// The one app that is not a core app, so its channels run end to end. It is
// the app shared/testdata already knows, and the one core.json's app list
// names.
export const APP = "org.sorglos.terminal";
export const APP_SERVICE = "terminal";
export const APP_ACTION = "shell";

export async function buildWorld() {
  const genesis = testdata("trust.json").find((c) => c.name === "genesis");
  // Through the contract, not through JSON.parse: what the mock starts from
  // is a list the shell's own parser accepts.
  const list = await trust.parseList(b64decode(genesis.chain[0].payload));

  const apiSign = await ed25519();
  const apiNoise = await x25519();
  const ctlNoise = await x25519();
  const listSigner = await ed25519();

  const seed = seedOf(PASSWORD, KDF);
  const userPub = await proof.publicFromSeed(seed);
  const anna = {
    id: USER_ID, name: USER_NAME, roles: [trust.ROLE_ADMIN],
    keys: [{
      id: await sign.keyID(userPub), kind: proof.KIND_PASSWORD, alg: proof.ALG_EDDSA,
      pub: userPub, credId: new Uint8Array(0),
      kdf: { alg: KDF.alg, salt: b64decode(KDF.salt), m: KDF.m, t: KDF.t, p: KDF.p },
    }],
  };
  // The three other ways in. They are real entries of the trust list and not
  // a table beside it, so the mock decides passwordToo the way the API does -
  // by asking the list (trust.passwordToo) - instead of keeping a second
  // opinion that could drift from it.
  const bernd = {
    id: BERND_ID, name: TOTP_NAME, roles: [trust.ROLE_ADMIN],
    keys: [(await passwordKey(PASSWORD, new Uint8Array(16).fill(0x06))).key],
  };
  const clara = {
    id: CLARA_ID, name: PASSKEY_NAME, roles: [ROLE_PASSKEY_ONLY],
    keys: [await passkeyKey("clara")],
  };
  const dora = {
    id: DORA_ID, name: BOTH_NAME, roles: [ROLE_PASSWORD_TOO],
    keys: [await passkeyKey("dora"), (await passwordKey(PASSWORD, new Uint8Array(16).fill(0x08))).key],
  };
  // Sorted by name, as the list demands of roles.
  list.roles = [...list.roles,
    { name: ROLE_PASSWORD_TOO, level: trust.LEVEL_OFF, black: [], white: [],
      passkeyOnly: false, passwordToo: true },
    { name: ROLE_PASSKEY_ONLY, level: trust.LEVEL_OFF, black: [], white: [],
      passkeyOnly: true, passwordToo: false },
  ].sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
  // Sorted by id, as the list demands; anna's id sorts after both admins'.
  list.users = [...list.users, anna, bernd, clara, dora];
  for (const n of list.nodes) {
    if (n.id === "api-1") { n.sign = apiSign.pub; n.noise = apiNoise.pub; }
    if (n.id === "ctl-1") { n.noise = ctlNoise.pub; }
  }

  const payload = trust.encodeList(list);
  // A real, self-consistent proof over the real payload. It is not the
  // genesis admin's - nobody holds that key - and nothing in these tests
  // follows the chain from the anchor (that is task 20). What it does buy is
  // that session.parseTrustChain and proof.validateProof read it as the
  // document they will read in operation.
  const listSig = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" },
    listSigner.priv, sign.message(sign.PURPOSE_TRUST_LIST, payload)));
  const chain = utf8(JSON.stringify({
    versions: [{
      payload: b64encode(payload),
      proof: {
        kind: proof.KIND_PASSWORD, kid: await sign.keyID(listSigner.pub),
        sig: b64encode(listSig), authData: "", clientData: "",
      },
    }],
  }));

  return {
    list,
    installation: list.installation,
    chainBytes: chain,
    user: { id: USER_ID, name: USER_NAME, key: anna.keys[0], kdf: KDF },
    // Who may log in, by the name that is typed. Everything else about them -
    // their keys, whether a role asks for the password as well - the mock
    // reads out of the trust list, because that is where the API reads it.
    // Only the second factor lives here: a trust list holds no TOTP secret.
    logins: new Map([
      [USER_NAME, { id: USER_ID, totp: false }],
      [TOTP_NAME, { id: BERND_ID, totp: true }],
      [PASSKEY_NAME, { id: CLARA_ID, totp: false }],
      [BOTH_NAME, { id: DORA_ID, totp: false }],
    ]),
    api: { id: "api-1", sign: apiSign, noise: apiNoise },
    controller: { id: "ctl-1", noise: ctlNoise },
    results: results(),
  };
}

// The answers of the core actions, taken from shared/testdata/core.json and
// written with the contract's own encoder where there is one. Every entry is
// held against core.parseResult before it is handed out, so a mock that
// wrote an answer the shell refuses fails here and not three layers away.
function results() {
  const data = testdata("core.json");
  const out = new Map();
  const put = (app, name, value) => {
    const action = core.lookupByKey(app, name);
    const bytes = utf8(JSON.stringify(value));
    core.parseResult(action, bytes);
    out.set(`${action.app}/${action.service}/${action.name}`, bytes);
  };
  // The one answer that is changed: the tests log in as anna, so "who am I"
  // has to say anna. Everything else in the record stays core.json's.
  put(core.APP_DESKTOP, core.ACT_ME, {
    ...data["ngc.desktop/me"], id: USER_ID, name: USER_NAME,
  });
  put(core.APP_DESKTOP, core.ACT_APPS, data["ngc.desktop/apps"]);
  put(core.APP_STORE, core.ACT_SOURCES, data["ngc.store/sources"]);
  put(core.APP_USERS, core.ACT_LIST, data["ngc.users/list"]);
  put(core.APP_USERS, core.ACT_NODES, data["ngc.users/nodes"]);
  return out;
}
