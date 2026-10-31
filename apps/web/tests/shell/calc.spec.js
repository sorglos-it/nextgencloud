import { test, expect, signIn, skipWithoutLogin } from "../fixtures/page.js";

// The calculator - org.sorglos.calculator - in the fenced frame every app runs
// in.
//
// IT MOVED IN TASK 25, and the nine cases that stood here before the move stand
// here after it. What changed is WHERE the keys and the display are looked up:
// inside shell.frameLocator(".appframe"), which is the brief's own locator,
// instead of in the shell's own document. What they assert did not change - the
// keys, the presses, the 15, the 14, the "Fehler", the comma and the point, the
// window in front, the start menu - because the point of the move is that the
// calculator still does all of it.
//
// ONE OF THE NINE CHANGED WHAT IT COUNTS, and it says so where it stands: the
// one about the keyboard listener. There is no listener of the calculator's in
// the shell any more to count, and that is the move itself.
//
// TWO ARE NEW, at the foot of the file, and both are about what the shell's own
// cases cannot see any more: the names of the keys and the walk through them
// with Tab both live in the app's document now.
test.beforeEach(async ({ shell }) => {
  // Same gap as in desktop.spec.js: no session keys, no desktop, no window to
  // put a calculator in. The proof of a user interface is an Ed25519 signature
  // as well, so this engine could not open the frame either.
  await skipWithoutLogin(shell);
  await signIn(shell);
  await expect(shell.locator("#taskbar")).toBeVisible();
});

// winOf finds a window by the name in its title bar. NOT by any text the window
// holds: the app store's own surface says "Rechner" too - about a machine - and
// since task 25 it lists the Rechner among the apps of the store, so a window
// matched by its text is two windows.
const winOf = (shell, title) =>
  shell.locator(".win").filter({ has: shell.locator(".titlebar .t", { hasText: title }) });

// active is the name of whatever has the focus INSIDE the frame. document in
// there is the app's own, so this is the app's answer and not the shell's -
// which answers with the frame itself for every one of these presses.
const active = (frame) => frame.locator("body").evaluate(() => {
  const a = document.activeElement;
  if (!a || a === document.body) return "(body)";
  return (a.getAttribute("aria-label") || a.textContent || "").trim();
});

// calc opens the calculator and hands back the window, the frame, the two lines
// of the display and a way to press a key.
async function calc(shell) {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  const win = winOf(shell, "Rechner");
  await expect(win).toBeVisible();
  const frame = shell.frameLocator(".appframe");
  // Nothing is pressed before the app is really in the frame. Its interface is
  // fetched, hashed and held against a signature first, and a key pressed
  // before that has nowhere to land.
  await expect(frame.locator(".calc .v")).toHaveText("0");
  return {
    win,
    frame,
    value: frame.locator(".calc .v"),
    expr: frame.locator(".calc .e"),
    // exact, because a name is matched as a substring by default and the "C"
    // is a letter of "Vorzeichen" and of "Zurück" as well.
    key: (k) => frame.getByRole("button", { name: k, exact: true }),
    // One key at a time, the way a finger does it. The keys go to the shell's
    // page and the browser hands them to whichever document has the focus -
    // which is the app's, and that is what makes the next line a measurement.
    async type(keys) {
      for (const k of keys) await shell.keyboard.press(k);
    },
  };
}

test("the calculator computes without eval", async ({ shell }) => {
  const c = await calc(shell);
  for (const key of ["1", "2", "+", "3", "="]) await c.key(key).click();
  await expect(c.value).toHaveText("15");
});

test("point before line, and a division by zero is an error, not Infinity", async ({ shell }) => {
  const c = await calc(shell);
  for (const k of ["2", "+", "3", "×", "4", "="]) await c.key(k).click();
  await expect(c.value).toHaveText("14");
  for (const k of ["C", "1", "÷", "0", "="]) await c.key(k).click();
  await expect(c.value).toHaveText("Fehler");
});

// ---- the keyboard -------------------------------------------------------
//
// Task 21 left the keyboard of the calculator measured by hand only. It is
// the one app the owner can touch today, so what it does under the fingers
// is written down here - and after the move it is the one thing that had to
// go on working key for key.

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
  // the digits meant for that one. BEFORE THE MOVE a check in calc.js asked
  // the desktop which window was in front; now the app simply has a document
  // of its own, and a document that does not hold the focus is handed no keys.
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
  // The window keeps the shell's idea of which window is in front while the
  // menu is open, so before the move every digit landed in both places and
  // calc.js needed a guard against it. The open menu takes the keyboard to its
  // search field, and the app's document is not where it is any more.
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.keyboard.type("777");
  await expect(shell.locator("#smsearch")).toHaveValue("777");
  await expect(c.value).toHaveText("5");
});

test("a closed calculator leaves nothing of itself in the shell", async ({ shell }) => {
  // WHAT THIS CASE COUNTED BEFORE TASK 25, and why the number is a different
  // one now. The calculator used to hang a keydown listener on the shell's own
  // window, and one left behind was invisible from the outside - it only
  // checked which window had the focus, so a stale one did nothing noticeable
  // and still cost a window's worth of the page for ever. So the case counted
  // the listeners and wanted exactly one while the window stood.
  //
  // The calculator listens in its own document now. The number to measure in
  // the shell is therefore ZERO, in every round and at every moment: the move
  // IS that the shell no longer hears a single key of an app. What takes the
  // listener away is no longer a line of calc.js at all - the window takes the
  // whole document with it, which is what the frame count below says.
  await shell.evaluate(() => {
    window.__keydowns = 0;
    const add = window.addEventListener.bind(window);
    const rm = window.removeEventListener.bind(window);
    window.addEventListener = (t, ...rest) => { if (t === "keydown") window.__keydowns++; return add(t, ...rest); };
    window.removeEventListener = (t, ...rest) => { if (t === "keydown") window.__keydowns--; return rm(t, ...rest); };
  });
  for (let round = 0; round < 3; round++) {
    const c = await calc(shell);
    await expect(shell.locator(".appframe")).toHaveCount(1);
    expect(await shell.evaluate(() => window.__keydowns)).toBe(0);
    await c.type(["5"]);
    await expect(c.value).toHaveText("5");
    await c.win.getByRole("button", { name: "Schließen" }).click();
    await expect(c.win).toHaveCount(0);
    // The document the listener lived in is gone with the window.
    await expect(shell.locator(".appframe")).toHaveCount(0);
    expect(await shell.evaluate(() => window.__keydowns)).toBe(0);
  }
  // And nothing is left over: a fresh calculator starts at nought, and the key
  // pressed while no window stood reached nobody.
  await shell.keyboard.press("7");
  const again = await calc(shell);
  await expect(again.value).toHaveText("0");
  await again.type(["7"]);
  await expect(again.value).toHaveText("7");
});

// ---- the app's own page -------------------------------------------------
//
// What the shell's own cases cannot see any more: the calculator's page is a
// document of its own, so desktop-keyboard.spec.js - which walks the shell -
// stops at the frame. The two rules it holds the shell to are held to the app
// here, and the look the shell hands in is measured with them.

test("the app is painted in the shell's colours, and follows when they change", async ({ shell }) => {
  // The shell writes the theme on <html> of the app's document before the first
  // paint, so a window is never painted dark and repainted light one frame
  // later; afterwards the app follows through ngc.onTheme. Both halves are here,
  // because only the first one is visible in a screenshot.
  const c = await calc(shell);
  // SINCE docs/gestaltung.md POINT 7 the shell follows the machine until
  // somebody chooses, and then its own document carries NO design to copy -
  // the stylesheet answers instead, and this document has no stylesheet of
  // ours in it. So the frame is handed the design that is actually on the
  // screen (theme.js effectiveTheme), and the fixture's machine is light
  // (playwright.config.js). That is the case this file could not have before:
  // the shell saying nothing and the app still being painted right.
  await expect(shell.locator("html")).not.toHaveAttribute("data-theme", /.*/);
  await expect(c.frame.locator("html")).toHaveAttribute("data-theme", "light");
  await shell.getByRole("button", { name: "Design wechseln" }).click();
  await expect(shell.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(c.frame.locator("html")).toHaveAttribute("data-theme", "dark");
  // And the accent is a colour, which no stylesheet can read out of an
  // attribute: the app writes it into a property of its own.
  const accent = await c.frame.locator("html").evaluate((h) =>
    getComputedStyle(h).getPropertyValue("--accent").trim());
  expect(accent).toMatch(/^#[0-9a-fA-F]{3,8}$/);
});

test("every key of the calculator has a name a screen reader can read", async ({ shell }) => {
  const c = await calc(shell);
  const nameless = await c.frame.locator("button").evaluateAll((bs) => bs
    .filter((b) => !(b.getAttribute("aria-label") || b.textContent || "").trim())
    .map((b) => b.className || b.tagName));
  expect(nameless).toEqual([]);
  // Twenty keys, and the three whose character is no word carry the word.
  await expect(c.frame.locator(".calc .keys button")).toHaveCount(20);
  for (const [label, name] of [["±", "Vorzeichen"], [",", "Komma"], ["⌫", "Zurück"]]) {
    await expect(c.key(name)).toHaveCount(1);
    await expect(c.key(name)).toHaveText(label);
  }
});

test("every key of the calculator is a tab stop, in the order it is drawn", async ({ shell }) => {
  // The walk through the shell (desktop-keyboard.spec.js) ends at the frame,
  // because Tab there leaves the shell's document. It goes on in here.
  const c = await calc(shell);
  const order = ["C", "Vorzeichen", "%", "÷", "7", "8", "9", "×",
    "4", "5", "6", "−", "1", "2", "3", "+", "0", "Komma", "Zurück", "="];
  await c.key(order[0]).focus();
  const stops = [await active(c.frame)];
  for (let i = 1; i < order.length; i++) {
    await shell.keyboard.press("Tab");
    stops.push(await active(c.frame));
  }
  expect(stops).toEqual(order);
});
