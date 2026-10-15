import { test, expect, shellGap, loginGap, signIn } from "../fixtures/page.js";

// What the boot of the shell has to end in, on every engine there is.
//
// It lives in its own file because desktop.spec.js and calc.spec.js skip
// themselves where the engine has no session keys, and a beforeEach skips the
// whole file with them. This case is the one that must run exactly there: it
// is what keeps that skip from being a hole. A blank page is no ending, and
// this case runs on every engine and skips nowhere - it is the only case in
// the tree that does.
//
// THREE ENDINGS, because the engine answers in three ways and not two, and
// that is measured (fixtures/page.js sessionKeyGaps):
//
//   shellGap non-empty  the engine fails shell/main.js browserFits(), the
//                       sentence comes up and no desktop does. The WebKit that
//                       Playwright brings is here: NotSupportedError at
//                       generateKey.
//   shellGap empty and
//   loginGap empty      the mask comes up and a password login goes through to
//                       the desktop. chromium is here.
//   shellGap empty and
//   loginGap non-empty  the mask comes up and a password login CANNOT go
//                       through. The Firefox that Playwright brings (build
//                       firefox-1490) is here: it answers OperationError to
//                       importKey("pkcs8") + exportKey("jwk"), the step
//                       contracts/proof.js publicFromSeed stands on.
//
// THE THIRD ENDING IS A PRODUCTION GAP AND NOT A TEST GAP, and this case must
// not be read as saying otherwise. browserFits
// (public/assets/js/shell/main.js:20-32) asks for two generateKey calls and
// nothing more, so it lets that Firefox past the gate and the shell then names
// "Firefox 130 oder neuer" (main.js:55) as supported while a password login
// dies in it. What this case can hold without touching the shell is that the
// page is not blank and the mask is really there - the ending it was written
// to defend. That the person is then let type a password that cannot work is
// reported, not asserted away here: the repair belongs in browserFits, which
// this task was not to change.
test("the shell either brings the desktop up or says why it cannot", async ({ shell }) => {
  const cannotBoot = await shellGap(shell);
  if (cannotBoot !== "") {
    await expect(shell.locator(".msg")).toContainText("Dieser Browser ist zu alt für NGC.");
    await expect(shell.locator("#taskbar")).toHaveCount(0);
    return;
  }
  if ((await loginGap(shell)) === "") {
    await signIn(shell);
    await expect(shell.locator("#taskbar")).toBeVisible();
    await expect(shell.getByRole("button", { name: "Start" })).toBeVisible();
    return;
  }
  // The shell came up on an engine that cannot finish a password login. The
  // mask has to be standing - a blank page here is the hole this file guards.
  await expect(shell.getByLabel("Benutzername")).toBeVisible();
  await expect(shell.getByLabel("Passwort")).toBeVisible();
  await expect(shell.locator("#taskbar")).toHaveCount(0);
});
