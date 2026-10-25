import { test, expect, signIn, skipWithoutLogin } from "../fixtures/page.js";

// The desktop: windows, taskbar, start menu, light and dark.
//
// SINCE TASK 18 the beforeEach really does log anna in: the mask, the client
// certificate, the proof by her password key, the cookie of the mock API and
// the Noise session. That is the "four lines of the brief" this file was
// waiting for, and nothing else here changed - the apps still come from the
// stand-in in shell/fake-link.js, because the desktop's own data path is this
// task's and the next ones', not the login's.
test.beforeEach(async ({ shell }) => {
  // shell/main.js needs Ed25519 and X25519 before it builds anything, and the
  // WebKit Playwright brings has neither. There is no desktop to test there,
  // and faking the two keys would test a shell nobody runs. What WebKit does
  // instead is asserted by shell/boot.spec.js, which runs everywhere.
  await skipWithoutLogin(shell);
  await signIn(shell);
  await expect(shell.locator("#taskbar")).toBeVisible();
});

test("the start menu lists only the apps this user may use", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await expect(shell.getByRole("menuitem", { name: "App-Store" })).toBeVisible();
  await expect(shell.getByRole("menuitem", { name: "Terminal" })).toBeVisible();
  // bernd has no right on the terminal; the mock API leaves it out for him.
  await expect(shell.getByRole("menuitem", { name: "Geheim" })).toHaveCount(0);
});

test("a window opens, moves, maximizes and closes", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner" }).click();
  const win = shell.locator(".win", { hasText: "Rechner" });
  await expect(win).toBeVisible();
  const before = await win.boundingBox();
  // The brief drags to (before.x + 120, before.y + 80), which is an absolute
  // point on the page: with a window 300 wide the pointer grabs the title bar
  // at its middle, 150 to the right of before.x, so that point lies 30 to the
  // LEFT of where the drag began and the window would move the wrong way. The
  // 120, the 80 and the 80 of the assertion are the brief's; what changed is
  // that they now count from the grip, which is what a drag of "120 to the
  // right" means.
  const bar = shell.locator(".win .titlebar", { hasText: "Rechner" });
  const grip = await bar.boundingBox();
  await bar.hover();
  await shell.mouse.down();
  await shell.mouse.move(grip.x + grip.width / 2 + 120, grip.y + grip.height / 2 + 80);
  await shell.mouse.up();
  const after = await win.boundingBox();
  expect(Math.round(after.x - before.x)).toBeGreaterThan(80);
  await win.getByRole("button", { name: "Maximieren" }).click();
  await expect(win).toHaveClass(/max/);
  await win.getByRole("button", { name: "Schließen" }).click();
  await expect(win).toHaveCount(0);
});

// docs/gestaltung.md point 7: the machine's setting is the DEFAULT, and the
// person may overrule it. Three cases, and the first of them is the one that
// used to be missing - until this the shell came up dark on a light machine
// and stayed dark for good.
//
// The fixture's machine is set to light (playwright.config.js). "Follows the
// machine" is read as the ABSENCE of data-theme: that is the one state in
// which the stylesheet's prefers-color-scheme rule bites, so the absence is
// the mechanism and not a detail of it.
test("nothing chosen means the machine decides", async ({ shell }) => {
  await expect(shell.locator("html")).not.toHaveAttribute("data-theme", /.*/);
  // And it is really light that is drawn, not just an attribute that is gone:
  // --page-bg of the light block is #e7ecf4.
  expect(await shell.evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue("--page-bg").trim()))
    .toBe("#e7ecf4");
  // The start menu marks that choice, and only that one.
  await shell.getByRole("button", { name: "Start" }).click();
  await expect(shell.locator("#smlook .row button.on")).toHaveCount(1);
  await expect(shell.locator('#smlook .row button[data-theme="system"]')).toHaveClass(/on/);
});

test("a chosen design overrules the machine and is kept", async ({ shell }) => {
  await shell.getByRole("button", { name: "Design wechseln" }).click();
  await expect(shell.locator("html")).toHaveAttribute("data-theme", "dark");
  await shell.reload();
  await expect(shell.locator("html")).toHaveAttribute("data-theme", "dark");
  // On a light machine, and it stays dark: that is what "overrules" means.
  expect(await shell.evaluate(() => matchMedia("(prefers-color-scheme: light)").matches)).toBe(true);
  await expect(shell.locator("html")).toHaveAttribute("data-theme", "dark");
});

test("the choice can be handed back to the machine", async ({ shell }) => {
  await shell.getByRole("button", { name: "Design wechseln" }).click();
  await expect(shell.locator("html")).toHaveAttribute("data-theme", "dark");
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("button", { name: "Wie das System", exact: true }).click();
  await expect(shell.locator("html")).not.toHaveAttribute("data-theme", /.*/);
  // Kept as a choice of its own: a reload comes back following the machine
  // and not back on the dark that was stored before it.
  await shell.reload();
  await expect(shell.locator("html")).not.toHaveAttribute("data-theme", /.*/);
});

test("the shell follows the machine changing its mind, without being reloaded", async ({ shell }) => {
  // The decision book forbids "please reload". Nothing is pressed here and
  // nothing is reloaded: the machine is turned to dark and the page has to be
  // dark afterwards. --page-bg of the dark block is #0d1017.
  await expect(shell.locator("html")).not.toHaveAttribute("data-theme", /.*/);
  await shell.emulateMedia({ colorScheme: "dark" });
  expect(await shell.evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue("--page-bg").trim()))
    .toBe("#0d1017");
  await shell.emulateMedia({ colorScheme: "light" });
  expect(await shell.evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue("--page-bg").trim()))
    .toBe("#e7ecf4");
});

test("a name from outside is never HTML", async ({ shell }) => {
  // The mock API names one app "<img src=x onerror=alert(1)>Böse".
  await shell.getByRole("button", { name: "Start" }).click();
  const item = shell.getByRole("menuitem", { name: /Böse/ });
  await expect(item).toBeVisible();
  expect(await item.evaluate((e) => e.querySelectorAll("img").length)).toBe(0);
});

test("a notice from the API is shown as text", async ({ shell }) => {
  await shell.evaluate(() => window.__ngcSendNotice("warn", "Update für ngc-web bereit."));
  await expect(shell.getByRole("status")).toHaveText("Update für ngc-web bereit.");
});

test("an app the API calls unreachable opens no window", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Fotos" }).click();
  await expect(shell.getByRole("status")).toHaveText("Fotos ist nicht erreichbar.");
  await expect(shell.locator(".win")).toHaveCount(0);
});

test("the taskbar brings a minimized window back", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner" }).click();
  const win = shell.locator(".win", { hasText: "Rechner" });
  await expect(win).toBeVisible();
  await win.getByRole("button", { name: "Minimieren" }).click();
  await expect(win).toBeHidden();
  await shell.getByRole("button", { name: "Fenster Rechner" }).click();
  await expect(win).toBeVisible();
});
