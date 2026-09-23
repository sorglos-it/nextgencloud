import { test as base } from "@playwright/test";

// The contract tests import the modules on the shell page. They run with the
// content security policy switched off, because Playwright injects its own
// script; a separate test checks the policy itself (Task 26).
export const test = base.extend({
  context: async ({ browser }, use) => {
    const ctx = await browser.newContext({ bypassCSP: true });
    await use(ctx);
    await ctx.close();
  },
  shell: async ({ page }, use) => {
    await page.goto("/");
    await use(page);
  },
});

export { expect } from "@playwright/test";
