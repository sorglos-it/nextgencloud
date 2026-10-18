// A DATABASE THAT IS BUSY, AND WHAT THE PERSON IN FRONT OF THE SCREEN READS.
//
// WHY THIS FILE EXISTS. Points 49, 52 and 53 of docs/vertraege-v1.2.md built
// the answer on the server: a database another process holds ends in 503 with
// Retry-After: 5 and a log line reason=db-busy, while one that is away gets
// the same 503 WITHOUT the header (apps/server-api/classes/httpapi/busy.go,
// FailCoded). The code is E_UNREACHABLE in both, because a word of its own
// would be a new wire.Code in frozen shared/wire/wire.go:450-462 - BLOCKED,
// P9. All three points end with the same line in their open list: what the
// person READS at 503 + Retry-After was never measured, only read in the
// source. This file measures it, in a browser, rendered.
//
// WHAT IT MEASURES AND WHAT IT CANNOT. It measures the two screens that are
// on before anybody is logged in - the login mask and the first run - against
// the two answers the server really gives, and it asks the rendered alert
// what it says. It cannot measure the server: the mock stands in for it, and
// what the mock does is held against busy.go line by line in mock/api.js
// (sendFault). A number here is a number about the SHELL.
import { test, expect, skipWithoutLogin, skipWithoutSetup, skipWithoutShell, standInOn } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";
import { PASSWORD, USER_NAME } from "../fixtures/logins.js";

// The one-time code of shared/testdata/session.json, as shell/setup.spec.js
// takes it: the common truth of Go and JavaScript, so no code is written here.
const CODE = vectors("session.json").setup.code;

test.beforeEach(async ({ shell }) => {
  await skipWithoutShell(shell);
  await standInOn(shell);
});

async function tellMock(shell, query) {
  const ok = await shell.evaluate(async (q) => {
    const res = await fetch("/api/v1/mock?" + q, { method: "POST", cache: "no-store" });
    return res.ok;
  }, query);
  expect(ok, "the mock took the instruction").toBe(true);
}

// The wait is the same DECISION shell/login.spec.js makes and a shorter form
// of it: the attempt is over when the shell SAYS so - the buttons back on, or
// a sentence in the alert - and not after a guess at how long Argon2id takes
// on a loaded machine (loginState there says why at length).
//
// It is shorter in one way that is named rather than copied: login.spec.js
// also knows the state "the mask is gone and the desktop is still being
// built", and every case here ends AT THE MASK, with a sentence in it. A case
// of this file that ever did get a desktop would wait here until its own
// timeout - which is a red case and never a green one.
async function attemptOver(shell) {
  await expect.poll(async () => shell.evaluate(() => {
    const box = document.querySelector("form.loginbox");
    if (box === null) return document.querySelector("#taskbar") !== null;
    const off = [...box.querySelectorAll("button")].every((b) => b.disabled);
    const said = box.querySelector('[role="alert"]');
    return !(off && (said === null || said.textContent === ""));
  }), { message: "the login attempt is over", timeout: 0 }).toBe(true);
}

async function tryLogin(shell, kind) {
  await tellMock(shell, "fault=" + kind);
  await shell.getByLabel("Benutzername").fill(USER_NAME);
  await shell.getByLabel("Passwort").fill(PASSWORD);
  await shell.getByRole("button", { name: "Anmelden", exact: true }).click();
  await attemptOver(shell);
}

// THE CASE THE THREE POINTS ASKED FOR. The sentence names the seconds out of
// the header, and it is a DIFFERENT sentence from the one a database that is
// away gets - which is the whole of what Retry-After buys while E_BUSY is
// blocked. The number comes out of the ANSWER and is not a 5 written here: a
// case that spelled the 5 itself would pass for any header at all.
test("a busy database says how long, and names the seconds the API sent", async ({ shell }) => {
  await skipWithoutLogin(shell);
  let sent = null;
  shell.on("response", (r) => {
    if (r.url().includes("/api/v1/login")) sent = r.headers()["retry-after"] ?? null;
  });
  await tryLogin(shell, "busy");
  expect(sent, "the API sent a Retry-After beside the 503").not.toBeNull();
  await expect(shell.getByRole("alert"))
    .toHaveText("Dienst gerade überlastet. Bitte in " + sent + " Sekunden noch einmal versuchen.");
});

// The mask stays usable, and that is the difference to E_LOCKED. A lock is
// the API counting against whoever is trying; busy is a state of the
// installation and says nothing about this person, so taking the buttons away
// for five seconds would take a choice away for nothing.
test("a busy database leaves the mask usable at once", async ({ shell }) => {
  await skipWithoutLogin(shell);
  await tryLogin(shell, "busy");
  await expect(shell.getByRole("button", { name: "Anmelden", exact: true })).toBeEnabled();
  await expect(shell.getByLabel("Benutzername")).toHaveValue(USER_NAME);
});

// The other half of the same measurement: without the header there is nothing
// left that tells busy from away, so the sentence must NOT name a number. A
// shell that said "in 5 seconds" to a database that is gone would be putting
// a number nobody sent in front of somebody.
test("a database that is away says later, and names no number", async ({ shell }) => {
  await skipWithoutLogin(shell);
  let sent = "missing";
  shell.on("response", (r) => {
    if (r.url().includes("/api/v1/login")) sent = r.headers()["retry-after"] ?? null;
  });
  await tryLogin(shell, "away");
  expect(sent, "no Retry-After beside this 503").toBeNull();
  await expect(shell.getByRole("alert"))
    .toHaveText("Dienst nicht erreichbar. Bitte später erneut versuchen.");
});

// AND THE ONE THAT MATTERS MOST, because it is point 50's mistake moved one
// screen along. A busy database during the FIRST RUN does not arrive as
// E_UNREACHABLE at all: setup.go's fault() turns every E_UNREACHABLE on that
// path into E_INTERNAL, and its own comment says "nothing on this path can
// say 503 any more". Until this case existed, E_INTERNAL fell through to the
// sentence for a code that does not fit - so the person was told to CHECK THE
// ONE-TIME CODE, and that code is good ONCE. Whoever believed it installed
// NGC again for nothing.
test("a busy database during the first run never blames the one-time code", async ({ shell }) => {
  // A FIRST RUN, so the same gate as shell/setup.spec.js: where no password
  // key can be made out of a seed, shell/main.js refuses the setup outright
  // and there is no code mask on the page (measured, Firefox 141).
  await skipWithoutSetup(shell);
  await tellMock(shell, "setup=1");
  await shell.route("**/config.json", async (route) => {
    const answer = await route.fetch();
    const config = JSON.parse(await answer.text());
    delete config.anchor;
    await route.fulfill({
      status: 200, contentType: "application/json; charset=utf-8", body: JSON.stringify(config),
    });
  });
  await shell.reload();
  await expect(shell.getByLabel("Einmal-Code")).toBeVisible();

  let status = 0;
  shell.on("response", (r) => {
    if (r.url().includes("/api/v1/setup")) status = r.status();
  });
  // THE RIGHT CODE, so the only reason this can go wrong is the database. A
  // case that typed a wrong code would measure the wrong-code sentence and
  // pass whatever the busy answer did.
  await tellMock(shell, "fault=busy");
  await shell.getByLabel("Einmal-Code").fill(CODE);
  await shell.getByRole("button", { name: "Weiter" }).click();

  const said = shell.getByRole("alert");
  await expect(said).toBeVisible();
  expect(status, "setup.go turns the 503 into a 500 before it leaves").toBe(500);
  // The two halves of the promise: nothing about the code, and the sentence
  // that says the code is still good.
  await expect(said).not.toContainText("Einmal-Code passt nicht");
  await expect(said).toHaveText("Die Anlage antwortet gerade nicht. Der Einmal-Code bleibt "
    + "gültig. Bitte in einem Moment noch einmal versuchen.");
  // And the person is still where they can use it: the field is there with
  // what they typed, so there is nothing to type a second time.
  await expect(shell.getByLabel("Einmal-Code")).toHaveValue(CODE);
});

// THE FLOOR UNDER THE FILES, and it is the one case of this block that the
// language suites do not hold. Everything else that stood here until the two
// language layers were made one - five languages answer, the five files hold
// the same keys, a key nobody has, a hostile tag cannot become a path - is
// measured against the surviving lookup in tests/lang/files.spec.js and
// tests/lang/lookup.spec.js, which ask the same questions of
// public/assets/js/lang.js and the /lang/ route of the one entry point.
// Nothing was dropped to go green; the questions moved to the module that
// answers them.
//
// What is measured here: every sentence of these two masks is a fetch away,
// and a fetch can fail. MEASURED before the files existed, rendered in
// chromium: with every language file refused and a wrong password typed, the
// alert of the login mask held "" and was not laid out at all
// (offsetHeight 0) - a mask that says nothing where it used to say
// "Anmeldung fehlgeschlagen.".
//
// It is not an invented state: a release that ships assets/js/ and forgets
// apps/web/lang/ is exactly this, and the packer names what goes out file by
// file (tools/release/extra-web.txt, release-packer branch, unmerged).
//
// The sentence it must say is lang.js's NOLANG - the one literal in that
// folder, and the only sentence that cannot come out of the files it is
// about. The run asks for German (playwright.config.js), so German is what
// the floor has to answer.
test("a mask with no language file at all is not silent, and says what to do", async ({ shell }) => {
  await skipWithoutLogin(shell);
  await shell.route("**/lang/*.json", (route) => route.abort());
  await shell.reload();

  // WHAT REALLY HAPPENS, measured rather than hoped for, and the price is
  // named: lang.js answers EVERY key out of NOLANG when it has no file at
  // all, so the mask is not in English - it is the one sentence, repeated,
  // captions and all. "Benutzername" is not on the page, which is why this
  // case cannot type into the mask and does not try.
  //
  // That is loud and it is not silent, which is what this case is for. The
  // layer that was retired in this merge had a per-key English floor for its
  // 17 sentences and would have shown an English mask here instead. Whether
  // the surviving lookup should compile the English file in the same way is a
  // DECISION, not a defect, and it is not taken inside a merge - it is named
  // here and in the report so the next person sees the choice rather than
  // inheriting it.
  // ENGLISH, and that is measured rather than expected: the route takes
  // /lang/index.json with the rest, so the shell cannot learn that a German
  // file exists at all. pick() then has an empty list and answers FALLBACK,
  // and NOLANG is read under that tag. A person whose browser asks for German
  // reads the English sentence here - named, because it is the one place in
  // this interface where the language of a sentence is not the language the
  // browser asked for, and there is nothing left that could know better.
  const sentence = "NGC could not load its language files. Please reload the page.";
  const said = shell.locator("form.loginbox");
  await expect(said).toBeVisible();
  const texts = await said.locator("label, button, h1, h2, p").allInnerTexts();
  expect(texts.length, "the mask is laid out at all").toBeGreaterThan(2);
  for (const text of texts) {
    expect(text.trim(), "every caption is the one sentence that says what to do")
      .toBe(sentence);
  }
  // Laid out, not merely present: an element with no text has no height, and
  // an empty mask is what this case was first written against.
  expect(await said.evaluate((n) => n.offsetHeight), "the mask is on the page").toBeGreaterThan(0);
});

// The floor's own drift risk: NOLANG is the only table in this interface that
// is NOT a language file, so nothing holds it to the five. A tag whose line
// is missing there answers out of English, and a person who reads neither
// gets no instruction at all.
test("the sentence of the floor is there in all five languages", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const idx = await (await fetch("/lang/index.json", { cache: "no-store" })).json();
    const { NOLANG } = await import("/assets/js/lang.js");
    return { languages: idx.languages, tags: Object.keys(NOLANG) };
  });
  expect(got.languages.length, "the folder holds languages at all").toBeGreaterThan(1);
  expect([...got.tags].sort(), "one line of the floor per language file")
    .toEqual([...got.languages].sort());
});
