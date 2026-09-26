// Everything the shell speaks before the WebSocket: HTTPS with JSON, the
// seven paths of shared/README.md, "Vor dem WebSocket". The cookie is set and
// deleted by the API; the shell never touches it.
//
// Every answer of an API is an ApiError or the parsed thing that was asked
// for. A body that is no Uint8Array is neither: that is a mistake of the
// caller, not an answer, and it leaves as b64's own BadInput so it stays
// loud instead of arriving in the shell dressed as a protocol error.
//
// The imports differ from the task brief's own block in seven names, the
// same reading task 7 gave trust.js and task 8 gave session.js. Added:
// bytesOf, because the one decision here about a body is a decision about
// bytes; utf8, the house encoder, where the brief reached for a second
// TextEncoder; safeNumber, which holds Retry-After to its digits;
// validLoginName, the grammar the API holds that same field to; validHost
// and validHostPort, so no address leaves here unchecked. Left out:
// fromUtf8, which the brief imports and its own body never calls.
import { bytesOf, utf8 } from "../contracts/b64.js";
import { safeNumber } from "../contracts/json.js";
import { validLoginName } from "../contracts/text.js";
import { validHost, validHostPort } from "../contracts/url.js";
import * as session from "../contracts/session.js";
import * as wire from "../contracts/wire.js";

// ApiError is the one failure this module has. Its text is the German one of
// wire.message and never the "message" the answer brought along: the table is
// ours, the answer's text is not, and two places that say the same thing in
// different words are two places that drift apart.
export class ApiError extends Error {
  constructor(code, retryAfter = 0) {
    super(wire.message(code) || "Unbekannter Fehler.");
    this.name = "ApiError";
    this.code = code;
    this.retryAfter = retryAfter;
  }
}

// parse runs a contract's parser over an answer and folds every "no" into one
// E_PROTOCOL, so an answer that does not hold up reaches the shell as the
// same kind of failure as every other. The parser's own text stays here:
// json.js names the rule that broke and quotes nothing, but an answer of
// PathLoginFinish holds a certificate, and a text about it has no business in
// the shell's error path.
function parse(parser, raw) {
  try {
    return parser(raw);
  } catch {
    throw new ApiError(wire.CODES.PROTOCOL);
  }
}

// retrySeconds reads the Retry-After header of an error answer - only
// E_LOCKED carries one - in RFC 9110's delta-seconds form and in no other,
// because seconds are what the API sends.
// The digits decide, as they do everywhere in the contracts (json.safeNumber),
// and only then is there a number: Number("0x2a") is 42 and Number("4e2") is
// 400, and this header comes from outside. Anything else - a missing header,
// an HTTP-date, a sign, a number past 2^53-1 - is "no waiting time given",
// which is 0.
function retrySeconds(text) {
  if (typeof text !== "string" || !/^[0-9]+$/.test(text) || !safeNumber(text)) return 0;
  return Number(text);
}

// read takes the body of an answer and stops at limit while it is still
// arriving, so nothing bigger than the contract allows is ever held in one
// piece. A parser's budget cannot do that: it counts once the whole answer is
// in memory, which is one round trip too late.
//
// null means the body ran past limit. What that means is the caller's to
// decide: too much of an answer that was going to be read is E_TOOLARGE, too
// much where a CLOSE was due is simply no CLOSE.
async function read(res, limit) {
  // An answer without a body has no stream to read; some engines give null
  // here rather than an empty one.
  if (res.body === null) return new Uint8Array(0);
  const reader = res.body.getReader();
  const parts = [];
  let n = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      // The size is taken once and kept beside the bytes it belongs to. These
      // chunks come from the engine and not from a caller, so nothing can
      // define their length - but the number counted here is the number
      // copied below all the same, and set() would throw rather than write
      // past the end if it ever were not.
      const len = value.length;
      if (n + len > limit) return null;
      parts.push({ bytes: value, len });
      n += len;
    }
  } catch {
    // A connection that broke while the body was arriving is the same thing
    // as one that never answered.
    throw new ApiError(wire.CODES.UNREACHABLE);
  } finally {
    // Releases the connection when the loop left early; after a finished read
    // it does nothing.
    await reader.cancel().catch(() => {});
  }
  const out = new Uint8Array(n);
  let at = 0;
  for (const p of parts) {
    out.set(p.bytes, at);
    at += p.len;
  }
  return out;
}

// call speaks one of the paths. It answers with the body as bytes, or null
// for the 204 the contract gives where there is nothing to say.
async function call(path, method, body, maxBody, maxAnswer) {
  let payload;
  if (body !== null) {
    // bytesOf and never a look at body.length: what is checked has to be what
    // is sent. A caller's Uint8Array may define length, and a length that
    // answers 1 to the bound and hands fetch 66560 real bytes would put a
    // body on the wire that no bound here ever saw - fetch takes the bytes
    // out of the internal slot, not out of the property. The copy settles
    // them before the bound is compared, and the copy is what goes out.
    payload = bytesOf(body, "http: body is not bytes");
    if (payload.length > maxBody) throw new ApiError(wire.CODES.TOOLARGE);
  }
  let res;
  try {
    res = await fetch(path, {
      method,
      // Nothing before the WebSocket may come out of a cache: a login
      // challenge is good once, and a trust chain answered from yesterday
      // would hide a version.
      cache: "no-store",
      // The paths above are this origin's own, and this says so: the session
      // cookie can then reach nothing else, whatever a later caller passes.
      credentials: "same-origin",
      mode: "same-origin",
      // A redirect is never followed. One to another host would carry the
      // request somewhere nobody chose, and an answer from there is no
      // answer of the API.
      redirect: "error",
      headers: payload === undefined ? {} : { "Content-Type": "application/json" },
      body: payload,
    });
  } catch {
    throw new ApiError(wire.CODES.UNREACHABLE);
  }
  if (res.status === 204) return null;
  // An error answer carries a CLOSE and nothing longer; a good one carries
  // what its own contract bounds.
  const raw = await read(res, res.ok ? maxAnswer : wire.MAX_CLOSE_PAYLOAD);
  if (raw === null) throw new ApiError(res.ok ? wire.CODES.TOOLARGE : wire.CODES.UNREACHABLE);
  if (res.ok) return raw;
  // The error is recognised by the code in the CLOSE JSON, not by the status.
  // Answers Apache gives itself - 502 and 503 when no API replies or its
  // certificate check fails - carry no CLOSE JSON at all. The answer's own
  // Content-Type is not asked: the bytes are held against the contract, which
  // is the stronger of the two questions.
  let close;
  try {
    close = wire.parseClose(raw);
  } catch {
    throw new ApiError(wire.CODES.UNREACHABLE);
  }
  if (close.status !== "error") throw new ApiError(wire.CODES.UNREACHABLE);
  throw new ApiError(close.code, retrySeconds(res.headers.get("Retry-After")));
}

// needBytes is call for a path whose answer carries the thing that was asked
// for. A 204 there is no answer, and handing null to a parser would turn that
// into a decoder error instead of what it is.
async function needBytes(path, method, body, maxBody, maxAnswer) {
  const raw = await call(path, method, body, maxBody, maxAnswer);
  if (raw === null) throw new ApiError(wire.CODES.PROTOCOL);
  return raw;
}

// getSetupOffer asks for the offer while no admin exists. It hands back the
// bytes: only the caller has the one-time code that session.parseSetupOffer
// checks the MAC with, and the code never leaves that caller.
export function getSetupOffer() {
  return needBytes(session.PATH_SETUP, "GET", null, 0, session.MAX_SETUP_OFFER);
}

// postSetup sends version 1 of the trust list with its MAC. The answer is a
// 204; anything else that is not an error is read and dropped, because a
// setup that worked must not fail over a proxy that rewrote the status.
export async function postSetup(body) {
  await call(session.PATH_SETUP, "POST", body, session.MAX_REQUEST, wire.MAX_CLOSE_PAYLOAD);
}

// loginStart asks for the challenge. The API holds LoginStart.Name to
// trust.ValidLoginName before it looks anything up (session.ParseLoginStart),
// so a name that is none never becomes a request. The grammar is public and
// says nothing about who exists, so this tells no one more than the API's own
// E_PROTOCOL would - and the answer for a name nobody has looks exactly like
// the answer for a name somebody has, which is why nothing beyond the grammar
// may be decided here.
export async function loginStart(name) {
  if (!validLoginName(name)) throw new ApiError(wire.CODES.PROTOCOL);
  const body = utf8(JSON.stringify({ name }));
  return parse(session.parseLoginChallenge,
    await needBytes(session.PATH_LOGIN_START, "POST", body, session.MAX_REQUEST, session.MAX_REQUEST));
}

// loginFinish sends the certificate and the proofs and takes the API's own
// certificate back. body is the encoded session.LoginFinish; the password
// itself never leaves the browser, only a proof over the certificate does.
export async function loginFinish(body) {
  return parse(session.parseLoginResult,
    await needBytes(session.PATH_LOGIN_FINISH, "POST", body, session.MAX_REQUEST, session.MAX_REQUEST));
}

// logout ends the session. The API deletes the cookie; there is nothing here
// to clear, because the cookie is HttpOnly and this code never saw it.
export async function logout() {
  await call(session.PATH_LOGOUT, "POST", null, 0, wire.MAX_CLOSE_PAYLOAD);
}

// trustChain fetches the versions from "from" on, or null when there is
// nothing newer.
export async function trustChain(from) {
  // from is a decimal number of 1 to 2^53-1 without a leading zero, which is
  // all session.ParseFrom takes; the shell puts nothing else into the query.
  // A safe integer writes itself as plain digits, so no exponent can reach
  // the address either.
  if (!Number.isSafeInteger(from) || from < 1) throw new ApiError(wire.CODES.PROTOCOL);
  const raw = await call(session.PATH_TRUST + "?from=" + from, "GET", null, 0, session.MAX_CHAIN);
  return raw === null ? null : parse(session.parseTrustChain, raw);
}

// socketURL is the address of the session WebSocket: this page's own, as wss.
export function socketURL() {
  const u = new URL(session.PATH_SOCKET, location.href);
  // http: and https: are special schemes and so are ws: and wss:, so the
  // setter takes this. A page on http: - localhost while developing, since
  // main.js refuses anything else - is upgraded rather than followed.
  u.protocol = "wss:";
  // An address goes on only after the grammar of trust has seen it. This one
  // comes from the page's own location, which is as trustworthy as it gets,
  // but a host no part of NGC can spell would fail much later and much less
  // clearly than here.
  if (!validHost(u.host) && !validHostPort(u.host)) throw new ApiError(wire.CODES.PROTOCOL);
  return u.toString();
}
