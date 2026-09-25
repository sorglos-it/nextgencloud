import { test, expect, hasSessionKeys } from "../fixtures/page.js";

// What the boot of the shell has to end in, on every engine there is.
//
// It lives in its own file because desktop.spec.js and calc.spec.js skip
// themselves where the engine has no session keys, and a beforeEach skips the
// whole file with them. This case is the one that must run exactly there: it
// is what keeps that skip from being a hole. The boot has two legal endings -
// the desktop, or a sentence that says what to do - and a blank page is
// neither.
test("the shell either brings the desktop up or says why it cannot", async ({ shell }) => {
  if (await hasSessionKeys(shell)) {
    await expect(shell.locator("#taskbar")).toBeVisible();
    await expect(shell.getByRole("button", { name: "Start" })).toBeVisible();
    return;
  }
  await expect(shell.locator(".msg")).toContainText("Dieser Browser ist zu alt für NGC.");
  await expect(shell.locator("#taskbar")).toHaveCount(0);
});
