import { test as base } from "@playwright/test";
import { USER_NAME, PASSWORD } from "./logins.js";

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

// signIn puts anna in. Since task 18 the shell shows the login mask before
// anything else, so every case that wants a desktop starts here - the four
// lines the briefs of tasks 21 to 25 always had, and the line those files
// promised to grow back "when the login lands".
//
// It is ONE function and not four lines in a dozen files, because the mask is
// going to change again: task 19 puts the initial setup in front of it, task
// 20 an alarm behind it. A caption that moves then moves in one place.
//
// It presses the button and does NOT wait for the desktop: what a caller
// waits for is its own business, and the case that has no desktop to wait for
// (shell/boot.spec.js on an engine without the session keys) never gets here.
export async function signIn(page) {
  await page.getByLabel("Benutzername").fill(USER_NAME);
  await page.getByLabel("Passwort").fill(PASSWORD);
  // exact, because "Mit Passkey anmelden" also contains the word: playwright
  // matches an accessible name as a case-insensitive substring.
  await page.getByRole("button", { name: "Anmelden", exact: true }).click();
}

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
