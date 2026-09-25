import { test, expect, hasSessionKeys } from "../fixtures/page.js";

// The desktop: windows, taskbar, start menu, light and dark.
//
// PULLED FORWARD. The brief for this task opens every case by logging anna in
// (Tasks 19 and 20), and the shell has no login yet - nor a session, nor a
// channel to the API. Building one to make a beforeEach pass would be a login
// that looks real and proves nothing, so there is none: the desktop starts as
// soon as the page has its configuration, and the apps come from the stand-in
// in shell/fake-link.js. When the login lands, this beforeEach grows the four
// lines of the brief again and nothing else here changes.
test.beforeEach(async ({ shell }) => {
  // shell/main.js needs Ed25519 and X25519 before it builds anything, and the
  // WebKit Playwright brings has neither. There is no desktop to test there,
  // and faking the two keys would test a shell nobody runs. What WebKit does
  // instead is asserted by shell/boot.spec.js, which runs everywhere.
  test.skip(!(await hasSessionKeys(shell)),
    "no Ed25519 and no X25519 in this engine's Web Crypto: the shell stops before the desktop");
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

test("light and dark are kept", async ({ shell }) => {
  await shell.getByRole("button", { name: "Design wechseln" }).click();
  await expect(shell.locator("html")).toHaveAttribute("data-theme", "light");
  await shell.reload();
  await expect(shell.locator("html")).toHaveAttribute("data-theme", "light");
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
