import {
  test, expect, shellGap, loginGap, passkeyAskGap, hasSessionKeys, signIn, expectDesktop
} from "../fixtures/page.js";

// What the boot of the shell has to end in, on every engine there is.
//
// It lives in its own file because desktop.spec.js and calc.spec.js skip
// themselves where the engine has no session keys, and a beforeEach skips the
// whole file with them. This case is the one that must run exactly there: it
// is what keeps that skip from being a hole. A blank page is never a legal
// ending, and neither is a mask that takes a password it cannot use.
//
// THERE ARE FOUR LEGAL ENDINGS, and this case has learned one of them twice
// the same way: by being red while the shell was doing nothing worse than
// coming up - on Firefox (ending 3, 17.10.2026) and on Playwright's WebKit on
// Linux (ending 4, run 42215156995):
//
//   1. the desktop - the engine can finish a password login;
//   2. the boot.tooold message - not even the two curves (Playwright's
//      WebKit, measured: NotSupportedError at generateKey);
//   3. the mask WITHOUT the password route, and a sentence that says why -
//      the curves are there and a passkey would get in, but the engine hands
//      out no public half of a password seed (Firefox 141, measured:
//      OperationError at exportKey("jwk"); see shell/main.js
//      passwordLoginFits).
//   4. the boot.noroute.passkeysoff message and NO mask - the password route
//      is shut AND this engine cannot be asked for a passkey either, so there
//      is no way in at all (Playwright's WebKit on Linux, measured:
//      navigator.credentials undefined; see fixtures/page.js passkeyAskGap).
//      Ending 3 offers a passkey button, so it needs an engine that HAS the
//      passkey route - which is why the third branch below asks for it
//      instead of assuming it.
//
// The third was a white lie of a screen before this round: Firefox drew the
// mask, took the password, ran Argon2id over it and said "Anmeldung
// fehlgeschlagen." - the sentence for a wrong password. Which ending applies
// is decided by the MEASURED gaps of this engine, never by its name.
test("the shell either brings the desktop up or says why it cannot", async ({ shell }) => {
  if (await hasSessionKeys(shell)) {
    await signIn(shell);
    await expectDesktop(shell);
    await expect(shell.getByRole("button", { name: "Start" })).toBeVisible();
    return;
  }
  if ((await shellGap(shell)) !== "") {
    // THE SENTENCE IS NOT WRITTEN OUT HERE, and that is the whole repair of
    // this branch. Until 03.11.2026 it read
    //
    //   toContainText("Dieser Browser ist zu alt für NGC.")
    //
    // and it had been RED on WebKit since 18.10.2026 - sixteen days, seen by
    // nobody, because the shell's wording was rewritten in 95a95476 while
    // this copy of it stayed behind. Measured then: "zu alt für NGC" existed
    // in this file and in no line of the shipped shell.
    //
    // It was not found by a case failing. The whole web suite runs in no CI
    // step (the Linux workflow names neither playwright nor web/tests), and
    // nobody drives WebKit by hand - so this branch, which exists precisely
    // for the engine that can do least, is the one nobody ever saw.
    //
    // THE WORDING IS GUARDED, once: shell/engines.spec.js:275 holds
    // boot.tooold with its exact text in all five languages, plus a check
    // that it names no version number, and that case is GREEN on WebKit
    // (measured 03.11.2026, "turned away in de-DE"). So this branch asks a
    // different question - WHICH message the boot ends in - and takes the
    // text from the file the shell itself serves. Two checks for two
    // mistakes, and the wording lives in one place.
    //
    // NOT read from shell/lang.js BUILT_IN: that is the fallback for a release
    // without assets/lang/, and the shell here serves de.json. Asking the
    // served file is asking what this page was actually given.
    const advise = await shell.evaluate(async () => {
      const r = await fetch("/assets/lang/de.json", { cache: "no-store" });
      return r.ok ? (await r.json())["boot.tooold"] : "";
    });
    // The grundmenge, and it is not decoration: toContainText("") passes on
    // every page there is, so a key that went missing would turn this case
    // into one that measures nothing while staying green - the exact failure
    // mode this repair exists to end.
    expect(advise.length, "de.json carries boot.tooold").toBeGreaterThan(20);
    await expect(shell.locator(".msg")).toContainText(advise);
    await expect(shell.locator("#taskbar")).toHaveCount(0);
    return;
  }
  // Ending 4. No password route and no passkey route: the shell says that and
  // draws no mask. A mask here would offer a button that cannot be pressed.
  const askGap = await passkeyAskGap(shell);
  if (askGap !== "") {
    await expect(shell.locator(".msg"))
      .toContainText("In diesem Browser ist keine Anmeldung möglich");
    await expect(shell.getByRole("heading", { name: "Bei NGC anmelden" })).toHaveCount(0);
    await expect(shell.locator("#taskbar")).toHaveCount(0);
    return;
  }
  // Ending 3. The mask is there, it names the reason, and the one route that
  // cannot work is shut - not merely unlabelled.
  await expect(shell.getByRole("heading", { name: "Bei NGC anmelden" })).toBeVisible();
  await expect(shell.locator(".note.warn"))
    .toContainText("Dieser Browser kann die Passwort-Anmeldung nicht ausführen.");
  await expect(shell.getByRole("button", { name: "Anmelden", exact: true })).toBeDisabled();
  await expect(shell.getByLabel("Passwort")).toBeDisabled();
  // And the way that DOES work is offered. tests/web.json carries a relying
  // party, so the passkey button belongs on this mask; without one shell/main.js
  // refuses the boot outright and this branch is never reached.
  await expect(shell.getByRole("button", { name: "Mit Passkey anmelden" })).toBeEnabled();
  await expect(shell.locator("#taskbar")).toHaveCount(0);
});

// The gap this case stands on, said out loud and in ONE place, so the branch
// above is never taken for a reason nobody measured. It is also the case that
// nails shell/main.js passwordLoginFits: the boot's third ending exists if and
// only if the engine's seed step is the thing that is missing.
test("where the boot shuts the password route, the seed step is what is missing", async ({ shell }) => {
  test.skip(await hasSessionKeys(shell), "this engine can finish a password login");
  test.skip((await shellGap(shell)) !== "", "this engine cannot run the shell at all");
  // THE THIRD ENDING, NOT THE FOURTH. Where the engine cannot be asked for a
  // passkey the boot takes ending 4 for a reason that is not the seed step, so
  // this claim is about a screen that is not on it - the wrong door.
  const askGap = await passkeyAskGap(shell);
  test.skip(askGap !== "", `this engine reaches ending 4, not the mask - ${askGap}`);
  // The STEP is pinned, not the exception's name: an engine that refuses the
  // same step with another error is the same gap and the same third ending.
  expect(await loginGap(shell)).toContain("public half out of a PKCS#8 seed");
});
