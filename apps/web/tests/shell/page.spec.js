import { test, expect } from "@playwright/test";

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
  expect(cfg.anchor).toEqual({ seq: 1, hash: "q6urq6urq6urq6urq6urq6urq6urq6urq6urq6urq6s" });
  expect(cfg.rp.id).toBe("ngc.example.org");
  expect(cfg.apis).toEqual(["https://api-1.ngc.test:7443", "https://api-2.ngc.test:7443"]);
});

test("an unknown path answers 404 in German", async ({ page }) => {
  const res = await page.goto("/gibtesnicht");
  expect(res.status()).toBe(404);
  await expect(page.locator("body")).toContainText("Seite nicht gefunden");
});
