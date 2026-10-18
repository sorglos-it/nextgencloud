import { test, expect, shellGap, loginGap, hasSessionKeys, signIn } from "../fixtures/page.js";

// What the boot of the shell has to end in, on every engine there is.
//
// It lives in its own file because desktop.spec.js and calc.spec.js skip
// themselves where the engine has no session keys, and a beforeEach skips the
// whole file with them. This case is the one that must run exactly there: it
// is what keeps that skip from being a hole. A blank page is never a legal
// ending, and neither is a mask that takes a password it cannot use.
//
// THERE ARE THREE LEGAL ENDINGS, and until 17.10.2026 this case knew two of
// them, which is why it was red on Firefox while the shell was doing nothing
// worse than coming up:
//
//   1. the desktop - the engine can finish a password login;
//   2. "Dieser Browser ist zu alt für NGC." - not even the two curves
//      (Playwright's WebKit, measured: NotSupportedError at generateKey);
//   3. the mask WITHOUT the password route, and a sentence that says why -
//      the curves are there and a passkey would get in, but the engine hands
//      out no public half of a password seed (Firefox 141, measured:
//      OperationError at exportKey("jwk"); see shell/main.js
//      passwordLoginFits).
//
// The third was a white lie of a screen before this round: Firefox drew the
// mask, took the password, ran Argon2id over it and said "Anmeldung
// fehlgeschlagen." - the sentence for a wrong password. Which ending applies
// is decided by the MEASURED gaps of this engine, never by its name.
test("the shell either brings the desktop up or says why it cannot", async ({ shell }) => {
  if (await hasSessionKeys(shell)) {
    await signIn(shell);
    await expect(shell.locator("#taskbar")).toBeVisible();
    await expect(shell.getByRole("button", { name: "Start" })).toBeVisible();
    return;
  }
  if ((await shellGap(shell)) !== "") {
    await expect(shell.locator(".msg")).toContainText("Dieser Browser ist zu alt für NGC.");
    await expect(shell.locator("#taskbar")).toHaveCount(0);
    return;
  }
  // Ending 3. The mask is there, it names the reason, and the one route that
  // cannot work is shut - not merely unlabelled.
  await expect(shell.getByRole("heading", { name: "Bei NGC anmelden" })).toBeVisible();
  await expect(shell.locator(".note.warn"))
    .toContainText("Dieser Browser kann die Passwort-Anmeldung nicht ausführen.");
  await expect(shell.getByRole("button", { name: "Anmelden", exact: true })).toBeDisabled();
  await expect(shell.getByLabel("Passwort")).toBeDisabled();
  // And the way that DOES work is offered. tests/web.json carries a relying
  // party, so the passkey button belongs on this mask; without one shell/main.js
  // refuses the boot outright and this branch is never reached.
  await expect(shell.getByRole("button", { name: "Mit Passkey anmelden" })).toBeEnabled();
  await expect(shell.locator("#taskbar")).toHaveCount(0);
});

// The gap this case stands on, said out loud and in ONE place, so the branch
// above is never taken for a reason nobody measured. It is also the case that
// nails shell/main.js passwordLoginFits: the boot's third ending exists if and
// only if the engine's seed step is the thing that is missing.
test("where the boot shuts the password route, the seed step is what is missing", async ({ shell }) => {
  test.skip(await hasSessionKeys(shell), "this engine can finish a password login");
  test.skip((await shellGap(shell)) !== "", "this engine cannot run the shell at all");
  // The STEP is pinned, not the exception's name: an engine that refuses the
  // same step with another error is the same gap and the same third ending.
  expect(await loginGap(shell)).toContain("public half out of a PKCS#8 seed");
});
