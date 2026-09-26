import { test, expect, hasSessionKeys } from "../fixtures/page.js";

// Getting through the desktop with the keyboard alone, and what a screen
// reader is told. The people this is built for include the ones who never
// touch a mouse.
//
// What is measured here is what holds today. What does not hold yet - the
// arrow keys a role="menu" promises, the focus that is not given back when
// the menu closes, a window that says what it is - is written down in
// docs/.../task-21-pruefung.md and is not asserted here, because a case that
// is red on purpose is a case everybody learns to ignore.
test.beforeEach(async ({ shell }) => {
  test.skip(!(await hasSessionKeys(shell)),
    "no Ed25519 and no X25519 in this engine's Web Crypto: the shell stops before the desktop");
  await expect(shell.locator("#taskbar")).toBeVisible();
});

const active = (shell) => shell.evaluate(() => {
  const a = document.activeElement;
  if (!a || a === document.body) return "(body)";
  return (a.getAttribute("aria-label") || a.textContent || "").trim();
});

test("the Start button is the first thing the keyboard reaches", async ({ shell }) => {
  await shell.evaluate(() => document.activeElement.blur());
  await shell.keyboard.press("Tab");
  await expect(shell.getByRole("button", { name: "Start" })).toBeFocused();
  await shell.keyboard.press("Tab");
  await expect(shell.getByRole("button", { name: "Design wechseln" })).toBeFocused();
});

test("the start menu opens with the keyboard and the search field has the focus", async ({ shell }) => {
  await shell.evaluate(() => document.activeElement.blur());
  await shell.keyboard.press("Tab");
  await shell.keyboard.press("Enter");
  await expect(shell.locator("#startmenu")).toHaveClass(/open/);
  expect(await shell.evaluate(() => document.activeElement.id)).toBe("smsearch");
  // Typing goes straight into the search, with nothing to click first.
  await shell.keyboard.type("rech");
  await expect(shell.locator("#smsearch")).toHaveValue("rech");
  await expect(shell.locator("#smlist .smitem")).toHaveCount(2);
});

test("an app opens from the menu with Enter and with the space bar", async ({ shell }) => {
  for (const [key, name] of [["Enter", "Rechner"], [" ", "App-Store"]]) {
    await shell.getByRole("button", { name: "Start" }).click();
    await shell.getByRole("menuitem", { name, exact: true }).focus();
    await shell.keyboard.press(key);
    await expect(shell.locator(".win", { hasText: name })).toBeVisible();
    await expect(shell.locator("#startmenu")).not.toHaveClass(/open/);
  }
  await expect(shell.locator(".win")).toHaveCount(2);
});

test("Escape closes the start menu from wherever the focus sits in it", async ({ shell }) => {
  for (const focus of ["#smsearch", "#smlist .smitem", "#smfoot button"]) {
    await shell.getByRole("button", { name: "Start" }).click();
    await expect(shell.locator("#startmenu")).toHaveClass(/open/);
    await shell.locator(focus).first().focus();
    await shell.keyboard.press("Escape");
    await expect(shell.locator("#startmenu")).not.toHaveClass(/open/);
  }
});

test("every tile of the start menu can be tabbed to", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  const count = await shell.locator("#smlist .smitem").count();
  expect(count).toBeGreaterThan(3);
  const seen = [];
  for (let i = 0; i < count; i++) {
    await shell.keyboard.press("Tab");
    seen.push(await active(shell));
  }
  const want = await shell.locator("#smlist .smitem").evaluateAll((is) => is.map((i) => i.textContent.trim()));
  expect(seen).toEqual(want);
});

test("the three buttons of a window and every key of the calculator are tab stops", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  const win = shell.locator(".win");
  for (const name of ["Minimieren", "Maximieren", "Schließen"]) {
    await win.getByRole("button", { name, exact: true }).focus();
    expect(await active(shell)).toBe(name);
  }
  await win.getByRole("button", { name: "Minimieren", exact: true }).focus();
  // Three title-bar buttons, then the twenty keys, one Tab at a time.
  const stops = [];
  for (let i = 0; i < 22; i++) {
    await shell.keyboard.press("Tab");
    stops.push(await active(shell));
  }
  expect(stops.slice(0, 2)).toEqual(["Maximieren", "Schließen"]);
  expect(stops.slice(2)).toEqual(["C", "Vorzeichen", "%", "÷", "7", "8", "9", "×",
    "4", "5", "6", "−", "1", "2", "3", "+", "0", "Komma", "Zurück", "="]);
});

test("everything the keyboard can reach shows a ring while it is there", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  await shell.evaluate(() => document.activeElement.blur());
  // Walk far enough to pass the taskbar, the window's buttons and some keys.
  const bad = [];
  for (let i = 0; i < 12; i++) {
    await shell.keyboard.press("Tab");
    const r = await shell.evaluate(() => {
      const a = document.activeElement;
      if (!a || a === document.body) return null;
      const s = getComputedStyle(a);
      return { what: (a.getAttribute("aria-label") || a.textContent || "").trim(), style: s.outlineStyle, width: parseFloat(s.outlineWidth) };
    });
    if (r && (r.style === "none" || !(r.width >= 2))) bad.push(r);
  }
  expect(bad).toEqual([]);
});

test("every control of the desktop has a name a screen reader can read", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  await shell.evaluate(() => window.__ngcSendNotice("warn", "Eine Meldung."));
  await shell.getByRole("button", { name: "Start" }).click();
  const nameless = await shell.evaluate(() => {
    const out = [];
    for (const c of document.querySelectorAll("button, input, [role=menuitem]")) {
      const name = (c.getAttribute("aria-label") || c.textContent || "").trim();
      if (!name) out.push(c.className || c.id || c.tagName);
    }
    return out;
  });
  expect(nameless).toEqual([]);
});

test("the glyph beside a name is not read out with it", async ({ shell }) => {
  // Every badge in the shell carries the first character of the app's name.
  // Without aria-hidden the Rechner would be announced as "R Rechner", and
  // an app called "Böse" as "B Böse".
  expect(await shell.evaluate(() => [...document.querySelectorAll(".dicon .g, .smitem .g, .win .g")]
    .every((g) => g.getAttribute("aria-hidden") === "true"))).toBe(true);
  await shell.getByRole("button", { name: "Start" }).click();
  // The exact name is the name alone: the badge is not part of it.
  await expect(shell.getByRole("menuitem", { name: "Rechner", exact: true })).toHaveCount(1);
  await shell.keyboard.press("Escape");
  await expect(shell.locator("#icons").getByRole("button", { name: "Rechner", exact: true })).toHaveCount(1);
});

test("the taskbar entry says which window it belongs to", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "App-Store", exact: true }).click();
  await expect(shell.getByRole("button", { name: "Fenster Rechner", exact: true })).toHaveCount(1);
  await expect(shell.getByRole("button", { name: "Fenster App-Store", exact: true })).toHaveCount(1);
  // And the keyboard can bring a minimized window back through it.
  const calc = shell.locator(".win", { hasText: "Rechner" });
  await calc.getByRole("button", { name: "Minimieren" }).click();
  await expect(calc).toBeHidden();
  await shell.getByRole("button", { name: "Fenster Rechner", exact: true }).focus();
  await shell.keyboard.press("Enter");
  await expect(calc).toBeVisible();
});

test("the page says which language it is in", async ({ shell }) => {
  // A screen reader needs it to pick the voice; every text of the shell is
  // German.
  await expect(shell.locator("html")).toHaveAttribute("lang", "de");
});
