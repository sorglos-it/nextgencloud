import { test, expect, signIn, skipWithoutLogin } from "../fixtures/page.js";

// The look in the start menu - accent colour and background - and the search
// above it. Task 21 left all three measured by hand only.
test.beforeEach(async ({ shell }) => {
  await skipWithoutLogin(shell);
  await signIn(shell);
  await expect(shell.locator("#taskbar")).toBeVisible();
});

const accent = (shell) => shell.evaluate(() =>
  getComputedStyle(document.documentElement).getPropertyValue("--accent").trim());
const wallpaper = (shell) => shell.locator("#wallpaper").evaluate((e) => e.style.background);
const names = (shell) => shell.locator("#smlist .smitem .n").allTextContents();

test("one of the six accent colours is taken and kept over a reload", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  expect(await accent(shell)).toBe("#2f7de1");
  // Six swatches, each with the name of its colour - a name is easier to hit
  // than a hex value, and there is no colour picker to get wrong.
  await expect(shell.locator("#smlook .swatches button")).toHaveCount(6);
  await shell.getByRole("button", { name: "Farbe Pink", exact: true }).click();
  expect(await accent(shell)).toBe("#e14f8a");
  // The swatch in force is the one that is marked, and only that one.
  await expect(shell.locator("#smlook .swatches button.on")).toHaveCount(1);
  await expect(shell.locator('#smlook .swatches button[data-accent="#e14f8a"]')).toHaveClass(/on/);
  await shell.reload();
  await signIn(shell);
  await expect(shell.locator("#taskbar")).toBeVisible();
  expect(await accent(shell)).toBe("#e14f8a");
  await shell.getByRole("button", { name: "Start" }).click();
  await expect(shell.locator('#smlook .swatches button[data-accent="#e14f8a"]')).toHaveClass(/on/);
});

test("the accent colour is the one the focus ring and the Start button use", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("button", { name: "Farbe Grün", exact: true }).click();
  await shell.keyboard.press("Escape");
  await shell.getByRole("button", { name: "Start" }).focus();
  const ring = await shell.evaluate(() => getComputedStyle(document.activeElement).outlineColor);
  // #22c55e
  expect(ring).toBe("rgb(34, 197, 94)");
});

test("one of the five backgrounds is taken and kept over a reload", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await expect(shell.locator("#smlook .wpgrid button")).toHaveCount(5);
  const before = await wallpaper(shell);
  await shell.getByRole("button", { name: "Hintergrund Abendrot", exact: true }).click();
  const after = await wallpaper(shell);
  expect(after).not.toBe(before);
  // Abendrot is the gradient of theme.js and nothing that was fetched: no
  // request leaves the page for a background.
  expect(after).toContain("255, 126, 95");
  await expect(shell.locator("#smlook .wpgrid button.on")).toHaveCount(1);
  await expect(shell.locator('#smlook .wpgrid button[data-wallpaper="sunset"]')).toHaveClass(/on/);
  await shell.reload();
  await signIn(shell);
  await expect(shell.locator("#taskbar")).toBeVisible();
  expect(await wallpaper(shell)).toBe(after);
  await shell.getByRole("button", { name: "Start" }).click();
  await expect(shell.locator('#smlook .wpgrid button[data-wallpaper="sunset"]')).toHaveClass(/on/);
});

test("the search finds an app by its name and by its id", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  const all = await names(shell);
  expect(all.length).toBeGreaterThan(3);
  await shell.locator("#smsearch").fill("rech");
  expect(await names(shell)).toEqual(["Benutzer und Rechte", "Rechner"]);
  // Upper and lower case are the same thing to somebody looking for an app.
  await shell.locator("#smsearch").fill("RECH");
  expect(await names(shell)).toEqual(["Benutzer und Rechte", "Rechner"]);
  // Spaces around the word are a slip, not a search.
  await shell.locator("#smsearch").fill("   rech  ");
  expect(await names(shell)).toEqual(["Benutzer und Rechte", "Rechner"]);
  // The id is searched as well, so an id out of a manual finds the app.
  await shell.locator("#smsearch").fill("org.sorglos.calculator");
  expect(await names(shell)).toEqual(["Rechner"]);
  // And an empty field is every app again.
  await shell.locator("#smsearch").fill("");
  expect(await names(shell)).toEqual(all);
});

test("the search says so when nothing matches", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.locator("#smsearch").fill("zzz");
  await expect(shell.locator("#smlist .smitem")).toHaveCount(0);
  await expect(shell.locator(".smempty")).toHaveText("Keine Treffer.");
  // Nothing found is not an error: typing on carries on searching.
  await shell.locator("#smsearch").fill("zzzrech");
  await expect(shell.locator(".smempty")).toBeVisible();
  await shell.locator("#smsearch").fill("rech");
  await expect(shell.locator(".smempty")).toHaveCount(0);
  await expect(shell.locator("#smlist .smitem")).toHaveCount(2);
});

test("Enter in the search opens the first hit", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.locator("#smsearch").fill("rechner");
  await shell.locator("#smsearch").press("Enter");
  await expect(shell.locator(".win", { hasText: "Rechner" })).toBeVisible();
  await expect(shell.locator("#startmenu")).not.toHaveClass(/open/);
});

test("Enter in the search opens nothing when nothing was found", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.locator("#smsearch").fill("zzz");
  await shell.locator("#smsearch").press("Enter");
  await expect(shell.locator(".win")).toHaveCount(0);
  // And the menu is still there to type on in.
  await expect(shell.locator("#startmenu")).toHaveClass(/open/);
});

test("Escape closes the menu, and the search starts empty the next time", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.locator("#smsearch").fill("rech");
  await shell.locator("#smsearch").press("Escape");
  await expect(shell.locator("#startmenu")).not.toHaveClass(/open/);
  await shell.getByRole("button", { name: "Start" }).click();
  await expect(shell.locator("#smsearch")).toHaveValue("");
  expect((await names(shell)).length).toBeGreaterThan(3);
  // The field has the focus, so the next keystroke is a search and not a
  // keystroke into whatever window was in front.
  expect(await shell.evaluate(() => document.activeElement.id)).toBe("smsearch");
});
