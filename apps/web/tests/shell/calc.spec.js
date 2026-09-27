import { test, expect, hasSessionKeys, signIn } from "../fixtures/page.js";

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
  await signIn(shell);
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

// ---- the keyboard -------------------------------------------------------
//
// Task 21 left the keyboard of the calculator measured by hand only. It is
// the one app the owner can touch today, so what it does under the fingers
// is written down here.

// calc opens the calculator and hands back the window, the two lines of the
// display and a way to press a key.
async function calc(shell) {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  const win = shell.locator(".win", { hasText: "Rechner" });
  await expect(win).toBeVisible();
  return {
    win,
    value: win.locator(".calc .v"),
    expr: win.locator(".calc .e"),
    // One key at a time, the way a finger does it.
    async type(keys) {
      for (const k of keys) await shell.keyboard.press(k);
    },
  };
}

test("the calculator can be worked with the keyboard alone", async ({ shell }) => {
  const c = await calc(shell);
  await c.type(["1", "2", "+", "3", "Enter"]);
  await expect(c.value).toHaveText("15");
  // Point before line on the keyboard too, and "=" does what Enter does.
  await c.type(["Escape", "2", "+", "3", "*", "4", "="]);
  await expect(c.value).toHaveText("14");
  await c.type(["Escape", "7", "/", "2", "Enter"]);
  await expect(c.value).toHaveText("3.5");
  // A division by zero is a mistake, not Infinity.
  await c.type(["Escape", "1", "/", "0", "Enter"]);
  await expect(c.value).toHaveText("Fehler");
});

test("Escape clears, Backspace takes the last digit back", async ({ shell }) => {
  const c = await calc(shell);
  await c.type(["9", "8", "7"]);
  await expect(c.value).toHaveText("987");
  await c.type(["Backspace"]);
  await expect(c.value).toHaveText("98");
  await c.type(["Backspace", "Backspace"]);
  await expect(c.value).toHaveText("0");
  // One more on an empty display is not an error and not a minus sign.
  await c.type(["Backspace"]);
  await expect(c.value).toHaveText("0");
  // Escape has to clear BOTH lines: the sum so far stands above, and a
  // cleared calculator that keeps it is a calculator that adds it back in.
  await c.type(["4", "2", "+", "7"]);
  await expect(c.expr).toHaveText("42 + ");
  await expect(c.value).toHaveText("7");
  await c.type(["Escape"]);
  await expect(c.value).toHaveText("0");
  await expect(c.expr).toHaveText("");
  // And what was cleared is really gone, not just out of sight.
  await c.type(["3", "Enter"]);
  await expect(c.value).toHaveText("3");
});

test("the comma key and the point key both give a decimal point", async ({ shell }) => {
  // A German keyboard has the comma on the letters and the point on the
  // number block. Both mean the same thing to somebody adding up a bill.
  const c = await calc(shell);
  await c.type(["1", "Comma", "5"]);
  await expect(c.value).toHaveText("1.5");
  await c.type(["Escape", "3", "Period", "2", "5"]);
  await expect(c.value).toHaveText("3.25");
  // And a second one does not make a second point.
  await c.type(["Comma", "Period", "1"]);
  await expect(c.value).toHaveText("3.251");
});

test("a key the calculator has no use for is left alone", async ({ shell }) => {
  const c = await calc(shell);
  await c.type(["5"]);
  await expect(c.value).toHaveText("5");
  for (const k of ["a", "(", ")", "F5", "ArrowLeft", "Home"]) await c.type([k]);
  await expect(c.value).toHaveText("5");
});

test("the keyboard belongs to the window in front", async ({ shell }) => {
  const c = await calc(shell);
  await c.type(["5"]);
  await expect(c.value).toHaveText("5");
  // A second window takes the focus; the calculator behind it must not eat
  // the digits meant for that one.
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "App-Store", exact: true }).click();
  await shell.keyboard.press("7");
  await expect(c.value).toHaveText("5");
  // And it gets them back when it is in front again.
  await c.win.locator(".titlebar").click();
  await shell.keyboard.press("7");
  await expect(c.value).toHaveText("57");
});

test("typing into the start menu's search does not reach the calculator", async ({ shell }) => {
  const c = await calc(shell);
  await c.type(["5"]);
  // The window keeps the shell's focus while the menu is open, so without
  // the guard in calc.js every digit would land in both places.
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.keyboard.type("777");
  await expect(shell.locator("#smsearch")).toHaveValue("777");
  await expect(c.value).toHaveText("5");
});

test("a closed calculator takes its keyboard listener with it", async ({ shell }) => {
  // Counted, not guessed. A listener that stays behind is invisible from the
  // outside - calc.js checks which window has the focus, so a stale one does
  // nothing noticeable - and it is still a window's worth of the page that
  // never gets freed. Opening and closing is what a person does all day.
  await shell.evaluate(() => {
    window.__keydowns = 0;
    const add = window.addEventListener.bind(window);
    const rm = window.removeEventListener.bind(window);
    window.addEventListener = (t, ...rest) => { if (t === "keydown") window.__keydowns++; return add(t, ...rest); };
    window.removeEventListener = (t, ...rest) => { if (t === "keydown") window.__keydowns--; return rm(t, ...rest); };
  });
  for (let round = 0; round < 3; round++) {
    const c = await calc(shell);
    expect(await shell.evaluate(() => window.__keydowns)).toBe(1);
    await c.type(["5"]);
    await expect(c.value).toHaveText("5");
    await c.win.getByRole("button", { name: "Schließen" }).click();
    await expect(c.win).toHaveCount(0);
    expect(await shell.evaluate(() => window.__keydowns)).toBe(0);
  }
  // And nothing is left over: a fresh calculator starts at nought.
  await shell.keyboard.press("7");
  const again = await calc(shell);
  await expect(again.value).toHaveText("0");
  await again.type(["7"]);
  await expect(again.value).toHaveText("7");
});
