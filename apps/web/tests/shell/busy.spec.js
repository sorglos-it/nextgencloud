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
import { test, expect, skipWithoutLogin, skipWithoutShell, standInOn } from "../fixtures/page.js";
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

// The same wait every login case in this suite uses: what the shell SAYS,
// rather than a guess at how long Argon2id takes on a loaded machine
// (shell/login.spec.js, loginState, says why at length).
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

// The five languages are files, so what is measured is that a FILE decides
// the sentence - not that French is spelled right, which no test can say. It
// asks for the same key in every one of the five and holds the answers
// against each other: five answers, none empty, no placeholder left standing,
// and the German one the text the cases above pin.
test("every one of the five languages answers, and none of them twice", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const lang = await import("/assets/js/shell/lang.js");
    const out = {};
    for (const name of lang.LANGS) {
      await lang.load([name]);
      out[name] = { chosen: lang.language(), text: lang.t("err.busy", { seconds: 5 }) };
    }
    return out;
  });
  expect(Object.keys(got), "five files").toEqual(["de", "en", "fr", "it", "es"]);
  for (const [name, g] of Object.entries(got)) {
    expect(g.chosen, name + " was chosen").toBe(name);
    expect(g.text.length, name + " says something").toBeGreaterThan(10);
    expect(g.text, name + " filled the placeholder in").toContain("5");
    expect(g.text, name + " left no placeholder standing").not.toContain("{seconds}");
  }
  const texts = Object.values(got).map((g) => g.text);
  expect(new Set(texts).size, "five different sentences").toBe(5);
  expect(got.de.text).toBe("Dienst gerade überlastet. Bitte in 5 Sekunden noch einmal versuchen.");
});

// Every file holds the same keys, which is the one thing that can go wrong
// without anybody noticing: a key missing in Italian is a mask with an empty
// line in it, and only in Italian. The house rule asks for the English text
// as the fallback, so the fallback is measured too - by asking for a key that
// is in no file at all.
test("the five files hold the same keys, and a key nobody has is empty", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const lang = await import("/assets/js/shell/lang.js");
    const keys = {};
    for (const name of lang.LANGS) {
      const res = await fetch("/assets/lang/" + name + ".json", { cache: "no-store" });
      keys[name] = Object.keys(await res.json()).sort();
    }
    await lang.load(["it"]);
    return { keys, missing: lang.t("no.such.key"), notAString: lang.t(null) };
  });
  const german = got.keys.de;
  expect(german.length, "there are keys at all").toBeGreaterThan(10);
  for (const [name, keys] of Object.entries(got.keys)) {
    expect(keys, name + " holds the same keys as de").toEqual(german);
  }
  expect(got.missing, "a key nobody has is empty, not the key itself").toBe("");
  expect(got.notAString, "a key that is no string is empty too").toBe("");
});

// A language tag from outside never becomes a path. pick() answers with one
// of the five and nothing else, which is what keeps a tag like
// "../../etc/ngc/web.json" out of the address fetchLang builds.
test("a language tag from outside cannot become a path", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const { pick, LANGS, FALLBACK } = await import("/assets/js/shell/lang.js");
    return {
      hostile: pick(["../../etc/ngc/web.json", "/etc/passwd", "de_DE/../.."]),
      region: pick(["de-CH"]),
      order: pick(["pt", "nl", "fr", "de"]),
      none: pick([]),
      notAList: pick("fr"),
      junk: pick([null, 42, {}, "it"]),
      langs: [...LANGS],
      fallback: FALLBACK,
    };
  });
  // "de_DE/../.." lower-cased has no "-" in it, so its primary subtag is the
  // whole string and that is in no list: it leaves as the fallback.
  expect(got.hostile, "none of those is a language").toBe("en");
  expect(got.region, "a region is the same file as its language").toBe("de");
  expect(got.order, "the browser's own order of preference decides").toBe("fr");
  expect(got.none).toBe("en");
  expect(got.notAList, "a string is no list of offers").toBe("en");
  expect(got.junk, "what is no string is skipped, not refused").toBe("it");
  expect(got.langs).toEqual(["de", "en", "fr", "it", "es"]);
  expect(got.fallback).toBe("en");
});
