// Package tunnel: pairing and joining. The fingerprints an admin compares with
// what the ulh script of a new server prints, and the codes the shell shows
// only when they fit its own verified version of the trust list (Go: tunnel).
//
// Everything here is read by a person and then acted on: a fingerprint is
// compared by eye and the keys behind it are dialled afterwards, a code is
// pasted from one machine into another. So both sides have to answer the same
// for the same input, to the character, and the vectors in shared/testdata/
// tunnel.json are what says they do.
import { BadInput, b64decode, b64encode, bytesOf, concat, equal, utf8 } from "./b64.js";
import { BadJSON, Budget, S, bytes, decode, str, uint } from "./json.js";
import { validTunnelURL } from "./url.js";
import { keyIDAsync } from "./sign.js";
import { AUTO_API, AUTO_CONTROLLER, AUTO_WEB, NODE_API } from "./trust.js";

export const PAIR_PREFIX = "ngc-pair:";
export const JOIN_PREFIX = "ngc-join:";
export const MAX_PAIR_CODE = 1024; // also for join codes
export const SECRET_SIZE = 16;
// The version of the contracts. Every part also understands the previous one.
export const CONTRACT = 1;

// The roles a server carries (Go: tunnel.RoleWeb and its three siblings).
export const ROLE_WEB = "web", ROLE_API = "api", ROLE_DB = "db", ROLE_SERVICES = "services";

// A Noise key and a TLS pin are 32 bytes each, and so is the hash of a trust
// list version (Go: 32 for the curve, sha256.Size for the other two). They are
// named rather than written as 32 at the four places below, because four places
// with the same number are four places that can drift apart.
const KEY_SIZE = 32;
const HASH_SIZE = 32;
// A fingerprint is the first 16 bytes of a SHA-256, like a key id.
const FINGERPRINT_SIZE = 16;

// What everything wrong with a code is, as it is two errors in Go
// (tunnel.ErrPairCode and tunnel.ErrJoinCode): which rule the text broke is
// nothing a caller has to tell apart, and it is nothing to tell an admin
// either, because the message would have to quote the code to say it - and a
// code carries a secret. An error of the engine stays as it is and shows up as
// the mistake it would be.
export class BadCode extends Error {}

const codeShape = S({ url: str, key: bytes, tls: bytes, secret: bytes,
  anchor: S({ seq: uint, hash: bytes }) });
// A code has no array, so its budget is its size alone (Go: codeBudget).
const codeBudget = new Budget({ size: MAX_PAIR_CODE });

// fingerprint names a Noise key for people and in the tunnel URL (Go:
// tunnel.Fingerprint, which is sign.KeyID).
//
// The bytes are COPIED before they are hashed, and that is the whole point of
// this function. A fingerprint is a statement about bytes that go on being
// used after it: the admin reads it off one screen, compares it with the other,
// and then the key is dialled with. b64.viewOf believes byteLength, so an
// object that reports half its size would be named by the fingerprint of its
// first half and dialled with all of it - checked A, used B. b64.bytesOf reads
// no property at all: the copy comes out of the internal slots, where no getter
// can be installed, so what is hashed is what the object really holds.
export async function fingerprint(key) {
  return keyIDAsync(bytesOf(key, "tunnel: key is not bytes"));
}

// What joinFingerprint hashes over, and the zero byte that stands after the
// label and after the node id. Both are text of no fixed length, and without a
// byte that may appear in neither of them a longer label and a shorter node
// would hash to the same bytes as a shorter label and a longer node (Go: the
// same writes into sha256.New).
const JOIN_LABEL = "ngc/join-fingerprint/v1";
const SEPARATOR = new Uint8Array([0]);

// joinFingerprint names what a joining API asks to be listed with, so the admin
// can compare it with what that server's ulh script prints: base64url of the
// first 16 bytes of SHA-256 over the label, a zero byte, node, a zero byte,
// noise, signKey, tlsPin and url (Go: tunnel.JoinFingerprint).
//
// The three keys are copied for the same reason as in fingerprint: they are
// exactly the keys the API will be listed with once the admin has said yes.
//
// node and url are not measured against a grammar here, and that is Go's
// division too: tunnel.JoinFingerprint hashes what it is handed, and whoever
// hands it over - tunnel.ParseJoinRequest there, core here - has already run
// the name through wire.ValidName and the address through the address grammar.
// Checking twice with two different answers is how the two sides drift.
export async function joinFingerprint(node, noise, signKey, tlsPin, url) {
  const b = concat(utf8(JOIN_LABEL), SEPARATOR, utf8(node), SEPARATOR,
    bytesOf(noise, "tunnel: Noise key is not bytes"),
    bytesOf(signKey, "tunnel: signing key is not bytes"),
    bytesOf(tlsPin, "tunnel: TLS pin is not bytes"),
    utf8(url));
  const h = new Uint8Array(await crypto.subtle.digest("SHA-256", b));
  return b64encode(h.subarray(0, FINGERPRINT_SIZE));
}

// The space a pasted code may carry around it: Go's strings.TrimSpace, which is
// unicode.White_Space, written out here rather than borrowed from the engine.
// The engine's own trim differs in exactly two characters - it leaves U+0085,
// which Go trims, and it trims U+FEFF, which Go does not - and two characters
// are enough for a code the controller takes and the shell refuses, which is a
// support call nobody can answer.
//
// The set stands as escapes and not as the characters themselves, because two
// of them - U+2028 and U+2029 - are line terminators in a JavaScript source,
// and a regular expression written with them in it does not survive being read.
const SPACE_CLASS = "[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]";
const SPACE = new RegExp(`^${SPACE_CLASS}+|${SPACE_CLASS}+$`, "g");

function parseCode(prefix, s, why) {
  // Go is handed a string. Here anything at all may turn up, and "" + s would
  // make text out of it: an object whose toString answers with a code would be
  // parsed, and a second reading of it could answer with another.
  if (typeof s !== "string") throw new BadCode(why);
  const trimmed = s.replace(SPACE, "");
  // Go measures the trimmed text in bytes, this counts UTF-16 units, and the
  // two differ only for characters outside ASCII - which the prefix has none of
  // and which b64decode below refuses outright. So a text the two sides measure
  // differently is refused by both, here for its alphabet and there for its
  // size.
  if (!trimmed.startsWith(prefix) || trimmed.length > MAX_PAIR_CODE) throw new BadCode(why);
  let c;
  try {
    // Strict JSON within the budget: an unknown, a doubled or a differently
    // spelled member name is refused, so one code has one reading.
    c = decode(b64decode(trimmed.slice(prefix.length)), codeBudget, codeShape);
  } catch (e) {
    if (e instanceof BadJSON || e instanceof BadInput) throw new BadCode(why);
    throw e;
  }
  // Every byte string here was made by the decoder out of the fresh array
  // b64decode answered with, so each size is ours and cannot answer twice: what
  // these six rules are about is what the fields hand on.
  if (!validTunnelURL(c.url) || c.key.length !== KEY_SIZE || c.tls.length !== HASH_SIZE
    || c.secret.length !== SECRET_SIZE || c.anchor.seq === 0 || c.anchor.hash.length !== HASH_SIZE) {
    throw new BadCode(why);
  }
  // Frozen, because what checkCode decided about is what the caller dials with
  // afterwards. The fields cannot be swapped between the two any more, so the
  // one reading this function made is the one that goes on applying.
  Object.freeze(c.anchor);
  return Object.freeze(c);
}

// parsePairCode reads the code the API hands to the ulh script of a new
// controller (Go: tunnel.ParsePairCode).
export const parsePairCode = (s) => parseCode(PAIR_PREFIX, s, "tunnel: malformed pairing code");

// parseJoinCode reads the code a new API server is joined with: the rules and
// limits of parsePairCode, under the other prefix (Go: tunnel.ParseJoinCode).
export const parseJoinCode = (s) => parseCode(JOIN_PREFIX, s, "tunnel: malformed join code");

// checkCode compares a code with the newest version the shell verified: key,
// URL and pin must all belong to ONE API node, because whoever receives the
// code dials that URL and trusts those two keys (Go: tunnel.code.check).
//
// It answers and never throws. It is asked about a code an admin pasted in,
// beside a version the shell verified, and the answer decides whether a code is
// shown at all; an exception out of it would be an unhandled rejection in the
// page where a refusal belongs.
export function checkCode(c, version) {
  try {
    // Every field of c is read once, into a local, and the rules below decide
    // on those readings alone. c is normally the frozen object parsePairCode
    // answers with, whose fields cannot be replaced at all; a caller who builds
    // its own could otherwise pass the check with one URL and dial another.
    const { url, key, tls, anchor } = c;
    const list = version?.list;
    if (!list || anchor.seq !== list.seq || !equal(anchor.hash, version.hash)) return false;
    const nodes = list.nodes;
    if (!Array.isArray(nodes)) return false;
    // The guard in front of n.kind is what trust.validAPINode writes for the
    // same reason: typeof null is "object", and a field of null throws where a
    // refusal was wanted.
    return nodes.some((n) => n !== null && typeof n === "object" && n.kind === NODE_API
      && equal(n.noise, key) && n.url === url && equal(n.tls, tls));
  } catch {
    // A field behind a getter or a proxy trap that throws: no answer is no
    // match.
    return false;
  }
}

// components returns the NGC software a server with these roles runs, sorted:
// always the controller, plus the API for ROLE_API and the web interface for
// ROLE_WEB. ROLE_DB runs the migrations out of the ngc-api release file without
// installing it, ROLE_SERVICES runs app containers (Go: tunnel.Components).
export function components(roles) {
  // Go is handed a []string. Here roles is whatever the caller has, and this
  // answer decides what gets installed on a machine: something that is no list
  // names the controller and nothing else, rather than throwing.
  const list = Array.isArray(roles) ? roles : [];
  const c = [AUTO_CONTROLLER];
  if (list.includes(ROLE_API)) c.push(AUTO_API);
  if (list.includes(ROLE_WEB)) c.push(AUTO_WEB);
  // The three names are ASCII, where sorting by UTF-16 unit and Go's sorting by
  // byte are the same order.
  return c.sort();
}
