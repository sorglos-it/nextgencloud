import { test as base } from "@playwright/test";

// The shell page for ordinary tests. The content security policy stays on, so
// a test notices when the page starts breaking it.
export const test = base.extend({
  shell: async ({ page }, use) => {
    await page.goto("/");
    await use(page);
  },
});

// Only the contract tests need this one: they import the modules of the shell
// page, and Playwright injects its own script for that. Switching the policy
// off is therefore its own export, never the default — otherwise no later
// test could ever see a violation again. A separate test checks the policy
// itself (Task 26).
export const testWithoutCSP = test.extend({
  context: async ({ browser }, use) => {
    const ctx = await browser.newContext({ bypassCSP: true });
    await use(ctx);
    await ctx.close();
  },
});

export { expect } from "@playwright/test";
