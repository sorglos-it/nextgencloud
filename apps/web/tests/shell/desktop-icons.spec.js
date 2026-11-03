import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { test, expect, signIn, skipWithoutLogin } from "../fixtures/page.js";

// The badges on the desktop, in the start menu, in the title bar and in the
// taskbar.
//
// What is held here is the one rule of js/shell/icon.js: a badge draws a shape
// of the SHELL, built with the DOM, never a picture out of an app's package
// and never a file this origin serves. The cases ask it from the outside -
// what the page really built, not what the code meant.
const here = dirname(fileURLToPath(import.meta.url));
const ICONS = join(here, "..", "..", "..", "..", "docs", "icons");

test.beforeEach(async ({ shell }) => {
  await skipWithoutLogin(shell);
  await signIn(shell);
  await expect(shell.locator("#taskbar")).toBeVisible();
});

test("every badge in the dock carries an icon that was really drawn", async ({ shell }) => {
  const seen = await shell.evaluate(() => [...document.querySelectorAll("#dock .dapp .g")]
    .map((g) => {
      const s = g.querySelector("svg.ico");
      if (!s) return "(no icon)";
      // A box of 0 is a badge that draws nothing - which is what the taskbar
      // did on 24.10.2026 before .task .tico had a size of its own.
      const r = s.getBoundingClientRect();
      return s.children.length > 0 && r.width > 8 && r.height > 8 ? "ok" : "empty";
    }));
  expect(seen.length).toBeGreaterThan(0);
  expect([...new Set(seen)]).toEqual(["ok"]);
});

test("nothing in a badge is a picture fetched from anywhere", async ({ shell }) => {
  // No <img>, no <use>, no url() - an icon that came off a server is an icon
  // somebody else could swap.
  const bad = await shell.evaluate(() => {
    const out = [];
    for (const s of document.querySelectorAll("svg.ico")) {
      if (s.querySelector("image, use, script, foreignObject")) out.push("element");
      if (s.innerHTML.includes("href") || s.innerHTML.includes("url(")) out.push("reference");
    }
    return out;
  });
  expect(bad).toEqual([]);
});

test("the app the shell knows gets its own icon, the unknown one the placeholder", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const m = await import("/assets/js/shell/icon.js");
    return [
      m.iconName({ id: "org.sorglos.calculator", name: "Rechner", state: "ok" }),
      m.iconName({ id: "org.sorglos.terminal", name: "Terminal", state: "ok" }),
      m.iconName({ id: "org.fremd.unbekannt", name: "Fremd", state: "ok" }),
      // An id out of the API must not reach through to Object's prototype.
      m.iconName({ id: "constructor", name: "B\u00f6se", state: "ok" }),
      m.iconName({ id: "org.sorglos.terminal", name: "Terminal", state: "unreachable" }),
      m.iconName({ id: "org.sorglos.terminal", name: "Terminal", state: "unlicensed" }),
    ];
  });
  expect(got).toEqual(["rechner", "terminal", "platzhalter", "platzhalter",
    "nicht-erreichbar", "keine-lizenz"]);
});

test("a window and its taskbar entry carry the app's icon", async ({ shell }) => {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Rechner", exact: true }).click();
  const sizes = await shell.evaluate(() =>
    [".win .titlebar .g svg.ico", ".task .tico svg.ico"].map((s) => {
      const e = document.querySelector(s);
      if (!e) return "(missing)";
      const r = e.getBoundingClientRect();
      return r.width > 8 && r.height > 8 ? "ok" : "empty " + Math.round(r.width);
    }));
  expect(sizes).toEqual(["ok", "ok"]);
});

test("the badge is not read out with the name", async ({ shell }) => {
  expect(await shell.evaluate(() => [...document.querySelectorAll(".dapp .g, .smitem .g, .win .g")]
    .every((g) => g.getAttribute("aria-hidden") === "true"))).toBe(true);
  await shell.getByRole("button", { name: "Start" }).click();
  await expect(shell.getByRole("menuitem", { name: "Rechner", exact: true })).toHaveCount(1);
});

test("docs/icons and the shell's table say the same thing", async ({ shell }) => {
  // TWO COPIES, SO THEY ARE MEASURED. docs/icons/*.svg is what an app builder
  // copies into their own ui/ and what docs/icons/uebersicht.html shows; the
  // shapes themselves live in shell/icon.js and the files are written out of
  // it (apps/web/tests/icons-build.mjs). This is the case that notices when
  // somebody edits one side and forgets the other.
  const { names, SHAPES } = await import("../../public/assets/js/shell/icon.js");
  const { svgText } = await import("../icons-build.mjs");
  const onDisk = readdirSync(ICONS).filter((f) => f.endsWith(".svg")).sort();
  expect(onDisk).toEqual(names().map((n) => n + ".svg").sort());
  const apart = names().filter((n) =>
    readFileSync(join(ICONS, n + ".svg"), "utf8").replace(/\r\n/g, "\n") !== svgText(n));
  expect(apart, "run: node apps/web/tests/icons-build.mjs").toEqual([]);
  expect(Object.keys(SHAPES).length).toBe(12);
});

test("the icon files hold the rule they are drawn by", async () => {
  // An SVG is a document and can carry a script, a reference to another server
  // or a font. These are ours and stay that way - and an app builder who
  // copies one into their package is copying something that holds the rule.
  const bad = [];
  for (const f of readdirSync(ICONS).filter((f) => f.endsWith(".svg"))) {
    const body = readFileSync(join(ICONS, f), "utf8");
    if (body.length > 4096) bad.push(f + ": " + body.length + " bytes");
    for (const forbidden of ["<script", "<foreignObject", "<image", "<use", "href", "on="]) {
      if (body.includes(forbidden)) bad.push(f + ": " + forbidden);
    }
    if (!body.includes('stroke="currentColor"')) bad.push(f + ": no currentColor");
    if (!body.includes('viewBox="0 0 24 24"')) bad.push(f + ": wrong canvas");
  }
  expect(bad).toEqual([]);
});
