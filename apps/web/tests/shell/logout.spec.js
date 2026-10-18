import { test, expect, signIn, skipWithoutLogin } from "../fixtures/page.js";

// Signing out. Until this task there was no way: the start menu offered
// "Fenster anordnen" and "Alle schließen" and nothing that ended the session,
// and net/http.js logout() had exactly ONE caller - the trust alarm in
// shell/trustlist.js - so a person on a shared machine stayed signed in until
// the cookie expired.
//
// THE SKIPS ASK FOR THE CAPABILITY AND NEVER FOR AN ENGINE'S NAME.
// skipWithoutLogin is "this engine finishes a password login", which is what
// standing on the desktop needs; chromium does it here, firefox 141 and
// webkit 26 do not (fixtures/page.js, loginGap). Nothing below names a
// browser.
test.beforeEach(async ({ shell }) => {
  await skipWithoutLogin(shell);
  await signIn(shell);
  await expect(shell.locator("#taskbar")).toBeVisible();
});

// The assertion itself: there IS a way out, in the place a person looks for
// it, and it is in the language of this browser (de-DE in this suite, out of
// public/assets/lang/de.json - no sentence of it stands in the source).
test("the start menu offers a way out, named in the language of the browser", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  const out = shell.getByRole("button", { name: "Abmelden" });
  await expect(out).toBeVisible();
  // Reachable with the keyboard, like the two buttons beside it: the start
  // menu's own trapTab() walks every button in it, so this must be one.
  const stops = await shell.evaluate(() => {
    const menu = document.getElementById("startmenu");
    return [...menu.querySelectorAll('input, button, [role="menuitem"][tabindex="0"]')]
      .map((e) => e.textContent);
  });
  expect(stops.some((s) => s === "Abmelden"), "the button is a tab stop of the menu").toBe(true);
});

// What the button DOES, measured on three things at once: the API was asked,
// the desktop is gone, and the login mask is back. The mask is the proof that
// nothing signed-in stayed standing - taskbar, windows and tiles all live in
// #root, which the login replaces.
test("signing out asks the API and lands back at the login mask", async ({ shell }) => {
  let asked = 0;
  await shell.route("**/api/v1/logout", async (r) => { asked++; await r.fallback(); });
  // A window stands open, so the teardown has something to take with it.
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner" }).click();
  await expect(shell.locator(".win")).toHaveCount(1);

  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("button", { name: "Abmelden" }).click();

  await expect(shell.getByRole("heading", { name: "Bei NGC anmelden" })).toBeVisible();
  expect(asked, "the API was told").toBe(1);
  await expect(shell.locator("#taskbar"), "no taskbar").toHaveCount(0);
  await expect(shell.locator(".win"), "no window").toHaveCount(0);
  await expect(shell.locator(".smitem"), "no tile").toHaveCount(0);
});

// POINT 50, and it is the whole reason this case exists: a screen that says
// "you are signed out" to somebody who is not is worse than a screen that
// says the attempt failed. The mock's fault door makes the next request
// answer E_BUSY, which is what a database under load answers.
test("a logout the server refuses leaves the desktop standing and says so", async ({ shell }) => {
  await shell.request.post("/api/v1/mock?fault=busy");
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("button", { name: "Abmelden" }).click();

  // Still signed in, and the screen shows it: the desktop is where it was.
  await expect(shell.locator("#taskbar")).toBeVisible();
  await expect(shell.getByRole("heading", { name: "Bei NGC anmelden" })).toHaveCount(0);
  // And it SAYS so, in the notice bar, as an error. What is pinned is the
  // load-bearing half of the sentence - "you are still signed in" - and not
  // its wording: a text that said only "that did not work" would leave the
  // person guessing which side of point 50 they are on.
  const status = shell.getByRole("status");
  await expect(status).toHaveText(/noch angemeldet/);
  // "error", so it waits for the person: an eight-second strip about a session
  // that is still open is a warning somebody can miss by looking away.
  await expect(status).toHaveClass(/error/);
  // A second press after the fault is cleared gets through: the refusal left
  // no latch behind.
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("button", { name: "Abmelden" }).click();
  await expect(shell.getByRole("heading", { name: "Bei NGC anmelden" })).toBeVisible();
});
