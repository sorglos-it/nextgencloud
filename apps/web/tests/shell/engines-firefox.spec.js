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
// MEASURED on 18.10.2026, Playwright's Firefox 141 on Windows. It is a pref an
// administrator or a hardened profile really does set, and Firefox 141 is also
// the engine that has no password seed step - so both halves of the dead end
// stand there at once, in a browser somebody runs.
//
// Firefox only, because the pref is Firefox's. The other two engines reach the
// same state through the cases next door.
// Top level and not in a describe: launchOptions forces a new worker, and
// Playwright refuses it inside a group.
test.use({ launchOptions: { firefoxUserPrefs: { "security.webauth.webauthn": false } } });

test("has no way to ask an authenticator, and NGC says so", async ({ shell, browserName }) => {
    test.skip(browserName !== "firefox", "the pref that makes this engine is Firefox's own");
    // The engine first, so a Firefox that grows the export - or one whose pref
    // stopped working - is a changed measurement and not a silent pass.
    expect(await shell.evaluate(() => typeof navigator?.credentials?.get), "the pref took")
      .toBe("undefined");
    expect(await shell.evaluate(async () => {
      const PK8 = new Uint8Array([0x30,0x2e,0x02,0x01,0x00,0x30,0x05,0x06,0x03,0x2b,0x65,0x70,0x04,0x22,0x04,0x20]);
      const der = new Uint8Array(48); der.set(PK8); der.set(crypto.getRandomValues(new Uint8Array(32)), 16);
      try {
        const k = await crypto.subtle.importKey("pkcs8", der, { name: "Ed25519" }, true, ["sign"]);
        await crypto.subtle.exportKey("jwk", k);
        return "exports";
      } catch (e) { return e.name; } finally { der.fill(0); }
    }), "and this engine still has no seed step").toBe("OperationError");

    // So there is no route, and the sentence stands before anything takes a
    // secret. This is the screen that used to be a mask with one dead button.
    await expect(shell.locator(".msg")).toContainText("In diesem Browser ist keine Anmeldung möglich");
    await expect(shell.getByLabel("Passwort")).toHaveCount(0);
    await expect(shell.getByRole("button", { name: "Mit Passkey anmelden" })).toHaveCount(0);
    await expect(shell.locator("#taskbar")).toHaveCount(0);
  });
