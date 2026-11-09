import { test, expect } from "../fixtures/page.js";

// THE ENGINE OF shell/engines.spec.js, FOR REAL, AND NOT MADE OUT OF ANOTHER.
//
// The cases next door build "an engine that cannot be asked for an
// authenticator" by taking navigator.credentials off the page, so they run in
// every engine. That is the right shape for a property - but it leaves one
// thing unmeasured: whether such an engine EXISTS. This one answers that, with
// one pref and no edit of the page:
//
//   security.webauth.webauthn = false   ->  navigator.credentials undefined,
//                                           window.PublicKeyCredential undefined
//
// MEASURED on 18.10.2026 with Playwright's Firefox 141, and AGAIN on
// 09.11.2026 with Playwright's Firefox 159. It is a pref an administrator or a
// hardened profile really does set, so the engine is one somebody runs.
//
// WHAT CHANGED BETWEEN THE TWO MEASUREMENTS, and why this case now asks
// something else: Firefox 141 had no password seed step either - it could not
// export the public half of an Ed25519 key - so both halves of the dead end
// stood there at once and NGC answered boot.noroute.passkeysoff. Firefox 159
// CAN (measured: shell/main.js passwordLoginFits() returns true under the same
// pref, and crypto.subtle.exportKey("jwk", ...) of an Ed25519 key no longer
// throws OperationError). So the engine with NO route at all does not exist in
// Firefox any more; that property keeps its home next door, where the engine
// is built out of the page.
//
// What stands here is the half that is still real, and it is the half this
// file was written for: an engine that CANNOT ask an authenticator must not be
// shown a passkey button. Round 1 drew that button off the CONFIGURATION
// alone, and pressing it read "Anmeldung fehlgeschlagen." in an engine where
// there was never a way in. The pref makes that engine for real.
//
// Firefox only, because the pref is Firefox's. The other two engines reach the
// same state through the cases next door.
// Top level and not in a describe: launchOptions forces a new worker, and
// Playwright refuses it inside a group.
test.use({ launchOptions: { firefoxUserPrefs: { "security.webauth.webauthn": false } } });

test("has no way to ask an authenticator, and NGC offers none", async ({ shell, browserName }) => {
    test.skip(browserName !== "firefox", "the pref that makes this engine is Firefox's own");
    // The engine first, so a Firefox that grows the export - or one whose pref
    // stopped working - is a changed measurement and not a silent pass.
    expect(await shell.evaluate(() => typeof navigator?.credentials?.get), "the pref took")
      .toBe("undefined");
    expect(await shell.evaluate(() => typeof window.PublicKeyCredential), "and nothing is left to ask with")
      .toBe("undefined");
    // And the OTHER half, read where the shell reads it rather than guessed
    // from a version number: this engine does have the password seed step, so
    // the password route is the one that is open here.
    expect(await shell.evaluate(async () =>
      (await import("/assets/js/shell/main.js")).passwordLoginFits()),
    "this engine can seed a password").toBe(true);

    // So NGC draws the route that exists and not the one it cannot walk: the
    // password mask, no passkey button, no sentence about a dead end - and no
    // desktop, because nobody has logged in yet.
    await expect(shell.getByLabel("Passwort")).toHaveCount(1);
    await expect(shell.getByRole("button", { name: "Mit Passkey anmelden" })).toHaveCount(0);
    await expect(shell.locator(".msg")).toHaveCount(0);
    await expect(shell.locator("#taskbar")).toHaveCount(0);
  });
