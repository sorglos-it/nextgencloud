import { test, expect, signIn, skipWithoutLogin } from "../fixtures/page.js";

// The app window: the sandboxed frame an app's user interface runs in.
//
// SINCE TASK 18 the beforeEach really does log anna in, the same one line as
// desktop.spec.js. The app, its proof and its ui-tar still come from the
// stand-in in shell/fake-link.js: the login decides who is in, not where the
// desktop's bytes come from.
//
// SINCE TASK 25 the app in the frame is the real terminal - its own page,
// stylesheet and script, out of apps/web/assets/apps/terminal/ - instead of a
// fixture written into the stand-in. What stands in is its SERVICE, which the
// controller builds; tests/shell/terminal.spec.js says what that costs.
//
// WHAT IS REAL HERE AND WHAT STANDS IN. Real: the store key is an Ed25519 key
// the page makes, the catalog carries a real signature over the real UI hash,
// and the shell reads the proof with core.parseUIProof / core.verifyUIProof,
// hashes the ui-tar it received and walks it with uitar.walkUI. Standing in:
// where the store key comes from - Task 20 opens the trust list, and until
// then the stand-in hands the shell a list holding that one store.
test.beforeEach(async ({ shell }) => {
  // shell/main.js needs Ed25519 and X25519 before it builds anything, and the
  // WebKit Playwright brings has neither. There is no desktop to test there,
  // and the proof of a UI is an Ed25519 signature either way.
  await skipWithoutLogin(shell);
  await signIn(shell);
  await expect(shell.locator("#taskbar")).toBeVisible();
});

async function openTerminal(shell) {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Terminal" }).click();
}

test("an app runs only when its ui-tar fits the signed proof", async ({ shell }) => {
  await openTerminal(shell);
  const frame = shell.frameLocator(".appframe");
  // The app really drew its page: the field it asks for commands with is
  // there. Since task 25 the app in the frame is the real terminal, so what
  // is looked for here is a part of its own surface and not a word of a
  // fixture nobody ships.
  await expect(frame.getByRole("textbox", { name: "Eingabe" })).toBeVisible();
});

test("a ui-tar that does not fit the proof is refused", async ({ shell }) => {
  await shell.evaluate(() => { window.__ngcTest.swapUI = true; });
  await openTerminal(shell);
  await expect(shell.getByRole("alert")).toHaveText(
    "Die Oberfläche dieser App ist nicht echt. NGC führt sie nicht aus.");
  await expect(shell.locator(".appframe")).toHaveCount(0);
});

test("an entry page that is not in the ui-tar is refused", async ({ shell }) => {
  await shell.evaluate(() => { window.__ngcTest.entry = "nichtda.html"; });
  await openTerminal(shell);
  await expect(shell.getByRole("alert")).toHaveText(
    "Die Startseite dieser App fehlt. NGC führt sie nicht aus.");
  await expect(shell.locator(".appframe")).toHaveCount(0);
});

test("an app reaches neither the shell nor the network", async ({ shell }) => {
  await openTerminal(shell);
  const frame = shell.frameLocator(".appframe");
  // The app really drew its page: the field it asks for commands with is
  // there. Since task 25 the app in the frame is the real terminal, so what
  // is looked for here is a part of its own surface and not a word of a
  // fixture nobody ships.
  await expect(frame.getByRole("textbox", { name: "Eingabe" })).toBeVisible();
  // FIVE QUESTIONS, BECAUSE EACH WALL IS A DIFFERENT ONE. Measured, and each
  // number is the reason its line is here:
  //
  //   parent, storage - the opaque origin. Nothing else stops them.
  //   fetch  - the opaque origin as well, NOT the policy: with connect-src
  //            set to * it still fails, because the answer carries no
  //            Access-Control-Allow-Origin and the shell's own
  //            Cross-Origin-Resource-Policy turns a no-cors one down too.
  //   script - that the policy is IN FORCE AT ALL in the written document.
  //            The frame builds its page with document.write, and that is
  //            exactly the call that could have thrown the policy away; it
  //            does not, and this line is what says so.
  //   socket - connect-src, which nothing else above measures.
  const out = await frame.locator("body").evaluate(async () => {
    const r = { parent: "no", fetch: "no", script: "no", socket: "no", storage: "no" };
    try { r.parent = String(window.parent.document.title); } catch { r.parent = "blocked"; }
    try { await fetch("/config.json"); r.fetch = "reached"; } catch { r.fetch = "blocked"; }
    r.script = await new Promise((res) => {
      const s = document.createElement("script");
      s.onload = () => res("reached");
      s.onerror = () => res("blocked");
      s.src = "/assets/js/shell/theme.js";
      document.head.append(s);
      setTimeout(() => res("reached"), 900);
    });
    // A socket blocked by the policy throws in WebKit and fires a violation
    // in Chromium - measured, both - so both count as blocked and neither
    // engine is asked for the other's signal.
    r.socket = await new Promise((res) => {
      document.addEventListener("securitypolicyviolation", (e) => {
        if (e.violatedDirective.startsWith("connect-src")) res("blocked");
      });
      setTimeout(() => res("reached"), 900);
      try {
        new WebSocket("ws://" + location.host + "/");
      } catch { res("blocked"); }
    });
    try { localStorage.setItem("x", "1"); r.storage = "reached"; } catch { r.storage = "blocked"; }
    return r;
  });
  expect(out).toEqual({
    parent: "blocked", fetch: "blocked", script: "blocked", socket: "blocked", storage: "blocked",
  });
});

// THE BRIEF'S CASE COULD NOT FAIL FOR ITS OWN REASON, in two ways, so it is
// repaired here and both are reported.
//
//  1. It sent the message without opening an app first. With no frame there
//     is no listener at all, and the count stays put whatever the bridge
//     does - green even against a bridge that checks nothing.
//  2. Its message carried no token. A bridge with no check on e.source at all
//     still turns it down at the token, so the case would have gone green
//     while the rule it is named after was gone.
//
// So the terminal is opened first, and the message is the real thing: this
// frame's own token, an action the app really has. The ONE thing that is
// wrong with it is where it comes from, which is the rule under test.
test("a message that does not come from the frame is ignored", async ({ shell }) => {
  await openTerminal(shell);
  // Waiting for the app's OWN channel to be open, not only for the page to be
  // there: a channel still on its way would land in the count while this case
  // is measuring it. The field is enabled when the channel stands.
  const frame = shell.frameLocator(".appframe");
  await expect(frame.getByRole("textbox", { name: "Eingabe" })).toBeEnabled();
  const token = await frame.locator("script[data-token]").getAttribute("data-token");
  expect(token, "the frame's document carries the word the bridge asks for").toBeTruthy();
  const out = await shell.evaluate(async (t) => {
    const before = window.__ngcChannelsOpened;
    window.postMessage(
      { ngc: 1, token: t, id: 1, op: "call", service: "terminal", action: "shell" }, "*");
    await new Promise((r) => setTimeout(r, 100));
    return window.__ngcChannelsOpened - before;
  }, token);
  expect(out).toBe(0);
});

// The frame is the one place in NGC that builds a document out of bytes, so
// what it refuses to build is worth its own case: a module cannot load
// another module out of a data: address, and an app that ships one would
// fail silently halfway through instead of at the start.
test("an app whose entry page needs modules is refused", async ({ shell }) => {
  await shell.evaluate(() => { window.__ngcTest.module = true; });
  await openTerminal(shell);
  await expect(shell.getByRole("alert")).toHaveText("Diese App braucht klassische Skripte.");
  await expect(shell.locator(".appframe")).toHaveCount(0);
});

// The app talks to its own services and to nothing else, and it talks through
// ngc.js. This is the round trip: the app asks, the shell answers, the app
// puts the answer on screen.
// THE SECOND WAY, and it was open for the length of one task. The user
// interfaces of the apps NGC ships are files of this repository, and the first
// draft of task 25 put them under public/ - so
// /assets/apps/terminal/index.html was a page of NGC's OWN origin: reachable by
// its address, with the shell's storage, cookies and session in reach, and
// without one of the walls this file builds. csp.spec.js found it by walking
// the web root; this case asks the SERVER, because a file that is not in the
// tree can still be handed out by a route somebody wrote.
test("an app's own files are no document of the shell's origin", async ({ shell }) => {
  const asked = {};
  for (const path of [
    "/assets/apps/terminal/index.html",
    "/assets/apps/calculator/index.html",
    "/assets/apps/terminal/app.js",
    "/assets/apps/calculator/app.css",
  ]) {
    asked[path] = await shell.evaluate(async (p) => {
      const r = await fetch(p);
      return r.status;
    }, path);
  }
  expect(Object.values(asked), JSON.stringify(asked)).toEqual([404, 404, 404, 404]);

  // The door the test harness hands them over through shares this origin, so
  // it would be the same hole one step further: it answers text/plain and says
  // nosniff, and that is what makes it a file instead of a page.
  const door = await shell.evaluate(async () => {
    const r = await fetch("/mock/ui/terminal/index.html");
    return { status: r.status, type: r.headers.get("content-type") ?? "", sniff: r.headers.get("x-content-type-options") ?? "" };
  });
  expect(door.status).toBe(200);
  expect(door.type).toContain("text/plain");
  expect(door.sniff).toBe("nosniff");
});

test("an app reaches its own service through ngc.js", async ({ shell }) => {
  await openTerminal(shell);
  const frame = shell.frameLocator(".appframe");
  const input = frame.getByRole("textbox", { name: "Eingabe" });
  await expect(input).toBeEnabled();
  await input.fill("cp -r");
  await input.press("Enter");
  // The whole way there and back: the app wrote to its channel, the shell
  // carried it to the service, and what the service answered is on the app's
  // own page. Since task 25 that channel is one that STAYS open, which is
  // what a terminal is; before it, the fixture app asked one question.
  await expect(frame.locator(".out")).toContainText("cp: missing destination");
});
