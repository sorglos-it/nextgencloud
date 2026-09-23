import { test, expect } from "../fixtures/page.js";

// The fuzz target for the texts, names, paths and versions (CLAUDE.md asks
// every parser for one). It holds them against what has to be true for every
// input at all:
//
//   - every check answers true or false, byteLength a whole number, and none
//     of them throws for a string.
//   - accepted text is 1 to max bytes long and holds no code point of the
//     categories C (control, format, surrogate, private use, unassigned) and
//     no spacing character but the ASCII space. Those categories are asked
//     for here, not the list of the allowed ones, so the criterion does not
//     repeat the rule it checks.
//   - an accepted name, app id, path, login name, version, id or code is
//     itself such a text: they are all shown to people.
//   - an accepted path leads nowhere else: no leading slash, no backslash, no
//     empty segment and no segment "." or ".." or one that begins with a dot.
//   - newer orders versions strictly: nothing is newer than itself, and of
//     two different versions exactly one is the newer one.
//   - and enough inputs are accepted: a check that refuses everything keeps
//     every rule above and proves nothing.
//
// Seed and rounds come from the environment, so a failure can be run again:
// NGC_FUZZ_SEED=123 NGC_FUZZ_ROUNDS=50000 npm test -- contracts/text-fuzz
const seed = Number(process.env.NGC_FUZZ_SEED || 20260923) >>> 0;
const rounds = Number(process.env.NGC_FUZZ_ROUNDS || 20000);

test(`fuzz: nothing but printable text, ${rounds} inputs from seed ${seed}`, async ({ shell }) => {
  const out = await shell.evaluate(async ([seed, rounds]) => {
    const t = await import("/assets/js/contracts/text.js");

    let state = seed || 1;
    const next = () => {
      state ^= state << 13; state >>>= 0;
      state ^= state >> 17;
      state ^= state << 5; state >>>= 0;
      return state / 4294967296;
    };
    const upto = (n) => Math.floor(next() * n);
    const pick = (a) => a[upto(a.length)];

    const pieces = ["a", "z", "A", "Z", "0", "9", "-", "_", ".", "/", "~", " ", "\t", "\n",
      "\u00e4", "\u00a0", "\u00ad", "\u202e", "\ufdd0", "\uffff", "\ud800", "\udfff",
      "\u{1F600}", "\u2028", "\u0000", "\u007f", "ngc.", "ctl-", "1.0.0", "index.html",
      "..", "a".repeat(31), "org.sorglos.terminal", "0123456789abcdef"];
    const from = (alphabet, n) => {
      let s = "";
      for (let i = 0; i < n; i++) s += alphabet[upto(alphabet.length)];
      return s;
    };
    // Half the inputs are built along one of the rules, so enough of them get
    // through for the floor below to say anything.
    const shaped = () => {
      switch (upto(8)) {
        case 0: return from("abcdefghijklmnopqrstuvwxyz0123456789-", 1 + upto(34));
        case 1: return from("abcdefghijklmnopqrstuvwxyz0123456789-.", 1 + upto(24));
        case 2: return from("abcdefghijklmnopqrstuvwxyz0123456789-._/", 1 + upto(12));
        case 3: return `${upto(1000100)}.${upto(1000100)}.${upto(1000100)}`;
        // An id is 22 characters whose last four bits belong to no byte.
        case 4: return from("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_", 21) +
          (upto(2) === 0 ? "AQgw"[upto(4)] : "BR");
        case 5: return from("0123456789abcdefABCDEF", 30 + upto(4));
        case 6: return from("0123456789", 4 + upto(4));
        default: return from("abcdefghijklmnopqrstuvwxyz0123456789-._", 1 + upto(70));
      }
    };
    const word = () => {
      if (upto(2) === 0) return shaped();
      let s = "";
      const n = upto(6);
      for (let i = 0; i < n; i++) {
        s += upto(4) === 0 ? String.fromCodePoint(upto(0x110000)) : pick(pieces);
      }
      return s;
    };

    const controlLike = /\p{C}/u;
    const spacing = /\p{Z}/u;
    const bad = [];
    const count = { text: 0, name: 0, appID: 0, path: 0, login: 0, version: 0, id: 0, totp: 0 };
    const say = (why, s) => {
      if (bad.length < 12) bad.push({ why, input: JSON.stringify(s).slice(0, 120) });
    };
    const answer = (name, fn, s) => {
      try {
        const got = fn(s);
        if (typeof got !== "boolean") { say(`${name} answered ${typeof got}`, s); return false; }
        return got;
      } catch (e) {
        say(`${name} threw ${e.constructor.name}: ${e.message}`, s);
        return false;
      }
    };
    // printable says what no accepted text may hold. It asks the categories
    // that are out, never the ones that are in.
    const printable = (s) => {
      for (const cp of s) {
        if (controlLike.test(cp)) return false;
        if (cp !== " " && spacing.test(cp)) return false;
      }
      return true;
    };

    const versions = [];
    for (let i = 0; i < rounds; i++) {
      const s = word();
      const max = upto(40);
      let n = -1;
      try {
        n = t.byteLength(s);
      } catch (e) {
        say(`byteLength threw ${e.constructor.name}`, s);
      }
      if (!Number.isInteger(n) || n < 0) say(`byteLength answered ${String(n)}`, s);

      if (answer("validText", (x) => t.validText(x, max), s)) {
        count.text++;
        if (s === "") say("empty text accepted", s);
        if (n > max) say(`text of ${n} bytes accepted with max ${max}`, s);
        if (!printable(s)) say("text with a character that is not printable", s);
      }
      // Everything people get to see is such a text as well.
      const alsoText = (name, ok, max) => {
        if (!ok) return;
        if (!t.validText(s, max)) say(`${name} that is no text of ${max} bytes`, s);
      };
      const name = answer("validName", t.validName, s);
      const appID = answer("validAppID", t.validAppID, s);
      const path = answer("validPath", t.validPath, s);
      const login = answer("validLoginName", t.validLoginName, s);
      const version = answer("validVersion", t.validVersion, s);
      const id = answer("validID", t.validID, s);
      const logID = answer("validLogID", t.validLogID, s);
      const totp = answer("validTOTP", t.validTOTP, s);
      alsoText("a name", name, 32);
      alsoText("an app id", appID, 100);
      alsoText("a path", path, 200);
      alsoText("a login name", login, 64);
      alsoText("a version", version, 20);
      alsoText("an id", id, 22);
      alsoText("a log id", logID, 32);
      alsoText("a code", totp, 6);
      if (name) count.name++;
      if (appID) count.appID++;
      if (login) count.login++;
      if (id) count.id++;
      if (totp) count.totp++;
      if (path) {
        count.path++;
        if (s.startsWith("/") || s.includes("\\") || s.includes("//")) say("a path that leaves its folder", s);
        for (const seg of s.split("/")) {
          if (seg === "" || seg === "." || seg === ".." || seg.startsWith(".")) {
            say("a path with an empty, hidden or dot segment", s);
            break;
          }
        }
      }
      if (version) {
        count.version++;
        if (t.newer(s, s)) say("a version newer than itself", s);
        if (versions.length < 200) versions.push(s);
        for (const other of versions) {
          const a = t.newer(s, other), b = t.newer(other, s);
          if (a && b) say(`two versions each newer than the other: ${other}`, s);
          if (!a && !b && s !== other) say(`two versions in no order: ${other}`, s);
        }
        // A build that is a release version suits a minNgc it is not older
        // than, and none that it is older than.
        for (const other of versions) {
          if (t.supportsMinNGC(s, other) !== !t.newer(other, s)) say(`minNgc out of order: ${other}`, s);
        }
      }
      if (t.supportsMinNGC(s, "1.2.3") && !(t.validVersion(s) && !t.newer("1.2.3", s))) {
        say("a build that suits a minNgc it cannot", s);
      }
    }
    return { bad, count };
  }, [seed, rounds]);

  expect(out.bad).toEqual([]);
  // A floor under every check, at about a third of what this seed reaches.
  const floors = { text: 2200, name: 750, appID: 600, path: 3500, login: 2400, version: 400, id: 200, totp: 100 };
  const low = Object.entries(floors)
    .filter(([name, floor]) => !(out.count[name] >= floor))
    .map(([name, floor]) => `${name}: ${out.count[name]} accepted, ${floor} wanted`);
  expect(low).toEqual([]);
});
