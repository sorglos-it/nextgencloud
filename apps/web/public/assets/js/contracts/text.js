// Package text: the names, texts, paths and versions of the NGC contracts,
// exactly as in Go (wire.ValidText, wire.ValidName, wire.ValidAppID,
// wire.CoreApp, store.ValidPath, store.ValidVersion, store.Newer,
// store.SupportsMinNGC, trust.ValidID, trust.ValidLoginName, core.ValidLogID,
// session.ValidTOTP).
//
// Go is handed a string and an int at every one of these; here a caller can
// leave a value out or hand over something else entirely. Nothing is guessed
// then: a missing or wrong value is no name, no text and no version, so the
// answer is false. A missing limit above all, because "1 to nothing bytes" is
// no text - never every text.
import { b64decode, BadInput } from "./b64.js";

const APP_ID = /^[a-z0-9]+(\.[a-z0-9-]+)+$/;
const NAME = /^[a-z][a-z0-9-]{0,31}$/;
const LOG_ID = /^[0-9a-f]{32}$/;
const TOTP = /^[0-9]{6}$/;
const VERSION = /^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})$/;
const PATH = /^[A-Za-z0-9_-][A-Za-z0-9._-]*(\/[A-Za-z0-9_-][A-Za-z0-9._-]*)*$/;
// Letters, marks, numbers, punctuation, symbols and the ASCII space: what
// unicode.IsPrint accepts in Go. A code point this table does not know counts
// as not printable, so a browser with older tables refuses more, never less.
const PRINTABLE = /^[\p{L}\p{M}\p{N}\p{P}\p{S} ]$/u;

// The byte limits of Go count UTF-8; only the grammars below that admit
// nothing but ASCII may count characters instead, where the two are the same.
const MAX_APP_ID = 100;
const MAX_PATH = 200;
const MAX_LOGIN = 64;

// notACodePoint holds the code points no table will ever assign: half a
// surrogate pair, which is no character at all and cannot be written in
// UTF-8, and the 66 noncharacters. Naming them keeps a regular-expression
// engine that takes one of them for a symbol from letting it in.
function notACodePoint(c) {
  return (c >= 0xd800 && c <= 0xdfff) || (c >= 0xfdd0 && c <= 0xfdef) || (c & 0xfffe) === 0xfffe;
}

// byteLength counts the bytes s has in UTF-8, the length every limit of the
// contracts is written in. Without a string there is nothing to count, and an
// answer of 0 would be the one that opens every limit.
export function byteLength(s) {
  if (typeof s !== "string") throw new BadInput("text: not a string");
  let n = 0;
  for (const cp of s) {
    const c = cp.codePointAt(0);
    n += c < 0x80 ? 1 : c < 0x800 ? 2 : c < 0x10000 ? 3 : 4;
  }
  return n;
}

// validText reports whether s is 1 to max bytes of printable UTF-8, code
// point by code point (Go: wire.ValidText, store.ValidText).
export function validText(s, max) {
  // Number.isSafeInteger keeps a limit that is no whole number out: a max of
  // undefined, NaN or Infinity compares to false against every length and
  // would let every text through.
  if (typeof s !== "string" || !Number.isSafeInteger(max)) return false;
  // Every code point takes at least as many bytes in UTF-8 as it takes units
  // here, so a string of more units than max has more bytes than max as well.
  // Counting the bytes of a text that is far too long would be work someone
  // outside decides on.
  if (s === "" || s.length > max || byteLength(s) > max) return false;
  for (const cp of s) {
    if (notACodePoint(cp.codePointAt(0)) || !PRINTABLE.test(cp)) return false;
  }
  return true;
}

// validName reports whether s is the name of a service or an action.
export const validName = (s) => typeof s === "string" && NAME.test(s);

// validAppID reports whether s is the id of an app: reverse domain, at most
// 100 bytes.
export const validAppID = (s) => typeof s === "string" && s.length <= MAX_APP_ID && APP_ID.test(s);

// coreApp reports whether app belongs to NGC itself.
export const coreApp = (s) => typeof s === "string" && s.startsWith("ngc.");

// validLogID reports whether s is the id of a log entry: 32 lower-case hex.
export const validLogID = (s) => typeof s === "string" && LOG_ID.test(s);

// validVersion reports whether s is MAJOR.MINOR.PATCH without leading zeros.
export const validVersion = (s) => typeof s === "string" && VERSION.test(s);

// validPath reports whether p is a safe relative path inside a package: no
// leading slash, no "..", no hidden file, at most 200 bytes.
export const validPath = (p) => typeof p === "string" && p.length <= MAX_PATH && PATH.test(p);

// validTOTP reports whether s is a TOTP code: exactly six ASCII digits.
export const validTOTP = (s) => typeof s === "string" && TOTP.test(s);

// validLoginName allows lower-case letters, digits and . _ - (1 to 64, the
// first a letter or a digit).
export function validLoginName(s) {
  if (typeof s !== "string" || s.length === 0 || s.length > MAX_LOGIN) return false;
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    const ok = (c >= "a" && c <= "z") || (c >= "0" && c <= "9")
      || (i > 0 && (c === "." || c === "_" || c === "-"));
    if (!ok) return false;
  }
  return true;
}

// validID reports whether s is an id: 16 bytes in base64url, 22 characters.
// b64decode is the strict one, so the four bits at the end that belong to no
// byte have to be zero - an id has one spelling.
export function validID(s) {
  if (typeof s !== "string" || s.length !== 22) return false;
  try {
    return b64decode(s).length === 16;
  } catch {
    return false;
  }
}

function parseVersion(s) {
  if (!validVersion(s)) return null;
  return s.split(".").map(Number);
}

// newer reports whether version a is higher than version b. A version that is
// no version is never newer, on either side.
export function newer(a, b) {
  const x = parseVersion(a), y = parseVersion(b);
  if (!x || !y) return false;
  for (let i = 0; i < 3; i++) {
    if (x[i] !== y[i]) return x[i] > y[i];
  }
  return false;
}

// supportsMinNGC reports whether an NGC of version ngc can take a package
// that asks for minNGC.
export function supportsMinNGC(ngc, minNGC) {
  // Go is handed two strings here. A minNgc that was left out is not an empty
  // minNgc: it is a question nobody asked, and the answer to it is no.
  if (typeof ngc !== "string" || typeof minNGC !== "string") return false;
  if (minNGC === "") return true;
  // A build that calls itself 0.0.0-dev is no release version and must fail
  // every minNgc, not pass it: newer is false on both sides for it, so both
  // versions are asked for here.
  return validVersion(ngc) && validVersion(minNGC) && !newer(minNGC, ngc);
}
