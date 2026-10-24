import { test, expect, signIn, skipWithoutLogin } from "../fixtures/page.js";

// Getting through the desktop with the keyboard alone, and what a screen
// reader is told. The people this is built for include the ones who never
// touch a mouse.
//
// What is measured here is what holds. The review of round 1 found that
// role="menu" promised keys the menu did not have and that the focus was
// dropped on the floor whenever the menu closed (W2); the fixing round made
// the menu do what it says, and the cases at the foot of this file hold it
// to that.
test.beforeEach(async ({ shell }) => {
  await skipWithoutLogin(shell);
  await signIn(shell);
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

// REPLACED IN THE FIXING ROUND, AND IT ASKS FOR MORE THAN IT DID.
//
// It used to say every tile is a tab stop, which is what the shell did: at
// 1000 apps that is 1000 presses of Tab to reach the buttons under the list,
// and W2 named it. A role="menu" is walked with the arrow keys and is ONE
// stop on the way through - so that is what is asked for here now: the arrows
// reach every tile in order, Home and End reach the ends, and one more Tab is
// past the whole list rather than one tile further into it.
test("the list is one tab stop and the arrow keys walk every tile of it", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  const want = await shell.locator("#smlist .smitem").evaluateAll((is) => is.map((i) => i.textContent.trim()));
  expect(want.length).toBeGreaterThan(3);

  // One Tab out of the search field lands on the list, at its first tile.
  await shell.keyboard.press("Tab");
  expect(await active(shell)).toBe(want[0]);

  // The arrows walk the rest of it, and they come round at the end.
  const seen = [want[0]];
  for (let i = 1; i < want.length; i++) {
    await shell.keyboard.press("ArrowDown");
    seen.push(await active(shell));
  }
  expect(seen).toEqual(want);
  await shell.keyboard.press("ArrowDown");
  expect(await active(shell)).toBe(want[0]);
  await shell.keyboard.press("ArrowUp");
  expect(await active(shell)).toBe(want[want.length - 1]);
  await shell.keyboard.press("Home");
  expect(await active(shell)).toBe(want[0]);
  await shell.keyboard.press("End");
  expect(await active(shell)).toBe(want[want.length - 1]);

  // ArrowRight and ArrowLeft do the same: the tiles stand in a grid, and
  // which way somebody reads it is their own business.
  await shell.keyboard.press("Home");
  await shell.keyboard.press("ArrowRight");
  expect(await active(shell)).toBe(want[1]);
  await shell.keyboard.press("ArrowLeft");
  expect(await active(shell)).toBe(want[0]);

  // And the next Tab is past the list, not one tile further into it.
  await shell.keyboard.press("Tab");
  expect(want).not.toContain(await active(shell));
  expect(await active(shell)).toBe("Hell");

  // Exactly one tile is reachable by Tab at any moment; the others are not.
  expect(await shell.locator("#smlist .smitem").evaluateAll((is) =>
    is.filter((i) => i.getAttribute("tabindex") === "0").length)).toBe(1);
});

test("the three buttons of a window are tab stops, and the app is the next one", async ({ shell }) => {
  // WHAT THIS CASE WALKED BEFORE TASK 25: three title-bar buttons and then the
  // calculator's twenty keys, all in the shell's own document. The calculator
  // runs in a frame now, so Tab leaves this document at the app - and from the
  // shell every one of those twenty presses answers with the frame. The walk is
  // therefore cut where the document is: the three buttons here, the twenty keys
  // in calc.spec.js ("every key of the calculator is a tab stop"), which reads
  // the focus inside the frame and holds the same twenty names in the same
  // order. Nothing stopped being measured; it is measured where it now is.
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  const win = shell.locator(".win");
  for (const name of ["Minimieren", "Maximieren", "Schließen"]) {
    await win.getByRole("button", { name, exact: true }).focus();
    expect(await active(shell)).toBe(name);
  }
  await win.getByRole("button", { name: "Minimieren", exact: true }).focus();
  const stops = [];
  for (let i = 0; i < 2; i++) {
    await shell.keyboard.press("Tab");
    stops.push(await active(shell));
  }
  expect(stops).toEqual(["Maximieren", "Schließen"]);
  // And the next press leaves the title bar and lands in the app.
  await shell.keyboard.press("Tab");
  await expect(win.locator(".appframe")).toBeFocused();
});

test("everything the keyboard can reach shows a ring while it is there", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  await shell.evaluate(() => document.activeElement.blur());
  // Walk far enough to pass the taskbar and the window's buttons. The app's
  // frame is left out of the count on purpose: the keyboard inside an app is in
  // the app's document, where the ring comes from the app's own stylesheet, and
  // a ring the shell drew around the whole window on top of it would be noise.
  // The measurement does not fall away with it - the ring on the app's own
  // controls is measured in the frame below.
  const bad = [];
  for (let i = 0; i < 12; i++) {
    await shell.keyboard.press("Tab");
    const r = await shell.evaluate(() => {
      const a = document.activeElement;
      if (!a || a === document.body || a.classList.contains("appframe")) return null;
      const s = getComputedStyle(a);
      return { what: (a.getAttribute("aria-label") || a.textContent || "").trim(), style: s.outlineStyle, width: parseFloat(s.outlineWidth) };
    });
    if (r && (r.style === "none" || !(r.width >= 2))) bad.push(r);
  }
  expect(bad).toEqual([]);
  // And inside the app, on one of its own keys.
  const frame = shell.frameLocator(".appframe");
  const key = frame.getByRole("button", { name: "C", exact: true });
  await key.focus();
  const ring = await key.evaluate((e) => {
    const s = getComputedStyle(e);
    return { style: s.outlineStyle, width: parseFloat(s.outlineWidth) };
  });
  expect(ring.style).not.toBe("none");
  expect(ring.width).toBeGreaterThanOrEqual(2);
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
  // Every badge in the shell carries an icon of the shell's own (icon.js).
  // Without aria-hidden it would be announced along with the name, and an
  // app whose icon the shell does not know would read as "App Böse".
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
  // By the title bar: the app store's window lists the Rechner among the apps
  // of the store since task 25, so "the window that says Rechner" is two.
  const calc = shell.locator(".win").filter({ has: shell.locator(".titlebar .t", { hasText: "Rechner" }) });
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

// ---------------------------------------------------------------------------
// The two findings of round 1 about promising what is not there (W1, W2).
// ---------------------------------------------------------------------------

test("the place a notice appears in can be found before there is a notice", async ({ shell }) => {
  // W1. desktop.js said the live region "stays, empty or not" and the
  // stylesheet took it out of the page with display:none, so a screen reader
  // had nothing to watch and a blind person was never told anything. The
  // region is in the page from the start now, and it takes up no room until
  // there is something to say.
  await expect(shell.getByRole("status")).toHaveCount(1);
  await expect(shell.getByRole("status")).toHaveText("");
  expect(await shell.evaluate(() => {
    const e = document.querySelector(".status");
    const c = getComputedStyle(e);
    return { display: c.display, visibility: c.visibility, height: e.getBoundingClientRect().height };
  })).toEqual({ display: "block", visibility: "visible", height: 0 });
  // And it is the same element that carries the notice a moment later - not
  // a second one built when the news arrives.
  const mark = await shell.evaluate(() => {
    document.querySelector(".status").dataset.mark = "one";
    window.__ngcSendNotice("warn", "Eine Meldung.");
    return document.querySelector(".status").dataset.mark;
  });
  expect(mark).toBe("one");
  await expect(shell.getByRole("status")).toHaveText("Eine Meldung.");
});

test("Escape gives the focus back to the button the menu was opened with", async ({ shell }) => {
  // W2. The focus used to land on <body>, where the next Tab starts over at
  // the top of the page.
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.keyboard.press("Escape");
  await expect(shell.getByRole("button", { name: "Start" })).toBeFocused();
  // From wherever in the menu it sat, not only from the search field.
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.locator("#smlist .smitem").first().focus();
  await shell.keyboard.press("Escape");
  await expect(shell.getByRole("button", { name: "Start" })).toBeFocused();
});

test("starting an app puts the focus in the window it opened", async ({ shell }) => {
  // W2, the other half: after a start the focus used to be on <body>, so the
  // keyboard had to walk the whole page to reach the window it had just asked
  // for.
  //
  // SINCE TASK 25 the Rechner runs in a frame, and the keyboard goes one step
  // further: into the app itself, the way an installed desktop puts the cursor
  // into the program it just started. It has to - an app's page is a document of
  // its own, and a key the shell holds on to is a key the app never gets. Both
  // are "inside the window that was opened", which is what this case is about.
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  const win = shell.locator(".win", { hasText: "Rechner" });
  await expect(win.locator(".appframe")).toBeFocused();
  // Typing goes straight into the app, with nothing to click first.
  await shell.keyboard.press("7");
  await expect(shell.frameLocator(".appframe").locator(".calc .v")).toHaveText("7");
  // A window the shell drew itself keeps the focus on the window, and from
  // there the window's own buttons are the way on.
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "App-Store", exact: true }).click();
  await expect(shell.locator(".win", { hasText: "App-Store" })).toBeFocused();
  await shell.keyboard.press("Tab");
  expect(await active(shell)).toBe("Minimieren");
});

test("an app that opens no window gives the focus back instead of dropping it", async ({ shell }) => {
  // "Fotos" is unreachable in the stand-in's list: no window opens, so there
  // is nowhere to send the focus but back where it came from.
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Fotos", exact: true }).click();
  await expect(shell.getByRole("status")).toHaveText("Fotos ist nicht erreichbar.");
  await expect(shell.getByRole("button", { name: "Start" })).toBeFocused();
});

test("Tab stays inside the open menu instead of walking off behind it", async ({ shell }) => {
  // W2. Nothing held the focus in the menu, so Tab walked out into a desktop
  // the menu was covering - into controls the person cannot see.
  await shell.getByRole("button", { name: "Start" }).click();
  const inMenu = () => shell.evaluate(() =>
    document.getElementById("startmenu").contains(document.activeElement));
  for (let i = 0; i < 30; i++) {
    await shell.keyboard.press("Tab");
    expect(await inMenu(), `Tab number ${i + 1} left the menu`).toBe(true);
  }
  await shell.locator("#smsearch").focus();
  await shell.keyboard.press("Shift+Tab");
  expect(await inMenu()).toBe(true);
});

test("the arrow keys reach the app list from the search field", async ({ shell }) => {
  // Typing and then pressing down is how somebody looks for an app without a
  // mouse; without this the search is a dead end.
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.locator("#smsearch").fill("rech");
  await shell.keyboard.press("ArrowDown");
  // The name alone: since the badge carries an icon instead of the first
  // letter (js/shell/icon.js), there is no "B" in front of it any more.
  expect(await active(shell)).toBe("Benutzer und Rechte");
  await shell.keyboard.press("Enter");
  await expect(shell.locator(".win", { hasText: "Benutzer und Rechte" })).toBeVisible();
});
