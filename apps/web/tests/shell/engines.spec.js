import { test, expect, shellGap, seedKeyGap } from "../fixtures/page.js";

// THE THREE ENGINES, AND THE ROUTE EACH OF THEM REALLY HAS.
//
// Why this file is its own: shell/boot.spec.js pins the three endings a boot
// may have, and it pins them against the engine it happens to run in. That
// leaves the one case nobody could reach - an engine with the curves, without
// the password seed step, and WITHOUT ANY WAY TO ASK AN AUTHENTICATOR - and
// that is the case the mask used to lie in. Firefox 141 has the first two
// properties and passkeys switched on, so its boot ends in a mask whose one
// enabled button works; turn the pref off and the same mask offers a button
// that cannot work at all.
//
// MEASURED on 18.10.2026, and the reason this file exists:
//
//   chromium 140  curves yes, seed step yes, navigator.credentials.get yes
//   firefox  141  curves yes, seed step NO (exportKey("jwk") OperationError),
//                 navigator.credentials.get yes - and undefined with the one
//                 pref security.webauth.webauthn = false
//   webkit    26  curves NO (NotSupportedError at generateKey), and
//                 window.PublicKeyCredential is an OBJECT while
//                 navigator.credentials is undefined
//
// The last line is why shell/main.js passkeyFits() asks for
// navigator.credentials.get and not for PublicKeyCredential: on WebKit the
// second question answers yes about an engine that cannot be asked for an
// assertion. It is pinned below rather than left in a comment.

// An engine that cannot be asked for an assertion, made out of whichever
// engine is running: the properties the shell reads are taken off the page
// before any of its code runs. This makes the engine LESS able, which is the
// state under test - it switches no check of the shell off.
async function withoutAuthenticator(page) {
  await page.addInitScript(() => {
    try { delete Navigator.prototype.credentials; } catch { /* own property below */ }
    Object.defineProperty(navigator, "credentials", { value: undefined, configurable: true });
    Object.defineProperty(window, "PublicKeyCredential", { value: undefined, configurable: true });
  });
}

// An engine without the password seed step, out of whichever engine is
// running: exportKey("jwk") of an Ed25519 key refuses, which is byte for byte
// what Firefox 141 does (OperationError, measured). Everything else of the
// engine stays as it is - this is the one call that is missing there.
async function withoutSeedStep(page) {
  await page.addInitScript(() => {
    const real = crypto.subtle.exportKey.bind(crypto.subtle);
    crypto.subtle.exportKey = function (format, key) {
      if (format === "jwk" && key?.algorithm?.name === "Ed25519") {
        return Promise.reject(new DOMException("measurement: no jwk of an Ed25519 key", "OperationError"));
      }
      return real(format, key);
    };
  });
}

// The gate itself, and it is the one that was missing. Both routes shut, and
// the person is told BEFORE a mask takes anything - which is the whole point:
// the sentence has to come before the password, not after Argon2id.
test("an engine with no route at all says so before anything is typed", async ({ shell }) => {
  test.skip((await shellGap(shell)) !== "", "this engine does not get past browserFits at all");
  await withoutSeedStep(shell);
  await withoutAuthenticator(shell);
  await shell.reload();

  await expect(shell.locator(".msg")).toContainText(
    "In diesem Browser ist keine Anmeldung möglich");
  // It names what to do about THIS blocker, which is not the one the other
  // sentence names: passkeys are off here, the browser is not too old.
  await expect(shell.locator(".msg")).toContainText("Passkeys");
  // And nothing that takes a secret is on the page.
  await expect(shell.locator("#taskbar")).toHaveCount(0);
  await expect(shell.getByLabel("Passwort")).toHaveCount(0);
  await expect(shell.getByRole("button", { name: "Mit Passkey anmelden" })).toHaveCount(0);
});

// The other half of the same gate: where the engine CAN be asked, the route
// stands. Without this case the gate above could be satisfied by a shell that
// refuses every engine, which is the mistake a guard makes when it is too
// strict - and a skipped way in looks like a finished one.
test("an engine that can be asked for an assertion keeps the passkey route", async ({ shell }) => {
  test.skip((await shellGap(shell)) !== "", "this engine does not get past browserFits at all");
  test.skip(!(await shell.evaluate(() => typeof navigator?.credentials?.get === "function")),
    "this engine has no way to ask an authenticator, so there is no route to keep");
  await withoutSeedStep(shell);
  await shell.reload();

  await expect(shell.getByRole("heading", { name: "Bei NGC anmelden" })).toBeVisible();
  await expect(shell.getByRole("button", { name: "Mit Passkey anmelden" })).toBeEnabled();
  await expect(shell.getByLabel("Passwort")).toBeDisabled();
});

// Where the password route works, a missing authenticator shuts nothing but
// the dead button. This is the case that keeps the new gate from locking
// anybody out: it is the lesson fixtures/page.js paid for twice.
test("without an authenticator the password route still gets in", async ({ shell }) => {
  await test.step("the engine must be able to do a password login at all", async () => {
    test.skip((await seedKeyGap(shell)) !== "", "this engine has no password seed step");
  });
  await withoutAuthenticator(shell);
  await shell.reload();

  await expect(shell.getByRole("heading", { name: "Bei NGC anmelden" })).toBeVisible();
  await expect(shell.getByLabel("Passwort")).toBeEnabled();
  await expect(shell.getByRole("button", { name: "Anmelden", exact: true })).toBeEnabled();
  // The one thing that goes: an offer nothing can serve.
  await expect(shell.getByRole("button", { name: "Mit Passkey anmelden" })).toHaveCount(0);
  await expect(shell.locator(".note.warn")).toHaveCount(0);
});

// THE QUESTION IS ASKED ABOUT THE CALL THE PATH MAKES.
//
// auth/keys.js proveWithPasskey calls navigator.credentials.get, so that is
// what shell/main.js passkeyFits() holds up. Measured on WebKit 26:
// window.PublicKeyCredential is an object there and navigator.credentials is
// undefined - a gate asking the first question would say yes about an engine
// that cannot be asked for an assertion. The engine below is that engine,
// built out of whichever one is running, and the gate has to see through it.
test("a PublicKeyCredential without navigator.credentials is no route", async ({ shell }) => {
  test.skip((await shellGap(shell)) !== "", "this engine does not get past browserFits at all");
  await shell.addInitScript(() => {
    try { delete Navigator.prototype.credentials; } catch { /* own property below */ }
    Object.defineProperty(navigator, "credentials", { value: undefined, configurable: true });
    // Present, and an object - exactly WebKit 26's shape.
    Object.defineProperty(window, "PublicKeyCredential", { value: {}, configurable: true });
  });
  await withoutSeedStep(shell);
  await shell.reload();

  await expect(shell.locator(".msg")).toContainText(
    "In diesem Browser ist keine Anmeldung möglich");
  await expect(shell.getByRole("button", { name: "Mit Passkey anmelden" })).toHaveCount(0);
});

// THE TWO SENTENCES MUST NOT CONTRADICT EACH OTHER.
//
// MEASURED on 18.10.2026 and the second finding of this round: the too-old
// message read "Bitte Chrome 137, Firefox 130 oder Safari 17.4 oder neuer
// benutzen." - and it is WebKit 26 that reads it, because WebKit has no
// Ed25519 at all. Somebody who followed it landed on Firefox 141, which this
// same shell then tells cannot do a password login. One screen recommended
// what the next one refuses.
//
// The browsers that cannot are DATA and not prose (shell/main.js
// NO_PASSWORD_LOGIN), because parsing them out of a sentence that also names
// the remedy cannot be done - the first attempt at this case read "Chrome" out
// of the password sentence, where it stands as the way IN. So: each name on
// that list must be named in the sentence that warns, and in none of the five
// languages in the sentence that advises.
test("no language recommends a browser the shell says cannot log in", async ({ shell }) => {
  const seen = await shell.evaluate(async () => {
    // The list is beside the gate that decides it (shell/main.js), and the
    // languages come from the folder itself: since the layer moved to
    // apps/web/lang/ PHP is the only part that can look into it, so
    // /lang/index.json is what says which languages there are.
    const { NO_PASSWORD_LOGIN } = await import("/assets/js/shell/main.js");
    const { languages: tags } = await (await fetch("/lang/index.json")).json();
    const out = { names: [...NO_PASSWORD_LOGIN], rows: [] };
    for (const tag of tags) {
      const file = await (await fetch(`/lang/${tag}.json`)).json();
      out.rows.push({
        tag,
        // The warning is shell.nopasswordkeys in this layer; main called it
        // boot.noroute.password before the two layers were merged.
        warn: file["shell.nopasswordkeys"],
        advise: file["boot.tooold"],
      });
    }
    return out;
  });

  // The list is this case's subject; an empty one would make it pass by
  // looking at nothing.
  expect(seen.names.length, "the shell names at least one such browser").toBeGreaterThan(0);
  expect(seen.rows.length, "all five languages were read").toBe(5);
  for (const row of seen.rows) {
    expect(typeof row.warn, `${row.tag} holds the warning sentence`).toBe("string");
    expect(typeof row.advise, `${row.tag} holds the advice`).toBe("string");
    // The advice has to name a browser at all, or there is nothing to check
    // and nothing for the person either.
    expect(row.advise, `${row.tag}: the advice names a browser`).toMatch(/Chrome|Safari/);
    for (const name of seen.names) {
      expect(row.warn, `${row.tag}: the warning names ${name} as unable`).toContain(name);
      expect(row.advise, `${row.tag}: ${name} cannot log in and is offered as the remedy`)
        .not.toContain(name);
    }
  }
});
