import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("packui.json");

test("the shell computes the UI hash of a package itself", async ({ shell }) => {
  test.setTimeout(120_000);
  const got = await shell.evaluate(async (cases) => {
    const p = await import("/assets/js/contracts/pack.js");
    const { b64decode, b64encode } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of cases) {
      const pkg = b64decode(c.package);
      const stream = new Blob([pkg]).stream();
      const r = await p.packageUI(stream);
      out.push({
        ui: b64encode(r.uiTar), hash: b64encode(r.sha256),
        pkgHash: b64encode(r.packageSha256) === b64encode(
          new Uint8Array(await crypto.subtle.digest("SHA-256", pkg))),
        id: r.manifest.id,
      });
    }
    return out;
  }, v);
  expect(got.map((g) => g.ui)).toEqual(v.map((c) => c.ui));
  expect(got.map((g) => g.hash)).toEqual(v.map((c) => c.hash));
  expect(got.every((g) => g.pkgHash)).toBe(true);
});

// Each hostile entry is the LAST of a package that would otherwise be taken,
// and both halves of that had to be measured before this case said anything.
//
//   - With the hostile entry alone, all five were refused before the reader
//     ever looked at a typeflag or a path: a package whose first entry is not
//     manifest.json is gone at that one rule. Taking the refusal of the
//     typeflag out of pack.js left the run GREEN with "symlink: true" in it.
//   - With a manifest in front but no ui/index.html behind, they were refused
//     at the LAST rule instead - the manifest names an entry the package does
//     not hold - and the same edit was still green.
//
// So the frame has to be a package that is accepted, and it is asserted here
// as frameIsFine. Only then does a refusal belong to the entry that was added.
test("a package with a link, an absolute path or a sparse entry is refused", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const p = await import("/assets/js/contracts/pack.js");
    const enc = new TextEncoder();
    const manifest = {
      name: "manifest.json", flag: 0x30,
      data: enc.encode(JSON.stringify({
        id: "org.sorglos.bad", name: "Bad", publisher: "Sorglos", version: "1.0.0",
        ui: { entry: "index.html" },
      })),
    };
    const index = { name: "ui/index.html", flag: 0x30, data: enc.encode("<!doctype html>") };
    const bad = async (make) => {
      try { await p.packageUI(new Blob([make()]).stream()); return false; } catch { return true; }
    };
    const frameIsFine = !(await bad(() => p.testTar([manifest, index])));
    const after = (...e) => p.testTar([manifest, index, ...e]);
    // One PAX record, "<length> key=value\n", the length counting itself.
    const record = (kv) => {
      const s = " " + kv + "\n";
      let n = s.length + 1;
      while (String(n).length + s.length !== n) n = String(n).length + s.length;
      return enc.encode(String(n) + s);
    };
    return {
      frameIsFine,
      symlink: await bad(() => after({ name: "ui/x", flag: 0x32, size: 0, link: "/etc/shadow" })),
      absolute: await bad(() => after({ name: "/etc/passwd", flag: 0x30, size: 0 })),
      parent: await bad(() => after({ name: "ui/../x", flag: 0x30, size: 0 })),
      sparse: await bad(() => after({ name: "ui/x", flag: 0x53, size: 0 })),
      // The dangerous spelling of a sparse file, and the one no typeflag
      // catches: a plain '0' entry with a GNU.sparse record in front of it.
      // archive/tar reports such an entry as a regular file of its FULL
      // logical size and fills the holes itself, so a few blocks of archive
      // hand out gigabytes - which is why store.sparse refuses on the KEY.
      paxSparse: await bad(() => after(
        { name: "", flag: 0x78, data: record("GNU.sparse.major=1") },
        { name: "ui/x", flag: 0x30, size: 0 },
      )),
      noManifest: await bad(() => p.testTar([index])),
    };
  });
  expect(out).toEqual({
    frameIsFine: true,
    symlink: true, absolute: true, parent: true, sparse: true, paxSparse: true, noManifest: true,
  });
});

// The reason packageUI takes a stream and not bytes: a package may be 4 GiB,
// and a shell that holds 4 GiB to hash it has lost the machine before it ever
// answers. Holding nothing is not something a comment can promise, so it is
// measured here, deterministically and without asking the engine how much
// memory it uses.
//
// The source below hands out ONE buffer, refilled before every chunk. A reader
// that keeps a chunk instead of consuming it keeps a window onto that buffer,
// and the next refill rewrites what it kept: every retained chunk would end up
// holding the LAST chunk's bytes, and both hashes come out wrong. So this case
// is red for the whole family of "collect the chunks, join them at the end"
// readers, and green only for one that is finished with a chunk before it asks
// for the next. Measured on this very file: the same source read to the end
// and joined afterwards gives a package hash that is NOT the package's.
//
// highWaterMark 0 is what makes that a statement about the reader. A stream
// with the default strategy pulls ahead - it asks the source for the next
// chunk while the one before it is still in the queue - so the buffer would be
// rewritten before ANY reader could look at it, and the case would be red for
// every reader alike, which says nothing about this one. At 0 the source is
// asked only when a read is waiting, and then the refill lands exactly where
// the question is: after the chunk was handed over, before it is needed again.
//
// What it does not measure is the peak itself, in bytes - no engine tells a
// page that number: an ArrayBuffer's memory is external to the JS heap, so
// performance.memory cannot see it, and measureUserAgentSpecificMemory needs a
// cross-origin-isolated page, which this one deliberately is not. The peak was
// measured outside the browser instead, in node, where process.memoryUsage()
// reports arrayBuffers exactly; the number and how it was taken stand in the
// report of this task.
test("a package that arrives in many chunks is read once, and no chunk is kept", async ({ shell }) => {
  test.setTimeout(120_000);
  const out = await shell.evaluate(async () => {
    const p = await import("/assets/js/contracts/pack.js");
    const { b64encode } = await import("/assets/js/contracts/b64.js");

    // A package big enough to arrive in many chunks: manifest, one ui file and
    // a service image that is skipped rather than kept.
    const manifest = new TextEncoder().encode(JSON.stringify({
      id: "org.sorglos.chunks", name: "Chunks", publisher: "Sorglos", version: "1.0.0",
      ui: { entry: "index.html" }, services: [{ name: "api", command: ["/bin/true"] }],
    }));
    const IMAGE = 4 << 20;
    const image = new Uint8Array(IMAGE);
    for (let i = 0; i < IMAGE; i++) image[i] = (i * 31 + 7) & 0xff;
    const pkg = p.testTar([
      { name: "manifest.json", flag: 0x30, data: manifest },
      { name: "ui/index.html", flag: 0x30, data: new TextEncoder().encode("<!doctype html>") },
      { name: "services/amd64.tar", flag: 0x30, data: image },
    ]);

    // Once as one piece, which is the answer to compare against.
    const whole = await p.packageUI(new Blob([pkg]).stream());

    // And once through a source that refills one buffer over and over. Small
    // chunks on purpose: the reader has to cross them inside a header, inside
    // a file and inside the padding.
    const CHUNK = 1 << 16;
    const scratch = new Uint8Array(CHUNK);
    let off = 0, handed = 0;
    const stream = new ReadableStream({
      pull(c) {
        if (off >= pkg.length) { c.close(); return; }
        const n = Math.min(CHUNK, pkg.length - off);
        // The refill: whatever a reader kept of the chunk before now reads
        // these bytes instead of the ones it was given.
        scratch.fill(0xa5);
        scratch.set(pkg.subarray(off, off + n), 0);
        off += n;
        handed++;
        c.enqueue(scratch.subarray(0, n));
      },
    }, { highWaterMark: 0 });
    const piecewise = await p.packageUI(stream);

    // What a reader that kept the chunks would get out of the same source, so
    // that the comparisons above are known to be able to fail at all.
    const s2 = new Uint8Array(CHUNK);
    let off2 = 0;
    const kept = [];
    const r2 = new ReadableStream({
      pull(c) {
        if (off2 >= pkg.length) { c.close(); return; }
        const n = Math.min(CHUNK, pkg.length - off2);
        s2.fill(0xa5);
        s2.set(pkg.subarray(off2, off2 + n), 0);
        off2 += n;
        c.enqueue(s2.subarray(0, n));
      },
    }, { highWaterMark: 0 }).getReader();
    for (;;) { const { value, done } = await r2.read(); if (done) break; kept.push(value); }
    let total = 0;
    for (const k of kept) total += k.length;
    const joined = new Uint8Array(total);
    let at = 0;
    for (const k of kept) { joined.set(k, at); at += k.length; }
    const collector = b64encode(new Uint8Array(await crypto.subtle.digest("SHA-256", joined)));

    return {
      chunks: handed,
      sameUI: b64encode(whole.uiTar) === b64encode(piecewise.uiTar),
      sameHash: b64encode(whole.sha256) === b64encode(piecewise.sha256),
      samePackageHash: b64encode(whole.packageSha256) === b64encode(piecewise.packageSha256),
      collectorIsWrong: collector !== b64encode(whole.packageSha256),
      // The ui-tar is all that is kept of a package this size.
      kept: piecewise.uiTar.length,
      size: pkg.length,
      id: piecewise.manifest.id,
    };
  });
  // The package really did arrive in many pieces, so the readings above mean
  // something: without this a source that handed everything over at once would
  // pass the three comparisons and prove nothing.
  expect(out.chunks).toBeGreaterThan(60);
  // The source really does rewrite what it handed over, so the three
  // comparisons below are a question and not a formality.
  expect(out.collectorIsWrong).toBe(true);
  expect(out.sameUI).toBe(true);
  expect(out.sameHash).toBe(true);
  expect(out.samePackageHash).toBe(true);
  expect(out.id).toBe("org.sorglos.chunks");
  // What is held is the ui-tar, and the service image is not in it.
  expect(out.size).toBeGreaterThan(4 << 20);
  expect(out.kept).toBeLessThan(64 << 10);
});

// The mistake this whole strand is built around, at the one door the guard in
// bytes.spec.js cannot reach: packageUI is handed a STREAM, so the probe over
// every contract never sees the byte objects that come out of it. Those
// objects belong to whoever wrote the source, and a Uint8Array may define
// length - so a chunk can report one size to the hash and hold another.
//
// b64.bytesOf is the answer, and this case is what says so. It reads no
// property of the object: a copy of a typed array is made from the array's own
// internal length, which no getter can move. Everything after it - the bound,
// the parsing, the hash - reads that copy. So the number that was hashed is
// the number that was parsed, whatever the chunk claims.
test("a chunk that lies about its size decides nothing", async ({ shell }) => {
  test.setTimeout(120_000);
  const out = await shell.evaluate(async () => {
    const p = await import("/assets/js/contracts/pack.js");
    const { b64encode } = await import("/assets/js/contracts/b64.js");
    const enc = new TextEncoder();
    const pkg = p.testTar([
      { name: "manifest.json", flag: 0x30, data: enc.encode(JSON.stringify({
        id: "org.sorglos.liar", name: "Liar", publisher: "Sorglos", version: "1.0.0",
        ui: { entry: "index.html" },
      })) },
      { name: "ui/index.html", flag: 0x30, data: enc.encode("<!doctype html><p>x") },
      { name: "ui/app.css", flag: 0x30, data: enc.encode("body{color:#191919}") },
    ]);
    const honest = await p.packageUI(new Blob([pkg]).stream());

    // Every chunk says it is one byte shorter than it is. Nothing else about
    // it is unusual, and the bytes it really holds are the package's.
    class Liar extends Uint8Array {
      get length() { return super.length - 1; }
      get byteLength() { return super.byteLength - 1; }
    }
    const CHUNK = 512;
    let off = 0;
    const stream = new ReadableStream({
      pull(c) {
        if (off >= pkg.length) { c.close(); return; }
        const n = Math.min(CHUNK, pkg.length - off);
        c.enqueue(new Liar(pkg.slice(off, off + n).buffer));
        off += n;
      },
    }, { highWaterMark: 0 });
    const lied = await p.packageUI(stream);
    return {
      // A copy of a typed array is made from the length the engine keeps, not
      // from the one the object reports - said out loud, because the whole
      // case rests on it.
      copyIgnoresTheLie: new Uint8Array(new Liar(new Uint8Array(8).buffer)).length === 8,
      sameUI: b64encode(honest.uiTar) === b64encode(lied.uiTar),
      sameHash: b64encode(honest.sha256) === b64encode(lied.sha256),
      samePackageHash: b64encode(honest.packageSha256) === b64encode(lied.packageSha256),
      honestIsRight: b64encode(honest.packageSha256) === b64encode(
        new Uint8Array(await crypto.subtle.digest("SHA-256", pkg))),
    };
  });
  expect(out.copyIgnoresTheLie).toBe(true);
  expect(out.honestIsRight).toBe(true);
  expect(out.sameUI).toBe(true);
  expect(out.sameHash).toBe(true);
  expect(out.samePackageHash).toBe(true);
});

// Being STRICTER than Go is not the safe side here, and this case is the fence
// around that. store.Walk decides which entries a package may hold, but what a
// header IS archive/tar decides, and it reads more shapes than ngc-store pack
// writes. A reader that only understood the one shape in packui.json would
// refuse packages the API takes, and an admin would be told his package is
// broken by one half of NGC and fine by the other.
//
// The four below are the ones with a second spelling. The USTAR prefix field
// is already covered by the vectors ("long file name"), so it is not repeated.
test("the header shapes archive/tar reads are read here too", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const p = await import("/assets/js/contracts/pack.js");
    const enc = new TextEncoder();
    const manifest = {
      name: "manifest.json", flag: 0x30,
      data: enc.encode(JSON.stringify({
        id: "org.sorglos.shapes", name: "Shapes", publisher: "Sorglos", version: "1.0.0",
        ui: { entry: "index.html" }, services: [{ name: "s", command: ["/bin/true"] }],
      })),
    };
    const index = { name: "ui/index.html", flag: 0x30, data: enc.encode("<!doctype html>") };
    const image = { name: "services/amd64.tar", flag: 0x30, data: new Uint8Array(1536).fill(7) };
    // A name of 150 bytes, too long for the 100-byte field.
    const long = "ui/" + "d".repeat(99) + "/" + "f".repeat(43) + ".css";
    const record = (kv) => {
      const s = " " + kv + "\n";
      let n = s.length + 1;
      while (String(n).length + s.length !== n) n = String(n).length + s.length;
      return enc.encode(String(n) + s);
    };
    const took = async (bytes) => {
      try { await p.packageUI(new Blob([bytes]).stream()); return true; } catch { return false; }
    };
    // Finds a header by its name field and hands it over for editing.
    const headerOf = (pkg, name) => {
      for (let i = 0; i + 512 <= pkg.length; i += 512) {
        const f = pkg.subarray(i, i + 100);
        const z = f.indexOf(0);
        if (String.fromCharCode(...f.subarray(0, z < 0 ? 100 : z)) === name) return pkg.subarray(i, i + 512);
      }
      throw new Error("no header " + name);
    };
    const reChecksum = (h) => {
      let sum = 8 * 0x20;
      for (let i = 0; i < 512; i++) if (i < 148 || i >= 156) sum += h[i];
      const f = sum.toString(8).padStart(6, "0") + "\u0000 ";
      for (let i = 0; i < 8; i++) h[148 + i] = f.charCodeAt(i);
    };

    // A size written base-256, which is the only way a size above 8 GiB is
    // written at all - and the reason the size field is not read as octal
    // without looking.
    const b256 = p.testTar([manifest, index, image]);
    const ih = headerOf(b256, "services/amd64.tar");
    ih.fill(0, 124, 136);
    ih[124] = 0x80;
    let v = image.data.length;
    for (let k = 135; k >= 125 && v > 0; k--) { ih[k] = v & 0xff; v = Math.floor(v / 256); }
    reChecksum(ih);

    // A header with no magic at all: V7, which Go still reads.
    const v7 = p.testTar([manifest, index, image]);
    const vh = headerOf(v7, "ui/index.html");
    vh.fill(0, 257, 265);
    reChecksum(vh);

    return {
      gnuLongName: await took(p.testTar([manifest, index, image,
        { name: "././@LongLink", flag: 0x4c, data: enc.encode(long + "\u0000") },
        { name: "ui/short.css", flag: 0x30, data: enc.encode("a{}") }])),
      paxPath: await took(p.testTar([manifest, index, image,
        { name: "", flag: 0x78, data: record("path=" + long) },
        { name: "", flag: 0x30, data: enc.encode("b{}") }])),
      base256Size: await took(b256),
      v7Header: await took(v7),
      // And the control: the same package with an octal size is taken too, so
      // the three above are about the spelling and not about the package.
      plain: await took(p.testTar([manifest, index, image])),
    };
  });
  expect(out).toEqual({
    gnuLongName: true, paxPath: true, base256Size: true, v7Header: true, plain: true,
  });
});
