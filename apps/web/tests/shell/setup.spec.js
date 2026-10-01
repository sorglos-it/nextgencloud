// The first run: a fresh installation, the one-time code, and the first
// admin. This is the first minute somebody ever spends with NGC, so what the
// cases below measure is mostly what that person sees - and, twice, what the
// wire does NOT carry.
//
// FOUR PLACES WHERE THIS FILE DIFFERS FROM THE TASK BRIEF, each measured
// rather than argued. They are the same three the login's own file already
// names, one of them twice over:
//
//  1. The brief steers the mock with `window.__ngcTest = { setup: true }` and
//     then calls shell.reload(). A reload throws that window away, so the
//     shell would read nothing at all - and a hook on window that decides
//     whether the SETUP runs is a hook anything else on the page could set,
//     on the one page where the whole installation is at stake. What steers
//     the mock here is the mock's own test door (/api/v1/mock, which no line
//     of the shell knows) and a route over /config.json, which is what a
//     server without a paired web controller really sends.
//  2. The brief's first case reads `window.__ngcSeenSetupRequests ?? 0` and
//     expects 0. Nothing anywhere sets that name, so the expression is 0 for
//     every implementation there could be, including one that fetches the
//     offer before the code is typed: the assertion cannot fail for the thing
//     it is named after. It is replaced here by asking the MOCK how many
//     times GET /api/v1/setup reached it - a number this side cannot produce
//     by itself. Measured: with the fetch moved in front of the code, the
//     mock answers "1" and the case goes red.
//  3. The brief fills `getByLabel("Passwort")` on a mask that also carries
//     "Passwort wiederholen". getByLabel matches an accessible name as a
//     substring, so that locator finds two fields and every fill of it ends
//     in a strict-mode violation (measured). exact: true here.
//  4. The brief's `test.use({ storageState: ... })` is left out. Every case
//     gets a fresh browser context anyway, and storageState says nothing
//     about IndexedDB, which is the one store this flow writes (the anchor).
//     What the cases need instead is stated where they need it.
import { test, expect, hasSessionKeys } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";
import { PASSWORD, USER_NAME } from "../fixtures/logins.js";

// The one-time code of shared/testdata/session.json. It is the common truth
// of Go and JavaScript for this flow, so neither the mock nor this file
// writes a code of its own.
const CODE = vectors("session.json").setup.code;

// The same code as somebody really types it: lower case, spaces instead of
// dashes. session.normalizeSetupCode has to make the canonical one of it
// before anything is asked of the API.
const TYPED = CODE.toLowerCase().replace(/-/g, " ");

// The one sentence a code that does not fit gets. It says what to do and
// nothing about HOW wrong the code was.
const NO_MATCH = "Der Einmal-Code passt nicht zu dieser Anlage. "
  + "Bitte den Code prüfen und noch einmal versuchen.";

// MEASURED, never read off the engine's name: shell/main.js needs Ed25519 and
// X25519 before it draws anything, and the WebKit that Playwright brings has
// neither - so there is no setup mask to type into there either. The boot
// says so instead, and shell/boot.spec.js is the case that pins THAT and runs
// everywhere. The day this WebKit gains the two, hasSessionKeys answers true
// by itself and every case below runs there with no edit here.
test.beforeEach(async ({ shell }) => {
  test.skip(!(await hasSessionKeys(shell)),
    "no Ed25519 and no X25519 in this engine's Web Crypto: the shell stops before the setup");
});

// One server answers the whole run, so a case that made the installation
// fresh puts it back - otherwise the file after this one logs in to an
// installation this one set up. A setup that went through does that by
// itself (the API has an admin again), and this is for the cases that did
// not get that far.
test.afterEach(async ({ shell }) => {
  await tellMock(shell, "setup=0");
});

async function tellMock(shell, query) {
  const ok = await shell.evaluate(async (q) => {
    const res = await fetch("/api/v1/mock?" + q, { method: "POST", cache: "no-store" });
    return res.ok;
  }, query);
  expect(ok, "the mock took the instruction").toBe(true);
}

async function askMock(shell, query) {
  return shell.evaluate(async (q) => {
    const res = await fetch("/api/v1/mock?" + q, { cache: "no-store" });
    return res.text();
  }, query);
}

// fresh puts the shell in front of an installation nobody has set up yet.
//
// Two things make one: the API has no admin (the mock's test door), and
// /config.json carries no anchor, which is what public/index.php really
// sends while the web controller is not paired - the member is left out, and
// net/config.js reads an absent anchor as none. The route takes the server's
// OWN answer and drops that one member, so the addresses and the relying
// party stay whatever the test installation has; a body written here would
// be a second configuration to keep in step.
async function fresh(shell, query = "") {
  await tellMock(shell, "setup=1" + (query === "" ? "" : "&" + query));
  await shell.route("**/config.json", async (route) => {
    const answer = await route.fetch();
    const config = JSON.parse(await answer.text());
    delete config.anchor;
    await route.fulfill({
      status: 200, contentType: "application/json; charset=utf-8", body: JSON.stringify(config),
    });
  });
  await shell.reload();
}

test("the code is asked for first, and the offer is checked with it", async ({ shell }) => {
  await fresh(shell);
  await expect(shell.getByLabel("Einmal-Code")).toBeVisible();
  // The offer is fetched only after the code has been typed: without the
  // code its MAC cannot be checked, and an offer nobody can check is an
  // offer nobody may read. The mock counts, not the page.
  expect(await askMock(shell, "ask=setup"), "nothing asked before the code").toBe("0");

  await shell.getByLabel("Einmal-Code").fill(TYPED);
  await shell.getByRole("button", { name: "Weiter" }).click();
  await expect(shell.getByLabel("Benutzername")).toBeVisible();
  await expect(shell.getByText("API-Server")).toBeVisible();
  expect(await askMock(shell, "ask=setup"), "asked once, with the code in hand").toBe("1");
});

test("a man in the middle with other keys is refused", async ({ shell }) => {
  // The mock MACs its offer with ANOTHER code: whoever does not hold the code
  // cannot make that MAC, which is the whole point of it. Its nodes carry
  // other keys, so the offer is a real one - just not this installation's.
  // Anything of it that reached the screen would show up below.
  await fresh(shell, "offer=foreign");
  await shell.getByLabel("Einmal-Code").fill(CODE);
  await shell.getByRole("button", { name: "Weiter" }).click();
  await expect(shell.getByRole("alert")).toHaveText(NO_MATCH);
  // Nothing of the offer may be shown: it was never read.
  await expect(shell.getByText("api-")).toHaveCount(0);
  await expect(shell.getByText("wss://")).toHaveCount(0);
  await expect(shell.getByLabel("Benutzername")).toHaveCount(0);
});

test("a wrong code says nothing about how wrong it was", async ({ shell }) => {
  await fresh(shell);
  // Too short: there is nothing to ask the API with, so nothing is asked.
  await shell.getByLabel("Einmal-Code").fill("ABCDE-FGHJK");
  await shell.getByRole("button", { name: "Weiter" }).click();
  await expect(shell.getByRole("alert")).toHaveText("Der Einmal-Code ist nicht vollständig.");
  expect(await askMock(shell, "ask=setup"), "an incomplete code asks nothing").toBe("0");

  // Complete, and wrong in one character. It gets the same sentence a code
  // that is wrong in twenty gets, and the same one a man in the middle gets:
  // the sentence must not say how close anybody got.
  const nearly = CODE.slice(0, -1) + (CODE.endsWith("W") ? "X" : "W");
  await shell.getByLabel("Einmal-Code").fill(nearly);
  await shell.getByRole("button", { name: "Weiter" }).click();
  await expect(shell.getByRole("alert")).toHaveText(NO_MATCH);

  // Wrong in every character, and the answer does not change.
  await shell.getByLabel("Einmal-Code").fill("22222-33333-44444-55555");
  await shell.getByRole("button", { name: "Weiter" }).click();
  await expect(shell.getByRole("alert")).toHaveText(NO_MATCH);
});

test("the one-time code never travels", async ({ shell }) => {
  const seen = [];
  shell.on("request", (r) => seen.push((r.postData() ?? "") + " " + r.url()));
  const frames = [];
  shell.on("websocket", (ws) => {
    ws.on("framesent", (f) => frames.push(String(f.payload)));
    ws.on("framereceived", (f) => frames.push(String(f.payload)));
  });
  const logged = [];
  shell.on("console", (m) => logged.push(m.text()));

  await fresh(shell);
  await shell.getByLabel("Einmal-Code").fill(CODE);
  await shell.getByRole("button", { name: "Weiter" }).click();
  await shell.getByLabel("Benutzername").fill(USER_NAME);
  await shell.getByLabel("Passwort", { exact: true }).fill(PASSWORD);
  await shell.getByLabel("Passwort wiederholen").fill(PASSWORD);
  await shell.getByRole("button", { name: "Einrichten" }).click();
  await expect(shell.locator("#taskbar")).toBeVisible({ timeout: 60_000 });

  // The code and the password as they could possibly be spelled on a wire:
  // plainly, form encoded, and in both base64 alphabets - base64url is the
  // one the contracts use, plain base64 the one everything else uses. The
  // four groups of the code stand for themselves as well: a shell that sent
  // the code in pieces would send these.
  const spellings = (s) => {
    const b64 = btoa(s);
    return [s, encodeURIComponent(s), b64, b64.replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")];
  };
  const forbidden = [...spellings(CODE), ...spellings(TYPED), ...spellings(PASSWORD), ...CODE.split("-")];
  for (const needle of forbidden) {
    expect(seen.join("\n"), needle).not.toContain(needle);
    expect(frames.join("\n"), needle).not.toContain(needle);
    expect(logged.join("\n"), needle).not.toContain(needle);
  }
  // A search that found nothing because there was nothing to search would
  // pass every line above. The setup and the login really went over this
  // wire, and the socket really opened.
  expect(seen.join("\n"), "the setup really went over this wire").toContain("/api/v1/setup");
  expect(seen.join("\n"), "the login after it did too").toContain("/api/v1/login/finish");
  expect(frames.length, "the session really opened a socket").toBeGreaterThan(0);
});

test("a setup that went through never leaves the person with nothing", async ({ shell }) => {
  await fresh(shell);
  await shell.getByLabel("Einmal-Code").fill(CODE);
  await shell.getByRole("button", { name: "Weiter" }).click();
  await shell.getByLabel("Benutzername").fill(USER_NAME);
  await shell.getByLabel("Passwort", { exact: true }).fill(PASSWORD);
  await shell.getByLabel("Passwort wiederholen").fill(PASSWORD);
  // The sign-in that follows the setup runs into a locked login. This is the
  // worst moment there is: the installation IS set up by then, the one-time
  // code is spent and there is no second one - so the one thing that must not
  // happen is a page with nothing on it.
  await tellMock(shell, "lock=30");
  await shell.getByRole("button", { name: "Einrichten" }).click();
  await expect(shell.getByText("NGC ist eingerichtet. Bitte jetzt anmelden.")).toBeVisible();
  await expect(shell.getByRole("button", { name: "Anmelden", exact: true })).toBeVisible();

  // And it really is set up: a reload finds the anchor this browser kept -
  // /config.json still carries none - and goes straight to the mask, with no
  // setup in front of it. That is the one measurement of rememberAnchor
  // there is from outside.
  await shell.reload();
  await expect(shell.getByRole("button", { name: "Anmelden", exact: true })).toBeVisible();
  await expect(shell.getByLabel("Einmal-Code")).toHaveCount(0);
});

test("the code is kept nowhere once it has been used", async ({ shell }) => {
  await fresh(shell);
  await shell.getByLabel("Einmal-Code").fill(CODE);
  await shell.getByRole("button", { name: "Weiter" }).click();
  await expect(shell.getByLabel("Benutzername")).toBeVisible();

  // The field is gone with the step it belonged to, and the address line
  // never carried it.
  await expect(shell.getByLabel("Einmal-Code")).toHaveCount(0);
  expect(shell.url()).not.toContain(CODE.slice(0, 5));

  // Neither store holds it, under any key. IndexedDB is checked in the one
  // place this flow writes to: shell/trustlist.js's own database.
  const kept = await shell.evaluate(async (code) => {
    const out = [];
    for (const store of [localStorage, sessionStorage]) {
      for (let i = 0; i < store.length; i++) {
        const k = store.key(i);
        out.push(k + "=" + store.getItem(k));
      }
    }
    const db = await new Promise((ok) => {
      const req = indexedDB.open("ngc", 1);
      req.onsuccess = () => ok(req.result);
      req.onerror = () => ok(null);
      req.onupgradeneeded = () => {
        const d = req.result;
        for (const n of ["trust", "setup"]) if (!d.objectStoreNames.contains(n)) d.createObjectStore(n);
      };
    });
    if (db !== null) {
      for (const name of [...db.objectStoreNames]) {
        const all = await new Promise((ok) => {
          const req = db.transaction(name, "readonly").objectStore(name).getAll();
          req.onsuccess = () => ok(req.result);
          req.onerror = () => ok([]);
        });
        for (const v of all) out.push(JSON.stringify(v, (_, x) => (x instanceof Uint8Array ? [...x] : x)));
      }
      db.close();
    }
    // Everything that is anywhere, as one string, plus the marker that says
    // the search really had something to search.
    return { all: out.join("\n"), n: out.length, code };
  }, CODE);
  expect(kept.all).not.toContain(CODE);
  for (const group of CODE.split("-")) expect(kept.all, group).not.toContain(group);
});
