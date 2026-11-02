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

// docs/gestaltung.md point 1: NGC has to be usable on a phone, and the way to
// the apps has to survive under 640 px, where the desktop icons step aside.
// The way Thomas named is the start menu, and this case pins that promise
// instead of leaving it to fall out of another case: every app this user may
// use is reachable on a 375 px screen, by name, and opening one works.
//
// The sheet's own "Heute" read the hidden icons as "no way to the apps on a
// phone". Measured, the start menu was already that way - the Start button is
// in the bar at every width. What was missing was this case saying so, so
// that the next change to the bar cannot take it away unnoticed.
test("every app is reachable on a phone, through the start menu", async ({ shell }) => {
  // The column of app icons is gone since 2026-11-01; what it is replaced by
  // on a narrow screen is the dock, so that is what is asked for here.
  await expect(shell.locator("#dock")).toBeVisible();
  const start = shell.getByRole("button", { name: "Start" });
  await expect(start).toBeVisible();
  // The button is wholly on the screen, not just present in the page.
  expect(await start.evaluate((e) => {
    const r = e.getBoundingClientRect();
    return r.left >= -0.5 && r.right <= innerWidth + 0.5 && r.width > 0;
  })).toBe(true);
  await start.click();
  // None of the apps is lost with the icons: the three the other cases of
  // this file open on a wide screen are all in the menu on a narrow one.
  const inMenu = await shell.locator("#smlist .smitem .n").allTextContents();
  for (const name of ["Rechner", "App-Store", "Benutzer und Rechte"]) {
    expect(inMenu, name + " is in the menu at 375 px").toContain(name);
  }
  // And one of them really opens, which is what "a way to the apps" means.
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  await expect(shell.locator(".win", { hasText: "Rechner" })).toBeVisible();
});

test("the icons step aside and the taskbar keeps its words to itself", async ({ shell }) => {
  // The column of app icons is gone since 2026-11-01; what it is replaced by
  // on a narrow screen is the dock, so that is what is asked for here.
  await expect(shell.locator("#dock")).toBeVisible();
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

// MEASURED on 2026-11-02, and it was a real defect: the standing passkey
// advice sat bottom left at z-index 100001 while the dock sits bottom centre
// at 99998, and at this width the dock is as wide as the room there is - 344
// of 344 px at 360, by the sheet's own arithmetic 16 + 7*40 + 8*6. So the
// advice lay ON the dock and document.elementFromPoint at the centre of an
// icon returned the paragraph. The dock could not be pressed in the middle.
//
// THE CASE DOES NOT ASK WHERE THE ADVICE IS. It asks whether anything at all
// covers the dock, so the next overlay parked at the bottom is caught too -
// naming this one paragraph would guard the bug and not the promise.
test("nothing lies on the dock on a phone", async ({ shell }) => {
  const dock = shell.locator("#dock");
  await expect(dock).toBeVisible();
  const n = await shell.locator("#dock .dapp").count();
  expect(n).toBeGreaterThan(0);

  const covered = await shell.evaluate(() =>
    [...document.querySelectorAll("#dock .dapp")].map((d, i) => {
      const r = d.getBoundingClientRect();
      const top = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
      return top && top.closest(".dapp") === d ? null : `${i}:${top ? top.className || top.tagName : "nichts"}`;
    }).filter(Boolean));
  expect(covered).toEqual([]);

  // And the advice itself is still there to be read - moving it out of the way
  // must not have been done by hiding it.
  await expect(shell.locator(".passkeyhint")).toBeVisible();
  expect(await inside(shell, ".passkeyhint", "viewport")).toBe(true);

  // IT TAKES NO CLICKS. Asked at its OWN centre, the shell answers with what
  // lies behind it, never with the paragraph: it is text and nothing else, so
  // there is nothing in it to press, and a box that swallows the pointer is a
  // trap wherever it is parked. MEASURED the hard way - with the corner moved
  // but the clicks still taken, a double click on a title bar timed out
  // thirty seconds on "passkeyhint intercepts pointer events".
  const unterDemHinweis = await shell.evaluate(() => {
    const r = document.querySelector(".passkeyhint").getBoundingClientRect();
    const e = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    return e ? !!e.closest(".passkeyhint") : false;
  });
  expect(unterDemHinweis).toBe(false);
});

// THE MAGNIFIER MUST NOT MAKE THE ROW LONGER. 360 and not this file's 375,
// because 360 is where the arithmetic of the sheet runs out: 16 + 7*40 + 8*6
// = 344 is the whole dock, the row is full by construction, and a magnifier
// that grows the button pushed the last icon off the screen (measured: the
// row asked for 360 px of the 342 it has, and the last icon ended at 369).
//
// The case does not ask HOW the magnifier is built. It asks that the row need
// no more room than before and that every icon still take its own click -
// asking for a transform would guard today's repair instead of the promise.
test.describe("das Dock bei 360", () => {
  test.use({ viewport: { width: 360, height: 700 } });

  test("the magnifier asks the row for no more room", async ({ shell }) => {
    const dock = shell.locator("#dock");
    await expect(dock).toBeVisible();
    // Everything that would show as a longer row: what the row needs against
    // what it has, the dock's own box, and the right edge of the last icon.
    const zeile = () => shell.evaluate(() => {
      const d = document.querySelector("#dock");
      const icons = [...d.querySelectorAll(".dapp")];
      const last = icons[icons.length - 1].getBoundingClientRect();
      const r = d.getBoundingClientRect();
      return {
        braucht: d.scrollWidth, hat: d.clientWidth,
        dock: [Math.round(r.left), Math.round(r.width)],
        letztes: Math.round(last.right),
        // What answers at the middle of each icon - the only measurement that
        // says whether a press would arrive.
        fremd: icons.map((a, i) => {
          const b = a.getBoundingClientRect();
          const top = document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2);
          return top && top.closest(".dapp") === a ? null : `${i}:${top ? top.className || top.tagName : "nichts"}`;
        }).filter(Boolean),
      };
    });

    const ruhe = await zeile();
    expect(ruhe.fremd).toEqual([]);
    expect(ruhe.braucht).toBeLessThanOrEqual(ruhe.hat);
    expect(ruhe.letztes).toBeLessThanOrEqual(360);

    await dock.locator(".dapp").first().hover();
    // The magnifier is a transition: waited for at the icon that grows, so
    // the row is read once the growth has happened and not before it.
    await expect.poll(async () => {
      const g = await dock.locator(".dapp").first().locator(".g").boundingBox();
      return Math.round(g.width);
    }).toBeGreaterThan(40);

    const lupe = await zeile();
    expect(lupe.fremd).toEqual([]);
    expect(lupe.braucht).toBeLessThanOrEqual(lupe.hat);
    expect(lupe.letztes).toBeLessThanOrEqual(360);
    expect(lupe.dock).toEqual(ruhe.dock);
  });
});
