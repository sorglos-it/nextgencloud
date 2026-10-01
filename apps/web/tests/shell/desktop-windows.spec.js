import { test, expect, hasSessionKeys, signIn } from "../fixtures/page.js";

// What a window does under the hand: dock it to an edge, pull it bigger,
// lay the windows out, close them all, and make the desktop smaller under
// them.
//
// Task 21 left all of this measured by hand and by nothing else. Handwork
// nobody repeats is a step backwards waiting to happen, so every one of them
// stands here now.
test.beforeEach(async ({ shell }) => {
  // Same gap as in desktop.spec.js: the WebKit Playwright brings has neither
  // Ed25519 nor X25519, the shell stops before the desktop, and there is
  // nothing here to look at. Measured, never assumed from the engine's name.
  test.skip(!(await hasSessionKeys(shell)),
    "no Ed25519 and no X25519 in this engine's Web Crypto: the shell stops before the desktop");
  await signIn(shell);
  await expect(shell.locator("#taskbar")).toBeVisible();
});

// The desktop, in the coordinates the window's style uses: the taskbar sits
// above it, so a page position is the style position plus this top.
async function area(shell) {
  return shell.evaluate(() => {
    const d = document.getElementById("desktop");
    const r = d.getBoundingClientRect();
    return { w: d.clientWidth, h: d.clientHeight, top: r.top };
  });
}

async function openApp(shell, name) {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name, exact: true }).click();
  // By the name in the TITLE BAR and not by any text the window holds: the app
  // store's own surface says "Rechner" as well - about a machine - and since
  // task 25 it lists the Rechner among the apps of the store, so a window
  // matched by its text would be two windows as soon as both stand open.
  return shell.locator(".win").filter({ has: shell.locator(".titlebar .t", { hasText: name }) });
}

// place puts a window somewhere known, so a case measures what it does and
// not where it happened to cascade to.
async function place(win, left, top, width, height) {
  await win.evaluate((w, g) => Object.assign(w.style, g),
    { left: left + "px", top: top + "px", width: width + "px", height: height + "px" });
}

// dragBarTo grabs the title bar 40 px in from its left - clear of the three
// buttons on its right - and drags to a page position.
async function dragBarTo(shell, win, x, y) {
  const bar = win.locator(".titlebar");
  const g = await bar.boundingBox();
  await shell.mouse.move(g.x + 40, g.y + g.height / 2);
  await shell.mouse.down();
  await shell.mouse.move(x, y, { steps: 12 });
  const hint = await shell.evaluate(() => {
    const h = document.getElementById("snaphint");
    return { shown: h.style.display === "block", left: h.style.left, top: h.style.top, width: h.style.width, height: h.style.height };
  });
  await shell.mouse.up();
  // The docking animation is 200 ms (window.js keeps the class that long).
  await shell.waitForTimeout(300);
  return hint;
}

const geom = (win) => win.evaluate((w) => ({
  left: w.style.left, top: w.style.top, width: w.style.width, height: w.style.height,
  max: w.classList.contains("max"),
}));

test("a window docks to the left edge and fills half the desktop", async ({ shell }) => {
  const win = await openApp(shell, "Rechner");
  const a = await area(shell);
  await place(win, 300, 200, 360, 260);
  const hint = await dragBarTo(shell, win, 3, a.top + 300);
  expect(hint.shown).toBe(true);
  expect(hint.left).toBe("0px");
  expect(hint.width).toBe(Math.round(a.w / 2) + "px");
  expect(await geom(win)).toEqual({
    left: "0px", top: "0px", width: Math.round(a.w / 2) + "px", height: a.h + "px", max: false,
  });
});

test("a window docks to the right edge and fills the other half", async ({ shell }) => {
  const win = await openApp(shell, "Rechner");
  const a = await area(shell);
  await place(win, 300, 200, 360, 260);
  const hint = await dragBarTo(shell, win, a.w - 3, a.top + 300);
  expect(hint.shown).toBe(true);
  expect(hint.left).toBe(Math.round(a.w / 2) + "px");
  expect(await geom(win)).toEqual({
    left: Math.round(a.w / 2) + "px", top: "0px", width: Math.round(a.w / 2) + "px",
    height: a.h + "px", max: false,
  });
});

test("a window docked at the top edge fills the whole desktop", async ({ shell }) => {
  const win = await openApp(shell, "Rechner");
  const a = await area(shell);
  await place(win, 300, 200, 360, 260);
  const hint = await dragBarTo(shell, win, Math.round(a.w / 2), a.top + 3);
  expect(hint.shown).toBe(true);
  expect(hint.width).toBe(a.w + "px");
  expect(await geom(win)).toEqual({
    left: "0px", top: "0px", width: a.w + "px", height: a.h + "px", max: true,
  });
});

test("a window dropped in the middle docks nowhere", async ({ shell }) => {
  const win = await openApp(shell, "Rechner");
  const a = await area(shell);
  await place(win, 300, 200, 360, 260);
  const hint = await dragBarTo(shell, win, Math.round(a.w / 2), a.top + Math.round(a.h / 2));
  expect(hint.shown).toBe(false);
  const g = await geom(win);
  expect(g.max).toBe(false);
  // The size is untouched; only the place changed.
  expect(g.width).toBe("360px");
  expect(g.height).toBe("260px");
});

// One case per edge of the window, because each one is its own line in
// window.js and a test that drags only the corner leaves four of them
// unwatched.
test("every edge of a window pulls it bigger and smaller", async ({ shell }) => {
  const win = await openApp(shell, "Rechner");
  const pull = async (dir, dx, dy) => {
    await place(win, 400, 200, 400, 300);
    const h = win.locator(".rs." + dir);
    const hb = await h.boundingBox();
    await shell.mouse.move(hb.x + hb.width / 2, hb.y + hb.height / 2);
    await shell.mouse.down();
    await shell.mouse.move(hb.x + hb.width / 2 + dx, hb.y + hb.height / 2 + dy, { steps: 10 });
    await shell.mouse.up();
    const g = await geom(win);
    return { left: parseInt(g.left, 10), top: parseInt(g.top, 10), width: parseInt(g.width, 10), height: parseInt(g.height, 10) };
  };
  // east and south move one edge and leave the opposite one where it is.
  expect(await pull("e", 120, 0)).toEqual({ left: 400, top: 200, width: 520, height: 300 });
  expect(await pull("e", -80, 0)).toEqual({ left: 400, top: 200, width: 320, height: 300 });
  expect(await pull("s", 0, 90)).toEqual({ left: 400, top: 200, width: 400, height: 390 });
  // west and north move the near edge, so the far one stays put: left plus
  // width and top plus height are the same before and after.
  expect(await pull("w", -100, 0)).toEqual({ left: 300, top: 200, width: 500, height: 300 });
  expect(await pull("n", 0, -80)).toEqual({ left: 400, top: 120, width: 400, height: 380 });
  // the two corners move two edges at once.
  expect(await pull("se", 120, 90)).toEqual({ left: 400, top: 200, width: 520, height: 390 });
  expect(await pull("sw", -120, 90)).toEqual({ left: 280, top: 200, width: 520, height: 390 });
});

test("a window never gets smaller than 240 x 150", async ({ shell }) => {
  const win = await openApp(shell, "Rechner");
  await place(win, 400, 200, 400, 300);
  const h = win.locator(".rs.se");
  const hb = await h.boundingBox();
  await shell.mouse.move(hb.x + hb.width / 2, hb.y + hb.height / 2);
  await shell.mouse.down();
  await shell.mouse.move(hb.x - 900, hb.y - 900, { steps: 10 });
  await shell.mouse.up();
  const g = await geom(win);
  expect(g.width).toBe("240px");
  expect(g.height).toBe("150px");
  // Small enough is still large enough to be closed by clicking.
  await expect(win.getByRole("button", { name: "Schließen" })).toBeVisible();
  await win.getByRole("button", { name: "Schließen" }).click();
  await expect(win).toHaveCount(0);
});

test("a maximized window has no handles to pull", async ({ shell }) => {
  const win = await openApp(shell, "Rechner");
  await win.getByRole("button", { name: "Maximieren" }).click();
  await expect(win).toHaveClass(/max/);
  await expect(win.locator(".rs.se")).toBeHidden();
});

test("Fenster anordnen lays the windows out without overlap", async ({ shell }) => {
  await openApp(shell, "Rechner");
  await openApp(shell, "App-Store");
  await openApp(shell, "Benutzer und Rechte");
  const a = await area(shell);
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("button", { name: "Fenster anordnen" }).click();
  // The menu is out of the way afterwards, so the layout can be seen.
  await expect(shell.locator("#startmenu")).not.toHaveClass(/open/);
  await shell.waitForTimeout(300);
  const boxes = await shell.locator(".win").evaluateAll((ws) => ws.map((w) => ({
    x: parseInt(w.style.left, 10), y: parseInt(w.style.top, 10),
    w: parseInt(w.style.width, 10), h: parseInt(w.style.height, 10),
    max: w.classList.contains("max"),
  })));
  expect(boxes).toHaveLength(3);
  // Three windows go into two columns and two rows, and none of them is
  // maximized any more.
  const cw = Math.floor(a.w / 2), ch = Math.floor(a.h / 2);
  expect(boxes).toEqual([
    { x: 0, y: 0, w: cw, h: ch, max: false },
    { x: cw, y: 0, w: cw, h: ch, max: false },
    { x: 0, y: ch, w: cw, h: ch, max: false },
  ]);
  for (let i = 0; i < boxes.length; i++) {
    for (let j = i + 1; j < boxes.length; j++) {
      const p = boxes[i], q = boxes[j];
      const over = p.x < q.x + q.w && q.x < p.x + p.w && p.y < q.y + q.h && q.y < p.y + p.h;
      expect(over, `window ${i} and window ${j} overlap`).toBe(false);
    }
  }
});

test("Fenster anordnen leaves a minimized window minimized", async ({ shell }) => {
  const calc = await openApp(shell, "Rechner");
  await openApp(shell, "App-Store");
  await openApp(shell, "Benutzer und Rechte");
  await calc.getByRole("button", { name: "Minimieren" }).click();
  await expect(calc).toBeHidden();
  const before = await calc.evaluate((w) => w.style.left + "/" + w.style.top);
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("button", { name: "Fenster anordnen" }).click();
  await shell.waitForTimeout(300);
  await expect(calc).toBeHidden();
  expect(await calc.evaluate((w) => w.style.left + "/" + w.style.top)).toBe(before);
  // The two that are on screen share the desktop between them.
  const a = await area(shell);
  expect(await shell.locator(".win:not(.min)").evaluateAll((ws) => ws.map((w) => w.style.width + "x" + w.style.height)))
    .toEqual([Math.floor(a.w / 2) + "px" + "x" + a.h + "px", Math.floor(a.w / 2) + "px" + "x" + a.h + "px"]);
});

test("Alle schließen closes every window and empties the taskbar", async ({ shell }) => {
  const calc = await openApp(shell, "Rechner");
  await openApp(shell, "App-Store");
  await openApp(shell, "Benutzer und Rechte");
  await calc.getByRole("button", { name: "Minimieren" }).click();
  await expect(shell.locator(".win")).toHaveCount(3);
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("button", { name: "Alle schließen" }).click();
  // A minimized window is a window too, and its taskbar entry has to go with
  // it - an entry for a window that is gone is a button that does nothing.
  await expect(shell.locator(".win")).toHaveCount(0);
  await expect(shell.locator("#tasks .task")).toHaveCount(0);
  await expect(shell.locator("#startmenu")).not.toHaveClass(/open/);
  // And the desktop is still a desktop afterwards.
  await openApp(shell, "Rechner");
  await expect(shell.locator(".win")).toHaveCount(1);
});

test("Alle schließen with nothing open does nothing and breaks nothing", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("button", { name: "Alle schließen" }).click();
  await expect(shell.locator(".win")).toHaveCount(0);
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("button", { name: "Fenster anordnen" }).click();
  await expect(shell.locator(".win")).toHaveCount(0);
  await openApp(shell, "Rechner");
  await expect(shell.locator(".win")).toHaveCount(1);
});

test("a window left outside is pulled back when the desktop shrinks", async ({ shell }) => {
  const win = await openApp(shell, "Rechner");
  await place(win, 900, 600, 300, 150);
  await shell.setViewportSize({ width: 700, height: 500 });
  // The desktop answers the resize event; give the layout a frame.
  await shell.waitForTimeout(250);
  const a = await area(shell);
  const g = await geom(win);
  // Enough of the window has to stay on the desktop that the title bar can
  // be grabbed again: window.js keeps 80 across and 40 down.
  expect(parseInt(g.left, 10)).toBeLessThanOrEqual(a.w - 80);
  expect(parseInt(g.top, 10)).toBeLessThanOrEqual(a.h - 40);
  const bar = await win.locator(".titlebar").boundingBox();
  expect(bar.x).toBeLessThan(a.w);
  expect(bar.y).toBeLessThan(a.top + a.h);
});

test("a maximized window keeps filling the desktop when it changes size", async ({ shell }) => {
  const win = await openApp(shell, "Rechner");
  await win.getByRole("button", { name: "Maximieren" }).click();
  for (const size of [{ width: 700, height: 500 }, { width: 1100, height: 900 }]) {
    await shell.setViewportSize(size);
    await shell.waitForTimeout(250);
    const a = await area(shell);
    expect(await geom(win)).toEqual({ left: "0px", top: "0px", width: a.w + "px", height: a.h + "px", max: true });
  }
});

// ---------------------------------------------------------------------------
// The four findings of round 1 that live in window.js (K1, K2, K4).
// ---------------------------------------------------------------------------

test("a double click on the title bar maximizes and restores, over and over", async ({ shell }) => {
  // K1. Four double clicks used to leave the window maximized four times:
  // pointerdown tore the maximized window loose and dblclick put it straight
  // back. This is the first gesture anybody tries on a window.
  const win = await openApp(shell, "Rechner");
  await place(win, 300, 200, 360, 260);
  const bar = win.locator(".titlebar");
  const beat = { position: { x: 40, y: 18 } };
  for (let i = 0; i < 2; i++) {
    await bar.dblclick(beat);
    await shell.waitForTimeout(300);
    await expect(win).toHaveClass(/max/);
    await bar.dblclick(beat);
    await shell.waitForTimeout(300);
    await expect(win).not.toHaveClass(/max/);
    expect(await geom(win)).toEqual({
      left: "300px", top: "200px", width: "360px", height: "260px", max: false,
    });
  }
});

test("a maximized window torn loose keeps the spot that was grabbed", async ({ shell }) => {
  // K2. The width was read in the middle of the .win.anim cross-fade, so it
  // was still the maximized one and the window jumped to left:0 - the left
  // edge of the screen, where it is easy to lose.
  const win = await openApp(shell, "Rechner");
  const a = await area(shell);
  await place(win, 300, 200, 360, 260);
  await win.getByRole("button", { name: "Maximieren" }).click();
  await expect(win).toHaveClass(/max/);
  await shell.waitForTimeout(300);
  const grabX = 400;
  const bar = win.locator(".titlebar");
  const bb = await bar.boundingBox();
  await shell.mouse.move(grabX, bb.y + bb.height / 2);
  await shell.mouse.down();
  await shell.mouse.move(grabX, bb.y + bb.height / 2 + 120, { steps: 10 });
  await shell.mouse.up();
  await shell.waitForTimeout(300);
  const g = await geom(win);
  expect(g.max).toBe(false);
  expect(g.width).toBe("360px");
  // The point under the pointer stays where it was: it sat grabX/a.w of the
  // way across the maximized window, so it sits there in the small one too.
  const want = Math.round(grabX - 360 * (grabX / a.w));
  expect(Math.abs(parseInt(g.left, 10) - want)).toBeLessThanOrEqual(2);
});

test("a press on a maximized title bar alone does not tear it loose", async ({ shell }) => {
  // The other half of K1: only a real drag tears a maximized window loose.
  // A press that goes nowhere is a click, and a click on a title bar does
  // nothing at all.
  const win = await openApp(shell, "Rechner");
  await win.getByRole("button", { name: "Maximieren" }).click();
  await expect(win).toHaveClass(/max/);
  await shell.waitForTimeout(300);
  const bar = win.locator(".titlebar");
  const bb = await bar.boundingBox();
  await shell.mouse.move(bb.x + 40, bb.y + bb.height / 2);
  await shell.mouse.down();
  await shell.mouse.up();
  await shell.waitForTimeout(300);
  await expect(win).toHaveClass(/max/);
});

test("pulling the north edge past the top leaves the bottom edge where it was", async ({ shell }) => {
  // K4. The top was clamped at the desktop's edge but the height kept
  // growing, so the bottom edge walked down the screen with it.
  const win = await openApp(shell, "Rechner");
  await place(win, 400, 10, 400, 300);
  const h = win.locator(".rs.n");
  const hb = await h.boundingBox();
  await shell.mouse.move(hb.x + hb.width / 2, hb.y + hb.height / 2);
  await shell.mouse.down();
  await shell.mouse.move(hb.x + hb.width / 2, hb.y + hb.height / 2 - 200, { steps: 10 });
  await shell.mouse.up();
  const g = await geom(win);
  expect(parseInt(g.top, 10)).toBe(0);
  // 10 + 300 was the bottom before, and it is the bottom after.
  expect(parseInt(g.top, 10) + parseInt(g.height, 10)).toBe(310);
});
