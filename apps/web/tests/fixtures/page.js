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

// Can this engine make the two keys a session needs? shell/main.js asks the
// same question with the same two generateKey calls, and a "no" stops the
// boot before the desktop is built - so a test of the desktop has nothing to
// look at on such an engine.
//
// Measured, never assumed from the engine's name: chromium says yes to both;
// the WebKit that Playwright brings answers NotSupportedError to both, the way
// it already does for the contract tests (noise.spec.js has the same probe for
// X25519 alone). Real Safari from 17.4 has them, which is why the shell may
// ask for them at all. The day Playwright's WebKit gains them, this answers
// true by itself and the cases below run there with no edit here.
export async function hasSessionKeys(shell) {
  return shell.evaluate(async () => {
    for (const [name, usages] of [["Ed25519", ["sign", "verify"]], ["X25519", ["deriveBits"]]]) {
      try {
        if (!(await crypto.subtle.generateKey({ name }, false, usages))) return false;
      } catch {
        return false;
      }
    }
    return true;
  });
}
