import { test, expect, hasSessionKeys } from "../fixtures/page.js";

// The calculator, the first app of the desktop with a face of its own.
//
// PULLED FORWARD, HALF OF TASK 25. That task builds the calculator AND the
// terminal, and the terminal needs a service of the controller that does not
// exist yet, so only the calculator is here. Its brief runs the keys inside
// shell.frameLocator(".appframe"), the sandboxed frame every app gets in Task
// 22; until that frame exists the calculator sits straight in the window, so
// the keys are looked up there. The keys, the presses and the 15 are the
// brief's.
test.beforeEach(async ({ shell }) => {
  // Same gap as in desktop.spec.js: no session keys, no desktop, no window to
  // put a calculator in.
  test.skip(!(await hasSessionKeys(shell)),
    "no Ed25519 and no X25519 in this engine's Web Crypto: the shell stops before the desktop");
  await expect(shell.locator("#taskbar")).toBeVisible();
});

test("the calculator computes without eval", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner" }).click();
  const win = shell.locator(".win", { hasText: "Rechner" });
  for (const key of ["1", "2", "+", "3", "="]) await win.getByRole("button", { name: key }).click();
  await expect(win.locator(".v")).toHaveText("15");
});

test("point before line, and a division by zero is an error, not Infinity", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner" }).click();
  const win = shell.locator(".win", { hasText: "Rechner" });
  // exact: true, because a name is matched as a substring by default and "C"
  // is a letter of "Schließen", "Vorzeichen" and "Zurück" as well.
  const key = (k) => win.getByRole("button", { name: k, exact: true });
  for (const k of ["2", "+", "3", "×", "4", "="]) await key(k).click();
  await expect(win.locator(".v")).toHaveText("14");
  for (const k of ["C", "1", "÷", "0", "="]) await key(k).click();
  await expect(win.locator(".v")).toHaveText("Fehler");
});
