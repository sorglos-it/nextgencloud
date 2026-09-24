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

// The list-shaped counterpart of isBytes (Fixrunde 2: Wichtig 4 widened from
// bytes to every array field checked by .length without confirming it is an
// array first). A plain object with a length property and numeric keys - an
// "array-like" - is not caught by any .length comparison: {length: 0} passes
// a === 0 or a + === 0 check exactly as [] would, and {length: 1, 0: <a
// genuinely valid element>} passes both a length bound and, wherever a field
// is walked by index (l.users, l.roles, l.nodes, l.stores, l.auto,
// validAPINode's rootUsers/rootRoles sum) is read back out as if it were a
// real array member. Where a field is instead walked with for...of, .find,
// .some or .every (rp.origins, a user's roles/keys, a controller's
// rootUsers/rootRoles, role(), validEntries), an array-like throws there
// instead, because a plain object has no Symbol.iterator and none of the
// array prototype methods - still not `false`, still a break of this
// module's "-> boolean, never throws" promise. Both failure modes close with
// the same one-line guard.
const isArray = Array.isArray;

// validateList checks structure, order and references of l, the rules of
// trust.List.Validate point by point. Never throws - Round 1's l.prev guard
// and the isArray/isBytes checks added since are the precise, reasoned
// rejections named at each of them; the try below is the backstop for what
// those do not individually name. Fixrunde 6, the closing review: measured
// still throwing on l.users/roles/nodes/stores/auto containing a null
// element - isArray([null]) is true (Array.isArray only ever asks about the
// array, never its contents), so the element is read two lines later as if
// it were a real user/role/node/store/auto entry. Same fix and the same
// reason sign.js's validateEnvelope already states for the identical
// problem: "An exception is no answer, and a check that has none refuses."
export function validateList(l) {
  try {
    if (!l || !(l.prev instanceof Uint8Array)) return false;
    if (l.seq === 0 || (l.seq === 1) !== (l.prev.length === 0) || (l.seq > 1 && l.prev.length !== 32)) return false;
    if (l.issued <= 0 || !validID(l.installation)) return false;
    if (!isArray(l.users) || !isArray(l.roles) || !isArray(l.nodes) || !isArray(l.stores) || !isArray(l.auto)
      || !isArray(l.rp?.origins)) return false;
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
          || !isArray(n.rootUsers) || !isArray(n.rootRoles)
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
  } catch {
    return false;
  }
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
      || !isArray(u.roles) || !isArray(u.keys)
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
  // One key's check does not depend on any other's, so a user's keys run as
  // one Promise.all group instead of one sequential await at a time -
  // bounded by construction, since MAX_KEYS_PER_USER caps every group at 16,
  // never all of a list's keys (up to MAX_USERS x MAX_KEYS_PER_USER) at once
  // (Fixrunde 3: sequential awaiting measured at ~15.3 ms per key in
  // WebKit's Web Crypto - about 340x Chromium's ~0.045 ms - which alone
  // accounted for the whole cost of parsing a large list; see the task
  // report for the batched number).
  for (const u of l.users) {
    const oks = await Promise.all(u.keys.map((k) => proof.validateUserKey(k)));
    if (oks.some((ok) => !ok)) throw new Error("trust: invalid list");
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

// chain (here and in advance) is not guarded the way validateList's and
// encodeList's fields now are: a malformed chain rejects this promise
// instead of answering a wrong true/false/value, which is what every fix in
// this task was actually about, so there is nothing here for that fix to close.
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
//
// Fixrunde 5: encodeList is exported and takes caller data, the same
// criterion as every other function this task closed - that it is normally
// handed parseList's own output does not change that; exported means
// someone can call it another way. Unlike those other functions it has no
// natural false/null to fall back to (its job is to produce bytes, not
// decide or look something up), so the whole body runs inside one try:
// whatever is wrong, and at whatever depth in encodeUser/encodeUserKey/
// encodeRole/encodeNode/encodeStore/encodeAuto it sits at, this answers the
// one controlled error parseList/parseSigned already promise on their own
// failures, not whichever uncontrolled exception happened to surface first.
export function encodeList(l) {
  try {
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
  } catch {
    throw new Error("trust: cannot encode this list");
  }
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

// Fixrunde 4: user, role, node, keyOwner and passwordToo are the third and,
// per the coordinator, last appearance in this task of the pattern
// mayRoot/matches/needsConfirm and validateList's own family already
// closed - exported, an array-only method on an l the caller controls, no
// Array.isArray first. l?.users (optional chaining) doubles as the guard
// for l itself being missing, the same trick validateList's l.rp?.origins
// uses.
export const user = (l, id) => (isArray(l?.users) && l.users.find((u) => u.id === id)) || null;
export const role = (l, name) => (isArray(l?.roles) && l.roles.find((r) => r.name === name)) || null;
export const node = (l, id) => (isArray(l?.nodes) && l.nodes.find((n) => n.id === id)) || null;

// Fixrunde 6 needed a try here for the same element-can-be-null reason as
// validateList: isArray(u.keys) says the array is real, not that a null
// inside it is a key. Fixrunde 7 moved that try inside the user loop, where
// it belongs. Around the loop it caught the exception and returned - which is
// exactly the abort the line below promises not to do, and it hid a real key
// a later, well-formed user owns. One user per try, one continue per failure:
// a user whose keys throw on being read is skipped like one whose keys is not
// an array at all. If no user is left to search, the loop ends and the answer
// is the same null this function already gives for "not found". What stands
// in front of the loop - reading l?.users, and iterating it - is outside the
// try and stays as unguarded as each of user/role/node leaves its own such
// read above: there is no next user to skip to there. Measured, so it is not
// read as more than it is: all four throw, but each at its own field only -
// keyOwner and user at l.users, role at l.roles, node at l.nodes. A throwing
// users getter alone leaves role and node untouched, because neither ever
// reads that field; both answer the null they give for any list without a
// match.
export function keyOwner(l, kid) {
  if (!isArray(l?.users)) return null;
  for (const u of l.users) {
    try {
      // A malformed keys on one user must not hide a real key a different,
      // well-formed user actually owns: skip just that user, not the search.
      if (!isArray(u?.keys)) continue;
      for (const k of u.keys) if (k.id === kid) return { user: u, key: k };
    } catch {
      continue;
    }
  }
  return null;
}

export function passwordToo(l, userID) {
  const u = user(l, userID);
  if (!u) return false;
  // Unlike mayRoot/matches/needsConfirm, false is not the safe answer here:
  // true means the login flow demands a password in addition to a passkey
  // (Go: session.VerifyClient already requires the passkey either way -
  // PasswordToo only ever adds to that, never replaces it), so answering
  // false on doubt would silently accept a weaker login than Go would have
  // required. A malformed u.roles therefore answers true: require the extra
  // factor rather than skip it.
  if (!isArray(u.roles)) return true;
  return u.roles.some((n) => role(l, n)?.passwordToo);
}

// Fixrunde 3, point 2: mayRoot, needsConfirm and matches below are exported
// (or, for matches, reached from an exported function) and take an l a
// caller can build by hand, same as validateList's own family - .includes
// and .some are array-only methods with no Array.isArray first, so an
// array-like plain object threw here instead of being refused. Closes the
// same gap Wichtig 4 closed, on the coordinator's decision.
export function mayRoot(l, userID, nodeID) {
  const n = node(l, nodeID), u = user(l, userID);
  if (!n || !u || n.kind !== NODE_CONTROLLER) return false;
  if (!isArray(n.rootUsers) || !isArray(n.rootRoles) || !isArray(u.roles)) return false;
  if (n.rootUsers.includes(userID)) return true;
  return u.roles.some((r) => n.rootRoles.includes(r));
}

// op is {app, action, node, root, critical, changes}.
export function needsConfirm(l, userID, op) {
  const u = user(l, userID);
  // A malformed u.roles is the same kind of doubt as no user at all: confirm.
  if (!u || !isArray(u.roles)) return true;
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
// A malformed entries (Fixrunde 3, point 2) answers false on both of its
// call sites in needsConfirm, which is the safe direction either way: a
// malformed black no longer forces a confirm by accident, but level checks
// still can; a malformed white no longer grants a bypass by accident.
const matches = (entries, op) => {
  if (!isArray(entries)) return false;
  const nd = op.node ?? "";
  return entries.some((e) =>
    e === "app/" + op.app || e === "app/" + op.app + "/" + op.action || (nd !== "" && e === "node/" + nd));
};

// Not part of this task's interface list and unexported in Go too (Round 1
// finding K4) - validAPINode below is the one exported surface that needs it.
const validNode = (n) => validName(n.id) && validText(n.name, 64) && isBytes(n.noise, 32);

// Fixrunde 6: n.kind is read before validNode(n) or any isArray/isBytes
// guard gets a chance to run, so validAPINode(null) and validAPINode()
// threw at that first read instead of answering false. Fixrunde 7, K2:
// those two are the whole list, measured - a number, a string, a boolean, a
// symbol, a bigint, an array or a function all read n.kind as undefined and
// were already refused one comparison later, and "non-object" would not
// even name null, since typeof null is "object". So the explicit guard
// sign.js's validateEnvelope writes in front of its own try (env === null
// || typeof env !== "object") is enough for both of them, and it now stands
// here too, in front rather than instead of the try. The try stays behind
// it for what it alone can catch: a field of a real object that throws on
// being read - a getter or a proxy trap - which no guard in front sees.
export const validAPINode = (n) => {
  if (n === null || typeof n !== "object") return false;
  try {
    return n.kind === NODE_API && validNode(n) && isBytes(n.sign, 32)
      && validTunnelURL(n.url) && isBytes(n.tls, 32)
      && isArray(n.rootUsers) && isArray(n.rootRoles) && n.rootUsers.length + n.rootRoles.length === 0;
  } catch {
    return false;
  }
};

// StoreLocal marks uploaded apps, so no catalog key may claim it. s.id was
// read at the very start, before anything in this function could refuse it -
// the same problem n.kind had above - so validStore(null) and validStore()
// threw at that first read, while every other type was already refused by
// validName(undefined). Fixrunde 7, K1: the guard sign.js writes now stands
// in front of the same try here too, and answers those two.
export const validStore = (s) => {
  if (s === null || typeof s !== "object") return false;
  try {
    return validName(s.id) && s.id !== STORE_LOCAL && validText(s.name, 64)
      && validStoreURL(s.url) && isBytes(s.key, 32);
  } catch {
    return false;
  }
};

export async function tlsPin(spki) {
  return new Uint8Array(await crypto.subtle.digest("SHA-256", spki));
}

const validLevel = (l) => l === LEVEL_OFF || l === LEVEL_CRITICAL || l === LEVEL_ROOT || l === LEVEL_HARDCORE;

function validEntries(entries) {
  if (!isArray(entries) || entries.length > MAX_ENTRIES) return false;
  return entries.every((e) => {
    // Array.isArray(entries) alone says nothing about what is inside it -
    // .startsWith below is a string method, called directly on e.
    if (typeof e !== "string") return false;
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
