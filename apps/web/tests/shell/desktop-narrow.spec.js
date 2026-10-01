import { test, expect, signIn, skipWithoutLogin } from "../fixtures/page.js";

// The desktop on a narrow screen. The stylesheet has one rule at 640 px -
// the desktop icons step aside and the taskbar entries drop their words -
// and Task 21 never measured what that leaves behind.
//
// 375 x 700 is a phone held upright, the narrowest thing this has to survive.
test.use({ viewport: { width: 375, height: 700 } });

test.beforeEach(async ({ shell }) => {
  await skipWithoutLogin(shell);
  await signIn(shell);
  await expect(shell.locator("#taskbar")).toBeVisible();
});

// inside reports whether the box of sel sits inside the box of outer, which
// is either a selector or the viewport.
const inside = (shell, sel, outer) => shell.evaluate(([s, o]) => {
  const a = document.querySelector(s).getBoundingClientRect();
  const b = o === "viewport"
    ? { left: 0, top: 0, right: innerWidth, bottom: innerHeight }
    : document.querySelector(o).getBoundingClientRect();
  return a.left >= b.left - 1 && a.right <= b.right + 1 && a.top >= b.top - 1 && a.bottom <= b.bottom + 1;
}, [sel, outer]);

test("the icons step aside and the taskbar keeps its words to itself", async ({ shell }) => {
  await expect(shell.locator("#icons")).toBeHidden();
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  const tab = shell.locator("#tasks .task");
  await expect(tab).toBeVisible();
  await expect(tab.locator(".lbl")).toBeHidden();
  // The word is gone from the screen, not from the screen reader.
  await expect(tab).toHaveAttribute("aria-label", "Fenster Rechner");
  // Nothing in the bar pushes past the edge, and there is no sideways scroll.
  expect(await shell.evaluate(() => {
    const out = [];
    for (const c of document.querySelectorAll("#taskbar > *, #tray > *")) {
      const r = c.getBoundingClientRect();
      if (r.right > innerWidth + 0.5 || r.left < -0.5) out.push(c.id || c.className);
    }
    return out;
  })).toEqual([]);
  expect(await shell.evaluate(() => [document.documentElement.scrollWidth, document.documentElement.clientWidth]))
    .toEqual([375, 375]);
});

test("the start menu fits on the screen and everything in it can be reached", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  expect(await inside(shell, "#startmenu", "viewport")).toBe(true);
  // Not one control of the menu is outside it or collapsed to nothing: the
  // search field, the app tiles, both themes, six colours, five backgrounds
  // and the two buttons at the foot.
  expect(await shell.evaluate(() => {
    const m = document.getElementById("startmenu").getBoundingClientRect();
    const bad = [];
    for (const c of document.querySelectorAll("#startmenu button, #startmenu input")) {
      const r = c.getBoundingClientRect();
      if (r.width < 1 || r.height < 1) bad.push(["collapsed", c.className || c.id]);
      else if (r.right > m.right + 1 || r.left < m.left - 1 || r.bottom > m.bottom + 1) {
        bad.push(["outside", c.className || c.id]);
      }
    }
    return bad;
  })).toEqual([]);
  await expect(shell.getByRole("button", { name: "Fenster anordnen" })).toBeVisible();
  await expect(shell.getByRole("button", { name: "Alle schließen" })).toBeVisible();
});

test("a window that asks for more than the screen has is cut to fit", async ({ shell }) => {
  // The App-Store asks for 760 x 520 and the desktop is 375 wide.
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "App-Store", exact: true }).click();
  const win = shell.locator(".win");
  await expect(win).toBeVisible();
  expect(await inside(shell, ".win", "#desktop")).toBe(true);
  // The three buttons of the title bar are what closes a window. All three
  // have to be on the screen, or the window cannot be got rid of.
  await expect(win.locator(".wbtn")).toHaveCount(3);
  expect(await shell.locator(".win .wbtn").evaluateAll((bs) =>
    bs.every((b) => { const r = b.getBoundingClientRect(); return r.right <= innerWidth + 0.5 && r.left >= -0.5; })))
    .toBe(true);
  await win.getByRole("button", { name: "Schließen" }).click();
  await expect(win).toHaveCount(0);
});

test("the notice strip stays on the screen", async ({ shell }) => {
  await shell.evaluate(() => window.__ngcSendNotice(
    "warn", "Ein etwas längerer Hinweis, damit zu sehen ist wie breit der Streifen wird."));
  await expect(shell.getByRole("status")).toBeVisible();
  expect(await inside(shell, ".noticebar", "viewport")).toBe(true);
  await shell.getByRole("button", { name: "Meldung schließen" }).click();
  await expect(shell.locator(".status")).toHaveText("");
});

test("the calculator's twenty keys all fit in the window it opens in", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  // SINCE TASK 25 the keys are in the app's own document, and so is the box they
  // have to fit into: the frame fills the window's body edge to edge, so "inside
  // the frame" and "inside the window" are the same room - which the line below
  // measures, by asking the app's own document for both.
  const keys = shell.frameLocator(".appframe").locator(".calc .keys button");
  await expect(keys).toHaveCount(20);
  // Every key inside the window, and none of them smaller than a fingertip
  // can hit. 24 px is the smallest a touch target may be by WCAG 2.2.
  expect(await keys.evaluateAll((bs) => {
    const w = document.documentElement.getBoundingClientRect();
    return bs.filter((b) => {
      const r = b.getBoundingClientRect();
      return r.right > w.right + 1 || r.bottom > w.bottom + 1 || r.width < 24 || r.height < 24;
    }).length;
  })).toBe(0);
});

test("Fenster anordnen keeps every window inside the desktop", async ({ shell }) => {
  // K3. tile() divided the width and .win{min-width} drew something wider:
  // at 375 px it laid out 187 px tiles, the browser drew 240, and one window
  // stood 52 px past the edge where nothing can reach it.
  //
  // Measured as DRAWN, not as written: getBoundingClientRect is what the
  // person sees, and that is where the two numbers disagreed.
  for (const name of ["Rechner", "App-Store", "Benutzer und Rechte"]) {
    await shell.getByRole("button", { name: "Start" }).click();
    await shell.getByRole("menuitem", { name, exact: true }).click();
  }
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("button", { name: "Fenster anordnen" }).click();
  await shell.waitForTimeout(350);
  const got = await shell.evaluate(() => {
    const d = document.getElementById("desktop").getBoundingClientRect();
    const boxes = [...document.querySelectorAll(".win")].map((w) => {
      const r = w.getBoundingClientRect();
      return { x: r.left - d.left, y: r.top - d.top, w: r.width, h: r.height };
    });
    const outside = boxes.filter((b) =>
      b.x < -1 || b.y < -1 || b.x + b.w > d.width + 1 || b.y + b.h > d.height + 1);
    const overlap = [];
    for (let i = 0; i < boxes.length; i++) {
      for (let j = i + 1; j < boxes.length; j++) {
        const p = boxes[i], q = boxes[j];
        if (p.x < q.x + q.w - 1 && q.x < p.x + p.w - 1 && p.y < q.y + q.h - 1 && q.y < p.y + p.h - 1) {
          overlap.push([i, j]);
        }
      }
    }
    return { count: boxes.length, outside, overlap };
  });
  expect(got).toEqual({ count: 3, outside: [], overlap: [] });
});
