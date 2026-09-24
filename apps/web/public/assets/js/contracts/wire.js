// Frames and channels, the unit of transfer between browser, API and
// controller (Go: wire). Every binary WebSocket message of a session link is
// one Noise transport message whose plaintext is exactly one frame; channel 0
// carries control messages, every other channel one call or one stream.
//
// Go is handed a Frame of three typed fields, an Open of eight, a Close of
// four, and a uint32 or a bool wherever one stands. Here every one of them
// comes out of an object or an argument the caller owns, so each is read
// exactly once and only the copy is used afterwards: a field that answers the
// check with one value and the writer with another would put a frame on the
// link that no check ever saw. viewOf below is that rule for bytes.
//
// The imports differ from the task brief's own block in nine names. Four are
// added: fromUtf8 and BadInput from b64.js and BadJSON from json.js, which the
// encoders and the folding of the decoder's errors need, and optInt from
// json.js, the shape this task adds there for the exit code. Five are left
// out: A, bool and int from json.js, which nothing here uses (the brief's own
// code does not either), and sign.validateEnvelope, on purpose - see
// validTaskEnvelope.
import { concat, utf8, fromUtf8, b64encode, BadInput } from "./b64.js";
import { decode, Budget, BadJSON, S, P, str, uint32, bytes, raw, optInt, RAW } from "./json.js";
import { validName, validAppID, coreApp, validText } from "./text.js";
import { proofShape, validateProof } from "./proof.js";
import { envelopeShape } from "./sign.js";

export const TYPE_OPEN = 1, TYPE_DATA = 2, TYPE_EOF = 3, TYPE_CLOSE = 4, TYPE_GRANT = 5;
export const HEADER_SIZE = 5;
export const MAX_FRAME = 65519;
export const MAX_PAYLOAD = MAX_FRAME - HEADER_SIZE;
export const CONTROL_CHANNEL = 0;
export const DEFAULT_WINDOW = 256 << 10;
export const MIN_WINDOW = 4 << 10;
export const MAX_WINDOW = 1 << 20;
export const MAX_SESSION_CHANNELS = 64;
export const MAX_OPEN_PAYLOAD = 40 << 10;
export const MAX_BROWSER_OPEN = 16 << 10;
export const MAX_CLOSE_PAYLOAD = 4 << 10;
export const MAX_PARAMS = 8 << 10;
export const MAX_MESSAGE_TEXT = 256;
export const HANDSHAKE_SIZE = 48;
export const MAX_E2E_PLAINTEXT = MAX_PAYLOAD - 16;
export const MAX_CALL_RESULT = 8 << 20;
export const E2E_FIRST_TIMEOUT = 30;
export const E2E_DATA = 1, E2E_END = 2;

// The payload of the task envelope in an OPEN: Go's task.MaxTask, which wire
// holds as maxTask of its own because wire cannot import task. The same
// layering decides here: task.js is built on session.js and proof.js, while
// this module is the frame layer under them, so an import from here into
// task.js would point the layers backwards. The number also stands in task.js
// as MAX_TASK, which is where a task itself is read.
const MAX_TASK = 16 << 10;

// An Ed25519 signature (Go: ed25519.SignatureSize). sign.js holds the same
// number for itself and does not export it; the one place that needs it here
// is validTaskEnvelope below.
const SIG_SIZE = 64;

// The largest channel number, and the largest credit: both are uint32 in Go,
// where the compiler answers for them; here a check has to.
const MAX_UINT32 = 4294967295;

// Everything a document or a caller gets wrong is this one error, as it is
// one error per parser in Go (ErrFrame, ErrOpen, ErrClose, ErrControl,
// ErrE2E): which rule was broken is nothing a caller has to tell apart, and
// no message here repeats a single byte of the input - the JSON decoder's own
// errors quote member names and parts of what stood there.
export class BadFrame extends Error {}

const EMPTY = new Uint8Array(0);

// viewOf answers with a plain view of the bytes v holds. Go is handed a
// []byte, whose length is the slice's own and cannot change between two
// looks. Here v is an object the caller owns: a subclass of Uint8Array may
// define length, and then a check that reads it and a writer that reads it
// again see two different numbers. buffer, byteOffset and byteLength are
// therefore asked once, here, and the view built from them is the single
// reading that every check, every size and every copy below uses. Without it
// a payload that says 0 for the check and 65515 for the copy leaves as a
// frame of 65520 bytes - over MaxFrame, which the other side drops and the
// link ends on. The test "a payload whose length lies is refused, not half
// written" builds exactly that payload.
function viewOf(v, why) {
  if (!(v instanceof Uint8Array)) throw new BadFrame(why);
  try {
    return new Uint8Array(v.buffer, v.byteOffset, v.byteLength);
  } catch {
    throw new BadFrame(why);
  }
}

// bytesOrNull is viewOf for a member that may be absent: null for nothing
// (the decoder gives a missing raw member as null, and a caller may leave a
// field out), undefined for something that is no bytes at all. It answers
// instead of throwing, for the validate functions, which answer true or false
// and never throw.
function bytesOrNull(v) {
  if (v === null || v === undefined) return null;
  if (!(v instanceof Uint8Array)) return undefined;
  try {
    return new Uint8Array(v.buffer, v.byteOffset, v.byteLength);
  } catch {
    return undefined;
  }
}

// checkFrame holds the rules of Go's Frame.check, in its order: the payload
// first, then what the type allows.
function checkFrame(type, channel, len) {
  if (len > MAX_PAYLOAD) throw new BadFrame("payload");
  switch (type) {
    case TYPE_DATA: return;
    case TYPE_OPEN:
      if (channel === CONTROL_CHANNEL || len === 0 || len > MAX_OPEN_PAYLOAD) throw new BadFrame("open");
      return;
    case TYPE_EOF:
      if (channel === CONTROL_CHANNEL || len !== 0) throw new BadFrame("eof");
      return;
    case TYPE_CLOSE:
      if (channel === CONTROL_CHANNEL || len === 0 || len > MAX_CLOSE_PAYLOAD) throw new BadFrame("close");
      return;
    case TYPE_GRANT:
      if (channel === CONTROL_CHANNEL || (len !== 4 && len !== 4 + HANDSHAKE_SIZE)) throw new BadFrame("grant");
      return;
  }
  throw new BadFrame("type");
}

// frameOf reads the three fields of a frame the caller owns exactly once and
// answers with a frame of its own. The type and the channel are checked as
// the types Go has for them. The channel has to be: DataView.setUint32 takes
// anything and writes a number for it - measured, "1" and 1.5 both become the
// channel 1, -1 becomes 4294967295 and "x" becomes 0, and the frame would go
// out on a channel nobody named. The type stands beside it so both fields are
// answered for in one place; checkFrame's switch compares strictly and turns
// a type of "2" down as well.
function frameOf(f) {
  if (f === null || typeof f !== "object") throw new BadFrame("no frame");
  const { type, channel, payload } = f;
  if (!Number.isInteger(type)) throw new BadFrame("type");
  if (!Number.isInteger(channel) || channel < 0 || channel > MAX_UINT32) throw new BadFrame("channel");
  return { type, channel, payload: viewOf(payload, "payload is not bytes") };
}

export function encodeFrame(f) {
  const { type, channel, payload } = frameOf(f);
  checkFrame(type, channel, payload.length);
  const b = new Uint8Array(HEADER_SIZE + payload.length);
  b[0] = type;
  new DataView(b.buffer).setUint32(1, channel);
  b.set(payload, HEADER_SIZE);
  return b;
}

// decodeFrame parses one frame and checks the rules for its type. Its payload
// is a view into b, as Go's Decode aliases the buffer it was handed, and is
// only valid as long as b is.
export function decodeFrame(b) {
  const v = viewOf(b, "size");
  if (v.length < HEADER_SIZE || v.length > MAX_FRAME) throw new BadFrame("size");
  const type = v[0];
  const channel = new DataView(v.buffer, v.byteOffset, v.byteLength).getUint32(1);
  const payload = v.subarray(HEADER_SIZE);
  checkFrame(type, channel, payload.length);
  return { type, channel, payload };
}

// grant builds the payload of a GRANT frame: credit, then nothing or the
// HandshakeSize-byte Noise message 2. Go's Grant writes any uint32, and
// parseGrant is the one that refuses 0 and more than MAX_WINDOW; measured
// against shared/wire, Grant(0) builds four bytes that ParseGrant turns down,
// and the two stay apart here as well.
export function grant(credit, handshake) {
  if (!Number.isInteger(credit) || credit < 0 || credit > MAX_UINT32) throw new BadFrame("credit");
  const h = handshake === null || handshake === undefined
    ? EMPTY : viewOf(handshake, "handshake is not bytes");
  const b = new Uint8Array(4 + h.length);
  new DataView(b.buffer).setUint32(0, credit);
  b.set(h, 4);
  return b;
}

export function parseGrant(p) {
  const v = viewOf(p, "grant");
  if (v.length !== 4 && v.length !== 4 + HANDSHAKE_SIZE) throw new BadFrame("grant");
  const credit = new DataView(v.buffer, v.byteOffset, v.byteLength).getUint32(0);
  if (credit === 0 || credit > MAX_WINDOW) throw new BadFrame("credit");
  return { credit, handshake: v.length === 4 ? null : v.subarray(4) };
}

// The shell writes an OPEN itself (encodeOpen) and reads none from outside;
// parseOpen is how it holds what it writes against the rules Go reads it by,
// and how the contract test holds this module against wire.json.
//
// The task and the confirmation take their members from sign.envelopeShape
// and proof.proofShape rather than naming them a second time: a member listed
// twice is a member that can drift. Both are pointer structs here, so a
// member that is not there is null and not an object of zero values, which is
// what Go's *sign.Envelope and *proof.Proof are.
export const openShape = S({
  app: str, service: str, action: str, params: raw, window: uint32, e2e: bytes,
  task: P({ ...envelopeShape.m }), confirm: P({ ...proofShape.m }),
});

// openBudget bounds an OPEN while it is decoded. It has no array; its params
// stay raw, byte for byte as noisep.E2EPrologue hashes them, and the whole
// OPEN is small enough to bound what decoding them costs.
const openBudget = new Budget({ size: MAX_BROWSER_OPEN, items: { params: RAW } });

export function parseOpen(p) {
  let o;
  try {
    o = decode(p, openBudget, openShape);
  } catch (e) {
    // What the decoder found is nothing a caller has to tell apart, and its
    // text quotes the input. Only the errors of the contracts are folded in;
    // one of the engine stays as it is and shows up as the mistake it would be.
    if (e instanceof BadJSON || e instanceof BadInput) throw new BadFrame("open payload");
    throw e;
  }
  if (!validateOpen(o)) throw new BadFrame("open payload");
  return o;
}

export function validateOpen(o) {
  if (o === null || typeof o !== "object") return false;
  // The whole body stands inside the guard, as sign.validateEnvelope's does:
  // o is an object a caller owns, and a field behind a getter or a proxy that
  // throws would carry its exception straight through whoever wrote
  // `if (validateOpen(...))`. An exception is no answer, and a check that has
  // none refuses.
  try {
    // Every field is read once here and only the copies are used below.
    const { app, service, action, window, task, confirm } = o;
    const params = bytesOrNull(o.params);
    const e2e = bytesOrNull(o.e2e);
    if (params === undefined || e2e === undefined) return false;
    if (!validAppID(app) || !validName(service) || !validName(action)) return false;
    if (!validParams(params)) return false;
    if (!Number.isInteger(window) || window < MIN_WINDOW || window > MAX_WINDOW) return false;
    // Core apps run in the API, inside the session's own Noise session; every
    // other app gets Noise message 1 of its end-to-end handshake.
    const handshake = e2e === null ? 0 : e2e.length;
    if (coreApp(app)) {
      if (handshake !== 0) return false;
    } else if (handshake !== HANDSHAKE_SIZE) return false;
    // With a task, only the signed params count, so the OPEN carries none.
    if (task !== null && task !== undefined) {
      if (params !== null && params.length !== 0) return false;
      if (!validTaskEnvelope(task)) return false;
    }
    if (confirm !== null && confirm !== undefined) {
      if (task === null || task === undefined || !validateProof(confirm)) return false;
    }
    return true;
  } catch {
    return false;
  }
}

// validTaskEnvelope holds the two lengths Go's Open.validate asks of the task
// envelope: a payload of 1 to MAX_TASK bytes and a 64-byte signature.
//
// It does not ask what the key id looks like, and that is the one place where
// this file departs from the task brief, which called sign.validateEnvelope
// here. Measured against shared/wire: ParseOpen takes a task whose kid is "",
// "short", 26 characters or 22 characters outside the base64url alphabet, and
// leaves the id to the step that looks the key up (sign.verify,
// task.verifyApproval). validateEnvelope refuses all four, one step earlier
// than the frozen contract does. The shell reads no OPEN from outside, so a
// task with a wrong id reaching this check would come from its own task.js -
// and there a shell that turns its own OPEN down cannot send it at all, while
// an id that is wrong is answered for where the signature is checked anyway.
// Matching the frozen contract is therefore the narrower risk of the two.
function validTaskEnvelope(env) {
  if (env === null || typeof env !== "object") return false;
  try {
    // Behind a getter or a proxy a read may throw, and an exception is no
    // answer for a function a caller uses inside an if.
    const payload = bytesOrNull(env.payload);
    const sig = bytesOrNull(env.sig);
    if (payload === null || payload === undefined) return false;
    if (payload.length === 0 || payload.length > MAX_TASK) return false;
    return sig !== null && sig !== undefined && sig.length === SIG_SIZE;
  } catch {
    return false;
  }
}

// validParams reports whether the params are empty or one JSON object of at
// most MAX_PARAMS bytes (Go: wire.paramsShape, the check ParseOpen uses).
// Whether what stands inside them is valid JSON at all is the strict
// decoder's answer, not this one's: it has already read every byte of them,
// checked the UTF-8 and refused a repeated name at any depth, and scanning
// the largest params a second time would double what they cost. A raw value
// the decoder keeps begins at the value itself - measured against shared/wire,
// Go's json.RawMessage keeps no white space around it either - so the first
// byte is the whole question of shape.
export function validParams(rawValue) {
  const p = bytesOrNull(rawValue);
  if (p === undefined) return false;
  if (p === null || p.length === 0) return true;
  return p.length <= MAX_PARAMS && p[0] === 0x7b;
}

// encodeOpen writes the OPEN in the field order of Go's wire.Open. These exact
// bytes travel to the controller unchanged, and the end-to-end prologue hashes
// the params and task members from them, so the task and the confirmation are
// handed over as the bytes they are to stand as (taskBytes, confirmBytes),
// while parseOpen reads them back as the objects task and confirm.
//
// What it hands back is held against parseOpen before it leaves. The rules
// are then written once, in validateOpen, instead of a second time here, and
// params a caller built by hand get the full scan of the decoder that
// validParams leaves to it. An OPEN is at most MAX_BROWSER_OPEN bytes and a
// channel opens once, so the second read costs nothing that matters.
export function encodeOpen(o) {
  if (o === null || typeof o !== "object") throw new BadFrame("open");
  const { app, service, action, window } = o;
  const params = bytesOrNull(o.params);
  const e2e = bytesOrNull(o.e2e);
  const taskBytes = bytesOrNull(o.taskBytes);
  const confirmBytes = bytesOrNull(o.confirmBytes);
  if (params === undefined || e2e === undefined || taskBytes === undefined || confirmBytes === undefined) {
    throw new BadFrame("open");
  }
  if (!Number.isInteger(window) || window < 0 || window > MAX_UINT32) throw new BadFrame("open window");
  let s = '{"app":' + jsonText(app) + ',"service":' + jsonText(service) + ',"action":' + jsonText(action);
  if (params !== null && params.length > 0) s += ',"params":' + rawText(params);
  s += ',"window":' + String(window);
  if (e2e !== null && e2e.length > 0) s += ',"e2e":"' + b64encode(e2e) + '"';
  if (taskBytes !== null && taskBytes.length > 0) s += ',"task":' + rawText(taskBytes);
  if (confirmBytes !== null && confirmBytes.length > 0) s += ',"confirm":' + rawText(confirmBytes);
  const b = utf8(s + "}");
  parseOpen(b);
  return b;
}

// jsonText writes one string as JSON. Go is handed a string; here a field a
// caller left out is undefined, and "…" + undefined would put the word
// undefined into the document instead of refusing it.
function jsonText(v) {
  if (typeof v !== "string") throw new BadFrame("text");
  return JSON.stringify(v);
}

// rawText reads bytes that are to stand in the document as they are. Invalid
// UTF-8 is refused here rather than turned into replacement characters, which
// is what a TextDecoder without fatal would quietly write.
function rawText(v) {
  try {
    return fromUtf8(v);
  } catch (e) {
    if (e instanceof BadInput) throw new BadFrame("raw member");
    throw e;
  }
}

export const CODES = Object.freeze({
  __proto__: null,
  FORBIDDEN: "E_FORBIDDEN", UNREACHABLE: "E_UNREACHABLE", EXIT: "E_EXIT", TIMEOUT: "E_TIMEOUT",
  LIMIT: "E_LIMIT", PROTOCOL: "E_PROTOCOL", CONFIRM: "E_CONFIRM", LICENSE: "E_LICENSE",
  NOTFOUND: "E_NOTFOUND", VERIFY: "E_VERIFY", INTERNAL: "E_INTERNAL", TOOLARGE: "E_TOOLARGE",
  LOGIN: "E_LOGIN", LOCKED: "E_LOCKED",
});

// The messages are a Map and not an object. Go's messages is a map[Code]string
// and holds these fourteen names and nothing else; an object holds everything
// Object.prototype holds as well, and both places that ask it would answer
// wrongly: message("constructor") would give back a function instead of "",
// and the check of a CLOSE below, which asks whether the code is in the table
// at all, would take {"status":"error","code":"toString"} - a CLOSE Go
// refuses, measured. A Map answers only for a key that was put into it.
const MESSAGES = new Map([
  ["E_FORBIDDEN", "Keine Berechtigung."],
  ["E_UNREACHABLE", "Dienst nicht erreichbar."],
  ["E_EXIT", "Dienst mit Fehler beendet."],
  ["E_TIMEOUT", "Zeitlimit überschritten."],
  ["E_LIMIT", "Zu viele offene Verbindungen."],
  ["E_PROTOCOL", "Ungültige Nachricht."],
  ["E_CONFIRM", "Bitte erneut bestätigen."],
  ["E_LICENSE", "Keine gültige Lizenz."],
  ["E_NOTFOUND", "Nicht gefunden."],
  ["E_VERIFY", "Unterschrift ungültig."],
  ["E_INTERNAL", "Interner Fehler."],
  ["E_TOOLARGE", "Zu viele Daten."],
  ["E_LOGIN", "Anmeldung fehlgeschlagen."],
  ["E_LOCKED", "Zu viele Fehlversuche. Bitte später erneut versuchen."],
]);

// message returns the German text shown to users, and "" for a code that is
// none (Go: Code.Message, which reads a map and gives the zero string).
export const message = (code) => MESSAGES.get(code) ?? "";

// closeShape carries the exit code as optInt: a number that may be missing,
// Go's *int. With plain int a CLOSE without an exit code and one with the exit
// code 0 would decode to the same 0.
const closeShape = S({ status: str, code: str, message: str, exitCode: optInt });
const closeBudget = new Budget({ size: MAX_CLOSE_PAYLOAD });

export function parseClose(p) {
  let c;
  try {
    c = decode(p, closeBudget, closeShape);
  } catch (e) {
    if (e instanceof BadJSON || e instanceof BadInput) throw new BadFrame("close");
    throw e;
  }
  if (c.status === "ok") {
    if (c.code !== "") throw new BadFrame("close");
  } else if (c.status === "error") {
    if (!MESSAGES.has(c.code)) throw new BadFrame("close");
  } else {
    throw new BadFrame("close");
  }
  // The shell shows the message to people, so it is text for people like any
  // name or title, which also bounds its length.
  if (c.message !== "" && !validText(c.message, MAX_MESSAGE_TEXT)) throw new BadFrame("close");
  if (c.exitCode !== null && (c.exitCode < 0 || c.exitCode > 255)) throw new BadFrame("close");
  return c;
}

// errorClose builds a CLOSE for code with its German message (Go: wire.Error).
// A code that is none gives a CLOSE with an empty message, which parseClose
// then refuses - as Go's Error does, whose Message() is "" just the same.
export const errorClose = (code) => ({ status: "error", code, message: message(code), exitCode: null });

// encodeClose writes a CLOSE payload and holds it against parseClose before
// it leaves, for the same reason encodeOpen does: the rules stand once, and a
// CLOSE the other side would drop never goes out.
export function encodeClose(c) {
  if (c === null || typeof c !== "object") throw new BadFrame("close");
  const { status, code, message: text, exitCode } = c;
  const o = { status };
  if (code) o.code = code;
  if (text) o.message = text;
  if (exitCode !== null && exitCode !== undefined) o.exitCode = exitCode;
  let b;
  try {
    b = utf8(JSON.stringify(o));
  } catch (e) {
    // JSON.stringify throws over a BigInt and over a structure that points at
    // itself; both are a caller's mistake, not an answer this module gives.
    if (e instanceof TypeError) throw new BadFrame("close");
    throw e;
  }
  parseClose(b);
  return b;
}

// e2e builds one end-to-end plaintext. For E2E_END the body is empty (shell)
// or a valid CLOSE payload (controller); Go's E2E(E2EEnd, nil) builds the
// empty one, so null and a missing argument mean the same here.
export function e2e(kind, body) {
  const b = bytesOrNull(body);
  if (b === undefined) throw new BadFrame("e2e body");
  const data = b === null ? EMPTY : b;
  if (kind === E2E_DATA) {
    if (data.length > MAX_E2E_PLAINTEXT - 1) throw new BadFrame("e2e data");
  } else if (kind === E2E_END) {
    if (data.length !== 0) parseClose(data);
  } else {
    throw new BadFrame("e2e kind");
  }
  return concat(new Uint8Array([kind]), data);
}

// parseE2E checks one end-to-end plaintext; fromController says who sent it.
// That flag is the whole question of whether a body is allowed, so it has to
// be the bool Go is handed: "false" and 0 are truthy and falsy by accident,
// and either accident decides here which side's rules a message is read by.
export function parseE2E(plain, fromController) {
  if (typeof fromController !== "boolean") throw new BadFrame("e2e side");
  const v = viewOf(plain, "e2e");
  if (v.length === 0 || v.length > MAX_E2E_PLAINTEXT) throw new BadFrame("e2e");
  const kind = v[0];
  const body = v.subarray(1);
  if (kind === E2E_DATA) return { kind, data: body, status: null };
  if (kind === E2E_END) {
    if (!fromController) {
      if (body.length !== 0) throw new BadFrame("e2e end");
      return { kind, data: null, status: null };
    }
    return { kind, data: null, status: parseClose(body) };
  }
  throw new BadFrame("e2e kind");
}

export const CTL_HELLO = "hello", CTL_TRUST = "trust", CTL_NOTIFY = "notify";
export const CTL_STATUS = "status", CTL_UPDATE = "update", CTL_BYE = "bye";
export const LINK_SESSION = "session", LINK_TUNNEL = "tunnel";

const CTL_TYPES = new Set([CTL_HELLO, CTL_TRUST, CTL_NOTIFY, CTL_STATUS, CTL_UPDATE, CTL_BYE]);

// controlAllowed reports whether a control message of type t may travel on
// link from this side (fromAPI: sent by the API). Anything else ends the link.
// A side that is not a bool is no side: Go is handed one, and here a truthy
// string would read as "sent by the API" and open the API's list of types to
// whoever asked.
export function controlAllowed(link, fromAPI, t) {
  if (typeof fromAPI !== "boolean") return false;
  if (link === LINK_SESSION && fromAPI) return t === CTL_TRUST || t === CTL_NOTIFY || t === CTL_BYE;
  if (link === LINK_SESSION) return t === CTL_BYE;
  if (link === LINK_TUNNEL && fromAPI) return t === CTL_HELLO || t === CTL_BYE;
  if (link === LINK_TUNNEL) {
    return t === CTL_HELLO || t === CTL_STATUS || t === CTL_UPDATE || t === CTL_NOTIFY || t === CTL_BYE;
  }
  return false;
}

const controlShape = S({ t: str, b: raw });
// The body stays raw for the parser of its type, bounded by the frame.
// Decoding a raw body costs up to about 20 times its size (json.Budget),
// about 1.4 MiB for a body of one frame.
const controlBudget = new Budget({ size: MAX_PAYLOAD, items: { b: RAW } });

export function parseControl(p) {
  let c;
  try {
    c = decode(p, controlBudget, controlShape);
  } catch (e) {
    if (e instanceof BadJSON || e instanceof BadInput) throw new BadFrame("control");
    throw e;
  }
  if (!CTL_TYPES.has(c.t)) throw new BadFrame("control");
  return c;
}

// encodeControl writes a control message and holds it against parseControl
// before it leaves. On a session link the shell may send bye and nothing else
// (controlAllowed), so what it writes here is a few bytes.
export function encodeControl(c) {
  if (c === null || typeof c !== "object") throw new BadFrame("control");
  const t = jsonText(c.t);
  const body = bytesOrNull(c.b);
  if (body === undefined) throw new BadFrame("control body");
  const b = utf8(body !== null && body.length > 0
    ? '{"t":' + t + ',"b":' + rawText(body) + "}"
    : '{"t":' + t + "}");
  parseControl(b);
  return b;
}

export { coreApp as CORE_APP };
