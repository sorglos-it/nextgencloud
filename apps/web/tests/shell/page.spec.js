import { readFileSync } from "node:fs";
import { test, expect } from "@playwright/test";

// The anchor is NOT written out a second time here. It stood in this file as
// the placeholder q6urq6ur... (0xAB repeated) until this round, while commit
// e3b4cffb had long since put the anchor derived from the seeds into
// trust-anchor.json - the same drift PR #164 found in the PHP check
// (apps/web/tests/php/config-test.php:297). This case has never run in CI, so
// nobody saw it.
//
// Reading the file is the right fix and not the lazy one: what this case is
// for is that the server HANDS OUT what the file holds. The guard against a
// WRONG anchor in the file is contracts/anchor.spec.js, which derives it a
// second time from the seeds and holds it against the file. One number, one
// place.
const ANCHOR = JSON.parse(
  readFileSync(new URL("../trust-anchor.json", import.meta.url), "utf8"),
);

test("the shell page carries the headers and the anchor", async ({ page }) => {
  const res = await page.goto("/");
  expect(res.status()).toBe(200);
  const h = res.headers();
  expect(h["content-security-policy"]).toContain("default-src 'none'");
  expect(h["content-security-policy"]).toContain("frame-ancestors 'none'");
  expect(h["cache-control"]).toBe("no-store");
  expect(h["x-content-type-options"]).toBe("nosniff");
  expect(h["referrer-policy"]).toBe("no-referrer");
  expect(await page.title()).toBe("NGC");
  await expect(page.locator("#root")).toBeAttached();

  const cfg = await (await page.request.get("/config.json")).json();
  expect(cfg.anchor).toEqual(ANCHOR);
  expect(cfg.rp.id).toBe("ngc.example.org");
  expect(cfg.apis).toEqual(["https://api-1.ngc.test:7443", "https://api-2.ngc.test:7443"]);
});

test("an unknown path answers 404 in German", async ({ page }) => {
  const res = await page.goto("/gibtesnicht");
  expect(res.status()).toBe(404);
  await expect(page.locator("body")).toContainText("Seite nicht gefunden");
});
