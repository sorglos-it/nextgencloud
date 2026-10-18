import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// The fuzz target for the ui-tar reader (CLAUDE.md asks every parser for one).
// This one takes the hardest input of the contracts: a binary archive out of a
// package somebody uploaded, where every field is a place to lie. ui.json says
// what Go decides for 100 archives; this says what has to hold for the ones
// nobody wrote down.
//
//   - walkUI answers with a list of files or throws BadPackage, and nothing
//     else. A TypeError or a RangeError from the engine is no answer a
//     contract can give, and a reader that walks off its own indexes is
//     exactly where one comes from.
//   - every file it hands out keeps the reader's own bounds, and the bounds
//     stand here in full rather than being asked of the module: a path "ui/"
//     and a safe relative path, a size that is the length of its own data, a
//     file within MAX_UI_FILE, all of them within MAX_UI_TOTAL, at most
//     MAX_ENTRIES of them, no path twice and no path under a file.
//   - the bytes it hands out are the reader's own copy: the caller's array is
//     overwritten after the answer and nothing in the answer moves. That is
//     the promise at the head of uitar.js, and the one a consumer hashes on.
//   - what UIWriter writes, walkUI reads back as exactly the entries that were
//     written, in order. The writer is the only second truth there is here, so
//     it is asked: an archive it produced that its own reader refuses is a
//     fault in one of the two.
//   - and enough of the inputs are accepted at all. Every rule above holds for
//     a reader that refuses everything, so without a floor under the accepted
//     ones a step backwards stays green.
//
// Seed and rounds come from the environment, so a failure can be run again:
// NGC_FUZZ_SEED=123 NGC_FUZZ_ROUNDS=50000 npm test -- contracts/uitar-fuzz
const v = vectors("ui.json");
const seed = Number(process.env.NGC_FUZZ_SEED || 20260923) >>> 0;
const rounds = Number(process.env.NGC_FUZZ_ROUNDS || 6000);

test(`fuzz: files or BadPackage, ${rounds} archives from seed ${seed}`, async ({ shell }) => {
  test.setTimeout(180_000);
  const out = await shell.evaluate(async ([tars, seed, rounds]) => {
    const u = await import("/assets/js/contracts/uitar.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");

    let state = seed || 1;
    const next = () => {
      state ^= state << 13; state >>>= 0;
      state ^= state >> 17;
      state ^= state << 5; state >>>= 0;
      return state / 4294967296;
    };
    const upto = (n) => Math.floor(next() * n);
    const pick = (a) => a[upto(a.length)];

    const BLOCK = 512;
    const put = (b, off, s) => { for (let i = 0; i < s.length; i++) b[off + i] = s.charCodeAt(i); };

    // The GESTALT of an archive, not only its numbers. Inputs that differ in a
    // size alone all walk the same way through a reader, so every entry below
    // is written in the ONE form store/ui.go writes - and then one single
    // thing about it is spoiled, and only in one entry out of three. Spoiling
    // every field of every entry gives an archive that dies at its first
    // header and says nothing about the rules behind it; this way whole
    // archives come out often enough for the rules further down to hold
    // against something.
    //
    // The fourteen spoilings are the fourteen ways a header can lie: a name
    // that leaves its folder or carries a NUL, a size in a spelling the field
    // does not have, a checksum over the wrong bytes or written the wrong way,
    // a mode, an owner, a time or a magic nobody writes, bytes behind the
    // name's NUL or in the tail nobody reads, a typeflag of another kind, a
    // link name, data that is not as long as its header says, and padding that
    // is not NUL.
    const badNames = [
      "ui/", "ui", "ui/css/", "ui/../etc/passwd", "ui/./x", "ui/.hidden", "ui//x", "ui/x/",
      "/ui/x", "ui\u0000/x", "ui/x\u0000y", "ui/ä", "ui/x y", "ui/x\\y", "..", "", "x",
      "UI/x", "services/amd64.tar", "manifest.json",
      "ui/" + "a".repeat(97), "ui/" + "a".repeat(197), "ui/" + "a/".repeat(60) + "x",
    ];
    const fileNames = ["ui/index.html", "ui/icon.svg", "ui/css/app.css", "ui/a", "ui/b.js",
      "ui/c/d.png", "ui/e/f/g.txt", "ui/A-B_c.1", "ui/x.y.z", "ui/" + "a".repeat(93),
      "ui/" + "a".repeat(96), "ui/" + "n".repeat(120),
      // A file and a path under it, so an archive that holds both really turns
      // up and the rule against a file under a file has something to refuse.
      "ui/a/b.txt", "ui/css", "ui/css/x/y.js"];
    const dirNames = ["ui", "ui/css", "ui/c", "ui/e", "ui/e/f", "ui/deep"];
    const sizes = [0, 1, 2, 3, 511, 512, 513, 1023, 1024, 4096, 16 << 20, (16 << 20) + 1,
      64 << 20, (64 << 20) + 1, 0x7fffffff];
    const modes = ["0000600\u0000", "0004755\u0000", "0000644 ", "0000755\u0000", "0000644\u0000"];
    const magics = ["ustar  \u0000", "ustar\u000001", "ustar\u0000\u0000\u0000",
      "\u0000\u0000\u0000\u0000\u0000\u0000\u0000\u0000", "GNUtar "];

    const octalField = (val, width) => {
      const s = val.toString(8);
      return s.length > width - 1 ? s.slice(0, width - 1) + "\u0000"
        : "0".repeat(width - 1 - s.length) + s + "\u0000";
    };
    const textBytes = (s) => {
      const b = new Uint8Array(s.length);
      put(b, 0, s);
      return b;
    };
    // The spellings a size field can be written in besides the one the writer
    // produces: base 256, a space for the NUL, no NUL at all, leading spaces,
    // digits that are no octal, an empty field, decimal.
    const oddSize = (size) => {
      switch (upto(7)) {
        case 0: {
          const f = new Uint8Array(12);
          f[0] = 0x80 | (upto(2) * 0x40);
          for (let i = 8; i < 12; i++) f[i] = upto(256);
          return f;
        }
        case 1: return textBytes(octalField(size, 12).replace("\u0000", " "));
        case 2: return textBytes(size.toString(8).padStart(12, "0"));
        case 3: return textBytes(" ".repeat(3) + octalField(size, 9));
        case 4: return textBytes(octalField(size, 12).replace(/0/g, () => pick(["0", "8", "9"])));
        case 5: return new Uint8Array(12);
        default: return textBytes(String(size).padStart(11, "0") + "\u0000");
      }
    };

    // header writes one block in the form of store/ui.go, with the one
    // spoiling s asks for and nothing else.
    const header = (flag, name, size, s) => {
      const b = new Uint8Array(BLOCK);
      put(b, 0, name.slice(0, 100));
      put(b, 100, s === 3 ? pick(modes) : flag === 0x35 ? "0000755\u0000" : "0000644\u0000");
      put(b, 108, s === 4 ? "0000001\u00000000001\u0000" : "0000000\u00000000000\u0000");
      if (s === 5) b.set(oddSize(size).subarray(0, 12), 124);
      else put(b, 124, octalField(size, 12));
      put(b, 136, s === 6 ? "00000000001\u0000" : "00000000000\u0000");
      b[156] = s === 7 ? pick([0x00, 0x32, 0x4c, 0x67, 0x53, 0x31, 0x39, 0x4b]) : flag;
      put(b, 257, s === 8 ? pick(magics) : "ustar\u000000");
      if (s === 9 && name.length < 99) b[name.length + 1] = 1 + upto(255);
      if (s === 10) b[265 + upto(BLOCK - 265)] = 1 + upto(255);
      if (s === 11) put(b, 157, "/etc/shadow");
      let sum = 8 * 0x20;
      for (let i = 0; i < BLOCK; i++) if (i < 148 || i >= 156) sum += b[i];
      // The checksum, right and four ways wrong: off by one, eight digits
      // without the space, a NUL where the space belongs, all spaces.
      switch (s) {
        case 0: put(b, 148, octalField(sum + 1, 7) + " "); break;
        case 1: put(b, 148, octalField(sum, 8)); break;
        case 2: put(b, 148, octalField(sum, 7) + "\u0000"); break;
        case 12: put(b, 148, "        "); break;
        default: put(b, 148, octalField(sum, 7) + " "); break;
      }
      return b;
    };

    // paxRecord is the record a PAX header carries, in the one spelling the
    // writer produces - and every fourth time in one that counts something
    // else, doubles the record, loses its newline or spells the key
    // differently.
    const paxRecord = (name) => {
      const s = " path=" + name + "\n";
      let n = s.length + 1;
      while (String(n).length + s.length !== n) n = String(n).length + s.length;
      const right = String(n) + s;
      if (upto(4) !== 0) return right;
      switch (upto(6)) {
        case 0: return right + right;
        case 1: return right.slice(0, -1);
        case 2: return "0" + right;
        case 3: return String(n + 1) + s;
        case 4: return String(n - 1) + s;
        default: return right.replace("path=", "Path=");
      }
    };

    // entry builds the blocks of one entry: a directory, a file with its data
    // and padding, or a name too long for the field - the PAX header, its
    // record block and then the entry's own header.
    const entry = () => {
      const parts = [];
      const s = upto(3) === 0 ? upto(14) : -1;
      const kind = upto(8);
      if (kind === 0) {
        const name = upto(3) === 0 ? pick([...fileNames, ...badNames]) : "ui/" + "n".repeat(100 + upto(60));
        const record = paxRecord(name);
        const data = new Uint8Array(BLOCK);
        put(data, 0, record);
        if (s === 13) data[BLOCK - 1] = 1 + upto(255); // padding that is not NUL
        parts.push(header(0x78, upto(10) === 0 ? pick(fileNames) : "", record.length, s));
        parts.push(data);
        parts.push(header(upto(10) === 0 ? 0x78 : 0x30, upto(10) === 0 ? pick(fileNames) : "", 0, -1));
        return parts;
      }
      if (kind === 1 || kind === 2) {
        const name = (upto(4) === 0 ? pick(badNames) : pick(dirNames)) + "/";
        parts.push(header(0x35, name, upto(8) === 0 ? pick(sizes) : 0, s));
        return parts;
      }
      // A file. The size in the header and the bytes behind it agree, except
      // where the round spoils exactly that: a header that promises what it
      // has not got is what the reader has to refuse from the header alone.
      const name = upto(4) === 0 ? pick(badNames) : pick(fileNames);
      const size = upto(8) === 0 ? pick(sizes) : upto(1200);
      const written = s === 13 ? upto(1200) : size;
      parts.push(header(0x30, name, size, s));
      if (written > 0 && written < 100000) {
        const data = new Uint8Array(written);
        for (let i = 0; i < written; i++) data[i] = upto(256);
        parts.push(data);
        const pad = (BLOCK - (written % BLOCK)) % BLOCK;
        if (pad > 0) {
          const p = new Uint8Array(pad);
          if (s === 13 && upto(2) === 0) p[upto(pad)] = 1 + upto(255);
          parts.push(p);
        }
      }
      return parts;
    };

    const join = (parts) => {
      let n = 0;
      for (const p of parts) n += p.length;
      const b = new Uint8Array(n);
      let o = 0;
      for (const p of parts) { b.set(p, o); o += p.length; }
      return b;
    };

    // archive puts entries and one of the six endings together: no marker, one
    // block, the two the form has, three, two and then junk, two and then
    // another entry.
    const archive = () => {
      const parts = [];
      const n = upto(5);
      for (let i = 0; i < n; i++) parts.push(...entry());
      switch (upto(12)) {
        case 0: break;
        case 1: parts.push(new Uint8Array(BLOCK)); break;
        case 2: parts.push(new Uint8Array(3 * BLOCK)); break;
        case 3: parts.push(new Uint8Array(2 * BLOCK), new Uint8Array(1 + upto(BLOCK))); break;
        case 4: parts.push(new Uint8Array(2 * BLOCK), ...entry(), new Uint8Array(2 * BLOCK)); break;
        default: parts.push(new Uint8Array(2 * BLOCK)); break;
      }
      return join(parts);
    };

    // Byte mutation, as the other fuzz targets do it.
    const mutate = (b) => {
      const out = Array.from(b);
      const steps = 1 + upto(3);
      for (let r = 0; r < steps && out.length > 0; r++) {
        const i = upto(out.length);
        switch (upto(5)) {
          case 0: out[i] ^= 1 << upto(8); break;
          case 1: out.splice(i, 1); break;
          case 2: out.splice(i, 0, upto(256)); break;
          case 3: out.splice(i, 0, ...out.slice(0, 1 + upto(8))); break;
          default: out.length = i; break;
        }
      }
      return new Uint8Array(out);
    };
    // Block mutation: the same archive with its BLOCKS moved. A byte flip
    // usually breaks a checksum and dies in the first header it touches;
    // moving whole blocks keeps every header right and breaks the ORDER, which
    // is the half of the form a checksum says nothing about.
    const shuffle = (b) => {
      const blocks = [];
      for (let i = 0; i + BLOCK <= b.length; i += BLOCK) blocks.push(b.subarray(i, i + BLOCK));
      const rest = b.subarray(blocks.length * BLOCK);
      if (blocks.length === 0) return b;
      switch (upto(6)) {
        case 0: blocks.splice(upto(blocks.length), 1); break;
        case 1: blocks.splice(upto(blocks.length), 0, blocks[upto(blocks.length)]); break;
        case 2: {
          const i = upto(blocks.length), j = upto(blocks.length);
          [blocks[i], blocks[j]] = [blocks[j], blocks[i]];
          break;
        }
        case 3: blocks.length = upto(blocks.length + 1); break;
        case 4: return b.subarray(0, upto(b.length + 1)); // a cut that is no block boundary
        default: blocks.push(new Uint8Array(BLOCK).fill(upto(256))); break;
      }
      return join([...blocks, rest]);
    };

    const corpus = tars.map((t) => b64decode(t));
    // What is no bytes at all: the reader has to say so itself and not let the
    // byte layer's error through.
    const notBytes = [null, undefined, 0, "ui/", [], {}, { length: BLOCK },
      new ArrayBuffer(BLOCK), new Uint16Array(BLOCK), new DataView(new ArrayBuffer(BLOCK))];

    // The path rule, spelled out here and not asked of the module: "ui/" and
    // then segments of letters, digits, "_" and "-", which may carry a "." but
    // never start with one - which is what says no "..", no hidden file, no
    // empty segment and nothing outside ASCII.
    const uiPath = (p) => {
      if (typeof p !== "string" || !p.startsWith("ui/") || p.length > 3 + 200) return false;
      const rest = p.slice(3);
      if (rest === "") return false;
      for (const seg of rest.split("/")) {
        if (seg === "") return false;
        if (!/^[A-Za-z0-9_-][A-Za-z0-9._-]*$/.test(seg)) return false;
      }
      return true;
    };

    const show = (b) => {
      let s = "";
      for (const byte of b.subarray(0, 96)) {
        s += byte >= 0x20 && byte < 0x7f ? String.fromCharCode(byte) : "\\x" + byte.toString(16).padStart(2, "0");
      }
      return s + (b.length > 96 ? "…" : "");
    };
    const found = [];
    const say = (why, b) => { if (found.length < 8) found.push({ why, input: b instanceof Uint8Array ? show(b) : String(b) }); };
    const count = { accepted: 0, files: 0, copied: 0 };

    for (let i = 0; i < rounds && found.length < 8; i++) {
      let input;
      switch (i % 6) {
        case 0: input = mutate(pick(corpus)); break;
        case 1: input = shuffle(pick(corpus)); break;
        case 2: case 3: input = archive(); break;
        case 4: input = mutate(archive()); break;
        default: input = shuffle(archive()); break;
      }
      let files;
      try {
        files = u.walkUI(input);
      } catch (e) {
        if (!(e instanceof u.BadPackage)) say(`walkUI threw ${e.constructor.name}: ${e.message}`, input);
        continue;
      }
      count.accepted++;
      if (!Array.isArray(files)) { say("no list of files came back", input); continue; }
      if (files.length > u.MAX_ENTRIES) { say(`${files.length} files for a limit of ${u.MAX_ENTRIES}`, input); continue; }
      count.files += files.length;
      let total = 0;
      const seen = new Set(), dirs = new Set();
      let bad = false;
      // Whether this archive holds a PAX header at all, for the rule below:
      // the name field is 100 bytes, so a longer path can only have come out
      // of one, and a reader that read past the field would hand out a path
      // this archive never carried.
      let hasPAX = false;
      for (let k = 0; k + BLOCK <= input.length; k += BLOCK) if (input[k + 156] === 0x78) hasPAX = true;
      for (const f of files) {
        if (!uiPath(f.path)) { say(`a file at ${JSON.stringify(f.path).slice(0, 80)}`, input); bad = true; break; }
        if (f.path.length > 100 && !hasPAX) { say(`a path of ${f.path.length} bytes out of an archive with no PAX header`, input); bad = true; break; }
        if (!(f.data instanceof Uint8Array)) { say("a file whose data is no bytes", input); bad = true; break; }
        if (f.size !== f.data.length) { say(`a size of ${f.size} over ${f.data.length} bytes`, input); bad = true; break; }
        if (f.size > u.MAX_UI_FILE) { say(`a file of ${f.size} bytes`, input); bad = true; break; }
        total += f.size;
        if (seen.has(f.path)) { say(`the path ${f.path} twice`, input); bad = true; break; }
        seen.add(f.path);
        // No file under another file: every prefix of this path is a
        // directory, so none of them may be a file of its own.
        for (let k = f.path.length - 1; k > 0; k--) if (f.path[k] === "/") dirs.add(f.path.slice(0, k));
      }
      if (bad) continue;
      if (total > u.MAX_UI_TOTAL) { say(`${total} bytes of files in one archive`, input); continue; }
      for (const p of seen) if (dirs.has(p)) { say(`${p} is a file and a directory at once`, input); bad = true; break; }
      if (bad) continue;
      // The copy: the caller's array is overwritten, and nothing that came
      // back may move with it.
      if (count.copied < 300 && input instanceof Uint8Array && files.length > 0) {
        const before = files.map((f) => Array.from(f.data.subarray(0, 32)));
        input.fill(0xff);
        const moved = files.some((f, k) => before[k].some((byte, j) => f.data[j] !== byte));
        if (moved) say("a file's bytes moved when the caller's array was overwritten", new Uint8Array(0));
        else count.copied++;
      }
    }

    for (const shape of notBytes) {
      try {
        u.walkUI(shape);
        say("something that is no bytes was read as an archive", String(shape));
      } catch (e) {
        if (!(e instanceof u.BadPackage)) say(`walkUI threw ${e.constructor.name} for what is no bytes`, String(shape));
      }
    }

    // What UIWriter writes, walkUI reads back. The writer keeps the same rules
    // as the reader, so a sequence it takes must come back as exactly those
    // entries - and after a refusal the writer refuses everything, close
    // included, because half an archive may not go on to look like a whole one.
    // Both halves are asserted: a writer that handed out the half it had
    // written would be an archive that reads as a package with files missing.
    const wrote = { archives: 0, entries: 0, refused: 0 };
    const good = ["ui/index.html", "ui/a", "ui/a.css", "ui/css/app.css", "ui/css", "ui",
      "ui/b/c/d", "ui/" + "n".repeat(120), "ui/A-B_c.1", "ui/x/y"];
    const paths = [...good, "ui/../x", "ui/.x", "ui//x", "", "x", "/ui/x", "ui/x\u0000"];
    for (let i = 0; i < 600 && found.length < 8; i++) {
      const w = new u.UIWriter();
      const want = [];
      const n = 1 + upto(6);
      // Two sequences in three draw only from paths the writer takes, so the
      // round trip below really runs; the rest draw freely and land in the
      // refusal rule.
      const from = upto(3) === 0 ? paths : good;
      let refused = false, broke = false;
      for (let k = 0; k < n; k++) {
        const p = pick(from);
        const dir = upto(3) === 0;
        const size = upto(4) === 0 ? upto(1500) : upto(40);
        const data = new Uint8Array(size);
        for (let j = 0; j < size; j++) data[j] = upto(256);
        try {
          if (dir) w.dir(p);
          else { w.file(p, data); want.push({ path: p, size }); }
        } catch (e) {
          if (!(e instanceof u.BadPackage)) {
            say(`UIWriter threw ${e.constructor.name}: ${e.message}`, new Uint8Array(0));
            broke = true;
          }
          refused = true;
          break;
        }
      }
      if (broke) break;
      if (refused) {
        // Everything after a refusal is refused, and nothing comes out.
        try {
          w.close();
          say("UIWriter handed out an archive after it had refused an entry", new Uint8Array(0));
        } catch (e) {
          if (!(e instanceof u.BadPackage)) say(`UIWriter.close threw ${e.constructor.name} after a refusal`, new Uint8Array(0));
          else wrote.refused++;
        }
        continue;
      }
      let out;
      try {
        out = w.close();
      } catch (e) {
        say(`UIWriter.close threw ${e.constructor.name}: ${e.message}`, new Uint8Array(0));
        break;
      }
      let back;
      try {
        back = u.walkUI(out);
      } catch (e) {
        say(`walkUI refused what UIWriter wrote: ${e.constructor.name}: ${e.message}`, out);
        break;
      }
      wrote.archives++;
      wrote.entries += back.length;
      if (back.length !== want.length || back.some((f, k) => f.path !== want[k].path || f.size !== want[k].size)) {
        say(`UIWriter wrote ${want.map((x) => x.path).join(",")} and walkUI read ${back.map((f) => f.path).join(",")}`, out);
      }
    }

    return { found, count, wrote };
  }, [v.map((c) => c.tar), seed, rounds]);

  // What was found comes first: the loop stops at the eighth finding, so a
  // fault early in the run leaves every count below far under its floor as
  // well. The floors are here to catch a run that did not really run.
  expect(out.found).toEqual([]);
  if (out.found.length === 0) {
    // Measured: 528 of 6000 archives of this seed are read and 303 files come
    // out of them, in both engines. The floors are at 2 and 1.5 in 100, a good
    // way under a third of that. They count in rounds and not in a fixed
    // number, because the rounds are a knob this file advertises itself: a
    // fixed count would turn every short run red without anything being wrong.
    expect(out.count.accepted).toBeGreaterThan(Math.floor(rounds * 0.02));
    // Enough of the accepted archives carried files, so the rules above had
    // something to hold: an archive of nothing but the end marker keeps all of
    // them and proves none.
    expect(out.count.files).toBeGreaterThan(Math.floor(rounds * 0.015));
    // The copy was really proved, not skipped for want of an accepted archive.
    expect(out.count.copied).toBeGreaterThan(60);
    // And the writer really wrote and really refused: without these, a
    // UIWriter that took nothing - or one that took everything - would leave
    // one of the two halves above with nothing to compare. Measured 225, 315
    // and 375 over the 600 sequences.
    expect(out.wrote.archives).toBeGreaterThan(70);
    expect(out.wrote.entries).toBeGreaterThan(100);
    expect(out.wrote.refused).toBeGreaterThan(100);
  }
});
