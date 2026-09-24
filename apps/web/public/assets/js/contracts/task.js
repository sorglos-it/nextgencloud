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
// The imports differ from the task brief's own block in six names, all of
// them added and none of the brief's own left out. fromUtf8 and encodedLen
// from b64.js are what the single-read rule needs, in parts below. BadInput
// and BadJSON are the two errors parseTask folds into the one error Go has
// there. verify from sign.js and certHash from session.js belong to
// verifyApproval, which stands in the brief's interface list with no line of
// code behind it and is built here after Go's task.VerifyApproval.
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
// Go keeps this in wire.MaxParams, which bounds the params of a task and the
// params an OPEN carries itself. The shell has no wire module, so the number
// stands here, in the one contract that reads it today - the same reason
// session.httpStatus spells out Go's wire.Code as strings.
export const MAX_PARAMS = 8 << 10;

// A task's cert is a SHA-256 (Go: sha256.Size), and buildTask gives a task
// the lifetime the shell needs for one channel, well under MAX_LIFETIME,
// which is the most a verifier still accepts.
const CERT_SIZE = 32;
const LIFETIME = 120;

// How many base64url characters ID_SIZE and CERT_SIZE bytes take. encodedLen
// grows with every further byte and so gives no two byte counts the same
// length: a base64url of ID_CHARS characters came out of ID_SIZE bytes and
// out of no other number of them.
const ID_CHARS = encodedLen(ID_SIZE);
const CERT_CHARS = encodedLen(CERT_SIZE);

export const taskShape = S({
  id: bytes, inst: str, cert: bytes, node: str, app: str, service: str, action: str,
  params: raw, iat: int, exp: int,
});

// taskBudget bounds a task while it is decoded: a task holds no array of its
// own, so only its size is counted, and its params stay raw bytes, which the
// rule and core read and this contract does not.
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
// decoder that read the task reads the params again. On a task that came from
// parseTask that is the very decoder that just made these bytes, so it decides
// nothing anew. On a task the shell builds it is stricter than Go's IsValid on
// one point the wire never carries anyway: a null, which enc and json.js
// refuse everywhere, raw values included.
//
// One place is milder: the levels are counted from the params' own first byte
// here and from the task's first byte there, so params nested exactly
// MAX_DEPTH levels deep pass this check and are one level too deep once they
// sit inside a task. Measured against this file, level by level: one below
// MAX_DEPTH is read back, exactly MAX_DEPTH passes here, one above is refused
// here. It costs a refusal, never an acceptance - encodeTask writes the task
// at MAX_DEPTH and no verifier reads it back, while buildTask refuses it,
// because it reads its own document back before it hands anything over.
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
// task: validateTask is one call of it and nothing else, and encodeTask writes
// what its own call checked, never a second reading of the same task.
//
// The byte fields are turned into their base64url here and held against
// ID_CHARS and CERT_CHARS, instead of being asked for a length and encoded
// again later. A Uint8Array may carry an own length property that answers
// every read with whatever it likes and still passes instanceof - measured,
// with the bytes still there behind it - so a length checked here and read
// again by b64encode would let a check pass for one byte string while another
// went into the signature (measured in task 8 for session.encodeClientCert,
// which is the same shape). What ID_CHARS pins is therefore the number of
// elements that one walk read, not what the array holds. Measured: an id of
// no bytes whose length answers ID_SIZE is valid here, and the base64url of
// ID_SIZE zero bytes is what goes into the signature - a wrong id, but the
// same wrong one in the check and in the bytes of that one call.
//
// Across two calls nothing is promised, and nothing can be. A Proxy around a
// Uint8Array passes instanceof, answers every index itself and may answer a
// different byte on each walk; measured against this file, two encodeTask
// calls over one such id wrote "YWFhYWFhYWFhYWFhYWFhYQ" and then
// "YmJiYmJiYmJiYmJiYmJiYg". Whoever has to hold a task and its bytes together
// must therefore make both out of one call, which is what buildTask does
// below. Its id is out of reach for this, because it comes from
// crypto.getRandomValues into an array of its own, but its cert and its
// params are the caller's: it reads them in exactly this one walk, and what
// it hands back comes out of the document that walk produced.
//
// The params have no length read at all: fromUtf8 hands back the text the
// bytes really hold, whatever length says, and both the check and the writer
// go by that text. Measured against the two lines the brief had, which both
// decided on params.length: params that really hold an object, with a length
// that answers 0, are valid there too - and the signed task then carries no
// params at all, so the action runs with none.
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

// write puts one reading of a task into the bytes Go's json.Marshal writes
// for task.Task, field for field in its order; these exact bytes are what the
// session key signs and what the end-to-end prologue hashes. It takes the
// pieces and never a task, so that what is written is always what parts read
// and checked, never a second reading of the same object: encodeTask below is
// one reading and this, buildTask is one reading, this, and a reading of the
// document that came out.
function write(p) {
  const head = JSON.stringify({
    id: p.id, inst: p.inst, cert: p.cert, node: p.node,
    app: p.app, service: p.service, action: p.action,
  }).slice(0, -1);
  const tail = JSON.stringify({ iat: p.iat, exp: p.exp }).slice(1);
  // params stay raw: the bytes go in as the text they hold, because the
  // prologue hashes exactly these bytes. Measured over all 1112064 code
  // points UTF-8 has: utf8(fromUtf8(x)) gives x again for every one of them
  // but one, so the text carries the bytes through. The exception is a U+FEFF
  // at the very front, which TextDecoder drops as a byte order mark: params
  // that begin with one are written without it, and params that are nothing
  // but one are written as no params at all. Go refuses both - measured
  // against shared/task, wire.ValidParams says false for a leading BOM -
  // while this file takes them. Closing it needs either a second reading of
  // the caller's bytes, which is the very thing parts exists to avoid, or a
  // decoder of its own in b64.js. Reported, not closed here.
  //
  // Go's Task has omitempty on params and on no other field, so params is the
  // one member that may be missing and node is written even when it is empty.
  const mid = p.params === "" ? "" : ',"params":' + p.params;
  return utf8(head + mid + "," + tail);
}

// encodeTask writes the task t.
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
  return write(p);
}

// buildTask makes the task for one channel. Its lifetime is short, so a task
// that leaks is useless soon; the verifier remembers its id until it expires.
//
// inst, node, app, service, action and now are the caller's values and go
// into the task as they stand. certHash and params are the caller's bytes,
// and bytes are the one kind of value here that can answer two readings
// differently (see parts). So the caller's bytes are walked exactly once, by
// parts, and everything after that is this function's own: the document out
// of what that walk read, and the task that comes back out of the document.
// Nothing the caller handed over is part of the answer.
//
// Reading the document back is also the only way to promise what a caller
// needs here, which is that task and bytes say the same thing: what comes
// back is then the task every verifier reads out of exactly these bytes, down
// to the white space a raw params value may sit behind - measured against
// shared/task, Go decodes all of it to the same id, cert and params. That
// white space is the one thing the task no longer carries: hand params in
// behind two spaces and encodeTask of the task that comes back is two bytes
// shorter than these bytes, because the document keeps them and no reader of
// it does. Go's json.Marshal drops them too.
//
// Measured against the version before this one, which walked the caller's
// cert twice, once for validateTask and once for encodeTask: a cert that
// answered the first walk with 32 sevens and the second with 32 nines was
// checked as the one and signed as the other, and a 0-byte array whose length
// answers 32 bound the task to 32 zero bytes while the returned task.cert
// stayed empty. The price is one decode of the document per call: measured on
// V8, 0.0116 ms for the vector's params and 0.0387 ms for params at
// MAX_PARAMS, out of 0.0229 ms and 0.1003 ms for the whole call.
export function buildTask({ inst, certHash: cert, node, app, service, action, params, now } = {}) {
  // The id is this function's own array, filled in place, so nothing the
  // caller answers can reach it and nothing depends on what getRandomValues
  // gives back.
  const id = new Uint8Array(ID_SIZE);
  crypto.getRandomValues(id);
  // A now that is no whole number makes an exp that is none either - a now of
  // text makes exp text, because a + next to a string joins instead of adding
  // - and parts refuses both, so nothing is signed for a point in time nobody
  // named.
  const p = parts({
    id, inst, cert, node: node || "", app, service, action,
    params: params || null, iat: now, exp: now + LIFETIME,
  });
  if (p === null) throw new Error("task: malformed task");
  const bytes = write(p);
  if (bytes.length > MAX_TASK) throw new Error("task: too large");
  // parseTask is the one place that says what a document holds, so it says it
  // here too. It refuses a document parts lets through and a verifier does
  // not: params nested exactly MAX_DEPTH levels deep, which are one level too
  // deep inside a task (see validParams). That refusal used to fall to the
  // verifier; now it falls here, where the shell still has the task. Of 414
  // further params shapes that parts accepts - member counts around
  // MAX_MEMBERS, names around MAX_NAME, long arrays and strings, the number
  // bounds, white space - it refused none.
  return { task: parseTask(bytes), bytes };
}

// signTask puts the session key's signature over the bytes encodeTask made.
// Go has none of this - only the shell signs tasks. It is no member of the
// -> boolean family and says no by throwing: an engine without Ed25519, a key
// that cannot sign and a sessionSignKey that is none all end in a rejected
// promise, and nothing comes back that could pass for an envelope.
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
