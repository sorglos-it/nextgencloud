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

// The dock. Measured rather than described, because every number in it was
// either computed (40, section 8 of the sheet) or read off LordOS (64).
test("the dock holds the apps, magnifies one, and marks the open one", async ({ shell }) => {
  const dock = shell.locator("#dock");
  await expect(dock).toBeVisible();
  const first = dock.locator(".dapp").first();
  const size = async () => {
    const r = await first.boundingBox();
    return [Math.round(r.width), Math.round(r.height)];
  };
  expect(await size()).toEqual([40, 40]);
  await first.hover();
  // The magnifier is a transition, so it is waited for and not slept through.
  await expect.poll(size).toEqual([64, 64]);

  // The dot is read off the register of open windows, so it has to appear and
  // go away again with the window and not only once.
  expect(await dock.locator(".dapp.on").count()).toBe(0);
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  await expect(dock.locator(".dapp.on")).toHaveCount(1);
  expect(await dock.locator(".dapp.on").getAttribute("aria-label")).toBe("Rechner");
  await shell.locator(".wbtn.close").first().click();
  await expect(dock.locator(".dapp.on")).toHaveCount(0);
});

// A dock carries icons and no names, so an app's name reaches it only as a
// LABEL. The mock's list holds an app called "<img src=x onerror=alert(1)>Böse"
// on purpose; this measures that the name arrives as text in an attribute and
// never as markup - there must be no img in the dock at all.
test("a hostile app name reaches the dock as text and not as markup", async ({ shell }) => {
  const dock = shell.locator("#dock");
  await expect(dock).toBeVisible();
  const evil = dock.locator('.dapp[aria-label^="<img"]');
  await expect(evil).toHaveCount(1);
  expect(await evil.getAttribute("aria-label")).toBe("<img src=x onerror=alert(1)>Böse");
  expect(await dock.locator("img").count()).toBe(0);
});

// The name of the app in front, in the band, and its menu.
test("the band names the app in front, and forgets it again", async ({ shell }) => {
  const btn = shell.locator("#appbtn");
  // Nothing in front, nothing named: NGC has no app that is always running.
  await expect(btn).toBeHidden();

  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  await expect(btn).toBeVisible();
  await expect(btn).toHaveText("Rechner");

  // A minimised window is not in front, so there is nothing to name.
  await shell.locator(".wbtn.mini").first().click();
  await expect(btn).toBeHidden();
  await shell.locator(".task").first().click();
  await expect(btn).toHaveText("Rechner");

  // MEASURED, and it cost a wrong order in Win.close: the band went on naming
  // a window that was gone, because markBar ran while desktop.focused still
  // pointed at it. It runs after focusTopmost now.
  await shell.locator(".wbtn.close").first().click();
  await expect(btn).toBeHidden();
});

test("the app menu says what can be done with the window as it stands", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  const drop = shell.locator("#appdrop");
  await shell.locator("#appbtn").click();
  await expect(drop).toBeVisible();
  // Arranging is about ALL the windows, so with one window it is off.
  await expect(drop.locator("button")).toHaveText(
    ["Minimieren", "Maximieren", "Fenster anordnen", "Schließen"]);
  await expect(drop.locator("button", { hasText: "Fenster anordnen" })).toBeDisabled();

  // Escape closes it and gives the keyboard back to the button that opened it.
  await shell.keyboard.press("Escape");
  await expect(drop).toBeHidden();
  expect(await shell.evaluate(() => document.activeElement.id)).toBe("appbtn");

  // A maximised window offers to come back instead of to grow.
  await shell.locator(".wbtn.maxi").first().click();
  await shell.locator("#appbtn").click();
  await expect(drop.locator("button").nth(1)).toHaveText("Wiederherstellen");
  await drop.locator("button", { hasText: "Schließen" }).click();
  await expect(drop).toBeHidden();
  await expect(shell.locator(".win")).toHaveCount(0);
});

// The mark alone has no name of its own, and 89 places in this suite aim at
// this button by the name "Start". It is written out as a label since the word
// came out of the band - MEASURED: without the label it was null, and 62 cases
// went red in one run.
test("the start button keeps its name without carrying the word", async ({ shell }) => {
  const b = shell.locator("#startbtn");
  expect(await b.getAttribute("aria-label")).toBe("Start");
  expect((await b.textContent()).trim()).toBe("⊞");
  await shell.getByRole("button", { name: "Start" }).click();
  await expect(shell.locator("#startmenu")).toHaveClass(/open/);
});
