// An app package as an admin uploads one: a ustar archive with manifest.json
// first and the app's ui/ files behind it, which is the order store.Walk and
// contracts/pack.js read.
//
// It is built HERE, in node, and handed to the file chooser as bytes. The
// shell then reads it with pack.packageUI - the real reader, on a real
// archive - so what a case measures is the shell's own arithmetic and not a
// number a fixture whispered to it.
//
// The header layout is the one contracts/uitar.js writes: the same fields,
// the same widths, the same checksum over eight spaces. Two writers of one
// format can drift apart, so nothing here is guessed - a package this file
// builds is read by pack.js without a word, or the upload cases go red.

const BLOCK = 512;

function put(b, off, s) {
  for (let i = 0; i < s.length; i++) b[off + i] = s.charCodeAt(i);
}

function octalField(v, width) {
  const s = v.toString(8);
  if (s.length > width - 1) throw new Error("pkg: octal field");
  return "0".repeat(width - 1 - s.length) + s + "\u0000";
}

function headerSum(b) {
  let sum = 8 * 0x20;
  for (let i = 0; i < BLOCK; i++) if (i < 148 || i >= 156) sum += b[i];
  return sum;
}

// header builds one block: typeflag flag, the entry's name and its size.
function header(flag, name, size) {
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

function entry(parts, name, data) {
  parts.push(header(0x30, name, data.length), data);
  const pad = (BLOCK - (data.length % BLOCK)) % BLOCK;
  if (pad > 0) parts.push(new Uint8Array(pad));
}

// manifest fills in every member store.ParseManifest wants, so a case has to
// name only what it is about. The defaults are an app with a window and no
// service: the shortest package that is a package.
export function manifest(o) {
  return {
    id: "de.example.notizen",
    name: "Notizen",
    version: "1.2.0",
    publisher: "Beispiel GmbH",
    description: "Notizen schreiben und wiederfinden.",
    icon: "",
    ui: { entry: "index.html", width: 600, height: 420 },
    actions: [],
    services: [],
    start: "",
    runtime: "",
    needs: [],
    cpu: 0,
    memory: 0,
    minNgc: "",
    ...o,
  };
}

// buildPackage writes the archive: manifest.json, the directory ui/ and the
// files under it, and a service image where the manifest names services.
// It hands back bytes, because a file chooser takes bytes.
//
// The image is not optional: store.Walk refuses a package whose manifest
// names services and whose archive carries no image, and one that carries an
// image for no service - "manifest names what is missing" in pack.js. A
// fixture that got that wrong would fail in the reader and look like a bug
// in the shell.
export function buildPackage(m, files) {
  const enc = new TextEncoder();
  const ui = files ?? (m.ui === null ? {} : { "ui/index.html": "<!DOCTYPE html><title>" + m.name + "</title>" });
  const parts = [];
  // A member that is missing is not the same as one that stands there as
  // null: strict JSON has no null at all (enc.ErrNull, json.js the same), and
  // a manifest with "ui":null in it is refused before anything is read. An
  // app without a window leaves the member out.
  entry(parts, "manifest.json", enc.encode(JSON.stringify(m, (k, v) => (v === null ? undefined : v))));
  if (Object.keys(ui).length > 0) parts.push(header(0x35, "ui/", 0));
  for (const [name, body] of Object.entries(ui)) {
    entry(parts, name, typeof body === "string" ? enc.encode(body) : body);
  }
  if (m.services.length > 0) {
    parts.push(header(0x35, "services/", 0));
    entry(parts, "services/amd64.tar", enc.encode("ein Abbild, das hier nichts tut"));
  }
  // The end marker, two blocks of NUL bytes.
  parts.push(new Uint8Array(2 * BLOCK));
  let n = 0;
  for (const p of parts) n += p.length;
  const out = new Uint8Array(n);
  let o = 0;
  for (const p of parts) {
    out.set(p, o);
    o += p.length;
  }
  return out;
}
