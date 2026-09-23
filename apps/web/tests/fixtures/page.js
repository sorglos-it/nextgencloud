import { test as base } from "@playwright/test";

// The shell page for every test. The content security policy stays on, so a
// test notices when the page starts breaking it. The contract tests import
// the modules of the page inside page.evaluate, which Playwright runs over
// the protocol, and the dynamic import of /assets/... is script-src 'self'
// and allowed - nothing here needs the policy switched off. A separate test
// checks the policy itself (Task 26).
export const test = base.extend({
  shell: async ({ page }, use) => {
    await page.goto("/");
    await use(page);
  },
});

export { expect } from "@playwright/test";
