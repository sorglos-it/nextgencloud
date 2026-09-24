// Signed tasks: the shell signs a task with the session key; controllers and
// the API verify the chain from the task over the client certificate to the
// user's key in the trust list (Go: task).
//
// Go is handed a Task whose ten fields have their types from the compiler,
// and an int64 wherever a point in time stands. Here every one of them comes
// out of an object the caller owns, so each is read exactly once and only the
// copy is used afterwards: a field that answers the check with one value and
// the writer with another would put something into a signature that no check
// ever saw.
//
// The imports differ from the task brief's own block in five names, all of
// them added: fromUtf8 and encodedLen from b64.js and BadJSON from json.js,
// which the single-read rule below needs; verify from sign.js and certHash
// from session.js, which verifyApproval needs - it stands in the brief's
// interface list with no line of code behind it, so it is built here after
// Go's task.VerifyApproval.
import { b64encode, utf8, fromUtf8, equal, encodedLen, BadInput } from "./b64.js";
import { decode, Budget, BadJSON, S, str, bytes, int, raw, RAW } from "./json.js";
import { validName, validAppID, validID } from "./text.js";
import * as proof from "./proof.js";
import { message, verify as verifyEnvelope, PURPOSE_TASK, PURPOSE_CONFIRM } from "./sign.js";
import { certHash } from "./session.js";
import { keyOwner } from "./trust.js";

export const ID_SIZE = 16;
export const MAX_LIFETIME = 300;
export const MAX_TASK = 16 << 10;
// What wire.MaxParams bounds in Go: the params of one task. The shell has no
// wire module, so the number stands here, with the one contract that reads it.
export const MAX_PARAMS = 8 << 10;

// A task's cert is a SHA-256 (Go: sha256.Size), and buildTask gives a task
// the lifetime the shell needs for one channel - well inside MAX_LIFETIME,
// which is what a verifier still accepts.
const CERT_SIZE = 32;
const LIFETIME = 120;

// How many base64url characters exactly ID_SIZE and CERT_SIZE bytes take.
// encodedLen grows with every further byte, so no other byte count gives
// either of these two lengths, and a text of ID_CHARS characters came from
// ID_SIZE bytes and from no other number of them.
const ID_CHARS = encodedLen(ID_SIZE);
const CERT_CHARS = encodedLen(CERT_SIZE);

export const taskShape = S({
  id: bytes, inst: str, cert: bytes, node: str, app: str, service: str, action: str,
  params: raw, iat: int, exp: int,
});

const taskBudget = new Budget({ size: MAX_TASK, items: { params: RAW } });

// paramsBudget measures the params on their own, the way Go's
// wire.ValidParams does: at most MAX_PARAMS bytes, and nothing inside them is
// counted or named - the rule and core read them, not this contract.
const paramsBudget = new Budget({ size: MAX_PARAMS, items: { "": RAW } });

// parseTask reads a task payload. Everything the decoder finds wrong is this
// one error, as it is one error in Go (task.ErrTask): which rule a document
// broke is nothing a caller has to tell apart, and an error of the engine
// stays as it is and shows up as the mistake it would be.
export function parseTask(payload) {
  try {
    return decode(payload, taskBudget, taskShape);
  } catch (e) {
    if (e instanceof BadJSON || e instanceof BadInput) throw new Error("task: malformed task");
    throw e;
  }
}

// validParams mirrors Go's wire.ValidParams over the text the params hold:
// nothing at all, or at most MAX_PARAMS bytes that are exactly one JSON
// object. decode gives back the bytes of that one value with the white space
// around it left off, which is what Go's jsontext.Value.Kind reads through as
// well - measured against Go: "  {}" is an object there and here, "[1,2]",
// "\"x\"" and "5" are none, and a repeated member name is no params at all.
//
// Go asks IsValid of a value its own decoder has already read; here the same
// decoder that read the task reads the params again, which is as strict on
// every point the decoder already decided and stricter on two the contract
// never lets through anyway: a null anywhere and more than MAX_DEPTH levels
// are refused by enc and by json.js before Validate is ever reached. So what
// comes out of buildTask is a task the verifier of either language takes.
function validParams(text) {
  if (text === "") return true;
  try {
    const v = decode(utf8(text), paramsBudget, raw);
    return v.length > 0 && v[0] === 0x7b;
  } catch {
    return false;
  }
}

// parts reads a task once and gives back the ten pieces encodeTask writes, or
// null for a task that is none. It is the one place that looks at a caller's
// task, so validateTask and encodeTask can never disagree about it.
//
// The byte fields are turned into their base64url here and held against
// ID_CHARS and CERT_CHARS, instead of being asked for a length and encoded
// again later. A Uint8Array may carry an own length property that answers two
// reads with two numbers and still passes instanceof, so a length checked
// here and read again by b64encode would let a check pass for one byte string
// while another went into the signature (measured in task 8 for
// session.encodeClientCert, which is the same shape). The same for the
// params: fromUtf8 hands back the text the bytes really hold, and both the
// check and the writer go by that text.
//
// The guard stands in front of the try, the way sign.validateEnvelope and
// session.validateClientCert have it: what is no object at all is refused
// without a try, and the try is there for the fields of a real object, which
// may be getters or proxy traps that throw on being read.
function parts(t) {
  if (t === null || typeof t !== "object") return null;
  try {
    const { id, inst, cert, node, app, service, action, params, iat, exp } = t;
    // b64encode and fromUtf8 refuse anything that is not bytes, so the type
    // is decided before any length is looked at.
    const idText = b64encode(id);
    const certText = b64encode(cert);
    const paramsText = params === null || params === undefined ? "" : fromUtf8(params);
    // In the order Go's Task.Validate has it.
    if (idText.length !== ID_CHARS || !validID(inst) || certText.length !== CERT_CHARS
      || (node !== "" && !validName(node)) || !validAppID(app)
      || !validName(service) || !validName(action) || !validParams(paramsText)) {
      return null;
    }
    // Go holds iat and exp as int64 and a decoder that fills one refuses a
    // string, a boolean and a fraction. The three comparisons below take all
    // of them - a string compares like the number it spells, true counts as
    // 1, a fraction is a number like any other - so both are held to
    // Number.isSafeInteger first. That bound is the API's, not the engine's:
    // enc.safeNumber in Go and safeNumber in json.js refuse every number
    // above 2^53-1, so a task carrying one arrives over no wire in either
    // language, although Go's Validate on its own would take it.
    if (!Number.isSafeInteger(iat) || !Number.isSafeInteger(exp)
      || iat <= 0 || exp <= iat || exp - iat > MAX_LIFETIME) {
      return null;
    }
    return { id: idText, inst, cert: certText, node, app, service, action, params: paramsText, iat, exp };
  } catch {
    return null;
  }
}

// validateTask checks the fields of t (Go: Task.Validate). It answers true or
// false and never throws.
export const validateTask = (t) => parts(t) !== null;

// encodeTask writes the task in the field order of Go's task.Task; these
// exact bytes are what the session key signs and what the end-to-end prologue
// hashes.
//
// It refuses a task it would not also call valid, for the reason
// session.encodeClientCert gives: Go's json.Marshal writes every field of the
// struct, while JSON.stringify leaves out a member whose value is undefined,
// so a task with a field nobody filled in would go unnoticed into the very
// bytes the session key signs. There is one way out for every failure, as
// trust.encodeList has one.
export function encodeTask(t) {
  const p = parts(t);
  if (p === null) throw new Error("task: cannot encode this task");
  const head = JSON.stringify({
    id: p.id, inst: p.inst, cert: p.cert, node: p.node,
    app: p.app, service: p.service, action: p.action,
  }).slice(0, -1);
  const tail = JSON.stringify({ iat: p.iat, exp: p.exp }).slice(1);
  // params stays raw: its bytes go in unchanged, as the prologue hashes them.
  // Go's Task has omitempty there and no other field has it, so params is the
  // one member that may be missing and node is written even when it is empty.
  const mid = p.params === "" ? "" : ',"params":' + p.params;
  return utf8(head + mid + "," + tail);
}

// buildTask makes the task for one channel. Its lifetime is short, so a task
// that leaks is useless soon; the verifier remembers its id until it expires.
export function buildTask({ inst, certHash: cert, node, app, service, action, params, now } = {}) {
  const id = crypto.getRandomValues(new Uint8Array(ID_SIZE));
  const t = {
    id, inst, cert, node: node || "", app, service, action,
    params: params || null, iat: now, exp: now + LIFETIME,
  };
  // A now that is no whole number makes an exp that is none either - text
  // even makes exp text, because + joins two strings - and parts refuses
  // both, so nothing is signed for a point in time nobody named.
  if (!validateTask(t)) throw new Error("task: malformed task");
  const bytes = encodeTask(t);
  if (bytes.length > MAX_TASK) throw new Error("task: too large");
  return { task: t, bytes };
}

export async function signTask(sessionSignKey, bytes) {
  const { privateKey, kid } = sessionSignKey ?? {};
  const sig = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, privateKey,
    message(PURPOSE_TASK, bytes)));
  return { payload: bytes, kid, sig };
}

// verifyApproval checks env the way Go's task.VerifyApproval does: signed
// with the session key of the client certificate cert, whose payload is
// client, bound to it and of the same installation - but not the time, and
// without a verified session. It is evidence of what a user approved once
// (Go: core.UIProof); nothing may run on it, and it remembers no task id.
// cert is what session.verifyApproval returned for client.
export async function verifyApproval(env, cert, client) {
  if (env === null || typeof env !== "object" || cert === null || typeof cert !== "object") {
    throw new Error("task: malformed task");
  }
  // The envelope and the certificate belong to the caller, so their fields
  // are read once here and only these copies go on. The payload above all:
  // the signature has to cover the very bytes the task is then decoded from,
  // and a second read would check a signature over one document and hand back
  // another - the hole task 8 found in session.verifyAPI.
  const { payload, kid, sig } = env;
  const { inst: certInst, sig: certSig } = cert;
  // Before any signature is checked, as Go has it: a payload beyond MAX_TASK
  // costs nothing but the look.
  if (!(payload instanceof Uint8Array) || payload.length > MAX_TASK) {
    throw new Error("task: malformed task");
  }
  // Go hands sign.Verify's own error on; verifyEnvelope answers true or false
  // and this one error says false.
  if (!(await verifyEnvelope({ payload, kid, sig }, certSig, PURPOSE_TASK))) {
    throw new Error("task: bad signature");
  }
  const t = parseTask(payload);
  if (!validateTask(t) || t.inst !== certInst || !equal(t.cert, await certHash(client))) {
    throw new Error("task: malformed task");
  }
  return t;
}

// verifyConfirm checks a re-confirmation of the task in env by a key of the
// user (Go: task.VerifyConfirm). It answers true or false and never throws,
// the way sign.verify and proof.verify do: a caller who had to catch an
// exception around a signature is a caller who lets one through the moment
// the engine surprises him.
export async function verifyConfirm(p, env, list, userID) {
  try {
    // The proof and the envelope are the caller's, so each field is read once
    // and the copies are what go on to keyOwner and proof.verify - the same
    // reason session.verifyApproval builds a copy of its proof. Read twice,
    // kid could name one key to the owner check and another to the signature
    // check. proof.verify reads kind, kid, sig, authData and clientData and
    // nothing else, so those five are what the copy carries.
    const { kind, kid, sig, authData, clientData } = p ?? {};
    const { payload } = env ?? {};
    const found = keyOwner(list, kid);
    // Go compares the owner KeyOwner returned; the key itself goes on as it
    // stands, because this function reads no field of it and proof.verify
    // reads each of them once.
    if (found === null || found.user.id !== userID) return false;
    return await proof.verify({ kind, kid, sig, authData, clientData }, found.key,
      list.rp, PURPOSE_CONFIRM, payload);
  } catch {
    return false;
  }
}
