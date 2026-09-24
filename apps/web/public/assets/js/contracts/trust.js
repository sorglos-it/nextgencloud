// The trust list: users and their keys, API servers and controllers, root
// permissions, stores, the re-confirmation policy and standing approvals. An
// admin signs every version; the shell verifies the chain from a pinned anchor
// itself, so the API can hand the list on but never forge it (Go: trust).
//
// Two of the imports below differ from the task brief's own line, both named
// in the Round 1 report: b64encode and encodedLen, for encodeList and
// signedBudget - both were prose in the brief, not given code, so the given
// import line never accounted for them. b64decode, P and uint32 were in that
// line but are not used anywhere in this file (Round 1 finding K3) and are
// left out.
import { b64encode, encodedLen, equal, utf8 } from "./b64.js";
import { decode, Budget, S, A, str, bool, bytes, uint } from "./json.js";
import { validName, validAppID, coreApp, validText, validID, validLoginName } from "./text.js";
import { validTunnelURL, validStoreURL, validOrigin } from "./url.js";
import * as proof from "./proof.js";
import { MAX_PAYLOAD, PURPOSE_TRUST_LIST } from "./sign.js";

export const ROLE_ADMIN = "admin";
export const MAX_USERS = 10000, MAX_NODES = 256, MAX_ROLES = 256, MAX_STORES = 64;
export const MAX_KEYS_PER_USER = 16, MAX_ENTRIES = 256, MAX_AUTO = 64, MAX_ORIGINS = 8, MAX_ROOT = 256;
export const LEVEL_OFF = "off", LEVEL_CRITICAL = "critical", LEVEL_ROOT = "root", LEVEL_HARDCORE = "hardcore";
export const NODE_API = "api", NODE_CONTROLLER = "controller";
export const STORE_GLOBAL = "global", STORE_LOCAL = "local";
export const AUTO_WEB = "ngc-web", AUTO_API = "ngc-api", AUTO_CONTROLLER = "ngc-controller";
export const AUTO_SYSTEM = "system";

const EMPTY = new Uint8Array(0);

const userShape = S({ id: str, name: str, roles: A(str), keys: A(proof.userKeyShape) });
const roleShape = S({ name: str, level: str, black: A(str), white: A(str),
  passkeyOnly: bool, passwordToo: bool });
const nodeShape = S({ id: str, kind: str, name: str, noise: bytes, sign: bytes, url: str,
  tls: bytes, rootUsers: A(str), rootRoles: A(str) });
const storeShape = S({ id: str, name: str, url: str, key: bytes });
const autoShape = S({ target: str, key: bytes });

export const listShape = S({
  seq: uint, prev: bytes, issued: uint, installation: str, rp: proof.rpShape,
  users: A(userShape), roles: A(roleShape), nodes: A(nodeShape),
  stores: A(storeShape), auto: A(autoShape),
});

export const signedShape = S({ payload: bytes, proof: proof.proofShape });

const listBudget = new Budget({
  size: MAX_PAYLOAD,
  items: {
    "rp/origins": MAX_ORIGINS,
    users: MAX_USERS,
    "users/*/roles": MAX_ROLES,
    "users/*/keys": MAX_KEYS_PER_USER,
    roles: MAX_ROLES,
    "roles/*/black": MAX_ENTRIES,
    "roles/*/white": MAX_ENTRIES,
    nodes: MAX_NODES,
    "nodes/*/rootUsers": MAX_ROOT,
    "nodes/*/rootRoles": MAX_ROOT,
    stores: MAX_STORES,
    auto: MAX_AUTO,
  },
});

// The base64url of a payload of MAX_PAYLOAD bytes, plus 64 KiB for the proof
// (at most about 12 KiB - a maximal passkey proof alone is 12 290 base64
// characters: sig 1366 + authData 5462 + clientData 5462), member names and
// white space (Go: shared/trust/trust.go, signedBudget). Round 1 finding
// Wichtig 2: this used to borrow sign.envelopeBudget(MAX_PAYLOAD).size, whose
// 1 KiB ENVELOPE_SLACK is sized for a plain envelope's kid+sig - 64512 bytes
// too little for a trust.Signed, which carries a full proof.Proof. It closes
// (refuses a bit more than it should), never opens, but a real admin-signed
// version with a large payload and a large proof would be refused by the
// browser and accepted by every Go node - measured, in the test this fixes.
const SIGNED_SLACK = 64 << 10;
const signedBudget = new Budget({ size: encodedLen(MAX_PAYLOAD) + SIGNED_SLACK });

// A byte string is a Uint8Array or it is not one - not an ArrayBuffer, and
// not same-length text: "A".repeat(32) has a .length of 32 same as 32 real
// bytes, and would pass n.noise.length === 32 unnoticed (Round 1 finding
// Wichtig 4; the same reasoning b64.js's equal and text.js's validText give
// for checking the type before the length). validateList, validAPINode and
// validStore are exported and so can be handed anything, not only what
// decode() already shaped through nodeShape/storeShape - unlike parseList,
// which only ever sees this module's own decode() output.
const isBytes = (v, n) => v instanceof Uint8Array && v.length === n;

// validateList checks structure, order and references of l, the rules of
// trust.List.Validate point by point. Never throws (Round 1 finding
// Wichtig 4: validateList({}) threw a TypeError at l.prev.length instead of
// answering false, though the interface promises -> boolean) - the guard
// below on l.prev is both the first rule Go checks and, being first, the one
// that stands between an arbitrary argument and every other read in this
// function.
export function validateList(l) {
  if (!l || !(l.prev instanceof Uint8Array)) return false;
  if (l.seq === 0 || (l.seq === 1) !== (l.prev.length === 0) || (l.seq > 1 && l.prev.length !== 32)) return false;
  if (l.issued <= 0 || !validID(l.installation)) return false;
  if (l.users.length === 0 || l.users.length > MAX_USERS || l.roles.length > MAX_ROLES
    || l.nodes.length > MAX_NODES || l.stores.length > MAX_STORES || l.auto.length > MAX_AUTO
    || l.rp.origins.length > MAX_ORIGINS) return false;
  // The RP ID goes on into the login answer and from there into the
  // browser's navigator.credentials.get, so it is a DNS name here already
  // (proof.validRPID, the same rule config.Web keeps). Round 1 finding
  // Wichtig 1: this check was missing outright; none of the 24 vectors ever
  // sets an invalid rp.id (all 52 payloads carry "ngc.example.org"), so its
  // absence never turned a test red on its own.
  if (l.rp.id !== "" && !proof.validRPID(l.rp.id)) return false;

  for (let i = 0; i < l.roles.length; i++) {
    const r = l.roles[i];
    // A role cannot both forbid password keys and ask for the password.
    if (!validName(r.name) || (i > 0 && l.roles[i - 1].name >= r.name) || !validLevel(r.level)
      || !validEntries(r.black) || !validEntries(r.white) || (r.passkeyOnly && r.passwordToo)) return false;
  }
  if (!role(l, ROLE_ADMIN)) return false;
  if (!validateUsersSync(l)) return false;

  for (let i = 0; i < l.nodes.length; i++) {
    const n = l.nodes[i];
    if (i > 0 && l.nodes[i - 1].id >= n.id) return false;
    if (n.kind === NODE_API) {
      if (!validAPINode(n)) return false;
    } else if (n.kind === NODE_CONTROLLER) {
      // Signing keys, addresses and TLS pins belong to API nodes: nobody
      // dials a controller.
      if (!validNode(n) || !isBytes(n.sign, 0) || n.url !== "" || !isBytes(n.tls, 0)
        || n.rootUsers.length > MAX_ROOT || n.rootRoles.length > MAX_ROOT) return false;
      for (const id of n.rootUsers) if (!user(l, id)) return false;
      for (const r of n.rootRoles) if (!role(l, r)) return false;
    } else {
      return false;
    }
  }
  for (let i = 0; i < l.stores.length; i++) {
    if (!validStore(l.stores[i]) || (i > 0 && l.stores[i - 1].id >= l.stores[i].id)) return false;
  }
  for (let i = 0; i < l.auto.length; i++) {
    const a = l.auto[i];
    if (i > 0 && l.auto[i - 1].target >= a.target) return false;
    if (a.target === AUTO_SYSTEM) {
      if (!isBytes(a.key, 0)) return false;
    } else if (a.target === AUTO_WEB || a.target === AUTO_API || a.target === AUTO_CONTROLLER
      || (validAppID(a.target) && !coreApp(a.target))) {
      if (!isBytes(a.key, 32)) return false;
    } else {
      return false;
    }
  }
  return true;
}

// validateUsersSync checks each user, the rules of trust.List.validateUsers
// point by point (Go: shared/trust/trust.go): a valid ID, ascending IDs
// without a duplicate, a valid login name not used twice, at least one role,
// ascending roles that all exist, 1 to MAX_KEYS_PER_USER keys, no key ID used
// twice across every user, no password key in a role that is passkeyOnly,
// passwordToo only with both a passkey and a password key, passkeyOnly and
// passwordToo never together, at least one admin; wherever a passkey exists,
// rp.id and at least one rp.origins must be set, and every origin present -
// whether or not a passkey is in play - must be validOrigin. A key's own
// structure is proof.validateUserKeySync, which leaves out id === keyID(pub):
// that one check runs afterwards and asynchronously, in parseList.
function validateUsersSync(l) {
  const keys = new Set();
  const names = new Set();
  let admins = 0, passkeys = false;
  for (let i = 0; i < l.users.length; i++) {
    const u = l.users[i];
    if (!validID(u.id) || (i > 0 && l.users[i - 1].id >= u.id) || !validLoginName(u.name) || names.has(u.name)
      || u.roles.length === 0 || u.keys.length === 0 || u.keys.length > MAX_KEYS_PER_USER) return false;
    names.add(u.name);
    let passkeyOnly = false, passwordToo = false;
    for (let j = 0; j < u.roles.length; j++) {
      const name = u.roles[j];
      const r = role(l, name);
      if (!r || (j > 0 && u.roles[j - 1] >= name)) return false;
      passkeyOnly = passkeyOnly || r.passkeyOnly;
      passwordToo = passwordToo || r.passwordToo;
    }
    // Such a user could never log in: one role forbids the password key the
    // other one asks for.
    if (passkeyOnly && passwordToo) return false;
    let hasPasskey = false, hasPassword = false;
    for (const k of u.keys) {
      if (!proof.validateUserKeySync(k) || keys.has(k.id) || (passkeyOnly && k.kind === proof.KIND_PASSWORD)) {
        return false;
      }
      keys.add(k.id);
      hasPasskey = hasPasskey || k.kind === proof.KIND_PASSKEY;
      hasPassword = hasPassword || k.kind === proof.KIND_PASSWORD;
    }
    passkeys = passkeys || hasPasskey;
    // Such a user could never log in either: a role that asks for the
    // password too needs a passkey and a password key (session.VerifyClient,
    // VerifyPassword). Setting it on the admin role would lock every admin
    // out, and the setup cannot run again once an admin exists.
    if (passwordToo && !(hasPasskey && hasPassword)) return false;
    if (u.roles.includes(ROLE_ADMIN)) admins++;
  }
  if (admins === 0 || (passkeys && (l.rp.id === "" || l.rp.origins.length === 0))) return false;
  for (const o of l.rp.origins) {
    if (!validOrigin(o)) return false;
  }
  return true;
}

export async function parseList(payload) {
  const l = decode(payload, listBudget, listShape);
  if (!validateList(l)) throw new Error("trust: invalid list");
  // The key id is the hash of the key, so a list may not claim another one.
  for (const u of l.users) {
    for (const k of u.keys) {
      if (!(await proof.validateUserKey(k))) throw new Error("trust: invalid list");
    }
  }
  return l;
}

export function parseSigned(b) {
  const s = decode(b, signedBudget, signedShape);
  if (s.payload.length === 0 || s.payload.length > MAX_PAYLOAD || !proof.validateProof(s.proof)) {
    throw new Error("trust: invalid list");
  }
  return s;
}

export async function hash(signed) {
  return new Uint8Array(await crypto.subtle.digest("SHA-256", signed.payload));
}

export async function open(anchor, chain) {
  if (chain.length === 0) throw new Error("trust: no anchor");
  const h = await hash(chain[0]);
  if (!equal(h, anchor.hash)) throw new Error("trust: does not match the anchor");
  const l = await parseList(chain[0].payload);
  if (l.seq !== anchor.seq) throw new Error("trust: does not match the anchor");
  if (l.seq === 1) {
    // Version 1 signs itself, so only an admin of it may have signed.
    const u = await verifySigner(l, chain[0]);
    if (!u.roles.includes(ROLE_ADMIN)) throw new Error("trust: signer may not make this change");
  }
  return advance({ list: l, hash: h }, chain.slice(1));
}

export async function advance(cur, chain) {
  if (!cur.list) throw new Error("trust: broken chain");
  for (const s of chain) {
    const u = await verifySigner(cur.list, s);
    const next = await parseList(s.payload);
    if (next.seq !== cur.list.seq + 1 || !equal(next.prev, cur.hash)
      || next.installation !== cur.list.installation) throw new Error("trust: broken chain");
    if (!u.roles.includes(ROLE_ADMIN) && !onlyOwnKeys(cur.list, next, u.id)) {
      throw new Error("trust: signer may not make this change");
    }
    cur = { list: next, hash: await hash(s) };
  }
  return cur;
}

async function verifySigner(prev, s) {
  const found = keyOwner(prev, s.proof.kid);
  if (!found) throw new Error("trust: unknown signer");
  if (!(await proof.verify(s.proof, found.key, prev.rp, PURPOSE_TRUST_LIST, s.payload))) {
    throw new Error("trust: bad signature");
  }
  return found.user;
}

// onlyOwnKeys reports whether next differs from prev in nothing but the keys
// of user id (and the version fields) (Go: trust.onlyOwnKeys). Both versions
// are already parseList's output, so every array below is not just present
// but non-empty wherever Go has no omitempty on it - the one case that needs
// naming is the user's own keys, cleared to null on both sides to match Go's
// nil slice on a field without omitempty; every other cleared or absent field
// has omitempty in Go and encodeList leaves it out the same way.
function onlyOwnKeys(prev, next, id) {
  const i = prev.users.findIndex((u) => u.id === id);
  const j = next.users.findIndex((u) => u.id === id);
  if (i < 0 || j < 0) return false;
  const a = { ...prev, seq: 0, prev: EMPTY, issued: 0,
    users: prev.users.map((u, k) => (k === i ? { ...u, keys: null } : u)) };
  const b = { ...next, seq: 0, prev: EMPTY, issued: 0,
    users: next.users.map((u, k) => (k === j ? { ...u, keys: null } : u)) };
  return equal(encodeList(a), encodeList(b));
}

// encodeList writes list as canonical JSON: the fields of the Go structs
// (List, proof.RP, User, Role, Node, Store, Auto, proof.UserKey, proof.KDF)
// in their declared order, a field left out wherever Go's struct tag carries
// omitempty and the value is empty, bytes as base64url (Go: enc.Bytes -
// encoding/json, called from trust.onlyOwnKeys, and by Task 23 to build the
// payload of a new version). A user's keys is the one array without
// omitempty that onlyOwnKeys can still hand this function as null rather than
// a list, for the one user whose keys do not count toward the comparison;
// encodeList writes that null literally, exactly as Go's json.Marshal would
// for a nil slice on a field with no omitempty.
export function encodeList(l) {
  const o = { seq: l.seq };
  if (l.prev.length > 0) o.prev = b64encode(l.prev);
  o.issued = l.issued;
  o.installation = l.installation;
  o.rp = encodeRP(l.rp);
  o.users = l.users.map(encodeUser);
  o.roles = l.roles.map(encodeRole);
  if (l.nodes.length > 0) o.nodes = l.nodes.map(encodeNode);
  if (l.stores.length > 0) o.stores = l.stores.map(encodeStore);
  if (l.auto.length > 0) o.auto = l.auto.map(encodeAuto);
  return utf8(JSON.stringify(o));
}

function encodeRP(rp) {
  const o = { id: rp.id };
  if (rp.origins.length > 0) o.origins = rp.origins;
  return o;
}

function encodeUser(u) {
  return { id: u.id, name: u.name, roles: u.roles, keys: u.keys === null ? null : u.keys.map(encodeUserKey) };
}

function encodeUserKey(k) {
  const o = { id: k.id, kind: k.kind, alg: k.alg, pub: b64encode(k.pub) };
  if (k.credId.length > 0) o.credId = b64encode(k.credId);
  if (k.kdf !== undefined && k.kdf !== null) o.kdf = encodeKDF(k.kdf);
  return o;
}

function encodeKDF(k) {
  return { alg: k.alg, salt: b64encode(k.salt), m: k.m, t: k.t, p: k.p };
}

function encodeRole(r) {
  const o = { name: r.name, level: r.level };
  if (r.black.length > 0) o.black = r.black;
  if (r.white.length > 0) o.white = r.white;
  if (r.passkeyOnly) o.passkeyOnly = true;
  if (r.passwordToo) o.passwordToo = true;
  return o;
}

function encodeNode(n) {
  const o = { id: n.id, kind: n.kind, name: n.name, noise: b64encode(n.noise) };
  if (n.sign.length > 0) o.sign = b64encode(n.sign);
  if (n.url !== "") o.url = n.url;
  if (n.tls.length > 0) o.tls = b64encode(n.tls);
  if (n.rootUsers.length > 0) o.rootUsers = n.rootUsers;
  if (n.rootRoles.length > 0) o.rootRoles = n.rootRoles;
  return o;
}

function encodeStore(s) {
  return { id: s.id, name: s.name, url: s.url, key: b64encode(s.key) };
}

function encodeAuto(a) {
  const o = { target: a.target };
  if (a.key.length > 0) o.key = b64encode(a.key);
  return o;
}

export const user = (l, id) => l.users.find((u) => u.id === id) || null;
export const role = (l, name) => l.roles.find((r) => r.name === name) || null;
export const node = (l, id) => l.nodes.find((n) => n.id === id) || null;

export function keyOwner(l, kid) {
  for (const u of l.users) {
    for (const k of u.keys) if (k.id === kid) return { user: u, key: k };
  }
  return null;
}

export function passwordToo(l, userID) {
  const u = user(l, userID);
  return !!u && u.roles.some((n) => role(l, n)?.passwordToo);
}

export function mayRoot(l, userID, nodeID) {
  const n = node(l, nodeID), u = user(l, userID);
  if (!n || !u || n.kind !== NODE_CONTROLLER) return false;
  if (n.rootUsers.includes(userID)) return true;
  return u.roles.some((r) => n.rootRoles.includes(r));
}

// op is {app, action, node, root, critical, changes}.
export function needsConfirm(l, userID, op) {
  const u = user(l, userID);
  if (!u) return true; // an unknown user always confirms
  return u.roles.some((name) => {
    const r = role(l, name);
    if (!r) return true;
    if (matches(r.black, op)) return true;
    if (matches(r.white, op)) return false;
    if (r.level === LEVEL_CRITICAL) return op.critical;
    if (r.level === LEVEL_ROOT) return op.critical || op.root;
    if (r.level === LEVEL_HARDCORE) return op.critical || op.root || op.changes;
    return false;
  });
}

// Round 1 finding K2: op.node !== "" was true for op.node === undefined too,
// and "node/" + undefined then compared equal to a white-list entry
// "node/undefined" (a well-formed entry: validName("undefined") is true) -
// excusing an operation that was never about any node, in the opening
// direction. nd falls back to Go's own zero value "" for a caller who left
// op.node unset, same as Go's Op.Node would read for a zero-value Op.
const matches = (entries, op) => {
  const nd = op.node ?? "";
  return entries.some((e) =>
    e === "app/" + op.app || e === "app/" + op.app + "/" + op.action || (nd !== "" && e === "node/" + nd));
};

// Not part of this task's interface list and unexported in Go too (Round 1
// finding K4) - validAPINode below is the one exported surface that needs it.
const validNode = (n) => validName(n.id) && validText(n.name, 64) && isBytes(n.noise, 32);

export const validAPINode = (n) => n.kind === NODE_API && validNode(n) && isBytes(n.sign, 32)
  && validTunnelURL(n.url) && isBytes(n.tls, 32) && n.rootUsers.length + n.rootRoles.length === 0;

// StoreLocal marks uploaded apps, so no catalog key may claim it.
export const validStore = (s) => validName(s.id) && s.id !== STORE_LOCAL && validText(s.name, 64)
  && validStoreURL(s.url) && isBytes(s.key, 32);

export async function tlsPin(spki) {
  return new Uint8Array(await crypto.subtle.digest("SHA-256", spki));
}

const validLevel = (l) => l === LEVEL_OFF || l === LEVEL_CRITICAL || l === LEVEL_ROOT || l === LEVEL_HARDCORE;

function validEntries(entries) {
  if (entries.length > MAX_ENTRIES) return false;
  return entries.every((e) => {
    if (e.startsWith("node/")) return validName(e.slice(5));
    if (e.startsWith("app/")) {
      const rest = e.slice(4);
      const i = rest.indexOf("/");
      const app = i < 0 ? rest : rest.slice(0, i);
      return validAppID(app) && (i < 0 || validName(rest.slice(i + 1)));
    }
    return false;
  });
}
