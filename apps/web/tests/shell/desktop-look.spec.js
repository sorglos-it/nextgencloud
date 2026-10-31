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

// A colour the user picks has to reach the app that is ALREADY open, not only
// the next one. It is here and not in calc.spec.js because the colour is what
// it is about; the calculator is only the app that happens to be at hand.
//
// MEASURED, and it is why the line in applyAccent exists: before that line the
// shell went to #e14f8a and this frame stayed on #2f7de1. The app's frame has
// an origin of its own and cannot read the shell's stylesheet, so the only way
// in is the message appframe.js sends on a theme change - and that message
// carries the accent too. An accent that calls no listener never leaves.
test("a colour picked while an app is open reaches that app's frame", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  const frame = shell.frameLocator(".appframe");
  await expect(frame.locator("body")).toBeVisible();
  const inFrame = () => frame.locator("body").evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue("--accent").trim());
  expect(await inFrame()).toBe("#2f7de1");

  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("button", { name: "Farbe Pink", exact: true }).click();
  expect(await accent(shell)).toBe("#e14f8a");
  await expect.poll(inFrame).toBe("#e14f8a");
});

// The six swatches carry the only thing a user can aim at - there is no colour
// picker - so their names are measured in ALL FIVE languages and not only in
// the one the suite otherwise runs in. A key that stands in four files and is
// missing from the fifth does not show up in a German run at all.
//
// Türkis is the swatch under test because it is the one word that differs in
// every language; "Orange" and "Pink" would pass with a file that was never
// translated.
for (const [locale, want] of [
  ["de-DE", "Farbe Türkis"],
  ["en-US", "Color Turquoise"],
  ["es-ES", "Color Turquesa"],
  ["fr-FR", "Couleur Turquoise"],
  ["it-IT", "Colore Turchese"],
]) {
  test.describe(`in ${locale}`, () => {
    test.use({ locale });
    test("the second swatch is named in this language", async ({ shell }) => {
      await shell.getByRole("button", { name: "Start" }).click();
      const second = shell.locator("#smlook .swatches button").nth(1);
      // The name as the BROWSER hands it out, not the string the source holds.
      expect(await second.evaluate((e) => e.getAttribute("aria-label"))).toBe(want);
    });
  });
}
