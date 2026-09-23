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

export function encodedLen(n) {
  return Math.floor((n * 8 + 5) / 6);
}

export function b64encode(u8) {
  if (!(u8 instanceof Uint8Array)) throw new BadInput("base64url: not bytes");
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
  let n = 0;
  for (const p of parts) {
    if (!(p instanceof Uint8Array)) throw new BadInput("concat: not bytes");
    n += p.length;
  }
  const out = new Uint8Array(n);
  let o = 0;
  for (const p of parts) {
    out.set(p, o);
    o += p.length;
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
  if (!(a instanceof Uint8Array) || !(b instanceof Uint8Array)) return false;
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a[i] ^ b[i];
  return diff === 0;
}
