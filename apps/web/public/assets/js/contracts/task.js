// Signed tasks: the shell signs a task with the session key; controllers and
// the API verify the chain from the task over the client certificate to the
// user's key in the trust list (Go: task).
//
// Go is handed a Task whose ten fields have their types from the compiler,
// and an int64 wherever a point in time stands. Here each of the ten comes out
// of an object the caller owns, so parts below reads each of them exactly once
// - measured with a counting getter on all ten, highest count 1 - and what it
// checks and what write puts into the signature come out of that one reading.
// A field that answered the check with one value and the writer with another
// would put something into a signature that no check ever saw.
//
// Reading a field once is not the same as holding still what the field points
// at, and that is where this file was open until this round. A Uint8Array
// subclass may define length: the field is read once, and the object behind it
// can still answer one reader differently from the next. Two entries handed
// such an object on to more than one reader - parseTask to decode, which keeps
// it and asks length again at every step of the parser, and verifyApproval to
// its own bound, to the signature check and to parseTask. Measured on
// chromium: a subclass answering 0 on readings 1 and 5 walked a 70027-byte
// task through a bound of 16384 and all the way out of verifyApproval,
// signature, form and binding included. Both now go through wire.viewOf, which
// asks buffer, byteOffset and byteLength once and builds the one view every
// reader below sees. The two tests named at payloadView build it.
//
// Every other place in this file that is handed a caller's bytes is a
// different case, and none of them is changed. Walked through, so the list
// above is not read as "and nowhere else":
//
//   - validateTask, encodeTask and buildTask all reach the caller's bytes
//     through parts, and parts does not hold the length of id, cert and params
//     still at all: it goes by what one walk over them produced. Why, and what
//     that does and does not promise, stands at parts.
//   - signTask has no check for a second reading to disagree with: it signs.
//     Measured against sign.message as it stood before this round, with a
//     length that answers 0 on the first read and the truth afterwards, it
//     ran into a RangeError out of concat and the promise was rejected; a
//     first read of 99 signed 99 bytes, of which the last 92 were zeros -
//     but that is the shape only while the real task is at most 99 bytes,
//     which a task never is. Measured again this round: 7 real bytes give
//     those 92 zeros, 99 give none, and 100 and 245 give the same RangeError
//     as the 0 case. Either way nothing came back that a verifier takes.
//     Since this round the question does not arise: concat asks each part for
//     its size once, so signTask signs the bytes the array holds and no
//     others.
//   - verifyConfirm reads env.payload once and hands it to proof.verify, which
//     is where that contract answers for it.
//   - verifyApproval's own client bytes go to certHash and nowhere else, one
//     read, and what comes back is this module's hash of whatever they held.
//
// The imports differ from the task brief's own block in nine names, counted by
// the two sets and not by hand; all nine are added and none of the brief's own
// is left out. fromUtf8 and encodedLen from b64.js are what the single-read
// rule needs, in parts below. BadInput and BadJSON are the two errors
// parseTask folds into the one error Go has there. verify from sign.js and
// certHash from session.js belong to verifyApproval, which stands in the
// brief's interface list with no line of code behind it and is built here
// after Go's task.VerifyApproval. MAX_PARAMS and validParams come from
// wire.js, where Go keeps both as well - the comment said six until the round
// before this one and had been one short since MAX_PARAMS arrived. viewOf is
// the ninth; it came from wire.js for one round and comes from b64.js since
// this one, which is why BadFrame is gone again and the count fell by one.
import { b64encode, utf8, fromUtf8, equal, encodedLen, viewOf, bytesOf, BadInput } from "./b64.js";
import { decode, Budget, BadJSON, S, str, bytes, int, raw, RAW } from "./json.js";
import { validName, validAppID, validID } from "./text.js";
import * as proof from "./proof.js";
import { message, verify as verifyEnvelope, PURPOSE_TASK, PURPOSE_CONFIRM } from "./sign.js";
import { certHash } from "./session.js";
import { keyOwner } from "./trust.js";
import { MAX_PARAMS, validParams as wireValidParams } from "./wire.js";

export const ID_SIZE = 16;
export const MAX_LIFETIME = 300;
export const MAX_TASK = 16 << 10;
// Go keeps this in wire.MaxParams, which bounds the params of a task and the
// params an OPEN carries itself. Since wire.js exists the number stands
// there, once; it is handed on from here so that a caller who reads tasks
// keeps reading one name. wire.js imports nothing from this file, so the two
// do not point at each other.
export { MAX_PARAMS };

// A task's cert is a SHA-256 (Go: sha256.Size), and buildTask gives a task
// the lifetime the shell needs for one channel, well under MAX_LIFETIME,
// which is the most a verifier still accepts.
const CERT_SIZE = 32;
const LIFETIME = 120;

// How many base64url characters ID_SIZE and CERT_SIZE bytes take. encodedLen
// grows with every further byte and so gives no two byte counts the same
// length: a base64url of ID_CHARS characters came out of ID_SIZE bytes and
// out of no other number of them. Measured from 0 to 8192 bytes: every step
// up, no two byte counts sharing a length, and b64encode's own output as many
// characters as encodedLen names.
const ID_CHARS = encodedLen(ID_SIZE);
const CERT_CHARS = encodedLen(CERT_SIZE);

export const taskShape = S({
  id: bytes, inst: str, cert: bytes, node: str, app: str, service: str, action: str,
  params: raw, iat: int, exp: int,
});

// taskBudget bounds a task while it is decoded: a task holds no array of its
// own, so only its size is counted, and its params stay raw bytes. What
// stands inside them is for the rule and the core; this contract reads them
// as one value and no further, in validParams below.
const taskBudget = new Budget({ size: MAX_TASK, items: { params: RAW } });

// payloadView is b64.viewOf under this file's one error: one view of the
// bytes a caller handed over, built from buffer, byteOffset and byteLength,
// which every reader of that payload then uses. b64.js holds the definition
// and this file asks for it, the way it asks wire.js for MAX_PARAMS and
// validParams - a second copy would be a second place to change. It stood in
// wire.js for one round and moved to the byte layer in this one, so the fold
// below is now of a BadInput and no longer of a BadFrame.
//
// Two tests build the case it closes: "parseTask measures a task by one
// reading of its length" and "verifyApproval decides on one reading of the
// payload and decodes those very bytes". The first also pins what it costs: an
// ArrayBuffer, which decode would have taken and this turns down. Go's decoder
// is handed a []byte and nothing else, wire.js took the same step one round
// earlier, and no caller that means to read a task hands one over: of the
// seven call sites of parseTask in this tree - two here and five in its test -
// six are handed bytes this file wrote or b64decode produced, and the seventh
// is the one line of the test that hands over a null, a string and that one
// ArrayBuffer, to hold this refusal down.
function payloadView(b) {
  try {
    return viewOf(b, "task: malformed task");
  } catch (e) {
    if (e instanceof BadInput) throw new Error("task: malformed task");
    throw e;
  }
}

// payloadCopy is payloadView's counterpart for the one place that must not
// hand a view on: a signature is checked over these bytes and the task is
// then decoded from them, across an await, so a VIEW is not enough. A view
// keeps pointing at the caller's buffer, and an ordinary Uint8Array rewritten
// while the engine is awaited therefore had the signature checked over one
// document and handed back ANOTHER - measured in node 24 and in chromium,
// signature over ctl-1/deactivate, ctl-2/reactivate returned, the second
// document refused on its own with "task: bad signature". That is
// sign.verify's and proof.verify's hole, in this file, and the repair is
// theirs: bytesOf copies the bytes into a buffer of ours, which the caller
// holds no reference to and can never touch again.
//
// The three steps, and why they are in this order:
//
//   1. The VIEW, which payloadView already built and every reader of a task
//      already decides on. Its own number is byteLength, not the caller's
//      length, so nothing here asks the caller for a count at all.
//   2. The bound, on the view's number, BEFORE the copy - a task past MAX_TASK
//      costs nothing but the look and is never allocated, as before.
//   3. The copy, made OUT OF THE VIEW. That is what makes the sentence above
//      true by construction rather than by a second check: the copy holds the
//      view's bytes, so the number the bound looked at is the number that is
//      worked with, and it takes no equality test to say so.
//
// What this deliberately does NOT do is refuse an object whose length
// disagrees with its view. That leniency is the tree's settled decision, with
// its own measurement - bytes.spec.js, "a length that disagrees with the view
// is ignored, not refused" - and it is Go's own behaviour for a slice whose
// capacity runs past its length. A first version of this fix tied the caller's
// count to the copy and refused the mismatch; it turned that test's boundLying
// case from taken to refused, and it was wrong to. Fixrunde 5's W1 is about
// the bound checking the number that is worked with, and step 3 gives that
// without touching the leniency.
function payloadCopy(b, max) {
  try {
    const view = viewOf(b, "task: malformed task");
    if (view.length > max) throw new BadInput("task: malformed task");
    return bytesOf(view, "task: malformed task");
  } catch (e) {
    if (e instanceof BadInput) throw new Error("task: malformed task");
    throw e;
  }
}

// parseTask reads a task payload. Everything the decoder finds wrong is this
// one error, as it is one error in Go (task.ErrTask): which rule a document
// broke is nothing a caller has to tell apart, and an error of the engine
// stays as it is and shows up as the mistake it would be.
//
// The payload is the caller's object, and decode keeps it: the budget is
// measured against one reading of length and the parser asks again at every
// step. Until this round the object went in as it stood, so a subclass whose
// first reading was 0 walked a task of any size past MAX_TASK - measured,
// 16385, 22063 and 70027 bytes, and measured against shared/task, Go refuses
// all three and takes the same document cut to 16384. The raw params came back
// as that subclass too, because slice keeps the class it was called on, and
// those are the bytes the end-to-end prologue hashes.
export function parseTask(payload) {
  const b = payloadView(payload);
  try {
    return decode(b, taskBudget, taskShape);
  } catch (e) {
    if (e instanceof BadJSON || e instanceof BadInput) throw new Error("task: malformed task");
    throw e;
  }
}

// validParams is wire.validParams over the text the params hold, as Go's
// task.Validate asks wire.ValidParams of its own: nothing at all, or at most
// MAX_PARAMS bytes that are exactly one JSON object. Until this round a
// paramsBudget of its own stood here as well, over the same MAX_PARAMS and
// with the same two items, and under it a body that asked the decoder the
// same question, its last line the same line - one question with two places
// to drift, where Go keeps it in one. The name is now defined once in the
// tree, in wire.js; grep over public/assets/js finds no second const.
//
// What that check costs against Go's IsValid is written out where it stands,
// in wire.js. One place is milder and belongs here, because it is measured
// against this file: the levels are counted from the params' own first byte
// here and from the task's first byte there, so params nested exactly
// MAX_DEPTH levels deep pass this check and are one level too deep once they
// sit inside a task. Measured level by level: one below MAX_DEPTH is read
// back, exactly MAX_DEPTH passes here, one above is refused here. It costs a
// refusal, never an acceptance - encodeTask writes the task at MAX_DEPTH and
// no verifier reads it back, while buildTask refuses it, because it reads its
// own document back before it hands anything over.
//
// The catch is this file's own, not wire's: an error of the engine is an
// answer of false here and travels on there. That difference is older than
// this round and is not touched by it.
function validParams(text) {
  try {
    return wireValidParams(utf8(text));
  } catch {
    return false;
  }
}

// parts reads a task once and gives back the ten pieces write puts into a
// document, or null for a task that is none. It is the one place that looks
// at a caller's task: validateTask is one call of it and nothing else,
// encodeTask writes what its own call checked, and buildTask writes what its
// own call checked and reads that document back. Measured with a counting
// getter on each of the ten fields: one validateTask and one encodeTask
// answer every one of them exactly once, highest count 1. verifyApproval is
// the one function here that reads a task twice over, and the task it reads
// is its own - see there.
//
// The byte fields are turned into their base64url here and held against
// ID_CHARS and CERT_CHARS, instead of being asked for a length and encoded
// again later. A Uint8Array may carry an own length property that answers
// every read with whatever it likes and still passes instanceof - measured,
// with the bytes still there behind it - so a length checked here and read
// again by b64encode would let a check pass for one byte string while another
// went into the signature (measured in task 8 for session.encodeClientCert,
// which is the same shape). b64encode asked for that length once per three
// bytes on its way through - measured, 7 reads for ID_SIZE and 12 for
// CERT_SIZE in one call - so a length was nothing this file could hold still,
// and the text that came out of the walk was the only thing both checks could
// share. Since this round b64encode asks b64.viewOf once and encodes out of
// that one view, which is the bytes the array really holds. The way the check
// is written here does not change: the text is still what both look at, and
// it costs nothing.
//
// What that moved, measured against this file before and after: an id of no
// real bytes whose length answers ID_SIZE was valid here and put the base64url
// of ID_SIZE zeros into the signature; it is refused now, because the view is
// of the empty buffer and its base64url is ID_CHARS characters short. And a
// Proxy over bytes with a get trap of its own, which passes instanceof, used
// to answer every index itself and could answer a different byte on each
// walk: two encodeTask calls over one such id wrote
// "YWFhYWFhYWFhYWFhYWFhYQ" and then "YmJiYmJiYmJiYmJiYmJiYg". Now only buffer,
// byteOffset and byteLength are asked of it, and the index trap never stands
// between the encoder and a byte. Measured, both ways such a proxy can go: a
// trap that forwards those three to the array itself gives the memory, and
// two calls write the same thing, twice "AAAAAAAAAAAAAAAAAAAAAA" over 16 zero
// bytes; a trap that forwards them with the proxy as the receiver cannot,
// because those accessors want the array, and parts answers null - which is
// the proxy the test builds. A Proxy with no get trap at all gets no further
// either way, and for the same reason - measured, a TypeError before and a
// BadInput now, both of which parts answers with null like any other
// refusal.
//
// What is still not promised here is a second call over an object whose
// buffer itself is written between the two. That such a promise is possible
// is not in doubt: built and measured, a WeakMap from the caller's array to
// its first walk's base64url makes the second call write what the first
// wrote. It is not built here, because it would have the module remember
// every array a caller ever handed it. Whoever has to hold a task and its
// bytes together makes both out of one call instead, which is what buildTask
// does below. Its id is out of reach for this, because it comes from
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
// false and never throws - measured against a getter that throws, a revoked
// Proxy, a Symbol.toPrimitive that throws, a Proxy whose get trap throws and
// a getter that runs itself out of stack: false, five times over.
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
  // but one, so the text carries the bytes through. Bytes that are no UTF-8
  // never become text at all - fromUtf8 decodes with fatal: true and throws
  // instead - so a stray byte inside the params is a refusal here and never a
  // quiet replacement character (measured, both in a string and behind the
  // object). The exception is a U+FEFF
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
// Reading the document back is not the only way to hold task and bytes
// together. The other one was built: assemble the task straight out of the
// same one walk - b64decode of the id and cert text, utf8 of the params text
// - and leave parseTask out. Measured over twelve cases inside the same call,
// it gives the same task as the read-back in nine, the walking cert, the
// lying length and the BOM among them. It differs in three, and all three are
// params handed in with white space around them: there the alternative hands
// back the caller's text with that white space, while every verifier reads
// the task without it.
//
// That is why the read-back is the one chosen, and not because there is
// nothing else: it checks the promise on the very document the caller gets,
// so what comes back is the task a verifier reads out of exactly these bytes
// - measured against shared/task, Go decodes all of it to the same id, cert
// and params. The white space is the one thing the task no longer carries:
// hand params in behind two spaces and encodeTask of the task that comes back
// is two bytes shorter than these bytes, because the document keeps them and
// no reader of it does. Go's json.Marshal drops them too.
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
  // This one cannot fire today, and it stays. Measured: the largest document
  // that gets this far is 8604 bytes against MAX_TASK 16384 - params at
  // MAX_PARAMS, an app of 100 characters, node, service and action of 32 each
  // and iat and exp at the largest whole number Number.isSafeInteger takes.
  // Take the line out and all eight tests stay green, so no mutation belongs
  // to it; what it is, is written here instead. The 7780 bytes of headroom
  // are not this file's to keep: every limit that makes them stands somewhere
  // else - MAX_PARAMS after Go's wire.MaxParams, the app id and name limits
  // in text.js - and the day one of them grows, this is the line that catches
  // the overflow before anything is signed.
  if (bytes.length > MAX_TASK) throw new Error("task: too large");
  // parseTask is what every verifier runs on these bytes, so it runs here
  // too. There is one document parts lets through and parseTask does not:
  // params nested exactly MAX_DEPTH levels deep, which are one level too deep
  // inside a task (see validParams). No verifier reads that document back
  // either - the refusal used to fall to the verifier and falls here now,
  // where the shell still has the task. Of 414 further params shapes that
  // parts accepts - member counts around MAX_MEMBERS, names around MAX_NAME,
  // long arrays and strings, the number bounds, white space - parseTask
  // refused none.
  return { task: parseTask(bytes), bytes };
}

// signTask puts the session key's signature over a task's bytes, the ones
// buildTask hands back or encodeTask writes. Go has none of this - only the
// shell signs tasks. It is no member of the -> boolean family and says no by
// throwing: an engine without Ed25519, a key that cannot sign, a
// sessionSignKey that is none and bytes that are none all end in a rejected
// promise, and nothing comes back that could pass for an envelope - measured,
// the last three of them.
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
  // The envelope and the certificate belong to the caller, so their fields are
  // read once here. The payload above all: the signature has to cover the very
  // bytes the task is then decoded from, and a second read of the field would
  // check a signature over one document and hand back another - the hole task 8
  // found in session.verifyAPI.
  const { payload: given, kid, sig } = env;
  const { inst: certInst, sig: certSig } = cert;
  // Reading the field once is not yet holding the bytes still, and holding
  // them still is what this line does. The round before built a VIEW here and
  // wrote that the signature covers "the very bytes the task is then decoded
  // from"; measured, it did not. A view keeps pointing at the caller's buffer,
  // so an ordinary Uint8Array rewritten while the engine was awaited had the
  // signature checked over document A and parseTask read document B out of the
  // same view - ctl-1/deactivate signed, ctl-2/reactivate returned, in node 24
  // and in chromium alike. payloadCopy takes the bytes out of the caller's
  // reach, bound and all, and everything below works on that copy: the
  // signature, the parse and the binding all see one document, and it is the
  // one that was signed.
  //
  // The older reading this line also closes stays closed: a subclass that
  // answers 0 on the first and the fifth reading of its length put a
  // 70027-byte task through the bound, the signature and the binding, and the
  // task came back. Go's VerifyApproval refuses those very bytes - what it
  // holds is a []byte, whose length the compiler answers for.
  const payload = payloadCopy(given, MAX_TASK);
  // Go hands sign.Verify's own error on; verifyEnvelope answers true or false
  // and this one error says false.
  if (!(await verifyEnvelope({ payload, kid, sig }, certSig, PURPOSE_TASK))) {
    throw new Error("task: bad signature");
  }
  // t is read more than once below - validateTask walks all ten fields, then
  // inst and cert are read again here, measured with counting getters: two
  // apiece. That is the one place in this file where a task is read twice,
  // and it is safe because the task is not the caller's: parseTask just built
  // it out of payload, so no field of it can answer two readings differently.
  // On a caller's task the same lines would be the very drift parts exists to
  // stop.
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
