import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// The fuzz target for the addresses (CLAUDE.md asks every parser for one).
// There is no second implementation to compare against here either, so it
// holds the checks against what has to be true for every input at all:
//
//   - every check answers true or false and nothing else, webURL a string or
//     null. An exception from the engine is no answer a contract can give.
//   - an accepted address carries nothing but the characters of its grammar.
//     Everything here ends up in configurations of other programs (Apache,
//     the database driver) and in the trust list, where a space, a quote or a
//     line feed would be a second line, not a bad address.
//   - the forms agree with one another: a host is an origin, a host and port
//     is a web and a tunnel address, and the address webURL derives from a
//     tunnel address passes validWebURL. They are built from one another in
//     Go, so one that drifts off is a mistake.
//   - and at least a few hundred of them are accepted: a check that refuses
//     everything keeps every rule above and proves nothing.
//
// Seed and rounds come from the environment, so a failure can be run again:
// NGC_FUZZ_SEED=123 NGC_FUZZ_ROUNDS=50000 npm test -- contracts/url-fuzz
const v = vectors("url.json");
const seed = Number(process.env.NGC_FUZZ_SEED || 20260923) >>> 0;
const rounds = Number(process.env.NGC_FUZZ_ROUNDS || 20000);

test(`fuzz: nothing but addresses, ${rounds} inputs from seed ${seed}`, async ({ shell }) => {
  const known = Object.values(v).flat().map((c) => c.value);
  const out = await shell.evaluate(async ([known, seed, rounds]) => {
    const u = await import("/assets/js/contracts/url.js");

    let state = seed || 1;
    const next = () => {
      state ^= state << 13; state >>>= 0;
      state ^= state >> 17;
      state ^= state << 5; state >>>= 0;
      return state / 4294967296;
    };
    const upto = (n) => Math.floor(next() * n);
    const pick = (a) => a[upto(a.length)];

    // Pieces that sit on the edges of the grammar, put together into whole
    // addresses often enough that many of them get through.
    const labels = ["a", "ab", "a-b", "-a", "a-", "A", "1", "12", "0", "01", "255",
      "256", "999", "localhost", "test", "xn--a", "_", "\u00e4", "a".repeat(63), "a".repeat(64), ""];
    const groups = ["0", "00", "000", "0000", "1", "01", "a", "A", "ff", "ffff", "db8",
      "2001", "", "ffff:1", "1.2.3.4"];
    const ports = ["1", "0", "01", "443", "80", "7443", "65535", "65536", "99999", "", "+1", "-1", "1a"];
    const junk = ["", " ", "\n", "\t", "\"", "'", "%", "@", "?", "#", "/", "\\", "..", ".", "~", ":", "::"];

    const dnsName = () => {
      const n = 1 + upto(4);
      const out = [];
      for (let i = 0; i < n; i++) out.push(pick(labels));
      return out.join(".");
    };
    const ipv6 = () => {
      const n = 1 + upto(9);
      const parts = [];
      for (let i = 0; i < n; i++) parts.push(pick(groups));
      if (upto(2) === 0) {
        const k = upto(parts.length + 1);
        return parts.slice(0, k).join(":") + "::" + parts.slice(k).join(":");
      }
      return parts.join(":");
    };
    const host = () => {
      switch (upto(4)) {
        case 0: return dnsName();
        case 1: return "[" + ipv6() + "]";
        case 2: return `${upto(300)}.${upto(300)}.${upto(300)}.${upto(300)}`;
        default: return ipv6();
      }
    };
    const hostPort = () => host() + (upto(8) === 0 ? "" : ":" + pick(ports));
    const path = () => {
      const n = upto(4);
      let s = "";
      for (let i = 0; i < n; i++) s += "/" + (upto(4) === 0 ? pick(junk) : pick(labels));
      return s;
    };
    const input = () => {
      switch (upto(8)) {
        case 0: return host();
        case 1: return hostPort();
        case 2: return "wss://" + hostPort() + "/tunnel";
        case 3: return "https://" + hostPort();
        case 4: return "https://" + hostPort() + path();
        case 5: return pick(known);
        case 6: {
          // A known address with one piece of junk pushed into it.
          const s = pick(known);
          const i = upto(s.length + 1);
          return s.slice(0, i) + pick(junk) + s.slice(i);
        }
        default: {
          let s = "";
          const n = 1 + upto(6);
          for (let i = 0; i < n; i++) s += pick([...labels, ...groups, ...ports, ...junk]);
          return s;
        }
      }
    };

    // The characters each form may carry. A host is lower case, because NGC
    // compares addresses as text; only a store path knows upper case.
    const HOST_CHARS = /^[a-z0-9.:[\]-]+$/;
    const URL_CHARS = /^[a-z0-9.:[\]/-]+$/;
    const STORE_CHARS = /^[A-Za-z0-9.:[\]/~_-]+$/;
    const bad = [];
    const count = { host: 0, hostPort: 0, tunnel: 0, web: 0, origin: 0, store: 0 };
    const say = (why, s) => { if (bad.length < 12) bad.push({ why, input: s.slice(0, 120) }); };
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

    for (let i = 0; i < rounds; i++) {
      const s = input();
      const h = answer("validHost", u.validHost, s);
      const hp = answer("validHostPort", u.validHostPort, s);
      const t = answer("validTunnelURL", u.validTunnelURL, s);
      const w = answer("validWebURL", u.validWebURL, s);
      const o = answer("validOrigin", u.validOrigin, s);
      const st = answer("validStoreURL", u.validStoreURL, s);
      let web = null;
      try {
        web = u.webURL(s);
      } catch (e) {
        say(`webURL threw ${e.constructor.name}`, s);
      }
      if (h) {
        count.host++;
        if (!HOST_CHARS.test(s) || s !== s.toLowerCase()) say("a host outside its characters", s);
        if (s.length > 255) say("a host longer than a name may be", s);
        // Go builds the other forms out of the host, so they have to follow.
        if (!u.validHostPort(s + ":443")) say("a host that is no host with a port", s);
        if (s.length + 8 <= 256 && !u.validOrigin("https://" + s)) say("a host that is no origin", s);
      }
      if (hp) {
        count.hostPort++;
        if (!HOST_CHARS.test(s) || s !== s.toLowerCase()) say("a host and port outside its characters", s);
        if (!s.includes(":")) say("a host and port without a port", s);
        if (s.length + 8 <= 256 && !u.validWebURL("https://" + s)) say("a host and port that is no web address", s);
        if (s.length + 13 <= 256 && !u.validTunnelURL("wss://" + s + "/tunnel")) {
          say("a host and port that is no tunnel address", s);
        }
      }
      if (t) {
        count.tunnel++;
        if (!URL_CHARS.test(s)) say("a tunnel address outside its characters", s);
        if (s.length > 256) say("a tunnel address longer than 256", s);
        if (!s.startsWith("wss://") || !s.endsWith("/tunnel")) say("a tunnel address without its ends", s);
        if (typeof web !== "string" || !u.validWebURL(web)) say("a tunnel address whose web address is none", s);
      }
      if (web !== null && !t) say("a web address derived from no tunnel address", s);
      if (w) {
        count.web++;
        if (!URL_CHARS.test(s) || s.length > 256) say("a web address outside its characters", s);
        if (s.slice(8).includes("/")) say("a web address with a path", s);
      }
      if (o) {
        count.origin++;
        if (!URL_CHARS.test(s) || s.length > 256) say("an origin outside its characters", s);
        if (s.slice(8).includes("/")) say("an origin with a path", s);
        if (s.endsWith(":443")) say("an origin with the default port", s);
      }
      if (st) {
        count.store++;
        if (!STORE_CHARS.test(s) || s.length > 256) say("a store address outside its characters", s);
        if (s.endsWith("/")) say("a store address with a trailing slash", s);
        const p = s.slice(8).split("/").slice(1);
        if (p.some((seg) => seg === "" || seg === "." || seg === "..")) say("a store path with an empty or dot segment", s);
      }
    }
    return { bad, count };
  }, [known, seed, rounds]);

  expect(out.bad).toEqual([]);
  // A floor under every check, at about a third of what this seed reaches:
  // a step backwards that refuses everything keeps every rule above and would
  // stay green without one.
  const floors = { host: 400, hostPort: 80, tunnel: 90, web: 110, origin: 140, store: 260 };
  const low = Object.entries(floors)
    .filter(([name, floor]) => !(out.count[name] >= floor))
    .map(([name, floor]) => `${name}: ${out.count[name]} accepted, ${floor} wanted`);
  expect(low).toEqual([]);
});
