// Package json reads JSON from outside exactly as Go does (enc.DecodeStrict
// with an enc.Budget): one value, no unknown or repeated member names at any
// depth, valid UTF-8, no null, integers only where the contract has them, no
// number beyond 2^53-1, at most MAX_DEPTH levels, arrays and maps only where
// the budget lists them. JSON.parse alone decides none of this.
//
// Go reads a document twice: a first pass checks it against the budget and
// knows only the path of each value, a second pass decodes it and knows only
// the Go type. Here one pass does both, and the two stay just as separate:
// the path alone says what is counted, the shape alone says what the value
// becomes. A budget that disagrees with a contract is a mistake in the
// contract, and the same documents come through as in Go.
import { b64decode, BadInput } from "./b64.js";

export const RAW = -1;
export const MAX_DEPTH = 32;
export const MAX_SAFE_INTEGER = 9007199254740991;
export const MAX_MEMBERS = 64;
export const MAX_NAME = 64;

// The most totals one budget may have (Go: enc.maxTotals).
const MAX_TOTALS = 8;

export class BadJSON extends Error {}

// Shapes. A shape says what a value may be; it mirrors the Go type the
// document decodes into. A member that is missing gives the zero value of its
// shape, as Go does, and the validate function of each contract asks for what
// has to be there.
//
// A shape is frozen where it is built, and a struct keeps its members as a
// copy of its own. Go has types, which nobody can add a field to at run time;
// here every contract holds its shapes in module variables that whoever runs
// on the page can reach, and one added member decides what a parser reads:
// envelopeShape.m.extra = str and sign.parseEnvelope takes envelopes the
// contract does not describe. The lock sits here, so no contract has to set
// it for itself and none can forget it.
export const str = Object.freeze({ k: "str" });
export const bool = Object.freeze({ k: "bool" });
export const int = Object.freeze({ k: "int" });
export const uint = Object.freeze({ k: "uint" });
export const uint32 = Object.freeze({ k: "uint32" });
export const uint64 = Object.freeze({ k: "uint" });
export const bytes = Object.freeze({ k: "bytes" });
export const raw = Object.freeze({ k: "raw" });
export const S = (members) => Object.freeze({ k: "struct", m: Object.freeze({ ...members }), ptr: false });
export const P = (members) => Object.freeze({ k: "struct", m: Object.freeze({ ...members }), ptr: true });
export const A = (of) => Object.freeze({ k: "array", of });
export const M = (of) => Object.freeze({ k: "map", of });

export class Budget {
  constructor({ size, items = {}, totals = {} }) {
    // A size that is missing is not a budget without a bound but a budget
    // that takes nothing: Go's Budget{} has Size 0 and refuses every
    // document. Everything a caller leaves out closes the gate.
    if (!Number.isInteger(size) || size < 0) throw new BadJSON("budget: size");
    if (items === null || typeof items !== "object") throw new BadJSON("budget: items");
    if (totals === null || typeof totals !== "object") throw new BadJSON("budget: totals");
    this.size = size;
    this.items = items;
    this.totals = totals;
  }
}

export function zero(shape) {
  switch (shape.k) {
    case "str": return "";
    case "bool": return false;
    case "int": case "uint": case "uint32": return 0;
    case "bytes": return new Uint8Array(0);
    case "raw": return null;
    case "array": return [];
    // A map takes its member names from the document, so it has no prototype
    // to reach: see Parser.object.
    case "map": return Object.create(null);
    case "struct": {
      if (shape.ptr) return null;
      const o = {};
      for (const [name, s] of Object.entries(shape.m)) o[name] = zero(s);
      return o;
    }
  }
  throw new BadJSON("unknown shape");
}

const WS = new Set([0x20, 0x09, 0x0d, 0x0a]);

// One decoder for the whole module. It is asked for the text of a slice and
// keeps nothing between calls, which is why one is enough.
const TEXT = new TextDecoder();

export function decode(input, budget, shape) {
  if (!(budget instanceof Budget)) throw new BadJSON("no budget");
  if (shape === null || typeof shape !== "object" || typeof shape.k !== "string") {
    throw new BadJSON("no shape");
  }
  let b;
  if (input instanceof Uint8Array) b = input;
  else if (input instanceof ArrayBuffer) b = new Uint8Array(input);
  else throw new BadJSON("no bytes");
  if (b.length > budget.size) throw new BadJSON("document too large");
  // One fatal decode proves the whole document is valid UTF-8, so the parser
  // below can walk the bytes and slice raw values without checking again.
  try {
    new TextDecoder("utf-8", { fatal: true }).decode(b);
  } catch {
    throw new BadJSON("not UTF-8");
  }
  const p = new Parser(b, budget);
  p.ws();
  const v = p.value(shape, "", 1, false);
  p.ws();
  if (p.i !== b.length) throw new BadJSON("trailing data");
  return v;
}

class Parser {
  constructor(b, budget) {
    this.b = b;
    this.i = 0;
    // Maps, not the objects themselves: a path is built from member names of
    // the document, and on an object a name like "__proto__" would answer
    // with something the budget never held.
    this.items = new Map(Object.entries(budget.items));
    this.totals = new Map();
    // Go tests the budget itself before it looks at a single byte, and a
    // budget that is wrong in itself refuses every document, whatever it
    // holds (enc.go, errLimit and maxTotals). A limit that is no whole
    // number cannot happen in Go, whose Items is a map[string]int; here it
    // is a wrong budget just the same.
    for (const limit of this.items.values()) {
      if (!Number.isInteger(limit) || (limit < 0 && limit !== RAW)) this.fail("budget");
    }
    const totals = Object.entries(budget.totals);
    if (totals.length > MAX_TOTALS) this.fail("budget");
    for (const [path, limit] of totals) {
      // A total for a path the budget does not list as an array or map, or
      // one that counts down, is a mistake in the contract.
      const items = this.items.get(path);
      if (items === undefined || items === RAW) this.fail("budget");
      if (!Number.isInteger(limit) || limit < 0) this.fail("budget");
      this.totals.set(path, { limit, n: 0 });
    }
  }

  // fail never repeats the input: an error says which rule was broken, and
  // nothing about what stood there.
  fail(why) {
    throw new BadJSON(why);
  }

  ws() {
    while (this.i < this.b.length && WS.has(this.b[this.i])) this.i++;
  }

  want(c) {
    if (this.i >= this.b.length || this.b[this.i] !== c) this.fail("expected " + String.fromCharCode(c));
    this.i++;
  }

  countTotal(path) {
    const t = this.totals.get(path);
    if (t === undefined) return;
    if (++t.n > t.limit) this.fail("total over its limit");
  }

  // value reads one value. Two things decide, and they never ask each other:
  // the path says what the budget counts, the shape says what is built. A
  // shape of null builds nothing - that is what is inside a raw value, where
  // the contract has no say but the rules that hold everywhere still do.
  value(shape, path, depth, inRaw) {
    if (this.i >= this.b.length) this.fail("no value");
    // Inside a value the budget marks RAW nothing is counted, no member name
    // is checked and an array needs no limit, all the way down.
    const raw = inRaw || this.items.get(path) === RAW;
    // A raw shape keeps the bytes as they stand and builds nothing, whatever
    // kind of value stands there: {"raw":9007199254740991.0} is a case of
    // json.json, and Go takes it.
    const keep = shape !== null && shape.k === "raw";
    const inner = keep ? null : shape;
    const start = this.i;
    const c = this.b[this.i];
    let v;
    if (c === 0x7b) v = this.object(inner, path, depth, raw);
    else if (c === 0x5b) v = this.array(inner, path, depth, raw);
    else if (c === 0x22) v = this.stringValue(inner);
    else if (c === 0x74 || c === 0x66) v = this.boolValue(inner);
    else if (c === 0x6e) this.fail("null");
    else if (c === 0x2d || (c >= 0x30 && c <= 0x39)) v = this.number(inner);
    else this.fail("unexpected byte");
    // A copy, not a view into the input: noisep.E2EPrologue hashes these
    // bytes and signatures cover them, so reusing the input buffer may not
    // change a value that was already checked (Go: json.RawMessage copies).
    return keep ? this.b.slice(start, this.i) : v;
  }

  object(shape, path, depth, inRaw) {
    if (depth > MAX_DEPTH) this.fail("too deep");
    // What the budget says about this path: a path it lists is counted like
    // a map and its members stand at "*"; a path it does not list is a
    // struct object with at most MAX_MEMBERS members whose names a Go field
    // could have. Inside a raw value neither holds.
    const limit = inRaw ? undefined : this.items.get(path);
    const counted = limit !== undefined;
    // What the shape says: a struct takes the members it knows, a map takes
    // every name the document brings.
    const struct = shape !== null && shape.k === "struct";
    const mapped = shape !== null && shape.k === "map";
    if (shape !== null && !struct && !mapped) this.fail("object where the shape has none");
    this.i++;
    // A map is built without a prototype: its names come from the document,
    // and "__proto__" has to be a member like any other instead of silently
    // replacing what the result is built on.
    const out = struct ? zero({ ...shape, ptr: false }) : mapped ? Object.create(null) : null;
    const seen = new Set();
    let n = 0;
    this.ws();
    if (this.b[this.i] === 0x7d) { this.i++; return out; }
    for (;;) {
      this.ws();
      const name = this.stringText();
      // Repeated names are refused at every depth, inside a raw value too
      // (Go: DecodeStrict, jsontext.ErrDuplicateName).
      if (seen.has(name)) this.fail("repeated member name");
      seen.add(name);
      if (counted) {
        if (++n > limit) this.fail("map over its limit");
        this.countTotal(path);
      } else if (!inRaw) {
        if (++n > MAX_MEMBERS) this.fail("too many members");
        if (nameBytes(name) > MAX_NAME || name.includes("/") || name === "*") this.fail("member name");
      }
      let sub = null;
      if (struct) {
        if (!Object.hasOwn(shape.m, name)) this.fail("unknown member");
        sub = shape.m[name];
      } else if (mapped) {
        sub = shape.of;
      }
      this.ws();
      this.want(0x3a);
      this.ws();
      const subPath = inRaw ? path : join(path, counted ? "*" : name);
      const v = this.value(sub, subPath, depth + 1, inRaw);
      if (out !== null) out[name] = v;
      this.ws();
      if (this.b[this.i] === 0x2c) { this.i++; continue; }
      this.want(0x7d);
      return out;
    }
  }

  array(shape, path, depth, inRaw) {
    if (depth > MAX_DEPTH) this.fail("too deep");
    // An array whose path the budget does not list is refused: nothing from
    // outside may grow without a limit. Inside a raw value nothing grows
    // into memory, so there is nothing to bound.
    const limit = inRaw ? undefined : this.items.get(path);
    if (!inRaw && limit === undefined) this.fail("array beyond its budget");
    const list = shape !== null && shape.k === "array";
    if (shape !== null && !list) this.fail("array where the shape has none");
    this.i++;
    const out = list ? [] : null;
    const sub = inRaw ? path : join(path, "*");
    let n = 0;
    this.ws();
    if (this.b[this.i] === 0x5d) { this.i++; return out; }
    for (;;) {
      this.ws();
      // Counted before it is read, so the first element too many is refused
      // unread.
      if (!inRaw) {
        if (++n > limit) this.fail("array over its limit");
        this.countTotal(path);
      }
      const v = this.value(list ? shape.of : null, sub, depth + 1, inRaw);
      if (out !== null) out.push(v);
      this.ws();
      if (this.b[this.i] === 0x2c) { this.i++; continue; }
      this.want(0x5d);
      return out;
    }
  }

  stringValue(shape) {
    const s = this.stringText();
    if (shape === null) return;
    if (shape.k === "str") return s;
    if (shape.k === "bytes") {
      try {
        return b64decode(s);
      } catch (e) {
        if (e instanceof BadInput) this.fail("bytes");
        throw e;
      }
    }
    this.fail("string where the shape has none");
  }

  boolValue(shape) {
    const v = this.boolText();
    if (shape === null) return;
    if (shape.k !== "bool") this.fail("bool where the shape has none");
    return v;
  }

  boolText() {
    const t = [0x74, 0x72, 0x75, 0x65], f = [0x66, 0x61, 0x6c, 0x73, 0x65];
    const want = this.b[this.i] === 0x74 ? t : f;
    for (let k = 0; k < want.length; k++) {
      if (this.b[this.i + k] !== want[k]) this.fail("bool");
    }
    this.i += want.length;
    return want === t;
  }

  number(shape) {
    const text = this.numberText();
    if (!safeNumber(text)) this.fail("number beyond 2^53-1");
    if (shape === null) return;
    const integer = /^-?(0|[1-9][0-9]*)$/.test(text);
    switch (shape.k) {
      case "int":
        if (!integer) this.fail("integer field");
        return Number(text);
      case "uint": case "uint32": {
        // An unsigned field refuses every minus, -0 included.
        if (!integer || text[0] === "-") this.fail("unsigned field");
        const v = Number(text);
        if (shape.k === "uint32" && v > 4294967295) this.fail("uint32 overflow");
        return v;
      }
    }
    this.fail("number where the shape has none");
  }

  numberText() {
    const start = this.i;
    if (this.b[this.i] === 0x2d) this.i++;
    const firstDigit = this.i;
    while (this.i < this.b.length && this.b[this.i] >= 0x30 && this.b[this.i] <= 0x39) this.i++;
    const digits = this.i - firstDigit;
    if (digits === 0) this.fail("number");
    if (digits > 1 && this.b[firstDigit] === 0x30) this.fail("leading zero");
    if (this.b[this.i] === 0x2e) {
      this.i++;
      const f = this.i;
      while (this.i < this.b.length && this.b[this.i] >= 0x30 && this.b[this.i] <= 0x39) this.i++;
      if (this.i === f) this.fail("number");
    }
    if (this.b[this.i] === 0x65 || this.b[this.i] === 0x45) {
      this.i++;
      if (this.b[this.i] === 0x2b || this.b[this.i] === 0x2d) this.i++;
      const e = this.i;
      while (this.i < this.b.length && this.b[this.i] >= 0x30 && this.b[this.i] <= 0x39) this.i++;
      if (this.i === e) this.fail("number");
    }
    // The decoder reads the whole slice; spreading the bytes into the
    // arguments of String.fromCharCode would tie the longest number the
    // parser can read to a limit of the engine - and V8, JavaScriptCore and
    // SpiderMonkey each stop somewhere else. A RangeError from there is no
    // answer a contract can give.
    return TEXT.decode(this.b.subarray(start, this.i));
  }

  // stringText reads one JSON string and gives its text after the escapes are
  // resolved: "a" and "a" are the same member name (rule 4).
  stringText() {
    this.want(0x22);
    let out = "";
    let chunk = this.i;
    for (;;) {
      if (this.i >= this.b.length) this.fail("string");
      const c = this.b[this.i];
      if (c === 0x22) {
        out += TEXT.decode(this.b.subarray(chunk, this.i));
        this.i++;
        return out;
      }
      if (c < 0x20) this.fail("control character in a string");
      if (c !== 0x5c) { this.i++; continue; }
      out += TEXT.decode(this.b.subarray(chunk, this.i));
      this.i++;
      const e = this.b[this.i++];
      const simple = { 0x22: '"', 0x5c: "\\", 0x2f: "/", 0x62: "\b", 0x66: "\f", 0x6e: "\n", 0x72: "\r", 0x74: "\t" };
      if (e in simple) {
        out += simple[e];
      } else if (e === 0x75) {
        const u = this.hex4();
        if (u >= 0xd800 && u <= 0xdbff) {
          if (this.b[this.i] !== 0x5c || this.b[this.i + 1] !== 0x75) this.fail("lone surrogate");
          this.i += 2;
          const lo = this.hex4();
          if (lo < 0xdc00 || lo > 0xdfff) this.fail("lone surrogate");
          out += String.fromCodePoint(0x10000 + ((u - 0xd800) << 10) + (lo - 0xdc00));
        } else if (u >= 0xdc00 && u <= 0xdfff) {
          this.fail("lone surrogate");
        } else {
          out += String.fromCharCode(u);
        }
      } else {
        this.fail("escape");
      }
      chunk = this.i;
    }
  }

  hex4() {
    let v = 0;
    for (let k = 0; k < 4; k++) {
      const c = this.b[this.i++];
      const d = c >= 0x30 && c <= 0x39 ? c - 0x30
        : c >= 0x61 && c <= 0x66 ? c - 0x61 + 10
        : c >= 0x41 && c <= 0x46 ? c - 0x41 + 10 : -1;
      if (d < 0) this.fail("escape");
      v = v * 16 + d;
    }
    return v;
  }
}

// join puts one segment after a path, as Go's appendSegment does: the members
// of the top-level value stand at their bare name.
function join(path, segment) {
  return path === "" ? segment : path + "/" + segment;
}

function nameBytes(s) {
  let n = 0;
  for (const cp of s) {
    const c = cp.codePointAt(0);
    n += c < 0x80 ? 1 : c < 0x800 ? 2 : c < 0x10000 ? 3 : 4;
  }
  return n;
}

// The digits of MAX_SAFE_INTEGER, taken from the constant itself: two places
// with the same number are two places that can drift apart.
const SAFE_DIGITS = String(MAX_SAFE_INTEGER);

// safeNumber decides on the digits, never on a Number: a Number would round
// 9007199254740991.4 down to the limit and let it through (Go: enc.safeNumber).
export function safeNumber(text) {
  const m = /^(-?)(\d+)(?:\.(\d*))?(?:[eE]([+-]?\d+))?$/.exec(text);
  if (!m) return false;
  const intDigits = m[2], fracDigits = m[3] || "";
  const exp = m[4] ? Number(m[4]) : 0;
  const all = intDigits + fracDigits;
  let first = 0;
  while (first < all.length && all[first] === "0") first++;
  if (first === all.length) return true;
  const whole = intDigits.length + exp - first;
  if (whole < SAFE_DIGITS.length) return true;
  if (whole > SAFE_DIGITS.length) return false;
  for (let j = 0; j < SAFE_DIGITS.length; j++) {
    const d = first + j < all.length ? all[first + j] : "0";
    if (d !== SAFE_DIGITS[j]) return d < SAFE_DIGITS[j];
  }
  for (let k = first + SAFE_DIGITS.length; k < all.length; k++) {
    if (all[k] !== "0") return false;
  }
  return true;
}
