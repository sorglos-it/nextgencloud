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
//
// standIn: false is how a case asks for the real way instead - see standInOn
// below; tests/shell/world.spec.js is the one case that does.
export async function signIn(page, { standIn = true } = {}) {
  if (standIn) await standInOn(page);
  await page.getByLabel("Benutzername").fill(USER_NAME);
  await page.getByLabel("Passwort").fill(PASSWORD);
  // exact, because "Mit Passkey anmelden" also contains the word: playwright
  // matches an accessible name as a case-insensitive substring.
  await page.getByRole("button", { name: "Anmelden", exact: true }).click();
}

// standInOn hands this page shell/fake-link.js instead of the real session.
//
// WHICH CASES STILL EAT THE STAND-IN, in one place and by name. Since task 17b
// shell/main.js takes the REAL session: net/world.js puts the surfaces'
// call(app, service, name, …) on net/link.js and opens the trust list on it.
// The stand-in answers two dozen actions where the mock API answers five, and
// none of them with the signed user interface the app frame needs - so every
// case that wants a desktop with apps, a store, a user administration or a
// running app still needs it and asks for it through here: signIn() for every
// case that goes through signIn, and shell/login.spec.js for its own cases,
// which type into the mask themselves.
//
// BOTH WAYS OF SETTING IT, and each one is needed. addInitScript covers every
// navigation that follows - the passkey cases move the page to its DNS name,
// and a flag set on the old document would be gone there. evaluate covers the
// document that is already standing, which is where signIn finds it; the hook
// is read when the login is through, so that is in time.
//
// IT LEAVES WITH shell/fake-link.js: when the mock answers every action of
// core.ACTIONS with a signed world, this function and that file go together.
//
// HOW TO SEE THAT IT MAY GO, countable and not a matter of judgement:
//
//   grep -rn standInOn apps/web/tests --include=*.js
//
// is over when the only hits left are this definition and the comments around
// it - no signIn() without `standIn: false`, no beforeEach calling it. Then
// this function, the `standIn` argument above, standIn() in
// shell/main.js and shell/fake-link.js go in one commit.
export async function standInOn(page) {
  await page.addInitScript(() => { window.__ngcStandIn = true; });
  try {
    await page.evaluate(() => { window.__ngcStandIn = true; });
  } catch {
    // No document on this page yet; the init script above is what sets it.
  }
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
