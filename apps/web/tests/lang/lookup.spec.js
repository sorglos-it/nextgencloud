// The lookup in the browser: which language it picks, what it does with a key
// nobody filled in, and - the one that matters most - that a naked key never
// reaches the screen.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { test, expect, signIn } from "../fixtures/page.js";

const WEB = fileURLToPath(new URL("../..", import.meta.url));
const tableOf = (tag) => JSON.parse(readFileSync(WEB + "/lang/" + tag + ".json", "utf8"));
// nakedKeys sweeps a page for text that is a key of the language files rather
// than a sentence out of them. Text nodes and the three attributes a person
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


// What the server answers, before any page asks. The folder is the list, so
// this is also the case that proves the discovery works at all.
test("the server names the languages it has", async ({ request }) => {
  const res = await request.get("/lang/index.json");
  expect(res.status()).toBe(200);
  const body = await res.json();
  expect([...body.languages].sort()).toEqual(["de", "en", "es", "fr", "it"]);
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

// THE LANGUAGE COMES FROM THE BROWSER, and each of these three cases sets its
// own: the whole point is that nothing in the code decides it.
for (const [locale, key, want] of [
  ["de-DE", "login.go", "Anmelden"],
  ["fr-FR", "login.go", "Se connecter"],
  ["it-IT", "login.go", "Accedi"],
  ["es-ES", "login.go", "Acceder"],
  ["en-GB", "login.go", "Sign in"],
]) {
  test(`a browser set to ${locale} reads ${locale.slice(0, 2)}`, async ({ browser }) => {
    const ctx = await browser.newContext({ locale });
    const page = await ctx.newPage();
    await page.goto("/");
    const got = await page.evaluate(async (k) => {
      const lang = await import("/assets/js/lang.js");
      return { tag: lang.tag, text: lang.t(k), docLang: document.documentElement.lang };
    }, key);
    expect(got).toEqual({ tag: locale.slice(0, 2), text: want, docLang: locale.slice(0, 2) });
    await ctx.close();
  });
}

test("a language nobody has falls back to English", async ({ browser }) => {
  // Icelandic: no file in the folder, and nothing in the code knows the name.
  const ctx = await browser.newContext({ locale: "is-IS" });
  const page = await ctx.newPage();
  await page.goto("/");
  const got = await page.evaluate(async () => {
    const lang = await import("/assets/js/lang.js");
    return { tag: lang.tag, text: lang.t("login.go") };
  });
  expect(got).toEqual({ tag: "en", text: "Sign in" });
  await ctx.close();
});

test("a region nobody has falls back to the language", async ({ browser }) => {
  const ctx = await browser.newContext({ locale: "fr-CA" });
  const page = await ctx.newPage();
  await page.goto("/");
  expect(await page.evaluate(async () => (await import("/assets/js/lang.js")).t("login.go")))
    .toBe("Se connecter");
  await ctx.close();
});

// A KEY THE CHOSEN LANGUAGE HAS NOT GOT reads out of English. The case cannot
// wait for a translation to fall behind to measure this - the file test above
// makes that impossible - so it takes the key OUT of the loaded French table
// and asks again. pick() and t() are the two halves of the lookup and this
// drives the second one on its own.
test("a key one language is missing reads out of English", async ({ browser }) => {
  const ctx = await browser.newContext({ locale: "fr-FR" });
  const page = await ctx.newPage();
  // The French file comes back without the key, so the page is in French and
  // does not have it. Everything else about the file stays as it is.
  await page.route("**/lang/fr.json", async (route) => {
    const res = await route.fetch();
    const body = await res.json();
    delete body["login.go"];
    await route.fulfill({ json: body });
  });
  await page.goto("/");
  const got = await page.evaluate(async () => {
    const lang = await import("/assets/js/lang.js");
    return { tag: lang.tag, missing: lang.t("login.go"), there: lang.t("login.password") };
  });
  // French for the keys it has, English for the one it has not, and never the
  // key itself and never "".
  expect(got).toEqual({ tag: "fr", missing: "Sign in", there: "Mot de passe" });
  await ctx.close();
});

test("a key no language has is a sentence, not the key", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const lang = await import("/assets/js/lang.js");
    return lang.t("no.such.key.anywhere");
  });
  expect(got).not.toBe("no.such.key.anywhere");
  expect(got).not.toBe("");
  // It is the one sentence that can be said without the files: that the files
  // are not there. Whatever it says, it is a sentence for a person.
  expect(got).toBe(tableOf("de")["boot.nolang"]);
});

test("a placeholder gets its value, and an unnamed one stays visible", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const lang = await import("/assets/js/lang.js");
    return {
      filled: lang.t("window.task", { title: "Rechner" }),
      unnamed: lang.t("window.task", {}),
    };
  });
  expect(got.filled).toBe("Fenster Rechner");
  // Not "Fenster undefined": a visible {title} is a bug somebody can report.
  expect(got.unnamed).toBe("Fenster {title}");
});

// ---------------------------------------------------------------------------
// THE ONE THAT GUARDS THE WHOLE THING: no naked key on the screen.
//
// Every other case here asks the lookup a question. This one asks the PAGE,
// and it is the only one that would catch a t("stor.saved") somebody typed
// wrongly in a branch no other case walks through: the text of a key is a word
// out of a programme, and a person must never read one.
//
// WHAT COUNTS AS A NAKED KEY: a text node, a title, a placeholder or an
// aria-label whose whole content is one of the keys of the language files.
// Not "anything with a dot in it" - a version number and a host name have dots
// too - so the list of keys is read off the disk and compared by name.
test("no naked key stands on the mask", async ({ shell }) => {
  const keys = Object.keys(tableOf("de"));
  expect(keys.length).toBeGreaterThan(100);
  expect(await nakedKeys(shell, keys)).toEqual([]);
});

test("no naked key stands on the desktop", async ({ shell }) => {
  const keys = Object.keys(tableOf("de"));
  await signIn(shell);
  await expect(shell.getByRole("button", { name: "Start" })).toBeVisible();
  const sweep = async (where) => ({ where, bad: await nakedKeys(shell, keys) });

  expect(await sweep("desktop")).toEqual({ where: "desktop", bad: [] });
  // The start menu, where the search field and the two foot buttons are.
  await shell.getByRole("button", { name: "Start" }).click();
  expect(await sweep("start menu")).toEqual({ where: "start menu", bad: [] });
});
