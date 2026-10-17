// The language files themselves: the five have to carry the same keys, and
// every key the code asks for has to be in them.
//
// WHY THIS READS THE FILES AND NOT THE PAGE. The rule it holds is about the
// FOLDER - "all five files have the same keys; a test reports a missing one as
// an error" - and a browser only ever sees the one language it chose. A case
// that went through the page would prove nothing about the other four.
import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { test, expect } from "@playwright/test";

const WEB = fileURLToPath(new URL("../..", import.meta.url));
const LANG = WEB + "/lang";

// The five languages the rule asks for (#ml). They are named HERE and nowhere
// in the code: this is the test that the folder holds them, and a programme
// that read a list like this would be the very thing the rule forbids.
const WANTED = ["de", "en", "fr", "it", "es"];

function tags() {
  return readdirSync(LANG).filter((n) => n.endsWith(".json")).map((n) => n.slice(0, -5)).sort();
}

const tableOf = (tag) => JSON.parse(readFileSync(LANG + "/" + tag + ".json", "utf8"));

test("the folder holds the five languages", () => {
  expect(tags()).toEqual([...WANTED].sort());
});

test("all language files carry the same keys", () => {
  const found = tags();
  // The grounds of the measurement, named before anything is compared: a run
  // that found no file would agree with itself about nothing and still look
  // green (messen.md, "a run that hits no case still reports all is well").
  expect(found.length).toBeGreaterThan(1);
  const base = Object.keys(tableOf("de")).sort();
  expect(base.length).toBeGreaterThan(100);
  for (const tag of found) {
    const keys = Object.keys(tableOf(tag)).sort();
    const missing = base.filter((k) => !keys.includes(k));
    const extra = keys.filter((k) => !base.includes(k));
    // The key names are in the message, not just the count: a number says a
    // file is wrong, the names say which line to write.
    expect({ tag, missing, extra }).toEqual({ tag, missing: [], extra: [] });
  }
});

test("no language file has an empty text", () => {
  for (const tag of tags()) {
    const table = tableOf(tag);
    const empty = Object.entries(table).filter(([, v]) => typeof v !== "string" || v.trim() === "");
    expect({ tag, empty: empty.map(([k]) => k) }).toEqual({ tag, empty: [] });
  }
});

test("every placeholder of a text stands in all five languages", () => {
  const names = (s) => [...s.matchAll(/\{([A-Za-z0-9_]+)\}/g)].map((m) => m[1]).sort();
  const base = tableOf("de");
  for (const tag of tags()) {
    if (tag === "de") continue;
    const table = tableOf(tag);
    const off = [];
    for (const [key, text] of Object.entries(base)) {
      // A placeholder a translation dropped is a value the person never sees;
      // one it invented stays on screen as the four characters it is.
      if (names(text).join(",") !== names(table[key]).join(",")) off.push(key);
    }
    expect({ tag, off }).toEqual({ tag, off: [] });
  }
});

// Every key the code asks t() for has to be in the files, and the other way
// round: a key nobody asks for is dead weight in five files.
//
// It reads the sources rather than running them, because a key in a branch no
// case reaches would otherwise never be seen. What it can see is every t("…")
// with a literal key, which is all of them: a key built out of pieces is in
// exactly one place in NGC (codeText in lang.js, "wire." + code), and the
// fourteen it makes are checked by name below.
const SOURCES = [
  "public/assets/js", "classes", "templates", "public/index.php",
];

function sourceFiles(rel, out = []) {
  const path = WEB + "/" + rel;
  let entries;
  try {
    entries = readdirSync(path, { withFileTypes: true });
  } catch {
    // A file, not a folder.
    out.push(rel);
    return out;
  }
  for (const e of entries) {
    if (e.name === "vendor" || e.name === "node_modules") continue;
    if (e.isDirectory()) sourceFiles(rel + "/" + e.name, out);
    else if (e.name.endsWith(".js") || e.name.endsWith(".php")) out.push(rel + "/" + e.name);
  }
  return out;
}

function askedFor() {
  const keys = new Set();
  const files = SOURCES.flatMap((s) => sourceFiles(s));
  expect(files.length).toBeGreaterThan(20);
  for (const rel of files) {
    const src = readFileSync(WEB + "/" + rel, "utf8");
    for (const m of src.matchAll(/\bt\(\s*"([^"]+)"/g)) keys.add(m[1]);
    for (const m of src.matchAll(/Lang::t\(\s*'([^']+)'/g)) keys.add(m[1]);
  }
  // Keys that are NAMED but not handed straight to t(): the accent colours
  // stand in a list that is mapped through t(), and the confirmation dialog's
  // two reasons stand in a table that is read by the name of the operation. A
  // key spelled out in a source file is asked for, however it then travels, so
  // any literal that is a key of the files counts.
  const named = new Set(Object.keys(tableOf("de")));
  for (const rel of files) {
    const src = readFileSync(WEB + "/" + rel, "utf8");
    for (const m of src.matchAll(/"([^"\n]+)"|'([^'\n]+)'/g)) {
      const lit = m[1] ?? m[2];
      if (named.has(lit)) keys.add(lit);
    }
  }
  // A key that ends in a dot is the literal HALF of a built one - codeText in
  // lang.js asks t("wire." + code) - and no file holds it. The fourteen codes
  // it makes are accounted for by name in the case below.
  return new Set([...keys].filter((k) => !k.endsWith(".")));
}

test("every key the code asks for is in the files", () => {
  const table = tableOf("de");
  const missing = [...askedFor()].filter((k) => !Object.prototype.hasOwnProperty.call(table, k)).sort();
  expect(missing).toEqual([]);
});

test("every key in the files is asked for somewhere", () => {
  const table = tableOf("de");
  const asked = askedFor();
  // FOUR GROUPS OF KEYS ARE NOT NAMED BY A LITERAL IN ANY SOURCE LINE, and
  // each one is accounted for here rather than quietly let through:
  //
  //   wire.*        the fourteen codes of the contract plus wire.unknown, all
  //                 reached through codeText(code) - "wire." + the code. This
  //                 is the ONE built key in NGC, and the count is pinned.
  //   confirm.why.* the confirmation dialog looks its sentence up by the name
  //                 of the operation ("task"/"list"), through the WHY table.
  //   users.who.*   one of two keys, picked by whether one person or several
  //                 hold the role.
  //   boot.nolang   the one sentence that can be said when the files are not
  //                 there, so neither t() nor Lang::t() names it: both of them
  //                 fall back to it.
  const wire = Object.keys(table).filter((k) => k.startsWith("wire."));
  expect(wire.length).toBe(15);
  const built = [...wire, "confirm.why.task", "confirm.why.list",
    "users.who.one", "users.who.many", "boot.nolang"];
  for (const k of built) {
    // Each one has to BE in the files - a name in this list that is not a key
    // would make the list a way to excuse a key that is simply gone.
    expect({ key: k, there: Object.prototype.hasOwnProperty.call(table, k) })
      .toEqual({ key: k, there: true });
    asked.add(k);
  }
  // The tables keyed by the contract's own constants: the accent colours, the
  // eight update states, the three app states, the three update modes and the
  // ten kinds of log entry. Each is a list of key names mapped through t(), so
  // the keys DO stand in the source - this is here because a reader of this
  // case should not have to work that out.
  const byTable = Object.keys(table).filter((k) => k.startsWith("accent.")
    || k.startsWith("store.update.") || k.startsWith("store.state.")
    || k.startsWith("store.mode.") || k.startsWith("users.kind."));
  expect(byTable.length).toBeGreaterThan(20);
  const dead = Object.keys(table).filter((k) => !asked.has(k)).sort();
  expect(dead).toEqual([]);
});
