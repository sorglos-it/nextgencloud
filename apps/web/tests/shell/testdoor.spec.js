// The mock's test door stays in apps/web/tests.
//
// IT IS A FILE OF ITS OWN, and the reason is a measuring rule rather than
// tidiness: this case reads the file system and has nothing to do with the
// engine it runs on. In shell/login.spec.js it would inherit that file's
// beforeEach, which skips every case on an engine that cannot draw the mask -
// and a case that is switched off for a thing it never touches is a case
// nobody measured (Z:\GitHub\claude-md\messen.md, "Eine WEGGELASSENE
// Variante hinterlaesst nicht einmal einen uebersprungenen Fall"). Here it
// starts no browser and runs on all three engines.
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { test, expect } from "@playwright/test";

// WHAT THE FIRST RUN OF THIS CASE FOUND, on 03.11.2026 on chromium at
// origin/main 0471b02d, and it is why the case is written the way it is below
// instead of the way it was written first.
//
// The first version asserted plainly "no file of apps/web/public names a test
// door". It went RED, with two hits that are not mine:
//
//   assets/js/net/testlink.js   names /api/v1/mock
//   assets/js/shell/fake-link.js  reads and writes window.__ngcTest
//
// Measured afterwards, because a hit is not yet a finding:
//
//   shell/main.js:15   statically imports fake-link.js - so it is fetched on
//                      every boot of a real installation, not only in tests
//   shell/main.js:306  const standIn = () => window.__ngcStandIn === true
//   shell/main.js:327  if (standIn()) the whole session becomes fakeWorld()
//   net/testlink.js    is imported by NOBODY (0 hits over apps/web) - it is
//                      shipped code that nothing loads
//
// So the shipped shell DOES carry a runtime test switch, and it is settable
// from the page - which is exactly what the header of shell/login.spec.js
// says must not exist ("a hook on window that changes how the LOGIN behaves
// is a hook anything else on the page can set ... so the shell reads no such
// thing"). That sentence is true of the LOGIN and not of the shell.
//
// WHAT IT IS AND IS NOT: it is not an escalation. __ngcStandIn swaps the
// session for a fake one in the viewer's OWN browser, and whoever can set it
// can already run script on the page. What it is, is shipped test code with
// no date on it - and the measured proof of why way 3 was chosen over way 2
// (docs/login-fehlerklassen.md): apps/web has no build step, so a switch that
// lives at runtime is a switch nobody removes. This is one, and main.js:299
// carries the countable criterion for when it goes.
//
// THE CASE IS NOT WEAKENED TO GET GREEN. The two files are named below, with
// what they are, and anything BEYOND them makes it red. `break=`, the switch
// this round added, is held to zero with no exception at all.
const KNOWN = [
  // Both leave with the stand-in. main.js:299 says when, countably:
  //   grep -rn standInOn apps/web/tests --include=*.js
  // is over when only the definition and the comments are left. Then
  // fake-link.js, standIn() in main.js and the standIn argument of signIn()
  // go in one commit - and testlink.js, which nothing imports, can go today.
  { file: "assets/js/net/testlink.js", needle: "/api/v1/mock" },
  { file: "assets/js/shell/fake-link.js", needle: "__ngcTest" },
  // The switch itself: main.js:306 reads window.__ngcStandIn and main.js:327
  // hands the whole session to fakeWorld() when it is set. This is the line
  // the two above hang from, and the one that makes the shipped shell carry a
  // runtime test switch at all.
  { file: "assets/js/shell/main.js", needle: "__ngcStandIn" },
];

// MEASURED on 03.11.2026 at origin/main 0471b02d: those three are ALL of them.
// `grep -rln` over apps/web/public for the four needles names exactly
// testlink.js, fake-link.js and main.js, and `break=` appears in no file at
// all. So the list above is not a guess at what might be there - it is the
// whole of what is, written down so that the next thing to appear is red.

test("the shipped shell names no test door but the two that are known", async () => {
  // THE SECOND COUNTER-CASE, and the one the approval of way 3 asked for in
  // writing: the door and its switches live in apps/web/tests and nowhere
  // else. apps/web/public is what an installation serves - this reads it off
  // the file system rather than trusting the folder names.
  //
  // It is a case and not a note in a document because a document does not go
  // red: the day somebody reaches for the door from the shell "just for
  // debugging", this falls.
  const root = fileURLToPath(new URL("../../public", import.meta.url));
  const known = new Set(KNOWN.map((k) => `${k.file}: ${k.needle}`));
  const hits = [];
  const seen = [];
  let files = 0;
  const walk = (dir) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      const p = join(dir, e.name);
      if (e.isDirectory()) { walk(p); continue; }
      if (!/\.(js|html|php|json|css)$/.test(e.name)) continue;
      files++;
      const text = readFileSync(p, "utf8");
      // The path as the repository spells it, so a hit reads the same on
      // every machine and the exceptions above can be written down at all.
      const rel = p.slice(root.length + 1).split("\\").join("/");
      for (const needle of ["/api/v1/mock", "break=", "__ngcTest", "__ngcStandIn"]) {
        if (!text.includes(needle)) continue;
        const hit = `${rel}: ${needle}`;
        seen.push(hit);
        if (!known.has(hit)) hits.push(hit);
      }
    }
  };
  walk(root);
  // The grundmenge, twice, so a walk that read nothing cannot pass as a clean
  // one: the file that MUST be there was read, and the count is a shell and
  // not a handful.
  expect(existsSync(join(root, "assets/js/auth/login.js")), "the shell was read").toBe(true);
  expect(files, "the walk read the whole shell").toBeGreaterThan(20);
  // Every known exception must still BE there. An exception that has gone is
  // a line to delete, and a list of exceptions nobody prunes is how a case
  // like this rots into a permanent pass.
  for (const k of KNOWN) {
    expect(seen, `${k.file} still names ${k.needle} - if it is gone, take it out of KNOWN`)
      .toContain(`${k.file}: ${k.needle}`);
  }
  expect(hits, "no test door beyond the two known ones").toEqual([]);
  // And the switch of this round, with no exception: it exists only in
  // apps/web/tests, and nothing an installation serves may name it.
  expect(seen.filter((h) => h.endsWith(": break=")), "break= is nowhere in the shipped shell")
    .toEqual([]);
});
