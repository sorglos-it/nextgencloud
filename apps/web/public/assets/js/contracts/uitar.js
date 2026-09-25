// Package uitar: a ui-tar is how the API keeps an app's UI and sends it to
// the shell (ngc.desktop/ui). There is exactly one form, the one
// shared/store/ui.go writes; everything else is refused, so the browser and Go
// see exactly the same files (Go: store.WalkUI and store.UIWriter, vectors:
// ui.json).
//
// Go is handed an io.Reader and reads it forward: what it has read is gone,
// and no caller can hand it different bytes for the second look. Here the
// whole archive stands in an object the caller owns and keeps. So walkUI takes
// one copy of it before it decides anything, and the checksums, the sizes, the
// tree and every f.data it hands out read that copy and nothing else. Why a
// view is not enough stands at b64.bytesOf; what it means here is the thing
// this reader exists for: the bytes a consumer hashes afterwards - task 12
// computes the UI hash of an uploaded package this way - are the very bytes
// the checks were made on. The copy costs a second image of at most about
// 84 MiB, which is what an archive of MAX_UI_TOTAL comes to.
import { bytesOf } from "./b64.js";
import { validPath } from "./text.js";

export const BLOCK = 512;
export const MAX_ENTRIES = 10000;
export const MAX_DIRS = 1000;
export const MAX_UI_FILE = 16 << 20;
export const MAX_UI_TOTAL = 64 << 20;
const NAME_FIELD = 100; // longest name a header holds

export class BadPackage extends Error {}

// ours answers with the caller's bytes as bytes of our own. The error of the
// byte layer is folded into this module's one error, because a caller of uitar
// tells its errors apart by module and not by where inside the module the
// bytes were refused.
function ours(v) {
  try {
    return bytesOf(v, "uitar: not bytes");
  } catch {
    throw new BadPackage("not bytes");
  }
}

function allNUL(b) {
  for (let i = 0; i < b.length; i++) if (b[i] !== 0) return false;
  return true;
}

// ascii answers with one character per byte. Go compares the raw bytes of a
// header, and every name a ui-tar may carry is ASCII - validPath admits
// nothing else - so a byte over 127 becomes a character no path may hold and
// is refused where the name is checked. Decoding UTF-8 instead would fold
// several byte strings into one name, and it would count a length in
// characters where every limit of the contracts counts bytes.
function ascii(b) {
  let s = "";
  for (let i = 0; i < b.length; i++) s += String.fromCharCode(b[i]);
  return s;
}

// octal reads a field of octal digits that ends in exactly one NUL. The widest
// field a header has is twelve bytes, so eleven digits, and 8^11 is far below
// Number.MAX_SAFE_INTEGER: the value is exact, and there is nothing for a
// check of its own to catch.
function octal(f) {
  if (f.length < 2 || f[f.length - 1] !== 0) throw new BadPackage("octal");
  let v = 0;
  for (let i = 0; i < f.length - 1; i++) {
    const c = f[i];
    if (c < 0x30 || c > 0x37) throw new BadPackage("octal");
    v = v * 8 + (c - 0x30);
  }
  return v;
}

// octalField writes v as octal digits and a NUL, width bytes in all. A value
// too wide cannot happen - a size is at most MAX_UI_FILE, nine digits in
// eleven; a PAX record at most a block; a checksum at most six digits - and it
// is refused rather than written short, because a field that lost its first
// digit would be a header that says a different size.
function octalField(v, width) {
  const s = v.toString(8);
  if (s.length > width - 1) throw new BadPackage("octal field");
  return "0".repeat(width - 1 - s.length) + s + "\u0000";
}

// headerSum is the sum of a header's bytes with its checksum field counted as
// eight spaces.
function headerSum(b) {
  let sum = 8 * 0x20;
  for (let i = 0; i < BLOCK; i++) if (i < 148 || i >= 156) sum += b[i];
  return sum;
}

// parseHeader checks one block against the layout the writer produces: every
// field has exactly one value but the name, the size and the checksum.
function parseHeader(b) {
  const flag = b[156];
  let mode = "0000644\u0000";
  if (flag === 0x35) mode = "0000755\u0000";
  else if (flag !== 0x30 && flag !== 0x78) throw new BadPackage("typeflag");
  // The name ends at its first NUL. indexOf reads the whole block, so a NUL
  // that stands behind the field is no end of a name.
  let n = b.indexOf(0, 0);
  if (n < 0 || n > NAME_FIELD) n = NAME_FIELD;
  const size = octal(b.subarray(124, 136));
  const sum = octal(b.subarray(148, 155));
  if (b[155] !== 0x20 || sum !== headerSum(b)) throw new BadPackage("checksum");
  if (!allNUL(b.subarray(n, NAME_FIELD)) || ascii(b.subarray(100, 108)) !== mode
    || ascii(b.subarray(108, 124)) !== "0000000\u00000000000\u0000"
    || ascii(b.subarray(136, 148)) !== "00000000000\u0000"
    || !allNUL(b.subarray(157, 257)) || ascii(b.subarray(257, 265)) !== "ustar\u000000"
    || !allNUL(b.subarray(265))) throw new BadPackage("header");
  if (flag === 0x35 && size !== 0) throw new BadPackage("directory size");
  return { flag, name: ascii(b.subarray(0, n)), size };
}

// uiPath: a path inside the UI of an app, "ui/" and a safe relative path.
const uiPath = (p) => p.startsWith("ui/") && validPath(p.slice(3));

// uiName checks the name of an entry and returns its path: a file
// "ui/<validPath>", a directory "ui/" or "ui/<validPath>/" without the "/".
function uiName(name, dir) {
  if (dir) {
    if (!name.endsWith("/")) throw new BadPackage("directory");
    const p = name.slice(0, -1);
    if (p !== "ui" && !uiPath(p)) throw new BadPackage("directory");
    return p;
  }
  if (!uiPath(name)) throw new BadPackage("name");
  return name;
}

// paxRecord is the one PAX record of a ui-tar: "<length> path=<name>\n", the
// length counting the whole record.
function paxRecord(name) {
  const s = " path=" + name + "\n";
  let n = s.length + 1;
  while (String(n).length + s.length !== n) n = String(n).length + s.length;
  return String(n) + s;
}

// paxPath reads the data of a PAX header: exactly paxRecord(name), so the
// length has no sign and no leading zero and counts the record.
function paxPath(data) {
  const s = ascii(data);
  const sp = s.indexOf(" ");
  if (sp < 0 || !s.slice(sp).startsWith(" path=")) throw new BadPackage("pax record");
  const name = s.slice(sp + " path=".length, -1);
  // The record written for this name is the only spelling of it, so a length
  // that counts something else, a second record or anything after the newline
  // is refused by the comparison and not by a rule of its own.
  if (name === "" || paxRecord(name) !== s) throw new BadPackage("pax record");
  return name;
}

// newTree holds the rules of store.tree: no path twice, no entry under a file,
// no file where a directory is needed, and at most MAX_DIRS directories - each
// counted once, whether an entry names it or a path needs it ("ui/a/b" needs
// "ui" and "ui/a"). The bound on the directories is what keeps this small
// whatever the paths are: without it MAX_ENTRIES paths 97 levels deep would
// hold a directory per level.
function newTree() {
  const names = new Set(), files = new Set(), dirs = new Set();
  return {
    add(name, file) {
      if (names.has(name) || (file && dirs.has(name))) return false;
      // The ancestors, longest first. Once one is needed as a directory, so
      // are all shorter ones, and none of them is a file.
      for (let i = name.length - 1; i > 0; i--) {
        if (name[i] !== "/") continue;
        const p = name.slice(0, i);
        if (dirs.has(p)) break;
        if (files.has(p)) return false;
        dirs.add(p);
      }
      names.add(name);
      if (file) files.add(name);
      else dirs.add(name);
      return dirs.size <= MAX_DIRS;
    },
  };
}

// walkUI reads a ui-tar and answers with its regular files in the order the
// archive lists them, each with the bytes belonging to it. Every failure is a
// BadPackage.
export function walkUI(bytes) {
  const b = ours(bytes); // see the head of this file: one copy, decided on once
  const files = [];
  const tree = newTree();
  let total = 0, i = 0, n = 0;
  const block = () => {
    if (i + BLOCK > b.length) throw new BadPackage("truncated");
    const out = b.subarray(i, i + BLOCK);
    i += BLOCK;
    return out;
  };
  for (;;) {
    n++;
    const blk = block();
    if (allNUL(blk)) {
      // The end marker: one more zero block, then nothing.
      if (!allNUL(block()) || i !== b.length) throw new BadPackage("end marker");
      return files;
    }
    let e = parseHeader(blk);
    let name = e.name;
    if (e.flag === 0x78) {
      // A name too long for a header, then the entry's own header. The size
      // decides how much of the block is the record, so it is checked before
      // the block is read at all.
      if (name !== "" || e.size > BLOCK) throw new BadPackage("pax");
      const data = block();
      if (!allNUL(data.subarray(e.size))) throw new BadPackage("pax padding");
      name = paxPath(data.subarray(0, e.size));
      if (name.length <= NAME_FIELD) throw new BadPackage("pax for a short name");
      e = parseHeader(block());
      if (e.flag === 0x78 || e.name !== "") throw new BadPackage("pax");
    } else if (name === "") {
      throw new BadPackage("name");
    }
    const isDir = e.flag === 0x35;
    const path = uiName(name, isDir);
    if (n > MAX_ENTRIES) throw new BadPackage("entries");
    if (!isDir) {
      // Decided from the header, before anything behind it is read: a header
      // that claims 16 MiB and one byte is refused for its size and not for
      // the data it has not got.
      total += e.size;
      if (e.size > MAX_UI_FILE || total > MAX_UI_TOTAL) throw new BadPackage("size");
    }
    if (!tree.add(path, !isDir)) throw new BadPackage("tree");
    if (isDir) continue;
    if (i + e.size > b.length) throw new BadPackage("truncated");
    const data = b.subarray(i, i + e.size);
    i += e.size;
    const pad = (BLOCK - (e.size % BLOCK)) % BLOCK;
    if (pad > 0) {
      if (i + pad > b.length || !allNUL(b.subarray(i, i + pad))) throw new BadPackage("padding");
      i += pad;
    }
    files.push({ path, size: e.size, data });
  }
}

// put writes a string one byte per character. Everything written this way is
// ASCII by then: the fixed fields are, and a name has passed uiName.
function put(b, off, s) {
  for (let i = 0; i < s.length; i++) b[off + i] = s.charCodeAt(i);
}

// uiHeader is a header block: typeflag flag, name (empty in a PAX header and
// in the header after it) and size.
function uiHeader(flag, name, size) {
  const b = new Uint8Array(BLOCK);
  put(b, 0, name);
  put(b, 100, flag === 0x35 ? "0000755\u0000" : "0000644\u0000");
  put(b, 108, "0000000\u00000000000\u0000");
  put(b, 124, octalField(size, 12));
  put(b, 136, "00000000000\u0000");
  b[156] = flag;
  put(b, 257, "ustar\u000000");
  // Last, because it is the sum of everything above it.
  put(b, 148, octalField(headerSum(b), 7) + " ");
  return b;
}

// UIWriter writes a ui-tar in the one form walkUI reads. It keeps the rules of
// walkUI as it goes and refuses an entry that breaks one; after any refusal it
// refuses everything, because half an archive may not go on to look like a
// whole one. The shell needs it to compute the UI hash of a package an admin
// uploads, the same way ngc-store, the API and the controller compute it.
export class UIWriter {
  #parts = [];
  #tree = newTree();
  #n = 0;
  #total = 0;
  #done = false;

  // dir adds the directory "ui" or "ui/<validPath>".
  dir(path) {
    this.#add(path, true, null);
  }

  // file adds the file "ui/<validPath>" with data as its contents.
  file(path, data) {
    this.#add(path, false, data);
  }

  // close writes the end marker and answers with the whole archive. Nothing
  // may follow it.
  close() {
    if (this.#done) throw new BadPackage("closed");
    this.#done = true;
    let n = 2 * BLOCK; // the end marker, two blocks of NUL bytes
    for (const p of this.#parts) n += p.length;
    const out = new Uint8Array(n);
    let o = 0;
    for (const p of this.#parts) {
      out.set(p, o);
      o += p.length;
    }
    this.#parts = [];
    return out;
  }

  #add(path, dir, data) {
    if (this.#done) throw new BadPackage("closed");
    this.#done = true; // until the entry is written in full
    if (typeof path !== "string") throw new BadPackage("path");
    // The caller's bytes become ours here, so the size below and the bytes
    // written further down are one and the same: a caller who rewrites what
    // it handed over cannot change the archive afterwards. The copy is taken
    // before the size is looked at, so a file over MAX_UI_FILE is copied and
    // then refused. That costs at most one more image of bytes the caller
    // already holds, and the alternative - asking the caller's object how big
    // it is and deciding on that - is the one reading this whole layer exists
    // not to make.
    const bytes = dir ? null : ours(data);
    const size = dir ? 0 : bytes.length;
    let name = dir ? path + "/" : path;
    uiName(name, dir);
    if (size > MAX_UI_FILE || this.#total + size > MAX_UI_TOTAL) throw new BadPackage("size");
    if (++this.#n > MAX_ENTRIES) throw new BadPackage("entries");
    if (!this.#tree.add(path, !dir)) throw new BadPackage("tree");
    this.#total += size;
    if (name.length > NAME_FIELD) {
      const record = paxRecord(name);
      // A record longer than a block is one walkUI refuses, and a name that
      // long cannot get here: validPath holds a path to 200 bytes.
      if (record.length > BLOCK) throw new BadPackage("pax record");
      const pax = new Uint8Array(BLOCK);
      put(pax, 0, record);
      this.#parts.push(uiHeader(0x78, "", record.length), pax);
      name = "";
    }
    this.#parts.push(uiHeader(dir ? 0x35 : 0x30, name, size));
    if (!dir) {
      this.#parts.push(bytes);
      const pad = (BLOCK - (size % BLOCK)) % BLOCK;
      if (pad > 0) this.#parts.push(new Uint8Array(pad));
    }
    this.#done = false;
  }
}
