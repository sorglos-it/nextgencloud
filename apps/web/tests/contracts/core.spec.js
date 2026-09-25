import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const core = vectors("core.json");
const ui = vectors("uiproof.json");

// ---------------------------------------------------------- the Go side

// The Go source of this contract, read out of shared/ itself.
//
// A test that pinned the JS table against a copy of Go's numbers would only
// ever pin the copy: both sides would have to be changed by the same hand, and
// the day one of them is not, nothing goes red. So the numbers are not written
// down here at all. Everything below is read out of shared/, which is frozen
// and belongs to the coordinator, and the moment it changes these tests change
// with it.
//
// This is the measurement that found the fourth plan-against-Go difference of
// this round: the task brief had maxIn 0 in both transfer rows where core.go
// has maxTransferIn, 4303355908. It was a throwaway script then; it is a test
// now, for the reason the round after it gave - a measurement without a test
// is gone with the session that made it.
const goSource = (path) =>
  readFileSync(fileURLToPath(new URL("../../../../shared/" + path, import.meta.url)), "utf8");

// Which file holds which Go package: core.go and results.go are both package
// core, so a bare name in the table resolves against both.
const GO = [
  ["core", goSource("core/core.go")],
  ["core", goSource("core/results.go")],
  ["session", goSource("session/session.go")],
  ["store", goSource("store/catalog.go")],
  ["trust", goSource("trust/trust.go")],
];
const CORE_GO = GO[0][1];

// Every constant of those files, as the text of its expression. Only const
// declarations are read - a "x = y" inside a function is no constant and must
// not turn up under that name.
const GO_CONSTS = new Map();
for (const [pkg, src] of GO) {
  const blocks = [...src.matchAll(/^const\s*\(\r?\n([\s\S]*?)^\)/gm)].map((m) => m[1]);
  for (const m of src.matchAll(/^const\s+(\w+)(?:\s+[A-Za-z_][\w.]*)?\s*=\s*(.+)$/gm)) {
    blocks.push(`${m[1]} = ${m[2]}`);
  }
  for (const block of blocks) {
    for (const line of block.split("\n")) {
      const m = /^\s*([A-Za-z_]\w*)\s+(?:[A-Za-z_]\w*\s+)?=\s*(.+?)\s*$/.exec(line.replace(/\/\/.*$/, ""));
      if (m) GO_CONSTS.set(`${pkg}.${m[1]}`, m[2].trim());
    }
  }
}

// goValue resolves one Go expression of the forms this contract uses: a
// string, a whole number, a shift, a sum, and a name of the same package or of
// another. Anything else throws instead of guessing, so an expression nobody
// taught it names itself in the failure rather than passing as something else.
function goValue(expr, pkg = "core", seen = new Set()) {
  const text = expr.trim();
  const quoted = /^"((?:[^"\\]|\\.)*)"$/.exec(text);
  if (quoted) return JSON.parse(text);
  if (text === "true") return true;
  if (text === "false") return false;
  if (text === "nil") return null;
  // A sum first, because << binds tighter than + in Go.
  if (text.includes("+")) {
    return text.split("+").reduce((sum, term) => sum + goValue(term, pkg, seen), 0);
  }
  if (text.includes("<<")) {
    const [a, b] = text.split("<<");
    return goValue(a, pkg, seen) * Math.pow(2, goValue(b, pkg, seen));
  }
  if (text.includes("*")) {
    return text.split("*").reduce((product, term) => product * goValue(term, pkg, seen), 1);
  }
  if (/^[0-9]+$/.test(text)) return Number(text);
  if (/^[A-Za-z_][\w.]*$/.test(text)) {
    const key = text.includes(".") ? text : `${pkg}.${text}`;
    if (seen.has(key)) throw new Error(`shared/: ${key} is defined in a circle`);
    const next = GO_CONSTS.get(key);
    if (next === undefined) throw new Error(`shared/: no constant ${key}`);
    return goValue(next, key.split(".")[0], new Set([...seen, key]));
  }
  throw new Error(`shared/: this test cannot read the expression ${text}`);
}

// The action table of shared/core/core.go, cell by cell. The params column is
// skipped: it is a Go constructor and has no value the shell could hold.
function goActions() {
  const start = CORE_GO.indexOf("var actions = []Action{");
  const block = CORE_GO.slice(start, CORE_GO.indexOf("\n}\n", start));
  const rows = [];
  for (const line of block.split("\n")) {
    const t = line.trim();
    if (!t.startsWith("{")) continue;
    const cells = t.replace(/^\{/, "").replace(/\},?$/, "").split(",").map((c) => c.trim());
    expect(cells.length, "a row of core.go's table has nine cells").toBe(9);
    const [app, service, name, admin, root, changes, flow, , maxIn] = cells;
    rows.push({
      app: goValue(app), service: goValue(service), name: goValue(name),
      admin: goValue(admin), root: goValue(root), changes: goValue(changes),
      flow: goValue(flow), maxIn: goValue(maxIn),
    });
  }
  return rows;
}

// The members of a Go struct, in the order json.Marshal writes them, with the
// ones it leaves out when they are empty.
function goStructFields(name) {
  const start = CORE_GO.indexOf(`type ${name} struct {`);
  expect(start, `shared/core/core.go holds ${name}`).toBeGreaterThan(-1);
  const block = CORE_GO.slice(start, CORE_GO.indexOf("\n}", start));
  const fields = [];
  for (const line of block.split("\n")) {
    const m = /`json:"([^"]+)"`/.exec(line);
    if (!m) continue;
    const [member, ...options] = m[1].split(",");
    fields.push({ name: member, optional: options.includes("omitempty") || options.includes("omitzero") });
  }
  return fields;
}

// The parameter types of the contract are the ones that carry Go's Params
// interface, which is one method: a new one in core.go turns the test below
// red until it is covered here too.
const GO_PARAM_TYPES = [...CORE_GO.matchAll(/^func \(p \*(\w+)\) Validate\(\) error/gm)].map((m) => m[1]);

const GO_ACTIONS = goActions();

// Does the Web Crypto API of this engine know Ed25519? The UI proofs need it
// twice over: trust.parseList imports every passkey of this list as spki, and
// verifying a catalog, a certificate and a task is three Ed25519 signatures.
// The WebKit that Playwright brings answers NotSupportedError to importKey, so
// parseList refuses the list before the first proof is looked at ("trust:
// invalid list", measured) - which is the right answer for an engine that
// cannot check, and no measurement of this contract. Same probe as
// session.spec.js and sign.spec.js, with a real key out of this vector's own
// list, so an engine that has Ed25519 but turns down a made-up 32-byte value
// is not skipped for the wrong reason.
//
// The other two tests read documents and check no signature; they run
// everywhere.
async function hasEd25519(shell, signed) {
  return shell.evaluate(async (s) => {
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const list = JSON.parse(new TextDecoder().decode(b64decode(s.payload)));
    let pub = null;
    for (const u of list.users) {
      for (const k of u.keys) {
        if (k.kind === "password") pub = k.pub;
      }
    }
    // Outside the try: a vector file that lost its password key must fail
    // loudly, not skip the test as "no Ed25519 here" on an engine that has it.
    if (typeof pub !== "string") throw new Error("uiproof.json's trust list holds no password key to probe with");
    try {
      await crypto.subtle.importKey("raw", b64decode(pub), { name: "Ed25519" }, false, ["verify"]);
      return true;
    } catch {
      return false;
    }
  }, signed);
}

test("every example result of core.json is read", async ({ shell }) => {
  const got = await shell.evaluate(async (d) => {
    const c = await import("/assets/js/contracts/core.js");
    const out = {};
    for (const [key, value] of Object.entries(d)) {
      if (key === "empty" || key === "notice") continue;
      const [app, name] = key.split("/");
      const action = c.lookupByKey(app, name);
      const bytes = new TextEncoder().encode(JSON.stringify(value));
      try {
        c.parseResult(action, bytes);
        out[key] = true;
      } catch (e) {
        out[key] = String(e);
      }
    }
    // Empty lists arrive as [], never as null.
    for (const [key, value] of Object.entries(d.empty)) {
      const [app, name] = key.split("/");
      const action = c.lookupByKey(app, name);
      const parsed = c.parseResult(action, new TextEncoder().encode(JSON.stringify(value)));
      out["empty:" + key] = Array.isArray(parsed) && parsed.length === 0;
    }
    out.notice = !!c.parseNotice(new TextEncoder().encode(JSON.stringify(d.notice)));
    return out;
  }, core);
  for (const [key, value] of Object.entries(got)) expect(value, key).toBe(true);
});

test("every UI proof of uiproof.json is decided as Go decides it", async ({ shell }) => {
  test.setTimeout(180_000);
  test.skip(!(await hasEd25519(shell, ui.trust)),
    "no Ed25519 in this engine's Web Crypto: it reads none of this list's passkeys and checks no signature");
  const got = await shell.evaluate(async (d) => {
    const c = await import("/assets/js/contracts/core.js");
    const t = await import("/assets/js/contracts/trust.js");
    const { b64decode, b64encode } = await import("/assets/js/contracts/b64.js");
    const list = await t.parseList(b64decode(d.trust.payload));
    const out = [];
    for (const k of d.cases) {
      try {
        const p = c.parseUIProof(b64decode(k.proof));
        const hash = await c.verifyUIProof(p, k.app, k.version, list);
        out.push({ valid: true, ui: b64encode(hash) });
      } catch {
        out.push({ valid: false, ui: "" });
      }
    }
    return out;
  }, ui);
  const wrong = ui.cases.filter((c, i) => String(c.valid) !== String(got[i].valid)
    || (String(c.valid) === "true" && c.ui !== got[i].ui)).map((c) => c.name);
  expect(wrong).toEqual([]);
});

test("a result over MaxCallResult is refused", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const c = await import("/assets/js/contracts/core.js");
    const w = await import("/assets/js/contracts/wire.js");
    const action = c.lookupByKey("ngc.desktop", "me");
    const big = new Uint8Array(w.MAX_CALL_RESULT + 1).fill(0x20);
    big[0] = 0x7b; big[big.length - 1] = 0x7d;
    try { c.parseResult(action, big); return false; } catch { return true; }
  });
  expect(out).toBe(true);
});

// ------------------------------------ the eight exports the three above miss

test("the action table is the one shared/core/core.go holds, cell by cell", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const c = await import("/assets/js/contracts/core.js");
    return c.ACTIONS.map((a) => ({
      app: a.app, service: a.service, name: a.name,
      admin: a.admin, root: a.root, changes: a.changes, flow: a.flow, maxIn: a.maxIn,
    }));
  });
  expect(got).toEqual(GO_ACTIONS);
  // And the names the shell reaches those rows by. Not matched against Go's
  // constant NAMES - the two languages spell them differently (ACT_UI_PROOF
  // against ActUIProof), and a mapping by hand would be one more thing to keep
  // in step. Matched against the table instead: the exported names are exactly
  // the apps, services and actions that stand in it, no more and no fewer. A
  // constant with a value nobody has a row for is as much a mistake as a row
  // the shell cannot name.
  const names = await shell.evaluate(async () => {
    const c = await import("/assets/js/contracts/core.js");
    const of = (prefix) =>
      [...new Set(Object.entries(c).filter(([k]) => k.startsWith(prefix)).map(([, v]) => v))].sort();
    return { act: of("ACT_"), app: of("APP_"), service: of("SERVICE_"), use: c.RIGHT_USE, local: c.LOCAL_STORE };
  });
  const inTable = (of) => [...new Set(GO_ACTIONS.map(of))].sort();
  expect(names.act).toEqual(inTable((a) => a.name));
  expect(names.app).toEqual(inTable((a) => a.app));
  expect(names.service).toEqual(inTable((a) => a.service));
  expect(names.use).toBe(goValue(GO_CONSTS.get("core.RightUse")));
  expect(names.local).toBe(goValue(GO_CONSTS.get("core.LocalStore")));
});

test("the limits and the result table are Go's, and the table cannot be written over", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const c = await import("/assets/js/contracts/core.js");
    // Whoever else runs on the page must not be able to add an action nobody
    // wrote, or clear the admin flag of one that stands there.
    let pushed = false, changed = false;
    try {
      c.ACTIONS.push({ app: "ngc.store", service: "api", name: "mine", admin: false });
      pushed = true;
    } catch { /* frozen, as it must be */ }
    try {
      c.ACTIONS[0].admin = true;
      changed = c.ACTIONS[0].admin === true;
    } catch { /* frozen, as it must be */ }
    return {
      maxNotice: c.MAX_NOTICE, maxTransferHeader: c.MAX_TRANSFER_HEADER,
      rows: c.ACTIONS.length, pushed, changed,
      shapes: Object.keys(c.RESULT_SHAPES).sort(),
      // An action whose result has no form at all would be a result nobody reads.
      everyActionHasAShape: c.ACTIONS.every((a) => c.RESULT_SHAPES[`${a.app}/${a.name}`] !== undefined),
      // The forms are the contract's, so nothing may be hung into them from
      // outside: one added member decides what a parser reads.
      frozenShapes: Object.values(c.RESULT_SHAPES).every((s) => Object.isFrozen(s)),
    };
  });
  expect(got.maxNotice).toBe(goValue(GO_CONSTS.get("core.MaxNotice")));
  expect(got.maxTransferHeader).toBe(goValue(GO_CONSTS.get("core.MaxTransferHeader")));
  expect(got.rows).toBe(GO_ACTIONS.length);
  expect(got.pushed).toBe(false);
  expect(got.changed).toBe(false);
  expect(got.everyActionHasAShape).toBe(true);
  expect(got.frozenShapes).toBe(true);
  // One form per action key, and none for a key that is no action: the keys
  // come off Go's table and are not written down here.
  expect(got.shapes).toEqual([...new Set(GO_ACTIONS.map((a) => `${a.app}/${a.name}`))].sort());
});

test("shellOpens and critical decide for every row as Go decides", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const c = await import("/assets/js/contracts/core.js");
    const activate = c.lookup("ngc.store", "controller", "activate");
    const uninstall = c.lookup("ngc.store", "controller", "uninstall");
    return {
      rows: c.ACTIONS.map((a) => ({
        app: a.app, service: a.service, name: a.name,
        opens: c.shellOpens(a), critical: c.critical(a, null),
      })),
      // Go's TestCritical, case for case, with the params that decide.
      named: {
        "activate without grants": c.critical(activate, { grants: [] }),
        "activate with root": c.critical(activate, { grants: ["root"] }),
        "uninstall, keep data": c.critical(uninstall, { keepData: true }),
        "uninstall, delete data": c.critical(uninstall, {}),
        "upload": c.critical(c.lookup("ngc.store", "api", "upload"), null),
        "delete kept data": c.critical(c.lookup("ngc.store", "controller", "delete-data"), { app: "org.a.b" }),
        "list users": c.critical(c.lookup("ngc.users", "api", "list"), null),
      },
      // Neither answers with an exception for something that is no action.
      nothing: [c.shellOpens(null), c.shellOpens({}), c.critical(null, null), c.critical(undefined, {})],
      // lookup and lookupByKey: the api row first, the controller row where
      // there is no api row, and null for a name nobody has.
      lookup: {
        trust: c.lookupByKey("ngc.users", "trust")?.service,
        activate: c.lookupByKey("ngc.store", "activate")?.service,
        license: c.lookupByKey("ngc.store", "license")?.service,
        missing: c.lookupByKey("ngc.desktop", "nope"),
        noApp: c.lookup(null, "api", "me"),
        both: c.lookup("ngc.users", "controller", "trust")?.maxIn,
      },
    };
  });
  expect(got.named).toEqual({
    "activate without grants": false, "activate with root": true,
    "uninstall, keep data": false, "uninstall, delete data": true,
    "upload": true, "delete kept data": true, "list users": false,
  });
  expect(got.nothing).toEqual([false, false, false, false]);
  expect(got.lookup).toEqual({
    trust: "api", activate: "controller", license: "api", missing: null, noApp: null,
    both: GO_ACTIONS.find((a) => a.app === "ngc.users" && a.service === "controller").maxIn,
  });
  // ShellOpens of Go is "service api, or a root task", and Critical without
  // params is "ngc.store upload or delete-data" - both read off Go's own table
  // rather than written out here.
  //
  // One row answers otherwise, and it is the one deliberate difference of this
  // contract: uninstall without params. Go reaches its default there and says
  // false, because its switch is on the TYPE of the params; this one switches
  // on the action and says true. Go never gets there - RuleFor refuses a
  // params-less activate or uninstall before Critical is asked - and an
  // uninstall whose keepData nobody named deletes the app's data. The safer
  // side is kept, and it is pinned HERE, in one place, so it cannot spread to
  // a second row unnoticed.
  const wrong = got.rows.filter((r, i) => {
    const go = GO_ACTIONS[i];
    const opens = go.service === "api" || go.root;
    const critical = go.name === "uninstall"
      ? true
      : go.app === "ngc.store" && (go.name === "upload" || go.name === "delete-data");
    return r.opens !== opens || r.critical !== critical;
  }).map((r) => `${r.app}/${r.service}/${r.name}`);
  expect(wrong).toEqual([]);
});

test("validRight takes what Go's ValidRight takes", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const c = await import("/assets/js/contracts/core.js");
    // Go's TestRights, its own two lists.
    const check = (list) => list.map((r) => c.validRight(r));
    return {
      good: check(["org.sorglos.terminal/use", "org.sorglos.terminal/shell"]),
      bad: check(["org.sorglos.terminal", "ngc.users/use", "org.sorglos.terminal/Shell", "/use"]),
      // Go is handed a string and can be handed nothing else.
      notText: check([null, undefined, 7, {}, ["org.a.b/use"]]),
    };
  });
  expect(got.good).toEqual([true, true]);
  expect(got.bad).toEqual([false, false, false, false]);
  expect(got.notText).toEqual([false, false, false, false, false]);
});

test("encodeParams writes the members Go's json.Marshal writes, in Go's order", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const c = await import("/assets/js/contracts/core.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const dec = new TextDecoder();
    const sha = b64decode("BQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQU");
    const ui = b64decode("CgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgo");
    // For each parameter type: the action that carries it, one value with
    // every member filled in, and one with nothing but the members Go writes
    // even when they are empty.
    const cases = {
      AppParams: ["ngc.desktop", "api", "ui-proof", { app: "org.example.notes" }, { app: "org.example.notes" }],
      CodeParams: ["ngc.desktop", "api", "totp-confirm", { code: "123456" }, { code: "123456" }],
      StoreParams: ["ngc.store", "api", "catalog", { store: "global" }, { store: "global" }],
      RoleRights: ["ngc.users", "api", "set-rights",
        { role: "user", rights: ["org.example.notes/use"] }, { role: "user" }],
      LogQuery: ["ngc.users", "api", "log",
        { beforeTime: 1790000000, beforeId: "000102030405060708090a0b0c0d0e0f", limit: 50, user: "ISEhISEhISEhISEhISEhIQ" },
        { limit: 50 }],
      UpdateSetting: ["ngc.store", "api", "set-update",
        { target: "ngc-web", mode: "auto", window: "02:00-04:00" }, { target: "ngc-web", mode: "auto" }],
      ActivateParams: ["ngc.store", "controller", "activate",
        { app: "org.example.notes", version: "0.1.0", sha256: sha, ui, store: "local", grants: ["root", "network"] },
        { app: "org.example.notes", version: "0.1.0", sha256: sha, ui, store: "local" }],
      UninstallParams: ["ngc.store", "controller", "uninstall",
        { app: "org.example.notes", keepData: true }, { app: "org.example.notes" }],
      StageParams: ["ngc.store", "controller", "stage",
        { target: "ngc-web", version: "1.1.0" }, { target: "ngc-web", version: "1.1.0" }],
      // The system takes neither a version nor a checksum: the controller runs
      // apt, and there is nothing to name.
      InstallParams: ["ngc.store", "controller", "install",
        { target: "ngc-web", version: "1.1.0", sha256: sha }, { target: "system" }],
    };
    const out = {};
    for (const [type, [app, service, name, full, minimal]] of Object.entries(cases)) {
      const a = c.lookup(app, service, name);
      out[type] = { full: dec.decode(c.encodeParams(a, full)), minimal: dec.decode(c.encodeParams(a, minimal)) };
    }
    // An action without parameters writes an empty object and takes nothing
    // else (Go: ParseParams with a nil Params).
    out.none = { full: dec.decode(c.encodeParams(c.lookup("ngc.desktop", "api", "me"), {})), minimal: "{}" };
    return out;
  });
  // A parameter type Go grows and nobody covers here turns this red.
  expect(Object.keys(got).filter((k) => k !== "none").sort()).toEqual([...GO_PARAM_TYPES].sort());
  for (const [type, docs] of Object.entries(got)) {
    if (type === "none") {
      expect(docs.full).toBe("{}");
      continue;
    }
    const fields = goStructFields(type);
    // The order is the document's own: JSON.parse keeps the members of an
    // object in the order they were written.
    expect(Object.keys(JSON.parse(docs.full)), `${type} with every member`).toEqual(fields.map((f) => f.name));
    expect(Object.keys(JSON.parse(docs.minimal)), `${type} with only what Go always writes`)
      .toEqual(fields.filter((f) => !f.optional).map((f) => f.name));
  }
});

test("encodeParams writes the very bytes Go marshalled into uiproof.json", async ({ shell }) => {
  const signed = ui.cases.find((c) => c.name === "uploaded app activated by the admin");
  expect(signed, "uiproof.json holds the admin's activation").toBeTruthy();
  // No skip for an engine without Ed25519: nothing here checks a signature.
  // parseUIProof and parseTask only read documents, so this runs everywhere.
  const got = await shell.evaluate(async (d) => {
    const c = await import("/assets/js/contracts/core.js");
    const t = await import("/assets/js/contracts/task.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const dec = new TextDecoder();
    // Go's own bytes: the params of a task Go marshalled and signed. A task
    // keeps its params raw, so these are the characters Go wrote and not a
    // second encoding of them.
    const proof = c.parseUIProof(b64decode(d.proof));
    const fromGo = dec.decode(t.parseTask(proof.task.payload).params);
    const v = JSON.parse(fromGo);
    const mine = c.encodeParams(c.lookup("ngc.store", "controller", "activate"), {
      app: v.app, version: v.version, sha256: b64decode(v.sha256), ui: b64decode(v.ui), store: v.store,
    });
    return { fromGo, mine: dec.decode(mine) };
  }, signed);
  expect(got.mine).toBe(got.fromGo);
});

test("encodeParams decides on the copy it writes, not on a second reading", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const c = await import("/assets/js/contracts/core.js");
    const { b64decode, b64encode } = await import("/assets/js/contracts/b64.js");
    const ui = b64decode("CgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgo");
    const activate = c.lookup("ngc.store", "controller", "activate");
    const params = (sha256) => ({ app: "org.example.notes", version: "0.1.0", sha256, ui, store: "local" });
    // A checksum whose own length says 32 on the first reading and the truth
    // afterwards. These bytes are signed inside a task; a check that reads the
    // caller's object and a writer that reads it again would sign a checksum
    // nobody checked.
    class Lying extends Uint8Array {
      #reads = 0;
      get length() { return this.#reads++ === 0 ? 32 : super.length; }
    }
    // A fresh one per call: the counter is what makes the first reading
    // differ from the second, so two calls must not share it. With a shared
    // object the second call would start at the truth and answer false for
    // the wrong reason - a test that passes whatever the contract does.
    const lying = () => params(new Lying(64).fill(7));
    const out = {};
    try {
      out.lying = new TextDecoder().decode(c.encodeParams(activate, lying()));
    } catch (e) {
      out.lying = String(e);
    }
    // THE assertion of this test: the copy is 64 bytes, so the check sees 64
    // and refuses. A contract that read the caller's object instead would see
    // the 32 of the first reading and answer true.
    out.lyingValid = c.validateParams(activate, lying());
    // The honest 64 bytes are refused for their real length, so the refusal
    // above is about the second reading and not about these bytes.
    out.plain64 = c.validateParams(activate, params(new Uint8Array(64).fill(7)));
    out.plain32 = c.validateParams(activate, params(new Uint8Array(32).fill(7)));
    // And an ordinary array rewritten after the call: what was checked is
    // already out of the caller's reach, so the document carries it.
    const moving = new Uint8Array(32).fill(1);
    const doc = c.encodeParams(activate, params(moving));
    moving.fill(9);
    out.written = JSON.parse(new TextDecoder().decode(doc)).sha256;
    out.checked = b64encode(new Uint8Array(32).fill(1));
    return out;
  });
  expect(got.lying).toBe("Error: core: invalid parameters");
  expect(got.lyingValid).toBe(false);
  expect(got.plain64).toBe(false);
  expect(got.plain32).toBe(true);
  expect(got.written).toBe(got.checked);
});

test("validateParams holds the rules of Go's Validate methods", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const c = await import("/assets/js/contracts/core.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const sha = b64decode("BQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQU");
    const ui = b64decode("CgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgo");
    const a = (app, service, name) => c.lookup(app, service, name);
    const activate = a("ngc.store", "controller", "activate");
    const log = a("ngc.users", "api", "log");
    const install = a("ngc.store", "controller", "install");
    const stage = a("ngc.store", "controller", "stage");
    const setUpdate = a("ngc.store", "api", "set-update");
    const rights = a("ngc.users", "api", "set-rights");
    const good = { app: "org.example.notes", version: "0.1.0", sha256: sha, ui, store: "local" };
    const named = {};
    const put = (name, action, params) => { named[name] = c.validateParams(action, params); };

    put("activate", activate, good);
    put("activate with both grants", activate, { ...good, grants: ["root", "network"] });
    put("activate of a core app", activate, { ...good, app: "ngc.store" });
    put("activate of no version", activate, { ...good, version: "1.0" });
    put("activate with a short checksum", activate, { ...good, sha256: sha.slice(0, 31) });
    put("activate with a short ui hash", activate, { ...good, ui: ui.slice(0, 31) });
    put("activate with a store that is no name", activate, { ...good, store: "Local" });
    put("activate with the same grant twice", activate, { ...good, grants: ["root", "root"] });
    put("activate with a grant nobody offers", activate, { ...good, grants: ["disk"] });
    put("activate without a checksum", activate, { ...good, sha256: undefined });

    put("log of 1", log, { limit: 1 });
    put("log of 500", log, { limit: 500 });
    put("log of 0", log, { limit: 0 });
    put("log of 501", log, { limit: 501 });
    put("log of half a page", log, { limit: 1.5 });
    put("log page", log, { limit: 5, beforeTime: 1790000000, beforeId: "000102030405060708090a0b0c0d0e0f" });
    put("log page without a time", log, { limit: 5, beforeId: "000102030405060708090a0b0c0d0e0f" });
    put("log page with an id that is none", log, { limit: 5, beforeTime: 1790000000, beforeId: "not-an-id" });
    put("log of a user", log, { limit: 5, user: "ISEhISEhISEhISEhISEhIQ" });
    put("log of a user that is no id", log, { limit: 5, user: "admin" });

    put("install the system", install, { target: "system" });
    put("install the system with a version", install, { target: "system", version: "1.0.0" });
    put("install a part", install, { target: "ngc-web", version: "1.1.0", sha256: sha });
    put("install a part without a checksum", install, { target: "ngc-web", version: "1.1.0" });
    put("stage the system", stage, { target: "system", version: "1.0.0" });
    put("stage a part", stage, { target: "ngc-web", version: "1.1.0" });

    put("a window", setUpdate, { target: "ngc-web", mode: "auto", window: "02:00-04:00" });
    put("no window", setUpdate, { target: "ngc-web", mode: "semi" });
    put("a window of 24 hours", setUpdate, { target: "ngc-web", mode: "auto", window: "24:00-04:00" });
    put("a mode nobody has", setUpdate, { target: "ngc-web", mode: "later" });

    put("rights", rights, { role: "user", rights: ["org.a.b/use", "org.a.b/shell"] });
    put("no rights", rights, { role: "user" });
    put("the same right twice", rights, { role: "user", rights: ["org.a.b/use", "org.a.b/use"] });
    put("a right on a core app", rights, { role: "user", rights: ["ngc.users/use"] });
    put("a role that is no name", rights, { role: "User", rights: [] });

    const me = a("ngc.desktop", "api", "me");
    put("an action without parameters", me, {});
    put("an action without parameters, and some", me, { app: "org.a.b" });
    put("a row nobody wrote", { app: "ngc.store", service: "controller", name: "made-up" }, {});
    put("no action at all", null, {});
    put("parameters that are no object", activate, "app=org.example.notes");
    return named;
  });
  expect(got).toEqual({
    "activate": true,
    "activate with both grants": true,
    "activate of a core app": false,
    "activate of no version": false,
    "activate with a short checksum": false,
    "activate with a short ui hash": false,
    "activate with a store that is no name": false,
    "activate with the same grant twice": false,
    "activate with a grant nobody offers": false,
    "activate without a checksum": false,
    "log of 1": true,
    "log of 500": true,
    "log of 0": false,
    "log of 501": false,
    "log of half a page": false,
    "log page": true,
    "log page without a time": false,
    "log page with an id that is none": false,
    "log of a user": true,
    "log of a user that is no id": false,
    "install the system": true,
    "install the system with a version": false,
    "install a part": true,
    "install a part without a checksum": false,
    "stage the system": false,
    "stage a part": true,
    "a window": true,
    "no window": true,
    "a window of 24 hours": false,
    "a mode nobody has": false,
    "rights": true,
    "no rights": true,
    "the same right twice": false,
    "a right on a core app": false,
    "a role that is no name": false,
    "an action without parameters": true,
    "an action without parameters, and some": false,
    "a row nobody wrote": false,
    "no action at all": false,
    "parameters that are no object": false,
  });
});
