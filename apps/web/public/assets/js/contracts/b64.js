// Package b64: the only binary-to-text encoding of the NGC contracts is
// base64url without padding (RFC 4648 section 5), decoded strictly: every
// byte from the alphabet, no padding, and the unused bits at the end are 0,
// so every byte string has exactly one encoding (Go: enc.B64).
//
// Every function here takes bytes or text and nothing else: what a caller
// leaves out or hands over in the wrong form is refused, never guessed.
const ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
const REV = new Int8Array(128).fill(-1);
for (let i = 0; i < 64; i++) REV[ALPHABET.charCodeAt(i)] = i;

export class BadInput extends Error {}

// viewOf answers with a plain view of the bytes v holds. Go is handed a
// []byte, whose length is the slice's own and cannot change between two
// looks. Here v is an object the caller owns: a subclass of Uint8Array may
// define length, and then a check that reads it and a reader that reads it
// again see two different numbers. buffer, byteOffset and byteLength are
// therefore asked once, here, and the view built from them is the single
// reading that every check, every size and every copy afterwards uses.
//
// It stands in this module because this is the byte layer: everything the
// contracts are handed from outside is bytes, and every one of them passes
// here. wire.js had it first, for frames, and task.js asked wire.js for it;
// both now ask this file, so the tree holds one definition, as it does for
// MAX_PARAMS and SIG_SIZE. Each of them folds the BadInput into its own one
// error, because a caller of wire or task tells its errors apart by module
// and not by where inside the module the bytes were refused.
//
// What it closes, measured over the contracts as they stood before it moved:
// twenty places asked a caller's object for its size more than once. Eleven
// of them through json.decode - a document of 70027 bytes came out of
// sign.parseEnvelope through a budget of 2390 - six through concat below, one
// through b64encode below, and two through equal below.
//
// Asking once is not the property, though, and the round after that measured
// why: noise.Transport.seal asked exactly once and put 70000 bytes through a
// bound of 65519 all the same, because it asked the object and then handed
// that same object to the Web Crypto API, which takes the bytes out of the
// internal slot. The property is that the number checked against is the
// number worked with, and this function is what makes it hold: the view it
// answers with is a plain Uint8Array, so the bound, the indexes, the copy and
// the engine all read that one number. The guard "every place that takes
// caller bytes decides on the number it works with" holds it, and it holds it
// by reading which properties were touched - buffer, byteOffset and
// byteLength once each and length not at all is this function and nothing
// else - and not by counting.
export function viewOf(v, why) {
  if (!(v instanceof Uint8Array)) throw new BadInput(why);
  try {
    return new Uint8Array(v.buffer, v.byteOffset, v.byteLength);
  } catch {
    throw new BadInput(why);
  }
}

export function encodedLen(n) {
  return Math.floor((n * 8 + 5) / 6);
}

export function b64encode(bytes) {
  // The caller's object is asked for its size once, and the loop below reads
  // the view's length instead - once per three bytes, which is where this
  // used to go wrong: a caller's length that answered the first read with 0
  // and the truth afterwards turned 32 real bytes into the empty string,
  // measured, and a check on that text would have held for a key nobody
  // encoded.
  const u8 = viewOf(bytes, "base64url: not bytes");
  let out = "";
  let i = 0;
  for (; i + 3 <= u8.length; i += 3) {
    const v = (u8[i] << 16) | (u8[i + 1] << 8) | u8[i + 2];
    out += ALPHABET[(v >> 18) & 63] + ALPHABET[(v >> 12) & 63] + ALPHABET[(v >> 6) & 63] + ALPHABET[v & 63];
  }
  const rest = u8.length - i;
  if (rest === 1) {
    out += ALPHABET[u8[i] >> 2] + ALPHABET[(u8[i] << 4) & 63];
  } else if (rest === 2) {
    const v = (u8[i] << 8) | u8[i + 1];
    out += ALPHABET[v >> 10] + ALPHABET[(v >> 4) & 63] + ALPHABET[(v << 2) & 63];
  }
  return out;
}

export function b64decode(s) {
  if (typeof s !== "string") throw new BadInput("base64url: not a string");
  const n = s.length;
  if (n % 4 === 1) throw new BadInput("base64url: length");
  // The alphabet is checked before anything is allocated, so a long string
  // that is no base64url costs nothing but the look (Go: enc.B64 asks
  // outsideAlphabet before it makes the output).
  for (let i = 0; i < n; i++) {
    const c = s.charCodeAt(i);
    if (c >= 128 || REV[c] < 0) throw new BadInput("base64url: character");
  }
  const out = new Uint8Array(Math.floor((n * 6) / 8));
  let acc = 0, bits = 0, o = 0;
  for (let i = 0; i < n; i++) {
    acc = (acc << 6) | REV[s.charCodeAt(i)];
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      out[o++] = (acc >> bits) & 0xff;
    }
  }
  // The bits left over belong to no byte; a decoder that ignored them would
  // give one byte string two spellings.
  if (bits > 0 && (acc & ((1 << bits) - 1)) !== 0) throw new BadInput("base64url: trailing bits");
  return out;
}

const ENCODER = new TextEncoder();

export function utf8(s) {
  if (typeof s !== "string") throw new BadInput("utf8: not a string");
  return ENCODER.encode(s);
}

export function fromUtf8(u8) {
  if (!(u8 instanceof Uint8Array)) throw new BadInput("fromUtf8: not bytes");
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(u8);
  } catch {
    // The module answers with its own error: a TypeError from the engine
    // would walk past every caller that watches for BadInput.
    throw new BadInput("fromUtf8: not UTF-8");
  }
}

export function concat(...parts) {
  // Each part is asked for its size once and only the view goes on. The old
  // body read every part's length twice, once to size the output and once to
  // step the cursor, and the copy in between takes the bytes the array really
  // holds. Measured: a part of 32 bytes whose second reading answered 0 wrote
  // the next part over its first byte - concat(a, b) came back as 33 bytes
  // with b at offset 0 - and sign.message signed that. A first reading of 0
  // instead made the output too small and out.set threw a RangeError out of a
  // function that promises BadInput.
  const views = parts.map((p) => viewOf(p, "concat: not bytes"));
  let n = 0;
  for (const v of views) n += v.length;
  const out = new Uint8Array(n);
  let o = 0;
  for (const v of views) {
    out.set(v, o);
    o += v.length;
  }
  return out;
}

// equal compares two byte strings in a time that depends on their length
// alone, for signatures and message codes.
export function equal(a, b) {
  // Only bytes are compared. A string has a length and an index just as
  // bytes do, but "a" ^ "b" is 0, so a comparison that took strings would
  // answer true for any two of the same length - quietly, and exactly where
  // it is asked whether a signature is right.
  //
  // Each side is asked for its size once, through viewOf, and the loop runs
  // over that view. The old body read a.length twice, once against b.length
  // and once as the bound of the loop: a first reading that matched b and a
  // second of 0 left the loop body unrun, diff at 0 and the answer true -
  // measured, true for two 32-byte arrays that differ in every byte, which is
  // exactly the answer a MAC comparison may never give. A caller who hands
  // over something that is no bytes gets false as before, so this function
  // keeps its promise to answer and never throw.
  let x, y;
  try {
    x = viewOf(a, "equal");
    y = viewOf(b, "equal");
  } catch {
    return false;
  }
  if (x.length !== y.length) return false;
  let diff = 0;
  for (let i = 0; i < x.length; i++) diff |= x[i] ^ y[i];
  return diff === 0;
}
