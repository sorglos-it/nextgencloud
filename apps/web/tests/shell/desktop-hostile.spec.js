import { test, expect } from "@playwright/test";
import { signIn, skipWithoutLogin } from "../fixtures/page.js";
import { app, CALC, serveApps } from "../fixtures/apps.js";

// What the desktop does with an app list that is out to break it.
//
// The list comes from the API, and a compromised API is exactly the case
// these checks exist for. The contract (core.parseResult) is what refuses
// most of it; this file measures the SURFACE: what the person sees, that
// nothing runs, and that the desktop is still a desktop afterwards.
//
// The page fixture is not used here: every case needs its own list, and the
// list has to be in place before the page is loaded.
//
// A DIALOG IS A FAILURE. alert, confirm and prompt are what a name that got
// executed would reach for, so every case fails on one instead of clicking
// it away.
async function desktop(page, apps) {
  page.on("dialog", (d) => {
    throw new Error("something in the page opened a " + d.type() + ": " + d.message());
  });
  await serveApps(page, apps);
  await page.goto("/");
  await skipWithoutLogin(page);
  await signIn(page);
  await expect(page.locator("#taskbar")).toBeVisible();
}

// stillADesktop is the second half of every case: a refusal that leaves the
// shell unusable is no better than the attack.
async function stillADesktop(page) {
  await page.getByRole("button", { name: "Start" }).click();
  await expect(page.locator("#startmenu")).toHaveClass(/open/);
  await expect(page.locator("#smsearch")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator("#startmenu")).not.toHaveClass(/open/);
  // The theme button as a sign of life: one press has to reach the document.
  // The fixture's machine is set to light (playwright.config.js) and nothing
  // has been chosen here, so the shell is following the machine and the press
  // goes to the other design.
  await page.getByRole("button", { name: "Design wechseln" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
}

// The contract caps a name at 64 bytes of printable UTF-8 and refuses a list
// that has the same id twice. A list with one bad app in it is a bad list,
// all of it: there is no way to tell which of the others were tampered with
// too. So the desktop shows none of them and says the one thing the person
// can act on.
const REFUSED = "Die Liste der Apps konnte nicht geladen werden.";

test("a name of 10 000 characters is refused, and nothing of it reaches the screen", async ({ page }) => {
  await desktop(page, [app({ id: "de.example.lang", name: "A".repeat(10000) }), CALC]);
  await expect(page.getByRole("status")).toHaveText(REFUSED);
  await expect(page.locator("#dock .dapp")).toHaveCount(0);
  // Not one character of the name is anywhere in the page.
  expect(await page.evaluate(() => document.body.textContent.includes("AAAAAAAAAA"))).toBe(false);
  await stillADesktop(page);
});

test("a name of nothing but control characters is refused", async ({ page }) => {
  await desktop(page, [app({ id: "de.example.steuer", name: "\u0001\u0002\u0007\u001b\u007f" }), CALC]);
  await expect(page.getByRole("status")).toHaveText(REFUSED);
  await expect(page.locator("#dock .dapp")).toHaveCount(0);
  await stillADesktop(page);
});

test("one control character inside an otherwise sound name is refused", async ({ page }) => {
  await desktop(page, [app({ id: "de.example.null", name: "Vor\u0000Nach" }), CALC]);
  await expect(page.getByRole("status")).toHaveText(REFUSED);
  await expect(page.locator("#dock .dapp")).toHaveCount(0);
  expect(await page.evaluate(() => document.body.textContent.includes("Nach"))).toBe(false);
  await stillADesktop(page);
});

test("a name with a bidi override in it is refused", async ({ page }) => {
  // U+202E turns the reading order round: "Rechnerexe.txt" could be made to
  // read as something else entirely. It is a format character, not a
  // printable one, so the contract does not let it in.
  await desktop(page, [app({ id: "de.example.bidi", name: "Rechner‮gpj.exe" }), CALC]);
  await expect(page.getByRole("status")).toHaveText(REFUSED);
  await expect(page.locator("#dock .dapp")).toHaveCount(0);
  await stillADesktop(page);
});

test("two apps with the same id are refused", async ({ page }) => {
  await desktop(page, [
    app({ id: "de.example.doppelt", name: "Eins" }),
    app({ id: "de.example.doppelt", name: "Zwei" }),
    CALC,
  ]);
  await expect(page.getByRole("status")).toHaveText(REFUSED);
  await expect(page.locator("#dock .dapp")).toHaveCount(0);
  await stillADesktop(page);
});

test("one app over the contract's thousand is refused", async ({ page }) => {
  await desktop(page, Array.from({ length: 1001 }, (_, i) => app({ id: "de.example.a" + i, name: "App " + i })));
  await expect(page.getByRole("status")).toHaveText(REFUSED);
  await expect(page.locator("#dock .dapp")).toHaveCount(0);
  await stillADesktop(page);
});

test("a thousand apps are shown, and the search is the way through them", async ({ page }) => {
  await desktop(page, Array.from({ length: 1000 }, (_, i) => app({ id: "de.example.a" + i, name: "App " + i })));
  // .status, because what is asserted here is that it holds nothing. The
  // strip stays in the page whether it has news or not - that was W1 of the
  // fixing round - so getByRole("status") would find it too; asking the
  // element directly says "empty" without going through a role to do it.
  await expect(page.locator(".status")).toHaveText("");
  await expect(page.locator("#dock .dapp")).toHaveCount(1000);
  await page.getByRole("button", { name: "Start" }).click();
  await expect(page.locator("#smlist .smitem")).toHaveCount(1000);
  // The list scrolls inside the menu instead of pushing the menu off screen,
  // and the two buttons under it stay where they can be clicked.
  const fits = await page.evaluate(() => {
    const m = document.getElementById("startmenu").getBoundingClientRect();
    const l = document.getElementById("smlist").getBoundingClientRect();
    const f = document.querySelector("#smfoot button").getBoundingClientRect();
    return {
      menuInView: m.top >= 0 && m.bottom <= innerHeight + 1 && m.left >= 0 && m.right <= innerWidth + 1,
      listInMenu: l.bottom <= m.bottom + 1,
      listScrolls: document.getElementById("smlist").scrollHeight > document.getElementById("smlist").clientHeight,
      footInMenu: f.bottom <= m.bottom + 1 && f.width > 0,
    };
  });
  expect(fits).toEqual({ menuInView: true, listInMenu: true, listScrolls: true, footInMenu: true });
  await page.locator("#smsearch").fill("App 999");
  expect(await page.locator("#smlist .smitem .n").allTextContents()).toEqual(["App 999"]);
});

test("a name from right to left is shown as the text it is", async ({ page }) => {
  const arabic = "مرحبا بالعالم";
  await desktop(page, [app({ id: "de.example.rtl", name: arabic }), CALC]);
  await expect(page.locator("#dock .dapp")).toHaveCount(2);
  // Character for character, in the dock, in the menu, in the title bar and in
  // the taskbar. A name that arrives from the right must not come out
  // shortened, reordered or with something stuck to it.
  //
  // THE DOCK CARRIES NO NAMES, only icons, so the name is read off the LABEL -
  // which is the stricter read of the two: a label is an attribute, and markup
  // in an attribute can never become an element.
  expect(await page.locator("#dock .dapp").first().getAttribute("aria-label")).toBe(arabic);
  await page.locator("#dock").getByRole("button", { name: arabic, exact: true }).click();
  await expect(page.locator(".win")).toHaveCount(1);
  expect(await page.locator(".win .t").textContent()).toBe(arabic);
  expect(await page.locator("#tasks .task .lbl").textContent()).toBe(arabic);
  await expect(page.locator("#tasks .task")).toHaveAttribute("aria-label", "Fenster " + arabic);
  await page.getByRole("button", { name: "Start" }).click();
  expect(await page.locator("#smlist .smitem .n").first().textContent()).toBe(arabic);
  await page.keyboard.press("Escape");
  await stillADesktop(page);
});

test("names that try to be markup become text, and nothing of them runs", async ({ page }) => {
  const bad = [
    "<script>alert(1)</script>",
    '"><svg onload=alert(2)>',
    "</div><img src=x onerror=alert(3)>",
    "<iframe src=javascript:alert(4)>",
  ];
  await desktop(page, [...bad.map((name, i) => app({ id: "de.example.b" + i, name })), CALC]);
  await expect(page.locator("#dock .dapp")).toHaveCount(5);
  expect(await page.locator("#dock .dapp").evaluateAll(
    (bs) => bs.map((b) => b.getAttribute("aria-label")))).toEqual([...bad, "Rechner"]);
  await page.getByRole("button", { name: "Start" }).click();
  expect(await page.locator("#smlist .smitem .n").allTextContents()).toEqual([...bad, "Rechner"]);
  await page.keyboard.press("Escape");
  // Open one of them: the title bar and the taskbar get the same treatment.
  await page.locator("#dock").getByRole("button", { name: bad[0], exact: true }).click();
  expect(await page.locator(".win .t").textContent()).toBe(bad[0]);
  // Nowhere a name is shown may an element out of that name have appeared.
  //
  // Since the badges carry icons (js/shell/icon.js) there ARE svg elements in
  // these three places, so the case asks the sharper question instead of the
  // easy one: every svg here is one the shell built itself, and it holds
  // nothing but shape. A name that became an <svg onload=…> would be an svg
  // without the class, or one with an attribute out of that name - both of
  // them counted below.
  const made = await page.evaluate(() => {
    const where = ["#desktop", "#startmenu", "#taskbar"];
    const out = { foreign: 0, svg: 0, attrs: [] };
    for (const w of where) {
      const root = document.querySelector(w);
      if (!root) continue;
      out.foreign += root.querySelectorAll("img, script, iframe, object, embed").length;
      for (const s of root.querySelectorAll("svg")) {
        if (!s.classList.contains("ico")) { out.svg++; continue; }
        for (const e of [s, ...s.querySelectorAll("*")]) {
          for (const a of e.getAttributeNames()) {
            if (a.startsWith("on") || a === "href" || a === "xlink:href") out.attrs.push(a);
          }
        }
      }
    }
    return out;
  });
  expect(made).toEqual({ foreign: 0, svg: 0, attrs: [] });
  await stillADesktop(page);
});

test("a notice that tries to be markup is shown as text", async ({ page }) => {
  await desktop(page, [CALC]);
  await page.evaluate(() => window.__ngcSendNotice("warn", "<img src=x onerror=alert(9)>"));
  await expect(page.getByRole("status")).toHaveText("<img src=x onerror=alert(9)>");
  expect(await page.locator(".noticebar img, .noticebar script, .noticebar svg").count()).toBe(0);
});

test("a notice the contract does not hold with is dropped without a word", async ({ page }) => {
  await desktop(page, [CALC]);
  await page.evaluate(() => window.__ngcSendNotice("warn", "Die eine Meldung."));
  await expect(page.getByRole("status")).toHaveText("Die eine Meldung.");
  // Too long for the wire, and an invented level: both are bodies the shell
  // cannot vouch for, and a message it cannot vouch for is worse than none.
  await page.evaluate(() => window.__ngcSendNotice("warn", "Z".repeat(5000)));
  await page.evaluate(() => window.__ngcSendNotice("erfunden", "Eine erfundene Stufe."));
  await expect(page.getByRole("status")).toHaveText("Die eine Meldung.");
  await stillADesktop(page);
});

test("an empty list of apps leaves a desktop that still works", async ({ page }) => {
  await desktop(page, []);
  await expect(page.locator("#dock .dapp")).toHaveCount(0);
  await expect(page.locator(".status")).toHaveText("");
  // docs/gestaltung.md point 3: one sentence, and it does not read as a
  // fault. Neither of the shell's two words for a defect is in it, and the
  // decision book has that measured for exactly this screen.
  await expect(page.locator("#deskempty")).toBeVisible();
  await expect(page.locator("#deskempty .emptymain")).toHaveText("Hier ist Platz für Ihre Apps.");
  const said = await page.locator("#deskempty").textContent();
  expect(said).not.toContain("nicht in Ordnung");
  expect(said).not.toContain("antwortet gerade nicht");
  // The start menu says the same thing, and NOT "Keine Treffer": nothing was
  // searched for.
  await page.getByRole("button", { name: "Start" }).click();
  await expect(page.locator(".smempty")).toHaveText("Hier ist Platz für Ihre Apps.");
  await page.keyboard.press("Escape");
  await stillADesktop(page);
});

test("a list that could not be fetched is not an empty desktop", async ({ page }) => {
  // THE CASE THE SENTENCE MUST NOT COVER. A refused list is a request that
  // failed, and "here is room for your apps" over it would be telling the
  // person something that was never measured: the strip says what happened
  // and the desktop stays bare.
  await desktop(page, [app({ id: "de.example.lang", name: "A".repeat(10000) }), CALC]);
  await expect(page.getByRole("status")).toHaveText(REFUSED);
  await expect(page.locator("#dock .dapp")).toHaveCount(0);
  await expect(page.locator("#deskempty")).toBeHidden();
  await page.getByRole("button", { name: "Start" }).click();
  await expect(page.locator(".smempty")).toHaveCount(0);
});
