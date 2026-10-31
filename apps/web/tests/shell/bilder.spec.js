import { mkdirSync } from "node:fs";
import { test, expect, signIn, skipWithoutLogin } from "../fixtures/page.js";
import { serveApps } from "../fixtures/apps.js";

// A gallery of the screens docs/gestaltung.md decides about, so the decisions
// can be looked at instead of read about.
//
// IT IS NOT PART OF THE SUITE. It asserts almost nothing - it takes pictures -
// and a run that only takes pictures and passes says nothing about the shell.
// So it stands still unless NGC_BILDER names a folder, and the same file run
// on another checkout of the same tests gives the pictures to compare with.
//
//   NGC_BILDER=<folder> NGC_WEB_TEST_PORT=<port> npx playwright test \
//     shell/bilder.spec.js --project=chromium
//
// The folder is made here rather than by the caller, because a screenshot into
// a folder that is not there fails the case and looks like a finding.
const into = process.env.NGC_BILDER || "";

test.skip(into === "", "NGC_BILDER names no folder: nothing to take pictures into");

// One name per picture, so the two runs produce files that line up by name.
async function shot(page, name) {
  mkdirSync(into, { recursive: true });
  await page.screenshot({ path: `${into}/${name}.png` });
}

// The two shapes NGC has to work in, and the second one is the decision of
// point 1: a phone held upright, the narrowest thing this has to survive.
const WIDE = { width: 1280, height: 800 };
const PHONE = { width: 375, height: 700 };

test("the login screen", async ({ page }) => {
  await page.setViewportSize(WIDE);
  await page.goto("/");
  await skipWithoutLogin(page);
  await expect(page.locator(".loginbox")).toBeVisible();
  // Point 6: what the installation runs on, small and under the box.
  await shot(page, "01-anmeldung");
  await page.setViewportSize(PHONE);
  await shot(page, "02-anmeldung-handy");
});

test("the desktop, the start menu and the three densities", async ({ page }) => {
  await page.setViewportSize(WIDE);
  await page.goto("/");
  await skipWithoutLogin(page);
  await signIn(page);
  await expect(page.locator("#taskbar")).toBeVisible();
  // Point 7: nothing chosen, so the machine decides - and the fixture's
  // machine is set to light (playwright.config.js). Before this the desktop
  // came up dark here whatever the machine said.
  await shot(page, "03-schreibtisch");

  await page.getByRole("button", { name: "Start" }).click();
  await expect(page.locator("#startmenu")).toHaveClass(/open/);
  // Points 5 and 7 together: three designs and three densities, under
  // "Aussehen", where the colour and the wallpaper already stood.
  await shot(page, "04-startmenue");

  for (const [step, name] of [["Eng", "05-dichte-eng"], ["Luftig", "06-dichte-luftig"]]) {
    await page.getByRole("button", { name: step, exact: true }).click();
    await shot(page, name);
  }
  await page.getByRole("button", { name: "Wie heute", exact: true }).click();
  await page.keyboard.press("Escape");

  // Point 7, the other two choices.
  await page.getByRole("button", { name: "Design wechseln" }).click();
  await shot(page, "07-schreibtisch-dunkel");
});

test("the desktop on a phone", async ({ page }) => {
  await page.setViewportSize(PHONE);
  await page.goto("/");
  await skipWithoutLogin(page);
  await signIn(page);
  await expect(page.locator("#taskbar")).toBeVisible();
  await shot(page, "08-handy-schreibtisch");
  // Point 1: the way to the apps that survives under 640 px.
  await page.getByRole("button", { name: "Start" }).click();
  await expect(page.locator("#startmenu")).toHaveClass(/open/);
  await shot(page, "09-handy-startmenue");
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Start" }).click();
  await page.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  await expect(page.locator(".win")).toBeVisible();
  await shot(page, "10-handy-fenster");
});

test("the desktop with no apps on it", async ({ page }) => {
  await page.setViewportSize(WIDE);
  // Point 3: the exceptional case. One sentence and one button, and it must
  // not read as a fault.
  await serveApps(page, []);
  await page.goto("/");
  await skipWithoutLogin(page);
  await signIn(page);
  await expect(page.locator("#taskbar")).toBeVisible();
  await shot(page, "11-leerer-schreibtisch");
  await page.getByRole("button", { name: "Start" }).click();
  await shot(page, "12-leeres-startmenue");
  await page.keyboard.press("Escape");
  await page.setViewportSize(PHONE);
  await shot(page, "13-leerer-schreibtisch-handy");
});

test("a message over the window it belongs to", async ({ page }) => {
  await page.setViewportSize(WIDE);
  // The default list of the stand-in, not a list of this test's own: the
  // picture needs the Rechner AND the App-Store side by side.
  await page.goto("/");
  await skipWithoutLogin(page);
  await signIn(page);
  await expect(page.locator("#taskbar")).toBeVisible();
  // Two windows, so the picture shows the thing the decision is about: the
  // message covers ONE of them and the other goes on being usable.
  for (const name of ["Rechner", "App-Store"]) {
    await page.getByRole("button", { name: "Start" }).click();
    await page.getByRole("menuitem", { name, exact: true }).click();
  }
  await page.getByRole("button", { name: "Start" }).click();
  await page.getByRole("button", { name: "Fenster anordnen" }).click();
  await page.waitForTimeout(350);
  // Point 4. Sent the way an app's own message arrives, through the notify
  // body of the control channel, with the app it belongs to named in it.
  await page.evaluate(() => window.__ngcSendNotice(
    "error", "Das hat der Rechner nicht angenommen. Bitte es noch einmal versuchen.",
    "org.sorglos.calculator"));
  await page.waitForTimeout(150);
  await shot(page, "14-meldung-im-fenster");
});

test("a window pushed at the edge", async ({ page }) => {
  await page.setViewportSize(PHONE);
  await page.goto("/");
  await skipWithoutLogin(page);
  await signIn(page);
  await expect(page.locator("#taskbar")).toBeVisible();
  await page.getByRole("button", { name: "Start" }).click();
  await page.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  const win = page.locator(".win");
  await expect(win).toBeVisible();
  // Point 2, and the picture is of the price: pushed hard past the bottom
  // right, the window is still whole and still has its three buttons.
  const g = await win.locator(".titlebar").boundingBox();
  await page.mouse.move(g.x + 40, g.y + g.height / 2);
  await page.mouse.down();
  await page.mouse.move(900, 1400, { steps: 12 });
  await shot(page, "15-fenster-am-rand");
  await page.mouse.up();
});
