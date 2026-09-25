import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// The fuzz target for the package reader (CLAUDE.md asks every parser for
// one). A package is up to 4 GiB and arrives as a STREAM, so this reader has
// one thing none of the others has: the pieces it is handed have nothing to do
// with the pieces the archive is made of. A header may arrive in seven chunks,
// a size field may be split down the middle, and a reader that is right for
// one chunking and wrong for another is right in the tests and wrong on the
// machine where the network decides.
//
// So the chunking is fuzzed as hard as the bytes: every package is read twice,
// under two different chunkings, and the two answers have to be the same
// answer. That is the property a buffering mistake breaks and nothing else
// sees.
//
// What the target holds the module against:
//
//   - packageUI answers with a result or throws BadPackage, or BadCatalog out
//     of the manifest it parses - those two and nothing else. A TypeError or a
//     RangeError from the engine is no answer a contract can give, and a size
//     field that says a negative number reaches an allocation as a length,
//     which is exactly such a RangeError.
//   - the same bytes in another chunking give the same answer, to the byte.
//   - every result it gives keeps its own promises, and they stand here in
//     full: a ui-tar that walkUI reads again, whose files are the ones the
//     manifest names, a UI hash that is the SHA-256 of that very ui-tar, and a
//     package hash that is the SHA-256 of exactly the bytes the stream handed
//     over.
//   - something that is no stream is refused as a package, and not by a
//     missing method three awaits later.
//   - and enough of the packages get through at all. Every rule above holds
//     for a reader that refuses everything, so without a floor under the
//     accepted ones a step backwards stays green.
//
// THIS RULE WAS RED WHEN IT WAS WRITTEN, AND IT IS WHY THE READER DRAINS THE
// STREAM. The smallest case that showed it - manifest.json, ui/index.html, the
// two zero blocks of the end marker, and one more entry behind them, 4096
// bytes of which the archive is the first 3072 - gave two hashes out of one
// package, in chromium and in WebKit alike:
//
//   chunks of 1 byte, and of 512   packageSha256 = t4c48EeX5LGYyR6NJawjLo8PSECla8AiorkpwR_5NAA
//   chunks of 4096, and the whole  packageSha256 = teY7PHfN30mgW0Vk_BLt7-N4LKroA1JXa7VxXdzhoHw
//
// The first is the SHA-256 of the 3072 bytes the reader had worked on, the
// second of all 4096 - and which one a caller got depended on where the source
// happened to cut its pieces, which a Blob or a fetch body decides for itself.
// The cause was TarReader.#more, which hashes a piece as it ARRIVES: the tail
// of the last piece went into the checksum although nobody read it.
//
// The contract settled it: store.Package.SHA256 is "of the file as downloaded"
// (shared/store/catalog.go), with Size beside it as the same file's size and
// UI as the other object. So packageUI now reads the stream to its end, the
// rule below is green under every chunking, and this paragraph stays because
// the next reader of TarReader.#more deserves to know what the drain is for.
// The rule itself was never touched to get there.
//
// Measured again after the drain, in both engines: all four chunkings answer
// teY7PHfN30mgW0Vk_BLt7-N4LKroA1JXa7VxXdzhoHw, which is the SHA-256 of all
// 4096 bytes - the second of the two above, the file's. The ui-tar and its
// hash never moved and have not moved now.
//
// Seed and rounds come from the environment, so a failure can be run again:
// NGC_FUZZ_SEED=123 NGC_FUZZ_ROUNDS=5000 npm test -- contracts/pack-fuzz
const v = vectors("packui.json");
const seed = Number(process.env.NGC_FUZZ_SEED || 20260923) >>> 0;
const rounds = Number(process.env.NGC_FUZZ_ROUNDS || 700);

test(`fuzz: a package or BadPackage, ${rounds} packages from seed ${seed}`, async ({ shell }) => {
  test.setTimeout(300_000);
  const out = await shell.evaluate(async ([packages, seed, rounds]) => {
    const p = await import("/assets/js/contracts/pack.js");
    const u = await import("/assets/js/contracts/uitar.js");
    const { BadCatalog } = await import("/assets/js/contracts/catalog.js");
    const { b64decode, b64encode, BadInput } = await import("/assets/js/contracts/b64.js");

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
    const octal = (val, width) => {
      const s = val.toString(8);
      return s.length > width - 1 ? s.slice(-(width - 1)) + "\u0000"
        : "0".repeat(width - 1 - s.length) + s + "\u0000";
    };

    // The typeflags, as the bytes they are in a header block.
    const REG_A = 0x00, REG = 0x30, DIR = 0x35, PAX = 0x78, GLOBAL = 0x67;
    const LONG_NAME = 0x4c, LONG_LINK = 0x4b, SPARSE = 0x53, SYMLINK = 0x32;

    // block writes one header. The dialects are the ones archive/tar reads and
    // this reader therefore reads too: USTAR with a prefix field, STAR with a
    // shorter one, GNU, and V7, which has no magic at all. The size may be
    // octal or base 256, and base 256 may say a NEGATIVE number - the one
    // field in this whole contract that can reach an allocation as a length.
    const block = (o) => {
      const b = new Uint8Array(BLOCK);
      const name = o.name ?? "";
      put(b, 0, name.slice(0, 100));
      put(b, 100, o.flag === DIR ? "0000755\u0000" : "0000644\u0000");
      put(b, 108, "0000000\u0000");
      put(b, 116, "0000000\u0000");
      switch (o.sizeAs ?? "octal") {
        case "base256": {
          b[124] = 0x80;
          const val = o.size ?? 0;
          for (let i = 11; i >= 4; i--) b[124 + i] = (val / 2 ** (8 * (11 - i))) & 0xff;
          break;
        }
        case "negative": {
          // Two's complement in big endian with the sign bit set: what Go
          // reads as a size below zero and refuses before it allocates.
          for (let i = 0; i < 12; i++) b[124 + i] = 0xff;
          b[124] = 0xff;
          b[135] = 256 - (o.size ?? 1);
          break;
        }
        case "spaces": put(b, 124, "  " + octal(o.size ?? 0, 10)); break;
        default: put(b, 124, octal(o.size ?? 0, 12)); break;
      }
      put(b, 136, "00000000000\u0000");
      b[156] = o.flag ?? REG;
      if (o.link) put(b, 157, o.link.slice(0, 100));
      const magic = o.magic ?? "ustar\u000000";
      put(b, 257, magic);
      if (o.prefix) put(b, 345, o.prefix.slice(0, 130));
      if (o.star) put(b, 508, "tar\u0000");
      // The checksum, counted unsigned as everybody writes it - or signed,
      // which is the second reading archive/tar accepts and only differs once
      // a byte above 127 stands in the block.
      let unsigned = 8 * 0x20, signed = 8 * 0x20;
      for (let i = 0; i < BLOCK; i++) {
        if (i >= 148 && i < 156) continue;
        unsigned += b[i];
        signed += b[i] > 127 ? b[i] - 256 : b[i];
      }
      const sum = o.checksum === "signed" ? signed : o.checksum === "wrong" ? unsigned + 1 : unsigned;
      put(b, 148, octal(sum < 0 ? 0 : sum, 7) + " ");
      return b;
    };

    const encoder = new TextEncoder();
    const join = (parts) => {
      let n = 0;
      for (const x of parts) n += x.length;
      const b = new Uint8Array(n);
      let o = 0;
      for (const x of parts) { b.set(x, o); o += x.length; }
      return b;
    };
    const padding = (n) => (BLOCK - (n % BLOCK)) % BLOCK;
    const file = (o, data) => {
      const parts = [block({ ...o, size: o.size ?? data.length })];
      if (data.length > 0) parts.push(data, new Uint8Array(padding(data.length)));
      return parts;
    };
    // One PAX record, "<length> key=value\n", the length counting itself.
    const record = (kv) => {
      const s = " " + kv + "\n";
      let n = s.length + 1;
      while (String(n).length + s.length !== n) n = String(n).length + s.length;
      return String(n) + s;
    };

    // The manifest of a package that is meant to be taken: a window app with
    // one file, which is the smallest package this reader accepts.
    const manifestOf = (o) => JSON.stringify({
      id: o.id ?? "org.sorglos.terminal", name: "Terminal", publisher: "Sorglos",
      version: o.version ?? "1.0.0",
      ...(o.ui === null ? {} : { ui: { entry: o.entry ?? "index.html" } }),
      ...(o.icon ? { icon: o.icon } : {}),
      ...(o.services ? { services: [{ name: "main", command: ["/usr/bin/app"] }] } : {}),
    });

    const uiNames = ["ui/index.html", "ui/icon.svg", "ui/css/app.css", "ui/a.js", "ui/b/c.png"];
    const oddNames = ["ui", "ui/", "ui/../x", "/etc/passwd", "ui/.hidden", "ui//x", "x",
      "services/amd64.tar", "services/arm64.tar", "services/x.tar", "services", "manifest.json",
      "other.txt", "ui/" + "a".repeat(200)];

    // The GESTALT of a package: one thing spoiled at a time, in two packages
    // out of three. A package that spoils everything at once is refused at its
    // first entry - the manifest - and says nothing about the reader behind
    // it, which is the same lesson pack.spec.js wrote down for its hostile
    // entries.
    const build = () => {
      const s = upto(3) === 0 ? -1 : upto(20);
      const parts = [];
      const withIcon = upto(4) === 0;
      const withServices = upto(4) === 0;
      const mf = encoder.encode(manifestOf({
        entry: s === 0 ? pick(["nosuch.html", "../x", ""]) : "index.html",
        icon: withIcon ? "icon.svg" : "",
        services: withServices,
        version: s === 1 ? pick(["1.0", "01.0.0", ""]) : "1.0.0",
        id: s === 2 ? pick(["ngc.desktop", "Terminal", ""]) : "org.sorglos.terminal",
        ui: s === 3 ? null : undefined,
      }));
      // The manifest, first and named exactly: everything else is refused at
      // this one rule, which is why it is spoiled only where the round says so.
      parts.push(...file({
        name: s === 4 ? pick(["Manifest.json", "manifest.jsonx", "ui/index.html", ""]) : "manifest.json",
        flag: s === 5 ? pick([DIR, SYMLINK, PAX, LONG_NAME, GLOBAL, SPARSE, REG_A]) : REG,
        magic: s === 6 ? pick(["ustar  \u0000", "\u0000".repeat(8), "ustar\u000001"]) : undefined,
        checksum: s === 7 ? pick(["wrong", "signed"]) : undefined,
        sizeAs: s === 8 ? pick(["base256", "spaces"]) : undefined,
      }, mf));
      // The entry the manifest names, then whatever else this round draws.
      parts.push(...file({ name: "ui/index.html" }, encoder.encode("<!doctype html>")));
      if (withIcon) parts.push(...file({ name: "ui/icon.svg" }, encoder.encode("<svg/>")));
      if (withServices) parts.push(...file({ name: "services/amd64.tar", size: s === 9 ? 5 * 1024 * 1024 * 1024 : undefined }, new Uint8Array(upto(600))));
      const n = upto(4);
      for (let i = 0; i < n; i++) {
        const name = s === 10 ? pick(oddNames) : pick(uiNames);
        switch (upto(10)) {
          case 0: // a directory, in both spellings
            parts.push(block({ name: name + (upto(2) === 0 ? "/" : ""), flag: upto(2) === 0 ? DIR : REG_A, size: 0 }));
            break;
          case 1: { // the other half of the name in the prefix field
            const star = upto(3) === 0;
            parts.push(...file({ name: "c.css", prefix: "ui/" + "d".repeat(upto(120)), star }, new Uint8Array(upto(40))));
            break;
          }
          case 2: { // a GNU long name, and a long link name that is read and dropped
            const long = "ui/" + "l".repeat(100 + upto(80)) + ".js";
            parts.push(...file({ name: "././@LongLink", flag: upto(6) === 0 ? LONG_LINK : LONG_NAME },
              encoder.encode(long + "\u0000")));
            parts.push(...file({ name: "ui/short.js" }, new Uint8Array(upto(40))));
            break;
          }
          case 3: { // a PAX header: a path, a size, or a sparse key
            const kv = pick([`path=${name}`, `size=${upto(2000)}`, "GNU.sparse.major=1",
              "GNU.sparse.name=ui/x", "path=", `size=-${upto(100)}`, "comment=x"]);
            parts.push(...file({ name: "PaxHeaders/0", flag: upto(5) === 0 ? GLOBAL : PAX }, encoder.encode(record(kv))));
            parts.push(...file({ name }, new Uint8Array(upto(40))));
            break;
          }
          case 4: // a size no reader may allocate on
            parts.push(block({ name, flag: REG, size: 1 + upto(200), sizeAs: "negative" }));
            break;
          case 5: // a typeflag that is no file and no directory
            parts.push(block({ name, flag: pick([SYMLINK, SPARSE, 0x33, 0x36, 0x37]), size: 0, link: "/etc/shadow" }));
            break;
          case 6: // a header written the way V7 wrote one, without any magic
            parts.push(...file({ name, magic: "\u0000".repeat(8) }, new Uint8Array(upto(40))));
            break;
          case 7: { // a META header whose size is negative
            // The one place a length below zero is not caught twice: a plain
            // entry's size is held against zero again after the PAX records
            // are merged in, a meta header's is not - its data is read
            // straight away, and a negative length would reach the allocation
            // as a RangeError instead of an answer.
            const meta = pick([PAX, GLOBAL, LONG_NAME, LONG_LINK]);
            parts.push(block({ name: "PaxHeaders/0", flag: meta, size: 1 + upto(200), sizeAs: "negative" }));
            parts.push(...file({ name }, new Uint8Array(upto(40))));
            break;
          }
          default:
            parts.push(...file({ name, sizeAs: upto(8) === 0 ? "base256" : undefined }, new Uint8Array(upto(600))));
            break;
        }
      }
      // The ending: the two zero blocks the form has, one alone, none at all,
      // two and then another entry.
      switch (s === 11 ? upto(4) : 4) {
        case 0: break;
        case 1: parts.push(new Uint8Array(BLOCK)); break;
        case 2: parts.push(new Uint8Array(2 * BLOCK), ...file({ name: "ui/z.js" }, new Uint8Array(8))); break;
        case 3: parts.push(new Uint8Array(2 * BLOCK), new Uint8Array(1 + upto(400))); break;
        default: parts.push(new Uint8Array(2 * BLOCK)); break;
      }
      const whole = join(parts);
      // And the cut: a stream that stops in the middle of a header, of a file
      // or of the end marker.
      return s === 12 ? whole.subarray(0, upto(whole.length + 1)) : whole;
    };

    const mutate = (b) => {
      const out = Uint8Array.from(b);
      const steps = 1 + upto(3);
      for (let r = 0; r < steps && out.length > 0; r++) out[upto(out.length)] ^= 1 << upto(8);
      return out;
    };

    // chunking hands the same bytes over in pieces of its own: one byte at a
    // time, half a block, a whole block and one byte, and pieces the round
    // draws. A reader that is right for one of these and wrong for another is
    // right in a test and wrong on a network.
    const chunkings = [[1], [3], [BLOCK], [BLOCK + 1], [BLOCK - 1], [4096], [1, 511, 2], [0]];
    const streamOf = (b, sizes) => {
      let i = 0, k = 0;
      return new ReadableStream({
        pull(controller) {
          if (i >= b.length) { controller.close(); return; }
          const want = sizes[k++ % sizes.length];
          const n = want === 0 ? b.length - i : Math.min(want, b.length - i);
          // A SLICE, so every read hands over its own array: a source that
          // refilled one buffer is a different question, and pack copies for
          // it. Here the point is only where the pieces fall.
          controller.enqueue(b.slice(i, i + n));
          i += n;
        },
      });
    };
    const digest = async (b) => b64encode(new Uint8Array(await crypto.subtle.digest("SHA-256", b)));
    const same = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);

    const found = [];
    const say = (why, what) => { if (found.length < 8) found.push({ why, input: String(what).slice(0, 140) }); };
    const count = { read: 0, refused: 0, files: 0, pairs: 0 };

    // read runs one package under one chunking and answers what came of it,
    // in a form the two runs can be compared by.
    const read = async (bytes, sizes) => {
      try {
        const r = await p.packageUI(streamOf(bytes, sizes));
        return { ok: true, r };
      } catch (e) {
        if (!(e instanceof u.BadPackage) && !(e instanceof BadCatalog)) {
          say(`packageUI threw ${e.constructor.name}: ${e.message}`, b64encode(bytes.subarray(0, 60)));
          return null;
        }
        return { ok: false, why: e.message };
      }
    };

    const corpus = packages.map((b) => b64decode(b));
    for (let i = 0; i < rounds && found.length < 8; i++) {
      let bytes;
      switch (i % 5) {
        case 0: bytes = mutate(pick(corpus)); break;
        case 1: bytes = pick(corpus); break;
        case 2: bytes = mutate(build()); break;
        default: bytes = build(); break;
      }
      const first = await read(bytes, pick(chunkings));
      if (first === null) continue;
      const second = await read(bytes, pick(chunkings));
      if (second === null) continue;
      count.pairs++;
      // The same bytes, two chunkings, one answer. Each of the three pieces
      // is compared on its own, so a failure names which of them moved.
      if (first.ok !== second.ok) {
        say(`one chunking read the package and another refused it: ${first.why ?? second.why}`, b64encode(bytes.subarray(0, 60)));
        continue;
      }
      if (!first.ok) {
        count.refused++;
        if (first.why !== second.why) say(`two chunkings refused for two reasons: ${first.why} and ${second.why}`, b64encode(bytes.subarray(0, 60)));
        continue;
      }
      count.read++;
      const a = first.r, b = second.r;
      if (!same(a.uiTar, b.uiTar)) {
        say("two chunkings wrote two different ui-tars out of the same bytes", b64encode(bytes.subarray(0, 60)));
        continue;
      }
      if (!same(a.sha256, b.sha256)) {
        say("two chunkings computed two different UI hashes out of the same bytes", b64encode(bytes.subarray(0, 60)));
        continue;
      }
      if (!same(a.packageSha256, b.packageSha256)) {
        say(`two chunkings computed two different PACKAGE hashes out of the same bytes: ${b64encode(a.packageSha256)} and ${b64encode(b.packageSha256)}`,
          b64encode(bytes.subarray(0, 60)));
        continue;
      }
      // The result keeps its own promises.
      if (!(a.uiTar instanceof Uint8Array) || !(a.sha256 instanceof Uint8Array) || a.sha256.length !== 32
        || !(a.packageSha256 instanceof Uint8Array) || a.packageSha256.length !== 32) {
        say("a result whose pieces are not what they are called", b64encode(bytes.subarray(0, 60)));
        continue;
      }
      if (b64encode(a.sha256) !== await digest(a.uiTar)) {
        say("the UI hash is not the hash of the ui-tar it came with", b64encode(bytes.subarray(0, 60)));
        continue;
      }
      if (b64encode(a.packageSha256) !== await digest(bytes)) {
        say("the package hash is not the hash of the bytes the stream handed over", b64encode(bytes.subarray(0, 60)));
        continue;
      }
      // The ui-tar is one walkUI reads, and it holds what the manifest names.
      let files;
      try {
        files = u.walkUI(a.uiTar);
      } catch (e) {
        say(`walkUI refused the ui-tar this reader wrote: ${e.message}`, b64encode(bytes.subarray(0, 60)));
        continue;
      }
      count.files += files.length;
      const paths = files.map((f) => f.path);
      if (new Set(paths).size !== paths.length) say("a ui-tar with a path twice", b64encode(bytes.subarray(0, 60)));
      if (files.some((f) => f.size !== f.data.length)) say("a ui file whose size is not its own length", b64encode(bytes.subarray(0, 60)));
      const entry = a.manifest.ui ? "ui/" + a.manifest.ui.entry : null;
      if (entry !== null && !paths.includes(entry)) say(`a package taken whose manifest names ${entry}`, b64encode(bytes.subarray(0, 60)));
      if (a.manifest.icon !== "" && !paths.includes("ui/" + a.manifest.icon)) {
        say(`a package taken whose manifest names the icon ${a.manifest.icon}`, b64encode(bytes.subarray(0, 60)));
      }
    }

    // What is no stream, and a stream whose pieces are no bytes: both are this
    // module's refusal and not a missing method three awaits later.
    // A document where a source belongs, and an object whose getReader is no
    // function: both are told so here and not by a missing method three awaits
    // later. An object whose getReader THROWS is not among them - what comes
    // back then is the caller's own exception out of the caller's own method,
    // and demanding that a reader swallow it would be this file inventing a
    // promise. The same goes for a stream that is already locked, where the
    // engine's own TypeError comes out of getReader.
    const notStreams = [null, undefined, 0, "", [], {}, new Uint8Array(BLOCK), { getReader: 7 }];
    for (const s of notStreams) {
      try {
        await p.packageUI(s);
        say("something that is no stream was read as a package", String(s));
      } catch (e) {
        if (!(e instanceof u.BadPackage)) say(`packageUI threw ${e.constructor.name} for what is no stream`, String(s));
      }
    }
    // A chunk that is no bytes is refused as well - with the byte layer's
    // BadInput, not with BadPackage. MEASURED, and left standing rather than
    // asserted away: uitar folds that error into its own ("a caller of uitar
    // tells its errors apart by module"), pack does not, so packageUI can end
    // in BadInput where walkUI would end in BadPackage. It is no hole and no
    // finding of this target, because it cannot be reached from a package at
    // all: the bytes of a document never pass through bytesOf here, only the
    // pieces a SOURCE hands over, and every source the shell uses - Blob
    // .stream(), Response.body - hands over Uint8Arrays. A caller who builds a
    // stream of strings has made a mistake in his own code and is told so.
    // What this loop still holds is the part that matters: such a stream is
    // never READ as a package.
    for (const chunk of ["abc", 7, null, [1, 2, 3], new Uint16Array(4), new ArrayBuffer(8)]) {
      const stream = new ReadableStream({
        start(controller) { controller.enqueue(chunk); controller.close(); },
      });
      try {
        await p.packageUI(stream);
        say("a stream of something that is no bytes was read as a package", String(chunk));
      } catch (e) {
        if (!(e instanceof u.BadPackage) && !(e instanceof BadInput)) {
          say(`packageUI threw ${e.constructor.name} for a chunk that is no bytes`, String(chunk));
        }
      }
    }

    return { found, count };
  }, [v.map((c) => c.package), seed, rounds]);

  // What was found comes first: the loop stops at the eighth finding, so a
  // fault early in the run leaves the counts below under their floors as well.
  expect(out.found).toEqual([]);
  if (out.found.length === 0) {
    // Measured: 282 of 700 packages of this seed are read and 510 ui files
    // come out of them, the same in both engines; the run costs chromium 3
    // seconds and the WebKit Playwright brings about 10. The floors are at 12
    // and 20 in 100, about a third of that. They count in rounds and not in a
    // fixed number, because the rounds are a knob this file advertises itself.
    expect(out.count.read).toBeGreaterThan(Math.floor(rounds * 0.12));
    expect(out.count.files).toBeGreaterThan(Math.floor(rounds * 0.20));
    expect(out.count.refused).toBeGreaterThan(Math.floor(rounds * 0.2));
    // And every round really ran twice, so the chunking rule held against
    // something on each of them.
    expect(out.count.pairs).toBeGreaterThan(Math.floor(rounds * 0.9));
  }
});
