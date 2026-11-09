// A trust list that GROWS, and the login that has to follow it.
//
// WHAT THIS FILE IS FOR. Measured at the six-VM plant on 30.10.2026: the first
// login of every pair failed with "Anmeldung fehlgeschlagen." and the second,
// with the same password, went through. The cause was that the mask built its
// trust list through a stand-in that took chain.versions[0] -- the OLDEST
// version of a list that grows. Version 1 of that plant carries api-1 alone;
// api-2 arrives with version 2. A certificate api-2 signed therefore found no
// issuer, session.verifyAPI threw, and the mask drew its one sentence.
//
// The appearance was the cheap half. The expensive half is that a list from
// version 1 also overlooks whatever a later version REVOKED: in a plant where
// a node is ever removed, that same mask goes on accepting its certificate.
// That is a security fault, and it is the reason the stand-in was removed
// rather than wired up at its one caller.
//
// Until this round the mock world served ONE version, so the property this is
// all about -- a list that grows -- was not present in the test world at all.
// A mock without growth cannot show a fault that comes from growth.
//
// WHY skipWithoutSeedKeys AND NOT A LOCAL hasEd25519: these cases log in with
// a password, so they need the seed step (proof.publicFromSeed). The loose
// local guard says YES in firefox, where the seed step is exactly what is
// missing, and a case behind it goes red as if the code were broken. Measured
// by the strand "Anmeldung in zwei von drei Browsern ungeprüft".
import { test, expect, skipWithoutLogin, skipWithoutSeedKeys } from "../fixtures/page.js";
import { USER_NAME, PASSWORD } from "../mock/world.js";

// answerAs has the mock answer as one of the two API nodes. api-2 is the one
// that arrives with version 2 of the list.
async function answerAs(shell, node) {
  const ok = await shell.evaluate(async (n) => {
    const r = await fetch(`/api/v1/mock?node=${n}`, { method: "POST" });
    return r.ok;
  }, node);
  expect(ok, "the mock's test door did not answer").toBe(true);
}

test.describe("a node that only exists from version 2", () => {
  test.afterEach(async ({ shell }) => {
    // Put the default back, so a later file does not inherit api-2: the rest
    // of the suite answers as api-1 on purpose, so that a fault in the chain
    // stays distinguishable from a fault in a window.
    await answerAs(shell, "api-1");
  });

  test("the FIRST login against it succeeds", async ({ shell }) => {
    await skipWithoutLogin(shell);
    await skipWithoutSeedKeys(shell);
    await answerAs(shell, "api-2");

    await shell.getByLabel("Benutzername").fill(USER_NAME);
    await shell.getByLabel("Passwort").fill(PASSWORD);
    // exact, because "Mit Passkey anmelden" carries the word too.
    await shell.getByRole("button", { name: "Anmelden", exact: true }).click();

    // ONE assertion and it waits by itself: the desktop appears. Deliberately
    // not a wait on the mask's buttons -- in firefox "Anmelden" is disabled
    // for good, so a condition over buttons is already true before the click
    // and would read a half-finished attempt. A waiting condition asks for a
    // CHANGE, not for a state.
    await expect(shell.locator("#taskbar"),
      "the first login against a node from version 2 did not reach the desktop")
      .toBeVisible();

    // And the sentence of a failed login must NOT be on the screen. Without
    // this line the case would also pass on a shell that shows both.
    await expect(shell.getByText("Anmeldung fehlgeschlagen.")).toHaveCount(0);
  });

  test("the login against the node of version 1 still succeeds", async ({ shell }) => {
    await skipWithoutLogin(shell);
    await skipWithoutSeedKeys(shell);
    await answerAs(shell, "api-1");

    // THE LIVING COUNTER-PROBE. Without it the ordinary case would have been
    // traded for the special one: a shell that followed the chain but broke
    // version 1 would pass the case above and fail every real installation
    // that has only one node. If this case dies, the repair went too far.
    await shell.getByLabel("Benutzername").fill(USER_NAME);
    await shell.getByLabel("Passwort").fill(PASSWORD);
    // exact, because "Mit Passkey anmelden" carries the word too.
    await shell.getByRole("button", { name: "Anmelden", exact: true }).click();
    await expect(shell.locator("#taskbar"),
      "the login against api-1 stopped working").toBeVisible();
  });
});
