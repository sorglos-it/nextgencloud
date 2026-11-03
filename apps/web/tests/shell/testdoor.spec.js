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

test("nothing of the shipped shell knows the mock or its switches", async () => {
  // THE SECOND COUNTER-CASE, and the one the approval of way 3 asked for in
  // writing: the door and its switches live in apps/web/tests and nowhere
  // else. apps/web/public is what an installation serves - this reads it off
  // the file system rather than trusting the folder names.
  //
  // It is a case and not a note in a document because a document does not go
  // red: the day somebody reaches for the door from the shell "just for
  // debugging", this falls.
  const root = fileURLToPath(new URL("../../public", import.meta.url));
  const hits = [];
  const walk = (dir) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      const p = join(dir, e.name);
      if (e.isDirectory()) { walk(p); continue; }
      if (!/\.(js|html|php|json|css)$/.test(e.name)) continue;
      const text = readFileSync(p, "utf8");
      for (const needle of ["/api/v1/mock", "break=", "__ngcTest"]) {
        if (text.includes(needle)) hits.push(`${p}: ${needle}`);
      }
    }
  };
  walk(root);
  // The grundmenge, so a walk that read nothing cannot pass as a clean one:
  // the shell is there and it is more than a handful of files.
  expect(existsSync(join(root, "assets/js/auth/login.js")), "the shell was read").toBe(true);
  expect(hits, "the shipped shell names no test door").toEqual([]);
});
