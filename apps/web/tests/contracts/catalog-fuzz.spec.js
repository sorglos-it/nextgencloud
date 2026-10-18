import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// The fuzz target for the store documents (CLAUDE.md asks every parser for
// one): the manifest out of a package somebody uploaded, the update row a
// controller sent, and the signed catalog a store served. What it holds the
// module against:
//
//   - parseManifest and parseUpdateReport answer with a document or throw
//     BadCatalog, and nothing else; verifyCatalog answers with a catalog or
//     throws BadCatalog, Rollback included, which is one of them. A TypeError
//     or a RangeError from the engine is no answer a contract can give.
//   - every document they answer with is one the contract describes, and the
//     rules stand here in full rather than being asked of the module: an app
//     id that is no NGC app, printable names, a version without leading
//     zeros, a window between 200 and 4000 or none, services whose first word
//     is an absolute path and whose arguments carry no NUL, actions that name
//     a service of this manifest, needs that are root and network and neither
//     twice.
//   - a catalog that comes back belongs to the store it was fetched for and
//     is not older than the sequence number the caller named - the two rules
//     an attacker who can serve bytes is up against.
//   - granted and approved answer true or false for anything at all, fields
//     behind throwing getters included: they are rights, and an exception out
//     of one is not a refusal.
//   - and enough of the inputs get through at all. Every rule above holds for
//     a parser that refuses everything, so without a floor under the accepted
//     ones a step backwards stays green.
//
// The catalog rounds go through sign.verify, which needs Ed25519 in the
// engine's Web Crypto. The WebKit Playwright brings has none (measured; see
// catalog.spec.js), so there every catalog is refused for its signature and
// never reaches the parser. The rule above still holds - a refusal is a
// BadCatalog - and only the FLOOR under the accepted catalogs is asked where
// the algorithm is really there.
//
// Seed and rounds come from the environment, so a failure can be run again:
// NGC_FUZZ_SEED=123 NGC_FUZZ_ROUNDS=50000 npm test -- contracts/catalog-fuzz
const v = vectors("store.json");
const seed = Number(process.env.NGC_FUZZ_SEED || 20260923) >>> 0;
const rounds = Number(process.env.NGC_FUZZ_ROUNDS || 6000);

test(`fuzz: a document or BadCatalog, ${rounds} inputs from seed ${seed}`, async ({ shell }) => {
  test.setTimeout(240_000);
  const out = await shell.evaluate(async ([d, seed, rounds]) => {
    const c = await import("/assets/js/contracts/catalog.js");
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

    // Whether this engine can read the store's key at all. Without Ed25519
    // every catalog is refused for its signature, which keeps every rule but
    // reaches no parser.
    let ed25519 = false;
    try {
      await crypto.subtle.importKey("raw", b64decode(d.storeKey), { name: "Ed25519" }, false, ["verify"]);
      ed25519 = true;
    } catch {
      ed25519 = false;
    }

    // ---- the pieces, right and wrong -------------------------------------
    // Of each member there are the values that keep its rule and the ones
    // that sit just outside it. A piece is drawn from the edge cases every
    // third time, so whole documents come out often enough for the rules
    // below to have something to hold.
    const goodIDs = ["org.sorglos.terminal", "com.example.app", "a.b", "x9.y-z"];
    const oddIDs = ["ngc.desktop", "ngc.store", "terminal", ".a", "a.", "A.B", "a..b", "",
      "a." + "b".repeat(99), "a.b/c", "a b.c"];
    const goodNames = ["Terminal", "Ein Name", "x", "N".repeat(64)];
    const oddNames = ["", "N".repeat(65), "a\u0000b", "a\nb", "a­b", "‮"];
    const goodVersions = ["1.0.0", "0.0.0", "999999.0.1", "1.10.0"];
    const oddVersions = ["1.0", "01.0.0", "1.0.0.0", "v1.0.0", "1000000.0.0", "", "1.0.0-rc"];
    const goodPaths = ["index.html", "a/b.css", "icon.svg", "A-B_c.1", "x".repeat(200)];
    const oddPaths = ["/a", "../a", "a/../b", ".hidden", "a//b", "a\\b", "", "x".repeat(201), "a b"];
    const goodSizes = [0, 200, 720, 4000];
    const oddSizes = [199, 4001, -1, 1.5, 100];
    const goodCommands = [["/usr/bin/app"], ["/bin/sh", "-c", "x"], ["/a", ...Array(31).fill("x")]];
    const oddCommands = [[], ["sh"], ["/"], ["/a", ""], ["/a", "b\u0000c"], ["/a", "x".repeat(257)],
      ["/a", ...Array(32).fill("x")]];
    const goodNeeds = [[], ["root"], ["network"], ["root", "network"]];
    const oddNeeds = [["root", "root"], ["sudo"], ["ROOT"], ["", "root"]];
    const goodServiceNames = ["main", "worker", "a", "s".repeat(32)];
    const oddServiceNames = ["Main", "1a", "-a", "", "s".repeat(33), "a_b"];
    const goodTimeouts = [0, 1, 3600];
    const oddTimeouts = [-1, 3601, 1.5];
    const piece = (good, odd) => (upto(3) === 0 ? pick(odd) : pick(good));

    // members writes a document out of a list of name/value pairs in the
    // order the round draws them, so a parser that leans on the order of a
    // document is seen. Every eighth document loses a member or gains one
    // nobody asked for.
    const extras = ["extra", "Id", "__proto__", "constructor", "", "ui", "id"];
    const members = (pairs) => {
      for (let i = pairs.length - 1; i > 0; i--) {
        const j = upto(i + 1);
        [pairs[i], pairs[j]] = [pairs[j], pairs[i]];
      }
      if (upto(8) === 0) pairs.push([pick(extras), pick([1, "x", null, [], {}])]);
      if (upto(8) === 0) pairs.pop();
      return "{" + pairs.map(([n, val]) => JSON.stringify(n) + ":" + JSON.stringify(val)).join(",") + "}";
    };

    // ONE thing at a time. A manifest is written whole, in a form the module
    // takes, and then exactly one of its eighteen places is drawn from the
    // edge cases - in two documents out of three. A document that spoils all
    // eighteen at once is refused at the first of them and says nothing about
    // the seventeen behind it, which is the mistake that left an earlier draft
    // of this file with four accepted manifests in six thousand rounds.
    const manifest = () => {
      const s = upto(3) === 0 ? -1 : upto(18);
      const serviceNames = ["main", "worker", "log"];
      const actionNames = ["run", "stop", "show"];
      const services = [];
      const nS = 1 + upto(2);
      for (let i = 0; i < nS; i++) {
        services.push({
          name: s === 10 && i === 0 ? pick(oddServiceNames) : serviceNames[i],
          command: s === 11 && i === 0 ? pick(oddCommands) : pick(goodCommands),
        });
      }
      const actions = [];
      const nA = upto(3);
      for (let i = 0; i < nA; i++) {
        const stream = upto(4) === 0;
        actions.push({
          name: s === 12 && i === 0 ? pick(oddServiceNames) : actionNames[i],
          title: s === 13 && i === 0 ? pick(oddNames) : pick(goodNames),
          service: s === 14 && i === 0 ? "nosuch" : services[upto(services.length)].name,
          stream,
          timeout: s === 15 && i === 0 ? pick(oddTimeouts) : stream ? 0 : pick(goodTimeouts),
          critical: upto(2) === 0,
        });
      }
      // A manifest is a window, a set of services, or both; both halves are
      // written here, and the round that spoils the window spoils all three of
      // its numbers at once, because they are one member.
      const ui = s === 6
        ? { entry: pick(oddPaths), width: pick(oddSizes), height: pick(oddSizes) }
        : upto(4) === 0 ? null
          : { entry: pick(goodPaths), width: pick(goodSizes), height: pick(goodSizes) };
      const plain = actions.filter((a) => !a.stream);
      return members([
        ["id", s === 0 ? pick(oddIDs) : pick(goodIDs)],
        ["name", s === 1 ? pick(oddNames) : pick(goodNames)],
        ["version", s === 2 ? pick(oddVersions) : pick(goodVersions)],
        ["publisher", s === 3 ? pick(oddNames) : pick(goodNames)],
        ["description", s === 4 ? "x".repeat(1001) : "eine App"],
        ["icon", s === 5 ? pick(oddPaths) : upto(3) === 0 ? pick(goodPaths) : ""],
        ["ui", ui],
        ["actions", actions],
        ["services", services],
        ["start", s === 7 ? pick(["nosuch", "run", ""]) : plain.length > 0 && upto(3) !== 0 ? pick(plain).name : ""],
        ["runtime", s === 8 ? pick(["docker", "PODMAN", "podman "]) : pick(["", "podman"])],
        ["needs", s === 9 ? pick(oddNeeds) : pick(goodNeeds)],
        ["cpu", s === 16 ? pick([99, 64001, -1]) : pick([0, 100, 64000])],
        ["memory", s === 17 ? pick([15, 65537, -1]) : pick([0, 16, 65536])],
        ["minNgc", upto(4) === 0 ? pick(goodVersions) : ""],
      ]);
    };

    const goodTargets = ["ngc-web", "ngc-api", "ngc-controller", "system", "org.sorglos.terminal"];
    const goodStates = ["available", "downloaded", "installing", "installed", "rolledback",
      "failed", "rejected", "needsgrants"];
    const report = () => members([
      ["target", upto(3) === 0 ? piece(goodIDs, oddIDs) : pick(goodTargets)],
      ["version", upto(3) === 0 ? piece(goodVersions, oddVersions) : ""],
      ["state", piece(goodStates, ["", "ok", "AVAILABLE", "done"])],
      ["message", upto(3) === 0 ? piece([...goodNames, "x".repeat(256)], [...oddNames, "x".repeat(257)]) : ""],
    ]);

    const encoder = new TextEncoder();
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

    // ---- the rules, spelled out here -------------------------------------
    const APP_ID = /^[a-z0-9]+(\.[a-z0-9-]+)+$/;
    const NAME = /^[a-z][a-z0-9-]{0,31}$/;
    const VERSION = /^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})$/;
    const PATH = /^[A-Za-z0-9_-][A-Za-z0-9._-]*(\/[A-Za-z0-9_-][A-Za-z0-9._-]*)*$/;
    // The categories no text a person reads may hold: control, format,
    // surrogate, private use and unassigned, and every spacing character but
    // the ASCII space. Asked as the categories that are OUT, so the criterion
    // does not repeat the list of the ones that are in.
    const controlLike = /\p{C}/u, spacing = /\p{Z}/u;
    const bytesIn = (s) => new TextEncoder().encode(s).length;
    const text = (s, max) => {
      if (typeof s !== "string" || s === "" || bytesIn(s) > max) return false;
      for (const cp of s) {
        if (controlLike.test(cp)) return false;
        if (cp !== " " && spacing.test(cp)) return false;
      }
      return true;
    };
    const app = (s) => typeof s === "string" && s.length <= 100 && APP_ID.test(s) && !s.startsWith("ngc.");
    const version = (s) => typeof s === "string" && VERSION.test(s);
    const path = (s) => typeof s === "string" && s.length <= 200 && PATH.test(s);
    const window = (n) => n === 0 || (Number.isInteger(n) && n >= 200 && n <= 4000);
    const needs = (a) => Array.isArray(a) && a.length <= 2
      && a.every((n, i) => (n === "root" || n === "network") && a.indexOf(n) === i);

    const found = [];
    const say = (why, what) => { if (found.length < 8) found.push({ why, input: String(what).slice(0, 160) }); };
    const count = { manifest: 0, report: 0, catalog: 0, refused: 0 };

    const checkManifest = (m, src) => {
      if (!app(m.id)) return say("a manifest of app " + m.id, src);
      if (!text(m.name, 64) || !text(m.publisher, 64)) return say("a manifest with a name nobody can show", src);
      if (!version(m.version)) return say("a manifest at version " + m.version, src);
      if (bytesIn(m.description) > 1000) return say("a description of " + bytesIn(m.description) + " bytes", src);
      if (m.icon !== "" && !path(m.icon)) return say("an icon at " + m.icon, src);
      if (m.minNgc !== "" && !version(m.minNgc)) return say("a minNgc of " + m.minNgc, src);
      // An app is a window, a set of services, or both.
      if (m.ui === null && m.services.length === 0) return say("a manifest that is neither a window nor a service", src);
      if (m.ui !== null && (!path(m.ui.entry) || !window(m.ui.width) || !window(m.ui.height))) {
        return say("a window of " + m.ui.width + "x" + m.ui.height + " at " + m.ui.entry, src);
      }
      if (m.services.length > 16 || m.actions.length > 64) return say("more services or actions than allowed", src);
      const names = new Set();
      for (const s of m.services) {
        if (!NAME.test(s.name) || names.has(s.name)) return say("a service called " + s.name, src);
        names.add(s.name);
        if (s.command.length === 0 || s.command.length > 32) return say("a command of " + s.command.length + " words", src);
        // The first word is the program, and it is absolute: a relative one
        // would be looked up in a PATH the package may write.
        if (bytesIn(s.command[0]) < 2 || !s.command[0].startsWith("/")) return say("a program at " + s.command[0], src);
        for (const arg of s.command) {
          // A NUL would end the argument where the kernel reads it and leave
          // the rest standing where NGC reads it: two different commands.
          if (arg === "" || bytesIn(arg) > 256 || arg.includes("\u0000")) return say("an argument " + JSON.stringify(arg), src);
        }
      }
      const actions = new Set();
      for (const a of m.actions) {
        if (!NAME.test(a.name) || actions.has(a.name)) return say("an action called " + a.name, src);
        actions.add(a.name);
        if (!text(a.title, 100) || !names.has(a.service)) return say("an action on service " + a.service, src);
        if (!Number.isInteger(a.timeout) || a.timeout < 0 || a.timeout > 3600) return say("a timeout of " + a.timeout, src);
        // A stream stays open, so a time limit on it would be a limit nobody
        // means; the two are never set together.
        if (a.stream && a.timeout !== 0) return say("a stream with a timeout", src);
      }
      if (m.start !== "") {
        const a = m.actions.find((x) => x.name === m.start);
        if (a === undefined || a.stream) return say("a start action " + m.start, src);
      }
      if (m.runtime !== "" && m.runtime !== "podman") return say("a runtime " + m.runtime, src);
      if (!needs(m.needs)) return say("needs " + JSON.stringify(m.needs), src);
      if (m.cpu !== 0 && (m.cpu < 100 || m.cpu > 64000)) return say("a cpu share of " + m.cpu, src);
      if (m.memory !== 0 && (m.memory < 16 || m.memory > 65536)) return say("a memory share of " + m.memory, src);
    };

    const checkReport = (r, src) => {
      if (!goodStates.includes(r.state)) return say("an update row in state " + r.state, src);
      if (!(["ngc-web", "ngc-api", "ngc-controller", "system"].includes(r.target) || app(r.target))) {
        return say("an update row for " + r.target, src);
      }
      if (r.version !== "" && !version(r.version)) return say("an update row at version " + r.version, src);
      if (r.message !== "" && !text(r.message, 256)) return say("an update row with a message nobody can show", src);
    };

    const checkCatalog = (cat, id, lastSeq, src) => {
      // A catalog belongs to the store it was fetched for, and is never older
      // than what was already seen: the two rules somebody who can serve bytes
      // is up against.
      if (cat.store !== id) return say("a catalog of store " + cat.store + " taken for " + id, src);
      if (!Number.isSafeInteger(cat.seq) || cat.seq < 1 || cat.seq < lastSeq) {
        return say("a catalog at seq " + cat.seq + " taken over " + lastSeq, src);
      }
      if (!(cat.issued > 0)) return say("a catalog issued at " + cat.issued, src);
      if (cat.apps.length > 10000) return say("a catalog of " + cat.apps.length + " apps", src);
      for (let i = 0; i < cat.apps.length; i++) {
        const e = cat.apps[i];
        // Sorted by id and without a double, so one app cannot stand twice
        // with two prices and leave it to the reader which one holds.
        if (i > 0 && !(cat.apps[i - 1].id < e.id)) return say("two apps out of order: " + e.id, src);
        if (!app(e.id) || !text(e.name, 64) || !text(e.publisher, 64)) return say("an app " + e.id, src);
        if (e.versions.length === 0 || e.versions.length > 100) return say("an app with " + e.versions.length + " versions", src);
        for (let k = 0; k < e.versions.length; k++) {
          const p = e.versions[k];
          if (!version(p.version)) return say("a package at version " + p.version, src);
          if (k > 0 && !(cmp(p.version, e.versions[k - 1].version) > 0)) return say("versions out of order at " + e.id, src);
          if (!(p.size > 0) || p.size > 4 * 1024 * 1024 * 1024) return say("a package of " + p.size + " bytes", src);
          if (p.sha256.length !== 32 || p.ui.length !== 32) return say("a package whose hashes are no 32 bytes", src);
          // A package is encrypted exactly where the app costs money: a free
          // app with an encrypted package has a key nobody can be given, and a
          // paid one in plain is the paid app handed out for nothing.
          if (p.encrypted !== (e.price !== null)) return say("a package encrypted " + p.encrypted + " at a price of " + e.price, src);
          if (!needs(p.needs)) return say("a package that needs " + JSON.stringify(p.needs), src);
          if (p.minNgc !== "" && !version(p.minNgc)) return say("a package for NGC " + p.minNgc, src);
        }
      }
    };
    // Version order as numbers, not as text: 1.9.0 is older than 1.10.0 and a
    // comparison of strings has it the other way round.
    const cmp = (a, b) => {
      const x = a.split(".").map(Number), y = b.split(".").map(Number);
      for (let i = 0; i < 3; i++) if (x[i] !== y[i]) return x[i] - y[i];
      return 0;
    };

    // ---- the run ---------------------------------------------------------
    const catalogs = [d.catalog, ...d.catalogRejects.map((r) => r.catalog)];
    const store = { id: d.store, name: "NGC Store", url: "https://store.ngc.example", key: b64decode(d.storeKey) };
    for (let i = 0; i < rounds && found.length < 8; i++) {
      const src = manifest();
      const b = i % 4 === 0 ? mutate(encoder.encode(src)) : encoder.encode(src);
      try {
        const m = c.parseManifest(b);
        count.manifest++;
        checkManifest(m, src);
      } catch (e) {
        if (!(e instanceof c.BadCatalog)) say(`parseManifest threw ${e.constructor.name}: ${e.message}`, src);
        count.refused++;
      }
      const rsrc = report();
      const rb = i % 4 === 1 ? mutate(encoder.encode(rsrc)) : encoder.encode(rsrc);
      try {
        const r = c.parseUpdateReport(rb);
        count.report++;
        checkReport(r, rsrc);
      } catch (e) {
        if (!(e instanceof c.BadCatalog)) say(`parseUpdateReport threw ${e.constructor.name}: ${e.message}`, rsrc);
        count.refused++;
      }
    }

    // The catalogs: one round in twenty, because each of them is a signature
    // check. MEASURED, and it is why the share is so small: 1000 of them cost
    // chromium 1.2 seconds and the WebKit Playwright brings 12.8, because
    // there importKey fails for Ed25519 and fails slowly. At one in twenty the
    // whole target costs under a second in chromium and about four in WebKit,
    // and 60 catalogs still get through - enough for the rules to hold
    // against.
    for (let i = 0; i < Math.ceil(rounds / 20) && found.length < 8; i++) {
      // The catalog of the vectors two rounds in three, one of the sixteen
      // rejects in the third: what the sixteen are refused for is
      // catalog.spec.js's question, and here they are only another shape of
      // document. Drawing all seventeen alike leaves almost nothing that gets
      // past the signature, and the rules below are about what DOES.
      const e = upto(3) === 0 ? pick(catalogs) : d.catalog;
      const env = {
        payload: upto(3) === 0 ? mutate(b64decode(e.payload)) : b64decode(e.payload),
        kid: upto(8) === 0 ? pick([e.kid, e.kid.slice(0, 21), e.kid + "A", "", 7]) : e.kid,
        sig: upto(4) === 0 ? mutate(b64decode(e.sig)) : b64decode(e.sig),
      };
      const st = upto(8) === 0 ? pick([{ ...store, id: "other" }, { ...store, key: new Uint8Array(32) }, {}, null]) : store;
      const lastSeq = upto(4) === 0 ? pick([6, 1000, -1, 1.5, "0", undefined]) : pick([0, 1, 5]);
      try {
        const cat = await c.verifyCatalog(env, st, lastSeq);
        count.catalog++;
        checkCatalog(cat, st?.id, lastSeq, "a catalog");
      } catch (err) {
        if (!(err instanceof c.BadCatalog)) say(`verifyCatalog threw ${err.constructor.name}: ${err.message}`, "a catalog");
        count.refused++;
      }
    }
    // Rollback is a BadCatalog, so a caller who only wants "refused" catches
    // one thing.
    if (!(new c.Rollback("x") instanceof c.BadCatalog)) say("Rollback is no BadCatalog", "");

    // granted, approved and validTarget over every VALUE no parser would hand
    // them: a list that is none, one without its auto entries, entries that
    // are null, a number or a text, an entry without a key.
    //
    // And not over a field behind a getter that throws. That is the one shape
    // asked of tunnel.checkCode and core.validateParams, because each of those
    // two says in its own words that it answers and never throws; approved
    // says something else - "everything that is not plainly an approval is
    // none" - which is a rule about VALUES, and the list it is handed is the
    // one trust.parseList built out of a verified document, never an object a
    // caller shaped. Asking it here anyway would be this file inventing a
    // promise and then reporting the module for not keeping it.
    const lists = [null, undefined, 0, "", [], {}, { auto: null }, { auto: [] },
      { auto: [null, 0, "x"] }, { auto: [{ target: "system" }] },
      { auto: [{ target: "ngc-web", key: new Uint8Array(32) }] },
      { auto: [{ target: "ngc-web" }] }, { auto: [{ target: "ngc-web", key: new Uint8Array(0) }] }];
    const keys = [null, undefined, new Uint8Array(0), new Uint8Array(32), "x", 7];
    const targets = ["ngc-web", "system", "org.sorglos.terminal", "ngc.desktop", "", null, 7];
    let asked = 0;
    for (const l of lists) {
      for (const k of keys) {
        for (const tg of targets) {
          let got;
          try {
            got = c.approved(l, tg, k);
          } catch (err) {
            say(`approved threw ${err.constructor.name}`, "a shape");
            continue;
          }
          if (typeof got !== "boolean") say(`approved answered ${typeof got}`, "a shape");
          else asked++;
          if (typeof c.validTarget(tg) !== "boolean") say("validTarget answered no boolean", String(tg));
        }
      }
    }
    // granted is a rule over two lists of needs and is asked the same way.
    for (const a of [[], ["root"], ["root", "network"], ["sudo"]]) {
      for (const b of [[], ["root"], ["root", "network"]]) {
        const got = c.granted(a, b);
        if (typeof got !== "boolean") say("granted answered no boolean", JSON.stringify(a));
        else if (got !== a.every((n) => b.includes(n))) say("granted answered against its own rule", JSON.stringify(a));
      }
    }

    return { found, count, asked, ed25519 };
  }, [v, seed, rounds]);

  // What was found comes first: the loop stops at the eighth finding, so a
  // fault early in the run leaves the counts below under their floors as well.
  expect(out.found).toEqual([]);
  if (out.found.length === 0) {
    // Measured: 1217 manifests and 1832 update rows out of 6000 rounds of this
    // seed, in both engines. The floors are at 5 and 10 in 100, about a third
    // of that. They count in rounds and not in a fixed number, because the
    // rounds are a knob this file advertises itself.
    expect(out.count.manifest).toBeGreaterThan(Math.floor(rounds * 0.05));
    expect(out.count.report).toBeGreaterThan(Math.floor(rounds * 0.10));
    expect(out.count.refused).toBeGreaterThan(rounds);
    // The catalogs only where the engine can read the store's key at all; on
    // one without Ed25519 they are all refused for their signature, which is
    // no fault of the parser and no reason to fail a run.
    if (out.ed25519) expect(out.count.catalog).toBeGreaterThan(Math.floor(rounds / 20 * 0.1));
    // Every shape reached approved and answered.
    expect(out.asked).toBe(13 * 6 * 7);
  }
});
