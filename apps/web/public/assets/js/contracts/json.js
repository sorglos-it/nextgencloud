// Package json reads JSON from outside exactly as Go does (enc.DecodeStrict
// with an enc.Budget): one value, no unknown or repeated member names at any
// depth, valid UTF-8, no null, integers only where the contract has them, no
// number beyond 2^53-1, at most MAX_DEPTH levels, arrays and maps only where
// the budget lists them. JSON.parse alone decides none of this.
//
// Go checks the budget in a first pass that builds nothing and then decodes.
// Here one pass does both: every limit is checked as a container is entered
// and every element before it is read, so exactly the same documents are
// refused, and a refused one is never built to its end.
import { b64decode, BadInput } from "./b64.js";

export const RAW = -1;
export const MAX_DEPTH = 32;
export const MAX_SAFE_INTEGER = 9007199254740991;
export const MAX_MEMBERS = 64;
export const MAX_NAME = 64;

export class BadJSON extends Error {}

// Shapes. A shape says what a value may be; it mirrors the Go type the
// document decodes into. A member that is missing gives the zero value of its
// shape, as Go does, and the validate function of each contract asks for what
// has to be there.
export const str = { k: "str" };
export const bool = { k: "bool" };
export const int = { k: "int" };
export const uint = { k: "uint" };
export const uint32 = { k: "uint32" };
export const uint64 = { k: "uint" };
export const bytes = { k: "bytes" };
export const raw = { k: "raw" };
export const S = (members) => ({ k: "struct", m: members, ptr: false });
export const P = (members) => ({ k: "struct", m: members, ptr: true });
export const A = (of) => ({ k: "array", of });
export const M = (of) => ({ k: "map", of });

export class Budget {
  constructor({ size, items = {}, totals = {} }) {
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
    case "map": return {};
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

export function decode(input, budget, shape) {
  const b = input instanceof Uint8Array ? input : new Uint8Array(input);
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
  const v = p.value(shape, "", 1);
  p.ws();
  if (p.i !== b.length) throw new BadJSON("trailing data");
  return v;
}

class Parser {
  constructor(b, budget) {
    this.b = b;
    this.i = 0;
    this.budget = budget;
    this.totals = new Map();
    for (const path of Object.keys(budget.totals)) {
      // A total for a path the budget does not list as an array or map is a
      // mistake in the contract, not in the document.
      if (!(path in budget.items) || budget.items[path] === RAW) throw new BadJSON("budget");
      this.totals.set(path, 0);
    }
  }

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
    if (!this.totals.has(path)) return;
    const n = this.totals.get(path) + 1;
    if (n > this.budget.totals[path]) this.fail("total over its limit");
    this.totals.set(path, n);
  }

  value(shape, path, depth) {
    if (depth > MAX_DEPTH) this.fail("too deep");
    if (this.i >= this.b.length) this.fail("no value");
    // A path the budget marks RAW keeps its bytes whatever kind of value
    // stands there. It is decided here and not in object() and array(),
    // because a raw member may just as well hold a number or a string, and
    // those two never reach them: {"raw":9007199254740991.0} is a case of
    // json.json, and Go takes it.
    if (this.budget.items[path] === RAW) {
      if (shape.k !== "raw") this.fail("raw where the shape has none");
      return this.rawValue(depth);
    }
    const c = this.b[this.i];
    if (c === 0x7b) return this.object(shape, path, depth);
    if (c === 0x5b) return this.array(shape, path, depth);
    if (c === 0x22) return this.stringValue(shape);
    if (c === 0x74 || c === 0x66) return this.boolValue(shape);
    if (c === 0x6e) this.fail("null");
    if (c === 0x2d || (c >= 0x30 && c <= 0x39)) return this.number(shape);
    this.fail("unexpected byte");
  }

  // rawValue keeps the bytes of a value exactly as they stand, from its
  // opening byte to its closing one: noisep.E2EPrologue hashes them and
  // signatures cover them. Inside, only the rules that hold everywhere still
  // apply (depth, numbers, no null, no repeated names).
  rawValue(depth) {
    const start = this.i;
    this.skip(depth);
    return this.b.subarray(start, this.i);
  }

  skip(depth) {
    if (depth > MAX_DEPTH) this.fail("too deep");
    const c = this.b[this.i];
    if (c === 0x7b) {
      this.i++;
      const seen = new Set();
      this.ws();
      if (this.b[this.i] === 0x7d) { this.i++; return; }
      for (;;) {
        this.ws();
        const name = this.stringText();
        if (seen.has(name)) this.fail("repeated member name");
        seen.add(name);
        this.ws();
        this.want(0x3a);
        this.ws();
        this.skip(depth + 1);
        this.ws();
        if (this.b[this.i] === 0x2c) { this.i++; continue; }
        this.want(0x7d);
        return;
      }
    }
    if (c === 0x5b) {
      this.i++;
      this.ws();
      if (this.b[this.i] === 0x5d) { this.i++; return; }
      for (;;) {
        this.ws();
        this.skip(depth + 1);
        this.ws();
        if (this.b[this.i] === 0x2c) { this.i++; continue; }
        this.want(0x5d);
        return;
      }
    }
    if (c === 0x22) { this.stringText(); return; }
    if (c === 0x6e) this.fail("null");
    if (c === 0x74 || c === 0x66) { this.boolText(); return; }
    if (c === 0x2d || (c >= 0x30 && c <= 0x39)) {
      if (!safeNumber(this.numberText())) this.fail("number beyond 2^53-1");
      return;
    }
    this.fail("unexpected byte");
  }

  // object and array never see a RAW path: value() took it already.
  object(shape, path, depth) {
    const limit = this.budget.items[path];
    const mapped = limit !== undefined;
    if (mapped ? shape.k !== "map" : shape.k !== "struct") this.fail("object where the shape has none");
    if (mapped && limit < 0) this.fail("budget");
    this.i++;
    const out = mapped ? {} : zero({ ...shape, ptr: false });
    const seen = new Set();
    let n = 0;
    this.ws();
    if (this.b[this.i] === 0x7d) { this.i++; return out; }
    for (;;) {
      this.ws();
      const name = this.stringText();
      if (seen.has(name)) this.fail("repeated member name");
      seen.add(name);
      n++;
      if (mapped) {
        if (n > limit) this.fail("map over its limit");
        this.countTotal(path);
      } else {
        if (n > MAX_MEMBERS) this.fail("too many members");
        if (nameBytes(name) > MAX_NAME || name.includes("/") || name === "*") this.fail("member name");
        if (!Object.hasOwn(shape.m, name)) this.fail("unknown member " + name);
      }
      this.ws();
      this.want(0x3a);
      this.ws();
      const sub = mapped ? shape.of : shape.m[name];
      const subPath = path === "" ? (mapped ? "*" : name) : path + "/" + (mapped ? "*" : name);
      out[name] = this.value(sub, subPath, depth + 1);
      this.ws();
      if (this.b[this.i] === 0x2c) { this.i++; continue; }
      this.want(0x7d);
      return out;
    }
  }

  array(shape, path, depth) {
    const limit = this.budget.items[path];
    // An array whose path the budget does not list is refused: nothing from
    // outside may grow without a limit.
    if (limit === undefined || limit < 0) this.fail("array beyond its budget");
    if (shape.k !== "array") this.fail("array where the shape has none");
    this.i++;
    const out = [];
    const sub = path === "" ? "*" : path + "/*";
    this.ws();
    if (this.b[this.i] === 0x5d) { this.i++; return out; }
    for (;;) {
      this.ws();
      // Counted before it is read, so the first element too many is refused
      // unread.
      if (out.length + 1 > limit) this.fail("array over its limit");
      this.countTotal(path);
      out.push(this.value(shape.of, sub, depth + 1));
      this.ws();
      if (this.b[this.i] === 0x2c) { this.i++; continue; }
      this.want(0x5d);
      return out;
    }
  }

  stringValue(shape) {
    const s = this.stringText();
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
    if (shape.k === "raw") this.fail("raw where the shape has none");
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
    return String.fromCharCode(...this.b.subarray(start, this.i));
  }

  // stringText reads one JSON string and gives its text after the escapes are
  // resolved: "a" and "a" are the same member name (rule 4).
  stringText() {
    this.want(0x22);
    let out = "";
    let chunk = this.i;
    const dec = new TextDecoder();
    for (;;) {
      if (this.i >= this.b.length) this.fail("string");
      const c = this.b[this.i];
      if (c === 0x22) {
        out += dec.decode(this.b.subarray(chunk, this.i));
        this.i++;
        return out;
      }
      if (c < 0x20) this.fail("control character in a string");
      if (c !== 0x5c) { this.i++; continue; }
      out += dec.decode(this.b.subarray(chunk, this.i));
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

function nameBytes(s) {
  let n = 0;
  for (const cp of s) {
    const c = cp.codePointAt(0);
    n += c < 0x80 ? 1 : c < 0x800 ? 2 : c < 0x10000 ? 3 : 4;
  }
  return n;
}

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
  const limit = "9007199254740991";
  const whole = intDigits.length + exp - first;
  if (whole < limit.length) return true;
  if (whole > limit.length) return false;
  for (let j = 0; j < limit.length; j++) {
    const d = first + j < all.length ? all[first + j] : "0";
    if (d !== limit[j]) return d < limit[j];
  }
  for (let k = first + limit.length; k < all.length; k++) {
    if (all[k] !== "0") return false;
  }
  return true;
}
