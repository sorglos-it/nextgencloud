// Package sign: envelopes for documents that NGC components sign with
// Ed25519 keys - API nodes, stores and the release key. A signature always
// covers message(purpose, payload), so a signature made for one purpose never
// verifies for another (Go: sign).
//
// Go is handed an Envelope whose three fields have their types from the
// compiler, and an int wherever a limit stands. Here a caller can leave a
// field out or hand over something else entirely, and none of it is guessed:
// a limit that is no whole number is no limit, and an envelope that is not
// built of bytes and text is no envelope. Neither opens a gate.
//
// The signature itself is checked by the Web Crypto API of the browser and
// nowhere else. An engine whose Web Crypto has no Ed25519 - the WebKit that
// Playwright brings is one - can tell no right signature from a wrong one, so
// it refuses every one of them.
import { b64encode, b64decode, concat, utf8, equal, encodedLen, viewOf, bytesOf, BadInput } from "./b64.js";
import { decode, Budget, BadJSON, S, str, bytes } from "./json.js";

// Purposes. Nothing here is derived from another: each one is the text Go
// holds, letter for letter.
export const PURPOSE_CLIENT_CERT = "clientcert/v1";
export const PURPOSE_API_CERT = "apicert/v1";
export const PURPOSE_TRUST_LIST = "trustlist/v1";
export const PURPOSE_TASK = "task/v1";
export const PURPOSE_CONFIRM = "confirm/v1";
export const PURPOSE_CATALOG = "catalog/v1";
export const PURPOSE_LICENSE = "license/v1";
export const PURPOSE_RELEASE = "release/v1";

// The largest payload an envelope may carry (Go: sign.MaxPayload).
export const MAX_PAYLOAD = 4 << 20;

// A key id is the first 16 bytes of a SHA-256, and its 22 characters are what
// that many bytes take in base64url. The character count is counted out from
// the byte count, because two places with the same number are two places that
// can drift apart.
const KEY_ID_SIZE = 16;
const KEY_ID_CHARS = encodedLen(KEY_ID_SIZE);
// Go: ed25519.PublicKeySize and ed25519.SignatureSize. The signature size is
// exported because wire.js needs it too, for the task envelope an OPEN
// carries: a second 64 in that file would be a second place to change.
const KEY_SIZE = 32;
export const SIG_SIZE = 64;
// What the JSON of an envelope holds besides the base64url of its payload:
// member names, key id, signature and white space (Go: envelopeSlack).
const ENVELOPE_SLACK = 1 << 10;

// Everything that is wrong with a document from outside is this one error, as
// it is one error in Go (sign.ErrEnvelope): which rule a document broke is
// nothing a caller has to tell apart, and an error of the engine would walk
// past everyone who watches for this one.
export class BadEnvelope extends Error {}

export const envelopeShape = S({ payload: bytes, kid: str, sig: bytes });

// payloadLimit is the limit an envelope is measured against: at most
// MAX_PAYLOAD, as in Go, and 0 - which refuses everything - where a caller
// handed over no limit at all. Go returns ErrEnvelope below 1 and cannot be
// handed anything but an int; here undefined, NaN and Infinity compare to
// false against every length and would quietly take the bound away, so they
// are refused instead of clamped.
function payloadLimit(maxPayload) {
  if (!Number.isSafeInteger(maxPayload) || maxPayload < 1) return 0;
  return Math.min(maxPayload, MAX_PAYLOAD);
}

// envelopeBudget bounds an envelope from outside: an envelope holds no array,
// so only its size is counted (Go: sign.ParseEnvelope).
export function envelopeBudget(maxPayload) {
  const max = payloadLimit(maxPayload);
  if (max === 0) throw new BadEnvelope("sign: no payload limit");
  return new Budget({ size: encodedLen(max) + ENVELOPE_SLACK });
}

// message returns the bytes a signature covers: "ngc/" || purpose || 0x00 ||
// payload (Go: sign.Message).
export function message(purpose, payload) {
  // Go is handed a string. Here "ngc/" + purpose would make text out of
  // anything: a misspelled import is undefined, and the signature would cover
  // the purpose "ngc/undefined" - a purpose nobody meant and nobody checks.
  if (typeof purpose !== "string") throw new BadInput("sign: purpose is not text");
  return concat(utf8("ngc/" + purpose), new Uint8Array([0]), payload);
}

// keyIDAsync identifies a public key: base64url of the first 16 bytes of its
// SHA-256 (Go: sign.KeyID).
export async function keyIDAsync(pub) {
  // What is hashed is the view, not the caller's object. The reason that stood
  // here - "under node the digest asks the object for its size, in the browsers
  // it takes the bytes out of the internal slot" - is measured FALSE: over six
  // shapes in node 24, chromium 140 and webkit 26, crypto.subtle.digest takes
  // the INTERNAL bytes in all three. A length that answers half, double or
  // throws changes no hash in any of them, so there never were two ids for one
  // key and this line never closed that.
  //
  // What the three do differ in is which property the digest TOUCHES: node
  // reads byteLength - and throws where that getter throws - while chromium and
  // webkit read nothing at all. The view is what ends that difference here, and
  // that is the whole gain: it is what lets bytes.spec.js keep ONE list of
  // readings for every engine. The price is measured as well, and it is the
  // same in all three: once the view is built, a byteLength that lies decides
  // how many bytes are hashed - half the bytes, half the hash - where the raw
  // digest hashed them all.
  const h = new Uint8Array(await crypto.subtle.digest("SHA-256", viewOf(pub, "sign: key is not bytes")));
  return b64encode(h.subarray(0, KEY_ID_SIZE));
}

// keyID is the name the Go contract uses for the same function. There is no
// synchronous form to have: SHA-256 in the Web Crypto API answers with a
// promise, and a hash written out here would be cryptography built by hand.
// One function under both names, so no caller ever holds two ids for one key.
export { keyIDAsync as keyID };

// validateEnvelope checks the structure of env without its signature: a
// payload of 1 to maxPayload bytes, a key id as keyID makes it and a 64-byte
// signature (Go: sign.Envelope.Validate). parseEnvelope runs it, and so does
// every parser of a document that holds an envelope.
export function validateEnvelope(env, maxPayload) {
  const max = payloadLimit(maxPayload);
  if (max === 0 || env === null || typeof env !== "object") return false;
  // The reads stand inside the guard, not in front of it. Go is handed an
  // Envelope of three typed fields; here env is an object whoever calls owns,
  // and a field behind a getter, a proxy or a length that throws would carry
  // its exception straight through a caller who wrote `if (validateEnvelope(
  // ...))`. An exception is no answer, and a check that has none refuses.
  try {
    // The three fields are read once and only the copies are used below, so
    // no two checks here ever see different bytes. Further than this call it
    // does not carry: a caller reads env.payload again after a true. Nothing
    // hands over an envelope that answers a second read with something else -
    // the only source is parseEnvelope, and that builds a plain object - so
    // the rule keeps this function honest and is no lock on its callers.
    const { payload, kid, sig } = env;
    if (!(payload instanceof Uint8Array) || payload.length === 0 || payload.length > max) return false;
    if (typeof kid !== "string" || kid.length !== KEY_ID_CHARS) return false;
    if (!(sig instanceof Uint8Array) || sig.length !== SIG_SIZE) return false;
    // b64decode refuses every character outside the alphabet and every bit at
    // the end that is not 0, so only the length is left to check.
    return b64decode(kid).length === KEY_ID_SIZE;
  } catch {
    return false;
  }
}

// parseEnvelope reads an envelope from outside - a license an admin uploads, a
// catalog or a release fetched from a store: strict JSON within the budget,
// then validateEnvelope. It checks no signature; verify does (Go:
// sign.ParseEnvelope).
export function parseEnvelope(b, maxPayload) {
  let env;
  try {
    env = decode(b, envelopeBudget(maxPayload), envelopeShape);
  } catch (e) {
    // What the decoder found is nothing a caller has to tell apart. Only the
    // errors of the contracts are folded in; one of the engine stays as it is
    // and shows up as the mistake it would be.
    if (e instanceof BadJSON || e instanceof BadInput) throw new BadEnvelope("sign: malformed envelope");
    throw e;
  }
  if (!validateEnvelope(env, maxPayload)) throw new BadEnvelope("sign: malformed envelope");
  return env;
}

// verifyRaw checks sig over msg with the raw public key pubRaw.
export async function verifyRaw(pubRaw, sig, msg) {
  // A check of a signature answers false where it cannot check, and never
  // throws: a caller who had to catch an exception around a signature is a
  // caller who lets one through the moment the engine surprises him (b64:
  // equal, for the same reason). An engine without Ed25519 lands in the catch
  // below and refuses every signature instead of accepting any. The three
  // byte strings are looked at inside the try for the same reason: what a
  // caller hands over may be a proxy, and then instanceof and length throw
  // where Go would have had a []byte and nothing to throw.
  try {
    // THE COPIES COME FIRST, AND THE CALLER IS NOT ASKED ANYTHING BEFORE THEM.
    //
    // Five rounds stood here and argued about ORDER: read the lengths first,
    // or build the views first. Each round fenced the shapes it had measured
    // and the next measurement found a new one, because a getter is code and
    // runs wherever the object is touched. The shapes, all measured:
    //
    //   umlegend       reading length makes byteLength shrink 33 -> 32
    //   werfend        the length getter throws
    //   umschreibend   the length getter rewrites the buffer the view is on
    //   wachsend       reading length makes byteLength grow 32 -> 33
    //
    // umlegend and wachsend are mirrors: whichever order refuses the one
    // accepts the other, which is why the argument never converged. And the
    // measurement that ended it found a shape no order can reach at all:
    //
    //   class T extends Uint8Array {
    //     get length() { queueMicrotask(() => raw.set(realSignature)); return 64; }
    //   }
    //
    //   every synchronous check saw   90 90 90 90   (rubbish)
    //   crypto.subtle.verify got      118 213 135 142  (the real signature)
    //   verifyRaw answered            true
    //
    // The buffer is rewritten after the last check, while the engine is
    // awaited. No arrangement of synchronous readings can see it, because at
    // every moment a reading could have looked, the bytes were honest rubbish.
    //
    // So the ordering argument is over, and what replaces it is not a better
    // order but a different object. b64.bytesOf copies the caller's bytes into
    // a buffer of ours. The caller holds no reference to it and can never
    // touch it again, whatever shape it has and whenever it tries. The engine
    // is handed the copies; every size is taken off the copies; nothing below
    // this line reads a byte the caller could still have changed.
    //
    // bytesOf reads no property of a real array: `new Uint8Array(v)` copies out
    // of the internal slots, which is where the truth lives and where no
    // getter can be installed - so a subclass that overrides all four accessors
    // is not consulted once, measured.
    //
    // One honest input answers differently than it did before the copy, and it
    // is named rather than fenced: bytes on a SharedArrayBuffer were false on
    // 8b6fa87 and are true from 03cbe8c on. The Web Crypto API refuses a view
    // onto shared memory; the copy is ordinary memory, so it verifies. The
    // bytes are honest and true is the right answer. proof.validateUserKey
    // carries the same flip from 4abaa15 on, not from this round: measured, a
    // passkey on shared memory is false on 03cbe8c and true from 4abaa15. With
    // THIS commit the flip reaches three more places - session.verifyAPI's
    // client certificate, trust.open and trust.advance, each refused by the
    // digest before and taken now, measured in node 24 - where the commit
    // message names proof.js alone.
    //
    // (A Proxy has no such slots: there
    // `new Uint8Array(v)` consults one thing only, its Symbol.iterator. A plain
    // Proxy over a typed array inherits that array's iterator, which throws at
    // the proxy receiver, so bytesOf answers BadInput - not through length or
    // an index, neither of which is read. A Proxy that carries its OWN iterator
    // is copied through it and may yield any bytes at all, measured [9,9] out of
    // a 4-byte target; it still ends up as one settled copy, and agrees below
    // refuses it because its count is not the caller's.) It also stands in for
    // the instanceof check that used to be here - what is no Uint8Array leaves
    // it as a BadInput and the catch below answers false, the same as before.
    const k = bytesOf(pubRaw, "sign: key is not bytes");
    const s = bytesOf(sig, "sign: signature is not bytes");
    const m = bytesOf(msg, "sign: message is not bytes");
    if (k.length !== KEY_SIZE || s.length !== SIG_SIZE) return false;
    // AND ONLY NOW the caller's own objects are asked for their numbers - and
    // now that sentence is true, which is the whole gain. The five rounds each
    // wrote some version of "after this point the caller can steer nothing",
    // and on a view it was never true: the view kept pointing at the caller's
    // buffer, so a getter running here still decided which bytes the engine
    // would see. On a copy it is true by construction. These readings can only
    // REFUSE. Whatever they run, whatever they rewrite, whatever they take
    // away - the bytes that will be verified were settled three lines up.
    //
    // Because they can only refuse, their order among themselves does not
    // matter for what can be FORGED, and that is measured and not assumed: the
    // eight shapes at the three positions answer the same with this line
    // reversed. Said exactly, because the round before said it too loosely and
    // W1 is what that cost: what the order cannot change is the set of inputs
    // that come back TRUE. It could still change which of two wrong inputs is
    // refused first, and hence which error a caller sees - here there is only
    // one answer, false, so nothing is left for it to change.
    //
    // They are here to keep an accepted assurance - "a check of a signature
    // answers false for an envelope that fights back". An object whose numbers
    // disagree with the bytes it handed over is refused: one that throws, one
    // that lies, one that threw its own buffer away between the copy and here.
    // byteLength is asked beside length because an object can lie in either,
    // and on a copy asking more is free of risk.
    //
    // An honest object always agrees with its copy, so nothing honest is
    // refused here: a plain array, and an honest subarray of a larger buffer,
    // pass with length and byteLength both equal to the copy - measured.
    //
    // tests/contracts/bytes.spec.js marks these three positions "copy": it
    // holds them to reading the caller's two NUMBERS - the line below is that
    // reading - and to never touching buffer or byteOffset, which are what a
    // view onto the caller's memory would be built from. sign.spec.js
    // "verifyRaw decides on a copy no getter can reach" measures all eight
    // shapes at all three positions, and "verify reads the key once" measures
    // the envelope.
    if (!agrees(pubRaw, k) || !agrees(sig, s) || !agrees(msg, m)) return false;
    const key = await crypto.subtle.importKey("raw", k, { name: "Ed25519" }, false, ["verify"]);
    return (await crypto.subtle.verify({ name: "Ed25519" }, key, s, m)) === true;
  } catch {
    return false;
  }
}

// agrees asks a caller's object whether it owns up to the bytes it handed
// over. It runs only after those bytes are copied out of reach, so it decides
// nothing about them and can do nothing but refuse. It stays module-private -
// it reads length on purpose, and bytes.spec walks every EXPORT and would
// rightly flag a byte-taking export that reads length; proof.js keeps its own
// copy of this one line for the same reason.
function agrees(v, copied) {
  return v.length === copied.length && v.byteLength === copied.length;
}

// verify checks env against pubRaw for purpose (Go: sign.Verify).
export async function verify(env, pubRaw, purpose) {
  if (env === null || typeof env !== "object") return false;
  // Everything else stands in one try, so that the promise at the top of the
  // file - answers false where it cannot check, never throws - holds for the
  // whole function and not call by call. Three things in here can throw: the
  // envelope itself, because its three fields come from an object the caller
  // owns and a getter or a proxy may throw on every read; the hash, because
  // outside a secure context there is no crypto.subtle at all; and the
  // message, because it refuses a purpose that is no text - and a purpose
  // that is no text is exactly what a misspelled constant hands over
  // (PURPOSE_CATALOGUE for PURPOSE_CATALOG is undefined). Here as everywhere:
  // what cannot be checked is refused, and never with an exception past a
  // caller who wrote `if (await verify(...))`.
  try {
    const { payload, kid, sig } = env;
    if (!(payload instanceof Uint8Array) || typeof kid !== "string" || !(sig instanceof Uint8Array)) return false;
    if (!(pubRaw instanceof Uint8Array)) return false;
    // In the order Go has it: the size, then the key the envelope names - an
    // envelope that names another key is refused before any signature is
    // checked - and only then the signature itself. validateEnvelope is not
    // run here, because Go does not run it either: the parser of every
    // document that holds an envelope has run it, and Verify of Go takes a
    // payload of no bytes (shared/sign: TestSignVerifyEmptyPayload).
    // The key and the payload are copied here, ONCE, and everything below
    // works on those copies. Without that this function reads each of them
    // twice from the caller's object - the key for its id and again for the
    // signature, the payload for its bound and again for the message - and two
    // readings of one hostile object are two different byte strings. That is
    // not a stricter kind of hygiene, it is a forgery: an object that answers
    // the first reading with a trusted key and the second with the attacker's
    // own passes `kid` against the trusted one and has the signature checked
    // against the other. Copying once is what makes the id and the signature
    // belong to the same key.
    const pub = bytesOf(pubRaw, "sign: key is not bytes");
    // The payload is bounded BEFORE it is copied, so a payload past the limit
    // is refused without ever being allocated. Measured on 64 MiB: refused in
    // 0.011 ms with no ArrayBuffer taken at all, against 15.8 ms and a full
    // 64 MiB when the copy came first (node 24; chromium 0 ms against 15.7 ms,
    // where a browser hands out no count of the buffers it holds and the time
    // carries the point on its own).
    //
    // FIXRUNDE 5, W1. The round before wrote "so the early read can only
    // refuse", and that sentence was measured FALSE. The bound was a SECOND
    // reading of the caller's count: it ate the first answer and agrees() took
    // the next one, so a length getter that answers 0 once and the truth
    // afterwards went from refused to ACCEPTED (03cbe8c false, 4abaa15 true),
    // and the same getter the other way round - truth first, lie after - went
    // from accepted to refused (true, then false). No forgery either way: what
    // is verified is the copy, under a real signature over those very bytes.
    // But the set of inputs this function refuses MOVED instead of only
    // growing, and not moving it was the whole reason the bound was allowed to
    // come forward. Measured in node 24 and in chromium, identical.
    //
    // So the caller's count is read ONCE, into count. That one number is what
    // the bound looks at AND what is tied to the copy two lines down, so the
    // bound really does check the number that is worked with, and the reading
    // count is back at the one 03cbe8c had. Measured after this change in both
    // engines: 1 reading; the lying getter DECIDED the same as on 03cbe8c both
    // ways round - lie first refused, truth first taken, which is the half
    // that says nothing formerly accepted is refused now either - and 64 MiB
    // still refused without a byte copied.
    //
    // 4abaa15 is on origin, so its commit message keeps the wrong sentence;
    // this is where the correction stands.
    //
    // AND WHY THIS IS NOT THE SHAPE the other three have. task.payloadCopy,
    // session.certCopy and trust.payloadCopy go view first, bound on the view,
    // copy out of the view. The round before gave bytes.spec.js as the reason
    // for the difference, and that reason does not hold: the three "copy"
    // positions that file pins are verifyRaw's, and sign.verify stands in no
    // list of that file at all. It has nothing to say about this line.
    //
    // Two view forms were measured against the one that stands here. WITHOUT an
    // agrees() on the payload the view form is simply wrong: a payload whose
    // length THROWS comes through, where sign.spec.js "a check of a signature
    // answers false for an envelope that fights back" holds it to false. WITH
    // agrees() kept it answers every test in the tree the same - the review of
    // this commit built it and ran the suite green - and it is still not taken,
    // for a reason no test in the tree asks about:
    //
    // viewOf believes buffer, byteOffset and byteLength - it must, that is
    // what a view is made of - and all three are the caller's getters, where
    // bytesOf reads no property at all. The other three functions
    // may believe them, because what they verify is what they HAND BACK: the
    // copy is parsed and returned, so the caller acts on the very bytes the
    // signature covered. verify hands nothing back. It answers true or false
    // about an envelope the caller keeps and reads again itself, so the bytes
    // it decides on have to be the bytes that object really holds - which is
    // bytesOf straight off it, out of the internal slots, where no getter can
    // be installed.
    //
    // Measured in node 24: a 64-byte payload whose only lie is `get buffer()`,
    // naming another document of the same size that the same key really signed.
    // With the view in front, verify answers TRUE over that other document
    // while the caller reading the object itself gets its own bytes; as it
    // stands here it answers false, and true only under the signature over the
    // bytes the object holds. Every number that object reports is honest, so
    // agrees() cannot see it: it compares counts, never bytes.
    const count = payload.length;
    if (count > MAX_PAYLOAD) return false;
    const load = bytesOf(payload, "sign: payload is not bytes");
    // Same as in verifyRaw, and for the same reason: after the copies these
    // can only refuse, so an object that fights back is still answered false.
    // agrees() is spelled out for the payload rather than called, because its
    // length reading is the one already taken above - calling it would put the
    // second reading straight back.
    if (!agrees(pubRaw, pub)) return false;
    if (count !== load.length || payload.byteLength !== load.length) return false;
    // This cannot fire any more: the line above holds count equal to
    // load.length and the bound above holds count at MAX_PAYLOAD. It stays as
    // the bound on the BYTES rather than on a number - a Proxy with an
    // iterator of its own hands bytesOf as many bytes as it likes, whatever
    // its count says - so the day the tie above is loosened, this is what
    // catches it.
    if (load.length > MAX_PAYLOAD) return false;
    if (kid !== (await keyIDAsync(pub))) return false;
    return await verifyRaw(pub, sig, message(purpose, load));
  } catch {
    return false;
  }
}

// equal belongs to b64 and is handed on from here, so that code which checks
// signatures needs one import for the whole job.
export { equal };
