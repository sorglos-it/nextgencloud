// Reading an app package the admin uploads. Whatever the shell signs it
// computes itself: the package's SHA-256, its manifest and its UI hash
// (store.PackageUI). The API's answer only has to match (core.UploadResult).
//
// A package may be up to 4 GiB, so it is read as a stream: every byte passes
// through once, and only the ui-tar is kept, which is at most 84 MiB.
//
// What the reader below accepts is what Go accepts, and that is two layers:
// archive/tar decides what a header IS (store.Walk never sees a PAX or a GNU
// long-name block), and store.Walk decides which of those entries a package
// may hold. Being STRICTER than Go here is not the safe side: the API would
// take a package this shell refuses to weigh, and an admin would be told his
// package is broken by one half of NGC and fine by the other. So the corners
// archive/tar reads - the USTAR prefix field, base-256 numbers, a checksum
// counted signed, a V7 header without magic - are read here too, and the
// refusing is left where Go leaves it: in the rules of Walk.
import { sha256 } from "../../vendor/noble-hashes/sha2.js";
import { UIWriter, BadPackage, MAX_ENTRIES, MAX_UI_FILE, MAX_UI_TOTAL, MAX_DIRS } from "./uitar.js";
import { parseManifest, MAX_PACKAGE, MAX_MANIFEST } from "./catalog.js";
import { validPath } from "./text.js";
import { bytesOf } from "./b64.js";

// Every limit this module holds a package to belongs to another one, and is
// taken from there rather than written down again. In Go there is one
// store.MaxPackage, one store.MaxManifest and one store.MaxEntries, and Walk
// and the ui-tar share them; a second copy here would be a number that can
// drift from the one the rest of the shell decides by, and the drift would
// show as a package the API takes and the shell will not weigh. They are
// re-exported because this is where a caller reading a package looks for them.
export { MAX_PACKAGE, MAX_MANIFEST, MAX_ENTRIES };
const BLOCK = 512;
// What archive/tar reads of a PAX block or a GNU long name before it gives up
// (maxSpecialFileSize). It is the one place a meta header's data is held whole,
// so it has a bound of its own, far below a ui file's.
const MAX_SPECIAL = 1 << 20;
const MANIFEST = "manifest.json";
const NAME_FIELD = 100;

// The typeflags, as the bytes they are in a header block.
const REG_A = 0x00, REG = 0x30, DIR = 0x35, PAX = 0x78, GLOBAL = 0x67;
const LONG_NAME = 0x4c, LONG_LINK = 0x4b, SPARSE = 0x53;

// sha256Stream is a SHA-256 that takes its input in pieces (update/digest).
// The Web Crypto API of a browser has no such thing - crypto.subtle.digest
// wants the whole document in memory, and 4 GiB of it is the machine - so this
// one comes from the copied noble-hashes, beside the Argon2id that is there
// for the same kind of reason.
export const sha256Stream = () => sha256.create();

export async function packageUI(stream) {
  const pkgHash = sha256Stream();
  const ui = new UIWriter();
  const uiHash = sha256Stream();
  const r = new TarReader(stream, pkgHash);

  const first = await r.next();
  if (!first || first.flag !== REG || first.name !== MANIFEST || first.size > MAX_MANIFEST) {
    throw new BadPackage("manifest");
  }
  const manifest = parseManifest(await r.read(first.size));

  const tree = newTree();
  tree.add(MANIFEST, true);
  let images = 0, uiTotal = 0, n = 0;
  for (;;) {
    const h = await r.next();
    if (!h) break;
    // Go counts from the first entry AFTER the manifest and refuses at
    // n > MaxEntries, so MAX_ENTRIES of them are allowed, not one less.
    if (++n > MAX_ENTRIES) throw new BadPackage("entries");
    // Every entry, a file as well: Go trims one trailing slash off the name
    // before it decides anything, so "ui/a.css/" is the file "ui/a.css".
    const name = h.name.endsWith("/") ? h.name.slice(0, -1) : h.name;
    if (h.flag === DIR) {
      if (h.size !== 0 || (name !== "ui" && name !== "services" && !uiPath(name))) {
        throw new BadPackage("dir");
      }
      if (!tree.add(name, false)) throw new BadPackage("tree");
      await r.skip(h.size);
      continue;
    }
    if (h.flag !== REG) throw new BadPackage("entry kind");
    if (uiPath(name)) {
      // Held against the two ui limits FROM THE HEADER, before a byte behind
      // it is read. UIWriter refuses the same sizes, but only once it has been
      // handed the data, and a header that claims 4 GiB would be 4 GiB of
      // allocation before it ever got to say so.
      uiTotal += h.size;
      if (h.size > MAX_UI_FILE || uiTotal > MAX_UI_TOTAL) throw new BadPackage("ui size");
      if (!tree.add(name, true)) throw new BadPackage("tree");
      // The ui-tar is written in the order of the package, without directory
      // entries, exactly as store.PackageUI does.
      ui.file(name, await r.read(h.size), uiHash);
      continue;
    }
    if (name === "services/amd64.tar" || name === "services/arm64.tar") {
      if (h.size > MAX_PACKAGE) throw new BadPackage("image size");
      if (!tree.add(name, true)) throw new BadPackage("tree");
      images++;
      await r.skip(h.size);
      continue;
    }
    throw new BadPackage("entry");
  }
  const entry = manifest.ui ? "ui/" + manifest.ui.entry : null;
  if ((entry && !tree.files.has(entry))
    || (manifest.icon !== "" && !tree.files.has("ui/" + manifest.icon))
    || (manifest.services.length > 0) !== (images > 0)) {
    throw new BadPackage("manifest names what is missing");
  }

  const uiTar = ui.close(uiHash);
  return { manifest, uiTar, sha256: uiHash.digest(), packageSha256: pkgHash.digest() };
}

const uiPath = (name) => name.startsWith("ui/") && validPath(name.slice(3));

// newTree holds the shape rules of store.tree over the WHOLE package - ui
// files, service images and the manifest in one tree, which is where Go holds
// them. UIWriter keeps a tree of its own over the ui files alone, and both
// have to be satisfied, so this one is not the same check twice: it is the one
// that sees "services/amd64.tar" standing under the file "services".
function newTree() {
  const names = new Set(), files = new Set(), dirs = new Set();
  return {
    files,
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
      if (file) files.add(name); else dirs.add(name);
      return dirs.size <= MAX_DIRS;
    },
  };
}

// TarReader hands out one entry at a time and never holds more of the package
// than the piece it was asked for. Every byte it takes from the stream goes
// into the package hash once, in order, as the COPY that is then parsed - so
// the checksum covers exactly the bytes this reader worked on, and a source
// that refills one buffer cannot put different bytes into the two.
class TarReader {
  #reader;
  #hash;
  #chunk = new Uint8Array(0);
  #off = 0;
  #total = 0;

  constructor(stream, hash) {
    // A stream and nothing else: a caller who hands over a document instead of
    // a source is told so here, and not by a missing method three awaits later.
    if (!stream || typeof stream.getReader !== "function") throw new BadPackage("not a stream");
    this.#reader = stream.getReader();
    this.#hash = hash;
  }

  // #more takes the next piece of the stream. The copy is the point: the piece
  // belongs to the caller, who may hand out one buffer over and over, so what
  // is hashed and what is read have to be one set of bytes that nobody else
  // can reach. b64.bytesOf copies without reading a single property of the
  // object, so not even the length can move between the two.
  async #more() {
    const { value, done } = await this.#reader.read();
    if (done) return false;
    const c = bytesOf(value, "pack: not bytes");
    this.#total += c.length;
    // Go reads a package through a LimitedReader of MaxPackage and calls an
    // archive that reaches the limit truncated.
    if (this.#total > MAX_PACKAGE) throw new BadPackage("package too large");
    this.#hash.update(c);
    this.#chunk = c;
    this.#off = 0;
    return true;
  }

  // #take moves n bytes out of the stream, into out when there is one and into
  // nothing when there is not. Skipping a service image of gigabytes goes
  // through here without an allocation of its own.
  async #take(n, out) {
    let got = 0;
    while (got < n) {
      if (this.#off >= this.#chunk.length && !(await this.#more())) {
        throw new BadPackage("package cut short");
      }
      const k = Math.min(n - got, this.#chunk.length - this.#off);
      if (out) out.set(this.#chunk.subarray(this.#off, this.#off + k), got);
      this.#off += k;
      got += k;
    }
  }

  // #block reads one header block, or answers null where the archive is over:
  // nothing left at a block boundary, which is the one end Go calls clean.
  async #block() {
    if (this.#off >= this.#chunk.length && !(await this.#more())) return null;
    const b = new Uint8Array(BLOCK);
    await this.#take(BLOCK, b);
    return b;
  }

  async read(size) {
    const out = new Uint8Array(size);
    await this.#take(size, out);
    await this.#take(padding(size));
    return out;
  }

  async skip(size) {
    await this.#take(size);
    await this.#take(padding(size));
  }

  // next hands out the next entry, with the meta headers already worked in.
  // A PAX block and a GNU long name are not entries in Go and are not entries
  // here: they change the header that follows them and are gone.
  async next() {
    let pax = null, longName = "";
    for (;;) {
      const blk = await this.#block();
      if (!blk) return null;
      if (allNUL(blk)) {
        // Two zero blocks end an archive, and so does one followed by the end
        // of the stream; a zero block and then a header is broken.
        const second = await this.#block();
        if (!second) return null;
        if (!allNUL(second)) throw new BadPackage("header after the end marker");
        return null;
      }
      const h = parseHeader(blk);
      // Before anything is done with it: a base-256 field may say a size is
      // negative, and Go refuses one (handleRegularFile). Here it would reach
      // an allocation as a length, which is a RangeError and not an answer.
      if (h.size < 0) throw new BadPackage("size");
      if (h.flag === PAX || h.flag === GLOBAL) {
        const records = parsePAX(await this.#special(h.size));
        // A global header is no meta header for Go: it comes back as an entry
        // of its own, and its typeflag is what refuses it.
        if (h.flag === GLOBAL) return { flag: h.flag, name: h.name, size: 0, pax: records };
        pax = records;
        continue;
      }
      if (h.flag === LONG_NAME || h.flag === LONG_LINK) {
        const s = cstring(await this.#special(h.size));
        // A long LINK name is read and dropped: every entry that could carry
        // one is refused on its typeflag, but the block still has to be eaten
        // or the entry behind it would be read as a header.
        if (h.flag === LONG_NAME) longName = s;
        continue;
      }
      let { flag, name, size } = h;
      if (pax) {
        // An empty value keeps what the header said (Go: mergePAX).
        if (pax.get("path")) name = pax.get("path");
        if (pax.get("size")) size = decimalValue(pax.get("size"), "pax size");
      }
      if (longName !== "") name = longName;
      // The legacy typeflag: a directory if the name ends in a slash.
      if (flag === REG_A) flag = name.endsWith("/") ? DIR : REG;
      // A PAX record may have put a size of its own in front of the header's.
      if (size < 0) throw new BadPackage("size");
      // Sparse files, in both spellings. archive/tar reports a PAX sparse file
      // as a plain file of its FULL logical size and fills the holes itself,
      // so a few blocks of archive would hand out gigabytes.
      if (flag === SPARSE || [...pax?.keys() ?? []].some((k) => k.startsWith("GNU.sparse."))) {
        throw new BadPackage("sparse");
      }
      return { flag, name, size, pax };
    }
  }

  async #special(size) {
    if (size > MAX_SPECIAL) throw new BadPackage("meta header too large");
    return await this.read(size);
  }
}

const padding = (size) => (BLOCK - (size % BLOCK)) % BLOCK;
const allNUL = (b) => b.every((c) => c === 0);

// Byte for byte, the way Go reads the bytes of a header field as a string.
// In pieces, because one of these is a GNU long name, and that is a whole
// megabyte: spreading it into String.fromCharCode in one call is as many
// arguments as bytes, and an engine refuses that long before the limit the
// reader is meant to refuse at.
function ascii(b) {
  let s = "";
  for (let i = 0; i < b.length; i += 4096) s += String.fromCharCode(...b.subarray(i, i + 4096));
  return s;
}

// A tar string ends at the first NUL, or at the end of its field.
function cstring(b) {
  const i = b.indexOf(0);
  return ascii(i < 0 ? b : b.subarray(0, i));
}

// parseHeader reads what archive/tar reads of a header block: the checksum
// decides whether it is a header at all, the magic decides where the name's
// other half lives, and the numbers may be octal or base-256.
function parseHeader(b) {
  const sum = octalValue(b.subarray(148, 156), "checksum");
  let unsigned = 0, signed = 0;
  for (let i = 0; i < BLOCK; i++) {
    const c = i >= 148 && i < 156 ? 0x20 : b[i];
    unsigned += c;
    signed += c > 127 ? c - 256 : c;
  }
  if (sum !== unsigned && sum !== signed) throw new BadPackage("checksum");

  // A block whose checksum holds is a header of SOME format for Go, down to
  // V7, which has no magic at all - there is no further "is this a tar"
  // question, and the refusing happens on what the entry SAYS, not on which
  // dialect wrote it. Only USTAR, PAX and STAR carry the other half of a long
  // name in a prefix field, and STAR keeps it in a shorter one. GNU and V7
  // have no prefix, so their names are the name field alone.
  const magic = ascii(b.subarray(257, 263));
  let prefix = "";
  if (magic === "ustar\0") {
    const star = ascii(b.subarray(508, 512)) === "tar\0";
    prefix = cstring(b.subarray(345, star ? 476 : 500));
  }
  let name = cstring(b.subarray(0, NAME_FIELD));
  if (prefix !== "") name = prefix + "/" + name;
  return { flag: b[156], name, size: numericValue(b.subarray(124, 136), "size") };
}

// octalValue reads a field the way Go reads one: NULs and spaces at either end
// do not count, an empty field is 0, and every other byte must be an octal
// digit. The value is built from the DIGITS and held against the largest whole
// number a browser counts with, so a field of twelve sevens is refused where
// it stands and never becomes a rounded number.
function octalValue(f, why) {
  let i = 0, j = f.length;
  while (i < j && (f[i] === 0 || f[i] === 0x20)) i++;
  while (j > i && (f[j - 1] === 0 || f[j - 1] === 0x20)) j--;
  let v = 0;
  for (let k = i; k < j; k++) {
    const d = f[k] - 0x30;
    if (d < 0 || d > 7) throw new BadPackage(why);
    if (v > (Number.MAX_SAFE_INTEGER - d) / 8) throw new BadPackage(why);
    v = v * 8 + d;
  }
  return v;
}

// numericValue is octalValue with the base-256 form beside it: a first byte
// with the high bit set means the field is a two's complement number in big
// endian, which is how a size above 8 GiB is written at all.
function numericValue(f, why) {
  if (f.length === 0 || (f[0] & 0x80) === 0) return octalValue(f, why);
  // The sign lives in the second bit; a negative size is refused by the caller.
  const inv = (f[0] & 0x40) !== 0 ? 0xff : 0x00;
  let v = 0;
  for (let i = 0; i < f.length; i++) {
    let c = f[i] ^ inv;
    if (i === 0) c &= 0x7f;
    if (v > (Number.MAX_SAFE_INTEGER - c) / 256) throw new BadPackage(why);
    v = v * 256 + c;
  }
  return inv === 0xff ? -v - 1 : v;
}

// decimalValue reads a PAX number. Same rule as the fields above: the digits
// decide, and a number no browser can hold is refused and not rounded.
function decimalValue(s, why) {
  let i = 0, sign = 1;
  if (s[0] === "+" || s[0] === "-") { sign = s[0] === "-" ? -1 : 1; i = 1; }
  if (i >= s.length) throw new BadPackage(why);
  let v = 0;
  for (; i < s.length; i++) {
    const d = s.charCodeAt(i) - 0x30;
    if (d < 0 || d > 9) throw new BadPackage(why);
    if (v > (Number.MAX_SAFE_INTEGER - d) / 10) throw new BadPackage(why);
    v = v * 10 + d;
  }
  return sign * v;
}

// parsePAX reads the records of a PAX block: "<length> key=value\n", the
// length counting the whole record. Everything is kept, not only the two
// records that are used, because store.sparse decides on the KEYS - any
// GNU.sparse.* at all and the package is gone.
function parsePAX(data) {
  const s = new TextDecoder("utf-8", { fatal: false }).decode(data);
  const out = new Map();
  let rest = s;
  while (rest.length > 0) {
    const sp = rest.indexOf(" ");
    if (sp < 0) throw new BadPackage("pax record");
    const n = decimalValue(rest.slice(0, sp), "pax length");
    if (n < 5 || n > rest.length) throw new BadPackage("pax record");
    const rec = rest.slice(sp + 1, n - 1);
    if (rest[n - 1] !== "\n") throw new BadPackage("pax record");
    const eq = rec.indexOf("=");
    if (eq < 1) throw new BadPackage("pax record");
    out.set(rec.slice(0, eq), rec.slice(eq + 1));
    rest = rest.slice(n);
  }
  return out;
}

// testTar builds a package out of header descriptions, for tests that need an
// archive no packer would write: a symbolic link, an absolute path, a sparse
// entry. It is the only export here that is not part of the contract.
export function testTar(entries) {
  const parts = [];
  for (const e of entries) {
    const data = e.data ?? new Uint8Array(0);
    const size = e.size ?? data.length;
    const dir = e.flag === DIR;
    const name = dir && !e.name.endsWith("/") ? e.name + "/" : e.name;
    const b = new Uint8Array(BLOCK);
    const put = (at, s) => { for (let i = 0; i < s.length; i++) b[at + i] = s.charCodeAt(i); };
    if (utf8Length(name) > NAME_FIELD) throw new Error("testTar: name too long for a header");
    put(0, name);
    put(100, dir ? "0000755\0" : "0000644\0");
    put(108, "0000000\0");
    put(116, "0000000\0");
    put(124, octalField(dir ? 0 : size, 12));
    put(136, "00000000000\0");
    b[156] = e.flag;
    if (e.link) put(157, e.link);
    put(257, "ustar\u000000");
    put(329, "0000000\0");
    put(337, "0000000\0");
    let sum = 8 * 0x20;
    for (let i = 0; i < BLOCK; i++) if (i < 148 || i >= 156) sum += b[i];
    put(148, octalField(sum, 7) + " ");
    parts.push(b);
    if (!dir && data.length > 0) {
      parts.push(data, new Uint8Array(padding(data.length)));
    }
  }
  parts.push(new Uint8Array(2 * BLOCK));
  let total = 0;
  for (const p of parts) total += p.length;
  const out = new Uint8Array(total);
  let at = 0;
  for (const p of parts) { out.set(p, at); at += p.length; }
  return out;
}

const octalField = (v, width) => v.toString(8).padStart(width - 1, "0") + "\0";
// The name field holds bytes, and a header whose name would not fit as UTF-8
// must not be written as if it did.
const UTF8 = new TextEncoder();
const utf8Length = (s) => UTF8.encode(s).length;
