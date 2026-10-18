// What a browser module is to Node, and who decides it.
//
// Every contract under public/assets/ is written as an ES module and loaded
// as one by the browser, which reads the `type="module"` on the script tag
// and nothing else. Node does not look at the page: for a plain .js file it
// walks UP the directory tree until it meets a package.json and takes that
// file's "type". There is one in tests/ ("module"), which covers the specs
// and the mock - but public/assets/ is not below tests/, so for those files
// the search leaves the repository and lands on whatever package.json the
// checkout happens to sit under.
//
// Measured on 18.10.2026: in a worktree under D:\claude, where a package.json
// with "type":"commonjs" sits two levels above the tree, `node mock/api.js`
// died before a single test ran -
//
//   SyntaxError: The requested module
//   '../../public/assets/vendor/noble-hashes/argon2.js'
//   does not provide an export named 'argon2id'
//
// - while the same commit in a tree with no package.json above it started the
// mock and ran the suite. The code was identical; the parent directory was
// not. And the failure is invisible as what it is: a CommonJS file simply has
// no named exports, so the error talks about the export and not about the
// file's type.
//
// THE PROPERTY: the "type" that governs public/assets/ is decided INSIDE this
// repository, not by whatever directory the checkout was placed in.
//
// A check that merely imported an asset module and read a named export off it
// would measure the parent directory rather than the repository: green here,
// and green before the fix as well in any tree that happens to sit nowhere in
// particular. So this one reads the chain itself - it walks up from each asset
// directory, stops at the first package.json, and holds two things about it:
// that it is still inside the repository, and that it says "module".
//
// It is a module and not a spec because the thing it guards is the start of
// the test run itself. Remove apps/web/package.json and the mock dies in
// node, so a Playwright case would never get to run and its red would be the
// webServer's, not its own (measured the same day). tools/check.sh calls this
// file directly, before anything needs a browser, and it is a hard red there.
import { existsSync, readFileSync } from "node:fs";
import { dirname, join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repo = join(here, "..", "..", "..");

// Every directory outside tests/ that a spec or the mock imports a plain .js
// file out of. A new one of these is a new place the search can escape from,
// so the list is this check's own subject matter.
export const reach = [
  "apps/web/public/assets/js/contracts",
  "apps/web/public/assets/vendor/noble-hashes",
];

function nearestManifest(from) {
  let at = join(repo, from);
  for (;;) {
    const file = join(at, "package.json");
    if (existsSync(file)) return file;
    const up = dirname(at);
    if (up === at) return null;
    at = up;
  }
}

// Every complaint there is to make, as sentences. Empty means the chain holds.
export function faults() {
  const bad = [];
  for (const from of reach) {
    // The walk is only worth anything if it starts where it is meant to: a
    // renamed or moved asset directory would otherwise let it begin one level
    // up and find a manifest that says nothing about the files in question.
    if (!existsSync(join(repo, from))) {
      bad.push(`${from} is not there, so nothing was read about it`);
      continue;
    }

    const file = nearestManifest(from);
    if (file === null) {
      bad.push(`nothing above ${from} declares a module type`);
      continue;
    }

    const inside = relative(repo, file);
    if (inside.startsWith("..") || inside.split(sep)[0] === "") {
      bad.push(`the type for ${from} comes from ${file}, outside this repository`);
      continue;
    }

    const said = JSON.parse(readFileSync(file, "utf8")).type;
    if (said !== "module") {
      bad.push(`${inside} governs ${from} and says ${JSON.stringify(said)}, not "module"`);
    }
  }
  return bad;
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  const bad = faults();
  for (const line of bad) console.error(`module type: ${line}`);
  if (bad.length) process.exit(1);
  console.log(`module type: ok, ${reach.length} directories inside the repository say "module"`);
}
