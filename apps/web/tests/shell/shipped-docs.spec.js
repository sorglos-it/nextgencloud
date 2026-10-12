import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { test, expect } from "@playwright/test";

// THE TWO FILES OF THIS INTERFACE THAT LEAVE THE REPOSITORY. The release
// archive carries apps/web/README.md (as README.md) and
// templates/apache-vhost.conf - the packer names both by hand in
// tools/release/extra-web.txt, on the release-packer branch - and docs/ is in
// none of its lists. So whoever stands on a server has these two and nothing
// else of this folder.
//
// WHAT WENT WRONG AND IS HELD HERE. Both files end their measuring list with
// "it is a FIND and belongs in docs/STAND.md". That file is in the repository
// and NOT in the archive, so for the one reader the sentence is written for -
// an admin on the first machine with a real Apache, who just measured
// something - it pointed at a name there is nothing behind. Measured on
// 10.10.2026 by the packer run: 66 files in the archive, docs/STAND.md not one
// of them.
//
// The cure is not to drop the name: the project really does keep these
// measurements there, and the next person with the repository needs to know
// where they went. The cure is that the sentence SAYS the file is not in a
// release. This case holds exactly that, and it holds it per paragraph: a
// marker three screens away would be no help to the reader, and the reader is
// the whole point.
const SHIPPED = {
  "apps/web/README.md": "../../README.md",
  "apps/web/templates/apache-vhost.conf": "../../templates/apache-vhost.conf",
};

const RECORD = "docs/STAND.md";
const MARKER = "not in a release";

// A paragraph is a run of lines with something in it. In Markdown the empty
// line separates; in the virtual host every line is a comment, so a line that
// is only "#" separates there. Both are the break a reader sees.
function paragraphs(text) {
  const out = [];
  let now = [];
  for (const line of text.split(/\r?\n/)) {
    if (/^\s*#?\s*$/.test(line)) {
      if (now.length) out.push(now.join(" "));
      now = [];
      continue;
    }
    now.push(line.replace(/^\s*#\s?/, ""));
  }
  if (now.length) out.push(now.join(" "));
  return out;
}

for (const [name, rel] of Object.entries(SHIPPED)) {
  test(`${name} ships, so where a FIND goes may not point into the repository alone`, () => {
    const text = readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
    const blocks = paragraphs(text);

    // Not a formality: without this the whole case passes on a file that lost
    // the sentence, and a hollow guard is worse than none. If the sentence is
    // meant to go for good, this line is the one to argue with first.
    const naming = blocks.filter((b) => b.includes(RECORD));
    expect(naming.length,
      `${name} names ${RECORD} nowhere, so this case measures nothing`).toBeGreaterThan(0);

    for (const block of naming) {
      expect(block,
        `${name} sends the reader to ${RECORD} without saying it is "${MARKER}": ${block}`,
      ).toContain(MARKER);
    }
  });
}
