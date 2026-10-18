// The lookup as the person meets it: which language the page comes up in, what
// happens to a key one file has not got, and - the one that matters most -
// that a naked key never reaches the screen.
//
// THREE THINGS WERE MEASURED WHILE WRITING THIS FILE, and each one changed it.
//
// 1. EVERY CASE READS THE PAGE, not the module. The first draft did
//    `await import("/assets/js/lang.js")` inside page.evaluate and asked t()
//    directly. WebKit answered "ReferenceError: Cannot access uninitialized
//    variable" for all ten cases while the same page rendered its mask
//    perfectly: that engine will not hand a module with a top-level await to a
//    dynamic import out of an injected evaluation. Ten cases red about the
//    engine, saying nothing about the code.
//
// 2. WHICH SENTENCE THE PAGE SHOWS IS NOT THE SAME ON EVERY ENGINE. The second
//    draft read the heading of the login mask. On the WebKit Playwright brings
//    there IS no mask: that engine has neither Ed25519 nor X25519, so
//    shell/main.js stops before the login and puts up "this browser is too
//    old" - a sentence out of lang/ just the same. So no case here names a
//    sentence; each asks the page which of a handful of first sentences it is
//    showing and works with that key.
//
// 3. THE LANGUAGE A BROWSER ASKS FOR CANNOT BE SET ON EVERY ENGINE. Measured
//    with a probe over all three: a WebKit context with locale "is-IS" AND
//    extraHTTPHeaders {"Accept-Language": "is-IS"} still gets a German page
//    back on this machine, while Chromium and Firefox get English. Playwright
//    reports "is-IS" as the header it set; what WebKit's own network stack
//    sends is the system's language, and there is no way through it from here.
//    So THE SERVER'S CHOICE IS MEASURED OVER HTTP, with the header set exactly,
//    by Playwright's own client - which is the thing being measured anyway,
//    because classes/Lang.php is what picks - and the BROWSER cases measure
//    that the page really renders what the block it was served says.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { test, expect, signIn, skipWithoutLogin } from "../fixtures/page.js";

const WEB = fileURLToPath(new URL("../..", import.meta.url));
const tableOf = (tag) => JSON.parse(readFileSync(WEB + "/lang/" + tag + ".json", "utf8"));
const TAGS = ["de", "en", "fr", "it", "es"];

// The sentences a page can greet somebody with, by key. One of them stands in
// #root after the boot, whichever way the boot went: the login mask's heading
// where the engine can open a session, and one of the refusals where it cannot
// or where the installation is not there.
const FIRST = ["login.head", "shell.oldbrowser", "shell.insecure", "page.notsetup",
  "setup.askcode", "trust.notsetup"];

const BLOCK = '<script type="application/json" id="ngc-lang">';

// blockOf reads the language block out of a page's HTML. It is what the shell
// reads, so a case that wants to know which language was served asks this and
// not the browser.
function blockOf(html) {
  const from = html.indexOf(BLOCK);
  if (from < 0) throw new Error("lang: the page carries no language block");
  const to = html.indexOf("</script>", from);
  return JSON.parse(html.slice(from + BLOCK.length, to).replaceAll("\\u003C", "<"));
}

// What stands in the shell's own root. NOT document.body, because the body also
// carries the <noscript> line - which is a sentence out of lang/ as well, in
// the chosen language, and a case that read it would be green about the page's
// language without the page having chosen one.
const rootText = (page) => page.locator("#root").textContent({ timeout: 15000 })
  .then((s) => s.replace(/\s+/g, " ").trim());

// WHAT A BENT PAGE IS ASSERTED WITH, and why it is not rootText. MEASURED: two
// of the cases below read #root once with rootText right after reload() and
// were FLAKY on Chromium - green in one run of the suite, red in the next,
// while WebKit had them green throughout. A single read is a race against the
// boot: the shell replaces the root's children when it is through, and the old
// text is there until it does. So every case that changed something navigates
// afresh and asserts with toContainText, which retries until the page has
// settled; only the "this must NOT be there" half is read once, AFTER the
// positive half has already waited for the new text.
const shows = (page, text) => expect(page.locator("#root")).toContainText(text, { timeout: 15000 });

// shownKey answers which of FIRST this page is showing, in language tag - and
// throws if it is none of them. That is the preparation check of every case
// below: a run that recognised nothing would otherwise compare two empty
// strings and look green (messen.md).
async function shownKey(page, tag) {
  const text = await rootText(page);
  const table = tableOf(tag);
  for (const key of FIRST) {
    if (table[key] !== undefined && text.includes(table[key])) return key;
  }
  throw new Error("lang: the page shows none of the sentences this case knows - " + text);
}

// langOf is the language the page the browser got was served in. Read off the
// document, which carries it server-side - never guessed from the locale the
// context asked for, for the reason in point 3 at the top.
const langOf = (page) => page.evaluate(() => document.documentElement.lang);

// bend rewrites the language block of the page on its way to the browser. The
// tables are IN the page (templates/desktop.php, Lang::page), so this is where
// a state the file test makes impossible - a key one language has not got - is
// made for one case and nowhere else.
//
// change gets {tag, main, base} and changes it in place; if it changed nothing,
// the case stops instead of reporting a number.
async function bend(page, change) {
  await page.route((url) => url.pathname === "/", async (route) => {
    const res = await route.fetch();
    const html = await res.text();
    const v = blockOf(html);
    const was = JSON.stringify(v);
    change(v);
    if (JSON.stringify(v) === was) throw new Error("lang: nothing was changed in the block");
    const from = html.indexOf(BLOCK);
    const to = html.indexOf("</script>", from);
    await route.fulfill({
      response: res,
      body: html.slice(0, from + BLOCK.length)
        + JSON.stringify(v).replaceAll("<", "\\u003C")
        + html.slice(to),
    });
  });
}

// drop takes one key out of one of the two tables, and says so if it was not
// there to begin with.
const drop = (which, key) => (v) => {
  if (!Object.prototype.hasOwnProperty.call(v[which], key)) {
    throw new Error("lang: " + which + " never had " + key + " - nothing was taken out");
  }
  delete v[which][key];
};

// nakedKeys sweeps a page for text that is a key of the language files rather
// than a sentence out of them: text nodes, and the three attributes a person
// also gets read out to them.
//
// WHAT COUNTS: the WHOLE content of a node or an attribute equals one of the
// keys. Not "anything with a dot in it" - a version number and a host name
// have dots too - so the names are read off the disk and compared one by one.
async function nakedKeys(page, keys) {
  return page.evaluate((all) => {
    const bad = [];
    const look = (s) => {
      const text = String(s ?? "").trim();
      if (text !== "" && all.includes(text)) bad.push(text);
    };
    const walk = (node) => {
      if (node.nodeType === Node.TEXT_NODE) {
        look(node.nodeValue);
        return;
      }
      if (node.nodeType !== Node.ELEMENT_NODE) return;
      look(node.getAttribute("title"));
      look(node.getAttribute("aria-label"));
      look(node.getAttribute("placeholder"));
      for (const c of node.childNodes) walk(c);
    };
    walk(document.body);
    return bad;
  }, keys);
}

// ---------------------------------------------------------------- the folder

// What the server answers, before any page asks. The folder is the list, so
// this is also the case that proves the discovery works at all.
test("the server names the languages it has", async ({ request }) => {
  const res = await request.get("/lang/index.json");
  expect(res.status()).toBe(200);
  const body = await res.json();
  expect([...body.languages].sort()).toEqual([...TAGS].sort());
});

test("a language file comes back whole", async ({ request }) => {
  const res = await request.get("/lang/fr.json");
  expect(res.status()).toBe(200);
  expect(await res.json()).toEqual(tableOf("fr"));
});

test("a tag that is no tag is a page that is not there", async ({ request }) => {
  for (const bad of ["/lang/..%2fweb.json", "/lang/De.json", "/lang/dede.json", "/lang/zz.json"]) {
    const res = await request.get(bad);
    expect({ bad, status: res.status() }).toEqual({ bad, status: 404 });
  }
});

// ------------------------------------------------- which language the server picks
//
// THE LANGUAGE COMES FROM THE BROWSER AND NOTHING IN THE CODE DECIDES IT, and
// these are the cases that hold that. They go over HTTP with the header set
// exactly - see point 3 at the top for why not through a browser context - and
// they read the page the server really sent: its lang attribute, the tag in
// its language block, and one sentence out of it.
for (const [accept, want] of [
  ["de-DE,de;q=0.9", "de"],
  ["fr-FR,fr;q=0.9", "fr"],
  ["it-IT,it;q=0.9", "it"],
  ["es-ES,es;q=0.9", "es"],
  ["en-GB,en;q=0.9", "en"],
  // A region nobody has falls back to the language itself.
  ["fr-CA", "fr"],
  // A language nobody has falls back to English. Icelandic: no file in the
  // folder, and nothing in the code knows the name.
  ["is-IS", "en"],
  // Nothing asked for at all.
  ["", "en"],
  // The weights decide, not the order: Italian stands second and wins.
  ["de-DE;q=0.2,it-IT;q=0.9", "it"],
  // A header that is nonsense leaves the fall-back standing.
  ["*", "en"],
]) {
  test(`a browser asking for "${accept}" is served ${want}`, async ({ request }) => {
    const res = await request.get("/", { headers: { "Accept-Language": accept } });
    expect(res.status()).toBe(200);
    const html = await res.text();
    const v = blockOf(html);
    expect({ attr: /<html lang="([a-z-]+)"/.exec(html)?.[1], tag: v.tag })
      .toEqual({ attr: want, tag: want });
    // And the table really is that language's, not just the label on it.
    expect(v.main["login.head"]).toBe(tableOf(want)["login.head"]);
    // English comes along as the fall-back whenever it is not the chosen one.
    expect(Object.keys(v.base).length > 0).toBe(want !== "en");
    await Promise.resolve();
  });
}

// ------------------------------------------- and the page shows what it was served

test("the page shows the language it was served, and no other", async ({ shell }) => {
  const tag = await langOf(shell);
  expect(TAGS).toContain(tag);
  const key = await shownKey(shell, tag);
  const text = await rootText(shell);
  // Not one of the other four languages' wording for the same key. Five files
  // that happened to hold the same words there would make the case green
  // without anything having been chosen.
  const others = TAGS
    .filter((o) => o !== tag && tableOf(o)[key] !== tableOf(tag)[key])
    .filter((o) => text.includes(tableOf(o)[key]));
  expect({ key, others }).toEqual({ key, others: [] });
});

// ------------------------------------------------------- what catches a gap

// A KEY THE CHOSEN LANGUAGE HAS NOT GOT reads out of English. The case cannot
// wait for a translation to fall behind to measure it - the file test makes
// that impossible - so it takes the key out on the way to the page.
test("a key the page's language is missing reads out of English", async ({ shell }) => {
  const tag = await langOf(shell);
  // English is the fall-back, so it cannot also be the language under test.
  test.skip(tag === "en", "this run is in English, so there is no fall-back to measure");
  const key = await shownKey(shell, tag);
  expect(tableOf(tag)[key]).not.toBe(tableOf("en")[key]);
  await bend(shell, drop("main", key));
  await shell.goto("/");
  // English for the key it has not got - and never the key, never an empty
  // place. The document still says its own language: one sentence of it is
  // simply not written yet.
  await shows(shell, tableOf("en")[key]);
  expect(await langOf(shell)).toBe(tag);
  expect(await rootText(shell)).not.toContain(tableOf(tag)[key]);
});

// A KEY NO FILE HAS AT ALL can only come from a typo in the code, and the
// person must still read a sentence. It goes out of BOTH the chosen language
// and the fall-back on the way to the page.
test("a key no language has is a sentence on the screen, not the key", async ({ shell }) => {
  const tag = await langOf(shell);
  const key = await shownKey(shell, tag);
  await bend(shell, (v) => {
    drop("main", key)(v);
    if (Object.prototype.hasOwnProperty.call(v.base, key)) drop("base", key)(v);
  });
  await shell.goto("/");
  // The one sentence that can be said without the files: that the files are
  // not there. Whatever it says, it is a sentence for a person.
  await shows(shell, tableOf(tag)["boot.nolang"]);
  expect(await rootText(shell)).not.toContain(key);
});

// The page without a block at all - a server that could not read the folder.
// The shell must still say what is wrong, because a blank page is the one
// answer that helps nobody.
test("a page with no language block says so instead of going blank", async ({ shell }) => {
  await shell.route((url) => url.pathname === "/", async (route) => {
    const res = await route.fetch();
    const html = await res.text();
    const from = html.indexOf(BLOCK);
    if (from < 0) throw new Error("lang: the page carries no language block");
    const to = html.indexOf("</script>", from);
    await route.fulfill({ response: res, body: html.slice(0, from + BLOCK.length) + html.slice(to) });
  });
  await shell.goto("/");
  // No tables means no chosen language either, so the sentence comes in
  // English - the fall-back, as the rule says.
  await shows(shell, tableOf("en")["boot.nolang"]);
});

// ------------------------------------------------------------- placeholders

// A name nobody passed stays as it stands rather than becoming "undefined":
// a visible {nobody} is a bug somebody can report in the words they see.
test("a placeholder nobody filled in stays visible", async ({ shell }) => {
  const tag = await langOf(shell);
  const key = await shownKey(shell, tag);
  await bend(shell, (v) => { v.main[key] = "Ein Satz mit {nobody} darin"; });
  await shell.goto("/");
  await shows(shell, "Ein Satz mit {nobody} darin");
});

// And a name the caller DOES pass is replaced by its value. The taskbar entry
// of an open window is the one text of this surface that carries one:
// window.js names it with t("window.task", { title }).
test("a placeholder the caller fills in gets the value", async ({ shell }) => {
  // The same gap the desktop cases are measured against: without the public
  // half of a password seed this engine cannot finish a login, so there is no
  // window here to name. Measured, never assumed from the engine's name.
  await skipWithoutLogin(shell);
  await bend(shell, (v) => { v.main["window.task"] = "FENSTER[{title}]"; });
  await shell.goto("/");
  await signIn(shell);
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  // The window's own title went into the placeholder - not "undefined", and
  // not the sentence with {title} still in it.
  await expect(shell.locator("button.task")).toHaveAttribute("aria-label", "FENSTER[Rechner]");
});

// ---------------------------------------------------------------------------
// THE ONE THAT GUARDS THE WHOLE THING: no naked key on the screen.
//
// Every other case asks the lookup one question. This one asks the PAGE, and
// it is the only one that would catch a t("stor.saved") somebody typed wrongly
// in a branch no other case walks through.
test("no naked key stands on the first page", async ({ shell }) => {
  const keys = Object.keys(tableOf("de"));
  expect(keys.length).toBeGreaterThan(100);
  expect(await nakedKeys(shell, keys)).toEqual([]);
});

// The same sweep over the desktop: the first page is one sentence, the desktop
// and its start menu carry the taskbar, the search field and the accent names.
test("no naked key stands on the desktop", async ({ shell }) => {
  await skipWithoutLogin(shell);
  const keys = Object.keys(tableOf("de"));
  await signIn(shell);
  await expect(shell.getByRole("button", { name: "Start" })).toBeVisible();
  expect({ where: "desktop", bad: await nakedKeys(shell, keys) })
    .toEqual({ where: "desktop", bad: [] });
  await shell.getByRole("button", { name: "Start" }).click();
  expect({ where: "start menu", bad: await nakedKeys(shell, keys) })
    .toEqual({ where: "start menu", bad: [] });
});
