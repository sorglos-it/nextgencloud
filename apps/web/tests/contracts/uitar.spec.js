import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// The vectors of shared/store/ui.go: 100 ui-tars and, for every archive Go
// accepts, the paths WalkUI hands out and their order. They carry no sizes -
// uiVector.Files is a []string - so the sizes are held here against the data
// the reader itself hands out, which is the reading that matters: a size that
// disagreed with its own bytes is a size nobody can use.
const v = vectors("ui.json");

// The two tests below build header blocks of their own, and they build them
// twice rather than sharing one helper. The page keeps its content security
// policy on, so nothing can be handed to the page as source: no data: module,
// no eval, no new Function. Playwright serializes the function it is given and
// nothing it closes over, so a helper has to stand inside the function that
// uses it. Written out twice is the price of that, and the alternative - a
// test-only module under public/ - would put test code where the shell is
// served from.
//
// And they are built here and never with UIWriter: a test that took a header's
// checksum from the module it is testing could not tell a right one from a
// wrong one, and the limits are exactly where a reader is tempted to read the
// data first and the header second.

test("every ui-tar of ui.json is decided as Go decides it", async ({ shell }) => {
  test.setTimeout(180_000);
  const got = await shell.evaluate(async (cases) => {
    const u = await import("/assets/js/contracts/uitar.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    return cases.map((c) => {
      try {
        const files = u.walkUI(b64decode(c.tar));
        return {
          valid: true,
          paths: files.map((f) => f.path),
          // Every file's size against the bytes handed out with it.
          sizesAgree: files.every((f) => f.size === f.data.length),
        };
      } catch (e) {
        return {
          valid: false,
          paths: [],
          why: e instanceof u.BadPackage ? e.message : "not BadPackage: " + e,
        };
      }
    });
  }, v);

  const wrong = [];
  for (let i = 0; i < v.length; i++) {
    if (v[i].valid !== got[i].valid) {
      wrong.push(`${v[i].name}: want ${v[i].valid}, got ${got[i].valid} ${got[i].why ?? ""}`);
    }
  }
  expect(wrong).toEqual([]);
  // For a valid archive the reader must hand out exactly the files Go hands
  // out, in the same order.
  for (let i = 0; i < v.length; i++) {
    if (!v[i].valid) continue;
    expect(got[i].paths, v[i].name).toEqual(v[i].files ?? []);
    expect(got[i].sizesAgree, `${v[i].name}: a size that is not the size of its own data`).toBe(true);
  }
  // A refusal is the module's own error and never one from the engine, so a
  // caller can tell "no ui-tar" from "this reader is broken".
  expect(got.filter((g) => !g.valid && g.why.startsWith("not BadPackage"))).toEqual([]);
});

test("the size limits are checked at the header, before any data is read", async ({ shell }) => {
  test.setTimeout(180_000);
  const out = await shell.evaluate(async () => {
    const u = await import("/assets/js/contracts/uitar.js");
    const BLOCK = 512;
    const octal = (n, width) => {
      const s = n.toString(8);
      return "0".repeat(width - 1 - s.length) + s + "\u0000";
    };
    // One header block in the form store/ui.go writes, with its checksum
    // summed here: the eight checksum bytes counted as spaces.
    const head = (flag, name, size) => {
      const b = new Uint8Array(BLOCK);
      const put = (off, s) => { for (let i = 0; i < s.length; i++) b[off + i] = s.charCodeAt(i); };
      put(0, name);
      put(100, flag === 0x35 ? "0000755\u0000" : "0000644\u0000");
      put(108, "0000000\u00000000000\u0000");
      put(124, octal(size, 12));
      put(136, "00000000000\u0000");
      b[156] = flag;
      put(257, "ustar\u000000");
      let sum = 8 * 0x20;
      for (let i = 0; i < BLOCK; i++) if (i < 148 || i >= 156) sum += b[i];
      put(148, octal(sum, 7) + " ");
      return b;
    };
    const END = new Uint8Array(2 * BLOCK);
    const cat = (...parts) => {
      let n = 0;
      for (const p of parts) n += p.length;
      const out = new Uint8Array(n);
      let o = 0;
      for (const p of parts) { out.set(p, o); o += p.length; }
      return out;
    };
    // Why it answered, not just that it answered: the reason is what tells a
    // limit that fired at the header from an archive that simply ran out.
    const why = (bytes) => {
      try {
        u.walkUI(bytes);
        return "ok";
      } catch (e) {
        return e instanceof u.BadPackage ? e.message : "not BadPackage: " + e;
      }
    };

    // A header alone, with nothing behind it. A reader that took the data
    // first would answer "truncated" for both of these; one over MaxUIFile
    // has to be refused from the header, and the archive at the limit is the
    // control that says the reason was the size and not the missing data.
    const overFile = why(cat(head(0x30, "ui/big.bin", u.MAX_UI_FILE + 1), END));
    const atFile = why(cat(head(0x30, "ui/big.bin", u.MAX_UI_FILE), END));

    // MaxEntries entries of no data at all, and one more.
    const empties = (n) => {
      const b = new Uint8Array(n * BLOCK + END.length);
      for (let i = 0; i < n; i++) b.set(head(0x30, "ui/f" + i, 0), i * BLOCK);
      b.set(END, n * BLOCK);
      return b;
    };
    const entriesAt = why(empties(u.MAX_ENTRIES));
    const entriesOver = why(empties(u.MAX_ENTRIES + 1));

    // MaxUITotal in all: four files of MaxUIFile, whose data has to be there
    // for the walk to reach the header after them. The buffer is built once
    // and read twice - first up to the two zero blocks already in it, then
    // with one more header written over them - so the 64 MiB it costs is
    // paid once.
    const four = 4 * (BLOCK + u.MAX_UI_FILE);
    const buf = new Uint8Array(four + BLOCK + END.length);
    for (let i = 0; i < 4; i++) buf.set(head(0x30, "ui/f" + i, u.MAX_UI_FILE), i * (BLOCK + u.MAX_UI_FILE));
    let totalAt = "ok", atCount = -1;
    try {
      atCount = u.walkUI(buf.subarray(0, four + END.length)).length;
    } catch (e) {
      totalAt = e instanceof u.BadPackage ? e.message : "not BadPackage: " + e;
    }
    // One more file of a single byte, its data not there either: 64 MiB and
    // one byte is over MaxUITotal, and the header says so on its own.
    buf.set(head(0x30, "ui/one-more", 1), four);
    const totalOver = why(buf);

    return { overFile, atFile, entriesAt, entriesOver, totalAt, atCount, totalOver };
  });

  // A file one byte over the limit is refused for its size; the same archive
  // at the limit gets as far as its missing data. That difference is the
  // whole of "checked at the header".
  expect(out.overFile).toBe("size");
  expect(out.atFile).toBe("truncated");
  // MaxEntries entries pass, one more does not.
  expect(out.entriesAt).toBe("ok");
  expect(out.entriesOver).toBe("entries");
  // MaxUITotal in all passes with its four files, one byte more does not.
  expect(out.totalAt).toBe("ok");
  expect(out.atCount).toBe(4);
  expect(out.totalOver).toBe("size");
});

test("what the reader checked is what it hands out", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const u = await import("/assets/js/contracts/uitar.js");
    const BLOCK = 512;
    const octal = (n, width) => {
      const s = n.toString(8);
      return "0".repeat(width - 1 - s.length) + s + "\u0000";
    };
    const head = (flag, name, size) => {
      const b = new Uint8Array(BLOCK);
      const put = (off, s) => { for (let i = 0; i < s.length; i++) b[off + i] = s.charCodeAt(i); };
      put(0, name);
      put(100, flag === 0x35 ? "0000755\u0000" : "0000644\u0000");
      put(108, "0000000\u00000000000\u0000");
      put(124, octal(size, 12));
      put(136, "00000000000\u0000");
      b[156] = flag;
      put(257, "ustar\u000000");
      let sum = 8 * 0x20;
      for (let i = 0; i < BLOCK; i++) if (i < 148 || i >= 156) sum += b[i];
      put(148, octal(sum, 7) + " ");
      return b;
    };
    const archive = () => {
      const b = new Uint8Array(4 * BLOCK);
      b.set(head(0x30, "ui/x.bin", 4), 0);
      b.set([1, 2, 3, 4], BLOCK);
      return b;
    };

    // The archive is checked, and then the caller rewrites every byte of it.
    // What the reader handed out may not follow: the checksum, the sizes and
    // the tree were decided on those bytes, and a consumer that hashes the
    // data afterwards would otherwise hash something nothing ever checked.
    const tar = archive();
    const files = u.walkUI(tar);
    tar.fill(0x41);
    const afterOverwrite = [...files[0].data];

    // And an object whose length lies is decided on the bytes it really
    // holds - ignored, not refused, the way the byte layer decides it
    // everywhere else (bytes.spec.js "a length that disagrees with the view
    // is ignored, not refused").
    class Liar extends Uint8Array {
      get length() { return 0; }
    }
    const liar = new Liar(4 * BLOCK);
    new Uint8Array(liar.buffer).set(archive());
    let lying;
    try {
      lying = u.walkUI(liar).map((x) => `${x.path}:${x.size}:${[...x.data].join(",")}`);
    } catch (e) {
      lying = "refused: " + e.message;
    }

    // What is no bytes at all is this module's own error and not the byte
    // layer's: a caller of uitar tells its errors apart by module.
    const notBytes = ["x", null, undefined, [1, 2, 3], new ArrayBuffer(8)].map((x) => {
      try {
        u.walkUI(x);
        return "taken";
      } catch (e) {
        return e instanceof u.BadPackage ? "BadPackage" : "other: " + e;
      }
    });
    return { afterOverwrite, lying, notBytes };
  });

  expect(out.afterOverwrite).toEqual([1, 2, 3, 4]);
  expect(out.lying).toEqual(["ui/x.bin:4:1,2,3,4"]);
  expect(out.notBytes).toEqual(["BadPackage", "BadPackage", "BadPackage", "BadPackage", "BadPackage"]);
});

test("UIWriter writes the one form walkUI reads, and keeps its rules", async ({ shell }) => {
  test.setTimeout(120_000);
  const out = await shell.evaluate(async () => {
    const u = await import("/assets/js/contracts/uitar.js");
    const bytes = (s) => new TextEncoder().encode(s);
    const long = "ui/" + "p".repeat(150); // over 100 bytes: a PAX header

    const w = new u.UIWriter();
    w.dir("ui");
    w.file("ui/index.html", bytes("<!doctype html>"));
    w.file("ui/big.bin", new Uint8Array(3 * 512 + 7).fill(0x7a));
    w.file(long, bytes("long"));
    w.dir("ui/" + "q".repeat(120)); // with its "/" over 100 bytes too
    w.file("ui/empty", new Uint8Array(0));
    const files = u.walkUI(w.close());
    const roundTrip = files.map((f) => `${f.path}:${f.size}`);
    const bigIsWhole = files[1].data.every((b) => b === 0x7a);

    // The writer holds the same rules as the reader, so what it produced is
    // what the reader takes: a refusal here is an entry that would have been
    // refused there.
    const refuses = (build) => {
      try {
        build(new u.UIWriter());
        return false;
      } catch (e) {
        return e instanceof u.BadPackage;
      }
    };
    const refused = {
      badName: refuses((x) => x.file("ui/../x", bytes("x"))),
      fileNamedUI: refuses((x) => x.file("ui", bytes("x"))),
      dirOutsideUI: refuses((x) => x.dir("x")),
      tooLarge: refuses((x) => x.file("ui/x", new Uint8Array(u.MAX_UI_FILE + 1))),
      duplicate: refuses((x) => { x.file("ui/x", bytes("x")); x.file("ui/x", bytes("x")); }),
      underAFile: refuses((x) => { x.file("ui/x", bytes("x")); x.dir("ui/x/y"); }),
      notBytes: refuses((x) => x.file("ui/x", "x")),
      afterClose: refuses((x) => { x.close(); x.dir("ui"); }),
    };
    // After a refusal the writer is done: half an archive may not go on to
    // look like a whole one.
    const dead = new u.UIWriter();
    try { dead.dir("x"); } catch { /* the refusal that kills it */ }
    let goesOn;
    try { dead.dir("ui"); goesOn = true; } catch { goesOn = false; }

    // The writer copies the bytes it is handed: rewriting them afterwards
    // cannot change the archive it already wrote.
    const mine = new Uint8Array(4).fill(9);
    const c = new u.UIWriter();
    c.file("ui/c.bin", mine);
    mine.fill(0x41);
    const copied = [...u.walkUI(c.close())[0].data];

    return { roundTrip, bigIsWhole, refused, goesOn, copied };
  });

  expect(out.roundTrip).toEqual([
    "ui/index.html:15",
    "ui/big.bin:1543",
    "ui/" + "p".repeat(150) + ":4",
    "ui/empty:0",
  ]);
  expect(out.bigIsWhole).toBe(true);
  expect(out.refused).toEqual({
    badName: true, fileNamedUI: true, dirOutsideUI: true, tooLarge: true,
    duplicate: true, underAFile: true, notBytes: true, afterClose: true,
  });
  expect(out.goesOn).toBe(false);
  expect(out.copied).toEqual([9, 9, 9, 9]);
});

// The optional hash, and why it exists at all: pack.packageUI reads a package
// of up to 4 GiB as a stream and may keep only the ui-tar, so the UI hash has
// to grow with the archive instead of being taken from it afterwards. Go has
// it in the same place - store.PackageUI hands NewUIWriter an io.MultiWriter
// of the output and a sha256.
//
// What is held here is the one property that makes it worth having: THE HASH
// IS THE ARCHIVE. Not "a hash of roughly the same bytes" - the same digest,
// down to the byte, as hashing what close hands out. Anything less and the
// shell would sign a number nobody can check.
test("the hash a writer fills is the archive it hands out", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const u = await import("/assets/js/contracts/uitar.js");
    const { sha256 } = await import("/assets/vendor/noble-hashes/sha2.js");
    const { b64encode, equal } = await import("/assets/js/contracts/b64.js");
    const enc = new TextEncoder();
    const long = "ui/" + "d".repeat(99) + "/" + "f".repeat(43) + ".css";

    // Every shape the writer can put into an archive: nothing at all, a file,
    // a directory, a file whose data is not a whole block, and a name too long
    // for the header field, which costs a PAX block.
    const shapes = {
      empty: [],
      one: [["file", "ui/index.html", "<!doctype html>"]],
      dirsAndFiles: [["dir", "ui"], ["file", "ui/index.html", "x"], ["dir", "ui/css"],
        ["file", "ui/css/app.css", "body{color:#191919}"]],
      exactBlock: [["file", "ui/b.bin", "y".repeat(512)]],
      longName: [["file", "ui/index.html", "x"], ["file", long, "a{}"]],
    };
    const res = {};
    for (const [name, steps] of Object.entries(shapes)) {
      // Once with a hash, once without, and the two archives compared.
      const h = sha256.create();
      const withHash = new u.UIWriter();
      const plain = new u.UIWriter();
      for (const [kind, path, data] of steps) {
        if (kind === "dir") { withHash.dir(path, h); plain.dir(path); }
        else { withHash.file(path, enc.encode(data), h); plain.file(path, enc.encode(data)); }
      }
      const archive = withHash.close(h);
      const old = plain.close();
      res[name] = {
        // The archive is unchanged by being hashed as it is written.
        sameArchive: equal(archive, old),
        // And the running hash is the digest of that archive.
        sameDigest: b64encode(h.digest())
          === b64encode(new Uint8Array(await crypto.subtle.digest("SHA-256", archive))),
        // The reader still takes it, so this is a ui-tar and not just bytes.
        reads: (() => { try { u.walkUI(archive); return true; } catch { return false; } })(),
      };
    }

    const refused = (make) => { try { make(); return false; } catch { return true; } };
    return {
      shapes: res,
      // Half a hash is no hash: whichever call names one first binds the
      // writer, and a later call that forgets it or names another is refused.
      forgotAtClose: refused(() => {
        const w = new u.UIWriter();
        w.file("ui/a", enc.encode("x"), sha256.create());
        w.close();
      }),
      forgotAtFile: refused(() => {
        const w = new u.UIWriter();
        w.file("ui/a", enc.encode("x"));
        w.close(sha256.create());
      }),
      twoHashes: refused(() => {
        const w = new u.UIWriter();
        w.file("ui/a", enc.encode("x"), sha256.create());
        w.file("ui/b", enc.encode("y"), sha256.create());
      }),
      notAHash: refused(() => new u.UIWriter().close({})),
      // An entry the writer refused wrote nothing, so it must weigh nothing
      // either. Two hashes side by side, because a refusal poisons the writer
      // and no archive can be finished afterwards to compare - which is the
      // documented behaviour and not a gap: half an archive may not go on to
      // look like a whole one.
      refusedEntryIsNotHashed: (() => {
        const h1 = sha256.create(), h2 = sha256.create();
        const w1 = new u.UIWriter();
        w1.file("ui/index.html", enc.encode("x"), h1);
        try { w1.file("/etc/passwd", enc.encode("bad"), h1); } catch { /* refused */ }
        const w2 = new u.UIWriter();
        w2.file("ui/index.html", enc.encode("x"), h2);
        return b64encode(h1.digest()) === b64encode(h2.digest());
      })(),
      // And the writer really is done after one: close is refused too.
      refusalPoisons: refused(() => {
        const h = sha256.create();
        const w = new u.UIWriter();
        try { w.file("/etc/passwd", enc.encode("bad"), h); } catch { /* refused */ }
        w.close(h);
      }),
    };
  });
  for (const [name, r] of Object.entries(out.shapes)) {
    expect(r, name).toEqual({ sameArchive: true, sameDigest: true, reads: true });
  }
  expect(out.forgotAtClose).toBe(true);
  expect(out.forgotAtFile).toBe(true);
  expect(out.twoHashes).toBe(true);
  expect(out.notAHash).toBe(true);
  expect(out.refusedEntryIsNotHashed).toBe(true);
  expect(out.refusalPoisons).toBe(true);
});
